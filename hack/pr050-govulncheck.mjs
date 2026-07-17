#!/usr/bin/env node

import {spawnSync} from 'node:child_process';
import {createHash} from 'node:crypto';
import {readFileSync} from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

const repoRoot = fileURLToPath(new URL('..', import.meta.url));
const policyPath = path.join(repoRoot, 'docs/security/govulncheck-reviewed-findings.json');
const expectedIds = ['GO-2026-4883', 'GO-2026-4887', 'GO-2026-5617', 'GO-2026-5668', 'GO-2026-5746'];
const expectedPolicySha256 = '62c1514dbec86a68c77ece7893e5e7aa41c2f30d68126671efde30a3abbb159a';
const expectedFeedback = {
  'GO-2026-4883': 'https://github.com/golang/vulndb/issues/4922#issuecomment-4976353536',
  'GO-2026-4887': 'https://github.com/golang/vulndb/issues/4921#issuecomment-4976353689',
  'GO-2026-5617': 'https://github.com/golang/vulndb/issues/5993',
  'GO-2026-5668': 'https://github.com/golang/vulndb/issues/5994',
  'GO-2026-5746': 'https://github.com/golang/vulndb/issues/5995',
};
const expectedScanner = {
  protocolVersion: 'v1.0.0',
  name: 'govulncheck',
  version: 'v1.6.0',
  database: 'https://vuln.go.dev',
  scanLevel: 'symbol',
  scanMode: 'source',
};
const expectedModule = {
  path: 'github.com/docker/docker',
  version: 'v28.5.2+incompatible',
};
const expectedTargets = ['./cmd/hub', './cmd/agent/docker', './cmd/agent/kubernetes'];
const requiredRejectedPrefixes = [
  'github.com/docker/docker/plugin',
  'github.com/docker/docker/pkg/authorization',
  'github.com/docker/docker/container',
  'github.com/docker/docker/daemon/archive',
  'github.com/docker/docker/pkg/archive',
];
const allowedMessageKinds = new Set(['config', 'SBOM', 'osv', 'progress', 'finding']);
const maximumExpiry = Date.parse('2026-08-17T00:00:00Z');

function fail(message) {
  throw new Error(message);
}

function assertPlainObject(value, label) {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) {
    fail(`${label} must be an object`);
  }
}

function canonical(value) {
  if (Array.isArray(value)) {
    return `[${value.map(canonical).join(',')}]`;
  }
  if (value !== null && typeof value === 'object') {
    return `{${Object.keys(value)
      .sort()
      .map((key) => `${JSON.stringify(key)}:${canonical(value[key])}`)
      .join(',')}}`;
  }
  return JSON.stringify(value);
}

function equal(actual, expected, label) {
  if (canonical(actual) !== canonical(expected)) {
    fail(`${label} mismatch`);
  }
}

function policySha256(policy) {
  return createHash('sha256').update(canonical(policy)).digest('hex');
}

export function parseJsonStream(text) {
  if (typeof text !== 'string' || text.trim() === '') {
    fail('malformed or truncated govulncheck JSON: empty output');
  }

  const messages = [];
  let start = -1;
  let depth = 0;
  let inString = false;
  let escaped = false;

  try {
    for (let index = 0; index < text.length; index += 1) {
      const character = text[index];
      if (start === -1) {
        if (/\s/u.test(character)) {
          continue;
        }
        if (character !== '{') {
          fail(`unexpected byte at offset ${index}`);
        }
        start = index;
        depth = 1;
        continue;
      }
      if (inString) {
        if (escaped) {
          escaped = false;
        } else if (character === '\\') {
          escaped = true;
        } else if (character === '"') {
          inString = false;
        }
        continue;
      }
      if (character === '"') {
        inString = true;
      } else if (character === '{') {
        depth += 1;
      } else if (character === '}') {
        depth -= 1;
        if (depth === 0) {
          messages.push(JSON.parse(text.slice(start, index + 1)));
          start = -1;
        }
      }
    }
    if (start !== -1 || inString || messages.length === 0) {
      fail('truncated object');
    }
  } catch (error) {
    fail(`malformed or truncated govulncheck JSON: ${error.message}`);
  }

  for (const [index, message] of messages.entries()) {
    assertPlainObject(message, `govulncheck message ${index}`);
    const keys = Object.keys(message);
    if (keys.length !== 1 || !allowedMessageKinds.has(keys[0])) {
      fail(`incompatible govulncheck JSON message ${index}`);
    }
  }
  return messages;
}

