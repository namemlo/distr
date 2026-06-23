package configascode

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	apiVersionV1Alpha1 = "distr.sh/v1alpha1"
	maxDocumentBytes   = 1024 * 1024
	maxDocumentDepth   = 64
)

var supportedKinds = map[string]kindSchema{
	"DeploymentProcess": {
		specFields: map[string]fieldSchema{
			"description": {},
			"steps":       {arrayItemFields: deploymentStepFields()},
			"application": {},
		},
	},
	"Channel": {
		specFields: map[string]fieldSchema{
			"application": {},
			"description": {},
			"isDefault":   {},
			"lifecycle":   {},
			"rules":       {},
			"sortOrder":   {},
		},
	},
	"Lifecycle": {
		specFields: map[string]fieldSchema{
			"description": {},
			"phases":      {},
		},
	},
	"VariableSetDefinition": {
		specFields: map[string]fieldSchema{
			"description": {},
			"variables": {
				arrayItemFields: map[string]fieldSchema{
					"name":           {},
					"type":           {},
					"description":    {},
					"default":        {},
					"secretRef":      {reference: true},
					"accountRef":     {reference: true},
					"certificateRef": {reference: true},
				},
			},
		},
	},
	"StepTemplateReference": {
		specFields: map[string]fieldSchema{
			"description": {},
			"source":      {},
			"template":    {},
			"version":     {},
		},
	},
	"Runbook": {
		specFields: map[string]fieldSchema{
			"description": {},
			"steps":       {arrayItemFields: deploymentStepFields()},
		},
	},
}

type kindSchema struct {
	specFields map[string]fieldSchema
}

type fieldSchema struct {
	arrayItemFields map[string]fieldSchema
	reference       bool
}

func ValidateDocuments(input []byte) ValidationResult {
	result := ValidationResult{Valid: true}
	if len(input) > maxDocumentBytes {
		result.addError(-1, "$", "document is too large")
		return result
	}
	if len(bytes.TrimSpace(input)) == 0 {
		result.addError(-1, "$", "document is empty")
		return result
	}

	documents, parseIssues := parseDocuments(input)
	for _, issue := range parseIssues {
		result.Errors = append(result.Errors, issue)
	}
	for i, document := range documents {
		result.validateDocument(i, document)
	}

	result.Valid = len(result.Errors) == 0
	return result
}

func parseDocuments(input []byte) ([]any, []Issue) {
	trimmed := bytes.TrimSpace(input)
	if len(trimmed) == 0 {
		return nil, nil
	}
	switch trimmed[0] {
	case '{', '[':
		return parseJSONDocuments(trimmed)
	default:
		return parseYAMLDocuments(input)
	}
}

func parseJSONDocuments(input []byte) ([]any, []Issue) {
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.UseNumber()
	var raw any
	if err := decoder.Decode(&raw); err != nil {
		return nil, []Issue{{DocumentIndex: -1, Path: "$", Message: fmt.Sprintf("invalid JSON: %s", err.Error())}}
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, []Issue{{DocumentIndex: -1, Path: "$", Message: "invalid JSON: trailing content"}}
	}
	switch typed := raw.(type) {
	case []any:
		return typed, nil
	default:
		return []any{raw}, nil
	}
}

func parseYAMLDocuments(input []byte) ([]any, []Issue) {
	decoder := yaml.NewDecoder(bytes.NewReader(input))
	var documents []any
	var issues []Issue
	for {
		var node yaml.Node
		if err := decoder.Decode(&node); err != nil {
			if err == io.EOF {
				break
			}
			return documents, append(issues, Issue{DocumentIndex: len(documents), Path: fmt.Sprintf("$[%d]", len(documents)), Message: fmt.Sprintf("invalid YAML: %s", err.Error())})
		}
		if len(node.Content) == 0 {
			continue
		}
		index := len(documents)
		value, nodeIssues := yamlNodeToValue(node.Content[0], fmt.Sprintf("$[%d]", index), index, 0)
		issues = append(issues, nodeIssues...)
		documents = append(documents, value)
	}
	return documents, issues
}

