# PR-012 Deployment Process Editor UI

## Scope

PR-012 adds the feature-gated Angular administration UI for Deployment Processes from the community fork roadmap.

Included:

- Deployment Process list view with application lookup, loading, empty, and API-error states.
- Create, update, and delete process actions backed by the PR-011 CRUD API.
- Revision history view backed by the PR-011 revision list API.
- Revision detail view for immutable revision steps.
- Structured revision creation form with ordered steps, dependencies, action metadata, input bindings, conditions, scoped channels, scoped environments, target tags, failure mode, timeout, retry policy, and required permissions.
- Feature-gated route, sidebar entry, Angular service/types, component tests, service tests, and feature-flag service coverage.

Excluded:

- Backend API, database, or migration changes.
- Process snapshots or Release Bundle process links.
- Step Template CRUD, imports, action registry, or built-in action schemas.
- Variables, scoped-variable resolution, deployment planning, approvals, retention, execution, notifications, task queue, or agent behavior.

Those features remain PR-013 or later roadmap work.

## Feature Flag

The Deployment Process UI is available only when the `deployment_processes` experimental feature flag is enabled.

The route and sidebar also require the existing `environments`, `lifecycles`, and `channels` flags because the editor exposes Channel and Environment selectors that depend on those earlier roadmap capabilities.

## UI

The new `/deployment-processes` route is restricted to vendor admins. It loads:

- `GET /api/v1/deployment-processes`
- `GET /api/v1/applications`
- `GET /api/v1/channels`
- `GET /api/v1/environments`

Admins can create, edit, and delete Deployment Processes. The process editor stores only process metadata: application, name, description, and sort order.

Admins can open revision history for a process, inspect immutable revision steps, and create a new revision through a structured step editor. Step input bindings are entered as JSON objects and validated client-side before the PR-011 API request is sent.

## API

PR-012 adds no API endpoints. The Angular service calls the existing PR-011 feature-gated endpoints:

- `GET /api/v1/deployment-processes`
- `POST /api/v1/deployment-processes`
- `GET /api/v1/deployment-processes/{deploymentProcessId}`
- `PUT /api/v1/deployment-processes/{deploymentProcessId}`
- `DELETE /api/v1/deployment-processes/{deploymentProcessId}`
- `GET /api/v1/deployment-processes/{deploymentProcessId}/revisions`
- `POST /api/v1/deployment-processes/{deploymentProcessId}/revisions`
- `GET /api/v1/deployment-processes/{deploymentProcessId}/revisions/{revisionId}`

## Database

No database migrations are added in PR-012.

## Compatibility

Existing Environment, Lifecycle, Channel, Release Bundle, deployment target, deployment, release-name, and agent behavior is unchanged. PR-012 does not alter PR-011 API contracts, validation rules, revision immutability, organization isolation, or Channel reference constraints.
