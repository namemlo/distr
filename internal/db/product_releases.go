package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/productrelease"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ProductReleaseValidationError struct {
	Issues []types.ProductReleaseValidationIssue
}

func (e *ProductReleaseValidationError) Error() string {
	return "product release validation failed"
}

func (e *ProductReleaseValidationError) Unwrap() error {
	return apierrors.ErrBadRequest
}

type ProductReleaseProvenanceEligibilityHook func(
	context.Context,
	uuid.UUID,
	uuid.UUID,
) (*types.ProductReleaseValidationIssue, error)

type ProductReleaseDependencyPolicyEligibilityHook func(
	context.Context,
	uuid.UUID,
	uuid.UUID,
) (*types.ProductReleaseValidationIssue, error)

var (
	productReleaseProvenanceHookMu sync.RWMutex
	productReleaseProvenanceHook   ProductReleaseProvenanceEligibilityHook = unavailableProductReleaseProvenance

	productReleaseDependencyPolicyHookMu sync.RWMutex
	productReleaseDependencyPolicyHook   ProductReleaseDependencyPolicyEligibilityHook = unavailableProductReleaseDependencyPolicy //nolint:lll
)

type productReleaseOrganizationContextKey struct{}

func WithProductReleaseOrganizationID(ctx context.Context, organizationID uuid.UUID) context.Context {
	return context.WithValue(ctx, productReleaseOrganizationContextKey{}, organizationID)
}

// SetProductReleaseProvenanceEligibilityHook is PR-061's narrow integration
// point. Its verifier must prove immutable provenance for the exact
// organization/component pair using the caller's transaction. A nil hook
// restores the fail-closed default. The returned function restores the prior
// hook and is intended for focused tests.
func SetProductReleaseProvenanceEligibilityHook(hook ProductReleaseProvenanceEligibilityHook) func() {
	if hook == nil {
		hook = unavailableProductReleaseProvenance
	}
	productReleaseProvenanceHookMu.Lock()
	previous := productReleaseProvenanceHook
	productReleaseProvenanceHook = hook
	productReleaseProvenanceHookMu.Unlock()
	return func() {
		productReleaseProvenanceHookMu.Lock()
		productReleaseProvenanceHook = previous
		productReleaseProvenanceHookMu.Unlock()
	}
}

// SetProductReleaseDependencyPolicyEligibilityHook is PR-067's narrow
// integration point. Its resolver must prove that the exact policy version is
// immutable, published, and owned by the supplied organization using the
// caller's transaction. A nil hook restores the fail-closed default.
func SetProductReleaseDependencyPolicyEligibilityHook(
	hook ProductReleaseDependencyPolicyEligibilityHook,
) func() {
	if hook == nil {
		hook = unavailableProductReleaseDependencyPolicy
	}
	productReleaseDependencyPolicyHookMu.Lock()
	previous := productReleaseDependencyPolicyHook
	productReleaseDependencyPolicyHook = hook
	productReleaseDependencyPolicyHookMu.Unlock()
	return func() {
		productReleaseDependencyPolicyHookMu.Lock()
		productReleaseDependencyPolicyHook = previous
		productReleaseDependencyPolicyHookMu.Unlock()
	}
}

