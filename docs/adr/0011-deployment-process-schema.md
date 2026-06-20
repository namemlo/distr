# ADR-0011: Deployment Process Schema

## Status

Accepted for PR-011.

## Context

The roadmap introduces Deployment Processes as reusable ordered or grouped sets of typed deployment steps. Later roadmap work will add a process editor UI, reusable Step Templates, immutable Release Bundle process snapshots, variables, deployment planning, approvals, execution, and agent behavior.

PR-011 is limited to the backend schema and API foundation needed before those later features can be built safely.

## Decision

Add organization/application-scoped Deployment Process persistence and API endpoints behind the `deployment_processes` experimental feature flag.

The schema stores:

- process metadata in `DeploymentProcess`;
- append-only revisions in `DeploymentProcessRevision`;
- normalized step rows in `DeploymentProcessStep`;
- dependency edges in `DeploymentProcessStepDependency`;
- step Channel scopes in `DeploymentProcessStepChannel`;
- step Environment scopes in `DeploymentProcessStepEnvironment`.

Each process belongs to one organization and one application. Process names are unique within the organization/application scope.

Each revision receives a monotonically increasing revision number while holding the parent process row lock. PR-011 APIs create revisions but do not update or delete individual revisions.

Step keys and sort orders are unique within a revision. Dependencies reference step keys in the same revision and are validated before persistence. Missing dependencies, self-dependencies, duplicate dependency edges, and cycles are rejected.

Step Channel references must belong to the current organization and process application. Step Environment references must belong to the current organization. Missing or cross-organization references return not found.

The API exposes:

```http
GET /api/v1/deployment-processes
POST /api/v1/deployment-processes
GET /api/v1/deployment-processes/{deploymentProcessId}
PUT /api/v1/deployment-processes/{deploymentProcessId}
DELETE /api/v1/deployment-processes/{deploymentProcessId}
GET /api/v1/deployment-processes/{deploymentProcessId}/revisions
POST /api/v1/deployment-processes/{deploymentProcessId}/revisions
GET /api/v1/deployment-processes/{deploymentProcessId}/revisions/{revisionId}
```

PR-011 intentionally does not expose Step Template endpoints, process editor UI, Release Bundle process links, deployment planning, execution, approvals, retention, notifications, or agent protocol changes.

## Consequences

- Later PRs can link releases and planners to immutable process revisions instead of mutable process definitions.
- Organization isolation is enforced by repository queries and relation validation.
- Channel references cannot silently cross application boundaries.
- Dependency validation prevents cyclic process definitions from entering storage.
- The API is additive and feature-gated.
- Existing Environment, Lifecycle, Channel, Release Bundle, deployment target, deployment, release-name, and agent behavior remains unchanged.
