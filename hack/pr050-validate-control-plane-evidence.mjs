#!/usr/bin/env node

import {createHash} from 'node:crypto';
import {readFileSync} from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

const checksumPattern = /^sha256:[0-9a-f]{64}$/;
const commitPattern = /^[0-9a-f]{40}$/;
const safeDatabaseNamePattern = /(test|ci|fixture|sandbox|control[_-]?plane)/i;
const requiredMigrationScenarios = Object.freeze([
  'migration-file-integrity',
  'postgres-runtime-version',
  'migration-138-to-166-upgrade',
  'clean-install',
  'single-step-down-and-refusal-contracts',
  'checkpoint-idempotency-and-cursor-resume',
  'v1-flags-off',
  'mixed-v1-v2',
  'v2-history-flags-off',
  'upstream-compatibility',
]);
const requiredLiveServices = Object.freeze([
  'postgres',
  'hub',
  'external-executor',
  'reference-executor',
  'observer-alpha',
  'observer-beta',
]);

function fail(message) {
  throw new Error(message);
}

function nonEmptyString(value, name) {
  if (typeof value !== 'string' || value.trim() === '') {
    fail(`${name} must be a non-empty string`);
  }
  return value;
}

function stableStringify(value) {
  if (Array.isArray(value)) {
    return `[${value.map(stableStringify).join(',')}]`;
  }
  if (value && typeof value === 'object') {
    return `{${Object.keys(value)
      .sort()
      .map((key) => `${JSON.stringify(key)}:${stableStringify(value[key])}`)
      .join(',')}}`;
  }
  return JSON.stringify(value);
}

function checksumText(text) {
  return `sha256:${createHash('sha256').update(text).digest('hex')}`;
}

function requireExactMembers(values, required, name) {
  if (!Array.isArray(values)) {
    fail(`${name} must be an array`);
  }
  const actual = new Set(values);
  if (actual.size !== values.length || actual.size !== required.length || required.some((value) => !actual.has(value))) {
    fail(`${name} must contain the exact required values`);
  }
}

function validateMigrationFile(file, expectedVersion, index) {
  if (file?.version !== expectedVersion) {
    fail(`migrationFiles[${index}].version must be ${expectedVersion}`);
  }
  for (const side of ['up', 'down']) {
    nonEmptyString(file?.[`${side}File`], `migrationFiles[${index}].${side}File`);
    if (!checksumPattern.test(file?.[`${side}Sha256`] ?? '')) {
      fail(`migrationFiles[${index}].${side}Sha256 must be a lowercase SHA-256`);
    }
  }
}

export function validateMigrationEvidence(report, expectedCommit, expectedPostgresVersion) {
  if (!report || typeof report !== 'object' || Array.isArray(report)) {
    fail('migration report must be an object');
  }
  if (!checksumPattern.test(report.reportChecksum ?? '')) {
    fail('migration report checksum is missing or malformed');
  }
  const withoutChecksum = {...report};
  delete withoutChecksum.reportChecksum;
  if (checksumText(JSON.stringify(withoutChecksum)) !== report.reportChecksum) {
    fail('migration report checksum does not match its content');
  }
  if (
    report.schemaVersion !== 'distr.control-plane-migration-matrix-report/v1' ||
    report.status !== 'PASS' ||
    report.planOnly !== false
  ) {
    fail('migration report must be a passing non-plan v1 execution');
  }
  if (!commitPattern.test(expectedCommit ?? '') || report.source?.commit !== expectedCommit) {
    fail('migration report source commit does not match the requested release');
  }
  if (report.source?.workingTreeDirty !== false) {
    fail('migration report source must be a clean tracked worktree');
  }
  if (report.range?.from !== 138 || report.range?.to !== 166) {
    fail('migration report must cover the exact 138 through 166 range');
  }
  if (
    !['16.14', '18.4'].includes(expectedPostgresVersion) ||
    report.database?.expectedServerVersion !== expectedPostgresVersion ||
    report.database?.observedServerVersion !== expectedPostgresVersion
  ) {
    fail('migration report PostgreSQL runtime does not match its expected matrix version');
  }
  if (
    !['postgres', 'postgresql'].includes(report.database?.scheme) ||
    !['localhost', '127.0.0.1', '::1'].includes(report.database?.host) ||
    !Number.isInteger(report.database?.port) ||
    report.database.port < 1 ||
    report.database.port > 65535 ||
    !safeDatabaseNamePattern.test(report.database?.name ?? '') ||
    typeof report.database?.user !== 'string' ||
    !safeDatabaseNamePattern.test(report.database.user) ||
    typeof report.database?.passwordPresent !== 'boolean' ||
    report.database?.sslMode !== 'disable'
  ) {
    fail('migration report database identity must be an explicit loopback test database');
  }
  if (!Array.isArray(report.migrationFiles) || report.migrationFiles.length !== 29) {
    fail('migration report must contain exactly 29 migration file pairs');
  }
  report.migrationFiles.forEach((file, index) => validateMigrationFile(file, 138 + index, index));
  if (
    report.coverage?.schemaUpgrade?.from !== 138 ||
    report.coverage?.schemaUpgrade?.to !== 166 ||
    report.coverage?.schemaDown?.mode !== 'single-step' ||
    report.coverage?.schemaDown?.from !== 166 ||
    report.coverage?.schemaDown?.to !== 165 ||
    report.coverage?.checkpoint !== 'idempotency-and-cursor-resume-tests' ||
    JSON.stringify(report.coverage?.notExecuted) !==
      JSON.stringify(['process-interruption-and-restart', 'binary-rollback'])
  ) {
    fail('migration report coverage disclosure is missing or inaccurate');
  }
  if (
    report.integrity?.algorithm !== 'sha256' ||
    report.integrity?.encoding !== 'utf8' ||
    report.integrity?.serialization !== 'compact-json-preserving-property-order' ||
    report.integrity?.scope !== 'complete-report-excluding-reportChecksum' ||
    report.integrity?.commandEvidence !== 'complete-redacted-output'
  ) {
    fail('migration report integrity contract is missing or inaccurate');
  }
  requireExactMembers(
    report.scenarios?.map((scenario) => scenario?.id),
    requiredMigrationScenarios,
    'migration scenario IDs'
  );
  for (const scenario of report.scenarios) {
    if (scenario.status !== 'PASS' || !Array.isArray(scenario.checks) || scenario.checks.length === 0) {
      fail(`migration scenario ${scenario.id} must contain passing executable checks`);
    }
    if (typeof scenario.diagnostic !== 'string' || scenario.diagnostic.length > 4107) {
      fail(`migration scenario ${scenario.id} must contain a bounded diagnostic`);
    }
    if (scenario.id !== 'migration-file-integrity') {
      for (const [index, check] of scenario.checks.entries()) {
        if (
          check?.exitCode !== 0 ||
          typeof check?.output !== 'string' ||
          typeof check?.diagnostic !== 'string' ||
          check.diagnostic.length > 4107 ||
          check.outputSha256 !== checksumText(check.output)
        ) {
          fail(`migration scenario ${scenario.id} check ${index} lacks complete redacted command evidence`);
        }
      }
    }
  }
  if (
    report.cleanup?.complete !== true ||
    !Number.isInteger(report.cleanup?.attemptedSchemas) ||
    report.cleanup.attemptedSchemas < 1 ||
    report.cleanup.droppedSchemas !== report.cleanup.attemptedSchemas
  ) {
    fail('migration report must prove complete isolated-schema cleanup');
  }
  return report;
}

