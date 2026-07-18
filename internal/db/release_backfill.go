package db

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/releasebundles"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	releaseBackfillBlockedMissingContract            = "missing_contract"
	releaseBackfillBlockedAmbiguousComponents        = "ambiguous_components"
	releaseBackfillBlockedAmbiguousOperations        = "ambiguous_operations"
	releaseBackfillBlockedAmbiguousCapabilities      = "ambiguous_capabilities"
	releaseBackfillBlockedInvalidSource              = "invalid_source"
	releaseBackfillBlockedInvalidBuild               = "invalid_build"
	releaseBackfillBlockedInvalidArtifact            = "invalid_artifact"
	releaseBackfillBlockedAmbiguousArtifactMediaType = "ambiguous_artifact_media_type"
	releaseBackfillBlockedInvalidChanges             = "invalid_changes"
	releaseBackfillBlockedInvalidV2Contract          = "invalid_v2_contract"
	releaseBackfillBlockedMutableSource              = "mutable_source"
	releaseBackfillBlockedSourceChecksum             = "source_checksum_mismatch"
	maxReleaseBackfillArtifactEvidence               = 2000
)

var releaseBackfillCommitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

type releaseBackfillCandidate struct {
	Bundle         types.ReleaseBundle
	AlreadyPresent bool
}

type releaseBackfillProjection struct {
	Bundle     types.ReleaseBundle
	ReasonCode string
}

func validateReleaseBackfillArtifactEvidenceRequest(
	request types.ReleaseBackfillRequest,
) error {
	evidence := request.ArtifactEvidence
	if len(evidence) > maxReleaseBackfillArtifactEvidence {
		return apierrors.NewBadRequest("artifactEvidence exceeds the bounded item limit")
	}
	hasDocument := request.EvidenceDocumentReference != "" ||
		request.EvidenceDocumentChecksum != "" ||
		len(evidence) > 0
	if request.Apply && (!hasDocument || len(evidence) == 0) {
		return apierrors.NewBadRequest("reviewed artifact evidence document is required when apply is enabled")
	}
	if hasDocument &&
		(!safeReleaseBackfillEvidenceText(request.EvidenceDocumentReference, 2048) ||
			!releasebundles.IsSHA256Digest(request.EvidenceDocumentChecksum)) {
		return apierrors.NewBadRequest("artifact evidence document binding is invalid")
	}
	for _, item := range evidence {
		if item.SourceReleaseBundleID == uuid.Nil ||
			!safeReleaseBackfillEvidenceText(item.ArtifactKey, 128) ||
			!releasebundles.IsSHA256Digest(item.ArtifactDigest) ||
			releaseBackfillArtifactTypeForAnyComponent(item.MediaType) == "" ||
			!safeReleaseBackfillEvidenceText(item.Reference, 2048) ||
			!releasebundles.IsSHA256Digest(item.EvidenceDigest) {
			return apierrors.NewBadRequest("artifactEvidence contains an invalid bounded observation")
		}
	}
	return nil
}

func safeReleaseBackfillEvidenceText(value string, limit int) bool {
	return value != "" &&
		len(value) <= limit &&
		utf8.ValidString(value) &&
		strings.TrimSpace(value) == value &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}

func releaseBackfillArtifactTypeForAnyComponent(mediaType string) string {
	for _, componentType := range []types.ReleaseBundleComponentType{
		types.ReleaseBundleComponentTypeOCIImage,
		types.ReleaseBundleComponentTypeOCIArtifact,
		types.ReleaseBundleComponentTypeHelmChart,
	} {
		if artifactType := releaseBackfillArtifactType(componentType, mediaType); artifactType != "" {
			return artifactType
		}
	}
	return ""
}

func recordReleaseBackfillDryRunProjection(
	report *types.ReleaseBackfillReport,
	projection releaseBackfillProjection,
) {
	if projection.ReasonCode != "" {
		report.Blocked++
		return
	}
	report.Eligible++
	report.WouldDerive++
}

