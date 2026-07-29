#!/usr/bin/env node

import {execFile, spawn} from 'node:child_process';
import {createHash} from 'node:crypto';
import {lstat, mkdtemp, readFile, realpath, rm, stat} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import {promisify} from 'node:util';
import {inflateSync} from 'node:zlib';

const execFileAsync = promisify(execFile);
const goTestDeclarationsHelper = fileURLToPath(
  new URL('./control-plane-go-test-declarations/main.go', import.meta.url)
);
const goTestDeclarationCache = new Map();
const expectedHeader = [
  'Acceptance ID',
  'Owning PR',
  'Automated test',
  'Manual/fixture evidence',
  'Status',
  'Artifact/checksum',
];
const expectedIDs = Array.from({length: 80}, (_, index) => `AC-${String(index + 1).padStart(2, '0')}`);
const expectedContractPath = 'docs/release/control-plane-acceptance-contract.json';
const testPathPattern = /(?:_test\.go|\.test\.mjs|\.spec\.ts)$/;
const checksumPattern = /^sha256:[0-9a-f]{64}$/;
const commitPattern = /^[0-9a-f]{40}$/;
const contractSchema = 'distr.control-plane-acceptance-contract/v1';
const evidenceSchema = 'distr.control-plane-acceptance-evidence/v1';
const testResultSchema = 'distr.control-plane-test-result/v1';
const performanceResultSchema = 'distr.control-plane-performance-result/v1';
const neutralLiveResultSchema = 'distr.control-plane-neutral-live-result/v1';
const browserResultSchema = 'distr.control-plane-browser-e2e-result/v1';
const browserAcceptanceId = 'AC-63';
const browserAutomatedTest = 'frontend/ui/e2e/control-plane.spec.ts';
const browserConfig = 'playwright.control-plane-evidence.config.ts';
const browserFixture = 'frontend/ui/e2e/fixtures/control-plane.ts';
const browserProject = 'chromium';
const browserTitle = '@evidence proves the reference client DEV release, approval, and previous-state journey';
const browserGrep = `${browserTitle.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}$`;
const browserCommand = [
  'pnpm',
  'exec',
  'playwright',
  'test',
  '--config',
  browserConfig,
  '--project',
  browserProject,
  browserAutomatedTest,
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
];
const pngSignature = Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]);
const adopterBundleSchema = 'distr.control-plane-adopter-execution-bundle/v1';
const adopterAuditSchema = 'distr.control-plane-adopter-audit-export/v1';
const supportedTestRunners = new Set(['node-test', 'go-test', 'playwright']);
const supportedProofClasses = new Set([
  'community-focused-test',
  'performance-measurement',
  'neutral-live-execution',
  'browser-e2e',
  'adopter-execution',
]);

export function controlledGoEnvironment() {
  return {
    ...process.env,
    GOENV: 'off',
    GOFLAGS: '-mod=readonly',
    GOWORK: 'off',
  };
}

function fail(message) {
  throw new Error(message);
}

function sha256(value) {
  return `sha256:${createHash('sha256').update(value).digest('hex')}`;
}

function runGoTestDeclarationHelper(sources) {
  const sourcePaths = sources.map((source) => source.path);
  return new Promise((resolve, reject) => {
    const child = spawn('go', ['run', goTestDeclarationsHelper], {
      env: controlledGoEnvironment(),
      shell: false,
      stdio: ['pipe', 'pipe', 'pipe'],
      windowsHide: true,
    });
    const stdout = [];
    const stderr = [];
    let retainedBytes = 0;
    const retain = (target) => (chunk) => {
      retainedBytes += chunk.length;
      if (retainedBytes > 4 * 1024 * 1024) {
        child.kill();
        reject(new Error('Go AST declaration helper output exceeded its limit'));
        return;
      }
      target.push(chunk);
    };
    child.stdout.on('data', retain(stdout));
    child.stderr.on('data', retain(stderr));
    child.on('error', reject);
    child.on('close', (status) => {
      if (status !== 0) {
        reject(
          new Error(
            `Go AST declaration helper failed for ${sourcePaths.join(', ')}: ${
              Buffer.concat(stderr).toString('utf8').trim() || `exit ${status}`
            }`
          )
        );
        return;
      }
      try {
        const parsed = JSON.parse(Buffer.concat(stdout).toString('utf8'));
        if (!Array.isArray(parsed?.sources) || parsed.sources.length !== sources.length) {
          throw new Error('unexpected response shape');
        }
        const results = new Map();
        for (const source of parsed.sources) {
          if (
            !sourcePaths.includes(source?.path) ||
            results.has(source.path) ||
            !Array.isArray(source.tests) ||
            new Set(source.tests).size !== source.tests.length ||
            source.tests.some((name) => typeof name !== 'string')
          ) {
            throw new Error('unexpected source declaration result');
          }
          results.set(source.path, source.tests);
        }
        resolve(results);
      } catch (error) {
        reject(new Error(`Go AST declaration helper returned invalid JSON: ${error.message}`));
      }
    });
    child.stdin.end(
      JSON.stringify({
        sources: sources.map((source) => ({
          path: source.path,
          contentBase64: Buffer.from(source.bytes).toString('base64'),
          rejectBuildConstraints: source.rejectBuildConstraints !== false,
        })),
      })
    );
  });
}

export async function declaredGoTestsForSources(sources) {
  if (!Array.isArray(sources) || sources.length === 0) {
    return new Map();
  }
  const paths = new Set();
  const missingByChecksum = new Map();
  for (const source of sources) {
    if (
      !source ||
      typeof source.path !== 'string' ||
      source.path === '' ||
      !Buffer.isBuffer(source.bytes) ||
      paths.has(source.path)
    ) {
      fail('Go AST declaration sources must contain unique paths and Buffer content');
    }
    paths.add(source.path);
    const cacheKey = `${source.rejectBuildConstraints !== false}:${sha256(source.bytes)}`;
    if (!goTestDeclarationCache.has(cacheKey) && !missingByChecksum.has(cacheKey)) {
      missingByChecksum.set(cacheKey, source);
    }
  }
  const missing = [...missingByChecksum.entries()];
  if (missing.length > 0) {
    const batch = runGoTestDeclarationHelper(missing.map(([, source]) => source));
    for (const [cacheKey, source] of missing) {
      goTestDeclarationCache.set(
        cacheKey,
        batch.then((results) => results.get(source.path))
      );
    }
  }
  const results = new Map();
  for (const source of sources) {
    const cacheKey = `${source.rejectBuildConstraints !== false}:${sha256(source.bytes)}`;
    results.set(source.path, await goTestDeclarationCache.get(cacheKey));
  }
  return results;
}

export async function declaredGoTests(sourceBytes, sourcePath) {
  const results = await declaredGoTestsForSources([{path: sourcePath, bytes: Buffer.from(sourceBytes)}]);
  return results.get(sourcePath);
}

function cleanCell(value) {
  const trimmed = value.trim();
  if (trimmed.startsWith('`') && trimmed.endsWith('`')) {
    return trimmed.slice(1, -1).trim();
  }
  return trimmed;
}

function tableCells(line) {
  const trimmed = line.trim();
  if (!trimmed.startsWith('|') || !trimmed.endsWith('|')) {
    return undefined;
  }
  return trimmed.slice(1, -1).split('|').map(cleanCell);
}

function isSeparator(cells) {
  return cells?.length === expectedHeader.length && cells.every((cell) => /^:?-{3,}:?$/.test(cell));
}

export function parseAcceptanceLedger(markdown) {
  const lines = markdown.split(/\r?\n/);
  let headerIndex = -1;
  for (let index = 0; index < lines.length; index += 1) {
    const cells = tableCells(lines[index]);
    if (
      cells?.length === expectedHeader.length &&
      cells.every((cell, cellIndex) => cell === expectedHeader[cellIndex])
    ) {
      headerIndex = index;
      break;
    }
  }
  if (headerIndex === -1) {
    fail(`acceptance table header must be: ${expectedHeader.join(' | ')}`);
  }
  if (!isSeparator(tableCells(lines[headerIndex + 1] ?? ''))) {
    fail('acceptance table must include a Markdown separator row');
  }
  const rows = [];
  for (let index = headerIndex + 2; index < lines.length; index += 1) {
    const cells = tableCells(lines[index]);
    if (!cells) {
      break;
    }
    if (cells.length !== expectedHeader.length) {
      fail(`acceptance table row ${index + 1} must contain exactly ${expectedHeader.length} columns`);
    }
    const [id, owner, automatedTest, manualEvidence, status, artifact] = cells;
    rows.push({id, owner, automatedTest, manualEvidence, status, artifact});
  }
  return rows;
}

function repositoryPath(root, value, id, label) {
  if (!value || path.isAbsolute(value)) {
    fail(`${id} ${label} must be a repository-relative path`);
  }
  const resolved = path.resolve(root, value);
  const relative = path.relative(root, resolved);
  if (relative === '' || relative.startsWith(`..${path.sep}`) || relative === '..' || path.isAbsolute(relative)) {
    fail(`${id} ${label} must be a repository-relative path`);
  }
  return resolved;
}

function gitPath(value) {
  return value.split(path.sep).join('/');
}

function pathIsWithin(root, candidate) {
  const relative = path.relative(root, candidate);
  return relative === '' || (!path.isAbsolute(relative) && relative !== '..' && !relative.startsWith(`..${path.sep}`));
}

