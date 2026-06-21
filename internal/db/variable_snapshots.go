package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/variabledrift"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	variableSnapshotOutputExpr = `
		vs.id,
		vs.created_at,
		vs.organization_id,
		vs.release_bundle_id,
		vs.application_id,
		vs.channel_id,
		vs.canonical_checksum,
		vs.canonical_payload
	`
)

type canonicalVariableSnapshot struct {
	ReleaseBundleID string                           `json:"releaseBundleId"`
	ApplicationID   string                           `json:"applicationId"`
	ChannelID       string                           `json:"channelId"`
	Values          []canonicalVariableSnapshotValue `json:"values"`
}

type canonicalVariableSnapshotValue struct {
	VariableSetID string                               `json:"variableSetId"`
	VariableID    string                               `json:"variableId"`
	Key           string                               `json:"key"`
	Type          string                               `json:"type"`
	IsRequired    bool                                 `json:"isRequired"`
	Status        string                               `json:"status"`
	Source        string                               `json:"source"`
	Value         json.RawMessage                      `json:"value,omitempty"`
	ReferenceID   string                               `json:"referenceId,omitempty"`
	ReferenceName string                               `json:"referenceName,omitempty"`
	Redacted      bool                                 `json:"redacted"`
	Trace         []types.VariableResolutionTraceEntry `json:"trace"`
}

func GetVariableSnapshot(ctx context.Context, id, orgID uuid.UUID) (*types.VariableSnapshot, error) {
	return getVariableSnapshot(ctx, id, orgID)
}

func GetVariableSnapshotForReleaseBundle(
	ctx context.Context,
	releaseBundleID uuid.UUID,
	orgID uuid.UUID,
) (*types.VariableSnapshot, error) {
	db := internalctx.GetDb(ctx)
	var snapshotID uuid.UUID
	err := db.QueryRow(ctx,
		`SELECT variable_snapshot_id
		FROM ReleaseBundle
		WHERE id = @releaseBundleId
			AND organization_id = @organizationId
			AND variable_snapshot_id IS NOT NULL`,
		pgx.NamedArgs{"releaseBundleId": releaseBundleID, "organizationId": orgID},
	).Scan(&snapshotID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("could not query ReleaseBundle variable snapshot: %w", err)
	}
	return getVariableSnapshot(ctx, snapshotID, orgID)
}

func getVariableSnapshot(ctx context.Context, id, orgID uuid.UUID) (*types.VariableSnapshot, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+variableSnapshotOutputExpr+`
		FROM VariableSnapshot vs
		WHERE vs.id = @id AND vs.organization_id = @organizationId`,
		pgx.NamedArgs{"id": id, "organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query VariableSnapshot: %w", err)
	}
	snapshot, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.VariableSnapshot])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("could not collect VariableSnapshot: %w", err)
	}
	snapshot.Values, err = getVariableSnapshotValues(ctx, snapshot.ID, snapshot.OrganizationID)
	if err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func createVariableSnapshotForReleaseBundle(
	ctx context.Context,
	bundle types.ReleaseBundle,
) (*types.VariableSnapshot, error) {
	variableSets, err := getVariableSetsForApplication(ctx, bundle.OrganizationID, bundle.ApplicationID)
	if err != nil {
		return nil, err
	}
	variableSetIDs := make([]uuid.UUID, 0, len(variableSets))
	for _, variableSet := range variableSets {
		variableSetIDs = append(variableSetIDs, variableSet.ID)
	}

	resolved := []types.ResolvedVariable{}
	if len(variableSetIDs) > 0 {
		resolved, err = ResolveVariablesPreview(ctx, bundle.OrganizationID, variableSetIDs, types.VariableResolutionScope{
			ApplicationID: &bundle.ApplicationID,
			ChannelID:     &bundle.ChannelID,
		}, nil)
		if err != nil {
			return nil, err
		}
	}

	payload, checksum, err := canonicalizeVariableSnapshot(bundle, resolved)
	if err != nil {
		return nil, err
	}

	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`INSERT INTO VariableSnapshot AS vs (
			organization_id,
			release_bundle_id,
			application_id,
			channel_id,
			canonical_checksum,
			canonical_payload
		) VALUES (
			@organizationId,
			@releaseBundleId,
			@applicationId,
			@channelId,
			@canonicalChecksum,
			@canonicalPayload
		)
		RETURNING `+variableSnapshotOutputExpr,
		pgx.NamedArgs{
			"organizationId":    bundle.OrganizationID,
			"releaseBundleId":   bundle.ID,
			"applicationId":     bundle.ApplicationID,
			"channelId":         bundle.ChannelID,
			"canonicalChecksum": checksum,
			"canonicalPayload":  payload,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not insert VariableSnapshot: %w", err)
	}
	snapshot, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.VariableSnapshot])
	if err != nil {
		return nil, fmt.Errorf("could not collect VariableSnapshot: %w", err)
	}
	if err := replaceVariableSnapshotValues(ctx, snapshot.ID, bundle.OrganizationID, resolved); err != nil {
		return nil, err
	}
	snapshot.Values, err = getVariableSnapshotValues(ctx, snapshot.ID, snapshot.OrganizationID)
	if err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func getVariableSetsForApplication(ctx context.Context, orgID, applicationID uuid.UUID) ([]types.VariableSet, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+variableSetOutputExpr+`
		FROM VariableSet vs
		JOIN VariableSetApplication vsa
			ON vsa.variable_set_id = vs.id
			AND vsa.organization_id = vs.organization_id
		WHERE vs.organization_id = @organizationId
			AND vsa.application_id = @applicationId
		ORDER BY vs.sort_order, vs.name, vs.id`,
		pgx.NamedArgs{"organizationId": orgID, "applicationId": applicationID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query application VariableSets: %w", err)
	}
	result, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.VariableSet])
	if err != nil {
		return nil, fmt.Errorf("could not collect application VariableSets: %w", err)
	}
	for i := range result {
		if err := hydrateVariableSet(ctx, &result[i]); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func replaceVariableSnapshotValues(
	ctx context.Context,
	snapshotID uuid.UUID,
	orgID uuid.UUID,
	resolved []types.ResolvedVariable,
) error {
	db := internalctx.GetDb(ctx)
	if _, err := db.Exec(ctx,
		`DELETE FROM VariableSnapshotValue
		WHERE variable_snapshot_id = @snapshotId AND organization_id = @organizationId`,
		pgx.NamedArgs{"snapshotId": snapshotID, "organizationId": orgID},
	); err != nil {
		return fmt.Errorf("could not replace VariableSnapshotValue rows: %w", err)
	}
	if len(resolved) == 0 {
		return nil
	}
	_, err := db.CopyFrom(
		ctx,
		pgx.Identifier{"variablesnapshotvalue"},
		[]string{
			"variable_snapshot_id",
			"organization_id",
			"variable_set_id",
			"variable_id",
			"key",
			"type",
			"is_required",
			"status",
			"source",
			"value",
			"reference_id",
			"reference_name",
			"redacted",
			"trace",
		},
		pgx.CopyFromSlice(len(resolved), func(i int) ([]any, error) {
			value := resolved[i]
			trace, err := json.Marshal(value.Trace)
			if err != nil {
				return nil, err
			}
			return []any{
				snapshotID,
				orgID,
				value.VariableSetID,
				value.VariableID,
				value.Key,
				value.Type,
				value.IsRequired,
				value.Status,
				value.Source,
				variableSnapshotJSONValue(value),
				value.ReferenceID,
				value.ReferenceName,
				value.Redacted,
				trace,
			}, nil
		}),
	)
	if err != nil {
		return fmt.Errorf("could not insert VariableSnapshotValue rows: %w", err)
	}
	return nil
}

func getVariableSnapshotValues(
	ctx context.Context,
	snapshotID uuid.UUID,
	orgID uuid.UUID,
) ([]types.VariableSnapshotValue, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT
			id,
			variable_snapshot_id,
			organization_id,
			variable_set_id,
			variable_id,
			key,
			type,
			is_required,
			status,
			source,
			value,
			reference_id,
			reference_name,
			redacted,
			trace
		FROM VariableSnapshotValue
		WHERE variable_snapshot_id = @snapshotId AND organization_id = @organizationId
		ORDER BY key, variable_set_id, variable_id`,
		pgx.NamedArgs{"snapshotId": snapshotID, "organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query VariableSnapshotValue: %w", err)
	}
	defer rows.Close()

	result := []types.VariableSnapshotValue{}
	for rows.Next() {
		var value types.VariableSnapshotValue
		var rawValue json.RawMessage
		var trace json.RawMessage
		if err := rows.Scan(
			&value.ID,
			&value.VariableSnapshotID,
			&value.OrganizationID,
			&value.VariableSetID,
			&value.VariableID,
			&value.Key,
			&value.Type,
			&value.IsRequired,
			&value.Status,
			&value.Source,
			&rawValue,
			&value.ReferenceID,
			&value.ReferenceName,
			&value.Redacted,
			&trace,
		); err != nil {
			return nil, fmt.Errorf("could not scan VariableSnapshotValue: %w", err)
		}
		if hasVariableJSONValue(rawValue) {
			value.Value = rawValue
		}
		if len(trace) > 0 {
			if err := json.Unmarshal(trace, &value.Trace); err != nil {
				return nil, fmt.Errorf("could not decode VariableSnapshotValue trace: %w", err)
			}
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("could not iterate VariableSnapshotValue: %w", err)
	}
	return result, nil
}

