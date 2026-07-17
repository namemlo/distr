package migrations

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestClassifyTimestampDirtyRecoveryMatrix(t *testing.T) {
	cases := []struct {
		name            string
		markerVersion   int
		shape           types.TimestampRecoveryCatalogShape
		expectedVersion uint
	}{
		{
			name:            "dirty 138 with predecessor catalog",
			markerVersion:   138,
			shape:           types.TimestampRecoveryCatalogShapePredecessor137,
			expectedVersion: 137,
		},
		{
			name:            "dirty 138 with expand catalog",
			markerVersion:   138,
			shape:           types.TimestampRecoveryCatalogShapeExpand138,
			expectedVersion: 138,
		},
		{
			name:            "dirty 137 with expand catalog",
			markerVersion:   137,
			shape:           types.TimestampRecoveryCatalogShapeExpand138,
			expectedVersion: 138,
		},
		{
			name:            "dirty 137 with predecessor catalog",
			markerVersion:   137,
			shape:           types.TimestampRecoveryCatalogShapePredecessor137,
			expectedVersion: 137,
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			version, err := ClassifyTimestampDirtyRecovery(
				SchemaStatus{Version: test.markerVersion, Dirty: true},
				test.shape,
			)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(version).To(Equal(test.expectedVersion))
		})
	}
}

func TestClassifyTimestampDirtyRecoveryRejectsUnknownShape(t *testing.T) {
	cases := []struct {
		name   string
		status SchemaStatus
		shape  types.TimestampRecoveryCatalogShape
		want   string
	}{
		{
			name:   "clean marker",
			status: SchemaStatus{Version: 138},
			shape:  types.TimestampRecoveryCatalogShapeExpand138,
			want:   "dirty",
		},
		{
			name:   "absent marker",
			status: SchemaStatus{Version: -1, Dirty: true},
			shape:  types.TimestampRecoveryCatalogShapeExpand138,
			want:   "malformed",
		},
		{
			name:   "unsupported older marker",
			status: SchemaStatus{Version: 136, Dirty: true},
			shape:  types.TimestampRecoveryCatalogShapePredecessor137,
			want:   "137 or 138",
		},
		{
			name:   "unsupported newer marker",
			status: SchemaStatus{Version: 139, Dirty: true},
			shape:  types.TimestampRecoveryCatalogShapeExpand138,
			want:   "137 or 138",
		},
		{
			name:   "unknown catalog",
			status: SchemaStatus{Version: 138, Dirty: true},
			shape:  types.TimestampRecoveryCatalogShapeUnknown,
			want:   "catalog shape",
		},
		{
			name:   "mixed catalog",
			status: SchemaStatus{Version: 138, Dirty: true},
			shape:  types.TimestampRecoveryCatalogShape("MIXED"),
			want:   "catalog shape",
		},
		{
			name:   "empty catalog marker",
			status: SchemaStatus{Version: 138, Dirty: true},
			shape:  "",
			want:   "catalog shape",
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			_, err := ClassifyTimestampDirtyRecovery(
				test.status,
				test.shape,
			)
			g.Expect(err).To(MatchError(ContainSubstring(test.want)))
		})
	}
}

func validTimestampDirtyRecoveryPlan() types.TimestampDirtyRecoveryPlan {
	startedAt := time.Date(2026, time.July, 17, 4, 5, 6, 123000000, time.UTC)
	return types.TimestampDirtyRecoveryPlan{
		FormatVersion:         types.TimestampDirtyRecoveryFormatVersion,
		RecordType:            types.TimestampDirtyRecoveryRecordTypePlan,
		RecoveryID:            uuid.MustParse("93af2518-7b0f-4463-9f4f-59500fb9e171"),
		CreatedAt:             startedAt,
		OperatorIdentity:      "release.operator@distr.example",
		Reason:                "Resume the interrupted timestamp expansion",
		WriterFenceIdentifier: "timestamp-expand:recovery-01",
		ExpectedDirtyVersion:  138,
		CatalogShape:          types.TimestampRecoveryCatalogShapeExpand138,
		ForceVersion:          138,
		CatalogChecksum:       "sha256:" + strings.Repeat("a", 64),
	}
}

