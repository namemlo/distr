# Community Release Architecture Overview

The community release keeps Distr's existing customer deployment foundation and adds release-management
primitives behind experimental flags.

## Core Runtime Boundaries

- Hub owns desired state, API validation, authorization, planning, task records, and audit metadata.
- PostgreSQL stores release, process, variable, plan, task, lease, compatibility, and authority records.
- Agents poll for scoped work, advertise capabilities, execute typed actions, and send redacted step events.
- The Angular UI is a client of documented API routes and does not bypass server authorization.
- Config as Code validates documents and authority state but does not sync or apply repository content in this
  release.

## Release Execution Path

1. CI builds once and publishes immutable Component Release artifacts, change metadata, provenance, SBOM, and test
   references; CI does not deploy a target.
2. A Product Release freezes capability providers, consumers, migration order, and exact component digests.
3. The target resolves an immutable configuration snapshot and the last healthy independently observed baseline.
4. The planner produces a target-bound plan, exact change set, blockers, checksum, and protocol version.
5. Scoped policy, maintenance-window, and checksum-bound approval decisions gate admission.
6. A campaign freezes membership and waves; tasks use signed intents, fences, leases, and versioned adapters.
7. Independent observations confirm the running digest and health before desired-state promotion.
8. Drift or uncertain execution remains visible and enters reconciliation; previous-state deployment creates a new
   immutable plan.
9. Correlated audit, export, checkpoint, and post-deployment evidence remains queryable under its retention policy.

The operator surfaces and exact route families are summarized in the
[operator control-plane API](../api/operator-control-plane-api.md). The neutral two-target reference workflow is
documented in [PR-081](../fork/PR-081_NEUTRAL_CONTROL_PLANE_PROOF.md); its deterministic fixture evidence does not
claim a live Hub, Docker Compose, staging, or production result.

## Release and Recovery Boundary

`operator_control_plane_v2` is the default-off umbrella for new operator admission.
`executor_protocol_v2` is effective only with the umbrella flag. Disabling v2 preserves untouched v1 behavior and
retained v2 history; it does not rewrite in-flight plans or authorize database downgrade.

Migration 162 adds exact-ID sample-retirement jobs, durable checkpoints, and audit-subject tombstones. Retirement is
an explicit operator workflow with backup and isolated restore proof, immutable preview, approval-bound apply, and
verification. It is not part of ordinary release execution and never acts as an audit-retention purge. See
[PR-082](../fork/PR-082_SAMPLE_DOMAIN_RETIREMENT.md).

The integrated PR-083 gate requires a checksummed migration report, immutable Hub image digest, SBOM and signed
provenance references, backup/restore evidence, and post-deployment verification. Until the environment, Docker,
security-scan, artifact, and deployment rows in the
[release-readiness package](../release/community-release-readiness.md#integrated-control-plane-release-gate) have
retained evidence, the control plane remains default-off and is not release-certified.

## Non-Goals

- No provider-specific orchestration is embedded in core.
- No arbitrary script console is added.
- No secret values are stored in demo fixtures or Config as Code documents.
- No existing direct deployment behavior is replaced.
- No fixture, acceptance-ledger, or local fallback result is relabeled as live release evidence.
