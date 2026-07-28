export interface OperatorPageRequest {
  cursor?: string | null;
  limit?: number | null;
}

export interface OperatorPage<T> {
  items: T[];
  nextCursor?: string;
  total?: number;
}

export interface OperatorEvidencePage {
  items: OperatorEvidenceRef[];
  nextCursor?: string;
}

export interface OperatorEvidenceRef {
  id: string;
  kind: string;
  label: string;
  href: string;
  checksum: string;
  createdAt: string;
}

export interface OperatorFleetFilters extends OperatorPageRequest {
  customerOrganizationId?: string | null;
  environmentId?: string | null;
  deploymentTargetId?: string | null;
  deploymentUnitId?: string | null;
  component?: string | null;
  observedState?: string | null;
  drift?: string | null;
  enrollment?: string | null;
  search?: string | null;
}

export interface OperatorReleaseFilters extends OperatorPageRequest {
  applicationId?: string | null;
  kind?: string | null;
  status?: string | null;
  search?: string | null;
}

export interface OperatorPlanFilters extends OperatorPageRequest {
  status?: string | null;
  environmentId?: string | null;
  deploymentUnitId?: string | null;
  productReleaseId?: string | null;
}

export interface OperatorCampaignFilters extends OperatorPageRequest {
  status?: string | null;
  environmentId?: string | null;
  deploymentPlanId?: string | null;
}

export interface OperatorExecutionFilters extends OperatorPageRequest {
  status?: string | null;
  campaignId?: string | null;
  deploymentPlanId?: string | null;
  deploymentTargetId?: string | null;
  from?: string | null;
  to?: string | null;
}

export interface OperatorReconciliationFilters extends OperatorPageRequest {
  status?: string | null;
  drift?: string | null;
  environmentId?: string | null;
  deploymentTargetId?: string | null;
}

export interface OperatorAuditFilters extends OperatorPageRequest {
  action?: string | null;
  subjectType?: string | null;
  subjectId?: string | null;
  actorUserAccountId?: string | null;
  from?: string | null;
  to?: string | null;
  search?: string | null;
}

export interface OperatorFleetRow {
  id: string;
  createdAt: string;
  customerOrganizationId?: string;
  customer: string;
  environmentId: string;
  environment: string;
  deploymentTargetId: string;
  target: string;
  deploymentUnitId: string;
  unit: string;
  componentId?: string;
  component: string;
  activeReleaseId?: string;
  activeRelease: string;
  pendingReleaseId?: string;
  pendingRelease: string;
  observedState: string;
  drift: string;
  lastExecutionId?: string;
  lastExecution: string;
  enrollment: string;
}

export interface OperatorReleaseRow {
  id: string;
  createdAt: string;
  kind: string;
  applicationId: string;
  releaseNumber?: number;
  version: string;
  status: string;
  checksum: string;
  sourceRevision: string;
  publishedAt?: string;
  artifactCount: number;
  evidenceCount: number;
  componentCount: number;
  graphEdgeCount: number;
}

export interface OperatorReleaseArtifact {
  id: string;
  name: string;
  version: string;
  manifestDigest: string;
  platformDigests: Record<string, string>;
}

export interface OperatorReleaseComponentPin {
  componentReleaseId: string;
  component: string;
  version: string;
  checksum: string;
  digest: string;
}

export interface OperatorReleaseGraphEdge {
  from: string;
  to: string;
  kind: string;
}

export interface OperatorReleaseDetail {
  release: OperatorReleaseRow;
  artifacts: OperatorReleaseArtifact[];
  componentPins: OperatorReleaseComponentPin[];
  graphEdges: OperatorReleaseGraphEdge[];
  evidence: OperatorEvidenceRef[];
}

export interface OperatorReleaseCompareFact {
  component: string;
  change: string;
  leftChecksum?: string;
  rightChecksum?: string;
  leftDigest?: string;
  rightDigest?: string;
}

export interface OperatorReleaseCompare {
  left: OperatorReleaseRow;
  right: OperatorReleaseRow;
  changes: OperatorReleaseCompareFact[];
}

