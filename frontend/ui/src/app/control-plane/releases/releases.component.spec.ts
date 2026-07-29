import {ComponentFixture, TestBed} from '@angular/core/testing';
import {ActivatedRoute, convertToParamMap, ParamMap, Router} from '@angular/router';
import {BehaviorSubject, of, Subject, throwError} from 'rxjs';
import {vi} from 'vitest';
import {OperatorControlPlaneService} from '../../services/operator-control-plane.service';
import {OverlayService} from '../../services/overlay.service';
import {
  OperatorEvidencePage,
  OperatorPage,
  OperatorReleaseCompareResponse,
  OperatorReleaseDetailResponse,
  OperatorReleaseRow,
} from '../../types/operator-control-plane';
import {ReleasesComponent} from './releases.component';

describe('ReleasesComponent', () => {
  let service: {
    listReleases: ReturnType<typeof vi.fn>;
    getRelease: ReturnType<typeof vi.fn>;
    compareReleases: ReturnType<typeof vi.fn>;
    getReleaseEvidence: ReturnType<typeof vi.fn>;
    createComponentRelease: ReturnType<typeof vi.fn>;
    validateComponentRelease: ReturnType<typeof vi.fn>;
    publishComponentRelease: ReturnType<typeof vi.fn>;
    createProductRelease: ReturnType<typeof vi.fn>;
    validateProductRelease: ReturnType<typeof vi.fn>;
    publishProductRelease: ReturnType<typeof vi.fn>;
  };
  let overlay: {confirm: ReturnType<typeof vi.fn>};
  let router: {navigate: ReturnType<typeof vi.fn>};
  let params$: BehaviorSubject<ParamMap>;

  beforeEach(() => {
    service = {
      listReleases: vi.fn(),
      getRelease: vi.fn(),
      compareReleases: vi.fn(),
      getReleaseEvidence: vi.fn(),
      createComponentRelease: vi.fn(),
      validateComponentRelease: vi.fn(),
      publishComponentRelease: vi.fn(),
      createProductRelease: vi.fn(),
      validateProductRelease: vi.fn(),
      publishProductRelease: vi.fn(),
    };
    overlay = {confirm: vi.fn().mockReturnValue(of(true))};
    router = {navigate: vi.fn().mockResolvedValue(true)};
    params$ = new BehaviorSubject(convertToParamMap({}));
    TestBed.configureTestingModule({
      imports: [ReleasesComponent],
      providers: [
        {provide: OperatorControlPlaneService, useValue: service},
        {provide: ActivatedRoute, useValue: {paramMap: params$.asObservable()}},
        {provide: OverlayService, useValue: overlay},
        {provide: Router, useValue: router},
      ],
    });
  });

  it('announces loading and renders the paginated release history', async () => {
    const response$ = new Subject<OperatorPage<OperatorReleaseRow>>();
    service.listReleases.mockReturnValue(response$);

    const fixture = TestBed.createComponent(ReleasesComponent);
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('[role="status"]').textContent).toContain('Loading releases');

    response$.next({items: [releaseRow()], nextCursor: 'cursor-2'});
    response$.complete();
    await fixture.whenStable();
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('caption').textContent).toContain('Release history');
    expect(fixture.nativeElement.textContent).toContain('2026.7.1');
    expect(fixture.nativeElement.textContent).toContain('Load more');
  });

  it('deduplicates overlapping release pages', async () => {
    service.listReleases
      .mockReturnValueOnce(of({items: [releaseRow()], nextCursor: 'cursor-2'}))
      .mockReturnValueOnce(of({items: [releaseRow(), releaseRow({id: 'release-2', version: '2026.7.2'})]}));
    const {component, fixture} = await createListComponent();

    await (component as any).loadMore();
    fixture.detectChanges();

    expect((component as any).releases().map((release: OperatorReleaseRow) => release.id)).toEqual([
      'release-1',
      'release-2',
    ]);
    expect(service.listReleases).toHaveBeenCalledWith({cursor: 'cursor-2', limit: 50});
  });

  it('applies scalable customer, application, deployment-unit, kind, status, and search filters to every page', async () => {
    service.listReleases.mockReturnValue(of({items: [releaseRow()], nextCursor: 'cursor-2'}));
    const {component} = await createListComponent();

    const controls = (component as any).filterForm.controls;
    controls.customerOrganizationId.setValue('customer-1');
    controls.applicationId.setValue('application-1');
    controls.deploymentUnitId.setValue('unit-1');
    controls.kind.setValue('product');
    controls.status.setValue('PUBLISHED');
    controls.search.setValue('payments');
    await (component as any).applyFilters();
    await (component as any).loadMore();

    const filters = {
      customerOrganizationId: 'customer-1',
      applicationId: 'application-1',
      deploymentUnitId: 'unit-1',
      kind: 'product',
      status: 'PUBLISHED',
      search: 'payments',
      limit: 50,
    };
    expect(service.listReleases.mock.calls[1][0]).toEqual(filters);
    expect(service.listReleases.mock.calls.at(-1)?.[0]).toEqual({...filters, cursor: 'cursor-2'});
  });

  it('uses releaseId route changes to render immutable release detail and evidence', async () => {
    service.getRelease.mockImplementation((id: string) =>
      of(releaseDetailResponse({id, version: id === 'release-2' ? '2026.7.2' : '2026.7.1'}))
    );
    service.getReleaseEvidence.mockReturnValue(of(evidencePage()));
    params$.next(convertToParamMap({releaseId: 'release-1'}));
    const fixture = TestBed.createComponent(ReleasesComponent);
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();

    expect(fixture.nativeElement.textContent).toContain('Release detail');
    expect(fixture.nativeElement.textContent).toContain('payments-api');
    expect(fixture.nativeElement.textContent).toContain('linux/amd64');
    expect(fixture.nativeElement.textContent).toContain('requires');
    expect(fixture.nativeElement.textContent).toContain('Payments application');
    expect(fixture.nativeElement.textContent).toContain('Acme Thailand');
    expect(fixture.nativeElement.textContent).toContain('ci-provider');
    expect(fixture.nativeElement.textContent).toContain('build-17');
    expect(fixture.nativeElement.textContent).toContain('Payments retry behavior');
    expect(fixture.nativeElement.textContent).toContain('Skipped release 2026.6.9');
    expect(fixture.nativeElement.textContent).toContain('shared_provider');
    expect(fixture.nativeElement.textContent).toContain('sha256:database-provider');
    expect(fixture.nativeElement.textContent).toContain('provider_deploy_and_health_before_consumer');
    expect(fixture.nativeElement.textContent).toContain('Signed build provenance');

    params$.next(convertToParamMap({releaseId: 'release-2'}));
    await fixture.whenStable();
    fixture.detectChanges();

    expect(service.getRelease).toHaveBeenCalledWith('release-2');
    expect(service.getReleaseEvidence).toHaveBeenCalledWith('release-2');
    expect(fixture.nativeElement.textContent).toContain('2026.7.2');
  });

  it('renders an immutable HTTPS SBOM reference when no digest was derived', async () => {
    const response = releaseDetailResponse();
    response.detail.sourceBuildProof[0].sbomReference =
      'https://evidence.example.invalid/payments/sha256/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/sbom.spdx.json';
    response.detail.sourceBuildProof[0].sbomDigest = '';
    service.getRelease.mockReturnValue(of(response));
    service.getReleaseEvidence.mockReturnValue(of(evidencePage()));
    params$.next(convertToParamMap({releaseId: 'release-1'}));

    const {fixture} = await createCurrentComponent();

    expect(fixture.nativeElement.textContent).toContain('SBOM');
    expect(fixture.nativeElement.textContent).toContain(
      'https://evidence.example.invalid/payments/sha256/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/sbom.spdx.json'
    );
  });

  it('compares the routed release with another release and renders checksum facts', async () => {
    service.getRelease.mockReturnValue(of(releaseDetailResponse()));
    service.getReleaseEvidence.mockReturnValue(of(evidencePage()));
    service.compareReleases.mockReturnValue(
      of({
        comparison: {
          left: releaseRow(),
          right: releaseRow({id: 'release-2', version: '2026.7.2'}),
          changes: [
            {
              component: 'payments-api',
              change: 'UPDATED',
              leftChecksum: 'sha256:left',
              rightChecksum: 'sha256:right',
            },
          ],
        },
      } satisfies OperatorReleaseCompareResponse)
    );
    params$.next(convertToParamMap({releaseId: 'release-1'}));
    const {component, fixture} = await createCurrentComponent();

    (component as any).compareReleaseId.setValue('release-2');
    await (component as any).compare();
    fixture.detectChanges();

    expect(service.compareReleases).toHaveBeenCalledWith('release-1', 'release-2');
    const comparison = fixture.nativeElement.querySelector('[aria-labelledby="release-comparison-heading"]');
    expect(comparison.textContent).toContain('Release comparison');
    expect(comparison.textContent).toContain('payments-api');
    expect(comparison.textContent).toContain('sha256:left');
    expect(comparison.textContent).toContain('sha256:right');
  });

  it('retains release detail and announces partial evidence when the evidence collection fails', async () => {
    service.getRelease.mockReturnValue(of(releaseDetailResponse()));
    service.getReleaseEvidence.mockReturnValue(throwError(() => ({status: 503, message: 'unsafe evidence error'})));
    params$.next(convertToParamMap({releaseId: 'release-1'}));

    const {fixture} = await createCurrentComponent();

    expect(fixture.nativeElement.textContent).toContain('Release detail');
    const status = fixture.nativeElement.querySelector('[role="status"]');
    expect(status.textContent).toContain('Partial evidence');
    expect(status.textContent).not.toContain('unsafe evidence error');
  });

  it('creates, validates, confirms, and publishes a component release through release-bundles v2', async () => {
    service.listReleases.mockReturnValue(of({items: []}));
    service.createComponentRelease.mockReturnValue(of(componentRelease()));
    service.validateComponentRelease.mockReturnValue(of({valid: true, errors: [], warnings: []}));
    service.publishComponentRelease.mockReturnValue(of({...componentRelease(), status: 'PUBLISHED'}));
    service.getRelease.mockReturnValue(of(releaseDetailResponse({id: 'component-release-1'})));
    service.getReleaseEvidence.mockReturnValue(of(evidencePage()));
    const {component, fixture} = await createListComponent();
    const request = componentReleaseRequest();

    (component as any).componentReleaseRequest.setValue(JSON.stringify(request));
    await (component as any).createComponentRelease();
    await (component as any).validateComponentRelease();
    await (component as any).publishComponentRelease();
    fixture.detectChanges();

    expect(service.createComponentRelease).toHaveBeenCalledWith(request);
    expect(service.validateComponentRelease).toHaveBeenCalledWith('component-release-1');
    expect(overlay.confirm).toHaveBeenCalledWith({
      message: {message: 'Publishing component release component-release-1 makes it immutable.'},
      requiredConfirmInputText: 'component-release-1',
    });
    expect(service.publishComponentRelease).toHaveBeenCalledWith('component-release-1');
    expect(router.navigate).toHaveBeenCalledWith(['/releases', 'component-release-1']);
    expect(service.getRelease).toHaveBeenCalledWith('component-release-1');
    expect(fixture.nativeElement.textContent).toContain('Published component release');
  });

  it('assembles, validates, confirms, and publishes a product release from immutable component pins', async () => {
    service.listReleases.mockReturnValue(of({items: []}));
    service.createProductRelease.mockReturnValue(of(productRelease()));
    service.validateProductRelease.mockReturnValue(of({valid: true, issues: []}));
    service.publishProductRelease.mockReturnValue(of({...productRelease(), status: 'PUBLISHED'}));
    service.getRelease.mockReturnValue(of(releaseDetailResponse({id: 'product-release-1', kind: 'PRODUCT'})));
    service.getReleaseEvidence.mockReturnValue(of(evidencePage()));
    const {component, fixture} = await createListComponent();
    const request = productReleaseRequest();

    (component as any).productReleaseRequest.setValue(JSON.stringify(request));
    await (component as any).createProductRelease();
    await (component as any).validateProductRelease();
    await (component as any).publishProductRelease();
    fixture.detectChanges();

    expect(service.createProductRelease).toHaveBeenCalledWith(request);
    expect(service.validateProductRelease).toHaveBeenCalledWith('product-release-1');
    expect(overlay.confirm).toHaveBeenCalledWith({
      message: {message: 'Publishing product release product-release-1 makes it immutable.'},
      requiredConfirmInputText: 'product-release-1',
    });
    expect(service.publishProductRelease).toHaveBeenCalledWith('product-release-1');
    expect(router.navigate).toHaveBeenCalledWith(['/releases', 'product-release-1']);
    expect(service.getRelease).toHaveBeenCalledWith('product-release-1');
    expect(fixture.nativeElement.textContent).toContain('Published product release');
  });

  it('does not publish when typed confirmation is cancelled', async () => {
    service.listReleases.mockReturnValue(of({items: []}));
    service.createProductRelease.mockReturnValue(of(productRelease()));
    service.validateProductRelease.mockReturnValue(of({valid: true, issues: []}));
    overlay.confirm.mockReturnValue(of(false));
    const {component} = await createListComponent();

    (component as any).productReleaseRequest.setValue(JSON.stringify(productReleaseRequest()));
    await (component as any).createProductRelease();
    await (component as any).validateProductRelease();
    await (component as any).publishProductRelease();

    expect(service.publishProductRelease).not.toHaveBeenCalled();
    expect(router.navigate).not.toHaveBeenCalled();
  });

  it('submits component assembly through the rendered reactive form without native navigation', async () => {
    service.listReleases.mockReturnValue(of({items: []}));
    service.createComponentRelease.mockReturnValue(of(componentRelease()));
    const {component, fixture} = await createListComponent();
    const request = componentReleaseRequest();
    (component as any).componentReleaseRequest.setValue(JSON.stringify(request));
    fixture.detectChanges();

    const submit = new Event('submit', {bubbles: true, cancelable: true});
    fixture.nativeElement.querySelectorAll('form')[0].dispatchEvent(submit);
    await fixture.whenStable();

    expect(submit.defaultPrevented).toBe(true);
    expect(service.createComponentRelease).toHaveBeenCalledWith(request);
  });

  it('submits product assembly through the rendered reactive form without native navigation', async () => {
    service.listReleases.mockReturnValue(of({items: []}));
    service.createProductRelease.mockReturnValue(of(productRelease()));
    const {component, fixture} = await createListComponent();
    const request = productReleaseRequest();
    (component as any).productReleaseRequest.setValue(JSON.stringify(request));
    fixture.detectChanges();

    const submit = new Event('submit', {bubbles: true, cancelable: true});
    fixture.nativeElement.querySelectorAll('form')[1].dispatchEvent(submit);
    await fixture.whenStable();

    expect(submit.defaultPrevented).toBe(true);
    expect(service.createProductRelease).toHaveBeenCalledWith(request);
  });

  it('submits comparison through the rendered reactive form without native navigation', async () => {
    service.getRelease.mockReturnValue(of(releaseDetailResponse()));
    service.getReleaseEvidence.mockReturnValue(of(evidencePage()));
    service.compareReleases.mockReturnValue(
      of({
        comparison: {
          left: releaseRow(),
          right: releaseRow({id: 'release-2', version: '2026.7.2'}),
          changes: [],
        },
      })
    );
    params$.next(convertToParamMap({releaseId: 'release-1'}));
    const {component, fixture} = await createCurrentComponent();
    (component as any).compareReleaseId.setValue('release-2');
    fixture.detectChanges();

    const submit = new Event('submit', {bubbles: true, cancelable: true});
    fixture.nativeElement.querySelector('form').dispatchEvent(submit);
    await fixture.whenStable();

    expect(submit.defaultPrevented).toBe(true);
    expect(service.compareReleases).toHaveBeenCalledWith('release-1', 'release-2');
  });

  it('renders empty, partial, stale, and unknown release states explicitly', async () => {
    service.listReleases.mockReturnValue(
      of({
        items: [
          releaseRow({id: 'partial', evidenceCount: 2, checksum: ''}),
          releaseRow({id: 'stale', status: 'STALE'}),
          releaseRow({id: 'unknown', status: 'UNKNOWN'}),
        ],
      })
    );
    let created = await createListComponent();

    expect(created.fixture.nativeElement.textContent).toContain('Partial evidence');
    expect(created.fixture.nativeElement.textContent).toContain('Stale evidence');
    expect(created.fixture.nativeElement.textContent).toContain('Unknown evidence');

    TestBed.resetTestingModule();
    params$ = new BehaviorSubject(convertToParamMap({}));
    service = {
      listReleases: vi.fn().mockReturnValue(of({items: []})),
      getRelease: vi.fn(),
      compareReleases: vi.fn(),
      getReleaseEvidence: vi.fn(),
      createComponentRelease: vi.fn(),
      validateComponentRelease: vi.fn(),
      publishComponentRelease: vi.fn(),
      createProductRelease: vi.fn(),
      validateProductRelease: vi.fn(),
      publishProductRelease: vi.fn(),
    };
    TestBed.configureTestingModule({
      imports: [ReleasesComponent],
      providers: [
        {provide: OperatorControlPlaneService, useValue: service},
        {provide: ActivatedRoute, useValue: {paramMap: params$.asObservable()}},
        {provide: OverlayService, useValue: overlay},
        {provide: Router, useValue: router},
      ],
    });
    created = await createListComponent();

    expect(created.fixture.nativeElement.textContent).toContain('No releases match the current scope');
  });

  for (const errorCase of [
    {status: 403, detail: false, message: 'You are not authorized to view operator releases.'},
    {status: 404, detail: false, message: 'The operator control plane is disabled for this organization.'},
    {status: 500, detail: false, message: 'Releases could not be loaded. Try again.'},
    {status: 404, detail: true, message: 'This release was not found or is outside your scope.'},
  ]) {
    it(`renders the safe ${errorCase.status} ${errorCase.detail ? 'detail' : 'list'} error as an alert`, async () => {
      if (errorCase.detail) {
        params$.next(convertToParamMap({releaseId: 'missing'}));
        service.getRelease.mockReturnValue(throwError(() => ({status: errorCase.status, message: 'unsafe detail'})));
        service.getReleaseEvidence.mockReturnValue(of({items: []}));
      } else {
        service.listReleases.mockReturnValue(
          throwError(() => ({status: errorCase.status, message: 'unsafe provider detail'}))
        );
      }

      const {fixture} = await createCurrentComponent();

      const alert = fixture.nativeElement.querySelector('[role="alert"]');
      expect(alert.textContent).toContain(errorCase.message);
      expect(alert.textContent).not.toContain('unsafe');
    });
  }

  async function createListComponent(): Promise<{
    component: ReleasesComponent;
    fixture: ComponentFixture<ReleasesComponent>;
  }> {
    params$.next(convertToParamMap({}));
    return createCurrentComponent();
  }

  async function createCurrentComponent(): Promise<{
    component: ReleasesComponent;
    fixture: ComponentFixture<ReleasesComponent>;
  }> {
    const fixture = TestBed.createComponent(ReleasesComponent);
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();
    return {component: fixture.componentInstance, fixture};
  }
});

