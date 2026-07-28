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

const actorClaims: Record<OperatorActor, Record<string, unknown>> = {
  vendorAdmin: {
    sub: 'user-vendor-admin',
    email: 'vendor-admin@example.invalid',
    name: 'Vendor administrator',
    role: 'admin',
    authorities: ['registry.manage', 'release.manage', 'plan.manage', 'campaign.control', 'audit.export'],
  },
  scopedApprover: {
    sub: 'user-scoped-approver',
    email: 'scoped-approver@example.invalid',
    name: 'Scoped approver',
    role: 'read_write',
    authorities: ['approval.decide'],
    authority_scope: {environmentIds: ['environment-production']},
  },
  executorOperator: {
    sub: 'user-executor-operator',
    email: 'executor-operator@example.invalid',
    name: 'Executor operator',
    role: 'read_write',
    authorities: ['campaign.control', 'execution.control', 'reconciliation.resolve'],
    authority_scope: {environmentIds: ['environment-production']},
  },
  auditViewer: {
    sub: 'user-audit-viewer',
    email: 'audit-viewer@example.invalid',
    name: 'Audit viewer',
    role: 'read_only',
    authorities: ['audit.read'],
  },
  unauthorized: {
    sub: 'user-customer-reader',
    email: 'customer-reader@example.invalid',
    name: 'Customer reader',
    role: 'read_only',
    c_org: 'customer-organization',
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
    id: 'fleet-payments-a',
    createdAt: timestamp,
    customerOrganizationId: 'customer-a',
    customer: 'Customer A',
    environmentId: 'environment-production',
    environment: 'Production',
    deploymentTargetId: 'target-shared',
    target: 'Shared production host',
    deploymentUnitId: 'unit-a',
    unit: 'Customer A unit',
    componentId: 'component-payments',
    component: 'payments-api',
    activeReleaseId: 'release-component-1',
    activeRelease: 'payments-api 2.4.0',
    pendingReleaseId: 'release-component-2',
    pendingRelease: 'payments-api 2.5.0',
    observedState: 'healthy',
    drift: 'none',
    lastExecutionId: 'execution-1',
    lastExecution: 'succeeded',
    enrollment: 'enabled',
  },
  {
    id: 'fleet-payments-b',
    createdAt: timestamp,
    customerOrganizationId: 'customer-b',
    customer: 'Customer B',
    environmentId: 'environment-production',
    environment: 'Production',
    deploymentTargetId: 'target-shared',
    target: 'Shared production host',
    deploymentUnitId: 'unit-b',
    unit: 'Customer B unit',
    componentId: 'component-payments',
    component: 'payments-api',
    activeReleaseId: 'release-component-1',
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
    id: 'release-component-1',
    createdAt: timestamp,
    kind: 'component',
    applicationId: 'application-payments',
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
    id: 'release-product-1',
    createdAt: timestamp,
    kind: 'product',
    applicationId: 'application-suite',
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
  id: 'plan-1',
  createdAt: timestamp,
  status: 'blocked',
  planSchema: 'distr.plan.v2',
  protocolVersion: 'v2',
  productReleaseId: 'release-product-1',
  productReleaseVersion: '2026.07',
  environmentId: 'environment-production',
  environment: 'Production',
  deploymentUnitId: 'unit-a',
  deploymentUnit: 'Customer A unit',
  targetConfigSnapshotId: 'config-snapshot-1',
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
    id: 'evidence-1',
    kind: 'provenance',
    label: 'Build provenance',
    href: '/deployments/plans/plan-1',
    checksum: checksums.product,
    createdAt: timestamp,
  },
];

const campaignRow = {
  id: 'campaign-1',
  createdAt: timestamp,
  draftId: 'campaign-draft-1',
  revisionId: 'campaign-revision-1',
  runId: 'campaign-run-1',
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
  id: 'execution-1',
  createdAt: timestamp,
  campaignId: 'campaign-1',
  deploymentPlanId: 'plan-1',
  deploymentTargetId: 'target-shared',
  taskId: 'task-1',
  stepRunId: 'step-run-1',
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
  id: 'reconciliation-1',
  createdAt: timestamp,
  driftCaseId: 'drift-1',
  executionId: 'execution-1',
  deploymentPlanId: 'plan-1',
  environmentId: 'environment-production',
  deploymentTargetId: 'target-shared',
  component: 'payments-api',
  drift: 'artifact_mismatch',
  status: 'OPEN',
  outcome: 'pending',
  observedAt: timestamp,
  evidenceChecksum: checksums.intent,
};

