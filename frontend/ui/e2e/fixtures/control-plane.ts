import {test as base, expect, type Page, type Route, type TestInfo} from '@playwright/test';

export type OperatorActor = 'vendorAdmin' | 'scopedApprover' | 'executorOperator' | 'auditViewer' | 'unauthorized';
export type ControlPlaneScenario = 'ready' | 'loading' | 'empty' | 'error' | 'disabled';

export interface RecordedAction {
  method: string;
  path: string;
  body?: unknown;
}

interface ControlPlaneMock {
  actions: RecordedAction[];
  setScenario(scenario: ControlPlaneScenario): void;
}

interface ControlPlaneFixtures {
  actor: OperatorActor;
  controlPlane: ControlPlaneMock;
}

export const fixtureIds = {
  organization: '00000000-0000-4000-8000-000000000001',
  customerOrganization: '00000000-0000-4000-8000-000000000002',
  customerA: '00000000-0000-4000-8000-000000000003',
  customerB: '00000000-0000-4000-8000-000000000004',
  vendorAdmin: '00000000-0000-4000-8000-000000000011',
  scopedApprover: '00000000-0000-4000-8000-000000000012',
  executorOperator: '00000000-0000-4000-8000-000000000013',
  auditViewer: '00000000-0000-4000-8000-000000000014',
  unauthorized: '00000000-0000-4000-8000-000000000015',
  environment: '00000000-0000-4000-8000-000000000101',
  target: '00000000-0000-4000-8000-000000000102',
  unitA: '00000000-0000-4000-8000-000000000103',
  unitB: '00000000-0000-4000-8000-000000000104',
  component: '00000000-0000-4000-8000-000000000105',
  applicationPayments: '00000000-0000-4000-8000-000000000106',
  applicationSuite: '00000000-0000-4000-8000-000000000107',
  channelStable: '00000000-0000-4000-8000-000000000108',
  fleetA: '00000000-0000-4000-8000-000000000109',
  fleetB: '00000000-0000-4000-8000-000000000110',
  componentRelease: '00000000-0000-4000-8000-000000000201',
  pendingComponentRelease: '00000000-0000-4000-8000-000000000202',
  productRelease: '00000000-0000-4000-8000-000000000203',
  componentReleaseDraft: '00000000-0000-4000-8000-000000000204',
  productReleaseDraft: '00000000-0000-4000-8000-000000000205',
  plan: '00000000-0000-4000-8000-000000000301',
  planDraft: '00000000-0000-4000-8000-000000000302',
  publishedPlan: '00000000-0000-4000-8000-000000000303',
  previousPlan: '00000000-0000-4000-8000-000000000304',
  snapshot: '00000000-0000-4000-8000-000000000305',
  campaign: '00000000-0000-4000-8000-000000000401',
  campaignDraft: '00000000-0000-4000-8000-000000000402',
  campaignRevision: '00000000-0000-4000-8000-000000000403',
  campaignRun: '00000000-0000-4000-8000-000000000404',
  execution: '00000000-0000-4000-8000-000000000501',
  executionAttempt: '00000000-0000-4000-8000-000000000502',
  executionStatusQuery: '00000000-0000-4000-8000-000000000503',
  task: '00000000-0000-4000-8000-000000000504',
  stepRun: '00000000-0000-4000-8000-000000000505',
  reconciliation: '00000000-0000-4000-8000-000000000601',
  drift: '00000000-0000-4000-8000-000000000602',
  approval: '00000000-0000-4000-8000-000000000701',
  invalidatedApproval: '00000000-0000-4000-8000-000000000702',
  approvalRequirement: '00000000-0000-4000-8000-000000000703',
  approvalDecision: '00000000-0000-4000-8000-000000000704',
  policyVersion: '00000000-0000-4000-8000-000000000705',
  approvalAuthority: '00000000-0000-4000-8000-000000000706',
  audit: '00000000-0000-4000-8000-000000000801',
  evidence: '00000000-0000-4000-8000-000000000802',
  artifact: '00000000-0000-4000-8000-000000000805',
  auditSink: '00000000-0000-4000-8000-000000000806',
  priorAudit: '00000000-0000-4000-8000-000000000807',
  registryImport: '00000000-0000-4000-8000-000000000901',
  enrollmentOrganization: '00000000-0000-4000-8000-000000000902',
  enrollmentEnvironment: '00000000-0000-4000-8000-000000000903',
} as const;

const actorClaims: Record<OperatorActor, Record<string, unknown>> = {
  vendorAdmin: {
    sub: fixtureIds.vendorAdmin,
    email: 'vendor-admin@example.invalid',
    name: 'Vendor administrator',
    role: 'admin',
    authorities: ['registry.manage', 'release.manage', 'plan.manage', 'campaign.control', 'audit.export'],
  },
  scopedApprover: {
    sub: fixtureIds.scopedApprover,
    email: 'scoped-approver@example.invalid',
    name: 'Scoped approver',
    role: 'read_write',
    authorities: ['approval.decide'],
    authority_scope: {environmentIds: [fixtureIds.environment]},
  },
  executorOperator: {
    sub: fixtureIds.executorOperator,
    email: 'executor-operator@example.invalid',
    name: 'Executor operator',
    role: 'read_write',
    authorities: ['campaign.control', 'execution.control', 'reconciliation.resolve'],
    authority_scope: {environmentIds: [fixtureIds.environment]},
  },
  auditViewer: {
    sub: fixtureIds.auditViewer,
    email: 'audit-viewer@example.invalid',
    name: 'Audit viewer',
    role: 'read_only',
    authorities: ['audit.read'],
  },
  unauthorized: {
    sub: fixtureIds.unauthorized,
    email: 'customer-reader@example.invalid',
    name: 'Customer reader',
    role: 'read_only',
    c_org: fixtureIds.customerOrganization,
    authorities: [],
  },
};

