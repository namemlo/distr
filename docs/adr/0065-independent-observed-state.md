# ADR-0065: Independent observed state and desired-state promotion

- Status: Accepted
- Date: 2026-07-18
- Decision owners: Distr community fork maintainers

## Context

The existing `TargetComponentState` and `TargetComponentObservation` records
are projections produced by the executor path. They are useful historical
evidence, but an executor cannot independently prove that the runtime now
matches what it attempted.

Operator control-plane v2 therefore needs separate records for:

- admitted, pending desired state;
- independently verified active desired state;
- executor reports;
- registered observer trust and scope;
- ordered observations;
- drift cases and reconciliation decisions.

Executor success must remain provisional. Failure, cancellation, timeout,
callback loss, partial evidence, and runtime mismatch must not overwrite the
last verified active desired revision.

## Decision

### Pending and active desired revisions

Execution admission appends one `PendingDesiredRevision` per component. It
does not change `ActiveDesiredRevision`.

Only a trusted, accepted, in-scope, complete, healthy observation captured
after pending admission and before its deadline, and still inside its persisted
freshness window, may create the next active revision. It must exactly match
artifact digest, configuration checksum, schema version, capability binding
checksum, platform, and topology. Promotion re-evaluates the repository state
for every current observer inside the serializable promotion transaction; a
caller-supplied result is only a concurrency hint. Components advance
independently. Every non-timeout terminal outcome retains the trusted
observation identity that proved it, while the component head continues to
reference the prior active revision.

### Observer trust boundary

`ObserverRegistration` binds an observer implementation and version to one
organization and Deployment Unit, optionally narrowed to one Component
Instance. The registration stores a SHA-256 credential fingerprint, not the
plaintext credential, plus supported measurements and capture-time freshness
and clock-skew policy.

The observer API uses:

```http
Authorization: Observer <opaque-token>
POST /api/observer/v1/observations
```

The route is available only while `operator_control_plane_v2` is enabled.
Registration identity, organization and component scope, credential
fingerprint, capture time, evidence checksum, and source sequence are checked
inside a serializable database transaction.

An exact replay is idempotent only when the full persisted envelope, component
scope, and evidence reference match. Sequence ordering is scoped per
observer/component, so equal sequence numbers on different components do not
collide. Older evidence is retained without replacing the head. A reused
sequence with different material is retained as conflict evidence and
quarantines the component. Unregistered, disabled, wrongly scoped, or
unauthenticated submissions are rejected. Freshness is persisted as
`fresh_until = captured_at + max_freshness`; it is not extended by replay or
receipt time. A later post-deadline sample does not erase otherwise eligible
pre-deadline evidence.

### Gate and campaign binding

The observation gate never treats executor success as verification. Complete
matching evidence verifies; partial, failed, cancelled, unknown, mismatching,
conflicting, or timed-out evidence cannot verify.

Timeout and conflict quarantine new mutation but release the completed
mutation lock. The quarantine is therefore explicit state, not an indefinitely
held lock.

`internal/observation.CampaignVerifier` structurally implements the PR-072
`campaigns.CampaignObservationVerifier` method:

```go
VerifyCampaignObservation(context.Context, uuid.UUID, uuid.UUID, string) error
```

The arguments are organization ID, observation ID, and exact state checksum.
The concrete repository accepts only a current, trusted, accepted, complete
observation with that exact identity and checksum. It fails closed when the
repository is not wired. Ordered integration wires this adapter into the
campaign scheduler; this synthetic-base PR does not import a future-only
campaign package.

`internal/observation.CampaignResolver` also exposes the frozen-prerequisite
resolution seam:

```go
ResolveCampaignObservation(context.Context, uuid.UUID, uuid.UUID, string) (uuid.UUID, string, error)
```

Its arguments are organization ID, canonical provider placement
(`ComponentInstance.id`), and expected checksum. The repository returns the
newest current, trusted, accepted, complete observation with that exact state
checksum; the scheduler must then fence the returned observation ID/checksum
through `CampaignVerifier` before admission.

`DeploymentPlanTargetComponent.id` is a plan-local projection and is not
interchangeable with `ComponentInstance.id`. Ordered campaign integration must
freeze the canonical component-instance identity or add an immutable bridge
from the plan-local provider placement before calling this resolver.

### Drift and reconciliation

Drift compares the active desired revision with independent measured state.
Classes cover artifact, configuration, schema, capability provider, health,
platform, topology, missing/stale evidence, and executor/observer mismatch.

A reconciliation action may restore desired state, create an approved plan,
close with evidence, or accept a deviation until an explicit future instant.
Creating work requires a concrete deployment plan and moves the case to
assigned, not resolved. Restore/close resolves only when a current trusted,
accepted, complete, healthy observation on the same Deployment Unit and
Component Instance proves the active desired material. Accepting a deviation
references the existing desired revision and observation; it does not rewrite
desired history.

### Persistence

Migration 159 creates:

- `PendingDesiredRevision`
- `ActiveDesiredRevision`
- `ComponentDesiredStateHead`
- `ExecutorReport`
- `ObserverRegistration`
- `ObservedComponentState`
- `ComponentObservationHead`
- `DriftCase`
- `DriftCaseEvent`
- `ReconciliationAction`

Organization, Deployment Unit, Component Instance, pending execution, active
revision, observation, and drift placement lineage are carried through
composite foreign keys. Evidence is append-only. Mutable heads may advance
atomically; the observation read-model flag may only transition from current
to historical while every identity, timestamp, and evidence field remains
unchanged. Organization retention uses the existing authorized transaction
setting and deletes dependents before immutable evidence and deployment plans.
Rollback refuses while evidence exists.

Because migration 159 follows campaign migration 154, it also adds the tenant
composite foreign key from
`CampaignPrerequisiteEvaluation(actual_observation_id, organization_id)` to
`ObservedComponentState(id, organization_id)`.

`TargetComponentState` and `TargetComponentObservation` remain unchanged and
are explicitly treated as legacy/executor projections.

## Consequences

- Deployment success can be reported only after independent verification.
- Partial component success preserves truthful per-component active state.
- Observer outages produce visible pending/timeout reconciliation work.
- Operators must register and rotate observer credentials separately from
  executor credentials.
- Historical v1 execution and target-state APIs remain unchanged.
- Accepted deviations require later expiry handling; they never silently
  redefine desired state.

## Alternatives rejected

- Treating executor callbacks as observed truth: no independent trust boundary.
- Updating active desired state at admission: records intent as success.
- Rewriting desired state to match drift: destroys audit history.
- Holding mutation locks until an observer recovers: risks deadlock and blocks
  safe explicit quarantine handling.
