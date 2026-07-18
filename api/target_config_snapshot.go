package api

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/distr-sh/distr/internal/targetconfig"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

var targetConfigCursorPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

const targetConfigSnapshotDefaultPageLimit = 50

type CreateTargetConfigSnapshotRequest struct {
	DeploymentUnitID              uuid.UUID `json:"deploymentUnitId"`
	TargetEnvironmentAssignmentID uuid.UUID `json:"targetEnvironmentAssignmentId"`
	EnvironmentID                 uuid.UUID `json:"environmentId"`

	SourceRepository   string            `json:"sourceRepository"`
	SourceCommit       string            `json:"sourceCommit"`
	SourceAdapter      string            `json:"sourceAdapter"`
	AdapterVersion     string            `json:"adapterVersion"`
	TargetPlatform     string            `json:"targetPlatform"`
	RuntimeConstraints map[string]string `json:"runtimeConstraints"`

	Objects          []types.TargetConfigSnapshotObjectDraft          `json:"objects"`
	Components       []types.TargetConfigSnapshotComponentDraft       `json:"components"`
	SecretReferences []types.TargetConfigSnapshotSecretReferenceDraft `json:"secretReferences"`
	FeatureFlags     []types.TargetConfigSnapshotFeatureFlagDraft     `json:"featureFlags"`
}

func (request CreateTargetConfigSnapshotRequest) ToDraft(
	organizationID uuid.UUID,
) types.TargetConfigSnapshotDraft {
	return types.TargetConfigSnapshotDraft{
		OrganizationID:                organizationID,
		DeploymentUnitID:              request.DeploymentUnitID,
		TargetEnvironmentAssignmentID: request.TargetEnvironmentAssignmentID,
		EnvironmentID:                 request.EnvironmentID,
		SourceRepository:              request.SourceRepository,
		SourceCommit:                  request.SourceCommit,
		SourceAdapter:                 request.SourceAdapter,
		AdapterVersion:                request.AdapterVersion,
		TargetPlatform:                request.TargetPlatform,
		RuntimeConstraints:            request.RuntimeConstraints,
		Objects:                       request.Objects,
		Components:                    request.Components,
		SecretReferences:              request.SecretReferences,
		FeatureFlags:                  request.FeatureFlags,
	}
}

func (request CreateTargetConfigSnapshotRequest) Validate() error {
	issues := targetconfig.ValidateDraft(request.ToDraft(uuid.Nil))
	if len(issues) == 0 {
		return nil
	}
	return fmt.Errorf("%s: %s", issues[0].Field, issues[0].Message)
}

type TargetConfigSnapshotListRequest struct {
	DeploymentUnitID              *uuid.UUID `query:"deploymentUnitId"`
	TargetEnvironmentAssignmentID *uuid.UUID `query:"targetEnvironmentAssignmentId"`
	Cursor                        string     `query:"cursor"`
	Limit                         int        `query:"limit"`
}

func (request TargetConfigSnapshotListRequest) Validate() error {
	if request.DeploymentUnitID != nil && *request.DeploymentUnitID == uuid.Nil {
		return fmt.Errorf("deploymentUnitId is invalid")
	}
	if request.TargetEnvironmentAssignmentID != nil &&
		*request.TargetEnvironmentAssignmentID == uuid.Nil {
		return fmt.Errorf("targetEnvironmentAssignmentId is invalid")
	}
	if request.Limit < 1 || request.Limit > 100 {
		return fmt.Errorf("limit must be between 1 and 100")
	}
	if request.Cursor != "" {
		if len(request.Cursor) > 2048 || !targetConfigCursorPattern.MatchString(request.Cursor) {
			return fmt.Errorf("cursor is invalid")
		}
		if _, err := base64.RawURLEncoding.DecodeString(request.Cursor); err != nil {
			return fmt.Errorf("cursor is invalid")
		}
	}
	return nil
}

