import assert from 'node:assert/strict';
import {execFile, spawn} from 'node:child_process';
import {createHash} from 'node:crypto';
import {mkdir, mkdtemp, readFile, stat, writeFile} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import test from 'node:test';
import {fileURLToPath} from 'node:url';
import {promisify} from 'node:util';
import {neutralLiveExecutionSourcePaths} from './control-plane-neutral-live-evidence-contract.mjs';

const execFileAsync = promisify(execFile);
const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const runner = path.join(repoRoot, 'hack', 'control-plane-neutral-live-evidence.mjs');
const automatedTest = 'examples/control-plane-e2e/reference-executor/main_test.go';
const selectedTests = ['TestReferenceExecutorOne', 'TestReferenceExecutorTwo'];

function sha256(value) {
  return `sha256:${createHash('sha256').update(value).digest('hex')}`;
}

async function git(directory, ...args) {
  return execFileAsync('git', args, {cwd: directory});
}

const fakeLiveRunner = `
const commit = process.env.DISTR_CP_SOURCE_COMMIT;
const scenario = process.env.FAKE_LIVE_SCENARIO || 'passed';
const report = {
  proofMode: 'live-hub-api',
  status: 'passed',
  acceptanceEligible: true,
  liveStack: {
    started: true,
    hubImage: {
      reference: 'distr-hub:fixture',
      imageId: 'sha256:' + 'a'.repeat(64),
      sourceCommit: scenario === 'wrong-image-source' ? 'b'.repeat(40) : commit
    }
  },
  targets: [{id: 'target-alpha'}, {id: 'target-beta'}],
  nonLocalCalls: 0,
  cleanup: {completed: true}
};
process.stdout.write(JSON.stringify(report) + '\\n');
`;

async function fixtureWorkspace({prohibitedTerm = false, missingSource = false} = {}) {
  const directory = await mkdtemp(path.join(tmpdir(), 'control-plane-neutral-live-evidence-'));
  const files = {
    'go.mod': 'module example.invalid/neutral-live-fixture\n\ngo 1.26.5\n',
    'docs/release/control-plane-acceptance-contract.json': `${JSON.stringify(
      {
        schema: 'distr.control-plane-acceptance-contract/v1',
        profiles: {
          'pr081-neutral-live': {
            automatedTest,
            manualEvidence: 'docs/fork/PR-081_NEUTRAL_CONTROL_PLANE_PROOF.md',
            allowedProofClasses: ['neutral-live-execution'],
            testRunner: 'go-test',
            selectedTests,
          },
        },
        acceptance: {'AC-53': {owner: 'PR-081', profile: 'pr081-neutral-live', pendingAdopter: false}},
      },
      null,
      2
    )}\n`,
    'docs/fork/PR-081_NEUTRAL_CONTROL_PLANE_PROOF.md': '# Neutral live fixture\n',
    'examples/control-plane-e2e/run.mjs': fakeLiveRunner,
    'examples/control-plane-e2e/fixture.json': '{"schemaVersion":"fixture/v1"}\n',
    'examples/control-plane-e2e/compose.yaml': 'services: {}\n',
    'examples/control-plane-e2e/external-executor.mjs': prohibitedTerm
      ? `// ${['Jenk', 'ins'].join('')} implementation must not enter the neutral proof.\n`
      : 'export const executor = "external";\n',
    'examples/control-plane-e2e/observer.mjs': 'export const observer = "independent";\n',
    'examples/control-plane-e2e/reference-executor/main.go': 'package main\n\nfunc main() {}\n',
    [automatedTest]: `package main\n\nimport "testing"\n\nfunc TestReferenceExecutorOne(t *testing.T) {}\nfunc TestReferenceExecutorTwo(t *testing.T) {}\n`,
    'examples/control-plane-e2e/provenance-fixture/main.go': 'package main\n\nfunc main() {}\n',
  };
  if (missingSource) delete files['examples/control-plane-e2e/observer.mjs'];
  for (const [relative, contents] of Object.entries(files)) {
    const target = path.join(directory, relative);
    await mkdir(path.dirname(target), {recursive: true});
    await writeFile(target, contents);
  }
  await git(directory, 'init', '--quiet');
  await git(directory, 'config', 'user.email', 'fixture@example.invalid');
  await git(directory, 'config', 'user.name', 'Neutral Live Fixture');
  await git(directory, 'add', '.');
  await git(directory, 'commit', '--quiet', '-m', 'neutral live source');
  const {stdout} = await git(directory, 'rev-parse', 'HEAD');
  return {directory, sourceCommit: stdout.trim(), outDir: 'retained/ac53'};
}

