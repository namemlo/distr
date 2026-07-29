#!/usr/bin/env node

import {execFile, spawn} from 'node:child_process';
import {createHash, randomUUID} from 'node:crypto';
import {link, lstat, mkdir, mkdtemp, open, readFile, readdir, realpath, rm, symlink, unlink} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {promisify} from 'node:util';
import {inflateSync} from 'node:zlib';

const execFileAsync = promisify(execFile);
const acceptanceId = 'AC-63';
const owner = 'PR-080';
const evidenceConfig = 'playwright.control-plane-evidence.config.ts';
const baseConfig = 'playwright.control-plane.config.ts';
const automatedTest = 'frontend/ui/e2e/control-plane.spec.ts';
const fixtureSource = 'frontend/ui/e2e/fixtures/control-plane.ts';
const manualEvidence = 'docs/fork/PR-080_OPERATOR_CONTROL_ROOM_UI.md';
const project = 'chromium';
const playwrightCLI = 'node_modules/@playwright/test/cli.js';
const exactTitle = '@evidence proves the reference client DEV release, approval, and previous-state journey';
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
const screenshotNames = [
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
const testResultSchema = 'distr.control-plane-test-result/v1';
const browserResultSchema = 'distr.control-plane-browser-e2e-result/v1';
const evidenceSchema = 'distr.control-plane-acceptance-evidence/v1';
const commitPattern = /^[0-9a-f]{40}$/;
const pngSignature = Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]);
const maximumOutput = 64 * 1024 * 1024;
const runnerOutput = 'output/playwright/control-plane-evidence';

function fail(message) {
  throw new Error(message);
}

function sha256(value) {
  return `sha256:${createHash('sha256').update(value).digest('hex')}`;
}

function gitPath(value) {
  return value.split(path.sep).join('/');
}

function stableJSON(value) {
  return Buffer.from(`${JSON.stringify(value, null, 2)}\n`);
}

function parseArguments(argv) {
  const values = {};
  const supported = new Map([
    ['--source-commit', 'sourceCommit'],
    ['--out-dir', 'outDir'],
  ]);
  for (let index = 0; index < argv.length; index += 1) {
    const option = argv[index];
    const key = supported.get(option);
    if (!key) fail(`unsupported option: ${option}`);
    const value = argv[++index];
    if (!value || value.startsWith('--') || Object.hasOwn(values, key)) {
      fail(`option ${option} must be supplied exactly once with a value`);
    }
    values[key] = value;
  }
  for (const [option, key] of supported) {
    if (!Object.hasOwn(values, key)) fail(`missing required option: ${option}`);
  }
  if (!commitPattern.test(values.sourceCommit)) {
    fail('--source-commit must be a full lowercase 40-character git commit');
  }
  return values;
}

async function git(root, args, options = {}) {
  return execFileAsync('git', args, {cwd: root, maxBuffer: maximumOutput, ...options});
}

async function repositoryRoot() {
  try {
    const {stdout} = await git(process.cwd(), ['rev-parse', '--show-toplevel']);
    return stdout.trim();
  } catch {
    fail('browser evidence execution requires a git worktree');
  }
}

function repositoryPath(root, value, label) {
  if (typeof value !== 'string' || value === '' || path.isAbsolute(value)) {
    fail(`${label} must be a repository-relative path`);
  }
  const resolved = path.resolve(root, value);
  const relative = path.relative(root, resolved);
  if (relative === '' || relative === '..' || relative.startsWith(`..${path.sep}`) || path.isAbsolute(relative)) {
    fail(`${label} must be a repository-relative path`);
  }
  return resolved;
}

function containedPath(root, target) {
  const relative = path.relative(root, target);
  return relative === '' || (!path.isAbsolute(relative) && relative !== '..' && !relative.startsWith(`..${path.sep}`));
}

async function assertSafePath(root, target, label, {regularFile = false} = {}) {
  const rootReal = await realpath(root);
  const relative = path.relative(root, target);
  if (!containedPath(root, target)) fail(`${label} must remain inside its trusted root`);
  let current = root;
  for (const segment of relative.split(path.sep).filter(Boolean)) {
    current = path.join(current, segment);
    let information;
    try {
      information = await lstat(current);
    } catch (error) {
      if (error?.code === 'ENOENT') break;
      throw error;
    }
    if (information.isSymbolicLink()) {
      fail(`${label} must not traverse symbolic links or reparse points`);
    }
    const currentReal = await realpath(current);
    if (!containedPath(rootReal, currentReal)) fail(`${label} resolved outside its trusted root`);
  }
  if (regularFile) {
    let information;
    try {
      information = await lstat(target);
    } catch {
      fail(`missing ${label}`);
    }
    if (!information.isFile()) fail(`${label} must be a regular file`);
  }
}

async function ensureSafeDirectory(root, directory, label) {
  await assertSafePath(root, directory, label);
  const relative = path.relative(root, directory);
  let current = root;
  for (const segment of relative.split(path.sep).filter(Boolean)) {
    current = path.join(current, segment);
    try {
      await mkdir(current);
    } catch (error) {
      if (error?.code !== 'EEXIST') throw error;
    }
    const information = await lstat(current);
    if (information.isSymbolicLink() || !information.isDirectory()) {
      fail(`${label} must not traverse symbolic links or reparse points`);
    }
    const rootReal = await realpath(root);
    const currentReal = await realpath(current);
    if (!containedPath(rootReal, currentReal)) fail(`${label} resolved outside its trusted root`);
  }
}

