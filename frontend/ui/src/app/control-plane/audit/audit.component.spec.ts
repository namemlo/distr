import {HttpErrorResponse} from '@angular/common/http';
import {ComponentFixture, TestBed} from '@angular/core/testing';
import {ActivatedRoute, convertToParamMap} from '@angular/router';
import {of, Subject, throwError} from 'rxjs';
import {vi} from 'vitest';
import {OperatorControlPlaneService} from '../../services/operator-control-plane.service';
import {AuditComponent} from './audit.component';

describe('AuditComponent', () => {
  let service: any;
  let queryParams: Record<string, string>;

  const auditRow = {
    id: 'AUTO-audit-1',
    createdAt: '2026-07-28T08:00:00Z',
    sequence: 42,
    action: 'campaign.pause',
    subjectType: 'campaign',
    subjectId: 'AUTO-campaign-1',
    actorUserAccountId: 'AUTO-user-1',
    outcome: 'PARTIAL',
    correlationCount: 2,
    payloadChecksum: `sha256:${'a'.repeat(64)}`,
  };

  beforeEach(() => {
    queryParams = {};
    service = {
      listAudit: vi.fn().mockReturnValue(of({items: [auditRow], nextCursor: 'next-audit'})),
      getAuditEvent: vi.fn().mockReturnValue(
        of({
          detail: {
            event: auditRow,
            correlations: [
              {id: 'AUTO-plan-1', kind: 'deploymentPlanId'},
              {id: 'AUTO-unknown-1', kind: 'unknown'},
            ],
            payload: {state: 'PARTIAL'},
            evidence: [],
          },
        })
      ),
      getAuditEvidence: vi.fn().mockReturnValue(
        of({
          items: [
            {
              id: 'AUTO-evidence-1',
              kind: 'deployment-plan',
              label: 'AUTO plan evidence',
              href: '/deployments/plans/AUTO-plan-1',
              checksum: `sha256:${'b'.repeat(64)}`,
              createdAt: '2026-07-28T08:00:00Z',
            },
          ],
        })
      ),
      createEvidenceBundle: vi.fn().mockReturnValue(
        of({
          version: 'distr.deployment-evidence/v1',
          deploymentPlanId: 'AUTO-plan-1',
          events: [{id: 'AUTO-audit-1'}],
          checksum: `sha256:${'c'.repeat(64)}`,
        })
      ),
      listAuditExportSinks: vi.fn().mockReturnValue(
        of([
          {
            id: 'AUTO-sink-1',
            name: 'AUTO security archive',
            kind: 'siem',
            endpointReference: 'secret://audit/AUTO-security-archive',
            configChecksum: `sha256:${'d'.repeat(64)}`,
            enabled: false,
            consecutiveFailures: 0,
            createdAt: '2026-07-28T08:00:00Z',
            updatedAt: '2026-07-28T08:00:00Z',
          },
        ])
      ),
      createAuditExportSink: vi.fn().mockReturnValue(of({id: 'AUTO-sink-2'})),
      listAuditExportStatus: vi.fn().mockReturnValue(
        of([
          {
            sink: {
              id: 'AUTO-sink-1',
              name: 'AUTO security archive',
              kind: 'siem',
              endpointReference: 'secret://audit/AUTO-security-archive',
              configChecksum: `sha256:${'d'.repeat(64)}`,
              enabled: false,
              consecutiveFailures: 0,
              createdAt: '2026-07-28T08:00:00Z',
              updatedAt: '2026-07-28T08:00:00Z',
            },
            lastExportedSequence: 40,
            latestSequence: 42,
            checkpointLag: 2,
            alert: true,
            lastAttemptStatus: '',
          },
          {
            sink: {
              id: 'AUTO-sink-2',
              name: 'AUTO enabled archive',
              kind: 'object_store',
              endpointReference: 'secret://audit/AUTO-enabled-archive',
              configChecksum: `sha256:${'e'.repeat(64)}`,
              enabled: true,
              consecutiveFailures: 1,
              createdAt: '2026-07-28T08:00:00Z',
              updatedAt: '2026-07-28T08:00:00Z',
            },
            lastExportedSequence: 40,
            latestSequence: 42,
            checkpointLag: 2,
            alert: true,
            lastAttemptStatus: 'FAILED',
          },
        ])
      ),
    };

    TestBed.configureTestingModule({
      imports: [AuditComponent],
      providers: [
        {provide: OperatorControlPlaneService, useValue: service},
        {
          provide: ActivatedRoute,
          useFactory: () => ({snapshot: {queryParamMap: convertToParamMap(queryParams)}}),
        },
      ],
    });
  });

  it('hydrates filters from an audit evidence deep link before the initial request', async () => {
    queryParams = {
      action: 'plan.publish',
      subjectType: 'deployment_plan',
      subjectId: 'AUTO-plan-1',
      actorUserAccountId: 'AUTO-user-1',
      search: 'AUTO evidence',
    };

    const {component} = await createComponent();

    expect((component as any).filters.getRawValue()).toEqual(queryParams);
    expect(service.listAudit.mock.calls[0][0]).toEqual({
      limit: 50,
      ...queryParams,
    });
  });

  it('renders paginated audit, disabled sink, stale lag, partial, and unknown states', async () => {
    const {fixture} = await createComponent();
    const text = fixture.nativeElement.textContent as string;

    for (const expected of [
      'Control-plane audit',
      'campaign.pause',
      'AUTO-campaign-1',
      'Partial',
      'Load more',
      'AUTO security archive',
      'Disabled',
      'Stale',
      'Unknown',
    ]) {
      expect(text).toContain(expected);
    }
  });

  it('loads the next keyset page with every active filter without discarding existing events', async () => {
    const {fixture, component} = await createComponent();
    (component as any).filters.setValue({
      action: 'campaign.pause',
      subjectType: 'campaign',
      subjectId: 'AUTO-campaign-1',
      actorUserAccountId: 'AUTO-user-1',
      search: 'AUTO',
    });
    service.listAudit.mockReturnValueOnce(
      of({
        items: [{...auditRow, id: 'AUTO-audit-2', sequence: 41, action: 'plan.publish'}],
      })
    );

    await (component as any).loadMore();
    fixture.detectChanges();

    expect(service.listAudit.mock.calls.at(-1)?.[0]).toEqual({
      cursor: 'next-audit',
      limit: 50,
      action: 'campaign.pause',
      subjectType: 'campaign',
      subjectId: 'AUTO-campaign-1',
      actorUserAccountId: 'AUTO-user-1',
      search: 'AUTO',
    });
    expect(fixture.nativeElement.textContent as string).toContain('campaign.pause');
    expect(fixture.nativeElement.textContent as string).toContain('plan.publish');
  });

  it('applies audit filters and replaces the prior result set', async () => {
    const {component} = await createComponent();
    (component as any).filters.setValue({
      action: 'campaign.pause',
      subjectType: 'campaign',
      subjectId: 'AUTO-campaign-1',
      actorUserAccountId: 'AUTO-user-1',
      search: 'AUTO',
    });

    await (component as any).search();

    expect(service.listAudit.mock.calls.at(-1)?.[0]).toEqual({
      limit: 50,
      action: 'campaign.pause',
      subjectType: 'campaign',
      subjectId: 'AUTO-campaign-1',
      actorUserAccountId: 'AUTO-user-1',
      search: 'AUTO',
    });
  });

  it('opens audit detail, payload, correlations, and backend-provided evidence deep links', async () => {
    const {fixture, component} = await createComponent();

    await (component as any).selectEvent(auditRow);
    fixture.detectChanges();

    expect(service.getAuditEvent).toHaveBeenCalledWith('AUTO-audit-1');
    expect(service.getAuditEvidence).toHaveBeenCalledWith('AUTO-audit-1');
    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('Audit event');
    expect(text).toContain('AUTO-plan-1');
    expect(text).toContain('AUTO-unknown-1');
    expect(text).toContain('"state": "PARTIAL"');
    const link = fixture.nativeElement.querySelector('a[href="/deployments/plans/AUTO-plan-1"]') as HTMLAnchorElement;
    expect(link.textContent).toContain('AUTO plan evidence');
  });

  it('shows not-found detail without discarding the audit list', async () => {
    service.getAuditEvent.mockReturnValue(
      throwError(() => new HttpErrorResponse({status: 404, statusText: 'Not Found'}))
    );
    const {fixture, component} = await createComponent();

    await (component as any).selectEvent(auditRow);
    fixture.detectChanges();

    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('This audit event was not found.');
    expect(text).toContain('campaign.pause');
  });

  it('builds and renders deterministic evidence metadata without inventing a download', async () => {
    const {fixture, component} = await createComponent();
    (component as any).bundleForm.setValue({deploymentPlanId: 'AUTO-plan-1'});

    await (component as any).buildBundle();
    fixture.detectChanges();

    expect(service.createEvidenceBundle).toHaveBeenCalledWith('AUTO-plan-1');
    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('distr.deployment-evidence/v1');
    expect(text).toContain(`sha256:${'c'.repeat(64)}`);
    expect(text).toContain('1 event');
    expect(text).not.toContain('Download bundle');
  });

  it('requires explicit sink confirmation and submits only a secret reference', async () => {
    const {fixture, component} = await createComponent();
    (component as any).sinkForm.setValue({
      name: 'AUTO object archive',
      kind: 'object_store',
      endpointReference: 'secret://audit/AUTO-object-archive',
      configChecksum: `sha256:${'e'.repeat(64)}`,
      enabled: true,
      confirmed: false,
    });

    await (component as any).createSink();
    expect(service.createAuditExportSink).not.toHaveBeenCalled();

    (component as any).sinkForm.controls.confirmed.setValue(true);
    await (component as any).createSink();
    fixture.detectChanges();

    expect(service.createAuditExportSink).toHaveBeenCalledWith({
      name: 'AUTO object archive',
      kind: 'object_store',
      endpointReference: 'secret://audit/AUTO-object-archive',
      configChecksum: `sha256:${'e'.repeat(64)}`,
      enabled: true,
    });
    expect(fixture.nativeElement.textContent as string).toContain('Export sink created.');
  });

  it('keeps available audit data when export status is unavailable', async () => {
    service.listAuditExportStatus.mockReturnValue(
      throwError(() => new HttpErrorResponse({status: 503, statusText: 'Unavailable'}))
    );
    const {fixture} = await createComponent();

    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('campaign.pause');
    expect(text).toContain('Export status is unavailable.');
  });

  it('renders forbidden and initial loading states', async () => {
    const audit$ = new Subject<any>();
    service.listAudit.mockReturnValue(audit$);
    const fixture = TestBed.createComponent(AuditComponent);
    fixture.detectChanges();
    expect(fixture.nativeElement.textContent).toContain('Loading audit events');

    audit$.error(new HttpErrorResponse({status: 403, statusText: 'Forbidden'}));
    await fixture.whenStable();
    fixture.detectChanges();
    expect(fixture.nativeElement.textContent).toContain('You do not have permission to view control-plane audit.');
  });

  async function createComponent(): Promise<{
    fixture: ComponentFixture<AuditComponent>;
    component: AuditComponent;
  }> {
    const fixture = TestBed.createComponent(AuditComponent);
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();
    return {fixture, component: fixture.componentInstance};
  }
});
