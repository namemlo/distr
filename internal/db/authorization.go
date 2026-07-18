package db

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var authorizationKeyPattern = regexp.MustCompile(`^[a-z0-9]+([._-][a-z0-9]+)*$`)

func ListAuthorizationAccessGrants(
	ctx context.Context,
	organizationID uuid.UUID,
	principalID uuid.UUID,
) ([]types.AccessGrant, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT
		  binding.id,
		  binding.principal_kind,
		  binding.scope_kind,
		  binding.scope_id,
		  binding.effective_from,
		  binding.effective_until,
		  array_agg(permission.action ORDER BY permission.action)
		FROM RoleBinding binding
		JOIN RolePermission permission
		  ON permission.role_definition_id = binding.role_definition_id
		 AND permission.organization_id = binding.organization_id
		WHERE binding.organization_id = @organizationID
		  AND (
		    (
		      binding.principal_kind = 'user'
		      AND binding.principal_id = @principalID
		    )
		    OR (
		      binding.principal_kind = 'group'
		      AND EXISTS (
		        SELECT 1
		        FROM PrincipalGroupMember membership
		        WHERE membership.organization_id = binding.organization_id
		          AND membership.group_id = binding.principal_id
		          AND membership.user_account_id = @principalID
		          AND membership.effective_from <= now()
		          AND (
		            membership.effective_until IS NULL
		            OR membership.effective_until > now()
		          )
		      )
		    )
		  )
		GROUP BY
		  binding.id,
		  binding.principal_kind,
		  binding.scope_kind,
		  binding.scope_id,
		  binding.effective_from,
		  binding.effective_until
		ORDER BY binding.id`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"principalID":    principalID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query scoped authorization grants: %w", err)
	}
	defer rows.Close()

	grants := make([]types.AccessGrant, 0)
	for rows.Next() {
		var grant types.AccessGrant
		var scopeKind string
		var actionValues []string
		if err := rows.Scan(
			&grant.BindingID,
			&grant.PrincipalKind,
			&scopeKind,
			&grant.Scope.ID,
			&grant.EffectiveFrom,
			&grant.EffectiveUntil,
			&actionValues,
		); err != nil {
			return nil, fmt.Errorf("could not scan scoped authorization grant: %w", err)
		}
		grant.Scope.Kind = types.PermissionScope(scopeKind)
		grant.Actions = make([]types.Action, 0, len(actionValues))
		for _, value := range actionValues {
			action := types.Action(value)
			if action.Valid() {
				grant.Actions = append(grant.Actions, action)
			}
		}
		grants = append(grants, grant)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("could not collect scoped authorization grants: %w", err)
	}
	return grants, nil
}

func GetAuthorizationLegacyUserRole(
	ctx context.Context,
	organizationID uuid.UUID,
	principalID uuid.UUID,
) (*types.UserRole, error) {
	var role types.UserRole
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT user_role
		FROM Organization_UserAccount
		WHERE organization_id = @organizationID
		  AND user_account_id = @principalID`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"principalID":    principalID,
		},
	).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("could not query legacy authorization role: %w", err)
	}
	return &role, nil
}

func ResolveAuthorizationResourceScopes(
	ctx context.Context,
	ref types.ResourceRef,
) ([]types.ScopeRef, error) {
	if ref.OrganizationID == uuid.Nil ||
		ref.ID == uuid.Nil ||
		!ref.Kind.Supported() {
		return nil, apierrors.ErrNotFound
	}
	organizationScope := types.ScopeRef{
		Kind: types.PermissionScopeOrganization,
		ID:   ref.OrganizationID,
	}
	switch ref.Kind {
	case types.PermissionScopeOrganization:
		if ref.ID != ref.OrganizationID {
			return nil, apierrors.ErrNotFound
		}
		return []types.ScopeRef{organizationScope}, nil
	case types.PermissionScopeCustomer:
		if err := authorizationResourceExists(
			ctx,
			"CustomerOrganization",
			ref.OrganizationID,
			ref.ID,
		); err != nil {
			return nil, err
		}
		return []types.ScopeRef{
			organizationScope,
			{Kind: types.PermissionScopeCustomer, ID: ref.ID},
		}, nil
	case types.PermissionScopeEnvironment:
		if err := authorizationResourceExists(
			ctx,
			"Environment",
			ref.OrganizationID,
			ref.ID,
		); err != nil {
			return nil, err
		}
		return []types.ScopeRef{
			organizationScope,
			{Kind: types.PermissionScopeEnvironment, ID: ref.ID},
		}, nil
	case types.PermissionScopeDeploymentUnit:
		return resolveDeploymentUnitAuthorizationScopes(ctx, ref)
	case types.PermissionScopeComponent:
		if err := authorizationResourceExists(
			ctx,
			"ComponentDefinition",
			ref.OrganizationID,
			ref.ID,
		); err != nil {
			return nil, err
		}
		return []types.ScopeRef{
			organizationScope,
			{Kind: types.PermissionScopeComponent, ID: ref.ID},
		}, nil
	case types.PermissionScopeCampaign:
		// Campaign storage is introduced by PR-071. Until then, the authenticated
		// organization boundary and the campaign UUID form the authorization key;
		// the campaign repository still performs the tenant-scoped existence check.
		return []types.ScopeRef{
			organizationScope,
			{Kind: types.PermissionScopeCampaign, ID: ref.ID},
		}, nil
	default:
		return nil, apierrors.ErrNotFound
	}
}

