# Neutral Control-Plane End-to-End Fixture

This fixture proves the control-plane contract with two separately configured neutral targets:

- `target-alpha` uses the HTTP external executor `adapter-http-alpha` and `observer-alpha`.
- `target-beta` uses the deterministic reference executor `adapter-reference-beta` and `observer-beta`.

Both targets receive the same immutable A and B release digests while retaining distinct target-config, capability,
topology, adapter, and observer identities. The product graph includes a provider/consumer capability binding, one
retry-safe migration, two campaign waves, checksum-bound approvals, a maintenance window, independent observations,
and an A-to-B-to-A previous-state flow.

## Fast contract proof

The contract mode requires only Node:

```shell
node examples/control-plane-e2e/run.mjs --mode contract
```

It validates `fixture.json`, executes the deterministic publish-to-observation state model, applies the migration once
across a replay, advances both independent observation sequences, and verifies the final active A state. It never
contacts a Hub, container runtime, cloud endpoint, or non-loopback address.

Machine-readable output is available with:

```shell
node examples/control-plane-e2e/run.mjs --mode contract --json
```

## Disposable local stack

Run the clean workflow from the repository root:

```shell
node examples/control-plane-e2e/run.mjs --mode clean
```

When Docker and a prebuilt local Hub image are available, the runner:

1. generates a unique Compose project ID and random secrets in memory;
2. removes only stale resources under that unique project ID;
3. starts isolated PostgreSQL and the supplied Hub image, then registers a disposable organization through Hub APIs;
4. enrolls that organization and environment in the v2 control plane, creates the two targets through Hub APIs, reports
   each target agent's exact `distr.compose.deploy@1.0.0` and `distr.preflight@1` capabilities, and starts target-bound
   executors and observers with the Hub-returned IDs;
5. publishes component releases and the product capability DAG, freezes per-target configs, and publishes approved plans;
6. records two decisions from separately invited approvers, obtains approval-bound `ADMIT` evaluations, publishes
   two-wave campaigns, and verifies a task-bound `PASSED` preflight before dispatch;
7. executes and completes the live A-to-B-to-A path, sends independent observations, and verifies final A in the fleet read model;
8. runs `docker compose down -v --remove-orphans`, enumerates project-labelled containers, volumes, and networks, and
   fails the run if teardown or absence of retained resources cannot be confirmed.

Set `DISTR_CP_HUB_IMAGE` to a prebuilt local Hub image. The runner uses `pull_policy: never` for every service and checks
that all required images are already present, so it never contacts a remote registry. If Docker, the local images, or
the offline Go module cache is unavailable, clean mode runs the independently executable fixture-contract proof, exits
successfully when that proof passes, and reports the exact live-stack blocker. It does not label the fallback as live
proof.

`DISTR_CP_FORCE_CONTRACT=true` deterministically exercises that fallback in tests.

## Executor and observer boundaries

Both executor implementations use the same bounded operation API:

```text
POST /v1/operations
GET  /v1/operations/{id}
POST /v1/operations/{id}/cancel
GET  /v1/operations/{id}/logs
```

An operation carries a signed v2 intent and explicit tenant, target, attempt, operation, idempotency, task, step, plan,
adapter, resource, fence, issued-at, and expiry bindings. The external executor validates authority against an injected
clock, handles an exact replay before fencing, and requires every new operation to advance the fence. It rejects outer
identity rebinding, expired or not-yet-valid authority, target mismatches, non-retry-safe migrations, and conflicting
idempotency-key reuse. Logs are bounded and redact authorization/signature material. The reference executor trusts the
public half of the Hub signing pair and derives the successful operation only from the verified signed intent and its
immutable artifact, target-config, adapter, resource, and fence bindings. It rejects unsigned request fields such as a
local operation spec. V2 lease requests use the SHA-256 revision derived from the exact adapter evidence frozen into the
published plan, never a fixture-supplied adapter label. Independent observer trust uses a separately generated Ed25519 key.

Each observer exposes:

```text
POST /v1/observations
GET  /v1/observations/latest
```

Observers are separately registered and target-bound. They accept only increasing sequences, allow an identical replay,
reject a conflicting or stale sequence, and derive an immutable evidence checksum from release, config, capability,
topology, schema, and health evidence.

## Tests

```shell
node --test examples/control-plane-e2e/contract.test.mjs
go test ./examples/control-plane-e2e/reference-executor -count=1 -race
node examples/control-plane-e2e/run.mjs --mode clean
```

The Node contract test starts the real local HTTP executor and observer servers on unused loopback ports. It verifies
idempotency, signed outer-identity binding, authority expiry, strict fencing, cancellation, bounded redacted logs,
observer identity/target binding, sequence replay, phased live bootstrap with Hub-created target IDs, separated trust
keys, organization/environment enrollment, exact agent capability reports, approval-bound admission, task-bound passed
preflight, frozen-adapter lease identity, and the frozen 14-case failure-matrix schema.

## Safety boundary

- All published Compose ports bind to `127.0.0.1`.
- The Compose network is internal and volumes are project-scoped.
- Secrets are randomly generated in process memory and are neither printed nor written to fixture files.
- The runner does not read ambient remote-host URLs or credentials.
- Cleanup names the exact generated Compose project; it never prunes global containers, networks, images, or volumes,
  and success is withheld unless scoped teardown and resource absence are confirmed.
- Contract-mode output is simulated fixture evidence. A fallback result is not evidence of a running Hub or database.
