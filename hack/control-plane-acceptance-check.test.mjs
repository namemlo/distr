import assert from 'node:assert/strict';
import {execFile, spawn} from 'node:child_process';
import {createHash} from 'node:crypto';
import {mkdir, mkdtemp, readFile, rm, symlink, writeFile} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {test} from 'node:test';
import {fileURLToPath} from 'node:url';
import {promisify} from 'node:util';
import {deflateSync} from 'node:zlib';

const execFileAsync = promisify(execFile);
const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const checker = path.join(repoRoot, 'hack', 'control-plane-acceptance-check.mjs');
const contractPath = 'docs/release/control-plane-acceptance-contract.json';
const browserAutomatedTest = 'frontend/ui/e2e/control-plane.spec.ts';
const browserManualEvidence = 'docs/fork/PR-080_OPERATOR_CONTROL_ROOM_UI.md';
const browserConfig = 'playwright.control-plane-evidence.config.ts';
const browserFixture = 'frontend/ui/e2e/fixtures/control-plane.ts';
const browserProject = 'chromium';
const browserPlaywrightCLI = 'node_modules/@playwright/test/cli.js';
const browserTitle = '@evidence proves the reference client DEV release, approval, and previous-state journey';
const browserGrep = `${browserTitle.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}$`;
const browserCommand = [
  'node',
  browserPlaywrightCLI,
  'test',
  browserAutomatedTest,
  '--config',
  browserConfig,
  '--project',
  browserProject,
  '--grep',
  browserGrep,
  '--reporter',
  'json',
];
const browserScreenshotNames = [
  '01-version-build.png',
  '02-accumulated-changelog.png',
  '03-dependency-constraints.png',
  '04-plan-approval-pending.png',
  '05-approval-request.png',
  '06-approval-approved.png',
  '07-plan-approval-satisfied.png',
  '08-previous-state-plan.png',
  '09-previous-state-comparison.png',
  '10-release-b-history-preserved.png',
  '11-immutable-history-audit.png',
];
const browserExecutionSourcePaths = [
  browserConfig,
  'playwright.control-plane.config.ts',
  browserFixture,
  'package.json',
  'pnpm-lock.yaml',
];
const adopterOwners = new Map([
  ['AC-01', 'ADOPTER-01'],
  ['AC-02', 'ADOPTER-01'],
  ['AC-48', 'ADOPTER-06'],
  ['AC-49', 'ADOPTER-06'],
  ['AC-52', 'ADOPTER-05'],
  ['AC-54', 'ADOPTER-05'],
  ['AC-55', 'ADOPTER-02'],
  ['AC-64', 'ADOPTER-06'],
  ['AC-79', 'ADOPTER-06'],
]);
const fixtureFleetRequirements = {
  performanceScenario: 'fleet-api-slos',
  performanceMetrics: [
    {name: 'registry-list-p95-ms', aggregation: 'p95', operator: 'lte', limit: 2000, unit: 'ms', minSamples: 3},
    {name: 'registry-list-p99-ms', aggregation: 'p99', operator: 'lte', limit: 5000, unit: 'ms', minSamples: 3},
  ],
  performanceFacts: {
    exact: {pageSize: 100, boundedResponses: true, workloads: ['registry-list']},
    minimum: {},
  },
};
const fixtureLoadRequirements = {
  performanceScenario: 'roadmap-scale-load',
  performanceMetrics: [
    {
      name: 'plan-create-validate-p95-ms',
      aggregation: 'p95',
      operator: 'lte',
      limit: 10000,
      unit: 'ms',
      minSamples: 5,
    },
    {
      name: 'event-ingest-ack-p95-ms',
      aggregation: 'p95',
      operator: 'lte',
      limit: 1000,
      unit: 'ms',
      minSamples: 3,
    },
    {
      name: 'lost-accepted-events',
      aggregation: 'sum',
      operator: 'eq',
      limit: 0,
      unit: 'count',
      minSamples: 1,
    },
  ],
  performanceFacts: {
    exact: {
      planRuns: 5,
      deterministicPlanChecksum: true,
      eventDurationSeconds: 600,
      eventRatePerSecond: 100,
      logBytes: 104857600,
      logStreamingBoundedMemory: true,
    },
    minimum: {acceptedEvents: 60000},
  },
};

function sha256(value) {
  return `sha256:${createHash('sha256').update(value).digest('hex')}`;
}

const fixturePNGCache = new Map();

function crc32(bytes) {
  let value = 0xffffffff;
  for (const byte of bytes) {
    value ^= byte;
    for (let bit = 0; bit < 8; bit += 1) value = (value >>> 1) ^ (0xedb88320 & -(value & 1));
  }
  return (value ^ 0xffffffff) >>> 0;
}

function pngChunk(type, data) {
  const typeBytes = Buffer.from(type);
  const length = Buffer.alloc(4);
  length.writeUInt32BE(data.length);
  const checksum = Buffer.alloc(4);
  checksum.writeUInt32BE(crc32(Buffer.concat([typeBytes, data])));
  return Buffer.concat([length, typeBytes, data, checksum]);
}

function fixturePNG(width = 1440, height = 1200, {secret = false} = {}) {
  const key = `${width}:${height}:${secret}`;
  if (!fixturePNGCache.has(key)) {
    const ihdr = Buffer.alloc(13);
    ihdr.writeUInt32BE(width, 0);
    ihdr.writeUInt32BE(height, 4);
    ihdr[8] = 8;
    ihdr[9] = 6;
    const scanlines = Buffer.alloc((width * 4 + 1) * height);
    const chunks = [pngChunk('IHDR', ihdr)];
    if (secret) chunks.push(pngChunk('tEXt', Buffer.from('password\0browser-secret-value')));
    chunks.push(pngChunk('IDAT', deflateSync(scanlines)));
    chunks.push(pngChunk('IEND', Buffer.alloc(0)));
    fixturePNGCache.set(key, Buffer.concat([Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]), ...chunks]));
  }
  return Buffer.from(fixturePNGCache.get(key));
}

function renderLedger(rows) {
  return [
    '# Acceptance ledger',
    '',
    '| Acceptance ID | Owning PR | Automated test | Manual/fixture evidence | Status | Artifact/checksum |',
    '| --- | --- | --- | --- | --- | --- |',
    ...rows.map(
      (row) =>
        `| \`${row.id}\` | \`${row.owner}\` | \`${row.automatedTest}\` | \`${row.manualEvidence}\` | \`${row.status}\` | \`${row.artifact}\` |`
    ),
    '',
  ].join('\n');
}

function fixtureContract(proofOverride) {
  const acceptance = {};
  for (let index = 0; index < 80; index += 1) {
    const id = `AC-${String(index + 1).padStart(2, '0')}`;
    const adopterOwner = adopterOwners.get(id);
    acceptance[id] = {
      owner: adopterOwner ?? (id === 'AC-40' || id === 'AC-41' ? 'PR-073' : 'PR-083'),
      profile: adopterOwner ? 'adopter-execution' : 'community-test',
      pendingAdopter: Boolean(adopterOwner),
    };
  }
  if (proofOverride) {
    const browserProof = proofOverride.proofClass === 'browser-e2e';
    const contract = {
      schema: 'distr.control-plane-acceptance-contract/v1',
      normativeSource: 'normative-plan.md',
      profiles: {
        'community-test': {
          automatedTest: 'evidence.test.mjs',
          manualEvidence: 'evidence.md',
          allowedProofClasses: ['community-focused-test'],
          testRunner: 'node-test',
        },
        'adopter-execution': {
          automatedTest: 'evidence.test.mjs',
          manualEvidence: 'evidence.md',
          allowedProofClasses: ['adopter-execution'],
          testRunner: 'node-test',
        },
        'special-proof': {
          automatedTest: browserProof ? browserAutomatedTest : 'evidence.test.mjs',
          manualEvidence: browserProof ? browserManualEvidence : 'evidence.md',
          allowedProofClasses: [proofOverride.proofClass],
          testRunner: browserProof ? 'playwright' : 'node-test',
          proofRequirements: proofOverride.proofRequirements ?? {},
        },
      },
      acceptance,
    };
    contract.acceptance[proofOverride.id].profile = 'special-proof';
    if (browserProof) contract.acceptance[proofOverride.id].owner = 'PR-080';
    return contract;
  }
  return {
    schema: 'distr.control-plane-acceptance-contract/v1',
    normativeSource: 'normative-plan.md',
    profiles: {
      'community-test': {
        automatedTest: 'evidence.test.mjs',
        manualEvidence: 'evidence.md',
        allowedProofClasses: ['community-focused-test'],
        testRunner: 'node-test',
      },
      'adopter-execution': {
        automatedTest: 'evidence.test.mjs',
        manualEvidence: 'evidence.md',
        allowedProofClasses: ['adopter-execution'],
        testRunner: 'node-test',
      },
    },
    acceptance,
  };
}

async function git(directory, ...args) {
  return execFileAsync('git', args, {cwd: directory});
}

