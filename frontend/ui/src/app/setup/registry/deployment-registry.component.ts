import {ChangeDetectionStrategy, Component, inject, signal} from '@angular/core';
import {toSignal} from '@angular/core/rxjs-interop';
import {FormBuilder, ReactiveFormsModule, Validators} from '@angular/forms';
import {catchError, firstValueFrom, of} from 'rxjs';
import {AuthService} from '../../services/auth.service';
import {DeploymentRegistryService} from '../../services/deployment-registry.service';
import {FeatureFlagService} from '../../services/feature-flag.service';
import {
  RegistryCoverage,
  RegistryImport,
  RegistryImportClassification,
  RegistryImportRequest,
  RegistryImportRoot,
  RegistryImportSourcePlacement,
} from '../../types/deployment-registry';
import {canMutateDeploymentRegistry} from './deployment-registry-access';

const emptyEvidenceChecksum = '0'.repeat(64);

@Component({
  selector: 'app-deployment-registry',
  imports: [ReactiveFormsModule],
  templateUrl: './deployment-registry.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
})
export class DeploymentRegistryComponent {
  private readonly fb = inject(FormBuilder).nonNullable;
  private readonly auth = inject(AuthService);
  private readonly deploymentRegistryService = inject(DeploymentRegistryService);
  private readonly featureFlags = inject(FeatureFlagService);
  private coverageRequestSequence = 0;

  protected readonly previewForm = this.fb.group({
    sourceKind: this.fb.control('inventory_report', Validators.required),
    toolName: this.fb.control('registry-audit', Validators.required),
    toolVersion: this.fb.control('1.0', Validators.required),
    sourceCommit: this.fb.control(''),
    parameters: this.fb.control('{}', Validators.required),
    evidenceReference: this.fb.control(`evidence://sha256/${emptyEvidenceChecksum}`, Validators.required),
    evidenceChecksum: this.fb.control(emptyEvidenceChecksum, Validators.required),
    roots: this.fb.control('[]', Validators.required),
    sourcePlacements: this.fb.control('[]', Validators.required),
  });
  protected readonly applyConfirmed = this.fb.control(false);
  protected readonly loadingCoverage = signal(false);
  protected readonly previewing = signal(false);
  protected readonly savingRootId = signal('');
  protected readonly applying = signal(false);
  protected readonly error = signal('');
  protected readonly stale = signal(false);
  protected readonly applied = signal(false);
  protected readonly registryImport = signal<RegistryImport | undefined>(undefined);
  protected readonly coverage = signal<RegistryCoverage | undefined>(undefined);
  protected readonly coverageError = signal(false);
  protected readonly mutationAllowed = canMutateDeploymentRegistry(this.auth)
    ? toSignal(
        this.featureFlags.isExperimentalFeatureEnabled$('operator_control_plane_v2').pipe(catchError(() => of(false))),
        {initialValue: false}
      )
    : signal(false);
  protected readonly classifications: RegistryImportClassification[] = [
    'standard',
    'shared',
    'external',
    'observe_only',
    'ignored',
    'needs_decision',
  ];

  protected async createPreview(): Promise<void> {
    if (!this.mutationAllowed()) return;

    this.error.set('');
    this.stale.set(false);
    this.applied.set(false);
    this.applyConfirmed.setValue(false);
    this.invalidateCoverage();
    if (this.previewForm.invalid) {
      this.previewForm.markAllAsTouched();
      return;
    }

    let request: RegistryImportRequest;
    try {
      request = this.previewRequest();
    } catch {
      this.error.set('Parameters must be a JSON object; roots and source placements must be JSON arrays of objects.');
      return;
    }

    this.previewing.set(true);
    try {
      const registryImport = await firstValueFrom(this.deploymentRegistryService.preview(request));
      this.registryImport.set(registryImport);
      await this.loadCoverage(registryImport.id);
    } catch {
      this.error.set('Could not create registry preview.');
    } finally {
      this.previewing.set(false);
    }
  }

  protected async saveClassification(
    root: RegistryImportRoot,
    classification: RegistryImportClassification
  ): Promise<void> {
    const registryImport = this.registryImport();
    if (!registryImport || !this.mutationAllowed()) return;

    this.error.set('');
    this.applyConfirmed.setValue(false);
    this.invalidateCoverage();
    this.savingRootId.set(root.key);
    try {
      await firstValueFrom(
        this.deploymentRegistryService.saveDecision(registryImport.id, {rootKey: root.key, classification})
      );
      const updated = await firstValueFrom(this.deploymentRegistryService.get(registryImport.id));
      this.registryImport.set(updated);
      this.applyConfirmed.setValue(false);
      await this.loadCoverage(updated.id);
    } catch {
      this.error.set('Could not save the classification.');
    } finally {
      this.savingRootId.set('');
    }
  }

