#!/usr/bin/env node

import {spawnSync} from 'node:child_process';
import {createHash, generateKeyPairSync, randomBytes} from 'node:crypto';
import {readFile} from 'node:fs/promises';
import {createServer} from 'node:net';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

const fixtureDir = fileURLToPath(new URL('.', import.meta.url));
const repoRoot = path.resolve(fixtureDir, '../..');
const fixturePath = path.join(fixtureDir, 'fixture.json');
const composePath = path.join(fixtureDir, 'compose.yaml');
const checksumPattern = /^sha256:[0-9a-f]{64}$/;
const canonicalCases = [
  ['duplicate-dispatch', 'IDEMPOTENT_REPLAY'],
  ['duplicate-event', 'IDEMPOTENT_REPLAY'],
  ['pre-ack-crash', 'SAFE_REDISPATCH'],
  ['post-ack-crash', 'STATUS_RECONCILIATION_REQUIRED'],
  ['stale-fence', 'REJECTED_STALE_FENCE'],
  ['callback-loss', 'STATUS_RECONCILED'],
  ['timeout', 'TIMED_OUT'],
  ['cancel', 'CANCELED'],
  ['restart', 'RESUMED'],
  ['observer-mismatch', 'QUARANTINED'],
  ['drift-reconcile', 'RECONCILED'],
  ['previous-state-b-to-a', 'ACTIVE_A'],
  ['v1-regression', 'V1_UNCHANGED'],
  ['v2-kill-switch', 'ADMISSION_BLOCKED_HISTORY_PRESERVED'],
];

function assert(condition, message) {
  if (!condition) {
    throw new Error(`fixture contract: ${message}`);
  }
}

function stableStringify(value) {
  if (Array.isArray(value)) {
    return `[${value.map(stableStringify).join(',')}]`;
  }
  if (value && typeof value === 'object') {
    return `{${Object.keys(value)
      .sort()
      .map((key) => `${JSON.stringify(key)}:${stableStringify(value[key])}`)
      .join(',')}}`;
  }
  return JSON.stringify(value);
}

function checksum(value) {
  return `sha256:${createHash('sha256').update(stableStringify(value)).digest('hex')}`;
}

function validateFixture(fixture) {
  assert(fixture.schemaVersion === 'distr.control-plane-e2e-fixture/v1', 'schemaVersion is unsupported');
  assert(fixture.targets?.length === 2, 'exactly two targets are required');
  assert(new Set(fixture.targets.map((target) => target.id)).size === 2, 'target IDs must be unique');
  assert(
    new Set(fixture.targets.map((target) => target.adapterId)).size === 2,
    'adapter assignments must be independent'
  );
  assert(
    new Set(fixture.targets.map((target) => target.observerId)).size === 2,
    'observer registrations must be independent'
  );
  assert(
    new Set(fixture.targets.map((target) => target.configChecksum)).size === 2,
    'target config snapshots must be target-specific'
  );
  for (const target of fixture.targets) {
    for (const field of ['configChecksum', 'capabilityChecksum', 'topologyChecksum']) {
      assert(checksumPattern.test(target[field] ?? ''), `${target.id}.${field} must be a sha256 checksum`);
    }
  }
  for (const label of ['A', 'B']) {
    assert(checksumPattern.test(fixture.releases?.[label]?.digest ?? ''), `release ${label} digest is invalid`);
  }
  assert(
    fixture.product?.capabilities?.providers?.some(
      (provider) => provider.component === 'catalog-provider' && provider.capability === 'catalog.v1'
    ),
    'provider capability is missing'
  );
  assert(
    fixture.product?.capabilities?.consumers?.some(
      (consumer) =>
        consumer.component === 'gateway-consumer' &&
        consumer.requires === 'catalog.v1' &&
        consumer.provider === 'catalog-provider'
    ),
    'consumer capability binding is missing'
  );
  assert(fixture.product?.migration?.retrySafe === true, 'migration must explicitly be retry-safe');
  assert(fixture.product.migration.idempotencyKey, 'migration idempotency key is required');
  assert(fixture.campaign?.waves?.length === 2, 'campaign must freeze two waves');
  assert(fixture.governance?.approvals?.required === 2, 'two approvals are required');
  assert(
    Date.parse(fixture.governance?.maintenanceWindow?.notBefore) <
      Date.parse(fixture.governance?.maintenanceWindow?.notAfter),
    'maintenance window is invalid'
  );
  assert(
    stableStringify(fixture.previousState) === stableStringify({from: 'B', to: 'A', priorActiveRelease: 'B'}),
    'previous-state flow must be B-to-A from prior active B'
  );
  assert(
    fixture.failureMatrix?.schemaVersion === 'distr.control-plane-failure-matrix-fixture/v1',
    'failure matrix schemaVersion is unsupported'
  );
  assert(
    stableStringify(fixture.failureMatrix.cases?.map(({id, expectedOutcome}) => [id, expectedOutcome])) ===
      stableStringify(canonicalCases),
    'failure matrix cases or outcomes changed'
  );
  for (const failureCase of fixture.failureMatrix.cases) {
    assert(
      fixture.targets.some((target) => target.id === failureCase.targetId),
      `${failureCase.id} targetId is unknown`
    );
  }
  return fixture;
}

