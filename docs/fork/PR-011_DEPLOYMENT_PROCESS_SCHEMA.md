# PR-011 Deployment Process Schema

## Scope

PR-011 adds the feature-flagged Deployment Process backend schema and API foundation.

Included:

- Organization/application-scoped Deployment Processes.
- Immutable process revisions created through revision POSTs.
- Ordered step persistence with typed action metadata.
- Step dependency persistence and validation.
- Step Channel and Environment references.
- Duplicate process-name rejection within one organization/application.
- Organization-scoped Application, Channel, and Environment validation.
- Backend, API, handler, mapping, repository, migration, and live PostgreSQL tests.

Excluded:

- Process editor UI.
- Step Template CRUD or imports.
- Built-in action registry.
- Release Bundle process snapshots or links.
- Variable sets or scoped variables.
- Deployment planning, previews, execution, approvals, retention, notifications, or agent changes.

Those features remain PR-012 or later roadmap work.

## Feature Flag

The API uses the existing experimental `deployment_processes` feature flag.

When the flag is disabled, all Deployment Process endpoints return `403 Forbidden`.

## Database

Migration `115_deployment_processes` adds:

- `DeploymentProcess`
- `DeploymentProcessRevision`
- `DeploymentProcessStep`
- `DeploymentProcessStepDependency`
- `DeploymentProcessStepChannel`
- `DeploymentProcessStepEnvironment`

`DeploymentProcess` belongs to one organization and one application. Names are unique by `(organization_id, application_id, name)`.

`DeploymentProcessRevision` belongs to one process and stores monotonically increasing `revision_number` values. Revisions are append-only through the PR-011 API.

`DeploymentProcessStep` stores generic step metadata:

- `key`
- `name`
- `action_type`
- nullable `step_template_version_id`
- `execution_location`
- `input_bindings`
- `condition`
- `target_tags`
- `failure_mode`
- `timeout_seconds`
- retry counters
- `required_permissions`
- `sort_order`

Step keys and sort orders are unique within one revision. Dependencies are stored by step key and constrained to existing step keys in the same revision.

Step Channel references must belong to the same organization and application as the process. Step Environment references must belong to the current organization.

## API

Endpoints:

- `GET /api/v1/deployment-processes`
- `POST /api/v1/deployment-processes`
- `GET /api/v1/deployment-processes/{deploymentProcessId}`
- `PUT /api/v1/deployment-processes/{deploymentProcessId}`
- `DELETE /api/v1/deployment-processes/{deploymentProcessId}`
- `GET /api/v1/deployment-processes/{deploymentProcessId}/revisions`
- `POST /api/v1/deployment-processes/{deploymentProcessId}/revisions`
- `GET /api/v1/deployment-processes/{deploymentProcessId}/revisions/{revisionId}`

Create/update process request:

```json
{
  "applicationId": "00000000-0000-0000-0000-000000000000",
  "name": "Standard deploy",
  "description": "Deploys through the standard lifecycle",
  "sortOrder": 10
}
```

Create revision request:

```json
{
  "description": "Initial revision",
  "steps": [
    {
      "key": "prepare",
      "name": "Prepare",
      "actionType": "script",
      "executionLocation": "hub",
      "sortOrder": 10
    },
    {
      "key": "deploy",
      "name": "Deploy",
      "actionType": "script",
      "executionLocation": "hub",
      "inputBindings": {
        "script": "make deploy"
      },
      "channelIds": ["00000000-0000-0000-0000-000000000000"],
      "environmentIds": ["00000000-0000-0000-0000-000000000000"],
      "sortOrder": 20,
      "dependencies": ["prepare"]
    }
  ]
}
```

Validation:

- Process and step names are trimmed before persistence.
- Empty process names are rejected.
- Missing application IDs are rejected.
- Duplicate process names are rejected within one organization/application.
- Step keys, names, action types, execution locations, conditions, failure modes, dependency keys, target tags, and required permissions are trimmed.
- At least one step is required for a revision.
- Duplicate trimmed step keys are rejected.
- Duplicate step sort orders are rejected.
- Dependencies must refer to existing step keys in the same revision.
- Self-dependencies and dependency cycles are rejected.
- Duplicate dependencies within one step are rejected.
- Nil Channel or Environment IDs are rejected.
- Channel and Environment references are organization-scoped.
- Channel references must belong to the process application.
- Malformed path UUIDs return `404 Not Found`.
- Missing or cross-organization resources return `404 Not Found`.

## Compatibility

Existing Environment, Lifecycle, Channel, Release Bundle, deployment target, deployment, release-name, and agent behavior is unchanged.

PR-011 adds no UI route, no process editor, no Release Bundle process link, and no deployment execution behavior.
