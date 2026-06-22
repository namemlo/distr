# ADR-0039: Runbook Model

## Status

Accepted

## Context

Deployment processes cover release-oriented workflows, but the roadmap also calls for generic runbooks: operational workflows that are not tied to release promotion. The model needs revision history and immutable published snapshots before UI, scheduling, or execution can be built on top of it.

The existing task queue is deployment-specific in practice, so this PR should reserve a runbook task type without changing current deployment leasing or agent behavior.

## Decision

Add organization/application-scoped `Runbook` records with append-only revisions and ordered steps. Runbook steps reuse the action registry, input binding validation, restricted condition syntax, output-reference dependency checks, retry fields, required permissions, and failure mode fields already used by deployment processes, while omitting deployment-specific channel, environment, and target filters.

Publishing a runbook revision creates an immutable `RunbookSnapshot` containing canonical JSON and a checksum. Publishing is idempotent per revision and returns the existing snapshot if the revision was already published.

Add feature-flagged runbook APIs for CRUD, revision list/create/read, and revision publication. Add `Task.task_type` with existing task creation explicitly defaulting to `deployment`; `runbook` remains reserved for later execution work.

## Consequences

Runbooks can be authored and versioned independently from deployment processes.

Future UI and execution PRs can depend on stable snapshots rather than mutable drafts.

Existing deployment tasks, agent leases, Docker/Kubernetes agent protocols, and deployment execution behavior remain unchanged.
