import {ChangeDetectionStrategy, Component, DestroyRef, inject, signal} from '@angular/core';
import {takeUntilDestroyed} from '@angular/core/rxjs-interop';
import {FormControl, ReactiveFormsModule} from '@angular/forms';
import {Router, RouterLink} from '@angular/router';
import {firstValueFrom, Observable} from 'rxjs';
import {OperatorControlPlaneService} from '../../services/operator-control-plane.service';
import {OverlayService} from '../../services/overlay.service';
import {
  OperatorCampaignDraft,
  OperatorCampaignDraftValidation,
  OperatorCampaignFilters,
  OperatorCampaignRevision,
  OperatorCampaignRow,
  OperatorCampaignRun,
  OperatorCampaignRunState,
  OperatorControlPlaneError,
  OperatorCreateCampaignDraftRequest,
} from '../../types/operator-control-plane';

const nextPreRunState: Partial<Record<OperatorCampaignRunState, OperatorCampaignRunState>> = {
  DRAFT: 'VALIDATED',
  VALIDATED: 'AWAITING_APPROVAL',
  AWAITING_APPROVAL: 'SCHEDULED',
  SCHEDULED: 'RUNNING',
};
const maximumCampaignMembershipPlanIds = 1000;
const maximumCampaignWaves = 100;
const maximumCampaignWavePlanIds = 1000;
const maximumCampaignPrerequisites = 5000;

interface CampaignWaveEditor {
  order: FormControl<number>;
  name: FormControl<string>;
  planIds: FormControl<string>;
  bakeSeconds: FormControl<number>;
  maximumConcurrency: FormControl<number>;
}

interface CampaignPrerequisiteEditor {
  downstreamPlanId: FormControl<string>;
  upstreamPlanId: FormControl<string>;
  upstreamStepKey: FormControl<string>;
  providerPlacementId: FormControl<string>;
  expectedRuntimeStateChecksum: FormControl<string>;
}