async function requireFile(root, value, id, label) {
  const resolved = repositoryPath(root, value, id, label);
  let facts;
  try {
    facts = await stat(resolved);
  } catch {
    fail(`${id} ${label} does not exist: ${value}`);
  }
  if (!facts.isFile()) {
    fail(`${id} ${label} is not a file: ${value}`);
  }
  return resolved;
}

async function parseJSONFile(root, value, id, label) {
  const resolved = await requireFile(root, value, id, label);
  let bytes;
  try {
    bytes = await readFile(resolved);
    return {resolved, bytes, value: JSON.parse(bytes.toString('utf8'))};
  } catch (error) {
    fail(`${id} ${label} must be valid JSON: ${error.message}`);
  }
}

async function loadGitFacts(root) {
  let stdout;
  try {
    ({stdout} = await execFileAsync('git', ['ls-files', '-z'], {
      cwd: root,
      encoding: 'buffer',
      maxBuffer: 16 * 1024 * 1024,
    }));
  } catch {
    fail('acceptance evidence requires a git worktree');
  }
  const tracked = new Set(
    stdout
      .toString('utf8')
      .split('\0')
      .filter(Boolean)
      .map((value) => gitPath(value))
  );
  return {tracked, commitChecks: new Map(), sourceBlobs: new Map(), compiledGoPackages: new Map()};
}

function requireTracked(gitFacts, value, id, label) {
  const normalized = gitPath(value);
  if (!gitFacts.tracked.has(normalized)) {
    fail(`${id} ${label} must be tracked by git: ${value}`);
  }
}

async function requireSourceCommit(root, gitFacts, commit, id) {
  if (!commitPattern.test(commit)) {
    fail(`${id} sourceCommit must be a full lowercase 40-character git commit`);
  }
  if (!gitFacts.commitChecks.has(commit)) {
    const check = (async () => {
      try {
        await execFileAsync('git', ['cat-file', '-e', `${commit}^{commit}`], {cwd: root});
        await execFileAsync('git', ['merge-base', '--is-ancestor', commit, 'HEAD'], {cwd: root});
      } catch {
        fail(`${id} sourceCommit must exist and be an ancestor of HEAD: ${commit}`);
      }
    })();
    gitFacts.commitChecks.set(commit, check);
  }
  await gitFacts.commitChecks.get(commit);
}

async function sourceBlob(root, gitFacts, commit, value, id, label) {
  const normalized = gitPath(value);
  const key = `${commit}:${normalized}`;
  if (!gitFacts.sourceBlobs.has(key)) {
    const load = (async () => {
      try {
        const {stdout} = await execFileAsync('git', ['show', `${commit}:${normalized}`], {
          cwd: root,
          encoding: 'buffer',
          maxBuffer: 64 * 1024 * 1024,
        });
        return stdout;
      } catch {
        fail(`${id} ${label} is absent from sourceCommit ${commit}: ${value}`);
      }
    })();
    gitFacts.sourceBlobs.set(key, load);
  }
  return gitFacts.sourceBlobs.get(key);
}

function requireString(value, id, label) {
  if (typeof value !== 'string' || value.trim() === '') {
    fail(`${id} ${label} must be a non-empty string`);
  }
}

function requireStringArray(value, id, label) {
  if (!Array.isArray(value) || value.length === 0 || value.some((item) => typeof item !== 'string' || !item.trim())) {
    fail(`${id} verified-adopter evidence must include non-empty ${label}`);
  }
}

function requireTimestamp(value, id, label) {
  requireString(value, id, label);
  const parsed = Date.parse(value);
  if (!Number.isFinite(parsed)) {
    fail(`${id} ${label} must be an ISO-8601 timestamp`);
  }
  return parsed;
}

function selectedGoTests(profile, id) {
  const selected = profile.selectedTests;
  if (
    !Array.isArray(selected) ||
    selected.length === 0 ||
    new Set(selected).size !== selected.length ||
    selected.some((name) => typeof name !== 'string' || !/^Test(?:[A-Z0-9_][A-Za-z0-9_]*)$/.test(name))
  ) {
    fail(`${id} contract selectedTests must contain unique Go test names`);
  }
  return selected;
}

function selectedGoTestPattern(selectedTests) {
  return `^(?:${selectedTests.map((name) => name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')).join('|')})$`;
}

async function loadAcceptanceContract(root, gitFacts) {
  const contractFile = await parseJSONFile(root, expectedContractPath, 'contract', 'manifest');
  const contract = contractFile.value;
  if (contract.schema !== contractSchema) {
    fail(`contract schema must be ${contractSchema}`);
  }
  requireString(contract.normativeSource, 'contract', 'normativeSource');
  await requireFile(root, contract.normativeSource, 'contract', 'normativeSource');
  requireTracked(gitFacts, contract.normativeSource, 'contract', 'normativeSource');
  if (!contract.profiles || typeof contract.profiles !== 'object' || Array.isArray(contract.profiles)) {
    fail('contract profiles must be an object');
  }
  if (!contract.acceptance || typeof contract.acceptance !== 'object' || Array.isArray(contract.acceptance)) {
    fail('contract acceptance must be an object');
  }
  const goProfiles = new Map();
  const contractIDs = Object.keys(contract.acceptance);
  for (const id of expectedIDs) {
    if (!Object.hasOwn(contract.acceptance, id)) {
      fail(`contract missing acceptance ID ${id}`);
    }
  }
  for (const id of contractIDs) {
    if (!expectedIDs.includes(id)) {
      fail(`contract has unexpected acceptance ID ${id}`);
    }
    const rule = contract.acceptance[id];
    requireString(rule?.owner, id, 'contract owner');
    requireString(rule?.profile, id, 'contract profile');
    const profile = contract.profiles[rule.profile];
    if (!profile) {
      fail(`${id} contract profile does not exist: ${rule.profile}`);
    }
    if (!testPathPattern.test(profile.automatedTest ?? '')) {
      fail(`${id} contract automated test must reference a *_test.go, *.test.mjs, or *.spec.ts file`);
    }
    requireString(profile.manualEvidence, id, 'contract manualEvidence');
    if (
      !Array.isArray(profile.allowedProofClasses) ||
      profile.allowedProofClasses.length === 0 ||
      profile.allowedProofClasses.some((item) => typeof item !== 'string' || !item.trim())
    ) {
      fail(`${id} contract allowedProofClasses must contain at least one proof class`);
    }
    for (const proofClass of profile.allowedProofClasses) {
      if (!supportedProofClasses.has(proofClass)) {
        fail(`${id} contract proof class is unsupported: ${proofClass}`);
      }
    }
    if (
      profile.allowedProofClasses.includes('performance-measurement') &&
      (typeof profile.proofRequirements?.performanceScenario !== 'string' ||
        profile.proofRequirements.performanceScenario.trim() === '')
    ) {
      fail(`${id} performance profile must declare proofRequirements.performanceScenario`);
    }
    if (profile.allowedProofClasses.includes('performance-measurement')) {
      const metrics = profile.proofRequirements?.performanceMetrics;
      if (!Array.isArray(metrics) || metrics.length === 0) {
        fail(`${id} performance profile must declare performanceMetrics`);
      }
      const names = new Set();
      for (const metric of metrics) {
        if (
          typeof metric?.name !== 'string' ||
          metric.name === '' ||
          names.has(metric.name) ||
          !['p95', 'p99', 'max', 'sum'].includes(metric.aggregation) ||
          !['lte', 'lt', 'eq'].includes(metric.operator) ||
          !Number.isFinite(metric.limit) ||
          typeof metric.unit !== 'string' ||
          metric.unit === '' ||
          !Number.isInteger(metric.minSamples) ||
          metric.minSamples <= 0
        ) {
          fail(`${id} performanceMetrics must have unique names and valid aggregation, threshold, unit, and samples`);
        }
        names.add(metric.name);
      }
      const facts = profile.proofRequirements?.performanceFacts;
      if (
        !facts ||
        typeof facts.exact !== 'object' ||
        facts.exact === null ||
        Array.isArray(facts.exact) ||
        typeof facts.minimum !== 'object' ||
        facts.minimum === null ||
        Array.isArray(facts.minimum) ||
        Object.keys(facts.exact).some((key) => Object.hasOwn(facts.minimum, key)) ||
        Object.values(facts.minimum).some((value) => !Number.isFinite(value))
      ) {
        fail(`${id} performance profile must declare non-overlapping exact and minimum performanceFacts`);
      }
    }
    if (!supportedTestRunners.has(profile.testRunner)) {
      fail(`${id} contract testRunner must be node-test, go-test, or playwright`);
    }
    if (profile.testRunner === 'go-test') {
      selectedGoTests(profile, id);
      if (!goProfiles.has(rule.profile)) {
        goProfiles.set(rule.profile, {id, profile});
      }
    } else if (Object.hasOwn(profile, 'selectedTests')) {
      fail(`${id} contract selectedTests is supported only for go-test profiles`);
    }
    if (typeof rule.pendingAdopter !== 'boolean') {
      fail(`${id} contract pendingAdopter must be boolean`);
    }
  }
  const goSources = new Map();
  for (const {id, profile} of goProfiles.values()) {
    if (!goSources.has(profile.automatedTest)) {
      const resolved = await requireFile(root, profile.automatedTest, id, 'automated test');
      requireTracked(gitFacts, profile.automatedTest, id, 'automated test');
      goSources.set(profile.automatedTest, await readFile(resolved));
    }
  }
  const declarationsByPath = await declaredGoTestsForSources(
    [...goSources].map(([sourcePath, bytes]) => ({path: sourcePath, bytes}))
  );
  for (const {id, profile} of goProfiles.values()) {
    const missing = selectedGoTests(profile, id).filter(
      (name) => !declarationsByPath.get(profile.automatedTest).includes(name)
    );
    if (missing.length > 0) {
      fail(
        `${id} selectedTests are not declared top-level Go tests in bound ${profile.automatedTest}: ${missing
          .slice(0, 20)
          .join(', ')}`
      );
    }
  }
  return contract;
}