async function fixtureWorkspace({
  mutateRows = () => {},
  verifiedAdopter,
  proofOverride,
  browserRuntimeBinding = true,
  browserLockImporterVersion = '1.52.0',
} = {}) {
  const directory = await mkdtemp(path.join(tmpdir(), 'control-plane-acceptance-'));
  await mkdir(path.join(directory, 'docs', 'release'), {recursive: true});
  await mkdir(path.join(directory, 'results'), {recursive: true});
  const evidence = '# Retained fixture evidence\n';
  const automatedTest = "import {test} from 'node:test';\ntest('proof', () => {});\n";
  const browserRuntimeAssertion = browserRuntimeBinding
    ? '  const expectedNodeVersion = process.env.DISTR_EVIDENCE_NODE_VERSION;\n  if (expectedNodeVersion !== undefined) {\n    expect(process.versions.node).toBe(expectedNodeVersion);\n  }\n'
    : '';
  const browserSources = {
    [browserAutomatedTest]: `test('${browserTitle}', async ({controlPlane}) => {\n${browserRuntimeAssertion}  expect(controlPlane.externalAttempts).toEqual([]);\n});\n`,
    [browserManualEvidence]: '# AC-63 browser evidence\n',
    [browserConfig]: `export default {projects: [{name: '${browserProject}'}]};\n`,
    'playwright.control-plane.config.ts': 'export default {};\n',
    [browserFixture]:
      "const externalAttempts = [];\npage.route('**/*', async (route) => {\n  if (!isLocalHost(new URL(route.request().url()).hostname)) {\n    externalAttempts.push(route.request().url());\n    await route.abort('blockedbyclient');\n  }\n});\n",
    'package.json':
      '{"name":"acceptance-browser-fixture","private":true,"packageManager":"pnpm@11.7.0","devDependencies":{"@playwright/test":"^1.52.0"}}\n',
    'pnpm-lock.yaml': `lockfileVersion: '9.0'\n\nimporters:\n\n  .:\n    devDependencies:\n      '@playwright/test':\n        specifier: ^1.52.0\n        version: ${browserLockImporterVersion}\n\npackages:\n\n  '@playwright/test@1.52.0':\n    resolution: {integrity: sha512-YWN0dWFsLWxvY2tmaWxlLWJvdW5kLXBsYXl3cmlnaHQtdGVzdA==}\n\nsnapshots:\n\n  '@playwright/test@1.52.0': {}\n`,
  };
  const contract = fixtureContract(proofOverride);
  await writeFile(path.join(directory, 'evidence.test.mjs'), automatedTest);
  await writeFile(path.join(directory, 'evidence.md'), evidence);
  for (const [relative, contents] of Object.entries(browserSources)) {
    const target = path.join(directory, relative);
    await mkdir(path.dirname(target), {recursive: true});
    await writeFile(target, contents);
  }
  await writeFile(path.join(directory, 'normative-plan.md'), '# Normative owner map\n');
  await writeFile(path.join(directory, contractPath), `${JSON.stringify(contract, null, 2)}\n`);
  await git(directory, 'init', '--quiet');
  await git(directory, 'config', 'user.email', 'fixture@example.invalid');
  await git(directory, 'config', 'user.name', 'Acceptance Fixture');
  await git(directory, 'add', '.');
  await git(directory, 'commit', '--quiet', '-m', 'fixture source');
  const {stdout} = await git(directory, 'rev-parse', 'HEAD');
  const sourceCommit = stdout.trim();
  const result = {
    schema: 'distr.control-plane-test-result/v1',
    sourceCommit,
    command: {
      runner: 'node-test',
      argv: ['node', '--test', 'evidence.test.mjs'],
      selectedTestSource: 'evidence.test.mjs',
    },
    exitCode: 0,
    tests: {expected: 1, passed: 1, failed: 0, skipped: 0},
    status: 'passed',
    startedAt: '2030-01-01T00:00:00.000Z',
    completedAt: '2030-01-01T00:00:01.000Z',
  };
  const resultBytes = `${JSON.stringify(result, null, 2)}\n`;
  await writeFile(path.join(directory, 'results', 'test-result.json'), resultBytes);
  let browserResultBytes;
  if (proofOverride?.proofClass === 'browser-e2e') {
    const browserResult = {
      schema: 'distr.control-plane-test-result/v1',
      sourceCommit,
      command: {
        runner: 'playwright',
        argv: browserCommand,
        selectedTestSource: browserAutomatedTest,
      },
      exitCode: 0,
      tests: {expected: 1, passed: 1, failed: 0, skipped: 0},
      status: 'passed',
      startedAt: '2030-01-01T00:00:00.000Z',
      completedAt: '2030-01-01T00:00:01.000Z',
    };
    browserResultBytes = `${JSON.stringify(browserResult, null, 2)}\n`;
    await writeFile(path.join(directory, 'results', 'browser-test-result.json'), browserResultBytes);
  }

  let classEvidence;
  if (proofOverride?.proofClass === 'performance-measurement') {
    const requirements = proofOverride.proofRequirements ?? {};
    const metricRequirements = requirements.performanceMetrics ?? [
      {name: 'fleet-p95', aggregation: 'p95', operator: 'lte', limit: 100, unit: 'ms', minSamples: 3},
    ];
    const series = {};
    const thresholds = metricRequirements.map((requirement) => {
      const value = requirement.operator === 'eq' ? requirement.limit : requirement.limit / 2;
      series[requirement.name] = Array.from({length: requirement.minSamples}, () => value);
      return {
        name: requirement.name,
        aggregation: requirement.aggregation,
        operator: requirement.operator,
        samples: requirement.minSamples,
        measured: value,
        limit: requirement.limit,
        unit: requirement.unit,
        passed: true,
      };
    });
    const rawSamples = `${JSON.stringify({series}, null, 2)}\n`;
    await writeFile(path.join(directory, 'results', 'performance-samples.json'), rawSamples);
    const facts = {
      ...(requirements.performanceFacts?.exact ?? {}),
      ...(requirements.performanceFacts?.minimum ?? {}),
    };
    if (requirements.performanceScenario === 'fleet-api-slos') {
      facts.maxResponseItems = 100;
    }
    if (requirements.performanceScenario === 'roadmap-scale-load') {
      facts.planChecksums = Array.from({length: 5}, () => `sha256:${'d'.repeat(64)}`);
      facts.waveOrderChecksums = Array.from({length: 2}, () => `sha256:${'e'.repeat(64)}`);
      facts.logPeakBufferBytes = 1048576;
      facts.authentication = 'authenticated-live';
      facts.concurrentAgents = 100;
      facts.authenticatedExecutorIds = Array.from(
        {length: 100},
        (_, index) => `executor-${String(index + 1).padStart(3, '0')}`
      );
      facts.authenticatedExecutorIdsChecksum = sha256(JSON.stringify([...facts.authenticatedExecutorIds].sort()));
    }
    const classReport = {
      schema: 'distr.control-plane-performance-result/v1',
      sourceCommit,
      scenario: requirements.performanceScenario,
      mode: 'measured-live',
      status: 'passed',
      rawSamples: {path: 'results/performance-samples.json', sha256: sha256(rawSamples)},
      hardware: {
        os: 'fixture-os',
        architecture: 'fixture-arch',
        cpu: 'fixture-cpu',
        logicalCores: 8,
        memoryBytes: 17179869184,
      },
      build: {version: '1.0.0', artifactDigest: `sha256:${'a'.repeat(64)}`},
      dataset: {targets: 1000, placements: 649, onlineExecutors: 100, components: 100, steps: 500},
      facts,
      thresholds,
    };
    const bytes = `${JSON.stringify(classReport, null, 2)}\n`;
    await writeFile(path.join(directory, 'results', 'class-report.json'), bytes);
    classEvidence = {path: 'results/class-report.json', sha256: sha256(bytes)};
  } else if (proofOverride?.proofClass === 'neutral-live-execution') {
    const releaseLineage = {
      componentReleases: [
        {
          id: 'component-release-a-provider',
          componentKey: 'catalog-provider',
          version: '1.0.0',
          artifactDigest: `sha256:${'a'.repeat(64)}`,
          canonicalChecksum: `sha256:${'1'.repeat(64)}`,
        },
        {
          id: 'component-release-a-consumer',
          componentKey: 'gateway-consumer',
          version: '1.0.0',
          artifactDigest: `sha256:${'b'.repeat(64)}`,
          canonicalChecksum: `sha256:${'2'.repeat(64)}`,
        },
        {
          id: 'component-release-b-provider',
          componentKey: 'catalog-provider',
          version: '1.1.0',
          artifactDigest: `sha256:${'c'.repeat(64)}`,
          canonicalChecksum: `sha256:${'3'.repeat(64)}`,
        },
        {
          id: 'component-release-b-consumer',
          componentKey: 'gateway-consumer',
          version: '1.1.0',
          artifactDigest: `sha256:${'d'.repeat(64)}`,
          canonicalChecksum: `sha256:${'4'.repeat(64)}`,
        },
      ],
      productReleases: [
        {
          id: 'product-release-a',
          version: '1.0.0',
          canonicalChecksum: `sha256:${'5'.repeat(64)}`,
          graphChecksum: `sha256:${'6'.repeat(64)}`,
          componentReleaseIds: ['component-release-a-provider', 'component-release-a-consumer'],
        },
        {
          id: 'product-release-b',
          version: '1.1.0',
          canonicalChecksum: `sha256:${'7'.repeat(64)}`,
          graphChecksum: `sha256:${'8'.repeat(64)}`,
          componentReleaseIds: ['component-release-b-provider', 'component-release-b-consumer'],
        },
      ],
    };
    const classReport = {
      schema: 'distr.control-plane-neutral-live-result/v1',
      sourceCommit,
      proofMode: 'live-hub-api',
      status: 'passed',
      acceptanceEligible: true,
      liveStack: {started: true},
      releaseLineage,
      productReleaseHistory: ['product-release-a', 'product-release-b', 'product-release-a'],
      targets: [
        {
          id: 'target-alpha',
          hubTargetId: 'hub-target-alpha',
          targetId: 'hub-target-alpha',
          activeRelease: 'A',
          configSnapshotId: 'config-alpha',
          configChecksum: `sha256:${'9'.repeat(64)}`,
          adapterKind: 'external-executor',
          executorId: 'executor-alpha',
          observerId: 'observer-alpha',
          status: 'passed',
          transitions: [
            {
              direction: 'A-to-B',
              targetConfigSnapshotId: 'config-alpha',
              targetConfigChecksum: `sha256:${'9'.repeat(64)}`,
              fromProductReleaseId: 'product-release-a',
              toProductReleaseId: 'product-release-b',
              planId: 'plan-alpha-a-to-b',
              planChecksum: `sha256:${'a'.repeat(64)}`,
              executions: [
                {
                  executionId: 'execution-alpha-a-to-b',
                  executionChecksum: `sha256:${'b'.repeat(64)}`,
                  stepKey: 'deploy:catalog-provider',
                },
              ],
              observations: [
                {
                  observationId: 'observation-alpha-a-to-b',
                  evidenceChecksum: `sha256:${'c'.repeat(64)}`,
                  stateChecksum: `sha256:${'d'.repeat(64)}`,
                  componentKey: 'catalog-provider',
                },
              ],
            },
            {
              direction: 'B-to-A',
              targetConfigSnapshotId: 'config-alpha',
              targetConfigChecksum: `sha256:${'9'.repeat(64)}`,
              fromProductReleaseId: 'product-release-b',
              toProductReleaseId: 'product-release-a',
              planId: 'plan-alpha-b-to-a',
              planChecksum: `sha256:${'e'.repeat(64)}`,
              executions: [
                {
                  executionId: 'execution-alpha-b-to-a',
                  executionChecksum: `sha256:${'f'.repeat(64)}`,
                  stepKey: 'deploy:catalog-provider',
                },
              ],
              observations: [
                {
                  observationId: 'observation-alpha-b-to-a',
                  evidenceChecksum: `sha256:${'0'.repeat(64)}`,
                  stateChecksum: `sha256:${'1'.repeat(64)}`,
                  componentKey: 'catalog-provider',
                },
              ],
            },
          ],
        },
        {
          id: 'target-beta',
          hubTargetId: 'hub-target-beta',
          targetId: 'hub-target-beta',
          activeRelease: 'A',
          configSnapshotId: 'config-beta',
          configChecksum: `sha256:${'2'.repeat(64)}`,
          adapterKind: 'reference',
          executorId: 'executor-beta',
          observerId: 'observer-beta',
          status: 'passed',
          transitions: [
            {
              direction: 'A-to-B',
              targetConfigSnapshotId: 'config-beta',
              targetConfigChecksum: `sha256:${'2'.repeat(64)}`,
              fromProductReleaseId: 'product-release-a',
              toProductReleaseId: 'product-release-b',
              planId: 'plan-beta-a-to-b',
              planChecksum: `sha256:${'3'.repeat(64)}`,
              executions: [
                {
                  executionId: 'execution-beta-a-to-b',
                  executionChecksum: `sha256:${'4'.repeat(64)}`,
                  stepKey: 'deploy:gateway-consumer',
                },
              ],
              observations: [
                {
                  observationId: 'observation-beta-a-to-b',
                  evidenceChecksum: `sha256:${'5'.repeat(64)}`,
                  stateChecksum: `sha256:${'6'.repeat(64)}`,
                  componentKey: 'gateway-consumer',
                },
              ],
            },
            {
              direction: 'B-to-A',
              targetConfigSnapshotId: 'config-beta',
              targetConfigChecksum: `sha256:${'2'.repeat(64)}`,
              fromProductReleaseId: 'product-release-b',
              toProductReleaseId: 'product-release-a',
              planId: 'plan-beta-b-to-a',
              planChecksum: `sha256:${'7'.repeat(64)}`,
              executions: [
                {
                  executionId: 'execution-beta-b-to-a',
                  executionChecksum: `sha256:${'8'.repeat(64)}`,
                  stepKey: 'deploy:gateway-consumer',
                },
              ],
              observations: [
                {
                  observationId: 'observation-beta-b-to-a',
                  evidenceChecksum: `sha256:${'9'.repeat(64)}`,
                  stateChecksum: `sha256:${'a'.repeat(64)}`,
                  componentKey: 'gateway-consumer',
                },
              ],
            },
          ],
        },
      ],
      cleanup: {completed: true},
      nonLocalCalls: 0,
    };
    const bytes = `${JSON.stringify(classReport, null, 2)}\n`;
    await writeFile(path.join(directory, 'results', 'class-report.json'), bytes);
    classEvidence = {path: 'results/class-report.json', sha256: sha256(bytes)};
  } else if (proofOverride?.proofClass === 'browser-e2e') {
    await mkdir(path.join(directory, 'results', 'screenshots'), {recursive: true});
    const attachments = [];
    const screenshots = [];
    for (const name of browserScreenshotNames) {
      const bytes = fixturePNG();
      const screenshotPath = `results/screenshots/${name}`;
      await writeFile(path.join(directory, screenshotPath), bytes);
      attachments.push({
        name,
        path: `output/playwright/control-plane-evidence/result/${name}`,
        contentType: 'image/png',
      });
      screenshots.push({name, path: screenshotPath, sha256: sha256(bytes), width: 1440, height: 1200});
    }
    const rawReport = {
      config: {version: '1.52.0', projects: [{name: browserProject}]},
      suites: [
        {
          title: 'operator control room route-mocked contract',
          file: browserAutomatedTest,
          specs: [
            {
              title: browserTitle,
              ok: true,
              file: browserAutomatedTest,
              tests: [
                {
                  projectName: browserProject,
                  expectedStatus: 'passed',
                  status: 'expected',
                  results: [{retry: 0, status: 'passed', errors: [], attachments}],
                },
              ],
            },
          ],
          suites: [],
        },
      ],
      errors: [],
      stats: {
        startTime: '2030-01-01T00:00:00.000Z',
        duration: 1000,
        expected: 1,
        skipped: 0,
        flaky: 0,
        unexpected: 0,
      },
    };
    const rawBytes = `${JSON.stringify(rawReport, null, 2)}\n`;
    await mkdir(path.join(directory, 'results', 'raw'), {recursive: true});
    await writeFile(path.join(directory, 'results', 'raw', 'browser-raw.json'), rawBytes);
    const classReport = {
      schema: 'distr.control-plane-browser-e2e-result/v1',
      sourceCommit,
      runner: 'playwright',
      status: 'passed',
      project: browserProject,
      testSource: browserAutomatedTest,
      testTitles: [browserTitle],
      tests: {expected: 1, passed: 1, unexpected: 0, flaky: 0, skipped: 0},
      rawResult: {path: 'results/raw/browser-raw.json', sha256: sha256(rawBytes)},
      screenshots,
      networkProof: {mode: 'bound-test-assertion', testTitle: browserTitle, externalAttempts: 0},
      executionSources: browserExecutionSourcePaths.map((sourcePath) => ({
        path: sourcePath,
        sha256: sha256(browserSources[sourcePath]),
      })),
      toolVersions: {node: '26.3.1', pnpm: '11.7.0', playwright: '1.52.0'},
    };
    const bytes = `${JSON.stringify(classReport, null, 2)}\n`;
    await writeFile(path.join(directory, 'results', 'class-report.json'), bytes);
    classEvidence = {path: 'results/class-report.json', sha256: sha256(bytes)};
  }

  const rows = [];
  for (let index = 0; index < 80; index += 1) {
    const id = `AC-${String(index + 1).padStart(2, '0')}`;
    const rule = contract.acceptance[id];
    const adopterOwner = adopterOwners.get(id);
    if (adopterOwner && verifiedAdopter !== id) {
      rows.push({
        id,
        owner: adopterOwner,
        automatedTest: 'evidence.test.mjs',
        manualEvidence: 'evidence.md',
        status: 'pending-adopter',
        artifact: `pending-adopter:${adopterOwner}`,
      });
      continue;
    }
    const proofClass =
      proofOverride?.id === id
        ? proofOverride.proofClass
        : adopterOwner
          ? 'adopter-execution'
          : 'community-focused-test';
    const profile = contract.profiles[rule.profile];
    const automatedBytes =
      profile.automatedTest === 'evidence.test.mjs' ? automatedTest : browserSources[profile.automatedTest];
    const manualBytes = profile.manualEvidence === 'evidence.md' ? evidence : browserSources[profile.manualEvidence];
    const browserRow = proofClass === 'browser-e2e';
    const artifact = {
      schema: 'distr.control-plane-acceptance-evidence/v1',
      acceptanceId: id,
      owner: rule.owner,
      proofClass,
      sourceCommit,
      automatedTest: {path: profile.automatedTest, sha256: sha256(automatedBytes)},
      manualEvidence: {path: profile.manualEvidence, sha256: sha256(manualBytes)},
      testResult: browserRow
        ? {path: 'results/browser-test-result.json', sha256: sha256(browserResultBytes)}
        : {path: 'results/test-result.json', sha256: sha256(resultBytes)},
    };
    if (proofOverride?.id === id) {
      artifact.classEvidence = classEvidence;
    }
    if (adopterOwner) {
      const auditExport = {
        schema: 'distr.control-plane-adopter-audit-export/v1',
        sourceCommit,
        taskOwner: adopterOwner,
        organizationId: 'organization-proof',
        environmentId: 'environment-dev',
        targetIds: ['target-proof'],
        campaignIds: ['campaign-proof'],
        executionIds: ['execution-proof'],
        observationIds: ['observation-proof'],
        eventChecksum: `sha256:${'e'.repeat(64)}`,
        status: 'passed',
      };
      const auditBytes = `${JSON.stringify(auditExport, null, 2)}\n`;
      const auditPath = `results/${id}-audit.json`;
      await writeFile(path.join(directory, auditPath), auditBytes);
      const executionBundle = {
        schema: 'distr.control-plane-adopter-execution-bundle/v1',
        sourceCommit,
        taskOwner: adopterOwner,
        organizationId: 'organization-proof',
        environmentId: 'environment-dev',
        targetIds: ['target-proof'],
        campaignIds: ['campaign-proof'],
        executions: [
          {
            executionId: 'execution-proof',
            targetId: 'target-proof',
            campaignId: 'campaign-proof',
            executorId: 'executor-proof',
            artifactDigest: `sha256:${'a'.repeat(64)}`,
            status: 'succeeded',
          },
        ],
        observations: [
          {
            observationId: 'observation-proof',
            executionId: 'execution-proof',
            targetId: 'target-proof',
            observerId: 'observer-proof',
            artifactDigest: `sha256:${'a'.repeat(64)}`,
            configChecksum: `sha256:${'c'.repeat(64)}`,
            status: 'verified',
          },
        ],
        auditExport: {path: auditPath, sha256: sha256(auditBytes)},
        status: 'passed',
      };
      const bundleBytes = `${JSON.stringify(executionBundle, null, 2)}\n`;
      const bundlePath = `results/${id}-execution-bundle.json`;
      await writeFile(path.join(directory, bundlePath), bundleBytes);
      artifact.adopterExecution = {
        taskOwner: adopterOwner,
        organizationId: 'organization-proof',
        environmentId: 'environment-dev',
        targetIds: ['target-proof'],
        campaignIds: ['campaign-proof'],
        startedAt: '2030-01-01T00:00:00.000Z',
        completedAt: '2030-01-01T00:00:01.000Z',
        result: 'passed',
        bundle: {path: bundlePath, sha256: sha256(bundleBytes)},
      };
    }
    const artifactBytes = `${JSON.stringify(artifact, null, 2)}\n`;
    const artifactPath = `results/${id}.json`;
    await writeFile(path.join(directory, artifactPath), artifactBytes);
    rows.push({
      id,
      owner: rule.owner,
      automatedTest: profile.automatedTest,
      manualEvidence: profile.manualEvidence,
      status: adopterOwner ? 'verified-adopter' : 'community-evidence-retained',
      artifact: `${artifactPath} @ ${sha256(artifactBytes)}`,
    });
  }
  mutateRows(rows, contract);
  await writeFile(path.join(directory, 'ledger.md'), renderLedger(rows));
  await writeFile(path.join(directory, contractPath), `${JSON.stringify(contract, null, 2)}\n`);
  await git(directory, 'add', '.');
  await git(directory, 'commit', '--quiet', '-m', 'retained evidence');
  return {directory, rows, sourceCommit};
}

