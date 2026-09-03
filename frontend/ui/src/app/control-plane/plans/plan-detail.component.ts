import {DatePipe} from '@angular/common';
import {ChangeDetectionStrategy, Component, DestroyRef, inject, OnInit, signal} from '@angular/core';
import {takeUntilDestroyed} from '@angular/core/rxjs-interop';
import {FormBuilder, ReactiveFormsModule, Validators} from '@angular/forms';
import {ActivatedRoute, Router, RouterLink} from '@angular/router';
import {distinctUntilChanged, firstValueFrom, map} from 'rxjs';
import {OperatorControlPlaneService} from '../../services/operator-control-plane.service';
import {OverlayService} from '../../services/overlay.service';
import {
  OperatorAdmissionEvaluation,
  OperatorBaselineAdoption,
  OperatorBaselineAdoptionComponentRequest,
  OperatorControlPlaneError,
  OperatorEvidenceRef,
  OperatorPlanCompare,
  OperatorPlanDetail,
  OperatorPlanDraftValidation,
  OperatorPlanFact,
  OperatorReviewAdmissionDecision,
  OperatorReviewAdmissionDecisionValue,
  OperatorReviewAdmissionMaterial,
} from '../../types/operator-control-plane';

const actionablePlanStatuses = new Set(['DRAFT', 'VALIDATING', 'BLOCKED', 'READY', 'PUBLISHED']);
const knownPlanStatuses = new Set([...actionablePlanStatuses, 'EXPIRED', 'EXECUTED', 'STALE', 'DISABLED']);
const previousStatePlanStatuses = new Set([...actionablePlanStatuses, 'EXECUTED']);
const baselineAdoptionComponentKeys: Array<keyof OperatorBaselineAdoptionComponentRequest> = [
  'componentInstanceId',
  'componentKey',
  'componentReleaseId',
  'componentReleaseChecksum',
  'sourceCommit',
  'buildId',
  'provenanceVerificationId',
  'provenanceEvidenceDigest',
  'provenancePolicyChecksum',
  'artifactDigest',
  'platform',
  'configChecksum',
  'schemaVersion',
  'capabilityChecksum',
  'topologyChecksum',
  'observationId',
  'observerId',
  'observationEvidenceChecksum',
  'observationStateChecksum',
  'observationRuntimeStateChecksum',
];

interface PlanFactSection {
  title: string;
  facts: OperatorPlanFact[];
}

@Component({
  selector: 'app-control-plane-plan-detail',
  templateUrl: './plan-detail.component.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [DatePipe, ReactiveFormsModule, RouterLink],
})
export class PlanDetailComponent implements OnInit {
  private readonly service = inject(OperatorControlPlaneService);
  private readonly overlay = inject(OverlayService);
  private readonly route = inject(ActivatedRoute);
  private readonly router = inject(Router);
  private readonly fb = inject(FormBuilder);
  private readonly destroyRef = inject(DestroyRef);
  private planLoadGeneration = 0;

  protected planId = '';
  protected readonly detail = signal<OperatorPlanDetail | null>(null);
  protected readonly evidence = signal<OperatorEvidenceRef[]>([]);
  protected readonly comparison = signal<OperatorPlanCompare | null>(null);
  protected readonly draftValidation = signal<OperatorPlanDraftValidation | null>(null);
  protected readonly reviewMaterial = signal<OperatorReviewAdmissionMaterial | null>(null);
  protected readonly reviewDecisionHistory = signal<OperatorReviewAdmissionDecision[]>([]);
  protected readonly admissionEvaluation = signal<OperatorAdmissionEvaluation | null>(null);
  protected readonly baselineAdoption = signal<OperatorBaselineAdoption | null>(null);
  protected readonly loading = signal(true);
  protected readonly evidenceLoading = signal(true);
  protected readonly reviewMaterialLoading = signal(true);
  protected readonly reviewDecisionHistoryLoading = signal(true);
  protected readonly actionLoading = signal<string | null>(null);
  protected readonly error = signal<OperatorControlPlaneError | null>(null);
  protected readonly evidenceError = signal<OperatorControlPlaneError | null>(null);
  protected readonly reviewMaterialError = signal<OperatorControlPlaneError | null>(null);
  protected readonly reviewDecisionHistoryError = signal<OperatorControlPlaneError | null>(null);
  protected readonly actionError = signal<string | null>(null);

