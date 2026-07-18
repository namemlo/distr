import {signal} from '@angular/core';
import {ComponentFixture, TestBed} from '@angular/core/testing';
import {provideRouter} from '@angular/router';
import {UserRole} from '@distr-sh/distr-sdk';
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

describe('SideBarComponent target configuration history link', () => {
  for (const role of ['read_only', 'read_write', 'admin'] satisfies UserRole[]) {
    it(`shows the vendor history link to ${role} when operator mutations are disabled`, async () => {
      const {fixture} = await createComponent({role});

      expect(targetConfigLinks(fixture).length).toBe(1);
    });
  }

  it('shows the vendor history link to a super administrator when operator mutations are disabled', async () => {
    const {fixture} = await createComponent({role: 'admin', superAdmin: true});

    expect(targetConfigLinks(fixture).length).toBe(1);
  });

  it('does not expose the vendor history link in partner navigation even when operator mutations are enabled', async () => {
    const {fixture} = await createComponent({role: 'admin', partner: true, mutationFlag: true});

    expect(targetConfigLinks(fixture).length).toBe(0);
  });

  async function createComponent(options: {
    role: UserRole;
    superAdmin?: boolean;
    partner?: boolean;
    mutationFlag?: boolean;
  }): Promise<{
    fixture: ComponentFixture<SideBarComponent>;
    featureFlags: ReturnType<typeof featureFlagStub>;
  }> {
    const featureFlags = featureFlagStub(options.mutationFlag ?? false);
    const isPartner = options.partner ?? false;
    const auth = {
      isVendor: vi.fn(() => !isPartner),
      isPartner: vi.fn(() => isPartner),
      isCustomer: vi.fn(() => false),
      isSuperAdmin: vi.fn(() => options.superAdmin ?? false),
      hasRole: vi.fn((role: UserRole) => role === options.role),
      hasAnyRole: vi.fn((...roles: UserRole[]) => roles.includes(options.role)),
    };
    TestBed.configureTestingModule({
      imports: [SideBarComponent],
      providers: [
        provideRouter([]),
        {provide: AuthService, useValue: auth},
        {
          provide: OrganizationService,
          useValue: {hasNoSubscription: signal(false)},
        },
        {
          provide: TutorialsService,
          useValue: {allStarted$: of(true)},
        },
        {
          provide: ContextService,
          useValue: {
            getCustomerOrganization: vi.fn(() => of(undefined)),
            getSidebarLinks: vi.fn(() => of([])),
          },
        },
        {provide: FeatureFlagService, useValue: featureFlags},
      ],
    });
    const fixture = TestBed.createComponent(SideBarComponent);
    fixture.componentRef.setInput('isSidebarVisible', true);
    fixture.componentRef.setInput('isSubscriptionBannerVisible', false);
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();
    return {fixture, featureFlags};
  }

  function featureFlagStub(mutationFlag: boolean) {
    const disabled$ = of(false);
    return {
      isLicensingEnabled$: disabled$,
      isNotificationsEnabled$: disabled$,
      isSupportBundlesEnabled$: disabled$,
      isVendorBillingEnabled: signal(false),
      isPartnerManagementEnabled: signal(false),
      isEnvironmentsEnabled$: disabled$,
      isLifecyclesEnabled$: disabled$,
      isChannelsEnabled$: disabled$,
      isReleaseBundlesEnabled$: disabled$,
      isDeploymentProcessesEnabled$: disabled$,
      isStepTemplatesEnabled$: disabled$,
      isRunbooksEnabled$: disabled$,
      isDeploymentPlansEnabled$: disabled$,
      isTaskQueueEnabled$: disabled$,
      isDeploymentTimelineEnabled$: disabled$,
      isScopedVariablesV2Enabled$: disabled$,
      isExperimentalFeatureEnabled$: vi.fn((_key: string) => of(mutationFlag)),
    };
  }

  function targetConfigLinks(fixture: ComponentFixture<SideBarComponent>): HTMLAnchorElement[] {
    return Array.from(fixture.nativeElement.querySelectorAll('a') as NodeListOf<HTMLAnchorElement>).filter((link) =>
      link.textContent?.includes('Target config snapshots')
    );
  }
});
