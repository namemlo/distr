import {inject} from '@angular/core';
import {CanActivateFn, Router, Routes} from '@angular/router';
import {UserRole} from '@distr-sh/distr-sdk';
import {catchError, firstValueFrom, map, of} from 'rxjs';
import {getRemoteEnvironment} from '../env/remote';
import {AccessTokensComponent} from './access-tokens/access-tokens.component';
import {AlertConfigurationsComponent} from './alert-configurations/alert-configurations.component';
import {ApplicationDetailComponent} from './applications/application-detail.component';
import {ApplicationsPageComponent} from './applications/applications-page.component';
import {ArtifactPullsComponent} from './artifacts/artifact-pulls/artifact-pulls.component';
import {ArtifactVersionsComponent} from './artifacts/artifact-versions/artifact-versions.component';
import {ArtifactsComponent} from './artifacts/artifacts/artifacts.component';
import {BillingComponent} from './billing/billing.component';
import {BillingSettingsComponent} from './billing/settings/billing-settings.component';
import {ChannelsComponent} from './channels/channels.component';
import {CustomerOrganizationsComponent} from './components/customer-organizations/customer-organizations.component';
import {DashboardComponent} from './components/dashboard/dashboard.component';
import {HomeComponent} from './components/home/home.component';
import {PartnerOrganizationsComponent} from './components/partner-organizations/partner-organizations.component';
import {CustomerUsersComponent} from './components/users/customers/customer-users.component';
import {PartnerUsersComponent} from './components/users/partners/partner-users.component';
import {VendorUsersComponent} from './components/users/vendors/vendor-users.component';
import {DeploymentPlansComponent} from './deployment-plans/deployment-plans.component';
import {DeploymentProcessesComponent} from './deployment-processes/deployment-processes.component';
import {DeploymentTimelineComponent} from './deployment-timeline/deployment-timeline.component';
import {DeploymentTargetDetailComponent} from './deployments/deployment-target-details/deployment-target-detail.component';
import {DeploymentTargetsComponent} from './deployments/deployment-targets.component';
import {EnvironmentsComponent} from './environments/environments.component';
import {CustomerLicenseDetailComponent} from './licenses/customer-license-detail.component';
import {LicenseKeysComponent} from './licenses/license-keys/license-keys.component';
import {LicensesOverviewComponent} from './licenses/licenses-overview.component';
import {LifecyclesComponent} from './lifecycles/lifecycles.component';
import {NotificationRecordsComponent} from './notification-records/notification-records.component';
import {OrganizationBrandingComponent} from './organization-branding/organization-branding.component';
import {OrganizationSettingsComponent} from './organization-settings/organization-settings.component';
import {ReleaseBundlesComponent} from './release-bundles/release-bundles.component';
import {RunbooksComponent} from './runbooks/runbooks.component';
import {CustomerSecretsPageComponent} from './secrets/customer-secrets-page.component';
import {SecretsPage} from './secrets/secrets-page.component';
import {AuthService} from './services/auth.service';
import {FeatureFlagService} from './services/feature-flag.service';
import {OrganizationService} from './services/organization.service';
import {ToastService} from './services/toast.service';
import {deploymentRegistryMutationGuard} from './setup/registry/deployment-registry-access';
import {DeploymentRegistryComponent} from './setup/registry/deployment-registry.component';
import {SidebarLinksPageComponent} from './sidebar-links/sidebar-links-page.component';
import {StepTemplatesComponent} from './step-templates/step-templates.component';
import {SubscriptionCallbackComponent} from './subscription/subscription-callback.component';
import {SubscriptionComponent} from './subscription/subscription.component';
import {SupportBundleDetailComponent} from './support-bundles/detail/support-bundle-detail.component';
import {SupportBundleListComponent} from './support-bundles/list/support-bundle-list.component';
import {SupportBundleSettingsComponent} from './support-bundles/vendor/support-bundle-settings.component';
import {AgentsTutorialComponent} from './tutorials/agents/agents-tutorial.component';
import {BrandingTutorialComponent} from './tutorials/branding/branding-tutorial.component';
import {RegistryTutorialComponent} from './tutorials/registry/registry-tutorial.component';
import {TutorialsComponent} from './tutorials/tutorials.component';
import {ExperimentalFeatureFlagKey} from './types/feature-flags';
import {isSubscriptionExpired} from './types/organization';
import {UserSettingsComponent} from './user-settings/user-settings.component';
import {VariableSetsComponent} from './variable-sets/variable-sets.component';