function validateRowContract(row, rule, profile) {
  if (row.owner !== rule.owner) {
    fail(`${row.id} owner must be ${rule.owner}`);
  }
  if (row.automatedTest !== profile.automatedTest) {
    fail(`${row.id} automated test must be ${profile.automatedTest}`);
  }
  if (row.manualEvidence !== profile.manualEvidence) {
    fail(`${row.id} manual/fixture evidence must be ${profile.manualEvidence}`);
  }
}

function validateStatus(row, rule) {
  if (row.status === 'pending-adopter') {
    if (!rule.pendingAdopter) {
      fail(`${row.id} may not use status pending-adopter`);
    }
    if (row.artifact !== `pending-adopter:${rule.owner}`) {
      fail(`${row.id} pending artifact must be pending-adopter:${rule.owner}`);
    }
    return;
  }
  if (row.status === 'verified-adopter') {
    if (!rule.pendingAdopter) {
      fail(`${row.id} may not use status verified-adopter`);
    }
    return;
  }
  if (row.status !== 'community-evidence-retained') {
    fail(`${row.id} has unsupported status ${row.status || '<empty>'}`);
  }
  if (rule.pendingAdopter) {
    fail(`${row.id} must remain pending-adopter until adopter-specific execution evidence is retained`);
  }
}

async function validateBoundFile(root, gitFacts, row, sourceCommit, binding, expectedPath, label) {
  if (!binding || binding.path !== expectedPath || !checksumPattern.test(binding.sha256 ?? '')) {
    fail(`${row.id} ${label} binding must contain the exact contract path and a SHA-256 checksum`);
  }
  const resolved = await requireFile(root, expectedPath, row.id, label);
  requireTracked(gitFacts, expectedPath, row.id, label);
  const currentBytes = await readFile(resolved);
  if (sha256(currentBytes) !== binding.sha256) {
    fail(`${row.id} ${label} checksum mismatch in the current worktree`);
  }
  const committedBytes = await sourceBlob(root, gitFacts, sourceCommit, expectedPath, row.id, label);
  if (sha256(committedBytes) !== binding.sha256) {
    fail(`${row.id} ${label} checksum mismatch at source commit`);
  }
  return committedBytes;
}

async function validateSelectedGoTestDeclarations(row, profile, sourceBytes) {
  if (profile.testRunner !== 'go-test') {
    return;
  }
  const selectedTests = selectedGoTests(profile, row.id);
  const declarations = await declaredGoTests(sourceBytes, row.automatedTest);
  const missing = selectedTests.filter((name) => !declarations.includes(name));
  if (missing.length > 0) {
    fail(
      `${row.id} selectedTests are not declared top-level Go tests in bound ${row.automatedTest}: ${missing
        .slice(0, 20)
        .join(', ')}`
    );
  }
}

function validateTestCommand(row, profile, result) {
  const command = result.command;
  if (!command || typeof command !== 'object' || Array.isArray(command)) {
    fail(`${row.id} test result command must be a structured object`);
  }
  if (command.runner !== profile.testRunner) {
    fail(`${row.id} test result runner must be ${profile.testRunner}`);
  }
  if (command.selectedTestSource !== row.automatedTest) {
    fail(`${row.id} test result selectedTestSource must be ${row.automatedTest}`);
  }
  if (
    !Array.isArray(command.argv) ||
    command.argv.length < 3 ||
    command.argv.some((item) => typeof item !== 'string')
  ) {
    fail(`${row.id} test result argv must be a non-empty string array`);
  }
  const normalizedSource = gitPath(row.automatedTest);
  if (profile.testRunner === 'node-test') {
    if (!/^node(?:\.exe)?$/i.test(path.basename(command.argv[0])) || !command.argv.includes('--test')) {
      fail(`${row.id} node-test result must execute node --test`);
    }
    if (!command.argv.map(gitPath).includes(normalizedSource)) {
      fail(`${row.id} node-test argv must include ${row.automatedTest}`);
    }
  } else if (profile.testRunner === 'go-test') {
    const packagePath = `./${path.posix.dirname(normalizedSource)}`;
    const selectedTests = selectedGoTests(profile, row.id);
    const expectedArgv = ['go', 'test', packagePath, '-run', selectedGoTestPattern(selectedTests), '-count=1', '-json'];
    if (
      !Array.isArray(command.selectedTests) ||
      command.selectedTests.length !== selectedTests.length ||
      command.selectedTests.some((name, index) => name !== selectedTests[index]) ||
      command.argv.length !== expectedArgv.length ||
      command.argv.some((argument, index) => argument !== expectedArgv[index])
    ) {
      fail(`${row.id} go-test argv must exactly select the declared tests from ${row.automatedTest}`);
    }
  } else {
    if (
      row.id === browserAcceptanceId &&
      (command.argv.length !== browserCommand.length ||
        command.argv.some((argument, index) => argument !== browserCommand[index]))
    ) {
      fail(`${row.id} playwright argv must exactly run the purpose-built browser evidence test`);
    }
    const playwrightIndex = command.argv.indexOf('playwright');
    if (
      playwrightIndex < 0 ||
      command.argv[playwrightIndex + 1] !== 'test' ||
      !command.argv.map(gitPath).includes(normalizedSource)
    ) {
      fail(`${row.id} playwright argv must execute ${row.automatedTest}`);
    }
  }
  if (result.exitCode !== 0) {
    fail(`${row.id} test result exitCode must be zero`);
  }
  const tests = result.tests;
  if (
    !tests ||
    !Number.isInteger(tests.expected) ||
    tests.expected <= 0 ||
    !Number.isInteger(tests.passed) ||
    tests.passed !== tests.expected ||
    !Number.isInteger(tests.failed) ||
    tests.failed !== 0 ||
    !Number.isInteger(tests.skipped) ||
    tests.skipped !== 0
  ) {
    if (row.id === browserAcceptanceId) {
      fail(`${row.id} generic and raw browser result counts and timestamps must exactly match`);
    }
    fail(`${row.id} test result counts must show expected tests greater than zero and zero failures`);
  }
  if (profile.testRunner === 'go-test') {
    const selectedTests = selectedGoTests(profile, row.id);
    if (
      !Array.isArray(tests.topLevel) ||
      tests.topLevel.length !== selectedTests.length ||
      new Set(tests.topLevel.map((test) => test?.name)).size !== tests.topLevel.length ||
      tests.topLevel.some(
        (test) => !test || typeof test.name !== 'string' || test.status !== 'pass' || !selectedTests.includes(test.name)
      ) ||
      selectedTests.some((name) => !tests.topLevel.some((test) => test.name === name))
    ) {
      fail(`${row.id} observed top-level Go tests must exactly match selectedTests and pass`);
    }
  }
}

async function gitWorktreeCommand(root, args, {allowFailure = false, encoding = 'utf8'} = {}) {
  try {
    return await execFileAsync('git', args, {
      cwd: root,
      encoding,
      maxBuffer: 16 * 1024 * 1024,
    });
  } catch (error) {
    if (allowFailure) return error;
    throw error;
  }
}

async function requireCleanDetachedCheckout(checkout, sourceCommit, phase) {
  const {stdout: head} = await gitWorktreeCommand(checkout, ['rev-parse', 'HEAD']);
  if (head.trim() !== sourceCommit) {
    fail(`checker isolated source checkout HEAD changed ${phase}`);
  }
  const {stdout} = await gitWorktreeCommand(checkout, ['status', '--porcelain=v1', '-z', '--untracked-files=all'], {
    encoding: 'buffer',
  });
  const dirty = stdout.toString('utf8').split('\0').find(Boolean);
  if (dirty) {
    fail(`checker isolated source checkout is dirty ${phase}: ${gitPath(dirty.slice(3))}`);
  }
}

async function reconstructCompiledGoPackageSources(root, gitFacts, row, profile, sourceCommit) {
  const packagePath = `./${path.posix.dirname(gitPath(profile.automatedTest))}`;
  const cacheKey = `${sourceCommit}:${packagePath}`;
  if (!gitFacts.compiledGoPackages.has(cacheKey)) {
    gitFacts.compiledGoPackages.set(
      cacheKey,
      (async () => {
        const base = await mkdtemp(path.join(tmpdir(), 'control-plane-checker-source-'));
        const checkout = path.join(base, 'source');
        let registered = false;
        try {
          await gitWorktreeCommand(root, ['worktree', 'add', '--detach', '--quiet', checkout, sourceCommit]);
          registered = true;
          await requireCleanDetachedCheckout(checkout, sourceCommit, 'before go list');
          const {stdout} = await execFileAsync('go', ['list', '-json', packagePath], {
            cwd: checkout,
            env: controlledGoEnvironment(),
            maxBuffer: 16 * 1024 * 1024,
          });
          const metadata = JSON.parse(stdout);
          const checkoutReal = await realpath(checkout);
          const packageReal = await realpath(metadata.Dir);
          if (!pathIsWithin(checkoutReal, packageReal)) {
            fail(`${row.id} go list package resolves outside the isolated source checkout`);
          }
          const names = [...(metadata.TestGoFiles ?? []), ...(metadata.XTestGoFiles ?? [])];
          const manifest = [];
          for (const name of names) {
            const resolved = path.resolve(metadata.Dir, name);
            if (!pathIsWithin(packageReal, resolved)) {
              fail(`${row.id} go list test source escapes package: ${name}`);
            }
            const sourcePath = gitPath(path.relative(checkout, resolved));
            const bytes = await sourceBlob(
              root,
              gitFacts,
              sourceCommit,
              sourcePath,
              row.id,
              'compiled Go package source'
            );
            manifest.push({path: sourcePath, sha256: sha256(bytes)});
          }
          manifest.sort((left, right) => left.path.localeCompare(right.path));
          await requireCleanDetachedCheckout(checkout, sourceCommit, 'after go list');
          return manifest;
        } finally {
          if (registered) {
            await gitWorktreeCommand(root, ['worktree', 'remove', '--force', checkout], {allowFailure: true});
          }
          await rm(base, {recursive: true, force: true});
          await gitWorktreeCommand(root, ['worktree', 'prune'], {allowFailure: true});
        }
      })()
    );
  }
  return gitFacts.compiledGoPackages.get(cacheKey);
}

