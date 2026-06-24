#!/usr/bin/env node

import {spawn, spawnSync} from 'node:child_process';
import {randomBytes} from 'node:crypto';
import {existsSync} from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

const repoRoot = path.resolve(fileURLToPath(new URL('../../', import.meta.url)));
const demoComposeFile = path.join(repoRoot, 'examples/community-e2e/compose.yaml');
const demoComposeProject = process.env.DISTR_DEMO_COMPOSE_PROJECT ?? 'distr-community-e2e-demo';
const demoDatabaseURL = 'postgres://local:local@127.0.0.1:15432/distr_demo?sslmode=disable';
const localJwtSecret = 'bG9jYWwtand0LXNlY3JldC1wbGFjZWhvbGRlci0zMi1ieXRlcw==';
const args = new Set(process.argv.slice(2));
if (args.has('--help')) {
  console.log(`Usage:
  node examples/community-e2e/live-demo.mjs --start-local --cleanup
  node examples/community-e2e/live-demo.mjs --require-running-hub

Options:
  --start-local          Start isolated demo compose dependencies and a local Hub.
  --cleanup              Stop and remove only the isolated demo compose project after --start-local.
  --require-running-hub  Use an already running Hub at DISTR_HOST, default http://localhost:8080.

Environment:
  DISTR_DEMO_DISPOSABLE_HUB=true   Required for running-Hub mode against disposable infrastructure.
  DISTR_DEMO_ALLOW_SHARED_HUB=true Explicitly acknowledge mutation of a shared Hub.
  DISTR_DEMO_HOST=http://127.0.0.1:8080 Optional start-local client URL override.
  DISTR_DEMO_DATABASE_URL=...       Optional start-local database URL override; defaults to the isolated demo DB.
`);
  process.exit(0);
}
const startLocal = args.has('--start-local');
const cleanup = args.has('--cleanup');
const requireRunningHub = args.has('--require-running-hub') || !startLocal;
const host = (startLocal
  ? (process.env.DISTR_DEMO_HOST ?? 'http://127.0.0.1:8080')
  : (process.env.DISTR_HOST ?? 'http://localhost:8080')
).replace(/\/$/, '');
const databaseURL = startLocal ? (process.env.DISTR_DEMO_DATABASE_URL ?? demoDatabaseURL) : process.env.DATABASE_URL;
const disposableHub = startLocal || process.env.DISTR_DEMO_DISPOSABLE_HUB === 'true';
const sharedHubAllowed = process.env.DISTR_DEMO_ALLOW_SHARED_HUB === 'true';
if (requireRunningHub && !disposableHub && !sharedHubAllowed) {
  throw new Error(
    '--require-running-hub creates and soft-deletes demo data. Use --start-local, set DISTR_DEMO_DISPOSABLE_HUB=true for a disposable Hub, or set DISTR_DEMO_ALLOW_SHARED_HUB=true to acknowledge shared-Hub mutation.',
  );
}
const featureFlags = [
  'environments',
  'lifecycles',
  'channels',
  'release_bundles',
  'deployment_processes',
  'scoped_variables_v2',
  'deployment_plans',
  'task_queue',
  'agent_capabilities',
  'agent_task_leases',
  'step_events',
  'deployment_timeline',
  'config_as_code',
].join(',');

const runId = randomBytes(8).toString('hex');
const demoUserEmail = `community-${runId}@demo.test`;
const demoUserPassphrase = `Pr050-${randomBytes(24).toString('base64url')}!aA1`;
const demoName = `pr050-demo-${runId}`;

function run(command, commandArgs, options = {}) {
  const result = spawnSync(command, commandArgs, {
    cwd: repoRoot,
    env: {...process.env, ...options.env},
    encoding: 'utf8',
    stdio: options.capture ? 'pipe' : 'inherit',
    shell: process.platform === 'win32',
  });
  if (result.status !== 0) {
    const details = options.capture ? `\n${result.stdout}\n${result.stderr}` : '';
    throw new Error(`${command} ${commandArgs.join(' ')} failed with ${result.status}${details}`);
  }
  return result;
}

function dockerComposeArgs(...extra) {
  return ['compose', '-p', demoComposeProject, '-f', demoComposeFile, ...extra];
}

function assert(condition, message) {
  if (!condition) {
    throw new Error(`Assertion failed: ${message}`);
  }
}

function basicAuth(username, passphrase) {
  return `Basic ${Buffer.from(`${username}:${passphrase}`).toString('base64')}`;
}

