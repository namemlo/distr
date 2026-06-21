# PR-026 - OCI one-shot job action

## Scope

PR-026 adds the Docker-agent execution adapter for the typed `distr.oci.job` action.

It adds:

- built-in `distr.oci.job` action registry metadata and input/output schemas
- Docker agent capability reporting for `distr.oci.job` version `1`
- Docker agent task-lease execution for one-shot OCI jobs
- digest-only image validation and trusted Docker-agent registry allowlisting
- network, volume, privilege, root filesystem, capability, user, CPU, and memory policy enforcement
- lease-time secret resolution for OCI job environment variables
- StepRun event, log, output, and returned-error redaction for resolved OCI secrets
- deterministic container naming from the action idempotency key so retries, lease reclaim, and agent restart do not execute the same job twice
- troubleshooting and security guidance for operators

## Feature flags

PR-026 does not introduce a new feature flag.

End-to-end typed execution still depends on the existing prerequisite feature flags and hidden endpoints from earlier roadmap PRs:

```text
DISTR_EXPERIMENTAL_FEATURE_FLAGS=agent_capabilities,task_queue,agent_task_leases,step_events
```

The Docker agent remains compatible when optional capability, lease, heartbeat, or step-event endpoints are missing or disabled.

## Database

No database migration is added in PR-026.

PR-026 reuses:

- `AgentCapabilityReport` and `AgentActionCapability`
- `Task`
- `StepRun`
- `TaskLease`
- `StepRunEvent`
- `StepRunLogChunk`
- `StepRunOutput`
- `Secret`

Stored Deployment Plans and Process Snapshots keep `secretEnvironment` as secret keys only. Secret values are resolved only when an authenticated agent lease is built, then removed from `secretEnvironment` and injected into the lease-only `environment` map.

## API

No new HTTP endpoint is added in PR-026.

PR-026 reuses:

```http
POST /api/v1/agents/{id}/capabilities
POST /api/v1/agents/{id}/lease
POST /api/v1/agents/{id}/tasks/{taskId}/heartbeat
POST /api/v1/agents/{id}/step-runs/{stepRunId}/events
```

The action registry now exposes `distr.oci.job` with this input shape:

```json
{
  "imageDigest": "registry.example.com/jobs/cleanup@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "command": ["/bin/cleanup"],
  "arguments": ["--tenant", "demo"],
  "environment": {
    "MODE": "once"
  },
  "secretEnvironment": {
    "API_TOKEN": "job_api_token"
  },
  "network": "none",
  "volumes": [
    {
      "source": "/var/lib/distr/jobs/input",
      "target": "/input",
      "readOnly": true
    }
  ],
  "timeoutSeconds": 60,
  "expectedExitCodes": [0],
  "idempotencyKey": "sha256:job-key",
  "runAsUser": "1000:1000",
  "resources": {
    "cpus": 0.5,
    "memoryBytes": 134217728
  },
  "security": {
    "readOnlyRootFilesystem": true
  }
}
```

`imageDigest` must be an immutable `@sha256:<64 hex chars>` reference with an explicit registry. Mutable tags are rejected.

Registry, network, and mount-root allowlists are trusted Docker-agent configuration, not Deployment Process input:

```text
DISTR_OCI_JOB_ALLOWED_REGISTRIES=registry.example.com
DISTR_OCI_JOB_ALLOWED_NETWORKS=none,job-network
DISTR_OCI_JOB_ALLOWED_MOUNT_ROOTS=/var/lib/distr/jobs
```

