export type VariableType =
  | 'string'
  | 'number'
  | 'boolean'
  | 'json'
  | 'secret_reference'
  | 'account_reference'
  | 'certificate_reference';

export interface VariableSet {
  id: string;
  createdAt: string;
  updatedAt: string;
  name: string;
  description: string;
  sortOrder: number;
  applicationIds: string[];
  variables: Variable[];
}

export interface Variable {
  id: string;
  createdAt: string;
  updatedAt: string;
  key: string;
  description: string;
  type: VariableType;
  isRequired: boolean;
  defaultValue?: unknown;
  referenceId?: string;
  referenceName?: string;
  scopedValues?: VariableScopedValue[];
}

export interface CreateUpdateVariableSetRequest {
  name: string;
  description: string;
  sortOrder: number;
  applicationIds: string[];
  variables: VariableRequest[];
}

export interface VariableRequest {
  key: string;
  description: string;
  type: VariableType;
  isRequired: boolean;
  defaultValue?: unknown;
  referenceId?: string;
  referenceName?: string;
  scopedValues?: VariableScopedValueRequest[];
}

export interface VariableScope {
  customerOrganizationId?: string;
  environmentId?: string;
  channelId?: string;
  deploymentTargetId?: string;
  applicationId?: string;
  targetTag?: string;
  processStepKey?: string;
}

export interface VariableScopedValue {
  id: string;
  createdAt: string;
  updatedAt: string;
  scope: VariableScope;
  sortOrder: number;
  value?: unknown;
  referenceId?: string;
  referenceName?: string;
}

export interface VariableScopedValueRequest {
  scope: VariableScope;
  sortOrder: number;
  value?: unknown;
  referenceId?: string;
  referenceName?: string;
}

export interface ResolveVariablesPreviewRequest {
  variableSetIds: string[];
  scope: VariableResolutionScope;
  promptedValues?: VariablePromptedValue[];
}

export interface VariableResolutionScope {
  customerOrganizationId?: string;
  environmentId?: string;
  channelId?: string;
  deploymentTargetId?: string;
  applicationId?: string;
  targetTags?: string[];
  processStepKey?: string;
}

export interface VariablePromptedValue {
  key: string;
  value?: unknown;
  referenceId?: string;
  referenceName?: string;
}

export interface ResolvedVariable {
  variableSetId: string;
  variableId: string;
  key: string;
  type: VariableType;
  isRequired: boolean;
  status: 'resolved' | 'unresolved';
  source: string;
  value?: unknown;
  referenceId?: string;
  referenceName?: string;
  redacted: boolean;
  trace: VariableResolutionTraceEntry[];
}

export interface VariableResolutionTraceEntry {
  source: string;
  scope: VariableScope;
  selected: boolean;
  reason: string;
}