async function validateCompiledGoPackageSources(root, gitFacts, row, profile, sourceCommit, result) {
  if (profile.testRunner !== 'go-test') {
    if (Object.hasOwn(result, 'compiledPackageSources')) {
      fail(`${row.id} compiledPackageSources is supported only for go-test results`);
    }
    return;
  }
  const manifest = result.compiledPackageSources;
  if (!Array.isArray(manifest) || new Set(manifest.map((binding) => binding?.path)).size !== manifest.length) {
    fail(`${row.id} compiled Go package source manifest must contain unique source bindings`);
  }
  if (!manifest.some((binding) => binding?.path === row.automatedTest)) {
    fail(`${row.id} compiled Go package source manifest must include ${row.automatedTest}`);
  }
  const packageDirectory = path.posix.dirname(gitPath(row.automatedTest));
  const sources = [];
  for (const binding of manifest) {
    if (
      !binding ||
      typeof binding.path !== 'string' ||
      path.posix.dirname(gitPath(binding.path)) !== packageDirectory ||
      !binding.path.endsWith('_test.go') ||
      !checksumPattern.test(binding.sha256 ?? '')
    ) {
      fail(`${row.id} compiled Go package source bindings must be checksummed *_test.go files in ${packageDirectory}`);
    }
    const resolved = await requireFile(root, binding.path, row.id, 'compiled Go package source');
    requireTracked(gitFacts, binding.path, row.id, 'compiled Go package source');
    const currentBytes = await readFile(resolved);
    const committedBytes = await sourceBlob(
      root,
      gitFacts,
      sourceCommit,
      binding.path,
      row.id,
      'compiled Go package source'
    );
    if (sha256(currentBytes) !== binding.sha256 || sha256(committedBytes) !== binding.sha256) {
      fail(`${row.id} compiled Go package source checksum mismatch for ${binding.path}`);
    }
    sources.push({path: binding.path, bytes: committedBytes, rejectBuildConstraints: false});
  }
  const expectedManifest = await reconstructCompiledGoPackageSources(root, gitFacts, row, profile, sourceCommit);
  const actualManifest = manifest
    .map((binding) => ({path: gitPath(binding.path), sha256: binding.sha256}))
    .sort((left, right) => left.path.localeCompare(right.path));
  if (JSON.stringify(actualManifest) !== JSON.stringify(expectedManifest)) {
    fail(`${row.id} compiled Go package source manifest must exactly match go list at sourceCommit`);
  }
  const declarations = await declaredGoTestsForSources(sources);
  for (const selected of selectedGoTests(profile, row.id)) {
    let count = 0;
    for (const tests of declarations.values()) {
      count += tests.filter((name) => name === selected).length;
    }
    if (count !== 1) {
      fail(`${row.id} compiled Go package manifest must declare ${selected} exactly once; found ${count}`);
    }
  }
}

async function validateTestResult(root, gitFacts, row, profile, sourceCommit, binding) {
  if (!binding || typeof binding.path !== 'string' || !checksumPattern.test(binding.sha256 ?? '')) {
    fail(`${row.id} testResult must contain a repository path and SHA-256 checksum`);
  }
  requireTracked(gitFacts, binding.path, row.id, 'test result');
  const resultFile = await parseJSONFile(root, binding.path, row.id, 'test result');
  if (sha256(resultFile.bytes) !== binding.sha256) {
    fail(`${row.id} test result checksum mismatch for ${binding.path}`);
  }
  const result = resultFile.value;
  if (result.schema !== testResultSchema) {
    fail(`${row.id} test result schema must be ${testResultSchema}`);
  }
  if (result.sourceCommit !== sourceCommit) {
    fail(`${row.id} test result sourceCommit must match evidence sourceCommit`);
  }
  validateTestCommand(row, profile, result);
  await validateCompiledGoPackageSources(root, gitFacts, row, profile, sourceCommit, result);
  if (result.status !== 'passed') {
    fail(`${row.id} test result status must be passed`);
  }
  const startedAt = requireTimestamp(result.startedAt, row.id, 'test result startedAt');
  const completedAt = requireTimestamp(result.completedAt, row.id, 'test result completedAt');
  if (completedAt < startedAt) {
    fail(`${row.id} test result completedAt must not precede startedAt`);
  }
  return result;
}

async function readTrackedBinding(root, gitFacts, row, binding, label) {
  if (!binding || typeof binding.path !== 'string' || !checksumPattern.test(binding.sha256 ?? '')) {
    fail(`${row.id} ${label} must contain a repository path and SHA-256 checksum`);
  }
  requireTracked(gitFacts, binding.path, row.id, label);
  const file = await parseJSONFile(root, binding.path, row.id, label);
  if (sha256(file.bytes) !== binding.sha256) {
    fail(`${row.id} ${label} checksum mismatch for ${binding.path}`);
  }
  return file.value;
}

function jsonEqual(left, right) {
  return JSON.stringify(left) === JSON.stringify(right);
}

function aggregateSamples(samples, aggregation) {
  if (aggregation === 'sum') {
    return samples.reduce((total, value) => total + value, 0);
  }
  const sorted = [...samples].sort((left, right) => left - right);
  if (aggregation === 'max') {
    return sorted[sorted.length - 1];
  }
  const percentile = aggregation === 'p95' ? 0.95 : 0.99;
  return sorted[Math.max(0, Math.ceil(percentile * sorted.length) - 1)];
}

function thresholdPasses(measured, operator, limit) {
  if (operator === 'eq') {
    return measured === limit;
  }
  if (operator === 'lt') {
    return measured < limit;
  }
  return measured <= limit;
}

