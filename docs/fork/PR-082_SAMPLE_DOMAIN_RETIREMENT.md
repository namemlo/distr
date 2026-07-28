# PR-082 — Allowlisted Sample Domain Retirement

## Scope

PR-082 adds a narrowly bounded workflow for retiring tutorial, demo, and other
verified non-production sample ownership boundaries. It adds no general bulk
delete, name-pattern cleanup, age-based retention, adopter inventory, or
production cleanup automation.

The workflow is:

```text
backup and isolated restore proof
  -> append-only ownership and recovery evidence registration
  -> exact-ID preview
  -> internal checksum-bound ApprovalRequest and four-eyes decision
  -> checkpointed apply
  -> same-job restart when interrupted
  -> read-only count, checkpoint, tombstone, and audit-lineage verification
```

## Database

Migration 162 adds:

- `SampleRetirementOwnershipEvidence` and
  `SampleRetirementRecoveryEvidence` for append-only, organization-scoped,
  source-bound prerequisites;
- `SampleRetirementJob` for organization scope, backup/restore references and
  registered evidence IDs, checksums, the immutable preview checksum,
  ApprovalRequest binding, state, and exact counts;
- `SampleRetirementItem` for the typed, ownership-proven allowlist and item
  evidence binding and outcome;
- `SampleRetirementCheckpoint` for durable, idempotent restart boundaries; and
- `AuditSubjectTombstone` for append-only audit-subject identity and checksum
  lineage after an active sample subject is removed.

The migration does not add an application-audit deletion path. Tombstones are
not replacements for audit events; they resolve the subject referenced by
retained events.

## API and CLI contract

The organization-scoped API exposes:

```text
POST /api/v1/sample-retirement-evidence/ownership
POST /api/v1/sample-retirement-evidence/recovery
POST /api/v1/sample-retirements/preview
GET  /api/v1/sample-retirements/{id}
POST /api/v1/sample-retirements/{id}/approval-requests
POST /api/v1/approval-requests/{approvalRequestId}/decisions
POST /api/v1/sample-retirements/{id}/apply
POST /api/v1/sample-retirements/{id}/verify
```

The CLI exposes `register-ownership-evidence`,
`register-recovery-evidence`, `preview`, `apply`, and `verify`. Registration
requires exact source facts and returns append-only evidence identities.
Preview resolves those registered rows and freezes the backup/restore evidence
IDs on the job and ownership-evidence ID on each item.

The retirement-specific approval route creates an internal ApprovalRequest with
subject type `sample_retirement`. Decisions use the shared approval route and
published organization policy. Operations require
`requester_cannot_approve`; group membership, quorum, revision, expiry,
idempotency, and policy/subscriber checksums are enforced before the request is
`APPROVED`.

Apply requires the job ID plus `previewChecksum`, the ApprovalRequest UUID as
`approvalId`, and the canonical `approvalChecksum`. It reloads and validates
the current approved request and atomically binds it to the same job and
organization while transitioning from `PREVIEWED` to `APPLYING`. A repeated
apply of the same approved job is a read-only no-op; an interrupted job resumes
from its recorded checkpoint.

## Safety boundary

Preview accepts only an explicit organization-scoped list of the closed
removable root types (`application`, `deployment_target`, and `environment`)
with an ownership marker, ownership checksum, and expected subject checksum. It
refuses:

- wildcard, prefix, name-pattern, label, and age-only selection;
- zero or duplicate UUIDs and pattern-like ownership markers;
- cross-organization or mutable candidates;
- missing ownership markers or a checksum that is not the SHA-256 of the exact
  marker;
- absent or mismatched append-only ownership-evidence registration;
- stale subject checksums or a before count other than one;
- unbounded cascades or inferred children outside the reviewed list;
- incomplete, stale, or mutable reverse-reference reports;
- absent, unverified, or mismatched registered backup/restore proof;
- protected reverse references; and
- stale preview or approval checksums.

Preview checksum schema v1 deterministically binds organization/requester,
backup/restore references and checksums, and the canonical sorted allowlist,
registered ownership evidence and source lineage, candidate facts, and
reference reports. Generated job/item IDs and timestamps are not checksum
inputs. Database foreign keys separately freeze the resolved recovery and
ownership evidence IDs.

Protected references include retained releases, plans, approvals, executions,
tasks and attempts, checksums, locks, independent observations, desired state,
reconciliation evidence, and previous-known-good state. Adding a protected
record to the proposed allowlist does not turn it into a sample record.

Adopter-specific removable and protected IDs remain in adopter-owned evidence,
not community source. Any ID outside the approved exact allowlist remains
protected. This preserves adopter release, plan, execution, observation,
reconciliation, previous-state, checksum, and audit history while allowing a
separately proven sample boundary to disappear from active views.

## Audit retention

Application audit events are never deleted by sample retirement. Removed
subjects produce append-only tombstones with organization, type, original ID,
subject checksum, first audit-event ID, audit-event count, and canonical job
lineage; the job resolves the approval binding. The tombstone is written in the
same item transaction before the active domain record is deleted.

Read-only verification reconciles checkpoint sequence and cumulative counts,
recomputes each tombstone lineage checksum, compares the current first audit
event with the frozen first-event ID, and refuses a retained audit count below
the frozen count. It does not update the job or audit history; `VERIFIED` in the
response is a computed result.

Sample retirement does not shorten retention, authorize audit purge, rewrite
an event to point at another subject, or claim indefinite retention in the
primary database. Any future audit-retention change requires its own policy,
authorization, and evidence.

## Neutral fixture proof

The neutral fixture models one removable tutorial/demo boundary and separate
protected history. Tests must prove:

- only exact, owned fixture IDs are candidates;
- wildcard/name/age/cross-organization input fails closed;
- a protected reverse reference blocks preview or apply;
- missing or mismatched restore proof and stale preview checksums fail closed;
- interruption and same-job retry preserve exact counts;
- repeated apply is a no-op;
- removed subjects have resolvable tombstones; and
- protected release, plan, execution, observation, previous-state, checksum,
  and application-audit records remain unchanged;
- checkpoint sequence and cumulative counts reconcile exactly; and
- verification detects first-event, audit-count, or tombstone-lineage drift
  without mutating history.

Fixture output is deterministic repository evidence. It does not represent a
live backup, restore, Hub, staging, production, or adopter cleanup.

## Operations

The operator procedure is
[`docs/operations/sample-domain-retirement.md`](../operations/sample-domain-retirement.md).
It requires backup and isolated restore proof before preview and preserves all
inputs, approvals, checkpoints, counts, tombstones, and verification results as
checksum-bound evidence.

## Compatibility and rollback

Existing deployment, release, execution, observation, reconciliation, and
audit reads are unchanged unless a caller explicitly creates and applies a
sample-retirement job. Existing retention-policy behavior is unchanged.

Binary rollback, migration downgrade, record restoration, and audit retention
are separate decisions. A downgrade must not delete tombstones or retained
audit evidence merely to cross migration 162.

## Verification status

Focused unit, repository, handler, CLI, migration, and neutral-fixture checks
passed. Migration 162 was also applied directly against isolated PostgreSQL 16
prerequisites, where append-only evidence, checksum-bound approval, refusal
paths, and empty up/down behavior passed.

The repository's full fresh-schema migration harness is explicitly not green.
It stops before migration 162 at the historical migration-142
constraint-name collision
`releasecontractv1extractionlineage_status_check already exists`
(`SQLSTATE 42710`). PR-082 did not change migration 142, and the isolated
migration-162 pass does not clear that full-harness blocker.

No live cleanup, live restore drill, staging result, production result, or
adopter mutation is claimed by this document.
