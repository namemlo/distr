#!/usr/bin/env node

import {execFile, spawn} from 'node:child_process';
import {createHash} from 'node:crypto';
import {mkdir, readFile, writeFile} from 'node:fs/promises';
import path from 'node:path';
import {promisify} from 'node:util';
import {parseAcceptanceLedger} from './control-plane-acceptance-check.mjs';

const execFileAsync = promisify(execFile);
const contractPath = 'docs/release/control-plane-acceptance-contract.json';
const evidenceDirectory = 'docs/release/evidence';
const contractSchema = 'distr.control-plane-acceptance-contract/v1';
const resultSchema = 'distr.control-plane-test-result/v1';
const evidenceSchema = 'distr.control-plane-acceptance-evidence/v1';
const commitPattern = /^[0-9a-f]{40}$/;

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

async function requireCleanTrackedWorktree(root) {
  const {stdout} = await git(root, ['status', '--porcelain', '--untracked-files=no']);
  const first = stdout.split(/\r?\n/).find(Boolean);
  if (first) {
    const dirtyPath = first.slice(3).trim().split(' -> ').at(-1);
    fail(`tracked worktree is dirty: ${gitPath(dirtyPath)}`);
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
    return {
      runner: 'go-test',
      argv: ['go', 'test', `./${path.posix.dirname(selectedTestSource)}`, '-count=1', '-json'],
      selectedTestSource,
    };
  }
  fail(`community-focused-test does not support runner ${profile.testRunner}`);
}

function commandKey(command) {
  return sha256(JSON.stringify(command));
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
  return {
    expected: tests.size,
    passed: actions.filter((action) => action === 'pass').length,
    failed: actions.filter((action) => action === 'fail').length,
    skipped: actions.filter((action) => action === 'skip').length,
  };
}

function runCommand(root, command) {
  return new Promise((resolve, reject) => {
    const startedAt = new Date().toISOString();
    const environment = {...process.env};
    delete environment.NODE_TEST_CONTEXT;
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
      const tests =
        command.runner === 'node-test'
          ? parseNodeCounts(combined, command.selectedTestSource)
          : parseGoCounts(combined);
      resolve({exitCode: exitCode ?? 1, tests, startedAt, completedAt, output: combined});
    });
  });
}

function requirePassedCounts(execution) {
  if (execution.tests.expected === 0) {
    fail('community command returned zero tests');
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
  await requireCleanTrackedWorktree(root);

  const groups = new Map();
  for (const item of eligible) {
    const key = commandKey(item.command);
    if (!groups.has(key)) groups.set(key, {key, command: item.command, items: []});
    groups.get(key).items.push(item);
  }
  const generatedResults = [];
  for (const group of [...groups.values()].sort((left, right) => left.key.localeCompare(right.key))) {
    const execution = await runCommand(root, group.command);
    requirePassedCounts(execution);
    const result = {
      schema: resultSchema,
      sourceCommit,
      command: group.command,
      exitCode: execution.exitCode,
      tests: execution.tests,
      status: 'passed',
      startedAt: execution.startedAt,
      completedAt: execution.completedAt,
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

  for (const output of [...generatedResults, ...artifacts]) {
    const resolved = repositoryPath(root, output.path, 'generated evidence');
    await mkdir(path.dirname(resolved), {recursive: true});
    await writeFile(resolved, output.bytes);
  }
  await writeFile(repositoryPath(root, ledgerPath, 'ledger'), promoteLedger(ledger, promotions));
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
}

main().catch((error) => {
  process.stderr.write(`${error.message}\n`);
  process.exitCode = 1;
});
