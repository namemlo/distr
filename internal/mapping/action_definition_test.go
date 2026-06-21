package mapping

import (
	"testing"

	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

func TestActionDefinitionToAPI(t *testing.T) {
	g := NewWithT(t)
	action := types.ActionDefinition{
		Type:        "distr.wait",
		Name:        "Wait",
		Description: "Waits for a duration.",
		InputSchema: map[string]any{
			"type": "object",
		},
		OutputSchema: map[string]any{
			"type": "object",
		},
	}

	result := ActionDefinitionToAPI(action)

	g.Expect(result.Type).To(Equal("distr.wait"))
	g.Expect(result.Name).To(Equal("Wait"))
	g.Expect(result.Description).To(Equal("Waits for a duration."))
	g.Expect(result.InputSchema).To(HaveKeyWithValue("type", "object"))
	g.Expect(result.OutputSchema).To(HaveKeyWithValue("type", "object"))
}
