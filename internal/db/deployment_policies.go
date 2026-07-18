package db

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/governance"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var deploymentPolicyResourceKeyPattern = regexp.MustCompile(
	`^[a-z0-9]+([._-][a-z0-9]+)*$`,
)

const (
	deploymentPolicyDefaultPageLimit  = 50
	deploymentPolicyMaximumPageLimit  = 100
	deploymentPolicyMaximumCursorSize = 2048
	deploymentPolicyCursorVersion     = 1

	deploymentPolicyCursorResourcePolicies = "policies"
	deploymentPolicyCursorResourceVersions = "versions"
	deploymentPolicyCursorResourceBindings = "bindings"
)

type deploymentPolicyCursor struct {
	Version   int       `json:"v"`
	Resource  string    `json:"resource"`
	ParentID  uuid.UUID `json:"parentId"`
	CreatedAt time.Time `json:"createdAt"`
	ID        uuid.UUID `json:"id"`
}

const deploymentPolicyOutputExpr = `
	policy.id,
	policy.created_at,
	policy.updated_at,
	policy.organization_id,
	policy.key,
	policy.name,
	policy.description
`

const deploymentPolicyVersionOutputExpr = `
	version.id,
	version.created_at,
	version.updated_at,
	version.organization_id,
	version.deployment_policy_id,
	version.version_number,
	version.state,
	version.document,
	version.canonical_checksum,
	version.canonical_payload,
	version.created_by_useraccount_id,
	version.published_by_useraccount_id,
	version.published_at
`

const deploymentPolicyVersionSummaryOutputExpr = `
	version.id,
	version.created_at,
	version.updated_at,
	version.organization_id,
	version.deployment_policy_id,
	version.version_number,
	version.state,
	version.canonical_checksum,
	version.created_by_useraccount_id,
	version.published_by_useraccount_id,
	version.published_at
`

const deploymentPolicyBindingOutputExpr = `
	binding.id,
	binding.created_at,
	binding.organization_id,
	binding.deployment_policy_version_id,
	binding.scope_kind,
	binding.scope_id,
	binding.binding_role,
	binding.created_by_useraccount_id,
	binding.retired_at
`

func CreateDeploymentPolicy(
	ctx context.Context,
	policy *types.DeploymentPolicy,
) error {
	if policy == nil {
		return apierrors.NewBadRequest("deployment policy is required")
	}
	if err := validateDeploymentPolicyForWrite(*policy); err != nil {
		return err
	}
	policy.Key = strings.TrimSpace(policy.Key)
	policy.Name = strings.TrimSpace(policy.Name)
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		INSERT INTO DeploymentPolicy AS policy (
			organization_id,
			key,
			name,
			description
		) VALUES (
			@organizationID,
			@key,
			@name,
			@description
		)
		RETURNING `+deploymentPolicyOutputExpr,
		pgx.NamedArgs{
			"organizationID": policy.OrganizationID,
			"key":            policy.Key,
			"name":           policy.Name,
			"description":    policy.Description,
		},
	)
	if err != nil {
		return mapDeploymentPolicyWriteError("create deployment policy", err)
	}
	result, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.DeploymentPolicy],
	)
	if err != nil {
		return mapDeploymentPolicyWriteError("read created deployment policy", err)
	}
	*policy = result
	return nil
}

func ListDeploymentPolicies(
	ctx context.Context,
	filter types.DeploymentPolicyListFilter,
) (types.Page[types.DeploymentPolicy], error) {
	return listDeploymentPolicyEntities(
		ctx,
		filter,
		deploymentPolicyCursorResourcePolicies,
		uuid.Nil,
		"DeploymentPolicy",
		"policy",
		deploymentPolicyOutputExpr,
		"",
		nil,
		func(policy types.DeploymentPolicy) (time.Time, uuid.UUID) {
			return policy.CreatedAt, policy.ID
		},
	)
}

func GetDeploymentPolicy(
	ctx context.Context,
	id uuid.UUID,
	organizationID uuid.UUID,
) (*types.DeploymentPolicy, error) {
	return getDeploymentPolicy(ctx, id, organizationID, false)
}

func getDeploymentPolicy(
	ctx context.Context,
	id uuid.UUID,
	organizationID uuid.UUID,
	forUpdate bool,
) (*types.DeploymentPolicy, error) {
	lockClause := ""
	if forUpdate {
		lockClause = " FOR UPDATE"
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+deploymentPolicyOutputExpr+`
		FROM DeploymentPolicy policy
		WHERE policy.id = @id
		  AND policy.organization_id = @organizationID
	`+lockClause,
		pgx.NamedArgs{"id": id, "organizationID": organizationID},
	)
	if err != nil {
		return nil, fmt.Errorf("get deployment policy: %w", err)
	}
	result, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.DeploymentPolicy],
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("collect deployment policy: %w", err)
	}
	return &result, nil
}

func UpdateDeploymentPolicy(
	ctx context.Context,
	policy *types.DeploymentPolicy,
) error {
	if policy == nil {
		return apierrors.NewBadRequest("deployment policy is required")
	}
	if err := validateDeploymentPolicyForWrite(*policy); err != nil {
		return err
	}
	policy.Name = strings.TrimSpace(policy.Name)
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		UPDATE DeploymentPolicy AS policy
		SET
			name = @name,
			description = @description,
			updated_at = now()
		WHERE policy.id = @id
		  AND policy.organization_id = @organizationID
		RETURNING `+deploymentPolicyOutputExpr,
		pgx.NamedArgs{
			"id":             policy.ID,
			"organizationID": policy.OrganizationID,
			"name":           policy.Name,
			"description":    policy.Description,
		},
	)
	if err != nil {
		return mapDeploymentPolicyWriteError("update deployment policy", err)
	}
	result, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.DeploymentPolicy],
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return apierrors.ErrNotFound
	}
	if err != nil {
		return mapDeploymentPolicyWriteError("read updated deployment policy", err)
	}
	*policy = result
	return nil
}

