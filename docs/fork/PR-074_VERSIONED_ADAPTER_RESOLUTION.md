# PR-074 - Versioned Adapter Resolution

## User story

As a release operator, I need every typed deployment step to use one exact
target-installed adapter implementation and immutable non-secret
configuration, so that approval cannot silently authorize a different adapter
version, capability, target scope, configuration, or signing key.

## Behavior

Component Release v2 contracts may declare adapter requirements by typed step
kind (`deploy`, `migration`, `backup`, or `health`). Each requirement contains
a stable capability key and an exact semantic version. The release requirement
is authoritative: an assignment cannot substitute a different capability or
version.

Plan validation resolves each requirement against organization-scoped facts:

- an enabled, versioned adapter implementation;
- an exact capability and capability version;
- an enabled assignment for the selected target, deployment unit, component
  instance, database resource, or observer registration;
- the selected Target Config Snapshot ID and canonical checksum; and
- non-secret signing configuration consisting of a key ID, public-key
  fingerprint, opaque secret-provider reference, and non-reversible secret
  version fingerprint.

Zero matches is a blocker. More than one match is ambiguous and is also a
blocker. Resolution never falls back to a weaker assignment capability.

Published target plans freeze a `DeploymentPlanStepAdapter` for each resolved
step. The frozen evidence is part of the canonical plan payload and is
available on existing deployment-plan reads as `stepAdapters`.

At preflight/start, the repository reloads the current assignment,
implementation, capability, configuration, and fingerprints. Removal,
disablement, implementation-version drift, capability drift, scope drift,
configuration drift, or key-fingerprint drift blocks start. Recovery requires
restoring the exact frozen state or publishing and approving a new plan and,
after campaign integration, a new campaign revision.

## Schema

Migration 156 adds:

- `AdapterImplementation`, immutable by `(organization, key, version)`;
- `AdapterCapability`, versioned capability declarations for an implementation;
- `AdapterAssignment`, target-scoped implementation/configuration bindings; and
- append-only `DeploymentPlanStepAdapter` plan evidence.

The down migration refuses rollback while frozen plan adapter evidence exists.
Schema checks reject embedded private-key material and accept only opaque
`secret-provider://` references plus lowercase `sha256:` fingerprints.

## API and authorization

The additive, organization-scoped routes are:

- `GET|POST /api/v1/adapter-implementations`
- `GET|POST /api/v1/adapter-assignments`

Existing `GET /api/v1/deployment-plans/{deploymentPlanId}` responses include
read-only `stepAdapters`.

Both list routes use deterministic, opaque keyset cursors with a default page
size of 50 and a maximum of 100.

The route families require vendor organization access and
`operator_control_plane_v2`. Mutations additionally require read-write/admin
authority and block super-admin mutation. Repository predicates validate
organization ownership without revealing whether a foreign identifier exists.

## Security

- Private key bytes never enter release contracts, assignments, plans, logs,
  errors, or evidence.
- A signing private key remains in its secret provider.
- Plans retain only an opaque provider reference, key ID, public-key
  fingerprint, and non-reversible key-version fingerprint.
- Public errors are tenant-safe and do not echo database or credential detail.
- Adapter and plan facts are deterministically ordered before checksum
  calculation.

## Compatibility and predecessor seams

Existing Component Release v2 payloads remain byte-compatible because
`adapterRequirements` is additive and omitted when absent. Existing v1 plans
and execution are unchanged. Protocol v2 plans remain blocked until PR-075
installs the signed, fenced executor.

This synthetic worktree starts at PR-063. Migrations 140-142 and 146-155, plus
the PR-071 through PR-073 campaign domain, are not present here. Migration 156
retains its allocated number. Once the predecessor series is integrated,
campaign admission must use the plan's frozen `DeploymentPlanStepAdapter`
records and treat adapter drift as a material start-time change requiring a
new approved revision.

## Validation

Focused tests cover exact capability/version resolution, missing and ambiguous
implementations, scope and config checksum mismatch, disabled assignments,
release authority, start-time version/fingerprint drift, API validation,
tenant-safe handlers, plan preflight, migration shape, and routing.

Migration lint cannot be green on this synthetic base until the allocated
predecessor migration pairs are integrated. The repository-local `mise`
launcher also requires an explicit trust decision outside this feature.
