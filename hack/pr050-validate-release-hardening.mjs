#!/usr/bin/env node

import {spawnSync} from 'node:child_process';
import {existsSync, readdirSync, readFileSync, statSync} from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

const repoRoot = fileURLToPath(new URL('..', import.meta.url));
const localJwtSecret = 'bG9jYWwtand0LXNlY3JldC1wbGFjZWhvbGRlci0zMi1ieXRlcw==';

const requiredFiles = [
  '.github/workflows/community-release-hardening.yaml',
  '.golangci.release.yml',
  'docs/adr/0050-community-release-hardening.md',
  'docs/fork/PR-050_COMMUNITY_RELEASE_HARDENING.md',
  'docs/fork/FORK_DIFF_INDEX.md',
  'docs/release/community-release-readiness.md',
  'docs/security/release-hardening-checklist.md',
  'docs/operations/operator-smoke-test.md',
  'docs/upgrade/community-release-upgrade-checklist.md',
  'docs/architecture/community-release-overview.md',
  'docs/api/community-release-api-index.md',
  'docs/upstream/contribution-breakdown.md',
  'examples/community-e2e/README.md',
  'examples/community-e2e/flow.fixture.json',
  'examples/community-e2e/run-demo.mjs',
  'examples/community-e2e/live-demo.mjs',
  'examples/community-e2e/compose.yaml',
  'hack/e2e-smoke-test.mjs',
  'hack/pr050-license-scan.mjs',
  'internal/handlers/pr050_community_live_demo_test.go',
];

const secretScanFiles = requiredFiles.filter(
  (file) => file.startsWith('docs/') || file.startsWith('examples/') || file.startsWith('.github/workflows/')
);

function fail(message) {
  throw new Error(message);
}

function readRel(relPath) {
  return readFileSync(path.join(repoRoot, relPath), 'utf8');
}

function assertFile(relPath) {
  if (!existsSync(path.join(repoRoot, relPath))) {
    fail(`missing required file: ${relPath}`);
  }
}

for (const file of requiredFiles) {
  assertFile(file);
}

const prDoc = readRel('docs/fork/PR-050_COMMUNITY_RELEASE_HARDENING.md');
for (const heading of [
  'Database/schema impact',
  'Public API impact',
  'Frontend/UI impact',
  'Agent/protocol impact',
  'Feature-flag impact',
  'Security impact',
  'Backward-compatibility impact',
  'Upgrade/downgrade impact',
]) {
  if (!prDoc.includes(`### ${heading}`)) {
    fail(`PR-050 report missing impact heading: ${heading}`);
  }
}

const index = readRel('docs/fork/FORK_DIFF_INDEX.md');
if (!index.includes('### PR-050 - Community release hardening')) {
  fail('fork diff index missing PR-050 entry');
}

const workflow = readRel('.github/workflows/community-release-hardening.yaml');
if (/^\s+paths:\s*$/m.test(workflow)) {
  fail(
    'release workflow must not path-filter pull_request or push runs; release gates need to run for runtime, migration, and dependency-manifest changes'
  );
}

for (const requiredWorkflowText of [
  'DISTR_TARGET_ID',
  'agent_capabilities,agent_task_leases,step_events',
  'go vet ./...',
  'golangci/golangci-lint-action',
  "version: 'v2.12.2'",
  'args: --config=.golangci.release.yml ./...',
  'pnpm run lint',
  'node hack/pr050-license-scan.mjs',
  'node examples/community-e2e/live-demo.mjs --require-running-hub',
  "DISTR_DEMO_DISPOSABLE_HUB: 'true'",
]) {
  if (!workflow.includes(requiredWorkflowText)) {
    fail(`release workflow missing required gate text: ${requiredWorkflowText}`);
  }
}

const releaseLintConfig = readRel('.golangci.release.yml');
for (const requiredReleaseLintText of [
  'default: none',
  '- asciicheck',
  '- bidichk',
  '- errcheck',
  '- gocheckcompilerdirectives',
  '- govet',
  '- ineffassign',
]) {
  if (!releaseLintConfig.includes(requiredReleaseLintText)) {
    fail(`release golangci config missing required correctness gate: ${requiredReleaseLintText}`);
  }
}
for (const forbiddenReleaseLintText of [
  '- dupl',
  '- lll',
  '- gci',
  '- gofmt',
  '- gofumpt',
  '- goimports',
  'formatters:',
]) {
  if (releaseLintConfig.includes(forbiddenReleaseLintText)) {
    fail(`release golangci config should not inherit broad style/debt gate: ${forbiddenReleaseLintText}`);
  }
}
const licenseScanner = readRel('hack/pr050-license-scan.mjs');
for (const requiredLicenseScanText of [
  'scanNodePackages',
  'scanGoModules',
  "go', ['list', '-m', '-json', 'all']",
  'direct Go modules missing license files',
  'direct Node dependencies missing license metadata',
]) {
  if (!licenseScanner.includes(requiredLicenseScanText)) {
    fail(`license scanner missing required Node+Go enforcement text: ${requiredLicenseScanText}`);
  }
}

