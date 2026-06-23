# PR-049 - Compatibility and Migration Release

## Summary

PR-049 keeps existing direct application-version deployments valid while adding compatibility metadata for
timeline/redeploy planning. Legacy deployment revisions are projected as deterministic, implicit,
single-component release entries without rewriting original deployment records or fabricating advanced
task/process/channel/environment data.

## Included

- Pure `internal/deploymentcompat` adapter for deterministic legacy deployment projections.
- Additive `DeploymentCompatibilityMetadata` schema for synthetic release identity, checksum, and explicit
  capability availability flags.
- Idempotent, organization-scoped, dry-run-by-default legacy deployment compatibility backfill.
- Operator command: `distr backfill-legacy-deployments --organization-id <org-id> [--apply]`.
- Deployment timeline read-through support for legacy compatibility entries.
- Upgrade guide, downgrade notes, and opt-in PR-049 benchmark fixtures.

## Out of Scope

- Removing or deprecating legacy deployment endpoints.
- Replacing current agent deployment semantics or lease payloads.
- Automatically converting legacy deployments into executable advanced tasks.
- Fabricating historical process snapshots, variable snapshots, channels, environments, actors, or logs.
- New dashboards, migration UI, destructive retention, or unrelated performance rewrites.
- Config as Code synchronization/apply workflows beyond PR-048.

## Compatibility Notes

Existing deployment APIs, deployment rows, deployment revision rows, and agent execution payloads remain
unchanged. Compatibility metadata can be removed without deleting original deployment history. Historical
process, variable, channel, and environment data is reported as unavailable unless it exists in advanced
task-backed history.

## Verification

- `go test ./internal/deploymentcompat ./internal/db ./internal/jobs -count=1`
- `go test ./cmd/hub/cmd -run 'TestBackfillLegacyDeployments' -count=1`
- `go test -run '^$' -bench 'PR049' -benchmem ./internal/deploymentcompat`
- `hack/validate-migrations.sh`
- `git diff --check`
