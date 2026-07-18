# PR-077: Desired state, independent observation, and drift

## Purpose

PR-077 separates intended state, executor reports, and independently measured
runtime state. A component becomes active desired state only after a registered
observer verifies the exact admitted artifact, configuration, schema,
capability binding, platform, topology, and health.

## Generic user story

As an operator, I need deployment success to mean that an independent runtime
observer verified the result, so that executor callback loss, false success,
manual changes, partial deployment, and stale evidence remain visible and do
not silently replace the last known-good desired state.

## State transition

1. An admitted component execution appends a pending desired revision.
2. The previous active desired revision remains authoritative.
3. An independently authenticated observer submits ordered captured evidence.
4. The gate verifies only exact, fresh, healthy, complete evidence.
5. A verified component appends a new active revision and advances its head.
6. A failed, cancelled, partial, unknown, conflicting, or timed-out component
   terminalizes its pending revision and retains its prior active revision.
7. Mismatch or uncertainty opens visible drift/reconciliation work.

Components advance independently, so partial campaign success cannot relabel
failed components as successful.

## Observation rules

- registration, enabled state, organization, Deployment Unit, optional
  Component Instance, and credential fingerprint must match;
- freshness uses the capture timestamp plus configured clock skew;
- source sequence is monotonically increasing per observer/component;
- exact replay is idempotent;
- out-of-order evidence is retained but cannot replace the current head;
- conflicting replay is retained and quarantines;
- independent observers conflict only when their measured runtime state
  differs, not merely because their envelope/evidence checksums differ;
- executor success is provisional and never wins over runtime evidence;
- timeout releases the completed mutation lock and quarantines the placement,
  avoiding both deadlock and unsafe new exposure.

The observer token is supplied as `Authorization: Observer <token>`.
Registrations persist only the SHA-256 fingerprint.

## Data model

Migration 159 adds pending and active desired revisions, mutable desired and
observation heads, executor reports, observer registration, append-only
observed state, drift cases/events, and reconciliation actions.

All durable identities are organization-scoped. Immutable evidence tables
reject update/delete/truncate outside the existing explicit organization
retention boundary. Pending intent fields cannot change when its terminal
outcome is recorded. Downgrade refuses while evidence exists.

The existing `TargetComponentState` and `TargetComponentObservation` tables
are not altered; they remain legacy/executor projections.

## API changes

| Method | Route | Purpose |
| --- | --- | --- |
| `POST` | `/api/observer/v1/observations` | Submit authenticated independent evidence |
| `POST` | `/api/v1/observer-registrations` | Register observer scope and trust |
| `GET` | `/api/v1/observer-registrations` | List observer registrations |
| `GET` | `/api/v1/observations` | List retained observations |
| `GET` | `/api/v1/drift-cases` | List drift and unknown cases |
| `POST` | `/api/v1/drift-cases/{id}/resolve` | Record an approved reconciliation decision |
| `GET` | `/api/v1/reconciliation-actions` | List reconciliation actions |

Management routes require vendor organization context and
`operator_control_plane_v2`. Mutations require read-write/admin and block
super-admin mutation. Errors do not expose foreign organization, credential,
or database details.

## Campaign scheduler seam

`internal/observation.CampaignVerifier` implements the exact structural method
required by PR-072:

```go
VerifyCampaignObservation(ctx, organizationID, observationID, checksum) error
```

It delegates to an organization-fenced lookup requiring the exact current,
trusted, accepted, complete observation. A nil store fails closed. During
ordered integration, construct it with
`db.CampaignObservationRepository{}` and supply it to the campaign scheduler.

## Drift and reconciliation

Drift classes distinguish artifact, configuration, schema, capability,
health, platform, topology, stale/missing evidence, and
executor/observer mismatch.

Accepted deviations require a reason and future expiry. They link to the
unchanged active desired revision and observation. Restoring desired state or
accepting a new desired state must use a new reviewed immutable deployment
plan; desired history is never rewritten.

## Compatibility and security impact

- Existing v1 routes, agents, executor projections, payloads, and checksums are
  unchanged.
- The feature is disabled unless `operator_control_plane_v2` is enabled.
- Observer credentials are separate from executor/user authentication and are
  stored only as fingerprints.
- Organization and placement scope are enforced in schema and repository
  queries.
- Evidence references are bounded; API errors redact trust and tenant details.
- No provider, client, cloud, CI system, or workload-specific logic is added.

## UI and agent changes

No UI is added in this backend slice. No current agent protocol changes.
Future operator UI consumes the management routes; observer adapters use the
separate observer route.

## Verification scope

Focused tests cover trust, scope, freshness, clock skew, sequence, replay,
out-of-order and conflict retention, agreeing and disagreeing independent
observers, timeout quarantine, executor-success/runtime-wrong, manual
artifact/config/schema drift, partial/unknown/cancel/failure, independent
active advancement, prior-active preservation, accepted-deviation expiry,
campaign evidence binding, API validation/mapping, authorization/error
redaction, schema guards, rollback refusal, and route registration.

Live PostgreSQL migration execution and ordered PR-072/PR-076 integration are
final stack-integration gates because this branch intentionally uses the
synthetic PR-063 base.
