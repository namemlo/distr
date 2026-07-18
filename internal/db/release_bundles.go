package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/channelrules"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/lifecycle"
	"github.com/distr-sh/distr/internal/releasebundles"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	releaseBundleOutputExpr = `
	rb.id,
	rb.created_at,
	rb.updated_at,
	rb.organization_id,
	rb.application_id,
	rb.channel_id,
	rb.process_snapshot_id,
	rb.variable_snapshot_id,
	rb.release_number,
	rb.release_notes,
	rb.source_revision,
	rb.source_repository,
	rb.source_branch,
	rb.source_tag,
	rb.ci_provider,
	rb.ci_run_id,
	rb.ci_run_url,
	rb.release_contract,
	rb.kind,
	rb.release_contract_schema,
	rb.status,
	rb.published_by_user_account_id,
	rb.published_at,
	rb.canonical_checksum,
	rb.canonical_payload
`
	releaseBundleComponentOutputExpr = `
	rbc.id,
	rbc.release_bundle_id,
	rbc.key,
	rbc.name,
	rbc.component_type,
	rbc.version,
	rbc.application_version_id,
	rbc.package_ref,
	rbc.digest,
	rbc.checksum,
	rbc.child_release_bundle_id
`
	releaseBundleAuditEventOutputExpr = `
	rbae.id,
	rbae.created_at,
	rbae.organization_id,
	rbae.release_bundle_id,
	rbae.actor_user_account_id,
	rbae.event_type,
	rbae.from_status,
	rbae.to_status,
	rbae.reason
	`
)

var ErrReleaseBundleIdempotencyConflict = errors.New("release bundle idempotency conflict")

func CreateReleaseBundle(ctx context.Context, bundle *types.ReleaseBundle) error {
	return RunTx(ctx, func(ctx context.Context) error {
		return createReleaseBundle(ctx, bundle)
	})
}

func CreateReleaseBundleWithIdempotency(ctx context.Context, bundle *types.ReleaseBundle, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return CreateReleaseBundle(ctx, bundle)
	}
	return RunTx(ctx, func(ctx context.Context) error {
		if bundle.Status == "" {
			bundle.Status = types.ReleaseBundleStatusDraft
		}
		if bundle.Status != types.ReleaseBundleStatusDraft {
			return fmt.Errorf("could not create ReleaseBundle: %w", apierrors.ErrConflict)
		}
		if err := ensureReleaseBundleReferences(ctx, bundle); err != nil {
			return err
		}
		if err := setReleaseBundleContractMetadata(bundle); err != nil {
			return err
		}
		if err := setReleaseBundleCanonicalFields(bundle); err != nil {
			return err
		}

		keyHash := hashReleaseBundleIdempotencyKey(key)
		if err := lockReleaseBundleIdempotencyKey(ctx, bundle.OrganizationID, keyHash); err != nil {
			return err
		}

		existingID, existingChecksum, found, err := getReleaseBundleIdempotencyRecord(
			ctx,
			bundle.OrganizationID,
			keyHash,
		)
		if err != nil {
			return err
		}
		if found {
			if existingChecksum != bundle.CanonicalChecksum {
				return fmt.Errorf("%w: different canonical request checksum", ErrReleaseBundleIdempotencyConflict)
			}
			existing, err := getReleaseBundle(ctx, existingID, bundle.OrganizationID, false)
			if err != nil {
				return err
			}
			*bundle = *existing
			return nil
		}

		if err := insertReleaseBundle(ctx, bundle); err != nil {
			return err
		}
		return insertReleaseBundleIdempotencyRecord(ctx, bundle.OrganizationID, keyHash, bundle.CanonicalChecksum, bundle.ID)
	})
}

func createReleaseBundle(ctx context.Context, bundle *types.ReleaseBundle) error {
	if bundle.Status == "" {
		bundle.Status = types.ReleaseBundleStatusDraft
	}
	if bundle.Status != types.ReleaseBundleStatusDraft {
		return fmt.Errorf("could not create ReleaseBundle: %w", apierrors.ErrConflict)
	}
	if err := ensureReleaseBundleReferences(ctx, bundle); err != nil {
		return err
	}
	if err := setReleaseBundleContractMetadata(bundle); err != nil {
		return err
	}
	if err := setReleaseBundleCanonicalFields(bundle); err != nil {
		return err
	}
	return insertReleaseBundle(ctx, bundle)
}

func insertReleaseBundle(ctx context.Context, bundle *types.ReleaseBundle) error {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`INSERT INTO ReleaseBundle AS rb (
			organization_id,
			application_id,
			channel_id,
			process_snapshot_id,
			release_number,
			release_notes,
			source_revision,
			source_repository,
			source_branch,
			source_tag,
			ci_provider,
			ci_run_id,
			ci_run_url,
			release_contract,
			kind,
			release_contract_schema,
			status,
			canonical_checksum,
			canonical_payload
		) VALUES (
			@organizationId,
			@applicationId,
			@channelId,
			@processSnapshotId,
			@releaseNumber,
			@releaseNotes,
			@sourceRevision,
			@sourceRepository,
			@sourceBranch,
			@sourceTag,
			@ciProvider,
			@ciRunId,
			@ciRunUrl,
			@releaseContract,
			@kind,
			@releaseContractSchema,
			@status,
			@canonicalChecksum,
			@canonicalPayload
		) RETURNING `+releaseBundleOutputExpr,
		pgx.NamedArgs{
			"organizationId":        bundle.OrganizationID,
			"applicationId":         bundle.ApplicationID,
			"channelId":             bundle.ChannelID,
			"processSnapshotId":     bundle.ProcessSnapshotID,
			"releaseNumber":         bundle.ReleaseNumber,
			"releaseNotes":          bundle.ReleaseNotes,
			"sourceRevision":        bundle.SourceRevision,
			"sourceRepository":      bundle.SourceRepository,
			"sourceBranch":          bundle.SourceBranch,
			"sourceTag":             bundle.SourceTag,
			"ciProvider":            bundle.CIProvider,
			"ciRunId":               bundle.CIRunID,
			"ciRunUrl":              bundle.CIRunURL,
			"releaseContract":       bundle.ReleaseContract,
			"kind":                  bundle.Kind,
			"releaseContractSchema": bundle.ReleaseContractSchema,
			"status":                bundle.Status,
			"canonicalChecksum":     bundle.CanonicalChecksum,
			"canonicalPayload":      bundle.CanonicalPayload,
		},
	)
	if err != nil {
		return mapReleaseBundleWriteError("insert", err)
	}
	created, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.ReleaseBundle])
	if err != nil {
		return mapReleaseBundleWriteError("scan created", err)
	}
	if err := insertReleaseBundleComponents(ctx, created.ID, bundle.Components); err != nil {
		return err
	}
	if err := replaceComponentReleaseFacts(ctx, created.ID, bundle.OrganizationID, bundle.ReleaseContract); err != nil {
		return err
	}
	loaded, err := getReleaseBundle(ctx, created.ID, bundle.OrganizationID, false)
	if err != nil {
		return err
	}
	*bundle = *loaded
	return nil
}

func hashReleaseBundleIdempotencyKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func lockReleaseBundleIdempotencyKey(ctx context.Context, organizationID uuid.UUID, keyHash string) error {
	db := internalctx.GetDb(ctx)
	if _, err := db.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended(@lockKey, 0))`,
		pgx.NamedArgs{"lockKey": organizationID.String() + ":" + keyHash},
	); err != nil {
		return fmt.Errorf("could not lock ReleaseBundle idempotency key: %w", err)
	}
	return nil
}

func getReleaseBundleIdempotencyRecord(
	ctx context.Context,
	organizationID uuid.UUID,
	keyHash string,
) (uuid.UUID, string, bool, error) {
	db := internalctx.GetDb(ctx)
	var releaseBundleID uuid.UUID
	var requestChecksum string
	err := db.QueryRow(
		ctx,
		`SELECT release_bundle_id, request_checksum
		FROM ReleaseBundleIdempotencyKey
		WHERE organization_id = @organizationId AND key_hash = @keyHash`,
		pgx.NamedArgs{
			"organizationId": organizationID,
			"keyHash":        keyHash,
		},
	).Scan(&releaseBundleID, &requestChecksum)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, "", false, nil
	}
	if err != nil {
		return uuid.Nil, "", false, fmt.Errorf("could not query ReleaseBundle idempotency key: %w", err)
	}
	return releaseBundleID, requestChecksum, true, nil
}

func insertReleaseBundleIdempotencyRecord(
	ctx context.Context,
	organizationID uuid.UUID,
	keyHash string,
	requestChecksum string,
	releaseBundleID uuid.UUID,
) error {
	db := internalctx.GetDb(ctx)
	_, err := db.Exec(
		ctx,
		`INSERT INTO ReleaseBundleIdempotencyKey (
			organization_id,
			key_hash,
			request_checksum,
			release_bundle_id
		) VALUES (
			@organizationId,
			@keyHash,
			@requestChecksum,
			@releaseBundleId
		)`,
		pgx.NamedArgs{
			"organizationId":  organizationID,
			"keyHash":         keyHash,
			"requestChecksum": requestChecksum,
			"releaseBundleId": releaseBundleID,
		},
	)
	if err != nil {
		return mapReleaseBundleWriteError("insert idempotency key", err)
	}
	return nil
}

func GetReleaseBundlesByOrganizationID(ctx context.Context, orgID uuid.UUID) ([]types.ReleaseBundle, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+releaseBundleOutputExpr+`
		FROM ReleaseBundle rb
		WHERE rb.organization_id = @organizationId
		ORDER BY rb.application_id, rb.release_number, rb.id`,
		pgx.NamedArgs{"organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query ReleaseBundle: %w", err)
	}
	bundles, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.ReleaseBundle])
	if err != nil {
		return nil, fmt.Errorf("could not collect ReleaseBundle: %w", err)
	}
	for i := range bundles {
		components, err := getReleaseBundleComponents(ctx, bundles[i].ID)
		if err != nil {
			return nil, err
		}
		bundles[i].Components = components
	}
	return bundles, nil
}

func GetReleaseBundle(ctx context.Context, id, orgID uuid.UUID) (*types.ReleaseBundle, error) {
	return getReleaseBundle(ctx, id, orgID, false)
}

func GetReleaseBundleEligibility(
	ctx context.Context,
	releaseBundleID uuid.UUID,
	environmentID uuid.UUID,
	organizationID uuid.UUID,
) (lifecycle.EligibilityResult, error) {
	bundle, err := GetReleaseBundle(ctx, releaseBundleID, organizationID)
	if err != nil {
		return lifecycle.EligibilityResult{}, err
	}
	if _, err := GetEnvironment(ctx, environmentID, organizationID); err != nil {
		return lifecycle.EligibilityResult{}, err
	}
	channel, err := getChannel(ctx, bundle.ChannelID, organizationID, false)
	if err != nil {
		return lifecycle.EligibilityResult{}, err
	}
	lifecycleModel, err := GetLifecycle(ctx, channel.LifecycleID, organizationID)
	if err != nil {
		return lifecycle.EligibilityResult{}, err
	}
	result := lifecycle.NewEligibilityService().Explain(ctx, lifecycle.EligibilityRequest{
		ReleaseBundle: *bundle,
		Channel:       *channel,
		Lifecycle:     *lifecycleModel,
		EnvironmentID: environmentID,
	})
	return result, nil
}

func getReleaseBundle(ctx context.Context, id, orgID uuid.UUID, forUpdate bool) (*types.ReleaseBundle, error) {
	lockClause := ""
	if forUpdate {
		lockClause = " FOR UPDATE"
	}

	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+releaseBundleOutputExpr+`
		FROM ReleaseBundle rb
		WHERE rb.id = @id AND rb.organization_id = @organizationId`+lockClause,
		pgx.NamedArgs{"id": id, "organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query ReleaseBundle: %w", err)
	}
	bundle, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.ReleaseBundle])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("could not collect ReleaseBundle: %w", err)
	}
	bundle.Components, err = getReleaseBundleComponents(ctx, bundle.ID)
	if err != nil {
		return nil, err
	}
	return &bundle, nil
}

func UpdateReleaseBundle(ctx context.Context, bundle *types.ReleaseBundle) error {
	return RunTx(ctx, func(ctx context.Context) error {
		existing, err := getReleaseBundle(ctx, bundle.ID, bundle.OrganizationID, true)
		if err != nil {
			return err
		}
		if existing.Status != types.ReleaseBundleStatusDraft {
			return fmt.Errorf("could not update ReleaseBundle: %w", apierrors.ErrConflict)
		}
		bundle.Status = existing.Status
		if bundle.DeploymentProcessRevisionID == nil {
			bundle.ProcessSnapshotID = existing.ProcessSnapshotID
		}
		if err := ensureReleaseBundleReferences(ctx, bundle); err != nil {
			return err
		}
		if err := setReleaseBundleContractMetadata(bundle); err != nil {
			return err
		}
		if err := setReleaseBundleCanonicalFields(bundle); err != nil {
			return err
		}

		db := internalctx.GetDb(ctx)
		rows, err := db.Query(ctx,
			`UPDATE ReleaseBundle AS rb SET
				application_id = @applicationId,
				channel_id = @channelId,
				process_snapshot_id = @processSnapshotId,
				release_number = @releaseNumber,
				release_notes = @releaseNotes,
				source_revision = @sourceRevision,
				source_repository = @sourceRepository,
				source_branch = @sourceBranch,
				source_tag = @sourceTag,
				ci_provider = @ciProvider,
				ci_run_id = @ciRunId,
				ci_run_url = @ciRunUrl,
				release_contract = @releaseContract,
				kind = @kind,
				release_contract_schema = @releaseContractSchema,
				canonical_checksum = @canonicalChecksum,
				canonical_payload = @canonicalPayload,
				updated_at = now()
			WHERE rb.id = @id AND rb.organization_id = @organizationId
			RETURNING `+releaseBundleOutputExpr,
			pgx.NamedArgs{
				"id":                    bundle.ID,
				"organizationId":        bundle.OrganizationID,
				"applicationId":         bundle.ApplicationID,
				"channelId":             bundle.ChannelID,
				"processSnapshotId":     bundle.ProcessSnapshotID,
				"releaseNumber":         bundle.ReleaseNumber,
				"releaseNotes":          bundle.ReleaseNotes,
				"sourceRevision":        bundle.SourceRevision,
				"sourceRepository":      bundle.SourceRepository,
				"sourceBranch":          bundle.SourceBranch,
				"sourceTag":             bundle.SourceTag,
				"ciProvider":            bundle.CIProvider,
				"ciRunId":               bundle.CIRunID,
				"ciRunUrl":              bundle.CIRunURL,
				"releaseContract":       bundle.ReleaseContract,
				"kind":                  bundle.Kind,
				"releaseContractSchema": bundle.ReleaseContractSchema,
				"canonicalChecksum":     bundle.CanonicalChecksum,
				"canonicalPayload":      bundle.CanonicalPayload,
			},
		)
		if err != nil {
			return mapReleaseBundleWriteError("update", err)
		}
		updated, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.ReleaseBundle])
		if errors.Is(err, pgx.ErrNoRows) {
			return apierrors.ErrNotFound
		} else if err != nil {
			return mapReleaseBundleWriteError("scan updated", err)
		}
		if _, err := db.Exec(
			ctx,
			`DELETE FROM ReleaseBundleComponent WHERE release_bundle_id = @releaseBundleId`,
			pgx.NamedArgs{"releaseBundleId": bundle.ID},
		); err != nil {
			return fmt.Errorf("could not replace ReleaseBundle components: %w", err)
		}
		if err := insertReleaseBundleComponents(ctx, bundle.ID, bundle.Components); err != nil {
			return err
		}
		if err := replaceComponentReleaseFacts(ctx, bundle.ID, bundle.OrganizationID, bundle.ReleaseContract); err != nil {
			return err
		}
		loaded, err := getReleaseBundle(ctx, updated.ID, bundle.OrganizationID, false)
		if err != nil {
			return err
		}
		*bundle = *loaded
		return nil
	})
}

func DeleteReleaseBundleWithID(ctx context.Context, id, organizationID uuid.UUID) error {
	return RunTx(ctx, func(ctx context.Context) error {
		bundle, err := getReleaseBundle(ctx, id, organizationID, true)
		if err != nil {
			return err
		}
		if bundle.Status != types.ReleaseBundleStatusDraft {
			return fmt.Errorf("could not delete ReleaseBundle: %w", apierrors.ErrConflict)
		}

		db := internalctx.GetDb(ctx)
		cmd, err := db.Exec(
			ctx,
			`DELETE FROM ReleaseBundle WHERE id = @id AND organization_id = @organizationId`,
			pgx.NamedArgs{"id": id, "organizationId": organizationID},
		)
		if err != nil {
			return mapReleaseBundleWriteError("delete", err)
		}
		if cmd.RowsAffected() == 0 {
			return apierrors.ErrNotFound
		}
		return nil
	})
}

func ValidateReleaseBundle(
	ctx context.Context,
	id uuid.UUID,
	organizationID uuid.UUID,
) (releasebundles.ValidationResult, error) {
	bundle, err := GetReleaseBundle(ctx, id, organizationID)
	if err != nil {
		return releasebundles.ValidationResult{}, err
	}
	return validateReleaseBundle(ctx, *bundle)
}

func PublishReleaseBundle(
	ctx context.Context,
	id uuid.UUID,
	organizationID uuid.UUID,
	actorUserAccountID uuid.UUID,
) (*types.ReleaseBundle, releasebundles.ValidationResult, error) {
	result := releasebundles.NewValidResult()
	var published *types.ReleaseBundle
	var operationErr error
	err := RunTx(ctx, func(ctx context.Context) error {
		bundle, err := getReleaseBundle(ctx, id, organizationID, true)
		if err != nil {
			return err
		}
		toStatus := types.ReleaseBundleStatusPublished
		if bundle.Status == types.ReleaseBundleStatusPublished &&
			bundle.Kind == types.ReleaseBundleKindComponent {
			published = bundle
			return nil
		}
		if bundle.Status != types.ReleaseBundleStatusDraft {
			reason := releaseBundleTransitionRejectedReason(bundle.Status, toStatus)
			if err := insertReleaseBundleAuditEvent(ctx, releaseBundleAuditEventForTransition(
				*bundle,
				actorUserAccountID,
				types.ReleaseBundleAuditEventTypeStateTransitionRejected,
				&toStatus,
				reason,
			)); err != nil {
				return err
			}
			operationErr = fmt.Errorf("could not publish ReleaseBundle: %w", apierrors.ErrConflict)
			return nil
		}
		result, err = validateReleaseBundle(ctx, *bundle)
		if err != nil {
			return err
		}
		if !result.Valid {
			if err := insertReleaseBundleAuditEvent(ctx, releaseBundleAuditEventForTransition(
				*bundle,
				actorUserAccountID,
				types.ReleaseBundleAuditEventTypeStateTransitionRejected,
				&toStatus,
				"validation failed",
			)); err != nil {
				return err
			}
			operationErr = fmt.Errorf("could not publish ReleaseBundle: %w", apierrors.ErrBadRequest)
			return nil
		}

		if bundle.Kind == types.ReleaseBundleKindComponent {
			conflict, err := componentReleaseIdentityConflict(ctx, *bundle)
			if err != nil {
				return err
			}
			if conflict {
				if err := insertReleaseBundleAuditEvent(ctx, releaseBundleAuditEventForTransition(
					*bundle,
					actorUserAccountID,
					types.ReleaseBundleAuditEventTypeStateTransitionRejected,
					&toStatus,
					"component version artifact identity conflict",
				)); err != nil {
					return err
				}
				operationErr = fmt.Errorf("could not publish ReleaseBundle: %w", apierrors.ErrConflict)
				return nil
			}
		} else {
			snapshot, err := createVariableSnapshotForReleaseBundle(ctx, *bundle)
			if err != nil {
				return err
			}
			bundle.VariableSnapshotID = &snapshot.ID
		}
		if err := setReleaseBundleCanonicalFields(bundle); err != nil {
			return err
		}

		updated, err := publishReleaseBundleStatus(ctx, bundle, toStatus, actorUserAccountID)
		if err != nil {
			return err
		}
		if err := insertReleaseBundleAuditEvent(ctx, releaseBundleAuditEventForTransition(
			*bundle,
			actorUserAccountID,
			types.ReleaseBundleAuditEventTypePublished,
			&toStatus,
			"",
		)); err != nil {
			return err
		}
		published = updated
		return nil
	})
	if err != nil {
		return published, result, err
	}
	return published, result, operationErr
}

func componentReleaseIdentityConflict(ctx context.Context, bundle types.ReleaseBundle) (bool, error) {
	if bundle.ReleaseContract == nil || bundle.ReleaseContract.ComponentV2 == nil {
		return false, nil
	}
	component := bundle.ReleaseContract.ComponentV2
	db := internalctx.GetDb(ctx)
	identityKey := strings.Join([]string{
		bundle.OrganizationID.String(),
		component.ComponentKey,
		component.Version,
	}, "\x00")
	if _, err := db.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended(@identity, 0))`,
		pgx.NamedArgs{"identity": identityKey},
	); err != nil {
		return false, fmt.Errorf("could not lock Component Release identity: %w", err)
	}
	rows, err := db.Query(ctx, `
		SELECT
			artifact.release_bundle_id,
			artifact.artifact_key,
			artifact.artifact_type,
			artifact.media_type,
			artifact.manifest_digest,
			artifact.platform,
			artifact.platform_digest
		FROM ComponentReleaseArtifact artifact
		WHERE artifact.organization_id = @organizationId
		  AND artifact.component_key = @componentKey
		  AND artifact.component_version = @componentVersion
		  AND artifact.release_bundle_id <> @releaseBundleId
		  AND EXISTS (
		    SELECT 1
		    FROM ReleaseBundleAuditEvent published_event
		    WHERE published_event.organization_id = artifact.organization_id
		      AND published_event.release_bundle_id = artifact.release_bundle_id
		      AND published_event.event_type = @publishedEventType
		  )
		ORDER BY artifact.release_bundle_id, artifact.artifact_key, artifact.platform`,
		pgx.NamedArgs{
			"organizationId":     bundle.OrganizationID,
			"componentKey":       component.ComponentKey,
			"componentVersion":   component.Version,
			"releaseBundleId":    bundle.ID,
			"publishedEventType": types.ReleaseBundleAuditEventTypePublished,
		},
	)
	if err != nil {
		return false, fmt.Errorf("could not query Component Release identity: %w", err)
	}
	defer rows.Close()
	existing := map[uuid.UUID][]string{}
	for rows.Next() {
		var releaseBundleID uuid.UUID
		var artifactKey, artifactType, mediaType, manifestDigest, platform, platformDigest string
		if err := rows.Scan(
			&releaseBundleID,
			&artifactKey,
			&artifactType,
			&mediaType,
			&manifestDigest,
			&platform,
			&platformDigest,
		); err != nil {
			return false, fmt.Errorf("could not scan Component Release identity: %w", err)
		}
		existing[releaseBundleID] = append(existing[releaseBundleID], componentReleaseArtifactIdentityToken(
			artifactKey,
			artifactType,
			mediaType,
			manifestDigest,
			platform,
			platformDigest,
		))
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("could not collect Component Release identity: %w", err)
	}
	candidateIdentity := componentReleaseArtifactIdentity(component.Artifacts)
	for _, existingIdentity := range existing {
		slices.Sort(existingIdentity)
		if !slices.Equal(existingIdentity, candidateIdentity) {
			return true, nil
		}
	}
	return false, nil
}