async function apiRequest(method, requestPath, {body, token, basic, expected = [200]} = {}) {
  const headers = {'Content-Type': 'application/json'};
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }
  if (basic) {
    headers.Authorization = basic;
  }
  const response = await fetch(`${host}${requestPath}`, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!expected.includes(response.status)) {
    const text = await response.text();
    throw new Error(`${method} ${requestPath} returned ${response.status}: ${text.trim()}`);
  }
  if (response.status === 204) {
    return null;
  }
  const text = await response.text();
  return text ? JSON.parse(text) : null;
}

async function cleanupDemoOrganization(token) {
  const deleteResponse = await fetch(`${host}/api/v1/organization`, {
    method: 'DELETE',
    headers: {Authorization: `Bearer ${token}`},
  });
  if (![204, 404].includes(deleteResponse.status)) {
    const text = await deleteResponse.text();
    throw new Error(`DELETE /api/v1/organization cleanup returned ${deleteResponse.status}: ${text.trim()}`);
  }

  const verifyResponse = await fetch(`${host}/api/v1/organization`, {
    headers: {Authorization: `Bearer ${token}`},
  });
  if (![401, 403, 404].includes(verifyResponse.status)) {
    const text = await verifyResponse.text();
    throw new Error(`demo organization still accessible after cleanup: ${verifyResponse.status}: ${text.trim()}`);
  }
}

async function waitForReady(timeoutMs = 60_000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(`${host}/ready`);
      if (response.ok) {
        return;
      }
    } catch {
      // retry until deadline
    }
    await new Promise((resolve) => setTimeout(resolve, 1000));
  }
  throw new Error(`Hub did not become ready at ${host}`);
}

async function waitForDemoPostgres(timeoutMs = 60_000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      run('docker', dockerComposeArgs('exec', '-T', 'postgres', 'pg_isready', '-U', 'local', '-d', 'distr_demo'), {
        capture: true,
      });
      return;
    } catch {
      await new Promise((resolve) => setTimeout(resolve, 1000));
    }
  }
  throw new Error('Demo PostgreSQL did not become ready');
}

function hubCommand() {
  const configured = process.env.DISTR_HUB_BIN;
  if (configured) {
    return {command: configured, args: ['serve']};
  }
  const built = process.platform === 'win32' ? 'dist/distr.exe' : 'dist/distr-amd64';
  if (existsSync(path.join(repoRoot, built))) {
    return {command: path.join(repoRoot, built), args: ['serve']};
  }
  return {command: 'go', args: ['run', './cmd/hub', 'serve']};
}

function newUserPayload() {
  const payload = {name: 'Community E2E Demo', organizationName: demoName, email: demoUserEmail};
  payload['password'] = demoUserPassphrase;
  return payload;
}

function loginPayload() {
  const payload = {email: demoUserEmail};
  payload['password'] = demoUserPassphrase;
  return payload;
}

async function registerAndLoginDemoUser() {
  await apiRequest('POST', '/api/v1/auth/register', {body: newUserPayload()});
  const login = await apiRequest('POST', '/api/v1/auth/login', {body: loginPayload()});
  assert(login?.token, 'login response must include a bearer token');
  return login.token;
}

async function reportAgentCapabilities(target, targetAccess) {
  const login = await apiRequest('POST', '/api/v1/agent/login', {
    basic: basicAuth(target.id, targetAccess.targetSecret),
  });
  assert(login?.token, 'agent login response must include a bearer token');

  await apiRequest('POST', `/api/v1/agents/${target.id}/capabilities`, {
    token: login.token,
    body: {
      protocolVersion: 'v1',
      agentVersion: 'pr050-api-demo',
      supportedRuntimes: ['node'],
      supportedActions: [{actionType: 'distr.http.check', versions: ['1']}],
      operatingSystem: process.platform,
      architecture: process.arch,
      availableTooling: ['fetch'],
      strategyCapabilities: ['api-only-live-demo'],
    },
  });

  return login.token;
}

