# Choice TP DEV single-reviewer pilot exception

Distr keeps four-eyes approval as the default. This exception exists only for the owner-approved Choice TP DEV proof of concept when no second reviewer account is available. It does not create, alias, or claim a second identity.

## Enable the bounded exception

All five settings are required. If the feature flag is absent, the normal distinct-reviewer and requester-cannot-approve rules remain in force.

The pilot flag must be named explicitly. `DISTR_EXPERIMENTAL_FEATURE_FLAGS=all` deliberately does not enable it.

```dotenv
DISTR_EXPERIMENTAL_FEATURE_FLAGS=operator_control_plane_v2,executor_protocol_v2,scoped_single_reviewer_pilot
DISTR_SINGLE_REVIEWER_PILOT_ORGANIZATION_ID=<exact Distr organization UUID>
DISTR_SINGLE_REVIEWER_PILOT_ENVIRONMENT_ID=<exact Choice TP DEV environment UUID>
DISTR_SINGLE_REVIEWER_PILOT_DEPLOYMENT_TARGET_ID=<exact Choice TP DEV target UUID>
DISTR_SINGLE_REVIEWER_PILOT_APPROVAL_REFERENCE=owner-approval:choice-tp-dev-20260903
```

The process fails closed at startup if the feature is enabled with an incomplete or malformed scope. The exception applies only when:

- the authenticated requester approves their own deployment request in the configured organization and environment;
- the protected-history issuer submits themselves as reviewer for exactly the configured deployment target, with no customer-wide scope, while that target has an active assignment to the configured Choice TP DEV environment at capture time; and
- the decision is `APPROVE`; rejection and all other authorization checks are unchanged.

## Audit evidence

The real authenticated account remains both identities. Distr stores `governanceExceptionKey=scoped-single-reviewer-pilot` and the configured approval reference on the append-only approval decision or protected-history artifact. The protected-history request and retention checksums include both fields, and its control-plane audit event must match them.

Disable the feature immediately after the pilot. Existing exception evidence remains readable and cannot be silently downgraded; migration rollback is refused while any exception evidence exists.

Before testing retention, verify the Choice TP DEV target's active `TargetEnvironmentAssignment` points to the UUID configured by `DISTR_SINGLE_REVIEWER_PILOT_ENVIRONMENT_ID`. Distr reads and locks that assignment itself; no governance-exception fields are accepted from the API caller.

## Not an enterprise default

Do not enable this setting for another client, production environment, customer-wide export, sample-data retirement, or routine operation. Standard client onboarding must provision a real second reviewer and continue to use the existing four-eyes policy.
