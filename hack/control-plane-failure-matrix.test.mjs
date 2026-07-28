import assert from 'node:assert/strict';
import {spawn} from 'node:child_process';
import {mkdtemp, writeFile} from 'node:fs/promises';
import {createServer} from 'node:http';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {test} from 'node:test';
import {fileURLToPath} from 'node:url';

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const matrixScript = path.join(repoRoot, 'hack', 'control-plane-failure-matrix.mjs');
const digest = (character) => `sha256:${character.repeat(64)}`;

const expectedOutcomes = {
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
};

function failureFixture() {
  const targetAlpha = {
    id: 'target-alpha',
    adapterId: 'adapter-http-alpha',
    observerId: 'observer-alpha',
    configChecksum: digest('1'),
    capabilityChecksum: digest('2'),
    topologyChecksum: digest('3'),
  };
  const targetBeta = {
    id: 'target-beta',
    adapterId: 'adapter-reference-beta',
    observerId: 'observer-beta',
    configChecksum: digest('4'),
    capabilityChecksum: digest('5'),
    topologyChecksum: digest('6'),
  };
  const cases = Object.entries(expectedOutcomes).map(([id, expectedOutcome]) => ({
    id,
    targetId: targetAlpha.id,
    expectedOutcome,
  }));
  Object.assign(
    cases.find(({id}) => id === 'duplicate-dispatch'),
    {
      idempotencyKey: 'target-alpha:plan-b:deploy',
    }
  );
  Object.assign(
    cases.find(({id}) => id === 'duplicate-event'),
    {
      sequence: 4,
      checksum: digest('7'),
    }
  );
  Object.assign(
    cases.find(({id}) => id === 'pre-ack-crash'),
    {
      acknowledged: false,
    }
  );
  Object.assign(
    cases.find(({id}) => id === 'post-ack-crash'),
    {
      acknowledged: true,
    }
  );
  Object.assign(
    cases.find(({id}) => id === 'stale-fence'),
    {
      presentedFenceGeneration: 1,
      currentFenceGeneration: 2,
    }
  );
  Object.assign(
    cases.find(({id}) => id === 'callback-loss'),
    {
      statusQuery: true,
    }
  );
  Object.assign(
    cases.find(({id}) => id === 'timeout'),
    {
      elapsedMs: 5001,
      timeoutMs: 5000,
    }
  );
  Object.assign(
    cases.find(({id}) => id === 'cancel'),
    {
      cancellable: true,
    }
  );
  Object.assign(
    cases.find(({id}) => id === 'restart'),
    {
      persistedOperation: true,
    }
  );
  Object.assign(
    cases.find(({id}) => id === 'observer-mismatch'),
    {
      targetId: targetBeta.id,
      presentedObserverId: targetAlpha.observerId,
      expectedObserverId: targetBeta.observerId,
    }
  );
  Object.assign(
    cases.find(({id}) => id === 'drift-reconcile'),
    {
      observedRelease: 'B',
      desiredRelease: 'A',
      reconcileTo: 'A',
    }
  );
  Object.assign(
    cases.find(({id}) => id === 'previous-state-b-to-a'),
    {
      from: 'B',
      to: 'A',
      priorActiveRelease: 'B',
    }
  );
  Object.assign(
    cases.find(({id}) => id === 'v1-regression'),
    {
      protocolVersion: 'v1',
      newFlagsEnabled: false,
    }
  );
  Object.assign(
    cases.find(({id}) => id === 'v2-kill-switch'),
    {
      executorProtocolV2: false,
      preserveHistory: true,
    }
  );
  return {
    schemaVersion: 'distr.control-plane-e2e-fixture/v1',
    fixtureId: 'neutral-control-plane-v1',
    failureMatrix: {
      schemaVersion: 'distr.control-plane-failure-matrix-fixture/v1',
      targets: [targetAlpha, targetBeta],
      releases: {
        A: {productReleaseId: 'release-a', digest: digest('a')},
        B: {productReleaseId: 'release-b', digest: digest('b')},
      },
      features: {
        operatorControlPlaneV2: true,
        executorProtocolV2: true,
        organizationEnrollment: true,
        environmentEnrollment: true,
        v1RegressionEnabled: true,
        v2KillSwitchEnabled: true,
      },
      execution: {
        fenceToken: 'fence-2',
        fenceGeneration: 2,
        leaseGeneration: 3,
        timeoutMs: 5000,
        deadline: '2030-01-01T00:05:00.000Z',
        priorActiveRelease: 'B',
      },
      cases,
    },
  };
}

