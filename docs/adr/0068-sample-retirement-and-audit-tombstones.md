# ADR-0068: Sample Retirement and Audit Tombstones

- Status: Accepted
- Date: 2026-07-28
- Owners: Distr control-plane maintainers

## Context

Tutorials, demonstrations, and acceptance exercises can leave non-production
ownership boundaries in a Hub database. Removing them with a name pattern, age
threshold, broad cascade, or image-pruning command can cross organization
boundaries or destroy release, execution, observation, and audit evidence.

The retention-policy domain is a separate lifecycle mechanism. Sample
retirement is a narrowly authorized operation for records whose exact
identities and sample ownership have already been established. It must remain
safe when interrupted, and it must not turn cleanup into an audit-retention
policy.

## Decision

Migration 162 adds append-only `SampleRetirementOwnershipEvidence` and
`SampleRetirementRecoveryEvidence` records plus `SampleRetirementJob`,
`SampleRetirementItem`, `SampleRetirementCheckpoint`, and
`AuditSubjectTombstone`.

A job is scoped to one organization and one immutable allowlist of typed subject
IDs. The closed set of removable roots is application, deployment target, and
environment. Every item carries an ownership marker, ownership checksum, and
expected subject checksum. The ownership checksum is the lowercase SHA-256 of
the exact marker. Each UUID must be non-zero and unique, resolve to one
immutable current row in the same organization, and match the expected
checksum. The preview rejects every other subject type plus wildcard,
name-pattern, age-only, cross-organization, mutable, missing, duplicate, and
unowned candidates. It also rejects candidates with incomplete, stale, mutable,
cross-organization, or protected reverse-reference reports.

Protected references include retained releases, plans, approvals, executions,
tasks and attempts, checksums, locks, independent observations, desired state,
reconciliation evidence, and previous-known-good state. A caller cannot make
one of those references removable merely by adding it to the same request.

Before preview, the operator must register:

- one ownership-evidence row for each exact subject, including the ownership
  marker and checksum plus an immutable inventory source reference and
  checksum;
- one verified `backup` recovery-evidence row, including its source kind, source
  ID, and source checksum;
- one verified `restore_proof` recovery-evidence row with the same source
  lineage fields; and
- the exact organization-scoped subject allowlist.

Registration is available through
`POST /api/v1/sample-retirement-evidence/ownership`,
`POST /api/v1/sample-retirement-evidence/recovery`, and the corresponding
`retire-sample-domain register-ownership-evidence` and
`register-recovery-evidence` commands. Evidence rows are organization-scoped,
audited, and append-only: update, delete, and truncate are refused. Replaying
the exact same registration is idempotent; rebinding the same identity to
different facts is refused.

Preview resolves ownership and reverse references, freezes the proposed items
and counts, and computes an immutable preview checksum. Preview checksum schema
v1 binds the organization and requester; exact backup and restore-proof
references and checksums; canonical sorted allowlist; registered ownership
evidence IDs, markers, source references, and checksums; current candidates;
and complete reverse-reference reports. Generated job/item IDs and timestamps
are deliberately excluded. Persistence resolves the exact organization, kind,
reference, and checksum of each registered recovery-evidence row and freezes
its evidence ID on the job. Each item freezes its registered
ownership-evidence ID.

The frozen preview is approved through the internal ApprovalRequest model.
`POST /api/v1/sample-retirements/{id}/approval-requests` creates a
`sample_retirement` request bound to the job version, preview checksum,
effective policy checksum, subscriber-set checksum, requester, and expiry.
Approvers append decisions through
`POST /api/v1/approval-requests/{approvalRequestId}/decisions`. The operating
policy requires four-eyes separation with `requester_cannot_approve`, and the
decision path checks group membership, quorum, request revision, expiry, and
idempotency before reaching `APPROVED`.

