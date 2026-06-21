package stepredaction

import (
	"regexp"
	"strings"
)

const redactedValue = "[REDACTED]"

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(authorization\s*:\s*bearer\s+)[^\s,;]+`),
	regexp.MustCompile(`(?i)\b(password|passwd|secret|token|api[_-]?key)\b(\s*[:=]\s*)[^\s,;]+`),
}

var secretKeyPattern = regexp.MustCompile(`(?i)(password|passwd|secret|token|api[_-]?key|authorization)`)

func RedactString(value string) (string, bool) {
	result := strings.ReplaceAll(value, "\x00", "")
	changed := result != value
	for _, pattern := range secretPatterns {
		next := pattern.ReplaceAllString(result, "${1}${2}"+redactedValue)
		if next != result {
			changed = true
			result = next
		}
	}
	return result, changed
}

func RedactValue(value any) (any, bool) {
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
			redacted, childChanged := RedactValue(child)
			result[key] = redacted
			changed = changed || childChanged
		}
		return result, changed
	case []any:
		changed := false
		result := make([]any, len(typed))
		for i, child := range typed {
			redacted, childChanged := RedactValue(child)
			result[i] = redacted
			changed = changed || childChanged
		}
		return result, changed
	case string:
		return RedactString(typed)
	default:
		return value, false
	}
}
