import {provideHttpClient} from '@angular/common/http';
import {HttpTestingController, provideHttpClientTesting} from '@angular/common/http/testing';
import {TestBed} from '@angular/core/testing';
import {DeploymentPlansService} from './deployment-plans.service';

describe('DeploymentPlansService', () => {
  let http: HttpTestingController;
  let service: DeploymentPlansService;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting()],
    });
    http = TestBed.inject(HttpTestingController);
    service = TestBed.inject(DeploymentPlansService);
  });

  afterEach(() => {
    http.verify();
  });

  it('performs deployment plan list, get, create, and execute requests', () => {
    const plan = {
      id: 'plan-1',
      createdAt: '2026-06-21T07:00:00Z',
      applicationId: 'application-1',
      releaseBundleId: 'bundle-1',
      channelId: 'channel-1',
      environmentId: 'environment-1',
      status: 'READY',
      canonicalChecksum: 'sha256:abc',
      targets: [],
      steps: [],
      variables: [],
      issues: [],
    };
    const request = {
      releaseBundleId: 'bundle-1',
      environmentId: 'environment-1',
      targetIds: ['target-1', 'target-2'],
    };

    service.list().subscribe((plans) => expect(plans[0].id).toBe('plan-1'));
    const listReq = http.expectOne('/api/v1/deployment-plans');
    expect(listReq.request.method).toBe('GET');
    listReq.flush([plan]);

    service.get('plan-1').subscribe((result) => expect(result.canonicalChecksum).toBe('sha256:abc'));
    const getReq = http.expectOne('/api/v1/deployment-plans/plan-1');
    expect(getReq.request.method).toBe('GET');
    getReq.flush(plan);

    service.create(request).subscribe((result) => expect(result.id).toBe('plan-1'));
    const createReq = http.expectOne('/api/v1/deployment-plans');
    expect(createReq.request.method).toBe('POST');
    expect(createReq.request.body).toEqual(request);
    createReq.flush(plan);

    service.execute('plan-1').subscribe((tasks) => expect(tasks[0].id).toBe('task-1'));
    const executeReq = http.expectOne('/api/v1/deployment-plans/plan-1/tasks');
    expect(executeReq.request.method).toBe('POST');
    expect(executeReq.request.body).toEqual({});
    executeReq.flush([
      {
        id: 'task-1',
        deploymentPlanId: 'plan-1',
        deploymentTargetId: 'target-1',
        status: 'QUEUED',
      },
    ]);
  });
});
