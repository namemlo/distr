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

## Operator Control Plane

The [operator control-plane API guide](operator-control-plane-api.md) is the human-readable index for the integrated
PR-055 through PR-082 route families. The generated OpenAPI document remains authoritative for request and response
shapes.

- Registry import and enrollment: exact organization-scoped inventory, classification, and enrollment.
- Component and Product Releases: immutable source/build identity, capability graph, migration DAG, provenance,
  SBOM, signatures, tests, and accumulated change metadata.
- Target configuration, plans, approvals, calendars, and overrides: immutable checksum-bound planning and admission.
- Campaigns, executions, executor callbacks, observations, desired state, and reconciliation: cursor-paged operator
  reads plus scoped mutation routes.
- Audit list, correlated detail, export sinks, and export status: retained source events remain retryable when an
  external export fails.
- Sample retirement: append-only ownership/recovery evidence registration under
  `/api/v1/sample-retirement-evidence`, followed by exact-ID preview, detail, approval-bound apply, checkpoint
  restart, and verification under `/api/v1/sample-retirements`.

`operator_control_plane_v2` is the default-off umbrella. `executor_protocol_v2` is effective only when the umbrella
is enabled. Both require scoped authorization/enrollment; disabling them blocks new v2 admission without deleting
retained history or silently executing through v1.

Sample retirement is backed by migration 162. Apply requires `previewChecksum`, `approvalId`, and
`approvalChecksum`; it is not a generic delete or retention endpoint. Application audit events remain retained and
removed subjects resolve through audit tombstones. See the
[sample-retirement contract](../fork/PR-082_SAMPLE_DOMAIN_RETIREMENT.md).

## Governance

- Approvals and manual intervention APIs.
- Deployment-plan admission and checksum-bound emergency-override APIs.
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
set -e
SCAN_BASE="${SCAN_BASE:?set SCAN_BASE to the reviewed ancestor commit or ref}"
node --test hack/control-plane-adopter-term-scan.test.mjs
node hack/control-plane-adopter-term-scan.mjs --base "$SCAN_BASE"
curl -sf http://localhost:8080/docs/openapi.json -o /tmp/distr-openapi.json
node hack/pr050-validate-release-hardening.mjs
node hack/control-plane-acceptance-check.mjs docs/release/enterprise-control-plane-acceptance.md
```

Run the validation block in one fail-fast shell so a nonzero scanner result
stops the chain. API examples must use placeholder credentials and secret
references only. The acceptance-ledger check validates evidence ownership and
links; it does not satisfy the pending migration, Docker, security, artifact,
or post-deployment gates in the
[release-readiness package](../release/community-release-readiness.md).
