import assert from 'node:assert/strict';
import {execFile, spawn} from 'node:child_process';
import {createHash} from 'node:crypto';
import {mkdir, mkdtemp, readdir, readFile, stat, symlink, writeFile} from 'node:fs/promises';
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
        `| \`${row.id}\`       | \`${row.owner}\`     | \`${row.automatedTest}\` | \`${row.manualEvidence}\` | \`${row.status}\` | \`${row.artifact}\` |`
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

function fixtureContract(communityProfile) {
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
        ...communityProfile,
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

async function fixtureWorkspace({
  proofSource = "import {test} from 'node:test';\ntest('proof', () => {});\n",
  communityProfile,
  additionalFiles = {},
} = {}) {
  const directory = await mkdtemp(path.join(tmpdir(), 'control-plane-evidence-'));
  await mkdir(path.join(directory, 'docs', 'release'), {recursive: true});
  const contract = fixtureContract(communityProfile);
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
  const proofPath = contract.profiles['community-proof'].automatedTest;
  await mkdir(path.dirname(path.join(directory, proofPath)), {recursive: true});
  await writeFile(path.join(directory, proofPath), proofSource);
  if (contract.profiles['community-proof'].testRunner === 'go-test') {
    await writeFile(path.join(directory, 'go.mod'), 'module example.invalid/acceptancefixture\n\ngo 1.26.5\n');
  }
  for (const [relativePath, content] of Object.entries(additionalFiles)) {
    const resolved = path.join(directory, relativePath);
    await mkdir(path.dirname(resolved), {recursive: true});
    await writeFile(resolved, content);
  }
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

function runGenerator(directory, sourceCommit, extraArguments = [], environment = {}) {
  const args = [
    generator,
    '--source-commit',
    sourceCommit,
    ...extraArguments,
    'docs/release/enterprise-control-plane-acceptance.md',
  ];
  return new Promise((resolve) => {
    const child = spawn(process.execPath, args, {
      cwd: directory,
      env: {...process.env, ...environment},
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

test('runs only the exact selected Go tests and retains their observed pass set', async () => {
  const {directory, sourceCommit} = await fixtureWorkspace({
    communityProfile: {
      automatedTest: 'proof/proof_test.go',
      testRunner: 'go-test',
      selectedTests: ['TestSelected'],
    },
    proofSource: `package proof

import "testing"

func TestSelected(t *testing.T) {}

func TestUnrelated(t *testing.T) {
  t.Fatal("unrelated package test must not execute")
}
`,
  });

  const result = await runGenerator(directory, sourceCommit);

  assert.equal(result.status, 0, result.stderr);
  const resultFiles = (
    await (await import('node:fs/promises')).readdir(path.join(directory, 'docs', 'release', 'evidence', 'results'))
  ).filter((name) => name.endsWith('.json'));
  assert.equal(resultFiles.length, 1);
  const retained = JSON.parse(
    await readFile(path.join(directory, 'docs', 'release', 'evidence', 'results', resultFiles[0]), 'utf8')
  );
  assert.deepEqual(retained.command, {
    runner: 'go-test',
    argv: ['go', 'test', './proof', '-run', '^(?:TestSelected)$', '-count=1', '-json'],
    selectedTestSource: 'proof/proof_test.go',
    selectedTests: ['TestSelected'],
  });
  assert.deepEqual(retained.tests.topLevel, [{name: 'TestSelected', status: 'pass'}]);
  assert.deepEqual(retained.tests, {
    expected: 1,
    passed: 1,
    failed: 0,
    skipped: 0,
    topLevel: [{name: 'TestSelected', status: 'pass'}],
  });
  assert.deepEqual(retained.compiledPackageSources, [
    {
      path: 'proof/proof_test.go',
      sha256: sha256(await readFile(path.join(directory, 'proof', 'proof_test.go'))),
    },
  ]);
});

test('fails before execution when an untracked Go source could affect the selected package', async () => {
  const {directory, sourceCommit} = await fixtureWorkspace({
    communityProfile: {
      automatedTest: 'proof/proof_test.go',
      testRunner: 'go-test',
      selectedTests: ['TestSelected'],
    },
    proofSource: `package proof

import "testing"

func TestSelected(t *testing.T) {}
`,
  });
  await writeFile(
    path.join(directory, 'proof', 'injected_test.go'),
    `package proof

import "testing"

func TestInjected(t *testing.T) {}
`
  );

  const result = await runGenerator(directory, sourceCommit);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /untracked Go source is not allowed before evidence execution: proof\/injected_test\.go/);
});

test('fails before execution for untracked files outside explicit safe artifact roots', async () => {
  const {directory, sourceCommit} = await fixtureWorkspace();
  await writeFile(path.join(directory, 'unexpected-notes.txt'), 'must not be silently ignored\n');

  const result = await runGenerator(directory, sourceCommit);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /untracked path is not allowed before evidence execution: unexpected-notes\.txt/);
});

test('allows non-Go untracked output only under explicit safe artifact roots', async () => {
  const {directory, sourceCommit} = await fixtureWorkspace();
  await mkdir(path.join(directory, 'work'), {recursive: true});
  await mkdir(path.join(directory, 'output'), {recursive: true});
  await writeFile(path.join(directory, 'work', 'prior-run.json'), '{}\n');
  await writeFile(path.join(directory, 'output', 'browser.log'), 'retained local output\n');

  const result = await runGenerator(directory, sourceCommit);

  assert.equal(result.status, 0, result.stderr);
});

test('rejects untracked Go source even inside an otherwise safe artifact root', async () => {
  const {directory, sourceCommit} = await fixtureWorkspace();
  await mkdir(path.join(directory, 'work'), {recursive: true});
  await writeFile(path.join(directory, 'work', 'generated_test.go'), 'package injected\n');

  const result = await runGenerator(directory, sourceCommit);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /untracked Go source is not allowed before evidence execution: work\/generated_test\.go/);
});

test('executes selected tests in a detached source-commit checkout without safe untracked inputs', async () => {
  const {directory, sourceCommit} = await fixtureWorkspace({
    proofSource: `import assert from 'node:assert/strict';
import {existsSync, statSync} from 'node:fs';
import {test} from 'node:test';

test('isolated source commit', () => {
  assert.equal(statSync('.git').isFile(), true);
  assert.equal(existsSync('work/shared-only.txt'), false);
});
`,
  });
  await mkdir(path.join(directory, 'work'), {recursive: true});
  await writeFile(path.join(directory, 'work', 'shared-only.txt'), 'must not enter execution checkout\n');

  const result = await runGenerator(directory, sourceCommit);

  assert.equal(result.status, 0, result.stderr);
});

test('clears adversarial GOFLAGS overlays for both Go package inspection and execution', async () => {
  const {directory, sourceCommit} = await fixtureWorkspace({
    communityProfile: {
      automatedTest: 'proof/proof_test.go',
      testRunner: 'go-test',
      selectedTests: ['TestSelected'],
    },
    proofSource: `package proof

import "testing"

func TestSelected(t *testing.T) {}
`,
  });
  await mkdir(path.join(directory, 'work'), {recursive: true});
  const overlay = path.join(directory, 'work', 'hostile-overlay.json');
  await writeFile(overlay, 'not valid overlay JSON\n');

  const result = await runGenerator(directory, sourceCommit, [], {
    GOFLAGS: `-overlay=${overlay}`,
  });

  assert.equal(result.status, 0, result.stderr);
});

test('forces GOWORK off for both Go package inspection and execution', async () => {
  const {directory, sourceCommit} = await fixtureWorkspace({
    communityProfile: {
      automatedTest: 'proof/proof_test.go',
      testRunner: 'go-test',
      selectedTests: ['TestSelected'],
    },
    proofSource: `package proof

import "testing"

func TestSelected(t *testing.T) {}
`,
  });
  await mkdir(path.join(directory, 'work'), {recursive: true});
  const hostileWorkspace = path.join(directory, 'work', 'go.work');
  await writeFile(hostileWorkspace, 'go 1.26.5\n\nuse ./missing-module\n');

  const result = await runGenerator(directory, sourceCommit, [], {
    GOWORK: hostileWorkspace,
  });

  assert.equal(result.status, 0, result.stderr);
});

test('rejects and cleans an isolated checkout changed by the selected command', async () => {
  const {directory, sourceCommit} = await fixtureWorkspace({
    proofSource: `import {writeFileSync} from 'node:fs';
import {test} from 'node:test';

test('mutates checkout', () => {
  writeFileSync('generated-during-test.txt', 'unexpected mutation\\n');
});
`,
  });
  const isolatedBasesBefore = new Set(
    (await readdir(tmpdir())).filter((name) => name.startsWith('control-plane-acceptance-checkout-'))
  );

  const result = await runGenerator(directory, sourceCommit);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /isolated execution worktree is dirty after command: generated-during-test\.txt/);
  assert.equal(await stat(path.join(directory, 'generated-during-test.txt')).catch(() => undefined), undefined);
  const isolatedBasesAfter = (await readdir(tmpdir())).filter(
    (name) => name.startsWith('control-plane-acceptance-checkout-') && !isolatedBasesBefore.has(name)
  );
  assert.deepEqual(isolatedBasesAfter, []);
});

test('removes failed initial checkout registrations instead of only deleting their directories', async () => {
  const {directory, sourceCommit} = await fixtureWorkspace({
    additionalFiles: {
      '.gitattributes': 'proof.test.mjs filter=acceptance-smudge\n',
    },
  });
  const filterScript = path.join(directory, 'work', 'smudge.cjs');
  await mkdir(path.dirname(filterScript), {recursive: true});
  await writeFile(
    filterScript,
    "let value='';process.stdin.setEncoding('utf8');process.stdin.on('data',c=>value+=c);process.stdin.on('end',()=>process.stdout.write(value+'// smudged\\n'));\n"
  );
  await git(directory, 'config', 'filter.acceptance-smudge.smudge', `"${process.execPath}" "${filterScript}"`);

  const result = await runGenerator(directory, sourceCommit);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /isolated execution worktree is dirty before execution/);
  const {stdout} = await git(directory, 'worktree', 'list', '--porcelain');
  assert.doesNotMatch(stdout, /control-plane-acceptance-checkout-/);
});

test('rejects a symlinked evidence results parent without writing outside the repository', async (t) => {
  const {directory, sourceCommit} = await fixtureWorkspace();
  const external = await mkdtemp(path.join(tmpdir(), 'control-plane-evidence-escape-'));
  await mkdir(path.join(directory, 'docs', 'release', 'evidence'), {recursive: true});
  try {
    await symlink(
      external,
      path.join(directory, 'docs', 'release', 'evidence', 'results'),
      process.platform === 'win32' ? 'junction' : 'dir'
    );
  } catch (error) {
    if (error.code === 'EPERM') {
      t.skip('filesystem does not permit test symlinks or junctions');
      return;
    }
    throw error;
  }

  const result = await runGenerator(directory, sourceCommit);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /generated evidence parent must not be a symbolic link or reparse point/);
  assert.deepEqual(await readdir(external), []);
});

test('rejects a symlinked evidence output path without replacing its external target', async (t) => {
  const {directory, sourceCommit} = await fixtureWorkspace();
  const external = await mkdtemp(path.join(tmpdir(), 'control-plane-evidence-output-escape-'));
  const evidenceDirectoryPath = path.join(directory, 'docs', 'release', 'evidence');
  await mkdir(evidenceDirectoryPath, {recursive: true});
  try {
    await symlink(
      external,
      path.join(evidenceDirectoryPath, 'AC-03.json'),
      process.platform === 'win32' ? 'junction' : 'dir'
    );
  } catch (error) {
    if (error.code === 'EPERM') {
      t.skip('filesystem does not permit test symlinks or junctions');
      return;
    }
    throw error;
  }

  const result = await runGenerator(directory, sourceCommit);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /generated evidence path must not be a symbolic link or reparse point/);
  assert.deepEqual(await readdir(external), []);
});

