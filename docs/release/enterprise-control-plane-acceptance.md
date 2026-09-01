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
| PostgreSQL migration 138 to 170, clean install, down/refusal, restart, and mixed v1/v2 matrix          | `pending-environment` | Database identity, commands, schema checkpoints, exact results, and report checksum                          |
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
| `AC-03` | `PR-057` | `internal/deploymentregistry/import_test.go` | `docs/fork/PR-057_DEPLOYMENT_REGISTRY_IMPORT.md` | `community-evidence-retained` | `docs/release/evidence/AC-03.json @ sha256:6e8f1c528f71fc28a38b974dd4bd0beb349e940578759eb62d11146497063d9f` |
| `AC-04` | `PR-057` | `internal/deploymentregistry/import_test.go` | `docs/fork/PR-057_DEPLOYMENT_REGISTRY_IMPORT.md` | `community-evidence-retained` | `docs/release/evidence/AC-04.json @ sha256:0a0e0b9edc11e431faf74b32f730877dbee0f03562766662b1b3679a84ce0c75` |
| `AC-05` | `PR-056` | `internal/deploymentregistry/validation_test.go` | `docs/fork/PR-056_CANONICAL_DEPLOYMENT_REGISTRY.md` | `community-evidence-retained` | `docs/release/evidence/AC-05.json @ sha256:c614ebc565cb0197e4fc2df93c95ad1e3a594330e88978f008acff3ecae0ee24` |
| `AC-06` | `PR-056` | `internal/deploymentregistry/validation_test.go` | `docs/fork/PR-056_CANONICAL_DEPLOYMENT_REGISTRY.md` | `community-evidence-retained` | `docs/release/evidence/AC-06.json @ sha256:4f19816ae03f9af7a549a6422511ab8965b43a9fedc38aae02e7f4cd958c499e` |
| `AC-07` | `PR-056` | `internal/deploymentregistry/validation_test.go` | `docs/fork/PR-056_CANONICAL_DEPLOYMENT_REGISTRY.md` | `community-evidence-retained` | `docs/release/evidence/AC-07.json @ sha256:9cba692f327f49bb168a1c9ad3e4cb1eb509bf04fd57303247e9f584187b3718` |
| `AC-08` | `PR-058` | `internal/targetconfig/canonical_test.go` | `docs/fork/PR-058_TARGET_CONFIG_SNAPSHOTS.md` | `community-evidence-retained` | `docs/release/evidence/AC-08.json @ sha256:52501bf9c042d97c942e661f146a57e85ac4fcac2aa71c6c117e7cbf8ba789c4` |
| `AC-09` | `PR-060` | `internal/releasebundles/release_contract_v2_test.go` | `docs/fork/PR-060_COMPONENT_RELEASE_CONTRACT_V2.md` | `community-evidence-retained` | `docs/release/evidence/AC-09.json @ sha256:26793e9cecb2820c327c395fa4c75f61a841f77a7ea7aed5e20470e0caee8e78` |
| `AC-10` | `PR-060` | `internal/releasebundles/release_contract_v2_test.go` | `docs/fork/PR-060_COMPONENT_RELEASE_CONTRACT_V2.md` | `community-evidence-retained` | `docs/release/evidence/AC-10.json @ sha256:da7c6bd155658648abc791c3ffb5abe775aa1fb961c68132b1e355cfcd079944` |
| `AC-11` | `PR-058` | `internal/targetconfig/validation_test.go` | `docs/fork/PR-058_TARGET_CONFIG_SNAPSHOTS.md` | `community-evidence-retained` | `docs/release/evidence/AC-11.json @ sha256:2987d6377c9814f38cf33e592172afbba8fdb40e27d932d2c3de7059b68c8b1e` |
| `AC-12` | `PR-058` | `internal/targetconfig/canonical_test.go` | `docs/fork/PR-058_TARGET_CONFIG_SNAPSHOTS.md` | `community-evidence-retained` | `docs/release/evidence/AC-12.json @ sha256:0fe3e01950f964d2288206d0bd055bf09218aac83dcbed40c67d879f1ea7d660` |
| `AC-13` | `PR-061` | `internal/releasebundles/provenance_test.go` | `docs/fork/PR-061_RELEASE_PROVENANCE_BACKFILL.md` | `community-evidence-retained` | `docs/release/evidence/AC-13.json @ sha256:15b31765802cd406bafa52cf5052477f86fba59b0708e3ee4fcdcdfe5d35a727` |
| `AC-14` | `PR-059` | `internal/targetconfig/v1_extraction_test.go` | `docs/fork/PR-059_V1_CONFIG_EXTRACTION.md` | `community-evidence-retained` | `docs/release/evidence/AC-14.json @ sha256:48922cd70a8a998d5eb9f3905ec99dfd806cefe22afc6b5a55804d8d58669fc1` |
| `AC-15` | `PR-062` | `internal/productrelease/graph_test.go` | `docs/fork/PR-062_PRODUCT_RELEASE_CAPABILITY_GRAPH.md` | `community-evidence-retained` | `docs/release/evidence/AC-15.json @ sha256:e3d7be6b265415b2a71e4ec6d5bced180d095f65a876f645f5e3f7408f5c9212` |
| `AC-16` | `PR-062` | `internal/productrelease/graph_test.go` | `docs/fork/PR-062_PRODUCT_RELEASE_CAPABILITY_GRAPH.md` | `community-evidence-retained` | `docs/release/evidence/AC-16.json @ sha256:a29937ded29df6decebb35e60026de417d9f2efe0c4620a8b8e398293df77933` |
| `AC-17` | `PR-063` | `internal/planning/resolver_test.go` | `docs/fork/PR-063_TARGET_DEPLOYMENT_PLAN_V2.md` | `community-evidence-retained` | `docs/release/evidence/AC-17.json @ sha256:726e9ef5b8646cfe4e559893a7185ee18e52284b7675246258eb403b59265b94` |
| `AC-18` | `PR-062` | `internal/productrelease/graph_test.go` | `docs/fork/PR-062_PRODUCT_RELEASE_CAPABILITY_GRAPH.md` | `community-evidence-retained` | `docs/release/evidence/AC-18.json @ sha256:a52000df4691c351c94ecd842f85697cb4707db2059bfb3707e6bf7882c88cb9` |
| `AC-19` | `PR-063` | `internal/planning/resolver_test.go` | `docs/fork/PR-063_TARGET_DEPLOYMENT_PLAN_V2.md` | `community-evidence-retained` | `docs/release/evidence/AC-19.json @ sha256:032350f45a6356caeb5351c839278d9a26685bd18459dd30aa736e8712a9297f` |
| `AC-20` | `PR-063` | `internal/planning/resolver_test.go` | `docs/fork/PR-063_TARGET_DEPLOYMENT_PLAN_V2.md` | `community-evidence-retained` | `docs/release/evidence/AC-20.json @ sha256:7cb3c2d6bd99a4a681ee2a0d868b4518dbcd4586bf175ca6b2db923814b84bfc` |
| `AC-21` | `PR-065` | `internal/migrationplanning/validation_test.go` | `docs/fork/PR-065_STRUCTURED_MIGRATION_PLANS.md` | `community-evidence-retained` | `docs/release/evidence/AC-21.json @ sha256:c1c7375b05ea58c496ecb08d9e89d12b0b2cb8ff2982d34ec7b000e1cd6b3f00` |
| `AC-22` | `PR-065` | `internal/migrationplanning/validation_test.go` | `docs/fork/PR-065_STRUCTURED_MIGRATION_PLANS.md` | `community-evidence-retained` | `docs/release/evidence/AC-22.json @ sha256:c00add3c1596193dbb0745b44b1eb92c5da7e9f86d8546740e0250a4b6373395` |
| `AC-23` | `PR-065` | `internal/migrationplanning/recovery_test.go` | `docs/fork/PR-065_STRUCTURED_MIGRATION_PLANS.md` | `community-evidence-retained` | `docs/release/evidence/AC-23.json @ sha256:2a0f629c328c865773b8ee5d2c7ea6b6cfce3846c1ba6a94ef69c02546eccf6a` |
| `AC-24` | `PR-065` | `internal/migrationplanning/recovery_test.go` | `docs/fork/PR-065_STRUCTURED_MIGRATION_PLANS.md` | `community-evidence-retained` | `docs/release/evidence/AC-24.json @ sha256:b6e274b2fae00b81b584692c4886086a73253df11bb94a0c6e04d394c8d87b2b` |
| `AC-25` | `PR-064` | `internal/planning/changeset_test.go` | `docs/fork/PR-064_EXACT_PLAN_CHANGESET.md` | `community-evidence-retained` | `docs/release/evidence/AC-25.json @ sha256:77ec06304f7f352dcad0ade78eba6fe5d151dc92e524ac0ed0ceff1bede59050` |
| `AC-26` | `PR-064` | `internal/planning/baseline_test.go` | `docs/fork/PR-064_EXACT_PLAN_CHANGESET.md` | `community-evidence-retained` | `docs/release/evidence/AC-26.json @ sha256:3b63385e8f86e2af97bc823481f86c1961bbdcf6c156c81f3ad2d8d8aeb8ce9d` |
| `AC-27` | `PR-063` | `internal/planning/baseline_test.go` | `docs/fork/PR-063_TARGET_DEPLOYMENT_PLAN_V2.md` | `community-evidence-retained` | `docs/release/evidence/AC-27.json @ sha256:e237d4e9979fc6d701a00b1aedccc1140ba087afecfbc7c6029e9982a288b8ee` |
| `AC-28` | `PR-067` | `internal/governance/policy_test.go` | `docs/fork/PR-067_VERSIONED_DEPLOYMENT_POLICIES.md` | `community-evidence-retained` | `docs/release/evidence/AC-28.json @ sha256:4aeb9ff7f2c1ed282fb6d0711b7dc050d8427cffdcb41585614daa3e70149c77` |
| `AC-29` | `PR-068` | `internal/governance/approval_test.go` | `docs/fork/PR-068_CHECKSUM_BOUND_APPROVALS.md` | `community-evidence-retained` | `docs/release/evidence/AC-29.json @ sha256:9b52b4fcd1517e600e8e98f0273e9db07c10065edcd38a304aed45a1adf08d6d` |
| `AC-30` | `PR-068` | `internal/governance/approval_test.go` | `docs/fork/PR-068_CHECKSUM_BOUND_APPROVALS.md` | `community-evidence-retained` | `docs/release/evidence/AC-30.json @ sha256:bbd6433a61bfaff8a2dc28b5aae091e9de67e0833a4e1e89a5dfed3ca42c8e23` |
| `AC-31` | `PR-069` | `internal/scheduling/calendar_test.go` | `docs/fork/PR-069_MAINTENANCE_CALENDARS_FREEZES.md` | `community-evidence-retained` | `docs/release/evidence/AC-31.json @ sha256:c3e0ecc1244baf05fa24a117b0689d3f26416298c8e6d4b7f9c21fdaba09566e` |
| `AC-32` | `PR-070` | `internal/scheduling/admission_test.go` | `docs/fork/PR-070_DEPLOYMENT_ADMISSION_OVERRIDES.md` | `community-evidence-retained` | `docs/release/evidence/AC-32.json @ sha256:c56999f7d0e5be8776c4e72e288a8deeb61e5e8852385e7116d1772e415938ac` |
| `AC-33` | `PR-075` | `internal/db/execution_v2_test.go` | `docs/fork/PR-075_FENCED_EXECUTOR_PROTOCOL_V2.md` | `community-evidence-retained` | `docs/release/evidence/AC-33.json @ sha256:1d326a126ad0a3506b4c20f1ded3df616cb12a035f6432cb6413616d70a0536b` |
| `AC-34` | `PR-075` | `internal/db/execution_v2_test.go` | `docs/fork/PR-075_FENCED_EXECUTOR_PROTOCOL_V2.md` | `community-evidence-retained` | `docs/release/evidence/AC-34.json @ sha256:670efe70cc63d6a70092f6d1a3e82544366bc9664ecf375b26ad4cfb56b73ef9` |
| `AC-35` | `PR-075` | `internal/db/execution_v2_test.go` | `docs/fork/PR-075_FENCED_EXECUTOR_PROTOCOL_V2.md` | `community-evidence-retained` | `docs/release/evidence/AC-35.json @ sha256:9b92aac69a964405aa8390a642c2d5b111edaea5dabedb9a44bbc5113030467d` |
| `AC-36` | `PR-072` | `internal/campaigns/state_machine_test.go` | `docs/fork/PR-072_CAMPAIGN_SCHEDULER_THRESHOLDS.md` | `community-evidence-retained` | `docs/release/evidence/AC-36.json @ sha256:328c5548e99da7814c4689214695e46ef7aafde98c31a222497c46a78d744747` |
| `AC-37` | `PR-072` | `internal/campaigns/state_machine_test.go` | `docs/fork/PR-072_CAMPAIGN_SCHEDULER_THRESHOLDS.md` | `community-evidence-retained` | `docs/release/evidence/AC-37.json @ sha256:d7c3f243a1536e5be5eb7953a719541df3e0e202b5113a2f091baf8c20b8eef1` |
| `AC-38` | `PR-072` | `internal/campaigns/thresholds_test.go` | `docs/fork/PR-072_CAMPAIGN_SCHEDULER_THRESHOLDS.md` | `community-evidence-retained` | `docs/release/evidence/AC-38.json @ sha256:c536a9c869d1e7b09133942e32ecd64f20d5beac0dd8709922bcb20c51019c26` |
| `AC-39` | `PR-073` | `internal/campaigns/controls_test.go` | `docs/fork/PR-073_CAMPAIGN_OPERATIONAL_CONTROLS.md` | `community-evidence-retained` | `docs/release/evidence/AC-39.json @ sha256:cd873b5fddd381f6d89977ae8ba50fcccfb8f4f5d9793fb81f7e0ccf3089b28f` |
| `AC-40` | `PR-073` | `internal/db/execution_v2_recovery_test.go` | `docs/fork/PR-076_EXECUTION_CANCEL_STATUS_RECONCILIATION.md` | `community-evidence-retained` | `docs/release/evidence/AC-40.json @ sha256:a2958c444baec3a436981c072fbcef1e05d515eede0d1cfcfdeb917c6daa645f` |
| `AC-41` | `PR-073` | `internal/db/execution_v2_recovery_test.go` | `docs/fork/PR-076_EXECUTION_CANCEL_STATUS_RECONCILIATION.md` | `community-evidence-retained` | `docs/release/evidence/AC-41.json @ sha256:bb61dacd9d1e49e7c68ae1e321cf0c1418d113e8444dd2e71dc26c1697ab0abc` |
| `AC-42` | `PR-071` | `internal/campaigns/canonical_test.go` | `docs/fork/PR-071_IMMUTABLE_CAMPAIGN_REVISIONS.md` | `community-evidence-retained` | `docs/release/evidence/AC-42.json @ sha256:db7aafdf896ca659e9b5f881bb0217f5aa0fc21888693854ff0915d4b9712c4f` |
| `AC-43` | `PR-077` | `internal/reconciliation/drift_test.go` | `docs/fork/PR-077_DESIRED_OBSERVED_RECONCILIATION.md` | `community-evidence-retained` | `docs/release/evidence/AC-43.json @ sha256:7c4b9f6fab7018fe209cb506d9e764c09772303718dd47db7ee4b6e107ddfd2b` |
| `AC-44` | `PR-077` | `internal/reconciliation/drift_test.go` | `docs/fork/PR-077_DESIRED_OBSERVED_RECONCILIATION.md` | `community-evidence-retained` | `docs/release/evidence/AC-44.json @ sha256:a5eefa30f723b7556b9903aa1265568c5148b2e69808bdda79ac0844aa3afb6d` |
| `AC-45` | `PR-077` | `internal/desiredstate/state_test.go` | `docs/fork/PR-077_DESIRED_OBSERVED_RECONCILIATION.md` | `community-evidence-retained` | `docs/release/evidence/AC-45.json @ sha256:1c2e20a7fcc8fa42b23cb730c7c59a6101b99bbe403af187d194d666e65a8f66` |
| `AC-46` | `PR-078` | `internal/auditexport/bundle_test.go` | `docs/fork/PR-078_CONTROL_PLANE_AUDIT_EXPORT.md` | `community-evidence-retained` | `docs/release/evidence/AC-46.json @ sha256:901ec4d0de8501f8faf8d5f5421cf95f4d24c8528702153789cc489a5661b13c` |
| `AC-47` | `PR-078` | `internal/auditexport/worker_test.go` | `docs/fork/PR-078_CONTROL_PLANE_AUDIT_EXPORT.md` | `community-evidence-retained` | `docs/release/evidence/AC-47.json @ sha256:f7fde1052a5b938792f99d100e1bb4c055b86d0af08871a4aae60fb34511dbfd` |
| `AC-48`       | `ADOPTER-06` | `internal/retirement/apply_test.go`                          | `docs/superpowers/plans/2026-07-14-control-plane-operator-adoption.md` | `pending-adopter`            | `pending-adopter:ADOPTER-06`        |
| `AC-49`       | `ADOPTER-06` | `internal/retirement/apply_test.go`                          | `docs/superpowers/plans/2026-07-14-control-plane-operator-adoption.md` | `pending-adopter`            | `pending-adopter:ADOPTER-06`        |
| `AC-50`       | `PR-081`     | `hack/control-plane-scale-harness.test.mjs`                  | `docs/fork/PR-081_NEUTRAL_CONTROL_PLANE_PROOF.md`                      | `pending-community-evidence` | `pending-community-evidence:PR-081` |
| `AC-51`       | `PR-081`     | `hack/control-plane-load-test.test.mjs`                      | `docs/fork/PR-081_NEUTRAL_CONTROL_PLANE_PROOF.md`                      | `pending-community-evidence` | `pending-community-evidence:PR-081` |
| `AC-52`       | `ADOPTER-05` | `hack/control-plane-failure-matrix.test.mjs`                 | `docs/superpowers/plans/2026-07-14-control-plane-operator-adoption.md` | `pending-adopter`            | `pending-adopter:ADOPTER-05`        |
| `AC-53`       | `PR-081`     | `examples/control-plane-e2e/reference-executor/main_test.go` | `docs/fork/PR-081_NEUTRAL_CONTROL_PLANE_PROOF.md`                      | `pending-community-evidence` | `pending-community-evidence:PR-081` |
| `AC-54`       | `ADOPTER-05` | `examples/control-plane-e2e/reference-executor/main_test.go` | `docs/superpowers/plans/2026-07-14-control-plane-operator-adoption.md` | `pending-adopter`            | `pending-adopter:ADOPTER-05`        |
| `AC-55`       | `ADOPTER-02` | `internal/releasebundles/provenance_test.go`                 | `docs/superpowers/plans/2026-07-14-control-plane-operator-adoption.md` | `pending-adopter`            | `pending-adopter:ADOPTER-02`        |
| `AC-56` | `PR-064` | `internal/planning/changeset_test.go` | `docs/fork/PR-064_EXACT_PLAN_CHANGESET.md` | `community-evidence-retained` | `docs/release/evidence/AC-56.json @ sha256:032fddec2f2768e01a8ccfe54584b74bad5c54701c2506fbdc71259ee49acb16` |
| `AC-57` | `PR-077` | `internal/observation/gate_test.go` | `docs/fork/PR-077_DESIRED_OBSERVED_RECONCILIATION.md` | `community-evidence-retained` | `docs/release/evidence/AC-57.json @ sha256:6685eca0e4fd2ed87876a336dcc05a9427aa1e68bec4491c9cb5ea5ec32192fd` |
| `AC-58` | `PR-075` | `internal/db/execution_v2_test.go` | `docs/fork/PR-075_FENCED_EXECUTOR_PROTOCOL_V2.md` | `community-evidence-retained` | `docs/release/evidence/AC-58.json @ sha256:4013cc98d3dadd0191e19c55323b16427b07a8bcad74fdf4e4c1d3da8ca85a34` |
| `AC-59` | `PR-058` | `internal/targetconfig/validation_test.go` | `docs/fork/PR-058_TARGET_CONFIG_SNAPSHOTS.md` | `community-evidence-retained` | `docs/release/evidence/AC-59.json @ sha256:4550bd8972dc93c07fde744e6c6514d0dc281afdb9934d8f006cbc61bad7ef78` |
| `AC-60` | `PR-065` | `internal/migrationplanning/validation_test.go` | `docs/fork/PR-065_STRUCTURED_MIGRATION_PLANS.md` | `community-evidence-retained` | `docs/release/evidence/AC-60.json @ sha256:6b46d776dc08ac224a105ded520de714693db654d6ab62606487ef84015e63f0` |
| `AC-61` | `PR-065` | `internal/migrationplanning/recovery_test.go` | `docs/fork/PR-065_STRUCTURED_MIGRATION_PLANS.md` | `community-evidence-retained` | `docs/release/evidence/AC-61.json @ sha256:2cdbc1a3974b3d7d9de0d2c506ecec3811b41d9ef566d139a52e7c3f99b71150` |
| `AC-62` | `PR-071` | `internal/campaigns/canonical_test.go` | `docs/fork/PR-071_IMMUTABLE_CAMPAIGN_REVISIONS.md` | `community-evidence-retained` | `docs/release/evidence/AC-62.json @ sha256:0d9d377775f455fa1ec722dc8f2ea7eb5855fa317fbe6a4cb6555ca02562a7a9` |
| `AC-63`       | `PR-080`     | `frontend/ui/e2e/control-plane.spec.ts`                      | `docs/fork/PR-080_OPERATOR_CONTROL_ROOM_UI.md`                         | `pending-community-evidence` | `pending-community-evidence:PR-080` |
| `AC-64`       | `ADOPTER-06` | `internal/retirement/apply_test.go`                          | `docs/superpowers/plans/2026-07-14-control-plane-operator-adoption.md` | `pending-adopter`            | `pending-adopter:ADOPTER-06`        |
| `AC-65` | `PR-061` | `internal/releasebundles/provenance_test.go` | `docs/fork/PR-061_RELEASE_PROVENANCE_BACKFILL.md` | `community-evidence-retained` | `docs/release/evidence/AC-65.json @ sha256:85893f55d7d1309e2f604ae20f213815924c775d38d7e9cf29e04f6e0251bcdb` |
| `AC-66` | `PR-072` | `internal/campaigns/thresholds_test.go` | `docs/fork/PR-072_CAMPAIGN_SCHEDULER_THRESHOLDS.md` | `community-evidence-retained` | `docs/release/evidence/AC-66.json @ sha256:dab097ab49d4ea91ee9677ee8b7a02148ecc4251895e1566ab2a775bc12f7040` |
| `AC-67` | `PR-067` | `internal/governance/policy_test.go` | `docs/fork/PR-067_VERSIONED_DEPLOYMENT_POLICIES.md` | `community-evidence-retained` | `docs/release/evidence/AC-67.json @ sha256:339ad88cd52a30ade3f8d81a7dfe37523eb43534dcbce6d35ede4ff5bb132e5d` |
| `AC-68` | `PR-059` | `internal/targetconfig/v1_extraction_test.go` | `docs/fork/PR-059_V1_CONFIG_EXTRACTION.md` | `community-evidence-retained` | `docs/release/evidence/AC-68.json @ sha256:5ad17d91d51813d5d8e308e0e2d20905836539c38beeabca308a08d493b33a62` |
| `AC-69` | `PR-075` | `internal/db/execution_v2_recovery_test.go` | `docs/fork/PR-075_FENCED_EXECUTOR_PROTOCOL_V2.md` | `community-evidence-retained` | `docs/release/evidence/AC-69.json @ sha256:ce455b1626ee83a2f23cb294a824d0b1c028ea373030a5ec0f29037de28ffccd` |
| `AC-70` | `PR-076` | `internal/db/execution_v2_recovery_test.go` | `docs/fork/PR-076_EXECUTION_CANCEL_STATUS_RECONCILIATION.md` | `community-evidence-retained` | `docs/release/evidence/AC-70.json @ sha256:ea8901abe0cef633427f360cefb674b9a75167d0d7b628015f577db64648b8a1` |
| `AC-71` | `PR-070` | `internal/scheduling/admission_test.go` | `docs/fork/PR-070_DEPLOYMENT_ADMISSION_OVERRIDES.md` | `community-evidence-retained` | `docs/release/evidence/AC-71.json @ sha256:182c876ad538dde9868773f3c9e8b135f80382898fd1399cb5144b7c73a0e18d` |
| `AC-72` | `PR-077` | `internal/desiredstate/state_test.go` | `docs/fork/PR-077_DESIRED_OBSERVED_RECONCILIATION.md` | `community-evidence-retained` | `docs/release/evidence/AC-72.json @ sha256:b62a7f455c6f02ea26b16ef1ef6048d7977d5793d6a17e2188262c1620d93dc1` |
| `AC-73` | `PR-077` | `internal/observation/ingest_test.go` | `docs/fork/PR-077_DESIRED_OBSERVED_RECONCILIATION.md` | `community-evidence-retained` | `docs/release/evidence/AC-73.json @ sha256:79f022a37f435daaf547d811dc28fd9d717a2bfc4a86bbf0d181b399a3ddb621` |
| `AC-74` | `PR-069` | `internal/scheduling/calendar_test.go` | `docs/fork/PR-069_MAINTENANCE_CALENDARS_FREEZES.md` | `community-evidence-retained` | `docs/release/evidence/AC-74.json @ sha256:31a62130e0c3309b894cd98edf14ad9b0811c32f1e573d47923fd4aa19d4918b` |
| `AC-75` | `PR-068` | `internal/governance/approval_test.go` | `docs/fork/PR-068_CHECKSUM_BOUND_APPROVALS.md` | `community-evidence-retained` | `docs/release/evidence/AC-75.json @ sha256:34e88f2d758e49daf066b61097991a7d729617e59506034eb0c81f0c4088233c` |
| `AC-76` | `PR-074` | `internal/adapterresolution/resolver_test.go` | `docs/fork/PR-074_VERSIONED_ADAPTER_RESOLUTION.md` | `community-evidence-retained` | `docs/release/evidence/AC-76.json @ sha256:39ac56a2bf5f200f559129949186739fea7244e8ee864454ab20e2a0413b6b50` |
| `AC-77` | `PR-065` | `internal/migrationplanning/recovery_test.go` | `docs/fork/PR-065_STRUCTURED_MIGRATION_PLANS.md` | `community-evidence-retained` | `docs/release/evidence/AC-77.json @ sha256:3ea0fec9e588e07b6e7a13a625abd91033e517a9350cc8d7b79c34ec7e1af364` |
| `AC-78` | `PR-062` | `internal/productrelease/graph_test.go` | `docs/fork/PR-062_PRODUCT_RELEASE_CAPABILITY_GRAPH.md` | `community-evidence-retained` | `docs/release/evidence/AC-78.json @ sha256:718e4b9fe4b4a582f2f2d845c1fc46e41ab205d3396c18a3f0b81cc25ad0718c` |
| `AC-79`       | `ADOPTER-06` | `internal/retirement/apply_test.go`                          | `docs/superpowers/plans/2026-07-14-control-plane-operator-adoption.md` | `pending-adopter`            | `pending-adopter:ADOPTER-06`        |
| `AC-80` | `PR-071` | `internal/campaigns/scheduler_test.go` | `docs/fork/PR-071_IMMUTABLE_CAMPAIGN_REVISIONS.md` | `community-evidence-retained` | `docs/release/evidence/AC-80.json @ sha256:092ae825976c07db83e3ec56a90b75f93a06e29e15ddc2a3dfda289bb2142d60` |
