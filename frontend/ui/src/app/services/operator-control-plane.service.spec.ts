import {HttpErrorResponse, provideHttpClient} from '@angular/common/http';
import {HttpTestingController, provideHttpClientTesting} from '@angular/common/http/testing';
import {TestBed} from '@angular/core/testing';
import {retry} from 'rxjs';
import {OPERATOR_ACTION_KEY_FACTORY, OperatorControlPlaneService} from './operator-control-plane.service';

describe('OperatorControlPlaneService', () => {
  let http: HttpTestingController;
  let service: OperatorControlPlaneService;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        {provide: OPERATOR_ACTION_KEY_FACTORY, useValue: () => 'action-key-1'},
      ],
    });
    http = TestBed.inject(HttpTestingController);
    service = TestBed.inject(OperatorControlPlaneService);
  });

  afterEach(() => {
    http.verify();
  });

  it('serializes exact cursor and filters for all seven read collections', () => {
    service
      .listFleet({
        cursor: 'fleet_cursor',
        limit: 100,
        customerOrganizationId: 'customer-1',
        environmentId: 'environment-1',
        deploymentTargetId: 'target-1',
        deploymentUnitId: 'unit-1',
        component: 'payments-api',
        observedState: 'RUNNING',
        drift: 'DRIFTED',
        enrollment: 'ENROLLED',
        search: 'choice tp',
      })
      .subscribe();
    expectRequest('/api/v1/control-plane/fleet', {
      cursor: 'fleet_cursor',
      limit: '100',
      customerOrganizationId: 'customer-1',
      environmentId: 'environment-1',
      deploymentTargetId: 'target-1',
      deploymentUnitId: 'unit-1',
      component: 'payments-api',
      observedState: 'RUNNING',
      drift: 'DRIFTED',
      enrollment: 'ENROLLED',
      search: 'choice tp',
    });

    service
      .listReleases({
        cursor: 'release_cursor',
        limit: 25,
        applicationId: 'app-1',
        kind: 'PRODUCT',
        status: 'PUBLISHED',
        search: '2026.7',
      })
      .subscribe();
    expectRequest('/api/v1/control-plane/releases', {
      cursor: 'release_cursor',
      limit: '25',
      applicationId: 'app-1',
      kind: 'PRODUCT',
      status: 'PUBLISHED',
      search: '2026.7',
    });

    service
      .listPlans({
        cursor: 'plan_cursor',
        limit: 20,
        status: 'READY',
        environmentId: 'environment-1',
        deploymentUnitId: 'unit-1',
        productReleaseId: 'release-1',
      })
      .subscribe();
    expectRequest('/api/v1/control-plane/plans', {
      cursor: 'plan_cursor',
      limit: '20',
      status: 'READY',
      environmentId: 'environment-1',
      deploymentUnitId: 'unit-1',
      productReleaseId: 'release-1',
    });

    service
      .listCampaigns({
        cursor: 'campaign_cursor',
        limit: 15,
        status: 'RUNNING',
        environmentId: 'environment-1',
        deploymentPlanId: 'plan-1',
      })
      .subscribe();
    expectRequest('/api/v1/control-plane/campaigns', {
      cursor: 'campaign_cursor',
      limit: '15',
      status: 'RUNNING',
      environmentId: 'environment-1',
      deploymentPlanId: 'plan-1',
    });

    service
      .listExecutions({
        cursor: 'execution_cursor',
        limit: 10,
        status: 'FAILED',
        campaignId: 'campaign-1',
        deploymentPlanId: 'plan-1',
        deploymentTargetId: 'target-1',
        from: '2026-07-01T00:00:00.000Z',
        to: '2026-07-31T23:59:59.000Z',
      })
      .subscribe();
    expectRequest('/api/v1/control-plane/executions', {
      cursor: 'execution_cursor',
      limit: '10',
      status: 'FAILED',
      campaignId: 'campaign-1',
      deploymentPlanId: 'plan-1',
      deploymentTargetId: 'target-1',
      from: '2026-07-01T00:00:00.000Z',
      to: '2026-07-31T23:59:59.000Z',
    });

    service
      .listReconciliation({
        cursor: 'reconciliation_cursor',
        limit: 5,
        status: 'OPEN',
        drift: 'CONFIG',
        environmentId: 'environment-1',
        deploymentTargetId: 'target-1',
      })
      .subscribe();
    expectRequest('/api/v1/control-plane/reconciliation', {
      cursor: 'reconciliation_cursor',
      limit: '5',
      status: 'OPEN',
      drift: 'CONFIG',
      environmentId: 'environment-1',
      deploymentTargetId: 'target-1',
    });

    service
      .listAudit({
        cursor: 'audit_cursor',
        limit: 50,
        action: 'campaign.pause',
        subjectType: 'campaign',
        subjectId: 'campaign-1',
        actorUserAccountId: 'user-1',
        from: '2026-07-01T00:00:00.000Z',
        to: '2026-07-31T23:59:59.000Z',
        search: 'choice tp',
      })
      .subscribe();
    expectRequest('/api/v1/control-plane/audit', {
      cursor: 'audit_cursor',
      limit: '50',
      action: 'campaign.pause',
      subjectType: 'campaign',
      subjectId: 'campaign-1',
      actorUserAccountId: 'user-1',
      from: '2026-07-01T00:00:00.000Z',
      to: '2026-07-31T23:59:59.000Z',
      search: 'choice tp',
    });
  });

  it('omits undefined, null, empty-string, and empty-array query values', () => {
    service
      .listFleet({
        cursor: '',
        limit: undefined,
        component: null,
        search: 'payments',
        ignored: [],
      } as never)
      .subscribe();

    expectRequest('/api/v1/control-plane/fleet', {search: 'payments'});
  });

  it('calls every detail, compare, and evidence read endpoint without changing identifiers', () => {
    const calls: Array<[() => void, string]> = [
      [() => service.getRelease('release-1').subscribe(), '/api/v1/control-plane/releases/release-1'],
      [
        () => service.compareReleases('release-1', 'release-2').subscribe(),
        '/api/v1/control-plane/releases/release-1/compare/release-2',
      ],
      [() => service.getReleaseEvidence('release-1').subscribe(), '/api/v1/control-plane/releases/release-1/evidence'],
      [() => service.getPlan('plan-1').subscribe(), '/api/v1/control-plane/plans/plan-1'],
      [() => service.comparePlans('plan-1', 'plan-2').subscribe(), '/api/v1/control-plane/plans/plan-1/compare/plan-2'],
      [() => service.getPlanEvidence('plan-1').subscribe(), '/api/v1/control-plane/plans/plan-1/evidence'],
      [() => service.getCampaign('campaign-1').subscribe(), '/api/v1/control-plane/campaigns/campaign-1'],
      [
        () => service.getCampaignEvidence('campaign-1').subscribe(),
        '/api/v1/control-plane/campaigns/campaign-1/evidence',
      ],
      [() => service.getExecution('execution-1').subscribe(), '/api/v1/control-plane/executions/execution-1'],
      [
        () => service.getExecutionEvidence('execution-1').subscribe(),
        '/api/v1/control-plane/executions/execution-1/evidence',
      ],
      [
        () => service.getReconciliation('reconciliation-1').subscribe(),
        '/api/v1/control-plane/reconciliation/reconciliation-1',
      ],
      [
        () => service.getReconciliationEvidence('reconciliation-1').subscribe(),
        '/api/v1/control-plane/reconciliation/reconciliation-1/evidence',
      ],
      [() => service.getAuditEvent('audit-1').subscribe(), '/api/v1/control-plane/audit/audit-1'],
      [() => service.getAuditEvidence('audit-1').subscribe(), '/api/v1/control-plane/audit/audit-1/evidence'],
    ];

    for (const [call, url] of calls) {
      call();
      expectRequest(url, {});
    }
  });

  it('reuses the generated campaign request id when retry resubscribes', () => {
    service
      .controlCampaign('campaign-run-1', 'pause', {expectedVersion: 3, reason: 'maintenance'})
      .pipe(retry(1))
      .subscribe();

    const first = http.expectOne('/api/v1/deployment-campaigns/campaign-run-1/pause');
    expect(first.request.body).toEqual({
      requestId: 'action-key-1',
      expectedVersion: 3,
      reason: 'maintenance',
    });
    first.flush('temporary failure', {status: 503, statusText: 'Unavailable'});

    const retryRequest = http.expectOne('/api/v1/deployment-campaigns/campaign-run-1/pause');
    expect(retryRequest.request.body.requestId).toBe('action-key-1');
    retryRequest.flush({status: 'APPLIED'});
  });

  it('reuses generated idempotency keys for approval and execution actions', () => {
    service.listApprovals({state: 'PENDING', cursor: 'approval_cursor', limit: 20}).subscribe();
    expectRequest('/api/v1/approval-requests', {
      state: 'PENDING',
      cursor: 'approval_cursor',
      limit: '20',
    });

    service.getApproval('approval-1').subscribe();
    expectRequest('/api/v1/approval-requests/approval-1', {});

    service
      .decideApproval('approval-1', {
        approvalRequirementId: 'requirement-1',
        decision: 'APPROVE',
        comment: 'verified',
        expectedRequestRevision: 4,
      })
      .subscribe();
    const approval = http.expectOne('/api/v1/approval-requests/approval-1/decisions');
    expect(approval.request.body.idempotencyKey).toBe('action-key-1');
    approval.flush({});

    service.cancelExecution('execution-1', {reason: 'operator cancel'}).subscribe();
    const cancellation = http.expectOne('/api/v1/executions/execution-1/cancel');
    expect(cancellation.request.body).toEqual({
      idempotencyKey: 'action-key-1',
      reason: 'operator cancel',
    });
    cancellation.flush(null);

    service
      .requestExecutionStatus('execution-1', {
        reason: 'confirm state',
        expiresInSeconds: 60,
      })
      .subscribe();
    const status = http.expectOne('/api/v1/executions/execution-1/status-queries');
    expect(status.request.body).toEqual({
      idempotencyKey: 'action-key-1',
      reason: 'confirm state',
      expiresInSeconds: 60,
    });
    status.flush({});
  });

  it('sends one retained component-release idempotency header across retries', () => {
    service
      .createComponentRelease({version: '2026.7'} as never, 'provided-release-key')
      .pipe(retry(1))
      .subscribe();

    const first = http.expectOne('/api/v1/release-bundles');
    expect(first.request.headers.get('Idempotency-Key')).toBe('provided-release-key');
    expect(first.request.body).toEqual({version: '2026.7'});
    first.flush('temporary failure', {status: 503, statusText: 'Unavailable'});

    const retried = http.expectOne('/api/v1/release-bundles');
    expect(retried.request.headers.get('Idempotency-Key')).toBe('provided-release-key');
    expect(retried.request.body).toEqual({version: '2026.7'});
    retried.flush({});
  });

  it('uses only existing mutation endpoints and exact request bodies', () => {
    service.resolveDriftCase('drift-1', {action: 'CREATE_PLAN', reason: 'replace drift'}).subscribe();
    expectMutation('/api/v1/drift-cases/drift-1/resolve', 'POST', {
      action: 'CREATE_PLAN',
      reason: 'replace drift',
    });

    service.previewRegistryImport({sourceKind: 'manifest'} as never).subscribe();
    expectMutation('/api/v1/deployment-registry/imports/preview', 'POST', {sourceKind: 'manifest'});
    service.getRegistryImport('import-1').subscribe();
    expectRequest('/api/v1/deployment-registry/imports/import-1', {});
    service.classifyRegistryImport('import-1', {rootKey: 'choice-tp', classification: 'standard'}).subscribe();
    expectMutation('/api/v1/deployment-registry/imports/import-1/decisions', 'POST', {
      rootKey: 'choice-tp',
      classification: 'standard',
    });
    service.applyRegistryImport('import-1', 'sha256:preview').subscribe();
    expectMutation('/api/v1/deployment-registry/imports/import-1/apply', 'POST', {
      previewChecksum: 'sha256:preview',
    });

    service.createComponentRelease({version: '2026.7'} as never).subscribe();
    const componentRelease = http.expectOne('/api/v1/release-bundles');
    expect(componentRelease.request.method).toBe('POST');
    expect(componentRelease.request.headers.get('Idempotency-Key')).toBe('action-key-1');
    expect(componentRelease.request.body).toEqual({version: '2026.7'});
    componentRelease.flush({});
    service.validateComponentRelease('component-1').subscribe();
    expectMutation('/api/v1/release-bundles/component-1/validate', 'POST', {});
    service.publishComponentRelease('component-1').subscribe();
    expectMutation('/api/v1/release-bundles/component-1/publish', 'POST', {});

    service.createProductRelease({product: 'choice-tp', version: '2026.7'} as never).subscribe();
    expectMutation('/api/v1/product-releases', 'POST', {product: 'choice-tp', version: '2026.7'});
    service.validateProductRelease('product-1').subscribe();
    expectMutation('/api/v1/product-releases/product-1/validate', 'POST', {});
    service.publishProductRelease('product-1').subscribe();
    expectMutation('/api/v1/product-releases/product-1/publish', 'POST', {});

    service.createPlanDraft({productReleaseId: 'product-1'} as never).subscribe();
    expectMutation('/api/v1/deployment-plan-drafts', 'POST', {productReleaseId: 'product-1'});
    service.updatePlanDraft('draft-1', {expectedRevision: 2} as never).subscribe();
    expectMutation('/api/v1/deployment-plan-drafts/draft-1', 'PATCH', {expectedRevision: 2});
    service.validatePlanDraft('draft-1').subscribe();
    expectMutation('/api/v1/deployment-plan-drafts/draft-1/validate', 'POST', {});
    service
      .publishPlanDraft('draft-1', {
        expectedRevision: 2,
        expectedPreviewChecksum: 'sha256:preview',
      })
      .subscribe();
    expectMutation('/api/v1/deployment-plan-drafts/draft-1/publish', 'POST', {
      expectedRevision: 2,
      expectedPreviewChecksum: 'sha256:preview',
    });
  });

  it('uses existing campaign member, plan approval, and previous-state endpoints', () => {
    service
      .controlCampaignMember('campaign-run-1', 'retry', {
        expectedVersion: 5,
        reason: 'retry after verification',
        memberRunId: 'member-run-1',
        protocolVersion: 'v2',
      })
      .subscribe();
    expectMutation('/api/v1/deployment-campaigns/campaign-run-1/retry', 'POST', {
      requestId: 'action-key-1',
      expectedVersion: 5,
      reason: 'retry after verification',
      memberRunId: 'member-run-1',
      protocolVersion: 'v2',
    });

    service.requestPlanApproval('plan-1', {expiresAt: '2026-08-01T00:00:00.000Z'}).subscribe();
    expectMutation('/api/v1/deployment-plans/plan-1/approval-requests', 'POST', {
      expiresAt: '2026-08-01T00:00:00.000Z',
    });

    service
      .createPreviousStatePlan('plan-current', {
        successfulDeploymentPlanId: 'plan-successful',
        reason: 'restore last successful state',
      })
      .subscribe();
    expectMutation('/api/v1/deployment-plans/plan-current/previous-state', 'POST', {
      successfulDeploymentPlanId: 'plan-successful',
      reason: 'restore last successful state',
    });
  });

  it('uses PR-078 audit export and evidence endpoints', () => {
    service.listControlPlaneAuditEvents({afterSequence: 42, limit: 25}).subscribe();
    expectRequest('/api/v1/control-plane-audit/events', {afterSequence: '42', limit: '25'});

    service.createEvidenceBundle('plan-1').subscribe();
    expectMutation('/api/v1/control-plane-audit/evidence-bundles', 'POST', {deploymentPlanId: 'plan-1'});

    service.listAuditExportSinks().subscribe();
    expectRequest('/api/v1/control-plane-audit/export-sinks', {});

    service
      .createAuditExportSink({
        name: 'EMLO SIEM',
        kind: 'siem',
        endpointReference: 'secret://audit/siem',
        configChecksum: 'sha256:config',
        enabled: true,
      })
      .subscribe();
    expectMutation('/api/v1/control-plane-audit/export-sinks', 'POST', {
      name: 'EMLO SIEM',
      kind: 'siem',
      endpointReference: 'secret://audit/siem',
      configChecksum: 'sha256:config',
      enabled: true,
    });

    service.listAuditExportStatus().subscribe();
    expectRequest('/api/v1/control-plane-audit/export-status', {});
  });

  it('composes setup readiness only from existing feature, enrollment, config, and coverage endpoints', () => {
    let readiness: unknown;
    service
      .loadSetupReadiness({
        importId: 'import-1',
        deploymentUnitId: 'unit-1',
      })
      .subscribe((value) => (readiness = value));

    const features = http.expectOne('/api/v1/experimental-feature-flags');
    features.flush([
      {key: 'operator_control_plane_v2', enabled: true},
      {key: 'executor_protocol_v2', enabled: true},
    ]);

    const enrollments = http.expectOne((request) => request.url === '/api/v1/authorization/control-plane-enrollments');
    expect(enrollments.request.params.get('limit')).toBe('100');
    expect(enrollments.request.params.get('cursor')).toBeNull();
    enrollments.flush({
      enrollments: [{id: 'enrollment-disabled', enabled: false}],
      nextCursor: 'enrollment_next',
    });

    const nextEnrollments = http.expectOne(
      (request) => request.url === '/api/v1/authorization/control-plane-enrollments'
    );
    expect(nextEnrollments.request.params.get('limit')).toBe('100');
    expect(nextEnrollments.request.params.get('cursor')).toBe('enrollment_next');
    nextEnrollments.flush({enrollments: [{id: 'enrollment-1', enabled: true}]});

    const snapshots = http.expectOne((request) => request.url === '/api/v1/target-config-snapshots/');
    expect(snapshots.request.params.get('deploymentUnitId')).toBe('unit-1');
    expect(snapshots.request.params.get('limit')).toBe('1');
    snapshots.flush({items: [{id: 'snapshot-1'}]});

    const coverage = http.expectOne((request) => request.url === '/api/v1/deployment-registry/coverage');
    expect(coverage.request.params.get('importId')).toBe('import-1');
    coverage.flush({complete: true});

    expect(readiness).toEqual({
      operatorControlPlaneEnabled: true,
      executorProtocolEnabled: true,
      hasEnabledEnrollment: true,
      hasTargetConfigSnapshot: true,
      registryCoverageComplete: true,
      ready: true,
    });
  });

  it('normalizes server failures without exposing unsafe response details', () => {
    let observed: unknown;
    service.listFleet().subscribe({error: (error) => (observed = error)});

    const request = http.expectOne('/api/v1/control-plane/fleet');
    request.flush(
      {
        code: 'SQL_QUERY_FAILED',
        message: 'select failed for postgresql://operator:secret@database/control',
        requestId: 'request-123',
      },
      {status: 500, statusText: 'Internal Server Error'}
    );

    expect(observed).toEqual({
      status: 500,
      code: 'SERVER_ERROR',
      message: 'The control plane could not complete the request. Try again.',
      retryable: true,
      requestId: 'request-123',
    });
    expect((observed as {cause?: HttpErrorResponse}).cause).toBeUndefined();
  });

  function expectRequest(url: string, params: Record<string, string>) {
    const request = http.expectOne((candidate) => candidate.url === url);
    expect(request.request.method).toBe('GET');
    expect(request.request.params.keys().sort()).toEqual(Object.keys(params).sort());
    for (const [key, value] of Object.entries(params)) {
      expect(request.request.params.get(key)).withContext(key).toBe(value);
    }
    request.flush({items: []});
  }

  function expectMutation(url: string, method: string, body: unknown) {
    const request = http.expectOne(url);
    expect(request.request.method).toBe(method);
    expect(request.request.body).toEqual(body);
    request.flush({});
  }
});
