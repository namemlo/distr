#!/usr/bin/env node

import {execFile} from 'node:child_process';
import {createHash, createPublicKey, sign, timingSafeEqual} from 'node:crypto';
import {chmod, readFile, writeFile} from 'node:fs/promises';
import {platform as hostPlatform} from 'node:os';
import {fileURLToPath} from 'node:url';
import {promisify} from 'node:util';

const execFileAsync = promisify(execFile);
const digestPattern = /^sha256:[0-9a-f]{64}$/;
const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const safeRemoteValuePattern = /^[A-Za-z0-9_./:@%+=,-]+$/;
const maximumIntentBytes = 64 * 1024;
const maximumEvidenceBytes = 64 * 1024;
const maximumCommandBytes = 32 * 1024;
const fixedProfileIdentity = Object.freeze({
  profileId: 'choice-tp-dev',
  host: '217.15.166.6',
  port: 22,
  user: 'emlo-admin',
  platform: 'linux/amd64',
  distrBaseUrl: 'https://distr.emlotech.com',
});
const fixedComponents = Object.freeze({
  'customer-api': Object.freeze({
    container: 'customer-api',
    composePath: '/home/emlo-admin/apps/remittance/dev/emlo-remittance-customer/docker-compose.yaml',
    configPath: '/home/emlo-admin/apps/remittance/dev/emlo-remittance-customer/appsettings.Production.json',
    alivePath: '/customer-api/alive',
    healthzPath: '/customer-api/healthz',
  }),
  'transaction-api': Object.freeze({
    container: 'transaction-api',
    composePath: '/home/emlo-admin/apps/remittance/dev/emlo-remittance-transaction/docker-compose.yaml',
    configPath: '/home/emlo-admin/apps/remittance/dev/emlo-remittance-transaction/appsettings.Production.json',
    alivePath: '/transaction-api/alive',
    healthzPath: '/transaction-api/healthz',
  }),
});

export function canonicalJSONString(value) {
  if (Array.isArray(value)) {
    return `[${value.map(canonicalJSONString).join(',')}]`;
  }
  if (value && typeof value === 'object') {
    return `{${Object.keys(value)
      .sort()
      .map((key) => `${JSON.stringify(key)}:${canonicalJSONString(value[key])}`)
      .join(',')}}`;
  }
  return JSON.stringify(value);
}

export function sha256(value) {
  const bytes = typeof value === 'string' || Buffer.isBuffer(value) ? value : canonicalJSONString(value);
  return `sha256:${createHash('sha256').update(bytes).digest('hex')}`;
}

function withoutField(value, field) {
  return Object.fromEntries(Object.entries(value).filter(([key]) => key !== field));
}

function requireExactKeys(value, expected, label) {
  const actual = Object.keys(value ?? {}).sort();
  const wanted = [...expected].sort();
  if (actual.length !== wanted.length || actual.some((key, index) => key !== wanted[index])) {
    throw new Error(`${label} contains unsupported or missing fields`);
  }
}

function requireString(value, label, maximum = 512) {
  if (typeof value !== 'string' || value.length === 0 || value.length > maximum) {
    throw new Error(`${label} is invalid`);
  }
}

function requireDigest(value, label) {
  if (!digestPattern.test(value ?? '')) {
    throw new Error(`${label} must be a lowercase sha256 digest`);
  }
}

function requireUUID(value, label) {
  if (!uuidPattern.test(value ?? '')) {
    throw new Error(`${label} must be a UUID`);
  }
}

function constantTimeTextEqual(actual, expected) {
  const left = Buffer.from(actual ?? '');
  const right = Buffer.from(expected ?? '');
  return left.length === right.length && timingSafeEqual(left, right);
}

