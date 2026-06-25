# ADR-0049: Compatibility and Migration Release

## Status

Accepted

## Context

The roadmap introduces advanced release bundles, deployment plans, tasks, timelines, and Config as Code, but
existing installations may already contain direct `Deployment` and `DeploymentRevision` history. That history
must remain valid without changing old agent protocols or pretending historical advanced metadata exists.

## Decision

Represent each legacy deployment revision as an implicit single-component release projection. The projection
derives synthetic identity and checksum from immutable identifiers and stored hashes only. It does not use
timestamps, current mutable application configuration, current variable values, environment files, values YAML,
or plaintext secrets.

Store additive compatibility metadata in `DeploymentCompatibilityMetadata`. The backfill is dry-run by default,
requires explicit `--apply`, is organization-scoped, idempotent, and resumable through a stable
`created_at`/revision-id cursor. Timeline reads use metadata when present and keep read-through projection
available during partial backfills.

## Consequences

- Existing direct deployment APIs and agent execution semantics remain unchanged.
- Legacy timeline entries can be displayed beside task-backed entries with `source=legacy_deployment`.
- Advanced-only dimensions such as process snapshots, variable snapshots, channels, environments, task logs, and
  redeploy-plan creation are explicitly unavailable for legacy entries.
- Downgrade can drop compatibility metadata without deleting original deployment history.

## Alternatives Considered

- Rewrite legacy rows into release bundles, deployment plans, and tasks. Rejected because required channel,
  environment, actor, process, variable, and log history may be unknowable.
- Leave legacy history out of timeline. Rejected because existing deployments must remain inspectable after the
  advanced deployment roadmap lands.
