import assert from 'node:assert/strict';
import {execFile, spawn} from 'node:child_process';
import {createHash} from 'node:crypto';
import {mkdir, mkdtemp, readFile, stat, symlink, writeFile} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {test} from 'node:test';
import {fileURLToPath} from 'node:url';
import {promisify} from 'node:util';
import {
  browserCheckpointClaims as checkpointClaims,
  browserCheckpointManifestName as checkpointManifestName,
  browserCheckpointManifestSchema as checkpointManifestSchema,
  browserEvidenceTitle as exactTitle,
  browserScreenshotNames as screenshotNames,
} from './control-plane-browser-evidence-contract.mjs';

const execFileAsync = promisify(execFile);
const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const runner = path.join(repoRoot, 'hack', 'control-plane-browser-evidence.mjs');
const evidenceConfig = 'playwright.control-plane-evidence.config.ts';
const automatedTest = 'frontend/ui/e2e/control-plane.spec.ts';
const manualEvidence = 'docs/fork/PR-080_OPERATOR_CONTROL_ROOM_UI.md';
const project = 'chromium';
const playwrightCLI = 'node_modules/@playwright/test/cli.js';
const exactGrep = `${exactTitle.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}$`;
const exactCommand = [
  'node',
  playwrightCLI,
  'test',
  automatedTest,
  '--config',
  evidenceConfig,
  '--project',
  project,
  '--grep',
  exactGrep,
  '--reporter',
  'json',
];
const browserContract = 'hack/control-plane-browser-evidence-contract.mjs';
const browserContractSource = await readFile(path.join(repoRoot, browserContract), 'utf8');

test('places the test filter before Playwright variadic project options', () => {
  assert.ok(exactCommand.indexOf(automatedTest) < exactCommand.indexOf('--project'));
});

test('starts the isolated evidence web server without package-manager execution', async () => {
  const config = await readFile(path.join(repoRoot, evidenceConfig), 'utf8');
  assert.match(
    config,
    /command:\s*\n\s*'node hack\/update-frontend-version\.js && node hack\/agent-changelog\.mjs CHANGELOG\.md frontend\/ui\/src\/data\/agent-changelog\.json && node node_modules\/@angular\/cli\/bin\/ng\.js serve /
  );
  assert.doesNotMatch(config, /pnpm\s+exec\s+ng\s+serve/);
});

function sha256(value) {
  return `sha256:${createHash('sha256').update(value).digest('hex')}`;
}

async function git(directory, ...args) {
  return execFileAsync('git', args, {cwd: directory});
}

const lockedPlaywrightVersion = '1.52.0';
const lockedPlaywrightIntegrity = 'sha512-YWN0dWFsLWxvY2tmaWxlLWJvdW5kLXBsYXl3cmlnaHQtdGVzdA==';

