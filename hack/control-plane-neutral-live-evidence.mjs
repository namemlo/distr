#!/usr/bin/env node

import {execFile, spawn} from 'node:child_process';
import {createHash, randomBytes} from 'node:crypto';
import {lstat, mkdir, mkdtemp, open, readFile, realpath, rm, unlink} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {promisify} from 'node:util';
import {controlledGoEnvironment, declaredGoTestsForSources} from './control-plane-acceptance-check.mjs';
import {scanLines} from './control-plane-adopter-term-scan.mjs';
import {
  neutralLiveAcceptanceId as acceptanceId,
  neutralLiveAutomatedTest as automatedTest,
  neutralLiveResultSchema as classSchema,
  neutralLiveEvidenceSchema as evidenceSchema,
  neutralLiveExecutionSourcePaths as executionSourcePaths,
  neutralLiveRunnerCommand as liveCommand,
  neutralLiveManualEvidence as manualEvidence,
  neutralLiveOwner as owner,
  neutralLiveProfile as profileName,
  neutralLiveTestResultSchema as testResultSchema,
} from './control-plane-neutral-live-evidence-contract.mjs';

const execFileAsync = promisify(execFile);
const contractPath = 'docs/release/control-plane-acceptance-contract.json';
const commitPattern = /^[0-9a-f]{40}$/;
const checksumPattern = /^sha256:[0-9a-f]{64}$/;

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

async function git(root, args, options = {}) {
  try {
    return await execFileAsync('git', args, {cwd: root, maxBuffer: 64 * 1024 * 1024, ...options});
  } catch (error) {
    if (options.allowFailure) return error;
    throw error;
  }
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
    if (values[key] !== undefined || index + 1 >= argv.length) fail(`invalid option: ${option}`);
    values[key] = argv[++index];
  }
  if (!commitPattern.test(values.sourceCommit ?? '')) {
    fail('--source-commit must be a full lowercase 40-character git commit');
  }
  if (typeof values.outDir !== 'string' || values.outDir === '') fail('--out-dir is required');
  return values;
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

async function repositoryRoot() {
  try {
    const {stdout} = await git(process.cwd(), ['rev-parse', '--show-toplevel']);
    return stdout.trim();
  } catch {
    fail('neutral live evidence generation requires a git worktree');
  }
}

async function requireExactCleanHead(root, sourceCommit, phase = 'before execution') {
  const {stdout: head} = await git(root, ['rev-parse', 'HEAD']);
  if (head.trim() !== sourceCommit) fail(`source worktree HEAD must equal selected source commit ${phase}`);
  const {stdout} = await git(root, ['status', '--porcelain=v1', '-z', '--untracked-files=all'], {encoding: 'buffer'});
  const dirty = stdout.toString('utf8').split('\0').find(Boolean);
  if (dirty) fail(`source worktree must be exactly clean ${phase}: ${gitPath(dirty.slice(3))}`);
}

async function trackedSet(root) {
  const {stdout} = await git(root, ['ls-files', '-z'], {encoding: 'buffer'});
  return new Set(stdout.toString('utf8').split('\0').filter(Boolean).map(gitPath));
}

async function sourceBinding(root, tracked, sourceCommit, relative, label) {
  if (!tracked.has(relative)) fail(`${label} must be tracked by git: ${relative}`);
  let bytes;
  try {
    const {stdout} = await git(root, ['show', `${sourceCommit}:${relative}`], {encoding: 'buffer'});
    bytes = stdout;
  } catch {
    fail(`${label} is absent from source commit: ${relative}`);
  }
  return {path: relative, sha256: sha256(bytes), bytes};
}

async function createIsolatedWorktree(root, sourceCommit) {
  const base = await mkdtemp(path.join(tmpdir(), 'control-plane-neutral-live-'));
  const checkout = path.join(base, 'source');
  let registered = false;
  try {
    await git(root, ['worktree', 'add', '--detach', '--quiet', checkout, sourceCommit]);
    registered = true;
    await requireExactCleanHead(checkout, sourceCommit, 'in isolated checkout');
    return {base, checkout};
  } catch (error) {
    if (registered) await git(root, ['worktree', 'remove', '--force', checkout], {allowFailure: true});
    await rm(base, {recursive: true, force: true});
    throw error;
  }
}