export interface OperatorPlanRow {
  id: string;
  createdAt: string;
  status: string;
  planSchema: string;
  protocolVersion: string;
  productReleaseId: string;
  productReleaseVersion: string;
  environmentId: string;
  environment: string;
  deploymentUnitId?: string;
  deploymentUnit: string;
  targetConfigSnapshotId?: string;
  canonicalChecksum: string;
  targetCount: number;
  stepCount: number;
  issueCount: number;
  blockingIssueCount: number;
  approvalBlockerCount: number;
  preflightBlockerCount: number;
  bootstrap: boolean;
}

export interface OperatorPlanFact {
  id?: string;
  key: string;
  kind?: string;
  status?: string;
  expected?: string;
  actual?: string;
  checksum?: string;
  message?: string;
  blocking: boolean;
  order: number;
}

export interface OperatorPlanDetail {
  plan: OperatorPlanRow;
  productReleaseChecksum: string;
  targetConfigChecksum: string;
  effectivePolicyChecksum: string;
  subscriberSetChecksum: string;
  graphChecksum: string;
  changeChecksum: string;
  baselineChecksum: string;
  providerResolutionChecksum: string;
  migrationChecksum: string;
  riskChecksum: string;
  approvalChecksum: string;
  windowChecksum: string;
  adapterChecksum: string;
  intentChecksum: string;
  targets: OperatorPlanFact[];
  baselines: OperatorPlanFact[];
  config: OperatorPlanFact[];
  requirements: OperatorPlanFact[];
  migrations: OperatorPlanFact[];
  changes: OperatorPlanFact[];
  risks: OperatorPlanFact[];
  approvals: OperatorPlanFact[];
  windows: OperatorPlanFact[];
  adapters: OperatorPlanFact[];
  steps: OperatorPlanFact[];
  edges: OperatorPlanFact[];
  issues: OperatorPlanFact[];
  intentBlockers: OperatorPlanFact[];
  evidence: OperatorEvidenceRef[];
}

export interface OperatorPlanCompare {
  left: OperatorPlanRow;
  right: OperatorPlanRow;
  changes: OperatorPlanFact[];
}

export interface OperatorCampaignRow {
  id: string;
  createdAt: string;
  draftId: string;
  revisionId?: string;
  runId?: string;
  name: string;
  status: string;
  canonicalChecksum: string;
  waveCount: number;
  memberCount: number;
  pendingCount: number;
  runningCount: number;
  succeededCount: number;
  failedCount: number;
  blockedCount: number;
}

export interface OperatorCampaignWave {
  id: string;
  order: number;
  name: string;
  status: string;
  bakeSeconds: number;
  maximumConcurrency: number;
  memberCount: number;
  succeededCount: number;
  failedCount: number;
}

export interface OperatorCampaignMember {
  id: string;
  memberRunId?: string;
  deploymentPlanId: string;
  deploymentUnitId: string;
  waveOrder: number;
  memberOrder: number;
  status: string;
  planChecksum: string;
}

export interface OperatorCampaignDetail {
  campaign: OperatorCampaignRow;
  runVersion?: number;
  revisionChecksum: string;
  membershipChecksum: string;
  prerequisiteChecksum: string;
  thresholdChecksum: string;
  controlChecksum: string;
  admissionChecksum: string;
  waves: OperatorCampaignWave[];
  members: OperatorCampaignMember[];
  prerequisites: OperatorPlanFact[];
  thresholds: OperatorPlanFact[];
  controls: OperatorPlanFact[];
  uncertaintyBlockers: OperatorPlanFact[];
  admissionBlockers: OperatorPlanFact[];
  evidence: OperatorEvidenceRef[];
}

export interface OperatorExecutionRow {
  id: string;
  createdAt: string;
  campaignId?: string;
  deploymentPlanId: string;
  deploymentTargetId: string;
  taskId: string;
  stepRunId: string;
  stepKey: string;
  attemptNumber: number;
  protocolVersion: string;
  status: string;
  planChecksum: string;
  artifactDigest: string;
  configChecksum: string;
  adapterRevision: string;
  completedAt?: string;
  cancellable: boolean;
  reconciliation: string;
  observation: string;
}

export interface OperatorExecutionDetail {
  execution: OperatorExecutionRow;
  intent?: OperatorPlanFact;
  adapter?: OperatorPlanFact;
  cancellation?: OperatorPlanFact;
  reconciliation?: OperatorPlanFact;
  previousState?: OperatorPlanFact;
  tasks: OperatorPlanFact[];
  steps: OperatorPlanFact[];
  attempts: OperatorPlanFact[];
  observations: OperatorPlanFact[];
  evidence: OperatorEvidenceRef[];
}

