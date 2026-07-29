#!/usr/bin/env node

import {execFile, spawn} from 'node:child_process';
import {createHash, randomBytes} from 'node:crypto';
import {link, lstat, mkdir, mkdtemp, open, readFile, realpath, rename, rm, unlink} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {promisify} from 'node:util';
import {
  controlledGoEnvironment,
  declaredGoTests,
  declaredGoTestsForSources,
  parseAcceptanceLedger,
} from './control-plane-acceptance-check.mjs';

const execFileAsync = promisify(execFile);
const contractPath = 'docs/release/control-plane-acceptance-contract.json';
const evidenceDirectory = 'docs/release/evidence';
const contractSchema = 'distr.control-plane-acceptance-contract/v1';
const resultSchema = 'distr.control-plane-test-result/v1';
const evidenceSchema = 'distr.control-plane-acceptance-evidence/v1';
const commitPattern = /^[0-9a-f]{40}$/;
const safeUntrackedPrefixes = ['work/', 'output/', `${evidenceDirectory}/`];

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
  return `${JSON.stringify(value, null, 2)}\n`;
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

function isWithin(root, candidate) {
  const relative = path.relative(root, candidate);
  return relative === '' || (!path.isAbsolute(relative) && relative !== '..' && !relative.startsWith(`..${path.sep}`));
}

async function pathFacts(value) {
  try {
    return await lstat(value);
  } catch (error) {
    if (error.code === 'ENOENT') {
      return undefined;
    }
    throw error;
  }
}

async function requireSafeExistingDirectory(rootReal, value, label) {
  const facts = await pathFacts(value);
  if (!facts) {
    return false;
  }
  if (facts.isSymbolicLink()) {
    fail(`${label} must not be a symbolic link or reparse point: ${gitPath(value)}`);
  }
  if (!facts.isDirectory()) {
    fail(`${label} must be a regular directory: ${gitPath(value)}`);
  }
  const resolved = await realpath(value);
  if (!isWithin(rootReal, resolved)) {
    fail(`${label} resolves outside the repository: ${gitPath(value)}`);
  }
  return true;
}

async function prepareSafeOutputPath(root, value, label, {immutable = false} = {}) {
  const resolved = repositoryPath(root, value, label);
  const rootReal = await realpath(root);
  const relative = path.relative(root, resolved);
  const parts = relative.split(path.sep);
  let current = root;
  await requireSafeExistingDirectory(rootReal, current, `${label} parent`);
  for (const part of parts.slice(0, -1)) {
    current = path.join(current, part);
    if (!(await requireSafeExistingDirectory(rootReal, current, `${label} parent`))) {
      try {
        await mkdir(current, {recursive: false});
      } catch (error) {
        if (error.code !== 'EEXIST') throw error;
      }
      await requireSafeExistingDirectory(rootReal, current, `${label} parent`);
    }
  }
  const facts = await pathFacts(resolved);
  if (facts?.isSymbolicLink()) {
    fail(`${label} path must not be a symbolic link or reparse point: ${gitPath(value)}`);
  }
  if (facts && immutable) {
    fail(`${label} already exists and is immutable: ${gitPath(value)}`);
  }
  if (facts && !facts.isFile()) {
    fail(`${label} path must be a regular file when it already exists: ${gitPath(value)}`);
  }
  if (facts && !isWithin(rootReal, await realpath(resolved))) {
    fail(`${label} path resolves outside the repository: ${gitPath(value)}`);
  }
  return {resolved, parent: path.dirname(resolved)};
}

async function writeExclusiveTemporary(parent, value, bytes) {
  const temporary = path.join(parent, `.${path.basename(value)}.${process.pid}.${randomBytes(12).toString('hex')}.tmp`);
  const handle = await open(temporary, 'wx', 0o600);
  try {
    await handle.writeFile(bytes);
    await handle.sync();
  } finally {
    await handle.close();
  }
  return temporary;
}