const fakePlaywrightCLISource = String.raw`
import {execFileSync} from 'node:child_process';
import {createHash} from 'node:crypto';
import {appendFile, mkdir, symlink, writeFile} from 'node:fs/promises';
import path from 'node:path';

const actualArguments = process.argv.slice(2);
const scenario = process.env.FAKE_PLAYWRIGHT_SCENARIO || 'passed';
if (path.resolve(process.cwd()) === path.resolve(process.env.EXPECTED_ORIGINAL_ROOT)) {
  process.stderr.write('Playwright ran in the mutable source worktree\n');
  process.exit(93);
}
const isolatedHead = execFileSync('git', ['rev-parse', 'HEAD'], {encoding: 'utf8'}).trim();
if (isolatedHead !== process.env.EXPECTED_SOURCE_COMMIT) {
  process.stderr.write('isolated worktree selected the wrong source commit\n');
  process.exit(94);
}
await writeFile(process.env.ISOLATED_PATH_RECORD, process.cwd());
if (JSON.stringify(actualArguments) === JSON.stringify(['--version'])) {
  if (scenario === 'replace-cli-after-version') {
    await appendFile(new URL(import.meta.url), '\n// replaced after version query\n');
  }
  process.stdout.write('Version ' + process.env.EXPECTED_PLAYWRIGHT_VERSION + '\n');
  process.exit(0);
}
const expected = JSON.parse(process.env.EXPECTED_PLAYWRIGHT_ARGV);
if (JSON.stringify(actualArguments) !== JSON.stringify(expected)) {
  process.stderr.write('unexpected direct Playwright argv: ' + JSON.stringify(actualArguments) + '\n');
  process.exit(91);
}
if (process.env.DISTR_EVIDENCE_NODE_VERSION !== process.versions.node) {
  process.stderr.write('direct Playwright Node runtime was not bound to the evidence packager\n');
  process.exit(98);
}
const title = process.env.EXACT_EVIDENCE_TITLE;
const source = process.env.EXACT_EVIDENCE_SOURCE;
const project = process.env.EXACT_EVIDENCE_PROJECT;
const names = JSON.parse(process.env.EXACT_SCREENSHOT_NAMES);
const checkpointClaims = JSON.parse(process.env.EXACT_CHECKPOINT_CLAIMS);
const output = path.resolve('output/playwright/control-plane-evidence/fake-result');
if (scenario === 'symlink-transient') {
  await mkdir(path.dirname(output), {recursive: true});
  const escaped = path.join(process.env.EXTERNAL_TEST_DIRECTORY, 'escaped-output');
  await mkdir(escaped, {recursive: true});
  await symlink(escaped, output, process.platform === 'win32' ? 'junction' : 'dir');
} else {
  await mkdir(output, {recursive: true});
}

function crc32(bytes) {
  let value = 0xffffffff;
  for (const byte of bytes) {
    value ^= byte;
    for (let bit = 0; bit < 8; bit += 1) value = (value >>> 1) ^ (0xedb88320 & -(value & 1));
  }
  return (value ^ 0xffffffff) >>> 0;
}

function chunk(type, data) {
  const name = Buffer.from(type);
  const length = Buffer.alloc(4);
  length.writeUInt32BE(data.length);
  const checksum = Buffer.alloc(4);
  checksum.writeUInt32BE(crc32(Buffer.concat([name, data])));
  return Buffer.concat([length, name, data, checksum]);
}

function png(secret = false, zeroWidth = false, checkpoint = 0) {
  const signature = Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]);
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(zeroWidth ? 0 : 1440, 0);
  ihdr.writeUInt32BE(1200, 4);
  ihdr[8] = 8;
  ihdr[9] = 6;
  const chunks = [chunk('IHDR', ihdr)];
  chunks.push(chunk('tEXt', Buffer.from('checkpoint\0' + checkpoint)));
  if (secret) chunks.push(chunk('tEXt', Buffer.from('password\0browser-secret-value')));
  chunks.push(chunk('IDAT', Buffer.from([120, 156, 3, 0, 0, 0, 0, 1])));
  chunks.push(chunk('IEND', Buffer.alloc(0)));
  return Buffer.concat([signature, ...chunks]);
}

const attachments = [];
const screenshotDigests = new Map();
for (const [index, name] of names.entries()) {
  if (scenario === 'missing-png' && index === 0) continue;
  const target = path.join(output, name);
  const bytes =
    scenario === 'bad-signature' && index === 0
      ? Buffer.from('not-a-png')
      : png(
          scenario === 'secret-png' && index === 0,
          scenario === 'zero-dimension' && index === 0,
          scenario === 'duplicate-png' ? 0 : index + 1
        );
  await writeFile(target, bytes);
  screenshotDigests.set(name, 'sha256:' + createHash('sha256').update(bytes).digest('hex'));
  attachments.push({name, path: target, contentType: 'image/png'});
}
const manifest = {
  schema: process.env.EXACT_CHECKPOINT_MANIFEST_SCHEMA,
  testTitle: title,
  checkpoints: checkpointClaims.map((claim, index) => ({
    ...claim,
    route: scenario === 'bad-checkpoint-route' && index === 0 ? '/forged-route' : claim.route,
    entityIds:
      scenario === 'bad-checkpoint-entity' && index === 0 ? {releaseId: 'forged-release'} : claim.entityIds,
    checksums:
      scenario === 'bad-checkpoint-checksum' && index === 0
        ? {release: 'sha256:' + 'f'.repeat(64)}
        : claim.checksums,
    filename: names[index],
    sha256: screenshotDigests.get(names[index]) || 'sha256:' + '0'.repeat(64)
  }))
};
if (scenario === 'secret-manifest') manifest.apiToken = 'browser-secret-value';
const manifestName = process.env.EXACT_CHECKPOINT_MANIFEST_NAME;
const manifestPath = path.join(output, manifestName);
await writeFile(manifestPath, JSON.stringify(manifest));
attachments.push({name: manifestName, path: manifestPath, contentType: 'application/json'});
if (scenario === 'process-failure') {
  process.stderr.write('password=playwright-stderr-secret cwd=' + process.cwd() + '\n');
  process.exit(92);
}
if (scenario === 'process-failure-stdout') {
  process.stdout.write('password=playwright-stdout-secret cwd=' + process.cwd() + '\n');
  process.exit(95);
}
if (scenario === 'original-source-drift') {
  await appendFile(path.join(process.env.EXPECTED_ORIGINAL_ROOT, source), '\n// concurrent source drift\n');
}
if (scenario === 'isolated-source-drift') {
  await appendFile(path.resolve(source), '\n// isolated source drift\n');
}
if (scenario === 'original-head-drift') {
  execFileSync('git', ['checkout', '--detach', process.env.ALTERNATE_SOURCE_COMMIT], {
    cwd: process.env.EXPECTED_ORIGINAL_ROOT
  });
}
if (scenario === 'isolated-head-drift') {
  execFileSync('git', ['checkout', '--detach', process.env.ALTERNATE_SOURCE_COMMIT]);
}
if (scenario === 'retained-collision') {
  await mkdir(path.dirname(process.env.RETAINED_COLLISION_PATH), {recursive: true});
  await writeFile(process.env.RETAINED_COLLISION_PATH, 'do-not-overwrite');
}
if (scenario === 'replace-cli-after-execution') {
  await appendFile(new URL(import.meta.url), '\n// replaced after evidence execution\n');
}

const testStatus =
  scenario === 'skipped' ? 'skipped' : scenario === 'flaky' ? 'flaky' : scenario === 'unexpected' ? 'unexpected' : 'expected';
const resultStatus = scenario === 'unexpected' ? 'failed' : scenario === 'skipped' ? 'skipped' : 'passed';
const attempts = [
  {
    retry: 0,
    status: resultStatus,
    duration: 1250,
    errors: [],
    stdout: [],
    stderr: [],
    attachments,
    startTime: '2026-07-29T00:00:00.000Z'
  }
];
if (scenario === 'flaky') attempts.unshift({...attempts[0], retry: 0, status: 'failed'});
const raw = {
  config: {
    version: process.env.EXPECTED_PLAYWRIGHT_VERSION,
    rootDir: path.join(process.cwd(), 'frontend', 'ui', 'e2e'),
    projects: [{name: project}]
  },
  suites: [{
    title: 'operator control room route-mocked contract',
    file: path.basename(source),
    specs: [{
      title: scenario === 'wrong-title' ? '@evidence different journey' : title,
      ok: !['unexpected', 'skipped'].includes(scenario),
      file: scenario === 'wrong-source' ? 'other.spec.ts' : path.basename(source),
      tests: [{
        projectName: scenario === 'wrong-project' ? 'firefox' : project,
        expectedStatus: 'passed',
        status: testStatus,
        results: attempts
      }]
    }],
    suites: []
  }],
  errors: [],
  stats: {
    startTime: '2026-07-29T00:00:00.000Z',
    duration: 1250,
    expected: ['skipped', 'flaky', 'unexpected', 'zero'].includes(scenario) ? 0 : 1,
    skipped: scenario === 'skipped' ? 1 : 0,
    flaky: scenario === 'flaky' ? 1 : 0,
    unexpected: scenario === 'unexpected' ? 1 : 0
  }
};
if (scenario === 'secret-json') raw.config.metadata = {apiToken: 'browser-secret-value'};
process.stdout.write(JSON.stringify(raw));
`;

