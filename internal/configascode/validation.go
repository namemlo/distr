package configascode

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"path"
	"strconv"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

const (
	apiVersionV1Alpha1 = "distr.sh/v1alpha1"
	maxDocumentBytes   = 1024 * 1024
	maxDocumentDepth   = 64
)

type valueKind int

const (
	valueAny valueKind = iota
	valueString
	valueBool
	valueInteger
	valueObject
	valueArray
)

var supportedKinds = map[string]kindSchema{
	"DeploymentProcess": {
		specFields: map[string]fieldSchema{
			"description": {kind: valueString},
			"application": {kind: valueString},
			"steps":       {kind: valueArray, required: true, arrayItem: objectItem(deploymentStepFields(), false)},
		},
	},
	"Channel": {
		specFields: map[string]fieldSchema{
			"application": {kind: valueString},
			"description": {kind: valueString},
			"isDefault":   {kind: valueBool},
			"lifecycle":   {kind: valueString},
			"rules":       {kind: valueArray, arrayItem: objectItem(nil, true)},
			"sortOrder":   {kind: valueInteger},
		},
	},
	"Lifecycle": {
		specFields: map[string]fieldSchema{
			"description": {kind: valueString},
			"phases": {
				kind:     valueArray,
				required: true,
				arrayItem: objectItem(map[string]fieldSchema{
					"name":        {kind: valueString, required: true},
					"description": {kind: valueString},
					"sortOrder":   {kind: valueInteger},
				}, false),
			},
		},
	},
	"VariableSetDefinition": {
		specFields: map[string]fieldSchema{
			"description": {kind: valueString},
			"variables": {
				kind:      valueArray,
				required:  true,
				arrayItem: objectItem(variableDefinitionFields(), false),
			},
		},
	},
	"StepTemplateReference": {
		specFields: map[string]fieldSchema{
			"description": {kind: valueString},
			"source":      {kind: valueObject, required: true, allowUnknownFields: true},
			"template":    {kind: valueString, required: true},
			"version":     {kind: valueString},
		},
	},
	"Runbook": {
		specFields: map[string]fieldSchema{
			"description": {kind: valueString},
			"steps":       {kind: valueArray, required: true, arrayItem: objectItem(deploymentStepFields(), false)},
		},
	},
}

type kindSchema struct {
	specFields map[string]fieldSchema
}

type fieldSchema struct {
	kind               valueKind
	required           bool
	fields             map[string]fieldSchema
	arrayItem          *fieldSchema
	allowUnknownFields bool
	reference          bool
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
	duplicateIssues, err := detectJSONDuplicateKeys(input)
	if err != nil {
		return nil, []Issue{{DocumentIndex: -1, Path: "$", Message: fmt.Sprintf("invalid JSON: %s", err.Error())}}
	}
	if len(duplicateIssues) > 0 {
		return nil, duplicateIssues
	}

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

func detectJSONDuplicateKeys(input []byte) ([]Issue, error) {
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.UseNumber()
	issues, err := scanJSONValue(decoder, "$")
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing content")
	}
	return issues, nil
}

func scanJSONValue(decoder *json.Decoder, issuePath string) ([]Issue, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil, nil
	}

	var issues []Issue
	switch delim {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, fmt.Errorf("object key must be a string")
			}
			childPath := issuePath + "." + key
			if _, ok := seen[key]; ok {
				issues = append(issues, Issue{DocumentIndex: -1, Path: childPath, Message: fmt.Sprintf("duplicate key %q", key)})
			}
			seen[key] = struct{}{}
			childIssues, err := scanJSONValue(decoder, childPath)
			if err != nil {
				return nil, err
			}
			issues = append(issues, childIssues...)
		}
		endToken, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		if endToken != json.Delim('}') {
			return nil, fmt.Errorf("object is not closed")
		}
	case '[':
		for i := 0; decoder.More(); i++ {
			childIssues, err := scanJSONValue(decoder, fmt.Sprintf("%s[%d]", issuePath, i))
			if err != nil {
				return nil, err
			}
			issues = append(issues, childIssues...)
		}
		endToken, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		if endToken != json.Delim(']') {
			return nil, fmt.Errorf("array is not closed")
		}
	default:
		return nil, fmt.Errorf("unexpected JSON delimiter")
	}
	return issues, nil
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

	apiVersion, apiVersionOK := r.requireNonEmptyString(index, basePath+".apiVersion", root["apiVersion"])
	if apiVersionOK && apiVersion != apiVersionV1Alpha1 {
		r.addError(index, basePath+".apiVersion", "unsupported apiVersion")
	}
	kind, kindOK := r.requireNonEmptyString(index, basePath+".kind", root["kind"])
	schema, schemaOK := supportedKinds[kind]
	if kindOK && !schemaOK {
		r.addError(index, basePath+".kind", "unsupported kind")
	}

	var name string
	var repositoryPath string
	metadata, metadataOK := r.requireObject(index, basePath+".metadata", root["metadata"], "metadata must be an object")
	if metadataOK {
		r.rejectUnknownFields(index, basePath+".metadata", metadata, map[string]fieldSchema{"name": {}, "path": {}})
		name, _ = r.requireNonEmptyString(index, basePath+".metadata.name", metadata["name"])
		repositoryPath, _ = r.validateMetadataPath(index, basePath+".metadata.path", metadata["path"])
	}

	spec, specOK := r.requireObject(index, basePath+".spec", root["spec"], "spec must be an object")
	if specOK && schemaOK {
		r.validateFields(index, basePath+".spec", spec, schema.specFields, false)
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
	r.Documents = append(r.Documents, DocumentResult{
		Kind:              kind,
		APIVersion:        apiVersion,
		MetadataName:      name,
		MetadataPath:      repositoryPath,
		CanonicalChecksum: checksum,
	})
}

