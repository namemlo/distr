package stepredaction

import (
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const redactedValue = "[REDACTED]"

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(authorization\s*:\s*bearer\s+)[^\s,;]+`),
	regexp.MustCompile(`(?i)\b(password|passwd|secret|token|api[_-]?key)\b(\s*[:=]\s*)[^\s,;]+`),
	regexp.MustCompile(`(?i)\bsha256=[a-f0-9]{64}\b`),
}

var secretKeyPattern = regexp.MustCompile(`(?i)(password|passwd|secret|token|api[_-]?key|authorization)`)

func RedactString(value string) (string, bool) {
	return RedactStringWithValues(value, nil)
}

func RedactStringWithValues(value string, secretValues []string) (string, bool) {
	result := strings.ReplaceAll(value, "\x00", "")
	changed := result != value
	for _, pattern := range secretPatterns {
		next := pattern.ReplaceAllString(result, "${1}${2}"+redactedValue)
		if next != result {
			changed = true
			result = next
		}
	}
	for _, secretValue := range normalizedSecretValues(secretValues) {
		next := strings.ReplaceAll(result, secretValue, redactedValue)
		if next != result {
			changed = true
			result = next
		}
	}
	return result, changed
}

func RedactValue(value any) (any, bool) {
	return RedactValueWithValues(value, nil)
}

func RedactValueWithValues(value any, secretValues []string) (any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		changed := false
		result := make(map[string]any, len(typed))
		for key, child := range typed {
			redactedKey, keyChanged := RedactStringWithValues(key, secretValues)
			changed = changed || keyChanged
			if secretKeyPattern.MatchString(key) {
				result[redactedKey] = redactedValue
				changed = true
				continue
			}
			redacted, childChanged := RedactValueWithValues(child, secretValues)
			result[redactedKey] = redacted
			changed = changed || childChanged
		}
		return result, changed
	case []any:
		changed := false
		result := make([]any, len(typed))
		for i, child := range typed {
			redacted, childChanged := RedactValueWithValues(child, secretValues)
			result[i] = redacted
			changed = changed || childChanged
		}
		return result, changed
	case string:
		return RedactStringWithValues(typed, secretValues)
	case json.Number:
		return redactScalarStringWithValues(typed.String(), value, secretValues)
	case float64:
		return redactScalarStringWithValues(strconv.FormatFloat(typed, 'f', -1, 64), value, secretValues)
	case float32:
		return redactScalarStringWithValues(strconv.FormatFloat(float64(typed), 'f', -1, 32), value, secretValues)
	case int:
		return redactScalarStringWithValues(strconv.FormatInt(int64(typed), 10), value, secretValues)
	case int8:
		return redactScalarStringWithValues(strconv.FormatInt(int64(typed), 10), value, secretValues)
	case int16:
		return redactScalarStringWithValues(strconv.FormatInt(int64(typed), 10), value, secretValues)
	case int32:
		return redactScalarStringWithValues(strconv.FormatInt(int64(typed), 10), value, secretValues)
	case int64:
		return redactScalarStringWithValues(strconv.FormatInt(typed, 10), value, secretValues)
	case uint:
		return redactScalarStringWithValues(strconv.FormatUint(uint64(typed), 10), value, secretValues)
	case uint8:
		return redactScalarStringWithValues(strconv.FormatUint(uint64(typed), 10), value, secretValues)
	case uint16:
		return redactScalarStringWithValues(strconv.FormatUint(uint64(typed), 10), value, secretValues)
	case uint32:
		return redactScalarStringWithValues(strconv.FormatUint(uint64(typed), 10), value, secretValues)
	case uint64:
		return redactScalarStringWithValues(strconv.FormatUint(typed, 10), value, secretValues)
	case bool:
		return redactScalarStringWithValues(strconv.FormatBool(typed), value, secretValues)
	case nil:
		return redactScalarStringWithValues("null", value, secretValues)
	default:
		return value, false
	}
}

func redactScalarStringWithValues(value string, original any, secretValues []string) (any, bool) {
	redacted, changed := RedactStringWithValues(value, secretValues)
	if changed {
		return redacted, true
	}
	return original, false
}

func normalizedSecretValues(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool {
		return len(result[i]) > len(result[j])
	})
	return result
}
