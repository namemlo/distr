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
const reportSchema = 'distr.control-plane-load-proof/v1';
const mebibyte = 1024 * 1024;
const repoRoot = path.resolve(new URL('..', import.meta.url).pathname.slice(process.platform === 'win32' ? 1 : 0));

function fail(message) {
  throw new Error(message);
}

function parsePositiveInteger(value, option, maximum = Number.MAX_SAFE_INTEGER) {
  if (!/^\d+$/.test(value ?? '')) {
    fail(`${option} must be a positive integer`);
  }
  const parsed = Number(value);
  if (!Number.isSafeInteger(parsed) || parsed < 1 || parsed > maximum) {
    fail(`${option} must be a positive integer`);
  }
  return parsed;
}

function parseDuration(value) {
  const match = /^(\d+)(s|m)$/.exec(value ?? '');
  if (!match) {
    fail('--duration must use a positive whole number of seconds or minutes');
  }
  const amount = parsePositiveInteger(match[1], '--duration', 3600);
  return amount * (match[2] === 'm' ? 60 : 1);
}

export function parseLoadArgs(argv) {
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
  for (const option of values.keys()) {
    if (
      !new Set([
        '--fixture',
        '--duration',
        '--rate',
        '--base-url',
        '--auth-env',
        '--timeout-ms',
        '--build-version',
        '--artifact-digest',
      ]).has(option)
    ) {
      fail(`unknown option ${option}`);
    }
  }
  if (!values.get('--fixture')?.trim()) {
    fail('--fixture is required');
  }
  let baseURL;
  if (values.has('--base-url')) {
    try {
      baseURL = new URL(values.get('--base-url'));
    } catch {
      fail('--base-url must be a valid HTTP or HTTPS URL');
    }
    if (!['http:', 'https:'].includes(baseURL.protocol)) {
      fail('--base-url must be a valid HTTP or HTTPS URL');
    }
    if (baseURL.username || baseURL.password || baseURL.search || baseURL.hash) {
      fail('--base-url must not contain credentials, a query, or a fragment');
    }
    if (!['localhost', '127.0.0.1', '[::1]'].includes(baseURL.hostname.toLowerCase())) {
      fail('--base-url must use a loopback host');
    }
  }
  const authEnv = values.get('--auth-env') ?? 'CONTROL_PLANE_LOAD_TEST_TOKEN';
  if (!/^[A-Z_][A-Z0-9_]*$/.test(authEnv)) {
    fail('--auth-env must be an uppercase environment-variable name');
  }
  if (baseURL && !process.env[authEnv]) {
    fail(`remote mode requires authorization from environment variable ${authEnv}`);
  }
  const buildVersion = values.get('--build-version');
  if (buildVersion !== undefined && !/^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$/.test(buildVersion)) {
    fail('--build-version must be a non-secret build identifier');
  }
  const artifactDigest = values.get('--artifact-digest');
  if (artifactDigest !== undefined && !/^sha256:[0-9a-f]{64}$/.test(artifactDigest)) {
    fail('--artifact-digest must be a lowercase SHA-256 digest');
  }
  return {
    fixture: path.resolve(values.get('--fixture')),
    durationSeconds: parseDuration(values.get('--duration') ?? '10m'),
    rate: parsePositiveInteger(values.get('--rate') ?? '100', '--rate', 10_000),
    baseURL,
    authEnv,
    timeoutMs: parsePositiveInteger(values.get('--timeout-ms') ?? '10000', '--timeout-ms', 300_000),
    buildVersion,
    artifactDigest,
  };
}

function percentile(samples, quantile) {
  if (samples.length === 0) {
    fail('cannot calculate a percentile without samples');
  }
  const sorted = samples.toSorted((left, right) => left - right);
  return sorted[Math.max(0, Math.ceil(quantile * sorted.length) - 1)];
}

function rounded(value) {
  return Number(value.toFixed(3));
}

