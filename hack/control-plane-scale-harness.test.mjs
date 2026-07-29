import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import {mkdtemp, readFile, writeFile} from 'node:fs/promises';
import {createServer} from 'node:http';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {test} from 'node:test';

import {benchmark} from './control-plane-read-model-benchmark.mjs';

const repoRoot = path.resolve(new URL('..', import.meta.url).pathname.slice(process.platform === 'win32' ? 1 : 0));
const fixtureScript = path.join(repoRoot, 'hack', 'control-plane-scale-fixture.mjs');
const benchmarkScript = path.join(repoRoot, 'hack', 'control-plane-read-model-benchmark.mjs');

const referenceArgs = [
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
];

function run(script, args, env = {}) {
  return spawnSync(process.execPath, [script, ...args], {
    cwd: repoRoot,
    encoding: 'utf8',
    env: {...process.env, ...env},
  });
}

async function generateFixture(directory, filename = 'fixture.json') {
  const output = path.join(directory, filename);
  const result = run(fixtureScript, [...referenceArgs, '--out', output]);
  assert.equal(result.status, 0, result.stderr);
  return {output, text: await readFile(output, 'utf8')};
}

test('scale fixture is deterministic and contains the reference workload', async () => {
  const directory = await mkdtemp(path.join(tmpdir(), 'control-plane-scale-'));
  const first = await generateFixture(directory, 'first.json');
  const second = await generateFixture(directory, 'second.json');
  const fixture = JSON.parse(first.text);

  assert.equal(first.text, second.text);
  assert.equal(fixture.schemaVersion, 'distr.control-plane-scale-fixture/v1');
  assert.deepEqual(fixture.parameters, {
    targets: 1000,
    placements: 649,
    agents: 100,
    components: 100,
    steps: 500,
  });
  assert.equal(fixture.targets.length, 1000);
  assert.equal(fixture.placements.length, 649);
  assert.equal(fixture.agents.length, 100);
  assert.equal(fixture.components.length, 625);
  assert.equal(fixture.steps.length, 500);
  assert.equal(fixture.campaign.waves[0].members.length, 500);
  assert.notEqual(fixture.primaryOrganization.id, fixture.isolationSentinel.organization.id);
  assert.equal(fixture.operatorReadModels.fleetRows.length, 1001);
  assert.ok(fixture.isolationSentinel.campaign.id);
  assert.ok(fixture.isolationSentinel.execution.id);
  for (const request of fixture.benchmark.remoteRequests) {
    assert.ok(request.forbiddenResourceIds.length > 0);
  }
  assert.deepEqual(fixture.loadProof, {
    planning: {componentCount: 100, runs: 5},
    wave: {stepCount: 500},
    events: {durationSeconds: 600, ratePerSecond: 100, concurrentAgents: 100},
    logs: {totalBytes: 100 * 1024 * 1024, maximumPageBytes: 1024 * 1024},
    thresholds: {
      planningP95Ms: 10_000,
      waveMaximumMs: 30_000,
      eventAcknowledgementP95Ms: 1_000,
      logFirstPageMs: 2_000,
      maximumCrossOrganizationRecords: 0,
      maximumNonPolicyErrorRateExclusive: 0.01,
    },
    remote: {
      planningPath: '/api/v1/control-plane/load-proof/plans',
      wavePath: '/api/v1/control-plane/load-proof/waves',
      eventPath: '/api/executor/v2/load-proof/events',
      logPath: '/api/v1/control-plane/load-proof/logs',
    },
  });
  assert.equal(fixture.expectations.cursorTie.orderedTargetIds.length, 4);
  assert.ok(fixture.expectations.filters.environment.targetIds.length > 0);
});

test('scale fixture models more than twenty isolated clients with more than twenty distinct services each', async () => {
  const directory = await mkdtemp(path.join(tmpdir(), 'control-plane-multi-client-scale-'));
  const generated = await generateFixture(directory);
  const fixture = JSON.parse(generated.text);

  assert.equal(fixture.clientOrganizations.length, 25);
  assert.equal(new Set(fixture.clientOrganizations.map((organization) => organization.id)).size, 25);
  assert.deepEqual(fixture.expectations.multiClient, {
    organizationCount: 25,
    minimumPlacementsPerOrganization: 25,
    minimumDistinctServicesPerOrganization: 25,
  });

  const componentByID = new Map(fixture.components.map((component) => [component.id, component]));
  for (const organization of fixture.clientOrganizations) {
    const componentDefinitions = fixture.components.filter((component) => component.organizationId === organization.id);
    const placements = fixture.placements.filter((placement) => placement.organizationId === organization.id);
    const distinctServiceIDs = new Set(placements.map((placement) => placement.componentId));

    assert.ok(
      componentDefinitions.length >= 25,
      `${organization.name} has only ${componentDefinitions.length} component definitions`
    );
    assert.ok(placements.length >= 25, `${organization.name} has only ${placements.length} placements`);
    assert.ok(distinctServiceIDs.size >= 25, `${organization.name} has only ${distinctServiceIDs.size} services`);
    for (const placement of placements) {
      assert.equal(componentByID.get(placement.componentId)?.organizationId, organization.id);
    }
  }
});

