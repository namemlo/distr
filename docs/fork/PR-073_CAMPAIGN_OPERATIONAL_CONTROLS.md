# PR-073: Campaign Operational Controls

## Scope

PR-073 adds reasoned, actor-attributed, organization-scoped pause, resume,
retry, exclude, and cancel controls for active campaign runs. Migration 155
stores append-only control requests and exclusions. A stable
`(organization_id, request_id)` key makes identical retries idempotent; reuse
with a different checksum conflicts.

The API surface is:

- `POST /api/v1/deployment-campaigns/{id}/pause`
- `POST /api/v1/deployment-campaigns/{id}/resume`
- `POST /api/v1/deployment-campaigns/{id}/retry`
- `POST /api/v1/deployment-campaigns/{id}/exclude`
- `POST /api/v1/deployment-campaigns/{id}/cancel`

The route group requires an authenticated read-write or administrator role and
blocks super-admin mutation. The final PR-071 replay must register the route
group behind `operator_control_plane_v2` and the PR-066 scoped authorization
adapter.

## Safe controls

Pause blocks admissions in the same optimistic update that appends the control
request. If work is active, the run records `pause_requested` and remains
running until the fenced scheduler observes a safe point. The scheduler then
persists `PAUSED`; a process restart does not lose the request. Resume clears
the persisted pause and admission block.

Cancel stops all new admission. It completes immediately only when active work
is cancellable. An uncertain execution produces
`PENDING_RECONCILIATION`, keeps the run visible, and requires later status
reconciliation rather than guessing success or cancellation.

Excluding a pending member prevents its admission. Excluding an already
admitted member retains an append-only exclusion with
`visible_incomplete=true` and a drift reason so the campaign cannot appear
complete.

## Retry boundary

The campaign controller preserves the protocol split:

- v1 delegates to a `SupersedingPlanCreator`, retaining ADR-0052's new-plan
  requirement after unprovable delivery;
- v2 returns `ErrCampaignV2RetryUnavailable` until PR-075 provides fenced,
  retry-safe incomplete-step attempts.

The synthetic base does not contain PR-063's final superseding-plan columns or
repository. Therefore the database adapter deliberately fails closed while the
controller seam and its v1/v2 tests are complete. Stack replay must bind the
final PR-063 creator; it must not clone or mutate a frozen plan locally.

## Concurrency and evidence

Control application locks the organization-scoped campaign run, checks the
expected version, computes the result, inserts the immutable request, and
updates the run in one transaction. Duplicate identical requests return the
stored response. Conflicting request reuse and stale concurrent controls return
a conflict. Exclusion membership uses composite campaign/run/organization
foreign keys.

## Replay seams

This change was built on the synthetic base and must be replayed after PR-071:

- combine the feature-local campaign API, mapping, types, repository, and
  handler files with PR-071's campaign revision files;
- bind PR-063's superseding-plan creator for v1 retry;
- register the operational routes through PR-071's campaign router, the
  experimental flag, and PR-066 scoped action authorization;
- reconcile migration 155 composite foreign keys with final migration 153
  campaign identity names;
- later PR-075 supplies v2 retry, and PR-076 supplies executor cancel/status
  reconciliation without changing the append-only control contract.

## Compatibility

Existing v1 deployment routes and callback semantics do not change. Campaign
controls are unavailable without the preceding campaign stack and feature
enrollment. Every unresolved dependency fails closed.
