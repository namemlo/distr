import assert from 'node:assert/strict';
import {generateKeyPairSync} from 'node:crypto';
import {mkdir, mkdtemp, readFile, rm, writeFile} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import test from 'node:test';
import {canonicalJSONString, sha256} from './observer.mjs';
import {
  initializeService,
  loadServiceState,
  migrateServiceState,
  pollOnce,
  readServiceHealth,
  runService,
  validateKnownHosts,
  validateServiceConfig,
} from './service.mjs';

const now = new Date('2030-01-01T00:05:00.000Z');
const profilePath = path.resolve(
  path.dirname(new URL(import.meta.url).pathname.replace(/^\/(.:)/, '$1')),
  'choice-tp-dev.profile.json'
);
const c0ProfilePath = path.resolve(
  path.dirname(new URL(import.meta.url).pathname.replace(/^\/(.:)/, '$1')),
  'choice-tp-dev-c0-t0.profile.json'
);
const expectedByComponent = {
  'customer-api': {
    artifactDigest: `sha256:${'a'.repeat(64)}`,
    composeChecksum: 'sha256:15de3cb3bca419370f0d64d8381938fbb2367f02e266780526b31bc624b67f63',
    configChecksum: `sha256:${'c'.repeat(64)}`,
    schemaVersion: '1.1.0',
    capabilityChecksum: `sha256:${'1'.repeat(64)}`,
    topologyChecksum: `sha256:${'3'.repeat(64)}`,
  },
  'transaction-api': {
    artifactDigest: `sha256:${'b'.repeat(64)}`,
    composeChecksum: 'sha256:15de3cb3bca419370f0d64d8381938fbb2367f02e266780526b31bc624b67f63',
    configChecksum: `sha256:${'d'.repeat(64)}`,
    schemaVersion: '2.1.0',
    capabilityChecksum: `sha256:${'2'.repeat(64)}`,
    topologyChecksum: `sha256:${'4'.repeat(64)}`,
  },
};
const legacyByComponent = {
  'customer-api': {
    stateLabel: 'C0',
    artifactDigest: 'sha256:6e953caf3153a76bdd15c2ee4a3826ebe36c671ff960a1d427398474cf42006c',
    configChecksum: 'sha256:9fc5daa5be6f84f375b2b1566cd635981d4cf5343fbf80410b2f6d507005b75c',
  },
  'transaction-api': {
    stateLabel: 'T0',
    artifactDigest: 'sha256:5ac3a4528ceb716ca3abb3cc4e152ce31a7ce8107d679d9e534c8a9b14f02d98',
    configChecksum: 'sha256:1177424488b2bebd163b84a56af66a3b325f9e13845e8be800b664a5de5a806e',
  },
};

function withChecksum(value) {
  return {...value, canonicalChecksum: sha256(value)};
}

function createLegacyEvidence() {
  return {
    schema: 'emlo.choice-tp-baseline-runtime-evidence/v1',
    capturedReadOnly: true,
    target: 'choice-tp-dev',
    components: Object.entries(legacyByComponent).map(([componentKey, pin]) => ({
      componentKey,
      stateLabel: pin.stateLabel,
      repositoryDigest: pin.artifactDigest,
      appSettingsChecksum: pin.configChecksum,
      runtimeEvidence: {
        classification: 'LEGACY_LIVENESS_ONLY',
        allowedUse: 'BASELINE_OR_ROLLBACK_ONLY',
        standardReadinessClaimed: false,
        healthyPromotionClaimed: false,
      },
    })),
  };
}

