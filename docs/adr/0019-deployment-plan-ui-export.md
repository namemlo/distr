# ADR-0019 - Deployment Plan UI and export

## Status

Accepted

## Context

PR-018 introduced durable Deployment Plan API responses that already contain the resolved targets, steps, variables, blockers, warnings, and canonical checksum needed for a read-only planning preview.

PR-019 needs to make those plans visible to vendor admins and provide JSON/Markdown export without starting PR-020 task queue or later execution features.

## Decision

Add a feature-flagged Angular Deployment Plans administration page that consumes the existing PR-018 API:

- `GET /api/v1/deployment-plans`
- `POST /api/v1/deployment-plans`
- `GET /api/v1/deployment-plans/{deploymentPlanId}`

The UI creates plans by sending a published Release Bundle ID, Environment ID, and selected Deployment Target IDs.

The UI renders blockers, warnings, resolved targets, resolved steps, resolved variables, and the canonical checksum.

JSON and Markdown export are generated client-side from the API response. No new backend export endpoint is added in PR-019.

## Consequences

- Exported JSON is exactly the API-visible Deployment Plan shape.
- Markdown export is a human-readable summary of the same API-visible data.
- Redacted variable values remain redacted because the UI only receives redacted data from the API.
- The backend planning model stays unchanged.
- PR-019 does not add execution, task queue, approval, lock, lease, notification, runbook, rollout wave, or agent protocol behavior.

## Alternatives Considered

Backend export endpoints were rejected for PR-019 because the existing API response is sufficient and adding export endpoints would expand the backend surface without improving the roadmap milestone.

Adding execution-aware fields or queue state to the UI was rejected because PR-020 and later PRs own those behaviors.
