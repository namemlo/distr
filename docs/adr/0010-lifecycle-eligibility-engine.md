# ADR-0010: Lifecycle Eligibility Engine

## Status

Accepted for PR-010.

## Context

PR-003 introduced Lifecycles and a skeleton eligibility service. PR-004 introduced Channels that select a Lifecycle for an Application. PR-006 through PR-009 introduced Release Bundles, publication, source metadata, and CI creation. The roadmap next requires lifecycle eligibility checks and an explanation API without implementing promotion execution, deployment planning, approvals, retention, notifications, or agent behavior.

The current fork domain uses Release Bundles as the release unit. The roadmap's generic `/api/v1/releases/{id}/eligibility` route therefore needs to map onto the existing Release Bundle API surface.

## Decision

Add a read-only eligibility endpoint:

```http
GET /api/v1/release-bundles/{releaseBundleId}/eligibility?environmentId={environmentId}
```

The endpoint is available only when these experimental feature flags are enabled:

- `release_bundles`
- `channels`
- `lifecycles`
- `environments`

The repository loads all referenced records through existing organization-scoped paths:

- Release Bundle by ID and organization.
- Environment by ID and organization.
- Channel by the Release Bundle channel ID and organization.
- Lifecycle by the Channel lifecycle ID and organization.

The lifecycle service returns a deterministic explanation:

- release status checks;
- Channel/Lifecycle consistency checks;
- target phase lookup by Environment ID;
- sorted phase details;
- optional prior phases skipped;
- required prior phases blocked until successful deployment evidence exists in later roadmap work;
- approval-required placeholders blocked without implementing approval evaluation.

The response includes structured reason codes rather than only free-form text.

## Consequences

- Clients can preview why a published Release Bundle can or cannot enter an Environment without causing mutations.
- Organization isolation remains enforced by repository reads rather than service-level trust in caller-provided records.
- The API is additive and feature-gated.
- No schema migration is required.
- Required-prior-phase eligibility is conservative until later deployment history models exist.
- PR-010 does not implement promotion execution, deployment planning, deployment creation, approvals, freeze windows, retention, notifications, UI workflows, or agent changes.
