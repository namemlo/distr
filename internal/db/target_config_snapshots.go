package db

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/targetconfig"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	targetConfigDefaultPageLimit   = 50
	targetConfigMaximumPageLimit   = 100
	targetConfigCursorVersion      = 1
	targetConfigV1ExtractorVersion = targetconfig.V1ExtractorVersion
)

const targetConfigSnapshotOutputExpr = `
	id,
	created_at,
	created_by_user_account_id,
	organization_id,
	deployment_unit_id,
	target_environment_assignment_id,
	environment_id,
	source_repository,
	source_commit,
	source_adapter,
	adapter_version,
	target_platform,
	runtime_constraints,
	schema,
	canonical_payload,
	canonical_checksum
`

const targetConfigSnapshotObjectOutputExpr = `
	id,
	target_config_snapshot_id,
	organization_id,
	key,
	kind,
	reference,
	version_id,
	media_type,
	size_bytes,
	checksum
`

const targetConfigSnapshotComponentOutputExpr = `
	id,
	target_config_snapshot_id,
	organization_id,
	deployment_unit_id,
	component_instance_id,
	physical_name
`

const targetConfigSnapshotSecretReferenceOutputExpr = `
	id,
	target_config_snapshot_id,
	organization_id,
	key,
	provider,
	reference,
	version_fingerprint
`

const targetConfigSnapshotFeatureFlagOutputExpr = `
	id,
	target_config_snapshot_id,
	organization_id,
	key,
	enabled
`

const v1ExtractionCheckpointOutputExpr = `
	id,
	created_at,
	organization_id,
	extractor_version,
	source_state_checksum,
	dry_run_checksum,
	source_count,
	candidate_count,
	blocked_count,
	batch_size
`

const v1ExtractionLineageOutputExpr = `
	id,
	created_at,
	organization_id,
	checkpoint_id,
	original_release_bundle_id,
	original_release_checksum,
	original_plan_id,
	original_plan_checksum,
	derived_snapshot_id,
	derived_snapshot_checksum,
	extractor_version,
	status,
	blocked_reason_code
`

type targetConfigCursor struct {
	Version   int       `json:"v"`
	CreatedAt time.Time `json:"createdAt"`
	ID        uuid.UUID `json:"id"`
}

const targetConfigComponentLockQuery = `
	SELECT
		id,
		deployment_unit_id,
		physical_name
	FROM ComponentInstance
	WHERE organization_id = @organizationID
	  AND deployment_unit_id = @deploymentUnitID
	  AND id = ANY(@componentInstanceIDs)
	FOR SHARE
`

type targetConfigLockedComponent struct {
	ID               uuid.UUID `db:"id"`
	DeploymentUnitID uuid.UUID `db:"deployment_unit_id"`
	PhysicalName     string    `db:"physical_name"`
}

type v1ExtractionOutcome struct {
	ReleaseBundleID  uuid.UUID
	ReleaseChecksum  string
	PlanID           uuid.UUID
	PlanChecksum     string
	Status           types.V1ExtractionStatus
	BlockedReason    string
	SnapshotDraft    *types.TargetConfigSnapshotDraft
	SnapshotChecksum string
}

type canonicalV1ExtractionSourceState struct {
	ReleaseBundleID string `json:"releaseBundleId"`
	ReleaseChecksum string `json:"releaseChecksum"`
	PlanID          string `json:"planId"`
	PlanChecksum    string `json:"planChecksum"`
}

type canonicalV1ExtractionDryRun struct {
	ExtractorVersion    string                            `json:"extractorVersion"`
	OrganizationID      string                            `json:"organizationId"`
	SourceStateChecksum string                            `json:"sourceStateChecksum"`
	Items               []canonicalV1ExtractionDryRunItem `json:"items"`
}

type canonicalV1ExtractionDryRunItem struct {
	ReleaseBundleID  string `json:"releaseBundleId"`
	ReleaseChecksum  string `json:"releaseChecksum"`
	PlanID           string `json:"planId"`
	PlanChecksum     string `json:"planChecksum"`
	Status           string `json:"status"`
	BlockedReason    string `json:"blockedReason,omitempty"`
	SnapshotChecksum string `json:"snapshotChecksum,omitempty"`
}

type v1ExtractionSource struct {
	Bundle types.ReleaseBundle
	Plan   types.DeploymentPlan
}

type v1ExtractionPlacement struct {
	DeploymentUnitID              uuid.UUID                 `db:"deployment_unit_id"`
	TargetEnvironmentAssignmentID uuid.UUID                 `db:"target_environment_assignment_id"`
	EnvironmentID                 uuid.UUID                 `db:"environment_id"`
	ComponentInstances            []types.ComponentInstance `db:"-"`
}

