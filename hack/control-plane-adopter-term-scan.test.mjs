import assert from 'node:assert/strict';
import {execFile} from 'node:child_process';
import {createHash} from 'node:crypto';
import {access, mkdir, mkdtemp, rm, writeFile} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {test} from 'node:test';
import {fileURLToPath} from 'node:url';
import {promisify} from 'node:util';

const execFileAsync = promisify(execFile);
const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const scanner = path.join(repoRoot, 'hack', 'control-plane-adopter-term-scan.mjs');
const authoritativeException = 'docs/superpowers/plans/2026-07-14-control-plane-operator-adoption.md';
const emloForkBaseline = 'docs/fork/EMLO_FORK_ADOPTER_TERM_BASELINE.json';
const requiredArtifacts = [
  'docs/api/community-release-api-index.md',
  'docs/api/operator-control-plane-api.md',
  'docs/fork/FORK_DIFF_INDEX.md',
  'docs/fork/PR-083_ENTERPRISE_CONTROL_PLANE_HARDENING.md',
  'docs/fork/UPGRADE_GUIDE.md',
  'hack/control-plane-adopter-term-scan.mjs',
  'hack/control-plane-adopter-term-scan.test.mjs',
];

async function git(cwd, ...args) {
  return execFileAsync('git', args, {cwd, encoding: 'utf8'});
}

async function repository() {
  const directory = await mkdtemp(path.join(tmpdir(), 'control-plane-adopter-scan-'));
  await git(directory, 'init', '--quiet');
  await git(directory, 'config', 'user.name', 'Acceptance Scanner');
  await git(directory, 'config', 'user.email', 'scanner@example.invalid');
  await writeFile(path.join(directory, 'README.md'), '# Neutral baseline\n');
  for (const artifact of requiredArtifacts) {
    const artifactPath = path.join(directory, ...artifact.split('/'));
    await mkdir(path.dirname(artifactPath), {recursive: true});
    await writeFile(artifactPath, '# Neutral required artifact\n');
  }
  await git(directory, 'add', 'README.md');
  await git(directory, 'add', ...requiredArtifacts);
  await git(directory, 'commit', '--quiet', '-m', 'baseline');
  return directory;
}

function sourceSha256(value) {
  return createHash('sha256').update(value, 'utf8').digest('hex');
}

async function writeEmloForkBaseline(directory, findings) {
  const sourceCommit = (await git(directory, 'rev-parse', 'HEAD')).stdout.trim();
  const baselinePath = path.join(directory, ...emloForkBaseline.split('/'));
  await mkdir(path.dirname(baselinePath), {recursive: true});
  await writeFile(
    baselinePath,
    `${JSON.stringify(
      {
        schema: 'distr.adopter-term-baseline/v1',
        profile: 'emlo-fork',
        repository: 'namemlo/distr',
        sourceCommit,
        findings,
      },
      null,
      2
    )}\n`
  );
}

function run(cwd, args = ['--base', 'HEAD']) {
  return new Promise((resolve) => {
    const child = execFile(process.execPath, [scanner, ...args], {cwd}, (error, stdout, stderr) => {
      resolve({status: error?.code ?? 0, stdout, stderr});
    });
    child.stdin?.end();
  });
}

test('reports prohibited terms in tracked and untracked changed lines with stable file and line locations', async () => {
  const directory = await repository();
  await writeFile(path.join(directory, 'README.md'), '# Neutral baseline\nUse a Jenkins job here.\n');
  await mkdir(path.join(directory, 'docs'), {recursive: true});
  await writeFile(path.join(directory, 'docs', 'new.md'), 'A choice_tp binding is not community-neutral.\n');

  const first = await run(directory);
  const second = await run(directory);

  assert.notEqual(first.status, 0);
  assert.equal(first.stdout, '');
  assert.equal(first.stderr, second.stderr);
  assert.match(first.stderr, /README\.md:2: prohibited Jenkins implementation term/);
  assert.match(first.stderr, /docs\/new\.md:1: prohibited Choice TP adopter name/);
  assert.ok(first.stderr.indexOf('README.md:2') < first.stderr.indexOf('docs/new.md:1'));
  assert.match(first.stderr, /Adopter-term scan failed: 2 findings in 2 scanned files\./);
});

