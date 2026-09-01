#!/usr/bin/env node

import {spawn} from 'node:child_process';
import {createHash} from 'node:crypto';
import {lstat, readFile} from 'node:fs/promises';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

const emloForkProfile = 'emlo-fork';
const emloForkBaselinePath = 'docs/fork/EMLO_FORK_ADOPTER_TERM_BASELINE.json';
const emloForkProfileDocPath = 'docs/fork/EMLO_FORK_RELEASE_PROFILE.md';
const emloForkBaselineSchema = 'distr.adopter-term-baseline/v1';
const emloForkRepository = 'namemlo/distr';
const emloForkBaselineKeys = Object.freeze(['schema', 'profile', 'repository', 'sourceCommit', 'findings']);
const emloForkFindingKeys = Object.freeze(['file', 'line', 'label', 'sourceSha256']);

const policyExceptions = new Map([
  ['docs/roadmaps/DISTR_COMMUNITY_FORK_MASTER_PLAN.md', 'authoritative fork policy and historical boundary'],
  [
    'docs/superpowers/specs/2026-07-14-enterprise-operator-control-plane-design.md',
    'approved adopter validation appendix',
  ],
  [
    'docs/superpowers/plans/2026-07-14-enterprise-operator-control-plane-program.md',
    'approved community-to-adopter program boundary',
  ],
  ['docs/superpowers/plans/2026-07-14-control-plane-operator-adoption.md', 'approved adopter execution tasks'],
  [emloForkBaselinePath, 'exact named-fork finding baseline'],
  [emloForkProfileDocPath, 'named-fork profile boundary and operating instructions'],
  ['hack/control-plane-adopter-term-scan.mjs', 'scanner rule definitions'],
  ['hack/control-plane-adopter-term-scan.test.mjs', 'scanner regression fixtures'],
]);

const pathPolicyExceptions = new Map([
  ['deploy/jenkins/Jenkinsfile.hub-image', 'generic publish-only provider integration'],
  ['deploy/jenkins/publish-hub-image.sh', 'generic publish-only provider integration'],
  ['hack/test-jenkins-hub-image.sh', 'generic provider integration regression tests'],
]);

const reviewedContentExceptions = new Map([
  [
    'deploy/jenkins/publish-hub-image.sh',
    new Map([
      ['Jenkins implementation term', new Set(['die "COSIGN_PASSWORD must come from a Jenkins string credential"'])],
    ]),
  ],
  [
    'hack/pr050-govulncheck.test.mjs',
    new Map([
      [
        'Jenkins implementation term',
        new Set([
          "test('the PR-050 validator seals Jenkins signed evidence, matrix, adopter scan, and failure retention', () => {",
          "readFileSync(new URL('../deploy/jenkins/Jenkinsfile.hub-image', import.meta.url), 'utf8'),",
          "readFileSync(new URL('../deploy/jenkins/publish-hub-image.sh', import.meta.url), 'utf8'),",
        ]),
      ],
    ]),
  ],
]);

const requiredReleaseArtifacts = Object.freeze([
  'docs/api/community-release-api-index.md',
  'docs/api/operator-control-plane-api.md',
  'docs/fork/FORK_DIFF_INDEX.md',
  'docs/fork/PR-083_ENTERPRISE_CONTROL_PLANE_HARDENING.md',
  'docs/fork/UPGRADE_GUIDE.md',
  'hack/control-plane-adopter-term-scan.mjs',
  'hack/control-plane-adopter-term-scan.test.mjs',
]);