func resolveDeploymentUnitAuthorizationScopes(
	ctx context.Context,
	ref types.ResourceRef,
) ([]types.ScopeRef, error) {
	var environmentID uuid.UUID
	var dedicatedCustomerID *uuid.UUID
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT
		  assignment.environment_id,
		  scope.customer_organization_id
		FROM DeploymentUnit unit
		JOIN TargetEnvironmentAssignment assignment
		  ON assignment.id = unit.target_environment_assignment_id
		 AND assignment.organization_id = unit.organization_id
		JOIN DeploymentScope scope
		  ON scope.id = unit.deployment_scope_id
		 AND scope.organization_id = unit.organization_id
		WHERE unit.organization_id = @organizationID
		  AND unit.id = @unitID`,
		pgx.NamedArgs{
			"organizationID": ref.OrganizationID,
			"unitID":         ref.ID,
		},
	).Scan(&environmentID, &dedicatedCustomerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("could not resolve deployment unit authorization scopes: %w", err)
	}

	scopes := []types.ScopeRef{
		{Kind: types.PermissionScopeOrganization, ID: ref.OrganizationID},
		{Kind: types.PermissionScopeEnvironment, ID: environmentID},
		{Kind: types.PermissionScopeDeploymentUnit, ID: ref.ID},
	}
	if dedicatedCustomerID != nil {
		scopes = append(scopes, types.ScopeRef{
			Kind: types.PermissionScopeCustomer,
			ID:   *dedicatedCustomerID,
		})
	}

	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT customer_organization_id
		FROM DeploymentUnitSubscriber
		WHERE organization_id = @organizationID
		  AND deployment_unit_id = @unitID
		  AND retired_at IS NULL
		ORDER BY customer_organization_id`,
		pgx.NamedArgs{
			"organizationID": ref.OrganizationID,
			"unitID":         ref.ID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query deployment unit authorization subscribers: %w", err)
	}
	customerIDs, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("could not collect deployment unit authorization subscribers: %w", err)
	}
	for _, customerID := range customerIDs {
		scopes = append(scopes, types.ScopeRef{
			Kind: types.PermissionScopeCustomer,
			ID:   customerID,
		})
	}
	return scopes, nil
}

func authorizationResourceExists(
	ctx context.Context,
	table string,
	organizationID uuid.UUID,
	id uuid.UUID,
) error {
	var exists bool
	err := internalctx.GetDb(ctx).QueryRow(ctx,
		`SELECT EXISTS (
		  SELECT 1
		  FROM `+table+`
		  WHERE organization_id = @organizationID
		    AND id = @id
		)`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"id":             id,
		},
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("could not validate authorization scope: %w", err)
	}
	if !exists {
		return apierrors.ErrNotFound
	}
	return nil
}

func ListControlPlaneEnrollmentsForScope(
	ctx context.Context,
	organizationID uuid.UUID,
	scopeKind types.PermissionScope,
	scopeID uuid.UUID,
) ([]types.ControlPlaneEnrollment, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT
		  id,
		  created_at,
		  organization_id,
		  scope_kind,
		  scope_id,
		  enabled,
		  effective_from,
		  effective_until,
		  actor_useraccount_id,
		  reason,
		  revision
		FROM ControlPlaneEnrollment
		WHERE organization_id = @organizationID
		  AND scope_kind = @scopeKind
		  AND scope_id = @scopeID
		ORDER BY revision, id`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"scopeKind":      scopeKind,
			"scopeID":        scopeID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query control-plane enrollments: %w", err)
	}
	return collectControlPlaneEnrollments(rows)
}

func ListControlPlaneEnrollments(
	ctx context.Context,
	organizationID uuid.UUID,
) ([]types.ControlPlaneEnrollment, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT
		  id,
		  created_at,
		  organization_id,
		  scope_kind,
		  scope_id,
		  enabled,
		  effective_from,
		  effective_until,
		  actor_useraccount_id,
		  reason,
		  revision
		FROM ControlPlaneEnrollment
		WHERE organization_id = @organizationID
		ORDER BY scope_kind, scope_id, revision, id`,
		pgx.NamedArgs{"organizationID": organizationID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query control-plane enrollments: %w", err)
	}
	return collectControlPlaneEnrollments(rows)
}

