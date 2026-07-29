import {ComponentFixture, TestBed} from '@angular/core/testing';
import {ActivatedRoute, convertToParamMap} from '@angular/router';
import {BehaviorSubject, of, Subject, throwError} from 'rxjs';
import {vi} from 'vitest';
import {OperatorControlPlaneService} from '../../services/operator-control-plane.service';
import {OverlayService} from '../../services/overlay.service';
import {OperatorApprovalRequest} from '../../types/operator-control-plane';
import {ApprovalsComponent} from './approvals.component';

describe('ApprovalsComponent', () => {
  let service: any;
  let overlay: any;
  let queryParams: BehaviorSubject<ReturnType<typeof convertToParamMap>>;

  beforeEach(() => {
    service = {
      listApprovals: vi.fn().mockReturnValue(of({items: [pendingApproval], nextCursor: 'page-2'})),
      getApproval: vi.fn().mockReturnValue(of(pendingApproval)),
      decideApproval: vi.fn().mockReturnValue(of(approvalDecision)),
    };
    overlay = {confirm: vi.fn().mockReturnValue(of(true))};
    queryParams = new BehaviorSubject(convertToParamMap({}));

    TestBed.configureTestingModule({
      imports: [ApprovalsComponent],
      providers: [
        {provide: OperatorControlPlaneService, useValue: service},
        {provide: OverlayService, useValue: overlay},
        {provide: ActivatedRoute, useValue: {queryParamMap: queryParams.asObservable()}},
      ],
    });
  });

  it('hydrates an exact approval deep link even when the request is absent from the first inbox page', async () => {
    const deepLinked = {...pendingApproval, id: 'approval-deep-link'};
    service.getApproval.mockReturnValueOnce(of(deepLinked));
    queryParams.next(convertToParamMap({requestId: deepLinked.id}));

    const {component} = await createComponent();

    expect(service.getApproval).toHaveBeenCalledWith(deepLinked.id);
    expect((component as any).selected()?.id).toBe(deepLinked.id);
  });

  it('renders a scoped forbidden state when an approval deep link is denied', async () => {
    service.getApproval.mockReturnValueOnce(throwError(() => ({status: 403})));
    queryParams.next(convertToParamMap({requestId: 'approval-denied'}));

    const {fixture, component} = await createComponent();

    expect((component as any).selected()).toBeUndefined();
    expect(fixture.nativeElement.textContent).toContain(
      'The server denied access to this approval request for your current scope'
    );
  });

  it('renders the approval inbox, server-derived invalidation, and unknown states', async () => {
    service.listApprovals.mockReturnValue(
      of({
        items: [
          pendingApproval,
          {
            ...pendingApproval,
            id: 'approval-invalidated',
            state: 'INVALIDATED',
            invalidationReason: 'PLAN_CHECKSUM_CHANGED',
            invalidatedAt: '2026-07-28T03:00:00Z',
          },
          {...pendingApproval, id: 'approval-unknown', state: 'WAITING_FOR_ORACLE'},
        ],
      })
    );

    const {fixture} = await createComponent();
    const text = fixture.nativeElement.textContent as string;

    expect(text).toContain('Approvals');
    expect(text).toContain('Invalidated by server');
    expect(text).toContain('PLAN_CHECKSUM_CHANGED');
    expect(text).toContain('Unknown (WAITING_FOR_ORACLE)');
    expect(fixture.nativeElement.querySelectorAll('button[aria-label="View approval"]').length).toBe(3);
  });

  it('loads immutable approval detail and renders partial evidence explicitly', async () => {
    service.getApproval.mockReturnValue(of({...pendingApproval, requirements: []}));
    const {fixture, component} = await createComponent();

    await (component as any).openApproval('approval-1');
    fixture.detectChanges();

    expect(service.getApproval).toHaveBeenCalledWith('approval-1');
    expect(fixture.nativeElement.textContent).toContain('Approval request');
    expect(fixture.nativeElement.textContent).toContain('Approval evidence is partial');
    expect(fixture.nativeElement.textContent).toContain('sha256:subject-1');
    expect(fixture.nativeElement.textContent).toContain('sha256:policy-1');
  });

  it('renders approval authority, principal group, and requester separation constraints', async () => {
    const {fixture, component} = await createComponent();
    await (component as any).openApproval('approval-1');
    fixture.detectChanges();

    const text = fixture.nativeElement.textContent;
    expect(text).toContain('group');
    expect(text).toContain('authority-1');
    expect(text).toContain('approvers');
    expect(text).toContain('Requester cannot approve');
  });

  it('confirms a decision and submits the loaded request revision with one persistent idempotency key', async () => {
    const {component} = await createComponent();
    await (component as any).openApproval('approval-1');
    (component as any).decisionComment.setValue('Reviewed immutable evidence.');

    await (component as any).decide(requirement, 'APPROVE');

    expect(overlay.confirm.mock.calls[0][0].confirmLabel).toBe('Approve');
    const firstRequest = service.decideApproval.mock.calls[0][1];
    expect(firstRequest.approvalRequirementId).toBe('requirement-1');
    expect(firstRequest.decision).toBe('APPROVE');
    expect(firstRequest.comment).toBe('Reviewed immutable evidence.');
    expect(firstRequest.expectedRequestRevision).toBe(4);
    expect(typeof firstRequest.idempotencyKey).toBe('string');
    expect(firstRequest.idempotencyKey.length).toBeGreaterThan(0);

    service.decideApproval.mockReturnValueOnce(throwError(() => ({status: 409})));
    (component as any).decisionComment.setValue('Reviewed immutable evidence.');
    await (component as any).decide(requirement, 'APPROVE');
    expect(service.decideApproval.mock.calls[1][1].idempotencyKey).toBe(firstRequest.idempotencyKey);
  });

  it('treats scoped 403 denial as authoritative and disables further decisions', async () => {
    service.decideApproval.mockReturnValue(throwError(() => ({status: 403})));
    const {fixture, component} = await createComponent();
    await (component as any).openApproval('approval-1');
    (component as any).decisionComment.setValue('Reviewed.');

    await (component as any).decide(requirement, 'REJECT');
    fixture.detectChanges();

    expect((component as any).decisionDenied()).toBe(true);
    expect((component as any).canDecide()).toBe(false);
    expect(fixture.nativeElement.textContent).toContain('The server denied approval decisions for your current scope');
  });

  it('shows stale conflicts, refreshes current detail, and never exposes an invalidate action', async () => {
    service.decideApproval.mockReturnValue(throwError(() => ({status: 409})));
    const {fixture, component} = await createComponent();
    await (component as any).openApproval('approval-1');
    (component as any).decisionComment.setValue('Reviewed.');

    await (component as any).decide(requirement, 'APPROVE');
    fixture.detectChanges();

    expect((component as any).stale()).toBe(true);
    expect(service.getApproval).toHaveBeenCalledTimes(2);
    expect(fixture.nativeElement.textContent).toContain('Approval changed on the server');
    expect(fixture.nativeElement.textContent).not.toContain('Invalidate approval');
  });

  it('handles loading, empty, forbidden, not-found, and generic error states', async () => {
    const pending = new Subject<any>();
    service.listApprovals.mockReturnValue(pending);
    const first = TestBed.createComponent(ApprovalsComponent);
    first.detectChanges();
    expect(first.nativeElement.textContent).toContain('Loading approvals');
    first.destroy();

    service.listApprovals.mockReturnValueOnce(of({items: []}));
    let created = await createComponent();
    expect(created.fixture.nativeElement.textContent).toContain('No approvals match');
    created.fixture.destroy();

    service.listApprovals.mockReturnValueOnce(throwError(() => ({status: 403})));
    created = await createComponent();
    expect(created.fixture.nativeElement.textContent).toContain('not authorized to view approvals');
    created.fixture.destroy();

    service.listApprovals.mockReturnValueOnce(throwError(() => ({status: 500})));
    created = await createComponent();
    expect(created.fixture.nativeElement.textContent).toContain('Could not load approvals');
    created.fixture.destroy();

    service.listApprovals.mockReturnValue(of({items: [pendingApproval]}));
    service.getApproval.mockReturnValueOnce(throwError(() => ({status: 404})));
    created = await createComponent();
    await (created.component as any).openApproval('missing');
    created.fixture.detectChanges();
    expect(created.fixture.nativeElement.textContent).toContain('Approval request was not found');
  });

  it('appends cursor pages without losing the inbox', async () => {
    const secondApproval = {...pendingApproval, id: 'approval-2'};
    service.listApprovals
      .mockReturnValueOnce(of({items: [pendingApproval], nextCursor: 'page-2'}))
      .mockReturnValueOnce(of({items: [secondApproval]}));
    const {component} = await createComponent();

    await (component as any).loadMore();

    expect(service.listApprovals.mock.calls.at(-1)?.[0]).toEqual({limit: 25, cursor: 'page-2'});
    expect((component as any).approvals().map((approval: OperatorApprovalRequest) => approval.id)).toEqual([
      'approval-1',
      'approval-2',
    ]);
  });

  it('clears the previous cursor before a reset request that fails', async () => {
    const {component} = await createComponent();
    expect((component as any).nextCursor()).toBe('page-2');
    service.listApprovals.mockReturnValueOnce(throwError(() => ({status: 500})));

    await (component as any).applyFilters();

    expect((component as any).nextCursor()).toBeUndefined();
  });

  async function createComponent(): Promise<{
    fixture: ComponentFixture<ApprovalsComponent>;
    component: ApprovalsComponent;
  }> {
    const fixture = TestBed.createComponent(ApprovalsComponent);
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();
    return {fixture, component: fixture.componentInstance};
  }
});

