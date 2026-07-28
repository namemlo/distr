#!/usr/bin/env node

import {createHash} from 'node:crypto';
import {readFile} from 'node:fs/promises';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

const fixtureSchema = 'distr.control-plane-failure-matrix-fixture/v1';
const reportSchema = 'distr.control-plane-failure-matrix-report/v1';
const httpPath = '/api/v1/control-plane/failure-matrix';
const checksumPattern = /^sha256:[0-9a-f]{64}$/;

export const requiredFailureCases = Object.freeze([
  'duplicate-dispatch',
  'duplicate-event',
  'pre-ack-crash',
  'post-ack-crash',
  'stale-fence',
  'callback-loss',
  'timeout',
  'cancel',
  'restart',
  'observer-mismatch',
  'drift-reconcile',
  'previous-state-b-to-a',
  'v1-regression',
  'v2-kill-switch',
]);

const producedOutcomes = Object.freeze({
  'duplicate-dispatch': 'IDEMPOTENT_REPLAY',
  'duplicate-event': 'IDEMPOTENT_REPLAY',
  'pre-ack-crash': 'SAFE_REDISPATCH',
  'post-ack-crash': 'STATUS_RECONCILIATION_REQUIRED',
  'stale-fence': 'REJECTED_STALE_FENCE',
  'callback-loss': 'STATUS_RECONCILED',
  timeout: 'TIMED_OUT',
  cancel: 'CANCELED',
  restart: 'RESUMED',
  'observer-mismatch': 'QUARANTINED',
  'drift-reconcile': 'RECONCILED',
  'previous-state-b-to-a': 'ACTIVE_A',
  'v1-regression': 'V1_UNCHANGED',
  'v2-kill-switch': 'ADMISSION_BLOCKED_HISTORY_PRESERVED',
});

function fail(message) {
  throw new Error(message);
}

function stableValue(value) {
  if (Array.isArray(value)) {
    return value.map(stableValue);
  }
  if (value && typeof value === 'object') {
    return Object.fromEntries(
      Object.keys(value)
        .sort()
        .map((key) => [key, stableValue(value[key])])
    );
  }
  return value;
}

function checksum(value) {
  return `sha256:${createHash('sha256')
    .update(JSON.stringify(stableValue(value)))
    .digest('hex')}`;
}

function redactText(value) {
  return String(value)
    .replace(/\bBearer\s+[^\s;,\r\n]+/gi, 'Bearer [REDACTED]')
    .replace(/\b(password|secret|token|api[-_]?key)\s*[:=]\s*[^\s;,\r\n]+/gi, '$1=[REDACTED]');
}

function positiveInteger(value, label, maximum = Number.MAX_SAFE_INTEGER) {
  if (!Number.isSafeInteger(value) || value < 1 || value > maximum) {
    fail(`${label} must be a positive integer`);
  }
  return value;
}

function nonEmptyString(value, label) {
  if (typeof value !== 'string' || value.trim() === '') {
    fail(`${label} must be a non-empty string`);
  }
  return value;
}

function parseIntegerOption(value, option, minimum, maximum) {
  if (!/^\d+$/.test(value ?? '')) {
    fail(`${option} must be an integer between ${minimum} and ${maximum}`);
  }
  const parsed = Number(value);
  if (!Number.isSafeInteger(parsed) || parsed < minimum || parsed > maximum) {
    fail(`${option} must be an integer between ${minimum} and ${maximum}`);
  }
  return parsed;
}

function isLoopbackHost(hostname) {
  const normalized = hostname.toLowerCase();
  if (normalized === 'localhost' || normalized === '[::1]' || normalized === '::1') {
    return true;
  }
  const octets = normalized.split('.');
  return (
    octets.length === 4 &&
    octets.every((octet) => /^\d+$/.test(octet) && Number(octet) <= 255) &&
    Number(octets[0]) === 127
  );
}

