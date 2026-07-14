# Control Plane Governance and Execution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement PR-066 through PR-078: scoped authorization, versioned policies/approvals/calendars, deterministic campaigns, adapter resolution, fenced executor protocol v2, independent observations/reconciliation, and correlated audit export.

**Architecture:** Treat governance artifacts as immutable, checksum-bound authority for plans/campaigns. Keep protocol v1 untouched and introduce a separate authenticated v2 execution/observer boundary with attempts, fencing, idempotent ordered events, status/cancel/reconciliation, pending/active desired state, and independently trusted observations.

**Tech Stack:** Go, PostgreSQL, chi/oaswrap, existing task/lease/step-event packages, Ed25519 or configured Sigstore-compatible intent signing, Angular-facing REST read/write models, background workers, mise.

## Global Constraints

- PR-055 through PR-065 and their exit gate must be accepted first.
- Migrations 147–159 are reserved from the program ledger; re-check immediately before each PR.
- New action authorization is deny-by-default and checked at handler, service, and repository scope boundaries.
- A process feature flag never grants tenant authority. Effective v2 access requires organization and environment enrollment from PR-066.
- Draft edits create no approval or execution authority. Only published immutable revision checksums can be approved or scheduled.
- Closed windows, freezes, and concurrency waits do not mutate immutable inputs; material input/policy/rule/override changes create a new revision and approval.
- All exact instants use UTC-aware storage and all admission decisions retain timezone rule evidence.
- Protocol v1 files/tables/functions retain ADR-0052 behavior and test coverage. V2 code must not branch inside v1 callback state transitions.
- Executor reports are evidence, not independent observation. Active desired state advances only after a trusted observer gate.
- All event/log/error/evidence inputs are redacted and size bounded before persistence.
- Each PR follows red → green focused tests, live DB tests, full regression, docs/ADR/index updates, and one focused commit.

---

## Task 1: PR-066 — Scoped Authorization and Feature Enrollment

**Files:**

- Create: `internal/migrations/sql/147_scoped_authorization_enrollment.up.sql`
- Create: `internal/migrations/sql/147_scoped_authorization_enrollment.down.sql`
- Create: `internal/types/authorization.go`
- Modify: `internal/types/permissions.go`
- Modify: `internal/types/permissions_test.go`
- Create: `internal/authorization/authorize.go`
- Create: `internal/authorization/authorize_test.go`
- Create: `internal/authorization/scope.go`
- Create: `internal/authorization/scope_test.go`
- Create: `internal/db/authorization.go`
- Create: `internal/db/authorization_test.go`
- Create: `api/authorization.go`
- Create: `internal/handlers/authorization.go`
- Create: `internal/handlers/authorization_test.go`
- Modify: `internal/middleware/middleware.go`
- Modify: `internal/routing/routing.go`
- Create: `docs/adr/0060-scoped-authorization-and-enrollment.md`
- Create: `docs/fork/PR-066_SCOPED_AUTHORIZATION_ENROLLMENT.md`

Migration 147 creates `RoleDefinition`, `RolePermission`, `RoleBinding`, `PrincipalGroup`, `PrincipalGroupMember`, and `ControlPlaneEnrollment`. Supported scope kinds are `organization`, `customer`, `environment`, `deployment_unit`, `component`, and `campaign`. Enrollment can be organization- or environment-scoped and has effective interval, actor, reason, and revision.

```go
type Action string
type ScopeRef struct { Kind PermissionScope; ID uuid.UUID }
type AccessRequest struct { OrganizationID, PrincipalID uuid.UUID; Action Action; ResourceScopes []ScopeRef }
type AccessDecision struct { Allowed bool; MatchedBindings []uuid.UUID; ReasonCode string }

func Authorize(context.Context, AccessRequest) (AccessDecision, error)
func ResolveResourceScopes(context.Context, ResourceRef) ([]ScopeRef, error)
func IsControlPlaneV2Effective(context.Context, organizationID, environmentID uuid.UUID) (bool, error)
```

Action constants cover release create/publish/block, registry/config manage, plan create/publish/execute, approval decide, policy/calendar/freeze manage, emergency override, campaign control, observer manage, reconciliation decide, audit view/export, and sample retirement. Existing built-in roles backfill checkpointedly; legacy `Organization_UserAccount.user_role` remains dual-read fallback until a separately approved removal.

- [ ] Add pure authorization tests for exact scope match, ancestor organization grant, wrong customer/environment/unit denial, group membership, expired binding, mutation versus view, and no information leakage.
- [ ] Add enrollment tests for process flag off, organization off, environment off, effective interval, and both gates on.
- [ ] Implement schema/repository and checkpointed built-in role backfill; test repeated backfill and flag rollback.
- [ ] Add admin APIs under `/api/v1/authorization/roles`, `/bindings`, `/groups`, and `/control-plane-enrollments`; add action-specific middleware helpers.
- [ ] Replace generic role checks only on new v2 routes; leave v1 authorization behavior unchanged.
- [ ] Verify and commit.

```powershell
go test ./internal/authorization ./internal/types ./internal/db ./api ./internal/handlers ./internal/middleware -run 'Authorization|RoleBinding|Enrollment|Scope' -count=1
mise run lint:migrations
git add internal/migrations/sql/147_* internal/types/authorization.go internal/types/permissions* internal/authorization internal/db/authorization* api/authorization.go internal/handlers/authorization* internal/middleware/middleware.go internal/routing/routing.go docs
git commit -m "feat: authorize scoped control plane actions"
```

