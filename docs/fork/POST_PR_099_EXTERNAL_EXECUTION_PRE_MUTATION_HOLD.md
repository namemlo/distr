# Post-PR-099 - External-execution pre-mutation hold

## User story

As a release operator, I can arm one exact plan/checksum/target/component hold,
observe it waiting while the deployment lock is retained, prove conflicting work
is rejected, and explicitly release it to fail before the adapter is invoked.

## Enablement

The capability is off by default. Enable both flags:

```text
DISTR_EXPERIMENTAL_FEATURE_FLAGS=operator_control_plane_v2,external_execution_pre_mutation_hold
```

Then provide one non-secret binding:

```json
{
  "schema": "distr.external-execution-pre-mutation-hold/v1",
  "controlId": "00000000-0000-4000-8000-000000000001",
  "organizationId": "00000000-0000-4000-8000-000000000002",
  "deploymentPlanId": "00000000-0000-4000-8000-000000000003",
  "deploymentTargetId": "00000000-0000-4000-8000-000000000004",
  "planChecksum": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "component": "sample-api",
  "reason": "demonstrate a dependency block before adapter mutation",
  "expiresAt": "2026-09-03T12:30:00Z"
}
```

Set the JSON as `DISTR_EXTERNAL_EXECUTION_PRE_MUTATION_HOLD_JSON` and restart the
Hub. Set `DISTR_EXTERNAL_EXECUTION_PRE_MUTATION_HOLD_RELEASE_FILE` to an absolute
path owned by the Hub operator and writable only through the approved operator
session. Do not put credentials or secret values in either document.

## Behavior

- Only a callback-mode `distr.webhook` external execution can reach this boundary.
- Every identity field must match the queued execution exactly.
- The first match enters `ARMED`, then `WAITING`; the external execution remains
  `QUEUED` with zero trigger attempts and an explicit waiting message.
- The Hub keeps heartbeating its task lease, so the existing target/component lock
  remains active and a `REJECT_NEW` request for that resource conflicts.
- No webhook or Jenkins adapter is invoked while waiting.
- A malformed or non-matching release file is ignored until corrected or expiry.
- Expiry automatically resolves the hold as `TIMED_OUT` and fails the execution.
- Audit contains armed, waiting, resolution, and consumed events with
  `adapterInvoked=false`.
- The consumed event automatically disables that exact control. A retry proceeds
  without changing the environment variable.

## Release-to-fail action

After observing WAITING and the expected competing-request conflict, atomically
place this exact-bound document at the configured release path:

```json
{
  "schema": "distr.external-execution-pre-mutation-hold-release/v1",
  "action": "RELEASE_FAIL",
  "controlId": "00000000-0000-4000-8000-000000000001",
  "controlChecksum": "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
  "organizationId": "00000000-0000-4000-8000-000000000002",
  "deploymentPlanId": "00000000-0000-4000-8000-000000000003",
  "deploymentTargetId": "00000000-0000-4000-8000-000000000004",
  "planChecksum": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "component": "sample-api"
}
```

The `controlChecksum` is emitted in the WAITING message and audit payload. Write
to a temporary file with restrictive permissions, then rename it onto the release
path so the Hub never observes a partial document.

## Compatibility and rollback

No schema, public API, agent protocol, adapter contract, or existing disabled-path
behavior changes. Remove `external_execution_pre_mutation_hold` from the flag list
to disable all configured holds. Audit and execution history are never removed.

## Verification

```text
go test ./internal/externalexecution ./internal/featureflags
go test ./internal/hubexecutor ./internal/db ./internal/svc
```
