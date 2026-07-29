# Enterprise control-plane acceptance ledger

This ledger reconciles the acceptance contract in
`docs/superpowers/specs/2026-07-14-enterprise-operator-control-plane-design.md` section 20 with the primary ownership
map in `docs/superpowers/plans/2026-07-14-enterprise-operator-control-plane-program.md` section 2.1 and the PR-083
gate in `docs/superpowers/plans/2026-07-14-control-plane-operator-adoption.md` Task 5.

`community-evidence-retained` is permitted only when a tracked,
checksummed `distr.control-plane-acceptance-evidence/v1` artifact binds the exact
AC owner, allowed proof class, source commit, automated-test bytes,
manual/fixture-evidence bytes, and a tracked passed
`distr.control-plane-test-result/v1` report. A test source file or PR summary
alone is not execution evidence. `pending-community-evidence` records that the
community mechanism exists but that complete retained result evidence has not
yet been supplied; the checker rejects that status.

`pending-adopter` means the community mechanism may exist, but the named adopter
task remains the only primary owner of the required real-adopter evidence. A
row may change to `verified-adopter` only with proof class
`adopter-execution` and retained organization, environment, target, campaign,
time range, passed result, source commit, test result, and checksums. No Choice
TP execution evidence is present in this ledger.

The checker must be run from the repository root:

```powershell
node hack/control-plane-acceptance-check.mjs docs/release/enterprise-control-plane-acceptance.md
```

## Evidence artifact contract

Every non-pending row's `Artifact/checksum` value points to a tracked JSON file
and the SHA-256 of its exact bytes. The JSON file uses this shape:

```json
{
  "schema": "distr.control-plane-acceptance-evidence/v1",
  "acceptanceId": "AC-NN",
  "owner": "PR-NNN",
  "proofClass": "community-focused-test",
  "sourceCommit": "full-lowercase-40-character-commit",
  "automatedTest": {
    "path": "repository/relative/test-path",
    "sha256": "sha256:..."
  },
  "manualEvidence": {
    "path": "repository/relative/evidence-path",
    "sha256": "sha256:..."
  },
  "testResult": {
    "path": "repository/relative/tracked-result.json",
    "sha256": "sha256:..."
  }
}
```

The result file uses schema `distr.control-plane-test-result/v1` and records the
same source commit, `passed` status, zero exit code, positive test counts with
zero failures, and start/completion timestamps. Its command is structured as
`runner`, `argv`, and `selectedTestSource`. The runner must match the
machine-readable profile, and the argv must execute the declared test source
directly for Node/Playwright or its exact Go package. An arbitrary command such
as `echo passed` cannot satisfy the contract.

The checker requires the source commit to exist and be an ancestor of `HEAD`,
verifies the test and manual-evidence bytes at that commit, and requires the
result and evidence files to be tracked. Proof classes add separate retained,
tracked, checksummed result contracts:

- `performance-measurement` requires
  `distr.control-plane-performance-result/v1`, measured-live mode, its exact AC
  scenario, finite numeric raw series bound one-to-one to every declared
  threshold, hardware/build/dataset metadata, roadmap minimums, and no missing,
  duplicate, or unexpected metrics. AC-50 requires p95 at or below two seconds
  and p99 at or below five seconds across registry, matrix, comparison, history,
  and campaign list/detail workloads, using page size 100 and bounded
  responses. AC-51 requires the five-run deterministic 100-component plan p95,
  five identical plan checksums, 500-step stable/no-duplicate wave duration and
  repeated order checksum, ten minutes at 100 authenticated events per second
  with acknowledgement p95 and zero lost accepted events. Event evidence must
  match the load harness fields `authentication: authenticated-live` and
  `concurrentAgents: 100`, and retain exactly 100 unique active executor
  identities plus the checksum of their canonical sorted set; a dataset
  inventory of 100 executors is not concurrency evidence. AC-51 also requires
  the 100 MiB bounded-memory log path with retained peak-buffer size and indexed
  first-page limit, zero
  cross-organization records, and a non-policy error rate below one percent.
