# PR-015 Scoped Variable Resolver

## Scope

PR-015 adds scoped values and deterministic preview resolution for the generic Variable Set model introduced in PR-014.

Included:

- Scoped values on Variables.
- Supported scope shapes:
  - application
  - channel
  - environment
  - environment + target tag
  - tenant + environment
  - tenant + environment + channel
  - tenant + environment + deployment target
  - tenant + environment + deployment target + channel + process step
- Deterministic precedence matching the roadmap.
- Prompted preview values as the highest precedence source.
- Explanation trace for selected and candidate matches.
- Feature-flagged `POST /api/v1/variables/resolve-preview`.
- Angular Variable Sets UI support for scoped values, conflict warnings, and resolution preview.

Excluded:

- Variable snapshots.
- Configuration drift APIs or UI.
- Deployment planning or deployment execution failure behavior.
- Release promotion, approvals, retention, notifications, task execution, or agent protocol changes.
- Runbook persistence; runbooks are not yet modeled in this repository.

## Feature Flag

All new API and UI behavior remains guarded by:

```text
DISTR_EXPERIMENTAL_FEATURE_FLAGS=scoped_variables_v2
```

Existing feature flags are preserved. Optional Channel and Environment lookup calls in the UI fail soft to empty selector lists so the Variable Sets page remains usable when only `scoped_variables_v2` is enabled.

## Database

Migration `118_variable_scoped_values` adds `VariableScopedValue`.

The table is scoped through `Variable`, `VariableSet`, and `organization_id`. It stores either:

- a typed JSON value,
- a same-organization Secret reference, or
- metadata-only account/certificate reference data.

The migration adds composite uniqueness constraints needed for same-organization foreign keys to Environment, Channel, DeploymentTarget, CustomerOrganization, and Variable.

Duplicate scoped value shapes for one Variable are rejected with `UNIQUE NULLS NOT DISTINCT`.

## API

Existing Variable Set create/update payloads now accept optional `scopedValues` per variable.

Preview endpoint:

```http
POST /api/v1/variables/resolve-preview
```

Request:

```json
{
  "variableSetIds": ["00000000-0000-0000-0000-000000000000"],
  "scope": {
    "applicationId": "00000000-0000-0000-0000-000000000000",
    "targetTags": ["linux"]
  },
  "promptedValues": [
    {
      "key": "api_url",
      "value": "https://prompted.example"
    }
  ]
}
```

Responses include resolved/unresolved status, selected source, safe value/reference metadata, and trace entries. Secret references are redacted and never expose secret values.

## Precedence

Resolver precedence:

1. Prompted deployment value.
2. Exact tenant + environment + target + channel + step.
3. Exact tenant + environment + target.
4. Exact tenant + environment + channel.
5. Exact tenant + environment.
6. Exact environment + target tag.
7. Exact environment.
8. Channel.
9. Application.
10. Unscoped default.

Required Variables with no matching prompted, scoped, or default value return `unresolved`.

## UI

The feature-flagged Variable Sets page now supports:

- editing scoped values per variable,
- selecting application/channel/environment/tenant/target scope data,
- duplicate scoped-scope conflict warnings,
- previewing resolution for a selected Variable Set and scope,
- displaying selected source, safe non-secret values, redacted references, and trace entries.

## Compatibility

Existing Environment, Lifecycle, Channel, Release Bundle, Deployment Process, deployment target, deployment, release-name, and agent behavior is unchanged.

PR-015 adds no snapshots, drift detection, deployment planning, execution behavior, or agent protocol changes.
