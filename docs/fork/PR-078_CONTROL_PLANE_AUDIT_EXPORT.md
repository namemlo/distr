# PR-078: Correlated Control-Plane Audit and External Export

## Scope

PR-078 adds the client-neutral evidence layer for the v2 control plane:

- migration 160 with append-only audit events, typed correlation ownership, and
  durable export state; downgrade locks all owned tables before refusing any
  retained primary, correlation, sink, checkpoint, or attempt evidence;
- direct-transaction and transactional-outbox append hooks with bounded payload
  redaction;
- deterministic, tenant-scoped deployment evidence bundles built from the
  connected correlation graph, with the versioned canonical schema identifier
  included in the checksum input;
- ordered, idempotent export batches with immutable retry and lag evidence;
- leased attempts with atomic stale-attempt recovery and background-safe failure
  persistence after cancellation or checkpoint-commit failure;
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

Events use a monotonically increasing per-organization sequence. Each typed
correlation identity is claimed by exactly one organization. A bundle roots at
the requested deployment plan, follows connected typed identities, refuses
disconnected or foreign-plan input, sorts by sequence, and hashes the canonical
JSON document. Export checkpoints advance only after the complete ordered batch
succeeds. Each delivery creates a distinct `RUNNING` attempt; success or failure
completes that row without overwriting earlier retry history. Resolver and sink
failures do not advance the checkpoint or remove source events. Attempts expire
after a bounded durable lease; the next start atomically fails a stale attempt
before retrying. Failure recording uses a short cancellation-detached context,
and error summaries are bounded without splitting UTF-8 code points.

Payloads are valid single JSON documents, preserve JSON numbers during
redaction, redact credential keys, authentication headers, cookies, private
keys, credential URLs, and token patterns, and are limited to 32 KiB. Export
configuration persists only a validated `secret:` reference and a checksum.

Campaign audit events expose separate typed correlations for campaign drafts,
published revisions, runs, wave definitions (`campaignWaveDefinitionId` in the
API and `campaign_wave_definition_id` in migration 160), wave runs, member
definitions, member runs, control requests, exclusions, prerequisite
evaluations, and threshold evaluations. They carry revision and control-request
checksums where applicable. Ambiguous legacy campaign, wave, `campaignWaveId`,
`campaign_wave_id`, and campaign-checksum fields are not part of the API or
persisted audit schema.

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

This synthetic implementation branch owns the audit/export core and sink
creation, which is audited in the same database transaction. It exposes
`RunControlPlaneAuditedMutation` for a direct transaction and
`ControlPlaneAuditAppendHook` for a transactional outbox adapter. During the
numbered integration replay, one of those hooks must be wired into plan
draft/publication, policy, approval, calendar/freeze, admission/override,
campaign/control, adapter assignment/resolution, execution/control,
desired/observed state, drift, and reconciliation mutations in the same
transaction or outbox boundary.

The core does not ship a webhook, object-store, or SIEM network client. A
deployment supplies an allowlisted sink adapter that resolves the stored
reference through its approved secret provider. This keeps endpoint policy,
credentials, DNS/IP controls, timeout, and retry ownership outside the generic
database layer.

Production export requires `operator_control_plane_v2`, a registered generic
factory for the configured sink kind, and a secret-reference resolver. The
resolver must return versioned configuration whose canonical checksum matches
the sink's persisted `configChecksum`; raw secret environment values and
adopter-specific providers are not part of the core contract. Without this
wiring the worker fails closed: enabled lagging sinks retain failed attempts and
lag alerts, and their checkpoints do not advance.

## Verification

Focused tests cover the full typed correlation shape, tenant-safe correlation
contracts, graph bundles, comprehensive redaction, JSON-number preservation,
safe sink references, transactional/outbox hooks, owned sink instrumentation,
ordered export, resolver failures, immutable retry history, checkpoint behavior,
primary-event retention, and downgrade TOCTOU protection. The complete migration
lint and live PostgreSQL gate remain deferred until migrations 140–159 have been
integrated ahead of migration 160.