test('never clobbers an existing immutable evidence artifact', async () => {
  const {directory, sourceCommit} = await fixtureWorkspace();
  const artifactPath = path.join(directory, 'docs', 'release', 'evidence', 'AC-03.json');
  const sentinel = Buffer.from('existing immutable artifact\n');
  await mkdir(path.dirname(artifactPath), {recursive: true});
  await writeFile(artifactPath, sentinel);

  const result = await runGenerator(directory, sourceCommit);

  assert.notEqual(result.status, 0);
  assert.match(
    result.stderr,
    /generated evidence already exists and is immutable: docs\/release\/evidence\/AC-03\.json/
  );
  assert.deepEqual(await readFile(artifactPath), sentinel);
});

test('never clobbers an existing immutable test result', async () => {
  const {directory, sourceCommit} = await fixtureWorkspace();
  const resultName = sha256(JSON.stringify(communityCommand())).slice('sha256:'.length);
  const resultPath = path.join(directory, 'docs', 'release', 'evidence', 'results', `${resultName}.json`);
  const sentinel = Buffer.from('existing immutable result\n');
  await mkdir(path.dirname(resultPath), {recursive: true});
  await writeFile(resultPath, sentinel);

  const result = await runGenerator(directory, sourceCommit);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /generated evidence already exists and is immutable/);
  assert.deepEqual(await readFile(resultPath), sentinel);
});

