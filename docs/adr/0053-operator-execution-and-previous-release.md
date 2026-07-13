# ADR-0053: Operator Execution Detail And Previous Release

## Status

Accepted

## Context

The control plane already stores durable tasks, step runs, progress events, release snapshots, and deployment
timeline comparisons. Operators nevertheless had to open raw JSON logs to follow execution, and the previous-release
action could create a plan from any deployment task with a release bundle, including an unsuccessful task. Execute
and previous-release actions also returned users to an unselected list rather than the exact task or plan.

## Decision

Use the existing Deployment Timeline side panel for two operator views: structured Execution detail and release
Comparison. The frontend reads the existing task and task-timeline endpoints, orders steps and events, and polls an
active selected task every three seconds until it reaches a terminal state. Raw logs remain available for detailed
investigation. Output values are displayed only when neither the sensitive nor redacted marker is set.

Use query parameters as record-selection deep links. Executing a plan navigates to Deployment Timeline with the
first created task ID. Creating a previous-release plan navigates to Deployment Plans with the new plan ID. Each
destination opens its requested record while preserving the existing route and layout.

Treat Deploy Previous Release as a reviewed forward deployment, not an inverse operation. The selected source must
be a successful deployment task older than the latest timeline attempt for the same application and target. Before
confirmation, compare the latest attempt with the selected historical task. The backend independently requires a
successful deployment task and creates a fresh immutable plan from its frozen release, environment, and target.
Normal execution-time preflight and concurrency checks still apply when the new plan is executed.

## Consequences

Operators can monitor a deployment from the control plane without depending on an external executor portal, while
raw provider logs remain reachable. Recovery selection is explicit, comparison-backed, and protected at both UI and
database boundaries. Deep links preserve context between plan execution, task monitoring, and historical recovery.

No migration, new endpoint, provider-specific schema, or agent protocol change is required. The three-second poll
adds bounded read traffic only while a selected task is queued or running. Previous-release deployment does not undo
database migrations or external side effects; those remain explicit release-contract and runbook responsibilities.

## Alternatives Considered

- Add a third page or table column for execution detail. Rejected because the existing side panel can switch views
  without reducing timeline scan space.
- Keep raw task JSON as the primary progress view. Rejected because operators need ordered step and progress state.
- Allow failed or running tasks as historical sources. Rejected because they do not prove a successfully deployed
  release.
- Invoke an inverse rollback operation. Rejected because external side effects and database migrations are not
  generally reversible; a fresh immutable deployment plan is auditable and reruns preflight.
- Store provider-specific job state in the UI. Rejected because durable task events and callbacks are the
  provider-neutral execution contract.

## Validation

- Angular tests cover task API calls, execution rendering, deep links, current-versus-historical comparison, and
  guarded previous-release creation.
- PostgreSQL 18 repository tests cover successful source creation, unsuccessful source conflict, and timeline
  availability.
- PostgreSQL 18 handler tests cover authenticated list, compare, and previous-release plan creation.