function releaseRow(overrides: Partial<OperatorReleaseRow> = {}): OperatorReleaseRow {
  return {
    id: 'release-1',
    createdAt: '2026-07-28T01:00:00Z',
    kind: 'PRODUCT',
    applicationId: 'application-1',
    application: 'Payments application',
    clients: [{id: 'customer-1', name: 'Acme Thailand'}],
    releaseNumber: 17,
    version: '2026.7.1',
    status: 'PUBLISHED',
    checksum: 'sha256:release',
    sourceRevision: '0123456789abcdef',
    publishedAt: '2026-07-28T02:00:00Z',
    artifactCount: 1,
    evidenceCount: 1,
    componentCount: 1,
    graphEdgeCount: 1,
    ...overrides,
  };
}

function releaseDetailResponse(releaseOverrides: Partial<OperatorReleaseRow> = {}): OperatorReleaseDetailResponse {
  return {
    detail: {
      release: releaseRow(releaseOverrides),
      artifacts: [
        {
          id: 'artifact-1',
          name: 'payments-api',
          version: '2026.7.1',
          manifestDigest: 'sha256:manifest',
          platformDigests: {'linux/amd64': 'sha256:amd64', 'linux/arm64': 'sha256:arm64'},
        },
      ],
      componentPins: [
        {
          componentReleaseId: 'component-release-1',
          component: 'payments-api',
          version: '2026.7.1',
          checksum: 'sha256:component',
          digest: 'sha256:manifest',
        },
      ],
      graphEdges: [
        {
          from: 'payments-api',
          to: 'database',
          kind: 'requires',
          consumerComponent: 'payments-api',
          providerComponent: 'database',
          capability: 'database.write',
          versionRange: '>=4.0.0',
          providerVersion: '4.2.0',
          providerArtifacts: [
            {
              artifactKey: 'database',
              artifactType: 'oci-image',
              manifestDigest: 'sha256:database-manifest',
              platform: 'linux/amd64',
              platformDigest: 'sha256:database-provider',
            },
          ],
          resolutionStage: 'product',
          allowedModes: ['shared_provider'],
          ordering: 'provider_deploy_and_health_before_consumer',
        },
      ],
      sourceBuildProof: [
        {
          component: 'payments-api',
          schema: 'distr.component-release/v2',
          declaredRepository: 'https://example.invalid/payments.git',
          declaredRequestedRef: 'refs/tags/2026.7.1',
          declaredSourceCommit: '0123456789abcdef',
          declaredBuilderId: 'declared-ci',
          declaredBuildId: 'declared-build-17',
          verifiedSourceUri: 'https://example.invalid/payments.git',
          verifiedSourceCommit: 'fedcba9876543210fedcba9876543210fedcba98',
          verifiedBuilderId: 'ci-provider',
          verifiedBuildId: 'build-17',
          verifiedBuildType: 'https://slsa.dev/provenance/v1',
          provenanceReference: 'oci://evidence/provenance',
          provenanceDigest: 'sha256:provenance',
          sbomReference: 'oci://evidence/sbom@sha256:sbom',
          sbomDigest: 'sha256:sbom',
          verificationState: 'VERIFIED',
        },
      ],
      changelog: [
        {category: 'code', component: 'payments-api', summary: 'Payments retry behavior'},
        {category: 'config', component: 'payments-api', summary: 'Increase retry window'},
        {category: 'migration', component: 'payments-api', summary: 'Add retry index', reference: 'retry-index'},
        {
          category: 'dependency',
          component: 'payments-api',
          summary: 'database.write >=4.0.0',
          reference: 'product',
        },
      ],
      skippedReleases: [
        {
          component: 'payments-api',
          releaseId: 'release-skipped',
          version: '2026.6.9',
          sourceRevision: 'abcdef0123456789',
          summary: 'Skipped release 2026.6.9',
        },
      ],
      changeContext: {
        deploymentPlanId: 'plan-1',
        deploymentUnitId: 'unit-1',
        state: 'READY',
      },
      evidence: evidencePage().items,
    },
  };
}