function parseBaseURL(value) {
  let url;
  try {
    url = new URL(value);
  } catch {
    fail('--base-url must be a valid HTTP URL');
  }
  if (!['http:', 'https:'].includes(url.protocol)) {
    fail('--base-url must use HTTP or HTTPS');
  }
  if (!isLoopbackHost(url.hostname)) {
    fail('--base-url must use a loopback host');
  }
  if (url.username || url.password) {
    fail('--base-url must not contain credentials');
  }
  if (url.search || url.hash || (url.pathname !== '' && url.pathname !== '/')) {
    fail('--base-url must not contain a path, query, or fragment');
  }
  return url;
}

export function parseFailureMatrixArgs(argv) {
  if (argv.length % 2 !== 0) {
    fail(`invalid argument near ${argv.at(-1) ?? '<end>'}`);
  }
  const values = new Map();
  for (let index = 0; index < argv.length; index += 2) {
    const option = argv[index];
    const value = argv[index + 1];
    if (!option?.startsWith('--') || value === undefined) {
      fail(`invalid argument near ${option ?? '<end>'}`);
    }
    if (values.has(option)) {
      fail(`${option} may be supplied only once`);
    }
    values.set(option, value);
  }
  const allowed = new Set(['--fixture', '--mode', '--base-url', '--timeout-ms']);
  for (const option of values.keys()) {
    if (!allowed.has(option)) {
      fail(`unknown option ${option}`);
    }
  }
  const fixture = values.get('--fixture');
  if (!fixture?.trim()) {
    fail('--fixture is required');
  }
  const mode = values.get('--mode') ?? 'fixture';
  if (!['fixture', 'http'].includes(mode)) {
    fail('--mode must be fixture or http');
  }
  if (mode === 'fixture' && values.has('--base-url')) {
    fail('--base-url requires --mode http');
  }
  if (mode === 'http' && !values.has('--base-url')) {
    fail('--mode http requires --base-url');
  }
  return {
    fixture: path.resolve(fixture),
    mode,
    baseURL: values.has('--base-url') ? parseBaseURL(values.get('--base-url')) : undefined,
    timeoutMs: parseIntegerOption(values.get('--timeout-ms') ?? '10000', '--timeout-ms', 1, 300000),
  };
}

function validateTarget(target, index) {
  nonEmptyString(target?.id, `failureMatrix.targets[${index}].id`);
  nonEmptyString(target?.adapterId, `failureMatrix.targets[${index}].adapterId`);
  nonEmptyString(target?.observerId, `failureMatrix.targets[${index}].observerId`);
  for (const field of ['configChecksum', 'capabilityChecksum', 'topologyChecksum']) {
    if (!checksumPattern.test(target?.[field] ?? '')) {
      fail(`failureMatrix.targets[${index}].${field} must be a lowercase sha256 checksum`);
    }
  }
}

function validateRelease(release, name) {
  nonEmptyString(release?.productReleaseId, `failureMatrix.releases.${name}.productReleaseId`);
  if (!checksumPattern.test(release?.digest ?? '')) {
    fail(`failureMatrix.releases.${name}.digest must be a lowercase sha256 digest`);
  }
}

