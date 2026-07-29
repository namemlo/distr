import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import {createHash, randomBytes} from 'node:crypto';
import {copyFile, mkdir, readFile, rm, writeFile} from 'node:fs/promises';
import path from 'node:path';
import test from 'node:test';
import {fileURLToPath} from 'node:url';

const repositoryRoot = fileURLToPath(new URL('..', import.meta.url));
const script = path.join(repositoryRoot, 'hack', 'control-plane-migration-matrix.ps1');
const pwsh = process.platform === 'win32' ? 'pwsh.exe' : 'pwsh';
const safeURL = 'postgres://matrix_user:matrix-secret@127.0.0.1:5432/control_plane_test?sslmode=disable';
const passwordlessSafeURL =
  'postgres://matrix_user@127.0.0.1:5432/control_plane_test?sslmode=disable';

function runScript(args, env = {}) {
  return spawnSync(pwsh, ['-NoLogo', '-NoProfile', '-NonInteractive', '-File', script, ...args], {
    cwd: repositoryRoot,
    encoding: 'utf8',
    env: {...process.env, DATABASE_URL: '', DISTR_TEST_DATABASE_URL: '', ...env},
  });
}

function runScriptWithAbsentCallerJwt(args, env = {}) {
  const quote = (value) => `'${String(value).replaceAll("'", "''")}'`;
  const invocation = args
    .map((value) => (String(value).startsWith('-') ? String(value) : quote(value)))
    .join(' ');
  const command = `
[Environment]::SetEnvironmentVariable('JWT_SECRET', $null, 'Process')
& ${quote(script)} ${invocation}
$scriptSucceeded = $?
if (-not $scriptSucceeded) { exit 92 }
if (-not [string]::IsNullOrEmpty([Environment]::GetEnvironmentVariable('JWT_SECRET', 'Process'))) {
  [Console]::Error.WriteLine('JWT_SECRET was not restored to its absent caller state')
  exit 91
}
`;
  const childEnvironment = {
    ...process.env,
    DATABASE_URL: '',
    DISTR_TEST_DATABASE_URL: '',
    ...env,
  };
  delete childEnvironment.JWT_SECRET;
  return spawnSync(
    pwsh,
    [
      '-NoLogo',
      '-NoProfile',
      '-NonInteractive',
      '-EncodedCommand',
      Buffer.from(command, 'utf16le').toString('base64'),
    ],
    {
      cwd: repositoryRoot,
      encoding: 'utf8',
      env: childEnvironment,
    }
  );
}

function runScriptWithCallerJwt(args, callerJwt, env = {}) {
  const quote = (value) => `'${String(value).replaceAll("'", "''")}'`;
  const invocation = args
    .map((value) => (String(value).startsWith('-') ? String(value) : quote(value)))
    .join(' ');
  const command = `
$callerJwt = [Environment]::GetEnvironmentVariable('JWT_SECRET', 'Process')
& ${quote(script)} ${invocation}
$scriptSucceeded = $?
if (-not $scriptSucceeded) { exit 92 }
if ($callerJwt -cne [Environment]::GetEnvironmentVariable('JWT_SECRET', 'Process')) {
  [Console]::Error.WriteLine('JWT_SECRET was not restored to its exact caller value')
  exit 91
}
`;
  return spawnSync(
    pwsh,
    [
      '-NoLogo',
      '-NoProfile',
      '-NonInteractive',
      '-EncodedCommand',
      Buffer.from(command, 'utf16le').toString('base64'),
    ],
    {
      cwd: repositoryRoot,
      encoding: 'utf8',
      env: {
        ...process.env,
        DATABASE_URL: '',
        DISTR_TEST_DATABASE_URL: '',
        ...env,
        JWT_SECRET: callerJwt,
      },
    }
  );
}

function sha256(text) {
  return `sha256:${createHash('sha256').update(text).digest('hex')}`;
}