- `neutral-live-execution` requires
  `distr.control-plane-neutral-live-result/v1`, a passed live Hub stack, two
  separately configured targets using external-executor and reference
  adapters, distinct executors and independent observers, and exact A-B-A
  history. The retained v1 field shape stores all actual Component Releases and
  the two Product Releases once in top-level `releaseLineage`, stores actual
  Product Release IDs in `productReleaseHistory`, and stores each target's
  ordered A-to-B then B-to-A plan, execution, and observation evidence in
  `targets[].transitions[]`. Target configurations, plans, executions, and
  observations must retain distinct IDs and canonical checksums. Legacy
  top-level shared plans, per-target release-lineage copies, scalar target
  execution/observation IDs, and `releaseHistory` substitutes are
  non-qualifying; no schema ID or compatibility converter is introduced.
  Cleanup must complete, top-level non-local calls must remain zero,
  `liveStack.started` and `acceptanceEligible` must be true, and both targets
  must finish on A.
- `browser-e2e` requires `distr.control-plane-browser-e2e-result/v1` with a
  passed Playwright run, expected tests greater than zero, every expected test
  passed, and zero unexpected or flaky results.

`verified-adopter` additionally requires `adopterExecution` with the exact
ADOPTER task owner, organization and environment IDs, non-empty target and
campaign IDs, start/completion timestamps, and a passed result. Its tracked,
checksummed `distr.control-plane-adopter-execution-bundle/v1` must cover every
retained target/campaign with exact execution IDs, executor IDs, artifact
digests, independent verified observation IDs, observer IDs, configuration
checksums, and the same source commit. The bundle must reference a tracked,
checksummed `distr.control-plane-adopter-audit-export/v1` whose target,
campaign, execution, and observation ID sets match exactly and whose audit
event checksum is retained. Generic, fixture, simulated, or
documentation-only evidence cannot promote an adopter row.

## Release-level gates not claimed by this ledger

The following PR-083 gates remain pending until their required environment, artifacts, or explicit execution window
is available. A successful ledger check must not be presented as release acceptance or live/staging proof.

| Gate                                                                                                   | Current state         | Required retained evidence                                                                                   |
| ------------------------------------------------------------------------------------------------------ | --------------------- | ------------------------------------------------------------------------------------------------------------ |
| PostgreSQL migration 138 to 162, clean install, down/refusal, restart, and mixed v1/v2 matrix          | `pending-environment` | Database identity, commands, schema checkpoints, exact results, and report checksum                          |
| Full Go, Angular, Playwright, Hub/agent build, Compose E2E, failure, scale, and ten-minute load suites | `pending-environment` | Complete command outputs and generated report checksums; focused test references below do not substitute     |
| Dependency, license, vulnerability, secret, and changed-file adopter-term scans                        | `pending-environment` | Tool versions, inputs, outputs, exceptions, and checksums                                                    |
| Immutable community image, SBOM, provenance, database report, and release sign-off                     | `pending-artifact`    | Image digest, signed provenance/SBOM references, database report, approvers, and immutable sign-off checksum |
| Staging/live target or adopter execution                                                               | `not-authorized`      | Separate approved campaign/preflight; PR-083 does not deploy to an adopter                                   |

## AC-01 through AC-80

The machine-readable owner, path, and proof-class source of truth is
`docs/release/control-plane-acceptance-contract.json`, derived from the
normative program plan. The `Owning PR` column is the single primary evidence
owner even when later PRs rerun a cross-cutting regression. In particular,
PR-073 remains the primary owner of AC-40 and AC-41; PR-076 supplies their
later cancel/status reconciliation regression evidence.