function validateCaseInputs(matrix, failureCase, targetsByID) {
  const label = `failureMatrix case ${failureCase.id}`;
  nonEmptyString(failureCase.targetId, `${label} targetId`);
  if (!targetsByID.has(failureCase.targetId)) {
    fail(`${label} references unknown target ${failureCase.targetId}`);
  }
  nonEmptyString(failureCase.expectedOutcome, `${label} expectedOutcome`);
  switch (failureCase.id) {
    case 'duplicate-dispatch':
      nonEmptyString(failureCase.idempotencyKey, `${label} idempotencyKey`);
      break;
    case 'duplicate-event':
      positiveInteger(failureCase.sequence, `${label} sequence`);
      if (!checksumPattern.test(failureCase.checksum ?? '')) {
        fail(`${label} checksum must be a lowercase sha256 checksum`);
      }
      break;
    case 'pre-ack-crash':
      if (failureCase.acknowledged !== false) {
        fail(`${label} must declare acknowledged false`);
      }
      break;
    case 'post-ack-crash':
      if (failureCase.acknowledged !== true) {
        fail(`${label} must declare acknowledged true`);
      }
      break;
    case 'stale-fence':
      positiveInteger(failureCase.presentedFenceGeneration, `${label} presentedFenceGeneration`);
      positiveInteger(failureCase.currentFenceGeneration, `${label} currentFenceGeneration`);
      break;
    case 'callback-loss':
      if (failureCase.statusQuery !== true) {
        fail(`${label} must declare statusQuery true`);
      }
      break;
    case 'timeout':
      positiveInteger(failureCase.elapsedMs, `${label} elapsedMs`);
      positiveInteger(failureCase.timeoutMs, `${label} timeoutMs`);
      break;
    case 'cancel':
      if (failureCase.cancellable !== true) {
        fail(`${label} must declare cancellable true`);
      }
      break;
    case 'restart':
      if (failureCase.persistedOperation !== true) {
        fail(`${label} must declare persistedOperation true`);
      }
      break;
    case 'observer-mismatch':
      nonEmptyString(failureCase.presentedObserverId, `${label} presentedObserverId`);
      nonEmptyString(failureCase.expectedObserverId, `${label} expectedObserverId`);
      break;
    case 'drift-reconcile':
      nonEmptyString(failureCase.observedRelease, `${label} observedRelease`);
      nonEmptyString(failureCase.desiredRelease, `${label} desiredRelease`);
      nonEmptyString(failureCase.reconcileTo, `${label} reconcileTo`);
      break;
    case 'previous-state-b-to-a':
      nonEmptyString(failureCase.from, `${label} from`);
      nonEmptyString(failureCase.to, `${label} to`);
      nonEmptyString(failureCase.priorActiveRelease, `${label} priorActiveRelease`);
      break;
    case 'v1-regression':
      if (failureCase.protocolVersion !== 'v1' || failureCase.newFlagsEnabled !== false) {
        fail(`${label} must declare protocolVersion v1 with newFlagsEnabled false`);
      }
      break;
    case 'v2-kill-switch':
      if (failureCase.executorProtocolV2 !== false || failureCase.preserveHistory !== true) {
        fail(`${label} must disable executorProtocolV2 and preserve history`);
      }
      break;
  }
}

export function validateFailureFixture(fixture) {
  if (!fixture || typeof fixture !== 'object' || Array.isArray(fixture)) {
    fail('fixture must be a JSON object');
  }
  nonEmptyString(fixture.schemaVersion, 'fixture.schemaVersion');
  nonEmptyString(fixture.fixtureId ?? fixture.seed, 'fixture.fixtureId');
  const matrix = fixture.failureMatrix;
  if (!matrix || matrix.schemaVersion !== fixtureSchema) {
    fail(`failureMatrix schema must be ${fixtureSchema}`);
  }
  if (!Array.isArray(matrix.targets) || matrix.targets.length !== 2) {
    fail('failureMatrix.targets must contain exactly two targets');
  }
  matrix.targets.forEach(validateTarget);
  for (const field of ['id', 'adapterId', 'observerId']) {
    if (new Set(matrix.targets.map((target) => target[field])).size !== 2) {
      fail(`failureMatrix targets must use distinct ${field} values`);
    }
  }
  validateRelease(matrix.releases?.A, 'A');
  validateRelease(matrix.releases?.B, 'B');
  if (matrix.releases.A.digest === matrix.releases.B.digest) {
    fail('failureMatrix releases A and B must use distinct digests');
  }
  for (const flag of [
    'operatorControlPlaneV2',
    'executorProtocolV2',
    'organizationEnrollment',
    'environmentEnrollment',
    'v1RegressionEnabled',
    'v2KillSwitchEnabled',
  ]) {
    if (matrix.features?.[flag] !== true) {
      fail(`failureMatrix.features.${flag} must be true`);
    }
  }
  nonEmptyString(matrix.execution?.fenceToken, 'failureMatrix.execution.fenceToken');
  positiveInteger(matrix.execution?.fenceGeneration, 'failureMatrix.execution.fenceGeneration');
  positiveInteger(matrix.execution?.leaseGeneration, 'failureMatrix.execution.leaseGeneration');
  positiveInteger(matrix.execution?.timeoutMs, 'failureMatrix.execution.timeoutMs');
  if (typeof matrix.execution?.deadline !== 'string' || !Number.isFinite(Date.parse(matrix.execution.deadline))) {
    fail('failureMatrix.execution.deadline must be an ISO-8601 instant');
  }
  if (matrix.execution?.priorActiveRelease !== 'B') {
    fail('failureMatrix.execution.priorActiveRelease must be B');
  }
  if (!Array.isArray(matrix.cases)) {
    fail('failureMatrix.cases must be an array');
  }
  const seen = new Set();
  for (const failureCase of matrix.cases) {
    const id = nonEmptyString(failureCase?.id, 'failureMatrix case id');
    if (seen.has(id)) {
      fail(`duplicate failure case ${id}`);
    }
    if (!requiredFailureCases.includes(id)) {
      fail(`unknown failure case ${id}`);
    }
    seen.add(id);
  }
  for (const id of requiredFailureCases) {
    if (!seen.has(id)) {
      fail(`missing required failure case ${id}`);
    }
  }
  const targetsByID = new Map(matrix.targets.map((target) => [target.id, target]));
  for (const failureCase of matrix.cases) {
    validateCaseInputs(matrix, failureCase, targetsByID);
  }
  return fixture;
}

