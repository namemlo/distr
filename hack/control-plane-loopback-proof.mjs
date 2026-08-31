#!/usr/bin/env node

import {execFileSync} from 'node:child_process';
import {createHash} from 'node:crypto';
import {mkdir, readFile, writeFile} from 'node:fs/promises';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

import {loadProof, parseLoadArgs} from './control-plane-load-test.mjs';
import {startLoopbackProofService} from './control-plane-loopback-proof-service.mjs';
import {benchmark, validateFixture as validateBenchmarkFixture} from './control-plane-read-model-benchmark.mjs';

const repoRoot = path.resolve(new URL('..', import.meta.url).pathname.slice(process.platform === 'win32' ? 1 : 0));
const performanceSchema = 'distr.control-plane-performance-result/v1';

function fail(message) {
  throw new Error(message);
}

function sha256(bytes) {
  return `sha256:${createHash('sha256').update(bytes).digest('hex')}`;
}

function currentCommit() {
  const commit = execFileSync('git', ['rev-parse', 'HEAD'], {cwd: repoRoot, encoding: 'utf8'}).trim();
  if (!/^[a-f0-9]{40}$/.test(commit)) fail('HEAD must resolve to a full Git commit');
  return commit;
}

function workingTreeDirty(ignoredPaths) {
  const ignored = ignoredPaths.map((item) => path.resolve(item));
  const status = execFileSync('git', ['status', '--porcelain=v1', '-z', '--untracked-files=all'], {
    cwd: repoRoot,
    encoding: 'utf8',
  });
  return status
    .split('\0')
    .filter(Boolean)
    .map((entry) => path.resolve(repoRoot, entry.slice(3)))
    .some(
      (item) => !ignored.some((ignoredPath) => item === ignoredPath || item.startsWith(`${ignoredPath}${path.sep}`))
    );
}

function parseArgs(argv) {
  const values = new Map();
  for (let index = 0; index < argv.length; index += 2) {
    const option = argv[index];
    const value = argv[index + 1];
    if (!option?.startsWith('--') || value === undefined || values.has(option)) fail(`invalid argument near ${option}`);
    values.set(option, value);
  }
  const allowed = new Set(['--fixture', '--out-dir', '--duration', '--rate', '--auth-env', '--timeout-ms']);
  for (const option of values.keys()) if (!allowed.has(option)) fail(`unknown option ${option}`);
  const fixture = values.get('--fixture');
  const outDir = values.get('--out-dir');
  const authEnv = values.get('--auth-env') ?? 'CONTROL_PLANE_LOOPBACK_PROOF_TOKEN';
  if (!fixture) fail('--fixture is required');
  if (!outDir) fail('--out-dir is required');
  if (!process.env[authEnv]) fail(`authorization is required from environment variable ${authEnv}`);
  return {
    fixture: path.resolve(fixture),
    outDir: path.resolve(outDir),
    duration: values.get('--duration') ?? '10m',
    rate: values.get('--rate') ?? '100',
    authEnv,
    timeoutMs: values.get('--timeout-ms') ?? '10000',
  };
}

function aggregate(samples, aggregation) {
  if (aggregation === 'sum') return samples.reduce((total, value) => total + value, 0);
  const sorted = [...samples].sort((left, right) => left - right);
  if (aggregation === 'max') return sorted.at(-1);
  const quantile = aggregation === 'p95' ? 0.95 : 0.99;
  return sorted[Math.max(0, Math.ceil(quantile * sorted.length) - 1)];
}

function thresholds(requirements, series) {
  return requirements.map((requirement) => {
    const measured = aggregate(series[requirement.name], requirement.aggregation);
    const passed =
      requirement.operator === 'eq'
        ? measured === requirement.limit
        : requirement.operator === 'lt'
          ? measured < requirement.limit
          : measured <= requirement.limit;
    return {...requirement, samples: series[requirement.name].length, measured, passed};
  });
}

async function writeJSON(output, value) {
  const bytes = Buffer.from(`${JSON.stringify(value, null, 2)}\n`);
  await writeFile(output, bytes);
  return {path: path.relative(repoRoot, output).replaceAll('\\', '/'), sha256: sha256(bytes)};
}