func DeleteDeploymentPolicy(
	ctx context.Context,
	id uuid.UUID,
	organizationID uuid.UUID,
) error {
	command, err := internalctx.GetDb(ctx).Exec(ctx, `
		DELETE FROM DeploymentPolicy
		WHERE id = @id
		  AND organization_id = @organizationID
		  AND NOT EXISTS (
		    SELECT 1
		    FROM DeploymentPolicyVersion version
		    WHERE version.deployment_policy_id = DeploymentPolicy.id
		      AND version.state = 'PUBLISHED'
		  )
	`,
		pgx.NamedArgs{"id": id, "organizationID": organizationID},
	)
	if err != nil {
		return mapDeploymentPolicyWriteError("delete deployment policy", err)
	}
	if command.RowsAffected() == 0 {
		if _, getErr := GetDeploymentPolicy(ctx, id, organizationID); getErr == nil {
			return apierrors.NewConflict(
				"deployment policy has immutable published versions",
			)
		}
		return apierrors.ErrNotFound
	}
	return nil
}

func CreateDeploymentPolicyVersion(
	ctx context.Context,
	version *types.DeploymentPolicyVersion,
) error {
	if version == nil {
		return apierrors.NewBadRequest("deployment policy version is required")
	}
	return RunTx(ctx, func(ctx context.Context) error {
		if version.OrganizationID == uuid.Nil ||
			version.PolicyID == uuid.Nil ||
			version.CreatedByUserAccountID == uuid.Nil {
			return apierrors.NewBadRequest(
				"organizationId, policyId, and createdByUserAccountId are required",
			)
		}
		if _, err := getDeploymentPolicy(
			ctx,
			version.PolicyID,
			version.OrganizationID,
			true,
		); err != nil {
			return err
		}
		if err := ensureDeploymentPolicyActor(
			ctx,
			version.OrganizationID,
			version.CreatedByUserAccountID,
		); err != nil {
			return err
		}
		if err := setDeploymentPolicyVersionCanonicalFields(version); err != nil {
			return err
		}
		version.State = types.DeploymentPolicyVersionStateDraft
		if err := internalctx.GetDb(ctx).QueryRow(ctx, `
			SELECT COALESCE(MAX(version_number), 0) + 1
			FROM DeploymentPolicyVersion
			WHERE deployment_policy_id = @policyID
			  AND organization_id = @organizationID
		`,
			pgx.NamedArgs{
				"policyID":       version.PolicyID,
				"organizationID": version.OrganizationID,
			},
		).Scan(&version.VersionNumber); err != nil {
			return fmt.Errorf("allocate deployment policy version number: %w", err)
		}
		rows, err := internalctx.GetDb(ctx).Query(ctx, `
			INSERT INTO DeploymentPolicyVersion AS version (
				organization_id,
				deployment_policy_id,
				version_number,
				state,
				document,
				canonical_checksum,
				canonical_payload,
				created_by_useraccount_id
			) VALUES (
				@organizationID,
				@policyID,
				@versionNumber,
				@state,
				@document,
				@canonicalChecksum,
				@canonicalPayload,
				@createdByUserAccountID
			)
			RETURNING `+deploymentPolicyVersionOutputExpr,
			pgx.NamedArgs{
				"organizationID":         version.OrganizationID,
				"policyID":               version.PolicyID,
				"versionNumber":          version.VersionNumber,
				"state":                  version.State,
				"document":               version.Document,
				"canonicalChecksum":      version.CanonicalChecksum,
				"canonicalPayload":       version.CanonicalPayload,
				"createdByUserAccountID": version.CreatedByUserAccountID,
			},
		)
		if err != nil {
			return mapDeploymentPolicyWriteError(
				"create deployment policy version",
				err,
			)
		}
		result, err := collectDeploymentPolicyVersion(rows)
		if err != nil {
			return mapDeploymentPolicyWriteError(
				"read created deployment policy version",
				err,
			)
		}
		*version = result
		return nil
	})
}

func ListDeploymentPolicyVersions(
	ctx context.Context,
	policyID uuid.UUID,
	filter types.DeploymentPolicyListFilter,
) (types.Page[types.DeploymentPolicyVersionSummary], error) {
	page := types.Page[types.DeploymentPolicyVersionSummary]{
		Items: []types.DeploymentPolicyVersionSummary{},
	}
	if _, err := GetDeploymentPolicy(ctx, policyID, filter.OrganizationID); err != nil {
		return page, err
	}
	return listDeploymentPolicyEntities(
		ctx,
		filter,
		deploymentPolicyCursorResourceVersions,
		policyID,
		"DeploymentPolicyVersion",
		"version",
		deploymentPolicyVersionSummaryOutputExpr,
		" AND version.deployment_policy_id = @policyID",
		pgx.NamedArgs{"policyID": policyID},
		func(version types.DeploymentPolicyVersionSummary) (time.Time, uuid.UUID) {
			return version.CreatedAt, version.ID
		},
	)
}

