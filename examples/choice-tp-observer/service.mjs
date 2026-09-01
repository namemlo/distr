#!/usr/bin/env node

import {createPrivateKey, randomUUID} from 'node:crypto';
import {chmod, mkdir, open, readdir, readFile, rename, stat, unlink, writeFile} from 'node:fs/promises';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import {
  buildObservationRequests,
  collectObservationEvidence,
  createSSHRunner,
  ObservationSubmissionError,
  readBoundedJSON,
  sha256,
  submitObservations,
  validateIntent,
  validateProfile,
  verifySignedEvidence,
} from './observer.mjs';

const maximumConfigBytes = 64 * 1024;
const maximumEvidenceBytes = 64 * 1024;
const maximumLegacyEvidenceBytes = 128 * 1024;
const maximumStateBytes = 256 * 1024;
const maximumIntentDirectoryEntries = 1024;
const maximumRetainedIntentStates = 512;
const digestPattern = /^sha256:[0-9a-f]{64}$/;
const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const fixedComponents = ['customer-api', 'transaction-api'];
const stateLabels = Object.freeze({'customer-api': 'C1', 'transaction-api': 'T1'});
const legacyLabels = Object.freeze({'customer-api': 'C0', 'transaction-api': 'T0'});

function requireExactKeys(value, expected, label) {
  const actual = Object.keys(value ?? {}).sort();
  const wanted = [...expected].sort();
  if (actual.length !== wanted.length || actual.some((key, index) => key !== wanted[index])) {
    throw new Error(`${label} contains unsupported or missing fields`);
  }
}