async function atomicWriteFile(root, value, bytes, label) {
  let temporary;
  try {
    const prepared = await prepareSafeOutputPath(root, value, label);
    temporary = await writeExclusiveTemporary(prepared.parent, value, bytes);
    await prepareSafeOutputPath(root, value, label);
    await requireSafeExistingDirectory(await realpath(root), prepared.parent, `${label} parent`);
    await rename(temporary, prepared.resolved);
    temporary = undefined;
    const finalFacts = await lstat(prepared.resolved);
    if (finalFacts.isSymbolicLink() || !finalFacts.isFile()) {
      fail(`${label} atomic write did not produce a regular file: ${gitPath(value)}`);
    }
  } finally {
    if (temporary) {
      await unlink(temporary).catch(() => {});
    }
  }
}

async function publishImmutableFile(root, value, bytes, label) {
  let temporary;
  try {
    const prepared = await prepareSafeOutputPath(root, value, label, {immutable: true});
    temporary = await writeExclusiveTemporary(prepared.parent, value, bytes);
    await prepareSafeOutputPath(root, value, label, {immutable: true});
    await requireSafeExistingDirectory(await realpath(root), prepared.parent, `${label} parent`);
    try {
      await link(temporary, prepared.resolved);
    } catch (error) {
      if (error.code === 'EEXIST') {
        fail(`${label} already exists and is immutable: ${gitPath(value)}`);
      }
      throw error;
    }
    const finalFacts = await lstat(prepared.resolved);
    if (finalFacts.isSymbolicLink() || !finalFacts.isFile()) {
      fail(`${label} exclusive publication did not produce a regular file: ${gitPath(value)}`);
    }
  } finally {
    if (temporary) {
      await unlink(temporary).catch(() => {});
    }
  }
}

async function git(root, args, options = {}) {
  try {
    return await execFileAsync('git', args, {
      cwd: root,
      maxBuffer: 64 * 1024 * 1024,
      ...options,
    });
  } catch (error) {
    if (options.allowFailure) {
      return error;
    }
    throw error;
  }
}

async function repositoryRoot(ledgerArgument) {
  const ledgerDirectory = path.dirname(path.resolve(ledgerArgument));
  try {
    const {stdout} = await git(ledgerDirectory, ['rev-parse', '--show-toplevel']);
    return stdout.trim();
  } catch {
    fail('acceptance evidence generation requires a git worktree');
  }
}

function parseArguments(argv) {
  let sourceCommit;
  const positional = [];
  for (let index = 0; index < argv.length; index += 1) {
    const argument = argv[index];
    if (argument === '--source-commit') {
      sourceCommit = argv[++index];
    } else if (argument.startsWith('--')) {
      fail(`unsupported option: ${argument}`);
    } else {
      positional.push(argument);
    }
  }
  if (!commitPattern.test(sourceCommit ?? '')) {
    fail('--source-commit must be a full lowercase 40-character git commit');
  }
  if (positional.length !== 1) {
    fail('usage: control-plane-acceptance-evidence.mjs --source-commit <commit> <ledger>');
  }
  return {sourceCommit, ledgerArgument: positional[0]};
}

async function requireSourceCommit(root, sourceCommit) {
  try {
    await git(root, ['cat-file', '-e', `${sourceCommit}^{commit}`]);
    await git(root, ['merge-base', '--is-ancestor', sourceCommit, 'HEAD']);
  } catch {
    fail(`source commit must exist and be an ancestor of HEAD: ${sourceCommit}`);
  }
}

async function requireExecutionHead(root, sourceCommit) {
  const {stdout} = await git(root, ['rev-parse', 'HEAD']);
  if (stdout.trim() !== sourceCommit) {
    fail(`execution source commit must equal HEAD: selected ${sourceCommit}, HEAD ${stdout.trim()}`);
  }
}

function statusPath(entry) {
  return gitPath(entry.slice(3));
}

async function requireIsolatedWorktreeClean(root, sourceCommit, phase) {
  const {stdout: head} = await git(root, ['rev-parse', 'HEAD']);
  if (head.trim() !== sourceCommit) {
    fail(`isolated execution worktree HEAD changed ${phase}`);
  }
  const {stdout} = await git(root, ['status', '--porcelain=v1', '-z', '--untracked-files=all'], {
    encoding: 'buffer',
  });
  const first = stdout.toString('utf8').split('\0').find(Boolean);
  if (first) {
    fail(`isolated execution worktree is dirty ${phase}: ${statusPath(first)}`);
  }
}

