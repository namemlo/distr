# PR-088: Fresh Dependency Provider Evidence

## Generic user story

As an operator publishing an immutable target plan, I need every reused or
external dependency provider to prove current, trusted, non-expired state so
that a stale provider or an observation without real approval cannot authorize
deployment.

## Behavior

- `pinned_existing` and `shared_provider` require a trusted current native
  observation whose `freshUntil` covers the server-selected planning instant.
- `approved_external` requires that same observation contract plus the exact
  provider-plan approval request/checksum and separate contract-probe
  observation/checksum.
- Historical legacy observations remain readable but cannot authorize a new
  provider resolution because they have no trusted-current freshness fields.
- All evidence fields participate in the requirement binding and canonical
  target-plan checksums.

## Schema and API

Migration 168 adds provider evidence versioning, observation freshness and
currentness, provider approval identity/checksum, and contract-probe
identity/checksum to the append-only resolved-requirement row. Foreign keys
retain the approval request and native observation. API draft validation,
published-plan reads, and operator read models expose the frozen evidence.

## Compatibility and security

Existing evidence-version-1 rows are not rewritten. New plans publish
evidence version 2. Migration rollback refuses when version-2 evidence exists.
The change adds no provider-specific integration, secret material, arbitrary
command execution, or client/database access.

## Verification

Focused resolver tests cover trusted, current, freshness, approval, and
contract-probe blockers and deterministic checksums. Repository and migration
contract tests cover the server-side query predicates, exact approval binding,
foreign keys, append-only persistence, and downgrade refusal.
