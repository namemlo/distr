import {HttpClient, HttpErrorResponse, HttpParams} from '@angular/common/http';
import {inject, Injectable, InjectionToken} from '@angular/core';
import {catchError, forkJoin, map, Observable, of, switchMap, throwError} from 'rxjs';
import {DeploymentPlan} from '../types/deployment-plan';
import {ExperimentalFeatureFlag} from '../types/feature-flags';
import {
  OperatorApprovalDecision,
  OperatorApprovalDecisionRequest,
  OperatorApprovalFilters,
  OperatorApprovalPage,
  OperatorApprovalRequest,
  OperatorAuditDetailResponse,
  OperatorAuditExportSink,
  OperatorAuditExportStatus,
  OperatorAuditFilters,
  OperatorAuditRow,
  OperatorCampaignControlAction,
  OperatorCampaignControlRequest,
  OperatorCampaignControlResult,
  OperatorCampaignDetailResponse,
  OperatorCampaignExclusion,
  OperatorCampaignFilters,
  OperatorCampaignMemberControlAction,
  OperatorCampaignMemberControlRequest,
  OperatorCampaignRow,
  OperatorControlPlaneAuditEventPage,
  OperatorControlPlaneAuditListRequest,
  OperatorControlPlaneEnrollment,
  OperatorControlPlaneEnrollmentPage,
  OperatorControlPlaneError,
  OperatorCreateAuditExportSinkRequest,
  OperatorCreatePlanDraftRequest,
  OperatorCreateProductReleaseRequest,
  OperatorEvidenceBundle,
  OperatorEvidencePage,
  OperatorExecutionCancelRequest,
  OperatorExecutionDetailResponse,
  OperatorExecutionFilters,
  OperatorExecutionRow,
  OperatorExecutionStatusRequest,
  OperatorExecutionStatusResponse,
  OperatorFleetFilters,
  OperatorFleetRow,
  OperatorPage,
  OperatorPageRequest,
  OperatorPlanApprovalRequest,
  OperatorPlanCompareResponse,
  OperatorPlanDetailResponse,
  OperatorPlanDraft,
  OperatorPlanDraftValidation,
  OperatorPlanFilters,
  OperatorPlanRow,
  OperatorPreviousStatePlanRequest,
  OperatorProductRelease,
  OperatorProductReleaseValidation,
  OperatorPublishPlanDraftRequest,
  OperatorReconciliationDecisionRequest,
  OperatorReconciliationDetailResponse,
  OperatorReconciliationFilters,
  OperatorReconciliationRow,
  OperatorRegistryCoverage,
  OperatorRegistryImportDecisionRequest,
  OperatorRegistryImportPreview,
  OperatorRegistryImportPreviewRequest,
  OperatorRegistryImportResult,
  OperatorReleaseCompareResponse,
  OperatorReleaseDetailResponse,
  OperatorReleaseFilters,
  OperatorReleaseRow,
  OperatorSetupReadiness,
  OperatorSetupReadinessRequest,
  OperatorUpdatePlanDraftRequest,
} from '../types/operator-control-plane';
import {
  CreateUpdateReleaseBundleRequest,
  ReleaseBundle,
  ReleaseBundleValidationResponse,
} from '../types/release-bundle';
import {TargetConfigSnapshotListFilter, TargetConfigSnapshotPage} from '../types/target-config-snapshot';

const controlPlaneUrl = '/api/v1/control-plane';

export const OPERATOR_ACTION_KEY_FACTORY = new InjectionToken<() => string>('OPERATOR_ACTION_KEY_FACTORY', {
  providedIn: 'root',
  factory: () => () => globalThis.crypto.randomUUID(),
});

export const OPERATOR_READINESS_CLOCK = new InjectionToken<() => Date>('OPERATOR_READINESS_CLOCK', {
  providedIn: 'root',
  factory: () => () => new Date(),
});

@Injectable({providedIn: 'root'})
export class OperatorControlPlaneService {
  private readonly httpClient = inject(HttpClient);
  private readonly newActionKey = inject(OPERATOR_ACTION_KEY_FACTORY);
  private readonly readinessClock = inject(OPERATOR_READINESS_CLOCK);