async function createReleaseTopology(token) {
  const app = await apiRequest('POST', '/api/v1/applications', {
    token,
    body: {
      name: `${demoName}-app`,
      type: 'docker',
    },
  });

  const target = await apiRequest('POST', '/api/v1/deployment-targets', {
    token,
    body: {
      name: `${demoName}-target`,
      type: 'docker',
      metricsEnabled: false,
      imageCleanupEnabled: false,
      autohealEnabled: false,
      deploymentLogsEnabled: false,
    },
  });
  const targetAccess = await apiRequest('POST', `/api/v1/deployment-targets/${target.id}/access-request`, {token});
  const agentToken = await reportAgentCapabilities(target, targetAccess);

  const environment = await apiRequest('POST', '/api/v1/environments', {
    token,
    body: {
      name: `${demoName}-dev`,
      description: 'PR-050 API-only live demo environment',
      sortOrder: 10,
      isProduction: false,
      allowDynamicTargets: false,
    },
  });

  const lifecycle = await apiRequest('POST', '/api/v1/lifecycles', {
    token,
    body: {
      name: `${demoName}-lifecycle`,
      description: 'PR-050 API-only live demo lifecycle',
      sortOrder: 10,
      phases: [
        {
          name: 'Development',
          description: 'Single live demo phase',
          sortOrder: 10,
          environmentIds: [environment.id],
          optional: false,
          automaticPromotion: false,
          minimumSuccessfulDeployments: 0,
        },
      ],
    },
  });

  const channel = await apiRequest('POST', '/api/v1/channels', {
    token,
    body: {
      applicationId: app.id,
      lifecycleId: lifecycle.id,
      name: `${demoName}-stable`,
      description: 'PR-050 API-only live demo channel',
      sortOrder: 10,
      isDefault: true,
      allowedVersionRanges: [],
      allowedPrereleasePatterns: [],
      allowedSourceBranches: [],
      allowedSourceTags: [],
    },
  });

  const processDefinition = await apiRequest('POST', '/api/v1/deployment-processes', {
    token,
    body: {
      applicationId: app.id,
      name: `${demoName}-process`,
      description: 'PR-050 API-only live demo process',
      sortOrder: 10,
    },
  });

  const revision = await apiRequest('POST', `/api/v1/deployment-processes/${processDefinition.id}/revisions`, {
    token,
    body: {
      description: 'Target agent executes a safe HTTP readiness check',
      steps: [
        {
          key: 'ready-check',
          name: 'Ready check',
          actionType: 'distr.http.check',
          executionLocation: 'target',
          inputBindings: {
            url: `${host}/ready`,
            method: 'GET',
            expectedStatusCodes: [200],
            maxLatencyMs: 10000,
          },
          channelIds: [channel.id],
          environmentIds: [environment.id],
          failureMode: 'fail',
          timeoutSeconds: 30,
          retryPolicy: {maxAttempts: 1, intervalSeconds: 1},
          sortOrder: 10,
        },
      ],
    },
  });

  const bundle = await apiRequest('POST', '/api/v1/release-bundles', {
    token,
    body: {
      applicationId: app.id,
      channelId: channel.id,
      deploymentProcessRevisionId: revision.id,
      releaseNumber: `pr050-${runId}`,
      releaseNotes: 'PR-050 API-only live release-to-task demo',
      sourceRevision: 'pr050-api-demo',
      components: [
        {
          key: 'demo-artifact',
          name: 'Demo artifact',
          type: 'external_artifact',
          version: '1.0.0',
          packageRef: 'demo://community-e2e',
          checksum: 'sha256:4f0c03dc2fb74fc9e0b6f3071d5c48f8477be35c0f14fd5e1cc3f3db4c3f48b4',
        },
      ],
    },
  });

  const published = await apiRequest('POST', `/api/v1/release-bundles/${bundle.id}/publish`, {token});

  return {
    app,
    target,
    environment,
    lifecycle,
    channel,
    processDefinition,
    revision,
    bundle: published,
    agentToken,
  };
}

async function executeHttpCheckStep(agentToken, target, lease) {
  assert(lease.steps?.length === 1, 'agent lease must include one executable step');
  const step = lease.steps[0];
  assert(step.actionType === 'distr.http.check', 'leased step must be an HTTP check');

  await apiRequest('POST', `/api/v1/agents/${target.id}/step-runs/${step.stepRunId}/events`, {
    token: agentToken,
    body: {
      leaseToken: lease.leaseToken,
      sequence: 1,
      type: 'STARTED',
      message: 'HTTP check started',
      progressPercent: 0,
      logs: [{stream: 'stdout', severity: 'info', body: `checking ${step.inputs.url}`}],
    },
  });

  const startedAt = Date.now();
  const response = await fetch(step.inputs.url, {method: step.inputs.method ?? 'GET'});
  const latencyMs = Date.now() - startedAt;
  const expected = step.inputs.expectedStatusCodes ?? [200];
  assert(expected.includes(response.status), `HTTP check expected ${expected.join(',')} but got ${response.status}`);

  await apiRequest('POST', `/api/v1/agents/${target.id}/step-runs/${step.stepRunId}/events`, {
    token: agentToken,
    body: {
      leaseToken: lease.leaseToken,
      sequence: 2,
      type: 'SUCCEEDED',
      message: `HTTP check returned ${response.status}`,
      progressPercent: 100,
      logs: [{stream: 'stdout', severity: 'info', body: `status=${response.status} latencyMs=${latencyMs}`}],
      outputs: [
        {name: 'passed', value: true},
        {name: 'statusCode', value: response.status},
        {name: 'latencyMs', value: latencyMs},
      ],
    },
  });

  return {step, statusCode: response.status};
}