async function goSelectedFixtureWorkspace({
  mutateContract = () => {},
  mutateResult = () => {},
  additionalGoSources = {},
} = {}) {
  const directory = await mkdtemp(path.join(tmpdir(), 'control-plane-go-acceptance-'));
  await mkdir(path.join(directory, 'docs', 'release'), {recursive: true});
  await mkdir(path.join(directory, 'proof'), {recursive: true});
  await mkdir(path.join(directory, 'results'), {recursive: true});
  const selectedSource = `package proof

import "testing"

func TestSelected(t *testing.T) {}
`;
  const manualEvidence = '# Go selected-test evidence\n';
  const contract = {
    schema: 'distr.control-plane-acceptance-contract/v1',
    normativeSource: 'normative-plan.md',
    profiles: {
      'go-community': {
        automatedTest: 'proof/proof_test.go',
        manualEvidence: 'evidence.md',
        allowedProofClasses: ['community-focused-test'],
        testRunner: 'go-test',
        selectedTests: ['TestSelected'],
      },
      adopter: {
        automatedTest: 'adopter.test.mjs',
        manualEvidence: 'adopter.md',
        allowedProofClasses: ['adopter-execution'],
        testRunner: 'node-test',
      },
    },
    acceptance: {},
  };
  for (let index = 1; index <= 80; index += 1) {
    const id = `AC-${String(index).padStart(2, '0')}`;
    contract.acceptance[id] =
      id === 'AC-03'
        ? {owner: 'PR-083', profile: 'go-community', pendingAdopter: false}
        : {owner: 'ADOPTER-FIXTURE', profile: 'adopter', pendingAdopter: true};
  }
  mutateContract(contract);
  await writeFile(path.join(directory, 'proof', 'proof_test.go'), selectedSource);
  await writeFile(path.join(directory, 'go.mod'), 'module example.invalid/checkerfixture\n\ngo 1.26.5\n');
  for (const [relativePath, source] of Object.entries(additionalGoSources)) {
    const resolved = path.join(directory, relativePath);
    await mkdir(path.dirname(resolved), {recursive: true});
    await writeFile(resolved, source);
  }
  await writeFile(path.join(directory, 'evidence.md'), manualEvidence);
  await writeFile(
    path.join(directory, 'adopter.test.mjs'),
    "import {test} from 'node:test';\ntest('adopter source', () => {});\n"
  );
  await writeFile(path.join(directory, 'adopter.md'), '# Adopter evidence\n');
  await writeFile(path.join(directory, 'normative-plan.md'), '# Normative plan\n');
  await writeFile(path.join(directory, contractPath), `${JSON.stringify(contract, null, 2)}\n`);
  await git(directory, 'init', '--quiet');
  await git(directory, 'config', 'user.email', 'fixture@example.invalid');
  await git(directory, 'config', 'user.name', 'Acceptance Fixture');
  await git(directory, 'add', '.');
  await git(directory, 'commit', '--quiet', '-m', 'fixture source');
  const {stdout} = await git(directory, 'rev-parse', 'HEAD');
  const sourceCommit = stdout.trim();
  const result = {
    schema: 'distr.control-plane-test-result/v1',
    sourceCommit,
    command: {
      runner: 'go-test',
      argv: ['go', 'test', './proof', '-run', '^(?:TestSelected)$', '-count=1', '-json'],
      selectedTestSource: 'proof/proof_test.go',
      selectedTests: ['TestSelected'],
    },
    exitCode: 0,
    tests: {
      expected: 1,
      passed: 1,
      failed: 0,
      skipped: 0,
      topLevel: [{name: 'TestSelected', status: 'pass'}],
    },
    compiledPackageSources: [{path: 'proof/proof_test.go', sha256: sha256(selectedSource)}],
    status: 'passed',
    startedAt: '2030-01-01T00:00:00.000Z',
    completedAt: '2030-01-01T00:00:01.000Z',
  };
  mutateResult(result);
  const resultBytes = `${JSON.stringify(result, null, 2)}\n`;
  await writeFile(path.join(directory, 'results', 'test-result.json'), resultBytes);
  const artifact = {
    schema: 'distr.control-plane-acceptance-evidence/v1',
    acceptanceId: 'AC-03',
    owner: 'PR-083',
    proofClass: 'community-focused-test',
    sourceCommit,
    automatedTest: {path: 'proof/proof_test.go', sha256: sha256(selectedSource)},
    manualEvidence: {path: 'evidence.md', sha256: sha256(manualEvidence)},
    testResult: {path: 'results/test-result.json', sha256: sha256(resultBytes)},
  };
  const artifactBytes = `${JSON.stringify(artifact, null, 2)}\n`;
  await writeFile(path.join(directory, 'results', 'AC-03.json'), artifactBytes);
  const rows = [];
  for (let index = 1; index <= 80; index += 1) {
    const id = `AC-${String(index).padStart(2, '0')}`;
    rows.push(
      id === 'AC-03'
        ? {
            id,
            owner: 'PR-083',
            automatedTest: 'proof/proof_test.go',
            manualEvidence: 'evidence.md',
            status: 'community-evidence-retained',
            artifact: `results/AC-03.json @ ${sha256(artifactBytes)}`,
          }
        : {
            id,
            owner: 'ADOPTER-FIXTURE',
            automatedTest: 'adopter.test.mjs',
            manualEvidence: 'adopter.md',
            status: 'pending-adopter',
            artifact: 'pending-adopter:ADOPTER-FIXTURE',
          }
    );
  }
  await writeFile(path.join(directory, 'ledger.md'), renderLedger(rows));
  await writeFile(path.join(directory, contractPath), `${JSON.stringify(contract, null, 2)}\n`);
  await git(directory, 'add', '.');
  await git(directory, 'commit', '--quiet', '-m', 'retained selected Go evidence');
  return {directory};
}