function createIntent(profile, overrides = {}) {
  const intent = {
    schemaVersion: 'emlo.choice-tp-observation-intent/v1',
    intentId: 'choice-tp-dev-c1-t1-observation-1',
    targetProfileChecksum: profile.canonicalChecksum,
    organizationId: '10000000-0000-4000-8000-000000000001',
    observerId: '10000000-0000-4000-8000-000000000002',
    deploymentUnitId: '10000000-0000-4000-8000-000000000003',
    observerCredentialSetId: 'choice-tp-independent-observer-v1',
    executorCredentialSetId: 'choice-tp-dev-jenkins-v1',
    notBefore: new Date(now.getTime() - 60_000).toISOString(),
    expiresAt: new Date(now.getTime() + 60_000).toISOString(),
    components: ['customer-api', 'transaction-api'].map((componentKey, index) => ({
      componentKey,
      componentInstanceId: `10000000-0000-4000-8000-00000000000${index + 4}`,
      sourceSequence: index + 10,
      expected: {...expectedByComponent[componentKey], platform: 'linux/amd64'},
    })),
    ...overrides,
  };
  return withChecksum(intent);
}

function createMockSSH(observedByComponent = expectedByComponent) {
  const calls = [];
  const runSSH = async (kind, command) => {
    calls.push({kind, command});
    const componentKey =
      command.includes("'transaction-api'") ||
      command.includes('/transaction-api/') ||
      command.includes('emlo-remittance-transaction') ||
      command.includes(`sha256:${'f'.repeat(64)}`)
        ? 'transaction-api'
        : 'customer-api';
    const expected = observedByComponent[componentKey];
    if (kind === 'container-inspect') {
      const imageID = componentKey === 'customer-api' ? `sha256:${'e'.repeat(64)}` : `sha256:${'f'.repeat(64)}`;
      return `${imageID}\trepository/${componentKey}:candidate\trunning`;
    }
    if (kind === 'image-inspect') {
      return `${JSON.stringify([`repository/${componentKey}@${expected.artifactDigest}`])}\tlinux/amd64`;
    }
    if (kind === 'checksums') {
      const composePath =
        componentKey === 'customer-api'
          ? '/home/emlo-admin/apps/remittance/dev/emlo-remittance-customer/docker-compose.yaml'
          : '/home/emlo-admin/apps/remittance/dev/emlo-remittance-transaction/docker-compose.yaml';
      const configPath =
        componentKey === 'customer-api'
          ? '/home/emlo-admin/apps/remittance/dev/emlo-remittance-customer/appsettings.Production.json'
          : '/home/emlo-admin/apps/remittance/dev/emlo-remittance-transaction/appsettings.Production.json';
      return `${expected.composeChecksum.slice(7)}  ${composePath}\n${expected.configChecksum.slice(7)}  ${configPath}`;
    }
    if (kind === 'runtime-probe') {
      return canonicalJSONString({
        schemaVersion: expected.schemaVersion,
        capabilityChecksum: expected.capabilityChecksum,
        topologyChecksum: expected.topologyChecksum,
      });
    }
    if (kind === 'alive') return '200';
    throw new Error(`unexpected SSH call ${kind}`);
  };
  return {runSSH, calls};
}