const checksums = {
  canonical: 'sha256:1111111111111111111111111111111111111111111111111111111111111111',
  product: 'sha256:2222222222222222222222222222222222222222222222222222222222222222',
  config: 'sha256:3333333333333333333333333333333333333333333333333333333333333333',
  policy: 'sha256:4444444444444444444444444444444444444444444444444444444444444444',
  subscriber: 'sha256:5555555555555555555555555555555555555555555555555555555555555555',
  graph: 'sha256:6666666666666666666666666666666666666666666666666666666666666666',
  change: 'sha256:7777777777777777777777777777777777777777777777777777777777777777',
  baseline: 'sha256:8888888888888888888888888888888888888888888888888888888888888888',
  provider: 'sha256:9999999999999999999999999999999999999999999999999999999999999999',
  migration: 'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
  risk: 'sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
  approval: 'sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc',
  window: 'sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd',
  adapter: 'sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee',
  intent: 'sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff',
};

const timestamp = '2026-07-28T08:00:00Z';

const fleetRows = [
  {
    id: fixtureIds.fleetA,
    createdAt: timestamp,
    customerOrganizationId: fixtureIds.customerA,
    customer: 'Customer A',
    environmentId: fixtureIds.environment,
    environment: 'Production',
    deploymentTargetId: fixtureIds.target,
    target: 'Shared production host',
    deploymentUnitId: fixtureIds.unitA,
    unit: 'Customer A unit',
    componentId: fixtureIds.component,
    component: 'payments-api',
    activeReleaseId: fixtureIds.componentRelease,
    activeRelease: 'payments-api 2.4.0',
    pendingReleaseId: fixtureIds.pendingComponentRelease,
    pendingRelease: 'payments-api 2.5.0',
    observedState: 'healthy',
    drift: 'none',
    lastExecutionId: fixtureIds.execution,
    lastExecution: 'succeeded',
    enrollment: 'enabled',
  },
  {
    id: fixtureIds.fleetB,
    createdAt: timestamp,
    customerOrganizationId: fixtureIds.customerB,
    customer: 'Customer B',
    environmentId: fixtureIds.environment,
    environment: 'Production',
    deploymentTargetId: fixtureIds.target,
    target: 'Shared production host',
    deploymentUnitId: fixtureIds.unitB,
    unit: 'Customer B unit',
    componentId: fixtureIds.component,
    component: 'payments-api',
    activeReleaseId: fixtureIds.componentRelease,
    activeRelease: 'payments-api 2.4.0',
    pendingRelease: '',
    observedState: 'stale',
    drift: 'unknown',
    lastExecution: 'partial',
    enrollment: 'enabled',
  },
];

const releases = [
  {
    id: fixtureIds.componentRelease,
    createdAt: timestamp,
    kind: 'component',
    applicationId: fixtureIds.applicationPayments,
    releaseNumber: 24,
    version: '2.4.0',
    status: 'published',
    checksum: checksums.product,
    sourceRevision: '0123456789abcdef',
    publishedAt: timestamp,
    artifactCount: 1,
    evidenceCount: 2,
    componentCount: 1,
    graphEdgeCount: 0,
  },
  {
    id: fixtureIds.productRelease,
    createdAt: timestamp,
    kind: 'product',
    applicationId: fixtureIds.applicationSuite,
    releaseNumber: 7,
    version: '2026.07',
    status: 'published',
    checksum: checksums.canonical,
    sourceRevision: 'fedcba9876543210',
    publishedAt: timestamp,
    artifactCount: 2,
    evidenceCount: 2,
    componentCount: 2,
    graphEdgeCount: 1,
  },
];

const planRow = {
  id: fixtureIds.plan,
  createdAt: timestamp,
  status: 'BLOCKED',
  planSchema: 'distr.plan.v2',
  protocolVersion: 'v2',
  productReleaseId: fixtureIds.productRelease,
  productReleaseVersion: '2026.07',
  environmentId: fixtureIds.environment,
  environment: 'Production',
  deploymentUnitId: fixtureIds.unitA,
  deploymentUnit: 'Customer A unit',
  targetConfigSnapshotId: fixtureIds.snapshot,
  canonicalChecksum: checksums.canonical,
  targetCount: 1,
  stepCount: 3,
  issueCount: 2,
  blockingIssueCount: 1,
  approvalBlockerCount: 1,
  preflightBlockerCount: 1,
  bootstrap: false,
};

const fact = (key: string, message: string, blocking = false, checksum?: string) => ({
  id: `fact-${key.toLowerCase().replaceAll(' ', '-')}`,
  key,
  kind: 'preflight',
  status: blocking ? 'blocked' : 'satisfied',
  checksum,
  message,
  blocking,
  order: 1,
});

