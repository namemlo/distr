# PR-053 - Operator Execution Detail And Previous Release

## Generic User Story

As a deployment operator, I want to follow structured task progress and create a reviewed deployment plan from a
successful historical release, so that execution and recovery remain visible and auditable in the deployment
control plane.

## Scope

- Reuse the existing Deployment Timeline side panel for execution detail and comparison tabs.
- Load task and step-run detail from the existing task API and poll active tasks every three seconds.
- Show ordered steps, status, progress, events, logs, and fail-closed redacted outputs.
- Deep-link a newly executed deployment plan to its exact task in Deployment Timeline.
- Restrict previous-release sources to successful historical deployment tasks.
- Compare the latest target attempt with the selected historical release before operator confirmation.
- Create a new immutable deployment plan and deep-link the operator to its plan detail.

## Required Impact Report

### Database/schema impact

None. The source task status is validated from the existing durable task record before a previous-release plan is
created.

### Public API impact

No endpoints or response fields are added. The existing redeploy endpoint now returns `409 Conflict` unless the
source is a successful deployment task. Timeline `redeployAvailable` is false for non-successful tasks.

### Frontend/UI impact

Deployment Timeline keeps its existing two-column layout. The right panel now has Execution and Comparison tabs.
Execution renders structured task progress and retains Raw logs as a lower-level link. Execute Plan navigates with
`taskId`; previous-release creation navigates with `planId`, and both pages open the requested record.

The previous-release action is shown only for a successful task that is older than the latest attempt for the same
application and deployment target. The operator sees the current-versus-historical comparison before confirming.

### Agent/protocol impact

None. Task, lease, callback, Docker agent, and Kubernetes agent contracts are unchanged.

### Feature-flag impact

No new flag. The UI and API remain under the existing `deployment_timeline`, `deployment_plans`, `task_queue`, and
`step_events` feature surfaces.

### Security impact

Positive. Sensitive task outputs remain redacted by the backend and the UI fails closed for outputs marked either
sensitive or redacted. Previous-release creation cannot use queued, running, failed, canceled, legacy, or runbook
tasks. The normal authenticated organization and role middleware remains authoritative.

### Backward-compatibility impact

Existing successful task history remains eligible. Existing comparison and raw-log links remain available. A
previous release always creates a new plan and reruns current preflight; it does not reverse database migrations,
external side effects, or configuration changes outside the frozen release contract.

## Validation

- Focused Angular task service, Deployment Timeline, and Deployment Plan tests.
- Live PostgreSQL 18 repository tests for successful, unsuccessful, and historical task behavior.
- Live PostgreSQL 18 authenticated handler list, compare, and redeploy flow.
- Full Go suite, Angular suite, community build, formatting, diff check, and credential scan before deployment.
