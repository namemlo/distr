import {DeploymentPlan} from './deployment-plan';
import {ReleaseBundleComponentType} from './release-bundle';

export type DeploymentTaskStatus = 'QUEUED' | 'RUNNING' | 'SUCCEEDED' | 'FAILED' | 'CANCELED';
export type DeploymentStepRunStatus = 'PENDING' | 'RUNNING' | 'SUCCEEDED' | 'FAILED' | 'SKIPPED';
export type DeploymentStepRunEventType = 'STARTED' | 'PROGRESS' | 'LOG' | 'OUTPUT' | 'SUCCEEDED' | 'FAILED';
export type DeploymentTimelineChangeKind = 'unchanged' | 'added' | 'removed' | 'changed';
export type DeploymentTimelineItemSource = 'task' | 'legacy_deployment';

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

export interface DeploymentCompatibilityAvailability {
  processSnapshot: boolean;
  variableSnapshot: boolean;
  channel: boolean;
  environment: boolean;
  taskLogs: boolean;
  redeployPlan: boolean;
}

export interface DeploymentTimelineItem {
  source?: DeploymentTimelineItemSource;
  taskId?: string;
  legacyDeploymentId?: string;
  legacyDeploymentRevisionId?: string;
  syntheticReleaseId?: string;
  deploymentPlanId?: string;
  deploymentPlanTargetId?: string;
  deploymentTargetId: string;
  applicationId: string;
  applicationName: string;
  releaseBundleId?: string;
  releaseNumber: string;
  channelId?: string;
  channelName: string;
  environmentId?: string;
  environmentName: string;
  customerOrganizationId?: string;
  deploymentTargetName: string;
  actorUserAccountId?: string;
  status?: DeploymentTaskStatus;
  queuedAt: string;
  startedAt?: string;
  completedAt?: string;
  processSnapshotId?: string;
  variableSnapshotId?: string;
  availability?: DeploymentCompatibilityAvailability;
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

export interface DeploymentTimelineCompareRef {
  taskId?: string;
  legacyDeploymentRevisionId?: string;
}

export interface DeploymentTimelineComparisonAvailability {
  process: boolean;
  steps: boolean;
  variables: boolean;
}

export interface DeploymentTimelineComparison {
  base: DeploymentTimelineItem;
  compare: DeploymentTimelineItem;
  process: DeploymentTimelineProcessChange;
  availability: DeploymentTimelineComparisonAvailability;
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

export interface DeploymentTask {
  id: string;
  createdAt?: string;
  updatedAt?: string;
  queuedAt: string;
  startedAt?: string;
  completedAt?: string;
  taskType?: string;
  deploymentPlanId?: string;
  deploymentPlanTargetId?: string;
  deploymentTargetId?: string;
  applicationId?: string;
  releaseBundleId?: string;
  channelId?: string;
  environmentId?: string;
  actorUserAccountId?: string;
  status: DeploymentTaskStatus;
  queueOrder?: number;
  stepRuns: DeploymentStepRun[];
}

export interface DeploymentStepRun {
  id: string;
  createdAt?: string;
  updatedAt?: string;
  startedAt?: string;
  completedAt?: string;
  taskId: string;
  deploymentPlanId?: string;
  deploymentPlanStepId?: string;
  stepKey: string;
  name: string;
  actionType: string;
  status: DeploymentStepRunStatus;
  sortOrder: number;
  skippedReason?: string;
}

export interface DeploymentTaskTimeline {
  organizationId: string;
  taskId: string;
  events: DeploymentStepRunEvent[];
}

export interface DeploymentStepRunEvent {
  id: string;
  createdAt: string;
  occurredAt: string;
  organizationId?: string;
  taskId: string;
  stepRunId: string;
  taskLeaseId?: string;
  agentId?: string;
  sequence: number;
  type: DeploymentStepRunEventType;
  message?: string;
  progressPercent?: number;
  details?: Record<string, unknown>;
  redacted: boolean;
  logs: DeploymentStepRunLog[];
  outputs: DeploymentStepRunOutput[];
}

export interface DeploymentStepRunLog {
  id?: string;
  createdAt?: string;
  occurredAt?: string;
  eventId?: string;
  stream: 'stdout' | 'stderr' | 'system';
  severity: 'debug' | 'info' | 'warn' | 'error';
  body: string;
  redacted: boolean;
}

export interface DeploymentStepRunOutput {
  id?: string;
  name: string;
  value?: unknown;
  sensitive: boolean;
  redacted: boolean;
}