test('default community scan still rejects a finding listed by the named-fork profile', async () => {
  const directory = await repository();
  const source = 'Choice TP release fixture.\n';
  await writeFile(path.join(directory, 'fork-release.md'), source);
  await writeEmloForkBaseline(directory, [
    {
      file: 'fork-release.md',
      line: 1,
      label: 'Choice TP adopter name',
      sourceSha256: sourceSha256(source.trimEnd()),
    },
  ]);

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /fork-release\.md:1: prohibited Choice TP adopter name/);
  assert.doesNotMatch(result.stdout, /emlo-fork adopter-term profile accepted/);
});

test('named-fork profile accepts only the exact declared baseline finding', async () => {
  const directory = await repository();
  const source = 'Choice TP release fixture.\n';
  await writeFile(path.join(directory, 'fork-release.md'), source);
  await writeEmloForkBaseline(directory, [
    {
      file: 'fork-release.md',
      line: 1,
      label: 'Choice TP adopter name',
      sourceSha256: sourceSha256(source.trimEnd()),
    },
  ]);

  const result = await run(directory, ['--base', 'HEAD', '--profile', 'emlo-fork']);

  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, /emlo-fork adopter-term profile accepted 1 exact reviewed finding\./);
  assert.match(result.stdout, /not a community or upstream release result/);
});

test('named-fork profile rejects an injected finding that is absent from the exact baseline', async () => {
  const directory = await repository();
  const acceptedSource = 'Choice TP release fixture.';
  await writeFile(path.join(directory, 'fork-release.md'), `${acceptedSource}\nUse a new Jenkins release job.\n`);
  await writeEmloForkBaseline(directory, [
    {
      file: 'fork-release.md',
      line: 1,
      label: 'Choice TP adopter name',
      sourceSha256: sourceSha256(acceptedSource),
    },
  ]);

  const result = await run(directory, ['--base', 'HEAD', '--profile', 'emlo-fork']);

  assert.notEqual(result.status, 0);
  assert.doesNotMatch(result.stderr, /fork-release\.md:1/);
  assert.match(result.stderr, /fork-release\.md:2: prohibited Jenkins implementation term/);
  assert.match(result.stderr, /Adopter-term scan failed: 1 finding in 1 scanned file\./);
});

test('named-fork profile rejects a stale or unused baseline finding', async () => {
  const directory = await repository();
  const source = 'Choice TP release fixture.';
  await writeFile(path.join(directory, 'fork-release.md'), `${source}\n`);
  await writeEmloForkBaseline(directory, [
    {
      file: 'fork-release.md',
      line: 1,
      label: 'Choice TP adopter name',
      sourceSha256: sourceSha256(source),
    },
    {
      file: 'unused-release.md',
      line: 1,
      label: 'Jenkins implementation term',
      sourceSha256: sourceSha256('Use a Jenkins release job.'),
    },
  ]);

  const result = await run(directory, ['--base', 'HEAD', '--profile', 'emlo-fork']);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /1 stale, unused, or forged exact finding/);
  assert.match(result.stderr, /named-fork baseline did not exactly match findings in 1 scanned file/);
});

test('named-fork profile rejects a forged source hash', async () => {
  const directory = await repository();
  await writeFile(path.join(directory, 'fork-release.md'), 'Choice TP release fixture.\n');
  await writeEmloForkBaseline(directory, [
    {
      file: 'fork-release.md',
      line: 1,
      label: 'Choice TP adopter name',
      sourceSha256: '0'.repeat(64),
    },
  ]);

  const result = await run(directory, ['--base', 'HEAD', '--profile', 'emlo-fork']);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /fork-release\.md:1: prohibited Choice TP adopter name/);
  assert.match(result.stderr, /1 stale, unused, or forged exact finding/);
  assert.match(result.stderr, /Adopter-term scan failed: 1 finding in 1 scanned file/);
});

test('named-fork profile requires canonical bytewise finding order', async () => {
  const directory = await repository();
  const choiceSource = 'Choice TP release fixture.';
  const jenkinsSource = 'Use a Jenkins release job.';
  await writeFile(path.join(directory, 'fork-release.md'), `${choiceSource}\n${jenkinsSource}\n`);
  await writeEmloForkBaseline(directory, [
    {
      file: 'fork-release.md',
      line: 2,
      label: 'Jenkins implementation term',
      sourceSha256: sourceSha256(jenkinsSource),
    },
    {
      file: 'fork-release.md',
      line: 1,
      label: 'Choice TP adopter name',
      sourceSha256: sourceSha256(choiceSource),
    },
  ]);

  const result = await run(directory, ['--base', 'HEAD', '--profile', 'emlo-fork']);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /baseline findings are not in canonical bytewise order/);
});

