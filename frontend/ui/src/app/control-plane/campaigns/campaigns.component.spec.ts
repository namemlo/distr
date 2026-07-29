import {HttpErrorResponse} from '@angular/common/http';
import {ComponentFixture, TestBed} from '@angular/core/testing';
import {provideRouter, Router} from '@angular/router';
import {defer, of, Subject, throwError} from 'rxjs';
import {vi} from 'vitest';
import {OperatorControlPlaneService} from '../../services/operator-control-plane.service';
import {OverlayService} from '../../services/overlay.service';
import {
  OperatorCampaignDraft,
  OperatorCampaignRevision,
  OperatorCampaignRow,
  OperatorCampaignRun,
  OperatorCampaignRunState,
  OperatorPage,
} from '../../types/operator-control-plane';
import {CampaignsComponent} from './campaigns.component';

describe('CampaignsComponent', () => {
  let fixture: ComponentFixture<CampaignsComponent>;
  let service: {
    listCampaigns: ReturnType<typeof vi.fn>;
    createCampaignDraft: ReturnType<typeof vi.fn>;
    getCampaignDraft: ReturnType<typeof vi.fn>;
    updateCampaignDraft: ReturnType<typeof vi.fn>;
    validateCampaignDraft: ReturnType<typeof vi.fn>;
    publishCampaignDraft: ReturnType<typeof vi.fn>;
    startCampaignRun: ReturnType<typeof vi.fn>;
    getCampaignRun: ReturnType<typeof vi.fn>;
    transitionCampaignRun: ReturnType<typeof vi.fn>;
  };
  let overlay: {confirm: ReturnType<typeof vi.fn>};
  let router: Router;

  beforeEach(() => {
    service = {
      listCampaigns: vi.fn().mockReturnValue(of({items: []})),
      createCampaignDraft: vi.fn(),
      getCampaignDraft: vi.fn().mockReturnValue(of(campaignDraft())),
      updateCampaignDraft: vi.fn(),
      validateCampaignDraft: vi.fn(),
      publishCampaignDraft: vi.fn(),
      startCampaignRun: vi.fn(),
      getCampaignRun: vi.fn(),
      transitionCampaignRun: vi.fn(),
    };
    overlay = {confirm: vi.fn().mockReturnValue(of(true))};
    TestBed.configureTestingModule({
      imports: [CampaignsComponent],
      providers: [
        provideRouter([]),
        {provide: OperatorControlPlaneService, useValue: service},
        {provide: OverlayService, useValue: overlay},
      ],
    });
    router = TestBed.inject(Router);
  });

  it('shows loading until the first campaign page arrives', () => {
    service.listCampaigns.mockReturnValue(new Subject<OperatorPage<OperatorCampaignRow>>());

    fixture = TestBed.createComponent(CampaignsComponent);
    fixture.detectChanges();

    expect(text()).toContain('Loading campaigns');
  });

  it('renders draft, immutable revision, run, counts, and unknown status without inventing comparison', async () => {
    service.listCampaigns.mockReturnValue(
      of({
        items: [
          campaign({
            id: 'campaign-1',
            name: 'Payments canary',
            status: '',
            revisionId: 'revision-1',
            runId: 'run-1',
          }),
        ],
        total: 1,
      })
    );

    await createComponent();

    for (const value of [
      'Payments canary',
      'Draft draft-1',
      'Revision revision-1',
      'Run run-1',
      'Unknown',
      '2 waves',
      '3 members',
    ]) {
      expect(text()).toContain(value);
    }
    expect(text()).not.toContain('Compare');
  });

  it('distinguishes empty, forbidden, and failed list states', async () => {
    service.listCampaigns.mockReturnValue(of({items: []}));
    await createComponent();
    expect(text()).toContain('No campaigns match these filters');

    fixture.destroy();
    service.listCampaigns.mockReturnValue(
      throwError(() => new HttpErrorResponse({status: 403, statusText: 'Forbidden'}))
    );
    await createComponent();
    expect(text()).toContain('You do not have access to deployment campaigns');

    fixture.destroy();
    service.listCampaigns.mockReturnValue(
      throwError(() => new HttpErrorResponse({status: 404, statusText: 'Not Found'}))
    );
    await createComponent();
    expect(text()).toContain('Campaign control plane is not available');

    fixture.destroy();
    service.listCampaigns.mockReturnValue(
      throwError(() => new HttpErrorResponse({status: 500, statusText: 'Server Error'}))
    );
    await createComponent();
    expect(text()).toContain('Campaigns could not be loaded');
  });

  it('loads the next cursor and preserves a partial list when load more fails', async () => {
    service.listCampaigns
      .mockReturnValueOnce(of({items: [campaign()], nextCursor: 'cursor-2', total: 2}))
      .mockReturnValueOnce(throwError(() => new HttpErrorResponse({status: 503, statusText: 'Unavailable'})));
    await createComponent();

    button('Load more').click();
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();

    expect(service.listCampaigns.mock.calls).toEqual([[{limit: 25}], [{limit: 25, cursor: 'cursor-2'}]]);
    expect(text()).toContain('Payments canary');
    expect(text()).toContain('Some campaigns could not be loaded');
    expect(text()).toContain('Retry load more');
  });

  it('creates and updates a campaign draft while invalidating downstream validation state', async () => {
    const created = campaignDraft();
    service.createCampaignDraft.mockReturnValue(of(created));
    service.updateCampaignDraft.mockReturnValue(of({...created, revision: 2, description: 'Updated rollout'}));
    service.validateCampaignDraft.mockReturnValue(of({valid: true, issues: []}));
    await createComponent();

    const component = fixture.componentInstance as any;
    enterStructuredDraft(component, campaignDraftRequest());
    await component.createDraft();
    await component.validateDraft();
    fixture.detectChanges();

    expect(text()).toContain('Draft draft-1 revision 1');
    expect(text()).toContain('Campaign draft is valid and ready to publish');

    const updatedRequest = {...campaignDraftRequest(), description: 'Updated rollout'};
    component.campaignDescription.setValue(updatedRequest.description);
    fixture.detectChanges();
    expect(button('Publish immutable revision').disabled).toBe(true);
    expect(text()).toContain('Authoring fields changed after validation');

    await component.updateDraft();
    fixture.detectChanges();

    expect(service.createCampaignDraft).toHaveBeenCalledWith(campaignDraftRequest());
    expect(service.updateCampaignDraft).toHaveBeenCalledWith('draft-1', {
      ...updatedRequest,
      expectedRevision: 1,
    });
    expect(text()).toContain('Draft draft-1 revision 2');
    expect(text()).not.toContain('Campaign draft is valid and ready to publish');
  });

  it('renders validation issues and keeps publication unavailable', async () => {
    service.createCampaignDraft.mockReturnValue(of(campaignDraft()));
    service.validateCampaignDraft.mockReturnValue(
      of({
        valid: false,
        issues: [{code: 'PLAN_NOT_READY', field: 'membership.planIds[0]', message: 'Plan must be READY.'}],
      })
    );
    await createComponent();

    const component = fixture.componentInstance as any;
    enterStructuredDraft(component, campaignDraftRequest());
    await component.createDraft();
    await component.validateDraft();
    fixture.detectChanges();

    expect(text()).toContain('Campaign draft validation failed');
    expect(text()).toContain('membership.planIds[0]');
    expect(text()).toContain('Plan must be READY.');
    expect(button('Publish immutable revision').disabled).toBe(true);
  });

  it('requires unsaved authoring changes to be updated before validating the stored draft', async () => {
    service.createCampaignDraft.mockReturnValue(of(campaignDraft()));
    service.validateCampaignDraft.mockReturnValue(of({valid: true, issues: []}));
    await createComponent();

    const component = fixture.componentInstance as any;
    enterStructuredDraft(component, campaignDraftRequest());
    await component.createDraft();
    component.campaignDescription.setValue('Unsaved local change');
    await component.validateDraft();
    fixture.detectChanges();

    expect(service.validateCampaignDraft).not.toHaveBeenCalled();
    expect(text()).toContain('Update the campaign draft before validating');
    expect(button('Validate draft').disabled).toBe(true);
    expect(button('Publish immutable revision').disabled).toBe(true);
  });

  it('rejects a validation response when authoring changes while validation is pending', async () => {
    const validation = new Subject<{valid: boolean; issues: []}>();
    service.createCampaignDraft.mockReturnValue(of(campaignDraft()));
    service.validateCampaignDraft.mockReturnValue(validation);
    await createComponent();

    const component = fixture.componentInstance as any;
    enterStructuredDraft(component, campaignDraftRequest());
    await component.createDraft();
    const validationPromise = component.validateDraft();
    component.campaignDescription.setValue('Changed during validation');
    validation.next({valid: true, issues: []});
    validation.complete();
    await validationPromise;
    fixture.detectChanges();

    expect(text()).toContain('Authoring fields changed while validation was running');
    expect(text()).not.toContain('Campaign draft is valid and ready to publish');
    expect(button('Publish immutable revision').disabled).toBe(true);
  });

  it('refreshes and repopulates an authoritative draft after a stale update conflict', async () => {
    service.createCampaignDraft.mockReturnValue(of(campaignDraft()));
    service.updateCampaignDraft.mockReturnValue(
      throwError(() => ({
        status: 409,
        code: 'CONFLICT',
        message: 'The record changed since it was loaded.',
        retryable: false,
      }))
    );
    const authoritative = {
      ...campaignDraft(),
      revision: 2,
      name: 'Authoritative progressive rollout',
      description: 'Updated by another operator',
    };
    service.getCampaignDraft.mockReturnValue(of(authoritative));
    await createComponent();

    const component = fixture.componentInstance as any;
    enterStructuredDraft(component, campaignDraftRequest());
    await component.createDraft();
    component.campaignDescription.setValue('My stale edit');
    await component.updateDraft();
    fixture.detectChanges();

    expect(service.getCampaignDraft).toHaveBeenCalledWith('draft-1');
    expect(component.campaignName.value).toBe('Authoritative progressive rollout');
    expect(component.campaignDescription.value).toBe('Updated by another operator');
    expect(text()).toContain('Draft draft-1 revision 2');
    expect(text()).toContain('Campaign draft changed while you were editing it');
    expect(text()).not.toContain('Campaign draft is valid and ready to publish');
  });

  it('publishes, creates a DRAFT run, and advances only through the legal pre-run states', async () => {
    const navigate = vi.spyOn(router, 'navigate').mockResolvedValue(true);
    service.createCampaignDraft.mockReturnValue(of(campaignDraft()));
    service.validateCampaignDraft.mockReturnValue(of({valid: true, issues: []}));
    service.publishCampaignDraft.mockReturnValue(of(campaignRevision()));
    service.startCampaignRun.mockReturnValue(of(campaignRun('DRAFT', 1)));
    service.transitionCampaignRun
      .mockReturnValueOnce(of(campaignRun('VALIDATED', 2)))
      .mockReturnValueOnce(of(campaignRun('AWAITING_APPROVAL', 3)))
      .mockReturnValueOnce(of(campaignRun('SCHEDULED', 4)))
      .mockReturnValueOnce(of(campaignRun('RUNNING', 5)));
    await createComponent();

    const component = fixture.componentInstance as any;
    enterStructuredDraft(component, campaignDraftRequest());
    await component.createDraft();
    await component.validateDraft();
    await component.publishDraft();
    await component.createRun();
    fixture.detectChanges();

    expect(text()).toContain('Run run-1 is DRAFT at version 1');
    expect(text()).not.toContain('Campaign rollout is running');
    expect(overlay.confirm.mock.calls.map(([request]) => request.requiredConfirmInputText)).toEqual([
      'draft-1',
      campaignRevision().canonicalChecksum,
    ]);

    component.transitionReason.setValue('Reviewed and approved for the next lifecycle state');
    for (const expected of [
      ['run-1', {expectedVersion: 1, to: 'VALIDATED', reason: 'Reviewed and approved for the next lifecycle state'}],
      [
        'run-1',
        {
          expectedVersion: 2,
          to: 'AWAITING_APPROVAL',
          reason: 'Reviewed and approved for the next lifecycle state',
        },
      ],
      ['run-1', {expectedVersion: 3, to: 'SCHEDULED', reason: 'Reviewed and approved for the next lifecycle state'}],
      ['run-1', {expectedVersion: 4, to: 'RUNNING', reason: 'Reviewed and approved for the next lifecycle state'}],
    ]) {
      await component.advanceRun();
      expect(service.transitionCampaignRun.mock.calls.at(-1)).toEqual(Array.from(expected));
    }
    fixture.detectChanges();

    expect(text()).toContain('Campaign rollout is running');
    expect(navigate).toHaveBeenCalledWith(['/deployments/campaigns', 'draft-1']);
  });

  it('does not publish when typed confirmation is cancelled', async () => {
    service.createCampaignDraft.mockReturnValue(of(campaignDraft()));
    service.validateCampaignDraft.mockReturnValue(of({valid: true, issues: []}));
    overlay.confirm.mockReturnValue(of(false));
    await createComponent();

    const component = fixture.componentInstance as any;
    enterStructuredDraft(component, campaignDraftRequest());
    await component.createDraft();
    await component.validateDraft();
    await component.publishDraft();

    expect(service.publishCampaignDraft).not.toHaveBeenCalled();
  });

  it('forces revalidation when the authoritative draft changes before publication confirmation', async () => {
    service.createCampaignDraft.mockReturnValue(of(campaignDraft()));
    service.validateCampaignDraft.mockReturnValue(of({valid: true, issues: []}));
    service.getCampaignDraft.mockReturnValue(
      of({
        ...campaignDraft(),
        revision: 2,
        description: 'Concurrent authoritative change',
      })
    );
    await createComponent();

    const component = fixture.componentInstance as any;
    enterStructuredDraft(component, campaignDraftRequest());
    await component.createDraft();
    await component.validateDraft();
    await component.publishDraft();
    fixture.detectChanges();

    expect(overlay.confirm).not.toHaveBeenCalled();
    expect(service.publishCampaignDraft).not.toHaveBeenCalled();
    expect(component.campaignDescription.value).toBe('Concurrent authoritative change');
    expect(text()).toContain('changed before publication');
    expect(button('Publish immutable revision').disabled).toBe(true);
  });

  it('reuses the same publication intent after a lost response', async () => {
    let subscriptions = 0;
    service.createCampaignDraft.mockReturnValue(of(campaignDraft()));
    service.validateCampaignDraft.mockReturnValue(of({valid: true, issues: []}));
    service.publishCampaignDraft.mockReturnValue(
      defer(() => {
        subscriptions++;
        return subscriptions === 1
          ? throwError(() => ({
              status: 503,
              code: 'SERVER_ERROR',
              message: 'The control plane could not complete the request.',
              retryable: true,
            }))
          : of(campaignRevision());
      })
    );
    await createComponent();

    const component = fixture.componentInstance as any;
    enterStructuredDraft(component, campaignDraftRequest());
    await component.createDraft();
    await component.validateDraft();
    await component.publishDraft();
    await component.publishDraft();
    fixture.detectChanges();

    expect(service.publishCampaignDraft).toHaveBeenCalledTimes(1);
    expect(subscriptions).toBe(2);
    expect(text()).toContain('Immutable campaign revision revision-1 published');
  });

  it('refreshes a stale run after a transition conflict and shows the authoritative version', async () => {
    service.createCampaignDraft.mockReturnValue(of(campaignDraft()));
    service.validateCampaignDraft.mockReturnValue(of({valid: true, issues: []}));
    service.publishCampaignDraft.mockReturnValue(of(campaignRevision()));
    service.startCampaignRun.mockReturnValue(of(campaignRun('DRAFT', 1)));
    service.transitionCampaignRun.mockReturnValue(
      throwError(() => ({
        status: 409,
        code: 'CONFLICT',
        message: 'The record changed since it was loaded.',
        retryable: false,
      }))
    );
    service.getCampaignRun.mockReturnValue(of(campaignRun('VALIDATED', 2)));
    await createComponent();

    const component = fixture.componentInstance as any;
    enterStructuredDraft(component, campaignDraftRequest());
    await component.createDraft();
    await component.validateDraft();
    await component.publishDraft();
    await component.createRun();
    component.transitionReason.setValue('Move after review');
    await component.advanceRun();
    fixture.detectChanges();

    expect(service.getCampaignRun).toHaveBeenCalledWith('run-1');
    expect(text()).toContain('Campaign run changed while you were reviewing it');
    expect(text()).toContain('Run run-1 is VALIDATED at version 2');
  });

  it('uses guided fields and exposes generated JSON only as a read-only preview', async () => {
    await createComponent();
    const component = fixture.componentInstance as any;

    fixture.detectChanges();
    expect(button('Create draft').disabled).toBe(true);
    expect(fixture.nativeElement.querySelector('textarea[data-testid="campaign-request-preview"]')).toBeNull();

    enterStructuredDraft(component, campaignDraftRequest());
    fixture.detectChanges();

    const preview = fixture.nativeElement.querySelector('[data-testid="campaign-request-preview"]') as HTMLElement;
    expect(preview.textContent).toContain('"name": "Sample progressive rollout"');
    expect(preview.textContent).toContain('"name": "Fleet"');
    expect(preview.textContent).toContain(`"providerPlacementId": "${providerPlacementId}"`);
    expect(button('Create draft').disabled).toBe(false);

    component.waves()[1].order.setValue(1);
    expect(component.authoringValid()).toBe(false);

    component.waves()[1].order.setValue(2);
    component.waves()[1].bakeSeconds.setValue(30);
    expect(component.authoringValid()).toBe(false);

    component.waves()[1].bakeSeconds.setValue(300);
    component.riskMaximumConcurrency.setValue(1001);
    expect(component.authoringValid()).toBe(false);

    component.riskMaximumConcurrency.setValue(1);
    expect(component.authoringValid()).toBe(true);

    await component.createDraft();
    expect(service.createCampaignDraft).toHaveBeenCalledWith(campaignDraftRequest());
  });

  it('enforces the backend collection limits in guided authoring', async () => {
    await createComponent();
    const component = fixture.componentInstance as any;
    enterStructuredDraft(component, campaignDraftRequest());

    component.membershipPlanIds.setValue(Array.from({length: 1001}, (_, index) => `plan-${index}`).join('\n'));
    expect(component.authoringValid()).toBe(false);
    component.membershipPlanIds.setValue(campaignDraftRequest().membership.planIds.join('\n'));

    const wave = component.waves()[0];
    wave.planIds.setValue(Array.from({length: 1001}, (_, index) => `plan-${index}`).join('\n'));
    expect(component.authoringValid()).toBe(false);
    wave.planIds.setValue(firstPlanId);

    const validWave = {
      order: {value: 1},
      name: {value: 'Wave'},
      planIds: {value: firstPlanId},
      bakeSeconds: {value: 60},
      maximumConcurrency: {value: 1},
    };
    component.waves.set(Array.from({length: 101}, (_, index) => ({...validWave, order: {value: index + 1}})));
    expect(component.authoringValid()).toBe(false);
    component.waves.set([validWave]);

    const validPrerequisite = {
      downstreamPlanId: {value: secondPlanId},
      upstreamPlanId: {value: firstPlanId},
      upstreamStepKey: {value: 'health-check'},
      providerPlacementId: {value: providerPlacementId},
      expectedRuntimeStateChecksum: {value: `sha256:${'c'.repeat(64)}`},
    };
    component.prerequisites.set(Array.from({length: 5001}, () => validPrerequisite));
    expect(component.authoringValid()).toBe(false);
  });

  async function createComponent(): Promise<void> {
    fixture = TestBed.createComponent(CampaignsComponent);
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();
  }

  function text(): string {
    return fixture.nativeElement.textContent as string;
  }

  function button(label: string): HTMLButtonElement {
    const buttons = Array.from(fixture.nativeElement.querySelectorAll('button')) as HTMLButtonElement[];
    const result = buttons.find((candidate) => candidate.textContent?.trim() === label);
    if (!result) throw new Error(`Missing button: ${label}`);
    return result;
  }
});