func GetDeploymentPolicyVersion(
	ctx context.Context,
	id uuid.UUID,
	organizationID uuid.UUID,
) (*types.DeploymentPolicyVersion, error) {
	return getDeploymentPolicyVersion(ctx, id, organizationID, false)
}

func getDeploymentPolicyVersion(
	ctx context.Context,
	id uuid.UUID,
	organizationID uuid.UUID,
	forUpdate bool,
) (*types.DeploymentPolicyVersion, error) {
	lockClause := ""
	if forUpdate {
		lockClause = " FOR UPDATE"
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+deploymentPolicyVersionOutputExpr+`
		FROM DeploymentPolicyVersion version
		WHERE version.id = @id
		  AND version.organization_id = @organizationID
	`+lockClause,
		pgx.NamedArgs{"id": id, "organizationID": organizationID},
	)
	if err != nil {
		return nil, fmt.Errorf("get deployment policy version: %w", err)
	}
	result, err := collectDeploymentPolicyVersion(rows)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("collect deployment policy version: %w", err)
	}
	return &result, nil
}

func UpdateDeploymentPolicyVersion(
	ctx context.Context,
	version *types.DeploymentPolicyVersion,
) error {
	if version == nil {
		return apierrors.NewBadRequest("deployment policy version is required")
	}
	return RunTx(ctx, func(ctx context.Context) error {
		existing, err := getDeploymentPolicyVersion(
			ctx,
			version.ID,
			version.OrganizationID,
			true,
		)
		if err != nil {
			return err
		}
		if existing.State != types.DeploymentPolicyVersionStateDraft {
			return apierrors.NewConflict(
				"published deployment policy versions are immutable",
			)
		}
		existing.Document = version.Document
		if err := setDeploymentPolicyVersionCanonicalFields(existing); err != nil {
			return err
		}
		rows, err := internalctx.GetDb(ctx).Query(ctx, `
			UPDATE DeploymentPolicyVersion AS version
			SET
				document = @document,
				canonical_checksum = @canonicalChecksum,
				canonical_payload = @canonicalPayload,
				updated_at = now()
			WHERE version.id = @id
			  AND version.organization_id = @organizationID
			  AND version.state = 'DRAFT'
			RETURNING `+deploymentPolicyVersionOutputExpr,
			pgx.NamedArgs{
				"id":                existing.ID,
				"organizationID":    existing.OrganizationID,
				"document":          existing.Document,
				"canonicalChecksum": existing.CanonicalChecksum,
				"canonicalPayload":  existing.CanonicalPayload,
			},
		)
		if err != nil {
			return mapDeploymentPolicyWriteError(
				"update deployment policy version",
				err,
			)
		}
		result, err := collectDeploymentPolicyVersion(rows)
		if errors.Is(err, pgx.ErrNoRows) {
			return apierrors.NewConflict(
				"published deployment policy versions are immutable",
			)
		}
		if err != nil {
			return mapDeploymentPolicyWriteError(
				"read updated deployment policy version",
				err,
			)
		}
		*version = result
		return nil
	})
}

func ValidateStoredDeploymentPolicyVersion(
	ctx context.Context,
	id uuid.UUID,
	organizationID uuid.UUID,
) ([]types.ValidationIssue, error) {
	version, err := GetDeploymentPolicyVersion(ctx, id, organizationID)
	if err != nil {
		return nil, err
	}
	return governance.ValidateDeploymentPolicyVersion(*version), nil
}

func PublishDeploymentPolicyVersion(
	ctx context.Context,
	id uuid.UUID,
	organizationID uuid.UUID,
	actorUserAccountID uuid.UUID,
) (*types.DeploymentPolicyVersion, []types.ValidationIssue, error) {
	var published *types.DeploymentPolicyVersion
	var issues []types.ValidationIssue
	err := RunTx(ctx, func(ctx context.Context) error {
		version, err := getDeploymentPolicyVersion(
			ctx,
			id,
			organizationID,
			true,
		)
		if err != nil {
			return err
		}
		if version.State != types.DeploymentPolicyVersionStateDraft {
			return apierrors.NewConflict(
				"published deployment policy versions are immutable",
			)
		}
		issues = governance.ValidateDeploymentPolicyVersion(*version)
		if len(issues) != 0 {
			return apierrors.NewBadRequest("deployment policy version is invalid")
		}
		if err := ensureDeploymentPolicyActor(
			ctx,
			organizationID,
			actorUserAccountID,
		); err != nil {
			return err
		}
		if err := validateDeploymentPolicyPrincipalGroupsIfAvailable(
			ctx,
			organizationID,
			version.Document,
		); err != nil {
			return err
		}
		rows, err := internalctx.GetDb(ctx).Query(ctx, `
			UPDATE DeploymentPolicyVersion AS version
			SET
				state = 'PUBLISHED',
				published_by_useraccount_id = @actorUserAccountID,
				published_at = now(),
				updated_at = now()
			WHERE version.id = @id
			  AND version.organization_id = @organizationID
			  AND version.state = 'DRAFT'
			RETURNING `+deploymentPolicyVersionOutputExpr,
			pgx.NamedArgs{
				"id":                 id,
				"organizationID":     organizationID,
				"actorUserAccountID": actorUserAccountID,
			},
		)
		if err != nil {
			return mapDeploymentPolicyWriteError(
				"publish deployment policy version",
				err,
			)
		}
		result, err := collectDeploymentPolicyVersion(rows)
		if errors.Is(err, pgx.ErrNoRows) {
			return apierrors.NewConflict(
				"published deployment policy versions are immutable",
			)
		}
		if err != nil {
			return mapDeploymentPolicyWriteError(
				"read published deployment policy version",
				err,
			)
		}
		published = &result
		return nil
	})
	return published, issues, err
}

