import {ComponentFixture, TestBed} from '@angular/core/testing';
import {ActivatedRoute} from '@angular/router';
import {BehaviorSubject, of, Subject, throwError} from 'rxjs';
import {vi} from 'vitest';
import {OperatorControlPlaneService} from '../../services/operator-control-plane.service';
import {OperatorExecutionRow, OperatorPage} from '../../types/operator-control-plane';
import {ExecutionsComponent} from './executions.component';

describe('ExecutionsComponent', () => {
  let service: {
    listExecutions: ReturnType<typeof vi.fn>;
  };
  let queryParams: BehaviorSubject<Record<string, string>>;

  const runningExecution: OperatorExecutionRow = {
    id: 'execution-1',
    createdAt: '2026-07-28T01:00:00Z',
    campaignId: 'campaign-1',
    deploymentPlanId: 'plan-1',
    deploymentTargetId: 'target-1',
    taskId: 'task-1',
    stepRunId: 'step-run-1',
    stepKey: 'deploy',
    attemptNumber: 2,
    protocolVersion: 'v2',
    status: 'RUNNING',
    planChecksum: 'sha256:plan',
    artifactDigest: 'image@sha256:artifact',
    configChecksum: 'sha256:config',
    adapterRevision: 'compose-v4',
    cancellable: true,
    reconciliation: 'PENDING',
    observation: 'STALE',
  };

  const unknownExecution: OperatorExecutionRow = {
    ...runningExecution,
    id: 'execution-2',
    taskId: 'task-2',
    stepRunId: 'step-run-2',
    attemptNumber: 1,
    status: 'WAITING_FOR_ORACLE',
    observation: 'UNKNOWN',
  };

  beforeEach(() => {
    service = {listExecutions: vi.fn().mockReturnValue(of({items: [runningExecution, unknownExecution]}))};
    queryParams = new BehaviorSubject<Record<string, string>>({});

    TestBed.configureTestingModule({
      imports: [ExecutionsComponent],
      providers: [
        {provide: OperatorControlPlaneService, useValue: service},
        {
          provide: ActivatedRoute,
          useValue: {
            snapshot: {queryParamMap: {get: () => null}},
            queryParams,
          },
        },
      ],
    });
  });

  it('renders task and attempt correlation while preserving stale and unknown API states', () => {
    const {fixture} = createComponent();
    const text = fixture.nativeElement.textContent;

    expect(text).toContain('task-1');
    expect(text).toContain('step-run-1');
    expect(text).toContain('Attempt 2');
    expect(text).toContain('Stale observation');
    expect(text).toContain('Unknown status: WAITING_FOR_ORACLE');
    expect(fixture.nativeElement.querySelector('a[href="/deployments/executions/execution-1"]')).not.toBeNull();
  });

  it('uses deploymentPlanId from the prior-state pivot and exposes the active scope', () => {
    queryParams.next({deploymentPlanId: 'plan-previous'});

    const {fixture} = createComponent();

    expect(service.listExecutions).toHaveBeenCalledWith({
      limit: 50,
      deploymentPlanId: 'plan-previous',
    });
    expect(fixture.nativeElement.textContent).toContain('Plan plan-previous');
  });

  it('keeps the loading state until the first page resolves', () => {
    const page = new Subject<OperatorPage<OperatorExecutionRow>>();
    service.listExecutions.mockReturnValue(page);

    const {fixture} = createComponent();
    expect(fixture.nativeElement.textContent).toContain('Loading executions');

    page.next({items: []});
    page.complete();
    fixture.detectChanges();
    expect(fixture.nativeElement.textContent).toContain('No executions found');
  });

  it('appends cursor pages without losing the active filters', () => {
    service.listExecutions
      .mockReturnValueOnce(of({items: [runningExecution], nextCursor: 'next-page'}))
      .mockReturnValueOnce(of({items: [unknownExecution]}));
    queryParams.next({deploymentPlanId: 'plan-1'});

    const {fixture, component} = createComponent();
    (component as any).loadNextPage();
    fixture.detectChanges();

    expect(service.listExecutions.mock.calls[1]).toEqual([
      {
        limit: 50,
        deploymentPlanId: 'plan-1',
        cursor: 'next-page',
      },
    ]);
    expect(fixture.nativeElement.textContent).toContain('task-1');
    expect(fixture.nativeElement.textContent).toContain('task-2');
    expect(fixture.nativeElement.querySelector('[data-testid="load-more-executions"]')).toBeNull();
  });

  it('preserves the partial list and cursor when loading the next page fails', () => {
    service.listExecutions
      .mockReturnValueOnce(of({items: [runningExecution], nextCursor: 'next-page'}))
      .mockReturnValueOnce(throwError(() => ({status: 503, code: 'SERVER_ERROR'})));

    const {fixture, component} = createComponent();
    (component as any).loadNextPage();
    fixture.detectChanges();

    expect(fixture.nativeElement.textContent).toContain('task-1');
    expect(fixture.nativeElement.textContent).toContain('Some executions could not be loaded');
    expect(fixture.nativeElement.textContent).toContain('Retry load more');
  });

  it('renders forbidden, not-found, and retryable error states without stale rows', () => {
    const cases = [
      [{status: 403, code: 'FORBIDDEN'}, 'You do not have permission to view executions'],
      [{status: 404, code: 'NOT_FOUND'}, 'Executions were not found'],
      [{status: 503, code: 'SERVER_ERROR', retryable: true}, 'Executions are temporarily unavailable'],
    ];

    for (const [error, expected] of cases) {
      service.listExecutions.mockReturnValue(throwError(() => error));
      const {fixture} = createComponent();
      expect(fixture.nativeElement.textContent).toContain(expected as string);
      expect(fixture.nativeElement.textContent).not.toContain('task-1');
      fixture.destroy();
    }
  });

  function createComponent(): {
    fixture: ComponentFixture<ExecutionsComponent>;
    component: ExecutionsComponent;
  } {
    const fixture = TestBed.createComponent(ExecutionsComponent);
    fixture.detectChanges();
    return {fixture, component: fixture.componentInstance};
  }
});
