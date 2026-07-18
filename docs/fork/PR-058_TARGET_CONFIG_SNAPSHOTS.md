# PR-058 - Immutable Target Config Snapshots

## Generic User Story

As an environment owner, I want to freeze one target's non-secret configuration and opaque secret references so
that later source or provider changes cannot alter an approved deployment input.

## Scope

- Add migration 141 and ADR-0057.
- Add deterministic `distr.target-config/v1` canonicalization and bounded validation.
- Persist immutable organization/placement-scoped parents and immutable child rows atomically.
- Attribute each snapshot to the authenticated organization user; clients cannot choose the creator.
- Add credential-free S3 identity validation and injected bounded object verification.
- Add create, list, get, and verify APIs below `/api/v1/target-config-snapshots`.
- Keep existing v1 deployment, release, task, callback, and agent behavior unchanged.

## Identity and Security Contract

| Concern        | Contract                                                                                                          |
| -------------- | ----------------------------------------------------------------------------------------------------------------- |
| Placement      | One organization, deployment unit, target-environment assignment, and environment                                 |
| Source         | Credential-free repository identity, full immutable commit, adapter, and adapter version                          |
| Objects        | S3 version ID or ADR-0054 content-addressed path; reference, media type, size, checksum only                      |
| Components     | Physical name maps to one component instance in the same organization and unit                                    |
| Component race | Creation locks instances and requires the current physical name; later renames preserve copied evidence           |
| Secrets        | Provider, opaque reference, and non-reversible version fingerprint only                                           |
| Canonical form | Explicit `distr.target-config/v1`; stable-key sorting; nil/null/empty sets normalize to `[]`; duplicates rejected |
| Verification   | Dedicated target-config S3 client; injected, streamed, 16 MiB maximum, bounded facts, no bytes or provider dumps  |
| Mutability     | No update/delete repository or route; database triggers reject mutation                                           |
| Downgrade      | Migration 141 down refuses while any snapshot evidence exists                                                     |

## API

| Method | Route                                                 | Behavior                                               |
| ------ | ----------------------------------------------------- | ------------------------------------------------------ |
| `POST` | `/api/v1/target-config-snapshots`                     | Create one immutable snapshot                          |
| `GET`  | `/api/v1/target-config-snapshots`                     | List with optional placement filters and keyset cursor |
| `GET`  | `/api/v1/target-config-snapshots/{snapshotId}`        | Get one tenant-scoped snapshot                         |
| `POST` | `/api/v1/target-config-snapshots/{snapshotId}/verify` | Observe pinned object integrity                        |

Authenticated reads remain available with the feature flag disabled. Both POST operations require
`operator_control_plane_v2`, a read-write or admin role, and a non-super-admin organization context. Public
responses omit internal organization IDs, exact canonical payload bytes, child-row IDs, and secret values.
They expose the authenticated creator's safe user-account ID for audit attribution.

## Required Impact Report

### Database/schema impact

Migration 141 adds `TargetConfigSnapshot`, `TargetConfigSnapshotObject`, `TargetConfigSnapshotComponent`,
`TargetConfigSnapshotSecretReference`, and `TargetConfigSnapshotFeatureFlag`. Composite tenant and placement
foreign keys reject cross-organization, cross-assignment, cross-environment, and cross-unit substitutions.
The creator uses an organization-scoped membership foreign key. Snapshot creation locks referenced component rows
and validates their current physical names before checksum calculation and insert; the copied name remains immutable
after later registry renames. Canonical bytes use `BYTEA`; object bodies and secret values have no columns. Every
table has an immutable trigger, and organization retention uses one explicit transaction-local exception. Down
migration takes exclusive locks and refuses while evidence remains.

### Public API impact

Adds the four routes above. An omitted list limit defaults to 50, while explicit `limit=0` is rejected; the maximum
is 100 and pagination uses an opaque versioned cursor. Unknown fields, client-supplied creator attribution,
oversized bodies/metadata, mutable object identities, unsafe secret material, invalid placement, and duplicates fail
before persistence. Not-found and conflict responses do not expose SQLSTATE, constraint, table, provider, or
foreign-tenant details.

### Frontend/UI impact

The backend exposes only non-secret snapshot metadata, opaque secret-reference metadata, and fingerprints for the
separate setup UI slice.

### Agent/protocol impact

None. No v1 agent, task lease, callback, execution, or observed-state payload changes.

### Feature-flag impact

Uses the existing process-wide `operator_control_plane_v2` flag for create and verify. Disabling it immediately
hides those POST operations after restart while preserving historical reads and all v1 behavior.

### Security impact

Positive. Object bodies remain in immutable object storage; verification streams bounded content and never returns
it. Plaintext secrets, inline credentials, mutable paths, provider errors, organization IDs, and canonical payload
internals are excluded from the public contract. Verification uses independent
`TARGET_CONFIG_OBJECT_STORE_ENABLED`/`TARGET_CONFIG_S3_*` configuration and does not require the built-in OCI
registry. Standard AWS endpoint discovery and the AWS default credential chain, including attached IAM roles, are
used when the optional endpoint and complete static credential pair are omitted. Missing, invalid, or partial
target-config storage configuration yields only bounded
`verification_unavailable` facts.

### Backward-compatibility impact

Additive only. Existing deployments and release contracts remain readable and executable. No backfill or v1
rewrite occurs in PR-058; PR-059 owns restartable derivation and lineage.

## Validation

Focused pure, API, mapping, handler, routing, compile, and migration-pair checks run locally. Live PostgreSQL 16/18,
full-repository Go, production Angular, container, browser, and end-to-end gates remain mandatory at final
integration and are not represented as local proof by this speculative branch.