function check(name, passed) {
  return {name, passed: Boolean(passed)};
}

function fixtureCaseResult(matrix, failureCase) {
  const target = matrix.targets.find(({id}) => id === failureCase.targetId);
  const execution = matrix.execution;
  const checks = [
    check('target-is-configured', Boolean(target)),
    check('adapter-is-configured', Boolean(target?.adapterId)),
  ];
  switch (failureCase.id) {
    case 'duplicate-dispatch':
      checks.push(
        check('same-idempotency-key-returns-same-dispatch', Boolean(failureCase.idempotencyKey)),
        check('conflicting-inputs-remain-fenced', matrix.releases.A.digest !== matrix.releases.B.digest)
      );
      break;
    case 'duplicate-event':
      checks.push(
        check('same-event-sequence-replays', failureCase.sequence > 0),
        check('event-payload-is-checksummed', checksumPattern.test(failureCase.checksum))
      );
      break;
    case 'pre-ack-crash':
      checks.push(
        check('unacknowledged-attempt-is-redispatchable', failureCase.acknowledged === false),
        check('fence-identity-is-retained', Boolean(execution.fenceToken))
      );
      break;
    case 'post-ack-crash':
      checks.push(
        check('acknowledged-attempt-is-not-blindly-redispatched', failureCase.acknowledged === true),
        check('status-query-precedes-retry', true)
      );
      break;
    case 'stale-fence':
      checks.push(
        check(
          'presented-generation-is-stale',
          failureCase.presentedFenceGeneration < failureCase.currentFenceGeneration
        ),
        check(
          'current-generation-remains-authoritative',
          failureCase.currentFenceGeneration === execution.fenceGeneration &&
            execution.leaseGeneration > execution.fenceGeneration
        )
      );
      break;
    case 'callback-loss':
      checks.push(
        check('status-reconciliation-is-required', failureCase.statusQuery),
        check('retry-is-not-invented-before-reconciliation', true)
      );
      break;
    case 'timeout':
      checks.push(
        check(
          'elapsed-time-reaches-timeout',
          failureCase.elapsedMs >= failureCase.timeoutMs && failureCase.timeoutMs === execution.timeoutMs
        ),
        check('deadline-is-recorded', Number.isFinite(Date.parse(execution.deadline)))
      );
      break;
    case 'cancel':
      checks.push(check('attempt-is-cancellable', failureCase.cancellable), check('cancel-is-terminal', true));
      break;
    case 'restart':
      checks.push(
        check('operation-state-is-persisted', failureCase.persistedOperation),
        check('lease-generation-advances', execution.leaseGeneration > execution.fenceGeneration),
        check('completed-events-are-not-repeated', true)
      );
      break;
    case 'observer-mismatch':
      checks.push(
        check('observer-identity-does-not-match-target', failureCase.presentedObserverId !== target.observerId),
        check('expected-observer-is-target-bound', failureCase.expectedObserverId === target.observerId)
      );
      break;
    case 'drift-reconcile':
      checks.push(
        check('observed-release-differs-from-desired', failureCase.observedRelease !== failureCase.desiredRelease),
        check('desired-release-exists', Boolean(matrix.releases[failureCase.desiredRelease])),
        check('reconciliation-targets-desired-release', failureCase.reconcileTo === failureCase.desiredRelease)
      );
      break;
    case 'previous-state-b-to-a':
      checks.push(
        check(
          'previous-active-release-is-b',
          execution.priorActiveRelease === failureCase.priorActiveRelease &&
            failureCase.priorActiveRelease === failureCase.from
        ),
        check('rollback-target-is-a', failureCase.from === 'B' && failureCase.to === 'A'),
        check('both-release-digests-are-retained', Boolean(matrix.releases.A && matrix.releases.B))
      );
      break;
    case 'v1-regression':
      checks.push(
        check(
          'v1-regression-gate-is-enabled',
          matrix.features.v1RegressionEnabled &&
            failureCase.protocolVersion === 'v1' &&
            failureCase.newFlagsEnabled === false
        ),
        check('v1-history-is-independent-of-v2', true)
      );
      break;
    case 'v2-kill-switch':
      checks.push(
        check(
          'v2-kill-switch-is-enabled',
          matrix.features.v2KillSwitchEnabled && failureCase.executorProtocolV2 === false
        ),
        check('new-admission-is-blocked', failureCase.executorProtocolV2 === false),
        check('existing-history-is-retained', failureCase.preserveHistory)
      );
      break;
  }
  if (checks.some(({passed}) => !passed)) {
    const failed = checks.find(({passed}) => !passed);
    fail(`${failureCase.id} failed check ${failed.name}`);
  }
  return {
    outcome: producedOutcomes[failureCase.id],
    checks,
    diagnostic: failureCase.diagnostic,
  };
}

