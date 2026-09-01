# ADR-0079: Durable Independent Observer Service

## Status

Accepted

## Context

The Choice TP observer was a secure one-shot command, but it had no durable scheduler or restart protocol. A stop
after signing evidence or after only one component submission could lead an operator to run the intent again. A new
measurement under the same component source sequence would conflict with retained evidence, while abandoning the
intent could leave Customer and Transaction observation state incomplete.

The observer also needs an explicit operational boundary from Jenkins and the executor. File paths and distinct
credential-set names alone do not prove that the underlying SSH key, token, or signing key was independently
generated. The historical C0/T0 artifact must remain legacy baseline/rollback evidence and must not be confused with
current C1/T1 runtime readiness.

## Decision

Package the adopter-side observer as a dependency-free Node.js service that reads immutable intent files from a
bounded local inbox. It supports a continuous container loop and a systemd oneshot/timer. It remains outside Distr
core and uses no database, agent, executor, or new Hub API.

For each intent, the service:

1. validates a checksummed configuration, exact organization/observer/Deployment Unit/Component Instance scope,
   target profile, token fingerprint, pinned `known_hosts`, credential separation, and C0/T0 evidence pin;
2. measures either the sealed Customer C1/Transaction T1 standard-readiness profile or the exact C0/T0 legacy
   liveness profile through the existing read-only observer command surface;
3. signs one evidence envelope with a separate Ed25519 key and exclusively persists it before submission;
4. submits both component requests with standard-readiness health policy lineage; and
5. records completion through atomically replaced local state.

If execution stops after evidence persistence, restart verifies the retained evidence checksum, signature key,
intent/profile scope, and component sequences, then replays the exact requests. It never remeasures that intent.
Identical replay uses the existing Distr observation idempotency rule.

Polling, directory size, intents per poll, SSH/HTTP deadlines, response sizes, retries per poll, delay, and total
attempts per intent are bounded. Only transport failures and HTTP `408`, `429`, or `5xx` are retried. A lock prevents
overlap and has an explicit stale threshold.

The service configuration pins:

- a scoped observer token fingerprint;
- distinct SSH, token, and evidence-signing files;
- known Jenkins/executor credential fingerprints that no observer credential may match;
- different observer and executor credential-set IDs;
- the exact legacy C0/T0 artifact checksum and component artifact/config digests; and
- the separate current C1/T1 state labels and component scope.

C0/T0 uses only the exact profile-pinned runtime-state helper and legacy Swagger liveness paths, and must retain
`LEGACY_LIVENESS_ONLY` / `BASELINE_OR_ROLLBACK_ONLY`. C1/T1 uses only bounded HTTP runtime metadata and
`STANDARD_READINESS` / `STANDARD_PROMOTION_ELIGIBLE`, an `evidence://sha256/<digest>` reference, and a checksum of
the fixed logical `/alive`-then-`/healthz` policy. The intent cannot select the mode. C1/T1 intents are rejected if
either candidate artifact digest reuses its corresponding C0/T0 digest.

The service writes a durable heartbeat separately from intent history. Liveness verifies a fresh heartbeat;
readiness additionally requires the latest poll to be successful. Config-only upgrades may migrate terminal
`COMPLETE`/`EXHAUSTED` history after proving the target, profile, checkpoint, scope, and legacy pins are unchanged;
the migration retains an exclusive backup and checksum receipt and refuses pending history.

The existing Fleet and execution read models expose the persisted artifact digest, config checksum, platform,
schema version, capability checksum, and health directly from `ObservedComponentState`. Fleet exposes a singular
runtime identity only when current accepted observations agree on one state; it leaves identity fields empty for a
conflict. The execution observation fact remains bound to the exact verified or terminal observation row and its
evidence checksum. This is an additive API projection and requires no schema or route change.

## Consequences

A restart cannot create conflicting evidence under the same source sequence, including the partial-submission
case. Completed intents are not probed or submitted again, while uncertain persisted evidence can be replayed
without changing capture time or signature.

Deployments can use a hardened read-only container or a systemd sandbox. Credentials are file-mounted/read, never
environment-driven, and packaging has no Jenkins workspace, executor secret, Docker socket, or arbitrary command
surface.

Operators can inspect exact component runtime identities in Fleet and execution detail without reducing Customer
and Transaction observations to a shared checkpoint label. Conflicting Fleet evidence remains visibly conflicted
and does not present an arbitrary digest/config/platform/schema/capability/health tuple as authoritative.

The service is intentionally bound to the reviewed Choice TP C1/T1 checkpoint. Observing a different checkpoint,
target, or component set requires a separately reviewed checksummed configuration rather than runtime overrides.
Exhausted intents remain retained and require operator reconciliation or a fresh higher-sequence intent; evidence
and service state must not be deleted to manufacture a retry.

## Alternatives Considered

- Remeasuring on every restart was rejected because it can create different material under a reused sequence.
- Polling a user-authenticated management endpoint was rejected because no observer-intent route exists and it
  would broaden the credential boundary.
- Using Jenkins scheduling or Jenkins credentials was rejected because the observer must remain independent from
  the execution authority it verifies.
- Storing credentials in environment variables was rejected because they are easier to expose through process,
  container, and diagnostic surfaces.
- Relabeling the C0/T0 liveness artifact as current readiness was rejected because it would erase the explicit
  legacy evidence limitation.

## Validation

Focused Node tests use temporary files and in-memory SSH/HTTP adapters. They cover persisted replay after partial
submission, terminal-file inbox starvation, retry/attempt exhaustion, exact scope and credential separation,
pinned known-hosts and C0/T0 evidence validation, sealed C0/T0 and C1/T1 mode classification, health/readiness,
terminal-only state migration, and the existing bounded measurement/signature behavior. Focused read-model and
Angular tests cover the native
observation identity projection and rendering. No live system or database is contacted.