const requirement = {
  id: 'requirement-1',
  ruleKey: 'production',
  policyVersionId: 'policy-version-1',
  authorityKind: 'group',
  authorityId: 'authority-1',
  principalGroupId: 'approvers',
  quorum: 1,
  separationConstraints: ['requester_cannot_approve'],
  sortOrder: 1,
};

const pendingApproval: OperatorApprovalRequest = {
  id: 'approval-1',
  createdAt: '2026-07-28T01:00:00Z',
  updatedAt: '2026-07-28T02:00:00Z',
  subjectType: 'DEPLOYMENT_PLAN',
  subjectId: 'plan-1',
  subjectRevision: 3,
  subjectChecksum: 'sha256:subject-1',
  effectivePolicyChecksum: 'sha256:policy-1',
  subscriberSetChecksum: 'sha256:subscribers-1',
  requesterUserAccountId: 'requester-1',
  state: 'PENDING',
  revision: 4,
  expiresAt: '2099-07-29T00:00:00Z',
  requirements: [requirement],
  decisions: [],
};

const approvalDecision = {
  id: 'decision-1',
  createdAt: '2026-07-28T04:00:00Z',
  approvalRequestId: 'approval-1',
  approvalRequirementId: 'requirement-1',
  decision: 'APPROVE' as const,
  comment: 'Reviewed immutable evidence.',
  actorUserAccountId: 'operator-1',
  requestRevision: 4,
  idempotencyKey: 'approval-key-1',
};
