# PR-090 - Baseline Adoption Fact Separation

## Purpose

Keep Component Release identity and independently observed runtime identity
separate during native baseline adoption.

## Contract

- `applicationVersion` is frozen from the exact Component Release pin in the
  sealed deployment plan.
- `schemaVersion` is frozen from the exact authenticated current observation.
- `capabilityChecksum` is frozen from the same observation and may differ from
  the Component Release canonical checksum.
- Release pin, artifact, provenance, config, topology, observation, desired
  state, and audit evidence remain exact and append-only.
- The canonical request checksum domain is unchanged, so retained idempotency
  keys and historical adoption checksums remain valid.

## Surface

Migration 169 adds the read-only response field `applicationVersion` to each
baseline-adoption component record and backfills it from retained plan pins.
The existing `schemaVersion` and `capabilityChecksum` request fields remain
independent observation facts. No executor, agent, UI, client database, or
workload protocol is changed.

## Verification Boundary

Focused Go and migration-contract tests cover validation, checksum sensitivity,
read projection, upgrade backfill, deferred database enforcement, and safe
downgrade shape. Final PostgreSQL certification must cover the complete ordered
138-to-169 range after the reserved migrations 167-168 are present in the
integration branch.
