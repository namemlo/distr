import {HttpErrorResponse} from '@angular/common/http';
import {ComponentFixture, TestBed} from '@angular/core/testing';
import {of, Subject, throwError} from 'rxjs';
import {vi} from 'vitest';
import {AuthService} from '../../services/auth.service';
import {DeploymentRegistryService} from '../../services/deployment-registry.service';
import {FeatureFlagService} from '../../services/feature-flag.service';
import {
  RegistryCoverage,
  RegistryImport,
  RegistryImportRequest,
  RegistryImportResult,
  RegistryImportRoot,
} from '../../types/deployment-registry';
import {DeploymentRegistryComponent} from './deployment-registry.component';

const evidenceChecksum = '0'.repeat(64);
const previewChecksum = `sha256:${'1'.repeat(64)}`;
const preview: RegistryImport = {
  id: 'import-1',
  previewChecksum,
  counts: {
    discoveredRoots: 26,
    classifiedRoots: 19,
    discoveredPlacements: 28,
    omittedPlacements: 0,
    creates: 26,
    updates: 0,
    retirements: 0,
    conflicts: 0,
  },
  diagnostics: [
    {
      code: 'classification_required',
      field: 'roots[19].classification',
      message: 'Root requires an operator decision.',
    },
  ],
  diagnosticsTruncated: false,
  omissions: [],
  roots: scaleRoots(),
  diff: {
    creates: Array.from({length: 26}, (_, index) => ({
      kind: 'root',
      rootKey: `root-${index + 1}`,
      message: `Create deployment root root-${index + 1}.`,
    })),
    updates: [],
    retirements: [],
    conflicts: [],
  },
};

