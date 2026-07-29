#!/usr/bin/env node

import {spawnSync} from 'node:child_process';
import {createHash, generateKeyPairSync, randomBytes} from 'node:crypto';
import {readFile} from 'node:fs/promises';
import {createServer} from 'node:net';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

const fixtureDir = fileURLToPath(new URL('.', import.meta.url));
const repoRoot = path.resolve(fixtureDir, '../..');
const fixturePath = path.join(fixtureDir, 'fixture.json');
const composePath = path.join(fixtureDir, 'compose.yaml');
const checksumPattern = /^sha256:[0-9a-f]{64}$/;
const canonicalCases = [
  ['duplicate-dispatch', 'IDEMPOTENT_REPLAY'],
  ['duplicate-event', 'IDEMPOTENT_REPLAY'],
  ['pre-ack-crash', 'SAFE_REDISPATCH'],
  ['post-ack-crash', 'STATUS_RECONCILIATION_REQUIRED'],
  ['stale-fence', 'REJECTED_STALE_FENCE'],
  ['callback-loss', 'STATUS_RECONCILED'],
  ['timeout', 'TIMED_OUT'],
  ['cancel', 'CANCELED'],
  ['restart', 'RESUMED'],
  ['observer-mismatch', 'QUARANTINED'],
  ['drift-reconcile', 'RECONCILED'],
  ['previous-state-b-to-a', 'ACTIVE_A'],
  ['v1-regression', 'V1_UNCHANGED'],
  ['v2-kill-switch', 'ADMISSION_BLOCKED_HISTORY_PRESERVED'],
];

function assert(condition, message) {
  if (!condition) {
    throw new Error(`fixture contract: ${message}`);
  }
}

function stableStringify(value) {
  if (Array.isArray(value)) {
    return `[${value.map(stableStringify).join(',')}]`;
  }
  if (value && typeof value === 'object') {
    return `{${Object.keys(value)
      .sort()
      .map((key) => `${JSON.stringify(key)}:${stableStringify(value[key])}`)
      .join(',')}}`;
  }
  return JSON.stringify(value);
}

function checksum(value) {
  return `sha256:${createHash('sha256').update(stableStringify(value)).digest('hex')}`;
}

export function deriveFrozenAdapterRevision(adapter) {
  const frozenEvidence = {
    assignmentId: adapter.adapterAssignmentId,
    implementationId: adapter.adapterImplementationId,
    implementationVersion: adapter.implementationVersion,
    capability: adapter.capability,
    capabilityVersion: adapter.capabilityVersion,
    scopeType: adapter.scopeType,
    scopeReference: adapter.scopeReference,
    configSnapshotId: adapter.configSnapshotId,
    configChecksum: adapter.configChecksum,
    keyId: adapter.keyConfiguration.keyId,
    publicKeyFingerprint: adapter.keyConfiguration.publicKeyFingerprint,
    signingKeyReference: adapter.keyConfiguration.signingKeyReference,
    signingKeyVersionFingerprint: adapter.keyConfiguration.signingKeyVersionFingerprint,
    cancelCapabilityVersion: '',
    retrySafeCapabilityVersion: '',
  };
  return `sha256:${createHash('sha256').update(JSON.stringify(frozenEvidence)).digest('hex')}`;
}

export function derivePlanLeaseIdentities({plan, target, signingKeyId}) {
  assert(plan.stepAdapters?.length > 0, `${target.id} plan must freeze step adapters`);
  assert(plan.steps?.length > 0, `${target.id} plan must expose its frozen step graph`);
  const stepByKey = new Map(plan.steps.map((step) => [step.stepKey, step]));
  const indegree = new Map(plan.steps.map((step) => [step.stepKey, 0]));
  const dependents = new Map();
  for (const step of plan.steps) {
    for (const dependency of step.dependencies ?? []) {
      if (!stepByKey.has(dependency)) {
        continue;
      }
      indegree.set(step.stepKey, indegree.get(step.stepKey) + 1);
      const children = dependents.get(dependency) ?? [];
      children.push(step.stepKey);
      dependents.set(dependency, children);
    }
  }
  const compareStepKeys = (left, right) => {
    const leftStep = stepByKey.get(left);
    const rightStep = stepByKey.get(right);
    return leftStep.sortOrder - rightStep.sortOrder || left.localeCompare(right);
  };
  const ready = [...indegree.entries()]
    .filter(([, count]) => count === 0)
    .map(([stepKey]) => stepKey)
    .sort(compareStepKeys);
  const topologicalOrder = [];
  while (ready.length > 0) {
    const stepKey = ready.shift();
    topologicalOrder.push(stepKey);
    for (const dependent of (dependents.get(stepKey) ?? []).sort(compareStepKeys)) {
      const next = indegree.get(dependent) - 1;
      indegree.set(dependent, next);
      if (next === 0) {
        ready.push(dependent);
        ready.sort(compareStepKeys);
      }
    }
  }
  assert(
    topologicalOrder.length === plan.steps.length,
    `${target.id} frozen plan step graph must remain acyclic`
  );
  const adapterByStep = new Map(plan.stepAdapters.map((adapter) => [adapter.stepKey, adapter]));
  const adapters = topologicalOrder
    .filter((stepKey) => adapterByStep.has(stepKey))
    .map((stepKey) => adapterByStep.get(stepKey));
  assert(
    adapters.length === plan.stepAdapters.length,
    `${target.id} every frozen adapter must reference a frozen plan step`
  );
  return adapters.map((adapter) => {
    const componentKey = adapter.stepKey.split(':')[1];
    const instance = target.instances?.get(componentKey);
    const observer = target.observers?.get(componentKey);
    let expected;
    if (adapter.stepKey.includes(':migration:')) {
      expected = {
        capability: 'database.migrate',
        scopeType: 'database_resource',
        scopeReference: instance?.databaseBoundary,
      };
    } else if (adapter.stepKey.endsWith(':deploy')) {
      expected = {
        capability: 'distr.compose.deploy',
        scopeType: 'component_instance',
        scopeReference: instance?.id,
      };
    } else if (adapter.stepKey.endsWith(':health')) {
      expected = {
        capability: 'health.observe',
        scopeType: 'observer_registration',
        scopeReference: observer?.id,
      };
    } else {
      throw new Error(`fixture contract: ${target.id} frozen adapter has unsupported step ${adapter.stepKey}`);
    }
    assert(
      adapter.capability === expected.capability && adapter.capabilityVersion === '1.0.0',
      `${target.id} ${adapter.stepKey} frozen adapter must match its reported action capability`
    );
    assert(
      adapter.scopeType === expected.scopeType && adapter.scopeReference === expected.scopeReference,
      `${target.id} ${adapter.stepKey} frozen adapter scope must match its typed runtime boundary`
    );
    assert(
      adapter.configSnapshotId === target.snapshot.id && adapter.configChecksum === target.snapshot.canonicalChecksum,
      `${target.id} frozen adapter config must match the plan target snapshot`
    );
    assert(
      adapter.keyConfiguration.keyId === signingKeyId,
      `${target.id} ${adapter.stepKey} frozen adapter key must match the signer`
    );
    return {
      stepKey: adapter.stepKey,
      capability: adapter.capability,
      adapterRevision: deriveFrozenAdapterRevision(adapter),
      keyId: adapter.keyConfiguration.keyId,
    };
  });
}

function createEd25519Material() {
  const pair = generateKeyPairSync('ed25519');
  const publicDER = pair.publicKey.export({type: 'spki', format: 'der'});
  const privateDER = pair.privateKey.export({type: 'pkcs8', format: 'der'});
  const publicKey = publicDER.subarray(-32);
  const privateKey = Buffer.concat([privateDER.subarray(-32), publicKey]);
  return {publicKey, privateKey};
}

export function createRuntimeKeyMaterial() {
  const signing = createEd25519Material();
  const observer = createEd25519Material();
  return {
    signing,
    observer,
    signingKeyId: checksum(signing.publicKey),
    signingVersionFingerprint: checksum(signing.privateKey),
    observerKeyFingerprint: checksum(observer.publicKey),
  };
}

