package variabledrift

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/compose-spec/compose-go/v2/dotenv"
	"github.com/distr-sh/distr/internal/types"
	"gopkg.in/yaml.v3"
)

type DeployedConfiguration struct {
	EnvFileData []byte
	ValuesYAML  []byte
}

type deployedValue struct {
	raw      json.RawMessage
	typeName string
}

func Compare(
	current []types.ResolvedVariable,
	deployed DeployedConfiguration,
) (types.ConfigurationDrift, error) {
	values, err := parseDeployedValues(deployed)
	if err != nil {
		return types.ConfigurationDrift{}, err
	}

	drift := types.ConfigurationDrift{
		NewRequiredVariables:   []types.ConfigurationDriftVariable{},
		MissingVariables:       []types.ConfigurationDriftVariable{},
		RemovedVariables:       []types.ConfigurationDriftRemovedValue{},
		TypeChanges:            []types.ConfigurationDriftTypeChange{},
		DefaultChanges:         []types.ConfigurationDriftDefaultChange{},
		SecretReferenceChanges: []types.ConfigurationDriftReferenceChange{},
	}

	currentByKey := map[string]types.ResolvedVariable{}
	for _, variable := range current {
		key := strings.TrimSpace(variable.Key)
		if key == "" {
			continue
		}
		currentByKey[key] = variable
		value, ok := values[key]
		if !ok {
			missing := configurationDriftVariable(variable)
			if variable.IsRequired {
				drift.NewRequiredVariables = append(drift.NewRequiredVariables, missing)
			} else {
				drift.MissingVariables = append(drift.MissingVariables, missing)
			}
			continue
		}
		if isReferenceVariableType(variable.Type) {
			if referenceChanged(variable, value) {
				drift.SecretReferenceChanges = append(
					drift.SecretReferenceChanges,
					configurationDriftReferenceChange(variable),
				)
			}
			continue
		}
		coerced, ok := coerceDeployedValue(value.raw, variable.Type)
		if !ok {
			drift.TypeChanges = append(drift.TypeChanges, types.ConfigurationDriftTypeChange{
				Key:          key,
				ExpectedType: variable.Type,
				DeployedType: value.typeName,
			})
			continue
		}
		if hasRawJSONValue(variable.Value) && !jsonEqual(variable.Value, coerced) {
			drift.DefaultChanges = append(drift.DefaultChanges, types.ConfigurationDriftDefaultChange{
				Key:           key,
				Type:          variable.Type,
				CurrentValue:  cloneRawMessage(variable.Value),
				DeployedValue: cloneRawMessage(coerced),
			})
		}
	}

	for key := range values {
		if _, ok := currentByKey[key]; !ok && !hasCurrentParentKey(currentByKey, key) {
			drift.RemovedVariables = append(drift.RemovedVariables, types.ConfigurationDriftRemovedValue{Key: key})
		}
	}

	sortDrift(&drift)
	drift.HasDrift = len(drift.NewRequiredVariables) > 0 ||
		len(drift.MissingVariables) > 0 ||
		len(drift.RemovedVariables) > 0 ||
		len(drift.TypeChanges) > 0 ||
		len(drift.DefaultChanges) > 0 ||
		len(drift.SecretReferenceChanges) > 0
	return drift, nil
}

func parseDeployedValues(config DeployedConfiguration) (map[string]deployedValue, error) {
	result := map[string]deployedValue{}
	if len(bytes.TrimSpace(config.ValuesYAML)) > 0 {
		var decoded any
		if err := yaml.Unmarshal(config.ValuesYAML, &decoded); err != nil {
			return nil, fmt.Errorf("parse deployed values YAML: %w", err)
		}
		flattenYAML(result, "", normalizeYAML(decoded))
	}
	if len(bytes.TrimSpace(config.EnvFileData)) > 0 {
		envValues, err := dotenv.UnmarshalBytesWithLookup(config.EnvFileData, nil)
		if err != nil {
			return nil, fmt.Errorf("parse deployed env file: %w", err)
		}
		for key, value := range envValues {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			result[key] = deployedValue{
				raw:      json.RawMessage(strconv.Quote(value)),
				typeName: deployedJSONType(json.RawMessage(strconv.Quote(value))),
			}
		}
	}
	return result, nil
}

func flattenYAML(result map[string]deployedValue, prefix string, value any) {
	switch typed := value.(type) {
	case map[string]any:
		if prefix != "" {
			if raw, err := json.Marshal(typed); err == nil {
				result[prefix] = deployedValue{raw: raw, typeName: deployedJSONType(raw)}
			}
		}
		for key, nested := range typed {
			nextKey := key
			if prefix != "" {
				nextKey = prefix + "." + key
			}
			flattenYAML(result, nextKey, nested)
		}
	case []any:
		for i, nested := range typed {
			nextKey := fmt.Sprintf("%s[%d]", prefix, i)
			flattenYAML(result, nextKey, nested)
		}
	default:
		if prefix == "" {
			return
		}
		raw, err := json.Marshal(typed)
		if err != nil {
			return
		}
		result[prefix] = deployedValue{raw: raw, typeName: deployedJSONType(raw)}
	}
}

func hasCurrentParentKey(currentByKey map[string]types.ResolvedVariable, deployedKey string) bool {
	for key := range currentByKey {
		if strings.HasPrefix(deployedKey, key+".") || strings.HasPrefix(deployedKey, key+"[") {
			return true
		}
	}
	return false
}