func CreateTargetConfigSnapshot(
	ctx context.Context,
	draft *types.TargetConfigSnapshotDraft,
) (*types.TargetConfigSnapshot, error) {
	if draft == nil {
		return nil, apierrors.NewBadRequest("target config snapshot is required")
	}
	normalized := normalizeTargetConfigDraft(*draft)
	if normalized.OrganizationID == uuid.Nil {
		return nil, apierrors.NewBadRequest("organizationId is required")
	}
	if normalized.CreatedByUserAccountID == uuid.Nil {
		return nil, apierrors.NewBadRequest("createdByUserAccountId is required")
	}
	if issues := targetconfig.ValidateDraft(normalized); len(issues) > 0 {
		return nil, apierrors.NewBadRequest(issues[0].Field + ": " + issues[0].Message)
	}

	tx, err := internalctx.GetDb(ctx).Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin target config snapshot transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if err := lockAndValidateTargetConfigComponents(ctx, tx, normalized); err != nil {
		return nil, err
	}
	canonicalPayload, canonicalChecksum, err := targetconfig.Canonicalize(normalized)
	if err != nil {
		return nil, apierrors.NewBadRequest(err.Error())
	}
	runtimeConstraints, err := json.Marshal(normalized.RuntimeConstraints)
	if err != nil {
		return nil, apierrors.NewBadRequest("runtimeConstraints are invalid")
	}

	var snapshot types.TargetConfigSnapshot
	err = tx.QueryRow(ctx, `
		INSERT INTO TargetConfigSnapshot (
			organization_id,
			created_by_user_account_id,
			deployment_unit_id,
			target_environment_assignment_id,
			environment_id,
			source_repository,
			source_commit,
			source_adapter,
			adapter_version,
			target_platform,
			runtime_constraints,
			schema,
			canonical_payload,
			canonical_checksum
		) VALUES (
			@organizationID,
			@createdByUserAccountID,
			@deploymentUnitID,
			@targetEnvironmentAssignmentID,
			@environmentID,
			@sourceRepository,
			@sourceCommit,
			@sourceAdapter,
			@adapterVersion,
			@targetPlatform,
			@runtimeConstraints,
			@schema,
			@canonicalPayload,
			@canonicalChecksum
		)
		RETURNING `+targetConfigSnapshotOutputExpr,
		pgx.NamedArgs{
			"organizationID":                normalized.OrganizationID,
			"createdByUserAccountID":        normalized.CreatedByUserAccountID,
			"deploymentUnitID":              normalized.DeploymentUnitID,
			"targetEnvironmentAssignmentID": normalized.TargetEnvironmentAssignmentID,
			"environmentID":                 normalized.EnvironmentID,
			"sourceRepository":              normalized.SourceRepository,
			"sourceCommit":                  normalized.SourceCommit,
			"sourceAdapter":                 normalized.SourceAdapter,
			"adapterVersion":                normalized.AdapterVersion,
			"targetPlatform":                normalized.TargetPlatform,
			"runtimeConstraints":            runtimeConstraints,
			"schema":                        types.TargetConfigSnapshotSchema,
			"canonicalPayload":              canonicalPayload,
			"canonicalChecksum":             canonicalChecksum,
		},
	).Scan(
		&snapshot.ID,
		&snapshot.CreatedAt,
		&snapshot.CreatedByUserAccountID,
		&snapshot.OrganizationID,
		&snapshot.DeploymentUnitID,
		&snapshot.TargetEnvironmentAssignmentID,
		&snapshot.EnvironmentID,
		&snapshot.SourceRepository,
		&snapshot.SourceCommit,
		&snapshot.SourceAdapter,
		&snapshot.AdapterVersion,
		&snapshot.TargetPlatform,
		&snapshot.RuntimeConstraints,
		&snapshot.Schema,
		&snapshot.CanonicalPayload,
		&snapshot.CanonicalChecksum,
	)
	if err != nil {
		return nil, mapTargetConfigWriteError("create target config snapshot", err)
	}

	if err := copyTargetConfigSnapshotChildren(ctx, tx, snapshot.ID, normalized); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, mapTargetConfigWriteError("commit target config snapshot", err)
	}
	return GetTargetConfigSnapshot(ctx, normalized.OrganizationID, snapshot.ID)
}

func lockAndValidateTargetConfigComponents(
	ctx context.Context,
	tx pgx.Tx,
	draft types.TargetConfigSnapshotDraft,
) error {
	componentIDs := make([]uuid.UUID, len(draft.Components))
	for index, component := range draft.Components {
		componentIDs[index] = component.ComponentInstanceID
	}
	rows, err := tx.Query(ctx, targetConfigComponentLockQuery, pgx.NamedArgs{
		"organizationID":       draft.OrganizationID,
		"deploymentUnitID":     draft.DeploymentUnitID,
		"componentInstanceIDs": componentIDs,
	})
	if err != nil {
		return fmt.Errorf("lock target config snapshot components: %w", err)
	}
	locked, err := pgx.CollectRows(rows, pgx.RowToStructByName[targetConfigLockedComponent])
	if err != nil {
		return fmt.Errorf("read locked target config snapshot components: %w", err)
	}
	return validateLockedTargetConfigComponents(draft, locked)
}

func validateLockedTargetConfigComponents(
	draft types.TargetConfigSnapshotDraft,
	locked []targetConfigLockedComponent,
) error {
	byID := make(map[uuid.UUID]targetConfigLockedComponent, len(locked))
	for _, component := range locked {
		byID[component.ID] = component
	}
	for index, expected := range draft.Components {
		actual, exists := byID[expected.ComponentInstanceID]
		if !exists || actual.DeploymentUnitID != draft.DeploymentUnitID {
			return apierrors.ErrNotFound
		}
		if actual.PhysicalName != expected.PhysicalName {
			return apierrors.NewBadRequest(fmt.Sprintf(
				"components[%d].physicalName does not match component instance",
				index,
			))
		}
	}
	return nil
}

func GetTargetConfigSnapshot(
	ctx context.Context,
	organizationID,
	snapshotID uuid.UUID,
) (*types.TargetConfigSnapshot, error) {
	if organizationID == uuid.Nil || snapshotID == uuid.Nil {
		return nil, apierrors.ErrNotFound
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+targetConfigSnapshotOutputExpr+`
		FROM TargetConfigSnapshot
		WHERE organization_id = @organizationID
		  AND id = @snapshotID`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"snapshotID":     snapshotID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("get target config snapshot: %w", err)
	}
	snapshot, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.TargetConfigSnapshot],
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read target config snapshot: %w", err)
	}
	if err := loadTargetConfigSnapshotChildren(
		ctx,
		internalctx.GetDb(ctx),
		organizationID,
		[]*types.TargetConfigSnapshot{&snapshot},
	); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func ListTargetConfigSnapshots(
	ctx context.Context,
	filter types.TargetConfigListFilter,
) (types.Page[types.TargetConfigSnapshot], error) {
	if filter.OrganizationID == uuid.Nil {
		return types.Page[types.TargetConfigSnapshot]{}, apierrors.NewBadRequest("organizationId is required")
	}
	limit := filter.Limit
	if limit == 0 {
		limit = targetConfigDefaultPageLimit
	}
	if limit < 1 || limit > targetConfigMaximumPageLimit {
		return types.Page[types.TargetConfigSnapshot]{}, apierrors.NewBadRequest("limit must be between 1 and 100")
	}
	cursor, err := decodeTargetConfigCursor(filter.Cursor)
	if err != nil {
		return types.Page[types.TargetConfigSnapshot]{}, apierrors.NewBadRequest("cursor is invalid")
	}

	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+targetConfigSnapshotOutputExpr+`
		FROM TargetConfigSnapshot
		WHERE organization_id = @organizationID
		  AND (
		    @deploymentUnitID::uuid IS NULL
		    OR deployment_unit_id = @deploymentUnitID
		  )
		  AND (
		    @assignmentID::uuid IS NULL
		    OR target_environment_assignment_id = @assignmentID
		  )
		  AND (
		    @cursorCreatedAt::timestamptz IS NULL
		    OR (created_at, id) < (@cursorCreatedAt, @cursorID)
		  )
		ORDER BY created_at DESC, id DESC
		LIMIT @limit`,
		pgx.NamedArgs{
			"organizationID":   filter.OrganizationID,
			"deploymentUnitID": filter.DeploymentUnitID,
			"assignmentID":     filter.TargetEnvironmentAssignmentID,
			"cursorCreatedAt":  nullableTargetConfigCursorTime(cursor),
			"cursorID":         nullableTargetConfigCursorID(cursor),
			"limit":            limit + 1,
		},
	)
	if err != nil {
		return types.Page[types.TargetConfigSnapshot]{}, fmt.Errorf("list target config snapshots: %w", err)
	}
	snapshots, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.TargetConfigSnapshot])
	if err != nil {
		return types.Page[types.TargetConfigSnapshot]{}, fmt.Errorf("read target config snapshots: %w", err)
	}

	page := types.Page[types.TargetConfigSnapshot]{Items: snapshots}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		last := page.Items[len(page.Items)-1]
		page.NextCursor, err = encodeTargetConfigCursor(last.CreatedAt, last.ID)
		if err != nil {
			return types.Page[types.TargetConfigSnapshot]{}, err
		}
	}
	snapshotPointers := make([]*types.TargetConfigSnapshot, len(page.Items))
	for index := range page.Items {
		snapshotPointers[index] = &page.Items[index]
	}
	if err := loadTargetConfigSnapshotChildren(
		ctx,
		internalctx.GetDb(ctx),
		filter.OrganizationID,
		snapshotPointers,
	); err != nil {
		return types.Page[types.TargetConfigSnapshot]{}, err
	}
	return page, nil
}