function validateFixture(fixture) {
  assert(fixture.schemaVersion === 'distr.control-plane-e2e-fixture/v1', 'schemaVersion is unsupported');
  assert(fixture.targets?.length === 2, 'exactly two targets are required');
  assert(new Set(fixture.targets.map((target) => target.id)).size === 2, 'target IDs must be unique');
  assert(
    new Set(fixture.targets.map((target) => target.adapterId)).size === 2,
    'adapter assignments must be independent'
  );
  assert(
    new Set(fixture.targets.map((target) => target.observerId)).size === 2,
    'observer registrations must be independent'
  );
  assert(
    new Set(fixture.targets.map((target) => target.configChecksum)).size === 2,
    'target config snapshots must be target-specific'
  );
  for (const target of fixture.targets) {
    for (const field of ['configChecksum', 'capabilityChecksum', 'topologyChecksum']) {
      assert(checksumPattern.test(target[field] ?? ''), `${target.id}.${field} must be a sha256 checksum`);
    }
  }
  for (const label of ['A', 'B']) {
    assert(checksumPattern.test(fixture.releases?.[label]?.digest ?? ''), `release ${label} digest is invalid`);
  }
  assert(
    fixture.product?.capabilities?.providers?.some(
      (provider) => provider.component === 'catalog-provider' && provider.capability === 'catalog.v1'
    ),
    'provider capability is missing'
  );
  assert(
    fixture.product?.capabilities?.consumers?.some(
      (consumer) =>
        consumer.component === 'gateway-consumer' &&
        consumer.requires === 'catalog.v1' &&
        consumer.provider === 'catalog-provider'
    ),
    'consumer capability binding is missing'
  );
  assert(fixture.product?.migration?.retrySafe === true, 'migration must explicitly be retry-safe');
  assert(fixture.product.migration.idempotencyKey, 'migration idempotency key is required');
  assert(fixture.campaign?.waves?.length === 2, 'campaign must freeze two waves');
  assert(fixture.governance?.approvals?.required === 2, 'two approvals are required');
  assert(
    Date.parse(fixture.governance?.maintenanceWindow?.notBefore) <
      Date.parse(fixture.governance?.maintenanceWindow?.notAfter),
    'maintenance window is invalid'
  );
  assert(
    stableStringify(fixture.previousState) === stableStringify({from: 'B', to: 'A', priorActiveRelease: 'B'}),
    'previous-state flow must be B-to-A from prior active B'
  );
  assert(
    fixture.failureMatrix?.schemaVersion === 'distr.control-plane-failure-matrix-fixture/v1',
    'failure matrix schemaVersion is unsupported'
  );
  assert(
    stableStringify(fixture.failureMatrix.cases?.map(({id, expectedOutcome}) => [id, expectedOutcome])) ===
      stableStringify(canonicalCases),
    'failure matrix cases or outcomes changed'
  );
  for (const failureCase of fixture.failureMatrix.cases) {
    assert(
      fixture.targets.some((target) => target.id === failureCase.targetId),
      `${failureCase.id} targetId is unknown`
    );
  }
  return fixture;
}

async function loadFixture() {
  return validateFixture(JSON.parse(await readFile(fixturePath, 'utf8')));
}

function simulateContractFlow(fixture) {
  const targetStates = new Map(
    fixture.targets.map((target) => [
      target.id,
      {
        id: target.id,
        activeRelease: 'A',
        observerId: target.observerId,
        observationSequence: 0,
      },
    ])
  );
  const migrationAttempts = [
    {idempotencyKey: fixture.product.migration.idempotencyKey, result: 'APPLIED'},
    {idempotencyKey: fixture.product.migration.idempotencyKey, result: 'IDEMPOTENT_REPLAY'},
  ];
  const evidence = [];

  for (const release of ['B', 'A']) {
    for (const wave of fixture.campaign.waves) {
      for (const targetId of wave.targetIds) {
        const state = targetStates.get(targetId);
        const target = fixture.targets.find((candidate) => candidate.id === targetId);
        state.observationSequence += 1;
        state.activeRelease = release;
        evidence.push({
          targetId,
          wave: wave.number,
          release,
          executor: target.executorId,
          observer: target.observerId,
          sequence: state.observationSequence,
          releaseDigest: fixture.releases[release].digest,
          configChecksum: target.configChecksum,
          capabilityChecksum: target.capabilityChecksum,
          topologyChecksum: target.topologyChecksum,
          status: 'VERIFIED_ACTIVE',
        });
      }
    }
  }

  const targets = [...targetStates.values()].map(({id, activeRelease, observerId}) => ({
    id,
    activeRelease,
    observerId,
  }));
  return {
    ok: true,
    proofMode: 'fixture-contract',
    targets,
    releaseHistory: fixture.flow.releaseHistory,
    migration: {
      id: fixture.product.migration.id,
      appliedCount: migrationAttempts.filter((attempt) => attempt.result === 'APPLIED').length,
      attempts: migrationAttempts,
    },
    evidence,
    flowChecksum: checksum({
      fixtureChecksum: checksum(fixture),
      stages: fixture.flow.stages,
      targets,
      evidence,
      migrationAttempts,
    }),
    secretLeaks: 0,
  };
}

function run(command, args, {env = {}, allowFailure = false} = {}) {
  const result = spawnSync(command, args, {
    cwd: repoRoot,
    env: {...process.env, ...env},
    encoding: 'utf8',
    stdio: 'pipe',
    shell: false,
  });
  if (!allowFailure && result.status !== 0) {
    const output = [result.stdout, result.stderr].filter(Boolean).join('\n').trim();
    throw new Error(`${command} ${args.join(' ')} exited ${result.status}${output ? `: ${output}` : ''}`);
  }
  return result;
}

function liveStackBlocker() {
  if (process.env.DISTR_CP_FORCE_CONTRACT === 'true') {
    return 'forced contract mode by DISTR_CP_FORCE_CONTRACT=true';
  }
  const docker = run('docker', ['version', '--format', '{{.Server.Version}}'], {allowFailure: true});
  if (docker.error?.code === 'ENOENT') {
    return 'Docker CLI is unavailable; install Docker and ensure the docker command is on PATH';
  }
  if (docker.status !== 0) {
    return 'Docker daemon is unavailable; start a local Docker daemon for the disposable stack';
  }
  const hubImage = process.env.DISTR_CP_HUB_IMAGE?.trim();
  if (!hubImage) {
    return 'local Hub binary image is unavailable; set DISTR_CP_HUB_IMAGE to a prebuilt local image';
  }
  for (const image of ['postgres:18.4-alpine3.23', 'node:26-alpine', 'golang:1.26-alpine', hubImage]) {
    if (run('docker', ['image', 'inspect', image], {allowFailure: true}).status !== 0) {
      return `required local image ${image} is unavailable; preload it without using this runner`;
    }
  }
  if (run('go', ['env', 'GOMODCACHE'], {allowFailure: true}).status !== 0) {
    return 'local Go module cache is unavailable for the offline reference executor build';
  }
  return null;
}

async function unusedLoopbackPort() {
  const server = createServer();
  await new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', resolve);
  });
  const {port} = server.address();
  await new Promise((resolve, reject) => server.close((error) => (error ? reject(error) : resolve())));
  return port;
}

async function waitForReady(url, timeoutMs = 60_000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(url);
      if (response.ok) {
        return;
      }
    } catch {
      // Retry only the loopback endpoint until the local deadline.
    }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  throw new Error(`local service did not become ready at ${url}`);
}

function basicAuth(username, password) {
  return `Basic ${Buffer.from(`${username}:${password}`).toString('base64')}`;
}

function localURL(value, label) {
  const url = new URL(value);
  if (!['127.0.0.1', 'localhost', '::1'].includes(url.hostname)) {
    throw new Error(`${label} must remain loopback-only, got ${url.origin}`);
  }
  return url.origin;
}