async function createFixture(t, configMutator) {
  const directory = await mkdtemp(path.join(tmpdir(), 'choice-tp-observer-service-'));
  t.after(() => rm(directory, {recursive: true, force: true}));
  const credentialsDirectory = path.join(directory, 'credentials');
  const inboxDirectory = path.join(directory, 'inbox');
  const evidenceDirectory = path.join(directory, 'evidence');
  const stateDirectory = path.join(directory, 'state');
  const sshKeyFile = path.join(credentialsDirectory, 'observer-ssh-key');
  const knownHostsFile = path.join(credentialsDirectory, 'known_hosts');
  const observerTokenFile = path.join(credentialsDirectory, 'observer-token');
  const evidencePrivateKeyFile = path.join(credentialsDirectory, 'evidence.pem');
  const legacyEvidenceFile = path.join(directory, 'legacy-c0-t0.json');
  await mkdir(credentialsDirectory, {recursive: true});
  await Promise.all([
    writeFile(knownHostsFile, '217.15.166.6 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestPinOnly\n', {
      mode: 0o600,
    }),
    writeFile(
      sshKeyFile,
      generateKeyPairSync('rsa', {modulusLength: 2048}).privateKey.export({type: 'pkcs8', format: 'pem'}),
      {mode: 0o600}
    ),
  ]);
  const token = 'choice-tp-scoped-observer-token-only-000001';
  const evidenceKey = generateKeyPairSync('ed25519').privateKey.export({type: 'pkcs8', format: 'pem'});
  const legacyBytes = Buffer.from(`${JSON.stringify(createLegacyEvidence(), null, 2)}\n`);
  await Promise.all([
    writeFile(observerTokenFile, `${token}\n`, {mode: 0o600}),
    writeFile(evidencePrivateKeyFile, evidenceKey, {mode: 0o600}),
    writeFile(legacyEvidenceFile, legacyBytes, {mode: 0o600}),
  ]);
  const configWithoutChecksum = {
    schemaVersion: 'emlo.choice-tp-observer-service/v1',
    profileFile: profilePath,
    inboxDirectory,
    evidenceDirectory,
    stateFile: path.join(stateDirectory, 'state.json'),
    lockFile: path.join(stateDirectory, 'service.lock'),
    credentials: {
      sshKeyFile,
      knownHostsFile,
      observerTokenFile,
      observerTokenFingerprint: sha256(token),
      evidencePrivateKeyFile,
      executorCredentialFingerprints: [sha256('jenkins-ssh-key'), sha256('jenkins-api-token')],
    },
    scope: {
      organizationId: '10000000-0000-4000-8000-000000000001',
      observerId: '10000000-0000-4000-8000-000000000002',
      deploymentUnitId: '10000000-0000-4000-8000-000000000003',
      componentInstanceIds: {
        'customer-api': '10000000-0000-4000-8000-000000000004',
        'transaction-api': '10000000-0000-4000-8000-000000000005',
      },
      observerCredentialSetId: 'choice-tp-independent-observer-v1',
      executorCredentialSetId: 'choice-tp-dev-jenkins-v1',
    },
    legacyBaseline: {
      checkpoint: 'C0/T0',
      evidenceFile: legacyEvidenceFile,
      evidenceFileChecksum: sha256(legacyBytes),
      components: structuredClone(legacyByComponent),
    },
    currentRuntime: {
      checkpoint: 'C1/T1',
      componentStateLabels: {'customer-api': 'C1', 'transaction-api': 'T1'},
    },
    polling: {intervalMs: 1000, maxIntentsPerPoll: 8, lockStaleMs: 300_000},
    retry: {
      maxAttemptsPerPoll: 1,
      maxTotalAttemptsPerIntent: 4,
      initialDelayMs: 100,
      maxDelayMs: 1000,
    },
  };
  configMutator?.(configWithoutChecksum);
  const config = withChecksum(configWithoutChecksum);
  const configPath = path.join(directory, 'service.json');
  await writeFile(configPath, `${JSON.stringify(config, null, 2)}\n`, {mode: 0o600});
  const context = await initializeService(configPath);
  await mkdir(inboxDirectory, {recursive: true});
  const profile = JSON.parse(await readFile(config.profileFile, 'utf8'));
  return {directory, config, configPath, context, profile, token, inboxDirectory, evidenceDirectory};
}

async function writeIntent(fixture, intent) {
  const intentPath = path.join(fixture.inboxDirectory, `${intent.intentId}.json`);
  await writeFile(intentPath, `${JSON.stringify(intent, null, 2)}\n`, {mode: 0o600});
  return intentPath;
}

