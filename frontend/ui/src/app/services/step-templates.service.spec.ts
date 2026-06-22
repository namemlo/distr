import {provideHttpClient} from '@angular/common/http';
import {HttpTestingController, provideHttpClientTesting} from '@angular/common/http/testing';
import {TestBed} from '@angular/core/testing';
import {ImportStepTemplateRequest} from '../types/step-template';
import {StepTemplatesService} from './step-templates.service';

describe('StepTemplatesService', () => {
  let http: HttpTestingController;
  let service: StepTemplatesService;

  const request: ImportStepTemplateRequest = {
    sourceType: 'builtin',
    sourceRef: 'builtin/http-health-check',
    name: 'HTTP health check',
    description: 'Checks that an HTTP endpoint returns a healthy status.',
    category: 'Health',
    version: '1.0.0',
    actionType: 'distr.http.check',
    executionLocation: 'hub',
    inputSchema: {type: 'object'},
    outputSchema: {type: 'object'},
    defaultInputBindings: {url: 'https://example.com/health'},
    minimumAgentVersion: '1.0.0',
    compatibleActionVersion: '1',
    runtimeCompatibilityNotes: 'Uses the built-in HTTP check action.',
    deprecated: false,
  };

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting()],
    });
    http = TestBed.inject(HttpTestingController);
    service = TestBed.inject(StepTemplatesService);
  });

  afterEach(() => {
    http.verify();
  });

  it('calls step template list, get, and import endpoints', () => {
    service.list().subscribe((templates) => {
      expect(templates[0].name).toBe('HTTP health check');
    });
    const listReq = http.expectOne('/api/v1/step-templates');
    expect(listReq.request.method).toBe('GET');
    listReq.flush([{id: 'template-1', versions: [], ...request}]);

    service.get('template-1').subscribe();
    const getReq = http.expectOne('/api/v1/step-templates/template-1');
    expect(getReq.request.method).toBe('GET');
    getReq.flush({id: 'template-1', versions: [], ...request});

    service.importTemplate(request).subscribe();
    const importReq = http.expectOne('/api/v1/step-templates/import');
    expect(importReq.request.method).toBe('POST');
    expect(importReq.request.body).toEqual(request);
    importReq.flush({id: 'template-1', versions: [], ...request});
  });
});