func componentReleaseArtifactIdentity(artifacts []types.ComponentReleaseArtifact) []string {
	identity := make([]string, 0)
	for _, artifact := range artifacts {
		for _, platform := range artifact.Platforms {
			identity = append(identity, componentReleaseArtifactIdentityToken(
				artifact.Key,
				artifact.Type,
				artifact.MediaType,
				artifact.Digest,
				platform.Platform,
				platform.Digest,
			))
		}
	}
	slices.Sort(identity)
	return identity
}

func componentReleaseArtifactIdentityToken(
	artifactKey string,
	artifactType string,
	mediaType string,
	manifestDigest string,
	platform string,
	platformDigest string,
) string {
	return strings.Join([]string{
		artifactKey,
		artifactType,
		mediaType,
		manifestDigest,
		platform,
		platformDigest,
	}, "\x00")
}

func publishReleaseBundleStatus(
	ctx context.Context,
	bundle *types.ReleaseBundle,
	status types.ReleaseBundleStatus,
	publishedByUserAccountID uuid.UUID,
) (*types.ReleaseBundle, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`UPDATE ReleaseBundle AS rb SET
				status = @status,
				variable_snapshot_id = @variableSnapshotId,
				canonical_checksum = @canonicalChecksum,
				canonical_payload = @canonicalPayload,
				published_by_user_account_id = @publishedByUserAccountId,
				published_at = now(),
				updated_at = now()
			WHERE rb.id = @id AND rb.organization_id = @organizationId
			RETURNING `+releaseBundleOutputExpr,
		pgx.NamedArgs{
			"id":                       bundle.ID,
			"organizationId":           bundle.OrganizationID,
			"status":                   status,
			"variableSnapshotId":       bundle.VariableSnapshotID,
			"canonicalChecksum":        bundle.CanonicalChecksum,
			"canonicalPayload":         bundle.CanonicalPayload,
			"publishedByUserAccountId": publishedByUserAccountID,
		},
	)
	if err != nil {
		return nil, mapReleaseBundleWriteError("publish", err)
	}
	updated, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.ReleaseBundle])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	} else if err != nil {
		return nil, mapReleaseBundleWriteError("scan published", err)
	}
	updated.Components, err = getReleaseBundleComponents(ctx, updated.ID)
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