function run(fixture, environment = {}) {
  return new Promise((resolve) => {
    const child = spawn(
      process.execPath,
      [runner, '--source-commit', fixture.sourceCommit, '--out-dir', fixture.outDir],
      {
        cwd: fixture.directory,
        env: {...process.env, ...environment},
        stdio: ['ignore', 'pipe', 'pipe'],
      }
    );
    let stdout = '';
    let stderr = '';
    child.stdout.setEncoding('utf8');
    child.stderr.setEncoding('utf8');
    child.stdout.on('data', (chunk) => (stdout += chunk));
    child.stderr.on('data', (chunk) => (stderr += chunk));
    child.on('close', (status) => resolve({status, stdout, stderr}));
  });
}

test('packages AC-53 from exact focused Go tests and a source-bound neutral live result', async () => {
  const fixture = await fixtureWorkspace();

  const result = await run(fixture);

  assert.equal(result.status, 0, result.stderr);
  const out = path.join(fixture.directory, fixture.outDir);
  const genericBytes = await readFile(path.join(out, 'results', 'AC-53-test-result.json'));
  const generic = JSON.parse(genericBytes);
  assert.deepEqual(generic.command, {
    runner: 'go-test',
    argv: [
      'go',
      'test',
      './examples/control-plane-e2e/reference-executor',
      '-run',
      '^(?:TestReferenceExecutorOne|TestReferenceExecutorTwo)$',
      '-count=1',
      '-json',
    ],
    selectedTestSource: automatedTest,
    selectedTests,
  });
  assert.deepEqual(
    generic.tests.topLevel,
    selectedTests.map((name) => ({name, status: 'pass'}))
  );
  assert.deepEqual(
    generic.compiledPackageSources.map(({path: sourcePath}) => sourcePath),
    [automatedTest]
  );

  const classBytes = await readFile(path.join(out, 'results', 'AC-53-neutral-live-result.json'));
  const classReport = JSON.parse(classBytes);
  assert.equal(classReport.sourceCommit, fixture.sourceCommit);
  assert.equal(classReport.liveStack.hubImage.sourceCommit, fixture.sourceCommit);
  assert.deepEqual(
    classReport.executionSources.map(({path: sourcePath}) => sourcePath),
    neutralLiveExecutionSourcePaths
  );
  assert.deepEqual(classReport.neutralityProof, {
    mode: 'source-bound-community-neutrality',
    scannedPaths: neutralLiveExecutionSourcePaths,
    findings: [],
  });

  const wrapper = JSON.parse(await readFile(path.join(out, 'AC-53.json'), 'utf8'));
  assert.equal(wrapper.acceptanceId, 'AC-53');
  assert.equal(wrapper.testResult.sha256, sha256(genericBytes));
  assert.equal(wrapper.classEvidence.sha256, sha256(classBytes));
  assert.equal(wrapper.automatedTest.path, automatedTest);
  const {stdout: status} = await git(fixture.directory, 'status', '--short');
  assert.match(status, /^\?\? retained\//m);
});

test('rejects a prohibited adopter term in any complete execution source', async () => {
  const fixture = await fixtureWorkspace({prohibitedTerm: true});

  const result = await run(fixture);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /neutral live execution sources contain prohibited adopter terms/);
  await assert.rejects(stat(path.join(fixture.directory, fixture.outDir)));
});

test('rejects a missing exact execution source', async () => {
  const fixture = await fixtureWorkspace({missingSource: true});

  const result = await run(fixture);

  assert.notEqual(result.status, 0);
  assert.match(
    result.stderr,
    /neutral live execution source must be tracked by git: examples\/control-plane-e2e\/observer\.mjs/
  );
});

test('rejects a live Hub image whose revision label does not match the selected source commit', async () => {
  const fixture = await fixtureWorkspace();

  const result = await run(fixture, {FAKE_LIVE_SCENARIO: 'wrong-image-source'});

  assert.notEqual(result.status, 0);
  assert.match(
    result.stderr,
    /neutral live runner must return a complete source-bound acceptance-eligible live-hub-api result/
  );
  await assert.rejects(stat(path.join(fixture.directory, fixture.outDir)));
});
