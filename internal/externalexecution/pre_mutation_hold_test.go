package externalexecution

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	expiresAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)

	control, err := ParsePreMutationHold([]byte(`{
		"schema":"distr.external-execution-pre-mutation-hold/v1",
		"controlId":"` + controlID.String() + `",
		"organizationId":"` + organizationID.String() + `",
		"deploymentPlanId":"` + planID.String() + `",
		"deploymentTargetId":"` + targetID.String() + `",
		"planChecksum":"` + planChecksum + `",
		"component":" transaction.api ",
		"reason":" pilot dependency-block demonstration ",
		"expiresAt":"` + expiresAt.Format(time.RFC3339) + `"
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
		`"expiresAt":"` + expiresAt.Format(time.RFC3339) + `",` +
		`"schema":"distr.external-execution-pre-mutation-hold/v1"}`))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(reordered.ControlChecksum).To(Equal(control.ControlChecksum))
}

func TestWaitForPreMutationHoldRequiresExactReleaseFailBinding(t *testing.T) {
	g := NewWithT(t)
	control := types.ExternalExecutionPreMutationHold{
		Schema: types.ExternalExecutionPreMutationHoldSchemaV1, ControlID: uuid.New(),
		OrganizationID: uuid.New(), DeploymentPlanID: uuid.New(), DeploymentTargetID: uuid.New(),
		PlanChecksum: "sha256:" + strings.Repeat("a", 64), Component: "transaction-api",
		Reason: "operator observes dependency conflict", ExpiresAt: time.Now().UTC().Add(time.Second),
	}
	var err error
	control.ControlChecksum, err = PreMutationHoldChecksum(control)
	g.Expect(err).NotTo(HaveOccurred())
	release := types.ExternalExecutionPreMutationHoldRelease{
		Schema: types.ExternalExecutionPreMutationHoldReleaseSchemaV1,
		Action: string(types.ExternalExecutionPreMutationHoldReleaseFail), ControlID: control.ControlID,
		ControlChecksum: control.ControlChecksum, OrganizationID: control.OrganizationID,
		DeploymentPlanID: control.DeploymentPlanID, DeploymentTargetID: control.DeploymentTargetID,
		PlanChecksum: control.PlanChecksum, Component: control.Component,
	}
	path := filepath.Join(t.TempDir(), "release.json")
	wrong := release
	wrong.DeploymentTargetID = uuid.New()
	value, err := json.Marshal(wrong)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(os.WriteFile(path, value, 0o600)).To(Succeed())

	result := make(chan types.ExternalExecutionPreMutationHoldResolution, 1)
	go func() {
		resolution, _ := WaitForPreMutationHoldRelease(context.Background(), control, path, time.Millisecond)
		result <- resolution
	}()
	select {
	case resolution := <-result:
		t.Fatalf("mismatched release unexpectedly resolved hold as %s", resolution)
	case <-time.After(20 * time.Millisecond):
	}
	value, err = json.Marshal(release)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(os.WriteFile(path, value, 0o600)).To(Succeed())
	select {
	case resolution := <-result:
		g.Expect(resolution).To(Equal(types.ExternalExecutionPreMutationHoldReleaseFail))
	case <-time.After(time.Second):
		t.Fatal("exact release did not resolve hold")
	}
}

func TestWaitForPreMutationHoldAutomaticallyTimesOut(t *testing.T) {
	g := NewWithT(t)
	control := types.ExternalExecutionPreMutationHold{
		Schema: types.ExternalExecutionPreMutationHoldSchemaV1, ControlID: uuid.New(),
		OrganizationID: uuid.New(), DeploymentPlanID: uuid.New(), DeploymentTargetID: uuid.New(),
		PlanChecksum: "sha256:" + strings.Repeat("a", 64), Component: "customer-api",
		Reason: "bounded hold", ExpiresAt: time.Now().UTC().Add(10 * time.Millisecond),
	}
	var err error
	control.ControlChecksum, err = PreMutationHoldChecksum(control)
	g.Expect(err).NotTo(HaveOccurred())
	resolution, err := WaitForPreMutationHoldRelease(
		context.Background(), control, filepath.Join(t.TempDir(), "missing.json"), time.Millisecond,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(resolution).To(Equal(types.ExternalExecutionPreMutationHoldTimedOut))
}

func TestParsePreMutationHoldRejectsIncompleteOrUnknownConfiguration(t *testing.T) {
	g := NewWithT(t)

	_, err := ParsePreMutationHold([]byte(`{"schema":"wrong","unknown":true}`))

	g.Expect(err).To(MatchError(ContainSubstring("unknown field")))
}