test('named-fork profile requires canonical finding key serialization', async () => {
  const directory = await repository();
  const source = 'Choice TP release fixture.';
  await writeFile(path.join(directory, 'fork-release.md'), `${source}\n`);
  await writeEmloForkBaseline(directory, [
    {
      label: 'Choice TP adopter name',
      file: 'fork-release.md',
      line: 1,
      sourceSha256: sourceSha256(source),
    },
  ]);

  const result = await run(directory, ['--base', 'HEAD', '--profile', 'emlo-fork']);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /baseline contains an invalid exact finding/);
});

test('named-fork profile requires the source commit to include the scan base', async () => {
  const directory = await repository();
  const source = 'Choice TP release fixture.';
  await writeFile(path.join(directory, 'fork-release.md'), `${source}\n`);
  await writeEmloForkBaseline(directory, [
    {
      file: 'fork-release.md',
      line: 1,
      label: 'Choice TP adopter name',
      sourceSha256: sourceSha256(source),
    },
  ]);
  await writeFile(path.join(directory, 'later-base.md'), 'Neutral later base.\n');
  await git(directory, 'add', 'later-base.md');
  await git(directory, 'commit', '--quiet', '-m', 'later scan base');

  const result = await run(directory, ['--base', 'HEAD', '--profile', 'emlo-fork']);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /profile sourceCommit does not include the requested scan base/);
});

test('allows prohibited vocabulary only in exact authoritative policy documents', async () => {
  const directory = await repository();
  const exceptionPath = path.join(directory, ...authoritativeException.split('/'));
  await mkdir(path.dirname(exceptionPath), {recursive: true});
  await writeFile(
    exceptionPath,
    'EMLO Choice TP remittance Jenkins ECR C:\\Users\\private\\workspace /home/private/workspace\n'
  );
  await writeFile(path.join(directory, 'neutral.md'), 'Generic OCI executor and configuration repository.\n');

  const result = await run(directory);

  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, /Adopter-term scan passed: 1 file scanned, 1 policy exception, 0 binary files skipped\./);
});

test('detects every adopter and private-path rule without printing the private path', async () => {
  const directory = await repository();
  const privateWindowsPath = 'C:\\Users\\alice\\secret-workspace';
  const privatePosixPath = '/home/alice/secret-workspace';
  const privateUNCPath = '\\\\private-server\\alice\\secret-workspace';
  await writeFile(
    path.join(directory, 'terms.md'),
    [
      'emlo',
      'Choice-TP',
      'remittance-api',
      'Jenkinsfile',
      'amazon ECR',
      privateWindowsPath,
      privatePosixPath,
      privateUNCPath,
      '',
    ].join('\n')
  );

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /terms\.md:1: prohibited EMLO adopter name/);
  assert.match(result.stderr, /terms\.md:2: prohibited Choice TP adopter name/);
  assert.match(result.stderr, /terms\.md:3: prohibited remittance domain term/);
  assert.match(result.stderr, /terms\.md:4: prohibited Jenkins implementation term/);
  assert.match(result.stderr, /terms\.md:5: prohibited ECR registry term/);
  assert.match(result.stderr, /terms\.md:6: prohibited private Windows path/);
  assert.match(result.stderr, /terms\.md:7: prohibited private POSIX home path/);
  assert.match(result.stderr, /terms\.md:8: prohibited private UNC path/);
  assert.doesNotMatch(result.stderr, /alice|private-server|secret-workspace/);
});

test('detects known organization and provider variants in changed content', async () => {
  const directory = await repository();
  await writeFile(
    path.join(directory, 'terms.md'),
    ['emlotech', 'distr.emlotech.com', 'remittances', 'JenkinsCI', 'ECRRepository', ''].join('\n')
  );

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /terms\.md:1: prohibited EMLO adopter name/);
  assert.match(result.stderr, /terms\.md:2: prohibited EMLO adopter name/);
  assert.match(result.stderr, /terms\.md:3: prohibited remittance domain term/);
  assert.match(result.stderr, /terms\.md:4: prohibited Jenkins implementation term/);
  assert.match(result.stderr, /terms\.md:5: prohibited ECR registry term/);
});