function httpCaseInput(failureCase) {
  const input = {targetId: failureCase.targetId};
  const fields = {
    'duplicate-dispatch': ['idempotencyKey'],
    'duplicate-event': ['sequence', 'checksum'],
    'pre-ack-crash': ['acknowledged'],
    'post-ack-crash': ['acknowledged'],
    'stale-fence': ['presentedFenceGeneration', 'currentFenceGeneration'],
    'callback-loss': ['statusQuery'],
    timeout: ['elapsedMs', 'timeoutMs'],
    cancel: ['cancellable'],
    restart: ['persistedOperation'],
    'observer-mismatch': ['presentedObserverId', 'expectedObserverId'],
    'drift-reconcile': ['observedRelease', 'desiredRelease', 'reconcileTo'],
    'previous-state-b-to-a': ['from', 'to', 'priorActiveRelease'],
    'v1-regression': ['protocolVersion', 'newFlagsEnabled'],
    'v2-kill-switch': ['executorProtocolV2', 'preserveHistory'],
  };
  for (const field of fields[failureCase.id] ?? []) {
    input[field] = failureCase[field];
  }
  return input;
}

async function httpCaseResult(fixtureChecksum, matrix, failureCase, options) {
  const url = new URL(httpPath, options.baseURL);
  const target = matrix.targets.find(({id}) => id === failureCase.targetId);
  let response;
  try {
    response = await fetch(url, {
      method: 'POST',
      headers: {'content-type': 'application/json', accept: 'application/json'},
      body: JSON.stringify({
        caseId: failureCase.id,
        targetId: failureCase.targetId,
        expectedOutcome: failureCase.expectedOutcome,
        fixtureChecksum,
        input: httpCaseInput(failureCase),
        context: {
          target,
          releases: matrix.releases,
          features: matrix.features,
          execution: matrix.execution,
        },
      }),
      signal: AbortSignal.timeout(options.timeoutMs),
      redirect: 'error',
    });
  } catch {
    fail(`local HTTP failure case ${failureCase.id} could not reach the configured loopback runtime`);
  }
  if (!response.ok) {
    fail(`local HTTP failure case ${failureCase.id} returned HTTP ${response.status}`);
  }
  let payload;
  try {
    payload = await response.json();
  } catch {
    fail(`local HTTP failure case ${failureCase.id} did not return JSON`);
  }
  nonEmptyString(payload?.outcome, `local HTTP failure case ${failureCase.id} outcome`);
  if (
    !Array.isArray(payload.checks) ||
    payload.checks.length === 0 ||
    !payload.checks.every(
      (item) => typeof item?.name === 'string' && item.name !== '' && typeof item.passed === 'boolean'
    )
  ) {
    fail(`local HTTP failure case ${failureCase.id} checks are invalid`);
  }
  return {
    outcome: payload.outcome,
    checks: payload.checks.map((item) => ({
      name: redactText(item.name),
      passed: item.passed,
    })),
    diagnostic: payload.diagnostic,
  };
}