const auditRow = {
  id: 'audit-1',
  createdAt: timestamp,
  sequence: 42,
  action: 'deployment_plan.approved',
  subjectType: 'deployment_plan',
  subjectId: 'plan-1',
  actorUserAccountId: 'user-scoped-approver',
  outcome: 'accepted',
  correlationCount: 2,
  payloadChecksum: checksums.approval,
};

const approvalRequest = {
  id: 'approval-1',
  createdAt: timestamp,
  updatedAt: timestamp,
  subjectType: 'deployment_plan',
  subjectId: 'plan-1',
  subjectRevision: 2,
  subjectChecksum: checksums.canonical,
  effectivePolicyChecksum: checksums.policy,
  subscriberSetChecksum: checksums.subscriber,
  requesterUserAccountId: 'user-vendor-admin',
  state: 'PENDING',
  revision: 3,
  expiresAt: '2026-07-29T08:00:00Z',
  requirements: [
    {
      id: 'approval-requirement-1',
      ruleKey: 'production-four-eyes',
      policyVersionId: 'policy-version-1',
      authorityKind: 'environment',
      authorityId: 'environment-production',
      principalGroupId: 'approvers',
      quorum: 1,
      separationConstraints: ['requester_cannot_approve'],
      sortOrder: 1,
    },
  ],
  decisions: [],
};