async function createFakeToolchain({postgresVersion = '18.4', goOutput = 'go-ok', goExitCode = 0} = {}) {
  const directory = path.join(repositoryRoot, 'work', `matrix-fake-tools-${process.pid}-${Date.now()}`);
  await mkdir(directory, {recursive: true});
  if (process.platform === 'win32') {
    const source = path.join(directory, 'fake-tool.go');
    const goExecutable = path.join(directory, 'go.exe');
    await writeFile(
      source,
      `package main
import (
  "crypto/sha256"
  "fmt"
  "os"
  "path/filepath"
  "strconv"
  "strings"
)
func main() {
  if strings.HasPrefix(strings.ToLower(filepath.Base(os.Args[0])), "psql") {
    fmt.Println(os.Getenv("MATRIX_FAKE_POSTGRES_VERSION"))
    return
  }
  if os.Getenv("MATRIX_FAKE_INSPECT_JWT") == "true" {
    jwtSecret := os.Getenv("JWT_SECRET")
    fingerprint := sha256.Sum256([]byte(jwtSecret))
    fmt.Printf(
      "jwt-present=%t jwt-length=%d jwt-sha256=%x JWT_SECRET=%s\\n",
      jwtSecret != "",
      len(jwtSecret),
      fingerprint,
      jwtSecret,
    )
    return
  }
  if expectedCallerFingerprint := os.Getenv("MATRIX_FAKE_CALLER_JWT_SHA256"); expectedCallerFingerprint != "" {
    jwtSecret := os.Getenv("JWT_SECRET")
    fingerprint := sha256.Sum256([]byte(jwtSecret))
    fmt.Printf(
      "jwt-present=%t jwt-length=%d jwt-differs-from-caller=%t\\n",
      jwtSecret != "",
      len(jwtSecret),
      fmt.Sprintf("%x", fingerprint) != expectedCallerFingerprint,
    )
    return
  }
  fmt.Println(os.Getenv("MATRIX_FAKE_GO_OUTPUT"))
  exitCode, _ := strconv.Atoi(os.Getenv("MATRIX_FAKE_GO_EXIT_CODE"))
  os.Exit(exitCode)
}
`
    );
    const built = spawnSync('go', ['build', '-o', goExecutable, source], {
      cwd: repositoryRoot,
      encoding: 'utf8',
    });
    assert.equal(built.status, 0, built.stderr || built.stdout);
    await copyFile(goExecutable, path.join(directory, 'psql.exe'));
  } else {
    await Promise.all([
      writeFile(path.join(directory, 'psql'), '#!/bin/sh\nprintf \'%s\\n\' "$MATRIX_FAKE_POSTGRES_VERSION"\n', {
        mode: 0o755,
      }),
      writeFile(
        path.join(directory, 'go'),
        '#!/bin/sh\nprintf \'%s\\n\' "$MATRIX_FAKE_GO_OUTPUT"\nexit "$MATRIX_FAKE_GO_EXIT_CODE"\n',
        {mode: 0o755}
      ),
    ]);
  }
  return {
    directory,
    environment: {
      MATRIX_FAKE_POSTGRES_VERSION: postgresVersion,
      MATRIX_FAKE_GO_OUTPUT: goOutput,
      MATRIX_FAKE_GO_EXIT_CODE: String(goExitCode),
    },
  };
}