| Acceptance ID | Owning PR    | Automated test                                               | Manual/fixture evidence                                                | Status                       | Artifact/checksum                   |
| ------------- | ------------ | ------------------------------------------------------------ | ---------------------------------------------------------------------- | ---------------------------- | ----------------------------------- |
| `AC-01`       | `ADOPTER-01` | `internal/deploymentregistry/import_test.go`                 | `docs/superpowers/plans/2026-07-14-control-plane-operator-adoption.md` | `pending-adopter`            | `pending-adopter:ADOPTER-01`        |
| `AC-02`       | `ADOPTER-01` | `internal/deploymentregistry/import_test.go`                 | `docs/superpowers/plans/2026-07-14-control-plane-operator-adoption.md` | `pending-adopter`            | `pending-adopter:ADOPTER-01`        |
| `AC-03`       | `PR-057`     | `internal/deploymentregistry/import_test.go`                 | `docs/fork/PR-057_DEPLOYMENT_REGISTRY_IMPORT.md`                       | `pending-community-evidence` | `pending-community-evidence:PR-057` |
| `AC-04`       | `PR-057`     | `internal/deploymentregistry/import_test.go`                 | `docs/fork/PR-057_DEPLOYMENT_REGISTRY_IMPORT.md`                       | `pending-community-evidence` | `pending-community-evidence:PR-057` |
| `AC-05`       | `PR-056`     | `internal/deploymentregistry/validation_test.go`             | `docs/fork/PR-056_CANONICAL_DEPLOYMENT_REGISTRY.md`                    | `pending-community-evidence` | `pending-community-evidence:PR-056` |
| `AC-06`       | `PR-056`     | `internal/deploymentregistry/validation_test.go`             | `docs/fork/PR-056_CANONICAL_DEPLOYMENT_REGISTRY.md`                    | `pending-community-evidence` | `pending-community-evidence:PR-056` |
| `AC-07`       | `PR-056`     | `internal/deploymentregistry/validation_test.go`             | `docs/fork/PR-056_CANONICAL_DEPLOYMENT_REGISTRY.md`                    | `pending-community-evidence` | `pending-community-evidence:PR-056` |
| `AC-08`       | `PR-058`     | `internal/targetconfig/canonical_test.go`                    | `docs/fork/PR-058_TARGET_CONFIG_SNAPSHOTS.md`                          | `pending-community-evidence` | `pending-community-evidence:PR-058` |
| `AC-09`       | `PR-060`     | `internal/releasebundles/release_contract_v2_test.go`        | `docs/fork/PR-060_COMPONENT_RELEASE_CONTRACT_V2.md`                    | `pending-community-evidence` | `pending-community-evidence:PR-060` |
| `AC-10`       | `PR-060`     | `internal/releasebundles/release_contract_v2_test.go`        | `docs/fork/PR-060_COMPONENT_RELEASE_CONTRACT_V2.md`                    | `pending-community-evidence` | `pending-community-evidence:PR-060` |
| `AC-11`       | `PR-058`     | `internal/targetconfig/validation_test.go`                   | `docs/fork/PR-058_TARGET_CONFIG_SNAPSHOTS.md`                          | `pending-community-evidence` | `pending-community-evidence:PR-058` |
| `AC-12`       | `PR-058`     | `internal/targetconfig/canonical_test.go`                    | `docs/fork/PR-058_TARGET_CONFIG_SNAPSHOTS.md`                          | `pending-community-evidence` | `pending-community-evidence:PR-058` |
| `AC-13`       | `PR-061`     | `internal/releasebundles/provenance_test.go`                 | `docs/fork/PR-061_RELEASE_PROVENANCE_BACKFILL.md`                      | `pending-community-evidence` | `pending-community-evidence:PR-061` |
| `AC-14`       | `PR-059`     | `internal/targetconfig/v1_extraction_test.go`                | `docs/fork/PR-059_V1_CONFIG_EXTRACTION.md`                             | `pending-community-evidence` | `pending-community-evidence:PR-059` |
| `AC-15`       | `PR-062`     | `internal/productrelease/graph_test.go`                      | `docs/fork/PR-062_PRODUCT_RELEASE_CAPABILITY_GRAPH.md`                 | `pending-community-evidence` | `pending-community-evidence:PR-062` |
| `AC-16`       | `PR-062`     | `internal/productrelease/graph_test.go`                      | `docs/fork/PR-062_PRODUCT_RELEASE_CAPABILITY_GRAPH.md`                 | `pending-community-evidence` | `pending-community-evidence:PR-062` |
| `AC-17`       | `PR-063`     | `internal/planning/resolver_test.go`                         | `docs/fork/PR-063_TARGET_DEPLOYMENT_PLAN_V2.md`                        | `pending-community-evidence` | `pending-community-evidence:PR-063` |
| `AC-18`       | `PR-062`     | `internal/productrelease/graph_test.go`                      | `docs/fork/PR-062_PRODUCT_RELEASE_CAPABILITY_GRAPH.md`                 | `pending-community-evidence` | `pending-community-evidence:PR-062` |
| `AC-19`       | `PR-063`     | `internal/planning/resolver_test.go`                         | `docs/fork/PR-063_TARGET_DEPLOYMENT_PLAN_V2.md`                        | `pending-community-evidence` | `pending-community-evidence:PR-063` |
| `AC-20`       | `PR-063`     | `internal/planning/resolver_test.go`                         | `docs/fork/PR-063_TARGET_DEPLOYMENT_PLAN_V2.md`                        | `pending-community-evidence` | `pending-community-evidence:PR-063` |
| `AC-21`       | `PR-065`     | `internal/migrationplanning/validation_test.go`              | `docs/fork/PR-065_STRUCTURED_MIGRATION_PLANS.md`                       | `pending-community-evidence` | `pending-community-evidence:PR-065` |
| `AC-22`       | `PR-065`     | `internal/migrationplanning/validation_test.go`              | `docs/fork/PR-065_STRUCTURED_MIGRATION_PLANS.md`                       | `pending-community-evidence` | `pending-community-evidence:PR-065` |
| `AC-23`       | `PR-065`     | `internal/migrationplanning/recovery_test.go`                | `docs/fork/PR-065_STRUCTURED_MIGRATION_PLANS.md`                       | `pending-community-evidence` | `pending-community-evidence:PR-065` |
| `AC-24`       | `PR-065`     | `internal/migrationplanning/recovery_test.go`                | `docs/fork/PR-065_STRUCTURED_MIGRATION_PLANS.md`                       | `pending-community-evidence` | `pending-community-evidence:PR-065` |
| `AC-25`       | `PR-064`     | `internal/planning/changeset_test.go`                        | `docs/fork/PR-064_EXACT_PLAN_CHANGESET.md`                             | `pending-community-evidence` | `pending-community-evidence:PR-064` |
| `AC-26`       | `PR-064`     | `internal/planning/baseline_test.go`                         | `docs/fork/PR-064_EXACT_PLAN_CHANGESET.md`                             | `pending-community-evidence` | `pending-community-evidence:PR-064` |
| `AC-27`       | `PR-063`     | `internal/planning/baseline_test.go`                         | `docs/fork/PR-063_TARGET_DEPLOYMENT_PLAN_V2.md`                        | `pending-community-evidence` | `pending-community-evidence:PR-063` |
| `AC-28`       | `PR-067`     | `internal/governance/policy_test.go`                         | `docs/fork/PR-067_VERSIONED_DEPLOYMENT_POLICIES.md`                    | `pending-community-evidence` | `pending-community-evidence:PR-067` |
| `AC-29`       | `PR-068`     | `internal/governance/approval_test.go`                       | `docs/fork/PR-068_CHECKSUM_BOUND_APPROVALS.md`                         | `pending-community-evidence` | `pending-community-evidence:PR-068` |
| `AC-30`       | `PR-068`     | `internal/governance/approval_test.go`                       | `docs/fork/PR-068_CHECKSUM_BOUND_APPROVALS.md`                         | `pending-community-evidence` | `pending-community-evidence:PR-068` |
| `AC-31`       | `PR-069`     | `internal/scheduling/calendar_test.go`                       | `docs/fork/PR-069_MAINTENANCE_CALENDARS_FREEZES.md`                    | `pending-community-evidence` | `pending-community-evidence:PR-069` |
| `AC-32`       | `PR-070`     | `internal/scheduling/admission_test.go`                      | `docs/fork/PR-070_DEPLOYMENT_ADMISSION_OVERRIDES.md`                   | `pending-community-evidence` | `pending-community-evidence:PR-070` |
| `AC-33`       | `PR-075`     | `internal/db/execution_v2_test.go`                           | `docs/fork/PR-075_FENCED_EXECUTOR_PROTOCOL_V2.md`                      | `pending-community-evidence` | `pending-community-evidence:PR-075` |
| `AC-34`       | `PR-075`     | `internal/db/execution_v2_test.go`                           | `docs/fork/PR-075_FENCED_EXECUTOR_PROTOCOL_V2.md`                      | `pending-community-evidence` | `pending-community-evidence:PR-075` |
| `AC-35`       | `PR-075`     | `internal/db/execution_v2_test.go`                           | `docs/fork/PR-075_FENCED_EXECUTOR_PROTOCOL_V2.md`                      | `pending-community-evidence` | `pending-community-evidence:PR-075` |
| `AC-36`       | `PR-072`     | `internal/campaigns/state_machine_test.go`                   | `docs/fork/PR-072_CAMPAIGN_SCHEDULER_THRESHOLDS.md`                    | `pending-community-evidence` | `pending-community-evidence:PR-072` |
| `AC-37`       | `PR-072`     | `internal/campaigns/state_machine_test.go`                   | `docs/fork/PR-072_CAMPAIGN_SCHEDULER_THRESHOLDS.md`                    | `pending-community-evidence` | `pending-community-evidence:PR-072` |
| `AC-38`       | `PR-072`     | `internal/campaigns/thresholds_test.go`                      | `docs/fork/PR-072_CAMPAIGN_SCHEDULER_THRESHOLDS.md`                    | `pending-community-evidence` | `pending-community-evidence:PR-072` |
| `AC-39`       | `PR-073`     | `internal/campaigns/controls_test.go`                        | `docs/fork/PR-073_CAMPAIGN_OPERATIONAL_CONTROLS.md`                    | `pending-community-evidence` | `pending-community-evidence:PR-073` |
| `AC-40`       | `PR-073`     | `internal/db/execution_v2_recovery_test.go`                  | `docs/fork/PR-076_EXECUTION_CANCEL_STATUS_RECONCILIATION.md`           | `pending-community-evidence` | `pending-community-evidence:PR-073` |
| `AC-41`       | `PR-073`     | `internal/db/execution_v2_recovery_test.go`                  | `docs/fork/PR-076_EXECUTION_CANCEL_STATUS_RECONCILIATION.md`           | `pending-community-evidence` | `pending-community-evidence:PR-073` |
| `AC-42`       | `PR-071`     | `internal/campaigns/canonical_test.go`                       | `docs/fork/PR-071_IMMUTABLE_CAMPAIGN_REVISIONS.md`                     | `pending-community-evidence` | `pending-community-evidence:PR-071` |
| `AC-43`       | `PR-077`     | `internal/reconciliation/drift_test.go`                      | `docs/fork/PR-077_DESIRED_OBSERVED_RECONCILIATION.md`                  | `pending-community-evidence` | `pending-community-evidence:PR-077` |
| `AC-44`       | `PR-077`     | `internal/reconciliation/drift_test.go`                      | `docs/fork/PR-077_DESIRED_OBSERVED_RECONCILIATION.md`                  | `pending-community-evidence` | `pending-community-evidence:PR-077` |
| `AC-45`       | `PR-077`     | `internal/desiredstate/state_test.go`                        | `docs/fork/PR-077_DESIRED_OBSERVED_RECONCILIATION.md`                  | `pending-community-evidence` | `pending-community-evidence:PR-077` |
| `AC-46`       | `PR-078`     | `internal/auditexport/bundle_test.go`                        | `docs/fork/PR-078_CONTROL_PLANE_AUDIT_EXPORT.md`                       | `pending-community-evidence` | `pending-community-evidence:PR-078` |
| `AC-47`       | `PR-078`     | `internal/auditexport/worker_test.go`                        | `docs/fork/PR-078_CONTROL_PLANE_AUDIT_EXPORT.md`                       | `pending-community-evidence` | `pending-community-evidence:PR-078` |
| `AC-48`       | `ADOPTER-06` | `internal/retirement/apply_test.go`                          | `docs/superpowers/plans/2026-07-14-control-plane-operator-adoption.md` | `pending-adopter`            | `pending-adopter:ADOPTER-06`        |
| `AC-49`       | `ADOPTER-06` | `internal/retirement/apply_test.go`                          | `docs/superpowers/plans/2026-07-14-control-plane-operator-adoption.md` | `pending-adopter`            | `pending-adopter:ADOPTER-06`        |
| `AC-50`       | `PR-081`     | `hack/control-plane-scale-harness.test.mjs`                  | `docs/fork/PR-081_NEUTRAL_CONTROL_PLANE_PROOF.md`                      | `pending-community-evidence` | `pending-community-evidence:PR-081` |
| `AC-51`       | `PR-081`     | `hack/control-plane-load-test.test.mjs`                      | `docs/fork/PR-081_NEUTRAL_CONTROL_PLANE_PROOF.md`                      | `pending-community-evidence` | `pending-community-evidence:PR-081` |
| `AC-52`       | `ADOPTER-05` | `hack/control-plane-failure-matrix.test.mjs`                 | `docs/superpowers/plans/2026-07-14-control-plane-operator-adoption.md` | `pending-adopter`            | `pending-adopter:ADOPTER-05`        |
| `AC-53`       | `PR-081`     | `examples/control-plane-e2e/reference-executor/main_test.go` | `docs/fork/PR-081_NEUTRAL_CONTROL_PLANE_PROOF.md`                      | `pending-community-evidence` | `pending-community-evidence:PR-081` |
| `AC-54`       | `ADOPTER-05` | `examples/control-plane-e2e/reference-executor/main_test.go` | `docs/superpowers/plans/2026-07-14-control-plane-operator-adoption.md` | `pending-adopter`            | `pending-adopter:ADOPTER-05`        |
| `AC-55`       | `ADOPTER-02` | `internal/releasebundles/provenance_test.go`                 | `docs/superpowers/plans/2026-07-14-control-plane-operator-adoption.md` | `pending-adopter`            | `pending-adopter:ADOPTER-02`        |
| `AC-56`       | `PR-064`     | `internal/planning/changeset_test.go`                        | `docs/fork/PR-064_EXACT_PLAN_CHANGESET.md`                             | `pending-community-evidence` | `pending-community-evidence:PR-064` |
| `AC-57`       | `PR-077`     | `internal/observation/gate_test.go`                          | `docs/fork/PR-077_DESIRED_OBSERVED_RECONCILIATION.md`                  | `pending-community-evidence` | `pending-community-evidence:PR-077` |
| `AC-58`       | `PR-075`     | `internal/db/execution_v2_test.go`                           | `docs/fork/PR-075_FENCED_EXECUTOR_PROTOCOL_V2.md`                      | `pending-community-evidence` | `pending-community-evidence:PR-075` |
| `AC-59`       | `PR-058`     | `internal/targetconfig/validation_test.go`                   | `docs/fork/PR-058_TARGET_CONFIG_SNAPSHOTS.md`                          | `pending-community-evidence` | `pending-community-evidence:PR-058` |
| `AC-60`       | `PR-065`     | `internal/migrationplanning/validation_test.go`              | `docs/fork/PR-065_STRUCTURED_MIGRATION_PLANS.md`                       | `pending-community-evidence` | `pending-community-evidence:PR-065` |
| `AC-61`       | `PR-065`     | `internal/migrationplanning/recovery_test.go`                | `docs/fork/PR-065_STRUCTURED_MIGRATION_PLANS.md`                       | `pending-community-evidence` | `pending-community-evidence:PR-065` |
| `AC-62`       | `PR-071`     | `internal/campaigns/canonical_test.go`                       | `docs/fork/PR-071_IMMUTABLE_CAMPAIGN_REVISIONS.md`                     | `pending-community-evidence` | `pending-community-evidence:PR-071` |
| `AC-63`       | `PR-080`     | `frontend/ui/e2e/control-plane.spec.ts`                      | `docs/fork/PR-080_OPERATOR_CONTROL_ROOM_UI.md`                         | `pending-community-evidence` | `pending-community-evidence:PR-080` |
| `AC-64`       | `ADOPTER-06` | `internal/retirement/apply_test.go`                          | `docs/superpowers/plans/2026-07-14-control-plane-operator-adoption.md` | `pending-adopter`            | `pending-adopter:ADOPTER-06`        |
| `AC-65`       | `PR-061`     | `internal/releasebundles/provenance_test.go`                 | `docs/fork/PR-061_RELEASE_PROVENANCE_BACKFILL.md`                      | `pending-community-evidence` | `pending-community-evidence:PR-061` |
| `AC-66`       | `PR-072`     | `internal/campaigns/thresholds_test.go`                      | `docs/fork/PR-072_CAMPAIGN_SCHEDULER_THRESHOLDS.md`                    | `pending-community-evidence` | `pending-community-evidence:PR-072` |
| `AC-67`       | `PR-067`     | `internal/governance/policy_test.go`                         | `docs/fork/PR-067_VERSIONED_DEPLOYMENT_POLICIES.md`                    | `pending-community-evidence` | `pending-community-evidence:PR-067` |
| `AC-68`       | `PR-059`     | `internal/targetconfig/v1_extraction_test.go`                | `docs/fork/PR-059_V1_CONFIG_EXTRACTION.md`                             | `pending-community-evidence` | `pending-community-evidence:PR-059` |
| `AC-69`       | `PR-075`     | `internal/db/execution_v2_recovery_test.go`                  | `docs/fork/PR-075_FENCED_EXECUTOR_PROTOCOL_V2.md`                      | `pending-community-evidence` | `pending-community-evidence:PR-075` |
| `AC-70`       | `PR-076`     | `internal/db/execution_v2_recovery_test.go`                  | `docs/fork/PR-076_EXECUTION_CANCEL_STATUS_RECONCILIATION.md`           | `pending-community-evidence` | `pending-community-evidence:PR-076` |
| `AC-71`       | `PR-070`     | `internal/scheduling/admission_test.go`                      | `docs/fork/PR-070_DEPLOYMENT_ADMISSION_OVERRIDES.md`                   | `pending-community-evidence` | `pending-community-evidence:PR-070` |
| `AC-72`       | `PR-077`     | `internal/desiredstate/state_test.go`                        | `docs/fork/PR-077_DESIRED_OBSERVED_RECONCILIATION.md`                  | `pending-community-evidence` | `pending-community-evidence:PR-077` |
| `AC-73`       | `PR-077`     | `internal/observation/ingest_test.go`                        | `docs/fork/PR-077_DESIRED_OBSERVED_RECONCILIATION.md`                  | `pending-community-evidence` | `pending-community-evidence:PR-077` |
| `AC-74`       | `PR-069`     | `internal/scheduling/calendar_test.go`                       | `docs/fork/PR-069_MAINTENANCE_CALENDARS_FREEZES.md`                    | `pending-community-evidence` | `pending-community-evidence:PR-069` |
| `AC-75`       | `PR-068`     | `internal/governance/approval_test.go`                       | `docs/fork/PR-068_CHECKSUM_BOUND_APPROVALS.md`                         | `pending-community-evidence` | `pending-community-evidence:PR-068` |
| `AC-76`       | `PR-074`     | `internal/adapterresolution/resolver_test.go`                | `docs/fork/PR-074_VERSIONED_ADAPTER_RESOLUTION.md`                     | `pending-community-evidence` | `pending-community-evidence:PR-074` |
| `AC-77`       | `PR-065`     | `internal/migrationplanning/recovery_test.go`                | `docs/fork/PR-065_STRUCTURED_MIGRATION_PLANS.md`                       | `pending-community-evidence` | `pending-community-evidence:PR-065` |
| `AC-78`       | `PR-062`     | `internal/productrelease/graph_test.go`                      | `docs/fork/PR-062_PRODUCT_RELEASE_CAPABILITY_GRAPH.md`                 | `pending-community-evidence` | `pending-community-evidence:PR-062` |
| `AC-79`       | `ADOPTER-06` | `internal/retirement/apply_test.go`                          | `docs/superpowers/plans/2026-07-14-control-plane-operator-adoption.md` | `pending-adopter`            | `pending-adopter:ADOPTER-06`        |
| `AC-80`       | `PR-071`     | `internal/campaigns/scheduler_test.go`                       | `docs/fork/PR-071_IMMUTABLE_CAMPAIGN_REVISIONS.md`                     | `pending-community-evidence` | `pending-community-evidence:PR-071` |
