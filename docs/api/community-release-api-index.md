# Community Release API Index

This index points reviewers to the API surfaces introduced by the roadmap. The generated OpenAPI document
remains authoritative for route shapes.

## Release and Promotion

- Release bundles: draft CRUD, validation, publish, block, archive.
- Lifecycle eligibility: release/environment explanation endpoint.
- Deployment processes: process CRUD and immutable revision endpoints.
- Deployment plans: preview, checksum, export, and task creation surfaces.

## Execution and Agents

- Task queue and task state APIs.
- Agent capability advertisement.
- Agent task leases and heartbeats.
- Structured step events and log chunk ingestion.

## Governance

- Approvals and manual intervention APIs.
- Tag sets, rollout groups, freezes, subscriptions, retention previews, and runbooks.
- Expanded RBAC permission checks on mutation paths.

## Operations

- Deployment timeline list, compare, and deploy-previous-release planning.
- Observability dashboard catalog and optional correlation metadata.
- Config as Code validation and authority APIs.
- Legacy deployment compatibility backfill CLI.

## Validation

Before release, verify:

```shell
curl -sf http://localhost:8080/docs/openapi.json -o /tmp/distr-openapi.json
node hack/pr050-validate-release-hardening.mjs
```

API examples must use placeholder credentials and secret references only.
