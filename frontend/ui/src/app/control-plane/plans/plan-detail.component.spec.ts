import {ComponentFixture, TestBed} from '@angular/core/testing';
import {ActivatedRoute, convertToParamMap, provideRouter, Router} from '@angular/router';
import {BehaviorSubject, of, Subject, throwError} from 'rxjs';
import {vi} from 'vitest';
import {OperatorControlPlaneService} from '../../services/operator-control-plane.service';
import {OverlayService} from '../../services/overlay.service';
import {
  OperatorEvidencePage,
  OperatorPlanDetail,
  OperatorPlanDetailResponse,
  OperatorPlanFact,
  OperatorPlanRow,
  OperatorReviewAdmissionMaterial,
} from '../../types/operator-control-plane';
import {PlanDetailComponent} from './plan-detail.component';

describe('PlanDetailComponent', () => {
  let service: {
    getPlan: ReturnType<typeof vi.fn>;
    getPlanEvidence: ReturnType<typeof vi.fn>;
    comparePlans: ReturnType<typeof vi.fn>;
    validatePlanDraft: ReturnType<typeof vi.fn>;
    publishPlanDraft: ReturnType<typeof vi.fn>;
    requestPlanApproval: ReturnType<typeof vi.fn>;
    getReviewAdmissionMaterial: ReturnType<typeof vi.fn>;
    recordReviewAdmissionDecision: ReturnType<typeof vi.fn>;
    createPreviousStatePlan: ReturnType<typeof vi.fn>;
  };
  let overlay: {confirm: ReturnType<typeof vi.fn>};
  let router: Router;
  let routeParams: BehaviorSubject<ReturnType<typeof convertToParamMap>>;

  const plan: OperatorPlanRow = {
    id: 'plan-1',
    createdAt: '2026-07-28T01:00:00Z',
    status: 'READY',
    planSchema: 'distr.target-deployment-plan/v2',
    protocolVersion: 'v2',
    productReleaseId: 'release-1',
    productReleaseVersion: '2026.7.1',
    environmentId: 'environment-1',
    environment: 'Production',
    deploymentUnitId: 'unit-1',
    deploymentUnit: 'Payments',
    targetConfigSnapshotId: 'snapshot-1',
    canonicalChecksum: 'sha256:canonical',
    targetCount: 2,
    stepCount: 4,
    issueCount: 2,
    blockingIssueCount: 1,
    approvalBlockerCount: 1,
    preflightBlockerCount: 0,
    bootstrap: false,
  };

  const fact = (key: string, checksum: string, blocking = false): OperatorPlanFact => ({
    id: `${key}-1`,
    key,
    kind: `${key}-kind`,
    status: blocking ? 'BLOCKED' : 'VERIFIED',
    expected: `expected-${key}`,
    actual: `actual-${key}`,
    checksum,
    message: `${key} message`,
    blocking,
    order: 1,
  });

  const detail: OperatorPlanDetail = {
    plan,
    productReleaseChecksum: 'sha256:product',
    targetConfigChecksum: 'sha256:config',
    effectivePolicyChecksum: 'sha256:policy',
    subscriberSetChecksum: 'sha256:subscribers',
    graphChecksum: 'sha256:graph',
    changeChecksum: 'sha256:change',
    baselineChecksum: 'sha256:baseline',
    providerResolutionChecksum: 'sha256:provider',
    migrationChecksum: 'sha256:migration',
    riskChecksum: 'sha256:risk',
    approvalChecksum: 'sha256:approval',
    windowChecksum: 'sha256:window',
    adapterChecksum: 'sha256:adapter',
    intentChecksum: 'sha256:intent',
    targets: [fact('target', 'sha256:target')],
    baselines: [fact('baseline', 'sha256:baseline-fact')],
    config: [fact('config', 'sha256:config-fact')],
    requirements: [fact('provider', 'sha256:provider-fact')],
    migrations: [fact('migration', 'sha256:migration-fact')],
    changes: [fact('change', 'sha256:change-fact')],
    risks: [fact('risk', 'sha256:risk-fact')],
    approvals: [fact('approval', 'sha256:approval-fact', true)],
    windows: [fact('window', 'sha256:window-fact')],
    adapters: [fact('adapter', 'sha256:adapter-fact')],
    steps: [fact('step', 'sha256:step')],
    edges: [fact('edge', 'sha256:edge')],
    issues: [fact('issue', 'sha256:issue', true)],
    intentBlockers: [fact('intent', 'sha256:intent-fact', true)],
    evidence: [],
  };

  const evidence: OperatorEvidencePage = {
    items: [
      {
        id: 'evidence-1',
        kind: 'manifest',
        label: 'Signed plan manifest',
        href: '/api/v1/evidence/evidence-1',
        checksum: 'sha256:evidence',
        createdAt: '2026-07-28T01:05:00Z',
      },
    ],
  };

  const reviewMaterial: OperatorReviewAdmissionMaterial = {
    deploymentPlanId: 'plan-1',
    planRevision: 1,
    planChecksum: 'sha256:canonical',
    observedStateChecksum: 'sha256:observed',
    reviewMaterialChecksum: 'sha256:review-material',
    reviewMaterialValid: true,
    admissionValid: true,
    admissionEvaluationId: 'admission-1',
    admissionDecision: 'ADMIT',
    admissionDecisionChecksum: 'sha256:admission',
    state: 'GO',
    canDecide: true,
    blockers: [],
    latestDecision: {
      id: 'review-1',
      createdAt: '2026-07-28T01:06:00Z',
      organizationId: 'organization-1',
      deploymentPlanId: 'plan-1',
      planRevision: 1,
      planChecksum: 'sha256:canonical',
      reviewMaterialChecksum: 'sha256:review-material',
      observedStateChecksum: 'sha256:observed',
      decision: 'GO',
      reason: 'Current observed state is ready.',
      actorUserAccountId: 'approver-1',
      expiresAt: '2026-08-01T12:00:00Z',
      authorizationEvidence: 'sha256:authorization',
      canonicalChecksum: 'sha256:decision',
      idempotencyKey: 'review-1',
    },
  };

  beforeEach(() => {
    service = {
      getPlan: vi.fn().mockReturnValue(of({detail} satisfies OperatorPlanDetailResponse)),
      getPlanEvidence: vi.fn().mockReturnValue(of(evidence)),
      comparePlans: vi
        .fn()
        .mockReturnValue(of({comparison: {left: plan, right: {...plan, id: 'plan-2'}, changes: []}})),
      validatePlanDraft: vi.fn(),
      publishPlanDraft: vi.fn(),
      requestPlanApproval: vi.fn(),
      getReviewAdmissionMaterial: vi.fn().mockReturnValue(of(reviewMaterial)),
      recordReviewAdmissionDecision: vi.fn(),
      createPreviousStatePlan: vi.fn(),
    };
    overlay = {confirm: vi.fn().mockReturnValue(of(true))};
    routeParams = new BehaviorSubject(convertToParamMap({planId: 'plan-1'}));
    TestBed.configureTestingModule({
      imports: [PlanDetailComponent],
      providers: [
        provideRouter([]),
        {
          provide: ActivatedRoute,
          useValue: {
            paramMap: routeParams.asObservable(),
            snapshot: {
              paramMap: convertToParamMap({planId: 'plan-1'}),
              queryParamMap: convertToParamMap({}),
            },
          },
        },
        {provide: OperatorControlPlaneService, useValue: service},
        {provide: OverlayService, useValue: overlay},
      ],
    });
    router = TestBed.inject(Router);
  });

  it('resets and reloads immutable detail when Angular reuses the component for a new plan id', () => {
    const {fixture, component} = createComponent();
    routeParams.next(convertToParamMap({planId: 'plan-published'}));
    fixture.detectChanges();

    expect(service.getPlan.mock.calls.at(-1)).toEqual(['plan-published']);
    expect(service.getPlanEvidence.mock.calls.at(-1)).toEqual(['plan-published']);
    expect((component as any).planId).toBe('plan-published');
  });

  it('ignores a late response from the previous plan after same-route navigation', () => {
    const oldDetail = new Subject<OperatorPlanDetailResponse>();
    const oldEvidence = new Subject<OperatorEvidencePage>();
    service.getPlan
      .mockReturnValueOnce(oldDetail)
      .mockReturnValueOnce(of({detail: {...detail, plan: {...plan, id: 'plan-published'}}}));
    service.getPlanEvidence.mockReturnValueOnce(oldEvidence).mockReturnValueOnce(of(evidence));
    const {component} = createComponent();

    routeParams.next(convertToParamMap({planId: 'plan-published'}));
    oldDetail.next({detail});
    oldEvidence.next(evidence);

    expect((component as any).detail()?.plan.id).toBe('plan-published');
  });

  it('renders every approval-critical checksum, blocker, and evidence section in the detail page', () => {
    const {fixture} = createComponent();
    const text = fixture.nativeElement.textContent;

    for (const label of [
      'Canonical plan checksum',
      'Product release checksum',
      'Target config checksum',
      'Effective policy checksum',
      'Subscriber set checksum',
      'Graph checksum',
      'Change checksum',
      'Baseline checksum',
      'Provider resolution checksum',
      'Migration checksum',
      'Risk checksum',
      'Approval checksum',
      'Window checksum',
      'Adapter checksum',
      'Intent checksum',
    ]) {
      expect(text).withContext(label).toContain(label);
    }
    for (const value of [
      'sha256:canonical',
      'sha256:product',
      'sha256:config',
      'sha256:policy',
      'sha256:subscribers',
      'sha256:graph',
      'sha256:provider',
      'sha256:migration',
      'sha256:risk',
      'sha256:approval',
      'sha256:window',
      'sha256:adapter',
      'sha256:intent',
    ]) {
      expect(text).withContext(value).toContain(value);
    }
    for (const section of [
      'Targets',
      'Baselines',
      'Configuration',
      'Provider requirements',
      'Migrations',
      'Changes',
      'Risks',
      'Approvals',
      'Windows',
      'Adapters',
      'Intent blockers',
      'Steps',
      'Graph edges',
      'Issues',
      'Evidence',
    ]) {
      expect(text).withContext(section).toContain(section);
    }
    expect(text).toContain('Blocking');
    expect(text).toContain('Signed plan manifest');
    expect(text).toContain('sha256:evidence');
    expect(text).toContain('GO / NO_GO review');
    expect(text).toContain('Current GO');
    expect(text).toContain('sha256:review-material');
  });

  it('shows current NO_GO and stale review states explicitly', () => {
    service.getReviewAdmissionMaterial.mockReturnValue(
      of({
        ...reviewMaterial,
        state: 'NO_GO',
        latestDecision: {...reviewMaterial.latestDecision!, decision: 'NO_GO'},
      })
    );
    let result = createComponent();
    expect(result.fixture.nativeElement.textContent).toContain('Current NO_GO');
    expect(result.fixture.nativeElement.textContent).toContain('Latest NO_GO');

    TestBed.resetTestingModule();
    service.getReviewAdmissionMaterial.mockReturnValue(
      of({
        ...reviewMaterial,
        state: 'STALE',
        canDecide: false,
        admissionValid: false,
        blockers: ['latest deployment admission is missing, stale, or not ADMIT'],
      })
    );
    configureTestBed();
    result = createComponent();
    expect(result.fixture.nativeElement.textContent).toContain('Stale review decision');
    expect(result.fixture.nativeElement.textContent).toContain('GO / NO_GO controls are disabled');
  });

  it('disables both review controls when material or admission is invalid', () => {
    service.getReviewAdmissionMaterial.mockReturnValue(
      of({
        ...reviewMaterial,
        reviewMaterialValid: false,
        admissionValid: false,
        canDecide: false,
        blockers: ['review material is incomplete', 'latest deployment admission is not ADMIT'],
      })
    );
    const {fixture, component} = createComponent();
    (component as any).reviewDecisionForm.setValue({
      reason: 'Reviewed exact evidence.',
      expiresAt: '2026-08-01T12:00Z',
    });
    fixture.detectChanges();

    const go = fixture.nativeElement.querySelector('[data-testid="record-review-go"]') as HTMLButtonElement;
    const noGo = fixture.nativeElement.querySelector('[data-testid="record-review-no-go"]') as HTMLButtonElement;
    expect(go.disabled).toBe(true);
    expect(noGo.disabled).toBe(true);
    expect(fixture.nativeElement.textContent).toContain('review material is incomplete');
  });

  it('posts checksum-bound NO_GO that supersedes and revokes the current GO tip', async () => {
    service.recordReviewAdmissionDecision.mockReturnValue(
      of({...reviewMaterial.latestDecision!, id: 'review-2', decision: 'NO_GO'})
    );
    const refreshed = {...reviewMaterial, state: 'NO_GO' as const};
    service.getReviewAdmissionMaterial.mockReturnValueOnce(of(reviewMaterial)).mockReturnValueOnce(of(refreshed));
    const {component} = createComponent();
    (component as any).reviewDecisionForm.setValue({
      reason: 'Runtime evidence requires investigation.',
      expiresAt: '2026-08-01T12:00Z',
    });

    await (component as any).recordReviewDecision('NO_GO');

    expect(overlay.confirm.mock.calls[0][0].requiredConfirmInputText).toBe('sha256:review-material');
    expect(service.recordReviewAdmissionDecision).toHaveBeenCalledWith('plan-1', {
      expectedPlanChecksum: 'sha256:canonical',
      reviewMaterialChecksum: 'sha256:review-material',
      observedStateChecksum: 'sha256:observed',
      decision: 'NO_GO',
      reason: 'Runtime evidence requires investigation.',
      expiresAt: '2026-08-01T12:00:00.000Z',
      supersedesDecisionId: 'review-1',
      revokesDecisionId: 'review-1',
    });
    expect(service.getReviewAdmissionMaterial).toHaveBeenCalledTimes(2);
  });

  it('renders forward and previous-state Customer and Transaction checkpoints as immutable facts', () => {
    service.getPlan.mockReturnValue(
      of({
        detail: {
          ...detail,
          baselines: [
            {
              ...fact('Baseline checkpoint C0/T0', 'sha256:c0-t0'),
              expected: 'C0/T0',
              actual: 'C0/T0',
              message: 'customer-api 1.0.0 and transaction-api 2.0.0 are healthy',
            },
          ],
          requirements: [
            {
              ...fact('customer.api exact requirement', 'sha256:customer-binding'),
              status: 'included',
              expected: 'customer.api =1.1.0',
              actual: 'customer-api 1.1.0 (included)',
            },
          ],
          steps: [
            {
              ...fact('Forward checkpoint C1/T0', 'sha256:c1-t0'),
              expected: 'C1/T0',
              actual: 'C1/T0',
            },
            {
              ...fact('Forward checkpoint C1/T1', 'sha256:c1-t1'),
              expected: 'C1/T1',
              actual: 'C1/T1',
            },
            {
              ...fact('Previous-state checkpoint C1/T0', 'sha256:previous-c1-t0'),
              expected: 'C1/T0',
              actual: 'C1/T0',
            },
            {
              ...fact('Previous-state checkpoint C0/T0', 'sha256:previous-c0-t0'),
              expected: 'C0/T0',
              actual: 'C0/T0',
            },
          ],
        },
      } satisfies OperatorPlanDetailResponse)
    );

    const {fixture} = createComponent();
    const text = fixture.nativeElement.textContent;

    expect(text).toContain('Baseline checkpoint C0/T0');
    expect(text).toContain('customer.api =1.1.0');
    expect(text).toContain('customer-api 1.1.0 (included)');
    expect(text).toContain('Forward checkpoint C1/T0');
    expect(text).toContain('Forward checkpoint C1/T1');
    expect(text).toContain('Previous-state checkpoint C1/T0');
    expect(text).toContain('Previous-state checkpoint C0/T0');
  });

  it('keeps loading visible and renders partial evidence without hiding the plan review', () => {
    const response = new Subject<OperatorPlanDetailResponse>();
    const evidenceResponse = new Subject<OperatorEvidencePage>();
    service.getPlan.mockReturnValue(response);
    service.getPlanEvidence.mockReturnValue(evidenceResponse);

    const {fixture} = createComponent();
    expect(fixture.nativeElement.textContent).toContain('Loading plan review');

    response.next({detail});
    response.complete();
    evidenceResponse.error({
      status: 503,
      code: 'SERVER_ERROR',
      message: 'Evidence service unavailable.',
      retryable: true,
    });
    fixture.detectChanges();

    expect(fixture.nativeElement.textContent).toContain('Canonical plan checksum');
    expect(fixture.nativeElement.textContent).toContain('Evidence is partial');
    expect(fixture.nativeElement.textContent).toContain('Evidence service unavailable');
  });

  it('renders dedicated 403 and 404 states and a retryable generic failure', () => {
    service.getPlan.mockReturnValue(
      throwError(() => ({status: 403, code: 'FORBIDDEN', message: 'forbidden', retryable: false}))
    );
    let result = createComponent();
    expect(result.fixture.nativeElement.textContent).toContain('Plan review is not available for your role');

    resetWithPlanError({status: 404, code: 'NOT_FOUND', message: 'not found', retryable: false});
    result = createComponent();
    expect(result.fixture.nativeElement.textContent).toContain('Deployment plan not found');

    resetWithPlanError({status: 503, code: 'SERVER_ERROR', message: 'Plan read failed.', retryable: true});
    result = createComponent();
    expect(result.fixture.nativeElement.textContent).toContain('Plan read failed');
    expect(result.fixture.nativeElement.textContent).toContain('Retry');
  });

  it('shows partial, stale, disabled, and unknown plan states explicitly', () => {
    service.getPlan.mockReturnValue(
      of({
        detail: {
          ...detail,
          productReleaseChecksum: '',
          plan: {...plan, status: 'STALE', canonicalChecksum: ''},
        },
      })
    );
    let result = createComponent();
    expect(result.fixture.nativeElement.textContent).toContain('This plan review is partial');
    expect(result.fixture.nativeElement.textContent).toContain('Stale plan');

    resetWithDetail({...detail, plan: {...plan, status: 'DISABLED'}});
    result = createComponent();
    expect(result.fixture.nativeElement.textContent).toContain('Plan disabled');
    expect(result.fixture.nativeElement.textContent).toContain('Actions are disabled');

    resetWithDetail({...detail, plan: {...plan, status: 'ALIEN_STATE'}});
    result = createComponent();
    expect(result.fixture.nativeElement.textContent).toContain('Unknown status: ALIEN_STATE');
    expect(result.fixture.nativeElement.textContent).toContain('Actions are disabled');
  });

  it('compares against an immutable plan and renders the returned checksum changes', () => {
    service.comparePlans.mockReturnValue(
      of({
        comparison: {
          left: plan,
          right: {...plan, id: 'plan-2', canonicalChecksum: 'sha256:other'},
          changes: [fact('config-change', 'sha256:compare-change')],
        },
      })
    );
    const {fixture, component} = createComponent();
    (component as any).compareForm.controls.otherPlanId.setValue('plan-2');

    (component as any).compare();
    fixture.detectChanges();

    expect(service.comparePlans).toHaveBeenCalledWith('plan-1', 'plan-2');
    expect(fixture.nativeElement.textContent).toContain('Comparison with plan-2');
    expect(fixture.nativeElement.textContent).toContain('sha256:compare-change');
  });

  it('validates a draft without confirmation and keeps the returned revision and preview checksum visible', async () => {
    service.validatePlanDraft.mockReturnValue(
      of({
        draft: {
          id: 'draft-1',
          createdAt: '2026-07-28T01:00:00Z',
          updatedAt: '2026-07-28T01:02:00Z',
          createdByUserAccountId: 'user-1',
          updatedByUserAccountId: 'user-1',
          revision: 4,
          productReleaseId: 'release-1',
          deploymentUnitId: 'unit-1',
          environmentAssignmentId: 'assignment-1',
          targetConfigSnapshotId: 'snapshot-1',
          protocolVersion: 'v2',
          previewChecksum: 'sha256:preview-4',
        },
        resolutions: [],
        graph: {steps: [], edges: [], topologicalOrder: [], checksum: 'sha256:graph'},
        baselines: [],
        changes: [],
        risks: [],
        bootstrap: false,
        issues: [],
        previewChecksum: 'sha256:preview-4',
      })
    );
    const {fixture, component} = createComponent();
    (component as any).draftForm.controls.draftId.setValue('draft-1');

    await (component as any).validateDraft();
    fixture.detectChanges();

    expect(overlay.confirm).not.toHaveBeenCalled();
    expect(service.validatePlanDraft).toHaveBeenCalledWith('draft-1');
    expect(fixture.nativeElement.textContent).toContain('Draft revision 4');
    expect(fixture.nativeElement.textContent).toContain('sha256:preview-4');
  });

  it('confirms a checksum-bound publish and navigates to the resulting immutable plan', async () => {
    service.publishPlanDraft.mockReturnValue(of({id: 'plan-published'}));
    vi.spyOn(router, 'navigate').mockResolvedValue(true);
    const {component} = createComponent();
    (component as any).draftForm.setValue({
      draftId: 'draft-1',
      expectedRevision: 4,
      expectedPreviewChecksum: 'sha256:preview-4',
    });

    await (component as any).publishDraft();

    expect(overlay.confirm.mock.calls[0][0].requiredConfirmInputText).toBe('sha256:preview-4');
    expect(service.publishPlanDraft).toHaveBeenCalledWith('draft-1', {
      expectedRevision: 4,
      expectedPreviewChecksum: 'sha256:preview-4',
    });
    expect(router.navigate).toHaveBeenCalledWith(['/deployments/plans', 'plan-published'], {
      queryParams: {deploymentUnitId: 'unit-1'},
    });
  });

  it('confirms approval scope and navigates to the resulting immutable approval request', async () => {
    service.requestPlanApproval.mockReturnValue(of({id: 'approval-1'}));
    vi.spyOn(router, 'navigate').mockResolvedValue(true);
    const {component} = createComponent();
    (component as any).approvalForm.controls.expiresAt.setValue('2026-08-01T12:00Z');

    await (component as any).requestApproval();

    expect(overlay.confirm).toHaveBeenCalled();
    expect(service.requestPlanApproval).toHaveBeenCalledWith('plan-1', {
      expiresAt: '2026-08-01T12:00:00.000Z',
    });
    expect(router.navigate).toHaveBeenCalledWith(['/approvals'], {queryParams: {requestId: 'approval-1'}});
  });

  it('requires typed immutable plan confirmation before creating and opening a previous-state plan', async () => {
    service.createPreviousStatePlan.mockReturnValue(of({id: 'plan-previous'}));
    vi.spyOn(router, 'navigate').mockResolvedValue(true);
    const {component} = createComponent();
    (component as any).previousStateForm.setValue({
      successfulDeploymentPlanId: 'plan-successful',
      reason: 'Restore the last independently verified state.',
    });

    await (component as any).createPreviousState();

    expect(overlay.confirm.mock.calls[0][0].requiredConfirmInputText).toBe('plan-successful');
    expect(service.createPreviousStatePlan).toHaveBeenCalledWith('plan-1', {
      successfulDeploymentPlanId: 'plan-successful',
      reason: 'Restore the last independently verified state.',
    });
    expect(router.navigate).toHaveBeenCalledWith(['/deployments/plans', 'plan-previous'], {
      queryParams: {deploymentUnitId: 'unit-1'},
    });
  });

  function createComponent(): {fixture: ComponentFixture<PlanDetailComponent>; component: PlanDetailComponent} {
    const fixture = TestBed.createComponent(PlanDetailComponent);
    fixture.detectChanges();
    return {fixture, component: fixture.componentInstance};
  }

  function resetWithPlanError(error: unknown) {
    TestBed.resetTestingModule();
    service.getPlan.mockReturnValue(throwError(() => error));
    configureTestBed();
  }

  function resetWithDetail(nextDetail: OperatorPlanDetail) {
    TestBed.resetTestingModule();
    service.getPlan.mockReturnValue(of({detail: nextDetail}));
    configureTestBed();
  }

  function configureTestBed() {
    TestBed.configureTestingModule({
      imports: [PlanDetailComponent],
      providers: [
        provideRouter([]),
        {
          provide: ActivatedRoute,
          useValue: {
            paramMap: of(convertToParamMap({planId: 'plan-1'})),
            snapshot: {
              paramMap: convertToParamMap({planId: 'plan-1'}),
              queryParamMap: convertToParamMap({}),
            },
          },
        },
        {provide: OperatorControlPlaneService, useValue: service},
        {provide: OverlayService, useValue: overlay},
      ],
    });
    router = TestBed.inject(Router);
  }
});
