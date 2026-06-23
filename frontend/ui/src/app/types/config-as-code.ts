export type ConfigAsCodeResourceKind =
  | 'DeploymentProcess'
  | 'Channel'
  | 'Lifecycle'
  | 'VariableSetDefinition'
  | 'StepTemplateReference'
  | 'Runbook';

export type ConfigAsCodeAuthorityValue = 'DATABASE_MANAGED' | 'GIT_MANAGED';

export interface ConfigAsCodeValidateRequest {
  documents: ConfigAsCodeValidateDocumentRequest[];
}

export interface ConfigAsCodeValidateDocumentRequest {
  content: string;
}

export interface ConfigAsCodeValidateResponse {
  valid: boolean;
  documents: ConfigAsCodeDocumentResult[];
  errors: ConfigAsCodeIssue[];
  warnings: ConfigAsCodeIssue[];
}

export interface ConfigAsCodeDocumentResult {
  kind: ConfigAsCodeResourceKind;
  apiVersion: string;
  metadataName?: string;
  metadataPath?: string;
  canonicalChecksum: string;
}

export interface ConfigAsCodeIssue {
  documentIndex: number;
  path: string;
  message: string;
}

export interface ConfigAsCodeAuthority {
  resourceKind: ConfigAsCodeResourceKind;
  resourceId: string;
  authority: ConfigAsCodeAuthorityValue;
  repositoryPath: string;
  sourceRevision: string;
  documentChecksum: string;
  updatedByUserId?: string;
  updatedAt: string;
}

export interface ConfigAsCodeAuthorityListResponse {
  authorities: ConfigAsCodeAuthority[];
}

export interface ConfigAsCodeAuthorityUpdateRequest {
  authority: ConfigAsCodeAuthorityValue;
  repositoryPath: string;
  sourceRevision: string;
  documentChecksum: string;
}