test('rejects selected Go test names that are only text and not AST declarations', async () => {
  const {directory, sourceCommit} = await fixtureWorkspace({
    communityProfile: {
      automatedTest: 'proof/proof_test.go',
      testRunner: 'go-test',
      selectedTests: ['TestSelected'],
    },
    proofSource: `package proof

const decoy = "func TestSelected(t *testing.T) {}"
`,
  });

  const result = await runGenerator(directory, sourceCommit);

  assert.notEqual(result.status, 0);
  assert.match(
    result.stderr,
    /proof\/proof_test\.go selectedTests are not declared top-level Go tests in the bound source: TestSelected/
  );
});

test('rejects duplicate selected declarations across the compiled Go package scope', async () => {
  const {directory, sourceCommit} = await fixtureWorkspace({
    communityProfile: {
      automatedTest: 'proof/proof_test.go',
      testRunner: 'go-test',
      selectedTests: ['TestSelected'],
    },
    proofSource: `package proof

import "testing"

func TestSelected(t *testing.T) {}
`,
    additionalFiles: {
      'proof/duplicate_external_test.go': `package proof_test

import "testing"

func TestSelected(t *testing.T) {}
`,
    },
  });

  const result = await runGenerator(directory, sourceCommit);

  assert.notEqual(result.status, 0);
  assert.match(
    result.stderr,
    /proof\/proof_test\.go selected test TestSelected must have exactly one declaration in compiled package \.\/proof; found 2/
  );
});