func BlockReleaseBundle(
	ctx context.Context,
	id uuid.UUID,
	organizationID uuid.UUID,
	actorUserAccountID uuid.UUID,
) (*types.ReleaseBundle, error) {
	return transitionReleaseBundle(
		ctx,
		id,
		organizationID,
		actorUserAccountID,
		types.ReleaseBundleStatusBlocked,
		types.ReleaseBundleAuditEventTypeBlocked,
		map[types.ReleaseBundleStatus]struct{}{types.ReleaseBundleStatusPublished: {}},
	)
}

func ArchiveReleaseBundle(
	ctx context.Context,
	id uuid.UUID,
	organizationID uuid.UUID,
	actorUserAccountID uuid.UUID,
) (*types.ReleaseBundle, error) {
	return transitionReleaseBundle(
		ctx,
		id,
		organizationID,
		actorUserAccountID,
		types.ReleaseBundleStatusArchived,
		types.ReleaseBundleAuditEventTypeArchived,
		map[types.ReleaseBundleStatus]struct{}{
			types.ReleaseBundleStatusPublished: {},
			types.ReleaseBundleStatusBlocked:   {},
		},
	)
}

func GetReleaseBundleAuditEvents(
	ctx context.Context,
	releaseBundleID uuid.UUID,
	organizationID uuid.UUID,
) ([]types.ReleaseBundleAuditEvent, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+releaseBundleAuditEventOutputExpr+`
		FROM ReleaseBundleAuditEvent rbae
		WHERE rbae.release_bundle_id = @releaseBundleId AND rbae.organization_id = @organizationId
		ORDER BY rbae.created_at, rbae.id`,
		pgx.NamedArgs{
			"releaseBundleId": releaseBundleID,
			"organizationId":  organizationID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query ReleaseBundleAuditEvent: %w", err)
	}
	events, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.ReleaseBundleAuditEvent])
	if err != nil {
		return nil, fmt.Errorf("could not collect ReleaseBundleAuditEvent: %w", err)
	}
	return events, nil
}