test('service configuration pins scope, known_hosts, C0/T0 evidence, and independent credentials', async (t) => {
  const fixture = await createFixture(t);
  assert.equal(validateServiceConfig(fixture.config), fixture.config);
  assert.equal(fixture.context.config.currentRuntime.checkpoint, 'C1/T1');
  assert.equal(fixture.context.config.legacyBaseline.checkpoint, 'C0/T0');
  assert.throws(
    () => validateKnownHosts('*.example.com ssh-ed25519 AAAATEST\n', fixture.profile),
    /explicit unhashed target host pin/
  );

  const rebound = structuredClone(fixture.config);
  rebound.scope.executorCredentialSetId = rebound.scope.observerCredentialSetId;
  rebound.canonicalChecksum = sha256(
    Object.fromEntries(Object.entries(rebound).filter(([key]) => key !== 'canonicalChecksum'))
  );
  assert.throws(() => validateServiceConfig(rebound), /credential-set IDs must be distinct/);

  const reusedCredential = structuredClone(fixture.config);
  reusedCredential.credentials.executorCredentialFingerprints = [reusedCredential.credentials.observerTokenFingerprint];
  reusedCredential.canonicalChecksum = sha256(
    Object.fromEntries(Object.entries(reusedCredential).filter(([key]) => key !== 'canonicalChecksum'))
  );
  const reusedConfigPath = path.join(fixture.directory, 'reused-credential.json');
  await writeFile(reusedConfigPath, `${JSON.stringify(reusedCredential, null, 2)}\n`, {mode: 0o600});
  await assert.rejects(
    initializeService(reusedConfigPath),
    /observer credential matches a pinned Jenkins\/executor credential fingerprint/
  );
});

test('polling persists signed C1/T1 evidence and skips completed intents after restart', async (t) => {
  const fixture = await createFixture(t);
  const intent = createIntent(fixture.profile);
  await writeIntent(fixture, intent);
  const {runSSH, calls} = createMockSSH();
  const requests = [];
  const first = await pollOnce(fixture.context, {
    now: () => now,
    runSSH,
    sleep: async () => {},
    fetchImpl: async (_url, request) => {
      requests.push(JSON.parse(request.body));
      return new Response('{}', {status: 202});
    },
  });
  assert.equal(first[0].status, 'COMPLETE');
  assert.equal(requests.length, 2);
  assert.ok(requests.every((request) => request.healthEvidenceKind === 'STANDARD_READINESS'));
  assert.ok(requests.every((request) => request.healthEvidenceUse === 'STANDARD_PROMOTION_ELIGIBLE'));
  assert.equal(calls.filter(({kind}) => kind === 'runtime-probe').length, 2);
  const state = await loadServiceState(fixture.config);
  assert.equal(state.intents[intent.canonicalChecksum].status, 'COMPLETE');
  const evidence = JSON.parse(
    await readFile(path.join(fixture.evidenceDirectory, state.intents[intent.canonicalChecksum].evidenceFile), 'utf8')
  );
  assert.equal(evidence.intentChecksum, intent.canonicalChecksum);

  const second = await pollOnce(fixture.context, {
    now: () => new Date(now.getTime() + 30_000),
    runSSH: async () => assert.fail('completed intent must not probe SSH again'),
    sleep: async () => {},
    fetchImpl: async () => assert.fail('completed intent must not submit again'),
  });
  assert.deepEqual(second, []);
});