func collectControlPlaneEnrollments(
	rows pgx.Rows,
) ([]types.ControlPlaneEnrollment, error) {
	defer rows.Close()
	enrollments := make([]types.ControlPlaneEnrollment, 0)
	for rows.Next() {
		var enrollment types.ControlPlaneEnrollment
		if err := rows.Scan(
			&enrollment.ID,
			&enrollment.CreatedAt,
			&enrollment.OrganizationID,
			&enrollment.Scope.Kind,
			&enrollment.Scope.ID,
			&enrollment.Enabled,
			&enrollment.EffectiveFrom,
			&enrollment.EffectiveUntil,
			&enrollment.ActorUserID,
			&enrollment.Reason,
			&enrollment.Revision,
		); err != nil {
			return nil, fmt.Errorf("could not scan control-plane enrollment: %w", err)
		}
		enrollments = append(enrollments, enrollment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("could not collect control-plane enrollments: %w", err)
	}
	return enrollments, nil
}

func CreateControlPlaneEnrollment(
	ctx context.Context,
	enrollment *types.ControlPlaneEnrollment,
) error {
	if err := validateControlPlaneEnrollment(*enrollment); err != nil {
		return err
	}
	if enrollment.ID == uuid.Nil {
		enrollment.ID = uuid.New()
	}
	enrollment.Reason = strings.TrimSpace(enrollment.Reason)
	enrollment.EffectiveFrom = enrollment.EffectiveFrom.UTC()
	if enrollment.EffectiveUntil != nil {
		value := enrollment.EffectiveUntil.UTC()
		enrollment.EffectiveUntil = &value
	}

	return withAuthorizationTransaction(ctx, func(txContext context.Context) error {
		if err := ensureAuthorizationScopeExists(
			txContext,
			enrollment.OrganizationID,
			enrollment.Scope,
		); err != nil {
			return err
		}
		if _, err := internalctx.GetDb(txContext).Exec(txContext, `
			SELECT pg_advisory_xact_lock(
			  hashtextextended(
			    @organizationID::text || ':' || @scopeKind || ':' || @scopeID::text,
			    0
			  )
			)`,
			pgx.NamedArgs{
				"organizationID": enrollment.OrganizationID,
				"scopeKind":      enrollment.Scope.Kind,
				"scopeID":        enrollment.Scope.ID,
			},
		); err != nil {
			return fmt.Errorf("could not lock control-plane enrollment revision: %w", err)
		}
		err := internalctx.GetDb(txContext).QueryRow(txContext, `
			INSERT INTO ControlPlaneEnrollment (
			  id,
			  organization_id,
			  scope_kind,
			  scope_id,
			  enabled,
			  effective_from,
			  effective_until,
			  actor_useraccount_id,
			  reason,
			  revision
			)
			SELECT
			  @id,
			  @organizationID,
			  @scopeKind,
			  @scopeID,
			  @enabled,
			  @effectiveFrom,
			  @effectiveUntil,
			  @actorUserID,
			  @reason,
			  COALESCE(max(revision), 0) + 1
			FROM ControlPlaneEnrollment
			WHERE organization_id = @organizationID
			  AND scope_kind = @scopeKind
			  AND scope_id = @scopeID
			RETURNING created_at, revision`,
			pgx.NamedArgs{
				"id":             enrollment.ID,
				"organizationID": enrollment.OrganizationID,
				"scopeKind":      enrollment.Scope.Kind,
				"scopeID":        enrollment.Scope.ID,
				"enabled":        enrollment.Enabled,
				"effectiveFrom":  enrollment.EffectiveFrom,
				"effectiveUntil": enrollment.EffectiveUntil,
				"actorUserID":    enrollment.ActorUserID,
				"reason":         enrollment.Reason,
			},
		).Scan(&enrollment.CreatedAt, &enrollment.Revision)
		if err != nil {
			return mapAuthorizationWriteError("create control-plane enrollment", err)
		}
		return nil
	})
}

func validateControlPlaneEnrollment(enrollment types.ControlPlaneEnrollment) error {
	if enrollment.OrganizationID == uuid.Nil {
		return apierrors.NewBadRequest("organizationId is required")
	}
	if enrollment.Scope.ID == uuid.Nil {
		return apierrors.NewBadRequest("scopeId is required")
	}
	if enrollment.Scope.Kind != types.PermissionScopeOrganization &&
		enrollment.Scope.Kind != types.PermissionScopeEnvironment {
		return apierrors.NewBadRequest("enrollment scope must be organization or environment")
	}
	if enrollment.Scope.Kind == types.PermissionScopeOrganization &&
		enrollment.Scope.ID != enrollment.OrganizationID {
		return apierrors.NewBadRequest("organization enrollment scope does not match organization")
	}
	if enrollment.ActorUserID == uuid.Nil {
		return apierrors.NewBadRequest("actorUserAccountId is required")
	}
	if enrollment.EffectiveFrom.IsZero() {
		return apierrors.NewBadRequest("effectiveFrom is required")
	}
	if enrollment.EffectiveUntil != nil &&
		!enrollment.EffectiveUntil.After(enrollment.EffectiveFrom) {
		return apierrors.NewBadRequest("effectiveUntil must be after effectiveFrom")
	}
	if strings.TrimSpace(enrollment.Reason) == "" {
		return apierrors.NewBadRequest("reason is required")
	}
	return nil
}

func CreateAuthorizationRoleDefinition(
	ctx context.Context,
	role *types.RoleDefinition,
) error {
	if err := normalizeAuthorizationRoleDefinition(role); err != nil {
		return err
	}
	if role.ID == uuid.Nil {
		role.ID = uuid.New()
	}

	return withAuthorizationTransaction(ctx, func(txContext context.Context) error {
		err := internalctx.GetDb(txContext).QueryRow(txContext, `
			INSERT INTO RoleDefinition (
			  id,
			  organization_id,
			  role_key,
			  display_name,
			  description,
			  built_in,
			  source_legacy_role,
			  revision,
			  created_by_useraccount_id
			) VALUES (
			  @id,
			  @organizationID,
			  @key,
			  @displayName,
			  @description,
			  false,
			  NULL,
			  @revision,
			  @createdByUserID
			)
			RETURNING created_at`,
			pgx.NamedArgs{
				"id":              role.ID,
				"organizationID":  role.OrganizationID,
				"key":             role.Key,
				"displayName":     role.DisplayName,
				"description":     role.Description,
				"revision":        role.Revision,
				"createdByUserID": role.CreatedByUserID,
			},
		).Scan(&role.CreatedAt)
		if err != nil {
			return mapAuthorizationWriteError("create role definition", err)
		}

		_, err = internalctx.GetDb(txContext).CopyFrom(
			txContext,
			pgx.Identifier{"rolepermission"},
			[]string{"organization_id", "role_definition_id", "action"},
			pgx.CopyFromSlice(len(role.Permissions), func(index int) ([]any, error) {
				return []any{
					role.OrganizationID,
					role.ID,
					string(role.Permissions[index]),
				}, nil
			}),
		)
		if err != nil {
			return mapAuthorizationWriteError("create role permissions", err)
		}
		return nil
	})
}

func normalizeAuthorizationRoleDefinition(role *types.RoleDefinition) error {
	if role.OrganizationID == uuid.Nil {
		return apierrors.NewBadRequest("organizationId is required")
	}
	role.Key = strings.ToLower(strings.TrimSpace(role.Key))
	role.DisplayName = strings.TrimSpace(role.DisplayName)
	role.Description = strings.TrimSpace(role.Description)
	if !authorizationKeyPattern.MatchString(role.Key) {
		return apierrors.NewBadRequest("role key is invalid")
	}
	if role.DisplayName == "" {
		return apierrors.NewBadRequest("displayName is required")
	}
	if len(role.Permissions) == 0 {
		return apierrors.NewBadRequest("at least one permission is required")
	}
	permissions := make([]types.Action, 0, len(role.Permissions))
	seen := make(map[types.Action]struct{}, len(role.Permissions))
	for _, action := range role.Permissions {
		if !action.Valid() {
			return apierrors.NewBadRequest("unsupported action")
		}
		if _, exists := seen[action]; exists {
			continue
		}
		seen[action] = struct{}{}
		permissions = append(permissions, action)
	}
	sort.Slice(permissions, func(i, j int) bool {
		return permissions[i] < permissions[j]
	})
	role.Permissions = permissions
	role.BuiltIn = false
	role.SourceLegacyRole = nil
	if role.Revision == 0 {
		role.Revision = 1
	}
	return nil
}

func ListAuthorizationRoleDefinitions(
	ctx context.Context,
	organizationID uuid.UUID,
) ([]types.RoleDefinition, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT
		  definition.id,
		  definition.created_at,
		  definition.organization_id,
		  definition.role_key,
		  definition.display_name,
		  definition.description,
		  definition.built_in,
		  definition.source_legacy_role,
		  definition.revision,
		  definition.created_by_useraccount_id,
		  array_agg(permission.action ORDER BY permission.action)
		FROM RoleDefinition definition
		JOIN RolePermission permission
		  ON permission.role_definition_id = definition.id
		 AND permission.organization_id = definition.organization_id
		WHERE definition.organization_id = @organizationID
		GROUP BY definition.id
		ORDER BY definition.built_in DESC, definition.role_key, definition.id`,
		pgx.NamedArgs{"organizationID": organizationID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query authorization role definitions: %w", err)
	}
	defer rows.Close()

	roles := make([]types.RoleDefinition, 0)
	for rows.Next() {
		var role types.RoleDefinition
		var actionValues []string
		var sourceLegacyRole *string
		if err := rows.Scan(
			&role.ID,
			&role.CreatedAt,
			&role.OrganizationID,
			&role.Key,
			&role.DisplayName,
			&role.Description,
			&role.BuiltIn,
			&sourceLegacyRole,
			&role.Revision,
			&role.CreatedByUserID,
			&actionValues,
		); err != nil {
			return nil, fmt.Errorf("could not scan authorization role definition: %w", err)
		}
		if sourceLegacyRole != nil {
			parsedRole, err := types.ParseUserRole(*sourceLegacyRole)
			if err != nil {
				return nil, fmt.Errorf("could not parse built-in authorization role: %w", err)
			}
			role.SourceLegacyRole = &parsedRole
		}
		for _, value := range actionValues {
			role.Permissions = append(role.Permissions, types.Action(value))
		}
		roles = append(roles, role)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("could not collect authorization role definitions: %w", err)
	}
	return roles, nil
}

func CreateAuthorizationRoleBinding(
	ctx context.Context,
	binding *types.RoleBinding,
) error {
	if err := validateAuthorizationRoleBinding(*binding); err != nil {
		return err
	}
	if binding.ID == uuid.Nil {
		binding.ID = uuid.New()
	}
	if binding.Revision == 0 {
		binding.Revision = 1
	}
	if binding.Source == "" {
		binding.Source = "admin_api"
	}
	binding.Reason = strings.TrimSpace(binding.Reason)
	binding.EffectiveFrom = binding.EffectiveFrom.UTC()
	if binding.EffectiveUntil != nil {
		value := binding.EffectiveUntil.UTC()
		binding.EffectiveUntil = &value
	}

	return withAuthorizationTransaction(ctx, func(txContext context.Context) error {
		if err := ensureAuthorizationRoleExists(
			txContext,
			binding.OrganizationID,
			binding.RoleDefinitionID,
		); err != nil {
			return err
		}
		if err := ensureAuthorizationPrincipalExists(
			txContext,
			binding.OrganizationID,
			binding.PrincipalKind,
			binding.PrincipalID,
		); err != nil {
			return err
		}
		if err := ensureAuthorizationScopeExists(
			txContext,
			binding.OrganizationID,
			binding.Scope,
		); err != nil {
			return err
		}

		err := internalctx.GetDb(txContext).QueryRow(txContext, `
			INSERT INTO RoleBinding (
			  id,
			  organization_id,
			  role_definition_id,
			  principal_kind,
			  principal_id,
			  scope_kind,
			  scope_id,
			  effective_from,
			  effective_until,
			  reason,
			  revision,
			  created_by_useraccount_id,
			  source
			) VALUES (
			  @id,
			  @organizationID,
			  @roleDefinitionID,
			  @principalKind,
			  @principalID,
			  @scopeKind,
			  @scopeID,
			  @effectiveFrom,
			  @effectiveUntil,
			  @reason,
			  @revision,
			  @createdByUserID,
			  @source
			)
			RETURNING created_at`,
			pgx.NamedArgs{
				"id":               binding.ID,
				"organizationID":   binding.OrganizationID,
				"roleDefinitionID": binding.RoleDefinitionID,
				"principalKind":    binding.PrincipalKind,
				"principalID":      binding.PrincipalID,
				"scopeKind":        binding.Scope.Kind,
				"scopeID":          binding.Scope.ID,
				"effectiveFrom":    binding.EffectiveFrom,
				"effectiveUntil":   binding.EffectiveUntil,
				"reason":           binding.Reason,
				"revision":         binding.Revision,
				"createdByUserID":  binding.CreatedByUserID,
				"source":           binding.Source,
			},
		).Scan(&binding.CreatedAt)
		if err != nil {
			return mapAuthorizationWriteError("create role binding", err)
		}
		return nil
	})
}

func validateAuthorizationRoleBinding(binding types.RoleBinding) error {
	if binding.OrganizationID == uuid.Nil {
		return apierrors.NewBadRequest("organizationId is required")
	}
	if binding.RoleDefinitionID == uuid.Nil {
		return apierrors.NewBadRequest("roleDefinitionId is required")
	}
	if !binding.PrincipalKind.Valid() || binding.PrincipalID == uuid.Nil {
		return apierrors.NewBadRequest("principal is invalid")
	}
	if !binding.Scope.Kind.Supported() || binding.Scope.ID == uuid.Nil {
		return apierrors.NewBadRequest("unsupported scope")
	}
	if binding.Scope.Kind == types.PermissionScopeOrganization &&
		binding.Scope.ID != binding.OrganizationID {
		return apierrors.NewBadRequest("organization scope does not match organization")
	}
	if binding.EffectiveFrom.IsZero() {
		return apierrors.NewBadRequest("effectiveFrom is required")
	}
	if binding.EffectiveUntil != nil &&
		!binding.EffectiveUntil.After(binding.EffectiveFrom) {
		return apierrors.NewBadRequest("effectiveUntil must be after effectiveFrom")
	}
	if strings.TrimSpace(binding.Reason) == "" {
		return apierrors.NewBadRequest("reason is required")
	}
	return nil
}

func ListAuthorizationRoleBindings(
	ctx context.Context,
	organizationID uuid.UUID,
) ([]types.RoleBinding, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT
		  id,
		  created_at,
		  organization_id,
		  role_definition_id,
		  principal_kind,
		  principal_id,
		  scope_kind,
		  scope_id,
		  effective_from,
		  effective_until,
		  reason,
		  revision,
		  created_by_useraccount_id,
		  source
		FROM RoleBinding
		WHERE organization_id = @organizationID
		ORDER BY created_at, id`,
		pgx.NamedArgs{"organizationID": organizationID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query authorization role bindings: %w", err)
	}
	defer rows.Close()
	bindings := make([]types.RoleBinding, 0)
	for rows.Next() {
		var binding types.RoleBinding
		if err := rows.Scan(
			&binding.ID,
			&binding.CreatedAt,
			&binding.OrganizationID,
			&binding.RoleDefinitionID,
			&binding.PrincipalKind,
			&binding.PrincipalID,
			&binding.Scope.Kind,
			&binding.Scope.ID,
			&binding.EffectiveFrom,
			&binding.EffectiveUntil,
			&binding.Reason,
			&binding.Revision,
			&binding.CreatedByUserID,
			&binding.Source,
		); err != nil {
			return nil, fmt.Errorf("could not scan authorization role binding: %w", err)
		}
		bindings = append(bindings, binding)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("could not collect authorization role bindings: %w", err)
	}
	return bindings, nil
}

func CreateAuthorizationPrincipalGroup(
	ctx context.Context,
	group *types.PrincipalGroup,
) error {
	if group.OrganizationID == uuid.Nil {
		return apierrors.NewBadRequest("organizationId is required")
	}
	group.Key = strings.ToLower(strings.TrimSpace(group.Key))
	group.DisplayName = strings.TrimSpace(group.DisplayName)
	group.Description = strings.TrimSpace(group.Description)
	if !authorizationKeyPattern.MatchString(group.Key) {
		return apierrors.NewBadRequest("group key is invalid")
	}
	if group.DisplayName == "" {
		return apierrors.NewBadRequest("displayName is required")
	}
	if group.ID == uuid.Nil {
		group.ID = uuid.New()
	}
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		INSERT INTO PrincipalGroup (
		  id,
		  organization_id,
		  group_key,
		  display_name,
		  description,
		  created_by_useraccount_id
		) VALUES (
		  @id,
		  @organizationID,
		  @key,
		  @displayName,
		  @description,
		  @createdByUserID
		)
		RETURNING created_at`,
		pgx.NamedArgs{
			"id":              group.ID,
			"organizationID":  group.OrganizationID,
			"key":             group.Key,
			"displayName":     group.DisplayName,
			"description":     group.Description,
			"createdByUserID": group.CreatedByUserID,
		},
	).Scan(&group.CreatedAt)
	if err != nil {
		return mapAuthorizationWriteError("create principal group", err)
	}
	return nil
}

func ListAuthorizationPrincipalGroups(
	ctx context.Context,
	organizationID uuid.UUID,
) ([]types.PrincipalGroup, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT
		  id,
		  created_at,
		  organization_id,
		  group_key,
		  display_name,
		  description,
		  created_by_useraccount_id
		FROM PrincipalGroup
		WHERE organization_id = @organizationID
		ORDER BY group_key, id`,
		pgx.NamedArgs{"organizationID": organizationID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query principal groups: %w", err)
	}
	result, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.PrincipalGroup])
	if err != nil {
		return nil, fmt.Errorf("could not collect principal groups: %w", err)
	}
	return result, nil
}