func getReleaseBundleComponents(
	ctx context.Context,
	releaseBundleID uuid.UUID,
) ([]types.ReleaseBundleComponent, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+releaseBundleComponentOutputExpr+`
		FROM ReleaseBundleComponent rbc
		WHERE rbc.release_bundle_id = @releaseBundleId
		ORDER BY rbc.key, rbc.id`,
		pgx.NamedArgs{"releaseBundleId": releaseBundleID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query ReleaseBundleComponent: %w", err)
	}
	components, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.ReleaseBundleComponent])
	if err != nil {
		return nil, fmt.Errorf("could not collect ReleaseBundleComponent: %w", err)
	}
	return components, nil
}

func validateReleaseBundle(ctx context.Context, bundle types.ReleaseBundle) (releasebundles.ValidationResult, error) {
	result := releasebundles.ValidateBundleContent(bundle)
	channel, err := getChannel(ctx, bundle.ChannelID, bundle.OrganizationID, false)
	if errors.Is(err, apierrors.ErrNotFound) {
		result.AddError("channelId", "exists", "release bundle channel does not exist in this organization")
		return result, nil
	} else if err != nil {
		return releasebundles.ValidationResult{}, err
	}
	if err := validateReleaseBundleSourceRules(&result, bundle, *channel); err != nil {
		return releasebundles.ValidationResult{}, err
	}

	for _, component := range bundle.Components {
		switch component.Type {
		case types.ReleaseBundleComponentTypeApplicationVersion:
			if err := validateApplicationVersionComponent(ctx, &result, bundle, component, *channel); err != nil {
				return releasebundles.ValidationResult{}, err
			}
		case types.ReleaseBundleComponentTypeChildReleaseBundle:
			if err := validateChildReleaseBundleComponent(ctx, &result, bundle, component); err != nil {
				return releasebundles.ValidationResult{}, err
			}
		}
	}
	result.Valid = len(result.Errors) == 0
	return result, nil
}

func validateReleaseBundleSourceRules(
	result *releasebundles.ValidationResult,
	bundle types.ReleaseBundle,
	channel types.Channel,
) error {
	channelResult, err := channelrules.EvaluateSource(
		channelrulesFromChannel(channel),
		channelrules.Input{
			SourceBranch: bundle.SourceBranch,
			SourceTag:    bundle.SourceTag,
		},
	)
	if err != nil {
		return fmt.Errorf("could not validate ReleaseBundle source rules: %w", err)
	}
	for _, issue := range channelResult.Issues {
		result.AddError(releaseBundleSourceIssueField(issue.Field), issue.Rule, issue.Message)
	}
	return nil
}

func releaseBundleSourceIssueField(field string) string {
	switch field {
	case "sourceBranch":
		return "sourceMetadata.branch"
	case "sourceTag":
		return "sourceMetadata.tag"
	case "source":
		return "sourceMetadata"
	default:
		return "sourceMetadata." + field
	}
}

func validateApplicationVersionComponent(
	ctx context.Context,
	result *releasebundles.ValidationResult,
	bundle types.ReleaseBundle,
	component types.ReleaseBundleComponent,
	channel types.Channel,
) error {
	fieldPrefix := "components." + component.Key
	if component.ApplicationVersionID == nil {
		result.AddError(
			fieldPrefix+".applicationVersionId",
			"required",
			"application version component must reference an application version",
		)
		return nil
	}

	db := internalctx.GetDb(ctx)
	var versionName string
	err := db.QueryRow(ctx,
		`SELECT av.name
		FROM ApplicationVersion av
		JOIN Application a ON a.id = av.application_id
		WHERE av.id = @applicationVersionId
			AND a.id = @applicationId
			AND a.organization_id = @organizationId`,
		pgx.NamedArgs{
			"applicationVersionId": *component.ApplicationVersionID,
			"applicationId":        bundle.ApplicationID,
			"organizationId":       bundle.OrganizationID,
		},
	).Scan(&versionName)
	if errors.Is(err, pgx.ErrNoRows) {
		result.AddError(
			fieldPrefix+".applicationVersionId",
			"organization",
			"application version does not belong to this application and organization",
		)
		return nil
	} else if err != nil {
		return fmt.Errorf("could not validate ReleaseBundle application version component: %w", err)
	}
	if versionName != component.Version {
		result.AddError(
			fieldPrefix+".version",
			"applicationVersion",
			"component version must match the referenced application version",
		)
	}

	channelResult, err := channelrules.EvaluateVersion(
		channelrulesFromChannel(channel),
		channelrules.Input{Version: component.Version},
	)
	if err != nil {
		return fmt.Errorf("could not validate ReleaseBundle channel rules: %w", err)
	}
	for _, issue := range channelResult.Issues {
		result.AddError(fieldPrefix+"."+issue.Field, issue.Rule, issue.Message)
	}
	return nil
}