## Task 2: PR-067 — Versioned Deployment Policies

**Files:**

- Create: `internal/migrations/sql/148_deployment_policies.up.sql`
- Create: `internal/migrations/sql/148_deployment_policies.down.sql`
- Create: `internal/types/deployment_policy.go`
- Create: `internal/governance/policy.go`
- Create: `internal/governance/policy_test.go`
- Create: `internal/db/deployment_policies.go`
- Create: `internal/db/deployment_policies_test.go`
- Create: `api/deployment_policy.go`
- Create: `api/deployment_policy_test.go`
- Create: `internal/mapping/deployment_policy.go`
- Create: `internal/handlers/deployment_policies.go`
- Create: `internal/handlers/deployment_policies_test.go`
- Modify: `internal/routing/routing.go`
- Create: `docs/fork/PR-067_VERSIONED_DEPLOYMENT_POLICIES.md`

Migration 148 creates `DeploymentPolicy`, immutable `DeploymentPolicyVersion`, and `DeploymentPolicyBinding`. Policy JSON has an exact schema for quorum, separation, risk gates, allowed resolution modes, minimum bake, wave thresholds, window/freeze references, override rules, required evidence, and bootstrap behavior.

```go
type EffectivePolicy struct { VersionIDs []uuid.UUID; Checksum string; ApprovalRules []ApprovalRule; AdmissionRules AdmissionRules; CampaignRules CampaignRules }
func ValidateDeploymentPolicyVersion(types.DeploymentPolicyVersion) []types.ValidationIssue
func ComposeEffectivePolicy(owner types.PolicySet, subscribers []types.PolicySet) (EffectivePolicy, []types.ValidationIssue)
func BindDeploymentPolicy(context.Context, types.PolicyBindingRequest) error
```

Composition uses strict conjunction: every owner/subscriber quorum must be independently satisfied; allowed modes intersect; mandatory gates union; maximum required wait/bake and minimum failure tolerance win; no common window blocks. Subscriber-set changes alter the effective checksum.

- [ ] Test deterministic policy checksum, immutable publish, invalid expression, owner/subscriber strict composition, conflicting modes/windows, bootstrap, and subscriber-set invalidation.
- [ ] Implement exact-schema validation/composition before DB/API.
- [ ] Expose CRUD/version/publish/bind/list under `/api/v1/deployment-policies`; drafts are mutable, versions are immutable.
- [ ] Integrate effective policy snapshot/checksum into plan publication without enabling approval/execution yet.
- [ ] Verify and commit.

```powershell
go test ./internal/governance ./internal/db ./api ./internal/mapping ./internal/handlers -run 'DeploymentPolicy|EffectivePolicy|PolicyBinding' -count=1
mise run lint:migrations
git add internal/migrations/sql/148_* internal/types/deployment_policy.go internal/governance internal/db/deployment_policies* api/deployment_policy* internal/mapping/deployment_policy.go internal/handlers/deployment_policies* internal/routing/routing.go docs
git commit -m "feat: compose versioned deployment policies"
```

## Task 3: PR-068 — Checksum-Bound Approval Workflow

**Files:**

- Create: `internal/migrations/sql/149_approval_workflow.up.sql`
- Create: `internal/migrations/sql/149_approval_workflow.down.sql`
- Create: `internal/types/approval.go`
- Create: `internal/governance/approval.go`
- Create: `internal/governance/approval_test.go`
- Create: `internal/db/approvals.go`
- Create: `internal/db/approvals_test.go`
- Create: `api/approval.go`
- Create: `api/approval_test.go`
- Create: `internal/mapping/approval.go`
- Create: `internal/handlers/approvals.go`
- Create: `internal/handlers/approvals_test.go`
- Modify: `internal/handlers/deployment_plans.go`
- Modify: `internal/routing/routing.go`
- Create: `docs/fork/PR-068_CHECKSUM_BOUND_APPROVALS.md`

Migration 149 creates `ApprovalRequest`, `ApprovalRequirement`, and append-only `ApprovalDecision`. Requests pin subject type/ID/revision/checksum, effective policy checksum, requester, subscriber set checksum, expiry, and state. Decisions use optimistic request revision plus unique `(request_id, actor_id, idempotency_key)`.

```go
func RequestApproval(context.Context, types.ApprovalRequestInput) (*types.ApprovalRequest, error)
func RecordApprovalDecision(context.Context, types.ApprovalDecisionInput) (*types.ApprovalDecision, error)
func EvaluateApprovalEligibility(context.Context, uuid.UUID) (types.ApprovalEvaluation, error)
func InvalidateApproval(context.Context, uuid.UUID, types.InvalidationReason) error
```

Routes: `POST /api/v1/deployment-plans/{id}/approval-requests`, `GET /api/v1/approval-requests`, `GET /{id}`, and `POST /{id}/decisions`.

