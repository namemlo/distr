#!/usr/bin/env node

import {spawnSync} from 'node:child_process';
import {createHash} from 'node:crypto';
import {existsSync, readdirSync, readFileSync, statSync} from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

const repoRoot = fileURLToPath(new URL('..', import.meta.url));
const localJwtSecret = 'bG9jYWwtand0LXNlY3JldC1wbGFjZWhvbGRlci0zMi1ieXRlcw==';
const expectedVulnerabilityPolicySha256 = '3be319e8475ddd3e281247f155902e584f2f8a2b60acf9be306da2d68e0acc9f';
const expectedVulnerabilityIds = ['GO-2026-4883', 'GO-2026-4887', 'GO-2026-5617', 'GO-2026-5668', 'GO-2026-5746'];
const expectedFeedback = {
  'GO-2026-4883': 'https://github.com/golang/vulndb/issues/4922#issuecomment-4976353536',
  'GO-2026-4887': 'https://github.com/golang/vulndb/issues/4921#issuecomment-4976353689',
  'GO-2026-5617': 'https://github.com/golang/vulndb/issues/5993',
  'GO-2026-5668': 'https://github.com/golang/vulndb/issues/5994',
  'GO-2026-5746': 'https://github.com/golang/vulndb/issues/5995',
};
const expectedDependencyPrefixes = [
  'github.com/docker/docker/plugin',
  'github.com/docker/docker/api/server/router/plugin',
  'github.com/docker/docker/pkg/authorization',
  'github.com/docker/docker/api/server/middleware',
  'github.com/docker/docker/container',
  'github.com/docker/docker/api/server/router/container',
  'github.com/docker/docker/daemon/archive',
  'github.com/docker/docker/pkg/archive',
  'github.com/docker/docker/pkg/chrootarchive',
];
const expectedVulnerabilityStep = [
  '      - name: Run Go vulnerability scan',
  '        run: |',
  '          go install golang.org/x/vuln/cmd/govulncheck@v1.6.0',
  '          node hack/pr050-govulncheck.mjs',
].join('\n');

const requiredFiles = [
  '.github/workflows/community-release-hardening.yaml',
  '.golangci.release.yml',
  'Dockerfile.docker-agent',
  'Dockerfile.kubernetes-agent',
  'docs/adr/0050-community-release-hardening.md',
  'docs/fork/PR-050_COMMUNITY_RELEASE_HARDENING.md',
  'docs/fork/FORK_DIFF_INDEX.md',
  'docs/release/community-release-readiness.md',
  'docs/security/govulncheck-reviewed-findings.json',
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
  'hack/pr050-govulncheck.mjs',
  'hack/pr050-govulncheck.test.mjs',
  'hack/pr050-license-scan.mjs',
  'hack/pr050-validate-control-plane-evidence.mjs',
  'hack/pr050-validate-control-plane-evidence.test.mjs',
  'hack/control-plane-adopter-term-scan.mjs',
  'hack/control-plane-adopter-term-scan.test.mjs',
  'docs/api/operator-control-plane-api.md',
  'deploy/jen' + 'kins/Jen' + 'kinsfile.hub-image',
  'deploy/jen' + 'kins/publish-hub-image.sh',
  'deploy/server-docker-compose/deploy.sh',
  'internal/handlers/pr050_community_live_demo_test.go',
  'go.mod',
  'mise.toml',
];

const secretScanFiles = requiredFiles.filter(
  (file) => file.startsWith('docs/') || file.startsWith('examples/') || file.startsWith('.github/workflows/')
);