function campaign(overrides: Partial<OperatorCampaignRow> = {}): OperatorCampaignRow {
  return {
    id: 'campaign-1',
    createdAt: '2026-07-28T01:00:00Z',
    draftId: 'draft-1',
    name: 'Payments canary',
    status: 'RUNNING',
    canonicalChecksum: `sha256:${'a'.repeat(64)}`,
    waveCount: 2,
    memberCount: 3,
    pendingCount: 1,
    runningCount: 1,
    succeededCount: 1,
    failedCount: 0,
    blockedCount: 0,
    ...overrides,
  };
}

const firstPlanId = '11111111-1111-4111-8111-111111111111';
const secondPlanId = '22222222-2222-4222-8222-222222222222';
const providerPlacementId = '33333333-3333-4333-8333-333333333333';

function campaignDraftRequest() {
  return {
    name: 'Sample progressive rollout',
    description: 'Progressive rollout',
    membership: {planIds: [firstPlanId, secondPlanId], tagQuery: 'environment=sample'},
    waves: [
      {
        order: 1,
        name: 'Canary',
        planIds: [firstPlanId],
        bakeSeconds: 60,
        maximumConcurrency: 1,
      },
      {
        order: 2,
        name: 'Fleet',
        planIds: [secondPlanId],
        bakeSeconds: 300,
        maximumConcurrency: 5,
      },
    ],
    prerequisites: [
      {
        downstreamPlanId: secondPlanId,
        upstreamPlanId: firstPlanId,
        upstreamStepKey: 'health-check',
        providerPlacementId,
        expectedRuntimeStateChecksum: `sha256:${'c'.repeat(64)}`,
      },
    ],
    riskPolicy: {
      maximumConcurrency: 1,
      failureToleranceBasisPoints: 0,
      minimumHealthyBasisPoints: 10000,
    },
  };
}