- [ ] Test requester self-approval denial, scope denial, independent subscriber quorum, duplicate decision idempotency, conflicting concurrent decision, expired/superseded request, material plan/policy/subscriber change invalidation, and unapproved campaign-member block.
- [ ] Implement transaction/row-lock decision resolution and append-only audit facts.
- [ ] Add plan approval endpoints and pending-work pagination; draft edits never create authority.
- [ ] Verify and commit.

```powershell
go test ./internal/governance ./internal/db ./api ./internal/mapping ./internal/handlers -run 'Approval|Decision|FourEyes|Invalidation' -count=1 -race
mise run lint:migrations
git add internal/migrations/sql/149_* internal/types/approval.go internal/governance internal/db/approvals* api/approval* internal/mapping/approval.go internal/handlers/approvals* internal/handlers/deployment_plans.go internal/routing/routing.go docs
git commit -m "feat: require checksum bound deployment approvals"
```

## Task 4: PR-069 — Versioned Calendars and Freezes

**Files:**

- Create: `internal/migrations/sql/150_maintenance_calendars_freezes.up.sql`
- Create: `internal/migrations/sql/150_maintenance_calendars_freezes.down.sql`
- Create: `internal/types/calendar.go`
- Create: `internal/scheduling/calendar.go`
- Create: `internal/scheduling/calendar_test.go`
- Create: `internal/db/maintenance_calendars.go`
- Create: `internal/db/maintenance_calendars_test.go`
- Create: `api/maintenance_calendar.go`
- Create: `internal/mapping/maintenance_calendar.go`
- Create: `internal/handlers/maintenance_calendars.go`
- Create: `internal/handlers/maintenance_calendars_test.go`
- Modify: `internal/routing/routing.go`
- Create: `docs/adr/0061-versioned-calendar-admission.md`
- Create: `docs/fork/PR-069_MAINTENANCE_CALENDARS_FREEZES.md`

Migration 150 creates `MaintenanceCalendar`, `MaintenanceCalendarVersion`, `MaintenanceWindowRule`, `DeploymentFreeze`, and `DeploymentFreezeRevision`. Drafts are editable; versions/revisions are immutable and checksum-bound.

```go
type CalendarEvaluationInput struct { UTCInstant time.Time; IANAZone, RuleVersion string }
type CalendarEvaluation struct { Allowed bool; UTCInstant time.Time; LocalTime time.Time; UTCOffsetSeconds int; CalendarVersionID, WindowRuleID *uuid.UUID; ReasonCode string }
func EvaluateCalendar(types.MaintenanceCalendarVersion, CalendarEvaluationInput) (CalendarEvaluation, error)
func EvaluateFreeze([]types.DeploymentFreezeRevision, CalendarEvaluationInput) types.FreezeEvaluation
```

Routes: `/api/v1/maintenance-calendars` with draft/version/publish operations and `/api/v1/deployment-freezes` with revision operations.

- [ ] Test ordinary allow/deny, overnight window, DST gap, repeated hour, timezone rule-version update, freeze overlap/priority, deterministic UTC decision, and no double execution identity.
- [ ] Implement pure calendar/freeze evaluation using IANA zone data and persist exact decision ingredients.
- [ ] Add repositories/APIs with server pagination and scoped authorization.
- [ ] Verify and commit.

```powershell
go test ./internal/scheduling ./internal/db ./api ./internal/mapping ./internal/handlers -run 'Calendar|Window|Freeze|DST' -count=1
mise run lint:migrations
git add internal/migrations/sql/150_* internal/types/calendar.go internal/scheduling internal/db/maintenance_calendars* api/maintenance_calendar.go internal/mapping/maintenance_calendar.go internal/handlers/maintenance_calendars* internal/routing/routing.go docs
git commit -m "feat: evaluate versioned deployment calendars"
```

## Task 5: PR-070 — Admission and Emergency Override

**Files:**

- Create: `internal/migrations/sql/151_deployment_admission_overrides.up.sql`
- Create: `internal/migrations/sql/151_deployment_admission_overrides.down.sql`
- Create: `internal/scheduling/admission.go`
- Create: `internal/scheduling/admission_test.go`
- Create: `internal/db/admission.go`
- Create: `internal/db/admission_test.go`
- Create: `api/admission.go`
- Create: `internal/handlers/admission.go`
- Create: `internal/handlers/admission_test.go`
- Create: `internal/scheduling/admitted_task_creation.go`
- Create: `internal/scheduling/admitted_task_creation_test.go`
- Modify: `internal/routing/routing.go`
- Create: `docs/fork/PR-070_DEPLOYMENT_ADMISSION_OVERRIDES.md`

Migration 151 creates append-only `AdmissionEvaluation` and `EmergencyOverride`. An evaluation pins plan/campaign/policy/calendar/freeze/approval revisions and records exact temporal evidence. An override pins allowed accelerations, reason, actor, approvals, expiry, and checksum; it cannot disable integrity, evidence, backup, provenance, observation, or mandatory health gates.

```go
func EvaluateAdmission(context.Context, types.AdmissionRequest) (types.AdmissionEvaluation, error)
func CreateEmergencyOverride(context.Context, types.EmergencyOverrideRequest) (*types.EmergencyOverride, error)
func AdmitDeploymentPlan(context.Context, uuid.UUID, time.Time) (types.AdmissionResult, error)
func CreateTasksForAdmittedV2Plan(context.Context, uuid.UUID, string) ([]types.Task, error)
```

