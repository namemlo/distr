# PR-004 Channel Domain Model

## Scope

PR-004 adds the feature-flagged Channel domain model for the community fork roadmap.

Included:

- Organization-scoped Channel CRUD.
- Channel association to one Application and one Lifecycle.
- Automatic default Channel creation where an organization has applications and at least one lifecycle.
- Admin UI for listing, creating, editing, and deleting Channels.
- Backend, API, handler, mapping, database, frontend, and migration coverage.

Excluded:

- Version validation.
- Source branch or tag rule evaluation.
- Release bundles.
- Release promotion.
- Deployment planning or execution.
- Approval, retention, and deployment process behavior.

Those features remain PR-005 or later roadmap work.

## Feature Flag

The API and UI use the existing experimental `channels` feature flag.

The frontend route and sidebar entry also require `environments` and `lifecycles` because the Channel editor depends on lifecycle data. Existing flags are preserved.

## Database

Migration `110_channels` adds `Channel`:

- `organization_id`
- `application_id`
- `lifecycle_id`
- `name`
- `description`
- `sort_order`
- `is_default`

Uniqueness rules:

- Channel names are unique per organization and application.
- At most one default Channel exists per organization and application.

References:

- Application references cascade on application delete.
- Lifecycle references restrict lifecycle delete while Channels use the lifecycle.

## API

Endpoints:

- `GET /api/v1/channels`
- `POST /api/v1/channels`
- `GET /api/v1/channels/{channelId}`
- `PUT /api/v1/channels/{channelId}`
- `DELETE /api/v1/channels/{channelId}`

Validation:

- Names are trimmed before persistence.
- Empty names are rejected.
- Negative sort order is rejected.
- Missing or malformed UUID references are rejected with 4xx responses.
- Application and lifecycle references must belong to the current organization.
- Cross-organization resources return 404.
- Duplicate names within the same organization/application scope return 400.
- Default Channels cannot be deleted.

## Default Channel Behavior

`GET /api/v1/channels` ensures one default Channel named `Stable` for applications that do not yet have any Channel, as long as the organization has at least one lifecycle.

The operation is idempotent:

- Running it multiple times does not create duplicate defaults.
- The database enforces only one default Channel per organization/application.
- Creating the first Channel for an application marks it default if the request did not.
- Marking another Channel default clears the previous default for that application.

## Compatibility

PR-004 does not alter Environment, Lifecycle, deployment target, deployment, release, or agent behavior. Existing application behavior is unchanged unless the experimental Channels API is called.