function enterStructuredDraft(component: any, request: ReturnType<typeof campaignDraftRequest>) {
  component.campaignName.setValue(request.name);
  component.campaignDescription.setValue(request.description);
  component.membershipPlanIds.setValue(request.membership.planIds.join('\n'));
  component.membershipTagQuery.setValue(request.membership.tagQuery);
  component.riskMaximumConcurrency.setValue(request.riskPolicy.maximumConcurrency);
  component.failureToleranceBasisPoints.setValue(request.riskPolicy.failureToleranceBasisPoints);
  component.minimumHealthyBasisPoints.setValue(request.riskPolicy.minimumHealthyBasisPoints);

  while (component.waves().length < request.waves.length) component.addWave();
  request.waves.forEach((wave, index) => {
    const controls = component.waves()[index];
    controls.order.setValue(wave.order);
    controls.name.setValue(wave.name);
    controls.planIds.setValue(wave.planIds.join('\n'));
    controls.bakeSeconds.setValue(wave.bakeSeconds);
    controls.maximumConcurrency.setValue(wave.maximumConcurrency);
  });

  while (component.prerequisites().length < request.prerequisites.length) component.addPrerequisite();
  request.prerequisites.forEach((prerequisite, index) => {
    const controls = component.prerequisites()[index];
    controls.downstreamPlanId.setValue(prerequisite.downstreamPlanId);
    controls.upstreamPlanId.setValue(prerequisite.upstreamPlanId);
    controls.upstreamStepKey.setValue(prerequisite.upstreamStepKey);
    controls.providerPlacementId.setValue(prerequisite.providerPlacementId);
    controls.expectedRuntimeStateChecksum.setValue(prerequisite.expectedRuntimeStateChecksum);
  });
}

