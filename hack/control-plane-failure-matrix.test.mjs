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

test('fixture mode emits all deterministic failure cases with retained checksums', async () => {
  const fixture = await writeFixture();

  const first = await run(['--fixture', fixture]);
  const second = await run(['--fixture', fixture]);

  assert.equal(first.status, 0, first.stderr);
  assert.equal(second.status, 0, second.stderr);
  assert.equal(first.stdout, second.stdout);
  const report = JSON.parse(first.stdout);
  assert.equal(report.schemaVersion, 'distr.control-plane-failure-matrix-report/v1');
  assert.equal(report.mode, 'fixture');
  assert.equal(report.status, 'PASS');
  assert.equal(report.caseCount, 14);
  assert.deepEqual(
    report.results.map(({id}) => id),
    Object.keys(expectedOutcomes)
  );
  for (const result of report.results) {
    assert.equal(result.status, 'PASS');
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

test('HTTP mode rejects non-loopback endpoints before making a request', async () => {
  const fixture = await writeFixture();

  const result = await run(['--fixture', fixture, '--mode', 'http', '--base-url', 'https://example.com']);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /--base-url must use a loopback host/);
});

test('HTTP mode must be explicit when a base URL is supplied', async () => {
  const fixture = await writeFixture();

  const result = await run(['--fixture', fixture, '--base-url', 'http://127.0.0.1:1']);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /--base-url requires --mode http/);
});

test('explicit HTTP mode runs every case against a loopback runtime', async () => {
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
          outcome: expectedOutcomes[payload.caseId],
          checks: [{name: 'local-runtime-contract', passed: true}],
          diagnostic: `completed ${payload.caseId}`,
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
      'http',
      '--base-url',
      `http://127.0.0.1:${address.port}`,
    ]);

    assert.equal(result.status, 0, result.stderr);
    const report = JSON.parse(result.stdout);
    assert.equal(report.mode, 'http');
    assert.equal(report.status, 'PASS');
    assert.equal(requests.length, 14);
    for (const request of requests) {
      assert.equal(request.method, 'POST');
      assert.equal(request.url, '/api/v1/control-plane/failure-matrix');
      assert.equal(request.payload.expectedOutcome, expectedOutcomes[request.payload.caseId]);
      assert.match(request.payload.fixtureChecksum, /^sha256:[0-9a-f]{64}$/);
      assert.equal(request.payload.input.targetId, request.payload.targetId);
      assert.equal(request.payload.context.target.id, request.payload.targetId);
      assert.equal(request.payload.context.execution.fenceGeneration, 2);
      assert.equal(request.payload.context.releases.A.productReleaseId, 'release-a');
    }
    const staleFence = requests.find(({payload}) => payload.caseId === 'stale-fence');
    assert.equal(staleFence.payload.input.presentedFenceGeneration, 1);
    assert.equal(staleFence.payload.input.currentFenceGeneration, 2);
    const duplicateEvent = requests.find(({payload}) => payload.caseId === 'duplicate-event');
    assert.equal(duplicateEvent.payload.input.sequence, 4);
    assert.equal(duplicateEvent.payload.input.checksum, digest('7'));
  } finally {
    await new Promise((resolve, reject) => server.close((error) => (error ? reject(error) : resolve())));
  }
});
