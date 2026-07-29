import assert from 'node:assert/strict';
import {spawn, spawnSync} from 'node:child_process';
import {createHmac} from 'node:crypto';
import {mkdtemp, readFile, rm, writeFile} from 'node:fs/promises';
import {createServer as createHttpServer} from 'node:http';
import {createServer} from 'node:net';
import {tmpdir} from 'node:os';
import path from 'node:path';
import test from 'node:test';
import {fileURLToPath} from 'node:url';
import {createExternalExecutor} from './external-executor.mjs';
import {bootstrapLiveHub, createRuntimeKeyMaterial} from './run.mjs';

const fixtureDir = fileURLToPath(new URL('.', import.meta.url));
const repoRoot = path.resolve(fixtureDir, '../..');
const node = process.execPath;
const executorSecretForTest = 'executor-memory-secret';

function campaignTask({id, planId, planTargetId, targetId}) {
  return {
    id,
    createdAt: '2026-07-28T00:00:00.000Z',
    updatedAt: '2026-07-28T00:00:00.000Z',
    queuedAt: '2026-07-28T00:00:00.000Z',
    taskType: 'deployment',
    deploymentPlanId: planId,
    deploymentPlanTargetId: planTargetId,
    deploymentTargetId: targetId,
    applicationId: '10000000-0000-4000-8000-000000000001',
    releaseBundleId: '10000000-0000-4000-8000-000000000002',
    channelId: '10000000-0000-4000-8000-000000000003',
    environmentId: '10000000-0000-4000-8000-000000000004',
    status: 'QUEUED',
    protocolVersion: 'v2',
    queueOrder: 1,
    locks: [],
    stepRuns: [],
  };
}

function passedCampaignPlan({plan, taskId}) {
  return {
    ...plan,
    preflightRuns: [
      {
        id: `preflight-${plan.id}`,
        createdAt: '2026-07-28T00:00:00.000Z',
        deploymentPlanId: plan.id,
        planChecksum: plan.canonicalChecksum,
        status: 'PASSED',
        checks: [
          ...plan.stepAdapters.map((adapter, index) => ({
            id: `adapter-check-${plan.id}-${index}`,
            createdAt: '2026-07-28T00:00:00.000Z',
            deploymentPreflightRunId: `preflight-${plan.id}`,
            deploymentPlanId: plan.id,
            checkKey: `adapter:${adapter.stepKey}`,
            status: 'PASSED',
            expected: {},
            actual: {},
            message: 'adapter evidence matched',
            sortOrder: index + 1,
          })),
          {
            id: `task-check-${plan.id}`,
            createdAt: '2026-07-28T00:00:00.000Z',
            deploymentPreflightRunId: `preflight-${plan.id}`,
            deploymentPlanId: plan.id,
            taskId,
            checkKey: 'plan_checksum',
            status: 'PASSED',
            expected: {},
            actual: {},
            message: 'task-bound plan evidence matched',
            sortOrder: 100,
          },
        ],
      },
    ],
  };
}

test('runtime trust uses the Hub signing public key and a separate observer key', () => {
  const material = createRuntimeKeyMaterial();
  assert.equal(material.signing.publicKey.length, 32);
  assert.equal(material.observer.publicKey.length, 32);
  assert.notDeepEqual(material.signing.publicKey, material.observer.publicKey);
  assert.notEqual(material.signingVersionFingerprint, material.observerKeyFingerprint);
});

test('lease adapter revision is the exact server frozen-evidence checksum', async () => {
  const runtime = await import('./run.mjs');
  assert.equal(typeof runtime.deriveFrozenAdapterRevision, 'function');
  const revision = runtime.deriveFrozenAdapterRevision({
    adapterAssignmentId: '11111111-1111-4111-8111-111111111111',
    adapterImplementationId: '22222222-2222-4222-8222-222222222222',
    implementationVersion: '1.0.0',
    capability: 'distr.compose.deploy',
    capabilityVersion: '1.0.0',
    scopeType: 'deployment_unit',
    scopeReference: '33333333-3333-4333-8333-333333333333',
    configSnapshotId: '44444444-4444-4444-8444-444444444444',
    configChecksum: `sha256:${'a'.repeat(64)}`,
    keyConfiguration: {
      keyId: `sha256:${'b'.repeat(64)}`,
      publicKeyFingerprint: `sha256:${'b'.repeat(64)}`,
      signingKeyReference: 'secret-provider://fixture/executor-signing',
      signingKeyVersionFingerprint: `sha256:${'c'.repeat(64)}`,
    },
  });

  assert.equal(revision, 'sha256:af18ff408f2f5cf1da246880f4c57e5fd10e9e0d1a225af56f7a4bb1b7b7a633');
});

test('campaign run follows the exact public pre-run transition chain with returned revisions', async () => {
  const runtime = await import('./run.mjs');
  assert.equal(typeof runtime.advanceCampaignRunToRunning, 'function');
  const calls = [];
  const states = ['VALIDATED', 'AWAITING_APPROVAL', 'SCHEDULED', 'RUNNING'];
  const topology = {
    token: 'operator-token',
    request: async (method, requestPath, options) => {
      calls.push({method, path: requestPath, ...options});
      const index = calls.length - 1;
      return {
        id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
        state: states[index],
        version: index + 2,
        admissionsBlocked: false,
      };
    },
  };
  const campaign = {
    run: {
      id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
      state: 'DRAFT',
      version: 1,
      admissionsBlocked: false,
    },
  };

  const running = await runtime.advanceCampaignRunToRunning({topology, campaign});

  assert.equal(running.state, 'RUNNING');
  assert.equal(running.version, 5);
  assert.deepEqual(
    calls.map(({path: requestPath, body}) => ({path: requestPath, body})),
    states.map((state, index) => ({
      path: '/api/v1/deployment-campaign-runs/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa/transitions',
      body: {
        expectedVersion: index + 1,
        to: state,
        reason: `Advance disposable campaign to ${state}`,
      },
    }))
  );
});

test('campaign run fails actionably when a required transition response is missing', async () => {
  const runtime = await import('./run.mjs');
  const campaign = {
    run: {
      id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
      state: 'DRAFT',
      version: 1,
      admissionsBlocked: false,
    },
  };

  await assert.rejects(
    runtime.advanceCampaignRunToRunning({
      topology: {
        token: 'operator-token',
        request: async () => undefined,
      },
      campaign,
    }),
    /transition to VALIDATED returned no campaign state/
  );
});

test('campaign run identifies the exact required transition when the API rejects it', async () => {
  const runtime = await import('./run.mjs');
  await assert.rejects(
    runtime.advanceCampaignRunToRunning({
      topology: {
        token: 'operator-token',
        request: async () => {
          throw new Error('409 version conflict');
        },
      },
      campaign: {
        run: {
          id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
          state: 'DRAFT',
          version: 1,
          admissionsBlocked: false,
        },
      },
    }),
    /campaign transition to VALIDATED failed: 409 version conflict/
  );
});

test('campaign run refuses to transition from an unexpected initial revision', async () => {
  const runtime = await import('./run.mjs');
  await assert.rejects(
    runtime.advanceCampaignRunToRunning({
      topology: {
        token: 'operator-token',
        request: async () => {
          throw new Error('transition API must not be called');
        },
      },
      campaign: {
        run: {
          id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
          state: 'DRAFT',
          version: 2,
          admissionsBlocked: false,
        },
      },
    }),
    /new campaign run must begin at DRAFT revision 1/
  );
});

