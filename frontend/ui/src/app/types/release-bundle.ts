import {DeploymentProcessRevision} from './deployment-process';

export type ReleaseBundleStatus = 'DRAFT' | 'VALIDATING' | 'PUBLISHED' | 'BLOCKED' | 'ARCHIVED';

export type ReleaseBundleComponentType =
  | 'application_version'
  | 'oci_image'
  | 'oci_artifact'
  | 'helm_chart'
  | 'child_release_bundle'
  | 'external_artifact';

export interface ReleaseBundleComponent {
  id: string;
  releaseBundleId: string;
  key: string;
  name: string;
  type: ReleaseBundleComponentType;
  version: string;
  applicationVersionId?: string;
  packageRef: string;
  digest: string;
  checksum: string;
  childReleaseBundleId?: string;
}

export interface ReleaseBundleSourceMetadata {
  repository: string;
  branch: string;
  tag: string;
  ciProvider: string;
  ciRunId: string;
  ciRunUrl: string;
}

export interface ReleaseContractV1 {
  schema: 'distr.release-contract/v1';
  source: {
    repository: string;
    branch: string;
    sourceCommit: string;
    builtCommit: string;
  };
  build: {externalId: string; externalUrl: string};
  components: Array<{
    name: string;
    version: string;
    image: string;
    platform: 'linux/amd64' | 'linux/arm64';
    contracts: string[];
  }>;
  compatibility: {
    requires: Array<{component: string; contract?: string; minimumVersion?: string; reason?: string}>;
    affectedComponents: string[];
  };
  operations: {migrationRequired: boolean; configChangeRequired: boolean};
  config: {
    repositoryCommit: string;
    composePath: string;
    serviceConfigPath: string;
    composeChecksum: string;
    serviceConfigChecksum: string;
    immutableObjects: Array<{uri: string; versionId: string; checksum: string}>;
  };
  changes: {summary: string; commits: string[]};
}

export interface ComponentReleaseContractV2 {
  schema: 'distr.component-release/v2';
  componentKey: string;
  version: string;
  source: {
    repository: string;
    requestedRef: string;
    commit: string;
  };
  build: {id: string; builder: string};
  artifacts: Array<{
    key: string;
    type: 'oci-image' | 'oci-artifact' | 'helm-chart';
    mediaType: string;
    digest: string;
    platforms: Array<{
      platform: 'linux/amd64' | 'linux/arm64';
      digest: string;
    }>;
  }>;
  provides: Array<{name: string; version: string}>;
  requires: Array<{
    name: string;
    range: string;
    resolutionStage: 'product' | 'target';
    allowedModes: Array<'included' | 'pinned-existing' | 'shared-provider' | 'approved-external' | 'feature-disabled'>;
  }>;
  migrations: Array<{
    key: string;
    type: 'database' | 'data' | 'runtime';
    order: number;
    compatibility: 'backward-compatible' | 'forward-compatible' | 'breaking';
    failurePolicy: 'stop' | 'retry' | 'forward-fix';
    description: string;
  }>;
  changes: {summary: string; commits: string[]};
  evidence: {
    provenance: string[];
    sbom: string[];
    signatures: string[];
    tests: string[];
  };
}

export type ReleaseContract = ReleaseContractV1 | ComponentReleaseContractV2;

export interface ReleaseBundle {
  id: string;
  createdAt: string;
  updatedAt: string;
  applicationId: string;
  channelId: string;
  processSnapshotId?: string;
  variableSnapshotId?: string;
  releaseNumber: string;
  releaseNotes: string;
  sourceRevision: string;
  sourceMetadata?: ReleaseBundleSourceMetadata;
  releaseContract?: ReleaseContract;
  kind?: 'legacy' | 'component' | 'product';
  releaseContractSchema?: 'distr.release/v1' | 'distr.component-release/v2' | 'distr.product-release/v1';
  status: ReleaseBundleStatus;
  publishedByUserAccountId?: string;
  publishedAt?: string;
  canonicalChecksum: string;
  components: ReleaseBundleComponent[];
}

export interface ReleaseBundleComponentRequest {
  key: string;
  name: string;
  type: ReleaseBundleComponentType;
  version: string;
  applicationVersionId?: string;
  packageRef: string;
  digest: string;
  checksum: string;
  childReleaseBundleId?: string;
}

export interface CreateUpdateReleaseBundleRequest {
  applicationId: string;
  channelId: string;
  deploymentProcessRevisionId?: string;
  releaseNumber: string;
  releaseNotes: string;
  sourceRevision: string;
  sourceMetadata?: ReleaseBundleSourceMetadata;
  releaseContract?: ReleaseContract;
  components: ReleaseBundleComponentRequest[];
}

export interface ReleaseBundleValidationIssue {
  field: string;
  rule: string;
  message: string;
}

export interface ReleaseBundleValidationResponse {
  valid: boolean;
  errors: ReleaseBundleValidationIssue[];
  warnings: ReleaseBundleValidationIssue[];
}

export interface ProcessSnapshot {
  id: string;
  createdAt: string;
  applicationId: string;
  deploymentProcessId: string;
  deploymentProcessRevisionId: string;
  revisionNumber: number;
  canonicalChecksum: string;
  revision: DeploymentProcessRevision;
}