const evidence = [
  {
    id: fixtureIds.evidence,
    kind: 'provenance',
    label: 'Build provenance',
    href: `/deployments/plans/${fixtureIds.plan}`,
    checksum: checksums.product,
    createdAt: timestamp,
  },
];

const campaignRow = {
  id: fixtureIds.campaign,
  createdAt: timestamp,
  draftId: fixtureIds.campaignDraft,
  revisionId: fixtureIds.campaignRevision,
  runId: fixtureIds.campaignRun,
  runVersion: 3,
  name: 'Production canary',
  status: 'running',
  canonicalChecksum: checksums.canonical,
  waveCount: 2,
  memberCount: 2,
  pendingCount: 1,
  runningCount: 1,
  succeededCount: 0,
  failedCount: 0,
  blockedCount: 0,
};

const executionRow = {
  id: fixtureIds.execution,
  createdAt: timestamp,
  campaignId: fixtureIds.campaign,
  deploymentPlanId: fixtureIds.plan,
  deploymentTargetId: fixtureIds.target,
  taskId: fixtureIds.task,
  stepRunId: fixtureIds.stepRun,
  stepKey: 'deploy-payments',
  attemptNumber: 1,
  protocolVersion: 'v2',
  status: 'running',
  planChecksum: checksums.canonical,
  artifactDigest: 'sha256:abababababababababababababababababababababababababababababababab',
  configChecksum: checksums.config,
  adapterRevision: 'compose-adapter@2',
  cancellable: true,
  reconciliation: 'not_required',
  observation: 'pending',
};

const reconciliationRow = {
  id: fixtureIds.reconciliation,
  createdAt: timestamp,
  driftCaseId: fixtureIds.drift,
  executionId: fixtureIds.execution,
  deploymentPlanId: fixtureIds.plan,
  environmentId: fixtureIds.environment,
  deploymentTargetId: fixtureIds.target,
  component: 'payments-api',
  drift: 'artifact_mismatch',
  status: 'OPEN',
  outcome: 'pending',
  observedAt: timestamp,
  evidenceChecksum: checksums.intent,
};

const auditRow = {
  id: fixtureIds.audit,
  createdAt: timestamp,
  sequence: 42,
  action: 'deployment_plan.approved',
  subjectType: 'deployment_plan',
  subjectId: fixtureIds.plan,
  actorUserAccountId: fixtureIds.scopedApprover,
  outcome: 'accepted',
  correlationCount: 2,
  payloadChecksum: checksums.approval,
};

const approvalRequest = {
  id: fixtureIds.approval,
  createdAt: timestamp,
  updatedAt: timestamp,
  subjectType: 'deployment_plan',
  subjectId: fixtureIds.plan,
  subjectRevision: 2,
  subjectChecksum: checksums.canonical,
  effectivePolicyChecksum: checksums.policy,
  subscriberSetChecksum: checksums.subscriber,
  requesterUserAccountId: fixtureIds.vendorAdmin,
  state: 'PENDING',
  revision: 3,
  expiresAt: '2026-07-29T08:00:00Z',
  requirements: [
    {
      id: fixtureIds.approvalRequirement,
      ruleKey: 'production-four-eyes',
      policyVersionId: fixtureIds.policyVersion,
      authorityKind: 'environment',
      authorityId: fixtureIds.environment,
      principalGroupId: fixtureIds.approvalAuthority,
      quorum: 1,
      separationConstraints: ['requester_cannot_approve'],
      sortOrder: 1,
    },
  ],
  decisions: [],
};

const invalidatedApproval = {
  ...approvalRequest,
  id: fixtureIds.invalidatedApproval,
  subjectRevision: 1,
  state: 'INVALIDATED',
  revision: 4,
  invalidatedAt: timestamp,
  invalidationReason: 'PLAN_CHECKSUM_CHANGED',
};

export const test = base.extend<ControlPlaneFixtures>({
  actor: ['vendorAdmin', {option: true}],
  controlPlane: [
    async ({page, actor}, use) => {
      let scenario: ControlPlaneScenario = 'ready';
      const actions: RecordedAction[] = [];

      await seedBrowserIdentity(page, actor);
      await page.route('**/*', async (route) => {
        const url = new URL(route.request().url());
        if (!isLocalHost(url.hostname)) {
          await route.abort('blockedbyclient');
          return;
        }
        if (url.pathname === '/internal/environment') {
          await json(route, {registryHost: 'registry.example.invalid', posthogApiKey: ''});
          return;
        }
        if (!url.pathname.startsWith('/api/')) {
          await route.continue();
          return;
        }
        if (scenario === 'loading' && isControlPlaneRead(route)) {
          await new Promise((resolve) => setTimeout(resolve, 750));
        }
        if (scenario === 'error' && isControlPlaneRead(route)) {
          await json(
            route,
            {
              code: 'CONTROL_PLANE_UNAVAILABLE',
              message: 'Operator read model is temporarily unavailable',
              requestId: 'request-fixture-503',
            },
            503
          );
          return;
        }
        await handleApi(route, actor, scenario, actions);
      });

      await use({
        actions,
        setScenario(next) {
          scenario = next;
        },
      });
    },
    {auto: true},
  ],
});

export {expect};

export async function attachContractEvidence(testInfo: TestInfo, name: string, value: unknown): Promise<void> {
  await testInfo.attach(name, {
    body: Buffer.from(JSON.stringify(value, null, 2)),
    contentType: 'application/json',
  });
}

