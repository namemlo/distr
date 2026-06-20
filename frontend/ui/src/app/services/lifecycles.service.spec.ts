import {provideHttpClient} from '@angular/common/http';
import {HttpTestingController, provideHttpClientTesting} from '@angular/common/http/testing';
import {TestBed} from '@angular/core/testing';
import {LifecyclesService} from './lifecycles.service';

describe('LifecyclesService', () => {
  let http: HttpTestingController;
  let service: LifecyclesService;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting()],
    });
    http = TestBed.inject(HttpTestingController);
    service = TestBed.inject(LifecyclesService);
  });

  afterEach(() => {
    http.verify();
  });

  it('lists lifecycles from the lifecycles endpoint', () => {
    service.list().subscribe((lifecycles) => {
      expect(lifecycles).toEqual([
        {
          id: 'lifecycle-1',
          createdAt: '2026-06-20T09:30:00Z',
          updatedAt: '2026-06-20T10:45:00Z',
          name: 'Standard',
          description: 'Development to production promotion',
          sortOrder: 20,
          phases: [
            {
              id: 'phase-1',
              name: 'Development',
              description: '',
              sortOrder: 10,
              environmentIds: ['env-1'],
              optional: false,
              automaticPromotion: true,
              minimumSuccessfulDeployments: 1,
            },
          ],
        },
      ]);
    });

    const req = http.expectOne('/api/v1/lifecycles');
    expect(req.request.method).toBe('GET');
    req.flush([
      {
        id: 'lifecycle-1',
        createdAt: '2026-06-20T09:30:00Z',
        updatedAt: '2026-06-20T10:45:00Z',
        name: 'Standard',
        description: 'Development to production promotion',
        sortOrder: 20,
        phases: [
          {
            id: 'phase-1',
            name: 'Development',
            description: '',
            sortOrder: 10,
            environmentIds: ['env-1'],
            optional: false,
            automaticPromotion: true,
            minimumSuccessfulDeployments: 1,
          },
        ],
      },
    ]);
  });

  it('creates, updates, replaces phases, and deletes lifecycles', () => {
    const request = {
      name: 'Standard',
      description: 'Development to production promotion',
      sortOrder: 20,
      phases: [
        {
          name: 'Development',
          description: '',
          sortOrder: 10,
          environmentIds: ['env-1'],
          optional: false,
          automaticPromotion: true,
          minimumSuccessfulDeployments: 1,
        },
      ],
    };

    service.create(request).subscribe();
    const createReq = http.expectOne('/api/v1/lifecycles');
    expect(createReq.request.method).toBe('POST');
    expect(createReq.request.body).toEqual(request);
    createReq.flush({id: 'lifecycle-1', ...request});

    service.update('lifecycle-1', {...request, name: 'Hotfix'}).subscribe();
    const updateReq = http.expectOne('/api/v1/lifecycles/lifecycle-1');
    expect(updateReq.request.method).toBe('PUT');
    expect(updateReq.request.body.name).toBe('Hotfix');
    updateReq.flush({id: 'lifecycle-1', ...request, name: 'Hotfix'});

    service.replacePhases('lifecycle-1', request.phases).subscribe();
    const phasesReq = http.expectOne('/api/v1/lifecycles/lifecycle-1/phases');
    expect(phasesReq.request.method).toBe('PUT');
    expect(phasesReq.request.body).toEqual({phases: request.phases});
    phasesReq.flush({id: 'lifecycle-1', ...request});

    service.delete('lifecycle-1').subscribe();
    const deleteReq = http.expectOne('/api/v1/lifecycles/lifecycle-1');
    expect(deleteReq.request.method).toBe('DELETE');
    deleteReq.flush(null);
  });
});
