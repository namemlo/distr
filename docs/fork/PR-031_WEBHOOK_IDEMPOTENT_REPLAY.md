# PR-031 - Webhook idempotent replay

PR-031 makes Docker-agent `distr.webhook` execution replay-aware. It keeps the same webhook action schema while adding a hidden agent timeline read so the agent can treat the Hub event store as the source of truth before attempting external HTTP work.

## Scope

Included:

- hidden agent-authenticated task timeline read endpoint
- generated Docker and Kubernetes agent manifest wiring for `DISTR_TASK_TIMELINE_ENDPOINT_TEMPLATE`
- optional agent client timeline helper
- webhook replay preflight before STARTED/progress events, DNS resolution, signing, or HTTP transport setup
- deterministic reconstruction of stored webhook success outputs
- fail-closed behavior for incomplete stored webhook attempts
- contract tests for duplicate execution, replay without network side effects, interrupted replay, and output reconstruction

Not included:

- UI changes
- database schema changes
- webhook action input/output schema changes
- generic replay support for Compose, OCI job, or file render actions
- a new durable local agent state store

## Behavior

Before a webhook step records a new STARTED event, it asks the Hub for the task timeline when `DISTR_TASK_TIMELINE_ENDPOINT_TEMPLATE` is configured.

If a prior SUCCEEDED event exists for the same step run, the agent reconstructs the stored webhook outputs and returns without emitting new events or performing DNS, signing, transport setup, or HTTP requests.

If prior non-terminal webhook events exist for the same step run without a SUCCEEDED event, the agent fails closed with `webhook replay is incomplete; refusing to re-execute external request`. This avoids duplicating an external side effect after an interrupted attempt whose remote outcome cannot be proven locally.

If no timeline endpoint is configured, the agent client returns no timeline for backwards compatibility. New generated manifests include the endpoint, so refreshed agents get replay protection automatically.

## Verification

Focused tests cover:

- agent timeline endpoint template construction and authenticated GET behavior
- generated manifest endpoint data
- duplicate webhook execution sending only one HTTP request
- replay mode performing zero DNS lookups and zero HTTP requests
- incomplete replay failing closed before duplicate HTTP
- reconstruction of stored webhook outputs