func BackfillComponentReleaseV2(
	ctx context.Context,
	request types.ReleaseBackfillRequest,
) (*types.ReleaseBackfillReport, error) {
	if request.OrganizationID == uuid.Nil {
		return nil, apierrors.NewBadRequest("organizationId is required")
	}
	if request.Apply && request.CheckpointID == uuid.Nil {
		return nil, apierrors.NewBadRequest("checkpointId is required when apply is enabled")
	}
	if request.BatchSize < 0 || request.BatchSize > 1000 {
		return nil, apierrors.NewBadRequest("batchSize must be between 1 and 1000 when provided")
	}
	if request.Cursor != nil &&
		(request.Cursor.ReleaseBundleID == uuid.Nil || request.Cursor.CreatedAt.IsZero()) {
		return nil, apierrors.NewBadRequest("cursor createdAt and releaseBundleId are required")
	}
	if err := validateReleaseBackfillArtifactEvidenceRequest(request); err != nil {
		return nil, err
	}
	if request.Apply {
		if err := ensureReleaseBackfillCheckpoint(ctx, request); err != nil {
			return nil, err
		}
	}
	report := &types.ReleaseBackfillReport{
		CheckpointID: request.CheckpointID,
		DryRun:       !request.Apply,
		NextCursor:   cloneReleaseBackfillCursor(request.Cursor),
	}
	listed, err := runReleaseBackfillInvocation(
		ctx,
		request,
		report,
		listReleaseBackfillCandidates,
		func(candidate releaseBackfillCandidate) (bool, bool, error) {
			if candidate.AlreadyPresent {
				report.AlreadyPresent++
				return true, false, nil
			}
			selectedEvidence, reviewed := singleReleaseBackfillArtifactEvidence(
				candidate.Bundle.ID,
				request.ArtifactEvidence,
			)
			if !reviewed {
				report.AwaitingEvidence++
				return !request.Apply, request.Apply, nil
			}
			if !request.Apply {
				projection := projectLegacyReleaseBundleV2(
					candidate.Bundle,
					[]types.ReleaseBackfillArtifactEvidence{selectedEvidence},
				)
				recordReleaseBackfillDryRunProjection(report, projection)
				return true, false, nil
			}
			outcome, err := applyReleaseBackfillCandidate(
				ctx,
				request,
				candidate.Bundle.ID,
				selectedEvidence,
			)
			if err != nil {
				report.Failed++
				return false, false, err
			}
			switch outcome {
			case "already_present":
				report.AlreadyPresent++
			case "blocked":
				report.Blocked++
			case "derived":
				report.Eligible++
				report.Derived++
			default:
				report.Failed++
				return false, false, fmt.Errorf("release contract v2 backfill returned an invalid outcome")
			}
			return true, false, nil
		},
	)
	if err != nil {
		if !listed {
			return nil, err
		}
		return report, err
	}
	return report, nil
}

func runReleaseBackfillInvocation(
	ctx context.Context,
	request types.ReleaseBackfillRequest,
	report *types.ReleaseBackfillReport,
	list func(context.Context, types.ReleaseBackfillRequest) ([]releaseBackfillCandidate, error),
	process func(releaseBackfillCandidate) (advance bool, stop bool, err error),
) (bool, error) {
	candidates, err := list(ctx, request)
	if err != nil {
		return false, err
	}
	for _, candidate := range candidates {
		report.Scanned++
		advance, stop, err := process(candidate)
		if err != nil {
			return true, err
		}
		if advance {
			cursor := &types.ReleaseBackfillCursor{
				CreatedAt:       candidate.Bundle.CreatedAt,
				ReleaseBundleID: candidate.Bundle.ID,
			}
			report.LastCursor = cursor
			report.NextCursor = cursor
		}
		if stop {
			return true, nil
		}
	}
	return true, nil
}