func AddAuthorizationPrincipalGroupMember(
	ctx context.Context,
	member *types.PrincipalGroupMember,
) error {
	if member.OrganizationID == uuid.Nil ||
		member.GroupID == uuid.Nil ||
		member.UserAccountID == uuid.Nil {
		return apierrors.NewBadRequest("organizationId, groupId, and userAccountId are required")
	}
	if member.EffectiveFrom.IsZero() {
		return apierrors.NewBadRequest("effectiveFrom is required")
	}
	if member.EffectiveUntil != nil &&
		!member.EffectiveUntil.After(member.EffectiveFrom) {
		return apierrors.NewBadRequest("effectiveUntil must be after effectiveFrom")
	}
	member.Reason = strings.TrimSpace(member.Reason)
	if member.Reason == "" {
		return apierrors.NewBadRequest("reason is required")
	}
	if member.ID == uuid.Nil {
		member.ID = uuid.New()
	}
	member.EffectiveFrom = member.EffectiveFrom.UTC()
	if member.EffectiveUntil != nil {
		value := member.EffectiveUntil.UTC()
		member.EffectiveUntil = &value
	}
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		INSERT INTO PrincipalGroupMember (
		  id,
		  organization_id,
		  group_id,
		  user_account_id,
		  effective_from,
		  effective_until,
		  added_by_useraccount_id,
		  reason
		) VALUES (
		  @id,
		  @organizationID,
		  @groupID,
		  @userAccountID,
		  @effectiveFrom,
		  @effectiveUntil,
		  @addedByUserID,
		  @reason
		)
		RETURNING created_at`,
		pgx.NamedArgs{
			"id":             member.ID,
			"organizationID": member.OrganizationID,
			"groupID":        member.GroupID,
			"userAccountID":  member.UserAccountID,
			"effectiveFrom":  member.EffectiveFrom,
			"effectiveUntil": member.EffectiveUntil,
			"addedByUserID":  member.AddedByUserID,
			"reason":         member.Reason,
		},
	).Scan(&member.CreatedAt)
	if err != nil {
		return mapAuthorizationWriteError("add principal group member", err)
	}
	return nil
}

func ListAuthorizationPrincipalGroupMembers(
	ctx context.Context,
	organizationID uuid.UUID,
	groupID uuid.UUID,
) ([]types.PrincipalGroupMember, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT
		  id,
		  created_at,
		  organization_id,
		  group_id,
		  user_account_id,
		  effective_from,
		  effective_until,
		  added_by_useraccount_id,
		  reason
		FROM PrincipalGroupMember
		WHERE organization_id = @organizationID
		  AND group_id = @groupID
		ORDER BY user_account_id, effective_from, id`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"groupID":        groupID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query principal group members: %w", err)
	}
	result, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.PrincipalGroupMember])
	if err != nil {
		return nil, fmt.Errorf("could not collect principal group members: %w", err)
	}
	return result, nil
}

