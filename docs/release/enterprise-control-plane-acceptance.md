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
| `AC-08` | `PR-058` | `internal/targetconfig/canonical_test.go` | `docs/fork/PR-058_TARGET_CONFIG_SNAPSHOTS.md` | `community-evidence-retained` | `docs/release/evidence/AC-08.json @ sha256:640f7671c9c403f77de234fb035b9bc306efd05267e1bc4f47d375eecbb158bf` |
| `AC-09` | `PR-060` | `internal/releasebundles/release_contract_v2_test.go` | `docs/fork/PR-060_COMPONENT_RELEASE_CONTRACT_V2.md` | `community-evidence-retained` | `docs/release/evidence/AC-09.json @ sha256:782b0ab4daca7cf77d953917317b75f264b796c9fa5ec546773455559587c203` |
| `AC-10` | `PR-060` | `internal/releasebundles/release_contract_v2_test.go` | `docs/fork/PR-060_COMPONENT_RELEASE_CONTRACT_V2.md` | `community-evidence-retained` | `docs/release/evidence/AC-10.json @ sha256:ca47583882af73018808d85d0abf5e012006d6c405d4167382bb4bdff6575d46` |
| `AC-11` | `PR-058` | `internal/targetconfig/validation_test.go` | `docs/fork/PR-058_TARGET_CONFIG_SNAPSHOTS.md` | `community-evidence-retained` | `docs/release/evidence/AC-11.json @ sha256:cf4cb866f1bc7cfffc0d9ddb4e453d87a44b56892e83df19cb9a5a5d85853b83` |
| `AC-12` | `PR-058` | `internal/targetconfig/canonical_test.go` | `docs/fork/PR-058_TARGET_CONFIG_SNAPSHOTS.md` | `community-evidence-retained` | `docs/release/evidence/AC-12.json @ sha256:efdd5465a5f1f742692bdfe49fc6dc76a28af2253d5a206c93cc39bdc8b1d99d` |
| `AC-13` | `PR-061` | `internal/releasebundles/provenance_test.go` | `docs/fork/PR-061_RELEASE_PROVENANCE_BACKFILL.md` | `community-evidence-retained` | `docs/release/evidence/AC-13.json @ sha256:3d7db9fe3440a0572bb5cf84f3eef7913fcdf5c5a4f888e505d3352ba2bd12db` |
| `AC-14` | `PR-059` | `internal/targetconfig/v1_extraction_test.go` | `docs/fork/PR-059_V1_CONFIG_EXTRACTION.md` | `community-evidence-retained` | `docs/release/evidence/AC-14.json @ sha256:f8da279ac7157a4935c97af25ad6642075178c3b6d5e442de7dbee0feebabed2` |
| `AC-15` | `PR-062` | `internal/productrelease/graph_test.go` | `docs/fork/PR-062_PRODUCT_RELEASE_CAPABILITY_GRAPH.md` | `community-evidence-retained` | `docs/release/evidence/AC-15.json @ sha256:0c2f5d690e29ce1d272087e1b5ff46ce920dfcbee7233fd31ea733e56e08887f` |
| `AC-16` | `PR-062` | `internal/productrelease/graph_test.go` | `docs/fork/PR-062_PRODUCT_RELEASE_CAPABILITY_GRAPH.md` | `community-evidence-retained` | `docs/release/evidence/AC-16.json @ sha256:ee20a66f5f80c4d5175d5b3445d61d66f7431f1b8b7ed7f6e7c8c5164fe6013d` |
| `AC-17` | `PR-063` | `internal/planning/resolver_test.go` | `docs/fork/PR-063_TARGET_DEPLOYMENT_PLAN_V2.md` | `community-evidence-retained` | `docs/release/evidence/AC-17.json @ sha256:e96763a80d281038c4dcb728d2b12a520781874b771ee677c1879e4058cbd863` |
| `AC-18` | `PR-062` | `internal/productrelease/graph_test.go` | `docs/fork/PR-062_PRODUCT_RELEASE_CAPABILITY_GRAPH.md` | `community-evidence-retained` | `docs/release/evidence/AC-18.json @ sha256:da5891016bacec4ab87cb0a212552f7631e0871da22f689a673dd9062352205f` |
| `AC-19` | `PR-063` | `internal/planning/resolver_test.go` | `docs/fork/PR-063_TARGET_DEPLOYMENT_PLAN_V2.md` | `community-evidence-retained` | `docs/release/evidence/AC-19.json @ sha256:f9d97ab78183fb8f44783ec6cc57d5229fc701d04193ac3f75724d0e1c567109` |
| `AC-20` | `PR-063` | `internal/planning/resolver_test.go` | `docs/fork/PR-063_TARGET_DEPLOYMENT_PLAN_V2.md` | `community-evidence-retained` | `docs/release/evidence/AC-20.json @ sha256:a401d1dcffe23a85710b427edb002fbb2cc84537b09efc5234fe366ad9643ba6` |
| `AC-21` | `PR-065` | `internal/migrationplanning/validation_test.go` | `docs/fork/PR-065_STRUCTURED_MIGRATION_PLANS.md` | `community-evidence-retained` | `docs/release/evidence/AC-21.json @ sha256:4000d0d7483fb33f5c019f3e03d0134a133d245c21b890a8354cd526f1b5c276` |
| `AC-22` | `PR-065` | `internal/migrationplanning/validation_test.go` | `docs/fork/PR-065_STRUCTURED_MIGRATION_PLANS.md` | `community-evidence-retained` | `docs/release/evidence/AC-22.json @ sha256:11f9ffbb48e928296ea13dabd4bc6c7ee944fe57b6f2a93ed4566c64faccdf2b` |
| `AC-23` | `PR-065` | `internal/migrationplanning/recovery_test.go` | `docs/fork/PR-065_STRUCTURED_MIGRATION_PLANS.md` | `community-evidence-retained` | `docs/release/evidence/AC-23.json @ sha256:74ef83f569008583332e4b94c923db469e4276bdb83637ae0ca3dbb0903b8655` |
| `AC-24` | `PR-065` | `internal/migrationplanning/recovery_test.go` | `docs/fork/PR-065_STRUCTURED_MIGRATION_PLANS.md` | `community-evidence-retained` | `docs/release/evidence/AC-24.json @ sha256:9172b07f09961d64cd7a8446621a29e261e1ba65a23bbd7a98a5af7934973e5f` |
| `AC-25` | `PR-064` | `internal/planning/changeset_test.go` | `docs/fork/PR-064_EXACT_PLAN_CHANGESET.md` | `community-evidence-retained` | `docs/release/evidence/AC-25.json @ sha256:c4a63272ada63aea71ad83c4b9701fa7c7214b531ac50f1727051b3afcc3614d` |
| `AC-26` | `PR-064` | `internal/planning/baseline_test.go` | `docs/fork/PR-064_EXACT_PLAN_CHANGESET.md` | `community-evidence-retained` | `docs/release/evidence/AC-26.json @ sha256:98c771b0a3b66a5c4c4c5682adf6ba8769ef9b3a32741064a2d319ca82c60835` |
| `AC-27` | `PR-063` | `internal/planning/baseline_test.go` | `docs/fork/PR-063_TARGET_DEPLOYMENT_PLAN_V2.md` | `community-evidence-retained` | `docs/release/evidence/AC-27.json @ sha256:63644c66bb56a223978f51cc4887e23612f7b477209ef11a10167878fef81628` |
| `AC-28` | `PR-067` | `internal/governance/policy_test.go` | `docs/fork/PR-067_VERSIONED_DEPLOYMENT_POLICIES.md` | `community-evidence-retained` | `docs/release/evidence/AC-28.json @ sha256:f1d0a79b9af34d71dd862f3492d1cb882453171aae1d6d0852fa8fa1d5d8a3db` |
| `AC-29` | `PR-068` | `internal/governance/approval_test.go` | `docs/fork/PR-068_CHECKSUM_BOUND_APPROVALS.md` | `community-evidence-retained` | `docs/release/evidence/AC-29.json @ sha256:26cfbd48bef0dc44396091f5ef00e1e6dc44d2bd87c65fca84ac67fdafae79b3` |
| `AC-30` | `PR-068` | `internal/governance/approval_test.go` | `docs/fork/PR-068_CHECKSUM_BOUND_APPROVALS.md` | `community-evidence-retained` | `docs/release/evidence/AC-30.json @ sha256:e82399610fd3672586ebf0f0d69011f9edc80884654b7406be2cadde331d9e33` |
| `AC-31` | `PR-069` | `internal/scheduling/calendar_test.go` | `docs/fork/PR-069_MAINTENANCE_CALENDARS_FREEZES.md` | `community-evidence-retained` | `docs/release/evidence/AC-31.json @ sha256:c0ebf04d58e0c9ee27365a53ddcbd53ef1cf45ec1f2cd1c7eac766f14aebec2f` |
| `AC-32` | `PR-070` | `internal/scheduling/admission_test.go` | `docs/fork/PR-070_DEPLOYMENT_ADMISSION_OVERRIDES.md` | `community-evidence-retained` | `docs/release/evidence/AC-32.json @ sha256:c6b07eee7cba332c5b12f6124e8ac0f1ed96a94ac4765efaa71c4eabd3605749` |
| `AC-33` | `PR-075` | `internal/db/execution_v2_test.go` | `docs/fork/PR-075_FENCED_EXECUTOR_PROTOCOL_V2.md` | `community-evidence-retained` | `docs/release/evidence/AC-33.json @ sha256:a23467bc350be603696e72c75ab0769eb0bbb8436e0cc2aa06f0dfa01253c535` |
| `AC-34` | `PR-075` | `internal/db/execution_v2_test.go` | `docs/fork/PR-075_FENCED_EXECUTOR_PROTOCOL_V2.md` | `community-evidence-retained` | `docs/release/evidence/AC-34.json @ sha256:52727b7348a0fa0f6465ac41193f365659237eb48855d47511838ec00119bcd1` |
| `AC-35` | `PR-075` | `internal/db/execution_v2_test.go` | `docs/fork/PR-075_FENCED_EXECUTOR_PROTOCOL_V2.md` | `community-evidence-retained` | `docs/release/evidence/AC-35.json @ sha256:1fa25f673bd04b65d13553e9b8a565e7b13eb4f4944ca7b7742a720c093d6955` |
| `AC-36` | `PR-072` | `internal/campaigns/state_machine_test.go` | `docs/fork/PR-072_CAMPAIGN_SCHEDULER_THRESHOLDS.md` | `community-evidence-retained` | `docs/release/evidence/AC-36.json @ sha256:328c5548e99da7814c4689214695e46ef7aafde98c31a222497c46a78d744747` |
| `AC-37` | `PR-072` | `internal/campaigns/state_machine_test.go` | `docs/fork/PR-072_CAMPAIGN_SCHEDULER_THRESHOLDS.md` | `community-evidence-retained` | `docs/release/evidence/AC-37.json @ sha256:d7c3f243a1536e5be5eb7953a719541df3e0e202b5113a2f091baf8c20b8eef1` |
| `AC-38` | `PR-072` | `internal/campaigns/thresholds_test.go` | `docs/fork/PR-072_CAMPAIGN_SCHEDULER_THRESHOLDS.md` | `community-evidence-retained` | `docs/release/evidence/AC-38.json @ sha256:c536a9c869d1e7b09133942e32ecd64f20d5beac0dd8709922bcb20c51019c26` |
| `AC-39` | `PR-073` | `internal/campaigns/controls_test.go` | `docs/fork/PR-073_CAMPAIGN_OPERATIONAL_CONTROLS.md` | `community-evidence-retained` | `docs/release/evidence/AC-39.json @ sha256:cd873b5fddd381f6d89977ae8ba50fcccfb8f4f5d9793fb81f7e0ccf3089b28f` |
| `AC-40` | `PR-073` | `internal/db/execution_v2_recovery_test.go` | `docs/fork/PR-076_EXECUTION_CANCEL_STATUS_RECONCILIATION.md` | `community-evidence-retained` | `docs/release/evidence/AC-40.json @ sha256:f53b9481ba40a0b8319376611299ef3d104a2f03657dbaefe253fa3b81f08739` |
| `AC-41` | `PR-073` | `internal/db/execution_v2_recovery_test.go` | `docs/fork/PR-076_EXECUTION_CANCEL_STATUS_RECONCILIATION.md` | `community-evidence-retained` | `docs/release/evidence/AC-41.json @ sha256:78f3fd83b7ba91a168e1bfd222257246a1c6dab01b58ed59f0a6df1ca53078a7` |
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
| `AC-56` | `PR-064` | `internal/planning/changeset_test.go` | `docs/fork/PR-064_EXACT_PLAN_CHANGESET.md` | `community-evidence-retained` | `docs/release/evidence/AC-56.json @ sha256:03ef230280c37956e0dd3ed5901f6df4298e74ccbcde598adc60169c1eed26aa` |
| `AC-57` | `PR-077` | `internal/observation/gate_test.go` | `docs/fork/PR-077_DESIRED_OBSERVED_RECONCILIATION.md` | `community-evidence-retained` | `docs/release/evidence/AC-57.json @ sha256:062339911e0aba3fc583a72616962e9b7f889ea1d9dc543f02c9f6481720e98e` |
| `AC-58` | `PR-075` | `internal/db/execution_v2_test.go` | `docs/fork/PR-075_FENCED_EXECUTOR_PROTOCOL_V2.md` | `community-evidence-retained` | `docs/release/evidence/AC-58.json @ sha256:51e79185232da99e40143dda6c69fe6ff0fd80ff7619bc204335c0bc4203f441` |
| `AC-59` | `PR-058` | `internal/targetconfig/validation_test.go` | `docs/fork/PR-058_TARGET_CONFIG_SNAPSHOTS.md` | `community-evidence-retained` | `docs/release/evidence/AC-59.json @ sha256:96165352a34823e7433f39c73791f8e41c0651a2c5dd4ad93f52fa0fb3636170` |
| `AC-60` | `PR-065` | `internal/migrationplanning/validation_test.go` | `docs/fork/PR-065_STRUCTURED_MIGRATION_PLANS.md` | `community-evidence-retained` | `docs/release/evidence/AC-60.json @ sha256:fd875dc6add556b242a8c832974116cb1a75d7e7268b1def59d3769f5bb7cd7d` |
| `AC-61` | `PR-065` | `internal/migrationplanning/recovery_test.go` | `docs/fork/PR-065_STRUCTURED_MIGRATION_PLANS.md` | `community-evidence-retained` | `docs/release/evidence/AC-61.json @ sha256:003e26aad080be40f5604c24d2ce43502dd4e1aee11e0bbc0c9d17c3304d1f6e` |
| `AC-62` | `PR-071` | `internal/campaigns/canonical_test.go` | `docs/fork/PR-071_IMMUTABLE_CAMPAIGN_REVISIONS.md` | `community-evidence-retained` | `docs/release/evidence/AC-62.json @ sha256:0d9d377775f455fa1ec722dc8f2ea7eb5855fa317fbe6a4cb6555ca02562a7a9` |
| `AC-63`       | `PR-080`     | `frontend/ui/e2e/control-plane.spec.ts`                      | `docs/fork/PR-080_OPERATOR_CONTROL_ROOM_UI.md`                         | `pending-community-evidence` | `pending-community-evidence:PR-080` |
| `AC-64`       | `ADOPTER-06` | `internal/retirement/apply_test.go`                          | `docs/superpowers/plans/2026-07-14-control-plane-operator-adoption.md` | `pending-adopter`            | `pending-adopter:ADOPTER-06`        |
| `AC-65` | `PR-061` | `internal/releasebundles/provenance_test.go` | `docs/fork/PR-061_RELEASE_PROVENANCE_BACKFILL.md` | `community-evidence-retained` | `docs/release/evidence/AC-65.json @ sha256:4c5472377d402c3d472dc177243e7fabe007aea9d781f61a83e6fe2065c955f5` |
| `AC-66` | `PR-072` | `internal/campaigns/thresholds_test.go` | `docs/fork/PR-072_CAMPAIGN_SCHEDULER_THRESHOLDS.md` | `community-evidence-retained` | `docs/release/evidence/AC-66.json @ sha256:dab097ab49d4ea91ee9677ee8b7a02148ecc4251895e1566ab2a775bc12f7040` |
| `AC-67` | `PR-067` | `internal/governance/policy_test.go` | `docs/fork/PR-067_VERSIONED_DEPLOYMENT_POLICIES.md` | `community-evidence-retained` | `docs/release/evidence/AC-67.json @ sha256:89c598fdbdfeabd77bb1325d45ed213c5938d24e86a0bb1dd68e913df02049b6` |
| `AC-68` | `PR-059` | `internal/targetconfig/v1_extraction_test.go` | `docs/fork/PR-059_V1_CONFIG_EXTRACTION.md` | `community-evidence-retained` | `docs/release/evidence/AC-68.json @ sha256:55b2ed45ed431945a7bfe0a93755c0dec51a92b901d2e9a98069c28e94fc49f4` |
| `AC-69` | `PR-075` | `internal/db/execution_v2_recovery_test.go` | `docs/fork/PR-075_FENCED_EXECUTOR_PROTOCOL_V2.md` | `community-evidence-retained` | `docs/release/evidence/AC-69.json @ sha256:0dfdcf3d526e4ecf759de17919ad0a13dc0848316e0c1f55ea4fae75f48b3dec` |
| `AC-70` | `PR-076` | `internal/db/execution_v2_recovery_test.go` | `docs/fork/PR-076_EXECUTION_CANCEL_STATUS_RECONCILIATION.md` | `community-evidence-retained` | `docs/release/evidence/AC-70.json @ sha256:1532e07dd821588953429027d9030cc21551d7b03cf62a7c6c067b93ce4b4678` |
| `AC-71` | `PR-070` | `internal/scheduling/admission_test.go` | `docs/fork/PR-070_DEPLOYMENT_ADMISSION_OVERRIDES.md` | `community-evidence-retained` | `docs/release/evidence/AC-71.json @ sha256:8352683192474fe0bf82222f2fdb53cabde97496b441ac05f726b56cb6e94428` |
| `AC-72` | `PR-077` | `internal/desiredstate/state_test.go` | `docs/fork/PR-077_DESIRED_OBSERVED_RECONCILIATION.md` | `community-evidence-retained` | `docs/release/evidence/AC-72.json @ sha256:b62a7f455c6f02ea26b16ef1ef6048d7977d5793d6a17e2188262c1620d93dc1` |
| `AC-73` | `PR-077` | `internal/observation/ingest_test.go` | `docs/fork/PR-077_DESIRED_OBSERVED_RECONCILIATION.md` | `community-evidence-retained` | `docs/release/evidence/AC-73.json @ sha256:1a781685bf1c892a1d6c61a63779ebe2f3ef2ddb6209c4dc45e8b4a06e9d3ed8` |
| `AC-74` | `PR-069` | `internal/scheduling/calendar_test.go` | `docs/fork/PR-069_MAINTENANCE_CALENDARS_FREEZES.md` | `community-evidence-retained` | `docs/release/evidence/AC-74.json @ sha256:c1a3f524a64d389eef1f2cf46549414f1c6366279648b02d4975a98395c80022` |
| `AC-75` | `PR-068` | `internal/governance/approval_test.go` | `docs/fork/PR-068_CHECKSUM_BOUND_APPROVALS.md` | `community-evidence-retained` | `docs/release/evidence/AC-75.json @ sha256:35c3d426b6f3305094bfe1b7add0b720b509b21b082d63e153f056bf33b9697f` |
| `AC-76` | `PR-074` | `internal/adapterresolution/resolver_test.go` | `docs/fork/PR-074_VERSIONED_ADAPTER_RESOLUTION.md` | `community-evidence-retained` | `docs/release/evidence/AC-76.json @ sha256:39ac56a2bf5f200f559129949186739fea7244e8ee864454ab20e2a0413b6b50` |
| `AC-77` | `PR-065` | `internal/migrationplanning/recovery_test.go` | `docs/fork/PR-065_STRUCTURED_MIGRATION_PLANS.md` | `community-evidence-retained` | `docs/release/evidence/AC-77.json @ sha256:65174edb468ea8e33af95e7b63ec9e4b9a4fa75aa42d6a5f98e55731b1deff9b` |
| `AC-78` | `PR-062` | `internal/productrelease/graph_test.go` | `docs/fork/PR-062_PRODUCT_RELEASE_CAPABILITY_GRAPH.md` | `community-evidence-retained` | `docs/release/evidence/AC-78.json @ sha256:91f37c679565ac2297f2dd772d953e3d3151dc972e6309cb7d32d788263cb4e1` |
| `AC-79`       | `ADOPTER-06` | `internal/retirement/apply_test.go`                          | `docs/superpowers/plans/2026-07-14-control-plane-operator-adoption.md` | `pending-adopter`            | `pending-adopter:ADOPTER-06`        |
| `AC-80` | `PR-071` | `internal/campaigns/scheduler_test.go` | `docs/fork/PR-071_IMMUTABLE_CAMPAIGN_REVISIONS.md` | `community-evidence-retained` | `docs/release/evidence/AC-80.json @ sha256:092ae825976c07db83e3ec56a90b75f93a06e29e15ddc2a3dfda289bb2142d60` |