func BindDeploymentPolicy(
	ctx context.Context,
	request types.PolicyBindingRequest,
) error {
	if err := validatePolicyBindingRequest(request); err != nil {
		return err
	}
	return RunTx(ctx, func(ctx context.Context) error {
		version, err := getDeploymentPolicyVersion(
			ctx,
			request.PolicyVersionID,
			request.OrganizationID,
			false,
		)
		if err != nil {
			return err
		}
		if version.State != types.DeploymentPolicyVersionStatePublished {
			return apierrors.NewConflict(
				"deployment policy bindings require a published immutable version",
			)
		}
		if err := ensureDeploymentPolicyActor(
			ctx,
			request.OrganizationID,
			request.CreatedByUserAccountID,
		); err != nil {
			return err
		}
		if err := ensureDeploymentPolicyBindingScope(
			ctx,
			request.OrganizationID,
			request.ScopeKind,
			request.ScopeID,
		); err != nil {
			return err
		}
		_, err = internalctx.GetDb(ctx).Exec(ctx, `
			INSERT INTO DeploymentPolicyBinding (
				organization_id,
				deployment_policy_version_id,
				scope_kind,
				scope_id,
				binding_role,
				created_by_useraccount_id
			) VALUES (
				@organizationID,
				@policyVersionID,
				@scopeKind,
				@scopeID,
				@role,
				@createdByUserAccountID
			)
		`,
			pgx.NamedArgs{
				"organizationID":         request.OrganizationID,
				"policyVersionID":        request.PolicyVersionID,
				"scopeKind":              request.ScopeKind,
				"scopeID":                request.ScopeID,
				"role":                   request.Role,
				"createdByUserAccountID": request.CreatedByUserAccountID,
			},
		)
		if err != nil {
			return mapDeploymentPolicyWriteError(
				"bind deployment policy version",
				err,
			)
		}
		return nil
	})
}

func ListDeploymentPolicyBindings(
	ctx context.Context,
	filter types.DeploymentPolicyListFilter,
) (types.Page[types.DeploymentPolicyBinding], error) {
	return listDeploymentPolicyEntities(
		ctx,
		filter,
		deploymentPolicyCursorResourceBindings,
		uuid.Nil,
		"DeploymentPolicyBinding",
		"binding",
		deploymentPolicyBindingOutputExpr,
		"",
		nil,
		func(binding types.DeploymentPolicyBinding) (time.Time, uuid.UUID) {
			return binding.CreatedAt, binding.ID
		},
	)
}

func RetireDeploymentPolicyBinding(
	ctx context.Context,
	id uuid.UUID,
	organizationID uuid.UUID,
) error {
	command, err := internalctx.GetDb(ctx).Exec(ctx, `
		UPDATE DeploymentPolicyBinding
		SET retired_at = now()
		WHERE id = @id
		  AND organization_id = @organizationID
		  AND retired_at IS NULL
	`,
		pgx.NamedArgs{"id": id, "organizationID": organizationID},
	)
	if err != nil {
		return mapDeploymentPolicyWriteError(
			"retire deployment policy binding",
			err,
		)
	}
	if command.RowsAffected() == 0 {
		return apierrors.ErrNotFound
	}
	return nil
}

