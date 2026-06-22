# PR-027 - File render action

## Scope

PR-027 adds the Docker-agent execution adapter for the typed `distr.file.render` action.

It adds:

- built-in `distr.file.render` action registry metadata and input/output schemas
- Docker agent capability reporting for `distr.file.render` version `1`
- Docker agent task-lease execution for rendering text files
- restricted `${name}` and `${secrets.name}` template rendering with scoped variables
- trusted Docker-agent destination-root allowlisting through `DISTR_FILE_RENDER_ALLOWED_ROOTS`
- canonical destination path checks for traversal, symlink escapes, and destination swaps
- private-temp atomic destination writes with optional descriptor-based backups
- file mode and best-effort owner/group application where the OS permits it
- lease-time secret resolution for file-render secret variables
- Docker-agent and Hub-side StepRun event, output, log, timeline, and returned-error redaction for resolved file-render secrets
- retry and restart safety when the destination already has the desired content
- troubleshooting and security guidance for operators

## Feature flags

PR-027 does not introduce a new feature flag.

End-to-end typed execution still depends on the existing prerequisite feature flags and hidden endpoints from earlier roadmap PRs:

```text
DISTR_EXPERIMENTAL_FEATURE_FLAGS=agent_capabilities,task_queue,agent_task_leases,step_events
```

The Docker agent remains compatible when optional capability, lease, heartbeat, or step-event endpoints are missing or disabled.

## Database

No database migration is added in PR-027.

PR-027 reuses:

- `AgentCapabilityReport` and `AgentActionCapability`
- `Task`
- `StepRun`
- `TaskLease`
- `StepRunEvent`
- `StepRunLogChunk`
- `StepRunOutput`
- `Secret`

Stored Deployment Plans and Process Snapshots keep `secretVariables` as secret keys only. Secret values are resolved only when an authenticated agent lease is built. The lease keeps resolved `secretVariables` separate from public `variables`.

## API

No new HTTP endpoint is added in PR-027.

PR-027 reuses:

```http
POST /api/v1/agents/{id}/capabilities
POST /api/v1/agents/{id}/lease
POST /api/v1/agents/{id}/tasks/{taskId}/heartbeat
POST /api/v1/agents/{id}/step-runs/{stepRunId}/events
```

The action registry now exposes `distr.file.render` with this input shape:

```json
{
  "destinationPath": "app/config/runtime.env",
  "template": "API_URL=${apiUrl}\nAPI_TOKEN=${secrets.apiToken}\n",
  "variables": {
    "apiUrl": "https://api.example.com"
  },
  "secretVariables": {
    "apiToken": "render_api_token"
  },
  "mode": "0640",
  "owner": "1000",
  "group": "1000",
  "backup": true,
  "idempotencyKey": "runtime-config",
  "timeoutSeconds": 30
}
```

Destination roots are trusted Docker-agent configuration, not Deployment Process input:

```text
DISTR_FILE_RENDER_ALLOWED_ROOTS=/etc/distr,/var/lib/distr/config
```

