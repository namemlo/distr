# PR-059 - v1 Target Config Extraction

## Generic User Story

As an operator with historical v1 deployment plans, I want to preview and checkpoint only configuration that can
be derived without guessing so that I can adopt immutable target configuration while retaining the exact original
release and plan evidence.

## Scope

- Add migration 142 with immutable organization-scoped dry-run checkpoints and append-only lineage.
- Derive `distr.target-config/v1` snapshots only from a single target, single component, and one unambiguous
  deployment-registry placement.
- Match the v1 compose and service-config checksums to immutable objects exactly.
- Convert only resolved opaque secret references; block plaintext or secret-looking configuration.
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
- one component instance that unambiguously matches the component;
- exact immutable object matches for compose and service-config checksums;
- a full immutable source commit and a credential-free source repository; and
- no plaintext, inline, or secret-looking variable material.

Resolved `secret_reference` variables may be converted only when `referenceId` is an opaque identifier. The
snapshot stores the opaque identifier, a neutral provider name, and a domain-separated SHA-256 fingerprint. It
does not store the referenced secret, display name, or value. Ambiguous, multi-target, multi-component, mutable,
missing, or unsafe inputs remain blocked; the command never guesses.

## Operator Workflow

Create or reuse a deterministic dry-run checkpoint:

```sh
distr backfill-target-config-snapshots \
  --organization-id <org-id> \
  --dry-run \
  --batch-size 100
```

Record the returned `checkpointId` and `dryRunChecksum`, review blocked reason codes, then apply exactly that
approved state:

```sh
distr backfill-target-config-snapshots \
  --organization-id <org-id> \
  --apply \
  --checkpoint-id <checkpoint-id> \
  --dry-run-checksum <sha256-checksum> \
  --batch-size 100
```

Each apply invocation processes at most one stable ID-ordered batch. Re-run the exact command until `pending=0`.
Already-applied items are no-ops. A process interruption can leave an unlinked snapshot, but the retry finds its
canonical checksum and appends the missing lineage event without creating a second snapshot.

Read the persisted evidence at any time:

```sh
distr backfill-target-config-snapshots \
  --organization-id <org-id> \
  --report <checkpoint-id>
```

Apply refuses a mismatched operator checksum, organization, extractor version, or recomputed source/dry-run state.

## Required Impact Report

### Database/schema impact

Migration 142 adds `BackfillCheckpoint` and `ReleaseContractV1ExtractionLineage`. Both are organization-scoped and
immutable. Candidate/blocked dry-run rows and applied rows are append-only; originals are foreign-keyed but never
updated. Down migration takes exclusive locks and refuses while either table contains evidence.

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
checksums, counts, statuses, and bounded reason codes only. Candidate creation uses PR-058 secret and immutable
object validation.

## Validation

Focused pure extractor, command, repository/static migration, formatting, and migration-pair checks cover
determinism, exact original checksum verification, ambiguity, secret conversion/blocking, dry-run approval,
restart, and repeated no-op behavior. Live PostgreSQL 16/18, full-repository, container, browser, and mixed-v1/v2
end-to-end gates remain final-integration work.