func (r *ValidationResult) validateMetadataPath(index int, issuePath string, rawPath any) (string, bool) {
	repositoryPath, ok := r.requireNonEmptyString(index, issuePath, rawPath)
	if !ok {
		r.Errors[len(r.Errors)-1].Message = "metadata.path must be a non-empty relative repository path"
		return "", false
	}
	if err := ValidateRepositoryPath(repositoryPath); err != nil {
		r.addError(index, issuePath, err.Error())
		return "", false
	}
	return repositoryPath, true
}

func ValidateRepositoryPath(repositoryPath string) error {
	repositoryPath = strings.TrimSpace(repositoryPath)
	if repositoryPath == "" {
		return fmt.Errorf("repository path must be a non-empty relative repository path")
	}
	if strings.Contains(repositoryPath, "\\") {
		return fmt.Errorf("repository path cannot contain backslash separators")
	}
	if path.IsAbs(repositoryPath) || hasWindowsDrivePrefix(repositoryPath) {
		return fmt.Errorf("repository path must be a relative repository path")
	}
	cleaned := path.Clean(repositoryPath)
	if cleaned == "." {
		return fmt.Errorf("repository path must be a non-empty relative repository path")
	}
	for _, segment := range strings.Split(repositoryPath, "/") {
		if segment == ".." {
			return fmt.Errorf("repository path cannot contain traversal")
		}
	}
	return nil
}

func (r *ValidationResult) validateFields(index int, issuePath string, value map[string]any, fields map[string]fieldSchema, allowUnknown bool) {
	if !allowUnknown {
		r.rejectUnknownFields(index, issuePath, value, fields)
	}
	for key, schema := range fields {
		rawValue, exists := value[key]
		if !exists {
			if schema.required {
				r.addMissingFieldError(index, issuePath+"."+key, schema)
			}
			continue
		}
		r.validateValue(index, issuePath+"."+key, rawValue, schema)
	}
}

func (r *ValidationResult) validateValue(index int, issuePath string, value any, schema fieldSchema) {
	if schema.reference {
		if str, ok := value.(string); !ok || strings.TrimSpace(str) == "" {
			r.addError(index, issuePath, "must be a non-empty string reference")
		}
		return
	}

	switch schema.kind {
	case valueAny:
		return
	case valueString:
		if str, ok := value.(string); !ok || strings.TrimSpace(str) == "" {
			r.addError(index, issuePath, "must be a non-empty string")
		}
	case valueBool:
		if _, ok := value.(bool); !ok {
			r.addError(index, issuePath, "must be a boolean")
		}
	case valueInteger:
		if !isIntegerValue(value) {
			r.addError(index, issuePath, "must be an integer")
		}
	case valueObject:
		object, ok := value.(map[string]any)
		if !ok {
			r.addError(index, issuePath, "must be an object")
			return
		}
		if schema.fields != nil || !schema.allowUnknownFields {
			r.validateFields(index, issuePath, object, schema.fields, schema.allowUnknownFields)
		}
	case valueArray:
		items, ok := value.([]any)
		if !ok {
			r.addError(index, issuePath, "must be an array")
			return
		}
		if schema.arrayItem == nil {
			return
		}
		for i, item := range items {
			r.validateValue(index, fmt.Sprintf("%s[%d]", issuePath, i), item, *schema.arrayItem)
		}
	}
}