describe('DeploymentRegistryComponent', () => {
  let service: {
    preview: ReturnType<typeof vi.fn>;
    saveDecision: ReturnType<typeof vi.fn>;
    get: ReturnType<typeof vi.fn>;
    apply: ReturnType<typeof vi.fn>;
    coverage: ReturnType<typeof vi.fn>;
  };
  let role: 'read_only' | 'read_write' | 'admin';
  let superAdmin: boolean;
  let vendor: boolean;
  let auth: {
    isVendor: ReturnType<typeof vi.fn>;
    isSuperAdmin: ReturnType<typeof vi.fn>;
    hasAnyRole: ReturnType<typeof vi.fn>;
  };
  let featureFlags: {
    isExperimentalFeatureEnabled$: ReturnType<typeof vi.fn>;
  };

  beforeEach(() => {
    role = 'admin';
    superAdmin = false;
    vendor = true;
    auth = {
      isVendor: vi.fn(() => vendor),
      isSuperAdmin: vi.fn(() => superAdmin),
      hasAnyRole: vi.fn((...roles: string[]) => roles.includes(role)),
    };
    featureFlags = {
      isExperimentalFeatureEnabled$: vi.fn().mockReturnValue(of(true)),
    };
    service = {
      preview: vi.fn().mockReturnValue(of(preview)),
      saveDecision: vi.fn().mockReturnValue(of(void 0)),
      get: vi.fn().mockReturnValue(of(readyPreview())),
      apply: vi.fn().mockReturnValue(of(appliedResult())),
      coverage: vi.fn().mockReturnValue(of(partialCoverage())),
    };
    TestBed.configureTestingModule({
      imports: [DeploymentRegistryComponent],
      providers: [
        {provide: AuthService, useValue: auth},
        {provide: DeploymentRegistryService, useValue: service},
        {provide: FeatureFlagService, useValue: featureFlags},
      ],
    });
  });

  it('does not request import-scoped coverage before a preview exists', async () => {
    const {component} = await createComponent();

    expect(service.coverage).not.toHaveBeenCalled();
    expect((component as any).coverageState()).toBe('empty');
  });

  it('submits the exact 26-root request and shows 19 decisions across 28 services', async () => {
    const {component} = await createComponent();
    const request = scaleRequest();
    (component as any).previewForm.setValue(previewFormValue(request));

    await (component as any).createPreview();

    expect(service.preview).toHaveBeenCalledWith(request);
    expect(service.coverage).toHaveBeenCalledWith('import-1');
    expect((component as any).decisionCount(preview)).toBe(19);
    expect((component as any).coverage().services).toBe(28);
    expect((component as any).coverage().omissions).toEqual([
      'root-20',
      'root-21',
      'root-22',
      'root-23',
      'root-24',
      'root-25',
      'root-26',
    ]);
    expect((component as any).coverageState()).toBe('partial');
    expect(JSON.stringify(preview)).not.toMatch(/https?:\/\/|secret/i);
  });

  it('persists a classification and enables apply only after unresolved roots are resolved', async () => {
    const {component} = await createComponent();
    const request = scaleRequest();
    (component as any).previewForm.setValue(previewFormValue(request));
    service.coverage
      .mockReturnValueOnce(of(partialCoverage()))
      .mockReturnValueOnce(of(completeCoverage()))
      .mockReturnValueOnce(of(completeCoverage()));

    await (component as any).createPreview();
    expect((component as any).canApply()).toBe(false);

    await (component as any).saveClassification(preview.roots[19], 'shared');

    expect(service.saveDecision).toHaveBeenCalledWith('import-1', {
      rootKey: 'root-20',
      classification: 'shared',
    });
    expect(service.get).toHaveBeenCalledWith('import-1');
    expect(service.coverage.mock.calls).toEqual([['import-1'], ['import-1']]);
    expect((component as any).registryImport().counts.classifiedRoots).toBe(19);
    expect((component as any).coverageState()).toBe('ready');
    expect((component as any).canApply()).toBe(false);

    (component as any).applyConfirmed.setValue(true);
    expect((component as any).canApply()).toBe(true);

    await (component as any).apply();

    expect(service.apply).toHaveBeenCalledWith('import-1', previewChecksum);
    expect((component as any).applied()).toBe(true);
  });

  it('shows loading while import-scoped coverage is pending', async () => {
    const coverage = new Subject<RegistryCoverage>();
    service.coverage.mockReturnValue(coverage);
    const {component} = await createComponent();
    (component as any).previewForm.setValue(previewFormValue(scaleRequest()));

    const operation = (component as any).createPreview();
    await Promise.resolve();

    expect(service.coverage).toHaveBeenCalledWith('import-1');
    expect((component as any).coverageState()).toBe('loading');

    coverage.next(partialCoverage());
    coverage.complete();
    await operation;
    expect((component as any).coverageState()).toBe('partial');
  });

  it('refreshes coverage for the same import after a successful apply', async () => {
    service.coverage.mockReturnValue(of(completeCoverage()));
    const {component} = await createComponent();
    (component as any).registryImport.set(readyPreview());
    (component as any).coverage.set(completeCoverage());
    (component as any).applyConfirmed.setValue(true);

    await (component as any).apply();

    expect(service.apply).toHaveBeenCalledWith('import-1', previewChecksum);
    expect(service.coverage).toHaveBeenCalledWith('import-1');
    expect((component as any).applied()).toBe(true);
    expect((component as any).coverageState()).toBe('ready');
  });

  it('sends the preview checksum and presents stale apply conflicts as refreshable state', async () => {
    const {component} = await createComponent();
    (component as any).registryImport.set(readyPreview());
    (component as any).coverage.set(completeCoverage());
    (component as any).applyConfirmed.setValue(true);
    service.apply.mockReturnValue(throwError(() => new HttpErrorResponse({status: 409, error: 'preview is stale'})));

    await (component as any).apply();

    expect(service.apply).toHaveBeenCalledWith('import-1', previewChecksum);
    expect(service.coverage).not.toHaveBeenCalled();
    expect((component as any).stale()).toBe(true);
    expect((component as any).error()).toContain('stale');
  });

  it('keeps an import-scoped coverage failure distinct from an empty registry', async () => {
    service.coverage.mockReturnValue(throwError(() => new Error('coverage unavailable')));
    const {component} = await createComponent();
    (component as any).previewForm.setValue(previewFormValue(scaleRequest()));

    await (component as any).createPreview();

    expect(service.coverage).toHaveBeenCalledWith('import-1');
    expect((component as any).coverageState()).toBe('error');
    expect((component as any).coverage()).toBeUndefined();
    expect((component as any).canApply()).toBe(false);
  });

  it('blocks apply for absent, loading, errored, partial, mismatched, omitted, unresolved, or conflicting coverage', async () => {
    const {component} = await createComponent();
    (component as any).registryImport.set(readyPreview());
    (component as any).applyConfirmed.setValue(true);

    expect((component as any).canApply()).toBe(false);

    (component as any).coverage.set(completeCoverage());
    (component as any).loadingCoverage.set(true);
    expect((component as any).canApply()).toBe(false);

    (component as any).loadingCoverage.set(false);
    (component as any).coverageError.set(true);
    expect((component as any).canApply()).toBe(false);

    (component as any).coverageError.set(false);
    (component as any).coverage.set(partialCoverage());
    expect((component as any).canApply()).toBe(false);

    (component as any).coverage.set({...completeCoverage(), importId: 'stale-import'});
    expect((component as any).canApply()).toBe(false);

    (component as any).coverage.set({...completeCoverage(), omittedPlacements: 1});
    expect((component as any).canApply()).toBe(false);

    (component as any).coverage.set({...completeCoverage(), omissions: ['root-26: service missing']});
    expect((component as any).canApply()).toBe(false);

    (component as any).coverage.set({...completeCoverage(), unresolvedRoots: 1});
    expect((component as any).canApply()).toBe(false);

    (component as any).coverage.set(completeCoverage());
    (component as any).registryImport.set({
      ...readyPreview(),
      counts: {...readyPreview().counts, conflicts: 1},
      diff: {
        ...readyPreview().diff,
        conflicts: [
          {
            kind: 'placement',
            rootKey: 'root-1',
            placementKey: 'component-1-1',
            physicalName: 'service-1-1',
            message: 'Physical identity conflicts with an active placement.',
          },
        ],
      },
    });
    expect((component as any).canApply()).toBe(false);
  });

  it('requires acknowledgement after exact complete coverage and all decisions are present', async () => {
    const {component} = await createComponent();
    (component as any).registryImport.set(readyPreview());
    (component as any).coverage.set(completeCoverage());

    expect((component as any).canApply()).toBe(false);

    (component as any).applyConfirmed.setValue(true);
    expect((component as any).canApply()).toBe(true);
  });

  it('rejects mismatched coverage returned for the active import', async () => {
    service.coverage.mockReturnValue(of({...completeCoverage(), importId: 'import-2'}));
    const {component} = await createComponent();
    (component as any).previewForm.setValue(previewFormValue(scaleRequest()));

    await (component as any).createPreview();

    expect((component as any).coverage()).toBeUndefined();
    expect((component as any).coverageState()).toBe('error');
    expect((component as any).canApply()).toBe(false);
  });

  it('discards stale async coverage without clearing the active request state', async () => {
    const firstCoverage = new Subject<RegistryCoverage>();
    const secondCoverage = new Subject<RegistryCoverage>();
    const secondPreview: RegistryImport = {
      ...readyPreview(),
      id: 'import-2',
      previewChecksum: `sha256:${'2'.repeat(64)}`,
    };
    service.preview.mockReturnValueOnce(of(preview)).mockReturnValueOnce(of(secondPreview));
    service.coverage.mockImplementation((importId: string) =>
      importId === 'import-1' ? firstCoverage : secondCoverage
    );
    const {component} = await createComponent();
    (component as any).previewForm.setValue(previewFormValue(scaleRequest()));

    const firstOperation = (component as any).createPreview();
    await Promise.resolve();
    const secondOperation = (component as any).createPreview();
    await Promise.resolve();

    secondCoverage.next({...completeCoverage(), importId: 'import-2'});
    secondCoverage.complete();
    await secondOperation;
    firstCoverage.next(completeCoverage());
    firstCoverage.complete();
    await firstOperation;

    expect((component as any).registryImport().id).toBe('import-2');
    expect((component as any).coverage().importId).toBe('import-2');
    expect((component as any).coverageState()).toBe('ready');
  });

  it('renders complete bounded preview evidence before the apply attestation', async () => {
    const {fixture, component} = await createComponent();
    const evidencePreview: RegistryImport = {
      ...readyPreview(),
      roots: [
        {
          ...readyPreview().roots[0],
          placements: [
            {
              ...readyPreview().roots[0].placements[0],
              renamedFrom: 'service-1-legacy',
            },
          ],
        },
      ],
      diagnostics: [
        {
          code: 'topology_conflict',
          field: 'roots[0].placements[0]',
          message: 'Placement requires an explicit topology decision.',
        },
      ],
      diagnosticsTruncated: true,
      omissions: ['root-1/preview-omitted: source placement was not discovered'],
      diff: {
        creates: [
          {
            kind: 'root',
            rootKey: 'root-1',
            physicalName: 'service-1-1',
            message: 'Create root one.',
          },
        ],
        updates: [
          {
            kind: 'placement',
            rootKey: 'root-1',
            placementKey: 'component-1-1',
            physicalName: 'service-1-1',
            message: 'Update placement metadata.',
          },
        ],
        retirements: [
          {
            kind: 'placement',
            rootKey: 'root-1',
            placementKey: 'legacy-component',
            physicalName: 'legacy-service',
            message: 'Retire legacy placement.',
          },
        ],
        conflicts: [
          {
            kind: 'placement',
            rootKey: 'root-1',
            placementKey: 'conflicting-component',
            physicalName: 'conflicting-service',
            message: 'Physical identity conflicts with an active placement.',
          },
        ],
      },
    };
    (component as any).registryImport.set(evidencePreview);
    (component as any).coverage.set({
      ...partialCoverage(),
      omissions: ['root-1/component-omitted: missing physical identity'],
    });
    fixture.detectChanges();

    const text = fixture.nativeElement.textContent as string;
    const attestationOffset = text.indexOf('I have reviewed the classifications and preview changes.');
    for (const expected of [
      'Root order 1',
      '11111111-1111-1111-1111-111111111111',
      '22222222-2222-2222-2222-000000000001',
      '33333333-3333-3333-3333-000000000001',
      '44444444-4444-4444-4444-444444444444',
      'identity-1',
      'Placement order 1',
      'component-1-1',
      'service-1-1',
      'root-1',
      'database-1',
      'http',
      'service-1-legacy',
      'Create root one.',
      'Update placement metadata.',
      'Retire legacy placement.',
      'Physical identity conflicts with an active placement.',
      'topology_conflict',
      'roots[0].placements[0]',
      'Placement requires an explicit topology decision.',
      'Diagnostics truncated',
      'root-1/preview-omitted: source placement was not discovered',
      'root-1/component-omitted: missing physical identity',
    ]) {
      expect(text).toContain(expected);
      expect(text.indexOf(expected)).toBeLessThan(attestationOffset);
    }
  });

  for (const {name, nextRole, nextSuperAdmin} of [
    {name: 'vendor read only', nextRole: 'read_only' as const, nextSuperAdmin: false},
    {name: 'super admin', nextRole: 'admin' as const, nextSuperAdmin: true},
  ]) {
    it(`defensively blocks ${name} mutations when the component is constructed directly`, async () => {
      role = nextRole;
      superAdmin = nextSuperAdmin;
      const {component} = await createComponent();
      (component as any).previewForm.setValue(previewFormValue(scaleRequest()));
      (component as any).registryImport.set(readyPreview());
      (component as any).coverage.set(completeCoverage());
      (component as any).applyConfirmed.setValue(true);

      await (component as any).createPreview();
      await (component as any).saveClassification(readyPreview().roots[0], 'shared');
      await (component as any).apply();

      expect(service.preview).not.toHaveBeenCalled();
      expect(service.saveDecision).not.toHaveBeenCalled();
      expect(service.apply).not.toHaveBeenCalled();
    });
  }

  async function createComponent(): Promise<{
    fixture: ComponentFixture<DeploymentRegistryComponent>;
    component: DeploymentRegistryComponent;
  }> {
    const fixture = TestBed.createComponent(DeploymentRegistryComponent);
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();
    return {fixture, component: fixture.componentInstance};
  }
});

