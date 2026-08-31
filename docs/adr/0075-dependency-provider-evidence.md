# ADR-0075: Fresh Dependency Provider Evidence

## Status

Accepted.

## Context

Target-plan dependency resolution previously compared expected and observed
state checksums but did not freeze the observation freshness boundary or prove
that the selected observation was still the trusted current observation.
`approved_external` also treated an observation as approval evidence without
binding an actual approval identity or a separately checksummed contract
probe.

That permits an otherwise checksum-correct provider state to be selected after
its observation has expired or been superseded. It also makes an external
provider decision impossible to audit independently from health evidence.

## Decision

Provider evidence version 2 is additive to immutable target plans. Historical
version-1 plan rows remain readable and retain their original checksums.

`pinned_existing`, `shared_provider`, and `approved_external` candidates now
require an `ObservedComponentState` that is all of:

- trusted and accepted;
- the current observation for its registered observer and component; and
- fresh through the plan's server-selected decision instant.

The immutable resolution freezes `freshUntil`, trusted/current decisions, and
the exact observed-state lineage. Legacy `TargetComponentObservation` rows do
not carry this contract and therefore remain readable but are no longer
eligible to authorize a new provider resolution.

An `approved_external` resolution additionally freezes:

- the provider deployment plan's still-valid `APPROVED` approval-request ID;
- the canonical approval-evidence checksum derived from that request;
- the contract-probe `ObservedComponentState` ID; and
- the probe evidence checksum.

The approval must match the provider plan's immutable plan, policy, and
subscriber-set checksums and must be unexpired at the planning instant. The
contract probe must be the same trusted current fresh observation selected for
the provider state. Approval and probe evidence are separate fields and
foreign keys; neither substitutes for the other.

Migration 168 adds the evidence fields and foreign keys to
`DeploymentPlanResolvedRequirement`. New rows use evidence version 2. The
downgrade refuses to discard any version-2 resolution.

## Consequences

- Superseded, untrusted, or expired observations cannot authorize a new plan.
- External providers fail closed when either approval or contract-probe
  evidence is absent.
- A provider approval change, probe change, or freshness change produces a new
  requirement binding and plan checksum.
- Existing published plans and v1 API behavior remain readable and unchanged.