func channelrulesFromChannel(channel types.Channel) channelrules.Rules {
	return channelrules.Rules{
		AllowedVersionRanges:        channel.AllowedVersionRanges,
		AllowedPrereleasePatterns:   channel.AllowedPrereleasePatterns,
		AllowedSourceBranchPatterns: channel.AllowedSourceBranchPatterns,
		AllowedSourceTagPatterns:    channel.AllowedSourceTagPatterns,
	}
}

func validateChildReleaseBundleComponent(
	ctx context.Context,
	result *releasebundles.ValidationResult,
	bundle types.ReleaseBundle,
	component types.ReleaseBundleComponent,
) error {
	fieldPrefix := "components." + component.Key
	if component.ChildReleaseBundleID == nil || *component.ChildReleaseBundleID == bundle.ID {
		result.AddError(fieldPrefix+".childReleaseBundleId", "exists", "child release bundle reference is invalid")
		return nil
	}

	db := internalctx.GetDb(ctx)
	var childStatus types.ReleaseBundleStatus
	err := db.QueryRow(ctx,
		`SELECT status
		FROM ReleaseBundle
		WHERE id = @childReleaseBundleId AND organization_id = @organizationId`,
		pgx.NamedArgs{
			"childReleaseBundleId": *component.ChildReleaseBundleID,
			"organizationId":       bundle.OrganizationID,
		},
	).Scan(&childStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		result.AddError(
			fieldPrefix+".childReleaseBundleId",
			"organization",
			"child release bundle does not belong to this organization",
		)
		return nil
	} else if err != nil {
		return fmt.Errorf("could not validate child ReleaseBundle component: %w", err)
	}
	if childStatus != types.ReleaseBundleStatusPublished {
		result.AddError(fieldPrefix+".childReleaseBundleId", "published", "child release bundle must be published")
	}
	return nil
}

func transitionReleaseBundle(
	ctx context.Context,
	id uuid.UUID,
	organizationID uuid.UUID,
	actorUserAccountID uuid.UUID,
	toStatus types.ReleaseBundleStatus,
	eventType types.ReleaseBundleAuditEventType,
	allowedFrom map[types.ReleaseBundleStatus]struct{},
) (*types.ReleaseBundle, error) {
	var updated *types.ReleaseBundle
	var operationErr error
	err := RunTx(ctx, func(ctx context.Context) error {
		bundle, err := getReleaseBundle(ctx, id, organizationID, true)
		if err != nil {
			return err
		}
		if _, ok := allowedFrom[bundle.Status]; !ok {
			reason := releaseBundleTransitionRejectedReason(bundle.Status, toStatus)
			if err := insertReleaseBundleAuditEvent(ctx, releaseBundleAuditEventForTransition(
				*bundle,
				actorUserAccountID,
				types.ReleaseBundleAuditEventTypeStateTransitionRejected,
				&toStatus,
				reason,
			)); err != nil {
				return err
			}
			operationErr = fmt.Errorf("could not transition ReleaseBundle: %w", apierrors.ErrConflict)
			return nil
		}
		updated, err = updateReleaseBundleStatus(ctx, bundle.ID, bundle.OrganizationID, toStatus, nil)
		if err != nil {
			return err
		}
		if err := insertReleaseBundleAuditEvent(ctx, releaseBundleAuditEventForTransition(
			*bundle,
			actorUserAccountID,
			eventType,
			&toStatus,
			"",
		)); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return updated, err
	}
	return updated, operationErr
}

func updateReleaseBundleStatus(
	ctx context.Context,
	id uuid.UUID,
	organizationID uuid.UUID,
	status types.ReleaseBundleStatus,
	publishedByUserAccountID *uuid.UUID,
) (*types.ReleaseBundle, error) {
	publishedAssignments := ""
	args := pgx.NamedArgs{
		"id":             id,
		"organizationId": organizationID,
		"status":         status,
	}
	if publishedByUserAccountID != nil {
		publishedAssignments = `,
				published_by_user_account_id = @publishedByUserAccountId,
				published_at = now()`
		args["publishedByUserAccountId"] = *publishedByUserAccountID
	}

	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`UPDATE ReleaseBundle AS rb SET
				status = @status,
				updated_at = now()`+publishedAssignments+`
			WHERE rb.id = @id AND rb.organization_id = @organizationId
			RETURNING `+releaseBundleOutputExpr,
		args,
	)
	if err != nil {
		return nil, mapReleaseBundleWriteError("transition", err)
	}
	updated, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.ReleaseBundle])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	} else if err != nil {
		return nil, mapReleaseBundleWriteError("scan transitioned", err)
	}
	updated.Components, err = getReleaseBundleComponents(ctx, updated.ID)
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

func releaseBundleAuditEventForTransition(
	bundle types.ReleaseBundle,
	actorUserAccountID uuid.UUID,
	eventType types.ReleaseBundleAuditEventType,
	toStatus *types.ReleaseBundleStatus,
	reason string,
) types.ReleaseBundleAuditEvent {
	return types.ReleaseBundleAuditEvent{
		OrganizationID:     bundle.OrganizationID,
		ReleaseBundleID:    bundle.ID,
		ActorUserAccountID: &actorUserAccountID,
		EventType:          eventType,
		FromStatus:         bundle.Status,
		ToStatus:           toStatus,
		Reason:             reason,
	}
}

func releaseBundleTransitionRejectedReason(
	fromStatus types.ReleaseBundleStatus,
	toStatus types.ReleaseBundleStatus,
) string {
	return fmt.Sprintf("release bundle cannot transition from %s to %s", fromStatus, toStatus)
}

func insertReleaseBundleAuditEvent(ctx context.Context, event types.ReleaseBundleAuditEvent) error {
	db := internalctx.GetDb(ctx)
	_, err := db.Exec(ctx,
		`INSERT INTO ReleaseBundleAuditEvent (
			organization_id,
			release_bundle_id,
			actor_user_account_id,
			event_type,
			from_status,
			to_status,
			reason
		) VALUES (
			@organizationId,
			@releaseBundleId,
			@actorUserAccountId,
			@eventType,
			@fromStatus,
			@toStatus,
			@reason
		)`,
		pgx.NamedArgs{
			"organizationId":     event.OrganizationID,
			"releaseBundleId":    event.ReleaseBundleID,
			"actorUserAccountId": event.ActorUserAccountID,
			"eventType":          event.EventType,
			"fromStatus":         event.FromStatus,
			"toStatus":           event.ToStatus,
			"reason":             event.Reason,
		},
	)
	if err != nil {
		return mapReleaseBundleWriteError("insert audit event", err)
	}
	return nil
}

