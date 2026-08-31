# ADR-0077: Baseline Adoption Fact Separation

## Status

Accepted

## Context

Baseline adoption binds two different classes of immutable fact:

- release facts declared by a published Component Release, including its
  application semantic version and canonical release checksum; and
- runtime facts supplied by a current authenticated observation, including its
  deployed schema version and capability-set checksum.

The first implementation admitted an adoption only when the observed schema
version equaled the application version and the observed capability checksum
equaled the Component Release checksum. Those equalities are not generally
true. A service application version, its data-contract schema version, its
capability-set checksum, and the checksum of its complete release declaration
are independent identities.

## Decision

Migration 169 adds `application_version` to the append-only
`BaselineAdoptionComponent` read model. Existing rows are backfilled only from
the exact Component Release pin in their retained deployment-plan canonical
payload. No observation, desired-state, release, request checksum, or outcome
checksum is rewritten.

New adoptions freeze the application version from that release pin. The schema
version and capability checksum remain caller-selected observation identities,
are included independently in the existing canonical adoption request checksum,
and must exactly match the retained current observation and the adopted active
desired revision. They are never inferred from the application version or
Component Release checksum.

The deferred database guard independently proves:

- Component Release ID, checksum, application version, artifact, and provenance
  against the frozen plan and published Product Release;
- config, platform, and topology against the frozen target plan; and
- schema version and capability checksum against authenticated current observed
  state and the resulting active desired revision.

The public request schema and its `distr.baseline-adoption-request/v1` checksum
domain remain unchanged. This preserves exact idempotent replay for retained
adoption history. Responses add the release-owned `applicationVersion` field so
operators can distinguish it from observation-owned `schemaVersion`.

## Consequences

Distr can adopt runtimes whose application SemVer, schema version, capability
checksum, and release checksum are all different without weakening release,
observation, or desired-state evidence. Existing adoption rows remain append-only
and retain their original checksums.

Downgrading migration 169 removes only the derived application-version column
and restores the migration-166 guard. The underlying frozen plan still retains
the application version, so adoption and audit history are not deleted.

## Validation

Focused API, canonical-checksum, repository, read-model, migration-contract,
and migration-lint tests prove independent fact handling and history-preserving
backfill. The final release matrix must certify the ordered migration range from
138 through 169 after migrations 167-168 are integrated.