export function validateProfile(profile) {
  requireExactKeys(
    profile,
    [
      'schemaVersion',
      'profileId',
      'host',
      'port',
      'user',
      'platform',
      'distrBaseUrl',
      'gateway',
      'components',
      'canonicalChecksum',
    ],
    'target profile'
  );
  for (const [key, expected] of Object.entries(fixedProfileIdentity)) {
    if (profile[key] !== expected) {
      throw new Error(`target profile ${key} is not the fixed Choice TP DEV value`);
    }
  }
  if (profile.schemaVersion !== 'emlo.choice-tp-observer-profile/v1') {
    throw new Error('target profile schema is unsupported');
  }
  requireExactKeys(profile.gateway, ['url', 'hostHeader'], 'target profile gateway');
  if (profile.gateway.url !== 'http://127.0.0.1:12000') {
    throw new Error('target profile gateway URL is not fixed');
  }
  if (profile.gateway.hostHeader !== 'api-gateway.dev.choice-tp.emlotech.com') {
    throw new Error('target profile gateway host header is not fixed');
  }
  requireExactKeys(profile.components, Object.keys(fixedComponents), 'target profile components');
  for (const [componentKey, expected] of Object.entries(fixedComponents)) {
    const actual = profile.components[componentKey];
    requireExactKeys(actual, Object.keys(expected), `${componentKey} target profile`);
    for (const [key, value] of Object.entries(expected)) {
      if (actual[key] !== value) {
        throw new Error(`${componentKey} ${key} is not the fixed Choice TP DEV value`);
      }
      if (!safeRemoteValuePattern.test(actual[key])) {
        throw new Error(`${componentKey} ${key} is unsafe for a read-only SSH command`);
      }
    }
  }
  requireDigest(profile.canonicalChecksum, 'target profile canonicalChecksum');
  const calculated = sha256(withoutField(profile, 'canonicalChecksum'));
  if (!constantTimeTextEqual(calculated, profile.canonicalChecksum)) {
    throw new Error('target profile canonical checksum does not match');
  }
  return profile;
}

export function validateIntent(intent, profile, now = new Date()) {
  requireExactKeys(
    intent,
    [
      'schemaVersion',
      'intentId',
      'targetProfileChecksum',
      'organizationId',
      'observerId',
      'deploymentUnitId',
      'observerCredentialSetId',
      'executorCredentialSetId',
      'notBefore',
      'expiresAt',
      'components',
      'canonicalChecksum',
    ],
    'observation intent'
  );
  if (intent.schemaVersion !== 'emlo.choice-tp-observation-intent/v1') {
    throw new Error('observation intent schema is unsupported');
  }
  requireString(intent.intentId, 'intentId', 128);
  requireUUID(intent.organizationId, 'organizationId');
  requireUUID(intent.observerId, 'observerId');
  requireUUID(intent.deploymentUnitId, 'deploymentUnitId');
  requireString(intent.observerCredentialSetId, 'observerCredentialSetId', 128);
  requireString(intent.executorCredentialSetId, 'executorCredentialSetId', 128);
  if (intent.observerCredentialSetId === intent.executorCredentialSetId) {
    throw new Error('observer credentials must be distinct from Jenkins/executor credentials');
  }
  requireDigest(intent.targetProfileChecksum, 'targetProfileChecksum');
  if (!constantTimeTextEqual(intent.targetProfileChecksum, profile.canonicalChecksum)) {
    throw new Error('observation intent targets a different profile checksum');
  }
  const notBefore = new Date(intent.notBefore);
  const expiresAt = new Date(intent.expiresAt);
  if (!Number.isFinite(notBefore.getTime()) || !Number.isFinite(expiresAt.getTime())) {
    throw new Error('observation intent times must be RFC3339 timestamps');
  }
  if (notBefore >= expiresAt || now < notBefore || now > expiresAt) {
    throw new Error('observation intent is outside its immutable validity window');
  }
  if (!Array.isArray(intent.components) || intent.components.length !== 2) {
    throw new Error('observation intent must contain exactly customer-api and transaction-api');
  }
  const componentKeys = intent.components.map(({componentKey}) => componentKey).sort();
  if (componentKeys.join(',') !== Object.keys(fixedComponents).sort().join(',')) {
    throw new Error('observation intent component scope is not the fixed Choice TP DEV pair');
  }
  const sequences = new Set();
  for (const component of intent.components) {
    requireExactKeys(
      component,
      ['componentKey', 'componentInstanceId', 'sourceSequence', 'expected'],
      `${component.componentKey} intent component`
    );
    requireUUID(component.componentInstanceId, `${component.componentKey} componentInstanceId`);
    if (!Number.isSafeInteger(component.sourceSequence) || component.sourceSequence < 1) {
      throw new Error(`${component.componentKey} sourceSequence must be a positive integer`);
    }
    const sequenceKey = `${component.componentInstanceId}:${component.sourceSequence}`;
    if (sequences.has(sequenceKey)) {
      throw new Error('observation intent reuses a component sequence');
    }
    sequences.add(sequenceKey);
    requireExactKeys(
      component.expected,
      [
        'artifactDigest',
        'composeChecksum',
        'configChecksum',
        'schemaVersion',
        'capabilityChecksum',
        'platform',
        'topologyChecksum',
      ],
      `${component.componentKey} expected observation`
    );
    for (const field of [
      'artifactDigest',
      'composeChecksum',
      'configChecksum',
      'capabilityChecksum',
      'topologyChecksum',
    ]) {
      requireDigest(component.expected[field], `${component.componentKey} ${field}`);
    }
    requireString(component.expected.schemaVersion, `${component.componentKey} schemaVersion`, 256);
    if (component.expected.platform !== profile.platform) {
      throw new Error(`${component.componentKey} expected platform does not match the target profile`);
    }
  }
  requireDigest(intent.canonicalChecksum, 'observation intent canonicalChecksum');
  const calculated = sha256(withoutField(intent, 'canonicalChecksum'));
  if (!constantTimeTextEqual(calculated, intent.canonicalChecksum)) {
    throw new Error('observation intent canonical checksum does not match');
  }
  return intent;
}

