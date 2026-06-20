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

export interface ReleaseBundle {
  id: string;
  createdAt: string;
  updatedAt: string;
  applicationId: string;
  channelId: string;
  releaseNumber: string;
  releaseNotes: string;
  sourceRevision: string;
  sourceMetadata?: ReleaseBundleSourceMetadata;
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
  releaseNumber: string;
  releaseNotes: string;
  sourceRevision: string;
  sourceMetadata?: ReleaseBundleSourceMetadata;
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
