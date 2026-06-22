# ADR-0044: Deployment Timeline and Compare

## Status

Accepted

## Context

The roadmap needs a deployment timeline that shows release history, last successful deployment, comparison between two deployments, and a way to deploy a previous release. Earlier PRs already introduced durable task history, deployment plans, release bundles, process snapshots, variable snapshots, and task logs.

The implementation should avoid implying that deploying a previous release reverses external side effects. It should also avoid introducing duplicate history stores when existing immutable records already contain the required data.

## Decision

Add a feature-flagged deployment timeline API and UI over existing deployment task records.

Persist the actor that created deployment tasks in `Task.actor_user_account_id`, because the timeline needs to show who initiated the deployment and task creation already receives the actor from authenticated handlers.

Derive timeline entries from `Task` joined to deployment plans, plan targets, release bundles, applications, channels, and environments. Mark the last successful deployment by comparing successful tasks for the same organization, application, environment, and deployment target.

Derive comparisons from the existing release bundle components, deployment plan steps, deployment plan variables, and process snapshot checksums. Do not add new snapshot tables in this PR.

Expose deploy previous release as plan creation from the selected task's release bundle, environment, and target. The API response and UI confirmation include forward-only warning text.

## Consequences

The timeline reflects current durable task history without duplicating records.

Older tasks created before this migration may have no actor and remain valid.

Deploying a previous release is explicit plan creation. It does not claim to undo external infrastructure, application, or database changes.

Existing deployment execution, task leases, rolling and blue-green lifecycle primitives, traffic providers, Docker/Kubernetes agents, runbooks, webhook actions, and release-bundle behavior remain unchanged.
