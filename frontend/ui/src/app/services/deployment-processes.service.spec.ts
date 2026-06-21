import {provideHttpClient} from '@angular/common/http';
import {HttpTestingController, provideHttpClientTesting} from '@angular/common/http/testing';
import {TestBed} from '@angular/core/testing';
import {DeploymentProcessesService} from './deployment-processes.service';

describe('DeploymentProcessesService', () => {
  let http: HttpTestingController;
  let service: DeploymentProcessesService;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting()],
    });
    http = TestBed.inject(HttpTestingController);
    service = TestBed.inject(DeploymentProcessesService);
  });

  afterEach(() => {
    http.verify();
  });

  it('performs deployment process CRUD requests', () => {
    const request = {
      applicationId: 'application-1',
      name: 'Standard deploy',
      description: 'Deploys the standard release',
      sortOrder: 10,
    };

    service.list().subscribe((processes) => {
      expect(processes[0].name).toBe('Standard deploy');
    });
    const listReq = http.expectOne('/api/v1/deployment-processes');
    expect(listReq.request.method).toBe('GET');
    listReq.flush([
      {id: 'process-1', createdAt: '2026-06-21T00:00:00Z', updatedAt: '2026-06-21T00:00:00Z', ...request},
    ]);

    service.get('process-1').subscribe();
    const getReq = http.expectOne('/api/v1/deployment-processes/process-1');
    expect(getReq.request.method).toBe('GET');
    getReq.flush({id: 'process-1', createdAt: '2026-06-21T00:00:00Z', updatedAt: '2026-06-21T00:00:00Z', ...request});

    service.create(request).subscribe();
    const createReq = http.expectOne('/api/v1/deployment-processes');
    expect(createReq.request.method).toBe('POST');
    expect(createReq.request.body).toEqual(request);
    createReq.flush({
      id: 'process-1',
      createdAt: '2026-06-21T00:00:00Z',
      updatedAt: '2026-06-21T00:00:00Z',
      ...request,
    });

    service.update('process-1', {...request, description: 'Edited'}).subscribe();
    const updateReq = http.expectOne('/api/v1/deployment-processes/process-1');
    expect(updateReq.request.method).toBe('PUT');
    expect(updateReq.request.body.description).toBe('Edited');
    updateReq.flush({
      id: 'process-1',
      createdAt: '2026-06-21T00:00:00Z',
      updatedAt: '2026-06-21T00:00:00Z',
      ...request,
    });

    service.delete('process-1').subscribe();
    const deleteReq = http.expectOne('/api/v1/deployment-processes/process-1');
    expect(deleteReq.request.method).toBe('DELETE');
    deleteReq.flush(null);
  });

  it('calls deployment process revision endpoints', () => {
    const revisionRequest = {
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
        },
      ],
    };

    service.listRevisions('process-1').subscribe((revisions) => {
      expect(revisions[0].revisionNumber).toBe(1);
    });
    const listReq = http.expectOne('/api/v1/deployment-processes/process-1/revisions');
    expect(listReq.request.method).toBe('GET');
    listReq.flush([{id: 'revision-1', deploymentProcessId: 'process-1', revisionNumber: 1, steps: []}]);

    service.getRevision('process-1', 'revision-1').subscribe();
    const getReq = http.expectOne('/api/v1/deployment-processes/process-1/revisions/revision-1');
    expect(getReq.request.method).toBe('GET');
    getReq.flush({id: 'revision-1', deploymentProcessId: 'process-1', revisionNumber: 1, steps: []});

    service.createRevision('process-1', revisionRequest).subscribe();
    const createReq = http.expectOne('/api/v1/deployment-processes/process-1/revisions');
    expect(createReq.request.method).toBe('POST');
    expect(createReq.request.body).toEqual(revisionRequest);
    createReq.flush({id: 'revision-1', deploymentProcessId: 'process-1', revisionNumber: 1, ...revisionRequest});
  });

  it('lists built-in action definitions', () => {
    service.listActionDefinitions().subscribe((actions) => {
      expect(actions[0].type).toBe('distr.preflight');
      expect(actions[0].inputSchema['type']).toBe('object');
    });

    const req = http.expectOne('/api/v1/action-definitions');
    expect(req.request.method).toBe('GET');
    req.flush([
      {
        type: 'distr.preflight',
        name: 'Preflight checks',
        description: 'Runs built-in agent preflight checks.',
        inputSchema: {type: 'object'},
        outputSchema: {type: 'object'},
      },
    ]);
  });
});
