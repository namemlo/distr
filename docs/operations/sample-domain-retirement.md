# Sample Domain Retirement

This runbook retires an explicitly identified non-production sample ownership
boundary. It is not a general retention or bulk-delete procedure.

Do not use this workflow for production records, records selected by name or
age, or any boundary whose release, execution, observation, previous-state, or
audit lineage is not understood.

## Safety contract

The only permitted scope is a reviewed, organization-scoped list of exact typed
IDs with positive sample-ownership evidence. The operation refuses:

- wildcard, prefix, regular-expression, label, and name-pattern selectors;
- age-only selectors;
- IDs from another organization;
- IDs without registered ownership evidence matching the exact marker and
  checksum;
- unlisted children discovered through a broad cascade;
- protected reverse references;
- missing, unverified, or checksum-mismatched registered backup and restore
  proof;
- an unapproved or stale preview checksum; and
- audit-event deletion.

Protected references include retained releases, plans, approvals, executions,
tasks and attempts, checksums, locks, independent observations, desired state,
reconciliation evidence, and previous-known-good state.

Keep adopter-specific inventories, credentials, hostnames, paths, and removable
IDs in the adopter-owned evidence repository. Do not add them to this community
runbook.

## Evidence directory

Create a restricted evidence directory outside the source tree. Record the
operator, UTC timestamps, source commit and image digest, database identity,
schema version and dirty flag, non-secret feature-flag state, and every command
exit status. Never copy passwords, tokens, connection strings, secret values,
or private keys into retained reports.

Retain these immutable inputs and outputs:

| Evidence             | Required binding                                                                                          |
| -------------------- | --------------------------------------------------------------------------------------------------------- |
| Database backup      | Opaque reference, size, UTC time, SHA-256, source kind, source ID, source checksum                        |
| Restore proof        | Isolated restore reference, verification time, SHA-256, source kind, source ID, source checksum           |
| Ownership inventory  | Exact typed IDs, organization ID, ownership markers/checksums, immutable source reference/checksum        |
| Registration records | Ownership evidence IDs, backup evidence ID, restore-proof evidence ID, actor, registration audit events   |
| Preview              | Job ID, item/evidence bindings, blocked references, counts, preview checksum                              |
| Approval             | ApprovalRequest ID/revision, requirement, decision actors, policy/subscriber checksums, approval checksum |
| Apply                | Job ID, checkpoint sequence/checksums, exact cumulative counts, item results                              |
| Verification         | After counts, checkpoint/tombstone/first-event lineage checks, retained-audit checks, computed status     |

An evidence reference is not enough by itself. Verify every retained file
against its recorded SHA-256 before using it to authorize the next phase.

## 1. Back up and prove restore

1. Stop or fence sample-creating writers according to the deployment's normal
   database-backup procedure.
2. Export a database backup and the pre-retirement evidence inventory.
3. Compute and retain the backup SHA-256.
4. Restore that exact backup into an isolated validation database with no
   production writer access.
5. Run schema, integrity, row-count, and protected-history checks against the
   restored database.
6. Compute and retain the restore-proof checksum and bind it to the source
   backup reference and checksum.

If the restore fails, the checksum differs from the reviewed record, or the
protected-history checks do not reconcile, stop. Do not preview or apply.

## 2. Confirm runtime and authorization prerequisites

Before registration, confirm:

- migration 162 is present in the target schema;
- the `operator-control-plane-v2` experimental feature is enabled for this
  isolated non-production operation;
- the authenticated vendor organization member has `sample.retire` and is not
  using a super-admin identity;
- an organization-scoped published owner policy supplies at least one approval
  requirement;
- that requirement uses `requester_cannot_approve` for four-eyes separation;
  and
- intended approvers are active members of the required principal group and
  have `approval.decide`.

Read access to the frozen job uses `audit.view`. Keep the requester and
approver credentials separate. Stop if the policy has no requirement, is
invalid, or cannot enforce the separation rule.

The full fresh-schema migration harness currently stops at the historical
migration-142 constraint-name collision
`releasecontractv1extractionlineage_status_check already exists`
(`SQLSTATE 42710`). Isolated PostgreSQL 16 execution of migration 162 passed,
but that focused result is not a full-chain migration certification. Do not use
this runbook to claim that the full migration gate is green.

## 3. Build and register the exact allowlist

Generate an inventory containing only the closed set of removable root types:
`application`, `deployment_target`, and `environment`. For every item, record:

- the exact subject type and UUID;
- its organization UUID;
- its sample-ownership marker and lowercase SHA-256 of that exact marker;
- its expected subject checksum; and
- its expected active-row count, which must be exactly one.

Register each inventory row before preview:

```shell
distr retire-sample-domain register-ownership-evidence \
  --subject-type "application" \
  --subject-id "<subject-uuid>" \
  --ownership-marker "<exact-marker>" \
  --ownership-checksum "sha256:<64-lowercase-hex>" \
  --source-reference "<immutable-inventory-reference>" \
  --source-checksum "sha256:<64-lowercase-hex>"
```

The API equivalent is:

```text
POST /api/v1/sample-retirement-evidence/ownership
```

Every UUID must be non-zero and unique. Each subject must resolve to an
immutable current row whose organization, ownership checksum, and expected
checksum match the inventory. Pattern-like ownership markers are not evidence.
Retain the returned ownership-evidence ID and its
`sample.retirement.ownership_evidence.registered` audit event. Registration
rows are append-only. If an inventory fact changes, stop and review the
boundary; do not attempt to edit the registered row.

Exclude every adopter-owned or operator-marked protected identity. In
particular, preserve release and change-log lineage, published plans,
approvals, executions and attempts, callbacks, locks, independent observations,
desired and reconciled state, previous-known-good artifacts, checksums, and
application audit events.

If an intended removal depends on a record outside the exact inventory, stop
and review the ownership boundary. Do not replace an ID list with a cascade,
pattern, or age condition.

## 4. Register backup and restore-proof evidence

Register the backup and restore proof as two distinct recovery-evidence rows:

```shell
distr retire-sample-domain register-recovery-evidence \
  --evidence-kind "backup" \
  --reference "<immutable-backup-reference>" \
  --checksum "sha256:<64-lowercase-hex>" \
  --source-kind "<lowercase-source-kind>" \
  --source-id "<source-uuid>" \
  --source-checksum "sha256:<64-lowercase-hex>" \
  --verified-at "<RFC3339-UTC>"

distr retire-sample-domain register-recovery-evidence \
  --evidence-kind "restore_proof" \
  --reference "<immutable-restore-proof-reference>" \
  --checksum "sha256:<64-lowercase-hex>" \
  --source-kind "<lowercase-source-kind>" \
  --source-id "<source-uuid>" \
  --source-checksum "sha256:<64-lowercase-hex>" \
  --verified-at "<RFC3339-UTC>"
```

The API equivalent for both kinds is:

```text
POST /api/v1/sample-retirement-evidence/recovery
```

`verifiedAt` must not be in the future. Retain each evidence ID and
`sample.retirement.recovery_evidence.registered` audit event. Preview resolves
the exact organization, evidence kind, reference, and checksum and freezes both
evidence IDs on the job. A reference/checksum pair that has not been registered
and verified is refused.

## 5. Preview

Submit the backup reference/checksum, restore-proof reference/checksum, and
exact allowlist to:

```text
POST /api/v1/sample-retirements/preview
```

The equivalent CLI shape is:

```shell
distr retire-sample-domain preview \
  --item "application,<uuid>,<ownership-marker>,sha256:<64-lowercase-hex>,sha256:<64-lowercase-hex>" \
  --backup-reference "<immutable-reference>" \
  --backup-checksum "sha256:<64-lowercase-hex>" \
  --restore-reference "<immutable-reference>" \
  --restore-checksum "sha256:<64-lowercase-hex>"
```

Repeat `--item` for each reviewed record. The five comma-separated values are
subject type, subject UUID, ownership marker, ownership checksum, and expected
subject checksum. Use only the three supported subject types and lowercase
SHA-256 values. Configure the server and API token through the CLI's normal
non-secret output path; never retain the token in evidence or shell history.

Retain the returned job ID, immutable item list, before counts,
reverse-reference report, and preview checksum. Retrieve the frozen preview
with:

```text
GET /api/v1/sample-retirements/{id}
```

Preview is read-only. It does not authorize apply. Treat any blocked reference,
ownership failure, cross-organization result, count mismatch, or unexpected
subject type as a stop condition. Also stop if a candidate or reference report
is missing, mutable, incomplete, or stale.

Preview checksum schema v1 binds the organization and requester, backup and
restore-proof references/checksums, canonical sorted allowlist, registered
ownership evidence and its source lineage, current candidate facts, and
complete reference reports. Generated job/item IDs and timestamps are excluded.
The stored job separately binds the resolved backup and restore-proof evidence
IDs, and each stored item binds its ownership-evidence ID.

## 6. Request and decide approval

The requester creates an ApprovalRequest for the frozen job:

```text
POST /api/v1/sample-retirements/{id}/approval-requests
```

The body contains a future `expiresAt` timestamp. The service creates a
`sample_retirement` request bound to the job version and preview checksum plus
the current effective-policy and subscriber-set checksums. Retain the returned
request ID, revision, requirements, and subject checksum.

An eligible second operator reviews the frozen preview, registered evidence,
and source inventory, then records a decision through:

```text
POST /api/v1/approval-requests/{approvalRequestId}/decisions
```

