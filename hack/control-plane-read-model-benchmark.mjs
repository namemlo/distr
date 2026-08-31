#!/usr/bin/env node

import {execFileSync} from 'node:child_process';
import {createHash} from 'node:crypto';
import {readFileSync} from 'node:fs';
import {readFile} from 'node:fs/promises';
import {cpus, platform, release, totalmem} from 'node:os';
import path from 'node:path';
import {performance} from 'node:perf_hooks';
import {fileURLToPath} from 'node:url';

const fixtureSchema = 'distr.control-plane-scale-fixture/v1';
const reportSchema = 'distr.control-plane-read-model-benchmark/v1';
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
const acceptanceBlocker =
  'AC-50 requires truthful registry, matrix, comparison, history, and campaign ' +
  'list/detail descriptors in remote acceptance mode';
const proofHeaders = {
  sourceCommit: 'x-distr-proof-source-commit',
  buildVersion: 'x-distr-proof-build-version',
  artifactDigest: 'x-distr-proof-artifact-digest',
};

function fail(message) {
  throw new Error(message);
}

function integerOption(value, option, minimum, maximum = Number.MAX_SAFE_INTEGER) {
  if (!/^\d+$/.test(value ?? '')) {
    fail(`${option} must be an integer between ${minimum} and ${maximum}`);
  }
  const parsed = Number(value);
  if (!Number.isSafeInteger(parsed) || parsed < minimum || parsed > maximum) {
    fail(`${option} must be an integer between ${minimum} and ${maximum}`);
  }
  return parsed;
}

function positiveNumberOption(value, option) {
  const parsed = Number(value);
  if (!Number.isFinite(parsed) || parsed <= 0) {
    fail(`${option} must be a positive number`);
  }
  return parsed;
}

export function parseBenchmarkArgs(argv) {
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
  const allowed = new Set([
    '--fixture',
    '--runs',
    '--page-size',
    '--p95-ms',
    '--p99-ms',
    '--base-url',
    '--auth-env',
    '--timeout-ms',
    '--build-version',
    '--image-digest',
    '--profile',
  ]);
  for (const option of values.keys()) {
    if (!allowed.has(option)) {
      fail(`unknown option ${option}`);
    }
  }
  if (!values.get('--fixture')?.trim()) {
    fail('--fixture is required');
  }
  const p95Ms = positiveNumberOption(values.get('--p95-ms') ?? '2000', '--p95-ms');
  const p99Ms = positiveNumberOption(values.get('--p99-ms') ?? '5000', '--p99-ms');
  if (p99Ms < p95Ms) {
    fail('--p99-ms must be greater than or equal to --p95-ms');
  }
  const authEnv = values.get('--auth-env') ?? 'CONTROL_PLANE_BENCHMARK_TOKEN';
  if (!/^[A-Z_][A-Z0-9_]*$/.test(authEnv)) {
    fail('--auth-env must be an uppercase environment-variable name');
  }
  let baseURL;
  if (values.has('--base-url')) {
    baseURL = new URL(values.get('--base-url'));
    if (!['http:', 'https:'].includes(baseURL.protocol)) {
      fail('--base-url must use HTTP or HTTPS');
    }
    if (baseURL.username || baseURL.password) {
      fail('--base-url must not contain credentials');
    }
    if (baseURL.search || baseURL.hash) {
      fail('--base-url must not contain a query or fragment');
    }
  }
  const buildVersion = values.get('--build-version');
  if (buildVersion !== undefined && !/^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$/.test(buildVersion)) {
    fail('--build-version must be a non-secret build identifier');
  }
  const imageDigest = values.get('--image-digest');
  if (imageDigest !== undefined && !/^sha256:[0-9a-f]{64}$/.test(imageDigest)) {
    fail('--image-digest must be a lowercase SHA-256 digest');
  }
  const profile = values.get('--profile') ?? 'smoke';
  if (!['smoke', 'acceptance'].includes(profile)) {
    fail('--profile must be smoke or acceptance');
  }
  return {
    fixture: path.resolve(values.get('--fixture')),
    runs: integerOption(values.get('--runs') ?? '20', '--runs', 20, 10000),
    pageSize: integerOption(values.get('--page-size') ?? '100', '--page-size', 1, 100),
    thresholds: {p95Ms, p99Ms},
    baseURL,
    authEnv,
    timeoutMs: integerOption(values.get('--timeout-ms') ?? '10000', '--timeout-ms', 1, 300000),
    buildVersion,
    imageDigest,
    profile,
  };
}