async function validatePerformanceEvidence(root, gitFacts, row, profile, sourceCommit, binding) {
  const report = await readTrackedBinding(root, gitFacts, row, binding, 'performance report');
  if (report.schema !== performanceResultSchema) {
    fail(`${row.id} performance report schema must be ${performanceResultSchema}`);
  }
  if (report.sourceCommit !== sourceCommit) {
    fail(`${row.id} performance report sourceCommit must match evidence sourceCommit`);
  }
  if (report.scenario !== profile.proofRequirements?.performanceScenario) {
    fail(`${row.id} performance report scenario must be ${profile.proofRequirements?.performanceScenario}`);
  }
  if (report.mode !== 'measured-live') {
    fail(`${row.id} performance report mode must be measured-live`);
  }
  if (report.status !== 'passed') {
    fail(`${row.id} performance report status must be passed`);
  }
  const rawSamples = await readTrackedBinding(root, gitFacts, row, report.rawSamples, 'performance raw samples');
  if (!rawSamples.series || typeof rawSamples.series !== 'object' || Array.isArray(rawSamples.series)) {
    fail(`${row.id} performance raw samples must contain metric series`);
  }
  const hardware = report.hardware;
  for (const field of ['os', 'architecture', 'cpu']) {
    requireString(hardware?.[field], row.id, `performance hardware ${field}`);
  }
  if (
    !Number.isInteger(hardware?.logicalCores) ||
    hardware.logicalCores <= 0 ||
    !Number.isInteger(hardware?.memoryBytes) ||
    hardware.memoryBytes <= 0
  ) {
    fail(`${row.id} performance hardware must include positive logicalCores and memoryBytes`);
  }
  requireString(report.build?.version, row.id, 'performance build version');
  if (!checksumPattern.test(report.build?.artifactDigest ?? '')) {
    fail(`${row.id} performance build artifactDigest must be a SHA-256 digest`);
  }
  const dataset = report.dataset;
  const minimums = {targets: 1000, placements: 649, onlineExecutors: 100, components: 100, steps: 500};
  for (const [field, minimum] of Object.entries(minimums)) {
    if (!Number.isInteger(dataset?.[field]) || dataset[field] < minimum) {
      fail(`${row.id} performance dataset ${field} must be at least ${minimum}`);
    }
  }
  const requirements = profile.proofRequirements.performanceMetrics;
  const expectedNames = requirements.map((requirement) => requirement.name);
  const thresholds = Array.isArray(report.thresholds) ? report.thresholds : [];
  const actualNames = thresholds.map((threshold) => threshold?.name);
  const seriesNames = Object.keys(rawSamples.series);
  if (
    actualNames.length !== expectedNames.length ||
    new Set(actualNames).size !== expectedNames.length ||
    seriesNames.length !== expectedNames.length ||
    new Set(seriesNames).size !== expectedNames.length ||
    expectedNames.some((name) => !actualNames.includes(name) || !seriesNames.includes(name)) ||
    actualNames.some((name) => !expectedNames.includes(name)) ||
    seriesNames.some((name) => !expectedNames.includes(name))
  ) {
    fail(`${row.id} performance metrics must exactly match the contract`);
  }
  for (const requirement of requirements) {
    const threshold = thresholds.find((candidate) => candidate.name === requirement.name);
    const samples = rawSamples.series[requirement.name];
    if (
      !Array.isArray(samples) ||
      samples.length < requirement.minSamples ||
      samples.some((sample) => !Number.isFinite(sample))
    ) {
      fail(
        `${row.id} performance raw series ${requirement.name} must contain finite numeric samples and meet minSamples`
      );
    }
    const measured = aggregateSamples(samples, requirement.aggregation);
    if (
      threshold.aggregation !== requirement.aggregation ||
      threshold.operator !== requirement.operator ||
      threshold.limit !== requirement.limit ||
      threshold.unit !== requirement.unit ||
      threshold.samples !== samples.length ||
      !Number.isFinite(threshold.measured) ||
      Math.abs(threshold.measured - measured) > 1e-9 ||
      !thresholdPasses(measured, requirement.operator, requirement.limit) ||
      threshold.passed !== true
    ) {
      fail(`${row.id} performance threshold ${requirement.name} must match its contract and raw sample series`);
    }
  }
  const factRequirements = profile.proofRequirements.performanceFacts;
  const facts = report.facts;
  if (!facts || typeof facts !== 'object' || Array.isArray(facts)) {
    fail(`${row.id} performance report must retain scenario facts`);
  }
  for (const [name, expected] of Object.entries(factRequirements.exact)) {
    if (!jsonEqual(facts[name], expected)) {
      fail(`${row.id} performance fact ${name} must equal ${JSON.stringify(expected)}`);
    }
  }
  for (const [name, minimum] of Object.entries(factRequirements.minimum)) {
    if (!Number.isFinite(facts[name]) || facts[name] < minimum) {
      fail(`${row.id} performance fact ${name} must be at least ${minimum}`);
    }
  }
  if (report.scenario === 'fleet-api-slos') {
    if (
      !Number.isInteger(facts.maxResponseItems) ||
      facts.maxResponseItems <= 0 ||
      facts.maxResponseItems > facts.pageSize
    ) {
      fail(`${row.id} fleet-api-slos maxResponseItems must be positive and no greater than pageSize`);
    }
  }
  if (report.scenario === 'roadmap-scale-load') {
    if (facts.authentication !== 'authenticated-live') {
      fail(`${row.id} roadmap-scale-load authentication must be authenticated-live`);
    }
    if (
      facts.concurrentAgents !== 100 ||
      !Array.isArray(facts.authenticatedExecutorIds) ||
      facts.authenticatedExecutorIds.length !== 100 ||
      new Set(facts.authenticatedExecutorIds).size !== 100 ||
      facts.authenticatedExecutorIds.some((value) => typeof value !== 'string' || value === '')
    ) {
      fail(`${row.id} roadmap-scale-load must retain exactly 100 unique authenticatedExecutorIds`);
    }
    const executorIDsChecksum = sha256(JSON.stringify([...facts.authenticatedExecutorIds].sort()));
    if (
      !checksumPattern.test(facts.authenticatedExecutorIdsChecksum ?? '') ||
      facts.authenticatedExecutorIdsChecksum !== executorIDsChecksum
    ) {
      fail(`${row.id} roadmap-scale-load authenticatedExecutorIdsChecksum must match the sorted identity set`);
    }
    if (
      !Array.isArray(facts.planChecksums) ||
      facts.planChecksums.length !== 5 ||
      facts.planChecksums.some((value) => !checksumPattern.test(value)) ||
      new Set(facts.planChecksums).size !== 1
    ) {
      fail(`${row.id} roadmap-scale-load planChecksums must contain five identical SHA-256 values`);
    }
    if (
      !Array.isArray(facts.waveOrderChecksums) ||
      facts.waveOrderChecksums.length < 2 ||
      facts.waveOrderChecksums.some((value) => !checksumPattern.test(value)) ||
      new Set(facts.waveOrderChecksums).size !== 1
    ) {
      fail(`${row.id} roadmap-scale-load waveOrderChecksums must contain repeated identical SHA-256 values`);
    }
    if (facts.acceptedEvents < facts.eventDurationSeconds * facts.eventRatePerSecond) {
      fail(`${row.id} roadmap-scale-load acceptedEvents must cover the full duration and rate`);
    }
    if (
      !Number.isInteger(facts.logPeakBufferBytes) ||
      facts.logPeakBufferBytes <= 0 ||
      facts.logPeakBufferBytes >= facts.logBytes
    ) {
      fail(`${row.id} roadmap-scale-load logPeakBufferBytes must prove bounded streaming below logBytes`);
    }
  }
}

function validateNeutralReleaseLineage(row, lineage) {
  if (!lineage || typeof lineage !== 'object' || Array.isArray(lineage)) {
    fail(`${row.id} neutral-live report must retain shared releaseLineage`);
  }
  if (!Array.isArray(lineage.componentReleases) || lineage.componentReleases.length !== 2) {
    fail(`${row.id} neutral-live lineage must retain exactly two Component Releases`);
  }
  const componentIDs = new Set();
  for (const release of lineage.componentReleases) {
    requireString(release?.id, row.id, 'neutral-live Component Release id');
    requireString(release?.version, row.id, 'neutral-live Component Release version');
    if (!checksumPattern.test(release?.artifactDigest ?? '') || componentIDs.has(release.id)) {
      fail(`${row.id} neutral-live Component Releases must have unique IDs and immutable artifact digests`);
    }
    componentIDs.add(release.id);
  }
  if (!Array.isArray(lineage.productReleases) || lineage.productReleases.length !== 2) {
    fail(`${row.id} neutral-live lineage must retain exactly two Product Releases`);
  }
  const productIDs = new Set();
  for (const release of lineage.productReleases) {
    requireString(release?.id, row.id, 'neutral-live Product Release id');
    requireString(release?.version, row.id, 'neutral-live Product Release version');
    if (
      !checksumPattern.test(release?.manifestChecksum ?? '') ||
      !checksumPattern.test(release?.graphChecksum ?? '') ||
      !Array.isArray(release?.componentReleaseIds) ||
      release.componentReleaseIds.length === 0 ||
      release.componentReleaseIds.some((id) => !componentIDs.has(id)) ||
      productIDs.has(release.id)
    ) {
      fail(
        `${row.id} neutral-live Product Releases must bind unique IDs, manifest/graph checksums, and Component Releases`
      );
    }
    productIDs.add(release.id);
  }
  if (!Array.isArray(lineage.plans) || lineage.plans.length !== 2) {
    fail(`${row.id} neutral-live lineage must retain exact A-to-B and B-to-A plans`);
  }
  const planIDs = new Set();
  for (const plan of lineage.plans) {
    requireString(plan?.id, row.id, 'neutral-live plan id');
    if (
      !checksumPattern.test(plan?.checksum ?? '') ||
      !productIDs.has(plan?.fromProductReleaseId) ||
      !productIDs.has(plan?.toProductReleaseId) ||
      plan.fromProductReleaseId === plan.toProductReleaseId ||
      planIDs.has(plan.id)
    ) {
      fail(`${row.id} neutral-live plans must have unique IDs/checksums and exact Product Release endpoints`);
    }
    planIDs.add(plan.id);
  }
  const [releaseA, releaseB] = lineage.productReleases;
  if (
    !lineage.plans.some(
      (plan) => plan.fromProductReleaseId === releaseA.id && plan.toProductReleaseId === releaseB.id
    ) ||
    !lineage.plans.some((plan) => plan.fromProductReleaseId === releaseB.id && plan.toProductReleaseId === releaseA.id)
  ) {
    fail(`${row.id} neutral-live lineage must retain both A-to-B and B-to-A plan checksums`);
  }
  return [releaseA.id, releaseB.id, releaseA.id];
}

async function validateNeutralLiveEvidence(root, gitFacts, row, sourceCommit, binding) {
  const report = await readTrackedBinding(root, gitFacts, row, binding, 'neutral-live report');
  if (report.schema !== neutralLiveResultSchema) {
    fail(`${row.id} neutral-live report schema must be ${neutralLiveResultSchema}`);
  }
  if (report.sourceCommit !== sourceCommit) {
    fail(`${row.id} neutral-live report sourceCommit must match evidence sourceCommit`);
  }
  if (report.proofMode !== 'live-hub-api' || report.liveStack?.started !== true || report.status !== 'passed') {
    fail(`${row.id} neutral-live report must be a passed live-hub-api run with a started live stack`);
  }
  if (!Array.isArray(report.targets) || report.targets.length !== 2) {
    fail(`${row.id} neutral-live report must contain exactly two targets`);
  }
  const uniqueFields = ['targetId', 'configChecksum', 'executorId', 'observerId', 'executionId', 'observationId'];
  for (const field of uniqueFields) {
    const values = report.targets.map((target) => target[field]);
    if (values.some((value) => typeof value !== 'string' || value === '') || new Set(values).size !== 2) {
      fail(`${row.id} neutral-live targets must have two distinct ${field} values`);
    }
  }
  if (report.targets.some((target) => !checksumPattern.test(target.configChecksum) || target.status !== 'passed')) {
    fail(`${row.id} neutral-live targets must retain passed results and configuration checksums`);
  }
  const adapters = new Set(report.targets.map((target) => target.adapterKind));
  if (!adapters.has('external-executor') || !adapters.has('reference') || adapters.size !== 2) {
    fail(`${row.id} neutral-live targets must use external-executor and reference adapters`);
  }
  if (report.targets.some((target) => target.observerId === target.executorId)) {
    fail(`${row.id} neutral-live observers must be independent from executors`);
  }
  const expectedHistory = validateNeutralReleaseLineage(row, report.releaseLineage);
  for (const target of report.targets) {
    if (!jsonEqual(target.releaseLineage, report.releaseLineage)) {
      fail(`${row.id} neutral-live target ${target.targetId} release lineage must match the shared lineage`);
    }
  }
  if (!jsonEqual(report.releaseHistory, expectedHistory)) {
    fail(`${row.id} neutral-live report must retain exact Product Release A-B-A history`);
  }
  if (report.cleanup?.completed !== true || report.nonLocalCalls !== 0) {
    fail(`${row.id} neutral-live report must complete cleanup and record zero non-local calls`);
  }
}