function commonPerformanceFields({
  sourceCommit,
  buildVersion,
  artifactDigest,
  fixture,
  fixtureSha256,
  fixtureSizeBytes,
  hardware,
}) {
  return {
    schema: performanceSchema,
    sourceCommit,
    mode: 'measured-live',
    hardware,
    build: {version: buildVersion, artifactDigest},
    dataset: {
      targets: fixture.targets.length,
      placements: fixture.placements.length,
      onlineExecutors: fixture.agents.length,
      components: fixture.components.length,
      steps: fixture.steps.length,
      fixtureSchema: fixture.schemaVersion,
      fixtureSeed: fixture.seed,
      fixtureSha256,
      fixtureSizeBytes,
    },
    environment: {
      scope: 'deterministic-loopback-reference',
      database: {mode: 'fixture', sizeBytes: fixtureSizeBytes},
      network: 'loopback-http',
      warmState: 'warm',
    },
  };
}

function scaleEvidence({common, report, requirements, rawSamples}) {
  return {
    ...common,
    scenario: 'fleet-api-slos',
    status: report.qualification.acceptanceEligible ? 'passed' : 'failed',
    thresholds: thresholds(requirements, report.rawSamples.series),
    rawSamples,
    facts: {
      pageSize: report.facts.pageSize,
      boundedResponses: report.facts.boundedResponses,
      workloads: report.facts.workloads,
      maxResponseItems: report.facts.maxResponseItems,
    },
  };
}

function loadSeries(report) {
  return {
    'plan-create-validate-p95-ms': report.rawSamples.planning,
    'wave-materialize-schedule-duration-ms': report.rawSamples.wave,
    'event-ingest-ack-p95-ms': report.rawSamples.eventAcknowledgements,
    'lost-accepted-events': [report.scenarios.events.lostAcceptedEvents],
    'log-first-page-indexed-ms': [report.scenarios.logs.firstPageMs],
    'cross-organization-records': [report.scenarios.isolation.crossOrganizationRecords],
    'non-policy-error-rate': [report.scenarios.errors.nonPolicyRate],
  };
}

function loadEvidence({common, report, requirements, rawSamples}) {
  const series = loadSeries(report);
  return {
    ...common,
    scenario: 'roadmap-scale-load',
    status: report.acceptanceEligible ? 'passed' : 'failed',
    thresholds: thresholds(requirements, series),
    rawSamples,
    facts: {
      planRuns: report.scenarios.planning.runs,
      deterministicPlanChecksum: report.scenarios.planning.deterministicChecksum,
      planChecksums: report.scenarios.planning.checksums,
      waveStableOrder: report.scenarios.wave.stableOrder,
      duplicateAdmissions: report.scenarios.wave.duplicateAdmissions,
      waveOrderChecksums: report.scenarios.wave.orderChecksums,
      eventDurationSeconds: report.scenarios.events.requestedDurationSeconds,
      eventRatePerSecond: report.scenarios.events.targetRatePerSecond,
      authentication: report.scenarios.events.authentication,
      concurrentAgents: report.scenarios.events.concurrentAgents,
      authenticatedExecutorIds: report.scenarios.events.authenticatedExecutorIds,
      authenticatedExecutorIdsChecksum: report.scenarios.events.authenticatedExecutorIdsChecksum,
      acceptedEvents: report.scenarios.events.acceptedEvents,
      lostAcceptedEvents: report.scenarios.events.lostAcceptedEvents,
      logBytes: report.scenarios.logs.totalBytes,
      logPeakBufferBytes: report.scenarios.logs.peakBufferBytes,
      logStreamingBoundedMemory: report.scenarios.logs.streamingBoundedMemory,
      crossOrganizationRecords: report.scenarios.isolation.crossOrganizationRecords,
    },
  };
}