function assertArray(value, label) {
  if (!Array.isArray(value)) {
    fail(`${label} must be an array`);
  }
}

function expectedAcceptanceRequests(fixture) {
  const resources = fixture.benchmark?.resources;
  if (
    !Array.isArray(resources?.releaseIds) ||
    resources.releaseIds.length !== 2 ||
    !Array.isArray(resources?.planIds) ||
    resources.planIds.length !== 2 ||
    !resources.executionId ||
    !resources.campaignId
  ) {
    fail('fixture benchmark acceptance resources are incomplete');
  }
  return [
    ['registry-list', '/api/v1/control-plane/releases', 'items', 100, [resources.releaseIds[0]]],
    [
      'registry-detail',
      `/api/v1/control-plane/releases/${resources.releaseIds[0]}`,
      'detail',
      1,
      [resources.releaseIds[0]],
    ],
    ['matrix-list', '/api/v1/control-plane/fleet', 'items', 25, [fixture.targets[0].id]],
    [
      'matrix-detail',
      `/api/v1/control-plane/fleet?deploymentTargetId=${fixture.targets[0].id}`,
      'items',
      1,
      [fixture.targets[0].id],
    ],
    ['comparison-list', '/api/v1/control-plane/plans', 'items', 100, [resources.planIds[0]]],
    [
      'comparison-detail',
      `/api/v1/control-plane/plans/${resources.planIds[0]}/compare/${resources.planIds[1]}`,
      'comparison',
      1,
      [...resources.planIds],
    ],
    ['history-list', '/api/v1/control-plane/executions', 'items', 100, [resources.executionId]],
    [
      'history-detail',
      `/api/v1/control-plane/executions/${resources.executionId}`,
      'detail',
      1,
      [resources.executionId],
    ],
    ['campaign-list', '/api/v1/control-plane/campaigns', 'items', 1, [resources.campaignId]],
    [
      'campaign-detail',
      `/api/v1/control-plane/campaigns/${resources.campaignId}`,
      'detail',
      1,
      [resources.campaignId],
    ],
  ].map(([name, requestPath, envelope, minimumItems, requiredResourceIds]) => ({
    name,
    path: requestPath,
    response: {envelope, minimumItems, requiredResourceIds},
  }));
}

function validateAcceptanceWorkloads(fixture, remoteRequests) {
  if (
    remoteRequests.length !== workloadNames.length ||
    workloadNames.some((name, index) => remoteRequests[index]?.name !== name)
  ) {
    fail('fixture benchmark must define exactly the ten required read-model workloads in contract order');
  }
  const expected = expectedAcceptanceRequests(fixture);
  for (let index = 0; index < expected.length; index++) {
    const request = remoteRequests[index];
    if (
      request.path !== expected[index].path ||
      JSON.stringify(request.response) !== JSON.stringify(expected[index].response)
    ) {
      fail(`fixture benchmark workload ${request.name} must use its canonical endpoint and response contract`);
    }
  }
}

