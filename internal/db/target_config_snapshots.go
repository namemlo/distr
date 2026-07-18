package db

import (
	"context"
	"encoding/base64"
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
	targetConfigDefaultPageLimit = 50
	targetConfigMaximumPageLimit = 100
	targetConfigCursorVersion    = 1
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
