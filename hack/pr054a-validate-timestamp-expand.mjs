#!/usr/bin/env node
import {spawnSync} from 'node:child_process';
import {existsSync, readFileSync, statSync} from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

const root = fileURLToPath(new URL('..', import.meta.url));
const read = (name) => readFileSync(path.join(root, name), 'utf8');
const fail = (message) => {
  throw new Error(message);
};

const privacyFindingLimit = 100;
const privacyGitBufferLimit = 64 * 1024 * 1024;
const privacyLineLimit = 1024 * 1024;
const privacyCommitLimit = 10000;
const privacyEdgeLimit = 20000;
const privacyPathLimit = 100000;
const privacyObjectPattern = /^[0-9a-f]{40}$/i;
const privacyReservedEmailSuffixes = ['test', 'example', 'invalid', 'localhost'];
const privacyExampleEmailDomains = ['example.com', 'example.net', 'example.org'];
const privacyDecoder = new TextDecoder('utf-8', {fatal: true});
const privacyGit = (args, input) => {
  const result = spawnSync('git', args, {
    cwd: process.cwd(),
    encoding: null,
    env: {...process.env, GIT_NO_REPLACE_OBJECTS: '1', GIT_OPTIONAL_LOCKS: '0'},
    input,
    maxBuffer: privacyGitBufferLimit,
    windowsHide: true,
  });
  if (result.error || result.status !== 0 || !Buffer.isBuffer(result.stdout)) {
    throw new Error('git operation');
  }
  return result.stdout;
};
const privacyDecode = (buffer) => {
  try {
    return privacyDecoder.decode(buffer);
  } catch {
    throw new Error('invalid UTF-8');
  }
};
const privacyGitText = (args, input) => privacyDecode(privacyGit(args, input));
const privacyObjectType = (object) => privacyGitText(['cat-file', '-t', object]).trim();
const privacyEmailIsReserved = (domain) => {
  const normalized = domain.toLowerCase();
  if (privacyReservedEmailSuffixes.some((suffix) => normalized === suffix || normalized.endsWith(`.${suffix}`))) {
    return true;
  }
  return privacyExampleEmailDomains.some(
    (candidate) => normalized === candidate || normalized.endsWith(`.${candidate}`)
  );
};
const privacyContainsNonReservedEmail = (text) => {
  const label = '[A-Z0-9](?:[A-Z0-9-]{0,61}[A-Z0-9])?';
  const candidate = new RegExp(
    `(?<![A-Z0-9.!#$%&'*+/=?^_\\x60{|}~-])[A-Z0-9.!#$%&'*+/=?^_\\x60{|}~-]+@(${label}(?:\\.${label})*)(?![A-Z0-9-])`,
    'gi'
  );
  for (const match of text.matchAll(candidate)) {
    const domain = match[1];
    const localPart = match[0].slice(0, match[0].lastIndexOf('@'));
    if (!domain.includes('.') && !/^[A-Z0-9][A-Z0-9._+-]*$/i.test(localPart)) continue;
    if (privacyIPv4IsValid(domain.split('.'))) continue;
    if (!privacyEmailIsReserved(domain)) return true;
  }
  return false;
};
const privacyContainsUserHome = (text) => {
  const windowsHome = /(?:^|[^A-Za-z0-9])(?:[A-Za-z]:[\\/]Users[\\/][^\\/\s"'`<>|?*]+)/i;
  const posixHome = /(?:^|[^A-Za-z0-9/])\/(?:home|Users)\/[^/\s"'`<>]+/;
  const rootHome = /(?:^|[^A-Za-z0-9/])\/root(?:\/|(?=$|[\s"'`)>}\]]))/;
  const fileHome = /\bfile:\/\/\/(?:home|Users)\/[^/\s"'`<>]+/i;
  const fileRoot = /\bfile:\/\/\/root(?:\/|(?=$|[\s"'`)]))/i;
  const backslashUnc = /(?:^|[^A-Za-z0-9\\])\\\\[^\\/\s"'`<>]+[\\/][^\\/\s"'`<>]+/;
  const forwardUnc = /(?:^|[^A-Za-z0-9\\/:])\/\/(?!\/)[^\\/\s"'`<>]+[\\/][^\\/\s"'`<>]+/;
  const fileUnc = /\bfile:\/\/(?!\/)[^\\/\s"'`<>]+\/[^\\/\s"'`<>]+/i;
  const extendedUnc = /(?:^|[^A-Za-z0-9\\])\\\\\?\\UNC\\[^\\/\s"'`<>]+\\[^\\/\s"'`<>]+/i;
  return (
    windowsHome.test(text) ||
    posixHome.test(text) ||
    rootHome.test(text) ||
    fileHome.test(text) ||
    fileRoot.test(text) ||
    backslashUnc.test(text) ||
    forwardUnc.test(text) ||
    fileUnc.test(text) ||
    extendedUnc.test(text)
  );
};
const privacyIPv4IsValid = (parts) =>
  parts.length === 4 && parts.every((part) => /^\d{1,3}$/.test(part) && Number(part) <= 255);
const privacyIPv4IsSafe = ([first, second, third, fourth]) =>
  first === 127 ||
  (first === 0 && second === 0 && third === 0 && fourth === 0) ||
  (first === 192 && second === 0 && third === 2) ||
  (first === 198 && second === 51 && third === 100) ||
  (first === 203 && second === 0 && third === 113);
const privacyContainsAddressIPv4 = (text) => {
  const candidate = /(?<![\d.])(?:\d{1,3}\.){3}\d{1,3}(?![\d.])/g;
  for (const match of text.matchAll(candidate)) {
    const parts = match[0].split('.');
    if (!privacyIPv4IsValid(parts)) continue;
    const octets = parts.map(Number);
    const before = text.slice(0, match.index);
    const compoundAddressKey =
      /(?:^|[^A-Za-z0-9])(?:[A-Za-z0-9_-]*(?:address|addr|endpoint|hostname|host|private|public|server|url|listen|bind|connect|dsn)[A-Za-z0-9_-]*|ip)\s*[:=]/i.test(
        before
      );
    const camelCaseIpKey = /(?:^|[^A-Za-z0-9])(?:[A-Za-z][A-Za-z0-9]*(?:Ip|IP)|Ip[A-Z][A-Za-z0-9]*)\s*[:=]\s*$/.test(
      before
    );
    const emailHostContext = /@\[?$/.test(before);
    const addressContext =
      /\b(?:address|addr|endpoint|host|ip|private|public|server|url|listen|bind|connect|dsn)\b/i.test(text) ||
      compoundAddressKey ||
      camelCaseIpKey ||
      emailHostContext ||
      /(?:https?|postgres(?:ql)?|ssh|tcp):\/\//i.test(text);
    const explicitVersionContext =
      /(?:^|[^A-Za-z0-9])(?:version|release(?:\s+version)?|tool(?:\s+version)?|image(?:[\s_-]+(?:tag|version))?)[\s:=@_-]*(?:v)?\s*$/i.test(
        before
      );
    if (!addressContext && explicitVersionContext) continue;
    if (!privacyIPv4IsSafe(octets)) return true;
  }
  return false;
};
const privacyContainsPrivateToken = (text, privateTokens) => {
  if (privateTokens.length === 0) return false;
  const normalized = text.toLowerCase();
  return privateTokens.some((token) => normalized.includes(token));
};
const privacyCategories = (text, privateTokens) => {
  if (text.length > privacyLineLimit) throw new Error('line limit');
  const categories = [];
  if (privacyContainsUserHome(text)) categories.push('user-home-path');
  if (privacyContainsNonReservedEmail(text)) categories.push('non-reserved-email');
  if (privacyContainsAddressIPv4(text)) categories.push('literal-ipv4');
  if (privacyContainsPrivateToken(text, privateTokens)) categories.push('private-scope');
  return categories;
};
const privacyPathCategories = (candidatePath, privateTokens) => {
  const categories = privacyCategories(candidatePath, privateTokens);
  if (/(?:^|\/)(?:home|Users)\/[^/]+/.test(candidatePath) && !categories.includes('user-home-path')) {
    categories.push('user-home-path');
  }
  return categories;
};
const privacyValidatePath = (candidate) => {
  if (
    candidate.length === 0 ||
    candidate.length > 4096 ||
    /[\x00-\x1f\x7f]/.test(candidate) ||
    candidate.includes('\\0') ||
    path.posix.isAbsolute(candidate)
  ) {
    throw new Error('unsafe path');
  }
  const normalized = path.posix.normalize(candidate);
  if (normalized !== candidate || normalized === '..' || normalized.startsWith('../')) {
    throw new Error('unsafe path');
  }
};
const privacyNulFields = (buffer) => {
  const text = privacyDecode(buffer);
  const fields = text.split('\0');
  if (fields.at(-1) !== '') throw new Error('malformed NUL output');
  fields.pop();
  return fields;
};
const privacyAddedLines = (base, head, candidatePath) => {
  const patchText = privacyGitText([
    '--literal-pathspecs',
    'diff',
    '--no-ext-diff',
    '--no-textconv',
    '--no-color',
    '--no-renames',
    '--text',
    '--unified=0',
    base,
    head,
    '--',
    candidatePath,
  ]);
  const lines = patchText.split(/\n/);
  const added = [];
  let nextLine = null;
  for (const line of lines) {
    const hunk = line.match(/^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@/);
    if (hunk) {
      nextLine = Number(hunk[1]);
      continue;
    }
    if (nextLine === null) continue;
    if (line.startsWith('+')) {
      added.push({line: nextLine, text: line.slice(1).replace(/\r$/, '')});
      nextLine += 1;
      continue;
    }
    if (line.startsWith('-') || line === '\\ No newline at end of file') continue;
    if (line.startsWith(' ')) {
      nextLine += 1;
      continue;
    }
    if (line.startsWith('diff --git ')) nextLine = null;
  }
  return added;
};
const privacyDiffRecords = (base, head) => {
  const fields = privacyNulFields(
    privacyGit([
      '--literal-pathspecs',
      'diff',
      '--no-ext-diff',
      '--no-textconv',
      '--no-color',
      '--no-renames',
      '--raw',
      '--abbrev=40',
      '-z',
      '--diff-filter=ACDMRTUXB',
      base,
      head,
      '--',
    ])
  );
  if (fields.length % 2 !== 0) throw new Error('malformed raw diff');
  const records = [];
  const seen = new Set();
  for (let index = 0; index < fields.length; index += 2) {
    const header = fields[index];
    const candidatePath = fields[index + 1];
    const match = header.match(/^:(\d{6}) (\d{6}) ([0-9a-f]{40}) ([0-9a-f]{40}) ([ACDMRTUXB])$/i);
    if (!match) throw new Error('malformed raw diff');
    const [, oldMode, newMode, oldObject, newObject, rawStatus] = match;
    const status = rawStatus.toUpperCase();
    if (status === 'C' || status === 'R') throw new Error('unexpected rename');
    if (![oldMode, newMode].every((mode) => /^(?:000000|100644|100755|120000|160000)$/.test(mode))) {
      throw new Error('unsupported mode');
    }
    privacyValidatePath(candidatePath);
    if (seen.has(candidatePath)) throw new Error('duplicate path');
    seen.add(candidatePath);
    records.push({candidatePath, newMode, newObject: newObject.toLowerCase(), oldMode, oldObject, status});
    if (records.length > privacyPathLimit) throw new Error('path limit');
  }
  return records;
};
const privacyRegularModes = new Set(['100644', '100755']);
const privacyBlobBinaryCache = new Map();
const privacyBlobIsBinary = (object) => {
  if (privacyBlobBinaryCache.has(object)) return privacyBlobBinaryCache.get(object);
  const contents = privacyGit(['cat-file', 'blob', object]);
  let binary = contents.includes(0);
  if (!binary) {
    try {
      privacyDecoder.decode(contents);
    } catch {
      binary = true;
    }
  }
  privacyBlobBinaryCache.set(object, binary);
  return binary;
};
const privacyCommitRange = (base, head, baseIsEmptyTree) => {
  const range = baseIsEmptyTree ? head : `${base}..${head}`;
  const output = privacyGitText(['rev-list', '--reverse', '--topo-order', range]).trim();
  if (output === '') return [];
  const commits = output.split(/\r?\n/);
  if (commits.length > privacyCommitLimit || commits.some((commit) => !privacyObjectPattern.test(commit))) {
    throw new Error('commit list');
  }
  if (new Set(commits.map((commit) => commit.toLowerCase())).size !== commits.length) {
    throw new Error('commit list');
  }
  return commits.map((commit) => commit.toLowerCase());
};
const privacyCommitParents = (commit) => {
  const output = privacyGitText(['rev-list', '--parents', '-n', '1', commit]).trim();
  const objects = output.split(/\s+/);
  if (
    objects.length === 0 ||
    objects[0].toLowerCase() !== commit.toLowerCase() ||
    objects.some((object) => !privacyObjectPattern.test(object))
  ) {
    throw new Error('commit parents');
  }
  return objects.slice(1).map((parent) => parent.toLowerCase());
};
const privacyReadSymlinkTarget = (object) => {
  const target = privacyGitText(['cat-file', 'blob', object]);
  if (target.length === 0 || target.length > 4096 || /[\x00-\x1f\x7f]/.test(target)) {
    throw new Error('unsafe symlink target');
  }
  return target;
};
const privacyRunDiffScan = (base, head, requirePrivate) => {
  const legacyPrivatePattern = process.env.DISTR_PRIVATE_SCOPE_PATTERN?.trim() ?? '';
  const privateTokenSource = process.env.DISTR_PRIVATE_SCOPE_TOKENS ?? '';
  const privateCanary = process.env.DISTR_PRIVATE_SCOPE_CANARY?.trim() ?? '';
  if (legacyPrivatePattern !== '') {
    console.error('privacy scan configuration error: legacy private scope pattern is unsupported');
    return 1;
  }
  const privateTokenLines = privateTokenSource
    .split(/\r?\n/)
    .map((token) => token.trim())
    .filter((token) => token !== '');
  if (privateTokenLines.length === 0) {
    if (requirePrivate || privateTokenSource.trim() !== '' || privateCanary !== '') {
      console.error('privacy scan configuration error: private scope tokens are required');
      return 1;
    }
  }
  if (
    privateTokenLines.length > 100 ||
    privateTokenLines.some((token) => token.length < 3 || token.length > 256 || /[\x00-\x1f\x7f]/.test(token)) ||
    privateTokenLines.reduce((total, token) => total + token.length, 0) > 8192
  ) {
    console.error('privacy scan configuration error: private scope tokens are invalid');
    return 1;
  }
  const privateTokens = [...new Set(privateTokenLines.map((token) => token.toLowerCase()))];
  if (privateTokens.length > 0 && privateCanary === '') {
    console.error('privacy scan configuration error: private scope canary is required');
    return 1;
  }
  if (
    privateCanary.length > 512 ||
    /[\x00-\x1f\x7f]/.test(privateCanary) ||
    (privateTokens.length > 0 && !privacyContainsPrivateToken(privateCanary, privateTokens))
  ) {
    console.error('privacy scan configuration error: private scope canary validation failed');
    return 1;
  }
  if (!privacyObjectPattern.test(base) || !privacyObjectPattern.test(head)) {
    console.error('privacy scan failed closed: invalid object');
    return 1;
  }
  try {
    const normalizedBase = base.toLowerCase();
    const normalizedHead = head.toLowerCase();
    const repositoryRoot = privacyGitText(['rev-parse', '--show-toplevel']).trim();
    if (path.resolve(repositoryRoot) !== path.resolve(process.cwd())) throw new Error('repository root mismatch');
    if (privacyObjectType(normalizedHead) !== 'commit') throw new Error('invalid head');
    const emptyTree = privacyGitText(['hash-object', '-t', 'tree', '--stdin'], Buffer.alloc(0)).trim();
    const normalizedEmptyTree = emptyTree.toLowerCase();
    const baseType = privacyObjectType(normalizedBase);
    const baseIsEmptyTree = baseType === 'tree' && normalizedBase === normalizedEmptyTree;
    if (baseType !== 'commit' && !baseIsEmptyTree) {
      throw new Error('invalid base');
    }
    const findings = [];
    const findingKeys = new Set();
    let findingOverflow = false;
    const addFinding = (category, candidatePath, line, redactPath = false) => {
      const outputPath = redactPath ? '<redacted>' : candidatePath;
      const key = `${category}\0${outputPath}\0${line}`;
      if (findingKeys.has(key)) return;
      findingKeys.add(key);
      if (findings.length >= privacyFindingLimit) {
        findingOverflow = true;
        return;
      }
      findings.push({category, path: outputPath, line});
    };
    const scanDiff = (diffBase, diffHead) => {
      const records = privacyDiffRecords(diffBase, diffHead);
      for (const record of records) {
        const {candidatePath, newMode, newObject, oldMode, oldObject, status} = record;
        const pathCategories = privacyPathCategories(candidatePath, privateTokens);
        const redactPath = pathCategories.length > 0;
        if (status === 'A') {
          for (const category of pathCategories) addFinding(category, candidatePath, 0, true);
        }
        if (oldMode === '160000' || newMode === '160000') {
          if (oldMode === '160000' && privacyObjectType(oldObject) !== 'commit') throw new Error('gitlink object');
          if (newMode === '160000' && privacyObjectType(newObject) !== 'commit') throw new Error('gitlink object');
          addFinding('gitlink-change', candidatePath, 0, redactPath);
          continue;
        }
        const oldBinary = privacyRegularModes.has(oldMode) && privacyBlobIsBinary(oldObject);
        const newBinary = privacyRegularModes.has(newMode) && privacyBlobIsBinary(newObject);
        if (oldBinary || newBinary) {
          addFinding(status === 'A' ? 'binary-addition' : 'binary-modification', candidatePath, 0, redactPath);
          continue;
        }
        if (newMode === '000000') continue;
        if (newMode === '120000') {
          const target = privacyReadSymlinkTarget(newObject);
          for (const [lineIndex, targetLine] of target.split(/\r?\n/).entries()) {
            for (const category of privacyCategories(targetLine, privateTokens)) {
              addFinding(category, candidatePath, lineIndex + 1, redactPath);
            }
          }
          continue;
        }
        for (const added of privacyAddedLines(diffBase, diffHead, candidatePath)) {
          for (const category of privacyCategories(added.text, privateTokens)) {
            addFinding(category, candidatePath, added.line, redactPath);
          }
        }
      }
    };
    const commits = privacyCommitRange(normalizedBase, normalizedHead, baseIsEmptyTree);
    const scannedEdges = new Set();
    for (const commit of commits) {
      if (privacyObjectType(commit) !== 'commit') throw new Error('commit list');
      const parents = privacyCommitParents(commit);
      if (parents.length === 0) {
        const edge = `${normalizedEmptyTree}\0${commit}`;
        if (!scannedEdges.has(edge)) {
          scannedEdges.add(edge);
          scanDiff(normalizedEmptyTree, commit);
        }
      } else {
        for (const parent of parents) {
          if (privacyObjectType(parent) !== 'commit') throw new Error('commit parents');
          const edge = `${parent}\0${commit}`;
          if (scannedEdges.has(edge)) continue;
          scannedEdges.add(edge);
          if (scannedEdges.size > privacyEdgeLimit) throw new Error('edge limit');
          scanDiff(parent, commit);
        }
      }
    }
    scanDiff(normalizedBase, normalizedHead);
    if (findings.length > 0) {
      findings.sort(
        (left, right) =>
          left.path.localeCompare(right.path) || left.line - right.line || left.category.localeCompare(right.category)
      );
      for (const finding of findings) {
        console.error(
          `privacy finding: category=${finding.category} path=${JSON.stringify(finding.path)} line=${finding.line}`
        );
      }
      if (findingOverflow) console.error('privacy scan failed closed: finding limit exceeded');
      return 1;
    }
    console.log('PR-054A privacy diff scan passed');
    return 0;
  } catch (error) {
    const safeReasons = new Set([
      'finding limit',
      'git operation',
      'invalid UTF-8',
      'invalid base',
      'invalid head',
      'commit list',
      'commit parents',
      'duplicate path',
      'edge limit',
      'gitlink object',
      'line limit',
      'malformed NUL output',
      'malformed raw diff',
      'path limit',
      'repository root mismatch',
      'unexpected rename',
      'unsupported mode',
      'unsafe path',
      'unsafe symlink target',
    ]);
    const reason = error instanceof Error && safeReasons.has(error.message) ? error.message : 'internal error';
    console.error(`privacy scan failed closed: repository diff could not be scanned (${reason})`);
    return 1;
  }
};
const privacyArgs = process.argv.slice(2);
if (privacyArgs.length > 0) {
  const requirePrivate = privacyArgs[3] === '--require-private-scope';
  if (
    privacyArgs[0] !== '--scan-privacy-diff' ||
    ![3, 4].includes(privacyArgs.length) ||
    (privacyArgs.length === 4 && !requirePrivate)
  ) {
    console.error(
      'usage: pr054a-validate-timestamp-expand.mjs --scan-privacy-diff <base-object> <head-commit> [--require-private-scope]'
    );
    process.exit(1);
  }
  process.exit(privacyRunDiffScan(privacyArgs[1], privacyArgs[2], requirePrivate));
}

const plans = [
  'docs/superpowers/plans/2026-07-14-control-plane-foundations.md',
  'docs/superpowers/plans/2026-07-14-control-plane-governance-execution.md',
  'docs/superpowers/plans/2026-07-14-control-plane-operator-adoption.md',
];
const expectedMigrations = new Map([
  [56, 139],
  [57, 140],
  [58, 141],
  [59, 142],
  [60, 143],
  [62, 144],
  [63, 145],
  [64, 146],
  [65, 147],
  [66, 148],
  [67, 149],
  [68, 150],
  [69, 151],
  [70, 152],
  [71, 153],
  [72, 154],
  [73, 155],
  [74, 156],
  [75, 157],
  [76, 158],
  [77, 159],
  [78, 160],
  [79, 161],
  [82, 162],
]);
const expectedADRs = new Map([
  [56, '0056'],
  [58, '0057'],
  [60, '0058'],
  [62, '0059'],
  [63, '0060'],
  [66, '0061'],
  [69, '0062'],
  [71, '0063'],
  [75, '0064'],
  [77, '0065'],
  [78, '0066'],
  [79, '0067'],
  [82, '0068'],
]);

const expectedPRs = Array.from({length: 29}, (_, index) => 55 + index);
const sameValues = (actual, expected) =>
  actual.length === expected.length && actual.every((value, index) => value === expected[index]);
const describe = (values) => (values.length === 0 ? 'none' : values.join(','));
const sectionLines = (text, heading, isEnd) => {
  const lines = text.split(/\r?\n/);
  const starts = lines.flatMap((line, index) => (line === heading ? [index] : []));
  if (starts.length !== 1) return null;
  const start = starts[0];
  const endOffset = lines.slice(start + 1).findIndex(isEnd);
  const end = endOffset === -1 ? lines.length : start + 1 + endOffset;
  return lines.slice(start, end);
};
const filesSectionLines = (block, file, pr) => {
  const lines = block.split(/\r?\n/);
  const starts = lines.flatMap((line, index) => (line === '**Files:**' ? [index] : []));
  if (starts.length !== 1) fail(`${file}: PR-${pr} must contain exactly one **Files:** section`);
  const afterHeading = starts[0] + 1;
  const endOffset = lines.slice(afterHeading).findIndex((line) => /^\*\*[^*]+\*\*$/.test(line));
  const end = endOffset === -1 ? lines.length : afterHeading + endOffset;
  const section = lines.slice(afterHeading, end);
  if (!section.some((line) => /^- (Create|Modify):/.test(line))) {
    fail(`${file}: PR-${pr} has an empty **Files:** section`);
  }
  return section;
};
const allocationDeclarations = (lines, file, pr) => {
  const migrations = [];
  const adrs = [];
  for (const line of lines) {
    const candidate = line.trimStart();
    if (!candidate.startsWith('- ')) continue;
    if (candidate !== line && (candidate.includes('internal/migrations/sql/') || /docs\/adr\/\d/.test(candidate))) {
      fail(`${file}: PR-${pr} malformed allocation declaration: ${line}`);
    }
    const declaration = candidate.match(/^- (Create|Modify): `([^`]+)`$/);
    if (!declaration) {
      if (line.includes('internal/migrations/sql/') || /docs\/adr\/\d/.test(line)) {
        fail(`${file}: PR-${pr} malformed allocation declaration: ${line}`);
      }
      continue;
    }
    const [, action, declaredPath] = declaration;
    if (declaredPath.startsWith('internal/migrations/sql/')) {
      const migration = declaredPath.match(/^internal\/migrations\/sql\/(\d+)_(.+)\.(up|down)\.sql$/);
      if (!migration) fail(`${file}: PR-${pr} malformed migration declaration: ${line}`);
      migrations.push({
        action,
        id: migration[1],
        stem: migration[2],
        direction: migration[3],
      });
      continue;
    }
    if (/^docs\/adr\/\d/.test(declaredPath)) {
      const adr = declaredPath.match(/^docs\/adr\/(\d+)-[^/]+\.md$/);
      if (!adr) fail(`${file}: PR-${pr} malformed ADR declaration: ${line}`);
      adrs.push({action, id: adr[1]});
    }
  }
  return {migrations, adrs};
};
const describeMigrations = (declarations) =>
  declarations.length === 0
    ? 'none'
    : declarations
        .map(({id, action, direction}) => `${id}:${action}:${direction}`)
        .sort((left, right) => left.localeCompare(right))
        .join(',');

const seenPRs = new Map();

for (const file of plans) {
  const text = read(file);
  const blocks = text.split(/^## Task /m).slice(1);
  for (const block of blocks) {
    const heading = block.match(/^(\d+): PR-(\d{3})(?= — )/);
    if (!heading) continue;
    const pr = Number(heading[2]);
    if (!expectedPRs.includes(pr)) continue;
    const location = `${file}: Task ${heading[1]}`;
    if (seenPRs.has(pr)) fail(`duplicate PR-${pr} allocation block: ${seenPRs.get(pr)} and ${location}`);
    seenPRs.set(pr, location);

    const {migrations, adrs} = allocationDeclarations(filesSectionLines(block, file, pr), file, pr);
    const expectedMigration = expectedMigrations.get(pr);
    if (expectedMigration === undefined) {
      if (migrations.length !== 0) {
        fail(
          `${file}: PR-${pr} migration declarations mismatch: expected none, found ${describeMigrations(migrations)}`
        );
      }
    } else {
      const directions = migrations.map(({direction}) => direction).sort();
      const paired =
        migrations.length === 2 &&
        migrations.every(({id}) => id === String(expectedMigration)) &&
        sameValues(directions, ['down', 'up']) &&
        migrations.every(({action}) => action === 'Create') &&
        migrations[0].stem === migrations[1].stem;
      if (!paired) {
        fail(
          `${file}: PR-${pr} migration declarations mismatch: expected paired ${expectedMigration} up/down, found ${describeMigrations(migrations)}`
        );
      }
    }

    const declaredADRs = adrs.map(({action, id}) => `${id}:${action}`).sort((left, right) => left.localeCompare(right));
    const expectedADR = expectedADRs.has(pr) ? [`${expectedADRs.get(pr)}:Create`] : [];
    if (!sameValues(declaredADRs, expectedADR)) {
      fail(
        `${file}: PR-${pr} ADR declarations mismatch: expected ${describe(expectedADR)}, found ${describe(declaredADRs)}`
      );
    }
  }
}

const missingPRs = expectedPRs.filter((pr) => !seenPRs.has(pr));
if (missingPRs.length > 0) {
  fail(`missing expected PR allocation blocks: ${missingPRs.map((pr) => `PR-${pr}`).join(', ')}`);
}

const forkDoc = 'docs/fork/PR-054A_EXTERNAL_EXECUTION_TIMESTAMP_EXPAND.md';
if (!existsSync(path.join(root, forkDoc))) fail(`missing ${forkDoc}`);
const decisionLines = [
  '- Physical expand migration: migration 138.',
  '- Architecture decision: ADR-0055.',
  '- Durable zero-history proof: `ExternalExecutionTimestampExpandState`.',
  '- Logical contract migration: the next unused contiguous number only when the contract release is shippable; 163 is conditional on every currently planned migration landing first.',
];
const forkDocLines = read(forkDoc).split(/\r?\n/);
for (const decision of decisionLines) {
  if (forkDocLines.filter((line) => line === decision).length !== 1) {
    fail(`${forkDoc}: expected exact decision line: ${decision}`);
  }
}

const evidenceIndexBlock = sectionLines(read(forkDoc), '## Evidence Index', (line) => line.startsWith('## '));
if (!evidenceIndexBlock) fail('PR-054A evidence index missing');
const expectedEvidenceTable = [
  '| Evidence                       | Source                                                                                                                            |',
  '| ------------------------------ | --------------------------------------------------------------------------------------------------------------------------------- |',
  '| Accepted architecture decision | [`ADR-0055`](../adr/0055-external-execution-timestamp-instants.md)                                                                |',
  '| Approved hybrid design         | [`External-execution TIMESTAMPTZ hybrid design`](../superpowers/specs/2026-07-15-external-execution-timestamptz-hybrid-design.md) |',
  '| Implementation plan            | [`External-execution timestamp expand`](../superpowers/plans/2026-07-15-external-execution-timestamp-expand.md)                   |',
  '| Extension allocation ledger    | [`Enterprise operator control-plane program`](../superpowers/plans/2026-07-14-enterprise-operator-control-plane-program.md)       |',
  '| Deterministic allocation check | [`pr054a-validate-timestamp-expand.mjs`](../../hack/pr054a-validate-timestamp-expand.mjs)                                         |',
];
const evidenceTable = evidenceIndexBlock.slice(1).filter((line) => line.trim() !== '');
if (!sameValues(evidenceTable, expectedEvidenceTable)) {
  fail('PR-054A evidence index mismatch');
}
const evidenceLinks = evidenceTable.flatMap((line) =>
  [...line.matchAll(/\[[^\]]+\]\(([^)]+)\)/g)].map((match) => match[1])
);
const forkDocDirectory = path.dirname(path.join(root, forkDoc));
for (const target of evidenceLinks) {
  if (path.isAbsolute(target) || /^[a-z][a-z0-9+.-]*:/i.test(target) || target.startsWith('#')) {
    fail(`PR-054A evidence link is not repository-relative: ${target}`);
  }
  const resolved = path.resolve(forkDocDirectory, target);
  const relative = path.relative(root, resolved);
  if (relative === '..' || relative.startsWith(`..${path.sep}`) || path.isAbsolute(relative)) {
    fail(`PR-054A evidence link escapes repository: ${target}`);
  }
  if (!existsSync(resolved) || !statSync(resolved).isFile()) {
    fail(`PR-054A evidence link target missing: ${target}`);
  }
}

const forkIndexHeading = '### PR-054A - External-execution timestamp expand';
const forkIndexBlock = sectionLines(read('docs/fork/FORK_DIFF_INDEX.md'), forkIndexHeading, (line) =>
  line.startsWith('### ')
);
if (!forkIndexBlock) fail('fork index missing structured PR-054A entry');
for (const required of [
  '- Status: Implemented locally; CI matrix configured; independent review and real PostgreSQL 16.14/18.4',
  '- Database changes: Migration 138 adds nullable instant shadows, paired future defaults, future indexes, immutable',
  '- Documentation: Added ADR-0055, the approved hybrid design, fenced Compose procedure, release/upgrade/smoke/security',
  '  service-container legs remain pending Task 11.',
  '- Tests: Local non-database validators, migration-pair validation, and the Compose orchestration harness passed. The',
  '  focused CI matrix is configured for pinned PostgreSQL 16.14 and 18.4; both real service-container legs remain',
  '  pending Task 11.',
]) {
  if (!forkIndexBlock.includes(required)) fail(`fork index PR-054A entry mismatch: missing ${required}`);
}

const masterRoadmapHeading = '### Accepted post-PR-050 extension ledger';
const masterRoadmapBlock = sectionLines(
  read('docs/roadmaps/DISTR_COMMUNITY_FORK_MASTER_PLAN.md'),
  masterRoadmapHeading,
  (line) => line === '---' || line.startsWith('## ')
);
if (!masterRoadmapBlock) fail('master roadmap missing structured extension ledger');
for (const required of [
  '[enterprise operator control-plane program](../superpowers/plans/2026-07-14-enterprise-operator-control-plane-program.md).',
  '[PR-054A](../fork/PR-054A_EXTERNAL_EXECUTION_TIMESTAMP_EXPAND.md).',
]) {
  if (!masterRoadmapBlock.includes(required)) fail(`master roadmap PR-054A ledger mismatch: missing ${required}`);
}
const expectedMasterLedger = [
  '| Extension slice       | Migration allocation | ADR allocation    |',
  '| --------------------- | -------------------- | ----------------- |',
  '| PR-054A               | 138                  | 0055              |',
  '| PR-055                | None                 | None              |',
  '| PR-056 through PR-065 | 139 through 147      | 0056 through 0060 |',
  '| PR-066 through PR-078 | 148 through 160      | 0061 through 0066 |',
  '| PR-079                | 161                  | 0067              |',
  '| PR-080 through PR-081 | None                 | None              |',
  '| PR-082                | 162                  | 0068              |',
  '| PR-083                | None                 | None              |',
];
const masterLedger = masterRoadmapBlock.filter((line) => line.trimStart().startsWith('|'));
if (!sameValues(masterLedger, expectedMasterLedger)) {
  fail('master roadmap allocation ledger mismatch');
}

const requireSnippets = (file, snippets) => {
  const text = read(file);
  for (const snippet of snippets) {
    if (!text.includes(snippet)) fail(`${file}: missing ${snippet}`);
  }
};

const forkImplementationDoc = read(forkDoc);
const obsoleteForkDocClaims = [
  '# PR-054A - External-Execution Timestamp Expand Allocation',
  'This allocation slice changes',
  'schema, runtime, API, UI, agent protocol, and deployment behavior remain',
  'None in this allocation slice',
  'reserved for the later additive expand implementation',
];
for (const claim of obsoleteForkDocClaims) {
  if (forkImplementationDoc.includes(claim)) {
    fail(`${forkDoc}: obsolete allocation-only claim: ${claim}`);
  }
}
requireSnippets(forkDoc, [
  '# PR-054A - External-Execution Timestamp Expand',
  'Tasks 1-10 are implemented locally',
  'Task 11 acceptance',
  'Migration 138 additively adds',
  'Docker-capable PostgreSQL matrix execution',
  'publication, and deployment remain pending',
]);

const buildHubWorkflowFile = '.github/workflows/build-hub.yaml';
const buildHubWorkflow = read(buildHubWorkflowFile);
const buildHubLintLines = sectionLines(buildHubWorkflow, '  lint-and-test-go:', (line) =>
  /^  [A-Za-z0-9_-]+:\s*$/.test(line)
);
const buildHubLintFailure = () => fail(`${buildHubWorkflowFile}: Go lint baseline gate mismatch`);
if (!buildHubLintLines) buildHubLintFailure();
const normalizedBuildHubLintLines = buildHubLintLines.map((line) => line.trim()).filter(Boolean);
const buildHubExactStep = (heading, expected) => {
  const starts = buildHubLintLines.flatMap((line, index) => (line === heading ? [index] : []));
  if (starts.length !== 1) buildHubLintFailure();
  const start = starts[0];
  const next = buildHubLintLines.findIndex((line, index) => index > start && line.startsWith('      - '));
  const actual = buildHubLintLines
    .slice(start, next < 0 ? buildHubLintLines.length : next)
    .map((line) => line.trim())
    .filter(Boolean);
  if (!sameValues(actual, expected)) buildHubLintFailure();
};
const permissionStart = buildHubLintLines.indexOf('    permissions:');
const stepsStart = buildHubLintLines.indexOf('    steps:');
if (
  permissionStart < 0 ||
  stepsStart < permissionStart ||
  !sameValues(
    buildHubLintLines
      .slice(permissionStart, stepsStart)
      .map((line) => line.trim())
      .filter(Boolean),
    ['permissions:', 'contents: read']
  )
) {
  buildHubLintFailure();
}
buildHubExactStep('      - uses: actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10 # v6.0.3', [
  '- uses: actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10 # v6.0.3',
  'with:',
  'fetch-depth: 0',
]);
buildHubExactStep('      - name: Resolve Go lint baseline', [
  '- name: Resolve Go lint baseline',
  'id: lint-baseline',
  "if: github.ref_type != 'tag'",
  'shell: bash',
  'env:',
  'EVENT_NAME: ${{ github.event_name }}',
  'EXPECTED_HEAD_SHA: ${{ github.sha }}',
  'PULL_REQUEST_BASE_SHA: ${{ github.event.pull_request.base.sha }}',
  'PUSH_BEFORE_SHA: ${{ github.event.before }}',
  'run: |',
  'set -Eeuo pipefail',
  'head_sha="$(git rev-parse HEAD)"',
  '[[ "$head_sha" == "$EXPECTED_HEAD_SHA" ]] || {',
  "printf 'checked-out HEAD does not match the workflow SHA\\n' >&2",
  'exit 1',
  '}',
  'case "$EVENT_NAME" in',
  'pull_request) base="$PULL_REQUEST_BASE_SHA" ;;',
  'push) base="$PUSH_BEFORE_SHA" ;;',
  '*)',
  'printf \'unsupported Go lint event: %s\\n\' "$EVENT_NAME" >&2',
  'exit 1',
  ';;',
  'esac',
  'if [[ "$EVENT_NAME" == push &&',
  '"$base" == 0000000000000000000000000000000000000000 ]]; then',
  'printf \'args=\\n\' >>"$GITHUB_OUTPUT"',
  'exit 0',
  'fi',
  '[[ "$base" =~ ^[0-9a-f]{40}$ ]] || {',
  "printf 'Go lint baseline is not a lowercase 40-hex SHA\\n' >&2",
  'exit 1',
  '}',
  'git cat-file -e "${base}^{commit}"',
  'git merge-base --is-ancestor "$base" HEAD',
  'printf \'args=--new-from-rev=%s\\n\' "$base" >>"$GITHUB_OUTPUT"',
]);
buildHubExactStep('      - name: Lint with golangci-lint', [
  '- name: Lint with golangci-lint',
  "if: github.ref_type != 'tag'",
  'uses: golangci/golangci-lint-action@82606bf257cbaff209d206a39f5134f0cfbfd2ee # v9.2.1',
  'with:',
  "version: 'v2.12.2' # renovate: datasource=github-releases depName=golangci/golangci-lint",
  'args: ${{ steps.lint-baseline.outputs.args }}',
]);
for (const forbidden of [
  'only-new-issues',
  'issues-exit-code=0',
  'issues-exit-code: 0',
  '--exclude',
  '--new-from-patch',
  '--new-from-rev=HEAD',
  'continue-on-error:',
  '|| true',
]) {
  if (normalizedBuildHubLintLines.some((line) => line.includes(forbidden))) {
    buildHubLintFailure();
  }
}

const workflowFile = '.github/workflows/community-release-hardening.yaml';
const workflow = read(workflowFile);
const workflowJobLines = (job) => {
  const heading = `  ${job}:`;
  const lines = sectionLines(workflow, heading, (line) => /^  [A-Za-z0-9_-]+:\s*$/.test(line));
  if (!lines) fail(`${workflowFile}: missing or duplicate ${job} job`);
  return lines;
};
const requireJobSnippets = (job, lines, snippets, message) => {
  const text = lines.join('\n');
  for (const snippet of snippets) {
    if (!text.includes(snippet)) fail(`${workflowFile}: ${job} ${message}`);
  }
};
const normalizeExecutionControl = (line, indentation) => {
  const match = line.match(
    new RegExp(
      `^ {${indentation}}(?:"(if|continue-on-error)"|'(if|continue-on-error)'|(if|continue-on-error))\\s*:\\s*(.*)$`
    )
  );
  if (!match) return undefined;
  const key = match[1] ?? match[2] ?? match[3];
  return match[4] === '' ? `${key}:` : `${key}: ${match[4]}`;
};
const hasUnsupportedProtectedMappingKey = (line, indentation) => {
  if (!new RegExp(`^ {${indentation}}\\S`).test(line)) return false;
  return !new RegExp(`^ {${indentation}}[A-Za-z0-9_-]+\\s*:`).test(line);
};
const requireExecutionControls = (job, lines, expectedJobControls, expectedStepControls) => {
  if (lines.some((line) => hasUnsupportedProtectedMappingKey(line, 4) || hasUnsupportedProtectedMappingKey(line, 8))) {
    fail(`${workflowFile}: ${job} execution control mismatch`);
  }
  const jobControls = lines.flatMap((line) => normalizeExecutionControl(line, 4) ?? []);
  const stepControls = lines.flatMap((line) => normalizeExecutionControl(line, 8) ?? []);
  if (!sameValues(jobControls, expectedJobControls) || !sameValues(stepControls, expectedStepControls)) {
    fail(`${workflowFile}: ${job} execution control mismatch`);
  }
};
const requireExactNamedStep = (job, lines, name, expectedLines, message) => {
  const heading = `      - name: ${name}`;
  const starts = lines.flatMap((line, index) => (line === heading ? [index] : []));
  if (starts.length !== 1) fail(`${workflowFile}: ${job} ${message}`);
  const start = starts[0];
  const next = lines.findIndex((line, index) => index > start && line.startsWith('      - '));
  const actual = lines
    .slice(start, next < 0 ? lines.length : next)
    .map((line) => line.trim())
    .filter((line) => line !== '');
  if (!sameValues(actual, expectedLines)) fail(`${workflowFile}: ${job} ${message}`);
};

const fastReleaseLines = workflowJobLines('fast-release-package');
requireExecutionControls('fast-release-package', fastReleaseLines, [], []);
requireExactNamedStep(
  'fast-release-package',
  fastReleaseLines,
  'Validate PR-054A timestamp expand package',
  ['- name: Validate PR-054A timestamp expand package', 'run: node hack/pr054a-validate-timestamp-expand.mjs'],
  'timestamp validator gate mismatch'
);
requireExactNamedStep(
  'fast-release-package',
  fastReleaseLines,
  'Validate timestamp expand Compose orchestration',
  ['- name: Validate timestamp expand Compose orchestration', 'run: bash hack/test-server-compose-timestamp-expand.sh'],
  'Compose orchestration gate mismatch'
);
requireJobSnippets(
  'fast-release-package',
  fastReleaseLines,
  [
    'node hack/pr054a-validate-timestamp-expand.mjs',
    'bash hack/test-server-compose-timestamp-expand.sh',
    'evidence_dir="$(mktemp -d)"',
    `trap 'rm -rf -- "$evidence_dir"' EXIT`,
    'chmod 0700 "$evidence_dir"',
    'export DISTR_COMPOSE_ENV_FILE="$PWD/deploy/server-docker-compose/.env.example"',
    'export DISTR_TIMESTAMP_EVIDENCE_DIR="$evidence_dir"',
    '[[ "$DISTR_COMPOSE_ENV_FILE" = /* ]]',
    '[[ "$DISTR_TIMESTAMP_EVIDENCE_DIR" = /* ]]',
  ],
  'Compose render gate mismatch'
);
const expectedComposeConfig =
  'docker compose --env-file "$DISTR_COMPOSE_ENV_FILE" -f deploy/server-docker-compose/docker-compose.yml --profile timestamp-operator config --quiet';
const composeConfigLines = fastReleaseLines
  .map((line) => line.trim())
  .filter((line) => line.startsWith('docker compose ') && line.includes(' config'));
if (!sameValues(composeConfigLines, [expectedComposeConfig])) {
  fail(`${workflowFile}: fast-release-package Compose render gate mismatch`);
}
const composeStepStarts = fastReleaseLines.flatMap((line, index) =>
  line === '      - name: Validate timestamp expand Compose rendering' ? [index] : []
);
if (composeStepStarts.length !== 1) fail(`${workflowFile}: fast-release-package Compose render gate mismatch`);
const composeStepStart = composeStepStarts[0];
const composeStepEnd = fastReleaseLines.findIndex(
  (line, index) => index > composeStepStart && line.startsWith('      - ')
);
const composeStepLines = fastReleaseLines
  .slice(composeStepStart, composeStepEnd < 0 ? fastReleaseLines.length : composeStepEnd)
  .map((line) => line.trim())
  .filter((line) => line !== '');
const expectedComposeStepLines = [
  '- name: Validate timestamp expand Compose rendering',
  'shell: bash',
  'run: |',
  'set -Eeuo pipefail',
  'evidence_dir="$(mktemp -d)"',
  `trap 'rm -rf -- "$evidence_dir"' EXIT`,
  'chmod 0700 "$evidence_dir"',
  'export DISTR_COMPOSE_ENV_FILE="$PWD/deploy/server-docker-compose/.env.example"',
  'export DISTR_TIMESTAMP_EVIDENCE_DIR="$evidence_dir"',
  '[[ "$DISTR_COMPOSE_ENV_FILE" = /* ]]',
  '[[ "$DISTR_TIMESTAMP_EVIDENCE_DIR" = /* ]]',
  expectedComposeConfig,
];
if (!sameValues(composeStepLines, expectedComposeStepLines)) {
  fail(`${workflowFile}: fast-release-package Compose render gate mismatch`);
}

const timestampPostgresLines = workflowJobLines('timestamp-expand-postgresql');
requireExecutionControls('timestamp-expand-postgresql', timestampPostgresLines, [], ['if: ${{ always() }}']);
requireExactNamedStep(
  'timestamp-expand-postgresql',
  timestampPostgresLines,
  'Capture PostgreSQL runtime and image evidence',
  [
    '- name: Capture PostgreSQL runtime and image evidence',
    'shell: bash',
    'env:',
    'EXPECTED_POSTGRES_VERSION: ${{ matrix.postgres_version }}',
    'EXPECTED_POSTGRES_IMAGE: ${{ matrix.postgres_image }}',
    'POSTGRES_CONTAINER_ID: ${{ job.services.postgres.id }}',
    'run: |',
    'set -Eeuo pipefail',
    'evidence_dir="$RUNNER_TEMP/timestamp-expand-postgresql-evidence"',
    'evidence_file="$evidence_dir/postgresql-${EXPECTED_POSTGRES_VERSION}.txt"',
    '[[ "$evidence_dir" = /* ]]',
    'install -d -m 0700 "$evidence_dir"',
    `runtime_version="$(docker exec "$POSTGRES_CONTAINER_ID" psql -U local -d distr -Atqc 'SHOW server_version')"`,
    `configured_image="$(docker inspect --format '{{.Config.Image}}' "$POSTGRES_CONTAINER_ID")"`,
    `image_id="$(docker inspect --format '{{.Image}}' "$POSTGRES_CONTAINER_ID")"`,
    "repo_digest=''",
    'repo_digest_count=0',
    'while IFS= read -r candidate; do',
    'if [[ "$candidate" =~ ^postgres@sha256:[0-9a-f]{64}$ ]]; then',
    'repo_digest="$candidate"',
    'repo_digest_count=$((repo_digest_count + 1))',
    'fi',
    `done < <(docker image inspect --format '{{range .RepoDigests}}{{println .}}{{end}}' "$image_id")`,
    '[[ "$runtime_version" == "$EXPECTED_POSTGRES_VERSION" ]]',
    '[[ "$configured_image" == "$EXPECTED_POSTGRES_IMAGE" ]]',
    '[[ "$image_id" =~ ^sha256:[0-9a-f]{64}$ ]]',
    '[[ "$repo_digest_count" -eq 1 ]]',
    '[[ "$repo_digest" =~ ^postgres@sha256:[0-9a-f]{64}$ ]]',
    'umask 077',
    "printf '%s\\n' \\",
    '"expected_server_version=$EXPECTED_POSTGRES_VERSION" \\',
    '"runtime_server_version=$runtime_version" \\',
    '"expected_image=$EXPECTED_POSTGRES_IMAGE" \\',
    '"configured_image=$configured_image" \\',
    '"image_id=$image_id" \\',
    '"repo_digest=$repo_digest" >"$evidence_file"',
    'chmod 0600 "$evidence_file"',
    '[[ -s "$evidence_file" ]]',
  ],
  'runtime evidence gate mismatch'
);
requireExactNamedStep(
  'timestamp-expand-postgresql',
  timestampPostgresLines,
  'Validate migration sequence',
  ['- name: Validate migration sequence', 'run: bash hack/validate-migrations.sh'],
  'migration sequence gate mismatch'
);
requireExactNamedStep(
  'timestamp-expand-postgresql',
  timestampPostgresLines,
  'Run timestamp expand migration and compatibility tests',
  [
    '- name: Run timestamp expand migration and compatibility tests',
    'run: go test -p=1 ./internal/externalexecutiontimestamp ./internal/migrations ./internal/db ./internal/hubexecutor ./internal/mapping ./cmd/hub/cmd -count=1 -timeout 30m',
  ],
  'targeted Go gate mismatch'
);
requireJobSnippets(
  'timestamp-expand-postgresql',
  timestampPostgresLines,
  [
    'postgres_image: postgres:16.14-alpine3.23',
    'postgres_image: postgres:18.4-alpine3.23',
    'image: ${{ matrix.postgres_image }}',
    'EXPECTED_POSTGRES_VERSION: ${{ matrix.postgres_version }}',
    'EXPECTED_POSTGRES_IMAGE: ${{ matrix.postgres_image }}',
    'POSTGRES_CONTAINER_ID: ${{ job.services.postgres.id }}',
    `runtime_version="$(docker exec "$POSTGRES_CONTAINER_ID" psql -U local -d distr -Atqc 'SHOW server_version')"`,
    `configured_image="$(docker inspect --format '{{.Config.Image}}' "$POSTGRES_CONTAINER_ID")"`,
    `image_id="$(docker inspect --format '{{.Image}}' "$POSTGRES_CONTAINER_ID")"`,
    `docker image inspect --format '{{range .RepoDigests}}{{println .}}{{end}}' "$image_id"`,
    '[[ "$runtime_version" == "$EXPECTED_POSTGRES_VERSION" ]]',
    '[[ "$configured_image" == "$EXPECTED_POSTGRES_IMAGE" ]]',
    '[[ "$image_id" =~ ^sha256:[0-9a-f]{64}$ ]]',
    '[[ "$repo_digest_count" -eq 1 ]]',
    '[[ "$repo_digest" =~ ^postgres@sha256:[0-9a-f]{64}$ ]]',
    'evidence_dir="$RUNNER_TEMP/timestamp-expand-postgresql-evidence"',
    '[[ "$evidence_dir" = /* ]]',
    'install -d -m 0700 "$evidence_dir"',
    'umask 077',
    'chmod 0600 "$evidence_file"',
    'if: ${{ always() }}',
    'actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a',
    'name: timestamp-expand-postgresql-${{ matrix.postgres_version }}',
    'path: ${{ runner.temp }}/timestamp-expand-postgresql-evidence',
    'if-no-files-found: error',
    'go test -p=1 ./internal/externalexecutiontimestamp ./internal/migrations ./internal/db ./internal/hubexecutor ./internal/mapping ./cmd/hub/cmd -count=1 -timeout 30m',
  ],
  'runtime evidence gate mismatch'
);

const releaseGateLines = workflowJobLines('release-gates');
requireExecutionControls('release-gates', releaseGateLines, ['if: ${{ always() }}'], []);
requireExactNamedStep(
  'release-gates',
  releaseGateLines,
  'Check committed patch whitespace',
  [
    '- name: Check committed patch whitespace',
    'shell: bash',
    'env:',
    'EVENT_NAME: ${{ github.event_name }}',
    'PULL_REQUEST_BASE_SHA: ${{ github.event.pull_request.base.sha }}',
    'PUSH_BEFORE_SHA: ${{ github.event.before }}',
    'run: |',
    'set -Eeuo pipefail',
    'case "$EVENT_NAME" in',
    'pull_request)',
    'base="$PULL_REQUEST_BASE_SHA"',
    ';;',
    'push)',
    'if [[ "$PUSH_BEFORE_SHA" =~ ^0{40}$ ]]; then',
    'empty_tree="$(git hash-object -t tree /dev/null)"',
    '[[ "$empty_tree" =~ ^[0-9a-f]{40}$ ]]',
    'git diff --check "$empty_tree" HEAD',
    'exit 0',
    'else',
    'base="$PUSH_BEFORE_SHA"',
    'fi',
    ';;',
    '*)',
    "printf 'unsupported event for release diff check\\n' >&2",
    'exit 1',
    ';;',
    'esac',
    '[[ "$base" =~ ^[0-9a-f]{40}$ ]]',
    'if ! git cat-file -e "${base}^{commit}" 2>/dev/null; then',
    'git fetch --no-tags --no-recurse-submodules --depth=1 origin "$base"',
    'fi',
    'git cat-file -e "${base}^{commit}"',
    'git diff --check "$base" HEAD',
  ],
  'committed diff check mismatch'
);
const releaseNeeds = releaseGateLines.filter((line) => line.trimStart().startsWith('needs:'));
if (
  !sameValues(
    releaseNeeds.map((line) => line.trim()),
    ['needs: [fast-release-package, timestamp-expand-postgresql]']
  )
) {
  fail(`${workflowFile}: release-gates dependency mismatch`);
}
const releaseGateJobIf = releaseGateLines.filter((line) => line.startsWith('    if:'));
if (!sameValues(releaseGateJobIf, ['    if: ${{ always() }}'])) {
  fail(`${workflowFile}: release-gates dependency mismatch`);
}
if (!releaseGateLines.join('\n').includes('image: postgres:18.4-alpine3.23')) {
  fail(`${workflowFile}: release-gates must remain pinned to postgres:18.4-alpine3.23`);
}
const fullGoSuiteLines = releaseGateLines
  .map((line) => line.trim())
  .filter((line) => line.startsWith('run: go test -p=1 ./...'));
if (!sameValues(fullGoSuiteLines, ['run: go test -p=1 ./... -count=1 -timeout 60m'])) {
  fail(`${workflowFile}: release-gates full Go suite mismatch`);
}
requireExactNamedStep(
  'release-gates',
  releaseGateLines,
  'Run Go tests',
  [
    '- name: Run Go tests',
    'env:',
    'DISTR_TEST_DATABASE_URL: postgres://local:local@localhost:5432/distr?sslmode=disable',
    'run: go test -p=1 ./... -count=1 -timeout 60m',
  ],
  'full Go suite mismatch'
);
requireJobSnippets(
  'release-gates',
  releaseGateLines,
  [
    'fetch-depth: 0',
    'EVENT_NAME: ${{ github.event_name }}',
    'PULL_REQUEST_BASE_SHA: ${{ github.event.pull_request.base.sha }}',
    'PUSH_BEFORE_SHA: ${{ github.event.before }}',
    'case "$EVENT_NAME" in',
    'base="$PULL_REQUEST_BASE_SHA"',
    'if [[ "$PUSH_BEFORE_SHA" =~ ^0{40}$ ]]; then',
    'empty_tree="$(git hash-object -t tree /dev/null)"',
    '[[ "$empty_tree" =~ ^[0-9a-f]{40}$ ]]',
    'git diff --check "$empty_tree" HEAD',
    'exit 0',
    'base="$PUSH_BEFORE_SHA"',
    '[[ "$base" =~ ^[0-9a-f]{40}$ ]]',
    'if ! git cat-file -e "${base}^{commit}" 2>/dev/null; then',
    'git fetch --no-tags --no-recurse-submodules --depth=1 origin "$base"',
    'git cat-file -e "${base}^{commit}"',
    'git diff --check "$base" HEAD',
  ],
  'committed diff check mismatch'
);
for (const repeatedEventInput of [
  'EVENT_NAME: ${{ github.event_name }}',
  'PULL_REQUEST_BASE_SHA: ${{ github.event.pull_request.base.sha }}',
  'PUSH_BEFORE_SHA: ${{ github.event.before }}',
]) {
  if (releaseGateLines.filter((line) => line.trim() === repeatedEventInput).length !== 2) {
    fail(`${workflowFile}: release-gates committed diff check mismatch`);
  }
}
requireJobSnippets(
  'release-gates',
  releaseGateLines,
  [
    "GITLEAKS_VERSION='8.30.1'",
    "GITLEAKS_SHA256='551f6fc83ea457d62a0d98237cbad105af8d557003051f41f3e7ca7b3f2470eb'",
    'PULL_REQUEST_HEAD_SHA: ${{ github.event.pull_request.head.sha }}',
    'EXECUTION_SHA: ${{ github.sha }}',
    `head_commit="$(git rev-parse --verify 'HEAD^{commit}')"`,
    '[[ "$head_commit" == "$EXECUTION_SHA" ]]',
    'git merge-base --is-ancestor "$PULL_REQUEST_HEAD_SHA" "$head_commit"',
    'https://github.com/gitleaks/gitleaks/releases/download/v${GITLEAKS_VERSION}/gitleaks_${GITLEAKS_VERSION}_linux_x64.tar.gz',
    "curl --proto '=https' --tlsv1.2 --fail --location --silent --show-error",
    `printf '%s  %s\\n' "$GITLEAKS_SHA256" "$gitleaks_archive" | sha256sum --check --status`,
    '[[ "$(uname -m)" == x86_64 ]]',
    '[[ "$("$scan_dir/gitleaks" version)" == "$GITLEAKS_VERSION" ]]',
    'unset GITLEAKS_CONFIG GITLEAKS_CONFIG_TOML',
    `printf '[extend]\\nuseDefault = true\\n' >"$gitleaks_config"`,
    ': >"$gitleaks_ignore"',
    'chmod 0600 "$gitleaks_config" "$gitleaks_ignore"',
    'gitleaks_log_opts="--full-history --root --diff-merges=separate --no-ext-diff --no-textconv $base_object..$head_commit"',
    'base_object="$(git hash-object -t tree /dev/null)"',
    'gitleaks_log_opts="--full-history --root --diff-merges=separate --no-ext-diff --no-textconv $head_commit"',
    '--config "$gitleaks_config"',
    '--gitleaks-ignore-path "$gitleaks_ignore"',
    '--ignore-gitleaks-allow',
    '--redact=100',
    '--log-opts="$gitleaks_log_opts"',
    '"$GITHUB_WORKSPACE" >"$gitleaks_stdout" 2>"$gitleaks_stderr"',
    'node hack/pr054a-validate-timestamp-expand.mjs --scan-privacy-diff "$base_object" "$head_commit"',
  ],
  'privacy gate mismatch'
);
const releaseGateText = releaseGateLines.join('\n');
for (const forbidden of [
  '--baseline-path',
  '--exclude-path',
  '--enable-rule',
  '--disable-rule',
  '--exit-code',
  '--max-target-megabytes',
  '--log-opts=--all',
  '.gitleaks.toml',
  '.gitleaksignore',
  'gitleaks:allow',
  '--scan-privacy-diff "$base_object" "$head_commit" --require-private-scope',
  'continue-on-error:',
  '|| true',
]) {
  if (releaseGateText.includes(forbidden)) {
    fail(`${workflowFile}: release-gates privacy gate mismatch`);
  }
}
const normalizedReleaseLines = releaseGateLines.map((line) => line.trim());
const countExactContiguousBlocks = (lines, expected) =>
  lines.reduce(
    (count, _line, index) => count + Number(sameValues(lines.slice(index, index + expected.length), expected)),
    0
  );
requireExactNamedStep(
  'fast-release-package',
  fastReleaseLines,
  'Run PR-054A security regression suite',
  ['- name: Run PR-054A security regression suite', 'run: node hack/pr054a-validate-timestamp-expand.test.mjs'],
  'security regression gate mismatch'
);
const committedDiffLines = normalizedReleaseLines.filter((line) => line.startsWith('git diff --check '));
if (!sameValues(committedDiffLines, ['git diff --check "$empty_tree" HEAD', 'git diff --check "$base" HEAD'])) {
  fail(`${workflowFile}: release-gates committed diff check mismatch`);
}
const missingBaseFetchLines = normalizedReleaseLines.filter((line) => line.startsWith('git fetch '));
if (!sameValues(missingBaseFetchLines, ['git fetch --no-tags --no-recurse-submodules --depth=1 origin "$base"'])) {
  fail(`${workflowFile}: release-gates committed diff check mismatch`);
}
const pushBaseAvailabilityBlock = [
  'if ! git cat-file -e "${base}^{commit}" 2>/dev/null; then',
  'git fetch --no-tags --no-recurse-submodules --depth=1 origin "$base"',
  'fi',
  'git cat-file -e "${base}^{commit}"',
  'git diff --check "$base" HEAD',
];
if (countExactContiguousBlocks(normalizedReleaseLines, pushBaseAvailabilityBlock) !== 1) {
  fail(`${workflowFile}: release-gates committed diff check mismatch`);
}
const prerequisiteResultBlock = [
  'steps:',
  '- name: Assert prerequisite jobs succeeded',
  'env:',
  'FAST_RELEASE_RESULT: ${{ needs.fast-release-package.result }}',
  'TIMESTAMP_POSTGRES_RESULT: ${{ needs.timestamp-expand-postgresql.result }}',
  'run: |',
  'set -Eeuo pipefail',
  '[[ "$FAST_RELEASE_RESULT" == success ]]',
  '[[ "$TIMESTAMP_POSTGRES_RESULT" == success ]]',
  '- uses: actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10 # v6.0.3',
];
if (countExactContiguousBlocks(normalizedReleaseLines, prerequisiteResultBlock) !== 1) {
  fail(`${workflowFile}: release-gates dependency mismatch`);
}
const gitleaksExecutionBlock = [
  'set +e',
  '"$scan_dir/gitleaks" git \\',
  '--no-banner \\',
  '--redact=100 \\',
  '--config "$gitleaks_config" \\',
  '--gitleaks-ignore-path "$gitleaks_ignore" \\',
  '--ignore-gitleaks-allow \\',
  '--log-opts="$gitleaks_log_opts" \\',
  '"$GITHUB_WORKSPACE" >"$gitleaks_stdout" 2>"$gitleaks_stderr"',
  'gitleaks_status=$?',
  'set -e',
  'case "$gitleaks_status" in',
  '0) ;;',
  '1)',
  `printf 'default secret scan detected findings\\n' >&2`,
  'exit 1',
  ';;',
  '*)',
  `printf 'default secret scan failed closed\\n' >&2`,
  'exit 1',
  ';;',
  'esac',
];
if (countExactContiguousBlocks(normalizedReleaseLines, gitleaksExecutionBlock) !== 1) {
  fail(`${workflowFile}: release-gates privacy gate mismatch`);
}
const privacyChecksumLine = `printf '%s  %s\\n' "$GITLEAKS_SHA256" "$gitleaks_archive" | sha256sum --check --status`;
const privacyChecksumIndexes = normalizedReleaseLines.flatMap((line, index) =>
  line.includes('sha256sum --check') ? [index] : []
);
const privacyExtractionLine = 'tar -xzf "$gitleaks_archive" -C "$scan_dir" gitleaks';
const privacyExtractionIndexes = normalizedReleaseLines.flatMap((line, index) =>
  line === privacyExtractionLine ? [index] : []
);
if (
  privacyChecksumIndexes.length !== 1 ||
  normalizedReleaseLines[privacyChecksumIndexes[0]] !== privacyChecksumLine ||
  privacyExtractionIndexes.length !== 1 ||
  privacyChecksumIndexes[0] >= privacyExtractionIndexes[0]
) {
  fail(`${workflowFile}: release-gates privacy gate mismatch`);
}
const privacyCliLine =
  'node hack/pr054a-validate-timestamp-expand.mjs --scan-privacy-diff "$base_object" "$head_commit"';
const privacyCliLines = normalizedReleaseLines.filter((line) => line.includes('--scan-privacy-diff'));
if (!sameValues(privacyCliLines, [privacyCliLine])) {
  fail(`${workflowFile}: release-gates privacy gate mismatch`);
}
const privacyExecutionBlock = [
  'set +e',
  privacyCliLine,
  'privacy_status=$?',
  'set -e',
  'case "$privacy_status" in',
  '0) ;;',
  '*)',
  `printf 'semantic privacy scan failed closed\\n' >&2`,
  'exit 1',
  ';;',
  'esac',
];
if (countExactContiguousBlocks(normalizedReleaseLines, privacyExecutionBlock) !== 1) {
  fail(`${workflowFile}: release-gates privacy gate mismatch`);
}
const privacyStepStarts = releaseGateLines.flatMap((line, index) =>
  line === '      - name: Scan committed secrets and privacy' ? [index] : []
);
if (privacyStepStarts.length !== 1) fail(`${workflowFile}: release-gates privacy gate mismatch`);
const privacyStepStart = privacyStepStarts[0];
const privacyStepEnd = releaseGateLines.findIndex(
  (line, index) => index > privacyStepStart && line.startsWith('      - ')
);
if (privacyStepEnd < 0) fail(`${workflowFile}: release-gates privacy gate mismatch`);
const privacyStepLines = releaseGateLines.slice(privacyStepStart, privacyStepEnd);
if (privacyStepLines.some((line) => normalizeExecutionControl(line, 8) !== undefined)) {
  fail(`${workflowFile}: release-gates privacy gate mismatch`);
}
const privacyRunMarker = privacyStepLines.findIndex((line) => line.trim() === 'run: |');
if (privacyRunMarker < 0) fail(`${workflowFile}: release-gates privacy gate mismatch`);
const privacyMetadataLines = privacyStepLines
  .slice(0, privacyRunMarker + 1)
  .map((line) => line.trim())
  .filter((line) => line !== '');
const expectedPrivacyMetadataLines = [
  '- name: Scan committed secrets and privacy',
  'shell: bash',
  'env:',
  'EVENT_NAME: ${{ github.event_name }}',
  'PULL_REQUEST_BASE_SHA: ${{ github.event.pull_request.base.sha }}',
  'PULL_REQUEST_HEAD_SHA: ${{ github.event.pull_request.head.sha }}',
  'PUSH_BEFORE_SHA: ${{ github.event.before }}',
  'EXECUTION_SHA: ${{ github.sha }}',
  'run: |',
];
if (!sameValues(privacyMetadataLines, expectedPrivacyMetadataLines)) {
  fail(`${workflowFile}: release-gates privacy gate mismatch`);
}
const privacyRunLines = privacyStepLines.slice(privacyRunMarker + 1).map((line) => line.trim());
const expectedPrivacyRunLines = [
  'set -Eeuo pipefail',
  'scan_dir="$(mktemp -d)"',
  `trap 'rm -rf -- "$scan_dir"' EXIT`,
  'chmod 0700 "$scan_dir"',
  'head_commit="$(git rev-parse --verify \'HEAD^{commit}\')"',
  '[[ "$head_commit" =~ ^[0-9a-f]{40}$ ]]',
  '[[ "$EXECUTION_SHA" =~ ^[0-9a-f]{40}$ ]]',
  '[[ "$head_commit" == "$EXECUTION_SHA" ]]',
  'case "$EVENT_NAME" in',
  'pull_request)',
  'base_object="$PULL_REQUEST_BASE_SHA"',
  '[[ "$PULL_REQUEST_HEAD_SHA" =~ ^[0-9a-f]{40}$ ]]',
  'git cat-file -e "${PULL_REQUEST_HEAD_SHA}^{commit}"',
  'git merge-base --is-ancestor "$PULL_REQUEST_HEAD_SHA" "$head_commit"',
  'gitleaks_log_opts="--full-history --root --diff-merges=separate --no-ext-diff --no-textconv $base_object..$head_commit"',
  ';;',
  'push)',
  'if [[ "$PUSH_BEFORE_SHA" =~ ^0{40}$ ]]; then',
  'base_object="$(git hash-object -t tree /dev/null)"',
  'gitleaks_log_opts="--full-history --root --diff-merges=separate --no-ext-diff --no-textconv $head_commit"',
  'else',
  'base_object="$PUSH_BEFORE_SHA"',
  'gitleaks_log_opts="--full-history --root --diff-merges=separate --no-ext-diff --no-textconv $base_object..$head_commit"',
  'fi',
  ';;',
  '*)',
  "printf 'unsupported event for privacy scan\\n' >&2",
  'exit 1',
  ';;',
  'esac',
  '[[ "$base_object" =~ ^[0-9a-f]{40}$ ]]',
  '[[ "$head_commit" =~ ^[0-9a-f]{40}$ ]]',
  'git cat-file -e "${base_object}^{object}"',
  'git cat-file -e "${head_commit}^{commit}"',
  "GITLEAKS_VERSION='8.30.1'",
  "GITLEAKS_SHA256='551f6fc83ea457d62a0d98237cbad105af8d557003051f41f3e7ca7b3f2470eb'",
  '[[ "$(uname -s)" == Linux ]]',
  '[[ "$(uname -m)" == x86_64 ]]',
  'gitleaks_archive="$scan_dir/gitleaks.tar.gz"',
  'gitleaks_url="https://github.com/gitleaks/gitleaks/releases/download/v${GITLEAKS_VERSION}/gitleaks_${GITLEAKS_VERSION}_linux_x64.tar.gz"',
  'curl --proto \'=https\' --tlsv1.2 --fail --location --silent --show-error "$gitleaks_url" --output "$gitleaks_archive"',
  'printf \'%s  %s\\n\' "$GITLEAKS_SHA256" "$gitleaks_archive" | sha256sum --check --status',
  'tar -xzf "$gitleaks_archive" -C "$scan_dir" gitleaks',
  'chmod 0700 "$scan_dir/gitleaks"',
  '[[ "$("$scan_dir/gitleaks" version)" == "$GITLEAKS_VERSION" ]]',
  'unset GITLEAKS_CONFIG GITLEAKS_CONFIG_TOML',
  'gitleaks_config="$scan_dir/default-gitleaks.toml"',
  'gitleaks_ignore="$scan_dir/empty-gitleaks-ignore"',
  'gitleaks_stdout="$scan_dir/gitleaks.stdout"',
  'gitleaks_stderr="$scan_dir/gitleaks.stderr"',
  'printf \'[extend]\\nuseDefault = true\\n\' >"$gitleaks_config"',
  ': >"$gitleaks_ignore"',
  ': >"$gitleaks_stdout"',
  ': >"$gitleaks_stderr"',
  'chmod 0600 "$gitleaks_config" "$gitleaks_ignore"',
  'chmod 0600 "$gitleaks_stdout" "$gitleaks_stderr"',
  'set +e',
  '"$scan_dir/gitleaks" git \\',
  '--no-banner \\',
  '--redact=100 \\',
  '--config "$gitleaks_config" \\',
  '--gitleaks-ignore-path "$gitleaks_ignore" \\',
  '--ignore-gitleaks-allow \\',
  '--log-opts="$gitleaks_log_opts" \\',
  '"$GITHUB_WORKSPACE" >"$gitleaks_stdout" 2>"$gitleaks_stderr"',
  'gitleaks_status=$?',
  'set -e',
  'case "$gitleaks_status" in',
  '0) ;;',
  '1)',
  "printf 'default secret scan detected findings\\n' >&2",
  'exit 1',
  ';;',
  '*)',
  "printf 'default secret scan failed closed\\n' >&2",
  'exit 1',
  ';;',
  'esac',
  'set +e',
  privacyCliLine,
  'privacy_status=$?',
  'set -e',
  'case "$privacy_status" in',
  '0) ;;',
  '*)',
  "printf 'semantic privacy scan failed closed\\n' >&2",
  'exit 1',
  ';;',
  'esac',
];
if (
  !sameValues(
    privacyRunLines.filter((line) => line !== ''),
    expectedPrivacyRunLines
  )
) {
  fail(`${workflowFile}: release-gates privacy gate mismatch`);
}
const gitleaksHistoryAssignments = normalizedReleaseLines.filter((line) => line.startsWith('gitleaks_log_opts='));
if (
  !sameValues(gitleaksHistoryAssignments, [
    'gitleaks_log_opts="--full-history --root --diff-merges=separate --no-ext-diff --no-textconv $base_object..$head_commit"',
    'gitleaks_log_opts="--full-history --root --diff-merges=separate --no-ext-diff --no-textconv $head_commit"',
    'gitleaks_log_opts="--full-history --root --diff-merges=separate --no-ext-diff --no-textconv $base_object..$head_commit"',
  ])
) {
  fail(`${workflowFile}: release-gates privacy gate mismatch`);
}
const executionHeadAssignments = normalizedReleaseLines.filter((line) => line.startsWith('head_commit='));
if (!sameValues(executionHeadAssignments, [`head_commit="$(git rev-parse --verify 'HEAD^{commit}')"`])) {
  fail(`${workflowFile}: release-gates privacy gate mismatch`);
}
const shellContinuation = String.fromCharCode(92);
const normalizeShellContinuation = (line) => {
  const trimmed = line.trim();
  if (!trimmed.endsWith(shellContinuation)) return trimmed;
  const beforeContinuation = trimmed.slice(0, -1);
  return /\s$/.test(beforeContinuation) ? beforeContinuation.trimEnd() : trimmed;
};
for (const [prefix, expected] of [
  ['--config ', ['--config "$gitleaks_config"']],
  ['--gitleaks-ignore-path ', ['--gitleaks-ignore-path "$gitleaks_ignore"']],
  ['--log-opts=', ['--log-opts="$gitleaks_log_opts"']],
]) {
  const actual = releaseGateLines.map(normalizeShellContinuation).filter((line) => line.startsWith(prefix));
  if (!sameValues(actual, expected)) fail(`${workflowFile}: release-gates privacy gate mismatch`);
}

const task11PlanFile = 'docs/superpowers/plans/2026-07-15-external-execution-timestamp-expand.md';
const task11Plan = read(task11PlanFile);
for (const obsolete of [
  '$genericLeakPattern =',
  'Select-String -Pattern $privateScopePattern',
  'DISTR_PRIVATE_SCOPE_PATTERN',
]) {
  if (task11Plan.includes(obsolete)) fail(`${task11PlanFile}: obsolete privacy gate`);
}
for (const required of [
  'Gitleaks v8.30.1 default rules',
  '551f6fc83ea457d62a0d98237cbad105af8d557003051f41f3e7ca7b3f2470eb',
  '--scan-privacy-diff <base-object> <head-commit> [--require-private-scope]',
  '--scan-privacy-diff $baseObject $headCommit --require-private-scope',
  'RFC 2606 reserved domains',
  'RFC 5737 documentation ranges',
  '--full-history --root --diff-merges=separate --no-ext-diff --no-textconv',
  'verified checkout `HEAD`',
  'every parent-to-commit edge',
  'literal pathspec semantics',
  'changed gitlinks',
  'DISTR_PRIVATE_SCOPE_TOKENS',
  'DISTR_PRIVATE_SCOPE_CANARY',
  'never prints matched values, private tokens, or the private canary',
  'exactly one non-merge child of the approved public baseline',
  'Push only the clean publish branch',
  'required `release-gates` aggregator runs with `always()`',
  'failed required aggregate check',
  'fetches the exact `before` object',
  'never depends on a triple-dot merge base',
  'node hack/pr054a-validate-timestamp-expand.test.mjs',
  'CI-enforced security regression suite',
]) {
  if (!task11Plan.includes(required)) fail(`${task11PlanFile}: privacy gate specification mismatch`);
}

const documentRequirements = new Map([
  [
    'docs/release/community-release-readiness.md',
    [
      '## External-Execution Timestamp Expand Gate',
      'A component release never implicitly deploys another component.',
      '`serve --migrate=false`',
      'embedded isolated-acceptance and final-Hub',
      'dedicated operator smoke test runs after apply returns',
    ],
  ],
  [
    'docs/upgrade/community-release-upgrade-checklist.md',
    ['## Migration 138 Decision Path', '`migrate --to 137` is intentionally refused'],
  ],
  [
    'docs/operations/server-docker-compose-deploy.md',
    [
      '## External-Execution Timestamp Expand (Migration 138)',
      'timestamp-expand-capture',
      'timestamp-expand-apply',
      'DISTR_COMPOSE_ENV_FILE',
      '--user "$(id -u):$(id -g)"',
      '`release` acquires the deployment lock',
      'preflight while the existing Hub writers remain online',
    ],
  ],
  [
    'docs/operations/operator-smoke-test.md',
    [
      '## Timestamp Expand Smoke',
      'manifest state is `VERIFIED`',
      'DISTR_COMPOSE_ENV_FILE',
      '--user "$(id -u):$(id -g)"',
      'DISTR_TIMESTAMP_APPROVED_MANIFEST',
      'DISTR_TIMESTAMP_MANIFEST_ID="$(',
      '.id | strings | select(test(',
    ],
  ],
  [
    'docs/security/release-hardening-checklist.md',
    ['## Timestamp Evidence Safety', 'Vulnerability, license, and secret scanners remain mandatory'],
  ],
  [
    'docs/fork/PR-054A_EXTERNAL_EXECUTION_TIMESTAMP_EXPAND.md',
    ['## Operator Evidence and Release Record', 'PostgreSQL 16.14 and PostgreSQL 18.4'],
  ],
  ['docs/fork/FORK_DIFF_INDEX.md', ['### PR-054A - External-execution timestamp expand', 'Migration 138']],
  [
    'docs/fork/UPGRADE_GUIDE.md',
    ['## Current Timestamp Procedure', 'community-release-upgrade-checklist.md#migration-138-decision-path'],
  ],
]);
for (const [file, snippets] of documentRequirements) requireSnippets(file, snippets);

const timestampSecurityBlock = sectionLines(
  read('docs/security/release-hardening-checklist.md'),
  '## Timestamp Evidence Safety',
  (line) => line.startsWith('## ')
);
const timestampSecurityText = timestampSecurityBlock?.join('\n') ?? '';
const timestampSecurityNormalized = timestampSecurityText.replace(/\s+/g, ' ');
if (
  !timestampSecurityBlock ||
  !timestampSecurityNormalized.includes('exact legacy `rawValue`') ||
  !timestampSecurityNormalized.includes('`sourceZone`') ||
  !timestampSecurityNormalized.includes('`convertedValue`') ||
  !timestampSecurityNormalized.includes('`evidenceReference`') ||
  !timestampSecurityNormalized.includes('approving identity') ||
  !timestampSecurityNormalized.includes('author/reviewer identity') ||
  !timestampSecurityNormalized.includes('release identity') ||
  timestampSecurityNormalized.includes('identifiers, counts, decisions, and checksums only')
) {
  fail('timestamp evidence field inventory mismatch');
}
for (const required of [
  'Free-text author, reviewer, approving-identity, and opaque evidence-reference values',
  'reviewed as non-sensitive before sealing',
  'DSNs, credentials, payloads, messages, tokens, passwords, customer data, or private absolute paths',
]) {
  if (!timestampSecurityNormalized.includes(required)) {
    fail('timestamp evidence free-text safety mismatch');
  }
}

const optionalDeployDoc = 'docs/operations/server-docker-compose-deploy.md';
const optionalDeployText = read(optionalDeployDoc);
const optionalDeployLines = optionalDeployText.split(/\r?\n/);
const requireSingleExactLine = (line, message) => {
  if (optionalDeployLines.filter((candidate) => candidate === line).length !== 1)
    fail(`${optionalDeployDoc}: ${message}`);
};
const requireOrderedExactLines = (lines, expected, message) => {
  let cursor = -1;
  for (const line of expected) {
    cursor = lines.findIndex((candidate, index) => index > cursor && candidate === line);
    if (cursor < 0) fail(`${optionalDeployDoc}: ${message}`);
  }
};
const requireDeploySnippet = (snippet, message) => {
  if (!optionalDeployText.includes(snippet)) fail(`${optionalDeployDoc}: ${message}`);
};

requireSingleExactLine(
  '- `curl`, `openssl`, `jq`, `sha256sum`, `bash`, and `flock`.',
  'server tool prerequisites mismatch'
);
requireDeploySnippet('copy its `DISTR_IMAGE_REF`, `DISTR_RELEASE_COMMIT`, and', 'release identity handoff mismatch');
requireDeploySnippet('`DISTR_IMAGE_DIGEST` values together', 'release identity handoff mismatch');
requireDeploySnippet(
  'Set `DISTR_CALLBACK_PROBE_URL` to a non-`CHANGE_ME` loopback callbacks route',
  'callback probe handoff mismatch'
);
requireDeploySnippet(
  'Timestamp-expand apply additionally requires `DISTR_AUDIT_HISTORY_PROBE_URL` for that captured historical execution',
  'timestamp audit handoff mismatch'
);
requireDeploySnippet('its read-only `DISTR_AUDIT_HISTORY_PROBE_TOKEN`', 'timestamp audit handoff mismatch');

const timestampDeployBlock = sectionLines(
  optionalDeployText,
  '## External-Execution Timestamp Expand (Migration 138)',
  (line) => line.startsWith('## ')
);
if (!timestampDeployBlock) fail(`${optionalDeployDoc}: approved manifest sidecar procedure mismatch`);
requireOrderedExactLines(
  timestampDeployBlock,
  [
    'Compose `--env-file` does not export those values into the host shell.',
    'set -Eeuo pipefail',
    'export DISTR_COMPOSE_ENV_FILE="$(realpath deploy/server-docker-compose/.env)"',
    'export DISTR_TIMESTAMP_AUTHOR="CHANGE_ME_NON_SECRET_AUTHOR_IDENTITY"',
    'export DISTR_TIMESTAMP_REVIEWER="CHANGE_ME_DISTINCT_NON_SECRET_REVIEWER_IDENTITY"',
    'export DISTR_TIMESTAMP_EVIDENCE_REFERENCE="CHANGE_ME_OPAQUE_NON_SECRET_EVIDENCE_REFERENCE"',
    '[[ "$DISTR_COMPOSE_ENV_FILE" = /* ]]',
    '[[ -f "$DISTR_COMPOSE_ENV_FILE" && ! -L "$DISTR_COMPOSE_ENV_FILE" ]]',
    '[[ "$(stat -c \'%a\' -- "$DISTR_COMPOSE_ENV_FILE")" == 600 ]]',
    'read_compose_env_value() {',
    '  mapfile -t matches < <(grep -E "^${key}=" "$DISTR_COMPOSE_ENV_FILE")',
    '  ((${#matches[@]} == 1)) || return 1',
    'export DISTR_TIMESTAMP_EVIDENCE_DIR="$(read_compose_env_value DISTR_TIMESTAMP_EVIDENCE_DIR)"',
    '[[ "$DISTR_TIMESTAMP_EVIDENCE_DIR" = /* ]]',
    '[[ -d "$DISTR_TIMESTAMP_EVIDENCE_DIR" && ! -L "$DISTR_TIMESTAMP_EVIDENCE_DIR" ]]',
    '[[ "$DISTR_TIMESTAMP_AUTHOR" != CHANGE_ME_* ]]',
    '[[ "$DISTR_TIMESTAMP_REVIEWER" != CHANGE_ME_* ]]',
    '[[ "$DISTR_TIMESTAMP_EVIDENCE_REFERENCE" != CHANGE_ME_* ]]',
    '[[ "$DISTR_TIMESTAMP_AUTHOR" != "$DISTR_TIMESTAMP_REVIEWER" ]]',
    'export DISTR_RELEASE_COMMIT="$(read_compose_env_value DISTR_RELEASE_COMMIT)"',
    'export DISTR_IMAGE_DIGEST="$(read_compose_env_value DISTR_IMAGE_DIGEST)"',
    '[[ "$DISTR_RELEASE_COMMIT" =~ ^[0-9a-f]{40}$ ]]',
    '[[ "$DISTR_IMAGE_DIGEST" =~ ^sha256:[0-9a-f]{64}$ ]]',
    'evidence_checksum_file="$DISTR_TIMESTAMP_EVIDENCE_DIR/evidence-bundle.sha256"',
    '[[ -f "$evidence_checksum_file" && ! -L "$evidence_checksum_file" ]]',
    '[[ "$(stat -c \'%a\' -- "$evidence_checksum_file")" == 600 ]]',
    'mapfile -t evidence_checksum_lines <"$evidence_checksum_file"',
    '((${#evidence_checksum_lines[@]} == 1))',
    'export DISTR_TIMESTAMP_EVIDENCE_CHECKSUM="${BASH_REMATCH[1]}"',
    '[[ "$(read_compose_env_value DISTR_TIMESTAMP_EVIDENCE_CHECKSUM)" == "$DISTR_TIMESTAMP_EVIDENCE_CHECKSUM" ]]',
    'docker compose \\',
  ],
  'timestamp seal preparation mismatch'
);
if (
  !timestampDeployBlock
    .join('\n')
    .includes('[[ "${evidence_checksum_lines[0]}" =~ ^(sha256:[0-9a-f]{64})\\ \\ timestamp-evidence-bundle-v1$ ]]')
) {
  fail(`${optionalDeployDoc}: timestamp seal preparation mismatch`);
}
requireOrderedExactLines(
  timestampDeployBlock,
  [
    'The sealing command writes only the approved JSON.',
    'Before apply, create its restricted checksum sidecar on the host:',
    'export DISTR_TIMESTAMP_APPROVED_MANIFEST="$DISTR_TIMESTAMP_EVIDENCE_DIR/approved-manifest.json"',
    '(',
    '  set -Eeuo pipefail',
    '  approved_name="$(basename -- "$DISTR_TIMESTAMP_APPROVED_MANIFEST")"',
    '  sidecar_name="${approved_name}.sha256"',
    '  cd -- "$DISTR_TIMESTAMP_EVIDENCE_DIR"',
    '  [[ -f "$approved_name" && ! -L "$approved_name" ]]',
    '  [[ ! -e "$sidecar_name" && ! -L "$sidecar_name" ]]',
    '  chmod 0600 -- "$approved_name"',
    '  umask 077',
    '  set -o noclobber',
    '  sha256sum --text -- "$approved_name" >"$sidecar_name"',
    '  chmod 0600 -- "$sidecar_name"',
    ')',
    './deploy/server-docker-compose/deploy.sh \\',
    '  timestamp-expand-apply \\',
    '  "$DISTR_TIMESTAMP_APPROVED_MANIFEST" \\',
    '  "$DISTR_TIMESTAMP_EVIDENCE_DIR"',
  ],
  'approved manifest sidecar procedure mismatch'
);

const optionalDeployBlock = sectionLines(read(optionalDeployDoc), '## Optional Server Build', (line) =>
  line.startsWith('## ')
);
if (!optionalDeployBlock) fail(`${optionalDeployDoc}: optional deploy procedure missing`);
const expectedOptionalDeployProcedure = [
  '1. Acquires the deployment lock and refuses an active timestamp-expand fence.',
  '2. Installs pinned build tools with `mise install`.',
  '3. Builds the community frontend and Hub from source.',
  '4. Copies `dist/distr` to the architecture-specific name required by `Dockerfile.hub`.',
  '5. Builds the Docker image tagged as `DISTR_IMAGE:DISTR_IMAGE_TAG`.',
  '6. Logs in to AWS ECR and pushes the image.',
  '7. Resolves the pushed tag to an ECR digest.',
  '8. Atomically updates `DISTR_IMAGE_REF`, `DISTR_RELEASE_COMMIT`, and `DISTR_IMAGE_DIGEST` in the deployment environment.',
  '9. Writes `dist/release-${DISTR_IMAGE_TAG}.env` from that same resolved image identity.',
  '10. Validates the Docker Compose config and immutable release identity.',
  '11. Pulls the digest-pinned image from ECR for Compose.',
  '12. Starts PostgreSQL and RustFS.',
  '13. Runs the read-only migration preflight while the existing Hub writers remain online.',
  '14. Stops Hub and verifies that its writers are stopped.',
  '15. Creates and restore-verifies PostgreSQL and RustFS backups when data already exists.',
  '16. Runs `distr migrate` explicitly.',
  '17. Starts Hub with `serve --migrate=false`.',
  '18. Waits for `http://127.0.0.1:${DISTR_HTTP_PORT}/ready`.',
];
const optionalDeployProcedure = optionalDeployBlock.filter((line) => /^\d+\. /.test(line));
if (!sameValues(optionalDeployProcedure, expectedOptionalDeployProcedure)) {
  fail(`${optionalDeployDoc}: optional deploy procedure mismatch`);
}

const extractSection = (file, heading) => {
  const level = heading.match(/^#+/)[0].length;
  const lines = sectionLines(read(file), heading, (line) => new RegExp(`^#{1,${level}} `).test(line));
  if (!lines) fail(`${file}: must contain exactly one section ${heading}`);
  return lines.join('\n');
};
const neutralSections = [
  ['docs/release/community-release-readiness.md', '## External-Execution Timestamp Expand Gate'],
  ['docs/upgrade/community-release-upgrade-checklist.md', '## Migration 138 Decision Path'],
  ['docs/operations/server-docker-compose-deploy.md', '## External-Execution Timestamp Expand (Migration 138)'],
  ['docs/operations/operator-smoke-test.md', '## Timestamp Expand Smoke'],
  ['docs/security/release-hardening-checklist.md', '## Timestamp Evidence Safety'],
  ['docs/fork/PR-054A_EXTERNAL_EXECUTION_TIMESTAMP_EXPAND.md', '## Operator Evidence and Release Record'],
  ['docs/fork/FORK_DIFF_INDEX.md', '### PR-054A - External-execution timestamp expand'],
  ['docs/fork/UPGRADE_GUIDE.md', '## Current Timestamp Procedure'],
];
const containsAddressLikeIPv4 = (text) => {
  const candidates = /\b(?:\d{1,3}\.){3}\d{1,3}\b/g;
  for (const match of text.matchAll(candidates)) {
    if (!match[0].split('.').every((part) => Number(part) <= 255)) continue;
    const lineStart = text.lastIndexOf('\n', match.index - 1) + 1;
    const lineEndOffset = text.slice(match.index).indexOf('\n');
    const lineEnd = lineEndOffset < 0 ? text.length : match.index + lineEndOffset;
    const line = text.slice(lineStart, lineEnd);
    const versionContext = text.slice(lineStart, match.index);
    const addressContext = /\b(?:address|endpoint|host|ip|private|public|server|url)\b/i.test(line);
    const explicitVersionContext =
      /(?:^|[^A-Za-z0-9])(?:version|release(?:\s+version)?|tool(?:\s+version)?|image(?:[\s_-]+(?:tag|version))?)[\s:=@_-]*(?:v)?\s*$/i.test(
        versionContext
      );
    if (!addressContext && explicitVersionContext) {
      continue;
    }
    return true;
  }
  return false;
};

const safeCredentialPlaceholders = new Set(['change_me', '<placeholder>', '[redacted]']);
const credentialMetadataSuffixes = new Set(['file', 'path', 'required', 'ttl']);
const credentialTerms = new Set(['password', 'passwd', 'secret', 'token', 'apikey', 'accesskey', 'privatekey']);
const identifierSegments = (identifier) =>
  identifier
    .replace(/([A-Z]+)([A-Z][a-z])/g, '$1_$2')
    .replace(/([a-z0-9])([A-Z])/g, '$1_$2')
    .split(/[._-]+/)
    .filter(Boolean)
    .map((segment) => segment.toLowerCase());
const credentialFieldEnd = (segments) => {
  let end = -1;
  for (let index = 0; index < segments.length; index += 1) {
    if (credentialTerms.has(segments[index])) {
      end = index;
      continue;
    }
    if (['api', 'access', 'private'].includes(segments[index]) && segments[index + 1] === 'key') {
      end = index + 1;
      index += 1;
    }
  }
  return end;
};
const isCredentialIdentifier = (identifier) => {
  if (
    /^[A-Z][A-Z0-9]*$/.test(identifier) &&
    /(?:PASSWORD|PASSWD|SECRET|TOKEN|APIKEY|ACCESSKEY|PRIVATEKEY)$/.test(identifier)
  ) {
    return true;
  }
  const segments = identifierSegments(identifier);
  const credentialEnd = credentialFieldEnd(segments);
  if (credentialEnd < 0) return false;
  const suffix = segments.slice(credentialEnd + 1);
  return suffix.length === 0 || suffix.length !== 1 || !credentialMetadataSuffixes.has(suffix[0]);
};
const normalizedAssignmentValue = (rawValue) => {
  let value = rawValue
    .trim()
    .replace(/[,;]\s*$/, '')
    .trim();
  if (value.length >= 2 && ['"', "'", '`'].includes(value[0])) {
    if (value.at(-1) !== value[0]) return null;
    value = value.slice(1, -1);
  }
  return value.toLowerCase();
};
const containsCredentialAssignment = (text) => {
  const assignment = /(^|[^A-Za-z0-9_.-])(?:(['"`])([A-Za-z][A-Za-z0-9_.-]*)\2|([A-Za-z][A-Za-z0-9_.-]*))\s*[:=]\s*/g;
  for (const line of text.split(/\r?\n/)) {
    for (const match of line.matchAll(assignment)) {
      const identifier = match[3] ?? match[4];
      if (!isCredentialIdentifier(identifier)) continue;
      const value = normalizedAssignmentValue(line.slice(match.index + match[0].length));
      if (value === null || !safeCredentialPlaceholders.has(value)) return true;
    }
  }
  return false;
};
const safeExampleEmailDomains = new Set(['example.com', 'example.invalid']);
const containsNonExampleEmail = (text) => {
  const domainLabel = '[A-Z0-9](?:[A-Z0-9-]*[A-Z0-9])?';
  const emailCandidates = new RegExp(`\\b[A-Z0-9._%+-]+@(${domainLabel}(?:\\.${domainLabel})+)`, 'gi');
  for (const match of text.matchAll(emailCandidates)) {
    if (!safeExampleEmailDomains.has(match[1].toLowerCase())) return true;
  }
  return false;
};

const forbiddenChecks = [
  ['IPv4 address', containsAddressLikeIPv4],
  ['non-example email', containsNonExampleEmail],
  ['local user path', (text) => /\b[A-Z]:\\Users\\/i.test(text)],
  ['credential assignment', containsCredentialAssignment],
];
for (const [file, heading] of neutralSections) {
  const section = extractSection(file, heading);
  for (const [label, containsForbiddenValue] of forbiddenChecks) {
    if (containsForbiddenValue(section)) fail(file + ': ' + heading + ' contains ' + label);
  }
}

console.log('PR-054A timestamp allocation validation passed');