function validateObservedEvidence(item, index) {
  nonEmptyString(item?.targetId, `evidence[${index}].targetId`);
  nonEmptyString(item?.component, `evidence[${index}].component`);
  if (!['A', 'B'].includes(item?.release) || !Number.isInteger(item?.attempts) || item.attempts < 1) {
    fail(`evidence[${index}] must identify an executed A or B release`);
  }
  if (!Array.isArray(item.executedStepKeys) || item.executedStepKeys.length < 1) {
    fail(`evidence[${index}] must contain executed step keys`);
  }
  const observed = item.observed;
  for (const field of [
    'id',
    'observerId',
    'deploymentUnitId',
    'componentInstanceId',
    'componentKey',
    'evidenceReference',
  ]) {
    nonEmptyString(observed?.[field], `evidence[${index}].observed.${field}`);
  }
  for (const field of [
    'evidenceChecksum',
    'artifactDigest',
    'configChecksum',
    'capabilityChecksum',
    'topologyChecksum',
    'stateChecksum',
    'runtimeStateChecksum',
  ]) {
    if (!checksumPattern.test(observed?.[field] ?? '')) {
      fail(`evidence[${index}].observed.${field} must be a SHA-256 reference`);
    }
  }
  if (
    !Number.isInteger(observed.sourceSequence) ||
    observed.sourceSequence < 1 ||
    observed.health !== 'HEALTHY' ||
    observed.outcome !== 'COMPLETE' ||
    observed.disposition !== 'ACCEPTED' ||
    observed.trusted !== true ||
    observed.current !== true
  ) {
    fail(`evidence[${index}] must contain accepted, trusted, current observer facts`);
  }
}

