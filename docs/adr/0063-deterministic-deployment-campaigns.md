# ADR-0063: Deterministic Deployment Campaigns

## Status

Accepted

## Context

A deployment campaign groups independently approved deployment plans into ordered waves. Selecting plans by mutable
tags, retaining only live plan references, or resolving shared-provider dependencies at scheduling time would let
the executed campaign differ from the reviewed campaign. Restarts also need to reproduce the same order and
checksum without depending on query order, process time, or generated child IDs.

## Decision

Migration 153 adds a mutable organization-scoped `DeploymentCampaignDraft` and immutable
`DeploymentCampaignRevision`, `DeploymentCampaignWave`, `DeploymentCampaignMember`, and
`DeploymentCampaignPrerequisite` records.

Publication resolves explicit plan IDs and the draft tag expression into one bounded candidate set. Members are
ordered by wave, deployment-unit UUID, and plan UUID. Each member freezes its deployment unit, deployment plan
checksum, approval request, and derived approval-evidence checksum. A deployment unit can occur only once in a
revision. Every selected plan must retain a current approved request whose subject checksum still matches the plan.
Changing tags after publication does not change the stored member rows.

Each wave freezes order, bake duration, and maximum concurrency. The campaign freezes maximum concurrency, failure
tolerance, and minimum healthy thresholds. Bake durations cannot decrease as exposure broadens. Scheduling,
threshold evaluation, pause/resume state, and operational controls remain PR-072 and PR-073.

A cross-plan prerequisite freezes the downstream plan, upstream plan, upstream step key, provider placement, and
expected observed-state checksum. It deliberately does not store a future observation ID. PR-072 admission must
record the actual trusted observation used and compare its measured checksum to the frozen expectation. A mismatch
pauses admission; it never rewrites or rebinds this revision.

Canonical JSON excludes generated revision/child IDs, publication time, and stored canonical fields. It includes
the organization, draft identity, revision numbers, ordered members, plan and approval checksums, waves, risk
policy, tag-query evidence, and prerequisites. Arrays are sorted by explicit semantic keys before SHA-256 hashing,
so recreating identical frozen inputs after restart produces identical bytes and checksum.

Draft writes use optimistic revision checks. Publication uses a caller idempotency key and a repeatable-read
transaction that locks the tenant-scoped draft, re-resolves live plan and approval evidence, validates it, writes
the immutable parent and children in batches, and updates the root's last-published pointer. Published rows reject
ordinary updates and deletes. The existing authorized organization-retention marker permits cascade deletion, and
downgrade refuses while campaign rows exist.

The API is additive below `/api/v1/deployment-campaign-drafts`. Mutations require
`operator_control_plane_v2` and a scoped campaign-action authorization seam. This synthetic stack does not contain
PR-066, so the production authorizer fails closed until that adapter is transplanted. Reads remain tenant-scoped.

## Consequences

Approval and execution can refer to one stable campaign checksum. Tag edits, plan edits, approval invalidation,
wave changes, threshold changes, and prerequisite changes require a new draft revision and publication. Existing
v1 plan and task behavior is unchanged.

Migration 153 must be applied after the complete predecessor sequence. The assigned synthetic branch does not
contain migrations 141 through 148 or migration 152, so sequential migration lint and live PostgreSQL application
remain integration gates. The full v2
provider-placement model from the planning stack must replace the narrow deployment-plan target-component evidence
adapter when the dependency stacks are combined.

## Alternatives Considered

- Resolve tags at every scheduler tick. Rejected because mutable membership would escape approval.
- Store only plan IDs. Rejected because plan, approval, and policy evidence could drift.
- Store a future observation ID. Rejected because no trusted observation exists at publication time.
- Hash database rows including generated IDs and timestamps. Rejected because recreation after restart would not be
  deterministic.
- Silently update an expectation after mismatch. Rejected because this would rebind an approved campaign.
