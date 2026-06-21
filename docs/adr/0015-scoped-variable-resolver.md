# ADR-0015: Scoped Variable Resolver

## Status

Accepted for PR-015.

## Context

PR-014 introduced organization-scoped Variable Sets and typed Variables, but intentionally excluded scoped resolution, precedence, preview APIs, snapshots, and drift behavior. The roadmap next requires a deterministic scoped-variable resolver with an explanation trace while preserving secret safety and existing deployment behavior.

The repository does not yet have a Runbook domain model. Deployment Process steps exist and expose stable step keys, and target tags exist as step/request data rather than as a standalone table.

## Decision

Add `VariableScopedValue` rows as children of Variables. Scoped values are organization-scoped and may reference Applications, Channels, Environments, DeploymentTargets, CustomerOrganizations, target tags, and process step keys according to the allowed roadmap precedence shapes.

Use a pure resolver package for deterministic precedence and explanation traces. The database repository loads Variable Sets and scoped values, validates same-organization references, prepares safe prompted Secret references, and delegates matching to the resolver.

Expose a read-only, feature-flagged preview endpoint:

```http
POST /api/v1/variables/resolve-preview
```

Return secret references as redacted metadata only. Do not return secret plaintext in Variable Set CRUD, preview responses, traces, or logs.

Extend the existing Angular Variable Sets page instead of creating a separate feature page. Optional feature-gated lookup data such as Channels and Environments may fail soft to empty selector lists so the page remains available when only `scoped_variables_v2` is enabled.

## Consequences

- Variable resolution behavior can be tested independently of persistence and HTTP.
- API callers receive a deterministic selected source and trace for every resolved or unresolved variable.
- Cross-organization scoped references return not found through repository validation and composite foreign keys.
- Duplicate scoped value shapes for one Variable are rejected before persistence and by the database.
- Existing Environment, Lifecycle, Channel, Release Bundle, Deployment Process, deployment target, deployment, and agent behavior remains unchanged.
- Later PRs can add snapshots, drift, deployment-plan integration, prompted-value collection workflows, and runbook bindings without changing the PR-015 resolver precedence contract.

## Alternatives Considered

Embedding scoped values as JSON inside `Variable` was rejected because it would weaken organization-scoped foreign keys and duplicate-scope enforcement.

Resolving variables only inside deployment planning was rejected because the roadmap requires a preview API and explanation trace before deployment planning changes.

Adding Runbook foreign keys in PR-015 was rejected because Runbooks are not modeled in this repository yet and would start later roadmap scope.

## Validation

PR-015 adds pure resolver tests, API validation tests, mapping tests, handler tests, live PostgreSQL repository and handler integration tests, migration checks, Angular service and component tests, feature-flag checks, and changed-file Unicode scans.