async function seedBrowserIdentity(page: Page, actor: OperatorActor): Promise<void> {
  const token = createFixtureToken(actor);
  await page.addInitScript(
    ({jwt}) => {
      localStorage.setItem('cloud_token', jwt);
      sessionStorage.setItem(
        'remoteEnvironment',
        JSON.stringify({registryHost: 'registry.example.invalid', posthogApiKey: ''})
      );
    },
    {jwt: token}
  );
}

function createFixtureToken(actor: OperatorActor): string {
  const claims = {
    ...actorClaims[actor],
    org: 'vendor-organization',
    password_reset: false,
    email_verified: true,
    exp: '4102444800',
    image_url: undefined,
  };
  return [encodeTokenPart({alg: 'none', typ: 'JWT'}), encodeTokenPart(claims), 'fixture-only'].join('.');
}

function encodeTokenPart(value: unknown): string {
  return Buffer.from(JSON.stringify(value)).toString('base64url');
}

async function handleApi(
  route: Route,
  actor: OperatorActor,
  scenario: ControlPlaneScenario,
  actions: RecordedAction[]
): Promise<void> {
  const request = route.request();
  const url = new URL(request.url());
  const path = url.pathname;
  const method = request.method();
  const empty = scenario === 'empty';

  if (method !== 'GET') {
    actions.push({method, path, body: request.postDataJSON()});
    if (!actorCanMutate(actor, path)) {
      await json(
        route,
        {
          code: 'FORBIDDEN',
          message: 'The fixture actor is not authorized for this scoped mutation.',
        },
        403
      );
      return;
    }
  }

  if (method === 'GET' && path === '/api/v1/context') {
    await json(route, contextResponse(actor));
  } else if (method === 'GET' && path === '/api/v1/experimental-feature-flags') {
    await json(
      route,
      scenario === 'disabled'
        ? []
        : [
            {key: 'operator_control_plane_v2', enabled: true},
            {key: 'executor_protocol_v2', enabled: true},
            {key: 'release_bundles', enabled: true},
            {key: 'deployment_processes', enabled: true},
            {key: 'scoped_variables_v2', enabled: true},
          ]
    );
  } else if (method === 'GET' && path === '/api/v1/tutorial-progress') {
    await json(route, []);
  } else if (method === 'GET' && path === '/api/v1/control-plane/fleet') {
    await json(route, pageOf(empty ? [] : fleetRows));
  } else if (method === 'GET' && path === '/api/v1/control-plane/releases') {
    await json(route, pageOf(empty ? [] : releases));
  } else if (method === 'GET' && path === `/api/v1/control-plane/releases/${fixtureIds.productRelease}`) {
    await json(route, {
      detail: {
        release: releases[1],
        artifacts: [
          {
            id: fixtureIds.artifact,
            name: 'payments-api',
            version: '2.5.0',
            manifestDigest: executionRow.artifactDigest,
            platformDigests: {'linux/amd64': executionRow.artifactDigest},
          },
        ],
        componentPins: [
          {
            componentReleaseId: fixtureIds.componentRelease,
            component: 'payments-api',
            version: '2.5.0',
            checksum: checksums.product,
            digest: executionRow.artifactDigest,
          },
        ],
        graphEdges: [{from: 'gateway', to: 'payments-api', kind: 'requires'}],
        evidence,
      },
    });
  } else if (
    method === 'GET' &&
    path === `/api/v1/control-plane/releases/${fixtureIds.productRelease}/compare/${fixtureIds.componentRelease}`
  ) {
    await json(route, {
      comparison: {
        left: releases[1],
        right: releases[0],
        changes: [
          {
            component: 'payments-api',
            change: 'digest_changed',
            leftChecksum: checksums.product,
            rightChecksum: checksums.baseline,
          },
        ],
      },
    });
  } else if (method === 'GET' && path.endsWith('/evidence')) {
    await json(route, {items: empty ? [] : evidence});
  } else if (method === 'GET' && path === '/api/v1/control-plane/plans') {
    await json(route, pageOf(empty ? [] : [planRow]));
  } else if (
    method === 'GET' &&
    [
      `/api/v1/control-plane/plans/${fixtureIds.plan}`,
      `/api/v1/control-plane/plans/${fixtureIds.publishedPlan}`,
      `/api/v1/control-plane/plans/${fixtureIds.previousPlan}`,
    ].includes(path)
  ) {
    const planId = path.split('/').at(-1) ?? fixtureIds.plan;
    await json(route, {
      detail: {
        plan: {...planRow, id: planId},
        productReleaseChecksum: checksums.product,
        targetConfigChecksum: checksums.config,
        effectivePolicyChecksum: checksums.policy,
        subscriberSetChecksum: checksums.subscriber,
        graphChecksum: checksums.graph,
        changeChecksum: checksums.change,
        baselineChecksum: checksums.baseline,
        providerResolutionChecksum: checksums.provider,
        migrationChecksum: checksums.migration,
        riskChecksum: checksums.risk,
        approvalChecksum: checksums.approval,
        windowChecksum: checksums.window,
        adapterChecksum: checksums.adapter,
        intentChecksum: checksums.intent,
        targets: [fact('target', 'Shared production host is selected')],
        baselines: [fact('baseline', 'Last healthy release is 2.4.0', false, checksums.baseline)],
        config: [fact('configuration', 'Configuration snapshot is immutable', false, checksums.config)],
        requirements: [fact('provider', 'Compose adapter v2 is available', false, checksums.provider)],
        migrations: [fact('migration', 'Forward migration is reversible', false, checksums.migration)],
        changes: [fact('change', 'payments-api changes from 2.4.0 to 2.5.0', false, checksums.change)],
        risks: [fact('risk', 'Shared-host blast radius requires approval', true, checksums.risk)],
        approvals: [fact('approval', 'Production approval is pending', true, checksums.approval)],
        windows: [fact('window', 'Inside maintenance window', false, checksums.window)],
        adapters: [fact('adapter', 'Compose adapter revision is pinned', false, checksums.adapter)],
        steps: [fact('deploy', 'Deploy payments-api')],
        edges: [fact('dependency', 'gateway requires payments-api')],
        issues: [fact('blocking issue', 'Approval must be satisfied before execution', true)],
        intentBlockers: [fact('intent blocker', 'Approval checksum is not satisfied', true, checksums.intent)],
        evidence,
      },
    });
  } else if (method === 'GET' && path === '/api/v1/control-plane/campaigns') {
    await json(route, pageOf(empty ? [] : [campaignRow]));
  } else if (method === 'GET' && path === `/api/v1/control-plane/campaigns/${fixtureIds.campaign}`) {
    await json(route, {
      detail: {
        campaign: campaignRow,
        runVersion: 3,
        revisionChecksum: checksums.canonical,
        membershipChecksum: checksums.subscriber,
        prerequisiteChecksum: checksums.graph,
        thresholdChecksum: checksums.risk,
        controlChecksum: checksums.intent,
        admissionChecksum: checksums.approval,
        waves: [
          {
            id: '00000000-0000-4000-8000-000000000405',
            order: 1,
            name: 'Canary',
            status: 'running',
            bakeSeconds: 300,
            maximumConcurrency: 1,
            memberCount: 1,
            succeededCount: 0,
            failedCount: 0,
          },
        ],
        members: [
          {
            id: '00000000-0000-4000-8000-000000000406',
            deploymentPlanId: fixtureIds.plan,
            deploymentUnitId: fixtureIds.unitA,
            waveOrder: 1,
            memberOrder: 1,
            status: 'running',
            planChecksum: checksums.canonical,
          },
        ],
        prerequisites: [fact('approval', 'Plan approval satisfied')],
        thresholds: [fact('error rate', 'Maximum error rate is 1%')],
        controls: [fact('pause', 'Pause preserves deterministic cursor')],
        uncertaintyBlockers: [],
        admissionBlockers: [],
        evidence,
      },
    });
  } else if (method === 'GET' && path === '/api/v1/control-plane/executions') {
    await json(route, pageOf(empty ? [] : [executionRow]));
  } else if (method === 'GET' && path === `/api/v1/control-plane/executions/${fixtureIds.execution}`) {
    await json(route, {
      detail: {
        execution: executionRow,
        intent: fact('intent', 'Plan checksum matches task input', false, checksums.intent),
        adapter: fact('adapter', 'Compose adapter revision is pinned', false, checksums.adapter),
        cancellation: fact('cancellation', 'Execution is cancellable'),
        reconciliation: fact('reconciliation', 'No reconciliation requested'),
        previousState: fact('previous state', 'payments-api 2.4.0 was last healthy', false, checksums.baseline),
        tasks: [fact('task', 'Task task-1 is leased')],
        steps: [fact('step', 'Deploy payments-api')],
        attempts: [fact('attempt', 'Attempt 1 is running')],
        observations: [fact('observation', 'Observer is pending')],
        evidence,
      },
    });
  } else if (method === 'GET' && path === '/api/v1/approval-requests') {
    await json(route, {items: empty ? [] : [currentApproval(actions), invalidatedApproval]});
  } else if (method === 'GET' && path === `/api/v1/approval-requests/${fixtureIds.approval}`) {
    await json(route, currentApproval(actions));
  } else if (method === 'GET' && path === '/api/v1/control-plane/reconciliation') {
    await json(route, pageOf(empty ? [] : [reconciliationRow]));
  } else if (method === 'GET' && path === `/api/v1/control-plane/reconciliation/${fixtureIds.reconciliation}`) {
    await json(route, {
      detail: {
        reconciliation: reconciliationRow,
        desiredState: fact('desired state', 'payments-api digest is pinned', false, checksums.product),
        observation: fact('observation', 'Observed digest differs', true, checksums.intent),
        decision: fact('decision', 'Operator decision is pending', true),
        evidence,
      },
    });
  } else if (method === 'GET' && path === '/api/v1/control-plane/audit') {
    await json(route, pageOf(empty ? [] : [auditRow]));
  } else if (method === 'GET' && path === `/api/v1/control-plane/audit/${fixtureIds.audit}`) {
    await json(route, {
      detail: {
        event: auditRow,
        correlations: [
          {id: '00000000-0000-4000-8000-000000000803', kind: 'deployment_plan', value: fixtureIds.plan},
          {id: '00000000-0000-4000-8000-000000000804', kind: 'campaign', value: fixtureIds.campaign},
        ],
        payload: {planChecksum: checksums.canonical},
        evidence,
      },
    });
  } else if (method === 'GET' && path === '/api/v1/control-plane-audit/export-sinks') {
    await json(
      route,
      empty
        ? []
        : [
            {
              id: fixtureIds.auditSink,
              name: 'Retention archive',
              kind: 'object_store',
              endpointReference: 'secret://fixture/audit-archive',
              configChecksum: checksums.config,
              enabled: true,
              consecutiveFailures: 0,
              createdAt: timestamp,
              updatedAt: timestamp,
            },
          ]
    );
  } else if (method === 'GET' && path === '/api/v1/control-plane-audit/export-status') {
    await json(route, [
      {
        sink: {
          id: fixtureIds.auditSink,
          name: 'Retention archive',
          kind: 'object_store',
          endpointReference: 'secret://fixture/audit-archive',
          configChecksum: checksums.config,
          enabled: true,
          consecutiveFailures: 0,
          createdAt: timestamp,
          updatedAt: timestamp,
        },
        lastExportedSequence: 41,
        lastExportedEventId: fixtureIds.priorAudit,
        latestSequence: 42,
        checkpointLag: 1,
        alert: false,
        lastAttemptStatus: 'succeeded',
        lastAttemptCompletedAt: timestamp,
      },
    ]);
  } else if (method === 'POST' && path === '/api/v1/control-plane-audit/evidence-bundles') {
    await json(
      route,
      {
        version: 'v1',
        deploymentPlanId: fixtureIds.plan,
        events: [
          {
            id: fixtureIds.audit,
            sequence: 42,
            eventType: 'deployment_plan.approved',
            outcome: 'accepted',
            deploymentPlanId: fixtureIds.plan,
            payloadRedacted: false,
            payloadTruncated: false,
            createdAt: timestamp,
          },
        ],
        checksum: checksums.canonical,
      },
      201
    );
  } else if (method === 'POST' && path === '/api/v1/deployment-registry/imports/preview') {
    await json(route, registryImport('needs_decision'));
  } else if (
    method === 'POST' &&
    path === `/api/v1/deployment-registry/imports/${fixtureIds.registryImport}/decisions`
  ) {
    await route.fulfill({status: 204});
  } else if (method === 'GET' && path === `/api/v1/deployment-registry/imports/${fixtureIds.registryImport}`) {
    await json(route, registryImport('standard'));
  } else if (method === 'POST' && path === `/api/v1/deployment-registry/imports/${fixtureIds.registryImport}/apply`) {
    await json(route, {
      id: fixtureIds.registryImport,
      previewChecksum: checksums.canonical,
      state: 'applied',
      applied: true,
      counts: registryCounts(),
      checkpoint: 1,
    });
  } else if (
    method === 'GET' &&
    path === '/api/v1/deployment-registry/coverage' &&
    url.searchParams.get('importId') === fixtureIds.registryImport
  ) {
    await json(route, registryCoverage());
  } else if (method === 'GET' && path === '/api/v1/authorization/control-plane-enrollments') {
    await json(route, {
      enrollments: [
        {
          id: fixtureIds.enrollmentOrganization,
          createdAt: timestamp,
          scope: {kind: 'organization', id: fixtureIds.organization},
          enabled: true,
          effectiveFrom: '2026-07-01T00:00:00Z',
          actorUserAccountId: fixtureIds.vendorAdmin,
          reason: 'Fixture enrollment',
          revision: 1,
        },
        {
          id: fixtureIds.enrollmentEnvironment,
          createdAt: timestamp,
          scope: {kind: 'environment', id: fixtureIds.environment},
          enabled: true,
          effectiveFrom: '2026-07-01T00:00:00Z',
          actorUserAccountId: fixtureIds.vendorAdmin,
          reason: 'Fixture environment enrollment',
          revision: 1,
        },
      ],
    });
  } else if (method === 'GET' && path === '/api/v1/target-config-snapshots/') {
    await json(route, {
      items: url.searchParams.has('deploymentUnitId')
        ? [
            {
              id: fixtureIds.snapshot,
              deploymentUnitId: fixtureIds.unitA,
              environmentId: fixtureIds.environment,
            },
          ]
        : [],
    });
  } else if (method === 'POST' && path === '/api/v1/release-bundles') {
    await json(route, componentRelease('DRAFT'), 201);
  } else if (method === 'POST' && path === `/api/v1/release-bundles/${fixtureIds.componentReleaseDraft}/validate`) {
    await json(route, {valid: true, errors: [], warnings: []});
  } else if (method === 'POST' && path === `/api/v1/release-bundles/${fixtureIds.componentReleaseDraft}/publish`) {
    await json(route, componentRelease('PUBLISHED'));
  } else if (method === 'POST' && path === '/api/v1/product-releases') {
    await json(route, productRelease('DRAFT'), 201);
  } else if (method === 'POST' && path === `/api/v1/product-releases/${fixtureIds.productReleaseDraft}/validate`) {
    await json(route, {valid: true, issues: []});
  } else if (method === 'POST' && path === `/api/v1/product-releases/${fixtureIds.productReleaseDraft}/publish`) {
    await json(route, productRelease('PUBLISHED'));
  } else if (method === 'POST' && path === `/api/v1/deployment-plan-drafts/${fixtureIds.planDraft}/publish`) {
    await json(route, {...planRow, id: fixtureIds.publishedPlan, status: 'PUBLISHED'});
  } else if (method === 'POST' && path === `/api/v1/deployment-plans/${fixtureIds.plan}/previous-state`) {
    await json(route, {...planRow, id: fixtureIds.previousPlan, status: 'READY'});
  } else if (method === 'POST' && path === `/api/v1/approval-requests/${fixtureIds.approval}/decisions`) {
    await json(route, approvalDecision(actions));
  } else if (
    method === 'POST' &&
    new RegExp(`^/api/v1/deployment-campaigns/${fixtureIds.campaignRun}/(pause|resume|cancel)$`).test(path)
  ) {
    const action = path.split('/').at(-1);
    const body = request.postDataJSON() as {requestId: string};
    await json(route, {
      requestId: body.requestId,
      status: 'accepted',
      run: {
        id: fixtureIds.campaignRun,
        createdAt: timestamp,
        updatedAt: timestamp,
        campaignRevisionId: fixtureIds.campaignRevision,
        state: action === 'pause' ? 'PAUSED' : action === 'resume' ? 'RUNNING' : 'CANCELED',
        version: 4,
        currentWaveOrder: 1,
        currentMemberOrder: 1,
        admissionsBlocked: action !== 'resume',
        pauseRequested: false,
        reconciliationRequired: false,
        fencingToken: 8,
      },
      pausePending: false,
      reconciliationRequired: false,
      duplicate: false,
    });
  } else if (method === 'POST' && path === `/api/v1/executions/${fixtureIds.execution}/cancel`) {
    await route.fulfill({status: 204});
  } else if (method === 'POST' && path === `/api/v1/executions/${fixtureIds.execution}/status-queries`) {
    const body = request.postDataJSON() as {idempotencyKey: string; reason: string; expiresInSeconds: number};
    await json(route, {
      query: {
        id: fixtureIds.executionStatusQuery,
        createdAt: timestamp,
        organizationId: fixtureIds.organization,
        executionId: fixtureIds.execution,
        executionAttemptId: fixtureIds.executionAttempt,
        requestedBy: fixtureIds.executorOperator,
        idempotencyKey: body.idempotencyKey,
        reason: body.reason,
        status: 'PENDING',
        expiresAt: '2026-07-28T08:01:00Z',
        requestedTtlSeconds: body.expiresInSeconds,
      },
    });
  } else if (method === 'POST' && path === `/api/v1/drift-cases/${fixtureIds.drift}/resolve`) {
    await route.fulfill({status: 204});
  } else {
    await json(route, {code: 'FIXTURE_ROUTE_MISSING', message: `No deterministic fixture for ${method} ${path}`}, 404);
  }
}

