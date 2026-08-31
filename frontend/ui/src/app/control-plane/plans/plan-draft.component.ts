import {DatePipe} from '@angular/common';
import {ChangeDetectionStrategy, Component, DestroyRef, inject, OnInit, signal} from '@angular/core';
import {takeUntilDestroyed} from '@angular/core/rxjs-interop';
import {FormBuilder, ReactiveFormsModule, Validators} from '@angular/forms';
import {ActivatedRoute, Router, RouterLink} from '@angular/router';
import {firstValueFrom} from 'rxjs';
import {OperatorControlPlaneService} from '../../services/operator-control-plane.service';
import {OverlayService} from '../../services/overlay.service';
import {
  OperatorCreatePlanDraftRequest,
  OperatorDeploymentUnit,
  OperatorPlanChange,
  OperatorPlanDraft,
  OperatorPlanDraftValidation,
  OperatorReleaseRow,
  OperatorRequirementResolution,
  OperatorTargetPlanEdge,
  OperatorTargetPlanStep,
} from '../../types/operator-control-plane';
import {ProcessSnapshot} from '../../types/release-bundle';
import {TargetConfigSnapshot} from '../../types/target-config-snapshot';

type StepDirection = 'forward' | 'reverse';

@Component({
  selector: 'app-control-plane-plan-draft',
  templateUrl: './plan-draft.component.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [DatePipe, ReactiveFormsModule, RouterLink],
})
export class PlanDraftComponent implements OnInit {
  private readonly service = inject(OperatorControlPlaneService);
  private readonly overlay = inject(OverlayService);
  private readonly route = inject(ActivatedRoute);
  private readonly router = inject(Router);
  private readonly fb = inject(FormBuilder);
  private readonly destroyRef = inject(DestroyRef);
  private processLoadGeneration = 0;
  private snapshotLoadGeneration = 0;

  protected draftId = this.route.snapshot.paramMap.get('draftId') ?? '';
  protected readonly releases = signal<OperatorReleaseRow[]>([]);
  protected readonly units = signal<OperatorDeploymentUnit[]>([]);
  protected readonly snapshots = signal<TargetConfigSnapshot[]>([]);
  protected readonly processSnapshot = signal<ProcessSnapshot | null>(null);
  protected readonly processSnapshotUnavailable = signal(false);
  protected readonly draft = signal<OperatorPlanDraft | null>(null);
  protected readonly validation = signal<OperatorPlanDraftValidation | null>(null);
  protected readonly loading = signal(true);
  protected readonly snapshotsLoading = signal(false);
  protected readonly actionLoading = signal<string | null>(null);
  protected readonly error = signal('');
  protected readonly actionError = signal('');

  protected readonly form = this.fb.nonNullable.group({
    productReleaseId: ['', Validators.required],
    deploymentUnitId: [this.route.snapshot.queryParamMap.get('deploymentUnitId') ?? '', Validators.required],
    environmentAssignmentId: ['', Validators.required],
    targetConfigSnapshotId: ['', Validators.required],
    protocolVersion: ['v2' as 'v1' | 'v2', Validators.required],
    supersedesDeploymentPlanId: [''],
    supersedeReason: ['', Validators.maxLength(2048)],
  });

  ngOnInit(): void {
    this.form.valueChanges.pipe(takeUntilDestroyed(this.destroyRef)).subscribe(() => this.validation.set(null));
    void this.initialize();
  }

  protected async selectDeploymentUnit(): Promise<void> {
    this.validation.set(null);
    this.actionError.set('');
    try {
      await this.loadUnitContext(this.form.controls.deploymentUnitId.value);
    } catch (error) {
      this.actionError.set(this.errorMessage(error, 'Target Config Snapshots could not be loaded.'));
    }
  }

  protected async selectProductRelease(): Promise<void> {
    this.validation.set(null);
    await this.loadProcessSnapshot(this.form.controls.productReleaseId.value);
  }