@Component({
  selector: 'app-campaigns',
  imports: [ReactiveFormsModule, RouterLink],
  templateUrl: './campaigns.component.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class CampaignsComponent {
  private readonly controlPlane = inject(OperatorControlPlaneService);
  private readonly overlay = inject(OverlayService);
  private readonly router = inject(Router);
  private readonly destroyRef = inject(DestroyRef);
  private hydratingAuthoring = false;
  private publishIntent: Observable<OperatorCampaignRevision> | null = null;

  protected readonly campaigns = signal<OperatorCampaignRow[]>([]);
  protected readonly loading = signal(true);
  protected readonly loadingMore = signal(false);
  protected readonly error = signal<'forbidden' | 'not-found' | 'failed' | ''>('');
  protected readonly partial = signal(false);
  protected readonly nextCursor = signal('');
  protected readonly total = signal<number | null>(null);
  protected readonly status = new FormControl('', {nonNullable: true});
  protected readonly environmentId = new FormControl('', {nonNullable: true});
  protected readonly deploymentPlanId = new FormControl('', {nonNullable: true});
  protected readonly campaignName = new FormControl('', {nonNullable: true});
  protected readonly campaignDescription = new FormControl('', {nonNullable: true});
  protected readonly membershipPlanIds = new FormControl('', {nonNullable: true});
  protected readonly membershipTagQuery = new FormControl('', {nonNullable: true});
  protected readonly riskMaximumConcurrency = new FormControl(1, {nonNullable: true});
  protected readonly failureToleranceBasisPoints = new FormControl(0, {nonNullable: true});
  protected readonly minimumHealthyBasisPoints = new FormControl(10000, {nonNullable: true});
  protected readonly waves = signal<CampaignWaveEditor[]>([newWaveEditor(1)]);
  protected readonly prerequisites = signal<CampaignPrerequisiteEditor[]>([]);
  protected readonly transitionReason = new FormControl('', {nonNullable: true});
  protected readonly draft = signal<OperatorCampaignDraft | null>(null);
  protected readonly validation = signal<OperatorCampaignDraftValidation | null>(null);
  protected readonly revision = signal<OperatorCampaignRevision | null>(null);
  protected readonly run = signal<OperatorCampaignRun | null>(null);
  protected readonly mutationLoading = signal(false);
  protected readonly mutationError = signal('');
  protected readonly mutationStatus = signal('');
  protected readonly staleRun = signal(false);
  protected readonly authoringChangedAfterValidation = signal(false);
  private readonly validatedRequestSnapshot = signal('');

  constructor() {
    this.watchAuthoringControl(this.campaignName);
    this.watchAuthoringControl(this.campaignDescription);
    this.watchAuthoringControl(this.membershipPlanIds);
    this.watchAuthoringControl(this.membershipTagQuery);
    this.watchAuthoringControl(this.riskMaximumConcurrency);
    this.watchAuthoringControl(this.failureToleranceBasisPoints);
    this.watchAuthoringControl(this.minimumHealthyBasisPoints);
    this.waves().forEach((wave) => this.watchWave(wave));
    void this.loadPage();
  }

  protected async applyFilters(): Promise<void> {
    this.campaigns.set([]);
    this.nextCursor.set('');
    this.total.set(null);
    this.partial.set(false);
    this.loading.set(true);
    await this.loadPage();
  }

  protected async loadMore(): Promise<void> {
    if (!this.nextCursor() || this.loadingMore()) return;
    this.loadingMore.set(true);
    await this.loadPage(this.nextCursor());
  }

  protected statusLabel(status: string): string {
    return status.trim() && status !== 'UNKNOWN' ? status : 'Unknown';
  }

  protected async createDraft(): Promise<void> {
    const request = this.buildCampaignRequest();
    if (!request) return;

    await this.runMutation(async () => {
      const draft = await firstValueFrom(this.controlPlane.createCampaignDraft(request));
      this.draft.set(draft);
      this.clearAfterDraft();
      this.mutationStatus.set(`Campaign draft ${draft.id} revision ${draft.revision} created.`);
    }, 'The campaign draft could not be created.');
  }

  protected async updateDraft(): Promise<void> {
    const current = this.draft();
    const request = this.buildCampaignRequest();
    if (!current || !request || this.mutationLoading()) return;

    this.mutationLoading.set(true);
    this.mutationError.set('');
    this.mutationStatus.set('');
    try {
      const draft = await firstValueFrom(
        this.controlPlane.updateCampaignDraft(current.id, {
          ...request,
          expectedRevision: current.revision,
        })
      );
      this.draft.set(draft);
      this.clearAfterDraft();
      this.mutationStatus.set(`Campaign draft ${draft.id} updated to revision ${draft.revision}.`);
    } catch (error) {
      if (errorStatus(error) === 409) {
        await this.refreshAuthoritativeDraft(
          current.id,
          'Campaign draft changed while you were editing it. The latest server revision is shown; review and validate again.'
        );
      } else {
        this.mutationError.set(errorMessage(error, 'The campaign draft could not be updated.'));
      }
    } finally {
      this.mutationLoading.set(false);
    }
  }

  protected async validateDraft(): Promise<void> {
    const draft = this.draft();
    if (!draft) return;
    const requestSnapshot = this.requestSnapshot();
    if (campaignDraftSnapshot(draft) !== requestSnapshot) {
      this.mutationStatus.set('');
      this.mutationError.set('Update the campaign draft before validating unsaved authoring changes.');
      return;
    }

    await this.runMutation(async () => {
      const validation = await firstValueFrom(this.controlPlane.validateCampaignDraft(draft.id));
      if (
        this.requestSnapshot() !== requestSnapshot ||
        this.draft()?.id !== draft.id ||
        this.draft()?.revision !== draft.revision
      ) {
        this.validation.set(null);
        this.validatedRequestSnapshot.set('');
        this.authoringChangedAfterValidation.set(true);
        this.mutationError.set('Authoring fields changed while validation was running. Update and validate again.');
        return;
      }
      this.validation.set(validation);
      this.validatedRequestSnapshot.set(validation.valid ? requestSnapshot : '');
      this.revision.set(null);
      this.run.set(null);
      this.mutationStatus.set(
        validation.valid
          ? 'Campaign draft is valid and ready to publish.'
          : 'Campaign draft validation failed. Review every issue before publishing.'
      );
    }, 'The campaign draft could not be validated.');
  }

  protected async publishDraft(): Promise<void> {
    const draft = this.draft();
    if (!draft || !this.publicationReady() || this.mutationLoading()) return;

    this.mutationLoading.set(true);
    this.mutationError.set('');
    let authoritative: OperatorCampaignDraft;
    try {
      authoritative = await firstValueFrom(this.controlPlane.getCampaignDraft(draft.id));
    } catch (error) {
      this.mutationError.set(errorMessage(error, 'The latest campaign draft could not be checked.'));
      this.mutationLoading.set(false);
      return;
    }
    this.mutationLoading.set(false);
    if (
      authoritative.revision !== draft.revision ||
      campaignDraftSnapshot(authoritative) !== this.validatedRequestSnapshot()
    ) {
      this.applyAuthoritativeDraft(authoritative);
      this.mutationError.set(
        'Campaign draft changed before publication. The latest server revision is shown; review and validate again.'
      );
      return;
    }

    const confirmed = await firstValueFrom(
      this.overlay.confirm({
        message: {
          message: `Publish the latest observed campaign draft ${draft.id} revision ${draft.revision}?`,
          alert: {
            type: 'warning',
            message:
              'Publishing freezes the campaign membership, waves, prerequisites, policy, and checksum. The server publish contract is not revision-bound.',
          },
        },
        requiredConfirmInputText: draft.id,
        confirmLabel: 'Publish immutable revision',
      })
    );
    if (!confirmed) return;

    await this.runMutation(async () => {
      this.publishIntent ??= this.controlPlane.publishCampaignDraft(draft.id);
      const revision = await firstValueFrom(this.publishIntent);
      this.publishIntent = null;
      this.revision.set(revision);
      this.run.set(null);
      this.mutationStatus.set(`Immutable campaign revision ${revision.id} published.`);
    }, 'The campaign revision could not be published.');
  }

  protected async createRun(): Promise<void> {
    const revision = this.revision();
    if (!revision || this.mutationLoading()) return;

    const confirmed = await firstValueFrom(
      this.overlay.confirm({
        message: {
          message: `Create a runtime campaign from immutable revision ${revision.id}?`,
          alert: {
            type: 'warning',
            message: 'The new run starts in DRAFT. It does not deploy until every pre-run transition is approved.',
          },
        },
        requiredConfirmInputText: revision.canonicalChecksum,
        confirmLabel: 'Create campaign run',
      })
    );
    if (!confirmed) return;

    await this.runMutation(async () => {
      const run = await firstValueFrom(this.controlPlane.startCampaignRun(revision.id));
      this.run.set(run);
      this.staleRun.set(false);
      this.mutationStatus.set(`Campaign run ${run.id} created in ${run.state} at version ${run.version}.`);
    }, 'The campaign run could not be created.');
  }

  protected nextRunState(): OperatorCampaignRunState | null {
    const run = this.run();
    return run ? (nextPreRunState[run.state] ?? null) : null;
  }

  protected addWave(): void {
    const nextOrder = Math.max(0, ...this.waves().map((wave) => wave.order.value)) + 1;
    const wave = newWaveEditor(nextOrder);
    this.watchWave(wave);
    this.waves.update((waves) => [...waves, wave]);
    this.markAuthoringChanged();
  }

  protected removeWave(index: number): void {
    if (this.waves().length === 1) return;
    this.waves.update((waves) => waves.filter((_, waveIndex) => waveIndex !== index));
    this.markAuthoringChanged();
  }

  protected addPrerequisite(): void {
    const prerequisite = newPrerequisiteEditor();
    this.watchPrerequisite(prerequisite);
    this.prerequisites.update((prerequisites) => [...prerequisites, prerequisite]);
    this.markAuthoringChanged();
  }

  protected removePrerequisite(index: number): void {
    this.prerequisites.update((prerequisites) =>
      prerequisites.filter((_, prerequisiteIndex) => prerequisiteIndex !== index)
    );
    this.markAuthoringChanged();
  }

  protected authoringValid(): boolean {
    const name = this.campaignName.value.trim();
    const description = this.campaignDescription.value;
    const planIds = splitIdentifiers(this.membershipPlanIds.value);
    const tagQuery = this.membershipTagQuery.value.trim();
    const waves = this.waves()
      .map((wave) => ({
        order: wave.order.value,
        name: wave.name.value.trim(),
        planIds: splitIdentifiers(wave.planIds.value),
        bakeSeconds: wave.bakeSeconds.value,
        maximumConcurrency: wave.maximumConcurrency.value,
      }))
      .sort((left, right) => left.order - right.order);
    const orders = new Set(waves.map((wave) => wave.order));
    const waveInputsValid =
      waves.length > 0 &&
      waves.length <= maximumCampaignWaves &&
      orders.size === waves.length &&
      waves.every(
        (wave, index) =>
          wave.order > 0 &&
          Boolean(wave.name) &&
          wave.planIds.length > 0 &&
          wave.planIds.length <= maximumCampaignWavePlanIds &&
          wave.bakeSeconds >= 0 &&
          wave.bakeSeconds <= 31536000 &&
          (index === 0 || wave.bakeSeconds >= waves[index - 1].bakeSeconds) &&
          wave.maximumConcurrency > 0 &&
          wave.maximumConcurrency <= 1000
      );
    const prerequisiteInputsValid =
      this.prerequisites().length <= maximumCampaignPrerequisites &&
      this.prerequisites().every((prerequisite) =>
        [
          prerequisite.downstreamPlanId.value,
          prerequisite.upstreamPlanId.value,
          prerequisite.upstreamStepKey.value,
          prerequisite.providerPlacementId.value,
          prerequisite.expectedRuntimeStateChecksum.value,
        ].every((value) => Boolean(value.trim()))
      );

    return (
      name.length > 0 &&
      name.length <= 200 &&
      description.length <= 4000 &&
      (planIds.length > 0 || tagQuery.length > 0) &&
      planIds.length <= maximumCampaignMembershipPlanIds &&
      waveInputsValid &&
      prerequisiteInputsValid &&
      this.riskMaximumConcurrency.value > 0 &&
      this.riskMaximumConcurrency.value <= 1000 &&
      basisPointsValid(this.failureToleranceBasisPoints.value) &&
      basisPointsValid(this.minimumHealthyBasisPoints.value)
    );
  }

  protected campaignRequestPreview(): string {
    return JSON.stringify(this.buildCampaignRequest(false), null, 2);
  }

  protected publicationReady(): boolean {
    return this.validation()?.valid === true && this.validatedRequestSnapshot() === this.requestSnapshot();
  }

  protected authoringMatchesDraft(): boolean {
    const draft = this.draft();
    return draft !== null && campaignDraftSnapshot(draft) === this.requestSnapshot();
  }

  protected async advanceRun(): Promise<void> {
    const run = this.run();
    const to = this.nextRunState();
    const reason = this.transitionReason.value.trim();
    if (!run || !to || !reason || this.mutationLoading()) return;

    const confirmed = await firstValueFrom(
      this.overlay.confirm({
        message: {
          message: `Advance campaign run ${run.id} from ${run.state} to ${to}?`,
          alert: {
            type: to === 'RUNNING' ? 'warning' : 'info',
            message: `The transition is guarded by expected run version ${run.version}.`,
          },
        },
        confirmLabel: `Advance to ${to}`,
      })
    );
    if (!confirmed) return;

    this.mutationLoading.set(true);
    this.mutationError.set('');
    this.mutationStatus.set('');
    this.staleRun.set(false);
    try {
      const transitioned = await firstValueFrom(
        this.controlPlane.transitionCampaignRun(run.id, {
          expectedVersion: run.version,
          to,
          reason,
        })
      );
      this.run.set(transitioned);
      this.mutationStatus.set(
        transitioned.state === 'RUNNING'
          ? 'Campaign rollout is running.'
          : `Campaign run advanced to ${transitioned.state} at version ${transitioned.version}.`
      );
      if (transitioned.state === 'RUNNING' && this.draft()) {
        await this.router.navigate(['/deployments/campaigns', this.draft()!.id]);
      }
    } catch (error) {
      if (errorStatus(error) === 409) {
        this.staleRun.set(true);
        try {
          this.run.set(await firstValueFrom(this.controlPlane.getCampaignRun(run.id)));
          this.mutationError.set('Campaign run changed while you were reviewing it. The authoritative state is shown.');
        } catch (refreshError) {
          this.mutationError.set(errorMessage(refreshError, 'Campaign run changed and could not be refreshed.'));
        }
      } else {
        this.mutationError.set(errorMessage(error, 'The campaign run could not be advanced.'));
      }
    } finally {
      this.mutationLoading.set(false);
    }
  }

  private async loadPage(cursor = ''): Promise<void> {
    this.error.set('');
    try {
      const page = await firstValueFrom(this.controlPlane.listCampaigns(this.filters(cursor)));
      const knownIds = new Set(this.campaigns().map((campaign) => campaign.id));
      this.campaigns.update((campaigns) => [
        ...campaigns,
        ...page.items.filter((campaign) => !knownIds.has(campaign.id)),
      ]);
      this.nextCursor.set(page.nextCursor ?? '');
      this.total.set(page.total ?? null);
      this.partial.set(false);
    } catch (error) {
      if (cursor && this.campaigns().length > 0) {
        this.partial.set(true);
        return;
      }
      this.campaigns.set([]);
      this.nextCursor.set('');
      const status = errorStatus(error);
      this.error.set(status === 403 ? 'forbidden' : status === 404 ? 'not-found' : 'failed');
    } finally {
      this.loading.set(false);
      this.loadingMore.set(false);
    }
  }

  private filters(cursor: string): OperatorCampaignFilters {
    return {
      limit: 25,
      ...(cursor ? {cursor} : {}),
      ...(this.status.value.trim() ? {status: this.status.value.trim()} : {}),
      ...(this.environmentId.value.trim() ? {environmentId: this.environmentId.value.trim()} : {}),
      ...(this.deploymentPlanId.value.trim() ? {deploymentPlanId: this.deploymentPlanId.value.trim()} : {}),
    };
  }

  private buildCampaignRequest(requireValid = true): OperatorCreateCampaignDraftRequest | null {
    if (requireValid && !this.authoringValid()) {
      this.mutationError.set('Complete the required campaign fields before continuing.');
      return null;
    }

    const tagQuery = this.membershipTagQuery.value.trim();
    return {
      name: this.campaignName.value.trim(),
      description: this.campaignDescription.value,
      membership: {
        planIds: splitIdentifiers(this.membershipPlanIds.value),
        ...(tagQuery ? {tagQuery} : {}),
      },
      waves: this.waves().map((wave) => ({
        order: wave.order.value,
        name: wave.name.value.trim(),
        planIds: splitIdentifiers(wave.planIds.value),
        bakeSeconds: wave.bakeSeconds.value,
        maximumConcurrency: wave.maximumConcurrency.value,
      })),
      prerequisites: this.prerequisites().map((prerequisite) => ({
        downstreamPlanId: prerequisite.downstreamPlanId.value.trim(),
        upstreamPlanId: prerequisite.upstreamPlanId.value.trim(),
        upstreamStepKey: prerequisite.upstreamStepKey.value.trim(),
        providerPlacementId: prerequisite.providerPlacementId.value.trim(),
        expectedRuntimeStateChecksum: prerequisite.expectedRuntimeStateChecksum.value.trim(),
      })),
      riskPolicy: {
        maximumConcurrency: this.riskMaximumConcurrency.value,
        failureToleranceBasisPoints: this.failureToleranceBasisPoints.value,
        minimumHealthyBasisPoints: this.minimumHealthyBasisPoints.value,
      },
    };
  }

  private clearAfterDraft(): void {
    this.validation.set(null);
    this.validatedRequestSnapshot.set('');
    this.authoringChangedAfterValidation.set(false);
    this.publishIntent = null;
    this.revision.set(null);
    this.run.set(null);
    this.staleRun.set(false);
  }

  private requestSnapshot(): string {
    return JSON.stringify(this.buildCampaignRequest(false));
  }

  private watchWave(wave: CampaignWaveEditor): void {
    Object.values(wave).forEach((control) => this.watchAuthoringControl(control));
  }

  private watchPrerequisite(prerequisite: CampaignPrerequisiteEditor): void {
    Object.values(prerequisite).forEach((control) => this.watchAuthoringControl(control));
  }

  private watchAuthoringControl(control: FormControl<string | number>): void {
    control.valueChanges.pipe(takeUntilDestroyed(this.destroyRef)).subscribe(() => this.markAuthoringChanged());
  }

  private markAuthoringChanged(): void {
    if (this.hydratingAuthoring || (!this.validation() && !this.revision() && !this.run())) return;
    this.authoringChangedAfterValidation.set(true);
    this.validation.set(null);
    this.validatedRequestSnapshot.set('');
    this.publishIntent = null;
    this.revision.set(null);
    this.run.set(null);
    this.staleRun.set(false);
  }

  private async refreshAuthoritativeDraft(draftId: string, message: string): Promise<void> {
    try {
      this.applyAuthoritativeDraft(await firstValueFrom(this.controlPlane.getCampaignDraft(draftId)));
      this.mutationError.set(message);
    } catch (error) {
      this.mutationError.set(errorMessage(error, 'The latest campaign draft could not be loaded.'));
    }
  }

  private applyAuthoritativeDraft(draft: OperatorCampaignDraft): void {
    this.hydratingAuthoring = true;
    try {
      this.campaignName.setValue(draft.name);
      this.campaignDescription.setValue(draft.description);
      this.membershipPlanIds.setValue(draft.membership.planIds.join('\n'));
      this.membershipTagQuery.setValue(draft.membership.tagQuery ?? '');
      this.riskMaximumConcurrency.setValue(draft.riskPolicy.maximumConcurrency);
      this.failureToleranceBasisPoints.setValue(draft.riskPolicy.failureToleranceBasisPoints);
      this.minimumHealthyBasisPoints.setValue(draft.riskPolicy.minimumHealthyBasisPoints);

      const populatedWaves = draft.waves.map((wave) => populatedWaveEditor(wave));
      const waves = populatedWaves.length > 0 ? populatedWaves : [newWaveEditor(1)];
      waves.forEach((wave) => this.watchWave(wave));
      this.waves.set(waves);

      const prerequisites = draft.prerequisites.map((prerequisite) => populatedPrerequisiteEditor(prerequisite));
      prerequisites.forEach((prerequisite) => this.watchPrerequisite(prerequisite));
      this.prerequisites.set(prerequisites);
      this.draft.set(draft);
      this.clearAfterDraft();
    } finally {
      this.hydratingAuthoring = false;
    }
  }

  private async runMutation(action: () => Promise<void>, fallbackMessage: string): Promise<void> {
    if (this.mutationLoading()) return;
    this.mutationLoading.set(true);
    this.mutationError.set('');
    this.mutationStatus.set('');
    this.staleRun.set(false);
    try {
      await action();
    } catch (error) {
      this.mutationError.set(errorMessage(error, fallbackMessage));
    } finally {
      this.mutationLoading.set(false);
    }
  }
}

function errorStatus(error: unknown): number {
  return typeof error === 'object' && error !== null && 'status' in error && typeof error.status === 'number'
    ? error.status
    : 0;
}

function errorMessage(error: unknown, fallback: string): string {
  const normalized = error as Partial<OperatorControlPlaneError> | null;
  return normalized?.message && typeof normalized.message === 'string' ? normalized.message : fallback;
}

function newWaveEditor(order: number): CampaignWaveEditor {
  return {
    order: new FormControl(order, {nonNullable: true}),
    name: new FormControl('', {nonNullable: true}),
    planIds: new FormControl('', {nonNullable: true}),
    bakeSeconds: new FormControl(0, {nonNullable: true}),
    maximumConcurrency: new FormControl(1, {nonNullable: true}),
  };
}

function newPrerequisiteEditor(): CampaignPrerequisiteEditor {
  return {
    downstreamPlanId: new FormControl('', {nonNullable: true}),
    upstreamPlanId: new FormControl('', {nonNullable: true}),
    upstreamStepKey: new FormControl('', {nonNullable: true}),
    providerPlacementId: new FormControl('', {nonNullable: true}),
    expectedRuntimeStateChecksum: new FormControl('', {nonNullable: true}),
  };
}

function populatedWaveEditor(wave: OperatorCampaignDraft['waves'][number]): CampaignWaveEditor {
  return {
    order: new FormControl(wave.order, {nonNullable: true}),
    name: new FormControl(wave.name, {nonNullable: true}),
    planIds: new FormControl(wave.planIds.join('\n'), {nonNullable: true}),
    bakeSeconds: new FormControl(wave.bakeSeconds, {nonNullable: true}),
    maximumConcurrency: new FormControl(wave.maximumConcurrency, {nonNullable: true}),
  };
}

function populatedPrerequisiteEditor(
  prerequisite: OperatorCampaignDraft['prerequisites'][number]
): CampaignPrerequisiteEditor {
  return {
    downstreamPlanId: new FormControl(prerequisite.downstreamPlanId, {nonNullable: true}),
    upstreamPlanId: new FormControl(prerequisite.upstreamPlanId, {nonNullable: true}),
    upstreamStepKey: new FormControl(prerequisite.upstreamStepKey, {nonNullable: true}),
    providerPlacementId: new FormControl(prerequisite.providerPlacementId, {nonNullable: true}),
    expectedRuntimeStateChecksum: new FormControl(prerequisite.expectedRuntimeStateChecksum, {nonNullable: true}),
  };
}

function campaignDraftSnapshot(draft: OperatorCampaignDraft): string {
  const tagQuery = draft.membership.tagQuery?.trim();
  const request: OperatorCreateCampaignDraftRequest = {
    name: draft.name.trim(),
    description: draft.description,
    membership: {
      planIds: draft.membership.planIds,
      ...(tagQuery ? {tagQuery} : {}),
    },
    waves: draft.waves.map((wave) => ({
      order: wave.order,
      name: wave.name.trim(),
      planIds: wave.planIds,
      bakeSeconds: wave.bakeSeconds,
      maximumConcurrency: wave.maximumConcurrency,
    })),
    prerequisites: draft.prerequisites.map((prerequisite) => ({
      downstreamPlanId: prerequisite.downstreamPlanId.trim(),
      upstreamPlanId: prerequisite.upstreamPlanId.trim(),
      upstreamStepKey: prerequisite.upstreamStepKey.trim(),
      providerPlacementId: prerequisite.providerPlacementId.trim(),
      expectedRuntimeStateChecksum: prerequisite.expectedRuntimeStateChecksum.trim(),
    })),
    riskPolicy: {
      maximumConcurrency: draft.riskPolicy.maximumConcurrency,
      failureToleranceBasisPoints: draft.riskPolicy.failureToleranceBasisPoints,
      minimumHealthyBasisPoints: draft.riskPolicy.minimumHealthyBasisPoints,
    },
  };
  return JSON.stringify(request);
}

function splitIdentifiers(value: string): string[] {
  return value
    .split(/[\s,]+/)
    .map((identifier) => identifier.trim())
    .filter(Boolean);
}

function basisPointsValid(value: number): boolean {
  return value >= 0 && value <= 10000;
}
