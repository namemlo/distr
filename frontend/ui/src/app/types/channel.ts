export interface Channel {
  id: string;
  createdAt: string;
  updatedAt: string;
  applicationId: string;
  lifecycleId: string;
  name: string;
  description: string;
  sortOrder: number;
  isDefault: boolean;
  allowedVersionRanges: string[];
  allowedPrereleasePatterns: string[];
  allowedSourceBranches: string[];
  allowedSourceTags: string[];
}

export interface CreateUpdateChannelRequest {
  applicationId: string;
  lifecycleId: string;
  name: string;
  description: string;
  sortOrder: number;
  isDefault: boolean;
  allowedVersionRanges: string[];
  allowedPrereleasePatterns: string[];
  allowedSourceBranches: string[];
  allowedSourceTags: string[];
}

export interface ValidateChannelVersionRequest {
  version: string;
  sourceBranch?: string;
  sourceTag?: string;
}

export interface ChannelValidationError {
  field: string;
  rule: string;
  message: string;
}

export interface ChannelVersionValidationResponse {
  valid: boolean;
  errors: ChannelValidationError[];
}
