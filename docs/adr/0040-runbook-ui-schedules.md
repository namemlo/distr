# ADR-0040: Runbook UI and Schedules

## Status

Accepted

## Context

PR-039 introduced the runbook domain model, revision APIs, and immutable published snapshots. Operators now need a feature-flagged UI for authoring and publishing runbooks, but the execution engine and scheduler are intentionally later roadmap work.

The first UI layer must avoid inventing backend behavior that does not exist yet, while still reserving the expected places for run history and schedule configuration.

## Decision

Add a vendor-admin Runbooks route guarded by the existing experimental `runbooks` feature flag. The route uses a typed Angular service for the PR-039 API endpoints and a standalone Runbooks component with editor, history, and schedules tabs.

The editor supports runbook metadata CRUD, runbook detail fetches, revision list loading, structured revision creation, and revision publication. History and schedules are rendered as read-only or disabled surfaces until execution, run history, and scheduler endpoints are added by later PRs.

Do not add database schema, task queue, agent protocol, scheduler, action registry, or execution engine changes in this PR.

## Consequences

Operators can start authoring and publishing runbook definitions behind the feature flag.

Future execution and scheduling PRs can extend the existing tabs instead of introducing a second navigation model.

The UI honestly reflects backend capabilities: run and schedule controls are present as future surfaces, but they do not submit until supporting APIs exist.

Existing deployment, task lease, Docker/Kubernetes agent, webhook, and release-bundle behavior remains unchanged.
