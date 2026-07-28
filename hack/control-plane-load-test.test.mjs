import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import {mkdtemp, readFile, writeFile} from 'node:fs/promises';
import {createServer} from 'node:http';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {test} from 'node:test';

const repoRoot = path.resolve(new URL('..', import.meta.url).pathname.slice(process.platform === 'win32' ? 1 : 0));
const fixtureScript = path.join(repoRoot, 'hack', 'control-plane-scale-fixture.mjs');
const loadScript = path.join(repoRoot, 'hack', 'control-plane-load-test.mjs');

function run(script, args, env = {}) {
  return spawnSync(process.execPath, [script, ...args], {
    cwd: repoRoot,
    encoding: 'utf8',
    env: {...process.env, ...env},
  });
}

async function referenceFixture() {
  const directory = await mkdtemp(path.join(tmpdir(), 'control-plane-load-'));
  const output = path.join(directory, 'fixture.json');
  const result = run(fixtureScript, [
    '--targets',
    '1000',
    '--placements',
    '649',
    '--agents',
    '100',
    '--components',
    '100',
    '--steps',
    '500',
    '--out',
    output,
  ]);
  assert.equal(result.status, 0, result.stderr);
  return {output, fixture: JSON.parse(await readFile(output, 'utf8'))};
}

test('fixture mode truthfully reports deterministic compressed evidence for every section 20.9 SLO', async () => {
  const {output} = await referenceFixture();
  const first = run(loadScript, ['--fixture', output, '--duration', '10m', '--rate', '100']);
  const second = run(loadScript, ['--fixture', output, '--duration', '10m', '--rate', '100']);

  assert.equal(first.status, 0, first.stderr);
  assert.equal(second.status, 0, second.stderr);
  const report = JSON.parse(first.stdout);
  const repeated = JSON.parse(second.stdout);

  assert.equal(report.schemaVersion, 'distr.control-plane-load-proof/v1');
  assert.equal(report.mode, 'simulation');
  assert.equal(report.measurement, 'deterministic-simulation');
  assert.equal(report.timeCompression.applied, true);
  assert.equal(report.timeCompression.requestedDurationSeconds, 600);
  assert.ok(report.timeCompression.wallClockDurationSeconds > 0);
  assert.ok(report.timeCompression.ratio > 1);
  assert.equal(report.passed, true);
  assert.equal(report.acceptanceProfile.met, true);
  assert.deepEqual(report.rawSamples, repeated.rawSamples);

  assert.equal(report.dataset.parameters.components, 100);
  assert.equal(report.dataset.parameters.steps, 500);
  assert.match(report.dataset.fixtureSha256, /^[a-f0-9]{64}$/);
  assert.ok(report.hardware.logicalCpuCount > 0);
  assert.ok(report.hardware.totalMemoryBytes > 0);
  assert.match(report.build.commit, /^(?:[a-f0-9]{40}|unknown)$/);
  assert.equal(typeof report.build.workingTreeDirty, 'boolean');
  assert.match(report.build.harnessSha256, /^[a-f0-9]{64}$/);

  assert.equal(report.scenarios.planning.samples, 5);
  assert.equal(report.scenarios.planning.deterministicChecksum, true);
  assert.ok(report.scenarios.planning.p95Ms <= 10_000);
  assert.equal(report.scenarios.wave.steps, 500);
  assert.equal(report.scenarios.wave.stableOrder, true);
  assert.equal(report.scenarios.wave.duplicateAdmissions, 0);
  assert.ok(report.scenarios.wave.p99Ms <= 30_000);
  assert.equal(report.scenarios.events.targetRatePerSecond, 100);
  assert.equal(report.scenarios.events.concurrentAgents, 100);
  assert.equal(report.scenarios.events.requestedEvents, 60_000);
  assert.equal(report.scenarios.events.authentication, 'simulated');
  assert.equal(report.scenarios.events.lostAcceptedEvents, 0);
  assert.ok(report.scenarios.events.p95Ms <= 1_000);
  assert.equal(report.scenarios.logs.totalBytes, 100 * 1024 * 1024);
  assert.ok(report.scenarios.logs.maximumPageBytes < report.scenarios.logs.totalBytes);
  assert.ok(report.scenarios.logs.firstPageMs <= 2_000);
  assert.equal(report.scenarios.isolation.crossOrganizationRecords, 0);
  assert.ok(report.scenarios.errors.nonPolicyRate < 0.01);

  for (const samples of Object.values(report.rawSamples)) {
    assert.ok(Array.isArray(samples));
    assert.ok(samples.length > 0);
    assert.ok(samples.every((sample) => Number.isFinite(sample)));
  }
});

