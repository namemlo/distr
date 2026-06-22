# PR-034 - Webhook audit trail

PR-034 adds a deterministic, replay-verifiable audit export to Docker-agent `distr.webhook` executions. The audit trail is stored with the webhook success event as reserved non-secret outputs, so replay can verify stored history without a schema migration.

## Scope

Included:

- deterministic webhook audit events for target resolution, each HTTP attempt, and final completion
- chained audit event hashes with root and final hash outputs
- reserved `auditChainRoot`, `auditEventHash`, and `auditTrail` webhook outputs
- strict replay verification through `STRICT_REPLAY_VERIFY=true`
- replay-time audit chain, parent, root, final hash, and success-output consistency checks
- audit export redaction boundary coverage to keep secrets and request bodies out of audit payloads
- action registry schema and validation updates for the new built-in audit outputs

Not included:

- database schema changes
- UI changes
- public timeline export endpoints
- inbound webhook receiver behavior
- action version bump

## Behavior

Successful webhook events now include three non-secret audit outputs:

- `auditChainRoot`: the first audit event hash
- `auditEventHash`: the final audit event hash
- `auditTrail`: a deterministic JSON object containing the ordered audit event chain

Each audit event includes the tenant, lease, task, step run, parent hash, event type, and the fields relevant to that event. The target-resolution event includes a DNS summary without IP addresses. Attempt events include attempt number, status code, and retry reason when the attempt is retried. The completion event includes the final status, attempt count, signing key version, and key-rotation flag.

When stored webhook success history contains audit outputs, replay verifies the chain before trusting the event. With `STRICT_REPLAY_VERIFY=true`, replay also fails closed when stored success history is missing audit outputs. Older PR-031/PR-032 stored successes remain replayable when strict mode is disabled.

## Verification

Focused tests cover:

- deterministic audit chain shape across retrying webhook execution
- audit root/final hash output consistency
- audit export redaction for signing secrets, secret headers, and body values
- strict replay rejection for tampered audit trail payloads before network I/O
- reserved `auditTrail` output-name validation in Docker-agent and action registry paths