test('campaign publication freezes exact approved and admitted members with one idempotency key', async () => {
  const runtime = await import('./run.mjs');
  assert.equal(typeof runtime.publishCampaign, 'function');
  const publishCalls = [];
  const plans = new Map([
    [
      'target-alpha',
      {
        id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
        approval: {
          id: '11111111-1111-4111-8111-111111111111',
          revision: 3,
          subjectChecksum: `sha256:${'1'.repeat(64)}`,
        },
        admission: {
          id: '33333333-3333-4333-8333-333333333333',
          decisionChecksum: `sha256:${'3'.repeat(64)}`,
        },
      },
    ],
    [
      'target-beta',
      {
        id: 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb',
        approval: {
          id: '22222222-2222-4222-8222-222222222222',
          revision: 4,
          subjectChecksum: `sha256:${'2'.repeat(64)}`,
        },
        admission: {
          id: '44444444-4444-4444-8444-444444444444',
          decisionChecksum: `sha256:${'4'.repeat(64)}`,
        },
      },
    ],
  ]);
  const topology = {
    token: 'operator-token',
    targets: [{id: 'target-alpha'}, {id: 'target-beta'}],
    request: async (method, requestPath, options) => {
      publishCalls.push({method, path: requestPath, ...options});
      if (requestPath === '/api/v1/deployment-campaign-drafts') {
        return {id: '55555555-5555-4555-8555-555555555555'};
      }
      if (requestPath.endsWith('/validate')) {
        return {valid: true, issues: []};
      }
      if (requestPath.endsWith('/publish')) {
        return {
          id: '66666666-6666-4666-8666-666666666666',
          members: [...plans.values()].map((plan) => ({
            planId: plan.id,
            approvalRequestId: plan.approval.id,
            approvalRequestRevision: plan.approval.revision,
            approvalChecksum: plan.approval.subjectChecksum,
            admissionEvaluationId: plan.admission.id,
            admissionChecksum: plan.admission.decisionChecksum,
          })),
        };
      }
      return {
        id: '77777777-7777-4777-8777-777777777777',
        campaignRevisionId: '66666666-6666-4666-8666-666666666666',
        state: 'DRAFT',
        version: 1,
        admissionsBlocked: false,
      };
    },
  };

  const campaign = await runtime.publishCampaign({
    topology,
    plans,
    label: 'initial-a',
    runId: 'run-081',
  });

  assert.equal(campaign.run.state, 'DRAFT');
  assert.deepEqual(publishCalls.find((call) => call.path.endsWith('/publish')).body, {
    idempotencyKey: 'neutral-initial-a-run-081',
  });
  assert.deepEqual(
    campaign.revision.members.map((member) => ({
      planId: member.planId,
      approvalRequestId: member.approvalRequestId,
      admissionEvaluationId: member.admissionEvaluationId,
    })),
    [
      {
        planId: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
        approvalRequestId: '11111111-1111-4111-8111-111111111111',
        admissionEvaluationId: '33333333-3333-4333-8333-333333333333',
      },
      {
        planId: 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb',
        approvalRequestId: '22222222-2222-4222-8222-222222222222',
        admissionEvaluationId: '44444444-4444-4444-8444-444444444444',
      },
    ]
  );
});

test('campaign readiness polling waits for delayed campaign tasks and exact task-bound adapter preflight', async () => {
  const runtime = await import('./run.mjs');
  assert.equal(typeof runtime.waitForCampaignReadiness, 'function');
  const alphaPlan = {
    id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
    canonicalChecksum: `sha256:${'a'.repeat(64)}`,
    targets: [
      {
        id: '33333333-3333-4333-8333-333333333333',
        deploymentTargetId: '55555555-5555-4555-8555-555555555555',
      },
    ],
    stepAdapters: [{stepKey: 'deploy-alpha'}],
  };
  const betaPlan = {
    id: 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb',
    canonicalChecksum: `sha256:${'b'.repeat(64)}`,
    targets: [
      {
        id: '44444444-4444-4444-8444-444444444444',
        deploymentTargetId: '66666666-6666-4666-8666-666666666666',
      },
    ],
    stepAdapters: [{stepKey: 'deploy-beta'}],
  };
  const alphaTask = campaignTask({
    id: '11111111-1111-4111-8111-111111111111',
    planId: alphaPlan.id,
    planTargetId: '33333333-3333-4333-8333-333333333333',
    targetId: '55555555-5555-4555-8555-555555555555',
  });
  const betaTask = campaignTask({
    id: '22222222-2222-4222-8222-222222222222',
    planId: betaPlan.id,
    planTargetId: '44444444-4444-4444-8444-444444444444',
    targetId: '66666666-6666-4666-8666-666666666666',
  });
  const plans = new Map([
    ['target-alpha', alphaPlan],
    ['target-beta', betaPlan],
  ]);
  const taskSequence = [[], [alphaTask], [alphaTask, betaTask]];
  const planSequence = [
    new Map([
      [alphaPlan.id, {...alphaPlan, preflightRuns: []}],
      [betaPlan.id, {...betaPlan, preflightRuns: []}],
    ]),
    new Map([
      [alphaPlan.id, {...alphaPlan, preflightRuns: []}],
      [betaPlan.id, {...betaPlan, preflightRuns: []}],
    ]),
    new Map([
      [alphaPlan.id, passedCampaignPlan({plan: alphaPlan, taskId: alphaTask.id})],
      [betaPlan.id, passedCampaignPlan({plan: betaPlan, taskId: betaTask.id})],
    ]),
  ];
  let poll = -1;
  let now = 0;
  const sleeps = [];
  const topology = {
    token: 'operator-token',
    request: async (method, requestPath) => {
      assert.equal(method, 'GET');
      if (requestPath === '/api/v1/deployment-campaign-runs/77777777-7777-4777-8777-777777777777') {
        poll += 1;
        return {
          id: '77777777-7777-4777-8777-777777777777',
          createdAt: '2026-07-28T00:00:00.000Z',
          updatedAt: '2026-07-28T00:00:00.000Z',
          campaignRevisionId: '88888888-8888-4888-8888-888888888888',
          state: 'RUNNING',
          version: 5,
          currentWaveOrder: 1,
          currentMemberOrder: 1,
          admissionsBlocked: false,
          pauseRequested: false,
          reconciliationRequired: false,
          fencingToken: 1,
        };
      }
      if (requestPath === '/api/v1/tasks') {
        return taskSequence[poll];
      }
      const planId = requestPath.split('/').at(-1);
      return planSequence[poll].get(planId);
    },
  };

  const result = await runtime.waitForCampaignReadiness({
    topology,
    campaign: {run: {id: '77777777-7777-4777-8777-777777777777'}},
    plans,
    timeoutMs: 5_000,
    intervalMs: 1_000,
    clock: {
      now: () => now,
      sleep: async (milliseconds) => {
        sleeps.push(milliseconds);
        now += milliseconds;
      },
    },
  });

  assert.equal(result.campaign.state, 'RUNNING');
  assert.deepEqual(
    result.tasks.map((task) => task.id),
    [alphaTask.id, betaTask.id]
  );
  assert.deepEqual(sleeps, [1_000, 1_000]);
  assert.equal(poll, 2);
});

test('campaign readiness polling times out at the exact bound with actionable last state', async () => {
  const runtime = await import('./run.mjs');
  const plan = {
    id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
    canonicalChecksum: `sha256:${'a'.repeat(64)}`,
    targets: [
      {
        id: '33333333-3333-4333-8333-333333333333',
        deploymentTargetId: '55555555-5555-4555-8555-555555555555',
      },
    ],
    stepAdapters: [{stepKey: 'deploy-alpha'}],
  };
  let now = 0;
  const sleeps = [];
  const topology = {
    token: 'operator-token',
    request: async (method, requestPath) => {
      assert.equal(method, 'GET');
      if (requestPath.startsWith('/api/v1/deployment-campaign-runs/')) {
        return {
          id: '77777777-7777-4777-8777-777777777777',
          state: 'DRAFT',
          version: 1,
          admissionsBlocked: false,
        };
      }
      if (requestPath === '/api/v1/tasks') {
        return [];
      }
      return {...plan, preflightRuns: []};
    },
  };

  await assert.rejects(
    runtime.waitForCampaignReadiness({
      topology,
      campaign: {run: {id: '77777777-7777-4777-8777-777777777777'}},
      plans: new Map([['target-alpha', plan]]),
      timeoutMs: 2_500,
      intervalMs: 1_000,
      clock: {
        now: () => now,
        sleep: async (milliseconds) => {
          sleeps.push(milliseconds);
          now += milliseconds;
        },
      },
    }),
    (error) => {
      assert.match(error.message, /campaign readiness timed out/);
      assert.match(error.message, /"state":"DRAFT"/);
      assert.match(error.message, /"preflightStatus":"MISSING"/);
      assert.match(error.message, /aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa/);
      return true;
    }
  );
  assert.equal(now, 2_500);
  assert.deepEqual(sleeps, [1_000, 1_000, 500]);
});

