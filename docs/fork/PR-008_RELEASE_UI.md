# PR-008 Release UI

## Scope

PR-008 adds the feature-flagged Angular administration UI for Release Bundles from the community fork roadmap.

Included:

- Release Bundle list view with application, channel, status, component count, checksum, loading, empty, and error states.
- Draft create and edit forms backed by the existing PR-006 Release Bundle CRUD API.
- Component table/editor for the existing Release Bundle component types.
- Read-only detail view for non-draft releases with component versions, references, digests, checksum, and publication metadata.
- Structured PR-007 validation error and warning display.
- Publish confirmation flow that validates first, shows validation results, and publishes only after explicit confirmation.
- Status-aware delete, block, and archive actions using existing PR-006 and PR-007 endpoints.
- Feature-flagged route, sidebar entry, Angular service, route coverage, component tests, and service tests.

Excluded:

- CI release API, idempotent release creation, CLI commands, or CI examples.
- Lifecycle eligibility, promotion checks, explanation APIs, deployment planning, approvals, retention, execution, notifications, or agent behavior.
- New component types, provider-specific registry behavior, or adopter-specific terminology.
- Database migrations or backend API changes.

Those features remain PR-009 or later roadmap work.

## Feature Flag

The Release Bundle UI is available only when the `release_bundles` experimental feature flag is enabled.

The route and sidebar also require the existing `environments`, `lifecycles`, and `channels` flags because the editor uses organization-scoped application and channel data already introduced by earlier roadmap PRs.

## UI

The new `/release-bundles` route is restricted to vendor admins. It loads:

- `GET /api/v1/release-bundles`
- `GET /api/v1/applications`
- `GET /api/v1/channels`

Draft releases can be created, edited, validated, published, or deleted. Published releases can be blocked or archived. Blocked releases can be archived. Non-draft releases open in a read-only detail view and remain immutable through the existing backend rules.

The component editor supports the PR-006 component types:

- `application_version`
- `oci_image`
- `oci_artifact`
- `helm_chart`
- `child_release_bundle`
- `external_artifact`

Application-version components can select an application version from the selected application. Other component types expose only their generic package, digest, checksum, or child release fields.

## API

PR-008 adds no API endpoints. The Angular service calls the existing feature-flagged endpoints:

- `GET /api/v1/release-bundles`
- `POST /api/v1/release-bundles`
- `GET /api/v1/release-bundles/{releaseBundleId}`
- `PUT /api/v1/release-bundles/{releaseBundleId}`
- `DELETE /api/v1/release-bundles/{releaseBundleId}`
- `POST /api/v1/release-bundles/{releaseBundleId}/validate`
- `POST /api/v1/release-bundles/{releaseBundleId}/publish`
- `POST /api/v1/release-bundles/{releaseBundleId}/block`
- `POST /api/v1/release-bundles/{releaseBundleId}/archive`

## Database

No database migrations are added in PR-008.

## Compatibility

Existing Environment, Lifecycle, Channel, deployment target, deployment, release-name, and agent behavior is unchanged. PR-008 does not alter the PR-006/PR-007 Release Bundle database model, API contracts, validation rules, publication state machine, or immutability guarantees.