The decision body supplies the exact `approvalRequirementId`, `APPROVE` or
`REJECT`, a non-empty comment, `expectedRequestRevision`, and a unique
idempotency key. The requester must not approve their own request. The service
checks separation constraints, principal-group membership, quorum, expiry, and
revision and records the decision append-only.

Retrieve the final request through
`GET /api/v1/approval-requests/{approvalRequestId}` and require state
`APPROVED`. Retain the associated `approval.decided` audit event and its
canonical approval checksum. That checksum binds the request ID, subject
ID/revision/checksum, effective-policy and subscriber-set checksums, request
revision, and state.

A changed item, count, ownership fact, reference report, registered evidence,
policy, subscriber set, or expiry invalidates the binding. Request a current
approval for the unchanged frozen preview; never reuse a stale decision.

## 7. Apply

Apply the approved job through the matching CLI command or:

```text
POST /api/v1/sample-retirements/{id}/apply
```

The equivalent CLI shape is:

```shell
distr retire-sample-domain apply "<job-uuid>" \
  --preview-checksum "sha256:<64-lowercase-hex>" \
  --approval-id "<approval-request-uuid>" \
  --approval-checksum "sha256:<64-lowercase-hex>"
```

Supply the exact preview checksum, approval ID, and approval checksum. Apply
reloads the ApprovalRequest and requires that it is approved, current,
unexpired, and bound to the exact frozen job, preview, policy, and subscriber
set. It then atomically binds those approval facts to the stored job while
transitioning from `PREVIEWED` to `APPLYING`. If any binding is missing, stale,
mismatched, invalidated, or unresolvable, stop. Monitor item and checkpoint
state. Do not run a second cleanup job against the same boundary while apply is
in progress.

For each pending item, apply atomically revalidates the job and item lock,
organization and ownership, and zero protected reverse references. It writes
the tombstone before deleting the active domain record, then stores the exact
count and checkpoint. The job reaches its final state only after every frozen
count and checkpoint reconciles.

Apply deletes only the frozen active sample items. It must not delete
application audit events. For each removed audit subject, retain the append-only
tombstone that resolves the original organization, subject type, subject ID,
subject checksum, first audit-event ID, audit-event count, job, and deletion
lineage. The job resolves the approval binding.

## 8. Restart an interrupted apply

If the process, connection, or worker stops, retain all partial evidence and
inspect the stored job and checkpoints. Resolve infrastructure failures, then
invoke apply again with the same:

- job ID;
- preview checksum;
- approval ID; and
- approval checksum.

Completed items are read-only no-ops and processing resumes at a durable
checkpoint. Use only the same API or CLI apply operation; direct database
mutation is unsupported. Do not edit the job or generate a broader allowlist.
An inconsistent checkpoint, checksum, or count is a stop-and-escalate
condition.

## 9. Verify

Request verification only after apply reports a terminal result:

```text
POST /api/v1/sample-retirements/{id}/verify
```

The equivalent CLI shape is:

```shell
distr retire-sample-domain verify "<job-uuid>"
```

Verification is read-only and must reconcile:

- frozen before counts, applied counts, and active after counts;
- every allowlisted item and no unlisted item;
- exact checkpoint sequence, monotonically increasing completed ordinals,
  and cumulative applied/skipped/tombstone counts; retain each checkpoint
  checksum for independent evidence review;
- tombstone subject/checksum/job lineage and canonical lineage checksum,
  including the job's approval binding;
- the current first audit-event ID against each frozen
  `firstAuditEventId`, with a retained audit-event count no lower than the
  tombstone's frozen count;
- unchanged protected release, plan, approval, execution, task, attempt,
  callback, lock, observation, desired-state, reconciliation, previous-state,
  and checksum history; and
- database integrity, login, readiness, and read-only operator views.

Retain the verification response and checksum it with the rest of the evidence
bundle. The endpoint computes the report without updating the job or audit
history. A returned `VERIFIED` state is the computed report state, not a
persisted transition. An API success code alone is not verification.

## Failure and recovery

Stop mutation and preserve evidence when any checksum, count, checkpoint,
ownership fact, protected reference, audit event, or tombstone does not
reconcile. Prefer diagnosis and forward repair. Restore the verified backup
only through the deployment's separately reviewed database-recovery procedure;
this runbook does not authorize an in-place production restore.

Rollback of the application binary, rollback of migration 162, restoration of
retired active records, and audit-retention decisions are separate operations.
Do not delete tombstones or audit evidence to make a downgrade pass.

## Proof boundary

Repository and neutral-fixture tests can prove request validation,
restartability, idempotency, and lineage rules. They are not evidence that a
particular backup was restorable or that a live environment was cleaned.
Record live or adopter results only after the entire procedure has been run in
that environment and its evidence has been independently reviewed.