  protected async saveDraft(): Promise<void> {
    if (!this.canSave()) {
      this.form.markAllAsTouched();
      return;
    }
    await this.runAction('save', async () => {
      await this.persistDraft();
    });
  }

  protected async validateDraft(): Promise<void> {
    if (!this.canSave()) {
      this.form.markAllAsTouched();
      return;
    }
    await this.runAction('validate', async () => {
      let current = this.draft();
      if (!current || this.form.dirty) {
        current = await this.persistDraft();
      }
      const result = await firstValueFrom(this.service.validatePlanDraft(current.id));
      this.draft.set(result.draft);
      this.validation.set(result);
      this.form.markAsPristine();
    });
  }

  protected async publishDraft(): Promise<void> {
    const validation = this.validation();
    const current = this.draft();
    const checksum = validation?.previewChecksum ?? validation?.draft.previewChecksum ?? '';
    if (!current || !validation || validation.issues.length > 0 || !checksum || this.form.dirty || this.isPublished()) {
      return;
    }
    const confirmed = await firstValueFrom(
      this.overlay.confirm({
        message: {
          message: `Publish target-plan draft ${current.id} revision ${validation.draft.revision}?`,
          alert: {
            type: 'warning',
            message: 'Publication freezes the exact dependency bindings, target facts, ordered graph, and checksum.',
          },
        },
        requiredConfirmInputText: checksum,
        confirmLabel: 'Publish immutable plan',
      })
    );
    if (!confirmed) return;

    await this.runAction('publish', async () => {
      const plan = await firstValueFrom(
        this.service.publishPlanDraft(current.id, {
          expectedRevision: validation.draft.revision,
          expectedPreviewChecksum: checksum,
        })
      );
      await this.router.navigate(['/deployments/plans', plan.id], {queryParams: this.deploymentUnitQuery()});
    });
  }

  protected canSave(): boolean {
    return this.form.valid && this.supersessionValid() && !this.isPublished();
  }

  protected canPublish(): boolean {
    const result = this.validation();
    return (
      result !== null &&
      result.issues.length === 0 &&
      Boolean(result.previewChecksum || result.draft.previewChecksum) &&
      !this.form.dirty &&
      !this.isPublished()
    );
  }

  protected isPublished(): boolean {
    return Boolean(this.draft()?.publishedDeploymentPlanId);
  }

  protected supersessionValid(): boolean {
    const planId = this.form.controls.supersedesDeploymentPlanId.value.trim();
    const reason = this.form.controls.supersedeReason.value.trim();
    return Boolean(planId) === Boolean(reason) && !reason.includes('\n') && !reason.includes('\r');
  }

  protected selectedUnit(): OperatorDeploymentUnit | undefined {
    const id = this.form.controls.deploymentUnitId.value;
    return this.units().find((unit) => unit.id === id);
  }

  protected selectedSnapshot(): TargetConfigSnapshot | undefined {
    const id = this.form.controls.targetConfigSnapshotId.value;
    return this.snapshots().find((snapshot) => snapshot.id === id);
  }

  protected selectedRelease(): OperatorReleaseRow | undefined {
    const id = this.form.controls.productReleaseId.value;
    return this.releases().find((release) => release.id === id);
  }

  protected deploymentUnitQuery(): Record<string, string> {
    const deploymentUnitId = this.form.controls.deploymentUnitId.value.trim();
    return deploymentUnitId ? {deploymentUnitId} : {};
  }

  protected orderedResolutions(): OperatorRequirementResolution[] {
    return [...(this.validation()?.resolutions ?? [])].sort(
      (left, right) => left.sortOrder - right.sortOrder || left.requirementKey.localeCompare(right.requirementKey)
    );
  }

  protected orderedSteps(direction: StepDirection): OperatorTargetPlanStep[] {
    const graph = this.validation()?.graph;
    if (!graph) return [];

    const byKey = new Map(graph.steps.map((step) => [step.stepKey, step]));
    const ordered = graph.topologicalOrder.flatMap((key) => {
      const step = byKey.get(key);
      if (!step) return [];
      byKey.delete(key);
      return [step];
    });
    ordered.push(...[...byKey.values()].sort((left, right) => left.sortOrder - right.sortOrder));
    return direction === 'reverse' ? ordered.reverse() : ordered;
  }

