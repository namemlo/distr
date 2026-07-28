import {DatePipe} from '@angular/common';
import {ChangeDetectionStrategy, Component, DestroyRef, inject, OnInit, signal} from '@angular/core';
import {takeUntilDestroyed} from '@angular/core/rxjs-interop';
import {FormBuilder, ReactiveFormsModule, Validators} from '@angular/forms';
import {ActivatedRoute, Router, RouterLink} from '@angular/router';
import {distinctUntilChanged, firstValueFrom, map} from 'rxjs';
import {OperatorControlPlaneService} from '../../services/operator-control-plane.service';
import {OverlayService} from '../../services/overlay.service';
import {
  OperatorControlPlaneError,
  OperatorEvidenceRef,
  OperatorPlanCompare,
  OperatorPlanDetail,
  OperatorPlanDraftValidation,
  OperatorPlanFact,
} from '../../types/operator-control-plane';

const actionablePlanStatuses = new Set(['DRAFT', 'VALIDATING', 'BLOCKED', 'READY', 'PUBLISHED']);
const knownPlanStatuses = new Set([...actionablePlanStatuses, 'EXPIRED', 'EXECUTED', 'STALE', 'DISABLED']);

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
  protected readonly loading = signal(true);
  protected readonly evidenceLoading = signal(true);
  protected readonly actionLoading = signal<string | null>(null);
  protected readonly error = signal<OperatorControlPlaneError | null>(null);
  protected readonly evidenceError = signal<OperatorControlPlaneError | null>(null);
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
  protected readonly previousStateForm = this.fb.nonNullable.group({
    successfulDeploymentPlanId: ['', Validators.required],
    reason: ['', [Validators.required, Validators.maxLength(2048)]],
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
    this.actionError.set(null);
    this.error.set(null);
    this.evidenceError.set(null);
  }

  protected load() {
    const generation = ++this.planLoadGeneration;
    this.loading.set(true);
    this.evidenceLoading.set(true);
    this.error.set(null);
    this.evidenceError.set(null);

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
      await this.router.navigate(['/deployments/plans', result.id]);
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

  protected async createPreviousState() {
    if (this.previousStateForm.invalid || !this.actionsEnabled()) {
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
      await this.router.navigate(['/deployments/plans', result.id]);
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
    return status !== undefined && actionablePlanStatuses.has(status);
  }

  protected isForbidden(): boolean {
    return this.error()?.status === 403;
  }

  protected isNotFound(): boolean {
    return this.error()?.status === 404;
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
}
