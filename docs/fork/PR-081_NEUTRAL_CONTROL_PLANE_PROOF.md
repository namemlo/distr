# PR-081 — Neutral End-to-End and Performance Proof

## Scope

PR-081 adds a synthetic, reproducible control-plane reference fixture. It
connects two separately configured targets to the same immutable release graph:
one target uses the HTTP external-executor adapter and the other uses the
deterministic reference executor. Each target has its own independently
registered observer.

The proof covers Component and Product Release lineage, target configuration
snapshots, target-bound plans, approval/window decisions, a two-wave campaign,
protocol-v2 execution, independent observation, desired-state activation, fault
injection, and roadmap-scale performance fixtures. It adds no adopter-specific
behavior, production credentials, or new write authority.

## Identity and lineage contract

`examples/control-plane-e2e/fixture.json` is the canonical neutral identity
source. The two targets share Component and Product Release identities and
digests, but have distinct logical target, runtime binding, configuration
snapshot, adapter assignment/revision, executor, and observer identities.

The logical identities are `target-alpha` / `adapter-http-alpha` /
`external-http@1.0.0` / `executor-http-alpha` / `observer-alpha` /
`config-alpha-v2` and `target-beta` / `adapter-reference-beta` /
`reference@1.0.0` / `executor-reference-beta` / `observer-beta` /
`config-beta-v2`. Release A is `component-release-a` /
`product-release-a` / `1.0.0` / schema `1`; Release B is
`component-release-b` / `product-release-b` / `1.1.0` / schema `2`.
`neutral-product` binds consumer `gateway-consumer` to provider
`catalog-provider` for `catalog.v1`. `migration-001` is an explicitly
retry-safe `oci-job` from schema `1` to `2`.

The frozen lineage is:

```text
component contract/change log
  -> product manifest/capability graph
  -> target configuration snapshot
  -> deployment plan and approval/window evidence
  -> campaign revision/wave/member
  -> task/attempt/signed intent/fence/events
  -> independent observation
  -> desired-state activation or drift/reconciliation evidence
```

Provider/consumer requirements and version constraints are declared in the
Component contracts and frozen Product manifest. The fixture includes one
retry-safe migration. Neither retry safety nor semantic version order implies
that previous-state deployment is safe; B-to-A behavior remains constrained by
the retained previous-state evidence.

The contract fixture does not allocate persisted Hub IDs for each plan,
approval, task, attempt, event, observation, desired-state revision, manifest,
graph, signing-key fingerprint, or change-log record. Its stage order,
synthetic evidence, immutable input checksums, and final flow checksum model
that lineage. A live result must retain the actual Hub IDs/checksums and must
not relabel these simulated identities as persisted evidence.

## Feature flags and compatibility

The reference workflow is default-off. Operator behavior requires
`operator_control_plane_v2`; protocol-v2 dispatch and execution controls also
require `executor_protocol_v2`. The fixture records every additional release,
environment, lifecycle, channel, process, approval, and planning prerequisite
that it enables.

The failure matrix includes an explicit executor-v2 kill-switch case and a v1
regression case. Disabled v2 behavior fails closed and cannot silently execute
through v1.

## Security boundary

The reference executor verifies signed intents, exact task/plan/step/target/
adapter/configuration binding, monotonically increasing fences, expiry,
idempotent replay, bounded logs, cancellation/status, and restart persistence.
Independent observers use separate registrations and trust keys. Executor
self-report cannot satisfy the independent-observation gate.

Private signing keys, bearer tokens, credentials, secret values, and connection
strings are excluded from fixtures and retained reports. Only opaque references
and public fingerprints are evidence. Cross-organization sentinel records must
remain absent from every primary-organization result.

The executor is a loopback reference implementation, not a production adapter:
it provides no TLS termination, distributed state, real workload execution,
key-revocation service, or retention engine. The documented race command also
requires CGO and a C compiler; a non-race test/vet result must remain labeled as
such when the host cannot build the race runtime.

## Failure and recovery coverage

The deterministic failure matrix exercises duplicate dispatch and events,
pre-acknowledgement and post-acknowledgement crash, stale fence, callback loss,
timeout, cancellation, restart, observer mismatch, drift/reconciliation,
previous-state B-to-A, v1 compatibility, and the v2 kill switch. A scenario is
passed in clean/live executable mode only when its terminal state, behavior
checks, and retained evidence match the explicit expectation. Default fixture
simulation is explicitly non-acceptance; unknown or partial evidence is never
projected as success.

## Performance proof boundary

The scale generator creates at least 1,000 targets, 649 placements, 100 online
agents/executors, 100 components, and a 500-step aggregate wave with a separate
organization-isolation sentinel. Machine-readable reports retain raw p50/p95/
p99, sample counts, thresholds, mode, dataset seed, and isolation outcomes.

