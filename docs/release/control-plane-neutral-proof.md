# Neutral Control-Plane Proof

## Status and claim boundary

PR-081 is the reproducible community reference proof for the operator control
plane. It is not a production certification. The checked-in fixture uses
synthetic organizations, targets, releases, configuration, executors, and
observers. It must not contain adopter identities, client data, credentials, or
host-specific paths.

Results belong in a retained result bundle and are valid only for the exact
source revision, fixture checksum, command, toolchain, feature flags, and
execution mode recorded with that bundle. A local simulated result is not a
measurement of a deployed Hub, PostgreSQL database, network, registry, identity
provider, secret store, executor host, observer, or audit sink.

## Root-verified local evidence

The following results were independently verified from the integrated PR-081
worktree on 2026-07-28:

- Reference-executor focused Go tests and `go vet` passed. Race verification
  remains blocked: the repository disables CGO by default, and the explicit
  CGO-enabled retry could not find GCC.
- The combined Node.js suites passed 28 of 28 tests.
- `node examples/control-plane-e2e/run.mjs --mode clean --json` exited zero in
  `fixture-contract` mode. It retained release history A-B-A, flow checksum
  `sha256:fc31db2b0aa7d56fd08622508be575ccc709a5c473c4efa04ca5005b1a8d8dd0`,
  completed cleanup, and recorded zero non-local calls.
- That clean run was not a live-stack result:
  `liveStack.started` was `false`, and the report identified the unavailable
  Docker CLI as the blocker.
- The deterministic failure matrix accepted all 14 expected cases. Its
  canonical fixture checksum was
  `sha256:346f08b0bd4904b5d52982538cb89da8510b9228376120e56e5d6f28f073388a`;
  its report checksum was
  `sha256:f476ac360e28da452b4c21c7b3c5a80455a6aae7471fa2eaf7ce4fc360448f3d`.
- The exact ten-minute/100-events-per-second deterministic simulation accepted
  60,000 events. Acknowledgement p95 was 5.564 ms with zero accepted-event
  loss, cross-organization leakage, or non-policy errors. Five-run
  100-component planning p95 was 6.844 ms with a stable checksum; the 500-step
  wave completed in 20.5 ms with stable order and no duplicate admission.

These are contract and time-compressed simulation results. No full live remote
run, live Compose run, Hub API end-to-end flow, staging run, or production run
was completed or claimed.

## Reproducible commands

Run from the repository root with the toolchain pinned by `mise.toml`:

```powershell
go test ./examples/control-plane-e2e/reference-executor -count=1 -race
node examples/control-plane-e2e/run.mjs --mode clean
node hack/control-plane-failure-matrix.mjs --fixture examples/control-plane-e2e/fixture.json
node hack/control-plane-scale-fixture.mjs --targets 1000 --placements 649 --agents 100 --components 100 --steps 500 --out work/control-plane-scale.json
node hack/control-plane-load-test.mjs --fixture work/control-plane-scale.json --duration 10m --rate 100
rg -n -i '<approved-forbidden-term-regex>' examples/control-plane-e2e hack/control-plane-failure-matrix.mjs hack/control-plane-load-test.mjs hack/control-plane-scale-fixture.mjs
```

Replace the placeholder with the review-approved adopter, client, product,
provider, CI, and registry term set. The final scan is a required neutrality
gate. An empty result is expected; any match must be reviewed and removed or
explicitly excluded before accepting the proof. The prohibited names
themselves are deliberately not copied into this public proof document.

The race command requires a race-capable Go toolchain, CGO, and a C compiler.
This repository defaults `CGO_ENABLED=0`; on a Windows host without GCC, retain
the exact blocker and run the ordinary focused tests plus `go vet` without
describing them as race evidence.

Retain machine-readable stdout and byte checksums:

```powershell
node examples/control-plane-e2e/run.mjs --mode clean --json > work/control-plane-e2e-result.json
node hack/control-plane-failure-matrix.mjs --fixture examples/control-plane-e2e/fixture.json > work/control-plane-failure-result.json
node hack/control-plane-load-test.mjs --fixture work/control-plane-scale.json --duration 10m --rate 100 > work/control-plane-load-result.json
Get-ChildItem work/control-plane-*.json | ForEach-Object { "$($_.Name) $((Get-FileHash -Algorithm SHA256 $_.FullName).Hash.ToLowerInvariant())" } | Set-Content -Encoding utf8NoBOM work/control-plane-sha256.txt
```

