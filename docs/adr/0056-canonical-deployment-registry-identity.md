# ADR-0056: Canonical Deployment Registry Identity

## Status

Accepted

## Context

Deployment targets identify where software runs, but they do not provide a stable logical identity for the
customer or shared scope, the target's environment interval, a physical deployment unit, or the components inside
that unit. Physical names can change, one target can host multiple scopes, and shared infrastructure can serve more
than one customer. Inferring these relationships from names or from the current deployment record would make
imports ambiguous and would lose historical identity during rename or retirement.

The registry must remain organization scoped, work for dedicated, shared, and externally operated delivery, and
preserve existing v1 behavior while the operator control-plane program is incomplete. Its public lists must also
remain stable at fleet scale without offset-pagination drift.

## Decision

Add seven public organization-owned identities in migration 139:

- `DeploymentScope` describes the logical delivery scope. Its delivery model is exactly `dedicated`, `shared`, or
  `external`.
- `TargetEnvironmentAssignment` records an explicit half-open environment interval for one deployment target.
  Overlapping intervals on the same target are rejected under a transaction-scoped advisory lock.
- `DeploymentUnit` identifies one physical unit within a scope, target, and assignment. The active physical
  identity and active logical key are unique.
- `DeploymentUnitSubscriber` binds an immutable shared-unit subscriber set to customer organizations.
- `ComponentDefinition` provides a canonical logical component key.
- `ComponentAlias` preserves an active legacy physical name for one component definition.
- `ComponentInstance` binds a physical component name to a definition and deployment unit.

Migration 139 also adds the private `ComponentInstanceRename` evidence table. It is not an API resource. Every row
records one immutable rename hop with its organization, component instance, active prior-name alias, exact old
physical name, exact new physical name, and timestamp. Update is always forbidden. Delete is allowed only during
the organization-retention cascade authorized by the transaction-local
`distr.deployment_registry_deletion_reason=ORGANIZATION_RETENTION` marker.

The repository and idempotent schema triggers apply the same canonicalization: human-readable names, unit physical
identities, and instance physical names are trimmed, while aliases are trimmed and lowercased. Schema checks still
reject empty canonical values. Active unit physical identities and active instance physical names preserve their
trimmed supplied case but compare case-insensitively for uniqueness; aliases use their stored lowercase value for
uniqueness.

Delivery topology and management classification are independent. Management classification is exactly `managed`,
`observe_only`, `external`, `legacy_cutover`, `backup`, `retired`, or `unclassified`. A retired resource carries a
retirement timestamp; retirement is not represented by changing its canonical key.

Every relationship includes `organization_id` in its foreign key. Repository reads and writes require the
organization ID explicitly, and a foreign or cross-organization identifier is returned as not found without
revealing whether it exists elsewhere.

Shared units store a deterministic SHA-256 checksum of their active customer IDs sorted by UUID bytes. PostgreSQL
orders the native UUID values rather than collation-sensitive text, and Go compares the UUID byte arrays
explicitly. The unit checksum is
immutable. `POST /units` supplies the initial customer IDs with the checksum. The unit and subscriber rows are
inserted in one transaction and then receive a one-way initialization seal. Deferred schema validation prevents an
unsealed or checksum-mismatched unit from committing, while a subscriber-row trigger rejects every later direct
insert, update, or delete. The standalone subscriber routes remain compatibility endpoints: POST and DELETE return
conflict after sealing, PUT returns the existing row only for an exact no-op, and any membership-changing PUT
returns conflict. Their generated OpenAPI contract therefore omits unreachable POST 200 and DELETE 204 responses.
A topology change retires the unit and creates a new identity.

Physical component renames require `renamedFrom` to equal the row-locked current physical name and require a
row-locked active alias for that prior name. The instance update and append-only evidence insert commit atomically.
An alias or instance referenced by rename evidence cannot be retired or hard-deleted; alias PUT, alias DELETE, and
instance DELETE expose that state as `409 Conflict`. Concurrent renames serialize on the instance, and concurrent
alias retirement or deletion serializes on the alias. Creating an imported instance with `renamedFrom` records the
same private evidence. The alternative is to retire the old instance and create a new identity without claiming an
in-place rename.