  protected readonly compareForm = this.fb.nonNullable.group({
    otherPlanId: [this.route.snapshot.queryParamMap.get('compareWith') ?? '', Validators.required],
  });
  protected readonly draftForm = this.fb.nonNullable.group({
    draftId: ['', Validators.required],
    expectedRevision: [1, [Validators.required, Validators.min(1)]],
    expectedPreviewChecksum: ['', Validators.required],
  });
  protected readonly approvalForm = this.fb.nonNullable.group({
    expiresAt: ['', Validators.required],
  });
  protected readonly reviewDecisionForm = this.fb.nonNullable.group({
    reason: ['', [Validators.required, Validators.maxLength(4096)]],
    expiresAt: ['', Validators.required],
  });
  protected readonly previousStateForm = this.fb.nonNullable.group({
    successfulDeploymentPlanId: ['', Validators.required],
    reason: ['', [Validators.required, Validators.maxLength(2048)]],
  });
  protected readonly baselineAdoptionForm = this.fb.nonNullable.group({
    reason: ['', [Validators.required, Validators.maxLength(2048)]],
    components: ['[]', Validators.required],
  });

  ngOnInit() {
    this.route.paramMap
      .pipe(
        map((params) => params.get('planId') ?? ''),
        distinctUntilChanged(),
        takeUntilDestroyed(this.destroyRef)
      )
      .subscribe((planId) => {
        this.planId = planId;
        this.resetPlanState();
        this.load();
      });
    if (this.compareForm.valid) {
      this.compare();
    }
  }

  private resetPlanState() {
    this.detail.set(null);
    this.evidence.set([]);
    this.comparison.set(null);
    this.draftValidation.set(null);
    this.reviewMaterial.set(null);
    this.reviewDecisionHistory.set([]);
    this.admissionEvaluation.set(null);
    this.baselineAdoption.set(null);
    this.actionError.set(null);
    this.error.set(null);
    this.evidenceError.set(null);
    this.reviewMaterialError.set(null);
    this.reviewDecisionHistoryError.set(null);
  }

  protected load() {
    const generation = ++this.planLoadGeneration;
    this.loading.set(true);
    this.evidenceLoading.set(true);
    this.reviewMaterialLoading.set(true);
    this.reviewDecisionHistoryLoading.set(true);
    this.error.set(null);
    this.evidenceError.set(null);
    this.reviewMaterialError.set(null);
    this.reviewDecisionHistoryError.set(null);

    this.service.getPlan(this.planId).subscribe({
      next: ({detail}) => {
        if (generation !== this.planLoadGeneration) return;
        this.detail.set(detail);
        this.evidence.set(detail.evidence ?? []);
        this.loading.set(false);
      },
      error: (error: OperatorControlPlaneError) => {
        if (generation !== this.planLoadGeneration) return;
        this.error.set(error);
        this.loading.set(false);
      },
    });

    this.service.getPlanEvidence(this.planId).subscribe({
      next: (page) => {
        if (generation !== this.planLoadGeneration) return;
        this.evidence.set(page.items);
        this.evidenceLoading.set(false);
      },
      error: (error: OperatorControlPlaneError) => {
        if (generation !== this.planLoadGeneration) return;
        this.evidenceError.set(error);
        this.evidenceLoading.set(false);
      },
    });

    this.loadReviewMaterial(generation);
    this.loadReviewDecisionHistory(generation);
  }

