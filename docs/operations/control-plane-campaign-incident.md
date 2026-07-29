# Control-Plane Campaign Incident Response

Use this runbook when a campaign breaches a threshold, an execution becomes
unknown, independent observation disagrees with the executor, a provider
prerequisite fails, or a migration/security condition makes further exposure
unsafe.

Related references:

- [Enterprise client deployment](enterprise-client-deployment.md)
- [Backup and restore](control-plane-backup-restore.md)
- [v1/v2 rollback](control-plane-v1-v2-rollback.md)
- [Operator control-plane API](../api/operator-control-plane-api.md)

## Incident principles

- Pause new exposure before diagnosing.
- Preserve immutable plans, campaign revisions, attempts, events,
  observations, locks, and audit evidence.
- Do not call unknown execution successful or failed without proof.
- Do not redispatch work after callback loss until status and retry safety are
  known.
- Do not change campaign membership, order, thresholds, or member plans in
  place.
- Independent runtime evidence, not executor assertion, closes the health gate.
- Database restore and unsafe previous-state deployment require separate
  decisions.

## Trigger conditions

| Trigger                               | Required first response                                      |
| ------------------------------------- | ------------------------------------------------------------ |
| Failure/unknown threshold reached     | Pause admissions and preserve threshold evaluation           |
| Critical alert or health gate failure | Pause the current/next wave                                  |
| Callback loss or adapter timeout      | Mark unknown, query status, retain expired callback evidence |
| Executor/observer mismatch            | Quarantine placement and open reconciliation                 |
| Provider prerequisite mismatch        | Stop downstream admission; do not rebind the frozen plan     |
| Migration/postcondition failure       | Hold dependent nodes and inspect backup/recovery evidence    |
| Stale fence or conflicting event      | Reject mutation/event and escalate as a security incident    |
| Unexpected config/artifact drift      | Quarantine and reconcile; do not rewrite desired state       |

## 1. Stabilize

1. Record incident UTC time, organization/campaign/run/wave IDs, campaign and
   member plan checksums, actor, and reason.
2. Issue **pause**. Confirm no new member is admitted.
3. Record in-flight executions and each step's cancellation/safe-point
   contract. By default, in-flight work continues to the next safe terminal or
   intervention point.
4. Preserve locks and fences until executor state is terminal or safely fenced.
5. Capture current desired, pending, executor-reported, independently observed,
   drift, and reconciliation state.
6. Capture audit export lag/checkpoint and all evidence checksums.

Pause is not cancellation. It does not claim that already mutating work stopped.

## 2. Determine execution truth

For every affected execution:

1. Validate attempt, plan, step, adapter, and fence identity.
2. Compare the last accepted ordered event with the callback deadline.
3. Query adapter status using fresh authentication when completion is missing.
4. Reject expired callbacks; store a proven fresh status response as a new
   reconciliation event.
5. Compare executor result with trusted observations.
6. Keep the outcome **UNKNOWN** if neither status nor observation proves it.

Do not release a quarantined placement to new mutation while unknown state
could overlap another attempt.

## 3. Select a control

| Control           | When to use                                                    | Effect                                                                           |
| ----------------- | -------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| Pause             | New exposure is unsafe                                         | Stops new admission; retains in-flight semantics                                 |
| Cancel            | A step declares cancellation support and cancellation is safer | Sends a typed request; uncertain result still reconciles                         |
| Retry             | Incomplete work is proven retry-safe                           | v2 creates a fenced attempt; v1 uses a superseding plan after uncertain delivery |
| Resume            | Root cause is resolved and all gates pass                      | Continues persisted state without repeating completed steps                      |
| Exclude           | One member cannot safely continue                              | Requires authorized reason; member remains visible as excluded/drifted           |
| Threshold stop    | Policy threshold is breached                                   | Atomically blocks new admissions; does not relabel in-flight tasks               |
| New plan/revision | Any material frozen fact changed                               | Revalidates and requires new approval                                            |

A missing provider, changed config, stale baseline, changed policy, or altered
membership cannot be fixed with resume. Publish a new plan/campaign revision.

## 4. Handle common cases

### Callback loss

Query status first. Under v2, retry only a declared-idempotent incomplete step
with a new fence. Under v1, follow the new-plan behavior. Never accept the late
expired callback as completion.

### Observation mismatch

Do not promote pending desired state. Retain the prior active revision, open a
drift case, and choose a corrective plan, restore-to-desired action, or
time-bounded approved exception.

### Provider failure

Stop downstream consumers. Verify the exact upstream expected-state checksum
against a fresh trusted observation. A mismatch pauses admission and requires
reconciliation or a new revision; it never silently substitutes another
provider.

### Migration failure

Stop dependent nodes. Verify database lock, backup, applied migration identity,
pre/post probes, and retry/reversibility classification. Prefer forward-fix for
data, switch, contract, destructive, or otherwise unsafe reverse paths. Use the
backup/restore runbook only through a separately approved recovery plan.

### Security/fencing failure

Reject stale credentials, attempts, fences, callbacks, and conflicting
duplicates. Preserve evidence, revoke or rotate the affected credential
reference through the approved secret provider, and require a new authorized
attempt where safe.

## 5. Resume safely

Before resume:

- root cause and affected scope are documented;
- no stale attempt can mutate or report;
- required locks and adapter capabilities are available;
- plan, provider, config, baseline, policy, window, and approval are still
  valid;
- trusted observations meet freshness requirements;
- thresholds and bake requirements pass; and
- the operator records an authorized reason.

Resume continues persisted per-plan/per-step state. Completed step keys are not
repeated.

## 6. Close the incident

Close only after:

- every affected execution has a proven terminal or explicitly retained unknown
  outcome;
- desired, pending, and observed state are consistent or linked to open drift;
- excluded members and remaining blast radius are visible;
- locks/fences are released or intentionally quarantined;
- audit export is healthy or lag is assigned and monitored;
- corrective/forward-fix plans have owners; and
- the evidence bundle correlates release, config, plan, approval, campaign,
  execution, adapter, observation, reconciliation, actor, and result.

Record live or production incident evidence only after this procedure is
actually executed. This document contains no live incident proof.