test('restart replays the exact persisted evidence after a partial submission', async (t) => {
  const fixture = await createFixture(t);
  const intent = createIntent(fixture.profile);
  await writeIntent(fixture, intent);
  const {runSSH} = createMockSSH();
  let firstCalls = 0;
  const first = await pollOnce(fixture.context, {
    now: () => now,
    runSSH,
    sleep: async () => {},
    fetchImpl: async () => {
      firstCalls += 1;
      if (firstCalls === 1) return new Response('{}', {status: 202});
      throw new Error('synthetic transport loss');
    },
  });
  assert.equal(first[0].status, 'FAILED');
  const pending = await loadServiceState(fixture.config);
  const retained = pending.intents[intent.canonicalChecksum];
  assert.equal(retained.status, 'PENDING');
  const originalEvidence = JSON.parse(
    await readFile(path.join(fixture.evidenceDirectory, retained.evidenceFile), 'utf8')
  );

  const replayBodies = [];
  const second = await pollOnce(fixture.context, {
    now: () => new Date(now.getTime() + 120_000),
    runSSH: async () => assert.fail('persisted evidence must be replayed without remeasurement'),
    sleep: async () => {},
    fetchImpl: async (_url, request) => {
      replayBodies.push(JSON.parse(request.body));
      return new Response('{}', {status: 202});
    },
  });
  assert.equal(second[0].status, 'COMPLETE');
  assert.equal(replayBodies.length, 2);
  assert.ok(replayBodies.every((request) => request.evidenceChecksum === originalEvidence.evidenceChecksum));
  assert.ok(replayBodies.every((request) => request.capturedAt === originalEvidence.capturedAt));
});

test('retry and per-intent attempt bounds stop repeated failing submissions', async (t) => {
  const fixture = await createFixture(t, (config) => {
    config.retry.maxAttemptsPerPoll = 3;
    config.retry.maxTotalAttemptsPerIntent = 2;
  });
  const intent = createIntent(fixture.profile);
  await writeIntent(fixture, intent);
  const {runSSH} = createMockSSH();
  const delays = [];
  let submissions = 0;
  const dependencies = {
    now: () => now,
    runSSH,
    sleep: async (delay) => delays.push(delay),
    fetchImpl: async () => {
      submissions += 1;
      return new Response('{}', {status: 503});
    },
  };
  await pollOnce(fixture.context, dependencies);
  await pollOnce(fixture.context, dependencies);
  const afterBound = submissions;
  const third = await pollOnce(fixture.context, dependencies);
  assert.equal(submissions, 6);
  assert.equal(afterBound, submissions);
  assert.deepEqual(delays, [100, 200, 100, 200]);
  assert.deepEqual(third, []);
  const state = await loadServiceState(fixture.config);
  assert.equal(state.intents[intent.canonicalChecksum].status, 'EXHAUSTED');
});

test('restart refuses tampered persisted evidence instead of remeasuring it', async (t) => {
  const fixture = await createFixture(t);
  const intent = createIntent(fixture.profile);
  await writeIntent(fixture, intent);
  const {runSSH} = createMockSSH();
  await pollOnce(fixture.context, {
    now: () => now,
    runSSH,
    sleep: async () => {},
    fetchImpl: async () => {
      throw new Error('synthetic transport loss');
    },
  });
  const pending = await loadServiceState(fixture.config);
  const evidencePath = path.join(fixture.evidenceDirectory, pending.intents[intent.canonicalChecksum].evidenceFile);
  const evidence = JSON.parse(await readFile(evidencePath, 'utf8'));
  evidence.signature.value = `${evidence.signature.value[0] === 'A' ? 'B' : 'A'}${evidence.signature.value.slice(1)}`;
  await writeFile(evidencePath, `${JSON.stringify(evidence, null, 2)}\n`, {mode: 0o600});
  let sshCalls = 0;
  let submissions = 0;
  const result = await pollOnce(fixture.context, {
    now: () => new Date(now.getTime() + 30_000),
    runSSH: async () => {
      sshCalls += 1;
    },
    sleep: async () => {},
    fetchImpl: async () => {
      submissions += 1;
      return new Response('{}', {status: 202});
    },
  });
  assert.equal(result[0].status, 'FAILED');
  assert.equal(sshCalls, 0);
  assert.equal(submissions, 0);
});

