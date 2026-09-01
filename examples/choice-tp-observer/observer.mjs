#!/usr/bin/env node

import {execFile} from 'node:child_process';
import {createHash, createPublicKey, sign, timingSafeEqual, verify} from 'node:crypto';
import {chmod, readFile, writeFile} from 'node:fs/promises';
import {platform as hostPlatform} from 'node:os';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import {promisify} from 'node:util';

const execFileAsync = promisify(execFile);
const digestPattern = /^sha256:[0-9a-f]{64}$/;
const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const safeRemoteValuePattern = /^[A-Za-z0-9_./:@%+=,-]+$/;
const maximumIntentBytes = 64 * 1024;
const maximumEvidenceBytes = 64 * 1024;
const maximumCommandBytes = 32 * 1024;
const maximumRuntimeProbeBytes = 4096;
const runtimeSchemaVersionPattern = /^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$/;
const safeProbeExecutablePattern = /^\/usr\/local\/libexec\/[a-z0-9][a-z0-9._-]{0,127}$/;
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
  }),
  'transaction-api': Object.freeze({
    container: 'transaction-api',
    composePath: '/home/emlo-admin/apps/remittance/dev/emlo-remittance-transaction/docker-compose.yaml',
    configPath: '/home/emlo-admin/apps/remittance/dev/emlo-remittance-transaction/appsettings.Production.json',
  }),
});
const fixedHealthPaths = Object.freeze({
  'customer-api': Object.freeze([
    Object.freeze({alivePath: '/customer-api/alive', healthzPath: '/customer-api/healthz'}),
    Object.freeze({
      alivePath: '/customer-api/swagger/v1/swagger.json',
      healthzPath: '/customer-api/swagger/v1/swagger.json',
    }),
  ]),
  'transaction-api': Object.freeze([
    Object.freeze({alivePath: '/transaction-api/alive', healthzPath: '/transaction-api/healthz'}),
    Object.freeze({
      alivePath: '/transaction-api/swagger/v1/swagger.json',
      healthzPath: '/transaction-api/swagger/v1/swagger.json',
    }),
  ]),
});
const standardHealthEvidence = Object.freeze({
  healthEvidenceKind: 'STANDARD_READINESS',
  healthEvidenceUse: 'STANDARD_PROMOTION_ELIGIBLE',
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

export function evidencePublicKeyIdentity(keyMaterial) {
  const publicKey = createPublicKey(keyMaterial);
  if (publicKey.asymmetricKeyType !== 'ed25519') {
    throw new Error('evidence signing key must be Ed25519');
  }
  const publicDER = publicKey.export({type: 'spki', format: 'der'});
  return {
    algorithm: 'Ed25519',
    keyFingerprint: sha256(publicDER),
    publicKeyPEM: publicKey.export({type: 'spki', format: 'pem'}).toString(),
  };
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

function validateRuntimeProbe(probe, label) {
  if (probe?.adapter === 'http-json/v1') {
    requireExactKeys(probe, ['adapter', 'path', 'timeoutMs'], label);
    requireString(probe.path, `${label} path`, 512);
    if (!/^\/[A-Za-z0-9._~:/@%+=,-]+$/.test(probe.path) || probe.path.includes('..') || probe.path.includes('//')) {
      throw new Error(`${label} path is not a safe local metadata path`);
    }
  } else if (probe?.adapter === 'command-json/v1') {
    requireExactKeys(probe, ['adapter', 'executable', 'arguments', 'timeoutMs'], label);
    if (!safeProbeExecutablePattern.test(probe.executable ?? '')) {
      throw new Error(`${label} must use a safe probe executable under /usr/local/libexec`);
    }
    if (!Array.isArray(probe.arguments) || probe.arguments.length > 16) {
      throw new Error(`${label} arguments are invalid`);
    }
    for (const argument of probe.arguments) {
      requireString(argument, `${label} argument`, 256);
      if (!safeRemoteValuePattern.test(argument)) {
        throw new Error(`${label} argument contains unsupported characters`);
      }
    }
  } else {
    throw new Error(`${label} adapter is unsupported`);
  }
  if (
    !Number.isSafeInteger(probe.timeoutMs) ||
    probe.timeoutMs < 1000 ||
    probe.timeoutMs > 10_000 ||
    probe.timeoutMs % 1000 !== 0
  ) {
    throw new Error(`${label} timeoutMs must be a whole second from 1000 through 10000`);
  }
  return probe;
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
  if (profile.schemaVersion !== 'emlo.choice-tp-observer-profile/v2') {
    throw new Error('target profile schema is unsupported');
  }
  requireExactKeys(profile.gateway, ['url', 'hostHeader'], 'target profile gateway');
  if (profile.gateway.url !== 'http://127.0.0.1:12000') {
    throw new Error('target profile gateway URL is not fixed');
  }
  if (profile.gateway.hostHeader !== 'api-gateway.dev.spi.emlotech.com') {
    throw new Error('target profile gateway host header is not fixed');
  }
  requireExactKeys(profile.components, Object.keys(fixedComponents), 'target profile components');
  for (const [componentKey, expected] of Object.entries(fixedComponents)) {
    const actual = profile.components[componentKey];
    requireExactKeys(
      actual,
      [...Object.keys(expected), 'alivePath', 'healthzPath', 'runtimeProbe'],
      `${componentKey} target profile`
    );
    for (const [key, value] of Object.entries(expected)) {
      if (actual[key] !== value) {
        throw new Error(`${componentKey} ${key} is not the fixed Choice TP DEV value`);
      }
      if (!safeRemoteValuePattern.test(actual[key])) {
        throw new Error(`${componentKey} ${key} is unsafe for a read-only SSH command`);
      }
    }
    const healthPathsMatch = fixedHealthPaths[componentKey].some(
      ({alivePath, healthzPath}) => actual.alivePath === alivePath && actual.healthzPath === healthzPath
    );
    if (!healthPathsMatch) {
      throw new Error(`${componentKey} health paths are not a sealed Choice TP DEV pair`);
    }
    for (const key of ['alivePath', 'healthzPath']) {
      if (!safeRemoteValuePattern.test(actual[key])) {
        throw new Error(`${componentKey} ${key} is unsafe for a read-only SSH command`);
      }
    }
    validateRuntimeProbe(actual.runtimeProbe, `${componentKey} runtime probe`);
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
  const allowlist = new Set(['/usr/bin/docker', '/usr/bin/sha256sum', '/usr/bin/curl', '/usr/bin/timeout']);
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
  const runtimeProbe = (() => {
    const probe = validateRuntimeProbe(component.runtimeProbe, `${componentKey} runtime probe`);
    const timeoutSeconds = String(probe.timeoutMs / 1000);
    if (probe.adapter === 'http-json/v1') {
      return remoteCommand('/usr/bin/curl', [
        '--silent',
        '--show-error',
        '--fail',
        '--request',
        'GET',
        '--proto',
        '=http',
        '--connect-timeout',
        '2',
        '--max-time',
        timeoutSeconds,
        '--max-filesize',
        String(maximumRuntimeProbeBytes),
        '--header',
        'Accept: application/json',
        '--header',
        `Host: ${profile.gateway.hostHeader}`,
        `${profile.gateway.url}${probe.path}`,
      ]);
    }
    return remoteCommand('/usr/bin/timeout', [
      '--signal=KILL',
      '--kill-after=1s',
      `${timeoutSeconds}s`,
      probe.executable,
      ...probe.arguments,
    ]);
  })();
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
    runtimeProbe,
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
  return async (kind, command, options = {}) => {
    const timeoutMs = options.timeoutMs ?? 20_000;
    if (!Number.isSafeInteger(timeoutMs) || timeoutMs < 1000 || timeoutMs > 20_000) {
      throw new Error('read-only SSH probe timeout is invalid');
    }
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
        timeout: timeoutMs,
        windowsHide: true,
      });
      return stdout;
    } catch (error) {
      throw new Error(`read-only SSH ${kind} probe failed (exit ${error.code ?? 'unknown'})`);
    }
  };
}

function parseContainerInspect(output) {
  const fields = output.trim().split('\t');
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
  const fields = output.trim().split('\t');
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
  for (const line of output.trim().split(/\r?\n/)) {
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
  const status = output.trim();
  if (!/^[0-9]{3}$/.test(status)) {
    throw new Error('health probe did not return one HTTP status code');
  }
  return Number.parseInt(status, 10);
}

function isHealthyStatus(status) {
  return status >= 200 && status < 300;
}

function normalizeHealthEvidence(healthEvidence = standardHealthEvidence) {
  requireExactKeys(healthEvidence, ['healthEvidenceKind', 'healthEvidenceUse'], 'health evidence classification');
  const valid =
    (healthEvidence.healthEvidenceKind === 'STANDARD_READINESS' &&
      healthEvidence.healthEvidenceUse === 'STANDARD_PROMOTION_ELIGIBLE') ||
    (healthEvidence.healthEvidenceKind === 'LEGACY_LIVENESS_ONLY' &&
      healthEvidence.healthEvidenceUse === 'BASELINE_OR_ROLLBACK_ONLY');
  if (!valid) {
    throw new Error('health evidence classification is unsupported');
  }
  return healthEvidence;
}

export function healthPolicyForComponent(componentKey, component, healthEvidence = standardHealthEvidence) {
  const classification = normalizeHealthEvidence(healthEvidence);
  return {
    schemaVersion: 'emlo.choice-tp-health-policy/v1',
    componentKey,
    preferredPath: component.alivePath,
    fallbackPath: component.healthzPath,
    acceptedStatusClass: '2xx',
    ...classification,
  };
}

export function parseRuntimeProbe(output, adapter) {
  if (
    typeof output !== 'string' ||
    Buffer.byteLength(output) === 0 ||
    Buffer.byteLength(output) > maximumRuntimeProbeBytes
  ) {
    throw new Error('runtime probe returned an invalid bounded record');
  }
  let measurement;
  try {
    measurement = JSON.parse(output);
  } catch {
    throw new Error('runtime probe returned invalid JSON');
  }
  requireExactKeys(measurement, ['schemaVersion', 'capabilityChecksum', 'topologyChecksum'], 'runtime probe');
  if (!runtimeSchemaVersionPattern.test(measurement.schemaVersion ?? '')) {
    throw new Error('runtime probe schemaVersion is invalid');
  }
  requireDigest(measurement.capabilityChecksum, 'runtime probe capabilityChecksum');
  requireDigest(measurement.topologyChecksum, 'runtime probe topologyChecksum');
  return {
    ...measurement,
    evidence: {
      adapter,
      evidenceChecksum: sha256(measurement),
    },
  };
}

export async function observeComponent({
  profile,
  intentComponent,
  runSSH,
  capturedAt,
  healthEvidence = standardHealthEvidence,
}) {
  const profileComponent = profile.components[intentComponent.componentKey];
  const commands = buildReadOnlyCommands(profile, intentComponent.componentKey);
  const container = parseContainerInspect(await runSSH('container-inspect', commands.containerInspect));
  const image = parseImageInspect(
    await runSSH('image-inspect', commands.imageInspect(container.imageId)),
    intentComponent.expected.platform
  );
  const checksums = parseChecksums(await runSSH('checksums', commands.checksum), profileComponent);
  const measuredRuntime = parseRuntimeProbe(
    await runSSH('runtime-probe', commands.runtimeProbe, {
      timeoutMs: Math.min(profileComponent.runtimeProbe.timeoutMs + 5000, 15_000),
    }),
    profileComponent.runtimeProbe.adapter
  );
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
    schemaVersion: measuredRuntime.schemaVersion === intentComponent.expected.schemaVersion,
    capabilityChecksum: measuredRuntime.capabilityChecksum === intentComponent.expected.capabilityChecksum,
    platform: image.platform === intentComponent.expected.platform,
    topologyChecksum: measuredRuntime.topologyChecksum === intentComponent.expected.topologyChecksum,
    running: container.state === 'running',
    health: isHealthyStatus(healthStatus),
  };
  const complete = Object.values(comparisons).every(Boolean);
  const classification = normalizeHealthEvidence(healthEvidence);
  const healthPolicy = healthPolicyForComponent(intentComponent.componentKey, profileComponent, classification);
  return {
    componentKey: intentComponent.componentKey,
    componentInstanceId: intentComponent.componentInstanceId,
    sourceSequence: intentComponent.sourceSequence,
    capturedAt,
    artifactDigest: exactArtifact?.digest ?? image.repoDigests[0].digest,
    configuredImageChecksum: sha256(container.configuredImage),
    configChecksum: checksums.configChecksum,
    composeChecksum: checksums.composeChecksum,
    schemaVersion: measuredRuntime.schemaVersion,
    capabilityChecksum: measuredRuntime.capabilityChecksum,
    platform: image.platform,
    topologyChecksum: measuredRuntime.topologyChecksum,
    health: complete ? 'HEALTHY' : 'UNHEALTHY',
    outcome: complete ? 'COMPLETE' : 'FAILED',
    comparisons,
    runtimeProbe: measuredRuntime.evidence,
    ...classification,
    healthPolicyChecksum: sha256(healthPolicy),
    healthProbe: {preferredPath: profileComponent.alivePath, aliveStatus, healthzStatus, selectedPath: healthPath},
  };
}

export function signEvidence(evidenceCore, privateKeyPEM) {
  const canonical = canonicalJSONString(evidenceCore);
  if (Buffer.byteLength(canonical) > maximumEvidenceBytes) {
    throw new Error('canonical observation evidence exceeds the bounded size');
  }
  const privateKey = Buffer.isBuffer(privateKeyPEM) ? privateKeyPEM : Buffer.from(privateKeyPEM);
  const publicIdentity = evidencePublicKeyIdentity(privateKey);
  return {
    ...evidenceCore,
    evidenceChecksum: sha256(canonical),
    signature: {
      algorithm: 'Ed25519',
      keyFingerprint: publicIdentity.keyFingerprint,
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
    evidenceReference: `evidence://sha256/${signedEvidence.evidenceChecksum.slice(7)}`,
    artifactDigest: component.artifactDigest,
    configChecksum: component.configChecksum,
    schemaVersion: component.schemaVersion,
    capabilityChecksum: component.capabilityChecksum,
    platform: component.platform,
    topologyChecksum: component.topologyChecksum,
    health: component.health,
    outcome: component.outcome,
    healthEvidenceKind: component.healthEvidenceKind,
    healthEvidenceUse: component.healthEvidenceUse,
    healthPolicyChecksum: component.healthPolicyChecksum,
  }));
}

