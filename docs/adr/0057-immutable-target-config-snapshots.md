# ADR-0057: Immutable Target Config Snapshots

## Status

Accepted

## Context

Target-neutral releases must deploy identical artifact bytes to differently configured targets without rebuilding
the release. Reading a mutable configuration repository, object-store key, live feature flag, or secret value at
execution time would make an approved plan change after publication. Storing object bodies or plaintext secrets in
PostgreSQL would also expand the Hub's credential and data-exposure boundary.

PR-056 supplies stable organization, deployment-unit, assignment, environment, and component-instance identities.
ADR-0054 already defines when an S3 object identity is immutable. Target configuration needs to bind those
identities and object rules into one canonical, tenant-safe snapshot without changing v1 execution.

## Decision

Migration 141 adds one immutable `TargetConfigSnapshot` parent and four immutable child tables for objects,
component mappings, secret references, and feature flags.

The parent freezes:

- organization, deployment unit, target-environment assignment, and environment;
- the authenticated creator's user-account ID, supplied by the Hub rather than the request body;
- credential-free configuration repository identity and full immutable commit;
- source adapter and adapter version;
- target platform and bounded non-secret runtime constraints; and
- exact canonical `distr.target-config/v1` bytes and their lowercase SHA-256 checksum.

Composite foreign keys prove that the deployment unit belongs to the assignment, the assignment belongs to the
environment, and every component instance belongs to the snapshot's deployment unit and organization. A foreign or
cross-scope identifier is returned as not found. The creator foreign key is scoped through
`Organization_UserAccount`, so a caller cannot attribute evidence to a user outside the organization.

Creation locks every referenced `ComponentInstance` row with `FOR SHARE`, then compares the submitted physical name
with the current database value before calculating the checksum or inserting any row. A concurrent rename therefore
cannot pass validation between the identity read and commit. A rename after commit remains allowed and does not
rewrite the copied physical-name evidence in an existing snapshot.

Canonical JSON sorts objects, component mappings, secret references, and feature flags by their stable semantic
keys. Duplicate keys are rejected rather than coalesced. The organization and complete placement identity are
included in the canonical bytes. PostgreSQL stores the exact bytes in `BYTEA`, verifies their checksum and schema
discriminator, and never stores object bodies. Omitted, JSON `null`, and empty optional collections are normalized
to empty arrays, so all three representations produce the same canonical bytes and checksum.

Object identity reuses ADR-0054. A supported S3 reference is immutable when it has a bounded non-empty object-store
version ID, or when its credential-free normalized path begins `/_immutable/sha256/{64-lowercase-hex}/` and the
embedded digest equals the declared checksum. Credentials, query strings, fragments, backslashes, traversal,
unsupported schemes, digest mismatch, oversized metadata, and unknown request fields are rejected.

Secret rows store only a provider, an opaque provider reference, and a non-reversible version fingerprint.
Plaintext-like fields, embedded credentials, URLs, local or absolute paths, and secret-looking non-secret metadata
are rejected. Public responses omit organization IDs, exact canonical payload bytes, internal child IDs, and any
secret value. They expose only the opaque reference metadata needed by an authorized operator.

Object verification uses an injected bounded interface and a dedicated target-config S3 client. It does not depend
on `REGISTRY_ENABLED` or reuse the OCI registry client. Operators enable it with
`TARGET_CONFIG_OBJECT_STORE_ENABLED=true` and configure `TARGET_CONFIG_S3_REGION` plus
`TARGET_CONFIG_S3_BUCKET`. Endpoint, path-style, checksum, and static-credential settings are optional. Omitting
the endpoint uses the standard AWS endpoint; omitting both static credentials uses the AWS default credential
chain, including attached IAM roles. A partial static credential pair or invalid optional value leaves verification
unavailable without preventing Hub startup; the endpoint returns a bounded `verification_unavailable` fact and
never exposes configuration or provider errors. The production adapter fetches only the pinned S3
reference/version, streams at most 16 MiB into SHA-256, and compares reference, version, supplied media type, size,
and checksum. Results contain bounded per-object facts and no body bytes or provider error dumps. A mismatch is an
observation; it never mutates the snapshot.

The API root is `/api/v1/target-config-snapshots`. It supports create, list, get, and verify only. Authenticated
reads remain available while `operator_control_plane_v2` is disabled. Create and verify require the flag plus a
read-write or admin role and reject super-admin mutation. No update or delete repository or route exists.

Every table rejects update and ordinary delete. Organization retention is the only delete exception and requires
the transaction-local `distr.target_config_snapshot_deletion_reason=ORGANIZATION_RETENTION` marker. Migration 141
down takes exclusive locks and refuses while any snapshot or child row exists.

## Consequences

Two targets can use one release digest with distinct immutable configuration evidence. Later repository, feature,
object, or secret-provider changes cannot alter an existing snapshot or plan input.

Snapshot creation performs bounded validation and one atomic parent/children transaction. Lists use an opaque
keyset cursor with default 50 and maximum 100 and batch-load all children. Omitting `limit` selects 50; an explicit
`limit=0` is invalid. Verification performs external reads and is intentionally separate from creation and
persistence.

The schema is additive, but rollback is deliberately blocked after adoption until snapshots are retired through a
separately reviewed retention procedure. PR-059 may derive snapshots from v1 history, but it must link new evidence
without rewriting historical bytes or checksums.

## Alternatives Considered

- Store configuration objects in PostgreSQL. Rejected because it duplicates immutable object storage and expands
  the sensitive-data boundary.
- Store mutable object paths and resolve them during execution. Rejected because later writes would change an
  approved plan.
- Require object-store versioning everywhere. Rejected because ADR-0054 already accepts equivalent
  content-addressed identity.
- Store plaintext secrets for convenient execution. Rejected because Distr is not the secret vault and public,
  diagnostic, audit, and backup surfaces would inherit the secret.
- Permit snapshot edits. Rejected because corrections must create new evidence and a new plan checksum.

## Validation

- Pure tests cover deterministic canonical bytes/checksums, material changes, duplicate stable keys, secret
  boundaries, mutable object references, placement mismatch, nil/empty collection equivalence, diagnostic bounds,
  unavailable/tamper facts, and bounded S3 streaming.
- API, mapping, handler, and routing tests cover strict request decoding, public redaction, feature/role behavior,
  server-owned creator attribution, explicit-zero pagination rejection, tenant-safe errors, create/list/get/verify
  operations, and absence of update/delete routes.
- PostgreSQL tests cover migration structure, atomic parent/children persistence, exact canonical bytes, duplicate
  and cross-placement rejection, creator scope, insert-time component identity locking, direct mutation guards,
  tenant-safe reads, organization retention, and downgrade refusal. Live PostgreSQL 16/18 execution remains an
  integration gate.