func copyTargetConfigSnapshotChildren(
	ctx context.Context,
	tx pgx.Tx,
	snapshotID uuid.UUID,
	draft types.TargetConfigSnapshotDraft,
) error {
	if len(draft.Objects) > 0 {
		_, err := tx.CopyFrom(
			ctx,
			pgx.Identifier{"targetconfigsnapshotobject"},
			[]string{
				"target_config_snapshot_id", "organization_id", "key", "kind",
				"reference", "version_id", "media_type", "size_bytes", "checksum",
			},
			pgx.CopyFromSlice(len(draft.Objects), func(index int) ([]any, error) {
				object := draft.Objects[index]
				return []any{
					snapshotID, draft.OrganizationID, object.Key, object.Kind,
					object.Reference, object.VersionID, object.MediaType,
					object.SizeBytes, object.Checksum,
				}, nil
			}),
		)
		if err != nil {
			return mapTargetConfigWriteError("create target config snapshot objects", err)
		}
	}
	if len(draft.Components) > 0 {
		_, err := tx.CopyFrom(
			ctx,
			pgx.Identifier{"targetconfigsnapshotcomponent"},
			[]string{
				"target_config_snapshot_id", "organization_id", "deployment_unit_id",
				"component_instance_id", "physical_name",
			},
			pgx.CopyFromSlice(len(draft.Components), func(index int) ([]any, error) {
				component := draft.Components[index]
				return []any{
					snapshotID, draft.OrganizationID, component.DeploymentUnitID,
					component.ComponentInstanceID, component.PhysicalName,
				}, nil
			}),
		)
		if err != nil {
			return mapTargetConfigWriteError("create target config snapshot components", err)
		}
	}
	if len(draft.SecretReferences) > 0 {
		_, err := tx.CopyFrom(
			ctx,
			pgx.Identifier{"targetconfigsnapshotsecretreference"},
			[]string{
				"target_config_snapshot_id", "organization_id", "key", "provider",
				"reference", "version_fingerprint",
			},
			pgx.CopyFromSlice(len(draft.SecretReferences), func(index int) ([]any, error) {
				reference := draft.SecretReferences[index]
				return []any{
					snapshotID, draft.OrganizationID, reference.Key, reference.Provider,
					reference.Reference, reference.VersionFingerprint,
				}, nil
			}),
		)
		if err != nil {
			return mapTargetConfigWriteError("create target config snapshot secret references", err)
		}
	}
	if len(draft.FeatureFlags) > 0 {
		_, err := tx.CopyFrom(
			ctx,
			pgx.Identifier{"targetconfigsnapshotfeatureflag"},
			[]string{"target_config_snapshot_id", "organization_id", "key", "enabled"},
			pgx.CopyFromSlice(len(draft.FeatureFlags), func(index int) ([]any, error) {
				flag := draft.FeatureFlags[index]
				return []any{snapshotID, draft.OrganizationID, flag.Key, flag.Enabled}, nil
			}),
		)
		if err != nil {
			return mapTargetConfigWriteError("create target config snapshot feature flags", err)
		}
	}
	return nil
}

func loadTargetConfigSnapshotChildren(
	ctx context.Context,
	queryable interface {
		Query(context.Context, string, ...any) (pgx.Rows, error)
	},
	organizationID uuid.UUID,
	snapshots []*types.TargetConfigSnapshot,
) error {
	if len(snapshots) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(snapshots))
	byID := make(map[uuid.UUID]*types.TargetConfigSnapshot, len(snapshots))
	for index, snapshot := range snapshots {
		ids[index] = snapshot.ID
		byID[snapshot.ID] = snapshot
		snapshot.Objects = []types.TargetConfigSnapshotObject{}
		snapshot.Components = []types.TargetConfigSnapshotComponent{}
		snapshot.SecretReferences = []types.TargetConfigSnapshotSecretReference{}
		snapshot.FeatureFlags = []types.TargetConfigSnapshotFeatureFlag{}
	}
	if err := collectTargetConfigObjects(ctx, queryable, organizationID, ids, byID); err != nil {
		return err
	}
	if err := collectTargetConfigComponents(ctx, queryable, organizationID, ids, byID); err != nil {
		return err
	}
	if err := collectTargetConfigSecretReferences(ctx, queryable, organizationID, ids, byID); err != nil {
		return err
	}
	return collectTargetConfigFeatureFlags(ctx, queryable, organizationID, ids, byID)
}

func collectTargetConfigObjects(
	ctx context.Context,
	queryable interface {
		Query(context.Context, string, ...any) (pgx.Rows, error)
	},
	organizationID uuid.UUID,
	ids []uuid.UUID,
	byID map[uuid.UUID]*types.TargetConfigSnapshot,
) error {
	rows, err := queryable.Query(ctx, `
		SELECT `+targetConfigSnapshotObjectOutputExpr+`
		FROM TargetConfigSnapshotObject
		WHERE organization_id = @organizationID
		  AND target_config_snapshot_id = ANY(@ids)
		ORDER BY target_config_snapshot_id, key, id`,
		pgx.NamedArgs{"organizationID": organizationID, "ids": ids},
	)
	if err != nil {
		return fmt.Errorf("list target config snapshot objects: %w", err)
	}
	values, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.TargetConfigSnapshotObject])
	if err != nil {
		return fmt.Errorf("read target config snapshot objects: %w", err)
	}
	for _, value := range values {
		byID[value.TargetConfigSnapshotID].Objects = append(
			byID[value.TargetConfigSnapshotID].Objects,
			value,
		)
	}
	return nil
}