const fakePnpmSource = String.raw`
import path from 'node:path';

const actualArguments = process.argv.slice(2);
if (JSON.stringify(actualArguments) === JSON.stringify(['--version'])) {
  process.stdout.write(process.env.EXPECTED_PNPM_VERSION + '\n');
  process.exit(0);
}
if (JSON.stringify(actualArguments) === JSON.stringify(['store', 'status'])) {
  if (path.resolve(process.cwd()) !== path.resolve(process.env.EXPECTED_ORIGINAL_ROOT)) {
    process.stdout.write('password=pnpm-store-wrong-root cwd=' + process.cwd() + '\n');
    process.exit(99);
  }
  if (process.env.FAKE_PLAYWRIGHT_SCENARIO === 'store-failure') {
    process.stdout.write('password=pnpm-store-secret cwd=' + process.cwd() + '\n');
    process.exit(97);
  }
  process.exit(0);
}
process.stdout.write('password=unexpected-pnpm-command ' + JSON.stringify(actualArguments) + '\n');
process.exit(96);
`;

async function fixtureWorkspace({
  networkAssertion = 'inside',
  fixtureNetworkGuard = true,
  runtimeBinding = 'exact',
  dependencyVariant = 'valid',
  lockVariant = 'valid',
  retriesZero = true,
} = {}) {
  const directory = await mkdtemp(path.join(tmpdir(), 'control-plane-browser-direct-'));
  const externalDirectory = await mkdtemp(path.join(tmpdir(), 'control-plane-browser-external-'));
  const evidenceTest =
    networkAssertion === 'inside'
      ? `test('${exactTitle}', async ({controlPlane}) => {\n${
          runtimeBinding === 'exact'
            ? '  const expectedNodeVersion = process.env.DISTR_EVIDENCE_NODE_VERSION;\n  if (expectedNodeVersion !== undefined) {\n    expect(process.versions.node).toBe(expectedNodeVersion);\n  }\n'
            : runtimeBinding === 'wrong'
              ? '  const expectedNodeVersion = process.env.DISTR_EVIDENCE_NODE_VERSION;\n  if (expectedNodeVersion !== undefined) {\n    expect(process.versions.node).not.toBe(expectedNodeVersion);\n  }\n'
              : runtimeBinding === 'noop'
                ? '  void process.env.DISTR_EVIDENCE_NODE_VERSION;\n  void process.versions.node;\n'
                : ''
        }  expect(controlPlane.externalAttempts).toEqual([]);\n});\n`
      : `test('${exactTitle}', async () => {});\ntest('unbound assertion', async ({controlPlane}) => {\n  expect(controlPlane.externalAttempts).toEqual([]);\n});\n`;
  const fixture =
    fixtureNetworkGuard === true
      ? `const externalAttempts = [];\npage.route('**/*', async (route) => {\n  if (!isLocalHost(new URL(route.request().url()).hostname)) {\n    externalAttempts.push(route.request().url());\n    await route.abort('blockedbyclient');\n  }\n});\n`
      : 'export const externalAttemptRecorder = false;\n';
  const importerVersion = lockVariant === 'mismatch' ? '1.51.0' : lockedPlaywrightVersion;
  const files = {
    '.gitignore': 'collision-output/\nnode_modules/\n',
    'package.json': `${JSON.stringify(
      {
        name: 'browser-evidence-fixture',
        private: true,
        packageManager: 'pnpm@11.7.0',
        devDependencies: {'@playwright/test': '^1.52.0'},
      },
      null,
      2
    )}\n`,
    'pnpm-lock.yaml': `lockfileVersion: '9.0'\n\nimporters:\n\n  .:\n    devDependencies:\n      '@playwright/test':\n        specifier: ^1.52.0\n        version: ${importerVersion}\n\npackages:\n\n  '@playwright/test@${lockedPlaywrightVersion}':\n    resolution: {integrity: ${lockedPlaywrightIntegrity}}\n    engines: {node: '>=20'}\n    hasBin: true\n\nsnapshots:\n\n  '@playwright/test@${lockedPlaywrightVersion}': {}\n`,
    [evidenceConfig]: `export default {projects: [{name: '${project}'}], retries: ${retriesZero ? 0 : 1}};\n`,
    'playwright.control-plane.config.ts': 'export default {};\n',
    [automatedTest]: evidenceTest,
    'frontend/ui/e2e/fixtures/control-plane.ts': fixture,
    [browserContract]: browserContractSource,
    [manualEvidence]: '# Browser evidence fixture\n',
  };
  for (const [relative, contents] of Object.entries(files)) {
    await mkdir(path.join(directory, path.dirname(relative)), {recursive: true});
    await writeFile(path.join(directory, relative), contents);
  }
  const dependencySource = path.join(directory, 'node_modules');
  const expectedPackage = path.join(
    dependencySource,
    '.pnpm',
    `@playwright+test@${lockedPlaywrightVersion}`,
    'node_modules',
    '@playwright',
    'test'
  );
  await mkdir(expectedPackage, {recursive: true});
  await writeFile(
    path.join(expectedPackage, 'package.json'),
    `${JSON.stringify({
      name: '@playwright/test',
      version: dependencyVariant === 'wrong-version' ? '9.9.9' : lockedPlaywrightVersion,
    })}\n`
  );
  if (dependencyVariant === 'replaced-cli') {
    const replacement = path.join(externalDirectory, 'replacement-cli');
    await mkdir(replacement, {recursive: true});
    await writeFile(path.join(replacement, 'index.mjs'), fakePlaywrightCLISource);
    await symlink(replacement, path.join(expectedPackage, 'cli.js'), process.platform === 'win32' ? 'junction' : 'dir');
  } else {
    await writeFile(path.join(expectedPackage, 'cli.js'), fakePlaywrightCLISource);
  }
  let linkedPackage = expectedPackage;
  if (dependencyVariant === 'out-of-tree') {
    linkedPackage = path.join(externalDirectory, 'out-of-tree-playwright');
    await mkdir(linkedPackage, {recursive: true});
    await writeFile(
      path.join(linkedPackage, 'package.json'),
      `${JSON.stringify({name: '@playwright/test', version: lockedPlaywrightVersion})}\n`
    );
    await writeFile(path.join(linkedPackage, 'cli.js'), fakePlaywrightCLISource);
  }
  const publicPackage = path.join(dependencySource, '@playwright', 'test');
  await mkdir(path.dirname(publicPackage), {recursive: true});
  await symlink(linkedPackage, publicPackage, process.platform === 'win32' ? 'junction' : 'dir');
  const bin = path.join(directory, 'bin');
  await mkdir(bin);
  const fakePnpm = path.join(bin, 'fake-pnpm.mjs');
  await writeFile(fakePnpm, fakePnpmSource);
  await writeFile(path.join(bin, 'pnpm.cmd'), `@echo off\r\n"${process.execPath}" "${fakePnpm}" %*\r\n`);
  await git(directory, 'init', '--quiet');
  await git(directory, 'config', 'user.email', 'fixture@example.invalid');
  await git(directory, 'config', 'user.name', 'Browser Evidence Fixture');
  await git(directory, 'add', '.');
  await git(directory, 'commit', '--quiet', '-m', 'exact browser source');
  const {stdout} = await git(directory, 'rev-parse', 'HEAD');
  const sourceCommit = stdout.trim();
  await writeFile(path.join(directory, 'alternate-marker.txt'), 'alternate clean commit\n');
  await git(directory, 'add', 'alternate-marker.txt');
  await git(directory, 'commit', '--quiet', '-m', 'alternate source');
  const {stdout: alternate} = await git(directory, 'rev-parse', 'HEAD');
  const alternateSourceCommit = alternate.trim();
  await git(directory, 'checkout', '--quiet', '--detach', sourceCommit);
  const isolatedPathRecord = path.join(externalDirectory, 'isolated-path.txt');
  return {
    directory,
    externalDirectory,
    isolatedPathRecord,
    sourceCommit,
    outDir: 'retained/browser',
    environment: {
      ...process.env,
      PATH: `${bin}${path.delimiter}${process.env.PATH}`,
      EXPECTED_PLAYWRIGHT_ARGV: JSON.stringify(exactCommand.slice(2)),
      EXACT_EVIDENCE_TITLE: exactTitle,
      EXACT_EVIDENCE_SOURCE: automatedTest,
      EXACT_EVIDENCE_PROJECT: project,
      EXACT_SCREENSHOT_NAMES: JSON.stringify(screenshotNames),
      EXACT_CHECKPOINT_CLAIMS: JSON.stringify(checkpointClaims),
      EXACT_CHECKPOINT_MANIFEST_NAME: checkpointManifestName,
      EXACT_CHECKPOINT_MANIFEST_SCHEMA: checkpointManifestSchema,
      EXPECTED_ORIGINAL_ROOT: directory,
      EXPECTED_SOURCE_COMMIT: sourceCommit,
      ALTERNATE_SOURCE_COMMIT: alternateSourceCommit,
      EXPECTED_PNPM_VERSION: '11.7.0',
      EXPECTED_PLAYWRIGHT_VERSION: lockedPlaywrightVersion,
      EXPECTED_NODE_VERSION: process.versions.node,
      EXTERNAL_TEST_DIRECTORY: externalDirectory,
      ISOLATED_PATH_RECORD: isolatedPathRecord,
      RETAINED_COLLISION_PATH: path.join(directory, 'collision-output', 'results', 'AC-63-test-result.json'),
    },
  };
}

