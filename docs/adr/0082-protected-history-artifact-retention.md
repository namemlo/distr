# ADR-0082: Protected-History Artifact Retention

## Status

Accepted.

## Context

The protected-history CLI can export and compare exact organization-scoped
history, but the Hub did not retain an immutable server-created export for a
release or recovery handoff. An operator could keep a local file and checksum,
yet the database had no append-only metadata proving which authenticated
issuer requested the capture, which distinct organization member reviewed it,
which immutable object was read back, or which correlated control-plane audit
event recorded the retention.

Accepting artifact bytes, object references, or checksums from a caller would
move the evidence boundary outside the Hub. Reusing registry or target-config
storage settings would also couple protected evidence to unrelated object
lifecycles and credentials.

## Decision

Add an experimental authenticated route family at
`/api/v1/protected-history-artifacts`. A create request contains only exact
customer-organization and deployment-target scope, a distinct reviewer user
account ID, and an idempotency key. Unknown fields are rejected. The Hub derives
the organization and issuer from authentication, verifies both issuer and
reviewer are current members of that organization, validates every scoped ID,
and exports the protected history itself in one read-only repeatable-read
transaction.

The Hub canonicalizes the export, computes its checksum, and writes it to a
checksum-addressed S3 reference with `If-None-Match: *`. A provider conflict is
accepted only when exact readback proves the same reference, media type, byte
length, and checksum. Different bytes or metadata fail as a conflict. Storage
uses only the dedicated `PROTECTED_HISTORY_OBJECT_STORE_ENABLED` and
`PROTECTED_HISTORY_S3_*` configuration.

Migration 170 adds `ProtectedHistoryArtifact` and a
`ControlPlaneAuditEvent.protected_history_artifact_id` correlation. Retained
rows contain canonical scope, source schema, artifact/root identities, object
identity, capture time, issuer/reviewer identity, idempotency material, and
retention/audit-binding checksums. PostgreSQL recomputes those checksums,
validates sorted unique organization-bound scope arrays and membership, blocks
update/delete/truncate, and uses deferred organization-bound foreign keys in
both directions so the retained row and exact audit event/sequence commit
atomically.

Schema 170 explicitly adds prior protected-history artifact rows and their
correlated control-plane audit events to the protected-history projection when
their scopes are contained by the requested scope. Schemas 138 through 169
retain their prior projections byte-for-byte. Schema 171 and later remain
refused until explicitly registered.

Create retries with the same organization, scope, issuer, reviewer, and
idempotency key return the same retained row only after exact object readback.
Changing any request material conflicts. Metadata and verification GETs are
read-only and never rewrite retained rows or append verification audit events.

## Consequences

- Release and recovery handoffs can cite a durable, checksum-bound artifact,
  reviewer, object reference, and audit event instead of an unauthenticated
  local file alone.
- The S3 provider must support conditional create semantics and exact readback.
- A successful object write can outlive a later database failure; the
  checksum-addressed object is harmless and a retry reuses it only after exact
  verification.
- Migration 170 down succeeds only while no protected-history artifact or
  correlated event exists. Once retained evidence exists, downgrade refuses
  instead of deleting it.
- The API is additive and remains behind the existing
  `operator_control_plane_v2` feature flag and scoped audit permissions.

## Alternatives Considered

Caller-uploaded history was rejected because the Hub could not prove that the
bytes came from the current protected database scope. Mutable object names such
as `latest` were rejected because replay could silently change evidence.
Database-only artifact bytes were rejected because large canonical exports do
not belong in the transactional control-plane store. Reusing registry or
target-config object-store configuration was rejected because those are
separate trust and lifecycle boundaries. Schema 169 reuse was rejected because
it has no durable append-only artifact identity, idempotency constraint, or
atomic audit correlation.

## Validation

Unit coverage verifies canonical retention and audit checksums, create-only S3
writes, identical replay, changed-byte conflict, configured-bucket rejection,
strict request decoding, authenticated issuer derivation, and OpenAPI routes.
Database and migration integration coverage verifies organization isolation,
atomic audit binding, idempotent replay/conflict, readback without SQL mutation,
append-only triggers, compatible empty down migration, and downgrade refusal
after retention. Release-matrix tooling targets migrations 138 through 170 and
fails closed when migration 171 is requested without an exact pair. No live
system or client database is required for focused validation.
