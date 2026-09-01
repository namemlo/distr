# PR-092: Durable Independent Observer Service

## Purpose

Turn the existing Choice TP one-shot observer into an independently credentialed, restart-safe scheduled service
without adding a Hub API, database dependency, executor dependency, or live mutation path.

## Generic user story

As an operator, I need independent observation to survive service restarts and partial callback failure so that the
same signed runtime evidence is safely completed instead of being silently replaced by a new measurement.

## Behavior

- A checksummed service configuration binds the fixed target profile, exact observer-token scope, Customer and
  Transaction Component Instances, credentials, state paths, retry limits, and C0/T0/C1/T1 separation.
- Signed evidence is exclusively persisted before submission. Restart verifies and replays that exact artifact;
  it does not remeasure under reused component sequences.
- Completed intents are skipped. Polling, inbox size, work per poll, retry count, backoff, and total intent attempts
  are bounded.
- Only transport errors and HTTP `408`, `429`, or `5xx` are retried. Authentication, scope, validation, and
  oversized-response errors fail without retry.
- A stale-bounded local lock prevents overlapping service instances.
- Current C1/T1 observations include independently measured artifact/config/schema/capability/platform/topology,
  exact health, `STANDARD_READINESS` / `STANDARD_PROMOTION_ELIGIBLE`, and a checksum-bound evidence reference.
- Existing Fleet and execution read models expose the persisted observation artifact digest, config checksum,
  platform, schema version, capability checksum, and health. Fleet also exposes the evidence checksum when exactly
  one current accepted observation supplies it; conflicting observations do not invent a singular identity.

## Credential and legacy boundary

The service requires a dedicated SSH key, pinned explicit `known_hosts`, scoped observer token, and separate Ed25519
evidence key. Their fingerprints must be mutually distinct and must not match any configured Jenkins/executor
credential fingerprint. Observer and executor credential-set IDs must also differ. No credential is accepted from
environment variables.

The reviewed C0/T0 evidence file is pinned as
`sha256:cbebf0295b9eda637afc207f03a28a3c67a99c2d701c5ca99697176ff5343429`, with exact component artifact and
configuration digests. Startup requires its legacy classification and rollback-only use. C1/T1 intents cannot
reuse the C0/T0 artifact digests.

## Packaging and operations

- A minimal Node/OpenSSH container runs as UID/GID 10001 with a read-only root filesystem, no capabilities, no
  privilege gain, observer-only read mounts, and writable evidence/state mounts.
- Compose requires a production image reference containing an immutable digest.
- systemd oneshot/timer units use a strict filesystem/device/kernel sandbox and an unprivileged observer account.
- The install/recovery/rollback runbook preserves evidence and state, prohibits sequence reuse, and never authorizes
  a client runtime or database mutation.

## Compatibility and scope

There is no new Hub route, database migration, agent protocol, or core feature-flag change. The existing Fleet and
execution API responses add observation identity fields, and their existing UI views render those native values.
The one-shot CLI remains available. Its current request builder now supplies the health-policy fields introduced by
the existing observation API and uses the canonical `evidence://sha256/<digest>` reference.

No live Choice TP, Distr, Jenkins, Docker, registry, or database system is contacted by this change or its tests.

## Verification

```shell
node --test \
  examples/choice-tp-observer/observer.test.mjs \
  examples/choice-tp-observer/service.test.mjs

go test ./internal/db -run \
  'TestOperatorFleetSQLKeepsSharedUnitSingleAndProjectsOperationalStates|TestOperatorExecutionDetailSQLScopesEveryEvidenceBranchToTenantAndExecution|TestOperatorExecutionListSQLIncludesControlObservationAndPreviousStateEvidence|TestListOperatorFleetRowsUsesOneBoundedQueryAndDecodesEmptyAndPartialRows' \
  -count=1

pnpm exec ng test ui --watch=false \
  --include frontend/ui/src/app/control-plane/fleet/fleet.component.spec.ts \
  --include frontend/ui/src/app/control-plane/executions/execution-detail.component.spec.ts
```

The focused suite covers restart replay, partial submission, terminal retry bounds, completed-intent suppression,
scope and credential pins, known-host validation, immutable legacy evidence, C0/T0 versus C1/T1 separation,
standard-readiness health evidence, the native Fleet/execution identity surface, and the original bounded observer
behavior.