test('campaign readiness polling fails immediately on a failed adapter preflight', async () => {
  const runtime = await import('./run.mjs');
  const plan = {
    id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
    canonicalChecksum: `sha256:${'a'.repeat(64)}`,
    targets: [
      {
        id: '33333333-3333-4333-8333-333333333333',
        deploymentTargetId: '55555555-5555-4555-8555-555555555555',
      },
    ],
    stepAdapters: [{stepKey: 'deploy-alpha'}],
  };
  const task = campaignTask({
    id: '11111111-1111-4111-8111-111111111111',
    planId: plan.id,
    planTargetId: plan.targets[0].id,
    targetId: plan.targets[0].deploymentTargetId,
  });
  let sleeps = 0;
  let now = 0;
  const topology = {
    token: 'operator-token',
    request: async (method, requestPath) => {
      assert.equal(method, 'GET');
      if (requestPath.startsWith('/api/v1/deployment-campaign-runs/')) {
        return {
          id: '77777777-7777-4777-8777-777777777777',
          state: 'RUNNING',
          version: 5,
          admissionsBlocked: false,
        };
      }
      if (requestPath === '/api/v1/tasks') {
        return [task];
      }
      return {
        ...plan,
        preflightRuns: [
          {
            id: '99999999-9999-4999-8999-999999999999',
            deploymentPlanId: plan.id,
            planChecksum: plan.canonicalChecksum,
            status: 'FAILED',
            checks: [
              {
                taskId: task.id,
                checkKey: 'adapter:deploy-alpha',
                status: 'FAILED',
                message: 'adapter config changed after publication',
              },
            ],
          },
        ],
      };
    },
  };

  await assert.rejects(
    runtime.waitForCampaignReadiness({
      topology,
      campaign: {run: {id: '77777777-7777-4777-8777-777777777777'}},
      plans: new Map([['target-alpha', plan]]),
      timeoutMs: 10_000,
      intervalMs: 1_000,
      clock: {
        now: () => now,
        sleep: async (milliseconds) => {
          sleeps += 1;
          now += milliseconds;
        },
      },
    }),
    /campaign readiness failed.*adapter config changed after publication/
  );
  assert.equal(sleeps, 0);
});

test('campaign readiness polling ignores older terminal evidence while the newest preflight is pending', async () => {
  const runtime = await import('./run.mjs');
  const plan = {
    id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
    canonicalChecksum: `sha256:${'a'.repeat(64)}`,
    targets: [
      {
        id: '33333333-3333-4333-8333-333333333333',
        deploymentTargetId: '55555555-5555-4555-8555-555555555555',
      },
    ],
    stepAdapters: [{stepKey: 'deploy-alpha'}],
  };
  const task = campaignTask({
    id: '11111111-1111-4111-8111-111111111111',
    planId: plan.id,
    planTargetId: plan.targets[0].id,
    targetId: plan.targets[0].deploymentTargetId,
  });
  const passed = passedCampaignPlan({plan, taskId: task.id});
  const olderPassed = {
    ...passed.preflightRuns[0],
    id: '77777777-7777-4777-8777-777777777779',
  };
  const olderFailure = {
    id: '99999999-9999-4999-8999-999999999999',
    deploymentPlanId: plan.id,
    planChecksum: plan.canonicalChecksum,
    status: 'FAILED',
    checks: [
      {
        taskId: task.id,
        checkKey: 'adapter:deploy-alpha',
        status: 'FAILED',
        message: 'superseded failure',
      },
    ],
  };
  const pending = {
    ...plan,
    preflightRuns: [
      {
        id: '88888888-8888-4888-8888-888888888888',
        deploymentPlanId: plan.id,
        planChecksum: plan.canonicalChecksum,
        status: 'PENDING',
        checks: [],
      },
      olderPassed,
      olderFailure,
    ],
  };
  passed.preflightRuns.push(olderPassed, olderFailure);
  let poll = -1;
  let now = 0;
  const sleeps = [];

  const result = await runtime.waitForCampaignReadiness({
    topology: {
      token: 'operator-token',
      request: async (method, requestPath) => {
        assert.equal(method, 'GET');
        if (requestPath.startsWith('/api/v1/deployment-campaign-runs/')) {
          poll += 1;
          return {
            id: '77777777-7777-4777-8777-777777777777',
            state: 'RUNNING',
            version: 5,
            admissionsBlocked: false,
          };
        }
        return requestPath === '/api/v1/tasks' ? [task] : [pending, passed][poll];
      },
    },
    campaign: {run: {id: '77777777-7777-4777-8777-777777777777'}},
    plans: new Map([['target-alpha', plan]]),
    timeoutMs: 1_000,
    intervalMs: 100,
    clock: {
      now: () => now,
      sleep: async (milliseconds) => {
        sleeps.push(milliseconds);
        now += milliseconds;
      },
    },
  });

  assert.equal(result.ready, true);
  assert.equal(result.plans[0].preflightStatus, 'PASSED');
  assert.deepEqual(sleeps, [100]);
});

test('target lease readiness tolerates transient 204s while Hub predecessors dispatch the target attempt', async () => {
  const runtime = await import('./run.mjs');
  assert.equal(typeof runtime.waitForTargetExecutionLease, 'function');
  const campaign = {run: {id: '77777777-7777-4777-8777-777777777777'}};
  const plan = {
    id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
    canonicalChecksum: `sha256:${'a'.repeat(64)}`,
  };
  const target = {
    hubTargetId: '55555555-5555-4555-8555-555555555555',
    agentToken: 'agent-token',
    executorId: 'executor-alpha',
  };
  const leaseIdentity = {
    adapterRevision: `sha256:${'b'.repeat(64)}`,
    keyId: `sha256:${'c'.repeat(64)}`,
  };
  const task = campaignTask({
    id: '11111111-1111-4111-8111-111111111111',
    planId: plan.id,
    planTargetId: '33333333-3333-4333-8333-333333333333',
    targetId: target.hubTargetId,
  });
  const taskSequence = [
    {
      ...task,
      status: 'RUNNING',
      stepRuns: [{stepKey: 'config:verify', executionLocation: 'hub', status: 'PENDING'}],
    },
    {
      ...task,
      status: 'RUNNING',
      stepRuns: [
        {stepKey: 'config:verify', executionLocation: 'hub', status: 'SUCCEEDED'},
        {stepKey: 'deploy-provider', executionLocation: 'target', status: 'PENDING'},
      ],
    },
  ];
  const expectedLease = {
    attempt: {
      id: '99999999-9999-4999-8999-999999999999',
      status: 'CLAIMED',
    },
  };
  let leasePoll = 0;
  let now = 0;
  const sleeps = [];
  const calls = [];
  const topology = {
    token: 'operator-token',
    request: async (method, requestPath, options) => {
      calls.push({method, path: requestPath, options});
      if (method === 'POST') {
        const result = [null, null, expectedLease][leasePoll];
        leasePoll += 1;
        return result;
      }
      if (requestPath.startsWith('/api/v1/deployment-campaign-runs/')) {
        return {id: campaign.run.id, state: 'RUNNING', version: 5, admissionsBlocked: false};
      }
      if (requestPath === '/api/v1/tasks') {
        return [taskSequence[leasePoll - 1]];
      }
      assert.equal(requestPath, `/api/v1/deployment-plans/${plan.id}`);
      return {...plan, preflightRuns: []};
    },
  };

  const lease = await runtime.waitForTargetExecutionLease({
    topology,
    campaign,
    plan,
    target,
    leaseIdentity,
    timeoutMs: 5_000,
    intervalMs: 1_000,
    clock: {
      now: () => now,
      sleep: async (milliseconds) => {
        sleeps.push(milliseconds);
        now += milliseconds;
      },
    },
  });

  assert.equal(lease, expectedLease);
  assert.deepEqual(sleeps, [1_000, 1_000]);
  assert.equal(calls.filter((call) => call.method === 'POST').length, 3);
  assert.deepEqual(calls[0], {
    method: 'POST',
    path: '/api/executor/v2/executions/lease',
    options: {
      token: target.agentToken,
      body: {
        executorId: target.executorId,
        adapterRevision: leaseIdentity.adapterRevision,
        keyId: leaseIdentity.keyId,
        leaseSeconds: 60,
      },
      expected: [200, 204],
    },
  });
  assert.deepEqual(
    calls.filter((call) => call.method === 'GET').map((call) => call.path),
    [
      `/api/v1/deployment-campaign-runs/${campaign.run.id}`,
      '/api/v1/tasks',
      `/api/v1/deployment-plans/${plan.id}`,
      `/api/v1/deployment-campaign-runs/${campaign.run.id}`,
      '/api/v1/tasks',
      `/api/v1/deployment-plans/${plan.id}`,
    ]
  );
});

