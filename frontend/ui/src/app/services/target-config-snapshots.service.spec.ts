import {provideHttpClient} from '@angular/common/http';
import {HttpTestingController, provideHttpClientTesting} from '@angular/common/http/testing';
import {TestBed} from '@angular/core/testing';
import {
  CreateTargetConfigSnapshotRequest,
  TargetConfigSnapshot,
  TargetConfigSnapshotVerification,
} from '../types/target-config-snapshot';
import {TargetConfigSnapshotsService} from './target-config-snapshots.service';

describe('TargetConfigSnapshotsService', () => {
  let http: HttpTestingController;
  let service: TargetConfigSnapshotsService;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting()],
    });
    http = TestBed.inject(HttpTestingController);
    service = TestBed.inject(TargetConfigSnapshotsService);
  });

  afterEach(() => {
    http.verify();
  });

  it('uses the immutable list, create, get, and verify endpoints with the exact contract', () => {
    const snapshot = snapshotFixture();
    const createRequest = createRequestFixture();
    const verification: TargetConfigSnapshotVerification = {
      snapshotId: snapshot.id,
      verified: true,
      objects: [
        {
          key: 'compose',
          verified: true,
          code: 'verified',
          message: 'Object metadata and digest match.',
          observedVersionId: 'version-7',
          observedMediaType: 'application/yaml',
          observedSizeBytes: 2048,
          observedChecksum: snapshot.objects[0].checksum,
        },
      ],
    };

    service
      .list({
        deploymentUnitId: 'unit-1',
        targetEnvironmentAssignmentId: 'assignment-1',
        cursor: 'cursor-1',
        limit: 100,
      })
      .subscribe((page) => expect(page.items[0].id).toBe(snapshot.id));
    const listReq = http.expectOne(
      '/api/v1/target-config-snapshots/?deploymentUnitId=unit-1&targetEnvironmentAssignmentId=assignment-1&cursor=cursor-1&limit=100'
    );
    expect(listReq.request.method).toBe('GET');
    listReq.flush({items: [snapshot], nextCursor: 'cursor-2'});

    service
      .create(createRequest)
      .subscribe((created) => expect(created.canonicalChecksum).toBe(snapshot.canonicalChecksum));
    const createReq = http.expectOne('/api/v1/target-config-snapshots/');
    expect(createReq.request.method).toBe('POST');
    expect(createReq.request.body).toEqual(createRequest);
    createReq.flush(snapshot);

    service.get(snapshot.id).subscribe((result) => expect(result.id).toBe(snapshot.id));
    const getReq = http.expectOne(`/api/v1/target-config-snapshots/${snapshot.id}/`);
    expect(getReq.request.method).toBe('GET');
    getReq.flush(snapshot);

    service.verify(snapshot.id).subscribe((result) => expect(result.verified).toBe(true));
    const verifyReq = http.expectOne(`/api/v1/target-config-snapshots/${snapshot.id}/verify`);
    expect(verifyReq.request.method).toBe('POST');
    expect(verifyReq.request.body).toEqual({});
    verifyReq.flush(verification);
  });
});

function createRequestFixture(): CreateTargetConfigSnapshotRequest {
  return {
    deploymentUnitId: 'unit-1',
    targetEnvironmentAssignmentId: 'assignment-1',
    environmentId: 'environment-1',
    sourceRepository: 'https://example.invalid/product.git',
    sourceCommit: '0123456789abcdef0123456789abcdef01234567',
    sourceAdapter: 'compose',
    adapterVersion: '1.0.0',
    targetPlatform: 'linux/amd64',
    runtimeConstraints: {docker: '>=27'},
    objects: [
      {
        key: 'compose',
        kind: 'deployment_descriptor',
        reference:
          's3://config-bucket/_immutable/sha256/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/compose.yaml',
        versionId: 'version-7',
        mediaType: 'application/yaml',
        sizeBytes: 2048,
        checksum: 'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
      },
    ],
    components: [{physicalName: 'api', componentInstanceId: 'component-1', deploymentUnitId: 'unit-1'}],
    secretReferences: [
      {
        key: 'database',
        provider: 'vault',
        reference: 'kv/releases/database',
        versionFingerprint: 'sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
      },
    ],
    featureFlags: [{key: 'new_checkout', enabled: true}],
  };
}

function snapshotFixture(): TargetConfigSnapshot {
  const request = createRequestFixture();
  return {
    id: 'snapshot-1',
    createdAt: '2026-07-18T09:30:00Z',
    createdByUserAccountId: '11111111-1111-4111-8111-111111111111',
    deploymentUnitId: request.deploymentUnitId,
    targetEnvironmentAssignmentId: request.targetEnvironmentAssignmentId,
    environmentId: request.environmentId,
    sourceRepository: request.sourceRepository,
    sourceCommit: request.sourceCommit,
    sourceAdapter: request.sourceAdapter,
    adapterVersion: request.adapterVersion,
    targetPlatform: request.targetPlatform,
    runtimeConstraints: request.runtimeConstraints,
    canonicalChecksum: 'sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc',
    objects: request.objects,
    components: request.components.map(({physicalName, componentInstanceId}) => ({
      physicalName,
      componentInstanceId,
    })),
    secretReferences: request.secretReferences.map(({key, provider, reference, versionFingerprint}) => ({
      key,
      provider,
      opaqueReference: reference,
      versionFingerprint,
    })),
    featureFlags: request.featureFlags,
  };
}