async function atomicExclusiveWrite(root, destination, bytes) {
  const directory = path.dirname(destination);
  await ensureSafeDirectory(root, directory, 'retained evidence output');
  await assertSafePath(root, destination, 'retained evidence output');
  const temporary = path.join(directory, `.${path.basename(destination)}.${randomUUID()}.tmp`);
  let handle;
  try {
    handle = await open(temporary, 'wx', 0o600);
    await handle.writeFile(bytes);
    await handle.sync();
    await handle.close();
    handle = undefined;
    await link(temporary, destination);
  } catch (error) {
    if (error?.code === 'EEXIST')
      fail(`refusing to overwrite retained evidence: ${gitPath(path.relative(root, destination))}`);
    throw error;
  } finally {
    if (handle) await handle.close().catch(() => {});
    await unlink(temporary).catch((error) => {
      if (error?.code !== 'ENOENT') throw error;
    });
  }
  await assertSafePath(root, destination, 'retained evidence output', {regularFile: true});
}

function statusPath(entry) {
  return gitPath((entry.length > 3 ? entry.slice(3).trim() : entry.trim()).split(' -> ').at(-1));
}

async function requireCleanWorktree(root, message, allowedPrefixes = []) {
  const {stdout} = await git(root, ['status', '--porcelain', '--untracked-files=all']);
  const dirty = stdout
    .split(/\r?\n/)
    .filter(Boolean)
    .find((entry) => {
      const value = statusPath(entry);
      return !allowedPrefixes.some((prefix) => value === prefix || value.startsWith(`${prefix}/`));
    });
  if (dirty) fail(`${message}: ${statusPath(dirty)}`);
}

async function requireExactHead(root, sourceCommit) {
  const {stdout} = await git(root, ['rev-parse', 'HEAD']);
  if (stdout.trim() !== sourceCommit) {
    fail(`source commit must equal HEAD: selected ${sourceCommit}, HEAD ${stdout.trim()}`);
  }
  await requireCleanWorktree(root, 'worktree must be exactly clean');
}

async function requireUnchangedHead(root, sourceCommit, message) {
  const {stdout} = await git(root, ['rev-parse', 'HEAD']);
  const current = stdout.trim();
  if (current !== sourceCommit) fail(`${message}: expected ${sourceCommit}, got ${current}`);
}

async function requireEmptyOutput(outDir) {
  try {
    const entries = await readdir(outDir);
    if (entries.length !== 0) fail(`output directory must be empty: ${outDir}`);
  } catch (error) {
    if (error?.code !== 'ENOENT') throw error;
  }
}

async function trackedSet(root) {
  const {stdout} = await git(root, ['ls-files', '-z'], {encoding: 'buffer'});
  return new Set(stdout.toString('utf8').split('\0').filter(Boolean).map(gitPath));
}

async function sourceBinding(root, tracked, sourceCommit, value, label) {
  if (!tracked.has(value)) fail(`${label} must be tracked by git: ${value}`);
  repositoryPath(root, value, label);
  let committed;
  try {
    ({stdout: committed} = await git(root, ['show', `${sourceCommit}:${value}`], {encoding: 'buffer'}));
  } catch {
    fail(`${label} is absent from source commit: ${value}`);
  }
  try {
    await git(root, ['diff', '--quiet', '--no-ext-diff', sourceCommit, '--', value]);
  } catch {
    fail(`${label} drifted from source commit: ${value}`);
  }
  return {path: value, sha256: sha256(committed)};
}

function uniqueIndentedBlock(text, indent, headers, label) {
  const lines = text.replaceAll('\r\n', '\n').split('\n');
  const prefix = ' '.repeat(indent);
  const accepted = new Set(headers.map((header) => `${prefix}${header}:`));
  const matches = [];
  for (let index = 0; index < lines.length; index += 1) {
    if (accepted.has(lines[index])) matches.push(index);
  }
  if (matches.length !== 1) fail(`pnpm lock must contain exactly one ${label}`);
  const start = matches[0] + 1;
  let end = lines.length;
  for (let index = start; index < lines.length; index += 1) {
    if (lines[index].startsWith(prefix) && lines[index].slice(indent).match(/^\S.*:\s*$/)) {
      end = index;
      break;
    }
  }
  return lines.slice(start, end).join('\n');
}

function uniqueLockScalar(block, indent, key, label) {
  const prefix = `${' '.repeat(indent)}${key}:`;
  const values = block
    .split('\n')
    .filter((line) => line.startsWith(prefix))
    .map((line) => line.slice(prefix.length).trim());
  if (values.length !== 1 || values[0] === '') fail(`pnpm lock must contain exactly one ${label}`);
  return values[0];
}

