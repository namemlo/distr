import {ComponentFixture, TestBed} from '@angular/core/testing';
import {of, Subject, throwError} from 'rxjs';
import {vi} from 'vitest';
import {OperatorControlPlaneService} from '../../services/operator-control-plane.service';
import {OverlayService} from '../../services/overlay.service';
import {OperatorReconciliationRow} from '../../types/operator-control-plane';
import {ReconciliationComponent} from './reconciliation.component';

describe('ReconciliationComponent', () => {
  let service: any;
  let overlay: any;

  beforeEach(() => {
    service = {
      listReconciliation: vi.fn().mockReturnValue(of({items: [openCase], nextCursor: 'page-2'})),
      getReconciliation: vi.fn().mockReturnValue(of({detail: reconciliationDetail})),
      getReconciliationEvidence: vi.fn().mockReturnValue(of({items: [evidence]})),
      resolveDriftCase: vi.fn().mockReturnValue(of(undefined)),
    };
    overlay = {confirm: vi.fn().mockReturnValue(of(true))};

    TestBed.configureTestingModule({
      imports: [ReconciliationComponent],
      providers: [
        {provide: OperatorControlPlaneService, useValue: service},
        {provide: OverlayService, useValue: overlay},
      ],
    });
  });

  it('renders reconciliation rows and preserves unknown server states', async () => {
    service.listReconciliation.mockReturnValue(
      of({items: [openCase, {...openCase, id: 'reconciliation-unknown', status: 'QUARANTINED'}]})
    );
    const {fixture} = await createComponent();

    expect(fixture.nativeElement.textContent).toContain('Reconciliation');
    expect(fixture.nativeElement.textContent).toContain('Unknown (QUARANTINED)');
    expect(fixture.nativeElement.querySelectorAll('button[aria-label="View reconciliation"]').length).toBe(2);
  });

  it('loads detail and evidence while exposing partial evidence failures', async () => {
    service.getReconciliationEvidence.mockReturnValue(throwError(() => ({status: 500})));
    const {fixture, component} = await createComponent();

    await (component as any).openReconciliation('reconciliation-1');
    fixture.detectChanges();

    expect(service.getReconciliation).toHaveBeenCalledWith('reconciliation-1');
    expect(service.getReconciliationEvidence).toHaveBeenCalledWith('reconciliation-1');
    expect(fixture.nativeElement.textContent).toContain('Reconciliation case');
    expect(fixture.nativeElement.textContent).toContain('Desired state');
    expect(fixture.nativeElement.textContent).toContain('Evidence is temporarily incomplete');
  });

  it('uses proportional confirmation and refetches list, detail, and evidence after a 204 resolution', async () => {
    const {component} = await createComponent();
    await (component as any).openReconciliation('reconciliation-1');
    (component as any).resolutionForm.setValue({
      action: 'CREATE_PLAN',
      reason: 'Create a reviewed replacement plan.',
      deploymentPlanId: 'plan-2',
      outcomeObservationId: '',
      acceptedUntil: '',
    });

    await (component as any).resolve();

    expect(overlay.confirm.mock.calls[0][0].confirmLabel).toBe('Resolve case');
    expect(service.resolveDriftCase).toHaveBeenCalledWith('drift-1', {
      action: 'CREATE_PLAN',
      reason: 'Create a reviewed replacement plan.',
      deploymentPlanId: 'plan-2',
    });
    expect(service.listReconciliation).toHaveBeenCalledTimes(2);
    expect(service.getReconciliation).toHaveBeenCalledTimes(2);
    expect(service.getReconciliationEvidence).toHaveBeenCalledTimes(2);
  });

  it('requires action-specific evidence and disables closed or unauthorized cases', async () => {
    const {component} = await createComponent();
    await (component as any).openReconciliation('reconciliation-1');
    (component as any).resolutionForm.setValue({
      action: 'RESTORE_DESIRED',
      reason: 'Restored and observed.',
      deploymentPlanId: '',
      outcomeObservationId: '',
      acceptedUntil: '',
    });
    expect((component as any).canResolve()).toBe(false);

    (component as any).resolutionForm.controls.outcomeObservationId.setValue('observation-2');
    expect((component as any).canResolve()).toBe(true);

    (component as any).detail.set({
      ...reconciliationDetail,
      reconciliation: {...openCase, status: 'RESOLVED'},
    });
    expect((component as any).canResolve()).toBe(false);

    (component as any).mutationDenied.set(true);
    expect((component as any).canResolve()).toBe(false);
  });

  it('treats a 403 decision denial as authoritative for the current scope', async () => {
    service.resolveDriftCase.mockReturnValue(throwError(() => ({status: 403})));
    const {fixture, component} = await createComponent();
    await (component as any).openReconciliation('reconciliation-1');
    (component as any).resolutionForm.setValue({
      action: 'ACCEPT_DEVIATION',
      reason: 'Accepted for a bounded maintenance window.',
      deploymentPlanId: '',
      outcomeObservationId: '',
      acceptedUntil: '2099-07-29T00:00',
    });

    await (component as any).resolve();
    fixture.detectChanges();

    expect((component as any).mutationDenied()).toBe(true);
    expect((component as any).canResolve()).toBe(false);
    expect(fixture.nativeElement.textContent).toContain(
      'The server denied reconciliation actions for your current scope'
    );
  });

  it('shows stale conflicts and refreshes the current case after 409', async () => {
    service.resolveDriftCase.mockReturnValue(throwError(() => ({status: 409})));
    const {fixture, component} = await createComponent();
    await (component as any).openReconciliation('reconciliation-1');
    (component as any).resolutionForm.setValue({
      action: 'CREATE_PLAN',
      reason: 'Replace stale intent.',
      deploymentPlanId: 'plan-2',
      outcomeObservationId: '',
      acceptedUntil: '',
    });

    await (component as any).resolve();
    fixture.detectChanges();

    expect((component as any).stale()).toBe(true);
    expect(service.getReconciliation).toHaveBeenCalledTimes(2);
    expect(service.getReconciliationEvidence).toHaveBeenCalledTimes(2);
    expect(fixture.nativeElement.textContent).toContain('case changed on the server');
  });

  it('handles loading, empty, forbidden, not-found, and generic error states', async () => {
    const pending = new Subject<any>();
    service.listReconciliation.mockReturnValue(pending);
    const first = TestBed.createComponent(ReconciliationComponent);
    first.detectChanges();
    expect(first.nativeElement.textContent).toContain('Loading reconciliation');
    first.destroy();

    service.listReconciliation.mockReturnValueOnce(of({items: []}));
    let created = await createComponent();
    expect(created.fixture.nativeElement.textContent).toContain('No reconciliation cases match');
    created.fixture.destroy();

    service.listReconciliation.mockReturnValueOnce(throwError(() => ({status: 403})));
    created = await createComponent();
    expect(created.fixture.nativeElement.textContent).toContain('not authorized to view reconciliation');
    created.fixture.destroy();

    service.listReconciliation.mockReturnValueOnce(throwError(() => ({status: 500})));
    created = await createComponent();
    expect(created.fixture.nativeElement.textContent).toContain('Could not load reconciliation cases');
    created.fixture.destroy();

    service.listReconciliation.mockReturnValue(of({items: [openCase]}));
    service.getReconciliation.mockReturnValueOnce(throwError(() => ({status: 404})));
    created = await createComponent();
    await (created.component as any).openReconciliation('missing');
    created.fixture.detectChanges();
    expect(created.fixture.nativeElement.textContent).toContain('Reconciliation case was not found');
  });

  it('appends cursor pages without losing earlier reconciliation cases', async () => {
    const secondCase = {...openCase, id: 'reconciliation-2'};
    service.listReconciliation
      .mockReturnValueOnce(of({items: [openCase], nextCursor: 'page-2'}))
      .mockReturnValueOnce(of({items: [secondCase]}));
    const {component} = await createComponent();

    await (component as any).loadMore();

    expect(service.listReconciliation.mock.calls.at(-1)?.[0]).toEqual({limit: 25, cursor: 'page-2'});
    expect((component as any).cases().map((item: OperatorReconciliationRow) => item.id)).toEqual([
      'reconciliation-1',
      'reconciliation-2',
    ]);
  });

  async function createComponent(): Promise<{
    fixture: ComponentFixture<ReconciliationComponent>;
    component: ReconciliationComponent;
  }> {
    const fixture = TestBed.createComponent(ReconciliationComponent);
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();
    return {fixture, component: fixture.componentInstance};
  }
});

const openCase: OperatorReconciliationRow = {
  id: 'reconciliation-1',
  createdAt: '2026-07-28T01:00:00Z',
  driftCaseId: 'drift-1',
  executionId: 'execution-1',
  deploymentPlanId: 'plan-1',
  environmentId: 'environment-1',
  deploymentTargetId: 'target-1',
  component: 'payments-api',
  drift: 'CONFIG',
  status: 'OPEN',
  outcome: 'ACTION_REQUIRED',
  observedAt: '2026-07-28T01:30:00Z',
  evidenceChecksum: 'sha256:evidence-1',
};

const reconciliationDetail = {
  reconciliation: openCase,
  desiredState: {key: 'release', actual: '2026.07.28', blocking: false, order: 1},
  observation: {key: 'release', actual: '2026.07.27', blocking: false, order: 1},
  decision: undefined,
  evidence: [],
};

const evidence = {
  id: 'evidence-1',
  kind: 'observation',
  label: 'Observed configuration',
  href: '/api/v1/evidence/evidence-1',
  checksum: 'sha256:evidence-1',
  createdAt: '2026-07-28T01:30:00Z',
};
