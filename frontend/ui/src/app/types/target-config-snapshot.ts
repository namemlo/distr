export type TargetConfigSnapshotObjectKind = 'deployment_descriptor' | 'service_config' | 'adapter_input';

export interface TargetConfigSnapshotObject {
  key: string;
  kind: TargetConfigSnapshotObjectKind;
  reference: string;
  versionId?: string;
  mediaType: string;
  sizeBytes: number;
  checksum: string;
}

export interface CreateTargetConfigSnapshotComponent {
  physicalName: string;
  componentInstanceId: string;
  deploymentUnitId: string;
}

export interface TargetConfigSnapshotComponent {
  physicalName: string;
  componentInstanceId: string;
}

export interface CreateTargetConfigSnapshotSecretReference {
  key: string;
  provider: string;
  reference: string;
  versionFingerprint: string;
}

export interface TargetConfigSnapshotSecretReference {
  key: string;
  provider: string;
  opaqueReference: string;
  versionFingerprint: string;
}

export interface TargetConfigSnapshotFeatureFlag {
  key: string;
  enabled: boolean;
}

export interface CreateTargetConfigSnapshotRequest {
  deploymentUnitId: string;
  targetEnvironmentAssignmentId: string;
  environmentId: string;
  sourceRepository: string;
  sourceCommit: string;
  sourceAdapter: string;
  adapterVersion: string;
  targetPlatform: string;
  runtimeConstraints: Record<string, string>;
  objects: TargetConfigSnapshotObject[];
  components: CreateTargetConfigSnapshotComponent[];
  secretReferences: CreateTargetConfigSnapshotSecretReference[];
  featureFlags: TargetConfigSnapshotFeatureFlag[];
}

export interface TargetConfigSnapshot {
  id: string;
  createdAt: string;
  createdByUserAccountId: string;
  deploymentUnitId: string;
  targetEnvironmentAssignmentId: string;
  environmentId: string;
  sourceRepository: string;
  sourceCommit: string;
  sourceAdapter: string;
  adapterVersion: string;
  targetPlatform: string;
  runtimeConstraints: Record<string, string>;
  canonicalChecksum: string;
  objects: TargetConfigSnapshotObject[];
  components: TargetConfigSnapshotComponent[];
  secretReferences: TargetConfigSnapshotSecretReference[];
  featureFlags: TargetConfigSnapshotFeatureFlag[];
}

export interface TargetConfigSnapshotListFilter {
  deploymentUnitId?: string;
  targetEnvironmentAssignmentId?: string;
  cursor?: string;
  limit?: number;
}

export interface TargetConfigSnapshotPage {
  items: TargetConfigSnapshot[];
  nextCursor?: string;
}

export type TargetConfigSnapshotVerificationCode =
  | 'verified'
  | 'reference_mismatch'
  | 'version_mismatch'
  | 'media_type_mismatch'
  | 'size_mismatch'
  | 'checksum_mismatch'
  | 'verification_unavailable'
  | 'verification_failed';

export interface TargetConfigSnapshotObjectVerification {
  key: string;
  verified: boolean;
  code: TargetConfigSnapshotVerificationCode;
  message: string;
  observedVersionId?: string;
  observedMediaType?: string;
  observedSizeBytes?: number;
  observedChecksum?: string;
}

export interface TargetConfigSnapshotVerification {
  snapshotId: string;
  verified: boolean;
  objects: TargetConfigSnapshotObjectVerification[];
}