function shellQuote(value) {
  if (typeof value !== 'string' || value.includes('\u0000') || /[\r\n]/.test(value)) {
    throw new Error('remote command argument is invalid');
  }
  return `'${value.replaceAll("'", `'"'"'`)}'`;
}

function remoteCommand(executable, args) {
  const allowlist = new Set(['/usr/bin/docker', '/usr/bin/sha256sum', '/usr/bin/curl']);
  if (!allowlist.has(executable)) {
    throw new Error('remote executable is not in the read-only allowlist');
  }
  return [executable, ...args].map(shellQuote).join(' ');
}

export function buildReadOnlyCommands(profile, componentKey) {
  const component = profile.components[componentKey];
  if (!component) {
    throw new Error('component is not in the fixed target profile');
  }
  const containerInspect = remoteCommand('/usr/bin/docker', [
    'inspect',
    '--type',
    'container',
    '--format',
    '{{.Image}}\t{{.Config.Image}}\t{{.State.Status}}',
    component.container,
  ]);
  const checksum = remoteCommand('/usr/bin/sha256sum', [component.composePath, component.configPath]);
  const health = (path) =>
    remoteCommand('/usr/bin/curl', [
      '--silent',
      '--show-error',
      '--output',
      '/dev/null',
      '--write-out',
      '%{http_code}',
      '--max-time',
      '8',
      '--header',
      `Host: ${profile.gateway.hostHeader}`,
      `${profile.gateway.url}${path}`,
    ]);
  return {
    containerInspect,
    imageInspect: (imageId) => {
      requireDigest(imageId, 'running image ID');
      return remoteCommand('/usr/bin/docker', [
        'image',
        'inspect',
        '--format',
        '{{json .RepoDigests}}\t{{.Os}}/{{.Architecture}}',
        imageId,
      ]);
    },
    checksum,
    alive: health(component.alivePath),
    healthz: health(component.healthzPath),
  };
}

export function createSSHRunner({profile, sshKeyFile, knownHostsFile, sshBinary = 'ssh'}) {
  for (const [label, value] of [
    ['sshKeyFile', sshKeyFile],
    ['knownHostsFile', knownHostsFile],
  ]) {
    requireString(value, label, 4096);
  }
  const target = `${profile.user}@${profile.host}`;
  return async (kind, command) => {
    const args = [
      '-F',
      hostPlatform() === 'win32' ? 'NUL' : '/dev/null',
      '-o',
      'BatchMode=yes',
      '-o',
      'IdentitiesOnly=yes',
      '-o',
      'StrictHostKeyChecking=yes',
      '-o',
      `UserKnownHostsFile=${knownHostsFile}`,
      '-o',
      'ConnectTimeout=10',
      '-p',
      String(profile.port),
      '-i',
      sshKeyFile,
      target,
      command,
    ];
    try {
      const {stdout} = await execFileAsync(sshBinary, args, {
        encoding: 'utf8',
        maxBuffer: maximumCommandBytes,
        timeout: 20_000,
        windowsHide: true,
      });
      return stdout.trim();
    } catch (error) {
      throw new Error(`read-only SSH ${kind} probe failed (exit ${error.code ?? 'unknown'})`);
    }
  };
}