function requiredRoleGuard(...userRole: UserRole[]): CanActivateFn {
  return () => {
    const auth = inject(AuthService);
    if (auth.isSuperAdmin() || auth.hasAnyRole(...userRole)) {
      return true;
    }
    return inject(Router).createUrlTree(['/']);
  };
}

const requireVendor: CanActivateFn = () => {
  if (inject(AuthService).isVendor()) {
    return true;
  }
  return inject(Router).createUrlTree(['/']);
};

const requireCustomer: CanActivateFn = () => {
  if (inject(AuthService).isCustomer()) {
    return true;
  }
  return inject(Router).createUrlTree(['/']);
};

const requireVendorOrPartner: CanActivateFn = () => {
  const auth = inject(AuthService);
  if (auth.isVendor() || auth.isPartner()) {
    return true;
  }
  return inject(Router).createUrlTree(['/']);
};

function licensingEnabledGuard(): CanActivateFn {
  return async () => {
    const featureFlags = inject(FeatureFlagService);
    return await firstValueFrom(featureFlags.isLicensingEnabled$);
  };
}

function notificationsEnabledGuard(): CanActivateFn {
  return async () => {
    const featureFlags = inject(FeatureFlagService);
    return await firstValueFrom(featureFlags.isNotificationsEnabled$);
  };
}

function supportBundlesEnabledGuard(): CanActivateFn {
  return async () => {
    const featureFlags = inject(FeatureFlagService);
    return await firstValueFrom(featureFlags.isSupportBundlesEnabled$);
  };
}

function experimentalFeatureEnabledGuard(key: ExperimentalFeatureFlagKey): CanActivateFn {
  return async () => {
    const featureFlags = inject(FeatureFlagService);
    const router = inject(Router);
    const enabled = await firstValueFrom(
      featureFlags.isExperimentalFeatureEnabled$(key).pipe(catchError(() => of(false)))
    );
    return enabled ? true : router.createUrlTree(['/']);
  };
}

function vendorBillingEnabledGuard(): CanActivateFn {
  return async () => {
    const featureFlags = inject(FeatureFlagService);
    return await firstValueFrom(featureFlags.isVendorBillingEnabled$);
  };
}

function partnerManagementEnabledGuard(): CanActivateFn {
  return async () => {
    const featureFlags = inject(FeatureFlagService);
    return await firstValueFrom(featureFlags.isPartnerManagementEnabled$);
  };
}

function registryHostSetOrRedirectGuard(redirectTo: string): CanActivateFn {
  return async () => {
    const router = inject(Router);
    const toast = inject(ToastService);
    const env = await getRemoteEnvironment();
    if ((env.registryHost ?? '').length > 0) {
      return true;
    }
    toast.error('Registry must be enabled first!');
    return router.createUrlTree([redirectTo]);
  };
}

function subscriptionGuard(): CanActivateFn {
  return () => {
    const auth = inject(AuthService);
    const router = inject(Router);
    const organizationService = inject(OrganizationService);
    return (
      auth.isCustomer() ||
      organizationService
        .get()
        .pipe(map((org) => (isSubscriptionExpired(org) ? router.createUrlTree(['/subscription']) : true)))
    );
  };
}

