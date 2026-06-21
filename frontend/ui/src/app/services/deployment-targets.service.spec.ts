import {provideHttpClient} from '@angular/common/http';
import {HttpTestingController, provideHttpClientTesting} from '@angular/common/http/testing';
import {TestBed} from '@angular/core/testing';
import {DeploymentTargetsService} from './deployment-targets.service';

describe('DeploymentTargetsService', () => {
  let http: HttpTestingController;
  let service: DeploymentTargetsService;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting()],
    });
    http = TestBed.inject(HttpTestingController);
    service = TestBed.inject(DeploymentTargetsService);
  });

  afterEach(() => {
    http.verify();
  });

  it('gets deployment configuration drift', () => {
    service.getConfigurationDrift('deployment-1').subscribe((drift) => {
      expect(drift.deploymentId).toBe('deployment-1');
      expect(drift.hasDrift).toBe(true);
      expect(drift.newRequiredVariables[0].key).toBe('REPLICAS');
    });

    const req = http.expectOne('/api/v1/deployments/deployment-1/configuration-drift');
    expect(req.request.method).toBe('GET');
    req.flush({
      deploymentId: 'deployment-1',
      applicationId: 'application-1',
      hasDrift: true,
      newRequiredVariables: [{key: 'REPLICAS', type: 'number', isRequired: true, source: 'unresolved'}],
      missingVariables: [],
      removedVariables: [],
      typeChanges: [],
      defaultChanges: [],
      secretReferenceChanges: [],
    });
  });
});