func listReleaseBackfillCandidates(
	ctx context.Context,
	request types.ReleaseBackfillRequest,
) ([]releaseBackfillCandidate, error) {
	batchSize := request.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}
	if batchSize > 1000 {
		batchSize = 1000
	}
	var cursorCreatedAt any
	var cursorBundleID any
	if request.Cursor != nil {
		cursorCreatedAt = request.Cursor.CreatedAt
		cursorBundleID = request.Cursor.ReleaseBundleID
	}
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx, `
		SELECT `+releaseBundleOutputExpr+`,
		       lineage.id IS NOT NULL AS already_present
		FROM ReleaseBundle rb
		LEFT JOIN ReleaseContractV2BackfillLineage lineage
		  ON lineage.organization_id = rb.organization_id
		 AND lineage.source_release_bundle_id = rb.id
		WHERE rb.organization_id = @organizationId
		  AND rb.kind = @legacyKind
		  AND rb.release_contract_schema = @legacySchema
		  AND rb.status IN (@publishedStatus, @blockedStatus, @archivedStatus)
		  AND (
		    @cursorCreatedAt::timestamptz IS NULL
		    OR (rb.created_at, rb.id) > (@cursorCreatedAt::timestamptz, @cursorReleaseBundleId::uuid)
		  )
		ORDER BY rb.created_at, rb.id
		LIMIT @batchSize`,
		pgx.NamedArgs{
			"organizationId":        request.OrganizationID,
			"legacyKind":            types.ReleaseBundleKindLegacy,
			"legacySchema":          types.ReleaseContractStorageSchemaV1,
			"publishedStatus":       types.ReleaseBundleStatusPublished,
			"blockedStatus":         types.ReleaseBundleStatusBlocked,
			"archivedStatus":        types.ReleaseBundleStatusArchived,
			"cursorCreatedAt":       cursorCreatedAt,
			"cursorReleaseBundleId": cursorBundleID,
			"batchSize":             batchSize,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query release contract v2 backfill candidates: %w", err)
	}
	defer rows.Close()
	candidates := make([]releaseBackfillCandidate, 0)
	for rows.Next() {
		var candidate releaseBackfillCandidate
		if err := rows.Scan(
			&candidate.Bundle.ID,
			&candidate.Bundle.CreatedAt,
			&candidate.Bundle.UpdatedAt,
			&candidate.Bundle.OrganizationID,
			&candidate.Bundle.ApplicationID,
			&candidate.Bundle.ChannelID,
			&candidate.Bundle.ProcessSnapshotID,
			&candidate.Bundle.VariableSnapshotID,
			&candidate.Bundle.ReleaseNumber,
			&candidate.Bundle.ReleaseNotes,
			&candidate.Bundle.SourceRevision,
			&candidate.Bundle.SourceRepository,
			&candidate.Bundle.SourceBranch,
			&candidate.Bundle.SourceTag,
			&candidate.Bundle.CIProvider,
			&candidate.Bundle.CIRunID,
			&candidate.Bundle.CIRunURL,
			&candidate.Bundle.ReleaseContract,
			&candidate.Bundle.Kind,
			&candidate.Bundle.ReleaseContractSchema,
			&candidate.Bundle.Status,
			&candidate.Bundle.PublishedByUserAccountID,
			&candidate.Bundle.PublishedAt,
			&candidate.Bundle.CanonicalChecksum,
			&candidate.Bundle.CanonicalPayload,
			&candidate.AlreadyPresent,
		); err != nil {
			return nil, fmt.Errorf("could not scan release contract v2 backfill candidate: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("could not collect release contract v2 backfill candidates: %w", err)
	}
	rows.Close()
	for i := range candidates {
		if candidates[i].AlreadyPresent {
			continue
		}
		candidates[i].Bundle.Components, err = getReleaseBundleComponents(ctx, candidates[i].Bundle.ID)
		if err != nil {
			return nil, err
		}
	}
	return candidates, nil
}

func applyReleaseBackfillCandidate(
	ctx context.Context,
	request types.ReleaseBackfillRequest,
	sourceReleaseBundleID uuid.UUID,
	selectedEvidence types.ReleaseBackfillArtifactEvidence,
) (string, error) {
	outcome := ""
	err := RunTx(ctx, func(ctx context.Context) error {
		source, err := getReleaseBundle(ctx, sourceReleaseBundleID, request.OrganizationID, true)
		if err != nil {
			return err
		}
		exists, err := releaseBackfillLineageExists(ctx, request.OrganizationID, source.ID)
		if err != nil {
			return err
		}
		if exists {
			outcome = "already_present"
			return nil
		}
		projection := projectLegacyReleaseBundleV2(
			*source,
			[]types.ReleaseBackfillArtifactEvidence{selectedEvidence},
		)
		if projection.ReasonCode != "" {
			if err := insertReleaseBackfillLineage(
				ctx,
				request,
				*source,
				nil,
				projection.ReasonCode,
				selectedEvidence,
			); err != nil {
				return err
			}
			outcome = "blocked"
			return nil
		}
		if err := createReleaseBundle(ctx, &projection.Bundle); err != nil {
			return err
		}
		if err := insertReleaseBackfillLineage(
			ctx,
			request,
			*source,
			&projection.Bundle,
			"",
			selectedEvidence,
		); err != nil {
			return err
		}
		outcome = "derived"
		return nil
	})
	return outcome, err
}

func projectLegacyReleaseBundleV2(
	source types.ReleaseBundle,
	artifactEvidence []types.ReleaseBackfillArtifactEvidence,
) releaseBackfillProjection {
	blocked := func(reason string) releaseBackfillProjection {
		return releaseBackfillProjection{ReasonCode: reason}
	}
	if source.Status == types.ReleaseBundleStatusDraft ||
		source.Status == types.ReleaseBundleStatusValidating ||
		source.CanonicalChecksum == "" ||
		len(source.CanonicalPayload) == 0 {
		return blocked(releaseBackfillBlockedMutableSource)
	}
	canonicalPayload, canonicalChecksum, err := releasebundles.Canonicalize(source)
	if err != nil ||
		canonicalChecksum != source.CanonicalChecksum ||
		!bytes.Equal(canonicalPayload, source.CanonicalPayload) {
		return blocked(releaseBackfillBlockedSourceChecksum)
	}
	if source.ReleaseContract == nil || source.ReleaseContract.ComponentV2 != nil ||
		source.ReleaseContract.Schema != types.ReleaseContractSchemaV1 {
		return blocked(releaseBackfillBlockedMissingContract)
	}
	legacy := source.ReleaseContract
	if len(legacy.Components) != 1 || len(source.Components) != 1 {
		return blocked(releaseBackfillBlockedAmbiguousComponents)
	}
	if legacy.Operations.MigrationRequired ||
		legacy.Operations.ConfigChangeRequired ||
		legacy.Config.RepositoryCommit != "" ||
		legacy.Config.ComposePath != "" ||
		legacy.Config.ServiceConfigPath != "" ||
		legacy.Config.ComposeChecksum != "" ||
		legacy.Config.ServiceConfigChecksum != "" ||
		len(legacy.Config.ImmutableObjects) > 0 {
		return blocked(releaseBackfillBlockedAmbiguousOperations)
	}
	if len(legacy.Compatibility.Requires) > 0 ||
		len(legacy.Compatibility.AffectedComponents) > 0 ||
		len(legacy.Components[0].Contracts) > 0 {
		return blocked(releaseBackfillBlockedAmbiguousCapabilities)
	}
	legacyComponent := legacy.Components[0]
	bundleComponent := source.Components[0]
	if legacyComponent.Name != bundleComponent.Key ||
		legacyComponent.Version != bundleComponent.Version ||
		legacyComponent.Platform == "" ||
		(legacyComponent.Platform != "linux/amd64" && legacyComponent.Platform != "linux/arm64") {
		return blocked(releaseBackfillBlockedAmbiguousComponents)
	}
	repository := legacy.Source.Repository
	requestedRef := legacy.Source.Branch
	if repository == "" ||
		strings.TrimSpace(repository) != repository ||
		requestedRef == "" ||
		strings.TrimSpace(requestedRef) != requestedRef ||
		(source.SourceRepository != "" && source.SourceRepository != repository) ||
		(source.SourceBranch != "" && source.SourceBranch != requestedRef) ||
		(source.SourceRevision != "" && source.SourceRevision != legacy.Source.BuiltCommit) ||
		legacy.Source.SourceCommit != legacy.Source.BuiltCommit ||
		!releaseBackfillCommitPattern.MatchString(legacy.Source.BuiltCommit) {
		return blocked(releaseBackfillBlockedInvalidSource)
	}
	buildID := legacy.Build.ExternalID
	if buildID == "" {
		buildID = source.CIRunID
	}
	builder := source.CIProvider
	buildURL := legacy.Build.ExternalURL
	if buildURL == "" {
		buildURL = source.CIRunURL
	}
	if buildID == "" ||
		strings.TrimSpace(buildID) != buildID ||
		builder == "" ||
		strings.TrimSpace(builder) != builder ||
		(source.CIRunID != "" && source.CIRunID != buildID) ||
		(source.CIRunURL != "" && buildURL != "" && source.CIRunURL != buildURL) {
		return blocked(releaseBackfillBlockedInvalidBuild)
	}
	if !releasebundles.IsSHA256Digest(bundleComponent.Digest) ||
		legacyComponent.Image != bundleComponent.PackageRef+"@"+bundleComponent.Digest {
		return blocked(releaseBackfillBlockedInvalidArtifact)
	}
	observedArtifact, ok := exactReleaseBackfillArtifactEvidence(
		source,
		bundleComponent,
		artifactEvidence,
	)
	if !ok {
		return blocked(releaseBackfillBlockedAmbiguousArtifactMediaType)
	}
	artifactType := releaseBackfillArtifactType(bundleComponent.Type, observedArtifact.MediaType)
	if artifactType == "" {
		return blocked(releaseBackfillBlockedAmbiguousArtifactMediaType)
	}
	if strings.TrimSpace(legacy.Changes.Summary) == "" {
		return blocked(releaseBackfillBlockedInvalidChanges)
	}
	for _, commit := range legacy.Changes.Commits {
		if !releaseBackfillCommitPattern.MatchString(commit) {
			return blocked(releaseBackfillBlockedInvalidChanges)
		}
	}
	contract := types.ComponentReleaseContractV2{
		Schema:       types.ReleaseContractSchemaV2,
		ComponentKey: legacyComponent.Name,
		Version:      legacyComponent.Version,
		Source: types.ComponentReleaseSource{
			Repository: repository, RequestedRef: requestedRef, Commit: legacy.Source.BuiltCommit,
		},
		Build: types.ComponentReleaseBuild{ID: buildID, Builder: builder},
		Artifacts: []types.ComponentReleaseArtifact{{
			Key: legacyComponent.Name, Type: artifactType,
			MediaType: observedArtifact.MediaType, Digest: bundleComponent.Digest,
			Platforms: []types.ComponentReleasePlatform{{
				Platform: legacyComponent.Platform, Digest: bundleComponent.Digest,
			}},
		}},
		Changes: types.ComponentReleaseChanges{
			Summary: legacy.Changes.Summary, Commits: append([]string(nil), legacy.Changes.Commits...),
		},
	}
	if len(releasebundles.ValidateComponentReleaseContractV2(contract)) > 0 {
		return blocked(releaseBackfillBlockedInvalidV2Contract)
	}
	derivedComponent := bundleComponent
	derivedComponent.ID = uuid.Nil
	derivedComponent.ReleaseBundleID = uuid.Nil
	return releaseBackfillProjection{Bundle: types.ReleaseBundle{
		OrganizationID:        source.OrganizationID,
		ApplicationID:         source.ApplicationID,
		ChannelID:             source.ChannelID,
		ProcessSnapshotID:     source.ProcessSnapshotID,
		ReleaseNumber:         source.ReleaseNumber + "-v2-" + source.ID.String(),
		ReleaseNotes:          source.ReleaseNotes,
		SourceRevision:        legacy.Source.BuiltCommit,
		SourceRepository:      repository,
		SourceBranch:          requestedRef,
		SourceTag:             source.SourceTag,
		CIProvider:            builder,
		CIRunID:               buildID,
		CIRunURL:              buildURL,
		ReleaseContract:       &types.ReleaseContract{Schema: types.ReleaseContractSchemaV2, ComponentV2: &contract},
		Kind:                  types.ReleaseBundleKindComponent,
		ReleaseContractSchema: types.ReleaseContractSchemaV2,
		Status:                types.ReleaseBundleStatusDraft,
		Components:            []types.ReleaseBundleComponent{derivedComponent},
	}}
}

func releaseBackfillArtifactType(componentType types.ReleaseBundleComponentType, mediaType string) string {
	switch componentType {
	case types.ReleaseBundleComponentTypeOCIImage:
		if mediaType == "application/vnd.oci.image.index.v1+json" ||
			mediaType == "application/vnd.oci.image.manifest.v1+json" {
			return "oci-image"
		}
	case types.ReleaseBundleComponentTypeOCIArtifact:
		if mediaType == "application/vnd.oci.artifact.manifest.v1+json" {
			return "oci-artifact"
		}
	case types.ReleaseBundleComponentTypeHelmChart:
		if mediaType == "application/vnd.cncf.helm.chart.content.v1.tar+gzip" {
			return "helm-chart"
		}
	}
	return ""
}

func singleReleaseBackfillArtifactEvidence(
	sourceReleaseBundleID uuid.UUID,
	evidence []types.ReleaseBackfillArtifactEvidence,
) (types.ReleaseBackfillArtifactEvidence, bool) {
	var selected types.ReleaseBackfillArtifactEvidence
	matches := 0
	for _, item := range evidence {
		if item.SourceReleaseBundleID == sourceReleaseBundleID {
			selected = item
			matches++
		}
	}
	return selected, matches == 1
}

func exactReleaseBackfillArtifactEvidence(
	source types.ReleaseBundle,
	component types.ReleaseBundleComponent,
	evidence []types.ReleaseBackfillArtifactEvidence,
) (types.ReleaseBackfillArtifactEvidence, bool) {
	var matched types.ReleaseBackfillArtifactEvidence
	matches := 0
	for _, item := range evidence {
		if item.SourceReleaseBundleID != source.ID || item.ArtifactKey != component.Key {
			continue
		}
		matches++
		matched = item
	}
	if matches != 1 ||
		matched.ArtifactDigest != component.Digest ||
		!safeReleaseBackfillEvidenceText(matched.Reference, 2048) ||
		!releasebundles.IsSHA256Digest(matched.EvidenceDigest) ||
		releaseBackfillArtifactType(component.Type, matched.MediaType) == "" {
		return types.ReleaseBackfillArtifactEvidence{}, false
	}
	return matched, true
}

func cloneReleaseBackfillCursor(cursor *types.ReleaseBackfillCursor) *types.ReleaseBackfillCursor {
	if cursor == nil {
		return nil
	}
	cloned := *cursor
	return &cloned
}

func ensureReleaseBackfillCheckpoint(
	ctx context.Context,
	request types.ReleaseBackfillRequest,
) error {
	return RunTx(ctx, func(ctx context.Context) error {
		db := internalctx.GetDb(ctx)
		if _, err := db.Exec(ctx, `
			INSERT INTO ReleaseContractV2BackfillCheckpoint (
				organization_id,
				checkpoint_id,
				evidence_document_reference,
				evidence_document_checksum
			) VALUES (
				@organizationId,
				@checkpointId,
				@evidenceDocumentReference,
				@evidenceDocumentChecksum
			)
			ON CONFLICT (organization_id, checkpoint_id) DO NOTHING`,
			pgx.NamedArgs{
				"organizationId":            request.OrganizationID,
				"checkpointId":              request.CheckpointID,
				"evidenceDocumentReference": request.EvidenceDocumentReference,
				"evidenceDocumentChecksum":  request.EvidenceDocumentChecksum,
			},
		); err != nil {
			return fmt.Errorf("could not record release contract v2 backfill checkpoint: %w", err)
		}

		var existing types.ReleaseBackfillCheckpoint
		if err := db.QueryRow(ctx, `
			SELECT
				id,
				created_at,
				organization_id,
				checkpoint_id,
				evidence_document_reference,
				evidence_document_checksum
			FROM ReleaseContractV2BackfillCheckpoint
			WHERE organization_id = @organizationId
			  AND checkpoint_id = @checkpointId`,
			pgx.NamedArgs{
				"organizationId": request.OrganizationID,
				"checkpointId":   request.CheckpointID,
			},
		).Scan(
			&existing.ID,
			&existing.CreatedAt,
			&existing.OrganizationID,
			&existing.CheckpointID,
			&existing.EvidenceDocumentReference,
			&existing.EvidenceDocumentChecksum,
		); err != nil {
			return fmt.Errorf("could not query release contract v2 backfill checkpoint: %w", err)
		}
		if !sameReleaseBackfillCheckpointBinding(existing, request) {
			return fmt.Errorf(
				"release contract v2 backfill checkpoint evidence document changed: %w",
				apierrors.ErrConflict,
			)
		}
		return nil
	})
}

func sameReleaseBackfillCheckpointBinding(
	checkpoint types.ReleaseBackfillCheckpoint,
	request types.ReleaseBackfillRequest,
) bool {
	return checkpoint.OrganizationID == request.OrganizationID &&
		checkpoint.CheckpointID == request.CheckpointID &&
		checkpoint.EvidenceDocumentReference == request.EvidenceDocumentReference &&
		checkpoint.EvidenceDocumentChecksum == request.EvidenceDocumentChecksum
}

func releaseBackfillLineageExists(
	ctx context.Context,
	organizationID uuid.UUID,
	sourceReleaseBundleID uuid.UUID,
) (bool, error) {
	db := internalctx.GetDb(ctx)
	var exists bool
	if err := db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM ReleaseContractV2BackfillLineage
			WHERE organization_id = @organizationId
			  AND source_release_bundle_id = @sourceReleaseBundleId
		)`,
		pgx.NamedArgs{
			"organizationId":        organizationID,
			"sourceReleaseBundleId": sourceReleaseBundleID,
		},
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("could not query release contract v2 backfill lineage: %w", err)
	}
	return exists, nil
}

func insertReleaseBackfillLineage(
	ctx context.Context,
	request types.ReleaseBackfillRequest,
	source types.ReleaseBundle,
	derived *types.ReleaseBundle,
	reasonCode string,
	selectedEvidence types.ReleaseBackfillArtifactEvidence,
) error {
	var derivedID any
	derivedChecksum := ""
	state := types.ReleaseBackfillLineageStateBlocked
	if derived != nil {
		derivedID = derived.ID
		derivedChecksum = derived.CanonicalChecksum
		state = types.ReleaseBackfillLineageStateDerived
	}
	db := internalctx.GetDb(ctx)
	_, err := db.Exec(ctx, `
		INSERT INTO ReleaseContractV2BackfillLineage (
			organization_id,
			checkpoint_id,
			source_release_bundle_id,
			source_canonical_checksum,
			derived_release_bundle_id,
			derived_canonical_checksum,
			state,
			reason_code,
			reviewed_artifact_key,
			reviewed_artifact_digest,
			artifact_media_type,
			artifact_evidence_reference,
			artifact_evidence_digest
		) VALUES (
			@organizationId,
			@checkpointId,
			@sourceReleaseBundleId,
			@sourceCanonicalChecksum,
			@derivedReleaseBundleId,
			@derivedCanonicalChecksum,
			@state,
			@reasonCode,
			@reviewedArtifactKey,
			@reviewedArtifactDigest,
			@artifactMediaType,
			@artifactEvidenceReference,
			@artifactEvidenceDigest
		)`,
		pgx.NamedArgs{
			"organizationId":            request.OrganizationID,
			"checkpointId":              request.CheckpointID,
			"sourceReleaseBundleId":     source.ID,
			"sourceCanonicalChecksum":   source.CanonicalChecksum,
			"derivedReleaseBundleId":    derivedID,
			"derivedCanonicalChecksum":  derivedChecksum,
			"state":                     state,
			"reasonCode":                reasonCode,
			"reviewedArtifactKey":       selectedEvidence.ArtifactKey,
			"reviewedArtifactDigest":    selectedEvidence.ArtifactDigest,
			"artifactMediaType":         selectedEvidence.MediaType,
			"artifactEvidenceReference": selectedEvidence.Reference,
			"artifactEvidenceDigest":    selectedEvidence.EvidenceDigest,
		},
	)
	if err != nil {
		return fmt.Errorf("could not record release contract v2 backfill lineage: %w", err)
	}
	return nil
}