function run(fixture, {scenario = 'passed', arguments_: extra = [], environment = {}, outDir = fixture.outDir} = {}) {
  const args = [runner, '--source-commit', fixture.sourceCommit, '--out-dir', outDir, ...extra];
  return new Promise((resolve) => {
    const child = spawn(process.execPath, args, {
      cwd: fixture.directory,
      env: {...fixture.environment, FAKE_PLAYWRIGHT_SCENARIO: scenario, ...environment},
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    let stdout = '';
    let stderr = '';
    child.stdout.setEncoding('utf8');
    child.stderr.setEncoding('utf8');
    child.stdout.on('data', (chunk) => (stdout += chunk));
    child.stderr.on('data', (chunk) => (stderr += chunk));
    child.on('close', (status) => resolve({status, stdout, stderr}));
  });
}

async function assertIsolatedWorktreeRemoved(fixture) {
  const isolated = (await readFile(fixture.isolatedPathRecord, 'utf8')).trim();
  assert.notEqual(path.resolve(isolated), path.resolve(fixture.directory));
  await assert.rejects(stat(isolated));
}

test('directly runs the exact AC-63 evidence test and derives retained raw, PNG, class, generic, and wrapper evidence', async () => {
  const fixture = await fixtureWorkspace();

  const result = await run(fixture);

  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, /Executed and packaged AC-63 browser evidence: 1 passed test, 11 screenshots/);
  const out = path.join(fixture.directory, fixture.outDir);
  const rawBytes = await readFile(path.join(out, 'raw', 'AC-63-playwright.json'));
  const raw = JSON.parse(rawBytes);
  assert.equal(raw.suites[0].specs[0].title, exactTitle);
  const genericBytes = await readFile(path.join(out, 'results', 'AC-63-test-result.json'));
  const generic = JSON.parse(genericBytes);
  assert.deepEqual(generic.command, {
    runner: 'playwright',
    argv: exactCommand,
    selectedTestSource: automatedTest,
  });
  assert.deepEqual(generic.tests, {expected: 1, passed: 1, failed: 0, skipped: 0});
  const classBytes = await readFile(path.join(out, 'results', 'AC-63-browser-result.json'));
  const classReport = JSON.parse(classBytes);
  assert.equal(classReport.project, project);
  assert.deepEqual(classReport.testTitles, [exactTitle]);
  assert.equal(classReport.rawResult.sha256, sha256(rawBytes));
  assert.deepEqual(
    classReport.screenshots.map(({name, width, height}) => ({name, width, height})),
    screenshotNames.map((name) => ({name, width: 1440, height: 1200}))
  );
  assert.equal(new Set(classReport.screenshots.map(({sha256: digest}) => digest)).size, screenshotNames.length);
  const checkpointManifestBytes = await readFile(path.join(out, 'results', checkpointManifestName));
  const checkpointManifest = JSON.parse(checkpointManifestBytes);
  assert.equal(classReport.checkpointManifest.sha256, sha256(checkpointManifestBytes));
  assert.equal(classReport.checkpointManifest.path, `${fixture.outDir}/results/${checkpointManifestName}`);
  assert.equal(checkpointManifest.schema, checkpointManifestSchema);
  assert.equal(checkpointManifest.testTitle, exactTitle);
  assert.deepEqual(
    checkpointManifest.checkpoints.map(({filename, sha256: digest, ...claim}) => claim),
    checkpointClaims
  );
  assert.deepEqual(
    checkpointManifest.checkpoints.map(({filename, sha256: digest}) => ({filename, sha256: digest})),
    classReport.screenshots.map(({name, sha256: digest}) => ({filename: name, sha256: digest}))
  );
  assert.deepEqual(classReport.networkProof, {
    mode: 'bound-test-assertion',
    testTitle: exactTitle,
    externalAttempts: 0,
  });
  assert.deepEqual(classReport.toolVersions, {
    node: process.versions.node,
    pnpm: '11.7.0',
    playwright: '1.52.0',
  });
  assert.deepEqual(
    classReport.executionSources.map(({path: sourcePath}) => sourcePath),
    [
      evidenceConfig,
      'playwright.control-plane.config.ts',
      'frontend/ui/e2e/fixtures/control-plane.ts',
      browserContract,
      'package.json',
      'pnpm-lock.yaml',
    ]
  );
  for (const screenshot of classReport.screenshots) {
    const bytes = await readFile(path.join(fixture.directory, screenshot.path));
    assert.equal(screenshot.sha256, sha256(bytes));
  }
  const wrapper = JSON.parse(await readFile(path.join(out, 'AC-63.json'), 'utf8'));
  assert.equal(wrapper.proofClass, 'browser-e2e');
  assert.equal(wrapper.testResult.sha256, sha256(genericBytes));
  assert.equal(wrapper.classEvidence.sha256, sha256(classBytes));
  await assert.rejects(stat(path.join(fixture.directory, 'output', 'playwright', 'control-plane-evidence')));
  await assertIsolatedWorktreeRemoved(fixture);
  const {stdout: staged} = await git(fixture.directory, 'diff', '--cached', '--name-only');
  assert.equal(staged, '');
});

test('rejects reused screenshot bytes for different AC-63 checkpoint claims', async () => {
  const fixture = await fixtureWorkspace();

  const result = await run(fixture, {scenario: 'duplicate-png'});

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /browser evidence screenshot bytes are duplicated/);
  await assertIsolatedWorktreeRemoved(fixture);
});