function run(directory) {
  return new Promise((resolve) => {
    const child = spawn(process.execPath, [checker, 'ledger.md'], {
      cwd: directory,
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

async function rewriteArtifact(directory, id, mutate, message) {
  const {readFile} = await import('node:fs/promises');
  const artifactPath = path.join(directory, 'results', `${id}.json`);
  const artifact = JSON.parse(await readFile(artifactPath, 'utf8'));
  await mutate(artifact);
  const artifactBytes = `${JSON.stringify(artifact, null, 2)}\n`;
  await writeFile(artifactPath, artifactBytes);
  const ledgerPath = path.join(directory, 'ledger.md');
  const ledger = await readFile(ledgerPath, 'utf8');
  await writeFile(
    ledgerPath,
    ledger.replace(
      new RegExp(`results/${id}\\.json @ sha256:[0-9a-f]{64}`),
      `results/${id}.json @ ${sha256(artifactBytes)}`
    )
  );
  await git(directory, 'add', '.');
  await git(directory, 'commit', '--quiet', '-m', message);
}

async function rewriteClassReport(directory, id, mutate, message) {
  const {readFile} = await import('node:fs/promises');
  await rewriteArtifact(
    directory,
    id,
    async (artifact) => {
      const reportPath = path.join(directory, artifact.classEvidence.path);
      const report = JSON.parse(await readFile(reportPath, 'utf8'));
      mutate(report);
      const reportBytes = `${JSON.stringify(report, null, 2)}\n`;
      await writeFile(reportPath, reportBytes);
      artifact.classEvidence.sha256 = sha256(reportBytes);
    },
    message
  );
}

async function rewriteBrowserEvidence(directory, mutate, message) {
  await rewriteArtifact(
    directory,
    'AC-63',
    async (artifact) => {
      const reportPath = path.join(directory, artifact.classEvidence.path);
      const report = JSON.parse(await readFile(reportPath, 'utf8'));
      const rawPath = path.join(directory, report.rawResult.path);
      const raw = JSON.parse(await readFile(rawPath, 'utf8'));
      await mutate({artifact, report, raw, directory});
      const rawBytes = `${JSON.stringify(raw, null, 2)}\n`;
      await writeFile(rawPath, rawBytes);
      report.rawResult.sha256 = sha256(rawBytes);
      const reportBytes = `${JSON.stringify(report, null, 2)}\n`;
      await writeFile(reportPath, reportBytes);
      artifact.classEvidence.sha256 = sha256(reportBytes);
    },
    message
  );
}

test('accepts exact contracts with tracked checksummed result evidence and nine pending adopter gates', async () => {
  const {directory} = await fixtureWorkspace();

  const result = await run(directory);

  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, /Validated 80 acceptance rows: 71 community-evidence-retained, 9 pending-adopter/);
});

test('rejects a missing acceptance ID', async () => {
  const {directory} = await fixtureWorkspace({mutateRows: (rows) => rows.splice(3, 1)});

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /missing acceptance ID AC-04/);
});

test('rejects a duplicate acceptance ID even when both rows name an owner', async () => {
  const {directory} = await fixtureWorkspace({mutateRows: (rows) => rows.push({...rows[2]})});

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /duplicate acceptance ID AC-03/);
});

test('rejects an owner that differs from the exact normative owner map', async () => {
  const {directory} = await fixtureWorkspace({
    mutateRows: (rows) => {
      rows[2].owner = 'PR-999';
    },
  });

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /AC-03 owner must be PR-083/);
});

test('rejects PR-076 as the primary owner of AC-40 and AC-41', async () => {
  const {directory} = await fixtureWorkspace({
    mutateRows: (rows) => {
      rows[39].owner = 'PR-076';
      rows[40].owner = 'PR-076';
    },
  });

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /AC-40 owner must be PR-073/);
});

