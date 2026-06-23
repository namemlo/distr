package deploymentcompat

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

const syntheticReleaseNamespace = "distr:legacy-direct-deployment"

type ProjectionContext struct {
	OrganizationID         uuid.UUID
	ApplicationID          uuid.UUID
	ApplicationName        string
	ApplicationVersionName string
}

type canonicalLegacyProjection struct {
	Source                     types.DeploymentCompatibilitySource `json:"source"`
	OrganizationID             string                              `json:"organizationId"`
	LegacyDeploymentID         string                              `json:"legacyDeploymentId"`
	LegacyDeploymentRevisionID string                              `json:"legacyDeploymentRevisionId"`
	DeploymentTargetID         string                              `json:"deploymentTargetId"`
	ApplicationID              string                              `json:"applicationId"`
	ApplicationVersionID       string                              `json:"applicationVersionId"`
	ValuesHash                 string                              `json:"valuesHash,omitempty"`
}

func ProjectLegacyDeployment(
	deployment types.Deployment,
	revision types.DeploymentRevision,
	context ProjectionContext,
) (types.LegacyDeploymentProjection, error) {
	if context.OrganizationID == uuid.Nil {
		return types.LegacyDeploymentProjection{}, errors.New("organization id is required")
	}
	if deployment.ID == uuid.Nil {
		return types.LegacyDeploymentProjection{}, errors.New("legacy deployment id is required")
	}
	if revision.ID == uuid.Nil {
		return types.LegacyDeploymentProjection{}, errors.New("legacy deployment revision id is required")
	}
	if deployment.DeploymentTargetID == uuid.Nil {
		return types.LegacyDeploymentProjection{}, errors.New("deployment target id is required")
	}
	if context.ApplicationID == uuid.Nil {
		return types.LegacyDeploymentProjection{}, errors.New("application id is required")
	}
	if revision.ApplicationVersionID == uuid.Nil {
		return types.LegacyDeploymentProjection{}, errors.New("application version id is required")
	}
	if revision.DeploymentID != deployment.ID {
		return types.LegacyDeploymentProjection{}, errors.New("revision does not belong to deployment")
	}

	syntheticReleaseID := syntheticReleaseID(context.OrganizationID, revision.ID)
	canonical := canonicalLegacyProjection{
		Source:                     types.DeploymentCompatibilitySourceLegacyDirectDeployment,
		OrganizationID:             context.OrganizationID.String(),
		LegacyDeploymentID:         deployment.ID.String(),
		LegacyDeploymentRevisionID: revision.ID.String(),
		DeploymentTargetID:         deployment.DeploymentTargetID.String(),
		ApplicationID:              context.ApplicationID.String(),
		ApplicationVersionID:       revision.ApplicationVersionID.String(),
		ValuesHash:                 hex.EncodeToString(revision.ValuesHash),
	}
	payload, err := json.Marshal(canonical)
	if err != nil {
		return types.LegacyDeploymentProjection{}, fmt.Errorf("marshal canonical legacy projection: %w", err)
	}
	sum := sha256.Sum256(payload)

	return types.LegacyDeploymentProjection{
		OrganizationID:             context.OrganizationID,
		LegacyDeploymentID:         deployment.ID,
		LegacyDeploymentRevisionID: revision.ID,
		DeploymentTargetID:         deployment.DeploymentTargetID,
		ApplicationID:              context.ApplicationID,
		ApplicationVersionID:       revision.ApplicationVersionID,
		ApplicationName:            context.ApplicationName,
		ApplicationVersionName:     context.ApplicationVersionName,
		SyntheticReleaseID:         syntheticReleaseID,
		CanonicalChecksum:          "sha256:" + hex.EncodeToString(sum[:]),
		CanonicalPayload:           append([]byte(nil), payload...),
		Components: []types.DeploymentTimelineComponent{
			{
				Key:     "application",
				Name:    context.ApplicationName,
				Type:    types.ReleaseBundleComponentTypeApplicationVersion,
				Version: context.ApplicationVersionName,
			},
		},
		Source:       types.DeploymentCompatibilitySourceLegacyDirectDeployment,
		Availability: types.DeploymentCompatibilityAvailability{},
	}, nil
}

func syntheticReleaseID(orgID, revisionID uuid.UUID) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(
		syntheticReleaseNamespace+":"+orgID.String()+":"+revisionID.String(),
	))
}