for (const [dependencyVariant, expected] of [
  ['replaced-cli', /lockfile-bound Playwright CLI must be a regular file/],
  ['out-of-tree', /installed @playwright\/test must resolve to the lockfile package/],
  ['wrong-version', /installed @playwright\/test version must match the lockfile/],
]) {
  test(`rejects ${dependencyVariant} installed Playwright dependency state`, async () => {
    const fixture = await fixtureWorkspace({dependencyVariant});

    const result = await run(fixture);

    assert.notEqual(result.status, 0);
    assert.match(result.stderr, expected);
  });
}

test('rejects a Playwright importer version that does not match the integrity-bound lock package', async () => {
  const fixture = await fixtureWorkspace({lockVariant: 'mismatch'});

  const result = await run(fixture);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /pnpm lock importer version must match one integrity-bound @playwright\/test package/);
});

for (const scenario of ['replace-cli-after-version', 'replace-cli-after-execution']) {
  test(`rejects a Playwright CLI replaced during ${scenario}`, async () => {
    const fixture = await fixtureWorkspace();

    const result = await run(fixture, {scenario});

    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /lockfile-bound Playwright CLI changed during evidence execution/);
    await assertIsolatedWorktreeRemoved(fixture);
  });
}

for (const [scenario, message] of [
  ['zero', /direct Playwright evidence returned zero tests/],
  ['skipped', /direct Playwright evidence reported skipped tests/],
  ['flaky', /direct Playwright evidence reported flaky tests/],
  ['unexpected', /direct Playwright evidence reported unexpected tests/],
  ['wrong-title', /direct Playwright evidence title set mismatch/],
  ['wrong-project', /direct Playwright evidence project must be chromium/],
  ['wrong-source', /direct Playwright evidence source must be frontend\/ui\/e2e\/control-plane\.spec\.ts/],
  ['missing-png', /direct Playwright evidence attachment set mismatch/],
  ['bad-signature', /invalid PNG signature: 01-version-build\.png/],
  ['zero-dimension', /invalid PNG dimensions: 01-version-build\.png/],
  ['secret-json', /retained Playwright JSON contains a secret-like value/],
  ['secret-png', /PNG metadata contains a secret-like value: 01-version-build\.png/],
  ['secret-manifest', /browser evidence checkpoint manifest contains a secret-like value/],
  ['bad-checkpoint-route', /browser evidence checkpoint manifest mismatch at sequence 1/],
  ['bad-checkpoint-entity', /browser evidence checkpoint manifest mismatch at sequence 1/],
  ['bad-checkpoint-checksum', /browser evidence checkpoint manifest mismatch at sequence 1/],
]) {
  test(`rejects ${scenario} direct evidence`, async () => {
    const fixture = await fixtureWorkspace();

    const result = await run(fixture, {scenario});

    assert.notEqual(result.status, 0);
    assert.match(result.stderr, message);
    await assert.rejects(stat(path.join(fixture.directory, 'output', 'playwright', 'control-plane-evidence')));
    await assertIsolatedWorktreeRemoved(fixture);
  });
}