test('plan-only report is checksummed, redacted, bounded, and makes no database attempt', async () => {
  const output = path.join(repositoryRoot, 'work', `matrix-test-${process.pid}-${Date.now()}.json`);
  try {
    const result = runScript([
      '-FromMigration',
      '138',
      '-ToMigration',
      '162',
      '-DatabaseUrl',
      safeURL,
      '-OutputPath',
      path.relative(repositoryRoot, output),
      '-PlanOnly',
    ]);

    assert.equal(result.status, 0, result.stderr || result.stdout);
    const raw = await readFile(output, 'utf8');
    assert.ok(!raw.includes('matrix-secret'));
    assert.ok(!raw.includes(safeURL));
    const report = JSON.parse(raw);
    assert.equal(report.schemaVersion, 'distr.control-plane-migration-matrix-report/v1');
    assert.equal(report.status, 'PLANNED');
    assert.equal(report.planOnly, true);
    assert.deepEqual(report.range, {from: 138, to: 162});
    assert.equal(report.database.passwordPresent, true);
    assert.equal(report.database.expectedServerVersion, '18.4');
    assert.equal(report.migrationFiles.length, 25);
    assert.equal(report.scenarios[0].id, 'migration-file-integrity');
    assert.equal(report.scenarios[0].status, 'PASS');
    assert.equal(report.scenarios.slice(1).length, 9);
    assert.ok(report.scenarios.slice(1).every(({status}) => status === 'PLANNED'));
    assert.deepEqual(report.coverage, {
      schemaUpgrade: {from: 138, to: 162},
      schemaDown: {mode: 'single-step', from: 162, to: 161},
      checkpoint: 'idempotency-and-cursor-resume-tests',
      notExecuted: ['process-interruption-and-restart', 'binary-rollback'],
    });
    assert.deepEqual(report.integrity, {
      algorithm: 'sha256',
      encoding: 'utf8',
      serialization: 'compact-json-preserving-property-order',
      scope: 'complete-report-excluding-reportChecksum',
      commandEvidence: 'complete-redacted-output',
    });
    assert.deepEqual(report.cleanup, {attemptedSchemas: 0, droppedSchemas: 0, complete: true, checks: []});

    const {reportChecksum, ...withoutChecksum} = report;
    assert.equal(reportChecksum, sha256(JSON.stringify(withoutChecksum)));

    const overwrite = runScript([
      '-DatabaseUrl',
      safeURL,
      '-OutputPath',
      path.relative(repositoryRoot, output),
      '-PlanOnly',
    ]);
    assert.notEqual(overwrite.status, 0);
    assert.match(`${overwrite.stdout}${overwrite.stderr}`, /reports are never overwritten/i);
    assert.equal(await readFile(output, 'utf8'), raw);
  } finally {
    await rm(output, {force: true});
  }
});

test('plan-only report supports both certified PostgreSQL versions without execution', async () => {
  for (const version of ['16.14', '18.4']) {
    const output = path.join(repositoryRoot, 'work', `matrix-version-${version}-${process.pid}-${Date.now()}.json`);
    try {
      const result = runScript([
        '-DatabaseUrl',
        safeURL,
        '-ExpectedPostgresVersion',
        version,
        '-OutputPath',
        path.relative(repositoryRoot, output),
        '-PlanOnly',
      ]);
      assert.equal(result.status, 0, result.stderr || result.stdout);
      const report = JSON.parse(await readFile(output, 'utf8'));
      assert.equal(report.database.expectedServerVersion, version);
      assert.equal(report.scenarios.find(({id}) => id === 'postgres-runtime-version').status, 'PLANNED');
    } finally {
      await rm(output, {force: true});
    }
  }
});

test('non-plan report retains complete redacted command output and its checksum', async () => {
  const marker = 'z'.repeat(6000);
  const fakeTools = await createFakeToolchain({
    goOutput: `postgres://matrix_user:do-not-retain@127.0.0.1/control_plane token=do-not-retain ${marker}`,
  });
  const output = path.join(repositoryRoot, 'work', `matrix-evidence-${process.pid}-${Date.now()}.json`);
  try {
    const result = runScript(['-DatabaseUrl', safeURL, '-OutputPath', path.relative(repositoryRoot, output)], {
      ...fakeTools.environment,
      PATH: `${fakeTools.directory}${path.delimiter}${process.env.PATH ?? ''}`,
    });
    const raw = await readFile(output, 'utf8');
    assert.ok(!raw.includes('do-not-retain'));
    const report = JSON.parse(raw);
    assert.equal(
      result.status,
      0,
      JSON.stringify(
        report.scenarios.filter(({status}) => status !== 'PASS'),
        null,
        2
      )
    );
    const command = report.scenarios
      .flatMap(({checks}) => checks)
      .find(({description}) => description.startsWith('exercise v1 behavior'));
    assert.ok(command.output.length > 6000);
    assert.ok(command.output.includes(marker));
    assert.equal(command.outputSha256, sha256(command.output));
    assert.ok(command.diagnostic.length < command.output.length);
    assert.equal(report.cleanup.checks.length, report.cleanup.attemptedSchemas);
    assert.ok(report.cleanup.checks.every(({exitCode}) => exitCode === 0));
  } finally {
    await rm(output, {force: true});
    await rm(fakeTools.directory, {recursive: true, force: true});
  }
});