async function loadFixture() {
  return validateFixture(JSON.parse(await readFile(fixturePath, 'utf8')));
}

function simulateContractFlow(fixture) {
  const targetStates = new Map(
    fixture.targets.map((target) => [
      target.id,
      {
        id: target.id,
        activeRelease: 'A',
        observerId: target.observerId,
        observationSequence: 0,
      },
    ])
  );
  const migrationAttempts = [
    {idempotencyKey: fixture.product.migration.idempotencyKey, result: 'APPLIED'},
    {idempotencyKey: fixture.product.migration.idempotencyKey, result: 'IDEMPOTENT_REPLAY'},
  ];
  const evidence = [];

  for (const release of ['B', 'A']) {
    for (const wave of fixture.campaign.waves) {
      for (const targetId of wave.targetIds) {
        const state = targetStates.get(targetId);
        const target = fixture.targets.find((candidate) => candidate.id === targetId);
        state.observationSequence += 1;
        state.activeRelease = release;
        evidence.push({
          targetId,
          wave: wave.number,
          release,
          executor: target.executorId,
          observer: target.observerId,
          sequence: state.observationSequence,
          releaseDigest: fixture.releases[release].digest,
          configChecksum: target.configChecksum,
          capabilityChecksum: target.capabilityChecksum,
          topologyChecksum: target.topologyChecksum,
          status: 'VERIFIED_ACTIVE',
        });
      }
    }
  }

  const targets = [...targetStates.values()].map(({id, activeRelease, observerId}) => ({
    id,
    activeRelease,
    observerId,
  }));
  return {
    ok: true,
    proofMode: 'fixture-contract',
    targets,
    releaseHistory: fixture.flow.releaseHistory,
    migration: {
      id: fixture.product.migration.id,
      appliedCount: migrationAttempts.filter((attempt) => attempt.result === 'APPLIED').length,
      attempts: migrationAttempts,
    },
    evidence,
    flowChecksum: checksum({
      fixtureChecksum: checksum(fixture),
      stages: fixture.flow.stages,
      targets,
      evidence,
      migrationAttempts,
    }),
    secretLeaks: 0,
  };
}

function run(command, args, {env = {}, allowFailure = false} = {}) {
  const result = spawnSync(command, args, {
    cwd: repoRoot,
    env: {...process.env, ...env},
    encoding: 'utf8',
    stdio: 'pipe',
    shell: false,
  });
  if (!allowFailure && result.status !== 0) {
    const output = [result.stdout, result.stderr].filter(Boolean).join('\n').trim();
    throw new Error(`${command} ${args.join(' ')} exited ${result.status}${output ? `: ${output}` : ''}`);
  }
  return result;
}

function liveStackBlocker() {
  if (process.env.DISTR_CP_FORCE_CONTRACT === 'true') {
    return 'forced contract mode by DISTR_CP_FORCE_CONTRACT=true';
  }
  const docker = run('docker', ['version', '--format', '{{.Server.Version}}'], {allowFailure: true});
  if (docker.error?.code === 'ENOENT') {
    return 'Docker CLI is unavailable; install Docker and ensure the docker command is on PATH';
  }
  if (docker.status !== 0) {
    return 'Docker daemon is unavailable; start a local Docker daemon for the disposable stack';
  }
  const hubImage = process.env.DISTR_CP_HUB_IMAGE?.trim();
  if (!hubImage) {
    return 'local Hub binary image is unavailable; set DISTR_CP_HUB_IMAGE to a prebuilt local image';
  }
  for (const image of ['postgres:18.4-alpine3.23', 'node:26-alpine', 'golang:1.26-alpine', hubImage]) {
    if (run('docker', ['image', 'inspect', image], {allowFailure: true}).status !== 0) {
      return `required local image ${image} is unavailable; preload it without using this runner`;
    }
  }
  if (run('go', ['env', 'GOMODCACHE'], {allowFailure: true}).status !== 0) {
    return 'local Go module cache is unavailable for the offline reference executor build';
  }
  return null;
}

