#!/usr/bin/env node

import {createPrivateKey} from 'node:crypto';
import {chmod, lstat, mkdir, open, readdir, readFile, unlink} from 'node:fs/promises';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import {evidencePublicKeyIdentity, sha256, verifyEvidenceEnvelopeSignature} from './observer.mjs';
import {validateServiceConfig} from './service.mjs';

const maximumCredentialBytes = 32 * 1024;
const maximumJSONBytes = 256 * 1024;
const maximumEvidenceFiles = 1024;
const maximumKeyManifests = 32;
const digestPattern = /^sha256:[0-9a-f]{64}$/;
const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const keyIdPattern = /^[a-z0-9][a-z0-9._-]{0,127}$/;
const registrationCredentialPlaceholder = '${DISTR_OBSERVER_TOKEN}';
const requiredMeasurements = Object.freeze([
  'artifactDigest',
  'configChecksum',
  'schemaVersion',
  'capabilityChecksum',
  'platform',
  'topologyChecksum',
  'health',
]);

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

function requireTimestamp(value, label) {
  requireString(value, label, 64);
  const parsed = new Date(value);
  if (!Number.isFinite(parsed.getTime()) || parsed.toISOString() !== value) {
    throw new Error(`${label} must be a canonical UTC timestamp`);
  }
  return parsed;
}

function withoutField(value, field) {
  return Object.fromEntries(Object.entries(value).filter(([key]) => key !== field));
}

async function readBoundedFile(filePath, maximumBytes, label, {privateFile = false} = {}) {
  const metadata = await lstat(filePath);
  if (!metadata.isFile() || metadata.isSymbolicLink() || metadata.size > maximumBytes) {
    throw new Error(`${label} must be a bounded non-symlink file`);
  }
  if (privateFile && process.platform !== 'win32' && (metadata.mode & 0o077) !== 0) {
    throw new Error(`${label} must not grant group or other permissions`);
  }
  return readFile(filePath);
}

async function readBoundedJSON(filePath, label) {
  const bytes = await readBoundedFile(filePath, maximumJSONBytes, label);
  try {
    return JSON.parse(bytes.toString('utf8'));
  } catch {
    throw new Error(`${label} must contain valid JSON`);
  }
}

async function writeExclusiveFiles(files) {
  const paths = files.map(({filePath}) => path.resolve(filePath));
  if (new Set(paths).size !== paths.length) {
    throw new Error('provisioning output paths must be distinct');
  }
  const opened = [];
  try {
    for (const file of files) {
      await mkdir(path.dirname(file.filePath), {recursive: true, mode: 0o700});
      const handle = await open(file.filePath, 'wx', file.mode);
      opened.push({...file, handle});
    }
    for (const file of opened) {
      await file.handle.writeFile(file.contents, 'utf8');
      await file.handle.close();
      file.handle = null;
      await chmod(file.filePath, file.mode);
    }
  } catch (error) {
    await Promise.all(
      opened.map(async (file) => {
        await file.handle?.close().catch(() => {});
        await unlink(file.filePath).catch(() => {});
      })
    );
    throw error;
  }
}

async function writeJSONExclusive(filePath, value, mode = 0o600) {
  await writeExclusiveFiles([{filePath, contents: `${JSON.stringify(value, null, 2)}\n`, mode}]);
}

function normalizeObserverToken(value) {
  const token = Buffer.isBuffer(value) ? value.toString('utf8').trim() : String(value).trim();
  if (token.length < 32 || token.length > 512 || /\s/.test(token)) {
    throw new Error('scoped observer token must be a 32-512 character opaque token');
  }
  return token;
}

export function fingerprintCredential(kind, value) {
  if (kind === 'normalized-token') {
    return sha256(normalizeObserverToken(value));
  }
  if (kind === 'file-bytes') {
    return sha256(Buffer.isBuffer(value) ? value : Buffer.from(value));
  }
  if (kind === 'evidence-public-key') {
    return evidencePublicKeyIdentity(value).keyFingerprint;
  }
  throw new Error('fingerprint kind must be normalized-token, file-bytes, or evidence-public-key');
}