function fail(message) {
  throw new Error(message);
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

export function validateVulnerabilityWorkflow(workflowText) {
  const lines = workflowText.replace(/\r\n/gu, '\n').split('\n');
  const starts = lines
    .map((line, index) => (line === '      - name: Run Go vulnerability scan' ? index : -1))
    .filter((index) => index >= 0);
  if (starts.length !== 1) {
    fail(`vulnerability step must exactly contain the reviewed commands; found ${starts.length} named steps`);
  }
  const start = starts[0];
  let jobStart = start - 1;
  while (jobStart >= 0 && !/^  [A-Za-z0-9_-]+:\s*$/u.test(lines[jobStart])) {
    jobStart -= 1;
  }
  if (jobStart < 0 || lines[jobStart] !== '  release-gates:') {
    fail('vulnerability step must be inside the full release-gates job');
  }
  let jobEnd = jobStart + 1;
  while (jobEnd < lines.length && !/^  [A-Za-z0-9_-]+:\s*$/u.test(lines[jobEnd])) {
    jobEnd += 1;
  }
  const forbiddenJobControl =
    /^    (?:"(?:if|continue-on-error)"|'(?:if|continue-on-error)'|(?:if|continue-on-error))\s*:/u;
  if (lines.slice(jobStart + 1, jobEnd).some((line) => forbiddenJobControl.test(line))) {
    fail('release job must not define if or continue-on-error');
  }
  let end = start + 1;
  while (end < lines.length && !lines[end].startsWith('      - ')) {
    end += 1;
  }
  while (end > start && lines[end - 1] === '') {
    end -= 1;
  }
  if (lines.slice(start, end).join('\n') !== expectedVulnerabilityStep) {
    fail('vulnerability step must exactly contain the pinned install and reviewed wrapper with no conditions');
  }
}

export function validateVulnerabilityPolicy(policy) {
  if (
    policy.schemaVersion !== 1 ||
    policy.reviewedAt !== '2026-07-17' ||
    policy.expiresAt !== '2026-08-17T00:00:00Z' ||
    policy.owner !== 'Distr control-plane maintainers' ||
    policy.reviewer !== 'Distr community fork maintainers'
  ) {
    fail('Go vulnerability policy review, expiry, owner, or reviewer metadata changed');
  }
  if (
    policy.scanner?.protocolVersion !== 'v1.0.0' ||
    policy.scanner?.name !== 'govulncheck' ||
    policy.scanner?.version !== 'v1.6.0' ||
    policy.scanner?.database !== 'https://vuln.go.dev' ||
    policy.module?.path !== 'github.com/docker/docker' ||
    policy.module?.version !== 'v28.5.2+incompatible'
  ) {
    fail('Go vulnerability policy scanner or Docker module contract changed');
  }
  const policyIds = policy.findings?.map((finding) => finding.id);
  if (
    !Array.isArray(policyIds) ||
    new Set(policyIds).size !== expectedVulnerabilityIds.length ||
    canonical(policyIds) !== canonical(expectedVulnerabilityIds)
  ) {
    fail('Go vulnerability policy must contain the five reviewed IDs in exact order');
  }
  for (const finding of policy.findings) {
    if (finding.goAdvisory !== `https://pkg.go.dev/vuln/${finding.id}`) {
      fail(`Go vulnerability policy advisory URL changed for ${finding.id}`);
    }
    if (finding.feedback !== expectedFeedback[finding.id]) {
      fail(`Go vulnerability policy submitted feedback URL changed for ${finding.id}`);
    }
  }
  if (canonical(policy.dependencyDefense?.rejectedPackagePrefixes) !== canonical(expectedDependencyPrefixes)) {
    fail('Go vulnerability policy dependency-family set changed');
  }
  const policySha256 = createHash('sha256').update(canonical(policy)).digest('hex');
  if (policySha256 !== expectedVulnerabilityPolicySha256) {
    fail(
      `Go vulnerability policy integrity SHA-256 mismatch: expected ${expectedVulnerabilityPolicySha256}, got ${policySha256}`
    );
  }
}

export function validateReleaseEvidenceWorkflow(workflowText) {
  if (workflowText.includes('"packages": []') || workflowText.includes("'packages': []")) {
    fail('release workflow must not accept an empty-package SPDX fallback');
  }
  for (const requiredText of [
    'Build release evidence image once',
    'anchore/sbom-action@e22c389904149dbc22b58101806040fa8d37a610',
    'output-file: dist/release-evidence/image.spdx.json',
    'sbom.packages.length === 0',
    'https://in-toto.io/Statement/v1',
    'https://slsa.dev/provenance/v1',
    'id-token: write',
    'cosign sign-blob',
    'cosign verify-blob',
    'provenance.sigstore.json',
    'control-plane-migration-matrix.ps1',
    "-OutputPath 'work/release-evidence/migration-postgresql-16.14.json'",
    "-OutputPath 'work/release-evidence/migration-postgresql-18.4.json'",
    'postgres:16.14-alpine3.23',
    'postgres:18.4-alpine3.23',
    'node hack/pr050-validate-control-plane-evidence.mjs migration',
    'node hack/pr050-validate-control-plane-evidence.mjs postdeploy',
    'node hack/pr050-validate-control-plane-evidence.mjs ui',
    'examples/control-plane-e2e/run.mjs --mode clean --json',
    'playwright.control-plane.config.ts',
    "schemaVersion: 'distr.control-plane-release-acceptance/v1'",
    'operator: live(',
    'api: live(',
    'ui: {',
    'flag: {',
    'audit: live(',
    'node --test hack/control-plane-adopter-term-scan.test.mjs',
    'node hack/control-plane-adopter-term-scan.mjs --base',
    'docs/api/operator-control-plane-api.md',
    'work/release-evidence',
    'if-no-files-found: error',
  ]) {
    if (!workflowText.includes(requiredText)) {
      fail(`release workflow missing fail-closed evidence contract: ${requiredText}`);
    }
  }
  const releaseImageBuilds = workflowText.match(/^\s+docker build \\\s*$/gmu) ?? [];
  if (releaseImageBuilds.length !== 1) {
    fail(`release workflow must build the evidence image exactly once; found ${releaseImageBuilds.length}`);
  }
  if (/-OutputPath\s+['"]dist\//u.test(workflowText)) {
    fail('release workflow must stage migration evidence below work before copying it to dist');
  }
}

function parseTomlString(value, context) {
  const trimmed = value.trim();
  const quote = trimmed[0];
  if ((quote !== '"' && quote !== "'") || trimmed.at(-1) !== quote) {
    fail(`${context} must be a literal TOML string`);
  }
  const inner = trimmed.slice(1, -1);
  if (quote === "'") {
    if (inner.includes("'")) fail(`${context} contains an invalid literal string`);
    return inner;
  }
  try {
    return JSON.parse(trimmed);
  } catch {
    fail(`${context} contains an invalid basic string`);
  }
}

function parseTomlStringList(value, context) {
  const trimmed = value.trim();
  if (!trimmed.startsWith('[')) return [parseTomlString(trimmed, context)];
  if (!trimmed.endsWith(']')) fail(`${context} must be a one-line string array`);
  const body = trimmed.slice(1, -1).trim();
  if (body === '') return [];
  const values = [];
  let start = 0;
  let quote = '';
  let escaped = false;
  for (let index = 0; index <= body.length; index += 1) {
    const character = body[index];
    if (quote === '"' && character === '\\' && !escaped) {
      escaped = true;
      continue;
    }
    if ((character === '"' || character === "'") && !escaped) {
      if (quote === '') quote = character;
      else if (quote === character) quote = '';
    }
    escaped = false;
    if ((character === ',' && quote === '') || index === body.length) {
      const item = body.slice(start, index).trim();
      if (item === '') fail(`${context} contains an empty dependency`);
      values.push(parseTomlString(item, context));
      start = index + 1;
    }
  }
  if (quote !== '') fail(`${context} contains an unterminated string`);
  return values;
}

function stripTomlComment(line) {
  let quote = '';
  let escaped = false;
  for (let index = 0; index < line.length; index += 1) {
    const character = line[index];
    if (quote === '"' && character === '\\' && !escaped) {
      escaped = true;
      continue;
    }
    if ((character === '"' || character === "'") && !escaped) {
      if (quote === '') quote = character;
      else if (quote === character) quote = '';
    } else if (character === '#' && quote === '') {
      return line.slice(0, index);
    }
    escaped = false;
  }
  return line;
}

export function parseMiseTasks(miseText) {
  const tasks = new Map();
  let currentTask;
  for (const rawLine of miseText.replace(/\r\n/gu, '\n').split('\n')) {
    const line = stripTomlComment(rawLine).trim();
    if (line === '') continue;
    const section = line.match(/^\[tasks\.(?:"([^"]+)"|([A-Za-z0-9_-]+))\]$/u);
    if (section) {
      const name = section[1] ?? section[2];
      if (tasks.has(name)) fail(`mise task ${name} is defined more than once`);
      currentTask = {depends: [], name, properties: new Set(), run: undefined};
      tasks.set(name, currentTask);
      continue;
    }
    if (line.startsWith('[')) {
      currentTask = undefined;
      continue;
    }
    if (!currentTask) continue;
    const property = line.match(/^([A-Za-z0-9_-]+)\s*=\s*(.+)$/u);
    if (!property) fail(`mise task ${currentTask.name} contains an unsupported multiline property`);
    const [, key, value] = property;
    if (currentTask.properties.has(key)) fail(`mise task ${currentTask.name} repeats property ${key}`);
    currentTask.properties.add(key);
    if (key === 'run') {
      const trimmed = value.trim();
      currentTask.run = trimmed.startsWith('[')
        ? parseTomlStringList(trimmed, `mise task ${currentTask.name} run`)
        : trimmed.startsWith('"') || trimmed.startsWith("'")
          ? parseTomlString(trimmed, `mise task ${currentTask.name} run`)
          : trimmed;
    } else if (key === 'depends') {
      currentTask.depends = parseTomlStringList(value, `mise task ${currentTask.name} depends`);
    }
  }
  return tasks;
}

export function validateMiseReleaseTasks(miseText) {
  const tasks = parseMiseTasks(miseText);
  const tidyCheck = tasks.get('go-tidy-check');
  if (!tidyCheck || tidyCheck.run !== 'go mod tidy -diff') {
    fail('mise go-tidy-check must run exactly go mod tidy -diff');
  }
  if (tidyCheck.depends.length !== 0) {
    fail('mise go-tidy-check must not delegate to another task');
  }

  const releaseTasks = [
    'build:hub:community',
    'build:hub:enterprise',
    'build:agent:docker',
    'build:agent:kubernetes',
    'lint:go',
    'test:go',
  ];
  for (const releaseTask of releaseTasks) {
    if (!tasks.has(releaseTask)) fail(`mise release task is missing: ${releaseTask}`);
    const reachable = new Set();
    const visiting = new Set();
    const visit = (taskName) => {
      if (visiting.has(taskName)) fail(`mise task dependency cycle reaches ${taskName}`);
      if (reachable.has(taskName)) return;
      const task = tasks.get(taskName);
      if (!task) fail(`mise task ${releaseTask} depends on missing task ${taskName}`);
      visiting.add(taskName);
      for (const dependency of task.depends) visit(dependency);
      visiting.delete(taskName);
      reachable.add(taskName);
    };
    visit(releaseTask);
    if (!reachable.has('go-tidy-check')) {
      fail(`${releaseTask} must transitively use the non-mutating go-tidy-check release gate`);
    }
    for (const taskName of reachable) {
      const task = tasks.get(taskName);
      const commands = Array.isArray(task?.run) ? task.run : [task?.run];
      if (commands.some((command) => typeof command === 'string' && /\bmise\s+run\b/u.test(command))) {
        fail(`${releaseTask} must declare task delegation through depends, not mise run`);
      }
      const runsMutatingTidy = commands.some(
        (command) => typeof command === 'string' && command.includes('go mod tidy') && command !== 'go mod tidy -diff'
      );
      if (taskName === 'go-tidy' || runsMutatingTidy) {
        fail(`${releaseTask} must not transitively reach mutating go mod tidy`);
      }
    }
  }
}

function shellFunctionBody(script, name) {
  const normalized = script.replace(/\r\n/gu, '\n');
  const signatures = [...normalized.matchAll(new RegExp(`^${name}\\(\\)\\s*(?:\\{|\\()\\s*$`, 'gmu'))];
  if (signatures.length !== 1) fail(`release shell must define ${name} exactly once`);
  const start = signatures[0].index + signatures[0][0].length;
  const remainder = normalized.slice(start);
  const nextFunction = remainder.search(/^[A-Za-z_][A-Za-z0-9_]*\(\)\s*(?:\{|\()\s*$/mu);
  return remainder.slice(0, nextFunction < 0 ? undefined : nextFunction);
}

function shellStatements(script, name) {
  const statements = [];
  const lines = shellFunctionBody(script, name).split('\n');
  let heredoc;
  let continued = '';
  for (const rawLine of lines) {
    const trimmed = rawLine.trim();
    if (heredoc) {
      if (trimmed === heredoc) heredoc = undefined;
      continue;
    }
    if (trimmed === '' || trimmed.startsWith('#')) continue;
    const line = trimmed.replace(/\s+#.*$/u, '').trim();
    const heredocMatch = line.match(/<<-?['"]?([A-Za-z_][A-Za-z0-9_]*)['"]?/u);
    const fragment = line.endsWith('\\') ? line.slice(0, -1).trimEnd() : line;
    continued = continued === '' ? fragment : `${continued} ${fragment}`;
    if (!line.endsWith('\\')) {
      statements.push(continued.replace(/\s+/gu, ' ').trim());
      continued = '';
    }
    if (heredocMatch) heredoc = heredocMatch[1];
  }
  if (continued !== '') statements.push(continued.replace(/\s+/gu, ' ').trim());
  return statements;
}

function shellUnconditionalStatements(script, name) {
  const statements = shellStatements(script, name);
  const hidden = [];
  const stack = [];
  for (const statement of statements) {
    if (/^(?:fi|esac|done)(?:\s|;|$)/u.test(statement)) stack.pop();
    const conditional = stack.some((block) => block !== 'required-scan-loop');
    if (conditional) hidden.push(statement);
    if (/^if\b.*(?:;\s*then|\bthen)$/u.test(statement)) stack.push('if');
    else if (/^(?:while|until)\b.*(?:;\s*do|\bdo)$/u.test(statement)) stack.push('conditional-loop');
    else if (/^case\b.*\bin$/u.test(statement)) stack.push('case');
    else if (/^for\b.*(?:;\s*do|\bdo)$/u.test(statement)) {
      stack.push(statement === 'for raw in "$scan_one" "$scan_two"; do' ? 'required-scan-loop' : 'conditional-loop');
    }
  }
  return statements.filter((statement) => !hidden.includes(statement));
}

function requireStatement(statements, pattern, message) {
  const matches = statements.filter((statement) => pattern.test(statement));
  if (matches.length !== 1) fail(`${message}; found ${matches.length} executable statements`);
  return statements.indexOf(matches[0]);
}

function rejectEarlySuccessfulTermination(statements, lastRequiredIndex, message) {
  if (statements.slice(0, lastRequiredIndex).some((statement) => /^(?:return|exit)(?: 0)?$/u.test(statement))) {
    fail(message);
  }
}

export function validateDeploymentReleaseShell(deployScript) {
  const clean = shellUnconditionalStatements(deployScript, 'require_source_clean');
  requireStatement(
    clean,
    /^dirty="\$\(git status --porcelain=v1 --untracked-files=all\)" \|\| return$/u,
    'release source-clean gate must inspect tracked and all untracked files'
  );
  if (clean.some((statement) => statement.includes('--untracked-files=no'))) {
    fail('release source-clean gate must not ignore untracked files');
  }

  const build = shellUnconditionalStatements(deployScript, 'build_image');
  const before = requireStatement(
    build,
    /^require_source_clean before \|\| return$/u,
    'build must run the pre-build clean gate'
  );
  const buildImage = requireStatement(build, /^docker build /u, 'build must execute exactly one Docker image build');
  const sbom = requireStatement(
    build,
    /^generate_image_sbom "\$image_ref" "\$commit" \|\| return$/u,
    'build must generate the image SBOM'
  );
  const after = requireStatement(
    build,
    /^require_source_clean after \|\| return$/u,
    'build must run the post-build clean gate'
  );
  if (!(before < buildImage && buildImage < sbom && sbom < after)) {
    fail('release clean gates and image SBOM generation are not in fail-closed build order');
  }
  rejectEarlySuccessfulTermination(build, after, 'build must not terminate successfully before release gates finish');

  const identity = shellUnconditionalStatements(deployScript, 'resolve_local_image_identity');
  requireStatement(
    identity,
    /^raw="\$\(docker image inspect --format '\{\{\.Id\}\}\|\{\{\.Os\}\}\/\{\{\.Architecture\}\}' "\$image_ref"\)" \|\| \{$/u,
    'local image identity must come from an exact Docker ID and platform inspection'
  );

  const imageSbom = shellUnconditionalStatements(deployScript, 'generate_image_sbom');
  requireStatement(
    imageSbom,
    /^syft_version="\$\(mise exec -- syft version -o json\)" \|\| return$/u,
    'image SBOM must verify the mise-pinned Syft executable'
  );
  const scan = requireStatement(
    imageSbom,
    /^mise exec -- syft "docker:\$\{image_id\}" --source-name "\$DISTR_IMAGE" --source-version "\$DISTR_IMAGE_TAG" --platform "\$platform" -o "spdx-json=\$\{raw\}" \|\| \{$/u,
    'image SBOM must scan the immutable local image ID with release identity arguments'
  );
  const firstIdentity = requireStatement(
    imageSbom,
    /^identity="\$\(resolve_local_image_identity "\$image_ref"\)" \|\| return$/u,
    'image SBOM must bind the release tag before scanning'
  );
  const currentIdentity = requireStatement(
    imageSbom,
    /^current_identity="\$\(resolve_local_image_identity "\$image_ref"\)" \|\| return$/u,
    'image SBOM must recheck the release tag after each scan'
  );
  requireStatement(
    imageSbom,
    /^\[\[ "\$current_identity" == "\$identity" \]\] \|\| \{$/u,
    'image SBOM must reject a tag-to-image race'
  );
  if (!(firstIdentity < scan && scan < currentIdentity)) {
    fail('image SBOM identity checks do not enclose the immutable-ID scan');
  }
  if (imageSbom.some((statement) => /^(?:syft |[A-Za-z_][A-Za-z0-9_]*="\$\(syft )/u.test(statement))) {
    fail('image SBOM must not execute an unpinned Syft from PATH');
  }
}

export function validatePublisherReleaseShell(publishScript) {
  const publish = shellUnconditionalStatements(publishScript, 'publish');
  const build = requireStatement(
    publish,
    /^GOOS=linux GOARCH=amd64 CGO_ENABLED=0 "\$DEPLOY_SCRIPT" build \|\| return$/u,
    'publisher must execute the reviewed deployment build'
  );
  const inspectTag = requireStatement(
    publish,
    /^inspect_image_identity "\$tagged_image" "\$expected_source" \|\| return$/u,
    'publisher must inspect the locally tagged image'
  );
  const push = requireStatement(
    publish,
    /^"\$DEPLOY_SCRIPT" push \|\| return$/u,
    'publisher must execute the reviewed push'
  );
  const digest = requireStatement(
    publish,
    /^digest="\$\(resolve_remote_digest\)" \|\| return$/u,
    'publisher must resolve the immutable ECR digest'
  );
  const pull = requireStatement(
    publish,
    /^docker pull "\$digest_ref" >\/dev\/null \|\| return$/u,
    'publisher must pull by digest'
  );
  const inspectDigest = requireStatement(
    publish,
    /^inspect_image_identity "\$digest_ref" "\$expected_source" \|\| return$/u,
    'publisher must inspect the pulled digest image'
  );
  const bindingChecks = publish
    .map((statement, index) => ({index, statement}))
    .filter(({statement}) => /^require_image_sbom_binding /u.test(statement));
  if (bindingChecks.length !== 2) {
    fail(`publisher must validate the image SBOM binding before and after push; found ${bindingChecks.length}`);
  }
  const provenance = requireStatement(
    publish,
    /^provenance="\$\(write_provenance_attestation /u,
    'publisher must generate provenance after digest verification'
  );
  const handoff = requireStatement(publish, /^write_exact_handoff /u, 'publisher must write the exact release handoff');
  if (
    !(
      build < inspectTag &&
      inspectTag < bindingChecks[0].index &&
      bindingChecks[0].index < push &&
      push < digest &&
      digest < pull &&
      pull < inspectDigest &&
      inspectDigest < bindingChecks[1].index &&
      bindingChecks[1].index < provenance &&
      provenance < handoff
    )
  ) {
    fail('publisher build, push, digest verification, provenance, and handoff order is unsafe');
  }
  rejectEarlySuccessfulTermination(
    publish,
    handoff,
    'publisher must not terminate successfully before writing the exact handoff'
  );

  const signing = shellUnconditionalStatements(publishScript, 'sign_provenance_attestation');
  const sign = requireStatement(signing, /^cosign sign-blob /u, 'publisher must sign provenance');
  const verify = requireStatement(
    signing,
    /^cosign verify-blob /u,
    'publisher must verify the new provenance signature'
  );
  if (sign >= verify) fail('publisher must verify provenance after signing it');

  const finalize = shellUnconditionalStatements(publishScript, 'finalize_evidence');
  const validateSbom = requireStatement(
    finalize,
    /^require_image_sbom_binding /u,
    'finalizer must revalidate the retained image SBOM binding'
  );
  const verifySignature = requireStatement(
    finalize,
    /^cosign verify-blob /u,
    'finalizer must revalidate signed provenance'
  );
  if (validateSbom >= verifySignature) fail('finalizer must validate the SBOM before accepting signed provenance');
}

export function validatePublicationPipeline({jenkinsfile, publishScript, deployScript}) {
  validateDeploymentReleaseShell(deployScript);
  validatePublisherReleaseShell(publishScript);
  const publicationPipeline = `${jenkinsfile}\n${publishScript}\n${deployScript}`;
  for (const requiredPublicationText of [
    'https://in-toto.io/Statement/v1',
    'https://slsa.dev/provenance/v1',
    'DISTR_PROVENANCE_SIGNATURE_REF',
    'control-plane-migration-matrix.ps1',
    'postgres:16.14-alpine3.23',
    'postgres:18.4-alpine3.23',
    'work/release-evidence-${DISTR_IMAGE_TAG}',
    'node hack/pr050-validate-control-plane-evidence.mjs migration',
    'node hack/pr050-validate-control-plane-evidence.mjs postdeploy',
    'node hack/pr050-validate-control-plane-evidence.mjs ui',
    'examples/control-plane-e2e/run.mjs --mode clean --json',
    'finalize-evidence',
    'distr.control-plane-release-acceptance/v1',
    'DISTR_MIGRATION_POSTGRESQL_16_REPORT_REF',
    'DISTR_MIGRATION_POSTGRESQL_18_REPORT_REF',
    'DISTR_ACCEPTANCE_BUNDLE_REF',
    'node --test hack/control-plane-adopter-term-scan.test.mjs',
    'node hack/control-plane-adopter-term-scan.mjs --base',
    'docs/api/operator-control-plane-api.md',
    'post {\n    always {\n      archiveArtifacts(',
    'onlyIfSuccessful: false',
  ]) {
    if (!publicationPipeline.includes(requiredPublicationText)) {
      fail(`image publication pipeline missing release evidence contract: ${requiredPublicationText}`);
    }
  }
  if (
    publicationPipeline.includes('"packages": []') ||
    /-OutputPath\s+["']dist\//u.test(publicationPipeline) ||
    publicationPipeline.includes('onlyIfSuccessful: true')
  ) {
    fail('image publication pipeline contains a release evidence bypass');
  }
  const archiveIndex = publicationPipeline.indexOf('archiveArtifacts(');
  const cleanupIndex = publicationPipeline.lastIndexOf('deleteDir()');
  if (archiveIndex < 0 || cleanupIndex < 0 || archiveIndex > cleanupIndex) {
    fail('image publication pipeline must archive failure evidence before workspace cleanup');
  }
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

const goVersionSources = new Map([
  ['go.mod', /^go\s+(\d+\.\d+\.\d+)$/m],
  ['mise.toml', /^go\s*=\s*"(\d+\.\d+\.\d+)"$/m],
  ['Dockerfile.docker-agent', /^FROM\s+golang:(\d+\.\d+\.\d+)-/m],
  ['Dockerfile.kubernetes-agent', /^FROM\s+golang:(\d+\.\d+\.\d+)-/m],
]);
const goVersions = new Map(
  [...goVersionSources].map(([file, pattern]) => {
    const match = readRel(file).match(pattern);
    if (!match) {
      fail(`could not read pinned Go version from ${file}`);
    }
    return [file, match[1]];
  })
);
if (new Set(goVersions.values()).size !== 1) {
  fail(`Go version pins must match: ${[...goVersions].map(([file, version]) => `${file}=${version}`).join(', ')}`);
}
const requiredGoVersion = '1.26.5';
if ([...goVersions.values()].some((version) => version !== requiredGoVersion)) {
  fail(`Go version pins must remain at the patched baseline ${requiredGoVersion}`);
}

const miseText = readRel('mise.toml');
if (!/^syft\s*=\s*"1\.51\.1"$/mu.test(miseText)) {
  fail('Syft must remain pinned at the reviewed 1.51.1 release');
}
validateMiseReleaseTasks(miseText);

const requiredContainerdVersion = 'v2.2.5';
if (!readRel('go.mod').includes(`github.com/containerd/containerd/v2 ${requiredContainerdVersion}`)) {
  fail(`containerd/v2 must remain at the patched baseline ${requiredContainerdVersion}`);
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
validateVulnerabilityWorkflow(workflow);
validateReleaseEvidenceWorkflow(workflow);

for (const requiredWorkflowText of [
  'DISTR_TARGET_ID',
  'agent_capabilities,agent_task_leases,step_events',
  'go vet ./...',
  'go install golang.org/x/vuln/cmd/govulncheck@v1.6.0',
  'node hack/pr050-govulncheck.mjs',
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

validatePublicationPipeline({
  jenkinsfile: readRel('deploy/jen' + 'kins/Jen' + 'kinsfile.hub-image'),
  publishScript: readRel('deploy/jen' + 'kins/publish-hub-image.sh'),
  deployScript: readRel('deploy/server-docker-compose/deploy.sh'),
});

const vulnerabilityWrapper = readRel('hack/pr050-govulncheck.mjs');
for (const requiredWrapperText of [
  "runner('govulncheck', ['-format=json', './...'])",
  "'https://vuln.go.dev'",
  "'v1.6.0'",
  "'github.com/docker/docker'",
  "'v28.5.2+incompatible'",
  'accepted reviewed risk',
  "['./cmd/hub', './cmd/agent/docker', './cmd/agent/kubernetes']",
  expectedVulnerabilityPolicySha256,
]) {
  if (!vulnerabilityWrapper.includes(requiredWrapperText)) {
    fail(`Go vulnerability wrapper missing fail-closed contract: ${requiredWrapperText}`);
  }
}
if (/process\.env|zero vulnerabilities|--(?:ignore|exclude)/iu.test(vulnerabilityWrapper)) {
  fail('Go vulnerability wrapper must not provide runtime bypasses, exclusions, or a zero-vulnerability claim');
}

const vulnerabilityPolicy = JSON.parse(readRel('docs/security/govulncheck-reviewed-findings.json'));
validateVulnerabilityPolicy(vulnerabilityPolicy);

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

for (const requiredSmokeCleanupCapture of [
  'deploymentTargetId = tutorialEvent.value.deploymentTargetId;',
  'applicationId = helloApp.id;',
]) {
  if (!smokeTest.includes(requiredSmokeCleanupCapture)) {
    fail(`smoke test cleanup must use an ID captured during the same smoke run: ${requiredSmokeCleanupCapture}`);
  }
}

const smokeCleanupStart = smokeTest.lastIndexOf('} finally {');
if (smokeCleanupStart === -1) {
  fail('smoke test missing deterministic finally cleanup');
}
const smokeCleanup = smokeTest.slice(smokeCleanupStart);
const targetCleanup = "await request('DELETE', `/api/v1/deployment-targets/${deploymentTargetId}`, {token});";
const applicationCleanup = "await request('DELETE', `/api/v1/applications/${applicationId}`, {token});";
const organizationCleanup = 'await cleanupDemoOrganization(token);';
for (const requiredCleanup of [targetCleanup, applicationCleanup, organizationCleanup]) {
  if (!smokeCleanup.includes(requiredCleanup)) {
    fail(`smoke test cleanup missing exact-ID teardown step: ${requiredCleanup}`);
  }
}
if (
  !(
    smokeCleanup.indexOf(targetCleanup) < smokeCleanup.indexOf(applicationCleanup) &&
    smokeCleanup.indexOf(applicationCleanup) < smokeCleanup.indexOf(organizationCleanup)
  )
) {
  fail('smoke test teardown must run sequentially: deployment target, application, then organization');
}
for (const broadCleanup of [
  "request('DELETE', '/api/v1/deployment-targets'",
  "request('DELETE', '/api/v1/applications'",
]) {
  if (smokeCleanup.includes(broadCleanup)) {
    fail(`smoke test teardown must not use a collection-wide delete: ${broadCleanup}`);
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
    normalized === '' ||
    /^<placeholder>$/i.test(normalized) ||
    /^placeholder$/i.test(normalized) ||
    /^local$/i.test(normalized) ||
    normalized === localJwtSecret ||
    /^process\.env\.[A-Za-z0-9_]+$/.test(normalized) ||
    /^\$(?:[A-Za-z_][A-Za-z0-9_]*|\{[A-Za-z_][A-Za-z0-9_]*\})$/.test(normalized) ||
    /^secret-ref:[A-Za-z0-9._/-]+$/.test(normalized) ||
    /^secret:\/\/[A-Za-z0-9._/-]+$/.test(normalized) ||
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
  'secret: secret://provider/reference',
  'secret=[REDACTED]',
  'POSTGRES_PASSWORD: local',
  'POSTGRES_PASSWORD="$database_credential"',
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
