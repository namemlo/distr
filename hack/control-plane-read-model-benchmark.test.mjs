import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import {mkdtemp, readFile} from 'node:fs/promises';
import {createServer} from 'node:http';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {test} from 'node:test';
import {fileURLToPath} from 'node:url';

import {startLoopbackProofService} from './control-plane-loopback-proof-service.mjs';
import {benchmark, parseBenchmarkArgs, validateFixture} from './control-plane-read-model-benchmark.mjs';

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const fixtureScript = path.join(repoRoot, 'hack', 'control-plane-scale-fixture.mjs');
const workloadNames = [
  'registry-list',
  'registry-detail',
  'matrix-list',
  'matrix-detail',
  'comparison-list',
  'comparison-detail',
  'history-list',
  'history-detail',
  'campaign-list',
  'campaign-detail',
];
const smokeWorkloadNames = [
  'fleet-list',
  'fleet-environment-filter',
  'fleet-stable-cursor',
  'campaign-wave-members',
  'fleet-detail',
];

async function generateFixture() {
  const directory = await mkdtemp(path.join(tmpdir(), 'control-plane-read-model-benchmark-'));
  const output = path.join(directory, 'fixture.json');
  const result = spawnSync(
    process.execPath,
    [
      fixtureScript,
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
    ],
    {cwd: repoRoot, encoding: 'utf8'}
  );
  assert.equal(result.status, 0, result.stderr);
  return JSON.parse(await readFile(output, 'utf8'));
}

function exactRemoteRequests(fixture) {
  return structuredClone(fixture.benchmark.remoteRequests);
}

test('fixture benchmark retains bounded smoke-workload samples and reports AC-50 as non-qualifying', async () => {
  const fixture = await generateFixture();
  const report = await benchmark(fixture, {
    runs: 20,
    pageSize: 100,
    thresholds: {p95Ms: 2000, p99Ms: 5000},
    timeoutMs: 10000,
    buildVersion: 'test-build',
    imageDigest: `sha256:${'a'.repeat(64)}`,
  });

  assert.deepEqual(
    report.workloads.map((workload) => workload.name),
    smokeWorkloadNames
  );
  assert.deepEqual(report.facts.workloads, smokeWorkloadNames);
  assert.equal(report.facts.boundedResponses, true);
  assert.equal(report.facts.pageSize, 100);
  assert.equal(report.facts.maxResponseItems <= 100, true);
  assert.equal(report.facts.isolation.violations, 0);
  for (const name of smokeWorkloadNames) {
    assert.equal(report.rawSamples.series[`${name}-p95-ms`].length, 20);
    assert.equal(report.rawSamples.series[`${name}-p99-ms`].length, 20);
    assert.equal(report.facts.responseCounts[name].responses, 20);
  }
  assert.deepEqual(report.qualification, {
    profile: 'smoke',
    acceptanceEligible: false,
    blockers: [
      'AC-50 requires truthful registry, matrix, comparison, history, and campaign list/detail descriptors in remote acceptance mode',
    ],
  });
  assert.equal(report.dataset.targets, 1000);
  assert.equal(report.dataset.placements, 649);
  assert.equal(report.dataset.onlineExecutors, 100);
  assert.equal(report.dataset.components >= 100, true);
  assert.equal(report.dataset.steps, 500);
  assert.equal(report.hardware.logicalCores > 0, true);
  assert.equal(report.hardware.memoryBytes > 0, true);
  assert.equal(report.build.version, 'test-build');
  assert.equal(report.build.artifactDigest, `sha256:${'a'.repeat(64)}`);
  assert.equal(report.image.digest, `sha256:${'a'.repeat(64)}`);
});