async function main() {
  const options = parseArgs(process.argv.slice(2));
  if (!options.outDir.startsWith(`${repoRoot}${path.sep}`)) fail('--out-dir must be inside the repository');
  const fixtureBytes = await readFile(options.fixture);
  const fixture = JSON.parse(fixtureBytes.toString('utf8'));
  validateBenchmarkFixture(fixture, {profile: 'acceptance'});

  const sourceCommit = currentCommit();
  const dirty = workingTreeDirty([options.fixture, options.outDir]);
  const packageJSON = JSON.parse(await readFile(path.join(repoRoot, 'package.json'), 'utf8'));
  const serviceBytes = await readFile(new URL('./control-plane-loopback-proof-service.mjs', import.meta.url));
  const artifactDigest = sha256(serviceBytes);
  const buildVersion = `${packageJSON.version}+loopback.${sourceCommit.slice(0, 12)}`;
  const metadata = {sourceCommit, buildVersion, artifactDigest};
  await mkdir(options.outDir, {recursive: true});

  const service = await startLoopbackProofService({fixture, token: process.env[options.authEnv], metadata});
  let scaleReport;
  let loadReport;
  let serviceSnapshot;
  try {
    scaleReport = await benchmark(fixture, {
      runs: 20,
      pageSize: 100,
      thresholds: {p95Ms: 2000, p99Ms: 5000},
      baseURL: service.baseURL,
      authEnv: options.authEnv,
      timeoutMs: Number(options.timeoutMs),
      buildVersion,
      imageDigest: artifactDigest,
      profile: 'acceptance',
      sourceCommit,
      workingTreeDirty: dirty,
    });
    const loadOptions = parseLoadArgs([
      '--fixture',
      options.fixture,
      '--duration',
      options.duration,
      '--rate',
      options.rate,
      '--base-url',
      service.baseURL.href,
      '--auth-env',
      options.authEnv,
      '--timeout-ms',
      options.timeoutMs,
      '--build-version',
      buildVersion,
      '--artifact-digest',
      artifactDigest,
    ]);
    loadOptions.sourceCommit = sourceCommit;
    loadOptions.workingTreeDirty = dirty;
    loadReport = await loadProof(fixture, loadOptions, fixtureBytes);
  } finally {
    serviceSnapshot = service.snapshot();
    await service.close();
  }

  const contract = JSON.parse(
    await readFile(path.join(repoRoot, 'docs', 'release', 'control-plane-acceptance-contract.json'), 'utf8')
  );
  const scaleRaw = await writeJSON(path.join(options.outDir, 'AC-50.raw-samples.json'), scaleReport.rawSamples);
  const loadRawValue = {series: loadSeries(loadReport)};
  const loadRaw = await writeJSON(path.join(options.outDir, 'AC-51.raw-samples.json'), loadRawValue);
  const scaleCommon = commonPerformanceFields({
    sourceCommit,
    buildVersion,
    artifactDigest,
    fixture,
    fixtureSha256: sha256(fixtureBytes),
    fixtureSizeBytes: fixtureBytes.length,
    hardware: scaleReport.hardware,
  });
  const loadCommon = commonPerformanceFields({
    sourceCommit,
    buildVersion,
    artifactDigest,
    fixture,
    fixtureSha256: sha256(fixtureBytes),
    fixtureSizeBytes: fixtureBytes.length,
    hardware: {
      os: `${loadReport.hardware.platform} ${loadReport.hardware.operatingSystemRelease}`,
      architecture: loadReport.hardware.architecture,
      cpu: loadReport.hardware.cpuModel,
      logicalCores: loadReport.hardware.logicalCpuCount,
      memoryBytes: loadReport.hardware.totalMemoryBytes,
    },
  });
  const scalePerformance = scaleEvidence({
    common: scaleCommon,
    report: scaleReport,
    requirements: contract.profiles['pr081-scale'].proofRequirements.performanceMetrics,
    rawSamples: scaleRaw,
  });
  const loadPerformance = loadEvidence({
    common: loadCommon,
    report: loadReport,
    requirements: contract.profiles['pr081-load'].proofRequirements.performanceMetrics,
    rawSamples: loadRaw,
  });
  const scaleBinding = await writeJSON(path.join(options.outDir, 'AC-50.performance.json'), scalePerformance);
  const loadBinding = await writeJSON(path.join(options.outDir, 'AC-51.performance.json'), loadPerformance);
  const summary = {
    schema: 'distr.control-plane-loopback-proof-summary/v1',
    sourceCommit,
    workingTreeDirty: dirty,
    buildVersion,
    artifactDigest,
    service: serviceSnapshot,
    status:
      scaleReport.qualification.acceptanceEligible && loadReport.acceptanceEligible && !dirty ? 'passed' : 'failed',
    scale: {
      acceptanceEligible: scaleReport.qualification.acceptanceEligible,
      aggregate: scaleReport.aggregate,
      binding: scaleBinding,
    },
    load: {
      acceptanceEligible: loadReport.acceptanceEligible,
      eventAcknowledgementP95Ms: loadReport.scenarios.events.p95Ms,
      acceptedEvents: loadReport.scenarios.events.acceptedEvents,
      lostAcceptedEvents: loadReport.scenarios.events.lostAcceptedEvents,
      authenticatedExecutors: loadReport.scenarios.events.authenticatedExecutorIds.length,
      binding: loadBinding,
    },
  };
  await writeJSON(path.join(options.outDir, 'summary.json'), summary);
  process.stdout.write(`${JSON.stringify(summary, null, 2)}\n`);
  if (summary.status !== 'passed') process.exitCode = 1;
}

const invokedPath = process.argv[1] ? path.resolve(process.argv[1]) : '';
if (invokedPath === fileURLToPath(import.meta.url)) {
  main().catch((error) => {
    process.stderr.write(`${error.message}\n`);
    process.exitCode = 1;
  });
}
