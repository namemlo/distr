# PR-016 Variable Snapshots and Drift

## Scope

PR-016 adds immutable variable snapshots for published Release Bundles and a read-only deployment configuration drift view.

Included:

- Variable snapshot creation during Release Bundle publication.
- Snapshot rows linked to organization, Release Bundle, Application, and Channel.
- Snapshot values for resolved Variable Sets associated with the Release Bundle Application.
- Canonical snapshot payload and checksum storage.
- Secret and reference variables represented as redacted metadata only.
- Feature-flagged snapshot read API.
- Feature-flagged deployment configuration drift API.
- Angular deployment detail drift panel with loading, no-drift, API-error, and drift-category states.

Excluded:

- Deployment planning.
- Release promotion execution.
- Approval, retention, notification, runbook, or task queue behavior.
- Action registry or built-in action execution.
- Agent protocol or agent execution changes.
- Plaintext secret value storage in snapshots or drift responses.

## Feature Flags

Release Bundle publication keeps its existing `release_bundles` feature flag behavior.

The new read-only snapshot API requires both:

```text
DISTR_EXPERIMENTAL_FEATURE_FLAGS=release_bundles,scoped_variables_v2
```

The deployment configuration drift API and UI require:

```text
DISTR_EXPERIMENTAL_FEATURE_FLAGS=scoped_variables_v2
```

Existing feature flags are preserved.

## Database

Migration `119_variable_snapshots` adds:

- `VariableSnapshot`
- `VariableSnapshotValue`
- nullable `ReleaseBundle.variable_snapshot_id`
- organization/application/channel foreign key constraints between Release Bundles and snapshots
- indexes for snapshot lookup
- a redaction check preventing redacted snapshot values from storing plaintext JSON values

The down migration removes the snapshot link and tables, then recomputes Release Bundle canonical payloads without `variableSnapshotId`.

## API

Snapshot endpoint:

```http
GET /api/v1/variable-snapshots/{variableSnapshotId}
```

Deployment drift endpoint:

```http
GET /api/v1/deployments/{deploymentId}/configuration-drift
```

Both endpoints preserve organization scoping. Malformed UUIDs return not found, and cross-organization reads return not found.

## Drift Comparison

Configuration drift compares the latest deployment revision values against the current resolved Variable schema for the deployment Application and target scope.

The response separates:

- new required variables
- missing optional variables
- removed deployed values
- type changes
- default value changes
- secret/reference changes

Secret-reference drift returns only reference metadata and redaction flags.

## Compatibility

Existing Environment, Lifecycle, Channel, Release Bundle draft/edit, Deployment Process, deployment target, deployment, release-name, and agent behavior is unchanged.

PR-016 adds no deployment planning, promotion, approval, retention, execution, notification, runbook persistence, action registry, or agent behavior.