  private loadReviewMaterial(generation = this.planLoadGeneration) {
    this.reviewMaterialLoading.set(true);
    this.reviewMaterialError.set(null);
    this.service.getReviewAdmissionMaterial(this.planId).subscribe({
      next: (material) => {
        if (generation !== this.planLoadGeneration) return;
        this.reviewMaterial.set(material);
        this.reviewMaterialLoading.set(false);
      },
      error: (error: OperatorControlPlaneError) => {
        if (generation !== this.planLoadGeneration) return;
        this.reviewMaterial.set(null);
        this.reviewMaterialError.set(error);
        this.reviewMaterialLoading.set(false);
      },
    });
  }

  private loadReviewDecisionHistory(generation = this.planLoadGeneration) {
    this.reviewDecisionHistoryLoading.set(true);
    this.reviewDecisionHistoryError.set(null);
    this.service.listReviewAdmissionDecisions(this.planId).subscribe({
      next: (decisions) => {
        if (generation !== this.planLoadGeneration) return;
        this.reviewDecisionHistory.set(decisions);
        this.reviewDecisionHistoryLoading.set(false);
      },
      error: (error: OperatorControlPlaneError) => {
        if (generation !== this.planLoadGeneration) return;
        this.reviewDecisionHistory.set([]);
        this.reviewDecisionHistoryError.set(error);
        this.reviewDecisionHistoryLoading.set(false);
      },
    });
  }

  protected compare() {
    if (this.compareForm.invalid) {
      return;
    }
    const otherPlanId = this.compareForm.controls.otherPlanId.value.trim();
    this.actionError.set(null);
    this.service.comparePlans(this.planId, otherPlanId).subscribe({
      next: ({comparison}) => this.comparison.set(comparison),
      error: (error: OperatorControlPlaneError) => this.actionError.set(error.message),
    });
  }

  protected async validateDraft() {
    const draftId = this.draftForm.controls.draftId.value.trim();
    if (!draftId) {
      return;
    }
    await this.runAction('validate', async () => {
      const result = await firstValueFrom(this.service.validatePlanDraft(draftId));
      this.draftValidation.set(result);
      this.draftForm.patchValue({
        expectedRevision: result.draft.revision,
        expectedPreviewChecksum: result.previewChecksum ?? result.draft.previewChecksum ?? '',
      });
    });
  }

  protected async publishDraft() {
    if (this.draftForm.invalid || !this.actionsEnabled()) {
      return;
    }
    const values = this.draftForm.getRawValue();
    const confirmed = await firstValueFrom(
      this.overlay.confirm({
        message: {
          message: `Publish draft ${values.draftId} revision ${values.expectedRevision}?`,
          alert: {
            type: 'warning',
            message: 'Publishing freezes an immutable plan checksum and invalidates stale approval assumptions.',
          },
        },
        requiredConfirmInputText: values.expectedPreviewChecksum,
        confirmLabel: 'Publish immutable plan',
      })
    );
    if (!confirmed) {
      return;
    }
    await this.runAction('publish', async () => {
      const result = await firstValueFrom(
        this.service.publishPlanDraft(values.draftId, {
          expectedRevision: values.expectedRevision,
          expectedPreviewChecksum: values.expectedPreviewChecksum,
        })
      );
      await this.router.navigate(['/deployments/plans', result.id], {queryParams: this.deploymentUnitQuery()});
    });
  }

  protected async requestApproval() {
    if (this.approvalForm.invalid || !this.actionsEnabled()) {
      return;
    }
    const expiresAt = new Date(this.approvalForm.controls.expiresAt.value).toISOString();
    const confirmed = await firstValueFrom(
      this.overlay.confirm({
        message: {
          message: `Request approval for plan ${this.planId}?`,
          alert: {
            type: 'warning',
            message: `The decision is bound to ${this.detail()?.plan.canonicalChecksum || 'the current checksum'}.`,
          },
        },
        confirmLabel: 'Request approval',
      })
    );
    if (!confirmed) {
      return;
    }
    await this.runAction('approval', async () => {
      const result = await firstValueFrom(this.service.requestPlanApproval(this.planId, {expiresAt}));
      await this.router.navigate(['/approvals'], {queryParams: {requestId: result.id}});
    });
  }