function scaleRequest(): RegistryImportRequest {
  return {
    sourceKind: 'inventory_report',
    toolName: 'registry-audit',
    toolVersion: '1.0',
    sourceCommit: 'a'.repeat(40),
    parameters: {scope: 'all', format: 'normalized-v1'},
    evidenceReference: `evidence://sha256/${evidenceChecksum}`,
    evidenceChecksum,
    roots: scaleRoots(),
    sourcePlacements: scaleRoots().flatMap((root) =>
      root.placements.map((placement) => ({rootKey: root.key, physicalName: placement.physicalName}))
    ),
  };
}

function previewFormValue(request: RegistryImportRequest) {
  return {
    sourceKind: request.sourceKind,
    toolName: request.toolName,
    toolVersion: request.toolVersion,
    sourceCommit: request.sourceCommit ?? '',
    parameters: JSON.stringify(request.parameters),
    evidenceReference: request.evidenceReference,
    evidenceChecksum: request.evidenceChecksum,
    roots: JSON.stringify(request.roots),
    sourcePlacements: JSON.stringify(request.sourcePlacements ?? []),
  };
}

function scaleRoots(): RegistryImportRoot[] {
  return Array.from({length: 26}, (_, index) => ({
    key: `root-${index + 1}`,
    name: `Root ${index + 1}`,
    deliveryModel: 'dedicated',
    classification: classificationAt(index),
    customerOrganizationId: index === 0 ? '11111111-1111-1111-1111-111111111111' : undefined,
    deploymentTargetId: `22222222-2222-2222-2222-${String(index + 1).padStart(12, '0')}`,
    environmentId: `33333333-3333-3333-3333-${String(index + 1).padStart(12, '0')}`,
    subscriberCustomerOrganizationIds: index === 0 ? ['44444444-4444-4444-4444-444444444444'] : undefined,
    physicalIdentity: `identity-${index + 1}`,
    placements: Array.from({length: index === 0 ? 3 : 1}, (_, placementIndex) => ({
      componentKey: `component-${index + 1}-${placementIndex + 1}`,
      physicalName: `service-${index + 1}-${placementIndex + 1}`,
      configNamespace: `root-${index + 1}`,
      databaseBoundary: `database-${index + 1}`,
      healthAdapter: 'http',
    })),
  }));
}