Expose organization-scoped CRUD/list routes below `/api/v1/deployment-registry` for scopes, assignments, units,
subscribers, definitions, aliases, and instances, plus aggregate placement reads. Public responses omit the
internal organization ID. Lists use a versioned opaque keyset cursor ordered by `(created_at DESC, id DESC)`,
default to 50 records, reject limits above 100, and fetch one extra row to determine `nextCursor`.
Every list resource has a matching `(organization_id, created_at DESC, id DESC)` index. Both single-placement and
placement-list assembly load the root unit and all related identities in one repeatable-read snapshot. Placement
lists use seven batch queries at any supported page size: roots, assignments, sibling units, subscribers,
instances, definitions, and aliases.
Generated operations enumerate operation-specific, runtime-exact authorization, validation, not-found, and
conflict statuses. Success payloads are JSON, no-content deletion remains bodyless, and `http.Error` outcomes are
documented as `text/plain` strings.

The process-wide `operator_control_plane_v2` flag gates only POST, PUT, and DELETE operations. Authenticated reads
remain available when it is disabled so recorded identity and history stay inspectable. Existing v1 routes and
execution behavior are unchanged.

Hard deletion is limited to unreferenced setup mistakes. Foreign-key protected history returns a stable conflict,
and expected domain failures never expose database constraint, table, or SQLSTATE details. Migration 139 down takes
exclusive locks and refuses to cross the boundary while any registry row remains; an empty registry can be
downgraded.

Organization retention is the sole exception to the ordinary history guards. Registry composite foreign keys use
`NO ACTION DEFERRABLE INITIALLY IMMEDIATE`, including links to customer organizations, deployment targets, and
environments. The retention transaction locks eligible organizations, sets the marker locally, defers all
deferrable constraints, and then cascades the complete graph. A normal or differently marked subscriber/history
delete remains rejected.

## Consequences

Operators and later import/reconciliation work receive a single canonical identity graph instead of repeatedly
inferring topology from mutable names. Dedicated, shared, external, legacy-cutover, backup, and observation-only
records can coexist without overloading the delivery model.

The schema adds seven public tables plus one private evidence table, deterministic page indexes for every list
resource, canonical-text normalization and checks, database-enforced subscriber initialization and immutability,
retention-safe deferred graph constraints, and a guarded downgrade. Reads of a placement load its root and related
identities in one repeatable-read snapshot. Large list callers must retain and return the opaque cursor rather than
constructing offsets.

Changing a shared subscriber set or a physical identity creates new history instead of editing old identity. This
cost is intentional: the registry favors auditability and deterministic reconciliation over in-place convenience.

## Alternatives Considered

- Reuse deployment-target names as canonical identity. Rejected because names are mutable and cannot represent
  multiple scopes, components, or shared subscribers.
- Put `observe_only` in the delivery model. Rejected because observation is a management classification and can
  apply independently to dedicated, shared, or external delivery.
- Use one global component-name table. Rejected because aliases and instances require organization ownership and
  explicit unit placement.
- Allow offset pagination. Rejected because concurrent inserts can duplicate or skip fleet results.
- Update physical names and subscriber membership in place. Rejected because this erases the identity evidence
  required by restartable imports and later audit.
- Cascade the down migration through populated registry tables. Rejected because downgrade must not silently erase
  control-plane history.

## Validation

- Pure validation tests cover dedicated/shared topology, deterministic subscriber checksums, overlapping active
  assignments, duplicate physical identity, missing subscribers, orphan instances, alias-required rename,
  cross-organization substitution, and stable ordered issue codes.
- Migration/repository tests cover 138-to-139 apply, retention of a sealed shared topology, complete dedicated and
  shared placements, atomic subscriber
  initialization, schema-level direct mutation rejection, direct-SQL canonicalization and normalized duplicate
  conflicts, append-only multi-hop rename evidence, sequential and concurrent alias/instance protection,
  case-insensitive physical uniqueness, native-UUID checksum ordering under an explicit non-default text collation,
  cross-organization rejection, keyset order and bounds, a maximum-limit runtime query-count assertion,
  deterministic lock-stepped placement snapshot consistency, zero-row/concurrent delete-update not-found behavior,
  protected deletion, and guarded down migration.
- API, mapping, handler, and routed integration tests cover request validation, organization-ID omission, every
  resource family, admin and read-only access, unauthenticated rejection, reads with the mutation flag disabled,
  foreign-ID not-found behavior, bounded pagination, stable subscriber compatibility conflicts/no-ops,
  non-leaking protected-delete conflicts, and complete generated OpenAPI parameters, schemas, statuses, and
  security.
- The community build, full Go suite, migration-pair validator, static analysis, and credential-neutral diff checks
  run before merge. A pinned PostgreSQL 16/18 CI matrix supplies the isolated database required by integration
  cases.