export function validateFixture(fixture, options = {}) {
  if (!fixture || fixture.schemaVersion !== fixtureSchema) {
    fail(`fixture schema must be ${fixtureSchema}`);
  }
  for (const field of ['targets', 'placements', 'agents', 'components', 'steps']) {
    assertArray(fixture[field], `fixture.${field}`);
    const requestedCount = fixture.parameters?.[field];
    if (
      !Number.isSafeInteger(requestedCount) ||
      (field === 'components' && fixture[field].length < requestedCount) ||
      (field !== 'components' && fixture[field].length !== requestedCount)
    ) {
      fail(`fixture.${field} count does not match parameters.${field}`);
    }
  }
  assertArray(fixture.operatorReadModels?.fleetRows, 'fixture.operatorReadModels.fleetRows');
  assertArray(fixture.campaign?.waves, 'fixture.campaign.waves');
  assertArray(fixture.campaign?.waves[0]?.members, 'fixture.campaign.waves[0].members');
  if (fixture.campaign.waves[0].members.length !== 500) {
    fail('fixture campaign reference wave must contain exactly 500 members');
  }
  const primaryID = fixture.primaryOrganization?.id;
  const sentinelID = fixture.isolationSentinel?.organization?.id;
  if (!primaryID || !sentinelID || primaryID === sentinelID) {
    fail('fixture must contain distinct primary and isolation-sentinel organizations');
  }
  if (!fixture.operatorReadModels.fleetRows.some((row) => row.organizationId === sentinelID)) {
    fail('fixture read models must contain the isolation-sentinel row');
  }
  if (fixture.expectations?.maximumPageSize !== 100) {
    fail('fixture maximum page size must be 100');
  }
  const remoteRequests = fixture.benchmark?.remoteRequests;
  assertArray(remoteRequests, 'fixture.benchmark.remoteRequests');
  const profile = options.profile ?? 'smoke';
  if (profile === 'acceptance') {
    validateAcceptanceWorkloads(fixture, remoteRequests);
  }
  const acceptanceSentinels = [
    fixture.isolationSentinel?.target?.id,
    fixture.isolationSentinel?.release?.id,
    fixture.isolationSentinel?.plan?.id,
    fixture.isolationSentinel?.campaign?.id,
    fixture.isolationSentinel?.execution?.id,
  ];
  const expectedSentinels =
    profile === 'acceptance'
      ? new Map(workloadNames.map((name) => [name, acceptanceSentinels]))
      : new Map([
          ['fleet-list', fixture.isolationSentinel?.target?.id],
          ['campaign-list', fixture.isolationSentinel?.campaign?.id],
          ['execution-list', fixture.isolationSentinel?.execution?.id],
        ]);
  for (const request of remoteRequests) {
    if (
      !Array.isArray(request?.forbiddenResourceIds) ||
      request.forbiddenResourceIds.length === 0 ||
      !request.forbiddenResourceIds.every((id) => typeof id === 'string' && id !== '')
    ) {
      fail(`fixture benchmark request ${request?.name ?? '<unknown>'} must define forbiddenResourceIds`);
    }
    const expectedSentinel = expectedSentinels.get(request.name);
    const expectedIDs = Array.isArray(expectedSentinel) ? expectedSentinel : [expectedSentinel];
    if (expectedSentinels.has(request.name) && (!expectedSentinel || expectedIDs.some((id) => !id))) {
      fail(`fixture benchmark request ${request.name} is missing its response-visible sentinel resource`);
    }
    if (expectedSentinel && expectedIDs.some((id) => !request.forbiddenResourceIds.includes(id))) {
      fail(`fixture benchmark request ${request.name} must include every response-visible sentinel ID`);
    }
  }
  return fixture;
}

function compareFleetRows(left, right) {
  const byTime = right.lastExecutionAt.localeCompare(left.lastExecutionAt);
  return byTime || left.targetId.localeCompare(right.targetId);
}

function page(items, pageSize, offset = 0) {
  if (pageSize > 100) {
    fail('workload attempted to request more than 100 rows');
  }
  const result = items.slice(offset, offset + pageSize);
  if (result.length > pageSize) {
    fail('workload returned an unbounded page');
  }
  return result;
}

