# ADR-0066: Correlated Control-Plane Audit and External Export

- Status: Accepted
- Date: 2026-07-18
- Owners: Distr control-plane maintainers

## Context

Enterprise deployment evidence crosses immutable releases, configuration, plans,
approvals, campaigns, executions, adapters, independent observations, drift, and
reconciliation. Domain-local audit rows cannot produce one tenant-safe,
checksum-verifiable deployment record, and an unavailable external sink must
never delete or block the primary evidence.

## Decision

Store one append-only, organization-scoped `ControlPlaneAuditEvent` sequence.
Every privileged v2 mutation appends through an explicit hook inside its database
transaction or transactional outbox boundary. The direct hook reuses the current
database transaction; an outbox implementation can satisfy the same interface.
Events contain bounded, redacted metadata and typed identifiers/checksums; they
never contain credentials. Correlation subjects are tenant-owned and immutable,
so the same typed identity cannot be claimed by another organization.
Campaign evidence names each lifecycle identity explicitly: draft, revision,
run, wave definition (`campaignWaveDefinitionId` / `campaign_wave_definition_id`),
wave run, member definition, member run, control request,
exclusion, prerequisite evaluation, and threshold evaluation. The contract does
not expose ambiguous `campaignId`, `waveId`, `campaignWaveId`,
`campaign_wave_id`, or `campaignChecksum` aliases.

Evidence bundles are built from the connected typed-correlation graph rooted at
the requested deployment plan, deterministically ordered by sequence, and
checksummed with SHA-256. The `distr.control-plane-evidence/v1` schema identifier
is part of the canonical checksum input, so a future schema must use a new
version rather than silently changing existing evidence. External sinks consume ordered batches through per-sink
checkpoints and idempotency keys. Every delivery starts a new immutable attempt
row in `RUNNING`; resolver or delivery failure completes that row as `FAILED`,
while a retry creates a new row. Failure retains the primary event and leaves the
checkpoint unchanged. Running attempts carry a durable lease. Starting the next
batch atomically marks an expired attempt failed before creating its replacement,
so a terminated worker cannot permanently wedge the sink. Cancellation and
checkpoint-commit failures persist through a short context detached from the
cancelled export request, and persisted error summaries remain valid UTF-8.

PR-078 instruments its owned export-sink creation transaction and establishes
the append/bundle/export core plus direct-transaction and outbox integration
hooks. The final ordered integration replay must use those hooks from the owning
PR-066 through PR-077 mutations after those repositories have stabilized.

`AuditView` authorizes event, bundle, sink, and status reads. `AuditExport`
authorizes sink configuration. Export transports are resolved through an
injected sink adapter; the core does not dereference endpoint references or make
outbound requests on its own. Production must explicitly enable the operator
control plane and register both a generic sink-kind factory and a secret-reference
resolver. Resolved versioned configuration must match the persisted canonical
configuration checksum. Missing or mismatched wiring fails closed and leaves the
checkpoint unchanged while preserving failed-attempt and lag evidence.

## Consequences

- Audit events cannot be updated, deleted, or truncated.
- Correlation-subject ownership and event links cannot be updated, deleted, or
  truncated.
- Cross-organization evidence is rejected before bundle generation.
- A bundle follows connected typed correlations and does not require every event
  to carry the deployment-plan ID directly.
- Sink failure is observable and retryable without changing primary evidence.
- Export targets store secret references only; secret values remain in the
  configured secret provider.
- Migration 160 takes `ACCESS EXCLUSIVE` locks on every owned audit/export table
  before checking for retained rows, then refuses rollback if any primary,
  correlation, sink, checkpoint, or attempt evidence exists. The locks remain
  held through the destructive statements, closing the check/drop race.

## Alternatives considered

- Domain-local audit tables were rejected because they cannot provide one
  organization-scoped ordering or deployment checksum.
- Advancing checkpoints per event was rejected because a partially delivered
  batch would appear complete.
- Storing resolved credentials or constructing arbitrary outbound clients in
  the core was rejected because it would widen the secret and SSRF boundaries.

## Validation

Focused tests cover typed correlation, deterministic bundle checksums,
cross-organization rejection, graph connectivity, JSON-number preservation,
payload redaction and bounds, safe sink references, ordered export, immutable
retry history, lag/failure visibility, transactional hooks, owned sink
instrumentation, source-event retention, and rollback lock/refusal coverage.
Worker cancellation, commit failure, stale-attempt recovery, and multibyte error
truncation also have focused coverage. Race execution is environment
blocked on this Windows host because CGO and a C compiler are unavailable.
Migration lint is expected to remain blocked on the synthetic branch's missing
ordered migrations 140-142 and 146-159; migration 160 is not renumbered or
backfilled around that gap.