async function createIsolatedWorktree(root, sourceCommit) {
  const base = await mkdtemp(path.join(tmpdir(), 'control-plane-acceptance-checkout-'));
  const checkout = path.join(base, 'source');
  let registered = false;
  try {
    await git(root, ['worktree', 'add', '--detach', '--quiet', checkout, sourceCommit]);
    registered = true;
    await requireIsolatedWorktreeClean(checkout, sourceCommit, 'before execution');
    return {base, checkout};
  } catch (error) {
    if (registered) {
      await git(root, ['worktree', 'remove', '--force', checkout], {allowFailure: true});
    }
    await rm(base, {recursive: true, force: true}).catch(() => {});
    await git(root, ['worktree', 'prune'], {allowFailure: true});
    throw error;
  }
}

async function removeIsolatedWorktree(root, worktree) {
  if (!worktree) return;
  const removal = await git(root, ['worktree', 'remove', '--force', worktree.checkout], {allowFailure: true});
  await rm(worktree.base, {recursive: true, force: true});
  await git(root, ['worktree', 'prune'], {allowFailure: true});
  if (removal instanceof Error) {
    throw new Error(`failed to remove isolated execution worktree: ${removal.message}`);
  }
}

async function requireSafeWorktree(root) {
  const {stdout} = await git(root, ['status', '--porcelain=v1', '-z', '--untracked-files=all'], {
    encoding: 'buffer',
  });
  for (const entry of stdout.toString('utf8').split('\0').filter(Boolean)) {
    const status = entry.slice(0, 2);
    const value = gitPath(entry.slice(3));
    if (status !== '??') {
      fail(`tracked worktree is dirty: ${value}`);
    }
    if (value.endsWith('.go')) {
      fail(`untracked Go source is not allowed before evidence execution: ${value}`);
    }
    if (!safeUntrackedPrefixes.some((prefix) => value.startsWith(prefix))) {
      fail(`untracked path is not allowed before evidence execution: ${value}`);
    }
  }
}

async function trackedPaths(root) {
  const {stdout} = await git(root, ['ls-files', '-z'], {encoding: 'buffer'});
  return new Set(stdout.toString('utf8').split('\0').filter(Boolean).map(gitPath));
}

function requireTracked(tracked, value, label) {
  const normalized = gitPath(value);
  if (!tracked.has(normalized)) {
    fail(`${label} must be tracked by git: ${value}`);
  }
}

async function committedBytes(root, sourceCommit, value, label) {
  try {
    const {stdout} = await git(root, ['show', `${sourceCommit}:${gitPath(value)}`], {encoding: 'buffer'});
    return stdout;
  } catch {
    fail(`${label} is absent from source commit: ${value}`);
  }
}

async function requireCleanSourcePath(root, tracked, sourceCommit, value) {
  requireTracked(tracked, value, 'relevant path');
  const {stdout} = await git(root, ['status', '--porcelain', '--', gitPath(value)]);
  if (stdout.trim()) {
    fail(`relevant path is dirty: ${gitPath(value)}`);
  }
  const current = await readFile(repositoryPath(root, value, 'relevant path'));
  const committed = await committedBytes(root, sourceCommit, value, 'relevant path');
  if (sha256(current) !== sha256(committed)) {
    fail(`relevant path drifted from source commit: ${gitPath(value)}`);
  }
  return {bytes: committed, checksum: sha256(committed)};
}

function selectedGoTests(profile) {
  const selected = profile.selectedTests;
  if (
    !Array.isArray(selected) ||
    selected.length === 0 ||
    new Set(selected).size !== selected.length ||
    selected.some((name) => typeof name !== 'string' || !/^Test(?:[A-Z0-9_][A-Za-z0-9_]*)$/.test(name))
  ) {
    fail(`${profile.automatedTest} selectedTests must contain unique Go test names`);
  }
  return [...selected];
}

function selectedGoTestPattern(selectedTests) {
  return `^(?:${selectedTests.map((name) => name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')).join('|')})$`;
}

