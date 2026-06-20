import {HttpErrorResponse} from '@angular/common/http';
import {ComponentFixture, TestBed} from '@angular/core/testing';
import {Application} from '@distr-sh/distr-sdk';
import {of, throwError} from 'rxjs';
import {vi} from 'vitest';
import {ApplicationsService} from '../services/applications.service';
import {ChannelsService} from '../services/channels.service';
import {OverlayService} from '../services/overlay.service';
import {ReleaseBundlesService} from '../services/release-bundles.service';
import {ToastService} from '../services/toast.service';
import {Channel} from '../types/channel';
import {ReleaseBundle} from '../types/release-bundle';
import {ReleaseBundlesComponent} from './release-bundles.component';

describe('ReleaseBundlesComponent', () => {
  let releaseBundlesService: any;
  let applicationsService: any;
  let channelsService: any;
  let overlay: any;
  let toast: any;

  const applications = [
    {
      id: 'application-1',
      name: 'Payments',
      type: 'docker',
      versions: [
        {id: 'version-1', name: '1.2.3', applicationId: 'application-1'},
        {id: 'version-2', name: '1.2.4', applicationId: 'application-1'},
      ],
    },
  ] as Application[];
  const channels: Channel[] = [
    {
      id: 'channel-1',
      createdAt: '2026-06-20T09:30:00Z',
      updatedAt: '2026-06-20T10:45:00Z',
      applicationId: 'application-1',
      lifecycleId: 'lifecycle-1',
      name: 'Stable',
      description: '',
      sortOrder: 10,
      isDefault: true,
      allowedVersionRanges: [],
      allowedPrereleasePatterns: [],
      allowedSourceBranches: [],
      allowedSourceTags: [],
    },
  ];
  const bundles: ReleaseBundle[] = [
    {
      id: 'bundle-1',
      createdAt: '2026-06-21T08:00:00Z',
      updatedAt: '2026-06-21T08:00:00Z',
      applicationId: 'application-1',
      channelId: 'channel-1',
      releaseNumber: '2026.06.21',
      releaseNotes: 'Initial release',
      sourceRevision: 'abc123',
      status: 'DRAFT',
      canonicalChecksum: 'sha256:abc',
      components: [
        {
          id: 'component-1',
          releaseBundleId: 'bundle-1',
          key: 'api',
          name: 'API',
          type: 'application_version',
          version: '1.2.3',
          applicationVersionId: 'version-1',
          packageRef: '',
          digest: '',
          checksum: '',
        },
      ],
    },
    {
      id: 'bundle-2',
      createdAt: '2026-06-21T09:00:00Z',
      updatedAt: '2026-06-21T09:00:00Z',
      applicationId: 'application-1',
      channelId: 'channel-1',
      releaseNumber: '2026.06.22',
      releaseNotes: 'Published release',
      sourceRevision: 'def456',
      status: 'PUBLISHED',
      publishedByUserAccountId: 'user-1',
      publishedAt: '2026-06-21T09:30:00Z',
      canonicalChecksum: 'sha256:def',
      components: [],
    },
  ];

  beforeEach(() => {
    releaseBundlesService = {
      list: vi.fn(),
      create: vi.fn(),
      update: vi.fn(),
      delete: vi.fn(),
      validate: vi.fn(),
      publish: vi.fn(),
      block: vi.fn(),
      archive: vi.fn(),
    };
    applicationsService = {
      list: vi.fn(),
    };
    channelsService = {
      list: vi.fn(),
    };
    overlay = {
      showModal: vi.fn(),
      confirm: vi.fn(),
    };
    toast = {
      error: vi.fn(),
      success: vi.fn(),
    };

    releaseBundlesService.list.mockReturnValue(of(bundles));
    releaseBundlesService.create.mockReturnValue(of(bundles[0]));
    releaseBundlesService.update.mockReturnValue(of(bundles[0]));
    releaseBundlesService.delete.mockReturnValue(of(undefined));
    releaseBundlesService.validate.mockReturnValue(of({valid: true, errors: [], warnings: []}));
    releaseBundlesService.publish.mockReturnValue(of({...bundles[0], status: 'PUBLISHED'}));
    releaseBundlesService.block.mockReturnValue(of({...bundles[1], status: 'BLOCKED'}));
    releaseBundlesService.archive.mockReturnValue(of({...bundles[1], status: 'ARCHIVED'}));
    applicationsService.list.mockReturnValue(of(applications));
    channelsService.list.mockReturnValue(of(channels));
    overlay.showModal.mockReturnValue({close: vi.fn()} as any);
    overlay.confirm.mockReturnValue(of(true));

    TestBed.configureTestingModule({
      imports: [ReleaseBundlesComponent],
      providers: [
        {provide: ReleaseBundlesService, useValue: releaseBundlesService},
        {provide: ApplicationsService, useValue: applicationsService},
        {provide: ChannelsService, useValue: channelsService},
        {provide: OverlayService, useValue: overlay},
        {provide: ToastService, useValue: toast},
      ],
    });
  });

  it('loads release bundles with application and channel lookup data', () => {
    const {component} = createComponent();

    expect((component as any).releaseBundles()).toEqual(bundles);
    expect((component as any).applicationName('application-1')).toBe('Payments');
    expect((component as any).channelName('channel-1')).toBe('Stable');
  });

  it('shows load errors', () => {
    releaseBundlesService.list.mockReturnValue(
      throwError(() => new HttpErrorResponse({status: 400, error: 'Could not load release bundles'}))
    );

    const {component} = createComponent();

    expect((component as any).loadError()).toBe('Could not load release bundles');
  });

  it('creates draft release bundles with components', async () => {
    const {component} = createComponent();

    (component as any).showCreateDialog();
    (component as any).releaseBundleForm.patchValue({
      releaseNumber: '2026.06.23',
      releaseNotes: 'Next release',
      sourceRevision: 'main@123',
    });
    (component as any).componentsArray.at(0).patchValue({
      key: 'api',
      name: 'API',
      type: 'application_version',
      version: '1.2.3',
      applicationVersionId: 'version-1',
    });
    await (component as any).submitForm();

    expect(releaseBundlesService.create).toHaveBeenCalledWith({
      applicationId: 'application-1',
      channelId: 'channel-1',
      releaseNumber: '2026.06.23',
      releaseNotes: 'Next release',
      sourceRevision: 'main@123',
      components: [
        {
          key: 'api',
          name: 'API',
          type: 'application_version',
          version: '1.2.3',
          applicationVersionId: 'version-1',
          packageRef: '',
          digest: '',
          checksum: '',
          childReleaseBundleId: undefined,
        },
      ],
    });
  });

  it('updates draft release bundles and loads component fields into the editor', async () => {
    const {component} = createComponent();

    (component as any).showUpdateDialog(bundles[0]);
    expect((component as any).componentsArray.at(0).value.key).toBe('api');

    (component as any).releaseBundleForm.patchValue({releaseNotes: 'Edited'});
    await (component as any).submitForm();

    expect(releaseBundlesService.update).toHaveBeenCalledWith('bundle-1', {
      applicationId: 'application-1',
      channelId: 'channel-1',
      releaseNumber: '2026.06.21',
      releaseNotes: 'Edited',
      sourceRevision: 'abc123',
      components: [
        {
          key: 'api',
          name: 'API',
          type: 'application_version',
          version: '1.2.3',
          applicationVersionId: 'version-1',
          packageRef: '',
          digest: '',
          checksum: '',
          childReleaseBundleId: undefined,
        },
      ],
    });
  });

  it('shows read-only detail state for non-draft releases', () => {
    const {component} = createComponent();

    (component as any).showDetailDialog(bundles[1]);

    expect((component as any).selectedReleaseBundle()).toEqual(bundles[1]);
    expect((component as any).isSelectedDraft()).toBe(false);
    expect(overlay.showModal).toHaveBeenCalled();
  });

  it('validates before publishing and requires confirmation', async () => {
    const {component} = createComponent();

    await (component as any).publish(bundles[0]);

    expect(releaseBundlesService.validate).toHaveBeenCalledWith('bundle-1');
    expect(overlay.confirm).toHaveBeenCalled();
    expect(releaseBundlesService.publish).toHaveBeenCalledWith('bundle-1');
  });

  it('does not publish when validation returns errors', async () => {
    releaseBundlesService.validate.mockReturnValue(
      of({
        valid: false,
        errors: [{field: 'components.api.version', rule: '>=2.0.0', message: 'version does not match'}],
        warnings: [],
      })
    );
    const {component} = createComponent();

    await (component as any).publish(bundles[0]);

    expect((component as any).validationResults()['bundle-1'].errors[0].field).toBe('components.api.version');
    expect(overlay.confirm).not.toHaveBeenCalled();
    expect(releaseBundlesService.publish).not.toHaveBeenCalled();
  });

  it('confirms before block, archive, and draft delete actions', async () => {
    const {component} = createComponent();

    await (component as any).block(bundles[1]);
    await (component as any).archive(bundles[1]);
    (component as any).delete(bundles[0]);
    await Promise.resolve();

    expect(releaseBundlesService.block).toHaveBeenCalledWith('bundle-2');
    expect(releaseBundlesService.archive).toHaveBeenCalledWith('bundle-2');
    expect(releaseBundlesService.delete).toHaveBeenCalledWith('bundle-1');
  });

  function createComponent(): {fixture: ComponentFixture<ReleaseBundlesComponent>; component: ReleaseBundlesComponent} {
    const fixture = TestBed.createComponent(ReleaseBundlesComponent);
    fixture.detectChanges();
    return {fixture, component: fixture.componentInstance};
  }
});
