# PR-071 - Immutable Campaign Revisions

## Generic User Story

As a fleet operator, I want editable deployment campaign drafts to publish immutable, checksum-bound membership and
wave revisions so that review, restart, and later scheduling always use the same approved plans and
shared-provider expectations.

## Contract

- Routes: create, get, edit, validate, and publish below `/api/v1/deployment-campaign-drafts`.
- Access: authenticated vendor organization context is required. Mutations require
  `operator_control_plane_v2` and PR-066 `campaign.control` authorization. Creation uses organization scope before
  the draft exists; edit, validate, and publish resolve the exact tenant-owned campaign scope.
- Draft concurrency: edits supply `expectedRevision`; a stale revision returns conflict.
- Membership: explicit plan IDs and a conjunction of canonical `key=value` tag terms are resolved into at most
  1,000 eligible organization plans.
- Publication: a repeatable-read transaction uses an idempotency key, applies explicit/tag membership before its
  1,000-result bound, revalidates current plans, approvals, and admissions, freezes normalized child rows, and
  returns the same revision for an idempotent replay, including concurrent unique/serialization races.
- Canonical evidence: ordered members, deployment units, plan, effective-policy, exact approval, calendar, and
  admission evidence, tag query, waves, bake, thresholds, risk/concurrency policy, and shared-provider
  prerequisites are SHA-256 bound.
- Prerequisites: downstream plan, upstream plan, step key, provider placement, and expected runtime-state checksum
  are frozen. Publication resolves the plan-local placement through the immutable target-config snapshot and also
  freezes the provider deployment unit and canonical component instance used by trusted-observation replay. A
  future observation ID is intentionally absent. `distr.campaign-runtime-expectation/v1` hashes only stable desired
  runtime fields: provider unit, component instance/key, pinned artifact digest, config checksum, and platform.

## Impact

Migration 153 adds `DeploymentCampaignDraft`, `DeploymentCampaignRevision`, `DeploymentCampaignWave`,
`DeploymentCampaignMember`, and `DeploymentCampaignPrerequisite`. Published tables reject updates, direct deletes,
and truncates. A published delete is accepted only for the campaign-specific, operation-bound `Organization`
retention cascade. Composite organization foreign keys bind each prerequisite to its revision, member plans, and
plan rows, and bind each member's plan/unit, approval/plan, and admission/plan identities. Step and plan-local
placement foreign keys both target the upstream plan. Unique constraints prevent a plan or deployment unit from
appearing twice and preserve deterministic wave/member order. The database recomputes the canonical checksum from
the canonical payload and caps both wave and campaign concurrency at 1,000.

The API and route are additive and default-off for writes. There is no UI, scheduler, task creation, campaign run,
pause/resume, executor protocol, observer write, client database, or deployment mutation in this slice. Existing
v1 deployment behavior and historical checksums are unchanged.

Migration 153 activates the campaign scope reserved by PR-066, PR-067, and PR-069. Authorization resource
resolution, role-binding creation, deployment-policy owner bindings, and deployment-freeze scopes all require an
existing deployment campaign draft with the same `organization_id`; there is no organization-only or
unknown-resource fallback. Migration 153 also upgrades the deployment-policy binding trigger and restores its
pre-campaign behavior on downgrade.

## Verification

Test-first coverage includes stable member resolution/order/checksum, checksum materiality, tag changes after
publication, missing explicit plans, unapproved plans, plan-checksum mismatch, duplicate deployment unit,
valid shared-provider prerequisites, expected-observation mismatch, invalid/decreasing bake, API bounds, immutable
migration structure, direct retention-marker forgery, exact step-placement pairs, pre-bound membership queries,
mapping fidelity, and draft edits without scoped authority.
Cross-PR coverage also proves canonical provider coordinates are persisted, canonicalized, mapped to the API, and
rejected when the plan-local placement has no immutable snapshot bridge.

Focused Go tests and `go vet` are the feature-local gates. Migrations 141 through 153 are present on the integrated
governance stack. Live sequential PostgreSQL and full-branch regression remain integration gates.

## Dependency Seams

1. PR-066 is wired through `newCampaignActionAuthorizer`; it resolves exact campaign and organization scopes and
   delegates to the shared credential-capped authorizer.
2. The integrated branch supplies migrations 141 through 153 in sequence; migration lint and live sequential
   database application remain required gates.
3. The full target-plan/provider-placement stack must replace the narrow target-component evidence adapter when
   campaign code is transplanted onto the integrated planning branch.
4. PR-072 consumes immutable revisions, records the actual matching trusted observation ID, and implements
   threshold/bake scheduling. It must not mutate PR-071 rows.