test('rejects a test path outside the acceptance contract', async () => {
  const {directory} = await fixtureWorkspace({
    mutateRows: (rows) => {
      rows[2].automatedTest = 'other.test.mjs';
    },
  });

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /AC-03 automated test must be evidence\.test\.mjs/);
});

test('rejects a proof class not allowed for the acceptance ID', async () => {
  const {directory} = await fixtureWorkspace();
  const artifactPath = path.join(directory, 'results', 'AC-03.json');
  const artifact = JSON.parse(await (await import('node:fs/promises')).readFile(artifactPath, 'utf8'));
  artifact.proofClass = 'adopter-execution';
  const artifactBytes = `${JSON.stringify(artifact, null, 2)}\n`;
  await writeFile(artifactPath, artifactBytes);
  const ledgerPath = path.join(directory, 'ledger.md');
  const ledger = await (await import('node:fs/promises')).readFile(ledgerPath, 'utf8');
  await writeFile(
    ledgerPath,
    ledger.replace(/results\/AC-03\.json @ sha256:[0-9a-f]{64}/, `results/AC-03.json @ ${sha256(artifactBytes)}`)
  );
  await git(directory, 'add', '.');
  await git(directory, 'commit', '--quiet', '-m', 'invalid proof class');

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /AC-03 proof class adopter-execution is not allowed/);
});

test('rejects a proof class that has no supported evidence schema', async () => {
  const {directory} = await fixtureWorkspace({
    proofOverride: {id: 'AC-03', proofClass: 'documentation-only'},
  });

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /AC-03 contract proof class is unsupported: documentation-only/);
});

test('rejects a performance profile without its exact scenario binding', async () => {
  const {directory} = await fixtureWorkspace({
    proofOverride: {id: 'AC-03', proofClass: 'performance-measurement'},
  });

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /AC-03 performance profile must declare proofRequirements\.performanceScenario/);
});

test('production contract declares the complete normative AC-50 and AC-51 metric sets', async () => {
  const contract = JSON.parse(
    await readFile(path.join(repoRoot, 'docs', 'release', 'control-plane-acceptance-contract.json'), 'utf8')
  );
  const workloads = [
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
  const fleetMetrics = workloads.flatMap((workload) => [
    {
      name: `${workload}-p95-ms`,
      aggregation: 'p95',
      operator: 'lte',
      limit: 2000,
      unit: 'ms',
      minSamples: 20,
    },
    {
      name: `${workload}-p99-ms`,
      aggregation: 'p99',
      operator: 'lte',
      limit: 5000,
      unit: 'ms',
      minSamples: 20,
    },
  ]);
  assert.deepEqual(contract.profiles['pr081-scale'].proofRequirements, {
    performanceScenario: 'fleet-api-slos',
    performanceMetrics: fleetMetrics,
    performanceFacts: {
      exact: {pageSize: 100, boundedResponses: true, workloads},
      minimum: {},
    },
  });
  assert.deepEqual(contract.profiles['pr081-load'].proofRequirements, {
    performanceScenario: 'roadmap-scale-load',
    performanceMetrics: [
      {
        name: 'plan-create-validate-p95-ms',
        aggregation: 'p95',
        operator: 'lte',
        limit: 10000,
        unit: 'ms',
        minSamples: 5,
      },
      {
        name: 'wave-materialize-schedule-duration-ms',
        aggregation: 'max',
        operator: 'lte',
        limit: 30000,
        unit: 'ms',
        minSamples: 1,
      },
      {
        name: 'event-ingest-ack-p95-ms',
        aggregation: 'p95',
        operator: 'lte',
        limit: 1000,
        unit: 'ms',
        minSamples: 60000,
      },
      {
        name: 'lost-accepted-events',
        aggregation: 'sum',
        operator: 'eq',
        limit: 0,
        unit: 'count',
        minSamples: 1,
      },
      {
        name: 'log-first-page-indexed-ms',
        aggregation: 'max',
        operator: 'lte',
        limit: 2000,
        unit: 'ms',
        minSamples: 1,
      },
      {
        name: 'cross-organization-records',
        aggregation: 'sum',
        operator: 'eq',
        limit: 0,
        unit: 'count',
        minSamples: 1,
      },
      {
        name: 'non-policy-error-rate',
        aggregation: 'max',
        operator: 'lt',
        limit: 0.01,
        unit: 'ratio',
        minSamples: 1,
      },
    ],
    performanceFacts: {
      exact: {
        planRuns: 5,
        deterministicPlanChecksum: true,
        waveStableOrder: true,
        duplicateAdmissions: 0,
        eventDurationSeconds: 600,
        eventRatePerSecond: 100,
        authentication: 'authenticated-live',
        concurrentAgents: 100,
        lostAcceptedEvents: 0,
        logBytes: 104857600,
        logStreamingBoundedMemory: true,
        crossOrganizationRecords: 0,
      },
      minimum: {acceptedEvents: 60000},
    },
  });
});

test('rejects a performance report missing one contract metric and raw series', async () => {
  const {directory} = await fixtureWorkspace({
    proofOverride: {
      id: 'AC-03',
      proofClass: 'performance-measurement',
      proofRequirements: fixtureFleetRequirements,
    },
  });
  await rewriteArtifact(
    directory,
    'AC-03',
    async (artifact) => {
      const reportPath = path.join(directory, artifact.classEvidence.path);
      const report = JSON.parse(await readFile(reportPath, 'utf8'));
      const removed = report.thresholds.pop();
      const rawPath = path.join(directory, report.rawSamples.path);
      const raw = JSON.parse(await readFile(rawPath, 'utf8'));
      delete raw.series[removed.name];
      const rawBytes = `${JSON.stringify(raw, null, 2)}\n`;
      await writeFile(rawPath, rawBytes);
      report.rawSamples.sha256 = sha256(rawBytes);
      const reportBytes = `${JSON.stringify(report, null, 2)}\n`;
      await writeFile(reportPath, reportBytes);
      artifact.classEvidence.sha256 = sha256(reportBytes);
    },
    'remove required performance metric'
  );

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /AC-03 performance metrics must exactly match the contract/);
});

test('rejects non-numeric raw performance samples', async () => {
  const {directory} = await fixtureWorkspace({
    proofOverride: {
      id: 'AC-03',
      proofClass: 'performance-measurement',
      proofRequirements: fixtureFleetRequirements,
    },
  });
  await rewriteArtifact(
    directory,
    'AC-03',
    async (artifact) => {
      const reportPath = path.join(directory, artifact.classEvidence.path);
      const report = JSON.parse(await readFile(reportPath, 'utf8'));
      const rawPath = path.join(directory, report.rawSamples.path);
      const raw = JSON.parse(await readFile(rawPath, 'utf8'));
      raw.series['registry-list-p95-ms'][0] = 'fast';
      const rawBytes = `${JSON.stringify(raw, null, 2)}\n`;
      await writeFile(rawPath, rawBytes);
      report.rawSamples.sha256 = sha256(rawBytes);
      const reportBytes = `${JSON.stringify(report, null, 2)}\n`;
      await writeFile(reportPath, reportBytes);
      artifact.classEvidence.sha256 = sha256(reportBytes);
    },
    'make raw sample non-numeric'
  );

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /AC-03 performance raw series registry-list-p95-ms must contain finite numeric samples/);
});

