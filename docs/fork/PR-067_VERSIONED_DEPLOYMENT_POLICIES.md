# PR-067 - Versioned Deployment Policies

## Generic User Story

As an operator, I want immutable, versioned deployment policies composed across an owning authority and every
subscriber so that a deployment plan records the exact governance contract that admitted it.

## Resource and API Contract

- `DeploymentPolicy` is stable organization-owned metadata identified by a canonical key.
- `DeploymentPolicyVersion` is a validated draft until publication. Publication records its actor and time; a
  published version cannot be changed or deleted.
- `DeploymentPolicyBinding` points only to a published version. Active bindings may be retired, never rewritten.
- CRUD, version, validate, publish, and binding routes live below `/api/v1/deployment-policies`.
- Authenticated reads remain available when operator control plane v2 is disabled. Every mutation is hidden unless
  `operator_control_plane_v2` is effective and also requires read-write/admin while excluding super-admin.
- Policy documents use the exact `distr.deployment-policy/v1` typed schema. Unknown request fields, trailing JSON,
  caller-supplied derived authority fields, invalid restricted expressions, and invalid bounds are rejected.

The policy schema contains independent approval quorums and separation constraints, restricted risk gates,
allowed requirement-resolution modes, minimum bake and maximum wait, maintenance-window and freeze-version
references, rollout wave/concurrency/health/failure thresholds, narrowly allow-listed override acceleration,
required evidence, and bootstrap behavior.

## Strict Effective-Policy Composition

Composition is deterministic and conjunctive:

- Owner and subscriber approval rules retain their own authority and version identity; quorums are not merged.
- Allowed requirement-resolution modes and declared maintenance windows intersect.
- Risk gates, freeze references, and evidence requirements union.
- The largest required bake, wait, wave bake, and minimum healthy threshold wins.
- The smallest maximum wave size, concurrency, and failure tolerance wins.
- No common mode or maintenance window is a blocking validation issue.
- Every authority must have at least one valid published policy.
- Shared subscriber UUID membership is sorted and domain-hashed with the registry algorithm. A changed subscriber
  set changes both the subscriber checksum and the effective-policy checksum.

The effective checksum is domain-separated as `distr.effective-deployment-policy/v1` and is independent of query
or insertion order.

## Deployment-Plan Publication

An optional `deploymentUnitId` on plan creation activates v2 policy publication. The unit must be active, match
the requested environment, and belong to one of the selected deployment targets. The plan freezes:

- deployment-unit identity;
- the complete effective-policy snapshot;
- effective-policy checksum; and
- subscriber-set checksum.

Every policy composition issue becomes a deployment-plan blocker. This slice does not create approvals or enable
execution; PR-068 and later slices consume the immutable snapshot. Requests without `deploymentUnitId` preserve
the existing v1 plan behavior and canonical shape.

## Database and Downgrade

Migration 149 adds the three policy resources and optional plan snapshot columns. Composite organization foreign
keys enforce tenant ownership, published versions are guarded at the schema boundary, active binding identity is
unique, and the plan constraint requires all snapshot fields together with matching checksums. Down migration
takes exclusive locks and refuses while any policy or plan-policy evidence exists.

Scoped principal-group validation is activated automatically when the PR-066 `PrincipalGroup` relation is
present. Campaign bindings remain rejected until the campaign resource exists. Owner/subscriber resolution uses
the PR-056 registry identity and frozen subscriber checksum; later rebasing onto PR-063 and PR-066 supplies the
full ownership and action-authorization surfaces without weakening this contract.

## Compatibility and Scope

The change is additive. Existing v1 reads, writes, plans, deployment execution, agents, and external executors are
unchanged. It stores no adopter name, client credential, host, provider, application database data, or external
system configuration.

## Verification

Focused tests cover exact validation, restricted expressions, bootstrap rules, deterministic checksums,
owner/subscriber authority separation, mode/window conflicts, strict thresholds, subscriber invalidation,
migration immutability and downgrade guards, API validation, mapping, strict JSON parsing, mutation kill-switch
behavior, and deployment-plan snapshot canonicalization.

Live PostgreSQL 16/18 migration and repository tests, the full repository suite, containers, and browser
verification remain final stacked-branch gates because migrations 141-148 are supplied by the prerequisite PRs.
