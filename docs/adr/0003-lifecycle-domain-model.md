# ADR-0003 - Lifecycle Domain Model

## Status

Accepted for PR-003.

## Context

The roadmap introduces Lifecycles as ordered promotion models composed of phases. PR-003 needs the standalone lifecycle and phase model, a basic phase editor, and an eligibility service skeleton without adding channels, release bundles, deployment planning, approvals, or the full promotion rule engine.

## Decision

Add organization-scoped `Lifecycle`, `LifecyclePhase`, and `LifecyclePhaseEnvironment` tables with DB types, API DTOs, CRUD API, and admin UI. The API is guarded by the `lifecycles` experimental feature flag introduced in PR-001.

Each lifecycle contains ordered phases. Each phase stores:

- name;
- description;
- sort order;
- one or more environment IDs;
- optional marker;
- automatic promotion marker;
- minimum successful deployments;
- nullable approval policy ID placeholder;
- nullable retention policy ID placeholder.

The UI route is also gated by the `environments` flag because the phase editor needs environment options from the PR-002 endpoint.

## Consequences

Existing deployment targets, deployments, releases, and agents are unaffected. Lifecycles can be created and edited independently, but they do not yet influence release promotion, channel selection, deployment planning, approvals, retention, or execution.

The eligibility service intentionally returns a skeleton explanation until release, channel, and deployment history models exist. Full required/optional phase evaluation and explanation APIs remain part of PR-010.
