import assert from 'node:assert/strict';
import {spawn, spawnSync} from 'node:child_process';
import {createHmac} from 'node:crypto';
import {readFile} from 'node:fs/promises';
import {createServer} from 'node:net';
import path from 'node:path';
import test from 'node:test';
import {fileURLToPath} from 'node:url';

const fixtureDir = fileURLToPath(new URL('.', import.meta.url));
const repoRoot = path.resolve(fixtureDir, '../..');
const node = process.execPath;
const executorSecretForTest = 'executor-memory-secret';

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

async function unusedLoopbackPort() {
  const server = createServer();
  await new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', resolve);
  });
  const {port} = server.address();
  await new Promise((resolve, reject) => server.close((error) => (error ? reject(error) : resolve())));
  return port;
}

async function waitForReady(url, child) {
  const deadline = Date.now() + 5_000;
  while (Date.now() < deadline) {
    if (child.exitCode !== null) {
      assert.fail(`server exited before ready with status ${child.exitCode}`);
    }
    try {
      const response = await fetch(url);
      if (response.ok) {
        return;
      }
    } catch {
      // The process can take a few event-loop turns to bind the port.
    }
    await new Promise((resolve) => setTimeout(resolve, 25));
  }
  assert.fail(`server did not become ready at ${url}`);
}