  listFleet(filters: OperatorFleetFilters = {}): Observable<OperatorPage<OperatorFleetRow>> {
    return this.getPage<OperatorFleetRow>(`${controlPlaneUrl}/fleet`, filters);
  }

  listReleases(filters: OperatorReleaseFilters = {}): Observable<OperatorPage<OperatorReleaseRow>> {
    return this.getPage<OperatorReleaseRow>(`${controlPlaneUrl}/releases`, filters);
  }

  getRelease(releaseId: string): Observable<OperatorReleaseDetailResponse> {
    return this.get<OperatorReleaseDetailResponse>(`${controlPlaneUrl}/releases/${pathId(releaseId)}`);
  }

  compareReleases(releaseId: string, otherReleaseId: string): Observable<OperatorReleaseCompareResponse> {
    return this.get<OperatorReleaseCompareResponse>(
      `${controlPlaneUrl}/releases/${pathId(releaseId)}/compare/${pathId(otherReleaseId)}`
    );
  }

  getReleaseEvidence(releaseId: string): Observable<OperatorEvidencePage> {
    return this.get<OperatorEvidencePage>(`${controlPlaneUrl}/releases/${pathId(releaseId)}/evidence`);
  }

  listPlans(filters: OperatorPlanFilters = {}): Observable<OperatorPage<OperatorPlanRow>> {
    return this.getPage<OperatorPlanRow>(`${controlPlaneUrl}/plans`, filters);
  }

  getPlan(planId: string): Observable<OperatorPlanDetailResponse> {
    return this.get<OperatorPlanDetailResponse>(`${controlPlaneUrl}/plans/${pathId(planId)}`);
  }

  comparePlans(planId: string, otherPlanId: string): Observable<OperatorPlanCompareResponse> {
    return this.get<OperatorPlanCompareResponse>(
      `${controlPlaneUrl}/plans/${pathId(planId)}/compare/${pathId(otherPlanId)}`
    );
  }

  getPlanEvidence(planId: string): Observable<OperatorEvidencePage> {
    return this.get<OperatorEvidencePage>(`${controlPlaneUrl}/plans/${pathId(planId)}/evidence`);
  }

  listCampaigns(filters: OperatorCampaignFilters = {}): Observable<OperatorPage<OperatorCampaignRow>> {
    return this.getPage<OperatorCampaignRow>(`${controlPlaneUrl}/campaigns`, filters);
  }

  getCampaign(campaignId: string): Observable<OperatorCampaignDetailResponse> {
    return this.get<OperatorCampaignDetailResponse>(`${controlPlaneUrl}/campaigns/${pathId(campaignId)}`);
  }

  getCampaignEvidence(campaignId: string): Observable<OperatorEvidencePage> {
    return this.get<OperatorEvidencePage>(`${controlPlaneUrl}/campaigns/${pathId(campaignId)}/evidence`);
  }

  listExecutions(filters: OperatorExecutionFilters = {}): Observable<OperatorPage<OperatorExecutionRow>> {
    return this.getPage<OperatorExecutionRow>(`${controlPlaneUrl}/executions`, filters);
  }

  getExecution(executionId: string): Observable<OperatorExecutionDetailResponse> {
    return this.get<OperatorExecutionDetailResponse>(`${controlPlaneUrl}/executions/${pathId(executionId)}`);
  }

  getExecutionEvidence(executionId: string): Observable<OperatorEvidencePage> {
    return this.get<OperatorEvidencePage>(`${controlPlaneUrl}/executions/${pathId(executionId)}/evidence`);
  }

  listReconciliation(filters: OperatorReconciliationFilters = {}): Observable<OperatorPage<OperatorReconciliationRow>> {
    return this.getPage<OperatorReconciliationRow>(`${controlPlaneUrl}/reconciliation`, filters);
  }

  getReconciliation(reconciliationId: string): Observable<OperatorReconciliationDetailResponse> {
    return this.get<OperatorReconciliationDetailResponse>(
      `${controlPlaneUrl}/reconciliation/${pathId(reconciliationId)}`
    );
  }