test('scale fixture keeps campaign membership and benchmark isolation scoped to the primary organization', async () => {
  const directory = await mkdtemp(path.join(tmpdir(), 'control-plane-multi-client-isolation-'));
  const generated = await generateFixture(directory);
  const fixture = JSON.parse(generated.text);
  const targetByID = new Map(fixture.targets.map((target) => [target.id, target]));
  const primaryID = fixture.primaryOrganization.id;

  for (const member of fixture.campaign.waves[0].members) {
    assert.equal(targetByID.get(member.targetId)?.organizationId, primaryID);
  }

  const fleetRequest = fixture.benchmark.remoteRequests.find((request) => request.name === 'fleet-list');
  assert.ok(fleetRequest);
  for (const organization of fixture.clientOrganizations.slice(1)) {
    const organizationTarget = fixture.targets.find((target) => target.organizationId === organization.id);
    assert.ok(organizationTarget);
    assert.ok(
      fleetRequest.forbiddenResourceIds.includes(organizationTarget.id),
      `${organization.name} is missing from the fleet isolation sentinels`
    );
  }
  assert.ok(fleetRequest.forbiddenResourceIds.includes(fixture.isolationSentinel.target.id));
});

test('scale fixture rejects values below the reference acceptance floor', async () => {
  const directory = await mkdtemp(path.join(tmpdir(), 'control-plane-scale-invalid-'));
  const invalidArgs = [...referenceArgs];
  invalidArgs[invalidArgs.indexOf('--targets') + 1] = '999';
  const result = run(fixtureScript, [...invalidArgs, '--out', path.join(directory, 'invalid.json')]);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /--targets must be an integer at least 1000/);
});

test('fixture benchmark runs twenty deterministic tenant-isolated pages', async () => {
  const directory = await mkdtemp(path.join(tmpdir(), 'control-plane-benchmark-'));
  const fixture = await generateFixture(directory);
  const result = run(benchmarkScript, ['--fixture', fixture.output, '--runs', '20', '--page-size', '100']);

  assert.equal(result.status, 0, result.stderr);
  const report = JSON.parse(result.stdout);
  assert.equal(report.schemaVersion, 'distr.control-plane-read-model-benchmark/v1');
  assert.equal(report.mode, 'fixture');
  assert.equal(report.runs, 20);
  assert.equal(report.pageSize, 100);
  assert.equal(report.isolationViolations, 0);
  assert.equal(report.thresholds.p95Ms, 2000);
  assert.equal(report.thresholds.p99Ms, 5000);
  assert.ok(report.samples > 0);
  for (const metric of [report.aggregate, ...report.workloads]) {
    assert.equal(typeof metric.p50Ms, 'number');
    assert.equal(typeof metric.p95Ms, 'number');
    assert.equal(typeof metric.p99Ms, 'number');
  }
});

test('benchmark rejects pages above the server maximum', async () => {
  const directory = await mkdtemp(path.join(tmpdir(), 'control-plane-page-limit-'));
  const fixture = await generateFixture(directory);
  const result = run(benchmarkScript, ['--fixture', fixture.output, '--runs', '20', '--page-size', '101']);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /--page-size must be an integer between 1 and 100/);
});

test('benchmark never prints a remote authorization token', async () => {
  const directory = await mkdtemp(path.join(tmpdir(), 'control-plane-auth-redaction-'));
  const fixture = await generateFixture(directory);
  const fixtureValue = JSON.parse(fixture.text);
  fixtureValue.benchmark.remoteRequests = [{name: 'unreachable', path: '/api/v1/control-plane/fleet'}];
  await writeFile(fixture.output, `${JSON.stringify(fixtureValue)}\n`);
  const secret = 'benchmark-secret-must-not-appear';
  const result = run(
    benchmarkScript,
    [
      '--fixture',
      fixture.output,
      '--runs',
      '20',
      '--base-url',
      'http://127.0.0.1:1',
      '--auth-env',
      'CONTROL_PLANE_TEST_TOKEN',
    ],
    {CONTROL_PLANE_TEST_TOKEN: secret}
  );

  assert.notEqual(result.status, 0);
  assert.doesNotMatch(`${result.stdout}\n${result.stderr}`, new RegExp(secret));
});