func BackfillBuiltInAuthorization(
	ctx context.Context,
	organizationID uuid.UUID,
) error {
	if organizationID == uuid.Nil {
		return apierrors.NewBadRequest("organizationId is required")
	}
	return withAuthorizationTransaction(ctx, func(txContext context.Context) error {
		var completed bool
		if err := internalctx.GetDb(txContext).QueryRow(txContext, `
			SELECT COALESCE((
			  SELECT completed
			  FROM AuthorizationBackfillCheckpoint
			  WHERE organization_id = @organizationID
			    AND checkpoint_key = 'built_in_roles_v1'
			), false)`,
			pgx.NamedArgs{"organizationID": organizationID},
		).Scan(&completed); err != nil {
			return fmt.Errorf("could not read built-in authorization checkpoint: %w", err)
		}
		if completed {
			return nil
		}

		queries := []string{
			authorizationRoleDefinitionBackfillSQL,
			authorizationRolePermissionBackfillSQL,
			authorizationCheckpointBackfillSQL,
		}
		for _, query := range queries {
			if _, err := internalctx.GetDb(txContext).Exec(
				txContext,
				query,
				pgx.NamedArgs{"organizationID": organizationID},
			); err != nil {
				return mapAuthorizationWriteError("backfill built-in authorization", err)
			}
		}
		return nil
	})
}

