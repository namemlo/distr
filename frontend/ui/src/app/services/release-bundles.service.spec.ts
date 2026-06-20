import {provideHttpClient} from '@angular/common/http';
import {HttpTestingController, provideHttpClientTesting} from '@angular/common/http/testing';
import {TestBed} from '@angular/core/testing';
import {ReleaseBundlesService} from './release-bundles.service';

describe('ReleaseBundlesService', () => {
  let http: HttpTestingController;
  let service: ReleaseBundlesService;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting()],
    });
    http = TestBed.inject(HttpTestingController);
    service = TestBed.inject(ReleaseBundlesService);
  });

  afterEach(() => {
    http.verify();
  });

  it('performs release bundle CRUD requests', () => {
    const request = {
      applicationId: 'application-1',
      channelId: 'channel-1',
      releaseNumber: '2026.06.21',
      releaseNotes: 'Initial release',
      sourceRevision: 'abc123',
      components: [
        {
          key: 'api',
          name: 'API',
          type: 'application_version' as const,
          version: '1.2.3',
          applicationVersionId: 'version-1',
          packageRef: '',
          digest: '',
          checksum: '',
        },
      ],
    };

    service.list().subscribe((bundles) => {
      expect(bundles[0].releaseNumber).toBe('2026.06.21');
    });
    const listReq = http.expectOne('/api/v1/release-bundles');
    expect(listReq.request.method).toBe('GET');
    listReq.flush([{id: 'bundle-1', status: 'DRAFT', canonicalChecksum: 'sha256:abc', ...request}]);

    service.get('bundle-1').subscribe();
    const getReq = http.expectOne('/api/v1/release-bundles/bundle-1');
    expect(getReq.request.method).toBe('GET');
    getReq.flush({id: 'bundle-1', status: 'DRAFT', canonicalChecksum: 'sha256:abc', ...request});

    service.create(request).subscribe();
    const createReq = http.expectOne('/api/v1/release-bundles');
    expect(createReq.request.method).toBe('POST');
    expect(createReq.request.body).toEqual(request);
    createReq.flush({id: 'bundle-1', status: 'DRAFT', canonicalChecksum: 'sha256:abc', ...request});

    service.update('bundle-1', {...request, releaseNotes: 'Updated'}).subscribe();
    const updateReq = http.expectOne('/api/v1/release-bundles/bundle-1');
    expect(updateReq.request.method).toBe('PUT');
    expect(updateReq.request.body.releaseNotes).toBe('Updated');
    updateReq.flush({id: 'bundle-1', status: 'DRAFT', canonicalChecksum: 'sha256:def', ...request});

    service.delete('bundle-1').subscribe();
    const deleteReq = http.expectOne('/api/v1/release-bundles/bundle-1');
    expect(deleteReq.request.method).toBe('DELETE');
    deleteReq.flush(null);
  });

  it('calls validation and state transition endpoints', () => {
    service.validate('bundle-1').subscribe((result) => expect(result.valid).toBe(true));
    const validateReq = http.expectOne('/api/v1/release-bundles/bundle-1/validate');
    expect(validateReq.request.method).toBe('POST');
    validateReq.flush({valid: true, errors: [], warnings: []});

    service.publish('bundle-1').subscribe((bundle) => expect(bundle.status).toBe('PUBLISHED'));
    const publishReq = http.expectOne('/api/v1/release-bundles/bundle-1/publish');
    expect(publishReq.request.method).toBe('POST');
    publishReq.flush({id: 'bundle-1', status: 'PUBLISHED', components: []});

    service.block('bundle-1').subscribe((bundle) => expect(bundle.status).toBe('BLOCKED'));
    const blockReq = http.expectOne('/api/v1/release-bundles/bundle-1/block');
    expect(blockReq.request.method).toBe('POST');
    blockReq.flush({id: 'bundle-1', status: 'BLOCKED', components: []});

    service.archive('bundle-1').subscribe((bundle) => expect(bundle.status).toBe('ARCHIVED'));
    const archiveReq = http.expectOne('/api/v1/release-bundles/bundle-1/archive');
    expect(archiveReq.request.method).toBe('POST');
    archiveReq.flush({id: 'bundle-1', status: 'ARCHIVED', components: []});
  });
});