const invalidatedApproval = {
  ...approvalRequest,
  id: 'approval-invalidated',
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
  } else if (method === 'GET' && path === '/api/v1/control-plane/releases/release-product-1') {
    await json(route, {
      detail: {
        release: releases[1],
        artifacts: [
          {
            id: 'artifact-1',
            name: 'payments-api',
            version: '2.5.0',
            manifestDigest: executionRow.artifactDigest,
            platformDigests: {'linux/amd64': executionRow.artifactDigest},
          },
        ],
        componentPins: [
          {
            componentReleaseId: 'release-component-1',
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
    path === '/api/v1/control-plane/releases/release-product-1/compare/release-component-1'
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
  } else if (method === 'GET' && path === '/api/v1/control-plane/plans/plan-1') {
    await json(route, {
      detail: {
        plan: planRow,
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
  } else if (method === 'GET' && path === '/api/v1/control-plane/campaigns/campaign-1') {
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
            id: 'wave-1',
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
            id: 'member-1',
            deploymentPlanId: 'plan-1',
            deploymentUnitId: 'unit-a',
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
  } else if (method === 'GET' && path === '/api/v1/control-plane/executions/execution-1') {
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
  } else if (method === 'GET' && path === '/api/v1/approval-requests/approval-1') {
    await json(route, currentApproval(actions));
  } else if (method === 'GET' && path === '/api/v1/control-plane/reconciliation') {
    await json(route, pageOf(empty ? [] : [reconciliationRow]));
  } else if (method === 'GET' && path === '/api/v1/control-plane/reconciliation/reconciliation-1') {
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
  } else if (method === 'GET' && path === '/api/v1/control-plane/audit/audit-1') {
    await json(route, {
      detail: {
        event: auditRow,
        correlations: [
          {id: 'correlation-1', kind: 'deployment_plan', value: 'plan-1'},
          {id: 'correlation-2', kind: 'campaign', value: 'campaign-1'},
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
              id: 'sink-1',
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
          id: 'sink-1',
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
        lastExportedEventId: 'audit-previous',
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
        deploymentPlanId: 'plan-1',
        events: [
          {
            id: 'audit-1',
            sequence: 42,
            eventType: 'deployment_plan.approved',
            outcome: 'accepted',
            deploymentPlanId: 'plan-1',
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
  } else if (method === 'POST' && path.includes('/deployment-registry/imports/registry-import-1/decisions')) {
    await route.fulfill({status: 204});
  } else if (method === 'GET' && path === '/api/v1/deployment-registry/imports/registry-import-1') {
    await json(route, registryImport('standard'));
  } else if (method === 'POST' && path.includes('/deployment-registry/imports/registry-import-1/apply')) {
    await json(route, {
      id: 'registry-import-1',
      previewChecksum: checksums.canonical,
      state: 'applied',
      applied: true,
      counts: registryCounts(),
      checkpoint: 1,
    });
  } else if (
    method === 'GET' &&
    path === '/api/v1/deployment-registry/coverage' &&
    url.searchParams.get('importId') === 'registry-import-1'
  ) {
    await json(route, registryCoverage());
  } else if (method === 'GET' && path === '/api/v1/authorization/control-plane-enrollments') {
    await json(route, {
      enrollments: [
        {
          id: 'enrollment-1',
          createdAt: timestamp,
          scope: {kind: 'environment', id: 'environment-production'},
          enabled: true,
          effectiveFrom: timestamp,
          actorUserAccountId: 'user-vendor-admin',
          reason: 'Fixture enrollment',
          revision: 1,
        },
      ],
    });
  } else if (method === 'GET' && path === '/api/v1/target-config-snapshots/') {
    await json(route, {items: url.searchParams.has('deploymentUnitId') ? [{id: 'config-snapshot-1'}] : []});
  } else if (method === 'POST' && path === '/api/v1/release-bundles') {
    await json(route, componentRelease('DRAFT'), 201);
  } else if (method === 'POST' && path === '/api/v1/release-bundles/release-component-draft/validate') {
    await json(route, {valid: true, errors: [], warnings: []});
  } else if (method === 'POST' && path === '/api/v1/release-bundles/release-component-draft/publish') {
    await json(route, componentRelease('PUBLISHED'));
  } else if (method === 'POST' && path === '/api/v1/product-releases') {
    await json(route, productRelease('DRAFT'), 201);
  } else if (method === 'POST' && path === '/api/v1/product-releases/release-product-draft/validate') {
    await json(route, {valid: true, issues: []});
  } else if (method === 'POST' && path === '/api/v1/product-releases/release-product-draft/publish') {
    await json(route, productRelease('PUBLISHED'));
  } else if (method === 'POST' && path === '/api/v1/approval-requests/approval-1/decisions') {
    await json(route, currentApproval(actions), 201);
  } else if (
    method === 'POST' &&
    /^\/api\/v1\/deployment-campaigns\/campaign-run-1\/(pause|resume|cancel)$/.test(path)
  ) {
    const action = path.split('/').at(-1);
    await json(route, {
      requestId: `${action}-request-1`,
      status: 'accepted',
      run: {
        id: 'campaign-run-1',
        createdAt: timestamp,
        updatedAt: timestamp,
        campaignRevisionId: 'campaign-revision-1',
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
  } else if (method === 'POST' && path === '/api/v1/executions/execution-1/cancel') {
    await json(route, {status: 'CANCEL_REQUESTED'}, 202);
  } else if (method === 'POST' && path === '/api/v1/executions/execution-1/status-queries') {
    await json(route, {status: 'QUERY_REQUESTED'}, 202);
  } else if (method === 'POST' && path === '/api/v1/drift-cases/drift-1/resolve') {
    await route.fulfill({status: 204});
  } else {
    await json(route, {code: 'FIXTURE_ROUTE_MISSING', message: `No deterministic fixture for ${method} ${path}`}, 404);
  }
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
      id: 'vendor-organization',
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
      ? {customerOrganization: {id: 'customer-organization', name: 'Fixture Customer', features: []}}
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
    id: 'registry-import-1',
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
        customerOrganizationId: 'customer-a',
        deploymentTargetId: 'target-shared',
        environmentId: 'environment-production',
        subscriberCustomerOrganizationIds: ['customer-a'],
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
    importId: 'registry-import-1',
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
  const approved = actions.some((action) => action.path === '/api/v1/approval-requests/approval-1/decisions');
  return approved
    ? {
        ...approvalRequest,
        state: 'APPROVED',
        resolvedAt: timestamp,
        decisions: [
          {
            id: 'approval-decision-1',
            createdAt: timestamp,
            approvalRequestId: 'approval-1',
            approvalRequirementId: 'approval-requirement-1',
            decision: 'APPROVE',
            comment: 'Reviewed production checksum and blockers',
            actorUserAccountId: 'user-scoped-approver',
            requestRevision: 3,
            idempotencyKey: 'fixture-approval-decision',
          },
        ],
      }
    : approvalRequest;
}

function componentRelease(status: 'DRAFT' | 'PUBLISHED') {
  return {
    id: 'release-component-draft',
    createdAt: timestamp,
    updatedAt: timestamp,
    applicationId: 'application-payments',
    channelId: 'channel-stable',
    releaseNumber: '25',
    releaseNotes: 'Payments component fixture',
    sourceRevision: '0123456789abcdef',
    kind: 'component',
    releaseContractSchema: 'distr.component-release/v2',
    status,
    ...(status === 'PUBLISHED' ? {publishedByUserAccountId: 'user-vendor-admin', publishedAt: timestamp} : {}),
    canonicalChecksum: checksums.product,
    components: [],
  };
}

function productRelease(status: 'DRAFT' | 'PUBLISHED') {
  return {
    id: 'release-product-draft',
    createdAt: timestamp,
    updatedAt: timestamp,
    applicationId: 'application-suite',
    channelId: 'channel-stable',
    status,
    canonicalChecksum: checksums.canonical,
    graphChecksum: checksums.graph,
    ...(status === 'PUBLISHED' ? {publishedByUserAccountId: 'user-vendor-admin', publishedAt: timestamp} : {}),
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
