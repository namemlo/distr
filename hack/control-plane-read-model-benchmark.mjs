#!/usr/bin/env node

import {readFile} from 'node:fs/promises';
import path from 'node:path';
import {performance} from 'node:perf_hooks';
import {fileURLToPath} from 'node:url';

const fixtureSchema = 'distr.control-plane-scale-fixture/v1';
const reportSchema = 'distr.control-plane-read-model-benchmark/v1';

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
  return {
    fixture: path.resolve(values.get('--fixture')),
    runs: integerOption(values.get('--runs') ?? '20', '--runs', 20, 10000),
    pageSize: integerOption(values.get('--page-size') ?? '100', '--page-size', 1, 100),
    thresholds: {p95Ms, p99Ms},
    baseURL,
    authEnv,
    timeoutMs: integerOption(values.get('--timeout-ms') ?? '10000', '--timeout-ms', 1, 300000),
  };
}

function assertArray(value, label) {
  if (!Array.isArray(value)) {
    fail(`${label} must be an array`);
  }
}

export function validateFixture(fixture) {
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
  const expectedSentinels = new Map([
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
    if (expectedSentinels.has(request.name) && !expectedSentinel) {
      fail(`fixture benchmark request ${request.name} is missing its response-visible sentinel resource`);
    }
    if (expectedSentinel && !request.forbiddenResourceIds.includes(expectedSentinel)) {
      fail(`fixture benchmark request ${request.name} must include its response-visible sentinel ID`);
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

async function runFixtureBenchmark(fixture, options) {
  const samples = new Map();
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
      violations += isolationViolations(rows, [fixture.isolationSentinel.target.id]);
      samples.get(workload.name).push(elapsed);
    }
  }
  return {samples, violations};
}

function responseRows(payload) {
  if (Array.isArray(payload)) {
    return payload;
  }
  if (payload && Array.isArray(payload.items)) {
    return payload.items;
  }
  fail('remote read-model response must be an array or contain an items array');
}

async function runRemoteBenchmark(fixture, options) {
  const requests = fixture.benchmark?.remoteRequests;
  assertArray(requests, 'fixture.benchmark.remoteRequests');
  if (requests.length === 0) {
    fail('fixture.benchmark.remoteRequests must not be empty');
  }
  const token = process.env[options.authEnv];
  const headers = {accept: 'application/json'};
  if (token) {
    headers.authorization = `Bearer ${token}`;
  }
  const samples = new Map(requests.map((request) => [request.name, []]));
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
      url.searchParams.set('limit', String(options.pageSize));
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
      let payload;
      try {
        payload = await response.json();
      } catch {
        fail(`remote workload ${request.name} did not return JSON`);
      }
      const rows = responseRows(payload);
      if (rows.length > options.pageSize) {
        fail(`remote workload ${request.name} returned more than ${options.pageSize} rows`);
      }
      violations += isolationViolations(rows, forbiddenResourceIDs);
      samples.get(request.name).push(performance.now() - started);
    }
  }
  return {samples, violations};
}

export async function benchmark(fixture, options) {
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
  validateFixture(fixture);
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