test('C1/T1 intents cannot silently reuse digest-pinned C0/T0 runtime artifacts', async (t) => {
  const fixture = await createFixture(t);
  const intent = createIntent(fixture.profile);
  intent.components[0].expected.artifactDigest = legacyByComponent['customer-api'].artifactDigest;
  intent.canonicalChecksum = sha256(
    Object.fromEntries(Object.entries(intent).filter(([key]) => key !== 'canonicalChecksum'))
  );
  await writeIntent(fixture, intent);
  let sshCalls = 0;
  let submissions = 0;
  const result = await pollOnce(fixture.context, {
    now: () => now,
    runSSH: async () => {
      sshCalls += 1;
    },
    sleep: async () => {},
    fetchImpl: async () => {
      submissions += 1;
      return new Response('{}', {status: 202});
    },
  });
  assert.equal(result[0].status, 'FAILED');
  assert.equal(sshCalls, 0);
  assert.equal(submissions, 0);
});

test('malformed intent files are also stopped by the total attempt bound', async (t) => {
  const fixture = await createFixture(t, (config) => {
    config.retry.maxTotalAttemptsPerIntent = 1;
  });
  await writeFile(path.join(fixture.inboxDirectory, 'malformed.json'), '{"intentId":"malformed"}\n', {
    mode: 0o600,
  });
  const first = await pollOnce(fixture.context, {now: () => now, sleep: async () => {}});
  const second = await pollOnce(fixture.context, {now: () => now, sleep: async () => {}});
  assert.equal(first[0].status, 'FAILED');
  assert.deepEqual(second, []);
  const state = await loadServiceState(fixture.config);
  assert.equal(Object.values(state.intents)[0].status, 'EXHAUSTED');
});

test('terminal intent history is pruned before the bounded state reaches capacity', async (t) => {
  const fixture = await createFixture(t);
  const intents = Object.fromEntries(
    Array.from({length: 512}, (_, index) => [
      `sha256:${index.toString(16).padStart(64, '0')}`,
      {
        intentId: `completed-${index}`,
        status: 'COMPLETE',
        attempts: 1,
        updatedAt: new Date(now.getTime() + index).toISOString(),
        failureCode: null,
        evidenceFile: null,
        evidenceChecksum: null,
      },
    ])
  );
  await writeFile(
    fixture.config.stateFile,
    `${JSON.stringify({
      schemaVersion: 'emlo.choice-tp-observer-service-state/v1',
      serviceConfigChecksum: fixture.config.canonicalChecksum,
      intents,
    })}\n`,
    {mode: 0o600}
  );
  const intent = createIntent(fixture.profile);
  await writeIntent(fixture, intent);
  const {runSSH} = createMockSSH();
  const result = await pollOnce(fixture.context, {
    now: () => now,
    runSSH,
    sleep: async () => {},
    fetchImpl: async () => new Response('{}', {status: 202}),
  });
  assert.equal(result[0].status, 'COMPLETE');
  const state = await loadServiceState(fixture.config);
  assert.equal(Object.keys(state.intents).length, 512);
  assert.equal(state.intents[`sha256:${'0'.repeat(64)}`], undefined);
  assert.equal(state.intents[intent.canonicalChecksum].status, 'COMPLETE');
});