function campaignDraft(): OperatorCampaignDraft {
  return {
    id: 'draft-1',
    createdAt: '2026-07-29T01:00:00Z',
    updatedAt: '2026-07-29T01:00:00Z',
    organizationId: 'organization-1',
    revision: 1,
    ...campaignDraftRequest(),
  };
}

function campaignRevision(): OperatorCampaignRevision {
  return {
    id: 'revision-1',
    publishedAt: '2026-07-29T02:00:00Z',
    organizationId: 'organization-1',
    campaignDraftId: 'draft-1',
    revisionNumber: 1,
    sourceDraftRevision: 1,
    name: 'Sample progressive rollout',
    description: 'Progressive rollout',
    riskPolicy: campaignDraftRequest().riskPolicy,
    canonicalChecksum: `sha256:${'b'.repeat(64)}`,
    publishedByUserAccountId: 'user-1',
    waves: [{order: 1, name: 'Canary', bakeSeconds: 60, maximumConcurrency: 1}],
    members: [],
    prerequisites: [],
  };
}

function campaignRun(state: OperatorCampaignRunState, version: number): OperatorCampaignRun {
  return {
    id: 'run-1',
    createdAt: '2026-07-29T03:00:00Z',
    updatedAt: '2026-07-29T03:00:00Z',
    campaignRevisionId: 'revision-1',
    state,
    version,
    currentWaveOrder: 0,
    currentMemberOrder: 0,
    admissionsBlocked: false,
    pauseRequested: false,
    reconciliationRequired: false,
    fencingToken: 0,
  };
}