`destinationPath` must be relative. The Docker agent opens the first configured allowlisted root with `os.Root`, writes beneath that root, and rejects traversal, absolute paths, destination symlinks, parent symlink escapes, destination swaps, and paths outside that root after canonicalization.

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
  }
]
```

For `distr.file.render`, the agent:

- heartbeats the task lease before and during execution
- emits `STARTED`
- emits `PROGRESS` before rendering/writing
- rejects unsupported action versions, missing allowlisted roots, absolute or traversing destinations, symlink escapes, unsupported template syntax, missing variables, public/secret variable conflicts, invalid modes, and invalid owner/group values
- renders only `${name}` and `${secrets.name}` placeholders from scoped maps
- creates missing parent directories under the allowlisted root
- backs up an existing regular destination to `.bak` from an opened file descriptor when requested
- writes through a private `0600` temporary file in the destination directory followed by root-relative rename
- applies final file mode and owner/group to the private temp descriptor before rename where supported
- defaults rendered files and temporary files to `0600` when `secretVariables` are present and `mode` is omitted, and writes backups with the more restrictive of the existing destination mode and target render mode
- no-ops when the destination already contains the desired bytes and requested metadata already matches
- uses the same private-temp atomic replacement path for equal-content metadata changes
- emits `SUCCEEDED` with non-sensitive `destinationPath`, `changed`, and optional `backupPath`
- emits `FAILED` with redacted errors

## Security notes

- Secrets remain references in Deployment Plans and Process Snapshots.
- Secret values are resolved only for an authenticated lease and are not stored back to the plan.
- Resolved secret values remain lease-only in `secretVariables` and are kept separate from public `variables`.
- StepRun event messages, logs, non-sensitive outputs, and returned agent errors are redacted using resolved secret values.
- Rendered file content is not emitted as a log or output.
- Destination roots are agent configuration. Action input cannot grant new filesystem access.
- Atomic rename avoids partially written destination files during cancellation, timeout, crash, or retry.
- Existing destination symlinks and parent symlink escapes are rejected to avoid writing outside trusted roots. Existing destination reads use `Lstat`, open, and `os.SameFile` before backup or no-op checks so swapped paths cannot redirect backup reads, chmod, or chown to another in-root file. Equal-content metadata changes are not applied to the live file; they use the same private-temp atomic replacement path. New writes keep temporary files agent-owned and `0600` while content is written and fsynced, then apply final mode and owner/group to the temp descriptor, fsync, verify the temp path, and root-relative rename only after metadata succeeds.
- This action does not execute shell commands or template functions.

## Troubleshooting

- `DISTR_FILE_RENDER_ALLOWED_ROOTS is required`: configure at least one absolute destination root on the Docker agent.
- `destinationPath must be relative`: remove leading `/`, `\`, or drive-root syntax from the action input.
- `destinationPath cannot contain traversal`: remove `..` path components.
- `destinationPath escapes allowlisted root`: inspect symlinks in the destination parent path.
- `destinationPath cannot be a symlink` or `destinationPath changed during file render`: inspect destination-file symlinks or concurrent path replacement.
- `unsupported template syntax`: use only `${name}` and `${secrets.name}` placeholders.
- `template variable ... is not provided`: add the public variable to `variables` or use the correct placeholder name.
- `template secret variable ... is not provided`: add the secret reference to `secretVariables` and ensure the secret key exists for the target scope.
- `mode must be an octal file mode`: use values like `0644`, `0600`, or `640`.

## UI

No Angular route, sidebar entry, or page is added in PR-027.

## Compatibility notes

Existing Docker Compose action execution, OCI job action execution, Kubernetes agent behavior, and legacy Docker resource-poll deployment behavior remain unchanged.

## Non-goals

PR-027 does not add:

- webhook actions
- Helm typed actions
- arbitrary host shell execution
- unrestricted filesystem access
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
go test ./internal/db -run TestTaskLeaseRepositoryResolvesFileRenderSecretVariablesOnlyForAgentLease
```

The Docker-agent tests cover validation, success, backup, private temporary-file mode, temporary-path tamper rejection, equal-content metadata rollback, secret-rendered `0600` mode defaults, backup mode non-widening, secret redaction, idempotent no-op retry, symlink escape rejection, deterministic parent path-swap rejection, destination-swap backup and metadata safety, cancellation, task-lease dispatch, and temporary-file cleanup.

The Docker-agent package requires the existing agent environment variables during package initialization. Local tests used dummy endpoint values for `DISTR_TARGET_ID`, `DISTR_TARGET_SECRET`, `DISTR_LOGIN_ENDPOINT`, `DISTR_MANIFEST_ENDPOINT`, `DISTR_RESOURCE_ENDPOINT`, `DISTR_STATUS_ENDPOINT`, `DISTR_METRICS_ENDPOINT`, `DISTR_LOGS_ENDPOINT`, and `DISTR_AGENT_LOGS_ENDPOINT`.

The live database tests require `DISTR_TEST_DATABASE_URL`.