function metrics(samples) {
  return {
    samples: samples.length,
    p50Ms: rounded(percentile(samples, 0.5)),
    p95Ms: rounded(percentile(samples, 0.95)),
    p99Ms: rounded(percentile(samples, 0.99)),
  };
}

function validateFixture(fixture) {
  if (!fixture || fixture.schemaVersion !== fixtureSchema) {
    fail(`fixture schema must be ${fixtureSchema}`);
  }
  for (const [field, minimum] of Object.entries({
    targets: 1000,
    placements: 649,
    agents: 100,
    components: 100,
    steps: 500,
  })) {
    if (!Array.isArray(fixture[field]) || fixture[field].length < minimum) {
      fail(`fixture.${field} must contain at least ${minimum} rows`);
    }
    const requestedCount = fixture.parameters?.[field];
    if (
      !Number.isSafeInteger(requestedCount) ||
      (field === 'components' && fixture[field].length < requestedCount) ||
      (field !== 'components' && fixture[field].length !== requestedCount)
    ) {
      fail(`fixture.${field} count does not match parameters.${field}`);
    }
  }
  const members = fixture.campaign?.waves?.[0]?.members;
  if (!Array.isArray(members) || members.length !== 500) {
    fail('fixture campaign reference wave must contain exactly 500 members');
  }
  if (!fixture.primaryOrganization?.id || !fixture.isolationSentinel?.organization?.id) {
    fail('fixture must contain primary and isolation-sentinel organizations');
  }
  for (const [pathLabel, expected] of Object.entries({
    'planning.componentCount': 100,
    'planning.runs': 5,
    'wave.stepCount': 500,
    'wave.runs': 2,
    'events.durationSeconds': 600,
    'events.ratePerSecond': 100,
    'events.concurrentAgents': 100,
    'logs.totalBytes': 100 * mebibyte,
    'logs.maximumPageBytes': mebibyte,
    'thresholds.planningP95Ms': 10_000,
    'thresholds.waveMaximumMs': 30_000,
    'thresholds.eventAcknowledgementP95Ms': 1_000,
    'thresholds.logFirstPageMs': 2_000,
    'thresholds.maximumCrossOrganizationRecords': 0,
    'thresholds.maximumNonPolicyErrorRateExclusive': 0.01,
  })) {
    const actual = pathLabel.split('.').reduce((value, key) => value?.[key], fixture.loadProof);
    if (actual !== expected) {
      fail(`fixture.loadProof.${pathLabel} must equal ${expected}`);
    }
  }
  return fixture;
}

function stablePlanningChecksum(fixture) {
  const planningInput = fixture.components
    .slice(0, 100)
    .map(({id, key, releaseDigest}) => ({id, key, releaseDigest}))
    .toSorted((left, right) => left.id.localeCompare(right.id));
  return `sha256:${createHash('sha256').update(JSON.stringify(planningInput)).digest('hex')}`;
}

function waveOrderChecksum(members) {
  const order = members.map(({id, order, planId, targetId}) => ({id, order, planId, targetId}));
  return `sha256:${createHash('sha256').update(JSON.stringify(order)).digest('hex')}`;
}

function identitySetChecksum(ids) {
  return `sha256:${createHash('sha256').update(JSON.stringify([...ids].sort())).digest('hex')}`;
}

function currentCommit() {
  try {
    const commit = execFileSync('git', ['rev-parse', 'HEAD'], {
      cwd: repoRoot,
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
        cwd: repoRoot,
        encoding: 'utf8',
        stdio: ['ignore', 'pipe', 'ignore'],
      }).trim().length > 0
    );
  } catch {
    return true;
  }
}

