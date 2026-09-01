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
      fenceGeneration: 42,
      fenceResourceKey: 'target:target-1',
      idempotencyKey: 'external:execution-1',
    },
    intent: {
      key: 'intent',
      status: 'VERIFIED',
      checksum: 'sha256:intent',
      keyId: 'sha256:intent-key',
      blocking: false,
      order: 1,
    },
    adapter: {
      key: 'adapter',
      status: 'VERIFIED',
      expected: 'compose-v4',
      actual: 'compose-v4',
      checksum: 'sha256:adapter-proof',
      blocking: false,
      order: 2,
    },
    cancellation: {
      key: 'cancellation',
      status: 'NOT_REQUESTED',
      checksum: 'sha256:cancellation-proof',
      blocking: false,
      order: 3,
    },
    reconciliation: {
      key: 'reconciliation',
      status: 'PENDING',
      expected: 'CONVERGED',
      actual: 'PENDING',
      checksum: 'sha256:reconciliation-proof',
      blocking: true,
      order: 4,
    },
    tasks: [{id: 'task-1', key: 'task-1', status: 'RUNNING', blocking: false, order: 1}],
    steps: [{id: 'step-run-1', key: 'deploy', status: 'RUNNING', blocking: false, order: 1}],
    attempts: [
      {
        id: 'attempt-2',
        stepKey: 'deploy',
        status: 'RUNNING',
        attemptNumber: 2,
        planChecksum: 'sha256:attempt-plan',
        artifactDigest: 'sha256:attempt-artifact',
        configChecksum: 'sha256:attempt-config',
        fenceGeneration: 42,
        fenceResourceKey: 'target:target-1',
        idempotencyKey: 'external:execution-1',
        message: 'leased',
        blocking: false,
      },
    ],
    locks: [
      {
        id: 'lock-1',
        resourceType: 'deployment_target',
        resourceKey: 'target-1',
        concurrencyPolicy: 'QUEUE',
        status: 'ACQUIRED',
        createdAt: '2026-07-28T01:00:00Z',
        acquiredAt: '2026-07-28T01:00:01Z',
        currentConflict: false,
      },
    ],
    leases: [
      {
        id: 'lease-1',
        executorType: 'AGENT',
        attempt: 2,
        status: 'ACTIVE',
        leasedAt: '2026-07-28T01:00:01Z',
        expiresAt: '2026-07-28T01:05:00Z',
        heartbeatAt: '2026-07-28T01:01:00Z',
      },
    ],
    coordination: {
      inFlight: true,
      activeLockCount: 1,
      unreleasedLockCount: 1,
      activeLeaseCount: 1,
      unreleasedLeaseCount: 1,
      fenceStatus: 'ACTIVE',
      fenceGeneration: 42,
      fenceLeaseExpiresAt: '2026-07-28T01:05:00Z',
      timedOut: false,
      reconciliationRequired: false,
      zeroLockClosure: false,
    },
    observations: [
      {
        id: 'observation-1',
        key: 'callback',
        status: 'WAITING_FOR_ORACLE',
        expected: 'healthy',
        actual: 'unknown',
        checksum: 'sha256:observation-proof',
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

  it('renders a failed Transaction attempt followed by its safe retry without collapsing history', () => {
    service.getExecution.mockReturnValue(
      of({
        detail: {
          ...detail,
          execution: {
            ...detail.execution,
            attemptNumber: 2,
            status: 'SUCCEEDED',
            planChecksum: 'sha256:choice-plan',
            artifactDigest: 'sha256:transaction-t1',
            configChecksum: 'sha256:choice-config',
            fenceGeneration: 42,
            fenceResourceKey: 'choice-tp-dev/transaction-api',
          },
          attempts: [
            {
              id: 'transaction-attempt-1',
              stepKey: 'transaction:deploy',
              status: 'FAILED',
              attemptNumber: 1,
              planChecksum: 'sha256:choice-plan',
              artifactDigest: 'sha256:transaction-t1',
              configChecksum: 'sha256:choice-config',
              fenceGeneration: 41,
              fenceResourceKey: 'choice-tp-dev/transaction-api',
              idempotencyKey: 'transaction-t1-attempt-1',
              message: 'Controlled failure before Transaction mutation; C1/T0 remains healthy',
              blocking: true,
            },
            {
              id: 'transaction-attempt-2',
              stepKey: 'transaction:deploy',
              status: 'SUCCEEDED',
              attemptNumber: 2,
              planChecksum: 'sha256:choice-plan',
              artifactDigest: 'sha256:transaction-t1',
              configChecksum: 'sha256:choice-config',
              fenceGeneration: 42,
              fenceResourceKey: 'choice-tp-dev/transaction-api',
              idempotencyKey: 'transaction-t1-attempt-2',
              message: 'Safe retry reused the exact immutable plan, artifact, and config',
              blocking: false,
            },
          ],
          observations: [
            {
              id: 'choice-checkpoint-c1-t1',
              key: 'Checkpoint C1/T1',
              status: 'VERIFIED',
              expected: 'C1/T1',
              actual: 'C1/T1',
              checksum: 'sha256:choice-observation',
              message: 'Customer C1 and Transaction T1 are independently observed healthy',
              blocking: false,
              order: 1,
            },
          ],
        },
      } satisfies OperatorExecutionDetailResponse)
    );

    const {fixture} = createComponent();
    const text = fixture.nativeElement.textContent;

    expect(text.indexOf('transaction-attempt-1')).toBeLessThan(text.indexOf('transaction-attempt-2'));
    expect(text).toContain('FAILED');
    expect(text).toContain('SUCCEEDED');
    expect(text).toContain('Controlled failure before Transaction mutation');
    expect(text).toContain('Safe retry reused the exact immutable plan, artifact, and config');
    expect(text).toContain('choice-tp-dev/transaction-api #41');
    expect(text).toContain('choice-tp-dev/transaction-api #42');
    expect(text).toContain('Checkpoint C1/T1');
  });

  it('renders the complete evidence page without inventing evidence pagination', () => {
    const {fixture} = createComponent();

    expect(service.getExecutionEvidence).toHaveBeenCalledWith('execution-1');
    expect(fixture.nativeElement.textContent).toContain('Attempt 2 started');
    expect(fixture.nativeElement.textContent).not.toContain('Load more evidence');
  });

  it('renders execution contract facts with expected, actual, and evidence checksums', () => {
    const {fixture} = createComponent();
    const text = fixture.nativeElement.textContent;

    expect(text).toContain('Intent');
    expect(text).toContain('Adapter');
    expect(text).toContain('Cancellation');
    expect(text).toContain('Reconciliation');
    expect(text).toContain('sha256:intent-key');
    expect(text).toContain('target:target-1');
    expect(text).toContain('external:execution-1');
    expect(text).toContain('sha256:attempt-plan');
    expect(text).toContain('sha256:attempt-artifact');
    expect(text).toContain('sha256:attempt-config');
    expect(text).toContain('sha256:adapter-proof');
    expect(text).toContain('sha256:cancellation-proof');
    expect(text).toContain('sha256:reconciliation-proof');
    expect(text).toContain('sha256:observation-proof');
  });

  it('renders acquired and released coordination facts, expiry, conflict, and terminal closure', () => {
    service.getExecution.mockReturnValue(
      of({
        detail: {
          ...detail,
          execution: {...detail.execution, status: 'TIMED_OUT'},
          locks: [
            {...detail.locks[0], status: 'CONFLICTED', currentConflict: true},
            {
              ...detail.locks[0],
              id: 'lock-2',
              status: 'RELEASED',
              releasedAt: '2026-07-28T01:02:00Z',
              releaseReason: 'derived from terminal execution attempt: TIMED_OUT',
            },
          ],
          leases: [
            {...detail.leases[0], status: 'EXPIRED'},
            {
              ...detail.leases[0],
              id: 'lease-2',
              status: 'RELEASED',
              releasedAt: '2026-07-28T01:02:00Z',
              releaseReason: 'derived from lease expiry or reclaim',
            },
          ],
          coordination: {
            ...detail.coordination,
            inFlight: false,
            activeLockCount: 0,
            activeLeaseCount: 0,
            fenceStatus: 'RELEASED',
            timedOut: true,
            reconciliationRequired: true,
            zeroLockClosure: true,
            fenceReleasedAt: '2026-07-28T01:02:00Z',
          },
        },
      })
    );

    const {fixture} = createComponent();
    const text = fixture.nativeElement.textContent;
    expect(text).toContain('Current conflict');
    expect(text).toContain('RELEASED');
    expect(text).toContain('EXPIRED');
    expect(text).toContain('The execution timed out');
    expect(text).toContain('Reconciliation is required');
    expect(text).toContain('Zero-lock closure proven');
    expect(text).toContain('derived from terminal execution attempt: TIMED_OUT');
  });

  it('renders explicit empty lock and lease states', () => {
    service.getExecution.mockReturnValue(of({detail: {...detail, locks: [], leases: []}}));

    const {fixture} = createComponent();
    expect(fixture.nativeElement.textContent).toContain('No retained lock facts');
    expect(fixture.nativeElement.textContent).toContain('No retained lease facts');
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