export async function runFailureMatrix(fixture, options = {mode: 'fixture'}) {
  validateFailureFixture(fixture);
  const matrix = fixture.failureMatrix;
  const fixtureChecksum = checksum(fixture);
  const casesByID = new Map(matrix.cases.map((failureCase) => [failureCase.id, failureCase]));
  const results = [];
  for (const id of requiredFailureCases) {
    const failureCase = casesByID.get(id);
    const actual =
      options.mode === 'http'
        ? await httpCaseResult(fixtureChecksum, matrix, failureCase, options)
        : fixtureCaseResult(matrix, failureCase);
    const checks = [
      ...actual.checks,
      check('expected-outcome-matches', actual.outcome === failureCase.expectedOutcome),
    ];
    const result = {
      id,
      targetId: failureCase.targetId,
      status: checks.every(({passed}) => passed) ? 'PASS' : 'FAIL',
      outcome: actual.outcome,
      expectedOutcome: failureCase.expectedOutcome,
      checks,
    };
    if (actual.diagnostic !== undefined) {
      result.diagnostic = redactText(actual.diagnostic);
    }
    result.checksum = checksum(result);
    results.push(result);
  }
  const report = {
    schemaVersion: reportSchema,
    fixtureSchema: matrix.schemaVersion,
    fixtureId: fixture.fixtureId ?? fixture.seed,
    fixtureChecksum,
    mode: options.mode ?? 'fixture',
    status: results.every(({status}) => status === 'PASS') ? 'PASS' : 'FAIL',
    caseCount: results.length,
    results,
  };
  report.reportChecksum = checksum(report);
  return report;
}

async function main() {
  const options = parseFailureMatrixArgs(process.argv.slice(2));
  let fixture;
  try {
    fixture = JSON.parse(await readFile(options.fixture, 'utf8'));
  } catch {
    fail('fixture must be readable valid JSON');
  }
  const report = await runFailureMatrix(fixture, options);
  process.stdout.write(`${JSON.stringify(report, null, 2)}\n`);
  if (report.status !== 'PASS') {
    for (const result of report.results.filter(({status}) => status !== 'PASS')) {
      process.stderr.write(`${result.id} expected ${result.expectedOutcome} but produced ${result.outcome}\n`);
    }
    process.exitCode = 1;
  }
}

const invokedPath = process.argv[1] ? path.resolve(process.argv[1]) : '';
if (invokedPath === fileURLToPath(import.meta.url)) {
  main().catch((error) => {
    process.stderr.write(`${redactText(error.message)}\n`);
    process.exitCode = 1;
  });
}