const prohibitedRules = Object.freeze([
  {label: 'EMLO adopter name', pattern: /\bemlo(?:tech)?\b/i},
  {label: 'Choice TP adopter name', pattern: /\bchoice[\s_-]*tp\b/i},
  {label: 'remittance domain term', pattern: /\bremittances?\b/i},
  {label: 'Jenkins implementation term', pattern: /\bjenkins(?:file|ci)?\b/i},
  {label: 'ECR registry term', pattern: /\b(?:amazon[\s_-]+)?ecr(?:repository)?\b/i},
  {
    label: 'private Windows path',
    pattern: /(?:^|[\s"'(])[a-z]:[\\/][^\s"'<>|]*/i,
  },
  {
    label: 'private POSIX home path',
    pattern: /(?:^|[\s"'(])(?:\/home\/|\/users\/)[^/\s"'<>]+(?:\/[^\s"'<>]*)?/i,
  },
  {
    label: 'private UNC path',
    pattern: /(?:^|[\s"'(])\\\\[^\\\s]+\\[^\\\s]+(?:\\[^\s"'<>|]*)?/i,
  },
]);
const prohibitedLabels = new Set(prohibitedRules.map(({label}) => label));

function fail(message) {
  throw new Error(message);
}

function compareText(left, right) {
  return left < right ? -1 : left > right ? 1 : 0;
}

function plural(count, singular, pluralForm = `${singular}s`) {
  return `${count} ${count === 1 ? singular : pluralForm}`;
}

function sha256(value) {
  return createHash('sha256').update(value, 'utf8').digest('hex');
}

export function validateBaseRef(value) {
  if (
    typeof value !== 'string' ||
    value.length === 0 ||
    value.length > 200 ||
    !/^[a-z0-9][a-z0-9._/-]*$/i.test(value) ||
    value.startsWith('-') ||
    value.includes('..') ||
    value.includes('//') ||
    value.includes('@{') ||
    value.endsWith('/') ||
    value.endsWith('.') ||
    value.endsWith('.lock') ||
    value.split('/').some((part) => part.length === 0 || part.startsWith('.'))
  ) {
    fail('--base must be a safe Git ref or full commit ID');
  }
  return value;
}

export function parseArgs(argv) {
  if (
    (argv.length !== 2 && argv.length !== 4) ||
    argv[0] !== '--base' ||
    (argv.length === 4 && (argv[2] !== '--profile' || argv[3] !== emloForkProfile))
  ) {
    fail('usage: node hack/control-plane-adopter-term-scan.mjs --base <git-ref> [--profile emlo-fork]');
  }
  return {
    base: validateBaseRef(argv[1]),
    profile: argv.length === 4 ? emloForkProfile : undefined,
  };
}

function runProcess(command, args, cwd) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, {
      cwd,
      shell: false,
      windowsHide: true,
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    const stdout = [];
    const stderr = [];
    child.stdout.on('data', (chunk) => stdout.push(chunk));
    child.stderr.on('data', (chunk) => stderr.push(chunk));
    child.on('error', reject);
    child.on('close', (status) => {
      resolve({
        status,
        stdout: Buffer.concat(stdout),
        stderr: Buffer.concat(stderr),
      });
    });
  });
}

async function git(cwd, args, allowedStatuses = [0]) {
  const result = await runProcess('git', args, cwd);
  if (!allowedStatuses.includes(result.status)) {
    fail(`Git command failed safely: git ${args[0]}`);
  }
  return result;
}

function nulPaths(buffer) {
  return buffer.toString('utf8').split('\0').filter(Boolean);
}

function safeRelativePath(root, value) {
  const normalized = value.replaceAll('\\', '/');
  if (
    normalized.length === 0 ||
    normalized.includes('\r') ||
    normalized.includes('\n') ||
    path.posix.isAbsolute(normalized) ||
    normalized.split('/').some((part) => part === '..')
  ) {
    fail('Git returned an unsafe changed-file path');
  }
  const resolved = path.resolve(root, ...normalized.split('/'));
  const relative = path.relative(root, resolved);
  if (relative === '' || relative === '..' || relative.startsWith(`..${path.sep}`) || path.isAbsolute(relative)) {
    fail('Git returned a changed-file path outside the repository');
  }
  return {normalized, resolved};
}

function addedLinesFromPatch(patch) {
  const added = [];
  let currentLine;
  for (const line of patch.split(/\r?\n/)) {
    const hunk = /^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@/.exec(line);
    if (hunk) {
      currentLine = Number(hunk[1]);
      continue;
    }
    if (currentLine === undefined) {
      continue;
    }
    if (line.startsWith('+')) {
      added.push({line: currentLine, text: line.slice(1)});
      currentLine += 1;
    } else if (line.startsWith('-') || line.startsWith('\\')) {
      continue;
    } else if (line.startsWith(' ')) {
      currentLine += 1;
    }
  }
  return added;
}

function allLines(text) {
  return text.split(/\r?\n/).map((line, index) => ({line: index + 1, text: line}));
}

function scanLinesWithIdentity(file, lines) {
  const findings = [];
  for (const source of lines) {
    for (const rule of prohibitedRules) {
      const allowedLines = reviewedContentExceptions.get(file)?.get(rule.label);
      if (rule.pattern.test(source.text) && !allowedLines?.has(source.text.trim())) {
        findings.push({file, line: source.line, label: rule.label, sourceSha256: sha256(source.text)});
      }
    }
  }
  return findings;
}

export function scanLines(file, lines) {
  return scanLinesWithIdentity(file, lines).map(({file: findingFile, line, label}) => ({
    file: findingFile,
    line,
    label,
  }));
}

function scanPath(file) {
  if (pathPolicyExceptions.has(file)) {
    return [];
  }
  return scanLinesWithIdentity(file, [{line: undefined, text: file}]);
}

async function changedPaths(root, base) {
  const tracked = await git(root, ['diff', '--name-only', '-z', '--no-renames', '--diff-filter=ACMR', base, '--']);
  const untracked = await git(root, ['ls-files', '--others', '--exclude-standard', '-z']);
  const trackedPaths = new Set(nulPaths(tracked.stdout));
  const untrackedPaths = new Set(nulPaths(untracked.stdout));
  const paths = [...new Set([...trackedPaths, ...untrackedPaths])].sort(compareText);
  return {paths, trackedPaths};
}

async function validateRequiredReleaseArtifacts(root) {
  for (const requiredPath of requiredReleaseArtifacts) {
    const {resolved} = safeRelativePath(root, requiredPath);
    let facts;
    try {
      facts = await lstat(resolved);
    } catch {
      fail(`required release artifact is missing or is not a regular file: ${requiredPath}`);
    }
    if (facts.isSymbolicLink() || !facts.isFile()) {
      fail(`required release artifact is missing or is not a regular file: ${requiredPath}`);
    }
  }
}

function hasExactKeys(value, expected) {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) {
    return false;
  }
  const keys = Object.keys(value).sort(compareText);
  const wanted = [...expected].sort(compareText);
  return keys.length === wanted.length && keys.every((key, index) => key === wanted[index]);
}

function findingIdentity({file, line, label, sourceSha256}) {
  return JSON.stringify([file, line ?? null, label, sourceSha256]);
}

function compareFindings(left, right) {
  return (
    compareText(left.file, right.file) ||
    (left.line ?? 0) - (right.line ?? 0) ||
    compareText(left.label, right.label) ||
    compareText(left.sourceSha256, right.sourceSha256)
  );
}

function hasExactKeyOrder(value, expected) {
  return hasExactKeys(value, expected) && Object.keys(value).every((key, index) => key === expected[index]);
}

async function loadEmloForkBaseline(root, base) {
  const {resolved} = safeRelativePath(root, emloForkBaselinePath);
  let facts;
  try {
    facts = await lstat(resolved);
  } catch {
    fail(`required ${emloForkProfile} profile artifact is missing or is not a regular file: ${emloForkBaselinePath}`);
  }
  if (facts.isSymbolicLink() || !facts.isFile()) {
    fail(`required ${emloForkProfile} profile artifact is missing or is not a regular file: ${emloForkBaselinePath}`);
  }
  if (facts.size > 1024 * 1024) {
    fail(`${emloForkProfile} profile baseline exceeds the 1 MiB safety limit`);
  }

  let baseline;
  try {
    baseline = JSON.parse(await readFile(resolved, 'utf8'));
  } catch {
    fail(`${emloForkProfile} profile baseline must be valid JSON`);
  }
  if (!hasExactKeyOrder(baseline, emloForkBaselineKeys)) {
    fail(`${emloForkProfile} profile baseline has an unsupported top-level shape`);
  }
  if (
    baseline.schema !== emloForkBaselineSchema ||
    baseline.profile !== emloForkProfile ||
    baseline.repository !== emloForkRepository ||
    !/^[0-9a-f]{40}$/.test(baseline.sourceCommit) ||
    !Array.isArray(baseline.findings) ||
    baseline.findings.length === 0 ||
    baseline.findings.length > 5000
  ) {
    fail(`${emloForkProfile} profile baseline metadata is invalid`);
  }

  const source = await git(
    root,
    ['rev-parse', '--verify', '--quiet', '--end-of-options', `${baseline.sourceCommit}^{commit}`],
    [0, 1]
  );
  if (source.status !== 0) {
    fail(`${emloForkProfile} profile sourceCommit does not resolve to a commit`);
  }
  const sourceIncludesBase = await git(root, ['merge-base', '--is-ancestor', base, baseline.sourceCommit], [0, 1]);
  if (sourceIncludesBase.status !== 0) {
    fail(`${emloForkProfile} profile sourceCommit does not include the requested scan base`);
  }
  const ancestor = await git(root, ['merge-base', '--is-ancestor', baseline.sourceCommit, 'HEAD'], [0, 1]);
  if (ancestor.status !== 0) {
    fail(`${emloForkProfile} profile sourceCommit is not an ancestor of HEAD`);
  }

  const identities = new Set();
  let previousFinding;
  for (const finding of baseline.findings) {
    if (
      !hasExactKeyOrder(finding, emloForkFindingKeys) ||
      typeof finding.file !== 'string' ||
      (finding.line !== null && (!Number.isSafeInteger(finding.line) || finding.line < 1)) ||
      !prohibitedLabels.has(finding.label) ||
      typeof finding.sourceSha256 !== 'string' ||
      !/^[0-9a-f]{64}$/.test(finding.sourceSha256)
    ) {
      fail(`${emloForkProfile} profile baseline contains an invalid exact finding`);
    }
    safeRelativePath(root, finding.file);
    const identity = findingIdentity(finding);
    if (identities.has(identity)) {
      fail(`${emloForkProfile} profile baseline contains a duplicate exact finding`);
    }
    if (previousFinding !== undefined && compareFindings(previousFinding, finding) >= 0) {
      fail(`${emloForkProfile} profile baseline findings are not in canonical bytewise order`);
    }
    identities.add(identity);
    previousFinding = finding;
  }
  return {findings: baseline.findings, identities};
}

async function validateRepositoryAndBase(cwd, base) {
  const rootResult = await git(cwd, ['rev-parse', '--show-toplevel']);
  const root = rootResult.stdout.toString('utf8').trim();
  const resolved = await git(
    root,
    ['rev-parse', '--verify', '--quiet', '--end-of-options', `${base}^{commit}`],
    [0, 1]
  );
  if (resolved.status !== 0) {
    fail(`base ref does not resolve to a commit: ${base}`);
  }
  const ancestor = await git(root, ['merge-base', '--is-ancestor', base, 'HEAD'], [0, 1]);
  if (ancestor.status !== 0) {
    fail(`base ref is not an ancestor of HEAD: ${base}`);
  }
  await validateRequiredReleaseArtifacts(root);
  return root;
}

export async function scanRepository(cwd, base, profile) {
  const root = await validateRepositoryAndBase(cwd, base);
  const baseline = profile === emloForkProfile ? await loadEmloForkBaseline(root, base) : undefined;
  const {paths, trackedPaths} = await changedPaths(root, base);
  const findings = [];
  let scannedFiles = 0;
  let exceptionFiles = 0;
  let binaryFiles = 0;

  for (const changedPath of paths) {
    const {normalized, resolved} = safeRelativePath(root, changedPath);
    let facts;
    try {
      facts = await lstat(resolved);
    } catch {
      fail(`changed file disappeared during scan: ${normalized}`);
    }
    if (facts.isSymbolicLink()) {
      fail(`refusing to scan symbolic link: ${normalized}`);
    }
    if (!facts.isFile()) {
      fail(`refusing to scan non-file path: ${normalized}`);
    }
    if (policyExceptions.has(normalized)) {
      exceptionFiles += 1;
      continue;
    }
    findings.push(...scanPath(normalized));

    const bytes = await readFile(resolved);
    if (bytes.includes(0)) {
      fail(`refusing to skip NUL-containing changed file: ${normalized}`);
    }
    scannedFiles += 1;
    let lines;
    if (trackedPaths.has(changedPath)) {
      const patch = await git(root, [
        'diff',
        '--unified=0',
        '--no-color',
        '--no-ext-diff',
        '--no-textconv',
        '--no-renames',
        base,
        '--',
        changedPath,
      ]);
      lines = addedLinesFromPatch(patch.stdout.toString('utf8'));
    } else {
      lines = allLines(bytes.toString('utf8'));
    }
    findings.push(...scanLinesWithIdentity(normalized, lines));
  }

  findings.sort(compareFindings);
  if (baseline === undefined) {
    return {
      findings,
      scannedFiles,
      exceptionFiles,
      binaryFiles,
      acceptedFindings: 0,
      unusedBaselineFindings: 0,
    };
  }
  const rejectedFindings = [];
  const matchedIdentities = new Set();
  for (const finding of findings) {
    const identity = findingIdentity(finding);
    if (baseline.identities.has(identity)) {
      matchedIdentities.add(identity);
    } else {
      rejectedFindings.push(finding);
    }
  }
  return {
    findings: rejectedFindings,
    scannedFiles,
    exceptionFiles,
    binaryFiles,
    acceptedFindings: matchedIdentities.size,
    unusedBaselineFindings: baseline.findings.length - matchedIdentities.size,
  };
}

async function main() {
  const {base, profile} = parseArgs(process.argv.slice(2));
  const report = await scanRepository(process.cwd(), base, profile);
  if (report.findings.length > 0 || report.unusedBaselineFindings > 0) {
    for (const finding of report.findings) {
      if (finding.line === undefined) {
        process.stderr.write(`${finding.file}: prohibited ${finding.label} in path\n`);
      } else {
        process.stderr.write(`${finding.file}:${finding.line}: prohibited ${finding.label}\n`);
      }
    }
    if (report.unusedBaselineFindings > 0) {
      process.stderr.write(
        `${emloForkProfile} adopter-term baseline failed: ${plural(
          report.unusedBaselineFindings,
          'stale, unused, or forged exact finding'
        )}.\n`
      );
    }
    if (report.findings.length > 0) {
      process.stderr.write(
        `Adopter-term scan failed: ${plural(report.findings.length, 'finding')} in ${plural(report.scannedFiles, 'scanned file')}.\n`
      );
    } else {
      process.stderr.write(
        `Adopter-term scan failed: the named-fork baseline did not exactly match findings in ${plural(
          report.scannedFiles,
          'scanned file'
        )}.\n`
      );
    }
    process.exitCode = 1;
    return;
  }
  if (profile === emloForkProfile) {
    process.stdout.write(
      `${emloForkProfile} adopter-term profile accepted ${plural(report.acceptedFindings, 'exact reviewed finding')}. ` +
        'This result is valid only for the named custom fork and is not a community or upstream release result.\n'
    );
  }
  process.stdout.write(
    `Adopter-term scan passed: ${plural(report.scannedFiles, 'file')} scanned, ${plural(report.exceptionFiles, 'policy exception')}, ${plural(report.binaryFiles, 'binary file')} skipped.\n`
  );
}

const invokedPath = process.argv[1] ? path.resolve(process.argv[1]) : '';
if (invokedPath === fileURLToPath(import.meta.url)) {
  main().catch((error) => {
    process.stderr.write(`${error.message}\n`);
    process.exitCode = 1;
  });
}