func variableSnapshotJSONValue(value types.ResolvedVariable) any {
	if value.Redacted || !hasVariableJSONValue(value.Value) {
		return nil
	}
	return value.Value
}

func canonicalizeVariableSnapshot(
	bundle types.ReleaseBundle,
	values []types.ResolvedVariable,
) ([]byte, string, error) {
	canonicalValues := make([]canonicalVariableSnapshotValue, 0, len(values))
	for _, value := range values {
		canonicalValues = append(canonicalValues, canonicalVariableSnapshotValue{
			VariableSetID: value.VariableSetID.String(),
			VariableID:    value.VariableID.String(),
			Key:           value.Key,
			Type:          string(value.Type),
			IsRequired:    value.IsRequired,
			Status:        string(value.Status),
			Source:        string(value.Source),
			Value:         variableSnapshotCanonicalValue(value),
			ReferenceID:   value.ReferenceID,
			ReferenceName: value.ReferenceName,
			Redacted:      value.Redacted,
			Trace:         value.Trace,
		})
	}
	sort.Slice(canonicalValues, func(i, j int) bool {
		if canonicalValues[i].Key != canonicalValues[j].Key {
			return canonicalValues[i].Key < canonicalValues[j].Key
		}
		if canonicalValues[i].VariableSetID != canonicalValues[j].VariableSetID {
			return canonicalValues[i].VariableSetID < canonicalValues[j].VariableSetID
		}
		return canonicalValues[i].VariableID < canonicalValues[j].VariableID
	})
	canonical := canonicalVariableSnapshot{
		ReleaseBundleID: bundle.ID.String(),
		ApplicationID:   bundle.ApplicationID.String(),
		ChannelID:       bundle.ChannelID.String(),
		Values:          canonicalValues,
	}
	payload, err := json.Marshal(canonical)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(payload)
	return payload, "sha256:" + hex.EncodeToString(sum[:]), nil
}

