import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {test} from 'node:test';

import {evaluateScan, runGate, validateDependencyOutputs} from './pr050-govulncheck.mjs';
import * as releaseValidator from './pr050-validate-release-hardening.mjs';

const acceptedIds = ['GO-2026-4883', 'GO-2026-4887', 'GO-2026-5617', 'GO-2026-5668', 'GO-2026-5746'];
const dockerModule = 'github.com/docker/docker';
const dockerVersion = 'v28.5.2+incompatible';
const committedPolicy = JSON.parse(
  readFileSync(new URL('../docs/security/govulncheck-reviewed-findings.json', import.meta.url), 'utf8')
);
const submittedFeedback = {
  'GO-2026-4883': 'https://github.com/golang/vulndb/issues/4922#issuecomment-4976353536',
  'GO-2026-4887': 'https://github.com/golang/vulndb/issues/4921#issuecomment-4976353689',
  'GO-2026-5617': 'https://github.com/golang/vulndb/issues/5993',
  'GO-2026-5668': 'https://github.com/golang/vulndb/issues/5994',
  'GO-2026-5746': 'https://github.com/golang/vulndb/issues/5995',
};

const hubTrace = [
  frame(
    dockerModule,
    dockerVersion,
    'github.com/docker/docker/api/types/versions',
    'init',
    'api/types/versions/compare.go'
  ),
  frame(
    'github.com/containers/image/v5',
    'v5.36.2',
    'github.com/containers/image/v5/manifest',
    'init',
    'manifest/docker_schema1.go'
  ),
  frame(
    'github.com/distr-sh/distr',
    undefined,
    'github.com/distr-sh/distr/internal/registry/upstream',
    'init',
    'internal/registry/upstream/upstream.go'
  ),
];

const dockerAgentTrace = [
  frame(
    dockerModule,
    dockerVersion,
    'github.com/docker/docker/pkg/namesgenerator',
    'init',
    'pkg/namesgenerator/names-generator.go'
  ),
  frame('github.com/docker/buildx', 'v0.33.0', 'github.com/docker/buildx/store', 'init', 'store/util.go'),
  frame(
    'github.com/docker/buildx',
    'v0.33.0',
    'github.com/docker/buildx/store/storeutil',
    'init',
    'store/storeutil/storeutil.go'
  ),
  frame(
    'github.com/docker/compose/v5',
    'v5.1.4',
    'github.com/docker/compose/v5/pkg/compose',
    'init',
    'pkg/compose/compose.go'
  ),
  frame(
    'github.com/distr-sh/distr',
    undefined,
    'github.com/distr-sh/distr/cmd/agent/docker',
    'init',
    'cmd/agent/docker/compose_service.go'
  ),
];

function frame(module, version, packageName, functionName, filename) {
  return {
    module,
    ...(version === undefined ? {} : {version}),
    package: packageName,
    function: functionName,
    position: {filename, offset: 123, line: 45, column: 6},
  };
}

function clone(value) {
  return structuredClone(value);
}

function makePolicy() {
  return clone(committedPolicy);
}

function makeMessages() {
  const policy = makePolicy();
  return [
    {
      config: {
        protocol_version: 'v1.0.0',
        scanner_name: 'govulncheck',
        scanner_version: 'v1.6.0',
        db: 'https://vuln.go.dev',
        db_last_modified: '2026-07-08T17:05:00Z',
        go_version: 'go1.26.5',
        scan_level: 'symbol',
        scan_mode: 'source',
      },
    },
    {
      SBOM: {
        go_version: 'go1.26.5',
        modules: [{path: 'github.com/distr-sh/distr'}, {path: dockerModule, version: dockerVersion}],
      },
    },
    ...policy.findings.map((finding) => ({
      osv: {
        id: finding.id,
        affected: clone(finding.affected),
        database_specific: {url: finding.goAdvisory, review_status: 'UNREVIEWED'},
      },
    })),
    {
      finding: {
        osv: 'GO-2026-5932',
        trace: [{module: 'golang.org/x/crypto', version: 'v0.53.0'}],
      },
    },
    ...policy.findings.flatMap((finding) =>
      finding.traces.map((trace) => ({finding: {osv: finding.id, trace: clone(trace)}}))
    ),
  ];
}

