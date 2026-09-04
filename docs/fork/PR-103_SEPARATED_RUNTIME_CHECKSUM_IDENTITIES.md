# Post-PR-103 - Separated Runtime Checksum Identities

## Scope

This change separates logical Target Config Snapshot identities from the
immutable runtime-manifest address and deployed service-config byte identity.
It adds no adopter, CI, registry, service, or database-specific behavior.

## Behavior

- Adds runtime contract v4 and signed intent v4 with distinct manifest,
  desired service-config, and expected-current service-config checksums.
- Preserves `configChecksum`, `expectedCurrentConfigChecksum`, and
  `resultConfigChecksum` as logical snapshot identities.
- Adds runtime-evidence v2 physical pre/result service-config checksums.
- Carries the fields through persistence, exact replay, retry, discovery,
  lease responses, operator execution views, and the reference executor.
- Retains v3 payloads and historical rows unchanged; missing physical evidence
  is never inferred from a logical checksum.

## Database

Migration 172 adds nullable columns to `ExecutionAttempt` and
`ExecutionRuntimeEvidence`. V4/v2 rows require complete SHA-256 shapes while
legacy-v2/v3/v1 rows require SQL `NULL`. Attempt fields are immutable, and the
down migration refuses after v4/v2 use.

## Compatibility

Existing v3 execution remains operational and byte-compatible. V4 activation
requires a later authorized runtime-manifest publication integration to supply
all three immutable inputs; this Distr change deliberately does not substitute
or invent them.

## Verification

Focused Go tests cover protocol signing, persistence, retry, discovery, API,
operator read models, reference execution, protected history, and migration
upgrade/down/refusal behavior. No backend, live target, or client database is
contacted by this change.