  protected async apply(): Promise<void> {
    const registryImport = this.registryImport();
    if (!registryImport || !this.mutationAllowed() || !this.canApply()) return;

    this.error.set('');
    this.applying.set(true);
    try {
      const result = await firstValueFrom(
        this.deploymentRegistryService.apply(registryImport.id, registryImport.previewChecksum)
      );
      this.applied.set(result.applied);
      await this.loadCoverage(result.id);
    } catch (error) {
      if (this.isConflict(error)) {
        this.stale.set(true);
        this.error.set('This preview is stale. Refresh the preview before applying it.');
      } else {
        this.error.set('Could not apply registry import.');
      }
    } finally {
      this.applying.set(false);
    }
  }

  protected canAcknowledge(): boolean {
    const registryImport = this.registryImport();
    const coverage = this.coverage();
    return Boolean(
      this.mutationAllowed() &&
      registryImport &&
      coverage &&
      !this.previewing() &&
      !this.applying() &&
      !this.applied() &&
      !this.loadingCoverage() &&
      !this.coverageError() &&
      this.savingRootId() === '' &&
      !this.stale() &&
      coverage.importId === registryImport.id &&
      coverage.complete &&
      coverage.unresolvedRoots === 0 &&
      coverage.omittedPlacements === 0 &&
      coverage.omissions.length === 0 &&
      coverage.classifiedRoots === coverage.discoveredRoots &&
      registryImport.counts.conflicts === 0 &&
      registryImport.diff.conflicts.length === 0 &&
      registryImport.roots.every((root) => root.classification !== 'needs_decision')
    );
  }

  protected canApply(): boolean {
    return this.canAcknowledge() && this.applyConfirmed.value;
  }

  protected decisionCount(registryImport: RegistryImport): number {
    return registryImport.roots.filter((root) => root.classification !== 'needs_decision').length;
  }

  protected coverageState(): 'loading' | 'error' | 'empty' | 'partial' | 'ready' {
    if (this.loadingCoverage()) return 'loading';
    if (this.coverageError()) return 'error';
    const coverage = this.coverage();
    const registryImport = this.registryImport();
    if (coverage && registryImport && coverage.importId !== registryImport.id) return 'error';
    if (!coverage || coverage.discoveredRoots === 0) return 'empty';
    return coverage.complete ? 'ready' : 'partial';
  }

  protected rootKey(root: RegistryImportRoot): string {
    return root.key;
  }

  private async loadCoverage(importId: string): Promise<void> {
    const requestSequence = ++this.coverageRequestSequence;
    this.loadingCoverage.set(true);
    this.coverageError.set(false);
    this.coverage.set(undefined);
    try {
      const coverage = await firstValueFrom(this.deploymentRegistryService.coverage(importId));
      if (!this.isActiveCoverageRequest(requestSequence, importId)) return;
      if (coverage.importId !== importId) throw new Error('coverage import mismatch');
      this.coverage.set(coverage);
    } catch {
      if (!this.isActiveCoverageRequest(requestSequence, importId)) return;
      this.coverage.set(undefined);
      this.coverageError.set(true);
    } finally {
      if (this.isActiveCoverageRequest(requestSequence, importId)) {
        this.loadingCoverage.set(false);
      }
    }
  }

  private invalidateCoverage(): void {
    this.coverageRequestSequence += 1;
    this.coverage.set(undefined);
    this.coverageError.set(false);
    this.loadingCoverage.set(false);
  }

  private isActiveCoverageRequest(requestSequence: number, importId: string): boolean {
    return requestSequence === this.coverageRequestSequence && this.registryImport()?.id === importId;
  }

  private previewRequest(): RegistryImportRequest {
    const value = this.previewForm.getRawValue();
    return {
      sourceKind: value.sourceKind,
      toolName: value.toolName,
      toolVersion: value.toolVersion,
      sourceCommit: value.sourceCommit,
      parameters: this.parseObject(value.parameters),
      evidenceReference: value.evidenceReference,
      evidenceChecksum: value.evidenceChecksum,
      roots: this.parseArray<RegistryImportRoot>(value.roots),
      sourcePlacements: this.parseArray<RegistryImportSourcePlacement>(value.sourcePlacements),
    };
  }

  private parseObject(value: string): Record<string, string> {
    const parsed: unknown = JSON.parse(value);
    if (!parsed || Array.isArray(parsed) || typeof parsed !== 'object') throw new Error('expected object');
    if (Object.values(parsed).some((item) => typeof item !== 'string')) {
      throw new Error('expected string values');
    }
    return parsed as Record<string, string>;
  }

  private parseArray<T>(value: string): T[] {
    const parsed: unknown = JSON.parse(value);
    if (!Array.isArray(parsed) || parsed.some((item) => !item || typeof item !== 'object' || Array.isArray(item))) {
      throw new Error('expected array of objects');
    }
    return parsed as T[];
  }

  private isConflict(error: unknown): boolean {
    return typeof error === 'object' && error !== null && 'status' in error && error.status === 409;
  }
}