function validatePolicy(policy, now) {
  assertPlainObject(policy, 'policy');
  if (policy.schemaVersion !== 1) {
    fail('policy schemaVersion must be 1');
  }
  if (policy.reviewedAt !== '2026-07-17') {
    fail('policy reviewedAt must be 2026-07-17');
  }
  if (policy.owner !== 'EMLO Platform') {
    fail('policy owner must be EMLO Platform');
  }
  if (policy.reviewer !== 'EMLO Platform Owner') {
    fail('policy reviewer must be EMLO Platform Owner');
  }

  const expiry = Date.parse(policy.expiresAt);
  if (!Number.isFinite(expiry) || expiry > maximumExpiry) {
    fail('policy expiresAt is invalid or later than 2026-08-17T00:00:00Z');
  }
  if (expiry <= now.getTime()) {
    fail(`policy expired at ${policy.expiresAt}`);
  }

  equal(policy.scanner, expectedScanner, 'policy scanner');
  equal(policy.module, expectedModule, 'policy Docker module');

  if (!Array.isArray(policy.findings)) {
    fail('policy findings must be an array');
  }
  const findings = new Map();
  for (const finding of policy.findings) {
    assertPlainObject(finding, 'policy finding');
    if (findings.has(finding.id)) {
      fail(`duplicate policy finding ${finding.id}`);
    }
    findings.set(finding.id, finding);
    if (typeof finding.rationale !== 'string' || finding.rationale.trim().length < 40) {
      fail(`policy rationale is missing for ${finding.id}`);
    }
    if (finding.goAdvisory !== `https://pkg.go.dev/vuln/${finding.id}`) {
      fail(`policy Go advisory is invalid for ${finding.id}`);
    }
    if (finding.feedback !== expectedFeedback[finding.id]) {
      fail(`policy submitted Go VulnDB feedback is invalid for ${finding.id}`);
    }
    if (!Array.isArray(finding.affected) || finding.affected.length === 0) {
      fail(`policy affected metadata is missing for ${finding.id}`);
    }
    if (!Array.isArray(finding.traces) || finding.traces.length !== 2) {
      fail(`policy must contain exactly 2 traces for ${finding.id}`);
    }
    for (const trace of finding.traces) {
      if (!Array.isArray(trace) || trace.length === 0 || trace.some((frame) => frame.function !== 'init')) {
        fail(`policy trace is invalid for ${finding.id}`);
      }
      if (trace[0].module !== expectedModule.path || trace[0].version !== expectedModule.version) {
        fail(`policy trace Docker module mismatch for ${finding.id}`);
      }
    }
  }
  equal([...findings.keys()].sort(), [...expectedIds].sort(), 'policy finding IDs');

  assertPlainObject(policy.dependencyDefense, 'policy dependencyDefense');
  equal(policy.dependencyDefense.targets, expectedTargets, 'policy dependency targets');
  if (!Array.isArray(policy.dependencyDefense.rejectedPackagePrefixes)) {
    fail('policy rejectedPackagePrefixes must be an array');
  }
  for (const prefix of requiredRejectedPrefixes) {
    if (!policy.dependencyDefense.rejectedPackagePrefixes.includes(prefix)) {
      fail(`policy is missing affected package prefix ${prefix}`);
    }
  }
  const actualPolicySha256 = policySha256(policy);
  if (actualPolicySha256 !== expectedPolicySha256) {
    fail(`policy integrity SHA-256 mismatch: expected ${expectedPolicySha256}, got ${actualPolicySha256}`);
  }
  return findings;
}

function normalizeTrace(trace) {
  return trace.map((frame) => ({
    module: frame.module,
    ...(frame.version === undefined ? {} : {version: frame.version}),
    package: frame.package,
    function: frame.function,
    file: frame.position?.filename,
  }));
}

function validateConfig(messages, policy) {
  const configs = messages.filter((message) => message.config);
  if (configs.length !== 1) {
    fail(`expected exactly one scanner config, got ${configs.length}`);
  }
  const config = configs[0].config;
  const actual = {
    protocolVersion: config.protocol_version,
    name: config.scanner_name,
    version: config.scanner_version,
    database: config.db,
    scanLevel: config.scan_level,
    scanMode: config.scan_mode,
  };
  equal(actual, policy.scanner, 'scanner config');
}

function validateSbom(messages, policy) {
  const sboms = messages.filter((message) => message.SBOM);
  if (sboms.length !== 1 || !Array.isArray(sboms[0].SBOM.modules)) {
    fail(`expected exactly one compatible SBOM, got ${sboms.length}`);
  }
  const dockerModules = sboms[0].SBOM.modules.filter((module) => module.path === policy.module.path);
  if (
    dockerModules.length !== 1 ||
    dockerModules[0].path !== policy.module.path ||
    dockerModules[0].version !== policy.module.version
  ) {
    fail('Docker module mismatch');
  }
}

function validateOsvMetadata(messages, findings) {
  for (const id of expectedIds) {
    const osvMessages = messages.filter((message) => message.osv?.id === id);
    if (osvMessages.length !== 1) {
      fail(`expected exactly one OSV record for ${id}, got ${osvMessages.length}`);
    }
    const osv = osvMessages[0].osv;
    if (canonical(osv.affected) !== canonical(findings.get(id).affected)) {
      fail(`OSV affected metadata mismatch for ${id}`);
    }
    if (osv.database_specific?.url !== findings.get(id).goAdvisory || osv.withdrawn !== undefined) {
      fail(`OSV advisory metadata mismatch for ${id}`);
    }
  }
}

