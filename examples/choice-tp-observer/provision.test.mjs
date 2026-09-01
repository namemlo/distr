import assert from 'node:assert/strict';
import {execFile} from 'node:child_process';
import {generateKeyPairSync} from 'node:crypto';
import {mkdtemp, readFile, rm, writeFile} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import test from 'node:test';
import {fileURLToPath} from 'node:url';
import {promisify} from 'node:util';
import {evidencePublicKeyIdentity, sha256, signEvidence} from './observer.mjs';
import {
  buildEvidenceKeyManifest,
  fingerprintCredential,
  prepareEvidenceKeyRotation,
  renderRegistrationRequest,
  validateEvidenceKeyManifest,
  validateRegistrationTemplate,
} from './provision.mjs';

const execFileAsync = promisify(execFile);
const directory = path.dirname(fileURLToPath(import.meta.url));

function withChecksum(value) {
  return {...value, canonicalChecksum: sha256(value)};
}

function createServiceConfig(evidencePublicKeyFingerprint) {
  return withChecksum({
    schemaVersion: 'emlo.choice-tp-observer-service/v1',
    profileFile: path.resolve(directory, 'choice-tp-dev.profile.json'),
    inboxDirectory: path.resolve(directory, 'test-intents'),
    evidenceDirectory: path.resolve(directory, 'test-evidence'),
    stateFile: path.resolve(directory, 'test-state/service-state.json'),
    lockFile: path.resolve(directory, 'test-state/service.lock'),
    credentials: {
      sshKeyFile: path.resolve(directory, 'test-secrets/observer-ssh-key'),
      knownHostsFile: path.resolve(directory, 'known_hosts.example'),
      observerTokenFile: path.resolve(directory, 'test-secrets/observer-token'),
      observerTokenFingerprint: `sha256:${'1'.repeat(64)}`,
      evidencePrivateKeyFile: path.resolve(directory, 'test-secrets/evidence.pem'),
      evidencePublicKeyFingerprint,
      executorCredentialFingerprints: [`sha256:${'2'.repeat(64)}`],
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
      evidenceFile: path.resolve(directory, 'legacy.json'),
      evidenceFileChecksum: `sha256:${'3'.repeat(64)}`,
      components: {
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
      },
    },
    currentRuntime: {
      checkpoint: 'C1/T1',
      componentStateLabels: {'customer-api': 'C1', 'transaction-api': 'T1'},
    },
    polling: {intervalMs: 30000, maxIntentsPerPoll: 8, lockStaleMs: 900000},
    retry: {maxAttemptsPerPoll: 3, maxTotalAttemptsPerIntent: 8, initialDelayMs: 1000, maxDelayMs: 10000},
  });
}

test('public-key export identity exactly matches signed evidence fingerprints', () => {
  const {privateKey} = generateKeyPairSync('ed25519');
  const privateKeyPEM = privateKey.export({type: 'pkcs8', format: 'pem'});
  const manifest = buildEvidenceKeyManifest({
    privateKeyPEM,
    keyId: 'choice-tp-observer-evidence-2030-01',
    activatedAt: '2030-01-01T00:00:00.000Z',
  });
  assert.equal(validateEvidenceKeyManifest(manifest), manifest);
  assert.equal(manifest.keyFingerprint, evidencePublicKeyIdentity(privateKeyPEM).keyFingerprint);
  assert.equal(
    signEvidence({schemaVersion: 'test/v1'}, privateKeyPEM).signature.keyFingerprint,
    manifest.keyFingerprint
  );
  assert.equal(fingerprintCredential('evidence-public-key', privateKeyPEM), manifest.keyFingerprint);
});

test('registration rendering uses the exact Distr API body and keeps secrets out of the retained handoff', async () => {
  const template = validateRegistrationTemplate(
    JSON.parse(await readFile(path.join(directory, 'observer-registration.request.template.json'), 'utf8'))
  );
  const {privateKey} = generateKeyPairSync('ed25519');
  const manifest = buildEvidenceKeyManifest({
    privateKeyPEM: privateKey.export({type: 'pkcs8', format: 'pem'}),
    keyId: 'choice-tp-observer-evidence-2030-01',
    activatedAt: '2030-01-01T00:00:00.000Z',
  });
  const token = 'choice-tp-observer-registration-token-000001';
  const {request, record} = renderRegistrationRequest({
    template,
    token: `${token}\n`,
    evidenceKeyManifest: manifest,
    createdAt: '2030-01-01T00:01:00.000Z',
  });
  assert.equal(request.credential, token);
  assert.equal(record.endpoint, '/api/v1/observer-registrations');
  assert.equal(record.observerTokenFingerprint, sha256(token));
  assert.equal(record.evidencePublicKeyFingerprint, manifest.keyFingerprint);
  assert.doesNotMatch(JSON.stringify(record), new RegExp(token));
  assert.equal(fingerprintCredential('normalized-token', `${token}\n`), record.observerTokenFingerprint);
});

