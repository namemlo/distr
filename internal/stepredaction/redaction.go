package stepredaction

import (
	"regexp"
	"sort"
	"strings"
)

const redactedValue = "[REDACTED]"

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(authorization\s*:\s*bearer\s+)[^\s,;]+`),
	regexp.MustCompile(`(?i)\b(password|passwd|secret|token|api[_-]?key)\b(\s*[:=]\s*)[^\s,;]+`),
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
			if secretKeyPattern.MatchString(key) {
				result[key] = redactedValue
				changed = true
				continue
			}
			redacted, childChanged := RedactValueWithValues(child, secretValues)
			result[key] = redacted
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
	default:
		return value, false
	}
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
