import {HttpErrorResponse} from '@angular/common/http';
import {ComponentFixture, TestBed} from '@angular/core/testing';
import {Application} from '@distr-sh/distr-sdk';
import {of, throwError} from 'rxjs';
import {vi} from 'vitest';
import {ApplicationsService} from '../services/applications.service';
import {ChannelsService} from '../services/channels.service';
import {DeploymentProcessesService} from '../services/deployment-processes.service';
import {EnvironmentsService} from '../services/environments.service';
import {OverlayService} from '../services/overlay.service';
import {ToastService} from '../services/toast.service';
import {Channel} from '../types/channel';
import {DeploymentProcess, DeploymentProcessRevision, DeploymentProcessStepRequest} from '../types/deployment-process';
import {Environment} from '../types/environment';
import {DeploymentProcessesComponent} from './deployment-processes.component';

describe('DeploymentProcessesComponent', () => {
  let deploymentProcessesService: any;
  let applicationsService: any;
  let channelsService: any;
  let environmentsService: any;
  let overlay: any;
  let toast: any;

  const applications = [
    {
      id: 'application-1',
      name: 'Payments',
      type: 'docker',
      versions: [],
    },
    {
      id: 'application-2',
      name: 'Portal',
      type: 'docker',
      versions: [],
    },
  ] as Application[];
  const channels: Channel[] = [
    {
      id: 'channel-1',
      createdAt: '2026-06-21T08:00:00Z',
      updatedAt: '2026-06-21T08:00:00Z',
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
    {
      id: 'channel-2',
      createdAt: '2026-06-21T08:00:00Z',
      updatedAt: '2026-06-21T08:00:00Z',
      applicationId: 'application-2',
      lifecycleId: 'lifecycle-1',
      name: 'Preview',
      description: '',
      sortOrder: 20,
      isDefault: true,
      allowedVersionRanges: [],
      allowedPrereleasePatterns: [],
      allowedSourceBranches: [],
      allowedSourceTags: [],
    },
  ];
  const environments: Environment[] = [
    {
      id: 'environment-1',
      createdAt: '2026-06-21T08:00:00Z',
      updatedAt: '2026-06-21T08:00:00Z',
      name: 'Production',
      description: '',
      sortOrder: 10,
      isProduction: true,
      allowDynamicTargets: false,
    },
  ];
  const processes: DeploymentProcess[] = [
    {
      id: 'process-1',
      createdAt: '2026-06-21T08:00:00Z',
      updatedAt: '2026-06-21T08:00:00Z',
      applicationId: 'application-1',
      name: 'Standard deploy',
      description: 'Deploys the release',
      sortOrder: 10,
    },
  ];
  const revisions: DeploymentProcessRevision[] = [
    {
      id: 'revision-1',
      createdAt: '2026-06-21T08:30:00Z',
      updatedAt: '2026-06-21T08:30:00Z',
      deploymentProcessId: 'process-1',
      revisionNumber: 1,
      description: 'Initial revision',
      steps: [
        {
          id: 'step-1',
          deploymentProcessRevisionId: 'revision-1',
          key: 'deploy',
          name: 'Deploy',
          actionType: 'distr.http.check',
          stepTemplateVersionId: 'template-version-1',
          executionLocation: 'target',
          inputBindings: {url: 'https://example.com/health'},
          condition: 'always',
          channelIds: ['channel-1'],
          environmentIds: ['environment-1'],
          targetTags: ['linux'],
          failureMode: 'fail',
          timeoutSeconds: 300,
          retryPolicy: {maxAttempts: 2, intervalSeconds: 30},
          requiredPermissions: ['deploy.write'],
          sortOrder: 10,
          dependencies: [],
        },
      ],
    },
  ];

  beforeEach(() => {
    deploymentProcessesService = {
      list: vi.fn(),
      create: vi.fn(),
      update: vi.fn(),
      delete: vi.fn(),
      listRevisions: vi.fn(),
      getRevision: vi.fn(),
      createRevision: vi.fn(),
    };
    applicationsService = {list: vi.fn()};
    channelsService = {list: vi.fn()};
    environmentsService = {list: vi.fn()};
    overlay = {
      showModal: vi.fn(),
      confirm: vi.fn(),
    };
    toast = {
      error: vi.fn(),
      success: vi.fn(),
    };

    deploymentProcessesService.list.mockReturnValue(of(processes));
    deploymentProcessesService.create.mockReturnValue(of(processes[0]));
    deploymentProcessesService.update.mockReturnValue(of(processes[0]));
    deploymentProcessesService.delete.mockReturnValue(of(undefined));
    deploymentProcessesService.listRevisions.mockReturnValue(of(revisions));
    deploymentProcessesService.getRevision.mockReturnValue(of(revisions[0]));
    deploymentProcessesService.createRevision.mockReturnValue(of(revisions[0]));
    applicationsService.list.mockReturnValue(of(applications));
    channelsService.list.mockReturnValue(of(channels));
    environmentsService.list.mockReturnValue(of(environments));
    overlay.showModal.mockReturnValue({close: vi.fn()} as any);
    overlay.confirm.mockReturnValue(of(true));

    TestBed.configureTestingModule({
      imports: [DeploymentProcessesComponent],
      providers: [
        {provide: DeploymentProcessesService, useValue: deploymentProcessesService},
        {provide: ApplicationsService, useValue: applicationsService},
        {provide: ChannelsService, useValue: channelsService},
        {provide: EnvironmentsService, useValue: environmentsService},
        {provide: OverlayService, useValue: overlay},
        {provide: ToastService, useValue: toast},
      ],
    });
  });

  it('loads deployment processes with application, channel, and environment lookup data', () => {
    const {component} = createComponent();

    expect((component as any).deploymentProcesses()).toEqual(processes);
    expect((component as any).applicationName('application-1')).toBe('Payments');
    expect((component as any).channelName('channel-1')).toBe('Stable');
    expect((component as any).environmentName('environment-1')).toBe('Production');
  });

  it('shows load errors', () => {
    deploymentProcessesService.list.mockReturnValue(
      throwError(() => new HttpErrorResponse({status: 400, error: 'Could not load deployment processes'}))
    );

    const {component} = createComponent();

    expect((component as any).loadError()).toBe('Could not load deployment processes');
  });

  it('creates deployment processes with selected application references', async () => {
    const {component} = createComponent();

    (component as any).showCreateProcessDialog();
    (component as any).processForm.patchValue({
      name: 'Canary deploy',
      description: 'Deploys a canary release',
      sortOrder: 20,
    });
    await (component as any).submitProcessForm();

    expect(deploymentProcessesService.create).toHaveBeenCalledWith({
      applicationId: 'application-1',
      name: 'Canary deploy',
      description: 'Deploys a canary release',
      sortOrder: 20,
    });
  });

  it('updates deployment processes', async () => {
    const {component} = createComponent();

    (component as any).showUpdateProcessDialog(processes[0]);
    (component as any).processForm.patchValue({description: 'Edited'});
    await (component as any).submitProcessForm();

    expect(deploymentProcessesService.update).toHaveBeenCalledWith('process-1', {
      applicationId: 'application-1',
      name: 'Standard deploy',
      description: 'Edited',
      sortOrder: 10,
    });
  });

  it('loads revision history for a selected process', async () => {
    const {component} = createComponent();

    await (component as any).showRevisionsDialog(processes[0]);

    expect(deploymentProcessesService.listRevisions).toHaveBeenCalledWith('process-1');
    expect((component as any).selectedProcess()).toEqual(processes[0]);
    expect((component as any).revisions()).toEqual(revisions);
    expect(overlay.showModal).toHaveBeenCalled();
  });

  it('renders complete immutable revision step detail fields', () => {
    const {component} = createComponent();
    (component as any).selectedRevision.set(revisions[0]);

    const detailTemplate = (component as any).revisionDetailDialog();
    const view = detailTemplate.createEmbeddedView({});
    view.detectChanges();
    const text = view.rootNodes.map((node: HTMLElement) => node.textContent ?? '').join(' ');
    view.destroy();

    expect(text).toContain('Condition: always');
    expect(text).toContain('Timeout: 300');
    expect(text).toContain('Retry: 2 attempts / 30 seconds');
    expect(text).toContain('Permissions: deploy.write');
    expect(text).toContain('Step Template: template-version-1');
  });

  it('creates revisions with structured steps and scoped selectors', async () => {
    const {component} = createComponent();

    (component as any).showCreateRevisionDialog(processes[0]);
    (component as any).revisionForm.patchValue({description: 'Initial revision'});
    (component as any).stepsArray.at(0).patchValue({
      key: 'deploy',
      name: 'Deploy',
      actionType: 'distr.http.check',
      executionLocation: 'target',
      inputBindingsText: '{\"url\":\"https://example.com/health\"}',
      condition: 'always',
      channelIds: ['channel-1'],
      environmentIds: ['environment-1'],
      targetTagsText: 'linux',
      failureMode: 'fail',
      timeoutSeconds: 300,
      retryMaxAttempts: 2,
      retryIntervalSeconds: 30,
      requiredPermissionsText: 'deploy.write',
      sortOrder: 10,
      dependenciesText: '',
    });
    await (component as any).submitRevisionForm();

    expect(deploymentProcessesService.createRevision).toHaveBeenCalledWith('process-1', {
      description: 'Initial revision',
      steps: [
        {
          key: 'deploy',
          name: 'Deploy',
          actionType: 'distr.http.check',
          executionLocation: 'target',
          inputBindings: {url: 'https://example.com/health'},
          condition: 'always',
          channelIds: ['channel-1'],
          environmentIds: ['environment-1'],
          targetTags: ['linux'],
          failureMode: 'fail',
          timeoutSeconds: 300,
          retryPolicy: {maxAttempts: 2, intervalSeconds: 30},
          requiredPermissions: ['deploy.write'],
          sortOrder: 10,
          dependencies: [],
        } satisfies DeploymentProcessStepRequest,
      ],
    });
  });

  it('does not create revisions with invalid input binding JSON', async () => {
    const {component} = createComponent();

    (component as any).showCreateRevisionDialog(processes[0]);
    (component as any).stepsArray.at(0).patchValue({
      key: 'deploy',
      name: 'Deploy',
      actionType: 'distr.http.check',
      executionLocation: 'target',
      inputBindingsText: '{not json',
    });
    await (component as any).submitRevisionForm();

    expect(deploymentProcessesService.createRevision).not.toHaveBeenCalled();
    expect(toast.error).toHaveBeenCalledWith('Step input bindings must be valid JSON.');
  });

  it('confirms before deleting deployment processes', async () => {
    const {component} = createComponent();

    (component as any).delete(processes[0]);
    await Promise.resolve();

    expect(overlay.confirm).toHaveBeenCalled();
    expect(deploymentProcessesService.delete).toHaveBeenCalledWith('process-1');
  });

  function createComponent(): {
    fixture: ComponentFixture<DeploymentProcessesComponent>;
    component: DeploymentProcessesComponent;
  } {
    const fixture = TestBed.createComponent(DeploymentProcessesComponent);
    fixture.detectChanges();
    return {fixture, component: fixture.componentInstance};
  }
});