func yamlNodeToValue(node *yaml.Node, issuePath string, documentIndex, depth int) (any, []Issue) {
	if depth > maxDocumentDepth {
		return nil, []Issue{{DocumentIndex: documentIndex, Path: issuePath, Message: "document is too deeply nested"}}
	}
	if node.Anchor != "" || node.Kind == yaml.AliasNode {
		return nil, []Issue{{
			DocumentIndex: documentIndex,
			Path:          issuePath,
			Message:       "YAML anchors and aliases are not supported",
		}}
	}

	switch node.Kind {
	case yaml.MappingNode:
		value := map[string]any{}
		seen := map[string]struct{}{}
		var issues []Issue
		for i := 0; i < len(node.Content); i += 2 {
			keyNode := node.Content[i]
			valueNode := node.Content[i+1]
			key := keyNode.Value
			childPath := issuePath + "." + key
			if _, ok := seen[key]; ok {
				issues = append(issues, Issue{DocumentIndex: documentIndex, Path: childPath, Message: fmt.Sprintf("duplicate key %q", key)})
				continue
			}
			seen[key] = struct{}{}
			childValue, childIssues := yamlNodeToValue(valueNode, childPath, documentIndex, depth+1)
			issues = append(issues, childIssues...)
			value[key] = childValue
		}
		return value, issues
	case yaml.SequenceNode:
		value := make([]any, 0, len(node.Content))
		var issues []Issue
		for i, child := range node.Content {
			childValue, childIssues := yamlNodeToValue(child, fmt.Sprintf("%s[%d]", issuePath, i), documentIndex, depth+1)
			issues = append(issues, childIssues...)
			value = append(value, childValue)
		}
		return value, issues
	case yaml.ScalarNode:
		return yamlScalarValue(node), nil
	case yaml.DocumentNode:
		if len(node.Content) == 0 {
			return nil, nil
		}
		return yamlNodeToValue(node.Content[0], issuePath, documentIndex, depth)
	default:
		return nil, []Issue{{DocumentIndex: documentIndex, Path: issuePath, Message: "unsupported YAML node"}}
	}
}

func yamlScalarValue(node *yaml.Node) any {
	switch node.Tag {
	case "!!null":
		return nil
	case "!!bool":
		value, err := strconv.ParseBool(node.Value)
		if err == nil {
			return value
		}
	case "!!int":
		value, err := strconv.ParseInt(node.Value, 10, 64)
		if err == nil {
			return value
		}
	case "!!float":
		value, err := strconv.ParseFloat(node.Value, 64)
		if err == nil {
			return value
		}
	}
	return node.Value
}

func (r *ValidationResult) validateDocument(index int, document any) {
	basePath := fmt.Sprintf("$[%d]", index)
	if tooDeep(document, 0) {
		r.addError(index, basePath, "document is too deeply nested")
		return
	}
	root, ok := document.(map[string]any)
	if !ok {
		r.addError(index, basePath, "document must be an object")
		return
	}
	r.rejectUnknownFields(index, basePath, root, map[string]fieldSchema{
		"apiVersion": {},
		"kind":       {},
		"metadata":   {},
		"spec":       {},
	})

	apiVersion, _ := root["apiVersion"].(string)
	if apiVersion != apiVersionV1Alpha1 {
		r.addError(index, basePath+".apiVersion", "unsupported apiVersion")
	}
	kind, _ := root["kind"].(string)
	schema, ok := supportedKinds[kind]
	if !ok {
		r.addError(index, basePath+".kind", "unsupported kind")
	}
	metadata, _ := root["metadata"].(map[string]any)
	if metadata == nil {
		r.addError(index, basePath+".metadata", "metadata must be an object")
	} else {
		r.rejectUnknownFields(index, basePath+".metadata", metadata, map[string]fieldSchema{"name": {}, "path": {}})
		r.validateMetadataPath(index, basePath+".metadata.path", metadata["path"])
	}
	spec, _ := root["spec"].(map[string]any)
	if spec == nil {
		r.addError(index, basePath+".spec", "spec must be an object")
	} else if ok {
		r.validateSpec(index, basePath+".spec", spec, schema.specFields)
	}
	r.rejectPlaintextSecrets(index, basePath, root, false)

	if hasDocumentError(r.Errors, index) {
		return
	}
	checksum, err := canonicalChecksum(document)
	if err != nil {
		r.addError(index, basePath, "document could not be canonicalized")
		return
	}
	name, _ := metadata["name"].(string)
	repositoryPath, _ := metadata["path"].(string)
	r.Documents = append(r.Documents, DocumentResult{
		Kind:              kind,
		APIVersion:        apiVersion,
		MetadataName:      name,
		MetadataPath:      repositoryPath,
		CanonicalChecksum: checksum,
	})
}

func (r *ValidationResult) validateMetadataPath(index int, issuePath string, rawPath any) {
	repositoryPath, ok := rawPath.(string)
	if !ok || strings.TrimSpace(repositoryPath) == "" {
		r.addError(index, issuePath, "metadata.path must be a non-empty relative repository path")
		return
	}
	repositoryPath = strings.ReplaceAll(repositoryPath, "\\", "/")
	if path.IsAbs(repositoryPath) || filepath.IsAbs(repositoryPath) {
		r.addError(index, issuePath, "metadata.path must be a relative repository path")
		return
	}
	cleaned := path.Clean(repositoryPath)
	if cleaned == "." || strings.HasPrefix(cleaned, "../") || cleaned == ".." || strings.Contains(cleaned, "/../") {
		r.addError(index, issuePath, "metadata.path cannot contain traversal")
	}
}