const authorizationRoleDefinitionBackfillSQL = `
	INSERT INTO RoleDefinition (
	  organization_id,
	  role_key,
	  display_name,
	  description,
	  built_in,
	  source_legacy_role,
	  revision
	)
	SELECT
	  @organizationID,
	  role.role_key,
	  role.display_name,
	  role.description,
	  true,
	  role.legacy_role::USER_ROLE,
	  1
	FROM (
	  VALUES
	    ('legacy.read_only', 'Viewer', 'Built-in compatibility role for read-only users.', 'read_only'),
	    ('legacy.read_write', 'Developer', 'Built-in compatibility role for read-write users.', 'read_write'),
	    ('legacy.admin', 'Administrator', 'Built-in compatibility role for administrators.', 'admin')
	) AS role(role_key, display_name, description, legacy_role)
	WHERE EXISTS (
	  SELECT 1 FROM Organization WHERE id = @organizationID
	)
	ON CONFLICT (organization_id, role_key) DO NOTHING`

const authorizationRolePermissionBackfillSQL = `
	INSERT INTO RolePermission (
	  organization_id,
	  role_definition_id,
	  action
	)
	SELECT
	  definition.organization_id,
	  definition.id,
	  permission.action
	FROM RoleDefinition definition
	JOIN (
	  VALUES
	    ('read_only', 'audit.view'),
	    ('read_only', 'audit.export'),
	    ('read_write', 'release.create'),
	    ('read_write', 'release.publish'),
	    ('read_write', 'registry.manage'),
	    ('read_write', 'config.manage'),
	    ('read_write', 'plan.create'),
	    ('read_write', 'plan.publish'),
	    ('read_write', 'plan.execute'),
	    ('read_write', 'campaign.control'),
	    ('read_write', 'audit.view'),
	    ('read_write', 'audit.export'),
	    ('admin', 'release.create'),
	    ('admin', 'release.publish'),
	    ('admin', 'release.block'),
	    ('admin', 'registry.manage'),
	    ('admin', 'config.manage'),
	    ('admin', 'plan.create'),
	    ('admin', 'plan.publish'),
	    ('admin', 'plan.execute'),
	    ('admin', 'approval.decide'),
	    ('admin', 'policy.manage'),
	    ('admin', 'calendar.manage'),
	    ('admin', 'freeze.manage'),
	    ('admin', 'emergency.override'),
	    ('admin', 'campaign.control'),
	    ('admin', 'observer.manage'),
	    ('admin', 'reconciliation.decide'),
	    ('admin', 'audit.view'),
	    ('admin', 'audit.export'),
	    ('admin', 'sample.retire'),
	    ('admin', 'authorization.manage')
	) AS permission(legacy_role, action)
	  ON permission.legacy_role = definition.source_legacy_role::TEXT
	WHERE definition.organization_id = @organizationID
	  AND definition.built_in
	ON CONFLICT (role_definition_id, action) DO NOTHING`

