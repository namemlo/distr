# PR-056 - Canonical Deployment Registry Identity

## Generic User Story

As an operator, I want a stable registry of delivery scopes, environment assignments, physical units, subscribers,
and logical components so that imports and later reconciliation can reason about identity without guessing from
mutable deployment names.

## Scope

- Add migration 139 and ADR-0056.
- Add organization-scoped repositories and pure topology validation for scopes, assignments, units, subscribers,
  component definitions, aliases, instances, and aggregate placements.
- Add typed API validation, mapping, CRUD/list handlers, and generated OpenAPI routes below
  `/api/v1/deployment-registry`.
- Keep existing v1 reads, writes, deployments, and agents unchanged.
- Gate only new registry mutations with `operator_control_plane_v2`.

## Identity Contract

| Concern                   | Contract                                                                                                                 |
| ------------------------- | ------------------------------------------------------------------------------------------------------------------------ |
| Delivery model            | `dedicated`, `shared`, `external`                                                                                        |
| Management classification | `managed`, `observe_only`, `external`, `legacy_cutover`, `backup`, `retired`, `unclassified`                             |
| Tenant boundary           | Every repository operation takes an organization ID; relations use composite organization foreign keys                   |
| Shared topology           | Initial customer IDs and their sorted, domain-separated SHA-256 checksum are committed atomically, sealed, and immutable |
| Target environment        | Assignment intervals are half-open and cannot overlap for the same target                                                |
| Component rename          | Preserve the old physical name as an alias, or retire the old instance and create a new identity                         |
| Pagination                | Versioned opaque keyset cursor; default 50, maximum 100, fetch `limit + 1`                                               |
| Canonical text            | Names and physical values are trimmed; aliases are trimmed lowercase; physical uniqueness is case-insensitive            |
| Placement reads           | The root unit and every related identity are assembled in one repeatable-read snapshot                                   |
| Downgrade                 | Migration 139 down refuses while any registry row exists and succeeds only for an empty registry                         |

## API

The API root is `/api/v1/deployment-registry`.

| Resource    | Routes                                                                |
| ----------- | --------------------------------------------------------------------- |
| Scopes      | `GET/POST /scopes`, `GET/PUT/DELETE /scopes/{scopeId}`                |
| Assignments | `GET/POST /assignments`, `GET/PUT/DELETE /assignments/{assignmentId}` |
| Units       | `GET/POST /units`, `GET/PUT/DELETE /units/{unitId}`                   |
| Subscribers | `GET/POST /subscribers`, `GET/PUT/DELETE /subscribers/{subscriberId}` |
| Definitions | `GET/POST /definitions`, `GET/PUT/DELETE /definitions/{definitionId}` |
| Aliases     | `GET/POST /aliases`, `GET/PUT/DELETE /aliases/{aliasId}`              |
| Instances   | `GET/POST /instances`, `GET/PUT/DELETE /instances/{instanceId}`       |
| Placements  | `GET /placements`, `GET /placements/{unitId}`                         |

All list routes accept `cursor` and `limit`. Responses omit internal organization IDs. Cross-organization IDs use
the same 404 response as missing IDs. Protected delete returns a stable `409 <resource> is in use` response and
does not expose SQLSTATE, constraint, table, or driver details.

`POST /units` accepts `subscriberCustomerOrganizationIds` for shared units. The request checksum must match those
unique customer IDs. The unit, all subscriber rows, and the one-way initialization seal commit in one transaction;
individual subscriber POST, PUT, and DELETE operations cannot change a sealed membership. The standalone
subscriber routes are compatibility endpoints after sealing: POST and DELETE return 409, PUT returns 200 only for
an exact no-op, and a membership-changing PUT returns 409. OpenAPI omits the unreachable subscriber POST 200 and
DELETE 204 responses.

The generated contract enumerates operation-specific, runtime-exact 400, 403, 404, and 409 outcomes rather than
advertising one broad mutation matrix. Successful payloads use JSON, successful deletions have no response body,
and runtime `http.Error` responses use a `text/plain` string schema.

Authenticated reads are available even when `operator_control_plane_v2` is off. POST, PUT, and DELETE require an
admin or read-write role, reject super-admin mutation, and return 404 while the flag is disabled.

## Required Impact Report

### Database/schema impact

Migration 139 adds `DeploymentScope`, `TargetEnvironmentAssignment`, `DeploymentUnit`,
`DeploymentUnitSubscriber`, `ComponentDefinition`, `ComponentAlias`, and `ComponentInstance`. It adds composite
organization foreign keys, active-identity indexes, deterministic keyset indexes, an overlap guard for assignment
intervals, a deferred final subscriber/checksum constraint, a one-way initialization seal, and a subscriber-row
mutation guard. A shared unit cannot commit without its complete initial membership, and later direct SQL
insert/update/delete attempts fail at the schema boundary.

All seven list resources have a matching `(organization_id, created_at DESC, id DESC)` page index. The repository
and idempotent schema triggers both trim names and physical values and trim/lower aliases; non-empty checks and
alias/case-insensitive physical-identity uniqueness operate on those canonical stored values.

The down migration takes exclusive locks and refuses while any registry record remains.

### Public API impact

Adds the routes listed above. This is an additive API surface. Existing endpoints and response fields do not
change. Cursor values are opaque and versioned; clients must not decode or construct them.

### Frontend/UI impact

None in this slice. The typed API is ready for the later operator setup UI.

### Agent/protocol impact

None. No agent message, execution lease, callback, or deployment payload changes.

### Feature-flag impact

`operator_control_plane_v2` gates registry mutations only. Removing the flag immediately hides new mutation
routes after restart while preserving registry reads and all v1 behavior.

### Security impact

Positive tenant isolation. Every query and mutation includes the authenticated organization boundary, and every
cross-resource relationship uses an organization-consistent foreign key. Expected write and delete errors are
translated to stable domain responses without database-detail leakage.

### Backward-compatibility impact

Existing data is not backfilled or reclassified. Existing deployments, tasks, agents, and v1 APIs continue
unchanged. Migration 139 is additive, but rollback is intentionally blocked after registry adoption until records
are retired and removed through an explicit reviewed procedure.

## Validation

- Focused pure validation, API, mapping, handler, routed authorization, and complete OpenAPI contract tests.
- Isolated PostgreSQL migration/repository and routed authenticated handler tests for complete topology,
  atomic shared membership, direct SQL mutation rejection and canonicalization, normalized duplicate conflicts,
  deterministic repeatable-read placement assembly, every resource family, flag-off reads, cross-organization
  rejection, bounded keyset pagination, stable subscriber conflicts/no-ops, protected deletion, and guarded
  downgrade.
- Full Go suite, community build, Hub and agent builds, migration validation, static analysis, diff checks, and
  credential-neutral scans before merge.

The repository integration harness skips explicitly when `DISTR_TEST_DATABASE_URL` is absent. A pinned PostgreSQL
16/18 CI matrix runs the migration, repository, and authenticated handler contract tests; skipped local integration
cases are not represented as live database proof.
