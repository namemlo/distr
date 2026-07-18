#!/usr/bin/env node
import {spawnSync} from 'node:child_process';
import {copyFileSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync} from 'node:fs';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

const root = fileURLToPath(new URL('..', import.meta.url));
const validator = 'hack/pr054a-validate-timestamp-expand.mjs';
const fixtureFiles = [
  validator,
  'docs/superpowers/plans/2026-07-14-control-plane-foundations.md',
  'docs/superpowers/plans/2026-07-14-control-plane-governance-execution.md',
  'docs/superpowers/plans/2026-07-14-control-plane-operator-adoption.md',
  'docs/fork/PR-054A_EXTERNAL_EXECUTION_TIMESTAMP_EXPAND.md',
  'docs/fork/FORK_DIFF_INDEX.md',
  'docs/fork/UPGRADE_GUIDE.md',
  'docs/operations/operator-smoke-test.md',
  'docs/operations/server-docker-compose-deploy.md',
  'docs/release/community-release-readiness.md',
  'docs/roadmaps/DISTR_COMMUNITY_FORK_MASTER_PLAN.md',
  'docs/security/release-hardening-checklist.md',
  'docs/adr/0055-external-execution-timestamp-instants.md',
  'docs/superpowers/specs/2026-07-15-external-execution-timestamptz-hybrid-design.md',
  'docs/superpowers/plans/2026-07-15-external-execution-timestamp-expand.md',
  'docs/superpowers/plans/2026-07-14-enterprise-operator-control-plane-program.md',
  '.github/workflows/build-hub.yaml',
  '.github/workflows/community-release-hardening.yaml',
  'deploy/server-docker-compose/deploy.sh',
  'hack/test-server-compose-timestamp-expand.sh',
  'cmd/hub/cmd/migrate.go',
  'cmd/hub/cmd/migrate_recover_dirty.go',
  'docs/upgrade/community-release-upgrade-checklist.md',
  'internal/migrations/sql/138_external_execution_timestamp_expand.up.sql',
  'internal/migrations/sql/138_external_execution_timestamp_expand.down.sql',
  'internal/migrations/migrate.go',
  'internal/migrations/preflight.go',
  'internal/migrations/timestamp_dirty_recovery_runner.go',
  'internal/db/external_execution_timestamps.go',
  'internal/db/organization.go',
  'internal/types/external_execution_timestamp.go',
];

const copyFixture = () => {
  const fixtureRoot = mkdtempSync(path.join(tmpdir(), 'pr054a-validator-'));
  for (const file of fixtureFiles) {
    const target = path.join(fixtureRoot, file);
    mkdirSync(path.dirname(target), {recursive: true});
    const contents = readFileSync(path.join(root, file), 'utf8').replace(/\r\n/g, '\n');
    writeFileSync(target, contents);
  }
  return fixtureRoot;
};

const updateFixture = (fixtureRoot, file, transform) => {
  const target = path.join(fixtureRoot, file);
  const original = readFileSync(target, 'utf8');
  const updated = transform(original);
  if (updated === original) throw new Error(`${file}: fixture mutation made no change`);
  writeFileSync(target, updated);
};

const replaceOnce = (text, before, after) => {
  const first = text.indexOf(before);
  if (first === -1) throw new Error(`fixture marker not found: ${before}`);
  if (text.indexOf(before, first + before.length) !== -1) {
    throw new Error(`fixture marker is not unique: ${before}`);
  }
  return `${text.slice(0, first)}${after}${text.slice(first + before.length)}`;
};
const fragment = (...parts) => parts.join('');
const atSign = String.fromCharCode(64);
const defaultComposeWorkflowStep =
  '      - name: Validate timestamp expand Compose orchestration\n' +
  '        run: bash hack/test-server-compose-timestamp-expand.sh';
const unpinnedDirtyRecoveryWorkflowStep =
  '      - name: Validate timestamp dirty recovery Compose orchestration\n' +
  '        run: DISTR_TIMESTAMP_TEST_GROUP=dirty-recovery bash hack/test-server-compose-timestamp-expand.sh';
const dirtyRecoveryWorkflowStep =
  '      - name: Validate timestamp dirty recovery Compose orchestration\n' +
  '        shell: bash\n' +
  '        run: DISTR_TIMESTAMP_TEST_GROUP=dirty-recovery bash hack/test-server-compose-timestamp-expand.sh';
const ensureDirtyRecoveryWorkflowStep = (text) =>
  text.includes(dirtyRecoveryWorkflowStep)
    ? text
    : text.includes(unpinnedDirtyRecoveryWorkflowStep)
      ? replaceOnce(text, unpinnedDirtyRecoveryWorkflowStep, dirtyRecoveryWorkflowStep)
      : replaceOnce(text, defaultComposeWorkflowStep, `${defaultComposeWorkflowStep}\n${dirtyRecoveryWorkflowStep}`);

const appendToSmokeSection = (fixtureRoot, text) => {
  const file = 'docs/operations/operator-smoke-test.md';
  const marker =
    'This smoke test does not authorize deleting execution history, provenance, audit records, or timestamp evidence.';
  updateFixture(fixtureRoot, file, (contents) => replaceOnce(contents, marker, `${marker}\n\n${text}`));
};

const prBlockBounds = (text, heading) => {
  const start = text.indexOf(heading);
  if (start === -1) throw new Error(`fixture PR heading not found: ${heading}`);
  const end = text.indexOf('\n## Task ', start + heading.length);
  if (end === -1) throw new Error(`fixture next PR heading not found after: ${heading}`);
  return {start, end};
};