const authorizationCheckpointBackfillSQL = `
	INSERT INTO AuthorizationBackfillCheckpoint (
	  organization_id,
	  checkpoint_key,
	  completed,
	  completed_at
	) VALUES (
	  @organizationID,
	  'built_in_roles_v1',
	  true,
	  now()
	)
	ON CONFLICT (organization_id, checkpoint_key) DO UPDATE
	SET
	  completed = true,
	  completed_at = COALESCE(
	    AuthorizationBackfillCheckpoint.completed_at,
	    EXCLUDED.completed_at
	  ),
	  updated_at = now()`

func ensureAuthorizationRoleExists(
	ctx context.Context,
	organizationID uuid.UUID,
	roleID uuid.UUID,
) error {
	return authorizationResourceExists(ctx, "RoleDefinition", organizationID, roleID)
}

func ensureAuthorizationPrincipalExists(
	ctx context.Context,
	organizationID uuid.UUID,
	kind types.AuthorizationPrincipalKind,
	principalID uuid.UUID,
) error {
	var exists bool
	var err error
	switch kind {
	case types.AuthorizationPrincipalUser:
		err = internalctx.GetDb(ctx).QueryRow(ctx, `
			SELECT EXISTS (
			  SELECT 1
			  FROM Organization_UserAccount
			  WHERE organization_id = @organizationID
			    AND user_account_id = @principalID
			)`,
			pgx.NamedArgs{
				"organizationID": organizationID,
				"principalID":    principalID,
			},
		).Scan(&exists)
	case types.AuthorizationPrincipalGroup:
		err = internalctx.GetDb(ctx).QueryRow(ctx, `
			SELECT EXISTS (
			  SELECT 1
			  FROM PrincipalGroup
			  WHERE organization_id = @organizationID
			    AND id = @principalID
			)`,
			pgx.NamedArgs{
				"organizationID": organizationID,
				"principalID":    principalID,
			},
		).Scan(&exists)
	default:
		return apierrors.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("could not validate authorization principal: %w", err)
	}
	if !exists {
		return apierrors.ErrNotFound
	}
	return nil
}

