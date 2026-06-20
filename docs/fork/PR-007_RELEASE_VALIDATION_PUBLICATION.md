# PR-007 Release Validation and Publication

## Scope

PR-007 adds the feature-flagged Release Bundle validation and publication layer for the community fork roadmap.

Included:

- Release Bundle validation service with structured errors and warnings.
- Validation for canonical checksum, component uniqueness, component type requirements, organization-scoped references, channel/application consistency, application-version references, child-bundle references, and PR-005 Channel rule compatibility.
- `POST /api/v1/release-bundles/{releaseBundleId}/validate`.
- `POST /api/v1/release-bundles/{releaseBundleId}/publish`.
- `POST /api/v1/release-bundles/{releaseBundleId}/block`.
- `POST /api/v1/release-bundles/{releaseBundleId}/archive`.
- Publication actor/time metadata.
- Published, blocked, and archived Release Bundle immutability through existing mutation guards.
- Append-only audit events for publish, block, archive, and rejected state transitions.
- Backend, API, handler, mapping, repository, validation, migration, and live PostgreSQL tests.

Excluded:

- Release Bundle UI.
- CI release API or CLI.
- Lifecycle eligibility or promotion checks.
- Deployment planning, approvals, retention, execution, or agent behavior.
- Adopter-specific component types, registries, deployment logic, or source metadata ingestion.

Those features remain PR-008 or later roadmap work.

## Feature Flag

The API continues to use the existing experimental `release_bundles` feature flag.

No frontend route or sidebar entry is added in PR-007. Release Bundle UI is reserved for PR-008.

## Database

Migration `113_release_bundle_publication` adds:

- `ReleaseBundle.published_by_user_account_id`
- `ReleaseBundle.published_at`
- `ReleaseBundleAuditEvent`

Audit event fields include:

- `organization_id`
- `release_bundle_id`
- `actor_user_account_id`
- `event_type`
- `from_status`
- `to_status`
- `reason`

Supported audit event types:

- `published`
- `blocked`
- `archived`
- `state_transition_rejected`

The down migration removes the audit table and publication columns.

## API

New endpoints:

- `POST /api/v1/release-bundles/{releaseBundleId}/validate`
- `POST /api/v1/release-bundles/{releaseBundleId}/publish`
- `POST /api/v1/release-bundles/{releaseBundleId}/block`
- `POST /api/v1/release-bundles/{releaseBundleId}/archive`

Validation response:

```json
{
  "valid": false,
  "errors": [
    {
      "field": "components.api.version",
      "rule": ">=2.0.0 <3.0.0",
      "message": "version does not match an allowed range"
    }
  ],
  "warnings": []
}
```

Release Bundle responses now include publication metadata when set:

```json
{
  "status": "PUBLISHED",
  "publishedByUserAccountId": "00000000-0000-0000-0000-000000000000",
  "publishedAt": "2026-06-20T00:00:00Z"
}
```

Malformed path UUIDs return 404. Missing or cross-organization Release Bundles return 404. Validation failures during publish return 400 with the validation response. Invalid state transitions return 409.

## Validation

Validation checks include:

- Canonical checksum matches current canonical content.
- At least one component exists.
- Component keys are present and unique.
- Component versions are present.
- Component type-specific required fields are present.
- OCI component digests use `sha256:`.
- Application-version components reference an Application Version belonging to the Release Bundle application and current organization.
- Application-version component versions match the referenced Application Version name.
- Application-version components satisfy the Channel's PR-005 version and source rules.
- Child-release components reference an existing Release Bundle in the current organization.
- Child-release components reference published child bundles.

PR-007 does not add source metadata capture. If a Channel has source branch or tag rules, validation requires matching source input and reports the missing source as a validation error until later PRs add source metadata to the release creation flow.

## State Transitions

Allowed transitions:

- `DRAFT -> PUBLISHED` through publish, after validation succeeds.
- `PUBLISHED -> BLOCKED` through block.
- `PUBLISHED -> ARCHIVED` through archive.
- `BLOCKED -> ARCHIVED` through archive.

Rejected transitions write `state_transition_rejected` audit events and return 409. Publish validation failures write `state_transition_rejected` audit events and return 400.

Existing draft update and delete APIs continue to reject non-draft rows, so published, blocked, and archived Release Bundles are immutable through PR-007 APIs.

## Compatibility

Existing Environment, Lifecycle, Channel, deployment target, deployment, release-name, and agent behavior is unchanged. Existing draft Release Bundle CRUD remains feature-flagged behind `release_bundles`. No Release UI, promotion, deployment planning, approval, retention, execution, or agent behavior is added in PR-007.