func (r *ValidationResult) validateSpec(index int, issuePath string, spec map[string]any, fields map[string]fieldSchema) {
	r.rejectUnknownFields(index, issuePath, spec, fields)
	for key, schema := range fields {
		if schema.arrayItemFields == nil {
			continue
		}
		rawItems, ok := spec[key].([]any)
		if !ok {
			continue
		}
		for i, rawItem := range rawItems {
			item, ok := rawItem.(map[string]any)
			if !ok {
				r.addError(index, fmt.Sprintf("%s.%s[%d]", issuePath, key, i), "array item must be an object")
				continue
			}
			r.rejectUnknownFields(index, fmt.Sprintf("%s.%s[%d]", issuePath, key, i), item, schema.arrayItemFields)
		}
	}
}

func (r *ValidationResult) rejectUnknownFields(index int, issuePath string, value map[string]any, allowed map[string]fieldSchema) {
	for key := range value {
		if _, ok := allowed[key]; !ok {
			r.addError(index, issuePath+"."+key, "unknown field")
		}
	}
}

func (r *ValidationResult) rejectPlaintextSecrets(index int, issuePath string, value any, inReference bool) {
	switch typed := value.(type) {
	case map[string]any:
		secretLikeVariable := variableDefinitionLooksSecret(typed)
		for key, item := range typed {
			childPath := issuePath + "." + key
			if isReferenceField(key) {
				r.rejectPlaintextSecrets(index, childPath, item, true)
				continue
			}
			if isSensitiveFieldName(key) {
				r.addError(index, childPath, "plaintext secret values are not allowed; use a secret/account/certificate reference")
				continue
			}
			if key == "default" && secretLikeVariable {
				r.addError(index, childPath, "plaintext secret values are not allowed; use a secret/account/certificate reference")
				continue
			}
			r.rejectPlaintextSecrets(index, childPath, item, inReference)
		}
	case []any:
		for i, item := range typed {
			r.rejectPlaintextSecrets(index, fmt.Sprintf("%s[%d]", issuePath, i), item, inReference)
		}
	case string:
		if !inReference && stringLooksSecret(typed) {
			r.addError(index, issuePath, "plaintext secret values are not allowed; use a secret/account/certificate reference")
		}
	}
}

func canonicalChecksum(value any) (string, error) {
	canonical, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

func tooDeep(value any, depth int) bool {
	if depth > maxDocumentDepth {
		return true
	}
	switch typed := value.(type) {
	case map[string]any:
		for _, item := range typed {
			if tooDeep(item, depth+1) {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if tooDeep(item, depth+1) {
				return true
			}
		}
	}
	return false
}

func (r *ValidationResult) addError(documentIndex int, issuePath, message string) {
	r.Valid = false
	r.Errors = append(r.Errors, Issue{
		DocumentIndex: documentIndex,
		Path:          issuePath,
		Message:       message,
	})
}

func hasDocumentError(errors []Issue, documentIndex int) bool {
	for _, issue := range errors {
		if issue.DocumentIndex == documentIndex {
			return true
		}
	}
	return false
}

func deploymentStepFields() map[string]fieldSchema {
	return map[string]fieldSchema{
		"actionType":     {},
		"description":    {},
		"inputBindings":  {},
		"key":            {},
		"name":           {},
		"outputBindings": {},
	}
}

func variableDefinitionLooksSecret(value map[string]any) bool {
	name, _ := value["name"].(string)
	variableType, _ := value["type"].(string)
	return isSensitiveIdentifier(name) || strings.EqualFold(variableType, "secret")
}

func isReferenceField(key string) bool {
	switch key {
	case "secretRef", "accountRef", "certificateRef", "passwordSecretRef", "signingSecretRef":
		return true
	default:
		return false
	}
}

func isSensitiveFieldName(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "_", ""), "-", ""))
	switch {
	case strings.Contains(normalized, "password"):
		return true
	case strings.Contains(normalized, "token"):
		return true
	case strings.Contains(normalized, "privatekey"):
		return true
	case strings.Contains(normalized, "connectionstring"):
		return true
	case strings.Contains(normalized, "secret") && !strings.HasSuffix(normalized, "ref"):
		return true
	default:
		return false
	}
}

func isSensitiveIdentifier(value string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(value, "_", ""), "-", ""))
	return strings.Contains(normalized, "password") ||
		strings.Contains(normalized, "token") ||
		strings.Contains(normalized, "privatekey") ||
		strings.Contains(normalized, "connectionstring") ||
		strings.Contains(normalized, "secret")
}

func stringLooksSecret(value string) bool {
	trimmed := strings.TrimSpace(value)
	lower := strings.ToLower(trimmed)
	return strings.Contains(trimmed, "-----BEGIN PRIVATE KEY-----") ||
		strings.Contains(lower, "password=") ||
		strings.Contains(lower, "user id=") && strings.Contains(lower, "password=") ||
		strings.HasPrefix(lower, "bearer ")
}