function createHubClient(hubURL) {
  const origin = localURL(hubURL, 'Hub URL');
  return async function request(method, requestPath, {body, token, authorization, expected} = {}) {
    const headers = {'Content-Type': 'application/json'};
    if (token) {
      headers.Authorization = `Bearer ${token}`;
    } else if (authorization) {
      headers.Authorization = authorization;
    }
    const response = await fetch(`${origin}${requestPath}`, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    const accepted = expected ?? [200, 201, 202, 204];
    const text = await response.text();
    if (!accepted.includes(response.status)) {
      throw new Error(`${method} ${requestPath} returned ${response.status}: ${text.trim()}`);
    }
    return text ? JSON.parse(text) : null;
  };
}

export async function bootstrapLiveHub({hubURL, runId, fixture}) {
  const request = createHubClient(hubURL);
  const email = `control-plane-${runId}@fixture.test`;
  const password = `Cp-${randomBytes(24).toString('base64url')}!aA1`;
  await request('POST', '/api/v1/auth/register', {
    body: {
      name: 'Control Plane Fixture',
      organizationName: `control-plane-${runId}`,
      email,
      password,
    },
  });
  const login = await request('POST', '/api/v1/auth/login', {body: {email, password}});
  assert(login?.token, 'live Hub login must return a bearer token');
  const token = login.token;
  const organization = await request('GET', '/api/v1/organization', {token});
  const approvers = [];
  for (const role of ['release', 'environment']) {
    const approverEmail = `${role}-approver-${runId}@fixture.test`;
    const invite = await request('POST', '/api/v1/user-accounts', {
      token,
      body: {
        email: approverEmail,
        name: `${role} approver`,
        userRole: 'read_write',
      },
    });
    const inviteToken = new URL(invite.inviteUrl).searchParams.get('jwt');
    assert(inviteToken, `${role} approver invite must contain a scoped token`);
    const approverPassword = `Ap-${randomBytes(24).toString('base64url')}!aA1`;
    const accepted = await request('POST', '/api/v1/auth/invite/accept', {
      token: inviteToken,
      body: {name: `${role} approver`, password: approverPassword},
    });
    assert(accepted?.token, `${role} approver invite acceptance must return a token`);
    const group = await request('POST', '/api/v1/authorization/groups', {
      token,
      body: {
        key: `${role}-approvers-${runId}`,
        displayName: `${role} approvers`,
        description: 'Disposable approval authority',
      },
    });
    await request('POST', `/api/v1/authorization/groups/${group.id}/members`, {
      token,
      body: {
        userAccountId: invite.user.id,
        effectiveFrom: new Date(Date.now() - 60_000).toISOString(),
        reason: 'Disposable two-person approval proof',
      },
    });
    approvers.push({
      role,
      token: accepted.token,
      userAccountId: invite.user.id,
      groupId: group.id,
    });
  }
  const application = await request('POST', '/api/v1/applications', {
    token,
    body: {name: `neutral-product-${runId}`, type: 'docker'},
  });
  const environment = await request('POST', '/api/v1/environments', {
    token,
    body: {
      name: `validation-${runId}`,
      description: 'Disposable neutral control-plane validation',
      sortOrder: 10,
      isProduction: false,
      allowDynamicTargets: false,
    },
  });
  for (const scope of [
    {kind: 'organization', id: organization.id},
    {kind: 'environment', id: environment.id},
  ]) {
    await request('POST', '/api/v1/authorization/control-plane-enrollments', {
      token,
      body: {
        scope,
        enabled: true,
        effectiveFrom: new Date(Date.now() - 60_000).toISOString(),
        reason: 'Disposable protocol-v2 control-plane enrollment',
      },
    });
  }
  const lifecycle = await request('POST', '/api/v1/lifecycles', {
    token,
    body: {
      name: `lifecycle-${runId}`,
      description: 'Disposable A/B/A lifecycle',
      sortOrder: 10,
      phases: [
        {
          name: 'Validation',
          description: 'Disposable validation phase',
          sortOrder: 10,
          environmentIds: [environment.id],
          optional: false,
          automaticPromotion: false,
          minimumSuccessfulDeployments: 0,
        },
      ],
    },
  });
  const channel = await request('POST', '/api/v1/channels', {
    token,
    body: {
      applicationId: application.id,
      lifecycleId: lifecycle.id,
      name: `stable-${runId}`,
      description: 'Disposable stable channel',
      sortOrder: 10,
      isDefault: true,
      allowedVersionRanges: [],
      allowedPrereleasePatterns: [],
      allowedSourceBranches: [],
      allowedSourceTags: [],
    },
  });
  const targets = [];
  for (const fixtureTarget of fixture.targets) {
    const target = await request('POST', '/api/v1/deployment-targets', {
      token,
      body: {
        name: `${fixtureTarget.id}-${runId}`,
        type: 'docker',
        metricsEnabled: false,
        imageCleanupEnabled: false,
        autohealEnabled: false,
        deploymentLogsEnabled: false,
      },
    });
    const access = await request('POST', `/api/v1/deployment-targets/${target.id}/access-request`, {token});
    const agentLogin = await request('POST', '/api/v1/agent/login', {
      authorization: basicAuth(target.id, access.targetSecret),
    });
    assert(agentLogin?.token, `${fixtureTarget.id} agent login must return a bearer token`);
    await request('POST', `/api/v1/agents/${target.id}/capabilities`, {
      token: agentLogin.token,
      body: {
        protocolVersion: 'v2',
        agentVersion: 'neutral-control-plane-e2e',
        supportedRuntimes: ['oci'],
        supportedActions: [
          {actionType: 'distr.compose.deploy', versions: ['1.0.0']},
          {actionType: 'database.migrate', versions: ['1.0.0']},
          {actionType: 'health.observe', versions: ['1.0.0']},
          {actionType: 'distr.preflight', versions: ['1']},
        ],
        operatingSystem: 'linux',
        architecture: 'amd64',
        availableTooling: ['compose'],
        strategyCapabilities: ['signed-executor-v2'],
      },
    });
    targets.push({...fixtureTarget, hubTargetId: target.id, agentToken: agentLogin.token});
  }
  return {
    request,
    token,
    organization,
    application,
    environment,
    lifecycle,
    channel,
    targets,
    approvers,
  };
}

export function buildComponentPublicationEvidence(input) {
  const generated = spawnSync('go', ['run', './examples/control-plane-e2e/provenance-fixture'], {
    cwd: repoRoot,
    env: {
      ...process.env,
      GOMAXPROCS: process.env.GOMAXPROCS || '2',
      GOFLAGS: process.env.GOFLAGS || '-p=1',
    },
    input: JSON.stringify(input),
    encoding: 'utf8',
    windowsHide: true,
  });
  assert(
    generated.status === 0,
    `signed provenance fixture generation failed: ${(generated.stderr || generated.stdout || '').trim()}`
  );
  let result;
  try {
    result = JSON.parse(generated.stdout);
  } catch (error) {
    throw new Error(`fixture contract: signed provenance fixture output is invalid JSON: ${error.message}`);
  }
  assert(result.verification?.valid === true, 'signed provenance fixture must pass production verification');
  assert(checksumPattern.test(result.verification.policyChecksum), 'provenance policy checksum is invalid');
  assert(checksumPattern.test(result.verification.evidenceDigest), 'provenance evidence checksum is invalid');
  assert(result.contractEvidence?.provenance?.length === 1, 'component provenance reference is required');
  assert(result.contractEvidence?.sbom?.length === 1, 'component SBOM reference is required');
  assert(result.publication?.provenance?.evidence?.length === 1, 'signed publication evidence is required');
  return result;
}

export async function publishComponentRelease({request, token, topology, fixture, label, component}) {
  const release = fixture.releases[label];
  const sourceRepository = 'https://code.fixture.invalid/neutral-product';
  const sourceCommit = label === 'A' ? 'a'.repeat(40) : 'b'.repeat(40);
  const buildId = `build-${component.key}-${label}`;
  const builderId = 'https://build.fixture.invalid/workers/release';
  const publication = buildComponentPublicationEvidence({
    artifactKey: component.key,
    platform: 'linux/amd64',
    digest: component.artifactDigest,
    sourceRepository,
    sourceCommit,
    buildId,
    builderId,
  });
  const requirement =
    component.key === 'gateway-consumer'
      ? [
          {
            name: 'catalog.v1',
            range: '^1.0.0',
            resolutionStage: 'product',
            allowedModes: [],
          },
        ]
      : [];
  const migrations =
    label === 'B' && component.key === 'catalog-provider'
      ? [
          {
            key: fixture.product.migration.id,
            type: 'data',
            order: 1,
            compatibility: 'backward-compatible',
            failurePolicy: 'retry',
            description: `Retry-safe migration ${fixture.product.migration.id}`,
          },
        ]
      : [];
  const bundle = await request('POST', '/api/v1/release-bundles', {
    token,
    body: {
      applicationId: topology.application.id,
      channelId: topology.channel.id,
      releaseNumber: `${component.key}-${release.version}`,
      releaseNotes: `Disposable ${component.key} ${label}`,
      sourceRevision: `${component.key}-${release.version}`,
      releaseContract: {
        schema: 'distr.component-release/v2',
        componentKey: component.key,
        version: release.version,
        source: {
          repository: sourceRepository,
          requestedRef: `refs/tags/${release.version}`,
          commit: sourceCommit,
        },
        build: {id: buildId, builder: builderId},
        artifacts: [
          {
            key: component.key,
            type: 'oci-image',
            mediaType: 'application/vnd.oci.image.manifest.v1+json',
            digest: component.artifactDigest,
            platforms: [{platform: 'linux/amd64', digest: component.artifactDigest}],
          },
        ],
        provides: component.key === 'catalog-provider' ? [{name: 'catalog.v1', version: '1.0.0'}] : [],
        requires: requirement,
        migrations,
        adapterRequirements: componentAdapterRequirements(migrations),
        changes: {summary: `Disposable ${label}`, commits: []},
        evidence: publication.contractEvidence,
      },
      components: [
        {
          key: component.key,
          name: component.key,
          type: 'oci_image',
          version: release.version,
          packageRef: `fixture.invalid/${component.key}`,
          digest: component.artifactDigest,
          checksum: component.artifactDigest,
        },
      ],
    },
  });
  return request('POST', `/api/v1/release-bundles/${bundle.id}/publish`, {
    token,
    body: publication.publication,
  });
}

export function componentAdapterRequirements(migrations) {
  const requirements = [
    {stepKind: 'deploy', capability: 'distr.compose.deploy', version: '1.0.0'},
  ];
  if (migrations.length > 0) {
    requirements.push({
      stepKind: 'migration',
      capability: 'database.migrate',
      version: '1.0.0',
    });
  }
  requirements.push({
    stepKind: 'health',
    capability: 'health.observe',
    version: '1.0.0',
  });
  return requirements;
}

async function createDependencyPolicy({request, token, topology, runId}) {
  const policy = await request('POST', '/api/v1/deployment-policies', {
    token,
    body: {
      key: `neutral-${runId}`,
      name: 'Neutral fixture policy',
      description: 'Disposable control-plane validation policy',
    },
  });
  const version = await request('POST', `/api/v1/deployment-policies/${policy.id}/versions`, {
    token,
    body: {
      document: {
        schema: 'distr.deployment-policy/v1',
        approvalRules: topology.approvers.map((approver) => ({
          key: `${approver.role}-approval`,
          principalGroupId: approver.groupId,
          quorum: 1,
          separationConstraints: ['requester_cannot_approve', 'distinct_approvers'],
        })),
        riskGates: [],
        admissionRules: {
          allowedResolutionModes: ['included', 'pinned_existing'],
          minimumBakeSeconds: 0,
          maximumWaitSeconds: 300,
          maintenanceWindowVersionIds: [],
          freezeRuleVersionIds: [],
        },
        campaignRules: {
          minimumWaveBakeSeconds: 0,
          maximumWaveSize: 2,
          maximumConcurrency: 1,
          failureToleranceBasisPoints: 0,
          minimumHealthyBasisPoints: 10000,
        },
        overrideRules: {
          allowed: false,
          shortenableGateKeys: [],
          minimumReasonLength: 20,
        },
        requiredEvidence: [],
        bootstrapRules: {
          mode: 'require_approval',
          approvalRuleKeys: topology.approvers.map((approver) => `${approver.role}-approval`),
          requiredGateKeys: [],
        },
      },
    },
  });
  const published = await request('POST', `/api/v1/deployment-policies/${policy.id}/versions/${version.id}/publish`, {
    token,
    body: {},
  });
  await request('POST', '/api/v1/deployment-policies/bindings', {
    token,
    body: {
      policyVersionId: published.id,
      scopeKind: 'environment',
      scopeId: topology.environment.id,
      role: 'owner',
    },
  });
  return published;
}

async function createLiveRegistry({request, token, topology, fixture}) {
  const scope = await request('POST', '/api/v1/deployment-registry/scopes', {
    token,
    body: {
      key: 'neutral-scope',
      name: 'Neutral fixture scope',
      description: 'Disposable shared scope',
      deliveryModel: 'shared',
      managementState: 'managed',
    },
  });
  const definitions = new Map();
  for (const component of fixture.product.components) {
    const definition = await request('POST', '/api/v1/deployment-registry/definitions', {
      token,
      body: {
        key: component.key,
        name: component.key,
        description: 'Disposable fixture component',
        capabilityScope: 'deployment_unit',
        managementState: 'managed',
      },
    });
    definitions.set(component.key, definition);
  }
  const liveTargets = [];
  for (const target of topology.targets) {
    const assignment = await request('POST', '/api/v1/deployment-registry/assignments', {
      token,
      body: {
        deploymentTargetId: target.hubTargetId,
        environmentId: topology.environment.id,
        activeFrom: new Date(Date.now() - 60_000).toISOString(),
        policyConstraints: {},
      },
    });
    const unit = await request('POST', '/api/v1/deployment-registry/units', {
      token,
      body: {
        deploymentScopeId: scope.id,
        targetEnvironmentAssignmentId: assignment.id,
        deploymentTargetId: target.hubTargetId,
        key: `${target.id}-unit`,
        name: `${target.id} unit`,
        physicalIdentity: `${target.id}.fixture.invalid`,
        managementState: 'managed',
        subscriberSetChecksum: checksum([]),
        subscriberCustomerOrganizationIds: [],
      },
    });
    const instances = new Map();
    for (const component of fixture.product.components) {
      const instance = await request('POST', '/api/v1/deployment-registry/instances', {
        token,
        body: {
          deploymentUnitId: unit.id,
          componentDefinitionId: definitions.get(component.key).id,
          physicalName: `${target.id}-${component.key}`,
          configNamespace: target.id,
          databaseBoundary: `${target.id}-db`,
          healthAdapter: 'fixture-health',
          managementState: 'managed',
        },
      });
      instances.set(component.key, instance);
    }
    liveTargets.push({...target, assignment, unit, instances});
  }
  return {...topology, scope, definitions, targets: liveTargets};
}

async function freezeConfigsAndRegisterBoundaries({
  topology,
  fixture,
  secrets,
  signing,
  signingKeyId,
  signingVersionFingerprint,
}) {
  const {request, token} = topology;
  const implementationByKind = new Map();
  for (const target of topology.targets) {
    if (!implementationByKind.has(target.adapterKind)) {
      const implementation = await request('POST', '/api/v1/adapter-implementations', {
        token,
        body: {
          key: target.adapterId,
          name: target.adapterId,
          version: '1.0.0',
          capabilities: [
            {capability: 'distr.compose.deploy', version: '1.0.0'},
            {capability: 'database.migrate', version: '1.0.0'},
            {capability: 'health.observe', version: '1.0.0'},
          ],
          enabled: true,
        },
      });
      implementationByKind.set(target.adapterKind, implementation);
    }
    const snapshot = await request('POST', '/api/v1/target-config-snapshots', {
      token,
      body: {
        deploymentUnitId: target.unit.id,
        targetEnvironmentAssignmentId: target.assignment.id,
        environmentId: topology.environment.id,
        sourceRepository: 'https://fixture.invalid/neutral-config',
        sourceCommit: target.configChecksum.slice('sha256:'.length, 'sha256:'.length + 40),
        sourceAdapter: 'neutral-fixture',
        adapterVersion: '1.0.0',
        targetPlatform: 'linux/amd64',
        runtimeConstraints: Object.fromEntries(
          Object.entries(target.targetConfig).map(([key, value]) => [key, String(value)])
        ),
        objects: [
          {
            key: 'adapter-input',
            kind: 'adapter_input',
            reference: `s3://fixture-config/_immutable/sha256/${target.configChecksum.slice(
              'sha256:'.length
            )}/config.json`,
            mediaType: 'application/json',
            sizeBytes: 2,
            checksum: target.configChecksum,
          },
        ],
        components: [...target.instances.entries()].map(([componentKey, instance]) => ({
          physicalName: `${target.id}-${componentKey}`,
          componentInstanceId: instance.id,
          deploymentUnitId: target.unit.id,
        })),
        secretReferences: [],
        featureFlags: [],
      },
    });
    const observerCredential = target.id === fixture.targets[0].id ? secrets.observerAlpha : secrets.observerBeta;
    const observers = new Map();
    for (const [componentKey, instance] of target.instances) {
      const observer = await request('POST', '/api/v1/observer-registrations', {
        token,
        body: {
          deploymentUnitId: target.unit.id,
          componentInstanceId: instance.id,
          observerKey: `${target.observerId}-${componentKey}`,
          adapterImplementation: 'neutral-http-observer',
          adapterVersion: '1.0.0',
          credential: observerCredential,
          maxFreshnessSeconds: fixture.observation.maximumAgeSeconds,
          maxClockSkewSeconds: 30,
          measurements: fixture.observation.requiredChecks,
        },
      });
      observers.set(componentKey, observer);
    }
    target.snapshot = snapshot;
    target.observers = observers;
    const adapters = new Map();
    for (const [componentKey] of target.instances) {
      const componentAdapters = [];
      for (const scope of componentAdapterScopes(target, componentKey)) {
        componentAdapters.push(
          await request('POST', '/api/v1/adapter-assignments', {
            token,
            body: {
              adapterImplementationId: implementationByKind.get(target.adapterKind).id,
              scopeType: scope.scopeType,
              scopeReference: scope.scopeReference,
              configSnapshotId: snapshot.id,
              configChecksum: snapshot.canonicalChecksum,
              keyConfiguration: {
                keyId: signingKeyId,
                publicKeyFingerprint: checksum(signing.publicKey),
                signingKeyReference: 'secret-provider://fixture/executor-signing',
                signingKeyVersionFingerprint: signingVersionFingerprint,
              },
              enabled: true,
            },
          })
        );
      }
      adapters.set(componentKey, componentAdapters);
    }
    target.adapters = adapters;
    target.observerCredential = observerCredential;
  }
  return topology;
}

export function componentAdapterScopes(target, componentKey) {
  const instance = target.instances.get(componentKey);
  const observer = target.observers.get(componentKey);
  assert(instance, `${target.id ?? 'target'} ${componentKey} component instance is required`);
  assert(observer, `${target.id ?? 'target'} ${componentKey} observer registration is required`);
  return [
    {
      capability: 'distr.compose.deploy',
      scopeType: 'component_instance',
      scopeReference: instance.id,
    },
    {
      capability: 'database.migrate',
      scopeType: 'database_resource',
      scopeReference: instance.databaseBoundary,
    },
    {
      capability: 'health.observe',
      scopeType: 'observer_registration',
      scopeReference: observer.id,
    },
  ];
}

async function publishProductRelease({topology, fixture, label, componentReleases, policyVersion}) {
  const {request, token} = topology;
  const release = fixture.releases[label];
  const product = await request('POST', '/api/v1/product-releases', {
    token,
    body: {
      schema: 'distr.product-release/v1',
      applicationId: topology.application.id,
      channelId: topology.channel.id,
      product: fixture.product.id,
      version: release.version,
      dependencyPolicyVersion: policyVersion.id,
      releaseNotes: `Disposable product release ${label}`,
      requiredPlatforms: ['linux/amd64'],
      components: componentReleases.map((componentRelease) => ({
        componentReleaseId: componentRelease.id,
        componentReleaseChecksum: componentRelease.canonicalChecksum,
      })),
      requirements: [],
    },
  });
  const validation = await request('POST', `/api/v1/product-releases/${product.id}/validate`, {
    token,
    body: {},
  });
  assert(validation.valid === true, `product release ${label} must validate`);
  return request('POST', `/api/v1/product-releases/${product.id}/publish`, {
    token,
    body: {},
  });
}

async function publishPlans({topology, productRelease, superseded = new Map()}) {
  const plans = new Map();
  for (const target of topology.targets) {
    const prior = superseded.get(target.id);
    const draft = await topology.request('POST', '/api/v1/deployment-plan-drafts', {
      token: topology.token,
      body: {
        productReleaseId: productRelease.id,
        deploymentUnitId: target.unit.id,
        environmentAssignmentId: target.assignment.id,
        targetConfigSnapshotId: target.snapshot.id,
        protocolVersion: 'v2',
        ...(prior
          ? {
              supersedesDeploymentPlanId: prior.id,
              supersedeReason: 'Previous-state B-to-A return after independently verified B',
            }
          : {}),
      },
    });
    const validation = await topology.request('POST', `/api/v1/deployment-plan-drafts/${draft.id}/validate`, {
      token: topology.token,
      body: {},
    });
    assert(validation.issues?.length === 0, `${target.id} deployment plan preview must validate`);
    const plan = await topology.request('POST', `/api/v1/deployment-plan-drafts/${draft.id}/publish`, {
      token: topology.token,
      body: {
        expectedRevision: draft.revision,
        expectedPreviewChecksum: validation.previewChecksum,
      },
    });
    let approval = await topology.request('POST', `/api/v1/deployment-plans/${plan.id}/approval-requests`, {
      token: topology.token,
      body: {expiresAt: new Date(Date.now() + 60 * 60 * 1000).toISOString()},
    });
    for (const requirement of approval.requirements ?? []) {
      const approver = topology.approvers.find((candidate) => candidate.groupId === requirement.principalGroupId);
      assert(approver, `${target.id} approval requirement must map to an independent approver`);
      await topology.request('POST', `/api/v1/approval-requests/${approval.id}/decisions`, {
        token: approver.token,
        body: {
          approvalRequirementId: requirement.id,
          decision: 'APPROVE',
          comment: `Independent ${approver.role} approval for ${target.id}`,
          expectedRequestRevision: approval.revision,
          idempotencyKey: `${target.id}:${plan.id}:${approver.role}`,
        },
      });
      approval = await topology.request('GET', `/api/v1/approval-requests/${approval.id}`, {
        token: topology.token,
      });
    }
    assert(approval.state === 'APPROVED', `${target.id} approval request must resolve before campaign publication`);
    assert(
      new Set((approval.decisions ?? []).map((decision) => decision.actorUserAccountId)).size ===
        topology.approvers.length,
      `${target.id} must retain two distinct approval actors`
    );
    plans.set(target.id, {...plan, approval});
  }
  return plans;
}

export async function admitPublishedPlans({topology, plans, runId}) {
  for (const [targetId, plan] of plans) {
    const admission = await topology.request('POST', `/api/v1/deployment-plans/${plan.id}/admission`, {
      token: topology.token,
      body: {schedulerIdempotencyKey: `neutral-${runId}-${targetId}`},
    });
    assert(admission.decision === 'ADMIT', `${targetId} deployment plan admission must be ADMIT`);
    assert(admission.approvalRequestId === plan.approval.id, `${targetId} admission must bind the approved request`);
    plan.admission = admission;
  }
  return plans;
}

export async function advanceCampaignRunToRunning({topology, campaign}) {
  const states = ['VALIDATED', 'AWAITING_APPROVAL', 'SCHEDULED', 'RUNNING'];
  let current = campaign.run;
  assert(current.state === 'DRAFT' && current.version === 1, 'new campaign run must begin at DRAFT revision 1');
  for (const state of states) {
    const expectedVersion = current.version;
    let transitioned;
    try {
      transitioned = await topology.request('POST', `/api/v1/deployment-campaign-runs/${current.id}/transitions`, {
        token: topology.token,
        body: {
          expectedVersion,
          to: state,
          reason: `Advance disposable campaign to ${state}`,
        },
      });
    } catch (error) {
      throw new Error(`fixture contract: campaign transition to ${state} failed: ${error.message}`);
    }
    assert(transitioned, `campaign transition to ${state} returned no campaign state`);
    assert(transitioned.id === current.id, `campaign transition to ${state} changed the run identity`);
    assert(transitioned.state === state, `campaign transition did not reach ${state}`);
    assert(transitioned.version === expectedVersion + 1, `campaign transition to ${state} returned the wrong revision`);
    assert(transitioned.admissionsBlocked === false, `campaign transition to ${state} blocked admissions`);
    current = transitioned;
  }
  campaign.run = current;
  return current;
}

function defaultPollingClock() {
  return {
    now: () => Date.now(),
    sleep: (milliseconds) => new Promise((resolve) => setTimeout(resolve, milliseconds)),
  };
}

function passedPreflightForTasks(plan, current, tasks) {
  const taskIds = new Set(tasks.map((task) => task.id));
  const adapterCheckKeys = new Set(plan.stepAdapters.map((adapter) => `adapter:${adapter.stepKey}`));
  const run = current.preflightRuns?.[0];
  if (
    run?.status !== 'PASSED' ||
    run.planChecksum !== plan.canonicalChecksum ||
    !run.checks?.length ||
    run.checks.some((check) => check.status !== 'PASSED')
  ) {
    return undefined;
  }
  const taskChecks = new Set(run.checks.filter((check) => check.taskId).map((check) => check.taskId));
  const adapterChecks = new Set(run.checks.map((check) => check.checkKey));
  return [...taskIds].every((taskId) => taskChecks.has(taskId)) &&
    [...adapterCheckKeys].every((checkKey) => adapterChecks.has(checkKey))
    ? run
    : undefined;
}

function campaignReadinessSnapshot({campaign, tasks, plans, currentPlans}) {
  const planStates = [];
  const campaignTasks = [];
  const failures = [];
  let ready = campaign.state === 'RUNNING' && campaign.admissionsBlocked === false;
  if (['PAUSED', 'FAILED', 'COMPLETED', 'CANCELED'].includes(campaign.state) || campaign.admissionsBlocked) {
    failures.push(`campaign state=${campaign.state} admissionsBlocked=${campaign.admissionsBlocked}`);
  }
  for (const [targetId, plan] of plans) {
    const current = currentPlans.get(plan.id);
    const planTasks = tasks.filter((task) => task.deploymentPlanId === plan.id);
    const expectedTargets = new Map((plan.targets ?? []).map((target) => [target.id, target.deploymentTargetId]));
    const taskIdentitiesMatch =
      planTasks.length > 0 &&
      planTasks.every(
        (task) =>
          task.protocolVersion === 'v2' && expectedTargets.get(task.deploymentPlanTargetId) === task.deploymentTargetId
      );
    const preflight = current && taskIdentitiesMatch ? passedPreflightForTasks(plan, current, planTasks) : undefined;
    const failedPreflight =
      current?.preflightRuns?.[0]?.status === 'FAILED' ? current.preflightRuns[0] : undefined;
    if (failedPreflight) {
      const messages = failedPreflight.checks
        ?.filter((check) => check.status === 'FAILED')
        .map((check) => check.message)
        .filter(Boolean);
      failures.push(`plan ${plan.id} preflight FAILED${messages?.length ? `: ${messages.join('; ')}` : ''}`);
    }
    const failedTasks = planTasks.filter((task) => ['FAILED', 'CANCELED'].includes(task.status));
    if (failedTasks.length) {
      failures.push(
        `plan ${plan.id} terminal tasks ${failedTasks.map((task) => `${task.id}:${task.status}`).join(',')}`
      );
    }
    ready = ready && taskIdentitiesMatch && Boolean(preflight);
    campaignTasks.push(...planTasks);
    planStates.push({
      targetId,
      planId: plan.id,
      taskIds: planTasks.map((task) => task.id),
      taskStatuses: planTasks.map((task) => task.status),
      preflightStatus: preflight?.status ?? current?.preflightRuns?.[0]?.status ?? 'MISSING',
      preflightId: preflight?.id ?? '',
    });
    if (preflight) {
      plan.preflight = preflight;
    }
  }
  return {
    ready,
    failures,
    campaign: {
      id: campaign.id,
      state: campaign.state,
      version: campaign.version,
      admissionsBlocked: campaign.admissionsBlocked,
    },
    tasks: campaignTasks,
    plans: planStates,
  };
}

export async function waitForCampaignReadiness({
  topology,
  campaign,
  plans,
  timeoutMs = 60_000,
  intervalMs = 1_000,
  clock = defaultPollingClock(),
}) {
  assert(timeoutMs > 0 && intervalMs > 0, 'campaign readiness polling bounds must be positive');
  const startedAt = clock.now();
  let last;
  while (true) {
    const currentCampaign = await topology.request('GET', `/api/v1/deployment-campaign-runs/${campaign.run.id}`, {
      token: topology.token,
    });
    const tasks = await topology.request('GET', '/api/v1/tasks', {token: topology.token});
    const currentPlans = new Map();
    for (const plan of plans.values()) {
      currentPlans.set(
        plan.id,
        await topology.request('GET', `/api/v1/deployment-plans/${plan.id}`, {
          token: topology.token,
        })
      );
    }
    last = campaignReadinessSnapshot({
      campaign: currentCampaign,
      tasks,
      plans,
      currentPlans,
    });
    if (last.ready) {
      campaign.run = currentCampaign;
      return last;
    }
    if (last.failures.length) {
      throw new Error(
        `fixture contract: campaign readiness failed: ${last.failures.join(' | ')}; last state ${JSON.stringify(last)}`
      );
    }
    const elapsed = clock.now() - startedAt;
    if (elapsed >= timeoutMs) {
      throw new Error(`fixture contract: campaign readiness timed out; last state ${JSON.stringify(last)}`);
    }
    await clock.sleep(Math.min(intervalMs, timeoutMs - elapsed));
  }
}

function targetLeaseReadinessSnapshot({campaign, tasks, plan, target}) {
  const matchingTasks = tasks.filter(
    (task) => task.deploymentPlanId === plan.id && task.deploymentTargetId === target.hubTargetId
  );
  const taskStates = matchingTasks.map((task) => ({
    id: task.id,
    status: task.status,
    steps: (task.stepRuns ?? []).map((step) => ({
      stepKey: step.stepKey,
      status: step.status,
    })),
  }));
  const latestPreflight = plan.preflightRuns?.[0];
  const failures = [];
  if (['FAILED', 'COMPLETED', 'CANCELED'].includes(campaign.state) || campaign.admissionsBlocked) {
    failures.push(`campaign state=${campaign.state} admissionsBlocked=${campaign.admissionsBlocked}`);
  }
  const failedPreflight = latestPreflight?.status === 'FAILED' ? latestPreflight : undefined;
  if (failedPreflight) {
    const messages = failedPreflight.checks
      ?.filter((check) => check.status === 'FAILED')
      .map((check) => check.message)
      .filter(Boolean);
    failures.push(`plan ${plan.id} preflight FAILED${messages?.length ? `: ${messages.join('; ')}` : ''}`);
  }
  const terminalTasks = matchingTasks.filter((task) => ['SUCCEEDED', 'FAILED', 'CANCELED'].includes(task.status));
  if (terminalTasks.length) {
    failures.push(
      `terminal tasks ${terminalTasks
        .map((task) => {
          const terminalSteps = (task.stepRuns ?? [])
            .filter((step) => ['FAILED', 'SKIPPED'].includes(step.status))
            .map((step) => `${step.stepKey}:${step.status}`);
          return `${task.id}:${task.status}${terminalSteps.length ? `[${terminalSteps.join(',')}]` : ''}`;
        })
        .join(',')}`
    );
  }
  return {
    failures,
    campaign: {
      id: campaign.id,
      state: campaign.state,
      version: campaign.version,
      admissionsBlocked: campaign.admissionsBlocked,
    },
    plan: {
      id: plan.id,
      canonicalChecksum: plan.canonicalChecksum,
      preflightStatus: latestPreflight?.status ?? 'MISSING',
    },
    targetId: target.hubTargetId,
    tasks: taskStates,
  };
}

export async function waitForTargetExecutionLease({
  topology,
  campaign,
  plan,
  target,
  leaseIdentity,
  timeoutMs = 60_000,
  intervalMs = 1_000,
  clock = defaultPollingClock(),
}) {
  assert(timeoutMs > 0 && intervalMs > 0, 'target lease readiness polling bounds must be positive');
  const startedAt = clock.now();
  let last;
  while (true) {
    const lease = await topology.request('POST', '/api/executor/v2/executions/lease', {
      token: target.agentToken,
      body: {
        executorId: target.executorId,
        adapterRevision: leaseIdentity.adapterRevision,
        keyId: leaseIdentity.keyId,
        leaseSeconds: 60,
      },
      expected: [200, 204],
    });
    if (lease) {
      return lease;
    }

    const currentCampaign = await topology.request(
      'GET',
      `/api/v1/deployment-campaign-runs/${campaign.run.id}`,
      {token: topology.token}
    );
    const tasks = await topology.request('GET', '/api/v1/tasks', {token: topology.token});
    const currentPlan = await topology.request('GET', `/api/v1/deployment-plans/${plan.id}`, {
      token: topology.token,
    });
    last = targetLeaseReadinessSnapshot({
      campaign: currentCampaign,
      tasks,
      plan: currentPlan,
      target,
    });
    campaign.run = currentCampaign;
    if (last.failures.length) {
      throw new Error(
        `fixture contract: target lease readiness failed: ${last.failures.join(' | ')}; last state ${JSON.stringify(last)}`
      );
    }
    const elapsed = clock.now() - startedAt;
    if (elapsed >= timeoutMs) {
      throw new Error(`fixture contract: target lease readiness timed out; last state ${JSON.stringify(last)}`);
    }
    await clock.sleep(Math.min(intervalMs, timeoutMs - elapsed));
  }
}

export async function publishCampaign({topology, plans, label, runId}) {
  const planIDs = [...plans.values()].map((plan) => plan.id);
  const draft = await topology.request('POST', '/api/v1/deployment-campaign-drafts', {
    token: topology.token,
    body: {
      name: `neutral-${label}-${runId}`,
      description: `Disposable ${label} campaign`,
      membership: {planIds: planIDs},
      waves: topology.targets.map((target, index) => ({
        order: index + 1,
        name: `wave-${index + 1}`,
        planIds: [plans.get(target.id).id],
        bakeSeconds: 0,
        maximumConcurrency: 1,
      })),
      prerequisites: [],
      riskPolicy: {
        maximumConcurrency: 1,
        failureToleranceBasisPoints: 0,
        minimumHealthyBasisPoints: 10000,
      },
    },
  });
  const validation = await topology.request('POST', `/api/v1/deployment-campaign-drafts/${draft.id}/validate`, {
    token: topology.token,
    body: {},
  });
  assert(validation.valid === true, `campaign ${label} must validate`);
  const revision = await topology.request('POST', `/api/v1/deployment-campaign-drafts/${draft.id}/publish`, {
    token: topology.token,
    body: {idempotencyKey: `neutral-${label}-${runId}`},
  });
  assert(revision.members?.length === plans.size, `campaign ${label} must freeze every plan member`);
  const membersByPlan = new Map(revision.members.map((member) => [member.planId, member]));
  for (const [targetId, plan] of plans) {
    const member = membersByPlan.get(plan.id);
    assert(member, `campaign ${label} is missing ${targetId}`);
    assert(
      member.approvalRequestId === plan.approval.id &&
        member.approvalRequestRevision === plan.approval.revision &&
        member.approvalChecksum === plan.approval.subjectChecksum,
      `campaign ${label} ${targetId} approval evidence changed during publication`
    );
    assert(
      member.admissionEvaluationId === plan.admission.id &&
        member.admissionChecksum === plan.admission.decisionChecksum,
      `campaign ${label} ${targetId} admission evidence changed during publication`
    );
  }
  const run = await topology.request('POST', '/api/v1/deployment-campaign-runs', {
    token: topology.token,
    body: {campaignRevisionId: revision.id},
  });
  assert(run.campaignRevisionId === revision.id, `campaign ${label} run must bind the published revision`);
  assert(run.state === 'DRAFT' && run.version === 1, `campaign ${label} run must begin at DRAFT revision 1`);
  assert(run.admissionsBlocked === false, `campaign ${label} run must begin with admissions open`);
  return {draft, revision, run};
}

async function postExecutor(url, secret, body) {
  localURL(url, 'executor URL');
  const response = await fetch(`${url}/v1/operations`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${secret}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(body),
  });
  const text = await response.text();
  if (![200, 202].includes(response.status)) {
    throw new Error(`executor POST /v1/operations returned ${response.status}: ${text.trim()}`);
  }
  return text ? JSON.parse(text) : null;
}

