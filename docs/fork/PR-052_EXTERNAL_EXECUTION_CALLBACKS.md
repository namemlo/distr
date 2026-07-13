# PR-052 - External Execution Callbacks And Observed State

## Generic User Story

As a deployment operator, I want a Hub-triggered external executor to report durable progress and exact observed
runtime state back to Distr, so that an accepted webhook is not mistaken for a successful deployment and Hub
restarts do not duplicate external work.

## Scope

- Extend `distr.webhook` with response or callback completion mode, component identity, and bounded callback wait.
- Prepare one immutable external execution per step run after execution-time preflight.
- Send signed, reserved runtime headers containing callback and frozen expected-state identity.
- Add authenticated organization-scoped callback and read endpoints.
- Enforce sequenced idempotent callbacks, terminal protection, deadline checks, a 256-event history cap, and bounded
  redacted errors.
- Project successful exact observed state to target-component current state and immutable observation history.
- Commit the external dispatch claim before HTTP delivery and resume running or terminal executions after Hub restart
  without retriggering the webhook.
- Require callback-mode provider/build identity and deployment results to arrive through the durable callback;
  synchronous declared response outputs remain response-mode only.

## Required Impact Report

### Database/schema impact

Migration 136 adds `ExternalExecution` and `ExternalExecutionEvent`, `config_reference` on target-component state and
observations, and an organization-scoped Release Bundle unique key. Expected state is frozen on execution creation;
callback events retain sequence and payload hash; the final event slot is reserved for a terminal state. Successful
projection uses optimistic state version/checksum matching and the task's active target-component lock. Lock release
and projection acquire the same transaction-scoped advisory lock.

### Public API impact

- `GET /api/v1/external-executions/{externalExecutionId}` returns immutable expected state, current execution state,
  observed state, and callback history.
- `POST /api/v1/external-executions/{externalExecutionId}/callbacks` records a sequenced authenticated callback.
- Callback mode adds reserved outbound `X-Distr-*` execution headers.

The endpoints require normal Distr authentication, vendor organization scope, and role middleware. Callback writes
require read-write or admin role and reject super-admin cross-organization use.

### Frontend/UI impact

No new page is added in this slice. External callback progress updates the existing task event timeline and terminal
state updates the Deployment Timeline. The external execution read endpoint supports the operator detail UI in the
next slice.

### Agent/protocol impact

None. External execution is a Hub-only action mode. Docker and Kubernetes agent payloads are unchanged.

### Feature-flag impact

No new flag. The route and worker are reachable only through the existing deployment plan, task queue, step event,
and Hub webhook feature surfaces.

### Security impact

Positive. Runtime callback headers and observed-state output names are reserved only where callback mode needs them;
response-mode webhook output capacity remains backward compatible. Trigger requests retain HTTPS, destination
allowlist, signing, idempotency, body bounds, and redaction controls. Callbacks are organization-bound,
authenticated, replay protected, reject credential-bearing provider URLs, and do not leak unexpected internal errors.

### Backward-compatibility impact

Webhook steps default to synchronous `response` completion. Existing processes and agent actions keep their current
behavior. Existing target-component rows receive an empty configuration reference until a verified external
observation updates them. Response-mode declared output capacity is unchanged; callback mode exposes only durable
external-execution outputs.

## Callback Example

```http
POST /api/v1/external-executions/{id}/callbacks
Authorization: AccessToken distr-REDACTED
Content-Type: application/json

{
  "sequence": 2,
  "status": "SUCCEEDED",
  "providerReference": "external-build-42",
  "providerUrl": "https://executor.example/jobs/42",
  "message": "service is healthy",
  "observedState": {
    "version": "1.4.2",
    "image": "registry.example/service@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    "platform": "linux/amd64",
    "contracts": ["service.v1"],
    "configReference": "s3://config-bucket/service.json?versionId=v42",
    "configChecksum": "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
    "health": "HEALTHY"
  }
}
```

The credential shown above is a placeholder. Store real credentials only in the configured credential store.

## Validation

- `go test ./api ./internal/actionregistry ./internal/webhookaction ./internal/externalexecution`
- `go test ./internal/hubexecutor ./internal/mapping ./internal/handlers ./internal/db`
- Full migration chain plus migration 136 down/up on PostgreSQL 18
- Live PostgreSQL external execution repository tests with `DISTR_TEST_DATABASE_URL`
- Full Go suite, format/lint, community Hub/frontend builds, diff check, and secret scan before deployment