test('allows only reviewed provider phrases in their exact implementation and test files', async () => {
  const directory = await repository();
  const publisher = path.join(directory, 'deploy', 'jenkins', 'publish-hub-image.sh');
  const validatorTest = path.join(directory, 'hack', 'pr050-govulncheck.test.mjs');
  await mkdir(path.dirname(publisher), {recursive: true});
  await writeFile(publisher, 'die "COSIGN_PASSWORD must come from a Jenkins string credential"\n');
  await writeFile(
    validatorTest,
    [
      "test('the PR-050 validator seals Jenkins signed evidence, matrix, adopter scan, and failure retention', () => {",
      "readFileSync(new URL('../deploy/jenkins/Jenkinsfile.hub-image', import.meta.url), 'utf8'),",
      "readFileSync(new URL('../deploy/jenkins/publish-hub-image.sh', import.meta.url), 'utf8'),",
      '',
    ].join('\n')
  );

  const result = await run(directory);

  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, /Adopter-term scan passed: 2 files scanned/);
});

test('does not extend provider phrase exceptions to another file or unreviewed context', async () => {
  const outsideDirectory = await repository();
  await writeFile(
    path.join(outsideDirectory, 'README.md'),
    '# Neutral baseline\nCOSIGN_PASSWORD must come from a Jenkins string credential\n'
  );
  const outsideResult = await run(outsideDirectory);
  assert.notEqual(outsideResult.status, 0);
  assert.match(outsideResult.stderr, /README\.md:2: prohibited Jenkins implementation term/);

  const unreviewedDirectory = await repository();
  const publisher = path.join(unreviewedDirectory, 'deploy', 'jenkins', 'publish-hub-image.sh');
  await mkdir(path.dirname(publisher), {recursive: true});
  await writeFile(publisher, 'Use JenkinsCI for this release.\n');
  const unreviewedResult = await run(unreviewedDirectory);
  assert.notEqual(unreviewedResult.status, 0);
  assert.match(
    unreviewedResult.stderr,
    /deploy\/jenkins\/publish-hub-image\.sh:1: prohibited Jenkins implementation term/
  );
});

test('detects prohibited terms in changed filenames even when file content is neutral', async () => {
  const directory = await repository();
  await writeFile(path.join(directory, 'emlotech-client.md'), 'Generic OCI executor.\n');

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /emlotech-client\.md: prohibited EMLO adopter name in path/);
});

test('fails closed on NUL-containing changed files instead of skipping them', async () => {
  const directory = await repository();
  await writeFile(path.join(directory, 'opaque.bin'), Buffer.from([0x6e, 0x65, 0x75, 0x74, 0x72, 0x61, 0x6c, 0]));

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /refusing to skip NUL-containing changed file: opaque\.bin/);
});

test('fails closed when a required release artifact is missing', async () => {
  const directory = await repository();
  await rm(path.join(directory, 'docs', 'api', 'operator-control-plane-api.md'));

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(
    result.stderr,
    /required release artifact is missing or is not a regular file: docs\/api\/operator-control-plane-api\.md/
  );
});

test('fails closed when the base ref does not resolve', async () => {
  const directory = await repository();

  const result = await run(directory, ['--base', 'refs/remotes/fork/missing']);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /base ref does not resolve to a commit: refs\/remotes\/fork\/missing/);
});

test('rejects shell syntax in a base ref without executing it', async () => {
  const directory = await repository();
  const sentinel = path.join(directory, 'shell-injection-sentinel');

  const result = await run(directory, ['--base', 'HEAD;touch shell-injection-sentinel']);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /--base must be a safe Git ref or full commit ID/);
  await assert.rejects(access(sentinel));
});

test('scans only added lines in an existing tracked file', async () => {
  const directory = await repository();
  await writeFile(path.join(directory, 'history.md'), 'Historic EMLO wording.\n');
  await git(directory, 'add', 'history.md');
  await git(directory, 'commit', '--quiet', '-m', 'historic policy');
  await writeFile(path.join(directory, 'history.md'), 'Historic EMLO wording.\nNew neutral wording.\n');

  const result = await run(directory);

  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, /Adopter-term scan passed: 1 file scanned/);
});

test('fails closed on symbolic links instead of following them outside the repository', async (t) => {
  const directory = await repository();
  const outside = path.join(directory, '..', `${path.basename(directory)}-outside.md`);
  await writeFile(outside, 'neutral\n');
  try {
    await import('node:fs/promises').then(({symlink}) => symlink(outside, path.join(directory, 'linked.md')));
  } catch (error) {
    if (error.code === 'EPERM') {
      t.skip('symbolic links are not available in this Windows environment');
      return;
    }
    throw error;
  }

  const result = await run(directory);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /refusing to scan symbolic link: linked\.md/);
});
