import {ComponentFixture, TestBed} from '@angular/core/testing';
import {ActivatedRoute, convertToParamMap, provideRouter, Router} from '@angular/router';
import {of} from 'rxjs';
import {vi} from 'vitest';
import {OperatorControlPlaneService} from '../../services/operator-control-plane.service';
import {OverlayService} from '../../services/overlay.service';
import {
  OperatorDeploymentUnit,
  OperatorPlanDraft,
  OperatorPlanDraftValidation,
  OperatorReleaseRow,
  OperatorTargetPlanStep,
} from '../../types/operator-control-plane';
import {ProcessSnapshot} from '../../types/release-bundle';
import {TargetConfigSnapshot} from '../../types/target-config-snapshot';
import {PlanDraftComponent} from './plan-draft.component';

describe('PlanDraftComponent', () => {
  let service: {
    listReleases: ReturnType<typeof vi.fn>;
    listDeploymentUnits: ReturnType<typeof vi.fn>;
    listTargetConfigSnapshots: ReturnType<typeof vi.fn>;
    getReleaseProcessSnapshot: ReturnType<typeof vi.fn>;
    getPlanDraft: ReturnType<typeof vi.fn>;
    createPlanDraft: ReturnType<typeof vi.fn>;
    updatePlanDraft: ReturnType<typeof vi.fn>;
    validatePlanDraft: ReturnType<typeof vi.fn>;
    publishPlanDraft: ReturnType<typeof vi.fn>;
  };
  let overlay: {confirm: ReturnType<typeof vi.fn>};
  let router: Router;

  const release: OperatorReleaseRow = {
    id: 'release-1',
    createdAt: '2026-08-30T01:00:00Z',
    kind: 'product',
    applicationId: 'application-1',
    application: 'Sample suite',
    clients: [],
    version: '2026.8.30',
    status: 'PUBLISHED',
    checksum: 'sha256:release',
    sourceRevision: 'commit-1',
    artifactCount: 2,
    evidenceCount: 2,
    componentCount: 2,
    graphEdgeCount: 1,
  };

  const unit: OperatorDeploymentUnit = {
    id: 'unit-1',
    createdAt: '2026-08-30T01:00:00Z',
    updatedAt: '2026-08-30T01:00:00Z',
    deploymentScopeId: 'scope-1',
    targetEnvironmentAssignmentId: 'assignment-1',
    deploymentTargetId: 'target-1',
    key: 'sample-dev',
    name: 'Sample development',
    physicalIdentity: 'sample-dev-host',
    managementState: 'managed',
    subscriberSetChecksum: 'sha256:subscribers',
  };

  const snapshot: TargetConfigSnapshot = {
    id: 'snapshot-1',
    createdAt: '2026-08-30T02:00:00Z',
    createdByUserAccountId: 'user-1',
    deploymentUnitId: 'unit-1',
    targetEnvironmentAssignmentId: 'assignment-1',
    environmentId: 'environment-1',
    sourceRepository: 'https://example.invalid/config.git',
    sourceCommit: 'config-commit-1',
    sourceAdapter: 'git',
    adapterVersion: 'v1',
    targetPlatform: 'linux/amd64',
    runtimeConstraints: {},
    canonicalChecksum: 'sha256:config',
    objects: [],
    components: [],
    secretReferences: [],
    featureFlags: [],
  };

  const draft: OperatorPlanDraft = {
    id: 'draft-1',
    createdAt: '2026-08-30T03:00:00Z',
    updatedAt: '2026-08-30T03:00:00Z',
    createdByUserAccountId: 'user-1',
    updatedByUserAccountId: 'user-1',
    revision: 3,
    productReleaseId: 'release-1',
    deploymentUnitId: 'unit-1',
    environmentAssignmentId: 'assignment-1',
    targetConfigSnapshotId: 'snapshot-1',
    protocolVersion: 'v2',
  };

  const processSnapshot: ProcessSnapshot = {
    id: 'process-snapshot-1',
    createdAt: '2026-08-30T01:30:00Z',
    applicationId: 'application-1',
    deploymentProcessId: 'process-1',
    deploymentProcessRevisionId: 'process-revision-1',
    revisionNumber: 7,
    canonicalChecksum: 'sha256:process',
    revision: {
      id: 'process-revision-1',
      createdAt: '2026-08-30T01:00:00Z',
      updatedAt: '2026-08-30T01:00:00Z',
      deploymentProcessId: 'process-1',
      revisionNumber: 7,
      description: 'Deploy provider before consumer',
      steps: [
        {
          id: 'process-step-1',
          deploymentProcessRevisionId: 'process-revision-1',
          key: 'deploy-provider',
          name: 'Deploy provider',
          actionType: 'component.deploy',
          executionLocation: 'target',
          inputBindings: {},
          condition: '',
          channelIds: [],
          environmentIds: [],
          targetTags: [],
          failureMode: 'stop',
          timeoutSeconds: 900,
          retryPolicy: {maxAttempts: 1, intervalSeconds: 0},
          requiredPermissions: [],
          sortOrder: 0,
          dependencies: [],
        },
      ],
    },
  };

  beforeEach(() => {
    service = {
      listReleases: vi.fn().mockReturnValue(of({items: [release]})),
      listDeploymentUnits: vi.fn().mockReturnValue(of({items: [unit]})),
      listTargetConfigSnapshots: vi.fn().mockReturnValue(of({items: [snapshot]})),
      getReleaseProcessSnapshot: vi.fn().mockReturnValue(of(processSnapshot)),
      getPlanDraft: vi.fn().mockReturnValue(of(draft)),
      createPlanDraft: vi.fn().mockReturnValue(of({...draft, revision: 1})),
      updatePlanDraft: vi.fn().mockReturnValue(of({...draft, revision: 4})),
      validatePlanDraft: vi.fn().mockReturnValue(of(validationFixture())),
      publishPlanDraft: vi.fn().mockReturnValue(of({id: 'plan-published'})),
    };
    overlay = {confirm: vi.fn().mockReturnValue(of(true))};
  });

  it('creates a draft from paged selectors and preserves deploymentUnitId navigation context', async () => {
    service.listReleases
      .mockReturnValueOnce(of({items: [release], nextCursor: 'release-cursor'}))
      .mockReturnValueOnce(of({items: [{...release, id: 'release-2', version: '2026.8.29'}]}));
    service.listDeploymentUnits
      .mockReturnValueOnce(of({items: [unit], nextCursor: 'unit-cursor'}))
      .mockReturnValueOnce(of({items: [{...unit, id: 'unit-2', name: 'Sample staging'}]}));
    service.listTargetConfigSnapshots
      .mockReturnValueOnce(of({items: [snapshot], nextCursor: 'snapshot-cursor'}))
      .mockReturnValueOnce(of({items: []}));
    const {fixture, component} = await createComponent('', {deploymentUnitId: 'unit-1'});
    vi.spyOn(router, 'navigate').mockResolvedValue(true);

    (component as any).form.controls.productReleaseId.setValue('release-1');
    await (component as any).selectProductRelease();
    await (component as any).saveDraft();
    fixture.detectChanges();

    expect(service.listReleases).toHaveBeenCalledWith({
      kind: 'product',
      status: 'PUBLISHED',
      cursor: undefined,
      limit: 100,
    });
    expect(service.listReleases).toHaveBeenCalledWith({
      kind: 'product',
      status: 'PUBLISHED',
      cursor: 'release-cursor',
      limit: 100,
    });
    expect(service.listDeploymentUnits).toHaveBeenCalledWith({cursor: 'unit-cursor', limit: 100});
    expect(service.listTargetConfigSnapshots).toHaveBeenCalledWith({
      deploymentUnitId: 'unit-1',
      targetEnvironmentAssignmentId: 'assignment-1',
      cursor: undefined,
      limit: 100,
    });
    expect(service.listTargetConfigSnapshots).toHaveBeenCalledWith({
      deploymentUnitId: 'unit-1',
      targetEnvironmentAssignmentId: 'assignment-1',
      cursor: 'snapshot-cursor',
      limit: 100,
    });
    expect(service.createPlanDraft).toHaveBeenCalledWith({
      productReleaseId: 'release-1',
      deploymentUnitId: 'unit-1',
      environmentAssignmentId: 'assignment-1',
      targetConfigSnapshotId: 'snapshot-1',
      protocolVersion: 'v2',
    });
    expect(router.navigate).toHaveBeenCalledWith(['/deployments/plans/drafts', 'draft-1'], {
      queryParams: {deploymentUnitId: 'unit-1'},
      replaceUrl: true,
    });
    expect(fixture.nativeElement.textContent).toContain('process-snapshot-1');
    expect(fixture.nativeElement.textContent).toContain(
      'The process snapshot is selected by the immutable Product Release'
    );
  });

  it('loads and updates an existing draft with its exact optimistic revision', async () => {
    const {component} = await createComponent('draft-1', {deploymentUnitId: 'unit-1'});
    (component as any).form.controls.protocolVersion.setValue('v1');

    await (component as any).saveDraft();

    expect(service.getPlanDraft).toHaveBeenCalledWith('draft-1');
    expect(service.updatePlanDraft).toHaveBeenCalledWith('draft-1', {
      productReleaseId: 'release-1',
      deploymentUnitId: 'unit-1',
      environmentAssignmentId: 'assignment-1',
      targetConfigSnapshotId: 'snapshot-1',
      protocolVersion: 'v1',
      expectedRevision: 3,
    });
  });

  it('renders the exact customer.api requirement and distinguishes included from pinned_existing', async () => {
    const {fixture, component} = await createComponent('draft-1', {deploymentUnitId: 'unit-1'});

    await (component as any).validateDraft();
    fixture.detectChanges();

    const text = fixture.nativeElement.textContent;
    expect(text).toContain('transaction-api requires customer.api =1.1.0');
    expect(text).toContain('included');
    expect(text).toContain('pinned_existing');
    expect(text).toContain('1.1.0 · linux/amd64');
    expect(text).toContain('v2');
    expect(text).toContain('sha256:expected-customer-state');
    expect(text).toContain('sha256:binding-included');
    expect(text).toContain('sha256:binding-pinned');
    expect(text).toContain('fresh until');
    expect(text).toContain('trusted yes · current yes');
    expect(text).toContain('provider-approval-1');
    expect(text).toContain('sha256:provider-approval');
    expect(text).toContain('contract-probe-1');
    expect(text).toContain('sha256:contract-probe');
    expect(text).toContain('sha256:preview');

    const forward = fixture.nativeElement.querySelector('[aria-labelledby="forward-heading"]').textContent;
    expect(forward.indexOf('Verify target configuration')).toBeLessThan(forward.indexOf('Deploy customer-api'));
    expect(forward.indexOf('Deploy customer-api')).toBeLessThan(forward.indexOf('Deploy transaction-api'));

    const reverse = fixture.nativeElement.querySelector('[aria-labelledby="reverse-heading"]').textContent;
    expect(reverse.indexOf('Deploy transaction-api')).toBeLessThan(reverse.indexOf('Deploy customer-api'));
    expect(reverse).toContain('Forward-only change: use an approved forward fix');
  });

  it('publishes only the validated revision and checksum and preserves context on the immutable plan', async () => {
    const {component} = await createComponent('draft-1', {deploymentUnitId: 'unit-1'});
    vi.spyOn(router, 'navigate').mockResolvedValue(true);
    await (component as any).validateDraft();

    await (component as any).publishDraft();

    expect(overlay.confirm.mock.calls[0][0].requiredConfirmInputText).toBe('sha256:preview');
    expect(service.publishPlanDraft).toHaveBeenCalledWith('draft-1', {
      expectedRevision: 3,
      expectedPreviewChecksum: 'sha256:preview',
    });
    expect(router.navigate).toHaveBeenCalledWith(['/deployments/plans', 'plan-published'], {
      queryParams: {deploymentUnitId: 'unit-1'},
    });
  });

  async function createComponent(
    draftId: string,
    queryParams: Record<string, string>
  ): Promise<{fixture: ComponentFixture<PlanDraftComponent>; component: PlanDraftComponent}> {
    TestBed.configureTestingModule({
      imports: [PlanDraftComponent],
      providers: [
        provideRouter([]),
        {
          provide: ActivatedRoute,
          useValue: {
            snapshot: {
              paramMap: convertToParamMap(draftId ? {draftId} : {}),
              queryParamMap: convertToParamMap(queryParams),
            },
          },
        },
        {provide: OperatorControlPlaneService, useValue: service},
        {provide: OverlayService, useValue: overlay},
      ],
    });
    router = TestBed.inject(Router);
    const fixture = TestBed.createComponent(PlanDraftComponent);
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();
    return {fixture, component: fixture.componentInstance};
  }

  function validationFixture(): OperatorPlanDraftValidation {
    return {
      draft: {...draft, previewChecksum: 'sha256:preview'},
      resolutions: [
        {
          requirementKey: 'transaction-api:customer.api:included',
          consumerKey: 'transaction-api',
          capability: 'customer.api',
          versionRange: '=1.1.0',
          mode: 'included',
          providerReleaseId: 'customer-release-1',
          providerVersion: '1.1.0',
          providerPlatform: 'linux/amd64',
          providerReleaseChecksum: 'sha256:customer-release',
          providerDeploymentUnitId: 'customer-unit-1',
          expectedStateVersion: 1,
          expectedStateChecksum: 'sha256:expected-customer-state',
          bindingChecksum: 'sha256:binding-included',
          sortOrder: 0,
        },
        {
          requirementKey: 'transaction-api:customer.api:pinned',
          consumerKey: 'transaction-api',
          capability: 'customer.api',
          versionRange: '=1.1.0',
          mode: 'pinned_existing',
          observationId: 'customer-observation-c1',
          providerVersion: '1.1.0',
          providerPlatform: 'linux/amd64',
          providerReleaseChecksum: 'sha256:customer-release',
          providerDeploymentUnitId: 'customer-unit-1',
          expectedStateVersion: 2,
          expectedStateChecksum: 'sha256:expected-customer-state',
          providerEvidenceVersion: 2,
          observationFreshUntil: '2026-07-18T04:00:00Z',
          observationTrusted: true,
          observationCurrent: true,
          bindingChecksum: 'sha256:binding-pinned',
          sortOrder: 1,
        },
        {
          requirementKey: 'transaction-api:email.api:external',
          consumerKey: 'transaction-api',
          capability: 'email.api',
          versionRange: '=1.0.0',
          mode: 'approved_external',
          activeDesiredRevisionId: 'external-active-1',
          observedComponentStateId: 'contract-probe-1',
          providerVersion: '1.0.0',
          providerPlatform: 'linux/amd64',
          expectedStateVersion: 3,
          expectedStateChecksum: 'sha256:external-state',
          providerEvidenceVersion: 2,
          observationFreshUntil: '2026-07-18T04:00:00Z',
          observationTrusted: true,
          observationCurrent: true,
          providerApprovalRequestId: 'provider-approval-1',
          providerApprovalChecksum: 'sha256:provider-approval',
          contractProbeObservationId: 'contract-probe-1',
          contractProbeEvidenceChecksum: 'sha256:contract-probe',
          bindingChecksum: 'sha256:binding-external',
          sortOrder: 2,
        },
      ],
      graph: {
        steps: [
          step('component:transaction-api:deploy', 'Deploy transaction-api', 2, 'transaction-api'),
          step('config:verify', 'Verify target configuration', 0),
          step('component:customer-api:deploy', 'Deploy customer-api', 1, 'customer-api'),
        ],
        edges: [
          {
            key: 'customer-before-transaction',
            fromStepKey: 'component:customer-api:deploy',
            toStepKey: 'component:transaction-api:deploy',
          },
        ],
        topologicalOrder: ['config:verify', 'component:customer-api:deploy', 'component:transaction-api:deploy'],
        checksum: 'sha256:graph',
      },
      baselines: [],
      changes: [
        {
          componentKey: 'transaction-api',
          kind: 'schema',
          before: 'v1',
          after: 'v2',
          forwardOnly: true,
          canonicalChecksum: 'sha256:change',
          sortOrder: 0,
        },
      ],
      risks: [],
      bootstrap: false,
      issues: [],
      previewChecksum: 'sha256:preview',
    };
  }

  function step(stepKey: string, name: string, sortOrder: number, componentKey?: string): OperatorTargetPlanStep {
    return {
      stepKey,
      name,
      kind: 'deploy',
      componentKey,
      actionType: 'builtin',
      actionName: 'component.deploy',
      executionLocation: 'target',
      inputBindings: {},
      targetLockKey: 'target-1',
      timeoutSeconds: 900,
      retryClass: 'bounded',
      cancellationBehavior: 'cooperative',
      expectedInputChecksum: `sha256:${sortOrder}`,
      observationRequirement: 'healthy state',
      v1Compatible: true,
      sortOrder,
    };
  }
});
