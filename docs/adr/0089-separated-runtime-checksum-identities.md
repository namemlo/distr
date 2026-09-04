# ADR-0089: Separated Runtime Checksum Identities

## Status

Accepted

## Context

Executor runtime contract v3 uses `configChecksum` for the logical Target
Config Snapshot before and after execution. Runtime adapters also need the
checksum of the immutable runtime-manifest document and the byte checksum of
the deployed service configuration. Treating any of these identities as
interchangeable makes manifest lookup, target compare-and-swap, evidence, and
rollback ambiguous.

Historical attempts and signed v3 intents are immutable evidence and cannot be
rewritten with inferred physical checksums.

## Decision

Runtime contract `v4` and signed intent schema
`distr.execution-intent/v4` add three immutable inputs:

- `runtimeManifestChecksum`: the checksum used to address the immutable
  runtime-manifest document;
- `desiredServiceConfigChecksum`: the desired deployed service-config bytes;
- `expectedCurrentServiceConfigChecksum`: the expected-current deployed
  service-config bytes.

`configChecksum` remains the desired logical Target Config Snapshot checksum,
and `expectedCurrentConfigChecksum` remains the expected-current logical
snapshot checksum. Neither field may contain a physical file checksum.

Runtime-evidence schema `distr.execution-runtime-evidence/v2` adds
`preExecutionServiceConfigChecksum` and `resultServiceConfigChecksum` while
retaining the logical pre/result snapshot checksums. A successful v4 attempt
requires v2 evidence matching every signed logical and physical identity.

Migration 172 adds nullable columns. Existing `legacy-v2` and `v3` attempts
and v1 evidence retain SQL `NULL`; v4 attempts and v2 evidence require complete
lowercase SHA-256 values. The attempt trigger makes the new fields immutable.
Downgrade refuses once v4 attempts or v2 physical evidence exist.

The application continues creating v3 attempts until an authoritative
runtime-manifest publication integration supplies all three v4 values. It
must not fabricate them from the logical snapshot checksum.

## Consequences

- Signed intents, lease responses, retries, persistence, operator reads, and
  the reference executor preserve distinct checksum identities.
- Existing v3 payload bytes, signatures, rows, and evidence remain unchanged.
- Runtime-manifest publication must provide the complete v4 tuple before v4
  execution can be enabled.
- Independent observation remains authoritative for desired-state promotion.

## Alternatives Considered

- Reuse `configChecksum` for service bytes: rejected because it destroys the
  logical Target Config Snapshot identity.
- Derive the runtime-manifest checksum from the plan: rejected because the
  manifest is a separate immutable document and may include the plan checksum.
- Backfill physical checksums: rejected because historical evidence does not
  prove those values.

## Validation

- v3 golden signed-intent compatibility plus v4 signing and tamper tests.
- v3/v4 persistence, retry, discovery, API, reference-executor, and operator
  read-model tests.
- v1/v2 runtime-evidence validation, canonical binding, replay, and success
  completion checks.
- Migration 172 static and disposable-PostgreSQL upgrade/down/refusal tests.
- Protected-history schema-172 projection and retention range tests.