func TestTimestampDirtyRecoveryPlanValidation(t *testing.T) {
	g := NewWithT(t)
	plan := validTimestampDirtyRecoveryPlan()
	g.Expect(plan.Validate()).To(Succeed())

	plan.Manifest = validTimestampDirtyRecoveryManifestBinding()
	g.Expect(plan.Validate()).To(Succeed())

	for _, matrixRow := range []struct {
		dirty uint
		force uint
		shape types.TimestampRecoveryCatalogShape
	}{
		{dirty: 138, force: 137, shape: types.TimestampRecoveryCatalogShapePredecessor137},
		{dirty: 138, force: 138, shape: types.TimestampRecoveryCatalogShapeExpand138},
		{dirty: 137, force: 138, shape: types.TimestampRecoveryCatalogShapeExpand138},
		{dirty: 137, force: 137, shape: types.TimestampRecoveryCatalogShapePredecessor137},
	} {
		plan := validTimestampDirtyRecoveryPlan()
		plan.ExpectedDirtyVersion = matrixRow.dirty
		plan.ForceVersion = matrixRow.force
		plan.CatalogShape = matrixRow.shape
		g.Expect(plan.Validate()).To(Succeed())
	}
}

func validTimestampDirtyRecoveryManifestBinding() *types.TimestampDirtyRecoveryManifestBinding {
	return &types.TimestampDirtyRecoveryManifestBinding{
		ID:                       uuid.MustParse("d2c25e3f-62f3-47da-bb47-ad13d0d256f7"),
		DocumentChecksum:         "sha256:" + strings.Repeat("b", 64),
		DecisionContentChecksum:  "sha256:" + strings.Repeat("c", 64),
		RawSetChecksum:           "sha256:" + strings.Repeat("d", 64),
		DatabaseIdentityChecksum: "sha256:" + strings.Repeat("e", 64),
		ExecutionCount:           2,
		EventCount:               3,
		RawCellCount:             13,
	}
}

