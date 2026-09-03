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
  customerOrganizationId?: string | null;
  applicationId?: string | null;
  deploymentUnitId?: string | null;
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

export interface OperatorDeploymentRegistryPage<T> {
  items: T[];
  nextCursor?: string;
}

export interface OperatorDeploymentUnit {
  id: string;
  createdAt: string;
  updatedAt: string;
  deploymentScopeId: string;
  targetEnvironmentAssignmentId: string;
  deploymentTargetId: string;
  key: string;
  name: string;
  physicalIdentity: string;
  managementState: string;
  subscriberSetChecksum: string;
  retiredAt?: string;
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
  observedEvidenceChecksum: string;
  observedArtifactDigest: string;
  observedConfigChecksum: string;
  observedSchemaVersion: string;
  observedCapabilityChecksum: string;
  observedPlatform: string;
  observedHealth: string;
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
  application: string;
  clients: OperatorReleaseClient[];
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

export interface OperatorReleaseClient {
  id: string;
  name: string;
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
  consumerComponent: string;
  providerComponent?: string;
  capability: string;
  versionRange: string;
  providerVersion?: string;
  providerArtifacts: OperatorReleaseProviderArtifactIdentity[];
  resolutionStage: string;
  allowedModes: string[];
  ordering?: string;
}

export interface OperatorReleaseProviderArtifactIdentity {
  artifactKey: string;
  artifactType: string;
  manifestDigest: string;
  platform: string;
  platformDigest: string;
}

export interface OperatorReleaseSourceBuildProof {
  component: string;
  schema: string;
  declaredRepository: string;
  declaredRequestedRef: string;
  declaredSourceCommit: string;
  declaredBuilderId: string;
  declaredBuildId: string;
  verifiedSourceUri?: string;
  verifiedSourceCommit?: string;
  verifiedBuilderId?: string;
  verifiedBuildId?: string;
  verifiedBuildType?: string;
  provenanceReference?: string;
  provenanceDigest?: string;
  verificationMode?: string;
  trustRootId?: string;
  keyId?: string;
  keyFingerprint?: string;
  sbomReference?: string;
  sbomDigest?: string;
  verificationState: string;
}

export interface OperatorReleaseChange {
  category: 'code' | 'config' | 'migration' | 'dependency' | string;
  component: string;
  summary: string;
  reference?: string;
}

export interface OperatorReleaseSkippedRelease {
  component: string;
  releaseId: string;
  version: string;
  sourceRevision: string;
  summary: string;
}

export interface OperatorReleaseChangeContext {
  deploymentPlanId?: string;
  deploymentUnitId?: string;
  state: 'READY' | 'CONTEXT_REQUIRED' | 'NOT_FOUND' | 'BASELINE_UNVERIFIED' | 'DIVERGENT_HISTORY' | string;
  message?: string;
}

export interface OperatorReleaseDetail {
  release: OperatorReleaseRow;
  artifacts: OperatorReleaseArtifact[];
  componentPins: OperatorReleaseComponentPin[];
  graphEdges: OperatorReleaseGraphEdge[];
  sourceBuildProof: OperatorReleaseSourceBuildProof[];
  changelog: OperatorReleaseChange[];
  skippedReleases: OperatorReleaseSkippedRelease[];
  changeContext: OperatorReleaseChangeContext;
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
  keyId?: string;
  artifactDigest?: string;
  configChecksum?: string;
  platform?: string;
  schemaVersion?: string;
  capabilityChecksum?: string;
  health?: string;
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
  requirementResolutions: OperatorRequirementResolution[];
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

export type OperatorReviewAdmissionDecisionValue = 'GO' | 'NO_GO';
export type OperatorReviewAdmissionMaterialState = 'MISSING' | 'GO' | 'NO_GO' | 'STALE';

export interface OperatorReviewAdmissionDecision {
  id: string;
  createdAt: string;
  organizationId: string;
  deploymentPlanId: string;
  planRevision: number;
  planChecksum: string;
  reviewMaterialChecksum: string;
  observedStateChecksum: string;
  decision: OperatorReviewAdmissionDecisionValue;
  reason: string;
  actorUserAccountId: string;
  expiresAt: string;
  supersedesDecisionId?: string;
  revokesDecisionId?: string;
  authorizationEvidence: string;
  canonicalChecksum: string;
  idempotencyKey: string;
}

export interface OperatorReviewAdmissionMaterial {
  deploymentPlanId: string;
  planRevision: number;
  planChecksum: string;
  observedStateChecksum: string;
  reviewMaterialChecksum: string;
  reviewMaterialValid: boolean;
  admissionValid: boolean;
  admissionEvaluationId?: string;
  admissionDecision?: string;
  admissionDecisionChecksum?: string;
  state: OperatorReviewAdmissionMaterialState;
  canDecide: boolean;
  blockers: string[];
  latestDecision?: OperatorReviewAdmissionDecision;
}

export interface OperatorReviewAdmissionDecisionRequest {
  expectedPlanChecksum: string;
  reviewMaterialChecksum: string;
  observedStateChecksum: string;
  decision: OperatorReviewAdmissionDecisionValue;
  reason: string;
  expiresAt: string;
  supersedesDecisionId?: string;
  revokesDecisionId?: string;
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

export interface OperatorCampaignCoordinationSummary {
  admissionsBlocked: boolean;
  pausePending: boolean;
  noNewExposure: boolean;
  inFlightMemberCount: number;
  reconciliationRequired: boolean;
  schedulerFenceGeneration: number;
  schedulerLeaseStatus: string;
  schedulerLeaseExpiresAt?: string;
  activeLockCount: number;
  unreleasedLockCount: number;
  activeLeaseCount: number;
  unreleasedLeaseCount: number;
  zeroLockClosure: boolean;
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
  coordination: OperatorCampaignCoordinationSummary;
  evidence: OperatorEvidenceRef[];
}

export interface OperatorCampaignMembershipRequest {
  planIds: string[];
  tagQuery?: string;
}

export interface OperatorCampaignRiskPolicy {
  maximumConcurrency: number;
  failureToleranceBasisPoints: number;
  minimumHealthyBasisPoints: number;
}

export interface OperatorCampaignWaveRequest {
  order: number;
  name: string;
  planIds: string[];
  bakeSeconds: number;
  maximumConcurrency: number;
}

export interface OperatorCampaignPrerequisiteRequest {
  downstreamPlanId: string;
  upstreamPlanId: string;
  upstreamStepKey: string;
  providerPlacementId: string;
  expectedRuntimeStateChecksum: string;
}

export interface OperatorCreateCampaignDraftRequest {
  name: string;
  description: string;
  membership: OperatorCampaignMembershipRequest;
  waves: OperatorCampaignWaveRequest[];
  prerequisites: OperatorCampaignPrerequisiteRequest[];
  riskPolicy: OperatorCampaignRiskPolicy;
}

export interface OperatorUpdateCampaignDraftRequest extends OperatorCreateCampaignDraftRequest {
  expectedRevision: number;
}

export interface OperatorCampaignDraft extends OperatorCreateCampaignDraftRequest {
  id: string;
  createdAt: string;
  updatedAt: string;
  organizationId: string;
  revision: number;
  lastPublishedRevisionId?: string;
}

export interface OperatorCampaignValidationIssue {
  code: string;
  field: string;
  message: string;
}

export interface OperatorCampaignDraftValidation {
  valid: boolean;
  issues: OperatorCampaignValidationIssue[];
}

export interface OperatorCampaignRevisionWave {
  order: number;
  name: string;
  bakeSeconds: number;
  maximumConcurrency: number;
}

export interface OperatorCampaignRevisionMember {
  planId: string;
  deploymentUnitId: string;
  planChecksum: string;
  effectivePolicyChecksum: string;
  approvalRequestId: string;
  approvalRequestRevision: number;
  approvalChecksum: string;
  calendarVersionIds: string[];
  calendarChecksums: string[];
  admissionEvaluationId: string;
  admissionChecksum: string;
  waveOrder: number;
  memberOrder: number;
}

export interface OperatorCampaignRevisionPrerequisite extends OperatorCampaignPrerequisiteRequest {
  providerDeploymentUnitId: string;
  providerComponentInstanceId: string;
}

export interface OperatorCampaignRevision {
  id: string;
  publishedAt: string;
  organizationId: string;
  campaignDraftId: string;
  revisionNumber: number;
  sourceDraftRevision: number;
  name: string;
  description: string;
  membershipTagQuery?: string;
  riskPolicy: OperatorCampaignRiskPolicy;
  canonicalChecksum: string;
  publishedByUserAccountId: string;
  waves: OperatorCampaignRevisionWave[];
  members: OperatorCampaignRevisionMember[];
  prerequisites: OperatorCampaignRevisionPrerequisite[];
}

export type OperatorCampaignRunState =
  | 'DRAFT'
  | 'VALIDATED'
  | 'AWAITING_APPROVAL'
  | 'SCHEDULED'
  | 'RUNNING'
  | 'PAUSED'
  | 'FAILED'
  | 'COMPLETED'
  | 'CANCELED';

export interface OperatorCampaignTransitionRequest {
  expectedVersion: number;
  to: OperatorCampaignRunState;
  reason: string;
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
  fenceGeneration?: number;
  fenceResourceKey?: string;
  idempotencyKey?: string;
}

export interface OperatorExecutionAttemptFact {
  id: string;
  stepKey: string;
  status: string;
  attemptNumber: number;
  planChecksum: string;
  artifactDigest: string;
  configChecksum: string;
  fenceGeneration: number;
  fenceResourceKey: string;
  idempotencyKey?: string;
  message?: string;
  blocking: boolean;
}

export interface OperatorExecutionLockFact {
  id: string;
  resourceType: string;
  resourceKey: string;
  concurrencyPolicy: string;
  status: 'WAITING' | 'CONFLICTED' | 'ACQUIRED' | 'RELEASED' | string;
  createdAt: string;
  acquiredAt?: string;
  releasedAt?: string;
  currentConflict: boolean;
  releaseReason?: string;
}

export interface OperatorExecutionLeaseFact {
  id: string;
  executorType: string;
  attempt: number;
  status: 'ACTIVE' | 'EXPIRED' | 'RELEASED' | string;
  leasedAt: string;
  expiresAt: string;
  heartbeatAt: string;
  releasedAt?: string;
  releaseReason?: string;
}

export interface OperatorExecutionCoordinationSummary {
  inFlight: boolean;
  activeLockCount: number;
  unreleasedLockCount: number;
  activeLeaseCount: number;
  unreleasedLeaseCount: number;
  fenceStatus: string;
  fenceGeneration: number;
  fenceLeaseExpiresAt?: string;
  fenceReleasedAt?: string;
  timedOut: boolean;
  reconciliationRequired: boolean;
  zeroLockClosure: boolean;
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
  attempts: OperatorExecutionAttemptFact[];
  locks: OperatorExecutionLockFact[];
  leases: OperatorExecutionLeaseFact[];
  coordination: OperatorExecutionCoordinationSummary;
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
  separationConstraints: string[];
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
  state: OperatorCampaignRunState;
  version: number;
  currentWaveOrder: number;
  currentMemberOrder: number;
  admissionsBlocked: boolean;
  resumeState?: OperatorCampaignRunState;
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

export interface OperatorBaselineAdoptionComponentRequest {
  componentInstanceId: string;
  componentKey: string;
  componentReleaseId: string;
  componentReleaseChecksum: string;
  sourceCommit: string;
  buildId: string;
  provenanceVerificationId: string;
  provenanceEvidenceDigest: string;
  provenancePolicyChecksum: string;
  artifactDigest: string;
  platform: string;
  configChecksum: string;
  schemaVersion: string;
  capabilityChecksum: string;
  topologyChecksum: string;
  observationId: string;
  observerId: string;
  observationEvidenceChecksum: string;
  observationStateChecksum: string;
  observationRuntimeStateChecksum: string;
}

export interface OperatorBaselineAdoptionRequest {
  reason: string;
  expectedPlanChecksum: string;
  expectedProductReleaseChecksum: string;
  expectedTargetConfigChecksum: string;
  components: OperatorBaselineAdoptionComponentRequest[];
}

export interface OperatorBaselineAdoption {
  id: string;
  createdAt: string;
  deploymentPlanId: string;
  status: string;
  deploymentPerformed: boolean;
  taskCount: number;
  lockCount: number;
  executionCount: number;
  requestChecksum: string;
  outcomeChecksum: string;
  components: Array<OperatorBaselineAdoptionComponentRequest & {id: string; applicationVersion: string}>;
}

export type OperatorAdmissionDecision = 'ADMIT' | 'WAIT' | 'BLOCK';

export interface OperatorAdmissionEvaluation {
  id: string;
  createdAt: string;
  deploymentPlanId: string;
  planRevision: number;
  planChecksum: string;
  decision: OperatorAdmissionDecision;
  reasonCodes: string[];
  evaluatedAt: string;
  materialChecksum: string;
  decisionChecksum: string;
  schedulerIdempotencyKey: string;
}

export interface OperatorProtectedHistoryArtifactRequest {
  customerOrganizationIds: string[];
  deploymentTargetIds: string[];
  reviewerUserAccountId: string;
}

export interface OperatorProtectedHistoryArtifact {
  id: string;
  schema: string;
  sourceSchemaVersion: number;
  scope: {
    organizationId: string;
    customerOrganizationIds: string[];
    deploymentTargetIds: string[];
  };
  artifactId: string;
  recordsRoot: string;
  recordCount: number;
  objectReference: string;
  mediaType: string;
  byteLength: number;
  contentChecksum: string;
  capturedAt: string;
  issuerUserAccountId: string;
  reviewerUserAccountId: string;
  governanceExceptionKey?: string;
  governanceExceptionReference?: string;
  retentionChecksum: string;
  auditEventId: string;
  auditEventSequence: number;
  auditBindingChecksum: string;
  idempotencyKey: string;
  requestChecksum: string;
  createdAt: string;
}

export interface OperatorProtectedHistoryArtifactVerification {
  protectedHistoryArtifactId: string;
  objectReference: string;
  mediaType: string;
  byteLength: number;
  contentChecksum: string;
  verifiedAt: string;
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
  dependencyPolicyChecksum: string;
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
  graph: OperatorProductReleaseGraph;
}

export interface OperatorProductReleaseManifest {
  schema: string;
  product: string;
  version: string;
  dependencyPolicyVersion: string;
  dependencyPolicyChecksum: string;
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
  platforms: string[];
  artifacts: OperatorProductReleaseArtifact[];
  provides: OperatorCapabilityDeclaration[];
  requires: OperatorCapabilityRequirement[];
  migrations: OperatorMigrationDeclaration[];
  migrationContracts: OperatorMigrationContract[];
}

export interface OperatorProductReleaseArtifact {
  key: string;
  type: string;
  mediaType: string;
  digest: string;
  platforms: Array<{platform: string; digest: string}>;
}

export interface OperatorCapabilityDeclaration {
  name: string;
  version: string;
}

export interface OperatorMigrationDeclaration {
  key: string;
  type: string;
  order: number;
  compatibility: string;
  failurePolicy: string;
  description: string;
}

export interface OperatorMigrationProbe {
  name: string;
  reference: string;
  expectedChecksum: string;
}

export interface OperatorMigrationContract {
  id: string;
  checksum: string;
  componentKey: string;
  databaseResourceKey: string;
  expectedSourceVersion: string;
  expectedSourceChecksum: string;
  resultingVersion: string;
  resultingSchemaChecksum: string;
  phase: string;
  dependsOn?: string[];
  lockType: string;
  lockTimeoutSeconds: number;
  operationalImpact: string;
  backupRequired: boolean;
  backupVerifier?: string;
  preconditionProbes: OperatorMigrationProbe[];
  postconditionProbes: OperatorMigrationProbe[];
  retryClass: string;
  idempotencyKey?: string;
  reversibility: string;
  previousApplicationCompatibility: string;
  recoveryProcedureReference: string;
  requiresForwardFix: boolean;
  adapterType?: string;
  artifactDigest?: string;
  evidenceRetentionDays: number;
}

export interface OperatorProductReleaseGraph {
  nodes: OperatorProductReleaseGraphNode[];
  edges: OperatorProductReleaseGraphEdge[];
  topologicalOrder: string[];
  checksum: string;
}

export interface OperatorProductReleaseGraphNode {
  key: string;
  kind: string;
  componentReleaseId?: string;
  componentKey?: string;
  version?: string;
  capability?: string;
  versionRange?: string;
  resolutionStage?: string;
  allowedModes?: string[];
  unresolved?: boolean;
}

export interface OperatorProductReleaseGraphEdge {
  key: string;
  from: string;
  to: string;
  capability: string;
  versionRange: string;
  providerVersion?: string;
  resolutionStage: string;
  allowedModes?: string[];
  ordering?: string;
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

export interface OperatorRequirementResolution {
  requirementKey: string;
  consumerKey: string;
  capability: string;
  versionRange: string;
  mode: string;
  providerReleaseId?: string;
  observationId?: string;
  activeDesiredRevisionId?: string;
  observedComponentStateId?: string;
  providerVersion: string;
  providerPlatform: string;
  providerReleaseChecksum?: string;
  provenanceBindingChecksum?: string;
  providerDeploymentUnitId?: string;
  componentInstanceId?: string;
  subscriberSetChecksum?: string;
  expectedStateVersion: number;
  expectedStateChecksum: string;
  providerEvidenceVersion?: number;
  observationFreshUntil?: string;
  observationTrusted?: boolean;
  observationCurrent?: boolean;
  providerApprovalRequestId?: string;
  providerApprovalChecksum?: string;
  contractProbeObservationId?: string;
  contractProbeEvidenceChecksum?: string;
  bindingChecksum: string;
  sortOrder: number;
  v1Compatible?: boolean;
}

export interface OperatorTargetPlanStep {
  stepKey: string;
  name: string;
  kind: string;
  componentKey?: string;
  componentReleaseId?: string;
  componentInstanceId?: string;
  actionType: string;
  actionName: string;
  executionLocation: string;
  inputBindings: Record<string, unknown>;
  targetLockKey: string;
  databaseLockKey?: string;
  timeoutSeconds: number;
  retryClass: string;
  cancellationBehavior: string;
  expectedInputChecksum: string;
  observationRequirement: string;
  v1Compatible: boolean;
  sortOrder: number;
}

export interface OperatorTargetPlanEdge {
  key: string;
  fromStepKey: string;
  toStepKey: string;
}

export interface OperatorTargetPlanGraph {
  steps: OperatorTargetPlanStep[];
  edges: OperatorTargetPlanEdge[];
  topologicalOrder: string[];
  checksum: string;
}

export interface OperatorPlanBaseline {
  componentInstanceId: string;
  componentKey: string;
  sourceDeploymentPlanId?: string;
  externalExecutionId?: string;
  activeDesiredRevisionId?: string;
  observedComponentStateId?: string;
  observationId?: string;
  observedAt?: string;
  desiredRevision: number;
  desiredChecksum: string;
  observationChecksum: string;
  releaseBundleId?: string;
  version: string;
  image: string;
  platform: string;
  targetConfigSnapshotId?: string;
  configChecksum: string;
  providerBindingChecksum: string;
  schemaState: string;
  schemaChecksum: string;
  topologyChecksum: string;
  projection: string;
  authorizesV2Execution: boolean;
  bootstrap: boolean;
  canonicalChecksum: string;
  sortOrder: number;
}

export interface OperatorPlanReleaseNote {
  releaseBundleId: string;
  version: string;
  publishedAt: string;
  sourceRevision: string;
  summary: string;
}

export interface OperatorPlanChange {
  componentInstanceId?: string;
  componentKey: string;
  kind: string;
  before: string;
  after: string;
  releaseNotes?: OperatorPlanReleaseNote[];
  forwardOnly: boolean;
  canonicalChecksum: string;
  sortOrder: number;
}

export interface OperatorPlanRisk {
  componentKey: string;
  code: string;
  level: string;
  blocking: boolean;
  message: string;
  canonicalChecksum: string;
  sortOrder: number;
}

export interface OperatorPlanValidationIssue {
  code: string;
  field: string;
  message: string;
}

export interface OperatorPublishPlanDraftRequest {
  expectedRevision: number;
  expectedPreviewChecksum: string;
}

export interface OperatorPlanDraftValidation {
  draft: OperatorPlanDraft;
  resolutions: OperatorRequirementResolution[];
  graph: OperatorTargetPlanGraph;
  baselines: OperatorPlanBaseline[];
  changes: OperatorPlanChange[];
  risks: OperatorPlanRisk[];
  bootstrap: boolean;
  issues: OperatorPlanValidationIssue[];
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
