package stepredaction

import (
	"encoding/json"
	"testing"

	. "github.com/onsi/gomega"
)

func TestRedactStringRemovesCommonSecretShapes(t *testing.T) {
	g := NewWithT(t)

	redacted, changed := RedactString("Authorization: Bearer abc123 password=secret token:top-secret api_key=value")

	g.Expect(changed).To(BeTrue())
	g.Expect(redacted).To(ContainSubstring("Authorization: Bearer [REDACTED]"))
	g.Expect(redacted).To(ContainSubstring("password=[REDACTED]"))
	g.Expect(redacted).To(ContainSubstring("token:[REDACTED]"))
	g.Expect(redacted).To(ContainSubstring("api_key=[REDACTED]"))
	g.Expect(redacted).NotTo(ContainSubstring("abc123"))
	g.Expect(redacted).NotTo(ContainSubstring("top-secret"))
}

func TestRedactStringRemovesWebhookSignatures(t *testing.T) {
	g := NewWithT(t)
	const signature = "sha256=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	redacted, changed := RedactString("webhook signature " + signature)

	g.Expect(changed).To(BeTrue())
	g.Expect(redacted).To(Equal("webhook signature [REDACTED]"))
	g.Expect(redacted).NotTo(ContainSubstring(signature))
}

func TestRedactValueWithValuesRedactsNumericSecret(t *testing.T) {
	g := NewWithT(t)

	value, changed := RedactValueWithValues(json.Number("123"), []string{"123"})

	g.Expect(changed).To(BeTrue())
	g.Expect(value).To(Equal("[REDACTED]"))
}

func TestRedactValueWithValuesRedactsFloatNumericSecret(t *testing.T) {
	g := NewWithT(t)

	value, changed := RedactValueWithValues(float64(123), []string{"123"})

	g.Expect(changed).To(BeTrue())
	g.Expect(value).To(Equal("[REDACTED]"))
}

func TestRedactValueWithValuesPreservesNonSecretNumericValue(t *testing.T) {
	g := NewWithT(t)

	value, changed := RedactValueWithValues(float64(456), []string{"123"})

	g.Expect(changed).To(BeFalse())
	g.Expect(value).To(Equal(float64(456)))
}

func TestRedactValueWithValuesRedactsBooleanSecret(t *testing.T) {
	g := NewWithT(t)

	value, changed := RedactValueWithValues(true, []string{"true"})

	g.Expect(changed).To(BeTrue())
	g.Expect(value).To(Equal("[REDACTED]"))
}

func TestRedactValueWithValuesRedactsNullSecret(t *testing.T) {
	g := NewWithT(t)

	value, changed := RedactValueWithValues(nil, []string{"null"})

	g.Expect(changed).To(BeTrue())
	g.Expect(value).To(Equal("[REDACTED]"))
}

func TestRedactValueWithValuesRedactsSecretObjectKey(t *testing.T) {
	g := NewWithT(t)
	const secret = "webhook-value-123"

	value, changed := RedactValueWithValues(map[string]any{
		secret: "value",
	}, []string{secret})

	g.Expect(changed).To(BeTrue())
	redacted := value.(map[string]any)
	g.Expect(redacted).NotTo(HaveKey(secret))
	g.Expect(redacted).To(HaveKeyWithValue("[REDACTED]", "value"))
}

func TestRedactValueRedactsSecretKeysAndNestedStrings(t *testing.T) {
	g := NewWithT(t)

	value, changed := RedactValue(map[string]any{
		"message": "token=secret",
		"nested": map[string]any{
			"password": "plaintext",
		},
		"list": []any{"Authorization: Bearer bearer-token"},
	})

	g.Expect(changed).To(BeTrue())
	redacted := value.(map[string]any)
	g.Expect(redacted["message"]).To(Equal("token=[REDACTED]"))
	g.Expect(redacted["nested"].(map[string]any)["password"]).To(Equal("[REDACTED]"))
	g.Expect(redacted["list"].([]any)[0]).To(Equal("Authorization: Bearer [REDACTED]"))
}
