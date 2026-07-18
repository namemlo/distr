package targetconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/distr-sh/distr/internal/types"
)

type canonicalTargetConfigSnapshot struct {
	Schema                        string                                           `json:"schema"`
	OrganizationID                string                                           `json:"organizationId"`
	DeploymentUnitID              string                                           `json:"deploymentUnitId"`
	TargetEnvironmentAssignmentID string                                           `json:"targetEnvironmentAssignmentId"`
	EnvironmentID                 string                                           `json:"environmentId"`
	SourceRepository              string                                           `json:"sourceRepository"`
	SourceCommit                  string                                           `json:"sourceCommit"`
	SourceAdapter                 string                                           `json:"sourceAdapter"`
	AdapterVersion                string                                           `json:"adapterVersion"`
	TargetPlatform                string                                           `json:"targetPlatform"`
	RuntimeConstraints            map[string]string                                `json:"runtimeConstraints"`
	Objects                       []types.TargetConfigSnapshotObjectDraft          `json:"objects"`
	Components                    []types.TargetConfigSnapshotComponentDraft       `json:"components"`
	SecretReferences              []types.TargetConfigSnapshotSecretReferenceDraft `json:"secretReferences"`
	FeatureFlags                  []types.TargetConfigSnapshotFeatureFlagDraft     `json:"featureFlags"`
}

func Canonicalize(draft types.TargetConfigSnapshotDraft) ([]byte, string, error) {
	if duplicate := duplicateObjectKey(draft.Objects); duplicate != "" {
		return nil, "", fmt.Errorf("duplicate object key %q", duplicate)
	}
	if duplicate := duplicateComponentKey(draft.Components); duplicate != "" {
		return nil, "", fmt.Errorf("duplicate component key %q", duplicate)
	}
	if duplicate := duplicateSecretReferenceKey(draft.SecretReferences); duplicate != "" {
		return nil, "", fmt.Errorf("duplicate secret reference key %q", duplicate)
	}
	if duplicate := duplicateFeatureFlagKey(draft.FeatureFlags); duplicate != "" {
		return nil, "", fmt.Errorf("duplicate feature flag key %q", duplicate)
	}

	objects := cloneCanonicalCollection(draft.Objects)
	components := cloneCanonicalCollection(draft.Components)
	secretReferences := cloneCanonicalCollection(draft.SecretReferences)
	featureFlags := cloneCanonicalCollection(draft.FeatureFlags)
	for index := range objects {
		objects[index].Key = strings.TrimSpace(objects[index].Key)
		objects[index].Reference = strings.TrimSpace(objects[index].Reference)
		objects[index].VersionID = strings.TrimSpace(objects[index].VersionID)
		objects[index].MediaType = strings.TrimSpace(objects[index].MediaType)
		objects[index].Checksum = strings.TrimSpace(objects[index].Checksum)
	}
	for index := range components {
		components[index].PhysicalName = strings.TrimSpace(components[index].PhysicalName)
	}
	for index := range secretReferences {
		secretReferences[index].Key = strings.TrimSpace(secretReferences[index].Key)
		secretReferences[index].Provider = strings.TrimSpace(secretReferences[index].Provider)
		secretReferences[index].Reference = strings.TrimSpace(secretReferences[index].Reference)
		secretReferences[index].VersionFingerprint = strings.TrimSpace(
			secretReferences[index].VersionFingerprint,
		)
	}
	for index := range featureFlags {
		featureFlags[index].Key = strings.TrimSpace(featureFlags[index].Key)
	}
	slices.SortFunc(objects, func(a, b types.TargetConfigSnapshotObjectDraft) int {
		return strings.Compare(a.Key, b.Key)
	})
	slices.SortFunc(components, func(a, b types.TargetConfigSnapshotComponentDraft) int {
		return strings.Compare(a.PhysicalName, b.PhysicalName)
	})
	slices.SortFunc(secretReferences, func(a, b types.TargetConfigSnapshotSecretReferenceDraft) int {
		return strings.Compare(a.Key, b.Key)
	})
	slices.SortFunc(featureFlags, func(a, b types.TargetConfigSnapshotFeatureFlagDraft) int {
		return strings.Compare(a.Key, b.Key)
	})

	runtimeConstraints := make(map[string]string, len(draft.RuntimeConstraints))
	for key, value := range draft.RuntimeConstraints {
		runtimeConstraints[key] = value
	}
	payload, err := json.Marshal(canonicalTargetConfigSnapshot{
		Schema:                        types.TargetConfigSnapshotSchema,
		OrganizationID:                draft.OrganizationID.String(),
		DeploymentUnitID:              draft.DeploymentUnitID.String(),
		TargetEnvironmentAssignmentID: draft.TargetEnvironmentAssignmentID.String(),
		EnvironmentID:                 draft.EnvironmentID.String(),
		SourceRepository:              strings.TrimSpace(draft.SourceRepository),
		SourceCommit:                  strings.TrimSpace(draft.SourceCommit),
		SourceAdapter:                 strings.TrimSpace(draft.SourceAdapter),
		AdapterVersion:                strings.TrimSpace(draft.AdapterVersion),
		TargetPlatform:                strings.TrimSpace(draft.TargetPlatform),
		RuntimeConstraints:            runtimeConstraints,
		Objects:                       objects,
		Components:                    components,
		SecretReferences:              secretReferences,
		FeatureFlags:                  featureFlags,
	})
	if err != nil {
		return nil, "", fmt.Errorf("marshal canonical target config snapshot: %w", err)
	}
	sum := sha256.Sum256(payload)
	return payload, "sha256:" + hex.EncodeToString(sum[:]), nil
}

func cloneCanonicalCollection[T any](values []T) []T {
	return append(make([]T, 0, len(values)), values...)
}

func duplicateObjectKey(values []types.TargetConfigSnapshotObjectDraft) string {
	return firstDuplicateKey(len(values), func(index int) string { return values[index].Key })
}

func duplicateComponentKey(values []types.TargetConfigSnapshotComponentDraft) string {
	return firstDuplicateKey(len(values), func(index int) string { return values[index].PhysicalName })
}

func duplicateSecretReferenceKey(values []types.TargetConfigSnapshotSecretReferenceDraft) string {
	return firstDuplicateKey(len(values), func(index int) string { return values[index].Key })
}

func duplicateFeatureFlagKey(values []types.TargetConfigSnapshotFeatureFlagDraft) string {
	return firstDuplicateKey(len(values), func(index int) string { return values[index].Key })
}

func firstDuplicateKey(count int, key func(int) string) string {
	seen := make(map[string]struct{}, count)
	for index := range count {
		value := strings.TrimSpace(key(index))
		if _, exists := seen[value]; exists {
			return value
		}
		seen[value] = struct{}{}
	}
	return ""
}