func ResolveEffectivePolicyForDeploymentUnit(
	ctx context.Context,
	organizationID uuid.UUID,
	deploymentUnitID uuid.UUID,
	environmentID uuid.UUID,
) (types.EffectivePolicy, []types.ValidationIssue, error) {
	type unitPolicyIdentity struct {
		SubscriberSetChecksum  string
		DeliveryModel          types.DeliveryModel
		CustomerOrganizationID *uuid.UUID
		EnvironmentID          uuid.UUID
	}
	var identity unitPolicyIdentity
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT
		  unit.subscriber_set_checksum,
		  scope.delivery_model,
		  scope.customer_organization_id,
		  assignment.environment_id
		FROM DeploymentUnit unit
		JOIN DeploymentScope scope
		  ON scope.id = unit.deployment_scope_id
		 AND scope.organization_id = unit.organization_id
		JOIN TargetEnvironmentAssignment assignment
		  ON assignment.id = unit.target_environment_assignment_id
		 AND assignment.organization_id = unit.organization_id
		WHERE unit.id = @deploymentUnitID
		  AND unit.organization_id = @organizationID
		  AND unit.retired_at IS NULL
	`,
		pgx.NamedArgs{
			"deploymentUnitID": deploymentUnitID,
			"organizationID":   organizationID,
		},
	).Scan(
		&identity.SubscriberSetChecksum,
		&identity.DeliveryModel,
		&identity.CustomerOrganizationID,
		&identity.EnvironmentID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return types.EffectivePolicy{}, nil, apierrors.ErrNotFound
	}
	if err != nil {
		return types.EffectivePolicy{}, nil, fmt.Errorf(
			"resolve deployment unit policy identity: %w",
			err,
		)
	}
	if identity.EnvironmentID != environmentID {
		return types.EffectivePolicy{}, nil, apierrors.ErrNotFound
	}

	subscriberIDs := make([]uuid.UUID, 0)
	if identity.DeliveryModel == types.DeliveryModelShared {
		rows, queryErr := internalctx.GetDb(ctx).Query(ctx, `
			SELECT customer_organization_id
			FROM DeploymentUnitSubscriber
			WHERE organization_id = @organizationID
			  AND deployment_unit_id = @deploymentUnitID
			  AND retired_at IS NULL
			ORDER BY customer_organization_id
		`,
			pgx.NamedArgs{
				"organizationID":   organizationID,
				"deploymentUnitID": deploymentUnitID,
			},
		)
		if queryErr != nil {
			return types.EffectivePolicy{}, nil, fmt.Errorf(
				"resolve deployment unit subscribers: %w",
				queryErr,
			)
		}
		subscriberIDs, queryErr = pgx.CollectRows(
			rows,
			pgx.RowTo[uuid.UUID],
		)
		if queryErr != nil {
			return types.EffectivePolicy{}, nil, fmt.Errorf(
				"collect deployment unit subscribers: %w",
				queryErr,
			)
		}
	}

	ownerID := organizationID
	var ownerCustomerID uuid.UUID
	// PR-063 seam: the registry is the authoritative fallback on this speculative
	// branch. The stacked ownership resource can replace this single authority
	// selection without changing composition or stored policy evidence.
	if identity.DeliveryModel == types.DeliveryModelDedicated &&
		identity.CustomerOrganizationID != nil {
		ownerID = *identity.CustomerOrganizationID
		ownerCustomerID = ownerID
	}
	ownerVersions, subscriberVersions, err := resolveBoundDeploymentPolicyVersions(
		ctx,
		organizationID,
		environmentID,
		deploymentUnitID,
		ownerCustomerID,
		subscriberIDs,
	)
	if err != nil {
		return types.EffectivePolicy{}, nil, err
	}
	subscriberSets := make([]types.PolicySet, 0, len(subscriberIDs))
	for _, subscriberID := range subscriberIDs {
		subscriberSets = append(subscriberSets, types.PolicySet{
			AuthorityKind: types.PolicyAuthoritySubscriber,
			AuthorityID:   subscriberID,
			Versions:      subscriberVersions[subscriberID],
		})
	}
	effective, issues := governance.ComposeEffectivePolicy(
		types.PolicySet{
			AuthorityKind:         types.PolicyAuthorityOwner,
			AuthorityID:           ownerID,
			SubscriberSetChecksum: identity.SubscriberSetChecksum,
			Versions:              ownerVersions,
		},
		subscriberSets,
	)
	return effective, issues, nil
}

func resolveBoundDeploymentPolicyVersions(
	ctx context.Context,
	organizationID uuid.UUID,
	environmentID uuid.UUID,
	deploymentUnitID uuid.UUID,
	ownerCustomerID uuid.UUID,
	subscriberIDs []uuid.UUID,
) (
	[]types.DeploymentPolicyVersion,
	map[uuid.UUID][]types.DeploymentPolicyVersion,
	error,
) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT
		  `+deploymentPolicyVersionOutputExpr+`,
		  binding.scope_kind,
		  binding.scope_id,
		  binding.binding_role
		FROM DeploymentPolicyBinding binding
		JOIN DeploymentPolicyVersion version
		  ON version.id = binding.deployment_policy_version_id
		 AND version.organization_id = binding.organization_id
		WHERE binding.organization_id = @organizationID
		  AND binding.retired_at IS NULL
		  AND version.state = 'PUBLISHED'
		  AND (
		    (
		      binding.binding_role = 'owner'
		      AND (
		        (
		          binding.scope_kind = 'organization'
		          AND binding.scope_id = @organizationID
		        )
		        OR (
		          binding.scope_kind = 'environment'
		          AND binding.scope_id = @environmentID
		        )
		        OR (
		          binding.scope_kind = 'deployment_unit'
		          AND binding.scope_id = @deploymentUnitID
		        )
		        OR (
		          binding.scope_kind = 'component'
		          AND binding.scope_id IN (
		            SELECT instance.component_definition_id
		            FROM ComponentInstance instance
		            WHERE instance.organization_id = @organizationID
		              AND instance.deployment_unit_id = @deploymentUnitID
		              AND instance.retired_at IS NULL
		          )
		        )
		        OR (
		          @ownerCustomerID <> '00000000-0000-0000-0000-000000000000'::uuid
		          AND binding.scope_kind = 'customer'
		          AND binding.scope_id = @ownerCustomerID
		        )
		      )
		    )
		    OR (
		      binding.binding_role = 'subscriber'
		      AND binding.scope_kind = 'customer'
		      AND binding.scope_id = ANY(@subscriberIDs)
		    )
		  )
		ORDER BY version.id, binding.scope_id
	`,
		pgx.NamedArgs{
			"organizationID":   organizationID,
			"environmentID":    environmentID,
			"deploymentUnitID": deploymentUnitID,
			"ownerCustomerID":  ownerCustomerID,
			"subscriberIDs":    subscriberIDs,
		},
	)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve bound deployment policies: %w", err)
	}
	defer rows.Close()

	ownerByID := map[uuid.UUID]types.DeploymentPolicyVersion{}
	subscriberByID := make(map[uuid.UUID]map[uuid.UUID]types.DeploymentPolicyVersion)
	for rows.Next() {
		version, scopeID, role, scanErr := scanDeploymentPolicyVersionBindingRow(rows)
		if scanErr != nil {
			return nil, nil, fmt.Errorf(
				"scan bound deployment policy version: %w",
				scanErr,
			)
		}
		if role == types.DeploymentPolicyBindingRoleOwner {
			ownerByID[version.ID] = version
			continue
		}
		if subscriberByID[scopeID] == nil {
			subscriberByID[scopeID] = map[uuid.UUID]types.DeploymentPolicyVersion{}
		}
		subscriberByID[scopeID][version.ID] = version
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate bound deployment policies: %w", err)
	}

	owner := policyVersionMapValues(ownerByID)
	subscribers := make(map[uuid.UUID][]types.DeploymentPolicyVersion, len(subscriberIDs))
	for _, subscriberID := range subscriberIDs {
		subscribers[subscriberID] = policyVersionMapValues(
			subscriberByID[subscriberID],
		)
	}
	return owner, subscribers, nil
}

