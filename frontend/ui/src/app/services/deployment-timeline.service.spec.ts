import {provideHttpClient} from '@angular/common/http';
import {HttpTestingController, provideHttpClientTesting} from '@angular/common/http/testing';
import {TestBed} from '@angular/core/testing';
import {DeploymentTimelineService} from './deployment-timeline.service';

describe('DeploymentTimelineService', () => {
  let http: HttpTestingController;
  let service: DeploymentTimelineService;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting()],
    });
    http = TestBed.inject(HttpTestingController);
    service = TestBed.inject(DeploymentTimelineService);
  });

  afterEach(() => {
    http.verify();
  });

  it('performs timeline, task detail, compare, and redeploy requests', () => {
    const timeline = {items: []};
    const comparison = {
      base: {} as any,
      compare: {} as any,
      process: {changed: false},
      availability: {process: true, steps: true, variables: true},
      components: [],
      steps: [],
      variables: [],
    };
    const redeploy = {
      plan: {id: 'plan-2'} as any,
      warning: 'Deploy previous release creates a new deployment plan.',
    };

    service
      .list({
        applicationId: 'application-1',
        environmentId: 'environment-1',
        includeNonTerminal: false,
        limit: 25,
      })
      .subscribe((result) => expect(result.items).toEqual([]));
    const listReq = http.expectOne((req) => req.url === '/api/v1/deployment-timeline');
    expect(listReq.request.method).toBe('GET');
    expect(listReq.request.params.get('applicationId')).toBe('application-1');
    expect(listReq.request.params.get('environmentId')).toBe('environment-1');
    expect(listReq.request.params.get('includeNonTerminal')).toBe('false');
    expect(listReq.request.params.get('limit')).toBe('25');
    listReq.flush(timeline);

    service.getTask('task-1').subscribe((result) => expect(result.id).toBe('task-1'));
    const taskReq = http.expectOne('/api/v1/tasks/task-1');
    expect(taskReq.request.method).toBe('GET');
    taskReq.flush({id: 'task-1', status: 'RUNNING', stepRuns: []});

    service.getTaskTimeline('task-1').subscribe((result) => expect(result.taskId).toBe('task-1'));
    const taskTimelineReq = http.expectOne('/api/v1/tasks/task-1/timeline');
    expect(taskTimelineReq.request.method).toBe('GET');
    taskTimelineReq.flush({organizationId: 'org-1', taskId: 'task-1', events: []});

    service.compare('task-1', 'task-2').subscribe((result) => expect(result.process.changed).toBe(false));
    const compareReq = http.expectOne('/api/v1/deployment-timeline/compare?baseTaskId=task-1&compareTaskId=task-2');
    expect(compareReq.request.method).toBe('GET');
    compareReq.flush(comparison);

    service.redeploy('task-1').subscribe((result) => expect(result.plan.id).toBe('plan-2'));
    const redeployReq = http.expectOne('/api/v1/deployment-timeline/task-1/redeploy');
    expect(redeployReq.request.method).toBe('POST');
    expect(redeployReq.request.body).toBeNull();
    redeployReq.flush(redeploy);
  });

  it('uses source-specific compare parameters for legacy entries', () => {
    const comparison = {
      base: {} as any,
      compare: {} as any,
      process: {changed: false},
      availability: {process: false, steps: false, variables: false},
      components: [],
      steps: [],
      variables: [],
    };

    (service as any)
      .compare({legacyDeploymentRevisionId: 'legacy-revision-1'}, {taskId: 'task-2'})
      .subscribe((result: any) => expect(result.availability.process).toBe(false));
    const compareReq = http.expectOne(
      '/api/v1/deployment-timeline/compare?baseLegacyDeploymentRevisionId=legacy-revision-1&compareTaskId=task-2'
    );
    expect(compareReq.request.method).toBe('GET');
    compareReq.flush(comparison);
  });
});