export interface OperatorReconciliationRow {
  id: string;
  createdAt: string;
  driftCaseId: string;
  executionId?: string;
  deploymentPlanId?: string;
  environmentId: string;
  deploymentTargetId: string;
  component: string;
  drift: string;
  status: string;
  outcome: string;
  observedAt?: string;
  evidenceChecksum: string;
}

export interface OperatorReconciliationDetail {
  reconciliation: OperatorReconciliationRow;
  desiredState?: OperatorPlanFact;
  observation?: OperatorPlanFact;
  decision?: OperatorPlanFact;
  evidence: OperatorEvidenceRef[];
}

export interface OperatorAuditRow {
  id: string;
  createdAt: string;
  sequence: number;
  action: string;
  subjectType: string;
  subjectId: string;
  actorUserAccountId?: string;
  outcome: string;
  correlationCount: number;
  payloadChecksum: string;
}

export interface OperatorAuditCorrelation {
  kind: string;
  id: string;
}

export interface OperatorAuditDetail {
  event: OperatorAuditRow;
  correlations: OperatorAuditCorrelation[];
  payload?: unknown;
  evidence: OperatorEvidenceRef[];
}

export interface OperatorReleaseDetailResponse {
  detail: OperatorReleaseDetail;
}

export interface OperatorReleaseCompareResponse {
  comparison: OperatorReleaseCompare;
}

export interface OperatorPlanDetailResponse {
  detail: OperatorPlanDetail;
}

export interface OperatorPlanCompareResponse {
  comparison: OperatorPlanCompare;
}

export interface OperatorCampaignDetailResponse {
  detail: OperatorCampaignDetail;
}

export interface OperatorExecutionDetailResponse {
  detail: OperatorExecutionDetail;
}

export interface OperatorReconciliationDetailResponse {
  detail: OperatorReconciliationDetail;
}

export interface OperatorAuditDetailResponse {
  detail: OperatorAuditDetail;
}

export interface OperatorApprovalFilters extends OperatorPageRequest {
  state?: string | null;
}

export interface OperatorApprovalRequirement {
  id: string;
  ruleKey: string;
  policyVersionId: string;
  authorityKind: string;
  authorityId: string;
  principalGroupId: string;
  quorum: number;
  separationConstraints: unknown[];
  sortOrder: number;
}

export interface OperatorApprovalDecision {
  id: string;
  createdAt: string;
  approvalRequestId: string;
  approvalRequirementId: string;
  decision: 'APPROVE' | 'REJECT';
  comment: string;
  actorUserAccountId: string;
  requestRevision: number;
  idempotencyKey: string;
}

export interface OperatorApprovalRequest {
  id: string;
  createdAt: string;
  updatedAt: string;
  subjectType: string;
  subjectId: string;
  subjectRevision: number;
  subjectChecksum: string;
  effectivePolicyChecksum: string;
  subscriberSetChecksum: string;
  requesterUserAccountId: string;
  expiresAt: string;
  state: string;
  revision: number;
  invalidationReason?: string;
  invalidatedAt?: string;
  resolvedAt?: string;
  requirements: OperatorApprovalRequirement[];
  decisions: OperatorApprovalDecision[];
}

export interface OperatorApprovalPage {
  items: OperatorApprovalRequest[];
  nextCursor?: string;
}

export interface OperatorApprovalDecisionRequest {
  approvalRequirementId: string;
  decision: 'APPROVE' | 'REJECT';
  comment: string;
  expectedRequestRevision: number;
  idempotencyKey?: string;
}

export type OperatorCampaignControlAction = 'pause' | 'resume' | 'cancel';

export interface OperatorCampaignControlRequest {
  expectedVersion: number;
  reason: string;
  requestId?: string;
}

export type OperatorCampaignMemberControlAction = 'exclude' | 'retry';

export interface OperatorCampaignMemberControlRequest extends OperatorCampaignControlRequest {
  memberRunId: string;
  protocolVersion?: 'v1' | 'v2';
}

export interface OperatorCampaignControlResult {
  requestId: string;
  status: string;
  run: OperatorCampaignRun;
  pausePending: boolean;
  reconciliationRequired: boolean;
  duplicate: boolean;
}

export interface OperatorCampaignExclusion {
  id: string;
  campaignRunId: string;
  memberRunId: string;
  reason: string;
  visibleIncomplete: boolean;
  driftReason: string;
  excludedAt: string;
}