func collectTargetConfigComponents(
	ctx context.Context,
	queryable interface {
		Query(context.Context, string, ...any) (pgx.Rows, error)
	},
	organizationID uuid.UUID,
	ids []uuid.UUID,
	byID map[uuid.UUID]*types.TargetConfigSnapshot,
) error {
	rows, err := queryable.Query(ctx, `
		SELECT `+targetConfigSnapshotComponentOutputExpr+`
		FROM TargetConfigSnapshotComponent
		WHERE organization_id = @organizationID
		  AND target_config_snapshot_id = ANY(@ids)
		ORDER BY target_config_snapshot_id, physical_name, id`,
		pgx.NamedArgs{"organizationID": organizationID, "ids": ids},
	)
	if err != nil {
		return fmt.Errorf("list target config snapshot components: %w", err)
	}
	values, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.TargetConfigSnapshotComponent])
	if err != nil {
		return fmt.Errorf("read target config snapshot components: %w", err)
	}
	for _, value := range values {
		byID[value.TargetConfigSnapshotID].Components = append(
			byID[value.TargetConfigSnapshotID].Components,
			value,
		)
	}
	return nil
}

func collectTargetConfigSecretReferences(
	ctx context.Context,
	queryable interface {
		Query(context.Context, string, ...any) (pgx.Rows, error)
	},
	organizationID uuid.UUID,
	ids []uuid.UUID,
	byID map[uuid.UUID]*types.TargetConfigSnapshot,
) error {
	rows, err := queryable.Query(ctx, `
		SELECT `+targetConfigSnapshotSecretReferenceOutputExpr+`
		FROM TargetConfigSnapshotSecretReference
		WHERE organization_id = @organizationID
		  AND target_config_snapshot_id = ANY(@ids)
		ORDER BY target_config_snapshot_id, key, id`,
		pgx.NamedArgs{"organizationID": organizationID, "ids": ids},
	)
	if err != nil {
		return fmt.Errorf("list target config snapshot secret references: %w", err)
	}
	values, err := pgx.CollectRows(
		rows,
		pgx.RowToStructByName[types.TargetConfigSnapshotSecretReference],
	)
	if err != nil {
		return fmt.Errorf("read target config snapshot secret references: %w", err)
	}
	for _, value := range values {
		byID[value.TargetConfigSnapshotID].SecretReferences = append(
			byID[value.TargetConfigSnapshotID].SecretReferences,
			value,
		)
	}
	return nil
}

func collectTargetConfigFeatureFlags(
	ctx context.Context,
	queryable interface {
		Query(context.Context, string, ...any) (pgx.Rows, error)
	},
	organizationID uuid.UUID,
	ids []uuid.UUID,
	byID map[uuid.UUID]*types.TargetConfigSnapshot,
) error {
	rows, err := queryable.Query(ctx, `
		SELECT `+targetConfigSnapshotFeatureFlagOutputExpr+`
		FROM TargetConfigSnapshotFeatureFlag
		WHERE organization_id = @organizationID
		  AND target_config_snapshot_id = ANY(@ids)
		ORDER BY target_config_snapshot_id, key, id`,
		pgx.NamedArgs{"organizationID": organizationID, "ids": ids},
	)
	if err != nil {
		return fmt.Errorf("list target config snapshot feature flags: %w", err)
	}
	values, err := pgx.CollectRows(
		rows,
		pgx.RowToStructByName[types.TargetConfigSnapshotFeatureFlag],
	)
	if err != nil {
		return fmt.Errorf("read target config snapshot feature flags: %w", err)
	}
	for _, value := range values {
		byID[value.TargetConfigSnapshotID].FeatureFlags = append(
			byID[value.TargetConfigSnapshotID].FeatureFlags,
			value,
		)
	}
	return nil
}

func normalizeTargetConfigDraft(
	draft types.TargetConfigSnapshotDraft,
) types.TargetConfigSnapshotDraft {
	draft.RuntimeConstraints = maps.Clone(draft.RuntimeConstraints)
	if draft.RuntimeConstraints == nil {
		draft.RuntimeConstraints = map[string]string{}
	}
	draft.Objects = cloneTargetConfigCollection(draft.Objects)
	draft.Components = cloneTargetConfigCollection(draft.Components)
	draft.SecretReferences = cloneTargetConfigCollection(draft.SecretReferences)
	draft.FeatureFlags = cloneTargetConfigCollection(draft.FeatureFlags)
	draft.SourceRepository = strings.TrimSpace(draft.SourceRepository)
	draft.SourceCommit = strings.TrimSpace(draft.SourceCommit)
	draft.SourceAdapter = strings.TrimSpace(draft.SourceAdapter)
	draft.AdapterVersion = strings.TrimSpace(draft.AdapterVersion)
	draft.TargetPlatform = strings.TrimSpace(draft.TargetPlatform)
	for index := range draft.Objects {
		draft.Objects[index].Key = strings.TrimSpace(draft.Objects[index].Key)
		draft.Objects[index].Reference = strings.TrimSpace(draft.Objects[index].Reference)
		draft.Objects[index].VersionID = strings.TrimSpace(draft.Objects[index].VersionID)
		draft.Objects[index].MediaType = strings.TrimSpace(draft.Objects[index].MediaType)
		draft.Objects[index].Checksum = strings.TrimSpace(draft.Objects[index].Checksum)
	}
	for index := range draft.Components {
		draft.Components[index].PhysicalName = strings.TrimSpace(draft.Components[index].PhysicalName)
	}
	for index := range draft.SecretReferences {
		draft.SecretReferences[index].Key = strings.TrimSpace(draft.SecretReferences[index].Key)
		draft.SecretReferences[index].Provider = strings.TrimSpace(draft.SecretReferences[index].Provider)
		draft.SecretReferences[index].Reference = strings.TrimSpace(draft.SecretReferences[index].Reference)
		draft.SecretReferences[index].VersionFingerprint = strings.TrimSpace(
			draft.SecretReferences[index].VersionFingerprint,
		)
	}
	for index := range draft.FeatureFlags {
		draft.FeatureFlags[index].Key = strings.TrimSpace(draft.FeatureFlags[index].Key)
	}
	return draft
}

func cloneTargetConfigCollection[T any](values []T) []T {
	return append(make([]T, 0, len(values)), values...)
}

