# PR-059 - v1 Target Config Extraction

## Generic User Story

As an operator with historical v1 deployment plans, I want to preview and checkpoint only configuration that can
be derived without guessing so that I can adopt immutable target configuration while retaining the exact original
release and plan evidence.

## Scope

- Add migration 142 with immutable organization-scoped dry-run checkpoints and append-only lineage.
- Derive `distr.target-config/v1` snapshots only from a single target, single component, and one unambiguous
  deployment-registry placement.
- Verify the exact reference, version, media type, size, and checksum of the v1 compose and service-config
  objects through the PR-058 target-config object verifier.
- Block every v1 variable that PR-058 cannot represent without loss. This includes ordinary resolved variables,
  unresolved variables, plaintext secrets, and mutable Distr secret references without an immutable provider
  version.
- Add a dry-run-first, restartable, idempotent Hub command.
- Keep v1 release/plan IDs, canonical bytes, checksums, reads, and execution unchanged.

## Extraction Contract

The extractor is pure and versioned. It verifies the supplied original canonical bytes against their stored
lowercase SHA-256 checksums before examining the v1 contract. It returns either one canonical PR-058 snapshot
candidate or one stable bounded reason code.

Safe derivation requires all of the following:

- exact `distr.release-contract/v1` schema;
- one deployment-plan target and one release-contract component;
- one active registry assignment/unit for the plan target and environment;
- one active component definition and instance that unambiguously matches the exact historical logical key/name
  or an active normalized alias;
- exact verified immutable object evidence for compose and service-config;
- a full immutable source commit and a credential-free source repository; and
- no variable that the PR-058 snapshot schema cannot represent.

A resolved `secret_reference` may be converted only when its provider supplies an immutable version fingerprint
and its organization/customer placement exactly matches the plan target. Current v1 Distr `Secret` rows are
mutable and have no immutable version history, so their UUIDs deliberately block with
`secret_reference_version_unavailable`; the extractor never hashes a UUID, mutable value, or update timestamp as
a substitute. Ambiguous, multi-target, multi-component, mutable, missing, or unsafe inputs remain blocked.

## Operator Workflow

Create or reuse a deterministic dry-run checkpoint:

```sh
distr backfill-target-config-snapshots \
  --organization-id <org-id> \
  --actor-user-account-id <organization-member-user-id> \
  --dry-run \
  --batch-size 100
```

The actor must be a current member of the organization. Record the returned `checkpointId`, `dryRunChecksum`,
`sourceThroughPlanId`, and `hasMore`, review every blocked reason, then apply exactly that approved state:

```sh
distr backfill-target-config-snapshots \
  --organization-id <org-id> \
  --actor-user-account-id <same-organization-member-user-id> \
  --apply \
  --checkpoint-id <checkpoint-id> \
  --dry-run-checksum <sha256-checksum> \
  --batch-size 100
```

Each checkpoint contains at most `batch-size` source plans in stable UUID order. Each apply invocation processes
at most `batch-size` approved candidates. Snapshot creation and applied-lineage insertion occur in one serializable
transaction, so a failed attempt leaves neither half committed. Re-run the identical apply command until
`pending=0`; already-applied items are no-ops.

When `hasMore=true`, start the next independently reviewable checkpoint at the returned exclusive cursor:

```sh
distr backfill-target-config-snapshots \
  --organization-id <org-id> \
  --actor-user-account-id <organization-member-user-id> \
  --dry-run \
  --after-plan-id <source-through-plan-id> \
  --batch-size 100
```

Read the persisted evidence at any time:

```sh
distr backfill-target-config-snapshots \
  --organization-id <org-id> \
  --report <checkpoint-id>
```

Apply refuses a mismatched actor, operator checksum, organization, extractor version, object evidence, registry
placement, or recomputed source/dry-run state.

## Required Impact Report

### Database/schema impact

Migration 142 adds `BackfillCheckpoint` and `ReleaseContractV1ExtractionLineage`. Both are organization-scoped and
immutable. A checkpoint records its organization-member actor and bounded cursor. Candidate/blocked dry-run rows
and applied rows are append-only; originals are foreign-keyed but never updated. Down migration takes exclusive
locks and refuses while either table contains evidence.

### Public API and UI impact

None. This is an operator CLI and repository slice. Later UI work may render the non-secret report.

### Agent/protocol impact

None. No agent payload, lease, callback, task, or execution path changes.

### Feature-flag and rollback impact

The backfill does not switch reads or execution. With `operator_control_plane_v2` disabled, existing v1
release/plan reads and execution remain authoritative. Binary rollback leaves additive checkpoints, lineage, and
snapshots dormant. Schema downgrade is deliberately refused after evidence exists.

### Security impact

Positive. Raw secret values and object bodies have no checkpoint or lineage column. Reports contain IDs,
checksums, cursors, counts, statuses, and bounded reason codes only. Candidate creation uses PR-058 validation and
the dedicated target-config object verifier; unverifiable objects and unversioned secrets block.

## Validation

Focused pure extractor, command, repository/static migration, formatting, and migration-pair checks cover
determinism, exact original checksum verification, object evidence, variable/secret blocking, logical component
resolution, bounded cursors, actor binding, dry-run approval, atomic restart, concurrency, rollback, mixed-v1/v2
history, and repeated no-op behavior. Live PostgreSQL 16/18 and full-system deployment remain final-integration
gates.
