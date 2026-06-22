package dashboards

import (
	"encoding/json"
	"testing"

	. "github.com/onsi/gomega"
)

func TestDefinitionsReturnStaticGrafanaDashboards(t *testing.T) {
	g := NewWithT(t)

	definitions := Definitions()

	g.Expect(definitions).To(HaveLen(3))
	g.Expect(definitions[0].ID).To(Equal("http-overview"))
	g.Expect(definitions[0].Version).NotTo(BeEmpty())
	g.Expect(json.Valid(definitions[0].Template)).To(BeTrue())
	g.Expect(definitions[1].ID).To(Equal("task-execution-overview"))
	g.Expect(json.Valid(definitions[1].Template)).To(BeTrue())
	g.Expect(definitions[2].ID).To(Equal("service-health-overview"))
	g.Expect(json.Valid(definitions[2].Template)).To(BeTrue())
}

func TestDefinitionsReturnImmutableCopies(t *testing.T) {
	g := NewWithT(t)

	definitions := Definitions()
	definitions[0].Template[0] = 'x'

	freshDefinitions := Definitions()
	g.Expect(json.Valid(freshDefinitions[0].Template)).To(BeTrue())
	g.Expect(freshDefinitions[0].ID).To(Equal("http-overview"))
}
