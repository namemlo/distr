import {VariableType} from './variable-set';

export interface ConfigurationDrift {
  deploymentId: string;
  applicationId: string;
  hasDrift: boolean;
  newRequiredVariables: ConfigurationDriftVariable[];
  missingVariables: ConfigurationDriftVariable[];
  removedVariables: ConfigurationDriftRemovedValue[];
  typeChanges: ConfigurationDriftTypeChange[];
  defaultChanges: ConfigurationDriftDefaultChange[];
  secretReferenceChanges: ConfigurationDriftReferenceChange[];
}

export interface ConfigurationDriftVariable {
  key: string;
  type: VariableType;
  isRequired: boolean;
  source: string;
  value?: unknown;
  referenceId?: string;
  referenceName?: string;
  redacted?: boolean;
}

export interface ConfigurationDriftRemovedValue {
  key: string;
}

export interface ConfigurationDriftTypeChange {
  key: string;
  expectedType: VariableType;
  deployedType: string;
}

export interface ConfigurationDriftDefaultChange {
  key: string;
  type: VariableType;
  currentValue?: unknown;
  deployedValue?: unknown;
}

export interface ConfigurationDriftReferenceChange {
  key: string;
  type: VariableType;
  referenceId?: string;
  referenceName?: string;
  redacted: boolean;
}
