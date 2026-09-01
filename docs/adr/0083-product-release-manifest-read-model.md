# ADR-0083: Complete Product Release Manifest Read Model

## Status

Accepted.

## Context

`distr.product-release/v1` already freezes the target-neutral Product Release
manifest, its exact Component Release contract snapshots, and the capability
graph. The public Product Release response and release-detail UI exposed only a
subset of those retained facts. Operators could see component IDs and versions
but not the dependency-policy checksum, OCI manifest and platform digests,
provided and required capabilities, migration declarations/contracts, or the
frozen graph order.

Target-specific dependency selection is a separate concern. A Product Release
declares allowed resolution modes, while an immutable Target Deployment Plan
selects `included`, `pinned_existing`, or another permitted mode using native
`RequirementResolution` rows. Copying the selected provider into the Product
Release would make a target-neutral release target-specific and create two
authoritative records.

Product Release v1 also has no compatibility-group or rollback-group field.
Inferring one from component order or dependencies would create execution
policy that was never published.

## Decision

Product Release create, get, and publish responses expose the complete retained
manifest read model:

- dependency-policy version and checksum;
- canonical and graph checksums;
- release notes and required platforms;
- exact Component Release IDs, checksums, component keys, versions, and
  platforms;
- artifact media types, immutable manifest digests, and per-platform digests;
- provided capabilities, component and product requirements, and their allowed
  resolution modes;
- migration declarations and complete structured migration contracts; and
- graph nodes, edges, checksum, and topological order.

The handler reloads the persisted Product Release graph for every create, get,
and publish response. Mapping fails closed when the dependency-policy checksum,
component contract snapshot, graph checksum, graph integrity, or frozen
component identity is unavailable or inconsistent. An incomplete stored read
model is returned as a conflict instead of a partial successful manifest.

The operator plan-detail read model additively exposes the native typed
`requirementResolutions` collection. The release UI may render selected target
resolution only when a frozen plan context is present and its Product Release
ID and checksum match the displayed Product Release. The plan remains the sole
owner of selected mode, provider identity, platform, observed/desired state
evidence, provider approval/probe evidence, and binding checksum.

Compatibility and rollback grouping remain owned by the frozen process, Target
Deployment Plan, and review material. The Product Release UI states that the
field is unavailable in v1 and does not infer or duplicate it.

## Consequences

- Operators can audit the complete build-to-release identity without querying
  internal persistence or reconstructing the graph in the browser.
- Target resolution remains target-specific and checksum-bound to one immutable
  plan.
- Existing persisted Product Release canonical bytes and database schema do not
  change; this is an additive API/read-model and UI change.
- API consumers must tolerate the additive `graph`, component-contract fields,
  and plan `requirementResolutions` collection.
- Corrupt or historically incomplete Product Release records no longer render
  as trustworthy partial manifests; repair or explicit migration is required.

## Alternatives Considered

Embedding selected providers in Product Release v1 was rejected because the
same Product Release may resolve differently for different targets. Deriving
provider selection from graph edges was rejected because target-stage
requirements are intentionally unresolved until planning. Inferring rollback
groups from graph order was rejected because ordering is not a compatibility
or recovery guarantee.

## Validation

Focused Go mapping, handler, and database tests cover complete projections,
projection isolation, graph and policy checksum conflicts, missing contract
snapshots, and native requirement-resolution preservation. Angular tests cover
complete manifest rendering, dependency-policy and graph parity, selected
resolution evidence, absent/mismatched plan context, and safe incomplete-state
messages. No live system, client database, or external executor is required.