test('rejects AC-51 evidence shorter than ten minutes at 100 events per second', async () => {
  const {directory} = await fixtureWorkspace({
    proofOverride: {
      id: 'AC-03',
      proofClass: 'performance-measurement',
      proofRequirements: fixtureLoadRequirements,
    },
  });
  await rewriteClassReport(
    directory,
    'AC-03',
    (report) => {
      report.facts.eventDurationSeconds = 60;
    },
    'shorten load duration'
  );

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /AC-03 performance fact eventDurationSeconds must equal 600/);
});

test('rejects AC-51 evidence with non-deterministic plan checksums', async () => {
  const {directory} = await fixtureWorkspace({
    proofOverride: {
      id: 'AC-03',
      proofClass: 'performance-measurement',
      proofRequirements: fixtureLoadRequirements,
    },
  });
  await rewriteClassReport(
    directory,
    'AC-03',
    (report) => {
      report.facts.planChecksums[4] = `sha256:${'f'.repeat(64)}`;
    },
    'change one plan checksum'
  );

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /AC-03 roadmap-scale-load planChecksums must contain five identical SHA-256 values/);
});

test('rejects AC-51 event evidence that is not authenticated-live', async () => {
  const {directory} = await fixtureWorkspace({
    proofOverride: {
      id: 'AC-03',
      proofClass: 'performance-measurement',
      proofRequirements: fixtureLoadRequirements,
    },
  });
  await rewriteClassReport(
    directory,
    'AC-03',
    (report) => {
      report.facts.authentication = 'simulated';
    },
    'downgrade event authentication'
  );

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /AC-03 roadmap-scale-load authentication must be authenticated-live/);
});

test('rejects AC-51 dataset inventory without 100 concurrently authenticated executor identities', async () => {
  const {directory} = await fixtureWorkspace({
    proofOverride: {
      id: 'AC-03',
      proofClass: 'performance-measurement',
      proofRequirements: fixtureLoadRequirements,
    },
  });
  await rewriteClassReport(
    directory,
    'AC-03',
    (report) => {
      report.facts.concurrentAgents = 1;
      report.facts.authenticatedExecutorIds = ['executor-001'];
      report.facts.authenticatedExecutorIdsChecksum = sha256(JSON.stringify(report.facts.authenticatedExecutorIds));
    },
    'reduce active authenticated executors'
  );

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /AC-03 roadmap-scale-load must retain exactly 100 unique authenticatedExecutorIds/);
});

test('rejects performance proof that is not a measured-live threshold result', async () => {
  const {directory} = await fixtureWorkspace({
    proofOverride: {
      id: 'AC-03',
      proofClass: 'performance-measurement',
      proofRequirements: fixtureFleetRequirements,
    },
  });
  await rewriteClassReport(
    directory,
    'AC-03',
    (report) => {
      report.mode = 'simulation';
    },
    'downgrade performance proof to simulation'
  );

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /AC-03 performance report mode must be measured-live/);
});

test('rejects neutral-live proof without two successful separately configured targets', async () => {
  const {directory} = await fixtureWorkspace({
    proofOverride: {id: 'AC-03', proofClass: 'neutral-live-execution'},
  });
  await rewriteClassReport(
    directory,
    'AC-03',
    (report) => {
      report.targets = report.targets.slice(0, 1);
    },
    'remove one neutral target'
  );

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /AC-03 neutral-live report must contain exactly two targets/);
});

test('rejects legacy neutral-live target copies of shared release lineage', async () => {
  const {directory} = await fixtureWorkspace({
    proofOverride: {id: 'AC-03', proofClass: 'neutral-live-execution'},
  });
  await rewriteClassReport(
    directory,
    'AC-03',
    (report) => {
      report.targets[1].releaseLineage = JSON.parse(JSON.stringify(report.releaseLineage));
    },
    'restore obsolete target release lineage copy'
  );

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /AC-03 neutral-live targets must not duplicate shared releaseLineage/);
});

test('rejects legacy neutral-live shared plans in release lineage', async () => {
  const {directory} = await fixtureWorkspace({
    proofOverride: {id: 'AC-03', proofClass: 'neutral-live-execution'},
  });
  await rewriteClassReport(
    directory,
    'AC-03',
    (report) => {
      report.releaseLineage.plans = [
        {
          id: 'fabricated-shared-plan',
          checksum: `sha256:${'f'.repeat(64)}`,
          fromProductReleaseId: 'product-release-a',
          toProductReleaseId: 'product-release-b',
        },
      ];
    },
    'restore obsolete shared plan'
  );

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /AC-03 neutral-live plans must be retained only in target transitions/);
});

test('rejects legacy neutral-live synthetic releaseHistory', async () => {
  const {directory} = await fixtureWorkspace({
    proofOverride: {id: 'AC-03', proofClass: 'neutral-live-execution'},
  });
  await rewriteClassReport(
    directory,
    'AC-03',
    (report) => {
      report.releaseHistory = ['A', 'B', 'A'];
    },
    'restore obsolete synthetic release history'
  );

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /AC-03 neutral-live report must not retain legacy releaseHistory/);
});

test('rejects legacy neutral-live scalar execution and observation identities', async () => {
  const {directory} = await fixtureWorkspace({
    proofOverride: {id: 'AC-03', proofClass: 'neutral-live-execution'},
  });
  await rewriteClassReport(
    directory,
    'AC-03',
    (report) => {
      report.targets[0].executionId = 'obsolete-shared-execution';
      report.targets[0].observationId = 'obsolete-shared-observation';
    },
    'restore obsolete scalar execution evidence'
  );

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(
    result.stderr,
    /AC-03 neutral-live execution and observation evidence must be retained only in target transitions/
  );
});

test('rejects incomplete or reused neutral-live transition evidence', async () => {
  const cases = [
    {
      name: 'non-qualifying live result',
      mutate: (report) => {
        report.acceptanceEligible = false;
      },
      message: /acceptance-eligible passed live-hub-api run/,
    },
    {
      name: 'non-local call',
      mutate: (report) => {
        report.nonLocalCalls = 1;
      },
      message: /complete cleanup and record zero non-local calls/,
    },
    {
      name: 'final release B',
      mutate: (report) => {
        report.targets[0].activeRelease = 'B';
      },
      message: /passed final-A results/,
    },
    {
      name: 'synthetic release history',
      mutate: (report) => {
        report.productReleaseHistory = ['A', 'B', 'A'];
      },
      message: /exact Product Release A-B-A history/,
    },
    {
      name: 'reversed target transition',
      mutate: (report) => {
        report.targets[0].transitions.reverse();
      },
      message: /ordered A-to-B then B-to-A endpoints/,
    },
    {
      name: 'reused target config snapshot',
      mutate: (report) => {
        report.targets[1].configSnapshotId = report.targets[0].configSnapshotId;
      },
      message: /two distinct configSnapshotId values/,
    },
    {
      name: 'reused target plan',
      mutate: (report) => {
        report.targets[1].transitions[0].planId = report.targets[0].transitions[0].planId;
      },
      message: /transition plan IDs must be distinct/,
    },
    {
      name: 'reused execution checksum',
      mutate: (report) => {
        report.targets[1].transitions[0].executions[0].executionChecksum =
          report.targets[0].transitions[0].executions[0].executionChecksum;
      },
      message: /transition execution checksums must be distinct/,
    },
    {
      name: 'reused observation identity',
      mutate: (report) => {
        report.targets[1].transitions[0].observations[0].observationId =
          report.targets[0].transitions[0].observations[0].observationId;
      },
      message: /transition observation IDs must be distinct/,
    },
    {
      name: 'empty observations',
      mutate: (report) => {
        report.targets[0].transitions[0].observations = [];
      },
      message: /transition observations must be non-empty/,
    },
  ];

  for (const testCase of cases) {
    const {directory} = await fixtureWorkspace({
      proofOverride: {id: 'AC-03', proofClass: 'neutral-live-execution'},
    });
    await rewriteClassReport(directory, 'AC-03', testCase.mutate, testCase.name);

    const result = await run(directory);

    assert.notEqual(result.status, 0, testCase.name);
    assert.match(result.stderr, testCase.message, testCase.name);
  }
});

test('rejects browser proof with zero expected tests', async () => {
  const {directory} = await fixtureWorkspace({
    proofOverride: {id: 'AC-63', proofClass: 'browser-e2e'},
  });
  await rewriteClassReport(
    directory,
    'AC-63',
    (report) => {
      report.tests.expected = 0;
      report.tests.passed = 0;
    },
    'erase browser test count'
  );

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /AC-63 browser report must contain exactly one passed test/);
});

test('rejects AC-63 generic evidence that did not run the exact purpose-built Playwright command', async () => {
  for (const [name, mutate] of [
    ['weaken exact browser command', (argv) => argv.filter((argument) => argument !== '--project')],
    ['leak host-specific Node path', (argv) => [process.execPath, ...argv.slice(1)]],
  ]) {
    const {directory} = await fixtureWorkspace({
      proofOverride: {id: 'AC-63', proofClass: 'browser-e2e'},
    });
    await rewriteArtifact(
      directory,
      'AC-63',
      async (artifact) => {
        const resultPath = path.join(directory, artifact.testResult.path);
        const result = JSON.parse(await readFile(resultPath, 'utf8'));
        result.command.argv = mutate(result.command.argv);
        const bytes = `${JSON.stringify(result, null, 2)}\n`;
        await writeFile(resultPath, bytes);
        artifact.testResult.sha256 = sha256(bytes);
      },
      name
    );

    const result = await run(directory);

    assert.notEqual(result.status, 0, name);
    assert.match(result.stderr, /AC-63 playwright argv must exactly run the purpose-built browser evidence test/, name);
  }
});

