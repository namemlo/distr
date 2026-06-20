# PR-005 SemVer and Source-Rule Engine

## Scope

PR-005 adds generic Channel version and source-rule configuration plus a backend validation engine.

Included:

- SemVer 2.0 version parsing for Channel validation requests.
- SemVer range validation using Channel `allowedVersionRanges`.
- Prerelease glob matching using Channel `allowedPrereleasePatterns`.
- Source branch glob matching using Channel `allowedSourceBranches`.
- Source tag glob matching using Channel `allowedSourceTags`.
- Structured validation errors from `POST /api/v1/channels/{channelId}/validate-version`.
- Backend, API, handler, mapping, database, frontend service/component, and migration coverage.

Excluded:

- Release Bundle tables or CRUD.
- Release publication, promotion, or deployment planning.
- Release form filtering UI.
- Approval, retention, execution, or agent behavior.
- Adopter-specific branch, tag, or version rules.

Those features remain PR-006 or later roadmap work.

## Feature Flag

The validation endpoint and Channel rule configuration remain guarded by the existing experimental `channels` feature flag.

The frontend Channels route keeps its existing `environments`, `lifecycles`, and `channels` gating because the Channel editor still depends on application and lifecycle data.

## Database

Migration `111_channel_rules` adds nullable-free text arrays to `Channel`:

- `allowed_version_ranges`
- `allowed_prerelease_patterns`
- `allowed_source_branches`
- `allowed_source_tags`

All four columns default to empty arrays. Empty arrays mean no restriction for that rule category.

## API

Channel create, update, and read payloads now include:

```json
{
  "allowedVersionRanges": [">=1.0.0 <2.0.0"],
  "allowedPrereleasePatterns": ["rc.*"],
  "allowedSourceBranches": ["main", "release/*"],
  "allowedSourceTags": ["v*"]
}
```

Validation endpoint:

```http
POST /api/v1/channels/{channelId}/validate-version
```

Request:

```json
{
  "version": "1.2.3-rc.1",
  "sourceBranch": "release/2026.06"
}
```

Response:

```json
{
  "valid": false,
  "errors": [
    {
      "field": "version",
      "rule": ">=2.0.0 <3.0.0",
      "message": "version does not match an allowed range"
    }
  ]
}
```

Validation behavior:

- Channel rule lists are trimmed before validation and persistence.
- Empty rule-list entries are rejected.
- Duplicate trimmed rule-list entries are rejected.
- Invalid SemVer ranges are rejected on Channel create/update.
- Invalid glob patterns are rejected on Channel create/update.
- Version validation requests require a non-empty strict SemVer 2.0 version.
- Missing or cross-organization Channels return 404.
- Source branch and tag rules are evaluated only when configured.

## Compatibility

Existing Environment, Lifecycle, deployment target, deployment, release, and agent behavior is unchanged. Existing Channels continue to work with empty rule arrays after migration.