function run(args, env = {}) {
  return new Promise((resolve) => {
    const child = spawn(process.execPath, [matrixScript, ...args], {
      cwd: repoRoot,
      env: {...process.env, ...env},
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    let stdout = '';
    let stderr = '';
    child.stdout.setEncoding('utf8');
    child.stderr.setEncoding('utf8');
    child.stdout.on('data', (chunk) => {
      stdout += chunk;
    });
    child.stderr.on('data', (chunk) => {
      stderr += chunk;
    });
    child.on('close', (status) => resolve({status, stdout, stderr}));
  });
}

async function writeFixture(fixture = failureFixture()) {
  const directory = await mkdtemp(path.join(tmpdir(), 'control-plane-failure-matrix-'));
  const fixturePath = path.join(directory, 'fixture.json');
  await writeFile(fixturePath, `${JSON.stringify(fixture, null, 2)}\n`);
  return fixturePath;
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

async function waitForReady(url, child, output) {
  const deadline = Date.now() + 5000;
  while (Date.now() < deadline) {
    if (child.exitCode !== null) {
      assert.fail(`adapter exited before ready: ${output()}`);
    }
    try {
      const response = await fetch(url);
      if (response.ok) return;
    } catch {
      // The child can take a few event-loop turns to bind.
    }
    await new Promise((resolve) => setTimeout(resolve, 25));
  }
  assert.fail(`adapter did not become ready: ${output()}`);
}

async function stopChild(child) {
  if (child.exitCode !== null) return;
  child.kill('SIGTERM');
  await Promise.race([
    new Promise((resolve) => child.once('exit', resolve)),
    new Promise((resolve) => setTimeout(resolve, 1000)),
  ]);
  if (child.exitCode === null) child.kill('SIGKILL');
}

test('fixture mode emits all deterministic failure cases with retained checksums', async () => {
  const fixture = await writeFixture();

  const first = await run(['--fixture', fixture]);
  const second = await run(['--fixture', fixture]);

  assert.equal(first.status, 0, first.stderr);
  assert.equal(second.status, 0, second.stderr);
  assert.equal(first.stdout, second.stdout);
  const report = JSON.parse(first.stdout);
  assert.equal(report.schemaVersion, 'distr.control-plane-failure-matrix-report/v2');
  assert.equal(report.mode, 'fixture');
  assert.equal(report.status, 'SIMULATION_ONLY');
  assert.equal(report.acceptanceEligible, false);
  assert.equal(report.proofMode, 'NON_ACCEPTANCE_FIXTURE_SIMULATION');
  assert.equal(report.caseCount, 14);
  assert.deepEqual(
    report.results.map(({id}) => id),
    Object.keys(expectedOutcomes)
  );
  for (const result of report.results) {
    assert.equal(result.status, 'SIMULATED');
    assert.equal(result.outcome, expectedOutcomes[result.id]);
    assert.match(result.checksum, /^sha256:[0-9a-f]{64}$/);
    assert.ok(result.checks.length > 0);
    assert.ok(result.checks.every(({passed}) => passed === true));
  }
  assert.match(report.fixtureChecksum, /^sha256:[0-9a-f]{64}$/);
  assert.match(report.reportChecksum, /^sha256:[0-9a-f]{64}$/);
});

test('matrix fails closed when a required case is missing', async () => {
  const fixtureValue = failureFixture();
  fixtureValue.failureMatrix.cases = fixtureValue.failureMatrix.cases.filter(({id}) => id !== 'stale-fence');
  const fixture = await writeFixture(fixtureValue);

  const result = await run(['--fixture', fixture]);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /missing required failure case stale-fence/);
});

test('matrix fails when a case expects an outcome that the contract does not produce', async () => {
  const fixtureValue = failureFixture();
  fixtureValue.failureMatrix.cases.find(({id}) => id === 'callback-loss').expectedOutcome =
    'SUCCEEDED_WITHOUT_RECONCILIATION';
  const fixture = await writeFixture(fixtureValue);

  const result = await run(['--fixture', fixture]);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /callback-loss expected SUCCEEDED_WITHOUT_RECONCILIATION but produced STATUS_RECONCILED/);
  assert.equal(result.stderr.trim().split(/\r?\n/).length, 1);
});

test('failed clean execution is not acceptance eligible', async () => {
  const fixtureValue = failureFixture();
  fixtureValue.failureMatrix.cases.find(({id}) => id === 'callback-loss').expectedOutcome =
    'SUCCEEDED_WITHOUT_RECONCILIATION';
  const fixture = await writeFixture(fixtureValue);

  const result = await run(['--fixture', fixture, '--mode', 'clean']);

  assert.notEqual(result.status, 0);
  const report = JSON.parse(result.stdout);
  assert.equal(report.status, 'FAIL');
  assert.equal(report.acceptanceEligible, false);
});

test('matrix redacts secrets from retained case diagnostics', async () => {
  const fixtureValue = failureFixture();
  const secret = 'fixture-secret-must-not-appear';
  fixtureValue.failureMatrix.cases.find(({id}) => id === 'cancel').diagnostic =
    `Authorization: Bearer ${secret}; password=${secret}`;
  const fixture = await writeFixture(fixtureValue);

  const result = await run(['--fixture', fixture]);

  assert.equal(result.status, 0, result.stderr);
  assert.doesNotMatch(`${result.stdout}\n${result.stderr}`, new RegExp(secret));
  assert.match(result.stdout, /\[REDACTED\]/);
});

test('live mode rejects non-loopback endpoints before making a request', async () => {
  const fixture = await writeFixture();

  const result = await run(['--fixture', fixture, '--mode', 'live', '--base-url', 'https://example.com']);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /--base-url must use a loopback host/);
});