Routes: `POST /api/v1/deployment-plans/{id}/admission` and `POST /api/v1/deployment-plans/{id}/emergency-overrides`.

- [ ] Test closed window waits, active freeze waits, approval missing blocks, policy/rule/override change requires revision, ordinary clock advance does not invalidate, acceleration whitelist, mandatory-gate protection, scope denial, and idempotent repeated scheduler evaluation.
- [ ] Implement admission as a pure decision plus append-only persistence.
- [ ] Add `CreateTasksForAdmittedV2Plan` as the only v2 wrapper around the existing task creator. Require `plan_schema=v2` and frozen `protocol_version=v2`, evaluate admission, then call the existing task function. Do not gate or change the shared v1 `CreateTasksForDeploymentPlan` path.
- [ ] Add a flags-off v1 regression proving current task creation/status/events remain unchanged without policy, approval, calendar, or enrollment rows.
- [ ] Verify and commit.

```powershell
go test ./internal/scheduling ./internal/db ./api ./internal/handlers -run 'Admission|EmergencyOverride|CreateTasksForAdmittedV2Plan|V1TaskCreation' -count=1
mise run lint:migrations
git add internal/migrations/sql/151_* internal/scheduling internal/db/admission* api/admission.go internal/handlers/admission* internal/routing/routing.go docs
git commit -m "feat: gate plans with deployment admission"
```

## Task 6: PR-071 — Immutable Campaign Revisions

**Files:**

- Create: `internal/migrations/sql/152_deployment_campaign_revisions.up.sql`
- Create: `internal/migrations/sql/152_deployment_campaign_revisions.down.sql`
- Create: `internal/types/campaign.go`
- Create: `internal/campaigns/canonical.go`
- Create: `internal/campaigns/canonical_test.go`
- Create: `internal/campaigns/membership.go`
- Create: `internal/campaigns/membership_test.go`
- Create: `internal/campaigns/validation.go`
- Create: `internal/campaigns/validation_test.go`
- Create: `internal/db/deployment_campaigns.go`
- Create: `internal/db/deployment_campaigns_test.go`
- Create: `api/deployment_campaign.go`
- Create: `api/deployment_campaign_test.go`
- Create: `internal/mapping/deployment_campaign.go`
- Create: `internal/handlers/deployment_campaigns.go`
- Create: `internal/handlers/deployment_campaigns_test.go`
- Modify: `internal/routing/routing.go`
- Create: `docs/adr/0062-deterministic-deployment-campaigns.md`
- Create: `docs/fork/PR-071_IMMUTABLE_CAMPAIGN_REVISIONS.md`

Migration 152 creates `DeploymentCampaignDraft`, `DeploymentCampaignRevision`, `DeploymentCampaignWave`, `DeploymentCampaignMember`, and `DeploymentCampaignPrerequisite`. Published revisions freeze ordered plan membership, tag-query result, plan/approval checksums, wave assignment, risk/bake/threshold policy, and each cross-plan prerequisite's upstream plan ID, step key, and expected observed-state checksum.

```go
func ResolveCampaignMembership(context.Context, types.CampaignDraft) ([]types.CampaignMember, error)
func CanonicalizeCampaignRevision(types.CampaignRevision) ([]byte, string, error)
func ValidateCampaignDraft(context.Context, types.CampaignDraft) []types.ValidationIssue
func PublishCampaignRevision(context.Context, uuid.UUID, string) (*types.CampaignRevision, error)
```

Routes: `POST/PATCH /api/v1/deployment-campaign-drafts`, `GET /{id}`, `POST /{id}/validate`, `POST /{id}/publish`.

- [ ] Test stable membership/order/checksum, tag change after publish, unapproved plan, mismatched plan checksum, duplicate unit, shared-provider prerequisite, observation checksum mismatch, invalid/non-increasing bake, and draft edit without authority.
- [ ] Implement pure membership/canonical/validation, then persistence/API.
- [ ] Verify deterministic recreation across process restart and commit.

```powershell
go test ./internal/campaigns ./internal/db ./api ./internal/mapping ./internal/handlers -run 'CampaignRevision|Membership|Prerequisite' -count=1
mise run lint:migrations
git add internal/migrations/sql/152_* internal/types/campaign.go internal/campaigns internal/db/deployment_campaigns* api/deployment_campaign* internal/mapping/deployment_campaign.go internal/handlers/deployment_campaigns* internal/routing/routing.go docs
git commit -m "feat: freeze deterministic deployment campaigns"
```

## Task 7: PR-072 — Campaign Scheduler and Thresholds

**Files:**

- Create: `internal/migrations/sql/153_deployment_campaign_runs.up.sql`
- Create: `internal/migrations/sql/153_deployment_campaign_runs.down.sql`
- Create: `internal/campaigns/state_machine.go`
- Create: `internal/campaigns/state_machine_test.go`
- Create: `internal/campaigns/scheduler.go`
- Create: `internal/campaigns/scheduler_test.go`
- Create: `internal/campaigns/thresholds.go`
- Create: `internal/campaigns/thresholds_test.go`
- Create: `internal/campaignworker/worker.go`
- Create: `internal/campaignworker/worker_test.go`
- Modify: `internal/db/deployment_campaigns.go`
- Modify: `internal/db/deployment_campaigns_test.go`
- Modify: `internal/handlers/deployment_campaigns.go`
- Create: `docs/fork/PR-072_CAMPAIGN_SCHEDULER_THRESHOLDS.md`