async function requireSelectedGoTestDeclarations(profile, sourceBytes) {
  if (profile.testRunner !== 'go-test') {
    return;
  }
  const selectedTests = selectedGoTests(profile);
  const declarations = await declaredGoTests(sourceBytes, gitPath(profile.automatedTest));
  const missing = selectedTests.filter((name) => !declarations.includes(name));
  if (missing.length > 0) {
    fail(
      `${gitPath(
        profile.automatedTest
      )} selectedTests are not declared top-level Go tests in the bound source: ${missing.slice(0, 20).join(', ')}`
    );
  }
}

function commandFor(profile) {
  const selectedTestSource = gitPath(profile.automatedTest);
  if (profile.testRunner === 'node-test') {
    return {
      runner: 'node-test',
      argv: ['node', '--test', '--test-reporter=tap', selectedTestSource],
      selectedTestSource,
    };
  }
  if (profile.testRunner === 'go-test') {
    const selectedTests = selectedGoTests(profile);
    return {
      runner: 'go-test',
      argv: [
        'go',
        'test',
        `./${path.posix.dirname(selectedTestSource)}`,
        '-run',
        selectedGoTestPattern(selectedTests),
        '-count=1',
        '-json',
      ],
      selectedTestSource,
      selectedTests,
    };
  }
  fail(`community-focused-test does not support runner ${profile.testRunner}`);
}

function commandKey(command) {
  return sha256(JSON.stringify(command));
}

async function compiledGoPackageFacts(root, command, cache) {
  const packagePath = command.argv[2];
  if (!cache.has(packagePath)) {
    cache.set(
      packagePath,
      (async () => {
        let metadata;
        try {
          const {stdout} = await execFileAsync('go', ['list', '-json', packagePath], {
            cwd: root,
            env: controlledGoEnvironment(),
            maxBuffer: 16 * 1024 * 1024,
          });
          metadata = JSON.parse(stdout);
        } catch (error) {
          fail(`cannot inspect compiled Go package ${packagePath}: ${error.message}`);
        }
        const rootReal = await realpath(root);
        const directoryReal = await realpath(metadata.Dir);
        if (!isWithin(rootReal, directoryReal)) {
          fail(`compiled Go package ${packagePath} resolves outside the isolated source checkout`);
        }
        const names = [...(metadata.TestGoFiles ?? []), ...(metadata.XTestGoFiles ?? [])];
        const sources = [];
        for (const name of names) {
          const resolved = path.resolve(metadata.Dir, name);
          if (!isWithin(directoryReal, resolved)) {
            fail(`compiled Go test source escapes package ${packagePath}: ${name}`);
          }
          sources.push({
            path: gitPath(path.relative(root, resolved)),
            bytes: await readFile(resolved),
            rejectBuildConstraints: false,
          });
        }
        const declarations = await declaredGoTestsForSources(sources);
        const manifest = [];
        for (const source of sources) {
          const {stdout: committed} = await git(root, ['show', `HEAD:${source.path}`], {encoding: 'buffer'});
          manifest.push({path: source.path, sha256: sha256(committed)});
        }
        manifest.sort((left, right) => left.path.localeCompare(right.path));
        return {names: new Set(sources.map((source) => source.path)), declarations, manifest};
      })()
    );
  }
  return cache.get(packagePath);
}

async function requireUniqueCompiledGoDeclarations(root, command, cache) {
  if (command.runner !== 'go-test') return undefined;
  const facts = await compiledGoPackageFacts(root, command, cache);
  if (!facts.names.has(command.selectedTestSource)) {
    fail(`${command.selectedTestSource} is not compiled in selected package ${command.argv[2]}`);
  }
  for (const selected of command.selectedTests) {
    let declarations = 0;
    for (const tests of facts.declarations.values()) {
      declarations += tests.filter((name) => name === selected).length;
    }
    if (declarations !== 1) {
      fail(
        `${command.selectedTestSource} selected test ${selected} must have exactly one declaration in compiled package ${command.argv[2]}; found ${declarations}`
      );
    }
  }
  return facts.manifest;
}