test('rotation receipt verifies retained evidence and refuses non-terminal state', () => {
  const currentPair = generateKeyPairSync('ed25519');
  const nextPair = generateKeyPairSync('ed25519');
  const current = buildEvidenceKeyManifest({
    privateKeyPEM: currentPair.privateKey.export({type: 'pkcs8', format: 'pem'}),
    keyId: 'choice-tp-observer-evidence-2030-01',
    activatedAt: '2030-01-01T00:00:00.000Z',
  });
  const next = buildEvidenceKeyManifest({
    privateKeyPEM: nextPair.privateKey.export({type: 'pkcs8', format: 'pem'}),
    keyId: 'choice-tp-observer-evidence-2030-02',
    activatedAt: '2030-02-01T00:00:00.000Z',
    previousKeyFingerprint: current.keyFingerprint,
  });
  const serviceConfig = createServiceConfig(current.keyFingerprint);
  const evidence = signEvidence(
    {schemaVersion: 'emlo.choice-tp-observation-evidence/v1', intentId: 'retained-intent'},
    currentPair.privateKey.export({type: 'pkcs8', format: 'pem'})
  );
  const state = {
    schemaVersion: 'emlo.choice-tp-observer-service-state/v1',
    serviceConfigChecksum: serviceConfig.canonicalChecksum,
    intents: {
      'sha256:test': {
        status: 'COMPLETE',
        evidenceFile: 'retained.evidence.json',
        evidenceChecksum: evidence.evidenceChecksum,
      },
    },
  };
  const receipt = prepareEvidenceKeyRotation({
    serviceConfig,
    state,
    keyManifests: [current],
    nextKeyManifest: next,
    evidenceFiles: [{name: 'retained.evidence.json', evidence}],
    preparedAt: '2030-01-31T00:00:00.000Z',
  });
  assert.equal(receipt.previousKeyFingerprint, current.keyFingerprint);
  assert.equal(receipt.nextKeyFingerprint, next.keyFingerprint);
  assert.equal(receipt.retainedEvidenceCount, 1);
  assert.match(receipt.canonicalChecksum, /^sha256:[0-9a-f]{64}$/);

  const disconnectedPair = generateKeyPairSync('ed25519');
  const disconnected = buildEvidenceKeyManifest({
    privateKeyPEM: disconnectedPair.privateKey.export({type: 'pkcs8', format: 'pem'}),
    keyId: 'unrelated-observer-evidence-2030-01',
    activatedAt: '2030-01-15T00:00:00.000Z',
  });
  assert.throws(
    () =>
      prepareEvidenceKeyRotation({
        serviceConfig,
        state,
        keyManifests: [current, disconnected],
        nextKeyManifest: next,
        evidenceFiles: [{name: 'retained.evidence.json', evidence}],
        preparedAt: '2030-01-31T00:00:00.000Z',
      }),
    /disconnected key/
  );

  const pending = structuredClone(state);
  pending.intents['sha256:test'].status = 'PENDING';
  assert.throws(
    () =>
      prepareEvidenceKeyRotation({
        serviceConfig,
        state: pending,
        keyManifests: [current],
        nextKeyManifest: next,
        evidenceFiles: [{name: 'retained.evidence.json', evidence}],
        preparedAt: '2030-01-31T00:00:00.000Z',
      }),
    /refuses pending or unknown intent state/
  );
});

test('export command writes append-only public material without printing private key bytes', async (t) => {
  const root = await mkdtemp(path.join(tmpdir(), 'choice-tp-observer-key-export-'));
  t.after(() => rm(root, {recursive: true, force: true}));
  const {privateKey} = generateKeyPairSync('ed25519');
  const privateKeyPEM = privateKey.export({type: 'pkcs8', format: 'pem'});
  const privateKeyFile = path.join(root, 'private.pem');
  const publicKeyFile = path.join(root, 'public.pem');
  const manifestFile = path.join(root, 'active.key.json');
  await writeFile(privateKeyFile, privateKeyPEM, {mode: 0o600});
  const args = [
    path.join(directory, 'provision.mjs'),
    'export-evidence-key',
    '--private-key-file',
    privateKeyFile,
    '--public-key-file',
    publicKeyFile,
    '--manifest-file',
    manifestFile,
    '--key-id',
    'choice-tp-observer-evidence-2030-01',
    '--activated-at',
    '2030-01-01T00:00:00.000Z',
  ];
  const first = await execFileAsync(process.execPath, args);
  assert.doesNotMatch(first.stdout, /BEGIN PRIVATE KEY/);
  assert.match(await readFile(publicKeyFile, 'utf8'), /BEGIN PUBLIC KEY/);
  validateEvidenceKeyManifest(JSON.parse(await readFile(manifestFile, 'utf8')));
  await assert.rejects(execFileAsync(process.execPath, args), /EEXIST|file already exists/i);

  const cleanedPublicKeyFile = path.join(root, 'cleaned-after-pair-failure.pem');
  const collisionArgs = [...args];
  collisionArgs[5] = cleanedPublicKeyFile;
  await assert.rejects(execFileAsync(process.execPath, collisionArgs), /EEXIST|file already exists/i);
  await assert.rejects(readFile(cleanedPublicKeyFile), (error) => error.code === 'ENOENT');
});
