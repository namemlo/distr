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
const generator = path.join(repoRoot, 'hack', 'control-plane-acceptance-evidence.mjs');

function sha256(value) {
  return `sha256:${createHash('sha256').update(value).digest('hex')}`;
}

function communityCommand() {
  return {
    runner: 'node-test',
    argv: ['node', '--test', '--test-reporter=tap', 'proof.test.mjs'],
    selectedTestSource: 'proof.test.mjs',
  };
}

async function git(directory, ...args) {
  return execFileAsync('git', args, {cwd: directory});
}

function renderLedger(rows) {
  return [
    '# Fixture acceptance ledger',
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

function parseRows(markdown) {
  return markdown
    .split(/\r?\n/)
    .filter((line) => /^\| `AC-\d{2}`/.test(line))
    .map((line) => {
      const cells = line
        .trim()
        .slice(1, -1)
        .split('|')
        .map((cell) => cell.trim().replace(/^`|`$/g, ''));
      return {
        id: cells[0],
        owner: cells[1],
        automatedTest: cells[2],
        manualEvidence: cells[3],
        status: cells[4],
        artifact: cells[5],
      };
    });
}

function fixtureContract() {
  const acceptance = {};
  for (let index = 1; index <= 80; index += 1) {
    const id = `AC-${String(index).padStart(2, '0')}`;
    if (id === 'AC-03' || id === 'AC-04') {
      acceptance[id] = {owner: 'PR-057', profile: 'community-proof', pendingAdopter: false};
    } else if (id === 'AC-50') {
      acceptance[id] = {owner: 'PR-081', profile: 'performance-proof', pendingAdopter: false};
    } else {
      acceptance[id] = {owner: 'ADOPTER-FIXTURE', profile: 'adopter-proof', pendingAdopter: true};
    }
  }
  return {
    schema: 'distr.control-plane-acceptance-contract/v1',
    normativeSource: 'normative-plan.md',
    profiles: {
      'community-proof': {
        automatedTest: 'proof.test.mjs',
        manualEvidence: 'evidence.md',
        allowedProofClasses: ['community-focused-test'],
        testRunner: 'node-test',
      },
      'performance-proof': {
        automatedTest: 'performance.test.mjs',
        manualEvidence: 'performance.md',
        allowedProofClasses: ['performance-measurement'],
        testRunner: 'node-test',
        proofRequirements: {
          performanceScenario: 'fixture',
          performanceMetrics: [
            {
              name: 'fixture-p95-ms',
              aggregation: 'p95',
              operator: 'lte',
              limit: 100,
              unit: 'ms',
              minSamples: 1,
            },
          ],
          performanceFacts: {exact: {}, minimum: {}},
        },
      },
      'adopter-proof': {
        automatedTest: 'adopter.test.mjs',
        manualEvidence: 'adopter.md',
        allowedProofClasses: ['adopter-execution'],
        testRunner: 'node-test',
      },
    },
    acceptance,
  };
}

async function fixtureWorkspace({proofSource = "import {test} from 'node:test';\ntest('proof', () => {});\n"} = {}) {
  const directory = await mkdtemp(path.join(tmpdir(), 'control-plane-evidence-'));
  await mkdir(path.join(directory, 'docs', 'release'), {recursive: true});
  const contract = fixtureContract();
  const rows = Object.entries(contract.acceptance).map(([id, rule]) => {
    const profile = contract.profiles[rule.profile];
    if (id === 'AC-03' || id === 'AC-04' || id === 'AC-50') {
      return {
        id,
        owner: rule.owner,
        automatedTest: profile.automatedTest,
        manualEvidence: profile.manualEvidence,
        status: 'pending-community-evidence',
        artifact: `pending-community-evidence:${rule.owner}`,
      };
    }
    return {
      id,
      owner: rule.owner,
      automatedTest: profile.automatedTest,
      manualEvidence: profile.manualEvidence,
      status: 'pending-adopter',
      artifact: `pending-adopter:${rule.owner}`,
    };
  });
  await writeFile(
    path.join(directory, 'docs', 'release', 'control-plane-acceptance-contract.json'),
    `${JSON.stringify(contract, null, 2)}\n`
  );
  await writeFile(
    path.join(directory, 'docs', 'release', 'enterprise-control-plane-acceptance.md'),
    renderLedger(rows)
  );
  await writeFile(path.join(directory, 'proof.test.mjs'), proofSource);
  await writeFile(
    path.join(directory, 'performance.test.mjs'),
    "import {test} from 'node:test';\ntest('performance source only', () => {});\n"
  );
  await writeFile(
    path.join(directory, 'adopter.test.mjs'),
    "import {test} from 'node:test';\ntest('adopter source only', () => {});\n"
  );
  await writeFile(path.join(directory, 'evidence.md'), '# Community fixture evidence\n');
  await writeFile(path.join(directory, 'performance.md'), '# Performance evidence is not generated here\n');
  await writeFile(path.join(directory, 'adopter.md'), '# Adopter evidence is not generated here\n');
  await writeFile(path.join(directory, 'normative-plan.md'), '# Fixture normative plan\n');
  await git(directory, 'init', '--quiet');
  await git(directory, 'config', 'user.email', 'fixture@example.invalid');
  await git(directory, 'config', 'user.name', 'Acceptance Evidence Fixture');
  await git(directory, 'add', '.');
  await git(directory, 'commit', '--quiet', '-m', 'fixture source');
  const {stdout} = await git(directory, 'rev-parse', 'HEAD');
  return {directory, sourceCommit: stdout.trim()};
}

function runGenerator(directory, sourceCommit, extraArguments = []) {
  const args = [
    generator,
    '--source-commit',
    sourceCommit,
    ...extraArguments,
    'docs/release/enterprise-control-plane-acceptance.md',
  ];
  return new Promise((resolve) => {
    const child = spawn(process.execPath, args, {cwd: directory, stdio: ['ignore', 'pipe', 'pipe']});
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

test('runs each exact community command once, stages deterministic evidence, and promotes only matching rows', async () => {
  const {directory, sourceCommit} = await fixtureWorkspace();

  const result = await runGenerator(directory, sourceCommit);

  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, /Generated 2 community evidence rows from 1 executed command/);
  const ledgerPath = path.join(directory, 'docs', 'release', 'enterprise-control-plane-acceptance.md');
  const rows = parseRows(await readFile(ledgerPath, 'utf8'));
  for (const id of ['AC-03', 'AC-04']) {
    const row = rows.find((candidate) => candidate.id === id);
    assert.equal(row.status, 'community-evidence-retained');
    assert.match(row.artifact, new RegExp(`^docs/release/evidence/${id}\\.json @ sha256:[0-9a-f]{64}$`));
  }
  assert.equal(rows.find((row) => row.id === 'AC-50').status, 'pending-community-evidence');
  assert.equal(rows.find((row) => row.id === 'AC-01').status, 'pending-adopter');
  const resultFiles = (
    await (await import('node:fs/promises')).readdir(path.join(directory, 'docs', 'release', 'evidence', 'results'))
  ).filter((name) => name.endsWith('.json'));
  assert.equal(resultFiles.length, 1);
  const resultBytes = await readFile(path.join(directory, 'docs', 'release', 'evidence', 'results', resultFiles[0]));
  const retainedResult = JSON.parse(resultBytes);
  assert.equal(retainedResult.schema, 'distr.control-plane-test-result/v1');
  assert.equal(retainedResult.sourceCommit, sourceCommit);
  assert.equal(retainedResult.tests.expected, 1);
  assert.equal(retainedResult.tests.skipped, 0);
  for (const id of ['AC-03', 'AC-04']) {
    const artifactPath = path.join(directory, 'docs', 'release', 'evidence', `${id}.json`);
    const artifact = JSON.parse(await readFile(artifactPath, 'utf8'));
    assert.equal(artifact.acceptanceId, id);
    assert.equal(artifact.proofClass, 'community-focused-test');
    assert.equal(artifact.sourceCommit, sourceCommit);
    assert.equal(artifact.automatedTest.sha256, sha256(await readFile(path.join(directory, 'proof.test.mjs'))));
    assert.equal(artifact.manualEvidence.sha256, sha256(await readFile(path.join(directory, 'evidence.md'))));
    assert.equal(artifact.testResult.sha256, sha256(resultBytes));
  }
  const {stdout: staged} = await git(directory, 'diff', '--cached', '--name-only');
  const stagedPaths = staged.trim().split(/\r?\n/);
  assert.deepEqual(
    stagedPaths.sort(),
    [
      'docs/release/enterprise-control-plane-acceptance.md',
      'docs/release/evidence/AC-03.json',
      'docs/release/evidence/AC-04.json',
      `docs/release/evidence/results/${resultFiles[0]}`,
    ].sort()
  );
});

test('fails without promoting when the executed command reports zero tests', async () => {
  const {directory, sourceCommit} = await fixtureWorkspace({proofSource: '// deliberately no tests\n'});

  const result = await runGenerator(directory, sourceCommit);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /community command returned zero tests/);
  const rows = parseRows(
    await readFile(path.join(directory, 'docs', 'release', 'enterprise-control-plane-acceptance.md'), 'utf8')
  );
  assert.equal(rows.find((row) => row.id === 'AC-03').status, 'pending-community-evidence');
});

test('fails without promoting when the executed command skips a test', async () => {
  const {directory, sourceCommit} = await fixtureWorkspace({
    proofSource: "import {test} from 'node:test';\ntest.skip('skipped proof', () => {});\n",
  });

  const result = await runGenerator(directory, sourceCommit);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /community command reported skipped tests/);
});

test('fails before execution when a relevant source file drifted from the selected commit', async () => {
  const {directory, sourceCommit} = await fixtureWorkspace();
  await writeFile(
    path.join(directory, 'proof.test.mjs'),
    "import {test} from 'node:test';\ntest('dirty proof', () => {});\n"
  );

  const result = await runGenerator(directory, sourceCommit);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /relevant path is dirty: proof\.test\.mjs/);
});

test('fails direct execution when HEAD is newer than the attributed source commit', async () => {
  const {directory, sourceCommit} = await fixtureWorkspace();
  await writeFile(path.join(directory, 'later-change.md'), '# A later tracked implementation change\n');
  await git(directory, 'add', 'later-change.md');
  await git(directory, 'commit', '--quiet', '-m', 'later implementation');

  const result = await runGenerator(directory, sourceCommit);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /execution source commit must equal HEAD/);
});

