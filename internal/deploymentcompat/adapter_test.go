package deploymentcompat_test

import (
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/deploymentcompat"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestProjectLegacyDeploymentBuildsDeterministicSingleComponentProjection(t *testing.T) {
	g := NewWithT(t)
	orgID := uuid.New()
	deploymentID := uuid.New()
	revisionID := uuid.New()
	targetID := uuid.New()
	applicationID := uuid.New()
	versionID := uuid.New()

	projection, err := deploymentcompat.ProjectLegacyDeployment(
		types.Deployment{
			Base: types.Base{
				ID:        deploymentID,
				CreatedAt: time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
			},
			DeploymentTargetID: targetID,
		},
		types.DeploymentRevision{
			Base: types.Base{
				ID:        revisionID,
				CreatedAt: time.Date(2026, 6, 2, 11, 0, 0, 0, time.UTC),
			},
			DeploymentID:         deploymentID,
			ApplicationVersionID: versionID,
			ValuesHash:           []byte("stored-values-hash"),
		},
		deploymentcompat.ProjectionContext{
			OrganizationID:         orgID,
			ApplicationID:          applicationID,
			ApplicationName:        "Billing API",
			ApplicationVersionName: "2.4.1",
		},
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(projection.OrganizationID).To(Equal(orgID))
	g.Expect(projection.LegacyDeploymentID).To(Equal(deploymentID))
	g.Expect(projection.LegacyDeploymentRevisionID).To(Equal(revisionID))
	g.Expect(projection.DeploymentTargetID).To(Equal(targetID))
	g.Expect(projection.ApplicationID).To(Equal(applicationID))
	g.Expect(projection.ApplicationVersionID).To(Equal(versionID))
	g.Expect(projection.ApplicationName).To(Equal("Billing API"))
	g.Expect(projection.ApplicationVersionName).To(Equal("2.4.1"))
	g.Expect(projection.Source).To(Equal(types.DeploymentCompatibilitySourceLegacyDirectDeployment))
	g.Expect(projection.SyntheticReleaseID).NotTo(Equal(uuid.Nil))
	g.Expect(projection.CanonicalChecksum).To(HavePrefix("sha256:"))
	g.Expect(projection.Components).To(Equal([]types.DeploymentTimelineComponent{
		{
			Key:     "application",
			Name:    "Billing API",
			Type:    types.ReleaseBundleComponentTypeApplicationVersion,
			Version: "2.4.1",
		},
	}))
	g.Expect(projection.Availability.ProcessSnapshot).To(BeFalse())
	g.Expect(projection.Availability.VariableSnapshot).To(BeFalse())
	g.Expect(projection.Availability.Channel).To(BeFalse())
	g.Expect(projection.Availability.Environment).To(BeFalse())
	g.Expect(projection.Availability.TaskLogs).To(BeFalse())
	g.Expect(projection.Availability.RedeployPlan).To(BeFalse())
}

func TestProjectLegacyDeploymentDoesNotUseMutableOrSensitiveValuesForIdentity(t *testing.T) {
	g := NewWithT(t)
	orgID := uuid.New()
	deploymentID := uuid.New()
	revisionID := uuid.New()
	targetID := uuid.New()
	applicationID := uuid.New()
	versionID := uuid.New()

	baseDeployment := types.Deployment{
		Base:               types.Base{ID: deploymentID},
		DeploymentTargetID: targetID,
	}
	baseRevision := types.DeploymentRevision{
		Base:                 types.Base{ID: revisionID},
		DeploymentID:         deploymentID,
		ApplicationVersionID: versionID,
		ValuesHash:           []byte("stable-stored-hash"),
		ValuesYaml:           []byte("password: old"),
		EnvFileData:          []byte("TOKEN=old"),
	}

	first, err := deploymentcompat.ProjectLegacyDeployment(baseDeployment, baseRevision, deploymentcompat.ProjectionContext{
		OrganizationID:         orgID,
		ApplicationID:          applicationID,
		ApplicationName:        "Billing API",
		ApplicationVersionName: "2.4.1",
	})
	g.Expect(err).NotTo(HaveOccurred())

	mutatedRevision := baseRevision
	mutatedRevision.CreatedAt = time.Now().Add(24 * time.Hour)
	mutatedRevision.ValuesYaml = []byte("password: new")
	mutatedRevision.EnvFileData = []byte("TOKEN=new")
	second, err := deploymentcompat.ProjectLegacyDeployment(baseDeployment, mutatedRevision, deploymentcompat.ProjectionContext{
		OrganizationID:         orgID,
		ApplicationID:          applicationID,
		ApplicationName:        "Billing API renamed",
		ApplicationVersionName: "2.4.1-hotfix",
	})
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(second.SyntheticReleaseID).To(Equal(first.SyntheticReleaseID))
	g.Expect(second.CanonicalChecksum).To(Equal(first.CanonicalChecksum))
	g.Expect(string(second.CanonicalPayload)).NotTo(ContainSubstring("password"))
	g.Expect(string(second.CanonicalPayload)).NotTo(ContainSubstring("TOKEN"))
	g.Expect(string(second.CanonicalPayload)).NotTo(ContainSubstring("Billing API renamed"))
	g.Expect(string(second.CanonicalPayload)).NotTo(ContainSubstring("2.4.1-hotfix"))
}
