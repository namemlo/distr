# ADR-0016: Variable Snapshots and Drift

## Status

Accepted for PR-016.

## Context

PR-014 introduced typed Variable Sets and PR-015 added deterministic scoped resolution. The roadmap next requires immutable Release Bundle variable snapshots and a way to preview configuration drift for existing deployments after Variable schema changes.

Secret safety remains a hard boundary: snapshot and drift APIs must not persist or return plaintext secret values.

## Decision

Create a `VariableSnapshot` when a draft Release Bundle is published. Snapshot creation runs inside the same database transaction as the publish transition, uses the Release Bundle organization/application/channel scope, resolves all Variable Sets attached to that Application, and links the resulting snapshot back to the Release Bundle.

Store snapshot values as typed rows plus a canonical payload/checksum. Redacted variables store reference metadata and trace information only; the database enforces that redacted rows cannot store a JSON value.

Expose a read-only snapshot API gated by both `release_bundles` and `scoped_variables_v2`.

Add a drift comparator that parses the latest deployment revision `env_file_data` and `values_yaml`, resolves the current Variable schema for the deployment Application and target scope, and reports drift categories without changing deployment execution behavior.

Expose the drift API and deployment detail UI behind `scoped_variables_v2`.

## Consequences

- Published Release Bundles can be traced to the exact Variable resolution snapshot present at publication time.
- Snapshot reads remain organization-scoped and do not expose cross-organization resources.
- Drift detection is read-only and does not change deployment target, deployment, release, or agent behavior.
- Secret/reference variables are safe to inspect because plaintext secret values are neither persisted nor returned.
- Later deployment planning work can reuse snapshots and drift results without changing PR-016 API contracts.

## Alternatives Considered

Storing snapshots as a single JSON payload only was rejected because row-level snapshot values are easier to query, validate, test, and redact.

Creating snapshots asynchronously after publication was rejected because a Release Bundle could be published without an attached Variable snapshot.

Adding deployment remediation or planning actions was rejected because those behaviors belong to later roadmap PRs.

## Validation

PR-016 adds drift comparator tests, mapping tests, handler and feature-flag tests, live PostgreSQL repository and handler integration tests, migration checks, Angular service and component tests, and changed-file Unicode scans.
