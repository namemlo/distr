#!/usr/bin/env node

import {lstat, readFile} from 'node:fs/promises';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import {sha256, validateProfile} from './observer.mjs';

const imagePattern = /^[a-z0-9][a-z0-9._:/-]*@sha256:[0-9a-f]{64}$/;
const revisionPattern = /^[0-9a-f]{40}$/;
const allowedEnvironmentKeys = new Set(['COMPOSE_PROJECT_NAME', 'CHOICE_TP_OBSERVER_IMAGE']);

export function validateImmutableImageReference(value) {
  if (!imagePattern.test(value ?? '') || value.includes('//') || value.includes('..')) {
    throw new Error('CHOICE_TP_OBSERVER_IMAGE must be one lowercase repository@sha256 digest reference');
  }
  return value;
}

export function validateSourceRevision(value) {
  if (!revisionPattern.test(value ?? '')) {
    throw new Error('SOURCE_REVISION must be one lowercase 40-hex Git commit');
  }
  return value;
}

export function parseEnvironment(contents) {
  const result = {};
  for (const rawLine of contents.split(/\r?\n/)) {
    const line = rawLine.trim();
    if (!line || line.startsWith('#')) continue;
    const separator = line.indexOf('=');
    if (separator < 1) throw new Error('observer environment contains an invalid line');
    const key = line.slice(0, separator);
    const value = line.slice(separator + 1);
    if (!allowedEnvironmentKeys.has(key) || Object.hasOwn(result, key)) {
      throw new Error('observer environment contains an unsupported or duplicate key');
    }
    result[key] = value;
  }
  validateImmutableImageReference(result.CHOICE_TP_OBSERVER_IMAGE);
  if (result.COMPOSE_PROJECT_NAME !== 'choice-tp-observer') {
    throw new Error('COMPOSE_PROJECT_NAME must be choice-tp-observer');
  }
  return result;
}

async function requirePath(root, relativePath, expectedType, privateFile = false) {
  const resolved = path.resolve(root, relativePath);
  if (!resolved.startsWith(`${path.resolve(root)}${path.sep}`)) {
    throw new Error(`preflight path escapes the deployment root: ${relativePath}`);
  }
  const metadata = await lstat(resolved);
  if (metadata.isSymbolicLink() || (expectedType === 'file' ? !metadata.isFile() : !metadata.isDirectory())) {
    throw new Error(`preflight ${relativePath} must be a non-symlink ${expectedType}`);
  }
  if (privateFile && process.platform !== 'win32' && (metadata.mode & 0o077) !== 0) {
    throw new Error(`preflight ${relativePath} must not grant group or other permissions`);
  }
}

export async function runPreflight({root, envFile = path.join(root, '.env')}) {
  const environment = parseEnvironment(await readFile(envFile, 'utf8'));
  for (const relativePath of [
    'config/service.json',
    'config/profile.json',
    'config/known_hosts',
    'config/choice-tp-c0-t0-baseline-runtime-evidence.json',
  ]) {
    await requirePath(root, relativePath, 'file');
  }
  for (const relativePath of [
    'secrets/observer_ssh_key',
    'secrets/distr_observer_token',
    'secrets/evidence_ed25519_key',
  ]) {
    await requirePath(root, relativePath, 'file', true);
  }
  for (const relativePath of ['config/history', 'intents', 'evidence', 'state']) {
    await requirePath(root, relativePath, 'directory');
  }
  const service = JSON.parse(await readFile(path.join(root, 'config/service.json'), 'utf8'));
  const serviceChecksum = sha256(
    Object.fromEntries(Object.entries(service).filter(([key]) => key !== 'canonicalChecksum'))
  );
  if (service.canonicalChecksum !== serviceChecksum || service.profileFile !== '/etc/choice-tp-observer/profile.json') {
    throw new Error('service config checksum or mounted profile path is invalid');
  }
  const profile = validateProfile(JSON.parse(await readFile(path.join(root, 'config/profile.json'), 'utf8')));
  return {
    image: environment.CHOICE_TP_OBSERVER_IMAGE,
    serviceConfigChecksum: service.canonicalChecksum,
    profileChecksum: profile.canonicalChecksum,
  };
}

function parseArguments(argv) {
  let root = process.cwd();
  let envFile;
  for (let index = 0; index < argv.length; index += 1) {
    if (argv[index] === '--root') {
      root = path.resolve(argv[index + 1]);
      index += 1;
    } else if (argv[index] === '--env-file') {
      envFile = path.resolve(argv[index + 1]);
      index += 1;
    } else {
      throw new Error('usage: preflight.mjs [--root <deployment-root>] [--env-file <path>]');
    }
  }
  return {root, envFile: envFile ?? path.join(root, '.env')};
}

const isMain = process.argv[1] && fileURLToPath(import.meta.url) === path.resolve(process.argv[1]);
if (isMain) {
  runPreflight(parseArguments(process.argv.slice(2)))
    .then((result) => process.stdout.write(`${JSON.stringify({status: 'valid', ...result})}\n`))
    .catch((error) => {
      process.stderr.write(`choice-tp observer preflight failed: ${error.message}\n`);
      process.exitCode = 1;
    });
}