function fixtureWorkloads(fixture, pageSize) {
  const organizationID = fixture.primaryOrganization.id;
  const fleetRows = fixture.operatorReadModels.fleetRows;
  const tenantFleet = () => fleetRows.filter((row) => row.organizationId === organizationID);
  const environmentID = fixture.expectations.filters.environment.id;
  return [
    {
      name: 'fleet-list',
      run: () => page(tenantFleet().toSorted(compareFleetRows), pageSize),
    },
    {
      name: 'fleet-environment-filter',
      run: () =>
        page(
          tenantFleet()
            .filter((row) => row.environmentId === environmentID)
            .toSorted(compareFleetRows),
          pageSize
        ),
    },
    {
      name: 'fleet-stable-cursor',
      run: () => page(tenantFleet().toSorted(compareFleetRows), pageSize, pageSize),
    },
    {
      name: 'campaign-wave-members',
      run: () => page(fixture.campaign.waves[0].members, pageSize),
    },
    {
      name: 'fleet-detail',
      run: () => [tenantFleet().find((row) => row.targetId === fixture.targets[0].id)],
    },
  ];
}

function percentile(samples, value) {
  if (samples.length === 0) {
    fail('cannot calculate a percentile without samples');
  }
  const sorted = samples.toSorted((left, right) => left - right);
  return sorted[Math.max(0, Math.ceil(value * sorted.length) - 1)];
}

function rounded(value) {
  return Number(value.toFixed(3));
}

function metrics(name, samples) {
  return {
    name,
    samples: samples.length,
    p50Ms: rounded(percentile(samples, 0.5)),
    p95Ms: rounded(percentile(samples, 0.95)),
    p99Ms: rounded(percentile(samples, 0.99)),
  };
}

function isolationViolations(rows, forbiddenResourceIDs) {
  const forbidden = new Set(forbiddenResourceIDs);
  const containsForbiddenID = (value) => {
    if (typeof value === 'string') return forbidden.has(value);
    if (Array.isArray(value)) return value.some(containsForbiddenID);
    if (value && typeof value === 'object') return Object.values(value).some(containsForbiddenID);
    return false;
  };
  return rows.filter(containsForbiddenID).length;
}

function responseCountMap(names) {
  return new Map(names.map((name) => [name, {responses: 0, items: 0, maxItems: 0}]));
}

function recordResponse(counts, name, itemCount) {
  const count = counts.get(name);
  count.responses++;
  count.items += itemCount;
  count.maxItems = Math.max(count.maxItems, itemCount);
}

async function runFixtureBenchmark(fixture, options) {
  const samples = new Map();
  const responseCounts = responseCountMap(smokeWorkloadNames);
  let violations = 0;
  for (const workload of fixtureWorkloads(fixture, options.pageSize)) {
    samples.set(workload.name, []);
  }
  for (let run = 0; run < options.runs; run++) {
    for (const workload of fixtureWorkloads(fixture, options.pageSize)) {
      const started = performance.now();
      const rows = workload.run();
      const elapsed = performance.now() - started;
      if (rows.length > options.pageSize) {
        fail(`${workload.name} returned more than ${options.pageSize} rows`);
      }
      recordResponse(responseCounts, workload.name, rows.length);
      violations += isolationViolations(rows, [fixture.isolationSentinel.target.id]);
      samples.get(workload.name).push(elapsed);
    }
  }
  return {samples, violations, responseCounts, isolationChecks: options.runs * smokeWorkloadNames.length};
}

function responseRows(payload, request) {
  const envelope = request.response?.envelope;
  if (envelope === 'items') {
    if (payload && !Array.isArray(payload) && Array.isArray(payload.items)) return payload.items;
    fail(`remote workload ${request.name} must return an object with an items array`);
  }
  if (['detail', 'comparison'].includes(envelope)) {
    if (payload && !Array.isArray(payload) && payload[envelope] && typeof payload[envelope] === 'object') {
      return [payload[envelope]];
    }
    fail(`remote workload ${request.name} must return an object with a ${envelope} object`);
  }
  if (request.name.endsWith('-detail') && payload && typeof payload === 'object' && !Array.isArray(payload)) {
    return [payload];
  }
  if (Array.isArray(payload)) return payload;
  if (payload && Array.isArray(payload.items)) return payload.items;
  fail('remote read-model response must be an array or contain an items array');
}

