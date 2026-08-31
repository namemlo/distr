import assert from 'node:assert/strict';
import {createHash} from 'node:crypto';
import {test} from 'node:test';

import {
  validateMigrationEvidence,
  validatePostdeployEvidence,
  validateUiEvidence,
} from './pr050-validate-control-plane-evidence.mjs';

const commit = 'a'.repeat(40);
const sha = (value) => `sha256:${createHash('sha256').update(value).digest('hex')}`;
const stableStringify = (value) => {
  if (Array.isArray(value)) return `[${value.map(stableStringify).join(',')}]`;
  if (value && typeof value === 'object') {
    return `{${Object.keys(value)
      .sort()
      .map((key) => `${JSON.stringify(key)}:${stableStringify(value[key])}`)
      .join(',')}}`;
  }
  return JSON.stringify(value);
};

function migrationReport(databaseOverrides = {}) {
  const requiredScenarios = [
    'migration-file-integrity',
    'postgres-runtime-version',
    'migration-138-to-167-upgrade',
    'clean-install',
    'single-step-down-and-refusal-contracts',
    'checkpoint-idempotency-and-cursor-resume',
    'v1-flags-off',
    'mixed-v1-v2',
    'v2-history-flags-off',
    'upstream-compatibility',
  ];
  const report = {
    schemaVersion: 'distr.control-plane-migration-matrix-report/v1',
    status: 'PASS',
    planOnly: false,
    startedAt: '2026-07-29T00:00:00.0000000+00:00',
    completedAt: '2026-07-29T00:01:00.0000000+00:00',
    source: {commit, workingTreeDirty: false},
    range: {from: 138, to: 167},
    database: {
      scheme: 'postgres',
      host: '127.0.0.1',
      port: 5432,
      name: 'control_plane_ci',
      user: 'release_ci',
      passwordPresent: true,
      sslMode: 'disable',
      expectedServerVersion: '16.14',
      observedServerVersion: '16.14',
      ...databaseOverrides,
    },
    migrationFiles: Array.from({length: 30}, (_, index) => ({
      version: 138 + index,
      upFile: `${138 + index}_migration.up.sql`,
      upSha256: sha('b'.repeat(64)),
      downFile: `${138 + index}_migration.down.sql`,
      downSha256: sha('c'.repeat(64)),
    })),
    scenarios: requiredScenarios.map((id) => {
      const output = `${id} complete\n`;
      return {
        id,
        status: 'PASS',
        checks:
          id === 'migration-file-integrity'
            ? [{description: 'exact migration pairs 138 through 167', count: 30, checksum: sha('inventory')}]
            : [
                {
                  description: `${id} executed`,
                  exitCode: 0,
                  durationMs: 1,
                  output,
                  outputSha256: sha(output),
                  diagnostic: output.trim(),
                },
              ],
        diagnostic: '',
      };
    }),
    coverage: {
      schemaUpgrade: {from: 138, to: 167},
      schemaDown: {mode: 'single-step', from: 167, to: 166},
      checkpoint: 'idempotency-and-cursor-resume-tests',
      notExecuted: ['process-interruption-and-restart', 'binary-rollback'],
    },
    integrity: {
      algorithm: 'sha256',
      encoding: 'utf8',
      serialization: 'compact-json-preserving-property-order',
      scope: 'complete-report-excluding-reportChecksum',
      commandEvidence: 'complete-redacted-output',
    },
    cleanup: {attemptedSchemas: 3, droppedSchemas: 3, complete: true},
  };
  report.reportChecksum = sha(JSON.stringify(report));
  return report;
}

function resealMigrationReport(report) {
  delete report.reportChecksum;
  report.reportChecksum = sha(JSON.stringify(report));
  return report;
}

function postdeployReport() {
  const targets = [
    {id: 'target-alpha', hubTargetId: 'hub-alpha', activeRelease: 'A', observerId: 'observer-alpha'},
    {id: 'target-beta', hubTargetId: 'hub-beta', activeRelease: 'A', observerId: 'observer-beta'},
  ];
  const evidence = targets.flatMap((target) =>
    ['A', 'B', 'A'].map((release, index) => ({
      targetId: target.id,
      component: 'api',
      release,
      attempts: 2,
      executedStepKeys:
        release === 'B' ? ['release-b:migration:migration-operator-v2'] : [`release-${release.toLowerCase()}:deploy`],
      observed: {
        id: `${target.id}-${release}-${index}`,
        observerId: target.observerId,
        deploymentUnitId: `${target.id}-unit`,
        componentInstanceId: `${target.id}-instance`,
        componentKey: 'api',
        sourceSequence: index + 1,
        evidenceChecksum: sha(`${target.id}-${release}-${index}`),
        evidenceReference: `fixture://${target.id}/${release}/api`,
        artifactDigest: sha(`artifact-${release}`),
        configChecksum: sha(`config-${release}`),
        capabilityChecksum: sha(`capability-${target.id}`),
        topologyChecksum: sha(`topology-${target.id}`),
        health: 'HEALTHY',
        outcome: 'COMPLETE',
        disposition: 'ACCEPTED',
        trusted: true,
        current: true,
        stateChecksum: sha(`state-${target.id}-${release}-${index}`),
        runtimeStateChecksum: sha(`runtime-${target.id}-${release}-${index}`),
      },
    }))
  );
  const report = {
    ok: true,
    proofMode: 'live-hub-api',
    targets,
    releaseHistory: ['A', 'B', 'A'],
    migration: {
      id: 'migration-operator-v2',
      appliedCount: 2,
      attempts: targets.map((target) => ({
        targetId: target.id,
        stepKey: 'release-b:migration:migration-operator-v2',
        result: 'SUCCEEDED_VIA_V2',
      })),
    },
    evidence,
    fleet: {
      items: targets.map((target) => ({
        deploymentTargetId: target.hubTargetId,
        activeRelease: '1.0.0',
      })),
    },
    flowChecksum: '',
    secretLeaks: 0,
    liveStack: {
      started: true,
      loopbackOnly: true,
      services: ['postgres', 'hub', 'external-executor', 'reference-executor', 'observer-alpha', 'observer-beta'],
      nonLocalCalls: 0,
    },
    cleanup: {completed: true, retainedResources: [], inspectionFailures: []},
  };
  report.flowChecksum = sha(
    stableStringify({
      releaseHistory: report.releaseHistory,
      evidence: report.evidence,
      fleet: report.fleet,
    })
  );
  return report;
}

