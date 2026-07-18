# Community Release API Index

This index points reviewers to the API surfaces introduced by the roadmap. The generated OpenAPI document
remains authoritative for route shapes.

## Release and Promotion

- Release bundles: draft CRUD, validation, publish, block, archive.
- Component Release Contract v2: target-neutral source/build identity, immutable manifest and platform digests,
  capabilities, migrations, changes, and distinct provenance/SBOM/signature/test references.
- Component publication provenance: offline signed in-toto/Sigstore verification against caller-supplied frozen
  roots and policy, with exact subject digest, Component Release repository/commit and build invocation/builder
  bindings, predicate, canonical source, build type, and external parameter checks.
- Lifecycle eligibility: release/environment explanation endpoint.
- Deployment processes: process CRUD and immutable revision endpoints.
- Deployment plans: preview, checksum, export, and task creation surfaces.

The provenance verifier does not add a route family or fetch network trust material. Existing release-bundle
responses carry additive kind/schema and immutable checksum/digest facts. Only a bounded accepted verification
receipt, including the exact verified source repository/commit and builder/invocation ID, is persisted; an
evidence reference alone is not treated as verified. Preflight compares those persisted values to the release
contract. The existing publish route accepts an optional `provenance` object containing the frozen policy and
embedded bundles; it remains optional for v1 and is required for Component Release v2.

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
- Dry-run-by-default, checkpointed Release Contract v1-to-v2 backfill CLI with stable lineage, explicit
  ambiguous-row blockers, immutable evidence-document/selected-row bindings, one bounded apply batch per
  invocation, resumable `nextCursor`, and separate `wouldDerive` versus persisted `derived` counts.

The release backfill is operator tooling, not a public API. It never rewrites v1 IDs, contract/canonical bytes,
checksums, statuses, or historical references, and it does not invent missing provenance. Publication and future
plan preflight remain fail-closed.

## Validation

Before release, verify:

```shell
curl -sf http://localhost:8080/docs/openapi.json -o /tmp/distr-openapi.json
node hack/pr050-validate-release-hardening.mjs
```

API examples must use placeholder credentials and secret references only.
