# PR-006 Release Bundle Foundation

## Scope

PR-006 adds the feature-flagged Release Bundle foundation for the community fork roadmap.

Included:

- Organization-scoped Release Bundle and Release Bundle Component schema.
- Draft-only CRUD API for Release Bundles and their components.
- Component key uniqueness within a bundle.
- Release number uniqueness within an organization/application scope.
- Organization-scoped application, channel, application-version, and child-bundle reference validation.
- Deterministic canonical serialization and stable SHA-256 checksum calculation.
- Backend, API, handler, mapping, repository, canonicalization, and migration tests.

Excluded:

- Release validation service.
- Publication, immutability transition, block, or archive behavior.
- Release UI.
- CI release API or CLI.
- Promotion, lifecycle eligibility, deployment planning, approvals, retention, execution, or agent behavior.
- Adopter-specific component types, registries, migration concepts, or deployment logic.

Those features remain PR-007 or later roadmap work.

## Feature Flag

The API uses the existing experimental `release_bundles` feature flag.

No frontend route or sidebar entry is added in PR-006. Release Bundle UI is reserved for PR-008.

## Database

Migration `112_release_bundles` adds:

- `ReleaseBundle`
- `ReleaseBundleComponent`

`ReleaseBundle` fields include:

- `organization_id`
- `application_id`
- `channel_id`
- `release_number`
- `release_notes`
- `source_revision`
- `status`
- `canonical_checksum`
- `canonical_payload`

`ReleaseBundleComponent` fields include:

- `release_bundle_id`
- `key`
- `name`
- `component_type`
- `version`
- `application_version_id`
- `package_ref`
- `digest`
- `checksum`
- `child_release_bundle_id`

Uniqueness rules:

- Release numbers are unique per organization and application.
- Component keys are unique per Release Bundle.

References:

- Organization references cascade on organization delete.
- Application, Channel, Application Version, and child Release Bundle references are restricted while Release Bundles use them.

## API

Endpoints:

- `GET /api/v1/release-bundles`
- `POST /api/v1/release-bundles`
- `GET /api/v1/release-bundles/{releaseBundleId}`
- `PUT /api/v1/release-bundles/{releaseBundleId}`
- `DELETE /api/v1/release-bundles/{releaseBundleId}`

Create/update request:

```json
{
  "applicationId": "00000000-0000-0000-0000-000000000000",
  "channelId": "00000000-0000-0000-0000-000000000000",
  "releaseNumber": "2026.06.20",
  "releaseNotes": "Initial release",
  "sourceRevision": "abc123",
  "components": [
    {
      "key": "api",
      "name": "API",
      "type": "application_version",
      "version": "1.2.3",
      "applicationVersionId": "00000000-0000-0000-0000-000000000000"
    }
  ]
}
```

Validation:

- Release numbers are trimmed before persistence.
- Empty release numbers are rejected.
- Missing application and channel IDs are rejected.
- At least one component is required.
- Component keys, names, versions, package refs, digests, and checksums are trimmed before persistence.
- Empty component keys and duplicate trimmed component keys are rejected.
- Component type-specific required fields are validated.
- Malformed path UUIDs return 404.
- Missing or cross-organization resources return 404.
- Duplicate release numbers within the same organization/application return 400.
- Non-draft update and delete attempts return 409.

## Canonical Checksum

The canonical payload excludes database row IDs, timestamps, and component row order. Components are sorted by key before JSON serialization. The stored checksum is `sha256:<hex>`.

Changing semantic content, such as a component version, changes the checksum. Reordering the same components does not.

## Compatibility

Existing Environment, Lifecycle, Channel, deployment target, deployment, release-name, and agent behavior is unchanged. Existing direct application-version deployments continue to work.