func TestTimestampDirtyRecoveryPlanValidationRejectsMutations(t *testing.T) {
	nonUTC := time.FixedZone("unsafe-offset", 3600)
	cases := []struct {
		name   string
		mutate func(*types.TimestampDirtyRecoveryPlan)
		want   string
	}{
		{
			name: "wrong format version",
			mutate: func(plan *types.TimestampDirtyRecoveryPlan) {
				plan.FormatVersion = "distr.timestamp-dirty-recovery/v2"
			},
			want: "format version",
		},
		{
			name: "wrong record type",
			mutate: func(plan *types.TimestampDirtyRecoveryPlan) {
				plan.RecordType = types.TimestampDirtyRecoveryRecordTypeResult
			},
			want: "record type",
		},
		{
			name: "nil recovery id",
			mutate: func(plan *types.TimestampDirtyRecoveryPlan) {
				plan.RecoveryID = uuid.Nil
			},
			want: "recovery id",
		},
		{
			name: "zero created time",
			mutate: func(plan *types.TimestampDirtyRecoveryPlan) {
				plan.CreatedAt = time.Time{}
			},
			want: "created at",
		},
		{
			name: "non UTC created time",
			mutate: func(plan *types.TimestampDirtyRecoveryPlan) {
				plan.CreatedAt = plan.CreatedAt.In(nonUTC)
			},
			want: "UTC",
		},
		{
			name: "unsupported expected dirty version",
			mutate: func(plan *types.TimestampDirtyRecoveryPlan) {
				plan.ExpectedDirtyVersion = 136
			},
			want: "expected dirty version",
		},
		{
			name: "unsupported force version",
			mutate: func(plan *types.TimestampDirtyRecoveryPlan) {
				plan.ForceVersion = 139
			},
			want: "force version",
		},
		{
			name: "catalog and force version mismatch",
			mutate: func(plan *types.TimestampDirtyRecoveryPlan) {
				plan.ForceVersion = 137
			},
			want: "force version",
		},
		{
			name: "unsafe operator identity whitespace",
			mutate: func(plan *types.TimestampDirtyRecoveryPlan) {
				plan.OperatorIdentity = "release operator"
			},
			want: "operator identity",
		},
		{
			name: "unsafe operator identity path",
			mutate: func(plan *types.TimestampDirtyRecoveryPlan) {
				plan.OperatorIdentity = `C:\private\operator`
			},
			want: "operator identity",
		},
		{
			name: "empty reason",
			mutate: func(plan *types.TimestampDirtyRecoveryPlan) {
				plan.Reason = ""
			},
			want: "reason",
		},
		{
			name: "reason with control character",
			mutate: func(plan *types.TimestampDirtyRecoveryPlan) {
				plan.Reason = "Resume migration\nwith hidden detail"
			},
			want: "reason",
		},
		{
			name: "reason with DSN",
			mutate: func(plan *types.TimestampDirtyRecoveryPlan) {
				plan.Reason = "Resume using postgres://operator:secret@localhost/distr"
			},
			want: "reason",
		},
		{
			name: "reason with credential assignment",
			mutate: func(plan *types.TimestampDirtyRecoveryPlan) {
				plan.Reason = "Resume after password=secret was rotated"
			},
			want: "reason",
		},
		{
			name: "unsafe writer fence",
			mutate: func(plan *types.TimestampDirtyRecoveryPlan) {
				plan.WriterFenceIdentifier = `C:\private\fence`
			},
			want: "writer fence",
		},
		{
			name: "malformed catalog checksum",
			mutate: func(plan *types.TimestampDirtyRecoveryPlan) {
				plan.CatalogChecksum = "sha256:" + strings.Repeat("A", 64)
			},
			want: "catalog checksum",
		},
		{
			name: "manifest nil UUID",
			mutate: func(plan *types.TimestampDirtyRecoveryPlan) {
				plan.Manifest = validTimestampDirtyRecoveryManifestBinding()
				plan.Manifest.ID = uuid.Nil
			},
			want: "manifest id",
		},
		{
			name: "manifest document checksum",
			mutate: func(plan *types.TimestampDirtyRecoveryPlan) {
				plan.Manifest = validTimestampDirtyRecoveryManifestBinding()
				plan.Manifest.DocumentChecksum = ""
			},
			want: "document checksum",
		},
		{
			name: "manifest decision checksum",
			mutate: func(plan *types.TimestampDirtyRecoveryPlan) {
				plan.Manifest = validTimestampDirtyRecoveryManifestBinding()
				plan.Manifest.DecisionContentChecksum = ""
			},
			want: "decision content checksum",
		},
		{
			name: "manifest raw set checksum",
			mutate: func(plan *types.TimestampDirtyRecoveryPlan) {
				plan.Manifest = validTimestampDirtyRecoveryManifestBinding()
				plan.Manifest.RawSetChecksum = ""
			},
			want: "raw set checksum",
		},
		{
			name: "manifest database identity checksum",
			mutate: func(plan *types.TimestampDirtyRecoveryPlan) {
				plan.Manifest = validTimestampDirtyRecoveryManifestBinding()
				plan.Manifest.DatabaseIdentityChecksum = ""
			},
			want: "database identity checksum",
		},
		{
			name: "manifest has zero history",
			mutate: func(plan *types.TimestampDirtyRecoveryPlan) {
				plan.Manifest = validTimestampDirtyRecoveryManifestBinding()
				plan.Manifest.ExecutionCount = 0
				plan.Manifest.EventCount = 0
				plan.Manifest.RawCellCount = 0
			},
			want: "non-empty history",
		},
		{
			name: "manifest raw cell count mismatch",
			mutate: func(plan *types.TimestampDirtyRecoveryPlan) {
				plan.Manifest = validTimestampDirtyRecoveryManifestBinding()
				plan.Manifest.RawCellCount++
			},
			want: "raw cell count",
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			plan := validTimestampDirtyRecoveryPlan()
			test.mutate(&plan)
			g.Expect(plan.Validate()).To(MatchError(ContainSubstring(test.want)))
		})
	}
}

func TestTimestampDirtyRecoveryPlanValidationBoundaries(t *testing.T) {
	g := NewWithT(t)
	plan := validTimestampDirtyRecoveryPlan()
	plan.OperatorIdentity = "a" + strings.Repeat("b", 127)
	plan.Reason = "a" + strings.Repeat("b", 255)
	plan.WriterFenceIdentifier = "a" + strings.Repeat("b", 127)
	g.Expect(plan.Validate()).To(Succeed())

	plan = validTimestampDirtyRecoveryPlan()
	plan.OperatorIdentity = "a"
	plan.Reason = "a"
	plan.WriterFenceIdentifier = "a"
	g.Expect(plan.Validate()).To(Succeed())

	for _, mutate := range []func(*types.TimestampDirtyRecoveryPlan){
		func(plan *types.TimestampDirtyRecoveryPlan) {
			plan.OperatorIdentity += "c"
		},
		func(plan *types.TimestampDirtyRecoveryPlan) {
			plan.Reason += "c"
		},
		func(plan *types.TimestampDirtyRecoveryPlan) {
			plan.WriterFenceIdentifier += "c"
		},
	} {
		plan := validTimestampDirtyRecoveryPlan()
		plan.OperatorIdentity = "a" + strings.Repeat("b", 127)
		plan.Reason = "a" + strings.Repeat("b", 255)
		plan.WriterFenceIdentifier = "a" + strings.Repeat("b", 127)
		mutate(&plan)
		g.Expect(plan.Validate()).NotTo(Succeed())
	}
}