test('target lease readiness fails immediately with actionable terminal task state', async () => {
  const runtime = await import('./run.mjs');
  const campaign = {run: {id: '77777777-7777-4777-8777-777777777777'}};
  const plan = {
    id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
    canonicalChecksum: `sha256:${'a'.repeat(64)}`,
  };
  const target = {
    hubTargetId: '55555555-5555-4555-8555-555555555555',
    agentToken: 'agent-token',
    executorId: 'executor-alpha',
  };
  const failedTask = {
    ...campaignTask({
      id: '11111111-1111-4111-8111-111111111111',
      planId: plan.id,
      planTargetId: '33333333-3333-4333-8333-333333333333',
      targetId: target.hubTargetId,
    }),
    status: 'FAILED',
    stepRuns: [
      {
        stepKey: 'config:verify',
        executionLocation: 'hub',
        status: 'FAILED',
      },
    ],
  };
  let sleeps = 0;

  await assert.rejects(
    runtime.waitForTargetExecutionLease({
      topology: {
        token: 'operator-token',
        request: async (method, requestPath) => {
          if (method === 'POST') {
            return null;
          }
          if (requestPath.startsWith('/api/v1/deployment-campaign-runs/')) {
            return {id: campaign.run.id, state: 'RUNNING', version: 5, admissionsBlocked: false};
          }
          if (requestPath === '/api/v1/tasks') {
            return [failedTask];
          }
          return {...plan, preflightRuns: []};
        },
      },
      campaign,
      plan,
      target,
      leaseIdentity: {
        adapterRevision: `sha256:${'b'.repeat(64)}`,
        keyId: `sha256:${'c'.repeat(64)}`,
      },
      timeoutMs: 5_000,
      intervalMs: 1_000,
      clock: {
        now: () => 0,
        sleep: async () => {
          sleeps += 1;
        },
      },
    }),
    (error) => {
      assert.match(error.message, /target lease readiness failed/);
      assert.match(error.message, /11111111-1111-4111-8111-111111111111:FAILED/);
      assert.match(error.message, /config:verify:FAILED/);
      return true;
    }
  );
  assert.equal(sleeps, 0);
});

test('published plans are manually admitted with exact approval linkage before campaign publication', async () => {
  const runtime = await import('./run.mjs');
  assert.equal(typeof runtime.admitPublishedPlans, 'function');
  const calls = [];
  const plans = new Map([
    [
      'target-alpha',
      {
        id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
        approval: {id: '11111111-1111-4111-8111-111111111111'},
      },
    ],
    [
      'target-beta',
      {
        id: 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb',
        approval: {id: '22222222-2222-4222-8222-222222222222'},
      },
    ],
  ]);
  const topology = {
    token: 'admin-token',
    request: async (method, requestPath, options) => {
      calls.push({method, path: requestPath, ...options});
      return {
        id: requestPath.includes('aaaaaaaa')
          ? '33333333-3333-4333-8333-333333333333'
          : '44444444-4444-4444-8444-444444444444',
        decision: 'ADMIT',
        approvalRequestId: requestPath.includes('aaaaaaaa')
          ? '11111111-1111-4111-8111-111111111111'
          : '22222222-2222-4222-8222-222222222222',
        decisionChecksum: requestPath.includes('aaaaaaaa') ? `sha256:${'3'.repeat(64)}` : `sha256:${'4'.repeat(64)}`,
      };
    },
  };

  await runtime.admitPublishedPlans({topology, plans, runId: 'run-081'});

  assert.deepEqual(
    calls
      .filter((call) => call.method === 'POST')
      .map(({path: requestPath, body}) => ({
        path: requestPath,
        body,
      })),
    [
      {
        path: '/api/v1/deployment-plans/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa/admission',
        body: {schedulerIdempotencyKey: 'neutral-run-081-target-alpha'},
      },
      {
        path: '/api/v1/deployment-plans/bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb/admission',
        body: {schedulerIdempotencyKey: 'neutral-run-081-target-beta'},
      },
    ]
  );
  assert.equal(plans.get('target-alpha').admission.id, '33333333-3333-4333-8333-333333333333');
  assert.equal(plans.get('target-beta').admission.id, '44444444-4444-4444-8444-444444444444');
});

test('lease identities retain each frozen target-step capability, scope, and revision', async () => {
  const runtime = await import('./run.mjs');
  assert.equal(typeof runtime.derivePlanLeaseIdentities, 'function');
  const keyId = `sha256:${'b'.repeat(64)}`;
  const base = {
    adapterAssignmentId: '11111111-1111-4111-8111-111111111111',
    adapterImplementationId: '22222222-2222-4222-8222-222222222222',
    implementationVersion: '1.0.0',
    capabilityVersion: '1.0.0',
    configSnapshotId: '44444444-4444-4444-8444-444444444444',
    configChecksum: `sha256:${'a'.repeat(64)}`,
    keyConfiguration: {
      keyId,
      publicKeyFingerprint: keyId,
      signingKeyReference: 'secret-provider://fixture/executor-signing',
      signingKeyVersionFingerprint: `sha256:${'c'.repeat(64)}`,
    },
  };

  const identities = runtime.derivePlanLeaseIdentities({
    plan: {
      steps: [
        {
          stepKey: 'component:catalog:deploy',
          sortOrder: 1,
          dependencies: ['component:catalog:migration:schema-v2'],
        },
        {
          stepKey: 'component:catalog:health',
          sortOrder: 2,
          dependencies: ['component:catalog:deploy'],
        },
        {
          stepKey: 'component:catalog:migration:schema-v2',
          sortOrder: 3,
          dependencies: ['config:verify'],
        },
      ],
      stepAdapters: [
        {
          stepKey: 'component:catalog:health',
          ...base,
          adapterAssignmentId: '11111111-1111-4111-8111-111111111113',
          capability: 'health.observe',
          scopeType: 'observer_registration',
          scopeReference: '77777777-7777-4777-8777-777777777777',
        },
        {
          stepKey: 'component:catalog:migration:schema-v2',
          ...base,
          adapterAssignmentId: '11111111-1111-4111-8111-111111111112',
          capability: 'database.migrate',
          scopeType: 'database_resource',
          scopeReference: 'target-alpha-db',
        },
        {
          stepKey: 'component:catalog:deploy',
          ...base,
          capability: 'distr.compose.deploy',
          scopeType: 'component_instance',
          scopeReference: '33333333-3333-4333-8333-333333333333',
        },
      ],
    },
    target: {
      id: 'target-alpha',
      instances: new Map([
        [
          'catalog',
          {
            id: '33333333-3333-4333-8333-333333333333',
            databaseBoundary: 'target-alpha-db',
          },
        ],
      ]),
      observers: new Map([
        ['catalog', {id: '77777777-7777-4777-8777-777777777777'}],
      ]),
      snapshot: {id: base.configSnapshotId, canonicalChecksum: base.configChecksum},
    },
    signingKeyId: keyId,
  });

  assert.deepEqual(
    identities.map(({stepKey, capability, keyId: identityKeyId}) => ({
      stepKey,
      capability,
      keyId: identityKeyId,
    })),
    [
      {
        stepKey: 'component:catalog:migration:schema-v2',
        capability: 'database.migrate',
        keyId,
      },
      {
        stepKey: 'component:catalog:deploy',
        capability: 'distr.compose.deploy',
        keyId,
      },
      {
        stepKey: 'component:catalog:health',
        capability: 'health.observe',
        keyId,
      },
    ]
  );
  assert.equal(new Set(identities.map((identity) => identity.adapterRevision)).size, 3);
  assert.ok(identities.every((identity) => /^sha256:[0-9a-f]{64}$/.test(identity.adapterRevision)));
});

test('component release and target setup cover deploy migration and health adapters', async () => {
  const runtime = await import('./run.mjs');
  assert.deepEqual(runtime.componentAdapterRequirements([]), [
    {stepKind: 'deploy', capability: 'distr.compose.deploy', version: '1.0.0'},
    {stepKind: 'health', capability: 'health.observe', version: '1.0.0'},
  ]);
  assert.deepEqual(runtime.componentAdapterRequirements([{key: 'schema-v2'}]), [
    {stepKind: 'deploy', capability: 'distr.compose.deploy', version: '1.0.0'},
    {stepKind: 'migration', capability: 'database.migrate', version: '1.0.0'},
    {stepKind: 'health', capability: 'health.observe', version: '1.0.0'},
  ]);

  const target = {
    instances: new Map([
      ['catalog', {id: '33333333-3333-4333-8333-333333333333', databaseBoundary: 'target-alpha-db'}],
    ]),
    observers: new Map([
      ['catalog', {id: '77777777-7777-4777-8777-777777777777'}],
    ]),
  };
  assert.deepEqual(runtime.componentAdapterScopes(target, 'catalog'), [
    {
      capability: 'distr.compose.deploy',
      scopeType: 'component_instance',
      scopeReference: '33333333-3333-4333-8333-333333333333',
    },
    {
      capability: 'database.migrate',
      scopeType: 'database_resource',
      scopeReference: 'target-alpha-db',
    },
    {
      capability: 'health.observe',
      scopeType: 'observer_registration',
      scopeReference: '77777777-7777-4777-8777-777777777777',
    },
  ]);
});

