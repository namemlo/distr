import {ChangeDetectionStrategy, Component, DestroyRef, inject, signal} from '@angular/core';
import {takeUntilDestroyed} from '@angular/core/rxjs-interop';
import {FormControl, FormGroup, ReactiveFormsModule, Validators} from '@angular/forms';
import {ActivatedRoute, Router} from '@angular/router';
import {firstValueFrom} from 'rxjs';
import {OperatorControlPlaneService} from '../../services/operator-control-plane.service';
import {OverlayService} from '../../services/overlay.service';
import {
  OperatorControlPlaneError,
  OperatorCreateProductReleaseRequest,
  OperatorEvidenceRef,
  OperatorProductRelease,
  OperatorProductReleaseValidation,
  OperatorReleaseCompare,
  OperatorReleaseDetail,
  OperatorReleaseFilters,
  OperatorReleaseRow,
} from '../../types/operator-control-plane';
import {
  CreateUpdateReleaseBundleRequest,
  ReleaseBundle,
  ReleaseBundleValidationResponse,
} from '../../types/release-bundle';

type EvidenceState = 'complete' | 'partial' | 'stale' | 'unknown';

@Component({
  selector: 'app-releases',
  imports: [ReactiveFormsModule],
  templateUrl: './releases.component.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ReleasesComponent {
  private readonly route = inject(ActivatedRoute);
  private readonly router = inject(Router);
  private readonly overlay = inject(OverlayService);
  private readonly destroyRef = inject(DestroyRef);
  private readonly controlPlane = inject(OperatorControlPlaneService);
  private requestVersion = 0;

  protected readonly compareReleaseId = new FormControl('', {nonNullable: true, validators: Validators.required});
  protected readonly filterForm = new FormGroup({
    customerOrganizationId: new FormControl('', {nonNullable: true}),
    applicationId: new FormControl('', {nonNullable: true}),
    deploymentUnitId: new FormControl('', {nonNullable: true}),
    kind: new FormControl('', {nonNullable: true}),
    status: new FormControl('', {nonNullable: true}),
    search: new FormControl('', {nonNullable: true}),
  });
  protected readonly componentReleaseRequest = new FormControl('', {
    nonNullable: true,
    validators: Validators.required,
  });
  protected readonly componentReleaseForm = new FormGroup({
    request: this.componentReleaseRequest,
  });
  protected readonly productReleaseRequest = new FormControl('', {
    nonNullable: true,
    validators: Validators.required,
  });
  protected readonly productReleaseForm = new FormGroup({
    request: this.productReleaseRequest,
  });
  protected readonly compareForm = new FormGroup({
    releaseId: this.compareReleaseId,
  });
  protected readonly releases = signal<OperatorReleaseRow[]>([]);
  protected readonly nextCursor = signal<string | undefined>(undefined);
  protected readonly releaseId = signal('');
  protected readonly detail = signal<OperatorReleaseDetail | undefined>(undefined);
  protected readonly evidence = signal<OperatorEvidenceRef[]>([]);
  protected readonly comparison = signal<OperatorReleaseCompare | undefined>(undefined);
  protected readonly componentRelease = signal<ReleaseBundle | undefined>(undefined);
  protected readonly componentValidation = signal<ReleaseBundleValidationResponse | undefined>(undefined);
  protected readonly productRelease = signal<OperatorProductRelease | undefined>(undefined);
  protected readonly productValidation = signal<OperatorProductReleaseValidation | undefined>(undefined);
  protected readonly loading = signal(true);
  protected readonly loadingMore = signal(false);
  protected readonly comparing = signal(false);
  protected readonly evidenceIncomplete = signal(false);
  protected readonly mutating = signal(false);
  protected readonly error = signal('');
  protected readonly compareError = signal('');
  protected readonly mutationError = signal('');
  protected readonly mutationStatus = signal('');

  constructor() {
    this.route.paramMap.pipe(takeUntilDestroyed(this.destroyRef)).subscribe((params) => {
      const releaseId = params.get('releaseId') ?? '';
      this.releaseId.set(releaseId);
      this.resetSelection();
      void (releaseId ? this.loadDetail(releaseId) : this.loadList());
    });
  }

  protected async loadList(): Promise<void> {
    const version = ++this.requestVersion;
    this.loading.set(true);
    this.error.set('');
    this.evidenceIncomplete.set(false);
    try {
      const page = await firstValueFrom(this.controlPlane.listReleases(this.releaseFilters()));
      if (version !== this.requestVersion) return;
      this.releases.set(page.items);
      this.nextCursor.set(page.nextCursor);
    } catch (error) {
      if (version !== this.requestVersion) return;
      this.releases.set([]);
      this.nextCursor.set(undefined);
      this.error.set(this.listErrorMessage(error));
    } finally {
      if (version === this.requestVersion) this.loading.set(false);
    }
  }

  protected async loadMore(): Promise<void> {
    const cursor = this.nextCursor();
    if (!cursor || this.loadingMore()) return;

    this.loadingMore.set(true);
    this.error.set('');
    try {
      const page = await firstValueFrom(this.controlPlane.listReleases({...this.releaseFilters(), cursor}));
      this.releases.update((current) => {
        const knownIds = new Set(current.map((release) => release.id));
        return [...current, ...page.items.filter((release) => !knownIds.has(release.id))];
      });
      this.nextCursor.set(page.nextCursor);
    } catch (error) {
      this.error.set(this.listErrorMessage(error));
    } finally {
      this.loadingMore.set(false);
    }
  }

  protected async applyFilters(): Promise<void> {
    this.nextCursor.set(undefined);
    await this.loadList();
  }

  protected async loadDetail(releaseId: string): Promise<void> {
    const version = ++this.requestVersion;
    this.loading.set(true);
    this.error.set('');
    try {
      const deploymentUnitId = this.route.snapshot?.queryParamMap?.get('deploymentUnitId') ?? undefined;
      const response = await firstValueFrom(
        deploymentUnitId
          ? this.controlPlane.getRelease(releaseId, deploymentUnitId)
          : this.controlPlane.getRelease(releaseId)
      );
      if (version !== this.requestVersion) return;
      this.detail.set(response.detail);
      if (response.detail.release.kind.toUpperCase() === 'PRODUCT') {
        try {
          this.productRelease.set(await firstValueFrom(this.controlPlane.getProductRelease(releaseId)));
        } catch {
          this.productRelease.set(undefined);
        }
      }
      this.evidence.set(this.dedupeEvidence(response.detail.evidence));
      try {
        const evidence = await firstValueFrom(this.controlPlane.getReleaseEvidence(releaseId));
        if (version !== this.requestVersion) return;
        this.evidence.set(this.dedupeEvidence([...response.detail.evidence, ...evidence.items]));
      } catch {
        if (version !== this.requestVersion) return;
        this.evidenceIncomplete.set(true);
      }
    } catch (error) {
      if (version !== this.requestVersion) return;
      this.detail.set(undefined);
      this.evidence.set([]);
      this.error.set(this.detailErrorMessage(error));
    } finally {
      if (version === this.requestVersion) this.loading.set(false);
    }
  }

  protected async compare(): Promise<void> {
    const currentReleaseId = this.releaseId();
    if (!currentReleaseId || this.compareReleaseId.invalid) {
      this.compareReleaseId.markAsTouched();
      return;
    }

    this.comparing.set(true);
    this.compareError.set('');
    try {
      const response = await firstValueFrom(
        this.controlPlane.compareReleases(currentReleaseId, this.compareReleaseId.value)
      );
      this.comparison.set(response.comparison);
    } catch (error) {
      this.comparison.set(undefined);
      this.compareError.set(
        (error as Partial<OperatorControlPlaneError> | null)?.status === 404
          ? 'The comparison release was not found or is outside your scope.'
          : 'The releases could not be compared. Try again.'
      );
    } finally {
      this.comparing.set(false);
    }
  }

  protected async createComponentRelease(): Promise<void> {
    const request = this.parseRequest<CreateUpdateReleaseBundleRequest>(
      this.componentReleaseRequest,
      'Enter a valid component release request as JSON.'
    );
    if (!request) return;

    await this.runMutation(async () => {
      const release = await firstValueFrom(this.controlPlane.createComponentRelease(request));
      this.componentRelease.set(release);
      this.componentValidation.set(undefined);
      this.mutationStatus.set(`Component release draft ${release.id} created.`);
    }, 'The component release could not be created.');
  }

  protected async validateComponentRelease(): Promise<void> {
    const release = this.componentRelease();
    if (!release) return;

    await this.runMutation(async () => {
      const validation = await firstValueFrom(this.controlPlane.validateComponentRelease(release.id));
      this.componentValidation.set(validation);
      this.mutationStatus.set(
        validation.valid ? 'Component release is valid and ready to publish.' : 'Component release validation failed.'
      );
    }, 'The component release could not be validated.');
  }

  protected async publishComponentRelease(): Promise<void> {
    const release = this.componentRelease();
    if (!release || !this.componentValidation()?.valid) return;

    const confirmed = await firstValueFrom(
      this.overlay.confirm({
        message: {message: `Publishing component release ${release.id} makes it immutable.`},
        requiredConfirmInputText: release.id,
      })
    );
    if (!confirmed) return;

    await this.runMutation(async () => {
      const published = await firstValueFrom(this.controlPlane.publishComponentRelease(release.id));
      this.componentRelease.set(published);
      this.mutationStatus.set(`Published component release ${published.id}.`);
      await this.navigateToPublished(published.id);
    }, 'The component release could not be published.');
  }

  protected async createProductRelease(): Promise<void> {
    const request = this.parseRequest<OperatorCreateProductReleaseRequest>(
      this.productReleaseRequest,
      'Enter a valid product release request as JSON.'
    );
    if (!request) return;

    await this.runMutation(async () => {
      const release = await firstValueFrom(this.controlPlane.createProductRelease(request));
      this.productRelease.set(release);
      this.productValidation.set(undefined);
      this.mutationStatus.set(`Product release draft ${release.id} created.`);
    }, 'The product release could not be created.');
  }

  protected async validateProductRelease(): Promise<void> {
    const release = this.productRelease();
    if (!release) return;

    await this.runMutation(async () => {
      const validation = await firstValueFrom(this.controlPlane.validateProductRelease(release.id));
      this.productValidation.set(validation);
      this.mutationStatus.set(
        validation.valid ? 'Product release is valid and ready to publish.' : 'Product release validation failed.'
      );
    }, 'The product release could not be validated.');
  }

  protected async publishProductRelease(): Promise<void> {
    const release = this.productRelease();
    if (!release || !this.productValidation()?.valid) return;

    const confirmed = await firstValueFrom(
      this.overlay.confirm({
        message: {message: `Publishing product release ${release.id} makes it immutable.`},
        requiredConfirmInputText: release.id,
      })
    );
    if (!confirmed) return;

    await this.runMutation(async () => {
      const published = await firstValueFrom(this.controlPlane.publishProductRelease(release.id));
      this.productRelease.set(published);
      this.mutationStatus.set(`Published product release ${published.id}.`);
      await this.navigateToPublished(published.id);
    }, 'The product release could not be published.');
  }

  protected evidenceState(release: OperatorReleaseRow): EvidenceState {
    const status = release.status.toUpperCase();
    if (status.includes('UNKNOWN')) return 'unknown';
    if (status.includes('STALE')) return 'stale';
    if (
      !release.checksum ||
      (release.evidenceCount > 0 && this.releaseId() === release.id && this.evidence().length === 0)
    ) {
      return 'partial';
    }
    return 'complete';
  }

  protected evidenceLabel(release: OperatorReleaseRow): string {
    switch (this.evidenceState(release)) {
      case 'partial':
        return 'Partial evidence';
      case 'stale':
        return 'Stale evidence';
      case 'unknown':
        return 'Unknown evidence';
      default:
        return 'Complete evidence';
    }
  }

  protected badgeClass(state: string): string {
    const value = state.toUpperCase();
    if (value.includes('FAIL') || value.includes('BLOCK') || value.includes('UNKNOWN')) {
      return 'border-red-200 bg-red-50 text-red-800 dark:border-red-900 dark:bg-red-950 dark:text-red-200';
    }
    if (value.includes('DRAFT') || value.includes('PARTIAL') || value.includes('STALE')) {
      return 'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900 dark:bg-amber-950 dark:text-amber-200';
    }
    return 'border-green-200 bg-green-50 text-green-800 dark:border-green-900 dark:bg-green-950 dark:text-green-200';
  }

  protected platformEntries(platformDigests: Record<string, string>): [string, string][] {
    return Object.entries(platformDigests).sort(([left], [right]) => left.localeCompare(right));
  }

  protected releaseHref(releaseId: string): string {
    const deploymentUnitId = this.filterForm.controls.deploymentUnitId.value.trim();
    return deploymentUnitId
      ? `/releases/${releaseId}?deploymentUnitId=${encodeURIComponent(deploymentUnitId)}`
      : `/releases/${releaseId}`;
  }

  private releaseFilters(): OperatorReleaseFilters {
    const raw = this.filterForm.getRawValue();
    return {
      ...(raw.customerOrganizationId.trim() ? {customerOrganizationId: raw.customerOrganizationId.trim()} : {}),
      ...(raw.applicationId.trim() ? {applicationId: raw.applicationId.trim()} : {}),
      ...(raw.deploymentUnitId.trim() ? {deploymentUnitId: raw.deploymentUnitId.trim()} : {}),
      ...(raw.kind ? {kind: raw.kind} : {}),
      ...(raw.status ? {status: raw.status} : {}),
      ...(raw.search.trim() ? {search: raw.search.trim()} : {}),
      limit: 50,
    };
  }

  private resetSelection(): void {
    this.requestVersion++;
    this.releases.set([]);
    this.nextCursor.set(undefined);
    this.detail.set(undefined);
    this.evidence.set([]);
    this.comparison.set(undefined);
    this.compareReleaseId.reset('');
    this.error.set('');
    this.compareError.set('');
    this.evidenceIncomplete.set(false);
  }

  private dedupeEvidence(items: OperatorEvidenceRef[]): OperatorEvidenceRef[] {
    const seen = new Set<string>();
    return items.filter((item) => {
      if (seen.has(item.id)) return false;
      seen.add(item.id);
      return true;
    });
  }

  private parseRequest<T>(control: FormControl<string>, errorMessage: string): T | undefined {
    this.mutationError.set('');
    if (control.invalid) {
      control.markAsTouched();
      this.mutationError.set(errorMessage);
      return undefined;
    }
    try {
      const request: unknown = JSON.parse(control.value);
      if (!request || Array.isArray(request) || typeof request !== 'object') throw new Error('invalid request');
      return request as T;
    } catch {
      this.mutationError.set(errorMessage);
      return undefined;
    }
  }

  private async runMutation(action: () => Promise<void>, fallbackMessage: string): Promise<void> {
    this.mutating.set(true);
    this.mutationError.set('');
    this.mutationStatus.set('');
    try {
      await action();
    } catch {
      this.mutationError.set(fallbackMessage);
    } finally {
      this.mutating.set(false);
    }
  }

  private async navigateToPublished(releaseId: string): Promise<void> {
    const deploymentUnitId =
      this.filterForm.controls.deploymentUnitId.value.trim() ||
      this.route.snapshot?.queryParamMap?.get('deploymentUnitId')?.trim();
    if (deploymentUnitId) {
      await this.router.navigate(['/releases', releaseId], {queryParams: {deploymentUnitId}});
    } else {
      await this.router.navigate(['/releases', releaseId]);
    }
    if (this.releaseId() !== releaseId) {
      this.releaseId.set(releaseId);
      await this.loadDetail(releaseId);
    }
  }

  private listErrorMessage(error: unknown): string {
    const status = (error as Partial<OperatorControlPlaneError> | null)?.status;
    if (status === 403) return 'You are not authorized to view operator releases.';
    if (status === 404) return 'The operator control plane is disabled for this organization.';
    return 'Releases could not be loaded. Try again.';
  }

  private detailErrorMessage(error: unknown): string {
    const status = (error as Partial<OperatorControlPlaneError> | null)?.status;
    if (status === 403) return 'You are not authorized to view this release.';
    if (status === 404) return 'This release was not found or is outside your scope.';
    return 'The release could not be loaded. Try again.';
  }
}
