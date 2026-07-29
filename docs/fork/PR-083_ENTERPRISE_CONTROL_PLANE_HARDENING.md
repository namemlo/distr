# PR-083 — Enterprise Control-Plane Hardening

## Scope

PR-083 consolidates the migrations and contracts through PR-082 into one
release, verification, operations, and cutover boundary. It adds no new
deployment abstraction and no adopter-specific adapter, inventory, credential,
host, pipeline, or target identity.

The release remains community-neutral and default-off until the complete
automated and neutral evidence gate is accepted. Adopter inventory and client
mutation are later, separately approved work.

## Standard workflow

The accepted operating model is:

```text
onboard and classify
  -> CI build-once/publish-only Component Releases
  -> Product Release capability DAG
  -> immutable target config
  -> last-healthy-observed baseline and accumulated changelog
  -> target plan/preflight
  -> checksum-bound approvals and campaign
  -> signed/fenced execution
  -> independent observation
  -> desired-state promotion or reconciliation
  -> correlated audit evidence
```

CI never deploys a target. Component versions and platform digests are
immutable. Target change comparison starts from the same placement's last
healthy independently observed state, includes skipped releases, and identifies
bootstrap explicitly when no baseline exists.

Dependencies resolve as versioned capabilities. A provider is included,
pinned-existing, shared, approved-external, or explicitly disabled by frozen
policy/config. Unresolved requirements block publication. Provider deployment,
migration, and health precede consumers in the frozen DAG; component-name
scripts do not create coupled deployments.

## Operations documentation

- [Enterprise client deployment](../operations/enterprise-client-deployment.md)
- [Control-plane backup and restore](../operations/control-plane-backup-restore.md)
- [v1/v2 rollback](../operations/control-plane-v1-v2-rollback.md)
- [Campaign incident response](../operations/control-plane-campaign-incident.md)
- [Operator control-plane API](../api/operator-control-plane-api.md)
- [Sample domain retirement](../operations/sample-domain-retirement.md)

These documents cover versions/changelogs, providers, approvals, campaigns,
execution, observations, reconciliation, previous state, backup/recovery,
incidents, flags, and rollback without embedding adopter details.

## Release evidence gate

The PR-083 gate requires:

- clean install and ordered migration 138 through 162;
- upgrade, safe down/refusal, interrupted checkpoint restart, v1-only,
  mixed-v1/v2, and v2-history-flags-off paths;
- PostgreSQL 16.14 and 18.4 coverage;
- full Go, Angular, Playwright, Hub/agent build, Compose, failure, scale/load,
  migration, lint, format, dependency, license, vulnerability, secret, and
  adopter-term checks;
- an AC-01 through AC-80 ledger with one primary evidence owner per row;
- immutable Hub image digest, OCI source revision, SBOM/provenance references,
  migration report, and checksummed handoff; and
- no unresolved Critical or Important review finding.

Evidence must bind the exact source commit and dirty state, toolchain,
parameters, database/schema, environment/mode, raw results, and artifact
checksums. Fixture, local, isolated restore, staging, and production evidence
must remain distinctly labeled.

The migration matrix accepts only the certified PostgreSQL versions `16.14`
and `18.4` through `-ExpectedPostgresVersion` and verifies the observed server
version before accepting execution evidence. Its report retains complete
redacted command output and checksums, plus explicit report-integrity metadata.
PlanOnly remains planning evidence only.

The current automated downgrade scope is one schema step from the configured
target followed by pinned refusal-contract tests. The checkpoint coverage is
idempotency and cursor-resume testing. Process interruption/restart and binary
rollback are named as not executed rather than being inferred from those
checks.

The neutral reference scale fixture models 25 client organizations with at
least 25 placements and 25 distinct service bindings per organization, backed
by a reusable 100-component catalog. It retains the 1,000-target, 649-placement,
100-agent, and 500-step workload floors plus a separate organization-isolation
sentinel. Fixture success proves deterministic contract and isolation behavior;
it is not a live database, staging, or adopter result.

## Flags and compatibility

- `operator_control_plane_v2` remains the default-off umbrella.
- `executor_protocol_v2` is effective only with the umbrella flag.
- scoped organization/environment enrollment remains mandatory.
- disabling v2 prevents new controlled admission while preserving untouched v1
  behavior and retained v2 history.
- protocol version is frozen in each plan; in-flight work is never converted.
- binary rollback requires explicit compatibility with the current schema.
- schema downgrade must pass refusal checks without deleting evidence.
- previous-state target deployment is a new immutable plan.
- database restore is a separate approved recovery operation.

## Security and audit

Execution uses immutable digests, config checksums, scoped short-lived
credentials, signed intent, and fences. Secret values do not enter plans,
events, logs, API examples, or evidence bundles.

Application audit events are append-only through the sample-retirement
boundary. External export checkpoints advance only after ordered delivery;
failed attempts remain visible and source events remain retryable.

## API, database, UI, and agent impact

- Database: no PR-083 migration; it certifies the integrated chain through
  migration 162.
- API: no new PR-083 route family; it documents and verifies the integrated
  operator, executor, observer, audit, and retirement contracts.
- UI: no new PR-083 workspace; it verifies the integrated control-room routes
  and legacy deployment compatibility.
- Agent/executor: no silent protocol change; v1 and v2 retain their versioned
  delivery and retry contracts.

## Proof status

This document defines the required release gate and operating contract. It does
not claim a completed live database restore, release publication, staging
deployment, production deployment, adopter inventory, or client mutation.