test('non-plan report supports an isolated passwordless test database', async () => {
  const fakeTools = await createFakeToolchain();
  const output = path.join(
    repositoryRoot,
    'work',
    `matrix-passwordless-${process.pid}-${Date.now()}.json`
  );
  try {
    const result = runScript(
      ['-DatabaseUrl', passwordlessSafeURL, '-OutputPath', path.relative(repositoryRoot, output)],
      {
        ...fakeTools.environment,
        PATH: `${fakeTools.directory}${path.delimiter}${process.env.PATH ?? ''}`,
      }
    );
    assert.equal(result.status, 0, result.stderr || result.stdout);
    const report = JSON.parse(await readFile(output, 'utf8'));
    assert.equal(report.status, 'PASS');
    assert.equal(report.database.passwordPresent, false);
  } finally {
    await rm(output, {force: true});
    await rm(fakeTools.directory, {recursive: true, force: true});
  }
});

test('non-plan execution injects a fresh ephemeral JWT secret without retaining its value', async () => {
  const fakeTools = await createFakeToolchain();
  const reports = [
    path.join(repositoryRoot, 'work', `matrix-jwt-first-${process.pid}-${Date.now()}.json`),
    path.join(repositoryRoot, 'work', `matrix-jwt-second-${process.pid}-${Date.now()}.json`),
  ];
  try {
    const fingerprints = [];
    for (const output of reports) {
      const result = runScript(['-DatabaseUrl', safeURL, '-OutputPath', path.relative(repositoryRoot, output)], {
        ...fakeTools.environment,
        MATRIX_FAKE_INSPECT_JWT: 'true',
        PATH: `${fakeTools.directory}${path.delimiter}${process.env.PATH ?? ''}`,
        JWT_SECRET: '',
      });
      assert.equal(result.status, 0, result.stderr || result.stdout);
      const raw = await readFile(output, 'utf8');
      const report = JSON.parse(raw);
      const commandOutput = report.scenarios
        .flatMap(({checks}) => checks)
        .find(({output: retainedOutput}) => retainedOutput?.includes('jwt-present='))
        ?.output;
      assert.match(commandOutput ?? '', /jwt-present=true jwt-length=64 jwt-sha256=[0-9a-f]{64}/);
      assert.match(commandOutput ?? '', /JWT_SECRET=\[REDACTED_SECRET\]/);
      const fingerprint = commandOutput.match(/jwt-sha256=([0-9a-f]{64})/)?.[1];
      assert.ok(fingerprint);
      fingerprints.push(fingerprint);
      assert.ok(!raw.includes('JWT_SECRET=') || raw.includes('JWT_SECRET=[REDACTED_SECRET]'));
    }
    assert.notEqual(fingerprints[0], fingerprints[1]);
  } finally {
    await Promise.all(reports.map((output) => rm(output, {force: true})));
    await rm(fakeTools.directory, {recursive: true, force: true});
  }
});