Migration 153 creates `DeploymentCampaignRun`, `DeploymentCampaignWaveRun`, `DeploymentCampaignMemberRun`, `CampaignPrerequisiteEvaluation`, and `CampaignThresholdEvaluation`. Each prerequisite evaluation retains the frozen expected checksum plus the actual matching observation ID/checksum used for admission. State transitions are optimistic and append evidence; scheduler leases use fencing and deterministic `(wave_order, member_order, plan_id)` admission.

```go
func TransitionCampaign(context.Context, types.CampaignTransition) (*types.CampaignRun, error)
func EvaluateNextWaveAdmission(context.Context, uuid.UUID, time.Time) (types.WaveAdmission, error)
func RecordCampaignPrerequisiteEvaluation(context.Context, types.CampaignPrerequisiteEvaluation) error
func RecordThresholdEvaluation(context.Context, types.ThresholdEvaluation) error
```

- [ ] Test Draft → Validated → AwaitingApproval → Scheduled → Running and pause/fail/complete/cancel paths; reject illegal transition.
- [ ] Test atomic stop on threshold breach, non-decreasing bake, restart/resume order, duplicate scheduler tick, lease loss, exact matching observation ID/checksum persistence, prerequisite mismatch pause without rebinding, and no new exposure while paused.
- [ ] Implement state machine/scheduler worker with persisted cursor and fenced lease.
- [ ] Verify and commit.

```powershell
go test ./internal/campaigns ./internal/campaignworker ./internal/db ./internal/handlers -run 'CampaignRun|Scheduler|Threshold|WaveAdmission' -count=1 -race
mise run lint:migrations
git add internal/migrations/sql/153_* internal/campaigns internal/campaignworker internal/db/deployment_campaigns* internal/handlers/deployment_campaigns.go docs
git commit -m "feat: schedule campaign waves safely"
```

## Task 8: PR-073 — Campaign Operational Controls

**Files:**

- Create: `internal/migrations/sql/154_campaign_controls.up.sql`
- Create: `internal/migrations/sql/154_campaign_controls.down.sql`
- Create: `internal/campaigns/controls.go`
- Create: `internal/campaigns/controls_test.go`
- Modify: `internal/db/deployment_campaigns.go`
- Modify: `internal/db/deployment_campaigns_test.go`
- Modify: `api/deployment_campaign.go`
- Modify: `internal/handlers/deployment_campaigns.go`
- Modify: `internal/handlers/deployment_campaigns_test.go`
- Create: `docs/fork/PR-073_CAMPAIGN_OPERATIONAL_CONTROLS.md`

Migration 154 creates `CampaignControlRequest` and `CampaignExclusion`. Control requests are idempotent, scoped, reasoned, and append-only.

```go
func PauseCampaign(context.Context, types.CampaignControlInput) error
func ResumeCampaign(context.Context, types.CampaignControlInput) error
func RetryCampaignMember(context.Context, types.CampaignMemberControlInput) (*types.DeploymentPlan, error)
func ExcludeCampaignMember(context.Context, types.CampaignMemberControlInput) error
func CancelCampaign(context.Context, types.CampaignControlInput) error
```

Routes: `POST /api/v1/deployment-campaigns/{id}/pause`, `/resume`, `/retry`, `/exclude`, and `/cancel`.

- [ ] Test pause safe point, no new admissions, resume after restart, authorized exclusion with visible incomplete/drift, cancel only cancellable steps, uncertain state reconciliation, duplicate control idempotency, and concurrent conflicting controls.
- [ ] Preserve retry split: v1 creates a superseding plan under ADR-0052; v2 retry remains blocked until PR-075 and later creates a fenced attempt only for retry-safe incomplete steps.
- [ ] Verify and commit.

```powershell
go test ./internal/campaigns ./internal/db ./api ./internal/handlers -run 'PauseCampaign|ResumeCampaign|RetryCampaign|ExcludeCampaign|CancelCampaign' -count=1 -race
mise run lint:migrations
git add internal/migrations/sql/154_* internal/campaigns/controls* internal/db/deployment_campaigns* api/deployment_campaign.go internal/handlers/deployment_campaigns* docs
git commit -m "feat: control active deployment campaigns"
```

## Task 9: PR-074 — Versioned Adapter Resolution

**Files:**

- Create: `internal/migrations/sql/155_adapter_assignments.up.sql`
- Create: `internal/migrations/sql/155_adapter_assignments.down.sql`
- Create: `internal/types/adapter.go`
- Create: `internal/adapterresolution/resolver.go`
- Create: `internal/adapterresolution/resolver_test.go`
- Create: `internal/db/adapters.go`
- Create: `internal/db/adapters_test.go`
- Create: `api/adapter.go`
- Create: `internal/mapping/adapter.go`
- Create: `internal/handlers/adapters.go`
- Create: `internal/handlers/adapters_test.go`
- Modify: `internal/planning/resolver.go`
- Modify: `internal/deploymentpreflight/evaluate.go`
- Modify: `internal/routing/routing.go`
- Create: `docs/fork/PR-074_VERSIONED_ADAPTER_RESOLUTION.md`

