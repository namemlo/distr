# PR-041 - Rolling deployment strategy

PR-041 adds the first rolling deployment state-machine foundation. It models rolling windows, per-target rollout state, and failure-threshold decisions without wiring the scheduler, task queue, database, API, UI, traffic providers, or agents.

## Scope

Included:

- core `internal/deployments` package for rolling rollout state
- per-target states: pending, in-progress, succeeded, failed, and skipped
- rollout phases: pending, in-progress, paused, aborted, and completed
- rolling window selection with `window_size`
- `maximum_unavailable` cap for targets started in one window
- `pause_between_windows` stored as part of rollout configuration
- absolute and percentage failure thresholds
- threshold actions for pausing or aborting rollout state
- stable target ordering by sort order and deployment-plan target ID

Not included:

- database schema or repository persistence
- deployment plan API changes
- task queue, task lease, scheduler, or worker execution wiring
- agent protocol or capability changes
- traffic-provider abstractions
- blue-green, canary, or custom adapter behavior
- UI changes for deployment visualization

## State Model

`RollingState` starts with normalized targets in `PENDING` state. `StartNextWindow` moves the next ordered subset to `IN_PROGRESS`, bounded by `window_size` and `maximum_unavailable`.

A window does not advance while any target in the current window is still non-terminal. Once the current window is terminal, the next call to `StartNextWindow` starts the next pending subset. When all targets are terminal, the phase becomes `COMPLETED`.

Failure thresholds can use either an absolute failed-target count or a failure percentage. When a threshold is reached, the configured action moves the rollout to `PAUSED` or `ABORTED`.

## Verification

Focused Go tests cover:

- initial target normalization and stable ordering
- window start limits
- window advancement only after terminal target states
- pause and abort failure-threshold actions
- percentage-based failure thresholds
- invalid rolling configuration rejection
