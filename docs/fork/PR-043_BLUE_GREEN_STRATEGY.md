# PR-043 - Blue-green strategy

PR-043 adds a backend-only blue-green lifecycle foundation. It models active/inactive slots, health verification, traffic shift planning, observation checks, promotion, retention, and rollback semantics on top of the PR-042 traffic-provider request contracts.

## Scope

Included:

- `internal/deployments/bluegreen` package
- active and inactive slot lifecycle states
- lifecycle phases for create, deploy, verify, shift, observe, promote, rollback, and completion
- inactive-slot deployment readiness transition
- health-check recording and replacement by check name
- traffic-shift request planning using the PR-042 provider request model
- post-shift observation recording
- promotion that makes the inactive slot active
- retention policy handling for the previous active slot: keep, scale down, or destroy
- rollback request planning that preserves the previous active slot and marks the inactive slot rolled back

Not included:

- database schema or repository persistence
- public API exposure
- scheduler, task queue, task lease, or worker execution wiring
- actual traffic-provider invocation
- Envoy, Nginx, cloud load balancer, or proxy-specific adapters
- UI changes
- changes to PR-041 rolling window/failure-threshold logic
- changes to the PR-042 provider contract or webhook provider

## Lifecycle

The lifecycle starts with an active slot and an inactive slot. The inactive slot moves through deploy and verify phases before a traffic shift can be planned.

Traffic shift planning requires all recorded health checks to pass. The planned provider request includes the active and inactive slots, rollout context, and an idempotency key.

After traffic has shifted, observation checks must pass before promotion. Promotion makes the inactive slot the new active slot and applies the configured retention policy to the previous active slot.

Rollback planning is available from shift or observe phases. It returns a provider rollback request, keeps the previous active slot active, records the rollback reason, and marks the inactive slot rolled back.

## Verification

Focused Go tests cover:

- active/inactive slot initialization
- invalid lifecycle configuration rejection
- health-check gating before traffic shift
- traffic-shift request planning
- promotion and retention-policy state changes
- rollback request planning from observation phase
