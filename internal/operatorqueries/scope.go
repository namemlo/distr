package operatorqueries

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

const auditViewScopeChecksumVersion = "operator-audit-view-scopes/v1"

// AuditViewScopes is the normalized, deny-by-default authorization input for
// operator read-model SQL. Callers must obtain grants through the
// organization-scoped authorization repository before resolving them here.
//
// An organization-wide grant subsumes every narrower grant. Otherwise queries
// must match at least one relevant row ancestor against the corresponding ID
// slice. All slices are non-nil, deduplicated, and sorted by native UUID bytes.
type AuditViewScopes struct {
	OrganizationID uuid.UUID
	DecisionAt     time.Time

	OrganizationWide  bool
	CustomerIDs       []uuid.UUID
	EnvironmentIDs    []uuid.UUID
	DeploymentUnitIDs []uuid.UUID
	ComponentIDs      []uuid.UUID
	CampaignIDs       []uuid.UUID
}

// ResolveAuditViewScopes selects effective audit.view grants at one decision
// instant and converts them into bounded SQL inputs. A zero organization,
// decision instant, malformed grant, unsupported scope, or unrelated action is
// ignored so the resulting scope set fails closed.
func ResolveAuditViewScopes(
	organizationID uuid.UUID,
	grants []types.AccessGrant,
	decisionAt time.Time,
) AuditViewScopes {
	scopes := emptyAuditViewScopes(organizationID, decisionAt.UTC())
	if organizationID == uuid.Nil || decisionAt.IsZero() {
		return scopes
	}

	for _, grant := range grants {
		if !auditViewGrantEffectiveAt(grant, decisionAt) ||
			!slices.Contains(grant.Actions, types.ActionAuditView) {
			continue
		}

		switch grant.Scope.Kind {
		case types.PermissionScopeOrganization:
			if grant.Scope.ID == organizationID {
				scopes.OrganizationWide = true
			}
		case types.PermissionScopeCustomer:
			scopes.CustomerIDs = appendScopeID(scopes.CustomerIDs, grant.Scope.ID)
		case types.PermissionScopeEnvironment:
			scopes.EnvironmentIDs = appendScopeID(scopes.EnvironmentIDs, grant.Scope.ID)
		case types.PermissionScopeDeploymentUnit:
			scopes.DeploymentUnitIDs = appendScopeID(scopes.DeploymentUnitIDs, grant.Scope.ID)
		case types.PermissionScopeComponent:
			scopes.ComponentIDs = appendScopeID(scopes.ComponentIDs, grant.Scope.ID)
		case types.PermissionScopeCampaign:
			scopes.CampaignIDs = appendScopeID(scopes.CampaignIDs, grant.Scope.ID)
		}
	}

	if scopes.OrganizationWide {
		scopes.CustomerIDs = []uuid.UUID{}
		scopes.EnvironmentIDs = []uuid.UUID{}
		scopes.DeploymentUnitIDs = []uuid.UUID{}
		scopes.ComponentIDs = []uuid.UUID{}
		scopes.CampaignIDs = []uuid.UUID{}
		return scopes
	}

	scopes.CustomerIDs = normalizeScopeIDs(scopes.CustomerIDs)
	scopes.EnvironmentIDs = normalizeScopeIDs(scopes.EnvironmentIDs)
	scopes.DeploymentUnitIDs = normalizeScopeIDs(scopes.DeploymentUnitIDs)
	scopes.ComponentIDs = normalizeScopeIDs(scopes.ComponentIDs)
	scopes.CampaignIDs = normalizeScopeIDs(scopes.CampaignIDs)
	return scopes
}

func (scopes AuditViewScopes) Empty() bool {
	return !scopes.OrganizationWide &&
		len(scopes.CustomerIDs) == 0 &&
		len(scopes.EnvironmentIDs) == 0 &&
		len(scopes.DeploymentUnitIDs) == 0 &&
		len(scopes.ComponentIDs) == 0 &&
		len(scopes.CampaignIDs) == 0
}