When `DISTR_OCI_JOB_ALLOWED_NETWORKS` is unset, only `none` is allowed. Host mounts require absolute source paths that resolve through symlinks under one of `DISTR_OCI_JOB_ALLOWED_MOUNT_ROOTS`.

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
  }
]
```

For `distr.oci.job`, the agent:

- heartbeats the task lease before and during execution
- emits `STARTED`
- emits `PROGRESS` before inspecting or starting the container
- rejects unsupported action versions, mutable image tags, non-allowlisted registries, non-allowlisted networks, writable or non-allowlisted host mounts, privileged mode, disabled no-new-privileges, and disabled read-only root filesystem
- writes declared environment variables to a temporary env file instead of putting secret values in Docker command-line arguments
- runs `docker run` with a deterministic container name, `--read-only`, `--security-opt no-new-privileges`, `--cap-drop ALL`, the selected allowlisted network, optional read-only allowlisted volumes, optional user, and optional CPU/memory limits
- reuses an existing deterministic container on retry, lease reclaim, or agent restart instead of running the operation again
- stops the container on timeout or cancellation
- emits `SUCCEEDED` with non-sensitive `containerName`, `exitCode`, and `status` outputs when the exit code is expected
- emits `FAILED` with redacted error and stderr-style details when validation or execution fails

## Security notes

- Secrets remain references in Deployment Plans and Process Snapshots.
- Secret values are resolved only for an authenticated lease and are not stored back to the plan.
- Resolved secret values are removed from `secretEnvironment` before the lease is returned.
- StepRun event messages, details, logs, non-sensitive outputs, and returned agent errors are redacted using resolved secret values.
- Docker command-line arguments do not include resolved secret values; the agent passes environment through a temporary env file and removes it after command completion.
- OCI jobs do not use privileged mode.
- Root filesystems are read-only by default and cannot be disabled by this adapter.
- Linux capabilities are dropped with `--cap-drop ALL`.
- Networks must be explicitly allowlisted; the default network is `none`.
- Host mounts must be read-only and under an allowlisted source root.

## Troubleshooting

- `imageDigest must be an immutable sha256 digest reference`: use `registry/name@sha256:<digest>`, not a mutable tag like `:latest`.
- `image registry is not allowlisted`: add the exact registry host to `DISTR_OCI_JOB_ALLOWED_REGISTRIES` on the Docker agent.
- `network is not allowlisted`: set `network` to `none` or add the selected Docker network to `DISTR_OCI_JOB_ALLOWED_NETWORKS` on the Docker agent.
- `volume source is not under an allowlisted mount root`: use an absolute source path that resolves under one of `DISTR_OCI_JOB_ALLOWED_MOUNT_ROOTS` on the Docker agent.
- `volume source must be an absolute path`: use an absolute host path; relative host mounts are rejected.
- `volumes must be read-only`: set each volume `readOnly` to `true`.
- `secretEnvironment must be resolved by the task lease`: the agent received unresolved secret references; check the server-side lease secret resolver and that the secret exists for the target scope.
- `OCI job timed out`: increase `timeoutSeconds` or inspect the deterministic container logs on the Docker host.
- Repeated retries do not re-run the job when the deterministic container already exists; inspect or remove the `distr-job-*` container only after confirming it is safe to allow re-execution.

## UI

No Angular route, sidebar entry, or page is added in PR-026.

## Compatibility notes

Existing Docker Compose action execution and legacy Docker resource-poll deployment behavior remain unchanged.

Existing Kubernetes agent behavior is unchanged in PR-026.

## Non-goals

PR-026 does not add:

- file render actions
- webhook actions
- Helm typed actions
- approvals
- guided failure
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
go test ./internal/db -run "Test(TaskLeaseRepositoryResolvesOCIJobSecretEnvironmentOnlyForAgentLease|StepEventRepositoryRedactsResolvedOCIJobSecretFromEventsLogsAndOutputs)"
```

The Docker-agent tests cover policy rejection, success, failure, expected non-zero exit codes, timeout stop, cancellation stop, redaction, deterministic-container reclaim/restart reuse, and task-lease dispatch with fake Docker commands.

The Docker-agent package requires the existing agent environment variables during package initialization. Local tests used dummy endpoint values for `DISTR_TARGET_ID`, `DISTR_TARGET_SECRET`, `DISTR_LOGIN_ENDPOINT`, `DISTR_MANIFEST_ENDPOINT`, `DISTR_RESOURCE_ENDPOINT`, `DISTR_STATUS_ENDPOINT`, `DISTR_METRICS_ENDPOINT`, `DISTR_LOGS_ENDPOINT`, and `DISTR_AGENT_LOGS_ENDPOINT`.

The live database tests require `DISTR_TEST_DATABASE_URL`.