func TestTimestampDirtyRecoveryPlanJSONHasNoOperatorTargetOrUnapprovedFields(t *testing.T) {
	g := NewWithT(t)
	encoded, err := json.Marshal(validTimestampDirtyRecoveryPlan())
	g.Expect(err).NotTo(HaveOccurred())
	var fields map[string]any
	g.Expect(json.Unmarshal(encoded, &fields)).To(Succeed())
	g.Expect(fields).To(HaveLen(11))
	g.Expect(mapKeys(fields)).To(ConsistOf(
		"formatVersion",
		"recordType",
		"recoveryId",
		"createdAt",
		"operatorIdentity",
		"reason",
		"writerFenceIdentifier",
		"expectedDirtyVersion",
		"catalogShape",
		"forceVersion",
		"catalogChecksum",
	))
}

func validTimestampDirtyRecoveryResult() types.TimestampDirtyRecoveryResult {
	return types.TimestampDirtyRecoveryResult{
		FormatVersion: types.TimestampDirtyRecoveryFormatVersion,
		RecordType:    types.TimestampDirtyRecoveryRecordTypeResult,
		RecoveryID:    uuid.MustParse("93af2518-7b0f-4463-9f4f-59500fb9e171"),
		PlanChecksum:  "sha256:" + strings.Repeat("f", 64),
		CompletedAt:   time.Date(2026, time.July, 17, 4, 5, 7, 0, time.UTC),
		PlannedStatus: types.TimestampDirtyRecoverySchemaStatus{
			Version: 138,
			Dirty:   true,
		},
		ObservedPreApplyStatus: types.TimestampDirtyRecoverySchemaStatus{
			Version: 138,
			Dirty:   true,
		},
		Action:          types.TimestampDirtyRecoveryActionForced,
		ForcedVersion:   138,
		CatalogChecksum: "sha256:" + strings.Repeat("a", 64),
		Result:          types.TimestampDirtyRecoveryResultSucceeded,
		PostStatus: types.TimestampDirtyRecoverySchemaStatus{
			Version: 138,
			Dirty:   false,
		},
	}
}

func TestTimestampDirtyRecoveryEvidenceOmitsSensitiveData(t *testing.T) {
	g := NewWithT(t)
	result := validTimestampDirtyRecoveryResult()
	g.Expect(result.Validate()).To(Succeed())
	encoded, err := json.Marshal(result)
	g.Expect(err).NotTo(HaveOccurred())
	var fields map[string]any
	g.Expect(json.Unmarshal(encoded, &fields)).To(Succeed())
	g.Expect(fields).To(HaveLen(12))
	g.Expect(mapKeys(fields)).To(ConsistOf(
		"formatVersion",
		"recordType",
		"recoveryId",
		"planChecksum",
		"completedAt",
		"plannedStatus",
		"observedPreApplyStatus",
		"action",
		"forcedVersion",
		"catalogChecksum",
		"result",
		"postStatus",
	))
	lower := strings.ToLower(string(encoded))
	for _, forbidden := range []string{
		"dsn",
		"host",
		"username",
		"password",
		"path",
		"rawtimestamp",
		"rowid",
		"evidenceref",
		"sql",
		"rawerror",
		"reason",
		"writerfence",
	} {
		g.Expect(lower).NotTo(ContainSubstring(forbidden))
	}
}

func mapKeys(fields map[string]any) []string {
	keys := make([]string, 0, len(fields))
	for field := range fields {
		keys = append(keys, field)
	}
	return keys
}

func TestTimestampDirtyRecoveryResultValidation(t *testing.T) {
	g := NewWithT(t)
	result := validTimestampDirtyRecoveryResult()
	g.Expect(result.Validate()).To(Succeed())

	result.Action = types.TimestampDirtyRecoveryActionObservedAlreadyClean
	result.ObservedPreApplyStatus.Dirty = false
	g.Expect(result.Validate()).To(Succeed())
}

