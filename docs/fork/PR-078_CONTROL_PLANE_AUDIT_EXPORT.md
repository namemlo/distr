# PR-078: Correlated Control-Plane Audit and External Export

## Scope

PR-078 adds the client-neutral evidence layer for the v2 control plane:

- migration 160 with append-only audit events and durable export state;
- one transaction-safe append helper with bounded payload redaction;
- deterministic, tenant-scoped deployment evidence bundles;
- ordered, idempotent export batches with failure and lag evidence;
- API seams for paginated events, bundles, sinks, and export status.

The HTTP surface is:

```text
GET  /api/v1/control-plane-audit/events
POST /api/v1/control-plane-audit/evidence-bundles
GET  /api/v1/control-plane-audit/export-sinks
POST /api/v1/control-plane-audit/export-sinks
GET  /api/v1/control-plane-audit/export-status
```

Reads require `AuditView`; sink creation requires `AuditExport`. All routes are
vendor, authenticated-organization, and `operator_control_plane_v2` scoped.
Transport adapters are injected into the export worker and must treat the stable
event ID as the delivery idempotency key.

## Integrity rules

Events use a monotonically increasing per-organization sequence. A bundle accepts
only events from the requested organization and deployment plan, sorts them by
sequence, and hashes the canonical JSON document. Export checkpoints advance
only after the complete ordered batch succeeds. Sink failure records an attempt,
does not advance the checkpoint, and never removes the source event.

Payloads are valid JSON, redacted for common credential fields and bearer/token
patterns, and limited to 32 KiB. Export configuration persists only a secret or
endpoint reference and a checksum.

Example sink request:

```json
{
  "name": "Security archive",
  "kind": "siem",
  "endpointReference": "secret://audit/security-archive",
  "configChecksum": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
}
```

## Integration boundary

This synthetic implementation branch owns only the audit/export core. During the
numbered integration replay, the append helper must be wired into plan
draft/publication, policy, approval, calendar/freeze, admission/override,
campaign/control, adapter assignment/resolution, execution/control,
desired/observed state, drift, and reconciliation mutations in the same
transaction or outbox boundary.

The core does not ship a webhook, object-store, or SIEM network client. A
deployment supplies an allowlisted sink adapter that resolves the stored
reference through its approved secret provider. This keeps endpoint policy,
credentials, DNS/IP controls, timeout, and retry ownership outside the generic
database layer.

## Verification

Focused tests cover deterministic correlation, cross-organization refusal,
redaction and payload bounds, ordered idempotent export, checkpoint behavior,
sink failure visibility, and primary-event retention. The complete migration
lint and live PostgreSQL gate remain deferred until migrations 140–159 have been
integrated ahead of migration 160.
