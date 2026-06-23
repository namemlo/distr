# ADR-0048: Config as Code Foundation

## Status

Accepted

## Context

The fork roadmap calls for declarative configuration for deployment processes, channels, lifecycles, non-secret variable definitions, step-template references, and runbooks. PR-048 is the foundation only: it validates documents and records whether a resource is managed by the database UI/API or by a future Git source.

## Decision

Add the experimental `config_as_code` feature flag and a schema-versioned validation package for `distr.sh/v1alpha1` YAML/JSON envelopes. Validation is strict: unsupported versions and kinds, unknown fields, duplicate YAML keys, unsafe paths, excessive size/depth, YAML aliases/anchors, and plaintext secret values are rejected with path-aware errors.

Add `ConfigAsCodeAuthority` persistence with two states:

```text
DATABASE_MANAGED -> GIT_MANAGED
GIT_MANAGED -> DATABASE_MANAGED
```

Existing resources without authority rows behave as `DATABASE_MANAGED`. Git-managed resources remain readable, but server-side mutation guards return `409 Conflict` before normal update/delete/revision-import mutations proceed. Authority changes are org-scoped, transaction-guarded, and audited with non-secret metadata only.

## Schema Policy

`distr.sh/v1alpha1` is additive only within this PR. Future schema versions must be explicitly registered and must not silently downgrade to older semantics. Documents with unsupported versions fail validation. Downgrade behavior is fail-closed: older Hubs reject newer versions rather than applying partial configuration.

## Security

Git-managed documents must not contain plaintext passwords, tokens, private keys, connection strings, or secret defaults. They may contain references such as `secretRef`, `accountRef`, and `certificateRef` where the schema permits. Validation errors, audit records, and logs must not echo secret-like values.

## Consequences

- PR-048 does not clone Git repositories, store repository credentials, run webhooks, poll, import, apply, export, or reconcile resources.
- The validation API is side-effect free.
- UI code can display authority and disable local edits, but backend guards remain authoritative.
- Authority records are generic by resource kind and resource ID, so resource deletion paths must explicitly remove authority rows.
