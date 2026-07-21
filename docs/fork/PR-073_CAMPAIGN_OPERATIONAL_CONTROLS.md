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

The route group is registered behind `operator_control_plane_v2`, requires an
authenticated read-write or administrator role, and blocks super-admin
mutation. This synthetic branch denies campaign mutations unconditionally
rather than approximating scoped authorization with legacy roles. Stack
integration replaces that explicit stop with
`RequireEffectiveControlPlaneAction(ActionCampaignControl,
OrganizationResourceRef)`.

## Safe controls

Pause blocks admissions in the same optimistic update that appends the control
request. If work is active, the run records `pause_requested` and remains
running until the fenced scheduler observes a safe point. The scheduler then
persists `PAUSED`; a process restart does not lose the request. Resume clears
the persisted pause and admission block and restores the exact pre-pause state
(`SCHEDULED` or `RUNNING`).

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

- v1 creates a fresh plan from the frozen source plan through a
  `SupersedingPlanCreator`, persists the retry request and response in the same
  transaction, permits only failed or canceled members, and returns the exact
  stored plan on duplicate replay;
- v2 returns `ErrCampaignV2RetryUnavailable` until PR-075 provides fenced,
  retry-safe incomplete-step attempts.

The v1 compatibility creator uses the existing immutable v1 plan inputs. When
the PR-063 v2 lineage model is replayed, the creator must be adapted to its
explicit superseding-plan fields without changing the control request's
idempotency contract.

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
- adapt the compatibility creator to PR-063's explicit superseding lineage;
- replace the fail-closed compatibility authorization adapter with PR-066's
  effective `campaign.control` action middleware;
- reconcile migration 155 composite foreign keys with final migration 153
  campaign identity names;
- later PR-075 supplies v2 retry, and PR-076 supplies executor cancel/status
  reconciliation without changing the append-only control contract.

## Compatibility

Existing v1 deployment routes and callback semantics do not change. Migration
155 refuses downgrade while any control/exclusion row or non-default retained
runtime control state exists. Campaign controls remain unavailable without the
preceding campaign stack and feature enrollment.
