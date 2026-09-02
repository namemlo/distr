# Post-PR-099 - External-execution pre-mutation hold

## User story

As a release operator, I can arm one exact plan/checksum/target/component hold so
that Distr visibly fails the first matching external execution before the adapter
is invoked, then automatically allows a retry.

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
  "reason": "demonstrate a dependency block before adapter mutation"
}
```

Set the JSON as `DISTR_EXTERNAL_EXECUTION_PRE_MUTATION_HOLD_JSON` and restart the
Hub. Do not put credentials or secret values in the reason.

## Behavior

- Only a callback-mode `distr.webhook` external execution can reach this boundary.
- Every identity field must match the queued execution exactly.
- The first match is atomically failed before webhook invocation.
- The failed external execution reports the control ID/checksum, remains at zero
  trigger attempts, and appears through the existing task/external-execution UI.
- Audit contains one armed event and one consumed event with
  `adapterInvoked=false`.
- The consumed event automatically disables that exact control. A retry proceeds
  without changing the environment variable.

## Compatibility and rollback

No schema, public API, agent protocol, adapter contract, or existing disabled-path
behavior changes. Remove `external_execution_pre_mutation_hold` from the flag list
to disable all configured holds. Audit and execution history are never removed.

## Verification

```text
go test ./internal/externalexecution ./internal/featureflags
go test ./internal/hubexecutor ./internal/db ./internal/svc
```