export function buildEvidenceKeyManifest({privateKeyPEM, keyId, activatedAt, previousKeyFingerprint = null}) {
  if (!keyIdPattern.test(keyId ?? '')) {
    throw new Error('evidence key ID is invalid');
  }
  createPrivateKey(privateKeyPEM);
  requireTimestamp(activatedAt, 'evidence key activatedAt');
  if (previousKeyFingerprint !== null) {
    requireDigest(previousKeyFingerprint, 'previous evidence key fingerprint');
  }
  const identity = evidencePublicKeyIdentity(privateKeyPEM);
  if (previousKeyFingerprint === identity.keyFingerprint) {
    throw new Error('evidence key rotation must change the public key');
  }
  const core = {
    schemaVersion: 'emlo.choice-tp-observer-evidence-key/v1',
    keyId,
    algorithm: identity.algorithm,
    keyFingerprint: identity.keyFingerprint,
    publicKeyPem: identity.publicKeyPEM,
    publicKeyPemChecksum: sha256(identity.publicKeyPEM),
    activatedAt,
    previousKeyFingerprint,
  };
  return {...core, canonicalChecksum: sha256(core)};
}

export function validateEvidenceKeyManifest(manifest) {
  requireExactKeys(
    manifest,
    [
      'schemaVersion',
      'keyId',
      'algorithm',
      'keyFingerprint',
      'publicKeyPem',
      'publicKeyPemChecksum',
      'activatedAt',
      'previousKeyFingerprint',
      'canonicalChecksum',
    ],
    'evidence public-key manifest'
  );
  if (manifest.schemaVersion !== 'emlo.choice-tp-observer-evidence-key/v1') {
    throw new Error('evidence public-key manifest schema is unsupported');
  }
  if (!keyIdPattern.test(manifest.keyId ?? '') || manifest.algorithm !== 'Ed25519') {
    throw new Error('evidence public-key manifest identity is invalid');
  }
  requireTimestamp(manifest.activatedAt, 'evidence public-key activatedAt');
  requireDigest(manifest.keyFingerprint, 'evidence public-key fingerprint');
  requireDigest(manifest.publicKeyPemChecksum, 'evidence public-key PEM checksum');
  requireDigest(manifest.canonicalChecksum, 'evidence public-key manifest checksum');
  if (manifest.previousKeyFingerprint !== null) {
    requireDigest(manifest.previousKeyFingerprint, 'previous evidence public-key fingerprint');
  }
  const identity = evidencePublicKeyIdentity(manifest.publicKeyPem);
  if (
    identity.keyFingerprint !== manifest.keyFingerprint ||
    sha256(identity.publicKeyPEM) !== manifest.publicKeyPemChecksum ||
    manifest.previousKeyFingerprint === manifest.keyFingerprint ||
    sha256(withoutField(manifest, 'canonicalChecksum')) !== manifest.canonicalChecksum
  ) {
    throw new Error('evidence public-key manifest checksum or key identity does not match');
  }
  return manifest;
}

export function validateRegistrationTemplate(template) {
  requireExactKeys(
    template,
    [
      'deploymentUnitId',
      'observerKey',
      'adapterImplementation',
      'adapterVersion',
      'credential',
      'maxFreshnessSeconds',
      'maxClockSkewSeconds',
      'measurements',
    ],
    'observer registration request template'
  );
  if (!uuidPattern.test(template.deploymentUnitId ?? '')) {
    throw new Error('observer registration deploymentUnitId must be a UUID');
  }
  for (const key of ['observerKey', 'adapterImplementation', 'adapterVersion']) {
    requireString(template[key], `observer registration ${key}`, 256);
  }
  if (template.credential !== registrationCredentialPlaceholder) {
    throw new Error(`observer registration credential must be ${registrationCredentialPlaceholder}`);
  }
  if (
    !Number.isSafeInteger(template.maxFreshnessSeconds) ||
    template.maxFreshnessSeconds < 1 ||
    template.maxFreshnessSeconds > 86400 ||
    !Number.isSafeInteger(template.maxClockSkewSeconds) ||
    template.maxClockSkewSeconds < 0 ||
    template.maxClockSkewSeconds > 300
  ) {
    throw new Error('observer registration freshness policy is invalid');
  }
  if (
    !Array.isArray(template.measurements) ||
    template.measurements.length !== requiredMeasurements.length ||
    requiredMeasurements.some((measurement) => !template.measurements.includes(measurement)) ||
    new Set(template.measurements).size !== template.measurements.length
  ) {
    throw new Error('observer registration measurements must match the Choice TP evidence contract');
  }
  return template;
}

