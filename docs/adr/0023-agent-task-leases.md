# ADR-0023 - Agent task leases

## Status

Accepted

## Context

PR-020 introduced durable Tasks and StepRuns. PR-021 added lock and concurrency semantics. PR-022 added agent capability reports and target-executed compatibility checks. The next roadmap step needs agents to claim executable work and heartbeat ownership, without adding step events, logs, completion, or execution adapters.

The lease protocol must preserve organization isolation, avoid raw token storage, and remain safe for existing agents that do not know about leases.

## Decision

Add a feature-flagged `TaskLease` model and hidden agent-authenticated endpoints:

```http
POST /api/v1/agents/{id}/lease
POST /api/v1/agents/{id}/tasks/{taskId}/heartbeat
```

The lease endpoint:

- requires `agent_task_leases` and `task_queue`
- uses existing agent token authentication
- requires the path agent ID to match the authenticated deployment target
- claims only Tasks for that deployment target and organization
- claims only Tasks with at least one included target-executed StepRun
- runs claim, lock acquisition, Task transition, expired-lease release, and new lease insertion in one transaction
- stores only a SHA-256 hash of the opaque lease token

Heartbeat validates the active lease token hash, rejects expired leases with conflict, and extends heartbeat and expiry timestamps.

Expired running leases can be reclaimed by a later claim for the same Task. Reclaim releases the expired lease row, creates a new active lease with the next attempt number, and leaves Task resource locks held by the Task.

Generated Docker and Kubernetes manifests include lease endpoint variables, and the agent client exposes helper methods. Existing agent main loops do not execute leased Tasks in PR-023.

## Consequences

- Agents can safely claim durable target-executed work without adding execution adapters.
- A Task has at most one active lease.
- Lease tokens are not stored in plaintext.
- Hub-executed-only Tasks are not claimed by target agents.
- Existing queued and lock behavior remains the source of truth for concurrency.
- Later PRs can add step events, logs, completion, and adapters on top of this lease boundary.

## Alternatives Considered

Storing raw lease tokens was rejected because replay resistance should not depend on database secrecy alone.

Claiming hub-executed steps through target-agent leases was rejected because hub execution belongs to a different executor path and PR-022 already treats hub-executed steps separately from target capability checks.

Marking expired Tasks back to `QUEUED` was rejected for PR-023 because the Task may already hold resource locks. Reclaiming by replacing only the lease preserves lock ownership while allowing a restarted agent to resume the Task attempt.

Adding task completion, step events, logs, or action adapters now was rejected because those behaviors belong to PR-024 and later roadmap work.
