import {ComponentFixture, TestBed} from '@angular/core/testing';
import {ActivatedRoute, Router} from '@angular/router';
import {of} from 'rxjs';
import {vi} from 'vitest';
import {DeploymentTimelineService} from '../services/deployment-timeline.service';
import {OverlayService} from '../services/overlay.service';
import {ToastService} from '../services/toast.service';
import {
  DeploymentTimelineComparison,
  DeploymentTimelineItem,
  DeploymentTimelineRedeploy,
} from '../types/deployment-timeline';
import {DeploymentTimelineComponent} from './deployment-timeline.component';

describe('DeploymentTimelineComponent', () => {
  let deploymentTimelineService: any;
  let overlay: any;
  let toast: any;
  let router: any;
  let route: any;

  const items: DeploymentTimelineItem[] = [
    {
      taskId: 'task-1',
      deploymentPlanId: 'plan-1',
      deploymentPlanTargetId: 'plan-target-1',
      deploymentTargetId: 'target-1',
      applicationId: 'application-1',
      applicationName: 'Payments',
      releaseBundleId: 'bundle-1',
      releaseNumber: '2026.06.21',
      channelId: 'channel-1',
      channelName: 'Stable',
      environmentId: 'environment-1',
      environmentName: 'Production',
      deploymentTargetName: 'cluster-a',
      actorUserAccountId: 'actor-1',
      status: 'SUCCEEDED',
      queuedAt: '2026-06-22T09:00:00Z',
      startedAt: '2026-06-22T09:01:00Z',
      completedAt: '2026-06-22T09:05:00Z',
      components: [{key: 'api', name: 'API', type: 'application_version', version: '1.2.3'}],
      lastSuccessful: true,
      redeployAvailable: true,
    },
    {
      taskId: 'task-2',
      deploymentPlanId: 'plan-2',
      deploymentPlanTargetId: 'plan-target-2',
      deploymentTargetId: 'target-1',
      applicationId: 'application-1',
      applicationName: 'Payments',
      releaseBundleId: 'bundle-2',
      releaseNumber: '2026.06.22',
      channelId: 'channel-1',
      channelName: 'Stable',
      environmentId: 'environment-1',
      environmentName: 'Production',
      deploymentTargetName: 'cluster-a',
      status: 'FAILED',
      queuedAt: '2026-06-22T10:00:00Z',
      startedAt: '2026-06-22T10:01:00Z',
      completedAt: '2026-06-22T10:02:00Z',
      components: [{key: 'api', name: 'API', type: 'application_version', version: '1.2.4'}],
      lastSuccessful: false,
      redeployAvailable: true,
    },
  ];

  const legacyItems: DeploymentTimelineItem[] = [
    {
      source: 'legacy_deployment',
      taskId: '',
      legacyDeploymentId: 'legacy-deployment-1',
      legacyDeploymentRevisionId: 'legacy-revision-1',
      syntheticReleaseId: 'synthetic-release-1',
      deploymentPlanId: '',
      deploymentPlanTargetId: '',
      deploymentTargetId: 'target-1',
      applicationId: 'application-1',
      applicationName: 'Payments',
      releaseBundleId: '',
      releaseNumber: 'legacy 1.2.2',
      channelId: '',
      channelName: '',
      environmentId: '',
      environmentName: '',
      deploymentTargetName: 'cluster-a',
      queuedAt: '2026-06-22T08:00:00Z',
      completedAt: '2026-06-22T08:00:00Z',
      availability: {
        processSnapshot: false,
        variableSnapshot: false,
        channel: false,
        environment: false,
        taskLogs: false,
        redeployPlan: false,
      },
      components: [{key: 'application', name: 'Payments', type: 'application_version', version: '1.2.2'}],
      lastSuccessful: false,
      redeployAvailable: false,
    } as any,
    {
      source: 'legacy_deployment',
      taskId: '',
      legacyDeploymentId: 'legacy-deployment-2',
      legacyDeploymentRevisionId: 'legacy-revision-2',
      syntheticReleaseId: 'synthetic-release-2',
      deploymentPlanId: '',
      deploymentPlanTargetId: '',
      deploymentTargetId: 'target-1',
      applicationId: 'application-1',
      applicationName: 'Payments',
      releaseBundleId: '',
      releaseNumber: 'legacy 1.2.1',
      channelId: '',
      channelName: '',
      environmentId: '',
      environmentName: '',
      deploymentTargetName: 'cluster-a',
      queuedAt: '2026-06-22T07:00:00Z',
      completedAt: '2026-06-22T07:00:00Z',
      availability: {
        processSnapshot: false,
        variableSnapshot: false,
        channel: false,
        environment: false,
        taskLogs: false,
        redeployPlan: false,
      },
      components: [{key: 'application', name: 'Payments', type: 'application_version', version: '1.2.1'}],
      lastSuccessful: false,
      redeployAvailable: false,
    } as any,
  ];

  const comparison: DeploymentTimelineComparison = {
    base: items[0],
    compare: items[1],
    process: {baseRevisionNumber: 1, compareRevisionNumber: 2, changed: true},
    availability: {process: true, steps: true, variables: true},
    components: [
      {
        key: 'api',
        name: 'API',
        kind: 'changed',
        baseVersion: '1.2.3',
        compareVersion: '1.2.4',
      },
    ],
    steps: [],
    variables: [
      {
        key: 'API_TOKEN',
        kind: 'changed',
        baseRedacted: true,
        compareRedacted: true,
      },
    ],
  };

  const redeploy: DeploymentTimelineRedeploy = {
    plan: {
      id: 'plan-3',
      createdAt: '2026-06-22T10:30:00Z',
      applicationId: 'application-1',
      releaseBundleId: 'bundle-1',
      channelId: 'channel-1',
      environmentId: 'environment-1',
      status: 'READY',
      canonicalChecksum: 'sha256:plan',
      targets: [],
      targetComponents: [],
      preflightRuns: [],
      steps: [],
      variables: [],
      issues: [],
    },
    warning: 'Deploy previous release creates a new deployment plan.',
  };

  beforeEach(() => {
    deploymentTimelineService = {
      list: vi.fn(),
      compare: vi.fn(),
      redeploy: vi.fn(),
      getTask: vi.fn(),
      getTaskTimeline: vi.fn(),
    };
    overlay = {
      confirm: vi.fn(),
    };
    toast = {
      error: vi.fn(),
      success: vi.fn(),
    };
    router = {navigate: vi.fn()};
    route = {snapshot: {queryParamMap: {get: vi.fn().mockReturnValue(null)}}};

    deploymentTimelineService.list.mockReturnValue(of({items}));
    deploymentTimelineService.compare.mockReturnValue(of(comparison));
    deploymentTimelineService.redeploy.mockReturnValue(of(redeploy));
    deploymentTimelineService.getTask.mockReturnValue(
      of({
        id: 'task-2',
        status: 'RUNNING',
        queuedAt: '2026-06-22T10:00:00Z',
        stepRuns: [
          {
            id: 'step-run-1',
            taskId: 'task-2',
            stepKey: 'deploy',
            name: 'Deploy loyalty-api',
            actionType: 'distr.webhook',
            status: 'RUNNING',
            sortOrder: 10,
          },
        ],
      })
    );
    deploymentTimelineService.getTaskTimeline.mockReturnValue(
      of({
        organizationId: 'org-1',
        taskId: 'task-2',
        events: [
          {
            id: 'event-1',
            taskId: 'task-2',
            stepRunId: 'step-run-1',
            sequence: 2,
            type: 'PROGRESS',
            occurredAt: '2026-06-22T10:01:30Z',
            createdAt: '2026-06-22T10:01:30Z',
            message: 'deploying loyalty-api',
            progressPercent: 60,
            redacted: false,
            logs: [],
            outputs: [],
          },
        ],
      })
    );
    overlay.confirm.mockReturnValue(of(true));

    TestBed.configureTestingModule({
      imports: [DeploymentTimelineComponent],
      providers: [
        {provide: DeploymentTimelineService, useValue: deploymentTimelineService},
        {provide: OverlayService, useValue: overlay},
        {provide: ToastService, useValue: toast},
        {provide: Router, useValue: router},
        {provide: ActivatedRoute, useValue: route},
      ],
    });
  });

  it('loads and filters timeline entries', () => {
    const {component} = createComponent();

    expect((component as any).timelineItems()).toEqual(items);
    expect((component as any).filteredTimelineItems().length).toBe(2);

    (component as any).filterForm.controls.search.setValue('1.2.4');

    expect((component as any).filteredTimelineItems()).toEqual([items[1]]);
  });

  it('compares selected timeline entries', async () => {
    const {component} = createComponent();

    (component as any).selectBase('task-1');
    (component as any).selectCompare('task-2');
    await (component as any).compare();

    expect(deploymentTimelineService.compare).toHaveBeenCalledWith({taskId: 'task-1'}, {taskId: 'task-2'});
    expect((component as any).comparison()).toEqual(comparison);
    expect((component as any).changedCount(comparison.components)).toBe(1);
  });

  it('uses source-specific keys and compare refs for legacy entries', async () => {
    deploymentTimelineService.list.mockReturnValue(of({items: [legacyItems[0], legacyItems[1], items[0]]}));
    const {component} = createComponent();

    expect((component as any).timelineItemKey(legacyItems[0])).toBe('legacy:legacy-revision-1');
    expect((component as any).timelineItemKey(legacyItems[1])).toBe('legacy:legacy-revision-2');
    (component as any).selectBase(legacyItems[0]);
    (component as any).selectCompare(items[0]);
    await (component as any).compare();

    expect(deploymentTimelineService.compare).toHaveBeenCalledWith(
      {legacyDeploymentRevisionId: 'legacy-revision-1'},
      {taskId: 'task-1'}
    );
  });

  it('hides task-only actions for legacy entries', () => {
    deploymentTimelineService.list.mockReturnValue(of({items: [legacyItems[0], items[0]]}));
    const {fixture, component} = createComponent();

    expect((component as any).canRedeploy(legacyItems[0])).toBe(false);
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelectorAll('button[title="Execution details"]').length).toBe(1);
    expect(fixture.nativeElement.querySelectorAll('button[title="Deploy previous release"]').length).toBe(0);
  });

  it('renders unavailable comparison dimensions for legacy entries', async () => {
    const unavailableComparison = {
      ...comparison,
      base: legacyItems[0],
      compare: items[0],
      availability: {process: false, steps: false, variables: false},
      process: {changed: false},
      steps: [],
      variables: [],
    } as any;
    deploymentTimelineService.list.mockReturnValue(of({items: [legacyItems[0], items[0]]}));
    deploymentTimelineService.compare.mockReturnValue(of(unavailableComparison));
    const {fixture, component} = createComponent();
    (component as any).selectBase(legacyItems[0]);
    (component as any).selectCompare(items[0]);
    await (component as any).compare();
    fixture.detectChanges();
    const text = fixture.nativeElement.textContent;
    expect(text).toContain('Process unavailable');
    expect(text).toContain('Variables unavailable');
    expect(text).toContain('Steps unavailable');
    expect(text).not.toContain('unchanged');
  });
  it('creates a deploy previous release plan after confirmation', async () => {
    const {component} = createComponent();

    await (component as any).deployPreviousRelease(items[0]);

    expect(overlay.confirm).toHaveBeenCalled();
    expect(overlay.confirm.mock.calls[0][0].confirmLabel).toBe('Deploy previous release');
    expect(deploymentTimelineService.compare).toHaveBeenCalledWith({taskId: 'task-2'}, {taskId: 'task-1'});
    expect(deploymentTimelineService.redeploy).toHaveBeenCalledWith('task-1');
    expect(toast.success).toHaveBeenCalledWith('Deployment plan plan-3 created');
    expect(router.navigate).toHaveBeenCalledWith(['/deployment-plans'], {queryParams: {planId: 'plan-3'}});
  });

  it('loads and renders structured execution progress in the existing side panel', async () => {
    const {fixture, component} = createComponent();

    await (component as any).viewTask(items[1]);
    fixture.detectChanges();

    expect(deploymentTimelineService.getTask).toHaveBeenCalledWith('task-2');
    expect(deploymentTimelineService.getTaskTimeline).toHaveBeenCalledWith('task-2');
    expect(fixture.nativeElement.textContent).toContain('Deploy loyalty-api');
    expect(fixture.nativeElement.textContent).toContain('deploying loyalty-api');
    expect(fixture.nativeElement.textContent).toContain('60%');
  });

  it('preserves API event order across task lease attempts', async () => {
    deploymentTimelineService.getTaskTimeline.mockReturnValue(
      of({
        organizationId: 'org-1',
        taskId: 'task-2',
        events: [
          {
            id: 'attempt-1-started',
            taskId: 'task-2',
            stepRunId: 'step-run-1',
            sequence: 1,
            type: 'STARTED',
            occurredAt: '2026-06-22T10:01:00Z',
            createdAt: '2026-06-22T10:01:00Z',
            message: 'attempt 1 started',
            redacted: false,
            logs: [],
            outputs: [],
          },
          {
            id: 'attempt-1-progress',
            taskId: 'task-2',
            stepRunId: 'step-run-1',
            sequence: 2,
            type: 'PROGRESS',
            occurredAt: '2026-06-22T10:01:30Z',
            createdAt: '2026-06-22T10:01:30Z',
            message: 'attempt 1 progressed',
            redacted: false,
            logs: [],
            outputs: [],
          },
          {
            id: 'attempt-2-started',
            taskId: 'task-2',
            stepRunId: 'step-run-1',
            sequence: 1,
            type: 'STARTED',
            occurredAt: '2026-06-22T10:02:00Z',
            createdAt: '2026-06-22T10:02:00Z',
            message: 'attempt 2 started',
            redacted: false,
            logs: [],
            outputs: [],
          },
        ],
      })
    );
    const {component} = createComponent();

    await (component as any).viewTask(items[1]);

    expect((component as any).eventsForStep('step-run-1').map((event: any) => event.message)).toEqual([
      'attempt 1 started',
      'attempt 1 progressed',
      'attempt 2 started',
    ]);
  });

  it('stops polling after the selected task becomes terminal', async () => {
    vi.useFakeTimers();
    try {
      deploymentTimelineService.getTask.mockReset();
      deploymentTimelineService.getTask
        .mockReturnValueOnce(
          of({
            id: 'task-2',
            status: 'RUNNING',
            queuedAt: '2026-06-22T10:00:00Z',
            stepRuns: [],
          })
        )
        .mockReturnValueOnce(
          of({
            id: 'task-2',
            status: 'SUCCEEDED',
            queuedAt: '2026-06-22T10:00:00Z',
            completedAt: '2026-06-22T10:03:00Z',
            stepRuns: [],
          })
        );
      const {fixture, component} = createComponent();

      await (component as any).viewTask(items[1]);
      expect(deploymentTimelineService.getTask).toHaveBeenCalledTimes(1);

      await vi.advanceTimersByTimeAsync(3000);
      expect(deploymentTimelineService.getTask).toHaveBeenCalledTimes(2);
      expect((component as any).selectedTask().status).toBe('SUCCEEDED');

      await vi.advanceTimersByTimeAsync(3000);
      expect(deploymentTimelineService.getTask).toHaveBeenCalledTimes(2);
      fixture.destroy();
    } finally {
      vi.useRealTimers();
    }
  });

  it('stops polling when the component is destroyed', async () => {
    vi.useFakeTimers();
    try {
      const {fixture, component} = createComponent();
      await (component as any).viewTask(items[1]);
      expect(deploymentTimelineService.getTask).toHaveBeenCalledTimes(1);

      fixture.destroy();
      await vi.advanceTimersByTimeAsync(3000);

      expect(deploymentTimelineService.getTask).toHaveBeenCalledTimes(1);
    } finally {
      vi.useRealTimers();
    }
  });

  it('fails closed when displaying sensitive or redacted outputs', () => {
    const {component} = createComponent();

    expect(
      (component as any).outputValue({name: 'token', value: 'must-not-render', sensitive: true, redacted: false})
    ).toBe('redacted');
    expect(
      (component as any).outputValue({name: 'token', value: 'must-not-render', sensitive: false, redacted: true})
    ).toBe('redacted');
  });

  it('opens the requested task from the execution deep link', async () => {
    route.snapshot.queryParamMap.get.mockImplementation((name: string) => (name === 'taskId' ? 'task-2' : null));

    const {fixture} = createComponent();
    await vi.waitFor(() => expect(deploymentTimelineService.getTask).toHaveBeenCalledWith('task-2'));
    fixture.detectChanges();

    expect(fixture.nativeElement.textContent).toContain('Deploy loyalty-api');
  });

  function createComponent(): {
    fixture: ComponentFixture<DeploymentTimelineComponent>;
    component: DeploymentTimelineComponent;
  } {
    const fixture = TestBed.createComponent(DeploymentTimelineComponent);
    fixture.detectChanges();
    return {fixture, component: fixture.componentInstance};
  }
});