test('live bootstrap captures Hub-created target IDs before target-bound services start', async () => {
  const fixture = JSON.parse(await readFile(path.join(fixtureDir, 'fixture.json'), 'utf8'));
  const calls = [];
  let targetNumber = 0;
  let approverNumber = 0;
  const server = createHttpServer(async (request, response) => {
    const chunks = [];
    for await (const chunk of request) {
      chunks.push(chunk);
    }
    const requestBody = chunks.length ? JSON.parse(Buffer.concat(chunks).toString('utf8')) : undefined;
    calls.push({
      method: request.method,
      path: request.url,
      authorization: request.headers.authorization,
      body: requestBody,
    });
    let body = {};
    let status = 200;
    switch (`${request.method} ${request.url}`) {
      case 'POST /api/v1/auth/login':
        body = {token: 'operator-token'};
        break;
      case 'GET /api/v1/organization':
        body = {id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', name: 'fixture'};
        break;
      case 'POST /api/v1/applications':
        body = {id: 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb'};
        break;
      case 'POST /api/v1/user-accounts':
        approverNumber += 1;
        body = {
          user: {id: `10000000-0000-4000-8000-00000000000${approverNumber}`},
          inviteUrl: `http://fixture.invalid/join?jwt=invite-${approverNumber}`,
        };
        break;
      case 'POST /api/v1/auth/invite/accept':
        body = {token: `approver-${approverNumber}`};
        break;
      case 'POST /api/v1/authorization/groups':
        body = {id: `20000000-0000-4000-8000-00000000000${approverNumber}`};
        break;
      case 'POST /api/v1/environments':
        body = {id: 'cccccccc-cccc-4ccc-8ccc-cccccccccccc'};
        break;
      case 'POST /api/v1/lifecycles':
        body = {id: 'dddddddd-dddd-4ddd-8ddd-dddddddddddd'};
        break;
      case 'POST /api/v1/channels':
        body = {id: 'eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee'};
        break;
      case 'POST /api/v1/deployment-targets':
        targetNumber += 1;
        body = {id: `00000000-0000-4000-8000-00000000000${targetNumber}`};
        break;
      case 'POST /api/v1/agent/login':
        body = {token: `agent-${targetNumber}`};
        break;
      default:
        if (request.url.endsWith('/access-request')) {
          body = {targetSecret: `target-secret-${targetNumber}`};
        } else if (request.url.endsWith('/members')) {
          body = {id: `30000000-0000-4000-8000-00000000000${approverNumber}`};
        } else if (request.url === '/api/v1/authorization/control-plane-enrollments') {
          body = {id: `40000000-0000-4000-8000-00000000000${calls.length}`};
          status = 201;
        } else if (request.url.endsWith('/capabilities')) {
          body = {id: `50000000-0000-4000-8000-00000000000${targetNumber}`};
        } else if (request.url === '/api/v1/auth/register') {
          body = {};
          status = 201;
        } else {
          status = 404;
          body = {error: 'unexpected route'};
        }
    }
    const encoded = JSON.stringify(body);
    response.writeHead(status, {'Content-Type': 'application/json'});
    response.end(encoded);
  });
  await new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', resolve);
  });
  try {
    const topology = await bootstrapLiveHub({
      hubURL: `http://127.0.0.1:${server.address().port}`,
      runId: 'test',
      fixture,
    });
    assert.deepEqual(
      topology.targets.map(({hubTargetId}) => hubTargetId),
      ['00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000002']
    );
    assert.equal(calls.filter((call) => call.path === '/api/v1/agent/login').length, 2);
    const enrollments = calls.filter((call) => call.path === '/api/v1/authorization/control-plane-enrollments');
    assert.deepEqual(
      enrollments.map((call) => ({
        authorization: call.authorization,
        scope: call.body.scope,
      })),
      [
        {
          authorization: 'Bearer operator-token',
          scope: {kind: 'organization', id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa'},
        },
        {
          authorization: 'Bearer operator-token',
          scope: {kind: 'environment', id: 'cccccccc-cccc-4ccc-8ccc-cccccccccccc'},
        },
      ]
    );
    const capabilityReports = calls.filter((call) => call.path.endsWith('/capabilities'));
    assert.deepEqual(
      capabilityReports.map(({path: requestPath, authorization}) => ({path: requestPath, authorization})),
      [
        {path: '/api/v1/agents/00000000-0000-4000-8000-000000000001/capabilities', authorization: 'Bearer agent-1'},
        {path: '/api/v1/agents/00000000-0000-4000-8000-000000000002/capabilities', authorization: 'Bearer agent-2'},
      ]
    );
    for (const report of capabilityReports) {
      assert.equal(report.body.protocolVersion, 'v2');
      assert.deepEqual(report.body.supportedActions, [
        {actionType: 'distr.compose.deploy', versions: ['1.0.0']},
        {actionType: 'database.migrate', versions: ['1.0.0']},
        {actionType: 'health.observe', versions: ['1.0.0']},
        {actionType: 'distr.preflight', versions: ['1']},
      ]);
    }
  } finally {
    await new Promise((resolve) => server.close(resolve));
  }
});

test('component v2 publication declares immutable SBOM and submits production-verified signed provenance', async () => {
  const runtime = await import('./run.mjs');
  assert.equal(typeof runtime.publishComponentRelease, 'function');
  const digest = `sha256:${'a'.repeat(64)}`;
  const calls = [];
  const published = await runtime.publishComponentRelease({
    request: async (method, requestPath, options) => {
      calls.push({method, path: requestPath, ...options});
      if (requestPath === '/api/v1/release-bundles') {
        return {id: '11111111-1111-4111-8111-111111111111'};
      }
      return {id: '11111111-1111-4111-8111-111111111111', status: 'published'};
    },
    token: 'operator-token',
    topology: {
      application: {id: '22222222-2222-4222-8222-222222222222'},
      channel: {id: '33333333-3333-4333-8333-333333333333'},
    },
    fixture: {
      releases: {A: {version: '1.0.0'}},
      product: {migration: {id: 'schema-v2'}},
    },
    label: 'A',
    component: {
      key: 'catalog-provider',
      artifactDigest: digest,
    },
  });

  assert.equal(published.status, 'published');
  assert.equal(calls.length, 2);
  const contract = calls[0].body.releaseContract;
  assert.equal(calls[0].method, 'POST');
  assert.match(contract.source.repository, /^https:\/\/[^?#]+$/);
  assert.match(contract.build.builder, /^https:\/\/[^?#]+$/);
  assert.equal(contract.evidence.provenance.length, 1);
  assert.equal(contract.evidence.sbom.length, 1);
  assert.match(contract.evidence.provenance[0], /^oci:\/\/.+@sha256:[0-9a-f]{64}$/);
  assert.match(contract.evidence.sbom[0], /^oci:\/\/.+@sha256:[0-9a-f]{64}$/);

  const publication = calls[1].body.provenance;
  assert.equal(calls[1].path, '/api/v1/release-bundles/11111111-1111-4111-8111-111111111111/publish');
  assert.equal(publication.policy.version, 'distr.provenance-policy/v1');
  assert.equal(publication.policy.trustedRoots.length, 1);
  assert.equal(publication.policy.allowedSignerIdentities.length, 1);
  assert.equal(publication.evidence.length, 1);
  assert.equal(publication.evidence[0].artifactKey, 'catalog-provider');
  assert.equal(publication.evidence[0].platform, 'linux/amd64');
  assert.equal(publication.evidence[0].reference, contract.evidence.provenance[0]);
  assert.equal(publication.evidence[0].trustRootId, publication.policy.trustedRoots[0].id);
  assert.equal(publication.evidence[0].bundle.mediaType, 'application/vnd.dev.sigstore.bundle.v0.3+json');
  assert.ok(publication.evidence[0].bundle.dsseEnvelope.signatures[0].sig);
  assert.ok(publication.evidence[0].bundle.verificationMaterial.tlogEntries.length);
  assert.ok(
    publication.evidence[0].bundle.verificationMaterial.timestampVerificationData.rfc3161Timestamps.length
  );
});

test('provenance helper builds once with local offline Go resolution before executing the cached binary', async (t) => {
  const resolvedRoot = spawnSync('go', ['env', 'GOROOT'], {
    encoding: 'utf8',
  });
  assert.equal(resolvedRoot.status, 0, resolvedRoot.stderr);
  const realGo = path.join(
    resolvedRoot.stdout.trim(),
    'bin',
    process.platform === 'win32' ? 'go.exe' : 'go'
  );
  const workDir = await mkdtemp(path.join(tmpdir(), 'distr-provenance-spawn-'));
  t.after(() => rm(workDir, {recursive: true, force: true}));
  const invocationLog = path.join(workDir, 'invocations.jsonl');
  const wrapperSource = path.join(workDir, 'go-wrapper.go');
  const wrapperBinary = path.join(workDir, process.platform === 'win32' ? 'go.exe' : 'go');
  await writeFile(
    wrapperSource,
    `package main

import (
  "encoding/json"
  "os"
  "os/exec"
)

func main() {
  record := map[string]any{
    "args": os.Args[1:],
    "env": map[string]string{
      "GOTOOLCHAIN": os.Getenv("GOTOOLCHAIN"),
      "GOPROXY": os.Getenv("GOPROXY"),
      "GOSUMDB": os.Getenv("GOSUMDB"),
      "GONOPROXY": os.Getenv("GONOPROXY"),
      "GONOSUMDB": os.Getenv("GONOSUMDB"),
      "GOPRIVATE": os.Getenv("GOPRIVATE"),
      "GOVCS": os.Getenv("GOVCS"),
      "GOFLAGS": os.Getenv("GOFLAGS"),
    },
  }
  encoded, _ := json.Marshal(record)
  logFile, _ := os.OpenFile(os.Getenv("DISTR_CP_TEST_GO_LOG"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
  _, _ = logFile.Write(append(encoded, '\\n'))
  _ = logFile.Close()

  command := exec.Command(os.Getenv("DISTR_CP_TEST_REAL_GO"), os.Args[1:]...)
  command.Env = os.Environ()
  command.Stdin = os.Stdin
  command.Stdout = os.Stdout
  command.Stderr = os.Stderr
  if err := command.Run(); err != nil {
    if exitError, ok := err.(*exec.ExitError); ok {
      os.Exit(exitError.ExitCode())
    }
    os.Exit(1)
  }
}
`
  );
  const wrapperBuild = spawnSync(realGo, ['build', '-trimpath', '-o', wrapperBinary, wrapperSource], {
    cwd: workDir,
    env: {...process.env, GOTOOLCHAIN: 'local', GOPROXY: 'off', GOSUMDB: 'off'},
    encoding: 'utf8',
  });
  assert.equal(wrapperBuild.status, 0, wrapperBuild.stderr || wrapperBuild.stdout);
  const runModule = new URL('./run.mjs', import.meta.url).href;
  const helperInput = {
    artifactKey: 'catalog-provider',
    platform: 'linux/amd64',
    digest: `sha256:${'a'.repeat(64)}`,
    sourceRepository: 'https://code.fixture.invalid/neutral-product',
    sourceCommit: 'a'.repeat(40),
    buildId: 'build-catalog-provider-A',
    builderId: 'https://build.fixture.invalid/workers/release',
  };
  const child = spawnSync(
    node,
    [
      '--input-type=module',
      '--eval',
      `import {buildComponentPublicationEvidence} from ${JSON.stringify(runModule)};
const input = ${JSON.stringify(helperInput)};
buildComponentPublicationEvidence(input);
buildComponentPublicationEvidence({...input, artifactKey: 'gateway-consumer', digest: 'sha256:${'b'.repeat(64)}'});
`,
    ],
    {
      cwd: repoRoot,
      env: {
        ...process.env,
        PATH: workDir,
        DISTR_CP_TEST_GO_LOG: invocationLog,
        DISTR_CP_TEST_REAL_GO: realGo,
        GOTOOLCHAIN: 'auto',
        GOPROXY: 'https://ambient-proxy.invalid',
        GOSUMDB: 'sum.golang.org',
        GONOPROXY: 'example.invalid',
        GONOSUMDB: 'example.invalid',
        GOPRIVATE: 'example.invalid',
        GOVCS: '*:all',
        GOFLAGS: '-mod=mod',
      },
      encoding: 'utf8',
    }
  );
  assert.equal(child.status, 0, child.stderr || child.stdout);
  const invocations = (await readFile(invocationLog, 'utf8'))
    .trim()
    .split(/\r?\n/)
    .map((line) => JSON.parse(line));
  const buildInvocations = invocations.filter((invocation) => invocation.args[0] === 'build');
  assert.equal(buildInvocations.length, 1);
  assert.ok(buildInvocations[0].args.includes('-trimpath'));
  assert.equal(buildInvocations[0].args.at(-1), './examples/control-plane-e2e/provenance-fixture');
  assert.deepEqual(buildInvocations[0].env, {
    GOTOOLCHAIN: 'go1.26.5',
    GOPROXY: 'off',
    GOSUMDB: 'off',
    GONOPROXY: '',
    GONOSUMDB: '',
    GOPRIVATE: '',
    GOVCS: '*:off',
    GOFLAGS: '-p=1 -mod=readonly',
  });
});

test('provenance helper reports missing local toolchain before attempting publication', async (t) => {
  const emptyPath = await mkdtemp(path.join(tmpdir(), 'distr-provenance-no-go-'));
  t.after(() => rm(emptyPath, {recursive: true, force: true}));
  const runModule = new URL('./run.mjs', import.meta.url).href;
  const child = spawnSync(
    node,
    [
      '--input-type=module',
      '--eval',
      `import {buildComponentPublicationEvidence} from ${JSON.stringify(runModule)};
try {
  buildComponentPublicationEvidence(${JSON.stringify({
    artifactKey: 'catalog-provider',
    platform: 'linux/amd64',
    digest: `sha256:${'a'.repeat(64)}`,
    sourceRepository: 'https://code.fixture.invalid/neutral-product',
    sourceCommit: 'a'.repeat(40),
    buildId: 'build-catalog-provider-A',
    builderId: 'https://build.fixture.invalid/workers/release',
  })});
} catch (error) {
  process.stderr.write(error.message);
  process.exit(23);
}
`,
    ],
    {
      cwd: repoRoot,
      env: {...process.env, PATH: emptyPath},
      encoding: 'utf8',
    }
  );
  assert.equal(child.status, 23, child.stderr || child.stdout);
  assert.match(
    child.stderr,
    /offline provenance helper build failed: required Go toolchain or module cache is unavailable/
  );
});

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

async function waitForReady(url, child) {
  const deadline = Date.now() + 5_000;
  while (Date.now() < deadline) {
    if (child.exitCode !== null) {
      assert.fail(`server exited before ready with status ${child.exitCode}`);
    }
    try {
      const response = await fetch(url);
      if (response.ok) {
        return;
      }
    } catch {
      // The process can take a few event-loop turns to bind the port.
    }
    await new Promise((resolve) => setTimeout(resolve, 25));
  }
  assert.fail(`server did not become ready at ${url}`);
}

function startFixtureServer(script, env) {
  const child = spawn(node, [path.join(fixtureDir, script)], {
    cwd: repoRoot,
    env: {...process.env, ...env},
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  let output = '';
  child.stdout.on('data', (chunk) => {
    output += chunk;
  });
  child.stderr.on('data', (chunk) => {
    output += chunk;
  });
  return {child, output: () => output};
}

async function stopFixtureServer(child) {
  if (child.exitCode !== null) {
    return;
  }
  child.kill('SIGTERM');
  await Promise.race([
    new Promise((resolve) => child.once('exit', resolve)),
    new Promise((resolve) => setTimeout(resolve, 1_000)),
  ]);
  if (child.exitCode === null) {
    child.kill('SIGKILL');
  }
}

function operation(overrides = {}) {
  const body = {
    attemptId: 'attempt-alpha-a',
    operationId: 'operation-alpha-a',
    idempotencyKey: 'target-alpha:plan-a:deploy',
    intent: {
      schemaVersion: 'distr.executor-intent/v2',
      tenantId: 'tenant-neutral',
      targetId: 'target-alpha',
      taskId: 'task-alpha-a',
      attemptId: 'attempt-alpha-a',
      operationId: 'operation-alpha-a',
      idempotencyKey: 'target-alpha:plan-a:deploy',
      stepId: 'deploy',
      planId: 'plan-alpha-a',
      adapterRevision: 'external-http@1.0.0',
      resourceKey: 'deployment-target:target-alpha',
      fenceGeneration: 2,
      issuedAt: '2020-01-01T00:00:00.000Z',
      expiresAt: '2099-01-01T00:00:00.000Z',
      payload: {
        releaseDigest: `sha256:${'b'.repeat(64)}`,
        configChecksum: `sha256:${'1'.repeat(64)}`,
        migration: {
          id: 'migration-001',
          idempotencyKey: 'migration-001:target-alpha',
          retrySafe: true,
        },
      },
    },
    ...overrides,
  };
  body.signature =
    overrides.signature ??
    `sha256:${createHmac('sha256', executorSecretForTest).update(stableStringify(body.intent)).digest('hex')}`;
  return body;
}

test('fixture freezes two neutral targets and the canonical failure matrix', async () => {
  const fixture = JSON.parse(await readFile(path.join(fixtureDir, 'fixture.json'), 'utf8'));

  assert.equal(fixture.schemaVersion, 'distr.control-plane-e2e-fixture/v1');
  assert.deepEqual(
    fixture.targets.map(({id, adapterId, observerId}) => ({id, adapterId, observerId})),
    [
      {id: 'target-alpha', adapterId: 'adapter-http-alpha', observerId: 'observer-alpha'},
      {id: 'target-beta', adapterId: 'adapter-reference-beta', observerId: 'observer-beta'},
    ]
  );
  assert.equal(new Set(fixture.targets.map((target) => target.observerId)).size, 2);
  assert.equal(new Set(fixture.targets.map((target) => target.configChecksum)).size, 2);
  for (const target of fixture.targets) {
    assert.match(target.bindingId, /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
    assert.match(target.configChecksum, /^sha256:[0-9a-f]{64}$/);
    assert.match(target.capabilityChecksum, /^sha256:[0-9a-f]{64}$/);
    assert.match(target.topologyChecksum, /^sha256:[0-9a-f]{64}$/);
  }

  assert.deepEqual(fixture.product.capabilities, {
    providers: [{component: 'catalog-provider', capability: 'catalog.v1'}],
    consumers: [
      {
        component: 'gateway-consumer',
        requires: 'catalog.v1',
        provider: 'catalog-provider',
      },
    ],
  });
  assert.equal(fixture.product.migration.retrySafe, true);
  assert.ok(fixture.product.migration.idempotencyKey);
  assert.equal(fixture.campaign.waves.length, 2);
  assert.equal(fixture.governance.approvals.required, 2);
  assert.ok(fixture.governance.maintenanceWindow.notBefore);
  assert.ok(fixture.governance.maintenanceWindow.notAfter);
  assert.deepEqual(fixture.previousState, {from: 'B', to: 'A', priorActiveRelease: 'B'});

  const expectedCases = [
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
  assert.equal(fixture.failureMatrix.schemaVersion, 'distr.control-plane-failure-matrix-fixture/v1');
  assert.deepEqual(
    fixture.failureMatrix.cases.map(({id, expectedOutcome}) => [id, expectedOutcome]),
    expectedCases
  );
  assert.deepEqual(
    fixture.failureMatrix.targets,
    fixture.targets.map((target) => ({
      id: target.id,
      adapterId: target.adapterId,
      observerId: target.observerId,
      configChecksum: target.configChecksum,
      capabilityChecksum: target.capabilityChecksum,
      topologyChecksum: target.topologyChecksum,
    }))
  );
  assert.deepEqual(fixture.failureMatrix.releases, {
    A: {
      productReleaseId: fixture.releases.A.productReleaseId,
      digest: fixture.releases.A.digest,
    },
    B: {
      productReleaseId: fixture.releases.B.productReleaseId,
      digest: fixture.releases.B.digest,
    },
  });
  assert.deepEqual(fixture.failureMatrix.features, fixture.features);
  assert.deepEqual(fixture.failureMatrix.execution, fixture.execution);
  const cases = Object.fromEntries(fixture.failureMatrix.cases.map((failureCase) => [failureCase.id, failureCase]));
  assert.deepEqual(
    {sequence: cases['duplicate-event'].sequence, checksum: cases['duplicate-event'].checksum},
    {
      sequence: 4,
      checksum: `sha256:${'8'.repeat(64)}`,
    }
  );
  assert.deepEqual(
    {
      presentedFenceGeneration: cases['stale-fence'].presentedFenceGeneration,
      currentFenceGeneration: cases['stale-fence'].currentFenceGeneration,
    },
    {presentedFenceGeneration: 1, currentFenceGeneration: 2}
  );
  assert.deepEqual(
    {elapsedMs: cases.timeout.elapsedMs, timeoutMs: cases.timeout.timeoutMs},
    {elapsedMs: 5001, timeoutMs: 5000}
  );
  assert.equal(cases.cancel.cancellable, true);
  assert.deepEqual(
    {
      presentedObserverId: cases['observer-mismatch'].presentedObserverId,
      expectedObserverId: cases['observer-mismatch'].expectedObserverId,
    },
    {presentedObserverId: 'observer-untrusted', expectedObserverId: 'observer-beta'}
  );
  assert.deepEqual(
    {
      observedRelease: cases['drift-reconcile'].observedRelease,
      desiredRelease: cases['drift-reconcile'].desiredRelease,
    },
    {observedRelease: 'A', desiredRelease: 'B'}
  );
  assert.deepEqual(
    {
      from: cases['previous-state-b-to-a'].from,
      to: cases['previous-state-b-to-a'].to,
      priorActiveRelease: cases['previous-state-b-to-a'].priorActiveRelease,
    },
    {from: 'B', to: 'A', priorActiveRelease: 'B'}
  );
  for (const failureCase of fixture.failureMatrix.cases) {
    assert.ok(failureCase.targetId);
  }
});

test('contract mode deterministically proves A, B, and previous-state B-to-A without live access', () => {
  const result = spawnSync(node, [path.join(fixtureDir, 'run.mjs'), '--mode', 'contract', '--json'], {
    cwd: repoRoot,
    encoding: 'utf8',
    env: {...process.env, DISTR_CP_ALLOW_LIVE: ''},
  });

  assert.equal(result.status, 0, result.stderr || result.stdout);
  const report = JSON.parse(result.stdout);
  assert.equal(report.ok, true);
  assert.equal(report.mode, 'contract');
  assert.deepEqual(report.targets, [
    {id: 'target-alpha', activeRelease: 'A', observerId: 'observer-alpha'},
    {id: 'target-beta', activeRelease: 'A', observerId: 'observer-beta'},
  ]);
  assert.deepEqual(report.releaseHistory, ['A', 'B', 'A']);
  assert.equal(report.migration.appliedCount, 1);
  assert.equal(report.secretLeaks, 0);
  assert.match(report.flowChecksum, /^sha256:[0-9a-f]{64}$/);
});

test('clean mode forced fallback reports the live blocker and completes scoped cleanup', () => {
  const result = spawnSync(node, [path.join(fixtureDir, 'run.mjs'), '--mode', 'clean', '--json'], {
    cwd: repoRoot,
    encoding: 'utf8',
    env: {
      ...process.env,
      DISTR_CP_FORCE_CONTRACT: 'true',
      DISTR_CP_ALLOW_LIVE: '',
    },
  });

  assert.equal(result.status, 0, result.stderr || result.stdout);
  const report = JSON.parse(result.stdout);
  assert.equal(report.ok, true);
  assert.equal(report.mode, 'clean');
  assert.equal(report.proofMode, 'fixture-contract');
  assert.equal(report.cleanup.completed, true);
  assert.match(report.liveStack.blocker, /forced contract mode/i);
  assert.equal(report.liveStack.nonLocalCalls, 0);
});

test('HTTP external executor is target-bound, fenced, idempotent, cancellable, and redacts logs', async () => {
  const port = await unusedLoopbackPort();
  const secret = executorSecretForTest;
  const {child, output} = startFixtureServer('external-executor.mjs', {
    PORT: String(port),
    EXECUTOR_ID: 'executor-http-alpha',
    TARGET_ID: 'target-alpha',
    EXECUTOR_SHARED_SECRET: secret,
    MAX_LOG_BYTES: '512',
  });
  const baseURL = `http://127.0.0.1:${port}`;
  const headers = {
    Authorization: `Bearer ${secret}`,
    'Content-Type': 'application/json',
  };

  try {
    await waitForReady(`${baseURL}/ready`, child);

    const first = await fetch(`${baseURL}/v1/operations`, {
      method: 'POST',
      headers,
      body: JSON.stringify(operation()),
    });
    assert.equal(first.status, 202);
    const firstBody = await first.json();
    assert.equal(firstBody.operationId, 'operation-alpha-a');
    assert.equal(firstBody.status, 'SUCCEEDED');

    const replay = await fetch(`${baseURL}/v1/operations`, {
      method: 'POST',
      headers,
      body: JSON.stringify(operation()),
    });
    assert.equal(replay.status, 200);
    assert.deepEqual(await replay.json(), firstBody);

    const stale = await fetch(`${baseURL}/v1/operations`, {
      method: 'POST',
      headers,
      body: JSON.stringify(
        operation({
          attemptId: 'attempt-stale',
          operationId: 'operation-stale',
          idempotencyKey: 'target-alpha:plan-stale:deploy',
          intent: {
            ...operation().intent,
            attemptId: 'attempt-stale',
            operationId: 'operation-stale',
            idempotencyKey: 'target-alpha:plan-stale:deploy',
            fenceGeneration: 1,
          },
        })
      ),
    });
    assert.equal(stale.status, 409);
    assert.equal((await stale.json()).code, 'STALE_FENCE');

    const invalidSignature = await fetch(`${baseURL}/v1/operations`, {
      method: 'POST',
      headers,
      body: JSON.stringify(
        operation({
          attemptId: 'attempt-invalid-signature',
          operationId: 'operation-invalid-signature',
          idempotencyKey: 'target-alpha:plan-invalid-signature:deploy',
          intent: {
            ...operation().intent,
            attemptId: 'attempt-invalid-signature',
            operationId: 'operation-invalid-signature',
            idempotencyKey: 'target-alpha:plan-invalid-signature:deploy',
            taskId: 'task-invalid-signature',
            planId: 'plan-invalid-signature',
            fenceGeneration: 3,
          },
          signature: `sha256:${'0'.repeat(64)}`,
        })
      ),
    });
    assert.equal(invalidSignature.status, 401);
    assert.equal((await invalidSignature.json()).code, 'INVALID_SIGNATURE');

    const longRunning = operation({
      attemptId: 'attempt-cancel',
      operationId: 'operation-cancel',
      idempotencyKey: 'target-alpha:plan-cancel:deploy',
      intent: {
        ...operation().intent,
        attemptId: 'attempt-cancel',
        operationId: 'operation-cancel',
        idempotencyKey: 'target-alpha:plan-cancel:deploy',
        taskId: 'task-cancel',
        planId: 'plan-cancel',
        fenceGeneration: 4,
        payload: {...operation().intent.payload, simulateLongRunning: true},
      },
    });
    const accepted = await fetch(`${baseURL}/v1/operations`, {
      method: 'POST',
      headers,
      body: JSON.stringify(longRunning),
    });
    assert.equal(accepted.status, 202);
    assert.equal((await accepted.json()).status, 'RUNNING');

    const canceled = await fetch(`${baseURL}/v1/operations/operation-cancel/cancel`, {
      method: 'POST',
      headers,
    });
    assert.equal(canceled.status, 200);
    assert.equal((await canceled.json()).status, 'CANCELED');

    const logs = await fetch(`${baseURL}/v1/operations/operation-alpha-a/logs`, {headers});
    assert.equal(logs.status, 200);
    const logBody = await logs.text();
    assert.ok(Buffer.byteLength(logBody) <= 512);
    assert.ok(logBody.includes('[REDACTED]'));
    assert.ok(!logBody.includes(secret));
    assert.ok(!output().includes(secret));
  } finally {
    await stopFixtureServer(child);
  }
});

test('external executor binds outer identities, rejects expired authority, and advances fences strictly', async () => {
  const fixedNow = new Date('2030-01-01T00:01:00.000Z');
  const server = createExternalExecutor({
    executorId: 'executor-http-alpha',
    targetId: 'target-alpha',
    sharedSecret: executorSecretForTest,
    now: () => fixedNow,
  });
  await new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', resolve);
  });
  const baseURL = `http://127.0.0.1:${server.address().port}`;
  const headers = {
    Authorization: `Bearer ${executorSecretForTest}`,
    'Content-Type': 'application/json',
  };
  const post = (body) =>
    fetch(`${baseURL}/v1/operations`, {
      method: 'POST',
      headers,
      body: JSON.stringify(body),
    });

  try {
    const first = await post(operation());
    assert.equal(first.status, 202);

    const rebound = operation({
      operationId: 'operation-rebound',
      idempotencyKey: 'target-alpha:plan-rebound:deploy',
      intent: operation().intent,
    });
    const reboundResponse = await post(rebound);
    assert.equal(reboundResponse.status, 400);
    assert.equal((await reboundResponse.json()).code, 'SIGNED_BINDING_MISMATCH');

    const expiredIntent = {
      ...operation().intent,
      operationId: 'operation-expired',
      idempotencyKey: 'target-alpha:plan-expired:deploy',
      attemptId: 'attempt-expired',
      taskId: 'task-expired',
      planId: 'plan-expired',
      fenceGeneration: 3,
      issuedAt: '2029-12-31T23:50:00.000Z',
      expiresAt: '2029-12-31T23:55:00.000Z',
    };
    const expired = await post(
      operation({
        attemptId: expiredIntent.attemptId,
        operationId: expiredIntent.operationId,
        idempotencyKey: expiredIntent.idempotencyKey,
        intent: expiredIntent,
      })
    );
    assert.equal(expired.status, 401);
    assert.equal((await expired.json()).code, 'EXPIRED_INTENT');

    const equalFenceIntent = {
      ...operation().intent,
      operationId: 'operation-equal-fence',
      idempotencyKey: 'target-alpha:plan-equal-fence:deploy',
      attemptId: 'attempt-equal-fence',
      taskId: 'task-equal-fence',
      planId: 'plan-equal-fence',
    };
    const equalFence = await post(
      operation({
        attemptId: equalFenceIntent.attemptId,
        operationId: equalFenceIntent.operationId,
        idempotencyKey: equalFenceIntent.idempotencyKey,
        intent: equalFenceIntent,
      })
    );
    assert.equal(equalFence.status, 409);
    assert.equal((await equalFence.json()).code, 'NON_INCREASING_FENCE');
  } finally {
    await new Promise((resolve) => server.close(resolve));
  }
});

test('independent observer enforces identity, target, sequence, and immutable evidence checksums', async () => {
  const port = await unusedLoopbackPort();
  const secret = 'observer-memory-secret';
  const {child, output} = startFixtureServer('observer.mjs', {
    PORT: String(port),
    OBSERVER_ID: 'observer-alpha',
    TARGET_ID: 'target-alpha',
    OBSERVER_SHARED_SECRET: secret,
  });
  const baseURL = `http://127.0.0.1:${port}`;
  const headers = {
    Authorization: `Bearer ${secret}`,
    'Content-Type': 'application/json',
  };
  const observation = {
    observerId: 'observer-alpha',
    targetId: 'target-alpha',
    sequence: 1,
    observedAt: '2030-01-01T00:01:00.000Z',
    releaseDigest: `sha256:${'b'.repeat(64)}`,
    configChecksum: `sha256:${'1'.repeat(64)}`,
    capabilityChecksum: `sha256:${'2'.repeat(64)}`,
    topologyChecksum: `sha256:${'3'.repeat(64)}`,
    schemaVersion: '1',
    health: 'HEALTHY',
  };

  try {
    await waitForReady(`${baseURL}/ready`, child);

    const accepted = await fetch(`${baseURL}/v1/observations`, {
      method: 'POST',
      headers,
      body: JSON.stringify(observation),
    });
    assert.equal(accepted.status, 202);
    const acceptedBody = await accepted.json();
    assert.match(acceptedBody.evidenceChecksum, /^sha256:[0-9a-f]{64}$/);

    const replay = await fetch(`${baseURL}/v1/observations`, {
      method: 'POST',
      headers,
      body: JSON.stringify(observation),
    });
    assert.equal(replay.status, 200);
    assert.deepEqual(await replay.json(), acceptedBody);

    const advanced = await fetch(`${baseURL}/v1/observations`, {
      method: 'POST',
      headers,
      body: JSON.stringify({...observation, sequence: 2, observedAt: '2030-01-01T00:02:00.000Z'}),
    });
    assert.equal(advanced.status, 202);
    const advancedBody = await advanced.json();

    const stale = await fetch(`${baseURL}/v1/observations`, {
      method: 'POST',
      headers,
      body: JSON.stringify(observation),
    });
    assert.equal(stale.status, 409);
    assert.equal((await stale.json()).code, 'STALE_OBSERVATION');

    const wrongTarget = await fetch(`${baseURL}/v1/observations`, {
      method: 'POST',
      headers,
      body: JSON.stringify({...observation, sequence: 2, targetId: 'target-beta'}),
    });
    assert.equal(wrongTarget.status, 403);

    const latest = await fetch(`${baseURL}/v1/observations/latest`, {headers});
    assert.equal(latest.status, 200);
    assert.deepEqual(await latest.json(), advancedBody);
    assert.ok(!output().includes(secret));
  } finally {
    await stopFixtureServer(child);
  }
});