func TestTimestampDirtyRecoveryResultValidationRejectsMutations(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*types.TimestampDirtyRecoveryResult)
		want   string
	}{
		{
			name: "wrong format version",
			mutate: func(result *types.TimestampDirtyRecoveryResult) {
				result.FormatVersion = "distr.timestamp-dirty-recovery/v2"
			},
			want: "format version",
		},
		{
			name: "wrong record type",
			mutate: func(result *types.TimestampDirtyRecoveryResult) {
				result.RecordType = types.TimestampDirtyRecoveryRecordTypePlan
			},
			want: "record type",
		},
		{
			name: "nil recovery id",
			mutate: func(result *types.TimestampDirtyRecoveryResult) {
				result.RecoveryID = uuid.Nil
			},
			want: "recovery id",
		},
		{
			name: "malformed plan checksum",
			mutate: func(result *types.TimestampDirtyRecoveryResult) {
				result.PlanChecksum = "bad"
			},
			want: "plan checksum",
		},
		{
			name: "zero completion time",
			mutate: func(result *types.TimestampDirtyRecoveryResult) {
				result.CompletedAt = time.Time{}
			},
			want: "completed at",
		},
		{
			name: "planned status must be dirty",
			mutate: func(result *types.TimestampDirtyRecoveryResult) {
				result.PlannedStatus.Dirty = false
			},
			want: "planned status",
		},
		{
			name: "forced action requires dirty observation",
			mutate: func(result *types.TimestampDirtyRecoveryResult) {
				result.ObservedPreApplyStatus.Dirty = false
			},
			want: "FORCED",
		},
		{
			name: "already clean action requires clean observation",
			mutate: func(result *types.TimestampDirtyRecoveryResult) {
				result.Action = types.TimestampDirtyRecoveryActionObservedAlreadyClean
			},
			want: "OBSERVED_ALREADY_CLEAN",
		},
		{
			name: "unknown action",
			mutate: func(result *types.TimestampDirtyRecoveryResult) {
				result.Action = "REFUSED"
			},
			want: "action",
		},
		{
			name: "post status is dirty",
			mutate: func(result *types.TimestampDirtyRecoveryResult) {
				result.PostStatus.Dirty = true
			},
			want: "post status",
		},
		{
			name: "post status version mismatch",
			mutate: func(result *types.TimestampDirtyRecoveryResult) {
				result.PostStatus.Version = 137
			},
			want: "forced version",
		},
		{
			name: "malformed catalog checksum",
			mutate: func(result *types.TimestampDirtyRecoveryResult) {
				result.CatalogChecksum = "bad"
			},
			want: "catalog checksum",
		},
		{
			name: "failure result is forbidden",
			mutate: func(result *types.TimestampDirtyRecoveryResult) {
				result.Result = "FAILED"
			},
			want: "SUCCEEDED",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			result := validTimestampDirtyRecoveryResult()
			test.mutate(&result)
			g.Expect(result.Validate()).To(MatchError(ContainSubstring(test.want)))
		})
	}
}

func timestampRecoveryCatalogFixture() []types.TimestampRecoveryCatalogRecord {
	return []types.TimestampRecoveryCatalogRecord{
		{
			Category:   types.TimestampRecoveryCatalogCategoryRelation,
			ObjectName: "externalexecution",
			Definition: "ordinary-table",
		},
		{
			Category:     types.TimestampRecoveryCatalogCategoryColumn,
			RelationName: "externalexecution",
			ObjectName:   "created_at_instant",
			Definition:   "timestamp-with-time-zone:not-null",
		},
		{
			Category:     types.TimestampRecoveryCatalogCategoryConstraint,
			RelationName: "externalexecution",
			ObjectName:   "externalexecution_created_at_instant_check",
			Definition:   "check:created_at_instant-is-not-null",
		},
		{
			Category:     types.TimestampRecoveryCatalogCategoryIndex,
			RelationName: "externalexecution",
			ObjectName:   "idx_externalexecution_created_at_instant_next",
			Definition:   "btree:created_at_instant",
		},
		{
			Category:     types.TimestampRecoveryCatalogCategoryTrigger,
			RelationName: "externalexecution",
			ObjectName:   "externalexecution_timestamp_pair_guard",
			Definition:   "before-insert-or-update",
		},
		{
			Category:   types.TimestampRecoveryCatalogCategoryFunction,
			ObjectName: "external_execution_timestamp_pair_guard",
			Definition: "trigger-function:v1",
		},
	}
}