function reportMetadata(fixture, fixtureBytes, options) {
  const cpuRows = cpus();
  return {
    hardware: {
      platform: platform(),
      operatingSystemRelease: release(),
      architecture: process.arch,
      cpuModel: cpuRows[0]?.model ?? 'unknown',
      logicalCpuCount: cpuRows.length,
      totalMemoryBytes: totalmem(),
    },
    build: {
      version: options.buildVersion ?? 'unknown',
      artifactDigest: options.artifactDigest ?? null,
      commit: options.sourceCommit ?? currentCommit(),
      workingTreeDirty: options.workingTreeDirty ?? workingTreeDirty(),
      harnessSha256: createHash('sha256')
        .update(readFileSync(fileURLToPath(import.meta.url)))
        .digest('hex'),
      nodeVersion: process.version,
    },
    dataset: {
      fixtureSchema: fixture.schemaVersion,
      fixtureSeed: fixture.seed,
      fixtureSha256: createHash('sha256').update(fixtureBytes).digest('hex'),
      fixtureSizeBytes: fixtureBytes.length,
      parameters: {...fixture.parameters},
    },
  };
}

function acceptanceProfile(fixture, options) {
  const required = {
    durationSeconds: fixture.loadProof.events.durationSeconds,
    ratePerSecond: fixture.loadProof.events.ratePerSecond,
    concurrentAgents: fixture.loadProof.events.concurrentAgents,
  };
  return {
    required,
    met: options.durationSeconds >= required.durationSeconds && options.rate >= required.ratePerSecond,
  };
}

function simulatedSamples(fixture, options) {
  const planningChecksum = stablePlanningChecksum(fixture);
  const planningChecksums = Array.from({length: 5}, () => stablePlanningChecksum(fixture));
  const planning = planningChecksums.map((_, index) =>
    rounded(4.8 + fixture.components.length * 0.018 + index * 0.061)
  );

  const memberIDs = fixture.campaign.waves[0].members.map((member) => member.id);
  const uniqueMemberIDs = new Set(memberIDs);
  const stableOrder = fixture.campaign.waves[0].members.every((member, index) => member.order === index + 1);
  const wave = Array.from({length: fixture.loadProof.wave.runs}, (_, index) =>
    rounded(8.5 + memberIDs.length * 0.024 + index * 0.013)
  );
  const waveOrderChecksums = Array.from({length: fixture.loadProof.wave.runs}, () =>
    waveOrderChecksum(fixture.campaign.waves[0].members)
  );

  const requestedEvents = options.durationSeconds * options.rate;
  const eventAcknowledgements = Array.from({length: requestedEvents}, (_, index) => rounded(4 + (index % 97) * 0.017));

  const logPageCount = 100;
  const logFirstPage = [7.125];

  return {
    planningChecksum,
    planningChecksums,
    waveOrderChecksums,
    stableOrder,
    duplicateAdmissions: memberIDs.length - uniqueMemberIDs.size,
    requestedEvents,
    logPageCount,
    rawSamples: {
      planning,
      wave,
      eventAcknowledgements,
      logFirstPage,
      isolationChecks: [0],
      nonPolicyErrors: [0],
    },
  };
}

export function evaluateSimulationAcceptance(evidence, thresholds) {
  return (
    evidence.acceptanceProfileMet &&
    evidence.planningP95Ms <= thresholds.planningP95Ms &&
    evidence.deterministicPlanningChecksum &&
    evidence.waveP99Ms <= thresholds.waveMaximumMs &&
    evidence.stableWaveOrder &&
    evidence.duplicateAdmissions === 0 &&
    evidence.eventAcknowledgementP95Ms <= thresholds.eventAcknowledgementP95Ms &&
    evidence.logFirstPageP95Ms <= thresholds.logFirstPageMs &&
    evidence.crossOrganizationRecords <= thresholds.maximumCrossOrganizationRecords &&
    evidence.nonPolicyRate < thresholds.maximumNonPolicyErrorRateExclusive
  );
}