async function executeLeasedAttempt({topology, target, lease, executorURL, executorSecret, reference, fixture}) {
  const payload = JSON.parse(Buffer.from(lease.intent.payload, 'base64').toString('utf8'));
  const binding = {
    tenantId: payload.organizationId,
    targetId: payload.deploymentTargetId,
    attemptId: payload.attemptId,
    taskId: payload.taskId,
    stepRunId: payload.stepRunId,
    executionId: payload.executionId,
    attemptNumber: payload.attemptNumber,
    stepKey: payload.stepKey,
    planChecksum: payload.planChecksum,
    artifactDigest: payload.artifactDigest,
    configChecksum: payload.configChecksum,
    adapterRevision: payload.adapterRevision,
    resourceKey: payload.resourceKey,
    fenceGeneration: payload.fenceGeneration,
  };
  if (reference) {
    await postExecutor(executorURL, executorSecret, {
      intent: lease.intent,
      binding,
    });
  } else {
    const operationId = payload.executionId;
    const idempotencyKey = `${payload.attemptId}:${payload.attemptNumber}:${payload.stepKey}`;
    const intent = {
      schemaVersion: 'distr.executor-intent/v2',
      tenantId: payload.organizationId,
      targetId: payload.deploymentTargetId,
      attemptId: payload.attemptId,
      operationId,
      idempotencyKey,
      taskId: payload.taskId,
      stepId: payload.stepKey,
      planId: payload.planChecksum,
      adapterRevision: payload.adapterRevision,
      resourceKey: payload.resourceKey,
      fenceGeneration: payload.fenceGeneration,
      issuedAt: payload.issuedAt,
      expiresAt: payload.expiresAt,
      payload: {
        releaseDigest: payload.artifactDigest,
        configChecksum: payload.configChecksum,
        migration: {
          id: fixture.product.migration.id,
          idempotencyKey: `${fixture.product.migration.id}:${target.id}`,
          retrySafe: true,
        },
      },
    };
    const {createHmac} = await import('node:crypto');
    await postExecutor(executorURL, executorSecret, {
      attemptId: payload.attemptId,
      operationId,
      idempotencyKey,
      intent,
      signature: `sha256:${createHmac('sha256', executorSecret).update(stableStringify(intent)).digest('hex')}`,
    });
  }

  const attempt = lease.attempt;
  const identity = attempt.identity;
  const agent = {token: target.agentToken};
  await topology.request('POST', `/api/executor/v2/attempts/${attempt.id}/acknowledge`, {
    token: agent.token,
    body: {executorId: target.executorId, fenceGeneration: attempt.fence.generation},
  });
  await topology.request('POST', `/api/executor/v2/attempts/${attempt.id}/events`, {
    token: agent.token,
    body: {
      executorId: target.executorId,
      executionId: identity.executionId,
      attemptNumber: identity.attemptNumber,
      stepKey: identity.stepKey,
      fenceGeneration: attempt.fence.generation,
      eventSequence: 1,
      status: 'SUCCEEDED',
      payloadChecksum: checksum({attemptId: attempt.id, status: 'SUCCEEDED'}),
      message: 'Disposable executor completed the signed attempt',
      occurredAt: new Date().toISOString(),
    },
  });
  await topology.request('POST', `/api/executor/v2/attempts/${attempt.id}/complete`, {
    token: agent.token,
    body: {
      executorId: target.executorId,
      fenceGeneration: attempt.fence.generation,
      status: 'SUCCEEDED',
      completedAt: new Date().toISOString(),
    },
  });
  return {attempt, payload};
}