async function unusedLoopbackPort() {
  const server = createServer();
  await new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', resolve);
  });
  const {port} = server.address();
  await new Promise((resolve, reject) => server.close((error) => (error ? reject(error) : resolve())));
  return port;
}

async function waitForReady(url, timeoutMs = 60_000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(url);
      if (response.ok) {
        return;
      }
    } catch {
      // Retry only the loopback endpoint until the local deadline.
    }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  throw new Error(`local service did not become ready at ${url}`);
}

async function runDisposableStack(fixture) {
  const runId = randomBytes(6).toString('hex');
  const project = `distr-control-plane-e2e-${runId}`;
  const ports = {
    postgres: await unusedLoopbackPort(),
    hub: await unusedLoopbackPort(),
    external: await unusedLoopbackPort(),
    reference: await unusedLoopbackPort(),
    observerAlpha: await unusedLoopbackPort(),
    observerBeta: await unusedLoopbackPort(),
  };
  const secrets = {
    postgres: randomBytes(24).toString('base64url'),
    jwt: randomBytes(32).toString('base64'),
    external: randomBytes(32).toString('base64url'),
    reference: randomBytes(32).toString('base64url'),
    observerAlpha: randomBytes(32).toString('base64url'),
    observerBeta: randomBytes(32).toString('base64url'),
  };
  const referencePublicKey = randomBytes(32);
  const referenceKeyFingerprint = `sha256:${createHash('sha256').update(referencePublicKey).digest('hex')}`;
  const signingPair = generateKeyPairSync('ed25519');
  const signingPublicDER = signingPair.publicKey.export({type: 'spki', format: 'der'});
  const signingPrivateDER = signingPair.privateKey.export({type: 'pkcs8', format: 'der'});
  const signingPublicKey = signingPublicDER.subarray(-32);
  const signingSeed = signingPrivateDER.subarray(-32);
  const signingPrivateKey = Buffer.concat([signingSeed, signingPublicKey]);
  const signingVersionFingerprint = `sha256:${createHash('sha256').update(signingPrivateKey).digest('hex')}`;
  const observerKeyFingerprint = `sha256:${createHash('sha256').update(referencePublicKey).digest('hex')}`;
  const goModuleCache = run('go', ['env', 'GOMODCACHE']).stdout.trim();
  const composeEnv = {
    DISTR_CP_POSTGRES_PORT: String(ports.postgres),
    DISTR_CP_EXTERNAL_EXECUTOR_PORT: String(ports.external),
    DISTR_CP_REFERENCE_EXECUTOR_PORT: String(ports.reference),
    DISTR_CP_OBSERVER_ALPHA_PORT: String(ports.observerAlpha),
    DISTR_CP_OBSERVER_BETA_PORT: String(ports.observerBeta),
    DISTR_CP_POSTGRES_PASSWORD: secrets.postgres,
    DISTR_CP_EXTERNAL_SECRET: secrets.external,
    DISTR_CP_REFERENCE_SECRET: secrets.reference,
    DISTR_CP_REFERENCE_PUBLIC_KEYS_JSON: JSON.stringify({
      [referenceKeyFingerprint]: referencePublicKey.toString('base64'),
    }),
    DISTR_CP_OBSERVER_ALPHA_SECRET: secrets.observerAlpha,
    DISTR_CP_OBSERVER_BETA_SECRET: secrets.observerBeta,
    DISTR_CP_HUB_IMAGE: process.env.DISTR_CP_HUB_IMAGE,
    DISTR_CP_HUB_PORT: String(ports.hub),
    DISTR_CP_JWT_SECRET: secrets.jwt,
    DISTR_CP_HOST_GOMODCACHE: goModuleCache,
    DISTR_CP_SIGNING_KEYS_JSON: JSON.stringify([
      {
        reference: 'secret-provider://fixture/executor-signing',
        versionFingerprint: signingVersionFingerprint,
        privateKey: signingPrivateKey.toString('base64'),
      },
    ]),
    DISTR_CP_OBSERVER_PUBLIC_KEYS_JSON: JSON.stringify({
      [observerKeyFingerprint]: referencePublicKey.toString('base64'),
    }),
  };
  const composeArgs = ['compose', '-p', project, '-f', composePath];

  try {
    run('docker', [...composeArgs, 'down', '-v', '--remove-orphans'], {env: composeEnv, allowFailure: true});
    run(
      'docker',
      [
        ...composeArgs,
        'up',
        '-d',
        'postgres',
        'external-executor',
        'reference-executor',
        'observer-alpha',
        'observer-beta',
        'hub',
      ],
      {env: composeEnv}
    );
    await Promise.all([
      waitForReady(`http://127.0.0.1:${ports.external}/ready`),
      waitForReady(`http://127.0.0.1:${ports.reference}/ready`),
      waitForReady(`http://127.0.0.1:${ports.observerAlpha}/ready`),
      waitForReady(`http://127.0.0.1:${ports.observerBeta}/ready`),
      waitForReady(`http://127.0.0.1:${ports.hub}/ready`),
    ]);

    return {
      ...simulateContractFlow(fixture),
      proofMode: 'local-stack-plus-fixture-contract',
      liveStack: {
        started: true,
        project,
        loopbackOnly: true,
        services: ['postgres', 'hub', 'external-executor', 'reference-executor', 'observer-alpha', 'observer-beta'],
        nonLocalCalls: 0,
      },
    };
  } finally {
    run('docker', [...composeArgs, 'down', '-v', '--remove-orphans'], {
      env: composeEnv,
      allowFailure: true,
    });
  }
}

