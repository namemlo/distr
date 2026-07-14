# Upstream Contribution Breakdown

Use this breakdown to propose upstream-sized slices instead of one large fork dump.

## Contribution Order

| Slice                              | Depends on              | Scope                                                                          | Compatibility notes                       |
| ---------------------------------- | ----------------------- | ------------------------------------------------------------------------------ | ----------------------------------------- |
| Fork records and feature flags     | None                    | ADR pattern, roadmap records, experimental flags                               | No runtime behavior change.               |
| Environments, lifecycles, channels | Feature flags           | Generic promotion metadata and rule validation                                 | Existing deployments unchanged.           |
| Release bundles and CI API         | Channels                | Immutable release records, checksums, publication, CLI examples                | Existing direct deployment remains valid. |
| Deployment processes and variables | Release bundles         | Process revisions, scoped variables, snapshots, drift                          | No execution change until planning.       |
| Planning and durable tasks         | Processes and variables | Plan checksums, locks, task queue, leases, events                              | Agents require capability-aware rollout.  |
| Built-in safe actions              | Task engine             | Compose adapter, OCI job, file render, HTTP check, wait, webhook               | Typed actions only; no script console.    |
| Governance and rollout             | Task engine             | Approvals, tags, waves, guided failure, freezes, subscriptions                 | Requires RBAC review.                     |
| Reusable operations                | Governance              | Step templates, output variables, runbooks, schedules                          | Keep template sources signed or built in. |
| Progressive delivery               | Rollout                 | Rolling, traffic provider, blue-green, timeline, retention                     | Provider-specific adapters stay optional. |
| Operational maturity               | Prior slices            | Observability, Config as Code validation, compatibility backfill, release docs | Config sync/apply remains future work.    |

## Upstream Review Notes

- Keep every slice community-neutral.
- Include tests and docs with each slice.
- Avoid changing existing public API behavior unless the slice documents a compatibility path.
- Submit schema changes with down migrations and upgrade notes.
- Include security notes for new auth, agent, secret, filesystem, network, or execution boundaries.
- Keep adopter-specific examples outside core docs.

## Fork-Only Divergences

The fork may temporarily carry:

- experimental feature flags until upstream APIs stabilize;
- compatibility metadata for deployments created before advanced release features;
- neutral demo fixtures that prove the combined roadmap flow;
- release-readiness scripts that upstream can adopt or replace.

## Licensing

All contributed code and docs must remain Apache-2.0 compatible. Do not copy proprietary Octopus text, UI,
assets, or implementation details.