const liveDemo = readRel('examples/community-e2e/live-demo.mjs');
for (const forbiddenDemoText of [
  'TestPR050CommunityLiveReleaseToTaskDemo',
  'DISTR_TEST_DATABASE_URL',
  'E2eSmoke123!',
  'Math.random().toString(16)',
  'process.env.CI',
  'process.env.DATABASE_URL ?? demoDatabaseURL',
]) {
  if (liveDemo.includes(forbiddenDemoText)) {
    fail(`live demo still contains forbidden direct DB/test hook text: ${forbiddenDemoText}`);
  }
}
for (const requiredDemoText of [
  'runLiveReleaseToTaskJourney',
  'executeHttpCheckStep',
  "'/api/v1/release-bundles'",
  "'/api/v1/deployment-plans'",
  "'/api/v1/agent/login'",
  'step-runs/${step.stepRunId}/events',
  "executionLocation: 'target'",
  'demoComposeProject',
  "'-f', demoComposeFile",
  'randomBytes(8)',
  'randomBytes(24)',
  'DISTR_DEMO_DISPOSABLE_HUB',
  'DISTR_DEMO_ALLOW_SHARED_HUB',
  'DISTR_DEMO_HOST',
  'DISTR_DEMO_DATABASE_URL',
  'cleanupDemoOrganization',
  'demo organization still accessible',
  '/api/v1/organization',
  "method: 'DELETE'",
  'organizationName: demoName',
]) {
  if (!liveDemo.includes(requiredDemoText)) {
    fail(`live demo missing required API-only live journey or compose isolation text: ${requiredDemoText}`);
  }
}

const smokeTest = readRel('hack/e2e-smoke-test.mjs');
for (const forbiddenSmokeText of ['E2eSmoke123!', 'Math.random().toString(16)']) {
  if (smokeTest.includes(forbiddenSmokeText)) {
    fail(`smoke test still contains fixed or weak credential text: ${forbiddenSmokeText}`);
  }
}
for (const requiredSmokeText of [
  'randomBytes(8)',
  'randomBytes(24)',
  'cleanupDemoOrganization',
  '/api/v1/organization',
  "method: 'DELETE'",
  'organizationName: `E2E Smoke ${RUN_ID}`',
]) {
  if (!smokeTest.includes(requiredSmokeText)) {
    fail(`smoke test missing random credential or cleanup text: ${requiredSmokeText}`);
  }
}

const compose = readRel('examples/community-e2e/compose.yaml');
for (const requiredComposeText of [
  '127.0.0.1:15432:5432',
  'POSTGRES_PASSWORD: local',
  'postgres:',
  'mailpit:',
  'storage:',
]) {
  if (!compose.includes(requiredComposeText)) {
    fail(`community demo compose missing isolated dependency text: ${requiredComposeText}`);
  }
}

function markdownFiles(relDir) {
  const root = path.join(repoRoot, relDir);
  const out = [];
  function visit(dir) {
    for (const entry of readdirSync(dir)) {
      const full = path.join(dir, entry);
      const stat = statSync(full);
      if (stat.isDirectory()) {
        visit(full);
      } else if (entry.endsWith('.md')) {
        out.push(path.relative(repoRoot, full).replaceAll(path.sep, '/'));
      }
    }
  }
  visit(root);
  return out;
}

const mdFiles = [...markdownFiles('docs'), ...markdownFiles('examples/community-e2e')].filter((file) =>
  /PR-050|release|community-e2e|operator|upstream|security|upgrade|architecture|api/.test(file)
);

for (const file of mdFiles) {
  const text = readRel(file);
  const baseDir = path.dirname(path.join(repoRoot, file));
  const links = text.matchAll(/\[[^\]]+\]\(([^)]+)\)/g);
  for (const match of links) {
    const rawTarget = match[1].split(/\s+/)[0].replace(/^<|>$/g, '');
    if (!rawTarget || rawTarget.startsWith('#') || /^[a-z]+:/i.test(rawTarget)) {
      continue;
    }
    const targetPath = rawTarget.split('#')[0];
    if (!targetPath) {
      continue;
    }
    if (!existsSync(path.resolve(baseDir, targetPath))) {
      fail(`broken markdown link in ${file}: ${rawTarget}`);
    }
  }
}

