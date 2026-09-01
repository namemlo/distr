import {HttpErrorResponse} from '@angular/common/http';
import {ComponentFixture, TestBed} from '@angular/core/testing';
import {ActivatedRoute, convertToParamMap, provideRouter} from '@angular/router';
import {Observable, of, Subject, throwError} from 'rxjs';
import {vi} from 'vitest';
import {OperatorControlPlaneService} from '../../services/operator-control-plane.service';
import {OverlayService} from '../../services/overlay.service';
import {
  OperatorCampaignControlResult,
  OperatorCampaignDetail,
  OperatorCampaignDetailResponse,
  OperatorCampaignRow,
  OperatorEvidencePage,
} from '../../types/operator-control-plane';
import {CampaignDetailComponent} from './campaign-detail.component';

describe('CampaignDetailComponent', () => {
  let fixture: ComponentFixture<CampaignDetailComponent>;
  let service: {
    getCampaign: ReturnType<typeof vi.fn>;
    getCampaignEvidence: ReturnType<typeof vi.fn>;
    controlCampaign: ReturnType<typeof vi.fn>;
    controlCampaignMember: ReturnType<typeof vi.fn>;
  };
  let overlay: {confirm: ReturnType<typeof vi.fn>};

  beforeEach(() => {
    service = {
      getCampaign: vi.fn().mockReturnValue(of({detail: detail()})),
      getCampaignEvidence: vi.fn().mockReturnValue(of({items: detail().evidence})),
      controlCampaign: vi.fn().mockReturnValue(of(controlResult())),
      controlCampaignMember: vi.fn().mockReturnValue(of({})),
    };
    overlay = {confirm: vi.fn().mockReturnValue(of(true))};
    TestBed.configureTestingModule({
      imports: [CampaignDetailComponent],
      providers: [
        provideRouter([]),
        {provide: ActivatedRoute, useValue: {snapshot: {paramMap: convertToParamMap({campaignId: 'campaign-1'})}}},
        {provide: OperatorControlPlaneService, useValue: service},
        {provide: OverlayService, useValue: overlay},
      ],
    });
  });

  it('shows loading until campaign detail arrives', () => {
    service.getCampaign.mockReturnValue(new Subject<OperatorCampaignDetailResponse>());
    service.getCampaignEvidence.mockReturnValue(new Subject<OperatorEvidencePage>());

    fixture = TestBed.createComponent(CampaignDetailComponent);
    fixture.detectChanges();

    expect(text()).toContain('Loading campaign');
  });

  it('presents draft, revision, checksums, waves, members, blockers, controls, and evidence', async () => {
    await createComponent();

    for (const value of [
      'Deployment campaign',
      'Payments canary',
      'Draft draft-1',
      'Revision revision-1',
      'revision-checksum',
      'membership-checksum',
      'Canary',
      'Maximum concurrency 1',
      'unit-1',
      'plan-checksum',
      'database-ready',
      'Pause requested',
      'Evidence',
      'Deployment plan',
      'evidence-checksum',
    ]) {
      expect(text()).toContain(value);
    }
    expect(text()).not.toContain('Compare campaign');
  });

  it('renders unknown and partial evidence states explicitly', async () => {
    service.getCampaign.mockReturnValue(of({detail: detail({campaign: campaign({status: ''})})}));
    service.getCampaignEvidence.mockReturnValue(
      throwError(() => new HttpErrorResponse({status: 503, statusText: 'Unavailable'}))
    );

    await createComponent();

    expect(text()).toContain('Unknown');
    expect(text()).toContain('Campaign details are partial');
    expect(text()).toContain('Evidence could not be loaded');
    (fixture.componentInstance as any).reason.set('maintenance');
    (fixture.componentInstance as any).expectedVersion.set(4);
    fixture.detectChanges();
    expect(button('Cancel campaign').disabled).toBe(true);
    expect(button('Exclude member').disabled).toBe(true);
  });

  it('renders pause-pending, no-new-exposure, in-flight, scheduler lease, and lock closure state', async () => {
    await createComponent();

    for (const value of [
      'Runtime coordination',
      'Blocked · No new exposure',
      'Pause pending at safe point',
      'In-flight members',
      'Generation 12',
      'ACTIVE',
      '2 active · 3 unreleased',
      '1 active · 2 unreleased',
      'Coordination state open',
    ]) {
      expect(text()).toContain(value);
    }

    service.getCampaign.mockReturnValue(
      of({
        detail: detail({
          campaign: campaign({status: 'COMPLETED'}),
          coordination: {
            ...detail().coordination,
            admissionsBlocked: false,
            pausePending: false,
            noNewExposure: false,
            inFlightMemberCount: 0,
            schedulerLeaseStatus: 'EXPIRED',
            activeLockCount: 0,
            unreleasedLockCount: 0,
            activeLeaseCount: 0,
            unreleasedLeaseCount: 0,
            zeroLockClosure: true,
          },
        }),
      })
    );
    fixture.destroy();
    await createComponent();
    expect(text()).toContain('Zero-lock closure proven');
    expect(text()).toContain('No pause pending');
  });

  it('distinguishes forbidden and not-found detail states', async () => {
    service.getCampaign.mockReturnValue(
      throwError(() => new HttpErrorResponse({status: 403, statusText: 'Forbidden'}))
    );
    await createComponent();
    expect(text()).toContain('You do not have access to this campaign');

    fixture.destroy();
    service.getCampaign.mockReturnValue(
      throwError(() => new HttpErrorResponse({status: 404, statusText: 'Not Found'}))
    );
    await createComponent();
    expect(text()).toContain('Campaign not found');
  });

  it('uses the read-model runVersion and requires a reason before enabling run controls', async () => {
    await createComponent();

    expect((fixture.componentInstance as any).expectedVersion()).toBe(4);
    expect(button('Pause campaign').disabled).toBe(true);
    (fixture.componentInstance as any).reason.set('maintenance');
    fixture.detectChanges();

    expect(button('Pause campaign').disabled).toBe(false);
  });

  it('uses proportional confirmation and submits expectedVersion only after confirmation', async () => {
    await createComponent();
    (fixture.componentInstance as any).reason.set('maintenance');

    await (fixture.componentInstance as any).requestControl('cancel');

    const confirmation = overlay.confirm.mock.calls[0][0] as {
      confirmLabel: string;
      requiredConfirmInputText: string;
      message: {alert: {type: string}};
    };
    expect(confirmation.confirmLabel).toBe('Cancel campaign');
    expect(confirmation.requiredConfirmInputText).toBe('Payments canary');
    expect(confirmation.message.alert.type).toBe('danger');
    expect(service.controlCampaign).toHaveBeenCalledWith('run-1', 'cancel', {
      expectedVersion: 4,
      reason: 'maintenance',
    });
  });

  it('does not create an action request when confirmation is declined', async () => {
    overlay.confirm.mockReturnValue(of(false));
    await createComponent();
    (fixture.componentInstance as any).reason.set('maintenance');

    await (fixture.componentInstance as any).requestControl('pause');

    expect(service.controlCampaign).not.toHaveBeenCalled();
  });

  it('keeps one confirmed intent when a retryable request is retried', async () => {
    let subscriptions = 0;
    const intent = new Observable<OperatorCampaignControlResult>((subscriber) => {
      subscriptions += 1;
      if (subscriptions === 1) {
        subscriber.error({status: 503, retryable: true});
      } else {
        subscriber.next(controlResult());
        subscriber.complete();
      }
    });
    service.controlCampaign.mockReturnValue(intent);
    await createComponent();
    (fixture.componentInstance as any).reason.set('maintenance');

    await (fixture.componentInstance as any).requestControl('pause');
    fixture.detectChanges();

    expect(text()).toContain('Retry same request');

    service.controlCampaign.mockClear();
    await (fixture.componentInstance as any).retryControl();
    expect(service.controlCampaign).not.toHaveBeenCalled();
    expect(subscriptions).toBe(2);
  });

  it('surfaces stale expectedVersion conflicts without retrying the stale intent', async () => {
    service.controlCampaign.mockReturnValue(throwError(() => ({status: 409, retryable: false})));
    await createComponent();
    (fixture.componentInstance as any).reason.set('maintenance');

    await (fixture.componentInstance as any).requestControl('pause');
    fixture.detectChanges();

    expect(text()).toContain('Campaign state is stale');
    expect(text()).not.toContain('Retry same request');
  });

  it('excludes a runtime member with danger confirmation and the read-model run version', async () => {
    await createComponent();
    (fixture.componentInstance as any).reason.set('maintenance');
    fixture.detectChanges();

    await (fixture.componentInstance as any).requestMemberControl(detail().members[0], 'exclude');

    const confirmation = overlay.confirm.mock.calls[0][0] as {
      confirmLabel: string;
      requiredConfirmInputText: string;
      message: {alert: {type: string}};
    };
    expect(confirmation.confirmLabel).toBe('Exclude member');
    expect(confirmation.requiredConfirmInputText).toBe('unit-1');
    expect(confirmation.message.alert.type).toBe('danger');
    expect(service.controlCampaignMember).toHaveBeenCalledWith('run-1', 'exclude', {
      expectedVersion: 4,
      reason: 'maintenance',
      memberRunId: 'member-run-1',
    });
  });

  it('requires an explicit protocol before retrying a runtime member', async () => {
    await createComponent();
    (fixture.componentInstance as any).reason.set('maintenance');
    fixture.detectChanges();
    expect(button('Retry member').disabled).toBe(true);

    (fixture.componentInstance as any).protocolVersion.set('v2');
    fixture.detectChanges();
    expect(button('Retry member').disabled).toBe(false);

    await (fixture.componentInstance as any).requestMemberControl(detail().members[0], 'retry');

    const confirmation = overlay.confirm.mock.calls[0][0] as {
      confirmLabel: string;
      message: {alert: {type: string}};
    };
    expect(confirmation.confirmLabel).toBe('Retry member');
    expect(confirmation.message.alert.type).toBe('warning');
    expect(service.controlCampaignMember).toHaveBeenCalledWith('run-1', 'retry', {
      expectedVersion: 4,
      reason: 'maintenance',
      memberRunId: 'member-run-1',
      protocolVersion: 'v2',
    });
  });

  it('disables campaign and member actions when required runtime identifiers are unavailable', async () => {
    service.getCampaign.mockReturnValue(
      of({
        detail: detail({
          campaign: campaign({runId: undefined}),
          runVersion: undefined,
          members: [{...detail().members[0], memberRunId: undefined}],
        }),
      })
    );
    await createComponent();

    expect(button('Pause campaign').disabled).toBe(true);
    expect(button('Exclude member').disabled).toBe(true);
    expect(button('Retry member').disabled).toBe(true);
    expect(text()).toContain('Runtime control metadata is unavailable');
  });

  async function createComponent(): Promise<void> {
    fixture = TestBed.createComponent(CampaignDetailComponent);
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
    revisionId: 'revision-1',
    runId: 'run-1',
    name: 'Payments canary',
    status: 'RUNNING',
    canonicalChecksum: 'canonical-checksum',
    waveCount: 1,
    memberCount: 1,
    pendingCount: 0,
    runningCount: 1,
    succeededCount: 0,
    failedCount: 0,
    blockedCount: 1,
    ...overrides,
  };
}

function detail(overrides: Partial<OperatorCampaignDetail> = {}): OperatorCampaignDetail {
  return {
    campaign: campaign(),
    runVersion: 4,
    revisionChecksum: 'revision-checksum',
    membershipChecksum: 'membership-checksum',
    prerequisiteChecksum: 'prerequisite-checksum',
    thresholdChecksum: 'threshold-checksum',
    controlChecksum: 'control-checksum',
    admissionChecksum: 'admission-checksum',
    waves: [
      {
        id: 'wave-1',
        order: 1,
        name: 'Canary',
        status: 'RUNNING',
        bakeSeconds: 60,
        maximumConcurrency: 1,
        memberCount: 1,
        succeededCount: 0,
        failedCount: 0,
      },
    ],
    members: [
      {
        id: 'member-1',
        memberRunId: 'member-run-1',
        deploymentPlanId: 'plan-1',
        deploymentUnitId: 'unit-1',
        waveOrder: 1,
        memberOrder: 1,
        status: 'FAILED',
        planChecksum: 'plan-checksum',
      },
    ],
    prerequisites: [
      {
        key: 'database-ready',
        status: 'BLOCKED',
        message: 'Database is not ready',
        blocking: true,
        order: 1,
      },
    ],
    thresholds: [],
    controls: [
      {
        key: 'PAUSE',
        status: 'PENDING_SAFE_POINT',
        message: 'Pause requested',
        blocking: true,
        order: 1,
      },
    ],
    uncertaintyBlockers: [],
    admissionBlockers: [],
    coordination: {
      admissionsBlocked: true,
      pausePending: true,
      noNewExposure: true,
      inFlightMemberCount: 1,
      reconciliationRequired: false,
      schedulerFenceGeneration: 12,
      schedulerLeaseStatus: 'ACTIVE',
      schedulerLeaseExpiresAt: '2026-07-28T01:05:00Z',
      activeLockCount: 2,
      unreleasedLockCount: 3,
      activeLeaseCount: 1,
      unreleasedLeaseCount: 2,
      zeroLockClosure: false,
    },
    evidence: [
      {
        id: 'evidence-1',
        kind: 'deployment_plan',
        label: 'Deployment plan',
        href: '/api/v1/control-plane/plans/plan-1',
        checksum: 'evidence-checksum',
        createdAt: '2026-07-28T01:00:00Z',
      },
    ],
    ...overrides,
  };
}

function controlResult(): OperatorCampaignControlResult {
  return {
    requestId: 'request-1',
    status: 'APPLIED',
    run: {
      id: 'run-1',
      createdAt: '2026-07-28T01:00:00Z',
      updatedAt: '2026-07-28T01:01:00Z',
      campaignRevisionId: 'revision-1',
      state: 'PAUSED',
      version: 5,
      currentWaveOrder: 1,
      currentMemberOrder: 1,
      admissionsBlocked: false,
      pauseRequested: false,
      reconciliationRequired: false,
      fencingToken: 1,
    },
    pausePending: false,
    reconciliationRequired: false,
    duplicate: false,
  };
}