func mapTargetConfigWriteError(action string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case pgerrcode.UniqueViolation:
			return apierrors.ErrAlreadyExists
		case pgerrcode.ForeignKeyViolation:
			return apierrors.ErrNotFound
		case pgerrcode.CheckViolation, pgerrcode.InvalidTextRepresentation:
			return apierrors.NewBadRequest("target config snapshot is invalid")
		}
	}
	return fmt.Errorf("%s: %w", action, err)
}

func encodeTargetConfigCursor(createdAt time.Time, id uuid.UUID) (string, error) {
	payload, err := json.Marshal(targetConfigCursor{
		Version: targetConfigCursorVersion, CreatedAt: createdAt, ID: id,
	})
	if err != nil {
		return "", fmt.Errorf("encode target config cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeTargetConfigCursor(value string) (*targetConfigCursor, error) {
	if value == "" {
		return nil, nil
	}
	if len(value) > 2048 {
		return nil, errors.New("cursor is too long")
	}
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, err
	}
	var cursor targetConfigCursor
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("cursor is invalid")
	}
	if cursor.Version != targetConfigCursorVersion ||
		cursor.CreatedAt.IsZero() ||
		cursor.ID == uuid.Nil {
		return nil, errors.New("cursor is invalid")
	}
	return &cursor, nil
}

func nullableTargetConfigCursorTime(cursor *targetConfigCursor) any {
	if cursor == nil {
		return nil
	}
	return cursor.CreatedAt
}

func nullableTargetConfigCursorID(cursor *targetConfigCursor) any {
	if cursor == nil {
		return nil
	}
	return cursor.ID
}

func CreateTargetConfigV1ExtractionDryRun(
	ctx context.Context,
	organizationID uuid.UUID,
	batchSize int,
) (*types.V1ExtractionReport, error) {
	if organizationID == uuid.Nil {
		return nil, apierrors.NewBadRequest("organizationId is required")
	}
	if batchSize < 1 || batchSize > 1000 {
		return nil, apierrors.NewBadRequest("batchSize must be between 1 and 1000")
	}
	var report *types.V1ExtractionReport
	err := RunTxRR(ctx, func(ctx context.Context) error {
		outcomes, err := evaluateTargetConfigV1Extraction(ctx, organizationID, batchSize)
		if err != nil {
			return err
		}
		checkpoint, err := buildV1ExtractionCheckpoint(organizationID, batchSize, outcomes)
		if err != nil {
			return err
		}
		if err := persistV1ExtractionDryRun(ctx, checkpoint, outcomes); err != nil {
			return err
		}
		report, err = GetTargetConfigV1ExtractionReport(ctx, organizationID, checkpoint.ID)
		return err
	})
	return report, err
}

func evaluateTargetConfigV1Extraction(
	ctx context.Context,
	organizationID uuid.UUID,
	batchSize int,
) ([]v1ExtractionOutcome, error) {
	sources, err := listV1ExtractionSources(ctx, organizationID, batchSize)
	if err != nil {
		return nil, err
	}
	outcomes := make([]v1ExtractionOutcome, 0, len(sources))
	for _, source := range sources {
		outcome := v1ExtractionOutcome{
			ReleaseBundleID: source.Bundle.ID,
			ReleaseChecksum: source.Bundle.CanonicalChecksum,
			PlanID:          source.Plan.ID,
			PlanChecksum:    source.Plan.CanonicalChecksum,
		}
		placements, err := resolveV1ExtractionPlacements(ctx, source.Plan)
		if err != nil {
			return nil, err
		}
		if len(placements) == 0 {
			outcome.Status = types.V1ExtractionStatusBlocked
			outcome.BlockedReason = "placement_not_found"
			outcomes = append(outcomes, outcome)
			continue
		}
		if len(placements) != 1 {
			outcome.Status = types.V1ExtractionStatusBlocked
			outcome.BlockedReason = "ambiguous_placement"
			outcomes = append(outcomes, outcome)
			continue
		}
		placement := placements[0]
		result, err := targetconfig.ExtractV1TargetConfig(targetconfig.V1ExtractionInput{
			OrganizationID:                organizationID,
			ReleaseBundleID:               source.Bundle.ID,
			ReleaseChecksum:               source.Bundle.CanonicalChecksum,
			ReleaseCanonicalPayload:       slices.Clone(source.Bundle.CanonicalPayload),
			PlanID:                        source.Plan.ID,
			PlanChecksum:                  source.Plan.CanonicalChecksum,
			PlanCanonicalPayload:          slices.Clone(source.Plan.CanonicalPayload),
			ReleaseContract:               source.Plan.ReleaseContract,
			PlanTargets:                   slices.Clone(source.Plan.Targets),
			PlanTargetComponents:          slices.Clone(source.Plan.TargetComponents),
			PlanVariables:                 slices.Clone(source.Plan.Variables),
			ComponentInstances:            slices.Clone(placement.ComponentInstances),
			DeploymentUnitID:              placement.DeploymentUnitID,
			TargetEnvironmentAssignmentID: placement.TargetEnvironmentAssignmentID,
			EnvironmentID:                 placement.EnvironmentID,
		})
		if err != nil {
			return nil, fmt.Errorf("extract v1 target config for plan %s: %w", source.Plan.ID, err)
		}
		if result.BlockedReasonCode != "" {
			outcome.Status = types.V1ExtractionStatusBlocked
			outcome.BlockedReason = string(result.BlockedReasonCode)
		} else {
			outcome.Status = types.V1ExtractionStatusCandidate
			outcome.SnapshotDraft = result.Draft
			outcome.SnapshotChecksum = result.CanonicalChecksum
		}
		outcomes = append(outcomes, outcome)
	}
	return outcomes, nil
}

func listV1ExtractionSources(
	ctx context.Context,
	organizationID uuid.UUID,
	batchSize int,
) ([]v1ExtractionSource, error) {
	sources := []v1ExtractionSource{}
	var afterID any
	for {
		rows, err := internalctx.GetDb(ctx).Query(ctx, `
			SELECT dp.id
			FROM DeploymentPlan dp
			JOIN ReleaseBundle rb
			  ON rb.id = dp.release_bundle_id
			 AND rb.organization_id = dp.organization_id
			WHERE dp.organization_id = @organizationID
			  AND dp.release_contract ->> 'schema' = @schema
			  AND rb.release_contract ->> 'schema' = @schema
			  AND rb.status IN ('PUBLISHED', 'BLOCKED', 'ARCHIVED')
			  AND (
			    @afterID::uuid IS NULL
			    OR dp.id > @afterID
			  )
			ORDER BY dp.id
			LIMIT @batchSize`,
			pgx.NamedArgs{
				"organizationID": organizationID,
				"schema":         types.ReleaseContractSchemaV1,
				"afterID":        afterID,
				"batchSize":      batchSize,
			},
		)
		if err != nil {
			return nil, fmt.Errorf("list v1 extraction source plans: %w", err)
		}
		planIDs, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return nil, fmt.Errorf("read v1 extraction source plans: %w", err)
		}
		if len(planIDs) == 0 {
			break
		}
		for _, planID := range planIDs {
			plan, err := getDeploymentPlan(ctx, planID, organizationID)
			if err != nil {
				return nil, err
			}
			bundle, err := GetReleaseBundle(ctx, plan.ReleaseBundleID, organizationID)
			if err != nil {
				return nil, err
			}
			sources = append(sources, v1ExtractionSource{Bundle: *bundle, Plan: *plan})
		}
		afterID = planIDs[len(planIDs)-1]
	}
	return sources, nil
}

