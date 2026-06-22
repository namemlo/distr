# PR-028 - Webhook action

## Scope

PR-028 adds the Docker-agent execution adapter for the typed `distr.webhook` action.

It adds:

- built-in `distr.webhook` action registry metadata and input/output schemas
- Docker agent capability reporting for `distr.webhook` version `1`
- Docker agent task-lease execution for signed outbound HTTPS webhooks
- trusted outbound host policy through `DISTR_WEBHOOK_ALLOWED_HOSTS` and `DISTR_WEBHOOK_ALLOWED_PRIVATE_HOSTS`
- deterministic request signing with timestamp, body digest, signature, and idempotency headers
- bounded JSON request and response bodies
- configured retry policy with stable idempotency key reuse across attempts and lease reclaim
- declared JSON Pointer response outputs with type checks and required/sensitive flags
- lease-time secret resolution for webhook secret headers and signing secret
- Docker-agent and Hub-side StepRun event, output, log, timeline, and returned-error redaction for resolved webhook secrets

## Feature flags

PR-028 does not introduce a new feature flag.

End-to-end typed execution still depends on the existing prerequisite feature flags and hidden endpoints from earlier roadmap PRs:

```text
DISTR_EXPERIMENTAL_FEATURE_FLAGS=agent_capabilities,task_queue,agent_task_leases,step_events
```

The Docker agent remains compatible when optional capability, lease, heartbeat, or step-event endpoints are missing or disabled.

## Database

No database migration is added in PR-028.

PR-028 reuses:

- `AgentCapabilityReport` and `AgentActionCapability`
- `Task`
- `StepRun`
- `TaskLease`
- `StepRunEvent`
- `StepRunLogChunk`
- `StepRunOutput`
- `Secret`

Stored Deployment Plans and Process Snapshots keep `secretHeaders` and `signingSecret` as secret keys only. Secret values are resolved only when an authenticated agent lease is built.

## API

No new HTTP endpoint is added in PR-028.

PR-028 reuses:

```http
POST /api/v1/agents/{id}/capabilities
POST /api/v1/agents/{id}/lease
POST /api/v1/agents/{id}/tasks/{taskId}/heartbeat
POST /api/v1/agents/{id}/step-runs/{stepRunId}/events
```

The action registry now exposes `distr.webhook` with this input shape:

```json
{
  "url": "https://hooks.example.com/deployments",
  "method": "POST",
  "headers": {
    "X-Deployment": "demo"
  },
  "secretHeaders": {
    "Authorization": "webhook_auth_token"
  },
  "body": {
    "deploymentId": "dep-123"
  },
  "sensitiveBody": true,
  "signingSecret": "webhook_signing_key",
  "timeoutSeconds": 30,
  "retry": {
    "maxAttempts": 3,
    "backoffSeconds": 1,
    "retryableStatusCodes": [429, 500, 502, 503, 504]
  },
  "expectedStatusCodes": [200, 202],
  "idempotencyKey": "notify-demo",
  "outputs": [
    {
      "name": "remoteId",
      "pointer": "/id",
      "type": "string",
      "required": true
    }
  ]
}
```

Outbound hosts are trusted Docker-agent configuration, not Deployment Process input:

```text
DISTR_WEBHOOK_ALLOWED_HOSTS=hooks.example.com
DISTR_WEBHOOK_ALLOWED_PRIVATE_HOSTS=127.0.0.1:8443
```

## Agent behavior

The Docker agent now reports these supported actions:

```json
[
  {
    "actionType": "distr.compose.deploy",
    "versions": ["1"]
  },
  {
    "actionType": "distr.oci.job",
    "versions": ["1"]
  },
  {
    "actionType": "distr.file.render",
    "versions": ["1"]
  },
  {
    "actionType": "distr.webhook",
    "versions": ["1"]
  }
]
```

For `distr.webhook`, the agent:

- heartbeats the task lease before and during execution
- emits `STARTED`
- emits `PROGRESS` before each request attempt
- rejects unsupported action versions, missing host policy, non-HTTPS URLs, URL credentials, unallowlisted hosts, unsafe private destinations without explicit private-host policy, plaintext authorization-like public headers, reserved signing/idempotency headers, invalid retry policies, and invalid declared outputs
- signs each request with HMAC-SHA256 over method, path and query, timestamp, idempotency key, and body digest
- sends `Idempotency-Key`, `X-Distr-Timestamp`, `X-Distr-Body-Digest`, and `X-Distr-Signature`
- disables redirects
- retries configured transient network or status failures only
- extracts only declared JSON Pointer outputs from bounded JSON responses
- emits sensitive declared outputs as redacted sensitive outputs
- emits `SUCCEEDED` with non-sensitive `statusCode`, `attempts`, and declared non-sensitive outputs
- emits `FAILED` with redacted errors

## Security notes

- Secrets remain references in Deployment Plans and Process Snapshots.
- Secret values are resolved only for an authenticated lease and are not stored back to the plan.
- StepRun event messages, details, logs, non-sensitive outputs, and returned agent errors are redacted using resolved secret values.
- Request and response bodies are not logged.
- Public headers cannot set authorization-like or reserved signing/idempotency headers.
- Action input cannot grant outbound host access.
- Redirects are disabled to avoid redirect-based SSRF.

## Troubleshooting

- `DISTR_WEBHOOK_ALLOWED_HOSTS is required`: configure at least one trusted outbound host or private host on the Docker agent.
- `url must use https`: use an HTTPS endpoint.
- `url must not include credentials`: move credentials to `secretHeaders`.
- `webhook host is not allowlisted`: add the host to trusted Docker-agent configuration.
- `webhook host resolves to unsafe address`: add the exact host to `DISTR_WEBHOOK_ALLOWED_PRIVATE_HOSTS` only when that private destination is trusted.
- `headers cannot include Authorization`: use `secretHeaders`.
- `required output ... is missing`: update the webhook response or output declaration.

## UI

No Angular route, sidebar entry, or page is added in PR-028.

## Compatibility notes

Existing Docker Compose action execution, OCI job action execution, file render action execution, Kubernetes agent behavior, and legacy Docker resource-poll deployment behavior remain unchanged.

## Non-goals

PR-028 does not add:

- Helm typed actions
- arbitrary host shell execution
- approval workflows
- notification outbox
- subscription hooks
- retry dashboards
- task cancellation UI
- runbooks
- retention
- timeline UI
- new database tables
- new public API routes

Those features remain later roadmap work.

## Verification

Focused verification:

```text
go test ./internal/actionregistry
go test ./cmd/agent/docker
go test ./internal/db -run TestTaskLeaseRepositoryResolvesWebhookSecretsOnlyForAgentLease
go test ./internal/db -run TestStepEventRepositoryRedactsResolvedWebhookSecretsFromEventsLogsAndOutputs
```

The Docker-agent tests cover validation, deterministic signing, success, retry, idempotency header reuse, declared output extraction, secret redaction, task-lease dispatch, and heartbeat integration.

The Docker-agent package requires the existing agent environment variables during package initialization. Local tests used dummy endpoint values for `DISTR_TARGET_ID`, `DISTR_TARGET_SECRET`, `DISTR_LOGIN_ENDPOINT`, `DISTR_MANIFEST_ENDPOINT`, `DISTR_RESOURCE_ENDPOINT`, `DISTR_STATUS_ENDPOINT`, `DISTR_METRICS_ENDPOINT`, `DISTR_LOGS_ENDPOINT`, and `DISTR_AGENT_LOGS_ENDPOINT`.

The live database tests require `DISTR_TEST_DATABASE_URL`.