  protected async recordReviewDecision(decision: OperatorReviewAdmissionDecisionValue) {
    const material = this.reviewMaterial();
    if (this.reviewDecisionForm.invalid || !material?.canDecide || this.actionLoading()) {
      return;
    }
    const values = this.reviewDecisionForm.getRawValue();
    const latest = material.latestDecision;
    const confirmed = await firstValueFrom(
      this.overlay.confirm({
        message: {
          message: `Record ${decision} for the current observed deployment material?`,
          alert: {
            type: decision === 'NO_GO' ? 'danger' : 'warning',
            message: 'This appends immutable review evidence bound to the exact plan and observed-state checksums.',
          },
        },
        requiredConfirmInputText: material.reviewMaterialChecksum,
        confirmLabel: decision === 'NO_GO' ? 'Record NO_GO' : 'Record GO',
      })
    );
    if (!confirmed) {
      return;
    }
    await this.runAction(`review-${decision.toLowerCase()}`, async () => {
      await firstValueFrom(
        this.service.recordReviewAdmissionDecision(this.planId, {
          expectedPlanChecksum: material.planChecksum,
          reviewMaterialChecksum: material.reviewMaterialChecksum,
          observedStateChecksum: material.observedStateChecksum,
          decision,
          reason: values.reason.trim(),
          expiresAt: new Date(values.expiresAt).toISOString(),
          supersedesDecisionId: latest?.id,
          revokesDecisionId: decision === 'NO_GO' && material.state === 'GO' ? latest?.id : undefined,
        })
      );
      this.reviewDecisionForm.controls.reason.reset('');
      this.loadReviewMaterial();
      this.loadReviewDecisionHistory();
    });
  }

  protected async createPreviousState() {
    if (this.previousStateForm.invalid || !this.previousStateEnabled()) {
      return;
    }
    const request = this.previousStateForm.getRawValue();
    const confirmed = await firstValueFrom(
      this.overlay.confirm({
        message: {
          message: `Create a new previous-state plan from ${request.successfulDeploymentPlanId}?`,
          alert: {
            type: 'danger',
            message: 'This creates a new immutable plan. It does not rewrite the current plan or its history.',
          },
        },
        requiredConfirmInputText: request.successfulDeploymentPlanId,
        confirmLabel: 'Create previous-state plan',
      })
    );
    if (!confirmed) {
      return;
    }
    await this.runAction('previous-state', async () => {
      const result = await firstValueFrom(this.service.createPreviousStatePlan(this.planId, request));
      await this.router.navigate(['/deployments/plans', result.id], {queryParams: this.deploymentUnitQuery()});
    });
  }

  protected async evaluateAdmission() {
    const detail = this.detail();
    if (!detail || !this.admissionEnabled()) {
      return;
    }
    const planId = this.planId;
    const planLoadGeneration = this.planLoadGeneration;
    const confirmed = await firstValueFrom(
      this.overlay.confirm({
        message: {
          message: `Evaluate deployment admission for plan ${planId}?`,
          alert: {
            type: 'warning',
            message:
              'This appends a new admission decision bound to the current immutable plan and live gate evidence.',
          },
        },
        requiredConfirmInputText: detail.plan.canonicalChecksum,
        confirmLabel: 'Evaluate admission',
      })
    );
    if (!confirmed) {
      return;
    }
    if (planLoadGeneration !== this.planLoadGeneration || this.planId !== planId) {
      this.actionError.set(
        'The selected plan changed while confirmation was open. Review and confirm the current plan.'
      );
      return;
    }
    await this.runAction('admission', async () => {
      this.admissionEvaluation.set(await firstValueFrom(this.service.admitDeploymentPlan(planId)));
      this.loadReviewMaterial();
    });
  }