// Matches checks an exact resource or ancestor scope. Non-organization IDs are
// meaningful only inside OrganizationID; the repository query must always keep
// its independent organization_id predicate.
func (scopes AuditViewScopes) Matches(scope types.ScopeRef) bool {
	if scopes.OrganizationID == uuid.Nil || scope.ID == uuid.Nil || !scope.Kind.Supported() {
		return false
	}
	if scope.Kind == types.PermissionScopeOrganization {
		return scopes.OrganizationWide && scope.ID == scopes.OrganizationID
	}
	if scopes.OrganizationWide {
		return true
	}

	switch scope.Kind {
	case types.PermissionScopeCustomer:
		return slices.Contains(scopes.CustomerIDs, scope.ID)
	case types.PermissionScopeEnvironment:
		return slices.Contains(scopes.EnvironmentIDs, scope.ID)
	case types.PermissionScopeDeploymentUnit:
		return slices.Contains(scopes.DeploymentUnitIDs, scope.ID)
	case types.PermissionScopeComponent:
		return slices.Contains(scopes.ComponentIDs, scope.ID)
	case types.PermissionScopeCampaign:
		return slices.Contains(scopes.CampaignIDs, scope.ID)
	default:
		return false
	}
}

func (scopes AuditViewScopes) MatchesAny(candidates ...types.ScopeRef) bool {
	for _, candidate := range candidates {
		if scopes.Matches(candidate) {
			return true
		}
	}
	return false
}

// ToOperatorScopeFilter returns a detached copy ready to embed in any operator
// read-model filter. Detached slices prevent downstream query normalization from
// changing the authorization decision or its cursor checksum.
func (scopes AuditViewScopes) ToOperatorScopeFilter() types.OperatorScopeFilter {
	return types.OperatorScopeFilter{
		OrganizationID:    scopes.OrganizationID,
		DecisionAt:        scopes.DecisionAt,
		OrganizationWide:  scopes.OrganizationWide,
		CustomerIDs:       slices.Clone(scopes.CustomerIDs),
		EnvironmentIDs:    slices.Clone(scopes.EnvironmentIDs),
		DeploymentUnitIDs: slices.Clone(scopes.DeploymentUnitIDs),
		ComponentIDs:      slices.Clone(scopes.ComponentIDs),
		CampaignIDs:       slices.Clone(scopes.CampaignIDs),
	}
}

// AuditViewScopesFromOperatorScopeFilter validates the trust-boundary copy
// received by a repository before it is used in SQL or cursor binding. The
// arrays must already be canonical so equivalent access sets have exactly one
// representation.
func AuditViewScopesFromOperatorScopeFilter(
	filter types.OperatorScopeFilter,
) (AuditViewScopes, error) {
	if filter.OrganizationID == uuid.Nil || filter.DecisionAt.IsZero() ||
		!canonicalScopeIDs(filter.CustomerIDs) ||
		!canonicalScopeIDs(filter.EnvironmentIDs) ||
		!canonicalScopeIDs(filter.DeploymentUnitIDs) ||
		!canonicalScopeIDs(filter.ComponentIDs) ||
		!canonicalScopeIDs(filter.CampaignIDs) ||
		(filter.OrganizationWide && (len(filter.CustomerIDs) != 0 ||
			len(filter.EnvironmentIDs) != 0 ||
			len(filter.DeploymentUnitIDs) != 0 ||
			len(filter.ComponentIDs) != 0 ||
			len(filter.CampaignIDs) != 0)) {
		return AuditViewScopes{}, apierrors.NewBadRequest("operator scope filter is invalid")
	}

	return AuditViewScopes{
		OrganizationID:    filter.OrganizationID,
		DecisionAt:        filter.DecisionAt.UTC(),
		OrganizationWide:  filter.OrganizationWide,
		CustomerIDs:       slices.Clone(filter.CustomerIDs),
		EnvironmentIDs:    slices.Clone(filter.EnvironmentIDs),
		DeploymentUnitIDs: slices.Clone(filter.DeploymentUnitIDs),
		ComponentIDs:      slices.Clone(filter.ComponentIDs),
		CampaignIDs:       slices.Clone(filter.CampaignIDs),
	}, nil
}