test('terminal inbox entries cannot starve pending work before maxIntentsPerPoll is applied', async (t) => {
  const fixture = await createFixture(t, (config) => {
    config.polling.maxIntentsPerPoll = 1;
  });
  const makeIntent = (intentId, sequence) =>
    createIntent(fixture.profile, {
      intentId,
      components: ['customer-api', 'transaction-api'].map((componentKey, index) => ({
        componentKey,
        componentInstanceId: `10000000-0000-4000-8000-00000000000${index + 4}`,
        sourceSequence: sequence + index,
        expected: {...expectedByComponent[componentKey], platform: 'linux/amd64'},
      })),
    });
  const terminalIntents = [
    makeIntent('a-terminal-1', 20),
    makeIntent('a-terminal-2', 30),
    makeIntent('a-terminal-3', 40),
  ];
  for (const intent of terminalIntents) await writeIntent(fixture, intent);
  const pendingIntent = makeIntent('z-pending', 50);
  await writeIntent(fixture, pendingIntent);
  const terminalState = Object.fromEntries(
    terminalIntents.map((intent, index) => [
      intent.canonicalChecksum,
      {
        intentId: intent.intentId,
        status: 'COMPLETE',
        attempts: 1,
        updatedAt: new Date(now.getTime() + index).toISOString(),
        failureCode: null,
        evidenceFile: null,
        evidenceChecksum: null,
      },
    ])
  );
  await writeFile(
    fixture.config.stateFile,
    `${JSON.stringify({
      schemaVersion: 'emlo.choice-tp-observer-service-state/v1',
      serviceConfigChecksum: fixture.config.canonicalChecksum,
      intents: terminalState,
    })}\n`,
    {mode: 0o600}
  );
  const {runSSH} = createMockSSH();
  const result = await pollOnce(fixture.context, {
    now: () => now,
    runSSH,
    sleep: async () => {},
    fetchImpl: async () => new Response('{}', {status: 202}),
  });
  assert.equal(result.length, 1);
  assert.equal(result[0].intentId, pendingIntent.intentId);
  assert.equal(result[0].status, 'COMPLETE');
});

test('sealed C0/T0 command profile remains legacy liveness and rollback-only evidence', async (t) => {
  const fixture = await createFixture(t, (config) => {
    config.profileFile = c0ProfilePath;
    config.currentRuntime = {
      checkpoint: 'C0/T0',
      componentStateLabels: {'customer-api': 'C0', 'transaction-api': 'T0'},
    };
  });
  const observedByComponent = Object.fromEntries(
    Object.entries(expectedByComponent).map(([componentKey, expected]) => [
      componentKey,
      {
        ...expected,
        artifactDigest: legacyByComponent[componentKey].artifactDigest,
        configChecksum: legacyByComponent[componentKey].configChecksum,
      },
    ])
  );
  const intent = createIntent(fixture.profile, {
    intentId: 'choice-tp-dev-c0-t0-observation-1',
    components: ['customer-api', 'transaction-api'].map((componentKey, index) => ({
      componentKey,
      componentInstanceId: `10000000-0000-4000-8000-00000000000${index + 4}`,
      sourceSequence: 60 + index,
      expected: {...observedByComponent[componentKey], platform: 'linux/amd64'},
    })),
  });
  await writeIntent(fixture, intent);
  const {runSSH, calls} = createMockSSH(observedByComponent);
  const requests = [];
  const result = await pollOnce(fixture.context, {
    now: () => now,
    runSSH,
    sleep: async () => {},
    fetchImpl: async (_url, request) => {
      requests.push(JSON.parse(request.body));
      return new Response('{}', {status: 202});
    },
  });
  assert.equal(result[0].status, 'COMPLETE');
  assert.equal(requests.length, 2);
  assert.ok(requests.every(({healthEvidenceKind}) => healthEvidenceKind === 'LEGACY_LIVENESS_ONLY'));
  assert.ok(requests.every(({healthEvidenceUse}) => healthEvidenceUse === 'BASELINE_OR_ROLLBACK_ONLY'));
  assert.ok(calls.filter(({kind}) => kind === 'runtime-probe').every(({command}) => command.includes("'/usr/bin/timeout'")));
  assert.ok(calls.filter(({kind}) => kind === 'alive').every(({command}) => command.includes('/swagger/v1/swagger.json')));
});