function parseContainerInspect(output) {
  const fields = output.split('\t');
  if (fields.length !== 3) {
    throw new Error('container inspection returned an invalid bounded record');
  }
  const [imageId, configuredImage, state] = fields;
  requireDigest(imageId, 'container image ID');
  requireString(configuredImage, 'configured image', 2048);
  if (!safeRemoteValuePattern.test(configuredImage)) {
    throw new Error('configured image contains unsupported characters');
  }
  if (state !== 'running') {
    throw new Error('container is not running');
  }
  return {imageId, configuredImage, state};
}

function parseImageInspect(output, expectedPlatform) {
  const fields = output.split('\t');
  if (fields.length !== 2) {
    throw new Error('image inspection returned an invalid bounded record');
  }
  let repoDigests;
  try {
    repoDigests = JSON.parse(fields[0]);
  } catch {
    throw new Error('image inspection returned invalid RepoDigests JSON');
  }
  if (!Array.isArray(repoDigests) || repoDigests.length === 0 || repoDigests.length > 16) {
    throw new Error('image inspection did not return bounded immutable RepoDigests');
  }
  const digests = repoDigests.map((reference) => {
    requireString(reference, 'RepoDigest', 2048);
    const separator = reference.lastIndexOf('@');
    if (separator < 1) {
      throw new Error('RepoDigest is not immutable');
    }
    const digest = reference.slice(separator + 1);
    requireDigest(digest, 'RepoDigest');
    return {reference, digest};
  });
  if (fields[1] !== expectedPlatform) {
    throw new Error(`running image platform ${fields[1]} does not match ${expectedPlatform}`);
  }
  return {repoDigests: digests, platform: fields[1]};
}

export function parseChecksums(output, component) {
  const byPath = new Map();
  for (const line of output.split(/\r?\n/)) {
    const match = /^([0-9a-f]{64})\s+(.+)$/.exec(line);
    if (!match || byPath.has(match[2])) {
      throw new Error('sha256sum returned an invalid bounded record');
    }
    byPath.set(match[2], `sha256:${match[1]}`);
  }
  if (byPath.size !== 2 || !byPath.has(component.composePath) || !byPath.has(component.configPath)) {
    throw new Error('sha256sum did not return the exact fixed compose and config paths');
  }
  return {
    composeChecksum: byPath.get(component.composePath),
    configChecksum: byPath.get(component.configPath),
  };
}

function parseHealthStatus(output) {
  if (!/^[0-9]{3}$/.test(output)) {
    throw new Error('health probe did not return one HTTP status code');
  }
  return Number.parseInt(output, 10);
}

function isHealthyStatus(status) {
  return status >= 200 && status < 300;
}