function requireString(value, label, maximum = 4096) {
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

function requireInteger(value, minimum, maximum, label) {
  if (!Number.isSafeInteger(value) || value < minimum || value > maximum) {
    throw new Error(`${label} must be an integer from ${minimum} through ${maximum}`);
  }
}

function requireAbsolutePath(value, label) {
  requireString(value, label);
  if (!path.isAbsolute(value)) {
    throw new Error(`${label} must be absolute`);
  }
}

function withoutField(value, field) {
  return Object.fromEntries(Object.entries(value).filter(([key]) => key !== field));
}

export function validateServiceConfig(config) {
  requireExactKeys(
    config,
    [
      'schemaVersion',
      'profileFile',
      'inboxDirectory',
      'evidenceDirectory',
      'stateFile',
      'lockFile',
      'credentials',
      'scope',
      'legacyBaseline',
      'currentRuntime',
      'polling',
      'retry',
      'canonicalChecksum',
    ],
    'observer service config'
  );
  if (config.schemaVersion !== 'emlo.choice-tp-observer-service/v1') {
    throw new Error('observer service config schema is unsupported');
  }
  for (const key of ['profileFile', 'inboxDirectory', 'evidenceDirectory', 'stateFile', 'lockFile']) {
    requireAbsolutePath(config[key], key);
  }
  if (new Set([config.inboxDirectory, config.evidenceDirectory, path.dirname(config.stateFile)]).size !== 3) {
    throw new Error('intent, evidence, and state directories must be distinct');
  }
  requireExactKeys(
    config.credentials,
    [
      'sshKeyFile',
      'knownHostsFile',
      'observerTokenFile',
      'observerTokenFingerprint',
      'evidencePrivateKeyFile',
      'executorCredentialFingerprints',
    ],
    'observer service credentials'
  );
  for (const key of ['sshKeyFile', 'knownHostsFile', 'observerTokenFile', 'evidencePrivateKeyFile']) {
    requireAbsolutePath(config.credentials[key], `credentials.${key}`);
  }
  const observerCredentialPaths = [
    config.credentials.sshKeyFile,
    config.credentials.observerTokenFile,
    config.credentials.evidencePrivateKeyFile,
  ];
  if (new Set(observerCredentialPaths).size !== observerCredentialPaths.length) {
    throw new Error('SSH, observer-token, and evidence-signing credential files must be distinct');
  }
  requireDigest(config.credentials.observerTokenFingerprint, 'credentials.observerTokenFingerprint');
  if (
    !Array.isArray(config.credentials.executorCredentialFingerprints) ||
    config.credentials.executorCredentialFingerprints.length === 0 ||
    config.credentials.executorCredentialFingerprints.length > 16
  ) {
    throw new Error('credentials.executorCredentialFingerprints must pin one through sixteen executor secrets');
  }
  for (const fingerprint of config.credentials.executorCredentialFingerprints) {
    requireDigest(fingerprint, 'executor credential fingerprint');
  }
  if (
    new Set(config.credentials.executorCredentialFingerprints).size !==
    config.credentials.executorCredentialFingerprints.length
  ) {
    throw new Error('executor credential fingerprints must be unique');
  }

  requireExactKeys(
    config.scope,
    [
      'organizationId',
      'observerId',
      'deploymentUnitId',
      'componentInstanceIds',
      'observerCredentialSetId',
      'executorCredentialSetId',
    ],
    'observer service scope'
  );
  requireUUID(config.scope.organizationId, 'scope.organizationId');
  requireUUID(config.scope.observerId, 'scope.observerId');
  requireUUID(config.scope.deploymentUnitId, 'scope.deploymentUnitId');
  requireExactKeys(config.scope.componentInstanceIds, fixedComponents, 'scope.componentInstanceIds');
  for (const componentKey of fixedComponents) {
    requireUUID(config.scope.componentInstanceIds[componentKey], `${componentKey} component instance ID`);
  }
  requireString(config.scope.observerCredentialSetId, 'scope.observerCredentialSetId', 128);
  requireString(config.scope.executorCredentialSetId, 'scope.executorCredentialSetId', 128);
  if (config.scope.observerCredentialSetId === config.scope.executorCredentialSetId) {
    throw new Error('observer and Jenkins/executor credential-set IDs must be distinct');
  }

  requireExactKeys(
    config.legacyBaseline,
    ['checkpoint', 'evidenceFile', 'evidenceFileChecksum', 'components'],
    'legacy baseline pin'
  );
  if (config.legacyBaseline.checkpoint !== 'C0/T0') {
    throw new Error('legacy baseline checkpoint must be C0/T0');
  }
  requireAbsolutePath(config.legacyBaseline.evidenceFile, 'legacyBaseline.evidenceFile');
  requireDigest(config.legacyBaseline.evidenceFileChecksum, 'legacyBaseline.evidenceFileChecksum');
  requireExactKeys(config.legacyBaseline.components, fixedComponents, 'legacy baseline components');
  for (const componentKey of fixedComponents) {
    const component = config.legacyBaseline.components[componentKey];
    requireExactKeys(component, ['stateLabel', 'artifactDigest', 'configChecksum'], `${componentKey} legacy pin`);
    if (component.stateLabel !== legacyLabels[componentKey]) {
      throw new Error(`${componentKey} legacy state label must be ${legacyLabels[componentKey]}`);
    }
    requireDigest(component.artifactDigest, `${componentKey} legacy artifactDigest`);
    requireDigest(component.configChecksum, `${componentKey} legacy configChecksum`);
  }

  requireExactKeys(config.currentRuntime, ['checkpoint', 'componentStateLabels'], 'current runtime');
  if (config.currentRuntime.checkpoint !== 'C1/T1') {
    throw new Error('current runtime checkpoint must be C1/T1');
  }
  requireExactKeys(config.currentRuntime.componentStateLabels, fixedComponents, 'current runtime labels');
  for (const componentKey of fixedComponents) {
    if (config.currentRuntime.componentStateLabels[componentKey] !== stateLabels[componentKey]) {
      throw new Error(`${componentKey} current state label must be ${stateLabels[componentKey]}`);
    }
  }

  requireExactKeys(config.polling, ['intervalMs', 'maxIntentsPerPoll', 'lockStaleMs'], 'polling config');
  requireInteger(config.polling.intervalMs, 1000, 300_000, 'polling.intervalMs');
  requireInteger(config.polling.maxIntentsPerPoll, 1, 32, 'polling.maxIntentsPerPoll');
  requireInteger(config.polling.lockStaleMs, 300_000, 3_600_000, 'polling.lockStaleMs');
  if (config.polling.lockStaleMs < config.polling.intervalMs * 3) {
    throw new Error('polling.lockStaleMs must be at least three polling intervals');
  }
  requireExactKeys(
    config.retry,
    ['maxAttemptsPerPoll', 'maxTotalAttemptsPerIntent', 'initialDelayMs', 'maxDelayMs'],
    'retry config'
  );
  requireInteger(config.retry.maxAttemptsPerPoll, 1, 5, 'retry.maxAttemptsPerPoll');
  requireInteger(config.retry.maxTotalAttemptsPerIntent, 1, 100, 'retry.maxTotalAttemptsPerIntent');
  requireInteger(config.retry.initialDelayMs, 100, 30_000, 'retry.initialDelayMs');
  requireInteger(config.retry.maxDelayMs, config.retry.initialDelayMs, 60_000, 'retry.maxDelayMs');

  requireDigest(config.canonicalChecksum, 'observer service config canonicalChecksum');
  if (sha256(withoutField(config, 'canonicalChecksum')) !== config.canonicalChecksum) {
    throw new Error('observer service config canonical checksum does not match');
  }
  return config;
}

async function readBoundedFile(filePath, maximumBytes, label) {
  const bytes = await readFile(filePath);
  if (bytes.length === 0 || bytes.length > maximumBytes) {
    throw new Error(`${label} size is invalid`);
  }
  return bytes;
}

async function requirePrivateFileMode(filePath, label) {
  if (process.platform === 'win32') {
    return;
  }
  const metadata = await stat(filePath);
  if ((metadata.mode & 0o077) !== 0) {
    throw new Error(`${label} must not grant group or other permissions`);
  }
}

export function validateKnownHosts(contents, profile) {
  const expectedHost = profile.port === 22 ? profile.host : `[${profile.host}]:${profile.port}`;
  const entries = contents
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter((line) => line && !line.startsWith('#'));
  if (entries.length === 0 || entries.length > 16) {
    throw new Error('known_hosts must contain one through sixteen pinned entries');
  }
  let matched = false;
  for (const entry of entries) {
    const fields = entry.split(/\s+/);
    if (fields.length < 3 || fields[0].startsWith('@')) {
      throw new Error('known_hosts contains an unsupported entry');
    }
    const hosts = fields[0].split(',');
    if (hosts.some((host) => /[*?!]|^\|/.test(host))) {
      throw new Error('known_hosts must use an explicit unhashed target host pin');
    }
    if (hosts.includes(expectedHost)) {
      matched = true;
    }
  }
  if (!matched) {
    throw new Error('known_hosts does not pin the fixed Choice TP target');
  }
}

export function validateLegacyEvidence(legacyEvidence, config) {
  if (
    legacyEvidence?.schema !== 'emlo.choice-tp-baseline-runtime-evidence/v1' ||
    legacyEvidence.capturedReadOnly !== true ||
    legacyEvidence.target !== 'choice-tp-dev' ||
    !Array.isArray(legacyEvidence.components) ||
    legacyEvidence.components.length !== fixedComponents.length
  ) {
    throw new Error('legacy C0/T0 evidence shape is invalid');
  }
  for (const componentKey of fixedComponents) {
    const component = legacyEvidence.components.find((candidate) => candidate.componentKey === componentKey);
    const pin = config.legacyBaseline.components[componentKey];
    if (
      !component ||
      component.stateLabel !== pin.stateLabel ||
      component.repositoryDigest !== pin.artifactDigest ||
      component.appSettingsChecksum !== pin.configChecksum ||
      component.runtimeEvidence?.classification !== 'LEGACY_LIVENESS_ONLY' ||
      component.runtimeEvidence?.allowedUse !== 'BASELINE_OR_ROLLBACK_ONLY' ||
      component.runtimeEvidence?.standardReadinessClaimed !== false ||
      component.runtimeEvidence?.healthyPromotionClaimed !== false
    ) {
      throw new Error(`${componentKey} legacy C0/T0 evidence does not match its immutable pin`);
    }
  }
  return legacyEvidence;
}

export async function validateCredentialFiles(config) {
  const sshKey = await readBoundedFile(config.credentials.sshKeyFile, 32 * 1024, 'observer SSH key');
  const tokenBytes = await readBoundedFile(config.credentials.observerTokenFile, 1024, 'scoped observer token');
  const evidenceKey = await readBoundedFile(
    config.credentials.evidencePrivateKeyFile,
    32 * 1024,
    'evidence signing key'
  );
  await requirePrivateFileMode(config.credentials.sshKeyFile, 'observer SSH key');
  await requirePrivateFileMode(config.credentials.observerTokenFile, 'scoped observer token');
  await requirePrivateFileMode(config.credentials.evidencePrivateKeyFile, 'evidence signing key');
  const token = tokenBytes.toString('utf8').trim();
  if (token.length < 32 || token.length > 512 || /\s/.test(token)) {
    throw new Error('scoped observer token must be a 32-512 character opaque token');
  }
  if (sha256(token) !== config.credentials.observerTokenFingerprint) {
    throw new Error('scoped observer token fingerprint does not match');
  }
  if (!/^-----BEGIN (?:OPENSSH |EC |RSA )?PRIVATE KEY-----/.test(sshKey.toString('utf8'))) {
    throw new Error('observer SSH key is not a supported private-key file');
  }
  const parsedEvidenceKey = createPrivateKey(evidenceKey);
  if (parsedEvidenceKey.asymmetricKeyType !== 'ed25519') {
    throw new Error('evidence signing key must be Ed25519');
  }
  const observerFingerprints = [sha256(sshKey), sha256(token), sha256(evidenceKey)];
  if (new Set(observerFingerprints).size !== observerFingerprints.length) {
    throw new Error('observer SSH, token, and evidence-signing credentials must be independently generated');
  }
  const forbidden = new Set(config.credentials.executorCredentialFingerprints);
  if (observerFingerprints.some((fingerprint) => forbidden.has(fingerprint))) {
    throw new Error('observer credential matches a pinned Jenkins/executor credential fingerprint');
  }
  return {token, privateKeyPEM: evidenceKey};
}

function validateIntentScope(intent, config) {
  if (
    intent.organizationId !== config.scope.organizationId ||
    intent.observerId !== config.scope.observerId ||
    intent.deploymentUnitId !== config.scope.deploymentUnitId ||
    intent.observerCredentialSetId !== config.scope.observerCredentialSetId ||
    intent.executorCredentialSetId !== config.scope.executorCredentialSetId
  ) {
    throw new Error('observation intent is outside the configured observer-token scope');
  }
  for (const componentKey of fixedComponents) {
    const component = intent.components.find((candidate) => candidate.componentKey === componentKey);
    const legacy = config.legacyBaseline.components[componentKey];
    if (!component || component.componentInstanceId !== config.scope.componentInstanceIds[componentKey]) {
      throw new Error(`${componentKey} observation intent is outside the configured component scope`);
    }
    if (component.expected.artifactDigest === legacy.artifactDigest) {
      throw new Error(
        `${componentKey} ${stateLabels[componentKey]} runtime must not reuse the ${legacy.stateLabel} artifact digest`
      );
    }
  }
}

async function writeJSONExclusive(filePath, value) {
  const encoded = `${JSON.stringify(value, null, 2)}\n`;
  if (Buffer.byteLength(encoded) > maximumEvidenceBytes) {
    throw new Error('signed observation evidence exceeds the bounded size');
  }
  await mkdir(path.dirname(filePath), {recursive: true, mode: 0o700});
  await writeFile(filePath, encoded, {encoding: 'utf8', mode: 0o600, flag: 'wx'});
  await chmod(filePath, 0o600);
}

async function writeJSONAtomic(filePath, value) {
  const encoded = `${JSON.stringify(value, null, 2)}\n`;
  if (Buffer.byteLength(encoded) > maximumStateBytes) {
    throw new Error('observer service state exceeds the bounded size');
  }
  await mkdir(path.dirname(filePath), {recursive: true, mode: 0o700});
  const temporaryPath = `${filePath}.${process.pid}.${randomUUID()}.tmp`;
  await writeFile(temporaryPath, encoded, {encoding: 'utf8', mode: 0o600, flag: 'wx'});
  await chmod(temporaryPath, 0o600);
  try {
    await rename(temporaryPath, filePath);
  } catch (error) {
    if (process.platform !== 'win32' || !['EEXIST', 'EPERM'].includes(error.code)) {
      throw error;
    }
    await unlink(filePath).catch((unlinkError) => {
      if (unlinkError.code !== 'ENOENT') throw unlinkError;
    });
    await rename(temporaryPath, filePath);
  }
}

function emptyState(config) {
  return {
    schemaVersion: 'emlo.choice-tp-observer-service-state/v1',
    serviceConfigChecksum: config.canonicalChecksum,
    intents: {},
  };
}

function reserveIntentStateSlot(state, stateKey) {
  if (Object.hasOwn(state.intents, stateKey) || Object.keys(state.intents).length < maximumRetainedIntentStates) {
    return;
  }
  const removable = Object.entries(state.intents)
    .filter(([, intentState]) => ['COMPLETE', 'EXHAUSTED'].includes(intentState?.status))
    .sort(([leftKey, left], [rightKey, right]) => {
      const updatedOrder = String(left?.updatedAt ?? '').localeCompare(String(right?.updatedAt ?? ''));
      return updatedOrder || leftKey.localeCompare(rightKey);
    });
  if (removable.length === 0) {
    throw new Error('observer service state capacity is exhausted by pending intents');
  }
  delete state.intents[removable[0][0]];
}

export async function loadServiceState(config) {
  let state;
  try {
    state = await readBoundedJSON(config.stateFile, maximumStateBytes, 'observer service state');
  } catch (error) {
    if (error.code === 'ENOENT') {
      return emptyState(config);
    }
    throw error;
  }
  requireExactKeys(state, ['schemaVersion', 'serviceConfigChecksum', 'intents'], 'observer service state');
  if (
    state.schemaVersion !== 'emlo.choice-tp-observer-service-state/v1' ||
    state.serviceConfigChecksum !== config.canonicalChecksum ||
    !state.intents ||
    Array.isArray(state.intents) ||
    typeof state.intents !== 'object' ||
    Object.keys(state.intents).length > maximumRetainedIntentStates
  ) {
    throw new Error('observer service state is invalid or belongs to another configuration');
  }
  return state;
}

function evidencePath(config, intent) {
  return path.join(config.evidenceDirectory, `${intent.canonicalChecksum.slice(7)}.evidence.json`);
}

async function readExistingEvidence(filePath) {
  return readBoundedJSON(filePath, maximumEvidenceBytes, 'signed observation evidence');
}

export async function submitWithRetry({profile, token, requests, retry, fetchImpl, sleep}) {
  let delayMs = retry.initialDelayMs;
  for (let attempt = 1; attempt <= retry.maxAttemptsPerPoll; attempt += 1) {
    try {
      return await submitObservations({profile, token, requests, fetchImpl});
    } catch (error) {
      const retryable = error instanceof ObservationSubmissionError && error.retryable;
      if (!retryable || attempt === retry.maxAttemptsPerPoll) {
        throw error;
      }
      await sleep(delayMs);
      delayMs = Math.min(delayMs * 2, retry.maxDelayMs);
    }
  }
  throw new Error('observation submission retry bound was exhausted');
}

function failureCode(error, evidencePersisted) {
  if (error instanceof ObservationSubmissionError) {
    return error.retryable ? 'SUBMISSION_RETRYABLE' : 'SUBMISSION_REJECTED';
  }
  return evidencePersisted ? 'EVIDENCE_REPLAY_FAILED' : 'COLLECTION_FAILED';
}

export async function processIntent({
  config,
  profile,
  state,
  intentPath,
  token,
  privateKeyPEM,
  runSSH,
  now,
  fetchImpl,
  sleep,
}) {
  const intent = await readBoundedJSON(intentPath, maximumConfigBytes, 'observation intent');
  const hasCanonicalChecksum = digestPattern.test(intent.canonicalChecksum ?? '');
  const stateKey = hasCanonicalChecksum ? intent.canonicalChecksum : sha256(intent);
  const retained = state.intents[stateKey];
  if (retained?.status === 'COMPLETE' || retained?.status === 'EXHAUSTED') {
    return {intentId: intent.intentId, status: retained.status, skipped: true};
  }
  reserveIntentStateSlot(state, stateKey);
  const attempts = (retained?.attempts ?? 0) + 1;
  if (attempts > config.retry.maxTotalAttemptsPerIntent) {
    state.intents[stateKey] = {
      intentId: intent.intentId,
      status: 'EXHAUSTED',
      attempts: retained?.attempts ?? config.retry.maxTotalAttemptsPerIntent,
      updatedAt: now.toISOString(),
      failureCode: retained?.failureCode ?? 'ATTEMPT_LIMIT_REACHED',
      evidenceFile: retained?.evidenceFile ?? null,
      evidenceChecksum: retained?.evidenceChecksum ?? null,
    };
    await writeJSONAtomic(config.stateFile, state);
    return {intentId: intent.intentId, status: 'EXHAUSTED', skipped: true};
  }

  const persistedPath = hasCanonicalChecksum ? evidencePath(config, intent) : null;
  let signedEvidence;
  let evidencePersisted = false;
  try {
    requireDigest(intent.canonicalChecksum, 'observation intent canonicalChecksum');
    try {
      signedEvidence = await readExistingEvidence(persistedPath);
      verifySignedEvidence({signedEvidence, intent, profile, privateKeyPEM});
      validateIntentScope(intent, config);
      evidencePersisted = true;
    } catch (error) {
      if (error.code !== 'ENOENT') {
        throw error;
      }
      validateIntent(intent, profile, now);
      validateIntentScope(intent, config);
      signedEvidence = await collectObservationEvidence({
        profile,
        intent,
        runSSH,
        privateKeyPEM,
        now,
      });
      await writeJSONExclusive(persistedPath, signedEvidence);
      evidencePersisted = true;
    }
    const submissions = await submitWithRetry({
      profile,
      token,
      requests: buildObservationRequests(intent, signedEvidence),
      retry: config.retry,
      fetchImpl,
      sleep,
    });
    state.intents[stateKey] = {
      intentId: intent.intentId,
      status: 'COMPLETE',
      attempts,
      updatedAt: now.toISOString(),
      failureCode: null,
      evidenceFile: path.basename(persistedPath),
      evidenceChecksum: signedEvidence.evidenceChecksum,
    };
    await writeJSONAtomic(config.stateFile, state);
    return {intentId: intent.intentId, status: 'COMPLETE', submissions};
  } catch (error) {
    const exhausted = attempts >= config.retry.maxTotalAttemptsPerIntent;
    state.intents[stateKey] = {
      intentId: intent.intentId ?? 'invalid-intent',
      status: exhausted ? 'EXHAUSTED' : 'PENDING',
      attempts,
      updatedAt: now.toISOString(),
      failureCode: failureCode(error, evidencePersisted),
      evidenceFile: evidencePersisted && persistedPath ? path.basename(persistedPath) : null,
      evidenceChecksum: signedEvidence?.evidenceChecksum ?? null,
    };
    await writeJSONAtomic(config.stateFile, state);
    throw error;
  }
}

async function listIntentFiles(config) {
  await mkdir(config.inboxDirectory, {recursive: true, mode: 0o700});
  const entries = await readdir(config.inboxDirectory, {withFileTypes: true});
  if (entries.length > maximumIntentDirectoryEntries) {
    throw new Error('observation intent inbox exceeds the bounded entry count');
  }
  return entries
    .filter((entry) => entry.isFile() && entry.name.endsWith('.json') && !entry.name.startsWith('.'))
    .map((entry) => path.join(config.inboxDirectory, entry.name))
    .sort()
    .slice(0, config.polling.maxIntentsPerPoll);
}

export async function pollOnce(context, dependencies = {}) {
  const now = dependencies.now?.() ?? new Date();
  const fetchImpl = dependencies.fetchImpl ?? fetch;
  const sleep = dependencies.sleep ?? ((delayMs) => new Promise((resolve) => setTimeout(resolve, delayMs)));
  const runSSH = dependencies.runSSH ?? context.runSSH;
  const state = await loadServiceState(context.config);
  const intentFiles = await listIntentFiles(context.config);
  const results = [];
  for (const intentPath of intentFiles) {
    try {
      results.push(
        await processIntent({
          ...context,
          state,
          intentPath,
          runSSH,
          now,
          fetchImpl,
          sleep,
        })
      );
    } catch (error) {
      results.push({
        intentFile: path.basename(intentPath),
        status: 'FAILED',
        errorClass: error instanceof ObservationSubmissionError ? error.name : 'ObserverServiceError',
      });
    }
  }
  return results;
}

export async function initializeService(configPath) {
  const config = validateServiceConfig(
    await readBoundedJSON(configPath, maximumConfigBytes, 'observer service config')
  );
  const profile = validateProfile(await readBoundedJSON(config.profileFile, maximumConfigBytes, 'target profile'));
  const knownHosts = (await readBoundedFile(config.credentials.knownHostsFile, 32 * 1024, 'known_hosts')).toString(
    'utf8'
  );
  validateKnownHosts(knownHosts, profile);
  const legacyBytes = await readBoundedFile(
    config.legacyBaseline.evidenceFile,
    maximumLegacyEvidenceBytes,
    'legacy C0/T0 evidence'
  );
  if (sha256(legacyBytes) !== config.legacyBaseline.evidenceFileChecksum) {
    throw new Error('legacy C0/T0 evidence file checksum does not match its immutable pin');
  }
  let legacyEvidence;
  try {
    legacyEvidence = JSON.parse(legacyBytes.toString('utf8'));
  } catch {
    throw new Error('legacy C0/T0 evidence must contain valid JSON');
  }
  validateLegacyEvidence(legacyEvidence, config);
  const {token, privateKeyPEM} = await validateCredentialFiles(config);
  await mkdir(config.evidenceDirectory, {recursive: true, mode: 0o700});
  await mkdir(path.dirname(config.stateFile), {recursive: true, mode: 0o700});
  return {
    config,
    profile,
    token,
    privateKeyPEM,
    runSSH: createSSHRunner({
      profile,
      sshKeyFile: config.credentials.sshKeyFile,
      knownHostsFile: config.credentials.knownHostsFile,
    }),
  };
}

export async function acquireServiceLock(config, now = new Date()) {
  await mkdir(path.dirname(config.lockFile), {recursive: true, mode: 0o700});
  const create = async () => {
    const handle = await open(config.lockFile, 'wx', 0o600);
    await handle.writeFile(
      `${JSON.stringify({pid: process.pid, updatedAt: now.toISOString(), configChecksum: config.canonicalChecksum})}\n`,
      'utf8'
    );
    await handle.close();
    await chmod(config.lockFile, 0o600);
  };
  try {
    await create();
  } catch (error) {
    if (error.code !== 'EEXIST') throw error;
    const metadata = await stat(config.lockFile);
    if (now.getTime() - metadata.mtimeMs < config.polling.lockStaleMs) {
      throw new Error('another observer service instance holds the durable lock');
    }
    await unlink(config.lockFile);
    await create();
  }
  return {
    refresh: async (instant = new Date()) => {
      await writeFile(
        config.lockFile,
        `${JSON.stringify({pid: process.pid, updatedAt: instant.toISOString(), configChecksum: config.canonicalChecksum})}\n`,
        {encoding: 'utf8', mode: 0o600}
      );
    },
    release: async () => {
      await unlink(config.lockFile).catch((error) => {
        if (error.code !== 'ENOENT') throw error;
      });
    },
  };
}

export async function runService({context, once = false, signal, dependencies = {}}) {
  const lock = await acquireServiceLock(context.config, dependencies.now?.() ?? new Date());
  const sleep = dependencies.sleep ?? ((delayMs) => new Promise((resolve) => setTimeout(resolve, delayMs)));
  try {
    do {
      await lock.refresh(dependencies.now?.() ?? new Date());
      const results = await pollOnce(context, dependencies);
      process.stdout.write(`${JSON.stringify({event: 'poll-complete', results})}\n`);
      if (once || signal?.aborted) break;
      await sleep(context.config.polling.intervalMs);
    } while (!signal?.aborted);
  } finally {
    await lock.release();
  }
}

function parseArguments(argv) {
  const result = {once: false, check: false, config: null};
  for (let index = 0; index < argv.length; index += 1) {
    const argument = argv[index];
    if (argument === '--once') {
      result.once = true;
    } else if (argument === '--check') {
      result.check = true;
    } else if (argument === '--config') {
      result.config = argv[index + 1];
      index += 1;
    } else {
      throw new Error('usage: service.mjs --config <absolute-path> [--once|--check]');
    }
  }
  requireAbsolutePath(result.config, '--config');
  if (result.once && result.check) {
    throw new Error('--once and --check are mutually exclusive');
  }
  return result;
}

async function main() {
  const args = parseArguments(process.argv.slice(2));
  const context = await initializeService(args.config);
  if (args.check) {
    process.stdout.write(`${JSON.stringify({status: 'valid', configChecksum: context.config.canonicalChecksum})}\n`);
    return;
  }
  const controller = new AbortController();
  process.once('SIGINT', () => controller.abort());
  process.once('SIGTERM', () => controller.abort());
  await runService({context, once: args.once, signal: controller.signal});
}

const isMain = process.argv[1] && fileURLToPath(import.meta.url) === path.resolve(process.argv[1]);
if (isMain) {
  main().catch((error) => {
    process.stderr.write(`choice-tp observer service failed: ${error.message}\n`);
    process.exitCode = 1;
  });
}
