# ADR-0014: Variable Types and Sets

## Status

Accepted for PR-014.

## Context

The roadmap requires a generic Variable Set model before scoped variable resolution, snapshots, deployment planning, or execution behavior can be added. Existing Release Bundle and Deployment Process models must remain unchanged, and secrets must not be copied into new plaintext storage.

Variable Sets need to be organization-scoped, reusable across Applications, and safe to expose through administration APIs and UI while the feature remains experimental.

## Decision

Add a `scoped_variables_v2` experimental feature flag.

Add `VariableSet`, `VariableSetApplication`, and `Variable` tables. Variable Sets are organization-scoped and have unique names within an organization. Applications are linked through an organization-scoped join table. Variables belong to one Variable Set and have unique keys within that set.

Support these variable types:

- string
- number
- boolean
- JSON
- secret reference
- account reference
- certificate reference

Store non-secret defaults as typed JSON. Store secret references by Secret ID only and derive the safe display name from the referenced organization-level Secret. Do not store or return secret plaintext through Variable Set APIs.

Account and certificate references are metadata-only references in PR-014. They require a reference ID and reference name but do not resolve provider credentials.

Expose feature-flagged organization-scoped CRUD endpoints under:

```http
/api/v1/variable-sets
```

Add an Angular vendor-admin UI for Variable Set CRUD, Application selection, typed variable editing, and safe Secret reference selection.

## Consequences

- Variable Sets can be modeled and administered before scoped resolution exists.
- Cross-organization Application and Secret references return not found.
- Referenced organization Secrets cannot be deleted while Variable rows point to them.
- Existing Environment, Lifecycle, Channel, Release Bundle, Deployment Process, deployment, and agent behavior remains unchanged.
- Later roadmap PRs can add scoped resolution, snapshots, prompted values, deployment planning, and execution without changing the PR-014 API contract for basic Variable Set CRUD.

## Alternatives Considered

Storing all variable defaults as text was rejected because it would defer type validation and make later resolution ambiguous.

Embedding secret values in Variable rows was rejected because it would duplicate sensitive data and bypass existing Secret ownership and deletion controls.

Binding Variable Sets directly to Deployment Processes in PR-014 was rejected because the roadmap assigns resolution and snapshot behavior to later PRs.

## Validation

PR-014 adds API validation tests, mapping tests, handler tests, live PostgreSQL repository and handler integration tests, migration checks, Angular service and component tests, feature-flag tests, and changed-file Unicode scans.
