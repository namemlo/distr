package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/deploymentregistry"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func CreateRegistryImportPreview(
	ctx context.Context,
	request types.RegistryImportRequest,
	preview *types.RegistryImportPreview,
) error {
	if preview == nil {
		return apierrors.NewBadRequest("registry import preview is required")
	}
	canonicalPreview, err := deploymentregistry.PreviewImport(ctx, request)
	if err != nil {
		return apierrors.NewBadRequest(err.Error())
	}
	if preview.PreviewChecksum != canonicalPreview.PreviewChecksum {
		return apierrors.NewBadRequest("registry import preview is not canonical")
	}
	request = deploymentregistry.NormalizeImportRequest(request)
	*preview = *canonicalPreview
	return RunTx(ctx, func(txCtx context.Context) error {
		if err := validateRegistryImportReferences(txCtx, request.OrganizationID, preview.Roots); err != nil {
			return err
		}
		counts, _ := json.Marshal(preview.Counts)
		diff, _ := json.Marshal(preview.Diff)
		omissions, _ := json.Marshal(preview.Omissions)
		diagnostics, _ := json.Marshal(preview.Diagnostics)
		parameters, _ := json.Marshal(request.Parameters)
		err := internalctx.GetDb(txCtx).QueryRow(txCtx, `
			INSERT INTO RegistryImport (
				organization_id, source_kind, tool_name, tool_version, source_commit,
				canonical_parameters, evidence_reference, evidence_checksum,
				preview_checksum, counts, diff, omissions, diagnostics, diagnostics_truncated,
				state, actor_useraccount_id
			) VALUES (
				@organizationID, @sourceKind, @toolName, @toolVersion, @sourceCommit,
				@parameters, @evidenceReference, @evidenceChecksum,
				@previewChecksum, @counts, @diff, @omissions, @diagnostics, @diagnosticsTruncated,
				'previewed', @actorID
			) RETURNING id`,
			pgx.NamedArgs{
				"organizationID": request.OrganizationID, "sourceKind": request.SourceKind,
				"toolName": request.ToolName, "toolVersion": request.ToolVersion,
				"sourceCommit": nullableImportText(request.SourceCommit), "parameters": parameters,
				"evidenceReference": request.EvidenceReference, "evidenceChecksum": request.EvidenceChecksum,
				"previewChecksum": preview.PreviewChecksum, "counts": counts, "diff": diff,
				"omissions":   omissions,
				"diagnostics": diagnostics, "diagnosticsTruncated": preview.DiagnosticsTruncated,
				"actorID": request.ActorID,
			},
		).Scan(&preview.ID)
		if err != nil {
			return fmt.Errorf("create registry import preview: %w", err)
		}

		rootIDs := make(map[string]uuid.UUID, len(preview.Roots))
		rootRows := make([][]any, 0, len(preview.Roots))
		for _, root := range preview.Roots {
			id := uuid.New()
			rootIDs[root.Key] = id
			candidateChecksum, err := importCandidateChecksum(root)
			if err != nil {
				return fmt.Errorf("checksum registry import root %q: %w", root.Key, err)
			}
			rootRows = append(rootRows, []any{
				id, request.OrganizationID, preview.ID, root.Key, root.Name, root.DeliveryModel,
				root.CustomerOrganizationID, nullableImportUUID(root.DeploymentTargetID),
				nullableImportUUID(root.EnvironmentID), root.SubscriberCustomerOrganizationIDs,
				root.PhysicalIdentity, candidateChecksum,
			})
		}
		if len(rootRows) > 0 {
			_, err = internalctx.GetDb(txCtx).CopyFrom(
				txCtx, pgx.Identifier{"registryimportroot"},
				[]string{
					"id", "organization_id", "registry_import_id", "root_key", "name",
					"delivery_model", "customer_organization_id", "deployment_target_id",
					"environment_id", "subscriber_customer_organization_ids",
					"physical_identity", "candidate_checksum",
				},
				pgx.CopyFromRows(rootRows),
			)
			if err != nil {
				return fmt.Errorf("store registry import roots: %w", err)
			}
		}

		placementRows := make([][]any, 0, preview.Counts.DiscoveredPlacements)
		decisionRows := make([][]any, 0, len(preview.Roots))
		for _, root := range preview.Roots {
			rootID := rootIDs[root.Key]
			for _, placement := range root.Placements {
				candidateChecksum, err := importCandidateChecksum(placement)
				if err != nil {
					return fmt.Errorf(
						"checksum registry import placement %q/%q: %w",
						root.Key, placement.ComponentKey, err,
					)
				}
				placementRows = append(placementRows, []any{
					uuid.New(), request.OrganizationID, preview.ID, rootID,
					placement.ComponentKey, placement.PhysicalName, placement.ConfigNamespace,
					placement.DatabaseBoundary, placement.HealthAdapter,
					nullableImportText(placement.RenamedFrom), candidateChecksum,
				})
			}
			decisionRows = append(decisionRows, []any{
				uuid.New(), request.OrganizationID, preview.ID, rootID,
				1, root.Classification, request.ActorID,
			})
		}
		if len(placementRows) > 0 {
			_, err = internalctx.GetDb(txCtx).CopyFrom(
				txCtx, pgx.Identifier{"registryimportplacement"},
				[]string{
					"id", "organization_id", "registry_import_id", "registry_import_root_id",
					"component_key", "physical_name", "config_namespace",
					"database_boundary", "health_adapter", "renamed_from", "candidate_checksum",
				},
				pgx.CopyFromRows(placementRows),
			)
			if err != nil {
				return fmt.Errorf("store registry import placements: %w", err)
			}
		}
		if len(decisionRows) > 0 {
			_, err = internalctx.GetDb(txCtx).CopyFrom(
				txCtx, pgx.Identifier{"registryimportdecision"},
				[]string{
					"id", "organization_id", "registry_import_id", "registry_import_root_id",
					"decision_ordinal", "classification", "actor_useraccount_id",
				},
				pgx.CopyFromRows(decisionRows),
			)
			if err != nil {
				return fmt.Errorf("store registry import decisions: %w", err)
			}
		}
		return nil
	})
}

