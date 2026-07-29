# Control-Plane Backup and Restore

This runbook separates backup creation, isolated restore verification, workload
recovery, and control-plane database recovery. A backup file is not recovery
proof until it has been restored and verified.

See also:

- [Enterprise client deployment](enterprise-client-deployment.md)
- [v1/v2 rollback](control-plane-v1-v2-rollback.md)
- [Campaign incident response](control-plane-campaign-incident.md)

## Safety rules

- Use immutable backup IDs/references and lowercase SHA-256 checksums.
- Keep database credentials in the approved secret provider. Evidence contains
  references and fingerprints only.
- Fence writers when the backup mechanism requires a consistent boundary.
- Restore verification uses an isolated destination with no production writer
  access.
- Application rollback never triggers database restore automatically.
- A live restore requires a separate recovery plan, data-loss analysis,
  authorization, and independent post-restore observation.
- Preserve audit, migration, execution, and observation evidence. Do not delete
  evidence to make a downgrade or restore appear clean.

## Evidence record

| Evidence      | Minimum retained facts                                                            |
| ------------- | --------------------------------------------------------------------------------- |
| Source state  | Database identity, schema version, dirty flag, UTC time, running image digest     |
| Backup        | Backup ID/reference, mechanism/procedure version, size, checksum, operator        |
| Restore drill | Isolated destination, source backup ID/checksum, start/end time, result checksum  |
| Validation    | Schema, integrity, row counts, protected fingerprints, audit head/checkpoint      |
| Compatibility | Candidate/previous image and supported schema range                               |
| Decision      | Approvers, reason, expected data-loss and recovery-time boundary                  |
| Outcome       | Readiness, migrations, API/read checks, final checksum, observation and audit IDs |

## 1. Back up before a control-plane release

1. Record the current immutable image digest, schema version, dirty flag,
   readiness, feature flags, and audit export status.
2. Record protected row counts and deterministic fingerprints for release,
   plan, execution, observation, reconciliation, and audit history.
3. Fence or stop writers according to the database backup procedure.
4. Create the backup with the approved mechanism.
5. Compute and retain its SHA-256 and size.
6. Resume writers only after the backup boundary is confirmed.

A missing checksum, dirty migration marker, inconsistent source state, or
unexplained protected-count drift blocks the release.

## 2. Prove restore in isolation

1. Create a new isolated database destination.
2. Restore the exact checksummed backup.
3. Verify schema state, constraints, indexes, and migration marker.
4. Recompute protected counts, fingerprints, and audit-head facts.
5. Run readiness and read-only API checks with the candidate binary when the
   release procedure requires it.
6. Apply the candidate migration chain only inside the isolated acceptance
   context and verify the expected clean terminal version.
7. Retain the restore report and checksum.

The backup is accepted only when the restored facts reconcile with the source
evidence. A successful command exit without count, fingerprint, schema, and
read verification is insufficient.

## 3. Typed workload backup actions

A target plan may contain:

```text
database.backup.create
  -> database.backup.verify
  -> database.migration.apply
  -> database.migration.validate
```

Each action freezes the database resource key, adapter/version, procedure,
expected source state, timeout, checksum, and validation probes. The executor
runs only those typed inputs and returns structured evidence. A failed backup or
verification blocks downstream mutation.

Do not replace typed actions with arbitrary shell commands embedded in a plan.

## 4. Choose recovery

| Condition                                                    | Default response                                                     |
| ------------------------------------------------------------ | -------------------------------------------------------------------- |
| Failure before mutation                                      | Fix the blocker and create a new attempt/plan as required            |
| Retry-safe incomplete migration                              | Resume the same stable migration/step identity after evidence review |
| Reversible migration with proven compatibility               | Use the declared reverse action under policy and locks               |
| Forward-only/data/switch/contract migration                  | Forward-fix                                                          |
| Older application incompatible with current schema/providers | Block previous-state deployment and forward-fix                      |
| Data corruption or unrecoverable database state              | Consider a separately approved restore plan                          |

Restore is an emergency data operation, not a normal deployment undo.

## 5. Approve a live restore

The recovery plan freezes:

- backup ID and checksum;
- destination database resource;
- expected data-loss time boundary and recovery-time objective;
- restore procedure and adapter version;
- current and target schema state;
- compatible control-plane image digest;
- required approver groups and operator scope;
- validation and independent observation probes;
- timeout, locks, and terminal states; and
- forward path if verification fails.

The manual operator claims that exact typed step, verifies authorization and
checksums, and uploads structured evidence. A different backup, destination,
procedure, image, or data-loss boundary requires a new plan and approval.

## 6. Restore and verify

1. Fence Hub and all database writers.
2. Capture final pre-restore evidence.
3. Restore only the approved backup to the approved destination.
4. Start only the compatible reviewed binary.
5. Verify schema and dirty state before enabling writes.
6. Recompute protected counts, fingerprints, and audit facts.
7. Verify login, readiness, static assets, operator reads, feature flags, audit
   export, and required target-independent workflows.
8. Record independent observations and reconciliation for affected runtime
   state.
9. Remove the fence only after every required probe passes.

Any mismatch keeps the system fenced and triggers evidence-preserving
diagnosis. Do not repeatedly restore different backups without separate
decisions.

## Interrupted operations

Preserve partial logs, checkpoints, backup bytes, and checksums. Determine
whether the database, migration, and application states are known before
retrying. Unknown state is not success.

Use the stable typed action and idempotency identity when the operation declares
retry safety. Otherwise stop and create a new approved recovery decision.

## Proof boundary

Repository migration tests and neutral restore fixtures do not prove a live
backup is readable or a production recovery objective is met. Retain
environment-specific source, restore, validation, and decision evidence before
classifying a drill or restore as complete.