test('rejects AC-63 browser class evidence with the wrong project, source, or title', async () => {
  for (const [field, mutate] of [
    ['project', ({report}) => (report.project = 'firefox')],
    ['source', ({report}) => (report.testSource = 'frontend/ui/e2e/other.spec.ts')],
    ['title', ({report}) => (report.testTitles = ['@evidence another journey'])],
  ]) {
    const {directory} = await fixtureWorkspace({
      proofOverride: {id: 'AC-63', proofClass: 'browser-e2e'},
    });
    await rewriteBrowserEvidence(directory, mutate, `change browser ${field}`);

    const result = await run(directory);

    assert.notEqual(result.status, 0, field);
    assert.match(result.stderr, /AC-63 browser report must bind the exact project, source, and title/, field);
  }
});

test('rejects AC-63 raw Playwright evidence with a different title or attachment set', async () => {
  for (const [field, mutate] of [
    ['title', ({raw}) => (raw.suites[0].specs[0].title = '@evidence forged journey')],
    ['attachments', ({raw}) => raw.suites[0].specs[0].tests[0].results[0].attachments.pop()],
  ]) {
    const {directory} = await fixtureWorkspace({
      proofOverride: {id: 'AC-63', proofClass: 'browser-e2e'},
    });
    await rewriteBrowserEvidence(directory, mutate, `change raw browser ${field}`);

    const result = await run(directory);

    assert.notEqual(result.status, 0, field);
    assert.match(
      result.stderr,
      /AC-63 raw Playwright report must contain the exact passed test and 11 attachments/,
      field
    );
  }
});

test('rejects AC-63 retained screenshot checksum or PNG dimension drift', async () => {
  for (const [field, mutate, expected] of [
    [
      'checksum',
      async ({report, directory}) => {
        await writeFile(path.join(directory, report.screenshots[0].path), fixturePNG(1200, 1440));
      },
      /AC-63 browser screenshot checksum mismatch/,
    ],
    [
      'dimensions',
      async ({report, directory}) => {
        const bytes = fixturePNG(1, 1);
        await writeFile(path.join(directory, report.screenshots[0].path), bytes);
        report.screenshots[0].sha256 = sha256(bytes);
      },
      /AC-63 browser screenshot PNG dimensions must match 1440x1200/,
    ],
  ]) {
    const {directory} = await fixtureWorkspace({
      proofOverride: {id: 'AC-63', proofClass: 'browser-e2e'},
    });
    await rewriteBrowserEvidence(directory, mutate, `change screenshot ${field}`);

    const result = await run(directory);

    assert.notEqual(result.status, 0, field);
    assert.match(result.stderr, expected, field);
  }
});

test('rejects AC-63 browser proof without exact network isolation, source bindings, and tool identity', async () => {
  for (const [field, mutate, expected] of [
    [
      'network',
      ({report}) => (report.networkProof.externalAttempts = 1),
      /AC-63 browser network proof must bind the exact test assertion and zero external attempts/,
    ],
    [
      'sources',
      ({report}) => report.executionSources.pop(),
      /AC-63 browser execution sources must exactly bind the purpose-built config, fixture, package, and lockfile/,
    ],
    [
      'tools',
      ({report}) => (report.toolVersions.playwright = '9.9.9'),
      /AC-63 browser tool versions must be canonical and match the raw Playwright report/,
    ],
  ]) {
    const {directory} = await fixtureWorkspace({
      proofOverride: {id: 'AC-63', proofClass: 'browser-e2e'},
    });
    await rewriteBrowserEvidence(directory, mutate, `change browser ${field}`);

    const result = await run(directory);

    assert.notEqual(result.status, 0, field);
    assert.match(result.stderr, expected, field);
  }
});

test('rejects AC-63 browser source without the Playwright worker Node version binding', async () => {
  const {directory} = await fixtureWorkspace({
    proofOverride: {id: 'AC-63', proofClass: 'browser-e2e'},
    browserRuntimeBinding: false,
  });

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /AC-63 browser automated test must bind the Playwright worker Node version/);
});

test('rejects AC-63 browser evidence when the source pnpm lock importer does not match its integrity-bound package', async () => {
  const {directory} = await fixtureWorkspace({
    proofOverride: {id: 'AC-63', proofClass: 'browser-e2e'},
    browserLockImporterVersion: '1.51.0',
  });

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(
    result.stderr,
    /AC-63 browser Playwright version must match the source pnpm lock importer and integrity-bound package/
  );
});

test('rejects structurally invalid, truncated, bad-CRC, missing-IEND, and secret-bearing PNG evidence', async () => {
  const mutations = [
    [
      'signature',
      () => {
        const bytes = fixturePNG();
        bytes[0] ^= 0xff;
        return bytes;
      },
      /AC-63 browser screenshot PNG is invalid/,
    ],
    ['truncated', () => fixturePNG().subarray(0, fixturePNG().length - 5), /AC-63 browser screenshot PNG is invalid/],
    [
      'CRC',
      () => {
        const bytes = fixturePNG();
        bytes[29] ^= 0xff;
        return bytes;
      },
      /AC-63 browser screenshot PNG is invalid/,
    ],
    ['IEND', () => fixturePNG().subarray(0, fixturePNG().length - 12), /AC-63 browser screenshot PNG is invalid/],
    ['secret', () => fixturePNG(1440, 1200, {secret: true}), /AC-63 browser screenshot metadata contains a secret/],
  ];
  for (const [name, makeBytes, expected] of mutations) {
    const {directory} = await fixtureWorkspace({
      proofOverride: {id: 'AC-63', proofClass: 'browser-e2e'},
    });
    await rewriteBrowserEvidence(
      directory,
      async ({report}) => {
        const bytes = makeBytes();
        await writeFile(path.join(directory, report.screenshots[0].path), bytes);
        report.screenshots[0].sha256 = sha256(bytes);
      },
      `forge PNG ${name}`
    );

    const result = await run(directory);

    assert.notEqual(result.status, 0, name);
    assert.match(result.stderr, expected, name);
  }
});

test('accepts an absolute raw spec path only when it normalizes through the declared isolated root', async () => {
  const {directory} = await fixtureWorkspace({
    proofOverride: {id: 'AC-63', proofClass: 'browser-e2e'},
  });
  await rewriteBrowserEvidence(
    directory,
    ({raw}) => {
      raw.config.rootDir = path.join(path.parse(directory).root, 'isolated-ac63-source');
      raw.suites[0].specs[0].file = path.join(raw.config.rootDir, ...browserAutomatedTest.split('/'));
    },
    'retain absolute isolated source path'
  );

  const result = await run(directory);

  assert.equal(result.status, 0, result.stderr);
});

test('rejects an absolute raw spec path outside the declared isolated root', async () => {
  const {directory} = await fixtureWorkspace({
    proofOverride: {id: 'AC-63', proofClass: 'browser-e2e'},
  });
  await rewriteBrowserEvidence(
    directory,
    ({raw}) => {
      raw.config.rootDir = path.join(path.parse(directory).root, 'isolated-ac63-source');
      raw.suites[0].specs[0].file = path.join(
        path.parse(directory).root,
        'outside',
        ...browserAutomatedTest.split('/')
      );
    },
    'escape absolute isolated source path'
  );

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /AC-63 raw Playwright report source must normalize to the bound automated test/);
});

test('rejects secret-like values anywhere in retained raw Playwright JSON', async () => {
  const {directory} = await fixtureWorkspace({
    proofOverride: {id: 'AC-63', proofClass: 'browser-e2e'},
  });
  await rewriteBrowserEvidence(
    directory,
    ({raw}) => {
      raw.config.metadata = {nested: [{apiToken: 'browser-secret-value'}]};
    },
    'inject retained raw secret'
  );

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /AC-63 retained Playwright JSON contains a secret-like value/);
});

test('rejects retained raw and screenshot bindings through directory junctions', async () => {
  for (const kind of ['raw', 'screenshots']) {
    const {directory} = await fixtureWorkspace({
      proofOverride: {id: 'AC-63', proofClass: 'browser-e2e'},
    });
    const sourceDirectory = path.join(directory, 'results', kind);
    const escaped = await mkdtemp(path.join(tmpdir(), `acceptance-${kind}-escape-`));
    const files = kind === 'raw' ? ['browser-raw.json'] : browserScreenshotNames;
    for (const file of files) {
      await writeFile(path.join(escaped, file), await readFile(path.join(sourceDirectory, file)));
    }
    await rm(sourceDirectory, {recursive: true});
    await symlink(escaped, sourceDirectory, process.platform === 'win32' ? 'junction' : 'dir');

    const result = await run(directory);

    assert.notEqual(result.status, 0, kind);
    assert.match(
      result.stderr,
      /AC-63 browser (?:raw Playwright report|screenshot) must not traverse a reparse point/,
      kind
    );
  }
});

