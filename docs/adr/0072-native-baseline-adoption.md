# ADR-0072: Native Baseline Adoption

## Status

Accepted

## Context

An existing runtime can already match an immutable Product Release and target
configuration before Distr has native desired-state history for that placement.
Creating tasks or execution attempts for that runtime would falsely claim a
deployment occurred. Leaving the placement without native lineage prevents
authoritative planning and previous-state selection.

Adoption must therefore prove the exact frozen component set from independent,
current observations while remaining distinct from deployment execution. Some
legacy runtimes expose only bounded liveness evidence rather than the standard
readiness contract. That limitation must remain visible and must not be reused
as evidence of a later deployment promotion.

## Decision

Add a first-class, append-only `BaselineAdoption` outcome and per-component
`BaselineAdoptionComponent` evidence in migration 166. The API is:

- `POST /api/v1/deployment-plans/{id}/baseline-adoptions`

The operation requires a sealed `READY` bootstrap plan using the native v2
schema and protocol, exact plan/Product Release/target-config checksums, a
published component release and provenance verification for every frozen pin,
and fresh current independent observations that exactly match artifact digest,
config checksum, schema, capability, platform, topology, component placement,
and observer evidence. The authenticated observation, rather than the adoption
caller, owns immutable health-evidence kind/use/policy fields. Policy-bearing
observations require an `evidence://sha256/<digest>` reference exactly bound to
their evidence checksum.

Application version, observed schema version, capability-set checksum, and
Component Release checksum are separate facts. The release pin owns the
application version and release checksum; the authenticated observation owns
schema version and capability checksum. Migration 169 exposes the frozen
application version explicitly and removes any equality assumption between
these identities while preserving existing adoption request checksums.

Success appends an `ADOPTED` outcome and `baseline_adoption.adopted` audit event,
creates one active desired revision per component, and moves the plan to its
successful terminal lifecycle state. It sets `deploymentPerformed=false`,
`taskCount=0`, `lockCount=0`, and `executionCount=0`. It creates no pending desired revision,
task, task resource lock, execution attempt, external execution, or executor
report.

`ActiveDesiredRevision` records whether its source is `EXECUTION` or
`BASELINE_ADOPTION`. The two lineage shapes are mutually exclusive and enforced
by database constraints. A deferred commit guard verifies the exact plan,
release, configuration, component pins, provenance, desired/observed state,
current observation heads, observer-retained health evidence, digest-addressed
evidence identity, and correlated audit event.

Health evidence is classified as either `STANDARD_READINESS` with
`STANDARD_PROMOTION_ELIGIBLE` use or `LEGACY_LIVENESS_ONLY` with
`BASELINE_OR_ROLLBACK_ONLY` use. Legacy evidence must point to an immutable,
checksum-bound observer artifact containing portable logical probe paths,
statuses, sizes, and body checksums. Transient transport addresses are not
portable evidence identity. Database constraints prevent legacy liveness from
being written as execution-sourced desired-state promotion, and native provider
discovery excludes it from `pinned_existing` or shared-provider promotion.

The route requires scoped `plan.execute` authorization for the deployment unit,
organization/environment enrollment, the existing operator and executor feature
gates, and a non-super-admin organization actor. A canonical request checksum
provides idempotency: exact replay, including a request racing the original
serializable write, re-reads and returns the retained outcome, while changed
material under the same key conflicts.

ADR-0088 additionally requires a current eligible approval, an exact current
`ADMIT` evaluation, and an exact persistent current `GO` decision before the
adoption transaction may mutate desired state or mark the plan executed. These
governance identities are part of the v2 adoption request checksum and the
correlated immutable audit evidence.

## Consequences

Healthy existing runtime can become authoritative native baseline state without
inventing deployment history. Planning, drift, and previous-state reads continue
to consume `ActiveDesiredRevision` without an alternate projection.

Adoption is initial lineage only. Existing pending or active desired history,
tasks, or external executions block it, including task or execution reassignment
after adoption. Evidence must still be current and fresh at commit. Legacy liveness remains an explicit operational limitation and does
not relax standard observation requirements for later deployment execution.

Migration 166 is additive. Its down migration refuses while adoption evidence,
adoption-sourced active desired revisions, or retained observation health-policy
evidence exists.

Migration 169 is a compatible projection and guard correction. It backfills the
application version only from the retained frozen plan pin and does not rewrite
existing observation, desired-state, adoption, or audit checksum material.

## Alternatives Considered

- Creating an already-succeeded task was rejected because it fabricates an
  execution and lock history that never occurred.
- Importing mutable target state without an immutable outcome was rejected
  because it is not auditable or idempotent.
- Treating legacy liveness as standard readiness was rejected because it hides
  a material health limitation and could authorize an unsafe later promotion.

## Validation

Focused API, desired-state, repository, handler, migration-contract, planning,
observation, reconciliation, and migration-matrix tests cover exact evidence,
idempotency, current-state CAS, health-policy restriction, zero synthetic
execution mutation, immutable lineage, and downgrade refusal. Live PostgreSQL
behavior remains a release-matrix gate when `DISTR_TEST_DATABASE_URL` is
available.
