# Control-Plane v1/v2 Rollback

Rollback is not one operation. Separate the process flag, binary, schema,
target application, and database recovery decisions.

Related references:

- [Enterprise client deployment](enterprise-client-deployment.md)
- [Backup and restore](control-plane-backup-restore.md)
- [Campaign incident response](control-plane-campaign-incident.md)

## Rollback dimensions

| Dimension             | What it changes                              | What it must preserve                                     |
| --------------------- | -------------------------------------------- | --------------------------------------------------------- |
| v2 process flags      | New v2 admission and execution paths         | v1 behavior and all v2 history                            |
| Scoped enrollment     | Eligibility for one organization/environment | Global process state and other scopes                     |
| Hub binary            | Running application code                     | Schema compatibility and retained evidence                |
| Target previous state | Desired application/config/provider state    | Current and prior plans/executions                        |
| Schema reverse        | Database structures/data interpretation      | Every retained record the older schema can represent      |
| Database restore      | Database contents to a prior point           | Explicitly accepted data-loss boundary and audit evidence |

Do not combine these decisions into a single “rollback” command.

## Feature flags

The controlled path uses two default-off process flags:

- `operator_control_plane_v2` is the umbrella kill switch.
- `executor_protocol_v2` is effective only with the umbrella flag.

Organization/environment enrollment is an additional eligibility gate; it does
not replace either process flag.

To stop new v2 admission, remove `operator_control_plane_v2` and
`executor_protocol_v2` from `DISTR_EXPERIMENTAL_FEATURE_FLAGS`, avoid `all`, and
restart the Hub. Confirm both flags report disabled through the experimental
feature-flag API.

Disabling flags:

- leaves untouched v1 reads and execution behavior available;
- prevents new v2 mutation/admission through gated routes;
- does not convert an in-flight v1 or v2 execution;
- does not delete v2 releases, plans, attempts, observations, or audit rows;
- does not rewrite historical IDs, bytes, checksums, or callbacks; and
- does not by itself authorize binary or schema downgrade.

Fence or reconcile in-flight work before changing flags.

## Protocol behavior

Protocol version is frozen in each plan.

| Situation                   | v1                                           | v2                                                                         |
| --------------------------- | -------------------------------------------- | -------------------------------------------------------------------------- |
| Delivery cannot be proven   | Create a new superseding plan under ADR-0052 | A new fenced attempt is allowed only before acknowledged delivery          |
| Crash after acknowledgement | Do not redispatch blindly                    | Query status first; retry only declared-idempotent work                    |
| Duplicate event             | Existing v1 contract                         | Exact replay is idempotent; conflicting replay is a security/audit failure |
| Stale executor              | Existing lease behavior                      | Fence token rejects mutation and callbacks                                 |
| Expired callback            | Reject                                       | Reject; fresh status becomes separate reconciliation evidence              |

Unknown state remains unknown until status or independent observation proves an
outcome.

## Decision procedure

1. Pause campaign admission and capture current flags, image digest, schema,
   dirty state, in-flight attempts, locks, observations, and audit checkpoint.
2. Classify the failure: control-plane binary, v2 admission, executor protocol,
   target application, migration, database, or observation.
3. Fence uncertain execution and use status/reconciliation before retry.
4. Select only a rollback dimension whose compatibility is proven.
5. Record the decision, checksums, expected result, approvers, and forward path.
6. Execute and independently verify.

## Hub binary rollback

A previous Hub image is eligible only when its immutable digest has an explicit
compatibility record for the current schema and retained v2 data. If
compatibility is absent, keep the Hub fenced and use a forward-fix or approved
database recovery procedure.

Never run an older binary against a schema merely because startup succeeds.
Verify readiness, migration state, read/write compatibility, v1 behavior,
audit, and retained v2 history.

## Target previous-state deployment

“Deploy previous state” creates a new immutable target plan. It freezes the
retained artifact digest, config snapshot, provider bindings, baseline, change
set, migrations, observation gate, and canonical checksum.

The plan is allowed only when:

- the older application supports the current schema;
- provider capability ranges remain satisfied;
- required artifacts and config objects remain available by checksum;
- migrations are reversible or unnecessary;
- target and database locks are available; and
- policy, approval, window, and health gates pass.

The new execution appends history. It does not edit the forward plan or erase
the failed/current state. If schema/provider compatibility fails, use a
forward-fix.

## Schema downgrade and database restore

Run a down migration only when its explicit refusal checks pass and the older
binary can represent all retained records. Never delete v2 evidence,
tombstones, checkpoints, or history merely to make a down migration succeed.

Database restore is a separately approved emergency operation with explicit
data-loss analysis. Follow [Control-Plane Backup and Restore](control-plane-backup-restore.md).
It is not an automatic consequence of flag, binary, or target rollback.

## Verification

After any rollback dimension:

- verify exact running image digest, schema version, and dirty flag;
- verify effective flags and scoped enrollment;
- verify v1 reads/execution where expected;
- verify v2 history remains checksum-stable and queryable;
- verify no stale attempt or lock can mutate;
- verify current desired/pending/observed state and drift classification;
- verify audit events/export checkpoint; and
- retain the decision and evidence bundle.

No live rollback is claimed by this document.