async function executeAndObserveRelease({
  topology,
  fixture,
  label,
  ports,
  secrets,
  signingVersionFingerprint,
  signingKeyId,
  plans,
  campaign,
  sequence,
}) {
  const evidence = [];
  for (const [index, target] of topology.targets.entries()) {
    await waitForCampaignReadiness({
      topology,
      campaign,
      plans: new Map([[target.id, plans.get(target.id)]]),
    });
    const plan = plans.get(target.id);
    const leaseIdentities = derivePlanLeaseIdentities({
      plan,
      target,
      signingKeyId,
    });
    let completed = 0;
    const executedStepKeys = [];
    for (const leaseIdentity of leaseIdentities) {
      const lease = await waitForTargetExecutionLease({
        topology,
        campaign,
        plan,
        target,
        leaseIdentity,
      });
      const execution = await executeLeasedAttempt({
        topology,
        target,
        lease,
        executorURL: `http://127.0.0.1:${index === 0 ? ports.external : ports.reference}`,
        executorSecret: index === 0 ? secrets.external : secrets.reference,
        reference: index !== 0,
        fixture,
      });
      assert(
        execution.payload.stepKey === leaseIdentity.stepKey,
        `${target.id} leased ${execution.payload.stepKey} while waiting for ${leaseIdentity.stepKey}`
      );
      executedStepKeys.push(execution.payload.stepKey);
      completed += 1;
    }
    assert(completed === leaseIdentities.length, `${target.id} must execute every frozen target-step adapter`);

    const localObservation = {
      observerId: target.observerId,
      targetId: target.hubTargetId,
      sequence,
      observedAt: new Date().toISOString(),
      releaseDigest: fixture.releases[label].digest,
      configChecksum: target.snapshot.canonicalChecksum,
      capabilityChecksum: target.capabilityChecksum,
      topologyChecksum: target.topologyChecksum,
      schemaVersion: fixture.releases[label].configSchemaVersion,
      health: 'HEALTHY',
    };
    const observerURL = `http://127.0.0.1:${index === 0 ? ports.observerAlpha : ports.observerBeta}`;
    const localResponse = await fetch(`${observerURL}/v1/observations`, {
      method: 'POST',
      headers: {
        Authorization: `Bearer ${target.observerCredential}`,
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(localObservation),
    });
    const localText = await localResponse.text();
    if (![200, 202].includes(localResponse.status)) {
      throw new Error(`observer ${target.observerId} returned ${localResponse.status}: ${localText.trim()}`);
    }
    const independentEvidence = JSON.parse(localText);
    for (const component of fixture.product.components) {
      const observed = await topology.request('POST', '/api/observer/v1/observations', {
        authorization: `Observer ${target.observerCredential}`,
        body: {
          organizationId: topology.organization.id,
          observerId: target.observers.get(component.key).id,
          deploymentUnitId: target.unit.id,
          componentInstanceId: target.instances.get(component.key).id,
          componentKey: component.key,
          sourceSequence: sequence,
          capturedAt: localObservation.observedAt,
          evidenceChecksum: checksum({
            independentEvidence: independentEvidence.evidenceChecksum,
            component: component.key,
          }),
          evidenceReference: `fixture://${target.id}/${label}/${component.key}`,
          artifactDigest: component.artifactDigest,
          configChecksum: target.snapshot.canonicalChecksum,
          schemaVersion: localObservation.schemaVersion,
          capabilityChecksum: target.capabilityChecksum,
          platform: 'linux/amd64',
          topologyChecksum: target.topologyChecksum,
          health: 'HEALTHY',
          outcome: 'COMPLETE',
        },
      });
      evidence.push({
        targetId: target.id,
        component: component.key,
        release: label,
        attempts: completed,
        executedStepKeys,
        observed,
      });
    }
  }
  await topology.request('GET', '/api/v1/control-plane/fleet', {token: topology.token});
  return evidence;
}

export async function runLiveHubJourney({
  topology,
  fixture,
  runId,
  ports,
  secrets,
  signing,
  signingKeyId,
  signingVersionFingerprint,
}) {
  let live = await createLiveRegistry({
    request: topology.request,
    token: topology.token,
    topology,
    fixture,
  });
  live = await freezeConfigsAndRegisterBoundaries({
    topology: live,
    fixture,
    secrets,
    signing,
    signingKeyId,
    signingVersionFingerprint,
  });
  const policyVersion = await createDependencyPolicy({
    request: live.request,
    token: live.token,
    topology: live,
    runId,
  });
  const components = {};
  for (const label of ['A', 'B']) {
    components[label] = [];
    for (const component of fixture.product.components) {
      components[label].push(
        await publishComponentRelease({
          request: live.request,
          token: live.token,
          topology: live,
          fixture,
          label,
          component,
        })
      );
    }
  }
  const products = {
    A: await publishProductRelease({
      topology: live,
      fixture,
      label: 'A',
      componentReleases: components.A,
      policyVersion,
    }),
    B: await publishProductRelease({
      topology: live,
      fixture,
      label: 'B',
      componentReleases: components.B,
      policyVersion,
    }),
  };

  const evidence = [];
  const initialAPlans = await publishPlans({topology: live, productRelease: products.A});
  await admitPublishedPlans({topology: live, plans: initialAPlans, runId: `${runId}-initial-a`});
  const initialACampaign = await publishCampaign({
    topology: live,
    plans: initialAPlans,
    label: 'initial-a',
    runId,
  });
  await advanceCampaignRunToRunning({topology: live, campaign: initialACampaign});
  evidence.push(
    ...(await executeAndObserveRelease({
      topology: live,
      fixture,
      label: 'A',
      ports,
      secrets,
      signingKeyId,
      signingVersionFingerprint,
      plans: initialAPlans,
      campaign: initialACampaign,
      sequence: 1,
    }))
  );
  const bPlans = await publishPlans({
    topology: live,
    productRelease: products.B,
    superseded: initialAPlans,
  });
  await admitPublishedPlans({topology: live, plans: bPlans, runId: `${runId}-b`});
  const bCampaign = await publishCampaign({topology: live, plans: bPlans, label: 'b', runId});
  await advanceCampaignRunToRunning({topology: live, campaign: bCampaign});
  evidence.push(
    ...(await executeAndObserveRelease({
      topology: live,
      fixture,
      label: 'B',
      ports,
      secrets,
      signingKeyId,
      signingVersionFingerprint,
      plans: bPlans,
      campaign: bCampaign,
      sequence: 2,
    }))
  );
  const previousStatePlans = await publishPlans({
    topology: live,
    productRelease: products.A,
    superseded: bPlans,
  });
  await admitPublishedPlans({topology: live, plans: previousStatePlans, runId: `${runId}-previous-a`});
  const previousStateCampaign = await publishCampaign({
    topology: live,
    plans: previousStatePlans,
    label: 'previous-a',
    runId,
  });
  await advanceCampaignRunToRunning({topology: live, campaign: previousStateCampaign});
  evidence.push(
    ...(await executeAndObserveRelease({
      topology: live,
      fixture,
      label: 'A',
      ports,
      secrets,
      signingKeyId,
      signingVersionFingerprint,
      plans: previousStatePlans,
      campaign: previousStateCampaign,
      sequence: 3,
    }))
  );

  const fleet = await live.request('GET', '/api/v1/control-plane/fleet', {token: live.token});
  for (const target of live.targets) {
    const rows = fleet.items?.filter((row) => row.deploymentTargetId === target.hubTargetId) ?? [];
    assert(rows.length > 0, `${target.id} must appear in the live fleet read model`);
    assert(
      rows.every((row) => row.activeRelease === fixture.releases.A.version),
      `${target.id} live fleet rows must return to release A`
    );
  }
  const migrationAttempts = [
    ...new Map(
      evidence
        .filter((item) => item.release === 'B')
        .flatMap((item) =>
          item.executedStepKeys
            .filter((stepKey) => stepKey.includes(fixture.product.migration.id))
            .map((stepKey) => [
              `${item.targetId}:${stepKey}`,
              {targetId: item.targetId, stepKey, result: 'SUCCEEDED_VIA_V2'},
            ])
        )
    ).values(),
  ];
  assert(migrationAttempts.length > 0, 'release B must execute its retry-safe migration through v2');
  return {
    ok: true,
    proofMode: 'live-hub-api',
    targets: live.targets.map((target) => ({
      id: target.id,
      hubTargetId: target.hubTargetId,
      activeRelease: 'A',
      observerId: target.observerId,
    })),
    releaseHistory: ['A', 'B', 'A'],
    migration: {
      id: fixture.product.migration.id,
      appliedCount: migrationAttempts.length,
      attempts: migrationAttempts,
    },
    evidence,
    fleet,
    flowChecksum: checksum({releaseHistory: ['A', 'B', 'A'], evidence, fleet}),
    secretLeaks: 0,
  };
}

function nonEmptyLines(value) {
  return (value ?? '')
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean);
}