const scenarios = [
  {
    name: 'missing-pr055',
    expected: 'missing expected PR allocation blocks: PR-55',
    mutate(fixtureRoot) {
      const file = fixtureFiles[1];
      updateFixture(fixtureRoot, file, (text) => {
        const {start, end} = prBlockBounds(text, '## Task 1: PR-055');
        return `${text.slice(0, start)}${text.slice(end + 1)}`;
      });
    },
  },
  {
    name: 'misnumbered-pr055',
    expected: 'missing expected PR allocation blocks: PR-55',
    mutate(fixtureRoot) {
      const file = fixtureFiles[1];
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '## Task 1: PR-055 — Establish the v2 Isolation Boundary',
          '## Task 1: PR-055A — Establish the v2 Isolation Boundary'
        )
      );
    },
  },
  {
    name: 'duplicate-pr055',
    expected: 'duplicate PR-55 allocation block',
    mutate(fixtureRoot) {
      const file = fixtureFiles[1];
      updateFixture(fixtureRoot, file, (text) => {
        const {start, end} = prBlockBounds(text, '## Task 1: PR-055');
        const block = text.slice(start, end);
        return `${text.slice(0, end)}\n${block}${text.slice(end)}`;
      });
    },
  },
  {
    name: 'pr055-migration-138',
    expected: 'PR-55 migration declarations mismatch: expected none, found 138',
    mutate(fixtureRoot) {
      const file = fixtureFiles[1];
      const marker = '- Modify: `internal/featureflags/featureflags.go`';
      const allocation = [
        '- Create: `internal/migrations/sql/138_stolen_timestamp_expand.up.sql`',
        '- Create: `internal/migrations/sql/138_stolen_timestamp_expand.down.sql`',
      ].join('\n');
      updateFixture(fixtureRoot, file, (text) => replaceOnce(text, marker, `${allocation}\n${marker}`));
    },
  },
  {
    name: 'pr055-migration-163',
    expected: 'PR-55 migration declarations mismatch: expected none, found 163',
    mutate(fixtureRoot) {
      const file = fixtureFiles[1];
      const marker = '- Modify: `internal/featureflags/featureflags.go`';
      const allocation = [
        '- Create: `internal/migrations/sql/163_conditional_contract.up.sql`',
        '- Create: `internal/migrations/sql/163_conditional_contract.down.sql`',
      ].join('\n');
      updateFixture(fixtureRoot, file, (text) => replaceOnce(text, marker, `${allocation}\n${marker}`));
    },
  },
  {
    name: 'pr055-second-files-paragraph',
    expected: 'PR-55 migration declarations mismatch: expected none, found 163',
    mutate(fixtureRoot) {
      const file = fixtureFiles[1];
      const marker = '- Create: `docs/fork/PR-055_OPERATOR_CONTROL_PLANE_FLAGS.md`';
      const allocation = [
        '- Create: `internal/migrations/sql/163_conditional_contract.up.sql`',
        '- Create: `internal/migrations/sql/163_conditional_contract.down.sql`',
      ].join('\n');
      updateFixture(fixtureRoot, file, (text) => replaceOnce(text, marker, `${marker}\n\n${allocation}`));
    },
  },
  {
    name: 'pr055-adr-0055',
    expected: 'PR-55 ADR declarations mismatch: expected none, found 0055',
    mutate(fixtureRoot) {
      const file = fixtureFiles[1];
      const marker = '- Modify: `internal/featureflags/featureflags.go`';
      const allocation = '- Create: `docs/adr/0055-stolen-timestamp-decision.md`';
      updateFixture(fixtureRoot, file, (text) => replaceOnce(text, marker, `${allocation}\n${marker}`));
    },
  },
  {
    name: 'pr055-adr-0069',
    expected: 'PR-55 ADR declarations mismatch: expected none, found 0069',
    mutate(fixtureRoot) {
      const file = fixtureFiles[1];
      const marker = '- Modify: `internal/featureflags/featureflags.go`';
      const allocation = '- Create: `docs/adr/0069-future-decision.md`';
      updateFixture(fixtureRoot, file, (text) => replaceOnce(text, marker, `${allocation}\n${marker}`));
    },
  },
  {
    name: 'missing-block',
    expected: 'missing expected PR allocation blocks: PR-57',
    mutate(fixtureRoot) {
      const file = fixtureFiles[1];
      updateFixture(fixtureRoot, file, (text) => {
        const {start, end} = prBlockBounds(text, '## Task 3: PR-057');
        return `${text.slice(0, start)}${text.slice(end + 1)}`;
      });
    },
  },
  {
    name: 'duplicate-block',
    expected: 'duplicate PR-57 allocation block',
    mutate(fixtureRoot) {
      const file = fixtureFiles[1];
      updateFixture(fixtureRoot, file, (text) => {
        const {start, end} = prBlockBounds(text, '## Task 3: PR-057');
        const block = text.slice(start, end);
        return `${text.slice(0, end)}\n${block}${text.slice(end)}`;
      });
    },
  },
  {
    name: 'extra-stale-migration',
    expected: 'PR-57 migration declarations mismatch: expected paired 140 up/down',
    mutate(fixtureRoot) {
      const file = fixtureFiles[1];
      const expected = '- Create: `internal/migrations/sql/140_deployment_registry_imports.up.sql`';
      const stale = '- Create: `internal/migrations/sql/139_stale_registry_imports.up.sql`';
      updateFixture(fixtureRoot, file, (text) => replaceOnce(text, expected, `${expected}\n${stale}`));
    },
  },
  {
    name: 'missing-up-declaration',
    expected: 'PR-57 migration declarations mismatch: expected paired 140 up/down',
    mutate(fixtureRoot) {
      const file = fixtureFiles[1];
      const declaration = '- Create: `internal/migrations/sql/140_deployment_registry_imports.up.sql`';
      updateFixture(fixtureRoot, file, (text) => replaceOnce(text, `${declaration}\n`, ''));
    },
  },
  {
    name: 'missing-down-declaration',
    expected: 'PR-57 migration declarations mismatch: expected paired 140 up/down',
    mutate(fixtureRoot) {
      const file = fixtureFiles[1];
      const declaration = '- Create: `internal/migrations/sql/140_deployment_registry_imports.down.sql`';
      updateFixture(fixtureRoot, file, (text) => replaceOnce(text, `${declaration}\n`, ''));
    },
  },
  {
    name: 'modified-migration-pair',
    expected: 'PR-57 migration declarations mismatch: expected paired 140 up/down',
    mutate(fixtureRoot) {
      const file = fixtureFiles[1];
      updateFixture(fixtureRoot, file, (text) => {
        const up = '- Create: `internal/migrations/sql/140_deployment_registry_imports.up.sql`';
        const down = '- Create: `internal/migrations/sql/140_deployment_registry_imports.down.sql`';
        return replaceOnce(
          replaceOnce(text, up, up.replace('- Create:', '- Modify:')),
          down,
          down.replace('- Create:', '- Modify:')
        );
      });
    },
  },
  {
    name: 'zero-padded-migration-pair',
    expected: 'PR-57 migration declarations mismatch: expected paired 140 up/down',
    mutate(fixtureRoot) {
      const file = fixtureFiles[1];
      updateFixture(fixtureRoot, file, (text) => {
        const up = '- Create: `internal/migrations/sql/140_deployment_registry_imports.up.sql`';
        const down = '- Create: `internal/migrations/sql/140_deployment_registry_imports.down.sql`';
        return replaceOnce(replaceOnce(text, up, up.replace('/140_', '/0140_')), down, down.replace('/140_', '/0140_'));
      });
    },
  },
  {
    name: 'indented-extra-migration-pair',
    expected: 'PR-57 malformed allocation declaration',
    mutate(fixtureRoot) {
      const file = fixtureFiles[1];
      const marker = '- Create: `internal/migrations/sql/140_deployment_registry_imports.down.sql`';
      const allocation = [
        ' - Create: `internal/migrations/sql/163_hidden_contract.up.sql`',
        ' - Create: `internal/migrations/sql/163_hidden_contract.down.sql`',
      ].join('\n');
      updateFixture(fixtureRoot, file, (text) => replaceOnce(text, marker, `${marker}\n\n${allocation}`));
    },
  },
  {
    name: 'unexpected-adr',
    expected: 'PR-57 ADR declarations mismatch: expected none, found 0056',
    mutate(fixtureRoot) {
      const file = fixtureFiles[1];
      const marker = '- Create: `docs/fork/PR-057_DEPLOYMENT_REGISTRY_IMPORT.md`';
      const stale = '- Create: `docs/adr/0056-stale-registry-import.md`';
      updateFixture(fixtureRoot, file, (text) => replaceOnce(text, marker, `${marker}\n${stale}`));
    },
  },
  {
    name: 'modified-adr-declaration',
    expected: 'PR-56 ADR declarations mismatch: expected 0056:Create, found 0056:Modify',
    mutate(fixtureRoot) {
      const file = fixtureFiles[1];
      const declaration = '- Create: `docs/adr/0056-canonical-deployment-registry-identity.md`';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, declaration, declaration.replace('- Create:', '- Modify:'))
      );
    },
  },
  {
    name: 'missing-exact-decision',
    expected: 'expected exact decision line: - Logical contract migration:',
    mutate(fixtureRoot) {
      const file = 'docs/fork/PR-054A_EXTERNAL_EXECUTION_TIMESTAMP_EXPAND.md';
      const decision =
        '- Logical contract migration: the next unused contiguous number only when the contract release is shippable; 163 is conditional on every currently planned migration landing first.';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, decision, '- Logical contract migration: allocated later.')
      );
    },
  },
  {
    name: 'missing-retention-tombstone-delete-guard',
    expected:
      'internal/migrations/sql/138_external_execution_timestamp_expand.up.sql: missing CREATE TRIGGER ExternalExecution_timestamp_deletion_tombstone',
    mutate(fixtureRoot) {
      const file = 'internal/migrations/sql/138_external_execution_timestamp_expand.up.sql';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          'CREATE TRIGGER ExternalExecution_timestamp_deletion_tombstone',
          'CREATE TRIGGER Removed_externalexecution_timestamp_deletion_tombstone'
        )
      );
    },
  },
  {
    name: 'missing-retention-operation-identity',
    expected: "internal/db/organization.go: missing 'distr.external_execution_timestamp_deletion_operation_id'",
    mutate(fixtureRoot) {
      const file = 'internal/db/organization.go';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          "'distr.external_execution_timestamp_deletion_operation_id'",
          "'distr.removed_timestamp_deletion_operation_id'"
        )
      );
    },
  },
  {
    name: 'missing-retention-nonzero-operation-constraint',
    expected:
      'internal/migrations/sql/138_external_execution_timestamp_expand.up.sql: missing externalexecutiontimestampdeletiontombstone_operation_nonzero',
    mutate(fixtureRoot) {
      const file = 'internal/migrations/sql/138_external_execution_timestamp_expand.up.sql';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          'externalexecutiontimestampdeletiontombstone_operation_nonzero',
          'externalexecutiontimestampdeletiontombstone_removed_operation_nonzero'
        )
      );
    },
  },
  {
    name: 'missing-retention-statement-time',
    expected:
      'internal/migrations/sql/138_external_execution_timestamp_expand.up.sql: missing deletion_timestamp := clock_timestamp()',
    mutate(fixtureRoot) {
      const file = 'internal/migrations/sql/138_external_execution_timestamp_expand.up.sql';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, 'deletion_timestamp := clock_timestamp()', 'deletion_timestamp := CURRENT_TIMESTAMP')
      );
    },
  },
  {
    name: 'missing-retention-downgrade-preflight',
    expected: 'internal/migrations/preflight.go: missing downgrade crossing 138 is forbidden after timestamp retention',
    mutate(fixtureRoot) {
      const file = 'internal/migrations/preflight.go';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          'downgrade crossing 138 is forbidden after timestamp retention',
          'removed downgrade retention protection'
        )
      );
    },
  },
  {
    name: 'missing-retention-downgrade-transaction-lock',
    expected:
      'internal/migrations/sql/138_external_execution_timestamp_expand.down.sql: missing IN ACCESS EXCLUSIVE MODE',
    mutate(fixtureRoot) {
      const file = 'internal/migrations/sql/138_external_execution_timestamp_expand.down.sql';
      updateFixture(fixtureRoot, file, (text) => replaceOnce(text, 'IN ACCESS EXCLUSIVE MODE', 'IN SHARE MODE'));
    },
  },
  {
    name: 'missing-retention-downgrade-status-repair',
    expected: 'internal/migrations/migrate.go: missing restoreTimestampDowngradeGuardStatus',
    mutate(fixtureRoot) {
      const file = 'internal/migrations/migrate.go';
      updateFixture(fixtureRoot, file, (text) =>
        text.replaceAll('restoreTimestampDowngradeGuardStatus', 'removedTimestampDowngradeGuardStatus')
      );
    },
  },
  {
    name: 'missing-retention-downgrade-bounded-repair',
    expected: 'internal/migrations/migrate.go: missing repairContext, repairCancel := context.WithTimeout',
    mutate(fixtureRoot) {
      const file = 'internal/migrations/migrate.go';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          'repairContext, repairCancel := context.WithTimeout',
          'repairContext, repairCancel := context.WithCancel'
        )
      );
    },
  },
  {
    name: 'missing-retained-promotion-ordering',
    expected:
      'internal/db/external_execution_timestamps.go: missing deletion follows or coincides with provenance promotion',
    mutate(fixtureRoot) {
      const file = 'internal/db/external_execution_timestamps.go';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          'deletion follows or coincides with provenance promotion',
          'removed retained promotion ordering'
        )
      );
    },
  },
  {
    name: 'missing-readiness-live-shadow-reporting',
    expected: 'internal/types/external_execution_timestamp.go: expected 2 occurrences of json:"resolvedShadowCount"',
    mutate(fixtureRoot) {
      const file = 'internal/types/external_execution_timestamp.go';
      updateFixture(fixtureRoot, file, (text) => {
        const marker = 'json:"resolvedShadowCount"';
        const index = text.lastIndexOf(marker);
        if (index === -1) throw new Error(`fixture marker not found: ${marker}`);
        return `${text.slice(0, index)}json:"removedResolvedShadowCount"${text.slice(index + marker.length)}`;
      });
    },
  },
  {
    name: 'missing-resolved-deleted-evidence-reporting',
    expected: 'internal/types/external_execution_timestamp.go: missing json:"resolvedDeletedEvidenceCount"',
    mutate(fixtureRoot) {
      const file = 'internal/types/external_execution_timestamp.go';
      updateFixture(fixtureRoot, file, (text) =>
        text.replaceAll('json:"resolvedDeletedEvidenceCount"', 'json:"removedResolvedDeletedEvidenceCount"')
      );
    },
  },
  {
    name: 'misleading-retention-security-boundary',
    expected: 'docs/security/release-hardening-checklist.md: missing not a PostgreSQL privilege boundary',
    mutate(fixtureRoot) {
      const file = 'docs/security/release-hardening-checklist.md';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, 'not a PostgreSQL privilege boundary', 'a PostgreSQL privilege boundary')
      );
    },
  },
  {
    name: 'loose-fork-index',
    expected: 'fork index missing structured PR-054A entry',
    mutate(fixtureRoot) {
      const file = 'docs/fork/FORK_DIFF_INDEX.md';
      updateFixture(fixtureRoot, file, (text) => {
        const heading = '### PR-054A - External-execution timestamp expand';
        const start = text.indexOf(heading);
        if (start === -1) throw new Error(`fixture marker not found: ${heading}`);
        return `${text.slice(0, start).trimEnd()}\n\nPR-054A\n`;
      });
    },
  },
  {
    name: 'loose-master-ledger',
    expected: 'master roadmap missing structured extension ledger',
    mutate(fixtureRoot) {
      const file = 'docs/roadmaps/DISTR_COMMUNITY_FORK_MASTER_PLAN.md';
      updateFixture(fixtureRoot, file, (text) => {
        const heading = '### Accepted post-PR-050 extension ledger';
        const start = text.indexOf(heading);
        const end = text.indexOf('\n---', start);
        if (start === -1 || end === -1) throw new Error('fixture extension ledger not found');
        return `${text.slice(0, start)}PR-054A\n${text.slice(end)}`;
      });
    },
  },
  {
    name: 'contradictory-ledger-ownership',
    expected: 'master roadmap allocation ledger mismatch',
    mutate(fixtureRoot) {
      const file = 'docs/roadmaps/DISTR_COMMUNITY_FORK_MASTER_PLAN.md';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '| PR-055                | None                 | None              |',
          '| PR-055                | 138                  | 0055              |'
        )
      );
    },
  },
  {
    name: 'duplicate-ledger-ownership',
    expected: 'master roadmap allocation ledger mismatch',
    mutate(fixtureRoot) {
      const file = 'docs/roadmaps/DISTR_COMMUNITY_FORK_MASTER_PLAN.md';
      const row = '| PR-054A               | 138                  | 0055              |';
      updateFixture(fixtureRoot, file, (text) => replaceOnce(text, row, `${row}\n${row}`));
    },
  },
  {
    name: 'indented-ledger-ownership',
    expected: 'master roadmap allocation ledger mismatch',
    mutate(fixtureRoot) {
      const file = 'docs/roadmaps/DISTR_COMMUNITY_FORK_MASTER_PLAN.md';
      const row = '| PR-055                | None                 | None              |';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, row, `${row}\n | PR-055                | 138                  | 0055              |`)
      );
    },
  },
  {
    name: 'broken-evidence-link',
    expected: 'PR-054A evidence index mismatch',
    mutate(fixtureRoot) {
      const file = 'docs/fork/PR-054A_EXTERNAL_EXECUTION_TIMESTAMP_EXPAND.md';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, '../adr/0055-external-execution-timestamp-instants.md', 'BROKEN.md')
      );
    },
  },
  {
    name: 'external-extra-evidence-link',
    expected: 'PR-054A evidence index mismatch',
    mutate(fixtureRoot) {
      const file = 'docs/fork/PR-054A_EXTERNAL_EXECUTION_TIMESTAMP_EXPAND.md';
      const row =
        '| Deterministic allocation check | [`pr054a-validate-timestamp-expand.mjs`](../../hack/pr054a-validate-timestamp-expand.mjs)                                         |';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, row, `${row}\n\nAdditional evidence: [external](https://example.com).`)
      );
    },
  },
  {
    name: 'deleted-adr-evidence-target',
    expected: 'PR-054A evidence link target missing: ../adr/0055-external-execution-timestamp-instants.md',
    mutate(fixtureRoot) {
      rmSync(path.join(fixtureRoot, 'docs/adr/0055-external-execution-timestamp-instants.md'));
    },
  },
  {
    name: 'deleted-plan-evidence-target',
    expected:
      'PR-054A evidence link target missing: ../superpowers/plans/2026-07-15-external-execution-timestamp-expand.md',
    mutate(fixtureRoot) {
      rmSync(path.join(fixtureRoot, 'docs/superpowers/plans/2026-07-15-external-execution-timestamp-expand.md'));
    },
  },
  {
    name: 'security-regression-suite-is-ci-enforced',
    expected: 'fast-release-package security regression gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '      - name: Run PR-054A security regression suite\n        run: node hack/pr054a-validate-timestamp-expand.test.mjs\n',
          ''
        )
      );
    },
  },
  {
    name: 'security-regression-step-rejects-anchored-if-key',
    expected: 'fast-release-package execution control mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '      - name: Run PR-054A security regression suite\n        run: node hack/pr054a-validate-timestamp-expand.test.mjs',
          '      - name: Run PR-054A security regression suite\n        &skip_regression if: ${{ false }}\n        run: node hack/pr054a-validate-timestamp-expand.test.mjs'
        )
      );
    },
  },
  {
    name: 'security-regression-step-rejects-tagged-if-key',
    expected: 'fast-release-package execution control mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '      - name: Run PR-054A security regression suite\n        run: node hack/pr054a-validate-timestamp-expand.test.mjs',
          '      - name: Run PR-054A security regression suite\n        !!str if: ${{ false }}\n        run: node hack/pr054a-validate-timestamp-expand.test.mjs'
        )
      );
    },
  },
  {
    name: 'security-regression-step-rejects-bash-env-metadata',
    expected: 'fast-release-package security regression gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '      - name: Run PR-054A security regression suite\n        run: node hack/pr054a-validate-timestamp-expand.test.mjs',
          '      - name: Run PR-054A security regression suite\n        run: node hack/pr054a-validate-timestamp-expand.test.mjs\n        env:\n          BASH_ENV: hack/regression-bootstrap.sh'
        )
      );
    },
  },
  {
    name: 'timestamp-validator-command-cannot-hide-behind-early-success',
    expected: 'fast-release-package timestamp validator gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '        run: node hack/pr054a-validate-timestamp-expand.mjs',
          '        run: exit 0 # node hack/pr054a-validate-timestamp-expand.mjs'
        )
      );
    },
  },
  {
    name: 'timestamp-validator-step-rejects-block-scalar-decoy',
    expected: 'fast-release-package timestamp validator gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) => {
        const withoutRealStep = replaceOnce(
          text,
          '      - name: Validate PR-054A timestamp expand package\n        run: node hack/pr054a-validate-timestamp-expand.mjs\n',
          ''
        );
        return replaceOnce(
          withoutRealStep,
          '      - name: Run deterministic advanced-flow verifier\n        run: node examples/community-e2e/run-demo.mjs',
          '      - name: Run deterministic advanced-flow verifier\n        run: |\n          exit 0\n          - name: Validate PR-054A timestamp expand package\n            run: node hack/pr054a-validate-timestamp-expand.mjs'
        );
      });
    },
  },
  {
    name: 'compose-orchestration-command-cannot-hide-behind-early-success',
    expected: 'fast-release-package Compose orchestration gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '        run: bash hack/test-server-compose-timestamp-expand.sh',
          '        run: exit 0 # bash hack/test-server-compose-timestamp-expand.sh'
        )
      );
    },
  },
  {
    name: 'dirty-recovery-focused-step-is-required',
    expected: 'fast-release-package dirty recovery Compose orchestration gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) => {
        if (!text.includes(dirtyRecoveryWorkflowStep)) {
          return `${text}\n# required focused dirty recovery step intentionally absent\n`;
        }
        return replaceOnce(text, `${dirtyRecoveryWorkflowStep}\n`, '');
      });
    },
  },
  {
    name: 'dirty-recovery-focused-command-is-exact',
    expected: 'fast-release-package dirty recovery Compose orchestration gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          ensureDirtyRecoveryWorkflowStep(text),
          '        run: DISTR_TIMESTAMP_TEST_GROUP=dirty-recovery bash hack/test-server-compose-timestamp-expand.sh',
          '        run: DISTR_TIMESTAMP_TEST_GROUP=dirty-recoveries bash hack/test-server-compose-timestamp-expand.sh'
        )
      );
    },
  },
  {
    name: 'dirty-recovery-focused-step-cannot-be-conditional',
    expected: 'fast-release-package execution control mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          ensureDirtyRecoveryWorkflowStep(text),
          dirtyRecoveryWorkflowStep,
          '      - name: Validate timestamp dirty recovery Compose orchestration\n' +
            '        if: ${{ false }}\n' +
            '        run: DISTR_TIMESTAMP_TEST_GROUP=dirty-recovery bash hack/test-server-compose-timestamp-expand.sh'
        )
      );
    },
  },
  {
    name: 'dirty-recovery-focused-command-cannot-hide-behind-early-success',
    expected: 'fast-release-package dirty recovery Compose orchestration gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          ensureDirtyRecoveryWorkflowStep(text),
          '        run: DISTR_TIMESTAMP_TEST_GROUP=dirty-recovery bash hack/test-server-compose-timestamp-expand.sh',
          '        run: exit 0 # DISTR_TIMESTAMP_TEST_GROUP=dirty-recovery bash hack/test-server-compose-timestamp-expand.sh'
        )
      );
    },
  },
  {
    name: 'dirty-recovery-cli-registration-is-required',
    expected: 'dirty recovery CLI contract mismatch',
    mutate(fixtureRoot) {
      const file = 'cmd/hub/cmd/migrate.go';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, '\tcommand.AddCommand(newMigrateRecoverDirtyCommand(migrateRecoverDirtyRuntime{}))\n', '')
      );
    },
  },
  {
    name: 'dirty-recovery-cli-name-is-exact',
    expected: 'dirty recovery CLI contract mismatch',
    mutate(fixtureRoot) {
      const file = 'cmd/hub/cmd/migrate_recover_dirty.go';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, '\t\tUse:           "recover-dirty",', '\t\tUse:           "recover_dirty",')
      );
    },
  },
  {
    name: 'review-dirty-recovery-cli-registration-cannot-be-commented-out',
    expected: 'dirty recovery CLI contract mismatch',
    mutate(fixtureRoot) {
      const file = 'cmd/hub/cmd/migrate.go';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '\tcommand.AddCommand(newMigrateRecoverDirtyCommand(migrateRecoverDirtyRuntime{}))',
          '\t// command.AddCommand(newMigrateRecoverDirtyCommand(migrateRecoverDirtyRuntime{}))'
        )
      );
    },
  },
  {
    name: 'dirty-recovery-plan-command-is-exact',
    expected: 'dirty recovery operator command contract mismatch',
    mutate(fixtureRoot) {
      const file = 'deploy/server-docker-compose/deploy.sh';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, '          migrate recover-dirty plan \\\n', '          migrate recover-dirt plan \\\n')
      );
    },
  },
  {
    name: 'dirty-recovery-dispatch-is-required',
    expected: 'dirty recovery dispatch contract mismatch',
    mutate(fixtureRoot) {
      const file = 'deploy/server-docker-compose/deploy.sh';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, '      timestamp_expand_recover_dirty "$@" || return', '      return 0')
      );
    },
  },
  {
    name: 'dirty-recovery-retained-artifact-state-is-required',
    expected: 'dirty recovery retained artifact contract mismatch',
    mutate(fixtureRoot) {
      const file = 'deploy/server-docker-compose/deploy.sh';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, '    1:0) printf FINAL_ONLY ;;', '    1:0) printf UNSAFE_PARTIAL ;;')
      );
    },
  },
  {
    name: 'dirty-recovery-focused-group-keeps-retained-artifact-proof',
    expected: 'dirty recovery focused harness mismatch',
    mutate(fixtureRoot) {
      const file = 'hack/test-server-compose-timestamp-expand.sh';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '  test_dirty_recovery_repairs_valid_finals_missing_sidecars\n',
          '  : # retained artifact retry proof removed\n'
        )
      );
    },
  },
  {
    name: 'dirty-recovery-focused-group-keeps-non-finalization-proof',
    expected: 'dirty recovery focused harness mismatch',
    mutate(fixtureRoot) {
      const file = 'hack/test-server-compose-timestamp-expand.sh';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '  test_dirty_recovery_function_never_starts_or_clears_fence\n',
          '  : # non-finalization proof removed\n'
        )
      );
    },
  },
  {
    name: 'review-dirty-recovery-focused-group-rejects-early-success',
    expected: 'dirty recovery focused harness mismatch',
    mutate(fixtureRoot) {
      const file = 'hack/test-server-compose-timestamp-expand.sh';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          'if [[ "${DISTR_TIMESTAMP_TEST_GROUP:-}" == dirty-recovery ]]; then\n',
          'if [[ "${DISTR_TIMESTAMP_TEST_GROUP:-}" == dirty-recovery ]]; then\n  exit 0\n'
        )
      );
    },
  },
  {
    name: 'review-dirty-recovery-focused-group-rejects-commented-call',
    expected: 'dirty recovery focused harness mismatch',
    mutate(fixtureRoot) {
      const file = 'hack/test-server-compose-timestamp-expand.sh';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '  test_dirty_recovery_function_never_starts_or_clears_fence\n',
          '  # test_dirty_recovery_function_never_starts_or_clears_fence\n'
        )
      );
    },
  },
  {
    name: 'dirty-recovery-function-cannot-finalize-deployment',
    expected: 'dirty recovery non-finalization contract mismatch',
    mutate(fixtureRoot) {
      const file = 'deploy/server-docker-compose/deploy.sh';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '  assert_hub_writers_stopped || return\n}\n\ntimestamp_expand_apply() {',
          '  assert_hub_writers_stopped || return\n  start_hub || return\n}\n\ntimestamp_expand_apply() {'
        )
      );
    },
  },
  {
    name: 'migration-force-cannot-escape-audited-recovery-runner',
    expected: 'migration Force confinement mismatch',
    mutate(fixtureRoot) {
      const file = 'internal/migrations/migrate.go';
      updateFixture(
        fixtureRoot,
        file,
        (text) =>
          `${text}\nfunc unsafeForce(instance interface{ Force(int) error }) {\n\tforce := instance.Force\n\t_ = force(138)\n}\n`
      );
    },
  },
  {
    name: 'review-dirty-recovery-step-rejects-inherited-noop-shell',
    expected: 'fast-release-package dirty recovery Compose orchestration gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) => {
        const pinned = ensureDirtyRecoveryWorkflowStep(text);
        const unpinned = replaceOnce(pinned, dirtyRecoveryWorkflowStep, unpinnedDirtyRecoveryWorkflowStep);
        return replaceOnce(unpinned, 'jobs:\n', 'defaults:\n  run:\n    shell: true {0}\n\njobs:\n');
      });
    },
  },
  {
    name: 'compose-render-quiet-command-is-validated',
    expected: 'fast-release-package Compose render gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      const required =
        'docker compose --env-file "$DISTR_COMPOSE_ENV_FILE" -f deploy/server-docker-compose/docker-compose.yml --profile timestamp-operator config --quiet';
      updateFixture(fixtureRoot, file, (text) => replaceOnce(text, required, required.replace(' --quiet', '')));
    },
  },
  {
    name: 'compose-render-absolute-env-is-validated',
    expected: 'fast-release-package Compose render gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          'export DISTR_COMPOSE_ENV_FILE="$PWD/deploy/server-docker-compose/.env.example"',
          'export DISTR_COMPOSE_ENV_FILE="deploy/server-docker-compose/.env.example"'
        )
      );
    },
  },
  {
    name: 'compose-render-evidence-mode-is-validated',
    expected: 'fast-release-package Compose render gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, 'chmod 0700 "$evidence_dir"', 'chmod 0755 "$evidence_dir"')
      );
    },
  },
  {
    name: 'compose-render-command-cannot-be-bypassed-by-early-success',
    expected: 'fast-release-package Compose render gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      const required =
        '          docker compose --env-file "$DISTR_COMPOSE_ENV_FILE" -f deploy/server-docker-compose/docker-compose.yml --profile timestamp-operator config --quiet';
      updateFixture(fixtureRoot, file, (text) => replaceOnce(text, required, `          exit 0\n${required}`));
    },
  },
  {
    name: 'postgres-runtime-version-is-validated',
    expected: 'timestamp-expand-postgresql runtime evidence gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) => replaceOnce(text, "'SHOW server_version'", "'SELECT version()'"));
    },
  },
  {
    name: 'postgres-migration-sequence-cannot-hide-behind-early-success',
    expected: 'timestamp-expand-postgresql migration sequence gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '        run: bash hack/validate-migrations.sh',
          '        run: exit 0 # bash hack/validate-migrations.sh'
        )
      );
    },
  },
  {
    name: 'postgres-migration-sequence-command-is-required',
    expected: 'timestamp-expand-postgresql migration sequence gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '      - name: Validate migration sequence\n        run: bash hack/validate-migrations.sh\n',
          ''
        )
      );
    },
  },
  {
    name: 'postgres-targeted-go-suite-cannot-hide-behind-early-success',
    expected: 'timestamp-expand-postgresql targeted Go gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      const required =
        '        run: go test -p=1 ./internal/externalexecutiontimestamp ./internal/migrations ./internal/db ./internal/hubexecutor ./internal/mapping ./cmd/hub/cmd -count=1 -timeout 30m';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, required, `        run: exit 0 #${required.trimStart().slice(4)}`)
      );
    },
  },
  {
    name: 'postgres-runtime-evidence-cannot-be-forged-before-early-success',
    expected: 'timestamp-expand-postgresql runtime evidence gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      const required =
        '          runtime_version="$(docker exec "$POSTGRES_CONTAINER_ID" psql -U local -d distr -Atqc \'SHOW server_version\')"';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, required, `          printf 'forged\\n' >"$evidence_file"\n          exit 0\n${required}`)
      );
    },
  },
  {
    name: 'postgres-service-container-identity-is-validated',
    expected: 'timestamp-expand-postgresql runtime evidence gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, 'POSTGRES_CONTAINER_ID: ${{ job.services.postgres.id }}', 'POSTGRES_CONTAINER_ID: postgres')
      );
    },
  },
  {
    name: 'postgres-repodigest-is-validated',
    expected: 'timestamp-expand-postgresql runtime evidence gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, '[[ "$repo_digest" =~ ^postgres@sha256:', '[[ "$repo_digest" =~ ^sha256:')
      );
    },
  },
  {
    name: 'postgres-evidence-file-mode-is-validated',
    expected: 'timestamp-expand-postgresql runtime evidence gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, 'chmod 0600 "$evidence_file"', 'chmod 0644 "$evidence_file"')
      );
    },
  },
  {
    name: 'postgres-evidence-directory-absolute-guard-is-validated',
    expected: 'timestamp-expand-postgresql runtime evidence gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, '[[ "$evidence_dir" = /* ]]', '[[ -n "$evidence_dir" ]]')
      );
    },
  },
  {
    name: 'postgres-configured-image-equality-is-validated',
    expected: 'timestamp-expand-postgresql runtime evidence gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, '[[ "$configured_image" == "$EXPECTED_POSTGRES_IMAGE" ]]', '[[ -n "$configured_image" ]]')
      );
    },
  },
  {
    name: 'postgres-evidence-artifact-pin-is-validated',
    expected: 'timestamp-expand-postgresql runtime evidence gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          'actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a',
          'actions/upload-artifact@main'
        )
      );
    },
  },
  {
    name: 'postgres-evidence-artifact-always-is-validated',
    expected: 'timestamp-expand-postgresql execution control mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '      - name: Retain PostgreSQL runtime evidence\n        if: ${{ always() }}',
          '      - name: Retain PostgreSQL runtime evidence\n        if: ${{ success() }}'
        )
      );
    },
  },
  {
    name: 'postgres-evidence-artifact-name-is-validated',
    expected: 'timestamp-expand-postgresql runtime evidence gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          'name: timestamp-expand-postgresql-${{ matrix.postgres_version }}',
          'name: timestamp-expand-postgresql'
        )
      );
    },
  },
  {
    name: 'postgres-evidence-artifact-path-is-validated',
    expected: 'timestamp-expand-postgresql runtime evidence gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          'path: ${{ runner.temp }}/timestamp-expand-postgresql-evidence',
          'path: timestamp-expand-postgresql-evidence'
        )
      );
    },
  },
  {
    name: 'postgres-evidence-artifact-missing-file-policy-is-validated',
    expected: 'timestamp-expand-postgresql runtime evidence gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, 'if-no-files-found: error', 'if-no-files-found: warn')
      );
    },
  },
  {
    name: 'fast-release-job-cannot-continue-on-error',
    expected: 'fast-release-package execution control mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '  fast-release-package:\n    name: Fast release package checks\n    runs-on:',
          '  fast-release-package:\n    name: Fast release package checks\n    continue-on-error: true\n    runs-on:'
        )
      );
    },
  },
  {
    name: 'fast-release-job-cannot-be-conditionally-skipped',
    expected: 'fast-release-package execution control mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '  fast-release-package:\n    name: Fast release package checks\n    runs-on:',
          '  fast-release-package:\n    name: Fast release package checks\n    if: ${{ false }}\n    runs-on:'
        )
      );
    },
  },
  {
    name: 'timestamp-matrix-job-cannot-continue-on-error',
    expected: 'timestamp-expand-postgresql execution control mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '  timestamp-expand-postgresql:\n    name: Timestamp expand / PostgreSQL ${{ matrix.postgres_version }}\n    runs-on:',
          '  timestamp-expand-postgresql:\n    name: Timestamp expand / PostgreSQL ${{ matrix.postgres_version }}\n    continue-on-error: true\n    runs-on:'
        )
      );
    },
  },
  {
    name: 'timestamp-matrix-job-cannot-be-conditionally-skipped',
    expected: 'timestamp-expand-postgresql execution control mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '  timestamp-expand-postgresql:\n    name: Timestamp expand / PostgreSQL ${{ matrix.postgres_version }}\n    runs-on:',
          '  timestamp-expand-postgresql:\n    name: Timestamp expand / PostgreSQL ${{ matrix.postgres_version }}\n    if: ${{ false }}\n    runs-on:'
        )
      );
    },
  },
  {
    name: 'fast-release-job-rejects-quoted-if-key',
    expected: 'fast-release-package execution control mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '  fast-release-package:\n    name: Fast release package checks\n    runs-on:',
          "  fast-release-package:\n    name: Fast release package checks\n    'if': ${{ false }}\n    runs-on:"
        )
      );
    },
  },
  {
    name: 'fast-release-job-rejects-quoted-continue-key',
    expected: 'fast-release-package execution control mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '  fast-release-package:\n    name: Fast release package checks\n    runs-on:',
          '  fast-release-package:\n    name: Fast release package checks\n    "continue-on-error": true\n    runs-on:'
        )
      );
    },
  },
  {
    name: 'fast-release-job-rejects-escaped-if-key',
    expected: 'fast-release-package execution control mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '  fast-release-package:\n    name: Fast release package checks\n    runs-on:',
          '  fast-release-package:\n    name: Fast release package checks\n    "i\\u0066": ${{ false }}\n    runs-on:'
        )
      );
    },
  },
  {
    name: 'fast-release-job-rejects-explicit-if-key',
    expected: 'fast-release-package execution control mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '  fast-release-package:\n    name: Fast release package checks\n    runs-on:',
          '  fast-release-package:\n    name: Fast release package checks\n    ? if\n    : ${{ false }}\n    runs-on:'
        )
      );
    },
  },
  {
    name: 'fast-release-job-rejects-spaced-if-key',
    expected: 'fast-release-package execution control mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '  fast-release-package:\n    name: Fast release package checks\n    runs-on:',
          '  fast-release-package:\n    name: Fast release package checks\n    if : ${{ false }}\n    runs-on:'
        )
      );
    },
  },
  {
    name: 'fast-release-job-rejects-escaped-continue-key',
    expected: 'fast-release-package execution control mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '  fast-release-package:\n    name: Fast release package checks\n    runs-on:',
          '  fast-release-package:\n    name: Fast release package checks\n    "continue-\\u006fn-error": true\n    runs-on:'
        )
      );
    },
  },
  {
    name: 'timestamp-matrix-job-rejects-quoted-if-key',
    expected: 'timestamp-expand-postgresql execution control mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '  timestamp-expand-postgresql:\n    name: Timestamp expand / PostgreSQL ${{ matrix.postgres_version }}\n    runs-on:',
          '  timestamp-expand-postgresql:\n    name: Timestamp expand / PostgreSQL ${{ matrix.postgres_version }}\n    "if": ${{ false }}\n    runs-on:'
        )
      );
    },
  },
  {
    name: 'timestamp-matrix-job-rejects-quoted-continue-key',
    expected: 'timestamp-expand-postgresql execution control mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '  timestamp-expand-postgresql:\n    name: Timestamp expand / PostgreSQL ${{ matrix.postgres_version }}\n    runs-on:',
          "  timestamp-expand-postgresql:\n    name: Timestamp expand / PostgreSQL ${{ matrix.postgres_version }}\n    'continue-on-error': true\n    runs-on:"
        )
      );
    },
  },
  {
    name: 'release-gates-job-rejects-quoted-continue-key',
    expected: 'release-gates execution control mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '  release-gates:\n    name: Full release gates and live demo\n    needs: [fast-release-package, timestamp-expand-postgresql]\n    runs-on: ubuntu-latest',
          "  release-gates:\n    name: Full release gates and live demo\n    needs: [fast-release-package, timestamp-expand-postgresql]\n    'continue-on-error': true\n    runs-on: ubuntu-latest"
        )
      );
    },
  },
  {
    name: 'postgres-evidence-step-rejects-quoted-if-key',
    expected: 'timestamp-expand-postgresql execution control mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '      - name: Capture PostgreSQL runtime and image evidence\n        shell: bash',
          "      - name: Capture PostgreSQL runtime and image evidence\n        'if': ${{ false }}\n        shell: bash"
        )
      );
    },
  },
  {
    name: 'postgres-evidence-step-rejects-quoted-continue-key',
    expected: 'timestamp-expand-postgresql execution control mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '      - name: Capture PostgreSQL runtime and image evidence\n        shell: bash',
          '      - name: Capture PostgreSQL runtime and image evidence\n        "continue-on-error": true\n        shell: bash'
        )
      );
    },
  },
  {
    name: 'whitespace-step-rejects-quoted-if-key',
    expected: 'release-gates execution control mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '      - name: Check committed patch whitespace\n        shell: bash',
          '      - name: Check committed patch whitespace\n        "if": ${{ false }}\n        shell: bash'
        )
      );
    },
  },
  {
    name: 'whitespace-step-rejects-quoted-continue-key',
    expected: 'release-gates execution control mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '      - name: Check committed patch whitespace\n        shell: bash',
          "      - name: Check committed patch whitespace\n        'continue-on-error': true\n        shell: bash"
        )
      );
    },
  },
  {
    name: 'privacy-step-rejects-quoted-if-key',
    expected: 'release-gates execution control mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '      - name: Scan committed secrets and privacy\n        shell: bash',
          "      - name: Scan committed secrets and privacy\n        'if': ${{ false }}\n        shell: bash"
        )
      );
    },
  },
  {
    name: 'privacy-step-rejects-quoted-continue-key',
    expected: 'release-gates execution control mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '      - name: Scan committed secrets and privacy\n        shell: bash',
          '      - name: Scan committed secrets and privacy\n        "continue-on-error": true\n        shell: bash'
        )
      );
    },
  },
  {
    name: 'privacy-step-rejects-escaped-if-key',
    expected: 'release-gates execution control mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '      - name: Scan committed secrets and privacy\n        shell: bash',
          '      - name: Scan committed secrets and privacy\n        "i\\u0066": ${{ false }}\n        shell: bash'
        )
      );
    },
  },
  {
    name: 'privacy-step-rejects-explicit-if-key',
    expected: 'release-gates execution control mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '      - name: Scan committed secrets and privacy\n        shell: bash',
          '      - name: Scan committed secrets and privacy\n        ? if\n        : ${{ false }}\n        shell: bash'
        )
      );
    },
  },
  {
    name: 'privacy-step-rejects-spaced-continue-key',
    expected: 'release-gates execution control mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '      - name: Scan committed secrets and privacy\n        shell: bash',
          '      - name: Scan committed secrets and privacy\n        continue-on-error : true\n        shell: bash'
        )
      );
    },
  },
  {
    name: 'release-gates-waits-for-timestamp-matrix',
    expected: 'release-gates dependency mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, 'needs: [fast-release-package, timestamp-expand-postgresql]', 'needs: [fast-release-package]')
      );
    },
  },
  {
    name: 'release-gates-rejects-job-level-always',
    expected: 'release-gates execution control mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '  release-gates:\n    name: Full release gates and live demo\n    needs: [fast-release-package, timestamp-expand-postgresql]\n    runs-on:',
          '  release-gates:\n    name: Full release gates and live demo\n    needs: [fast-release-package, timestamp-expand-postgresql]\n    if: ${{ always() }}\n    runs-on:'
        )
      );
    },
  },
  {
    name: 'release-gates-fail-closed-on-prerequisite-failure',
    expected: 'release-gates dependency mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, '[[ "$FAST_RELEASE_RESULT" == success ]]', ': "$FAST_RELEASE_RESULT"')
      );
    },
  },
  {
    name: 'full-go-suite-freshness-is-validated',
    expected: 'release-gates full Go suite mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, 'go test -p=1 ./... -count=1 -timeout 60m', 'go test -p=1 ./... -timeout 60m')
      );
    },
  },
  {
    name: 'full-go-suite-rejects-block-scalar-decoy',
    expected: 'release-gates full Go suite mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) => {
        const withoutRealStep = replaceOnce(
          text,
          '      - name: Run Go tests\n        env:\n          DISTR_TEST_DATABASE_URL: postgres://local:local@localhost:5432/distr?sslmode=disable\n        run: go test -p=1 ./... -count=1 -timeout 60m\n',
          ''
        );
        return replaceOnce(
          withoutRealStep,
          '      - name: Validate migrations\n        run: hack/validate-migrations.sh',
          '      - name: Validate migrations\n        run: |\n          exit 0\n          - name: Run Go tests\n            env:\n              DISTR_TEST_DATABASE_URL: postgres://local:local@localhost:5432/distr?sslmode=disable\n            run: go test -p=1 ./... -count=1 -timeout 60m'
        );
      });
    },
  },
  {
    name: 'git-diff-check-is-validated',
    expected: 'release-gates committed diff check mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, 'git diff --check "$base" HEAD', 'git diff --check')
      );
    },
  },
  {
    name: 'git-diff-check-cannot-be-bypassed-by-early-success',
    expected: 'release-gates committed diff check mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      const required =
        '          PUSH_BEFORE_SHA: ${{ github.event.before }}\n        run: |\n          set -Eeuo pipefail\n          case "$EVENT_NAME" in';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          required,
          '          PUSH_BEFORE_SHA: ${{ github.event.before }}\n        run: |\n          set -Eeuo pipefail\n          exit 0\n          case "$EVENT_NAME" in'
        )
      );
    },
  },
  {
    name: 'git-diff-check-non-ancestor-range-is-direct',
    expected: 'release-gates committed diff check mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, 'git diff --check "$base" HEAD', 'git diff --check "$base"...HEAD')
      );
    },
  },
  {
    name: 'git-diff-check-fetch-depth-is-validated',
    expected: 'release-gates committed diff check mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) => replaceOnce(text, 'fetch-depth: 0', 'fetch-depth: 1'));
    },
  },
  {
    name: 'git-diff-check-pr-base-is-validated',
    expected: 'release-gates committed diff check mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          'PULL_REQUEST_BASE_SHA: ${{ github.event.pull_request.base.sha }}\n          PUSH_BEFORE_SHA: ${{ github.event.before }}',
          'PULL_REQUEST_BASE_SHA: ${{ github.sha }}\n          PUSH_BEFORE_SHA: ${{ github.event.before }}'
        )
      );
    },
  },
  {
    name: 'git-diff-check-push-before-is-validated',
    expected: 'release-gates committed diff check mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          'PULL_REQUEST_BASE_SHA: ${{ github.event.pull_request.base.sha }}\n          PUSH_BEFORE_SHA: ${{ github.event.before }}',
          'PULL_REQUEST_BASE_SHA: ${{ github.event.pull_request.base.sha }}\n          PUSH_BEFORE_SHA: ${{ github.sha }}'
        )
      );
    },
  },
  {
    name: 'git-diff-check-initial-push-fallback-is-validated',
    expected: 'release-gates committed diff check mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, 'empty_tree="$(git hash-object -t tree /dev/null)"', 'base="$PUSH_BEFORE_SHA"')
      );
    },
  },
  {
    name: 'git-diff-check-initial-push-root-commit-fallback-is-rejected',
    expected: 'release-gates committed diff check mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          'git diff --check "$empty_tree" HEAD',
          'git diff --check "$(git rev-list --max-parents=0 HEAD | tail -n 1)"...HEAD'
        )
      );
    },
  },
  {
    name: 'git-diff-check-base-existence-is-validated',
    expected: 'release-gates committed diff check mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '          fi\n          git cat-file -e "${base}^{commit}"\n          git diff --check "$base" HEAD',
          '          fi\n          git rev-parse "$base" >/dev/null\n          git diff --check "$base" HEAD'
        )
      );
    },
  },
  {
    name: 'git-diff-check-fetches-orphaned-push-base',
    expected: 'release-gates committed diff check mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          'git fetch --no-tags --no-recurse-submodules --depth=1 origin "$base"',
          ': "missing push base"'
        )
      );
    },
  },
  {
    name: 'gitleaks-version-is-pinned',
    expected: 'release-gates privacy gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, "GITLEAKS_VERSION='8.30.1'", "GITLEAKS_VERSION='8.30.2'")
      );
    },
  },
  {
    name: 'gitleaks-archive-sha-is-pinned',
    expected: 'release-gates privacy gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          "GITLEAKS_SHA256='551f6fc83ea457d62a0d98237cbad105af8d557003051f41f3e7ca7b3f2470eb'",
          "GITLEAKS_SHA256='aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'"
        )
      );
    },
  },
  {
    name: 'gitleaks-checksum-verification-cannot-fail-open',
    expected: 'release-gates privacy gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      const required = `printf '%s  %s\\n' "$GITLEAKS_SHA256" "$gitleaks_archive" | sha256sum --check --status`;
      updateFixture(fixtureRoot, file, (text) => replaceOnce(text, required, `${required} || true`));
    },
  },
  {
    name: 'gitleaks-download-requires-https-and-tls12',
    expected: 'release-gates privacy gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          "curl --proto '=https' --tlsv1.2 --fail --location --silent --show-error",
          'curl --fail --location --silent --show-error'
        )
      );
    },
  },
  {
    name: 'gitleaks-event-range-is-validated',
    expected: 'release-gates privacy gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, '--log-opts="$gitleaks_log_opts"', '--log-opts=HEAD')
      );
    },
  },
  {
    name: 'privacy-scan-uses-verified-checkout-head',
    expected: 'release-gates privacy gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          `head_commit="$(git rev-parse --verify 'HEAD^{commit}')"`,
          'head_commit="$PULL_REQUEST_HEAD_SHA"'
        )
      );
    },
  },
  {
    name: 'gitleaks-merge-resolution-diffs-are-required',
    expected: 'release-gates privacy gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        text.replace(
          /gitleaks_log_opts="--full-history --root --diff-merges=separate --no-ext-diff --no-textconv /g,
          'gitleaks_log_opts="--full-history --root --diff-merges=off --no-ext-diff --no-textconv '
        )
      );
    },
  },
  {
    name: 'gitleaks-default-only-config-is-validated',
    expected: 'release-gates privacy gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, "printf '[extend]\\nuseDefault = true\\n'", "printf '[extend]\\nuseDefault = false\\n'")
      );
    },
  },
  {
    name: 'gitleaks-repository-ignore-is-rejected',
    expected: 'release-gates privacy gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, '--gitleaks-ignore-path "$gitleaks_ignore"', '--gitleaks-ignore-path .gitleaksignore')
      );
    },
  },
  {
    name: 'gitleaks-inline-allow-comments-are-disabled',
    expected: 'release-gates privacy gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) => replaceOnce(text, '--ignore-gitleaks-allow', '--verbose'));
    },
  },
  {
    name: 'gitleaks-baseline-is-rejected',
    expected: 'release-gates privacy gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, '--redact=100', '--redact=100\\n            --baseline-path "$scan_dir/baseline.json"')
      );
    },
  },
  {
    name: 'gitleaks-exit-code-override-is-rejected',
    expected: 'release-gates privacy gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, '--redact=100 \\', '--redact=100 \\\n            --exit-code 0 \\')
      );
    },
  },
  {
    name: 'gitleaks-status-capture-cannot-be-overridden',
    expected: 'release-gates privacy gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) => replaceOnce(text, 'gitleaks_status=$?', 'gitleaks_status=0'));
    },
  },
  {
    name: 'gitleaks-status-capture-must-be-immediate',
    expected: 'release-gates privacy gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '"$GITHUB_WORKSPACE" >"$gitleaks_stdout" 2>"$gitleaks_stderr"\n          gitleaks_status=$?',
          '"$GITHUB_WORKSPACE" >"$gitleaks_stdout" 2>"$gitleaks_stderr"\n          true\n          gitleaks_status=$?'
        )
      );
    },
  },
  {
    name: 'gitleaks-finding-exit-must-remain-fail-closed',
    expected: 'release-gates privacy gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          "              printf 'default secret scan detected findings\\n' >&2\n              exit 1",
          "              printf 'default secret scan detected findings\\n' >&2"
        )
      );
    },
  },
  {
    name: 'privacy-step-rejects-early-success-exit',
    expected: 'release-gates privacy gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '          set +e\n          "$scan_dir/gitleaks" git \\',
          '          exit 0\n          set +e\n          "$scan_dir/gitleaks" git \\'
        )
      );
    },
  },
  {
    name: 'privacy-step-rejects-early-return',
    expected: 'release-gates privacy gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '          set +e\n          "$scan_dir/gitleaks" git \\',
          '          return 0\n          set +e\n          "$scan_dir/gitleaks" git \\'
        )
      );
    },
  },
  {
    name: 'privacy-step-rejects-uncalled-wrapper',
    expected: 'release-gates privacy gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) => {
        const opened = replaceOnce(
          text,
          '          set +e\n          "$scan_dir/gitleaks" git \\',
          '          privacy_gate() {\n          set +e\n          "$scan_dir/gitleaks" git \\'
        );
        return replaceOnce(
          opened,
          "              printf 'semantic privacy scan failed closed\\n' >&2\n              exit 1\n              ;;\n          esac\n      - name: Setup Go",
          "              printf 'semantic privacy scan failed closed\\n' >&2\n              exit 1\n              ;;\n          esac\n          }\n      - name: Setup Go"
        );
      });
    },
  },
  {
    name: 'privacy-step-rejects-range-collapse-before-semantic-scan',
    expected: 'release-gates privacy gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      const required =
        'node hack/pr054a-validate-timestamp-expand.mjs --scan-privacy-diff "$base_object" "$head_commit"';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, required, `base_object="$head_commit"\n          ${required}`)
      );
    },
  },
  {
    name: 'privacy-step-rejects-default-config-overwrite-before-gitleaks',
    expected: 'release-gates privacy gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '          chmod 0600 "$gitleaks_stdout" "$gitleaks_stderr"\n\n          set +e',
          '          chmod 0600 "$gitleaks_stdout" "$gitleaks_stderr"\n          printf \'[extend]\\nuseDefault = false\\n\' >"$gitleaks_config"\n\n          set +e'
        )
      );
    },
  },
  {
    name: 'privacy-step-rejects-obfuscated-early-success-command',
    expected: 'release-gates privacy gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '          set -Eeuo pipefail\n          scan_dir="$(mktemp -d)"',
          '          set -Eeuo pipefail\n          terminate=ex\n          terminate="${terminate}it"\n          "$terminate" 0\n          scan_dir="$(mktemp -d)"'
        )
      );
    },
  },
  {
    name: 'privacy-step-rejects-bash-env-metadata',
    expected: 'release-gates privacy gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '      - name: Scan committed secrets and privacy\n        shell: bash\n        env:\n          EVENT_NAME: ${{ github.event_name }}',
          '      - name: Scan committed secrets and privacy\n        shell: bash\n        env:\n          BASH_ENV: hack/privacy-bootstrap.sh\n          EVENT_NAME: ${{ github.event_name }}'
        )
      );
    },
  },
  {
    name: 'privacy-step-rejects-working-directory-metadata',
    expected: 'release-gates privacy gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '      - name: Scan committed secrets and privacy\n        shell: bash',
          '      - name: Scan committed secrets and privacy\n        shell: bash\n        working-directory: /tmp'
        )
      );
    },
  },
  {
    name: 'privacy-step-rejects-duplicate-step',
    expected: 'release-gates privacy gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) => {
        const privacyStart = text.indexOf('      - name: Scan committed secrets and privacy\n');
        const nextStep = text.indexOf('      - name: Setup Go\n', privacyStart);
        if (privacyStart < 0 || nextStep < 0) throw new Error('privacy step fixture bounds not found');
        const duplicate =
          '      - name: Scan committed secrets and privacy\n        shell: bash\n        run: echo duplicate\n';
        return `${text.slice(0, nextStep)}${duplicate}${text.slice(nextStep)}`;
      });
    },
  },
  {
    name: 'privacy-cli-cannot-fail-open',
    expected: 'release-gates privacy gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      const required =
        'node hack/pr054a-validate-timestamp-expand.mjs --scan-privacy-diff "$base_object" "$head_commit"';
      updateFixture(fixtureRoot, file, (text) => replaceOnce(text, required, `${required} || true`));
    },
  },
  {
    name: 'privacy-cli-status-capture-must-be-immediate',
    expected: 'release-gates privacy gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      const required =
        'node hack/pr054a-validate-timestamp-expand.mjs --scan-privacy-diff "$base_object" "$head_commit"';
      updateFixture(fixtureRoot, file, (text) => replaceOnce(text, required, `${required}\n          true`));
    },
  },
  {
    name: 'privacy-step-cannot-continue-on-error',
    expected: 'release-gates execution control mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '      - name: Scan committed secrets and privacy\n        shell: bash',
          '      - name: Scan committed secrets and privacy\n        continue-on-error: true\n        shell: bash'
        )
      );
    },
  },
  {
    name: 'privacy-step-cannot-be-conditionally-skipped',
    expected: 'release-gates execution control mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '      - name: Scan committed secrets and privacy\n        shell: bash',
          '      - name: Scan committed secrets and privacy\n        if: ${{ false }}\n        shell: bash'
        )
      );
    },
  },
  {
    name: 'gitleaks-rule-restriction-is-rejected',
    expected: 'release-gates privacy gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/community-release-hardening.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, '--redact=100 \\', '--redact=100 \\\n            --enable-rule generic-api-key \\')
      );
    },
  },
  {
    name: 'timestamp-evidence-field-inventory-is-validated',
    expected: 'timestamp evidence field inventory mismatch',
    mutate(fixtureRoot) {
      const file = 'docs/security/release-hardening-checklist.md';
      updateFixture(fixtureRoot, file, (text) => replaceOnce(text, 'exact legacy `rawValue`', 'legacy timestamps'));
    },
  },
  {
    name: 'timestamp-evidence-free-text-safety-is-validated',
    expected: 'timestamp evidence free-text safety mismatch',
    mutate(fixtureRoot) {
      const file = 'docs/security/release-hardening-checklist.md';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, 'customer data, or private absolute paths', 'private paths')
      );
    },
  },
  {
    name: 'timestamp-seal-env-file-semantics-is-validated',
    expected: 'timestamp seal preparation mismatch',
    mutate(fixtureRoot) {
      const file = 'docs/operations/server-docker-compose-deploy.md';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '`--env-file` does not export those values into the host shell',
          '`--env-file` loads host variables'
        )
      );
    },
  },
  {
    name: 'timestamp-seal-author-definition-is-validated',
    expected: 'timestamp seal preparation mismatch',
    mutate(fixtureRoot) {
      const file = 'docs/operations/server-docker-compose-deploy.md';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          'export DISTR_TIMESTAMP_AUTHOR="CHANGE_ME_NON_SECRET_AUTHOR_IDENTITY"',
          'export DISTR_TIMESTAMP_AUTHOR=""'
        )
      );
    },
  },
  {
    name: 'timestamp-seal-reviewer-definition-is-validated',
    expected: 'timestamp seal preparation mismatch',
    mutate(fixtureRoot) {
      const file = 'docs/operations/server-docker-compose-deploy.md';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          'export DISTR_TIMESTAMP_REVIEWER="CHANGE_ME_DISTINCT_NON_SECRET_REVIEWER_IDENTITY"',
          'export DISTR_TIMESTAMP_REVIEWER="$DISTR_TIMESTAMP_AUTHOR"'
        )
      );
    },
  },
  {
    name: 'timestamp-seal-evidence-reference-definition-is-validated',
    expected: 'timestamp seal preparation mismatch',
    mutate(fixtureRoot) {
      const file = 'docs/operations/server-docker-compose-deploy.md';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          'export DISTR_TIMESTAMP_EVIDENCE_REFERENCE="CHANGE_ME_OPAQUE_NON_SECRET_EVIDENCE_REFERENCE"',
          'export DISTR_TIMESTAMP_EVIDENCE_REFERENCE=""'
        )
      );
    },
  },
  {
    name: 'timestamp-seal-distinct-reviewer-is-validated',
    expected: 'timestamp seal preparation mismatch',
    mutate(fixtureRoot) {
      const file = 'docs/operations/server-docker-compose-deploy.md';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '[[ "$DISTR_TIMESTAMP_AUTHOR" != "$DISTR_TIMESTAMP_REVIEWER" ]]',
          '[[ -n "$DISTR_TIMESTAMP_REVIEWER" ]]'
        )
      );
    },
  },
  {
    name: 'timestamp-seal-evidence-directory-load-is-validated',
    expected: 'timestamp seal preparation mismatch',
    mutate(fixtureRoot) {
      const file = 'docs/operations/server-docker-compose-deploy.md';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          'export DISTR_TIMESTAMP_EVIDENCE_DIR="$(read_compose_env_value DISTR_TIMESTAMP_EVIDENCE_DIR)"',
          'export DISTR_TIMESTAMP_EVIDENCE_DIR="${DISTR_TIMESTAMP_EVIDENCE_DIR:-}"'
        )
      );
    },
  },
  {
    name: 'timestamp-seal-release-identity-load-is-validated',
    expected: 'timestamp seal preparation mismatch',
    mutate(fixtureRoot) {
      const file = 'docs/operations/server-docker-compose-deploy.md';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          'export DISTR_RELEASE_COMMIT="$(read_compose_env_value DISTR_RELEASE_COMMIT)"',
          'export DISTR_RELEASE_COMMIT="$(git rev-parse HEAD)"'
        )
      );
    },
  },
  {
    name: 'timestamp-seal-evidence-bundle-label-is-validated',
    expected: 'timestamp seal preparation mismatch',
    mutate(fixtureRoot) {
      const file = 'docs/operations/server-docker-compose-deploy.md';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, 'timestamp-evidence-bundle-v1$ ]]', 'timestamp-evidence$ ]]')
      );
    },
  },
  {
    name: 'timestamp-seal-apply-checksum-consistency-is-validated',
    expected: 'timestamp seal preparation mismatch',
    mutate(fixtureRoot) {
      const file = 'docs/operations/server-docker-compose-deploy.md';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '[[ "$(read_compose_env_value DISTR_TIMESTAMP_EVIDENCE_CHECKSUM)" == "$DISTR_TIMESTAMP_EVIDENCE_CHECKSUM" ]]',
          'read_compose_env_value DISTR_TIMESTAMP_EVIDENCE_CHECKSUM >/dev/null'
        )
      );
    },
  },
  {
    name: 'identifier-prefixed-credential',
    expected: 'contains credential assignment',
    mutate(fixtureRoot) {
      appendToSmokeSection(fixtureRoot, fragment('DISTR_PASS', 'WORD=not-a-placeholder'));
    },
  },
  {
    name: 'uppercase-prefixed-api-key',
    expected: 'contains credential assignment',
    mutate(fixtureRoot) {
      appendToSmokeSection(fixtureRoot, fragment('DISTR_CLIENT_API_', 'KEY=live-value'));
    },
  },
  {
    name: 'camel-case-client-secret',
    expected: 'contains credential assignment',
    mutate(fixtureRoot) {
      appendToSmokeSection(fixtureRoot, fragment('client', 'Secret=live-value'));
    },
  },
  {
    name: 'camel-case-db-password',
    expected: 'contains credential assignment',
    mutate(fixtureRoot) {
      appendToSmokeSection(fixtureRoot, fragment('db', 'Password=live-value'));
    },
  },
  {
    name: 'quoted-json-db-password',
    expected: 'contains credential assignment',
    mutate(fixtureRoot) {
      appendToSmokeSection(fixtureRoot, fragment('"db', 'Password": "live-value",'));
    },
  },
  {
    name: 'unseparated-uppercase-db-password',
    expected: 'contains credential assignment',
    mutate(fixtureRoot) {
      appendToSmokeSection(fixtureRoot, fragment('DBPASS', 'WORD=live-value'));
    },
  },
  {
    name: 'safe-placeholder-with-comma-live-suffix',
    expected: 'contains credential assignment',
    mutate(fixtureRoot) {
      appendToSmokeSection(fixtureRoot, fragment('DB_PASS', 'WORD=CHANGE_ME,live-value'));
    },
  },
  {
    name: 'angle-placeholder-prefix-with-live-suffix',
    expected: 'contains credential assignment',
    mutate(fixtureRoot) {
      appendToSmokeSection(fixtureRoot, fragment('DISTR_SE', 'CRET=<placeholder>live'));
    },
  },
  {
    name: 'redacted-placeholder-prefix-with-live-suffix',
    expected: 'contains credential assignment',
    mutate(fixtureRoot) {
      appendToSmokeSection(fixtureRoot, fragment('API_TO', 'KEN=[REDACTED]live'));
    },
  },
  {
    name: 'quoted-placeholder-prefix-with-live-suffix',
    expected: 'contains credential assignment',
    mutate(fixtureRoot) {
      appendToSmokeSection(fixtureRoot, fragment('dbPass', 'word="CHANGE_ME"live'));
    },
  },
  {
    name: 'contextual-ipv4-url',
    expected: 'contains IPv4 address',
    mutate(fixtureRoot) {
      appendToSmokeSection(fixtureRoot, fragment('Private operator URL: https://192.0.', '2.40/ready'));
    },
  },
  {
    name: 'private-host-version-ipv4',
    expected: 'contains IPv4 address',
    mutate(fixtureRoot) {
      appendToSmokeSection(fixtureRoot, fragment('Private host version: 192.168.', '1.10'));
    },
  },
  {
    name: 'bare-postgres-ipv4',
    expected: 'contains IPv4 address',
    mutate(fixtureRoot) {
      appendToSmokeSection(fixtureRoot, fragment('POSTGRES=192.168.', '1.10'));
    },
  },
  {
    name: 'inline-heading-decoy-before-real-section',
    expected: 'contains credential assignment',
    mutate(fixtureRoot) {
      const file = 'docs/operations/operator-smoke-test.md';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '## Timestamp Expand Smoke',
          'Reference: `## Timestamp Expand Smoke`\n\n## Timestamp Expand Smoke'
        )
      );
      appendToSmokeSection(fixtureRoot, fragment('DBPASS', 'WORD=live-value'));
    },
  },
  {
    name: 'duplicate-exact-smoke-heading',
    expected: 'must contain exactly one section ## Timestamp Expand Smoke',
    mutate(fixtureRoot) {
      appendToSmokeSection(fixtureRoot, '## Timestamp Expand Smoke');
    },
  },
  {
    name: 'approved-manifest-sidecar-procedure-is-validated',
    expected: 'approved manifest sidecar procedure mismatch',
    mutate(fixtureRoot) {
      const file = 'docs/operations/server-docker-compose-deploy.md';
      const required = '  sha256sum --text -- "$approved_name" >"$sidecar_name"';
      updateFixture(fixtureRoot, file, (text) => replaceOnce(text, required, '  sha256sum --text -- "$approved_name"'));
    },
  },
  {
    name: 'approved-manifest-sidecar-text-mode-is-validated',
    expected: 'approved manifest sidecar procedure mismatch',
    mutate(fixtureRoot) {
      const file = 'docs/operations/server-docker-compose-deploy.md';
      const required = '  sha256sum --text -- "$approved_name" >"$sidecar_name"';
      updateFixture(fixtureRoot, file, (text) => {
        if (text.includes(required)) {
          return replaceOnce(text, required, '  sha256sum -- "$approved_name" >"$sidecar_name"');
        }
        return replaceOnce(text, 'The relative `sha256sum` input', 'The binary-mode `sha256sum` input');
      });
    },
  },
  {
    name: 'server-tool-prerequisites-are-validated',
    expected: 'server tool prerequisites mismatch',
    mutate(fixtureRoot) {
      const file = 'docs/operations/server-docker-compose-deploy.md';
      const required = '- `curl`, `openssl`, `jq`, `sha256sum`, `bash`, and `flock`.';
      updateFixture(fixtureRoot, file, (text) => replaceOnce(text, required, required.replace('`jq`, ', '')));
    },
  },
  {
    name: 'release-identity-handoff-is-validated',
    expected: 'release identity handoff mismatch',
    mutate(fixtureRoot) {
      const file = 'docs/operations/server-docker-compose-deploy.md';
      const required = '`DISTR_IMAGE_DIGEST` values together';
      updateFixture(fixtureRoot, file, (text) => replaceOnce(text, required, 'copy only its `DISTR_IMAGE_REF` value'));
    },
  },
  {
    name: 'callback-probe-handoff-is-validated',
    expected: 'callback probe handoff mismatch',
    mutate(fixtureRoot) {
      const file = 'docs/operations/server-docker-compose-deploy.md';
      const required = 'Set `DISTR_CALLBACK_PROBE_URL` to a non-`CHANGE_ME` loopback callbacks route';
      updateFixture(fixtureRoot, file, (text) => replaceOnce(text, required, required.replace('non-`CHANGE_ME` ', '')));
    },
  },
  {
    name: 'timestamp-audit-handoff-is-validated',
    expected: 'timestamp audit handoff mismatch',
    mutate(fixtureRoot) {
      const file = 'docs/operations/server-docker-compose-deploy.md';
      const required = 'its read-only `DISTR_AUDIT_HISTORY_PROBE_TOKEN`';
      updateFixture(fixtureRoot, file, (text) => replaceOnce(text, required, required.replace('read-only ', '')));
    },
  },
  {
    name: 'optional-deploy-identity-order-is-validated',
    expected: 'optional deploy procedure mismatch',
    mutate(fixtureRoot) {
      const file = 'docs/operations/server-docker-compose-deploy.md';
      const required = '7. Resolves the pushed tag to an ECR digest.';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, required, '7. Resolves the pushed tag to an ECR digest and writes release metadata first.')
      );
    },
  },
  {
    name: 'optional-deploy-procedure-is-validated',
    expected: 'optional deploy procedure mismatch',
    mutate(fixtureRoot) {
      const file = 'docs/operations/server-docker-compose-deploy.md';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, 'Runs `distr migrate` explicitly.', 'Runs migration implicitly.')
      );
    },
  },
  {
    name: 'example-com-suffix-email',
    expected: 'contains non-example email',
    mutate(fixtureRoot) {
      appendToSmokeSection(fixtureRoot, fragment('Contact: operator@example.com', '.evil'));
    },
  },
  {
    name: 'example-invalid-suffix-email',
    expected: 'contains non-example email',
    mutate(fixtureRoot) {
      appendToSmokeSection(fixtureRoot, fragment('Contact: operator@example.invalid', '.corp'));
    },
  },
  {
    name: 'example-com-hyphenated-email-domain',
    expected: 'contains non-example email',
    mutate(fixtureRoot) {
      appendToSmokeSection(fixtureRoot, fragment('Contact: operator@example.com', '-evil'));
    },
  },
  {
    name: 'example-invalid-hyphenated-email-domain',
    expected: 'contains non-example email',
    mutate(fixtureRoot) {
      appendToSmokeSection(fixtureRoot, fragment('Contact: operator@example.invalid', '-corp'));
    },
  },
  {
    name: 'set-prefixed-distr-password',
    expected: 'contains credential assignment',
    mutate(fixtureRoot) {
      appendToSmokeSection(fixtureRoot, fragment('Set DISTR_PASS', 'WORD=live-value'));
    },
  },
  {
    name: 'set-prefixed-password',
    expected: 'contains credential assignment',
    mutate(fixtureRoot) {
      appendToSmokeSection(fixtureRoot, fragment('Set pass', 'word=live-value'));
    },
  },
  {
    name: 'dot-separated-db-password',
    expected: 'contains credential assignment',
    mutate(fixtureRoot) {
      appendToSmokeSection(fixtureRoot, fragment('db.pass', 'word=live-value'));
    },
  },
  {
    name: 'later-credential-assignment-on-line',
    expected: 'contains credential assignment',
    mutate(fixtureRoot) {
      appendToSmokeSection(fixtureRoot, fragment('Set mode=CHANGE_ME; db.pass', 'word=live-value'));
    },
  },
  {
    name: 'safe-placeholder-with-assignment-suffix',
    expected: 'contains credential assignment',
    mutate(fixtureRoot) {
      appendToSmokeSection(fixtureRoot, fragment('DISTR_PASS', 'WORD=CHANGE_ME; mode=live-value'));
    },
  },
  {
    name: 'go-lint-baseline-rejects-pull-request-permission',
    expected: 'build-hub.yaml: Go lint baseline gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/build-hub.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '  lint-and-test-go:\n    name: Lint and test (Go)\n    runs-on: ubuntu-latest\n    permissions:\n      contents: read',
          '  lint-and-test-go:\n    name: Lint and test (Go)\n    runs-on: ubuntu-latest\n    permissions:\n      contents: read\n      pull-requests: read'
        )
      );
    },
  },
  {
    name: 'go-lint-baseline-requires-full-checkout-history',
    expected: 'build-hub.yaml: Go lint baseline gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/build-hub.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, '          fetch-depth: 0', '          fetch-depth: 1')
      );
    },
  },
  {
    name: 'go-lint-baseline-resolver-cannot-be-renamed',
    expected: 'build-hub.yaml: Go lint baseline gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/build-hub.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, '      - name: Resolve Go lint baseline', '      - name: Guess Go lint baseline')
      );
    },
  },
  {
    name: 'go-lint-baseline-must-validate-checked-out-head',
    expected: 'build-hub.yaml: Go lint baseline gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/build-hub.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '          [[ "$head_sha" == "$EXPECTED_HEAD_SHA" ]] || {',
          '          [[ -n "$head_sha" ]] || {'
        )
      );
    },
  },
  {
    name: 'go-lint-baseline-must-use-pr-base-sha',
    expected: 'build-hub.yaml: Go lint baseline gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/build-hub.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '            pull_request) base="$PULL_REQUEST_BASE_SHA" ;;',
          '            pull_request) base="HEAD^" ;;'
        )
      );
    },
  },
  {
    name: 'go-lint-baseline-must-use-push-before-sha',
    expected: 'build-hub.yaml: Go lint baseline gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/build-hub.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, '            push) base="$PUSH_BEFORE_SHA" ;;', '            push) base="HEAD^" ;;')
      );
    },
  },
  {
    name: 'go-lint-baseline-initial-push-must-run-full-lint',
    expected: 'build-hub.yaml: Go lint baseline gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/build-hub.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '            printf \'args=\\n\' >>"$GITHUB_OUTPUT"',
          '            printf \'args=--new-from-rev=HEAD^\\n\' >>"$GITHUB_OUTPUT"'
        )
      );
    },
  },
  {
    name: 'go-lint-baseline-requires-lowercase-commit-sha',
    expected: 'build-hub.yaml: Go lint baseline gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/build-hub.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, '          [[ "$base" =~ ^[0-9a-f]{40}$ ]] || {', '          [[ -n "$base" ]] || {')
      );
    },
  },
  {
    name: 'go-lint-baseline-requires-commit-object',
    expected: 'build-hub.yaml: Go lint baseline gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/build-hub.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, '          git cat-file -e "${base}^{commit}"', '          git cat-file -e "$base"')
      );
    },
  },
  {
    name: 'go-lint-baseline-requires-ancestor',
    expected: 'build-hub.yaml: Go lint baseline gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/build-hub.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '          git merge-base --is-ancestor "$base" HEAD',
          '          git merge-base "$base" HEAD'
        )
      );
    },
  },
  {
    name: 'go-lint-baseline-output-must-use-validated-sha',
    expected: 'build-hub.yaml: Go lint baseline gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/build-hub.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '          printf \'args=--new-from-rev=%s\\n\' "$base" >>"$GITHUB_OUTPUT"',
          '          printf \'args=--new-from-rev=HEAD^\\n\' >>"$GITHUB_OUTPUT"'
        )
      );
    },
  },
  {
    name: 'go-lint-baseline-action-remains-pinned',
    expected: 'build-hub.yaml: Go lint baseline gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/build-hub.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          'golangci/golangci-lint-action@82606bf257cbaff209d206a39f5134f0cfbfd2ee',
          'golangci/golangci-lint-action@main'
        )
      );
    },
  },
  {
    name: 'go-lint-baseline-rejects-only-new-issues',
    expected: 'build-hub.yaml: Go lint baseline gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/build-hub.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(text, '          args: ${{ steps.lint-baseline.outputs.args }}', '          only-new-issues: true')
      );
    },
  },
  {
    name: 'go-lint-baseline-rejects-fail-open-exit-code',
    expected: 'build-hub.yaml: Go lint baseline gate mismatch',
    mutate(fixtureRoot) {
      const file = '.github/workflows/build-hub.yaml';
      updateFixture(fixtureRoot, file, (text) =>
        replaceOnce(
          text,
          '          args: ${{ steps.lint-baseline.outputs.args }}',
          '          args: ${{ steps.lint-baseline.outputs.args }} --issues-exit-code=0'
        )
      );
    },
  },
];

const acceptedScenarios = [
  {
    name: 'review-force-selector-in-comment-is-safe',
    mutate(fixtureRoot) {
      const file = 'internal/migrations/migrate.go';
      updateFixture(fixtureRoot, file, (text) => `${text}\n// Documentation may mention instance.Force(138) safely.\n`);
    },
  },
  {
    name: 'recover-dirty-reference-is-not-globally-banned',
    mutate(fixtureRoot) {
      const file = 'internal/migrations/migrate.go';
      updateFixture(
        fixtureRoot,
        file,
        (text) => `${text}\n// recover-dirty remains an allowed audited command reference.\n`
      );
    },
  },
  {
    name: 'four-part-release-version',
    mutate(fixtureRoot) {
      appendToSmokeSection(fixtureRoot, 'Compatible release version: 1.2.3.4');
    },
  },
  {
    name: 'explicit-safe-placeholders',
    mutate(fixtureRoot) {
      appendToSmokeSection(
        fixtureRoot,
        [
          'DISTR_PASSWORD=CHANGE_ME',
          'DISTR_SECRET=<placeholder>',
          'API_TOKEN=[REDACTED]',
          'clientSecret="CHANGE_ME"',
          "dbPassword='<placeholder>'",
        ].join('\n')
      );
    },
  },
  {
    name: 'quoted-json-safe-placeholder',
    mutate(fixtureRoot) {
      appendToSmokeSection(fixtureRoot, '"dbPassword": "CHANGE_ME",');
    },
  },
  {
    name: 'credential-metadata-assignments',
    mutate(fixtureRoot) {
      appendToSmokeSection(
        fixtureRoot,
        ['TOKEN_TTL=3600', 'PASSWORD_REQUIRED=true', 'PASSWORD_FILE=/run/secrets/password'].join('\n')
      );
    },
  },
  {
    name: 'non-address-four-part-placeholder',
    mutate(fixtureRoot) {
      appendToSmokeSection(fixtureRoot, 'Four-part version placeholder: 999.999.999.999');
    },
  },
  {
    name: 'exact-example-email-domains',
    mutate(fixtureRoot) {
      appendToSmokeSection(
        fixtureRoot,
        ['Contact: operator@example.com', 'Contact: operator@example.invalid', 'Contact: operator@EXAMPLE.COM'].join(
          '\n'
        )
      );
    },
  },
  {
    name: 'exact-example-email-domains-with-punctuation',
    mutate(fixtureRoot) {
      appendToSmokeSection(
        fixtureRoot,
        ['Contact: operator@example.com.', 'Contact: operator@example.invalid,', '(operator@EXAMPLE.COM).'].join('\n')
      );
    },
  },
  {
    name: 'safe-prefixed-and-dotted-credential-placeholders',
    mutate(fixtureRoot) {
      appendToSmokeSection(
        fixtureRoot,
        [
          'Set DISTR_PASSWORD=CHANGE_ME',
          'Set password=<placeholder>',
          'db.password=[REDACTED]',
          'Set "db.password": "CHANGE_ME",',
        ].join('\n')
      );
    },
  },
  {
    name: 'dotted-credential-metadata-assignments',
    mutate(fixtureRoot) {
      appendToSmokeSection(
        fixtureRoot,
        ['token.ttl=3600', 'password.required=true', 'password.file=/run/secrets/password'].join('\n')
      );
    },
  },
];

const runFixtureValidator = (fixtureRoot) => {
  const result = spawnSync(process.execPath, [path.join(fixtureRoot, validator)], {
    encoding: 'utf8',
    timeout: 60_000,
    maxBuffer: 1024 * 1024,
  });
  if (result.error || result.signal || result.status === null) {
    const reason = result.error?.code ?? result.signal ?? 'missing exit status';
    throw new Error(`fixture validator terminated abnormally: ${reason}`);
  }
  return result;
};

const abnormalFixtureRoot = copyFixture();
try {
  const expectedFailure = 'release-gates privacy gate mismatch';
  writeFileSync(
    path.join(abnormalFixtureRoot, validator),
    [
      `process.stdout.write(${JSON.stringify(`${expectedFailure}\n`)});`,
      "process.stdout.write('x'.repeat(2 * 1024 * 1024));",
    ].join('\n')
  );
  let rejectedAbnormalTermination = false;
  try {
    runFixtureValidator(abnormalFixtureRoot);
  } catch {
    rejectedAbnormalTermination = true;
  }
  if (!rejectedAbnormalTermination) {
    throw new Error('abnormally terminated fixture validator was allowed to reach substring checks');
  }
  console.log('PASS abnormal fixture validator termination is rejected');
} finally {
  rmSync(abnormalFixtureRoot, {recursive: true, force: true});
}

const baselineRoot = copyFixture();
try {
  const baseline = runFixtureValidator(baselineRoot);
  if (baseline.status !== 0) {
    throw new Error(`unmodified fixture must pass:\n${baseline.stdout ?? ''}\n${baseline.stderr ?? ''}`);
  }
  console.log('PASS unmodified-fixture baseline');
} finally {
  rmSync(baselineRoot, {recursive: true, force: true});
}

const failures = [];
for (const scenario of scenarios) {
  const fixtureRoot = copyFixture();
  try {
    scenario.mutate(fixtureRoot);
    const result = runFixtureValidator(fixtureRoot);
    const output = `${result.stdout ?? ''}\n${result.stderr ?? ''}`;
    if (result.status === 0) {
      failures.push(`${scenario.name}: validator accepted invalid fixture`);
      continue;
    }
    if (!output.includes(scenario.expected)) {
      failures.push(`${scenario.name}: expected ${JSON.stringify(scenario.expected)}, received ${output.trim()}`);
      continue;
    }
    console.log(`PASS ${scenario.name}: ${scenario.expected}`);
  } finally {
    rmSync(fixtureRoot, {recursive: true, force: true});
  }
}

for (const scenario of acceptedScenarios) {
  const fixtureRoot = copyFixture();
  try {
    scenario.mutate(fixtureRoot);
    const result = runFixtureValidator(fixtureRoot);
    if (result.status !== 0) {
      failures.push(
        `${scenario.name}: validator rejected safe fixture: ${(result.stderr ?? result.stdout ?? '').trim()}`
      );
      continue;
    }
    console.log(`PASS ${scenario.name}: safe fixture accepted`);
  } finally {
    rmSync(fixtureRoot, {recursive: true, force: true});
  }
}

const runGit = (cwd, args, input) =>
  spawnSync('git', args, {
    cwd,
    encoding: 'utf8',
    input,
    maxBuffer: 16 * 1024 * 1024,
    env: {...process.env, GIT_CONFIG_NOSYSTEM: '1'},
  });
const requireGitSuccess = (cwd, args, input) => {
  const result = runGit(cwd, args, input);
  if (result.status !== 0) {
    throw new Error(`git fixture command failed: git ${args[0]}`);
  }
  return result.stdout.trim();
};
const writePrivacyFiles = (repo, files) => {
  for (const [name, contents] of Object.entries(files ?? {})) {
    const target = path.join(repo, name);
    mkdirSync(path.dirname(target), {recursive: true});
    writeFileSync(target, contents);
  }
};
const createPrivacyRepo = (baseFiles = {}) => {
  const repo = mkdtempSync(path.join(tmpdir(), 'pr054a-privacy-'));
  mkdirSync(path.join(repo, 'hack'), {recursive: true});
  copyFileSync(path.join(root, validator), path.join(repo, validator));
  requireGitSuccess(repo, ['init', '--quiet']);
  requireGitSuccess(repo, ['config', 'user.name', 'Privacy Fixture']);
  requireGitSuccess(repo, ['config', 'user.email', 'privacy-fixture@example.invalid']);
  requireGitSuccess(repo, ['config', 'commit.gpgsign', 'false']);
  requireGitSuccess(repo, ['config', 'core.autocrlf', 'false']);
  writeFileSync(path.join(repo, 'README.md'), 'privacy fixture\n');
  writePrivacyFiles(repo, baseFiles);
  requireGitSuccess(repo, ['add', '--all']);
  requireGitSuccess(repo, ['commit', '--quiet', '-m', 'base']);
  return {repo, base: requireGitSuccess(repo, ['rev-parse', 'HEAD'])};
};
const commitPrivacyChange = (repo, {files = {}, remove = []}) => {
  writePrivacyFiles(repo, files);
  for (const name of remove) rmSync(path.join(repo, name), {force: true});
  requireGitSuccess(repo, ['add', '--all']);
  requireGitSuccess(repo, ['commit', '--quiet', '-m', 'privacy change']);
  return requireGitSuccess(repo, ['rev-parse', 'HEAD']);
};
const privacyWriteBlob = (repo, contents) => requireGitSuccess(repo, ['hash-object', '-w', '--stdin'], contents);
const privacyTreeWithEntry = (repo, sourceCommit, {mode, type, object, name}) => {
  const listing = runGit(repo, ['ls-tree', '-z', sourceCommit]);
  if (listing.status !== 0) throw new Error('git fixture command failed: git ls-tree');
  const records = listing.stdout.split('\0').filter((record) => record !== '');
  const suffix = `\t${name}`;
  const retained = records.filter((record) => !record.endsWith(suffix));
  retained.push(`${mode} ${type} ${object}${suffix}`);
  return requireGitSuccess(repo, ['mktree', '-z'], `${retained.join('\0')}\0`);
};
const privacyCommitTree = (repo, tree, parent) => {
  const args = ['commit-tree', tree, '-m', 'privacy plumbing change'];
  if (parent) args.push('-p', parent);
  return requireGitSuccess(repo, args);
};
const commitPrivacyPlumbingEntry = (repo, parent, entry) => {
  const object = entry.object ?? privacyWriteBlob(repo, entry.contents);
  const tree = privacyTreeWithEntry(repo, parent, {...entry, object});
  return privacyCommitTree(repo, tree, parent);
};
const runPrivacyScan = ({repo, base, head, requirePrivate = false, privateTokens, privateCanary}) => {
  const env = {...process.env};
  delete env.DISTR_PRIVATE_SCOPE_PATTERN;
  delete env.DISTR_PRIVATE_SCOPE_TOKENS;
  delete env.DISTR_PRIVATE_SCOPE_CANARY;
  if (privateTokens !== undefined) env.DISTR_PRIVATE_SCOPE_TOKENS = privateTokens;
  if (privateCanary !== undefined) env.DISTR_PRIVATE_SCOPE_CANARY = privateCanary;
  const args = [path.join(repo, validator), '--scan-privacy-diff', base, head];
  if (requirePrivate) args.push('--require-private-scope');
  return spawnSync(process.execPath, args, {
    cwd: repo,
    encoding: 'utf8',
    maxBuffer: 16 * 1024 * 1024,
    timeout: 10000,
    env,
  });
};
const privacyEmptyTree = (repo) => requireGitSuccess(repo, ['hash-object', '-t', 'tree', '--stdin'], '');
const privacyCommitCurrentTree = (repo, parent) =>
  privacyCommitTree(repo, requireGitSuccess(repo, ['rev-parse', `${parent}^{tree}`]), parent);
const buildPrivacyAddDeleteHistory = (repo, base, {name, contents}) => {
  commitPrivacyChange(repo, {files: {[name]: contents}});
  return {base, head: commitPrivacyChange(repo, {remove: [name]})};
};
const buildPrivacyObjectRootAddDeleteHistory = (repo, base, {name, contents, scanBase}) => {
  const leakTree = privacyTreeWithEntry(repo, base, {
    mode: '100644',
    type: 'blob',
    object: privacyWriteBlob(repo, contents),
    name,
  });
  const rootCommit = privacyCommitTree(repo, leakTree);
  const cleanTree = requireGitSuccess(repo, ['rev-parse', `${base}^{tree}`]);
  const head = privacyCommitTree(repo, cleanTree, rootCommit);
  return {base: scanBase === 'empty-tree' ? privacyEmptyTree(repo) : base, head};
};
const buildPrivacyMergeResolutionHistory = (repo, base, contents) => {
  requireGitSuccess(repo, ['switch', '--quiet', '-c', 'privacy-left', base]);
  const left = commitPrivacyChange(repo, {files: {'merge-resolution.txt': 'left safe value\n'}});
  requireGitSuccess(repo, ['switch', '--quiet', '-c', 'privacy-right', base]);
  const right = commitPrivacyChange(repo, {files: {'merge-resolution.txt': 'right safe value\n'}});
  requireGitSuccess(repo, ['switch', '--quiet', 'privacy-left']);
  const mergeAttempt = runGit(repo, ['merge', '--quiet', '--no-ff', '--no-commit', 'privacy-right']);
  if (mergeAttempt.status === 0) throw new Error('merge fixture did not conflict');
  writeFileSync(path.join(repo, 'merge-resolution.txt'), contents);
  requireGitSuccess(repo, ['add', '--', 'merge-resolution.txt']);
  requireGitSuccess(repo, ['commit', '--quiet', '-m', 'synthetic merge resolution']);
  return {base, head: requireGitSuccess(repo, ['rev-parse', 'HEAD']), left, right};
};
const buildPrivacyMergeRetentionHistory = (repo, base, retainedContents) => {
  requireGitSuccess(repo, ['switch', '--quiet', '-c', 'privacy-retain-left', base]);
  const mergedContents = `${retainedContents}left safe value\n`;
  const left = commitPrivacyChange(repo, {files: {'merge-retention.txt': mergedContents}});
  requireGitSuccess(repo, ['switch', '--quiet', '-c', 'privacy-delete-right', base]);
  const right = commitPrivacyChange(repo, {remove: ['merge-retention.txt']});
  requireGitSuccess(repo, ['switch', '--quiet', 'privacy-retain-left']);
  const mergeAttempt = runGit(repo, ['merge', '--quiet', '--no-ff', '--no-commit', 'privacy-delete-right']);
  if (mergeAttempt.status === 0) throw new Error('merge retention fixture did not conflict');
  writeFileSync(path.join(repo, 'merge-retention.txt'), mergedContents);
  requireGitSuccess(repo, ['add', '--', 'merge-retention.txt']);
  requireGitSuccess(repo, ['commit', '--quiet', '-m', 'synthetic merge retention']);
  return {base, head: requireGitSuccess(repo, ['rev-parse', 'HEAD']), left, right};
};
const verifyMergeAwareGitLog = (repo, base, head, secret) => {
  const common = ['log', '--format=', '-p', '-U0'];
  const plain = requireGitSuccess(repo, [...common, '--no-ext-diff', '--no-textconv', `${base}..${head}`]);
  const mergeAware = requireGitSuccess(repo, [
    ...common,
    '--full-history',
    '--root',
    '--diff-merges=separate',
    '--no-ext-diff',
    '--no-textconv',
    `${base}..${head}`,
  ]);
  if (plain.includes(secret)) throw new Error('plain Git log unexpectedly exposed merge-resolution-only secret');
  if (!mergeAware.includes(secret)) throw new Error('merge-aware Git log omitted merge-resolution-only secret');
};
const verifyMergeParentEdgeFixture = (repo, base, head, right, retainedValue) => {
  const common = ['--literal-pathspecs', 'diff', '--no-ext-diff', '--no-textconv', '--no-renames', '--unified=0'];
  const aggregate = requireGitSuccess(repo, [...common, base, head, '--', 'merge-retention.txt']);
  const rightEdge = requireGitSuccess(repo, [...common, right, head, '--', 'merge-retention.txt']);
  const addedContains = (patch, value) =>
    patch.split(/\r?\n/).some((line) => line.startsWith('+') && !line.startsWith('+++') && line.includes(value));
  if (addedContains(aggregate, retainedValue)) throw new Error('aggregate diff added retained legacy value');
  if (!addedContains(rightEdge, retainedValue)) throw new Error('merge parent edge omitted retained legacy value');
};
const verifyNonAncestorDiffCheck = (repo, base, head) => {
  const direct = runGit(repo, ['diff', '--check', base, head]);
  const mergeBaseRange = runGit(repo, ['diff', '--check', `${base}...${head}`]);
  if (direct.status !== 0) throw new Error('direct non-ancestor diff check failed');
  if (mergeBaseRange.status === 0) throw new Error('triple-dot non-ancestor fixture unexpectedly had a merge base');
};
const mergeResolutionSecret = fragment('AK', 'IA', 'QWERTYUIOPASDFGH');
const mergeResolutionPrivateEmail = fragment('operator@company.', 'corp');

const privacyCases = [
  {
    name: 'privacy-reserved-email-domains-pass',
    files: {
      'emails.txt': [
        'operator@localhost',
        'operator@example',
        'operator@sub.test',
        'operator@sub.example',
        'operator@sub.invalid',
        'operator@sub.localhost',
        'operator@sub.example.com',
        'operator@sub.example.net',
        'operator@sub.example.org',
      ].join('\n'),
    },
    status: 0,
  },
  {
    name: 'privacy-email-suffix-trick-rejects',
    files: {'email.txt': fragment('operator@example.com', '.evil')},
    status: 1,
    category: 'non-reserved-email',
    forbiddenOutput: [fragment('operator@example.com', '.evil')],
  },
  {
    name: 'privacy-non-reserved-single-label-email-rejects',
    files: {'email.txt': fragment('operator@', 'company')},
    status: 1,
    category: 'non-reserved-email',
    forbiddenOutput: [fragment('operator@', 'company')],
  },
  {
    name: 'privacy-single-label-non-email-at-syntax-is-safe',
    files: {
      'syntax.txt': [
        'WHERE id=@rowId',
        'registry.invalid/distr@sha256:aaaaaaaa',
        'postgres://distr:${postgres_password}@postgres:5432/distr',
      ].join('\n'),
    },
    status: 0,
  },
  {
    name: 'privacy-package-version-at-syntax-is-safe',
    files: {
      'package-versions.txt': [
        'go install golang.org/x/vuln/cmd/govulncheck@v1.6.0',
        fragment('package', atSign, '3.4.12:'),
        '@scope/name@10.29.7:',
        fragment('package', atSign, '1.2.3-beta.1:'),
      ].join('\n'),
    },
    status: 0,
  },
  {
    name: 'privacy-numeric-version-domain-email-rejects',
    files: {'email.txt': fragment('operator', atSign, '1.2.', '3')},
    status: 1,
    category: 'non-reserved-email',
    forbiddenOutput: [fragment('operator', atSign, '1.2.', '3')],
  },
  {
    name: 'privacy-v-prefixed-version-domain-email-rejects',
    files: {'email.txt': fragment('operator', atSign, 'v1.6.', '0')},
    status: 1,
    category: 'non-reserved-email',
    forbiddenOutput: [fragment('operator', atSign, 'v1.6.', '0')],
  },
  {
    name: 'privacy-assignment-version-domain-email-rejects',
    files: {'email.txt': fragment('contact=operator', atSign, '1.2.', '3:')},
    status: 1,
    category: 'non-reserved-email',
    forbiddenOutput: [fragment('contact=operator', atSign, '1.2.', '3:')],
  },
  {
    name: 'privacy-version-shaped-alpha-tld-email-rejects',
    files: {'email.txt': fragment('operator', atSign, 'v1.6.', 'company')},
    status: 1,
    category: 'non-reserved-email',
    forbiddenOutput: [fragment('operator', atSign, 'v1.6.', 'company')],
  },
  {
    name: 'privacy-new-side-line-number-is-exact',
    files: {
      'line-number.txt': ['safe first line', 'safe second line', fragment('operator@company.', 'corp')].join('\n'),
    },
    status: 1,
    category: 'non-reserved-email',
    expectedLine: 3,
    forbiddenOutput: [fragment('operator@company.', 'corp')],
  },
  {
    name: 'privacy-windows-backslash-home-rejects',
    files: {'path.txt': fragment('C:', '\\', 'Users', '\\', 'alice', '\\', 'artifact.txt')},
    status: 1,
    category: 'user-home-path',
    forbiddenOutput: [fragment('C:', '\\', 'Users', '\\', 'alice', '\\', 'artifact.txt')],
  },
  {
    name: 'privacy-windows-forward-slash-home-rejects',
    files: {'path.txt': fragment('D:', '/', 'Users', '/', 'alice', '/', 'artifact.txt')},
    status: 1,
    category: 'user-home-path',
    forbiddenOutput: [fragment('D:', '/', 'Users', '/', 'alice', '/', 'artifact.txt')],
  },
  {
    name: 'privacy-posix-home-rejects',
    files: {'path.txt': fragment('/', 'home', '/', 'alice', '/', 'artifact.txt')},
    status: 1,
    category: 'user-home-path',
    forbiddenOutput: [fragment('/', 'home', '/', 'alice', '/', 'artifact.txt')],
  },
  {
    name: 'privacy-posix-root-rejects',
    files: {'path.txt': fragment('/', 'root', '/', '.ssh', '/', 'config')},
    status: 1,
    category: 'user-home-path',
    forbiddenOutput: [fragment('/', 'root', '/', '.ssh', '/', 'config')],
  },
  {
    name: 'privacy-markdown-backtick-posix-home-rejects',
    files: {'path.txt': fragment('`', '/', 'home', '/', 'alice', '/', 'report.txt', '`')},
    status: 1,
    category: 'user-home-path',
    forbiddenOutput: [fragment('/', 'home', '/', 'alice', '/', 'report.txt')],
  },
  {
    name: 'privacy-bracket-delimited-posix-home-rejects',
    files: {'path.txt': fragment('[', '/', 'home', '/', 'alice', '/', 'report.txt', ']')},
    status: 1,
    category: 'user-home-path',
    forbiddenOutput: [fragment('/', 'home', '/', 'alice', '/', 'report.txt')],
  },
  {
    name: 'privacy-angle-delimited-posix-home-rejects',
    files: {'path.txt': fragment('<', '/', 'home', '/', 'alice', '/', 'report.txt', '>')},
    status: 1,
    category: 'user-home-path',
    forbiddenOutput: [fragment('/', 'home', '/', 'alice', '/', 'report.txt')],
  },
  {
    name: 'privacy-brace-delimited-root-home-rejects',
    files: {'path.txt': fragment('{', '/', 'root', '/', '.ssh', '/', 'config', '}')},
    status: 1,
    category: 'user-home-path',
    forbiddenOutput: [fragment('/', 'root', '/', '.ssh', '/', 'config')],
  },
  {
    name: 'privacy-file-uri-posix-home-rejects',
    files: {'path.txt': fragment('file://', '/', 'home', '/', 'alice', '/', 'report.txt')},
    status: 1,
    category: 'user-home-path',
    forbiddenOutput: [fragment('/', 'home', '/', 'alice', '/', 'report.txt')],
  },
  {
    name: 'privacy-https-home-route-is-safe',
    files: {'path.txt': 'https://example.com/home/alice/report.txt'},
    status: 0,
  },
  {
    name: 'privacy-unc-backslash-rejects',
    files: {'path.txt': fragment('\\\\', 'server', '\\', 'share', '\\', 'artifact.txt')},
    status: 1,
    category: 'user-home-path',
    forbiddenOutput: [fragment('\\\\', 'server', '\\', 'share', '\\', 'artifact.txt')],
  },
  {
    name: 'privacy-unc-forward-slash-rejects',
    files: {'path.txt': fragment('//', 'server', '/', 'share', '/', 'artifact.txt')},
    status: 1,
    category: 'user-home-path',
    forbiddenOutput: [fragment('//', 'server', '/', 'share', '/', 'artifact.txt')],
  },
  {
    name: 'privacy-markdown-backtick-unc-rejects',
    files: {'path.txt': fragment('`', '\\\\', 'server', '\\', 'share', '\\', 'report.txt', '`')},
    status: 1,
    category: 'user-home-path',
    forbiddenOutput: [fragment('\\\\', 'server', '\\', 'share', '\\', 'report.txt')],
  },
  {
    name: 'privacy-angle-delimited-unc-rejects',
    files: {'path.txt': fragment('<', '\\\\', 'server', '\\', 'share', '\\', 'report.txt', '>')},
    status: 1,
    category: 'user-home-path',
    forbiddenOutput: [fragment('\\\\', 'server', '\\', 'share', '\\', 'report.txt')],
  },
  {
    name: 'privacy-file-uri-unc-rejects',
    files: {'path.txt': fragment('file://', 'server', '/', 'share', '/', 'report.txt')},
    status: 1,
    category: 'user-home-path',
    forbiddenOutput: [fragment('//', 'server', '/', 'share', '/', 'report.txt')],
  },
  {
    name: 'privacy-safe-ipv4-ranges-and-version-pass',
    files: {
      'addresses.txt': [
        'Bind address: 0.0.0.0',
        'Health URL: http://127.0.0.1/ready',
        'Database DSN: postgres://local@127.0.0.1/distr',
        'Documentation host: 192.0.2.40',
        'Documentation endpoint: 198.51.100.25',
        'Documentation URL: https://203.0.113.10/ready',
        'Release version: 10.20.30.40',
      ].join('\n'),
    },
    status: 0,
  },
  {
    name: 'privacy-rfc5737-first-and-last-addresses-pass',
    files: {
      'addresses.txt': [
        'Documentation host: 192.0.2.0',
        'Documentation host: 192.0.2.255',
        'Documentation endpoint: 198.51.100.0',
        'Documentation endpoint: 198.51.100.255',
        'Documentation URL: https://203.0.113.0/ready',
        'Documentation URL: https://203.0.113.255/ready',
      ].join('\n'),
    },
    status: 0,
  },
  {
    name: 'privacy-bare-list-ipv4-rejects',
    files: {'address.txt': fragment('- 10.1.', '2.3')},
    status: 1,
    category: 'literal-ipv4',
    forbiddenOutput: [fragment('10.1.', '2.3')],
  },
  {
    name: 'privacy-generic-json-ipv4-rejects',
    files: {'address.json': fragment('["8.8.', '8.8"]')},
    status: 1,
    category: 'literal-ipv4',
    forbiddenOutput: [fragment('8.8.', '8.8')],
  },
  {
    name: 'privacy-rfc5737-adjacent-addresses-reject',
    files: {
      'addresses.txt': [
        fragment('Documentation host: 192.0.', '1.255'),
        fragment('Documentation endpoint: 198.51.', '101.0'),
        fragment('Documentation URL: https://203.0.', '114.0/ready'),
      ].join('\n'),
    },
    status: 1,
    category: 'literal-ipv4',
    forbiddenOutput: [fragment('192.0.', '1.255'), fragment('198.51.', '101.0'), fragment('203.0.', '114.0')],
  },
  {
    name: 'privacy-rfc1918-ipv4-rejects',
    files: {'address.txt': fragment('Private host: 192.168.', '1.10')},
    status: 1,
    category: 'literal-ipv4',
    forbiddenOutput: [fragment('192.168.', '1.10')],
  },
  {
    name: 'privacy-nonzero-unspecified-range-ipv4-rejects',
    files: {'address.txt': fragment('Bind address: 0.0.0.', '1')},
    status: 1,
    category: 'literal-ipv4',
    forbiddenOutput: [fragment('0.0.0.', '1')],
  },
  {
    name: 'privacy-link-local-ipv4-rejects',
    files: {'address.txt': fragment('Service endpoint: 169.254.', '10.20')},
    status: 1,
    category: 'literal-ipv4',
    forbiddenOutput: [fragment('169.254.', '10.20')],
  },
  {
    name: 'privacy-nondocumentation-public-ipv4-rejects',
    files: {'address.txt': fragment('Public server URL: https://8.8.', '8.8/ready')},
    status: 1,
    category: 'literal-ipv4',
    forbiddenOutput: [fragment('8.8.', '8.8')],
  },
  {
    name: 'privacy-hostname-key-ipv4-rejects',
    files: {'address.txt': fragment('hostname=8.8.', '8.8')},
    status: 1,
    category: 'literal-ipv4',
    forbiddenOutput: [fragment('8.8.', '8.8')],
  },
  {
    name: 'privacy-snake-case-address-key-ipv4-rejects',
    files: {'address.txt': fragment('server_address=8.8.', '8.8')},
    status: 1,
    category: 'literal-ipv4',
    forbiddenOutput: [fragment('8.8.', '8.8')],
  },
  {
    name: 'privacy-email-literal-ipv4-domain-rejects',
    files: {'address.txt': fragment('operator', '@', '8.8.', '8.8')},
    status: 1,
    category: 'literal-ipv4',
    forbiddenOutput: [fragment('8.8.', '8.8')],
  },
  {
    name: 'privacy-email-bracketed-literal-ipv4-domain-rejects',
    files: {'address.txt': fragment('operator@[8.8.', '8.8]')},
    status: 1,
    category: 'literal-ipv4',
    forbiddenOutput: [fragment('8.8.', '8.8')],
  },
  {
    name: 'privacy-camel-case-ip-key-rejects',
    files: {'address.txt': fragment('sourceIp=8.8.', '8.8')},
    status: 1,
    category: 'literal-ipv4',
    forbiddenOutput: [fragment('8.8.', '8.8')],
  },
  {
    name: 'privacy-unlabeled-four-part-value-rejects',
    files: {'value.txt': fragment('description=10.20.', '30.40')},
    status: 1,
    category: 'literal-ipv4',
    forbiddenOutput: [fragment('10.20.', '30.40')],
  },
  {
    name: 'privacy-snake-case-release-version-ipv4-is-safe',
    files: {'version.txt': 'release_version=10.20.30.40'},
    status: 0,
  },
  {
    name: 'privacy-private-content-hit-is-redacted',
    files: {'content.txt': fragment('adopter-private-', 'marker')},
    privateTokens: fragment('adopter-private-', 'marker'),
    privateCanary: fragment('canary-adopter-private-', 'marker'),
    status: 1,
    category: 'private-scope',
    forbiddenOutput: [fragment('adopter-private-', 'marker'), fragment('canary-adopter-private-', 'marker')],
  },
  {
    name: 'privacy-private-added-path-rejects',
    files: {[fragment('adopter-private-', 'path.txt')]: 'safe content'},
    privateTokens: fragment('adopter-private-', 'path'),
    privateCanary: fragment('canary-adopter-private-', 'path'),
    status: 1,
    category: 'private-scope',
    expectedPath: '<redacted>',
    forbiddenOutput: [
      fragment('adopter-private-', 'path.txt'),
      fragment('adopter-private-', 'path'),
      fragment('canary-adopter-private-', 'path'),
    ],
  },
  {
    name: 'privacy-user-home-added-path-rejects',
    files: {[fragment('artifacts/', 'home', '/', 'alice', '/report.txt')]: 'safe content'},
    status: 1,
    category: 'user-home-path',
    expectedPath: '<redacted>',
    forbiddenOutput: [fragment('artifacts/', 'home', '/', 'alice', '/report.txt')],
  },
  {
    name: 'privacy-email-added-path-rejects',
    files: {[fragment('artifacts/operator@company.', 'corp/report.txt')]: 'safe content'},
    status: 1,
    category: 'non-reserved-email',
    expectedPath: '<redacted>',
    forbiddenOutput: [fragment('artifacts/operator@company.', 'corp/report.txt')],
  },
  {
    name: 'privacy-legacy-shaped-modified-path-is-not-new-path-leak',
    baseFiles: {[fragment('artifacts/operator@company.', 'corp/report.txt')]: 'old safe content'},
    files: {[fragment('artifacts/operator@company.', 'corp/report.txt')]: 'new safe content'},
    status: 0,
  },
  {
    name: 'privacy-private-tokens-required',
    files: {'safe.txt': 'safe content'},
    requirePrivate: true,
    status: 1,
    expected: 'private scope tokens are required',
  },
  {
    name: 'privacy-private-canary-is-required-without-echo',
    files: {'safe.txt': 'safe content'},
    privateTokens: fragment('private-', 'token'),
    status: 1,
    expected: 'private scope canary is required',
    forbiddenOutput: [fragment('private-', 'token')],
  },
  {
    name: 'privacy-private-canary-must-prove-matcher-without-echo',
    files: {'safe.txt': 'safe content'},
    privateTokens: fragment('private-', 'token'),
    privateCanary: fragment('unmatched-', 'canary'),
    status: 1,
    expected: 'private scope canary validation failed',
    forbiddenOutput: [fragment('private-', 'token'), fragment('unmatched-', 'canary')],
  },
  {
    name: 'privacy-private-whitespace-list-is-no-op-and-rejected',
    files: {'safe.txt': 'safe content'},
    privateTokens: '  \n\t',
    privateCanary: fragment('private-', 'canary'),
    requirePrivate: true,
    status: 1,
    expected: 'private scope tokens are required',
    forbiddenOutput: [fragment('private-', 'canary')],
  },
  {
    name: 'privacy-regex-shaped-private-token-is-literal-and-redacted',
    files: {'content.txt': fragment('prefix-', '(a+)+$', '-suffix')},
    privateTokens: fragment('(a+', ')+$'),
    privateCanary: fragment('canary-', '(a+)+$'),
    status: 1,
    category: 'private-scope',
    forbiddenOutput: [fragment('(a+', ')+$'), fragment('canary-', '(a+)+$')],
  },
  {
    name: 'privacy-deletions-are-ignored',
    baseFiles: {'obsolete.txt': fragment('operator@company.', 'corp')},
    remove: ['obsolete.txt'],
    status: 0,
  },
  {
    name: 'privacy-linear-add-then-delete-history-rejects',
    buildHistory: (repo, base) =>
      buildPrivacyAddDeleteHistory(repo, base, {
        name: 'transient.txt',
        contents: fragment('operator@company.', 'corp'),
      }),
    status: 1,
    category: 'non-reserved-email',
    forbiddenOutput: [fragment('operator@company.', 'corp')],
  },
  {
    name: 'privacy-root-add-then-delete-history-rejects',
    buildHistory: (repo, base) =>
      buildPrivacyObjectRootAddDeleteHistory(repo, base, {
        name: 'root-transient.txt',
        contents: fragment('operator@company.', 'corp'),
        scanBase: 'empty-tree',
      }),
    status: 1,
    category: 'non-reserved-email',
    forbiddenOutput: [fragment('operator@company.', 'corp')],
  },
  {
    name: 'privacy-force-push-non-ancestor-history-rejects',
    buildHistory: (repo, base) =>
      buildPrivacyObjectRootAddDeleteHistory(repo, base, {
        name: 'replacement-transient.txt',
        contents: fragment('operator@company.', 'corp'),
      }),
    verifyHistory: ({repo, base, head}) => verifyNonAncestorDiffCheck(repo, base, head),
    status: 1,
    category: 'non-reserved-email',
    forbiddenOutput: [fragment('operator@company.', 'corp')],
  },
  {
    name: 'privacy-synthetic-merge-resolution-only-secret-rejects',
    baseFiles: {'merge-resolution.txt': 'shared base value\n'},
    buildHistory: (repo, base) =>
      buildPrivacyMergeResolutionHistory(repo, base, `${mergeResolutionSecret}\n${mergeResolutionPrivateEmail}\n`),
    verifyHistory: ({repo, base, head}) => verifyMergeAwareGitLog(repo, base, head, mergeResolutionSecret),
    status: 1,
    category: 'non-reserved-email',
    forbiddenOutput: [mergeResolutionSecret, mergeResolutionPrivateEmail],
  },
  {
    name: 'privacy-every-merge-parent-edge-is-scanned',
    baseFiles: {'merge-retention.txt': `${mergeResolutionPrivateEmail}\n`},
    buildHistory: (repo, base) => buildPrivacyMergeRetentionHistory(repo, base, `${mergeResolutionPrivateEmail}\n`),
    verifyHistory: ({repo, base, head, right}) =>
      verifyMergeParentEdgeFixture(repo, base, head, right, mergeResolutionPrivateEmail),
    status: 1,
    category: 'non-reserved-email',
    forbiddenOutput: [mergeResolutionPrivateEmail],
  },
  {
    name: 'privacy-pure-rename-is-explicit-and-safe',
    baseFiles: {'old-safe-name.txt': 'safe content\n'},
    buildHistory: (repo, base) => {
      writeFileSync(path.join(repo, 'new-safe-name.txt'), 'safe content\n');
      rmSync(path.join(repo, 'old-safe-name.txt'));
      return {base, head: commitPrivacyChange(repo, {})};
    },
    status: 0,
  },
  {
    name: 'privacy-rename-with-added-leak-rejects',
    baseFiles: {'old-safe-name.txt': 'safe content\n'},
    buildHistory: (repo, base) => {
      writeFileSync(path.join(repo, 'new-safe-name.txt'), fragment('operator@company.', 'corp'));
      rmSync(path.join(repo, 'old-safe-name.txt'));
      return {base, head: commitPrivacyChange(repo, {})};
    },
    status: 1,
    category: 'non-reserved-email',
    forbiddenOutput: [fragment('operator@company.', 'corp')],
  },
  {
    name: 'privacy-transient-binary-history-fails-closed',
    buildHistory: (repo, base) =>
      buildPrivacyAddDeleteHistory(repo, base, {
        name: 'transient.bin',
        contents: Buffer.from([0, 255, 0, 255, 1, 2, 3]),
      }),
    status: 1,
    category: 'binary-addition',
  },
  {
    name: 'privacy-legacy-deletion-before-base-is-not-rescanned',
    baseFiles: {'legacy.txt': fragment('operator@company.', 'corp')},
    buildHistory: (repo) => {
      const scanBase = commitPrivacyChange(repo, {remove: ['legacy.txt']});
      return {base: scanBase, head: commitPrivacyChange(repo, {files: {'safe.txt': 'safe content\n'}})};
    },
    status: 0,
  },
  {
    name: 'privacy-diff-header-like-added-content-is-scanned',
    files: {'header-like.txt': fragment('+++ b/operator@company.', 'corp')},
    status: 1,
    category: 'non-reserved-email',
    forbiddenOutput: [fragment('operator@company.', 'corp')],
  },
  {
    name: 'privacy-bracket-pathspec-is-literal',
    files: {'[a].txt': fragment('operator@company.', 'corp')},
    status: 1,
    category: 'non-reserved-email',
    expectedLine: 1,
    expectedPath: '[a].txt',
    forbiddenOutput: [fragment('operator@company.', 'corp')],
  },
  {
    name: 'privacy-magic-signature-pathspec-is-literal',
    plumbingEntry: {
      mode: '100644',
      type: 'blob',
      name: ':(literal)privacy.txt',
      contents: fragment('operator@company.', 'corp'),
    },
    status: 1,
    category: 'non-reserved-email',
    expectedLine: 1,
    expectedPath: ':(literal)privacy.txt',
    forbiddenOutput: [fragment('operator@company.', 'corp')],
  },
  {
    name: 'privacy-leading-dash-pathspec-is-literal',
    files: {'-privacy.txt': fragment('operator@company.', 'corp')},
    status: 1,
    category: 'non-reserved-email',
    expectedLine: 1,
    expectedPath: '-privacy.txt',
    forbiddenOutput: [fragment('operator@company.', 'corp')],
  },
  {
    name: 'privacy-safe-symlink-target-passes',
    plumbingEntry: {
      mode: '120000',
      type: 'blob',
      name: 'safe-link',
      contents: 'docs/target.txt',
    },
    status: 0,
  },
  {
    name: 'privacy-unsafe-symlink-target-rejects',
    plumbingEntry: {
      mode: '120000',
      type: 'blob',
      name: 'unsafe-link',
      contents: fragment('/', 'home', '/', 'alice', '/', 'target.txt'),
    },
    status: 1,
    category: 'user-home-path',
    forbiddenOutput: [fragment('/', 'home', '/', 'alice', '/', 'target.txt')],
  },
  {
    name: 'privacy-nul-symlink-target-fails-closed',
    plumbingEntry: {
      mode: '120000',
      type: 'blob',
      name: 'nul-link',
      contents: Buffer.from([100, 111, 99, 115, 0, 116, 97, 114, 103, 101, 116]),
    },
    status: 1,
    expected: 'unsafe symlink target',
  },
  {
    name: 'privacy-invalid-utf8-symlink-target-fails-closed',
    plumbingEntry: {
      mode: '120000',
      type: 'blob',
      name: 'invalid-link',
      contents: Buffer.from([100, 111, 99, 115, 47, 255, 116, 97, 114, 103, 101, 116]),
    },
    status: 1,
    expected: 'invalid UTF-8',
  },
  {
    name: 'privacy-gitlink-addition-fails-closed',
    buildHistory: (repo, base) => ({
      base,
      head: commitPrivacyPlumbingEntry(repo, base, {
        mode: '160000',
        type: 'commit',
        object: base,
        name: 'submodule',
      }),
    }),
    status: 1,
    category: 'gitlink-change',
  },
  {
    name: 'privacy-gitlink-change-fails-closed',
    buildHistory: (repo, base) => {
      const first = commitPrivacyPlumbingEntry(repo, base, {
        mode: '160000',
        type: 'commit',
        object: base,
        name: 'submodule',
      });
      const alternateTarget = privacyCommitCurrentTree(repo, base);
      return {
        base: first,
        head: commitPrivacyPlumbingEntry(repo, first, {
          mode: '160000',
          type: 'commit',
          object: alternateTarget,
          name: 'submodule',
        }),
      };
    },
    status: 1,
    category: 'gitlink-change',
  },
  {
    name: 'privacy-added-binary-fails-closed',
    files: {'artifact.bin': Buffer.from([0, 255, 0, 255, 1, 2, 3])},
    status: 1,
    category: 'binary-addition',
  },
  {
    name: 'privacy-modified-binary-fails-closed',
    baseFiles: {'artifact.bin': Buffer.from([0, 255, 1, 2, 3])},
    files: {'artifact.bin': Buffer.from([0, 255, 4, 5, 6])},
    status: 1,
    category: 'binary-modification',
  },
  {
    name: 'privacy-forced-text-attribute-added-binary-fails-closed',
    files: {
      '.gitattributes': '*.bin diff\n',
      'artifact.bin': Buffer.from([0, 65, 0, 66]),
    },
    status: 1,
    category: 'binary-addition',
  },
  {
    name: 'privacy-forced-text-attribute-modified-binary-fails-closed',
    baseFiles: {
      '.gitattributes': '*.bin diff\n',
      'artifact.bin': Buffer.from([0, 65, 0, 66]),
    },
    files: {'artifact.bin': Buffer.from([0, 67, 0, 68])},
    status: 1,
    category: 'binary-modification',
  },
  {
    name: 'privacy-malformed-base-is-rejected-without-echo',
    files: {'safe.txt': 'safe content'},
    baseOverride: fragment('malformed-', 'base-object'),
    status: 1,
    expected: 'invalid object',
    forbiddenOutput: [fragment('malformed-', 'base-object')],
  },
  {
    name: 'privacy-nonexistent-head-is-rejected-without-echo',
    files: {'safe.txt': 'safe content'},
    headOverride: 'a'.repeat(40),
    status: 1,
    expected: 'repository diff could not be scanned',
    forbiddenOutput: ['a'.repeat(40)],
  },
];

const sensitivePrivacyCategories = new Set(['literal-ipv4', 'non-reserved-email', 'private-scope', 'user-home-path']);
const missingRawDisclosureGuards = privacyCases
  .filter((scenario) => sensitivePrivacyCategories.has(scenario.category))
  .filter(
    (scenario) =>
      !Object.hasOwn(scenario, 'forbiddenOutput') ||
      !Array.isArray(scenario.forbiddenOutput) ||
      scenario.forbiddenOutput.length === 0 ||
      scenario.forbiddenOutput.some((value) => typeof value !== 'string' || value.length === 0)
  )
  .map((scenario) => scenario.name);
if (missingRawDisclosureGuards.length > 0) {
  throw new Error(
    `sensitive privacy fixtures must explicitly forbid their raw match: ${missingRawDisclosureGuards.join(', ')}`
  );
}

const privacyDisclosureFailure = (scenario, output) => {
  const disclosed = (scenario.forbiddenOutput ?? []).some((value) => {
    const jsonEncodedValue = JSON.stringify(value).slice(1, -1);
    return output.includes(value) || output.includes(jsonEncodedValue);
  });
  return disclosed ? `${scenario.name}: output disclosed a matched value, private token, or canary` : null;
};

const disclosureSelfTestScenario = privacyCases.find(
  (scenario) => scenario.name === 'privacy-windows-backslash-home-rejects'
);
if (!disclosureSelfTestScenario) throw new Error('raw disclosure self-test fixture is missing');
const redactedSelfTestOutput = 'privacy finding category=user-home-path path="<redacted>" line=1';
if (privacyDisclosureFailure(disclosureSelfTestScenario, redactedSelfTestOutput)) {
  throw new Error('raw disclosure self-test rejected fully redacted output');
}
const rawSelfTestPath = disclosureSelfTestScenario.forbiddenOutput[0];
const disclosureMutation = `${redactedSelfTestOutput} path="${JSON.stringify(rawSelfTestPath).slice(1, -1)}"`;
if (!privacyDisclosureFailure(disclosureSelfTestScenario, disclosureMutation)) {
  throw new Error('raw disclosure self-test allowed output containing both redacted and raw paths');
}
console.log('PASS raw disclosure mutation is rejected even when redacted output is also present');

for (const scenario of privacyCases) {
  const initial = createPrivacyRepo(scenario.baseFiles);
  const {repo} = initial;
  try {
    const history = scenario.buildHistory
      ? scenario.buildHistory(repo, initial.base)
      : {
          base: initial.base,
          head: scenario.plumbingEntry
            ? commitPrivacyPlumbingEntry(repo, initial.base, scenario.plumbingEntry)
            : commitPrivacyChange(repo, {files: scenario.files, remove: scenario.remove}),
        };
    scenario.verifyHistory?.({repo, ...history});
    const result = runPrivacyScan({
      repo,
      base: scenario.baseOverride ?? history.base,
      head: scenario.headOverride ?? history.head,
      requirePrivate: scenario.requirePrivate,
      privateTokens: scenario.privateTokens,
      privateCanary: scenario.privateCanary,
    });
    const output = `${result.stdout ?? ''}\n${result.stderr ?? ''}`;
    if (result.status !== scenario.status) {
      failures.push(`${scenario.name}: expected exit ${scenario.status}, received ${result.status}: ${output.trim()}`);
      continue;
    }
    if (scenario.category && !output.includes(`category=${scenario.category}`)) {
      failures.push(`${scenario.name}: missing redacted category ${scenario.category}: ${output.trim()}`);
      continue;
    }
    if (scenario.category && !output.includes('line=')) {
      failures.push(`${scenario.name}: missing redacted line number: ${output.trim()}`);
      continue;
    }
    if (scenario.expectedLine !== undefined && !output.includes(`line=${scenario.expectedLine}`)) {
      failures.push(`${scenario.name}: expected new-side line ${scenario.expectedLine}: ${output.trim()}`);
      continue;
    }
    if (scenario.expectedPath !== undefined && !output.includes(`path=${JSON.stringify(scenario.expectedPath)}`)) {
      failures.push(
        `${scenario.name}: expected reported path ${JSON.stringify(scenario.expectedPath)}: ${output.trim()}`
      );
      continue;
    }
    if (scenario.expected && !output.includes(scenario.expected)) {
      failures.push(`${scenario.name}: missing expected message ${scenario.expected}: ${output.trim()}`);
      continue;
    }
    const disclosureFailure = privacyDisclosureFailure(scenario, output);
    if (disclosureFailure) {
      failures.push(disclosureFailure);
      continue;
    }
    console.log(`PASS ${scenario.name}: privacy diff behavior`);
  } finally {
    rmSync(repo, {recursive: true, force: true});
  }
}

if (failures.length > 0) {
  for (const failure of failures) console.error(`FAIL ${failure}`);
  throw new Error(`${failures.length} PR-054A validator negative fixture tests failed`);
}

console.log('PR-054A validator negative fixture tests passed');