test('durable health distinguishes live state from readiness', async (t) => {
  const fixture = await createFixture(t);
  await runService({
    context: fixture.context,
    once: true,
    dependencies: {now: () => now, sleep: async () => {}},
  });
  const ready = await readServiceHealth(fixture.config, now, {requireReady: true});
  assert.equal(ready.status, 'READY');
  assert.equal(ready.ready, true);

  await writeFile(path.join(fixture.inboxDirectory, 'malformed-for-health.json'), '{"invalid":true}\n', {mode: 0o600});
  await runService({
    context: fixture.context,
    once: true,
    dependencies: {now: () => new Date(now.getTime() + 1000), sleep: async () => {}},
  });
  const degraded = await readServiceHealth(fixture.config, new Date(now.getTime() + 1000));
  assert.equal(degraded.status, 'DEGRADED');
  assert.equal(degraded.ready, false);
  await assert.rejects(
    readServiceHealth(fixture.config, new Date(now.getTime() + 1000), {requireReady: true}),
    /live but not ready/
  );
  await assert.rejects(
    readServiceHealth(fixture.config, new Date(now.getTime() + fixture.config.polling.lockStaleMs + 2000)),
    /heartbeat is stale/
  );
});

test('state migration preserves only terminal history with immutable backup and receipt', async (t) => {
  const previous = await createFixture(t);
  const terminalState = {
    schemaVersion: 'emlo.choice-tp-observer-service-state/v1',
    serviceConfigChecksum: previous.config.canonicalChecksum,
    intents: {
      [`sha256:${'9'.repeat(64)}`]: {
        intentId: 'completed-before-upgrade',
        status: 'COMPLETE',
        attempts: 1,
        updatedAt: now.toISOString(),
        failureCode: null,
        evidenceFile: null,
        evidenceChecksum: null,
      },
    },
  };
  const previousStateBytes = `${JSON.stringify(terminalState)}\n`;
  await writeFile(previous.config.stateFile, previousStateBytes, {mode: 0o600});
  const current = await createFixture(t, (config) => {
    config.polling.intervalMs = 2000;
  });
  const migrated = await migrateServiceState({
    currentContext: current.context,
    previousConfigPath: previous.configPath,
    now,
  });
  const currentState = await loadServiceState(current.config);
  assert.equal(currentState.serviceConfigChecksum, current.config.canonicalChecksum);
  assert.equal(currentState.intents[`sha256:${'9'.repeat(64)}`].status, 'COMPLETE');
  assert.equal(await readFile(migrated.backupPath, 'utf8'), previousStateBytes);
  assert.equal(migrated.receipt.previousStateChecksum, sha256(previousStateBytes));
  assert.equal(migrated.receipt.retainedIntentCount, 1);
  assert.equal(JSON.parse(await readFile(migrated.receiptPath, 'utf8')).currentConfigChecksum, current.config.canonicalChecksum);
});

test('state migration refuses pending history or a changed checkpoint contract', async (t) => {
  const previous = await createFixture(t);
  await writeFile(
    previous.config.stateFile,
    `${JSON.stringify({
      schemaVersion: 'emlo.choice-tp-observer-service-state/v1',
      serviceConfigChecksum: previous.config.canonicalChecksum,
      intents: {
        [`sha256:${'8'.repeat(64)}`]: {
          intentId: 'pending-before-upgrade',
          status: 'PENDING',
          attempts: 1,
          updatedAt: now.toISOString(),
          failureCode: 'SUBMISSION_RETRYABLE',
          evidenceFile: 'retained.evidence.json',
          evidenceChecksum: `sha256:${'7'.repeat(64)}`,
        },
      },
    })}\n`,
    {mode: 0o600}
  );
  const current = await createFixture(t, (config) => {
    config.retry.maxTotalAttemptsPerIntent = 5;
  });
  await assert.rejects(
    migrateServiceState({currentContext: current.context, previousConfigPath: previous.configPath, now}),
    /refuses pending or unknown intent state/
  );

  const c0Current = await createFixture(t, (config) => {
    config.profileFile = c0ProfilePath;
    config.currentRuntime = {
      checkpoint: 'C0/T0',
      componentStateLabels: {'customer-api': 'C0', 'transaction-api': 'T0'},
    };
  });
  await assert.rejects(
    migrateServiceState({currentContext: c0Current.context, previousConfigPath: previous.configPath, now}),
    /cannot change target, checkpoint, profile, scope, or legacy pins/
  );
});
