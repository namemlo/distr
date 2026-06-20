# ADR-0005: SemVer and Source-Rule Engine

## Status

Accepted for PR-005.

## Context

The roadmap introduces Channels as release tracks that can restrict acceptable versions and sources. PR-004 created the Channel model and intentionally left version/source rule execution to PR-005.

PR-005 needs a generic validation foundation without adding Release Bundles, release publication, promotion, deployment planning, approvals, retention, or agent execution changes.

## Decision

Add Channel rule fields directly to the `Channel` table as text arrays:

- `allowed_version_ranges`
- `allowed_prerelease_patterns`
- `allowed_source_branches`
- `allowed_source_tags`

Empty arrays mean no restriction in that category. The API trims rule entries before persistence, rejects empty trimmed entries, rejects duplicate trimmed entries, validates SemVer ranges with `github.com/Masterminds/semver/v3`, and validates glob syntax before storing a Channel.

Add a pure internal `channelrules` package for evaluation. It validates strict SemVer 2.0 input, checks configured version ranges, checks prerelease glob patterns when a prerelease exists, and checks configured source branch/tag globs. The package returns structured issues instead of writing HTTP responses directly.

Expose validation through:

```http
POST /api/v1/channels/{channelId}/validate-version
```

The endpoint is organization-scoped and guarded by the existing `channels` experimental feature flag. Missing and cross-organization Channels return 404.

The Angular Channel editor stores rule lists as newline-separated text areas and sends arrays to the existing Channel CRUD API. The Angular service also exposes the validation endpoint for later release UI work.

## Consequences

- PR-005 keeps rule configuration generic and reusable.
- Existing Channels migrate with empty rule arrays, preserving current behavior.
- API callers can validate versions and sources before Release Bundles exist.
- Release form filtering and release publication enforcement remain later roadmap work.
- No deployment, release, retention, approval, process, or agent behavior changes in PR-005.

## Alternatives Considered

Add separate rule tables. This was rejected for PR-005 because the roadmap fields are small ordered string lists and there is no rule metadata or lifecycle independent from Channels yet.

Delay persistence and provide only a pure library. This was rejected because Channel rules need to be configured and retrieved through the existing Channel API before later release UI work can consume them.