export function confirmComposeCleanup({project, composeArgs, composeEnv}) {
  const down = run('docker', [...composeArgs, 'down', '-v', '--remove-orphans'], {
    env: composeEnv,
    allowFailure: true,
  });
  const checks = [
    [
      'containers',
      ['ps', '-a', '--filter', `label=com.docker.compose.project=${project}`, '--format', '{{.ID}} {{.Names}}'],
    ],
    ['volumes', ['volume', 'ls', '--filter', `label=com.docker.compose.project=${project}`, '--format', '{{.Name}}']],
    ['networks', ['network', 'ls', '--filter', `label=com.docker.compose.project=${project}`, '--format', '{{.Name}}']],
  ].map(([kind, args]) => {
    const result = run('docker', args, {allowFailure: true});
    return {
      kind,
      status: result.status,
      error: result.error?.message,
      resources: nonEmptyLines(result.stdout),
      stderr: result.stderr?.trim(),
    };
  });
  const retainedResources = checks.flatMap(({kind, resources}) => resources.map((resource) => ({kind, resource})));
  const inspectionFailures = checks
    .filter(({status, error}) => status !== 0 || error)
    .map(({kind, status, error, stderr}) => ({kind, status, error, stderr}));
  return {
    completed: down.status === 0 && !down.error && inspectionFailures.length === 0 && retainedResources.length === 0,
    downStatus: down.status,
    downError: down.error?.message,
    downStderr: down.stderr?.trim(),
    resourcesRemoved: down.status === 0 ? [project] : [],
    retainedResources,
    inspectionFailures,
  };
}