Migration 155 creates `AdapterImplementation`, `AdapterCapability`, `AdapterAssignment`, and immutable `DeploymentPlanStepAdapter`. Release contracts declare required capability; target assignment selects implementation/version/config snapshot at plan time.

```go
func ResolveStepAdapter(context.Context, types.AdapterResolutionRequest) (*types.ResolvedStepAdapter, []types.ValidationIssue)
func VerifyAdapterAtStart(context.Context, types.DeploymentPlanStepAdapter) error
```

Routes: `/api/v1/adapter-implementations`, `/adapter-assignments`, and plan read-only adapter details.

- [ ] Test exact capability/version match, missing/ambiguous implementation, target scope, config checksum, disabled assignment, adapter version drift after approval, and release capability remaining authoritative.
- [ ] Implement resolution and freeze adapter IDs/versions/config checksums into plan steps; start-time mismatch blocks and requires restoration or a new revision.
- [ ] Verify and commit.

```powershell
go test ./internal/adapterresolution ./internal/planning ./internal/deploymentpreflight ./internal/db ./api ./internal/handlers -run 'Adapter|ResolveStepAdapter' -count=1
mise run lint:migrations
git add internal/migrations/sql/155_* internal/types/adapter.go internal/adapterresolution internal/db/adapters* api/adapter.go internal/mapping/adapter.go internal/handlers/adapters* internal/planning/resolver.go internal/deploymentpreflight/evaluate.go internal/routing/routing.go docs
git commit -m "feat: freeze plan adapter assignments"
```

## Task 10: PR-075 — Fenced Executor Protocol v2

**Files:**

- Create: `internal/migrations/sql/156_external_execution_protocol_v2.up.sql`
- Create: `internal/migrations/sql/156_external_execution_protocol_v2.down.sql`
- Create: `internal/types/execution_v2.go`
- Create: `internal/executionprotocol/intent.go`
- Create: `internal/executionprotocol/intent_test.go`
- Create: `internal/executionprotocol/signing.go`
- Create: `internal/executionprotocol/signing_test.go`
- Create: `internal/executionprotocol/state_machine.go`
- Create: `internal/executionprotocol/state_machine_test.go`
- Create: `internal/db/execution_v2.go`
- Create: `internal/db/execution_v2_test.go`
- Create: `api/execution_v2.go`
- Create: `api/execution_v2_test.go`
- Create: `internal/handlers/execution_v2.go`
- Create: `internal/handlers/execution_v2_test.go`
- Create: `internal/executionworker/dispatcher.go`
- Create: `internal/executionworker/dispatcher_test.go`
- Modify: `internal/types/task_queue.go`
- Modify: `internal/db/task_queue.go`
- Modify: `internal/routing/routing.go`
- Create: `docs/adr/0063-fenced-executor-protocol-v2.md`
- Create: `docs/fork/PR-075_FENCED_EXECUTOR_PROTOCOL_V2.md`

Migration 156 creates `ExecutionAttempt`, `ExecutionFence`, `ExecutionIntent`, and append-only `ExecutionEvent`; `Task` gains frozen `protocol_version`. It does not alter `ExternalExecution` or `ExternalExecutionEvent`.

```go
type ExecutionIdentity struct { ExecutionID uuid.UUID; AttemptNumber int; StepKey string }
type ExecutionFence struct { ResourceKey string; Generation int64; LeaseExpiresAt time.Time }
type SignedExecutionIntent struct { Payload []byte; Checksum, KeyID, Signature string }

func BuildExecutionIntent(context.Context, types.ExecutionAttempt) (types.SignedExecutionIntent, error)
func VerifyExecutionIntent(types.SignedExecutionIntent, types.TrustPolicy) error
func ClaimExecutionAttempt(context.Context, types.ClaimRequest) (*types.ExecutionAttempt, error)
func HeartbeatExecutionAttempt(context.Context, types.HeartbeatRequest) error
func RecordExecutionEvent(context.Context, types.ExecutionEventInput) (*types.ExecutionEvent, error)
func CompleteExecutionAttempt(context.Context, types.CompletionInput) error
func FenceExecutionAttempt(context.Context, uuid.UUID, string) error
```

Executor routes use executor credentials under `POST /api/executor/v2/executions/claim` and `/api/executor/v2/attempts/{id}/heartbeat|events|complete`. The idempotency tuple is `(execution_id, attempt_number, step_key, event_sequence)`; conflicting duplicates fail securely. Lease loss increments fence generation.

ADR-0063 explicitly supersedes only v1 delivery semantics for plans frozen to v2. V1 remains claim-before-dispatch, at-most-once, late-callback rejecting, and requires a new plan after unprovable delivery.

- [ ] Add golden signed-intent tests and tamper/wrong-key/expired-intent/config/artifact mismatch cases.
- [ ] Add state tests for claim, duplicate dispatch/event, conflicting duplicate, ordered sequence, heartbeat, lease loss, stale generation, resource locks, crash before acknowledge, crash after acknowledge, terminal lease/lock release, timeout, and restart.
- [ ] Implement new tables/repository/protocol/dispatcher without modifying v1 transition functions.
- [ ] Add a regression test that executes current v1 flow with flags disabled and compares all v1 statuses/events.
- [ ] Gate new v2 admission on both flags, scoped enrollment, approved/admitted plan, and adapter preflight.
- [ ] Verify hub and both agent builds; commit.