test('load proof rejects a fixture that weakens a bounded section 20.9 workload', async () => {
  const generated = await referenceFixture();
  generated.fixture.loadProof.logs.totalBytes -= 1;
  await writeFile(generated.output, `${JSON.stringify(generated.fixture)}\n`);

  const result = run(loadScript, ['--fixture', generated.output, '--duration', '10m', '--rate', '100']);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /fixture\.loadProof\.logs\.totalBytes must equal 104857600/);
});

test('remote mode rejects non-loopback origins without exposing its environment-only token', async () => {
  const generated = await referenceFixture();
  const secret = 'load-proof-secret-must-not-appear';
  const result = run(
    loadScript,
    [
      '--fixture',
      generated.output,
      '--duration',
      '1s',
      '--rate',
      '1',
      '--base-url',
      'https://example.com',
      '--auth-env',
      'CONTROL_PLANE_LOAD_TEST_TOKEN',
    ],
    {CONTROL_PLANE_LOAD_TEST_TOKEN: secret}
  );

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /--base-url must use a loopback host/);
  assert.doesNotMatch(`${result.stdout}\n${result.stderr}`, new RegExp(secret));
});

test('loopback remote mode measures authenticated acknowledgements and bounded log pages without compression', async () => {
  const generated = await referenceFixture();
  const loadModule = await import('./control-plane-load-test.mjs');
  assert.equal(typeof loadModule.loadProof, 'function');
  const secret = 'loopback-load-proof-token';
  const authorizations = [];
  const logPage = Buffer.alloc(1024 * 1024, 97);
  const server = createServer(async (request, response) => {
    authorizations.push(request.headers.authorization);
    const url = new URL(request.url, 'http://localhost');
    response.setHeader('content-type', 'application/json');
    if (url.pathname === generated.fixture.loadProof.remote.planningPath) {
      response.end(JSON.stringify({checksum: 'planning-checksum'}));
      return;
    }
    if (url.pathname === generated.fixture.loadProof.remote.wavePath) {
      response.end(JSON.stringify({stepCount: 500, stableOrder: true, duplicateAdmissions: 0}));
      return;
    }
    if (url.pathname === generated.fixture.loadProof.remote.eventPath) {
      let body = '';
      for await (const chunk of request) body += chunk;
      const event = JSON.parse(body);
      response.end(JSON.stringify({accepted: true, eventId: event.eventId}));
      return;
    }
    if (url.pathname === generated.fixture.loadProof.remote.logPath) {
      const page = Number(url.searchParams.get('page') ?? '0');
      response.setHeader('content-type', 'application/octet-stream');
      response.setHeader('x-next-page', page < 99 ? String(page + 1) : '');
      response.end(logPage);
      return;
    }
    response.end(JSON.stringify({items: []}));
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  const previousToken = process.env.CONTROL_PLANE_LOAD_TEST_TOKEN;
  process.env.CONTROL_PLANE_LOAD_TEST_TOKEN = secret;
  try {
    const fixtureBytes = Buffer.from(await readFile(generated.output));
    const report = await loadModule.loadProof(
      generated.fixture,
      {
        durationSeconds: 1,
        rate: 2,
        baseURL: new URL(`http://127.0.0.1:${address.port}`),
        authEnv: 'CONTROL_PLANE_LOAD_TEST_TOKEN',
        timeoutMs: 10_000,
      },
      fixtureBytes
    );

    assert.equal(report.mode, 'remote');
    assert.equal(report.measurement, 'measured-live');
    assert.equal(report.timeCompression.applied, false);
    assert.ok(report.timeCompression.wallClockDurationSeconds >= 1);
    assert.equal(report.passed, false);
    assert.equal(report.acceptanceProfile.met, false);
    assert.equal(report.scenarios.events.authentication, 'authenticated-live');
    assert.equal(report.scenarios.events.acceptedEvents, 2);
    assert.equal(report.scenarios.events.acknowledgedEvents, 2);
    assert.equal(report.scenarios.events.lostAcceptedEvents, 0);
    assert.equal(report.scenarios.logs.totalBytes, 100 * 1024 * 1024);
    assert.equal(report.scenarios.logs.maximumPageBytes, 1024 * 1024);
    assert.equal(report.scenarios.isolation.crossOrganizationRecords, 0);
    assert.ok(report.scenarios.errors.nonPolicyRate < 0.01);
    assert.ok(authorizations.length > 100);
    assert.deepEqual(new Set(authorizations), new Set([`Bearer ${secret}`]));
    assert.doesNotMatch(JSON.stringify(report), new RegExp(secret));
  } finally {
    if (previousToken === undefined) delete process.env.CONTROL_PLANE_LOAD_TEST_TOKEN;
    else process.env.CONTROL_PLANE_LOAD_TEST_TOKEN = previousToken;
    await new Promise((resolve, reject) => server.close((error) => (error ? reject(error) : resolve())));
  }
});

test('remote mode fails closed before resolving a network-path log endpoint', async () => {
  const generated = await referenceFixture();
  generated.fixture.loadProof.remote.logPath = '//example.com/logs';
  const loadModule = await import('./control-plane-load-test.mjs');
  const server = createServer(async (request, response) => {
    const url = new URL(request.url, 'http://localhost');
    response.setHeader('content-type', 'application/json');
    if (url.pathname === generated.fixture.loadProof.remote.planningPath) {
      response.end(JSON.stringify({checksum: 'planning-checksum'}));
      return;
    }
    if (url.pathname === generated.fixture.loadProof.remote.wavePath) {
      response.end(JSON.stringify({stepCount: 500, stableOrder: true, duplicateAdmissions: 0}));
      return;
    }
    if (url.pathname === generated.fixture.loadProof.remote.eventPath) {
      let body = '';
      for await (const chunk of request) body += chunk;
      const event = JSON.parse(body);
      response.end(JSON.stringify({accepted: true, eventId: event.eventId}));
      return;
    }
    response.end(JSON.stringify({items: []}));
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  const previousToken = process.env.CONTROL_PLANE_LOAD_TEST_TOKEN;
  process.env.CONTROL_PLANE_LOAD_TEST_TOKEN = 'same-origin-test-token';
  try {
    await assert.rejects(
      loadModule.loadProof(
        generated.fixture,
        {
          durationSeconds: 1,
          rate: 1,
          baseURL: new URL(`http://127.0.0.1:${address.port}`),
          authEnv: 'CONTROL_PLANE_LOAD_TEST_TOKEN',
          timeoutMs: 10_000,
        },
        Buffer.from(JSON.stringify(generated.fixture))
      ),
      /fixture load-proof remote path must be same-origin relative/
    );
  } finally {
    if (previousToken === undefined) delete process.env.CONTROL_PLANE_LOAD_TEST_TOKEN;
    else process.env.CONTROL_PLANE_LOAD_TEST_TOKEN = previousToken;
    await new Promise((resolve, reject) => server.close((error) => (error ? reject(error) : resolve())));
  }
});