func variableSnapshotCanonicalValue(value types.ResolvedVariable) json.RawMessage {
	if value.Redacted || !hasVariableJSONValue(value.Value) {
		return nil
	}
	clone := make([]byte, len(value.Value))
	copy(clone, value.Value)
	return clone
}

func GetDeploymentConfigurationDrift(
	ctx context.Context,
	deploymentID uuid.UUID,
	orgID uuid.UUID,
) (types.ConfigurationDrift, error) {
	deployment, err := getDeploymentWithLatestRevision(ctx, deploymentID, orgID)
	if err != nil {
		return types.ConfigurationDrift{}, err
	}
	target, err := GetDeploymentTargetForDeploymentID(ctx, deploymentID)
	if err != nil {
		return types.ConfigurationDrift{}, err
	}
	if target.OrganizationID != orgID {
		return types.ConfigurationDrift{}, apierrors.ErrNotFound
	}
	applicationID := deployment.Application.ID
	variableSets, err := getVariableSetsForApplication(ctx, orgID, applicationID)
	if err != nil {
		return types.ConfigurationDrift{}, err
	}
	variableSetIDs := make([]uuid.UUID, 0, len(variableSets))
	for _, variableSet := range variableSets {
		variableSetIDs = append(variableSetIDs, variableSet.ID)
	}

	resolved := []types.ResolvedVariable{}
	if len(variableSetIDs) > 0 {
		resolved, err = ResolveVariablesPreview(ctx, orgID, variableSetIDs, types.VariableResolutionScope{
			CustomerOrganizationID: target.CustomerOrganizationID,
			DeploymentTargetID:     &target.ID,
			ApplicationID:          &applicationID,
		}, nil)
		if err != nil {
			return types.ConfigurationDrift{}, err
		}
	}
	drift, err := variabledrift.Compare(resolved, variabledrift.DeployedConfiguration{
		EnvFileData: deployment.EnvFileData,
		ValuesYAML:  deployment.ValuesYaml,
	})
	if err != nil {
		return types.ConfigurationDrift{}, err
	}
	drift.DeploymentID = deployment.ID
	drift.ApplicationID = applicationID
	return drift, nil
}