func GetRegistryImportPreview(
	ctx context.Context, organizationID, importID uuid.UUID,
) (*types.RegistryImportPreview, error) {
	var preview types.RegistryImportPreview
	var counts, diff, omissions, diagnostics []byte
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT id, preview_checksum, counts, diff, omissions, diagnostics, diagnostics_truncated
		FROM RegistryImport
		WHERE id = @importID AND organization_id = @organizationID`,
		pgx.NamedArgs{"importID": importID, "organizationID": organizationID},
	).Scan(
		&preview.ID, &preview.PreviewChecksum, &counts, &diff, &omissions,
		&diagnostics, &preview.DiagnosticsTruncated,
	)
	if err == pgx.ErrNoRows {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get registry import: %w", err)
	}
	if err = json.Unmarshal(counts, &preview.Counts); err != nil {
		return nil, err
	}
	if err = json.Unmarshal(diff, &preview.Diff); err != nil {
		return nil, err
	}
	if err = json.Unmarshal(omissions, &preview.Omissions); err != nil {
		return nil, err
	}
	if err = json.Unmarshal(diagnostics, &preview.Diagnostics); err != nil {
		return nil, err
	}

	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT r.id, r.root_key, r.name, r.delivery_model, r.customer_organization_id,
		       r.deployment_target_id, r.environment_id, r.subscriber_customer_organization_ids,
		       r.physical_identity,
		       latest.classification
		FROM RegistryImportRoot r
		JOIN LATERAL (
		  SELECT classification FROM RegistryImportDecision d
		  WHERE d.registry_import_root_id = r.id
		    AND d.registry_import_id = r.registry_import_id
		    AND d.organization_id = r.organization_id
		  ORDER BY d.decision_ordinal DESC LIMIT 1
		) latest ON true
		WHERE r.registry_import_id = @importID AND r.organization_id = @organizationID
		ORDER BY r.root_key, r.id`,
		pgx.NamedArgs{"importID": importID, "organizationID": organizationID},
	)
	if err != nil {
		return nil, fmt.Errorf("list registry import roots: %w", err)
	}
	rootIDs := make(map[uuid.UUID]int)
	for rows.Next() {
		var rootID uuid.UUID
		var root types.RegistryImportCandidateRoot
		var deploymentTargetID, environmentID *uuid.UUID
		if err = rows.Scan(&rootID, &root.Key, &root.Name, &root.DeliveryModel,
			&root.CustomerOrganizationID, &deploymentTargetID, &environmentID,
			&root.SubscriberCustomerOrganizationIDs, &root.PhysicalIdentity, &root.Classification); err != nil {
			rows.Close()
			return nil, err
		}
		if deploymentTargetID != nil {
			root.DeploymentTargetID = *deploymentTargetID
		}
		if environmentID != nil {
			root.EnvironmentID = *environmentID
		}
		rootIDs[rootID] = len(preview.Roots)
		preview.Roots = append(preview.Roots, root)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("read registry import roots: %w", err)
	}
	rows, err = internalctx.GetDb(ctx).Query(ctx, `
		SELECT registry_import_root_id, component_key, physical_name, config_namespace,
		       database_boundary, health_adapter, COALESCE(renamed_from, '')
		FROM RegistryImportPlacement
		WHERE registry_import_id = @importID AND organization_id = @organizationID
		ORDER BY registry_import_root_id, component_key, physical_name, id`,
		pgx.NamedArgs{"importID": importID, "organizationID": organizationID},
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var rootID uuid.UUID
		var placement types.RegistryImportCandidatePlacement
		if err = rows.Scan(&rootID, &placement.ComponentKey, &placement.PhysicalName,
			&placement.ConfigNamespace, &placement.DatabaseBoundary, &placement.HealthAdapter,
			&placement.RenamedFrom); err != nil {
			return nil, err
		}
		index, ok := rootIDs[rootID]
		if !ok {
			return nil, fmt.Errorf("registry import placement references unknown root")
		}
		preview.Roots[index].Placements = append(preview.Roots[index].Placements, placement)
	}
	return &preview, rows.Err()
}

