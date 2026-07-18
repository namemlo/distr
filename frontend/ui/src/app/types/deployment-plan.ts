import {DeploymentType} from '@distr-sh/distr-sdk';
import {ReleaseContract} from './release-bundle';
import {VariableResolutionTraceEntry, VariableType} from './variable-set';

export type DeploymentPlanStatus = 'DRAFT' | 'VALIDATING' | 'BLOCKED' | 'READY' | 'EXPIRED' | 'EXECUTED';
export type DeploymentPlanIssueSeverity = 'blocker' | 'warning';
export type DeploymentPlanVariableStatus = 'resolved' | 'unresolved';

export interface CreateDeploymentPlanRequest {
  releaseBundleId: string;
  environmentId: string;
  targetIds: string[];
}

export interface DeploymentPlanTask {
  id: string;
  deploymentPlanId: string;
  deploymentTargetId: string;
  status: 'QUEUED' | 'RUNNING' | 'SUCCEEDED' | 'FAILED' | 'CANCELED';
}

export interface DeploymentPlan {
  id: string;
  createdAt: string;
  applicationId: string;
  releaseBundleId: string;
  channelId: string;
  environmentId: string;
  processSnapshotId?: string;
  variableSnapshotId?: string;
  releaseContract?: ReleaseContract;
  planSchema: 'distr.deployment-plan/v1' | 'distr.target-deployment-plan/v2';
  draftId?: string;
  deploymentUnitId?: string;
  targetConfigSnapshotId?: string;
  protocolVersion: 'v1' | 'v2';
  supersedesDeploymentPlanId?: string;
  supersedeReason?: string;
  previousStateSourcePlanId?: string;
  status: DeploymentPlanStatus;
  canonicalChecksum: string;
  bootstrap: boolean;
  targets: DeploymentPlanTarget[];
  targetComponents: DeploymentPlanTargetComponent[];
  preflightRuns: DeploymentPreflightRun[];
  steps: DeploymentPlanStep[];
  variables: DeploymentPlanVariable[];
  issues: DeploymentPlanIssue[];
  baselines?: DeploymentPlanBaseline[];
  changes?: DeploymentPlanChangeEntry[];
  risks?: DeploymentPlanRiskEntry[];
}

export type DeploymentPlanBaselineProjection = 'verified_v2' | 'legacy_projection' | 'bootstrap';
export type DeploymentPlanChangeKind =
  | 'bootstrap'
  | 'baseline_authority'
  | 'image'
  | 'config'
  | 'provider'
  | 'schema'
  | 'topology'
  | 'source_notes'
  | 'previous_state'
  | 'planning_limit_exceeded';
export type DeploymentPlanRiskLevel = 'low' | 'medium' | 'high' | 'critical';

export interface DeploymentPlanBaseline {
  id?: string;
  createdAt?: string;
  deploymentPlanId?: string;
  componentInstanceId: string;
  componentKey: string;
  sourceDeploymentPlanId?: string;
  externalExecutionId?: string;
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
  projection: DeploymentPlanBaselineProjection;
  authorizesV2Execution: boolean;
  bootstrap: boolean;
  actorUserAccountId?: string;
  canonicalChecksum: string;
  sortOrder: number;
}

export interface DeploymentPlanReleaseNote {
  releaseBundleId: string;
  version: string;
  publishedAt: string;
  sourceRevision: string;
  summary: string;
}

export interface DeploymentPlanChangeEntry {
  id?: string;
  createdAt?: string;
  deploymentPlanId?: string;
  componentInstanceId?: string;
  componentKey: string;
  kind: DeploymentPlanChangeKind;
  before: string;
  after: string;
  releaseNotes?: DeploymentPlanReleaseNote[];
  forwardOnly: boolean;
  actorUserAccountId?: string;
  canonicalChecksum: string;
  sortOrder: number;
}

export interface DeploymentPlanRiskEntry {
  id?: string;
  createdAt?: string;
  deploymentPlanId?: string;
  componentKey: string;
  code: string;
  level: DeploymentPlanRiskLevel;
  blocking: boolean;
  message: string;
  actorUserAccountId?: string;
  canonicalChecksum: string;
  sortOrder: number;
}

export interface CreatePreviousStateDeploymentPlanRequest {
  successfulDeploymentPlanId: string;
  reason: string;
}

export type DeploymentPreflightStatus = 'PASSED' | 'FAILED';
export type DeploymentPreflightCheckStatus = 'PASSED' | 'FAILED';

export interface DeploymentPreflightRun {
  id: string;
  createdAt: string;
  deploymentPlanId: string;
  planChecksum: string;
  actorUserAccountId?: string;
  status: DeploymentPreflightStatus;
  checks: DeploymentPreflightCheck[];
}

export interface DeploymentPreflightCheck {
  id: string;
  createdAt: string;
  deploymentPreflightRunId: string;
  deploymentPlanId: string;
  deploymentPlanTargetId?: string;
  deploymentTargetId?: string;
  taskId?: string;
  component?: string;
  checkKey: string;
  status: DeploymentPreflightCheckStatus;
  expected: Record<string, unknown>;
  actual: Record<string, unknown>;
  message: string;
  sortOrder: number;
}

export interface DeploymentPlanTarget {
  id: string;
  deploymentTargetId: string;
  name: string;
  type: DeploymentType;
  platform: 'linux/amd64' | 'linux/arm64';
  customerOrganizationId?: string;
  sortOrder: number;
}

export interface DeploymentPlanTargetComponent {
  id: string;
  deploymentPlanTargetId: string;
  deploymentTargetId: string;
  component: string;
  version: string;
  image: string;
  platform: 'linux/amd64' | 'linux/arm64';
  contracts: string[];
  configChecksum: string;
  expectedStateVersion: number;
  expectedStateChecksum: string;
  expectedReleaseBundleId?: string;
  sortOrder: number;
}

export interface DeploymentPlanStep {
  id: string;
  stepKey: string;
  name: string;
  actionType: string;
  actionName: string;
  executionLocation: string;
  inputBindings: Record<string, unknown>;
  condition: string;
  targetTags: string[];
  failureMode: string;
  timeoutSeconds: number;
  retryMaxAttempts: number;
  retryIntervalSeconds: number;
  requiredPermissions: string[];
  sortOrder: number;
  dependencies: string[];
  included: boolean;
  excludedReason?: string;
}

export interface DeploymentPlanVariable {
  id: string;
  variableSetId: string;
  variableId: string;
  key: string;
  type: VariableType;
  isRequired: boolean;
  status: DeploymentPlanVariableStatus;
  source: string;
  value?: unknown;
  referenceId?: string;
  referenceName?: string;
  redacted: boolean;
  trace: VariableResolutionTraceEntry[];
}

export interface DeploymentPlanIssue {
  id: string;
  severity: DeploymentPlanIssueSeverity;
  code: string;
  field: string;
  message: string;
  sortOrder: number;
}