async function runRemoteBenchmark(fixture, options) {
  const requests = fixture.benchmark?.remoteRequests;
  assertArray(requests, 'fixture.benchmark.remoteRequests');
  if (requests.length === 0) {
    fail('fixture.benchmark.remoteRequests must not be empty');
  }
  if ((options.profile ?? 'smoke') === 'acceptance') {
    validateAcceptanceWorkloads(fixture, requests);
  }
  const token = process.env[options.authEnv];
  const headers = {accept: 'application/json'};
  if (token) {
    headers.authorization = `Bearer ${token}`;
  }
  const samples = new Map(requests.map((request) => [request.name, []]));
  const responseCounts = responseCountMap(requests.map((request) => request.name));
  const responseContracts = Object.fromEntries(
    requests.map((request) => [request.name, {responses: 0, validResponses: 0, issues: []}])
  );
  const metadataBindings = {responses: 0, validResponses: 0, issues: []};
  const expectedMetadata = {
    sourceCommit: options.sourceCommit ?? currentCommit(),
    buildVersion: options.buildVersion,
    artifactDigest: options.imageDigest,
  };
  let violations = 0;
  for (let run = 0; run < options.runs; run++) {
    for (const request of requests) {
      if (
        typeof request.name !== 'string' ||
        !/^[a-z0-9][a-z0-9-]*$/.test(request.name) ||
        !/^\/[A-Za-z0-9/?=&._~-]+$/.test(request.path ?? '') ||
        request.path.startsWith('//')
      ) {
        fail('remote benchmark request name or path is invalid');
      }
      const forbiddenResourceIDs = request.forbiddenResourceIds;
      if (
        !Array.isArray(forbiddenResourceIDs) ||
        forbiddenResourceIDs.length === 0 ||
        !forbiddenResourceIDs.every((id) => typeof id === 'string' && id !== '')
      ) {
        fail('remote benchmark request forbiddenResourceIds is invalid');
      }
      const url = new URL(request.path, options.baseURL);
      if (url.origin !== options.baseURL.origin) {
        fail('remote benchmark request name or path is invalid');
      }
      if (request.name.endsWith('-list')) {
        url.searchParams.set('limit', String(options.pageSize));
      }
      const started = performance.now();
      let response;
      try {
        response = await fetch(url, {
          headers,
          signal: AbortSignal.timeout(options.timeoutMs),
        });
      } catch {
        fail(`remote workload ${request.name} could not reach the configured base URL`);
      }
      if (!response.ok) {
        fail(`remote workload ${request.name} returned HTTP ${response.status}`);
      }
      if ((options.profile ?? 'smoke') === 'acceptance') {
        metadataBindings.responses++;
        const metadataIssues = [];
        for (const [field, expectedValue] of Object.entries(expectedMetadata)) {
          if (!expectedValue || response.headers.get(proofHeaders[field]) !== expectedValue) metadataIssues.push(field);
        }
        if (metadataIssues.length === 0) metadataBindings.validResponses++;
        else metadataBindings.issues.push(`${request.name}:${[...new Set(metadataIssues)].join(',')}`);
      }
      let payload;
      try {
        payload = await response.json();
      } catch {
        fail(`remote workload ${request.name} did not return JSON`);
      }
      const rows = responseRows(payload, request);
      if (rows.length > options.pageSize) {
        fail(`remote workload ${request.name} returned more than ${options.pageSize} rows`);
      }
      const contract = responseContracts[request.name];
      contract.responses++;
      const contractIssues = [];
      const minimumItems = request.response?.minimumItems ?? 0;
      if (rows.length < minimumItems) contractIssues.push(`minimumItems:${minimumItems}`);
      for (const requiredID of request.response?.requiredResourceIds ?? []) {
        if (isolationViolations([payload], [requiredID]) === 0) contractIssues.push(`requiredResourceId:${requiredID}`);
      }
      if (contractIssues.length === 0) contract.validResponses++;
      else contract.issues.push(...contractIssues);
      recordResponse(responseCounts, request.name, rows.length);
      violations += isolationViolations(rows, forbiddenResourceIDs);
      samples.get(request.name).push(performance.now() - started);
    }
  }
  return {
    samples,
    violations,
    responseCounts,
    responseContracts,
    metadataBindings,
    isolationChecks: options.runs * requests.length,
  };
}