export interface OperatorCampaignRun {
  id: string;
  createdAt: string;
  updatedAt: string;
  campaignRevisionId: string;
  state: string;
  version: number;
  currentWaveOrder: number;
  currentMemberOrder: number;
  admissionsBlocked: boolean;
  resumeState?: string;
  pauseRequested: boolean;
  reconciliationRequired: boolean;
  fencingToken: number;
  leaseExpiresAt?: string;
}

export interface OperatorExecutionCancelRequest {
  reason: string;
  idempotencyKey?: string;
}

export interface OperatorExecutionStatusRequest {
  reason: string;
  expiresInSeconds: number;
  idempotencyKey?: string;
}

export interface OperatorExecutionStatusQuery {
  id: string;
  createdAt: string;
  organizationId: string;
  executionId: string;
  executionAttemptId: string;
  requestedBy: string;
  idempotencyKey: string;
  reason: string;
  status: string;
  expiresAt: string;
  requestedTtlSeconds: number;
  reportedAt?: string;
}

export interface OperatorExecutionStatusResponse {
  query: OperatorExecutionStatusQuery;
}

export interface OperatorReconciliationDecisionRequest {
  action: 'RESTORE_DESIRED' | 'CREATE_PLAN' | 'ACCEPT_DEVIATION' | 'CLOSE_WITH_EVIDENCE';
  reason: string;
  deploymentPlanId?: string;
  outcomeObservationId?: string;
  acceptedUntil?: string;
}

export interface OperatorPlanApprovalRequest {
  expiresAt: string;
}

export interface OperatorPreviousStatePlanRequest {
  successfulDeploymentPlanId: string;
  reason: string;
}

export interface OperatorRegistryImportPreviewRequest {
  sourceKind: string;
  toolName: string;
  toolVersion: string;
  sourceCommit?: string;
  parameters: Record<string, string>;
  evidenceReference: string;
  evidenceChecksum: string;
  sourcePlacements?: OperatorRegistryImportSourcePlacement[];
  roots: OperatorRegistryImportCandidateRoot[];
}

export interface OperatorRegistryImportSourcePlacement {
  rootKey: string;
  physicalName: string;
}

export type OperatorRegistryDeliveryModel = 'dedicated' | 'shared' | 'external';
export type OperatorRegistryImportClassification =
  | 'standard'
  | 'shared'
  | 'external'
  | 'observe_only'
  | 'ignored'
  | 'needs_decision';

export interface OperatorRegistryImportCandidatePlacement {
  componentKey: string;
  physicalName: string;
  configNamespace?: string;
  databaseBoundary?: string;
  healthAdapter?: string;
  renamedFrom?: string;
}

export interface OperatorRegistryImportCandidateRoot {
  key: string;
  name: string;
  deliveryModel: OperatorRegistryDeliveryModel;
  classification: OperatorRegistryImportClassification;
  customerOrganizationId?: string;
  deploymentTargetId: string;
  environmentId: string;
  subscriberCustomerOrganizationIds?: string[];
  physicalIdentity: string;
  placements: OperatorRegistryImportCandidatePlacement[];
}

export interface OperatorRegistryImportCounts {
  discoveredRoots: number;
  classifiedRoots: number;
  discoveredPlacements: number;
  omittedPlacements: number;
  creates: number;
  updates: number;
  retirements: number;
  conflicts: number;
}

export interface OperatorRegistryImportChange {
  kind: string;
  rootKey: string;
  placementKey?: string;
  physicalName?: string;
  message: string;
}

export interface OperatorRegistryImportDiff {
  creates: OperatorRegistryImportChange[];
  updates: OperatorRegistryImportChange[];
  retirements: OperatorRegistryImportChange[];
  conflicts: OperatorRegistryImportChange[];
}

export interface OperatorRegistryValidationIssue {
  code: string;
  field: string;
  message: string;
}

export interface OperatorRegistryImportPreview {
  id: string;
  previewChecksum: string;
  counts: OperatorRegistryImportCounts;
  diff: OperatorRegistryImportDiff;
  omissions: string[];
  diagnostics: OperatorRegistryValidationIssue[];
  diagnosticsTruncated: boolean;
  roots: OperatorRegistryImportCandidateRoot[];
}

