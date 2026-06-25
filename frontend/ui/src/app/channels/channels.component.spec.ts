import {HttpErrorResponse} from '@angular/common/http';
import {ComponentFixture, TestBed} from '@angular/core/testing';
import {Application} from '@distr-sh/distr-sdk';
import {of, throwError} from 'rxjs';
import {vi} from 'vitest';
import {ApplicationsService} from '../services/applications.service';
import {ChannelsService} from '../services/channels.service';
import {ConfigAsCodeService} from '../services/config-as-code.service';
import {FeatureFlagService} from '../services/feature-flag.service';
import {LifecyclesService} from '../services/lifecycles.service';
import {OverlayService} from '../services/overlay.service';
import {ToastService} from '../services/toast.service';
import {Channel} from '../types/channel';
import {ConfigAsCodeAuthority} from '../types/config-as-code';
import {Lifecycle} from '../types/lifecycle';
import {ChannelsComponent} from './channels.component';

describe('ChannelsComponent', () => {
  let channelsService: any;
  let configAsCodeService: any;
  let featureFlagService: any;
  let applicationsService: any;
  let lifecyclesService: any;
  let overlay: any;
  let toast: any;

  const channels: Channel[] = [
    {
      id: 'channel-1',
      createdAt: '2026-06-20T09:30:00Z',
      updatedAt: '2026-06-20T10:45:00Z',
      applicationId: 'application-1',
      lifecycleId: 'lifecycle-1',
      name: 'Stable',
      description: 'Default production-ready channel',
      sortOrder: 10,
      isDefault: true,
      allowedVersionRanges: ['>=1.0.0 <2.0.0'],
      allowedPrereleasePatterns: ['rc.*'],
      allowedSourceBranches: ['main', 'release/*'],
      allowedSourceTags: ['v*'],
    },
    {
      id: 'channel-2',
      createdAt: '2026-06-20T09:40:00Z',
      updatedAt: '2026-06-20T10:55:00Z',
      applicationId: 'application-1',
      lifecycleId: 'lifecycle-1',
      name: 'Hotfix',
      description: '',
      sortOrder: 20,
      isDefault: false,
      allowedVersionRanges: [],
      allowedPrereleasePatterns: [],
      allowedSourceBranches: [],
      allowedSourceTags: [],
    },
  ];
  const applications = [{id: 'application-1', name: 'Payments'}] as Application[];
  const gitManagedChannelAuthority: ConfigAsCodeAuthority = {
    resourceKind: 'Channel',
    resourceId: 'channel-2',
    authority: 'GIT_MANAGED',
    repositoryPath: 'channels/hotfix.yaml',
    sourceRevision: '6dcb09f',
    documentChecksum: 'sha256:1234',
    updatedByUserId: 'user-1',
    updatedAt: '2026-06-20T11:00:00Z',
  };
  const lifecycles: Lifecycle[] = [
    {
      id: 'lifecycle-1',
      createdAt: '2026-06-20T09:30:00Z',
      updatedAt: '2026-06-20T10:45:00Z',
      name: 'Standard',
      description: '',
      sortOrder: 10,
      phases: [],
    },
  ];

  beforeEach(() => {
    channelsService = {
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
    applicationsService = {
      list: vi.fn(),
    };
    lifecyclesService = {
      list: vi.fn(),
    };
    overlay = {
      showModal: vi.fn(),
      confirm: vi.fn(),
    };
    toast = {
      error: vi.fn(),
    };

    channelsService.list.mockReturnValue(of(channels));
    channelsService.create.mockReturnValue(of(channels[0]));
    channelsService.update.mockReturnValue(of(channels[0]));
    channelsService.delete.mockReturnValue(of(undefined));
    configAsCodeService.listAuthorities.mockReturnValue(of({authorities: []}));
    applicationsService.list.mockReturnValue(of(applications));
    lifecyclesService.list.mockReturnValue(of(lifecycles));
    overlay.showModal.mockReturnValue({close: vi.fn()} as any);
    overlay.confirm.mockReturnValue(of(true));

    TestBed.configureTestingModule({
      imports: [ChannelsComponent],
      providers: [
        {provide: ChannelsService, useValue: channelsService},
        {provide: ConfigAsCodeService, useValue: configAsCodeService},
        {provide: FeatureFlagService, useValue: featureFlagService},
        {provide: ApplicationsService, useValue: applicationsService},
        {provide: LifecyclesService, useValue: lifecyclesService},
        {provide: OverlayService, useValue: overlay},
        {provide: ToastService, useValue: toast},
      ],
    });
  });

  it('loads channels with application and lifecycle lookup data', () => {
    const {component} = createComponent();

    expect((component as any).channels()).toEqual(channels);
    expect((component as any).applicationName('application-1')).toBe('Payments');
    expect((component as any).lifecycleName('lifecycle-1')).toBe('Standard');
  });

  it('loads config-as-code authority badges and blocks Git-managed mutations', () => {
    featureFlagService.isConfigAsCodeEnabled$ = of(true);
    configAsCodeService.listAuthorities.mockReturnValue(of({authorities: [gitManagedChannelAuthority]}));
    const {component} = createComponent();

    expect((component as any).authorityFor(channels[1])).toEqual(gitManagedChannelAuthority);
    expect((component as any).isGitManaged(channels[1])).toBe(true);

    (component as any).showUpdateDialog(channels[1]);
    (component as any).delete(channels[1]);

    expect(overlay.showModal).not.toHaveBeenCalled();
    expect(overlay.confirm).not.toHaveBeenCalled();
    expect(toast.error).toHaveBeenCalledWith('This channel is managed from Git.');
  });

  it('shows load errors', () => {
    channelsService.list.mockReturnValue(
      throwError(() => new HttpErrorResponse({status: 400, error: 'Could not load channels'}))
    );

    const {component} = createComponent();

    expect((component as any).loadError()).toBe('Could not load channels');
  });

  it('creates channels with selected application and lifecycle references', async () => {
    const {component} = createComponent();

    (component as any).showCreateDialog();
    (component as any).channelForm.patchValue({
      name: 'Stable',
      description: 'Default production-ready channel',
      sortOrder: 10,
      isDefault: true,
      allowedVersionRangesText: '>=1.0.0 <2.0.0\n>=3.0.0 <4.0.0',
      allowedPrereleasePatternsText: 'rc.*',
      allowedSourceBranchesText: 'main\nrelease/*',
      allowedSourceTagsText: 'v*',
    });
    await (component as any).submitForm();

    expect(channelsService.create).toHaveBeenCalledWith({
      applicationId: 'application-1',
      lifecycleId: 'lifecycle-1',
      name: 'Stable',
      description: 'Default production-ready channel',
      sortOrder: 10,
      isDefault: true,
      allowedVersionRanges: ['>=1.0.0 <2.0.0', '>=3.0.0 <4.0.0'],
      allowedPrereleasePatterns: ['rc.*'],
      allowedSourceBranches: ['main', 'release/*'],
      allowedSourceTags: ['v*'],
    });
  });

  it('updates channels', async () => {
    const {component} = createComponent();

    (component as any).showUpdateDialog(channels[1]);
    (component as any).channelForm.patchValue({name: 'Urgent'});
    await (component as any).submitForm();

    expect(channelsService.update).toHaveBeenCalledWith('channel-2', {
      applicationId: 'application-1',
      lifecycleId: 'lifecycle-1',
      name: 'Urgent',
      description: '',
      sortOrder: 20,
      isDefault: false,
      allowedVersionRanges: [],
      allowedPrereleasePatterns: [],
      allowedSourceBranches: [],
      allowedSourceTags: [],
    });
  });

  it('loads channel rules into update form textareas', () => {
    const {component} = createComponent();

    (component as any).showUpdateDialog(channels[0]);

    expect((component as any).channelForm.value.allowedVersionRangesText).toBe('>=1.0.0 <2.0.0');
    expect((component as any).channelForm.value.allowedPrereleasePatternsText).toBe('rc.*');
    expect((component as any).channelForm.value.allowedSourceBranchesText).toBe('main\nrelease/*');
    expect((component as any).channelForm.value.allowedSourceTagsText).toBe('v*');
  });

  it('confirms before deleting channels', async () => {
    const {component} = createComponent();

    (component as any).delete(channels[1]);
    await Promise.resolve();

    expect(overlay.confirm).toHaveBeenCalled();
    expect(channelsService.delete).toHaveBeenCalledWith('channel-2');
  });

  function createComponent(): {fixture: ComponentFixture<ChannelsComponent>; component: ChannelsComponent} {
    const fixture = TestBed.createComponent(ChannelsComponent);
    fixture.detectChanges();
    return {fixture, component: fixture.componentInstance};
  }
});
