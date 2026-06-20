export interface Lifecycle {
  id: string;
  createdAt: string;
  updatedAt: string;
  name: string;
  description: string;
  sortOrder: number;
  phases: LifecyclePhase[];
}

export interface LifecyclePhase {
  id: string;
  name: string;
  description: string;
  sortOrder: number;
  environmentIds: string[];
  optional: boolean;
  automaticPromotion: boolean;
  minimumSuccessfulDeployments: number;
  approvalPolicyId?: string;
  retentionPolicyId?: string;
}

export interface CreateUpdateLifecycleRequest {
  name: string;
  description: string;
  sortOrder: number;
  phases: CreateUpdateLifecyclePhaseRequest[];
}

export interface CreateUpdateLifecyclePhaseRequest {
  name: string;
  description: string;
  sortOrder: number;
  environmentIds: string[];
  optional: boolean;
  automaticPromotion: boolean;
  minimumSuccessfulDeployments: number;
  approvalPolicyId?: string;
  retentionPolicyId?: string;
}