test('cleans transient output when direct Playwright execution fails', async () => {
  const fixture = await fixtureWorkspace();

  const result = await run(fixture, {scenario: 'process-failure'});

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /direct Playwright evidence failed with exit code 92; stdout bytes=0; stderr bytes=\d+/);
  assert.doesNotMatch(result.stderr, /playwright-stderr-secret/);
  assert.equal(result.stderr.includes(fixture.directory), false);
  await assert.rejects(stat(path.join(fixture.directory, 'output', 'playwright', 'control-plane-evidence')));
  await assertIsolatedWorktreeRemoved(fixture);
});

test('reports only stdout byte counts when direct Playwright stderr is empty', async () => {
  const fixture = await fixtureWorkspace();

  const result = await run(fixture, {scenario: 'process-failure-stdout'});

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /direct Playwright evidence failed with exit code 95; stdout bytes=\d+; stderr bytes=0/);
  assert.doesNotMatch(result.stderr, /playwright-stdout-secret/);
  assert.equal(result.stderr.includes(fixture.directory), false);
  await assertIsolatedWorktreeRemoved(fixture);
});

test('runs read-only pnpm store status from the original clean root and redacts its failure output', async () => {
  const fixture = await fixtureWorkspace();

  const result = await run(fixture, {scenario: 'store-failure'});

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /pnpm store status failed with exit code 97; stdout bytes=\d+; stderr bytes=0/);
  assert.doesNotMatch(result.stderr, /pnpm-store-secret/);
  assert.equal(result.stderr.includes(fixture.directory), false);
});

