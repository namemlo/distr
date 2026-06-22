# ADR-0027 - File render action

## Status

Accepted

## Context

PR-025 and PR-026 introduced target-executed Compose and OCI task actions through the task lease, heartbeat, and StepRun event protocol. Some deployments also need to materialize target-local configuration files from release variables and secret references before or between execution steps.

The action writes on customer-controlled target hosts, so it must not let Deployment Process input grant arbitrary filesystem access, execute shell templates, persist rendered secrets, or corrupt existing files during retry, lease reclaim, or agent restart.

## Decision

Add a built-in action registry entry for:

```text
distr.file.render
```

The Docker agent advertises `distr.file.render` version `1` and executes it through the existing task lease, heartbeat, StepRun event, output, retry, and reclaim infrastructure.

The action accepts:

- `destinationPath`
- `template`
- `variables`
- `secretVariables`
- `mode`
- `owner`
- `group`
- `backup`
- `idempotencyKey`
- `timeoutSeconds`

Destination access is controlled by trusted Docker-agent configuration:

```text
DISTR_FILE_RENDER_ALLOWED_ROOTS
```

The action input does not choose or widen allowed roots. The Docker agent resolves the configured roots through symlinks, opens the first configured root with `os.Root`, uses it as the destination base for relative `destinationPath` values, creates missing parent directories beneath that root, rejects absolute paths, traversal, destination symlinks, parent symlink escapes, and directory destinations, and performs reads, backup writes, temporary writes, and renames through the root handle.

Templates use a restricted non-shell replacement language:

```text
${name}
${secrets.name}
```

Variable names must match `[A-Za-z_][A-Za-z0-9_]*`. Go templates, shell substitution, dotted public expressions, backticks, missing variables, and public/secret variable name conflicts are rejected.

The server resolves `secretVariables` only while building an authenticated agent lease. Stored Deployment Plans and Process Snapshots keep secret keys only. The lease payload contains resolved secret values in lease-only `secretVariables`, leaves public `variables` separate, and records `SecretReferences`. StepRun event ingestion continues to redact resolved values from messages, logs, details, and non-sensitive outputs.

The Docker adapter renders the file in memory and emits no rendered file content. Before reading an existing destination for no-op detection or backup, it checks the destination with `Lstat`, opens it, then confirms the opened descriptor still matches the checked file with `os.SameFile`; symlinks and swapped destinations are rejected. Backups copy from the opened descriptor to `destinationPath + ".bak"` through a same-directory private `0600` temporary file, apply the final backup mode to the temp descriptor after copying and fsync, verify the temp path still points at the copied file, then rename it. Changed writes create a private `0600` temporary file in the destination directory, write and fsync content while the temp remains agent-owned and inaccessible to other users, apply final mode and numeric owner/group to the temp descriptor, fsync and verify the temp path still points at the written file, then rename it over the destination. If the destination already has the desired bytes and requested metadata already matches, the adapter no-ops so retry, lease reclaim, or agent restart cannot rotate backups or rewrite the file unnecessarily. If bytes match but metadata must change, the adapter uses the same private-temp, metadata-before-rename path instead of mutating the live file in place. When `secretVariables` are present and `mode` is omitted, rendered files default to `0600`; otherwise the default mode is `0644`. Backups use the more restrictive of the existing destination mode and the target render mode, so replacing a private file cannot widen the backup.

The action emits:

- `STARTED` before validation/execution
- `PROGRESS` before rendering/writing
- `SUCCEEDED` with non-sensitive `destinationPath`, `changed`, and optional `backupPath`
- `FAILED` with redacted errors

## Consequences

- Target-executed tasks can materialize configuration files without arbitrary shell execution.
- Secret values stay out of stored plans, snapshots, events, logs, outputs, and returned errors.
- Agent operators must configure trusted destination roots on each Docker agent that supports this action.
- Retried or reclaimed steps are idempotent when the rendered destination already matches the desired content.
- Backups use a deterministic `.bak` path; operators should collect or rotate those files according to their host policy.

## Alternatives Considered

Allowing absolute destination paths or destination roots in action input was rejected because Deployment Process authors must not grant host filesystem permissions.

Using Go templates or shell commands was rejected because this PR only needs scoped string replacement and must avoid template execution surprises.

Persisting rendered content as a StepRun output was rejected because rendered files may include secrets.

Writing directly to the destination was rejected because cancellation, crashes, and retry can leave partial files.

Adding UI, approvals, webhooks, Helm actions, or notification behavior was rejected because those belong to later roadmap PRs.

## Validation

Validation added in PR-027 covers:

- action registry schema/order and Docker capability reporting
- restricted template syntax, missing variables, variable conflicts, traversal, absolute paths, symlink escape rejection, and configured destination-root requirements
- atomic replacement, backup behavior, private temporary-file mode, temporary-path tamper rejection, mode handling, equal-content metadata rollback, secret-rendered `0600` defaults, deterministic parent path-swap rejection, destination-swap backup/metadata rejection, retry no-op behavior, timeout/cancellation handling, and temporary-file cleanup
- task lease dispatch, heartbeat integration, and lifecycle StepRun events
- omission/redaction of resolved secret values from emitted events and outputs
- lease-time file render secret resolution without storing plaintext secrets in plans
- Hub-side file-render StepRun event/log/output redaction and read-path persistence checks
