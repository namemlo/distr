# PR-044 - Deployment timeline and compare

PR-044 adds a feature-flagged deployment timeline for durable deployment tasks. Vendor admins can review deployment history, compare two deployment entries, open task logs, and create a new deployment plan for a previous release.

## Scope

Included:

- `deployment_timeline` experimental feature flag
- nullable task actor persistence through `Task.actor_user_account_id`
- deployment timeline repository over durable `Task`, `DeploymentPlan`, `DeploymentPlanTarget`, `ReleaseBundle`, `Application`, `Channel`, and `Environment` records
- `GET /api/v1/deployment-timeline`
- `GET /api/v1/deployment-timeline/compare`
- `POST /api/v1/deployment-timeline/{taskId}/redeploy`
- Angular timeline route and sidebar entry
- timeline filters for search, status, and in-progress tasks
- last-successful marker per application, environment, and target
- component, variable, step, and process comparison view
- task-log links for timeline entries
- deploy previous release confirmation and warning text

Not included:

- executing the newly created deployment plan
- automatic promotion to another environment
- task scheduling or lease behavior changes
- rollback of external systems or database state
- new release snapshot tables
- blue-green or rolling orchestration wiring
- agent protocol changes

## Behavior

The timeline is built from immutable deployment task history. Entries are ordered by completed, started, or queued time and can be filtered by application, release bundle, environment, deployment target, customer organization, terminal status, and limit.

Each item exposes release number, channel, process snapshot, variable snapshot, actor, target, status, timing, component versions, task log link, last-successful marker, and whether deploying the previous release is available.

Comparison loads the two selected tasks and compares:

- release components by component key and version
- deployment process snapshot revision and checksum
- resolved deployment plan steps by step key
- resolved variables by key, status, source, redaction, reference, and non-redacted value

Deploy previous release creates a new deployment plan using the selected task's release bundle, environment, and deployment target. The UI and API warning state that this creates a new plan and does not reverse external state or database changes.

## Verification

Focused tests cover:

- timeline filtering and last-successful marking
- actor persistence on created deployment tasks
- release, process, component, and variable comparison
- creating a previous-release deployment plan from a timeline task
- handler list, compare, and deploy-previous-release flows
- Angular service requests
- Angular timeline filtering, comparison, and confirmation behavior
