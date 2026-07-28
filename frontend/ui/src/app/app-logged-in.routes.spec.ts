import {TestBed} from '@angular/core/testing';
import {
  ActivatedRouteSnapshot,
  CanActivateFn,
  provideRouter,
  Route,
  Router,
  RouterStateSnapshot,
  UrlTree,
} from '@angular/router';
import {UserRole} from '@distr-sh/distr-sdk';
import 'dayjs/plugin/relativeTime';
import 'dayjs/plugin/utc';
import {of} from 'rxjs';
import {vi} from 'vitest';
import {routes} from './app-logged-in.routes';
import {AuthService} from './services/auth.service';
import {FeatureFlagService} from './services/feature-flag.service';
import {TargetConfigSnapshotsComponent} from './setup/config-snapshots/target-config-snapshots.component';

describe('operator control-plane routes', () => {
  let auth: {
    isVendor: ReturnType<typeof vi.fn>;
    isSuperAdmin: ReturnType<typeof vi.fn>;
    hasAnyRole: ReturnType<typeof vi.fn>;
  };
  let featureFlags: {isExperimentalFeatureEnabled$: ReturnType<typeof vi.fn>};
  let router: Router;

  beforeEach(() => {
    auth = {
      isVendor: vi.fn().mockReturnValue(true),
      isSuperAdmin: vi.fn().mockReturnValue(false),
      hasAnyRole: vi.fn().mockReturnValue(true),
    };
    featureFlags = {
      isExperimentalFeatureEnabled$: vi.fn().mockImplementation(() => of(true)),
    };
    TestBed.configureTestingModule({
      providers: [
        provideRouter([]),
        {provide: AuthService, useValue: auth},
        {provide: FeatureFlagService, useValue: featureFlags},
      ],
    });
    router = TestBed.inject(Router);
  });

  it('registers every operator page at its stable URL', () => {
    expect(operatorPaths()).toEqual([
      '/fleet',
      '/releases',
      '/releases/:releaseId',
      '/deployments/plans',
      '/deployments/plans/:planId',
      '/deployments/campaigns',
      '/deployments/campaigns/:campaignId',
      '/deployments/executions',
      '/deployments/executions/:executionId',
      '/approvals',
      '/reconciliation',
      '/audit',
      '/setup',
    ]);
  });

  it('guards every operator route family with vendor and all three required features', () => {
    const guardedRoutes = [
      operatorRoute('/fleet'),
      operatorRoute('/releases'),
      operatorRoute('/deployments/plans'),
      operatorRoute('/deployments/plans/:planId'),
      operatorRoute('/deployments/campaigns'),
      operatorRoute('/deployments/campaigns/:campaignId'),
      operatorRoute('/deployments/executions'),
      operatorRoute('/deployments/executions/:executionId'),
      operatorRoute('/approvals'),
      operatorRoute('/reconciliation'),
      operatorRoute('/audit'),
      operatorRoute('/setup').children?.find((candidate) => candidate.path === ''),
    ];

    expect(guardedRoutes.every((route) => route?.canActivate?.length === 4)).toBe(true);
  });

  it('redirects the deployment root to the canonical target list', () => {
    const redirect = deploymentsRoute().children?.find((candidate) => candidate.path === '');

    expect(redirect?.path).toBe('');
    expect(redirect?.pathMatch).toBe('full');
    expect(redirect?.redirectTo).toBe('targets');
  });

  it('places every static deployment child before the legacy target parameter', () => {
    const childPaths = deploymentsRoute().children?.map((child) => child.path);

    expect(childPaths).toEqual([
      '',
      'targets',
      'targets/:deploymentTargetId',
      'plans',
      'plans/:planId',
      'campaigns',
      'campaigns/:campaignId',
      'executions',
      'executions/:executionId',
      ':deploymentTargetId',
    ]);
  });

  it('redirects a legacy target URL while preserving its query parameters and fragment', async () => {
    const legacyRoute = deploymentsRoute().children?.find((candidate) => candidate.path === ':deploymentTargetId');
    expect(legacyRoute).toBeDefined();

    const [result] = await evaluateGuards(
      legacyRoute!.canActivate,
      '/deployments/target-42?tab=events&cursor=next#step-9'
    );

    expect(router.serializeUrl(result as UrlTree)).toBe('/deployments/targets/target-42?tab=events&cursor=next#step-9');
  });

  it('navigates a legacy target URL through a valid terminal route', async () => {
    router.resetConfig([{path: 'deployments', children: deploymentsRoute().children}]);

    await router.navigateByUrl('/deployments/target-42?tab=events&cursor=next#step-9');

    expect(router.url).toBe('/deployments/targets/target-42?tab=events&cursor=next#step-9');
  });

  it('keeps the existing registry and target snapshot setup children compatible', () => {
    const setup = loggedInChildren().find((candidate) => candidate.path === 'setup');
    const setupChildren = setup?.children ?? [];

    expect(setupChildren.map((child) => child.path)).toEqual(['', 'registry', 'config-snapshots']);
    expect(setupChildren.find((child) => child.path === 'config-snapshots')?.component).toBe(
      TargetConfigSnapshotsComponent
    );
  });

  it('redirects a non-vendor away from the operator control plane', async () => {
    auth.isVendor.mockReturnValue(false);
    const [result] = await evaluateGuards(operatorRoute('/fleet').canActivate, '/fleet');

    expect(router.serializeUrl(result as UrlTree)).toBe('/');
  });

  for (const disabledFeature of ['operator_control_plane_v2', 'deployment_processes', 'scoped_variables_v2'] as const) {
    it(`redirects when the ${disabledFeature} feature is disabled`, async () => {
      featureFlags.isExperimentalFeatureEnabled$.mockImplementation((feature: string) =>
        of(feature !== disabledFeature)
      );

      const results = await evaluateGuards(operatorRoute('/fleet').canActivate, '/fleet');

      expect(results.some((result) => result instanceof UrlTree)).toBe(true);
      expect(featureFlags.isExperimentalFeatureEnabled$).toHaveBeenCalledWith(disabledFeature);
    });
  }

  function loggedInChildren(): Route[] {
    return routes[0].children ?? [];
  }

  function deploymentsRoute(): Route {
    const route = loggedInChildren().find((candidate) => candidate.path === 'deployments');
    expect(route).toBeDefined();
    return route!;
  }

  function operatorRoute(path: string): Route {
    const route = path.startsWith('/deployments/')
      ? deploymentsRoute().children?.find((candidate) => candidate.path === path.slice('/deployments/'.length))
      : loggedInChildren().find((candidate) => candidate.path === path.slice(1));
    expect(route).toBeDefined();
    return route!;
  }

  function operatorPaths(): string[] {
    const deployments = deploymentsRoute().children ?? [];
    const releases = loggedInChildren().find((candidate) => candidate.path === 'releases')?.children ?? [];
    const setup = loggedInChildren().find((candidate) => candidate.path === 'setup')?.children ?? [];
    return [
      ...loggedInChildren()
        .filter((route) => ['fleet', 'approvals', 'reconciliation', 'audit'].includes(route.path ?? ''))
        .map((route) => `/${route.path}`),
      ...releases.map((route) => `/releases${route.path ? `/${route.path}` : ''}`),
      ...deployments
        .filter((route) =>
          [
            'plans',
            'plans/:planId',
            'campaigns',
            'campaigns/:campaignId',
            'executions',
            'executions/:executionId',
          ].includes(route.path ?? '')
        )
        .map((route) => `/deployments/${route.path}`),
      ...setup.filter((route) => route.path === '').map(() => '/setup'),
    ].sort((left, right) => {
      const order = [
        '/fleet',
        '/releases',
        '/releases/:releaseId',
        '/deployments/plans',
        '/deployments/plans/:planId',
        '/deployments/campaigns',
        '/deployments/campaigns/:campaignId',
        '/deployments/executions',
        '/deployments/executions/:executionId',
        '/approvals',
        '/reconciliation',
        '/audit',
        '/setup',
      ];
      return order.indexOf(left) - order.indexOf(right);
    });
  }

  async function evaluateGuards(guards: Route['canActivate'], url: string): Promise<(boolean | UrlTree)[]> {
    const results: (boolean | UrlTree)[] = [];
    for (const guard of guards ?? []) {
      const result = TestBed.runInInjectionContext(() =>
        (guard as CanActivateFn)({params: {deploymentTargetId: 'target-42'}} as any, {url} as RouterStateSnapshot)
      );
      results.push(await Promise.resolve(result as boolean | UrlTree | Promise<boolean | UrlTree>));
    }
    return results;
  }
});

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
    const route = routes[0].children
      ?.find((candidate) => candidate.path === 'setup')
      ?.children?.find((candidate) => candidate.path === 'config-snapshots');
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
