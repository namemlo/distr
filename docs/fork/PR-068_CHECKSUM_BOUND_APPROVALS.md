# PR-068 - Checksum-Bound Approval Workflow

## Generic User Story

As an operator, I want every approval to be an immutable decision about one exact deployment plan, effective
policy, and subscriber set so that a changed deployment can never reuse stale authority.

## Approval Contract

- An approval request pins the deployment-plan ID, revision, canonical checksum, effective-policy checksum,
  subscriber-set checksum, requester, expiry, and optimistic request revision.
- Requirements are copied from the plan's frozen effective-policy snapshot. Owner and subscriber requirements
  retain separate policy-version, authority, principal-group, quorum, and separation evidence.
- A requester may not approve a requirement carrying `requester_cannot_approve`. Approvers must be active members
  of the exact pinned principal group at the database decision time.
- Each actor contributes at most one decision per requirement. Exact retries return the existing append-only
  decision through `(request, actor, idempotency key)`; a changed payload under the same key conflicts.
- Any rejection rejects the request. Every independent owner/subscriber requirement must reach its own distinct
  actor quorum before the request becomes eligible.
- A changed plan revision/checksum, policy checksum, or subscriber-set checksum invalidates eligibility.
  Current bindings and subscriber membership are recomposed before each new decision and admission evaluation;
  expiration and supersession are terminal audit states.
- Draft plans cannot create authority. A request is accepted only for a `READY` plan with valid frozen policy
  evidence and at least one approval rule.

The request, requirements, and decisions are organization-scoped. Decision resolution uses the same plan-then-
request lock order as request creation, rechecks current subject evidence, checks the caller's expected revision,
evaluates active group membership, appends the decision, and advances the request by exactly one revision in one
transaction. A material mismatch commits the terminal invalidation and rejects the proposed decision.

## API and Pending Work

The feature adds:

- `POST /api/v1/deployment-plans/{deploymentPlanId}/approval-requests`
- `GET /api/v1/approval-requests`
- `GET /api/v1/approval-requests/{approvalRequestId}`
- `POST /api/v1/approval-requests/{approvalRequestId}/decisions`

Pending work defaults to state `PENDING`, excludes requests past their pinned expiry, uses an opaque
`(created_at, id)` keyset cursor, defaults to 50 rows, and is bounded to 100. Requirements and decisions are batch
loaded with the same organization predicate.
Unknown JSON fields, trailing JSON values, oversized bodies, invalid idempotency keys, and stale revisions fail
closed.

Reads remain available to authenticated organization users. Mutations require `operator_control_plane_v2`.
Mutations resolve the exact plan deployment-unit/environment hierarchy through PR-066 and call its shared scoped
authorizer; legacy role compatibility remains credential-capped inside that evaluator.

## Database and Audit History

Migration 150 creates `ApprovalRequest`, `ApprovalRequirement`, and `ApprovalDecision`. Binding fields and
requirements are immutable, decisions are append-only, and the only permitted request mutations are guarded
one-revision state transitions. An active-subject unique index prevents two pending/approved requests for the same
plan. Organization retention is the only deletion path and must set the transaction-local
`distr.approval_deletion_reason=ORGANIZATION_RETENTION` marker.

The down migration takes exclusive locks and refuses to cross migration 150 while approval evidence exists.
It never silently deletes audit history.

## Integrated PR-066/PR-067 Wiring

The integrated governance stack contains PR-066 migration 148 and `internal/authorization` before PR-067 migration
149 and approval migration 150. Migration and repository verification must preserve that 148 -> 149 -> 150 order.

The integrated production adapter is backed only by PR-066:

1. Inside the supplied database authorization callback, resolve the exact organization-owned
   plan/unit/environment resource scopes with `authorization.ResolveResourceScopes`.
2. Build `types.AccessRequest` from the authenticated principal ID, credential role, super-admin state, action,
   resolved scopes, and the callback's database `DecisionAt`.
3. Call `authorization.Authorize`. Request creation uses `ActionPlanPublish`; decisions use
   `ActionApprovalDecide`. The v2 route must not add a legacy read-write role gate before this evaluator; PR-066
   credential capability is the bound.
4. Continue only for an allowed result. Keep the database principal-group membership/quorum check as an
   independent requirement; do not replace it with a broad role or legacy fallback.

`approvalActorInRequiredGroup` uses the final `PrincipalGroupMember` and latest effective revision contract while
preserving organization, membership-generation, effective-time, and active-state predicates.

## Later Admission and Campaign Wiring

- PR-070 must call approval evaluation before creating execution work. It must also reject an executor whose actor
  ID appears in a requirement carrying `executor_cannot_approve`; append-only decisions retain the necessary actor
  evidence. Existing v1 execution remains unchanged until that admission slice is integrated.
- PR-071 must call `db.EvaluateDeploymentPlanApproval(organizationID, deploymentPlanID)` followed by
  `governance.RequireApprovedCampaignMember`. A campaign must reference the approved plan; it must not copy,
  rebind, or reinterpret approval decisions.
- Any later material plan, policy, or subscriber change must create a new request. No endpoint mutates pinned
  evidence.

## Compatibility and Scope

The change is additive and generic. Existing v1 plan, task, execution, agent, and external-executor behavior is
unchanged. There is no UI or agent-protocol change in this slice. It stores no adopter name, client credential,
host, provider, application-database data, or external-system configuration.

## Verification

Fast tests cover requester self-approval denial, principal-group scope denial, independent subscriber quorum,
exact idempotent retry matching, optimistic-revision conflict, row-lock and keyset repository contracts,
expiration/supersession/material invalidation, campaign-member blocking, strict API parsing, mapping, and the
mutation kill switch.

Live PostgreSQL 16/18 migrations and repositories, the complete stacked suite, containers, and browser verification
remain final integration gates because the speculative base does not yet contain migrations 141-148.