export async function observeComponent({profile, intentComponent, runSSH, capturedAt}) {
  const profileComponent = profile.components[intentComponent.componentKey];
  const commands = buildReadOnlyCommands(profile, intentComponent.componentKey);
  const container = parseContainerInspect(await runSSH('container-inspect', commands.containerInspect));
  const image = parseImageInspect(
    await runSSH('image-inspect', commands.imageInspect(container.imageId)),
    intentComponent.expected.platform
  );
  const checksums = parseChecksums(await runSSH('checksums', commands.checksum), profileComponent);
  const aliveStatus = parseHealthStatus(await runSSH('alive', commands.alive));
  let healthzStatus = null;
  let healthPath = profileComponent.alivePath;
  let healthStatus = aliveStatus;
  if (!isHealthyStatus(aliveStatus)) {
    healthzStatus = parseHealthStatus(await runSSH('healthz', commands.healthz));
    healthPath = profileComponent.healthzPath;
    healthStatus = healthzStatus;
  }
  const exactArtifact = image.repoDigests.find(({digest}) => digest === intentComponent.expected.artifactDigest);
  const comparisons = {
    artifactDigest: Boolean(exactArtifact),
    composeChecksum: checksums.composeChecksum === intentComponent.expected.composeChecksum,
    configChecksum: checksums.configChecksum === intentComponent.expected.configChecksum,
    platform: image.platform === intentComponent.expected.platform,
    running: container.state === 'running',
    health: isHealthyStatus(healthStatus),
  };
  const complete = Object.values(comparisons).every(Boolean);
  return {
    componentKey: intentComponent.componentKey,
    componentInstanceId: intentComponent.componentInstanceId,
    sourceSequence: intentComponent.sourceSequence,
    capturedAt,
    artifactDigest: exactArtifact?.digest ?? image.repoDigests[0].digest,
    configuredImageChecksum: sha256(container.configuredImage),
    configChecksum: checksums.configChecksum,
    composeChecksum: checksums.composeChecksum,
    schemaVersion: intentComponent.expected.schemaVersion,
    capabilityChecksum: intentComponent.expected.capabilityChecksum,
    platform: image.platform,
    topologyChecksum: intentComponent.expected.topologyChecksum,
    health: complete ? 'HEALTHY' : 'UNHEALTHY',
    outcome: complete ? 'COMPLETE' : 'FAILED',
    comparisons,
    healthProbe: {preferredPath: profileComponent.alivePath, aliveStatus, healthzStatus, selectedPath: healthPath},
  };
}

export function signEvidence(evidenceCore, privateKeyPEM) {
  const canonical = canonicalJSONString(evidenceCore);
  if (Buffer.byteLength(canonical) > maximumEvidenceBytes) {
    throw new Error('canonical observation evidence exceeds the bounded size');
  }
  const privateKey = Buffer.isBuffer(privateKeyPEM) ? privateKeyPEM : Buffer.from(privateKeyPEM);
  const publicKey = createPublicKey(privateKey);
  const publicDER = publicKey.export({type: 'spki', format: 'der'});
  return {
    ...evidenceCore,
    evidenceChecksum: sha256(canonical),
    signature: {
      algorithm: 'Ed25519',
      keyFingerprint: sha256(publicDER),
      value: sign(null, Buffer.from(canonical), privateKey).toString('base64'),
    },
  };
}

export function buildObservationRequests(intent, signedEvidence) {
  return signedEvidence.components.map((component) => ({
    organizationId: intent.organizationId,
    observerId: intent.observerId,
    deploymentUnitId: intent.deploymentUnitId,
    componentInstanceId: component.componentInstanceId,
    componentKey: component.componentKey,
    sourceSequence: component.sourceSequence,
    capturedAt: component.capturedAt,
    evidenceChecksum: signedEvidence.evidenceChecksum,
    evidenceReference: `urn:sha256:${signedEvidence.evidenceChecksum.slice(7)}#${component.componentKey}`,
    artifactDigest: component.artifactDigest,
    configChecksum: component.configChecksum,
    schemaVersion: component.schemaVersion,
    capabilityChecksum: component.capabilityChecksum,
    platform: component.platform,
    topologyChecksum: component.topologyChecksum,
    health: component.health,
    outcome: component.outcome,
  }));
}

export async function submitObservations({profile, token, requests, fetchImpl = fetch}) {
  requireString(token, 'observer token', 512);
  if (token.length < 32 || /\s/.test(token)) {
    throw new Error('observer token must be a 32-512 character opaque token');
  }
  const endpoint = `${profile.distrBaseUrl}/api/observer/v1/observations`;
  const results = [];
  for (const request of requests) {
    const response = await fetchImpl(endpoint, {
      method: 'POST',
      headers: {
        Authorization: `Observer ${token}`,
        'Content-Type': 'application/json',
        Accept: 'application/json',
      },
      body: JSON.stringify(request),
      redirect: 'error',
      signal: AbortSignal.timeout(15_000),
    });
    const body = await response.arrayBuffer();
    if (body.byteLength > maximumCommandBytes) {
      throw new Error(`Distr observation response for ${request.componentKey} exceeds the bounded size`);
    }
    if (response.status !== 202) {
      throw new Error(`Distr observation submission for ${request.componentKey} returned HTTP ${response.status}`);
    }
    results.push({componentKey: request.componentKey, status: response.status});
  }
  return results;
}

