import {TestBed} from '@angular/core/testing';
import {ActivatedRouteSnapshot, CanActivateFn, Route, Router, RouterStateSnapshot, UrlTree} from '@angular/router';
import {UserRole} from '@distr-sh/distr-sdk';
import 'dayjs/plugin/relativeTime';
import 'dayjs/plugin/utc';
import {of} from 'rxjs';
import {vi} from 'vitest';
import {routes} from './app-logged-in.routes';
import {AuthService} from './services/auth.service';
import {FeatureFlagService} from './services/feature-flag.service';
import {TargetConfigSnapshotsComponent} from './setup/config-snapshots/target-config-snapshots.component';

describe('target configuration snapshot route', () => {
  let auth: {
    isVendor: ReturnType<typeof vi.fn>;
    isSuperAdmin: ReturnType<typeof vi.fn>;
    hasAnyRole: ReturnType<typeof vi.fn>;
  };
  let featureFlags: {isExperimentalFeatureEnabled$: ReturnType<typeof vi.fn>};
  let router: {createUrlTree: ReturnType<typeof vi.fn>};
  const denied = {} as UrlTree;

  beforeEach(() => {
    auth = {
      isVendor: vi.fn().mockReturnValue(true),
      isSuperAdmin: vi.fn().mockReturnValue(false),
      hasAnyRole: vi.fn().mockReturnValue(true),
    };
    featureFlags = {isExperimentalFeatureEnabled$: vi.fn().mockReturnValue(of(true))};
    router = {createUrlTree: vi.fn().mockReturnValue(denied)};
    TestBed.configureTestingModule({
      providers: [
        {provide: AuthService, useValue: auth},
        {provide: FeatureFlagService, useValue: featureFlags},
        {provide: Router, useValue: router},
      ],
    });
  });

  for (const role of ['read_only', 'read_write', 'admin'] satisfies UserRole[]) {
    it(`allows vendor ${role} readers to inspect history when the mutation flag is disabled`, async () => {
      auth.hasAnyRole.mockImplementation((...roles: UserRole[]) => roles.includes(role));
      featureFlags.isExperimentalFeatureEnabled$.mockReturnValue(of(false));
      const route = targetConfigRoute();

      expect(route.component).toBe(TargetConfigSnapshotsComponent);
      expect(await evaluateGuards(route.canActivate)).toEqual([true]);
      expect(featureFlags.isExperimentalFeatureEnabled$).not.toHaveBeenCalled();
    });
  }

  it('allows a vendor super administrator to inspect history when the mutation flag is disabled', async () => {
    auth.isSuperAdmin.mockReturnValue(true);
    featureFlags.isExperimentalFeatureEnabled$.mockReturnValue(of(false));

    expect(await evaluateGuards(targetConfigRoute().canActivate)).toEqual([true]);
    expect(featureFlags.isExperimentalFeatureEnabled$).not.toHaveBeenCalled();
  });

  it('keeps target configuration history vendor-only', async () => {
    auth.isVendor.mockReturnValue(false);

    expect(await evaluateGuards(targetConfigRoute().canActivate)).toEqual([denied]);
  });

  function targetConfigRoute() {
    const route = routes[0].children?.find((candidate) => candidate.path === 'setup/config-snapshots');
    expect(route).toBeDefined();
    return route!;
  }

  async function evaluateGuards(guards: Route['canActivate']): Promise<(boolean | UrlTree)[]> {
    const results: (boolean | UrlTree)[] = [];
    for (const guard of guards ?? []) {
      const guardFn = guard as CanActivateFn;
      const result = TestBed.runInInjectionContext(() =>
        guardFn({} as ActivatedRouteSnapshot, {} as RouterStateSnapshot)
      );
      results.push(await Promise.resolve(result as boolean | UrlTree | Promise<boolean | UrlTree>));
    }
    return results;
  }
});
