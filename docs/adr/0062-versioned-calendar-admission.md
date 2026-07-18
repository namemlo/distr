# ADR-0062: Versioned Calendar Admission

## Status

Accepted

## Context

Deployment admission needs recurring positive maintenance windows and exact negative freeze intervals. A wall-clock
time or a timezone abbreviation is not enough evidence: daylight-saving gaps, repeated hours, and timezone-rule
updates can otherwise change a later interpretation. Operators also need to edit future policy without mutating the
rule set that a published plan or later admission record references.

## Decision

Migration 151 adds mutable organization-scoped `MaintenanceCalendar` and `DeploymentFreeze` roots. Every draft uses
an optimistic `draft_revision`. Publication copies the draft into an immutable `MaintenanceCalendarVersion` or
`DeploymentFreezeRevision`, stores a canonical payload and SHA-256 checksum, and binds publication to the source
draft revision. Repeating publication for the same source revision returns the existing immutable result.
`MaintenanceWindowRule` rows are immutable children of a calendar version. A draft rule UUID remains its logical
identity, while each published version derives a distinct immutable child-row UUID.

A calendar version uses unique rule names, sorted weekdays, and local start-inclusive, end-exclusive minute
intervals. An end minute less than the start minute is an overnight interval and its after-midnight portion belongs
to the preceding configured weekday. A freeze uses start-inclusive, end-exclusive UTC instants. Overlapping freezes
are ordered by descending priority and then immutable revision UUID, so every evaluator selects the same blocker.

Evaluation begins with one UTC instant and converts it through the configured IANA zone using an injected
timezone-rule provider. Production pins an embedded IANA 2026a dataset whose exact module checksum identity is
reported by the provider; it never consults host `ZONEINFO`, and the process-dependent `Local` zone is forbidden.
Evidence includes the UTC instant, resulting local time, exact UTC offset, IANA zone, caller-pinned timezone rule
version, immutable
calendar/freeze identity, reason code, and a deterministic evaluation identity. A daylight-saving gap therefore
cannot invent a nonexistent local time. The two occurrences of a repeated local hour retain different UTC
instants, offsets, and identities. A zone or rule-version binding mismatch fails closed.

The new APIs use opaque keyset pagination and organization predicates on every read and write. They are completely
hidden unless `operator_control_plane_v2` is enabled, and currently require vendor organization admin access while
blocking super-admin mutation-by-impersonation. Mutations call the `calendar.manage` or `freeze.manage` scoped
authorization seam without changing repository tenant predicates. Until PR-066 supplies its shared
`authorization.Authorize` adapter, the production seam fails closed; test injection cannot become a permissive
production fallback. Freeze updates
authorize both the exact expected current scope and any destination scope. Freeze publication authorizes the
locked revision scope inside the publication transaction.

Freeze scopes support organization, customer, environment, deployment unit, and component-definition identities.
Campaign scope is part of the forward-compatible enum but cannot be written until immutable campaign revisions
exist. Emergency override, admission-record persistence, planner visibility, and in-flight execution behavior are
separate later slices.

Published rows reject ordinary update or delete. The existing transaction-local organization-retention marker
permits cascaded deletion during the authorized organization purge path. Downgrade refuses while any calendar or
freeze row exists.

## Consequences

Draft editing never changes published policy. A scheduler can reproduce ordinary, overnight, DST-gap, repeated-hour,
and overlapping-freeze decisions from explicit evidence, and later admission records can pin immutable IDs and
checksums. Existing v1 deployment and agent behavior is unchanged because this slice only exposes feature-flagged
governance APIs and pure evaluation.

The application must submit the provider's IANA rule version and verify its exact reported rule-data identity as
part of build/deployment evidence. Migration 151 is speculative until migrations 141 through 150 are integrated
and must be rebased in sequence before live application.

## Alternatives Considered

- Store only local timestamps. Rejected because gaps and repeated hours are ambiguous.
- Store only UTC. Rejected because recurring business windows are defined in local civil time and need a pinned
  zone/rule interpretation.
- Mutate published rules in place. Rejected because approvals and admission evidence must retain their exact input.
- Let database time determine the active rule. Rejected because retries and audits need the caller-supplied instant.
- Resolve equal-priority overlaps by query order. Rejected because query order is not a deterministic policy.