test('rejects generic/raw timestamp drift and a generic AC-63 count other than exactly one pass', async () => {
  for (const field of ['timestamp', 'count']) {
    const {directory} = await fixtureWorkspace({
      proofOverride: {id: 'AC-63', proofClass: 'browser-e2e'},
    });
    await rewriteArtifact(
      directory,
      'AC-63',
      async (artifact) => {
        const resultPath = path.join(directory, artifact.testResult.path);
        const generic = JSON.parse(await readFile(resultPath, 'utf8'));
        if (field === 'timestamp') generic.completedAt = '2030-01-01T00:00:02.000Z';
        else generic.tests = {expected: 2, passed: 2, failed: 0, skipped: 0};
        const bytes = `${JSON.stringify(generic, null, 2)}\n`;
        await writeFile(resultPath, bytes);
        artifact.testResult.sha256 = sha256(bytes);
      },
      `drift browser generic ${field}`
    );

    const result = await run(directory);

    assert.notEqual(result.status, 0, field);
    assert.match(result.stderr, /AC-63 generic and raw browser result counts and timestamps must exactly match/, field);
  }
});

test('rejects a pnpm tool version that differs from the source-bound packageManager', async () => {
  const {directory} = await fixtureWorkspace({
    proofOverride: {id: 'AC-63', proofClass: 'browser-e2e'},
  });
  await rewriteBrowserEvidence(
    directory,
    ({report, raw}) => {
      report.toolVersions.pnpm = '12.0.0';
      report.toolVersions.playwright = '2.0.0';
      raw.config.version = '2.0.0';
    },
    'drift tool identities'
  );

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /AC-63 browser pnpm version must match source-bound packageManager/);
});

test('accepts valid performance neutral-live and browser class evidence contracts', async () => {
  const cases = [
    {
      id: 'AC-03',
      proofClass: 'performance-measurement',
      proofRequirements: fixtureFleetRequirements,
    },
    {id: 'AC-03', proofClass: 'neutral-live-execution'},
    {id: 'AC-63', proofClass: 'browser-e2e'},
  ];
  for (const proofOverride of cases) {
    const {directory} = await fixtureWorkspace({proofOverride});
    const result = await run(directory);
    assert.equal(result.status, 0, `${proofOverride.proofClass}: ${result.stderr}`);
  }
});

test('rejects an untracked test-result artifact', async () => {
  const {directory} = await fixtureWorkspace();
  await writeFile(
    path.join(directory, 'results', 'untracked.json'),
    `${JSON.stringify(
      {
        schema: 'distr.control-plane-test-result/v1',
        sourceCommit: '0'.repeat(40),
        command: 'node --test evidence.test.mjs',
        status: 'passed',
        startedAt: '2030-01-01T00:00:00.000Z',
        completedAt: '2030-01-01T00:00:01.000Z',
      },
      null,
      2
    )}\n`
  );
  const artifactPath = path.join(directory, 'results', 'AC-03.json');
  const {readFile} = await import('node:fs/promises');
  const artifact = JSON.parse(await readFile(artifactPath, 'utf8'));
  const untrackedBytes = await readFile(path.join(directory, 'results', 'untracked.json'));
  artifact.testResult = {path: 'results/untracked.json', sha256: sha256(untrackedBytes)};
  const artifactBytes = `${JSON.stringify(artifact, null, 2)}\n`;
  await writeFile(artifactPath, artifactBytes);
  const ledgerPath = path.join(directory, 'ledger.md');
  const ledger = await readFile(ledgerPath, 'utf8');
  await writeFile(
    ledgerPath,
    ledger.replace(/results\/AC-03\.json @ sha256:[0-9a-f]{64}/, `results/AC-03.json @ ${sha256(artifactBytes)}`)
  );
  await git(directory, 'add', 'results/AC-03.json', 'ledger.md');
  await git(directory, 'commit', '--quiet', '-m', 'reference untracked result');

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /AC-03 test result must be tracked by git: results\/untracked\.json/);
});

test('rejects a passed result whose structured command does not execute the declared test', async () => {
  const {directory} = await fixtureWorkspace();
  await rewriteArtifact(
    directory,
    'AC-03',
    async (artifact) => {
      const {readFile} = await import('node:fs/promises');
      const resultPath = path.join(directory, artifact.testResult.path);
      const testResult = JSON.parse(await readFile(resultPath, 'utf8'));
      testResult.command = {
        runner: 'shell',
        argv: ['echo', 'passed'],
        selectedTestSource: 'evidence.test.mjs',
      };
      const resultBytes = `${JSON.stringify(testResult, null, 2)}\n`;
      await writeFile(resultPath, resultBytes);
      artifact.testResult.sha256 = sha256(resultBytes);
    },
    'replace test command with echo'
  );

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /AC-03 test result runner must be node-test/);
});

test('rejects a Go community profile without an exact selected test set', async () => {
  const {directory} = await goSelectedFixtureWorkspace({
    mutateContract: (contract) => {
      delete contract.profiles['go-community'].selectedTests;
    },
  });

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /AC-03 contract selectedTests must contain unique Go test names/);
});

test('rejects a Go result whose argv omits the exact anchored selected-test filter', async () => {
  const {directory} = await goSelectedFixtureWorkspace({
    mutateResult: (result) => {
      result.command.argv = ['go', 'test', './proof', '-count=1', '-json'];
    },
  });

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /AC-03 go-test argv must exactly select the declared tests from proof\/proof_test\.go/);
});

test('rejects a Go result whose observed top-level set differs from the declared tests', async () => {
  const {directory} = await goSelectedFixtureWorkspace({
    mutateResult: (result) => {
      result.tests.topLevel = [{name: 'TestOther', status: 'pass'}];
    },
  });

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /AC-03 observed top-level Go tests must exactly match selectedTests/);
});

test('rejects selected Go tests that are not AST declarations in the bound automated test', async () => {
  const {directory} = await goSelectedFixtureWorkspace({
    mutateContract: (contract) => {
      contract.profiles['go-community'].selectedTests = ['TestImaginary'];
    },
    mutateResult: (result) => {
      result.command.argv = ['go', 'test', './proof', '-run', '^(?:TestImaginary)$', '-count=1', '-json'];
      result.command.selectedTests = ['TestImaginary'];
      result.tests.topLevel = [{name: 'TestImaginary', status: 'pass'}];
    },
  });

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(
    result.stderr,
    /AC-03 selectedTests are not declared top-level Go tests in bound proof\/proof_test\.go: TestImaginary/
  );
});

test('rejects a Go result whose compiled package source manifest omits the bound source', async () => {
  const {directory} = await goSelectedFixtureWorkspace({
    mutateResult: (result) => {
      result.compiledPackageSources = [];
    },
  });

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /AC-03 compiled Go package source manifest must include proof\/proof_test\.go/);
});

test('rejects a Go manifest that omits a compiled external test with a duplicate selected declaration', async () => {
  const {directory} = await goSelectedFixtureWorkspace({
    additionalGoSources: {
      'proof/duplicate_external_test.go': `package proof_test

import "testing"

func TestSelected(t *testing.T) {}
`,
    },
  });

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /AC-03 compiled Go package source manifest must exactly match go list at sourceCommit/);
});

test('rejects verified-adopter without adopter-specific execution identities', async () => {
  const {directory} = await fixtureWorkspace({verifiedAdopter: 'AC-01'});
  const artifactPath = path.join(directory, 'results', 'AC-01.json');
  const {readFile} = await import('node:fs/promises');
  const artifact = JSON.parse(await readFile(artifactPath, 'utf8'));
  delete artifact.adopterExecution.campaignIds;
  const artifactBytes = `${JSON.stringify(artifact, null, 2)}\n`;
  await writeFile(artifactPath, artifactBytes);
  const ledgerPath = path.join(directory, 'ledger.md');
  const ledger = await readFile(ledgerPath, 'utf8');
  await writeFile(
    ledgerPath,
    ledger.replace(/results\/AC-01\.json @ sha256:[0-9a-f]{64}/, `results/AC-01.json @ ${sha256(artifactBytes)}`)
  );
  await git(directory, 'add', '.');
  await git(directory, 'commit', '--quiet', '-m', 'remove adopter campaign identity');

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /AC-01 verified-adopter evidence must include non-empty campaignIds/);
});

test('rejects verified-adopter without a tracked checksummed execution observation and audit bundle', async () => {
  const {directory} = await fixtureWorkspace({verifiedAdopter: 'AC-01'});
  await rewriteArtifact(
    directory,
    'AC-01',
    (artifact) => {
      delete artifact.adopterExecution.bundle;
    },
    'remove adopter execution bundle'
  );

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /AC-01 adopter execution bundle must contain a repository path and SHA-256 checksum/);
});

test('accepts verified-adopter with an exact tracked execution observation and audit bundle', async () => {
  const {directory} = await fixtureWorkspace({verifiedAdopter: 'AC-01'});

  const result = await run(directory);

  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, /8 pending-adopter, 1 verified-adopter/);
});

test('rejects a source commit that does not contain the checksummed test source', async () => {
  const {directory} = await fixtureWorkspace();
  const artifactPath = path.join(directory, 'results', 'AC-03.json');
  const {readFile} = await import('node:fs/promises');
  const changedTest = "import {test} from 'node:test';\ntest('changed proof', () => {});\n";
  await writeFile(path.join(directory, 'evidence.test.mjs'), changedTest);
  const artifact = JSON.parse(await readFile(artifactPath, 'utf8'));
  artifact.automatedTest.sha256 = sha256(changedTest);
  const artifactBytes = `${JSON.stringify(artifact, null, 2)}\n`;
  await writeFile(artifactPath, artifactBytes);
  const ledgerPath = path.join(directory, 'ledger.md');
  const ledger = await readFile(ledgerPath, 'utf8');
  await writeFile(
    ledgerPath,
    ledger.replace(/results\/AC-03\.json @ sha256:[0-9a-f]{64}/, `results/AC-03.json @ ${sha256(artifactBytes)}`)
  );
  await git(directory, 'add', '.');
  await git(directory, 'commit', '--quiet', '-m', 'break source checksum');

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /AC-03 automated test checksum mismatch at source commit/);
});
