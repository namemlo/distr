# PR-056 Final Quality Pass Implementation Plan

> **For Codex:** Execute test-first and keep all work in the existing PR-056 commit.

**Goal:** Close the remaining retention, rename-history, placement-read, checksum-ordering, and write-race gaps in the canonical deployment registry.

**Architecture:** Migration 139 remains the single schema boundary. Organization retention authorizes narrowly scoped cascade deletes with a transaction-local marker and deferred tenant-graph constraints. Component rename evidence is private, append-only, and protected by row locks plus database constraints. Placement lists assemble all aggregates in one repeatable-read snapshot with a fixed seven-query batch plan.

**Tech Stack:** Go, pgx v5, PostgreSQL 16/18-compatible SQL, Gomega, chi-openapi.

---

### Task 1: Encode the failing contracts

**Files:**

- Modify: `internal/db/deployment_registry_test.go`
- Modify: `internal/db/organization_cleanup_test.go`
- Modify: `internal/deploymentregistry/validation_test.go`
- Modify: `internal/handlers/deployment_registry_test.go`

1. Add live migration/retention coverage with a sealed shared unit.
2. Add sequential and concurrent rename-history protection coverage.
3. Add placement runtime query-count and repeatable-read list coverage.
4. Add checksum native-UUID ordering and zero-row/concurrent write coverage.
5. Add exact OpenAPI conflict-response expectations.
6. Run the focused PostgreSQL suite and record the expected failures.

### Task 2: Implement schema and repository invariants

**Files:**

- Modify: `internal/migrations/sql/139_deployment_registry.up.sql`
- Modify: `internal/migrations/sql/139_deployment_registry.down.sql`
- Modify: `internal/db/organization.go`
- Modify: `internal/db/deployment_registry.go`
- Modify: `internal/types/deployment_registry.go`

1. Make registry composite foreign keys deferred `NO ACTION` constraints.
2. Add the retention marker bypasses and private rename evidence table/guards.
3. Lock rename participants and persist evidence atomically.
4. Return stable conflicts for protected alias/instance history.
5. Map write-result `pgx.ErrNoRows` to not found.

### Task 3: Replace placement N+1 and locale-sensitive ordering

**Files:**

- Modify: `internal/db/deployment_registry.go`
- Modify: `internal/deploymentregistry/validation.go`
- Modify: `internal/migrations/sql/139_deployment_registry.up.sql`

1. Batch-load placement roots and six relation sets inside one repeatable-read transaction.
2. Group rows deterministically without changing response order.
3. Sort UUID bytes in Go and native UUID values in PostgreSQL.

### Task 4: Update public contract notes and verify

**Files:**

- Modify: `internal/handlers/deployment_registry.go`
- Modify: `docs/adr/0056-canonical-deployment-registry-identity.md`
- Modify: `docs/fork/FORK_DIFF_INDEX.md`
- Modify: `docs/superpowers/plans/2026-07-14-control-plane-foundations.md`

1. Add the three reachable history-conflict responses to OpenAPI.
2. Document private rename evidence, retention behavior, and fixed-query placement reads.
3. Run the exact PostgreSQL suite, focused packages, migration validator, lint, diff checks, and serial full Go suite if disk permits.
4. Amend the existing PR-056 commit without pushing.