func resolveV1ExtractionPlacements(
	ctx context.Context,
	plan types.DeploymentPlan,
) ([]v1ExtractionPlacement, error) {
	if len(plan.Targets) != 1 {
		return []v1ExtractionPlacement{{}}, nil
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT
			du.id AS deployment_unit_id,
			tea.id AS target_environment_assignment_id,
			tea.environment_id
		FROM TargetEnvironmentAssignment tea
		JOIN DeploymentUnit du
		  ON du.target_environment_assignment_id = tea.id
		 AND du.organization_id = tea.organization_id
		WHERE tea.organization_id = @organizationID
		  AND tea.deployment_target_id = @deploymentTargetID
		  AND tea.environment_id = @environmentID
		  AND tea.active_from <= now()
		  AND (
		    tea.active_until IS NULL
		    OR tea.active_until > now()
		  )
		  AND du.retired_at IS NULL
		ORDER BY du.id`,
		pgx.NamedArgs{
			"organizationID":     plan.OrganizationID,
			"deploymentTargetID": plan.Targets[0].DeploymentTargetID,
			"environmentID":      plan.EnvironmentID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("resolve v1 extraction placement: %w", err)
	}
	placements, err := pgx.CollectRows(
		rows,
		pgx.RowToStructByName[v1ExtractionPlacement],
	)
	if err != nil {
		return nil, fmt.Errorf("read v1 extraction placements: %w", err)
	}
	if len(placements) != 1 {
		return placements, nil
	}
	rows, err = internalctx.GetDb(ctx).Query(ctx, `
		SELECT
			ci.id,
			ci.created_at,
			ci.updated_at,
			ci.organization_id,
			ci.deployment_unit_id,
			ci.component_definition_id,
			ci.physical_name,
			ci.config_namespace,
			ci.database_boundary,
			ci.health_adapter,
			ci.management_state,
			ci.retired_at
		FROM ComponentInstance ci
		WHERE ci.organization_id = @organizationID
		  AND ci.deployment_unit_id = @deploymentUnitID
		  AND ci.retired_at IS NULL
		ORDER BY ci.physical_name, ci.id`,
		pgx.NamedArgs{
			"organizationID":   plan.OrganizationID,
			"deploymentUnitID": placements[0].DeploymentUnitID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("resolve v1 extraction component instances: %w", err)
	}
	placements[0].ComponentInstances, err = pgx.CollectRows(
		rows,
		pgx.RowToStructByName[types.ComponentInstance],
	)
	if err != nil {
		return nil, fmt.Errorf("read v1 extraction component instances: %w", err)
	}
	return placements, nil
}

func buildV1ExtractionCheckpoint(
	organizationID uuid.UUID,
	batchSize int,
	outcomes []v1ExtractionOutcome,
) (types.V1ExtractionCheckpoint, error) {
	sourceState := make([]canonicalV1ExtractionSourceState, 0, len(outcomes))
	dryRunItems := make([]canonicalV1ExtractionDryRunItem, 0, len(outcomes))
	candidateCount := 0
	blockedCount := 0
	for _, outcome := range outcomes {
		sourceState = append(sourceState, canonicalV1ExtractionSourceState{
			ReleaseBundleID: outcome.ReleaseBundleID.String(),
			ReleaseChecksum: outcome.ReleaseChecksum,
			PlanID:          outcome.PlanID.String(),
			PlanChecksum:    outcome.PlanChecksum,
		})
		dryRunItems = append(dryRunItems, canonicalV1ExtractionDryRunItem{
			ReleaseBundleID:  outcome.ReleaseBundleID.String(),
			ReleaseChecksum:  outcome.ReleaseChecksum,
			PlanID:           outcome.PlanID.String(),
			PlanChecksum:     outcome.PlanChecksum,
			Status:           string(outcome.Status),
			BlockedReason:    outcome.BlockedReason,
			SnapshotChecksum: outcome.SnapshotChecksum,
		})
		if outcome.Status == types.V1ExtractionStatusCandidate {
			candidateCount++
		} else {
			blockedCount++
		}
	}
	sourceStateChecksum, err := checksumV1ExtractionValue(sourceState)
	if err != nil {
		return types.V1ExtractionCheckpoint{}, err
	}
	dryRunChecksum, err := checksumV1ExtractionValue(canonicalV1ExtractionDryRun{
		ExtractorVersion:    targetConfigV1ExtractorVersion,
		OrganizationID:      organizationID.String(),
		SourceStateChecksum: sourceStateChecksum,
		Items:               dryRunItems,
	})
	if err != nil {
		return types.V1ExtractionCheckpoint{}, err
	}
	checkpointID := uuid.NewSHA1(
		uuid.NameSpaceOID,
		[]byte("distr.target-config-v1-extraction\x00"+organizationID.String()+"\x00"+dryRunChecksum),
	)
	return types.V1ExtractionCheckpoint{
		ID:                  checkpointID,
		OrganizationID:      organizationID,
		ExtractorVersion:    targetConfigV1ExtractorVersion,
		SourceStateChecksum: sourceStateChecksum,
		DryRunChecksum:      dryRunChecksum,
		SourceCount:         len(outcomes),
		CandidateCount:      candidateCount,
		BlockedCount:        blockedCount,
		BatchSize:           batchSize,
	}, nil
}

func checksumV1ExtractionValue(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal v1 extraction checksum input: %w", err)
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func persistV1ExtractionDryRun(
	ctx context.Context,
	checkpoint types.V1ExtractionCheckpoint,
	outcomes []v1ExtractionOutcome,
) error {
	database := internalctx.GetDb(ctx)
	commandTag, err := database.Exec(ctx, `
		INSERT INTO BackfillCheckpoint (
			id,
			organization_id,
			extractor_version,
			source_state_checksum,
			dry_run_checksum,
			source_count,
			candidate_count,
			blocked_count,
			batch_size
		) VALUES (
			@id,
			@organizationID,
			@extractorVersion,
			@sourceStateChecksum,
			@dryRunChecksum,
			@sourceCount,
			@candidateCount,
			@blockedCount,
			@batchSize
		)
		ON CONFLICT (id) DO NOTHING`,
		pgx.NamedArgs{
			"id":                  checkpoint.ID,
			"organizationID":      checkpoint.OrganizationID,
			"extractorVersion":    checkpoint.ExtractorVersion,
			"sourceStateChecksum": checkpoint.SourceStateChecksum,
			"dryRunChecksum":      checkpoint.DryRunChecksum,
			"sourceCount":         checkpoint.SourceCount,
			"candidateCount":      checkpoint.CandidateCount,
			"blockedCount":        checkpoint.BlockedCount,
			"batchSize":           checkpoint.BatchSize,
		},
	)
	if err != nil {
		return fmt.Errorf("create v1 extraction checkpoint: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		persisted, loadErr := getV1ExtractionCheckpoint(
			ctx,
			checkpoint.OrganizationID,
			checkpoint.ID,
		)
		if loadErr != nil {
			return loadErr
		}
		if persisted.DryRunChecksum != checkpoint.DryRunChecksum ||
			persisted.SourceStateChecksum != checkpoint.SourceStateChecksum {
			return fmt.Errorf("v1 extraction checkpoint conflict: %w", apierrors.ErrConflict)
		}
		return nil
	}
	if len(outcomes) == 0 {
		return nil
	}
	_, err = database.CopyFrom(
		ctx,
		pgx.Identifier{"releasecontractv1extractionlineage"},
		[]string{
			"organization_id",
			"checkpoint_id",
			"original_release_bundle_id",
			"original_release_checksum",
			"original_plan_id",
			"original_plan_checksum",
			"derived_snapshot_checksum",
			"extractor_version",
			"status",
			"blocked_reason_code",
		},
		pgx.CopyFromSlice(len(outcomes), func(index int) ([]any, error) {
			outcome := outcomes[index]
			return []any{
				checkpoint.OrganizationID,
				checkpoint.ID,
				outcome.ReleaseBundleID,
				outcome.ReleaseChecksum,
				outcome.PlanID,
				outcome.PlanChecksum,
				outcome.SnapshotChecksum,
				checkpoint.ExtractorVersion,
				outcome.Status,
				outcome.BlockedReason,
			}, nil
		}),
	)
	if err != nil {
		return fmt.Errorf("create v1 extraction lineage: %w", err)
	}
	return nil
}

func getV1ExtractionCheckpoint(
	ctx context.Context,
	organizationID,
	checkpointID uuid.UUID,
) (*types.V1ExtractionCheckpoint, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+v1ExtractionCheckpointOutputExpr+`
		FROM BackfillCheckpoint
		WHERE id = @checkpointID
		  AND organization_id = @organizationID`,
		pgx.NamedArgs{
			"checkpointID":   checkpointID,
			"organizationID": organizationID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("get v1 extraction checkpoint: %w", err)
	}
	checkpoint, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.V1ExtractionCheckpoint],
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read v1 extraction checkpoint: %w", err)
	}
	return &checkpoint, nil
}

func GetTargetConfigV1ExtractionReport(
	ctx context.Context,
	organizationID,
	checkpointID uuid.UUID,
) (*types.V1ExtractionReport, error) {
	checkpoint, err := getV1ExtractionCheckpoint(ctx, organizationID, checkpointID)
	if err != nil {
		return nil, err
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+v1ExtractionLineageOutputExpr+`
		FROM ReleaseContractV1ExtractionLineage
		WHERE organization_id = @organizationID
		  AND checkpoint_id = @checkpointID
		ORDER BY original_plan_id, status, id`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"checkpointID":   checkpointID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("list v1 extraction lineage: %w", err)
	}
	items, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.V1ExtractionLineage])
	if err != nil {
		return nil, fmt.Errorf("read v1 extraction lineage: %w", err)
	}
	report := &types.V1ExtractionReport{
		Checkpoint: *checkpoint,
		Items:      items,
	}
	appliedPlans := make(map[uuid.UUID]struct{}, checkpoint.CandidateCount)
	candidateRows := 0
	for _, item := range items {
		switch item.Status {
		case types.V1ExtractionStatusApplied:
			appliedPlans[item.OriginalPlanID] = struct{}{}
		case types.V1ExtractionStatusCandidate:
			candidateRows++
		case types.V1ExtractionStatusBlocked:
			report.Blocked++
		}
	}
	if candidateRows != checkpoint.CandidateCount ||
		report.Blocked != checkpoint.BlockedCount {
		return nil, fmt.Errorf("v1 extraction checkpoint contains incomplete lineage")
	}
	report.Applied = len(appliedPlans)
	report.Pending = checkpoint.CandidateCount - report.Applied
	if report.Pending < 0 {
		return nil, fmt.Errorf("v1 extraction checkpoint contains inconsistent lineage")
	}
	return report, nil
}

func ApplyTargetConfigV1Extraction(
	ctx context.Context,
	organizationID,
	checkpointID uuid.UUID,
	approvedDryRunChecksum string,
	batchSize int,
) (*types.V1ExtractionReport, error) {
	if organizationID == uuid.Nil || checkpointID == uuid.Nil {
		return nil, apierrors.NewBadRequest("organizationId and checkpointId are required")
	}
	if batchSize < 1 || batchSize > 1000 {
		return nil, apierrors.NewBadRequest("batchSize must be between 1 and 1000")
	}
	checkpoint, err := getV1ExtractionCheckpoint(ctx, organizationID, checkpointID)
	if err != nil {
		return nil, err
	}
	if checkpoint.ExtractorVersion != targetConfigV1ExtractorVersion ||
		checkpoint.DryRunChecksum != approvedDryRunChecksum {
		return nil, fmt.Errorf("v1 extraction dry-run approval does not match: %w", apierrors.ErrConflict)
	}
	report, err := GetTargetConfigV1ExtractionReport(ctx, organizationID, checkpointID)
	if err != nil {
		return nil, err
	}
	if report.Pending == 0 {
		report.NoOp = report.Applied
		return report, nil
	}

	currentOutcomes, err := evaluateTargetConfigV1Extraction(
		ctx,
		organizationID,
		checkpoint.BatchSize,
	)
	if err != nil {
		return nil, err
	}
	currentCheckpoint, err := buildV1ExtractionCheckpoint(
		organizationID,
		checkpoint.BatchSize,
		currentOutcomes,
	)
	if err != nil {
		return nil, err
	}
	if currentCheckpoint.SourceStateChecksum != checkpoint.SourceStateChecksum ||
		currentCheckpoint.DryRunChecksum != checkpoint.DryRunChecksum {
		return nil, fmt.Errorf("v1 extraction source state changed after dry run: %w", apierrors.ErrConflict)
	}

	outcomeByPlanID := make(map[uuid.UUID]v1ExtractionOutcome, len(currentOutcomes))
	for _, outcome := range currentOutcomes {
		outcomeByPlanID[outcome.PlanID] = outcome
	}
	appliedPlans := make(map[uuid.UUID]struct{}, report.Applied)
	candidates := make([]types.V1ExtractionLineage, 0, checkpoint.CandidateCount)
	for _, item := range report.Items {
		switch item.Status {
		case types.V1ExtractionStatusApplied:
			appliedPlans[item.OriginalPlanID] = struct{}{}
		case types.V1ExtractionStatusCandidate:
			candidates = append(candidates, item)
		}
	}
	slices.SortFunc(candidates, func(left, right types.V1ExtractionLineage) int {
		return strings.Compare(left.OriginalPlanID.String(), right.OriginalPlanID.String())
	})

	noOp := len(appliedPlans)
	processed := 0
	for _, candidate := range candidates {
		if _, alreadyApplied := appliedPlans[candidate.OriginalPlanID]; alreadyApplied {
			continue
		}
		if processed >= batchSize {
			break
		}
		outcome, exists := outcomeByPlanID[candidate.OriginalPlanID]
		if !exists ||
			outcome.Status != types.V1ExtractionStatusCandidate ||
			outcome.SnapshotDraft == nil ||
			outcome.SnapshotChecksum != candidate.DerivedSnapshotChecksum {
			return nil, fmt.Errorf("v1 extraction candidate no longer matches checkpoint: %w", apierrors.ErrConflict)
		}
		snapshot, err := createOrGetV1ExtractionSnapshot(ctx, *outcome.SnapshotDraft, outcome.SnapshotChecksum)
		if err != nil {
			return nil, err
		}
		inserted, err := insertV1ExtractionAppliedLineage(ctx, candidate, snapshot)
		if err != nil {
			return nil, err
		}
		if !inserted {
			noOp++
		}
		processed++
	}
	report, err = GetTargetConfigV1ExtractionReport(ctx, organizationID, checkpointID)
	if err != nil {
		return nil, err
	}
	report.NoOp = noOp
	return report, nil
}

func createOrGetV1ExtractionSnapshot(
	ctx context.Context,
	draft types.TargetConfigSnapshotDraft,
	expectedChecksum string,
) (*types.TargetConfigSnapshot, error) {
	snapshot, err := CreateTargetConfigSnapshot(ctx, &draft)
	if err == nil {
		if snapshot.CanonicalChecksum != expectedChecksum {
			return nil, fmt.Errorf("created target config snapshot checksum mismatch")
		}
		return snapshot, nil
	}
	if !errors.Is(err, apierrors.ErrAlreadyExists) {
		return nil, err
	}
	return getTargetConfigSnapshotByChecksum(ctx, draft.OrganizationID, expectedChecksum)
}

func getTargetConfigSnapshotByChecksum(
	ctx context.Context,
	organizationID uuid.UUID,
	checksum string,
) (*types.TargetConfigSnapshot, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+targetConfigSnapshotOutputExpr+`
		FROM TargetConfigSnapshot
		WHERE organization_id = @organizationID
		  AND canonical_checksum = @checksum`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"checksum":       checksum,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("get target config snapshot by checksum: %w", err)
	}
	snapshot, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.TargetConfigSnapshot],
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read target config snapshot by checksum: %w", err)
	}
	return &snapshot, nil
}

func insertV1ExtractionAppliedLineage(
	ctx context.Context,
	candidate types.V1ExtractionLineage,
	snapshot *types.TargetConfigSnapshot,
) (bool, error) {
	commandTag, err := internalctx.GetDb(ctx).Exec(ctx, `
		INSERT INTO ReleaseContractV1ExtractionLineage (
			organization_id,
			checkpoint_id,
			original_release_bundle_id,
			original_release_checksum,
			original_plan_id,
			original_plan_checksum,
			derived_snapshot_id,
			derived_snapshot_checksum,
			extractor_version,
			status,
			blocked_reason_code
		) VALUES (
			@organizationID,
			@checkpointID,
			@releaseBundleID,
			@releaseChecksum,
			@planID,
			@planChecksum,
			@snapshotID,
			@snapshotChecksum,
			@extractorVersion,
			@status,
			''
		)
		ON CONFLICT (
			checkpoint_id,
			organization_id,
			original_plan_id,
			status
		) DO NOTHING`,
		pgx.NamedArgs{
			"organizationID":   candidate.OrganizationID,
			"checkpointID":     candidate.CheckpointID,
			"releaseBundleID":  candidate.OriginalReleaseBundleID,
			"releaseChecksum":  candidate.OriginalReleaseChecksum,
			"planID":           candidate.OriginalPlanID,
			"planChecksum":     candidate.OriginalPlanChecksum,
			"snapshotID":       snapshot.ID,
			"snapshotChecksum": snapshot.CanonicalChecksum,
			"extractorVersion": candidate.ExtractorVersion,
			"status":           types.V1ExtractionStatusApplied,
		},
	)
	if err != nil {
		return false, fmt.Errorf("append applied v1 extraction lineage: %w", err)
	}
	if commandTag.RowsAffected() == 1 {
		return true, nil
	}
	var persistedSnapshotID uuid.UUID
	var persistedChecksum string
	err = internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT derived_snapshot_id, derived_snapshot_checksum
		FROM ReleaseContractV1ExtractionLineage
		WHERE organization_id = @organizationID
		  AND checkpoint_id = @checkpointID
		  AND original_plan_id = @planID
		  AND status = 'applied'`,
		pgx.NamedArgs{
			"organizationID": candidate.OrganizationID,
			"checkpointID":   candidate.CheckpointID,
			"planID":         candidate.OriginalPlanID,
		},
	).Scan(&persistedSnapshotID, &persistedChecksum)
	if err != nil {
		return false, fmt.Errorf("verify applied v1 extraction lineage: %w", err)
	}
	if persistedSnapshotID != snapshot.ID || persistedChecksum != snapshot.CanonicalChecksum {
		return false, fmt.Errorf("applied v1 extraction lineage conflict: %w", apierrors.ErrConflict)
	}
	return false, nil
}