test('rejects imported Playwright JSON and screenshot manifest options', async () => {
  const fixture = await fixtureWorkspace();

  const result = await run(fixture, {
    arguments_: ['--playwright-json', 'forged.json', '--screenshot-manifest', 'forged-manifest.json'],
  });

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /unsupported option: --playwright-json/);
});

test('requires an exactly clean worktree including untracked files', async () => {
  const fixture = await fixtureWorkspace();
  await writeFile(path.join(fixture.directory, 'untracked-bypass_test.go'), 'package bypass\n');

  const result = await run(fixture);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /worktree must be exactly clean: untracked-bypass_test\.go/);
});

test('requires the zero-external-attempt assertion inside the exact evidence test', async () => {
  const fixture = await fixtureWorkspace({networkAssertion: 'outside'});

  const result = await run(fixture);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /exact browser evidence test must assert zero external attempts/);
});

test('requires the exact evidence test to compare the Playwright Node runtime', async () => {
  for (const runtimeBinding of ['missing', 'wrong', 'noop']) {
    const fixture = await fixtureWorkspace({runtimeBinding});

    const result = await run(fixture);

    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /exact browser evidence test must verify the Playwright Node runtime/);
  }
});

test('requires the bound fixture to record and block non-local network attempts', async () => {
  const fixture = await fixtureWorkspace({fixtureNetworkGuard: false});

  const result = await run(fixture);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /bound control-plane fixture must record and block non-local requests/);
});

