import {DeploymentType} from '@distr-sh/distr-sdk';
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
  status: DeploymentPlanStatus;
  canonicalChecksum: string;
  targets: DeploymentPlanTarget[];
  steps: DeploymentPlanStep[];
  variables: DeploymentPlanVariable[];
  issues: DeploymentPlanIssue[];
}

export interface DeploymentPlanTarget {
  id: string;
  deploymentTargetId: string;
  name: string;
  type: DeploymentType;
  customerOrganizationId?: string;
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