// Checksum binds an operator cursor to the effective visibility set without
// exposing tenant or scope IDs. DecisionAt is deliberately excluded: pagination
// must freeze that instant in its cursor separately, while this checksum changes
// only when the normalized visible scope set changes.
func (scopes AuditViewScopes) Checksum() string {
	hash := sha256.New()
	writeScopeChecksumPart(hash, auditViewScopeChecksumVersion)
	writeScopeChecksumPart(hash, scopes.OrganizationID.String())
	if scopes.OrganizationWide {
		writeScopeChecksumPart(hash, "organization-wide")
		return "sha256:" + hex.EncodeToString(hash.Sum(nil))
	}

	writeScopeChecksumIDs(hash, string(types.PermissionScopeCustomer), scopes.CustomerIDs)
	writeScopeChecksumIDs(hash, string(types.PermissionScopeEnvironment), scopes.EnvironmentIDs)
	writeScopeChecksumIDs(
		hash,
		string(types.PermissionScopeDeploymentUnit),
		scopes.DeploymentUnitIDs,
	)
	writeScopeChecksumIDs(hash, string(types.PermissionScopeComponent), scopes.ComponentIDs)
	writeScopeChecksumIDs(hash, string(types.PermissionScopeCampaign), scopes.CampaignIDs)
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func emptyAuditViewScopes(organizationID uuid.UUID, decisionAt time.Time) AuditViewScopes {
	return AuditViewScopes{
		OrganizationID:    organizationID,
		DecisionAt:        decisionAt,
		CustomerIDs:       []uuid.UUID{},
		EnvironmentIDs:    []uuid.UUID{},
		DeploymentUnitIDs: []uuid.UUID{},
		ComponentIDs:      []uuid.UUID{},
		CampaignIDs:       []uuid.UUID{},
	}
}

func auditViewGrantEffectiveAt(grant types.AccessGrant, at time.Time) bool {
	if grant.BindingID == uuid.Nil || grant.Scope.ID == uuid.Nil ||
		grant.EffectiveFrom.IsZero() || grant.EffectiveFrom.After(at) {
		return false
	}
	return grant.EffectiveUntil == nil || grant.EffectiveUntil.After(at)
}

func appendScopeID(ids []uuid.UUID, id uuid.UUID) []uuid.UUID {
	if id == uuid.Nil {
		return ids
	}
	return append(ids, id)
}

func normalizeScopeIDs(ids []uuid.UUID) []uuid.UUID {
	normalized := slices.Clone(ids)
	slices.SortFunc(normalized, func(left, right uuid.UUID) int {
		return bytes.Compare(left[:], right[:])
	})
	return slices.Compact(normalized)
}

func canonicalScopeIDs(ids []uuid.UUID) bool {
	if ids == nil {
		return false
	}
	for index, id := range ids {
		if id == uuid.Nil ||
			(index > 0 && bytes.Compare(ids[index-1][:], id[:]) >= 0) {
			return false
		}
	}
	return true
}

type scopeChecksumWriter interface {
	Write([]byte) (int, error)
}

func writeScopeChecksumIDs(writer scopeChecksumWriter, kind string, ids []uuid.UUID) {
	writeScopeChecksumPart(writer, kind)
	for _, id := range normalizeScopeIDs(ids) {
		writeScopeChecksumPart(writer, id.String())
	}
}

func writeScopeChecksumPart(writer scopeChecksumWriter, value string) {
	_, _ = writer.Write([]byte(value))
	_, _ = writer.Write([]byte{0})
}