test('fails direct execution when any tracked worktree path is dirty', async () => {
  const {directory, sourceCommit} = await fixtureWorkspace();
  await writeFile(path.join(directory, 'adopter.md'), '# Dirty but otherwise unrelated tracked path\n');

  const result = await runGenerator(directory, sourceCommit);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /tracked worktree is dirty: adopter\.md/);
});

test('rejects a synthetic imported pass instead of bypassing direct execution', async () => {
  const {directory, sourceCommit} = await fixtureWorkspace({
    proofSource: "throw new Error('this source must not execute during import');\n",
  });
  const command = communityCommand();
  const importedResult = {
    schema: 'distr.control-plane-test-result/v1',
    sourceCommit,
    command,
    exitCode: 0,
    tests: {expected: 1, passed: 1, failed: 0, skipped: 0},
    status: 'passed',
    startedAt: '2026-07-29T00:00:00.000Z',
    completedAt: '2026-07-29T00:00:01.000Z',
  };
  const importedBytes = Buffer.from(`${JSON.stringify(importedResult, null, 2)}\n`);
  const importedPath = 'docs/release/evidence/imported-result.json';
  await mkdir(path.join(directory, 'docs', 'release', 'evidence'), {recursive: true});
  await writeFile(path.join(directory, importedPath), importedBytes);
  const importManifest = {
    schema: 'distr.control-plane-test-result-import/v1',
    sourceCommit,
    results: [
      {
        commandKey: sha256(JSON.stringify(command)),
        path: importedPath,
        sha256: sha256(importedBytes),
      },
    ],
  };
  await writeFile(path.join(directory, 'imports.json'), `${JSON.stringify(importManifest, null, 2)}\n`);
  await git(directory, 'add', 'imports.json', importedPath);
  await git(directory, 'commit', '--quiet', '-m', 'retained imported result');

  const result = await runGenerator(directory, sourceCommit, ['--import-manifest', 'imports.json']);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /unsupported option: --import-manifest/);
  const rows = parseRows(
    await readFile(path.join(directory, 'docs', 'release', 'enterprise-control-plane-acceptance.md'), 'utf8')
  );
  assert.equal(rows.find((row) => row.id === 'AC-03').status, 'pending-community-evidence');
});