func CreateProductReleaseDraft(
	ctx context.Context,
	manifest *types.ProductReleaseManifest,
) (*types.ReleaseBundle, error) {
	if manifest == nil {
		return nil, apierrors.NewBadRequest("product release manifest is required")
	}
	normalizeProductReleaseBoundary(manifest)
	if err := validateProductReleaseDraftBoundary(*manifest); err != nil {
		return nil, err
	}

	var created *types.ReleaseBundle
	err := RunTx(ctx, func(ctx context.Context) error {
		hydrated, err := hydrateProductReleaseComponents(ctx, *manifest)
		if err != nil {
			return err
		}
		manifest.Components = hydrated
		*manifest = productrelease.NormalizeProductReleaseManifest(*manifest)
		if issues := productrelease.ValidateProductReleaseGraph(*manifest); len(issues) > 0 {
			return &ProductReleaseValidationError{Issues: issues}
		}

		bundle := &types.ReleaseBundle{
			OrganizationID:        manifest.OrganizationID,
			ApplicationID:         manifest.ApplicationID,
			ChannelID:             manifest.ChannelID,
			ReleaseNumber:         manifest.Version,
			ReleaseNotes:          manifest.ReleaseNotes,
			SourceRevision:        manifest.DependencyPolicyVersion.String(),
			Kind:                  types.ReleaseBundleKindProduct,
			ReleaseContractSchema: types.ProductReleaseSchemaV1,
			Status:                types.ReleaseBundleStatusDraft,
		}
		if err := ensureReleaseBundleReferences(ctx, bundle); err != nil {
			return err
		}

		initialPayload, initialChecksum, err := productrelease.CanonicalizeProductRelease(*manifest)
		if err != nil {
			return fmt.Errorf("could not canonicalize Product Release draft: %w", err)
		}
		initialGraphChecksum, err := productrelease.ProductReleaseGraphChecksum(*manifest)
		if err != nil {
			return fmt.Errorf("could not checksum Product Release graph: %w", err)
		}
		manifest.GraphChecksum = initialGraphChecksum
		bundle.ReleaseContract = productReleaseContract(*manifest)
		bundle.CanonicalPayload = initialPayload
		bundle.CanonicalChecksum = initialChecksum
		if err := insertReleaseBundle(ctx, bundle); err != nil {
			return err
		}

		manifest.ReleaseBundleID = bundle.ID
		manifest.CreatedAt = bundle.CreatedAt
		manifest.Status = bundle.Status
		payload, checksum, err := productrelease.CanonicalizeProductRelease(*manifest)
		if err != nil {
			return fmt.Errorf("could not canonicalize Product Release draft: %w", err)
		}
		graphChecksum, err := productrelease.ProductReleaseGraphChecksum(*manifest)
		if err != nil {
			return fmt.Errorf("could not checksum Product Release graph: %w", err)
		}
		manifest.GraphChecksum = graphChecksum
		bundle.ReleaseContract = productReleaseContract(*manifest)
		bundle.CanonicalPayload = payload
		bundle.CanonicalChecksum = checksum
		if err := updateProductReleaseCanonical(ctx, *bundle); err != nil {
			return err
		}
		if err := insertProductReleaseFacts(ctx, *manifest); err != nil {
			return err
		}
		created, err = getReleaseBundle(ctx, bundle.ID, bundle.OrganizationID, false)
		if err != nil {
			return err
		}
		if err := recordReleaseControlPlaneAuditMutation(ctx, releaseControlPlaneAuditInput(
			*created,
			"product_release.draft.created",
			nil,
			"SUCCEEDED",
		)); err != nil {
			return err
		}
		for _, component := range manifest.Components {
			if err := recordReleaseControlPlaneAuditMutation(
				ctx,
				productReleaseComponentAuditInput(
					*created,
					component,
					"product_release.component.pinned",
				),
			); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

func GetProductRelease(
	ctx context.Context,
	id uuid.UUID,
	organizationID uuid.UUID,
) (*types.ReleaseBundle, *types.ProductReleaseManifest, error) {
	bundle, err := getReleaseBundle(ctx, id, organizationID, false)
	if err != nil {
		return nil, nil, err
	}
	if bundle.Kind != types.ReleaseBundleKindProduct ||
		bundle.ReleaseContract == nil ||
		bundle.ReleaseContract.ProductV1 == nil {
		return nil, nil, apierrors.ErrNotFound
	}
	manifest, err := loadProductReleaseManifest(ctx, *bundle)
	if err != nil {
		return nil, nil, err
	}
	return bundle, manifest, nil
}

func ValidateProductRelease(
	ctx context.Context,
	id uuid.UUID,
	organizationID uuid.UUID,
) ([]types.ProductReleaseValidationIssue, error) {
	var issues []types.ProductReleaseValidationIssue
	err := RunTx(ctx, func(txCtx context.Context) error {
		bundle, manifest, err := GetProductRelease(txCtx, id, organizationID)
		if err != nil {
			return err
		}
		issues, err = validateProductReleaseEligibility(txCtx, *manifest, organizationID, false)
		if err != nil {
			return err
		}
		outcome := "SUCCEEDED"
		if len(issues) > 0 {
			outcome = "REJECTED"
		}
		return recordReleaseControlPlaneAuditMutation(txCtx, releaseControlPlaneAuditInput(
			*bundle,
			"product_release.validated",
			nil,
			outcome,
		))
	})
	return issues, err
}

func GetProductReleaseGraph(
	ctx context.Context,
	id uuid.UUID,
	organizationID uuid.UUID,
) (*types.ProductReleaseGraph, error) {
	_, manifest, err := GetProductRelease(ctx, id, organizationID)
	if err != nil {
		return nil, err
	}
	graph := productrelease.BuildProductReleaseGraph(*manifest)
	checksum, err := productrelease.ProductReleaseGraphChecksum(*manifest)
	if err != nil {
		return nil, fmt.Errorf("could not checksum Product Release graph: %w", err)
	}
	graph.Checksum = checksum
	if manifest.GraphChecksum != "" && manifest.GraphChecksum != graph.Checksum {
		return nil, apierrors.NewConflict("stored Product Release graph checksum does not match frozen graph")
	}
	if err := verifyProductReleaseEdges(ctx, manifest.ReleaseBundleID, organizationID, graph.Edges); err != nil {
		return nil, err
	}
	return &graph, nil
}

func PublishProductRelease(
	ctx context.Context,
	id uuid.UUID,
	actorUserAccountID uuid.UUID,
) (*types.ReleaseBundle, error) {
	var published *types.ReleaseBundle
	var operationErr error
	err := RunTx(ctx, func(ctx context.Context) error {
		organizationID, err := currentOrganizationID(ctx)
		if err != nil {
			return err
		}
		bundle, err := getReleaseBundle(ctx, id, organizationID, true)
		if err != nil {
			return err
		}
		if bundle.Kind != types.ReleaseBundleKindProduct ||
			bundle.ReleaseContract == nil ||
			bundle.ReleaseContract.ProductV1 == nil {
			return apierrors.ErrNotFound
		}
		manifest, err := loadProductReleaseManifest(ctx, *bundle)
		if err != nil {
			return err
		}
		payload, checksum, err := productrelease.CanonicalizeProductRelease(*manifest)
		if err != nil {
			return fmt.Errorf("could not canonicalize Product Release: %w", err)
		}
		graphChecksum, err := productrelease.ProductReleaseGraphChecksum(*manifest)
		if err != nil {
			return fmt.Errorf("could not checksum Product Release graph: %w", err)
		}
		graph := productrelease.BuildProductReleaseGraph(*manifest)
		if err := verifyProductReleaseEdges(
			ctx,
			manifest.ReleaseBundleID,
			organizationID,
			graph.Edges,
		); err != nil {
			return err
		}
		if bundle.Status == types.ReleaseBundleStatusPublished {
			if bundle.CanonicalChecksum == checksum &&
				manifest.GraphChecksum == graphChecksum {
				published = bundle
				return nil
			}
			if err := recordReleaseControlPlaneAuditMutation(ctx, releaseControlPlaneAuditInput(
				*bundle,
				"product_release.publish.rejected",
				&actorUserAccountID,
				"REJECTED",
			)); err != nil {
				return err
			}
			operationErr = apierrors.NewConflict("published Product Release does not match frozen checksum")
			return nil
		}
		if bundle.Status != types.ReleaseBundleStatusDraft {
			toStatus := types.ReleaseBundleStatusPublished
			if err := insertReleaseBundleAuditEvent(ctx, releaseBundleAuditEventForTransition(
				*bundle,
				actorUserAccountID,
				types.ReleaseBundleAuditEventTypeStateTransitionRejected,
				&toStatus,
				releaseBundleTransitionRejectedReason(bundle.Status, toStatus),
			)); err != nil {
				return err
			}
			if err := recordReleaseControlPlaneAuditMutation(ctx, releaseControlPlaneAuditInput(
				*bundle,
				"product_release.publish.rejected",
				&actorUserAccountID,
				"REJECTED",
			)); err != nil {
				return err
			}
			operationErr = apierrors.NewConflict("product release state transition is invalid")
			return nil
		}

		// Lock every child in deterministic UUID order, then re-evaluate its
		// immutable identity and external evidence inside this transaction.
		issues, err := validateProductReleaseEligibility(ctx, *manifest, organizationID, true)
		if err != nil {
			return err
		}
		if len(issues) > 0 {
			toStatus := types.ReleaseBundleStatusPublished
			if err := insertReleaseBundleAuditEvent(ctx, releaseBundleAuditEventForTransition(
				*bundle,
				actorUserAccountID,
				types.ReleaseBundleAuditEventTypeStateTransitionRejected,
				&toStatus,
				"product release validation failed",
			)); err != nil {
				return err
			}
			if err := recordReleaseControlPlaneAuditMutation(ctx, releaseControlPlaneAuditInput(
				*bundle,
				"product_release.publish.rejected",
				&actorUserAccountID,
				"REJECTED",
			)); err != nil {
				return err
			}
			operationErr = &ProductReleaseValidationError{Issues: issues}
			return nil
		}

		manifest.GraphChecksum = graphChecksum
		bundle.ReleaseContract = productReleaseContract(*manifest)
		bundle.CanonicalPayload = payload
		bundle.CanonicalChecksum = checksum
		updated, err := publishProductReleaseRow(ctx, *bundle, actorUserAccountID)
		if err != nil {
			return err
		}
		toStatus := types.ReleaseBundleStatusPublished
		if err := insertReleaseBundleAuditEvent(ctx, releaseBundleAuditEventForTransition(
			*bundle,
			actorUserAccountID,
			types.ReleaseBundleAuditEventTypePublished,
			&toStatus,
			"",
		)); err != nil {
			return err
		}
		if err := recordReleaseControlPlaneAuditMutation(ctx, releaseControlPlaneAuditInput(
			*updated,
			"product_release.published",
			&actorUserAccountID,
			"SUCCEEDED",
		)); err != nil {
			return err
		}
		published = updated
		return nil
	})
	if err != nil {
		return nil, err
	}
	return published, operationErr
}

func currentOrganizationID(ctx context.Context) (uuid.UUID, error) {
	organizationID, ok := ctx.Value(productReleaseOrganizationContextKey{}).(uuid.UUID)
	if !ok || organizationID == uuid.Nil {
		return uuid.Nil, apierrors.NewForbidden("product release organization scope is required")
	}
	return organizationID, nil
}

func hydrateProductReleaseComponents(
	ctx context.Context,
	manifest types.ProductReleaseManifest,
) ([]types.ProductReleaseComponent, error) {
	if len(manifest.Components) > types.ProductReleaseMaxComponents {
		return nil, apierrors.NewBadRequest("too many component releases")
	}
	components := make([]types.ProductReleaseComponent, 0, len(manifest.Components))
	seen := make(map[uuid.UUID]struct{}, len(manifest.Components))
	componentReleaseIDs := make([]uuid.UUID, 0, len(manifest.Components))
	for _, requested := range manifest.Components {
		if requested.ComponentReleaseID == uuid.Nil {
			return nil, apierrors.NewBadRequest("componentReleaseId is required")
		}
		if _, duplicate := seen[requested.ComponentReleaseID]; duplicate {
			return nil, apierrors.NewBadRequest("component release ids must be unique")
		}
		seen[requested.ComponentReleaseID] = struct{}{}
		if !productReleaseChecksumPatternDB(requested.ComponentReleaseChecksum) {
			return nil, apierrors.NewBadRequest("componentReleaseChecksum must be lowercase sha256")
		}
		componentReleaseIDs = append(componentReleaseIDs, requested.ComponentReleaseID)
	}
	children, err := loadProductReleaseChildBundles(
		ctx,
		manifest.OrganizationID,
		componentReleaseIDs,
		false,
	)
	if err != nil {
		return nil, err
	}
	for _, requested := range manifest.Components {
		child, ok := children[requested.ComponentReleaseID]
		if !ok {
			return nil, apierrors.NewBadRequest(
				"component release must be a published v2 Component Release",
			)
		}
		if child.Kind != types.ReleaseBundleKindComponent ||
			child.Status != types.ReleaseBundleStatusPublished ||
			child.ReleaseContract == nil ||
			child.ReleaseContract.ComponentV2 == nil {
			return nil, apierrors.NewBadRequest("component release must be a published v2 Component Release")
		}
		if child.CanonicalChecksum != requested.ComponentReleaseChecksum {
			return nil, apierrors.NewConflict("component release checksum does not match the published release")
		}
		contract := cloneComponentReleaseContract(*child.ReleaseContract.ComponentV2)
		if requested.ComponentKey != "" && strings.TrimSpace(requested.ComponentKey) != contract.ComponentKey {
			return nil, apierrors.NewConflict("component key does not match the published release")
		}
		if requested.Version != "" && strings.TrimSpace(requested.Version) != contract.Version {
			return nil, apierrors.NewConflict("component version does not match the published release")
		}
		components = append(components, types.ProductReleaseComponent{
			OrganizationID:           manifest.OrganizationID,
			ComponentReleaseID:       child.ID,
			ComponentReleaseChecksum: child.CanonicalChecksum,
			ComponentKey:             contract.ComponentKey,
			Version:                  contract.Version,
			Published:                true,
			Provides:                 slices.Clone(contract.Provides),
			Requires:                 cloneCapabilityRequirements(contract.Requires),
			Migrations:               slices.Clone(contract.Migrations),
			Platforms:                componentContractPlatforms(contract),
			Contract:                 &contract,
		})
	}
	return components, nil
}

func loadProductReleaseChildBundles(
	ctx context.Context,
	organizationID uuid.UUID,
	componentReleaseIDs []uuid.UUID,
	forUpdate bool,
) (map[uuid.UUID]types.ReleaseBundle, error) {
	if len(componentReleaseIDs) > types.ProductReleaseMaxComponents {
		return nil, apierrors.NewBadRequest("too many component releases")
	}
	if len(componentReleaseIDs) == 0 {
		return map[uuid.UUID]types.ReleaseBundle{}, nil
	}
	ids := slices.Clone(componentReleaseIDs)
	slices.SortFunc(ids, func(left, right uuid.UUID) int {
		return strings.Compare(left.String(), right.String())
	})
	lockClause := ""
	if forUpdate {
		lockClause = " FOR UPDATE OF rb"
	}
	database := internalctx.GetDb(ctx)
	rows, err := database.Query(ctx, `
		SELECT `+releaseBundleOutputExpr+`
		FROM ReleaseBundle rb
		WHERE rb.organization_id = @organizationId
		  AND rb.id = ANY(@componentReleaseIds)
		ORDER BY rb.id`+lockClause,
		pgx.NamedArgs{
			"organizationId":      organizationID,
			"componentReleaseIds": ids,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query Product Release children: %w", err)
	}
	bundles, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.ReleaseBundle])
	if err != nil {
		return nil, fmt.Errorf("could not collect Product Release children: %w", err)
	}
	result := make(map[uuid.UUID]types.ReleaseBundle, len(bundles))
	for _, bundle := range bundles {
		result[bundle.ID] = bundle
	}
	return result, nil
}

func loadProductReleaseManifest(
	ctx context.Context,
	bundle types.ReleaseBundle,
) (*types.ProductReleaseManifest, error) {
	base := *bundle.ReleaseContract.ProductV1
	base.ReleaseBundleID = bundle.ID
	base.OrganizationID = bundle.OrganizationID
	base.ApplicationID = bundle.ApplicationID
	base.ChannelID = bundle.ChannelID
	base.CreatedAt = bundle.CreatedAt
	base.Status = bundle.Status
	base.CanonicalChecksum = bundle.CanonicalChecksum
	base.PublishedByUserAccountID = bundle.PublishedByUserAccountID
	base.PublishedAt = bundle.PublishedAt

	database := internalctx.GetDb(ctx)
	rows, err := database.Query(ctx, `
		SELECT
			prc.id,
			prc.component_release_bundle_id,
			prc.component_release_checksum,
			prc.component_key,
			prc.component_version,
			prc.contract_snapshot
		FROM ProductReleaseComponent prc
		WHERE prc.product_release_bundle_id = @productReleaseBundleId
		  AND prc.organization_id = @organizationId
		ORDER BY prc.component_key, prc.component_release_bundle_id`,
		pgx.NamedArgs{
			"productReleaseBundleId": bundle.ID,
			"organizationId":         bundle.OrganizationID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query Product Release components: %w", err)
	}
	defer rows.Close()

	components := make([]types.ProductReleaseComponent, 0)
	for rows.Next() {
		var component types.ProductReleaseComponent
		var snapshot []byte
		if err := rows.Scan(
			&component.ID,
			&component.ComponentReleaseID,
			&component.ComponentReleaseChecksum,
			&component.ComponentKey,
			&component.Version,
			&snapshot,
		); err != nil {
			return nil, fmt.Errorf("could not scan Product Release component: %w", err)
		}
		var contract types.ComponentReleaseContractV2
		if err := json.Unmarshal(snapshot, &contract); err != nil {
			return nil, fmt.Errorf("could not decode Product Release component snapshot: %w", err)
		}
		component.ProductReleaseBundleID = bundle.ID
		component.OrganizationID = bundle.OrganizationID
		component.Published = true
		component.Provides = slices.Clone(contract.Provides)
		component.Requires = cloneCapabilityRequirements(contract.Requires)
		component.Migrations = slices.Clone(contract.Migrations)
		component.Platforms = componentContractPlatforms(contract)
		component.Contract = &contract
		components = append(components, component)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("could not collect Product Release components: %w", err)
	}
	base.Components = components
	return &base, nil
}

func insertProductReleaseFacts(ctx context.Context, manifest types.ProductReleaseManifest) error {
	database := internalctx.GetDb(ctx)
	components := make([]types.ProductReleaseComponent, len(manifest.Components))
	contractSnapshots := make([][]byte, len(manifest.Components))
	for index, component := range manifest.Components {
		if component.Contract == nil {
			return apierrors.NewBadRequest("component release contract snapshot is required")
		}
		component.ID = uuid.New()
		component.ProductReleaseBundleID = manifest.ReleaseBundleID
		component.OrganizationID = manifest.OrganizationID
		components[index] = component
		snapshot, err := json.Marshal(component.Contract)
		if err != nil {
			return fmt.Errorf("could not encode Component Release contract snapshot: %w", err)
		}
		contractSnapshots[index] = snapshot
	}
	if _, err := database.CopyFrom(
		ctx,
		pgx.Identifier{"productreleasecomponent"},
		[]string{
			"id",
			"product_release_bundle_id",
			"organization_id",
			"component_release_bundle_id",
			"component_release_checksum",
			"component_key",
			"component_version",
			"contract_snapshot",
		},
		pgx.CopyFromSlice(len(components), func(index int) ([]any, error) {
			component := components[index]
			return []any{
				component.ID,
				component.ProductReleaseBundleID,
				component.OrganizationID,
				component.ComponentReleaseID,
				component.ComponentReleaseChecksum,
				component.ComponentKey,
				component.Version,
				contractSnapshots[index],
			}, nil
		}),
	); err != nil {
		return mapReleaseBundleWriteError("insert Product Release components", err)
	}

	graph := productrelease.BuildProductReleaseGraph(manifest)
	if len(graph.Edges) == 0 {
		return nil
	}
	if _, err := database.CopyFrom(
		ctx,
		pgx.Identifier{"productreleasecapabilityedge"},
		[]string{
			"id",
			"product_release_bundle_id",
			"organization_id",
			"edge_key",
			"from_node_key",
			"to_node_key",
			"consumer_component_key",
			"provider_component_key",
			"capability_name",
			"version_range",
			"provider_version",
			"resolution_stage",
			"allowed_modes",
			"ordering",
		},
		pgx.CopyFromSlice(len(graph.Edges), func(index int) ([]any, error) {
			edge := graph.Edges[index]
			var providerComponentKey *string
			if edge.ResolutionStage == types.CapabilityResolutionStageProduct {
				value := strings.TrimPrefix(edge.From, "component:")
				providerComponentKey = &value
			}
			allowedModes := make([]string, 0, len(edge.AllowedModes))
			for _, mode := range edge.AllowedModes {
				allowedModes = append(allowedModes, string(mode))
			}
			return []any{
				uuid.New(),
				manifest.ReleaseBundleID,
				manifest.OrganizationID,
				edge.Key,
				edge.From,
				edge.To,
				strings.TrimPrefix(strings.TrimPrefix(edge.To, "component:"), "product:"),
				providerComponentKey,
				edge.Capability,
				edge.VersionRange,
				edge.ProviderVersion,
				edge.ResolutionStage,
				allowedModes,
				edge.Ordering,
			}, nil
		}),
	); err != nil {
		return mapReleaseBundleWriteError("insert Product Release capability edges", err)
	}
	return nil
}

func updateProductReleaseCanonical(ctx context.Context, bundle types.ReleaseBundle) error {
	database := internalctx.GetDb(ctx)
	command, err := database.Exec(ctx, `
		UPDATE ReleaseBundle
		SET release_contract = @releaseContract,
		    canonical_checksum = @canonicalChecksum,
		    canonical_payload = @canonicalPayload,
		    updated_at = now()
		WHERE id = @id
		  AND organization_id = @organizationId
		  AND kind = 'product'
		  AND status = 'DRAFT'`,
		pgx.NamedArgs{
			"id":                bundle.ID,
			"organizationId":    bundle.OrganizationID,
			"releaseContract":   bundle.ReleaseContract,
			"canonicalChecksum": bundle.CanonicalChecksum,
			"canonicalPayload":  bundle.CanonicalPayload,
		},
	)
	if err != nil {
		return mapReleaseBundleWriteError("update Product Release checksum", err)
	}
	if command.RowsAffected() != 1 {
		return apierrors.NewConflict("product release draft changed before checksum freeze")
	}
	return nil
}

func publishProductReleaseRow(
	ctx context.Context,
	bundle types.ReleaseBundle,
	actorUserAccountID uuid.UUID,
) (*types.ReleaseBundle, error) {
	database := internalctx.GetDb(ctx)
	rows, err := database.Query(ctx, `
		UPDATE ReleaseBundle AS rb
		SET release_contract = @releaseContract,
		    canonical_checksum = @canonicalChecksum,
		    canonical_payload = @canonicalPayload,
		    status = 'PUBLISHED',
		    published_by_user_account_id = @publishedByUserAccountId,
		    published_at = now(),
		    updated_at = now()
		WHERE rb.id = @id
		  AND rb.organization_id = @organizationId
		  AND rb.kind = 'product'
		  AND rb.status = 'DRAFT'
		RETURNING `+releaseBundleOutputExpr,
		pgx.NamedArgs{
			"id":                       bundle.ID,
			"organizationId":           bundle.OrganizationID,
			"releaseContract":          bundle.ReleaseContract,
			"canonicalChecksum":        bundle.CanonicalChecksum,
			"canonicalPayload":         bundle.CanonicalPayload,
			"publishedByUserAccountId": actorUserAccountID,
		},
	)
	if err != nil {
		return nil, mapReleaseBundleWriteError("publish Product Release", err)
	}
	updated, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.ReleaseBundle])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.NewConflict("product release draft changed before publication")
	} else if err != nil {
		return nil, mapReleaseBundleWriteError("scan published Product Release", err)
	}
	return &updated, nil
}

func verifyProductReleaseEdges(
	ctx context.Context,
	productReleaseID uuid.UUID,
	organizationID uuid.UUID,
	expected []types.GraphEdge,
) error {
	database := internalctx.GetDb(ctx)
	rows, err := database.Query(ctx, `
		SELECT
			edge_key,
			from_node_key,
			to_node_key,
			capability_name,
			version_range,
			provider_version,
			resolution_stage,
			allowed_modes,
			ordering
		FROM ProductReleaseCapabilityEdge
		WHERE product_release_bundle_id = @productReleaseBundleId
		  AND organization_id = @organizationId
		ORDER BY edge_key`,
		pgx.NamedArgs{
			"productReleaseBundleId": productReleaseID,
			"organizationId":         organizationID,
		},
	)
	if err != nil {
		return fmt.Errorf("could not query Product Release graph: %w", err)
	}
	defer rows.Close()
	actual := make([]types.GraphEdge, 0)
	for rows.Next() {
		var edge types.GraphEdge
		var stage string
		var modes []string
		if err := rows.Scan(
			&edge.Key,
			&edge.From,
			&edge.To,
			&edge.Capability,
			&edge.VersionRange,
			&edge.ProviderVersion,
			&stage,
			&modes,
			&edge.Ordering,
		); err != nil {
			return fmt.Errorf("could not scan Product Release graph: %w", err)
		}
		edge.ResolutionStage = types.CapabilityResolutionStage(stage)
		for _, mode := range modes {
			edge.AllowedModes = append(edge.AllowedModes, types.RequirementResolutionMode(mode))
		}
		actual = append(actual, edge)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("could not collect Product Release graph: %w", err)
	}
	if !slices.EqualFunc(actual, expected, func(a, b types.GraphEdge) bool {
		return a.Key == b.Key &&
			a.From == b.From &&
			a.To == b.To &&
			a.Capability == b.Capability &&
			a.VersionRange == b.VersionRange &&
			a.ProviderVersion == b.ProviderVersion &&
			a.ResolutionStage == b.ResolutionStage &&
			slices.Equal(a.AllowedModes, b.AllowedModes) &&
			a.Ordering == b.Ordering
	}) {
		return apierrors.NewConflict("stored Product Release graph does not match frozen manifest")
	}
	return nil
}

func productReleaseContract(manifest types.ProductReleaseManifest) *types.ReleaseContract {
	public := manifest
	public.OrganizationID = uuid.Nil
	public.ApplicationID = uuid.Nil
	public.ChannelID = uuid.Nil
	public.CreatedAt = public.CreatedAt.UTC()
	public.Status = ""
	public.CanonicalChecksum = ""
	public.PublishedByUserAccountID = nil
	public.PublishedAt = nil
	return &types.ReleaseContract{
		Schema:    types.ProductReleaseSchemaV1,
		ProductV1: &public,
	}
}

func normalizeProductReleaseBoundary(manifest *types.ProductReleaseManifest) {
	manifest.Schema = strings.TrimSpace(manifest.Schema)
	if manifest.Schema == "" {
		manifest.Schema = types.ProductReleaseSchemaV1
	}
	manifest.Product = strings.TrimSpace(manifest.Product)
	manifest.Version = strings.TrimSpace(manifest.Version)
	manifest.ReleaseNotes = strings.TrimSpace(manifest.ReleaseNotes)
	for index := range manifest.Components {
		component := &manifest.Components[index]
		component.ComponentReleaseChecksum = strings.TrimSpace(component.ComponentReleaseChecksum)
		component.ComponentKey = strings.TrimSpace(component.ComponentKey)
		component.Version = strings.TrimSpace(component.Version)
	}
}

func validateProductReleaseDraftBoundary(manifest types.ProductReleaseManifest) error {
	switch {
	case manifest.Schema != types.ProductReleaseSchemaV1:
		return apierrors.NewBadRequest("schema must be distr.product-release/v1")
	case manifest.OrganizationID == uuid.Nil:
		return apierrors.NewBadRequest("organization is required")
	case manifest.ApplicationID == uuid.Nil:
		return apierrors.NewBadRequest("applicationId is required")
	case manifest.ChannelID == uuid.Nil:
		return apierrors.NewBadRequest("channelId is required")
	case manifest.Product == "":
		return apierrors.NewBadRequest("product is required")
	case manifest.Version == "":
		return apierrors.NewBadRequest("version is required")
	case len(manifest.Version) > types.ProductReleaseMaxVersionBytes:
		return apierrors.NewBadRequest("version is too long")
	case manifest.DependencyPolicyVersion == uuid.Nil:
		return apierrors.NewBadRequest("dependencyPolicyVersion is required")
	case len(manifest.Components) == 0:
		return apierrors.NewBadRequest("at least one component release is required")
	case len(manifest.Components) > types.ProductReleaseMaxComponents:
		return apierrors.NewBadRequest("too many component releases")
	case len(manifest.Requirements) > types.ProductReleaseMaxRequirements:
		return apierrors.NewBadRequest("too many product requirements")
	case len(manifest.RequiredPlatforms) > types.ProductReleaseMaxRequiredPlatforms:
		return apierrors.NewBadRequest("too many required platforms")
	default:
		for _, requirement := range manifest.Requirements {
			if len(strings.TrimSpace(requirement.Range)) > types.ProductReleaseMaxCapabilityRangeBytes {
				return apierrors.NewBadRequest("requirement range is too long")
			}
			if len(requirement.AllowedModes) > types.ProductReleaseMaxResolutionModes {
				return apierrors.NewBadRequest("too many requirement resolution modes")
			}
		}
		return nil
	}
}

func productReleaseProvenanceEligibility(
	ctx context.Context,
	organizationID uuid.UUID,
	componentReleaseID uuid.UUID,
) (*types.ProductReleaseValidationIssue, error) {
	productReleaseProvenanceHookMu.RLock()
	hook := productReleaseProvenanceHook
	productReleaseProvenanceHookMu.RUnlock()
	return hook(ctx, organizationID, componentReleaseID)
}

func unavailableProductReleaseProvenance(
	context.Context,
	uuid.UUID,
	uuid.UUID,
) (*types.ProductReleaseValidationIssue, error) {
	return &types.ProductReleaseValidationIssue{
		Field:   "components",
		Rule:    "provenanceVerifierUnavailable",
		Message: "component provenance verifier is unavailable",
	}, nil
}

func productReleaseDependencyPolicyEligibility(
	ctx context.Context,
	organizationID uuid.UUID,
	dependencyPolicyVersionID uuid.UUID,
) (*types.ProductReleaseValidationIssue, error) {
	productReleaseDependencyPolicyHookMu.RLock()
	hook := productReleaseDependencyPolicyHook
	productReleaseDependencyPolicyHookMu.RUnlock()
	return hook(ctx, organizationID, dependencyPolicyVersionID)
}

func unavailableProductReleaseDependencyPolicy(
	context.Context,
	uuid.UUID,
	uuid.UUID,
) (*types.ProductReleaseValidationIssue, error) {
	return &types.ProductReleaseValidationIssue{
		Field:   "dependencyPolicyVersion",
		Rule:    "publishedPolicyUnavailable",
		Message: "published organization-scoped dependency policy verifier is unavailable",
	}, nil
}

func validateProductReleaseEligibility(
	ctx context.Context,
	manifest types.ProductReleaseManifest,
	organizationID uuid.UUID,
	lockChildren bool,
) ([]types.ProductReleaseValidationIssue, error) {
	issues := productrelease.ValidateProductReleaseGraph(manifest)
	componentReleaseIDs := make([]uuid.UUID, 0, len(manifest.Components))
	for _, component := range manifest.Components {
		componentReleaseIDs = append(componentReleaseIDs, component.ComponentReleaseID)
	}
	children, err := loadProductReleaseChildBundles(
		ctx,
		organizationID,
		componentReleaseIDs,
		lockChildren,
	)
	if err != nil {
		return nil, err
	}
	policyIssue, err := productReleaseDependencyPolicyEligibility(
		ctx,
		organizationID,
		manifest.DependencyPolicyVersion,
	)
	if err != nil {
		return nil, err
	}
	if policyIssue != nil {
		issues = append(issues, *policyIssue)
	}
	for _, component := range manifest.Components {
		child, ok := children[component.ComponentReleaseID]
		if !ok {
			issues = append(issues, types.ProductReleaseValidationIssue{
				Field:   "components." + component.ComponentKey,
				Rule:    "publishedChild",
				Message: "pinned component release is not available in this organization",
			})
			continue
		}
		if child.Kind != types.ReleaseBundleKindComponent ||
			child.Status != types.ReleaseBundleStatusPublished ||
			child.CanonicalChecksum != component.ComponentReleaseChecksum {
			issues = append(issues, types.ProductReleaseValidationIssue{
				Field:   "components." + component.ComponentKey,
				Rule:    "publishedChild",
				Message: "pinned component release identity or checksum is no longer eligible",
			})
		}
		provenanceIssue, err := productReleaseProvenanceEligibility(
			ctx,
			organizationID,
			component.ComponentReleaseID,
		)
		if err != nil {
			return nil, err
		}
		if provenanceIssue != nil {
			issues = append(issues, *provenanceIssue)
		}
	}
	slices.SortFunc(issues, func(a, b types.ProductReleaseValidationIssue) int {
		if cmp := strings.Compare(a.Field, b.Field); cmp != 0 {
			return cmp
		}
		if cmp := strings.Compare(a.Rule, b.Rule); cmp != 0 {
			return cmp
		}
		if cmp := strings.Compare(a.Message, b.Message); cmp != 0 {
			return cmp
		}
		return strings.Compare(strings.Join(a.Path, "\x00"), strings.Join(b.Path, "\x00"))
	})
	compacted := issues[:0]
	for _, issue := range issues {
		if len(compacted) > 0 {
			previous := compacted[len(compacted)-1]
			if previous.Field == issue.Field &&
				previous.Rule == issue.Rule &&
				previous.Message == issue.Message &&
				slices.Equal(previous.Path, issue.Path) {
				continue
			}
		}
		compacted = append(compacted, issue)
	}
	return compacted, nil
}

func componentContractPlatforms(contract types.ComponentReleaseContractV2) []string {
	seen := make(map[string]struct{})
	for _, artifact := range contract.Artifacts {
		for _, platform := range artifact.Platforms {
			seen[platform.Platform] = struct{}{}
		}
	}
	result := make([]string, 0, len(seen))
	for platform := range seen {
		result = append(result, platform)
	}
	slices.Sort(result)
	return result
}

func cloneComponentReleaseContract(contract types.ComponentReleaseContractV2) types.ComponentReleaseContractV2 {
	clone := contract
	clone.Artifacts = slices.Clone(contract.Artifacts)
	for index := range clone.Artifacts {
		clone.Artifacts[index].Platforms = slices.Clone(contract.Artifacts[index].Platforms)
	}
	clone.Provides = slices.Clone(contract.Provides)
	clone.Requires = cloneCapabilityRequirements(contract.Requires)
	clone.Migrations = slices.Clone(contract.Migrations)
	clone.Changes.Commits = slices.Clone(contract.Changes.Commits)
	clone.Evidence.Provenance = slices.Clone(contract.Evidence.Provenance)
	clone.Evidence.SBOM = slices.Clone(contract.Evidence.SBOM)
	clone.Evidence.Signatures = slices.Clone(contract.Evidence.Signatures)
	clone.Evidence.Tests = slices.Clone(contract.Evidence.Tests)
	return clone
}

func cloneCapabilityRequirements(input []types.CapabilityRequirement) []types.CapabilityRequirement {
	result := slices.Clone(input)
	for index := range result {
		result[index].AllowedModes = slices.Clone(input[index].AllowedModes)
	}
	return result
}

func productReleaseChecksumPatternDB(checksum string) bool {
	if len(checksum) != len("sha256:")+64 || !strings.HasPrefix(checksum, "sha256:") {
		return false
	}
	for _, char := range checksum[len("sha256:"):] {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}
