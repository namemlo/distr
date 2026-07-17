# External Execution Timestamp Expand Release Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the independently deployable external-execution timestamp expand release: migration 138, complete raw-cell manifest/provenance, evidence-gated backfill, UTC-safe dual writes, fail-closed startup and migration checks, and a fenced two-phase Compose rollout.

**Architecture:** Keep the current `TIMESTAMP WITHOUT TIME ZONE` columns canonical for this release. Add nullable `TIMESTAMPTZ` shadows and append-only evidence, capture one complete schema-137 snapshot before migration, populate only evidence-resolved shadows, and dual-write all future mutations from one authoritative instant. The Hub starts only on the expand schema when historical ambiguity is either represented by a verified manifest or the database is provably zero-history.

**Tech Stack:** Go 1.26.5, pgx/v5, PostgreSQL 16.14 and 18.4, golang-migrate v4.19.1, Cobra, Gomega, Bash, Docker Compose, GitHub Actions.

## Global Constraints

- This is the logical `PR-054A` prerequisite. It does not consume planned PR-055 and preserves all historical PR numbering.
- Read `docs/adr/0055-external-execution-timestamp-instants.md` and `docs/superpowers/specs/2026-07-15-external-execution-timestamptz-hybrid-design.md` before every task.
- Only the Distr repository, Distr control-plane PostgreSQL schema, and Distr-owned deployment metadata are in scope. Do not access or change adopter application/service repositories, workload runtimes, or any client application/business database.
- Do not expose the six shadow fields through `internal/types/external_execution.go`, API types, mapping, or JSON before the contract release.
- Do not infer an offset from a naive timestamp. A populated historical cell remains `UNRESOLVED` unless direct evidence makes it `PROVEN` or a named human supplies an `ATTESTED` offset.
- `verify` is read-only. `apply` performs its own verification inside the write transaction and atomically advances `APPROVED -> APPLIED -> VERIFIED`.
- The expand and contract changes never ship in the same release. This plan does not implement `prepare-contract`, a physical contract migration, canonical instant reads, column/index swaps, `ANALYZE`, dirty-version `Force`, or post-contract rollback.
- No generic `--force`, vulnerability allowlist, vulnerability suppression, or scanner downgrade is permitted.
- The dated five-execution fixture has 30 populated cells, 18 `PROVEN`, and 12 `UNRESOLVED`; those numbers are a test fixture and prior evidence baseline, never a live-deployment acceptance shortcut.
- Output and logs must not contain DSNs, credentials, tokens, payloads, messages, customer data, or private host paths. Manifest/evidence references are opaque identifiers.
- All PostgreSQL integration tests use `DISTR_TEST_DATABASE_URL`, isolated UUID-named schemas, and serialized `go test -p=1` execution.
- Each task starts with failing tests, ends green, passes `git diff --check`, and creates the stated focused commit.

## Fixed External Timestamp Contract

The complete cell allowlist and ordinal mapping is immutable in this release:

| Ordinal | Table                    | Column                 | Shadow                         |
| ------: | ------------------------ | ---------------------- | ------------------------------ |
|       1 | `externalexecution`      | `created_at`           | `created_at_instant`           |
|       2 | `externalexecution`      | `updated_at`           | `updated_at_instant`           |
|       3 | `externalexecution`      | `started_at`           | `started_at_instant`           |
|       4 | `externalexecution`      | `completed_at`         | `completed_at_instant`         |
|       5 | `externalexecution`      | `callback_deadline_at` | `callback_deadline_at_instant` |
|       6 | `externalexecutionevent` | `created_at`           | `created_at_instant`           |

Canonical raw wall values use `2006-01-02T15:04:05.000000`. Canonical instants use UTC RFC3339 with six fractional digits. Offset sign follows ISO-8601: `local wall = UTC + offset`, so conversion is `instant = wall - offset`.

Every SHA-256 input is framed by concatenating, for each field, a four-byte unsigned big-endian length followed by the UTF-8 bytes. Null uses the literal framed token `NULL`; non-null uses `VALUE` followed by the framed fixed-format value. Checksums use the lowercase `sha256:` prefix followed by exactly 64 lowercase hexadecimal characters.

The encoding is exact. Integers are unsigned or signed base-10 without leading zeroes; UUIDs, table names, and column names are lowercase; list counts precede list members; IDs and cells are sorted before framing. An empty optional string is encoded as `NULL`; a present string, including a required empty string under a negative test, is encoded as `VALUE` followed by its bytes. `H(domain, fields...)` means SHA-256 over framed `domain` followed by the framed fields below:

| Checksum                  | Domain                                                    | Ordered fields                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| ------------------------- | --------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| raw cell                  | `distr.external-execution-timestamp/raw-cell/v1`          | source table, lowercase row UUID, source column, decimal ordinal, then `NULL` or `VALUE` plus fixed raw wall value                                                                                                                                                                                                                                                                                                                                           |
| raw set                   | `distr.external-execution-timestamp/raw-set/v1`           | decimal cell count, then each raw-cell checksum sorted by table, row UUID, ordinal                                                                                                                                                                                                                                                                                                                                                                           |
| database identity         | `distr.external-execution-timestamp/database-identity/v1` | decimal source schema version, decimal execution count, sorted execution UUIDs, decimal event count, sorted event UUIDs, decimal raw-cell count, raw-set checksum                                                                                                                                                                                                                                                                                            |
| cell decision             | `distr.external-execution-timestamp/cell-decision/v1`     | raw-cell checksum, decision, nullable source zone, nullable decimal offset seconds, nullable canonical UTC converted instant, nullable evidence reference, nullable evidence checksum, nullable approving identity, conversion-expression version                                                                                                                                                                                                            |
| manifest decision content | `distr.external-execution-timestamp/manifest-decision/v1` | manifest UUID, nullable superseded-manifest UUID, database-identity checksum, decimal source schema version, snapshot start/end instants, four decimal counts (execution, event, raw, populated), raw-set checksum, nullable evidence-bundle reference/checksum, tool version, conversion-expression version, nullable author/reviewer, nullable target commit/image digest, decimal decision count, then each cell-decision checksum in raw-cell sort order |

The manifest decision checksum excludes `state`, `approvedAt`, and database lifecycle timestamps only. A provenance row copies its raw-cell checksum and the parent manifest decision checksum verbatim. An external evidence-file checksum is ordinary SHA-256 over the exact file bytes; it is not re-framed.

---

## Task 1: Reconcile the Roadmap and Allocate PR-054A

**Files:**

- Create: `docs/fork/PR-054A_EXTERNAL_EXECUTION_TIMESTAMP_EXPAND.md`
- Create: `hack/pr054a-validate-timestamp-expand.mjs`
- Create: `hack/pr054a-validate-timestamp-expand.test.mjs`
- Modify: `docs/fork/FORK_DIFF_INDEX.md`
- Modify: `docs/roadmaps/DISTR_COMMUNITY_FORK_MASTER_PLAN.md`
- Modify: `docs/superpowers/plans/2026-07-14-enterprise-operator-control-plane-program.md`
- Modify: `docs/superpowers/plans/2026-07-14-control-plane-foundations.md`
- Modify: `docs/superpowers/plans/2026-07-14-control-plane-governance-execution.md`
- Modify: `docs/superpowers/plans/2026-07-14-control-plane-operator-adoption.md`

**Interfaces:**

- Consumes: accepted ADR-0055, the approved hybrid design, and the existing PR-055 through PR-082 extension plans.
- Produces: collision-free migration/ADR allocations and a PR-054A evidence index used by every later task.

Apply this deterministic allocation shift without changing PR identifiers:

| Existing slice        |   Old migration |   New migration |           Old ADR |           New ADR |
| --------------------- | --------------: | --------------: | ----------------: | ----------------: |
| PR-056                |             138 |             139 |              0055 |              0056 |
| PR-057                |             139 |             140 |                 - |                 - |
| PR-058                |             140 |             141 |              0056 |              0057 |
| PR-059                |             141 |             142 |                 - |                 - |
| PR-060                |             142 |             143 |              0057 |              0058 |
| PR-062                |             143 |             144 |              0058 |              0059 |
| PR-063                |             144 |             145 |              0059 |              0060 |
| PR-064                |             145 |             146 |                 - |                 - |
| PR-065                |             146 |             147 |                 - |                 - |
| PR-066 through PR-078 | 147 through 159 | 148 through 160 | 0060 through 0065 | 0061 through 0066 |
| PR-079                |             160 |             161 |              0066 |              0067 |
| PR-082                |             161 |             162 |              0067 |              0068 |

- [ ] **Step 1: Write the failing allocation validator.** Create `hack/pr054a-validate-timestamp-expand.mjs` with this parser; it inspects each PR task block instead of relying on loose global string matches:

```js
#!/usr/bin/env node
import {existsSync, readFileSync, statSync} from 'node:fs';
import {fileURLToPath} from 'node:url';
import path from 'node:path';

const root = fileURLToPath(new URL('..', import.meta.url));
const read = (name) => readFileSync(path.join(root, name), 'utf8');
const fail = (message) => {
  throw new Error(message);
};
const plans = [
  'docs/superpowers/plans/2026-07-14-control-plane-foundations.md',
  'docs/superpowers/plans/2026-07-14-control-plane-governance-execution.md',
  'docs/superpowers/plans/2026-07-14-control-plane-operator-adoption.md',
];
const expectedMigrations = new Map([
  [56, 139],
  [57, 140],
  [58, 141],
  [59, 142],
  [60, 143],
  [62, 144],
  [63, 145],
  [64, 146],
  [65, 147],
  [66, 148],
  [67, 149],
  [68, 150],
  [69, 151],
  [70, 152],
  [71, 153],
  [72, 154],
  [73, 155],
  [74, 156],
  [75, 157],
  [76, 158],
  [77, 159],
  [78, 160],
  [79, 161],
  [82, 162],
]);
const expectedADRs = new Map([
  [56, '0056'],
  [58, '0057'],
  [60, '0058'],
  [62, '0059'],
  [63, '0060'],
  [66, '0061'],
  [69, '0062'],
  [71, '0063'],
  [75, '0064'],
  [77, '0065'],
  [78, '0066'],
  [79, '0067'],
  [82, '0068'],
]);

const expectedPRs = Array.from({length: 29}, (_, index) => 55 + index);
const sameValues = (actual, expected) =>
  actual.length === expected.length && actual.every((value, index) => value === expected[index]);
const describe = (values) => (values.length === 0 ? 'none' : values.join(','));
const sectionLines = (text, heading, isEnd) => {
  const lines = text.split(/\r?\n/);
  const starts = lines.flatMap((line, index) => (line === heading ? [index] : []));
  if (starts.length !== 1) return null;
  const start = starts[0];
  const endOffset = lines.slice(start + 1).findIndex(isEnd);
  const end = endOffset === -1 ? lines.length : start + 1 + endOffset;
  return lines.slice(start, end);
};
const filesSectionLines = (block, file, pr) => {
  const lines = block.split(/\r?\n/);
  const starts = lines.flatMap((line, index) => (line === '**Files:**' ? [index] : []));
  if (starts.length !== 1) fail(`${file}: PR-${pr} must contain exactly one **Files:** section`);
  const afterHeading = starts[0] + 1;
  const endOffset = lines.slice(afterHeading).findIndex((line) => /^\*\*[^*]+\*\*$/.test(line));
  const end = endOffset === -1 ? lines.length : afterHeading + endOffset;
  const section = lines.slice(afterHeading, end);
  if (!section.some((line) => /^- (Create|Modify):/.test(line))) {
    fail(`${file}: PR-${pr} has an empty **Files:** section`);
  }
  return section;
};
const allocationDeclarations = (lines, file, pr) => {
  const migrations = [];
  const adrs = [];
  for (const line of lines) {
    const candidate = line.trimStart();
    if (!candidate.startsWith('- ')) continue;
    if (candidate !== line && (candidate.includes('internal/migrations/sql/') || /docs\/adr\/\d/.test(candidate))) {
      fail(`${file}: PR-${pr} malformed allocation declaration: ${line}`);
    }
    const declaration = candidate.match(/^- (Create|Modify): `([^`]+)`$/);
    if (!declaration) {
      if (line.includes('internal/migrations/sql/') || /docs\/adr\/\d/.test(line)) {
        fail(`${file}: PR-${pr} malformed allocation declaration: ${line}`);
      }
      continue;
    }
    const [, action, declaredPath] = declaration;
    if (declaredPath.startsWith('internal/migrations/sql/')) {
      const migration = declaredPath.match(/^internal\/migrations\/sql\/(\d+)_(.+)\.(up|down)\.sql$/);
      if (!migration) fail(`${file}: PR-${pr} malformed migration declaration: ${line}`);
      migrations.push({
        action,
        id: migration[1],
        stem: migration[2],
        direction: migration[3],
      });
      continue;
    }
    if (/^docs\/adr\/\d/.test(declaredPath)) {
      const adr = declaredPath.match(/^docs\/adr\/(\d+)-[^/]+\.md$/);
      if (!adr) fail(`${file}: PR-${pr} malformed ADR declaration: ${line}`);
      adrs.push({action, id: adr[1]});
    }
  }
  return {migrations, adrs};
};
const describeMigrations = (declarations) =>
  declarations.length === 0
    ? 'none'
    : declarations
        .map(({id, action, direction}) => `${id}:${action}:${direction}`)
        .sort((left, right) => left.localeCompare(right))
        .join(',');

const seenPRs = new Map();

for (const file of plans) {
  const text = read(file);
  const blocks = text.split(/^## Task /m).slice(1);
  for (const block of blocks) {
    const heading = block.match(/^(\d+): PR-(\d{3})(?= — )/);
    if (!heading) continue;
    const pr = Number(heading[2]);
    if (!expectedPRs.includes(pr)) continue;
    const location = `${file}: Task ${heading[1]}`;
    if (seenPRs.has(pr)) fail(`duplicate PR-${pr} allocation block: ${seenPRs.get(pr)} and ${location}`);
    seenPRs.set(pr, location);

    const {migrations, adrs} = allocationDeclarations(filesSectionLines(block, file, pr), file, pr);
    const expectedMigration = expectedMigrations.get(pr);
    if (expectedMigration === undefined) {
      if (migrations.length !== 0) {
        fail(
          `${file}: PR-${pr} migration declarations mismatch: expected none, found ${describeMigrations(migrations)}`
        );
      }
    } else {
      const directions = migrations.map(({direction}) => direction).sort();
      const paired =
        migrations.length === 2 &&
        migrations.every(({id}) => id === String(expectedMigration)) &&
        sameValues(directions, ['down', 'up']) &&
        migrations.every(({action}) => action === 'Create') &&
        migrations[0].stem === migrations[1].stem;
      if (!paired) {
        fail(
          `${file}: PR-${pr} migration declarations mismatch: expected paired ${expectedMigration} up/down, found ${describeMigrations(migrations)}`
        );
      }
    }

    const declaredADRs = adrs.map(({action, id}) => `${id}:${action}`).sort((left, right) => left.localeCompare(right));
    const expectedADR = expectedADRs.has(pr) ? [`${expectedADRs.get(pr)}:Create`] : [];
    if (!sameValues(declaredADRs, expectedADR)) {
      fail(
        `${file}: PR-${pr} ADR declarations mismatch: expected ${describe(expectedADR)}, found ${describe(declaredADRs)}`
      );
    }
  }
}

const missingPRs = expectedPRs.filter((pr) => !seenPRs.has(pr));
if (missingPRs.length > 0) {
  fail(`missing expected PR allocation blocks: ${missingPRs.map((pr) => `PR-${pr}`).join(', ')}`);
}

const forkDoc = 'docs/fork/PR-054A_EXTERNAL_EXECUTION_TIMESTAMP_EXPAND.md';
if (!existsSync(path.join(root, forkDoc))) fail(`missing ${forkDoc}`);
const decisionLines = [
  '- Physical expand migration: migration 138.',
  '- Architecture decision: ADR-0055.',
  '- Durable zero-history proof: `ExternalExecutionTimestampExpandState`.',
  '- Logical contract migration: the next unused contiguous number only when the contract release is shippable; 163 is conditional on every currently planned migration landing first.',
];
const forkDocLines = read(forkDoc).split(/\r?\n/);
for (const decision of decisionLines) {
  if (forkDocLines.filter((line) => line === decision).length !== 1) {
    fail(`${forkDoc}: expected exact decision line: ${decision}`);
  }
}

const evidenceIndexBlock = sectionLines(read(forkDoc), '## Evidence Index', (line) => line.startsWith('## '));
if (!evidenceIndexBlock) fail('PR-054A evidence index missing');
const expectedEvidenceTable = [
  '| Evidence | Source |',
  '| --- | --- |',
  '| Accepted architecture decision | [`ADR-0055`](../adr/0055-external-execution-timestamp-instants.md) |',
  '| Approved hybrid design | [`External-execution TIMESTAMPTZ hybrid design`](../superpowers/specs/2026-07-15-external-execution-timestamptz-hybrid-design.md) |',
  '| Implementation plan | [`External-execution timestamp expand`](../superpowers/plans/2026-07-15-external-execution-timestamp-expand.md) |',
  '| Extension allocation ledger | [`Enterprise operator control-plane program`](../superpowers/plans/2026-07-14-enterprise-operator-control-plane-program.md) |',
  '| Deterministic allocation check | [`pr054a-validate-timestamp-expand.mjs`](../../hack/pr054a-validate-timestamp-expand.mjs) |',
];
const evidenceTable = evidenceIndexBlock.slice(1).filter((line) => line.trim() !== '');
if (!sameValues(evidenceTable, expectedEvidenceTable)) {
  fail('PR-054A evidence index mismatch');
}
const evidenceLinks = evidenceTable.flatMap((line) =>
  [...line.matchAll(/\[[^\]]+\]\(([^)]+)\)/g)].map((match) => match[1])
);
const forkDocDirectory = path.dirname(path.join(root, forkDoc));
for (const target of evidenceLinks) {
  if (path.isAbsolute(target) || /^[a-z][a-z0-9+.-]*:/i.test(target) || target.startsWith('#')) {
    fail(`PR-054A evidence link is not repository-relative: ${target}`);
  }
  const resolved = path.resolve(forkDocDirectory, target);
  const relative = path.relative(root, resolved);
  if (relative === '..' || relative.startsWith(`..${path.sep}`) || path.isAbsolute(relative)) {
    fail(`PR-054A evidence link escapes repository: ${target}`);
  }
  if (!existsSync(resolved) || !statSync(resolved).isFile()) {
    fail(`PR-054A evidence link target missing: ${target}`);
  }
}

const forkIndexHeading = '### PR-054A - External-execution timestamp expand allocation';
const forkIndexBlock = sectionLines(read('docs/fork/FORK_DIFF_INDEX.md'), forkIndexHeading, (line) =>
  line.startsWith('### ')
);
if (!forkIndexBlock) fail('fork index missing structured PR-054A entry');
for (const required of [
  '- Status: Allocated; this documentation-only slice reserves migration and ADR identities for later implementation.',
  '- Database changes: None in this slice. Physical migration 138 is reserved for the additive expand implementation.',
  '- Documentation: Added [PR-054A evidence index](PR-054A_EXTERNAL_EXECUTION_TIMESTAMP_EXPAND.md), master-roadmap extension ledger, and collision-free unopened plan allocations.',
  '- Tests: Added `hack/pr054a-validate-timestamp-expand.mjs` and `hack/pr054a-validate-timestamp-expand.test.mjs` for exact allocation and isolated negative-fixture validation.',
]) {
  if (!forkIndexBlock.includes(required)) fail(`fork index PR-054A entry mismatch: missing ${required}`);
}

const masterRoadmapHeading = '### Accepted post-PR-050 extension ledger';
const masterRoadmapBlock = sectionLines(
  read('docs/roadmaps/DISTR_COMMUNITY_FORK_MASTER_PLAN.md'),
  masterRoadmapHeading,
  (line) => line === '---' || line.startsWith('## ')
);
if (!masterRoadmapBlock) fail('master roadmap missing structured extension ledger');
for (const required of [
  '[enterprise operator control-plane program](../superpowers/plans/2026-07-14-enterprise-operator-control-plane-program.md).',
  '[PR-054A](../fork/PR-054A_EXTERNAL_EXECUTION_TIMESTAMP_EXPAND.md).',
]) {
  if (!masterRoadmapBlock.includes(required)) fail(`master roadmap PR-054A ledger mismatch: missing ${required}`);
}
const expectedMasterLedger = [
  '| Extension slice | Migration allocation | ADR allocation |',
  '| --- | --- | --- |',
  '| PR-054A | 138 | 0055 |',
  '| PR-055 | None | None |',
  '| PR-056 through PR-065 | 139 through 147 | 0056 through 0060 |',
  '| PR-066 through PR-078 | 148 through 160 | 0061 through 0066 |',
  '| PR-079 | 161 | 0067 |',
  '| PR-080 through PR-081 | None | None |',
  '| PR-082 | 162 | 0068 |',
  '| PR-083 | None | None |',
];
const masterLedger = masterRoadmapBlock.filter((line) => line.trimStart().startsWith('|'));
if (!sameValues(masterLedger, expectedMasterLedger)) {
  fail('master roadmap allocation ledger mismatch');
}

console.log('PR-054A timestamp allocation validation passed');
```

- [ ] **Step 2: Run the validator and confirm the red state.**

```powershell
node hack/pr054a-validate-timestamp-expand.mjs
```

Expected: FAIL with `missing docs/fork/PR-054A_EXTERNAL_EXECUTION_TIMESTAMP_EXPAND.md`.

- [ ] **Step 3: Apply the exact allocation table.** Reconcile `FORK_DIFF_INDEX.md`, add the PR-054A fork document, link the master roadmap extension ledger, and replace every unopened migration/ADR filename/range/predecessor using the table above. Preserve historical PR-000 through PR-050 text. The PR-054A document must contain these literal decision lines:

```markdown
- Physical expand migration: migration 138.
- Architecture decision: ADR-0055.
- Durable zero-history proof: `ExternalExecutionTimestampExpandState`.
- Logical contract migration: the next unused contiguous number only when the contract release is shippable; 163 is conditional on every currently planned migration landing first.
```

- [ ] **Step 4: Run the validator and confirm green.**

```powershell
node hack/pr054a-validate-timestamp-expand.test.mjs
node hack/pr054a-validate-timestamp-expand.mjs
```

Expected: the isolated negative fixtures are rejected for their intended reasons, followed by
`PR-054A timestamp allocation validation passed`.

- [ ] **Step 5: Verify and commit.**

```powershell
rg -n "migration 138|138_|ADR-0055|0055-" docs/superpowers/plans docs/roadmaps docs/fork
node hack/pr054a-validate-timestamp-expand.test.mjs
node hack/pr054a-validate-timestamp-expand.mjs
git diff --check
git add docs/fork docs/roadmaps/DISTR_COMMUNITY_FORK_MASTER_PLAN.md docs/superpowers/plans hack/pr054a-validate-timestamp-expand.mjs hack/pr054a-validate-timestamp-expand.test.mjs
git commit -m "docs: allocate timestamp expand prerequisite"
```

## Task 2: Implement the Pure Manifest and Conversion Contract

**Files:**

- Create: `internal/types/external_execution_timestamp.go`
- Create: `internal/externalexecutiontimestamp/canonical.go`
- Create: `internal/externalexecutiontimestamp/canonical_test.go`
- Create: `internal/externalexecutiontimestamp/manifest.go`
- Create: `internal/externalexecutiontimestamp/manifest_test.go`
- Create: `internal/externalexecutiontimestamp/migration_lock.go`
- Create: `internal/externalexecutiontimestamp/migration_lock_test.go`

**Interfaces:**

- Consumes: only Go standard-library hashing, binary, sorting, regexp, and time packages plus `google/uuid`; it never reads a database, environment variable, locale, or `time.Local`.
- Produces:

```go
const RawWallLayout = "2006-01-02T15:04:05.000000"
const InstantLayout = "2006-01-02T15:04:05.000000Z"
const ConversionExpressionVersion = "external-execution-offset/v1"

func CanonicalRawCell(types.ExternalExecutionTimestampRawCell) ([]byte, error)
func ComputeRawCellChecksum(types.ExternalExecutionTimestampRawCell) (string, error)
func ComputeRawSetChecksum([]types.ExternalExecutionTimestampRawCell) (string, error)
func ComputeDatabaseIdentityChecksum(
    sourceVersion uint,
    executionIDs []uuid.UUID,
    eventIDs []uuid.UUID,
    rawCellCount uint64,
    rawSetChecksum string,
) (string, error)
func ParseInstant(string) (time.Time, error)
func FormatInstant(time.Time) string
func ConvertWallClock(string, int32) (time.Time, error)
func ComputeCellDecisionChecksum(types.ExternalExecutionTimestampCellDecision) (string, error)
func ComputeDecisionContentChecksum(types.ExternalExecutionTimestampManifest) (string, error)
func ValidateManifestDocument(types.ExternalExecutionTimestampManifest) []error
func ValidateSupersession(
    previous types.ExternalExecutionTimestampManifest,
    next types.ExternalExecutionTimestampManifest,
) []error

const MigrationAdvisoryLockKey int64 = 4303907527027985411
```

`ComputeRawCellChecksum` hashes one cell. `ComputeRawSetChecksum` hashes the complete sorted set. Tasks 4-6 use those exact names; do not reintroduce the ambiguous `RawCellChecksum([]cell)` interface.

Task 2 also owns the shared advisory-lock key because Task 5 must compile before the migration runner is implemented. Create `migration_lock.go` with the constant above. Pin the domain and value in `migration_lock_test.go`:

```go
func TestMigrationAdvisoryLockKey(t *testing.T) {
    digest := sha256.Sum256([]byte(
        "distr-external-execution-timestamp-migration/v1",
    ))
    derived := int64(binary.BigEndian.Uint64(digest[:8]))
    if derived != MigrationAdvisoryLockKey {
        t.Fatalf("migration advisory lock key = %d, want %d",
            MigrationAdvisoryLockKey, derived)
    }
}
```

The constant is the signed big-endian value of the first eight SHA-256 bytes of that exact versioned domain. No task asks PostgreSQL to derive or hash the key.

- [ ] **Step 1: Add the operator-only document types**

Create `internal/types/external_execution_timestamp.go` with this content. Do not modify `internal/types/external_execution.go`.

```go
package types

import "github.com/google/uuid"

type ExternalExecutionTimestampDecision string

const (
    ExternalExecutionTimestampDecisionProven ExternalExecutionTimestampDecision = "PROVEN"
    ExternalExecutionTimestampDecisionAttested ExternalExecutionTimestampDecision = "ATTESTED"
    ExternalExecutionTimestampDecisionUnresolved ExternalExecutionTimestampDecision = "UNRESOLVED"
    ExternalExecutionTimestampDecisionNull ExternalExecutionTimestampDecision = "NULL_VALUE"
)

type ExternalExecutionTimestampManifestState string

const (
    ExternalExecutionTimestampManifestStateDraft ExternalExecutionTimestampManifestState = "DRAFT"
    ExternalExecutionTimestampManifestStateApproved ExternalExecutionTimestampManifestState = "APPROVED"
    ExternalExecutionTimestampManifestStateApplied ExternalExecutionTimestampManifestState = "APPLIED"
    ExternalExecutionTimestampManifestStateVerified ExternalExecutionTimestampManifestState = "VERIFIED"
    ExternalExecutionTimestampManifestStateRevokedBeforeApply ExternalExecutionTimestampManifestState =
        "REVOKED_BEFORE_APPLY"
)

type ExternalExecutionTimestampRawCell struct {
    SourceTable string `json:"sourceTable"`
    SourceRowID uuid.UUID `json:"sourceRowId"`
    SourceColumn string `json:"sourceColumn"`
    ColumnOrdinal uint8 `json:"columnOrdinal"`
    RawValue *string `json:"rawValue"`
    RawCellChecksum string `json:"rawCellChecksum"`
}

type ExternalExecutionTimestampCellDecision struct {
    ExternalExecutionTimestampRawCell
    Decision ExternalExecutionTimestampDecision `json:"decision"`
    SourceZone string `json:"sourceZone,omitempty"`
    SourceOffsetSeconds *int32 `json:"sourceOffsetSeconds,omitempty"`
    ConvertedValue *string `json:"convertedValue,omitempty"`
    EvidenceReference string `json:"evidenceReference,omitempty"`
    EvidenceChecksum string `json:"evidenceChecksum,omitempty"`
    ApprovingIdentity string `json:"approvingIdentity,omitempty"`
    ConversionExpressionVersion string `json:"conversionExpressionVersion"`
}

type ExternalExecutionTimestampManifest struct {
    ID uuid.UUID `json:"id"`
    SupersedesManifestID *uuid.UUID `json:"supersedesManifestId,omitempty"`
    DatabaseIdentityChecksum string `json:"databaseIdentityChecksum"`
    SourceSchemaVersion uint `json:"sourceSchemaVersion"`
    SnapshotStartedAt string `json:"snapshotStartedAt"`
    SnapshotEndedAt string `json:"snapshotEndedAt"`
    ExecutionCount uint64 `json:"executionCount"`
    EventCount uint64 `json:"eventCount"`
    RawCellCount uint64 `json:"rawCellCount"`
    PopulatedCellCount uint64 `json:"populatedCellCount"`
    RawCellChecksum string `json:"rawCellChecksum"`
    EvidenceBundleReference string `json:"evidenceBundleReference,omitempty"`
    EvidenceBundleChecksum string `json:"evidenceBundleChecksum,omitempty"`
    ToolVersion string `json:"toolVersion"`
    ConversionExpressionVersion string `json:"conversionExpressionVersion"`
    AuthorIdentity string `json:"authorIdentity,omitempty"`
    ReviewerIdentity string `json:"reviewerIdentity,omitempty"`
    ApprovedAt string `json:"approvedAt,omitempty"`
    TargetReleaseCommit string `json:"targetReleaseCommit,omitempty"`
    TargetImageDigest string `json:"targetImageDigest,omitempty"`
    State ExternalExecutionTimestampManifestState `json:"state"`
    DecisionContentChecksum string `json:"decisionContentChecksum"`
    Cells []ExternalExecutionTimestampCellDecision `json:"cells"`
}
```

- [ ] **Step 2: Write the failing byte-level checksum and conversion tests**

Create `internal/externalexecutiontimestamp/canonical_test.go`:

```go
package externalexecutiontimestamp_test

import (
    "strings"
    "testing"

    "github.com/distr-sh/distr/internal/externalexecutiontimestamp"
    "github.com/distr-sh/distr/internal/types"
    "github.com/google/uuid"
    . "github.com/onsi/gomega"
)

func stringPointer(value string) *string { return &value }

func TestCanonicalRawCellKnownVectorsAndSorting(t *testing.T) {
    g := NewWithT(t)
    rowID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
    created := types.ExternalExecutionTimestampRawCell{
        SourceTable: "ExternalExecution", SourceRowID: rowID,
        SourceColumn: "CREATED_AT", ColumnOrdinal: 1,
        RawValue: stringPointer("2026-07-15T10:11:12.123456"),
    }
    started := types.ExternalExecutionTimestampRawCell{
        SourceTable: "externalexecution", SourceRowID: rowID,
        SourceColumn: "started_at", ColumnOrdinal: 3,
    }

    createdChecksum, err := externalexecutiontimestamp.ComputeRawCellChecksum(created)
    g.Expect(err).NotTo(HaveOccurred())
    g.Expect(createdChecksum).To(Equal(
        "sha256:570790f803e3f137b10c20d677ff5a769ce570e5c4cb4631dfea9c860b875599",
    ))
    nullChecksum, err := externalexecutiontimestamp.ComputeRawCellChecksum(started)
    g.Expect(err).NotTo(HaveOccurred())
    g.Expect(nullChecksum).To(Equal(
        "sha256:971fac6746efa0eee7a3b0da1bbd3d41c63b4869de9c3a59df96e8f7389aa9be",
    ))

    first, err := externalexecutiontimestamp.ComputeRawSetChecksum(
        []types.ExternalExecutionTimestampRawCell{started, created},
    )
    g.Expect(err).NotTo(HaveOccurred())
    second, err := externalexecutiontimestamp.ComputeRawSetChecksum(
        []types.ExternalExecutionTimestampRawCell{created, started},
    )
    g.Expect(err).NotTo(HaveOccurred())
    g.Expect(first).To(Equal(second))
    g.Expect(first).To(Equal(
        "sha256:ec4f13a15923b3f14255d3f40692ee40e591f640f735605fddf62497a8f24fa0",
    ))

    identity, err := externalexecutiontimestamp.ComputeDatabaseIdentityChecksum(
        137, []uuid.UUID{rowID}, nil, 2, first,
    )
    g.Expect(err).NotTo(HaveOccurred())
    g.Expect(identity).To(Equal(
        "sha256:896dbdbb325b56a0a47c6dbfcbed6a35b98e09550fb4e76aedcdd4946f532d00",
    ))
}

func TestCanonicalRawSetRejectsDuplicatesAndInvalidCells(t *testing.T) {
    g := NewWithT(t)
    value := "2026-07-15T10:11:12.123456"
    cell := types.ExternalExecutionTimestampRawCell{
        SourceTable: "externalexecution", SourceRowID: uuid.New(),
        SourceColumn: "created_at", ColumnOrdinal: 1, RawValue: &value,
    }
    _, err := externalexecutiontimestamp.ComputeRawSetChecksum(
        []types.ExternalExecutionTimestampRawCell{cell, cell},
    )
    g.Expect(err).To(MatchError(ContainSubstring("duplicate raw cell")))

    cell.ColumnOrdinal = 2
    _, err = externalexecutiontimestamp.ComputeRawCellChecksum(cell)
    g.Expect(err).To(MatchError(ContainSubstring("allowlist")))
}

func TestConvertWallClockUsesOnlyExplicitOffset(t *testing.T) {
    testCases := []struct {
        raw string
        offset int32
        want string
    }{
        {"2026-07-15T12:00:00.000000", 0, "2026-07-15T12:00:00.000000Z"},
        {"2026-07-15T12:00:00.000000", 7 * 3600, "2026-07-15T05:00:00.000000Z"},
        {"2026-07-15T12:00:00.000000", -5 * 3600, "2026-07-15T17:00:00.000000Z"},
        {"2026-07-15T12:00:00.000000", 5*3600 + 30*60, "2026-07-15T06:30:00.000000Z"},
        {"2026-11-01T01:30:00.000000", -4 * 3600, "2026-11-01T05:30:00.000000Z"},
        {"2026-11-01T01:30:00.000000", -5 * 3600, "2026-11-01T06:30:00.000000Z"},
    }
    for _, testCase := range testCases {
        instant, err := externalexecutiontimestamp.ConvertWallClock(
            testCase.raw, testCase.offset,
        )
        NewWithT(t).Expect(err).NotTo(HaveOccurred())
        NewWithT(t).Expect(externalexecutiontimestamp.FormatInstant(instant)).
            To(Equal(testCase.want))
    }
    _, err := externalexecutiontimestamp.ConvertWallClock(
        "2026-07-15T12:00:00.000000", 64801,
    )
    NewWithT(t).Expect(err).To(MatchError(ContainSubstring("offset seconds")))
}

func TestDatabaseIdentityRejectsDuplicateIDs(t *testing.T) {
    id := uuid.New()
    _, err := externalexecutiontimestamp.ComputeDatabaseIdentityChecksum(
        137, []uuid.UUID{id, id}, nil, 10, "sha256:"+strings.Repeat("a", 64),
    )
    NewWithT(t).Expect(err).To(MatchError(ContainSubstring("duplicate execution id")))
}
```

- [ ] **Step 3: Run the canonical tests and verify they fail**

Run:

```powershell
go test -p=1 ./internal/externalexecutiontimestamp -run 'Test(Canonical|Convert|DatabaseIdentity)' -count=1
```

Expected: FAIL because the package and exported functions do not exist.

- [ ] **Step 4: Implement the fixed framing and conversion primitives**

Create `internal/externalexecutiontimestamp/canonical.go`. Use these exact domains and allowlist:

```go
const rawCellDomain = "distr.external-execution-timestamp/raw-cell/v1"
const rawSetDomain = "distr.external-execution-timestamp/raw-set/v1"
const databaseIdentityDomain = "distr.external-execution-timestamp/database-identity/v1"
const cellDecisionDomain = "distr.external-execution-timestamp/cell-decision/v1"
const manifestDecisionDomain = "distr.external-execution-timestamp/manifest-decision/v1"

var cellOrdinals = map[string]map[string]uint8{
    "externalexecution": {
        "created_at": 1,
        "updated_at": 2,
        "started_at": 3,
        "completed_at": 4,
        "callback_deadline_at": 5,
    },
    "externalexecutionevent": {"created_at": 6},
}
```

Implement framing with a `bytes.Buffer`: for every field, write `binary.BigEndian.PutUint32` of its UTF-8 byte length, followed by the bytes. `CanonicalRawCell` frames domain, lowercase table, lowercase UUID, lowercase column, base-10 ordinal, then `NULL` or `VALUE` plus the exact `RawWallLayout` value. `ComputeRawSetChecksum` copies and sorts cells by table/UUID/ordinal, rejects a duplicate key, frames the decimal count and each recomputed cell checksum. `ComputeDatabaseIdentityChecksum` copies/sorts both ID slices, rejects nil/duplicate IDs, and frames the exact contract fields. Use:

```go
func ParseInstant(value string) (time.Time, error) {
    instant, err := time.Parse(InstantLayout, value)
    if err != nil || instant.Format(InstantLayout) != value {
        return time.Time{}, fmt.Errorf("instant must use %s", InstantLayout)
    }
    return instant.UTC(), nil
}

func FormatInstant(value time.Time) string {
    return value.UTC().Format(InstantLayout)
}

func ConvertWallClock(raw string, offsetSeconds int32) (time.Time, error) {
    if offsetSeconds < -64800 || offsetSeconds > 64800 {
        return time.Time{}, fmt.Errorf(
            "offset seconds must be between -64800 and 64800",
        )
    }
    wall, err := time.Parse(RawWallLayout, raw)
    if err != nil || wall.Format(RawWallLayout) != raw {
        return time.Time{}, fmt.Errorf("raw wall value must use %s", RawWallLayout)
    }
    return wall.Add(-time.Duration(offsetSeconds) * time.Second).UTC(), nil
}
```

Run:

```powershell
gofmt -w internal/types/external_execution_timestamp.go internal/externalexecutiontimestamp/canonical.go internal/externalexecutiontimestamp/canonical_test.go
go test -p=1 ./internal/externalexecutiontimestamp -run 'Test(Canonical|Convert|DatabaseIdentity)' -count=1
```

Expected: PASS with the four fixed SHA-256 vectors unchanged.

- [ ] **Step 5: Write the failing manifest tests with a complete five-cell fixture**

Create `internal/externalexecutiontimestamp/manifest_test.go`:

```go
package externalexecutiontimestamp_test

import (
    "slices"
    "strings"
    "testing"

    "github.com/distr-sh/distr/internal/externalexecutiontimestamp"
    "github.com/distr-sh/distr/internal/types"
    "github.com/google/uuid"
    . "github.com/onsi/gomega"
)

func validDraftManifest(t *testing.T) types.ExternalExecutionTimestampManifest {
    t.Helper()
    rowID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
    rawValues := []*string{
        stringPointer("2026-07-15T10:00:00.000000"),
        stringPointer("2026-07-15T10:05:00.000000"),
        nil,
        nil,
        stringPointer("2026-07-15T10:10:00.000000"),
    }
    columns := []string{
        "created_at", "updated_at", "started_at",
        "completed_at", "callback_deadline_at",
    }
    rawCells := make([]types.ExternalExecutionTimestampRawCell, 0, 5)
    decisions := make([]types.ExternalExecutionTimestampCellDecision, 0, 5)
    for index, column := range columns {
        raw := types.ExternalExecutionTimestampRawCell{
            SourceTable: "externalexecution",
            SourceRowID: rowID,
            SourceColumn: column,
            ColumnOrdinal: uint8(index + 1),
            RawValue: rawValues[index],
        }
        checksum, err := externalexecutiontimestamp.ComputeRawCellChecksum(raw)
        NewWithT(t).Expect(err).NotTo(HaveOccurred())
        raw.RawCellChecksum = checksum
        decision := types.ExternalExecutionTimestampDecisionUnresolved
        if raw.RawValue == nil {
            decision = types.ExternalExecutionTimestampDecisionNull
        }
        rawCells = append(rawCells, raw)
        decisions = append(decisions, types.ExternalExecutionTimestampCellDecision{
            ExternalExecutionTimestampRawCell: raw,
            Decision: decision,
            ConversionExpressionVersion:
                externalexecutiontimestamp.ConversionExpressionVersion,
        })
    }
    rawSet, err := externalexecutiontimestamp.ComputeRawSetChecksum(rawCells)
    NewWithT(t).Expect(err).NotTo(HaveOccurred())
    identity, err := externalexecutiontimestamp.ComputeDatabaseIdentityChecksum(
        137, []uuid.UUID{rowID}, nil, 5, rawSet,
    )
    NewWithT(t).Expect(err).NotTo(HaveOccurred())
    manifest := types.ExternalExecutionTimestampManifest{
        ID: uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
        DatabaseIdentityChecksum: identity,
        SourceSchemaVersion: 137,
        SnapshotStartedAt: "2026-07-15T10:20:00.000000Z",
        SnapshotEndedAt: "2026-07-15T10:20:01.000000Z",
        ExecutionCount: 1,
        RawCellCount: 5,
        PopulatedCellCount: 3,
        RawCellChecksum: rawSet,
        ToolVersion: "distr-test",
        ConversionExpressionVersion:
            externalexecutiontimestamp.ConversionExpressionVersion,
        State: types.ExternalExecutionTimestampManifestStateDraft,
        Cells: decisions,
    }
    manifest.DecisionContentChecksum, err =
        externalexecutiontimestamp.ComputeDecisionContentChecksum(manifest)
    NewWithT(t).Expect(err).NotTo(HaveOccurred())
    return manifest
}

func approveManifest(
    t *testing.T,
    manifest types.ExternalExecutionTimestampManifest,
) types.ExternalExecutionTimestampManifest {
    t.Helper()
    manifest.State = types.ExternalExecutionTimestampManifestStateApproved
    manifest.EvidenceBundleReference = "evidence-bundle:timestamp-review-1"
    manifest.EvidenceBundleChecksum = "sha256:" + strings.Repeat("e", 64)
    manifest.AuthorIdentity = "operator@example.test"
    manifest.ReviewerIdentity = "reviewer@example.test"
    manifest.ApprovedAt = "2026-07-15T11:00:00.000000Z"
    manifest.TargetReleaseCommit = strings.Repeat("a", 40)
    manifest.TargetImageDigest = "sha256:" + strings.Repeat("b", 64)
    var err error
    manifest.DecisionContentChecksum, err =
        externalexecutiontimestamp.ComputeDecisionContentChecksum(manifest)
    NewWithT(t).Expect(err).NotTo(HaveOccurred())
    return manifest
}

func resolveFirstCell(
    t *testing.T,
    manifest *types.ExternalExecutionTimestampManifest,
) {
    t.Helper()
    offset := int32(7 * 3600)
    converted, err := externalexecutiontimestamp.ConvertWallClock(
        *manifest.Cells[0].RawValue, offset,
    )
    NewWithT(t).Expect(err).NotTo(HaveOccurred())
    value := externalexecutiontimestamp.FormatInstant(converted)
    manifest.Cells[0].Decision =
        types.ExternalExecutionTimestampDecisionProven
    manifest.Cells[0].SourceZone = "Asia/Bangkok"
    manifest.Cells[0].SourceOffsetSeconds = &offset
    manifest.Cells[0].ConvertedValue = &value
    manifest.Cells[0].EvidenceReference = "log-record:execution-created"
    manifest.Cells[0].EvidenceChecksum = "sha256:" + strings.Repeat("c", 64)
    manifest.Cells[0].ApprovingIdentity = "reviewer@example.test"
}

func containsProblem(problems []error, fragment string) bool {
    return slices.ContainsFunc(problems, func(problem error) bool {
        return strings.Contains(problem.Error(), fragment)
    })
}

func TestValidateManifestRequiresCompleteSnapshot(t *testing.T) {
    g := NewWithT(t)
    manifest := validDraftManifest(t)
    g.Expect(externalexecutiontimestamp.ValidateManifestDocument(manifest)).
        To(BeEmpty())

    missing := manifest
    missing.Cells = slices.Clone(manifest.Cells[:4])
    missing.DecisionContentChecksum, _ =
        externalexecutiontimestamp.ComputeDecisionContentChecksum(missing)
    g.Expect(containsProblem(
        externalexecutiontimestamp.ValidateManifestDocument(missing),
        "raw cell count",
    )).To(BeTrue())

    duplicate := manifest
    duplicate.Cells = append(slices.Clone(manifest.Cells), manifest.Cells[0])
    duplicate.DecisionContentChecksum, _ =
        externalexecutiontimestamp.ComputeDecisionContentChecksum(duplicate)
    g.Expect(containsProblem(
        externalexecutiontimestamp.ValidateManifestDocument(duplicate),
        "duplicate cell",
    )).To(BeTrue())
}

func TestValidateManifestDecisionMatrix(t *testing.T) {
    g := NewWithT(t)
    manifest := validDraftManifest(t)
    converted := "2026-07-15T03:00:00.000000Z"
    manifest.Cells[0].ConvertedValue = &converted
    manifest.DecisionContentChecksum, _ =
        externalexecutiontimestamp.ComputeDecisionContentChecksum(manifest)
    g.Expect(containsProblem(
        externalexecutiontimestamp.ValidateManifestDocument(manifest),
        "UNRESOLVED",
    )).To(BeTrue())

    manifest = validDraftManifest(t)
    manifest.Cells[0].Decision =
        types.ExternalExecutionTimestampDecisionProven
    manifest.DecisionContentChecksum, _ =
        externalexecutiontimestamp.ComputeDecisionContentChecksum(manifest)
    g.Expect(containsProblem(
        externalexecutiontimestamp.ValidateManifestDocument(manifest),
        "PROVEN",
    )).To(BeTrue())

    manifest = validDraftManifest(t)
    resolveFirstCell(t, &manifest)
    wrong := "2026-07-15T04:00:00.000000Z"
    manifest.Cells[0].ConvertedValue = &wrong
    manifest.DecisionContentChecksum, _ =
        externalexecutiontimestamp.ComputeDecisionContentChecksum(manifest)
    g.Expect(containsProblem(
        externalexecutiontimestamp.ValidateManifestDocument(manifest),
        "does not reproduce",
    )).To(BeTrue())
}

func TestApprovedManifestRequiresIndependentReviewAndReleaseIdentity(t *testing.T) {
    g := NewWithT(t)
    manifest := approveManifest(t, validDraftManifest(t))
    g.Expect(externalexecutiontimestamp.ValidateManifestDocument(manifest)).
        To(BeEmpty())

    manifest.ReviewerIdentity = manifest.AuthorIdentity
    manifest.DecisionContentChecksum, _ =
        externalexecutiontimestamp.ComputeDecisionContentChecksum(manifest)
    g.Expect(containsProblem(
        externalexecutiontimestamp.ValidateManifestDocument(manifest),
        "must differ",
    )).To(BeTrue())
}

func TestDecisionChecksumExcludesOnlyLifecycleStateAndApprovalInstant(t *testing.T) {
    g := NewWithT(t)
    manifest := approveManifest(t, validDraftManifest(t))
    first, err := externalexecutiontimestamp.ComputeDecisionContentChecksum(manifest)
    g.Expect(err).NotTo(HaveOccurred())
    manifest.State = types.ExternalExecutionTimestampManifestStateApplied
    manifest.ApprovedAt = "2026-07-15T11:01:00.000000Z"
    second, err := externalexecutiontimestamp.ComputeDecisionContentChecksum(manifest)
    g.Expect(err).NotTo(HaveOccurred())
    g.Expect(second).To(Equal(first))
    manifest.TargetReleaseCommit = strings.Repeat("c", 40)
    third, err := externalexecutiontimestamp.ComputeDecisionContentChecksum(manifest)
    g.Expect(err).NotTo(HaveOccurred())
    g.Expect(third).NotTo(Equal(first))
}

func TestValidateSupersessionPreservesResolvedCells(t *testing.T) {
    g := NewWithT(t)
    previous := approveManifest(t, validDraftManifest(t))
    resolveFirstCell(t, &previous)
    previous.DecisionContentChecksum, _ =
        externalexecutiontimestamp.ComputeDecisionContentChecksum(previous)
    next := previous
    next.ID = uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
    next.SupersedesManifestID = &previous.ID
    next.Cells = slices.Clone(previous.Cells)
    next.DecisionContentChecksum, _ =
        externalexecutiontimestamp.ComputeDecisionContentChecksum(next)
    g.Expect(externalexecutiontimestamp.ValidateSupersession(previous, next)).
        To(BeEmpty())

    wrong := "2026-07-15T04:00:00.000000Z"
    next.Cells[0].ConvertedValue = &wrong
    next.DecisionContentChecksum, _ =
        externalexecutiontimestamp.ComputeDecisionContentChecksum(next)
    g.Expect(containsProblem(
        externalexecutiontimestamp.ValidateSupersession(previous, next),
        "resolved instant cannot change",
    )).To(BeTrue())
}
```

- [ ] **Step 6: Run the manifest tests and verify they fail**

Run:

```powershell
go test -p=1 ./internal/externalexecutiontimestamp -run 'Test(Validate|Approved|DecisionChecksum)' -count=1
```

Expected: FAIL with undefined manifest checksum and validation functions.

- [ ] **Step 7: Implement decision framing and validation**

Create `internal/externalexecutiontimestamp/manifest.go`. `ComputeCellDecisionChecksum` frames the raw-cell checksum, decision, nullable zone, nullable base-10 offset, nullable canonical instant, evidence reference/checksum, approver, and required conversion version. `ComputeDecisionContentChecksum` sorts a copy of `Cells` by the raw-cell key and frames exactly the manifest-decision fields from the fixed contract; it does not frame `State` or `ApprovedAt`.

Use this exact conversion matrix in `ValidateManifestDocument`:

```go
switch cell.Decision {
case types.ExternalExecutionTimestampDecisionNull:
    if cell.RawValue != nil || !noConversion {
        add("cell %d NULL_VALUE requires null raw value and no conversion evidence", index)
    }
case types.ExternalExecutionTimestampDecisionUnresolved:
    if cell.RawValue == nil || !noConversion {
        add("cell %d UNRESOLVED requires a raw value and no conversion evidence", index)
    }
case types.ExternalExecutionTimestampDecisionProven,
    types.ExternalExecutionTimestampDecisionAttested:
    if cell.RawValue == nil ||
        cell.SourceOffsetSeconds == nil ||
        cell.ConvertedValue == nil ||
        strings.TrimSpace(cell.EvidenceReference) == "" ||
        !checksumPattern.MatchString(cell.EvidenceChecksum) ||
        strings.TrimSpace(cell.ApprovingIdentity) == "" {
        add(
            "cell %d %s requires raw value, explicit offset, converted value, evidence, and approver",
            index, cell.Decision,
        )
        break
    }
    expected, err := ConvertWallClock(*cell.RawValue, *cell.SourceOffsetSeconds)
    if err != nil {
        add("cell %d conversion: %v", index, err)
        break
    }
    converted, err := ParseInstant(*cell.ConvertedValue)
    if err != nil {
        add("cell %d converted value: %v", index, err)
    } else if !converted.Equal(expected) {
        add(
            "cell %d converted value does not reproduce raw wall minus explicit offset",
            index,
        )
    }
default:
    add("cell %d has unsupported decision %q", index, cell.Decision)
}
```

Validation must also require source schema version exactly 137; recompute every raw-cell checksum, the complete raw-set checksum, the database identity, and the decision checksum; enforce exactly five ordinals per execution and ordinal 6 per event; enforce `rawCellCount = 5*executionCount + eventCount = len(cells)`; and reject duplicate/unexpected cells. A `DRAFT` has either all approval/release metadata empty or all of it complete with no `ApprovedAt`. `APPROVED`, `APPLIED`, and `VERIFIED` require a canonical `ApprovedAt`, evidence bundle, distinct author/reviewer, full 40-character lowercase commit, and image digest. `REVOKED_BEFORE_APPLY` preserves either the draft-empty or approved-complete metadata form.

`ValidateSupersession` validates the new complete document and requires `next.SupersedesManifestID == previous.ID`. A superseding manifest is a complete decision revision over the same original fenced schema-137 snapshot, not a new capture of live lifecycle columns: source version, snapshot interval, execution/event/raw/populated counts, database identity, raw-set checksum, exact cell key set, and every raw value/raw-cell checksum remain identical. A prior `PROVEN` or `ATTESTED` decision, converted instant, zone/offset, cell evidence, approving identity, and conversion-expression version remain immutable, and `NULL_VALUE` remains immutable. Only an `UNRESOLVED` decision on an unchanged raw cell may remain unresolved or advance to `PROVEN` or `ATTESTED`; manifest identity, approval/evidence-bundle metadata, target release metadata, tool version, and the decision checksum may change.

- [ ] **Step 8: Run, inspect the public boundary, and commit**

```powershell
gofmt -w internal/types/external_execution_timestamp.go internal/externalexecutiontimestamp
go test -p=1 ./internal/externalexecutiontimestamp -count=1
go vet ./internal/externalexecutiontimestamp
git diff -- internal/types/external_execution.go internal/mapping api
git diff --check
git add internal/types/external_execution_timestamp.go internal/externalexecutiontimestamp
git commit -m "feat: define timestamp evidence manifest contract"
```

Expected: tests and vet PASS; the public execution/mapping/API diff has no output.

## Task 3: Add Migration 138 Without Historical Backfill

**Files:**

- Create: `internal/migrations/sql/138_external_execution_timestamp_expand.up.sql`
- Create: `internal/migrations/sql/138_external_execution_timestamp_expand.down.sql`
- Create: `internal/migrations/test_database_test.go`
- Create: `internal/migrations/external_execution_timestamp_integration_test.go`

**Interfaces:**

- Consumes: the embedded migration filesystem in `internal/migrations/migrate.go`, migration 137, golang-migrate v4.19.1, and `DISTR_TEST_DATABASE_URL`.
- Produces: schema 138 with six nullable shadows, paired future defaults, two future indexes, immutable manifest/provenance records, one immutable expand-transition row, the contract-gate foundation, and a reversible pre-application down migration.
- The SQL does not import Go manifest code, update an existing execution/event row, or infer any historical offset.

- [ ] **Step 1: Build a real isolated-schema golang-migrate harness**

Create `internal/migrations/test_database_test.go`:

```go
package migrations

import (
    "context"
    "errors"
    "os"
    "strings"
    "testing"

    migrate "github.com/golang-migrate/migrate/v4"
    "github.com/golang-migrate/migrate/v4/database/postgres"
    "github.com/golang-migrate/migrate/v4/source/iofs"
    "github.com/google/uuid"
    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/jackc/pgx/v5/stdlib"
    . "github.com/onsi/gomega"
)

type migrationTestDatabase struct {
    pool *pgxpool.Pool
    runner *migrate.Migrate
}

func newMigrationTestDatabase(t *testing.T) *migrationTestDatabase {
    t.Helper()
    g := NewWithT(t)
    databaseURL := os.Getenv("DISTR_TEST_DATABASE_URL")
    if databaseURL == "" {
        t.Skip("DISTR_TEST_DATABASE_URL is not set")
    }
    ctx := context.Background()
    admin, err := pgxpool.New(ctx, databaseURL)
    g.Expect(err).NotTo(HaveOccurred())
    schema := "migration_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
    quotedSchema := pgx.Identifier{schema}.Sanitize()
    _, err = admin.Exec(ctx, "CREATE SCHEMA "+quotedSchema)
    g.Expect(err).NotTo(HaveOccurred())

    sqlConfig, err := pgx.ParseConfig(databaseURL)
    g.Expect(err).NotTo(HaveOccurred())
    sqlConfig.RuntimeParams["search_path"] = schema
    sqlDB := stdlib.OpenDB(*sqlConfig)
    g.Expect(sqlDB.PingContext(ctx)).To(Succeed())
    databaseDriver, err := postgres.WithInstance(sqlDB, &postgres.Config{
        SchemaName: schema,
    })
    g.Expect(err).NotTo(HaveOccurred())
    sourceDriver, err := iofs.New(fs, "sql")
    g.Expect(err).NotTo(HaveOccurred())
    runner, err := migrate.NewWithInstance(
        "", sourceDriver, "distr-test", databaseDriver,
    )
    g.Expect(err).NotTo(HaveOccurred())

    poolConfig, err := pgxpool.ParseConfig(databaseURL)
    g.Expect(err).NotTo(HaveOccurred())
    poolConfig.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
        _, err := conn.Exec(ctx, "SET search_path TO "+quotedSchema)
        return err
    }
    pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
    g.Expect(err).NotTo(HaveOccurred())
    database := &migrationTestDatabase{pool: pool, runner: runner}
    t.Cleanup(func() {
        pool.Close()
        _, _ = runner.Close()
        _, dropErr := admin.Exec(
            context.Background(), "DROP SCHEMA IF EXISTS "+quotedSchema+" CASCADE",
        )
        if dropErr != nil {
            t.Logf("drop migration test schema: %v", dropErr)
        }
        admin.Close()
    })
    return database
}

func (database *migrationTestDatabase) migrateTo(t *testing.T, version uint) {
    t.Helper()
    err := database.runner.Migrate(version)
    if !errors.Is(err, migrate.ErrNoChange) {
        NewWithT(t).Expect(err).NotTo(HaveOccurred())
    }
    actual, dirty, err := database.runner.Version()
    NewWithT(t).Expect(err).NotTo(HaveOccurred())
    NewWithT(t).Expect(actual).To(Equal(version))
    NewWithT(t).Expect(dirty).To(BeFalse())
}

func dropExternalExecutionFixtureForeignKeys(
    t *testing.T,
    database *migrationTestDatabase,
) {
    t.Helper()
    _, err := database.pool.Exec(context.Background(), `
DO $$
DECLARE item RECORD;
BEGIN
  FOR item IN
    SELECT relation.relname, constraint_row.conname
    FROM pg_constraint constraint_row
    JOIN pg_class relation ON relation.oid = constraint_row.conrelid
    WHERE relation.relname IN ('externalexecution', 'externalexecutionevent')
      AND constraint_row.contype = 'f'
  LOOP
    EXECUTE format(
      'ALTER TABLE %I DROP CONSTRAINT %I',
      item.relname,
      item.conname
    );
  END LOOP;
END
$$`)
    NewWithT(t).Expect(err).NotTo(HaveOccurred())
}

func insertHistoricalExecutionFixture(
    t *testing.T,
    database *migrationTestDatabase,
) (uuid.UUID, uuid.UUID) {
    t.Helper()
    g := NewWithT(t)
    executionID := uuid.New()
    eventID := uuid.New()
    organizationID := uuid.New()
    dropExternalExecutionFixtureForeignKeys(t, database)
    _, err := database.pool.Exec(context.Background(), `
INSERT INTO ExternalExecution (
  id, created_at, updated_at, started_at, completed_at, callback_deadline_at,
  organization_id, step_run_id, task_id, deployment_plan_id,
  deployment_plan_target_id, deployment_target_id, application_id,
  release_bundle_id, component, plan_checksum, idempotency_key,
  expected_state_version, expected_version, expected_image, expected_platform,
  expected_config_reference, expected_config_checksum
) VALUES (
  $1,
  TIMESTAMP '2026-07-15 10:00:00.000001',
  TIMESTAMP '2026-07-15 10:01:00.000002',
  TIMESTAMP '2026-07-15 10:02:00.000003',
  TIMESTAMP '2026-07-15 10:03:00.000004',
  TIMESTAMP '2026-07-15 10:04:00.000005',
  $2, $3, $4, $5, $6, $7, $8, $9,
  'api-image', 'sha256:' || repeat('a', 64), 'fixture-' || $1::text,
  0, '1.0.0', 'repo/image@sha256:' || repeat('b', 64), 'linux/amd64',
  'config:fixture', 'sha256:' || repeat('c', 64)
)`,
        executionID, organizationID, uuid.New(), uuid.New(), uuid.New(),
        uuid.New(), uuid.New(), uuid.New(), uuid.New(),
    )
    g.Expect(err).NotTo(HaveOccurred())
    _, err = database.pool.Exec(context.Background(), `
INSERT INTO ExternalExecutionEvent (
  id, created_at, organization_id, external_execution_id,
  sequence, status, payload_hash
) VALUES (
  $1, TIMESTAMP '2026-07-15 10:05:00.000006',
  $2, $3, 1, 'SUCCEEDED', 'sha256:' || repeat('d', 64)
)`, eventID, organizationID, executionID)
    g.Expect(err).NotTo(HaveOccurred())
    return executionID, eventID
}
```

The fixture drops only foreign keys inside its UUID-named test schema. It still uses the real embedded source, the PostgreSQL migration driver, `search_path`, and `postgres.Config.SchemaName`.

- [ ] **Step 2: Write the failing historical, marker, catalog, trigger, and down tests**

Create `internal/migrations/external_execution_timestamp_integration_test.go`:

```go
package migrations

import (
    "context"
    "strings"
    "testing"

    "github.com/google/uuid"
    . "github.com/onsi/gomega"
)

func TestExternalExecutionTimestampMigration138LeavesHistoryUnchanged(t *testing.T) {
    g := NewWithT(t)
    database := newMigrationTestDatabase(t)
    database.migrateTo(t, 137)
    executionID, eventID := insertHistoricalExecutionFixture(t, database)
    database.migrateTo(t, 138)

    var allShadowsNull bool
    err := database.pool.QueryRow(context.Background(), `
SELECT
  execution.created_at_instant IS NULL
  AND execution.updated_at_instant IS NULL
  AND execution.started_at_instant IS NULL
  AND execution.completed_at_instant IS NULL
  AND execution.callback_deadline_at_instant IS NULL
  AND event.created_at_instant IS NULL
FROM ExternalExecution execution
JOIN ExternalExecutionEvent event
  ON event.external_execution_id = execution.id
WHERE execution.id = $1 AND event.id = $2`,
        executionID, eventID,
    ).Scan(&allShadowsNull)
    g.Expect(err).NotTo(HaveOccurred())
    g.Expect(allShadowsNull).To(BeTrue())

    var kind string
    var sourceVersion, executionCount, eventCount, rawCellCount int64
    err = database.pool.QueryRow(context.Background(), `
SELECT transition_kind, source_schema_version,
       transition_execution_count, transition_event_count,
       transition_raw_cell_count
FROM ExternalExecutionTimestampExpandState
WHERE singleton`).Scan(
        &kind, &sourceVersion, &executionCount, &eventCount, &rawCellCount,
    )
    g.Expect(err).NotTo(HaveOccurred())
    g.Expect(kind).To(Equal("MANIFEST_REQUIRED"))
    g.Expect([]int64{
        sourceVersion, executionCount, eventCount, rawCellCount,
    }).To(Equal([]int64{137, 1, 1, 6}))
}

func TestExternalExecutionTimestampMigration138RecordsDurableZeroHistory(t *testing.T) {
    g := NewWithT(t)
    database := newMigrationTestDatabase(t)
    database.migrateTo(t, 138)

    var kind string
    var executionCount, eventCount, rawCellCount int64
    err := database.pool.QueryRow(context.Background(), `
SELECT transition_kind, transition_execution_count,
       transition_event_count, transition_raw_cell_count
FROM ExternalExecutionTimestampExpandState
WHERE singleton`).Scan(
        &kind, &executionCount, &eventCount, &rawCellCount,
    )
    g.Expect(err).NotTo(HaveOccurred())
    g.Expect(kind).To(Equal("ZERO_HISTORY"))
    g.Expect([]int64{
        executionCount, eventCount, rawCellCount,
    }).To(Equal([]int64{0, 0, 0}))

    _, err = database.pool.Exec(context.Background(), `
UPDATE ExternalExecutionTimestampExpandState
SET transition_kind = 'MANIFEST_REQUIRED'
WHERE singleton`)
    g.Expect(err).To(MatchError(ContainSubstring("expand state is append-only")))
    _, err = database.pool.Exec(context.Background(), `
DELETE FROM ExternalExecutionTimestampExpandState WHERE singleton`)
    g.Expect(err).To(MatchError(ContainSubstring("expand state is append-only")))
}

func TestExternalExecutionTimestampMigration138DefaultsIgnoreSessionTimezone(
    t *testing.T,
) {
    for _, zone := range []string{"UTC", "Asia/Bangkok", "America/New_York"} {
        t.Run(zone, func(t *testing.T) {
            g := NewWithT(t)
            database := newMigrationTestDatabase(t)
            database.migrateTo(t, 138)
            dropExternalExecutionFixtureForeignKeys(t, database)
            connection, err := database.pool.Acquire(context.Background())
            g.Expect(err).NotTo(HaveOccurred())
            defer connection.Release()
            _, err = connection.Exec(
                context.Background(),
                `SELECT set_config('TimeZone', $1, false)`,
                zone,
            )
            g.Expect(err).NotTo(HaveOccurred())
            executionID := uuid.New()
            eventID := uuid.New()
            organizationID := uuid.New()
            _, err = connection.Exec(context.Background(), `
INSERT INTO ExternalExecution (
  id, callback_deadline_at, callback_deadline_at_instant,
  organization_id, step_run_id, task_id, deployment_plan_id,
  deployment_plan_target_id, deployment_target_id, application_id,
  release_bundle_id, component, plan_checksum, idempotency_key,
  expected_state_version, expected_version, expected_image, expected_platform,
  expected_config_reference, expected_config_checksum
) VALUES (
  $1, CURRENT_TIMESTAMP AT TIME ZONE 'UTC', CURRENT_TIMESTAMP,
  $2, $3, $4, $5, $6, $7, $8, $9,
  'api-image', 'sha256:' || repeat('a', 64), 'default-' || $1::text,
  0, '1.0.0', 'repo/image@sha256:' || repeat('b', 64), 'linux/amd64',
  'config:fixture', 'sha256:' || repeat('c', 64)
)`,
                executionID, organizationID, uuid.New(), uuid.New(), uuid.New(),
                uuid.New(), uuid.New(), uuid.New(), uuid.New(),
            )
            g.Expect(err).NotTo(HaveOccurred())
            _, err = connection.Exec(context.Background(), `
INSERT INTO ExternalExecutionEvent (
  id, organization_id, external_execution_id, sequence, status, payload_hash
) VALUES (
  $1, $2, $3, 1, 'RUNNING', 'sha256:' || repeat('d', 64)
)`, eventID, organizationID, executionID)
            g.Expect(err).NotTo(HaveOccurred())

            var paired bool
            err = connection.QueryRow(context.Background(), `
SELECT
  execution.created_at =
    execution.created_at_instant AT TIME ZONE 'UTC'
  AND execution.updated_at =
    execution.updated_at_instant AT TIME ZONE 'UTC'
  AND event.created_at =
    event.created_at_instant AT TIME ZONE 'UTC'
FROM ExternalExecution execution
JOIN ExternalExecutionEvent event
  ON event.external_execution_id = execution.id
WHERE execution.id = $1 AND event.id = $2`,
                executionID, eventID,
            ).Scan(&paired)
            g.Expect(err).NotTo(HaveOccurred())
            g.Expect(paired).To(BeTrue())
        })
    }
}

func TestExternalExecutionTimestampMigration138Catalog(t *testing.T) {
    g := NewWithT(t)
    database := newMigrationTestDatabase(t)
    database.migrateTo(t, 138)
    var columnCount int64
    var allTimestamptz, allNullable bool
    err := database.pool.QueryRow(context.Background(), `
SELECT
  count(*),
  bool_and(data_type = 'timestamp with time zone'),
  bool_and(is_nullable = 'YES')
FROM information_schema.columns
WHERE table_schema = current_schema()
  AND (
    (table_name = 'externalexecution' AND column_name IN (
      'created_at_instant', 'updated_at_instant', 'started_at_instant',
      'completed_at_instant', 'callback_deadline_at_instant'
    ))
    OR
    (table_name = 'externalexecutionevent' AND column_name = 'created_at_instant')
  )`).Scan(&columnCount, &allTimestamptz, &allNullable)
    g.Expect(err).NotTo(HaveOccurred())
    g.Expect(columnCount).To(Equal(int64(6)))
    g.Expect(allTimestamptz).To(BeTrue())
    g.Expect(allNullable).To(BeTrue())

    for indexName, expected := range map[string]string{
        "externalexecution_organization_status_instant_next":
            "(organization_id, status, updated_at_instant DESC, id)",
        "externalexecution_task_instant_next":
            "(task_id, created_at_instant, id)",
    } {
        var definition string
        err = database.pool.QueryRow(context.Background(), `
SELECT pg_get_indexdef(index_row.indexrelid)
FROM pg_index index_row
JOIN pg_class index_class ON index_class.oid = index_row.indexrelid
WHERE index_class.relname = $1`, indexName).Scan(&definition)
        g.Expect(err).NotTo(HaveOccurred())
        g.Expect(definition).To(ContainSubstring(expected))
    }
}

func insertTimestampManifestFixture(
    t *testing.T,
    database *migrationTestDatabase,
) (uuid.UUID, string) {
    t.Helper()
    manifestID := uuid.New()
    checksum := "sha256:" + strings.Repeat("a", 64)
    _, err := database.pool.Exec(context.Background(), `
INSERT INTO ExternalExecutionTimestampManifest (
  id, database_identity_checksum, source_schema_version,
  snapshot_started_at, snapshot_ended_at,
  execution_count, event_count, raw_cell_count, populated_cell_count,
  raw_cell_checksum, evidence_bundle_reference, evidence_bundle_checksum,
  tool_version, conversion_expression_version,
  author_identity, reviewer_identity, approved_at,
  target_release_commit, target_image_digest,
  state, decision_content_checksum
) VALUES (
  $1, $2, 137,
  CURRENT_TIMESTAMP - INTERVAL '2 minutes',
  CURRENT_TIMESTAMP - INTERVAL '1 minute',
  1, 0, 5, 5,
  $2, 'evidence:fixture', $2,
  'distr-test', 'external-execution-offset/v1',
  'author@example.test', 'reviewer@example.test',
  CURRENT_TIMESTAMP - INTERVAL '30 seconds',
  $3, $4, 'APPROVED', $2
)`,
        manifestID, checksum, strings.Repeat("b", 40),
        "sha256:"+strings.Repeat("c", 64),
    )
    NewWithT(t).Expect(err).NotTo(HaveOccurred())
    _, err = database.pool.Exec(context.Background(), `
INSERT INTO ExternalExecutionTimestampCellProvenance (
  manifest_id, source_table, source_row_id, source_column, column_ordinal,
  raw_value, raw_is_null, decision, source_zone, source_offset_seconds,
  converted_value, evidence_reference, evidence_checksum, approving_identity,
  raw_cell_checksum, parent_manifest_checksum, conversion_expression_version
) VALUES (
  $1, 'externalexecution', $2, 'created_at', 1,
  TIMESTAMP '2026-07-15 10:00:00', FALSE, 'PROVEN', 'UTC', 0,
  TIMESTAMPTZ '2026-07-15 10:00:00+00', 'evidence:fixture', $3,
  'reviewer@example.test', $3, $3, 'external-execution-offset/v1'
)`, manifestID, uuid.New(), checksum)
    NewWithT(t).Expect(err).NotTo(HaveOccurred())
    return manifestID, checksum
}

func TestExternalExecutionTimestampMigration138EnforcesImmutableLifecycle(
    t *testing.T,
) {
    g := NewWithT(t)
    database := newMigrationTestDatabase(t)
    database.migrateTo(t, 138)
    manifestID, _ := insertTimestampManifestFixture(t, database)

    _, err := database.pool.Exec(context.Background(), `
UPDATE ExternalExecutionTimestampCellProvenance
SET approving_identity = 'changed@example.test'
WHERE manifest_id = $1`, manifestID)
    g.Expect(err).To(MatchError(ContainSubstring("provenance is append-only")))
    _, err = database.pool.Exec(context.Background(), `
DELETE FROM ExternalExecutionTimestampCellProvenance
WHERE manifest_id = $1`, manifestID)
    g.Expect(err).To(MatchError(ContainSubstring("provenance is append-only")))
    _, err = database.pool.Exec(context.Background(), `
UPDATE ExternalExecutionTimestampManifest
SET tool_version = 'changed'
WHERE id = $1`, manifestID)
    g.Expect(err).To(MatchError(ContainSubstring("manifest content is immutable")))
    _, err = database.pool.Exec(context.Background(), `
UPDATE ExternalExecutionTimestampManifest
SET state = 'VERIFIED', verified_at = CURRENT_TIMESTAMP
WHERE id = $1`, manifestID)
    g.Expect(err).To(MatchError(ContainSubstring("invalid manifest lifecycle transition")))

    _, err = database.pool.Exec(context.Background(), `
UPDATE ExternalExecutionTimestampManifest
SET state = 'APPLIED', applied_at = CURRENT_TIMESTAMP
WHERE id = $1`, manifestID)
    g.Expect(err).NotTo(HaveOccurred())
    _, err = database.pool.Exec(context.Background(), `
UPDATE ExternalExecutionTimestampManifest
SET state = 'VERIFIED', verified_at = CURRENT_TIMESTAMP
WHERE id = $1`, manifestID)
    g.Expect(err).NotTo(HaveOccurred())
    _, err = database.pool.Exec(context.Background(), `
UPDATE ExternalExecutionTimestampManifest
SET state = 'APPROVED', verified_at = NULL
WHERE id = $1`, manifestID)
    g.Expect(err).To(MatchError(ContainSubstring("invalid manifest lifecycle transition")))
}

func TestExternalExecutionTimestampMigration138RejectsManifestForks(
    t *testing.T,
) {
    g := NewWithT(t)
    database := newMigrationTestDatabase(t)
    database.migrateTo(t, 138)
    rootID, checksum := insertTimestampManifestFixture(t, database)

    insertChild := func(id uuid.UUID, state string) error {
        _, err := database.pool.Exec(context.Background(), `
INSERT INTO ExternalExecutionTimestampManifest (
  id, supersedes_manifest_id, database_identity_checksum,
  source_schema_version, snapshot_started_at, snapshot_ended_at,
  execution_count, event_count, raw_cell_count, populated_cell_count,
  raw_cell_checksum, evidence_bundle_reference, evidence_bundle_checksum,
  tool_version, conversion_expression_version, author_identity,
  reviewer_identity, approved_at, target_release_commit,
  target_image_digest, state, decision_content_checksum, revoked_at
) SELECT
  $1, $2, $3, source_schema_version,
  CURRENT_TIMESTAMP - INTERVAL '2 minutes',
  CURRENT_TIMESTAMP - INTERVAL '1 minute',
  execution_count, event_count, raw_cell_count, populated_cell_count,
  raw_cell_checksum, evidence_bundle_reference, evidence_bundle_checksum,
  tool_version, conversion_expression_version, author_identity,
  reviewer_identity, CURRENT_TIMESTAMP - INTERVAL '30 seconds',
  target_release_commit, target_image_digest, $4,
  'sha256:' || repeat(substr(md5($1::text), 1, 1), 64),
  CASE WHEN $4 = 'REVOKED_BEFORE_APPLY' THEN CURRENT_TIMESTAMP ELSE NULL END
FROM ExternalExecutionTimestampManifest WHERE id = $2`,
            id, rootID, checksum, state,
        )
        return err
    }

    firstChild := uuid.New()
    g.Expect(insertChild(firstChild, "APPROVED")).To(Succeed())
    g.Expect(insertChild(uuid.New(), "APPROVED")).To(
        MatchError(ContainSubstring("externalexecutiontimestampmanifest_active_parent_unique")),
    )

    _, err := database.pool.Exec(context.Background(), `
UPDATE ExternalExecutionTimestampManifest
SET state = 'REVOKED_BEFORE_APPLY', revoked_at = CURRENT_TIMESTAMP
WHERE id = $1`, firstChild)
    g.Expect(err).NotTo(HaveOccurred())
    g.Expect(insertChild(uuid.New(), "APPROVED")).To(Succeed())
}

func TestExternalExecutionTimestampMigration138DownRestores137BeforeApply(
    t *testing.T,
) {
    g := NewWithT(t)
    database := newMigrationTestDatabase(t)
    database.migrateTo(t, 137)
    executionID, eventID := insertHistoricalExecutionFixture(t, database)
    database.migrateTo(t, 138)
    database.migrateTo(t, 137)

    var executionExists, eventExists bool
    g.Expect(database.pool.QueryRow(context.Background(),
        `SELECT EXISTS (SELECT 1 FROM ExternalExecution WHERE id = $1)`,
        executionID,
    ).Scan(&executionExists)).To(Succeed())
    g.Expect(database.pool.QueryRow(context.Background(),
        `SELECT EXISTS (SELECT 1 FROM ExternalExecutionEvent WHERE id = $1)`,
        eventID,
    ).Scan(&eventExists)).To(Succeed())
    g.Expect(executionExists).To(BeTrue())
    g.Expect(eventExists).To(BeTrue())

    var shadowCount int64
    g.Expect(database.pool.QueryRow(context.Background(), `
SELECT count(*)
FROM information_schema.columns
WHERE table_schema = current_schema()
  AND table_name IN ('externalexecution', 'externalexecutionevent')
  AND column_name LIKE '%_instant'`).Scan(&shadowCount)).To(Succeed())
    g.Expect(shadowCount).To(Equal(int64(0)))
    var markerExists bool
    g.Expect(database.pool.QueryRow(context.Background(), `
SELECT to_regclass('externalexecutiontimestampexpandstate') IS NOT NULL`).
        Scan(&markerExists)).To(Succeed())
    g.Expect(markerExists).To(BeFalse())
}
```

- [ ] **Step 3: Run the tests before adding migration 138**

Run:

```powershell
go test -p=1 ./internal/migrations -run 'TestExternalExecutionTimestampMigration138' -count=1 -timeout 20m
```

Expected: FAIL because migration 138 and its relations do not exist.

- [ ] **Step 4: Add migration 138 with complete constraints and triggers**

Create `internal/migrations/sql/138_external_execution_timestamp_expand.up.sql`:

```sql
SET LOCAL lock_timeout = '10s';
SET LOCAL statement_timeout = '5min';

ALTER TABLE ExternalExecution
  ADD COLUMN created_at_instant TIMESTAMPTZ,
  ADD COLUMN updated_at_instant TIMESTAMPTZ,
  ADD COLUMN started_at_instant TIMESTAMPTZ,
  ADD COLUMN completed_at_instant TIMESTAMPTZ,
  ADD COLUMN callback_deadline_at_instant TIMESTAMPTZ;

ALTER TABLE ExternalExecutionEvent
  ADD COLUMN created_at_instant TIMESTAMPTZ;

ALTER TABLE ExternalExecution
  ALTER COLUMN created_at SET DEFAULT (CURRENT_TIMESTAMP AT TIME ZONE 'UTC'),
  ALTER COLUMN updated_at SET DEFAULT (CURRENT_TIMESTAMP AT TIME ZONE 'UTC'),
  ALTER COLUMN created_at_instant SET DEFAULT CURRENT_TIMESTAMP,
  ALTER COLUMN updated_at_instant SET DEFAULT CURRENT_TIMESTAMP;

ALTER TABLE ExternalExecutionEvent
  ALTER COLUMN created_at SET DEFAULT (CURRENT_TIMESTAMP AT TIME ZONE 'UTC'),
  ALTER COLUMN created_at_instant SET DEFAULT CURRENT_TIMESTAMP;

CREATE INDEX ExternalExecution_organization_status_instant_next
  ON ExternalExecution (organization_id, status, updated_at_instant DESC, id);
CREATE INDEX ExternalExecution_task_instant_next
  ON ExternalExecution (task_id, created_at_instant, id);

CREATE TABLE ExternalExecutionTimestampManifest (
  id UUID PRIMARY KEY,
  supersedes_manifest_id UUID
    REFERENCES ExternalExecutionTimestampManifest(id)
    ON UPDATE RESTRICT ON DELETE RESTRICT,
  database_identity_checksum TEXT NOT NULL
    CHECK (database_identity_checksum ~ '^sha256:[0-9a-f]{64}$'),
  source_schema_version INTEGER NOT NULL CHECK (source_schema_version >= 137),
  snapshot_started_at TIMESTAMPTZ NOT NULL,
  snapshot_ended_at TIMESTAMPTZ NOT NULL,
  execution_count BIGINT NOT NULL CHECK (execution_count >= 0),
  event_count BIGINT NOT NULL CHECK (event_count >= 0),
  raw_cell_count BIGINT NOT NULL CHECK (raw_cell_count >= 0),
  populated_cell_count BIGINT NOT NULL
    CHECK (populated_cell_count >= 0 AND populated_cell_count <= raw_cell_count),
  raw_cell_checksum TEXT NOT NULL
    CHECK (raw_cell_checksum ~ '^sha256:[0-9a-f]{64}$'),
  evidence_bundle_reference TEXT,
  evidence_bundle_checksum TEXT,
  tool_version TEXT NOT NULL CHECK (length(btrim(tool_version)) > 0),
  conversion_expression_version TEXT NOT NULL
    CHECK (conversion_expression_version = 'external-execution-offset/v1'),
  author_identity TEXT,
  reviewer_identity TEXT,
  approved_at TIMESTAMPTZ,
  target_release_commit TEXT,
  target_image_digest TEXT,
  state TEXT NOT NULL CHECK (
    state IN ('DRAFT', 'APPROVED', 'APPLIED', 'VERIFIED', 'REVOKED_BEFORE_APPLY')
  ),
  decision_content_checksum TEXT NOT NULL
    CHECK (decision_content_checksum ~ '^sha256:[0-9a-f]{64}$'),
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  applied_at TIMESTAMPTZ,
  verified_at TIMESTAMPTZ,
  revoked_at TIMESTAMPTZ,
  CONSTRAINT externalexecutiontimestampmanifest_not_self_superseding
    CHECK (supersedes_manifest_id IS NULL OR supersedes_manifest_id <> id),
  CONSTRAINT externalexecutiontimestampmanifest_snapshot_order
    CHECK (snapshot_ended_at >= snapshot_started_at),
  CONSTRAINT externalexecutiontimestampmanifest_expected_cell_count
    CHECK (raw_cell_count = 5 * execution_count + event_count),
  CONSTRAINT externalexecutiontimestampmanifest_metadata_all_or_none CHECK (
    (
      evidence_bundle_reference IS NULL
      AND evidence_bundle_checksum IS NULL
      AND author_identity IS NULL
      AND reviewer_identity IS NULL
      AND target_release_commit IS NULL
      AND target_image_digest IS NULL
    )
    OR
    (
      evidence_bundle_reference IS NOT NULL
      AND evidence_bundle_checksum IS NOT NULL
      AND author_identity IS NOT NULL
      AND reviewer_identity IS NOT NULL
      AND target_release_commit IS NOT NULL
      AND target_image_digest IS NOT NULL
      AND length(btrim(evidence_bundle_reference)) > 0
      AND evidence_bundle_checksum ~ '^sha256:[0-9a-f]{64}$'
      AND length(btrim(author_identity)) > 0
      AND length(btrim(reviewer_identity)) > 0
      AND author_identity <> reviewer_identity
      AND target_release_commit ~ '^[0-9a-f]{40}$'
      AND target_image_digest ~ '^sha256:[0-9a-f]{64}$'
    )
  ),
  CONSTRAINT externalexecutiontimestampmanifest_lifecycle_shape CHECK (
    (
      state = 'DRAFT'
      AND approved_at IS NULL AND applied_at IS NULL
      AND verified_at IS NULL AND revoked_at IS NULL
    )
    OR
    (
      state = 'APPROVED'
      AND approved_at IS NOT NULL
      AND evidence_bundle_reference IS NOT NULL
      AND applied_at IS NULL AND verified_at IS NULL AND revoked_at IS NULL
    )
    OR
    (
      state = 'APPLIED'
      AND approved_at IS NOT NULL AND applied_at IS NOT NULL
      AND applied_at >= approved_at
      AND evidence_bundle_reference IS NOT NULL
      AND verified_at IS NULL AND revoked_at IS NULL
    )
    OR
    (
      state = 'VERIFIED'
      AND approved_at IS NOT NULL AND applied_at IS NOT NULL
      AND verified_at IS NOT NULL
      AND applied_at >= approved_at AND verified_at >= applied_at
      AND evidence_bundle_reference IS NOT NULL
      AND revoked_at IS NULL
    )
    OR
    (
      state = 'REVOKED_BEFORE_APPLY'
      AND applied_at IS NULL AND verified_at IS NULL
      AND revoked_at IS NOT NULL AND revoked_at >= created_at
      AND (
        approved_at IS NULL
        OR (
          evidence_bundle_reference IS NOT NULL
          AND revoked_at >= approved_at
        )
      )
    )
  ),
  CONSTRAINT externalexecutiontimestampmanifest_id_checksum_unique
    UNIQUE (id, decision_content_checksum)
);

CREATE UNIQUE INDEX externalexecutiontimestampmanifest_active_parent_unique
  ON ExternalExecutionTimestampManifest (supersedes_manifest_id)
  NULLS NOT DISTINCT
  WHERE state <> 'REVOKED_BEFORE_APPLY';

CREATE TABLE ExternalExecutionTimestampCellProvenance (
  manifest_id UUID NOT NULL,
  source_table TEXT NOT NULL,
  source_row_id UUID NOT NULL,
  source_column TEXT NOT NULL,
  column_ordinal SMALLINT NOT NULL,
  raw_value TIMESTAMP WITHOUT TIME ZONE,
  raw_is_null BOOLEAN NOT NULL,
  decision TEXT NOT NULL
    CHECK (decision IN ('PROVEN', 'ATTESTED', 'UNRESOLVED', 'NULL_VALUE')),
  source_zone TEXT,
  source_offset_seconds INTEGER
    CHECK (source_offset_seconds BETWEEN -64800 AND 64800),
  converted_value TIMESTAMPTZ,
  evidence_reference TEXT,
  evidence_checksum TEXT CHECK (
    evidence_checksum IS NULL
    OR evidence_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  approving_identity TEXT,
  raw_cell_checksum TEXT NOT NULL
    CHECK (raw_cell_checksum ~ '^sha256:[0-9a-f]{64}$'),
  parent_manifest_checksum TEXT NOT NULL
    CHECK (parent_manifest_checksum ~ '^sha256:[0-9a-f]{64}$'),
  conversion_expression_version TEXT NOT NULL
    CHECK (conversion_expression_version = 'external-execution-offset/v1'),
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (manifest_id, source_table, source_row_id, source_column),
  CONSTRAINT externalexecutiontimestampcell_manifest_checksum_fk
    FOREIGN KEY (manifest_id, parent_manifest_checksum)
    REFERENCES ExternalExecutionTimestampManifest(id, decision_content_checksum)
    ON UPDATE RESTRICT ON DELETE RESTRICT,
  CONSTRAINT externalexecutiontimestampcell_raw_null_match
    CHECK (raw_is_null = (raw_value IS NULL)),
  CONSTRAINT externalexecutiontimestampcell_allowlist CHECK (
    (
      source_table = 'externalexecution'
      AND (source_column, column_ordinal) IN (
        ('created_at', 1),
        ('updated_at', 2),
        ('started_at', 3),
        ('completed_at', 4),
        ('callback_deadline_at', 5)
      )
    )
    OR
    (
      source_table = 'externalexecutionevent'
      AND source_column = 'created_at'
      AND column_ordinal = 6
    )
  ),
  CONSTRAINT externalexecutiontimestampcell_decision_shape CHECK (
    (
      decision = 'NULL_VALUE'
      AND raw_is_null AND raw_value IS NULL
      AND source_zone IS NULL AND source_offset_seconds IS NULL
      AND converted_value IS NULL AND evidence_reference IS NULL
      AND evidence_checksum IS NULL AND approving_identity IS NULL
    )
    OR
    (
      decision = 'UNRESOLVED'
      AND NOT raw_is_null AND raw_value IS NOT NULL
      AND source_zone IS NULL AND source_offset_seconds IS NULL
      AND converted_value IS NULL AND evidence_reference IS NULL
      AND evidence_checksum IS NULL AND approving_identity IS NULL
    )
    OR
    (
      decision IN ('PROVEN', 'ATTESTED')
      AND NOT raw_is_null AND raw_value IS NOT NULL
      AND source_offset_seconds IS NOT NULL
      AND converted_value IS NOT NULL
      AND evidence_reference IS NOT NULL
      AND evidence_checksum IS NOT NULL
      AND approving_identity IS NOT NULL
      AND length(btrim(evidence_reference)) > 0
      AND evidence_checksum ~ '^sha256:[0-9a-f]{64}$'
      AND length(btrim(approving_identity)) > 0
      AND (source_zone IS NULL OR length(btrim(source_zone)) > 0)
    )
  )
);

CREATE TABLE ExternalExecutionTimestampExpandState (
  singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
  transition_kind TEXT NOT NULL
    CHECK (transition_kind IN ('ZERO_HISTORY', 'MANIFEST_REQUIRED')),
  source_schema_version INTEGER NOT NULL CHECK (source_schema_version = 137),
  transition_execution_count BIGINT NOT NULL
    CHECK (transition_execution_count >= 0),
  transition_event_count BIGINT NOT NULL CHECK (transition_event_count >= 0),
  transition_raw_cell_count BIGINT NOT NULL
    CHECK (transition_raw_cell_count >= 0),
  transitioned_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT externalexecutiontimestampexpandstate_cell_count CHECK (
    transition_raw_cell_count =
      5 * transition_execution_count + transition_event_count
  ),
  CONSTRAINT externalexecutiontimestampexpandstate_kind_matches_counts CHECK (
    (
      transition_kind = 'ZERO_HISTORY'
      AND transition_execution_count = 0
      AND transition_event_count = 0
      AND transition_raw_cell_count = 0
    )
    OR
    (
      transition_kind = 'MANIFEST_REQUIRED'
      AND (transition_execution_count > 0 OR transition_event_count > 0)
    )
  )
);

CREATE TABLE ExternalExecutionTimestampContractGate (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  manifest_id UUID NOT NULL
    REFERENCES ExternalExecutionTimestampManifest(id)
    ON UPDATE RESTRICT ON DELETE RESTRICT,
  expected_schema_version INTEGER NOT NULL
    CHECK (expected_schema_version >= 138),
  contract_migration_version INTEGER NOT NULL
    CHECK (contract_migration_version > expected_schema_version),
  target_release_commit TEXT NOT NULL
    CHECK (target_release_commit ~ '^[0-9a-f]{40}$'),
  target_image_digest TEXT NOT NULL
    CHECK (target_image_digest ~ '^sha256:[0-9a-f]{64}$'),
  backup_reference TEXT NOT NULL CHECK (length(btrim(backup_reference)) > 0),
  backup_checksum TEXT NOT NULL
    CHECK (backup_checksum ~ '^sha256:[0-9a-f]{64}$'),
  restore_verification_reference TEXT NOT NULL
    CHECK (length(btrim(restore_verification_reference)) > 0),
  restore_verification_checksum TEXT NOT NULL
    CHECK (restore_verification_checksum ~ '^sha256:[0-9a-f]{64}$'),
  writer_fence_identifier TEXT NOT NULL
    CHECK (length(btrim(writer_fence_identifier)) > 0),
  prepared_by TEXT NOT NULL CHECK (length(btrim(prepared_by)) > 0),
  prepared_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  expires_at TIMESTAMPTZ NOT NULL,
  consumed_at TIMESTAMPTZ,
  CONSTRAINT externalexecutiontimestampcontractgate_expiry
    CHECK (expires_at > prepared_at),
  CONSTRAINT externalexecutiontimestampcontractgate_consumption_window CHECK (
    consumed_at IS NULL
    OR (consumed_at >= prepared_at AND consumed_at <= expires_at)
  )
);

CREATE FUNCTION external_execution_timestamp_provenance_append_only()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'external execution timestamp provenance is append-only';
END;
$$;

CREATE TRIGGER ExternalExecutionTimestampCellProvenance_append_only
BEFORE UPDATE OR DELETE ON ExternalExecutionTimestampCellProvenance
FOR EACH ROW
EXECUTE FUNCTION external_execution_timestamp_provenance_append_only();

CREATE FUNCTION external_execution_timestamp_expand_state_append_only()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'external execution timestamp expand state is append-only';
END;
$$;

CREATE TRIGGER ExternalExecutionTimestampExpandState_append_only
BEFORE UPDATE OR DELETE ON ExternalExecutionTimestampExpandState
FOR EACH ROW
EXECUTE FUNCTION external_execution_timestamp_expand_state_append_only();

CREATE FUNCTION external_execution_lifecycle_pair_one_shot()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.started_at_instant IS NOT NULL THEN
    IF NEW.started_at IS DISTINCT FROM OLD.started_at
       OR NEW.started_at_instant IS DISTINCT FROM OLD.started_at_instant THEN
      RAISE EXCEPTION 'external execution started_at pair is immutable';
    END IF;
  ELSIF OLD.started_at IS NOT NULL THEN
    IF NEW.started_at IS DISTINCT FROM OLD.started_at THEN
      RAISE EXCEPTION 'external execution started_at pair is immutable';
    END IF;
  ELSIF (NEW.started_at IS NULL) <> (NEW.started_at_instant IS NULL)
     OR (
       NEW.started_at IS NOT NULL
       AND NEW.started_at IS DISTINCT FROM
         (NEW.started_at_instant AT TIME ZONE 'UTC')
     ) THEN
    RAISE EXCEPTION 'external execution started_at must resolve to one exact UTC pair';
  END IF;

  IF OLD.completed_at_instant IS NOT NULL THEN
    IF NEW.completed_at IS DISTINCT FROM OLD.completed_at
       OR NEW.completed_at_instant IS DISTINCT FROM OLD.completed_at_instant THEN
      RAISE EXCEPTION 'external execution completed_at pair is immutable';
    END IF;
  ELSIF OLD.completed_at IS NOT NULL THEN
    IF NEW.completed_at IS DISTINCT FROM OLD.completed_at THEN
      RAISE EXCEPTION 'external execution completed_at pair is immutable';
    END IF;
  ELSIF (NEW.completed_at IS NULL) <> (NEW.completed_at_instant IS NULL)
     OR (
       NEW.completed_at IS NOT NULL
       AND NEW.completed_at IS DISTINCT FROM
         (NEW.completed_at_instant AT TIME ZONE 'UTC')
     ) THEN
    RAISE EXCEPTION 'external execution completed_at must resolve to one exact UTC pair';
  END IF;

  RETURN NEW;
END;
$$;

CREATE TRIGGER ExternalExecution_lifecycle_pair_one_shot
BEFORE UPDATE OF
  started_at, started_at_instant, completed_at, completed_at_instant
ON ExternalExecution
FOR EACH ROW
EXECUTE FUNCTION external_execution_lifecycle_pair_one_shot();

CREATE FUNCTION external_execution_timestamp_manifest_lifecycle()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'external execution timestamp manifest is append-only';
  END IF;

  IF ROW(
    OLD.id, OLD.supersedes_manifest_id, OLD.database_identity_checksum,
    OLD.source_schema_version, OLD.snapshot_started_at, OLD.snapshot_ended_at,
    OLD.execution_count, OLD.event_count, OLD.raw_cell_count,
    OLD.populated_cell_count, OLD.raw_cell_checksum,
    OLD.evidence_bundle_reference, OLD.evidence_bundle_checksum,
    OLD.tool_version, OLD.conversion_expression_version,
    OLD.author_identity, OLD.reviewer_identity, OLD.target_release_commit,
    OLD.target_image_digest, OLD.decision_content_checksum, OLD.created_at
  ) IS DISTINCT FROM ROW(
    NEW.id, NEW.supersedes_manifest_id, NEW.database_identity_checksum,
    NEW.source_schema_version, NEW.snapshot_started_at, NEW.snapshot_ended_at,
    NEW.execution_count, NEW.event_count, NEW.raw_cell_count,
    NEW.populated_cell_count, NEW.raw_cell_checksum,
    NEW.evidence_bundle_reference, NEW.evidence_bundle_checksum,
    NEW.tool_version, NEW.conversion_expression_version,
    NEW.author_identity, NEW.reviewer_identity, NEW.target_release_commit,
    NEW.target_image_digest, NEW.decision_content_checksum, NEW.created_at
  ) THEN
    RAISE EXCEPTION 'external execution timestamp manifest content is immutable';
  END IF;

  IF OLD.state = 'DRAFT' AND NEW.state = 'APPROVED'
     AND OLD.approved_at IS NULL AND NEW.approved_at IS NOT NULL
     AND NEW.applied_at IS NOT DISTINCT FROM OLD.applied_at
     AND NEW.verified_at IS NOT DISTINCT FROM OLD.verified_at
     AND NEW.revoked_at IS NOT DISTINCT FROM OLD.revoked_at THEN
    RETURN NEW;
  END IF;
  IF OLD.state IN ('DRAFT', 'APPROVED')
     AND NEW.state = 'REVOKED_BEFORE_APPLY'
     AND NEW.approved_at IS NOT DISTINCT FROM OLD.approved_at
     AND NEW.applied_at IS NOT DISTINCT FROM OLD.applied_at
     AND NEW.verified_at IS NOT DISTINCT FROM OLD.verified_at
     AND OLD.revoked_at IS NULL AND NEW.revoked_at IS NOT NULL THEN
    RETURN NEW;
  END IF;
  IF OLD.state = 'APPROVED' AND NEW.state = 'APPLIED'
     AND NEW.approved_at IS NOT DISTINCT FROM OLD.approved_at
     AND OLD.applied_at IS NULL AND NEW.applied_at IS NOT NULL
     AND NEW.verified_at IS NOT DISTINCT FROM OLD.verified_at
     AND NEW.revoked_at IS NOT DISTINCT FROM OLD.revoked_at THEN
    RETURN NEW;
  END IF;
  IF OLD.state = 'APPLIED' AND NEW.state = 'VERIFIED'
     AND NEW.approved_at IS NOT DISTINCT FROM OLD.approved_at
     AND NEW.applied_at IS NOT DISTINCT FROM OLD.applied_at
     AND OLD.verified_at IS NULL AND NEW.verified_at IS NOT NULL
     AND NEW.revoked_at IS NOT DISTINCT FROM OLD.revoked_at THEN
    RETURN NEW;
  END IF;
  RAISE EXCEPTION 'invalid manifest lifecycle transition from % to %',
    OLD.state, NEW.state;
END;
$$;

CREATE TRIGGER ExternalExecutionTimestampManifest_lifecycle
BEFORE UPDATE OR DELETE ON ExternalExecutionTimestampManifest
FOR EACH ROW
EXECUTE FUNCTION external_execution_timestamp_manifest_lifecycle();

WITH transition_counts AS (
  SELECT
    (SELECT count(*) FROM ExternalExecution) AS execution_count,
    (SELECT count(*) FROM ExternalExecutionEvent) AS event_count
)
INSERT INTO ExternalExecutionTimestampExpandState (
  singleton, transition_kind, source_schema_version,
  transition_execution_count, transition_event_count,
  transition_raw_cell_count, transitioned_at
)
SELECT
  TRUE,
  CASE
    WHEN execution_count = 0 AND event_count = 0 THEN 'ZERO_HISTORY'
    ELSE 'MANIFEST_REQUIRED'
  END,
  137,
  execution_count,
  event_count,
  5 * execution_count + event_count,
  CURRENT_TIMESTAMP
FROM transition_counts;
```

- [ ] **Step 5: Add the reversible pre-application down migration**

Create `internal/migrations/sql/138_external_execution_timestamp_expand.down.sql`:

```sql
SET LOCAL lock_timeout = '10s';
SET LOCAL statement_timeout = '5min';

DROP TABLE ExternalExecutionTimestampContractGate;

DROP TRIGGER ExternalExecutionTimestampCellProvenance_append_only
  ON ExternalExecutionTimestampCellProvenance;
DROP FUNCTION external_execution_timestamp_provenance_append_only();

DROP TRIGGER ExternalExecutionTimestampManifest_lifecycle
  ON ExternalExecutionTimestampManifest;
DROP FUNCTION external_execution_timestamp_manifest_lifecycle();
DROP TABLE ExternalExecutionTimestampCellProvenance;
DROP TABLE ExternalExecutionTimestampManifest;

DROP TRIGGER ExternalExecutionTimestampExpandState_append_only
  ON ExternalExecutionTimestampExpandState;
DROP FUNCTION external_execution_timestamp_expand_state_append_only();
DROP TABLE ExternalExecutionTimestampExpandState;

DROP TRIGGER ExternalExecution_lifecycle_pair_one_shot
  ON ExternalExecution;
DROP FUNCTION external_execution_lifecycle_pair_one_shot();

DROP INDEX ExternalExecution_task_instant_next;
DROP INDEX ExternalExecution_organization_status_instant_next;

ALTER TABLE ExternalExecutionEvent
  DROP COLUMN created_at_instant,
  ALTER COLUMN created_at SET DEFAULT now();

ALTER TABLE ExternalExecution
  DROP COLUMN callback_deadline_at_instant,
  DROP COLUMN completed_at_instant,
  DROP COLUMN started_at_instant,
  DROP COLUMN updated_at_instant,
  DROP COLUMN created_at_instant,
  ALTER COLUMN created_at SET DEFAULT now(),
  ALTER COLUMN updated_at SET DEFAULT now();
```

Task 6's supported migration preflight refuses this down migration after a manifest reaches `APPLIED` or `VERIFIED`.

- [ ] **Step 6: Validate, prove no historical backfill, and commit**

```powershell
bash hack/validate-migrations.sh
go test -p=1 ./internal/migrations -run 'TestExternalExecutionTimestampMigration138' -count=1 -timeout 20m
rg -n "UPDATE\s+ExternalExecution|UPDATE\s+ExternalExecutionEvent|USING\s+.*AT TIME ZONE" internal/migrations/sql/138_external_execution_timestamp_expand.up.sql
go test -p=1 ./internal/db -run 'TestExternalExecution' -count=1 -timeout 20m
git diff --check
git add internal/migrations
git commit -m "feat: expand external execution timestamp schema"
```

Expected: migration validation and tests PASS; the prohibited-backfill `rg` has no output; existing external-execution tests PASS.

## Task 4: Inspect and Seal a Complete Offline Manifest

**Files:**

- Modify: `internal/types/external_execution_timestamp.go`
- Modify: `internal/db/tx.go`
- Create: `internal/db/tx_test.go`
- Create: `internal/db/external_execution_timestamps.go`
- Create: `internal/db/external_execution_timestamps_test.go`
- Create: `cmd/hub/cmd/external_execution_timestamps.go`
- Create: `cmd/hub/cmd/external_execution_timestamps_test.go`

**Interfaces:**

- Consumes: Task 2 `ComputeRawCellChecksum`, `ComputeRawSetChecksum`, `ComputeDatabaseIdentityChecksum`, `ComputeDecisionContentChecksum`, `ConvertWallClock`, and `ValidateManifestDocument`; Task 3 schema/catalog.
- Produces: repeatable-read raw inspection, offline seal/validate, and direct-pool CLI entry points consumed by apply and the Compose adapter.

Add transaction options without changing existing callers:

```go
func RunTxOptions(ctx context.Context, options pgx.TxOptions, f func(context.Context) error) error

func RunReadOnlyTxRR(ctx context.Context, f func(context.Context) error) error {
    return RunTxOptions(ctx, pgx.TxOptions{
        IsoLevel:   pgx.RepeatableRead,
        AccessMode: pgx.ReadOnly,
    }, f)
}
```

Add repository interfaces:

```go
func InspectExternalExecutionTimestamps(
    context.Context,
) (*types.ExternalExecutionTimestampManifest, error)

func ValidateExternalExecutionTimestampManifest(
    context.Context,
    types.ExternalExecutionTimestampManifest,
) (*types.ExternalExecutionTimestampValidationReport, error)

func inspectExternalExecutionTimestampsInTx(
    context.Context,
) (*types.ExternalExecutionTimestampManifest, error)

type ExternalExecutionTimestampValidationReport struct {
    ManifestID               uuid.UUID `json:"manifestId"`
    SchemaVersion            uint      `json:"schemaVersion"`
    ExecutionCount           uint64    `json:"executionCount"`
    EventCount               uint64    `json:"eventCount"`
    RawCellCount             uint64    `json:"rawCellCount"`
    PopulatedCellCount       uint64    `json:"populatedCellCount"`
    UnresolvedCellCount      uint64    `json:"unresolvedCellCount"`
    RawSetChecksum           string    `json:"rawSetChecksum"`
    DatabaseIdentityChecksum string    `json:"databaseIdentityChecksum"`
    DecisionContentChecksum  string    `json:"decisionContentChecksum"`
}

type ExternalExecutionTimestampSealOptions struct {
    AuthorIdentity, ReviewerIdentity string
    EvidenceBundleReference, EvidenceBundleChecksum string
    TargetReleaseCommit, TargetImageDigest string
}

func SealManifest(
    types.ExternalExecutionTimestampManifest,
    types.ExternalExecutionTimestampSealOptions,
    time.Time,
) (types.ExternalExecutionTimestampManifest, error)
```

Inspection requirements:

1. Begin one `REPEATABLE READ, READ ONLY` transaction.
2. Read `schema_migrations` directly; do not initialize golang-migrate because that can create its table.
3. Reject dirty state and any schema other than exact 137 or an expand-compatible schema 138 or later.
4. Capture `clock_timestamp()` at snapshot start and end.
5. Read sorted execution/event UUIDs and emit exactly five cells per execution plus one per event, including null cells.
6. Render naive values with `to_char(value, 'YYYY-MM-DD"T"HH24:MI:SS.US')` so the database session timezone is irrelevant.
7. Default populated cells to `UNRESOLVED` and null cells to `NULL_VALUE`; inspection never invents proof.
8. Compute raw-cell, database-identity, and decision-content checksums with Task 2 functions.
9. Emit `DRAFT`; do not write any database row.

Add a direct-pool Cobra command. It must call `pgxpool.New` from `DATABASE_URL`, not `svc.New`, so schema-137 inspection cannot start workers, initialize application services, or run migrations.

```text
distr external-execution-timestamps inspect --output draft.json
distr external-execution-timestamps seal-manifest \
  --input reviewed-draft.json --output approved.json \
  --author "$DISTR_TIMESTAMP_AUTHOR" --reviewer "$DISTR_TIMESTAMP_REVIEWER" \
  --evidence-reference "$DISTR_TIMESTAMP_EVIDENCE_REFERENCE" \
  --evidence-checksum "$DISTR_TIMESTAMP_EVIDENCE_CHECKSUM" \
  --target-commit "$DISTR_RELEASE_COMMIT" \
  --target-image-digest "$DISTR_IMAGE_DIGEST"
distr external-execution-timestamps validate-manifest --manifest approved.json
```

The six environment variables in that command are mandatory runtime evidence supplied by the operator; the command rejects an unset value. `seal-manifest` is offline. It revalidates every cell, recomputes every checksum and converted value, requires different non-empty author/reviewer identities, stamps UTC approval time, and writes a create-new `0600` file. It never overwrites an existing file.

- [ ] **Step 1: Write and run the failing read-only transaction test.**

```go
func TestRunReadOnlyTxRRUsesRepeatableReadAndRejectsWrites(t *testing.T) {
    ctx := releaseBundleDBTestContext(t)
    g := NewWithT(t)
    err := db.RunReadOnlyTxRR(ctx, func(ctx context.Context) error {
        database := internalctx.GetDb(ctx)
        var isolation, readOnly string
        g.Expect(database.QueryRow(ctx, `SHOW transaction_isolation`).
            Scan(&isolation)).To(Succeed())
        g.Expect(database.QueryRow(ctx, `SHOW transaction_read_only`).
            Scan(&readOnly)).To(Succeed())
        g.Expect(isolation).To(Equal("repeatable read"))
        g.Expect(readOnly).To(Equal("on"))
        _, writeErr := database.Exec(ctx,
            `CREATE TABLE timestamp_read_only_write_must_fail (id integer)`)
        return writeErr
    })
    g.Expect(err).To(MatchError(ContainSubstring("read-only transaction")))
}
```

Run: `go test -p=1 ./internal/db -run TestRunReadOnlyTxRRUsesRepeatableReadAndRejectsWrites -count=1`

Expected before implementation: FAIL with `undefined: db.RunReadOnlyTxRR`.

- [ ] **Step 2: Implement transaction options and make Step 1 pass.**

```go
func RunTxOptions(
    ctx context.Context,
    options pgx.TxOptions,
    f func(context.Context) error,
) error {
    database := internalctx.GetDb(ctx)
    switch connection := database.(type) {
    case queryable.Conn:
        tx, err := connection.BeginEx(ctx, &options)
        if err != nil {
            return err
        }
        return runTxFunc(ctx, tx, f)
    case queryable.PoolConn:
        tx, err := connection.BeginTx(ctx, options)
        if err != nil {
            return err
        }
        return runTxFunc(ctx, tx, f)
    default:
        return errors.New(
            "RunTxOptions can not be called from within an existing transaction",
        )
    }
}

func RunReadOnlyTxRR(ctx context.Context, f func(context.Context) error) error {
    return RunTxOptions(ctx, pgx.TxOptions{
        IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly,
    }, f)
}

func RunTxIso(
    ctx context.Context,
    isoLevel pgx.TxIsoLevel,
    f func(context.Context) error,
) error {
    return RunTxOptions(ctx, pgx.TxOptions{IsoLevel: isoLevel}, f)
}
```

The type switch rejects nested transactions because `pgx.Tx` implements neither `queryable.Conn` nor `queryable.PoolConn`.

- [ ] **Step 3: Write and run failing complete-snapshot inspection tests.**

```go
manifest, err := db.InspectExternalExecutionTimestamps(ctx)
g.Expect(err).NotTo(HaveOccurred())
g.Expect(manifest.State).To(Equal(
    types.ExternalExecutionTimestampManifestStateDraft,
))
g.Expect(manifest.ExecutionCount).To(Equal(uint64(2)))
g.Expect(manifest.EventCount).To(Equal(uint64(1)))
g.Expect(manifest.RawCellCount).To(Equal(uint64(11)))
g.Expect(manifest.Cells).To(HaveLen(11))
for _, cell := range manifest.Cells {
    if cell.RawValue == nil {
        g.Expect(cell.Decision).To(Equal(
            types.ExternalExecutionTimestampDecisionNull,
        ))
    } else {
        g.Expect(*cell.RawValue).To(MatchRegexp(
            `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{6}$`,
        ))
    }
}
```

Run the same fixture under `UTC`, `Asia/Bangkok`, and `America/New_York` and require identical checksums. Add dirty, clean-136, clean-137, compatible-138, contracted-shape, null, and concurrent-update cases. The 137-to-138 test must retain the logical source version while checking the live catalog version separately:

```go
before, err := db.InspectExternalExecutionTimestamps(schema137Context)
g.Expect(err).NotTo(HaveOccurred())
g.Expect(before.SourceSchemaVersion).To(Equal(uint(137)))
migrateTestDatabaseTo(t, schema137Context, 138)
after, err := db.InspectExternalExecutionTimestamps(schema137Context)
g.Expect(err).NotTo(HaveOccurred())
g.Expect(after.SourceSchemaVersion).To(Equal(uint(137)))
g.Expect(after.DatabaseIdentityChecksum).To(Equal(
    before.DatabaseIdentityChecksum,
))
report, err := db.ValidateExternalExecutionTimestampManifest(
    schema137Context, *before,
)
g.Expect(err).NotTo(HaveOccurred())
g.Expect(report.SchemaVersion).To(Equal(uint(138)))
```

Run: `go test -p=1 ./internal/db -run TestInspectExternalExecutionTimestamps -count=1 -timeout 20m`

Expected before implementation: FAIL with `undefined: db.InspectExternalExecutionTimestamps`.

- [ ] **Step 4: Implement one-snapshot inspection and make Step 3 pass.**

Create `internal/db/external_execution_timestamps.go` with this import set, then add the complete queries below:

```go
package db

import (
    "context"
    "errors"
    "fmt"
    "time"

    "github.com/distr-sh/distr/internal/buildconfig"
    internalctx "github.com/distr-sh/distr/internal/context"
    "github.com/distr-sh/distr/internal/externalexecutiontimestamp"
    "github.com/distr-sh/distr/internal/types"
    "github.com/google/uuid"
    "github.com/jackc/pgx/v5"
)
```

```go
const externalExecutionTimestampCatalogSQL = `
WITH required_legacy(table_name, column_name, nullable) AS (
  VALUES
    ('externalexecution', 'created_at', false),
    ('externalexecution', 'updated_at', false),
    ('externalexecution', 'started_at', true),
    ('externalexecution', 'completed_at', true),
    ('externalexecution', 'callback_deadline_at', false),
    ('externalexecutionevent', 'created_at', false)
), required_shadow(table_name, column_name) AS (
  VALUES
    ('externalexecution', 'created_at_instant'),
    ('externalexecution', 'updated_at_instant'),
    ('externalexecution', 'started_at_instant'),
    ('externalexecution', 'completed_at_instant'),
    ('externalexecution', 'callback_deadline_at_instant'),
    ('externalexecutionevent', 'created_at_instant')
)
SELECT
  (SELECT count(*) FROM required_legacy required
   JOIN information_schema.columns column_row
     ON column_row.table_schema = current_schema()
    AND column_row.table_name = required.table_name
    AND column_row.column_name = required.column_name
    AND column_row.data_type = 'timestamp without time zone'
    AND (column_row.is_nullable = 'YES') = required.nullable),
  (SELECT count(*) FROM required_shadow required
   JOIN information_schema.columns column_row
     ON column_row.table_schema = current_schema()
    AND column_row.table_name = required.table_name
    AND column_row.column_name = required.column_name
    AND column_row.data_type = 'timestamp with time zone'
    AND column_row.is_nullable = 'YES'),
  to_regclass(format('%I.externalexecutiontimestampmanifest', current_schema())) IS NOT NULL,
  to_regclass(format('%I.externalexecutiontimestampcellprovenance', current_schema())) IS NOT NULL,
  to_regclass(format('%I.externalexecutiontimestampexpandstate', current_schema())) IS NOT NULL`

const externalExecutionTimestampRawCellsSQL = `
SELECT source_table, source_row_id, source_column, column_ordinal, raw_value
FROM (
  SELECT
    'externalexecution'::text AS source_table,
    execution.id AS source_row_id,
    cell.source_column,
    cell.column_ordinal,
    CASE WHEN cell.raw_value IS NULL THEN NULL ELSE
      to_char(cell.raw_value, 'YYYY-MM-DD"T"HH24:MI:SS.US')
    END AS raw_value
  FROM ExternalExecution execution
  CROSS JOIN LATERAL (VALUES
    ('created_at'::text, 1::smallint, execution.created_at),
    ('updated_at'::text, 2::smallint, execution.updated_at),
    ('started_at'::text, 3::smallint, execution.started_at),
    ('completed_at'::text, 4::smallint, execution.completed_at),
    ('callback_deadline_at'::text, 5::smallint,
      execution.callback_deadline_at)
  ) AS cell(source_column, column_ordinal, raw_value)
  UNION ALL
  SELECT
    'externalexecutionevent'::text,
    event.id,
    'created_at'::text,
    6::smallint,
    to_char(event.created_at, 'YYYY-MM-DD"T"HH24:MI:SS.US')
  FROM ExternalExecutionEvent event
) cells
ORDER BY source_table, source_row_id, column_ordinal`
```

Use these complete helpers. `CatalogVersion` is the actual migration version. `IdentitySourceVersion` remains 137 after migration 138 because it comes from the immutable transition row; this is what allows a manifest captured on 137 to validate after the additive schema migration.

```go
type externalExecutionTimestampSchemaContract struct {
    CatalogVersion uint
    IdentitySourceVersion uint
}

type externalExecutionTimestampRawCellRow struct {
    SourceTable string `db:"source_table"`
    SourceRowID uuid.UUID `db:"source_row_id"`
    SourceColumn string `db:"source_column"`
    ColumnOrdinal int16 `db:"column_ordinal"`
    RawValue *string `db:"raw_value"`
}

func readExternalExecutionTimestampSchemaContractInTx(
    ctx context.Context,
) (externalExecutionTimestampSchemaContract, error) {
    database := internalctx.GetDb(ctx)
    var versionTableExists bool
    if err := database.QueryRow(ctx, `
SELECT to_regclass(format('%I.schema_migrations', current_schema()))
       IS NOT NULL`).Scan(&versionTableExists); err != nil {
        return externalExecutionTimestampSchemaContract{}, err
    }
    if !versionTableExists {
        return externalExecutionTimestampSchemaContract{},
            errors.New("schema_migrations is absent")
    }
    var version int
    var dirty bool
    if err := database.QueryRow(ctx, `
SELECT version, dirty FROM schema_migrations LIMIT 1`).
        Scan(&version, &dirty); err != nil {
        return externalExecutionTimestampSchemaContract{}, err
    }
    if dirty {
        return externalExecutionTimestampSchemaContract{},
            fmt.Errorf("schema version %d is dirty", version)
    }
    var legacyCount, shadowCount int64
    var manifestTable, provenanceTable, expandStateTable bool
    if err := database.QueryRow(ctx, externalExecutionTimestampCatalogSQL).Scan(
        &legacyCount, &shadowCount, &manifestTable,
        &provenanceTable, &expandStateTable,
    ); err != nil {
        return externalExecutionTimestampSchemaContract{}, err
    }
    if legacyCount != 6 {
        return externalExecutionTimestampSchemaContract{},
            fmt.Errorf("legacy timestamp catalog has %d of 6 required columns", legacyCount)
    }
    if version == 137 {
        if shadowCount != 0 || manifestTable || provenanceTable || expandStateTable {
            return externalExecutionTimestampSchemaContract{},
                errors.New("schema 137 has unexpected expand objects")
        }
        return externalExecutionTimestampSchemaContract{
            CatalogVersion: 137, IdentitySourceVersion: 137,
        }, nil
    }
    if version < 138 || shadowCount != 6 ||
        !manifestTable || !provenanceTable || !expandStateTable {
        return externalExecutionTimestampSchemaContract{},
            fmt.Errorf("schema %d is not expand-compatible", version)
    }
    var sourceVersion uint
    var stateRows int64
    if err := database.QueryRow(ctx, `
SELECT count(*), COALESCE(min(source_schema_version), 0)
FROM ExternalExecutionTimestampExpandState`).
        Scan(&stateRows, &sourceVersion); err != nil {
        return externalExecutionTimestampSchemaContract{}, err
    }
    if stateRows != 1 || sourceVersion != 137 {
        return externalExecutionTimestampSchemaContract{},
            fmt.Errorf("expand state must contain one source version 137 row")
    }
    return externalExecutionTimestampSchemaContract{
        CatalogVersion: uint(version),
        IdentitySourceVersion: sourceVersion,
    }, nil
}

func inspectExternalExecutionTimestampsInTx(
    ctx context.Context,
) (*types.ExternalExecutionTimestampManifest, error) {
    database := internalctx.GetDb(ctx)
    contract, err := readExternalExecutionTimestampSchemaContractInTx(ctx)
    if err != nil {
        return nil, err
    }
    var startedAt time.Time
    if err := database.QueryRow(ctx, `SELECT clock_timestamp()`).
        Scan(&startedAt); err != nil {
        return nil, err
    }
    rows, err := database.Query(ctx, externalExecutionTimestampRawCellsSQL)
    if err != nil {
        return nil, err
    }
    rawRows, err := pgx.CollectRows(
        rows, pgx.RowToStructByName[externalExecutionTimestampRawCellRow],
    )
    if err != nil {
        return nil, err
    }
    var endedAt time.Time
    if err := database.QueryRow(ctx, `SELECT clock_timestamp()`).
        Scan(&endedAt); err != nil {
        return nil, err
    }

    rawCells := make([]types.ExternalExecutionTimestampRawCell, 0, len(rawRows))
    decisions := make([]types.ExternalExecutionTimestampCellDecision, 0, len(rawRows))
    executionIDs := make([]uuid.UUID, 0)
    eventIDs := make([]uuid.UUID, 0)
    seenExecutions := map[uuid.UUID]struct{}{}
    seenEvents := map[uuid.UUID]struct{}{}
    var populated uint64
    for _, row := range rawRows {
        raw := types.ExternalExecutionTimestampRawCell{
            SourceTable: row.SourceTable,
            SourceRowID: row.SourceRowID,
            SourceColumn: row.SourceColumn,
            ColumnOrdinal: uint8(row.ColumnOrdinal),
            RawValue: row.RawValue,
        }
        raw.RawCellChecksum, err =
            externalexecutiontimestamp.ComputeRawCellChecksum(raw)
        if err != nil {
            return nil, err
        }
        decision := types.ExternalExecutionTimestampDecisionUnresolved
        if raw.RawValue == nil {
            decision = types.ExternalExecutionTimestampDecisionNull
        } else {
            populated++
        }
        rawCells = append(rawCells, raw)
        decisions = append(decisions,
            types.ExternalExecutionTimestampCellDecision{
                ExternalExecutionTimestampRawCell: raw,
                Decision: decision,
                ConversionExpressionVersion:
                    externalexecutiontimestamp.ConversionExpressionVersion,
            },
        )
        if row.SourceTable == "externalexecution" {
            if _, exists := seenExecutions[row.SourceRowID]; !exists {
                seenExecutions[row.SourceRowID] = struct{}{}
                executionIDs = append(executionIDs, row.SourceRowID)
            }
        } else if _, exists := seenEvents[row.SourceRowID]; !exists {
            seenEvents[row.SourceRowID] = struct{}{}
            eventIDs = append(eventIDs, row.SourceRowID)
        }
    }
    rawSetChecksum, err :=
        externalexecutiontimestamp.ComputeRawSetChecksum(rawCells)
    if err != nil {
        return nil, err
    }
    identity, err := externalexecutiontimestamp.ComputeDatabaseIdentityChecksum(
        contract.IdentitySourceVersion, executionIDs, eventIDs,
        uint64(len(rawCells)), rawSetChecksum,
    )
    if err != nil {
        return nil, err
    }
    manifest := &types.ExternalExecutionTimestampManifest{
        ID: uuid.New(),
        DatabaseIdentityChecksum: identity,
        SourceSchemaVersion: contract.IdentitySourceVersion,
        SnapshotStartedAt: externalexecutiontimestamp.FormatInstant(startedAt),
        SnapshotEndedAt: externalexecutiontimestamp.FormatInstant(endedAt),
        ExecutionCount: uint64(len(executionIDs)),
        EventCount: uint64(len(eventIDs)),
        RawCellCount: uint64(len(rawCells)),
        PopulatedCellCount: populated,
        RawCellChecksum: rawSetChecksum,
        ToolVersion: buildconfig.Version(),
        ConversionExpressionVersion:
            externalexecutiontimestamp.ConversionExpressionVersion,
        State: types.ExternalExecutionTimestampManifestStateDraft,
        Cells: decisions,
    }
    manifest.DecisionContentChecksum, err =
        externalexecutiontimestamp.ComputeDecisionContentChecksum(*manifest)
    if err != nil {
        return nil, err
    }
    return manifest, nil
}

func InspectExternalExecutionTimestamps(
    ctx context.Context,
) (manifest *types.ExternalExecutionTimestampManifest, finalErr error) {
    finalErr = RunReadOnlyTxRR(ctx, func(ctx context.Context) error {
        inspected, err := inspectExternalExecutionTimestampsInTx(ctx)
        manifest = inspected
        return err
    })
    return manifest, finalErr
}
```

Implement first-manifest validation against the live fenced snapshot with exact field and cell equality; approval decisions may differ, but raw keys/values/checksums may not. Superseding manifests use the retained verified-tip/provenance snapshot instead, as shown in the shared dry-run/apply preflight below. That preflight returns exactly the cell keys that still require shadow writes: all resolved cells for a root, but for a child only unchanged-raw `UNRESOLVED -> PROVEN|ATTESTED` promotions whose current shadow remains null. A promoted lifecycle cell whose current raw has already evolved as a valid paired write needs no historical shadow rewrite:

```go
func requireManifestMatchesSnapshot(
    manifest types.ExternalExecutionTimestampManifest,
    snapshot types.ExternalExecutionTimestampManifest,
) error {
    if manifest.SourceSchemaVersion != snapshot.SourceSchemaVersion ||
        manifest.ExecutionCount != snapshot.ExecutionCount ||
        manifest.EventCount != snapshot.EventCount ||
        manifest.RawCellCount != snapshot.RawCellCount ||
        manifest.PopulatedCellCount != snapshot.PopulatedCellCount ||
        manifest.RawCellChecksum != snapshot.RawCellChecksum ||
        manifest.DatabaseIdentityChecksum != snapshot.DatabaseIdentityChecksum {
        return errors.New("manifest does not match the current raw snapshot")
    }
    current := make(map[string]types.ExternalExecutionTimestampRawCell,
        len(snapshot.Cells))
    for _, cell := range snapshot.Cells {
        key := fmt.Sprintf("%s/%s/%s/%d", cell.SourceTable,
            cell.SourceRowID, cell.SourceColumn, cell.ColumnOrdinal)
        current[key] = cell.ExternalExecutionTimestampRawCell
    }
    for _, cell := range manifest.Cells {
        key := fmt.Sprintf("%s/%s/%s/%d", cell.SourceTable,
            cell.SourceRowID, cell.SourceColumn, cell.ColumnOrdinal)
        raw, exists := current[key]
        sameRaw := raw.RawValue == nil && cell.RawValue == nil
        if raw.RawValue != nil && cell.RawValue != nil {
            sameRaw = *raw.RawValue == *cell.RawValue
        }
        if !exists || !sameRaw || raw.RawCellChecksum != cell.RawCellChecksum {
            return fmt.Errorf("manifest raw cell %s does not match snapshot", key)
        }
        delete(current, key)
    }
    if len(current) != 0 {
        return errors.New("manifest omits current raw cells")
    }
    return nil
}

func validationReportFromManifest(
    manifest types.ExternalExecutionTimestampManifest,
    catalogVersion uint,
) *types.ExternalExecutionTimestampValidationReport {
    var unresolved uint64
    for _, cell := range manifest.Cells {
        if cell.Decision ==
            types.ExternalExecutionTimestampDecisionUnresolved {
            unresolved++
        }
    }
    return &types.ExternalExecutionTimestampValidationReport{
        ManifestID: manifest.ID,
        SchemaVersion: catalogVersion,
        ExecutionCount: manifest.ExecutionCount,
        EventCount: manifest.EventCount,
        RawCellCount: manifest.RawCellCount,
        PopulatedCellCount: manifest.PopulatedCellCount,
        UnresolvedCellCount: unresolved,
        RawSetChecksum: manifest.RawCellChecksum,
        DatabaseIdentityChecksum: manifest.DatabaseIdentityChecksum,
        DecisionContentChecksum: manifest.DecisionContentChecksum,
    }
}

func ValidateExternalExecutionTimestampManifest(
    ctx context.Context,
    manifest types.ExternalExecutionTimestampManifest,
) (report *types.ExternalExecutionTimestampValidationReport, finalErr error) {
    if problems := externalexecutiontimestamp.ValidateManifestDocument(manifest);
        len(problems) != 0 {
        return nil, errors.Join(problems...)
    }
    finalErr = RunReadOnlyTxRR(ctx, func(ctx context.Context) error {
        contract, err := readExternalExecutionTimestampSchemaContractInTx(ctx)
        if err != nil {
            return err
        }
        snapshot, err := inspectExternalExecutionTimestampsInTx(ctx)
        if err != nil {
            return err
        }
        if err := requireManifestMatchesSnapshot(manifest, *snapshot); err != nil {
            return err
        }
        report = validationReportFromManifest(
            manifest, contract.CatalogVersion,
        )
        return nil
    })
    return report, finalErr
}
```

- [ ] **Step 5: Write failing offline seal/file tests, then implement them.**

```go
sealed, err := externalexecutiontimestamp.SealManifest(
    reviewedDraft,
    types.ExternalExecutionTimestampSealOptions{
        AuthorIdentity: "release-author@example.invalid",
        ReviewerIdentity: "release-reviewer@example.invalid",
        EvidenceBundleReference: "evidence:bundle-42",
        EvidenceBundleChecksum: "sha256:" + strings.Repeat("a", 64),
        TargetReleaseCommit: strings.Repeat("b", 40),
        TargetImageDigest: "sha256:" + strings.Repeat("c", 64),
    },
    time.Date(2026, 7, 15, 3, 4, 5, 123456000, time.UTC),
)
g.Expect(err).NotTo(HaveOccurred())
g.Expect(sealed.State).To(Equal(
    types.ExternalExecutionTimestampManifestStateApproved,
))
g.Expect(sealed.ApprovedAt).To(Equal("2026-07-15T03:04:05.123456Z"))
```

Test equal identities, changed raw checksum, wrong conversion, unknown JSON, runtime-factory count zero during seal, create-new `0600`, no overwrite, stdin/stdout, and redaction. Implement `DisallowUnknownFields`, `os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)`, conversion recomputation, and all `Compute*` validations.

Run: `go test -p=1 ./internal/externalexecutiontimestamp ./cmd/hub/cmd -run 'TestSealManifest|TestExternalExecutionTimestampCommandSeal' -count=1`

Expected before implementation: FAIL; after implementation: PASS.

- [ ] **Step 6: Run the task gate and commit.**

```powershell
go test -p=1 ./internal/externalexecutiontimestamp ./internal/db ./cmd/hub/cmd -run 'Test(Inspect|Seal|Validate).*ExternalExecutionTimestamp|TestRunReadOnlyTxRR' -count=1 -timeout 20m
git diff --check
git add internal/db/tx.go internal/db/tx_test.go internal/db/external_execution_timestamps* cmd/hub/cmd/external_execution_timestamps* internal/externalexecutiontimestamp internal/types/external_execution_timestamp.go
git commit -m "feat: inspect and seal timestamp evidence"
```

## Task 5: Apply and Verify Evidence Without Rewriting History

**Files:**

- Modify: `internal/types/external_execution_timestamp.go`
- Modify: `internal/externalexecutiontimestamp/manifest.go`
- Modify: `internal/externalexecutiontimestamp/manifest_test.go`
- Modify: `internal/db/external_execution_timestamps.go`
- Modify: `internal/db/external_execution_timestamps_test.go`
- Modify: `cmd/hub/cmd/external_execution_timestamps.go`
- Modify: `cmd/hub/cmd/external_execution_timestamps_test.go`
- Modify: `internal/migrations/sql/138_external_execution_timestamp_expand.up.sql`
- Modify: `internal/migrations/sql/138_external_execution_timestamp_expand.down.sql`
- Modify: `internal/migrations/external_execution_timestamp_integration_test.go`

**Interfaces:**

- Consumes: Task 4 sealed complete manifests, Task 3 evidence tables/shadows, and Task 2's single exported `externalexecutiontimestamp.MigrationAdvisoryLockKey int64`. Task 5 never derives or hashes a lock key.
- Produces: dry-run/apply/standalone verification primitives and stable JSON count/checksum reports. Task 8 owns readiness, its startup wrapper, and its CLI.

Required interfaces:

```go
type ExternalExecutionTimestampApplyRequest struct {
    Manifest                   ExternalExecutionTimestampManifest
    Apply                      bool
    WriterFenceIdentifier      string
    BackupReference            string
    BackupChecksum             string
    RestoreVerificationReference string
    RestoreVerificationChecksum string
}

func ApplyExternalExecutionTimestampManifest(
    context.Context,
    types.ExternalExecutionTimestampApplyRequest,
) (*types.ExternalExecutionTimestampApplyReport, error)

func VerifyExternalExecutionTimestampManifest(
    context.Context,
    uuid.UUID,
) (*types.ExternalExecutionTimestampVerificationReport, error)

type ExternalExecutionTimestampApplyReport struct {
    ManifestID uuid.UUID `json:"manifestId"`
    DryRun, Idempotent bool
    ProvenCount, AttestedCount uint64
    UnresolvedCount, NullCount uint64
    ProvenanceRows, WouldPopulateCount uint64
    PopulatedShadowCount uint64
    RawSetChecksum, DatabaseIdentityChecksum string
}

type ExternalExecutionTimestampVerificationReport struct {
    ManifestID uuid.UUID `json:"manifestId"`
    SchemaVersion uint
    SourceExecutionCount, SourceEventCount uint64
    CurrentExecutionCount, CurrentEventCount uint64
    ProvenanceRows, ResolvedShadowCount uint64
    UnresolvedShadowCount, PostManifestPairedCount uint64
    RawSetChecksum, DecisionContentChecksum string
}

func verifyExternalExecutionTimestampManifestInTx(
    context.Context,
    uuid.UUID,
) (*types.ExternalExecutionTimestampVerificationReport, error)
```

Apply behavior is fixed:

- Omitted `--apply` is a read-only dry run and returns the exact would-insert/would-populate/unresolved counts on either exact schema 137 or the expand schema. Mutating apply requires schema 138 or later.
- Mutation requires non-empty writer-fence, backup, restore, checksum, target commit, and image-digest evidence.
- Acquire the shared timestamp advisory lock, then one `REPEATABLE READ, READ WRITE` transaction with bounded local timeouts and `LOCK TABLE ExternalExecution, ExternalExecutionEvent IN SHARE ROW EXCLUSIVE MODE`.
- For a first/root manifest, reproduce the live fenced raw snapshot and reject any count, UUID, raw value, raw checksum, or database identity mismatch.
- For a superseding manifest, require an exact decision revision of the verified tip's original schema-137 snapshot and separately run lifecycle-evolution verification against the current database/provenance/shadow pairs; never substitute live lifecycle values into manifest cells.
- Require exactly one decision for every expected cell and no extra decision.
- Insert one complete manifest and every immutable provenance row.
- Use six static allowlisted update statements. Fill only a null shadow for `PROVEN` or `ATTESTED`; reject a non-null different value; leave `UNRESOLVED` and `NULL_VALUE` null.
- Reproduce conversions and all invariants inside the same transaction, then advance `APPROVED -> APPLIED -> VERIFIED`. A failed check rolls back manifest, provenance, and shadows together.
- Reapplying the exact verified manifest is a verified no-op. A checksum collision or changed content aborts.
- A superseding complete manifest may be approved after writers resume and may fill a previously unresolved null shadow, but it preserves the original fenced raw snapshot, may never change an existing instant or mutate prior provenance, and does not absorb later live lifecycle values.
- Under `externalexecutiontimestamp.MigrationAdvisoryLockKey`, a first manifest must have `SupersedesManifestID == nil`; every later manifest must name the one unique current `VERIFIED` tip. A revoked-before-apply child is ignored. A missing tip, multiple tips, skipped ancestor, second active root, or fork is rejected before any insert.
- Standalone `verify` uses `REPEATABLE READ, READ ONLY` and does not update lifecycle state. It requires execution `created_at`, `callback_deadline_at`, and every event `created_at` to remain byte-equal to provenance. For `updated_at`, a changed raw value is valid only when it is an exact UTC pair with a non-null shadow at or after the root manifest's `verified_at`. For `started_at`/`completed_at`, an originally non-null raw value remains immutable; an original null may remain a null pair or transition once to such a pair at or after the root verification instant. Every revision in one chain uses that stable root baseline, so applying a later child cannot invalidate lifecycle evolution that was valid before the child was approved. An unchanged unresolved raw value must retain a null shadow. This permits legitimate post-expand lifecycle writes without reinterpreting old history.
- Migration 138 durably enforces the `started_at`/`completed_at` one-shot rule with a `BEFORE UPDATE OF` trigger. A historical non-null raw/null-shadow pair may remain unresolved or receive exactly one non-null shadow without changing raw; Task 5 provenance/apply verification proves the approved conversion, including nonzero source offsets. A null/null pair may remain null or transition once to an exact UTC raw/instant pair. Once a shadow is non-null, both fields are immutable; equal-value `COALESCE` updates remain valid. The down migration removes this trigger/function before removing shadows. Integration tests cover both lifecycle columns, first transition, second-transition rejection, nonzero-offset historical shadow fill, and Task 7-shaped no-op updates.

CLI surface:

```text
distr external-execution-timestamps apply --manifest approved.json
distr external-execution-timestamps apply --manifest approved.json --apply \
  --writer-fence-id "$DISTR_TIMESTAMP_FENCE_ID" \
  --backup-reference "$DISTR_TIMESTAMP_BACKUP_REFERENCE" \
  --backup-checksum "$DISTR_TIMESTAMP_BACKUP_CHECKSUM" \
  --restore-reference "$DISTR_TIMESTAMP_RESTORE_REFERENCE" \
  --restore-checksum "$DISTR_TIMESTAMP_RESTORE_CHECKSUM"
distr external-execution-timestamps verify --manifest-id "$DISTR_TIMESTAMP_MANIFEST_ID"
```

- [ ] **Step 1: Create the five-execution fixture and write the failing dry-run tests.**

```go
statuses := []types.ExternalExecutionStatus{
    types.ExternalExecutionStatusSucceeded,
    types.ExternalExecutionStatusSucceeded,
    types.ExternalExecutionStatusSucceeded,
    types.ExternalExecutionStatusSucceeded,
    types.ExternalExecutionStatusTimedOut,
}
fixture := createFiveExecutionTimestampFixture(t, ctx, statuses)
g.Expect(fixture.Manifest.ExecutionCount).To(Equal(uint64(5)))
g.Expect(fixture.Manifest.EventCount).To(Equal(uint64(5)))
g.Expect(fixture.Manifest.RawCellCount).To(Equal(uint64(30)))

report, err := db.ApplyExternalExecutionTimestampManifest(ctx,
    types.ExternalExecutionTimestampApplyRequest{
        Manifest: fixture.Manifest, Apply: false,
    })
g.Expect(err).NotTo(HaveOccurred())
g.Expect(report.WouldPopulateCount).To(Equal(uint64(18)))
g.Expect(report.UnresolvedCount).To(Equal(uint64(12)))
g.Expect(countLedgerAndShadowWrites(t, ctx)).To(Equal(uint64(0)))
```

```go
func TestApplyExternalExecutionTimestampManifestDryRunSchema137WithoutExpandTables(
    t *testing.T,
) {
    ctx := arrangeExactSchema137FiveExecutionFixture(t)
    manifest := inspectSchema137ManifestFixture(t, ctx)
    g := NewWithT(t)
    g.Expect(timestampExpandTablesExist(t, ctx)).To(BeFalse())

    report, err := db.ApplyExternalExecutionTimestampManifest(ctx,
        types.ExternalExecutionTimestampApplyRequest{
            Manifest: manifest, Apply: false,
        })

    g.Expect(err).NotTo(HaveOccurred())
    g.Expect(report.DryRun).To(BeTrue())
    g.Expect(report.WouldPopulateCount).To(Equal(
        resolvedDecisionCount(manifest),
    ))
    g.Expect(timestampExpandTablesExist(t, ctx)).To(BeFalse())
}
```

Run: `go test -p=1 ./internal/db -run TestApplyExternalExecutionTimestampManifestDryRun -count=1 -timeout 20m`

Expected before implementation: FAIL with `undefined: db.ApplyExternalExecutionTimestampManifest`. If the initial implementation queries the verified-tip/manifest tables before checking the schema contract, the schema-137 regression fails with `relation "externalexecutiontimestampmanifest" does not exist`.

- [ ] **Step 2: Implement read-only dry run and make Step 1 pass.**

Implement the dry-run branch with no advisory lock, table lock, or write:

```go
func dryRunExternalExecutionTimestampManifest(
    ctx context.Context,
    manifest types.ExternalExecutionTimestampManifest,
) (report *types.ExternalExecutionTimestampApplyReport, finalErr error) {
    if problems := externalexecutiontimestamp.ValidateManifestDocument(manifest);
        len(problems) != 0 {
        return nil, errors.Join(problems...)
    }
    finalErr = RunReadOnlyTxRR(ctx, func(ctx context.Context) error {
        cellsToPopulate, err := requireManifestSnapshotForApplyInTx(
            ctx, manifest,
        )
        if err != nil {
            return err
        }
        report = externalExecutionTimestampApplyReport(manifest, true, false)
        report.WouldPopulateCount = uint64(len(cellsToPopulate))
        return nil
    })
    return report, finalErr
}
```

- [ ] **Step 3: Write failing atomic apply and rollback tests.**

```go
request := types.ExternalExecutionTimestampApplyRequest{
    Manifest: fixture.Manifest, Apply: true,
    WriterFenceIdentifier: "fence:fixture-42",
    BackupReference: "backup:postgres-42",
    BackupChecksum: "sha256:" + strings.Repeat("d", 64),
    RestoreVerificationReference: "restore:fixture-42",
    RestoreVerificationChecksum: "sha256:" + strings.Repeat("e", 64),
}
report, err := db.ApplyExternalExecutionTimestampManifest(ctx, request)
g.Expect(err).NotTo(HaveOccurred())
g.Expect(report.ProvenanceRows).To(Equal(uint64(30)))
g.Expect(report.PopulatedShadowCount).To(Equal(uint64(18)))
g.Expect(readNonTimestampSnapshot(t, ctx)).To(Equal(
    fixture.NonTimestampSnapshot,
))

cases := []struct {
    name string
    mutate func(*types.ExternalExecutionTimestampApplyRequest, context.Context)
}{
    {"raw changed", mutateOneRawWallValue},
    {"missing decision", removeOneDecision},
    {"extra decision", appendUnexpectedDecision},
    {"wrong conversion", changeOneConvertedInstant},
    {"missing fence", clearWriterFence},
    {"missing backup", clearBackupEvidence},
    {"missing restore", clearRestoreEvidence},
    {"conflicting shadow", populateDifferentShadow},
    {"concurrent writer committed while apply waits", insertConcurrentEventInOpenTx},
    {"schema 138 recomputed as catalog version", useCatalogVersionInIdentity},
    {"second active root", clearSupersedesWithExistingTip},
    {"forked verified tip", supersedeVerifiedAncestorInsteadOfTip},
}
```

Each negative case requires zero manifest/provenance/shadow writes after rollback. Coordinate the concurrent-writer case by observing the apply session waiting for the `ExternalExecution`/`ExternalExecutionEvent` table lock in `pg_stat_activity`, then commit the writer; do not use timing sleeps. Add a late transactional failure with a test-only trigger that rejects the `APPLIED -> VERIFIED` transition after manifest, provenance, and shadow mutations, then prove every mutation and unrelated business field rolled back.

Run: `go test -p=1 ./internal/db -run 'TestApplyExternalExecutionTimestampManifest(Atomic|RollsBack)' -count=1 -timeout 20m`

Expected before implementation: FAIL.

- [ ] **Step 4: Implement atomic apply and make Step 3 pass.**

Add `regexp` and `strings` to the existing `internal/db/external_execution_timestamps.go` import block.

Add the exact SQL constants below. Both statements accept only Task 2's exported key; Task 5 contains no hash algorithm or lock-name literal.

```go
const externalExecutionTimestampTrySessionLockSQL =
    `SELECT pg_try_advisory_lock(@migrationAdvisoryLockKey)`
const externalExecutionTimestampUnlockSessionSQL =
    `SELECT pg_advisory_unlock(@migrationAdvisoryLockKey)`

const insertExternalExecutionTimestampManifestSQL = `
INSERT INTO ExternalExecutionTimestampManifest (
  id, supersedes_manifest_id, database_identity_checksum,
  source_schema_version, snapshot_started_at, snapshot_ended_at,
  execution_count, event_count, raw_cell_count, populated_cell_count,
  raw_cell_checksum, evidence_bundle_reference, evidence_bundle_checksum,
  tool_version, conversion_expression_version, author_identity,
  reviewer_identity, approved_at, target_release_commit,
  target_image_digest, state, decision_content_checksum
) VALUES (
  @id, @supersedesManifestId, @databaseIdentityChecksum,
  @sourceSchemaVersion, CAST(@snapshotStartedAt AS timestamptz),
  CAST(@snapshotEndedAt AS timestamptz), @executionCount, @eventCount,
  @rawCellCount, @populatedCellCount, @rawCellChecksum,
  @evidenceBundleReference, @evidenceBundleChecksum, @toolVersion,
  @conversionExpressionVersion, @authorIdentity, @reviewerIdentity,
  CAST(@approvedAt AS timestamptz), @targetReleaseCommit,
  @targetImageDigest, 'APPROVED', @decisionContentChecksum
)`

const insertExternalExecutionTimestampProvenanceSQL = `
INSERT INTO ExternalExecutionTimestampCellProvenance (
  manifest_id, source_table, source_row_id, source_column,
  column_ordinal, raw_value, raw_is_null, decision, source_zone,
  source_offset_seconds, converted_value, evidence_reference,
  evidence_checksum, approving_identity, raw_cell_checksum,
  parent_manifest_checksum, conversion_expression_version
) VALUES (
  @manifestId, @sourceTable, @sourceRowId, @sourceColumn,
  @columnOrdinal, CAST(@rawValue AS timestamp without time zone),
  @rawIsNull, @decision, @sourceZone, @sourceOffsetSeconds,
  CAST(@convertedValue AS timestamptz), @evidenceReference,
  @evidenceChecksum, @approvingIdentity, @rawCellChecksum,
  @parentManifestChecksum, @conversionExpressionVersion
)`

const updateExecutionCreatedInstantSQL = `
UPDATE ExternalExecution SET created_at_instant=CAST(@converted AS timestamptz)
WHERE id=@rowId AND created_at IS NOT DISTINCT FROM
  CAST(@raw AS timestamp without time zone)
AND (created_at_instant IS NULL OR
     created_at_instant=CAST(@converted AS timestamptz))`
const updateExecutionUpdatedInstantSQL = `
UPDATE ExternalExecution SET updated_at_instant=CAST(@converted AS timestamptz)
WHERE id=@rowId AND updated_at IS NOT DISTINCT FROM
  CAST(@raw AS timestamp without time zone)
AND (updated_at_instant IS NULL OR
     updated_at_instant=CAST(@converted AS timestamptz))`
const updateExecutionStartedInstantSQL = `
UPDATE ExternalExecution SET started_at_instant=CAST(@converted AS timestamptz)
WHERE id=@rowId AND started_at IS NOT DISTINCT FROM
  CAST(@raw AS timestamp without time zone)
AND (started_at_instant IS NULL OR
     started_at_instant=CAST(@converted AS timestamptz))`
const updateExecutionCompletedInstantSQL = `
UPDATE ExternalExecution SET completed_at_instant=CAST(@converted AS timestamptz)
WHERE id=@rowId AND completed_at IS NOT DISTINCT FROM
  CAST(@raw AS timestamp without time zone)
AND (completed_at_instant IS NULL OR
     completed_at_instant=CAST(@converted AS timestamptz))`
const updateExecutionDeadlineInstantSQL = `
UPDATE ExternalExecution
SET callback_deadline_at_instant=CAST(@converted AS timestamptz)
WHERE id=@rowId AND callback_deadline_at IS NOT DISTINCT FROM
  CAST(@raw AS timestamp without time zone)
AND (callback_deadline_at_instant IS NULL OR
     callback_deadline_at_instant=CAST(@converted AS timestamptz))`
const updateEventCreatedInstantSQL = `
UPDATE ExternalExecutionEvent
SET created_at_instant=CAST(@converted AS timestamptz)
WHERE id=@rowId AND created_at IS NOT DISTINCT FROM
  CAST(@raw AS timestamp without time zone)
AND (created_at_instant IS NULL OR
     created_at_instant=CAST(@converted AS timestamptz))`
```

Use a closed switch and require one affected row for every resolved cell that the shared preflight scheduled for population. A child never replays already-resolved cells:

```go
func timestampShadowUpdateSQL(
    cell types.ExternalExecutionTimestampCellDecision,
) (string, error) {
    switch cell.SourceTable + "/" + cell.SourceColumn {
    case "externalexecution/created_at":
        return updateExecutionCreatedInstantSQL, nil
    case "externalexecution/updated_at":
        return updateExecutionUpdatedInstantSQL, nil
    case "externalexecution/started_at":
        return updateExecutionStartedInstantSQL, nil
    case "externalexecution/completed_at":
        return updateExecutionCompletedInstantSQL, nil
    case "externalexecution/callback_deadline_at":
        return updateExecutionDeadlineInstantSQL, nil
    case "externalexecutionevent/created_at":
        return updateEventCreatedInstantSQL, nil
    default:
        return "", fmt.Errorf("timestamp cell is outside the update allowlist")
    }
}

func applyTimestampShadowInTx(
    ctx context.Context,
    cell types.ExternalExecutionTimestampCellDecision,
) error {
    if cell.Decision != types.ExternalExecutionTimestampDecisionProven &&
        cell.Decision != types.ExternalExecutionTimestampDecisionAttested {
        return nil
    }
    statement, err := timestampShadowUpdateSQL(cell)
    if err != nil {
        return err
    }
    result, err := internalctx.GetDb(ctx).Exec(ctx, statement, pgx.NamedArgs{
        "rowId": cell.SourceRowID,
        "raw": *cell.RawValue,
        "converted": *cell.ConvertedValue,
    })
    if err != nil {
        return err
    }
    if result.RowsAffected() != 1 {
        return fmt.Errorf("resolved timestamp update affected %d rows",
            result.RowsAffected())
    }
    return nil
}
```

Define and use one deterministic chain-tip query. The Task 3 partial unique index prevents active roots/forks at insertion; this query additionally rejects a malformed catalog before apply:

```go
const externalExecutionTimestampVerifiedTipsSQL = `
SELECT manifest.id
FROM ExternalExecutionTimestampManifest manifest
WHERE manifest.state = 'VERIFIED'
  AND NOT EXISTS (
    SELECT 1 FROM ExternalExecutionTimestampManifest child
    WHERE child.supersedes_manifest_id = manifest.id
      AND child.state <> 'REVOKED_BEFORE_APPLY'
  )
ORDER BY manifest.verified_at DESC, manifest.id`

func readUniqueVerifiedManifestTipInTx(
    ctx context.Context,
) (*uuid.UUID, error) {
    database := internalctx.GetDb(ctx)
    var incomplete int64
    if err := database.QueryRow(ctx, `
SELECT count(*) FROM ExternalExecutionTimestampManifest
WHERE state IN ('DRAFT','APPROVED','APPLIED')`).Scan(&incomplete); err != nil {
        return nil, err
    }
    if incomplete != 0 {
        return nil, fmt.Errorf("found %d incomplete manifests", incomplete)
    }
    rows, err := database.Query(ctx,
        externalExecutionTimestampVerifiedTipsSQL)
    if err != nil {
        return nil, err
    }
    tips, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
    if err != nil {
        return nil, err
    }
    if len(tips) > 1 {
        return nil, fmt.Errorf("found %d verified manifest tips", len(tips))
    }
    if len(tips) == 0 {
        return nil, nil
    }
    return &tips[0], nil
}

func requireManifestSnapshotForApplyInTx(
    ctx context.Context,
    manifest types.ExternalExecutionTimestampManifest,
) (map[string]struct{}, error) {
    contract, err := readExternalExecutionTimestampSchemaContractInTx(ctx)
    if err != nil {
        return nil, err
    }
    if contract.CatalogVersion == 137 {
        if manifest.SupersedesManifestID != nil {
            return nil, errors.New(
                "schema-137 dry-run manifest cannot supersede another manifest",
            )
        }
        liveSnapshot, err := inspectExternalExecutionTimestampsInTx(ctx)
        if err != nil {
            return nil, err
        }
        if err := requireManifestMatchesSnapshot(
            manifest, *liveSnapshot,
        ); err != nil {
            return nil, err
        }
        return resolvedTimestampCellKeys(manifest), nil
    }

    // The schema-contract read above proves that every non-137 version here is
    // expand-compatible and that manifest/provenance/state tables exist before
    // any verified-tip query executes.
    tip, err := readUniqueVerifiedManifestTipInTx(ctx)
    if err != nil {
        return nil, err
    }
    if tip == nil {
        if manifest.SupersedesManifestID != nil {
            return nil, errors.New("first manifest cannot supersede another manifest")
        }
        liveSnapshot, err := inspectExternalExecutionTimestampsInTx(ctx)
        if err != nil {
            return nil, err
        }
        if err := requireManifestMatchesSnapshot(
            manifest, *liveSnapshot,
        ); err != nil {
            return nil, err
        }
        return resolvedTimestampCellKeys(manifest), nil
    }
    if manifest.SupersedesManifestID == nil ||
        *manifest.SupersedesManifestID != *tip {
        return nil, fmt.Errorf("manifest must supersede verified tip %s", *tip)
    }
    previous, _, err := readStoredExternalExecutionTimestampManifestInTx(
        ctx, *tip,
    )
    if err != nil {
        return nil, err
    }
    if problems := externalexecutiontimestamp.ValidateSupersession(
        *previous, manifest,
    ); len(problems) != 0 {
        return nil, errors.Join(problems...)
    }
    if _, err := verifyExternalExecutionTimestampManifestInTx(
        ctx, *tip,
    ); err != nil {
        return nil, fmt.Errorf(
            "live lifecycle verification against verified tip: %w", err,
        )
    }
    return newlyPromotedTimestampCellsRequiringShadowFillInTx(
        ctx, *previous, manifest,
    )
}
```

The schema-contract read is the first snapshot-bearing database read after the dedicated session has acquired the advisory lock and the transaction has acquired both table locks. Exact schema 137 rejects a child and validates a root directly against the live fenced snapshot; it never calls the verified-tip or stored-manifest readers because those relations do not exist yet. Only a validated schema 138-or-later expand shape may enter the manifest-chain path.

`resolvedTimestampCellKeys` returns every root `PROVEN`/`ATTESTED` key. `newlyPromotedTimestampCellsRequiringShadowFillInTx` compares the previous and next documents by canonical cell key and considers only `UNRESOLVED -> PROVEN|ATTESTED` promotions. It returns a key only when the current raw value is still byte-equal to the retained raw value and the current shadow is null. If the raw value has evolved, the preceding verified-tip lifecycle check must already have proved a permitted exact pair, and no shadow update is scheduled. Any missing current row, same-raw non-null shadow, invalid pair, or other state aborts. Thus dry-run and mutating apply share one root-versus-superseding decision and the same exact would-populate set.

Implement the transaction exactly as follows. `nullableTimestampEvidence` returns `nil` for an empty string and the original string otherwise. `requireApplyEvidence` requires every apply-request evidence field, manifest target commit/digest, and `APPROVED` state before lock acquisition.

`runExternalExecutionTimestampApplyTransaction` acquires one pinned `pgxpool.Conn`, polls `pg_try_advisory_lock` every 100ms with a bounded ten-second context, and only after success begins the repeatable-read/read-write transaction. Its callback sets local timeouts and takes both table locks before any snapshot-bearing read. Cleanup uses `context.WithoutCancel` plus a short independent timeout and requires `pg_advisory_unlock` to return `true`. A failed/uncertain acquisition, transaction state, or unlock hijacks and closes the connection so no session lock can return to the pool. Tests cover bounded contention/cancellation and inspect every pooled session for lock leakage.

```go
func ApplyExternalExecutionTimestampManifest(
    ctx context.Context,
    request types.ExternalExecutionTimestampApplyRequest,
) (report *types.ExternalExecutionTimestampApplyReport, finalErr error) {
    if !request.Apply {
        return dryRunExternalExecutionTimestampManifest(ctx, request.Manifest)
    }
    if err := requireApplyEvidence(request); err != nil {
        return nil, err
    }
    finalErr = runExternalExecutionTimestampApplyTransaction(
      ctx, func(ctx context.Context) error {
        database := internalctx.GetDb(ctx)
        for _, statement := range []string{
            `SET LOCAL statement_timeout = '5min'`,
            `SET LOCAL lock_timeout = '10s'`,
        } {
            if _, err := database.Exec(ctx, statement); err != nil {
                return err
            }
        }
        if _, err := database.Exec(ctx, `
LOCK TABLE ExternalExecution, ExternalExecutionEvent
IN SHARE ROW EXCLUSIVE MODE`); err != nil {
            return err
        }
        contract, err := readExternalExecutionTimestampSchemaContractInTx(ctx)
        if err != nil {
            return err
        }
        if contract.CatalogVersion < 138 ||
            request.Manifest.SourceSchemaVersion !=
                contract.IdentitySourceVersion {
            return errors.New("mutating apply requires compatible schema 138")
        }
        existing, state, err :=
            readStoredExternalExecutionTimestampManifestInTx(
                ctx, request.Manifest.ID,
            )
        if err == nil {
            if state != types.ExternalExecutionTimestampManifestStateVerified ||
                existing.DecisionContentChecksum !=
                    request.Manifest.DecisionContentChecksum {
                return errors.New("manifest id collision")
            }
            verified, err := verifyExternalExecutionTimestampManifestInTx(
                ctx, request.Manifest.ID,
            )
            if err != nil {
                return err
            }
            report = externalExecutionTimestampApplyReport(
                request.Manifest, false, true,
            )
            report.WouldPopulateCount = 0
            report.PopulatedShadowCount = 0
            report.ProvenanceRows = verified.ProvenanceRows
            return nil
        }
        if !errors.Is(err, pgx.ErrNoRows) {
            return err
        }
        cellsToPopulate, err := requireManifestSnapshotForApplyInTx(
            ctx, request.Manifest,
        )
        if err != nil {
            return err
        }
        if _, err := database.Exec(ctx,
            insertExternalExecutionTimestampManifestSQL,
            manifestInsertArgs(request.Manifest),
        ); err != nil {
            return err
        }
        for _, cell := range request.Manifest.Cells {
            if _, err := database.Exec(ctx,
                insertExternalExecutionTimestampProvenanceSQL,
                provenanceInsertArgs(request.Manifest, cell),
            ); err != nil {
                return err
            }
            key := timestampCellKey(cell.SourceTable, cell.SourceRowID,
                cell.SourceColumn, cell.ColumnOrdinal)
            if _, shouldPopulate := cellsToPopulate[key]; shouldPopulate {
                if err := applyTimestampShadowInTx(ctx, cell); err != nil {
                    return err
                }
            }
        }
        for _, transition := range []struct {
            statement string
            expectedState string
        }{
            {`UPDATE ExternalExecutionTimestampManifest
              SET state='APPLIED', applied_at=clock_timestamp()
              WHERE id=@id AND state='APPROVED'`, "APPLIED"},
            {`UPDATE ExternalExecutionTimestampManifest
              SET state='VERIFIED', verified_at=clock_timestamp()
              WHERE id=@id AND state='APPLIED'`, "VERIFIED"},
        } {
            result, err := database.Exec(ctx, transition.statement,
                pgx.NamedArgs{"id": request.Manifest.ID})
            if err != nil {
                return err
            }
            if result.RowsAffected() != 1 {
                return fmt.Errorf("could not advance manifest to %s",
                    transition.expectedState)
            }
        }
        verified, err := verifyExternalExecutionTimestampManifestInTx(
            ctx, request.Manifest.ID,
        )
        if err != nil {
            return err
        }
        report = externalExecutionTimestampApplyReport(
            request.Manifest, false, false,
        )
        report.WouldPopulateCount = uint64(len(cellsToPopulate))
        report.PopulatedShadowCount = uint64(len(cellsToPopulate))
        report.ProvenanceRows = verified.ProvenanceRows
        return nil
    })
    return report, finalErr
}
```

Add the exact argument/report helpers:

```go
var timestampSHA256Pattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func nullableTimestampText(value string) any {
    if strings.TrimSpace(value) == "" {
        return nil
    }
    return value
}

func nullableTimestampUUID(value *uuid.UUID) any {
    if value == nil {
        return nil
    }
    return *value
}

func requireApplyEvidence(
    request types.ExternalExecutionTimestampApplyRequest,
) error {
    if request.Manifest.State !=
        types.ExternalExecutionTimestampManifestStateApproved {
        return errors.New("mutating apply requires an APPROVED manifest")
    }
    if problems := externalexecutiontimestamp.ValidateManifestDocument(
        request.Manifest,
    ); len(problems) != 0 {
        return errors.Join(problems...)
    }
    for name, value := range map[string]string{
        "writer fence": request.WriterFenceIdentifier,
        "backup reference": request.BackupReference,
        "restore reference": request.RestoreVerificationReference,
    } {
        if strings.TrimSpace(value) == "" {
            return fmt.Errorf("%s is required", name)
        }
    }
    for name, value := range map[string]string{
        "backup checksum": request.BackupChecksum,
        "restore checksum": request.RestoreVerificationChecksum,
    } {
        if !timestampSHA256Pattern.MatchString(value) {
            return fmt.Errorf("%s must be canonical SHA-256", name)
        }
    }
    return nil
}

func manifestInsertArgs(
    manifest types.ExternalExecutionTimestampManifest,
) pgx.NamedArgs {
    return pgx.NamedArgs{
        "id": manifest.ID,
        "supersedesManifestId": nullableTimestampUUID(
            manifest.SupersedesManifestID,
        ),
        "databaseIdentityChecksum": manifest.DatabaseIdentityChecksum,
        "sourceSchemaVersion": manifest.SourceSchemaVersion,
        "snapshotStartedAt": manifest.SnapshotStartedAt,
        "snapshotEndedAt": manifest.SnapshotEndedAt,
        "executionCount": manifest.ExecutionCount,
        "eventCount": manifest.EventCount,
        "rawCellCount": manifest.RawCellCount,
        "populatedCellCount": manifest.PopulatedCellCount,
        "rawCellChecksum": manifest.RawCellChecksum,
        "evidenceBundleReference": manifest.EvidenceBundleReference,
        "evidenceBundleChecksum": manifest.EvidenceBundleChecksum,
        "toolVersion": manifest.ToolVersion,
        "conversionExpressionVersion": manifest.ConversionExpressionVersion,
        "authorIdentity": manifest.AuthorIdentity,
        "reviewerIdentity": manifest.ReviewerIdentity,
        "approvedAt": manifest.ApprovedAt,
        "targetReleaseCommit": manifest.TargetReleaseCommit,
        "targetImageDigest": manifest.TargetImageDigest,
        "decisionContentChecksum": manifest.DecisionContentChecksum,
    }
}

func provenanceInsertArgs(
    manifest types.ExternalExecutionTimestampManifest,
    cell types.ExternalExecutionTimestampCellDecision,
) pgx.NamedArgs {
    return pgx.NamedArgs{
        "manifestId": manifest.ID,
        "sourceTable": cell.SourceTable,
        "sourceRowId": cell.SourceRowID,
        "sourceColumn": cell.SourceColumn,
        "columnOrdinal": cell.ColumnOrdinal,
        "rawValue": cell.RawValue,
        "rawIsNull": cell.RawValue == nil,
        "decision": cell.Decision,
        "sourceZone": nullableTimestampText(cell.SourceZone),
        "sourceOffsetSeconds": cell.SourceOffsetSeconds,
        "convertedValue": cell.ConvertedValue,
        "evidenceReference": nullableTimestampText(cell.EvidenceReference),
        "evidenceChecksum": nullableTimestampText(cell.EvidenceChecksum),
        "approvingIdentity": nullableTimestampText(cell.ApprovingIdentity),
        "rawCellChecksum": cell.RawCellChecksum,
        "parentManifestChecksum": manifest.DecisionContentChecksum,
        "conversionExpressionVersion": cell.ConversionExpressionVersion,
    }
}

func externalExecutionTimestampApplyReport(
    manifest types.ExternalExecutionTimestampManifest,
    dryRun bool,
    idempotent bool,
) *types.ExternalExecutionTimestampApplyReport {
    report := &types.ExternalExecutionTimestampApplyReport{
        ManifestID: manifest.ID,
        DryRun: dryRun,
        Idempotent: idempotent,
        ProvenanceRows: manifest.RawCellCount,
        RawSetChecksum: manifest.RawCellChecksum,
        DatabaseIdentityChecksum: manifest.DatabaseIdentityChecksum,
    }
    for _, cell := range manifest.Cells {
        switch cell.Decision {
        case types.ExternalExecutionTimestampDecisionProven:
            report.ProvenCount++
            report.WouldPopulateCount++
        case types.ExternalExecutionTimestampDecisionAttested:
            report.AttestedCount++
            report.WouldPopulateCount++
        case types.ExternalExecutionTimestampDecisionUnresolved:
            report.UnresolvedCount++
        case types.ExternalExecutionTimestampDecisionNull:
            report.NullCount++
        }
    }
    if dryRun {
        report.ProvenanceRows = 0
    }
    return report
}
```

- [ ] **Step 5: Write failing idempotency/supersession/read-only verification tests.**

```go
tests := []struct {
    name string
    arrange func(context.Context)
    wantError string
}{
    {"exact reapply", arrangeExactVerifiedReapply, ""},
    {"checksum collision", arrangeChecksumCollision, "collision"},
    {"fill unresolved", arrangeSupersedingResolution, ""},
    {"rewrite resolved", arrangeSupersedingConflict, "resolved instant"},
    {"immutable drift", arrangeCreatedAtDrift, "immutable raw"},
    {"paired updated", arrangePairedUpdatedAt, ""},
    {"unpaired updated", arrangeUnpairedUpdatedAt, "updated_at pair"},
    {"null to paired started", arrangePairedStartedAt, ""},
    {"rewrite nonnull started", arrangeChangedNonnullStartedAt,
        "immutable lifecycle"},
    {"filled unresolved", arrangeFilledUnresolvedShadow,
        "unresolved shadow"},
    {"missing provenance", arrangeMissingProvenance, "provenance"},
    {"paired later row", arrangePairedPostSnapshotRow, ""},
    {"unpaired later row", arrangeUnpairedPostSnapshotRow,
        "post-manifest pair"},
    {"evolved updated then superseding promotion",
        arrangeEvolvedUpdatedThenSupersedingPromotion, ""},
}
```

The final case is an end-to-end regression: apply and verify a root, evolve one `updated_at` as an exact UTC-naive/instant pair, approve a child that preserves the root snapshot while promoting a different unchanged unresolved cell, and apply the child. Assert dry-run and apply count only that one newly promoted still-null shadow, the evolved `updated_at` pair is untouched, and standalone verification of the new tip succeeds using the root verification instant as the lifecycle baseline.

Run: `go test -p=1 ./internal/db -run 'Test(Verify|Superseding)ExternalExecutionTimestampManifest' -count=1 -timeout 20m`

Expected before implementation: FAIL.

- [ ] **Step 6: Implement standalone verification and make Step 5 pass.**

```go
const storedExternalExecutionTimestampManifestSQL = `
SELECT id, supersedes_manifest_id, database_identity_checksum,
  source_schema_version,
  to_char(snapshot_started_at AT TIME ZONE 'UTC',
    'YYYY-MM-DD"T"HH24:MI:SS.US') || 'Z' AS snapshot_started_at,
  to_char(snapshot_ended_at AT TIME ZONE 'UTC',
    'YYYY-MM-DD"T"HH24:MI:SS.US') || 'Z' AS snapshot_ended_at,
  execution_count, event_count, raw_cell_count, populated_cell_count,
  raw_cell_checksum, evidence_bundle_reference, evidence_bundle_checksum,
  tool_version, conversion_expression_version, author_identity,
  reviewer_identity,
  to_char(approved_at AT TIME ZONE 'UTC',
    'YYYY-MM-DD"T"HH24:MI:SS.US') || 'Z' AS approved_at,
  target_release_commit, target_image_digest, state,
  decision_content_checksum,
  to_char(verified_at AT TIME ZONE 'UTC',
    'YYYY-MM-DD"T"HH24:MI:SS.US') || 'Z' AS verified_at
FROM ExternalExecutionTimestampManifest WHERE id=@manifestId`

const storedExternalExecutionTimestampProvenanceSQL = `
SELECT source_table, source_row_id, source_column, column_ordinal,
  CASE WHEN raw_is_null THEN NULL ELSE
    to_char(raw_value, 'YYYY-MM-DD"T"HH24:MI:SS.US') END AS raw_value,
  decision, source_zone, source_offset_seconds,
  CASE WHEN converted_value IS NULL THEN NULL ELSE
    to_char(converted_value AT TIME ZONE 'UTC',
      'YYYY-MM-DD"T"HH24:MI:SS.US') || 'Z' END AS converted_value,
  evidence_reference, evidence_checksum, approving_identity,
  raw_cell_checksum, conversion_expression_version
FROM ExternalExecutionTimestampCellProvenance
WHERE manifest_id=@manifestId
ORDER BY source_table, source_row_id, column_ordinal`

const currentExternalExecutionTimestampCellsSQL = `
SELECT source_table, source_row_id, source_column, column_ordinal,
  CASE WHEN raw_value IS NULL THEN NULL ELSE
    to_char(raw_value, 'YYYY-MM-DD"T"HH24:MI:SS.US') END AS raw_value,
  CASE WHEN instant_value IS NULL THEN NULL ELSE
    to_char(instant_value AT TIME ZONE 'UTC',
      'YYYY-MM-DD"T"HH24:MI:SS.US') || 'Z' END AS instant_value,
  CASE WHEN row_created_instant IS NULL THEN NULL ELSE
    to_char(row_created_instant AT TIME ZONE 'UTC',
      'YYYY-MM-DD"T"HH24:MI:SS.US') || 'Z' END AS row_created_instant
FROM (
  SELECT 'externalexecution'::text AS source_table,
    execution.id AS source_row_id, cell.source_column,
    cell.column_ordinal, cell.raw_value, cell.instant_value,
    execution.created_at_instant AS row_created_instant
  FROM ExternalExecution execution
  CROSS JOIN LATERAL (VALUES
    ('created_at'::text, 1::smallint,
      execution.created_at, execution.created_at_instant),
    ('updated_at'::text, 2::smallint,
      execution.updated_at, execution.updated_at_instant),
    ('started_at'::text, 3::smallint,
      execution.started_at, execution.started_at_instant),
    ('completed_at'::text, 4::smallint,
      execution.completed_at, execution.completed_at_instant),
    ('callback_deadline_at'::text, 5::smallint,
      execution.callback_deadline_at,
      execution.callback_deadline_at_instant)
  ) AS cell(source_column, column_ordinal, raw_value, instant_value)
  UNION ALL
  SELECT 'externalexecutionevent'::text, event.id, 'created_at'::text,
    6::smallint, event.created_at, event.created_at_instant,
    event.created_at_instant
  FROM ExternalExecutionEvent event
) current_cells
ORDER BY source_table, source_row_id, column_ordinal`

type storedExternalExecutionTimestampManifestRow struct {
    ID uuid.UUID `db:"id"`
    SupersedesManifestID *uuid.UUID `db:"supersedes_manifest_id"`
    DatabaseIdentityChecksum string `db:"database_identity_checksum"`
    SourceSchemaVersion uint `db:"source_schema_version"`
    SnapshotStartedAt string `db:"snapshot_started_at"`
    SnapshotEndedAt string `db:"snapshot_ended_at"`
    ExecutionCount uint64 `db:"execution_count"`
    EventCount uint64 `db:"event_count"`
    RawCellCount uint64 `db:"raw_cell_count"`
    PopulatedCellCount uint64 `db:"populated_cell_count"`
    RawCellChecksum string `db:"raw_cell_checksum"`
    EvidenceBundleReference string `db:"evidence_bundle_reference"`
    EvidenceBundleChecksum string `db:"evidence_bundle_checksum"`
    ToolVersion string `db:"tool_version"`
    ConversionExpressionVersion string `db:"conversion_expression_version"`
    AuthorIdentity string `db:"author_identity"`
    ReviewerIdentity string `db:"reviewer_identity"`
    ApprovedAt string `db:"approved_at"`
    TargetReleaseCommit string `db:"target_release_commit"`
    TargetImageDigest string `db:"target_image_digest"`
    State types.ExternalExecutionTimestampManifestState `db:"state"`
    DecisionContentChecksum string `db:"decision_content_checksum"`
    VerifiedAt *string `db:"verified_at"`
}

type storedExternalExecutionTimestampProvenanceRow struct {
    SourceTable string `db:"source_table"`
    SourceRowID uuid.UUID `db:"source_row_id"`
    SourceColumn string `db:"source_column"`
    ColumnOrdinal int16 `db:"column_ordinal"`
    RawValue *string `db:"raw_value"`
    Decision types.ExternalExecutionTimestampDecision `db:"decision"`
    SourceZone *string `db:"source_zone"`
    SourceOffsetSeconds *int32 `db:"source_offset_seconds"`
    ConvertedValue *string `db:"converted_value"`
    EvidenceReference *string `db:"evidence_reference"`
    EvidenceChecksum *string `db:"evidence_checksum"`
    ApprovingIdentity *string `db:"approving_identity"`
    RawCellChecksum string `db:"raw_cell_checksum"`
    ConversionExpressionVersion string `db:"conversion_expression_version"`
}

type currentExternalExecutionTimestampCell struct {
    SourceTable string `db:"source_table"`
    SourceRowID uuid.UUID `db:"source_row_id"`
    SourceColumn string `db:"source_column"`
    ColumnOrdinal int16 `db:"column_ordinal"`
    RawValue *string `db:"raw_value"`
    InstantValue *string `db:"instant_value"`
    RowCreatedInstant *string `db:"row_created_instant"`
}

func dereferenceTimestampText(value *string) string {
    if value == nil {
        return ""
    }
    return *value
}

func readStoredExternalExecutionTimestampManifestInTx(
    ctx context.Context,
    manifestID uuid.UUID,
) (*types.ExternalExecutionTimestampManifest,
    types.ExternalExecutionTimestampManifestState, error) {
    database := internalctx.GetDb(ctx)
    rows, err := database.Query(ctx,
        storedExternalExecutionTimestampManifestSQL,
        pgx.NamedArgs{"manifestId": manifestID})
    if err != nil {
        return nil, "", err
    }
    stored, err := pgx.CollectExactlyOneRow(
        rows, pgx.RowToStructByName[storedExternalExecutionTimestampManifestRow],
    )
    if err != nil {
        return nil, "", err
    }
    cellRows, err := database.Query(ctx,
        storedExternalExecutionTimestampProvenanceSQL,
        pgx.NamedArgs{"manifestId": manifestID})
    if err != nil {
        return nil, "", err
    }
    provenance, err := pgx.CollectRows(cellRows,
        pgx.RowToStructByName[storedExternalExecutionTimestampProvenanceRow])
    if err != nil {
        return nil, "", err
    }
    manifest := &types.ExternalExecutionTimestampManifest{
        ID: stored.ID,
        SupersedesManifestID: stored.SupersedesManifestID,
        DatabaseIdentityChecksum: stored.DatabaseIdentityChecksum,
        SourceSchemaVersion: stored.SourceSchemaVersion,
        SnapshotStartedAt: stored.SnapshotStartedAt,
        SnapshotEndedAt: stored.SnapshotEndedAt,
        ExecutionCount: stored.ExecutionCount,
        EventCount: stored.EventCount,
        RawCellCount: stored.RawCellCount,
        PopulatedCellCount: stored.PopulatedCellCount,
        RawCellChecksum: stored.RawCellChecksum,
        EvidenceBundleReference: stored.EvidenceBundleReference,
        EvidenceBundleChecksum: stored.EvidenceBundleChecksum,
        ToolVersion: stored.ToolVersion,
        ConversionExpressionVersion: stored.ConversionExpressionVersion,
        AuthorIdentity: stored.AuthorIdentity,
        ReviewerIdentity: stored.ReviewerIdentity,
        ApprovedAt: stored.ApprovedAt,
        TargetReleaseCommit: stored.TargetReleaseCommit,
        TargetImageDigest: stored.TargetImageDigest,
        State: stored.State,
        DecisionContentChecksum: stored.DecisionContentChecksum,
        Cells: make([]types.ExternalExecutionTimestampCellDecision,
            0, len(provenance)),
    }
    for _, row := range provenance {
        manifest.Cells = append(manifest.Cells,
            types.ExternalExecutionTimestampCellDecision{
                ExternalExecutionTimestampRawCell:
                    types.ExternalExecutionTimestampRawCell{
                        SourceTable: row.SourceTable,
                        SourceRowID: row.SourceRowID,
                        SourceColumn: row.SourceColumn,
                        ColumnOrdinal: uint8(row.ColumnOrdinal),
                        RawValue: row.RawValue,
                        RawCellChecksum: row.RawCellChecksum,
                    },
                Decision: row.Decision,
                SourceZone: dereferenceTimestampText(row.SourceZone),
                SourceOffsetSeconds: row.SourceOffsetSeconds,
                ConvertedValue: row.ConvertedValue,
                EvidenceReference:
                    dereferenceTimestampText(row.EvidenceReference),
                EvidenceChecksum:
                    dereferenceTimestampText(row.EvidenceChecksum),
                ApprovingIdentity:
                    dereferenceTimestampText(row.ApprovingIdentity),
                ConversionExpressionVersion:
                    row.ConversionExpressionVersion,
            },
        )
    }
    return manifest, stored.State, nil
}

func timestampCellKey(table string, rowID uuid.UUID,
    column string, ordinal uint8) string {
    return fmt.Sprintf("%s/%s/%s/%d", table, rowID, column, ordinal)
}

func requireExactUTCTimestampPair(
    raw *string,
    instant *string,
) error {
    if raw == nil && instant == nil {
        return nil
    }
    if raw == nil || instant == nil {
        return errors.New("timestamp pair is incomplete")
    }
    expected, err := externalexecutiontimestamp.ConvertWallClock(*raw, 0)
    if err != nil {
        return err
    }
    if externalexecutiontimestamp.FormatInstant(expected) != *instant {
        return errors.New("timestamp pair does not represent one UTC instant")
    }
    return nil
}

func verifyHistoricalTimestampCell(
    provenance types.ExternalExecutionTimestampCellDecision,
    current currentExternalExecutionTimestampCell,
    lifecycleBaselineAt time.Time,
) error {
    sameRaw := provenance.RawValue == nil && current.RawValue == nil
    if provenance.RawValue != nil && current.RawValue != nil {
        sameRaw = *provenance.RawValue == *current.RawValue
    }
    expectedInstant := provenance.ConvertedValue
    if provenance.Decision ==
        types.ExternalExecutionTimestampDecisionUnresolved ||
        provenance.Decision ==
            types.ExternalExecutionTimestampDecisionNull {
        expectedInstant = nil
    }
    unchanged := func() error {
        if !sameRaw {
            return errors.New("immutable raw timestamp changed")
        }
        sameInstant := expectedInstant == nil && current.InstantValue == nil
        if expectedInstant != nil && current.InstantValue != nil {
            sameInstant = *expectedInstant == *current.InstantValue
        }
        if !sameInstant {
            return errors.New("historical shadow differs from provenance")
        }
        return nil
    }
    immutable := provenance.SourceTable == "externalexecutionevent" ||
        provenance.SourceColumn == "created_at" ||
        provenance.SourceColumn == "callback_deadline_at"
    if immutable || (provenance.SourceColumn == "started_at" &&
        provenance.RawValue != nil) ||
        (provenance.SourceColumn == "completed_at" &&
            provenance.RawValue != nil) {
        return unchanged()
    }
    if sameRaw {
        return unchanged()
    }
    if provenance.SourceColumn != "updated_at" &&
        provenance.SourceColumn != "started_at" &&
        provenance.SourceColumn != "completed_at" {
        return errors.New("unsupported lifecycle evolution")
    }
    if provenance.RawValue != nil &&
        provenance.SourceColumn != "updated_at" {
        return errors.New("non-null lifecycle history is immutable")
    }
    if err := requireExactUTCTimestampPair(
        current.RawValue, current.InstantValue,
    ); err != nil {
        return err
    }
    if current.InstantValue == nil {
        return errors.New("evolved lifecycle instant is absent")
    }
    instant, err := externalexecutiontimestamp.ParseInstant(
        *current.InstantValue,
    )
    if err != nil {
        return err
    }
    if instant.Before(lifecycleBaselineAt) {
        return errors.New("evolved lifecycle instant predates verification")
    }
    return nil
}

func readExternalExecutionTimestampLifecycleBaselineInTx(
    ctx context.Context,
    manifestID uuid.UUID,
) (time.Time, error) {
    currentID := manifestID
    seen := map[uuid.UUID]struct{}{}
    for {
        if _, duplicate := seen[currentID]; duplicate {
            return time.Time{}, errors.New("manifest chain contains a cycle")
        }
        seen[currentID] = struct{}{}
        var parentID *uuid.UUID
        var state types.ExternalExecutionTimestampManifestState
        var verifiedAt *time.Time
        if err := internalctx.GetDb(ctx).QueryRow(ctx, `
SELECT supersedes_manifest_id, state, verified_at
FROM ExternalExecutionTimestampManifest WHERE id=@id`,
            pgx.NamedArgs{"id": currentID},
        ).Scan(&parentID, &state, &verifiedAt); err != nil {
            return time.Time{}, err
        }
        if state != types.ExternalExecutionTimestampManifestStateVerified ||
            verifiedAt == nil {
            return time.Time{}, errors.New(
                "manifest lifecycle baseline must be VERIFIED",
            )
        }
        if parentID == nil {
            return verifiedAt.UTC(), nil
        }
        currentID = *parentID
    }
}

func verifyExternalExecutionTimestampManifestInTx(
    ctx context.Context,
    manifestID uuid.UUID,
) (*types.ExternalExecutionTimestampVerificationReport, error) {
    database := internalctx.GetDb(ctx)
    manifest, state, err :=
        readStoredExternalExecutionTimestampManifestInTx(ctx, manifestID)
    if err != nil {
        return nil, err
    }
    if state != types.ExternalExecutionTimestampManifestStateVerified {
        return nil, fmt.Errorf("manifest state must be VERIFIED")
    }
    if problems := externalexecutiontimestamp.ValidateManifestDocument(
        *manifest,
    ); len(problems) != 0 {
        return nil, errors.Join(problems...)
    }
    lifecycleBaselineAt, err :=
        readExternalExecutionTimestampLifecycleBaselineInTx(ctx, manifestID)
    if err != nil {
        return nil, err
    }
    rows, err := database.Query(ctx,
        currentExternalExecutionTimestampCellsSQL)
    if err != nil {
        return nil, err
    }
    currentRows, err := pgx.CollectRows(rows,
        pgx.RowToStructByName[currentExternalExecutionTimestampCell])
    if err != nil {
        return nil, err
    }
    current := make(map[string]currentExternalExecutionTimestampCell,
        len(currentRows))
    executionIDs := map[uuid.UUID]struct{}{}
    eventIDs := map[uuid.UUID]struct{}{}
    for _, cell := range currentRows {
        key := timestampCellKey(cell.SourceTable, cell.SourceRowID,
            cell.SourceColumn, uint8(cell.ColumnOrdinal))
        current[key] = cell
        if cell.SourceTable == "externalexecution" {
            executionIDs[cell.SourceRowID] = struct{}{}
        } else {
            eventIDs[cell.SourceRowID] = struct{}{}
        }
    }
    report := &types.ExternalExecutionTimestampVerificationReport{
        ManifestID: manifest.ID,
        SourceExecutionCount: manifest.ExecutionCount,
        SourceEventCount: manifest.EventCount,
        CurrentExecutionCount: uint64(len(executionIDs)),
        CurrentEventCount: uint64(len(eventIDs)),
        ProvenanceRows: uint64(len(manifest.Cells)),
        RawSetChecksum: manifest.RawCellChecksum,
        DecisionContentChecksum: manifest.DecisionContentChecksum,
    }
    contract, err := readExternalExecutionTimestampSchemaContractInTx(ctx)
    if err != nil {
        return nil, err
    }
    report.SchemaVersion = contract.CatalogVersion
    for _, provenance := range manifest.Cells {
        key := timestampCellKey(provenance.SourceTable,
            provenance.SourceRowID, provenance.SourceColumn,
            provenance.ColumnOrdinal)
        currentCell, exists := current[key]
        if !exists {
            return nil, fmt.Errorf("provenance cell %s has no source row", key)
        }
        if err := verifyHistoricalTimestampCell(
            provenance, currentCell, lifecycleBaselineAt,
        ); err != nil {
            return nil, fmt.Errorf("cell %s: %w", key, err)
        }
        if provenance.Decision ==
            types.ExternalExecutionTimestampDecisionProven ||
            provenance.Decision ==
                types.ExternalExecutionTimestampDecisionAttested {
            report.ResolvedShadowCount++
        } else if provenance.Decision ==
            types.ExternalExecutionTimestampDecisionUnresolved {
            report.UnresolvedShadowCount++
        }
        delete(current, key)
    }
    for key, cell := range current {
        if cell.RowCreatedInstant == nil {
            return nil, fmt.Errorf("post-manifest cell %s has no creation instant", key)
        }
        created, err := externalexecutiontimestamp.ParseInstant(
            *cell.RowCreatedInstant,
        )
        if err != nil || created.Before(lifecycleBaselineAt) {
            return nil, fmt.Errorf("post-manifest cell %s predates verification", key)
        }
        if err := requireExactUTCTimestampPair(
            cell.RawValue, cell.InstantValue,
        ); err != nil {
            return nil, fmt.Errorf("post-manifest cell %s: %w", key, err)
        }
        report.PostManifestPairedCount++
    }
    if report.ProvenanceRows != manifest.RawCellCount {
        return nil, errors.New("provenance row count is incomplete")
    }
    return report, nil
}

func VerifyExternalExecutionTimestampManifest(
    ctx context.Context,
    manifestID uuid.UUID,
) (report *types.ExternalExecutionTimestampVerificationReport, finalErr error) {
    finalErr = RunReadOnlyTxRR(ctx, func(ctx context.Context) error {
        verified, err := verifyExternalExecutionTimestampManifestInTx(
            ctx, manifestID,
        )
        report = verified
        return err
    })
    return report, finalErr
}
```

This single verifier is used by atomic apply, standalone `verify`, Task 8 startup, and the post-start operator check. Loading the stored document joins the latest manifest to its append-only provenance rows; `ValidateManifestDocument` therefore reproduces the original schema-137 execution/event IDs, counts, database identity, and raw-set checksum without comparing the manifest to mutable live columns. The verifier then requires every retained original row to remain present, evaluates permitted live evolution against the root manifest's stable verification instant, and validates every post-expand row as complete paired writes. No caller reimplements these snapshot or lifecycle rules.

- [ ] **Step 7: Test/implement CLI and run the task gate.**

```go
command.SetArgs([]string{
    "verify", "--manifest-id",
    "11111111-1111-4111-8111-111111111111",
})
g.Expect(command.Execute()).To(Succeed())
```

Operational examples use only the environment variables shown above; flags without `--apply` are rejected and reports never echo evidence/raw values.

```powershell
go test -p=1 ./internal/externalexecutiontimestamp ./internal/db ./cmd/hub/cmd -run 'Test(Apply|Verify|Supersed).*ExternalExecutionTimestamp|TestExternalExecutionTimestampFiveExecutionFixture' -count=1 -timeout 20m
git diff --check
git add internal/types/external_execution_timestamp.go internal/externalexecutiontimestamp internal/db/external_execution_timestamps* cmd/hub/cmd/external_execution_timestamps*
git commit -m "feat: apply verified timestamp provenance"
```

## Task 6: Make Migration Execution Context-Bounded and Fail-Closed

**Files:**

- Modify: `internal/migrations/migrate.go`
- Create: `internal/migrations/migrate_test.go`
- Create: `internal/migrations/migrate_integration_test.go`
- Create: `internal/migrations/preflight.go`
- Create: `internal/migrations/preflight_test.go`
- Modify: `cmd/hub/cmd/migrate.go`
- Create: `cmd/hub/cmd/migrate_test.go`
- Modify: `internal/svc/registry.go`

**Interfaces:**

- Consumes: Task 2 approved-manifest decoding and Task 5 read-only database validation plus the single exported `externalexecutiontimestamp.MigrationAdvisoryLockKey int64` used by both manifest application and schema migration.
- Produces: lazy read-only status/preflight and bounded mutation APIs used by `serve`, the CLI, and the Compose adapter.

Consume the shared key and pinned derivation test created by Task 2. Task 6 must not redeclare the constant or derive a database-side hash. The runtime never asks PostgreSQL to hash text, so Task 5 apply and Task 6 migration cannot drift across PostgreSQL versions or use different hash functions.

Refactor the embedded migrator around an owned runner:

```go
type SchemaStatus struct {
    Version int // -1 means no schema_migrations version
    Dirty   bool
}

type RunOptions struct {
    Down           bool
    Target         *uint
    CheckOnly      bool
    ExpandManifest *types.ExternalExecutionTimestampManifest
    LockTimeout    time.Duration
}

const DefaultMigrationLockTimeout = 10 * time.Second

type Runner struct { /* owns *sql.DB; constructs *migrate.Migrate only for mutation */ }

func Open(databaseURL string, log *zap.Logger) (*Runner, error)
func (r *Runner) Close() error
func (r *Runner) Status(context.Context) (SchemaStatus, error)
func (r *Runner) Run(context.Context, RunOptions) error
```

Keep `Up`, `Down`, and `Migrate` as thin context-aware compatibility functions. Do not expose `Force` from this release.

`Open` connects but does not call `postgres.WithInstance`, `iofs.New`, or any golang-migrate constructor. `Status` and every `CheckOnly` path query `to_regclass`, `pg_catalog`, and an existing `schema_migrations` table directly; an absent version table returns version `-1` without creating it. Only after a mutating run passes preflight does the runner read `current_schema()`, pass that exact value to `postgres.Config.SchemaName`, and construct golang-migrate. Production therefore uses its configured current schema, while integration tests isolate `schema_migrations` and application objects by setting a UUID schema in the connection `search_path`.

`Runner.Run` normalizes a zero `LockTimeout` to `DefaultMigrationLockTimeout` (ten seconds), rejects a negative duration, obtains a dedicated connection, and loops on parameterized `SELECT pg_try_advisory_lock($1)` with `externalexecutiontimestamp.MigrationAdvisoryLockKey` until context or that timeout. It holds the session lock through preflight and golang-migrate execution and releases the same key with `SELECT pg_advisory_unlock($1)` under `context.WithoutCancel`. Set golang-migrate `LockTimeout` to the normalized value and `postgres.Config.StatementTimeout` to five minutes. `GracefulStop` on context cancellation prevents a subsequent migration from starting but is not claimed to interrupt the current PostgreSQL statement. Migration SQL retains `SET LOCAL lock_timeout` and `statement_timeout`, and the Compose adapter wraps the real `docker compose` process with `timeout --signal=TERM --kill-after=15s 5m`; forced process exit closes the database connection so PostgreSQL cancels an in-flight statement.

Preflight rules:

- Dirty current schema: always refuse before invoking golang-migrate.
- Upward crossing of 138 with no `ExternalExecution` table: allow clean install.
- Upward crossing of 138 with both execution tables empty: allow the verified zero-history path.
- Upward crossing of 138 with data and current version below 137: refuse and require migration to exact 137 followed by inspection.
- Upward crossing of 138 with data at exact 137: require an approved manifest argument whose fresh database identity/raw checksum exactly matches the database.
- A manifest-assisted run must use explicit `--to 138`; it may not silently migrate to later versions.
- Downward crossing of 138: refuse before golang-migrate when any manifest is `APPLIED` or `VERIFIED`; allow before application.
- `--check` executes all applicable status/preflight checks without schema mutation.

Refactor Cobra to injected `RunE`:

```go
type MigrateOptions struct {
    Down             bool
    To               uint
    ToSet            bool
    Check             bool
    TimestampManifest string
    LockTimeout       time.Duration
}

func NewMigrateCommand() *cobra.Command
func newMigrateCommand(runtime migrateRuntime) *cobra.Command
func runMigrate(context.Context, MigrateOptions, migrateRuntime) error
```

Track `cmd.Flags().Changed("to")`; explicit `--to 0` must target zero instead of accidentally running `Up`. Add `--check` and `--external-execution-timestamp-manifest`; reject invalid flag combinations. Change `svc.New` migration calls to pass context.

- [ ] **Step 1: Write failing lazy status/check tests.**

```go
constructed := uint64(0)
runner.engineFactory = func(
    *sql.DB, *zap.Logger, string, time.Duration,
) (migrationEngine, error) {
    constructed++
    return &fakeMigrationEngine{}, nil
}
status, err := runner.Status(ctx)
g.Expect(err).NotTo(HaveOccurred())
g.Expect(status).To(Equal(migrations.SchemaStatus{
    Version: -1, Dirty: false,
}))
g.Expect(runner.Run(ctx,
    migrations.RunOptions{CheckOnly: true})).To(Succeed())
g.Expect(constructed).To(Equal(uint64(0)))
```

Before/after, require `to_regclass(format('%I.schema_migrations', current_schema())) IS NULL`.

Run: `go test -p=1 ./internal/migrations -run 'TestRunner(Status|CheckOnly)DoesNotConstructMigrator' -count=1`

Expected before implementation: FAIL with `undefined: migrations.Runner`.

- [ ] **Step 2: Implement lazy `Open`/`Status` and make Step 1 pass.**

```go
func Open(databaseURL string, log *zap.Logger) (*Runner, error) {
    database, err := sql.Open("pgx", databaseURL)
    if err != nil { return nil, err }
    database.SetMaxOpenConns(4)
    database.SetMaxIdleConns(4)
    return &Runner{
        db: database, log: log,
        engineFactory: newMigrationEngine,
    }, nil
}
```

`Status` queries `to_regclass` first, then the existing table only. Missing/empty is version `-1` clean. Do not ping or construct migration source/driver.

- [ ] **Step 3: Write failing preflight matrix tests.**

```go
cases := []struct {
    name string
    current int
    target uint
    executions, events uint64
    manifestState, wantError string
}{
    {"clean install", -1, 138, 0, 0, "", ""},
    {"empty 137", 137, 138, 0, 0, "", ""},
    {"data below 137", 136, 138, 1, 0, "",
        "migrate to exact schema 137"},
    {"nonempty no manifest", 137, 138, 1, 1, "",
        "approved manifest is required"},
    {"matching manifest", 137, 138, 1, 1, "APPROVED", ""},
    {"preapply down", 138, 137, 1, 1, "APPROVED", ""},
    {"applied down", 138, 137, 1, 1, "APPLIED",
        "downgrade crossing 138"},
    {"verified down", 138, 137, 1, 1, "VERIFIED",
        "downgrade crossing 138"},
}
```

Add dirty, changed raw, manifest-assisted target 139, and absent-version-table/partial-schema cases. Every refusal asserts the engine factory count stays zero.

Run: `go test -p=1 ./internal/migrations -run TestExternalExecutionTimestampPreflight -count=1`

Expected before implementation: FAIL.

- [ ] **Step 4: Implement direct read-only preflight and make Step 3 pass.**

```go
func desiredVersion(options RunOptions, latest uint) uint {
    if options.Down { return 0 }
    if options.Target != nil { return *options.Target }
    return latest
}
```

Use `database/sql` and Task 2 `Compute*` functions, never `internal/db`. Enforce every rule above before engine construction. Manifest-assisted crossing requires explicit target 138.

- [ ] **Step 5: Write failing bounded-lock/cancellation tests.**

```go
lock := acquireTimestampMigrationAdvisoryLock(t, ctx, pool)
defer releaseTimestampMigrationAdvisoryLock(
    t, context.Background(), lock,
)
before := readSchemaStatus(t, ctx)
err := runner.Run(ctx, migrations.RunOptions{
    Target: uintPointer(138),
    LockTimeout: 100 * time.Millisecond,
})
g.Expect(err).To(MatchError(ContainSubstring(
    "timestamp migration advisory lock timeout after 100ms",
)))
g.Expect(engineFactoryCalls).To(Equal(uint64(0)))
g.Expect(readSchemaStatus(t, ctx)).To(Equal(before))
```

The integration helper must acquire and release the same exported key; do not repeat a SQL hash expression in tests:

```go
func acquireTimestampMigrationAdvisoryLock(
    t *testing.T, ctx context.Context, pool *pgxpool.Pool,
) *pgxpool.Conn {
    t.Helper()
    connection, err := pool.Acquire(ctx)
    NewWithT(t).Expect(err).NotTo(HaveOccurred())
    _, err = connection.Exec(ctx,
        `SELECT pg_advisory_lock($1)`,
        externalexecutiontimestamp.MigrationAdvisoryLockKey,
    )
    NewWithT(t).Expect(err).NotTo(HaveOccurred())
    return connection
}

func releaseTimestampMigrationAdvisoryLock(
    t *testing.T, ctx context.Context, connection *pgxpool.Conn,
) {
    t.Helper()
    _, err := connection.Exec(ctx,
        `SELECT pg_advisory_unlock($1)`,
        externalexecutiontimestamp.MigrationAdvisoryLockKey,
    )
    NewWithT(t).Expect(err).NotTo(HaveOccurred())
    connection.Release()
}
```

Add table tests for the timeout contract: zero normalizes to exactly ten seconds, a positive value is preserved exactly, and a negative value is rejected before acquiring a connection or constructing the engine.

```go
normalized, err := normalizeMigrationLockTimeout(0)
g.Expect(err).NotTo(HaveOccurred())
g.Expect(normalized).To(Equal(10 * time.Second))
normalized, err = normalizeMigrationLockTimeout(275 * time.Millisecond)
g.Expect(err).NotTo(HaveOccurred())
g.Expect(normalized).To(Equal(275 * time.Millisecond))
_, err = normalizeMigrationLockTimeout(-time.Nanosecond)
g.Expect(err).To(MatchError("migration lock timeout must be positive"))
```

The cancellation fake asserts `Stop` once and no next migration. The factory test captures exact current schema, five-minute statement timeout, and bounded migrate lock timeout.

Run: `go test -p=1 ./internal/migrations -run 'TestRunner(AdvisoryLockTimeout|CancellationStopsBeforeNextMigration|ConfiguresDriverTimeouts)' -count=1 -timeout 20m`

Expected before implementation: FAIL.

- [ ] **Step 6: Implement bounded session locking/lazy engine and make Step 5 pass.**

Normalize the timeout once, before acquiring the connection, and pass the normalized value to both the advisory-lock loop and golang-migrate:

```go
func normalizeMigrationLockTimeout(value time.Duration) (time.Duration, error) {
    if value == 0 {
        return DefaultMigrationLockTimeout, nil
    }
    if value < 0 {
        return 0, errors.New("migration lock timeout must be positive")
    }
    return value, nil
}
```

Poll every 100ms with a parameter; Task 5 application and Task 6 migration must import the same constant rather than independently hashing text:

```go
var locked bool
err := connection.QueryRowContext(ctx,
    `SELECT pg_try_advisory_lock($1)`,
    externalexecutiontimestamp.MigrationAdvisoryLockKey,
).Scan(&locked)
```

Defer an unlock that uses the same dedicated connection and key, even when the caller context has been canceled:

```go
defer func() {
    unlockContext, cancel := context.WithTimeout(
        context.WithoutCancel(ctx), DefaultMigrationLockTimeout,
    )
    defer cancel()
    var unlocked bool
    if err := connection.QueryRowContext(unlockContext,
        `SELECT pg_advisory_unlock($1)`,
        externalexecutiontimestamp.MigrationAdvisoryLockKey,
    ).Scan(&unlocked); err != nil || !unlocked {
        r.log.Error("failed to release timestamp migration advisory lock",
            zap.Error(err), zap.Bool("unlocked", unlocked))
    }
}()
```

Hold the dedicated connection through preflight/migration; release with `context.WithoutCancel`. Only after successful mutating preflight read `current_schema()` and construct:

```go
databaseDriver, err := postgres.WithInstance(database, &postgres.Config{
    SchemaName: schema,
    StatementTimeout: 5 * time.Minute,
})
if err != nil { return nil, err }
sourceDriver, err := iofs.New(fs, "sql")
if err != nil { return nil, err }
instance, err := migrate.NewWithInstance(
    "", sourceDriver, "distr", databaseDriver,
)
if err != nil { return nil, err }
instance.LockTimeout = lockTimeout
instance.Log = &Logger{r.log.Sugar()}
return instance, nil
```

`GracefulStop` prevents the next migration; the Compose hard timeout terminates an active statement.

- [ ] **Step 7: Write failing CLI tests, then implement injected `RunE`.**

```go
command := newMigrateCommand(fakeFactory)
command.SetArgs([]string{"--to", "0"})
g.Expect(command.Execute()).To(Succeed())
g.Expect(runtime.options.Target).NotTo(BeNil())
g.Expect(*runtime.options.Target).To(Equal(uint(0)))

for _, args := range [][]string{
    {"--down", "--to", "137"},
    {"--down", "--check"},
    {"--external-execution-timestamp-manifest", "approved.json"},
    {"--to", "139",
     "--external-execution-timestamp-manifest", "approved.json"},
} {
    command := newMigrateCommand(fakeFactory)
    command.SetArgs(args)
    g.Expect(command.Execute()).NotTo(Succeed())
}
```

Set `ToSet = cmd.Flags().Changed("to")`, decode manifest with `DisallowUnknownFields`, and return errors rather than `util.Must`. Change `Up`/`Down`/`Migrate` and `svc.New` caller to accept/pass context. Do not add `Force`.

Bind `--lock-timeout` with the same exported default and pass it unchanged to `RunOptions`; the runner remains responsible for normalization so programmatic callers receive the same behavior:

```go
command.Flags().DurationVar(
    &options.LockTimeout,
    "lock-timeout",
    migrations.DefaultMigrationLockTimeout,
    "maximum time to wait for the migration lock",
)
```

CLI tests assert the omitted flag supplies exactly `10*time.Second`, `--lock-timeout=275ms` is preserved, and `--lock-timeout=-1ns` fails before runtime invocation.

- [ ] **Step 8: Run live boundaries, validate, and commit.**

```powershell
go test -p=1 ./internal/externalexecutiontimestamp ./internal/migrations ./cmd/hub/cmd -run 'Test(MigrationAdvisoryLockKey|Migrate|ExternalExecutionTimestampPreflight)' -count=1 -timeout 20m
bash hack/validate-migrations.sh
git diff --check
git add internal/externalexecutiontimestamp/migration_lock* internal/migrations cmd/hub/cmd/migrate.go cmd/hub/cmd/migrate_test.go internal/svc/registry.go
git commit -m "feat: gate timestamp schema transitions"
```

## Task 7: Dual-Write Every External-Execution Timestamp Mutation

**Files:**

- Modify: `internal/db/external_executions.go`
- Modify: `internal/db/external_executions_test.go`
- Create: `internal/db/external_execution_timestamp_writes_test.go`

**Interfaces:**

- Consumes: Task 3 shadow columns/defaults; preserves all existing repository/public types.
- Produces: atomic paired writer semantics assumed by Task 5 verification and Task 8 startup readiness.

All production writes are confined to five execution paths plus the single event insert in `internal/db/external_executions.go`. Keep `externalExecutionOutputExpr` and `GetExternalExecutionEvents` on legacy columns.

For execution creation, explicitly bind one Go UTC deadline:

```sql
callback_deadline_at = CAST(@callbackDeadlineAt AS TIMESTAMPTZ) AT TIME ZONE 'UTC'
callback_deadline_at_instant = CAST(@callbackDeadlineAt AS TIMESTAMPTZ)
```

For trigger, timeout, failure, and callback updates, use one statement clock:

```sql
WITH write_clock AS (SELECT CURRENT_TIMESTAMP AS instant)
UPDATE ExternalExecution AS ee
SET updated_at = write_clock.instant AT TIME ZONE 'UTC',
    updated_at_instant = write_clock.instant
FROM write_clock
```

Preserve ambiguous history when a lifecycle value already exists:

```sql
started_at = COALESCE(ee.started_at, write_clock.instant AT TIME ZONE 'UTC'),
started_at_instant = CASE
  WHEN ee.started_at IS NULL THEN write_clock.instant
  ELSE ee.started_at_instant
END
```

Use the same legacy-field guard for `completed_at`. Never use `COALESCE(completed_at_instant, write_clock.instant)`, because that would assign a new instant to an unresolved historical wall value. PostgreSQL `CURRENT_TIMESTAMP` is transaction-stable, so the event default and execution mutation share the same instant.

- [ ] **Step 1: Add paired-deadline helper and failing create test.**

```go
func setExternalExecutionDeadline(
    t *testing.T, ctx context.Context,
    executionID uuid.UUID, deadline time.Time,
) {
    _, err := internalctx.GetDb(ctx).Exec(ctx, `
UPDATE ExternalExecution
SET callback_deadline_at =
      CAST(@deadline AS timestamptz) AT TIME ZONE 'UTC',
    callback_deadline_at_instant =
      CAST(@deadline AS timestamptz)
WHERE id = @id`,
        pgx.NamedArgs{
            "deadline": deadline.UTC(), "id": executionID,
        })
    NewWithT(t).Expect(err).NotTo(HaveOccurred())
}
```

Replace both one-column test updates. Assert after create:

```go
var createdPair, updatedPair, deadlinePair bool
g.Expect(database.QueryRow(ctx, `
SELECT
 created_at IS NOT DISTINCT FROM created_at_instant AT TIME ZONE 'UTC',
 updated_at IS NOT DISTINCT FROM updated_at_instant AT TIME ZONE 'UTC',
 callback_deadline_at IS NOT DISTINCT FROM
   callback_deadline_at_instant AT TIME ZONE 'UTC'
FROM ExternalExecution WHERE id=@id`,
    pgx.NamedArgs{"id": execution.ID},
).Scan(&createdPair, &updatedPair, &deadlinePair)).To(Succeed())
g.Expect([]bool{createdPair, updatedPair, deadlinePair}).
    To(Equal([]bool{true, true, true}))
```

Run: `go test -p=1 ./internal/db -run TestExternalExecutionInsertWritesTimestampPairs -count=1 -timeout 20m`

Expected before implementation: FAIL.

- [ ] **Step 2: Implement one-clock execution insert and make Step 1 pass.**

Add deadline shadow plus created/updated pairs to the existing insert:

```sql
WITH write_clock AS (SELECT CURRENT_TIMESTAMP AS instant)
INSERT INTO ExternalExecution AS ee (
  id, callback_deadline_at, callback_deadline_at_instant,
  organization_id, step_run_id, task_id, deployment_plan_id,
  deployment_plan_target_id, deployment_target_id,
  application_id, release_bundle_id, component, plan_checksum,
  idempotency_key, expected_state_version,
  expected_state_checksum, expected_version, expected_image,
  expected_platform, expected_contracts,
  expected_config_reference, expected_config_checksum,
  expected_compose_reference, expected_compose_checksum, status,
  created_at, created_at_instant, updated_at, updated_at_instant
)
SELECT
  @id,
  CAST(@callbackDeadlineAt AS timestamptz) AT TIME ZONE 'UTC',
  CAST(@callbackDeadlineAt AS timestamptz),
  @organizationId, @stepRunId, @taskId, @deploymentPlanId,
  @deploymentPlanTargetId, @deploymentTargetId,
  @applicationId, @releaseBundleId, @component, @planChecksum,
  @idempotencyKey, @expectedStateVersion,
  @expectedStateChecksum, @expectedVersion, @expectedImage,
  @expectedPlatform, @expectedContracts,
  @expectedConfigReference, @expectedConfigChecksum,
  @expectedComposeReference, @expectedComposeChecksum, @status,
  write_clock.instant AT TIME ZONE 'UTC', write_clock.instant,
  write_clock.instant AT TIME ZONE 'UTC', write_clock.instant
FROM write_clock
RETURNING ` + externalExecutionOutputExpr
```

Bind `@callbackDeadlineAt` from `execution.CallbackDeadlineAt.UTC()`; keep every other existing named argument unchanged.

- [ ] **Step 3: Write failing trigger/callback/timeout/failure/event tests.**

```go
var eventMatchesUpdated, terminalMatchesCompleted bool
g.Expect(database.QueryRow(ctx, `
SELECT event.created_at_instant = execution.updated_at_instant,
 CASE WHEN execution.status IN
   ('SUCCEEDED','FAILED','CANCELED','TIMED_OUT')
 THEN event.created_at_instant=execution.completed_at_instant
 ELSE true END
FROM ExternalExecution execution
JOIN ExternalExecutionEvent event
 ON event.external_execution_id=execution.id
WHERE execution.id=@id
ORDER BY event.sequence DESC LIMIT 1`,
    pgx.NamedArgs{"id": execution.ID},
).Scan(&eventMatchesUpdated, &terminalMatchesCompleted)).
    To(Succeed())
g.Expect(eventMatchesUpdated).To(BeTrue())
g.Expect(terminalMatchesCompleted).To(BeTrue())
```

Seed non-null legacy started/completed with null shadows and prove transitions preserve null shadows.

Run: `go test -p=1 ./internal/db -run 'TestExternalExecution(Trigger|Callback|Timeout|Failure)WritesTimestampPairs' -count=1 -timeout 20m`

Expected before implementation: FAIL.

- [ ] **Step 4: Implement all transition/event paths and make Step 3 pass.**

Replace the four update statements and event insert with these complete SQL bodies. Keep their existing named arguments and `RETURNING externalExecutionOutputExpr` calls.

`MarkExternalExecutionTriggered`:

```sql
WITH write_clock AS (SELECT CURRENT_TIMESTAMP AS instant)
UPDATE ExternalExecution AS ee
SET status=@status,
    started_at=COALESCE(
      ee.started_at, write_clock.instant AT TIME ZONE 'UTC'),
    started_at_instant=CASE WHEN ee.started_at IS NULL
      THEN write_clock.instant ELSE ee.started_at_instant END,
    trigger_attempts=GREATEST(ee.trigger_attempts, @triggerAttempts),
    updated_at=write_clock.instant AT TIME ZONE 'UTC',
    updated_at_instant=write_clock.instant
FROM write_clock
WHERE ee.id=@id AND ee.organization_id=@organizationId
RETURNING ` + externalExecutionOutputExpr
```

Both `timeoutExternalExecutionLocked` and `FailExternalExecution` use this exact body; their existing status/sequence/message arguments distinguish the business result:

```sql
WITH write_clock AS (SELECT CURRENT_TIMESTAMP AS instant)
UPDATE ExternalExecution AS ee
SET status=@status,
    completed_at=COALESCE(
      ee.completed_at, write_clock.instant AT TIME ZONE 'UTC'),
    completed_at_instant=CASE WHEN ee.completed_at IS NULL
      THEN write_clock.instant ELSE ee.completed_at_instant END,
    last_callback_sequence=@sequence,
    last_message=@message,
    error_summary=@message,
    updated_at=write_clock.instant AT TIME ZONE 'UTC',
    updated_at_instant=write_clock.instant
FROM write_clock
WHERE ee.id=@id AND ee.organization_id=@organizationId
RETURNING ` + externalExecutionOutputExpr
```

`updateExternalExecutionFromCallback` preserves every existing business-field assignment and uses this complete body:

```sql
WITH write_clock AS (SELECT CURRENT_TIMESTAMP AS instant)
UPDATE ExternalExecution AS ee
SET status=@status,
    started_at=COALESCE(
      ee.started_at, write_clock.instant AT TIME ZONE 'UTC'),
    started_at_instant=CASE WHEN ee.started_at IS NULL
      THEN write_clock.instant ELSE ee.started_at_instant END,
    completed_at=CASE WHEN @terminal THEN COALESCE(
      ee.completed_at, write_clock.instant AT TIME ZONE 'UTC')
      ELSE ee.completed_at END,
    completed_at_instant=CASE
      WHEN NOT @terminal THEN ee.completed_at_instant
      WHEN ee.completed_at IS NULL THEN write_clock.instant
      ELSE ee.completed_at_instant END,
    provider_reference=@providerReference,
    provider_url=@providerUrl,
    last_callback_sequence=@sequence,
    last_message=@message,
    error_summary=@errorSummary,
    actual_version=@actualVersion,
    actual_image=@actualImage,
    actual_platform=@actualPlatform,
    actual_contracts=@actualContracts,
    actual_config_reference=@actualConfigReference,
    actual_config_checksum=@actualConfigChecksum,
    actual_health=@actualHealth,
    observed_state_checksum=@observedStateChecksum,
    updated_at=write_clock.instant AT TIME ZONE 'UTC',
    updated_at_instant=write_clock.instant
FROM write_clock
WHERE ee.id=@id AND ee.organization_id=@organizationId
RETURNING ` + externalExecutionOutputExpr
```

`insertExternalExecutionEvent` explicitly writes both representations:

```sql
WITH write_clock AS (SELECT CURRENT_TIMESTAMP AS instant)
INSERT INTO ExternalExecutionEvent (
  created_at, created_at_instant, organization_id,
  external_execution_id, sequence, status, provider_reference,
  provider_url, message, observed_state, payload_hash
)
SELECT
  write_clock.instant AT TIME ZONE 'UTC', write_clock.instant,
  @organizationId, @externalExecutionId, @sequence, @status,
  @providerReference, @providerUrl, @message, @observedState,
  @payloadHash
FROM write_clock
```

Every event insert and its execution update remain inside the existing `RunTx`; PostgreSQL `CURRENT_TIMESTAMP` is transaction-stable, so the separately stated CTEs still yield the same instant. Never replace the guarded `CASE` expressions with `COALESCE(*_instant, write_clock.instant)`: a populated historical legacy value with a null shadow is unresolved history and must remain null.

- [ ] **Step 5: Write/run timezone and legacy-read matrix.**

```go
for _, databaseZone := range []string{
    "UTC", "Asia/Bangkok", "America/New_York",
} {
    for _, applicationZone := range []*time.Location{
        time.UTC,
        time.FixedZone("host-plus-seven", 7*60*60),
        time.FixedZone("host-minus-five", -5*60*60),
    } {
        t.Run(databaseZone+"/"+applicationZone.String(),
            func(t *testing.T) {
                previous := time.Local
                time.Local = applicationZone
                t.Cleanup(func() { time.Local = previous })
                ctx := externalExecutionContextWithSessionZone(
                    t, databaseZone,
                )
                assertAllExternalTimestampWritePathsArePaired(t, ctx)
            })
    }
}
```

Do not use `t.Parallel`. Store different legacy/shadow sentinels, marshal repository output, and assert:

```go
g.Expect(string(encoded)).NotTo(ContainSubstring("Instant"))
g.Expect(execution.CreatedAt).To(Equal(legacyCreatedSentinel))
```

Run: `go test -p=1 ./internal/db ./internal/mapping -run 'TestExternalExecutionTimestampWritesIgnoreSessionAndHostTimezone|TestExternalExecutionLegacyReadsIgnoreInstantShadows' -count=1 -timeout 20m`

Expected before every path is corrected: FAIL; after: PASS.

- [ ] **Step 6: Run regressions and commit.**

```powershell
go test -p=1 ./internal/db ./internal/hubexecutor ./internal/mapping -run 'ExternalExecution|HubExecutor' -count=1 -timeout 20m
git diff --check
git add internal/db/external_executions.go internal/db/external_executions_test.go internal/db/external_execution_timestamp_writes_test.go
git commit -m "feat: dual-write external execution instants"
```

## Task 8: Refuse Incompatible or Unverified Schemas at Startup

**Files:**

- Modify: `internal/types/external_execution_timestamp.go`
- Modify: `internal/db/external_execution_timestamps.go`
- Modify: `internal/db/external_execution_timestamps_test.go`
- Modify: `cmd/hub/cmd/external_execution_timestamps.go`
- Modify: `cmd/hub/cmd/external_execution_timestamps_test.go`
- Modify: `cmd/hub/cmd/serve.go`
- Create: `cmd/hub/cmd/serve_test.go`

**Interfaces:**

- Consumes: Task 5 standalone verification primitives and Task 7 paired-writer invariants.
- Produces: a fail-closed pre-write Hub startup gate and a standalone read-only `external-execution-timestamps readiness` command that reruns the identical lifecycle validation after startup.

Add:

```go
type ExternalExecutionTimestampReadiness struct {
    SchemaVersion uint
    TransitionKind string
    ManifestID *uuid.UUID
    ExecutionCount, EventCount uint64
    ProvenanceRows, PostTransitionPairCount uint64
}
func CheckExternalExecutionTimestampExpandReadiness(
    context.Context,
) (*types.ExternalExecutionTimestampReadiness, error)
func RequireExternalExecutionTimestampExpandReadiness(context.Context) error
// The direct-pool CLI calls CheckExternalExecutionTimestampExpandReadiness;
// it does not initialize svc.Registry, run migrations, or start workers.
type serveDatabaseStartupHooks struct {
    requireTimestampReadiness func(context.Context) error
    createAgentVersion func(context.Context) error
    reconcileEditionFeatures func(context.Context) error
}
func runServeDatabaseStartup(
    context.Context,
    serveDatabaseStartupHooks,
) error
```

Task 8 implements the single fail-closed readiness entry point, its startup wrapper, and its direct-pool CLI. It composes Task 5's standalone verification primitives rather than duplicating manifest/shadow rules. The guard runs immediately after the pool exists and before `CreateAgentVersion`, subscription reconciliation, metrics initialization, server start, worker start, or scheduler start. It requires:

- clean schema version 138 or later;
- all six legacy fields still `timestamp without time zone` with original nullability;
- all six shadows present as nullable `timestamp with time zone`;
- the migration-138 `started_at`/`completed_at` one-shot lifecycle trigger and function present and enabled;
- current/future indexes in their expand names;
- an immutable `ZERO_HISTORY` expand-state marker before allowing a no-manifest database, plus exactly paired shadows for every current populated legacy value;
- otherwise, a latest `VERIFIED` complete manifest whose immutable/evolved cells satisfy Task 5's exact verification rules, plus exact paired shadows for rows created after its snapshot.

The guard naturally rejects schema 137, dirty state, missing/wrong columns, a contracted schema, a partially applied ledger, a non-empty expanded historical database without a verified manifest, immutable/invalid raw drift, and shadow drift; it accepts only the defined paired lifecycle evolution.

- [ ] **Step 1: Write failing catalog and durable-marker tests.**

```go
cases := []struct {
    name string
    arrange func(context.Context)
    wantError string
}{
    {"clean zero history", arrangeCleanZeroHistory, ""},
    {"later additive shape", arrangeLaterExpandShape, ""},
    {"schema 137", arrangeSchema137, "schema version"},
    {"dirty 138", arrangeDirty138, "dirty"},
    {"missing shadow", arrangeMissingShadow, "column shape"},
    {"wrong type", arrangeWrongShadowType, "column shape"},
    {"missing lifecycle trigger", arrangeMissingLifecycleTrigger, "lifecycle trigger"},
    {"contracted", arrangeContractedShape, "column shape"},
    {"missing index", arrangeMissingFutureIndex, "index shape"},
    {"missing marker", arrangeMissingExpandState, "expand state"},
    {"forged marker", arrangeForgedExpandState, "expand state"},
}
readiness, err :=
    db.CheckExternalExecutionTimestampExpandReadiness(ctx)
g.Expect(err).NotTo(HaveOccurred())
g.Expect(readiness.TransitionKind).To(Equal("ZERO_HISTORY"))
g.Expect(readiness.ManifestID).To(BeNil())
```

Require all six legacy types/nullabilities, six nullable timestamp-with-time-zone shadows, the enabled one-shot lifecycle trigger/function, three legacy indexes, and two instant-next indexes.

Run: `go test -p=1 ./internal/db -run 'TestCheckExternalExecutionTimestampExpandReadiness(Catalog|ExpandState)' -count=1 -timeout 20m`

Expected before implementation: FAIL.

- [ ] **Step 2: Implement the single read-only entry point.**

```go
func CheckExternalExecutionTimestampExpandReadiness(
    ctx context.Context,
) (out *types.ExternalExecutionTimestampReadiness, finalErr error) {
    finalErr = RunReadOnlyTxRR(ctx, func(ctx context.Context) error {
        checked, err :=
            checkExternalExecutionTimestampExpandReadinessInTx(ctx)
        out = checked
        return err
    })
    return out, finalErr
}
func RequireExternalExecutionTimestampExpandReadiness(
    ctx context.Context,
) error {
    _, err := CheckExternalExecutionTimestampExpandReadiness(ctx)
    return err
}
```

Add the exact catalog/state query and dispatcher:

```go
const externalExecutionTimestampReadinessIndexesSQL = `
WITH expected(index_name, columns_fragment) AS (
  VALUES
    ('externalexecution_organization_status',
      '(organization_id, status, updated_at desc, id)'),
    ('externalexecution_task', '(task_id, created_at, id)'),
    ('externalexecutionevent_execution_sequence',
      '(external_execution_id, sequence, id)'),
    ('externalexecution_organization_status_instant_next',
      '(organization_id, status, updated_at_instant desc, id)'),
    ('externalexecution_task_instant_next',
      '(task_id, created_at_instant, id)')
)
SELECT count(*)
FROM expected
JOIN pg_class index_class ON index_class.relname=expected.index_name
JOIN pg_namespace namespace_row
  ON namespace_row.oid=index_class.relnamespace
 AND namespace_row.nspname=current_schema()
JOIN pg_index index_row ON index_row.indexrelid=index_class.oid
WHERE index_row.indisvalid AND index_row.indisready
  AND position(expected.columns_fragment IN
    lower(pg_get_indexdef(index_row.indexrelid))) > 0`

const externalExecutionTimestampExpandStateSQL = `
SELECT transition_kind, source_schema_version,
  transition_execution_count, transition_event_count,
  transition_raw_cell_count, transitioned_at
FROM ExternalExecutionTimestampExpandState`

type externalExecutionTimestampExpandState struct {
    TransitionKind string `db:"transition_kind"`
    SourceSchemaVersion uint `db:"source_schema_version"`
    ExecutionCount uint64 `db:"transition_execution_count"`
    EventCount uint64 `db:"transition_event_count"`
    RawCellCount uint64 `db:"transition_raw_cell_count"`
    TransitionedAt time.Time `db:"transitioned_at"`
}

func readExternalExecutionTimestampExpandStateInTx(
    ctx context.Context,
) (externalExecutionTimestampExpandState, error) {
    rows, err := internalctx.GetDb(ctx).Query(
        ctx, externalExecutionTimestampExpandStateSQL,
    )
    if err != nil {
        return externalExecutionTimestampExpandState{}, err
    }
    state, err := pgx.CollectExactlyOneRow(rows,
        pgx.RowToStructByName[externalExecutionTimestampExpandState])
    if err != nil {
        return externalExecutionTimestampExpandState{},
            fmt.Errorf("expand state must contain exactly one row: %w", err)
    }
    if state.SourceSchemaVersion != 137 ||
        state.RawCellCount != 5*state.ExecutionCount+state.EventCount {
        return externalExecutionTimestampExpandState{},
            errors.New("expand state counts/source version are invalid")
    }
    return state, nil
}

func checkExternalExecutionTimestampExpandReadinessInTx(
    ctx context.Context,
) (*types.ExternalExecutionTimestampReadiness, error) {
    contract, err := readExternalExecutionTimestampSchemaContractInTx(ctx)
    if err != nil {
        return nil, err
    }
    if contract.CatalogVersion < 138 {
        return nil, fmt.Errorf("schema version %d is pre-expand",
            contract.CatalogVersion)
    }
    var validIndexes int64
    if err := internalctx.GetDb(ctx).QueryRow(
        ctx, externalExecutionTimestampReadinessIndexesSQL,
    ).Scan(&validIndexes); err != nil {
        return nil, err
    }
    if validIndexes != 5 {
        return nil, fmt.Errorf("index shape has %d of 5 required indexes",
            validIndexes)
    }
    state, err := readExternalExecutionTimestampExpandStateInTx(ctx)
    if err != nil {
        return nil, err
    }
    switch state.TransitionKind {
    case "ZERO_HISTORY":
        return checkExternalExecutionTimestampZeroHistoryInTx(
            ctx, contract.CatalogVersion, state,
        )
    case "MANIFEST_REQUIRED":
        return checkExternalExecutionTimestampManifestReadinessInTx(
            ctx, contract.CatalogVersion, state,
        )
    default:
        return nil, fmt.Errorf("unsupported expand transition %q",
            state.TransitionKind)
    }
}
```

- [ ] **Step 3: Write failing `ZERO_HISTORY` tests, implement, and pass.**

```go
tests := []struct {
    name string
    arrange func(context.Context)
    wantError string
}{
    {"empty", arrangeEmptyZeroHistory, ""},
    {"later pairs", arrangePairedRowsAfterTransition, ""},
    {"row before transition", arrangeRowBeforeTransition,
        "post-transition creation"},
    {"missing shadow", arrangeMissingPostTransitionShadow, "unpaired"},
    {"different shadow", arrangeDifferentPostTransitionShadow, "unpaired"},
    {"manifest exists", arrangeManifestOnZeroHistory,
        "zero-history ledger"},
    {"nonzero transition", arrangeNonzeroZeroHistoryMarker,
        "zero-history transition counts"},
}
```

Implement the no-manifest branch with the same current-cell query and pair function used by Task 5:

```go
func checkExternalExecutionTimestampZeroHistoryInTx(
    ctx context.Context,
    schemaVersion uint,
    state externalExecutionTimestampExpandState,
) (*types.ExternalExecutionTimestampReadiness, error) {
    if state.ExecutionCount != 0 || state.EventCount != 0 ||
        state.RawCellCount != 0 {
        return nil, errors.New("zero-history transition counts must be zero")
    }
    database := internalctx.GetDb(ctx)
    var manifestCount, provenanceCount uint64
    if err := database.QueryRow(ctx, `
SELECT
  (SELECT count(*) FROM ExternalExecutionTimestampManifest),
  (SELECT count(*) FROM ExternalExecutionTimestampCellProvenance)`).
        Scan(&manifestCount, &provenanceCount); err != nil {
        return nil, err
    }
    if manifestCount != 0 || provenanceCount != 0 {
        return nil, errors.New("zero-history ledger must remain empty")
    }
    rows, err := database.Query(ctx,
        currentExternalExecutionTimestampCellsSQL)
    if err != nil {
        return nil, err
    }
    cells, err := pgx.CollectRows(rows,
        pgx.RowToStructByName[currentExternalExecutionTimestampCell])
    if err != nil {
        return nil, err
    }
    executionIDs := map[uuid.UUID]struct{}{}
    eventIDs := map[uuid.UUID]struct{}{}
    for _, cell := range cells {
        if cell.RowCreatedInstant == nil {
            return nil, errors.New("post-transition creation instant is absent")
        }
        createdAt, err := externalexecutiontimestamp.ParseInstant(
            *cell.RowCreatedInstant,
        )
        if err != nil || createdAt.Before(state.TransitionedAt) {
            return nil, errors.New("row predates zero-history transition")
        }
        if err := requireExactUTCTimestampPair(
            cell.RawValue, cell.InstantValue,
        ); err != nil {
            return nil, fmt.Errorf("zero-history row is unpaired: %w", err)
        }
        if cell.SourceTable == "externalexecution" {
            executionIDs[cell.SourceRowID] = struct{}{}
        } else {
            eventIDs[cell.SourceRowID] = struct{}{}
        }
    }
    return &types.ExternalExecutionTimestampReadiness{
        SchemaVersion: schemaVersion,
        TransitionKind: "ZERO_HISTORY",
        ExecutionCount: uint64(len(executionIDs)),
        EventCount: uint64(len(eventIDs)),
        PostTransitionPairCount: uint64(len(cells)),
    }, nil
}
```

Run: `go test -p=1 ./internal/db -run TestCheckExternalExecutionTimestampExpandReadinessZeroHistory -count=1 -timeout 20m`

Expected before implementation: FAIL; after: PASS.

- [ ] **Step 4: Write failing `MANIFEST_REQUIRED` tests, implement, and pass.**

```go
tests := []struct {
    name string
    arrange func(context.Context)
    wantError string
}{
    {"missing", arrangeNoManifest, "verified manifest"},
    {"approved", arrangeApprovedManifest, "VERIFIED"},
    {"applied", arrangeAppliedManifest, "VERIFIED"},
    {"verified", arrangeVerifiedManifest, ""},
    {"count mismatch", arrangeTransitionCountMismatch,
        "transition counts"},
    {"missing provenance", arrangeMissingProvenance, "provenance"},
    {"immutable drift", arrangeImmutableRawDrift, "immutable raw"},
    {"paired updated", arrangePairedUpdatedEvolution, ""},
    {"unpaired updated", arrangeUnpairedUpdatedEvolution,
        "updated_at pair"},
    {"null to paired completed", arrangePairedCompletedEvolution, ""},
    {"nonnull rewrite", arrangeNonnullCompletedRewrite,
        "immutable lifecycle"},
    {"filled unresolved", arrangeFilledUnresolvedShadow,
        "unresolved shadow"},
    {"paired later row", arrangePairedLaterRow, ""},
    {"evolved updated with superseding tip",
        arrangeEvolvedUpdatedWithSupersedingTip, ""},
}
```

Add fork/root cases to the table above:

```go
{"second verified root", arrangeSecondVerifiedRoot, "manifest tip"},
{"verified fork", arrangeVerifiedFork, "manifest tip"},
{"latest manifest changed source version", arrangeLatestSourceVersion138,
    "source version"},
```

Implement manifest readiness by selecting Task 5's unique verified tip and walking its one parent chain to the root. Rebuild and validate every complete document from its manifest row plus append-only provenance and validate every parent/child supersession link. This explicitly reproduces the original schema-137 counts, execution/event IDs, database identity, and raw-set checksum from the latest decision revision rather than current mutable columns. Compare the root snapshot counts with the immutable transition counts, then invoke the single Task 5 verifier on the tip to prove that every retained original row still exists, all permitted lifecycle evolution is valid against the root verification baseline, and every post-expand row is a complete paired write. The evolved-updated/superseding-tip case must pass end to end:

```go
func checkExternalExecutionTimestampManifestReadinessInTx(
    ctx context.Context,
    schemaVersion uint,
    state externalExecutionTimestampExpandState,
) (*types.ExternalExecutionTimestampReadiness, error) {
    if state.ExecutionCount == 0 && state.EventCount == 0 {
        return nil, errors.New("manifest-required transition has zero counts")
    }
    tip, err := readUniqueVerifiedManifestTipInTx(ctx)
    if err != nil {
        return nil, err
    }
    if tip == nil {
        return nil, errors.New("verified manifest tip is required")
    }
    currentID := *tip
    seen := map[uuid.UUID]struct{}{}
    var root *types.ExternalExecutionTimestampManifest
    var child *types.ExternalExecutionTimestampManifest
    for {
        if _, duplicate := seen[currentID]; duplicate {
            return nil, errors.New("manifest chain contains a cycle")
        }
        seen[currentID] = struct{}{}
        manifest, manifestState, err :=
            readStoredExternalExecutionTimestampManifestInTx(ctx, currentID)
        if err != nil {
            return nil, err
        }
        if manifestState !=
            types.ExternalExecutionTimestampManifestStateVerified {
            return nil, errors.New("manifest chain contains a non-VERIFIED row")
        }
        if problems := externalexecutiontimestamp.ValidateManifestDocument(
            *manifest,
        ); len(problems) != 0 {
            return nil, errors.Join(problems...)
        }
        if manifest.SourceSchemaVersion != state.SourceSchemaVersion {
            return nil, errors.New("manifest chain source version differs from expand state")
        }
        if child != nil {
            if problems := externalexecutiontimestamp.ValidateSupersession(
                *manifest, *child,
            ); len(problems) != 0 {
                return nil, errors.Join(problems...)
            }
        }
        if manifest.SupersedesManifestID == nil {
            root = manifest
            break
        }
        child = manifest
        currentID = *manifest.SupersedesManifestID
    }
    if root.ExecutionCount != state.ExecutionCount ||
        root.EventCount != state.EventCount ||
        root.RawCellCount != state.RawCellCount {
        return nil, errors.New("root manifest does not match transition counts")
    }
    verified, err := verifyExternalExecutionTimestampManifestInTx(ctx, *tip)
    if err != nil {
        return nil, err
    }
    return &types.ExternalExecutionTimestampReadiness{
        SchemaVersion: schemaVersion,
        TransitionKind: "MANIFEST_REQUIRED",
        ManifestID: tip,
        ExecutionCount: verified.CurrentExecutionCount,
        EventCount: verified.CurrentEventCount,
        ProvenanceRows: verified.ProvenanceRows,
        PostTransitionPairCount: verified.PostManifestPairedCount,
    }, nil
}
```

Run: `go test -p=1 ./internal/db -run TestCheckExternalExecutionTimestampExpandReadinessManifestRequired -count=1 -timeout 20m`

Expected before implementation: FAIL; after: PASS.

- [ ] **Step 5: Write failing startup-order test, then add the guard.**

```go
err := runServeDatabaseStartup(context.Background(),
    serveDatabaseStartupHooks{
        requireTimestampReadiness: func(context.Context) error {
            calls = append(calls, "readiness")
            return errors.New("not ready")
        },
        createAgentVersion: func(context.Context) error {
            calls = append(calls, "agent-version")
            return nil
        },
        reconcileEditionFeatures: func(context.Context) error {
            calls = append(calls, "subscription")
            return nil
        },
    })
g.Expect(err).To(HaveOccurred())
g.Expect(calls).To(Equal([]string{"readiness"}))
```

Implement the helper and replace the two direct startup writes in `runServe`:

```go
func runServeDatabaseStartup(
    ctx context.Context,
    hooks serveDatabaseStartupHooks,
) error {
    if err := hooks.requireTimestampReadiness(ctx); err != nil {
        return fmt.Errorf("external-execution timestamp readiness: %w", err)
    }
    if err := hooks.createAgentVersion(ctx); err != nil {
        return fmt.Errorf("create agent version: %w", err)
    }
    if err := hooks.reconcileEditionFeatures(ctx); err != nil {
        return fmt.Errorf("reconcile edition features: %w", err)
    }
    return nil
}

// Immediately after dbLogCtx is constructed:
util.Must(runServeDatabaseStartup(dbLogCtx, serveDatabaseStartupHooks{
    requireTimestampReadiness:
        db.RequireExternalExecutionTimestampExpandReadiness,
    createAgentVersion: db.CreateAgentVersion,
    reconcileEditionFeatures: subscription.ReconcileEditionFeatures,
}))
```

Place this call immediately after `dbLogCtx := internalctx.WithLogger(dbCtx, registry.GetLogger())`. Metrics initialization, server construction/start, `HubExecutor.Start`, and `JobsScheduler.Start` remain after it.

Add a direct-pool subcommand for the post-start check. It invokes the same function, so lifecycle behavior cannot drift from startup:

```go
func newExternalExecutionTimestampReadinessCommand() *cobra.Command {
    return &cobra.Command{
        Use: "readiness",
        Args: cobra.NoArgs,
        RunE: func(command *cobra.Command, _ []string) error {
            pool, err := pgxpool.New(command.Context(), env.DatabaseUrl())
            if err != nil {
                return err
            }
            defer pool.Close()
            ctx := internalctx.WithDb(command.Context(), pool)
            report, err := db.CheckExternalExecutionTimestampExpandReadiness(ctx)
            if err != nil {
                return err
            }
            encoder := json.NewEncoder(command.OutOrStdout())
            encoder.SetEscapeHTML(false)
            return encoder.Encode(report)
        },
    }
}
```

Register it under `external-execution-timestamps` and add a command test that injects a direct-pool test database, evolves `updated_at` as an exact pair, runs `readiness`, and asserts success; mutate only the shadow and assert the command fails with `updated_at pair`. This is the exact read-only post-start lifecycle validation consumed by the Compose acceptance check.

Run: `go test -p=1 ./cmd/hub/cmd -run 'TestServe(RefusesTimestampSchema|DatabaseStartup)' -count=1`

Expected before implementation: FAIL; after: PASS.

- [ ] **Step 6: Run readiness/startup gate and commit.**

```powershell
go test -p=1 ./internal/db ./cmd/hub/cmd -run 'TestRequireExternalExecutionTimestampExpandReadiness|TestServeRefusesTimestampSchema' -count=1 -timeout 20m
go build ./cmd/hub
git diff --check
git add internal/types/external_execution_timestamp.go internal/db/external_execution_timestamps* cmd/hub/cmd/external_execution_timestamps* cmd/hub/cmd/serve.go cmd/hub/cmd/serve_test.go
git commit -m "feat: enforce expand schema startup contract"
```

## Task 9: Add the Fenced Two-Phase Compose Rollout

**Files:**

- Modify: `deploy/server-docker-compose/deploy.sh`
- Modify: `deploy/server-docker-compose/docker-compose.yml`
- Modify: `deploy/server-docker-compose/.env.example`
- Create: `hack/test-server-compose-timestamp-expand.sh`

**Interfaces:**

- Consumes: Tasks 4-8 CLI commands/reports plus the existing digest-pinned Compose deployment.
- Produces: durable fenced capture/apply/cancel operations and a stop-before-backup ordinary release path.

The script interface is fixed so the shell test can source and replace one operation at a time:

```bash
active_timestamp_fence
persist_timestamp_fence "$state" "$fence_id" "$evidence_dir" "$source_digest" "$target_digest"
fence_value "$key"
require_timestamp_fence "$evidence_dir"
clear_timestamp_fence "$evidence_dir"
persist_timestamp_compatibility "$manifest_id"
require_rollback_schema_compatibility "$target_digest"
assert_hub_writers_stopped
run_timestamp_operator "$evidence_dir" external-execution-timestamps inspect --output /evidence/draft-manifest.json
run_timestamp_operator_with_database "$evidence_dir" "$database_url" external-execution-timestamps inspect --output /evidence/restore-inspection.json
copy_file_create_new_0600 "$source_file" "$destination_file"
stage_approved_manifest "$approved_manifest" "$evidence_dir"
write_sha256_sidecar_create_new "$file"
aggregate_volume_checksum "$volume_name"
backup_and_restore_timestamp_evidence "$evidence_dir"
backup_and_restore_release_evidence
compare_timestamp_inspections "$source_json" "$restore_json" "$object_restore_json"
verify_timestamp_evidence "$approved_manifest" "$evidence_dir"
require_clean_schema_138
require_verified_manifest "$manifest_id"
verify_post_start_counts "$source_inspection"
verify_audit_history_visibility "$manifest_id" "$evidence_dir"
verify_task_lock_integrity
verify_no_duplicate_event_sequence
timestamp_expand_capture "$evidence_dir"
timestamp_expand_apply "$approved_manifest" "$evidence_dir"
timestamp_expand_cancel "$evidence_dir"
dispatch_command "$@"
```

At the top of `deploy.sh`, preserve a caller-supplied test/operator env file and define the two durable state files. Do not restore the current unconditional `.env` assignment:

```bash
ENV_FILE="${ENV_FILE:-${SCRIPT_DIR}/.env}"
[[ "$ENV_FILE" == /* ]] || {
  printf '[distr-deploy] ERROR: ENV_FILE must be absolute: %s\n' "$ENV_FILE" >&2
  return 1 2>/dev/null || exit 1
}
TIMESTAMP_FENCE_FILE="${TIMESTAMP_FENCE_FILE:-${SCRIPT_DIR}/.timestamp-expand-fence}"
TIMESTAMP_COMPATIBILITY_FILE="${TIMESTAMP_COMPATIBILITY_FILE:-${SCRIPT_DIR}/.timestamp-expand-compatibility}"

compose() {
  DISTR_COMPOSE_ENV_FILE="${DISTR_COMPOSE_ENV_FILE:-$ENV_FILE}" \
    docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" "$@" || return
}
```

The wrapper makes Compose interpolation and every service-level `env_file` resolve to the same caller-selected absolute file. A test-provided `ENV_FILE` therefore cannot silently render with one file and start the container with the repository `.env`.

Every new helper is safe when its caller appears in `if`, `!`, `&&`, or `||`: every critical external command, command substitution, pipeline, and filesystem mutation has its own `|| return`. `set -e` is defense in depth, not control flow. Cleanup-only commands may use `|| true`.

Every function returns nonzero on refusal. `persist_timestamp_fence` uses `mktemp` in the fence directory, `chmod 600`, `mv` on the same filesystem, and exactly six keys: `STATE`, `FENCE_ID`, `EVIDENCE_DIR_CHECKSUM`, `SOURCE_IMAGE_DIGEST`, `TARGET_IMAGE_DIGEST`, and `CREATED_AT`. Values are validated against `^[A-Za-z0-9_./:@+-]+$`; the script never sources the file. `require_timestamp_fence` parses only those known keys with `IFS='=' read -r key value` and compares the canonical evidence-directory SHA-256.

`copy_file_create_new_0600` requires a regular non-symlink source, creates a temporary file in the destination directory under `umask 077`, copies and fsyncs it, then uses a same-filesystem hard link to create the final path without replacement. It removes the temporary name on every exit. An existing destination is never overwritten. `stage_approved_manifest` stages the operator-reviewed file as `approved-manifest.json` before any manifest CLI call; a retry may reuse that path only when its mode is exactly `0600` and its checksum matches the supplied source.

Add this operator profile; apply the same configurable `env_file` expression to `migrate`, `hub`, and `artifact-blob-cleanup`. Hub remains `serve --migrate=false`.

The evidence directory remains mode `0700` and owned by the deployment account. Do not `chown` it to an image-specific UID. Every one-shot `timestamp-operator` run supplies `--user "$(id -u):$(id -g)"`, including restored-database inspection, validate, dry-run/apply, verify, preflight, and migration, so host-side staging and container output share one owner.

```yaml
timestamp-operator:
  image: ${DISTR_IMAGE_REF:?set DISTR_IMAGE_REF in .env}
  command: ['external-execution-timestamps', '--help']
  env_file:
    - ${DISTR_COMPOSE_ENV_FILE:?deploy.sh supplies the absolute env file}
  environment:
    PGAPPNAME: distr-timestamp-operator
  volumes:
    - ${DISTR_TIMESTAMP_EVIDENCE_DIR:-./timestamp-evidence}:/evidence
  depends_on:
    postgres:
      condition: service_healthy
    storage:
      condition: service_healthy
  profiles:
    - timestamp-operator

hub:
  image: ${DISTR_IMAGE_REF:?set DISTR_IMAGE_REF in .env}
  command: ['serve', '--migrate=false']
  env_file:
    - ${DISTR_COMPOSE_ENV_FILE:?deploy.sh supplies the absolute env file}
  environment:
    PGAPPNAME: distr-hub
  depends_on:
    postgres:
      condition: service_healthy
    storage:
      condition: service_healthy
  ports:
    - '127.0.0.1:${DISTR_HTTP_PORT:-8080}:8080'
    - '127.0.0.1:${DISTR_REGISTRY_PORT:-8585}:8585'
  networks:
    default:
    nginx-ui:
      aliases:
        - distr-hub
  restart: unless-stopped
  stop_grace_period: 45s
```

Use the same required `env_file` expression for `migrate` and `artifact-blob-cleanup`. `DISTR_COMPOSE_ENV_FILE` lets validation point to an absolute example env without creating or overwriting the real `.env`; `deploy.sh` defaults it to the already absolute `ENV_FILE` in the process environment of every real `docker compose` invocation.

Add these script commands:

```text
./deploy/server-docker-compose/deploy.sh timestamp-expand-capture "$DISTR_TIMESTAMP_EVIDENCE_DIR"
./deploy/server-docker-compose/deploy.sh timestamp-expand-apply "$DISTR_TIMESTAMP_APPROVED_MANIFEST" "$DISTR_TIMESTAMP_EVIDENCE_DIR"
./deploy/server-docker-compose/deploy.sh timestamp-expand-cancel "$DISTR_TIMESTAMP_EVIDENCE_DIR"
```

`TIMESTAMP_FENCE_FILE` defaults to `deploy/server-docker-compose/.timestamp-expand-fence`. Capture creates it atomically with mode `0600` and records only the opaque fence ID, evidence-directory checksum, source image digest, target image digest, and creation instant. While it exists, `up`, ordinary `release`, `deploy`, `rollback`, cleanup, and every other mutating command refuse to start Hub or change schema; only apply or cancel with the matching evidence directory may proceed.

`timestamp-expand-capture` executes exactly:

1. acquire `.deploy.lock`;
2. validate Compose and digest-pinned image configuration;
3. pull the target image and start only PostgreSQL/object storage;
4. generate the opaque fence ID and atomically persist the matching durable fence file in `PREPARING` state;
5. stop every Compose Hub replica, verify no project Hub container or Hub-labeled PostgreSQL session remains, prove the callback endpoint is unavailable, and advance the durable fence to `CAPTURED_WRITERS_STOPPED`;
6. create PostgreSQL and object-store backups;
7. write SHA-256 sidecars with restrictive permissions;
8. restore PostgreSQL into a fresh ephemeral PostgreSQL 18.4 container/volume and restore the object-store archive into a separate ephemeral volume;
9. run Task 4 `inspect` against the restored database, compute and record the restored object-volume aggregate, and only then run Task 4 `inspect` against the fenced source database;
10. require equal schema version, execution/event counts, complete `rawCellChecksum`, database identity, and object-store aggregate checksum;
11. write `draft-manifest.json`, `restore-inspection.json`, `object-restore-inspection.json`, `source-inspection.json`, and `fence-id` under the evidence directory with mode `0600`;
12. remove every ephemeral restore container/volume; and
13. leave Hub stopped for independent manifest review/sealing.

The backup must precede source inspection, matching ADR-0055. The writer fence makes both snapshots stable.

`timestamp-expand-apply` executes exactly:

1. reacquire `.deploy.lock` and prove Hub remains stopped;
2. verify database/object backups, both restore inspections, source inspection, fence, externally reviewed approved manifest, target commit, and image-digest checksums;
3. safely stage the reviewed manifest as create-new mode-`0600` `approved-manifest.json` in the evidence directory, then run Task 4 `validate-manifest` against the still-fenced source;
4. run a dry-run `apply` and require expected counts;
5. run `timeout --signal=TERM --kill-after=15s 5m distr migrate --to 138 --external-execution-timestamp-manifest approved-manifest.json`;
6. run mutating `apply --apply` with fence/backup/restore evidence;
7. run Task 5 read-only `verify`;
8. start only the target digest with `serve --migrate=false`, whose Task 8 guard checks readiness before its first service write;
9. verify `/ready`, running image digest, clean schema 138, manifest `VERIFIED`, execution/event counts, audit/history visibility, task locks, and no duplicate sequence; and
10. remove the active fence file only after every post-start check passes; and
11. retain the evidence directory, fence evidence, and previous-known-good image reference.

`timestamp-expand-cancel` is the pre-migration escape path. It requires the matching active fence, clean exact schema 137, unchanged source inspection/database identity, and the previous-known-good digest; it starts that previous image, passes health checks, and only then clears the active fence. It refuses schema 138 or later. After migration 138 begins, recovery is either completion with the expand image or the separately documented verified-backup restore; the pre-expand image must not resume writes on schema 138.

Change ordinary `release` to run `distr migrate --check` first. On a non-empty schema 137 it must fail while the old Hub is still running and print the two staged commands; it may not stop Hub, create a backup, or invoke migration. When check passes, every ordinary release uses `stop/fence Hub -> verify writers stopped -> PostgreSQL and object backup -> isolated PostgreSQL and object restore -> restore inspection/checksum proof -> migrate -> start -> health`; the current backup-before-stop order is removed even for zero-history and later schemas. A failed backup, restore, readiness probe, inspection, or checksum comparison returns before `run_migrations`. The release-specific evidence directory is retained under `${BACKUP_DIR}/release-evidence-<UTC>-<random>` with mode `0700`. Do not merge demo/tutorial cleanup into either timestamp command.

After a successful expand, persist `.timestamp-expand-compatibility` before clearing the active fence. It records exact schema `138`, the expand image digest/ref, the pre-expand image digest/ref, manifest UUID, and creation instant. It remains after the active fence is removed. Ordinary `rollback` reads this file without `source`: schema below 138 may use the pre-existing rollback rules; exact schema 138 permits only the exact expand image recorded by valid schema-138 metadata; schema above 138 fails closed because this release defines no compatibility record for a later schema. A future release may relax that refusal only by adding metadata whose schema exactly equals the live schema. Every refusal occurs before changing `.env`, pulling, or starting Hub.

- [ ] **Step 1: Write the failing sourced orchestration test.** Start `hack/test-server-compose-timestamp-expand.sh` with the exact source guard and order harness below, then add the four named test calls shown at the end:

```bash
#!/usr/bin/env bash
set -Eeuo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
export DISTR_DEPLOY_LIB_ONLY=1
export ENV_FILE="$TMP/.env"
export BACKUP_DIR="$TMP/backups"
export TIMESTAMP_FENCE_FILE="$TMP/fence"
export DISTR_TIMESTAMP_EVIDENCE_DIR="$TMP/evidence"
mkdir -p "$DISTR_TIMESTAMP_EVIDENCE_DIR"
source "$ROOT/deploy/server-docker-compose/deploy.sh"

events=()
record(){
  events+=("$1")
  printf '%s\n' "$1" >>"$TMP/event-log"
}
assert_events(){
  local want="$1" actual
  actual="$(paste -sd' ' "$TMP/event-log")"
  [[ "$actual" == "$want" ]] || {
    printf 'want: %s\n got: %s\n' "$want" "$actual" >&2
    return 1
  }
}
reset_stubs(){
 events=()
 : >"$TMP/event-log"
 active_timestamp_fence(){ return 1; }
 acquire_deploy_lock(){ record lock; }
 compose_config(){ record config; }
 pull_image(){ record pull; }
 start_dependencies(){ record deps; }
 prepare_timestamp_evidence_dir(){ :; }
 write_fence_id_evidence(){ :; }
 persist_timestamp_fence(){ record "fence:$1"; }
 stop_hub(){ record stop; }
 assert_hub_writers_stopped(){ record writers-stopped; }
 backup_and_restore_timestamp_evidence(){
   record backup-db
   record backup-object
   record restore-db
   record restore-object
   record restore-inspect
   record object-restore-inspect
 }
 compare_timestamp_inspections(){ record compare; }
 require_timestamp_fence(){ record require-fence; }
 verify_timestamp_evidence(){ record evidence; }
 stage_approved_manifest(){ record stage-approved; }
 run_timestamp_operator(){ case "$*" in *validate-manifest*) record validate;; *'apply --manifest'*'--apply'*) record apply;; *'apply --manifest'*) record dry-run;; *verify*) record verify;; *inspect*) record source-inspect;; esac; }
 run_timestamp_migration_138(){ record migrate-138; }
 start_hub(){ record start; }
 health(){ record health; }
 verify_running_digest(){ record digest; }
 require_clean_schema_138(){ record schema-138; }
 require_verified_manifest(){ record manifest-verified; }
 verify_post_start_counts(){ record counts; }
 verify_audit_history_visibility(){ record audit; }
 verify_task_lock_integrity(){ record task-locks; }
 verify_no_duplicate_event_sequence(){ record sequences; }
 persist_timestamp_compatibility(){ record compatibility; }
 clear_timestamp_fence(){ record clear-fence; }
 run_migration_preflight(){ record check; }
 backup_and_restore_release_evidence(){
   record ordinary-backup-db
   record ordinary-backup-object
   record ordinary-restore-db
   record ordinary-restore-object
   record ordinary-verify-restore
 }
 run_migrations(){ record ordinary-migrate; }
 require_clean_schema_137(){ record schema-137; }
 validate_source_inspection(){ record source-validate; }
 set_env_var(){ record set-source-image; }
 load_env(){ :; }
 fence_value(){ printf '%s' 'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'; }
 running_hub_digest(){ printf '%s' 'registry.invalid/distr@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'; }
 latest_database_backup(){ printf '%s' "$BACKUP_DIR/postgres-fence42.dump"; }
 checksum_value(){ printf '%s' 'sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'; }
 manifest_id(){ printf '%s' '11111111-1111-4111-8111-111111111111'; }
}
test_capture_order(){ reset_stubs; timestamp_expand_capture "$DISTR_TIMESTAMP_EVIDENCE_DIR"; assert_events 'lock config pull deps fence:PREPARING stop writers-stopped fence:CAPTURED_WRITERS_STOPPED backup-db backup-object restore-db restore-object restore-inspect object-restore-inspect source-inspect compare'; }
test_apply_order(){ reset_stubs; timestamp_expand_apply "$TMP/approved.json" "$DISTR_TIMESTAMP_EVIDENCE_DIR"; assert_events 'lock require-fence writers-stopped evidence stage-approved validate dry-run migrate-138 apply verify start health digest schema-138 manifest-verified counts audit task-locks sequences compatibility clear-fence'; }
test_ordinary_release_order(){ reset_stubs; release_from_ecr; assert_events 'lock config pull deps check stop writers-stopped ordinary-backup-db ordinary-backup-object ordinary-restore-db ordinary-restore-object ordinary-verify-restore ordinary-migrate start health'; }
test_failure_keeps_fence(){ reset_stubs; run_timestamp_migration_138(){ record migrate-138; return 42; }; if timestamp_expand_apply "$TMP/approved.json" "$DISTR_TIMESTAMP_EVIDENCE_DIR"; then return 1; fi; [[ " ${events[*]} " != *' start '* && " ${events[*]} " != *' clear-fence '* ]]; }
test_post_start_failure_stops_hub_and_keeps_fence(){
  reset_stubs
  verify_task_lock_integrity(){ record task-locks; return 42; }
  if timestamp_expand_apply "$TMP/approved.json" "$DISTR_TIMESTAMP_EVIDENCE_DIR"; then
    return 1
  fi
  assert_events 'lock require-fence writers-stopped evidence stage-approved validate dry-run migrate-138 apply verify start health digest schema-138 manifest-verified counts audit task-locks stop writers-stopped'
  ! grep -Eq '^(compatibility|clear-fence)$' "$TMP/event-log"
}
test_ordinary_restore_failure_prevents_migration(){
  reset_stubs
  backup_and_restore_release_evidence(){
    record ordinary-backup-db
    record ordinary-backup-object
    record ordinary-restore-db
    return 42
  }
  if release_from_ecr; then return 1; fi
  assert_events 'lock config pull deps check stop writers-stopped ordinary-backup-db ordinary-backup-object ordinary-restore-db'
  [[ " ${events[*]} " != *' ordinary-migrate '* && " ${events[*]} " != *' start '* ]]
}
test_nonempty_137_preflight_keeps_old_hub_running(){
  reset_stubs
  run_migration_preflight(){ record check; return 42; }
  if release_from_ecr; then return 1; fi
  assert_events 'lock config pull deps check'
  ! grep -Eq '^(stop|writers-stopped|ordinary-backup-|ordinary-migrate|start)$' \
    "$TMP/event-log"
}
test_capture_order
test_apply_order
test_ordinary_release_order
test_failure_keeps_fence
test_post_start_failure_stops_hub_and_keeps_fence
test_ordinary_restore_failure_prevents_migration
test_nonempty_137_preflight_keeps_old_hub_running
printf 'timestamp expand compose orchestration tests passed\n'
```

- [ ] **Step 2: Run the orchestration test and confirm red.**

```powershell
bash hack/test-server-compose-timestamp-expand.sh
```

Expected: FAIL because `DISTR_DEPLOY_LIB_ONLY` and the timestamp functions do not exist.

- [ ] **Step 3: Implement the source guard and fixed orchestration.** Add this guard immediately before the command `case`, then implement the three functions exactly in terms of the interfaces above:

```bash
timestamp_expand_capture() {
  local evidence_dir="${1:?evidence directory is required}" fence_id source_digest
  acquire_deploy_lock || return
  if active_timestamp_fence; then
    die "timestamp expand fence already active: ${TIMESTAMP_FENCE_FILE}"
    return 1
  fi
  compose_config || return
  pull_image || return
  start_dependencies || return
  prepare_timestamp_evidence_dir "$evidence_dir" || return
  fence_id="$(openssl rand -hex 16)" || return
  source_digest="$(running_hub_digest)" || return
  persist_timestamp_fence PREPARING \
    "$fence_id" "$evidence_dir" "$source_digest" "$DISTR_IMAGE_REF" ||
    return
  write_fence_id_evidence "$fence_id" "$evidence_dir" || return
  stop_hub || return
  assert_hub_writers_stopped || return
  persist_timestamp_fence CAPTURED_WRITERS_STOPPED \
    "$fence_id" "$evidence_dir" "$source_digest" "$DISTR_IMAGE_REF" ||
    return
  backup_and_restore_timestamp_evidence "$evidence_dir" || return
  run_timestamp_operator "$evidence_dir" \
    external-execution-timestamps inspect \
    --output /evidence/source-inspection.json || return
  write_sha256_sidecar_create_new \
    "$evidence_dir/source-inspection.json" || return
  copy_file_create_new_0600 \
    "$evidence_dir/source-inspection.json" \
    "$evidence_dir/draft-manifest.json" || return
  write_sha256_sidecar_create_new \
    "$evidence_dir/draft-manifest.json" || return
  compare_timestamp_inspections \
    "$evidence_dir/source-inspection.json" \
    "$evidence_dir/restore-inspection.json" \
    "$evidence_dir/object-restore-inspection.json" || return
}
timestamp_expand_apply() {
  local manifest="${1:?approved manifest is required}"
  local evidence_dir="${2:?evidence directory is required}"
  local fence_id backup_file backup_reference backup_checksum restore_checksum approved_id
  acquire_deploy_lock || return
  require_timestamp_fence "$evidence_dir" || return
  assert_hub_writers_stopped || return
  verify_timestamp_evidence "$manifest" "$evidence_dir" || return
  stage_approved_manifest "$manifest" "$evidence_dir" || return
  run_timestamp_operator "$evidence_dir" \
    external-execution-timestamps validate-manifest \
    --manifest /evidence/approved-manifest.json || return
  run_timestamp_operator "$evidence_dir" \
    external-execution-timestamps apply \
    --manifest /evidence/approved-manifest.json || return
  run_timestamp_migration_138 "$evidence_dir" || return
  fence_id="$(fence_value FENCE_ID)" || return
  backup_file="$(latest_database_backup)" || return
  backup_reference="$(basename -- "$backup_file")" || return
  backup_checksum="$(checksum_value "$backup_file")" || return
  restore_checksum="$(checksum_value \
    "$evidence_dir/restore-inspection.json")" || return
  run_timestamp_operator "$evidence_dir" \
    external-execution-timestamps apply \
    --manifest /evidence/approved-manifest.json --apply \
    --writer-fence-id "$fence_id" \
    --backup-reference "$backup_reference" \
    --backup-checksum "$backup_checksum" \
    --restore-reference restore-inspection.json \
    --restore-checksum "$restore_checksum" ||
    return
  approved_id="$(manifest_id "$evidence_dir/approved-manifest.json")" || return
  run_timestamp_operator "$evidence_dir" \
    external-execution-timestamps verify \
    --manifest-id "$approved_id" || return
  start_verify_and_finalize_timestamp_expand \
    "$approved_id" "$evidence_dir" || return
}
timestamp_expand_cancel() {
  local evidence_dir="${1:?evidence directory is required}" source_digest
  acquire_deploy_lock || return
  require_timestamp_fence "$evidence_dir" || return
  assert_hub_writers_stopped || return
  require_clean_schema_137 || return
  validate_source_inspection \
    "$evidence_dir/source-inspection.json" || return
  source_digest="$(fence_value SOURCE_IMAGE_DIGEST)" || return
  set_env_var DISTR_IMAGE_REF "$source_digest" || return
  load_env || return
  pull_image || return
  start_verify_cancel_and_clear "$evidence_dir" || return
}

release_from_ecr() {
  acquire_deploy_lock || return
  compose_config || return
  pull_image || return
  start_dependencies || return
  run_migration_preflight || return
  stop_hub || return
  assert_hub_writers_stopped || return
  backup_and_restore_release_evidence || return
  run_migrations || return
  start_hub || return
  health || return
}

require_no_active_timestamp_fence() {
  if active_timestamp_fence; then
    die "timestamp expand fence is active; only apply or cancel is allowed"
    return 1
  fi
}

dispatch_command() {
  local command_name="${1:-}"
  case "$command_name" in
    timestamp-expand-apply)
      shift
      timestamp_expand_apply "$@" || return
      ;;
    timestamp-expand-cancel)
      shift
      timestamp_expand_cancel "$@" || return
      ;;
    timestamp-expand-capture)
      shift
      timestamp_expand_capture "$@" || return
      ;;
    init|build|push|pull|deps|backup|migrate|up|release|deploy|cleanup-artifacts|rollback)
      require_no_active_timestamp_fence || return
      dispatch_ordinary_command "$@" || return
      ;;
    *)
      dispatch_read_only_command "$@" || return
      ;;
  esac
}
if [[ "${DISTR_DEPLOY_LIB_ONLY:-0}" == 1 ]]; then return 0 2>/dev/null || exit 0; fi
```

Extract the existing mutating `case` arms into `dispatch_ordinary_command` and the help/check/config/health/logs/ps arms into `dispatch_read_only_command`; neither helper recursively calls `dispatch_command`. The sole production tail is `dispatch_command "$@" || exit $?`.

Implement the state files as data, never shell code. This parser rejects symlinks, non-`0600` files, unknown/duplicate/missing keys, extra `=`, and values outside the allowlist:

```bash
parse_restricted_key_file() {
  local file="${1:?state file is required}"
  local expected_csv="${2:?expected keys are required}"
  local output_name="${3:?output name is required}"
  local mode key value extra candidate allowed
  local -a expected
  local -n output="$output_name"
  output=()
  [[ -f "$file" && ! -L "$file" ]] || {
    die "unsafe or missing state file: $file"; return 1;
  }
  mode="$(stat -c '%a' -- "$file")" || return
  [[ "$mode" == 600 ]] || {
    die "state file mode must be 0600: $file"; return 1;
  }
  IFS=',' read -r -a expected <<<"$expected_csv" || return
  while IFS='=' read -r key value extra || [[ -n "$key$value$extra" ]]; do
    [[ -n "$key" && -n "$value" && -z "$extra" ]] || {
      die "invalid state-file record: $file"; return 1;
    }
    [[ "$value" =~ ^[A-Za-z0-9_./:@+-]+$ ]] || {
      die "invalid state-file value for $key"; return 1;
    }
    allowed=0
    for candidate in "${expected[@]}"; do
      [[ "$key" == "$candidate" ]] && allowed=1
    done
    ((allowed == 1)) || { die "unknown state-file key: $key"; return 1; }
    [[ ! -v "output[$key]" ]] || {
      die "duplicate state-file key: $key"; return 1;
    }
    output["$key"]="$value"
  done <"$file"
  for candidate in "${expected[@]}"; do
    [[ -v "output[$candidate]" ]] || {
      die "missing state-file key: $candidate"; return 1;
    }
  done
  ((${#output[@]} == ${#expected[@]})) || return 1
}

evidence_dir_checksum() {
  local evidence_dir="${1:?evidence directory is required}" canonical
  [[ -d "$evidence_dir" && ! -L "$evidence_dir" ]] || {
    die "evidence directory is unsafe: $evidence_dir"; return 1;
  }
  canonical="$(readlink -f -- "$evidence_dir")" || return
  printf '%s' "$canonical" | sha256sum | awk '{print "sha256:"$1}' || return
}

prepare_timestamp_evidence_dir() {
  local evidence_dir="${1:?evidence directory is required}"
  local deployment_uid deployment_gid expected_owner actual_owner
  deployment_uid="$(id -u)" || return
  deployment_gid="$(id -g)" || return
  expected_owner="$deployment_uid:$deployment_gid"
  mkdir -p -- "$evidence_dir" || return
  [[ ! -L "$evidence_dir" ]] || {
    die "evidence directory may not be a symlink"; return 1;
  }
  chmod 0700 -- "$evidence_dir" || return
  actual_owner="$(stat -c '%u:%g' -- "$evidence_dir")" || return
  [[ "$actual_owner" == "$expected_owner" ]] || {
    die "evidence directory must be owned by the deployment user"; return 1;
  }
}

active_timestamp_fence() { [[ -e "$TIMESTAMP_FENCE_FILE" ]]; }

persist_timestamp_fence() (
  local state="${1:?state is required}" fence_id="${2:?fence id is required}"
  local evidence_dir="${3:?evidence directory is required}"
  local source_digest="${4:?source digest is required}"
  local target_digest="${5:?target digest is required}"
  local checksum created_at directory temporary=''
  local keys='STATE,FENCE_ID,EVIDENCE_DIR_CHECKSUM,SOURCE_IMAGE_DIGEST,TARGET_IMAGE_DIGEST,CREATED_AT'
  local -A current=()
  [[ "$state" == PREPARING || "$state" == CAPTURED_WRITERS_STOPPED ]] || {
    die "invalid timestamp fence state: $state"; return 1;
  }
  checksum="$(evidence_dir_checksum "$evidence_dir")" || return
  if [[ -e "$TIMESTAMP_FENCE_FILE" ]]; then
    parse_restricted_key_file "$TIMESTAMP_FENCE_FILE" "$keys" current || return
    [[ "${current[FENCE_ID]}" == "$fence_id" &&
       "${current[EVIDENCE_DIR_CHECKSUM]}" == "$checksum" &&
       "${current[SOURCE_IMAGE_DIGEST]}" == "$source_digest" &&
       "${current[TARGET_IMAGE_DIGEST]}" == "$target_digest" ]] || {
      die "timestamp fence identity changed"; return 1;
    }
    [[ "${current[STATE]}" == PREPARING &&
       "$state" == CAPTURED_WRITERS_STOPPED ]] || {
      die "invalid timestamp fence transition"; return 1;
    }
    created_at="${current[CREATED_AT]}"
  else
    created_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)" || return
  fi
  directory="$(dirname -- "$TIMESTAMP_FENCE_FILE")" || return
  [[ -d "$directory" && ! -L "$directory" ]] || return 1
  umask 077
  temporary="$(mktemp "$directory/.timestamp-fence.XXXXXX")" || return
  trap 'rm -f -- "${temporary:-}" >/dev/null 2>&1 || true' EXIT HUP INT TERM
  {
    printf 'STATE=%s\n' "$state"
    printf 'FENCE_ID=%s\n' "$fence_id"
    printf 'EVIDENCE_DIR_CHECKSUM=%s\n' "$checksum"
    printf 'SOURCE_IMAGE_DIGEST=%s\n' "$source_digest"
    printf 'TARGET_IMAGE_DIGEST=%s\n' "$target_digest"
    printf 'CREATED_AT=%s\n' "$created_at"
  } >"$temporary" || return
  chmod 0600 -- "$temporary" || return
  sync -f "$temporary" || return
  mv -fT -- "$temporary" "$TIMESTAMP_FENCE_FILE" || return
  temporary=''
  sync -f "$directory" || return
)

fence_value() {
  local wanted="${1:?fence key is required}"
  local keys='STATE,FENCE_ID,EVIDENCE_DIR_CHECKSUM,SOURCE_IMAGE_DIGEST,TARGET_IMAGE_DIGEST,CREATED_AT'
  local -A values=()
  parse_restricted_key_file "$TIMESTAMP_FENCE_FILE" "$keys" values || return
  [[ -v "values[$wanted]" ]] || { die "unknown fence key: $wanted"; return 1; }
  printf '%s' "${values[$wanted]}"
}

require_timestamp_fence() {
  local evidence_dir="${1:?evidence directory is required}" actual expected
  local keys='STATE,FENCE_ID,EVIDENCE_DIR_CHECKSUM,SOURCE_IMAGE_DIGEST,TARGET_IMAGE_DIGEST,CREATED_AT'
  local -A values=()
  parse_restricted_key_file "$TIMESTAMP_FENCE_FILE" "$keys" values || return
  [[ "${values[STATE]}" == CAPTURED_WRITERS_STOPPED ]] || {
    die "timestamp fence is not ready for apply/cancel"; return 1;
  }
  actual="$(evidence_dir_checksum "$evidence_dir")" || return
  expected="${values[EVIDENCE_DIR_CHECKSUM]}"
  [[ "$actual" == "$expected" ]] || {
    die "timestamp evidence directory does not match fence"; return 1;
  }
}

clear_timestamp_fence() {
  local evidence_dir="${1:?evidence directory is required}"
  require_timestamp_fence "$evidence_dir" || return
  rm -- "$TIMESTAMP_FENCE_FILE" || return
}

persist_timestamp_compatibility() (
  local approved_id="${1:?manifest id is required}"
  local keys='SCHEMA_VERSION,EXPAND_IMAGE_DIGEST,PRE_EXPAND_IMAGE_DIGEST,MANIFEST_ID,CREATED_AT'
  local expand_digest pre_expand_digest created_at directory temporary=''
  local -A current=()
  [[ "$approved_id" =~ ^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$ ]] || return 1
  expand_digest="$(fence_value TARGET_IMAGE_DIGEST)" || return
  pre_expand_digest="$(fence_value SOURCE_IMAGE_DIGEST)" || return
  if [[ -e "$TIMESTAMP_COMPATIBILITY_FILE" ]]; then
    parse_restricted_key_file "$TIMESTAMP_COMPATIBILITY_FILE" "$keys" current || return
    [[ "${current[SCHEMA_VERSION]}" == 138 &&
       "${current[EXPAND_IMAGE_DIGEST]}" == "$expand_digest" &&
       "${current[PRE_EXPAND_IMAGE_DIGEST]}" == "$pre_expand_digest" &&
       "${current[MANIFEST_ID]}" == "$approved_id" ]] || {
      die "existing timestamp compatibility record differs"; return 1;
    }
    return 0
  fi
  created_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)" || return
  directory="$(dirname -- "$TIMESTAMP_COMPATIBILITY_FILE")" || return
  temporary="$(mktemp "$directory/.timestamp-compatibility.XXXXXX")" || return
  trap 'rm -f -- "${temporary:-}" >/dev/null 2>&1 || true' EXIT HUP INT TERM
  {
    printf 'SCHEMA_VERSION=138\n'
    printf 'EXPAND_IMAGE_DIGEST=%s\n' "$expand_digest"
    printf 'PRE_EXPAND_IMAGE_DIGEST=%s\n' "$pre_expand_digest"
    printf 'MANIFEST_ID=%s\n' "$approved_id"
    printf 'CREATED_AT=%s\n' "$created_at"
  } >"$temporary" || return
  chmod 0600 -- "$temporary" || return
  sync -f "$temporary" || return
  ln -- "$temporary" "$TIMESTAMP_COMPATIBILITY_FILE" || return
  rm -- "$temporary" || return
  temporary=''
  sync -f "$directory" || return
)
```

`rollback_app` resolves its immutable target first, then calls `require_rollback_schema_compatibility "$image_ref" || return` before either `set_env_var`, `pull_image`, or `start_hub`. Implement the gate as:

```bash
require_rollback_schema_compatibility() {
  local target="${1:?rollback image is required}" schema
  local keys='SCHEMA_VERSION,EXPAND_IMAGE_DIGEST,PRE_EXPAND_IMAGE_DIGEST,MANIFEST_ID,CREATED_AT'
  local -A values=()
  schema="$(current_schema_version)" || return
  [[ "$schema" =~ ^[0-9]+$ ]] || {
    die "rollback refused: current schema version is invalid"; return 1;
  }
  if ((schema < 138)); then return 0; fi
  if ((schema > 138)); then
    die "rollback refused: no exact compatibility metadata exists for schema $schema"
    return 1
  fi
  parse_restricted_key_file "$TIMESTAMP_COMPATIBILITY_FILE" "$keys" values || return
  [[ "${values[SCHEMA_VERSION]}" == "$schema" ]] || {
    die "rollback refused: compatibility metadata schema differs from live schema"; return 1;
  }
  [[ "$target" != "${values[PRE_EXPAND_IMAGE_DIGEST]}" ]] || {
    die "rollback refused: pre-expand image cannot write schema 138"; return 1;
  }
  [[ "$target" == "${values[EXPAND_IMAGE_DIGEST]}" ]] || {
    die "rollback refused: image is not recorded compatible with schema 138"; return 1;
  }
}
```

In `rollback_app`, insert this line immediately after the digest/tag has been resolved into `image_ref` and before the existing `info`/`set_env_var` sequence:

```bash
require_rollback_schema_compatibility "$image_ref" || return
```

Implement safe staging exactly as a same-directory create-new operation; do not rely on no-clobber copy/move flags or a check-then-overwrite sequence:

```bash
copy_file_create_new_0600() (
  set -Eeuo pipefail
  local source="${1:?source is required}"
  local destination="${2:?destination is required}"
  local directory temporary destination_name
  [[ -f "$source" && ! -L "$source" ]] || {
    die "source must be a regular non-symlink file"; return 1;
  }
  [[ ! -e "$destination" && ! -L "$destination" ]] || {
    die "destination already exists: $destination"; return 1;
  }
  directory="$(dirname -- "$destination")" || return
  destination_name="$(basename -- "$destination")" || return
  [[ -d "$directory" && ! -L "$directory" ]] || {
    die "destination directory is unsafe"; return 1;
  }
  umask 077
  temporary="$(mktemp "$directory/.$destination_name.tmp.XXXXXX")" || return
  trap 'rm -f -- "${temporary:-}"' EXIT HUP INT TERM
  install -m 0600 -- "$source" "$temporary" || return
  sync -f "$temporary" || return
  ln -- "$temporary" "$destination" || {
    die "could not create destination without replacement"; return 1;
  }
  rm -f -- "$temporary" || return
  temporary=''
  trap - EXIT HUP INT TERM || return
)

stage_approved_manifest() {
  local source="${1:?approved manifest is required}"
  local evidence_dir="${2:?evidence directory is required}"
  local destination="$evidence_dir/approved-manifest.json" mode
  if [[ -e "$destination" ]]; then
    [[ -f "$destination" && ! -L "$destination" ]] || {
      die "staged approved manifest is unsafe"; return 1;
    }
    mode="$(stat -c '%a' -- "$destination")" || return
    [[ "$mode" == 600 ]] || {
      die "staged approved manifest mode must be 0600"; return 1;
    }
    cmp -s -- "$source" "$destination" || {
      die "staged approved manifest differs from supplied manifest"; return 1;
    }
    verify_sha256_sidecar "$destination" || return
    return
  fi
  copy_file_create_new_0600 "$source" "$destination" || return
  write_sha256_sidecar_create_new "$destination" || return
}

write_sha256_sidecar_create_new() (
  set -Eeuo pipefail
  local file="${1:?file is required}"
  local directory sidecar temporary digest sidecar_name file_name
  directory="$(dirname -- "$file")" || return
  sidecar="$file.sha256"
  sidecar_name="$(basename -- "$sidecar")" || return
  file_name="$(basename -- "$file")" || return
  [[ ! -e "$sidecar" ]] || {
    die "checksum sidecar exists: $sidecar"; return 1;
  }
  digest="$(sha256sum -- "$file" | awk '{print $1}')" || return
  umask 077
  temporary="$(mktemp "$directory/.$sidecar_name.tmp.XXXXXX")" || return
  trap 'rm -f -- "${temporary:-}"' EXIT HUP INT TERM
  printf '%s  %s\n' "$digest" "$file_name" >"$temporary" || return
  chmod 0600 "$temporary" || return
  sync -f "$temporary" || return
  ln -- "$temporary" "$sidecar" || {
    die "could not create checksum sidecar without replacement"; return 1;
  }
  rm -f -- "$temporary" || return
  temporary=''
  trap - EXIT HUP INT TERM || return
)
```

Use only the final aggregate digest outside the checksum container:

```bash
aggregate_volume_checksum() {
  local volume="${1:?volume name is required}"
  docker run --rm -v "$volume:/data:ro" alpine:3.23 sh -ceu '
    cd /data
    find . -type f -print0 |
      LC_ALL=C sort -z |
      xargs -0 -r sha256sum |
      sha256sum |
      awk "{print \$1}"
  ' || return
}

compare_timestamp_inspections() {
  local source_json="${1:?source inspection is required}"
  local restore_json="${2:?restore inspection is required}"
  local object_json="${3:?object inspection is required}"
  local source_fingerprint restore_fingerprint
  source_fingerprint="$(jq -c '[
    .sourceSchemaVersion,
    .executionCount,
    .eventCount,
    .rawCellCount,
    .rawCellChecksum,
    .databaseIdentityChecksum
  ]' "$source_json")" || return
  restore_fingerprint="$(jq -c '[
    .sourceSchemaVersion,
    .executionCount,
    .eventCount,
    .rawCellCount,
    .rawCellChecksum,
    .databaseIdentityChecksum
  ]' "$restore_json")" || return
  [[ "$source_fingerprint" == "$restore_fingerprint" ]] || {
    die "restored database inspection differs from fenced source"; return 1;
  }
  jq -e '
    .sourceAggregateChecksum ==
    .restoredAggregateChecksum
  ' "$object_json" >/dev/null || {
    die "restored object aggregate differs from fenced source"; return 1;
  }
}

run_timestamp_operator() {
  local evidence_dir="${1:?evidence directory is required}"
  local deployment_uid deployment_gid
  shift
  deployment_uid="$(id -u)" || return
  deployment_gid="$(id -g)" || return
  DISTR_COMPOSE_ENV_FILE="${DISTR_COMPOSE_ENV_FILE:-$ENV_FILE}" \
    DISTR_TIMESTAMP_EVIDENCE_DIR="$evidence_dir" \
    docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" \
      --profile timestamp-operator run --rm \
      --user "$deployment_uid:$deployment_gid" \
      timestamp-operator "$@" || return
}

run_timestamp_operator_with_database() {
  local evidence_dir="$1" database_url="$2"
  local deployment_uid deployment_gid
  shift 2
  deployment_uid="$(id -u)" || return
  deployment_gid="$(id -g)" || return
  DISTR_COMPOSE_ENV_FILE="${DISTR_COMPOSE_ENV_FILE:-$ENV_FILE}" \
    DISTR_TIMESTAMP_EVIDENCE_DIR="$evidence_dir" \
    docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" \
      --profile timestamp-operator run --rm \
      --user "$deployment_uid:$deployment_gid" \
      -e "DATABASE_URL=$database_url" \
      timestamp-operator "$@" || return
}
```

Add the concrete evidence, digest, writer-fence, migration, and post-start proof helpers below. `pull_image` must also run `compose --profile timestamp-operator pull timestamp-operator || return` so the operator is the same pinned image as Hub.

```bash
verify_sha256_sidecar() {
  local file="${1:?file is required}" sidecar="$file.sha256"
  local file_mode sidecar_mode directory sidecar_name
  [[ -f "$file" && ! -L "$file" && -f "$sidecar" && ! -L "$sidecar" ]] || return 1
  file_mode="$(stat -c '%a' -- "$file")" || return
  sidecar_mode="$(stat -c '%a' -- "$sidecar")" || return
  [[ "$file_mode" == 600 && "$sidecar_mode" == 600 ]] || return 1
  directory="$(dirname -- "$file")" || return
  sidecar_name="$(basename -- "$sidecar")" || return
  (cd "$directory" || return; sha256sum -c --status -- "$sidecar_name" || return)
}

checksum_value() {
  local file="${1:?file is required}" digest recorded expected
  verify_sha256_sidecar "$file" || return
  read -r digest recorded <"$file.sha256" || return
  expected="$(basename -- "$file")" || return
  [[ "$digest" =~ ^[0-9a-f]{64}$ && "$recorded" == "$expected" ]] || return 1
  printf 'sha256:%s' "$digest"
}

latest_database_backup() {
  local fence_id path
  fence_id="$(fence_value FENCE_ID)" || return
  path="$BACKUP_DIR/postgres-$fence_id.dump"
  [[ -f "$path" && ! -L "$path" ]] || return 1
  printf '%s' "$path"
}

latest_object_backup() {
  local fence_id path
  fence_id="$(fence_value FENCE_ID)" || return
  path="$BACKUP_DIR/rustfs-$fence_id.tar.gz"
  [[ -f "$path" && ! -L "$path" ]] || return 1
  printf '%s' "$path"
}

manifest_id() {
  local manifest="${1:?manifest is required}"
  jq -er '.id | select(test("^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))' \
    "$manifest" || return
}

running_hub_digest() {
  local container configured
  container="$(compose ps -q hub)" || return
  [[ -n "$container" ]] || return 1
  configured="$(docker inspect --format '{{.Config.Image}}' "$container")" || return
  [[ "$configured" == *@sha256:* ]] || return 1
  printf '%s' "$configured"
}

verify_running_digest() {
  local actual
  actual="$(running_hub_digest)" || return
  [[ "$actual" == "$DISTR_IMAGE_REF" ]] || {
    die "running Hub image differs from DISTR_IMAGE_REF"; return 1;
  }
}

write_fence_id_evidence() {
  local fence_id="${1:?fence id is required}"
  local evidence_dir="${2:?evidence directory is required}" temporary
  temporary="$(mktemp "$evidence_dir/.fence-id.XXXXXX")" || return
  printf '%s\n' "$fence_id" >"$temporary" || { rm -f -- "$temporary" || true; return 1; }
  chmod 0600 -- "$temporary" || { rm -f -- "$temporary" || true; return 1; }
  copy_file_create_new_0600 "$temporary" "$evidence_dir/fence-id" || {
    rm -f -- "$temporary" || true; return 1;
  }
  rm -f -- "$temporary" || return
  write_sha256_sidecar_create_new "$evidence_dir/fence-id" || return
}

assert_hub_writers_stopped() {
  local running sessions callback_url
  need_cmd curl || return
  running="$(compose ps --status running -q hub)" || return
  [[ -z "$running" ]] || { die "a Compose Hub container is still running"; return 1; }
  sessions="$(compose exec -T postgres sh -ceu '
      PGPASSWORD="$POSTGRES_PASSWORD" psql \
        --username="$POSTGRES_USER" --dbname="$POSTGRES_DB" \
        -v ON_ERROR_STOP=1 -AtX -c \
        "SELECT count(*) FROM pg_stat_activity WHERE application_name = '\''distr-hub'\'' AND pid <> pg_backend_pid()"
    ')" || return
  [[ "$sessions" == 0 ]] || { die "distr-hub PostgreSQL sessions remain"; return 1; }
  callback_url="${DISTR_CALLBACK_PROBE_URL:-http://127.0.0.1:${DISTR_HTTP_PORT:-8080}/api/v1/external-executions/00000000-0000-4000-8000-000000000000/callbacks}"
  [[ "$callback_url" =~ ^https?://(127\.0\.0\.1|localhost)(:[0-9]+)?/ ]] || {
    die "callback fence probe must use the local Hub endpoint"; return 1;
  }
  if curl --silent --show-error --connect-timeout 2 --max-time 3 \
      --output /dev/null "$callback_url"; then
    die "Hub callback endpoint remains reachable"
    return 1
  fi
}

verify_timestamp_evidence() {
  local approved="${1:?approved manifest is required}"
  local evidence_dir="${2:?evidence directory is required}"
  local database_backup object_backup file recorded_fence expected_fence
  local state target_commit target_digest target_ref evidence_checksum
  require_timestamp_fence "$evidence_dir" || return
  database_backup="$(latest_database_backup)" || return
  object_backup="$(latest_object_backup)" || return
  for file in \
      "$database_backup" "$object_backup" \
      "$evidence_dir/fence-id" \
      "$evidence_dir/restore-inspection.json" \
      "$evidence_dir/object-restore-inspection.json" \
      "$evidence_dir/source-inspection.json" \
      "$evidence_dir/draft-manifest.json" \
      "$approved"; do
    verify_sha256_sidecar "$file" || {
      die "missing, unsafe, or invalid evidence checksum: $file"; return 1;
    }
  done
  read -r recorded_fence <"$evidence_dir/fence-id" || return
  expected_fence="$(fence_value FENCE_ID)" || return
  [[ "$recorded_fence" == "$expected_fence" ]] || return 1
  state="$(jq -er '.state' "$approved")" || return
  target_commit="$(jq -er '.targetReleaseCommit' "$approved")" || return
  target_digest="$(jq -er '.targetImageDigest' "$approved")" || return
  evidence_checksum="$(jq -er '.evidenceBundleChecksum' "$approved")" || return
  target_ref="$(fence_value TARGET_IMAGE_DIGEST)" || return
  [[ "$state" == APPROVED &&
     "$target_commit" == "$DISTR_RELEASE_COMMIT" &&
     "$target_digest" == "$DISTR_IMAGE_DIGEST" &&
     "$target_ref" == "$DISTR_IMAGE_REF" &&
     "$evidence_checksum" == "$DISTR_TIMESTAMP_EVIDENCE_CHECKSUM" ]] || {
    die "approved manifest release/evidence binding differs from deployment"; return 1;
  }
  manifest_id "$approved" >/dev/null || return
}

postgres_scalar() {
  local sql="${1:?SQL is required}"
  compose exec -T postgres sh -ceu '
      PGPASSWORD="$POSTGRES_PASSWORD" psql \
        --username="$POSTGRES_USER" --dbname="$POSTGRES_DB" \
        -v ON_ERROR_STOP=1 -AtX -c "$1"
    ' sh "$sql" || return
}

current_schema_version() {
  postgres_scalar 'SELECT version FROM schema_migrations' || return
}

require_clean_schema_137() {
  local status
  status="$(postgres_scalar "SELECT version::text || ':' || dirty::text FROM schema_migrations")" || return
  [[ "$status" == 137:false ]] || { die "cancel requires clean schema 137"; return 1; }
}

require_clean_schema_138() {
  local status
  status="$(postgres_scalar "SELECT version::text || ':' || dirty::text FROM schema_migrations")" || return
  [[ "$status" == 138:false ]] || { die "post-start schema must be 138 and clean"; return 1; }
}

require_verified_manifest() {
  local approved_id="${1:?manifest id is required}" state
  [[ "$approved_id" =~ ^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$ ]] || return 1
  state="$(postgres_scalar "SELECT state FROM ExternalExecutionTimestampManifest WHERE id = '$approved_id'::uuid")" || return
  [[ "$state" == VERIFIED ]] || { die "timestamp manifest is not VERIFIED"; return 1; }
}

verify_post_start_counts() {
  local source="${1:?source inspection is required}" expected actual
  expected="$(jq -er '[.executionCount,.eventCount] | map(tostring) | join(":")' "$source")" || return
  actual="$(postgres_scalar "SELECT (SELECT count(*) FROM ExternalExecution)::text || ':' || (SELECT count(*) FROM ExternalExecutionEvent)::text")" || return
  [[ "$actual" == "$expected" ]] || { die "post-start execution/event counts changed"; return 1; }
}

verify_audit_history_visibility() {
  local approved_id="${1:?manifest id is required}"
  local evidence_dir="${2:?evidence directory is required}"
  run_timestamp_operator "$evidence_dir" external-execution-timestamps verify \
    --manifest-id "$approved_id" || return
}

verify_task_lock_integrity() {
  local defects
  defects="$(postgres_scalar "WITH duplicate_active AS (SELECT 1 FROM TaskResourceLock WHERE acquired_at IS NOT NULL AND released_at IS NULL AND concurrency_policy <> 'ALLOW_PARALLEL' GROUP BY organization_id,resource_type,resource_key HAVING count(*) > 1) SELECT (SELECT count(*) FROM duplicate_active)::text || ':' || (SELECT count(*) FROM TaskResourceLock WHERE acquired_at IS NULL AND released_at IS NOT NULL)::text")" || return
  [[ "$defects" == 0:0 ]] || { die "task-resource lock integrity check failed"; return 1; }
}

verify_no_duplicate_event_sequence() {
  local duplicates
  duplicates="$(postgres_scalar "SELECT count(*) FROM (SELECT external_execution_id,sequence FROM ExternalExecutionEvent GROUP BY external_execution_id,sequence HAVING count(*) > 1) duplicate_sequence")" || return
  [[ "$duplicates" == 0 ]] || { die "duplicate external-execution event sequence found"; return 1; }
}

start_verify_and_finalize_timestamp_expand() (
  local approved_id="${1:?manifest id is required}"
  local evidence_dir="${2:?evidence directory is required}" complete=0
  cleanup_failed_start() {
    local status=$?
    trap - EXIT HUP INT TERM
    if ((complete == 0)); then
      stop_hub || true
      assert_hub_writers_stopped || true
    fi
    exit "$status"
  }
  trap cleanup_failed_start EXIT HUP INT TERM
  start_hub || return
  health || return
  verify_running_digest || return
  require_clean_schema_138 || return
  require_verified_manifest "$approved_id" || return
  verify_post_start_counts "$evidence_dir/source-inspection.json" || return
  verify_audit_history_visibility "$approved_id" "$evidence_dir" || return
  verify_task_lock_integrity || return
  verify_no_duplicate_event_sequence || return
  persist_timestamp_compatibility "$approved_id" || return
  clear_timestamp_fence "$evidence_dir" || return
  complete=1
)

start_verify_cancel_and_clear() (
  local evidence_dir="${1:?evidence directory is required}" complete=0
  cleanup_failed_cancel() {
    local status=$?
    trap - EXIT HUP INT TERM
    if ((complete == 0)); then
      stop_hub || true
      assert_hub_writers_stopped || true
    fi
    exit "$status"
  }
  trap cleanup_failed_cancel EXIT HUP INT TERM
  start_hub || return
  health || return
  verify_running_digest || return
  clear_timestamp_fence "$evidence_dir" || return
  complete=1
)

run_migration_preflight() {
  local deployment_uid deployment_gid
  deployment_uid="$(id -u)" || return
  deployment_gid="$(id -g)" || return
  DISTR_COMPOSE_ENV_FILE="${DISTR_COMPOSE_ENV_FILE:-$ENV_FILE}" \
  timeout --signal=TERM --kill-after=15s 5m \
    docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" \
      --profile timestamp-operator run --rm \
      --user "$deployment_uid:$deployment_gid" \
      timestamp-operator migrate --check || return
}

run_timestamp_migration_138() {
  local evidence_dir="${1:?evidence directory is required}"
  local deployment_uid deployment_gid
  deployment_uid="$(id -u)" || return
  deployment_gid="$(id -g)" || return
  DISTR_COMPOSE_ENV_FILE="${DISTR_COMPOSE_ENV_FILE:-$ENV_FILE}" \
    DISTR_TIMESTAMP_EVIDENCE_DIR="$evidence_dir" \
    timeout --signal=TERM --kill-after=15s 5m \
      docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" \
        --profile timestamp-operator run --rm \
        --user "$deployment_uid:$deployment_gid" \
        timestamp-operator migrate --to 138 \
        --external-execution-timestamp-manifest \
        /evidence/approved-manifest.json || return
}
```

The timeout deliberately receives the real `docker compose` argv; it never attempts to execute the shell-only `compose` function.

Because these legacy helpers are now called beneath `|| return`, rewrite their critical paths with explicit propagation too; otherwise Bash disables `errexit` for the whole function body. Keep the existing validation messages, but use this control-flow shape:

```bash
load_env() {
  [[ -f "$ENV_FILE" ]] || { die "missing $ENV_FILE"; return 1; }
  set -a || return
  # shellcheck disable=SC1090
  source "$ENV_FILE" || { set +a || true; return 1; }
  set +a || return
}

set_env_var() {
  local key="${1:?key is required}" value="${2:?value is required}"
  if grep -qE "^${key}=" "$ENV_FILE"; then
    sed -i.bak -E "s#^${key}=.*#${key}=${value}#" "$ENV_FILE" || return
  else
    printf '\n%s=%s\n' "$key" "$value" >>"$ENV_FILE" || return
  fi
  rm -f -- "$ENV_FILE.bak" || return
}

acquire_deploy_lock() {
  need_cmd flock || return
  exec 9>"$LOCK_FILE" || return
  flock -n 9 || { die "another deployment is already running; lock: $LOCK_FILE"; return 1; }
}

check_runtime_env() {
  load_env || return
  local required=(COMPOSE_PROJECT_NAME POSTGRES_USER POSTGRES_PASSWORD POSTGRES_DB DATABASE_URL DISTR_HOST REGISTRY_HOST JWT_SECRET RUSTFS_ACCESS_KEY RUSTFS_SECRET_KEY)
  local key value
  for key in "${required[@]}"; do
    value="${!key:-}"
    [[ -n "$value" && "$value" != *CHANGE_ME* ]] || {
      die "$key is unset or still contains CHANGE_ME"; return 1;
    }
  done
}

check_env() {
  check_runtime_env || return
  [[ "$DISTR_IMAGE_REF" == *'.dkr.ecr.'* && "$DISTR_IMAGE_REF" == *@sha256:* ]] || {
    die "DISTR_IMAGE_REF must be an ECR digest reference"; return 1;
  }
  [[ "$DISTR_RELEASE_COMMIT" =~ ^[0-9a-f]{40}$ ]] || return 1
  [[ "$DISTR_IMAGE_DIGEST" =~ ^sha256:[0-9a-f]{64}$ ]] || return 1
  [[ "$DISTR_IMAGE_REF" == *@"$DISTR_IMAGE_DIGEST" ]] || return 1
}

ecr_login() {
  local registry
  check_image_env || return
  need_cmd aws || return
  need_cmd docker || return
  registry="$(ecr_registry)" || return
  aws ecr get-login-password --region "$AWS_REGION" |
    docker login --username AWS --password-stdin "$registry" || return
}

compose_config() {
  check_env || return
  need_cmd docker || return
  compose config --quiet || return
}

pull_image() {
  check_env || return
  need_cmd docker || return
  ecr_login || return
  compose pull hub || return
  compose --profile migrate pull migrate || return
  compose --profile cleanup pull artifact-blob-cleanup || return
  compose --profile timestamp-operator pull timestamp-operator || return
}

start_dependencies() {
  check_runtime_env || return
  need_cmd docker || return
  compose up -d --wait --wait-timeout 180 postgres storage || return
}

stop_hub() {
  check_env || return
  need_cmd docker || return
  compose stop hub || return
}

start_hub() {
  check_env || return
  need_cmd docker || return
  compose up -d hub || return
}

run_migrations() {
  check_env || return
  need_cmd docker || return
  compose --profile migrate run --rm migrate || return
}

health() {
  check_env || return
  need_cmd curl || return
  local url="${DISTR_LOCAL_HEALTH_URL:-http://127.0.0.1:${DISTR_HTTP_PORT:-8080}/ready}" attempt
  for ((attempt=1; attempt<=60; attempt++)); do
    if curl -fsS "$url" >/dev/null; then return 0; fi
    sleep 2 || return
  done
  compose ps || true
  compose logs --tail=120 hub || true
  die "Hub did not become ready at $url"
  return 1
}
```

Apply the same explicit-return rule to `check_image_env`, `ecr_registry`, and `need_cmd`, which are transitively called above. Add a shell regression that makes each nested `docker`, `aws`, `curl`, `flock`, `stat`, `jq`, `sha256sum`, `pg_dump`, `pg_restore`, and `tar` call fail once and asserts that no later mutation event is recorded.

Implement the complete isolated-restore function as a subshell so its trap always removes both ephemeral containers and volumes:

```bash
backup_and_restore_timestamp_evidence() (
  set -Eeuo pipefail
  local evidence_dir="${1:?evidence directory is required}"
  local evidence_id="${2:-}" project source_object_volume host_uid host_gid ready attempt
  local database_backup object_backup database_restore_container
  local database_restore_volume object_restore_volume restore_password
  local source_object_checksum restored_object_checksum object_json
  local database_backup_name object_backup_name
  if [[ -z "$evidence_id" ]]; then
    evidence_id="$(fence_value FENCE_ID)" || return
  fi
  [[ "$evidence_id" =~ ^[A-Za-z0-9_+-]+$ ]] || return 1
  project="${COMPOSE_PROJECT_NAME:-distr-prod}"
  source_object_volume="${project}_rustfs"
  database_backup="$BACKUP_DIR/postgres-$evidence_id.dump"
  object_backup="$BACKUP_DIR/rustfs-$evidence_id.tar.gz"
  database_restore_container="${project}-timestamp-pg-$evidence_id"
  database_restore_volume="${project}_timestamp_pg_$evidence_id"
  object_restore_volume="${project}_timestamp_object_$evidence_id"
  restore_password="$evidence_id"
  object_json="$evidence_dir/object-restore-inspection.json"
  database_backup_name="$(basename -- "$database_backup")" || return
  object_backup_name="$(basename -- "$object_backup")" || return
  host_uid="${SUDO_UID:-$(id -u)}" || return
  host_gid="${SUDO_GID:-$(id -g)}" || return

  cleanup_timestamp_restore() {
    docker rm -f "$database_restore_container" >/dev/null 2>&1 || true
    docker volume rm -f "$database_restore_volume" >/dev/null 2>&1 || true
    docker volume rm -f "$object_restore_volume" >/dev/null 2>&1 || true
  }
  trap cleanup_timestamp_restore EXIT HUP INT TERM || return
  mkdir -p "$BACKUP_DIR" || return
  chmod 0700 "$BACKUP_DIR" || return
  prepare_timestamp_evidence_dir "$evidence_dir" || return
  [[ ! -e "$database_backup" && ! -e "$object_backup" ]] ||
    { die "evidence-specific backup already exists"; return 1; }
  docker volume inspect "$source_object_volume" >/dev/null ||
    { die "source object volume is missing"; return 1; }

  compose exec -T postgres sh -ceu '
    PGPASSWORD="$POSTGRES_PASSWORD" pg_dump \
      --username="$POSTGRES_USER" \
      --dbname="$POSTGRES_DB" \
      --format=custom
  ' >"$database_backup" || return
  chmod 0600 "$database_backup" || return
  write_sha256_sidecar_create_new "$database_backup" || return

  docker run --rm \
    -v "$source_object_volume:/data:ro" \
    -v "$BACKUP_DIR:/backup" \
    alpine:3.23 \
    tar -C /data -czf "/backup/$object_backup_name" . || return
  docker run --rm -v "$BACKUP_DIR:/backup" alpine:3.23 \
    chown "$host_uid:$host_gid" "/backup/$object_backup_name" || return
  chmod 0600 "$object_backup" || return
  write_sha256_sidecar_create_new "$object_backup" || return
  source_object_checksum="$(aggregate_volume_checksum "$source_object_volume")" || return

  docker volume create "$database_restore_volume" >/dev/null || return
  docker volume create "$object_restore_volume" >/dev/null || return
  docker run -d --name "$database_restore_container" \
    --network "${project}_default" \
    -v "$database_restore_volume:/var/lib/postgresql/data" \
    -v "$BACKUP_DIR:/backup:ro" \
    -e POSTGRES_USER=distr_restore \
    -e "POSTGRES_PASSWORD=$restore_password" \
    -e POSTGRES_DB=distr_restore \
    postgres:18.4-alpine3.23 >/dev/null || return
  ready=0
  for ((attempt=1; attempt<=60; attempt++)); do
    if docker exec "$database_restore_container" \
      pg_isready -U distr_restore -d distr_restore >/dev/null 2>&1; then
      ready=1
      break
    fi
    sleep 1 || return
  done
  ((ready == 1)) || { die "restored PostgreSQL did not become ready"; return 1; }
  docker exec "$database_restore_container" \
    pg_isready -U distr_restore -d distr_restore >/dev/null ||
    { die "restored PostgreSQL did not remain ready"; return 1; }
  docker exec -e "PGPASSWORD=$restore_password" \
    "$database_restore_container" \
    pg_restore --exit-on-error --no-owner --no-privileges \
      --username=distr_restore --dbname=distr_restore \
      "/backup/$database_backup_name" || return

  docker run --rm \
    -v "$object_restore_volume:/restore" \
    -v "$BACKUP_DIR:/backup:ro" \
    alpine:3.23 \
    tar -C /restore -xzf "/backup/$object_backup_name" || return
  restored_object_checksum="$(aggregate_volume_checksum "$object_restore_volume")" || return
  [[ "$source_object_checksum" == "$restored_object_checksum" ]] ||
    { die "restored object aggregate differs from source"; return 1; }

  run_timestamp_operator_with_database \
    "$evidence_dir" \
    "postgres://distr_restore:$restore_password@$database_restore_container:5432/distr_restore?sslmode=disable" \
    external-execution-timestamps inspect \
    --output /evidence/restore-inspection.json || return
  write_sha256_sidecar_create_new \
    "$evidence_dir/restore-inspection.json" || return

  local object_tmp
  object_tmp="$(mktemp "$evidence_dir/.object-restore.XXXXXX")" || return
  trap 'rm -f -- "${object_tmp:-}"; cleanup_timestamp_restore' \
    EXIT HUP INT TERM
  printf '{"sourceAggregateChecksum":"sha256:%s","restoredAggregateChecksum":"sha256:%s"}\n' \
    "$source_object_checksum" "$restored_object_checksum" >"$object_tmp" || return
  chmod 0600 "$object_tmp" || return
  copy_file_create_new_0600 "$object_tmp" "$object_json" || return
  write_sha256_sidecar_create_new "$object_json" || return
  rm -f "$object_tmp" || return
)
```

Use the same database/object backup and isolated-restore proof for an ordinary zero-history or later-schema release; only the evidence ID and retained directory differ:

```bash
backup_and_restore_release_evidence() {
  local stamp random release_id evidence_dir
  stamp="$(date -u +%Y%m%dT%H%M%SZ)" || return
  random="$(openssl rand -hex 8)" || return
  release_id="release_${stamp}_${random}"
  evidence_dir="$BACKUP_DIR/release-evidence-$stamp-$random"
  prepare_timestamp_evidence_dir "$evidence_dir" || return
  backup_and_restore_timestamp_evidence "$evidence_dir" "$release_id" || return
  run_timestamp_operator "$evidence_dir" \
    external-execution-timestamps inspect \
    --output /evidence/source-inspection.json || return
  write_sha256_sidecar_create_new \
    "$evidence_dir/source-inspection.json" || return
  compare_timestamp_inspections \
    "$evidence_dir/source-inspection.json" \
    "$evidence_dir/restore-inspection.json" \
    "$evidence_dir/object-restore-inspection.json" || return
  info "retained verified release evidence: $evidence_dir"
}

validate_source_inspection() {
  local source="${1:?source inspection is required}"
  local evidence_dir current
  evidence_dir="$(dirname -- "$source")" || return
  current="$evidence_dir/cancel-source-inspection.json"
  [[ ! -e "$current" ]] || { die "cancel inspection already exists"; return 1; }
  run_timestamp_operator "$evidence_dir" \
    external-execution-timestamps inspect \
    --output /evidence/cancel-source-inspection.json || return
  write_sha256_sidecar_create_new "$current" || return
  compare_timestamp_inspections "$source" "$current" \
    "$evidence_dir/object-restore-inspection.json" || return
}
```

Add `DISTR_RELEASE_COMMIT`, `DISTR_IMAGE_DIGEST`, `DISTR_TIMESTAMP_EVIDENCE_CHECKSUM`, and `DISTR_CALLBACK_PROBE_URL` to `.env.example` with non-secret `CHANGE_ME` examples. The digest is the `sha256:...` value embedded in `DISTR_IMAGE_REF`; the release commit is exactly 40 lowercase hex characters. Production `check_env` validates those formats without printing their values.

- [ ] **Step 4: Add executable safe-staging, active-fence, cleanup, and cancel cases.** Append these tests and invoke them after the four order tests:

```bash
test_approved_manifest_is_create_new_0600() {
  reset_stubs
  local source="$TMP/reviewed-approved.json"
  printf '{"id":"11111111-1111-4111-8111-111111111111"}\n' >"$source"
  # Exercise the real staging functions rather than the reset stub.
  unset -f stage_approved_manifest
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  stage_approved_manifest "$source" "$DISTR_TIMESTAMP_EVIDENCE_DIR"
  [[ "$(stat -c '%a' \
    "$DISTR_TIMESTAMP_EVIDENCE_DIR/approved-manifest.json")" == 600 ]]
  printf '{"id":"22222222-2222-4222-8222-222222222222"}\n' >"$source"
  if (stage_approved_manifest "$source" "$DISTR_TIMESTAMP_EVIDENCE_DIR"); then
    printf 'changed approved manifest unexpectedly replaced staged file\n' >&2
    return 1
  fi
  grep -q '11111111-1111-4111-8111-111111111111' \
    "$DISTR_TIMESTAMP_EVIDENCE_DIR/approved-manifest.json"
}

test_active_fence_refuses_every_mutating_command() {
  local command
  for command in up release deploy rollback cleanup-artifacts; do
    reset_stubs
    active_timestamp_fence(){ return 0; }
    if (
      case "$command" in
        rollback)
          dispatch_command rollback \
            'registry.example.invalid/distr@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
          ;;
        *) dispatch_command "$command" ;;
      esac
    ); then
      printf '%s unexpectedly accepted an active timestamp fence\n' \
        "$command" >&2
      return 1
    fi
    if grep -Eq '^(start|ordinary-migrate|ordinary-backup-|clear-fence)' \
      "$TMP/event-log"; then
      printf '%s mutated state after active-fence refusal\n' \
        "$command" >&2
      return 1
    fi
  done
}

test_cancel_clean_137_order() {
  reset_stubs
  timestamp_expand_cancel "$DISTR_TIMESTAMP_EVIDENCE_DIR"
  assert_events \
    'lock require-fence writers-stopped schema-137 source-validate set-source-image pull start health digest clear-fence'
}

test_cancel_refuses_schema_138() {
  reset_stubs
  require_clean_schema_137(){
    record schema-138
    return 42
  }
  if timestamp_expand_cancel "$DISTR_TIMESTAMP_EVIDENCE_DIR"; then
    printf 'cancel unexpectedly accepted schema 138\n' >&2
    return 1
  fi
  assert_events 'lock require-fence writers-stopped schema-138'
  ! grep -Eq '^(start|clear-fence)$' "$TMP/event-log"
}

test_fence_file_is_atomic_restricted_and_directory_bound() {
  reset_stubs
  unset -f active_timestamp_fence persist_timestamp_fence fence_value \
    require_timestamp_fence clear_timestamp_fence
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  local evidence="$TMP/fence-evidence" other="$TMP/other-evidence"
  local valid="$TMP/valid-fence"
  mkdir -p "$evidence" "$other"
  chmod 0700 "$evidence" "$other"
  rm -f "$TIMESTAMP_FENCE_FILE"
  persist_timestamp_fence PREPARING fence42 "$evidence" \
    'registry.invalid/distr@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' \
    'registry.invalid/distr@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'
  persist_timestamp_fence CAPTURED_WRITERS_STOPPED fence42 "$evidence" \
    'registry.invalid/distr@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' \
    'registry.invalid/distr@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'
  [[ "$(stat -c '%a' "$TIMESTAMP_FENCE_FILE")" == 600 ]]
  [[ "$(wc -l <"$TIMESTAMP_FENCE_FILE")" == 6 ]]
  require_timestamp_fence "$evidence"
  if (require_timestamp_fence "$other"); then return 1; fi
  cp "$TIMESTAMP_FENCE_FILE" "$valid"

  printf 'UNKNOWN=value\n' >>"$TIMESTAMP_FENCE_FILE"
  if (require_timestamp_fence "$evidence"); then return 1; fi
  cp "$valid" "$TIMESTAMP_FENCE_FILE"
  printf 'STATE=CAPTURED_WRITERS_STOPPED\n' >>"$TIMESTAMP_FENCE_FILE"
  if (require_timestamp_fence "$evidence"); then return 1; fi
  grep -v '^STATE=' "$valid" >"$TIMESTAMP_FENCE_FILE"
  if (require_timestamp_fence "$evidence"); then return 1; fi

  cp "$valid" "$TIMESTAMP_FENCE_FILE"
  chmod 0600 "$TIMESTAMP_FENCE_FILE"
  clear_timestamp_fence "$evidence"
  [[ ! -e "$TIMESTAMP_FENCE_FILE" ]]
}

test_operator_uses_deployment_identity_and_env_override() {
  reset_stubs
  local fakebin="$TMP/operator-fakebin" uid gid owner mode operator_lines
  mkdir -p "$fakebin"
  cat >"$fakebin/docker" <<'SH'
#!/usr/bin/env bash
printf '%s|%s\n' "${DISTR_COMPOSE_ENV_FILE:-}" "$*" >>"$TMP/operator-docker-log"
exit 0
SH
  chmod +x "$fakebin/docker"
  export PATH="$fakebin:$PATH" TMP
  unset DISTR_COMPOSE_ENV_FILE
  unset -f compose run_timestamp_operator run_timestamp_operator_with_database \
    run_migration_preflight run_timestamp_migration_138 \
    prepare_timestamp_evidence_dir
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  : >"$TMP/operator-docker-log"
  uid="$(id -u)"
  gid="$(id -g)"

  prepare_timestamp_evidence_dir "$TMP/operator-evidence"
  mode="$(stat -c '%a' "$TMP/operator-evidence")"
  owner="$(stat -c '%u:%g' "$TMP/operator-evidence")"
  [[ "$mode" == 700 && "$owner" == "$uid:$gid" ]]

  compose config
  run_timestamp_operator "$TMP/operator-evidence" \
    external-execution-timestamps validate-manifest --manifest /evidence/approved-manifest.json
  run_timestamp_operator_with_database "$TMP/operator-evidence" \
    'postgres://restore.invalid/distr' \
    external-execution-timestamps inspect --output /evidence/restore-inspection.json
  run_migration_preflight
  run_timestamp_migration_138 "$TMP/operator-evidence"

  while IFS='|' read -r propagated_env arguments; do
    [[ "$propagated_env" == "$ENV_FILE" ]] || return 1
  done <"$TMP/operator-docker-log"
  operator_lines="$(grep 'timestamp-operator' "$TMP/operator-docker-log")"
  [[ "$(grep -c -- "--user $uid:$gid" <<<"$operator_lines")" == 4 ]]
}

test_restore_failure_runs_cleanup_trap() {
  reset_stubs
  local fakebin="$TMP/fakebin"
  mkdir -p "$fakebin"
  cat >"$fakebin/docker" <<'SH'
#!/usr/bin/env bash
printf 'docker %s\n' "$*" >>"$TMP/docker-log"
case "$*" in
  *'pg_restore'*) exit 42 ;;
  *'tar -C /data -czf'*)
    : >"$BACKUP_DIR/rustfs-fence42.tar.gz"
    ;;
esac
exit 0
SH
  chmod +x "$fakebin/docker"
  export PATH="$fakebin:$PATH"
  export TMP BACKUP_DIR
  : >"$TMP/docker-log"
  unset -f backup_and_restore_timestamp_evidence
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  fence_value(){ printf '%s' fence42; }
  compose(){ printf 'database-backup'; }
  prepare_timestamp_evidence_dir(){ mkdir -p "$1"; chmod 0700 "$1"; }
  aggregate_volume_checksum(){
    printf '%064d\n' 0
  }
  if backup_and_restore_timestamp_evidence \
      "$DISTR_TIMESTAMP_EVIDENCE_DIR"; then
    printf 'restore unexpectedly succeeded\n' >&2
    return 1
  fi
  grep -q 'docker rm -f .*timestamp-pg-fence42' "$TMP/docker-log"
  grep -q 'docker volume rm -f .*timestamp_pg_fence42' \
    "$TMP/docker-log"
  grep -q 'docker volume rm -f .*timestamp_object_fence42' \
    "$TMP/docker-log"
}

test_pre_expand_rollback_refused_after_fence_clear() {
  reset_stubs
  rm -f "$TIMESTAMP_FENCE_FILE"
  export TIMESTAMP_COMPATIBILITY_FILE="$TMP/compatibility"
  cat >"$TIMESTAMP_COMPATIBILITY_FILE" <<'EOF'
SCHEMA_VERSION=138
EXPAND_IMAGE_DIGEST=registry.invalid/distr@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
PRE_EXPAND_IMAGE_DIGEST=registry.invalid/distr@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
MANIFEST_ID=11111111-1111-4111-8111-111111111111
CREATED_AT=2026-07-15T00:00:00Z
EOF
  chmod 0600 "$TIMESTAMP_COMPATIBILITY_FILE"
  current_schema_version(){ printf 138; }
  if (require_rollback_schema_compatibility \
      'registry.invalid/distr@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'); then
    printf 'pre-expand rollback unexpectedly accepted after fence clear\n' >&2
    return 1
  fi
}

test_schema_139_rollback_fails_closed_before_mutation() {
  reset_stubs
  export TIMESTAMP_COMPATIBILITY_FILE="$TMP/compatibility-139"
  cat >"$TIMESTAMP_COMPATIBILITY_FILE" <<'EOF'
SCHEMA_VERSION=138
EXPAND_IMAGE_DIGEST=registry.invalid/distr@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
PRE_EXPAND_IMAGE_DIGEST=registry.invalid/distr@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
MANIFEST_ID=11111111-1111-4111-8111-111111111111
CREATED_AT=2026-07-15T00:00:00Z
EOF
  chmod 0600 "$TIMESTAMP_COMPATIBILITY_FILE"
  check_env(){ :; }
  current_schema_version(){ printf 139; }
  if rollback_app \
      'registry.invalid/distr@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'; then
    printf 'schema-139 rollback unexpectedly accepted schema-138 metadata\n' >&2
    return 1
  fi
  assert_events 'lock'
  ! grep -Eq '^(set-source-image|pull|start)$' "$TMP/event-log"
}

test_rollback_calls_compatibility_gate_before_mutation() {
  reset_stubs
  check_env(){ :; }
  require_rollback_schema_compatibility(){ record rollback-gate; return 42; }
  if rollback_app \
      'registry.invalid/distr@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'; then
    return 1
  fi
  assert_events 'lock rollback-gate'
  [[ " ${events[*]} " != *' set-source-image '* &&
     " ${events[*]} " != *' pull '* && " ${events[*]} " != *' start '* ]]
}

test_approved_manifest_is_create_new_0600
test_active_fence_refuses_every_mutating_command
test_cancel_clean_137_order
test_cancel_refuses_schema_138
test_fence_file_is_atomic_restricted_and_directory_bound
test_operator_uses_deployment_identity_and_env_override
test_restore_failure_runs_cleanup_trap
test_pre_expand_rollback_refused_after_fence_clear
test_schema_139_rollback_fails_closed_before_mutation
test_rollback_calls_compatibility_gate_before_mutation
```

Refactor the current command `case` into `dispatch_command`. Its first action calls `require_no_active_timestamp_fence` for every mutating command except `timestamp-expand-apply` and `timestamp-expand-cancel`. Keep the `DISTR_DEPLOY_LIB_ONLY` return immediately before the one production call `dispatch_command "$@"`. Every safety check inside capture/apply/cancel uses `command || return` so Bash conditional-call semantics cannot disable `errexit` and continue after a failed guard.

- [ ] **Step 5: Run the shell test and confirm green.**

```powershell
bash hack/test-server-compose-timestamp-expand.sh
```

Expected: `timestamp expand compose orchestration tests passed`.

- [ ] **Step 6: Validate the real Compose rendering and commit.**

```powershell
bash hack/test-server-compose-timestamp-expand.sh
$env:DISTR_COMPOSE_ENV_FILE = (Resolve-Path deploy/server-docker-compose/.env.example).Path
try {
  docker compose --env-file deploy/server-docker-compose/.env.example -f deploy/server-docker-compose/docker-compose.yml config --quiet
} finally {
  Remove-Item Env:DISTR_COMPOSE_ENV_FILE -ErrorAction SilentlyContinue
}
git diff --check
git add deploy/server-docker-compose hack/test-server-compose-timestamp-expand.sh
git commit -m "feat: stage timestamp expand deployment"
```

## Task 10: Add PostgreSQL 16/18 Gates and Generic Operator Documentation

**Files:**

- Modify: `hack/pr054a-validate-timestamp-expand.mjs`
- Modify: `.github/workflows/community-release-hardening.yaml`
- Modify: `docs/release/community-release-readiness.md`
- Modify: `docs/upgrade/community-release-upgrade-checklist.md`
- Modify: `docs/operations/server-docker-compose-deploy.md`
- Modify: `docs/operations/operator-smoke-test.md`
- Modify: `docs/security/release-hardening-checklist.md`
- Modify: `docs/fork/PR-054A_EXTERNAL_EXECUTION_TIMESTAMP_EXPAND.md`
- Modify: `docs/fork/FORK_DIFF_INDEX.md`
- Modify: `docs/fork/UPGRADE_GUIDE.md`

**Interfaces:**

- Consumes: the migration, manifest, CLI, startup guard, and fenced deployment behavior verified in Tasks 1-9.
- Produces: a focused PostgreSQL 16.14/18.4 CI gate, executable documentation validation, and the community-neutral operating procedure consumed by Task 11.
- Preserves: the existing full release job on `postgres:18.4-alpine3.23`; the matrix does not duplicate Angular, container builds, vulnerability scans, or the live demo.
- Excludes: adopter names, client repositories/databases, credentials, host addresses, live manifests, and tutorial/demo cleanup.

- [ ] **Step 1: Extend the PR-054A validator before editing workflow or documentation.**

Insert the following immediately before the final `console.log` in `hack/pr054a-validate-timestamp-expand.mjs`:

```js
const requireSnippets = (file, snippets) => {
  const text = read(file);
  for (const snippet of snippets) {
    if (!text.includes(snippet)) fail(`${file}: missing ${snippet}`);
  }
};

const workflowFile = '.github/workflows/community-release-hardening.yaml';
const workflow = read(workflowFile);
if (!workflow.includes('\n  timestamp-expand-postgresql:\n')) {
  fail(`${workflowFile}: missing timestamp-expand-postgresql job`);
}
requireSnippets(workflowFile, [
  'postgres_image: postgres:16.14-alpine3.23',
  'postgres_image: postgres:18.4-alpine3.23',
  'node hack/pr054a-validate-timestamp-expand.mjs',
  'bash hack/test-server-compose-timestamp-expand.sh',
  'go test -p=1 ./internal/externalexecutiontimestamp ./internal/migrations ./internal/db ./internal/hubexecutor ./internal/mapping ./cmd/hub/cmd -count=1 -timeout 30m',
]);
const releaseGateStart = workflow.indexOf('\n  release-gates:\n');
if (releaseGateStart < 0) fail(`${workflowFile}: missing release-gates job`);
if (!workflow.slice(releaseGateStart).includes('image: postgres:18.4-alpine3.23')) {
  fail(`${workflowFile}: release-gates must remain pinned to postgres:18.4-alpine3.23`);
}

const documentRequirements = new Map([
  [
    'docs/release/community-release-readiness.md',
    [
      '## External-Execution Timestamp Expand Gate',
      'A component release never implicitly deploys another component.',
      '`serve --migrate=false`',
    ],
  ],
  [
    'docs/upgrade/community-release-upgrade-checklist.md',
    ['## Migration 138 Decision Path', '`migrate --to 137` is intentionally refused'],
  ],
  [
    'docs/operations/server-docker-compose-deploy.md',
    [
      '## External-Execution Timestamp Expand (Migration 138)',
      'timestamp-expand-capture',
      'timestamp-expand-apply',
      'DISTR_COMPOSE_ENV_FILE',
      '--user "$(id -u):$(id -g)"',
    ],
  ],
  [
    'docs/operations/operator-smoke-test.md',
    [
      '## Timestamp Expand Smoke',
      'manifest state is `VERIFIED`',
      'DISTR_COMPOSE_ENV_FILE',
      '--user "$(id -u):$(id -g)"',
    ],
  ],
  [
    'docs/security/release-hardening-checklist.md',
    ['## Timestamp Evidence Safety', 'Vulnerability, license, and secret scanners remain mandatory'],
  ],
  [
    'docs/fork/PR-054A_EXTERNAL_EXECUTION_TIMESTAMP_EXPAND.md',
    ['## Operator Evidence and Release Record', 'PostgreSQL 16.14 and PostgreSQL 18.4'],
  ],
  ['docs/fork/FORK_DIFF_INDEX.md', ['### PR-054A - External-execution timestamp expand', 'Migration 138']],
  [
    'docs/fork/UPGRADE_GUIDE.md',
    ['## Current Timestamp Procedure', 'community-release-upgrade-checklist.md#migration-138-decision-path'],
  ],
]);
for (const [file, snippets] of documentRequirements) requireSnippets(file, snippets);

const extractSection = (file, heading) => {
  const text = read(file);
  const start = text.indexOf(heading);
  if (start < 0) fail(`${file}: missing section ${heading}`);
  const level = heading.match(/^#+/)[0].length;
  const remainder = text.slice(start + heading.length);
  const next = new RegExp(`\\n#{1,${level}} `).exec(remainder);
  const end = next === null ? text.length : start + heading.length + next.index;
  return text.slice(start, end).toLowerCase();
};
const neutralSections = [
  ['docs/release/community-release-readiness.md', '## External-Execution Timestamp Expand Gate'],
  ['docs/upgrade/community-release-upgrade-checklist.md', '## Migration 138 Decision Path'],
  ['docs/operations/server-docker-compose-deploy.md', '## External-Execution Timestamp Expand (Migration 138)'],
  ['docs/operations/operator-smoke-test.md', '## Timestamp Expand Smoke'],
  ['docs/security/release-hardening-checklist.md', '## Timestamp Evidence Safety'],
  ['docs/fork/PR-054A_EXTERNAL_EXECUTION_TIMESTAMP_EXPAND.md', '## Operator Evidence and Release Record'],
  ['docs/fork/FORK_DIFF_INDEX.md', '### PR-054A - External-execution timestamp expand'],
  ['docs/fork/UPGRADE_GUIDE.md', '## Current Timestamp Procedure'],
];
const forbiddenPatterns = [
  ['IPv4 address', /\b(?:\d{1,3}\.){3}\d{1,3}\b/],
  ['non-example email', /\b[A-Z0-9._%+-]+@(?!example\.(?:com|invalid)\b)[A-Z0-9.-]+\.[A-Z]{2,}\b/i],
  ['local user path', /\b[A-Z]:\\Users\\/i],
  ['credential assignment', /\b(?:password|secret|token)\s*[:=]\s*(?!CHANGE_ME\b)\S+/i],
];
for (const [file, heading] of neutralSections) {
  const section = extractSection(file, heading);
  for (const [label, pattern] of forbiddenPatterns) {
    if (pattern.test(section)) fail(file + ': ' + heading + ' contains ' + label);
  }
}
```

- [ ] **Step 2: Run the validator and confirm the red state.**

Run:

```powershell
node hack/pr054a-validate-timestamp-expand.mjs
```

Expected: nonzero exit containing:

```text
.github/workflows/community-release-hardening.yaml: missing timestamp-expand-postgresql job
```

- [ ] **Step 3: Add the fast validators and complete PostgreSQL matrix job.**

In `.github/workflows/community-release-hardening.yaml`, add these steps immediately after `Validate PR-050 release hardening package` in `fast-release-package`:

```yaml
- name: Validate PR-054A timestamp expand package
  run: node hack/pr054a-validate-timestamp-expand.mjs
- name: Run PR-054A security regression suite
  run: node hack/pr054a-validate-timestamp-expand.test.mjs
- name: Validate timestamp expand Compose orchestration
  run: bash hack/test-server-compose-timestamp-expand.sh
```

Insert this complete job between `fast-release-package` and `release-gates`:

```yaml
timestamp-expand-postgresql:
  name: Timestamp expand / PostgreSQL ${{ matrix.postgres_version }}
  runs-on: ubuntu-latest
  timeout-minutes: 35
  permissions:
    contents: read
  strategy:
    fail-fast: false
    matrix:
      include:
        - postgres_version: '16.14'
          postgres_image: postgres:16.14-alpine3.23
        - postgres_version: '18.4'
          postgres_image: postgres:18.4-alpine3.23
  services:
    postgres:
      image: ${{ matrix.postgres_image }}
      env:
        POSTGRES_USER: local
        POSTGRES_PASSWORD: local
        POSTGRES_DB: distr
      options: >-
        --health-cmd pg_isready
        --health-interval 10s
        --health-timeout 5s
        --health-retries 5
      ports:
        - 5432:5432
  env:
    DATABASE_URL: postgres://local:local@localhost:5432/distr?sslmode=disable
    DISTR_TEST_DATABASE_URL: postgres://local:local@localhost:5432/distr?sslmode=disable
  steps:
    - uses: actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10 # v6.0.3
    - name: Setup Go
      uses: actions/setup-go@4a3601121dd01d1626a1e23e37211e3254c1c06c # v6.4.0
      with:
        go-version-file: 'go.mod'
        check-latest: true
        cache-dependency-path: |
          go.sum
    - name: Validate migration sequence
      run: bash hack/validate-migrations.sh
    - name: Run timestamp expand migration and compatibility tests
      run: go test -p=1 ./internal/externalexecutiontimestamp ./internal/migrations ./internal/db ./internal/hubexecutor ./internal/mapping ./cmd/hub/cmd -count=1 -timeout 30m
```

Do not alter the existing `release-gates` PostgreSQL service image; it remains:

```yaml
image: postgres:18.4-alpine3.23
```

- [ ] **Step 4: Add the release and upgrade decision text.**

Insert the following in `docs/release/community-release-readiness.md` immediately before `## Release Gate Checklist`:

```markdown
## External-Execution Timestamp Expand Gate

Migration 138 is a Distr control-plane expand release. It retains every legacy external-execution timestamp, adds
nullable instant shadows and immutable provenance, and leaves public API fields and agent behavior unchanged.
PostgreSQL compatibility is gated on the exact images `postgres:16.14-alpine3.23` and
`postgres:18.4-alpine3.23`.

Before deployment, retain one release record containing:

- source commit and reviewed change range;
- immutable Hub image digest;
- schema version before and after deployment;
- manifest ID, raw-cell checksum, decision-content checksum, and database-identity checksum;
- database and object-store backup checksums;
- isolated-restore verification checksums;
- component release identity, dependency manifest identity, operator, reviewer, and timestamps; and
- previous-known-good image digest and recovery evidence.

A component release never implicitly deploys another component. Each component has its own immutable release and
change log. A coordinated rollout uses an explicit dependency DAG or product manifest whose reviewed entries name
the exact component releases; dependency relationships never trigger hidden deployments.

The migration decision path is fixed:

1. Run the read-only `distr migrate --check`.
2. If the result proves zero external-execution history, the ordinary release may stop writers, back up, run the
   explicit migration, and start the Hub with `serve --migrate=false`.
3. If the database is non-empty at exact schema 137, use `timestamp-expand-capture`, retain the backup and isolated
   restore evidence, independently review and seal the complete manifest, then use `timestamp-expand-apply`.
4. Require schema 138, a clean migration state, a `VERIFIED` manifest or durable zero-history proof, a matching image
   digest, and successful smoke checks before releasing the writer fence.

`UNRESOLVED` cells remain visible with null shadows and fail closed. Expand acceptance does not claim contract
eligibility and does not authorize a contract migration.
```

Insert the following in `docs/upgrade/community-release-upgrade-checklist.md` immediately before `## Before Upgrade`:

```markdown
## Migration 138 Decision Path

This section overrides the generic migration order only when crossing from schema 137 to migration 138.

1. Run `distr migrate --check` while the current Hub is still running.
2. For proven zero history, stop/fence writers, verify they are stopped, back up PostgreSQL and object storage,
   verify both backups through isolated restore, run the explicit migration, and start only the expand-compatible
   Hub with `serve --migrate=false`.
3. For a non-empty schema 137 database, run `timestamp-expand-capture`. Keep the Hub stopped while an independent
   reviewer classifies every cell and seals the complete manifest.
4. Run `timestamp-expand-apply`; it revalidates the fence and evidence, performs a dry run, migrates to 138, applies
   and verifies provenance, checks startup compatibility, starts the digest-pinned Hub, and clears the fence only
   after health and history checks pass.
5. Retain the evidence directory and previous-known-good image until release acceptance.

Before migration 138 starts, `timestamp-expand-cancel` may resume the previous image only when schema 137 and the
captured database identity remain unchanged. After an `APPLIED` or `VERIFIED` manifest exists,
`migrate --to 137` is intentionally refused. Recovery then completes the expand release or restores the verified
database and object-store backups before the previous image resumes.

Unresolved cells remain fail-closed and do not block the additive expand schema. They do block the separately
planned contract release.
```

- [ ] **Step 5: Add the exact Compose operations and smoke procedure.**

In `docs/operations/server-docker-compose-deploy.md`, replace the existing paragraph beginning with
`` `release` locks deployment `` with:

```markdown
`release` first runs the read-only migration check. When the check allows an ordinary release, the script locks
deployment, validates Compose, pulls the digest-pinned image, starts dependencies, stops and fences Hub writers,
verifies they are stopped, backs up PostgreSQL and object storage, migrates explicitly, starts Hub with
`serve --migrate=false`, and runs the health check. A non-empty schema 137 database is refused before the Hub is
stopped and must use the staged migration-138 procedure below.
```

Immediately after that paragraph and before `## Optional Server Build`, insert:

````markdown
## External-Execution Timestamp Expand (Migration 138)

Set `DISTR_TIMESTAMP_EVIDENCE_DIR` to a protected empty directory on the deployment host, create it with mode
`0700`, and retain it through release acceptance:

```bash
install -d -m 0700 "$DISTR_TIMESTAMP_EVIDENCE_DIR"
./deploy/server-docker-compose/deploy.sh timestamp-expand-capture "$DISTR_TIMESTAMP_EVIDENCE_DIR"
```

Capture acquires the deployment lock, persists the writer fence, stops every Hub writer, creates checksummed
PostgreSQL and object-store backups, restores both into isolated temporary resources, compares restored and source
identity, writes the complete draft manifest, removes temporary restore resources, and leaves Hub stopped.

An independent reviewer copies `draft-manifest.json` to `reviewed-draft.json`, records one decision for every cell,
and supplies named author/reviewer identities plus a checksummed evidence reference. Seal the reviewed file with
the same digest-pinned image:

```bash
export DISTR_COMPOSE_ENV_FILE="$(realpath deploy/server-docker-compose/.env)"
docker compose \
  --env-file "$DISTR_COMPOSE_ENV_FILE" \
  -f deploy/server-docker-compose/docker-compose.yml \
  --profile timestamp-operator \
  run --rm --user "$(id -u):$(id -g)" timestamp-operator \
  external-execution-timestamps seal-manifest \
  --input /evidence/reviewed-draft.json \
  --output /evidence/approved-manifest.json \
  --author "$DISTR_TIMESTAMP_AUTHOR" \
  --reviewer "$DISTR_TIMESTAMP_REVIEWER" \
  --evidence-reference "$DISTR_TIMESTAMP_EVIDENCE_REFERENCE" \
  --evidence-checksum "$DISTR_TIMESTAMP_EVIDENCE_CHECKSUM" \
  --target-commit "$DISTR_RELEASE_COMMIT" \
  --target-image-digest "$DISTR_IMAGE_DIGEST"
```

Apply only the sealed manifest from the active evidence directory:

```bash
export DISTR_TIMESTAMP_APPROVED_MANIFEST="$DISTR_TIMESTAMP_EVIDENCE_DIR/approved-manifest.json"
./deploy/server-docker-compose/deploy.sh \
  timestamp-expand-apply \
  "$DISTR_TIMESTAMP_APPROVED_MANIFEST" \
  "$DISTR_TIMESTAMP_EVIDENCE_DIR"
```

Apply rechecks the active fence, backup and isolated-restore evidence, target commit and image digest, runs
`validate-manifest`, runs a non-mutating apply preview, executes bounded `migrate --to 138`, applies and verifies
the immutable ledger, checks expand startup compatibility, starts only `serve --migrate=false`, and clears the
fence only after post-start checks pass.

Before migration starts, cancel is allowed only for unchanged clean schema 137:

```bash
./deploy/server-docker-compose/deploy.sh timestamp-expand-cancel "$DISTR_TIMESTAMP_EVIDENCE_DIR"
```

After manifest application, do not run a schema down migration. The legacy columns remain the expand release's
read path, but operational recovery uses the retained previous image only with a compatible schema or restores the
verified PostgreSQL and object-store backups first. Timestamp migration, audit retention, and tutorial/demo cleanup
are separate operations.
````

Insert the following in `docs/operations/operator-smoke-test.md` immediately before
`## Upgrade and Rollback Notes`:

````markdown
## Timestamp Expand Smoke

After `timestamp-expand-apply`, run:

```bash
./deploy/server-docker-compose/deploy.sh health
./deploy/server-docker-compose/deploy.sh ps
export DISTR_COMPOSE_ENV_FILE="$(realpath deploy/server-docker-compose/.env)"
docker compose \
  --env-file "$DISTR_COMPOSE_ENV_FILE" \
  -f deploy/server-docker-compose/docker-compose.yml \
  --profile timestamp-operator \
  run --rm --user "$(id -u):$(id -g)" timestamp-operator \
  external-execution-timestamps verify \
  --manifest-id "$DISTR_TIMESTAMP_MANIFEST_ID"
```

Confirm all of the following before accepting the release:

- `/ready` succeeds and the running Hub uses the reviewed immutable image digest;
- `schema_migrations` reports clean schema 138;
- the manifest state is `VERIFIED`, or the durable state proves zero history;
- execution and event counts, IDs, statuses, sequences, messages, hashes, and references match pre-release evidence;
- every converted shadow reproduces from its raw value and explicit offset;
- unresolved cells remain null and visible as `UNRESOLVED`;
- no duplicate callback sequence or unexpected task lock exists;
- login, execution-history reads, task progress, and audit views remain available; and
- the evidence directory, backup checksums, restore checksums, fence record, and previous image digest are retained.

This smoke test does not authorize deleting execution history, provenance, audit records, or timestamp evidence.
````

- [ ] **Step 6: Add security, fork evidence, and the historical-guide pointer.**

In `docs/security/release-hardening-checklist.md`, add this row after `Compatibility metadata`:

```markdown
| Timestamp provenance | Historical wall clocks are converted only with explicit per-cell evidence; provenance is append-only. | `internal/externalexecutiontimestamp`, timestamp migration/repository tests |
```

Replace the first `Required Scans` command block with:

````markdown
```shell
node hack/pr050-validate-release-hardening.mjs
node hack/pr054a-validate-timestamp-expand.mjs
bash hack/validate-migrations.sh
bash hack/test-server-compose-timestamp-expand.sh
go test -p=1 ./internal/externalexecutiontimestamp ./internal/migrations ./internal/db ./internal/hubexecutor ./internal/mapping ./cmd/hub/cmd -count=1 -timeout 30m
go test -p=1 ./...
go vet ./...
pnpm run lint
git diff --check
```
````

Insert this section immediately before `## Secret Handling Rules`:

```markdown
## Timestamp Evidence Safety

- Manifest and provenance reports contain identifiers, counts, decisions, and checksums only; they do not contain
  DSNs, credentials, payloads, messages, tokens, passwords, or private absolute paths.
- Draft, reviewed, approved, backup, restore, and fence artifacts use restrictive permissions and are retained
  outside the source repository.
- `UNRESOLVED` is not converted into UTC by deployment approval.
- Provenance rows are append-only; corrections use a new complete superseding manifest.
- Vulnerability, license, and secret scanners remain mandatory and are not suppressed or allowlisted for this
  release.
- Timestamp evidence is control-plane metadata and never authorizes access to an adopter workload database.
```

Append this section to `docs/fork/PR-054A_EXTERNAL_EXECUTION_TIMESTAMP_EXPAND.md`:

```markdown
## Operator Evidence and Release Record

- Compatibility gate: PostgreSQL 16.14 and PostgreSQL 18.4.
- Database impact: additive migration 138; six nullable instant shadows, future indexes, manifest/provenance tables,
  contract-gate foundation, and durable zero-history state.
- API impact: none; expand reads and public JSON remain on the legacy columns.
- UI impact: none.
- Agent protocol impact: none.
- Deployment impact: non-empty schema 137 databases require fenced capture, backup and isolated restore, independent
  manifest review, explicit migration, apply/verify, and `serve --migrate=false`.
- Rollback limit: schema 138 may return to 137 only before any manifest is applied; afterward recovery uses retained
  legacy columns with a compatible binary or verified backup restore.
- Contract status: excluded from this PR; unresolved cells remain fail-closed.

The retained release record names the source commit, image digest, schema before/after, manifest and database
identity checksums, backup and restore checksums, component release identity, dependency manifest identity,
operator, reviewer, and previous-known-good digest. It contains no adopter credentials or workload data.
```

Normalize the PR-054A block in `docs/fork/FORK_DIFF_INDEX.md` to:

```markdown
### PR-054A - External-execution timestamp expand

- Status: Implemented locally; independent review and PostgreSQL matrix verification are required before release.
- Feature flag: None.
- User-facing behavior: None. Existing external-execution API fields and null behavior remain unchanged.
- Database changes: Migration 138 adds nullable instant shadows, paired future defaults, future indexes, immutable
  timestamp manifest/provenance metadata, contract-gate foundation, and durable zero-history proof.
- API changes: None.
- UI changes: None.
- Agent protocol changes: None.
- Documentation: Added ADR-0055, the approved hybrid design, fenced Compose procedure, release/upgrade/smoke/security
  guidance, and PostgreSQL 16.14/18.4 gates.
- Tests: Canonical conversion, migration, inspection, sealing, apply/verify, downgrade refusal, dual-write, startup
  compatibility, and Compose orchestration tests run against pinned PostgreSQL 16.14 and 18.4.
- Upstream contribution notes: Community-neutral control-plane migration; no adopter repository, application
  database, credentials, host names, or cleanup behavior.
- Compatibility notes: Expand continues reading legacy timestamps and writes paired legacy/instant values. Contract
  eligibility and canonical instant reads are separate later work.
```

Insert this pointer in `docs/fork/UPGRADE_GUIDE.md` immediately before `## PR-049 Compatibility Metadata`; leave the
existing PR-049 content unchanged:

```markdown
## Current Timestamp Procedure

Migration 138 uses the current
[Community Release Upgrade Checklist](../upgrade/community-release-upgrade-checklist.md#migration-138-decision-path).
The remainder of this file is the historical PR-049/schema-131 compatibility guide and does not override the
migration-138 fence, manifest, backup, restore, or downgrade rules.
```

- [ ] **Step 7: Run the expanded validator and confirm green.**

Run:

```powershell
node hack/pr054a-validate-timestamp-expand.mjs
```

Expected:

```text
PR-054A timestamp allocation validation passed
```

- [ ] **Step 8: Run documentation, orchestration, migration, and formatting gates.**

Run:

```powershell
node hack/pr050-validate-release-hardening.mjs
bash hack/validate-migrations.sh
bash hack/test-server-compose-timestamp-expand.sh
pnpm exec prettier --check .github/workflows/community-release-hardening.yaml docs/release/community-release-readiness.md docs/upgrade/community-release-upgrade-checklist.md docs/operations/server-docker-compose-deploy.md docs/operations/operator-smoke-test.md docs/security/release-hardening-checklist.md docs/fork/PR-054A_EXTERNAL_EXECUTION_TIMESTAMP_EXPAND.md docs/fork/FORK_DIFF_INDEX.md docs/fork/UPGRADE_GUIDE.md
git diff --check
```

Expected output includes:

```text
PR-050 release hardening validation passed
Validating migrations 0 through 138
Success: All migration files are properly paired
timestamp expand compose orchestration tests passed
All matched files use Prettier code style!
```

Expected from `git diff --check`: no output and exit code 0.

The new GitHub matrix must run this exact command successfully in both
`Timestamp expand / PostgreSQL 16.14` and `Timestamp expand / PostgreSQL 18.4`:

```bash
go test -p=1 ./internal/externalexecutiontimestamp ./internal/migrations ./internal/db ./internal/hubexecutor ./internal/mapping ./cmd/hub/cmd -count=1 -timeout 30m
```

Expected: each matrix leg exits 0 with only `ok` package results and no `FAIL`.

- [ ] **Step 9: Commit the CI and community-neutral operating package.**

```powershell
git add hack/pr054a-validate-timestamp-expand.mjs .github/workflows/community-release-hardening.yaml docs/release/community-release-readiness.md docs/upgrade/community-release-upgrade-checklist.md docs/operations/server-docker-compose-deploy.md docs/operations/operator-smoke-test.md docs/security/release-hardening-checklist.md docs/fork/PR-054A_EXTERNAL_EXECUTION_TIMESTAMP_EXPAND.md docs/fork/FORK_DIFF_INDEX.md docs/fork/UPGRADE_GUIDE.md
git commit -m "docs: standardize timestamp expand operations"
```

Expected: one new commit with subject `docs: standardize timestamp expand operations`.

## Task 11: Run the Expand Release Acceptance Gate

**Files:**

- Modify only files required to fix evidence-backed defects found by this gate.

- [ ] Confirm the diff contains no contract migration, canonical instant reads, global pgx timestamp codec, Force path, adopter name/host/credential, client DB access, or demo cleanup.

```powershell
git diff 35318e6d40e66c15c1fd95091b16605a9a618d20 HEAD --stat
$implementationDiff = git diff 35318e6d40e66c15c1fd95091b16605a9a618d20 HEAD -- internal cmd deploy .github
if ($implementationDiff | Select-String -Pattern 'prepare-contract|recover-dirty|\.Force\(') { throw 'contract or Force implementation leaked into expand release' }
node hack/pr054a-validate-timestamp-expand.test.mjs
if ($LASTEXITCODE -ne 0) { throw 'CI-enforced security regression suite failed' }
$baseObject = '35318e6d40e66c15c1fd95091b16605a9a618d20'
$headCommit = (git rev-parse HEAD).Trim()
node hack/pr054a-validate-timestamp-expand.mjs --scan-privacy-diff $baseObject $headCommit --require-private-scope
if ($LASTEXITCODE -ne 0) { throw 'privacy diff gate failed' }
```

The public workflow runs independent secret and privacy gates over the event commit history. The first installs the official
Gitleaks v8.30.1 default rules archive for Linux x64, verifies SHA-256
`551f6fc83ea457d62a0d98237cbad105af8d557003051f41f3e7ca7b3f2470eb` before extraction, uses only a protected
temporary `[extend] useDefault = true` configuration and empty ignore file, disables inline allow comments, and scans
the verified checkout `HEAD` (including the pull-request synthetic merge) rather than trusting branch-head metadata.
Every Gitleaks range uses fixed `--full-history --root --diff-merges=separate --no-ext-diff --no-textconv` history
options: pull requests are base-to-execution-head, normal pushes are before-to-execution-head, and all-zero initial
pushes are complete execution-head history.
It never uses a baseline, repository config/ignore file, path exclusion, custom allowlist, or disabled default rule.
CI suppresses scanner detail and returns a fixed failure; any authorized local remediation rerun must keep full
redaction enabled.

The fast release job runs `node hack/pr054a-validate-timestamp-expand.test.mjs` as a CI-enforced security regression suite.
It must pass before the PostgreSQL-backed release aggregator can authorize publication.

The required `release-gates` aggregator runs with `always()` and its first step asserts that both
`fast-release-package` and `timestamp-expand-postgresql` completed with `success`. A failed, cancelled, or skipped
prerequisite therefore produces a failed required aggregate check instead of silently skipping the release gate.
For a non-ancestor force push, the whitespace gate fetches the exact `before` object if it is not already present and
uses a direct before-to-execution-head comparison; it never depends on a triple-dot merge base. If the old object
cannot be fetched, the release fails closed before publication.

The second gate is the validator CLI mode
`--scan-privacy-diff <base-object> <head-commit> [--require-private-scope]`. It enumerates the selected commits in
reverse topological order, scans every parent-to-commit edge (empty-tree-to-root for root commits), and then scans the
aggregate base-to-head diff. Every path-scoped Git call uses literal pathspec semantics, disables external diff and
text conversion, and disables rename folding so newly introduced pathnames remain explicit. It scans added pathnames,
added content, and symlink target blobs; ignores deleted text and diff metadata; and fails closed on changed gitlinks,
changed binary files, malformed Git evidence, or unsupported modes. It reports only a category, redacted-or-safe
repository path, and new-side line number; it never prints matched values, private tokens, or the private canary.

Email controls accept RFC 2606 reserved domains (`.test`, `.example`, `.invalid`, and `.localhost`) plus
`example.com`, `example.net`, `example.org`, and their subdomains, while rejecting suffix tricks at complete label
boundaries. Address controls reject user-home/UNC paths and valid literal IPv4 values in host/address/URL contexts,
except loopback, the exact unspecified bind, RFC 5737 documentation ranges, and explicit non-address version context.
`DISTR_PRIVATE_SCOPE_TOKENS` is a newline-delimited list of literal private tokens injected only by the
adopter-private release workspace and compared case-insensitively to paths and added content. The private workspace
also supplies `DISTR_PRIVATE_SCOPE_CANARY`; the gate must prove that at least one configured literal token matches the
canary before scanning. Both remain mandatory for the local release command above. Missing, no-op, oversized, or
unproved private configuration fails without echoing any token or the canary. Expected: no contract/Force
implementation and no newly added secret, local identity, private address, adopter-specific value, or unscannable
binary artifact.

Before any publication, construct a fresh publish branch whose final tree is byte-identical to the accepted source
tree and whose history is exactly one non-merge child of the approved public baseline. Run the checksum-pinned
Gitleaks gate and the semantic privacy CLI locally on that unpublished commit, with the required private tokens and
canary, before the first push. Push only the clean publish branch; retain the multi-commit development history locally
as evidence and never upload it to the public remote.

- [ ] Run the focused PostgreSQL suite on both pinned versions and retain the job output.
- [ ] Verify the required release aggregate runs after every prerequisite result and fails unless both prerequisite jobs succeeded.
- [ ] Run the CI-enforced security regression suite, migration validation, deployment adapter tests, full serialized Go tests, vet, release lint, Angular tests/build, community Hub/agent builds, the checksum-pinned Gitleaks default-rules scan, the privacy diff CLI, production dependency audit, license scan, and the unchanged Go vulnerability scan.
- [ ] Verify the unpublished clean publish branch is one non-merge child of the approved public baseline, is tree-identical to the accepted source, and passes both privacy gates before its first push.

```powershell
bash hack/validate-migrations.sh
bash hack/test-server-compose-timestamp-expand.sh
go test -p=1 ./internal/externalexecutiontimestamp ./internal/migrations ./internal/db ./internal/hubexecutor ./internal/mapping ./cmd/hub/cmd -count=1 -timeout 30m
go test -p=1 ./... -count=1 -timeout 60m
go vet ./...
golangci-lint run --config=.golangci.release.yml ./...
pnpm run test
pnpm run build:community
go build ./cmd/hub
go build ./cmd/agent/docker
go build ./cmd/agent/kubernetes
pnpm audit --prod
node hack/pr050-license-scan.mjs
node hack/pr050-validate-release-hardening.mjs
node hack/pr054a-validate-timestamp-expand.mjs
govulncheck ./...
git diff --check
```

- [ ] Have one independent reviewer audit the code against ADR-0055 and this plan, and a second reviewer audit only migration/rollback/deployment failure paths. Resolve every finding with targeted tests.
- [ ] Re-run the entire acceptance gate after the final fix; record exact commit, commands, PostgreSQL versions, image digest, and any externally blocked gate without suppressing it.
- [ ] Update `docs/fork/PR-054A_EXTERNAL_EXECUTION_TIMESTAMP_EXPAND.md` with the final evidence table and commit only if evidence text changed.

```powershell
git status --short
git log --oneline 35318e6d40e66c15c1fd95091b16605a9a618d20..HEAD
```

Expected: clean worktree; focused PR-054A commits; every local gate green. Publication/merge/deployment waits if the unchanged Go VulnDB gate remains externally red.

## Expand Release Exit Criteria

- [ ] Migration 138 is the only new migration and every later planned allocation has shifted without gaps.
- [ ] A schema-137 non-empty database cannot cross 138 without a complete matching approved manifest captured while writers were fenced.
- [ ] Historical shadows are null immediately after migration and only evidence-resolved cells are populated by apply.
- [ ] Provenance is append-only; manifest content is immutable; apply is atomic and idempotent; standalone verification is read-only.
- [ ] All new execution writes pair legacy UTC-naive values and `TIMESTAMPTZ` instants from one authoritative instant across UTC, Asia/Bangkok, and a DST zone.
- [ ] Public API fields, null behavior, ordering, execution state, callback sequencing, hashes, and legacy reads are unchanged.
- [ ] Startup refuses pre-expand, contracted, dirty, incomplete, drifted, or unverified schema/data shapes.
- [ ] The Compose adapter proves writer fence, checksum backup, isolated restore, exact raw capture, explicit migration, verified apply, `serve --migrate=false`, and post-start checks in fixed order.
- [ ] PostgreSQL 16.14 and 18.4 focused gates pass, followed by the full release gate without exclusions.
- [ ] No client repository/runtime/database was touched and no demo/tutorial cleanup was combined with this release.
- [ ] The later contract release remains a separate reviewed plan with a dynamically allocated contiguous migration number.