test('fails before execution when a selected Go test is not declared in the bound source', async () => {
  const {directory, sourceCommit} = await fixtureWorkspace({
    communityProfile: {
      automatedTest: 'proof/proof_test.go',
      testRunner: 'go-test',
      selectedTests: ['TestSelected', 'TestMissing'],
    },
    proofSource: `package proof

import "testing"

func TestSelected(t *testing.T) {}
`,
  });

  const result = await runGenerator(directory, sourceCommit);

  assert.notEqual(result.status, 0);
  assert.match(
    result.stderr,
    /proof\/proof_test\.go selectedTests are not declared top-level Go tests in the bound source: TestMissing/
  );
});

test('reports bounded selected Go failure names without retaining raw test output', async () => {
  const {directory, sourceCommit} = await fixtureWorkspace({
    communityProfile: {
      automatedTest: 'proof/proof_test.go',
      testRunner: 'go-test',
      selectedTests: ['TestSelected'],
    },
    proofSource: `package proof

import "testing"

func TestSelected(t *testing.T) {
  t.Fatal("DO_NOT_RETAIN_SECRET_TEST_OUTPUT")
}
`,
  });

  const result = await runGenerator(directory, sourceCommit);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /selected source proof\/proof_test\.go package \.\/proof failed tests: TestSelected/);
  assert.doesNotMatch(result.stderr, /DO_NOT_RETAIN_SECRET_TEST_OUTPUT/);
});

test('reports bounded selected Go skip names and preserves the zero-skip policy', async () => {
  const {directory, sourceCommit} = await fixtureWorkspace({
    communityProfile: {
      automatedTest: 'proof/proof_test.go',
      testRunner: 'go-test',
      selectedTests: ['TestSelected'],
    },
    proofSource: `package proof

import "testing"

func TestSelected(t *testing.T) {
  t.Run("environment", func(t *testing.T) {
    t.Skip("DO_NOT_RETAIN_SKIP_REASON")
  })
}
`,
  });

  const result = await runGenerator(directory, sourceCommit);

  assert.notEqual(result.status, 0);
  assert.match(
    result.stderr,
    /selected source proof\/proof_test\.go package \.\/proof skipped tests: TestSelected\/environment/
  );
  assert.doesNotMatch(result.stderr, /DO_NOT_RETAIN_SKIP_REASON/);
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