function encode(messages) {
  return messages.map((message) => JSON.stringify(message, null, 2)).join('\n');
}

function scannerResult(messages = makeMessages()) {
  return {status: 0, signal: null, error: undefined, stderr: '', stdout: encode(messages)};
}

function evaluate(messages = makeMessages(), policy = makePolicy()) {
  return evaluateScan(scannerResult(messages), policy, {now: new Date('2026-07-17T12:00:00Z')});
}

function reachable(messages) {
  return messages.filter((message) => message.finding?.trace?.some((entry) => entry.function));
}

test('the committed policy contains the exact submitted Go VulnDB feedback links', () => {
  assert.deepEqual(
    Object.fromEntries(committedPolicy.findings.map((finding) => [finding.id, finding.feedback])),
    submittedFeedback
  );
});

test('co-mutating policy and scanner traces still fails the immutable policy seal', () => {
  const policy = makePolicy();
  const messages = makeMessages();
  const changedPackage = 'github.com/containers/image/v5/changed';
  policy.findings[0].traces[0][1].package = changedPackage;
  const matchingFinding = reachable(messages).find(
    (message) => message.finding.osv === policy.findings[0].id && message.finding.trace.length === 3
  );
  matchingFinding.finding.trace[1].package = changedPackage;

  assert.throws(() => evaluate(messages, policy), /policy integrity SHA-256 mismatch/);
});

test('generic feedback forms fail runtime policy validation', () => {
  const policy = makePolicy();
  policy.findings[0].feedback =
    'https://github.com/golang/vulndb/issues/new?report=GO-2026-4883&template=suggest_edit.yaml';
  assert.throws(() => evaluate(makeMessages(), policy), /submitted Go VulnDB feedback is invalid/);
});

test('exact reviewed stream passes regardless of JSON object order and normalizes only locations', () => {
  const messages = makeMessages().reverse();
  for (const message of reachable(messages)) {
    for (const traceFrame of message.finding.trace) {
      traceFrame.position.offset += 1000;
      traceFrame.position.line += 100;
      traceFrame.position.column += 10;
    }
  }

  const result = evaluate(messages);
  assert.deepEqual(result.acceptedIds, acceptedIds);
  assert.deepEqual(result.informationalIds, ['GO-2026-5932']);
  assert.match(result.summary, /accepted reviewed risk/);
  assert.doesNotMatch(result.summary, /zero vulnerabilities/i);
});

test('an extra or missing reachable ID fails closed', () => {
  const extra = makeMessages();
  extra.push({finding: {osv: 'GO-2099-9999', trace: clone(hubTrace)}});
  assert.throws(() => evaluate(extra), /unexpected reachable finding GO-2099-9999/);

  const missing = makeMessages().filter((message) => message.finding?.osv !== acceptedIds[0]);
  assert.throws(() => evaluate(missing), /expected 2 reachable traces for GO-2026-4883, got 0/);
});

test('a duplicate reachable finding fails closed', () => {
  const messages = makeMessages();
  messages.push(clone(reachable(messages)[0]));
  assert.throws(() => evaluate(messages), /expected 2 reachable traces .* got 3/);
});

test('module version package function file intermediate and root frame mutations fail closed', async (t) => {
  const mutations = [
    ['module', (trace) => (trace[0].module = 'example.invalid/module')],
    ['version', (trace) => (trace[0].version = 'v28.5.3+incompatible')],
    ['package', (trace) => (trace[0].package = 'github.com/docker/docker/other')],
    ['function', (trace) => (trace[0].function = 'Run')],
    ['file', (trace) => (trace[0].position.filename = 'pkg/other.go')],
    ['intermediate frame', (trace) => (trace[1].package = 'github.com/docker/buildx/other')],
    ['root frame', (trace) => (trace.at(-1).package = 'github.com/distr-sh/distr/cmd/other')],
  ];

  for (const [name, mutate] of mutations) {
    await t.test(name, () => {
      const messages = makeMessages();
      mutate(reachable(messages)[1].finding.trace);
      assert.throws(() => evaluate(messages), /reachable trace mismatch/);
    });
  }
});

