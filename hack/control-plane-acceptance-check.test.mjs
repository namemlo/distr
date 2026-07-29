import assert from 'node:assert/strict';
import {execFile, spawn} from 'node:child_process';
import {createHash} from 'node:crypto';
import {mkdir, mkdtemp, readFile, writeFile} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {test} from 'node:test';
import {fileURLToPath} from 'node:url';
import {promisify} from 'node:util';

const execFileAsync = promisify(execFile);
const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const checker = path.join(repoRoot, 'hack', 'control-plane-acceptance-check.mjs');
const contractPath = 'docs/release/control-plane-acceptance-contract.json';
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
          automatedTest: 'evidence.test.mjs',
          manualEvidence: 'evidence.md',
          allowedProofClasses: [proofOverride.proofClass],
          testRunner: 'node-test',
          proofRequirements: proofOverride.proofRequirements ?? {},
        },
      },
      acceptance,
    };
    contract.acceptance[proofOverride.id].profile = 'special-proof';
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

async function fixtureWorkspace({mutateRows = () => {}, verifiedAdopter, proofOverride} = {}) {
  const directory = await mkdtemp(path.join(tmpdir(), 'control-plane-acceptance-'));
  await mkdir(path.join(directory, 'docs', 'release'), {recursive: true});
  await mkdir(path.join(directory, 'results'), {recursive: true});
  const evidence = '# Retained fixture evidence\n';
  const automatedTest = "import {test} from 'node:test';\ntest('proof', () => {});\n";
  const contract = fixtureContract(proofOverride);
  await writeFile(path.join(directory, 'evidence.test.mjs'), automatedTest);
  await writeFile(path.join(directory, 'evidence.md'), evidence);
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
          id: 'component-release-a',
          version: '1.0.0',
          artifactDigest: `sha256:${'a'.repeat(64)}`,
        },
        {
          id: 'component-release-b',
          version: '1.1.0',
          artifactDigest: `sha256:${'b'.repeat(64)}`,
        },
      ],
      productReleases: [
        {
          id: 'product-release-a',
          version: '1.0.0',
          manifestChecksum: `sha256:${'3'.repeat(64)}`,
          graphChecksum: `sha256:${'4'.repeat(64)}`,
          componentReleaseIds: ['component-release-a'],
        },
        {
          id: 'product-release-b',
          version: '1.1.0',
          manifestChecksum: `sha256:${'5'.repeat(64)}`,
          graphChecksum: `sha256:${'6'.repeat(64)}`,
          componentReleaseIds: ['component-release-b'],
        },
      ],
      plans: [
        {
          id: 'plan-a-to-b',
          checksum: `sha256:${'7'.repeat(64)}`,
          fromProductReleaseId: 'product-release-a',
          toProductReleaseId: 'product-release-b',
        },
        {
          id: 'plan-b-to-a',
          checksum: `sha256:${'8'.repeat(64)}`,
          fromProductReleaseId: 'product-release-b',
          toProductReleaseId: 'product-release-a',
        },
      ],
    };
    const classReport = {
      schema: 'distr.control-plane-neutral-live-result/v1',
      sourceCommit,
      proofMode: 'live-hub-api',
      status: 'passed',
      liveStack: {started: true},
      releaseLineage,
      targets: [
        {
          targetId: 'target-alpha',
          configChecksum: `sha256:${'1'.repeat(64)}`,
          adapterKind: 'external-executor',
          executorId: 'executor-alpha',
          observerId: 'observer-alpha',
          executionId: 'execution-alpha',
          observationId: 'observation-alpha',
          status: 'passed',
          releaseLineage: JSON.parse(JSON.stringify(releaseLineage)),
        },
        {
          targetId: 'target-beta',
          configChecksum: `sha256:${'2'.repeat(64)}`,
          adapterKind: 'reference',
          executorId: 'executor-beta',
          observerId: 'observer-beta',
          executionId: 'execution-beta',
          observationId: 'observation-beta',
          status: 'passed',
          releaseLineage: JSON.parse(JSON.stringify(releaseLineage)),
        },
      ],
      releaseHistory: ['product-release-a', 'product-release-b', 'product-release-a'],
      cleanup: {completed: true},
      nonLocalCalls: 0,
    };
    const bytes = `${JSON.stringify(classReport, null, 2)}\n`;
    await writeFile(path.join(directory, 'results', 'class-report.json'), bytes);
    classEvidence = {path: 'results/class-report.json', sha256: sha256(bytes)};
  } else if (proofOverride?.proofClass === 'browser-e2e') {
    const classReport = {
      schema: 'distr.control-plane-browser-e2e-result/v1',
      sourceCommit,
      runner: 'playwright',
      status: 'passed',
      tests: {expected: 12, passed: 12, unexpected: 0, flaky: 0, skipped: 0},
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
    const artifact = {
      schema: 'distr.control-plane-acceptance-evidence/v1',
      acceptanceId: id,
      owner: rule.owner,
      proofClass,
      sourceCommit,
      automatedTest: {path: 'evidence.test.mjs', sha256: sha256(automatedTest)},
      manualEvidence: {path: 'evidence.md', sha256: sha256(evidence)},
      testResult: {path: 'results/test-result.json', sha256: sha256(resultBytes)},
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
      automatedTest: 'evidence.test.mjs',
      manualEvidence: 'evidence.md',
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

test('rejects neutral-live targets that do not share exact immutable release lineage', async () => {
  const {directory} = await fixtureWorkspace({
    proofOverride: {id: 'AC-03', proofClass: 'neutral-live-execution'},
  });
  await rewriteClassReport(
    directory,
    'AC-03',
    (report) => {
      report.targets[1].releaseLineage.productReleases[1].id = 'different-product-release';
    },
    'split neutral release lineage'
  );

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /AC-03 neutral-live target target-beta release lineage must match the shared lineage/);
});

test('rejects browser proof with zero expected tests', async () => {
  const {directory} = await fixtureWorkspace({
    proofOverride: {id: 'AC-03', proofClass: 'browser-e2e'},
  });
  await rewriteClassReport(
    directory,
    'AC-03',
    (report) => {
      report.tests.expected = 0;
      report.tests.passed = 0;
    },
    'erase browser test count'
  );

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /AC-03 browser report must have expected tests greater than zero/);
});

test('accepts valid performance neutral-live and browser class evidence contracts', async () => {
  const cases = [
    {
      id: 'AC-03',
      proofClass: 'performance-measurement',
      proofRequirements: fixtureFleetRequirements,
    },
    {id: 'AC-03', proofClass: 'neutral-live-execution'},
    {id: 'AC-03', proofClass: 'browser-e2e'},
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
