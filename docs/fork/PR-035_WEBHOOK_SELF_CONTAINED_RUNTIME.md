# PR-035 - Webhook self-contained runtime

PR-035 adds an optional self-contained runtime mode for Docker-agent `distr.webhook` execution. The mode removes remote timeline reads and live DNS lookup from the webhook execution and replay path, while preserving the final outbound webhook HTTP request itself.

## Scope

Included:

- `WEBHOOK_SELF_CONTAINED_MODE=true` runtime switch
- cached-only hostname resolution through `DISTR_WEBHOOK_RESOLVED_IP_CACHE`
- fail-closed behavior when self-contained mode is enabled and a non-IP webhook host has no cached resolution
- local in-process task timeline reconstruction for replay checks during a task lease execution
- replay preference for local timeline data in self-contained mode, without calling `DISTR_TASK_TIMELINE_ENDPOINT_TEMPLATE`
- focused self-contained runtime tests covering DNS suppression, missing-cache failure, local replay, and duplicate-step replay

Not included:

- Docker-agent local database introduction
- database schema changes
- action registry schema changes
- UI changes
- public timeline export endpoints
- removal of the final configured outbound webhook HTTP request
- action version bump

## Behavior

Self-contained mode is disabled by default. Existing agents continue to use the PR-031 hidden task timeline endpoint for replay preflight and normal DNS resolution for trusted webhook hosts.

When `WEBHOOK_SELF_CONTAINED_MODE=true`, webhook replay reads only from a local timeline provider. `executeTaskLease` wraps the existing leased-task client with an in-process event mirror, so duplicate webhook steps in the same lease execution can be replayed from locally recorded success events without a remote timeline API call.

When self-contained mode is enabled, non-IP webhook hosts must be present in `DISTR_WEBHOOK_RESOLVED_IP_CACHE`. The cache format is:

```text
host=203.0.113.10
host:443=203.0.113.10|203.0.113.11
```

Entries may be separated by commas, semicolons, or new lines. Multiple IPs for one host are separated by `|`. Cached IPs still pass through the existing unsafe-IP policy, so private or loopback addresses require `DISTR_WEBHOOK_ALLOWED_PRIVATE_HOSTS` just like live DNS results.

Literal IP webhook hosts remain supported without a cache entry and still use the same unsafe-IP checks.

## Verification

Focused tests cover:

- self-contained execution uses cached resolution without calling DNS
- self-contained execution fails closed when a host cache entry is missing
- replay uses local timeline data without calling the remote timeline API
- `executeTaskLease` suppresses a duplicate webhook step from the local event mirror
- existing idempotent replay, audit trail, webhook input validation, and task-lease heartbeat tests remain green

