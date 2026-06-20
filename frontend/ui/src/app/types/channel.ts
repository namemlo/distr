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
}

export interface CreateUpdateChannelRequest {
  applicationId: string;
  lifecycleId: string;
  name: string;
  description: string;
  sortOrder: number;
  isDefault: boolean;
}
