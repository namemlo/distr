import {provideHttpClient} from '@angular/common/http';
import {HttpTestingController, provideHttpClientTesting} from '@angular/common/http/testing';
import {TestBed} from '@angular/core/testing';
import {CreateUpdateVariableSetRequest, ResolveVariablesPreviewRequest} from '../types/variable-set';
import {VariableSetsService} from './variable-sets.service';

describe('VariableSetsService', () => {
  let http: HttpTestingController;
  let service: VariableSetsService;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting()],
    });
    http = TestBed.inject(HttpTestingController);
    service = TestBed.inject(VariableSetsService);
  });

  afterEach(() => {
    http.verify();
  });

  it('lists variable sets from the variable sets endpoint', () => {
    service.list().subscribe((variableSets) => {
      expect(variableSets).toEqual([
        {
          id: 'variable-set-1',
          createdAt: '2026-06-21T09:30:00Z',
          updatedAt: '2026-06-21T10:45:00Z',
          name: 'Runtime Defaults',
          description: 'Shared application variables',
          sortOrder: 10,
          applicationIds: ['application-1'],
          variables: [
            {
              id: 'variable-1',
              createdAt: '2026-06-21T09:30:00Z',
              updatedAt: '2026-06-21T10:45:00Z',
              key: 'api_url',
              description: '',
              type: 'string',
              isRequired: false,
              defaultValue: 'https://example.test',
            },
          ],
        },
      ]);
    });

    const req = http.expectOne('/api/v1/variable-sets');
    expect(req.request.method).toBe('GET');
    req.flush([
      {
        id: 'variable-set-1',
        createdAt: '2026-06-21T09:30:00Z',
        updatedAt: '2026-06-21T10:45:00Z',
        name: 'Runtime Defaults',
        description: 'Shared application variables',
        sortOrder: 10,
        applicationIds: ['application-1'],
        variables: [
          {
            id: 'variable-1',
            createdAt: '2026-06-21T09:30:00Z',
            updatedAt: '2026-06-21T10:45:00Z',
            key: 'api_url',
            description: '',
            type: 'string',
            isRequired: false,
            defaultValue: 'https://example.test',
          },
        ],
      },
    ]);
  });

  it('creates, updates, and deletes variable sets', () => {
    const request: CreateUpdateVariableSetRequest = {
      name: 'Runtime Defaults',
      description: 'Shared application variables',
      sortOrder: 10,
      applicationIds: ['application-1'],
      variables: [
        {
          key: 'api_url',
          description: '',
          type: 'string',
          isRequired: false,
          defaultValue: 'https://example.test',
        },
      ],
    };

    service.create(request).subscribe();
    const createReq = http.expectOne('/api/v1/variable-sets');
    expect(createReq.request.method).toBe('POST');
    expect(createReq.request.body).toEqual(request);
    createReq.flush({id: 'variable-set-1', ...request});

    service.update('variable-set-1', {...request, name: 'Tenant Defaults'}).subscribe();
    const updateReq = http.expectOne('/api/v1/variable-sets/variable-set-1');
    expect(updateReq.request.method).toBe('PUT');
    expect(updateReq.request.body.name).toBe('Tenant Defaults');
    updateReq.flush({id: 'variable-set-1', ...request, name: 'Tenant Defaults'});

    service.delete('variable-set-1').subscribe();
    const deleteReq = http.expectOne('/api/v1/variable-sets/variable-set-1');
    expect(deleteReq.request.method).toBe('DELETE');
    deleteReq.flush(null);
  });

  it('previews scoped variable resolution', () => {
    const request: ResolveVariablesPreviewRequest = {
      variableSetIds: ['variable-set-1'],
      scope: {
        applicationId: 'application-1',
        targetTags: ['linux'],
      },
      promptedValues: [{key: 'api_url', value: 'https://prompted.example'}],
    };

    service.resolvePreview(request).subscribe((variables) => {
      expect(variables).toEqual([
        {
          variableSetId: 'variable-set-1',
          variableId: 'variable-1',
          key: 'api_url',
          type: 'string',
          isRequired: false,
          status: 'resolved',
          source: 'prompted',
          value: 'https://prompted.example',
          redacted: false,
          trace: [{source: 'prompted', scope: {}, selected: true, reason: 'matched resolution scope'}],
        },
      ]);
    });

    const req = http.expectOne('/api/v1/variables/resolve-preview');
    expect(req.request.method).toBe('POST');
    expect(req.request.body).toEqual(request);
    req.flush([
      {
        variableSetId: 'variable-set-1',
        variableId: 'variable-1',
        key: 'api_url',
        type: 'string',
        isRequired: false,
        status: 'resolved',
        source: 'prompted',
        value: 'https://prompted.example',
        redacted: false,
        trace: [{source: 'prompted', scope: {}, selected: true, reason: 'matched resolution scope'}],
      },
    ]);
  });
});