async function runLiveReleaseToTaskJourney() {
  let token;
  let topology;

  try {
    token = await registerAndLoginDemoUser();
    topology = await createReleaseTopology(token);

    const plan = await apiRequest('POST', '/api/v1/deployment-plans', {
      token,
      body: {
        releaseBundleId: topology.bundle.id,
        environmentId: topology.environment.id,
        targetIds: [topology.target.id],
      },
    });
    assert(plan.status === 'READY', `deployment plan must be READY, got ${plan.status}`);
    assert(plan.steps?.length === 1, 'deployment plan must contain the target HTTP check step');

    const tasks = await apiRequest('POST', `/api/v1/deployment-plans/${plan.id}/tasks`, {token, body: {}});
    assert(tasks.length === 1, 'deployment plan must create one task');
    assert(tasks[0].status === 'QUEUED', `created task must be QUEUED, got ${tasks[0].status}`);

    const lease = await apiRequest('POST', `/api/v1/agents/${topology.target.id}/lease`, {
      token: topology.agentToken,
    });
    assert(lease?.taskId === tasks[0].id, 'agent lease must claim the created task');

    const actionResult = await executeHttpCheckStep(topology.agentToken, topology.target, lease);
    assert(actionResult.statusCode === 200, 'HTTP action must observe Hub readiness');

    const completedTask = await apiRequest('GET', `/api/v1/tasks/${tasks[0].id}`, {token});
    assert(completedTask.status === 'SUCCEEDED', `task must be SUCCEEDED, got ${completedTask.status}`);
    assert(completedTask.stepRuns?.[0]?.status === 'SUCCEEDED', 'step run must be SUCCEEDED');

    const timeline = await apiRequest('GET', `/api/v1/tasks/${tasks[0].id}/timeline`, {token});
    assert(timeline.taskId === tasks[0].id, 'task timeline must reference the created task');
    assert(timeline.events?.length === 2, 'task timeline must include STARTED and SUCCEEDED events');
    assert(timeline.events[0].type === 'STARTED', 'first timeline event must be STARTED');
    assert(timeline.events[1].type === 'SUCCEEDED', 'second timeline event must be SUCCEEDED');

    const logs = await apiRequest('GET', `/api/v1/tasks/${tasks[0].id}/logs`, {token});
    assert(logs.length === 2, 'task logs must include both action log chunks');
    assert(logs.some((log) => log.body.includes('status=200')), 'task logs must include HTTP status proof');

    const deploymentTimeline = await apiRequest(
      'GET',
      `/api/v1/deployment-timeline?applicationId=${topology.app.id}`,
      {token},
    );
    assert(
      deploymentTimeline.items?.some((item) => item.taskId === tasks[0].id),
      'deployment timeline must include the created task',
    );

    console.log(`Live release-to-task journey passed for task ${tasks[0].id}`);
  } finally {
    if (token) {
      await cleanupDemoOrganization(token);
    }
  }

}
let hubProcess;

try {
  if (startLocal) {
    console.log('Starting isolated demo dependencies');
    run('docker', dockerComposeArgs('up', '-d', 'postgres', 'mailpit', 'storage'));
    await waitForDemoPostgres();

    console.log('Starting local Hub');
    const hub = hubCommand();
    hubProcess = spawn(hub.command, hub.args, {
      cwd: repoRoot,
      env: {
        ...process.env,
        DATABASE_URL: databaseURL,
        JWT_SECRET: process.env.JWT_SECRET ?? localJwtSecret,
        DISTR_HOST: host,
        USER_EMAIL_VERIFICATION_REQUIRED: 'false',
        DISTR_EXPERIMENTAL_FEATURE_FLAGS: process.env.DISTR_EXPERIMENTAL_FEATURE_FLAGS ?? featureFlags,
      },
      stdio: 'inherit',
      shell: process.platform === 'win32',
    });
  }

  if (requireRunningHub || startLocal) {
    console.log(`Waiting for Hub at ${host}`);
    await waitForReady();
  }

  console.log('Running live Hub API smoke journey');
  run(process.execPath, ['hack/e2e-smoke-test.mjs'], {
    env: {
      DISTR_HOST: host,
    },
  });

  console.log('Running API-only live release-to-task journey');
  await runLiveReleaseToTaskJourney();

  console.log('Running deterministic advanced release-to-task verifier');
  run(process.execPath, ['examples/community-e2e/run-demo.mjs']);

  console.log('Community live demo passed');
} finally {
  if (hubProcess) {
    hubProcess.kill('SIGTERM');
  }
  if (cleanup && startLocal) {
    run('docker', dockerComposeArgs('down', '-v', '--remove-orphans'), {capture: false});
  }
}
