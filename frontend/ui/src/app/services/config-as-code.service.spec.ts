import {provideHttpClient} from '@angular/common/http';
import {HttpTestingController, provideHttpClientTesting} from '@angular/common/http/testing';
import {TestBed} from '@angular/core/testing';
import {ConfigAsCodeService} from './config-as-code.service';

describe('ConfigAsCodeService', () => {
  let http: HttpTestingController;
  let service: ConfigAsCodeService;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting()],
    });
    http = TestBed.inject(HttpTestingController);
    service = TestBed.inject(ConfigAsCodeService);
  });

  afterEach(() => {
    http.verify();
  });

  it('validates documents without mutating resources', () => {
    service.validate({documents: [{content: 'apiVersion: distr.sh/v1alpha1\nkind: Channel\n'}]}).subscribe((result) => {
      expect(result.valid).toBe(true);
      expect(result.documents[0].kind).toBe('Channel');
      expect(result.documents[0].canonicalChecksum).toBe('a'.repeat(64));
    });

    const req = http.expectOne('/api/v1/config-as-code/validate');
    expect(req.request.method).toBe('POST');
    expect(req.request.body.documents.length).toBe(1);
    req.flush({
      valid: true,
      documents: [
        {
          kind: 'Channel',
          apiVersion: 'distr.sh/v1alpha1',
          canonicalChecksum: 'a'.repeat(64),
        },
      ],
      errors: [],
      warnings: [],
    });
  });

  it('loads and updates resource authority', () => {
    service
      .getAuthority('Channel', '00000000-0000-0000-0000-000000000001')
      .subscribe((authority) => expect(authority.authority).toBe('GIT_MANAGED'));

    const getReq = http.expectOne('/api/v1/config-as-code/authorities/Channel/00000000-0000-0000-0000-000000000001');
    expect(getReq.request.method).toBe('GET');
    getReq.flush({
      resourceKind: 'Channel',
      resourceId: '00000000-0000-0000-0000-000000000001',
      authority: 'GIT_MANAGED',
      repositoryPath: 'channels/stable.yaml',
      sourceRevision: 'abc123',
      documentChecksum: 'b'.repeat(64),
      updatedAt: '2026-06-23T00:00:00Z',
    });

    service
      .updateAuthority('Channel', '00000000-0000-0000-0000-000000000001', {
        authority: 'DATABASE_MANAGED',
        repositoryPath: '',
        sourceRevision: '',
        documentChecksum: '',
      })
      .subscribe((authority) => expect(authority.authority).toBe('DATABASE_MANAGED'));

    const putReq = http.expectOne('/api/v1/config-as-code/authorities/Channel/00000000-0000-0000-0000-000000000001');
    expect(putReq.request.method).toBe('PUT');
    expect(putReq.request.body.authority).toBe('DATABASE_MANAGED');
    putReq.flush({
      resourceKind: 'Channel',
      resourceId: '00000000-0000-0000-0000-000000000001',
      authority: 'DATABASE_MANAGED',
      repositoryPath: '',
      sourceRevision: '',
      documentChecksum: '',
      updatedAt: '2026-06-23T00:01:00Z',
    });
  });
});