function actorCanMutate(actor: OperatorActor, path: string): boolean {
  if (path === '/api/v1/control-plane-audit/evidence-bundles') {
    return actor === 'vendorAdmin' || actor === 'auditViewer';
  }
  if (path.startsWith('/api/v1/control-plane-audit/export-sinks')) {
    return actor === 'vendorAdmin';
  }
  if (path.startsWith('/api/v1/approval-requests/')) {
    return actor === 'scopedApprover';
  }
  if (
    path.startsWith('/api/v1/deployment-campaigns/') ||
    path.startsWith('/api/v1/executions/') ||
    path.startsWith('/api/v1/drift-cases/')
  ) {
    return actor === 'executorOperator';
  }
  if (
    path.startsWith('/api/v1/deployment-registry/') ||
    path.startsWith('/api/v1/release-bundles') ||
    path.startsWith('/api/v1/product-releases') ||
    path.startsWith('/api/v1/deployment-plan')
  ) {
    return actor === 'vendorAdmin';
  }
  return false;
}

function contextResponse(actor: OperatorActor) {
  const claims = actorClaims[actor];
  return {
    user: {
      id: claims.sub,
      createdAt: timestamp,
      updatedAt: timestamp,
      email: claims.email,
      name: claims.name,
      userRole: claims.role,
      joinedOrgAt: timestamp,
      emailVerified: true,
      mfaEnabled: false,
    },
    organization: {
      id: fixtureIds.organization,
      createdAt: timestamp,
      updatedAt: timestamp,
      name: 'Fixture Vendor',
      features: [],
      subscriptionType: 'community',
      subscriptionLimits: {
        maxCustomerOrganizations: 100,
        maxUsersPerCustomerOrganization: 100,
        maxDeploymentsPerCustomerOrganization: 100,
      },
      subscriptionCustomerOrganizationQuantity: 0,
      subscriptionUserAccountQuantity: 0,
      currentBillableUserAccountCount: 5,
      currentCustomerOrganizationCount: 2,
      connectScriptIsSudo: false,
      stripeWebhookSecretConfigured: false,
    },
    ...(actor === 'unauthorized'
      ? {customerOrganization: {id: fixtureIds.customerOrganization, name: 'Fixture Customer', features: []}}
      : {}),
    availableContexts: [],
    sidebarLinks: [],
  };
}

