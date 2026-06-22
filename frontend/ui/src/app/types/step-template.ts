export interface ImportStepTemplateRequest {
  sourceType: 'builtin' | 'oci';
  sourceRef: string;
  name: string;
  description: string;
  category: string;
  version: string;
  actionType: string;
  executionLocation: string;
  inputSchema: Record<string, unknown>;
  outputSchema: Record<string, unknown>;
  defaultInputBindings: Record<string, unknown>;
  minimumAgentVersion: string;
  compatibleActionVersion: string;
  runtimeCompatibilityNotes: string;
  deprecated: boolean;
}

export interface StepTemplate {
  id: string;
  createdAt: string;
  updatedAt: string;
  sourceType: 'builtin' | 'oci';
  sourceRef: string;
  name: string;
  description: string;
  category: string;
  installedAt: string;
  installedByUserAccountId?: string;
  versions: StepTemplateVersion[];
}

export interface StepTemplateVersion {
  id: string;
  createdAt: string;
  stepTemplateId: string;
  version: string;
  actionType: string;
  executionLocation: string;
  inputSchema: Record<string, unknown>;
  outputSchema: Record<string, unknown>;
  defaultInputBindings: Record<string, unknown>;
  minimumAgentVersion: string;
  compatibleActionVersion: string;
  runtimeCompatibilityNotes: string;
  deprecated: boolean;
}
