# ADR-0034: Webhook Audit Trail

## Status

Accepted

## Context

PR-031 made webhook execution replay-safe by checking stored StepRun timeline events before re-sending an external request. PR-032 added signing-key rotation. PR-033 scoped replay to the active organization, task lease, and authenticated agent. The remaining gap was forensic reconstruction: operators need a deterministic record of what the webhook adapter decided during execution, and replay needs a way to detect mutated stored audit metadata before trusting it.

The existing StepRun event model already stores immutable event outputs and idempotent payload hashes. Adding a new audit table would increase migration and API surface area for a Docker-agent-only hardening step.

## Decision

Webhook success events now include reserved built-in outputs for a deterministic audit export:

- `auditChainRoot`
- `auditEventHash`
- `auditTrail`

The audit trail is an ordered JSON object with audit events for target resolution, each webhook attempt, and completion. Every audit event is hashed from a canonical payload that includes the parent hash, event type, tenant id, lease id, task id, step run id, and event-specific fields such as attempt number, status code, retry reason, DNS summary, signing key version, and key-rotation flag. The event hash is also the event id, and each event points to the previous event hash.

Replay parses and verifies audit outputs when present. Verification checks parent linkage, event hashes, root hash, final hash, and consistency between the final audit event and stored webhook built-in outputs. Setting `STRICT_REPLAY_VERIFY=true` makes replay fail when stored success history is missing audit outputs.

The audit export intentionally excludes raw secret values, full request bodies, full response bodies, signatures, and resolved IP addresses. It records only bounded metadata needed for deterministic replay integrity and forensic sequencing.

## Consequences

Webhook execution history can be checked for audit metadata tampering before replay trusts a stored success.

The change stays schema-free and action-version-compatible by using reserved StepRun outputs that already participate in existing event immutability and redaction paths.

Older stored successes without audit outputs remain replayable by default. Operators that want fail-closed forensic replay can enable `STRICT_REPLAY_VERIFY=true` after agents have produced PR-034 audit outputs.