async function readBoundedJSON(path, maximumBytes, label) {
  const bytes = await readFile(path);
  if (bytes.length === 0 || bytes.length > maximumBytes) {
    throw new Error(`${label} size is invalid`);
  }
  try {
    return JSON.parse(bytes.toString('utf8'));
  } catch {
    throw new Error(`${label} must contain valid JSON`);
  }
}

function parseArguments(argv) {
  const values = {};
  for (let index = 0; index < argv.length; index += 2) {
    const key = argv[index];
    const value = argv[index + 1];
    if (!key?.startsWith('--') || value === undefined) {
      throw new Error('arguments must be supplied as --name value pairs');
    }
    values[key.slice(2)] = value;
  }
  for (const required of [
    'profile',
    'intent',
    'ssh-key-file',
    'known-hosts-file',
    'observer-token-file',
    'evidence-private-key-file',
    'output',
  ]) {
    requireString(values[required], `--${required}`, 4096);
  }
  return values;
}

export async function runObserver({
  profile,
  intent,
  runSSH,
  token,
  privateKeyPEM,
  outputPath,
  now = new Date(),
  fetchImpl = fetch,
}) {
  validateProfile(profile);
  validateIntent(intent, profile, now);
  const capturedAt = now.toISOString();
  const components = [];
  for (const intentComponent of intent.components) {
    components.push(await observeComponent({profile, intentComponent, runSSH, capturedAt}));
  }
  const evidenceCore = {
    schemaVersion: 'emlo.choice-tp-observation-evidence/v1',
    intentId: intent.intentId,
    intentChecksum: intent.canonicalChecksum,
    targetProfileChecksum: profile.canonicalChecksum,
    capturedAt,
    observerCredentialSetId: intent.observerCredentialSetId,
    components,
  };
  const signedEvidence = signEvidence(evidenceCore, privateKeyPEM);
  const encoded = `${JSON.stringify(signedEvidence, null, 2)}\n`;
  if (Buffer.byteLength(encoded) > maximumEvidenceBytes) {
    throw new Error('signed observation evidence exceeds the bounded size');
  }
  if (outputPath) {
    await writeFile(outputPath, encoded, {encoding: 'utf8', mode: 0o600, flag: 'wx'});
    await chmod(outputPath, 0o600);
  }
  const requests = buildObservationRequests(intent, signedEvidence);
  const submissions = await submitObservations({profile, token, requests, fetchImpl});
  return {evidenceChecksum: signedEvidence.evidenceChecksum, submissions, signedEvidence};
}

async function main() {
  const args = parseArguments(process.argv.slice(2));
  const profile = await readBoundedJSON(args.profile, maximumIntentBytes, 'target profile');
  const intent = await readBoundedJSON(args.intent, maximumIntentBytes, 'observation intent');
  const token = (await readFile(args['observer-token-file'], 'utf8')).trim();
  const privateKeyPEM = await readFile(args['evidence-private-key-file']);
  const runSSH = createSSHRunner({
    profile: validateProfile(profile),
    sshKeyFile: args['ssh-key-file'],
    knownHostsFile: args['known-hosts-file'],
  });
  const result = await runObserver({
    profile,
    intent,
    runSSH,
    token,
    privateKeyPEM,
    outputPath: args.output,
  });
  process.stdout.write(
    `${JSON.stringify({evidenceChecksum: result.evidenceChecksum, submissions: result.submissions})}\n`
  );
}

const isMain =
  process.argv[1] &&
  fileURLToPath(import.meta.url) === fileURLToPath(new URL(`file:///${process.argv[1].replaceAll('\\', '/')}`));
if (isMain) {
  main().catch((error) => {
    process.stderr.write(`choice-tp observer failed: ${error.message}\n`);
    process.exitCode = 1;
  });
}