  getReconciliationEvidence(reconciliationId: string): Observable<OperatorEvidencePage> {
    return this.get<OperatorEvidencePage>(`${controlPlaneUrl}/reconciliation/${pathId(reconciliationId)}/evidence`);
  }

  listAudit(filters: OperatorAuditFilters = {}): Observable<OperatorPage<OperatorAuditRow>> {
    return this.getPage<OperatorAuditRow>(`${controlPlaneUrl}/audit`, filters);
  }

  getAuditEvent(auditEventId: string): Observable<OperatorAuditDetailResponse> {
    return this.get<OperatorAuditDetailResponse>(`${controlPlaneUrl}/audit/${pathId(auditEventId)}`);
  }

  getAuditEvidence(auditEventId: string): Observable<OperatorEvidencePage> {
    return this.get<OperatorEvidencePage>(`${controlPlaneUrl}/audit/${pathId(auditEventId)}/evidence`);
  }

  listApprovals(filters: OperatorApprovalFilters = {}): Observable<OperatorApprovalPage> {
    return this.get<OperatorApprovalPage>('/api/v1/approval-requests', filters);
  }

  getApproval(approvalRequestId: string): Observable<OperatorApprovalRequest> {
    return this.get<OperatorApprovalRequest>(`/api/v1/approval-requests/${pathId(approvalRequestId)}`);
  }

  decideApproval(
    approvalRequestId: string,
    request: OperatorApprovalDecisionRequest
  ): Observable<OperatorApprovalDecision> {
    const body = {...request, idempotencyKey: request.idempotencyKey || this.newActionKey()};
    return this.post<OperatorApprovalDecision>(
      `/api/v1/approval-requests/${pathId(approvalRequestId)}/decisions`,
      body
    );
  }

  controlCampaign(
    campaignRunId: string,
    action: OperatorCampaignControlAction,
    request: OperatorCampaignControlRequest
  ): Observable<OperatorCampaignControlResult> {
    const body = {...request, requestId: request.requestId || this.newActionKey()};
    return this.post<OperatorCampaignControlResult>(
      `/api/v1/deployment-campaigns/${pathId(campaignRunId)}/${action}`,
      body
    );
  }

  controlCampaignMember(
    campaignRunId: string,
    action: OperatorCampaignMemberControlAction,
    request: OperatorCampaignMemberControlRequest
  ): Observable<OperatorCampaignExclusion | DeploymentPlan> {
    const body = {...request, requestId: request.requestId || this.newActionKey()};
    return this.post<OperatorCampaignExclusion | DeploymentPlan>(
      `/api/v1/deployment-campaigns/${pathId(campaignRunId)}/${action}`,
      body
    );
  }

  cancelExecution(executionId: string, request: OperatorExecutionCancelRequest): Observable<void> {
    const body = {...request, idempotencyKey: request.idempotencyKey || this.newActionKey()};
    return this.post<void>(`/api/v1/executions/${pathId(executionId)}/cancel`, body);
  }

  requestExecutionStatus(
    executionId: string,
    request: OperatorExecutionStatusRequest
  ): Observable<OperatorExecutionStatusResponse> {
    const body = {...request, idempotencyKey: request.idempotencyKey || this.newActionKey()};
    return this.post<OperatorExecutionStatusResponse>(`/api/v1/executions/${pathId(executionId)}/status-queries`, body);
  }

  resolveDriftCase(driftCaseId: string, request: OperatorReconciliationDecisionRequest): Observable<void> {
    return this.post<void>(`/api/v1/drift-cases/${pathId(driftCaseId)}/resolve`, request);
  }

  previewRegistryImport(request: OperatorRegistryImportPreviewRequest): Observable<OperatorRegistryImportPreview> {
    return this.post<OperatorRegistryImportPreview>('/api/v1/deployment-registry/imports/preview', request);
  }

  classifyRegistryImport(importId: string, request: OperatorRegistryImportDecisionRequest): Observable<void> {
    return this.post<void>(`/api/v1/deployment-registry/imports/${pathId(importId)}/decisions`, request);
  }