export function evaluateScan(processResult, policy, {now = new Date()} = {}) {
  if (
    processResult?.error ||
    processResult?.signal !== null ||
    processResult?.status !== 0 ||
    typeof processResult?.stdout !== 'string'
  ) {
    const detail = processResult?.error?.message ?? processResult?.stderr?.trim() ?? 'unexpected exit';
    fail(`govulncheck process failed: ${detail}`);
  }
  if (processResult.stderr?.trim()) {
    fail('govulncheck process failed: unexpected stderr output');
  }

  const findings = validatePolicy(policy, now);
  const messages = parseJsonStream(processResult.stdout);
  validateConfig(messages, policy);
  validateSbom(messages, policy);
  validateOsvMetadata(messages, findings);

  const reachableById = new Map(expectedIds.map((id) => [id, []]));
  const informationalIds = new Set();
  for (const message of messages.filter((candidate) => candidate.finding)) {
    const finding = message.finding;
    if (typeof finding.osv !== 'string' || !Array.isArray(finding.trace) || finding.trace.length === 0) {
      fail('incompatible govulncheck finding');
    }
    const isReachable = finding.trace.some((frame) => typeof frame.function === 'string');
    if (!isReachable) {
      if (!findings.has(finding.osv)) {
        informationalIds.add(finding.osv);
      }
      continue;
    }
    if (!findings.has(finding.osv)) {
      fail(`unexpected reachable finding ${finding.osv}`);
    }
    if (finding.trace.some((frame) => frame.function !== 'init')) {
      fail(`reachable trace mismatch: only reviewed init functions may be accepted for ${finding.osv}`);
    }
    reachableById.get(finding.osv).push(normalizeTrace(finding.trace));
  }

  for (const id of expectedIds) {
    const actualTraces = reachableById.get(id);
    if (actualTraces.length !== 2) {
      fail(`expected 2 reachable traces for ${id}, got ${actualTraces.length}`);
    }
    const expectedTraces = findings.get(id).traces.map(normalizeTrace);
    const actualCanonical = actualTraces.map(canonical).sort();
    const expectedCanonical = expectedTraces.map(canonical).sort();
    if (canonical(actualCanonical) !== canonical(expectedCanonical)) {
      fail(`reachable trace mismatch for ${id}`);
    }
  }

  const informational = [...informationalIds].sort();
  return {
    acceptedIds: [...expectedIds],
    informationalIds: informational,
    summary: [
      `accepted reviewed risk: ${expectedIds.join(', ')}`,
      informational.length > 0
        ? `informational not called: ${informational.join(', ')}`
        : 'informational not called: none',
    ].join('\n'),
  };
}

export function validateDependencyOutputs(outputs, policy) {
  for (const target of policy.dependencyDefense.targets) {
    const output = outputs.get(target);
    if (typeof output !== 'string') {
      fail(`missing dependency output for ${target}`);
    }
    for (const packagePath of output.split(/\r?\n/u).filter(Boolean)) {
      const rejectedPrefix = policy.dependencyDefense.rejectedPackagePrefixes.find(
        (prefix) => packagePath === prefix || packagePath.startsWith(`${prefix}/`)
      );
      if (rejectedPrefix) {
        fail(`affected Moby daemon package compiled into ${target}: ${packagePath}`);
      }
    }
  }
}

function defaultRunner(command, args) {
  return spawnSync(command, args, {
    cwd: repoRoot,
    encoding: 'utf8',
    maxBuffer: 128 * 1024 * 1024,
    windowsHide: true,
  });
}

export function runGate({
  policy = JSON.parse(readFileSync(policyPath, 'utf8')),
  now = new Date(),
  runner = defaultRunner,
} = {}) {
  const scanResult = evaluateScan(runner('govulncheck', ['-format=json', './...']), policy, {now});
  const dependencyOutputs = new Map();
  for (const target of policy.dependencyDefense.targets) {
    const result = runner('go', ['list', '-deps', '-buildvcs=false', target]);
    if (result?.error || result?.signal !== null || result?.status !== 0 || typeof result?.stdout !== 'string') {
      const detail = result?.error?.message ?? result?.stderr?.trim() ?? 'unexpected exit';
      fail(`go list dependency check failed for ${target}: ${detail}`);
    }
    dependencyOutputs.set(target, result.stdout);
  }
  validateDependencyOutputs(dependencyOutputs, policy);
  return {
    ...scanResult,
    summary: `${scanResult.summary}\ndependency defense passed for ${policy.dependencyDefense.targets.length} shipped binaries`,
  };
}

const invokedPath = process.argv[1] ? path.resolve(process.argv[1]) : '';
if (invokedPath === fileURLToPath(import.meta.url)) {
  try {
    console.log(runGate().summary);
  } catch (error) {
    console.error(`Go vulnerability gate failed closed: ${error.message}`);
    process.exitCode = 1;
  }
}
