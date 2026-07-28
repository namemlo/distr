import {HttpErrorResponse} from '@angular/common/http';
import {ComponentFixture, TestBed} from '@angular/core/testing';
import {By} from '@angular/platform-browser';
import {of, Subject, throwError} from 'rxjs';
import {vi} from 'vitest';
import {AuthService} from '../../services/auth.service';
import {DeploymentRegistryService} from '../../services/deployment-registry.service';
import {FeatureFlagService} from '../../services/feature-flag.service';
import {OperatorControlPlaneService} from '../../services/operator-control-plane.service';
import {TargetConfigSnapshotsService} from '../../services/target-config-snapshots.service';
import {DeploymentRegistryComponent} from '../../setup/registry/deployment-registry.component';
import {SetupComponent} from './setup.component';

describe('SetupComponent', () => {
  let service: any;
  let registryService: any;

  beforeEach(() => {
    service = {
      loadSetupReadiness: vi.fn().mockReturnValue(
        of({
          operatorControlPlaneEnabled: true,
          executorProtocolEnabled: true,
          hasEnabledEnrollment: true,
          hasTargetConfigSnapshot: true,
          registryCoverageComplete: true,
          ready: true,
        })
      ),
    };
    registryService = {
      preview: vi.fn(),
      saveDecision: vi.fn(),
      apply: vi.fn(),
      get: vi.fn(),
      coverage: vi.fn(),
    };

    TestBed.configureTestingModule({
      imports: [SetupComponent],
      providers: [
        {provide: OperatorControlPlaneService, useValue: service},
        {
          provide: AuthService,
          useValue: {
            isVendor: () => true,
            isSuperAdmin: () => false,
            hasAnyRole: () => true,
          },
        },
        {
          provide: FeatureFlagService,
          useValue: {
            isExperimentalFeatureEnabled$: () => of(true),
          },
        },
        {provide: DeploymentRegistryService, useValue: registryService},
        {
          provide: TargetConfigSnapshotsService,
          useValue: {
            list: vi.fn().mockReturnValue(of({items: []})),
            get: vi.fn(),
            create: vi.fn(),
            verify: vi.fn(),
          },
        },
      ],
    });
  });

  it('renders unknown readiness plus registry and configuration setup surfaces before assessment', async () => {
    const {fixture} = await createComponent();
    const text = fixture.nativeElement.textContent as string;

    for (const expected of [
      'Control-plane setup',
      'Feature readiness',
      'Enrollment readiness',
      'Configuration readiness',
      'Registry coverage',
      'Unknown',
      'Registry setup',
      'Target configuration snapshots',
    ]) {
      expect(text).toContain(expected);
    }
  });

  it('loads aggregate readiness from the exact import and deployment unit', async () => {
    const {fixture, component} = await createComponent();
    (component as any).readinessForm.setValue({
      importId: 'AUTO-import-1',
      deploymentUnitId: 'AUTO-unit-1',
    });

    await (component as any).refreshReadiness();
    fixture.detectChanges();

    expect(service.loadSetupReadiness).toHaveBeenCalledWith({
      importId: 'AUTO-import-1',
      deploymentUnitId: 'AUTO-unit-1',
    });
    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('Operator control plane: Enabled');
    expect(text).toContain('Executor protocol: Enabled');
    expect(text).toContain('Enrollment readiness Enabled');
    expect(text).toContain('Configuration readiness Available');
    expect(text).toContain('Registry coverage Complete');
    expect(text).toContain('Overall readiness Ready');
  });

  it('composes the real registry preview, decision, coverage, and confirmed apply flow', async () => {
    const checksum = `sha256:${'a'.repeat(64)}`;
    const unresolvedRoot = {
      key: 'AUTO-root-1',
      name: 'AUTO root',
      deliveryModel: 'shared' as const,
      classification: 'needs_decision' as const,
      deploymentTargetId: 'AUTO-target-1',
      environmentId: 'AUTO-environment-1',
      physicalIdentity: 'AUTO-physical-1',
      placements: [
        {
          componentKey: 'AUTO-component-1',
          physicalName: 'AUTO-service-1',
          configNamespace: 'AUTO-config',
          databaseBoundary: 'AUTO-database',
          healthAdapter: 'http',
        },
      ],
    };
    const preview = {
      id: 'AUTO-import-1',
      previewChecksum: checksum,
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
      diff: {
        creates: [{kind: 'root', rootKey: 'AUTO-root-1', message: 'AUTO create root'}],
        updates: [],
        retirements: [],
        conflicts: [],
      },
      diagnostics: [],
      diagnosticsTruncated: false,
      omissions: [],
      roots: [unresolvedRoot],
    };
    const resolved = {
      ...preview,
      counts: {...preview.counts, classifiedRoots: 1},
      roots: [{...unresolvedRoot, classification: 'shared' as const}],
    };
    const coverage = {
      importId: 'AUTO-import-1',
      discoveredRoots: 1,
      classifiedRoots: 1,
      actionableManagedRoots: 1,
      observeOnlyRoots: 0,
      externalRoots: 0,
      ignoredRoots: 0,
      unresolvedRoots: 0,
      discoveredPlacements: 1,
      services: 1,
      omittedPlacements: 0,
      omissions: [],
      complete: true,
    };
    registryService.preview.mockReturnValue(of(preview));
    registryService.coverage.mockReturnValue(of(coverage));
    registryService.saveDecision.mockReturnValue(of(undefined));
    registryService.get.mockReturnValue(of(resolved));
    registryService.apply.mockReturnValue(
      of({
        id: 'AUTO-import-1',
        previewChecksum: checksum,
        state: 'applied',
        applied: true,
        counts: resolved.counts,
        checkpoint: 1,
      })
    );
    const {fixture} = await createComponent();
    const registry = fixture.debugElement.query(By.directive(DeploymentRegistryComponent))
      .componentInstance as DeploymentRegistryComponent;
    (registry as any).previewForm.setValue({
      sourceKind: 'inventory_report',
      toolName: 'AUTO-registry-audit',
      toolVersion: '1.0.0',
      sourceCommit: 'a'.repeat(40),
      parameters: JSON.stringify({scope: 'AUTO-all'}),
      evidenceReference: `evidence://sha256/${'a'.repeat(64)}`,
      evidenceChecksum: checksum,
      roots: JSON.stringify([unresolvedRoot]),
      sourcePlacements: JSON.stringify([{rootKey: 'AUTO-root-1', physicalName: 'AUTO-service-1'}]),
    });

    await (registry as any).createPreview();
    await (registry as any).saveClassification(unresolvedRoot, 'shared');
    (registry as any).applyConfirmed.setValue(true);
    await (registry as any).apply();

    expect(registryService.preview.mock.calls[0][0]).toEqual({
      sourceKind: 'inventory_report',
      toolName: 'AUTO-registry-audit',
      toolVersion: '1.0.0',
      sourceCommit: 'a'.repeat(40),
      parameters: {scope: 'AUTO-all'},
      evidenceReference: `evidence://sha256/${'a'.repeat(64)}`,
      evidenceChecksum: checksum,
      roots: [unresolvedRoot],
      sourcePlacements: [{rootKey: 'AUTO-root-1', physicalName: 'AUTO-service-1'}],
    });
    expect(registryService.saveDecision).toHaveBeenCalledWith('AUTO-import-1', {
      rootKey: 'AUTO-root-1',
      classification: 'shared',
    });
    expect(registryService.coverage).toHaveBeenCalledWith('AUTO-import-1');
    expect(registryService.apply).toHaveBeenCalledWith('AUTO-import-1', checksum);
  });

  it('renders disabled, missing, and partial readiness without implying readiness', async () => {
    service.loadSetupReadiness.mockReturnValue(
      of({
        operatorControlPlaneEnabled: false,
        executorProtocolEnabled: false,
        hasEnabledEnrollment: false,
        hasTargetConfigSnapshot: false,
        registryCoverageComplete: false,
        ready: false,
      })
    );
    const {fixture, component} = await createComponent();
    (component as any).readinessForm.setValue({
      importId: 'AUTO-import-disabled',
      deploymentUnitId: 'AUTO-unit-disabled',
    });

    await (component as any).refreshReadiness();
    fixture.detectChanges();

    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('Operator control plane: Disabled');
    expect(text).toContain('Executor protocol: Disabled');
    expect(text).toContain('Enrollment readiness Disabled');
    expect(text).toContain('Configuration readiness Missing');
    expect(text).toContain('Registry coverage Partial');
    expect(text).toContain('Overall readiness Not ready');
  });

  it('marks an assessed result stale when its identifying inputs change', async () => {
    const {fixture, component} = await createComponent();
    (component as any).readinessForm.setValue({
      importId: 'AUTO-import-1',
      deploymentUnitId: 'AUTO-unit-1',
    });
    await (component as any).refreshReadiness();

    (component as any).readinessForm.controls.importId.setValue('AUTO-import-2');
    fixture.detectChanges();

    expect(fixture.nativeElement.textContent).toContain('Stale');
  });

  it('renders loading and forbidden readiness independently of the setup tools', async () => {
    const readiness$ = new Subject<any>();
    service.loadSetupReadiness.mockReturnValue(readiness$);
    const {fixture, component} = await createComponent();
    (component as any).readinessForm.setValue({
      importId: 'AUTO-import-1',
      deploymentUnitId: 'AUTO-unit-1',
    });

    const operation = (component as any).refreshReadiness();
    fixture.detectChanges();
    expect(fixture.nativeElement.textContent).toContain('Checking setup readiness');

    readiness$.error(new HttpErrorResponse({status: 403, statusText: 'Forbidden'}));
    await operation;
    fixture.detectChanges();

    expect(fixture.nativeElement.textContent).toContain('You do not have permission to assess control-plane setup.');
    expect(fixture.nativeElement.textContent).toContain('Registry setup');
  });

  it('shows retryable readiness errors without clearing the entered scope', async () => {
    service.loadSetupReadiness.mockReturnValue(
      throwError(() => new HttpErrorResponse({status: 503, statusText: 'Unavailable'}))
    );
    const {fixture, component} = await createComponent();
    (component as any).readinessForm.setValue({
      importId: 'AUTO-import-1',
      deploymentUnitId: 'AUTO-unit-1',
    });

    await (component as any).refreshReadiness();
    fixture.detectChanges();

    expect(fixture.nativeElement.textContent).toContain('Could not assess setup readiness. Try again.');
    expect((component as any).readinessForm.getRawValue()).toEqual({
      importId: 'AUTO-import-1',
      deploymentUnitId: 'AUTO-unit-1',
    });
  });

  async function createComponent(): Promise<{
    fixture: ComponentFixture<SetupComponent>;
    component: SetupComponent;
  }> {
    const fixture = TestBed.createComponent(SetupComponent);
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();
    return {fixture, component: fixture.componentInstance};
  }
});
