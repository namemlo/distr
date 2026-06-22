package conditions

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/distr-sh/distr/internal/types"
)

type OutputReference struct {
	StepKey string
	Name    string
}

type OutputKey struct {
	StepKey string
	Name    string
}

type OutputValue struct {
	Value     any
	Sensitive bool
	Redacted  bool
}

type Context struct {
	Success                 bool
	Failure                 bool
	ChannelName             string
	EnvironmentIsProduction bool
	Variables               map[string]string
	Outputs                 map[OutputKey]OutputValue
}

type Result struct {
	Ready  bool
	Value  bool
	Reason string
}

type expressionKind int

const (
	expressionEmpty expressionKind = iota
	expressionAlways
	expressionSuccess
	expressionFailure
	expressionEnvironmentProduction
	expressionCompare
)

type leftOperandKind int

const (
	leftChannel leftOperandKind = iota
	leftVariable
	leftOutput
)

type expression struct {
	kind        expressionKind
	left        leftOperandKind
	operator    string
	literal     any
	variableKey string
	outputRef   OutputReference
}

var (
	functionPattern    = regexp.MustCompile(`^(always|success|failure)\(\)$`)
	environmentPattern = regexp.MustCompile(`^environment\.isProduction$`)
	channelPattern     = regexp.MustCompile(`^channel\s*(==|!=)\s*(.+)$`)
	variablePattern    = regexp.MustCompile(`^variable\("([^"]+)"\)\s*(==|!=)\s*(.+)$`)
	outputPattern      = regexp.MustCompile(`^output\("([^"]+)"\s*,\s*"([^"]+)"\)\s*(==|!=)\s*(.+)$`)
	bareLiteralPattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]+$`)
)

func Validate(condition string) error {
	_, err := parse(condition)
	return err
}

func OutputReferences(condition string) ([]OutputReference, error) {
	parsed, err := parse(condition)
	if err != nil {
		return nil, err
	}
	if parsed.kind != expressionCompare || parsed.left != leftOutput {
		return nil, nil
	}
	return []OutputReference{parsed.outputRef}, nil
}

func Evaluate(condition string, ctx Context) (Result, error) {
	parsed, err := parse(condition)
	if err != nil {
		return Result{}, err
	}
	switch parsed.kind {
	case expressionEmpty, expressionAlways:
		return Result{Ready: true, Value: true}, nil
	case expressionSuccess:
		return Result{Ready: true, Value: ctx.Success}, nil
	case expressionFailure:
		return Result{Ready: true, Value: ctx.Failure}, nil
	case expressionEnvironmentProduction:
		return Result{Ready: true, Value: ctx.EnvironmentIsProduction}, nil
	case expressionCompare:
		left, ready, reason := resolveLeft(parsed, ctx)
		if !ready {
			return Result{Ready: false, Value: false, Reason: reason}, nil
		}
		matched := compareValues(left, parsed.literal)
		if parsed.operator == "!=" {
			matched = !matched
		}
		return Result{Ready: true, Value: matched}, nil
	default:
		return Result{}, fmt.Errorf("unsupported condition")
	}
}

func parse(condition string) (expression, error) {
	condition = strings.TrimSpace(condition)
	if condition == "" {
		return expression{kind: expressionEmpty}, nil
	}
	if matches := functionPattern.FindStringSubmatch(condition); matches != nil {
		switch matches[1] {
		case "always":
			return expression{kind: expressionAlways}, nil
		case "success":
			return expression{kind: expressionSuccess}, nil
		case "failure":
			return expression{kind: expressionFailure}, nil
		}
	}
	if environmentPattern.MatchString(condition) {
		return expression{kind: expressionEnvironmentProduction}, nil
	}
	if matches := channelPattern.FindStringSubmatch(condition); matches != nil {
		literal, err := parseLiteral(matches[2])
		if err != nil {
			return expression{}, err
		}
		return expression{kind: expressionCompare, left: leftChannel, operator: matches[1], literal: literal}, nil
	}
	if matches := variablePattern.FindStringSubmatch(condition); matches != nil {
		key := strings.TrimSpace(matches[1])
		if key == "" {
			return expression{}, fmt.Errorf("variable key is required")
		}
		literal, err := parseLiteral(matches[3])
		if err != nil {
			return expression{}, err
		}
		return expression{
			kind:        expressionCompare,
			left:        leftVariable,
			operator:    matches[2],
			literal:     literal,
			variableKey: key,
		}, nil
	}
	if matches := outputPattern.FindStringSubmatch(condition); matches != nil {
		stepKey := strings.TrimSpace(matches[1])
		name := strings.TrimSpace(matches[2])
		if stepKey == "" || name == "" {
			return expression{}, fmt.Errorf("output step key and name are required")
		}
		if !types.IsValidStepRunOutputName(name) {
			return expression{}, fmt.Errorf("output name is invalid")
		}
		literal, err := parseLiteral(matches[4])
		if err != nil {
			return expression{}, err
		}
		return expression{
			kind:     expressionCompare,
			left:     leftOutput,
			operator: matches[3],
			literal:  literal,
			outputRef: OutputReference{
				StepKey: stepKey,
				Name:    name,
			},
		}, nil
	}
	return expression{}, fmt.Errorf("condition is not in the restricted expression language")
}

func parseLiteral(value string) (any, error) {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		var decoded string
		if err := json.Unmarshal([]byte(value), &decoded); err != nil {
			return nil, fmt.Errorf("condition literal is invalid: %w", err)
		}
		return decoded, nil
	}
	if value == "true" {
		return true, nil
	}
	if value == "false" {
		return false, nil
	}
	if number, err := strconv.ParseFloat(value, 64); err == nil && !math.IsInf(number, 0) && !math.IsNaN(number) {
		return number, nil
	}
	if bareLiteralPattern.MatchString(value) {
		return value, nil
	}
	return nil, fmt.Errorf("condition literal is invalid")
}

func resolveLeft(parsed expression, ctx Context) (any, bool, string) {
	switch parsed.left {
	case leftChannel:
		return ctx.ChannelName, true, ""
	case leftVariable:
		value, ok := ctx.Variables[parsed.variableKey]
		if !ok {
			return nil, false, "variable is unavailable"
		}
		return value, true, ""
	case leftOutput:
		output, ok := ctx.Outputs[OutputKey(parsed.outputRef)]
		if !ok {
			return nil, false, "output is unavailable"
		}
		if output.Sensitive || output.Redacted {
			return nil, false, "output is sensitive"
		}
		return output.Value, true, ""
	default:
		return nil, false, "left operand is unavailable"
	}
}

func compareValues(left any, right any) bool {
	left = normalizeValue(left)
	right = normalizeValue(right)
	leftNumber, leftNumberOK := asFloat64(left)
	rightNumber, rightNumberOK := asFloat64(right)
	if leftNumberOK && rightNumberOK {
		return leftNumber == rightNumber
	}
	return fmt.Sprint(left) == fmt.Sprint(right)
}

func normalizeValue(value any) any {
	switch typed := value.(type) {
	case json.RawMessage:
		var decoded any
		if err := json.Unmarshal(typed, &decoded); err == nil {
			return decoded
		}
	case []byte:
		var decoded any
		if err := json.Unmarshal(typed, &decoded); err == nil {
			return decoded
		}
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case float32:
		return float64(typed)
	}
	return value
}

func asFloat64(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case json.Number:
		value, err := typed.Float64()
		return value, err == nil
	default:
		return 0, false
	}
}