test('live mode must be explicit when a base URL is supplied', async () => {
  const fixture = await writeFixture();

  const result = await run(['--fixture', fixture, '--base-url', 'http://127.0.0.1:1']);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /--base-url requires --mode live/);
});

test('clean mode injects every failure through the executable loopback adapter', async () => {
  const fixture = await writeFixture();

  const result = await run(['--fixture', fixture, '--mode', 'clean']);

  assert.equal(result.status, 0, result.stderr);
  const report = JSON.parse(result.stdout);
  assert.equal(report.mode, 'clean');
  assert.equal(report.status, 'PASS');
  assert.equal(report.acceptanceEligible, true);
  assert.equal(report.proofMode, 'LOOPBACK_EXECUTABLE_FAILURE_INJECTION');
  assert.equal(report.caseCount, 14);
  for (const entry of report.results) {
    assert.equal(entry.status, 'PASS');
    assert.ok(entry.evidence.actions.length >= 2, entry.id);
    assert.match(entry.evidence.runtimeChecksum, /^sha256:[0-9a-f]{64}$/);
  }
  assert.ok(report.results.find(({id}) => id === 'callback-loss').evidence.actions.includes('reconcile-status'));
  assert.ok(report.results.find(({id}) => id === 'restart').evidence.actions.includes('restart'));
  assert.ok(report.results.find(({id}) => id === 'v2-kill-switch').evidence.actions.includes('admit-v2'));
  assert.deepEqual(report.results.find(({id}) => id === 'v1-regression').evidence.terminalDetails.transitions, [
    'QUEUED->RUNNING',
    'RUNNING->SUCCEEDED',
  ]);
  assert.equal(report.results.find(({id}) => id === 'v2-kill-switch').evidence.terminalDetails.historyCount, 1);
});