func insertReleaseBundleComponents(
	ctx context.Context,
	releaseBundleID uuid.UUID,
	components []types.ReleaseBundleComponent,
) error {
	if len(components) == 0 {
		return nil
	}
	rows := make([]types.ReleaseBundleComponent, len(components))
	for i, component := range components {
		component.ID = uuid.New()
		component.ReleaseBundleID = releaseBundleID
		rows[i] = component
	}

	db := internalctx.GetDb(ctx)
	_, err := db.CopyFrom(
		ctx,
		pgx.Identifier{"releasebundlecomponent"},
		[]string{
			"id",
			"release_bundle_id",
			"key",
			"name",
			"component_type",
			"version",
			"application_version_id",
			"package_ref",
			"digest",
			"checksum",
			"child_release_bundle_id",
		},
		pgx.CopyFromSlice(len(rows), func(i int) ([]any, error) {
			component := rows[i]
			return []any{
				component.ID,
				component.ReleaseBundleID,
				component.Key,
				component.Name,
				component.Type,
				component.Version,
				component.ApplicationVersionID,
				component.PackageRef,
				component.Digest,
				component.Checksum,
				component.ChildReleaseBundleID,
			}, nil
		}),
	)
	if err != nil {
		return mapReleaseBundleWriteError("insert components", err)
	}
	return nil
}

func setReleaseBundleContractMetadata(bundle *types.ReleaseBundle) error {
	if bundle.ReleaseContract != nil && bundle.ReleaseContract.ComponentV2 != nil {
		bundle.Kind = types.ReleaseBundleKindComponent
		bundle.ReleaseContractSchema = types.ReleaseContractSchemaV2
		return nil
	}
	if bundle.Kind == "" {
		bundle.Kind = types.ReleaseBundleKindLegacy
	}
	if bundle.ReleaseContractSchema == "" {
		bundle.ReleaseContractSchema = types.ReleaseContractStorageSchemaV1
	}
	if bundle.Kind != types.ReleaseBundleKindLegacy ||
		bundle.ReleaseContractSchema != types.ReleaseContractStorageSchemaV1 {
		return apierrors.NewBadRequest("release bundle kind and release contract schema do not match")
	}
	return nil
}

func setReleaseBundleCanonicalFields(bundle *types.ReleaseBundle) error {
	bundle.ReleaseContract = releasebundles.NormalizedReleaseContract(bundle.ReleaseContract)
	payload, checksum, err := releasebundles.Canonicalize(*bundle)
	if err != nil {
		return fmt.Errorf("could not canonicalize ReleaseBundle: %w", err)
	}
	bundle.CanonicalPayload = payload
	bundle.CanonicalChecksum = checksum
	return nil
}

type componentReleaseArtifactFact struct {
	artifact types.ComponentReleaseArtifact
	platform types.ComponentReleasePlatform
}

type componentReleaseEvidenceFact struct {
	kind      string
	reference string
}

func replaceComponentReleaseFacts(
	ctx context.Context,
	releaseBundleID uuid.UUID,
	organizationID uuid.UUID,
	contract *types.ReleaseContract,
) error {
	db := internalctx.GetDb(ctx)
	if _, err := db.Exec(ctx, `
		WITH deleted_migrations AS (
			DELETE FROM ComponentReleaseMigrationDeclaration
			WHERE release_bundle_id = @releaseBundleId
		),
		deleted_capabilities AS (
			DELETE FROM ComponentReleaseCapability
			WHERE release_bundle_id = @releaseBundleId
		),
		deleted_evidence AS (
			DELETE FROM ComponentReleaseEvidence
			WHERE release_bundle_id = @releaseBundleId
		)
		DELETE FROM ComponentReleaseArtifact
		WHERE release_bundle_id = @releaseBundleId`,
		pgx.NamedArgs{"releaseBundleId": releaseBundleID},
	); err != nil {
		return fmt.Errorf("could not replace Component Release facts: %w", err)
	}
	if contract == nil || contract.ComponentV2 == nil {
		return nil
	}
	component := contract.ComponentV2
	artifacts := make([]componentReleaseArtifactFact, 0)
	for _, artifact := range component.Artifacts {
		for _, platform := range artifact.Platforms {
			artifacts = append(artifacts, componentReleaseArtifactFact{artifact: artifact, platform: platform})
		}
	}
	if len(artifacts) > 0 {
		if _, err := db.CopyFrom(
			ctx,
			pgx.Identifier{"componentreleaseartifact"},
			[]string{
				"id", "release_bundle_id", "organization_id", "component_key", "component_version",
				"artifact_key", "artifact_type", "media_type", "manifest_digest", "platform", "platform_digest",
			},
			pgx.CopyFromSlice(len(artifacts), func(i int) ([]any, error) {
				fact := artifacts[i]
				return []any{
					uuid.New(), releaseBundleID, organizationID, component.ComponentKey, component.Version,
					fact.artifact.Key, fact.artifact.Type, fact.artifact.MediaType, fact.artifact.Digest,
					fact.platform.Platform, fact.platform.Digest,
				}, nil
			}),
		); err != nil {
			return fmt.Errorf("could not insert Component Release artifacts: %w", err)
		}
	}
	evidence := make([]componentReleaseEvidenceFact, 0)
	for _, reference := range component.Evidence.Provenance {
		evidence = append(evidence, componentReleaseEvidenceFact{kind: "provenance", reference: reference})
	}
	for _, reference := range component.Evidence.SBOM {
		evidence = append(evidence, componentReleaseEvidenceFact{kind: "sbom", reference: reference})
	}
	for _, reference := range component.Evidence.Signatures {
		evidence = append(evidence, componentReleaseEvidenceFact{kind: "signature", reference: reference})
	}
	for _, reference := range component.Evidence.Tests {
		evidence = append(evidence, componentReleaseEvidenceFact{kind: "test", reference: reference})
	}
	if len(evidence) > 0 {
		if _, err := db.CopyFrom(
			ctx,
			pgx.Identifier{"componentreleaseevidence"},
			[]string{"id", "release_bundle_id", "organization_id", "evidence_type", "reference"},
			pgx.CopyFromSlice(len(evidence), func(i int) ([]any, error) {
				fact := evidence[i]
				return []any{uuid.New(), releaseBundleID, organizationID, fact.kind, fact.reference}, nil
			}),
		); err != nil {
			return fmt.Errorf("could not insert Component Release evidence: %w", err)
		}
	}
	capabilityCount := len(component.Provides) + len(component.Requires)
	if capabilityCount > 0 {
		if _, err := db.CopyFrom(
			ctx,
			pgx.Identifier{"componentreleasecapability"},
			[]string{
				"id", "release_bundle_id", "organization_id", "direction", "name", "version_or_range",
				"resolution_stage", "allowed_modes",
			},
			pgx.CopyFromSlice(capabilityCount, func(i int) ([]any, error) {
				if i < len(component.Provides) {
					capability := component.Provides[i]
					return []any{
						uuid.New(), releaseBundleID, organizationID, "provides", capability.Name,
						capability.Version, "", []string{},
					}, nil
				}
				capability := component.Requires[i-len(component.Provides)]
				return []any{
					uuid.New(), releaseBundleID, organizationID, "requires", capability.Name,
					capability.Range, capability.ResolutionStage, capability.AllowedModes,
				}, nil
			}),
		); err != nil {
			return fmt.Errorf("could not insert Component Release capabilities: %w", err)
		}
	}
	if len(component.Migrations) > 0 {
		if _, err := db.CopyFrom(
			ctx,
			pgx.Identifier{"componentreleasemigrationdeclaration"},
			[]string{
				"id", "release_bundle_id", "organization_id", "key", "migration_type", "sort_order",
				"compatibility", "failure_policy", "description",
			},
			pgx.CopyFromSlice(len(component.Migrations), func(i int) ([]any, error) {
				migration := component.Migrations[i]
				return []any{
					uuid.New(), releaseBundleID, organizationID, migration.Key, migration.Type, migration.Order,
					migration.Compatibility, migration.FailurePolicy, migration.Description,
				}, nil
			}),
		); err != nil {
			return fmt.Errorf("could not insert Component Release migrations: %w", err)
		}
	}
	return nil
}

