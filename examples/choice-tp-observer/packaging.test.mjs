import assert from 'node:assert/strict';
import {appendFile, mkdir, mkdtemp, readFile, rm, writeFile} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import test from 'node:test';
import {sha256} from './observer.mjs';
import {parseEnvironment, runPreflight, validateImmutableImageReference, validateSourceRevision} from './preflight.mjs';
import {validateLegacyEvidence, validateServiceConfig} from './service.mjs';

const read = (name) => readFile(new URL(name, import.meta.url), 'utf8');
const readBytes = (name) => readFile(new URL(name, import.meta.url));
const legacyBaselineChecksum = 'sha256:791955e37fd9911e472aa03512197a4e013784049e7651eaa772bad74e5a3815';

test('Dockerfile pins and validates its base and revision identity', async () => {
  const dockerfile = await read('Dockerfile');
  assert.match(
    dockerfile,
    /node:24\.8\.0-alpine3\.22@sha256:3e843c608bb5232f39ecb2b25e41214b958b0795914707374c8acc28487dea17/
  );
  assert.match(dockerfile, /grep -Eq '@sha256:\[0-9a-f\]\{64\}\$'/);
  assert.match(dockerfile, /grep -Eq '\^\[0-9a-f\]\{40\}\$'/);
  assert.match(dockerfile, /org\.opencontainers\.image\.revision="\$\{SOURCE_REVISION\}"/);
});

test('Compose keeps an immutable, bounded, secret-file-only runtime', async () => {
  const compose = await read('compose.yaml');
  for (const required of [
    'platform: linux/amd64',
    'pull_policy: always',
    "cpus: '0.50'",
    'mem_limit: 256m',
    'pids_limit: 128',
    'max-size: 10m',
    'observer_ssh_key:',
    'distr_observer_token:',
    'evidence_ed25519_key:',
    'observer-egress:',
    '- --health',
  ]) {
    assert.match(compose, new RegExp(required.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')));
  }
  assert.doesNotMatch(compose, /\.\/secrets:\/etc\//);
  assert.doesNotMatch(compose, /docker\.sock|environment:/);
});

test('Docker build context contains only the runtime files', async () => {
  assert.equal(await read('.dockerignore'), '*\n!Dockerfile\n!observer.mjs\n!service.mjs\n');
});

test('environment and release identity validators reject mutable inputs', async () => {
  const environment = parseEnvironment(await read('.env.example'));
  assert.match(environment.CHOICE_TP_OBSERVER_IMAGE, /@sha256:/);
  assert.equal(validateSourceRevision('a'.repeat(40)), 'a'.repeat(40));
  assert.throws(() => validateSourceRevision('main'), /40-hex Git commit/);
  assert.throws(() => validateImmutableImageReference('registry.example/observer:latest'), /repository@sha256/);
  assert.throws(
    () =>
      parseEnvironment(
        `COMPOSE_PROJECT_NAME=choice-tp-observer\nCHOICE_TP_OBSERVER_IMAGE=${environment.CHOICE_TP_OBSERVER_IMAGE}\nTOKEN=secret\n`
      ),
    /unsupported or duplicate key/
  );
});

test('tracked C0/T0 evidence exactly matches both immutable service pins', async () => {
  assert.equal(await read('.gitattributes'), 'choice-tp-c0-t0-baseline-runtime-evidence.json text eol=lf\n');
  const bytes = await readBytes('choice-tp-c0-t0-baseline-runtime-evidence.json');
  const evidence = JSON.parse(bytes.toString('utf8'));
  assert.equal(sha256(bytes), legacyBaselineChecksum);
  for (const configName of ['service.example.json', 'service.c0-t0.example.json']) {
    const config = validateServiceConfig(JSON.parse(await read(configName)));
    assert.equal(config.legacyBaseline.evidenceFileChecksum, legacyBaselineChecksum);
    assert.equal(validateLegacyEvidence(evidence, config), evidence);
  }
});

test('preflight validates the sealed deployment layout without reading live systems', async (t) => {
  const root = await mkdtemp(path.join(tmpdir(), 'choice-tp-observer-preflight-'));
  t.after(() => rm(root, {recursive: true, force: true}));
  for (const directory of ['config/history', 'secrets', 'intents', 'evidence', 'state']) {
    await mkdir(path.join(root, directory), {recursive: true});
  }
  await Promise.all([
    writeFile(path.join(root, '.env'), await read('.env.example')),
    writeFile(path.join(root, 'config/service.json'), await read('service.example.json')),
    writeFile(path.join(root, 'config/profile.json'), await read('choice-tp-dev.profile.json')),
    writeFile(path.join(root, 'config/known_hosts'), '217.15.166.6 ssh-ed25519 AAAATEST\n'),
    writeFile(
      path.join(root, 'config/choice-tp-c0-t0-baseline-runtime-evidence.json'),
      await readBytes('choice-tp-c0-t0-baseline-runtime-evidence.json')
    ),
    writeFile(path.join(root, 'secrets/observer_ssh_key'), 'test-only\n', {mode: 0o600}),
    writeFile(path.join(root, 'secrets/distr_observer_token'), 'test-only\n', {mode: 0o600}),
    writeFile(path.join(root, 'secrets/evidence_ed25519_key'), 'test-only\n', {mode: 0o600}),
  ]);
  const result = await runPreflight({root});
  assert.equal(result.profileChecksum, 'sha256:dc02d13606d0594268aba1bc3841218ec03daf9a8a4324894bb50e91406ca3d8');
  assert.equal(result.serviceConfigChecksum, 'sha256:5e8a07dbaa60a077af7f2da3dc7b3dfcc36d86911a5dd621a7d7df7160428164');
  assert.equal(result.legacyBaselineEvidenceChecksum, legacyBaselineChecksum);

  await appendFile(path.join(root, 'config/choice-tp-c0-t0-baseline-runtime-evidence.json'), ' ');
  await assert.rejects(() => runPreflight({root}), /legacy C0\/T0 evidence file checksum/);
});