function requireExactObjectKeys(value, keys, id, label) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    fail(`${id} ${label} must be an object`);
  }
  const actual = Object.keys(value).sort();
  const expected = [...keys].sort();
  if (!jsonEqual(actual, expected)) fail(`${id} ${label} fields must exactly match the browser evidence schema`);
}

async function requireNoReparsePath(root, value, row, label) {
  const resolved = repositoryPath(root, value, row.id, label);
  const relative = path.relative(root, resolved);
  let current = root;
  for (const segment of relative.split(path.sep).filter(Boolean)) {
    current = path.join(current, segment);
    const information = await lstat(current);
    if (information.isSymbolicLink()) fail(`${row.id} ${label} must not traverse a reparse point`);
  }
  const [rootReal, resolvedReal] = await Promise.all([realpath(root), realpath(resolved)]);
  if (!pathIsWithin(rootReal, resolvedReal)) fail(`${row.id} ${label} must resolve inside the repository`);
}

async function readTrackedBytesBinding(root, gitFacts, row, binding, label) {
  if (!binding || typeof binding.path !== 'string' || !checksumPattern.test(binding.sha256 ?? '')) {
    fail(`${row.id} ${label} must contain a repository path and SHA-256 checksum`);
  }
  requireTracked(gitFacts, binding.path, row.id, label);
  await requireNoReparsePath(root, binding.path, row, label);
  const resolved = await requireFile(root, binding.path, row.id, label);
  const bytes = await readFile(resolved);
  if (sha256(bytes) !== binding.sha256) fail(`${row.id} ${label} checksum mismatch for ${binding.path}`);
  return bytes;
}

