import assert from 'node:assert/strict';
import {generateKeyPairSync, verify} from 'node:crypto';
import {mkdtemp, readFile, rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import test from 'node:test';
import {
  buildObservationRequests,
  buildReadOnlyCommands,
  canonicalJSONString,
  runObserver,
  sha256,
  signEvidence,
  validateIntent,
  validateProfile,
} from './observer.mjs';

const profileWithoutChecksum = {
  schemaVersion: 'emlo.choice-tp-observer-profile/v1',
  profileId: 'choice-tp-dev',
  host: '217.15.166.6',
  port: 22,
  user: 'emlo-admin',
  platform: 'linux/amd64',
  distrBaseUrl: 'https://distr.emlotech.com',
  gateway: {
    url: 'http://127.0.0.1:12000',
    hostHeader: 'api-gateway.dev.choice-tp.emlotech.com',
  },
  components: {
    'customer-api': {
      container: 'customer-api',
      composePath: '/home/emlo-admin/apps/remittance/dev/emlo-remittance-customer/docker-compose.yaml',
      configPath: '/home/emlo-admin/apps/remittance/dev/emlo-remittance-customer/appsettings.Production.json',
      alivePath: '/customer-api/alive',
      healthzPath: '/customer-api/healthz',
    },
    'transaction-api': {
      container: 'transaction-api',
      composePath: '/home/emlo-admin/apps/remittance/dev/emlo-remittance-transaction/docker-compose.yaml',
      configPath: '/home/emlo-admin/apps/remittance/dev/emlo-remittance-transaction/appsettings.Production.json',
      alivePath: '/transaction-api/alive',
      healthzPath: '/transaction-api/healthz',
    },
  },
};
const profile = {...profileWithoutChecksum, canonicalChecksum: sha256(profileWithoutChecksum)};
const expectedByComponent = {
  'customer-api': {
    artifactDigest: 'sha256:6e953caf3153a76bdd15c2ee4a3826ebe36c671ff960a1d427398474cf42006c',
    composeChecksum: 'sha256:15de3cb3bca419370f0d64d8381938fbb2367f02e266780526b31bc624b67f63',
    configChecksum: 'sha256:9fc5daa5be6f84f375b2b1566cd635981d4cf5343fbf80410b2f6d507005b75c',
  },
  'transaction-api': {
    artifactDigest: 'sha256:5ac3a4528ceb716ca3abb3cc4e152ce31a7ce8107d679d9e534c8a9b14f02d98',
    composeChecksum: 'sha256:15de3cb3bca419370f0d64d8381938fbb2367f02e266780526b31bc624b67f63',
    configChecksum: 'sha256:1177424488b2bebd163b84a56af66a3b325f9e13845e8be800b664a5de5a806e',
  },
};

function createIntent(now = new Date('2030-01-01T00:05:00.000Z')) {
  const intent = {
    schemaVersion: 'emlo.choice-tp-observation-intent/v1',
    intentId: 'choice-tp-dev-c0-t0-observation-1',
    targetProfileChecksum: profile.canonicalChecksum,
    organizationId: '10000000-0000-4000-8000-000000000001',
    observerId: '10000000-0000-4000-8000-000000000002',
    deploymentUnitId: '10000000-0000-4000-8000-000000000003',
    observerCredentialSetId: 'choice-tp-independent-observer-v1',
    executorCredentialSetId: 'choice-tp-jenkins-executor-v1',
    notBefore: new Date(now.getTime() - 60_000).toISOString(),
    expiresAt: new Date(now.getTime() + 60_000).toISOString(),
    components: ['customer-api', 'transaction-api'].map((componentKey, index) => ({
      componentKey,
      componentInstanceId: `10000000-0000-4000-8000-00000000000${index + 4}`,
      sourceSequence: index + 1,
      expected: {
        ...expectedByComponent[componentKey],
        schemaVersion: 'no-workload-db-query',
        capabilityChecksum: `sha256:${String(index + 1).repeat(64)}`,
        platform: 'linux/amd64',
        topologyChecksum: `sha256:${String(index + 3).repeat(64)}`,
      },
    })),
  };
  return {...intent, canonicalChecksum: sha256(intent)};
}

function createMockSSH({aliveStatus = 200, healthzStatus = 200, configMismatch = false} = {}) {
  const calls = [];
  const runSSH = async (kind, command) => {
    calls.push({kind, command});
    const componentKey =
      command.includes("'transaction-api'") ||
      command.includes('emlo-remittance-transaction') ||
      command.includes('e77fe1d2b9a1d08f417678aa6bd85824f9c7e0277c28afc34a6f049b51713b39')
        ? 'transaction-api'
        : 'customer-api';
    const expected = expectedByComponent[componentKey];
    if (kind === 'container-inspect') {
      return componentKey === 'customer-api'
        ? 'sha256:dcf99672ca5c8a744a44af189ba2526f04fb3d4b564e3bfa82f8232e875d707e\trepository/customer:immutable\trunning'
        : 'sha256:e77fe1d2b9a1d08f417678aa6bd85824f9c7e0277c28afc34a6f049b51713b39\trepository/transaction:immutable\trunning';
    }
    if (kind === 'image-inspect') {
      return `${JSON.stringify([`repository/${componentKey}@${expected.artifactDigest}`])}\tlinux/amd64`;
    }
    if (kind === 'checksums') {
      const component = profile.components[componentKey];
      const configChecksum = configMismatch ? `sha256:${'f'.repeat(64)}` : expected.configChecksum;
      return `${expected.composeChecksum.slice(7)}  ${component.composePath}\n${configChecksum.slice(7)}  ${component.configPath}`;
    }
    if (kind === 'alive') {
      return String(aliveStatus);
    }
    if (kind === 'healthz') {
      return String(healthzStatus);
    }
    throw new Error(`unexpected mock SSH command kind ${kind}`);
  };
  return {runSSH, calls};
}

test('canonical JSON and checksums are stable across object key order', () => {
  assert.equal(canonicalJSONString({b: 2, a: [3, {d: 4, c: 5}]}), '{"a":[3,{"c":5,"d":4}],"b":2}');
  assert.equal(sha256({b: 2, a: 1}), sha256({a: 1, b: 2}));
});

test('fixed profile and immutable intent reject target or credential rebinding', () => {
  const now = new Date('2030-01-01T00:05:00.000Z');
  assert.equal(validateProfile(profile), profile);
  assert.equal(validateIntent(createIntent(now), profile, now).components.length, 2);

  const reboundProfile = {...profile, host: '127.0.0.1'};
  assert.throws(() => validateProfile(reboundProfile), /host is not the fixed/);

  const intent = createIntent(now);
  const rebound = {
    ...intent,
    executorCredentialSetId: intent.observerCredentialSetId,
  };
  rebound.canonicalChecksum = sha256({...rebound, canonicalChecksum: undefined});
  assert.throws(() => validateIntent(rebound, profile, now), /observer credentials must be distinct/);
});

test('SSH command surface is fixed, bounded, and read-only', () => {
  for (const componentKey of Object.keys(profile.components)) {
    const commands = buildReadOnlyCommands(profile, componentKey);
    const rendered = [
      commands.containerInspect,
      commands.imageInspect(`sha256:${'a'.repeat(64)}`),
      commands.checksum,
      commands.alive,
      commands.healthz,
    ];
    for (const command of rendered) {
      assert.match(command, /^'\/usr\/bin\/(docker|sha256sum|curl)'/);
      assert.doesNotMatch(
        command,
        /'\/usr\/bin\/docker'\s+'(?:up|run|start|stop|restart|rm|rmi|pull|push|compose|exec|cp|kill|update|create|system|prune)'/
      );
      assert.doesNotMatch(command, /'\/usr\/bin\/(?:psql|mysql|mongo)'/);
      assert.doesNotMatch(command, /[\r\n;&><`]/);
    }
  }
});

test('observer verifies exact runtime evidence, signs it, writes mode-safe output, and submits only Distr observation requests', async () => {
  const now = new Date('2030-01-01T00:05:00.000Z');
  const intent = createIntent(now);
  const {privateKey, publicKey} = generateKeyPairSync('ed25519');
  const privateKeyPEM = privateKey.export({type: 'pkcs8', format: 'pem'});
  const {runSSH, calls} = createMockSSH();
  const submissions = [];
  const fetchImpl = async (url, request) => {
    submissions.push({url, request});
    return new Response('{}', {status: 202, headers: {'Content-Type': 'application/json'}});
  };
  const directory = await mkdtemp(path.join(tmpdir(), 'choice-tp-observer-'));
  const outputPath = path.join(directory, 'evidence.json');
  try {
    const result = await runObserver({
      profile,
      intent,
      runSSH,
      token: 'observer-token-that-is-distinct-and-long-enough',
      privateKeyPEM,
      outputPath,
      now,
      fetchImpl,
    });
    assert.match(result.evidenceChecksum, /^sha256:[0-9a-f]{64}$/);
    assert.equal(result.signedEvidence.components.length, 2);
    assert.ok(result.signedEvidence.components.every(({health}) => health === 'HEALTHY'));
    assert.equal(calls.filter(({kind}) => kind === 'healthz').length, 0);
    assert.equal(submissions.length, 2);
    for (const {url, request} of submissions) {
      assert.equal(url, 'https://distr.emlotech.com/api/observer/v1/observations');
      assert.equal(request.headers.Authorization, 'Observer observer-token-that-is-distinct-and-long-enough');
      const body = JSON.parse(request.body);
      assert.deepEqual(Object.keys(body).sort(), [
        'artifactDigest',
        'capabilityChecksum',
        'capturedAt',
        'componentInstanceId',
        'componentKey',
        'configChecksum',
        'deploymentUnitId',
        'evidenceChecksum',
        'evidenceReference',
        'health',
        'observerId',
        'organizationId',
        'outcome',
        'platform',
        'schemaVersion',
        'sourceSequence',
        'topologyChecksum',
      ]);
    }
    const persisted = JSON.parse(await readFile(outputPath, 'utf8'));
    const core = Object.fromEntries(
      Object.entries(persisted).filter(([key]) => !['evidenceChecksum', 'signature'].includes(key))
    );
    assert.equal(persisted.evidenceChecksum, sha256(core));
    assert.equal(
      verify(null, Buffer.from(canonicalJSONString(core)), publicKey, Buffer.from(persisted.signature.value, 'base64')),
      true
    );
    assert.doesNotMatch(await readFile(outputPath, 'utf8'), /observer-token-that-is-distinct/);
  } finally {
    await rm(directory, {recursive: true, force: true});
  }
});

test('observer probes /alive first, falls back to /healthz, and reports mismatches truthfully', async () => {
  const now = new Date('2030-01-01T00:05:00.000Z');
  const intent = createIntent(now);
  const {privateKey} = generateKeyPairSync('ed25519');
  const {runSSH, calls} = createMockSSH({aliveStatus: 404, healthzStatus: 200, configMismatch: true});
  const bodies = [];
  const result = await runObserver({
    profile,
    intent,
    runSSH,
    token: 'observer-token-that-is-distinct-and-long-enough',
    privateKeyPEM: privateKey.export({type: 'pkcs8', format: 'pem'}),
    now,
    fetchImpl: async (_url, request) => {
      bodies.push(JSON.parse(request.body));
      return new Response('{}', {status: 202});
    },
  });
  assert.equal(calls.filter(({kind}) => kind === 'alive').length, 2);
  assert.equal(calls.filter(({kind}) => kind === 'healthz').length, 2);
  assert.ok(result.signedEvidence.components.every(({health}) => health === 'UNHEALTHY'));
  assert.ok(bodies.every(({health, outcome}) => health === 'UNHEALTHY' && outcome === 'FAILED'));
});

test('signed evidence and API requests are bounded and never include credentials', () => {
  const {privateKey} = generateKeyPairSync('ed25519');
  const evidence = signEvidence(
    {
      schemaVersion: 'emlo.choice-tp-observation-evidence/v1',
      intentId: 'intent',
      intentChecksum: `sha256:${'a'.repeat(64)}`,
      targetProfileChecksum: `sha256:${'b'.repeat(64)}`,
      capturedAt: '2030-01-01T00:00:00.000Z',
      observerCredentialSetId: 'observer-v1',
      components: [],
    },
    privateKey.export({type: 'pkcs8', format: 'pem'})
  );
  assert.doesNotMatch(JSON.stringify(evidence), /private|token|authorization/i);
  assert.deepEqual(buildObservationRequests(createIntent(), {...evidence, components: []}), []);
});
