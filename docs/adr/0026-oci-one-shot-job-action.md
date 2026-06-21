# ADR-0026 - OCI one-shot job action

## Status

Accepted

## Context

PR-020 through PR-025 added durable Tasks, StepRuns, locks, agent capabilities, task leases, heartbeats, structured step events, and the first Docker-agent execution adapter for Compose. PR-026 needs a generic one-shot container action that can run migrations, importers, validation utilities, and maintenance jobs without hard-coding those workflows into core.

The action runs on customer-controlled Docker targets, so it must keep the security boundary tight. It must not introduce mutable image execution, arbitrary host shell execution, privileged containers, unrestricted networks, unrestricted host mounts, or plaintext secret leakage through stored plans, lease-at-rest data, logs, events, outputs, returned errors, or command-line process snapshots.

The task protocol can retry after lease expiry, reclaim work after an agent restart, and reissue the same step. OCI jobs may have external side effects, so the adapter needs idempotency before it is safe to retry.

## Decision

Add a built-in action registry entry for:

```text
distr.oci.job
```

The action accepts:

- `imageDigest`
- `command`
- `arguments`
- `environment`
- `secretEnvironment`
- `network`
- `volumes`
- `timeoutSeconds`
- `expectedExitCodes`
- `idempotencyKey`
- `runAsUser`
- `resources`
- `security`

The Docker agent advertises `distr.oci.job` version `1` through the existing capability report protocol and executes it through the existing task lease, heartbeat, StepRun event, log, output, and retry infrastructure.

Registry, network, and host mount-root allowlists are trusted Docker-agent configuration:

```text
DISTR_OCI_JOB_ALLOWED_REGISTRIES
DISTR_OCI_JOB_ALLOWED_NETWORKS
DISTR_OCI_JOB_ALLOWED_MOUNT_ROOTS
```

They are intentionally not accepted from Deployment Process action input, because action input must not grant its own host permissions.

The server resolves `secretEnvironment` only while building an authenticated agent lease. Stored Deployment Plans and Process Snapshots keep secret keys only. The lease payload removes `secretEnvironment`, injects resolved values into `environment`, and records `SecretReferences`. StepRun event ingestion resolves the same secret references from stored plan input and redacts matching values from event messages, details, logs, and non-sensitive outputs.

The Docker adapter enforces these policies:

- `imageDigest` must be an immutable `@sha256` image reference with an explicit registry allowlisted by the Docker agent.
- Mutable tags are rejected.
- The default network is `none`; any selected network must be present in the Docker agent's trusted network allowlist.
- Volumes must be read-only, absolute host paths, and under a symlink-resolved trusted host source root.
- Privileged containers are rejected.
- The root filesystem is read-only and cannot be disabled by the adapter.
- `--security-opt no-new-privileges` is always used.
- `--cap-drop ALL` is always used.
- Optional `runAsUser`, CPU, and memory limits are passed through to Docker.

The adapter writes declared environment variables to a temporary env file and passes `--env-file` to Docker so resolved secret values are not visible in Docker command-line arguments. The temp file is removed after the command exits.

Idempotency uses a deterministic Docker container name derived from the action idempotency key. Before running `docker run`, the adapter inspects the deterministic container. If it already exists, the adapter reads its existing state/logs and does not run the operation again. This covers normal retry, lease expiry reclaim, and agent restart on the same Docker host.

The action emits:

- `STARTED` before validation/execution
- `PROGRESS` before inspect/run
- `SUCCEEDED` with `containerName`, `exitCode`, and `status`
- `FAILED` with redacted error and stderr-style status details

## Consequences

- Generic one-shot OCI work can run through the typed task protocol.
- Docker agents can advertise both `distr.compose.deploy` and `distr.oci.job`.
- Compose execution and legacy Docker resource-poll deployment behavior remain unchanged.
- Secret references are resolved only for authenticated leases and redacted again at event ingestion.
- The adapter avoids plaintext secrets in Docker command-line process arguments.
- Deterministic containers provide at-most-once execution for the same idempotency key on the same Docker host.
- Operators may need to inspect or intentionally remove deterministic `distr-job-*` containers before re-running a previously completed side-effecting job.

## Alternatives Considered

Allowing mutable tags was rejected because the same task could run different image content across retry or restart.

Passing secrets as `--env KEY=value` was rejected because it exposes resolved secrets in Docker command-line process arguments.

Using `--rm` was rejected because removing the container after exit would remove the idempotency marker and allow the same operation to run again after lease reclaim or agent restart.

Using unrestricted Docker networks or host mounts was rejected because this action is generic and should not silently widen target host access.

Adding a server-side job execution table was deferred because the existing task lease, StepRun, and deterministic container state are enough for this PR's Docker-host retry boundary.

Adding file-render, webhook, approvals, guided failure, cancellation UI, or task timeline UI behavior was rejected because those belong to later roadmap PRs.

## Validation

Validation added in PR-026 covers:

- action registry schema/order and Docker capability reporting
- OCI input policy rejection for mutable tags, registry allowlist mismatch, network allowlist mismatch, writable mounts, disallowed mount roots, privileged mode, and disabled read-only root filesystem
- Docker command construction with digest image references and hardening flags
- omission of resolved secret values from Docker command-line arguments
- task lease dispatch and lifecycle StepRun events
- failure and returned-error redaction
- expected non-zero exit code handling
- timeout stop behavior
- cancellation stop behavior
- deterministic-container idempotency reuse after reclaim or restart
- lease-time OCI secret resolution without storing plaintext secrets in plans
- server-side StepRun event/log/output redaction for OCI secrets