function currentCommit() {
  try {
    const commit = execFileSync('git', ['rev-parse', 'HEAD'], {
      cwd: path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..'),
      encoding: 'utf8',
      stdio: ['ignore', 'pipe', 'ignore'],
    }).trim();
    return /^[a-f0-9]{40}$/.test(commit) ? commit : 'unknown';
  } catch {
    return 'unknown';
  }
}

function workingTreeDirty() {
  try {
    return (
      execFileSync('git', ['status', '--porcelain'], {
        cwd: path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..'),
        encoding: 'utf8',
        stdio: ['ignore', 'pipe', 'ignore'],
      }).trim().length > 0
    );
  } catch {
    return true;
  }
}

function reportMetadata(fixture, options) {
  const cpuRows = cpus();
  const sourceCommit = options.sourceCommit ?? currentCommit();
  const dirty = options.workingTreeDirty ?? workingTreeDirty();
  return {
    dataset: {
      fixtureSchema: fixture.schemaVersion,
      fixtureSeed: fixture.seed,
      fixtureSha256: `sha256:${createHash('sha256').update(JSON.stringify(fixture)).digest('hex')}`,
      targets: fixture.targets.length,
      placements: fixture.placements.length,
      onlineExecutors: fixture.agents.length,
      components: fixture.components.length,
      steps: fixture.steps.length,
    },
    hardware: {
      os: `${platform()} ${release()}`,
      architecture: process.arch,
      cpu: cpuRows[0]?.model ?? 'unknown',
      logicalCores: cpuRows.length,
      memoryBytes: totalmem(),
    },
    image: {
      digest: options.imageDigest ?? null,
    },
    build: {
      version: options.buildVersion ?? 'unknown',
      artifactDigest: options.imageDigest ?? null,
      commit: sourceCommit,
      workingTreeDirty: dirty,
      harnessSha256: `sha256:${createHash('sha256')
        .update(readFileSync(fileURLToPath(import.meta.url)))
        .digest('hex')}`,
      nodeVersion: process.version,
    },
  };
}

function rawSampleSeries(samples) {
  const series = {};
  for (const [name, values] of samples) {
    const retained = values.map(rounded);
    series[`${name}-p95-ms`] = retained;
    series[`${name}-p99-ms`] = [...retained];
  }
  return {series};
}

function responseCountObject(responseCounts) {
  return Object.fromEntries(responseCounts);
}

