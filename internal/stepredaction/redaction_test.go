package stepredaction

import (
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