test('migration evidence requires an intact 138-167 executable matrix from a clean source and disposable database', () => {
  assert.doesNotThrow(() => validateMigrationEvidence(migrationReport(), commit, '16.14'));

  for (const mutate of [
    (report) => (report.source.workingTreeDirty = true),
    (report) => (report.range.to = 163),
    (report) => report.scenarios.pop(),
    (report) => (report.scenarios[0].status = 'PLANNED'),
    (report) => (report.database.host = 'db.example.invalid'),
    (report) => (report.database.observedServerVersion = '18.4'),
    (report) => (report.integrity.scope = 'partial-report'),
    (report) => (report.coverage.notExecuted = []),
    (report) => (report.scenarios[1].checks[0].output = 'tampered output'),
    (report) => (report.cleanup.droppedSchemas = 2),
    (report) => (report.migrationFiles[0].upSha256 = 'bad'),
  ]) {
    const report = migrationReport();
    mutate(report);
    assert.throws(() => validateMigrationEvidence(report, commit, '16.14'));
  }

  const checksumTamper = migrationReport();
  checksumTamper.status = 'FAIL';
  assert.throws(() => validateMigrationEvidence(checksumTamper, commit, '16.14'), /checksum/i);

  const bareMigrationFileChecksum = migrationReport();
  bareMigrationFileChecksum.migrationFiles[0].upSha256 = 'b'.repeat(64);
  resealMigrationReport(bareMigrationFileChecksum);
  assert.throws(
    () => validateMigrationEvidence(bareMigrationFileChecksum, commit, '16.14'),
    /lowercase SHA-256/
  );
});

test('migration evidence permits passwordless access only for an explicit loopback test database identity', () => {
  assert.doesNotThrow(() =>
    validateMigrationEvidence(migrationReport({passwordPresent: false}), commit, '16.14')
  );

  for (const databaseOverrides of [
    {host: 'db.example.invalid', passwordPresent: false},
    {name: 'production', passwordPresent: false},
    {user: 'production_owner', passwordPresent: false},
    {passwordPresent: false, sslMode: 'require'},
    {passwordPresent: 'false'},
    {passwordPresent: null},
    {passwordPresent: undefined},
  ]) {
    assert.throws(
      () => validateMigrationEvidence(migrationReport(databaseOverrides), commit, '16.14'),
      /explicit loopback test database/
    );
  }
});

test('post-deploy evidence proves live operator, API, observer, audit, and reconciliation facts', () => {
  assert.doesNotThrow(() => validatePostdeployEvidence(postdeployReport()));

  for (const mutate of [
    (report) => (report.proofMode = 'fixture-contract'),
    (report) => (report.targets[1].observerId = report.targets[0].observerId),
    (report) => (report.releaseHistory = ['A', 'B']),
    (report) => (report.migration.attempts[0].result = 'FAILED'),
    (report) => (report.evidence[0].observed.trusted = false),
    (report) => (report.evidence[0].observed.evidenceChecksum = ''),
    (report) => (report.fleet.items = []),
    (report) => (report.liveStack.services = ['postgres', 'hub']),
    (report) => (report.cleanup.retainedResources = ['container']),
  ]) {
    const report = postdeployReport();
    mutate(report);
    assert.throws(() => validatePostdeployEvidence(report));
  }
});

test('UI evidence requires at least one passing non-flaky browser assertion', () => {
  assert.doesNotThrow(() =>
    validateUiEvidence({
      schemaVersion: 'distr.control-plane-ui-report/v1',
      status: 'PASS',
      stats: {expected: 7, unexpected: 0, flaky: 0, skipped: 0},
    })
  );
  assert.throws(() =>
    validateUiEvidence({
      schemaVersion: 'distr.control-plane-ui-report/v1',
      status: 'PASS',
      stats: {expected: 7, unexpected: 1, flaky: 0, skipped: 0},
    })
  );
});