test('remote benchmark accepts bounded object details without leaking request secrets', async () => {
  const fixture = await generateFixture();
  fixture.benchmark.remoteRequests = exactRemoteRequests(fixture);
  const token = 'read-model-benchmark-secret';
  const sourceCommit = 'a'.repeat(40);
  const artifactDigest = `sha256:${'b'.repeat(64)}`;
  process.env.CONTROL_PLANE_READ_MODEL_TEST_TOKEN = token;
  const service = await startLoopbackProofService({
    fixture,
    token,
    metadata: {sourceCommit, buildVersion: 'remote-test', artifactDigest},
  });
  try {
    const report = await benchmark(fixture, {
      runs: 20,
      pageSize: 100,
      thresholds: {p95Ms: 2000, p99Ms: 5000},
      baseURL: service.baseURL,
      authEnv: 'CONTROL_PLANE_READ_MODEL_TEST_TOKEN',
      timeoutMs: 10000,
      buildVersion: 'remote-test',
      imageDigest: artifactDigest,
      profile: 'acceptance',
      sourceCommit,
      workingTreeDirty: false,
    });

    for (const name of workloadNames) {
      assert.equal(report.facts.responseCounts[name].responses, 20);
      assert.equal(report.facts.responseContracts[name].validResponses, 20);
    }
    assert.equal(report.facts.maxResponseItems, 100);
    assert.equal(report.facts.isolation.checks, workloadNames.length * 20);
    assert.equal(report.facts.isolation.violations, 0);
    assert.deepEqual(report.qualification, {
      profile: 'acceptance',
      acceptanceEligible: true,
      blockers: [],
    });
    assert.deepEqual(report.facts.metadataBindings, {responses: 200, validResponses: 200, issues: []});
    const serialized = JSON.stringify(report);
    assert.doesNotMatch(serialized, new RegExp(token));
    assert.doesNotMatch(serialized, /CONTROL_PLANE_READ_MODEL_TEST_TOKEN/);
  } finally {
    delete process.env.CONTROL_PLANE_READ_MODEL_TEST_TOKEN;
    await service.close();
  }
});

test('acceptance profile keeps zero-row and unbound build evidence diagnostic-only', async () => {
  const fixture = await generateFixture();
  fixture.benchmark.remoteRequests = exactRemoteRequests(fixture);
  const server = createServer((request, response) => {
    response.setHeader('content-type', 'application/json');
    const url = new URL(request.url, 'http://localhost');
    const descriptor = fixture.benchmark.remoteRequests.find((candidate) => {
      const expected = new URL(candidate.path, 'http://localhost');
      if (expected.pathname !== url.pathname) return false;
      for (const [key, value] of expected.searchParams) if (url.searchParams.get(key) !== value) return false;
      return true;
    });
    const envelope = descriptor?.response.envelope ?? 'items';
    response.end(JSON.stringify(envelope === 'items' ? {items: []} : {[envelope]: {}}));
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  try {
    const report = await benchmark(fixture, {
      runs: 2,
      pageSize: 100,
      thresholds: {p95Ms: 2000, p99Ms: 5000},
      baseURL: new URL(`http://127.0.0.1:${address.port}`),
      authEnv: 'CONTROL_PLANE_BENCHMARK_TOKEN',
      timeoutMs: 10000,
      profile: 'acceptance',
    });

    for (const name of workloadNames.filter((name) => name.endsWith('-list'))) {
      assert.equal(report.facts.responseCounts[name].items, 0);
    }
    assert.equal(report.build.version, 'unknown');
    assert.equal(report.build.artifactDigest, null);
    assert.equal(report.image.digest, null);
    assert.equal(report.qualification.acceptanceEligible, false);
    assert.match(report.qualification.blockers.join('\n'), /response contracts failed/);
    assert.match(report.qualification.blockers.join('\n'), /clean known source commit, build version/);
    assert.match(report.qualification.blockers.join('\n'), /not bound to the measured source commit/);
  } finally {
    await new Promise((resolve, reject) => server.close((error) => (error ? reject(error) : resolve())));
  }
});

test('fixture validation and CLI metadata require the exact workload set and an immutable image digest', async () => {
  const fixture = await generateFixture();
  assert.equal(validateFixture(fixture), fixture);
  assert.equal(validateFixture(fixture, {profile: 'acceptance'}), fixture);
  fixture.benchmark.remoteRequests[0].path = '/benchmark/registry-list';
  assert.throws(
    () => validateFixture(fixture, {profile: 'acceptance'}),
    /canonical endpoint and response contract/
  );

  const options = parseBenchmarkArgs([
    '--fixture',
    'fixture.json',
    '--build-version',
    '2026.7.29',
    '--image-digest',
    `sha256:${'c'.repeat(64)}`,
    '--profile',
    'acceptance',
  ]);
  assert.equal(options.buildVersion, '2026.7.29');
  assert.equal(options.imageDigest, `sha256:${'c'.repeat(64)}`);
  assert.equal(options.profile, 'acceptance');
  assert.throws(
    () => parseBenchmarkArgs(['--fixture', 'fixture.json', '--image-digest', 'latest']),
    /--image-digest must be a lowercase SHA-256 digest/
  );
});