export function validatePostdeployEvidence(report) {
  if (
    report?.ok !== true ||
    report.proofMode !== 'live-hub-api' ||
    report.liveStack?.started !== true ||
    report.liveStack?.blocker != null ||
    report.liveStack?.loopbackOnly !== true ||
    report.liveStack?.nonLocalCalls !== 0
  ) {
    fail('post-deploy report must prove the live loopback Hub API journey');
  }
  requireExactMembers(report.liveStack.services, requiredLiveServices, 'liveStack.services');
  if (
    report.cleanup?.completed !== true ||
    !Array.isArray(report.cleanup?.retainedResources) ||
    report.cleanup.retainedResources.length !== 0 ||
    !Array.isArray(report.cleanup?.inspectionFailures) ||
    report.cleanup.inspectionFailures.length !== 0
  ) {
    fail('post-deploy report must prove complete live-stack cleanup');
  }
  if (report.secretLeaks !== 0 || !checksumPattern.test(report.flowChecksum ?? '')) {
    fail('post-deploy report must contain secret-safe checksummed audit evidence');
  }
  if (!Array.isArray(report.targets) || report.targets.length < 2) {
    fail('post-deploy report must contain at least two live operator targets');
  }
  const targetIds = report.targets.map((target) => nonEmptyString(target?.id, 'target.id'));
  const hubTargetIds = report.targets.map((target) => nonEmptyString(target?.hubTargetId, 'target.hubTargetId'));
  const observerIds = report.targets.map((target) => nonEmptyString(target?.observerId, 'target.observerId'));
  if (
    new Set(targetIds).size !== targetIds.length ||
    new Set(hubTargetIds).size !== hubTargetIds.length ||
    new Set(observerIds).size !== observerIds.length ||
    report.targets.some((target) => target.activeRelease !== 'A')
  ) {
    fail('post-deploy targets must have independent identities and return to release A');
  }
  if (JSON.stringify(report.releaseHistory) !== JSON.stringify(['A', 'B', 'A'])) {
    fail('post-deploy report must prove the A to B to A reconciliation history');
  }
  if (
    !Array.isArray(report.migration?.attempts) ||
    report.migration.attempts.length < report.targets.length ||
    report.migration.appliedCount !== report.migration.attempts.length ||
    report.migration.attempts.some(
      (attempt) =>
        !targetIds.includes(attempt?.targetId) ||
        typeof attempt?.stepKey !== 'string' ||
        !attempt.stepKey.includes(report.migration?.id ?? '\0') ||
        attempt.result !== 'SUCCEEDED_VIA_V2'
    )
  ) {
    fail('post-deploy report must prove per-target v2 migration execution');
  }
  if (!Array.isArray(report.evidence) || report.evidence.length < report.targets.length * 3) {
    fail('post-deploy report must contain retained observer evidence for A, B, and previous-state A');
  }
  report.evidence.forEach(validateObservedEvidence);
  for (const targetId of targetIds) {
    const releases = report.evidence.filter((item) => item.targetId === targetId).map((item) => item.release);
    if (!releases.includes('B') || releases.filter((release) => release === 'A').length < 2) {
      fail(`post-deploy observer evidence for ${targetId} must cover A, B, and previous-state A`);
    }
  }
  const observerRegistrationIds = report.evidence.map((item) => item.observed.observerId);
  if (new Set(observerRegistrationIds).size < report.targets.length) {
    fail('post-deploy report must prove independent observer registrations');
  }
  if (
    !Array.isArray(report.fleet?.items) ||
    hubTargetIds.some(
      (hubTargetId) =>
        !report.fleet.items.some(
          (row) =>
            row?.deploymentTargetId === hubTargetId &&
            typeof row?.activeRelease === 'string' &&
            row.activeRelease.length > 0
        )
    )
  ) {
    fail('post-deploy fleet read model must contain every reconciled target');
  }
  const expectedFlowChecksum = checksumText(
    stableStringify({
      releaseHistory: report.releaseHistory,
      evidence: report.evidence,
      fleet: report.fleet,
    })
  );
  if (report.flowChecksum !== expectedFlowChecksum) {
    fail('post-deploy flow checksum does not match retained audit facts');
  }
  return report;
}

export function validateUiEvidence(report) {
  if (
    report?.schemaVersion !== 'distr.control-plane-ui-report/v1' ||
    report.status !== 'PASS' ||
    !Number.isInteger(report.stats?.expected) ||
    report.stats.expected < 1 ||
    report.stats.unexpected !== 0 ||
    report.stats.flaky !== 0 ||
    !Number.isInteger(report.stats.skipped) ||
    report.stats.skipped < 0
  ) {
    fail('UI report must contain passing non-flaky browser evidence');
  }
  return report;
}

function readJson(file) {
  return JSON.parse(readFileSync(file, 'utf8'));
}

function usage() {
  fail(
    'usage: node hack/pr050-validate-control-plane-evidence.mjs migration <report> <commit> <postgres-version> | postdeploy <report> | ui <report>'
  );
}

function main(argv) {
  const [command, reportPath, expectedCommit, expectedPostgresVersion, ...extra] = argv;
  if (!command || !reportPath || extra.length > 0) usage();
  const report = readJson(reportPath);
  if (
    command === 'migration' &&
    expectedCommit &&
    commitPattern.test(expectedCommit) &&
    ['16.14', '18.4'].includes(expectedPostgresVersion)
  ) {
    validateMigrationEvidence(report, expectedCommit, expectedPostgresVersion);
  } else if (command === 'postdeploy' && expectedCommit === undefined && expectedPostgresVersion === undefined) {
    validatePostdeployEvidence(report);
  } else if (command === 'ui' && expectedCommit === undefined && expectedPostgresVersion === undefined) {
    validateUiEvidence(report);
  } else {
    usage();
  }
  process.stdout.write(`Validated ${command} evidence: ${path.basename(reportPath)}\n`);
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    main(process.argv.slice(2));
  } catch (error) {
    process.stderr.write(`${error.message}\n`);
    process.exitCode = 1;
  }
}