  applyRegistryImport(importId: string, previewChecksum: string): Observable<OperatorRegistryImportResult> {
    return this.post<OperatorRegistryImportResult>(`/api/v1/deployment-registry/imports/${pathId(importId)}/apply`, {
      previewChecksum,
    });
  }

  getRegistryImport(importId: string): Observable<OperatorRegistryImportPreview> {
    return this.get<OperatorRegistryImportPreview>(`/api/v1/deployment-registry/imports/${pathId(importId)}`);
  }

  getRegistryCoverage(importId: string): Observable<OperatorRegistryCoverage> {
    return this.get<OperatorRegistryCoverage>('/api/v1/deployment-registry/coverage', {importId});
  }

  createComponentRelease(
    request: CreateUpdateReleaseBundleRequest,
    idempotencyKey?: string
  ): Observable<ReleaseBundle> {
    const retainedKey = idempotencyKey || this.newActionKey();
    return this.safe(
      this.httpClient.post<ReleaseBundle>('/api/v1/release-bundles', request, {
        headers: {'Idempotency-Key': retainedKey},
      })
    );
  }

  validateComponentRelease(releaseId: string): Observable<ReleaseBundleValidationResponse> {
    return this.post<ReleaseBundleValidationResponse>(`/api/v1/release-bundles/${pathId(releaseId)}/validate`, {});
  }

  publishComponentRelease(releaseId: string): Observable<ReleaseBundle> {
    return this.post<ReleaseBundle>(`/api/v1/release-bundles/${pathId(releaseId)}/publish`, {});
  }

  createProductRelease(request: OperatorCreateProductReleaseRequest): Observable<OperatorProductRelease> {
    return this.post<OperatorProductRelease>('/api/v1/product-releases', request);
  }

  validateProductRelease(productReleaseId: string): Observable<OperatorProductReleaseValidation> {
    return this.post<OperatorProductReleaseValidation>(
      `/api/v1/product-releases/${pathId(productReleaseId)}/validate`,
      {}
    );
  }

  publishProductRelease(productReleaseId: string): Observable<OperatorProductRelease> {
    return this.post<OperatorProductRelease>(`/api/v1/product-releases/${pathId(productReleaseId)}/publish`, {});
  }

  createPlanDraft(request: OperatorCreatePlanDraftRequest): Observable<OperatorPlanDraft> {
    return this.post<OperatorPlanDraft>('/api/v1/deployment-plan-drafts', request);
  }

  updatePlanDraft(draftId: string, request: OperatorUpdatePlanDraftRequest): Observable<OperatorPlanDraft> {
    return this.safe(
      this.httpClient.patch<OperatorPlanDraft>(`/api/v1/deployment-plan-drafts/${pathId(draftId)}`, request)
    );
  }

  validatePlanDraft(draftId: string): Observable<OperatorPlanDraftValidation> {
    return this.post<OperatorPlanDraftValidation>(`/api/v1/deployment-plan-drafts/${pathId(draftId)}/validate`, {});
  }

  publishPlanDraft(draftId: string, request: OperatorPublishPlanDraftRequest): Observable<DeploymentPlan> {
    return this.post<DeploymentPlan>(`/api/v1/deployment-plan-drafts/${pathId(draftId)}/publish`, request);
  }

  requestPlanApproval(planId: string, request: OperatorPlanApprovalRequest): Observable<OperatorApprovalRequest> {
    return this.post<OperatorApprovalRequest>(`/api/v1/deployment-plans/${pathId(planId)}/approval-requests`, request);
  }

  createPreviousStatePlan(planId: string, request: OperatorPreviousStatePlanRequest): Observable<DeploymentPlan> {
    return this.post<DeploymentPlan>(`/api/v1/deployment-plans/${pathId(planId)}/previous-state`, request);
  }

  listControlPlaneAuditEvents(
    filters: OperatorControlPlaneAuditListRequest = {}
  ): Observable<OperatorControlPlaneAuditEventPage> {
    return this.get<OperatorControlPlaneAuditEventPage>('/api/v1/control-plane-audit/events', filters);
  }

