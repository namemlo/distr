package conditions

import (
	"testing"

	. "github.com/onsi/gomega"
)

func TestValidateAcceptsRestrictedConditionLanguage(t *testing.T) {
	tests := []string{
		"",
		"always()",
		"success()",
		"failure()",
		"environment.isProduction",
		`channel == "Stable"`,
		`channel != stable`,
		`variable("Feature.Enabled") == "true"`,
		`output("prepare", "statusCode") == 200`,
	}

	for _, condition := range tests {
		t.Run(condition, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(Validate(condition)).To(Succeed())
		})
	}
}

func TestValidateRejectsGeneralPurposeExpressions(t *testing.T) {
	tests := []string{
		`channel =~ "Stable"`,
		`variable(Feature.Enabled) == "true"`,
		`output("prepare") == "ok"`,
		`output("prepare", "status") > 200`,
		`output("prepare", "status code") == "ok"`,
		`system("rm -rf /")`,
		`channel == "Stable" || always()`,
	}

	for _, condition := range tests {
		t.Run(condition, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(Validate(condition)).NotTo(Succeed())
		})
	}
}

func TestOutputReferencesReturnsReferencedStepOutputs(t *testing.T) {
	g := NewWithT(t)

	refs, err := OutputReferences(`output("prepare", "statusCode") == 200`)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(refs).To(Equal([]OutputReference{{StepKey: "prepare", Name: "statusCode"}}))
}

func TestEvaluateRestrictedConditions(t *testing.T) {
	ctx := Context{
		Success:                 true,
		ChannelName:             "Stable",
		EnvironmentIsProduction: true,
		Variables: map[string]string{
			"Feature.Enabled": "true",
		},
		Outputs: map[OutputKey]OutputValue{
			{StepKey: "prepare", Name: "statusCode"}: {Value: float64(200)},
			{StepKey: "prepare", Name: "message"}:    {Value: "ok"},
		},
	}

	tests := []struct {
		name      string
		condition string
		want      bool
	}{
		{name: "empty", condition: "", want: true},
		{name: "always", condition: "always()", want: true},
		{name: "success", condition: "success()", want: true},
		{name: "channel", condition: `channel == "Stable"`, want: true},
		{name: "production", condition: "environment.isProduction", want: true},
		{name: "variable", condition: `variable("Feature.Enabled") == "true"`, want: true},
		{name: "numeric output", condition: `output("prepare", "statusCode") == 200`, want: true},
		{name: "string output", condition: `output("prepare", "message") != "failed"`, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			result, err := Evaluate(tt.condition, ctx)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(result.Ready).To(BeTrue())
			g.Expect(result.Value).To(Equal(tt.want))
		})
	}
}

func TestEvaluateDoesNotExposeSensitiveOrMissingOutputs(t *testing.T) {
	g := NewWithT(t)
	ctx := Context{
		Outputs: map[OutputKey]OutputValue{
			{StepKey: "prepare", Name: "secret"}: {Value: "token", Sensitive: true},
		},
	}

	sensitive, err := Evaluate(`output("prepare", "secret") == "token"`, ctx)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(sensitive.Ready).To(BeFalse())
	g.Expect(sensitive.Value).To(BeFalse())

	missing, err := Evaluate(`output("prepare", "missing") == "token"`, ctx)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(missing.Ready).To(BeFalse())
	g.Expect(missing.Value).To(BeFalse())
}