export async function benchmark(fixture, options) {
  const profile = options.profile ?? 'smoke';
  if (profile === 'acceptance' && !options.baseURL) {
    fail('--profile acceptance requires --base-url');
  }
  const outcome = options.baseURL
    ? await runRemoteBenchmark(fixture, options)
    : await runFixtureBenchmark(fixture, options);
  if (outcome.violations !== 0) {
    fail(`tenant isolation check found ${outcome.violations} foreign rows`);
  }
  const workloads = [...outcome.samples.entries()].map(([name, samples]) => metrics(name, samples));
  const aggregateSamples = [...outcome.samples.values()].flat();
  const aggregate = metrics('aggregate', aggregateSamples);
  for (const metric of workloads) {
    if (metric.p95Ms > options.thresholds.p95Ms || metric.p99Ms > options.thresholds.p99Ms) {
      fail(`${metric.name} exceeded benchmark thresholds: p95=${metric.p95Ms}ms p99=${metric.p99Ms}ms`);
    }
  }
  const responseCounts = responseCountObject(outcome.responseCounts);
  const maxResponseItems = Math.max(...Object.values(responseCounts).map((count) => count.maxItems));
  const metadata = reportMetadata(fixture, options);
  const blockers = [];
  if (profile !== 'acceptance') {
    blockers.push(acceptanceBlocker);
  } else {
    if (options.pageSize !== 100) blockers.push('AC-50 acceptance requires page size 100');
    if (options.runs < 20) blockers.push('AC-50 acceptance requires at least 20 samples per workload');
    if (options.thresholds.p95Ms > 2000 || options.thresholds.p99Ms > 5000) {
      blockers.push('AC-50 acceptance thresholds may not exceed p95 2000 ms and p99 5000 ms');
    }
    if (!process.env[options.authEnv]) {
      blockers.push('AC-50 acceptance requires environment-only bearer authentication');
    }
    if (
      metadata.build.commit === 'unknown' ||
      metadata.build.workingTreeDirty ||
      metadata.build.version === 'unknown' ||
      !/^sha256:[0-9a-f]{64}$/.test(metadata.build.artifactDigest ?? '')
    ) {
      blockers.push(
        'AC-50 acceptance requires a clean known source commit, build version, and immutable artifact digest'
      );
    }
    const responseContractFailures = Object.entries(outcome.responseContracts ?? {})
      .filter(([, contract]) => contract.responses !== options.runs || contract.validResponses !== options.runs)
      .map(([name]) => name);
    if (responseContractFailures.length > 0) {
      blockers.push(`AC-50 response contracts failed for: ${responseContractFailures.join(', ')}`);
    }
    const expectedResponseCount = options.runs * workloadNames.length;
    if (
      outcome.metadataBindings?.responses !== expectedResponseCount ||
      outcome.metadataBindings?.validResponses !== expectedResponseCount
    ) {
      blockers.push(
        'AC-50 live responses are not bound to the measured source commit, build version, and artifact digest'
      );
    }
    if (maxResponseItems <= 0) blockers.push('AC-50 acceptance requires non-empty bounded responses');
  }
  return {
    schemaVersion: reportSchema,
    fixtureSchema: fixture.schemaVersion,
    fixtureSeed: fixture.seed,
    mode: options.baseURL ? 'remote' : 'fixture',
    runs: options.runs,
    pageSize: options.pageSize,
    samples: aggregateSamples.length,
    thresholds: options.thresholds,
    isolationViolations: outcome.violations,
    qualification: {
      profile,
      acceptanceEligible: profile === 'acceptance' && blockers.length === 0,
      blockers,
    },
    ...metadata,
    facts: {
      pageSize: options.pageSize,
      boundedResponses: true,
      workloads: [...outcome.samples.keys()],
      maxResponseItems,
      responseCounts,
      responseContracts: outcome.responseContracts ?? {},
      metadataBindings: outcome.metadataBindings ?? {responses: 0, validResponses: 0, issues: []},
      isolation: {
        checks: outcome.isolationChecks,
        violations: outcome.violations,
      },
    },
    rawSamples: rawSampleSeries(outcome.samples),
    aggregate,
    workloads,
  };
}

async function main() {
  const options = parseBenchmarkArgs(process.argv.slice(2));
  let fixture;
  try {
    fixture = JSON.parse(await readFile(options.fixture, 'utf8'));
  } catch {
    fail('fixture must be readable valid JSON');
  }
  validateFixture(fixture, options);
  const report = await benchmark(fixture, options);
  process.stdout.write(`${JSON.stringify(report, null, 2)}\n`);
}

const invokedPath = process.argv[1] ? path.resolve(process.argv[1]) : '';
if (invokedPath === fileURLToPath(import.meta.url)) {
  main().catch((error) => {
    process.stderr.write(`${error.message}\n`);
    process.exitCode = 1;
  });
}