func (r *ValidationResult) addMissingFieldError(index int, issuePath string, schema fieldSchema) {
	if schema.reference {
		r.addError(index, issuePath, "must be a non-empty string reference")
		return
	}
	switch schema.kind {
	case valueObject:
		r.addError(index, issuePath, "must be an object")
	case valueArray:
		r.addError(index, issuePath, "must be an array")
	case valueBool:
		r.addError(index, issuePath, "must be a boolean")
	case valueInteger:
		r.addError(index, issuePath, "must be an integer")
	default:
		r.addError(index, issuePath, "must be a non-empty string")
	}
}

func (r *ValidationResult) requireObject(index int, issuePath string, raw any, message string) (map[string]any, bool) {
	value, ok := raw.(map[string]any)
	if !ok {
		r.addError(index, issuePath, message)
		return nil, false
	}
	return value, true
}

func (r *ValidationResult) requireNonEmptyString(index int, issuePath string, raw any) (string, bool) {
	value, ok := raw.(string)
	if !ok || strings.TrimSpace(value) == "" {
		r.addError(index, issuePath, "must be a non-empty string")
		return "", false
	}
	return value, true
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
	normalized, err := canonicalValue(value)
	if err != nil {
		return "", err
	}
	canonical, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

func canonicalValue(value any) (any, error) {
	switch typed := value.(type) {
	case map[string]any:
		normalized := make(map[string]any, len(typed))
		for key, item := range typed {
			child, err := canonicalValue(item)
			if err != nil {
				return nil, err
			}
			normalized[key] = child
		}
		return normalized, nil
	case []any:
		normalized := make([]any, 0, len(typed))
		for _, item := range typed {
			child, err := canonicalValue(item)
			if err != nil {
				return nil, err
			}
			normalized = append(normalized, child)
		}
		return normalized, nil
	case json.Number:
		return canonicalNumber(string(typed))
	case int:
		return int64(typed), nil
	case int64:
		return typed, nil
	case float64:
		return canonicalFloat(typed)
	default:
		return value, nil
	}
}

func canonicalNumber(value string) (any, error) {
	if integer, err := strconv.ParseInt(value, 10, 64); err == nil {
		return integer, nil
	}
	floatValue, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return nil, err
	}
	return canonicalFloat(floatValue)
}

func canonicalFloat(value float64) (any, error) {
	if math.IsInf(value, 0) || math.IsNaN(value) {
		return nil, fmt.Errorf("invalid number")
	}
	if math.Trunc(value) == value && value >= math.MinInt64 && value <= math.MaxInt64 {
		return int64(value), nil
	}
	return value, nil
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
		"actionType":     {kind: valueString, required: true},
		"description":    {kind: valueString},
		"inputBindings":  {kind: valueObject, allowUnknownFields: true},
		"key":            {kind: valueString, required: true},
		"name":           {kind: valueString},
		"outputBindings": {kind: valueObject, allowUnknownFields: true},
	}
}

func variableDefinitionFields() map[string]fieldSchema {
	return map[string]fieldSchema{
		"name":           {kind: valueString, required: true},
		"type":           {kind: valueString, required: true},
		"description":    {kind: valueString},
		"default":        {kind: valueAny},
		"secretRef":      {kind: valueString, reference: true},
		"accountRef":     {kind: valueString, reference: true},
		"certificateRef": {kind: valueString, reference: true},
	}
}

func objectItem(fields map[string]fieldSchema, allowUnknown bool) *fieldSchema {
	return &fieldSchema{kind: valueObject, fields: fields, allowUnknownFields: allowUnknown}
}

func isIntegerValue(value any) bool {
	switch typed := value.(type) {
	case int:
		return true
	case int64:
		return true
	case float64:
		return math.Trunc(typed) == typed && !math.IsInf(typed, 0) && !math.IsNaN(typed)
	case json.Number:
		if _, err := strconv.ParseInt(string(typed), 10, 64); err == nil {
			return true
		}
		floatValue, err := strconv.ParseFloat(string(typed), 64)
		return err == nil && math.Trunc(floatValue) == floatValue && !math.IsInf(floatValue, 0) && !math.IsNaN(floatValue)
	default:
		return false
	}
}

func hasWindowsDrivePrefix(value string) bool {
	return len(value) >= 3 && value[1] == ':' && (value[2] == '/' || value[2] == '\\') && unicode.IsLetter(rune(value[0]))
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
	case strings.Contains(normalized, "apikey"):
		return true
	case strings.Contains(normalized, "accesskey"):
		return true
	case strings.Contains(normalized, "authorization"):
		return true
	case strings.Contains(normalized, "credential"):
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
		strings.Contains(normalized, "apikey") ||
		strings.Contains(normalized, "accesskey") ||
		strings.Contains(normalized, "authorization") ||
		strings.Contains(normalized, "credential") ||
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
