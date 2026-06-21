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
}
