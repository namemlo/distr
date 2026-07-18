import {inject} from '@angular/core';
import {CanActivateFn, Router} from '@angular/router';
import {AuthService} from '../../services/auth.service';

type DeploymentRegistryAuth = Pick<AuthService, 'hasAnyRole' | 'isSuperAdmin' | 'isVendor'>;

export function canMutateDeploymentRegistry(auth: DeploymentRegistryAuth): boolean {
  return auth.isVendor() && !auth.isSuperAdmin() && auth.hasAnyRole('read_write', 'admin');
}

export const deploymentRegistryMutationGuard: CanActivateFn = () => {
  return canMutateDeploymentRegistry(inject(AuthService)) ? true : inject(Router).createUrlTree(['/']);
};
