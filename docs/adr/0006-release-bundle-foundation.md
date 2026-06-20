# ADR-0006: Release Bundle Foundation

## Status

Accepted for PR-006.

## Context

The roadmap introduces Release Bundles as generic, immutable release snapshots that can coordinate one or more versioned components. PR-006 is limited to the foundation: release and component persistence, draft CRUD APIs, deterministic canonical serialization, and checksum calculation.

Validation services, publication, immutability enforcement for published releases, block/archive behavior, Release UI, CI APIs, deployment planning, promotion, approvals, retention, execution, and agent changes belong to later roadmap PRs.

## Decision

Add organization-scoped `ReleaseBundle` and `ReleaseBundleComponent` tables.

Each Release Bundle belongs to:

- one organization;
- one application;
- one channel.

Release numbers are unique by `(organization_id, application_id, release_number)`. This lets separate applications reuse the same release number while preventing ambiguous releases within one application.

PR-006 stores release state but exposes only draft CRUD behavior. Create always creates `DRAFT`; update and delete reject non-draft rows. Later PRs may add APIs that transition the stored state to `VALIDATING`, `PUBLISHED`, `BLOCKED`, or `ARCHIVED`.

Components are keyed within a bundle. Component keys are unique per bundle. PR-006 supports generic component types:

- `application_version`
- `oci_image`
- `oci_artifact`
- `helm_chart`
- `child_release_bundle`
- `external_artifact`

Application-version components must reference an application version that belongs to the bundle application and current organization. Child-release components must reference a Release Bundle in the current organization. Other component types store generic package, digest, or checksum metadata without provider-specific behavior.

Canonical serialization is computed in a pure `internal/releasebundles` package. Components are ordered by key before JSON serialization, and the SHA-256 checksum is stored as `sha256:<hex>`. IDs, timestamps, and database row order are excluded from the canonical payload.

The API is guarded by the `release_bundles` experimental feature flag.

## Consequences

- Cross-organization application, channel, application-version, and child-bundle references return not found.
- Duplicate release numbers within one application are rejected.
- Duplicate component keys within one bundle are rejected.
- Draft bundles can be created, read, updated, and deleted.
- Non-draft rows cannot be updated or deleted through PR-006 APIs.
- Existing application, channel, deployment target, deployment, release-name, and agent behavior is unchanged.
- PR-006 does not add Release UI, publish/validate/block/archive endpoints, CI APIs, promotion, deployment planning, approval, retention, execution, or agent behavior.