  protected orderedEdges(): OperatorTargetPlanEdge[] {
    return [...(this.validation()?.graph.edges ?? [])].sort((left, right) => left.key.localeCompare(right.key));
  }

  protected orderedChanges(): OperatorPlanChange[] {
    return [...(this.validation()?.changes ?? [])].sort(
      (left, right) => left.sortOrder - right.sortOrder || left.componentKey.localeCompare(right.componentKey)
    );
  }

  protected reverseBlocked(step: OperatorTargetPlanStep): boolean {
    return Boolean(
      step.componentKey &&
      this.validation()?.changes.some((change) => change.componentKey === step.componentKey && change.forwardOnly)
    );
  }

  protected processSteps() {
    return [...(this.processSnapshot()?.revision.steps ?? [])].sort(
      (left, right) => left.sortOrder - right.sortOrder || left.key.localeCompare(right.key)
    );
  }

  private async initialize(): Promise<void> {
    this.loading.set(true);
    this.error.set('');
    try {
      const [releases, units] = await Promise.all([this.loadAllProductReleases(), this.loadAllDeploymentUnits()]);
      this.releases.set(releases);
      this.units.set(units);

      if (this.draftId) {
        const draft = await firstValueFrom(this.service.getPlanDraft(this.draftId));
        this.draft.set(draft);
        this.form.patchValue({
          productReleaseId: draft.productReleaseId,
          deploymentUnitId: draft.deploymentUnitId,
          environmentAssignmentId: draft.environmentAssignmentId,
          targetConfigSnapshotId: draft.targetConfigSnapshotId,
          protocolVersion: draft.protocolVersion === 'v1' ? 'v1' : 'v2',
          supersedesDeploymentPlanId: draft.supersedesDeploymentPlanId ?? '',
          supersedeReason: draft.supersedeReason ?? '',
        });
      }

      await Promise.all([
        this.loadUnitContext(this.form.controls.deploymentUnitId.value),
        this.loadProcessSnapshot(this.form.controls.productReleaseId.value),
      ]);
      this.form.markAsPristine();
    } catch (error) {
      this.error.set(this.errorMessage(error, 'The target-plan draft could not be loaded.'));
    } finally {
      this.loading.set(false);
    }
  }

  private async loadAllProductReleases(): Promise<OperatorReleaseRow[]> {
    const releases: OperatorReleaseRow[] = [];
    let cursor: string | undefined;
    do {
      const page = await firstValueFrom(
        this.service.listReleases({kind: 'product', status: 'PUBLISHED', cursor, limit: 100})
      );
      releases.push(...page.items);
      cursor = page.nextCursor;
    } while (cursor);
    return releases.sort(
      (left, right) => right.createdAt.localeCompare(left.createdAt) || left.id.localeCompare(right.id)
    );
  }

  private async loadAllDeploymentUnits(): Promise<OperatorDeploymentUnit[]> {
    const units: OperatorDeploymentUnit[] = [];
    let cursor: string | undefined;
    do {
      const page = await firstValueFrom(this.service.listDeploymentUnits({cursor, limit: 100}));
      units.push(...page.items);
      cursor = page.nextCursor;
    } while (cursor);
    return units
      .filter((unit) => unit.managementState !== 'retired' && !unit.retiredAt)
      .sort((left, right) => left.name.localeCompare(right.name) || left.id.localeCompare(right.id));
  }