test('live mode drives the supplied loopback adapter action protocol', async () => {
  const fixture = await writeFixture();
  const port = await unusedLoopbackPort();
  const child = spawn(
    process.execPath,
    [matrixScript, '--fixture', fixture, '--mode', 'serve', '--port', String(port)],
    {
      cwd: repoRoot,
      env: {...process.env},
      stdio: ['ignore', 'pipe', 'pipe'],
    }
  );
  let output = '';
  child.stdout.setEncoding('utf8');
  child.stderr.setEncoding('utf8');
  child.stdout.on('data', (chunk) => {
    output += chunk;
  });
  child.stderr.on('data', (chunk) => {
    output += chunk;
  });
  try {
    await waitForReady(`http://127.0.0.1:${port}/ready`, child, () => output);

    const result = await run(['--fixture', fixture, '--mode', 'live', '--base-url', `http://127.0.0.1:${port}`]);

    assert.equal(result.status, 0, result.stderr);
    const report = JSON.parse(result.stdout);
    assert.equal(report.mode, 'live');
    assert.equal(report.status, 'PASS');
    assert.equal(report.acceptanceEligible, true);
    assert.equal(report.proofMode, 'LOOPBACK_EXECUTABLE_FAILURE_INJECTION');
    assert.equal(report.results.length, 14);
  } finally {
    await stopChild(child);
  }
});

test('live mode rejects a loopback adapter loaded with a different fixture', async () => {
  const fixture = await writeFixture();
  const differentFixtureValue = failureFixture();
  differentFixtureValue.fixtureId = 'different-neutral-fixture';
  const differentFixture = await writeFixture(differentFixtureValue);
  const port = await unusedLoopbackPort();
  const child = spawn(
    process.execPath,
    [matrixScript, '--fixture', differentFixture, '--mode', 'serve', '--port', String(port)],
    {
      cwd: repoRoot,
      env: {...process.env},
      stdio: ['ignore', 'pipe', 'pipe'],
    }
  );
  let output = '';
  child.stdout.setEncoding('utf8');
  child.stderr.setEncoding('utf8');
  child.stdout.on('data', (chunk) => {
    output += chunk;
  });
  child.stderr.on('data', (chunk) => {
    output += chunk;
  });
  try {
    await waitForReady(`http://127.0.0.1:${port}/ready`, child, () => output);

    const result = await run(['--fixture', fixture, '--mode', 'live', '--base-url', `http://127.0.0.1:${port}`]);

    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /local failure-injection adapter fixture checksum mismatch/);
  } finally {
    await stopChild(child);
  }
});

test('live mode rejects an outcome-echo endpoint that does not execute adapter actions', async () => {
  const fixture = await writeFixture();
  const requests = [];
  const server = createServer((request, response) => {
    let body = '';
    request.setEncoding('utf8');
    request.on('data', (chunk) => {
      body += chunk;
    });
    request.on('end', () => {
      const payload = JSON.parse(body);
      requests.push({method: request.method, url: request.url, payload});
      response.setHeader('content-type', 'application/json');
      response.end(
        JSON.stringify({
          outcome: expectedOutcomes[payload.caseId] ?? 'IDEMPOTENT_REPLAY',
          checks: [{name: 'local-runtime-contract', passed: true}],
        })
      );
    });
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  try {
    const result = await run([
      '--fixture',
      fixture,
      '--mode',
      'live',
      '--base-url',
      `http://127.0.0.1:${address.port}`,
    ]);

    assert.notEqual(result.status, 0);
    assert.ok(requests.length > 0);
    assert.ok(requests.every(({url}) => url === '/api/v1/control-plane/failure-matrix/actions'));
    assert.match(result.stderr, /local failure-injection adapter response is invalid/);
  } finally {
    await new Promise((resolve, reject) => server.close((error) => (error ? reject(error) : resolve())));
  }
});