  protected async adoptBaseline() {
    const detail = this.detail();
    if (!detail || this.baselineAdoptionForm.invalid || !this.baselineAdoptionEnabled()) {
      return;
    }
    let components: OperatorBaselineAdoptionComponentRequest[];
    try {
      components = this.parseBaselineAdoptionComponents(this.baselineAdoptionForm.controls.components.value);
    } catch {
      this.actionError.set('Baseline component evidence must be a non-empty JSON array with every documented field.');
      return;
    }
    const reason = this.baselineAdoptionForm.controls.reason.value.trim();
    const confirmed = await firstValueFrom(
      this.overlay.confirm({
        message: {
          message: `Adopt the independently observed runtime as the baseline for plan ${this.planId}?`,
          alert: {
            type: 'danger',
            message: 'This marks the READY plan EXECUTED without creating deployment tasks, locks, or executions.',
          },
        },
        requiredConfirmInputText: detail.plan.canonicalChecksum,
        confirmLabel: 'Adopt baseline',
      })
    );
    if (!confirmed) {
      return;
    }
    await this.runAction('baseline-adoption', async () => {
      const adoption = await firstValueFrom(
        this.service.createBaselineAdoption(this.planId, {
          reason,
          expectedPlanChecksum: detail.plan.canonicalChecksum,
          expectedProductReleaseChecksum: detail.productReleaseChecksum,
          expectedTargetConfigChecksum: detail.targetConfigChecksum,
          components,
        })
      );
      this.baselineAdoption.set(adoption);
      this.load();
    });
  }

  protected factSections(): PlanFactSection[] {
    const detail = this.detail();
    if (!detail) {
      return [];
    }
    return [
      {title: 'Targets', facts: detail.targets},
      {title: 'Baselines', facts: detail.baselines},
      {title: 'Configuration', facts: detail.config},
      {title: 'Provider requirements', facts: detail.requirements},
      {title: 'Migrations', facts: detail.migrations},
      {title: 'Changes', facts: detail.changes},
      {title: 'Risks', facts: detail.risks},
      {title: 'Approvals', facts: detail.approvals},
      {title: 'Windows', facts: detail.windows},
      {title: 'Adapters', facts: detail.adapters},
      {title: 'Intent blockers', facts: detail.intentBlockers},
      {title: 'Steps', facts: detail.steps},
      {title: 'Graph edges', facts: detail.edges},
      {title: 'Issues', facts: detail.issues},
    ];
  }

  protected isPartial(): boolean {
    const detail = this.detail();
    return (
      detail !== null &&
      [
        detail.plan.canonicalChecksum,
        detail.productReleaseChecksum,
        detail.targetConfigChecksum,
        detail.effectivePolicyChecksum,
        detail.subscriberSetChecksum,
        detail.graphChecksum,
        detail.changeChecksum,
        detail.baselineChecksum,
        detail.providerResolutionChecksum,
        detail.migrationChecksum,
        detail.riskChecksum,
        detail.approvalChecksum,
        detail.windowChecksum,
        detail.adapterChecksum,
        detail.intentChecksum,
      ].some((checksum) => !checksum)
    );
  }

  protected statusLabel(): string {
    const status = this.detail()?.plan.status ?? '';
    if (status === 'STALE') {
      return 'Stale plan';
    }
    if (status === 'DISABLED') {
      return 'Plan disabled';
    }
    return knownPlanStatuses.has(status) ? status : `Unknown status: ${status || 'missing'}`;
  }

  protected actionsEnabled(): boolean {
    const status = this.detail()?.plan.status;
    return status !== undefined && actionablePlanStatuses.has(status) && !this.isPartial();
  }

  protected admissionEnabled(): boolean {
    return this.detail()?.plan.status === 'READY' && !this.isPartial() && !this.actionLoading();
  }

  protected baselineAdoptionEnabled(): boolean {
    return this.admissionEnabled();
  }

  protected previousStateEnabled(): boolean {
    const status = this.detail()?.plan.status;
    return status !== undefined && previousStatePlanStatuses.has(status) && !this.isPartial() && !this.actionLoading();
  }