func ClassifyImportRoot(
	ctx context.Context, organizationID uuid.UUID, decision types.RegistryImportDecision,
) error {
	if !decision.Classification.IsValid() {
		return apierrors.NewBadRequest("classification is invalid")
	}
	decision.RootKey = strings.ToLower(strings.TrimSpace(decision.RootKey))
	return RunTx(ctx, func(txCtx context.Context) error {
		var state string
		err := internalctx.GetDb(txCtx).QueryRow(txCtx, `
			SELECT state
			FROM RegistryImport
			WHERE id = @importID AND organization_id = @organizationID
			FOR UPDATE`,
			pgx.NamedArgs{"importID": decision.ImportID, "organizationID": organizationID},
		).Scan(&state)
		if err == pgx.ErrNoRows {
			return apierrors.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock registry import for classification: %w", err)
		}
		if state != "previewed" {
			return apierrors.NewConflict("registry import is no longer classifiable")
		}
		tag, err := internalctx.GetDb(txCtx).Exec(txCtx, `
			INSERT INTO RegistryImportDecision (
				organization_id, registry_import_id, registry_import_root_id,
				decision_ordinal, classification, actor_useraccount_id
			)
			SELECT r.organization_id, r.registry_import_id, r.id,
			       COALESCE((
			         SELECT max(existing.decision_ordinal)
			         FROM RegistryImportDecision existing
			         WHERE existing.registry_import_root_id = r.id
			           AND existing.registry_import_id = r.registry_import_id
			           AND existing.organization_id = r.organization_id
			       ), 0) + 1,
			       @classification, @actorID
			FROM RegistryImportRoot r
			WHERE r.registry_import_id = @importID
			  AND r.organization_id = @organizationID
			  AND r.root_key = @rootKey`,
			pgx.NamedArgs{
				"organizationID": organizationID, "importID": decision.ImportID,
				"rootKey": decision.RootKey, "classification": decision.Classification,
				"actorID": decision.ActorID,
			},
		)
		if err != nil {
			return fmt.Errorf("classify registry import root: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return apierrors.ErrNotFound
		}
		return nil
	})
}

func ApplyImport(
	ctx context.Context, organizationID, importID uuid.UUID, expectedPreviewChecksum string,
	actorID uuid.UUID,
) (*types.RegistryImportResult, error) {
	claimID := uuid.New()
	result, claimed, err := claimRegistryImport(
		ctx, organizationID, importID, expectedPreviewChecksum, claimID,
	)
	if err != nil || !claimed {
		return result, err
	}
	preview, err := GetRegistryImportPreview(ctx, organizationID, importID)
	if err != nil {
		_ = failRegistryImportClaim(ctx, organizationID, importID, claimID)
		return nil, err
	}
	if err = validateRegistryImportApplyability(*preview); err != nil {
		if releaseErr := releaseRegistryImportClaim(ctx, organizationID, importID, claimID); releaseErr != nil {
			return nil, releaseErr
		}
		return nil, err
	}
	err = RunTx(ctx, func(txCtx context.Context) error {
		if err := lockRegistryImportCheckpoint(
			txCtx, organizationID, importID, claimID, result.Checkpoint,
		); err != nil {
			return err
		}
		if err := validateRegistryImportReferences(
			txCtx, organizationID, preview.Roots,
		); err != nil {
			return err
		}
		if err := validateRegistryImportRenameAliases(
			txCtx, organizationID, preview.Roots, preview.Diff,
		); err != nil {
			return err
		}
		return validateRegistryImportExistingTopology(
			txCtx, organizationID, preview.Roots, preview.Diff,
		)
	})
	if err != nil {
		if result.Checkpoint == 0 {
			_ = releaseRegistryImportClaim(ctx, organizationID, importID, claimID)
		} else {
			_ = failRegistryImportClaim(ctx, organizationID, importID, claimID)
		}
		return nil, fmt.Errorf("preflight registry import apply: %w", err)
	}
	for index := result.Checkpoint; index < len(preview.Roots); index++ {
		root := preview.Roots[index]
		if err = RunTx(ctx, func(txCtx context.Context) error {
			if err := lockRegistryImportCheckpoint(
				txCtx, organizationID, importID, claimID, index,
			); err != nil {
				return err
			}
			if err := applyRegistryImportRootChanges(
				txCtx, organizationID, root, preview.Diff,
			); err != nil {
				return err
			}
			return advanceRegistryImportCheckpoint(
				txCtx, organizationID, importID, claimID, index, index+1,
			)
		}); err != nil {
			_ = failRegistryImportClaim(ctx, organizationID, importID, claimID)
			return nil, fmt.Errorf("apply registry import checkpoint %d: %w", index+1, err)
		}
		result.Checkpoint = index + 1
	}
	finalCheckpoint := len(preview.Roots)
	if len(preview.Diff.Retirements) > 0 {
		finalCheckpoint++
		if result.Checkpoint < finalCheckpoint {
			if err = RunTx(ctx, func(txCtx context.Context) error {
				if err := lockRegistryImportCheckpoint(
					txCtx, organizationID, importID, claimID, result.Checkpoint,
				); err != nil {
					return err
				}
				if err := applyRegistryImportRetirements(
					txCtx, organizationID, preview.Diff.Retirements,
				); err != nil {
					return err
				}
				return advanceRegistryImportCheckpoint(
					txCtx, organizationID, importID, claimID, result.Checkpoint, finalCheckpoint,
				)
			}); err != nil {
				_ = failRegistryImportClaim(ctx, organizationID, importID, claimID)
				return nil, fmt.Errorf("apply registry import retirement checkpoint: %w", err)
			}
			result.Checkpoint = finalCheckpoint
		}
	}
	tag, err := internalctx.GetDb(ctx).Exec(ctx, `
		UPDATE RegistryImport SET state = 'applied', applied_at = now(), updated_at = now(),
		  applied_by_useraccount_id = @actorID, apply_claim_id = NULL, apply_claimed_at = NULL
		WHERE id = @importID AND organization_id = @organizationID
		  AND state = 'applying' AND apply_claim_id = @claimID
		  AND last_committed_checkpoint = @checkpoint`,
		pgx.NamedArgs{
			"checkpoint": finalCheckpoint, "importID": importID,
			"organizationID": organizationID, "actorID": actorID, "claimID": claimID,
		},
	)
	if err != nil {
		_ = failRegistryImportClaim(ctx, organizationID, importID, claimID)
		return nil, err
	}
	if tag.RowsAffected() != 1 {
		return nil, apierrors.NewConflict("registry import state changed")
	}
	result.State, result.Applied, result.Checkpoint = "applied", true, finalCheckpoint
	return result, nil
}

func validateRegistryImportApplyability(preview types.RegistryImportPreview) error {
	coverage := deploymentregistry.RegistryCoverageWithOmissions(
		preview.Roots, preview.Omissions,
	)
	if preview.Counts.OmittedPlacements != len(preview.Omissions) ||
		!coverage.Complete || len(preview.Diff.Conflicts) != 0 {
		return apierrors.NewConflict(
			"registry import has unresolved classifications, omissions, or conflicts",
		)
	}
	return nil
}

func claimRegistryImport(
	ctx context.Context,
	organizationID, importID uuid.UUID,
	expectedPreviewChecksum string,
	claimID uuid.UUID,
) (*types.RegistryImportResult, bool, error) {
	var result types.RegistryImportResult
	claimed := false
	err := RunTx(ctx, func(txCtx context.Context) error {
		var counts []byte
		var previousClaimID *uuid.UUID
		var claimActive bool
		err := internalctx.GetDb(txCtx).QueryRow(txCtx, `
			SELECT id, preview_checksum, state, counts, last_committed_checkpoint,
			       apply_claim_id,
			       COALESCE(
			         apply_claimed_at > clock_timestamp() - interval '5 minutes',
			         false
			       )
			FROM RegistryImport
			WHERE id = @importID AND organization_id = @organizationID
			FOR UPDATE`,
			pgx.NamedArgs{"importID": importID, "organizationID": organizationID},
		).Scan(
			&result.ID, &result.PreviewChecksum, &result.State, &counts, &result.Checkpoint,
			&previousClaimID, &claimActive,
		)
		if err == pgx.ErrNoRows {
			return apierrors.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock registry import for apply: %w", err)
		}
		if result.PreviewChecksum != expectedPreviewChecksum {
			return apierrors.NewConflict("registry import preview checksum is stale")
		}
		if err := json.Unmarshal(counts, &result.Counts); err != nil {
			return fmt.Errorf("decode registry import counts: %w", err)
		}
		if result.State == "applied" {
			result.Applied = false
			return nil
		}
		if result.State == "applying" && claimActive {
			return apierrors.NewConflict("registry import is already applying")
		}
		if result.State != "previewed" && result.State != "failed" && result.State != "applying" {
			return apierrors.NewConflict("registry import state cannot be applied")
		}
		tag, err := internalctx.GetDb(txCtx).Exec(txCtx, `
			UPDATE RegistryImport
			SET state = 'applying', apply_claim_id = @claimID,
			    apply_claimed_at = clock_timestamp(), updated_at = clock_timestamp()
			WHERE id = @importID AND organization_id = @organizationID
			  AND state = @previousState
			  AND last_committed_checkpoint = @checkpoint
			  AND apply_claim_id IS NOT DISTINCT FROM @previousClaimID`,
			pgx.NamedArgs{
				"claimID": claimID, "importID": importID, "organizationID": organizationID,
				"previousState": result.State, "checkpoint": result.Checkpoint,
				"previousClaimID": previousClaimID,
			},
		)
		if err != nil {
			return fmt.Errorf("claim registry import: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return apierrors.NewConflict("registry import claim changed")
		}
		result.State = "applying"
		claimed = true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return &result, claimed, nil
}

func lockRegistryImportCheckpoint(
	ctx context.Context,
	organizationID, importID, claimID uuid.UUID,
	expectedCheckpoint int,
) error {
	tag, err := internalctx.GetDb(ctx).Exec(ctx, `
		UPDATE RegistryImport
		SET apply_claimed_at = clock_timestamp(), updated_at = clock_timestamp()
		WHERE id = @importID AND organization_id = @organizationID
		  AND state = 'applying' AND apply_claim_id = @claimID
		  AND last_committed_checkpoint = @expectedCheckpoint`,
		pgx.NamedArgs{
			"organizationID": organizationID, "importID": importID,
			"claimID": claimID, "expectedCheckpoint": expectedCheckpoint,
		},
	)
	if err != nil {
		return fmt.Errorf("renew registry import claim: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return apierrors.NewConflict("registry import checkpoint ownership changed")
	}
	return nil
}

func advanceRegistryImportCheckpoint(
	ctx context.Context,
	organizationID, importID, claimID uuid.UUID,
	expectedCheckpoint, nextCheckpoint int,
) error {
	tag, err := internalctx.GetDb(ctx).Exec(ctx, `
		UPDATE RegistryImport
		SET last_committed_checkpoint = @nextCheckpoint,
		    apply_claimed_at = clock_timestamp(), updated_at = clock_timestamp()
		WHERE id = @importID AND organization_id = @organizationID
		  AND state = 'applying' AND apply_claim_id = @claimID
		  AND last_committed_checkpoint = @expectedCheckpoint`,
		pgx.NamedArgs{
			"organizationID": organizationID, "importID": importID, "claimID": claimID,
			"expectedCheckpoint": expectedCheckpoint, "nextCheckpoint": nextCheckpoint,
		},
	)
	if err != nil {
		return fmt.Errorf("advance registry import checkpoint: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return apierrors.NewConflict("registry import checkpoint changed")
	}
	return nil
}

func releaseRegistryImportClaim(
	ctx context.Context,
	organizationID, importID, claimID uuid.UUID,
) error {
	tag, err := internalctx.GetDb(ctx).Exec(ctx, `
		UPDATE RegistryImport
		SET state = 'previewed', apply_claim_id = NULL, apply_claimed_at = NULL, updated_at = now()
		WHERE id = @importID AND organization_id = @organizationID
		  AND state = 'applying' AND apply_claim_id = @claimID
		  AND last_committed_checkpoint = 0`,
		pgx.NamedArgs{
			"organizationID": organizationID, "importID": importID, "claimID": claimID,
		},
	)
	if err != nil {
		return fmt.Errorf("release registry import claim: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return apierrors.NewConflict("registry import claim changed")
	}
	return nil
}

func failRegistryImportClaim(
	ctx context.Context,
	organizationID, importID, claimID uuid.UUID,
) error {
	tag, err := internalctx.GetDb(ctx).Exec(ctx, `
		UPDATE RegistryImport
		SET state = 'failed', apply_claim_id = NULL, apply_claimed_at = NULL, updated_at = now()
		WHERE id = @importID AND organization_id = @organizationID
		  AND state = 'applying' AND apply_claim_id = @claimID`,
		pgx.NamedArgs{
			"organizationID": organizationID, "importID": importID, "claimID": claimID,
		},
	)
	if err != nil {
		return fmt.Errorf("fail registry import claim: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return apierrors.NewConflict("registry import claim changed")
	}
	return nil
}

func RegistryImportBaseline(
	ctx context.Context, organizationID uuid.UUID,
) ([]types.RegistryImportCandidateRoot, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT s.key, s.name, s.delivery_model, s.customer_organization_id,
		       u.deployment_target_id, a.environment_id, u.physical_identity,
		       d.key, i.physical_name, i.config_namespace, i.database_boundary, i.health_adapter,
		       ARRAY(
		         SELECT us.customer_organization_id FROM DeploymentUnitSubscriber us
		         WHERE us.organization_id = u.organization_id AND us.deployment_unit_id = u.id
		           AND us.retired_at IS NULL ORDER BY us.customer_organization_id
		       )
		FROM DeploymentScope s
		JOIN DeploymentUnit u ON u.deployment_scope_id = s.id
		  AND u.organization_id = s.organization_id AND u.retired_at IS NULL
		JOIN TargetEnvironmentAssignment a ON a.id = u.target_environment_assignment_id
		  AND a.organization_id = u.organization_id
		JOIN ComponentInstance i ON i.deployment_unit_id = u.id
		  AND i.organization_id = u.organization_id AND i.retired_at IS NULL
		JOIN ComponentDefinition d ON d.id = i.component_definition_id
		  AND d.organization_id = i.organization_id AND d.retired_at IS NULL
		WHERE s.organization_id = @organizationID AND s.retired_at IS NULL
		ORDER BY s.key, d.key, i.physical_name`,
		pgx.NamedArgs{"organizationID": organizationID},
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var roots []types.RegistryImportCandidateRoot
	index := map[string]int{}
	for rows.Next() {
		var root types.RegistryImportCandidateRoot
		var placement types.RegistryImportCandidatePlacement
		if err = rows.Scan(&root.Key, &root.Name, &root.DeliveryModel, &root.CustomerOrganizationID,
			&root.DeploymentTargetID, &root.EnvironmentID, &root.PhysicalIdentity,
			&placement.ComponentKey, &placement.PhysicalName, &placement.ConfigNamespace,
			&placement.DatabaseBoundary, &placement.HealthAdapter,
			&root.SubscriberCustomerOrganizationIDs); err != nil {
			return nil, err
		}
		root.Classification = registryImportClassification(root.DeliveryModel)
		position, exists := index[root.Key]
		if !exists {
			position = len(roots)
			index[root.Key] = position
			roots = append(roots, root)
		}
		roots[position].Placements = append(roots[position].Placements, placement)
	}
	return roots, rows.Err()
}

func applyRegistryImportRoot(
	ctx context.Context, organizationID uuid.UUID, root types.RegistryImportCandidateRoot,
) error {
	model, state, create, err := deploymentregistry.ClassificationResult(root.Classification, root.DeliveryModel)
	if err != nil || !create {
		return err
	}
	if root.DeploymentTargetID == uuid.Nil || root.EnvironmentID == uuid.Nil {
		return apierrors.NewBadRequest("actionable root requires deploymentTargetId and environmentId")
	}
	if model == types.DeliveryModelDedicated && root.CustomerOrganizationID == nil {
		return apierrors.NewBadRequest("standard root requires customerOrganizationId")
	}
	scope := types.DeploymentScope{
		OrganizationID: organizationID, CustomerOrganizationID: root.CustomerOrganizationID,
		Key: root.Key, Name: root.Name, DeliveryModel: model, ManagementState: state,
	}
	if err = CreateDeploymentScope(ctx, &scope); err != nil {
		return err
	}
	assignment, err := findOrCreateRegistryImportAssignment(
		ctx, organizationID, root.DeploymentTargetID, root.EnvironmentID,
	)
	if err != nil {
		return err
	}
	subscribers := make([]types.DeploymentUnitSubscriber, len(root.SubscriberCustomerOrganizationIDs))
	for index, customerID := range root.SubscriberCustomerOrganizationIDs {
		subscribers[index] = types.DeploymentUnitSubscriber{
			OrganizationID: organizationID, CustomerOrganizationID: customerID,
		}
	}
	unit := types.DeploymentUnit{
		OrganizationID: organizationID, DeploymentScopeID: scope.ID,
		TargetEnvironmentAssignmentID: assignment.ID, DeploymentTargetID: root.DeploymentTargetID,
		Key: root.Key, Name: root.Name, PhysicalIdentity: root.PhysicalIdentity,
		ManagementState: state, SubscriberSetChecksum: deploymentregistry.SubscriberSetChecksum(subscribers),
	}
	if err = CreateDeploymentUnitWithSubscribers(ctx, &unit, subscribers); err != nil {
		return err
	}
	for _, placement := range root.Placements {
		if err = createRegistryImportPlacement(
			ctx, organizationID, unit.ID, state, placement,
		); err != nil {
			return err
		}
	}
	return nil
}

func rootHasRegistryImportChangeKind(
	diff types.RegistryImportDiff,
	rootKey, kind string,
) bool {
	for _, change := range diff.Creates {
		if change.RootKey == rootKey && change.Kind == kind {
			return true
		}
	}
	return false
}

func applyRegistryImportRootChanges(
	ctx context.Context,
	organizationID uuid.UUID,
	root types.RegistryImportCandidateRoot,
	diff types.RegistryImportDiff,
) error {
	if rootHasRegistryImportChangeKind(diff, root.Key, "create_root") {
		return applyRegistryImportRoot(ctx, organizationID, root)
	}
	for _, change := range diff.Creates {
		if change.RootKey != root.Key || change.Kind != "create_placement" {
			continue
		}
		placement, exists := registryImportPlacementForChange(root, change)
		if !exists {
			return apierrors.NewConflict("registry import create placement is missing")
		}
		unitID, state, err := registryImportActiveUnit(ctx, organizationID, root)
		if err != nil {
			return err
		}
		if err := createRegistryImportPlacement(
			ctx, organizationID, unitID, state, placement,
		); err != nil {
			return err
		}
	}
	return applyRegistryImportRootUpdates(ctx, organizationID, root, diff)
}

func applyRegistryImportRootUpdates(
	ctx context.Context,
	organizationID uuid.UUID,
	root types.RegistryImportCandidateRoot,
	diff types.RegistryImportDiff,
) error {
	for _, change := range diff.Updates {
		if change.RootKey != root.Key ||
			(change.Kind != "rename_placement" && change.Kind != "update_placement") {
			continue
		}
		placement, exists := registryImportPlacementForChange(root, change)
		if !exists {
			return apierrors.NewConflict("registry import update placement is missing")
		}
		currentPhysicalName := placement.PhysicalName
		if change.Kind == "rename_placement" {
			currentPhysicalName = placement.RenamedFrom
		}
		rows, err := internalctx.GetDb(ctx).Query(ctx, `
			SELECT `+componentInstanceOutputExpr+`
			FROM ComponentInstance i
			JOIN ComponentDefinition d ON d.id = i.component_definition_id
			 AND d.organization_id = i.organization_id
			JOIN DeploymentUnit u ON u.id = i.deployment_unit_id
			 AND u.organization_id = i.organization_id
			JOIN DeploymentScope s ON s.id = u.deployment_scope_id
			 AND s.organization_id = u.organization_id
			WHERE i.organization_id = @organizationID
			  AND s.key = @rootKey AND d.key = @componentKey
			  AND lower(i.physical_name) = lower(@currentPhysicalName)
			  AND i.retired_at IS NULL`,
			pgx.NamedArgs{
				"organizationID": organizationID, "rootKey": root.Key,
				"componentKey":        placement.ComponentKey,
				"currentPhysicalName": currentPhysicalName,
			},
		)
		if err != nil {
			return err
		}
		instance, err := pgx.CollectExactlyOneRow(
			rows, pgx.RowToStructByName[types.ComponentInstance],
		)
		if err != nil {
			return apierrors.NewConflict("registry import rename target is ambiguous or missing")
		}
		instance.PhysicalName = placement.PhysicalName
		instance.RenamedFrom = placement.RenamedFrom
		instance.ConfigNamespace = placement.ConfigNamespace
		instance.DatabaseBoundary = placement.DatabaseBoundary
		instance.HealthAdapter = placement.HealthAdapter
		if err = UpdateComponentInstance(ctx, &instance); err != nil {
			return err
		}
	}
	return nil
}

func findOrCreateRegistryImportAssignment(
	ctx context.Context,
	organizationID, deploymentTargetID, environmentID uuid.UUID,
) (*types.TargetEnvironmentAssignment, error) {
	_, err := internalctx.GetDb(ctx).Exec(ctx, `
		SELECT pg_advisory_xact_lock(
		  hashtextextended(@lockKey, 0)
		)`,
		pgx.NamedArgs{"lockKey": organizationID.String() + ":" + deploymentTargetID.String()},
	)
	if err != nil {
		return nil, fmt.Errorf("lock registry import target assignment: %w", err)
	}
	var lockedTargetID uuid.UUID
	err = internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT id
		FROM DeploymentTarget
		WHERE id = @deploymentTargetID AND organization_id = @organizationID
		FOR UPDATE`,
		pgx.NamedArgs{
			"organizationID": organizationID, "deploymentTargetID": deploymentTargetID,
		},
	).Scan(&lockedTargetID)
	if err == pgx.ErrNoRows {
		return nil, apierrors.NewConflict("deployment target changed during import")
	}
	if err != nil {
		return nil, fmt.Errorf("lock registry import deployment target: %w", err)
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+targetEnvironmentAssignmentOutputExpr+`
		FROM TargetEnvironmentAssignment a
		WHERE a.organization_id = @organizationID
		  AND a.deployment_target_id = @deploymentTargetID
		  AND a.environment_id = @environmentID
		  AND a.active_until IS NULL
		ORDER BY a.active_from, a.id`,
		pgx.NamedArgs{
			"organizationID": organizationID, "deploymentTargetID": deploymentTargetID,
			"environmentID": environmentID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("find registry import target assignment: %w", err)
	}
	assignment, err := pgx.CollectExactlyOneRow(
		rows, pgx.RowToStructByName[types.TargetEnvironmentAssignment],
	)
	if err == nil {
		return &assignment, nil
	}
	if err != pgx.ErrNoRows {
		return nil, apierrors.NewConflict("target environment assignment is ambiguous")
	}

	rows, err = internalctx.GetDb(ctx).Query(ctx, `
		INSERT INTO TargetEnvironmentAssignment AS a (
			organization_id, deployment_target_id, environment_id,
			active_from, policy_constraints
		) VALUES (
			@organizationID, @deploymentTargetID, @environmentID, now(), '{}'::jsonb
		)
		RETURNING `+targetEnvironmentAssignmentOutputExpr,
		pgx.NamedArgs{
			"organizationID": organizationID, "deploymentTargetID": deploymentTargetID,
			"environmentID": environmentID,
		},
	)
	if err != nil {
		return nil, mapDeploymentRegistryWriteError(
			"create registry import target assignment", err,
		)
	}
	assignment, err = pgx.CollectExactlyOneRow(
		rows, pgx.RowToStructByName[types.TargetEnvironmentAssignment],
	)
	if err != nil {
		return nil, mapDeploymentRegistryWriteError(
			"read registry import target assignment", err,
		)
	}
	return &assignment, nil
}

func registryImportActiveUnit(
	ctx context.Context,
	organizationID uuid.UUID,
	root types.RegistryImportCandidateRoot,
) (uuid.UUID, types.RegistryManagementState, error) {
	var unitID uuid.UUID
	var state types.RegistryManagementState
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT u.id, u.management_state
		FROM DeploymentUnit u
		JOIN DeploymentScope s ON s.id = u.deployment_scope_id
		  AND s.organization_id = u.organization_id
		JOIN TargetEnvironmentAssignment a ON a.id = u.target_environment_assignment_id
		  AND a.organization_id = u.organization_id
		WHERE u.organization_id = @organizationID
		  AND s.key = @rootKey
		  AND u.key = @rootKey
		  AND u.deployment_target_id = @deploymentTargetID
		  AND a.environment_id = @environmentID
		  AND s.retired_at IS NULL AND u.retired_at IS NULL
		FOR SHARE OF u, s, a`,
		pgx.NamedArgs{
			"organizationID": organizationID, "rootKey": root.Key,
			"deploymentTargetID": root.DeploymentTargetID, "environmentID": root.EnvironmentID,
		},
	).Scan(&unitID, &state)
	if err != nil {
		return uuid.Nil, "", apierrors.NewConflict(
			"registry import root topology changed before placement creation",
		)
	}
	return unitID, state, nil
}

func createRegistryImportPlacement(
	ctx context.Context,
	organizationID, unitID uuid.UUID,
	state types.RegistryManagementState,
	placement types.RegistryImportCandidatePlacement,
) error {
	var definitionID uuid.UUID
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT id FROM ComponentDefinition
		WHERE organization_id = @organizationID AND key = @key AND retired_at IS NULL
		FOR SHARE`,
		pgx.NamedArgs{"organizationID": organizationID, "key": placement.ComponentKey},
	).Scan(&definitionID)
	if err == pgx.ErrNoRows {
		definition := types.ComponentDefinition{
			OrganizationID: organizationID, Key: placement.ComponentKey,
			Name: placement.ComponentKey, ManagementState: state,
		}
		if err = CreateComponentDefinition(ctx, &definition); err != nil {
			return err
		}
		definitionID = definition.ID
	} else if err != nil {
		return err
	}
	instance := types.ComponentInstance{
		OrganizationID: organizationID, DeploymentUnitID: unitID,
		ComponentDefinitionID: definitionID, PhysicalName: placement.PhysicalName,
		ConfigNamespace: placement.ConfigNamespace, DatabaseBoundary: placement.DatabaseBoundary,
		HealthAdapter: placement.HealthAdapter, ManagementState: state,
		RenamedFrom: placement.RenamedFrom,
	}
	return CreateComponentInstance(ctx, &instance)
}

func registryImportPlacementForChange(
	root types.RegistryImportCandidateRoot,
	change types.RegistryImportChange,
) (types.RegistryImportCandidatePlacement, bool) {
	var fallback types.RegistryImportCandidatePlacement
	found := false
	for _, placement := range root.Placements {
		if placement.ComponentKey != change.PlacementKey {
			continue
		}
		if change.PhysicalName != "" &&
			strings.EqualFold(placement.PhysicalName, change.PhysicalName) {
			return placement, true
		}
		if found {
			return types.RegistryImportCandidatePlacement{}, false
		}
		fallback, found = placement, true
	}
	return fallback, found
}

func applyRegistryImportRetirements(
	ctx context.Context,
	organizationID uuid.UUID,
	retirements []types.RegistryImportChange,
) error {
	for _, change := range retirements {
		switch change.Kind {
		case "retire_placement":
			if err := applyRegistryImportPlacementRetirement(
				ctx, organizationID, change,
			); err != nil {
				return err
			}
		case "retire_root":
			if err := applyRegistryImportRootRetirement(
				ctx, organizationID, change.RootKey,
			); err != nil {
				return err
			}
		default:
			return apierrors.NewConflict("unsupported registry import retirement")
		}
	}
	return nil
}

func applyRegistryImportPlacementRetirement(
	ctx context.Context,
	organizationID uuid.UUID,
	change types.RegistryImportChange,
) error {
	tag, err := internalctx.GetDb(ctx).Exec(ctx, `
		UPDATE ComponentInstance i
		SET management_state = 'retired', retired_at = now(), updated_at = now()
		FROM ComponentDefinition d, DeploymentUnit u, DeploymentScope s
		WHERE i.component_definition_id = d.id
		  AND i.deployment_unit_id = u.id
		  AND u.deployment_scope_id = s.id
		  AND i.organization_id = @organizationID
		  AND d.organization_id = i.organization_id
		  AND u.organization_id = i.organization_id
		  AND s.organization_id = i.organization_id
		  AND s.key = @rootKey AND d.key = @componentKey
		  AND lower(i.physical_name) = lower(@physicalName)
		  AND i.retired_at IS NULL`,
		pgx.NamedArgs{
			"organizationID": organizationID, "rootKey": change.RootKey,
			"componentKey": change.PlacementKey, "physicalName": change.PhysicalName,
		},
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return apierrors.NewConflict("registry import retirement target is ambiguous or missing")
	}
	return nil
}

func applyRegistryImportRootRetirement(
	ctx context.Context,
	organizationID uuid.UUID,
	rootKey string,
) error {
	tag, err := internalctx.GetDb(ctx).Exec(ctx, `
		WITH retired_instances AS (
		  UPDATE ComponentInstance i
		  SET management_state = 'retired', retired_at = now(), updated_at = now()
		  FROM DeploymentUnit u, DeploymentScope s
		  WHERE i.deployment_unit_id = u.id AND u.deployment_scope_id = s.id
		    AND i.organization_id = @organizationID
		    AND u.organization_id = i.organization_id
		    AND s.organization_id = i.organization_id
		    AND s.key = @rootKey AND i.retired_at IS NULL
		), retired_units AS (
		  UPDATE DeploymentUnit u
		  SET management_state = 'retired', retired_at = now(), updated_at = now()
		  FROM DeploymentScope s
		  WHERE u.deployment_scope_id = s.id
		    AND u.organization_id = @organizationID
		    AND s.organization_id = u.organization_id
		    AND s.key = @rootKey AND u.retired_at IS NULL
		)
		UPDATE DeploymentScope
		SET management_state = 'retired', retired_at = now(), updated_at = now()
		WHERE organization_id = @organizationID AND key = @rootKey
		  AND retired_at IS NULL`,
		pgx.NamedArgs{"organizationID": organizationID, "rootKey": rootKey},
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return apierrors.NewConflict("registry import root retirement target is missing")
	}
	return nil
}

func registryImportClassification(model types.DeliveryModel) types.ImportClassification {
	switch model {
	case types.DeliveryModelShared:
		return types.ImportClassificationShared
	case types.DeliveryModelExternal:
		return types.ImportClassificationExternal
	default:
		return types.ImportClassificationStandard
	}
}

func CoverageReport(
	ctx context.Context, organizationID, importID uuid.UUID,
) (*types.RegistryCoverageReport, error) {
	preview, err := GetRegistryImportPreview(ctx, organizationID, importID)
	if err != nil {
		return nil, err
	}
	result := deploymentregistry.RegistryCoverageWithOmissions(
		preview.Roots, preview.Omissions,
	)
	result.ImportID = importID
	result.DiscoveredPlacements = preview.Counts.DiscoveredPlacements
	result.Complete = result.UnresolvedRoots == 0 && result.OmittedPlacements == 0 &&
		len(preview.Diff.Conflicts) == 0
	return &result, nil
}

func validateRegistryImportReferences(
	ctx context.Context,
	organizationID uuid.UUID,
	roots []types.RegistryImportCandidateRoot,
) error {
	targetSet := make(map[uuid.UUID]struct{})
	environmentSet := make(map[uuid.UUID]struct{})
	customerSet := make(map[uuid.UUID]struct{})
	for _, root := range roots {
		if root.DeploymentTargetID != uuid.Nil {
			targetSet[root.DeploymentTargetID] = struct{}{}
		}
		if root.EnvironmentID != uuid.Nil {
			environmentSet[root.EnvironmentID] = struct{}{}
		}
		if root.CustomerOrganizationID != nil {
			customerSet[*root.CustomerOrganizationID] = struct{}{}
		}
		for _, customerID := range root.SubscriberCustomerOrganizationIDs {
			customerSet[customerID] = struct{}{}
		}
	}
	targetIDs := registryImportUUIDSet(targetSet)
	environmentIDs := registryImportUUIDSet(environmentSet)
	customerIDs := registryImportUUIDSet(customerSet)
	var targetsValid, environmentsValid, customersValid bool
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT
		  NOT EXISTS (
		    SELECT 1
		    FROM unnest(@targetIDs::uuid[]) requested(id)
		    LEFT JOIN DeploymentTarget target
		      ON target.id = requested.id AND target.organization_id = @organizationID
		    WHERE target.id IS NULL
		  ),
		  NOT EXISTS (
		    SELECT 1
		    FROM unnest(@environmentIDs::uuid[]) requested(id)
		    LEFT JOIN Environment environment
		      ON environment.id = requested.id AND environment.organization_id = @organizationID
		    WHERE environment.id IS NULL
		  ),
		  NOT EXISTS (
		    SELECT 1
		    FROM unnest(@customerIDs::uuid[]) requested(id)
		    LEFT JOIN CustomerOrganization customer
		      ON customer.id = requested.id AND customer.organization_id = @organizationID
		    WHERE customer.id IS NULL
		  )`,
		pgx.NamedArgs{
			"organizationID": organizationID, "targetIDs": targetIDs,
			"environmentIDs": environmentIDs, "customerIDs": customerIDs,
		},
	).Scan(&targetsValid, &environmentsValid, &customersValid)
	if err != nil {
		return fmt.Errorf("validate registry import references: %w", err)
	}
	if !targetsValid || !environmentsValid || !customersValid {
		return apierrors.NewBadRequest(
			"registry import contains a resource outside the current organization",
		)
	}
	return nil
}

func validateRegistryImportRenameAliases(
	ctx context.Context,
	organizationID uuid.UUID,
	roots []types.RegistryImportCandidateRoot,
	diff types.RegistryImportDiff,
) error {
	rootByKey := make(map[string]types.RegistryImportCandidateRoot, len(roots))
	type aliasIdentity struct {
		componentKey string
		alias        string
	}
	requiredAliases := make(map[aliasIdentity]struct{})
	for _, root := range roots {
		rootByKey[root.Key] = root
		for _, placement := range root.Placements {
			if placement.RenamedFrom != "" {
				requiredAliases[aliasIdentity{
					componentKey: placement.ComponentKey,
					alias:        placement.RenamedFrom,
				}] = struct{}{}
			}
		}
	}
	for _, change := range diff.Updates {
		if change.Kind != "rename_placement" {
			continue
		}
		root, exists := rootByKey[change.RootKey]
		if !exists {
			return apierrors.NewConflict("registry import rename root is missing")
		}
		placement, exists := registryImportPlacementForChange(root, change)
		if !exists || placement.RenamedFrom == "" {
			return apierrors.NewConflict("registry import rename evidence is incomplete")
		}
	}
	identities := make([]aliasIdentity, 0, len(requiredAliases))
	for identity := range requiredAliases {
		identities = append(identities, identity)
	}
	sort.Slice(identities, func(i, j int) bool {
		return identities[i].componentKey+"\x00"+identities[i].alias <
			identities[j].componentKey+"\x00"+identities[j].alias
	})
	componentKeys := make([]string, len(identities))
	aliases := make([]string, len(identities))
	for index, identity := range identities {
		componentKeys[index], aliases[index] = identity.componentKey, identity.alias
	}
	if len(componentKeys) == 0 {
		return nil
	}
	var valid bool
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		WITH requested(component_key, alias) AS (
		  SELECT * FROM unnest(@componentKeys::text[], @aliases::text[])
		)
		SELECT NOT EXISTS (
		  SELECT 1
		  FROM requested
		  WHERE NOT EXISTS (
		    SELECT 1
		    FROM ComponentDefinition definition
		    JOIN ComponentAlias component_alias
		      ON component_alias.component_definition_id = definition.id
		     AND component_alias.organization_id = definition.organization_id
		     AND component_alias.retired_at IS NULL
		    WHERE definition.organization_id = @organizationID
		      AND definition.key = requested.component_key
		      AND definition.retired_at IS NULL
		      AND component_alias.alias = lower(requested.alias)
		  )
		)`,
		pgx.NamedArgs{
			"organizationID": organizationID, "componentKeys": componentKeys,
			"aliases": aliases,
		},
	).Scan(&valid)
	if err != nil {
		return fmt.Errorf("validate registry import rename aliases: %w", err)
	}
	if !valid {
		return apierrors.NewConflict(
			"registry import rename requires an active organization-scoped alias",
		)
	}
	return nil
}

func validateRegistryImportExistingTopology(
	ctx context.Context,
	organizationID uuid.UUID,
	roots []types.RegistryImportCandidateRoot,
	diff types.RegistryImportDiff,
) error {
	rootByKey := make(map[string]types.RegistryImportCandidateRoot, len(roots))
	for _, root := range roots {
		rootByKey[root.Key] = root
	}
	rootNeedsUnit := make(map[string]struct{})
	for _, change := range append(
		append([]types.RegistryImportChange(nil), diff.Creates...),
		diff.Updates...,
	) {
		if change.Kind == "create_placement" ||
			change.Kind == "update_placement" ||
			change.Kind == "rename_placement" {
			rootNeedsUnit[change.RootKey] = struct{}{}
		}
	}
	for rootKey := range rootNeedsUnit {
		root, exists := rootByKey[rootKey]
		if !exists {
			return apierrors.NewConflict("registry import existing root is missing")
		}
		if _, _, err := registryImportActiveUnit(ctx, organizationID, root); err != nil {
			return err
		}
	}
	for _, change := range diff.Retirements {
		var exists bool
		switch change.Kind {
		case "retire_root":
			err := internalctx.GetDb(ctx).QueryRow(ctx, `
				SELECT EXISTS (
				  SELECT 1 FROM DeploymentScope
				  WHERE organization_id = @organizationID
				    AND key = @rootKey AND retired_at IS NULL
				)`,
				pgx.NamedArgs{
					"organizationID": organizationID, "rootKey": change.RootKey,
				},
			).Scan(&exists)
			if err != nil {
				return err
			}
		case "retire_placement":
			err := internalctx.GetDb(ctx).QueryRow(ctx, `
				SELECT EXISTS (
				  SELECT 1
				  FROM ComponentInstance instance
				  JOIN ComponentDefinition definition
				    ON definition.id = instance.component_definition_id
				   AND definition.organization_id = instance.organization_id
				  JOIN DeploymentUnit unit
				    ON unit.id = instance.deployment_unit_id
				   AND unit.organization_id = instance.organization_id
				  JOIN DeploymentScope scope
				    ON scope.id = unit.deployment_scope_id
				   AND scope.organization_id = unit.organization_id
				  WHERE instance.organization_id = @organizationID
				    AND scope.key = @rootKey
				    AND definition.key = @componentKey
				    AND lower(instance.physical_name) = lower(@physicalName)
				    AND instance.retired_at IS NULL
				)`,
				pgx.NamedArgs{
					"organizationID": organizationID, "rootKey": change.RootKey,
					"componentKey": change.PlacementKey, "physicalName": change.PhysicalName,
				},
			).Scan(&exists)
			if err != nil {
				return err
			}
		default:
			return apierrors.NewConflict("unsupported registry import retirement")
		}
		if !exists {
			return apierrors.NewConflict("registry import topology changed before apply")
		}
	}
	return nil
}

func registryImportUUIDSet(values map[uuid.UUID]struct{}) []uuid.UUID {
	result := make([]uuid.UUID, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].String() < result[j].String()
	})
	return result
}

func importCandidateChecksum(candidate any) (string, error) {
	payload, err := json.Marshal(candidate)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func nullableImportText(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullableImportUUID(value uuid.UUID) any {
	if value == uuid.Nil {
		return nil
	}
	return value
}
