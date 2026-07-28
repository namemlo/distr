import {ComponentFixture, TestBed} from '@angular/core/testing';
import {provideRouter, Router} from '@angular/router';
import {of, Subject, throwError} from 'rxjs';
import {vi} from 'vitest';
import {OperatorControlPlaneService} from '../../services/operator-control-plane.service';
import {OperatorControlPlaneError, OperatorPage, OperatorPlanRow} from '../../types/operator-control-plane';
import {PlanListComponent} from './plan-list.component';

describe('PlanListComponent', () => {
  let service: {listPlans: ReturnType<typeof vi.fn>};
  let router: Router;

  const readyPlan: OperatorPlanRow = {
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
    canonicalChecksum: 'sha256:plan-1',
    targetCount: 2,
    stepCount: 4,
    issueCount: 1,
    blockingIssueCount: 0,
    approvalBlockerCount: 0,
    preflightBlockerCount: 0,
    bootstrap: false,
  };

  beforeEach(() => {
    service = {listPlans: vi.fn().mockReturnValue(of({items: [readyPlan], nextCursor: 'cursor-2', total: 2}))};
    TestBed.configureTestingModule({
      imports: [PlanListComponent],
      providers: [provideRouter([]), {provide: OperatorControlPlaneService, useValue: service}],
    });
    router = TestBed.inject(Router);
  });

  it('renders immutable plan identity, status, blockers, and the next-page affordance', () => {
    const {fixture} = createComponent();
    const text = fixture.nativeElement.textContent;

    expect(text).toContain('Deployment plans');
    expect(text).toContain('2026.7.1');
    expect(text).toContain('Production');
    expect(text).toContain('Payments');
    expect(text).toContain('sha256:plan-1');
    expect(text).toContain('READY');
    expect(text).toContain('Load more');
    expect(fixture.nativeElement.querySelector('a[href="/deployments/plans/plan-1"]')).not.toBeNull();
  });

  it('keeps the loading state visible until the server page resolves', () => {
    const response = new Subject<OperatorPage<OperatorPlanRow>>();
    service.listPlans.mockReturnValue(response);

    const {fixture} = createComponent();
    expect(fixture.nativeElement.textContent).toContain('Loading deployment plans');

    response.next({items: []});
    response.complete();
    fixture.detectChanges();
    expect(fixture.nativeElement.textContent).toContain('No deployment plans found');
  });

  it('appends cursor pages without dropping the first immutable page', () => {
    const secondPlan = {...readyPlan, id: 'plan-2', canonicalChecksum: 'sha256:plan-2', status: 'BLOCKED'};
    service.listPlans
      .mockReturnValueOnce(of({items: [readyPlan], nextCursor: 'cursor-2'}))
      .mockReturnValueOnce(of({items: [secondPlan]}));
    const {fixture, component} = createComponent();

    (component as any).loadMore();
    fixture.detectChanges();

    expect(service.listPlans.mock.calls[1][0].cursor).toBe('cursor-2');
    expect(service.listPlans.mock.calls[1][0].limit).toBe(25);
    expect(fixture.nativeElement.textContent).toContain('sha256:plan-1');
    expect(fixture.nativeElement.textContent).toContain('sha256:plan-2');
    expect(fixture.nativeElement.textContent).not.toContain('Load more');
  });

  it('serializes filters into the server request and URL before replacing the page', () => {
    const {fixture, component} = createComponent();
    vi.spyOn(router, 'navigate').mockResolvedValue(true);
    (component as any).filters.patchValue({
      status: 'BLOCKED',
      environmentId: 'environment-2',
      deploymentUnitId: 'unit-2',
      productReleaseId: 'release-2',
    });

    (component as any).applyFilters();
    fixture.detectChanges();

    expect(service.listPlans.mock.calls.at(-1)?.[0]).toEqual({
      cursor: undefined,
      limit: 25,
      status: 'BLOCKED',
      environmentId: 'environment-2',
      deploymentUnitId: 'unit-2',
      productReleaseId: 'release-2',
    });
    expect(router.navigate).toHaveBeenCalledWith([], {
      queryParams: {
        status: 'BLOCKED',
        environmentId: 'environment-2',
        deploymentUnitId: 'unit-2',
        productReleaseId: 'release-2',
      },
      queryParamsHandling: 'merge',
      replaceUrl: true,
    });
  });

  it('distinguishes permission denial from retryable read errors', () => {
    const forbidden: OperatorControlPlaneError = {
      status: 403,
      code: 'FORBIDDEN',
      message: 'You do not have permission to perform this action.',
      retryable: false,
    };
    service.listPlans.mockReturnValue(throwError(() => forbidden));
    let result = createComponent();
    expect(result.fixture.nativeElement.textContent).toContain('Deployment plans are not available for your role');
    expect(result.fixture.nativeElement.textContent).not.toContain('Retry');

    TestBed.resetTestingModule();
    service = {
      listPlans: vi.fn().mockReturnValue(
        throwError(() => ({
          status: 503,
          code: 'SERVER_ERROR',
          message: 'The control plane could not complete the request. Try again.',
          retryable: true,
        }))
      ),
    };
    TestBed.configureTestingModule({
      imports: [PlanListComponent],
      providers: [provideRouter([]), {provide: OperatorControlPlaneService, useValue: service}],
    });
    result = createComponent();
    expect(result.fixture.nativeElement.textContent).toContain('The control plane could not complete the request');
    expect(result.fixture.nativeElement.textContent).toContain('Retry');
  });

  it('surfaces partial, stale, disabled, and unknown records instead of presenting them as healthy', () => {
    const rows = [
      {...readyPlan, id: 'partial', productReleaseVersion: '', canonicalChecksum: ''},
      {...readyPlan, id: 'stale', status: 'STALE'},
      {...readyPlan, id: 'disabled', status: 'DISABLED'},
      {...readyPlan, id: 'unknown', status: 'ALIEN_STATE'},
    ];
    service.listPlans.mockReturnValue(of({items: rows}));

    const {fixture} = createComponent();
    const text = fixture.nativeElement.textContent;

    expect(text).toContain('Some plan records are partial');
    expect(text).toContain('Stale plan');
    expect(text).toContain('Plan disabled');
    expect(text).toContain('Unknown status: ALIEN_STATE');
  });

  function createComponent(): {fixture: ComponentFixture<PlanListComponent>; component: PlanListComponent} {
    const fixture = TestBed.createComponent(PlanListComponent);
    fixture.detectChanges();
    return {fixture, component: fixture.componentInstance};
  }
});