  private async loadUnitContext(deploymentUnitId: string): Promise<void> {
    const generation = ++this.snapshotLoadGeneration;
    const unit = this.units().find((candidate) => candidate.id === deploymentUnitId);
    this.snapshots.set([]);
    if (!unit) {
      this.form.patchValue({environmentAssignmentId: '', targetConfigSnapshotId: ''}, {emitEvent: false});
      return;
    }

    this.form.controls.environmentAssignmentId.setValue(unit.targetEnvironmentAssignmentId, {emitEvent: false});
    this.snapshotsLoading.set(true);
    try {
      const snapshots: TargetConfigSnapshot[] = [];
      let cursor: string | undefined;
      do {
        const page = await firstValueFrom(
          this.service.listTargetConfigSnapshots({
            deploymentUnitId: unit.id,
            targetEnvironmentAssignmentId: unit.targetEnvironmentAssignmentId,
            cursor,
            limit: 100,
          })
        );
        snapshots.push(...page.items);
        cursor = page.nextCursor;
      } while (cursor);
      if (generation !== this.snapshotLoadGeneration) return;

      snapshots.sort((left, right) => right.createdAt.localeCompare(left.createdAt) || left.id.localeCompare(right.id));
      this.snapshots.set(snapshots);
      const currentSnapshotId = this.form.controls.targetConfigSnapshotId.value;
      if (!snapshots.some((snapshot) => snapshot.id === currentSnapshotId)) {
        this.form.controls.targetConfigSnapshotId.setValue(snapshots[0]?.id ?? '', {emitEvent: false});
      }
    } finally {
      if (generation === this.snapshotLoadGeneration) this.snapshotsLoading.set(false);
    }
  }

  private async loadProcessSnapshot(releaseId: string): Promise<void> {
    const generation = ++this.processLoadGeneration;
    this.processSnapshot.set(null);
    this.processSnapshotUnavailable.set(false);
    if (!releaseId) return;
    try {
      const snapshot = await firstValueFrom(this.service.getReleaseProcessSnapshot(releaseId));
      if (generation !== this.processLoadGeneration) return;
      this.processSnapshot.set(snapshot);
    } catch {
      if (generation !== this.processLoadGeneration) return;
      this.processSnapshotUnavailable.set(true);
    }
  }

  private async persistDraft(): Promise<OperatorPlanDraft> {
    const request = this.draftRequest();
    const current = this.draft();
    const saved = current
      ? await firstValueFrom(
          this.service.updatePlanDraft(current.id, {
            ...request,
            expectedRevision: current.revision,
          })
        )
      : await firstValueFrom(this.service.createPlanDraft(request));
    this.draft.set(saved);
    this.draftId = saved.id;
    this.validation.set(null);
    this.form.markAsPristine();
    if (!current) {
      await this.router.navigate(['/deployments/plans/drafts', saved.id], {
        queryParams: this.deploymentUnitQuery(),
        replaceUrl: true,
      });
    }
    return saved;
  }

  private draftRequest(): OperatorCreatePlanDraftRequest {
    const value = this.form.getRawValue();
    const request: OperatorCreatePlanDraftRequest = {
      productReleaseId: value.productReleaseId,
      deploymentUnitId: value.deploymentUnitId,
      environmentAssignmentId: value.environmentAssignmentId,
      targetConfigSnapshotId: value.targetConfigSnapshotId,
      protocolVersion: value.protocolVersion,
    };
    const supersedesDeploymentPlanId = value.supersedesDeploymentPlanId.trim();
    const supersedeReason = value.supersedeReason.trim();
    if (supersedesDeploymentPlanId && supersedeReason) {
      request.supersedesDeploymentPlanId = supersedesDeploymentPlanId;
      request.supersedeReason = supersedeReason;
    }
    return request;
  }

  private async runAction(key: string, action: () => Promise<void>): Promise<void> {
    this.actionLoading.set(key);
    this.actionError.set('');
    try {
      await action();
    } catch (error) {
      this.actionError.set(this.errorMessage(error, 'The target-plan action could not be completed.'));
    } finally {
      this.actionLoading.set(null);
    }
  }

  private errorMessage(error: unknown, fallback: string): string {
    if (error && typeof error === 'object' && 'message' in error && typeof error.message === 'string') {
      return error.message;
    }
    return fallback;
  }
}