export interface OperatorRegistryImportDecisionRequest {
  rootKey: string;
  classification: OperatorRegistryImportClassification;
}

export interface OperatorRegistryImportResult {
  id: string;
  previewChecksum: string;
  state: string;
  applied: boolean;
  counts: OperatorRegistryImportCounts;
  checkpoint: number;
}

export interface OperatorRegistryCoverage {
  importId: string;
  discoveredRoots: number;
  classifiedRoots: number;
  actionableManagedRoots: number;
  observeOnlyRoots: number;
  externalRoots: number;
  ignoredRoots: number;
  unresolvedRoots: number;
  discoveredPlacements: number;
  services: number;
  omittedPlacements: number;
  omissions: string[];
  complete: boolean;
}

export interface OperatorControlPlaneAuditListRequest {
  afterSequence?: number | null;
  limit?: number | null;
}

export interface OperatorControlPlaneAuditEvent {
  id: string;
  sequence: number;
  eventType: string;
  actorId?: string;
  outcome: string;
  releaseId?: string;
  componentReleaseId?: string;
  productReleaseId?: string;
  targetConfigId?: string;
  deploymentPlanId?: string;
  deploymentPolicyId?: string;
  deploymentPolicyVersionId?: string;
  approvalId?: string;
  maintenanceCalendarId?: string;
  deploymentFreezeId?: string;
  admissionDecisionId?: string;
  emergencyOverrideId?: string;
  campaignDraftId?: string;
  campaignRevisionId?: string;
  campaignRunId?: string;
  campaignWaveDefinitionId?: string;
  campaignWaveRunId?: string;
  campaignMemberId?: string;
  campaignMemberRunId?: string;
  campaignControlRequestId?: string;
  campaignExclusionId?: string;
  campaignPrerequisiteEvaluationId?: string;
  campaignThresholdEvaluationId?: string;
  executionId?: string;
  executionAttemptId?: string;
  adapterRevisionId?: string;
  desiredStateId?: string;
  observationId?: string;
  driftCaseId?: string;
  reconciliationId?: string;
  deploymentTargetId?: string;
  environmentId?: string;
  customerOrganizationId?: string;
  deploymentUnitId?: string;
  componentId?: string;
  taskId?: string;
  stepRunId?: string;
  auditExportSinkId?: string;
  auditExportAttemptId?: string;
  releaseChecksum?: string;
  componentReleaseChecksum?: string;
  productReleaseChecksum?: string;
  artifactDigest?: string;
  manifestDigest?: string;
  targetConfigChecksum?: string;
  deploymentPlanChecksum?: string;
  policyChecksum?: string;
  approvalChecksum?: string;
  calendarChecksum?: string;
  admissionChecksum?: string;
  campaignRevisionChecksum?: string;
  campaignControlChecksum?: string;
  executionChecksum?: string;
  desiredStateChecksum?: string;
  observationChecksum?: string;
  driftChecksum?: string;
  reconciliationChecksum?: string;
  auditExportConfigChecksum?: string;
  payload?: unknown;
  payloadRedacted: boolean;
  payloadTruncated: boolean;
  createdAt: string;
}

export interface OperatorControlPlaneAuditEventPage {
  items: OperatorControlPlaneAuditEvent[];
  nextAfterSequence?: number;
}

export interface OperatorEvidenceBundle {
  version: string;
  deploymentPlanId: string;
  events: OperatorControlPlaneAuditEvent[];
  checksum: string;
}

export type OperatorAuditExportSinkKind = 'webhook' | 'object_store' | 'siem';

export interface OperatorCreateAuditExportSinkRequest {
  name: string;
  kind: OperatorAuditExportSinkKind;
  endpointReference: string;
  configChecksum: string;
  enabled?: boolean;
}

export interface OperatorAuditExportSink {
  id: string;
  name: string;
  kind: OperatorAuditExportSinkKind;
  endpointReference: string;
  configChecksum: string;
  enabled: boolean;
  lastSuccessAt?: string;
  lastFailureAt?: string;
  consecutiveFailures: number;
  createdAt: string;
  updatedAt: string;
}

export interface OperatorAuditExportStatus {
  sink: OperatorAuditExportSink;
  lastExportedSequence: number;
  lastExportedEventId?: string;
  latestSequence: number;
  checkpointLag: number;
  alert: boolean;
  lastAttemptStatus?: string;
  lastAttemptError?: string;
  lastAttemptCompletedAt?: string;
}