func getDeploymentWithLatestRevision(
	ctx context.Context,
	deploymentID uuid.UUID,
	orgID uuid.UUID,
) (*types.DeploymentWithLatestRevision, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT`+deploymentOutputExpr+`,
				dr.application_version_id AS application_version_id,
				dr.values_yaml AS values_yaml,
				dr.env_file_data AS env_file_data,
				dr.values_hash AS values_hash,
				dr.id AS deployment_revision_id,
				dr.created_at AS deployment_revision_created_at,
				dr.force_restart AS force_restart,
				dr.ignore_revision_skew AS ignore_revision_skew,
				CASE WHEN dr.helm_options_timeout IS NOT NULL THEN (
					dr.helm_options_timeout,
					dr.helm_options_wait_strategy,
					dr.helm_options_rollback_on_failure,
					dr.helm_options_cleanup_on_failure,
					dr.helm_options_force_conflicts
				) END AS helm_options,
				a.id AS application_id,
				a.name AS application_name,
				(`+applicationOutputExpr+`) AS application,
				av.name AS application_version_name,
				av.link_template AS application_link_template,
				CASE WHEN drs.id IS NOT NULL THEN (
					drs.id,
					drs.created_at,
					drs.deployment_revision_id,
					drs.type, drs.message
				) END AS latest_status
			FROM `+deploymentWithLatestRevisionFromExpr+`
			JOIN DeploymentTarget dt ON dt.id = d.deployment_target_id
			WHERE d.id = @deploymentId AND dt.organization_id = @organizationId`,
		pgx.NamedArgs{"deploymentId": deploymentID, "organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query Deployment: %w", err)
	}
	result, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.DeploymentWithLatestRevision])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("failed to scan Deployment: %w", err)
	}
	if err := TemplateDeploymentLinks([]types.DeploymentWithLatestRevision{result}); err != nil {
		return nil, fmt.Errorf("failed to template deployment link: %w", err)
	}
	return &result, nil
}