```powershell
go test ./internal/executionprotocol ./internal/executionworker ./internal/db ./api ./internal/handlers -run 'ExecutionV2|Fence|SignedIntent' -count=1 -race
go test ./internal/db ./internal/hubexecutor -run 'ExternalExecution|ADR0052|ProtocolV1' -count=1
mise run lint:migrations
mise run build:hub:community
mise run build:agent:docker
mise run build:agent:kubernetes
git add internal/migrations/sql/156_* internal/types/execution_v2.go internal/types/task_queue.go internal/executionprotocol internal/executionworker internal/db/execution_v2* internal/db/task_queue.go api/execution_v2* internal/handlers/execution_v2* internal/routing/routing.go docs
git commit -m "feat: add fenced executor protocol v2"
```

## Task 11: PR-076 — Cancel, Status, and Callback-Loss Reconciliation

**Files:**

- Create: `internal/migrations/sql/157_execution_controls.up.sql`
- Create: `internal/migrations/sql/157_execution_controls.down.sql`
- Create: `internal/executionprotocol/controls.go`
- Create: `internal/executionprotocol/controls_test.go`
- Modify: `internal/db/execution_v2.go`
- Modify: `internal/db/execution_v2_test.go`
- Modify: `api/execution_v2.go`
- Modify: `internal/handlers/execution_v2.go`
- Create: `docs/fork/PR-076_EXECUTION_CANCEL_STATUS_RECONCILIATION.md`

Migration 157 creates `ExecutionCancelRequest`, `ExecutionStatusQuery`, and `ExecutionReconciliationEvent`.

```go
func RequestExecutionCancel(context.Context, types.CancelRequest) error
func RecordCancelAcknowledgement(context.Context, types.CancelAcknowledgement) error
func RequestExecutionStatus(context.Context, types.StatusRequest) (*types.ExecutionStatusQuery, error)
func ImportReconciliationStatus(context.Context, types.ReconciliationStatusInput) error
```

Operator routes: `POST /api/v1/executions/{id}/cancel`, `/status-queries`, and `/reconciliation-events`; executor polling/ack routes remain under `/api/executor/v2`.

- [ ] Test cancellable/non-cancellable steps, duplicate cancel, status before retry after acknowledged delivery, callback loss to proven success/failure/unknown, expired callback rejection, new reconciliation event identity, and retry only for declared-idempotent incomplete operations.
- [ ] Integrate campaign cancel/retry with these controls; never invent success.
- [ ] Verify and commit.

```powershell
go test ./internal/executionprotocol ./internal/campaigns ./internal/db ./api ./internal/handlers -run 'Cancel|StatusQuery|CallbackLoss|ReconciliationStatus' -count=1 -race
mise run lint:migrations
git add internal/migrations/sql/157_* internal/executionprotocol/controls* internal/db/execution_v2* api/execution_v2.go internal/handlers/execution_v2.go docs
git commit -m "feat: reconcile uncertain executor outcomes"
```

## Task 12: PR-077 — Desired State, Independent Observation, and Drift

**Files:**

- Create: `internal/migrations/sql/158_desired_observed_reconciliation.up.sql`
- Create: `internal/migrations/sql/158_desired_observed_reconciliation.down.sql`
- Create: `internal/types/desired_state.go`
- Create: `internal/types/observation.go`
- Create: `internal/types/reconciliation.go`
- Create: `internal/desiredstate/state.go`
- Create: `internal/desiredstate/state_test.go`
- Create: `internal/observation/ingest.go`
- Create: `internal/observation/ingest_test.go`
- Create: `internal/observation/gate.go`
- Create: `internal/observation/gate_test.go`
- Create: `internal/reconciliation/drift.go`
- Create: `internal/reconciliation/drift_test.go`
- Create: `internal/db/desired_observed_state.go`
- Create: `internal/db/desired_observed_state_test.go`
- Create: `api/observation.go`
- Create: `api/reconciliation.go`
- Create: `internal/handlers/observations.go`
- Create: `internal/handlers/observations_test.go`
- Create: `internal/handlers/reconciliation.go`
- Create: `internal/handlers/reconciliation_test.go`
- Modify: `internal/routing/routing.go`
- Create: `docs/adr/0064-independent-observed-state.md`
- Create: `docs/fork/PR-077_DESIRED_OBSERVED_RECONCILIATION.md`

Migration 158 creates `PendingDesiredRevision`, `ActiveDesiredRevision`, `ComponentDesiredStateHead`, `ExecutorReport`, `ObserverRegistration`, `ObservedComponentState`, `ComponentObservationHead`, `DriftCase`, `DriftCaseEvent`, and `ReconciliationAction`. Existing `TargetComponentState`/`TargetComponentObservation` remain labeled legacy/executor projection.

```go
func AdmitPendingDesiredRevision(context.Context, types.PendingDesiredRevisionInput) (*types.PendingDesiredRevision, error)
func AdvanceActiveDesiredRevision(context.Context, uuid.UUID, types.ObservationGateResult) error
func IngestObservation(context.Context, types.ObservationEnvelope) (*types.ObservedComponentState, error)
func EvaluateObservationGate(context.Context, uuid.UUID) (types.ObservationGateResult, error)
func ClassifyDrift(types.ActiveDesiredRevision, types.ObservedComponentState) types.DriftClassification
func OpenDriftCase(context.Context, types.DriftInput) (*types.DriftCase, error)
func ResolveDriftCase(context.Context, types.ReconciliationDecision) error
```