func ensureAuthorizationScopeExists(
	ctx context.Context,
	organizationID uuid.UUID,
	scope types.ScopeRef,
) error {
	if scope.ID == uuid.Nil || !scope.Kind.Supported() {
		return apierrors.ErrNotFound
	}
	switch scope.Kind {
	case types.PermissionScopeOrganization:
		if scope.ID != organizationID {
			return apierrors.ErrNotFound
		}
		return nil
	case types.PermissionScopeCustomer:
		return authorizationResourceExists(ctx, "CustomerOrganization", organizationID, scope.ID)
	case types.PermissionScopeEnvironment:
		return authorizationResourceExists(ctx, "Environment", organizationID, scope.ID)
	case types.PermissionScopeDeploymentUnit:
		return authorizationResourceExists(ctx, "DeploymentUnit", organizationID, scope.ID)
	case types.PermissionScopeComponent:
		return authorizationResourceExists(ctx, "ComponentDefinition", organizationID, scope.ID)
	case types.PermissionScopeCampaign:
		return nil
	default:
		return apierrors.ErrNotFound
	}
}

func withAuthorizationTransaction(
	ctx context.Context,
	fn func(context.Context) error,
) error {
	tx, err := internalctx.GetDb(ctx).Begin(ctx)
	if err != nil {
		return fmt.Errorf("could not begin authorization transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	txContext := internalctx.WithDb(ctx, tx)
	if err := fn(txContext); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("could not commit authorization transaction: %w", err)
	}
	return nil
}

func mapAuthorizationWriteError(action string, err error) error {
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		switch pgError.Code {
		case pgerrcode.UniqueViolation:
			return fmt.Errorf("could not %s: %w", action, apierrors.ErrAlreadyExists)
		case pgerrcode.ForeignKeyViolation:
			return fmt.Errorf("could not %s: %w", action, apierrors.ErrNotFound)
		case pgerrcode.CheckViolation, pgerrcode.NotNullViolation:
			return fmt.Errorf("could not %s: %w", action, apierrors.ErrBadRequest)
		}
	}
	return fmt.Errorf("could not %s: %w", action, err)
}