  protected hasEnabledPlanAction(): boolean {
    return this.actionsEnabled() || this.previousStateEnabled();
  }

  protected reviewDecisionStateLabel(): string {
    switch (this.reviewMaterial()?.state) {
      case 'GO':
        return 'Current GO';
      case 'NO_GO':
        return 'Current NO_GO';
      case 'STALE':
        return 'Stale review decision';
      case 'MISSING':
        return 'No review decision';
      default:
        return 'Review state unavailable';
    }
  }

  protected reviewDecisionEnabled(): boolean {
    return this.reviewMaterial()?.canDecide === true && !this.reviewMaterialLoading();
  }

  protected reviewDecisionHistoryStatus(decision: OperatorReviewAdmissionDecision): string {
    const history = this.reviewDecisionHistory();
    const revoker = history.find((candidate) => candidate.revokesDecisionId === decision.id);
    if (revoker) {
      return `Revoked by ${revoker.id}`;
    }
    const superseder = history.find((candidate) => candidate.supersedesDecisionId === decision.id);
    if (superseder) {
      return `Superseded by ${superseder.id}`;
    }
    const material = this.reviewMaterial();
    if (
      material &&
      (decision.planChecksum !== material.planChecksum ||
        decision.reviewMaterialChecksum !== material.reviewMaterialChecksum ||
        decision.observedStateChecksum !== material.observedStateChecksum)
    ) {
      return 'Invalidated by material change';
    }
    if (new Date(decision.expiresAt).getTime() <= Date.now()) {
      return 'Expired';
    }
    if (material?.latestDecision?.id === decision.id && material.state === 'STALE') {
      return 'Invalidated';
    }
    return material?.latestDecision?.id === decision.id ? 'Current tip' : 'Historical';
  }

  protected isForbidden(): boolean {
    return this.error()?.status === 403;
  }

  protected isNotFound(): boolean {
    return this.error()?.status === 404;
  }

  protected deploymentUnitQuery(): Record<string, string> {
    const deploymentUnitId =
      this.route.snapshot.queryParamMap.get('deploymentUnitId')?.trim() || this.detail()?.plan.deploymentUnitId?.trim();
    return deploymentUnitId ? {deploymentUnitId} : {};
  }

  private async runAction(key: string, action: () => Promise<void>) {
    this.actionLoading.set(key);
    this.actionError.set(null);
    try {
      await action();
    } catch (error) {
      this.actionError.set(this.errorMessage(error));
    } finally {
      this.actionLoading.set(null);
    }
  }

  private errorMessage(error: unknown): string {
    if (error && typeof error === 'object' && 'message' in error && typeof error.message === 'string') {
      return error.message;
    }
    return 'The plan action could not be completed. Refresh and try again.';
  }

  private parseBaselineAdoptionComponents(value: string): OperatorBaselineAdoptionComponentRequest[] {
    const parsed: unknown = JSON.parse(value);
    if (!Array.isArray(parsed) || parsed.length === 0) {
      throw new Error('baseline adoption components are required');
    }
    return parsed.map((candidate) => {
      if (!candidate || typeof candidate !== 'object' || Array.isArray(candidate)) {
        throw new Error('baseline adoption component must be an object');
      }
      const record = candidate as Record<string, unknown>;
      if (
        Object.keys(record).some(
          (key) => !baselineAdoptionComponentKeys.includes(key as keyof OperatorBaselineAdoptionComponentRequest)
        ) ||
        baselineAdoptionComponentKeys.some(
          (key) => typeof record[key] !== 'string' || String(record[key]).trim() === ''
        )
      ) {
        throw new Error('baseline adoption component fields are invalid');
      }
      return Object.fromEntries(
        baselineAdoptionComponentKeys.map((key) => [key, String(record[key]).trim()])
      ) as unknown as OperatorBaselineAdoptionComponentRequest;
    });
  }
}