function evidencePage(): OperatorEvidencePage {
  return {
    items: [
      {
        id: 'evidence-1',
        kind: 'PROVENANCE',
        label: 'Signed build provenance',
        href: '/api/v1/evidence/evidence-1',
        checksum: 'sha256:evidence',
        createdAt: '2026-07-28T02:00:00Z',
      },
    ],
  };
}

function componentReleaseRequest() {
  return {
    applicationId: 'application-1',
    channelId: 'channel-1',
    releaseNumber: '2026.7.1',
    releaseNotes: 'Payments component',
    sourceRevision: '0123456789abcdef',
    releaseContract: {
      schema: 'distr.component-release/v2' as const,
      componentKey: 'payments-api',
      version: '2026.7.1',
      source: {
        repository: 'https://example.invalid/payments.git',
        requestedRef: 'refs/tags/2026.7.1',
        commit: '0123456789abcdef',
      },
      build: {id: 'build-17', builder: 'ci-provider'},
      artifacts: [
        {
          key: 'payments-api',
          type: 'oci-image' as const,
          mediaType: 'application/vnd.oci.image.manifest.v1+json',
          digest: 'sha256:manifest',
          platforms: [{platform: 'linux/amd64' as const, digest: 'sha256:amd64'}],
        },
      ],
      provides: [{name: 'payments-api', version: '2026.7.1'}],
      requires: [],
      migrations: [],
      changes: {summary: 'Payments release', commits: ['0123456789abcdef']},
      evidence: {
        provenance: ['evidence/provenance.json'],
        sbom: ['evidence/sbom.json'],
        signatures: ['evidence/signature.json'],
        tests: ['evidence/tests.json'],
      },
    },
    components: [],
  };
}