test('missing or extra trace and a newly reachable non-init function fail closed', () => {
  const missing = makeMessages();
  missing.splice(missing.indexOf(reachable(missing)[0]), 1);
  assert.throws(() => evaluate(missing), /expected 2 reachable traces .* got 1/);

  const extra = makeMessages();
  const third = clone(reachable(extra)[0]);
  third.finding.trace.at(-1).position.filename = 'internal/registry/upstream/other.go';
  extra.push(third);
  assert.throws(() => evaluate(extra), /expected 2 reachable traces .* got 3/);

  const nonInit = makeMessages();
  reachable(nonInit)[0].finding.trace[0].function = 'ValidatePrivileges';
  assert.throws(() => evaluate(nonInit), /only reviewed init functions may be accepted/);
});

test('OSV fixed-version metadata mutation fails closed', () => {
  const messages = makeMessages();
  messages.find((message) => message.osv?.id === acceptedIds[0]).osv.affected[2].ranges[0].events[1].fixed =
    '2.0.0-beta.99';
  assert.throws(() => evaluate(messages), /OSV affected metadata mismatch/);
});

test('expired and malformed policies fail closed', () => {
  const expired = makePolicy();
  expired.expiresAt = '2026-07-17T11:59:59Z';
  assert.throws(() => evaluate(makeMessages(), expired), /policy expired/);

  const malformed = makePolicy();
  delete malformed.reviewer;
  assert.throws(() => evaluate(makeMessages(), malformed), /policy reviewer/);

  const duplicate = makePolicy();
  duplicate.findings.push(clone(duplicate.findings[0]));
  assert.throws(() => evaluate(makeMessages(), duplicate), /duplicate policy finding/);
});

test('scanner protocol config or Docker module downgrade fails closed', () => {
  for (const mutate of [
    (messages) => (messages.find((message) => message.config).config.protocol_version = 'v0.1.0'),
    (messages) => (messages.find((message) => message.config).config.scanner_version = 'v1.5.0'),
    (messages) => (messages.find((message) => message.config).config.db = 'https://example.invalid'),
    (messages) =>
      (messages.find((message) => message.SBOM).SBOM.modules.find((module) => module.path === dockerModule).version =
        'v28.5.1+incompatible'),
  ]) {
    const messages = makeMessages();
    mutate(messages);
    assert.throws(() => evaluate(messages), /scanner config mismatch|Docker module mismatch/);
  }
});

test('malformed or truncated JSON and scanner process failure fail closed', () => {
  assert.throws(
    () =>
      evaluateScan({...scannerResult(), stdout: '{"config": '}, makePolicy(), {
        now: new Date('2026-07-17T12:00:00Z'),
      }),
    /malformed or truncated govulncheck JSON/
  );
  assert.throws(
    () =>
      evaluateScan({...scannerResult(), status: 2, stderr: 'scanner failed'}, makePolicy(), {
        now: new Date('2026-07-17T12:00:00Z'),
      }),
    /govulncheck process failed/
  );
  assert.throws(
    () =>
      evaluateScan({...scannerResult(), status: null, error: new Error('ENOENT')}, makePolicy(), {
        now: new Date('2026-07-17T12:00:00Z'),
      }),
    /govulncheck process failed/
  );
});

