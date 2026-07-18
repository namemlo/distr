export type RegistryImportClassification =
  | 'standard'
  | 'shared'
  | 'external'
  | 'observe_only'
  | 'ignored'
  | 'needs_decision';

export type RegistryImportDeliveryModel = 'dedicated' | 'shared' | 'external';

export interface RegistryImportDecision {
  rootKey: string;
  classification: RegistryImportClassification;
}

export interface RegistryImportPlacement {
  componentKey: string;
  physicalName: string;
  configNamespace?: string;
  databaseBoundary?: string;
  healthAdapter?: string;
  renamedFrom?: string;
}

export interface RegistryImportSourcePlacement {
  rootKey: string;
  physicalName: string;
}

export interface RegistryImportRoot {
  key: string;
  name: string;
  deliveryModel: RegistryImportDeliveryModel;
  classification: RegistryImportClassification;
  customerOrganizationId?: string;
  deploymentTargetId: string;
  environmentId: string;
  subscriberCustomerOrganizationIds?: string[];
  physicalIdentity: string;
  placements: RegistryImportPlacement[];
}

export interface RegistryImportRequest {
  sourceKind: string;
  toolName: string;
  toolVersion: string;
  sourceCommit?: string;
  parameters: Record<string, string>;
  evidenceReference: string;
  evidenceChecksum: string;
  roots: RegistryImportRoot[];
  sourcePlacements?: RegistryImportSourcePlacement[];
}

export interface RegistryImportChange {
  kind: string;
  rootKey: string;
  placementKey?: string;
  physicalName?: string;
  message: string;
}

export interface RegistryImportDiff {
  creates: RegistryImportChange[];
  updates: RegistryImportChange[];
  retirements: RegistryImportChange[];
  conflicts: RegistryImportChange[];
}

export interface RegistryImportCounts {
  discoveredRoots: number;
  classifiedRoots: number;
  discoveredPlacements: number;
  omittedPlacements: number;
  creates: number;
  updates: number;
  retirements: number;
  conflicts: number;
}

export interface RegistryImportDiagnostic {
  code: string;
  field: string;
  message: string;
}

export interface RegistryImport {
  id: string;
  previewChecksum: string;
  counts: RegistryImportCounts;
  diff: RegistryImportDiff;
  diagnostics: RegistryImportDiagnostic[];
  diagnosticsTruncated: boolean;
  omissions: string[];
  roots: RegistryImportRoot[];
}

export interface RegistryImportResult {
  id: string;
  previewChecksum: string;
  state: string;
  applied: boolean;
  counts: RegistryImportCounts;
  checkpoint: number;
}

export interface RegistryCoverage {
  importId: string;
  discoveredRoots: number;
  classifiedRoots: number;
  actionableManagedRoots: number;
  observeOnlyRoots: number;
  externalRoots: number;
  ignoredRoots: number;
  unresolvedRoots: number;
  discoveredPlacements: number;
  services: number;
  omittedPlacements: number;
  omissions: string[];
  complete: boolean;
}
