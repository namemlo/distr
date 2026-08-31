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
| PostgreSQL migration 138 to 164, clean install, down/refusal, restart, and mixed v1/v2 matrix          | `pending-environment` | Database identity, commands, schema checkpoints, exact results, and report checksum                          |
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
| `AC-08` | `PR-058` | `internal/targetconfig/canonical_test.go` | `docs/fork/PR-058_TARGET_CONFIG_SNAPSHOTS.md` | `community-evidence-retained` | `docs/release/evidence/AC-08.json @ sha256:66bc769146f4a6002067292947df138b291222b0c7440e7fc5bd02f6811c209f` |
| `AC-09` | `PR-060` | `internal/releasebundles/release_contract_v2_test.go` | `docs/fork/PR-060_COMPONENT_RELEASE_CONTRACT_V2.md` | `community-evidence-retained` | `docs/release/evidence/AC-09.json @ sha256:59f479e48c863d11d7d00da880075c56cd8483dd1b17c9364a91df26be4cc485` |
| `AC-10` | `PR-060` | `internal/releasebundles/release_contract_v2_test.go` | `docs/fork/PR-060_COMPONENT_RELEASE_CONTRACT_V2.md` | `community-evidence-retained` | `docs/release/evidence/AC-10.json @ sha256:8f2ab0c132c1da2243a212e00189b04dbad567eec2f2083ce8de079c329a7c1c` |
| `AC-11` | `PR-058` | `internal/targetconfig/validation_test.go` | `docs/fork/PR-058_TARGET_CONFIG_SNAPSHOTS.md` | `community-evidence-retained` | `docs/release/evidence/AC-11.json @ sha256:f941ce4c92d4e445210733bd2ee5db7075b3c88399af1d7bcf51aba10b7b5ac9` |
| `AC-12` | `PR-058` | `internal/targetconfig/canonical_test.go` | `docs/fork/PR-058_TARGET_CONFIG_SNAPSHOTS.md` | `community-evidence-retained` | `docs/release/evidence/AC-12.json @ sha256:21de4f3451c468952d3bf85df5caee1dc5728ea526fa5c6d028b3e63b6796b2a` |
| `AC-13` | `PR-061` | `internal/releasebundles/provenance_test.go` | `docs/fork/PR-061_RELEASE_PROVENANCE_BACKFILL.md` | `community-evidence-retained` | `docs/release/evidence/AC-13.json @ sha256:ec204cdcb039a15fa26484130344a7cf90dbbd2c53dbb63f7146fc6eeac55a6c` |
| `AC-14` | `PR-059` | `internal/targetconfig/v1_extraction_test.go` | `docs/fork/PR-059_V1_CONFIG_EXTRACTION.md` | `community-evidence-retained` | `docs/release/evidence/AC-14.json @ sha256:02718f70ea9015be5ae955fcc13cbc6ac3f6c24edd978005f0e266094f9dbafd` |
| `AC-15` | `PR-062` | `internal/productrelease/graph_test.go` | `docs/fork/PR-062_PRODUCT_RELEASE_CAPABILITY_GRAPH.md` | `community-evidence-retained` | `docs/release/evidence/AC-15.json @ sha256:7797bd99a0bfda52ea632da4240608492814e5ce160a48c9a966fe554ced5ac7` |
| `AC-16` | `PR-062` | `internal/productrelease/graph_test.go` | `docs/fork/PR-062_PRODUCT_RELEASE_CAPABILITY_GRAPH.md` | `community-evidence-retained` | `docs/release/evidence/AC-16.json @ sha256:e98ac7eab6c041193a1341b0412b8e6e134860e14b83c32bc47f4fa28aed693c` |
| `AC-17` | `PR-063` | `internal/planning/resolver_test.go` | `docs/fork/PR-063_TARGET_DEPLOYMENT_PLAN_V2.md` | `community-evidence-retained` | `docs/release/evidence/AC-17.json @ sha256:239436cf0735e8dabfa549b0f57dc8b6c5c87704a95cde192985dfec249d66b7` |
| `AC-18` | `PR-062` | `internal/productrelease/graph_test.go` | `docs/fork/PR-062_PRODUCT_RELEASE_CAPABILITY_GRAPH.md` | `community-evidence-retained` | `docs/release/evidence/AC-18.json @ sha256:ae69ccd6edcf7bb4db6ce4c7f2dd800ba2a738bc45fa69ef1f4ecdb7446e4604` |
| `AC-19` | `PR-063` | `internal/planning/resolver_test.go` | `docs/fork/PR-063_TARGET_DEPLOYMENT_PLAN_V2.md` | `community-evidence-retained` | `docs/release/evidence/AC-19.json @ sha256:eee457d184500956cbe52fa740946f9e0bdd461b8a74d393b22433e77028d92b` |
| `AC-20` | `PR-063` | `internal/planning/resolver_test.go` | `docs/fork/PR-063_TARGET_DEPLOYMENT_PLAN_V2.md` | `community-evidence-retained` | `docs/release/evidence/AC-20.json @ sha256:ac08c40edac7446827c2b0a68cc98aa92c3463268bf85a3bd64ad9d85ed69f22` |
| `AC-21` | `PR-065` | `internal/migrationplanning/validation_test.go` | `docs/fork/PR-065_STRUCTURED_MIGRATION_PLANS.md` | `community-evidence-retained` | `docs/release/evidence/AC-21.json @ sha256:1c80d0033491754e14e85e6226806a53af637394d420803b0df24002df114f17` |
| `AC-22` | `PR-065` | `internal/migrationplanning/validation_test.go` | `docs/fork/PR-065_STRUCTURED_MIGRATION_PLANS.md` | `community-evidence-retained` | `docs/release/evidence/AC-22.json @ sha256:b9b5d7d1a8255c8de7953cb4d3defc19871f46b2a544473518062a7347786861` |
| `AC-23` | `PR-065` | `internal/migrationplanning/recovery_test.go` | `docs/fork/PR-065_STRUCTURED_MIGRATION_PLANS.md` | `community-evidence-retained` | `docs/release/evidence/AC-23.json @ sha256:b75c5bf79c151732900ccd91b2537dd3633530386fc77ab2693e7057c5cfd956` |
| `AC-24` | `PR-065` | `internal/migrationplanning/recovery_test.go` | `docs/fork/PR-065_STRUCTURED_MIGRATION_PLANS.md` | `community-evidence-retained` | `docs/release/evidence/AC-24.json @ sha256:b65c72f477030c8ac49cb670d1fcf2024d981eb38e5ebeeacc809e2663d42f21` |
| `AC-25` | `PR-064` | `internal/planning/changeset_test.go` | `docs/fork/PR-064_EXACT_PLAN_CHANGESET.md` | `community-evidence-retained` | `docs/release/evidence/AC-25.json @ sha256:f9cd73b610a2292b6940215dbfc9e7e0ea2deabeb5755cd755209f5c062e2b40` |
| `AC-26` | `PR-064` | `internal/planning/baseline_test.go` | `docs/fork/PR-064_EXACT_PLAN_CHANGESET.md` | `community-evidence-retained` | `docs/release/evidence/AC-26.json @ sha256:b43ddc0a475eb5841fdf0e04bfecbf7567d64430c2526ceb485dd14b7e441fe5` |
| `AC-27` | `PR-063` | `internal/planning/baseline_test.go` | `docs/fork/PR-063_TARGET_DEPLOYMENT_PLAN_V2.md` | `community-evidence-retained` | `docs/release/evidence/AC-27.json @ sha256:6a2d03093eaa7179306e3a0b90e17ba7a4ac7730a6b9538e5d1446d8e37c55cd` |
| `AC-28` | `PR-067` | `internal/governance/policy_test.go` | `docs/fork/PR-067_VERSIONED_DEPLOYMENT_POLICIES.md` | `community-evidence-retained` | `docs/release/evidence/AC-28.json @ sha256:05d88a7558aa4e8766697ec805c5e932b02e0677852eec63de9e89ffaed2be52` |
| `AC-29` | `PR-068` | `internal/governance/approval_test.go` | `docs/fork/PR-068_CHECKSUM_BOUND_APPROVALS.md` | `community-evidence-retained` | `docs/release/evidence/AC-29.json @ sha256:967be57ceb9db48af56a0b3761a8e1b747d72279e8dffa3778f3315ee7dcb045` |
| `AC-30` | `PR-068` | `internal/governance/approval_test.go` | `docs/fork/PR-068_CHECKSUM_BOUND_APPROVALS.md` | `community-evidence-retained` | `docs/release/evidence/AC-30.json @ sha256:f36ae3b26e6a07e9a2f866c32832ea95ee7a3eb80f2b97c04607c4c56e897950` |
| `AC-31` | `PR-069` | `internal/scheduling/calendar_test.go` | `docs/fork/PR-069_MAINTENANCE_CALENDARS_FREEZES.md` | `community-evidence-retained` | `docs/release/evidence/AC-31.json @ sha256:a4c445353dd84535990e826f9176a9773070fbcec7ecdb5abf7657af5a15137f` |
| `AC-32` | `PR-070` | `internal/scheduling/admission_test.go` | `docs/fork/PR-070_DEPLOYMENT_ADMISSION_OVERRIDES.md` | `community-evidence-retained` | `docs/release/evidence/AC-32.json @ sha256:8c098050cf98e2cbb2094f3189867bdd1600646be6bc37aacb726f47c2106345` |
| `AC-33` | `PR-075` | `internal/db/execution_v2_test.go` | `docs/fork/PR-075_FENCED_EXECUTOR_PROTOCOL_V2.md` | `community-evidence-retained` | `docs/release/evidence/AC-33.json @ sha256:f57ef9bd6f0752f247d5c129779de12cfd49da8108e333c440c262947d206dff` |
| `AC-34` | `PR-075` | `internal/db/execution_v2_test.go` | `docs/fork/PR-075_FENCED_EXECUTOR_PROTOCOL_V2.md` | `community-evidence-retained` | `docs/release/evidence/AC-34.json @ sha256:607e5c1163758c34db872c33389b133d15c32e4a5e9f9eff61bd232b77b1d5bc` |
| `AC-35` | `PR-075` | `internal/db/execution_v2_test.go` | `docs/fork/PR-075_FENCED_EXECUTOR_PROTOCOL_V2.md` | `community-evidence-retained` | `docs/release/evidence/AC-35.json @ sha256:937a2b1fbe081902371e7afcd62077d6de3631553901ad1a51a73f683e3609bc` |
| `AC-36` | `PR-072` | `internal/campaigns/state_machine_test.go` | `docs/fork/PR-072_CAMPAIGN_SCHEDULER_THRESHOLDS.md` | `community-evidence-retained` | `docs/release/evidence/AC-36.json @ sha256:328c5548e99da7814c4689214695e46ef7aafde98c31a222497c46a78d744747` |
| `AC-37` | `PR-072` | `internal/campaigns/state_machine_test.go` | `docs/fork/PR-072_CAMPAIGN_SCHEDULER_THRESHOLDS.md` | `community-evidence-retained` | `docs/release/evidence/AC-37.json @ sha256:d7c3f243a1536e5be5eb7953a719541df3e0e202b5113a2f091baf8c20b8eef1` |
| `AC-38` | `PR-072` | `internal/campaigns/thresholds_test.go` | `docs/fork/PR-072_CAMPAIGN_SCHEDULER_THRESHOLDS.md` | `community-evidence-retained` | `docs/release/evidence/AC-38.json @ sha256:c536a9c869d1e7b09133942e32ecd64f20d5beac0dd8709922bcb20c51019c26` |
| `AC-39` | `PR-073` | `internal/campaigns/controls_test.go` | `docs/fork/PR-073_CAMPAIGN_OPERATIONAL_CONTROLS.md` | `community-evidence-retained` | `docs/release/evidence/AC-39.json @ sha256:cd873b5fddd381f6d89977ae8ba50fcccfb8f4f5d9793fb81f7e0ccf3089b28f` |
| `AC-40` | `PR-073` | `internal/db/execution_v2_recovery_test.go` | `docs/fork/PR-076_EXECUTION_CANCEL_STATUS_RECONCILIATION.md` | `community-evidence-retained` | `docs/release/evidence/AC-40.json @ sha256:541857301d4b1dbf706ee448f1edbc17a3bd9495e60f8b3a428ea6d597d8ee78` |
| `AC-41` | `PR-073` | `internal/db/execution_v2_recovery_test.go` | `docs/fork/PR-076_EXECUTION_CANCEL_STATUS_RECONCILIATION.md` | `community-evidence-retained` | `docs/release/evidence/AC-41.json @ sha256:91d6e332917994182da33e116cf6944755de9b98c6afc9952c6bebea1c28087a` |
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
| `AC-56` | `PR-064` | `internal/planning/changeset_test.go` | `docs/fork/PR-064_EXACT_PLAN_CHANGESET.md` | `community-evidence-retained` | `docs/release/evidence/AC-56.json @ sha256:121f98407583517f3184f9219785a85c6442b0b6a8f8e7d14ce0e74ab88e9b37` |
| `AC-57` | `PR-077` | `internal/observation/gate_test.go` | `docs/fork/PR-077_DESIRED_OBSERVED_RECONCILIATION.md` | `community-evidence-retained` | `docs/release/evidence/AC-57.json @ sha256:de125a5633a4b41d9943e75bc6d0cf5e829c838dda282e90bc541af1e1c493e0` |
| `AC-58` | `PR-075` | `internal/db/execution_v2_test.go` | `docs/fork/PR-075_FENCED_EXECUTOR_PROTOCOL_V2.md` | `community-evidence-retained` | `docs/release/evidence/AC-58.json @ sha256:708cc76bb6b1670df839bdaad1d2e250a950bb5ae8a96ee9e9624498552c4add` |
| `AC-59` | `PR-058` | `internal/targetconfig/validation_test.go` | `docs/fork/PR-058_TARGET_CONFIG_SNAPSHOTS.md` | `community-evidence-retained` | `docs/release/evidence/AC-59.json @ sha256:19aacfe1c966895dfc14e77c6914aae470e150208aed4fc4a929bf2c424c5196` |
| `AC-60` | `PR-065` | `internal/migrationplanning/validation_test.go` | `docs/fork/PR-065_STRUCTURED_MIGRATION_PLANS.md` | `community-evidence-retained` | `docs/release/evidence/AC-60.json @ sha256:ce0c183b972f15e3029335c6659fbfd5fc9ba66b662b92175951349b5f1e720a` |
| `AC-61` | `PR-065` | `internal/migrationplanning/recovery_test.go` | `docs/fork/PR-065_STRUCTURED_MIGRATION_PLANS.md` | `community-evidence-retained` | `docs/release/evidence/AC-61.json @ sha256:64de2356afba95f38f1f4501171c9cc1eeba00b9779c1d1a8ed4bd457743ffa2` |
| `AC-62` | `PR-071` | `internal/campaigns/canonical_test.go` | `docs/fork/PR-071_IMMUTABLE_CAMPAIGN_REVISIONS.md` | `community-evidence-retained` | `docs/release/evidence/AC-62.json @ sha256:0d9d377775f455fa1ec722dc8f2ea7eb5855fa317fbe6a4cb6555ca02562a7a9` |
| `AC-63`       | `PR-080`     | `frontend/ui/e2e/control-plane.spec.ts`                      | `docs/fork/PR-080_OPERATOR_CONTROL_ROOM_UI.md`                         | `pending-community-evidence` | `pending-community-evidence:PR-080` |
| `AC-64`       | `ADOPTER-06` | `internal/retirement/apply_test.go`                          | `docs/superpowers/plans/2026-07-14-control-plane-operator-adoption.md` | `pending-adopter`            | `pending-adopter:ADOPTER-06`        |
| `AC-65` | `PR-061` | `internal/releasebundles/provenance_test.go` | `docs/fork/PR-061_RELEASE_PROVENANCE_BACKFILL.md` | `community-evidence-retained` | `docs/release/evidence/AC-65.json @ sha256:fdea96bcb21441825d5136a02d732b67a0c010070c106388b95806e44396cc71` |
| `AC-66` | `PR-072` | `internal/campaigns/thresholds_test.go` | `docs/fork/PR-072_CAMPAIGN_SCHEDULER_THRESHOLDS.md` | `community-evidence-retained` | `docs/release/evidence/AC-66.json @ sha256:dab097ab49d4ea91ee9677ee8b7a02148ecc4251895e1566ab2a775bc12f7040` |
| `AC-67` | `PR-067` | `internal/governance/policy_test.go` | `docs/fork/PR-067_VERSIONED_DEPLOYMENT_POLICIES.md` | `community-evidence-retained` | `docs/release/evidence/AC-67.json @ sha256:d9c37271e35952cc5d00226321f6ebc2f626489f0e7ab34ddde8f6a85d8fd9f3` |
| `AC-68` | `PR-059` | `internal/targetconfig/v1_extraction_test.go` | `docs/fork/PR-059_V1_CONFIG_EXTRACTION.md` | `community-evidence-retained` | `docs/release/evidence/AC-68.json @ sha256:7e6eb8ee0dcce5223883014cc9a98e42ac34a9ca6ef860d81ded53c3f8d24f71` |
| `AC-69` | `PR-075` | `internal/db/execution_v2_recovery_test.go` | `docs/fork/PR-075_FENCED_EXECUTOR_PROTOCOL_V2.md` | `community-evidence-retained` | `docs/release/evidence/AC-69.json @ sha256:9f412d7a2ef5f30c5bb34b59f7b812c258c61e5fce2d21ba56f0a36f717d8eb5` |
| `AC-70` | `PR-076` | `internal/db/execution_v2_recovery_test.go` | `docs/fork/PR-076_EXECUTION_CANCEL_STATUS_RECONCILIATION.md` | `community-evidence-retained` | `docs/release/evidence/AC-70.json @ sha256:9614c110c60fd69a969ca4068f61dd489d3350e843b1694cf44ec6f2c54691e1` |
| `AC-71` | `PR-070` | `internal/scheduling/admission_test.go` | `docs/fork/PR-070_DEPLOYMENT_ADMISSION_OVERRIDES.md` | `community-evidence-retained` | `docs/release/evidence/AC-71.json @ sha256:9a900d04baf4d618e2af0fc941e34242212cd3a0fc4eb355852f76ea766cdf67` |
| `AC-72` | `PR-077` | `internal/desiredstate/state_test.go` | `docs/fork/PR-077_DESIRED_OBSERVED_RECONCILIATION.md` | `community-evidence-retained` | `docs/release/evidence/AC-72.json @ sha256:b62a7f455c6f02ea26b16ef1ef6048d7977d5793d6a17e2188262c1620d93dc1` |
| `AC-73` | `PR-077` | `internal/observation/ingest_test.go` | `docs/fork/PR-077_DESIRED_OBSERVED_RECONCILIATION.md` | `community-evidence-retained` | `docs/release/evidence/AC-73.json @ sha256:d1690172f17b5350e6f8f3ea7c3f4e830ce4d101afc32acbaf982131278ea94a` |
| `AC-74` | `PR-069` | `internal/scheduling/calendar_test.go` | `docs/fork/PR-069_MAINTENANCE_CALENDARS_FREEZES.md` | `community-evidence-retained` | `docs/release/evidence/AC-74.json @ sha256:17d73f96a0ffffaf5ac528e0f0384792dc529827ad9330501835047dd79e3373` |
| `AC-75` | `PR-068` | `internal/governance/approval_test.go` | `docs/fork/PR-068_CHECKSUM_BOUND_APPROVALS.md` | `community-evidence-retained` | `docs/release/evidence/AC-75.json @ sha256:6238d1d7b8999d8114d7986baf4d110edccc7b595913728fd023fb00d06f4558` |
| `AC-76` | `PR-074` | `internal/adapterresolution/resolver_test.go` | `docs/fork/PR-074_VERSIONED_ADAPTER_RESOLUTION.md` | `community-evidence-retained` | `docs/release/evidence/AC-76.json @ sha256:39ac56a2bf5f200f559129949186739fea7244e8ee864454ab20e2a0413b6b50` |
| `AC-77` | `PR-065` | `internal/migrationplanning/recovery_test.go` | `docs/fork/PR-065_STRUCTURED_MIGRATION_PLANS.md` | `community-evidence-retained` | `docs/release/evidence/AC-77.json @ sha256:14b638a7c81312ea6459128982bc3429ab12bec4b98d712e1764287d4f3240d7` |
| `AC-78` | `PR-062` | `internal/productrelease/graph_test.go` | `docs/fork/PR-062_PRODUCT_RELEASE_CAPABILITY_GRAPH.md` | `community-evidence-retained` | `docs/release/evidence/AC-78.json @ sha256:ba7e34cc3003ddb8b3ba84cdf3fc6bff36bc538b8a1bf45800061fd46651e98d` |
| `AC-79`       | `ADOPTER-06` | `internal/retirement/apply_test.go`                          | `docs/superpowers/plans/2026-07-14-control-plane-operator-adoption.md` | `pending-adopter`            | `pending-adopter:ADOPTER-06`        |
| `AC-80` | `PR-071` | `internal/campaigns/scheduler_test.go` | `docs/fork/PR-071_IMMUTABLE_CAMPAIGN_REVISIONS.md` | `community-evidence-retained` | `docs/release/evidence/AC-80.json @ sha256:092ae825976c07db83e3ec56a90b75f93a06e29e15ddc2a3dfda289bb2142d60` |