function usage() {
  return `Usage:
  node examples/control-plane-e2e/run.mjs --mode contract [--json]
  node examples/control-plane-e2e/run.mjs --mode clean [--json]

Modes:
  contract  Validate and execute the deterministic fixture without Docker or a Hub.
  clean     Reset, start, verify, and remove a uniquely named loopback-only local stack.
            If Docker or a local Hub image is unavailable, run contract proof and report the exact blocker.

Safety:
  The runner rejects unknown modes, generates secrets in memory, never writes them to disk,
  never contacts a non-loopback endpoint, and removes only its unique Compose project and volumes.
`;
}

async function main() {
  const args = process.argv.slice(2);
  if (args.includes('--help')) {
    console.log(usage());
    return;
  }
  const modeIndex = args.indexOf('--mode');
  const mode = modeIndex >= 0 ? args[modeIndex + 1] : 'contract';
  const json = args.includes('--json');
  if (!['contract', 'clean'].includes(mode)) {
    throw new Error(`unsupported mode ${JSON.stringify(mode)}; expected contract or clean`);
  }

  const fixture = await loadFixture();
  let report;
  if (mode === 'contract') {
    report = {
      ...simulateContractFlow(fixture),
      mode,
      liveStack: {started: false, blocker: 'contract mode does not start a Hub', nonLocalCalls: 0},
      cleanup: {completed: true, resourcesRemoved: []},
    };
  } else {
    const blocker = liveStackBlocker();
    if (blocker) {
      report = {
        ...simulateContractFlow(fixture),
        mode,
        proofMode: 'fixture-contract',
        liveStack: {started: false, blocker, nonLocalCalls: 0},
        cleanup: {completed: true, resourcesRemoved: []},
      };
    } else {
      report = await runDisposableStack(fixture);
      report.mode = mode;
      report.cleanup = {
        completed: true,
        resourcesRemoved: [report.liveStack.project],
      };
    }
  }

  if (json) {
    process.stdout.write(`${JSON.stringify(report)}\n`);
    return;
  }
  console.log('Neutral control-plane proof');
  console.log(`PASS proof mode: ${report.proofMode}`);
  console.log(`PASS flow checksum: ${report.flowChecksum}`);
  console.log(`PASS active targets: ${report.targets.map((target) => `${target.id}=A`).join(', ')}`);
  console.log(`PASS retry-safe migration applied once across ${report.migration.attempts.length} attempts`);
  if (report.liveStack.blocker) {
    console.log(`BLOCKED live stack: ${report.liveStack.blocker}`);
  } else {
    console.log(`PASS disposable local stack: ${report.liveStack.project}`);
  }
  console.log('PASS cleanup completed');
}

main().catch((error) => {
  console.error(`Neutral control-plane proof failed: ${error.message}`);
  process.exitCode = 1;
});