function classificationAt(index: number): RegistryImportRoot['classification'] {
  if (index < 12) return 'standard';
  if (index < 14) return 'observe_only';
  if (index < 16) return 'external';
  if (index < 19) return 'ignored';
  return 'needs_decision';
}

function resolvedRoots(): RegistryImportRoot[] {
  return scaleRoots().map((root) => ({
    ...root,
    classification: root.classification === 'needs_decision' ? ('shared' as const) : root.classification,
  }));
}

function readyPreview(): RegistryImport {
  return {
    ...preview,
    roots: resolvedRoots(),
    diff: {
      ...preview.diff,
      conflicts: [],
    },
  };
}

function appliedResult(): RegistryImportResult {
  return {
    id: 'import-1',
    previewChecksum,
    state: 'applied',
    applied: true,
    counts: preview.counts,
    checkpoint: 26,
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

function completeCoverage(): RegistryCoverage {
  return {
    importId: 'import-1',
    discoveredRoots: 26,
    classifiedRoots: 26,
    actionableManagedRoots: 19,
    observeOnlyRoots: 2,
    externalRoots: 2,
    ignoredRoots: 3,
    unresolvedRoots: 0,
    discoveredPlacements: 28,
    services: 28,
    omittedPlacements: 0,
    omissions: [],
    complete: true,
  };
}