test('non-plan execution restores an absent caller JWT_SECRET after matrix commands', async () => {
  const fakeTools = await createFakeToolchain();
  const output = path.join(repositoryRoot, 'work', `matrix-jwt-restore-${process.pid}-${Date.now()}.json`);
  try {
    const result = runScriptWithAbsentCallerJwt(
      ['-DatabaseUrl', safeURL, '-OutputPath', path.relative(repositoryRoot, output)],
      {
        ...fakeTools.environment,
        MATRIX_FAKE_INSPECT_JWT: 'true',
        PATH: `${fakeTools.directory}${path.delimiter}${process.env.PATH ?? ''}`,
      }
    );
    assert.equal(result.status, 0, result.stderr || result.stdout);
    const report = JSON.parse(await readFile(output, 'utf8'));
    const retainedOutput = report.scenarios
      .flatMap(({checks}) => checks)
      .find(({output: commandOutput}) => commandOutput?.includes('jwt-present='))
      ?.output;
    assert.match(retainedOutput ?? '', /jwt-present=true/);
  } finally {
    await rm(output, {force: true});
    await rm(fakeTools.directory, {recursive: true, force: true});
  }
});

test('non-plan execution replaces a caller JWT_SECRET for children and restores it afterward', async () => {
  const fakeTools = await createFakeToolchain();
  const output = path.join(repositoryRoot, 'work', `matrix-jwt-caller-${process.pid}-${Date.now()}.json`);
  const callerJwt = randomBytes(48).toString('base64');
  try {
    const result = runScriptWithCallerJwt(
      ['-DatabaseUrl', safeURL, '-OutputPath', path.relative(repositoryRoot, output)],
      callerJwt,
      {
        ...fakeTools.environment,
        MATRIX_FAKE_CALLER_JWT_SHA256: createHash('sha256').update(callerJwt).digest('hex'),
        PATH: `${fakeTools.directory}${path.delimiter}${process.env.PATH ?? ''}`,
      }
    );
    assert.equal(result.status, 0, result.stderr || result.stdout);
    const raw = await readFile(output, 'utf8');
    assert.ok(!raw.includes(callerJwt));
    const retainedOutputs = JSON.parse(raw).scenarios
      .flatMap(({checks}) => checks.map(({output: commandOutput}) => commandOutput))
      .filter((commandOutput) => typeof commandOutput === 'string' && commandOutput.includes('jwt-present='));
    assert.ok(retainedOutputs.length > 0);
    assert.ok(
      retainedOutputs.every((commandOutput) =>
        /jwt-present=true jwt-length=64 jwt-differs-from-caller=true/.test(commandOutput)
      )
    );
  } finally {
    await rm(output, {force: true});
    await rm(fakeTools.directory, {recursive: true, force: true});
  }
});

test('failed commands retain complete redacted evidence before the scenario fails', async () => {
  const marker = 'f'.repeat(6000);
  const fakeTools = await createFakeToolchain({
    goOutput: `secret=do-not-retain ${marker}`,
    goExitCode: 7,
  });
  const output = path.join(repositoryRoot, 'work', `matrix-failed-evidence-${process.pid}-${Date.now()}.json`);
  try {
    const result = runScript(['-DatabaseUrl', safeURL, '-OutputPath', path.relative(repositoryRoot, output)], {
      ...fakeTools.environment,
      PATH: `${fakeTools.directory}${path.delimiter}${process.env.PATH ?? ''}`,
    });
    assert.notEqual(result.status, 0);
    const raw = await readFile(output, 'utf8');
    assert.ok(!raw.includes('do-not-retain'));
    const report = JSON.parse(raw);
    const scenario = report.scenarios.find(({id}) => id === 'migration-138-to-162-upgrade');
    assert.equal(scenario.status, 'FAIL');
    const failedCheck = scenario.checks.find(({exitCode}) => exitCode === 7);
    assert.ok(failedCheck.output.includes(marker));
    assert.equal(failedCheck.outputSha256, sha256(failedCheck.output));
    assert.ok(failedCheck.diagnostic.length < failedCheck.output.length);
  } finally {
    await rm(output, {force: true});
    await rm(fakeTools.directory, {recursive: true, force: true});
  }
});