type TargetConfigSnapshot struct {
	ID                     uuid.UUID `json:"id"`
	CreatedAt              time.Time `json:"createdAt"`
	CreatedByUserAccountID uuid.UUID `json:"createdByUserAccountId"`

	DeploymentUnitID              uuid.UUID `json:"deploymentUnitId"`
	TargetEnvironmentAssignmentID uuid.UUID `json:"targetEnvironmentAssignmentId"`
	EnvironmentID                 uuid.UUID `json:"environmentId"`

	SourceRepository   string            `json:"sourceRepository"`
	SourceCommit       string            `json:"sourceCommit"`
	SourceAdapter      string            `json:"sourceAdapter"`
	AdapterVersion     string            `json:"adapterVersion"`
	TargetPlatform     string            `json:"targetPlatform"`
	RuntimeConstraints map[string]string `json:"runtimeConstraints"`
	CanonicalChecksum  string            `json:"canonicalChecksum"`

	Objects          []TargetConfigSnapshotObject          `json:"objects"`
	Components       []TargetConfigSnapshotComponent       `json:"components"`
	SecretReferences []TargetConfigSnapshotSecretReference `json:"secretReferences"`
	FeatureFlags     []TargetConfigSnapshotFeatureFlag     `json:"featureFlags"`
}

type TargetConfigSnapshotObject struct {
	Key       string                       `json:"key"`
	Kind      types.TargetConfigObjectKind `json:"kind"`
	Reference string                       `json:"reference"`
	VersionID string                       `json:"versionId,omitempty"`
	MediaType string                       `json:"mediaType"`
	SizeBytes int64                        `json:"sizeBytes"`
	Checksum  string                       `json:"checksum"`
}

type TargetConfigSnapshotComponent struct {
	PhysicalName        string    `json:"physicalName"`
	ComponentInstanceID uuid.UUID `json:"componentInstanceId"`
}

type TargetConfigSnapshotSecretReference struct {
	Key                string `json:"key"`
	Provider           string `json:"provider"`
	OpaqueReference    string `json:"opaqueReference"`
	VersionFingerprint string `json:"versionFingerprint"`
}

type TargetConfigSnapshotFeatureFlag struct {
	Key     string `json:"key"`
	Enabled bool   `json:"enabled"`
}

type TargetConfigSnapshotPage struct {
	Items      []TargetConfigSnapshot `json:"items"`
	NextCursor string                 `json:"nextCursor,omitempty"`
}

type TargetConfigObjectVerificationFact struct {
	Key               string `json:"key"`
	Verified          bool   `json:"verified"`
	Code              string `json:"code"`
	Message           string `json:"message"`
	ObservedVersionID string `json:"observedVersionId,omitempty"`
	ObservedMediaType string `json:"observedMediaType,omitempty"`
	ObservedSizeBytes *int64 `json:"observedSizeBytes,omitempty"`
	ObservedChecksum  string `json:"observedChecksum,omitempty"`
}

type TargetConfigObjectVerificationResult struct {
	SnapshotID uuid.UUID                            `json:"snapshotId"`
	Verified   bool                                 `json:"verified"`
	Objects    []TargetConfigObjectVerificationFact `json:"objects"`
}

func TargetConfigSnapshotListRequestFromQuery(
	rawDeploymentUnitID,
	rawAssignmentID,
	rawLimit,
	cursor string,
) (TargetConfigSnapshotListRequest, error) {
	request := TargetConfigSnapshotListRequest{
		Cursor: cursor,
		Limit:  targetConfigSnapshotDefaultPageLimit,
	}
	if rawDeploymentUnitID != "" {
		id, err := uuid.Parse(rawDeploymentUnitID)
		if err != nil {
			return request, fmt.Errorf("deploymentUnitId is invalid")
		}
		request.DeploymentUnitID = &id
	}
	if rawAssignmentID != "" {
		id, err := uuid.Parse(rawAssignmentID)
		if err != nil {
			return request, fmt.Errorf("targetEnvironmentAssignmentId is invalid")
		}
		request.TargetEnvironmentAssignmentID = &id
	}
	if rawLimit != "" {
		limit, err := strconv.Atoi(rawLimit)
		if err != nil {
			return request, fmt.Errorf("limit is invalid")
		}
		request.Limit = limit
	}
	return request, request.Validate()
}