const demo = spawnSync(process.execPath, ['examples/community-e2e/run-demo.mjs', '--json'], {
  cwd: repoRoot,
  encoding: 'utf8',
});
if (demo.status !== 0) {
  fail(`community fixture verifier failed:\n${demo.stderr || demo.stdout}`);
}
const demoResult = JSON.parse(demo.stdout);
if (!demoResult.ok || !demoResult.flowDigest) {
  fail('community fixture verifier did not report ok=true with a flow digest');
}

const literalSecretPatterns = [
  {name: 'aws secret access key', pattern: /\bAWS_SECRET_ACCESS_KEY\b\s*[:=]\s*([^\s#]+)/g},
  {name: 'github token', pattern: /\bGITHUB_TOKEN\b\s*[:=]\s*([^\s#]+)/g},
  {name: 'gitlab token', pattern: /\bGITLAB_TOKEN\b\s*[:=]\s*([^\s#]+)/g},
  {
    name: 'credential environment variable',
    pattern: /\b[A-Z0-9_]*(?:PASSWORD|SECRET|TOKEN|ACCESS_KEY|PRIVATE_KEY)[A-Z0-9_]*\b\s*[:=]\s*([^\s#]+)/g,
  },
  {name: 'password', pattern: /\bpassword\b\s*[:=]\s*([^\s#]+)/gi},
  {name: 'secret', pattern: /\bsecret\b\s*[:=]\s*([^\s#]+)/gi},
];

function normalizeCredentialValue(value) {
  return value
    .trim()
    .replace(/^[`'"]|[`'"]$/g, '')
    .replace(/[,;.)]+$/g, '')
    .replace(/^[`'"]|[`'"]$/g, '');
}

function isAllowedCredentialPlaceholder(value) {
  const normalized = normalizeCredentialValue(value);
  return (
    /^<placeholder>$/i.test(normalized) ||
    /^placeholder$/i.test(normalized) ||
    /^local$/i.test(normalized) ||
    normalized === localJwtSecret ||
    /^process\.env\.[A-Za-z0-9_]+$/.test(normalized) ||
    /^secret-ref:[A-Za-z0-9._/-]+$/.test(normalized) ||
    /^\[REDACTED\]$/.test(normalized) ||
    /^\$\{\{/.test(normalized)
  );
}

function findPlainCredentials(text) {
  const findings = [];
  for (const {name, pattern} of literalSecretPatterns) {
    pattern.lastIndex = 0;
    for (const match of text.matchAll(pattern)) {
      const value = match[1];
      if (!isAllowedCredentialPlaceholder(value)) {
        findings.push({name, value: normalizeCredentialValue(value)});
      }
    }
  }
  return findings;
}

for (const allowed of [
  'password: <placeholder>',
  'password=placeholder',
  'password: local',
  'secret: secret-ref:demo-api-token',
  'secret=[REDACTED]',
  'POSTGRES_PASSWORD: local',
  `JWT_SECRET: ${localJwtSecret}`,
  'JWT_SECRET: process.env.JWT_SECRET',
  'RUSTFS_SECRET_KEY: local',
  'DISTR_TARGET_SECRET: local',
  'GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}',
]) {
  const findings = findPlainCredentials(allowed);
  if (findings.length > 0) {
    fail(`secret scanner rejected allowed placeholder: ${allowed}`);
  }
}

for (const denied of [
  'password: hunter2',
  'password=plain-text-value',
  'secret: abc123',
  'secret=raw-token',
  'AWS_SECRET_ACCESS_KEY=AKIAEXAMPLE',
  'GITHUB_TOKEN=ghp_exampletoken',
  'POSTGRES_PASSWORD: hunter2',
  'JWT_SECRET: raw-secret',
  'RUSTFS_SECRET_KEY=abc123',
]) {
  const findings = findPlainCredentials(denied);
  if (findings.length === 0) {
    fail(`secret scanner missed plaintext fixture: ${denied}`);
  }
}

for (const file of secretScanFiles) {
  const findings = findPlainCredentials(readRel(file));
  if (findings.length > 0) {
    fail(`possible plaintext credential in ${file}: ${findings.map((finding) => finding.name).join(', ')}`);
  }
}

console.log('PR-050 release hardening validation passed');
console.log(`Community demo digest: ${demoResult.flowDigest}`);