async function removeIsolatedWorktree(root, worktree) {
  if (!worktree) return;
  await git(root, ['worktree', 'remove', '--force', worktree.checkout], {allowFailure: true});
  await rm(worktree.base, {recursive: true, force: true});
  await git(root, ['worktree', 'prune'], {allowFailure: true});
}

function selectedTestPattern(selectedTests) {
  return `^(?:${selectedTests.map((name) => name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')).join('|')})$`;
}

function validateProfile(contract) {
  const rule = contract?.acceptance?.[acceptanceId];
  const profile = contract?.profiles?.[profileName];
  if (
    rule?.owner !== owner ||
    rule?.profile !== profileName ||
    profile?.automatedTest !== automatedTest ||
    profile?.manualEvidence !== manualEvidence ||
    profile?.testRunner !== 'go-test' ||
    JSON.stringify(profile?.allowedProofClasses) !== JSON.stringify(['neutral-live-execution']) ||
    !Array.isArray(profile?.selectedTests) ||
    profile.selectedTests.length === 0 ||
    new Set(profile.selectedTests).size !== profile.selectedTests.length ||
    profile.selectedTests.some((name) => !/^Test(?:[A-Z0-9_][A-Za-z0-9_]*)$/.test(name))
  ) {
    fail('AC-53 acceptance contract must define the exact neutral live Go-test profile');
  }
  return profile;
}

async function compiledPackageSources(root, packagePath, selectedTests) {
  const {stdout} = await execFileAsync('go', ['list', '-json', packagePath], {
    cwd: root,
    env: controlledGoEnvironment(),
    maxBuffer: 16 * 1024 * 1024,
  });
  const metadata = JSON.parse(stdout);
  const rootReal = await realpath(root);
  const packageReal = await realpath(metadata.Dir);
  const relative = path.relative(rootReal, packageReal);
  if (relative === '..' || relative.startsWith(`..${path.sep}`) || path.isAbsolute(relative)) {
    fail('reference executor package resolves outside the isolated source checkout');
  }
  const sources = [];
  for (const name of [...(metadata.TestGoFiles ?? []), ...(metadata.XTestGoFiles ?? [])]) {
    const resolved = path.join(metadata.Dir, name);
    sources.push({path: gitPath(path.relative(root, resolved)), bytes: await readFile(resolved)});
  }
  const declarations = await declaredGoTestsForSources(sources);
  for (const selected of selectedTests) {
    const count = [...declarations.values()].flat().filter((name) => name === selected).length;
    if (count !== 1) fail(`selected reference executor test ${selected} must have exactly one declaration`);
  }
  const manifest = [];
  for (const source of sources) {
    const {stdout: committed} = await git(root, ['show', `HEAD:${source.path}`], {encoding: 'buffer'});
    manifest.push({path: source.path, sha256: sha256(committed)});
  }
  return manifest.sort((left, right) => left.path.localeCompare(right.path));
}