export function renderRegistrationRequest({template, token, evidenceKeyManifest, createdAt}) {
  validateRegistrationTemplate(template);
  validateEvidenceKeyManifest(evidenceKeyManifest);
  requireTimestamp(createdAt, 'registration handoff createdAt');
  const normalizedToken = normalizeObserverToken(token);
  const request = {...template, credential: normalizedToken};
  const recordCore = {
    schemaVersion: 'emlo.choice-tp-observer-registration-handoff/v1',
    endpoint: '/api/v1/observer-registrations',
    deploymentUnitId: request.deploymentUnitId,
    observerKey: request.observerKey,
    adapterImplementation: request.adapterImplementation,
    adapterVersion: request.adapterVersion,
    measurements: request.measurements,
    observerTokenFingerprint: sha256(normalizedToken),
    evidenceKeyId: evidenceKeyManifest.keyId,
    evidencePublicKeyFingerprint: evidenceKeyManifest.keyFingerprint,
    evidencePublicKeyManifestChecksum: evidenceKeyManifest.canonicalChecksum,
    registrationRequestChecksum: sha256(request),
    createdAt,
  };
  return {request, record: {...recordCore, canonicalChecksum: sha256(recordCore)}};
}

function validateServiceState(state, serviceConfig) {
  requireExactKeys(state, ['schemaVersion', 'serviceConfigChecksum', 'intents'], 'observer service state');
  if (
    state.schemaVersion !== 'emlo.choice-tp-observer-service-state/v1' ||
    state.serviceConfigChecksum !== serviceConfig.canonicalChecksum ||
    !state.intents ||
    Array.isArray(state.intents) ||
    typeof state.intents !== 'object' ||
    Object.keys(state.intents).length > 512
  ) {
    throw new Error('observer service state does not match the active service configuration');
  }
  for (const intentState of Object.values(state.intents)) {
    if (!['COMPLETE', 'EXHAUSTED'].includes(intentState?.status)) {
      throw new Error('evidence key rotation refuses pending or unknown intent state');
    }
  }
  return state;
}

export function prepareEvidenceKeyRotation({
  serviceConfig,
  state,
  keyManifests,
  nextKeyManifest,
  evidenceFiles,
  preparedAt,
}) {
  validateServiceConfig(serviceConfig);
  validateServiceState(state, serviceConfig);
  requireTimestamp(preparedAt, 'evidence key rotation preparedAt');
  if (keyManifests.length < 1 || keyManifests.length > maximumKeyManifests) {
    throw new Error(`evidence public-key history must contain one through ${maximumKeyManifests} manifests`);
  }
  if (evidenceFiles.length > maximumEvidenceFiles) {
    throw new Error(`retained evidence inventory exceeds ${maximumEvidenceFiles} files`);
  }
  const history = keyManifests.map(validateEvidenceKeyManifest);
  const next = validateEvidenceKeyManifest(nextKeyManifest);
  const byFingerprint = new Map();
  const keyIds = new Set();
  for (const manifest of history) {
    if (byFingerprint.has(manifest.keyFingerprint) || keyIds.has(manifest.keyId)) {
      throw new Error('evidence public-key history contains a duplicate identity');
    }
    byFingerprint.set(manifest.keyFingerprint, manifest);
    keyIds.add(manifest.keyId);
  }
  const activeFingerprint = serviceConfig.credentials.evidencePublicKeyFingerprint;
  const current = byFingerprint.get(activeFingerprint);
  if (!current) {
    throw new Error('active evidence public key is missing from retained key history');
  }
  if (
    next.previousKeyFingerprint !== current.keyFingerprint ||
    byFingerprint.has(next.keyFingerprint) ||
    keyIds.has(next.keyId) ||
    new Date(next.activatedAt) <= new Date(current.activatedAt)
  ) {
    throw new Error('next evidence public key does not form a valid forward rotation');
  }
  for (const manifest of history) {
    if (manifest.previousKeyFingerprint === null) continue;
    const previous = byFingerprint.get(manifest.previousKeyFingerprint);
    if (!previous || new Date(previous.activatedAt) >= new Date(manifest.activatedAt)) {
      throw new Error('evidence public-key history is incomplete or out of order');
    }
  }
  const lineage = new Set();
  for (let manifest = current; manifest; manifest = byFingerprint.get(manifest.previousKeyFingerprint)) {
    if (lineage.has(manifest.keyFingerprint)) {
      throw new Error('evidence public-key history contains a cycle');
    }
    lineage.add(manifest.keyFingerprint);
    if (manifest.previousKeyFingerprint === null) break;
  }
  if (lineage.size !== history.length) {
    throw new Error('evidence public-key history contains a disconnected key');
  }
  const evidenceByName = new Map();
  const inventory = evidenceFiles
    .map(({name, evidence}) => {
      requireString(name, 'retained evidence file name', 255);
      if (path.basename(name) !== name) {
        throw new Error('retained evidence file name must not contain a path');
      }
      if (evidenceByName.has(name)) {
        throw new Error('retained evidence inventory contains a duplicate file name');
      }
      const keyFingerprint = evidence?.signature?.keyFingerprint;
      const keyManifest = byFingerprint.get(keyFingerprint);
      if (!keyManifest) {
        throw new Error('retained evidence has no matching public key in key history');
      }
      verifyEvidenceEnvelopeSignature(evidence, keyManifest.publicKeyPem);
      evidenceByName.set(name, evidence);
      return {name, evidenceChecksum: evidence.evidenceChecksum, keyFingerprint};
    })
    .sort((left, right) => left.name.localeCompare(right.name));
  for (const intentState of Object.values(state.intents)) {
    if (intentState.evidenceFile === null) continue;
    const evidence = evidenceByName.get(intentState.evidenceFile);
    if (!evidence || evidence.evidenceChecksum !== intentState.evidenceChecksum) {
      throw new Error('observer state evidence reference is missing or checksum-mismatched');
    }
  }
  const core = {
    schemaVersion: 'emlo.choice-tp-observer-evidence-key-rotation/v1',
    serviceConfigChecksum: serviceConfig.canonicalChecksum,
    serviceStateChecksum: sha256(state),
    previousKeyFingerprint: current.keyFingerprint,
    previousKeyManifestChecksum: current.canonicalChecksum,
    nextKeyFingerprint: next.keyFingerprint,
    nextKeyManifestChecksum: next.canonicalChecksum,
    retainedKeyFingerprints: [...byFingerprint.keys()].sort(),
    retainedEvidenceCount: inventory.length,
    retainedEvidenceInventoryChecksum: sha256(inventory),
    preparedAt,
  };
  return {...core, canonicalChecksum: sha256(core)};
}

