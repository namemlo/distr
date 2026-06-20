export interface CreateUpdateDeploymentProcessRequest {
  applicationId: string;
  name: string;
  description: string;
  sortOrder: number;
}

export interface DeploymentProcess {
  id: string;
  createdAt: string;
  updatedAt: string;
  applicationId: string;
  name: string;
  description: string;
  sortOrder: number;
}

export interface CreateDeploymentProcessRevisionRequest {
  description: string;
  steps: DeploymentProcessStepRequest[];
}

export interface DeploymentProcessRevision {
  id: string;
  createdAt: string;
  updatedAt: string;
  deploymentProcessId: string;
  revisionNumber: number;
  description: string;
  steps: DeploymentProcessStep[];
}

export interface DeploymentProcessStepRetryPolicy {
  maxAttempts: number;
  intervalSeconds: number;
}

export interface DeploymentProcessStepRequest {
  key: string;
  name: string;
  actionType: string;
  stepTemplateVersionId?: string;
  executionLocation: string;
  inputBindings: Record<string, unknown>;
  condition: string;
  channelIds: string[];
  environmentIds: string[];
  targetTags: string[];
  failureMode: string;
  timeoutSeconds: number;
  retryPolicy: DeploymentProcessStepRetryPolicy;
  requiredPermissions: string[];
  sortOrder: number;
  dependencies: string[];
}

export interface DeploymentProcessStep extends DeploymentProcessStepRequest {
  id: string;
  deploymentProcessRevisionId: string;
}