function parseNodeCounts(output, selectedTestSource) {
  const count = (name) => {
    const matches = [...output.matchAll(new RegExp(`^# ${name} (\\d+)\\s*$`, 'gm'))];
    return matches.length ? Number(matches.at(-1)[1]) : 0;
  };
  const counts = {
    expected: count('tests'),
    passed: count('pass'),
    failed: count('fail'),
    skipped: count('skipped'),
  };
  const subtests = [...output.matchAll(/^# Subtest: (.+)\r?$/gm)].map((match) => gitPath(match[1].trim()));
  if (
    counts.expected === 1 &&
    subtests.length === 1 &&
    (subtests[0] === selectedTestSource || path.posix.basename(subtests[0]) === path.posix.basename(selectedTestSource))
  ) {
    return {expected: 0, passed: 0, failed: 0, skipped: 0};
  }
  return counts;
}

function parseGoCounts(output) {
  const tests = new Map();
  for (const line of output.split(/\r?\n/)) {
    if (!line.trim()) continue;
    let event;
    try {
      event = JSON.parse(line);
    } catch {
      continue;
    }
    if (typeof event.Test !== 'string' || event.Test === '') continue;
    if (event.Action === 'run' && !tests.has(event.Test)) tests.set(event.Test, 'run');
    if (['pass', 'fail', 'skip'].includes(event.Action)) tests.set(event.Test, event.Action);
  }
  const actions = [...tests.values()];
  const events = [...tests.entries()]
    .map(([name, status]) => ({name, status}))
    .sort((left, right) => left.name.localeCompare(right.name));
  return {
    tests: {
      expected: tests.size,
      passed: actions.filter((action) => action === 'pass').length,
      failed: actions.filter((action) => action === 'fail').length,
      skipped: actions.filter((action) => action === 'skip').length,
      topLevel: events.filter((event) => !event.name.includes('/')),
    },
    events,
  };
}

function runCommand(root, command) {
  return new Promise((resolve, reject) => {
    const startedAt = new Date().toISOString();
    const environment = command.runner === 'go-test' ? controlledGoEnvironment() : {...process.env};
    delete environment.NODE_TEST_CONTEXT;
    delete environment.NODE_OPTIONS;
    delete environment.NODE_PATH;
    const child = spawn(command.argv[0], command.argv.slice(1), {
      cwd: root,
      env: environment,
      shell: false,
      stdio: ['ignore', 'pipe', 'pipe'],
      windowsHide: true,
    });
    const output = [];
    child.stdout.on('data', (chunk) => output.push(chunk));
    child.stderr.on('data', (chunk) => output.push(chunk));
    child.on('error', reject);
    child.on('close', (exitCode) => {
      const completedAt = new Date().toISOString();
      const combined = Buffer.concat(output).toString('utf8');
      const parsed =
        command.runner === 'node-test'
          ? {tests: parseNodeCounts(combined, command.selectedTestSource), events: []}
          : parseGoCounts(combined);
      resolve({
        exitCode: exitCode ?? 1,
        tests: parsed.tests,
        testEvents: parsed.events,
        startedAt,
        completedAt,
      });
    });
  });
}

function boundedTestNames(events, status) {
  const names = events.filter((event) => event.status === status).map((event) => event.name);
  const retained = names.slice(0, 20);
  return `${retained.join(', ')}${names.length > retained.length ? ` (+${names.length - retained.length} more)` : ''}`;
}

function requirePassedCounts(execution, command) {
  if (execution.tests.expected === 0) {
    fail('community command returned zero tests');
  }
  if (command.runner === 'go-test') {
    const observedNames = execution.tests.topLevel.map((test) => test.name);
    const missing = command.selectedTests.filter((name) => !observedNames.includes(name));
    const extra = observedNames.filter((name) => !command.selectedTests.includes(name));
    const label = `selected source ${command.selectedTestSource} package ${command.argv[2]}`;
    if (missing.length > 0) {
      fail(`${label} did not observe declared tests: ${missing.slice(0, 20).join(', ')}`);
    }
    if (extra.length > 0) {
      fail(`${label} observed undeclared tests: ${extra.slice(0, 20).join(', ')}`);
    }
    if (execution.tests.skipped !== 0) {
      fail(`${label} skipped tests: ${boundedTestNames(execution.testEvents, 'skip')}`);
    }
    if (execution.exitCode !== 0 || execution.tests.failed !== 0 || execution.tests.passed < execution.tests.expected) {
      fail(`${label} failed tests: ${boundedTestNames(execution.testEvents, 'fail') || '<package>'}`);
    }
    return;
  }
  if (execution.tests.skipped !== 0) {
    fail('community command reported skipped tests');
  }
  if (execution.exitCode !== 0 || execution.tests.failed !== 0 || execution.tests.passed < execution.tests.expected) {
    fail(
      `community command failed: exit=${execution.exitCode}, passed=${execution.tests.passed}, failed=${execution.tests.failed}`
    );
  }
}

function renderPromotedLine(row, artifact) {
  return `| \`${row.id}\` | \`${row.owner}\` | \`${row.automatedTest}\` | \`${row.manualEvidence}\` | \`community-evidence-retained\` | \`${artifact}\` |`;
}

function promoteLedger(markdown, promotions) {
  const lines = markdown.split(/\r?\n/);
  for (const [id, promotion] of promotions) {
    const index = lines.findIndex((line) => line.trimStart().startsWith(`| \`${id}\` |`));
    if (index < 0) fail(`ledger row disappeared during generation: ${id}`);
    lines[index] = renderPromotedLine(promotion.row, promotion.artifact);
  }
  return lines.join('\n');
}

async function main() {
  const {sourceCommit, ledgerArgument} = parseArguments(process.argv.slice(2));
  const root = await repositoryRoot(ledgerArgument);
  const ledgerPath = gitPath(path.relative(root, path.resolve(ledgerArgument)));
  repositoryPath(root, ledgerPath, 'ledger');
  await requireSourceCommit(root, sourceCommit);
  await requireExecutionHead(root, sourceCommit);

  const tracked = await trackedPaths(root);
  const contract = JSON.parse(await readFile(repositoryPath(root, contractPath, 'contract'), 'utf8'));
  if (contract.schema !== contractSchema || !contract.profiles || !contract.acceptance) {
    fail(`contract must use ${contractSchema}`);
  }
  const ledgerBytes = await readFile(repositoryPath(root, ledgerPath, 'ledger'));
  const ledger = ledgerBytes.toString('utf8');
  const rows = parseAcceptanceLedger(ledger);
  const eligible = [];
  for (const row of rows) {
    const rule = contract.acceptance[row.id];
    const profile = rule && contract.profiles[rule.profile];
    if (
      row.status !== 'pending-community-evidence' ||
      !rule ||
      rule.pendingAdopter !== false ||
      !profile ||
      profile.allowedProofClasses?.length !== 1 ||
      profile.allowedProofClasses[0] !== 'community-focused-test'
    ) {
      continue;
    }
    if (
      row.owner !== rule.owner ||
      row.automatedTest !== profile.automatedTest ||
      row.manualEvidence !== profile.manualEvidence
    ) {
      fail(`${row.id} does not match its acceptance contract`);
    }
    eligible.push({row, rule, profile, command: commandFor(profile)});
  }
  if (eligible.length === 0) {
    fail('no pending community-focused-test rows are eligible');
  }

  const relevantPaths = new Set([contractPath, ledgerPath]);
  for (const item of eligible) {
    relevantPaths.add(gitPath(item.row.automatedTest));
    relevantPaths.add(gitPath(item.row.manualEvidence));
  }
  const sourceBindings = new Map();
  for (const value of [...relevantPaths].sort()) {
    sourceBindings.set(value, await requireCleanSourcePath(root, tracked, sourceCommit, value));
  }
  await requireSafeWorktree(root);
  const goSources = new Map();
  for (const item of eligible) {
    if (item.profile.testRunner === 'go-test') {
      const sourcePath = gitPath(item.row.automatedTest);
      goSources.set(sourcePath, sourceBindings.get(sourcePath).bytes);
    }
  }
  await declaredGoTestsForSources([...goSources].map(([sourcePath, bytes]) => ({path: sourcePath, bytes})));
  for (const item of eligible) {
    await requireSelectedGoTestDeclarations(item.profile, sourceBindings.get(gitPath(item.row.automatedTest)).bytes);
  }

  const groups = new Map();
  for (const item of eligible) {
    const key = commandKey(item.command);
    if (!groups.has(key)) groups.set(key, {key, command: item.command, items: []});
    groups.get(key).items.push(item);
  }
  let isolatedWorktree;
  try {
    isolatedWorktree = await createIsolatedWorktree(root, sourceCommit);
    const packageCache = new Map();
    for (const group of [...groups.values()].sort((left, right) => left.key.localeCompare(right.key))) {
      group.compiledPackageSources = await requireUniqueCompiledGoDeclarations(
        isolatedWorktree.checkout,
        group.command,
        packageCache
      );
    }
    await requireIsolatedWorktreeClean(isolatedWorktree.checkout, sourceCommit, 'after package inspection');

    const generatedResults = [];
    for (const group of [...groups.values()].sort((left, right) => left.key.localeCompare(right.key))) {
      const execution = await runCommand(isolatedWorktree.checkout, group.command);
      await requireIsolatedWorktreeClean(isolatedWorktree.checkout, sourceCommit, 'after command');
      requirePassedCounts(execution, group.command);
      const result = {
        schema: resultSchema,
        sourceCommit,
        command: group.command,
        exitCode: execution.exitCode,
        tests: execution.tests,
        status: 'passed',
        startedAt: execution.startedAt,
        completedAt: execution.completedAt,
        ...(group.compiledPackageSources ? {compiledPackageSources: group.compiledPackageSources} : {}),
      };
      const bytes = Buffer.from(stableJSON(result));
      const resultPath = `${evidenceDirectory}/results/${group.key.slice('sha256:'.length)}.json`;
      group.result = {path: resultPath, bytes, value: result};
      generatedResults.push(group.result);
    }

    const promotions = new Map();
    const artifacts = [];
    for (const group of [...groups.values()].sort((left, right) => left.key.localeCompare(right.key))) {
      for (const item of group.items.sort((left, right) => left.row.id.localeCompare(right.row.id))) {
        const artifactPath = `${evidenceDirectory}/${item.row.id}.json`;
        const artifact = {
          schema: evidenceSchema,
          acceptanceId: item.row.id,
          owner: item.row.owner,
          proofClass: 'community-focused-test',
          sourceCommit,
          automatedTest: {
            path: gitPath(item.row.automatedTest),
            sha256: sourceBindings.get(gitPath(item.row.automatedTest)).checksum,
          },
          manualEvidence: {
            path: gitPath(item.row.manualEvidence),
            sha256: sourceBindings.get(gitPath(item.row.manualEvidence)).checksum,
          },
          testResult: {
            path: group.result.path,
            sha256: sha256(group.result.bytes),
          },
        };
        const bytes = Buffer.from(stableJSON(artifact));
        artifacts.push({path: artifactPath, bytes});
        promotions.set(item.row.id, {
          row: item.row,
          artifact: `${artifactPath} @ ${sha256(bytes)}`,
        });
      }
    }

    await requireIsolatedWorktreeClean(isolatedWorktree.checkout, sourceCommit, 'before output');
    await requireExecutionHead(root, sourceCommit);
    await requireSafeWorktree(root);
    for (const output of [...generatedResults, ...artifacts]) {
      await prepareSafeOutputPath(root, output.path, 'generated evidence', {immutable: true});
    }
    await prepareSafeOutputPath(root, ledgerPath, 'ledger');
    for (const output of [...generatedResults, ...artifacts]) {
      await publishImmutableFile(root, output.path, output.bytes, 'generated evidence');
    }
    await atomicWriteFile(root, ledgerPath, Buffer.from(promoteLedger(ledger, promotions)), 'ledger');
    const pathsToStage = [
      ledgerPath,
      ...generatedResults.map((item) => item.path),
      ...artifacts.map((item) => item.path),
    ];
    await git(root, ['add', '--', ...pathsToStage]);

    const commandWord = groups.size === 1 ? 'command' : 'commands';
    process.stdout.write(
      `Generated ${promotions.size} community evidence rows from ${groups.size} executed ${commandWord}\n`
    );
  } finally {
    await removeIsolatedWorktree(root, isolatedWorktree);
  }
}

main().catch((error) => {
  process.stderr.write(`${error.message}\n`);
  process.exitCode = 1;
});