function startFixtureServer(script, env) {
  const child = spawn(node, [path.join(fixtureDir, script)], {
    cwd: repoRoot,
    env: {...process.env, ...env},
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  let output = '';
  child.stdout.on('data', (chunk) => {
    output += chunk;
  });
  child.stderr.on('data', (chunk) => {
    output += chunk;
  });
  return {child, output: () => output};
}

async function stopFixtureServer(child) {
  if (child.exitCode !== null) {
    return;
  }
  child.kill('SIGTERM');
  await Promise.race([
    new Promise((resolve) => child.once('exit', resolve)),
    new Promise((resolve) => setTimeout(resolve, 1_000)),
  ]);
  if (child.exitCode === null) {
    child.kill('SIGKILL');
  }
}

function operation(overrides = {}) {
  const body = {
    operationId: 'operation-alpha-a',
    idempotencyKey: 'target-alpha:plan-a:deploy',
    intent: {
      schemaVersion: 'distr.executor-intent/v2',
      tenantId: 'tenant-neutral',
      targetId: 'target-alpha',
      taskId: 'task-alpha-a',
      stepId: 'deploy',
      planId: 'plan-alpha-a',
      adapterRevision: 'external-http@1.0.0',
      resourceKey: 'deployment-target:target-alpha',
      fenceGeneration: 2,
      issuedAt: '2029-12-31T23:59:00.000Z',
      expiresAt: '2030-01-01T00:05:00.000Z',
      payload: {
        releaseDigest: `sha256:${'b'.repeat(64)}`,
        configChecksum: `sha256:${'1'.repeat(64)}`,
        migration: {
          id: 'migration-001',
          idempotencyKey: 'migration-001:target-alpha',
          retrySafe: true,
        },
      },
    },
    ...overrides,
  };
  body.signature =
    overrides.signature ??
    `sha256:${createHmac('sha256', executorSecretForTest).update(stableStringify(body.intent)).digest('hex')}`;
  return body;
}

test('fixture freezes two neutral targets and the canonical failure matrix', async () => {
  const fixture = JSON.parse(await readFile(path.join(fixtureDir, 'fixture.json'), 'utf8'));

  assert.equal(fixture.schemaVersion, 'distr.control-plane-e2e-fixture/v1');
  assert.deepEqual(
    fixture.targets.map(({id, adapterId, observerId}) => ({id, adapterId, observerId})),
    [
      {id: 'target-alpha', adapterId: 'adapter-http-alpha', observerId: 'observer-alpha'},
      {id: 'target-beta', adapterId: 'adapter-reference-beta', observerId: 'observer-beta'},
    ]
  );
  assert.equal(new Set(fixture.targets.map((target) => target.observerId)).size, 2);
  assert.equal(new Set(fixture.targets.map((target) => target.configChecksum)).size, 2);
  for (const target of fixture.targets) {
    assert.match(target.bindingId, /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
    assert.match(target.configChecksum, /^sha256:[0-9a-f]{64}$/);
    assert.match(target.capabilityChecksum, /^sha256:[0-9a-f]{64}$/);
    assert.match(target.topologyChecksum, /^sha256:[0-9a-f]{64}$/);
  }

  assert.deepEqual(fixture.product.capabilities, {
    providers: [{component: 'catalog-provider', capability: 'catalog.v1'}],
    consumers: [
      {
        component: 'gateway-consumer',
        requires: 'catalog.v1',
        provider: 'catalog-provider',
      },
    ],
  });
  assert.equal(fixture.product.migration.retrySafe, true);
  assert.ok(fixture.product.migration.idempotencyKey);
  assert.equal(fixture.campaign.waves.length, 2);
  assert.equal(fixture.governance.approvals.required, 2);
  assert.ok(fixture.governance.maintenanceWindow.notBefore);
  assert.ok(fixture.governance.maintenanceWindow.notAfter);
  assert.deepEqual(fixture.previousState, {from: 'B', to: 'A', priorActiveRelease: 'B'});

  const expectedCases = [
    ['duplicate-dispatch', 'IDEMPOTENT_REPLAY'],
    ['duplicate-event', 'IDEMPOTENT_REPLAY'],
    ['pre-ack-crash', 'SAFE_REDISPATCH'],
    ['post-ack-crash', 'STATUS_RECONCILIATION_REQUIRED'],
    ['stale-fence', 'REJECTED_STALE_FENCE'],
    ['callback-loss', 'STATUS_RECONCILED'],
    ['timeout', 'TIMED_OUT'],
    ['cancel', 'CANCELED'],
    ['restart', 'RESUMED'],
    ['observer-mismatch', 'QUARANTINED'],
    ['drift-reconcile', 'RECONCILED'],
    ['previous-state-b-to-a', 'ACTIVE_A'],
    ['v1-regression', 'V1_UNCHANGED'],
    ['v2-kill-switch', 'ADMISSION_BLOCKED_HISTORY_PRESERVED'],
  ];
  assert.equal(fixture.failureMatrix.schemaVersion, 'distr.control-plane-failure-matrix-fixture/v1');
  assert.deepEqual(
    fixture.failureMatrix.cases.map(({id, expectedOutcome}) => [id, expectedOutcome]),
    expectedCases
  );
  assert.deepEqual(
    fixture.failureMatrix.targets,
    fixture.targets.map((target) => ({
      id: target.id,
      adapterId: target.adapterId,
      observerId: target.observerId,
      configChecksum: target.configChecksum,
      capabilityChecksum: target.capabilityChecksum,
      topologyChecksum: target.topologyChecksum,
    }))
  );
  assert.deepEqual(fixture.failureMatrix.releases, {
    A: {
      productReleaseId: fixture.releases.A.productReleaseId,
      digest: fixture.releases.A.digest,
    },
    B: {
      productReleaseId: fixture.releases.B.productReleaseId,
      digest: fixture.releases.B.digest,
    },
  });
  assert.deepEqual(fixture.failureMatrix.features, fixture.features);
  assert.deepEqual(fixture.failureMatrix.execution, fixture.execution);
  const cases = Object.fromEntries(fixture.failureMatrix.cases.map((failureCase) => [failureCase.id, failureCase]));
  assert.deepEqual(
    {sequence: cases['duplicate-event'].sequence, checksum: cases['duplicate-event'].checksum},
    {
      sequence: 4,
      checksum: `sha256:${'8'.repeat(64)}`,
    }
  );
  assert.deepEqual(
    {
      presentedFenceGeneration: cases['stale-fence'].presentedFenceGeneration,
      currentFenceGeneration: cases['stale-fence'].currentFenceGeneration,
    },
    {presentedFenceGeneration: 1, currentFenceGeneration: 2}
  );
  assert.deepEqual(
    {elapsedMs: cases.timeout.elapsedMs, timeoutMs: cases.timeout.timeoutMs},
    {elapsedMs: 5001, timeoutMs: 5000}
  );
  assert.equal(cases.cancel.cancellable, true);
  assert.deepEqual(
    {
      presentedObserverId: cases['observer-mismatch'].presentedObserverId,
      expectedObserverId: cases['observer-mismatch'].expectedObserverId,
    },
    {presentedObserverId: 'observer-untrusted', expectedObserverId: 'observer-beta'}
  );
  assert.deepEqual(
    {
      observedRelease: cases['drift-reconcile'].observedRelease,
      desiredRelease: cases['drift-reconcile'].desiredRelease,
    },
    {observedRelease: 'A', desiredRelease: 'B'}
  );
  assert.deepEqual(
    {
      from: cases['previous-state-b-to-a'].from,
      to: cases['previous-state-b-to-a'].to,
      priorActiveRelease: cases['previous-state-b-to-a'].priorActiveRelease,
    },
    {from: 'B', to: 'A', priorActiveRelease: 'B'}
  );
  for (const failureCase of fixture.failureMatrix.cases) {
    assert.ok(failureCase.targetId);
  }
});

test('contract mode deterministically proves A, B, and previous-state B-to-A without live access', () => {
  const result = spawnSync(node, [path.join(fixtureDir, 'run.mjs'), '--mode', 'contract', '--json'], {
    cwd: repoRoot,
    encoding: 'utf8',
    env: {...process.env, DISTR_CP_ALLOW_LIVE: ''},
  });

  assert.equal(result.status, 0, result.stderr || result.stdout);
  const report = JSON.parse(result.stdout);
  assert.equal(report.ok, true);
  assert.equal(report.mode, 'contract');
  assert.deepEqual(report.targets, [
    {id: 'target-alpha', activeRelease: 'A', observerId: 'observer-alpha'},
    {id: 'target-beta', activeRelease: 'A', observerId: 'observer-beta'},
  ]);
  assert.deepEqual(report.releaseHistory, ['A', 'B', 'A']);
  assert.equal(report.migration.appliedCount, 1);
  assert.equal(report.secretLeaks, 0);
  assert.match(report.flowChecksum, /^sha256:[0-9a-f]{64}$/);
});

test('clean mode forced fallback reports the live blocker and completes scoped cleanup', () => {
  const result = spawnSync(node, [path.join(fixtureDir, 'run.mjs'), '--mode', 'clean', '--json'], {
    cwd: repoRoot,
    encoding: 'utf8',
    env: {
      ...process.env,
      DISTR_CP_FORCE_CONTRACT: 'true',
      DISTR_CP_ALLOW_LIVE: '',
    },
  });

  assert.equal(result.status, 0, result.stderr || result.stdout);
  const report = JSON.parse(result.stdout);
  assert.equal(report.ok, true);
  assert.equal(report.mode, 'clean');
  assert.equal(report.proofMode, 'fixture-contract');
  assert.equal(report.cleanup.completed, true);
  assert.match(report.liveStack.blocker, /forced contract mode/i);
  assert.equal(report.liveStack.nonLocalCalls, 0);
});

test('HTTP external executor is target-bound, fenced, idempotent, cancellable, and redacts logs', async () => {
  const port = await unusedLoopbackPort();
  const secret = executorSecretForTest;
  const {child, output} = startFixtureServer('external-executor.mjs', {
    PORT: String(port),
    EXECUTOR_ID: 'executor-http-alpha',
    TARGET_ID: 'target-alpha',
    EXECUTOR_SHARED_SECRET: secret,
    MAX_LOG_BYTES: '512',
  });
  const baseURL = `http://127.0.0.1:${port}`;
  const headers = {
    Authorization: `Bearer ${secret}`,
    'Content-Type': 'application/json',
  };

  try {
    await waitForReady(`${baseURL}/ready`, child);

    const first = await fetch(`${baseURL}/v1/operations`, {
      method: 'POST',
      headers,
      body: JSON.stringify(operation()),
    });
    assert.equal(first.status, 202);
    const firstBody = await first.json();
    assert.equal(firstBody.operationId, 'operation-alpha-a');
    assert.equal(firstBody.status, 'SUCCEEDED');

    const replay = await fetch(`${baseURL}/v1/operations`, {
      method: 'POST',
      headers,
      body: JSON.stringify(operation()),
    });
    assert.equal(replay.status, 200);
    assert.deepEqual(await replay.json(), firstBody);

    const stale = await fetch(`${baseURL}/v1/operations`, {
      method: 'POST',
      headers,
      body: JSON.stringify(
        operation({
          operationId: 'operation-stale',
          idempotencyKey: 'target-alpha:plan-stale:deploy',
          intent: {...operation().intent, fenceGeneration: 1},
        })
      ),
    });
    assert.equal(stale.status, 409);
    assert.equal((await stale.json()).code, 'STALE_FENCE');

    const invalidSignature = await fetch(`${baseURL}/v1/operations`, {
      method: 'POST',
      headers,
      body: JSON.stringify(
        operation({
          operationId: 'operation-invalid-signature',
          idempotencyKey: 'target-alpha:plan-invalid-signature:deploy',
          intent: {
            ...operation().intent,
            taskId: 'task-invalid-signature',
            planId: 'plan-invalid-signature',
            fenceGeneration: 3,
          },
          signature: `sha256:${'0'.repeat(64)}`,
        })
      ),
    });
    assert.equal(invalidSignature.status, 401);
    assert.equal((await invalidSignature.json()).code, 'INVALID_SIGNATURE');

    const longRunning = operation({
      operationId: 'operation-cancel',
      idempotencyKey: 'target-alpha:plan-cancel:deploy',
      intent: {
        ...operation().intent,
        taskId: 'task-cancel',
        planId: 'plan-cancel',
        fenceGeneration: 4,
        payload: {...operation().intent.payload, simulateLongRunning: true},
      },
    });
    const accepted = await fetch(`${baseURL}/v1/operations`, {
      method: 'POST',
      headers,
      body: JSON.stringify(longRunning),
    });
    assert.equal(accepted.status, 202);
    assert.equal((await accepted.json()).status, 'RUNNING');

    const canceled = await fetch(`${baseURL}/v1/operations/operation-cancel/cancel`, {
      method: 'POST',
      headers,
    });
    assert.equal(canceled.status, 200);
    assert.equal((await canceled.json()).status, 'CANCELED');

    const logs = await fetch(`${baseURL}/v1/operations/operation-alpha-a/logs`, {headers});
    assert.equal(logs.status, 200);
    const logBody = await logs.text();
    assert.ok(Buffer.byteLength(logBody) <= 512);
    assert.ok(logBody.includes('[REDACTED]'));
    assert.ok(!logBody.includes(secret));
    assert.ok(!output().includes(secret));
  } finally {
    await stopFixtureServer(child);
  }
});

test('independent observer enforces identity, target, sequence, and immutable evidence checksums', async () => {
  const port = await unusedLoopbackPort();
  const secret = 'observer-memory-secret';
  const {child, output} = startFixtureServer('observer.mjs', {
    PORT: String(port),
    OBSERVER_ID: 'observer-alpha',
    TARGET_ID: 'target-alpha',
    OBSERVER_SHARED_SECRET: secret,
  });
  const baseURL = `http://127.0.0.1:${port}`;
  const headers = {
    Authorization: `Bearer ${secret}`,
    'Content-Type': 'application/json',
  };
  const observation = {
    observerId: 'observer-alpha',
    targetId: 'target-alpha',
    sequence: 1,
    observedAt: '2030-01-01T00:01:00.000Z',
    releaseDigest: `sha256:${'b'.repeat(64)}`,
    configChecksum: `sha256:${'1'.repeat(64)}`,
    capabilityChecksum: `sha256:${'2'.repeat(64)}`,
    topologyChecksum: `sha256:${'3'.repeat(64)}`,
    schemaVersion: '1',
    health: 'HEALTHY',
  };

  try {
    await waitForReady(`${baseURL}/ready`, child);

    const accepted = await fetch(`${baseURL}/v1/observations`, {
      method: 'POST',
      headers,
      body: JSON.stringify(observation),
    });
    assert.equal(accepted.status, 202);
    const acceptedBody = await accepted.json();
    assert.match(acceptedBody.evidenceChecksum, /^sha256:[0-9a-f]{64}$/);

    const replay = await fetch(`${baseURL}/v1/observations`, {
      method: 'POST',
      headers,
      body: JSON.stringify(observation),
    });
    assert.equal(replay.status, 200);
    assert.deepEqual(await replay.json(), acceptedBody);

    const advanced = await fetch(`${baseURL}/v1/observations`, {
      method: 'POST',
      headers,
      body: JSON.stringify({...observation, sequence: 2, observedAt: '2030-01-01T00:02:00.000Z'}),
    });
    assert.equal(advanced.status, 202);
    const advancedBody = await advanced.json();

    const stale = await fetch(`${baseURL}/v1/observations`, {
      method: 'POST',
      headers,
      body: JSON.stringify(observation),
    });
    assert.equal(stale.status, 409);
    assert.equal((await stale.json()).code, 'STALE_OBSERVATION');

    const wrongTarget = await fetch(`${baseURL}/v1/observations`, {
      method: 'POST',
      headers,
      body: JSON.stringify({...observation, sequence: 2, targetId: 'target-beta'}),
    });
    assert.equal(wrongTarget.status, 403);

    const latest = await fetch(`${baseURL}/v1/observations/latest`, {headers});
    assert.equal(latest.status, 200);
    assert.deepEqual(await latest.json(), advancedBody);
    assert.ok(!output().includes(secret));
  } finally {
    await stopFixtureServer(child);
  }
});