test('remote benchmark applies the requested bounded page size', async () => {
  const directory = await mkdtemp(path.join(tmpdir(), 'control-plane-remote-page-'));
  const generated = await generateFixture(directory);
  const fixture = JSON.parse(generated.text);
  fixture.benchmark.remoteRequests = [
    {
      name: 'fleet-list',
      path: '/api/v1/control-plane/fleet?limit=100',
      forbiddenResourceIds: [fixture.isolationSentinel.target.id],
    },
  ];
  const limits = [];
  const server = createServer((request, response) => {
    const url = new URL(request.url, 'http://localhost');
    const limit = Number(url.searchParams.get('limit'));
    limits.push(limit);
    response.setHeader('content-type', 'application/json');
    response.end(
      JSON.stringify({
        items: Array.from({length: limit}, (_, index) => ({
          organizationId: fixture.primaryOrganization.id,
          id: `row-${index}`,
        })),
      })
    );
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  try {
    const report = await benchmark(fixture, {
      runs: 20,
      pageSize: 25,
      thresholds: {p95Ms: 2000, p99Ms: 5000},
      baseURL: new URL(`http://127.0.0.1:${address.port}`),
      authEnv: 'CONTROL_PLANE_BENCHMARK_TOKEN',
      timeoutMs: 10000,
    });
    assert.equal(report.mode, 'remote');
    assert.deepEqual(new Set(limits), new Set([25]));
  } finally {
    await new Promise((resolve, reject) => server.close((error) => (error ? reject(error) : resolve())));
  }
});

test('remote benchmark rejects paths that could redirect authorization to another origin', async () => {
  const directory = await mkdtemp(path.join(tmpdir(), 'control-plane-remote-origin-'));
  const generated = await generateFixture(directory);
  const fixture = JSON.parse(generated.text);
  fixture.benchmark.remoteRequests = [{name: 'foreign-origin', path: '//localhost/api/v1/control-plane/fleet'}];

  await assert.rejects(
    benchmark(fixture, {
      runs: 20,
      pageSize: 100,
      thresholds: {p95Ms: 2000, p99Ms: 5000},
      baseURL: new URL('http://127.0.0.1:2'),
      authEnv: 'CONTROL_PLANE_BENCHMARK_TOKEN',
      timeoutMs: 10,
    }),
    /remote benchmark request name or path is invalid/
  );
});

test('remote benchmark rejects a response containing a visible sentinel resource ID', async () => {
  const directory = await mkdtemp(path.join(tmpdir(), 'control-plane-remote-isolation-'));
  const generated = await generateFixture(directory);
  const fixture = JSON.parse(generated.text);
  fixture.benchmark.remoteRequests = [
    {
      name: 'fleet-list',
      path: '/api/v1/control-plane/fleet',
      forbiddenResourceIds: [fixture.isolationSentinel.target.id],
    },
  ];
  const server = createServer((_request, response) => {
    response.setHeader('content-type', 'application/json');
    response.end(JSON.stringify({items: [{deploymentTargetId: fixture.isolationSentinel.target.id}]}));
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  try {
    await assert.rejects(
      benchmark(fixture, {
        runs: 20,
        pageSize: 100,
        thresholds: {p95Ms: 2000, p99Ms: 5000},
        baseURL: new URL(`http://127.0.0.1:${address.port}`),
        authEnv: 'CONTROL_PLANE_BENCHMARK_TOKEN',
        timeoutMs: 10000,
      }),
      /tenant isolation check found 20 foreign rows/
    );
  } finally {
    await new Promise((resolve, reject) => server.close((error) => (error ? reject(error) : resolve())));
  }
});

for (const collection of ['campaign', 'execution']) {
  test(`remote benchmark rejects a ${collection} response containing its visible sentinel ID`, async () => {
    const directory = await mkdtemp(path.join(tmpdir(), `control-plane-remote-${collection}-isolation-`));
    const generated = await generateFixture(directory);
    const fixture = JSON.parse(generated.text);
    const sentinel = fixture.isolationSentinel[collection];
    fixture.benchmark.remoteRequests = [
      {
        name: `${collection}-list`,
        path: `/api/v1/control-plane/${collection}s`,
        forbiddenResourceIds: [sentinel.id],
      },
    ];
    const server = createServer((_request, response) => {
      response.setHeader('content-type', 'application/json');
      response.end(JSON.stringify({items: [{id: sentinel.id}]}));
    });
    await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
    const address = server.address();
    try {
      await assert.rejects(
        benchmark(fixture, {
          runs: 20,
          pageSize: 100,
          thresholds: {p95Ms: 2000, p99Ms: 5000},
          baseURL: new URL(`http://127.0.0.1:${address.port}`),
          authEnv: 'CONTROL_PLANE_BENCHMARK_TOKEN',
          timeoutMs: 10000,
        }),
        /tenant isolation check found 20 foreign rows/
      );
    } finally {
      await new Promise((resolve, reject) => server.close((error) => (error ? reject(error) : resolve())));
    }
  });
}

test('remote benchmark fails closed when a workload omits forbidden sentinel IDs', async () => {
  const directory = await mkdtemp(path.join(tmpdir(), 'control-plane-remote-missing-sentinel-'));
  const generated = await generateFixture(directory);
  const fixture = JSON.parse(generated.text);
  fixture.benchmark.remoteRequests = [
    {
      name: 'campaign-list',
      path: '/api/v1/control-plane/campaigns',
    },
  ];

  await assert.rejects(
    benchmark(fixture, {
      runs: 20,
      pageSize: 100,
      thresholds: {p95Ms: 2000, p99Ms: 5000},
      baseURL: new URL('http://127.0.0.1:2'),
      authEnv: 'CONTROL_PLANE_BENCHMARK_TOKEN',
      timeoutMs: 10,
    }),
    /remote benchmark request forbiddenResourceIds is invalid/
  );
});
