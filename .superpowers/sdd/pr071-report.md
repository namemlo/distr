# PR-071 SDD Report

## Status

Implementation complete on the assigned synthetic stack; focused verification is recorded below. Sequential
migration lint remains blocked by intentionally absent predecessor migrations 141 through 148 and PR-070
migration 152.

## Initial HEAD

`43a57716e9a92655b561a0609509b11f6beb1015`

Branch: `codex/pr-071-campaigns`

## Changes

- Added campaign draft, revision, wave, member, prerequisite, candidate, risk, authorization, and publication
  context types.
- Added deterministic tag membership resolution, canonical JSON/SHA-256 generation, and draft validation.
- Added migration 153 with normalized immutable publication evidence, tenant-safe foreign keys, ordering and
  uniqueness constraints, retention-only deletion, and guarded downgrade.
- Added tenant-scoped draft create/get/optimistic edit/validate and idempotent repeatable-read publication.
- Added strict API request validation, response mapping, authenticated routes, feature-gated mutations, and a
  fail-closed PR-066 authorization seam.
- Added ADR-0063, PR-071 fork notes, and the fork diff-index entry.

## TDD Evidence

- Pure red: the focused `internal/campaigns` test build failed on missing `CampaignRevision`,
  `ResolveCampaignMembership`, `CanonicalizeCampaignRevision`, and `ValidateCampaignDraft`.
- Pure green: the same focused package passed after the minimal domain and algorithms were added.
- Persistence/API red: the focused package build failed on missing request, repository, mapping, authorizer, and
  migration contracts.
- Persistence/API green: all five focused packages passed after implementation.

## Verification

- Focused Go tests passed:
  `go test ./internal/campaigns ./internal/db ./api ./internal/mapping ./internal/handlers -run
  'CampaignRevision|Membership|Prerequisite' -count=1`.
- Focused `go vet` passed for campaign, database, API, mapping, handler, and routing packages.
- `mise run lint:migrations` could not start because the worktree `mise.toml` is not trusted. Running
  `hack/validate-migrations.sh` directly reached the validator and reported only the synthetic-stack predecessor
  gaps: 141 through 148 and 152; migration 153 has exactly one up and one down file.
- `git diff --check` passed.
- Self-review tightened current approval eligibility (expiry, invalidation, plan, effective-policy, and subscriber
  checksum binding) and made prerequisite checks placement-specific. No unresolved feature-local finding remains.

## Commit

Feature-local conventional commit: `feat: freeze deterministic deployment campaigns`

No push, merge, deployment, external mutation, or client mutation is authorized or performed.

## Dependency Seams

- PR-066: wire the shared scoped `campaign.control` authorization adapter. The feature-local production factory
  fails closed.
- Integration predecessors: transplant migrations 141 through 148 and PR-070 migration 152 before sequential
  migration lint/live migration application.
- Integrated planning stack: replace the synthetic target-component evidence adapter with the full frozen
  provider-placement and v2 target-plan source.
- PR-072: consume immutable rows for scheduling and persist the actual trusted observation identity/checksum;
  mismatch pauses and never rewrites PR-071 evidence.

## Blocking Review Follow-up

The blocking review findings were reproduced and fixed test-first:

- Published members now persist and expose the exact effective-policy checksum, approval request ID/revision and
  source subject checksum, ordered calendar version IDs/checksums, and admission evaluation ID/decision checksum.
  The campaign-owned synthetic approval digest was removed. All evidence is included in canonical revision bytes.
- Published updates remain rejected. Direct deletes with a forged marker are rejected because retention requires a
  campaign-specific UUID operation marker, nested trigger depth, and an already-deleted parent `Organization`.
  Truncate is rejected by statement-level triggers. The organization cleanup transaction now supplies the
  campaign-specific retention markers.
- Prerequisites use composite organization foreign keys to the revision, both member references, and both plan
  references. Exact requested `(upstream plan, step key, provider placement)` tuples are hydrated and validated;
  the former step-by-placement cross product and independent evidence maps were removed.
- Candidate SQL applies explicit ID/tag selection before `LIMIT 1001`; only the bounded selected set is hydrated,
  so an explicit one-plan campaign remains resolvable in an organization with more than 1,000 other eligible
  plans.
- Added focused regression coverage for exact frozen evidence/canonical materiality/API mapping, forged retention
  markers, truncate guards, tenant-composite prerequisite references, exact step-placement pairing, missing
  admission evidence, and membership-before-bound query shape.

Follow-up verification:

- `go test ./internal/campaigns ./internal/db ./api ./internal/mapping ./internal/handlers ./internal/routing -run
  'Campaign|campaign' -count=1` passed.
- `go vet ./internal/campaigns ./internal/db ./api ./internal/mapping ./internal/handlers ./internal/routing`
  passed.
- Direct `hack/validate-migrations.sh` again reached the validator and reported only the unchanged synthetic-stack
  gaps: migrations 141 through 148 and 152. Migration 153 remains paired.
- No live database, push, merge, rebase, deployment, or external mutation was performed.
