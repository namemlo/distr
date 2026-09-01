# PR-095: Protected-History Artifact Retention

## Purpose

Retain an exact Hub-created protected-history export as immutable object-store
evidence with distinct review identity, idempotent replay, and an atomic
control-plane audit binding.

## Generic user story

As a release or recovery operator, I need the Hub to capture the current exact
protected scope, verify the immutable stored bytes, and retain who issued and
reviewed that evidence so a later handoff does not depend on an unbound local
file.

## API and authorization

- `POST /api/v1/protected-history-artifacts` accepts only exact customer/target
  scope, `reviewerUserAccountId`, and `idempotencyKey`; unknown fields fail.
- The authenticated organization and user are always the organization and
  issuer. Issuer and reviewer must be distinct current organization members.
- `GET /api/v1/protected-history-artifacts/{id}` returns immutable metadata.
- `GET /api/v1/protected-history-artifacts/{id}/verification` reads the object
  back and verifies reference, media type, byte length, and checksum without a
  SQL write.
- Routes use the existing default-off `operator_control_plane_v2` boundary,
  `audit_export` for creation, and `audit_view` for reads.

## Storage and database

- Dedicated `PROTECTED_HISTORY_OBJECT_STORE_ENABLED` and
  `PROTECTED_HISTORY_S3_*` variables construct an independent S3 client/store.
- Objects use
  `s3://<bucket>/_immutable/sha256/<digest>/protected-history.json` and
  conditional create-only writes.
- Migration 170 adds one `ProtectedHistoryArtifact` table and one nullable
  control-plane audit correlation column. Retained rows and audit events point
  to each other with deferred organization-bound foreign keys.
- Database checks recompute request, retention, and audit-binding checksums;
  validate exact canonical scope and participant membership; and block
  update/delete/truncate.
- Down migration refuses after any retained artifact exists.

## Protected-history compatibility

Schemas 138 through 169 retain their existing registered projections. Schema
170 adds only contained prior `ProtectedHistoryArtifact` rows and their
correlated `ControlPlaneAuditEvent` rows. Unknown schema 171 still fails closed.

## Idempotency and failure behavior

The idempotency checksum binds organization, canonical scope, authenticated
issuer, reviewer, and key. Exact replay returns the original metadata after
object readback. Changed material or changed object bytes returns conflict. An
unconfigured object store fails closed and never creates database metadata.

## UI and agent protocol

No UI or agent protocol changes. This is an operator API, database, and object
storage boundary.

## Documentation and operations

Adds ADR-0082, operator API and protected-history runbook guidance, dedicated
object-store configuration, upgrade/downgrade notes, and migration-170 release
matrix targets.

## Validation

Focused Go tests cover domain checksums, S3 semantics, strict handlers,
OpenAPI, environment/service wiring, schema projections, database idempotency,
organization isolation, audit correlation, no-write verification, and migration
append-only/down-refusal behavior. Node and PowerShell release-matrix tests
cover the 138-to-170 target and missing-171 refusal. Live PostgreSQL integration
tests run only when `DISTR_TEST_DATABASE_URL` is explicitly configured; no live
system or client database is contacted by default.

## Upstream contribution notes

The design is community-neutral and uses generic organization scope, S3
semantics, authenticated review identity, and existing audit permissions. It
contains no adopter, cloud-provider, registry, CI, or client-schema assumption.

## Compatibility notes

The API is additive and default-off. Existing protected-history CLI exports and
schemas 138 through 169 are unchanged. A binary rollback may disable the route,
but schema downgrade from 170 is forbidden once evidence has been retained.
