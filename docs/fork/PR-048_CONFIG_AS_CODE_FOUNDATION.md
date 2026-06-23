# PR-048 - Config as Code Foundation

## Summary

PR-048 adds the first Config as Code foundation behind `config_as_code`. It validates declarative YAML/JSON documents, persists resource authority, prevents Git-managed resources from being mutated through normal database-managed APIs, and adds minimal UI support for authority visibility.

## Included

- Strict validation for `distr.sh/v1alpha1` envelopes, required metadata, kind-specific schemas, typed nested fields, channel rule objects, step-template source references, and variable default/reference semantics.
- Supported kinds: `DeploymentProcess`, `Channel`, `Lifecycle`, `VariableSetDefinition`, `StepTemplateReference`, and `Runbook`.
- Canonical JSON checksums for valid documents, including format-independent numeric normalization.
- Plaintext-secret rejection with redacted validation messages, nested credential-key scanning, and typed non-empty reference fields.
- `POST /api/v1/config-as-code/validate`.
- Org-scoped authority APIs under `/api/v1/config-as-code/authorities`.
- `DATABASE_MANAGED` and `GIT_MANAGED` authority persistence plus non-secret audit events and shared repository-path validation, including traversal, backslash, absolute, and Windows drive/drive-relative rejection.
- Server-side mutation guards for the supported resource families.
- Frontend service/types, feature flag plumbing, and authority badge/read-only controls for deployment processes, channels, lifecycles, variable sets, step templates, and runbooks.

## Out of Scope

- Git cloning or provider integrations.
- Repository credentials, deploy keys, polling, webhooks, or background reconciliation.
- Import/apply/export workflows.
- Branch protection and release-bundle Git commit references.
- Secret resolution from Git-managed documents.
- Agent protocol, planner, task, or deployment execution changes.

## Verification

- `go test -p=1 ./...`
- `NODE_OPTIONS=--max-old-space-size=4096 pnpm test -- --watch=false`
- `NODE_OPTIONS=--max-old-space-size=4096 pnpm build:community`
- `pnpm exec prettier --check ...`
- `git diff --check`

Live PostgreSQL repository tests use `DISTR_TEST_DATABASE_URL`; they skip when that environment variable is unavailable. Authority tests include a row-lock race covering a Git authority switch serialized against channel update/delete paths.