export const routes: Routes = [
  {
    path: '',
    canActivate: [subscriptionGuard()],
    children: [
      {
        path: 'dashboard',
        component: DashboardComponent,
        canActivate: [requireVendorOrPartner],
      },
      {
        path: 'home',
        component: HomeComponent,
        canActivate: [requireCustomer],
      },
      {
        path: 'applications',
        canActivate: [requireVendor],
        children: [
          {
            path: '',
            pathMatch: 'full',
            component: ApplicationsPageComponent,
          },
          {
            path: ':applicationId',
            component: ApplicationDetailComponent,
          },
        ],
      },
      {
        path: 'deployments',
        children: [
          {path: '', pathMatch: 'full', component: DeploymentTargetsComponent},
          {path: ':deploymentTargetId', component: DeploymentTargetDetailComponent},
        ],
      },
      {
        path: 'environments',
        component: EnvironmentsComponent,
        canActivate: [requireVendor, requiredRoleGuard('admin'), experimentalFeatureEnabledGuard('environments')],
      },
      {
        path: 'lifecycles',
        component: LifecyclesComponent,
        canActivate: [
          requireVendor,
          requiredRoleGuard('admin'),
          experimentalFeatureEnabledGuard('environments'),
          experimentalFeatureEnabledGuard('lifecycles'),
        ],
      },
      {
        path: 'channels',
        component: ChannelsComponent,
        canActivate: [
          requireVendor,
          requiredRoleGuard('admin'),
          experimentalFeatureEnabledGuard('environments'),
          experimentalFeatureEnabledGuard('lifecycles'),
          experimentalFeatureEnabledGuard('channels'),
        ],
      },
      {
        path: 'release-bundles',
        component: ReleaseBundlesComponent,
        canActivate: [
          requireVendor,
          requiredRoleGuard('admin'),
          experimentalFeatureEnabledGuard('environments'),
          experimentalFeatureEnabledGuard('lifecycles'),
          experimentalFeatureEnabledGuard('channels'),
          experimentalFeatureEnabledGuard('release_bundles'),
        ],
      },
      {
        path: 'deployment-processes',
        component: DeploymentProcessesComponent,
        canActivate: [
          requireVendor,
          requiredRoleGuard('admin'),
          experimentalFeatureEnabledGuard('environments'),
          experimentalFeatureEnabledGuard('lifecycles'),
          experimentalFeatureEnabledGuard('channels'),
          experimentalFeatureEnabledGuard('deployment_processes'),
        ],
      },
      {
        path: 'step-templates',
        component: StepTemplatesComponent,
        canActivate: [
          requireVendor,
          requiredRoleGuard('admin'),
          experimentalFeatureEnabledGuard('environments'),
          experimentalFeatureEnabledGuard('lifecycles'),
          experimentalFeatureEnabledGuard('channels'),
          experimentalFeatureEnabledGuard('deployment_processes'),
          experimentalFeatureEnabledGuard('step_templates'),
        ],
      },
      {
        path: 'runbooks',
        component: RunbooksComponent,
        canActivate: [requireVendor, requiredRoleGuard('admin'), experimentalFeatureEnabledGuard('runbooks')],
      },
      {
        path: 'deployment-plans',
        component: DeploymentPlansComponent,
        canActivate: [
          requireVendor,
          requiredRoleGuard('admin'),
          experimentalFeatureEnabledGuard('environments'),
          experimentalFeatureEnabledGuard('lifecycles'),
          experimentalFeatureEnabledGuard('channels'),
          experimentalFeatureEnabledGuard('release_bundles'),
          experimentalFeatureEnabledGuard('deployment_processes'),
          experimentalFeatureEnabledGuard('scoped_variables_v2'),
          experimentalFeatureEnabledGuard('deployment_plans'),
        ],
      },
      {
        path: 'deployment-timeline',
        component: DeploymentTimelineComponent,
        canActivate: [
          requireVendor,
          requiredRoleGuard('admin'),
          experimentalFeatureEnabledGuard('environments'),
          experimentalFeatureEnabledGuard('lifecycles'),
          experimentalFeatureEnabledGuard('channels'),
          experimentalFeatureEnabledGuard('release_bundles'),
          experimentalFeatureEnabledGuard('deployment_processes'),
          experimentalFeatureEnabledGuard('scoped_variables_v2'),
          experimentalFeatureEnabledGuard('deployment_plans'),
          experimentalFeatureEnabledGuard('task_queue'),
          experimentalFeatureEnabledGuard('deployment_timeline'),
        ],
      },
      {
        path: 'variable-sets',
        component: VariableSetsComponent,
        canActivate: [
          requireVendor,
          requiredRoleGuard('admin'),
          experimentalFeatureEnabledGuard('scoped_variables_v2'),
        ],
      },
      {
        path: 'setup/registry',
        component: DeploymentRegistryComponent,
        canActivate: [
          requireVendor,
          deploymentRegistryMutationGuard,
          experimentalFeatureEnabledGuard('operator_control_plane_v2'),
        ],
      },
      {
        path: 'artifacts',
        children: [
          {path: '', pathMatch: 'full', component: ArtifactsComponent},
          {path: ':id', component: ArtifactVersionsComponent},
        ],
      },
      {
        path: 'artifact-pulls',
        component: ArtifactPullsComponent,
        canActivate: [requireVendorOrPartner],
      },
      {
        path: 'customers',
        component: CustomerOrganizationsComponent,
        canActivate: [requireVendorOrPartner],
      },
      {
        path: 'customers/:customerOrganizationId',
        canActivate: [requireVendorOrPartner],
        children: [
          {path: 'users', component: CustomerUsersComponent},
          {path: 'secrets', component: CustomerSecretsPageComponent},
          {path: 'links', component: SidebarLinksPageComponent},
          {path: '', pathMatch: 'full', redirectTo: 'users'},
        ],
      },
      {
        path: 'partners',
        component: PartnerOrganizationsComponent,
        canActivate: [requireVendor, partnerManagementEnabledGuard()],
      },
      {
        path: 'partners/:partnerOrganizationId',
        canActivate: [requireVendor, partnerManagementEnabledGuard()],
        children: [
          {path: 'users', component: PartnerUsersComponent},
          {path: '', pathMatch: 'full', redirectTo: 'users'},
        ],
      },
      {
        path: 'users',
        component: VendorUsersComponent,
        canActivate: [requiredRoleGuard('admin')],
      },
      {
        path: 'secrets',
        component: SecretsPage,
      },
      {
        path: 'license-keys',
        component: LicenseKeysComponent,
        canActivate: [requireCustomer, licensingEnabledGuard()],
      },
      {
        path: 'branding',
        component: OrganizationBrandingComponent,
        data: {userRole: 'vendor'},
        canActivate: [requireVendor, requiredRoleGuard('read_write', 'admin')],
      },
      {
        path: 'billing',
        canActivate: [requireVendor, vendorBillingEnabledGuard()],
        children: [
          {
            path: '',
            pathMatch: 'full',
            component: BillingComponent,
          },
          {
            path: 'settings',
            component: BillingSettingsComponent,
            canActivate: [requiredRoleGuard('admin')],
          },
        ],
      },
      {
        path: 'licenses',
        canActivate: [requireVendorOrPartner, licensingEnabledGuard()],
        data: {userRole: 'vendor'},
        children: [
          {
            path: '',
            pathMatch: 'full',
            component: LicensesOverviewComponent,
          },
          {
            path: ':customerOrganizationId',
            component: CustomerLicenseDetailComponent,
          },
        ],
      },
      {
        path: 'settings',
        children: [
          {
            path: '',
            pathMatch: 'full',
            redirectTo: 'organization',
          },
          {
            path: 'organization',
            component: OrganizationSettingsComponent,
            data: {userRole: 'vendor'},
            canActivate: [requireVendor, requiredRoleGuard('admin')],
          },
          {
            path: 'profile',
            component: UserSettingsComponent,
          },
          {
            path: 'access-tokens',
            component: AccessTokensComponent,
          },
        ],
      },
      {
        path: 'tutorials',
        canActivate: [requireVendor, requiredRoleGuard('admin')],
        children: [
          {
            path: '',
            pathMatch: 'full',
            component: TutorialsComponent,
          },
          {
            path: 'agents',
            component: AgentsTutorialComponent,
          },
          {
            path: 'branding',
            component: BrandingTutorialComponent,
          },
          {
            path: 'registry',
            canActivate: [registryHostSetOrRedirectGuard('/tutorials')],
            component: RegistryTutorialComponent,
          },
        ],
      },
      {
        path: 'notifications',
        canActivate: [notificationsEnabledGuard()],
        children: [
          {
            path: 'alert-configurations',
            component: AlertConfigurationsComponent,
          },
          {
            path: 'history',
            component: NotificationRecordsComponent,
          },
        ],
      },
      {
        path: 'support-bundles',
        canActivate: [requireVendorOrPartner, supportBundlesEnabledGuard()],
        children: [
          {
            path: '',
            pathMatch: 'full',
            component: SupportBundleListComponent,
          },
          {
            path: 'settings',
            component: SupportBundleSettingsComponent,
            canActivate: [requireVendor, requiredRoleGuard('read_write', 'admin')],
          },
          {
            path: ':supportBundleId',
            component: SupportBundleDetailComponent,
          },
        ],
      },
      {
        path: 'support',
        canActivate: [requireCustomer],
        children: [
          {
            path: '',
            pathMatch: 'full',
            component: SupportBundleListComponent,
          },
          {
            path: ':supportBundleId',
            component: SupportBundleDetailComponent,
          },
        ],
      },
    ],
  },
  {
    path: 'subscription',
    canActivate: [requireVendor],
    children: [
      {
        path: '',
        pathMatch: 'full',
        component: SubscriptionComponent,
      },
      {
        path: 'callback',
        component: SubscriptionCallbackComponent,
      },
    ],
  },
];
