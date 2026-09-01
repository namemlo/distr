import {ComponentFixture, TestBed} from '@angular/core/testing';
import {of, Subject, throwError} from 'rxjs';
import {vi} from 'vitest';
import {OperatorControlPlaneService} from '../../services/operator-control-plane.service';
import {OperatorFleetRow, OperatorPage} from '../../types/operator-control-plane';
import {FleetComponent} from './fleet.component';

describe('FleetComponent', () => {
  let service: {listFleet: ReturnType<typeof vi.fn>};

  beforeEach(() => {
    service = {listFleet: vi.fn()};
    TestBed.configureTestingModule({
      imports: [FleetComponent],
      providers: [{provide: OperatorControlPlaneService, useValue: service}],
    });
  });

  it('announces loading before rendering the fleet matrix', async () => {
    const response$ = new Subject<OperatorPage<OperatorFleetRow>>();
    service.listFleet.mockReturnValue(response$);

    const fixture = TestBed.createComponent(FleetComponent);
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('[role="status"]').textContent).toContain('Loading fleet');

    response$.next({items: [fleetRow()]});
    response$.complete();
    await fixture.whenStable();
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('caption').textContent).toContain('Fleet matrix');
    expect(fixture.nativeElement.textContent).toContain('Payments API');
  });

  it('compares every loaded placement for a shared target without requesting a comparison endpoint', async () => {
    service.listFleet.mockReturnValue(
      of({
        items: [
          fleetRow(),
          fleetRow({
            id: 'fleet-2',
            deploymentUnitId: 'unit-2',
            unit: 'Worker',
            componentId: 'component-2',
            component: 'Payments worker',
            activeReleaseId: 'release-2',
            activeRelease: '2026.7.2',
          }),
        ],
      })
    );
    const {component, fixture} = await createComponent();

    (component as any).compareTarget('target-1');
    fixture.detectChanges();

    const comparison = fixture.nativeElement.querySelector('[aria-labelledby="shared-target-comparison-heading"]');
    expect(comparison.textContent).toContain('Shared target comparison');
    expect(comparison.textContent).toContain('Payments API');
    expect(comparison.textContent).toContain('Payments worker');
    expect(comparison.textContent).toContain('2026.7.1');
    expect(comparison.textContent).toContain('2026.7.2');
    expect(service.listFleet).toHaveBeenCalledTimes(1);
  });

  it('deduplicates overlapping cursor pages and retains the next cursor', async () => {
    service.listFleet.mockReturnValueOnce(of({items: [fleetRow()], nextCursor: 'cursor-2'})).mockReturnValueOnce(
      of({
        items: [fleetRow(), fleetRow({id: 'fleet-2', deploymentTargetId: 'target-2', target: 'Secondary'})],
        nextCursor: 'cursor-3',
      })
    );
    const {component, fixture} = await createComponent();

    await (component as any).loadMore();
    fixture.detectChanges();

    expect((component as any).rows().map((row: OperatorFleetRow) => row.id)).toEqual(['fleet-1', 'fleet-2']);
    expect((component as any).nextCursor()).toBe('cursor-3');
    expect(service.listFleet).toHaveBeenCalledWith({cursor: 'cursor-2', limit: 50});
    expect(fixture.nativeElement.querySelectorAll('tbody tr').length).toBe(2);
  });

  it('renders empty, partial, stale, and unknown states explicitly', async () => {
    service.listFleet.mockReturnValue(
      of({
        items: [
          fleetRow({id: 'partial', activeReleaseId: undefined, activeRelease: ''}),
          fleetRow({id: 'stale', observedState: 'STALE'}),
          fleetRow({id: 'unknown', observedState: 'UNKNOWN', drift: 'UNKNOWN'}),
        ],
      })
    );
    let created = await createComponent();

    expect(created.fixture.nativeElement.textContent).toContain('Partial evidence');
    expect(created.fixture.nativeElement.textContent).toContain('Stale evidence');
    expect(created.fixture.nativeElement.textContent).toContain('Unknown evidence');

    TestBed.resetTestingModule();
    service = {listFleet: vi.fn().mockReturnValue(of({items: []}))};
    TestBed.configureTestingModule({
      imports: [FleetComponent],
      providers: [{provide: OperatorControlPlaneService, useValue: service}],
    });
    created = await createComponent();

    expect(created.fixture.nativeElement.textContent).toContain('No fleet placements match the current scope');
  });

  it('renders each component native observation identity without collapsing checkpoint state', async () => {
    service.listFleet.mockReturnValue(
      of({
        items: [
          fleetRow({
            id: 'customer-c1',
            component: 'Customer API',
            observedArtifactDigest: 'sha256:customer-c1',
            observedConfigChecksum: 'sha256:customer-config-c1',
          }),
          fleetRow({
            id: 'transaction-t0',
            component: 'Transaction API',
            observedArtifactDigest: 'sha256:transaction-t0',
            observedConfigChecksum: 'sha256:transaction-config-t0',
          }),
        ],
      })
    );

    const {fixture} = await createComponent();
    const text = fixture.nativeElement.textContent;

    expect(text).toContain('Observed runtime identity');
    expect(text).toContain('sha256:customer-c1');
    expect(text).toContain('sha256:customer-config-c1');
    expect(text).toContain('sha256:transaction-t0');
    expect(text).toContain('sha256:transaction-config-t0');
    expect(text).toContain('linux/amd64');
    expect(text).toContain('2026-07-28');
    expect(text).toContain('sha256:capabilities-c1');
    expect(text).toContain('HEALTHY');
    expect(text).toContain('sha256:evidence-c1');
  });

  for (const errorCase of [
    {status: 403, message: 'You are not authorized to view the operator fleet.'},
    {status: 404, message: 'The operator control plane is disabled for this organization.'},
    {status: 500, message: 'The fleet could not be loaded. Try again.'},
  ]) {
    it(`renders the safe ${errorCase.status} fleet error as an alert`, async () => {
      service.listFleet.mockReturnValue(
        throwError(() => ({status: errorCase.status, message: 'unsafe provider detail'}))
      );

      const {fixture} = await createComponent();

      const alert = fixture.nativeElement.querySelector('[role="alert"]');
      expect(alert.textContent).toContain(errorCase.message);
      expect(alert.textContent).not.toContain('unsafe provider detail');
    });
  }

  async function createComponent(): Promise<{
    component: FleetComponent;
    fixture: ComponentFixture<FleetComponent>;
  }> {
    const fixture = TestBed.createComponent(FleetComponent);
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();
    return {component: fixture.componentInstance, fixture};
  }
});