function parseOptions(argv) {
  const result = new Map();
  for (let index = 0; index < argv.length; index += 2) {
    const key = argv[index];
    const value = argv[index + 1];
    if (!key?.startsWith('--') || value === undefined || result.has(key)) {
      throw new Error('provisioning options must be unique --name value pairs');
    }
    result.set(key, value);
  }
  return result;
}

function requireOption(options, name) {
  const value = options.get(name);
  if (!value) throw new Error(`${name} is required`);
  return value;
}

function rejectUnknownOptions(options, allowed) {
  for (const key of options.keys()) {
    if (!allowed.includes(key)) throw new Error(`unsupported option ${key}`);
  }
}

async function loadJSONDirectory(directory, suffix, minimumEntries, maximumEntries, label) {
  const entries = await readdir(directory, {withFileTypes: true});
  const selected = entries
    .filter((entry) => entry.isFile() && entry.name.endsWith(suffix))
    .sort((a, b) => a.name.localeCompare(b.name));
  if (selected.length < minimumEntries || selected.length > maximumEntries) {
    throw new Error(`${label} must contain ${minimumEntries} through ${maximumEntries} ${suffix} files`);
  }
  return Promise.all(
    selected.map(async (entry) => ({
      name: entry.name,
      value: await readBoundedJSON(path.join(directory, entry.name), `${label} ${entry.name}`),
    }))
  );
}

async function runExportEvidenceKey(options) {
  rejectUnknownOptions(options, [
    '--private-key-file',
    '--public-key-file',
    '--manifest-file',
    '--key-id',
    '--activated-at',
    '--previous-key-fingerprint',
  ]);
  const privateKeyFile = path.resolve(requireOption(options, '--private-key-file'));
  const publicKeyFile = path.resolve(requireOption(options, '--public-key-file'));
  const manifestFile = path.resolve(requireOption(options, '--manifest-file'));
  const privateKeyPEM = await readBoundedFile(privateKeyFile, maximumCredentialBytes, 'evidence private key', {
    privateFile: true,
  });
  const manifest = buildEvidenceKeyManifest({
    privateKeyPEM,
    keyId: requireOption(options, '--key-id'),
    activatedAt: requireOption(options, '--activated-at'),
    previousKeyFingerprint: options.get('--previous-key-fingerprint') ?? null,
  });
  await writeExclusiveFiles([
    {filePath: publicKeyFile, contents: manifest.publicKeyPem, mode: 0o644},
    {filePath: manifestFile, contents: `${JSON.stringify(manifest, null, 2)}\n`, mode: 0o644},
  ]);
  return {
    status: 'exported',
    keyId: manifest.keyId,
    keyFingerprint: manifest.keyFingerprint,
    manifestChecksum: manifest.canonicalChecksum,
  };
}