test('requires the purpose-built browser evidence config to disable retries', async () => {
  const fixture = await fixtureWorkspace({retriesZero: false});

  const result = await run(fixture);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /purpose-built browser evidence config must disable retries/);
});

test('rejects source drift in the mutable worktree after isolated execution', async () => {
  const fixture = await fixtureWorkspace();

  const result = await run(fixture, {scenario: 'original-source-drift'});

  assert.notEqual(result.status, 0);
  assert.match(
    result.stderr,
    /source worktree drifted during isolated execution: frontend\/ui\/e2e\/control-plane\.spec\.ts/
  );
  await assertIsolatedWorktreeRemoved(fixture);
});

test('rejects source drift inside the detached execution worktree', async () => {
  const fixture = await fixtureWorkspace();

  const result = await run(fixture, {scenario: 'isolated-source-drift'});

  assert.notEqual(result.status, 0);
  assert.match(
    result.stderr,
    /isolated source worktree drifted during execution: frontend\/ui\/e2e\/control-plane\.spec\.ts/
  );
  await assertIsolatedWorktreeRemoved(fixture);
});

test('rejects a clean mutable worktree that moved to another commit during execution', async () => {
  const fixture = await fixtureWorkspace();

  const result = await run(fixture, {scenario: 'original-head-drift'});

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /source worktree HEAD drifted during isolated execution/);
  await assertIsolatedWorktreeRemoved(fixture);
});

test('rejects a clean detached execution worktree that moved to another commit', async () => {
  const fixture = await fixtureWorkspace();

  const result = await run(fixture, {scenario: 'isolated-head-drift'});

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /isolated source worktree HEAD drifted during execution/);
  await assertIsolatedWorktreeRemoved(fixture);
});

test('rejects a symlink or reparse-point parent in the retained evidence path', async () => {
  const fixture = await fixtureWorkspace();
  const escaped = path.join(fixture.externalDirectory, 'retained-escape');
  await mkdir(escaped);
  await symlink(
    escaped,
    path.join(fixture.directory, 'retained-link'),
    process.platform === 'win32' ? 'junction' : 'dir'
  );

  const result = await run(fixture, {outDir: 'retained-link/browser'});

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /--out-dir must not traverse symbolic links or reparse points/);
});

test('rejects a symlink or reparse-point parent in transient Playwright attachments', async () => {
  const fixture = await fixtureWorkspace();

  const result = await run(fixture, {scenario: 'symlink-transient'});

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /transient Playwright output must not traverse symbolic links or reparse points/);
  await stat(path.join(fixture.externalDirectory, 'escaped-output', screenshotNames[0]));
  await assertIsolatedWorktreeRemoved(fixture);
});

test('uses exclusive retained writes and never overwrites a raced destination', async () => {
  const fixture = await fixtureWorkspace();

  const result = await run(fixture, {scenario: 'retained-collision', outDir: 'collision-output'});

  assert.notEqual(result.status, 0);
  assert.match(
    result.stderr,
    /refusing to overwrite retained evidence: collision-output\/results\/AC-63-test-result\.json/
  );
  assert.equal(await readFile(fixture.environment.RETAINED_COLLISION_PATH, 'utf8'), 'do-not-overwrite');
  await assertIsolatedWorktreeRemoved(fixture);
});

test('requires the output directory to remain inside the repository', async () => {
  const fixture = await fixtureWorkspace();

  const result = await run(fixture, {outDir: path.join('..', 'outside')});

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /--out-dir must be a repository-relative path/);
});
