import {HttpErrorResponse} from '@angular/common/http';
import {ComponentFixture, TestBed} from '@angular/core/testing';
import {Router} from '@angular/router';
import {Application, DeploymentTarget} from '@distr-sh/distr-sdk';
import {of, throwError} from 'rxjs';
import {vi} from 'vitest';
import {ApplicationsService} from '../services/applications.service';
import {ChannelsService} from '../services/channels.service';
import {DeploymentPlansService} from '../services/deployment-plans.service';
import {DeploymentTargetsService} from '../services/deployment-targets.service';
import {EnvironmentsService} from '../services/environments.service';
import {OverlayService} from '../services/overlay.service';
import {ReleaseBundlesService} from '../services/release-bundles.service';
import {ToastService} from '../services/toast.service';
import {Channel} from '../types/channel';
import {DeploymentPlan} from '../types/deployment-plan';
import {Environment} from '../types/environment';
import {ReleaseBundle} from '../types/release-bundle';
import {DeploymentPlansComponent} from './deployment-plans.component';

describe('DeploymentPlansComponent', () => {
  let deploymentPlansService: any;
  let releaseBundlesService: any;
  let applicationsService: any;
  let channelsService: any;
  let environmentsService: any;
  let deploymentTargetsService: any;
  let overlay: any;
  let toast: any;
  let router: any;

  const applications = [{id: 'application-1', name: 'Payments', type: 'docker', versions: []}] as Application[];
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
  const environments: Environment[] = [
    {
      id: 'environment-1',
      createdAt: '2026-06-20T08:00:00Z',
      updatedAt: '2026-06-20T08:00:00Z',
      name: 'Production',
      description: '',
      sortOrder: 20,
      isProduction: true,
      allowDynamicTargets: false,
    },
  ];
  const targets = [
    {
      id: 'target-1',
      name: 'Cluster A',
      type: 'docker',
      deployments: [],
      metricsEnabled: false,
      imageCleanupEnabled: false,
      deploymentLogsEnabled: false,
    },
  ] as DeploymentTarget[];
  const releaseBundles: ReleaseBundle[] = [
    {
      id: 'bundle-1',
      createdAt: '2026-06-21T08:00:00Z',
      updatedAt: '2026-06-21T08:00:00Z',
      applicationId: 'application-1',
      channelId: 'channel-1',
      releaseNumber: '2026.06.21',
      releaseNotes: 'Initial release',
      sourceRevision: 'abc123',
      status: 'PUBLISHED',
      canonicalChecksum: 'sha256:release',
      components: [],
    },
  ];
  const plans: DeploymentPlan[] = [
    {
      id: 'plan-1',
      createdAt: '2026-06-21T09:00:00Z',
      applicationId: 'application-1',
      releaseBundleId: 'bundle-1',
      channelId: 'channel-1',
      environmentId: 'environment-1',
      status: 'BLOCKED',
      canonicalChecksum: 'sha256:plan',
      targets: [
        {
          id: 'plan-target-1',
          deploymentTargetId: 'target-1',
          name: 'Cluster A',
          type: 'docker',
          sortOrder: 0,
        },
      ],
      steps: [
        {
          id: 'step-1',
          stepKey: 'deploy',
          name: 'Deploy',
          actionType: 'distr.http.check',
          actionName: 'HTTP check',
          executionLocation: 'hub',
          inputBindings: {url: 'https://example.test'},
          condition: '',
          targetTags: [],
          failureMode: 'fail',
          timeoutSeconds: 30,
          retryMaxAttempts: 1,
          retryIntervalSeconds: 5,
          requiredPermissions: [],
          sortOrder: 10,
          dependencies: [],
          included: true,
        },
      ],
      variables: [
        {
          id: 'variable-1',
          variableSetId: 'variable-set-1',
          variableId: 'live-variable-1',
          key: 'API_TOKEN',
          type: 'secret_reference',
          isRequired: true,
          status: 'resolved',
          source: 'default',
          referenceName: 'api-token',
          redacted: true,
          trace: [],
        },
      ],
      issues: [
        {
          id: 'issue-1',
          severity: 'blocker',
          code: 'required_variable_unresolved',
          field: 'variables.API_URL',
          message: 'Required variable is unresolved.',
          sortOrder: 10,
        },
        {
          id: 'issue-2',
          severity: 'warning',
          code: 'dry_run_not_performed',
          field: '',
          message: 'Remote dry run was not performed.',
          sortOrder: 20,
        },
      ],
    },
  ];

  beforeEach(() => {
    deploymentPlansService = {
      list: vi.fn(),
      create: vi.fn(),
      execute: vi.fn(),
    };
    releaseBundlesService = {
      list: vi.fn(),
    };
    applicationsService = {
      list: vi.fn(),
    };
    channelsService = {
      list: vi.fn(),
    };
    environmentsService = {
      list: vi.fn(),
    };
    deploymentTargetsService = {
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
    router = {
      navigate: vi.fn(),
    };

    deploymentPlansService.list.mockReturnValue(of(plans));
    deploymentPlansService.create.mockReturnValue(of(plans[0]));
    releaseBundlesService.list.mockReturnValue(of(releaseBundles));
    applicationsService.list.mockReturnValue(of(applications));
    channelsService.list.mockReturnValue(of(channels));
    environmentsService.list.mockReturnValue(of(environments));
    deploymentTargetsService.list.mockReturnValue(of(targets));
    overlay.showModal.mockReturnValue({close: vi.fn()} as any);

    TestBed.configureTestingModule({
      imports: [DeploymentPlansComponent],
      providers: [
        {provide: DeploymentPlansService, useValue: deploymentPlansService},
        {provide: ReleaseBundlesService, useValue: releaseBundlesService},
        {provide: ApplicationsService, useValue: applicationsService},
        {provide: ChannelsService, useValue: channelsService},
        {provide: EnvironmentsService, useValue: environmentsService},
        {provide: DeploymentTargetsService, useValue: deploymentTargetsService},
        {provide: OverlayService, useValue: overlay},
        {provide: ToastService, useValue: toast},
        {provide: Router, useValue: router},
      ],
    });
  });

  it('loads deployment plans with lookup data and issue summaries', () => {
    const {component} = createComponent();

    expect((component as any).deploymentPlans()).toEqual(plans);
    expect((component as any).applicationName('application-1')).toBe('Payments');
    expect((component as any).releaseLabel('bundle-1')).toBe('2026.06.21');
    expect((component as any).environmentName('environment-1')).toBe('Production');
    expect((component as any).issueCount(plans[0], 'blocker')).toBe(1);
    expect((component as any).issueCount(plans[0], 'warning')).toBe(1);
  });

  it('shows load errors', () => {
    deploymentPlansService.list.mockReturnValue(
      throwError(() => new HttpErrorResponse({status: 400, error: 'Could not load deployment plans'}))
    );

    const {component} = createComponent();

    expect((component as any).loadError()).toBe('Could not load deployment plans');
  });

  it('creates deployment plans for the selected release, environment, and targets', async () => {
    const {component} = createComponent();

    (component as any).showCreateDialog();
    (component as any).planForm.patchValue({
      releaseBundleId: 'bundle-1',
      environmentId: 'environment-1',
      targetIds: ['target-1'],
    });
    await (component as any).submitForm();

    expect(deploymentPlansService.create).toHaveBeenCalledWith({
      releaseBundleId: 'bundle-1',
      environmentId: 'environment-1',
      targetIds: ['target-1'],
    });
    expect(toast.success).toHaveBeenCalledWith('Deployment plan created');
  });

  it('confirms and executes a ready deployment plan, then opens the deployment timeline', async () => {
    const readyPlan: DeploymentPlan = {...plans[0], status: 'READY', issues: []};
    deploymentPlansService.list.mockReturnValue(of([readyPlan]));
    deploymentPlansService.execute.mockReturnValue(
      of([
        {
          id: 'task-1',
          deploymentPlanId: readyPlan.id,
          deploymentTargetId: 'target-1',
          status: 'QUEUED',
        },
      ])
    );
    overlay.confirm.mockReturnValue(of(true));
    const {component} = createComponent();

    await (component as any).executePlan(readyPlan);

    expect(overlay.confirm).toHaveBeenCalled();
    expect(deploymentPlansService.execute).toHaveBeenCalledWith('plan-1');
    expect(toast.success).toHaveBeenCalledWith('Deployment started for 1 target');
    expect(router.navigate).toHaveBeenCalledWith(['/deployment-timeline']);
  });

  it('does not execute blocked deployment plans', async () => {
    const {component} = createComponent();

    await (component as any).executePlan(plans[0]);

    expect(overlay.confirm).not.toHaveBeenCalled();
    expect(deploymentPlansService.execute).not.toHaveBeenCalled();
    expect(router.navigate).not.toHaveBeenCalled();
  });

  it('renders JSON and Markdown exports without exposing redacted values', () => {
    const {component} = createComponent();

    const json = (component as any).deploymentPlanJson(plans[0]);
    const markdown = (component as any).deploymentPlanMarkdown(plans[0]);

    expect(json).toContain('"canonicalChecksum": "sha256:plan"');
    expect(markdown).toContain('# Deployment Plan plan-1');
    expect(markdown).toContain('Checksum: `sha256:plan`');
    expect(markdown).toContain('## Blockers');
    expect(markdown).toContain('Required variable is unresolved.');
    expect(markdown).toContain('API_TOKEN');
    expect(markdown).toContain('redacted');
    expect(markdown).not.toContain('secret-value');
  });

  function createComponent(): {
    fixture: ComponentFixture<DeploymentPlansComponent>;
    component: DeploymentPlansComponent;
  } {
    const fixture = TestBed.createComponent(DeploymentPlansComponent);
    fixture.detectChanges();
    return {fixture, component: fixture.componentInstance};
  }
});
