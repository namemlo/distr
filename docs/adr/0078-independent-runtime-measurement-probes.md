# ADR-0078: Independent Runtime Measurement Probes

## Status

Accepted

## Context

The adopter-side Choice TP observer independently inspected the running image,
configuration, platform, container state, and health, but populated
`schemaVersion`, `capabilityChecksum`, and `topologyChecksum` from the immutable
plan intent. Those copied values could satisfy the observation payload shape
without proving the corresponding runtime facts.

The control plane already compares these fields exactly when promoting desired
state. The observer therefore needs a separate, read-only source for all three
values. Schema evidence must not broaden the approved boundary into direct or
indirect workload database access.

## Decision

Adopter-side observers use a profile-pinned runtime measurement probe. The
Choice TP profile schema v2 supports two generic adapters:

- `http-json/v1` performs one bounded GET against a fixed loopback metadata
  route through the pinned local gateway; and
- `command-json/v1` invokes one exact executable below `/usr/local/libexec`
  with profile-pinned arguments under `/usr/bin/timeout`.

The plan intent cannot supply or override an executable, argument, path, host,
timeout, or adapter. HTTP does not follow redirects. Both adapters have a
one-to-ten-second remote deadline, a bounded SSH deadline, and a 4 KiB output
limit. Probe errors expose only the probe kind and failure class; stdout,
stderr, HTTP bodies, credentials, and configuration content are discarded.

The accepted probe record has exactly:

```json
{
  "schemaVersion": "1.1.0",
  "capabilityChecksum": "sha256:...",
  "topologyChecksum": "sha256:..."
}
```

The observer validates the schema value and both lowercase SHA-256 digests,
computes a canonical checksum of the accepted record, and retains only the
adapter identity and that checksum alongside the measured values in signed
evidence. It compares each measured value with the frozen intent. A mismatch
is submitted truthfully as measured `UNHEALTHY`/`FAILED` evidence; incomplete,
malformed, oversized, timed-out, or unauthenticated measurement is not
submitted.

`schemaVersion` is application release/build metadata. It may be obtained only
from an approved non-database health/metadata endpoint or the configured safe
probe. Database clients, connection strings, ORM migration history, SQL, and
client workload database access are outside this contract.

## Consequences

The observation gate now receives independently measured schema, capability,
and topology facts rather than plan echoes. Runtime drift remains visible even
when image, config, platform, and health still match.

The Choice TP services or host must expose the exact non-secret measurement
record before this adapter can be used live. The adapter does not create that
metadata, install a probe, access a database, or contact a live environment.

There is no database migration, Hub API change, agent protocol change, or core
provider dependency. Existing observer profiles v1 must be regenerated as v2
with a new canonical profile checksum and intent binding.

## Alternatives Considered

- Copying the plan values was rejected because it was not independent
  observation.
- Querying migration tables was rejected because it crosses the client
  workload database boundary and conflates application release identity with
  database state.
- Arbitrary remote shell was rejected because it is not auditable or safely
  bounded.
- Retaining the raw metadata response was rejected because the three accepted
  fields and their canonical checksum are sufficient and minimize disclosure.

## Validation

Focused Node tests cover both adapter command surfaces, timeout and output
bounds, unsafe executable rejection, exact-key validation, canonical evidence
checksums, measured-value submission, mismatch behavior, credential redaction,
and zero submission for malformed probe output. Tests use only in-memory SSH
and HTTP substitutes and do not contact a database or live system.
