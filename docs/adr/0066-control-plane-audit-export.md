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
Every privileged v2 mutation appends through the same helper inside its database
transaction or transactional outbox boundary. Events contain bounded, redacted
metadata and correlated identifiers/checksums; they never contain credentials.

Evidence bundles are deterministically ordered by sequence and checksummed with
SHA-256. External sinks consume ordered batches through per-sink checkpoints and
idempotency keys. A failed export records an attempt and lag while retaining the
primary event and leaving the checkpoint unchanged.

The first PR-078 commit establishes migration 160, the append/bundle/export core,
and API seams. The final integration replay instruments the owning PR-066 through
PR-077 mutations after those repositories have stabilized.

`AuditView` authorizes event, bundle, sink, and status reads. `AuditExport`
authorizes sink configuration. Export transports are resolved through an
injected sink adapter; the core does not dereference endpoint references or make
outbound requests on its own.

## Consequences

- Audit events cannot be updated, deleted, or truncated.
- Cross-organization evidence is rejected before bundle generation.
- Sink failure is observable and retryable without changing primary evidence.
- Export targets store secret references only; secret values remain in the
  configured secret provider.
- Migration 160 refuses rollback while audit evidence or export sinks exist.

## Alternatives considered

- Domain-local audit tables were rejected because they cannot provide one
  organization-scoped ordering or deployment checksum.
- Advancing checkpoints per event was rejected because a partially delivered
  batch would appear complete.
- Storing resolved credentials or constructing arbitrary outbound clients in
  the core was rejected because it would widen the secret and SSRF boundaries.

## Validation

Focused race tests cover correlation, deterministic bundle checksums,
cross-organization rejection, payload redaction and bounds, ordered export,
idempotent checkpoints, lag/failure visibility, and source-event retention.
Migration lint is expected to remain blocked on the synthetic branch's missing
ordered migrations 140-142 and 146-159; migration 160 is not renumbered or
backfilled around that gap.
