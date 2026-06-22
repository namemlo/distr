import {provideHttpClient} from '@angular/common/http';
import {HttpTestingController, provideHttpClientTesting} from '@angular/common/http/testing';
import {TestBed} from '@angular/core/testing';
import {RunbooksService} from './runbooks.service';

describe('RunbooksService', () => {
  let http: HttpTestingController;
  let service: RunbooksService;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting()],
    });
    http = TestBed.inject(HttpTestingController);
    service = TestBed.inject(RunbooksService);
  });

  afterEach(() => {
    http.verify();
  });

  it('performs runbook CRUD requests', () => {
    const request = {
      applicationId: 'application-1',
      name: 'Rotate keys',
      description: 'Rotate service signing keys',
      sortOrder: 10,
    };

    service.list().subscribe((runbooks) => {
      expect(runbooks[0].name).toBe('Rotate keys');
    });
    const listReq = http.expectOne('/api/v1/runbooks');
    expect(listReq.request.method).toBe('GET');
    listReq.flush([
      {id: 'runbook-1', createdAt: '2026-06-22T00:00:00Z', updatedAt: '2026-06-22T00:00:00Z', ...request},
    ]);

    service.get('runbook-1').subscribe();
    const getReq = http.expectOne('/api/v1/runbooks/runbook-1');
    expect(getReq.request.method).toBe('GET');
    getReq.flush({id: 'runbook-1', createdAt: '2026-06-22T00:00:00Z', updatedAt: '2026-06-22T00:00:00Z', ...request});

    service.create(request).subscribe();
    const createReq = http.expectOne('/api/v1/runbooks');
    expect(createReq.request.method).toBe('POST');
    expect(createReq.request.body).toEqual(request);
    createReq.flush({
      id: 'runbook-1',
      createdAt: '2026-06-22T00:00:00Z',
      updatedAt: '2026-06-22T00:00:00Z',
      ...request,
    });

    service.update('runbook-1', {...request, description: 'Edited'}).subscribe();
    const updateReq = http.expectOne('/api/v1/runbooks/runbook-1');
    expect(updateReq.request.method).toBe('PUT');
    expect(updateReq.request.body.description).toBe('Edited');
    updateReq.flush({
      id: 'runbook-1',
      createdAt: '2026-06-22T00:00:00Z',
      updatedAt: '2026-06-22T00:00:00Z',
      ...request,
    });

    service.delete('runbook-1').subscribe();
    const deleteReq = http.expectOne('/api/v1/runbooks/runbook-1');
    expect(deleteReq.request.method).toBe('DELETE');
    deleteReq.flush(null);
  });

  it('calls runbook revision and publish endpoints', () => {
    const revisionRequest = {
      description: 'Initial revision',
      steps: [
        {
          key: 'verify',
          name: 'Verify',
          actionType: 'distr.preflight',
          executionLocation: 'hub',
          inputBindings: {},
          condition: 'always()',
          failureMode: 'fail',
          timeoutSeconds: 120,
          retryPolicy: {maxAttempts: 2, intervalSeconds: 30},
          requiredPermissions: ['runbook.execute'],
          sortOrder: 10,
          dependencies: [],
        },
      ],
    };

    service.listRevisions('runbook-1').subscribe((revisions) => {
      expect(revisions[0].revisionNumber).toBe(1);
    });
    const listReq = http.expectOne('/api/v1/runbooks/runbook-1/revisions');
    expect(listReq.request.method).toBe('GET');
    listReq.flush([{id: 'revision-1', runbookId: 'runbook-1', revisionNumber: 1, steps: []}]);

    service.getRevision('runbook-1', 'revision-1').subscribe();
    const getReq = http.expectOne('/api/v1/runbooks/runbook-1/revisions/revision-1');
    expect(getReq.request.method).toBe('GET');
    getReq.flush({id: 'revision-1', runbookId: 'runbook-1', revisionNumber: 1, steps: []});

    service.createRevision('runbook-1', revisionRequest).subscribe();
    const createReq = http.expectOne('/api/v1/runbooks/runbook-1/revisions');
    expect(createReq.request.method).toBe('POST');
    expect(createReq.request.body).toEqual(revisionRequest);
    createReq.flush({id: 'revision-1', runbookId: 'runbook-1', revisionNumber: 1, ...revisionRequest});

    service.publishRevision('runbook-1', 'revision-1').subscribe();
    const publishReq = http.expectOne('/api/v1/runbooks/runbook-1/revisions/revision-1/publish');
    expect(publishReq.request.method).toBe('POST');
    publishReq.flush({
      id: 'snapshot-1',
      runbookId: 'runbook-1',
      runbookRevisionId: 'revision-1',
      revisionNumber: 1,
      canonicalChecksum: 'sha256:abc',
      revision: {id: 'revision-1', runbookId: 'runbook-1', revisionNumber: 1, steps: []},
    });
  });
});