function componentRelease() {
  return {
    id: 'component-release-1',
    createdAt: '2026-07-28T01:00:00Z',
    updatedAt: '2026-07-28T01:00:00Z',
    applicationId: 'application-1',
    channelId: 'channel-1',
    releaseNumber: '2026.7.1',
    releaseNotes: 'Payments component',
    sourceRevision: '0123456789abcdef',
    releaseContract: componentReleaseRequest().releaseContract,
    kind: 'component' as const,
    releaseContractSchema: 'distr.component-release/v2' as const,
    status: 'DRAFT' as const,
    canonicalChecksum: 'sha256:component-release',
    components: [],
  };
}

function productReleaseRequest() {
  return {
    schema: 'distr.product-release/v1',
    applicationId: 'application-1',
    channelId: 'channel-1',
    product: 'payments',
    version: '2026.7.1',
    dependencyPolicyVersion: 'policy-1',
    releaseNotes: 'Payments product release',
    requiredPlatforms: ['linux/amd64'],
    components: [
      {
        componentReleaseId: 'component-release-1',
        componentReleaseChecksum: 'sha256:component-release',
      },
    ],
    requirements: [],
  };
}

function productRelease() {
  return {
    id: 'product-release-1',
    createdAt: '2026-07-28T01:00:00Z',
    updatedAt: '2026-07-28T01:00:00Z',
    applicationId: 'application-1',
    channelId: 'channel-1',
    status: 'DRAFT',
    canonicalChecksum: 'sha256:product-release',
    graphChecksum: 'sha256:graph',
    manifest: productReleaseRequest(),
  };
}
