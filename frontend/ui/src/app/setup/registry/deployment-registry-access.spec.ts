import {TestBed} from '@angular/core/testing';
import {Router} from '@angular/router';
import {vi} from 'vitest';
import {AuthService} from '../../services/auth.service';
import {deploymentRegistryMutationGuard} from './deployment-registry-access';

describe('deployment registry route access', () => {
  for (const {name, role, superAdmin, allowed} of [
    {name: 'vendor read write', role: 'read_write', superAdmin: false, allowed: true},
    {name: 'vendor admin', role: 'admin', superAdmin: false, allowed: true},
    {name: 'vendor read only', role: 'read_only', superAdmin: false, allowed: false},
    {name: 'super admin', role: 'admin', superAdmin: true, allowed: false},
  ] as const) {
    it(`${allowed ? 'allows' : 'rejects'} ${name}`, () => {
      const auth = {
        isVendor: vi.fn(() => true),
        isSuperAdmin: vi.fn(() => superAdmin),
        hasAnyRole: vi.fn((...roles: string[]) => roles.includes(role)),
      };
      const router = {createUrlTree: vi.fn(() => ({redirect: '/'}))};
      TestBed.configureTestingModule({
        providers: [
          {provide: AuthService, useValue: auth},
          {provide: Router, useValue: router},
        ],
      });

      const result = TestBed.runInInjectionContext(() => deploymentRegistryMutationGuard({} as never, {} as never));

      expect(result === true).toBe(allowed);
    });
  }
});