func normalizeYAML(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := map[string]any{}
		for key, nested := range typed {
			result[key] = normalizeYAML(nested)
		}
		return result
	case map[any]any:
		result := map[string]any{}
		for key, nested := range typed {
			result[fmt.Sprint(key)] = normalizeYAML(nested)
		}
		return result
	case []any:
		result := make([]any, 0, len(typed))
		for _, nested := range typed {
			result = append(result, normalizeYAML(nested))
		}
		return result
	default:
		return typed
	}
}

func configurationDriftVariable(variable types.ResolvedVariable) types.ConfigurationDriftVariable {
	return types.ConfigurationDriftVariable{
		Key:           strings.TrimSpace(variable.Key),
		Type:          variable.Type,
		IsRequired:    variable.IsRequired,
		Source:        variable.Source,
		Value:         cloneRawMessage(variable.Value),
		ReferenceID:   variable.ReferenceID,
		ReferenceName: variable.ReferenceName,
		Redacted:      variable.Redacted,
	}
}

func referenceChanged(variable types.ResolvedVariable, value deployedValue) bool {
	if variable.Type == types.VariableTypeSecretReference {
		return true
	}
	var deployedString string
	if err := json.Unmarshal(value.raw, &deployedString); err != nil {
		return true
	}
	return deployedString != variable.ReferenceID && deployedString != variable.ReferenceName
}

func configurationDriftReferenceChange(variable types.ResolvedVariable) types.ConfigurationDriftReferenceChange {
	return types.ConfigurationDriftReferenceChange{
		Key:           strings.TrimSpace(variable.Key),
		Type:          variable.Type,
		ReferenceID:   variable.ReferenceID,
		ReferenceName: variable.ReferenceName,
		Redacted:      variable.Type == types.VariableTypeSecretReference || variable.Redacted,
	}
}

func coerceDeployedValue(value json.RawMessage, variableType types.VariableType) (json.RawMessage, bool) {
	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return nil, false
	}
	switch variableType {
	case types.VariableTypeString:
		if _, ok := decoded.(string); ok {
			return cloneRawMessage(value), true
		}
		return nil, false
	case types.VariableTypeNumber:
		switch typed := decoded.(type) {
		case json.Number:
			if _, err := typed.Float64(); err != nil {
				return nil, false
			}
			return cloneRawMessage(value), true
		case string:
			if _, err := strconv.ParseFloat(typed, 64); err != nil {
				return nil, false
			}
			return json.RawMessage(typed), true
		default:
			return nil, false
		}
	case types.VariableTypeBoolean:
		switch typed := decoded.(type) {
		case bool:
			return cloneRawMessage(value), true
		case string:
			if typed != "true" && typed != "false" {
				return nil, false
			}
			return json.RawMessage(typed), true
		default:
			return nil, false
		}
	case types.VariableTypeJSON:
		return cloneRawMessage(value), decoded != nil
	default:
		return cloneRawMessage(value), true
	}
}

func deployedJSONType(value json.RawMessage) string {
	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return "invalid"
	}
	switch decoded.(type) {
	case string:
		return "string"
	case json.Number:
		return "number"
	case bool:
		return "boolean"
	case []any:
		return "json"
	case map[string]any:
		return "json"
	case nil:
		return "null"
	default:
		return "json"
	}
}

func jsonEqual(left, right json.RawMessage) bool {
	var decodedLeft any
	var decodedRight any
	leftDecoder := json.NewDecoder(bytes.NewReader(left))
	leftDecoder.UseNumber()
	rightDecoder := json.NewDecoder(bytes.NewReader(right))
	rightDecoder.UseNumber()
	if err := leftDecoder.Decode(&decodedLeft); err != nil {
		return false
	}
	if err := rightDecoder.Decode(&decodedRight); err != nil {
		return false
	}
	return reflect.DeepEqual(decodedLeft, decodedRight)
}

func isReferenceVariableType(value types.VariableType) bool {
	switch value {
	case types.VariableTypeSecretReference,
		types.VariableTypeAccountReference,
		types.VariableTypeCertificateReference:
		return true
	default:
		return false
	}
}

func hasRawJSONValue(value json.RawMessage) bool {
	trimmed := bytes.TrimSpace(value)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}

func cloneRawMessage(value json.RawMessage) json.RawMessage {
	if value == nil {
		return nil
	}
	clone := make([]byte, len(value))
	copy(clone, value)
	return clone
}

func sortDrift(drift *types.ConfigurationDrift) {
	sort.Slice(drift.NewRequiredVariables, func(i, j int) bool {
		return drift.NewRequiredVariables[i].Key < drift.NewRequiredVariables[j].Key
	})
	sort.Slice(drift.MissingVariables, func(i, j int) bool {
		return drift.MissingVariables[i].Key < drift.MissingVariables[j].Key
	})
	sort.Slice(drift.RemovedVariables, func(i, j int) bool {
		return drift.RemovedVariables[i].Key < drift.RemovedVariables[j].Key
	})
	sort.Slice(drift.TypeChanges, func(i, j int) bool {
		return drift.TypeChanges[i].Key < drift.TypeChanges[j].Key
	})
	sort.Slice(drift.DefaultChanges, func(i, j int) bool {
		return drift.DefaultChanges[i].Key < drift.DefaultChanges[j].Key
	})
	sort.Slice(drift.SecretReferenceChanges, func(i, j int) bool {
		return drift.SecretReferenceChanges[i].Key < drift.SecretReferenceChanges[j].Key
	})
}