For a live read-model measurement, use the benchmark's explicit remote mode and
provide the bearer token through the named environment variable. Do not put a
token in an argument, URL, fixture, report, or shell history:

```powershell
$env:CONTROL_PLANE_BENCHMARK_TOKEN = '<scoped test token>'
node hack/control-plane-read-model-benchmark.mjs --fixture work/control-plane-scale.json --base-url https://hub.example.invalid --auth-env CONTROL_PLANE_BENCHMARK_TOKEN --runs 20 --page-size 100 --p95-ms 2000 --p99-ms 5000
Remove-Item Env:CONTROL_PLANE_BENCHMARK_TOKEN
```

Replace the reserved example URL only in an approved test environment. Retain a
redacted report; never retain the token.

The section 20.9 load tool also has an explicit loopback measured mode. The
server must implement the fixture's four `loadProof.remote` paths:

```powershell
$env:CONTROL_PLANE_LOAD_TOKEN = '<scoped local test token>'
node hack/control-plane-load-test.mjs --fixture work/control-plane-scale.json --duration 10m --rate 100 --base-url http://127.0.0.1:8080 --auth-env CONTROL_PLANE_LOAD_TOKEN > work/control-plane-load-live-result.json
Remove-Item Env:CONTROL_PLANE_LOAD_TOKEN
```

Only `localhost`, `127.0.0.0/8`, or `::1` is accepted. The tool forbids
redirects and cross-origin/network-path requests, takes authentication only
from the named environment variable, paces events in wall-clock time, streams
bounded log pages, and omits the token from errors and reports. A short smoke is
reported as measured but cannot satisfy the full ten-minute/100-rate acceptance
profile.

## Reference-fixture identity

The source of truth for the neutral end-to-end identity graph is
`examples/control-plane-e2e/fixture.json`. Its exact checked-in identities are:

| Kind                   | Target alpha                                                              | Target beta                                                               |
| ---------------------- | ------------------------------------------------------------------------- | ------------------------------------------------------------------------- |
| Target                 | `target-alpha`                                                            | `target-beta`                                                             |
| Runtime target binding | `11111111-1111-4111-8111-111111111111`                                    | `22222222-2222-4222-8222-222222222222`                                    |
| Adapter assignment     | `adapter-http-alpha`                                                      | `adapter-reference-beta`                                                  |
| Adapter kind/revision  | `http-external` / `external-http@1.0.0`                                   | `reference` / `reference@1.0.0`                                           |
| Executor               | `executor-http-alpha`                                                     | `executor-reference-beta`                                                 |
| Independent observer   | `observer-alpha`                                                          | `observer-beta`                                                           |
| Config snapshot        | `config-alpha-v2`                                                         | `config-beta-v2`                                                          |
| Config checksum        | `sha256:1111111111111111111111111111111111111111111111111111111111111111` | `sha256:4444444444444444444444444444444444444444444444444444444444444444` |
| Capability checksum    | `sha256:2222222222222222222222222222222222222222222222222222222222222222` | `sha256:5555555555555555555555555555555555555555555555555555555555555555` |
| Topology checksum      | `sha256:3333333333333333333333333333333333333333333333333333333333333333` | `sha256:6666666666666666666666666666666666666666666666666666666666666666` |
| Target profile         | `region-one`, port `18081`, `standard`                                    | `region-two`, port `18082`, `restricted`                                  |

The shared synthetic tenancy is `tenant-neutral` /
`organization-neutral` / `environment-validation`. Release A is Component
Release `component-release-a`, Product Release `product-release-a`, version
`1.0.0`, schema `1`, and digest
`sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa`.
Release B is Component Release `component-release-b`, Product Release
`product-release-b`, version `1.1.0`, schema `2`, and digest
`sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb`.

The Product identity is `neutral-product`. `catalog-provider` publishes
`catalog.v1` from artifact digest
`sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc`;
`gateway-consumer` requires that capability and uses artifact digest
`sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd`.
The retry-safe `oci-job` migration is `migration-001`, digest
`sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee`,
idempotency key `migration-001:neutral-product:1-to-2`, and declares schema
`1` to `2`.

Campaign `campaign-a-to-b`, revision `1`, freezes target alpha in wave 1 and
target beta in wave 2. Policy checksum
`sha256:7777777777777777777777777777777777777777777777777777777777777777`
requires two plan-checksum-bound, separated approvals in the UTC window
`2030-01-01T00:00:00Z` through `2030-01-01T01:00:00Z`. Execution uses
protocol v2, fence `fence-2` at generation `2`, lease generation `3`, a
5,000 ms timeout, and a 65,536-byte fixture log bound.