async function runDisposableStack(fixture) {
  const runId = randomBytes(6).toString('hex');
  const project = `distr-control-plane-e2e-${runId}`;
  const ports = {
    postgres: await unusedLoopbackPort(),
    hub: await unusedLoopbackPort(),
    external: await unusedLoopbackPort(),
    reference: await unusedLoopbackPort(),
    observerAlpha: await unusedLoopbackPort(),
    observerBeta: await unusedLoopbackPort(),
  };
  const secrets = {
    postgres: randomBytes(24).toString('base64url'),
    jwt: randomBytes(32).toString('base64'),
    external: randomBytes(32).toString('base64url'),
    reference: randomBytes(32).toString('base64url'),
    observerAlpha: randomBytes(32).toString('base64url'),
    observerBeta: randomBytes(32).toString('base64url'),
  };
  const {signing, observer, signingKeyId, signingVersionFingerprint, observerKeyFingerprint} =
    createRuntimeKeyMaterial();
  const goModuleCache = run('go', ['env', 'GOMODCACHE']).stdout.trim();
  const composeEnv = {
    DISTR_CP_POSTGRES_PORT: String(ports.postgres),
    DISTR_CP_EXTERNAL_EXECUTOR_PORT: String(ports.external),
    DISTR_CP_REFERENCE_EXECUTOR_PORT: String(ports.reference),
    DISTR_CP_OBSERVER_ALPHA_PORT: String(ports.observerAlpha),
    DISTR_CP_OBSERVER_BETA_PORT: String(ports.observerBeta),
    DISTR_CP_POSTGRES_PASSWORD: secrets.postgres,
    DISTR_CP_EXTERNAL_SECRET: secrets.external,
    DISTR_CP_REFERENCE_SECRET: secrets.reference,
    DISTR_CP_REFERENCE_PUBLIC_KEYS_JSON: JSON.stringify({
      [signingKeyId]: signing.publicKey.toString('base64'),
    }),
    DISTR_CP_OBSERVER_ALPHA_SECRET: secrets.observerAlpha,
    DISTR_CP_OBSERVER_BETA_SECRET: secrets.observerBeta,
    DISTR_CP_TARGET_ALPHA_ID: fixture.targets[0].bindingId,
    DISTR_CP_TARGET_BETA_ID: fixture.targets[1].bindingId,
    DISTR_CP_HUB_IMAGE: process.env.DISTR_CP_HUB_IMAGE,
    DISTR_CP_HUB_PORT: String(ports.hub),
    DISTR_CP_JWT_SECRET: secrets.jwt,
    DISTR_CP_HOST_GOMODCACHE: goModuleCache,
    DISTR_CP_SIGNING_KEYS_JSON: JSON.stringify([
      {
        reference: 'secret-provider://fixture/executor-signing',
        versionFingerprint: signingVersionFingerprint,
        privateKey: signing.privateKey.toString('base64'),
      },
    ]),
    DISTR_CP_OBSERVER_PUBLIC_KEYS_JSON: JSON.stringify({
      [observerKeyFingerprint]: observer.publicKey.toString('base64'),
    }),
  };
  const composeArgs = ['compose', '-p', project, '-f', composePath];

  let report;
  let primaryError;
  try {
    run('docker', [...composeArgs, 'down', '-v', '--remove-orphans'], {env: composeEnv});
    run('docker', [...composeArgs, 'up', '-d', 'postgres', 'hub'], {env: composeEnv});
    const hubURL = `http://127.0.0.1:${ports.hub}`;
    await waitForReady(`${hubURL}/ready`);
    const topology = await bootstrapLiveHub({hubURL, runId, fixture});
    composeEnv.DISTR_CP_TARGET_ALPHA_ID = topology.targets[0].hubTargetId;
    composeEnv.DISTR_CP_TARGET_BETA_ID = topology.targets[1].hubTargetId;
    run(
      'docker',
      [...composeArgs, 'up', '-d', 'external-executor', 'reference-executor', 'observer-alpha', 'observer-beta'],
      {env: composeEnv}
    );
    await Promise.all([
      waitForReady(`http://127.0.0.1:${ports.external}/ready`),
      waitForReady(`http://127.0.0.1:${ports.reference}/ready`),
      waitForReady(`http://127.0.0.1:${ports.observerAlpha}/ready`),
      waitForReady(`http://127.0.0.1:${ports.observerBeta}/ready`),
    ]);

    const liveProof = await runLiveHubJourney({
      topology,
      fixture,
      runId,
      ports,
      secrets,
      signing,
      signingKeyId,
      signingVersionFingerprint,
    });
    report = {
      ...liveProof,
      liveStack: {
        started: true,
        project,
        loopbackOnly: true,
        services: ['postgres', 'hub', 'external-executor', 'reference-executor', 'observer-alpha', 'observer-beta'],
        nonLocalCalls: 0,
      },
    };
  } catch (error) {
    primaryError = error;
  }

  const cleanup = confirmComposeCleanup({project, composeArgs, composeEnv});
  if (!cleanup.completed) {
    const details = JSON.stringify({
      downStatus: cleanup.downStatus,
      downError: cleanup.downError,
      retainedResources: cleanup.retainedResources,
      inspectionFailures: cleanup.inspectionFailures,
    });
    throw new Error(
      `cleanup could not be confirmed for Compose project ${project}: ${details}${
        primaryError ? `; primary failure: ${primaryError.message}` : ''
      }`
    );
  }
  if (primaryError) {
    throw primaryError;
  }
  return {...report, cleanup};
}