test('affected Moby daemon packages in shipped binary dependencies fail closed', () => {
  const policy = makePolicy();
  assert.throws(
    () =>
      validateDependencyOutputs(
        new Map([
          ['./cmd/hub', 'github.com/distr-sh/distr/cmd/hub\n'],
          [
            './cmd/agent/docker',
            'github.com/docker/docker/pkg/namesgenerator\ngithub.com/docker/docker/pkg/authorization\n',
          ],
          ['./cmd/agent/kubernetes', 'github.com/distr-sh/distr/cmd/agent/kubernetes\n'],
        ]),
        policy
      ),
    /affected Moby daemon package .*pkg\/authorization/
  );
});

test('module-only informational findings are reported and never called accepted', () => {
  const result = evaluate();
  assert.deepEqual(result.informationalIds, ['GO-2026-5932']);
  assert.ok(!result.acceptedIds.includes('GO-2026-5932'));
  assert.match(result.summary, /informational not called: GO-2026-5932/);
  assert.doesNotMatch(result.summary, /accepted reviewed risk:.*GO-2026-5932/);
});

test('the real runner uses the pinned scanner protocol and all shipped binary dependency checks', () => {
  const calls = [];
  const dependencyOutputs = new Map([
    ['./cmd/hub', 'github.com/docker/docker/api/types/versions\n'],
    ['./cmd/agent/docker', 'github.com/docker/docker/pkg/namesgenerator\n'],
    ['./cmd/agent/kubernetes', 'github.com/distr-sh/distr/cmd/agent/kubernetes\n'],
  ]);
  const runner = (command, args) => {
    calls.push([command, args]);
    if (command === 'govulncheck') {
      return scannerResult();
    }
    return {
      status: 0,
      signal: null,
      error: undefined,
      stderr: '',
      stdout: dependencyOutputs.get(args.at(-1)),
    };
  };

  const result = runGate({policy: makePolicy(), now: new Date('2026-07-17T12:00:00Z'), runner});
  assert.match(result.summary, /accepted reviewed risk/);
  assert.deepEqual(calls, [
    ['govulncheck', ['-format=json', './...']],
    ['go', ['list', '-deps', '-buildvcs=false', './cmd/hub']],
    ['go', ['list', '-deps', '-buildvcs=false', './cmd/agent/docker']],
    ['go', ['list', '-deps', '-buildvcs=false', './cmd/agent/kubernetes']],
  ]);
});

test('the PR-050 validator structurally rejects vulnerability-step bypasses', () => {
  assert.equal(typeof releaseValidator.validateVulnerabilityWorkflow, 'function');
  const exactWorkflow = [
    'jobs:',
    '  release-gates:',
    '    runs-on: ubuntu-latest',
    '    steps:',
    '      - name: Run Go vulnerability scan',
    '        run: |',
    '          go install golang.org/x/vuln/cmd/govulncheck@v1.6.0',
    '          node hack/pr050-govulncheck.mjs',
  ].join('\n');
  assert.doesNotThrow(() => releaseValidator.validateVulnerabilityWorkflow(exactWorkflow));

  for (const bypass of [
    exactWorkflow.replace('        run: |', '        continue-on-error: true\n        run: |'),
    exactWorkflow.replace('node hack/pr050-govulncheck.mjs', 'node hack/pr050-govulncheck.mjs || :'),
    exactWorkflow.replace('        run: |', '        if: ${{ false }}\n        run: |'),
    exactWorkflow.replace('        run: |', '        if: ${{ always() }}\n        run: |'),
    exactWorkflow.replace('node hack/pr050-govulncheck.mjs', 'if false; then node hack/pr050-govulncheck.mjs; fi'),
    `${exactWorkflow}\n          exit 0`,
  ]) {
    assert.throws(
      () => releaseValidator.validateVulnerabilityWorkflow(bypass),
      /vulnerability step must exactly contain/
    );
  }
});