func TestTimestampRecoveryCatalogChecksumIsOrderStable(t *testing.T) {
	g := NewWithT(t)
	records := timestampRecoveryCatalogFixture()
	original := slices.Clone(records)
	forward, err := ComputeTimestampRecoveryCatalogChecksum(records)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(forward).To(MatchRegexp(`^sha256:[0-9a-f]{64}$`))
	g.Expect(records).To(Equal(original), "checksum must not mutate caller order")

	reversed := slices.Clone(records)
	slices.Reverse(reversed)
	backward, err := ComputeTimestampRecoveryCatalogChecksum(reversed)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(backward).To(Equal(forward))
}

func TestTimestampRecoveryCatalogChecksumLengthFramesFields(t *testing.T) {
	g := NewWithT(t)
	left, err := ComputeTimestampRecoveryCatalogChecksum(
		[]types.TimestampRecoveryCatalogRecord{{
			Category:   types.TimestampRecoveryCatalogCategoryRelation,
			ObjectName: "ab",
			Definition: "c",
		}},
	)
	g.Expect(err).NotTo(HaveOccurred())
	right, err := ComputeTimestampRecoveryCatalogChecksum(
		[]types.TimestampRecoveryCatalogRecord{{
			Category:   types.TimestampRecoveryCatalogCategoryRelation,
			ObjectName: "a",
			Definition: "bc",
		}},
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(left).NotTo(Equal(right))
}

func TestTimestampRecoveryCatalogChecksumRejectsMutations(t *testing.T) {
	cases := []struct {
		name    string
		records func() []types.TimestampRecoveryCatalogRecord
		want    string
	}{
		{
			name:    "empty catalog",
			records: func() []types.TimestampRecoveryCatalogRecord { return nil },
			want:    "at least one",
		},
		{
			name: "unknown category",
			records: func() []types.TimestampRecoveryCatalogRecord {
				records := timestampRecoveryCatalogFixture()
				records[0].Category = "VIEW"
				return records
			},
			want: "category",
		},
		{
			name: "relation has parent",
			records: func() []types.TimestampRecoveryCatalogRecord {
				records := timestampRecoveryCatalogFixture()
				records[0].RelationName = "public"
				return records
			},
			want: "relation name",
		},
		{
			name: "column missing relation",
			records: func() []types.TimestampRecoveryCatalogRecord {
				records := timestampRecoveryCatalogFixture()
				records[1].RelationName = ""
				return records
			},
			want: "relation name",
		},
		{
			name: "function has relation",
			records: func() []types.TimestampRecoveryCatalogRecord {
				records := timestampRecoveryCatalogFixture()
				records[5].RelationName = "externalexecution"
				return records
			},
			want: "relation name",
		},
		{
			name: "unsafe object name",
			records: func() []types.TimestampRecoveryCatalogRecord {
				records := timestampRecoveryCatalogFixture()
				records[0].ObjectName = `C:\private\table`
				return records
			},
			want: "object name",
		},
		{
			name: "empty definition",
			records: func() []types.TimestampRecoveryCatalogRecord {
				records := timestampRecoveryCatalogFixture()
				records[0].Definition = ""
				return records
			},
			want: "definition",
		},
		{
			name: "definition contains NUL",
			records: func() []types.TimestampRecoveryCatalogRecord {
				records := timestampRecoveryCatalogFixture()
				records[0].Definition = "ordinary\x00table"
				return records
			},
			want: "definition",
		},
		{
			name: "duplicate identity",
			records: func() []types.TimestampRecoveryCatalogRecord {
				records := timestampRecoveryCatalogFixture()
				records = append(records, records[0])
				records[len(records)-1].Definition = "different-definition"
				return records
			},
			want: "duplicate",
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			_, err := ComputeTimestampRecoveryCatalogChecksum(test.records())
			g.Expect(err).To(MatchError(ContainSubstring(test.want)))
		})
	}
}

func TestTimestampDirtyRecoveryCanonicalDocumentChecksumGolden(t *testing.T) {
	checksum, err := computeTimestampDirtyRecoveryDocumentChecksum(
		struct {
			A string `json:"a"`
		}{A: "b"},
	)

	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(checksum).To(Equal(
		"sha256:bf5d360a201497a7353c13dbd865c0968cacefcf8dd3b7a5904ecc8843a727f4",
	))
}
