package externalexecution

import (
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestParsePreMutationHoldNormalizesAndChecksumsExactBinding(t *testing.T) {
	g := NewWithT(t)
	controlID := uuid.New()
	organizationID := uuid.New()
	planID := uuid.New()
	targetID := uuid.New()
	planChecksum := "sha256:" + strings.Repeat("a", 64)

	control, err := ParsePreMutationHold([]byte(`{
		"schema":"distr.external-execution-pre-mutation-hold/v1",
		"controlId":"` + controlID.String() + `",
		"organizationId":"` + organizationID.String() + `",
		"deploymentPlanId":"` + planID.String() + `",
		"deploymentTargetId":"` + targetID.String() + `",
		"planChecksum":"` + planChecksum + `",
		"component":" transaction.api ",
		"reason":" pilot dependency-block demonstration "
	}`))

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(control.Component).To(Equal("transaction.api"))
	g.Expect(control.Reason).To(Equal("pilot dependency-block demonstration"))
	g.Expect(control.ControlChecksum).To(MatchRegexp(`^sha256:[0-9a-f]{64}$`))
	g.Expect(MatchesPreMutationHold(*control, types.ExternalExecution{
		OrganizationID: organizationID, DeploymentPlanID: planID,
		DeploymentTargetID: targetID, PlanChecksum: planChecksum, Component: "transaction.api",
	})).To(BeTrue())
	g.Expect(MatchesPreMutationHold(*control, types.ExternalExecution{
		OrganizationID: organizationID, DeploymentPlanID: planID,
		DeploymentTargetID: targetID, PlanChecksum: planChecksum, Component: "customer.api",
	})).To(BeFalse())

	changed := *control
	changed.DeploymentTargetID = uuid.New()
	changed.ControlChecksum = ""
	changedChecksum, err := PreMutationHoldChecksum(changed)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(changedChecksum).NotTo(Equal(control.ControlChecksum))

	reordered, err := ParsePreMutationHold([]byte(`{"reason":"pilot dependency-block demonstration",` +
		`"component":"transaction.api","planChecksum":"` + planChecksum + `",` +
		`"deploymentTargetId":"` + targetID.String() + `","deploymentPlanId":"` + planID.String() + `",` +
		`"organizationId":"` + organizationID.String() + `","controlId":"` + controlID.String() + `",` +
		`"schema":"distr.external-execution-pre-mutation-hold/v1"}`))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(reordered.ControlChecksum).To(Equal(control.ControlChecksum))
}

func TestParsePreMutationHoldRejectsIncompleteOrUnknownConfiguration(t *testing.T) {
	g := NewWithT(t)

	_, err := ParsePreMutationHold([]byte(`{"schema":"wrong","unknown":true}`))

	g.Expect(err).To(MatchError(ContainSubstring("unknown field")))
}