test('the PR-050 validator rejects enclosing release-job execution bypasses', () => {
  const exactWorkflow = [
    'jobs:',
    '  release-gates:',
    '    runs-on: ubuntu-latest',
    '    steps:',
    '      - name: Run Go vulnerability scan',
    '        run: |',
    '          go install golang.org/x/vuln/cmd/govulncheck@v1.6.0',
    '          node hack/pr050-govulncheck.mjs',
  ].join('\n');

  for (const bypass of [
    exactWorkflow.replace('    runs-on:', '    if: ${{ false }}\n    runs-on:'),
    exactWorkflow.replace('    runs-on:', '    continue-on-error: true\n    runs-on:'),
  ]) {
    assert.throws(
      () => releaseValidator.validateVulnerabilityWorkflow(bypass),
      /release job must not define if or continue-on-error/
    );
  }
});

test('the PR-050 validator seals executable release evidence and rejects empty SBOM acceptance', () => {
  assert.equal(typeof releaseValidator.validateReleaseEvidenceWorkflow, 'function');
  const workflow = readFileSync(
    new URL('../.github/workflows/community-release-hardening.yaml', import.meta.url),
    'utf8'
  );
  assert.doesNotThrow(() => releaseValidator.validateReleaseEvidenceWorkflow(workflow));
  assert.throws(
    () =>
      releaseValidator.validateReleaseEvidenceWorkflow(
        workflow.replace('sbom.packages.length === 0', '"packages": []')
      ),
    /empty-package SPDX fallback/
  );
  assert.throws(
    () =>
      releaseValidator.validateReleaseEvidenceWorkflow(
        workflow.replaceAll(
          'node hack/pr050-validate-control-plane-evidence.mjs postdeploy',
          'node hack/pr050-validate-control-plane-evidence.mjs fixture'
        )
      ),
    /missing fail-closed evidence contract/
  );
  for (const requiredGate of [
    'cosign sign-blob',
    'cosign verify-blob',
    "-OutputPath 'work/release-evidence/migration-postgresql-16.14.json'",
    "-OutputPath 'work/release-evidence/migration-postgresql-18.4.json'",
    'node hack/pr050-validate-control-plane-evidence.mjs migration',
    'node --test hack/control-plane-adopter-term-scan.test.mjs',
    'node hack/control-plane-adopter-term-scan.mjs --base',
    'docs/api/operator-control-plane-api.md',
  ]) {
    assert.throws(
      () => releaseValidator.validateReleaseEvidenceWorkflow(workflow.replaceAll(requiredGate, 'REMOVED_GATE')),
      /missing fail-closed evidence contract/
    );
  }
});

test('the PR-050 validator seals Jenkins signed evidence, matrix, adopter scan, and failure retention', () => {
  assert.equal(typeof releaseValidator.validatePublicationPipeline, 'function');
  const publicationPipeline = {
    jenkinsfile: readFileSync(new URL('../deploy/jenkins/Jenkinsfile.hub-image', import.meta.url), 'utf8'),
    publishScript: readFileSync(new URL('../deploy/jenkins/publish-hub-image.sh', import.meta.url), 'utf8'),
    deployScript: readFileSync(new URL('../deploy/server-docker-compose/deploy.sh', import.meta.url), 'utf8'),
  };
  assert.doesNotThrow(() => releaseValidator.validatePublicationPipeline(publicationPipeline));
  for (const requiredGate of [
    'postgres:16.14-alpine3.23',
    'postgres:18.4-alpine3.23',
    'work/release-evidence-${DISTR_IMAGE_TAG}',
    'node hack/pr050-validate-control-plane-evidence.mjs migration',
    'node --test hack/control-plane-adopter-term-scan.test.mjs',
    'node hack/control-plane-adopter-term-scan.mjs --base',
    'docs/api/operator-control-plane-api.md',
    'post {\n    always {\n      archiveArtifacts(',
  ]) {
    const mutated = Object.fromEntries(
      Object.entries(publicationPipeline).map(([name, contents]) => [
        name,
        contents.replaceAll(requiredGate, 'REMOVED_GATE'),
      ])
    );
    assert.throws(
      () => releaseValidator.validatePublicationPipeline(mutated),
      /image publication pipeline missing release evidence contract/
    );
  }
});