test('non-plan report fails when PostgreSQL runtime differs from the expected version', async () => {
  const fakeTools = await createFakeToolchain({postgresVersion: '16.14'});
  const output = path.join(repositoryRoot, 'work', `matrix-version-mismatch-${process.pid}-${Date.now()}.json`);
  try {
    const result = runScript(
      [
        '-DatabaseUrl',
        safeURL,
        '-ExpectedPostgresVersion',
        '18.4',
        '-OutputPath',
        path.relative(repositoryRoot, output),
      ],
      {
        ...fakeTools.environment,
        PATH: `${fakeTools.directory}${path.delimiter}${process.env.PATH ?? ''}`,
      }
    );
    assert.notEqual(result.status, 0);
    const report = JSON.parse(await readFile(output, 'utf8'));
    assert.equal(report.status, 'FAIL');
    const versionScenario = report.scenarios.find(({id}) => id === 'postgres-runtime-version');
    assert.equal(versionScenario.status, 'FAIL');
    assert.match(versionScenario.diagnostic, /expected PostgreSQL 18\.4 but found 16\.14/);
  } finally {
    await rm(output, {force: true});
    await rm(fakeTools.directory, {recursive: true, force: true});
  }
});

test('unsafe database URLs fail before an evidence report or external command', () => {
  const cases = [
    'postgres://matrix_user:do-not-print@database.example.invalid:5432/control_plane_test',
    'postgres://matrix_user:do-not-print@127.0.0.1:5432/production',
    'https://matrix_user:do-not-print@127.0.0.1:5432/control_plane_test',
    'postgres://matrix_user:do-not-print@127.0.0.1:5432/control_plane_test?search_path=public',
  ];

  for (const [index, url] of cases.entries()) {
    const result = runScript([
      '-DatabaseUrl',
      url,
      '-OutputPath',
      `work/matrix-unsafe-${process.pid}-${index}.json`,
      '-PlanOnly',
    ]);
    assert.notEqual(result.status, 0);
    assert.ok(!`${result.stdout}${result.stderr}`.includes('do-not-print'));
  }

  const outsideWork = runScript([
    '-DatabaseUrl',
    safeURL,
    '-OutputPath',
    'migration-matrix-outside-work.json',
    '-PlanOnly',
  ]);
  assert.notEqual(outsideWork.status, 0);
  assert.match(`${outsideWork.stdout}${outsideWork.stderr}`, /must stay below the repository work directory/i);
});

test('missing migration pairs and reversed ranges fail closed in plan mode', () => {
  const reversed = runScript([
    '-FromMigration',
    '162',
    '-ToMigration',
    '138',
    '-DatabaseUrl',
    safeURL,
    '-OutputPath',
    `work/matrix-reversed-${process.pid}.json`,
    '-PlanOnly',
  ]);
  assert.notEqual(reversed.status, 0);

  const missing = runScript([
    '-FromMigration',
    '138',
    '-ToMigration',
    '163',
    '-DatabaseUrl',
    safeURL,
    '-OutputPath',
    `work/matrix-missing-${process.pid}.json`,
    '-PlanOnly',
  ]);
  assert.notEqual(missing.status, 0);
  assert.match(`${missing.stdout}${missing.stderr}`, /migration 163 must have exactly one up and one down file/i);
});

test('script source keeps destructive execution scoped and secret-safe', async () => {
  const source = await readFile(script, 'utf8');
  assert.doesNotMatch(source, /Invoke-Expression/i);
  assert.match(source, /DatabaseUrl must use localhost or a loopback IP address/);
  assert.match(source, /OutputPath already exists; evidence reports are never overwritten/);
  assert.match(source, /DROP SCHEMA IF EXISTS/);
  assert.match(source, /DISTR_TEST_DATABASE_URL/);
  assert.match(source, /DISTR_EXPERIMENTAL_FEATURE_FLAGS/);
  assert.match(source, /reportChecksum/);
  for (const scenario of [
    'migration-138-to-162-upgrade',
    'clean-install',
    'postgres-runtime-version',
    'single-step-down-and-refusal-contracts',
    'checkpoint-idempotency-and-cursor-resume',
    'v1-flags-off',
    'mixed-v1-v2',
    'v2-history-flags-off',
    'upstream-compatibility',
  ]) {
    assert.ok(source.includes(scenario), `missing scenario ${scenario}`);
  }
});