export interface OperatorControlPlaneEnrollment {
  id: string;
  createdAt: string;
  scope: {kind: string; id: string};
  enabled: boolean;
  effectiveFrom: string;
  effectiveUntil?: string;
  actorUserAccountId: string;
  reason: string;
  revision: number;
}

export interface OperatorControlPlaneEnrollmentPage {
  enrollments: OperatorControlPlaneEnrollment[];
  nextCursor?: string;
}

export interface OperatorSetupReadinessRequest {
  importId: string;
  deploymentUnitId: string;
}

export interface OperatorSetupReadiness {
  operatorControlPlaneEnabled: boolean;
  executorProtocolEnabled: boolean;
  hasEnabledEnrollment: boolean;
  hasTargetConfigSnapshot: boolean;
  registryCoverageComplete: boolean;
  ready: boolean;
}

export interface OperatorCapabilityRequirement {
  name: string;
  range: string;
  resolutionStage: string;
  allowedModes: string[];
}

export interface OperatorCreateProductReleaseRequest {
  schema?: string;
  applicationId: string;
  channelId: string;
  product: string;
  version: string;
  dependencyPolicyVersion: string;
  releaseNotes: string;
  requiredPlatforms: string[];
  components: Array<{componentReleaseId: string; componentReleaseChecksum: string}>;
  requirements: OperatorCapabilityRequirement[];
}

export interface OperatorProductRelease {
  id: string;
  createdAt: string;
  updatedAt: string;
  applicationId: string;
  channelId: string;
  status: string;
  canonicalChecksum: string;
  graphChecksum: string;
  publishedByUserAccountId?: string;
  publishedAt?: string;
  manifest: OperatorProductReleaseManifest;
}

export interface OperatorProductReleaseManifest {
  schema: string;
  product: string;
  version: string;
  dependencyPolicyVersion: string;
  releaseNotes: string;
  requiredPlatforms: string[];
  components: OperatorProductReleaseComponent[];
  requirements: OperatorCapabilityRequirement[];
}

export interface OperatorProductReleaseComponent {
  componentReleaseId: string;
  componentReleaseChecksum: string;
  componentKey: string;
  version: string;
}

export interface OperatorProductReleaseValidation {
  valid: boolean;
  issues: Array<{field: string; rule: string; message: string; path?: string[]}>;
}

export interface OperatorCreatePlanDraftRequest {
  productReleaseId: string;
  deploymentUnitId: string;
  environmentAssignmentId: string;
  targetConfigSnapshotId: string;
  protocolVersion: 'v1' | 'v2';
  supersedesDeploymentPlanId?: string;
  supersedeReason?: string;
}

export interface OperatorUpdatePlanDraftRequest extends OperatorCreatePlanDraftRequest {
  expectedRevision: number;
}

export interface OperatorPlanDraft {
  id: string;
  createdAt: string;
  updatedAt: string;
  createdByUserAccountId: string;
  updatedByUserAccountId: string;
  revision: number;
  productReleaseId: string;
  deploymentUnitId: string;
  environmentAssignmentId: string;
  targetConfigSnapshotId: string;
  protocolVersion: string;
  supersedesDeploymentPlanId?: string;
  supersedeReason?: string;
  previewChecksum?: string;
  publishedDeploymentPlanId?: string;
  publishedDeploymentPlanStatus?: string;
}

export interface OperatorPublishPlanDraftRequest {
  expectedRevision: number;
  expectedPreviewChecksum: string;
}

export interface OperatorPlanDraftValidation {
  draft: OperatorPlanDraft;
  resolutions: unknown[];
  graph: unknown;
  baselines: unknown[];
  changes: unknown[];
  risks: unknown[];
  bootstrap: boolean;
  issues: unknown[];
  previewChecksum?: string;
}

export interface OperatorControlPlaneError {
  status: number;
  code:
    | 'NETWORK_ERROR'
    | 'INVALID_REQUEST'
    | 'UNAUTHENTICATED'
    | 'FORBIDDEN'
    | 'NOT_FOUND'
    | 'CONFLICT'
    | 'VALIDATION_FAILED'
    | 'RATE_LIMITED'
    | 'SERVER_ERROR'
    | 'REQUEST_FAILED';
  message: string;
  retryable: boolean;
  requestId?: string;
}