function runProcess(command, args, {cwd, env}) {
  return new Promise((resolve, reject) => {
    const startedAt = new Date().toISOString();
    const child = spawn(command, args, {
      cwd,
      env,
      shell: false,
      windowsHide: true,
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    const stdout = [];
    const stderr = [];
    child.stdout.on('data', (chunk) => stdout.push(chunk));
    child.stderr.on('data', (chunk) => stderr.push(chunk));
    child.on('error', reject);
    child.on('close', (exitCode) =>
      resolve({
        exitCode: exitCode ?? 1,
        stdout: Buffer.concat(stdout),
        stderr: Buffer.concat(stderr),
        startedAt,
        completedAt: new Date().toISOString(),
      })
    );
  });
}

function parseGoResult(output, selectedTests) {
  const tests = new Map();
  for (const line of output.toString('utf8').split(/\r?\n/)) {
    if (!line.trim()) continue;
    try {
      const event = JSON.parse(line);
      if (typeof event.Test === 'string' && ['run', 'pass', 'fail', 'skip'].includes(event.Action)) {
        tests.set(event.Test, event.Action);
      }
    } catch {
      // Package diagnostics are validated through the process exit status and retained counts.
    }
  }
  const topLevel = [...tests]
    .filter(([name]) => !name.includes('/'))
    .map(([name, status]) => ({name, status}))
    .sort((left, right) => left.name.localeCompare(right.name));
  const actions = [...tests.values()];
  if (
    topLevel.length !== selectedTests.length ||
    topLevel.some((item) => item.status !== 'pass' || !selectedTests.includes(item.name)) ||
    selectedTests.some((name) => !topLevel.some((item) => item.name === name)) ||
    actions.some((status) => status !== 'pass')
  ) {
    fail('focused reference executor tests must all pass without skips or extra top-level tests');
  }
  return {
    expected: tests.size,
    passed: actions.filter((status) => status === 'pass').length,
    failed: actions.filter((status) => status === 'fail').length,
    skipped: actions.filter((status) => status === 'skip').length,
    topLevel,
  };
}

function validateLiveReport(report, sourceCommit) {
  if (
    !report ||
    report.proofMode !== 'live-hub-api' ||
    report.status !== 'passed' ||
    report.acceptanceEligible !== true ||
    report.liveStack?.started !== true ||
    report.liveStack?.hubImage?.sourceCommit !== sourceCommit ||
    !checksumPattern.test(report.liveStack?.hubImage?.imageId ?? '') ||
    !Array.isArray(report.targets) ||
    report.targets.length !== 2 ||
    report.nonLocalCalls !== 0 ||
    report.cleanup?.completed !== true
  ) {
    fail('neutral live runner must return a complete source-bound acceptance-eligible live-hub-api result');
  }
}

async function requireEmptyOutput(outDir) {
  try {
    await lstat(outDir);
    fail('--out-dir must not already exist');
  } catch (error) {
    if (error.code !== 'ENOENT') throw error;
  }
}

async function writeExclusive(root, destination, bytes) {
  const relative = gitPath(path.relative(root, destination));
  repositoryPath(root, relative, 'generated evidence path');
  await mkdir(path.dirname(destination), {recursive: true});
  const temporary = path.join(
    path.dirname(destination),
    `.${path.basename(destination)}.${process.pid}.${randomBytes(8).toString('hex')}.tmp`
  );
  const handle = await open(temporary, 'wx', 0o600);
  try {
    await handle.writeFile(bytes);
    await handle.sync();
  } finally {
    await handle.close();
  }
  try {
    const target = await open(destination, 'wx', 0o600);
    try {
      await target.writeFile(bytes);
      await target.sync();
    } finally {
      await target.close();
    }
  } finally {
    await unlink(temporary).catch(() => {});
  }
}

async function main() {
  const options = parseArguments(process.argv.slice(2));
  const root = await repositoryRoot();
  const outDir = repositoryPath(root, options.outDir, '--out-dir');
  await requireExactCleanHead(root, options.sourceCommit);
  await requireEmptyOutput(outDir);

  let isolated;
  try {
    isolated = await createIsolatedWorktree(root, options.sourceCommit);
    const tracked = await trackedSet(isolated.checkout);
    const contract = JSON.parse(await readFile(path.join(isolated.checkout, ...contractPath.split('/')), 'utf8'));
    const profile = validateProfile(contract);
    const automatedBinding = await sourceBinding(
      isolated.checkout,
      tracked,
      options.sourceCommit,
      automatedTest,
      'automated test'
    );
    const manualBinding = await sourceBinding(
      isolated.checkout,
      tracked,
      options.sourceCommit,
      manualEvidence,
      'manual evidence'
    );
    const executionSources = [];
    const findings = [];
    for (const sourcePath of executionSourcePaths) {
      const binding = await sourceBinding(
        isolated.checkout,
        tracked,
        options.sourceCommit,
        sourcePath,
        'neutral live execution source'
      );
      executionSources.push({path: binding.path, sha256: binding.sha256});
      const text = binding.bytes.toString('utf8');
      findings.push(
        ...scanLines(
          sourcePath,
          text.split(/\r?\n/).map((line, index) => ({line: index + 1, text: line}))
        )
      );
    }
    if (findings.length !== 0) {
      fail(`neutral live execution sources contain prohibited adopter terms: ${JSON.stringify(findings)}`);
    }

    const selectedTests = [...profile.selectedTests];
    const packagePath = `./${path.posix.dirname(automatedTest)}`;
    const testCommand = {
      runner: 'go-test',
      argv: ['go', 'test', packagePath, '-run', selectedTestPattern(selectedTests), '-count=1', '-json'],
      selectedTestSource: automatedTest,
      selectedTests,
    };
    const compiledSources = await compiledPackageSources(isolated.checkout, packagePath, selectedTests);
    const testExecution = await runProcess(testCommand.argv[0], testCommand.argv.slice(1), {
      cwd: isolated.checkout,
      env: controlledGoEnvironment(),
    });
    const tests = parseGoResult(testExecution.stdout, selectedTests);
    if (testExecution.exitCode !== 0) fail('focused reference executor tests exited nonzero');

    const liveExecution = await runProcess(liveCommand[0], liveCommand.slice(1), {
      cwd: isolated.checkout,
      env: {...process.env, DISTR_CP_SOURCE_COMMIT: options.sourceCommit},
    });
    if (liveExecution.exitCode !== 0) {
      fail(
        `neutral live runner exited nonzero; stdout bytes=${liveExecution.stdout.length}; stderr bytes=${liveExecution.stderr.length}`
      );
    }
    let liveReport;
    try {
      liveReport = JSON.parse(liveExecution.stdout.toString('utf8'));
    } catch {
      fail('neutral live runner must emit one JSON report');
    }
    validateLiveReport(liveReport, options.sourceCommit);
    await requireExactCleanHead(isolated.checkout, options.sourceCommit, 'after evidence execution');

    const prefix = gitPath(path.relative(root, outDir));
    const genericRelative = 'results/AC-53-test-result.json';
    const classRelative = 'results/AC-53-neutral-live-result.json';
    const wrapperRelative = 'AC-53.json';
    const genericBytes = stableJSON({
      schema: testResultSchema,
      sourceCommit: options.sourceCommit,
      command: testCommand,
      exitCode: 0,
      tests,
      compiledPackageSources: compiledSources,
      status: 'passed',
      startedAt: testExecution.startedAt,
      completedAt: testExecution.completedAt,
    });
    const classBytes = stableJSON({
      ...liveReport,
      schema: classSchema,
      sourceCommit: options.sourceCommit,
      executionSources,
      neutralityProof: {
        mode: 'source-bound-community-neutrality',
        scannedPaths: [...executionSourcePaths],
        findings: [],
      },
    });
    const wrapperBytes = stableJSON({
      schema: evidenceSchema,
      acceptanceId,
      owner,
      proofClass: 'neutral-live-execution',
      sourceCommit: options.sourceCommit,
      automatedTest: {path: automatedBinding.path, sha256: automatedBinding.sha256},
      manualEvidence: {path: manualBinding.path, sha256: manualBinding.sha256},
      testResult: {path: `${prefix}/${genericRelative}`, sha256: sha256(genericBytes)},
      classEvidence: {path: `${prefix}/${classRelative}`, sha256: sha256(classBytes)},
    });

    await requireExactCleanHead(root, options.sourceCommit, 'after isolated evidence execution');
    for (const [relative, bytes] of [
      [genericRelative, genericBytes],
      [classRelative, classBytes],
      [wrapperRelative, wrapperBytes],
    ]) {
      await writeExclusive(root, path.join(outDir, ...relative.split('/')), bytes);
    }
    process.stdout.write(`Executed and packaged ${acceptanceId} source-bound neutral live evidence\n`);
  } finally {
    await removeIsolatedWorktree(root, isolated);
  }
}

main().catch((error) => {
  process.stderr.write(`${error.message}\n`);
  process.exitCode = 1;
});