function registryCounts() {
  return {
    discoveredRoots: 1,
    classifiedRoots: 1,
    discoveredPlacements: 1,
    omittedPlacements: 0,
    creates: 1,
    updates: 0,
    retirements: 0,
    conflicts: 0,
  };
}

function registryImport(classification: 'needs_decision' | 'standard') {
  return {
    id: fixtureIds.registryImport,
    previewChecksum: checksums.canonical,
    counts: {
      ...registryCounts(),
      classifiedRoots: classification === 'needs_decision' ? 0 : 1,
    },
    diff: {
      creates: [
        {
          kind: 'deployment_root',
          rootKey: 'production',
          message: 'Add one managed deployment root',
        },
      ],
      updates: [],
      retirements: [],
      conflicts: [],
    },
    omissions: [],
    diagnostics: [],
    diagnosticsTruncated: false,
    roots: [
      {
        key: 'production',
        name: 'Production root',
        deliveryModel: 'dedicated',
        classification,
        customerOrganizationId: fixtureIds.customerA,
        deploymentTargetId: fixtureIds.target,
        environmentId: fixtureIds.environment,
        subscriberCustomerOrganizationIds: [fixtureIds.customerA],
        physicalIdentity: 'fixture-production-root',
        placements: [
          {
            componentKey: 'payments-api',
            physicalName: 'payments-api',
            configNamespace: 'production',
            databaseBoundary: 'payments',
            healthAdapter: 'http',
          },
        ],
      },
    ],
  };
}