function parsePlaywrightLock(manifestBytes, lockBytes) {
  let manifest;
  try {
    manifest = JSON.parse(manifestBytes.toString('utf8'));
  } catch {
    fail('source package manifest must be valid JSON');
  }
  const manifestSpecifier = manifest?.devDependencies?.['@playwright/test'];
  if (typeof manifestSpecifier !== 'string' || manifestSpecifier === '') {
    fail('source package manifest must declare @playwright/test');
  }
  const lock = lockBytes.toString('utf8');
  const importers = uniqueIndentedBlock(lock, 0, ['importers'], 'importers mapping');
  const rootImporter = uniqueIndentedBlock(importers, 2, ['.'], 'root importer');
  const devDependencies = uniqueIndentedBlock(rootImporter, 4, ['devDependencies'], 'root devDependencies');
  const importer = uniqueIndentedBlock(
    devDependencies,
    6,
    ["'@playwright/test'", '"@playwright/test"', '@playwright/test'],
    '@playwright/test importer'
  );
  const specifier = uniqueLockScalar(importer, 8, 'specifier', '@playwright/test importer specifier');
  const version = uniqueLockScalar(importer, 8, 'version', '@playwright/test importer version');
  if (specifier !== manifestSpecifier) {
    fail('package.json and pnpm lock importer disagree for @playwright/test');
  }
  if (!/^\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$/.test(version)) {
    fail('pnpm lock @playwright/test importer version must be canonical semver');
  }
  const packages = uniqueIndentedBlock(lock, 0, ['packages'], 'packages mapping');
  const packageHeaders = packages
    .split('\n')
    .filter((line) => /^  (?:'|")?@playwright\/test@/.test(line) && line.trimEnd().endsWith(':'));
  if (packageHeaders.length !== 1) {
    fail('pnpm lock importer version must match one integrity-bound @playwright/test package');
  }
  const expectedHeaders = [
    `'@playwright/test@${version}'`,
    `"@playwright/test@${version}"`,
    `@playwright/test@${version}`,
  ];
  let packageBlock;
  try {
    packageBlock = uniqueIndentedBlock(packages, 2, expectedHeaders, '@playwright/test package');
  } catch {
    fail('pnpm lock importer version must match one integrity-bound @playwright/test package');
  }
  const resolution = uniqueLockScalar(packageBlock, 4, 'resolution', '@playwright/test package resolution');
  const integrityMatch = /^\{\s*integrity:\s*(sha512-[A-Za-z0-9+/]+={0,2})\s*\}$/.exec(resolution);
  if (!integrityMatch) {
    fail('pnpm lock importer version must match one integrity-bound @playwright/test package');
  }
  const packageManagerMatch = /^pnpm@(\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?)$/.exec(manifest?.packageManager ?? '');
  if (!packageManagerMatch) fail('source packageManager must pin a canonical pnpm version');
  return {
    version,
    specifier,
    integrity: integrityMatch[1],
    pnpmVersion: packageManagerMatch[1],
  };
}

async function sourcePlaywrightDependency(root, sourceCommit) {
  let manifestBytes;
  let lockBytes;
  try {
    ({stdout: manifestBytes} = await git(root, ['show', `${sourceCommit}:package.json`], {encoding: 'buffer'}));
    ({stdout: lockBytes} = await git(root, ['show', `${sourceCommit}:pnpm-lock.yaml`], {encoding: 'buffer'}));
  } catch {
    fail('source commit must contain package.json and pnpm-lock.yaml');
  }
  return parsePlaywrightLock(manifestBytes, lockBytes);
}

function samePath(left, right) {
  return path.relative(left, right) === '';
}

async function trustedDependencySource(root) {
  const source = path.join(root, 'node_modules');
  let information;
  try {
    information = await lstat(source);
  } catch {
    fail('repository node_modules must exist before isolated evidence execution');
  }
  if (information.isSymbolicLink() || !information.isDirectory()) {
    fail('repository node_modules must be a real directory before isolated evidence execution');
  }
  const sourceReal = await realpath(source);
  if (!containedPath(await realpath(root), sourceReal)) {
    fail('repository node_modules resolved outside the trusted source worktree');
  }
  return sourceReal;
}

async function validatePlaywrightDependency(executionRoot, trustedSourceReal, dependency, baseline) {
  let executionModulesReal;
  try {
    executionModulesReal = await realpath(path.join(executionRoot, 'node_modules'));
  } catch {
    fail('evidence execution requires the trusted repository node_modules');
  }
  if (!samePath(trustedSourceReal, executionModulesReal)) {
    fail('evidence node_modules must resolve to the trusted dependency source');
  }
  const expectedPackage = path.join(
    trustedSourceReal,
    '.pnpm',
    `@playwright+test@${dependency.version}`,
    'node_modules',
    '@playwright',
    'test'
  );
  let expectedInformation;
  try {
    expectedInformation = await lstat(expectedPackage);
  } catch {
    fail('lockfile-bound @playwright/test package is missing from the trusted dependency source');
  }
  if (expectedInformation.isSymbolicLink() || !expectedInformation.isDirectory()) {
    fail('lockfile-bound @playwright/test package must be a real directory');
  }
  const expectedPackageReal = await realpath(expectedPackage);
  if (!containedPath(trustedSourceReal, expectedPackageReal) || !samePath(expectedPackage, expectedPackageReal)) {
    fail('lockfile-bound @playwright/test package resolved outside the trusted dependency source');
  }
  const publicPackage = path.join(executionRoot, 'node_modules', '@playwright', 'test');
  let publicInformation;
  try {
    publicInformation = await lstat(publicPackage);
  } catch {
    fail('installed @playwright/test package link is missing');
  }
  if (!publicInformation.isSymbolicLink()) fail('installed @playwright/test must be a pnpm dependency link');
  const publicReal = await realpath(publicPackage);
  if (!samePath(publicReal, expectedPackageReal)) {
    fail('installed @playwright/test must resolve to the lockfile package');
  }
  const packageManifestPath = path.join(publicPackage, 'package.json');
  const cliPath = path.join(publicPackage, 'cli.js');
  let packageInformation;
  let cliInformation;
  try {
    packageInformation = await lstat(packageManifestPath);
    cliInformation = await lstat(cliPath);
  } catch {
    fail('lockfile-bound Playwright package files are missing');
  }
  if (packageInformation.isSymbolicLink() || !packageInformation.isFile()) {
    fail('lockfile-bound Playwright package manifest must be a regular file');
  }
  if (cliInformation.isSymbolicLink() || !cliInformation.isFile()) {
    fail('lockfile-bound Playwright CLI must be a regular file');
  }
  const expectedManifestReal = path.join(expectedPackageReal, 'package.json');
  const expectedCLIReal = path.join(expectedPackageReal, 'cli.js');
  const [manifestReal, cliReal] = await Promise.all([realpath(packageManifestPath), realpath(cliPath)]);
  if (!samePath(manifestReal, expectedManifestReal) || !samePath(cliReal, expectedCLIReal)) {
    fail('lockfile-bound Playwright package files resolved outside the expected package');
  }
  const [manifestBytes, cliBytes] = await Promise.all([readFile(packageManifestPath), readFile(cliPath)]);
  let installedManifest;
  try {
    installedManifest = JSON.parse(manifestBytes.toString('utf8'));
  } catch {
    fail('installed @playwright/test package manifest must be valid JSON');
  }
  if (installedManifest?.name !== '@playwright/test' || installedManifest?.version !== dependency.version) {
    fail('installed @playwright/test version must match the lockfile');
  }
  const fingerprint = {
    packageReal: expectedPackageReal,
    manifestSha256: sha256(manifestBytes),
    cliReal,
    cliSha256: sha256(cliBytes),
  };
  if (
    baseline &&
    (!samePath(fingerprint.packageReal, baseline.packageReal) ||
      fingerprint.manifestSha256 !== baseline.manifestSha256 ||
      !samePath(fingerprint.cliReal, baseline.cliReal) ||
      fingerprint.cliSha256 !== baseline.cliSha256)
  ) {
    fail('lockfile-bound Playwright CLI changed during evidence execution');
  }
  return fingerprint;
}

async function linkExecutionDependencies(root, isolatedRoot) {
  const source = path.join(root, 'node_modules');
  let information;
  try {
    information = await lstat(source);
  } catch (error) {
    if (error?.code === 'ENOENT') return undefined;
    throw error;
  }
  if (information.isSymbolicLink() || !information.isDirectory()) {
    fail('repository node_modules must be a real directory before isolated evidence execution');
  }
  await assertSafePath(root, source, 'repository node_modules');
  const sourceReal = await realpath(source);
  const destination = path.join(isolatedRoot, 'node_modules');
  await symlink(sourceReal, destination, process.platform === 'win32' ? 'junction' : 'dir');
  const destinationReal = await realpath(destination);
  if (destinationReal !== sourceReal) fail('isolated dependency link resolved to an unexpected target');
  return {destination, sourceReal};
}

async function createIsolatedWorktree(root, sourceCommit) {
  const sandbox = await mkdtemp(path.join(tmpdir(), 'distr-ac63-evidence-'));
  const isolatedRoot = path.join(sandbox, 'source');
  let added = false;
  try {
    await git(root, ['worktree', 'add', '--detach', isolatedRoot, sourceCommit]);
    added = true;
    const {stdout} = await git(isolatedRoot, ['rev-parse', 'HEAD']);
    if (stdout.trim() !== sourceCommit) fail('isolated evidence worktree selected an unexpected source commit');
    const dependencyLink = await linkExecutionDependencies(root, isolatedRoot);
    await requireCleanWorktree(isolatedRoot, 'isolated source worktree must start clean', ['node_modules']);
    return {sandbox, isolatedRoot, dependencyLink};
  } catch (error) {
    if (added) await git(root, ['worktree', 'remove', '--force', isolatedRoot]).catch(() => {});
    await rm(sandbox, {recursive: true, force: true}).catch(() => {});
    throw error;
  }
}

async function removeTransientOutput(isolatedRoot) {
  const transient = path.join(isolatedRoot, ...runnerOutput.split('/'));
  try {
    const information = await lstat(transient);
    if (information.isSymbolicLink()) {
      await unlink(transient);
    } else {
      await rm(transient, {recursive: true, force: true});
    }
  } catch (error) {
    if (error?.code !== 'ENOENT') throw error;
  }
}

async function cleanupIsolatedWorktree(root, isolated) {
  await removeTransientOutput(isolated.isolatedRoot).catch(() => {});
  if (isolated.dependencyLink) await unlink(isolated.dependencyLink.destination).catch(() => {});
  await git(root, ['worktree', 'remove', '--force', isolated.isolatedRoot]).catch(() => {});
  await rm(isolated.sandbox, {recursive: true, force: true});
  await git(root, ['worktree', 'prune']).catch(() => {});
}

async function requireDependencyLinkUnchanged(dependencyLink) {
  if (!dependencyLink) return;
  const information = await lstat(dependencyLink.destination);
  if (!information.isSymbolicLink()) fail('isolated dependency link changed during evidence execution');
  const destinationReal = await realpath(dependencyLink.destination);
  if (path.relative(dependencyLink.sourceReal, destinationReal) !== '') {
    fail('isolated dependency link changed during evidence execution');
  }
}

async function runCapturedProcess(
  root,
  executable,
  arguments_,
  label,
  environment = process.env,
  windowsVerbatimArguments = false
) {
  return new Promise((resolve, reject) => {
    const child = spawn(executable, arguments_, {
      cwd: root,
      env: environment,
      shell: false,
      windowsVerbatimArguments,
      windowsHide: true,
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    const stdout = [];
    const stderr = [];
    let stdoutLength = 0;
    let stderrLength = 0;
    let overflow = false;
    child.stdout.on('data', (chunk) => {
      stdoutLength += chunk.length;
      if (stdoutLength > maximumOutput) {
        overflow = true;
        child.kill();
      } else {
        stdout.push(chunk);
      }
    });
    child.stderr.on('data', (chunk) => {
      stderrLength += chunk.length;
      if (stderrLength > maximumOutput) {
        overflow = true;
        child.kill();
      } else {
        stderr.push(chunk);
      }
    });
    child.on('error', (error) =>
      reject(new Error(`could not execute ${label}; process error code ${error?.code ?? 'unknown'}`))
    );
    child.on('close', (exitCode) => {
      if (overflow) {
        reject(new Error(`${label} exceeded the output limit`));
        return;
      }
      const stdoutBytes = Buffer.concat(stdout);
      const stderrBytes = Buffer.concat(stderr);
      if (exitCode !== 0) {
        reject(
          new Error(
            `${label} failed with exit code ${exitCode}; stdout bytes=${stdoutBytes.length}; stderr bytes=${stderrBytes.length}`
          )
        );
        return;
      }
      resolve({stdout: stdoutBytes, stderr: stderrBytes});
    });
  });
}

async function runPnpm(root, commandArguments, label, environment = process.env) {
  const logicalCommand = ['pnpm', ...commandArguments];
  const windows = process.platform === 'win32';
  const executable = windows ? process.env.ComSpec || 'cmd.exe' : 'pnpm';
  const arguments_ = windows
    ? [
        '/d',
        '/s',
        '/c',
        `"pnpm.cmd ${logicalCommand
          .slice(1)
          .map((argument) => `"${argument.replaceAll('"', '""')}"`)
          .join(' ')}"`,
      ]
    : logicalCommand.slice(1);
  return runCapturedProcess(root, executable, arguments_, label, environment, windows);
}

async function runNode(root, arguments_, label, environment = process.env) {
  return runCapturedProcess(root, process.execPath, arguments_, label, environment);
}

async function runPlaywright(root, nodeVersion) {
  return runNode(root, exactCommand.slice(1), 'direct Playwright evidence', {
    ...process.env,
    DISTR_EVIDENCE_NODE_VERSION: nodeVersion,
  });
}

async function validateRootPnpm(root, dependency) {
  const pnpmResult = await runPnpm(root, ['--version'], 'pnpm version query');
  const pnpm = pnpmResult.stdout.toString('utf8').trim();
  if (!/^\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$/.test(pnpm) || pnpm !== dependency.pnpmVersion) {
    fail('pnpm tool version must match the source packageManager');
  }
  await runPnpm(root, ['store', 'status'], 'pnpm store status');
  return pnpm;
}

async function collectToolVersions(root, trustedSourceReal, dependency, baseline, pnpm) {
  await validatePlaywrightDependency(root, trustedSourceReal, dependency, baseline);
  const playwrightResult = await runNode(root, [playwrightCLI, '--version'], 'Playwright version query');
  await validatePlaywrightDependency(root, trustedSourceReal, dependency, baseline);
  const node = process.versions.node;
  const playwrightOutput = playwrightResult.stdout.toString('utf8').trim();
  const playwrightMatch = /^Version\s+(\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?)$/.exec(playwrightOutput);
  if (!/^\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$/.test(node)) {
    fail('Node tool version is not canonical semver');
  }
  if (!playwrightMatch || playwrightMatch[1] !== dependency.version) {
    fail('Playwright tool version must match the source pnpm lock');
  }
  return {node, pnpm, playwright: playwrightMatch[1]};
}

function secretLikeString(value) {
  return (
    /-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----/i.test(value) ||
    /\bBearer\s+[A-Za-z0-9._~+/=-]{8,}/i.test(value) ||
    /\bAKIA[0-9A-Z]{16}\b/.test(value) ||
    /\b(?:password|passwd|token|secret|api[_-]?key|private[_-]?key|authorization|cookie)\s*(?:[:=]|\0)\s*\S+/i.test(
      value
    )
  );
}

function containsJSONSecret(value, key = '') {
  if (value === null || value === undefined) return false;
  if (typeof value === 'string') {
    return (
      (/(?:password|passwd|token|secret|api[_-]?key|private[_-]?key|authorization|cookie)/i.test(key) &&
        value.trim() !== '') ||
      secretLikeString(value)
    );
  }
  if (Array.isArray(value)) return value.some((item) => containsJSONSecret(item));
  if (typeof value === 'object') {
    return Object.entries(value).some(([childKey, item]) => containsJSONSecret(item, childKey));
  }
  return false;
}

function parseRawReport(bytes) {
  let report;
  try {
    report = JSON.parse(bytes.toString('utf8'));
  } catch (error) {
    fail(`direct Playwright evidence did not return valid JSON: ${error.message}`);
  }
  if (containsJSONSecret(report)) fail('retained Playwright JSON contains a secret-like value');
  return report;
}

function normalizedTestFile(root, report, value) {
  if (typeof value !== 'string' || value === '') return '';
  const configuredRoot = report?.config?.rootDir;
  const sourceRoot =
    typeof configuredRoot === 'string' && path.isAbsolute(configuredRoot) && containedPath(root, configuredRoot)
      ? configuredRoot
      : root;
  const resolved = path.isAbsolute(value) ? value : path.resolve(sourceRoot, value);
  return containedPath(root, resolved) ? gitPath(path.relative(root, resolved)) : '';
}

function collectSpecs(suite, result = []) {
  if (!suite || typeof suite !== 'object') return result;
  if (Array.isArray(suite.specs)) result.push(...suite.specs);
  if (Array.isArray(suite.suites)) {
    for (const child of suite.suites) collectSpecs(child, result);
  }
  return result;
}

function validatePlaywrightReport(root, report, toolVersions) {
  if (report?.config?.version !== toolVersions.playwright) {
    fail('direct Playwright evidence version must match the queried Playwright tool');
  }
  const stats = report?.stats;
  if (!stats || !Number.isInteger(stats.expected) || stats.expected < 0) {
    fail('direct Playwright evidence must contain integer result statistics');
  }
  for (const field of ['skipped', 'flaky', 'unexpected']) {
    if (!Number.isInteger(stats[field]) || stats[field] < 0) {
      fail(`direct Playwright evidence ${field} count must be a non-negative integer`);
    }
    if (stats[field] !== 0) fail(`direct Playwright evidence reported ${field} tests`);
  }
  if (stats.expected === 0) fail('direct Playwright evidence returned zero tests');
  if (stats.expected !== 1) fail(`direct Playwright evidence must run exactly one test, got ${stats.expected}`);
  if (!Array.isArray(report.errors) || report.errors.length !== 0) {
    fail('direct Playwright evidence contains top-level errors');
  }
  if (!Array.isArray(report.suites)) fail('direct Playwright evidence must contain suites');
  const specs = report.suites.flatMap((suite) => collectSpecs(suite));
  if (specs.length !== 1) fail('direct Playwright evidence must retain exactly one spec');
  const spec = specs[0];
  if (normalizedTestFile(root, report, spec.file) !== automatedTest) {
    fail(`direct Playwright evidence source must be ${automatedTest}`);
  }
  if (spec.title !== exactTitle) fail('direct Playwright evidence title set mismatch');
  if (spec.ok !== true || !Array.isArray(spec.tests) || spec.tests.length !== 1) {
    fail('direct Playwright evidence spec did not pass exactly once');
  }
  const item = spec.tests[0];
  if (item.projectName !== project) fail(`direct Playwright evidence project must be ${project}`);
  if (
    item.expectedStatus !== 'passed' ||
    item.status !== 'expected' ||
    !Array.isArray(item.results) ||
    item.results.length !== 1 ||
    item.results[0]?.retry !== 0 ||
    item.results[0]?.status !== 'passed' ||
    !Array.isArray(item.results[0]?.errors) ||
    item.results[0].errors.length !== 0
  ) {
    fail('direct Playwright evidence must contain exactly one passed, non-retried attempt');
  }
  const attachments = item.results[0].attachments;
  if (!Array.isArray(attachments)) fail('direct Playwright evidence screenshot set mismatch');
  const actualNames = attachments.map((attachment) => attachment?.name);
  if (
    actualNames.length !== screenshotNames.length ||
    new Set(actualNames).size !== actualNames.length ||
    screenshotNames.some((name, index) => actualNames[index] !== name) ||
    attachments.some((attachment) => attachment?.contentType !== 'image/png')
  ) {
    fail('direct Playwright evidence screenshot set mismatch');
  }
  const started = Date.parse(stats.startTime);
  if (!Number.isFinite(started) || !Number.isFinite(stats.duration) || stats.duration < 0) {
    fail('direct Playwright evidence must contain a valid startTime and duration');
  }
  return {
    startedAt: new Date(started).toISOString(),
    completedAt: new Date(started + stats.duration).toISOString(),
    attachments,
  };
}

function crc32(bytes) {
  let value = 0xffffffff;
  for (const byte of bytes) {
    value ^= byte;
    for (let bit = 0; bit < 8; bit += 1) {
      value = (value >>> 1) ^ (0xedb88320 & -(value & 1));
    }
  }
  return (value ^ 0xffffffff) >>> 0;
}

function pngMetadataText(type, data) {
  if (type === 'tEXt') return data.toString('latin1');
  if (type === 'zTXt') {
    const separator = data.indexOf(0);
    if (separator < 0 || data[separator + 1] !== 0) return '';
    try {
      return `${data.subarray(0, separator).toString('latin1')}\0${inflateSync(data.subarray(separator + 2)).toString(
        'utf8'
      )}`;
    } catch {
      return '';
    }
  }
  if (type === 'iTXt') return data.toString('utf8');
  return '';
}

function inspectPNG(bytes, name) {
  if (bytes.length < pngSignature.length || !bytes.subarray(0, pngSignature.length).equals(pngSignature)) {
    fail(`invalid PNG signature: ${name}`);
  }
  let offset = pngSignature.length;
  let width;
  let height;
  let first = true;
  let ended = false;
  const metadata = [];
  while (offset + 12 <= bytes.length) {
    const length = bytes.readUInt32BE(offset);
    const end = offset + 12 + length;
    if (end > bytes.length) fail(`invalid PNG structure: ${name}`);
    const typeBytes = bytes.subarray(offset + 4, offset + 8);
    const type = typeBytes.toString('ascii');
    const data = bytes.subarray(offset + 8, offset + 8 + length);
    const expectedCRC = bytes.readUInt32BE(offset + 8 + length);
    if (crc32(Buffer.concat([typeBytes, data])) !== expectedCRC) fail(`invalid PNG checksum: ${name}`);
    if (first) {
      if (type !== 'IHDR' || length !== 13) fail(`invalid PNG header: ${name}`);
      width = data.readUInt32BE(0);
      height = data.readUInt32BE(4);
      first = false;
    }
    if (['tEXt', 'zTXt', 'iTXt'].includes(type)) metadata.push(pngMetadataText(type, data));
    offset = end;
    if (type === 'IEND') {
      ended = true;
      break;
    }
  }
  if (!ended || offset !== bytes.length) fail(`invalid PNG structure: ${name}`);
  if (!width || !height) fail(`invalid PNG dimensions: ${name}`);
  if (metadata.some((value) => secretLikeString(value))) {
    fail(`PNG metadata contains a secret-like value: ${name}`);
  }
  return {width, height};
}

async function attachmentPath(root, value, name) {
  if (typeof value !== 'string' || value === '') fail(`missing screenshot attachment path: ${name}`);
  const resolved = path.resolve(root, value);
  const relative = path.relative(root, resolved);
  if (relative === '..' || relative.startsWith(`..${path.sep}`) || path.isAbsolute(relative)) {
    fail(`screenshot attachment must remain inside the repository: ${name}`);
  }
  const normalized = gitPath(relative);
  if (!(normalized === runnerOutput || normalized.startsWith(`${runnerOutput}/`))) {
    fail(`screenshot attachment must come from the purpose-built Playwright output: ${name}`);
  }
  await assertSafePath(root, resolved, 'transient Playwright output', {regularFile: true});
  const rootReal = await realpath(root);
  const resolvedReal = await realpath(resolved);
  if (!containedPath(rootReal, resolvedReal)) fail('transient Playwright output resolved outside its trusted root');
  return resolved;
}

async function inspectScreenshots(root, attachments, prefix) {
  const screenshots = [];
  for (const [index, attachment] of attachments.entries()) {
    const name = screenshotNames[index];
    const source = await attachmentPath(root, attachment.path, name);
    let bytes;
    try {
      bytes = await readFile(source);
    } catch {
      fail(`missing screenshot attachment: ${name}`);
    }
    const {width, height} = inspectPNG(bytes, name);
    screenshots.push({
      name,
      path: `${prefix}/screenshots/${name}`,
      sha256: sha256(bytes),
      width,
      height,
      bytes,
    });
  }
  return screenshots;
}

function binding(prefix, relative, bytes) {
  return {path: `${prefix}/${relative}`, sha256: sha256(bytes)};
}

function requireBoundNetworkProof(testSource, fixtureText) {
  const titleOffset = testSource.indexOf(exactTitle);
  if (titleOffset < 0) fail('exact browser evidence title is absent from the automated test');
  const nextTest = /\n\s*test(?:\.(?:only|skip|fixme))?\s*\(/g;
  nextTest.lastIndex = titleOffset + exactTitle.length;
  const next = nextTest.exec(testSource);
  const evidenceBody = testSource.slice(titleOffset, next?.index ?? testSource.length);
  if (!/expect\s*\(\s*controlPlane\.externalAttempts\s*\)\s*\.toEqual\s*\(\s*\[\s*\]\s*\)/.test(evidenceBody)) {
    fail('exact browser evidence test must assert zero external attempts');
  }
  if (
    !/page\.route\s*\(\s*['"]\*\*\/\*['"]/.test(fixtureText) ||
    !/!\s*isLocalHost\s*\(/.test(fixtureText) ||
    !/externalAttempts\.push\s*\(/.test(fixtureText) ||
    !/route\.abort\s*\(/.test(fixtureText)
  ) {
    fail('bound control-plane fixture must record and block non-local requests');
  }
  if (
    !/const\s+expectedNodeVersion\s*=\s*process\.env\.DISTR_EVIDENCE_NODE_VERSION\s*;\s*if\s*\(\s*expectedNodeVersion\s*!==\s*undefined\s*\)\s*\{\s*expect\s*\(\s*process\.versions\.node\s*\)\s*\.toBe\s*\(\s*expectedNodeVersion\s*\)\s*;\s*\}/.test(
      evidenceBody
    )
  ) {
    fail('exact browser evidence test must verify the Playwright Node runtime');
  }
}

async function main() {
  const options = parseArguments(process.argv.slice(2));
  const root = await repositoryRoot();
  const outDir = repositoryPath(root, options.outDir, '--out-dir');
  await assertSafePath(root, outDir, '--out-dir');
  const prefix = gitPath(path.relative(root, outDir));
  if (prefix === runnerOutput || prefix.startsWith(`${runnerOutput}/`)) {
    fail('--out-dir must not overlap the transient Playwright output');
  }
  await requireExactHead(root, options.sourceCommit);
  await requireEmptyOutput(outDir);
  const dependency = await sourcePlaywrightDependency(root, options.sourceCommit);
  const trustedSourceReal = await trustedDependencySource(root);
  await validatePlaywrightDependency(root, trustedSourceReal, dependency);
  const pnpmVersion = await validateRootPnpm(root, dependency);
  const dependencyBaseline = await validatePlaywrightDependency(root, trustedSourceReal, dependency);
  const isolated = await createIsolatedWorktree(root, options.sourceCommit);
  try {
    const tracked = await trackedSet(isolated.isolatedRoot);
    const automatedBinding = await sourceBinding(
      isolated.isolatedRoot,
      tracked,
      options.sourceCommit,
      automatedTest,
      'automated test'
    );
    const manualBinding = await sourceBinding(
      isolated.isolatedRoot,
      tracked,
      options.sourceCommit,
      manualEvidence,
      'manual evidence'
    );
    const executionSources = [];
    for (const [source, label] of [
      [evidenceConfig, 'evidence Playwright config'],
      [baseConfig, 'base Playwright config'],
      [fixtureSource, 'control-plane fixture'],
      ['package.json', 'package manifest'],
    ]) {
      executionSources.push(await sourceBinding(isolated.isolatedRoot, tracked, options.sourceCommit, source, label));
    }
    if (tracked.has('pnpm-lock.yaml')) {
      executionSources.push(
        await sourceBinding(
          isolated.isolatedRoot,
          tracked,
          options.sourceCommit,
          'pnpm-lock.yaml',
          'pnpm dependency lockfile'
        )
      );
    }
    const testSource = await readFile(path.join(isolated.isolatedRoot, ...automatedTest.split('/')), 'utf8');
    const fixtureText = await readFile(path.join(isolated.isolatedRoot, ...fixtureSource.split('/')), 'utf8');
    requireBoundNetworkProof(testSource, fixtureText);

    let executed;
    let toolVersions;
    try {
      toolVersions = await collectToolVersions(
        isolated.isolatedRoot,
        trustedSourceReal,
        dependency,
        dependencyBaseline,
        pnpmVersion
      );
      await validatePlaywrightDependency(root, trustedSourceReal, dependency, dependencyBaseline);
      await validatePlaywrightDependency(isolated.isolatedRoot, trustedSourceReal, dependency, dependencyBaseline);
      executed = await runPlaywright(isolated.isolatedRoot, toolVersions.node);
    } finally {
      await requireUnchangedHead(root, options.sourceCommit, 'source worktree HEAD drifted during isolated execution');
      await requireUnchangedHead(
        isolated.isolatedRoot,
        options.sourceCommit,
        'isolated source worktree HEAD drifted during execution'
      );
      await requireCleanWorktree(root, 'source worktree drifted during isolated execution');
      await requireCleanWorktree(isolated.isolatedRoot, 'isolated source worktree drifted during execution', [
        'node_modules',
        runnerOutput,
      ]);
      await requireDependencyLinkUnchanged(isolated.dependencyLink);
      await validatePlaywrightDependency(root, trustedSourceReal, dependency, dependencyBaseline);
      await validatePlaywrightDependency(isolated.isolatedRoot, trustedSourceReal, dependency, dependencyBaseline);
    }
    const report = parseRawReport(executed.stdout);
    const run = validatePlaywrightReport(isolated.isolatedRoot, report, toolVersions);
    const screenshots = await inspectScreenshots(isolated.isolatedRoot, run.attachments, prefix);

    const rawRelative = `raw/${acceptanceId}-playwright.json`;
    const genericRelative = `results/${acceptanceId}-test-result.json`;
    const classRelative = `results/${acceptanceId}-browser-result.json`;
    const wrapperRelative = `${acceptanceId}.json`;
    const rawBytes = stableJSON(report);
    const genericBytes = stableJSON({
      schema: testResultSchema,
      sourceCommit: options.sourceCommit,
      command: {
        runner: 'playwright',
        argv: exactCommand,
        selectedTestSource: automatedTest,
      },
      exitCode: 0,
      tests: {expected: 1, passed: 1, failed: 0, skipped: 0},
      status: 'passed',
      startedAt: run.startedAt,
      completedAt: run.completedAt,
    });
    const classBytes = stableJSON({
      schema: browserResultSchema,
      sourceCommit: options.sourceCommit,
      runner: 'playwright',
      status: 'passed',
      project,
      testSource: automatedTest,
      testTitles: [exactTitle],
      tests: {expected: 1, passed: 1, unexpected: 0, flaky: 0, skipped: 0},
      rawResult: binding(prefix, rawRelative, rawBytes),
      screenshots: screenshots.map(({bytes: _bytes, ...screenshot}) => screenshot),
      networkProof: {
        mode: 'bound-test-assertion',
        testTitle: exactTitle,
        externalAttempts: 0,
      },
      executionSources,
      toolVersions,
    });
    const wrapperBytes = stableJSON({
      schema: evidenceSchema,
      acceptanceId,
      owner,
      proofClass: 'browser-e2e',
      sourceCommit: options.sourceCommit,
      automatedTest: automatedBinding,
      manualEvidence: manualBinding,
      testResult: binding(prefix, genericRelative, genericBytes),
      classEvidence: binding(prefix, classRelative, classBytes),
    });

    const outputs = [
      {relative: rawRelative, bytes: rawBytes},
      {relative: genericRelative, bytes: genericBytes},
      {relative: classRelative, bytes: classBytes},
      {relative: wrapperRelative, bytes: wrapperBytes},
      ...screenshots.map((screenshot) => ({relative: `screenshots/${screenshot.name}`, bytes: screenshot.bytes})),
    ];
    for (const output of outputs) {
      const destination = path.join(outDir, ...output.relative.split('/'));
      await atomicExclusiveWrite(root, destination, output.bytes);
    }
    process.stdout.write(
      `Executed and packaged ${acceptanceId} browser evidence: 1 passed test, ${screenshots.length} screenshots\n`
    );
  } finally {
    await cleanupIsolatedWorktree(root, isolated);
  }
}

main().catch((error) => {
  process.stderr.write(`${error.message}\n`);
  process.exitCode = 1;
});
