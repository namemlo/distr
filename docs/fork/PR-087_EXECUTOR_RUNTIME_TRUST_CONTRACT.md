# PR-087 - Executor Runtime Trust Contract

## Scope

This change closes the generic executor-success trust gap without adding any
client, CI-provider, registry, or runtime-specific core behavior.

## Behavior

- Freezes a verified current-state precondition, target caller, adapter
  audience, and platform into every new protocol-v2 signed intent.
- Rejects bootstrap, missing, ambiguous, or non-authoritative baselines.
- Adds one immutable runtime-evidence record per execution attempt.
- Requires healthy, desired-state-matching evidence before `SUCCEEDED`.
- Keeps independent observation authoritative for desired-state promotion.
- Exposes retained runtime evidence in operator execution details.

## Database

Migration 167 retains historical attempts as `legacy-v2`, defaults new attempts
to strict `v3`, adds immutable trust-binding columns, and creates
`ExecutionRuntimeEvidence`. Exact attempt and intent lineage is enforced by
foreign keys. Append-only guards allow only the existing organization-retention
deletion boundary. Down migration locks and refuses while v3 contracts or
evidence remain. Pending legacy attempts are not eligible for new leases or
direct claims after upgrade; already in-flight legacy work can terminate only
without a v3 success assertion.

## API and Protocol

`POST /api/executor/v2/attempts/{attemptId}/runtime-evidence` accepts bounded
executor evidence under authenticated organization and deployment-target
scope. The response returns the retained row and server canonical checksum.
Successful completion supplies that row ID/checksum; other terminal outcomes do
not.

Signed intent schema advances to `distr.execution-intent/v3` while remaining on
the explicit executor protocol-v2 route and feature boundary.

## Verification

Focused Go tests cover intent signing/binding, authoritative baseline
derivation, API/handler scope, evidence validation/canonicalization, completion
gating, operator evidence visibility, and migration shape/refusal. Migration
matrix and release selectors cover the ordered 138-through-167 chain.

Live PostgreSQL, Docker, remote executor, staging, and production evidence are
not claimed by this PR and remain release/deployment gates.