function collectBrowserSpecs(suite, result = []) {
  if (!suite || typeof suite !== 'object') return result;
  if (Array.isArray(suite.specs)) result.push(...suite.specs);
  if (Array.isArray(suite.suites)) {
    for (const child of suite.suites) collectBrowserSpecs(child, result);
  }
  return result;
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

function normalizedRawBrowserSource(raw, value) {
  if (typeof value !== 'string' || value === '') return '';
  if (!path.isAbsolute(value)) return gitPath(value);
  const rootDir = raw?.config?.rootDir;
  if (typeof rootDir !== 'string' || !path.isAbsolute(rootDir) || !pathIsWithin(rootDir, value)) return '';
  return gitPath(path.relative(rootDir, value));
}

function validateRawBrowserReport(row, raw, toolVersions) {
  if (containsJSONSecret(raw)) fail(`${row.id} retained Playwright JSON contains a secret-like value`);
  const stats = raw?.stats;
  const specs = Array.isArray(raw?.suites) ? raw.suites.flatMap((suite) => collectBrowserSpecs(suite)) : [];
  const spec = specs[0];
  const test = spec?.tests?.[0];
  const attempt = test?.results?.[0];
  const attachments = attempt?.attachments;
  if (
    !Array.isArray(raw?.errors) ||
    raw.errors.length !== 0 ||
    stats?.expected !== 1 ||
    stats?.skipped !== 0 ||
    stats?.flaky !== 0 ||
    stats?.unexpected !== 0 ||
    !Number.isFinite(Date.parse(stats?.startTime)) ||
    !Number.isFinite(stats?.duration) ||
    stats.duration < 0 ||
    specs.length !== 1 ||
    spec.title !== browserTitle ||
    spec.ok !== true ||
    !Array.isArray(spec.tests) ||
    spec.tests.length !== 1 ||
    test.projectName !== browserProject ||
    test.expectedStatus !== 'passed' ||
    test.status !== 'expected' ||
    !Array.isArray(test.results) ||
    test.results.length !== 1 ||
    attempt.retry !== 0 ||
    attempt.status !== 'passed' ||
    !Array.isArray(attempt.errors) ||
    attempt.errors.length !== 0 ||
    !Array.isArray(attachments) ||
    attachments.length !== browserScreenshotNames.length ||
    attachments.some(
      (attachment, index) =>
        attachment?.name !== browserScreenshotNames[index] ||
        attachment?.contentType !== 'image/png' ||
        typeof attachment?.path !== 'string' ||
        path.posix.basename(gitPath(attachment.path)) !== browserScreenshotNames[index]
    ) ||
    raw?.config?.version !== toolVersions.playwright
  ) {
    fail(`${row.id} raw Playwright report must contain the exact passed test and 11 attachments`);
  }
  if (normalizedRawBrowserSource(raw, spec.file) !== browserAutomatedTest) {
    fail(`${row.id} raw Playwright report source must normalize to the bound automated test`);
  }
  const started = Date.parse(stats.startTime);
  return {
    startedAt: new Date(started).toISOString(),
    completedAt: new Date(started + stats.duration).toISOString(),
  };
}

function crc32(bytes) {
  let value = 0xffffffff;
  for (const byte of bytes) {
    value ^= byte;
    for (let bit = 0; bit < 8; bit += 1) value = (value >>> 1) ^ (0xedb88320 & -(value & 1));
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

function pngDimensions(bytes, row, name) {
  const invalid = () => fail(`${row.id} browser screenshot PNG is invalid: ${name}`);
  if (bytes.length < pngSignature.length || !bytes.subarray(0, pngSignature.length).equals(pngSignature)) invalid();
  let offset = pngSignature.length;
  let width;
  let height;
  let ihdrCount = 0;
  let idatCount = 0;
  let ended = false;
  const metadata = [];
  while (offset + 12 <= bytes.length) {
    const length = bytes.readUInt32BE(offset);
    const end = offset + 12 + length;
    if (end > bytes.length) invalid();
    const typeBytes = bytes.subarray(offset + 4, offset + 8);
    const type = typeBytes.toString('ascii');
    if (!/^[A-Za-z]{4}$/.test(type)) invalid();
    const data = bytes.subarray(offset + 8, offset + 8 + length);
    if (crc32(Buffer.concat([typeBytes, data])) !== bytes.readUInt32BE(offset + 8 + length)) invalid();
    if (type === 'IHDR') {
      ihdrCount += 1;
      if (ihdrCount !== 1 || offset !== pngSignature.length || length !== 13) invalid();
      width = data.readUInt32BE(0);
      height = data.readUInt32BE(4);
    } else if (type === 'IDAT') {
      if (ihdrCount !== 1 || ended) invalid();
      idatCount += 1;
    } else if (type === 'IEND') {
      if (length !== 0 || ihdrCount !== 1 || idatCount === 0) invalid();
      ended = true;
    }
    if (['tEXt', 'zTXt', 'iTXt'].includes(type)) metadata.push(pngMetadataText(type, data));
    offset = end;
    if (ended) break;
  }
  if (!ended || offset !== bytes.length || ihdrCount !== 1 || idatCount === 0 || !width || !height) invalid();
  if (metadata.some((value) => secretLikeString(value))) {
    fail(`${row.id} browser screenshot metadata contains a secret: ${name}`);
  }
  return {width, height};
}

async function sourcePathExists(root, sourceCommit, value) {
  try {
    await execFileAsync('git', ['cat-file', '-e', `${sourceCommit}:${value}`], {cwd: root});
    return true;
  } catch {
    return false;
  }
}

async function validateBrowserExecutionSources(root, gitFacts, row, sourceCommit, bindings) {
  const expectedPaths = [...browserExecutionSourcePaths];
  if (await sourcePathExists(root, sourceCommit, 'pnpm-lock.yaml')) expectedPaths.push('pnpm-lock.yaml');
  if (
    !Array.isArray(bindings) ||
    bindings.length !== expectedPaths.length ||
    bindings.some(
      (binding, index) =>
        !binding ||
        Object.keys(binding).length !== 2 ||
        binding.path !== expectedPaths[index] ||
        !checksumPattern.test(binding.sha256 ?? '')
    )
  ) {
    fail(
      `${row.id} browser execution sources must exactly bind the purpose-built config, fixture, package, and lockfile`
    );
  }
  for (const binding of bindings) {
    requireTracked(gitFacts, binding.path, row.id, 'browser execution source');
    const currentPath = await requireFile(root, binding.path, row.id, 'browser execution source');
    const [current, committed] = await Promise.all([
      readFile(currentPath),
      sourceBlob(root, gitFacts, sourceCommit, binding.path, row.id, 'browser execution source'),
    ]);
    if (sha256(current) !== binding.sha256 || sha256(committed) !== binding.sha256) {
      fail(`${row.id} browser execution source checksum mismatch for ${binding.path}`);
    }
  }
}

function requireBrowserNetworkSource(row, automatedSource, fixtureSource, networkProof) {
  const automated = automatedSource.toString('utf8');
  const fixture = fixtureSource.toString('utf8');
  const titleOffset = automated.indexOf(browserTitle);
  const nextTest = /\n\s*test(?:\.(?:only|skip|fixme))?\s*\(/g;
  nextTest.lastIndex = Math.max(0, titleOffset + browserTitle.length);
  const next = nextTest.exec(automated);
  const evidenceBody = automated.slice(titleOffset, next?.index ?? automated.length);
  if (
    !networkProof ||
    !jsonEqual(Object.keys(networkProof).sort(), ['externalAttempts', 'mode', 'testTitle']) ||
    networkProof.mode !== 'bound-test-assertion' ||
    networkProof.testTitle !== browserTitle ||
    networkProof.externalAttempts !== 0 ||
    titleOffset < 0 ||
    !/expect\s*\(\s*controlPlane\.externalAttempts\s*\)\s*\.toEqual\s*\(\s*\[\s*\]\s*\)/.test(evidenceBody) ||
    !/page\.route\s*\(\s*['"]\*\*\/\*['"]/.test(fixture) ||
    !/!\s*isLocalHost\s*\(/.test(fixture) ||
    !/externalAttempts\.push\s*\(/.test(fixture) ||
    !/route\.abort\s*\(/.test(fixture)
  ) {
    fail(`${row.id} browser network proof must bind the exact test assertion and zero external attempts`);
  }
}

async function validateBrowserEvidence(root, gitFacts, row, sourceCommit, binding, genericResult) {
  const report = await readTrackedBinding(root, gitFacts, row, binding, 'browser report');
  if (report.schema !== browserResultSchema) {
    fail(`${row.id} browser report schema must be ${browserResultSchema}`);
  }
  requireExactObjectKeys(
    report,
    [
      'schema',
      'sourceCommit',
      'runner',
      'status',
      'project',
      'testSource',
      'testTitles',
      'tests',
      'rawResult',
      'screenshots',
      'networkProof',
      'executionSources',
      'toolVersions',
    ],
    row.id,
    'browser report'
  );
  if (report.sourceCommit !== sourceCommit) {
    fail(`${row.id} browser report sourceCommit must match evidence sourceCommit`);
  }
  if (report.runner !== 'playwright' || report.status !== 'passed') {
    fail(`${row.id} browser report must be a passed Playwright result`);
  }
  if (
    report.project !== browserProject ||
    report.testSource !== browserAutomatedTest ||
    !jsonEqual(report.testTitles, [browserTitle])
  ) {
    fail(`${row.id} browser report must bind the exact project, source, and title`);
  }
  if (!jsonEqual(report.tests, {expected: 1, passed: 1, unexpected: 0, flaky: 0, skipped: 0})) {
    fail(`${row.id} browser report must contain exactly one passed test`);
  }
  requireExactObjectKeys(report.toolVersions, ['node', 'pnpm', 'playwright'], row.id, 'browser toolVersions');
  const versionPattern = /^\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$/;
  if (Object.values(report.toolVersions).some((version) => !versionPattern.test(version))) {
    fail(`${row.id} browser tool versions must be canonical and match the raw Playwright report`);
  }
  await requireNoReparsePath(root, report.rawResult?.path, row, 'browser raw Playwright report');
  const raw = await readTrackedBinding(root, gitFacts, row, report.rawResult, 'browser raw Playwright report');
  if (raw?.config?.version !== report.toolVersions.playwright) {
    fail(`${row.id} browser tool versions must be canonical and match the raw Playwright report`);
  }
  const rawTimes = validateRawBrowserReport(row, raw, report.toolVersions);
  if (
    !jsonEqual(genericResult.tests, {expected: 1, passed: 1, failed: 0, skipped: 0}) ||
    genericResult.startedAt !== rawTimes.startedAt ||
    genericResult.completedAt !== rawTimes.completedAt
  ) {
    fail(`${row.id} generic and raw browser result counts and timestamps must exactly match`);
  }

  if (
    !Array.isArray(report.screenshots) ||
    report.screenshots.length !== browserScreenshotNames.length ||
    report.screenshots.some(
      (screenshot, index) =>
        !screenshot ||
        !jsonEqual(Object.keys(screenshot).sort(), ['height', 'name', 'path', 'sha256', 'width']) ||
        screenshot.name !== browserScreenshotNames[index] ||
        path.posix.basename(gitPath(screenshot.path ?? '')) !== browserScreenshotNames[index] ||
        screenshot.width !== 1440 ||
        screenshot.height !== 1200
    )
  ) {
    fail(`${row.id} browser screenshots must exactly match the 11 retained visual checkpoints`);
  }
  for (const screenshot of report.screenshots) {
    let bytes;
    try {
      bytes = await readTrackedBytesBinding(root, gitFacts, row, screenshot, 'browser screenshot');
    } catch (error) {
      if (error.message.includes('checksum mismatch')) {
        fail(`${row.id} browser screenshot checksum mismatch for ${screenshot.name}`);
      }
      throw error;
    }
    const dimensions = pngDimensions(bytes, row, screenshot.name);
    if (dimensions.width !== screenshot.width || dimensions.height !== screenshot.height) {
      fail(`${row.id} browser screenshot PNG dimensions must match 1440x1200`);
    }
  }

  await validateBrowserExecutionSources(root, gitFacts, row, sourceCommit, report.executionSources);
  let packageManifest;
  try {
    const packageBytes = await sourceBlob(
      root,
      gitFacts,
      sourceCommit,
      'package.json',
      row.id,
      'browser package manifest'
    );
    packageManifest = JSON.parse(packageBytes.toString('utf8'));
  } catch {
    fail(`${row.id} browser package manifest must be valid JSON`);
  }
  const packageManagerMatch = /^pnpm@(\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?)/.exec(
    packageManifest?.packageManager ?? ''
  );
  if (!packageManagerMatch || packageManagerMatch[1] !== report.toolVersions.pnpm) {
    fail(`${row.id} browser pnpm version must match source-bound packageManager`);
  }
  const [automatedSource, fixtureSource] = await Promise.all([
    sourceBlob(root, gitFacts, sourceCommit, browserAutomatedTest, row.id, 'browser automated test'),
    sourceBlob(root, gitFacts, sourceCommit, browserFixture, row.id, 'browser fixture'),
  ]);
  requireBrowserNetworkSource(row, automatedSource, fixtureSource, report.networkProof);
}

async function validateProofClassEvidence(root, gitFacts, row, profile, evidence, testResult) {
  if (evidence.proofClass === 'performance-measurement') {
    await validatePerformanceEvidence(root, gitFacts, row, profile, evidence.sourceCommit, evidence.classEvidence);
  } else if (evidence.proofClass === 'neutral-live-execution') {
    await validateNeutralLiveEvidence(root, gitFacts, row, evidence.sourceCommit, evidence.classEvidence);
  } else if (evidence.proofClass === 'browser-e2e') {
    await validateBrowserEvidence(root, gitFacts, row, evidence.sourceCommit, evidence.classEvidence, testResult);
  } else if (Object.hasOwn(evidence, 'classEvidence')) {
    fail(`${row.id} proof class ${evidence.proofClass} must not contain classEvidence`);
  }
}

function sameStringSet(left, right) {
  return (
    Array.isArray(left) &&
    Array.isArray(right) &&
    left.length === right.length &&
    new Set(left).size === left.length &&
    left.every((value) => typeof value === 'string' && value !== '' && right.includes(value))
  );
}

async function validateAdopterBundle(root, gitFacts, row, evidence, execution) {
  const bundle = await readTrackedBinding(root, gitFacts, row, execution.bundle, 'adopter execution bundle');
  if (bundle.schema !== adopterBundleSchema) {
    fail(`${row.id} adopter execution bundle schema must be ${adopterBundleSchema}`);
  }
  if (bundle.sourceCommit !== evidence.sourceCommit) {
    fail(`${row.id} adopter execution bundle sourceCommit must match evidence sourceCommit`);
  }
  for (const field of ['taskOwner', 'organizationId', 'environmentId']) {
    if (bundle[field] !== execution[field]) {
      fail(`${row.id} adopter execution bundle ${field} must match adopterExecution`);
    }
  }
  for (const field of ['targetIds', 'campaignIds']) {
    if (!sameStringSet(bundle[field], execution[field])) {
      fail(`${row.id} adopter execution bundle ${field} must exactly match adopterExecution`);
    }
  }
  if (bundle.status !== 'passed') {
    fail(`${row.id} adopter execution bundle status must be passed`);
  }
  if (!Array.isArray(bundle.executions) || bundle.executions.length === 0) {
    fail(`${row.id} adopter execution bundle must contain executions`);
  }
  const executionIDs = new Set();
  for (const item of bundle.executions) {
    for (const field of ['executionId', 'targetId', 'campaignId', 'executorId']) {
      requireString(item[field], row.id, `adopter execution ${field}`);
    }
    if (
      !execution.targetIds.includes(item.targetId) ||
      !execution.campaignIds.includes(item.campaignId) ||
      !checksumPattern.test(item.artifactDigest ?? '') ||
      item.status !== 'succeeded' ||
      executionIDs.has(item.executionId)
    ) {
      fail(`${row.id} adopter executions must have unique exact IDs, digests, scope, and succeeded status`);
    }
    executionIDs.add(item.executionId);
  }
  if (
    execution.targetIds.some((targetId) => !bundle.executions.some((item) => item.targetId === targetId)) ||
    execution.campaignIds.some((campaignId) => !bundle.executions.some((item) => item.campaignId === campaignId))
  ) {
    fail(`${row.id} adopter executions must cover every retained target and campaign`);
  }
  if (!Array.isArray(bundle.observations) || bundle.observations.length === 0) {
    fail(`${row.id} adopter execution bundle must contain observations`);
  }
  const observationIDs = new Set();
  for (const item of bundle.observations) {
    const matchingExecution = bundle.executions.find((candidate) => candidate.executionId === item.executionId);
    for (const field of ['observationId', 'executionId', 'targetId', 'observerId']) {
      requireString(item[field], row.id, `adopter observation ${field}`);
    }
    if (
      !matchingExecution ||
      matchingExecution.targetId !== item.targetId ||
      matchingExecution.executorId === item.observerId ||
      matchingExecution.artifactDigest !== item.artifactDigest ||
      !checksumPattern.test(item.configChecksum ?? '') ||
      item.status !== 'verified' ||
      observationIDs.has(item.observationId)
    ) {
      fail(`${row.id} adopter observations must independently verify exact executions, targets, and checksums`);
    }
    observationIDs.add(item.observationId);
  }
  if (
    bundle.executions.some(
      (item) => !bundle.observations.some((observation) => observation.executionId === item.executionId)
    )
  ) {
    fail(`${row.id} adopter observations must cover every retained execution`);
  }
  const audit = await readTrackedBinding(root, gitFacts, row, bundle.auditExport, 'adopter audit export');
  if (audit.schema !== adopterAuditSchema) {
    fail(`${row.id} adopter audit export schema must be ${adopterAuditSchema}`);
  }
  if (audit.sourceCommit !== evidence.sourceCommit || audit.status !== 'passed') {
    fail(`${row.id} adopter audit export must be passed and match evidence sourceCommit`);
  }
  for (const field of ['taskOwner', 'organizationId', 'environmentId']) {
    if (audit[field] !== execution[field]) {
      fail(`${row.id} adopter audit export ${field} must match adopterExecution`);
    }
  }
  const exactSets = {
    targetIds: execution.targetIds,
    campaignIds: execution.campaignIds,
    executionIds: [...executionIDs],
    observationIds: [...observationIDs],
  };
  for (const [field, expected] of Object.entries(exactSets)) {
    if (!sameStringSet(audit[field], expected)) {
      fail(`${row.id} adopter audit export ${field} must exactly match the execution bundle`);
    }
  }
  if (!checksumPattern.test(audit.eventChecksum ?? '')) {
    fail(`${row.id} adopter audit export eventChecksum must be a SHA-256 checksum`);
  }
}

async function validateAdopterExecution(root, gitFacts, row, evidence) {
  const execution = evidence.adopterExecution;
  if (!execution || typeof execution !== 'object' || Array.isArray(execution)) {
    fail(`${row.id} verified-adopter evidence must include adopterExecution`);
  }
  if (execution.taskOwner !== row.owner) {
    fail(`${row.id} verified-adopter taskOwner must be ${row.owner}`);
  }
  requireString(execution.organizationId, row.id, 'verified-adopter organizationId');
  requireString(execution.environmentId, row.id, 'verified-adopter environmentId');
  requireStringArray(execution.targetIds, row.id, 'targetIds');
  requireStringArray(execution.campaignIds, row.id, 'campaignIds');
  const startedAt = requireTimestamp(execution.startedAt, row.id, 'verified-adopter startedAt');
  const completedAt = requireTimestamp(execution.completedAt, row.id, 'verified-adopter completedAt');
  if (completedAt < startedAt) {
    fail(`${row.id} verified-adopter completedAt must not precede startedAt`);
  }
  if (execution.result !== 'passed') {
    fail(`${row.id} verified-adopter result must be passed`);
  }
  await validateAdopterBundle(root, gitFacts, row, evidence, execution);
}

async function validateEvidenceArtifact(root, gitFacts, row, profile) {
  if (row.status === 'pending-adopter') {
    return;
  }
  const match = /^(.*?)\s+@\s+(sha256:[0-9a-f]{64})$/.exec(row.artifact);
  if (!match) {
    fail(`${row.id} artifact/checksum must use <repository path> @ sha256:<64 lowercase hex>`);
  }
  const [, artifactPath, expectedChecksum] = match;
  requireTracked(gitFacts, artifactPath, row.id, 'evidence artifact');
  const artifactFile = await parseJSONFile(root, artifactPath, row.id, 'evidence artifact');
  if (sha256(artifactFile.bytes) !== expectedChecksum) {
    fail(`${row.id} artifact checksum mismatch for ${artifactPath}`);
  }
  const evidence = artifactFile.value;
  if (evidence.schema !== evidenceSchema) {
    fail(`${row.id} evidence artifact schema must be ${evidenceSchema}`);
  }
  if (evidence.acceptanceId !== row.id) {
    fail(`${row.id} evidence artifact acceptanceId must match the ledger row`);
  }
  if (evidence.owner !== row.owner) {
    fail(`${row.id} evidence artifact owner must be ${row.owner}`);
  }
  if (!profile.allowedProofClasses.includes(evidence.proofClass)) {
    fail(`${row.id} proof class ${evidence.proofClass || '<empty>'} is not allowed`);
  }
  await requireSourceCommit(root, gitFacts, evidence.sourceCommit, row.id);
  const automatedTestBytes = await validateBoundFile(
    root,
    gitFacts,
    row,
    evidence.sourceCommit,
    evidence.automatedTest,
    row.automatedTest,
    'automated test'
  );
  await validateSelectedGoTestDeclarations(row, profile, automatedTestBytes);
  await validateBoundFile(
    root,
    gitFacts,
    row,
    evidence.sourceCommit,
    evidence.manualEvidence,
    row.manualEvidence,
    'manual/fixture evidence'
  );
  const testResult = await validateTestResult(root, gitFacts, row, profile, evidence.sourceCommit, evidence.testResult);
  await validateProofClassEvidence(root, gitFacts, row, profile, evidence, testResult);
  if (row.status === 'verified-adopter') {
    if (evidence.proofClass !== 'adopter-execution') {
      fail(`${row.id} verified-adopter proof class must be adopter-execution`);
    }
    await validateAdopterExecution(root, gitFacts, row, evidence);
  } else if (Object.hasOwn(evidence, 'adopterExecution')) {
    fail(`${row.id} community evidence must not contain adopterExecution`);
  }
}

export async function validateAcceptanceLedger(markdown, root) {
  const rows = parseAcceptanceLedger(markdown);
  const rowsByID = new Map();
  for (const row of rows) {
    if (!/^AC-\d{2}$/.test(row.id)) {
      fail(`invalid acceptance ID ${row.id || '<empty>'}`);
    }
    if (rowsByID.has(row.id)) {
      fail(`duplicate acceptance ID ${row.id}`);
    }
    rowsByID.set(row.id, row);
  }
  for (const id of expectedIDs) {
    if (!rowsByID.has(id)) {
      fail(`missing acceptance ID ${id}`);
    }
  }
  for (const id of rowsByID.keys()) {
    if (!expectedIDs.includes(id)) {
      fail(`unexpected acceptance ID ${id}`);
    }
  }

  const gitFacts = await loadGitFacts(root);
  const contract = await loadAcceptanceContract(root, gitFacts);
  for (const id of expectedIDs) {
    const row = rowsByID.get(id);
    const rule = contract.acceptance[id];
    const profile = contract.profiles[rule.profile];
    validateRowContract(row, rule, profile);
    validateStatus(row, rule);
    await requireFile(root, row.automatedTest, id, 'automated test');
    requireTracked(gitFacts, row.automatedTest, id, 'automated test');
    await requireFile(root, row.manualEvidence, id, 'manual/fixture evidence');
    requireTracked(gitFacts, row.manualEvidence, id, 'manual/fixture evidence');
    await validateEvidenceArtifact(root, gitFacts, row, profile);
  }

  const counts = rows.reduce((result, row) => {
    result[row.status] = (result[row.status] ?? 0) + 1;
    return result;
  }, {});
  return {rows, counts};
}

async function main() {
  const [ledgerPath, ...extra] = process.argv.slice(2);
  if (!ledgerPath || extra.length > 0) {
    fail('usage: node hack/control-plane-acceptance-check.mjs <acceptance-ledger.md>');
  }
  const root = process.cwd();
  const resolvedLedger = repositoryPath(root, ledgerPath, 'ledger', 'path');
  let markdown;
  try {
    markdown = await readFile(resolvedLedger, 'utf8');
  } catch {
    fail(`acceptance ledger does not exist: ${ledgerPath}`);
  }
  const {rows, counts} = await validateAcceptanceLedger(markdown, root);
  process.stdout.write(
    `Validated ${rows.length} acceptance rows: ${counts['community-evidence-retained'] ?? 0} community-evidence-retained, ${counts['pending-adopter'] ?? 0} pending-adopter, ${counts['verified-adopter'] ?? 0} verified-adopter.\n`
  );
}

const invokedPath = process.argv[1] ? path.resolve(process.argv[1]) : '';
if (invokedPath === fileURLToPath(import.meta.url)) {
  main().catch((error) => {
    process.stderr.write(`${error.message}\n`);
    process.exitCode = 1;
  });
}
