#!/usr/bin/env node

import {createHash} from 'node:crypto';
import {readFile} from 'node:fs/promises';
import {createServer} from 'node:http';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

const fixtureSchema = 'distr.control-plane-failure-matrix-fixture/v1';
const reportSchema = 'distr.control-plane-failure-matrix-report/v2';
const actionPath = '/api/v1/control-plane/failure-matrix/actions';
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

const simulationOutcomes = Object.freeze({
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
  const allowed = new Set(['--fixture', '--mode', '--base-url', '--timeout-ms', '--port']);
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
  if (!['fixture', 'clean', 'live', 'serve'].includes(mode)) {
    fail('--mode must be fixture, clean, live, or serve');
  }
  if (mode !== 'live' && values.has('--base-url')) {
    fail('--base-url requires --mode live');
  }
  if (mode === 'live' && !values.has('--base-url')) {
    fail('--mode live requires --base-url');
  }
  if (mode === 'serve' && !values.has('--port')) {
    fail('--mode serve requires --port');
  }
  if (mode !== 'serve' && values.has('--port')) {
    fail('--port requires --mode serve');
  }
  return {
    fixture: path.resolve(fixture),
    mode,
    baseURL: values.has('--base-url') ? parseBaseURL(values.get('--base-url')) : undefined,
    timeoutMs: parseIntegerOption(values.get('--timeout-ms') ?? '10000', '--timeout-ms', 1, 300000),
    port: values.has('--port') ? parseIntegerOption(values.get('--port'), '--port', 1, 65535) : undefined,
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
    outcome: simulationOutcomes[failureCase.id],
    checks,
    diagnostic: failureCase.diagnostic,
  };
}

function createSession(matrix) {
  return {
    attempts: {},
    dispatches: {},
    resourceFences: {},
    activeReleases: Object.fromEntries(matrix.targets.map(({id}) => [id, 'A'])),
    history: [],
    v2Enabled: matrix.features.executorProtocolV2,
  };
}

function sessionSnapshot(session) {
  return {
    attempts: session.attempts,
    dispatches: session.dispatches,
    resourceFences: session.resourceFences,
    activeReleases: session.activeReleases,
    history: session.history,
    v2Enabled: session.v2Enabled,
  };
}

function adapterResult(session, action, status, details = {}, httpStatus = 200) {
  return {
    httpStatus,
    body: {
      schemaVersion: 'distr.control-plane-failure-injection-action/v1',
      action,
      status,
      details,
      runtimeChecksum: checksum(sessionSnapshot(session)),
    },
  };
}

function exactDispatch(existing, payload) {
  return (
    existing.targetId === payload.targetId &&
    existing.fenceGeneration === payload.fenceGeneration &&
    existing.inputChecksum === payload.inputChecksum
  );
}

function executeAdapterAction(matrix, sessions, request, fixtureChecksum) {
  const sessionID = nonEmptyString(request?.sessionId, 'local adapter sessionId');
  const action = nonEmptyString(request?.action, 'local adapter action');
  const payload = request?.payload ?? {};
  if (action === 'reset') {
    if (payload.fixtureChecksum !== fixtureChecksum) {
      fail('fixture checksum mismatch');
    }
    const session = createSession(matrix);
    sessions.set(sessionID, session);
    return adapterResult(session, action, 'RESET');
  }
  const session = sessions.get(sessionID);
  if (!session) {
    fail('local adapter session must be reset before actions');
  }
  const persist = (attempt) => {
    attempt.persisted = JSON.parse(JSON.stringify(attempt));
  };
  switch (action) {
    case 'set-fence': {
      session.resourceFences[payload.targetId] = payload.generation;
      return adapterResult(session, action, 'FENCE_SET', {
        generation: session.resourceFences[payload.targetId],
      });
    }
    case 'dispatch': {
      const existingID = session.dispatches[payload.idempotencyKey];
      if (existingID) {
        const existing = session.attempts[existingID];
        if (!exactDispatch(existing, payload)) {
          return adapterResult(session, action, 'CONFLICTING_DUPLICATE', {}, 409);
        }
        return adapterResult(session, action, 'REPLAY', {attemptId: existingID});
      }
      const currentFence = session.resourceFences[payload.targetId] ?? 0;
      if (payload.fenceGeneration <= currentFence) {
        return adapterResult(
          session,
          action,
          'STALE_FENCE',
          {presented: payload.fenceGeneration, current: currentFence},
          409
        );
      }
      const attempt = {
        id: payload.attemptId,
        targetId: payload.targetId,
        idempotencyKey: payload.idempotencyKey,
        inputChecksum: payload.inputChecksum,
        fenceGeneration: payload.fenceGeneration,
        acknowledged: false,
        status: 'CLAIMED',
        hubStatus: 'RUNNING',
        runtimeStatus: 'RUNNING',
        cancellable: payload.cancellable !== false,
        events: {},
        lastEventSequence: 0,
      };
      session.attempts[attempt.id] = attempt;
      session.dispatches[payload.idempotencyKey] = attempt.id;
      session.resourceFences[payload.targetId] = payload.fenceGeneration;
      persist(attempt);
      return adapterResult(session, action, 'DISPATCHED', {attemptId: attempt.id});
    }
    case 'event': {
      const attempt = session.attempts[payload.attemptId];
      if (!attempt) fail('local adapter event attempt is missing');
      const existing = attempt.events[payload.sequence];
      if (existing) {
        if (existing !== payload.checksum) {
          return adapterResult(session, action, 'CONFLICTING_DUPLICATE', {}, 409);
        }
        return adapterResult(session, action, 'REPLAY', {
          sequence: payload.sequence,
        });
      }
      if (payload.sequence !== attempt.lastEventSequence + 1) {
        return adapterResult(session, action, 'OUT_OF_ORDER', {}, 409);
      }
      attempt.events[payload.sequence] = payload.checksum;
      attempt.lastEventSequence = payload.sequence;
      persist(attempt);
      return adapterResult(session, action, 'EVENT_RECORDED', {
        sequence: payload.sequence,
      });
    }
    case 'acknowledge': {
      const attempt = session.attempts[payload.attemptId];
      if (!attempt) fail('local adapter acknowledgement attempt is missing');
      attempt.acknowledged = true;
      attempt.status = 'RUNNING';
      persist(attempt);
      return adapterResult(session, action, 'ACKNOWLEDGED');
    }
    case 'crash': {
      const attempt = session.attempts[payload.attemptId];
      if (!attempt) fail('local adapter crash attempt is missing');
      if (attempt.acknowledged) {
        attempt.status = 'UNKNOWN';
        attempt.hubStatus = 'UNKNOWN';
        persist(attempt);
        return adapterResult(session, action, 'STATUS_RECONCILIATION_REQUIRED', {
          redispatchable: false,
          reconciliationRequired: true,
        });
      }
      attempt.status = 'PENDING';
      attempt.hubStatus = 'PENDING';
      persist(attempt);
      return adapterResult(session, action, 'SAFE_REDISPATCH', {
        redispatchable: true,
        reconciliationRequired: false,
      });
    }
    case 'complete': {
      const attempt = session.attempts[payload.attemptId];
      if (!attempt) fail('local adapter completion attempt is missing');
      attempt.runtimeStatus = payload.status;
      attempt.status = payload.status;
      attempt.hubStatus = payload.callbackLoss ? 'UNKNOWN' : payload.status;
      persist(attempt);
      return adapterResult(session, action, 'RUNTIME_COMPLETED', {
        runtimeStatus: attempt.runtimeStatus,
        hubStatus: attempt.hubStatus,
      });
    }
    case 'reconcile-status': {
      const attempt = session.attempts[payload.attemptId];
      if (!attempt) fail('local adapter reconciliation attempt is missing');
      attempt.hubStatus = attempt.runtimeStatus;
      persist(attempt);
      return adapterResult(session, action, 'STATUS_RECONCILED', {
        status: attempt.hubStatus,
      });
    }
    case 'advance-time': {
      const attempt = session.attempts[payload.attemptId];
      if (!attempt) fail('local adapter timeout attempt is missing');
      if (payload.elapsedMs < payload.timeoutMs) {
        return adapterResult(session, action, 'RUNNING', {
          elapsedMs: payload.elapsedMs,
        });
      }
      attempt.status = 'TIMED_OUT';
      attempt.runtimeStatus = 'TIMED_OUT';
      attempt.hubStatus = 'TIMED_OUT';
      persist(attempt);
      return adapterResult(session, action, 'TIMED_OUT');
    }
    case 'cancel': {
      const attempt = session.attempts[payload.attemptId];
      if (!attempt) fail('local adapter cancel attempt is missing');
      if (!attempt.cancellable) {
        return adapterResult(session, action, 'NOT_CANCELLABLE', {}, 409);
      }
      attempt.status = 'CANCELED';
      attempt.runtimeStatus = 'CANCELED';
      attempt.hubStatus = 'CANCELED';
      persist(attempt);
      return adapterResult(session, action, 'CANCELED');
    }
    case 'restart': {
      const attempt = session.attempts[payload.attemptId];
      if (!attempt?.persisted) fail('local adapter restart state is missing');
      session.attempts[payload.attemptId] = JSON.parse(JSON.stringify(attempt.persisted));
      return adapterResult(session, action, 'RESUMED', {
        status: session.attempts[payload.attemptId].status,
        lastEventSequence: session.attempts[payload.attemptId].lastEventSequence,
      });
    }
    case 'observe': {
      const target = matrix.targets.find(({id}) => id === payload.targetId);
      if (!target) fail('local adapter observation target is missing');
      if (payload.observerId !== target.observerId) {
        return adapterResult(session, action, 'QUARANTINED', {expectedObserverId: target.observerId}, 403);
      }
      session.activeReleases[payload.targetId] = payload.observedRelease;
      const drift = payload.observedRelease !== payload.desiredRelease;
      return adapterResult(session, action, drift ? 'DRIFT' : 'VERIFIED', {
        observedRelease: payload.observedRelease,
        desiredRelease: payload.desiredRelease,
      });
    }
    case 'reconcile-release': {
      session.activeReleases[payload.targetId] = payload.release;
      session.history.push({targetId: payload.targetId, release: payload.release});
      return adapterResult(session, action, 'RECONCILED', {
        activeRelease: payload.release,
      });
    }
    case 'set-active': {
      session.activeReleases[payload.targetId] = payload.release;
      session.history.push({targetId: payload.targetId, release: payload.release});
      return adapterResult(session, action, 'ACTIVE_SET', {
        activeRelease: payload.release,
      });
    }
    case 'deploy-release': {
      session.activeReleases[payload.targetId] = payload.release;
      session.history.push({targetId: payload.targetId, release: payload.release});
      return adapterResult(session, action, `ACTIVE_${payload.release}`, {
        activeRelease: payload.release,
      });
    }
    case 'run-v1': {
      const allowed = {
        QUEUED: new Set(['RUNNING', 'SUCCEEDED', 'FAILED', 'CANCELED']),
        RUNNING: new Set(['RUNNING', 'SUCCEEDED', 'FAILED', 'CANCELED']),
      };
      let state = 'QUEUED';
      const transitions = [];
      for (const next of ['RUNNING', 'SUCCEEDED']) {
        if (!allowed[state]?.has(next)) {
          return adapterResult(session, action, 'V1_TRANSITION_REJECTED', {
            transitions,
            finalStatus: state,
          });
        }
        transitions.push(`${state}->${next}`);
        state = next;
      }
      return adapterResult(session, action, 'V1_EXECUTED', {
        transitions,
        finalStatus: state,
        flagsDisabled: payload.protocolVersion === 'v1' && payload.newFlagsEnabled === false,
      });
    }
    case 'record-history': {
      session.history.push({kind: 'v2-execution', status: 'SUCCEEDED'});
      return adapterResult(session, action, 'HISTORY_RECORDED', {
        historyCount: session.history.length,
      });
    }
    case 'set-v2': {
      session.v2Enabled = payload.enabled;
      return adapterResult(session, action, payload.enabled ? 'V2_ENABLED' : 'V2_DISABLED');
    }
    case 'admit-v2': {
      if (!session.v2Enabled) {
        return adapterResult(session, action, 'ADMISSION_BLOCKED', {historyCount: session.history.length}, 409);
      }
      return adapterResult(session, action, 'ADMITTED', {
        historyCount: session.history.length,
      });
    }
    default:
      fail(`unknown local adapter action ${action}`);
  }
}

async function readJSONRequest(request) {
  let body = '';
  for await (const chunk of request) {
    body += chunk;
    if (body.length > 1024 * 1024) fail('local adapter request is too large');
  }
  try {
    return JSON.parse(body);
  } catch {
    fail('local adapter request must be valid JSON');
  }
}

export function createFailureInjectionAdapter(fixture) {
  validateFailureFixture(fixture);
  const matrix = fixture.failureMatrix;
  const fixtureChecksum = checksum(fixture);
  const sessions = new Map();
  return createServer(async (request, response) => {
    response.setHeader('content-type', 'application/json');
    if (request.method === 'GET' && request.url === '/ready') {
      response.end(
        JSON.stringify({
          schemaVersion: 'distr.control-plane-failure-injection-adapter/v1',
          fixtureChecksum,
        })
      );
      return;
    }
    if (request.method !== 'POST' || request.url !== actionPath) {
      response.statusCode = 404;
      response.end(JSON.stringify({code: 'NOT_FOUND'}));
      return;
    }
    try {
      const result = executeAdapterAction(matrix, sessions, await readJSONRequest(request), fixtureChecksum);
      response.statusCode = result.httpStatus;
      response.end(JSON.stringify(result.body));
    } catch (error) {
      response.statusCode = 400;
      response.end(
        JSON.stringify({
          schemaVersion: 'distr.control-plane-failure-injection-error/v1',
          code: 'INVALID_ACTION',
          message: redactText(error.message),
        })
      );
    }
  });
}

async function callAdapter(baseURL, sessionId, action, payload, timeoutMs) {
  let response;
  try {
    response = await fetch(new URL(actionPath, baseURL), {
      method: 'POST',
      headers: {'content-type': 'application/json', accept: 'application/json'},
      body: JSON.stringify({sessionId, action, payload}),
      signal: AbortSignal.timeout(timeoutMs),
      redirect: 'error',
    });
  } catch {
    fail(`local failure-injection adapter could not execute ${action}`);
  }
  let result;
  try {
    result = await response.json();
  } catch {
    fail('local failure-injection adapter response is invalid');
  }
  if (!response.ok && result?.code === 'INVALID_ACTION' && typeof result.message === 'string') {
    fail(`local failure-injection adapter ${result.message}`);
  }
  if (
    result?.schemaVersion !== 'distr.control-plane-failure-injection-action/v1' ||
    result.action !== action ||
    typeof result.status !== 'string' ||
    !checksumPattern.test(result.runtimeChecksum ?? '')
  ) {
    fail('local failure-injection adapter response is invalid');
  }
  return {httpStatus: response.status, ...result};
}

function actionCheck(name, passed) {
  return {name, passed: Boolean(passed)};
}

async function executeLiveCase(matrix, failureCase, fixtureChecksum, baseURL, timeoutMs) {
  const sessionId = `failure-${failureCase.id}`;
  const actions = [];
  let final;
  const run = async (action, payload = {}) => {
    actions.push(action);
    final = await callAdapter(baseURL, sessionId, action, payload, timeoutMs);
    return final;
  };
  await run('reset', {fixtureChecksum});
  const attemptId = `${failureCase.id}-attempt`;
  const dispatch = (overrides = {}) =>
    run('dispatch', {
      attemptId,
      targetId: failureCase.targetId,
      idempotencyKey: failureCase.idempotencyKey ?? `${failureCase.id}:dispatch`,
      inputChecksum: checksum({
        targetId: failureCase.targetId,
        release: matrix.releases.B.digest,
      }),
      fenceGeneration: matrix.execution.fenceGeneration,
      cancellable: failureCase.cancellable ?? true,
      ...overrides,
    });
  let outcome = 'UNPROVEN';
  let checks = [];
  switch (failureCase.id) {
    case 'duplicate-dispatch': {
      const first = await dispatch();
      const replay = await dispatch();
      outcome = replay.status === 'REPLAY' ? 'IDEMPOTENT_REPLAY' : 'DUPLICATE_EXECUTED';
      checks = [
        actionCheck('first-dispatch-created-attempt', first.status === 'DISPATCHED'),
        actionCheck('duplicate-dispatch-replayed', replay.status === 'REPLAY'),
        actionCheck('duplicate-kept-attempt-identity', replay.details.attemptId === attemptId),
      ];
      break;
    }
    case 'duplicate-event': {
      await dispatch();
      for (let sequence = 1; sequence < failureCase.sequence; sequence++) {
        await run('event', {
          attemptId,
          sequence,
          checksum: checksum(`seed-event-${sequence}`),
        });
      }
      const first = await run('event', {
        attemptId,
        sequence: failureCase.sequence,
        checksum: failureCase.checksum,
      });
      const replay = await run('event', {
        attemptId,
        sequence: failureCase.sequence,
        checksum: failureCase.checksum,
      });
      outcome = replay.status === 'REPLAY' ? 'IDEMPOTENT_REPLAY' : 'DUPLICATE_EXECUTED';
      checks = [
        actionCheck('first-event-recorded', first.status === 'EVENT_RECORDED'),
        actionCheck('duplicate-event-replayed', replay.status === 'REPLAY'),
      ];
      break;
    }
    case 'pre-ack-crash': {
      await dispatch();
      const crash = await run('crash', {attemptId});
      outcome = crash.status;
      checks = [
        actionCheck('crash-was-before-ack', failureCase.acknowledged === false),
        actionCheck('attempt-is-redispatchable', crash.details.redispatchable === true),
      ];
      break;
    }
    case 'post-ack-crash': {
      await dispatch();
      await run('acknowledge', {attemptId});
      const crash = await run('crash', {attemptId});
      outcome = crash.status;
      checks = [
        actionCheck('crash-was-after-ack', failureCase.acknowledged === true),
        actionCheck('status-reconciliation-required', crash.details.reconciliationRequired === true),
      ];
      break;
    }
    case 'stale-fence': {
      await run('set-fence', {
        targetId: failureCase.targetId,
        generation: failureCase.currentFenceGeneration,
      });
      const stale = await dispatch({
        fenceGeneration: failureCase.presentedFenceGeneration,
      });
      outcome = stale.status === 'STALE_FENCE' ? 'REJECTED_STALE_FENCE' : stale.status;
      checks = [
        actionCheck('stale-dispatch-rejected', stale.httpStatus === 409),
        actionCheck('current-fence-retained', stale.details.current === failureCase.currentFenceGeneration),
      ];
      break;
    }
    case 'callback-loss': {
      await dispatch();
      await run('acknowledge', {attemptId});
      const completed = await run('complete', {
        attemptId,
        status: 'SUCCEEDED',
        callbackLoss: true,
      });
      const reconciled = await run('reconcile-status', {attemptId});
      outcome = reconciled.status;
      checks = [
        actionCheck('callback-loss-left-hub-unknown', completed.details.hubStatus === 'UNKNOWN'),
        actionCheck('status-query-proved-success', reconciled.details.status === 'SUCCEEDED'),
      ];
      break;
    }
    case 'timeout': {
      await dispatch();
      await run('acknowledge', {attemptId});
      const timedOut = await run('advance-time', {
        attemptId,
        elapsedMs: failureCase.elapsedMs,
        timeoutMs: failureCase.timeoutMs,
      });
      outcome = timedOut.status;
      checks = [actionCheck('deadline-produced-terminal-timeout', timedOut.status === 'TIMED_OUT')];
      break;
    }
    case 'cancel': {
      await dispatch();
      const canceled = await run('cancel', {attemptId});
      outcome = canceled.status;
      checks = [
        actionCheck('cancel-was-runtime-action', actions.includes('cancel')),
        actionCheck('cancel-became-terminal', canceled.status === 'CANCELED'),
      ];
      break;
    }
    case 'restart': {
      await dispatch();
      await run('acknowledge', {attemptId});
      await run('event', {
        attemptId,
        sequence: 1,
        checksum: checksum('restart-event'),
      });
      const restarted = await run('restart', {attemptId});
      outcome = restarted.status;
      checks = [
        actionCheck('restart-restored-running-state', restarted.details.status === 'RUNNING'),
        actionCheck('restart-retained-event-sequence', restarted.details.lastEventSequence === 1),
      ];
      break;
    }
    case 'observer-mismatch': {
      const observed = await run('observe', {
        targetId: failureCase.targetId,
        observerId: failureCase.presentedObserverId,
        observedRelease: 'A',
        desiredRelease: 'A',
      });
      outcome = observed.status;
      checks = [
        actionCheck('mismatched-observer-rejected', observed.httpStatus === 403),
        actionCheck(
          'expected-observer-remained-bound',
          observed.details.expectedObserverId === failureCase.expectedObserverId
        ),
      ];
      break;
    }
    case 'drift-reconcile': {
      await run('set-active', {
        targetId: failureCase.targetId,
        release: failureCase.observedRelease,
      });
      const drift = await run('observe', {
        targetId: failureCase.targetId,
        observerId: matrix.targets.find(({id}) => id === failureCase.targetId).observerId,
        observedRelease: failureCase.observedRelease,
        desiredRelease: failureCase.desiredRelease,
      });
      const reconciled = await run('reconcile-release', {
        targetId: failureCase.targetId,
        release: failureCase.reconcileTo,
      });
      outcome = reconciled.status;
      checks = [
        actionCheck('independent-observation-detected-drift', drift.status === 'DRIFT'),
        actionCheck(
          'reconciliation-activated-desired-release',
          reconciled.details.activeRelease === failureCase.desiredRelease
        ),
      ];
      break;
    }
    case 'previous-state-b-to-a': {
      await run('set-active', {
        targetId: failureCase.targetId,
        release: failureCase.from,
      });
      const deployed = await run('deploy-release', {
        targetId: failureCase.targetId,
        release: failureCase.to,
      });
      outcome = deployed.status;
      checks = [
        actionCheck('previous-state-started-at-b', failureCase.from === 'B'),
        actionCheck('previous-state-ended-at-a', deployed.details.activeRelease === 'A'),
      ];
      break;
    }
    case 'v1-regression': {
      const v1 = await run('run-v1', {
        protocolVersion: failureCase.protocolVersion,
        newFlagsEnabled: failureCase.newFlagsEnabled,
      });
      const transitionsUnchanged =
        JSON.stringify(v1.details.transitions) === JSON.stringify(['QUEUED->RUNNING', 'RUNNING->SUCCEEDED']);
      outcome =
        transitionsUnchanged && v1.details.finalStatus === 'SUCCEEDED' && v1.details.flagsDisabled
          ? 'V1_UNCHANGED'
          : 'V1_CHANGED';
      checks = [
        actionCheck('v1-transitions-executed', transitionsUnchanged),
        actionCheck('v1-reached-terminal-success', v1.details.finalStatus === 'SUCCEEDED'),
        actionCheck('v2-flags-disabled-for-v1', v1.details.flagsDisabled === true),
      ];
      break;
    }
    case 'v2-kill-switch': {
      const history = await run('record-history');
      await run('set-v2', {enabled: failureCase.executorProtocolV2});
      const admission = await run('admit-v2');
      outcome =
        admission.status === 'ADMISSION_BLOCKED' && admission.details.historyCount === history.details.historyCount
          ? 'ADMISSION_BLOCKED_HISTORY_PRESERVED'
          : admission.status;
      checks = [
        actionCheck('new-v2-admission-blocked', admission.httpStatus === 409),
        actionCheck('existing-history-preserved', admission.details.historyCount === history.details.historyCount),
      ];
      break;
    }
  }
  return {
    outcome,
    checks,
    evidence: {
      actions,
      runtimeChecksum: final.runtimeChecksum,
      terminalStatus: final.status,
      terminalDetails: final.details,
    },
  };
}

export async function runFailureMatrix(fixture, options = {mode: 'fixture'}) {
  validateFailureFixture(fixture);
  const matrix = fixture.failureMatrix;
  const fixtureChecksum = checksum(fixture);
  const simulation = (options.mode ?? 'fixture') === 'fixture';
  if (!simulation && !options.baseURL) {
    fail('executable failure matrix requires a loopback adapter base URL');
  }
  const casesByID = new Map(matrix.cases.map((failureCase) => [failureCase.id, failureCase]));
  const results = [];
  for (const id of requiredFailureCases) {
    const failureCase = casesByID.get(id);
    const actual = simulation
      ? fixtureCaseResult(matrix, failureCase)
      : await executeLiveCase(matrix, failureCase, fixtureChecksum, options.baseURL, options.timeoutMs);
    const checks = [
      ...actual.checks,
      check('expected-outcome-matches', actual.outcome === failureCase.expectedOutcome),
    ];
    const passed = checks.every(({passed}) => passed);
    const result = {
      id,
      targetId: failureCase.targetId,
      status: passed ? (simulation ? 'SIMULATED' : 'PASS') : 'FAIL',
      outcome: actual.outcome,
      expectedOutcome: failureCase.expectedOutcome,
      checks,
    };
    if (actual.evidence !== undefined) {
      result.evidence = actual.evidence;
    }
    if (actual.diagnostic !== undefined) {
      result.diagnostic = redactText(actual.diagnostic);
    }
    result.checksum = checksum(result);
    results.push(result);
  }
  const failed = results.some(({status}) => status === 'FAIL');
  const report = {
    schemaVersion: reportSchema,
    fixtureSchema: matrix.schemaVersion,
    fixtureId: fixture.fixtureId ?? fixture.seed,
    fixtureChecksum,
    mode: options.mode ?? 'fixture',
    proofMode: simulation ? 'NON_ACCEPTANCE_FIXTURE_SIMULATION' : 'LOOPBACK_EXECUTABLE_FAILURE_INJECTION',
    acceptanceEligible: !simulation && !failed,
    status: failed ? 'FAIL' : simulation ? 'SIMULATION_ONLY' : 'PASS',
    caseCount: results.length,
    results,
  };
  report.reportChecksum = checksum(report);
  return report;
}

async function listen(server, port) {
  await new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(port, '127.0.0.1', resolve);
  });
  return server.address();
}

