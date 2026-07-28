import {ComponentFixture, TestBed} from '@angular/core/testing';
import {ActivatedRoute} from '@angular/router';
import {of, Subject, throwError} from 'rxjs';
import {vi} from 'vitest';
import {OperatorControlPlaneService} from '../../services/operator-control-plane.service';
import {
  OperatorEvidencePage,
  OperatorExecutionDetail,
  OperatorExecutionDetailResponse,
} from '../../types/operator-control-plane';
import {ExecutionDetailComponent} from './execution-detail.component';

describe('ExecutionDetailComponent', () => {
  let service: {
    getExecution: ReturnType<typeof vi.fn>;
    getExecutionEvidence: ReturnType<typeof vi.fn>;
    cancelExecution: ReturnType<typeof vi.fn>;
    requestExecutionStatus: ReturnType<typeof vi.fn>;
  };

  const detail: OperatorExecutionDetail = {
    execution: {
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
    },
    intent: {key: 'intent', status: 'VERIFIED', checksum: 'sha256:intent', blocking: false, order: 1},
    tasks: [{id: 'task-1', key: 'task-1', status: 'RUNNING', blocking: false, order: 1}],
    steps: [{id: 'step-run-1', key: 'deploy', status: 'RUNNING', blocking: false, order: 1}],
    attempts: [{id: 'attempt-2', key: 'attempt-2', status: 'RUNNING', message: 'leased', blocking: false, order: 2}],
    observations: [
      {
        id: 'observation-1',
        key: 'callback',
        status: 'WAITING_FOR_ORACLE',
        message: 'No current callback',
        blocking: false,
        order: 1,
      },
    ],
    evidence: [],
  };

  const firstEvidence: OperatorEvidencePage = {
    items: [
      {
        id: 'evidence-1',
        kind: 'EXECUTION_EVENT',
        label: 'Attempt 2 started',
        href: '/api/v1/evidence/evidence-1',
        checksum: 'sha256:evidence-1',
        createdAt: '2026-07-28T01:01:00Z',
      },
    ],
    nextCursor: 'evidence-next',
  };

  beforeEach(() => {
    service = {
      getExecution: vi.fn().mockReturnValue(of({detail} satisfies OperatorExecutionDetailResponse)),
      getExecutionEvidence: vi.fn().mockReturnValue(of(firstEvidence)),
      cancelExecution: vi.fn().mockReturnValue(of(null)),
      requestExecutionStatus: vi.fn().mockReturnValue(of({})),
    };

    TestBed.configureTestingModule({
      imports: [ExecutionDetailComponent],
      providers: [
        {provide: OperatorControlPlaneService, useValue: service},
        {
          provide: ActivatedRoute,
          useValue: {snapshot: {paramMap: {get: (name: string) => (name === 'executionId' ? 'execution-1' : null)}}},
        },
      ],
    });
  });

  it('renders execution, task, step, attempt, observation, and evidence correlation', () => {
    const {fixture} = createComponent();
    const text = fixture.nativeElement.textContent;

    expect(text).toContain('task-1');
    expect(text).toContain('step-run-1');
    expect(text).toContain('attempt-2');
    expect(text).toContain('Attempt 2 started');
    expect(text).toContain('Stale observation');
    expect(text).toContain('Unknown status: WAITING_FOR_ORACLE');
    expect(
      fixture.nativeElement.querySelector('a[href="/deployments/executions?deploymentPlanId=plan-1"]')
    ).not.toBeNull();
  });

  it('renders the complete evidence page without inventing evidence pagination', () => {
    const {fixture} = createComponent();

    expect(service.getExecutionEvidence).toHaveBeenCalledWith('execution-1');
    expect(fixture.nativeElement.textContent).toContain('Attempt 2 started');
    expect(fixture.nativeElement.textContent).not.toContain('Load more evidence');
  });

  it('keeps the detail usable and marks evidence partial when evidence fails', () => {
    service.getExecutionEvidence.mockReturnValue(throwError(() => ({status: 503, code: 'SERVER_ERROR'})));

    const {fixture} = createComponent();

    expect(fixture.nativeElement.textContent).toContain('task-1');
    expect(fixture.nativeElement.textContent).toContain('Some execution evidence is unavailable');
  });

  it('renders loading, forbidden, not-found, and generic detail states', () => {
    const pending = new Subject<OperatorExecutionDetailResponse>();
    service.getExecution.mockReturnValue(pending);
    let created = createComponent();
    expect(created.fixture.nativeElement.textContent).toContain('Loading execution');
    created.fixture.destroy();

    const cases = [
      [{status: 403, code: 'FORBIDDEN'}, 'You do not have permission to view this execution'],
      [{status: 404, code: 'NOT_FOUND'}, 'Execution not found'],
      [{status: 500, code: 'SERVER_ERROR'}, 'Unable to load execution'],
    ];
    for (const [error, expected] of cases) {
      service.getExecution.mockReturnValue(throwError(() => error));
      created = createComponent();
      expect(created.fixture.nativeElement.textContent).toContain(expected as string);
      created.fixture.destroy();
    }
  });

  it('requires an exact-id destructive confirmation and retains one cancel key across retry', () => {
    service.cancelExecution
      .mockReturnValueOnce(throwError(() => ({status: 503, code: 'SERVER_ERROR'})))
      .mockReturnValueOnce(of(null));
    const {fixture, component} = createComponent();
    const controls = (component as any).cancelForm.controls;

    controls.reason.setValue('Operator confirmed duplicate deployment');
    controls.confirmation.setValue('wrong-id');
    expect((component as any).canSubmitCancel()).toBe(false);

    controls.confirmation.setValue('execution-1');
    expect((component as any).canSubmitCancel()).toBe(true);
    (component as any).submitCancel();
    const firstRequest = service.cancelExecution.mock.calls[0][1];
    fixture.detectChanges();
    expect(fixture.nativeElement.textContent).toContain('Cancellation request failed');

    (component as any).submitCancel();
    const retriedRequest = service.cancelExecution.mock.calls[1][1];
    expect(firstRequest.idempotencyKey).toBeTruthy();
    expect(retriedRequest.idempotencyKey).toBe(firstRequest.idempotencyKey);
  });

  it('uses lightweight status confirmation and retains one status-query key across retry', () => {
    service.requestExecutionStatus
      .mockReturnValueOnce(throwError(() => ({status: 503, code: 'SERVER_ERROR'})))
      .mockReturnValueOnce(of({}));
    const {component} = createComponent();
    const controls = (component as any).statusForm.controls;

    controls.reason.setValue('Callback appears stale');
    controls.expiresInSeconds.setValue(60);
    expect((component as any).canSubmitStatusQuery()).toBe(true);

    (component as any).submitStatusQuery();
    (component as any).submitStatusQuery();

    const firstRequest = service.requestExecutionStatus.mock.calls[0][1];
    const retriedRequest = service.requestExecutionStatus.mock.calls[1][1];
    expect(firstRequest.idempotencyKey).toBeTruthy();
    expect(retriedRequest.idempotencyKey).toBe(firstRequest.idempotencyKey);
    expect(retriedRequest.expiresInSeconds).toBe(60);
  });

  it('disables cancel for a non-cancellable execution', () => {
    service.getExecution.mockReturnValue(
      of({detail: {...detail, execution: {...detail.execution, cancellable: false}}})
    );

    const {fixture, component} = createComponent();
    expect((component as any).canSubmitCancel()).toBe(false);
    expect(fixture.nativeElement.querySelector('[data-testid="cancel-execution"]').disabled).toBe(true);
  });

  function createComponent(): {
    fixture: ComponentFixture<ExecutionDetailComponent>;
    component: ExecutionDetailComponent;
  } {
    const fixture = TestBed.createComponent(ExecutionDetailComponent);
    fixture.detectChanges();
    return {fixture, component: fixture.componentInstance};
  }
});