The fixture does not currently assign separate deployment-unit, plan, approval,
task, attempt, event, observation, desired-state, signing-key-fingerprint,
manifest-checksum, graph-checksum, or change-log IDs. Those records are
represented by the ordered `flow.stages`, target/config/release checksums,
synthetic evidence rows, and the result `flowChecksum`. Do not describe absent
identities as frozen production records. A future live proof must retain those
exact IDs and checksums from the Hub.

The two targets use separately configured adapters and separately registered
observers while consuming the same fixture Release A/B identities and digests.
Target-specific configuration is frozen separately; equality of a release
digest does not imply equality of target configuration, execution authority, or
observation authority.

## Source manifest and release baseline

PR-081 is proof tooling on the PR-080 checkpoint `bf877e5a`. The repository
manifest remains `@distr-sh/distr` version `2.24.1`, and the latest checked-in
`CHANGELOG.md` release is `2.24.1`; PR-081 does not claim a package version or
changelog release of its own. `mise.toml` pins Node.js `26`, pnpm `11.7.0`, and
Go `1.26.5`. A retained result records the exact executing versions because a
developer's ambient tools can differ from the manifest.

The neutral fixture freezes exact A/B versions and digests but no SemVer range.
Its dependency constraint is the explicit capability edge
`gateway-consumer requires catalog.v1 from catalog-provider`; its schema
transition constraint is `1` to `2` through retry-safe `migration-001`.
Neither the repository changelog nor a higher fixture version establishes
target compatibility.

## Evidence lineage

The clean reference run follows one forward-only evidence chain:

```text
Component Release contracts and change logs
  -> Product Release manifest and capability DAG
  -> immutable target configuration snapshots
  -> target-bound deployment plans
  -> approval and maintenance-window decisions
  -> immutable campaign revision and two waves
  -> protocol-v2 task, attempt, intent, fence, and ordered events
  -> separately authenticated observations
  -> desired-state gate
  -> active state or explicit drift/reconciliation evidence
```

Every child record must retain its parent IDs and relevant immutable checksums.
The migration in the fixture is retry-safe but is not evidence that an older
release is rollback-safe. Previous-state B-to-A behavior must use the frozen
previous-state evidence and constraints; it must not infer compatibility from
version ordering.

## Feature flags and compatibility

PR-081 exercises the default-off operator and executor-v2 boundaries. The
retained result metadata must record the complete effective feature-flag set.
At minimum:

- operator control-plane routes and orchestration require
  `operator_control_plane_v2`;
- protocol-v2 dispatch and controls additionally require
  `executor_protocol_v2`;
- fixture booleans additionally assert organization/environment enrollment,
  v1 regression, and v2 kill-switch cases. These are proof controls, not
  additional `DISTR_EXPERIMENTAL_FEATURE_FLAGS` keys.

The failure matrix must verify the v2 kill switch and an unchanged v1
compatibility path. Turning a flag off is a denial boundary, not permission to
fall back silently from a v2 intent to v1 execution.

## Security boundary

- The reference executor requires a UUID runtime target binding, a bearer
  secret, and a JSON map from public-key fingerprint to Ed25519 public key. Its
  operation, status, cancel, and log routes are authenticated; only `/ready` is
  unauthenticated.
- Intent signing keys and observer keys are independent. A key trusted for one
  role is not trusted for the other.
- Fixtures and reports contain only public fingerprints and opaque references;
  private keys, bearer tokens, secret values, and connection strings are never
  retained.
- Organization, target, task, step, plan, adapter revision, resource,
  configuration checksum, signing-key fingerprint, lease, and fence identities
  remain exact through dispatch and completion.
- Duplicate dispatches and events are accepted only as exact idempotent replay.
  Changed payloads under a reused identity fail closed.
- Stale fences, expired authority, late callbacks, mismatched observations, and
  unresolved/unknown evidence cannot activate desired state.
- Observer evidence is independently authenticated and cannot be replaced by an
  executor's self-report.
- Bounded logs and result payloads prevent proof tooling from becoming an
  unbounded memory or disclosure path.
- Reference-executor request bodies are capped at 64 KiB. Generated logs
  default to 8,192 bytes and cannot be configured above 65,536 bytes. State is
  atomically replaced with mode `0600` and reloaded after restart; the persisted
  form excludes bearer credentials and raw operation specs.
- Every read-model and load result checks that no isolation-sentinel identity is
  returned to the primary organization.

## Performance evidence

Section 20.9 of the control-plane design defines six independent
test-environment SLOs. Record each one separately:

