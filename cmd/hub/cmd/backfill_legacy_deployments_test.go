package cmd

import (
	"bytes"
	"context"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestBackfillLegacyDeploymentsCommandDefaultsToDryRun(t *testing.T) {
	g := NewWithT(t)
	orgID := uuid.New()
	var got types.DeploymentCompatibilityBackfillRequest
	stdout, _, err := executeBackfillLegacyDeploymentsCommandForTest(t, backfillLegacyDeploymentsRuntime{
		Run: func(_ context.Context, request types.DeploymentCompatibilityBackfillRequest) (*types.DeploymentCompatibilityBackfillReport, error) {
			got = request
			return &types.DeploymentCompatibilityBackfillReport{Scanned: 1, Eligible: 1, Projected: 1}, nil
		},
	}, "--organization-id", orgID.String())

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(got.OrganizationID).To(Equal(orgID))
	g.Expect(got.Apply).To(BeFalse())
	g.Expect(got.BatchSize).To(Equal(500))
	g.Expect(stdout).To(ContainSubstring("dryRun=true"))
	g.Expect(stdout).To(ContainSubstring("projected=1"))
}

func TestBackfillLegacyDeploymentsCommandRequiresExplicitApply(t *testing.T) {
	g := NewWithT(t)
	orgID := uuid.New()
	var got types.DeploymentCompatibilityBackfillRequest
	stdout, _, err := executeBackfillLegacyDeploymentsCommandForTest(t, backfillLegacyDeploymentsRuntime{
		Run: func(_ context.Context, request types.DeploymentCompatibilityBackfillRequest) (*types.DeploymentCompatibilityBackfillReport, error) {
			got = request
			return &types.DeploymentCompatibilityBackfillReport{Scanned: 2, Projected: 2}, nil
		},
	}, "--organization-id", orgID.String(), "--apply", "--batch-size", "25")

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(got.Apply).To(BeTrue())
	g.Expect(got.BatchSize).To(Equal(25))
	g.Expect(stdout).To(ContainSubstring("dryRun=false"))
}

func TestBackfillLegacyDeploymentsCommandRequiresOrganizationID(t *testing.T) {
	g := NewWithT(t)

	_, _, err := executeBackfillLegacyDeploymentsCommandForTest(t, backfillLegacyDeploymentsRuntime{
		Run: func(context.Context, types.DeploymentCompatibilityBackfillRequest) (*types.DeploymentCompatibilityBackfillReport, error) {
			t.Fatal("run should not be called without organization id")
			return nil, nil
		},
	})

	g.Expect(err).To(HaveOccurred())
}

func executeBackfillLegacyDeploymentsCommandForTest(
	t *testing.T,
	runtime backfillLegacyDeploymentsRuntime,
	args ...string,
) (string, string, error) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if runtime.Stdout == nil {
		runtime.Stdout = &stdout
	}
	if runtime.Stderr == nil {
		runtime.Stderr = &stderr
	}
	cmd := newBackfillLegacyDeploymentsCommand(runtime)
	cmd.SetArgs(args)
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}