async function close(server) {
  await new Promise((resolve, reject) => server.close((error) => (error ? reject(error) : resolve())));
}

async function serveAdapter(fixture, port) {
  const server = createFailureInjectionAdapter(fixture);
  await listen(server, port);
  process.stdout.write(
    `${JSON.stringify({
      schemaVersion: 'distr.control-plane-failure-injection-adapter/v1',
      ready: true,
      host: '127.0.0.1',
      port,
    })}\n`
  );
  await new Promise((resolve) => {
    const stop = () => resolve();
    process.once('SIGTERM', stop);
    process.once('SIGINT', stop);
  });
  await close(server);
}

async function main() {
  const options = parseFailureMatrixArgs(process.argv.slice(2));
  let fixture;
  try {
    fixture = JSON.parse(await readFile(options.fixture, 'utf8'));
  } catch {
    fail('fixture must be readable valid JSON');
  }
  if (options.mode === 'serve') {
    await serveAdapter(fixture, options.port);
    return;
  }
  let report;
  if (options.mode === 'clean') {
    const server = createFailureInjectionAdapter(fixture);
    const address = await listen(server, 0);
    try {
      report = await runFailureMatrix(fixture, {
        ...options,
        baseURL: new URL(`http://127.0.0.1:${address.port}`),
      });
    } finally {
      await close(server);
    }
  } else {
    report = await runFailureMatrix(fixture, options);
  }
  process.stdout.write(`${JSON.stringify(report, null, 2)}\n`);
  if (report.status === 'FAIL') {
    for (const result of report.results.filter(({status}) => status === 'FAIL')) {
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