  createEvidenceBundle(deploymentPlanId: string): Observable<OperatorEvidenceBundle> {
    return this.post<OperatorEvidenceBundle>('/api/v1/control-plane-audit/evidence-bundles', {deploymentPlanId});
  }

  listAuditExportSinks(): Observable<OperatorAuditExportSink[]> {
    return this.get<OperatorAuditExportSink[]>('/api/v1/control-plane-audit/export-sinks');
  }

  createAuditExportSink(request: OperatorCreateAuditExportSinkRequest): Observable<OperatorAuditExportSink> {
    return this.post<OperatorAuditExportSink>('/api/v1/control-plane-audit/export-sinks', request);
  }

  listAuditExportStatus(): Observable<OperatorAuditExportStatus[]> {
    return this.get<OperatorAuditExportStatus[]>('/api/v1/control-plane-audit/export-status');
  }

  listControlPlaneEnrollments(filters: OperatorPageRequest = {}): Observable<OperatorControlPlaneEnrollmentPage> {
    return this.get<OperatorControlPlaneEnrollmentPage>('/api/v1/authorization/control-plane-enrollments', filters);
  }

  listTargetConfigSnapshots(filters: TargetConfigSnapshotListFilter = {}): Observable<TargetConfigSnapshotPage> {
    return this.get<TargetConfigSnapshotPage>('/api/v1/target-config-snapshots/', filters);
  }

  listExperimentalFeatureFlags(): Observable<ExperimentalFeatureFlag[]> {
    return this.get<ExperimentalFeatureFlag[]>('/api/v1/experimental-feature-flags');
  }

  loadSetupReadiness(request: OperatorSetupReadinessRequest): Observable<OperatorSetupReadiness> {
    return forkJoin({
      features: this.listExperimentalFeatureFlags(),
      enrollments: this.listAllControlPlaneEnrollments(),
      snapshots: this.listTargetConfigSnapshots({deploymentUnitId: request.deploymentUnitId, limit: 1}),
      coverage: this.getRegistryCoverage(request.importId),
    }).pipe(
      map(({features, enrollments, snapshots, coverage}) => {
        const operatorControlPlaneEnabled = features.some(
          (flag) => flag.key === 'operator_control_plane_v2' && flag.enabled
        );
        const executorProtocolEnabled = features.some((flag) => flag.key === 'executor_protocol_v2' && flag.enabled);
        const hasTargetConfigSnapshot = snapshots.items.length > 0;
        const selectedEnvironmentId = snapshots.items[0]?.environmentId;
        const decisionAt = this.readinessClock();
        const hasEnabledEnrollment =
          Boolean(selectedEnvironmentId) &&
          enrollmentEffectiveAt(enrollments, 'organization', undefined, decisionAt) &&
          enrollmentEffectiveAt(enrollments, 'environment', selectedEnvironmentId, decisionAt);
        const registryCoverageComplete = coverage.complete;
        return {
          operatorControlPlaneEnabled,
          executorProtocolEnabled,
          hasEnabledEnrollment,
          hasTargetConfigSnapshot,
          registryCoverageComplete,
          ready:
            operatorControlPlaneEnabled &&
            executorProtocolEnabled &&
            hasEnabledEnrollment &&
            hasTargetConfigSnapshot &&
            registryCoverageComplete,
        };
      })
    );
  }

  private listAllControlPlaneEnrollments(
    cursor?: string,
    accumulated: OperatorControlPlaneEnrollment[] = []
  ): Observable<OperatorControlPlaneEnrollment[]> {
    return this.listControlPlaneEnrollments({cursor, limit: 100}).pipe(
      switchMap((page) => {
        const enrollments = [...accumulated, ...page.enrollments];
        return page.nextCursor ? this.listAllControlPlaneEnrollments(page.nextCursor, enrollments) : of(enrollments);
      })
    );
  }

  private getPage<T>(url: string, filters: object): Observable<OperatorPage<T>> {
    return this.get<OperatorPage<T>>(url, filters);
  }

