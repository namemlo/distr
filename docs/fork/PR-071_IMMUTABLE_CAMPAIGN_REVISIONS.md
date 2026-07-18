# PR-071 - Immutable Campaign Revisions

## Generic User Story

As a fleet operator, I want editable deployment campaign drafts to publish immutable, checksum-bound membership and
wave revisions so that review, restart, and later scheduling always use the same approved plans and
shared-provider expectations.

## Contract

- Routes: create, get, edit, validate, and publish below `/api/v1/deployment-campaign-drafts`.
- Access: authenticated vendor organization context is required. Mutations require
  `operator_control_plane_v2` and the campaign-control authorization seam; the seam fails closed until PR-066 is
  integrated.
- Draft concurrency: edits supply `expectedRevision`; a stale revision returns conflict.
- Membership: explicit plan IDs and a conjunction of canonical `key=value` tag terms are resolved into at most
  1,000 eligible organization plans.
- Publication: a repeatable-read transaction uses an idempotency key, applies explicit/tag membership before its
  1,000-result bound, revalidates current plans, approvals, and admissions, freezes normalized child rows, and
  returns the same revision for an idempotent replay.
- Canonical evidence: ordered members, deployment units, plan, effective-policy, exact approval, calendar, and
  admission evidence, tag query, waves, bake, thresholds, risk/concurrency policy, and shared-provider
  prerequisites are SHA-256 bound.
- Prerequisites: downstream plan, upstream plan, step key, provider placement, and expected observed-state checksum
  are frozen. Publication resolves the plan-local placement through the immutable target-config snapshot and also
  freezes the provider deployment unit and canonical component instance used by trusted-observation replay. A
  future observation ID is intentionally absent.

## Impact

Migration 153 adds `DeploymentCampaignDraft`, `DeploymentCampaignRevision`, `DeploymentCampaignWave`,
`DeploymentCampaignMember`, and `DeploymentCampaignPrerequisite`. Published tables reject updates, direct deletes,
and truncates. A published delete is accepted only for the campaign-specific, operation-bound `Organization`
retention cascade. Composite organization foreign keys bind each prerequisite to its revision, member plans, and
plan rows. Unique constraints prevent a plan or deployment unit from appearing twice and preserve deterministic
wave/member order.

The API and route are additive and default-off for writes. There is no UI, scheduler, task creation, campaign run,
pause/resume, executor protocol, observer write, client database, or deployment mutation in this slice. Existing
v1 deployment behavior and historical checksums are unchanged.

## Verification

Test-first coverage includes stable member resolution/order/checksum, checksum materiality, tag changes after
publication, missing explicit plans, unapproved plans, plan-checksum mismatch, duplicate deployment unit,
valid shared-provider prerequisites, expected-observation mismatch, invalid/decreasing bake, API bounds, immutable
migration structure, direct retention-marker forgery, exact step-placement pairs, pre-bound membership queries,
mapping fidelity, and draft edits without scoped authority.
Cross-PR coverage also proves canonical provider coordinates are persisted, canonicalized, mapped to the API, and
rejected when the plan-local placement has no immutable snapshot bridge.

Focused Go tests and `go vet` are the feature-local gates. Migrations 141 through 148 and migration 152 are absent
from the assigned synthetic stack, so migration lint reports those exact predecessor gaps until the missing
planning, authorization, and PR-070 stacks are transplanted. Live sequential PostgreSQL and full-branch regression
remain integration gates.

## Dependency Seams

1. PR-066 must replace `newCampaignActionAuthorizer` with the shared scoped authorization adapter. The current
   production factory denies every mutation.
2. The integration branch must supply migrations 141 through 148 and PR-070 migration 152 before migration lint or
   sequential database application can pass.
3. The full target-plan/provider-placement stack must replace the narrow target-component evidence adapter when
   campaign code is transplanted onto the integrated planning branch.
4. PR-072 consumes immutable revisions, records the actual matching trusted observation ID, and implements
   threshold/bake scheduling. It must not mutate PR-071 rows.
