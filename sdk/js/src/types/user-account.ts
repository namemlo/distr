import {BaseModel} from './base';

export type AccountRole = 'read_only' | 'read_write' | 'admin';

export interface UserAccount extends BaseModel {
  email: string;
  emailVerified: boolean;
  name?: string;
  imageId?: string;
  imageUrl?: string;
  mfaEnabled: boolean;
}

export interface UserAccountWithRole extends UserAccount {
  userRole: AccountRole;
  customerOrganizationId?: string;
  joinedOrgAt: string;
}
