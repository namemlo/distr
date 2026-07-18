import {provideHttpClient} from '@angular/common/http';
import {HttpTestingController, provideHttpClientTesting} from '@angular/common/http/testing';
import {TestBed} from '@angular/core/testing';
import {
  RegistryCoverage,
  RegistryImport,
  RegistryImportRequest,
  RegistryImportResult,
  RegistryImportRoot,
} from '../types/deployment-registry';
import {DeploymentRegistryService} from './deployment-registry.service';

const evidenceChecksum = '0'.repeat(64);
const previewChecksum = `sha256:${'1'.repeat(64)}`;

describe('DeploymentRegistryService', () => {
  let http: HttpTestingController;
  let service: DeploymentRegistryService;

  beforeEach(() => {
    TestBed.configureTestingModule({providers: [provideHttpClient(), provideHttpClientTesting()]});
    http = TestBed.inject(HttpTestingController);
    service = TestBed.inject(DeploymentRegistryService);
  });

  afterEach(() => http.verify());

  it('sends the exact typed preview request and reads the complete preview response', () => {
    const request = importRequest();
    const response = importPreview();

    service.preview(request).subscribe((result) => {
      expect(result).toEqual(response);
      expect(result.diagnostics[0]).toEqual({
        code: 'classification_required',
        field: 'roots[0].classification',
        message: 'Root requires an operator decision.',
      });
      expect(result.diff.creates[0].physicalName).toBe('transaction-api');
      expect(result.omissions).toEqual(['root-1/legacy-api: missing physical identity']);
    });

    const preview = http.expectOne('/api/v1/deployment-registry/imports/preview');
    expect(preview.request.method).toBe('POST');
    expect(preview.request.body).toEqual(request);
    preview.flush(response);
  });

  it('sends the exact classification decision and accepts the 204 response', () => {
    service.saveDecision('import-1', {rootKey: 'root-1', classification: 'shared'}).subscribe();

    const decision = http.expectOne('/api/v1/deployment-registry/imports/import-1/decisions');
    expect(decision.request.method).toBe('POST');
    expect(decision.request.body).toEqual({rootKey: 'root-1', classification: 'shared'});
    decision.flush(null, {status: 204, statusText: 'No Content'});
  });

  it('sends the preview checksum and reads every apply result field', () => {
    const response = importResult();

    service.apply('import-1', previewChecksum).subscribe((result) => expect(result).toEqual(response));

    const apply = http.expectOne('/api/v1/deployment-registry/imports/import-1/apply');
    expect(apply.request.method).toBe('POST');
    expect(apply.request.body).toEqual({previewChecksum});
    apply.flush(response);
  });

  it('reads the complete stored preview response', () => {
    const response = importPreview();

    service.get('import-1').subscribe((result) => expect(result).toEqual(response));

    const get = http.expectOne('/api/v1/deployment-registry/imports/import-1');
    expect(get.request.method).toBe('GET');
    get.flush(response);
  });

  it('sends the required importId query parameter and reads string omissions', () => {
    const response = partialCoverage();

    service.coverage('import-1').subscribe((coverage) => {
      expect(coverage).toEqual(response);
      expect(coverage.omissions).toEqual(['root-20', 'root-21', 'root-22', 'root-23', 'root-24', 'root-25', 'root-26']);
    });

    const coverage = http.expectOne(`/api/v1/deployment-registry/coverage?importId=${encodeURIComponent('import-1')}`);
    expect(coverage.request.method).toBe('GET');
    expect(coverage.request.params.keys()).toEqual(['importId']);
    coverage.flush(response);
  });

  it('keeps rejected source paths outside the exact public root DTO', () => {
    const root: RegistryImportRoot = {
      ...importRequest().roots[0],
      // @ts-expect-error sourcePath is deliberately not accepted by the public API.
      sourcePath: 'C:\\private\\compose.yaml',
    };

    expect(root).toBeDefined();
  });
});

function importRequest(): RegistryImportRequest {
  return {
    sourceKind: 'inventory_report',
    toolName: 'registry-audit',
    toolVersion: '1.0',
    sourceCommit: 'a'.repeat(40),
    parameters: {scope: 'all', format: 'normalized-v1'},
    evidenceReference: `evidence://sha256/${evidenceChecksum}`,
    evidenceChecksum,
    roots: [
      {
        key: 'root-1',
        name: 'Root 1',
        deliveryModel: 'dedicated',
        classification: 'needs_decision',
        customerOrganizationId: '11111111-1111-1111-1111-111111111111',
        deploymentTargetId: '22222222-2222-2222-2222-222222222222',
        environmentId: '33333333-3333-3333-3333-333333333333',
        subscriberCustomerOrganizationIds: ['44444444-4444-4444-4444-444444444444'],
        physicalIdentity: 'identity-root-1',
        placements: [
          {
            componentKey: 'transaction-api',
            physicalName: 'transaction-api',
            configNamespace: 'transaction-api-dev',
            databaseBoundary: 'transaction-api-db',
            healthAdapter: 'http',
            renamedFrom: 'transaction-api-legacy',
          },
        ],
      },
    ],
    sourcePlacements: [{rootKey: 'root-1', physicalName: 'transaction-api'}],
  };
}

function importPreview(): RegistryImport {
  return {
    id: 'import-1',
    previewChecksum,
    counts: {
      discoveredRoots: 1,
      classifiedRoots: 0,
      discoveredPlacements: 1,
      omittedPlacements: 0,
      creates: 1,
      updates: 0,
      retirements: 0,
      conflicts: 0,
    },
    diagnostics: [
      {
        code: 'classification_required',
        field: 'roots[0].classification',
        message: 'Root requires an operator decision.',
      },
    ],
    diagnosticsTruncated: false,
    omissions: ['root-1/legacy-api: missing physical identity'],
    roots: importRequest().roots,
    diff: {
      creates: [
        {
          kind: 'root',
          rootKey: 'root-1',
          physicalName: 'transaction-api',
          message: 'Create deployment root root-1.',
        },
      ],
      updates: [],
      retirements: [],
      conflicts: [],
    },
  };
}

function importResult(): RegistryImportResult {
  return {
    id: 'import-1',
    previewChecksum,
    state: 'applied',
    applied: true,
    counts: importPreview().counts,
    checkpoint: 1,
  };
}

function partialCoverage(): RegistryCoverage {
  return {
    importId: 'import-1',
    discoveredRoots: 26,
    classifiedRoots: 19,
    actionableManagedRoots: 12,
    observeOnlyRoots: 2,
    externalRoots: 2,
    ignoredRoots: 3,
    unresolvedRoots: 7,
    discoveredPlacements: 28,
    services: 28,
    omittedPlacements: 0,
    omissions: ['root-20', 'root-21', 'root-22', 'root-23', 'root-24', 'root-25', 'root-26'],
    complete: false,
  };
}
