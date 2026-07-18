import {signal} from '@angular/core';
import {TestBed} from '@angular/core/testing';
import {of} from 'rxjs';
import {vi} from 'vitest';
import {AuthService} from '../../services/auth.service';
import {ContextService} from '../../services/context.service';
import {FeatureFlagService} from '../../services/feature-flag.service';
import {OrganizationService} from '../../services/organization.service';
import {SidebarService} from '../../services/sidebar.service';
import {TutorialsService} from '../../services/tutorials.service';
import {SideBarComponent} from './side-bar.component';

describe('SideBarComponent deployment registry access', () => {
  for (const {name, role, superAdmin, visible} of [
    {name: 'vendor read write', role: 'read_write', superAdmin: false, visible: true},
    {name: 'vendor admin', role: 'admin', superAdmin: false, visible: true},
    {name: 'vendor read only', role: 'read_only', superAdmin: false, visible: false},
    {name: 'super admin', role: 'admin', superAdmin: true, visible: false},
  ] as const) {
    it(`${visible ? 'shows' : 'hides'} registry setup for ${name}`, () => {
      const auth = {
        isVendor: vi.fn(() => true),
        isSuperAdmin: vi.fn(() => superAdmin),
        hasRole: vi.fn((expected: string) => role === expected),
        hasAnyRole: vi.fn((...expected: string[]) => expected.includes(role)),
      };
      const enabled$ = of(true);
      const featureFlags = {
        isLicensingEnabled$: enabled$,
        isNotificationsEnabled$: enabled$,
        isSupportBundlesEnabled$: enabled$,
        isVendorBillingEnabled: signal(false),
        isPartnerManagementEnabled: signal(false),
        isEnvironmentsEnabled$: enabled$,
        isLifecyclesEnabled$: enabled$,
        isChannelsEnabled$: enabled$,
        isReleaseBundlesEnabled$: enabled$,
        isDeploymentProcessesEnabled$: enabled$,
        isStepTemplatesEnabled$: enabled$,
        isRunbooksEnabled$: enabled$,
        isDeploymentPlansEnabled$: enabled$,
        isTaskQueueEnabled$: enabled$,
        isDeploymentTimelineEnabled$: enabled$,
        isScopedVariablesV2Enabled$: enabled$,
        isExperimentalFeatureEnabled$: vi.fn(() => enabled$),
      };
      TestBed.configureTestingModule({
        providers: [
          {provide: AuthService, useValue: auth},
          {provide: FeatureFlagService, useValue: featureFlags},
          {provide: OrganizationService, useValue: {hasNoSubscription: signal(false)}},
          {
            provide: ContextService,
            useValue: {
              getCustomerOrganization: vi.fn(() => of(undefined)),
              getSidebarLinks: vi.fn(() => of([])),
            },
          },
          {provide: SidebarService, useValue: {hide: vi.fn()}},
          {provide: TutorialsService, useValue: {allStarted$: of(true)}},
        ],
      });

      const component = TestBed.runInInjectionContext(() => new SideBarComponent());

      expect((component as any).isOperatorControlPlaneV2FeatureEnabled()).toBe(visible);
    });
  }
});