test('the PR-050 validator parses mise task dependencies and seals the non-mutating tidy closure', () => {
  assert.equal(typeof releaseValidator.validateMiseReleaseTasks, 'function');
  const mise = readFileSync(new URL('../mise.toml', import.meta.url), 'utf8');
  assert.doesNotThrow(() => releaseValidator.validateMiseReleaseTasks(mise));

  const transitive = mise
    .replace(
      'depends = ["go-tidy-check", "build:frontend:community"]',
      'depends = ["release-tidy-gate", "build:frontend:community"]'
    )
    .concat('\n[tasks.release-tidy-gate]\ndepends = ["go-tidy-check"]\n');
  assert.doesNotThrow(() => releaseValidator.validateMiseReleaseTasks(transitive));

  const bypasses = [
    mise.replace("run = 'go mod tidy -diff'", "run = 'go mod tidy -diff && go mod tidy'"),
    mise.replace(
      "[tasks.go-tidy-check]\nrun = 'go mod tidy -diff'",
      '[tasks.go-tidy-check]\nrun = \'go mod tidy -diff\'\ndepends = ["go-tidy"]'
    ),
    mise.replace(
      'depends = ["go-tidy-check", "build:frontend:community"]',
      'depends = ["build:frontend:community"] # go-tidy-check'
    ),
    mise
      .replace(
        'depends = ["go-tidy-check", "build:frontend:community"]',
        'depends = ["release-tidy-gate", "build:frontend:community"]'
      )
      .concat('\n[tasks.release-tidy-gate]\ndepends = ["go-tidy-check", "go-tidy"]\n'),
    mise
      .replace(
        'depends = ["go-tidy-check", "build:frontend:community"]',
        'depends = ["renamed-mutating-tidy", "go-tidy-check", "build:frontend:community"]'
      )
      .concat("\n[tasks.renamed-mutating-tidy]\nrun = 'go mod tidy -v'\n"),
    mise
      .replace(
        'run = \'go build -ldflags="{{vars.ldflags}} -X github.com/distr-sh/distr/internal/buildconfig.edition=community" -o dist/distr ./cmd/hub\'',
        "run = 'mise run renamed-mutating-tidy && go build -o dist/distr ./cmd/hub'"
      )
      .concat("\n[tasks.renamed-mutating-tidy]\nrun = 'go mod tidy'\n"),
    `${mise}\n[tasks.go-tidy-check]\nrun = 'go mod tidy -diff'\n`,
  ];
  for (const bypass of bypasses) {
    assert.throws(() => releaseValidator.validateMiseReleaseTasks(bypass), /mise|go-tidy|mutating/i);
  }
});

