# ADR-0054: Immutable Config Execution Inputs

## Status

Accepted

## Context

Release contracts record the compose file and service configuration used by a deployment. External execution
previously required every immutable object to have an object-store version ID and froze only the service
configuration reference. Some object stores or buckets do not provide version IDs, while a content-addressed key is
still immutable when its path and declared digest agree. An external compose executor also needs the exact compose
file; fetching its mutable environment key would break the release contract after preflight.

## Decision

Support two immutable object identities. A non-empty object version remains valid and is appended to the URI as the
`versionId` query parameter. Without a version, accept only an S3 URI whose normalized path begins
`/_immutable/sha256/{64-hex-digest}/`, whose digest matches the object's declared SHA-256 checksum, and which has no
credentials, query, or fragment.

When preparing an external execution, independently resolve the service-config checksum and compose checksum
against the frozen release contract. Persist both references and checksums on the execution before dispatch. Add
the compose identity to the public expected-state response. Do not add compose fields to successful callback
observed state because that state represents the running component projection, while the compose file is an input
to the external execution.

## Consequences

External executors can download and verify every mutable-looking deployment input without relying on bucket
versioning. Contracts fail before dispatch if either checksum has no immutable object. Existing versioned contracts
remain compatible. Existing execution rows retain empty compose fields after migration, while new executions always
freeze both inputs.

Content-addressed publishers must use create-once semantics and must not overwrite an existing digest key with
different bytes. Digest verification remains mandatory after download; the path is identity, not proof by itself.

## Alternatives Considered

- Enable bucket versioning everywhere. Rejected as the only option because operators may not control existing
  buckets and content-addressed storage provides equivalent immutable identity when enforced.
- Store only the mutable compose path from the contract. Rejected because the bytes could change between preflight
  and execution.
- Put compose identity in callback observed state. Rejected because compose is an execution input, not the runtime
  state projected for one component.
- Store the whole release contract again on each execution. Rejected because the execution needs a small frozen
  provider-neutral input contract and already references the immutable release bundle.

## Validation

- Unit tests prove matching content-addressed objects are accepted and digest mismatches are rejected.
- PostgreSQL repository tests prove both references survive execution preparation and reload.
- API mapping tests prove compose identity is returned without exposing organization internals.
- Migration up/down checks and full repository verification run before rollout.