export function verifyEvidenceEnvelopeSignature(signedEvidence, publicKeyMaterial) {
  requireDigest(signedEvidence.evidenceChecksum, 'signed observation evidence checksum');
  requireExactKeys(
    signedEvidence.signature,
    ['algorithm', 'keyFingerprint', 'value'],
    'signed observation evidence signature'
  );
  if (signedEvidence.signature.algorithm !== 'Ed25519') {
    throw new Error('signed observation evidence algorithm is unsupported');
  }
  requireDigest(signedEvidence.signature.keyFingerprint, 'signed observation evidence key fingerprint');
  const evidenceCore = withoutField(withoutField(signedEvidence, 'evidenceChecksum'), 'signature');
  const canonical = canonicalJSONString(evidenceCore);
  if (!constantTimeTextEqual(sha256(canonical), signedEvidence.evidenceChecksum)) {
    throw new Error('signed observation evidence checksum does not match');
  }
  const publicIdentity = evidencePublicKeyIdentity(publicKeyMaterial);
  if (!constantTimeTextEqual(publicIdentity.keyFingerprint, signedEvidence.signature.keyFingerprint)) {
    throw new Error('signed observation evidence key fingerprint does not match');
  }
  let signature;
  try {
    signature = Buffer.from(signedEvidence.signature.value, 'base64');
  } catch {
    throw new Error('signed observation evidence signature is invalid');
  }
  if (
    signature.length !== 64 ||
    !verify(null, Buffer.from(canonical), createPublicKey(publicIdentity.publicKeyPEM), signature)
  ) {
    throw new Error('signed observation evidence signature does not verify');
  }
  return signedEvidence;
}