export function simulateLoadProof(fixture, options, fixtureBytes) {
  const startedAt = performance.now();
  validateFixture(fixture);
  const outcome = simulatedSamples(fixture, options);
  const planningMetrics = metrics(outcome.rawSamples.planning);
  const waveMetrics = metrics(outcome.rawSamples.wave);
  const eventMetrics = metrics(outcome.rawSamples.eventAcknowledgements);
  const logMetrics = metrics(outcome.rawSamples.logFirstPage);
  const nonPolicyErrorCount = outcome.rawSamples.nonPolicyErrors.reduce((sum, sample) => sum + sample, 0);
  const totalOperations =
    outcome.rawSamples.planning.length +
    outcome.rawSamples.wave.length +
    outcome.rawSamples.eventAcknowledgements.length +
    outcome.logPageCount;
  const nonPolicyRate = nonPolicyErrorCount / totalOperations;
  const crossOrganizationRecords = outcome.rawSamples.isolationChecks.reduce((sum, sample) => sum + sample, 0);
  const profile = acceptanceProfile(fixture, options);
  const passed = evaluateSimulationAcceptance(
    {
      acceptanceProfileMet: profile.met,
      planningP95Ms: planningMetrics.p95Ms,
      deterministicPlanningChecksum: new Set(outcome.planningChecksums).size === 1,
      waveP99Ms: waveMetrics.p99Ms,
      stableWaveOrder: outcome.stableOrder,
      duplicateAdmissions: outcome.duplicateAdmissions,
      eventAcknowledgementP95Ms: eventMetrics.p95Ms,
      logFirstPageP95Ms: logMetrics.p95Ms,
      crossOrganizationRecords,
      nonPolicyRate,
    },
    fixture.loadProof.thresholds
  );
  const wallClockDurationSeconds = Math.max(0.000001, (performance.now() - startedAt) / 1000);

  return {
    schemaVersion: reportSchema,
    mode: 'simulation',
    measurement: 'deterministic-simulation',
    passed,
    acceptanceEligible: false,
    qualification: {
      blockers: ['AC-51 acceptance requires an authenticated measured-live run without time compression'],
    },
    acceptanceProfile: profile,
    timeCompression: {
      applied: true,
      requestedDurationSeconds: options.durationSeconds,
      wallClockDurationSeconds: rounded(wallClockDurationSeconds),
      ratio: rounded(options.durationSeconds / wallClockDurationSeconds),
    },
    ...reportMetadata(fixture, fixtureBytes, options),
    environment: {
      database: {mode: 'fixture', sizeBytes: fixtureBytes.length},
      network: 'in-process',
      concurrency: fixture.loadProof.events.concurrentAgents,
      warmState: 'warm-simulated',
    },
    thresholds: {...fixture.loadProof.thresholds},
    scenarios: {
      planning: {
        ...planningMetrics,
        components: fixture.loadProof.planning.componentCount,
        runs: fixture.loadProof.planning.runs,
        checksum: outcome.planningChecksum,
        checksums: outcome.planningChecksums,
        deterministicChecksum: new Set(outcome.planningChecksums).size === 1,
      },
      wave: {
        ...waveMetrics,
        steps: fixture.campaign.waves[0].members.length,
        stableOrder: outcome.stableOrder,
        duplicateAdmissions: outcome.duplicateAdmissions,
        orderChecksums: outcome.waveOrderChecksums,
        deterministicOrderChecksum: new Set(outcome.waveOrderChecksums).size === 1,
      },
      events: {
        ...eventMetrics,
        targetRatePerSecond: options.rate,
        concurrentAgents: fixture.loadProof.events.concurrentAgents,
        requestedDurationSeconds: options.durationSeconds,
        requestedEvents: outcome.requestedEvents,
        acceptedEvents: outcome.requestedEvents,
        acknowledgedEvents: outcome.requestedEvents,
        lostAcceptedEvents: 0,
        authentication: 'simulated',
        authenticatedExecutorIds: [],
        authenticatedExecutorIdsChecksum: identitySetChecksum([]),
      },
      logs: {
        ...logMetrics,
        totalBytes: fixture.loadProof.logs.totalBytes,
        pageCount: outcome.logPageCount,
        maximumPageBytes: fixture.loadProof.logs.maximumPageBytes,
        firstPageMs: logMetrics.p99Ms,
        materialization: 'virtual-bounded-pages',
        peakBufferBytes: fixture.loadProof.logs.maximumPageBytes,
        streamingBoundedMemory: true,
      },
      isolation: {
        checks: outcome.rawSamples.isolationChecks.length,
        crossOrganizationRecords,
      },
      errors: {
        operations: totalOperations,
        nonPolicyErrors: nonPolicyErrorCount,
        nonPolicyRate,
      },
    },
    rawSamples: outcome.rawSamples,
  };
}