Apply supplies the preview checksum, ApprovalRequest ID, and canonical approval
evidence checksum. The service reloads the request, verifies that it remains
approved, current, unexpired, policy-current, and bound to the exact frozen
preview, then atomically binds those facts while transitioning the job from
`PREVIEWED` to `APPLYING`. The approval checksum binds the request ID, subject
ID/revision/checksum, policy and subscriber-set checksums, request revision, and
state. A stale, mismatched, rejected, expired, invalidated, or unresolvable
preview or approval is refused.

Each pending item runs in one transaction that locks and revalidates the job and
item, organization and ownership, and absence of protected reverse references.
That transaction writes the tombstone before deleting the active domain record,
then records the exact count and checkpoint. Finalization is atomic and occurs
only after all frozen counts and checkpoints reconcile.

An interrupted caller resumes the same job with the same checksum and approval;
it does not broaden the allowlist or create a replacement cleanup request.
Already applied items are read-only no-ops, so repeated apply is idempotent.
Verification is read-only. It does not update the job or audit history. It
reconciles the frozen and applied counts, remaining active records, exact
checkpoint sequence and cumulative counts, tombstone lineage checksums, and
retained audit lineage. For every tombstone it compares the current first audit
event with the frozen `first_audit_event_id` and requires the retained audit
event count to be at least the frozen count. A response may report the computed
state `VERIFIED`; this is a verification result, not a persisted state
transition.

Retirement never deletes application audit events. Each removed audit subject
leaves an append-only `AuditSubjectTombstone` containing enough organization,
subject-type, subject-ID, subject checksum, first audit-event ID, audit-event
count, and job lineage to resolve retained audit events after the active subject
is gone. The job resolves the approval binding. Exported audit evidence remains
subject to its configured retention policy. Neither tombstones nor audit
exports authorize audit deletion, and sample retirement has no audit-purge
mode.

Adopter-specific removable IDs and protected IDs stay outside the community
source tree. The generic policy is fail closed: any identity not present in the
approved exact allowlist remains protected. Release, plan, execution,
observation, reconciliation, previous-state, checksum, and audit history for an
adopter boundary remains untouched unless a separately reviewed retention
policy explicitly governs that data.

## Consequences

- Cleanup has more preparation than an ad hoc delete, but its mutation scope is
  reviewable and checksum-bound.
- A successfully restored backup is a precondition rather than a recovery plan
  written after mutation.
- Operators can distinguish active domain records from retained audit subjects
  without rewriting historical audit events.
- Checkpoints permit restart without interpreting a second request as approval
  for additional records.
- Tombstones and retained audit evidence consume storage. Their retention is
  managed separately from this operation.
- Sample retirement cannot be used for ordinary lifecycle, compliance, or
  capacity-based data retention.

## Alternatives considered

- Name, prefix, wildcard, and age-based deletion were rejected because they do
  not prove ownership or bound cross-tenant effects.
- Unbounded database cascades were rejected because their effective allowlist
  is not independently reviewable.
- Deleting or rewriting audit events was rejected because it breaks actor,
  action, result, and checksum lineage.
- Treating a backup file as sufficient was rejected because an unreadable or
  incomplete backup is not recovery evidence.
- One uncheckpointed transaction for the entire domain was rejected because it
  does not provide a durable, reviewable restart boundary for larger fixtures.

## Validation

Unit, repository, handler, CLI, and neutral-fixture tests cover exact-ID
allowlists, ownership and organization isolation, protected reverse references,
registered backup/restore proof, checksum-bound ApprovalRequest decisions,
stale previews, interrupted apply, repeat no-op, checkpoint/count
reconciliation, first-event and tombstone lineage, retained application audit
events, and the absence of application-audit deletion.

Migration 162 was applied and exercised directly against isolated PostgreSQL
16 prerequisites; its append-only evidence guards, approval binding, refusal
paths, and empty up/down path passed. The repository's full fresh-schema
migration harness is not green: it stops before migration 162 at the historical
migration-142 constraint-name collision
`releasecontractv1extractionlineage_status_check already exists`
(`SQLSTATE 42710`). Migration 142 was not changed by PR-082.

These tests are community proof only. They do not demonstrate a live cleanup,
restore drill, staging result, or production result.