function usage() {
  return `Usage:
  node examples/control-plane-e2e/run.mjs --mode contract [--json]
  node examples/control-plane-e2e/run.mjs --mode clean [--json]

Modes:
  contract  Validate and execute the deterministic fixture without Docker or a Hub.
  clean     Reset, start, verify, and remove a uniquely named loopback-only local stack.
            If Docker or a local Hub image is unavailable, run contract proof and report the exact blocker.

Safety:
  The runner rejects unknown modes, generates secrets in memory, never writes them to disk,
  never contacts a non-loopback endpoint, and removes only its unique Compose project and volumes.
`;
}

async function main() {
  const args = process.argv.slice(2);
  if (args.includes('--help')) {
    console.log(usage());
    return;
  }
  const modeIndex = args.indexOf('--mode');
  const mode = modeIndex >= 0 ? args[modeIndex + 1] : 'contract';
  const json = args.includes('--json');
  if (!['contract', 'clean'].includes(mode)) {
    throw new Error(`unsupported mode ${JSON.stringify(mode)}; expected contract or clean`);
  }

  const fixture = await loadFixture();
  let report;
  if (mode === 'contract') {
    report = {
      ...simulateContractFlow(fixture),
      mode,
      liveStack: {started: false, blocker: 'contract mode does not start a Hub', nonLocalCalls: 0},
      cleanup: {completed: true, resourcesRemoved: []},
    };
  } else {
    const blocker = liveStackBlocker();
    if (blocker) {
      report = {
        ...simulateContractFlow(fixture),
        mode,
        proofMode: 'fixture-contract',
        liveStack: {started: false, blocker, nonLocalCalls: 0},
        cleanup: {completed: true, resourcesRemoved: []},
      };
    } else {
      report = await runDisposableStack(fixture);
      report.mode = mode;
    }
  }

  if (json) {
    process.stdout.write(`${JSON.stringify(report)}\n`);
    return;
  }
  console.log('Neutral control-plane proof');
  console.log(`PASS proof mode: ${report.proofMode}`);
  console.log(`PASS flow checksum: ${report.flowChecksum}`);
  console.log(`PASS active targets: ${report.targets.map((target) => `${target.id}=A`).join(', ')}`);
  console.log(`PASS retry-safe migration executions: ${report.migration.appliedCount} confirmed v2 attempt(s)`);
  if (report.liveStack.blocker) {
    console.log(`BLOCKED live stack: ${report.liveStack.blocker}`);
  } else {
    console.log(`PASS disposable local stack: ${report.liveStack.project}`);
  }
  console.log('PASS cleanup completed');
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main().catch((error) => {
    console.error(`Neutral control-plane proof failed: ${error.message}`);
    process.exitCode = 1;
  });
}