func scanDeploymentPolicyVersionBindingRow(
	row interface{ Scan(...any) error },
) (
	types.DeploymentPolicyVersion,
	uuid.UUID,
	types.DeploymentPolicyBindingRole,
	error,
) {
	var version types.DeploymentPolicyVersion
	var documentJSON, canonicalPayload json.RawMessage
	var scopeKind types.DeploymentPolicyBindingScopeKind
	var scopeID uuid.UUID
	var role types.DeploymentPolicyBindingRole
	err := row.Scan(
		&version.ID,
		&version.CreatedAt,
		&version.UpdatedAt,
		&version.OrganizationID,
		&version.PolicyID,
		&version.VersionNumber,
		&version.State,
		&documentJSON,
		&version.CanonicalChecksum,
		&canonicalPayload,
		&version.CreatedByUserAccountID,
		&version.PublishedByUserAccountID,
		&version.PublishedAt,
		&scopeKind,
		&scopeID,
		&role,
	)
	if err != nil {
		return version, uuid.Nil, "", err
	}
	if err := json.Unmarshal(documentJSON, &version.Document); err != nil {
		return version, uuid.Nil, "", fmt.Errorf(
			"decode deployment policy document: %w",
			err,
		)
	}
	version.CanonicalPayload = canonicalPayload
	return version, scopeID, role, nil
}

