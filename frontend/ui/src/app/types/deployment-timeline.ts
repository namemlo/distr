import {DeploymentPlan} from './deployment-plan';
import {ReleaseBundleComponentType} from './release-bundle';

export type DeploymentTaskStatus = 'QUEUED' | 'RUNNING' | 'SUCCEEDED' | 'FAILED' | 'CANCELED';
export type DeploymentTimelineChangeKind = 'unchanged' | 'added' | 'removed' | 'changed';

export interface DeploymentTimelineQuery {
  applicationId?: string;
  releaseBundleId?: string;
  environmentId?: string;
  deploymentTargetId?: string;
  customerOrganizationId?: string;
  limit?: number;
  includeNonTerminal?: boolean;
}

export interface DeploymentTimeline {
  items: DeploymentTimelineItem[];
}

export interface DeploymentTimelineItem {
  taskId: string;
  deploymentPlanId: string;
  deploymentPlanTargetId: string;
  deploymentTargetId: string;
  applicationId: string;
  applicationName: string;
  releaseBundleId: string;
  releaseNumber: string;
  channelId: string;
  channelName: string;
  environmentId: string;
  environmentName: string;
  customerOrganizationId?: string;
  deploymentTargetName: string;
  actorUserAccountId?: string;
  status: DeploymentTaskStatus;
  queuedAt: string;
  startedAt?: string;
  completedAt?: string;
  processSnapshotId?: string;
  variableSnapshotId?: string;
  components: DeploymentTimelineComponent[];
  lastSuccessful: boolean;
  redeployAvailable: boolean;
}

export interface DeploymentTimelineComponent {
  key: string;
  name: string;
  type: ReleaseBundleComponentType;
  version: string;
}

export interface DeploymentTimelineComparison {
  base: DeploymentTimelineItem;
  compare: DeploymentTimelineItem;
  process: DeploymentTimelineProcessChange;
  components: DeploymentTimelineComponentChange[];
  steps: DeploymentTimelineStepChange[];
  variables: DeploymentTimelineVariableChange[];
}

export interface DeploymentTimelineProcessChange {
  baseProcessSnapshotId?: string;
  compareProcessSnapshotId?: string;
  baseRevisionNumber?: number;
  compareRevisionNumber?: number;
  baseCanonicalChecksum?: string;
  compareCanonicalChecksum?: string;
  changed: boolean;
}

export interface DeploymentTimelineComponentChange {
  key: string;
  name: string;
  kind: DeploymentTimelineChangeKind;
  baseVersion?: string;
  compareVersion?: string;
  baseType?: ReleaseBundleComponentType;
  compareType?: ReleaseBundleComponentType;
}

export interface DeploymentTimelineStepChange {
  stepKey: string;
  name: string;
  kind: DeploymentTimelineChangeKind;
  baseActionType?: string;
  compareActionType?: string;
  baseIncluded?: boolean;
  compareIncluded?: boolean;
}

export interface DeploymentTimelineVariableChange {
  key: string;
  kind: DeploymentTimelineChangeKind;
  baseStatus?: string;
  compareStatus?: string;
  baseSource?: string;
  compareSource?: string;
  baseRedacted: boolean;
  compareRedacted: boolean;
  baseValue?: unknown;
  compareValue?: unknown;
  baseReference?: string;
  compareReference?: string;
}

export interface DeploymentTimelineRedeploy {
  plan: DeploymentPlan;
  warning: string;
}
