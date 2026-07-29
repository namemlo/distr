#!/usr/bin/env node

import {spawn} from 'node:child_process';
import {lstat, readFile} from 'node:fs/promises';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

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

function fail(message) {
  throw new Error(message);
}

function compareText(left, right) {
  return left < right ? -1 : left > right ? 1 : 0;
}

function plural(count, singular, pluralForm = `${singular}s`) {
  return `${count} ${count === 1 ? singular : pluralForm}`;
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
  if (argv.length !== 2 || argv[0] !== '--base') {
    fail('usage: node hack/control-plane-adopter-term-scan.mjs --base <git-ref>');
  }
  return {base: validateBaseRef(argv[1])};
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

export function scanLines(file, lines) {
  const findings = [];
  for (const source of lines) {
    for (const rule of prohibitedRules) {
      const allowedLines = reviewedContentExceptions.get(file)?.get(rule.label);
      if (rule.pattern.test(source.text) && !allowedLines?.has(source.text.trim())) {
        findings.push({file, line: source.line, label: rule.label});
      }
    }
  }
  return findings;
}

function scanPath(file) {
  if (pathPolicyExceptions.has(file)) {
    return [];
  }
  return scanLines(file, [{line: undefined, text: file}]);
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

export async function scanRepository(cwd, base) {
  const root = await validateRepositoryAndBase(cwd, base);
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
    findings.push(...scanLines(normalized, lines));
  }

  findings.sort((left, right) => {
    return (
      compareText(left.file, right.file) || (left.line ?? 0) - (right.line ?? 0) || compareText(left.label, right.label)
    );
  });
  return {findings, scannedFiles, exceptionFiles, binaryFiles};
}

async function main() {
  const {base} = parseArgs(process.argv.slice(2));
  const report = await scanRepository(process.cwd(), base);
  if (report.findings.length > 0) {
    for (const finding of report.findings) {
      if (finding.line === undefined) {
        process.stderr.write(`${finding.file}: prohibited ${finding.label} in path\n`);
      } else {
        process.stderr.write(`${finding.file}:${finding.line}: prohibited ${finding.label}\n`);
      }
    }
    process.stderr.write(
      `Adopter-term scan failed: ${plural(report.findings.length, 'finding')} in ${plural(report.scannedFiles, 'scanned file')}.\n`
    );
    process.exitCode = 1;
    return;
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
