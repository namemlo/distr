# ADR-0035: Webhook Self-Contained Runtime

## Status

Accepted

## Context

PR-031 made webhook execution replay-safe by reading stored task timelines before re-sending external HTTP requests. PR-034 added deterministic audit outputs and strict stored-history verification. Those controls still relied on remote timeline reads and live DNS resolution in the default Docker-agent execution path.

For degraded or air-gapped execution environments, operators need a mode where webhook execution does not call hidden platform timeline APIs and does not perform live DNS lookup after the lease has started. The Docker agent does not currently have a local database dependency, so adding persistent local storage would be a larger architectural change than this hardening step.

## Decision

Add `WEBHOOK_SELF_CONTAINED_MODE=true` as an opt-in Docker-agent runtime mode.

In self-contained mode, webhook replay checks use a local timeline interface instead of `GetTaskTimeline`. `executeTaskLease` wraps the leased-task client with an in-process event mirror that records successful local StepRun event writes and exposes them as a task timeline for replay checks during the same lease execution. The remote `DISTR_TASK_TIMELINE_ENDPOINT_TEMPLATE` path is not called in this mode.

Also add `DISTR_WEBHOOK_RESOLVED_IP_CACHE` for cached-only webhook hostname resolution. When self-contained mode is enabled, non-IP webhook hosts must have a cached IP entry. Missing cache entries fail closed before HTTP transport setup. Cached IP addresses are validated with the same unsafe-IP policy as live DNS results.

The final outbound webhook HTTP request remains the only intended network operation in self-contained mode.

## Consequences

Self-contained mode can run webhook execution without remote timeline reads or live DNS lookup.

Replay within a single lease execution can reconstruct prior webhook success from locally mirrored StepRun events. Replay across agent restarts still requires a future persistent local store; this PR intentionally does not introduce a Docker-agent database.

Existing agents and manifests are unchanged unless `WEBHOOK_SELF_CONTAINED_MODE=true` is set. Normal mode keeps the PR-031 remote timeline replay behavior and live DNS resolution.