function fleetRow(overrides: Partial<OperatorFleetRow> = {}): OperatorFleetRow {
  return {
    id: 'fleet-1',
    createdAt: '2026-07-28T01:00:00Z',
    customerOrganizationId: 'customer-1',
    customer: 'Choice Retail',
    environmentId: 'environment-1',
    environment: 'Production',
    deploymentTargetId: 'target-1',
    target: 'Shared cluster',
    deploymentUnitId: 'unit-1',
    unit: 'Payments',
    componentId: 'component-1',
    component: 'Payments API',
    activeReleaseId: 'release-1',
    activeRelease: '2026.7.1',
    pendingReleaseId: 'release-2',
    pendingRelease: '2026.7.2',
    observedState: 'RUNNING',
    observedEvidenceChecksum: 'sha256:evidence-c1',
    observedArtifactDigest: 'sha256:artifact-c1',
    observedConfigChecksum: 'sha256:config-c1',
    observedSchemaVersion: '2026-07-28',
    observedCapabilityChecksum: 'sha256:capabilities-c1',
    observedPlatform: 'linux/amd64',
    observedHealth: 'HEALTHY',
    drift: 'NONE',
    lastExecutionId: 'execution-1',
    lastExecution: 'SUCCEEDED',
    enrollment: 'ENROLLED',
    ...overrides,
  };
}
