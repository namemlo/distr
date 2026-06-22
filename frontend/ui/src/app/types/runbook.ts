export interface CreateUpdateRunbookRequest {
  applicationId: string;
  name: string;
  description: string;
  sortOrder: number;
}

export interface Runbook {
  id: string;
  createdAt: string;
  updatedAt: string;
  applicationId: string;
  name: string;
  description: string;
  sortOrder: number;
}

export interface CreateRunbookRevisionRequest {
  description: string;
  steps: RunbookStepRequest[];
}

export interface RunbookRevision {
  id: string;
  createdAt: string;
  updatedAt: string;
  runbookId: string;
  revisionNumber: number;
  description: string;
  steps: RunbookStep[];
}

export interface RunbookStepRetryPolicy {
  maxAttempts: number;
  intervalSeconds: number;
}

export interface RunbookStepRequest {
  key: string;
  name: string;
  actionType: string;
  stepTemplateVersionId?: string;
  executionLocation: string;
  inputBindings: Record<string, unknown>;
  condition: string;
  failureMode: string;
  timeoutSeconds: number;
  retryPolicy: RunbookStepRetryPolicy;
  requiredPermissions: string[];
  sortOrder: number;
  dependencies: string[];
}

export interface RunbookStep extends RunbookStepRequest {
  id: string;
  runbookRevisionId: string;
}

export interface RunbookSnapshot {
  id: string;
  createdAt: string;
  publishedAt: string;
  publishedByUserAccountId?: string;
  applicationId: string;
  runbookId: string;
  runbookRevisionId: string;
  revisionNumber: number;
  canonicalChecksum: string;
  revision: RunbookRevision;
}