| Scenario                                                       | Required evidence                                                                                                                        |
| -------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- |
| Fleet/list/detail at 1,000 targets and at least 649 placements | Warm page-size-100 p50/p95/p99 per workload, bounded response, execution mode, database/network metadata, and zero isolation violations  |
| Create/validate a 100-component target plan                    | Five identical runs, raw timings, p95 at or below 10 seconds, and identical deterministic checksums                                      |
| Materialize/schedule a 500-step aggregate wave                 | Raw duration at or below 30 seconds, stable order, and no duplicate admission                                                            |
| 100 concurrent online executors                                | Ten-minute run at 100 authenticated events/second, raw acknowledgement percentiles, accepted/lost counts, and p95 at or below one second |
| 100 MiB log/evidence path                                      | Streaming/memory evidence and first indexed page within two seconds                                                                      |
| Isolation/error budget                                         | Zero cross-organization records and below one percent non-policy server errors                                                           |

The deterministic fixture mode of
`hack/control-plane-read-model-benchmark.mjs` measures local JavaScript
projection/sorting only. It is useful for fixture, pagination, cursor, bound,
and isolation regressions; it is not database/API latency evidence. Remote mode
measures the configured HTTP boundary, but still proves only the requests and
environment described in its result metadata.

## Result metadata and checksums

Retain the raw, machine-readable reports without rewriting their percentile
values. Alongside them record:

- UTC start/end time and wall-clock duration;
- source commit and dirty-state indicator;
- package version, changelog release, Node.js, pnpm, Go, OS, architecture, CPU,
  logical cores, and memory;
- mode (`simulated`, `local-compose`, or `remote`) and cold/warm state;
- database engine/version, row counts, network placement, and external-probe
  inclusion/exclusion where applicable;
- fixture schema, seed, parameters, and SHA-256;
- full non-secret feature-flag set and bounded concurrency/rate settings;
- report schema, sample counts, p50/p95/p99, accepted/lost/error counts,
  isolation violations, and every asserted threshold;
- SHA-256 for each raw fixture, report, log index, and evidence manifest.

Checksums establish byte identity only. They do not upgrade simulated evidence
to live evidence or prove that an unrecorded external dependency participated.

## Limitations

- Contract mode runs `simulateContractFlow` entirely in memory.
- Clean mode can start a loopback-only Hub, PostgreSQL, two executors, and two
  observers and wait for readiness, but the current release-flow result still
  comes from `simulateContractFlow`; it does not call the Hub release,
  planning, campaign, execution, or observation APIs.
- Clean mode generates an ephemeral reference-executor trust-key entry and
  public fingerprint in memory so the service can start. Because the runner
  sends no signed intent to that service, readiness is not signed-intent
  execution evidence.
- If Docker, its daemon, or a Hub binary is unavailable, clean mode deliberately
  falls back to fixture-contract mode and can still exit successfully. Inspect
  `proofMode`, `liveStack.started`, `liveStack.blocker`, `nonLocalCalls`, and
  cleanup metadata before classifying the result.
- The failure matrix defaults to fixture mode. Its HTTP mode posts one case at
  a time to `/api/v1/control-plane/failure-matrix`, a proof endpoint that a
  compatible loopback test server must explicitly provide. It rejects
  non-loopback base URLs and disables redirects. Its report schema is
  `distr.control-plane-failure-matrix-report/v1` and retains fixture identity,
  mode, status, ordered per-case checks/checksums, and a report checksum.
- The load proof reports `mode: simulation`, `measurement:
deterministic-simulation`, an in-process network/database, simulated
  authentication, virtual bounded log pages, and time compression. Its
  percentiles are not live SLO measurements. Supplying a permitted loopback
  `--base-url` switches to `mode: remote`, `measurement: measured-live`, and no
  time compression; that result still covers only the explicit local proof
  server and requested duration/rate.
- Reference adapters demonstrate the documented protocol contract; they are not
  production executors or observers.
- The reference executor has no TLS termination, distributed state, signing-key
  revocation, retention engine, or real workload adapter. Those remain
  deployment/integration responsibilities.
- Local Compose and deterministic Node.js modes do not establish staging,
  production, multi-host, failover, database contention, or real-network SLOs.
- Test keys and synthetic digests do not validate production key custody,
  registry availability, artifact provenance, or secret-provider integration.
- A successful clean run does not authorize schema rollback, destructive
  recovery, or deployment of an incompatible previous release.
- Missing, partial, stale, unknown, or unretained evidence remains a failed
  acceptance item.