function remoteURL(baseURL, requestPath) {
  if (typeof requestPath !== 'string' || !requestPath.startsWith('/') || requestPath.startsWith('//')) {
    fail('fixture load-proof remote path must be same-origin relative');
  }
  const url = new URL(requestPath, baseURL);
  if (url.origin !== baseURL.origin) {
    fail('fixture load-proof remote path must be same-origin relative');
  }
  return url;
}

async function readBoundedBody(response, maximumBytes) {
  if (!response.body) {
    fail('remote log page did not return a body');
  }
  let bytes = 0;
  for await (const chunk of response.body) {
    bytes += chunk.byteLength;
    if (bytes > maximumBytes) {
      fail(`remote log page exceeded the ${maximumBytes}-byte bound`);
    }
  }
  return bytes;
}

function containsForbiddenID(value, forbiddenIDs) {
  if (typeof value === 'string') return forbiddenIDs.has(value);
  if (Array.isArray(value)) return value.some((item) => containsForbiddenID(item, forbiddenIDs));
  return Boolean(
    value && typeof value === 'object' && Object.values(value).some((item) => containsForbiddenID(item, forbiddenIDs))
  );
}

function sleep(milliseconds) {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}

async function runRemoteLoadProof(fixture, options, fixtureBytes) {
  validateFixture(fixture);
  const token = process.env[options.authEnv];
  if (!token) {
    fail(`remote mode requires authorization from environment variable ${options.authEnv}`);
  }
  const headers = {
    accept: 'application/json',
    authorization: `Bearer ${token}`,
  };
  const startedAt = performance.now();
  let operations = 0;
  let nonPolicyErrors = 0;
  let policyErrors = 0;
  const request = async (requestPath, init = {}) => {
    const url = remoteURL(options.baseURL, requestPath);
    let response;
    try {
      response = await fetch(url, {
        ...init,
        headers: {...headers, ...init.headers},
        redirect: 'error',
        signal: AbortSignal.timeout(options.timeoutMs),
      });
    } catch {
      fail('remote load-proof request could not reach the configured loopback origin');
    }
    operations += 1;
    if (!response.ok) {
      if (response.status >= 500) nonPolicyErrors += 1;
      else policyErrors += 1;
    }
    return response;
  };
  const requestJSON = async (requestPath, init = {}) => {
    const response = await request(requestPath, init);
    if (!response.ok) return {response};
    try {
      return {response, payload: await response.json()};
    } catch {
      fail('remote load-proof endpoint did not return valid JSON');
    }
  };

  const planningSamples = [];
  const planningChecksums = [];
  for (let run = 0; run < fixture.loadProof.planning.runs; run++) {
    const started = performance.now();
    const {payload} = await requestJSON(fixture.loadProof.remote.planningPath, {
      method: 'POST',
      headers: {'content-type': 'application/json'},
      body: JSON.stringify({
        run,
        components: fixture.components.slice(0, fixture.loadProof.planning.componentCount),
      }),
    });
    planningSamples.push(performance.now() - started);
    if (typeof payload?.checksum === 'string' && payload.checksum !== '') {
      planningChecksums.push(payload.checksum);
    }
  }

  const waveSamples = [];
  const wavePayloads = [];
  for (let run = 0; run < fixture.loadProof.wave.runs; run++) {
    const waveStarted = performance.now();
    const {payload} = await requestJSON(fixture.loadProof.remote.wavePath, {
      method: 'POST',
      headers: {'content-type': 'application/json'},
      body: JSON.stringify({run, members: fixture.campaign.waves[0].members}),
    });
    waveSamples.push(performance.now() - waveStarted);
    wavePayloads.push(payload);
  }

  const requestedEvents = options.durationSeconds * options.rate;
  const eventSamples = [];
  const acceptedEventIDs = new Set();
  const acknowledgedEventIDs = new Set();
  const authenticatedExecutorIDs = new Set();
  const eventWindowStarted = performance.now();
  let eventIndex = 0;
  for (let second = 0; second < options.durationSeconds; second++) {
    const scheduledAt = eventWindowStarted + second * 1000;
    const waitMilliseconds = scheduledAt - performance.now();
    if (waitMilliseconds > 0) {
      await sleep(waitMilliseconds);
    }
    const batch = [];
    for (let slot = 0; slot < options.rate; slot++) {
      const index = eventIndex++;
      const eventID = `load-event-${String(index + 1).padStart(8, '0')}`;
      const agentID = fixture.agents[index % fixture.agents.length].id;
      const eventStarted = performance.now();
      batch.push(
        requestJSON(fixture.loadProof.remote.eventPath, {
          method: 'POST',
          headers: {'content-type': 'application/json'},
          body: JSON.stringify({eventId: eventID, agentId: agentID, sequence: index + 1}),
        }).then(({payload}) => {
          eventSamples.push(performance.now() - eventStarted);
          if (payload?.accepted === true) acceptedEventIDs.add(eventID);
          if (payload?.accepted === true && payload.eventId === eventID) acknowledgedEventIDs.add(eventID);
          if (payload?.accepted === true && payload.agentId === agentID) authenticatedExecutorIDs.add(agentID);
        })
      );
    }
    await Promise.all(batch);
  }
  const remainingEventWindow = eventWindowStarted + options.durationSeconds * 1000 - performance.now();
  if (remainingEventWindow > 0) {
    await sleep(remainingEventWindow);
  }
  const eventWallClockDurationSeconds = (performance.now() - eventWindowStarted) / 1000;
  const eventSchedulingOverrunMs = Math.max(0, (eventWallClockDurationSeconds - options.durationSeconds) * 1000);

  const logSamples = [];
  let logBytes = 0;
  let maximumPageBytes = 0;
  let logPeakBufferBytes = 0;
  let boundedLogBufferProof = true;
  let page = 0;
  const baseLogURL = remoteURL(options.baseURL, fixture.loadProof.remote.logPath);
  while (logBytes < fixture.loadProof.logs.totalBytes) {
    const logStarted = performance.now();
    const logURL = new URL(baseLogURL);
    logURL.searchParams.set('page', String(page));
    const response = await request(`${logURL.pathname}${logURL.search}`);
    if (!response.ok) break;
    const declaredPeakBufferBytes = Number(response.headers.get('x-distr-proof-peak-buffer-bytes'));
    if (!Number.isSafeInteger(declaredPeakBufferBytes) || declaredPeakBufferBytes <= 0) {
      boundedLogBufferProof = false;
    } else {
      logPeakBufferBytes = Math.max(logPeakBufferBytes, declaredPeakBufferBytes);
    }
    const pageBytes = await readBoundedBody(response, fixture.loadProof.logs.maximumPageBytes);
    logSamples.push(performance.now() - logStarted);
    if (pageBytes === 0) {
      fail('remote log page must not be empty before the declared total is reached');
    }
    logBytes += pageBytes;
    maximumPageBytes = Math.max(maximumPageBytes, pageBytes);
    const nextPage = response.headers.get('x-next-page');
    if (logBytes < fixture.loadProof.logs.totalBytes && nextPage !== String(page + 1)) {
      fail('remote log paging ended before the declared 100 MiB total');
    }
    page += 1;
  }
  if (logBytes > fixture.loadProof.logs.totalBytes) {
    fail('remote log paging exceeded the declared 100 MiB total');
  }

  const isolationSamples = [];
  let crossOrganizationRecords = 0;
  for (const isolationRequest of fixture.benchmark.remoteRequests) {
    if (!Array.isArray(isolationRequest.forbiddenResourceIds) || isolationRequest.forbiddenResourceIds.length === 0) {
      fail('fixture benchmark isolation request must define forbiddenResourceIds');
    }
    const isolationStarted = performance.now();
    const {payload} = await requestJSON(isolationRequest.path);
    isolationSamples.push(performance.now() - isolationStarted);
    if (payload !== undefined && containsForbiddenID(payload, new Set(isolationRequest.forbiddenResourceIds))) {
      crossOrganizationRecords += 1;
    }
  }

  const planningMetrics = metrics(planningSamples);
  const waveMetrics = metrics(waveSamples);
  const eventMetrics = metrics(eventSamples);
  const logMetrics = metrics(logSamples);
  const nonPolicyRate = operations === 0 ? 1 : nonPolicyErrors / operations;
  const deterministicChecksum =
    planningChecksums.length === fixture.loadProof.planning.runs && new Set(planningChecksums).size === 1;
  const acknowledgedEvents = acknowledgedEventIDs.size;
  const acceptedEvents = acceptedEventIDs.size;
  const lostAcceptedEvents = [...acceptedEventIDs].filter((eventID) => !acknowledgedEventIDs.has(eventID)).length;
  const waveOrderChecksums = wavePayloads
    .map((payload) => payload?.orderChecksum)
    .filter((checksum) => typeof checksum === 'string');
  const stableOrder = wavePayloads.every(
    (payload) => payload?.stepCount === fixture.loadProof.wave.stepCount && payload?.stableOrder === true
  );
  const duplicateAdmissions = wavePayloads.every((payload) => payload?.duplicateAdmissions === 0)
    ? 0
    : fixture.loadProof.wave.stepCount;
  const profile = acceptanceProfile(fixture, options);
  const passed =
    profile.met &&
    planningMetrics.p95Ms <= fixture.loadProof.thresholds.planningP95Ms &&
    deterministicChecksum &&
    waveMetrics.p99Ms <= fixture.loadProof.thresholds.waveMaximumMs &&
    stableOrder &&
    duplicateAdmissions === 0 &&
    waveOrderChecksums.length === fixture.loadProof.wave.runs &&
    new Set(waveOrderChecksums).size === 1 &&
    eventMetrics.p95Ms <= fixture.loadProof.thresholds.eventAcknowledgementP95Ms &&
    acceptedEvents === requestedEvents &&
    acknowledgedEvents === acceptedEvents &&
    lostAcceptedEvents === 0 &&
    authenticatedExecutorIDs.size === fixture.loadProof.events.concurrentAgents &&
    eventSchedulingOverrunMs <= fixture.loadProof.thresholds.eventAcknowledgementP95Ms &&
    logBytes === fixture.loadProof.logs.totalBytes &&
    logSamples[0] <= fixture.loadProof.thresholds.logFirstPageMs &&
    boundedLogBufferProof &&
    logPeakBufferBytes > 0 &&
    logPeakBufferBytes < fixture.loadProof.logs.totalBytes &&
    crossOrganizationRecords === 0 &&
    policyErrors === 0 &&
    nonPolicyRate < fixture.loadProof.thresholds.maximumNonPolicyErrorRateExclusive;

  const qualificationBlockers = [];
  if (!passed) qualificationBlockers.push('AC-51 measured workload did not satisfy every section 20.9 threshold');
  if (
    !/^[a-f0-9]{40}$/.test(options.sourceCommit ?? currentCommit()) ||
    (options.workingTreeDirty ?? workingTreeDirty()) ||
    !/^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$/.test(options.buildVersion ?? '') ||
    !/^sha256:[0-9a-f]{64}$/.test(options.artifactDigest ?? '')
  ) {
    qualificationBlockers.push(
      'AC-51 acceptance requires a clean known source commit, build version, and artifact digest'
    );
  }
  const acceptanceEligible = qualificationBlockers.length === 0;

  return {
    schemaVersion: reportSchema,
    mode: 'remote',
    measurement: 'measured-live',
    passed,
    acceptanceEligible,
    qualification: {blockers: qualificationBlockers},
    acceptanceProfile: profile,
    timeCompression: {
      applied: false,
      requestedDurationSeconds: options.durationSeconds,
      wallClockDurationSeconds: rounded((performance.now() - startedAt) / 1000),
    },
    ...reportMetadata(fixture, fixtureBytes, options),
    environment: {
      database: {mode: 'remote', sizeBytes: null},
      network: `loopback:${options.baseURL.protocol}`,
      concurrency: fixture.loadProof.events.concurrentAgents,
      warmState: 'caller-managed',
    },
    thresholds: {...fixture.loadProof.thresholds},
    scenarios: {
      planning: {
        ...planningMetrics,
        components: fixture.loadProof.planning.componentCount,
        runs: fixture.loadProof.planning.runs,
        checksum: planningChecksums[0] ?? null,
        checksums: planningChecksums,
        deterministicChecksum,
      },
      wave: {
        ...waveMetrics,
        steps: fixture.loadProof.wave.stepCount,
        stableOrder,
        duplicateAdmissions,
        orderChecksums: waveOrderChecksums,
        deterministicOrderChecksum:
          waveOrderChecksums.length === fixture.loadProof.wave.runs && new Set(waveOrderChecksums).size === 1,
      },
      events: {
        ...eventMetrics,
        targetRatePerSecond: options.rate,
        concurrentAgents: fixture.loadProof.events.concurrentAgents,
        requestedDurationSeconds: options.durationSeconds,
        wallClockDurationSeconds: rounded(eventWallClockDurationSeconds),
        schedulingOverrunMs: rounded(eventSchedulingOverrunMs),
        requestedEvents,
        acceptedEvents,
        acknowledgedEvents,
        lostAcceptedEvents,
        authentication: 'authenticated-live',
        authenticatedExecutorIds: [...authenticatedExecutorIDs].sort(),
        authenticatedExecutorIdsChecksum: identitySetChecksum(authenticatedExecutorIDs),
      },
      logs: {
        ...logMetrics,
        totalBytes: logBytes,
        pageCount: logSamples.length,
        maximumPageBytes,
        firstPageMs: rounded(logSamples[0]),
        materialization: 'remote-bounded-pages',
        peakBufferBytes: logPeakBufferBytes,
        streamingBoundedMemory: boundedLogBufferProof && logPeakBufferBytes < fixture.loadProof.logs.totalBytes,
      },
      isolation: {
        checks: isolationSamples.length,
        crossOrganizationRecords,
      },
      errors: {
        operations,
        policyErrors,
        nonPolicyErrors,
        nonPolicyRate,
      },
    },
    rawSamples: {
      planning: planningSamples.map(rounded),
      wave: waveSamples.map(rounded),
      eventAcknowledgements: eventSamples.map(rounded),
      logPages: logSamples.map(rounded),
      isolationChecks: isolationSamples.map(rounded),
      nonPolicyErrors: [nonPolicyErrors],
    },
  };
}

export async function loadProof(fixture, options, fixtureBytes) {
  return options.baseURL
    ? runRemoteLoadProof(fixture, options, fixtureBytes)
    : simulateLoadProof(fixture, options, fixtureBytes);
}

async function main() {
  const options = parseLoadArgs(process.argv.slice(2));
  let fixtureBytes;
  let fixture;
  try {
    fixtureBytes = await readFile(options.fixture);
    fixture = JSON.parse(fixtureBytes.toString('utf8'));
  } catch {
    fail('fixture must be readable valid JSON');
  }
  const report = await loadProof(fixture, options, fixtureBytes);
  process.stdout.write(`${JSON.stringify(report, null, 2)}\n`);
  if (!report.passed) {
    process.exitCode = 1;
  }
}

const invokedPath = process.argv[1] ? path.resolve(process.argv[1]) : '';
if (invokedPath === fileURLToPath(import.meta.url)) {
  main().catch((error) => {
    process.stderr.write(`${error.message}\n`);
    process.exitCode = 1;
  });
}