Fixture mode is an in-process deterministic harness and is not a live database
or API measurement. Remote mode measures only the configured HTTP boundary.
Plan creation, wave materialization, ten-minute authenticated ingest, 100 MiB
log streaming, database behavior, and production-like networking require their
own recorded measurements; no one subtest substitutes for another section
20.9 SLO.

The load tool currently reports deterministic simulation with time compression,
in-process networking and fixture storage, simulated authentication, and
virtual bounded log pages. The failure matrix defaults to
`NON_ACCEPTANCE_FIXTURE_SIMULATION`, with `acceptanceEligible: false`. Clean
mode starts an owned stateful adapter on an ephemeral loopback port; live mode
drives a separately managed compatible loopback adapter. Those executable modes
post ordered actions to `/api/v1/control-plane/failure-matrix/actions` without
sending expected outcomes. They prove the bounded failure-injection behavior,
not the full Hub, database, executor, observer, or network release flow.

The load tool also supports an explicit loopback-only `measured-live` mode. It
uses environment-only bearer authentication, forbids redirects and cross-origin
paths, paces events in wall-clock time, streams bounded pages, and distinguishes
a short smoke from the complete ten-minute/100-events-per-second acceptance
profile. No feature flag is consumed by the load harness itself.

## Release evidence

The operator-facing reproduction guide is
`docs/release/control-plane-neutral-proof.md`. Acceptance evidence must bind:

- source commit, dirty state, package/changelog version, and toolchain;
- fixture schema, seed, exact identities, parameters, and SHA-256;
- non-secret feature flags and execution mode;
- hardware, OS/architecture, database, network, concurrency, and warm/cold
  metadata;
- raw reports, percentiles, counts, errors, isolation results, and SHA-256
  sidecars.

The reports prove only the recorded local simulated, local Compose, or remote
environment. They are not automatically staging or production proof.

Clean mode may start loopback-only services and verify readiness, but its
release-flow result is still produced by the deterministic contract model. If
Docker, its daemon, or a Hub binary is unavailable, clean mode falls back to
contract mode and may exit successfully while recording the blocker. Consumers
must inspect `proofMode`, `liveStack.started`, `liveStack.blocker`, and cleanup
metadata before classifying the result.

## Root-verified result

Integrated local verification passed the reference-executor focused tests and
vet, all 28 Node.js tests, the exact ten-minute/100-rate deterministic load
simulation, and both failure-matrix classifications: the default non-acceptance
fixture simulation evaluated all 14 cases, while a separate clean executable
loopback run passed all 14 ordered-action cases.

The clean runner exited zero only in `fixture-contract` mode because the Docker
CLI was unavailable. It recorded `liveStack.started: false`, zero non-local
calls, completed cleanup, release history A-B-A, and flow checksum
`sha256:fc31db2b0aa7d56fd08622508be575ccc709a5c473c4efa04ca5005b1a8d8dd0`.
The failure-matrix fixture checksum was
`sha256:346f08b0bd4904b5d52982538cb89da8510b9228376120e56e5d6f28f073388a`.
The non-acceptance fixture-simulation report checksum was
`sha256:95e1959d13e0e32a882de2c7c74ccd48ef88ab82259bb968f2fa0942297a09ae`;
the clean executable loopback report checksum was
`sha256:cfccb61038cc6771e8f0742643d1cb72684d794751bf041d24efc61a8838613d`.

The time-compressed load result covered 60,000 simulated events with 5.564 ms
acknowledgement p95 and zero loss, tenant leakage, or non-policy errors.
Planning p95 was 6.844 ms across five deterministic-checksum runs, and the
500-step wave took 20.5 ms with stable order and no duplicate admission.

Race verification remains blocked first by the repository's CGO-disabled
default and then by the absence of GCC. No full live remote/Compose, Hub API
end-to-end, staging, or production result exists. The clean failure-matrix proof
does not resolve that full runtime gate, so overall PR-081 acceptance remains
pending runtime review.

## Database, API, UI, and agent impact

- Database changes: None.
- API changes: None to production routes; the reference tools consume existing
  control-plane contracts.
- UI changes: None.
- Agent protocol changes: None; the reference executor implements and tests the
  existing protocol-v2 contract.
- Production configuration changes: None. Proof-only feature flags are supplied
  explicitly by the reference environment.

## Upstream contribution notes

The fixture uses neutral organizations, targets, components, adapters,
observers, versions, and digests. It contains no adopter/client names, private
paths, infrastructure-provider assumptions, credentials, or proprietary
artifacts.
