package db

import (
	"context"
	"fmt"

	"github.com/distr-sh/distr/internal/schemaevidence"
	"github.com/distr-sh/distr/internal/types"
)

func loadTargetPlanSchemaEvidence(
	ctx context.Context,
	verifier TargetConfigObjectVerifier,
	objects []types.TargetPlanConfigObject,
) ([]types.SchemaReportRecord, []types.MigrationEvidenceRecord, []types.ValidationIssue) {
	reports := make([]types.SchemaReportRecord, 0)
	evidence := make([]types.MigrationEvidenceRecord, 0)
	issues := make([]types.ValidationIssue, 0)
	reader, readable := verifier.(TargetConfigEvidenceReader)
	for _, object := range objects {
		isReport := object.MediaType == types.SchemaReportMediaTypeV1
		isMigrationEvidence := object.MediaType == types.MigrationEvidenceMediaTypeV1
		if !isReport && !isMigrationEvidence {
			continue
		}
		field := "targetConfigSnapshotId.objects." + object.Key
		if object.Kind != types.TargetConfigObjectKindAdapterInput {
			issues = append(issues, types.ValidationIssue{
				Code: "schema_evidence_wrong_kind", Field: field,
				Message: "schema evidence objects must use adapter_input kind",
			})
			continue
		}
		if !readable {
			issues = append(issues, types.ValidationIssue{
				Code: "schema_evidence_unavailable", Field: field,
				Message: "schema evidence object reading is unavailable",
			})
			continue
		}
		observed, body, err := reader.ReadTargetConfigObject(
			ctx,
			object,
			schemaevidence.MaxDocumentBytes,
		)
		if err != nil {
			issues = append(issues, types.ValidationIssue{
				Code: "schema_evidence_unavailable", Field: field,
				Message: "schema evidence object could not be read and verified",
			})
			continue
		}
		if observed.Reference != object.Reference || observed.VersionID != object.VersionID ||
			observed.MediaType != object.MediaType || observed.SizeBytes != object.SizeBytes ||
			observed.Checksum != object.Checksum {
			issues = append(issues, types.ValidationIssue{
				Code: "schema_evidence_object_mismatch", Field: field,
				Message: "schema evidence object does not match the immutable target config binding",
			})
			continue
		}
		binding := schemaEvidenceObjectFromTargetConfig(object)
		if isReport {
			report, decodeErr := schemaevidence.DecodeSchemaReport(body)
			if decodeErr != nil {
				issues = append(issues, schemaEvidenceDecodeIssue(field, decodeErr))
				continue
			}
			reports = append(reports, types.SchemaReportRecord{Object: binding, Report: report})
			continue
		}
		migrationEvidence, decodeErr := schemaevidence.DecodeMigrationEvidence(body)
		if decodeErr != nil {
			issues = append(issues, schemaEvidenceDecodeIssue(field, decodeErr))
			continue
		}
		evidence = append(evidence, types.MigrationEvidenceRecord{
			Object: binding, Evidence: migrationEvidence,
		})
	}
	return reports, evidence, issues
}

func schemaEvidenceObjectFromTargetConfig(
	object types.TargetPlanConfigObject,
) types.SchemaEvidenceObject {
	return types.SchemaEvidenceObject{
		ObjectKey: object.Key, Reference: object.Reference, VersionID: object.VersionID,
		MediaType: object.MediaType, SizeBytes: object.SizeBytes, Checksum: object.Checksum,
	}
}

func schemaEvidenceDecodeIssue(field string, err error) types.ValidationIssue {
	return types.ValidationIssue{
		Code: schemaevidence.ErrorCode(err), Field: field,
		Message: fmt.Sprintf("schema evidence document is invalid: %s", err.Error()),
	}
}
