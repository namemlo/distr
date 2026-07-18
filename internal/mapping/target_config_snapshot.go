package mapping

import (
	"encoding/json"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
)

func TargetConfigSnapshotToAPI(snapshot types.TargetConfigSnapshot) api.TargetConfigSnapshot {
	runtimeConstraints := map[string]string{}
	_ = json.Unmarshal(snapshot.RuntimeConstraints, &runtimeConstraints)
	return api.TargetConfigSnapshot{
		ID:                            snapshot.ID,
		CreatedAt:                     snapshot.CreatedAt,
		CreatedByUserAccountID:        snapshot.CreatedByUserAccountID,
		DeploymentUnitID:              snapshot.DeploymentUnitID,
		TargetEnvironmentAssignmentID: snapshot.TargetEnvironmentAssignmentID,
		EnvironmentID:                 snapshot.EnvironmentID,
		SourceRepository:              snapshot.SourceRepository,
		SourceCommit:                  snapshot.SourceCommit,
		SourceAdapter:                 snapshot.SourceAdapter,
		AdapterVersion:                snapshot.AdapterVersion,
		TargetPlatform:                snapshot.TargetPlatform,
		RuntimeConstraints:            runtimeConstraints,
		CanonicalChecksum:             snapshot.CanonicalChecksum,
		Objects:                       List(snapshot.Objects, targetConfigSnapshotObjectToAPI),
		Components:                    List(snapshot.Components, targetConfigSnapshotComponentToAPI),
		SecretReferences:              List(snapshot.SecretReferences, targetConfigSnapshotSecretReferenceToAPI),
		FeatureFlags:                  List(snapshot.FeatureFlags, targetConfigSnapshotFeatureFlagToAPI),
	}
}

func TargetConfigSnapshotPageToAPI(
	page types.Page[types.TargetConfigSnapshot],
) api.TargetConfigSnapshotPage {
	return api.TargetConfigSnapshotPage{
		Items:      List(page.Items, TargetConfigSnapshotToAPI),
		NextCursor: page.NextCursor,
	}
}

func TargetConfigVerificationResultToAPI(
	result types.ObjectVerificationResult,
) api.TargetConfigObjectVerificationResult {
	return api.TargetConfigObjectVerificationResult{
		SnapshotID: result.SnapshotID,
		Verified:   result.Verified,
		Objects:    List(result.Objects, targetConfigVerificationFactToAPI),
	}
}

func targetConfigSnapshotObjectToAPI(
	object types.TargetConfigSnapshotObject,
) api.TargetConfigSnapshotObject {
	return api.TargetConfigSnapshotObject{
		Key:       object.Key,
		Kind:      object.Kind,
		Reference: object.Reference,
		VersionID: object.VersionID,
		MediaType: object.MediaType,
		SizeBytes: object.SizeBytes,
		Checksum:  object.Checksum,
	}
}

func targetConfigSnapshotComponentToAPI(
	component types.TargetConfigSnapshotComponent,
) api.TargetConfigSnapshotComponent {
	return api.TargetConfigSnapshotComponent{
		PhysicalName:        component.PhysicalName,
		ComponentInstanceID: component.ComponentInstanceID,
	}
}

func targetConfigSnapshotSecretReferenceToAPI(
	reference types.TargetConfigSnapshotSecretReference,
) api.TargetConfigSnapshotSecretReference {
	return api.TargetConfigSnapshotSecretReference{
		Key:                reference.Key,
		Provider:           reference.Provider,
		OpaqueReference:    reference.Reference,
		VersionFingerprint: reference.VersionFingerprint,
	}
}

func targetConfigSnapshotFeatureFlagToAPI(
	flag types.TargetConfigSnapshotFeatureFlag,
) api.TargetConfigSnapshotFeatureFlag {
	return api.TargetConfigSnapshotFeatureFlag{
		Key:     flag.Key,
		Enabled: flag.Enabled,
	}
}

func targetConfigVerificationFactToAPI(
	fact types.ObjectVerificationFact,
) api.TargetConfigObjectVerificationFact {
	return api.TargetConfigObjectVerificationFact{
		Key:               fact.Key,
		Verified:          fact.Verified,
		Code:              fact.Code,
		Message:           fact.Message,
		ObservedVersionID: fact.ObservedVersionID,
		ObservedMediaType: fact.ObservedMediaType,
		ObservedSizeBytes: fact.ObservedSizeBytes,
		ObservedChecksum:  fact.ObservedChecksum,
	}
}
