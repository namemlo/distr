import {ComponentFixture, TestBed} from '@angular/core/testing';
import {UserRole} from '@distr-sh/distr-sdk';
import {of, Subject, throwError} from 'rxjs';
import {vi} from 'vitest';
import {AuthService} from '../../services/auth.service';
import {FeatureFlagService} from '../../services/feature-flag.service';
import {TargetConfigSnapshotsService} from '../../services/target-config-snapshots.service';
import {
  CreateTargetConfigSnapshotRequest,
  TargetConfigSnapshot,
  TargetConfigSnapshotVerification,
} from '../../types/target-config-snapshot';
import {TargetConfigSnapshotsComponent} from './target-config-snapshots.component';

describe('TargetConfigSnapshotsComponent', () => {
  let service: {
    list: ReturnType<typeof vi.fn>;
    create: ReturnType<typeof vi.fn>;
    get: ReturnType<typeof vi.fn>;
    verify: ReturnType<typeof vi.fn>;
  };
  let auth: {
    isVendor: ReturnType<typeof vi.fn>;
    isSuperAdmin: ReturnType<typeof vi.fn>;
    hasAnyRole: ReturnType<typeof vi.fn>;
  };
  let mutationEnabled$: Subject<boolean>;

  beforeEach(() => {
    const snapshot = snapshotFixture();
    service = {
      list: vi.fn().mockReturnValue(of({items: [snapshot]})),
      create: vi.fn().mockReturnValue(of(snapshot)),
      get: vi.fn().mockReturnValue(of(snapshot)),
      verify: vi.fn().mockReturnValue(of(verificationFixture())),
    };
    auth = {
      isVendor: vi.fn().mockReturnValue(true),
      isSuperAdmin: vi.fn().mockReturnValue(false),
      hasAnyRole: vi.fn().mockReturnValue(true),
    };
    mutationEnabled$ = new Subject<boolean>();
    TestBed.configureTestingModule({
      imports: [TargetConfigSnapshotsComponent],
      providers: [
        {provide: TargetConfigSnapshotsService, useValue: service},
        {provide: AuthService, useValue: auth},
        {
          provide: FeatureFlagService,
          useValue: {isExperimentalFeatureEnabled$: vi.fn(() => mutationEnabled$)},
        },
      ],
    });
  });

  it('loads immutable snapshots and retrieves a selected snapshot without rendering secret values', async () => {
    const {component, fixture} = await createComponent();
    const snapshot = snapshotFixture() as TargetConfigSnapshot & {value?: string; plaintext?: string};
    snapshot.value = 'DO_NOT_RENDER_SECRET_VALUE';
    snapshot.plaintext = 'DO_NOT_RENDER_PLAINTEXT';
    service.get.mockReturnValue(of(snapshot));

    await (component as any).selectSnapshot(snapshot);
    fixture.detectChanges();
    const text = fixture.nativeElement.textContent;

    expect(service.list).toHaveBeenCalledWith({limit: 50});
    expect(service.get).toHaveBeenCalledWith('snapshot-1');
    expect(text).toContain('vault');
    expect(text).toContain('kv/releases/database');
    expect(text).toContain('sha256:bbbb');
    expect(text).not.toContain('DO_NOT_RENDER_SECRET_VALUE');
    expect(text).not.toContain('DO_NOT_RENDER_PLAINTEXT');
    expect(text).toContain('Immutable');
    expect(text).toContain('Created by user');
    expect(text).toContain('11111111-1111-4111-8111-111111111111');
  });

  it('creates a snapshot from strict metadata-only JSON and refreshes the immutable list', async () => {
    const {component} = await createComponent();
    const request = createRequestFixture();
    (component as any).createForm.patchValue({
      deploymentUnitId: request.deploymentUnitId,
      targetEnvironmentAssignmentId: request.targetEnvironmentAssignmentId,
      environmentId: request.environmentId,
      sourceRepository: request.sourceRepository,
      sourceCommit: request.sourceCommit,
      sourceAdapter: request.sourceAdapter,
      adapterVersion: request.adapterVersion,
      targetPlatform: request.targetPlatform,
      runtimeConstraints: JSON.stringify(request.runtimeConstraints),
      objects: JSON.stringify(request.objects),
      components: JSON.stringify(request.components),
      secretReferences: JSON.stringify(request.secretReferences),
      featureFlags: JSON.stringify(request.featureFlags),
    });

    await (component as any).createSnapshot();

    expect(service.create).toHaveBeenCalledWith(request);
    expect(service.list).toHaveBeenCalledTimes(2);
    expect((component as any).selectedSnapshot().id).toBe('snapshot-1');
  });

  it('uses the opaque cursor to append the next snapshot page', async () => {
    const first = snapshotFixture();
    const second = {...snapshotFixture(), id: 'snapshot-2'};
    service.list
      .mockReturnValueOnce(of({items: [first], nextCursor: 'cursor-2'}))
      .mockReturnValueOnce(of({items: [second]}));
    const {component} = await createComponent();

    await (component as any).loadMore();

    expect(service.list.mock.calls[1][0]).toEqual({cursor: 'cursor-2', limit: 50});
    expect((component as any).snapshots().map((snapshot: TargetConfigSnapshot) => snapshot.id)).toEqual([
      'snapshot-1',
      'snapshot-2',
    ]);
    expect((component as any).nextCursor()).toBeUndefined();
  });

  it('rejects secret value fields in the create form before sending them', async () => {
    const {component} = await createComponent();
    (component as any).createForm.patchValue({
      deploymentUnitId: 'unit-1',
      targetEnvironmentAssignmentId: 'assignment-1',
      environmentId: 'environment-1',
      sourceRepository: 'https://example.invalid/product.git',
      sourceCommit: '0123456789abcdef0123456789abcdef01234567',
      sourceAdapter: 'compose',
      adapterVersion: '1.0.0',
      targetPlatform: 'linux/amd64',
      runtimeConstraints: '{}',
      objects: '[]',
      components: '[]',
      secretReferences: '[{"key":"database","provider":"vault","reference":"kv/database","value":"plaintext"}]',
      featureFlags: '[]',
    });

    await (component as any).createSnapshot();

    expect(service.create).not.toHaveBeenCalled();
    expect((component as any).actionError()).toContain('metadata');
    expect((component as any).actionError()).not.toContain('plaintext');
  });

  it('verifies immutable objects and bounds provider diagnostics before rendering', async () => {
    const {component, fixture} = await createComponent();
    const snapshot = snapshotFixture();
    service.verify.mockReturnValue(
      of({
        ...verificationFixture(),
        verified: false,
        objects: [
          {
            key: 'compose',
            verified: false,
            code: 'verification_failed',
            message: `${'x'.repeat(300)}UNBOUNDED_TRAILER`,
          },
        ],
      })
    );
    (component as any).selectedSnapshot.set(snapshot);

    await (component as any).verifySnapshot();
    fixture.detectChanges();

    expect(service.verify).toHaveBeenCalledWith(snapshot.id);
    expect((component as any).verification().verified).toBe(false);
    expect(
      (component as any).verificationMessage((component as any).verification().objects[0]).length
    ).toBeLessThanOrEqual(240);
    expect(fixture.nativeElement.textContent).not.toContain('UNBOUNDED_TRAILER');
  });

  it('renders verification unavailable distinctly from a mismatch', async () => {
    const {component, fixture} = await createComponent();
    const snapshot = snapshotFixture();
    const unavailable: TargetConfigSnapshotVerification = {
      snapshotId: snapshot.id,
      verified: false,
      objects: [
        {
          key: 'compose',
          verified: false,
          code: 'verification_unavailable',
          message: 'Object verification is unavailable.\nRetry after object storage is configured.',
        },
      ],
    };
    service.verify.mockReturnValue(of(unavailable));
    (component as any).selectedSnapshot.set(snapshot);

    await (component as any).verifySnapshot();
    fixture.detectChanges();

    expect(fixture.nativeElement.textContent).toContain('Verification unavailable');
    expect(fixture.nativeElement.textContent).not.toContain('Mismatch detected');
    expect(fixture.nativeElement.textContent).toContain(
      'Object verification is unavailable. Retry after object storage is configured.'
    );
  });

  it('renders every observed object fact for mismatch audit', async () => {
    const {component, fixture} = await createComponent();
    const snapshot = snapshotFixture();
    service.verify.mockReturnValue(
      of({
        snapshotId: snapshot.id,
        verified: false,
        objects: [
          {
            key: 'compose',
            verified: false,
            code: 'version_mismatch',
            message: 'Object version does not match snapshot.',
            observedVersionId: 'version-8',
            observedMediaType: 'application/json',
            observedSizeBytes: 0,
            observedChecksum: 'sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd',
          },
        ],
      } satisfies TargetConfigSnapshotVerification)
    );
    (component as any).selectedSnapshot.set(snapshot);

    await (component as any).verifySnapshot();
    fixture.detectChanges();

    const text = fixture.nativeElement.textContent;
    expect(text).toContain('Observed version');
    expect(text).toContain('version-8');
    expect(text).toContain('Observed media type');
    expect(text).toContain('application/json');
    expect(text).toContain('Observed size');
    expect(text).toContain('0 bytes');
    expect(text).toContain('Observed checksum');
    expect(text).toContain('sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd');
  });

  it('uses bounded operator-safe errors for list, detail, create, and verify failures', async () => {
    service.list.mockReturnValue(throwError(() => new Error('raw list database details')));
    const {component} = await createComponent();
    expect((component as any).loadError()).toBe('Could not load target configuration snapshots.');

    service.get.mockReturnValue(throwError(() => new Error('raw detail provider details')));
    await (component as any).selectSnapshot(snapshotFixture());
    expect((component as any).actionError()).toBe('Could not load the target configuration snapshot.');

    service.verify.mockReturnValue(throwError(() => new Error('raw verification provider details')));
    (component as any).selectedSnapshot.set(snapshotFixture());
    await (component as any).verifySnapshot();
    expect((component as any).actionError()).toBe('Could not verify the target configuration snapshot.');
    expect((component as any).actionError()).not.toContain('provider');
  });

  const readOnlyCases: {role: UserRole; superAdmin: boolean; flag: boolean; vendor?: boolean}[] = [
    {role: 'read_only', superAdmin: false, flag: true},
    {role: 'read_write', superAdmin: false, flag: false},
    {role: 'admin', superAdmin: true, flag: true},
    {role: 'read_write', superAdmin: false, flag: true, vendor: false},
  ];
  for (const {role, superAdmin, flag, vendor = true} of readOnlyCases) {
    it(`blocks create and verify for role=${role} superAdmin=${superAdmin} flag=${flag} vendor=${vendor}`, async () => {
      auth.isVendor.mockReturnValue(vendor);
      auth.isSuperAdmin.mockReturnValue(superAdmin);
      auth.hasAnyRole.mockImplementation((...roles: UserRole[]) => roles.includes(role));
      const {component, fixture} = await createComponent(flag);
      const request = createRequestFixture();
      fillCreateForm(component, request);
      (component as any).selectedSnapshot.set(snapshotFixture());

      await (component as any).createSnapshot();
      await (component as any).verifySnapshot();
      fixture.detectChanges();

      expect(service.list).toHaveBeenCalledWith({limit: 50});
      expect(service.create).not.toHaveBeenCalled();
      expect(service.verify).not.toHaveBeenCalled();
      expect(fixture.nativeElement.textContent).toContain('Immutable history');
      expect(fixture.nativeElement.textContent).not.toContain('Create immutable snapshot');
      expect(fixture.nativeElement.textContent).not.toContain('Verify objects');
    });
  }

  for (const role of ['read_write', 'admin'] satisfies UserRole[]) {
    it(`enables create and verify controls for a non-super-admin ${role} when the mutation flag is enabled`, async () => {
      auth.hasAnyRole.mockImplementation((...roles: UserRole[]) => roles.includes(role));
      const {component, fixture} = await createComponent(true);
      (component as any).selectedSnapshot.set(snapshotFixture());
      fixture.detectChanges();

      expect(fixture.nativeElement.textContent).toContain('Create immutable snapshot');
      expect(fixture.nativeElement.textContent).toContain('Verify objects');
    });
  }

  it('discards a stale verification completion and finally state after selecting and verifying another snapshot', async () => {
    const verificationA$ = new Subject<TargetConfigSnapshotVerification>();
    const verificationB$ = new Subject<TargetConfigSnapshotVerification>();
    service.verify.mockReturnValueOnce(verificationA$).mockReturnValueOnce(verificationB$);
    const {component} = await createComponent(true);
    const snapshotA = snapshotFixture();
    const snapshotB = {...snapshotFixture(), id: 'snapshot-2'};
    service.get.mockReturnValue(of(snapshotB));
    (component as any).selectedSnapshot.set(snapshotA);

    const verifyA = (component as any).verifySnapshot();
    expect((component as any).verifying()).toBe(true);
    await (component as any).selectSnapshot(snapshotB);
    const verifyB = (component as any).verifySnapshot();
    expect((component as any).verifying()).toBe(true);

    verificationA$.next(verificationFixture());
    verificationA$.complete();
    await verifyA;

    expect((component as any).selectedSnapshot().id).toBe(snapshotB.id);
    expect((component as any).verification()).toBeUndefined();
    expect((component as any).verifying()).toBe(true);

    verificationB$.next({...verificationFixture(), snapshotId: snapshotB.id});
    verificationB$.complete();
    await verifyB;

    expect((component as any).verification().snapshotId).toBe(snapshotB.id);
    expect((component as any).verifying()).toBe(false);
  });

  it('does not let a stale verification error overwrite newer snapshot evidence or loading state', async () => {
    const verificationA$ = new Subject<TargetConfigSnapshotVerification>();
    const verificationB$ = new Subject<TargetConfigSnapshotVerification>();
    service.verify.mockReturnValueOnce(verificationA$).mockReturnValueOnce(verificationB$);
    const {component} = await createComponent(true);
    const snapshotA = snapshotFixture();
    const snapshotB = {...snapshotFixture(), id: 'snapshot-2'};
    service.get.mockReturnValue(of(snapshotB));
    (component as any).selectedSnapshot.set(snapshotA);

    const verifyA = (component as any).verifySnapshot();
    await (component as any).selectSnapshot(snapshotB);
    const verifyB = (component as any).verifySnapshot();
    verificationB$.next({...verificationFixture(), snapshotId: snapshotB.id});
    verificationB$.complete();
    await verifyB;
    verificationA$.error(new Error('stale provider failure'));
    await verifyA;

    expect((component as any).verification().snapshotId).toBe(snapshotB.id);
    expect((component as any).actionError()).toBe('');
    expect((component as any).verifying()).toBe(false);
  });

  it('discards a verification response whose snapshot ID does not match the captured selection', async () => {
    const {component} = await createComponent(true);
    (component as any).selectedSnapshot.set(snapshotFixture());
    service.verify.mockReturnValue(of({...verificationFixture(), snapshotId: 'snapshot-2'}));

    await (component as any).verifySnapshot();

    expect((component as any).verification()).toBeUndefined();
    expect((component as any).actionError()).toBe('Could not verify the target configuration snapshot.');
  });

  async function createComponent(mutationFlag = true): Promise<{
    fixture: ComponentFixture<TargetConfigSnapshotsComponent>;
    component: TargetConfigSnapshotsComponent;
  }> {
    const fixture = TestBed.createComponent(TargetConfigSnapshotsComponent);
    mutationEnabled$.next(mutationFlag);
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();
    return {fixture, component: fixture.componentInstance};
  }

  function fillCreateForm(component: TargetConfigSnapshotsComponent, request: CreateTargetConfigSnapshotRequest): void {
    (component as any).createForm.patchValue({
      deploymentUnitId: request.deploymentUnitId,
      targetEnvironmentAssignmentId: request.targetEnvironmentAssignmentId,
      environmentId: request.environmentId,
      sourceRepository: request.sourceRepository,
      sourceCommit: request.sourceCommit,
      sourceAdapter: request.sourceAdapter,
      adapterVersion: request.adapterVersion,
      targetPlatform: request.targetPlatform,
      runtimeConstraints: JSON.stringify(request.runtimeConstraints),
      objects: JSON.stringify(request.objects),
      components: JSON.stringify(request.components),
      secretReferences: JSON.stringify(request.secretReferences),
      featureFlags: JSON.stringify(request.featureFlags),
    });
  }
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

function verificationFixture(): TargetConfigSnapshotVerification {
  return {
    snapshotId: 'snapshot-1',
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
        observedChecksum: 'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
      },
    ],
  };
}