function registryCoverage() {
  return {
    importId: fixtureIds.registryImport,
    discoveredRoots: 1,
    classifiedRoots: 1,
    actionableManagedRoots: 1,
    observeOnlyRoots: 0,
    externalRoots: 0,
    ignoredRoots: 0,
    unresolvedRoots: 0,
    discoveredPlacements: 1,
    services: 1,
    omittedPlacements: 0,
    omissions: [],
    complete: true,
  };
}

function currentApproval(actions: RecordedAction[]) {
  const approved = actions.some(
    (action) => action.path === `/api/v1/approval-requests/${fixtureIds.approval}/decisions`
  );
  return approved
    ? {
        ...approvalRequest,
        state: 'APPROVED',
        resolvedAt: timestamp,
        decisions: [
          {
            id: fixtureIds.approvalDecision,
            createdAt: timestamp,
            approvalRequestId: fixtureIds.approval,
            approvalRequirementId: fixtureIds.approvalRequirement,
            decision: 'APPROVE',
            comment: 'Reviewed production checksum and blockers',
            actorUserAccountId: fixtureIds.scopedApprover,
            requestRevision: 3,
            idempotencyKey: 'fixture-approval-decision',
          },
        ],
      }
    : approvalRequest;
}

function approvalDecision(actions: RecordedAction[]) {
  const action = actions.findLast(
    (candidate) => candidate.path === `/api/v1/approval-requests/${fixtureIds.approval}/decisions`
  );
  const body = action?.body as
    | {
        approvalRequirementId: string;
        decision: 'APPROVE' | 'REJECT';
        comment: string;
        expectedRequestRevision: number;
        idempotencyKey: string;
      }
    | undefined;
  return {
    id: fixtureIds.approvalDecision,
    createdAt: timestamp,
    approvalRequestId: fixtureIds.approval,
    approvalRequirementId: body?.approvalRequirementId ?? fixtureIds.approvalRequirement,
    decision: body?.decision ?? 'APPROVE',
    comment: body?.comment ?? '',
    actorUserAccountId: fixtureIds.scopedApprover,
    requestRevision: body?.expectedRequestRevision ?? approvalRequest.revision,
    idempotencyKey: body?.idempotencyKey ?? '',
  };
}