Observer route: `POST /api/observer/v1/observations`; management routes: `/api/v1/observer-registrations`, `/observations`, `/drift-cases`, and `/reconciliation-actions`.

- [ ] Test trusted/fresh/in-scope observation, stale/replayed/out-of-order/untrusted/conflicting rejection or retained evidence, timeout quarantine, executor-success/runtime-wrong, manual image/config/schema drift, partial success, unknown/cancel/failure, and no lock/fence deadlock.
- [ ] Test active state advances only for independently verified components; prior active remains for terminal pending failure; accepted deviation is time-bound and does not rewrite desired history.
- [ ] Implement observer authentication, monotonic sequence/freshness, gate, drift cases, and approved reconciliation plans/actions.
- [ ] Verify and commit.

```powershell
go test ./internal/desiredstate ./internal/observation ./internal/reconciliation ./internal/db ./api ./internal/handlers -run 'Desired|Observation|Drift|Reconciliation' -count=1 -race
mise run lint:migrations
git add internal/migrations/sql/158_* internal/types/desired_state.go internal/types/observation.go internal/types/reconciliation.go internal/desiredstate internal/observation internal/reconciliation internal/db/desired_observed_state* api/observation.go api/reconciliation.go internal/handlers/observations* internal/handlers/reconciliation* internal/routing/routing.go docs
git commit -m "feat: verify independent observed state"
```

## Task 13: PR-078 — Correlated Audit and External Export

**Files:**

- Create: `internal/migrations/sql/159_control_plane_audit_export.up.sql`
- Create: `internal/migrations/sql/159_control_plane_audit_export.down.sql`
- Create: `internal/types/control_plane_audit.go`
- Create: `internal/db/control_plane_audit.go`
- Create: `internal/db/control_plane_audit_test.go`
- Create: `internal/auditexport/bundle.go`
- Create: `internal/auditexport/bundle_test.go`
- Create: `internal/auditexport/worker.go`
- Create: `internal/auditexport/worker_test.go`
- Create: `api/control_plane_audit.go`
- Create: `internal/handlers/control_plane_audit.go`
- Create: `internal/handlers/control_plane_audit_test.go`
- Modify: `internal/routing/routing.go`
- Create: `docs/adr/0065-control-plane-audit-export.md`
- Create: `docs/fork/PR-078_CONTROL_PLANE_AUDIT_EXPORT.md`

Migration 159 creates append-only `ControlPlaneAuditEvent`, `AuditExportSink`, `AuditExportCheckpoint`, and `AuditExportAttempt`. Events correlate release/config/plan/approval/campaign/wave/execution/adapter/observation/reconciliation/actor/outcome IDs and checksums; payloads are bounded/redacted.

```go
func AppendControlPlaneAuditEvent(context.Context, types.ControlPlaneAuditEventInput) (*types.ControlPlaneAuditEvent, error)
func BuildDeploymentEvidenceBundle(context.Context, types.EvidenceBundleQuery) (*types.EvidenceBundle, error)
func ExportAuditBatch(context.Context, uuid.UUID, int) (types.ExportBatchResult, error)
```

Routes: paginated `/api/v1/control-plane-audit/events`, `/evidence-bundles`, `/export-sinks`, and `/export-status` with `AuditView`/`AuditExport` actions.

- [ ] Test complete correlation, deterministic bundle checksum, cross-org isolation, secret/oversize redaction, ordered retry, sink failure/lag alert, idempotent export, and primary event retention after failure.
- [ ] Instrument every v2 privileged mutation and state transition through one append helper in the same transaction/outbox boundary.
- [ ] Verify and commit.

```powershell
go test ./internal/auditexport ./internal/db ./api ./internal/handlers -run 'ControlPlaneAudit|EvidenceBundle|AuditExport' -count=1 -race
mise run lint:migrations
git add internal/migrations/sql/159_* internal/types/control_plane_audit.go internal/db/control_plane_audit* internal/auditexport api/control_plane_audit.go internal/handlers/control_plane_audit* internal/routing/routing.go docs
git commit -m "feat: export correlated control plane audit"
```

## Governance and Execution Exit Gate

- [ ] PR-066 through PR-078 are individually reviewed and accepted.
- [ ] Migrations 147–159 upgrade a migration-146 fixture and all backfilled roles/enrollments are countable/restartable.
- [ ] A plan cannot execute without scoped enrollment, policy, current approvals, calendar/freeze admission, campaign membership, adapter match, signed intent, and locks.
- [ ] V1 regression proves ADR-0052 behavior byte/status/event compatible with both flags disabled.
- [ ] V2 duplicate delivery/callback, lease loss, stale fence, process restart, cancel, timeout, callback loss, and unknown state release or transfer every lock atomically.
- [ ] Executor success with wrong independent observation fails the deployment and opens reconciliation.
- [ ] One evidence bundle traces the entire neutral deployment and audit-export failure remains visible/retryable.
