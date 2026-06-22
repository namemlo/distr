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
DISTR_OCI_JOB_SECRET_STAGING_DIR
```

They are intentionally not accepted from Deployment Process action input, because action input must not grant its own host permissions.

The server resolves `secretEnvironment` only while building an authenticated agent lease. Stored Deployment Plans and Process Snapshots keep secret keys only. The lease payload keeps resolved values in lease-only `secretEnvironment`, leaves public `environment` separate, and records `SecretReferences`. StepRun event ingestion resolves the same secret references from stored plan input and redacts matching values from event messages, details, logs, and non-sensitive outputs.

The Docker adapter enforces these policies:

- `imageDigest` must be an immutable `@sha256` image reference with an explicit registry allowlisted by the Docker agent.
- Mutable tags are rejected.
- The default network is `none`; any selected network must be present in the Docker agent's trusted network allowlist.
- Volumes must be read-only, absolute host paths, and under a symlink-resolved trusted host source root. The resolved canonical source path is stored in the decoded action input and passed to Docker.
- Privileged containers are rejected.
- The root filesystem is read-only and cannot be disabled by the adapter.
- `--security-opt no-new-privileges` is always used.
- `--cap-drop ALL` is always used.
- `--log-driver none` is always used so retained deterministic containers do not keep raw stdout/stderr in Docker logs.
- Optional `runAsUser`, CPU, and memory limits are passed through to Docker.

The adapter writes public environment variables to a temporary env file and passes that file through Docker `--env-file`. Resolved `secretEnvironment` values are written to a separate per-operation temporary shell env file under `DISTR_OCI_JOB_SECRET_STAGING_DIR`, chmodded container-readable, bind-mounted read-only into the container, sourced by an explicit `--entrypoint /bin/sh` wrapper, and removed after command completion. For containerized Docker agents using a host Docker socket, this staging directory must be mounted into the agent at the same host-visible absolute path so the Docker daemon can resolve the bind source. This keeps secret values out of Docker command-line arguments and retained container `Config.Env` metadata. Images that use `secretEnvironment` must provide `/bin/sh`.

Idempotency uses a deterministic Docker container name derived from the action idempotency key. The adapter reserves that deterministic operation before inspect, secret staging, and container create so concurrent same-operation agents cannot delete each other's staging files or report a false name-conflict failure. Before running `docker run`, the adapter removes stale unreferenced secret staging directories only for that deterministic operation and inspects the deterministic container. An exited matching container is treated as the completed operation, a running matching container is waited, and a created matching container is started and then waited so restart/reclaim does not falsely mark an unexecuted job successful. Timeout/cancellation stops the deterministic container, falls back to `docker kill` if stop fails, and verifies the container is no longer running before removing any mounted secret staging source. Once an existing container reaches a terminal state or is stopped during timeout/cancellation, the adapter removes its mounted per-operation secret staging directory. If a created matching container references a missing secret staging mount, it is removed and recreated with a fresh staged secret file because it has not executed yet. Reclaim does not replay retained raw Docker logs because they may contain old secret values after rotation; it returns a generic reuse status with the exit code. Unsupported existing-container states fail explicitly. This covers normal retry, lease expiry reclaim, and agent restart on the same Docker host.

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
- The adapter avoids plaintext secrets in Docker command-line process arguments and retained Docker container environment metadata.
- Deterministic containers provide at-most-once execution for the same idempotency key on the same Docker host.
- Operators may need to inspect or intentionally remove deterministic `distr-job-*` containers before re-running a previously completed side-effecting job.

## Alternatives Considered

Allowing mutable tags was rejected because the same task could run different image content across retry or restart.

Passing secrets as `--env KEY=value` or Docker `--env-file` was rejected because it exposes resolved secrets in Docker command-line process arguments or retained container environment metadata.

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