func ensureReleaseBundleReferences(ctx context.Context, bundle *types.ReleaseBundle) error {
	if err := ensureReleaseBundleParentReferences(ctx, *bundle); err != nil {
		return err
	}
	if err := ensureReleaseBundleProcessSnapshotReference(ctx, bundle); err != nil {
		return err
	}
	for _, component := range bundle.Components {
		if err := ensureReleaseBundleComponentReferences(ctx, *bundle, component); err != nil {
			return err
		}
	}
	return nil
}

func ensureReleaseBundleProcessSnapshotReference(ctx context.Context, bundle *types.ReleaseBundle) error {
	if bundle.DeploymentProcessRevisionID != nil {
		snapshot, err := ensureProcessSnapshotForRevision(
			ctx,
			bundle.OrganizationID,
			bundle.ApplicationID,
			*bundle.DeploymentProcessRevisionID,
		)
		if err != nil {
			return err
		}
		bundle.ProcessSnapshotID = &snapshot.ID
		return nil
	}
	if bundle.ProcessSnapshotID == nil {
		return nil
	}
	snapshot, err := getProcessSnapshot(ctx, *bundle.ProcessSnapshotID, bundle.OrganizationID)
	if err != nil {
		return err
	}
	if snapshot.ApplicationID != bundle.ApplicationID {
		return apierrors.ErrNotFound
	}
	return nil
}

func ensureReleaseBundleParentReferences(ctx context.Context, bundle types.ReleaseBundle) error {
	db := internalctx.GetDb(ctx)
	var exists bool
	err := db.QueryRow(ctx,
		`SELECT
			EXISTS (
				SELECT 1
				FROM Application
				WHERE id = @applicationId AND organization_id = @organizationId
			)
			AND EXISTS (
				SELECT 1
				FROM Channel
				WHERE id = @channelId
					AND organization_id = @organizationId
					AND application_id = @applicationId
			)`,
		pgx.NamedArgs{
			"organizationId": bundle.OrganizationID,
			"applicationId":  bundle.ApplicationID,
			"channelId":      bundle.ChannelID,
		},
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("could not validate ReleaseBundle references: %w", err)
	}
	if !exists {
		return apierrors.ErrNotFound
	}
	return nil
}

func ensureReleaseBundleComponentReferences(
	ctx context.Context,
	bundle types.ReleaseBundle,
	component types.ReleaseBundleComponent,
) error {
	switch component.Type {
	case types.ReleaseBundleComponentTypeApplicationVersion:
		return ensureReleaseBundleApplicationVersionReference(ctx, bundle, component)
	case types.ReleaseBundleComponentTypeChildReleaseBundle:
		return ensureReleaseBundleChildReference(ctx, bundle, component)
	case types.ReleaseBundleComponentTypeOCIImage, types.ReleaseBundleComponentTypeOCIArtifact:
		if !releasebundles.IsSHA256Digest(component.Digest) {
			return apierrors.NewBadRequest("component digest must be a sha256 digest")
		}
	default:
		return nil
	}
	return nil
}

func ensureReleaseBundleApplicationVersionReference(
	ctx context.Context,
	bundle types.ReleaseBundle,
	component types.ReleaseBundleComponent,
) error {
	if component.ApplicationVersionID == nil {
		return apierrors.ErrNotFound
	}
	db := internalctx.GetDb(ctx)
	var exists bool
	err := db.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1
			FROM ApplicationVersion av
			JOIN Application a ON a.id = av.application_id
			WHERE av.id = @applicationVersionId
				AND a.id = @applicationId
				AND a.organization_id = @organizationId
		)`,
		pgx.NamedArgs{
			"organizationId":       bundle.OrganizationID,
			"applicationId":        bundle.ApplicationID,
			"applicationVersionId": *component.ApplicationVersionID,
		},
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("could not validate ReleaseBundle component references: %w", err)
	}
	if !exists {
		return apierrors.ErrNotFound
	}
	return nil
}

func ensureReleaseBundleChildReference(
	ctx context.Context,
	bundle types.ReleaseBundle,
	component types.ReleaseBundleComponent,
) error {
	if component.ChildReleaseBundleID == nil || *component.ChildReleaseBundleID == bundle.ID {
		return apierrors.ErrNotFound
	}
	db := internalctx.GetDb(ctx)
	var exists bool
	err := db.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1
			FROM ReleaseBundle
			WHERE id = @childReleaseBundleId
				AND organization_id = @organizationId
		)`,
		pgx.NamedArgs{
			"organizationId":       bundle.OrganizationID,
			"childReleaseBundleId": *component.ChildReleaseBundleID,
		},
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("could not validate ReleaseBundle child reference: %w", err)
	}
	if !exists {
		return apierrors.ErrNotFound
	}
	return nil
}

func mapReleaseBundleWriteError(action string, err error) error {
	if isProtectedReferenceViolation(err) {
		return fmt.Errorf("could not %s ReleaseBundle: %w", action, apierrors.ErrConflict)
	}
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		switch pgError.Code {
		case pgerrcode.UniqueViolation:
			return fmt.Errorf("could not %s ReleaseBundle: %w", action, apierrors.ErrAlreadyExists)
		}
	}
	return fmt.Errorf("could not %s ReleaseBundle: %w", action, err)
}