  private get<T>(url: string, filters?: object): Observable<T> {
    const params = filters ? toHttpParams(filters) : undefined;
    return this.safe(this.httpClient.get<T>(url, {params}));
  }

  private post<T>(url: string, body: unknown): Observable<T> {
    return this.safe(this.httpClient.post<T>(url, body));
  }

  private safe<T>(request: Observable<T>): Observable<T> {
    return request.pipe(catchError((error: unknown) => throwError(() => normalizeOperatorControlPlaneError(error))));
  }
}

function enrollmentEffectiveAt(
  enrollments: OperatorControlPlaneEnrollment[],
  scopeKind: string,
  scopeId: string | undefined,
  decisionAt: Date
): boolean {
  const selected = enrollments
    .filter(
      (enrollment) =>
        enrollment.scope.kind === scopeKind &&
        (scopeId === undefined || enrollment.scope.id === scopeId) &&
        Date.parse(enrollment.effectiveFrom) <= decisionAt.getTime() &&
        (enrollment.effectiveUntil === undefined || Date.parse(enrollment.effectiveUntil) > decisionAt.getTime())
    )
    .sort(
      (left, right) => right.revision - left.revision || Date.parse(right.createdAt) - Date.parse(left.createdAt)
    )[0];
  return selected?.enabled === true;
}

export function normalizeOperatorControlPlaneError(error: unknown): OperatorControlPlaneError {
  if (!(error instanceof HttpErrorResponse)) {
    return {
      status: 0,
      code: 'REQUEST_FAILED',
      message: 'The control plane request failed. Try again.',
      retryable: false,
    };
  }

  const details: Record<number, Pick<OperatorControlPlaneError, 'code' | 'message' | 'retryable'>> = {
    0: {
      code: 'NETWORK_ERROR',
      message: 'The control plane is unreachable. Check your connection and try again.',
      retryable: true,
    },
    400: {
      code: 'INVALID_REQUEST',
      message: 'The request is invalid. Review the values and try again.',
      retryable: false,
    },
    401: {code: 'UNAUTHENTICATED', message: 'Your session has expired. Sign in again.', retryable: false},
    403: {code: 'FORBIDDEN', message: 'You do not have permission to perform this action.', retryable: false},
    404: {code: 'NOT_FOUND', message: 'The requested control plane record was not found.', retryable: false},
    409: {
      code: 'CONFLICT',
      message: 'The record changed since it was loaded. Refresh and review the latest revision.',
      retryable: false,
    },
    422: {
      code: 'VALIDATION_FAILED',
      message: 'The request did not pass control plane validation.',
      retryable: false,
    },
    429: {code: 'RATE_LIMITED', message: 'Too many requests. Wait a moment and try again.', retryable: true},
  };
  const fallback =
    error.status >= 500
      ? {
          code: 'SERVER_ERROR' as const,
          message: 'The control plane could not complete the request. Try again.',
          retryable: true,
        }
      : {
          code: 'REQUEST_FAILED' as const,
          message: 'The control plane request failed. Try again.',
          retryable: false,
        };
  const normalized = details[error.status] ?? fallback;
  const requestId = safeRequestId(error.error);

  return {
    status: error.status,
    ...normalized,
    ...(requestId ? {requestId} : {}),
  };
}

function toHttpParams(filters: object): HttpParams {
  let params = new HttpParams();
  for (const [key, value] of Object.entries(filters)) {
    if (value === undefined || value === null || value === '' || (Array.isArray(value) && value.length === 0)) {
      continue;
    }
    if (value instanceof Date) {
      params = params.set(key, value.toISOString());
      continue;
    }
    params = params.set(key, Array.isArray(value) ? value.join(',') : String(value));
  }
  return params;
}

function safeRequestId(body: unknown): string | undefined {
  if (!body || typeof body !== 'object' || !('requestId' in body)) {
    return undefined;
  }
  const requestId = (body as {requestId?: unknown}).requestId;
  return typeof requestId === 'string' && /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/.test(requestId) ? requestId : undefined;
}

function pathId(id: string): string {
  return encodeURIComponent(id);
}