function componentRelease(status: 'DRAFT' | 'PUBLISHED') {
  return {
    id: fixtureIds.componentReleaseDraft,
    createdAt: timestamp,
    updatedAt: timestamp,
    applicationId: fixtureIds.applicationPayments,
    channelId: fixtureIds.channelStable,
    releaseNumber: '25',
    releaseNotes: 'Payments component fixture',
    sourceRevision: '0123456789abcdef',
    kind: 'component',
    releaseContractSchema: 'distr.component-release/v2',
    status,
    ...(status === 'PUBLISHED' ? {publishedByUserAccountId: fixtureIds.vendorAdmin, publishedAt: timestamp} : {}),
    canonicalChecksum: checksums.product,
    components: [],
  };
}

function productRelease(status: 'DRAFT' | 'PUBLISHED') {
  return {
    id: fixtureIds.productReleaseDraft,
    createdAt: timestamp,
    updatedAt: timestamp,
    applicationId: fixtureIds.applicationSuite,
    channelId: fixtureIds.channelStable,
    status,
    canonicalChecksum: checksums.canonical,
    graphChecksum: checksums.graph,
    ...(status === 'PUBLISHED' ? {publishedByUserAccountId: fixtureIds.vendorAdmin, publishedAt: timestamp} : {}),
    manifest: {
      schema: 'distr.product-release/v1',
      product: 'fixture-suite',
      version: '2026.08',
    },
  };
}

function pageOf<T>(items: T[]) {
  return {items, nextCursor: items.length > 0 ? 'fixture-next-cursor' : undefined, total: items.length};
}

function isControlPlaneRead(route: Route): boolean {
  const path = new URL(route.request().url()).pathname;
  return (
    route.request().method() === 'GET' &&
    (path.startsWith('/api/v1/control-plane/') || path.startsWith('/api/v1/approval-requests'))
  );
}

function isLocalHost(hostname: string): boolean {
  return hostname === '127.0.0.1' || hostname === 'localhost' || hostname === '[::1]';
}

async function json(route: Route, body: unknown, status = 200): Promise<void> {
  await route.fulfill({
    status,
    contentType: 'application/json',
    body: JSON.stringify(body),
  });
}
