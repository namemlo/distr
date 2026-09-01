# PR-096: Complete Product Release Manifest Read Model

## Purpose

Expose the complete immutable Product Release contract and its frozen graph in
the public API and operator UI, then correlate any selected dependency provider
only from the matching immutable Target Deployment Plan.

## Generic user story

As a release operator, I need to inspect every exact component, artifact,
capability, migration, policy, and ordering fact before deployment, and I need
to distinguish target-neutral release constraints from the provider mode chosen
for one target.

## API and read model

- Product Release create, get, and publish responses include the complete
  component contract snapshots and the verified Product Release graph.
- Missing policy checksums, graph checksums, component snapshots, or mismatched
  frozen identities return a conflict rather than a partial successful read.
- Operator plan detail additively includes typed native
  `requirementResolutions`, retaining provider identity, platform, state and
  approval/probe evidence, selection mode, and binding checksum.

## UI behavior

- Draft and published Product Release views show dependency-policy version and
  checksum plus graph checksum and topological order.
- Published detail shows exact Component Release pins, OCI manifest and
  platform digests, provided/required capabilities, migration declarations and
  contracts, and graph edges.
- Selected `included` or `pinned_existing` resolution renders only from a
  frozen plan whose Product Release ID/checksum matches the displayed release.
- Missing or mismatched manifest/plan context fails closed with an explicit
  message.
- Product Release v1 does not invent compatibility or rollback grouping; the UI
  directs operators to the frozen process/plan review material.

## Data, protocol, and compatibility

- Database changes: none.
- Canonical Product Release and plan persistence: unchanged.
- Agent/executor protocol: unchanged.
- API compatibility: additive response fields; existing request and route
  families remain unchanged.
- Scope: community-neutral; no adopter, CI provider, registry, host, service,
  credential, or client database is encoded.

## Verification

Focused Go tests cover Product Release mapping/handler/database behavior and
typed plan resolution projection. Angular tests cover complete draft/published
manifest rendering, frozen target resolution, and fail-closed error states.
Frontend build/typecheck, formatting, and diff checks complete the local gate;
live deployment proof remains a separate integration activity.