test('the PR-050 validator requires executable release-shell gates in their owning functions', () => {
  assert.equal(typeof releaseValidator.validateDeploymentReleaseShell, 'function');
  assert.equal(typeof releaseValidator.validatePublisherReleaseShell, 'function');
  const deploy = readFileSync(new URL('../deploy/server-docker-compose/deploy.sh', import.meta.url), 'utf8');
  const publisher = readFileSync(new URL('../deploy/jenkins/publish-hub-image.sh', import.meta.url), 'utf8');
  assert.doesNotThrow(() => releaseValidator.validateDeploymentReleaseShell(deploy));
  assert.doesNotThrow(() => releaseValidator.validatePublisherReleaseShell(publisher));

  for (const bypass of [
    deploy.replace(
      '  dirty="$(git status --porcelain=v1 --untracked-files=all)" || return',
      '  # dirty="$(git status --porcelain=v1 --untracked-files=all)" || return\n  dirty=""'
    ),
    deploy.replace('  require_source_clean after || return', '  # require_source_clean after || return\n  true'),
    deploy.replace(
      '    mise exec -- syft "docker:${image_id}" \\',
      '    # mise exec -- syft "docker:${image_id}" --source-name "$DISTR_IMAGE" --source-version "$DISTR_IMAGE_TAG" --platform "$platform" -o "spdx-json=${raw}" || {\n    syft "${image_ref}" \\'
    ),
    deploy.replace(
      '    current_identity="$(resolve_local_image_identity "$image_ref")" || return',
      '    # current_identity="$(resolve_local_image_identity "$image_ref")" || return\n    current_identity="$identity"'
    ),
    deploy.replace(
      '  require_source_clean before || return',
      '  if false; then\n    require_source_clean before || return\n  fi'
    ),
    deploy.replace(
      '  require_source_clean before || return',
      '  if [[ 1 -eq 0 ]]; then\n    require_source_clean before || return\n  fi'
    ),
    deploy.replace('  require_source_clean before || return', '  return 0\n  require_source_clean before || return'),
    deploy.replace('  require_source_clean before || return', '  return\n  require_source_clean before || return'),
  ]) {
    assert.throws(() => releaseValidator.validateDeploymentReleaseShell(bypass), /release|build|SBOM|Syft|identity/i);
  }

  for (const bypass of [
    publisher.replace(
      '  inspect_image_identity "$digest_ref" "$expected_source" || return',
      '  # inspect_image_identity "$digest_ref" "$expected_source" || return\n  true'
    ),
    publisher.replace(
      '  cosign verify-blob \\',
      '  # cosign verify-blob --key "$PROVENANCE_SIGNING_PUBLIC_KEY" --signature "$temporary" "$provenance"\n  true \\'
    ),
    publisher.replace(
      '  write_exact_handoff \\\n    "$release_file" "$digest" "$sbom" "$binding" "$provenance" "$signature" "$expected_source" || return',
      '  # write_exact_handoff "$release_file" "$digest" "$sbom" "$binding" "$provenance" "$signature" "$expected_source" || return\n  true'
    ),
    publisher.replace(
      '  write_exact_handoff \\\n    "$release_file" "$digest" "$sbom" "$binding" "$provenance" "$signature" "$expected_source" || return',
      '  if false; then\n    write_exact_handoff "$release_file" "$digest" "$sbom" "$binding" "$provenance" "$signature" "$expected_source" || return\n  fi'
    ),
    publisher.replace(
      '  write_exact_handoff \\\n    "$release_file" "$digest" "$sbom" "$binding" "$provenance" "$signature" "$expected_source" || return',
      '  if true; then\n    :\n  else\n    write_exact_handoff "$release_file" "$digest" "$sbom" "$binding" "$provenance" "$signature" "$expected_source" || return\n  fi'
    ),
    publisher.replace(
      '  write_exact_handoff \\\n    "$release_file" "$digest" "$sbom" "$binding" "$provenance" "$signature" "$expected_source" || return',
      '  exit 0\n  write_exact_handoff "$release_file" "$digest" "$sbom" "$binding" "$provenance" "$signature" "$expected_source" || return'
    ),
  ]) {
    assert.throws(
      () => releaseValidator.validatePublisherReleaseShell(bypass),
      /release|publisher|provenance|handoff/i
    );
  }
});

test('the PR-050 validator seals the exact committed policy and submitted links', () => {
  assert.equal(typeof releaseValidator.validateVulnerabilityPolicy, 'function');
  assert.doesNotThrow(() => releaseValidator.validateVulnerabilityPolicy(makePolicy()));

  const changedTrace = makePolicy();
  changedTrace.findings[0].traces[0][1].position.filename = 'manifest/changed.go';
  assert.throws(() => releaseValidator.validateVulnerabilityPolicy(changedTrace), /policy integrity SHA-256 mismatch/);

  const genericFeedback = makePolicy();
  genericFeedback.findings[0].feedback =
    'https://github.com/golang/vulndb/issues/new?report=GO-2026-4883&template=suggest_edit.yaml';
  assert.throws(() => releaseValidator.validateVulnerabilityPolicy(genericFeedback), /submitted feedback URL changed/);
});