async function runFingerprint(options) {
  rejectUnknownOptions(options, ['--kind', '--input-file']);
  const kind = requireOption(options, '--kind');
  const inputFile = path.resolve(requireOption(options, '--input-file'));
  const value = await readBoundedFile(inputFile, maximumCredentialBytes, 'credential input', {
    privateFile: kind !== 'file-bytes',
  });
  return {status: 'fingerprinted', kind, fingerprint: fingerprintCredential(kind, value)};
}

async function runRenderRegistration(options) {
  rejectUnknownOptions(options, [
    '--template',
    '--token-file',
    '--evidence-key-manifest',
    '--request-output',
    '--record-output',
    '--created-at',
  ]);
  const template = await readBoundedJSON(
    path.resolve(requireOption(options, '--template')),
    'observer registration template'
  );
  const token = await readBoundedFile(
    path.resolve(requireOption(options, '--token-file')),
    1024,
    'scoped observer token',
    {privateFile: true}
  );
  const evidenceKeyManifest = await readBoundedJSON(
    path.resolve(requireOption(options, '--evidence-key-manifest')),
    'evidence public-key manifest'
  );
  const rendered = renderRegistrationRequest({
    template,
    token,
    evidenceKeyManifest,
    createdAt: requireOption(options, '--created-at'),
  });
  await writeExclusiveFiles([
    {
      filePath: path.resolve(requireOption(options, '--request-output')),
      contents: `${JSON.stringify(rendered.request, null, 2)}\n`,
      mode: 0o600,
    },
    {
      filePath: path.resolve(requireOption(options, '--record-output')),
      contents: `${JSON.stringify(rendered.record, null, 2)}\n`,
      mode: 0o600,
    },
  ]);
  return {
    status: 'rendered',
    observerTokenFingerprint: rendered.record.observerTokenFingerprint,
    evidencePublicKeyFingerprint: rendered.record.evidencePublicKeyFingerprint,
    recordChecksum: rendered.record.canonicalChecksum,
  };
}

async function runPrepareRotation(options) {
  rejectUnknownOptions(options, [
    '--service-config',
    '--state-file',
    '--key-history-directory',
    '--next-key-manifest',
    '--evidence-directory',
    '--receipt-output',
    '--prepared-at',
  ]);
  const serviceConfig = await readBoundedJSON(
    path.resolve(requireOption(options, '--service-config')),
    'observer service config'
  );
  const state = await readBoundedJSON(path.resolve(requireOption(options, '--state-file')), 'observer service state');
  const keyEntries = await loadJSONDirectory(
    path.resolve(requireOption(options, '--key-history-directory')),
    '.key.json',
    1,
    maximumKeyManifests,
    'evidence key history'
  );
  const evidenceEntries = await loadJSONDirectory(
    path.resolve(requireOption(options, '--evidence-directory')),
    '.evidence.json',
    0,
    maximumEvidenceFiles,
    'retained evidence directory'
  );
  const nextKeyManifest = await readBoundedJSON(
    path.resolve(requireOption(options, '--next-key-manifest')),
    'next evidence public-key manifest'
  );
  const receipt = prepareEvidenceKeyRotation({
    serviceConfig,
    state,
    keyManifests: keyEntries.map(({value}) => value),
    nextKeyManifest,
    evidenceFiles: evidenceEntries.map(({name, value}) => ({name, evidence: value})),
    preparedAt: requireOption(options, '--prepared-at'),
  });
  await writeJSONExclusive(path.resolve(requireOption(options, '--receipt-output')), receipt, 0o600);
  return {
    status: 'rotation-prepared',
    previousKeyFingerprint: receipt.previousKeyFingerprint,
    nextKeyFingerprint: receipt.nextKeyFingerprint,
    retainedEvidenceCount: receipt.retainedEvidenceCount,
    receiptChecksum: receipt.canonicalChecksum,
  };
}

async function main() {
  const [command, ...argv] = process.argv.slice(2);
  const options = parseOptions(argv);
  let result;
  if (command === 'export-evidence-key') result = await runExportEvidenceKey(options);
  else if (command === 'fingerprint') result = await runFingerprint(options);
  else if (command === 'render-registration') result = await runRenderRegistration(options);
  else if (command === 'prepare-key-rotation') result = await runPrepareRotation(options);
  else {
    throw new Error(
      'usage: provision.mjs <export-evidence-key|fingerprint|render-registration|prepare-key-rotation> [options]'
    );
  }
  process.stdout.write(`${JSON.stringify(result)}\n`);
}

const isMain = process.argv[1] && fileURLToPath(import.meta.url) === path.resolve(process.argv[1]);
if (isMain) {
  main().catch((error) => {
    process.stderr.write(`choice-tp observer provisioning failed: ${error.message}\n`);
    process.exitCode = 1;
  });
}
