import {ComponentFixture, TestBed} from '@angular/core/testing';
import {of} from 'rxjs';
import {vi} from 'vitest';
import {ConfigAsCodeService} from '../services/config-as-code.service';
import {EnvironmentsService} from '../services/environments.service';
import {FeatureFlagService} from '../services/feature-flag.service';
import {LifecyclesService} from '../services/lifecycles.service';
import {OverlayService} from '../services/overlay.service';
import {ToastService} from '../services/toast.service';
import {ConfigAsCodeAuthority} from '../types/config-as-code';
import {Environment} from '../types/environment';
import {Lifecycle} from '../types/lifecycle';
import {LifecyclesComponent} from './lifecycles.component';

describe('LifecyclesComponent', () => {
  let lifecyclesService: any;
  let configAsCodeService: any;
  let featureFlagService: any;
  let environmentsService: any;
  let overlay: any;
  let toast: any;

  const lifecycles: Lifecycle[] = [
    {
      id: 'lifecycle-1',
      createdAt: '2026-06-20T09:30:00Z',
      updatedAt: '2026-06-20T10:45:00Z',
      name: 'Standard',
      description: 'Standard promotion path',
      sortOrder: 10,
      phases: [
        {
          id: 'phase-1',
          name: 'Production',
          description: '',
          sortOrder: 10,
          environmentIds: ['environment-1'],
          optional: false,
          automaticPromotion: false,
          minimumSuccessfulDeployments: 1,
        },
      ],
    },
  ];
  const environments: Environment[] = [
    {
      id: 'environment-1',
      createdAt: '2026-06-20T09:30:00Z',
      updatedAt: '2026-06-20T10:45:00Z',
      name: 'Production',
      description: '',
      sortOrder: 10,
      isProduction: true,
      allowDynamicTargets: false,
    },
  ];
  const gitManagedLifecycleAuthority: ConfigAsCodeAuthority = {
    resourceKind: 'Lifecycle',
    resourceId: 'lifecycle-1',
    authority: 'GIT_MANAGED',
    repositoryPath: 'lifecycles/standard.yaml',
    sourceRevision: '6dcb09f',
    documentChecksum: 'sha256:1234',
    updatedByUserId: 'user-1',
    updatedAt: '2026-06-20T11:00:00Z',
  };

  beforeEach(() => {
    lifecyclesService = {
      list: vi.fn(),
      create: vi.fn(),
      update: vi.fn(),
      delete: vi.fn(),
    };
    configAsCodeService = {
      listAuthorities: vi.fn(),
    };
    featureFlagService = {
      isConfigAsCodeEnabled$: of(false),
    };
    environmentsService = {
      list: vi.fn(),
    };
    overlay = {
      showModal: vi.fn(),
      confirm: vi.fn(),
    };
    toast = {
      error: vi.fn(),
    };

    lifecyclesService.list.mockReturnValue(of(lifecycles));
    lifecyclesService.create.mockReturnValue(of(lifecycles[0]));
    lifecyclesService.update.mockReturnValue(of(lifecycles[0]));
    lifecyclesService.delete.mockReturnValue(of(undefined));
    configAsCodeService.listAuthorities.mockReturnValue(of({authorities: []}));
    environmentsService.list.mockReturnValue(of(environments));
    overlay.showModal.mockReturnValue({close: vi.fn()} as any);
    overlay.confirm.mockReturnValue(of(true));

    TestBed.configureTestingModule({
      imports: [LifecyclesComponent],
      providers: [
        {provide: LifecyclesService, useValue: lifecyclesService},
        {provide: ConfigAsCodeService, useValue: configAsCodeService},
        {provide: FeatureFlagService, useValue: featureFlagService},
        {provide: EnvironmentsService, useValue: environmentsService},
        {provide: OverlayService, useValue: overlay},
        {provide: ToastService, useValue: toast},
      ],
    });
  });

  it('loads lifecycles with environment lookup data', () => {
    const {component} = createComponent();

    expect((component as any).lifecycles()).toEqual(lifecycles);
    expect((component as any).environmentNames(['environment-1'])).toBe('Production');
  });

  it('loads config-as-code authorities and blocks Git-managed lifecycle mutations', async () => {
    featureFlagService.isConfigAsCodeEnabled$ = of(true);
    configAsCodeService.listAuthorities.mockReturnValue(of({authorities: [gitManagedLifecycleAuthority]}));
    const {component} = createComponent();

    expect((component as any).authorityFor(lifecycles[0])).toEqual(gitManagedLifecycleAuthority);
    expect((component as any).isGitManaged(lifecycles[0])).toBe(true);

    (component as any).showUpdateDialog(lifecycles[0]);
    (component as any).delete(lifecycles[0]);
    await Promise.resolve();

    expect(overlay.showModal).not.toHaveBeenCalled();
    expect(overlay.confirm).not.toHaveBeenCalled();
    expect(lifecyclesService.delete).not.toHaveBeenCalled();
    expect(toast.error).toHaveBeenCalledWith('This lifecycle is managed from Git.');
  });

  function createComponent(): {fixture: ComponentFixture<LifecyclesComponent>; component: LifecyclesComponent} {
    const fixture = TestBed.createComponent(LifecyclesComponent);
    fixture.detectChanges();
    return {fixture, component: fixture.componentInstance};
  }
});