export class ObservationSubmissionError extends Error {
  constructor(message, {retryable, status = null, componentKey = null} = {}) {
    super(message);
    this.name = 'ObservationSubmissionError';
    this.retryable = Boolean(retryable);
    this.status = status;
    this.componentKey = componentKey;
  }
}

async function consumeBoundedResponseBody(response) {
  const declaredLength = response.headers?.get?.('content-length');
  if (declaredLength !== null && declaredLength !== undefined) {
    if (!/^\d+$/.test(declaredLength) || Number(declaredLength) > maximumCommandBytes) {
      await response.body?.cancel?.();
      throw new Error('response body exceeds the bounded size');
    }
  }
  if (!response.body?.getReader) {
    const body = await response.arrayBuffer();
    if (body.byteLength > maximumCommandBytes) {
      throw new Error('response body exceeds the bounded size');
    }
    return;
  }
  const reader = response.body.getReader();
  let totalBytes = 0;
  try {
    while (true) {
      const {done, value} = await reader.read();
      if (done) return;
      totalBytes += value.byteLength;
      if (totalBytes > maximumCommandBytes) {
        await reader.cancel();
        throw new Error('response body exceeds the bounded size');
      }
    }
  } finally {
    reader.releaseLock();
  }
}

export async function submitObservations({profile, token, requests, fetchImpl = fetch}) {
  requireString(token, 'observer token', 512);
  if (token.length < 32 || /\s/.test(token)) {
    throw new Error('observer token must be a 32-512 character opaque token');
  }
  const endpoint = `${profile.distrBaseUrl}/api/observer/v1/observations`;
  const results = [];
  for (const request of requests) {
    let response;
    try {
      response = await fetchImpl(endpoint, {
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
    } catch {
      throw new ObservationSubmissionError(`Distr observation submission for ${request.componentKey} failed`, {
        retryable: true,
        componentKey: request.componentKey,
      });
    }
    try {
      await consumeBoundedResponseBody(response);
    } catch {
      throw new ObservationSubmissionError(
        `Distr observation response for ${request.componentKey} exceeds the bounded size`,
        {retryable: false, status: response.status, componentKey: request.componentKey}
      );
    }
    if (response.status !== 202) {
      throw new ObservationSubmissionError(
        `Distr observation submission for ${request.componentKey} returned HTTP ${response.status}`,
        {
          retryable: response.status === 408 || response.status === 429 || response.status >= 500,
          status: response.status,
          componentKey: request.componentKey,
        }
      );
    }
    results.push({componentKey: request.componentKey, status: response.status});
  }
  return results;
}

export async function readBoundedJSON(path, maximumBytes, label) {
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
  healthEvidence = standardHealthEvidence,
  now = new Date(),
  fetchImpl = fetch,
}) {
  const signedEvidence = await collectObservationEvidence({
    profile,
    intent,
    runSSH,
    privateKeyPEM,
    healthEvidence,
    now,
  });
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

export async function collectObservationEvidence({
  profile,
  intent,
  runSSH,
  privateKeyPEM,
  healthEvidence = standardHealthEvidence,
  now = new Date(),
}) {
  validateProfile(profile);
  validateIntent(intent, profile, now);
  const capturedAt = now.toISOString();
  const components = [];
  for (const intentComponent of intent.components) {
    components.push(await observeComponent({profile, intentComponent, runSSH, capturedAt, healthEvidence}));
  }
  const evidenceCore = {
    schemaVersion: 'emlo.choice-tp-observation-evidence/v2',
    intentId: intent.intentId,
    intentChecksum: intent.canonicalChecksum,
    targetProfileChecksum: profile.canonicalChecksum,
    capturedAt,
    observerCredentialSetId: intent.observerCredentialSetId,
    components,
  };
  return signEvidence(evidenceCore, privateKeyPEM);
}

export function verifySignedEvidence({signedEvidence, intent, profile, privateKeyPEM, healthEvidence = null}) {
  requireExactKeys(
    signedEvidence,
    [
      'schemaVersion',
      'intentId',
      'intentChecksum',
      'targetProfileChecksum',
      'capturedAt',
      'observerCredentialSetId',
      'components',
      'evidenceChecksum',
      'signature',
    ],
    'signed observation evidence'
  );
  if (signedEvidence.schemaVersion !== 'emlo.choice-tp-observation-evidence/v2') {
    throw new Error('signed observation evidence schema is unsupported');
  }
  const capturedAt = new Date(signedEvidence.capturedAt);
  if (!Number.isFinite(capturedAt.getTime())) {
    throw new Error('signed observation evidence capturedAt is invalid');
  }
  validateIntent(intent, validateProfile(profile), capturedAt);
  if (
    signedEvidence.intentId !== intent.intentId ||
    !constantTimeTextEqual(signedEvidence.intentChecksum, intent.canonicalChecksum) ||
    !constantTimeTextEqual(signedEvidence.targetProfileChecksum, profile.canonicalChecksum) ||
    signedEvidence.observerCredentialSetId !== intent.observerCredentialSetId
  ) {
    throw new Error('signed observation evidence scope does not match the immutable intent');
  }
  if (!Array.isArray(signedEvidence.components) || signedEvidence.components.length !== intent.components.length) {
    throw new Error('signed observation evidence component scope is invalid');
  }
  for (const intentComponent of intent.components) {
    const component = signedEvidence.components.find(
      (candidate) => candidate.componentKey === intentComponent.componentKey
    );
    if (
      !component ||
      component.componentInstanceId !== intentComponent.componentInstanceId ||
      component.sourceSequence !== intentComponent.sourceSequence ||
      component.capturedAt !== signedEvidence.capturedAt
    ) {
      throw new Error('signed observation evidence component identity is invalid');
    }
    requireDigest(component.healthPolicyChecksum, `${component.componentKey} healthPolicyChecksum`);
    normalizeHealthEvidence({
      healthEvidenceKind: component.healthEvidenceKind,
      healthEvidenceUse: component.healthEvidenceUse,
    });
    if (
      healthEvidence &&
      (component.healthEvidenceKind !== healthEvidence.healthEvidenceKind ||
        component.healthEvidenceUse !== healthEvidence.healthEvidenceUse)
    ) {
      throw new Error('signed observation evidence health classification does not match the service profile');
    }
  }
  return verifyEvidenceEnvelopeSignature(signedEvidence, privateKeyPEM);
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

const isMain = process.argv[1] && fileURLToPath(import.meta.url) === path.resolve(process.argv[1]);
if (isMain) {
  main().catch((error) => {
    process.stderr.write(`choice-tp observer failed: ${error.message}\n`);
    process.exitCode = 1;
  });
}