func policyVersionMapValues(
	values map[uuid.UUID]types.DeploymentPolicyVersion,
) []types.DeploymentPolicyVersion {
	result := make([]types.DeploymentPolicyVersion, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	return result
}

func collectDeploymentPolicyVersion(
	rows pgx.Rows,
) (types.DeploymentPolicyVersion, error) {
	versions, err := collectDeploymentPolicyVersions(rows)
	if err != nil {
		return types.DeploymentPolicyVersion{}, err
	}
	if len(versions) == 0 {
		return types.DeploymentPolicyVersion{}, pgx.ErrNoRows
	}
	if len(versions) != 1 {
		return types.DeploymentPolicyVersion{}, fmt.Errorf(
			"expected one deployment policy version, got %d",
			len(versions),
		)
	}
	return versions[0], nil
}

func collectDeploymentPolicyVersions(
	rows pgx.Rows,
) ([]types.DeploymentPolicyVersion, error) {
	defer rows.Close()
	result := make([]types.DeploymentPolicyVersion, 0)
	for rows.Next() {
		var version types.DeploymentPolicyVersion
		var documentJSON, canonicalPayload json.RawMessage
		if err := rows.Scan(
			&version.ID,
			&version.CreatedAt,
			&version.UpdatedAt,
			&version.OrganizationID,
			&version.PolicyID,
			&version.VersionNumber,
			&version.State,
			&documentJSON,
			&version.CanonicalChecksum,
			&canonicalPayload,
			&version.CreatedByUserAccountID,
			&version.PublishedByUserAccountID,
			&version.PublishedAt,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(documentJSON, &version.Document); err != nil {
			return nil, fmt.Errorf("decode deployment policy document: %w", err)
		}
		version.CanonicalPayload = canonicalPayload
		result = append(result, version)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func listDeploymentPolicyEntities[T any](
	ctx context.Context,
	filter types.DeploymentPolicyListFilter,
	resource string,
	parentID uuid.UUID,
	table string,
	alias string,
	outputExpression string,
	extraWhere string,
	extraArgs pgx.NamedArgs,
	key func(T) (time.Time, uuid.UUID),
) (types.Page[T], error) {
	page := types.Page[T]{Items: []T{}}
	limit, cursor, err := normalizeDeploymentPolicyListFilter(
		filter,
		resource,
		parentID,
	)
	if err != nil {
		return page, err
	}
	cursorCreatedAt, cursorID := deploymentPolicyCursorValues(cursor)
	args := pgx.NamedArgs{
		"organizationID":  filter.OrganizationID,
		"cursorCreatedAt": cursorCreatedAt,
		"cursorID":        cursorID,
		"fetchLimit":      limit + 1,
	}
	for name, value := range extraArgs {
		args[name] = value
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx,
		"SELECT "+outputExpression+
			" FROM "+table+" "+alias+
			" WHERE "+alias+".organization_id = @organizationID"+
			extraWhere+
			" AND ("+
			" @cursorCreatedAt::timestamptz IS NULL"+
			" OR ("+alias+".created_at, "+alias+".id) <"+
			" (@cursorCreatedAt::timestamptz, @cursorID::uuid)"+
			" )"+
			" ORDER BY "+alias+".created_at DESC, "+alias+".id DESC"+
			" LIMIT @fetchLimit",
		args,
	)
	if err != nil {
		return page, fmt.Errorf("list deployment policy %s: %w", resource, err)
	}
	items, err := pgx.CollectRows(rows, pgx.RowToStructByName[T])
	if err != nil {
		return page, fmt.Errorf("collect deployment policy %s: %w", resource, err)
	}
	return finishDeploymentPolicyPage(items, limit, resource, parentID, key)
}

func finishDeploymentPolicyPage[T any](
	items []T,
	limit int,
	resource string,
	parentID uuid.UUID,
	key func(T) (time.Time, uuid.UUID),
) (types.Page[T], error) {
	page := types.Page[T]{Items: items}
	hasMore := len(items) > limit
	if hasMore {
		page.Items = items[:limit]
	}
	if !hasMore || len(page.Items) == 0 {
		return page, nil
	}
	createdAt, id := key(page.Items[len(page.Items)-1])
	cursor, err := encodeDeploymentPolicyCursor(deploymentPolicyCursor{
		Version:   deploymentPolicyCursorVersion,
		Resource:  resource,
		ParentID:  parentID,
		CreatedAt: createdAt,
		ID:        id,
	})
	if err != nil {
		return page, err
	}
	page.NextCursor = cursor
	return page, nil
}

func normalizeDeploymentPolicyListFilter(
	filter types.DeploymentPolicyListFilter,
	resource string,
	parentID uuid.UUID,
) (int, *deploymentPolicyCursor, error) {
	if filter.OrganizationID == uuid.Nil {
		return 0, nil, apierrors.NewBadRequest("organizationId is required")
	}
	limit := filter.Limit
	if limit == 0 {
		limit = deploymentPolicyDefaultPageLimit
	}
	if limit < 1 || limit > deploymentPolicyMaximumPageLimit {
		return 0, nil, apierrors.NewBadRequest("limit must be between 1 and 100")
	}
	cursor, err := decodeDeploymentPolicyCursor(filter.Cursor, resource, parentID)
	if err != nil {
		return 0, nil, err
	}
	return limit, cursor, nil
}

func deploymentPolicyCursorValues(cursor *deploymentPolicyCursor) (any, any) {
	if cursor == nil {
		return nil, nil
	}
	return cursor.CreatedAt, cursor.ID
}

func encodeDeploymentPolicyCursor(cursor deploymentPolicyCursor) (string, error) {
	if cursor.Version != deploymentPolicyCursorVersion ||
		cursor.Resource == "" ||
		cursor.CreatedAt.IsZero() ||
		cursor.ID == uuid.Nil {
		return "", fmt.Errorf("encode deployment policy cursor: invalid cursor")
	}
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("encode deployment policy cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeDeploymentPolicyCursor(
	value string,
	resource string,
	parentID uuid.UUID,
) (*deploymentPolicyCursor, error) {
	if value == "" {
		return nil, nil
	}
	if len(value) > deploymentPolicyMaximumCursorSize {
		return nil, apierrors.NewBadRequest("cursor is invalid")
	}
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, apierrors.NewBadRequest("cursor is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var cursor deploymentPolicyCursor
	if err := decoder.Decode(&cursor); err != nil {
		return nil, apierrors.NewBadRequest("cursor is invalid")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, apierrors.NewBadRequest("cursor is invalid")
	}
	if cursor.Version != deploymentPolicyCursorVersion ||
		cursor.Resource != resource ||
		cursor.ParentID != parentID ||
		cursor.CreatedAt.IsZero() ||
		cursor.ID == uuid.Nil {
		return nil, apierrors.NewBadRequest("cursor is invalid")
	}
	return &cursor, nil
}

func setDeploymentPolicyVersionCanonicalFields(
	version *types.DeploymentPolicyVersion,
) error {
	issues := governance.ValidateDeploymentPolicyVersion(*version)
	if len(issues) != 0 {
		return apierrors.NewBadRequest(issues[0].Message)
	}
	normalized, payload, checksum, err := governance.CanonicalizeDeploymentPolicyDocument(
		version.Document,
	)
	if err != nil {
		return err
	}
	version.Document = normalized
	version.CanonicalPayload = payload
	version.CanonicalChecksum = checksum
	return nil
}

func validateDeploymentPolicyForWrite(policy types.DeploymentPolicy) error {
	if policy.OrganizationID == uuid.Nil {
		return apierrors.NewBadRequest("organizationId is required")
	}
	if policy.ID != uuid.Nil && strings.TrimSpace(policy.Key) == "" {
		return apierrors.NewBadRequest("key is required")
	}
	key := strings.TrimSpace(policy.Key)
	if key == "" ||
		len(key) > 128 ||
		!deploymentPolicyResourceKeyPattern.MatchString(key) {
		return apierrors.NewBadRequest("key is invalid")
	}
	name := strings.TrimSpace(policy.Name)
	if name == "" || len(name) > 256 {
		return apierrors.NewBadRequest("name is invalid")
	}
	if len(policy.Description) > 4096 {
		return apierrors.NewBadRequest("description is too long")
	}
	return nil
}

func validatePolicyBindingRequest(request types.PolicyBindingRequest) error {
	if request.OrganizationID == uuid.Nil {
		return apierrors.NewBadRequest("organizationId is required")
	}
	if request.PolicyVersionID == uuid.Nil {
		return apierrors.NewBadRequest("policyVersionId is required")
	}
	if !request.ScopeKind.IsValid() {
		return apierrors.NewBadRequest("scopeKind is invalid")
	}
	if request.ScopeID == uuid.Nil {
		return apierrors.NewBadRequest("scopeId is required")
	}
	if !request.Role.IsValid() {
		return apierrors.NewBadRequest("role is invalid")
	}
	if request.Role == types.DeploymentPolicyBindingRoleSubscriber &&
		request.ScopeKind != types.DeploymentPolicyBindingScopeCustomer {
		return apierrors.NewBadRequest(
			"subscriber bindings require customer scope",
		)
	}
	if request.ScopeKind == types.DeploymentPolicyBindingScopeCampaign {
		return apierrors.NewBadRequest(
			"campaign bindings are unavailable until campaign resources are present",
		)
	}
	if request.CreatedByUserAccountID == uuid.Nil {
		return apierrors.NewBadRequest("createdByUserAccountId is required")
	}
	return nil
}

func ensureDeploymentPolicyActor(
	ctx context.Context,
	organizationID uuid.UUID,
	userAccountID uuid.UUID,
) error {
	var exists bool
	if err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1
		  FROM Organization_UserAccount membership
		  WHERE membership.organization_id = @organizationID
		    AND membership.user_account_id = @userAccountID
		)
	`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"userAccountID":  userAccountID,
		},
	).Scan(&exists); err != nil {
		return fmt.Errorf("validate deployment policy actor: %w", err)
	}
	if !exists {
		return apierrors.ErrNotFound
	}
	return nil
}

func ensureDeploymentPolicyBindingScope(
	ctx context.Context,
	organizationID uuid.UUID,
	scopeKind types.DeploymentPolicyBindingScopeKind,
	scopeID uuid.UUID,
) error {
	var query string
	switch scopeKind {
	case types.DeploymentPolicyBindingScopeOrganization:
		if scopeID != organizationID {
			return apierrors.ErrNotFound
		}
		query = `SELECT EXISTS (
		  SELECT 1 FROM Organization
		  WHERE id = @scopeID
		)`
	case types.DeploymentPolicyBindingScopeCustomer:
		query = `SELECT EXISTS (
		  SELECT 1 FROM CustomerOrganization
		  WHERE id = @scopeID AND organization_id = @organizationID
		)`
	case types.DeploymentPolicyBindingScopeEnvironment:
		query = `SELECT EXISTS (
		  SELECT 1 FROM Environment
		  WHERE id = @scopeID AND organization_id = @organizationID
		)`
	case types.DeploymentPolicyBindingScopeDeploymentUnit:
		query = `SELECT EXISTS (
		  SELECT 1 FROM DeploymentUnit
		  WHERE id = @scopeID AND organization_id = @organizationID
		)`
	case types.DeploymentPolicyBindingScopeComponent:
		query = `SELECT EXISTS (
		  SELECT 1 FROM ComponentDefinition
		  WHERE id = @scopeID AND organization_id = @organizationID
		)`
	default:
		return apierrors.NewBadRequest("scopeKind is unavailable")
	}
	var exists bool
	if err := internalctx.GetDb(ctx).QueryRow(ctx, query, pgx.NamedArgs{
		"scopeID":        scopeID,
		"organizationID": organizationID,
	}).Scan(&exists); err != nil {
		return fmt.Errorf("validate deployment policy binding scope: %w", err)
	}
	if !exists {
		return apierrors.ErrNotFound
	}
	return nil
}

func validateDeploymentPolicyPrincipalGroupsIfAvailable(
	ctx context.Context,
	organizationID uuid.UUID,
	document types.DeploymentPolicyDocument,
) error {
	// PR-066 seam: validate policy authorities against scoped principal groups
	// when that prerequisite relation is present on the stacked branch.
	var relationAvailable bool
	if err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT to_regclass(
		  format('%I.%I', current_schema(), 'principalgroup')
		) IS NOT NULL
	`).Scan(&relationAvailable); err != nil {
		return fmt.Errorf("detect scoped authorization principal groups: %w", err)
	}
	if !relationAvailable {
		return nil
	}
	groupIDs := make([]uuid.UUID, 0, len(document.ApprovalRules)+1)
	for _, rule := range document.ApprovalRules {
		groupIDs = append(groupIDs, rule.PrincipalGroupID)
	}
	if document.OverrideRules.AuthorityGroupID != nil {
		groupIDs = append(groupIDs, *document.OverrideRules.AuthorityGroupID)
	}
	var count int
	if err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT count(DISTINCT id)
		FROM PrincipalGroup
		WHERE organization_id = @organizationID
		  AND id = ANY(@groupIDs)
	`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"groupIDs":       groupIDs,
		},
	).Scan(&count); err != nil {
		return fmt.Errorf("validate scoped authorization principal groups: %w", err)
	}
	expected := make(map[uuid.UUID]struct{}, len(groupIDs))
	for _, groupID := range groupIDs {
		expected[groupID] = struct{}{}
	}
	if count != len(expected) {
		return apierrors.ErrNotFound
	}
	return nil
}

func mapDeploymentPolicyWriteError(action string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case pgerrcode.UniqueViolation:
			return fmt.Errorf("%s: %w", action, apierrors.ErrAlreadyExists)
		case pgerrcode.ForeignKeyViolation:
			return fmt.Errorf("%s: %w", action, apierrors.ErrNotFound)
		case pgerrcode.CheckViolation:
			return fmt.Errorf("%s: %w", action, apierrors.ErrBadRequest)
		}
	}
	return fmt.Errorf("%s: %w", action, err)
}
