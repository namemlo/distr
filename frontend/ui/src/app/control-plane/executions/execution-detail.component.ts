import {DatePipe} from '@angular/common';
import {ChangeDetectionStrategy, Component, DestroyRef, inject, signal} from '@angular/core';
import {takeUntilDestroyed} from '@angular/core/rxjs-interop';
import {FormBuilder, ReactiveFormsModule, Validators} from '@angular/forms';
import {ActivatedRoute} from '@angular/router';
import {OperatorControlPlaneService} from '../../services/operator-control-plane.service';
import {
  OperatorControlPlaneError,
  OperatorEvidenceRef,
  OperatorExecutionDetail,
  OperatorPlanFact,
} from '../../types/operator-control-plane';

const knownFactStatuses = new Set([
  'PENDING',
  'CLAIMED',
  'RUNNING',
  'SUCCEEDED',
  'FAILED',
  'CANCELED',
  'TIMED_OUT',
  'FENCED',
  'UNKNOWN',
  'VERIFIED',
  'APPLIED',
  'REJECTED',
]);

@Component({
  templateUrl: './execution-detail.component.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [DatePipe, ReactiveFormsModule],
})
export class ExecutionDetailComponent {
  private readonly service = inject(OperatorControlPlaneService);
  private readonly route = inject(ActivatedRoute);
  private readonly destroyRef = inject(DestroyRef);
  private readonly fb = inject(FormBuilder).nonNullable;
  private readonly executionId = this.route.snapshot.paramMap.get('executionId') ?? '';

  protected readonly detail = signal<OperatorExecutionDetail | undefined>(undefined);
  protected readonly evidence = signal<OperatorEvidenceRef[]>([]);
  protected readonly loading = signal(true);
  protected readonly evidenceLoading = signal(true);
  protected readonly detailError = signal<'forbidden' | 'not-found' | 'failed' | undefined>(undefined);
  protected readonly evidencePartial = signal(false);
  protected readonly cancelSubmitting = signal(false);
  protected readonly statusSubmitting = signal(false);
  protected readonly cancelMessage = signal('');
  protected readonly statusMessage = signal('');

  protected readonly cancelForm = this.fb.group({
    reason: this.fb.control('', [Validators.required, Validators.maxLength(2048)]),
    confirmation: this.fb.control('', [Validators.required]),
  });
  protected readonly statusForm = this.fb.group({
    reason: this.fb.control('', [Validators.required, Validators.maxLength(2048)]),
    expiresInSeconds: this.fb.control(60, [Validators.required, Validators.min(30), Validators.max(3600)]),
  });

  private cancelIntentSignature = '';
  private cancelIntentKey = '';
  private statusIntentSignature = '';
  private statusIntentKey = '';

  constructor() {
    this.loadDetail();
    this.loadEvidence();
  }

  protected canSubmitCancel(): boolean {
    return Boolean(
      this.detail()?.execution.cancellable &&
      this.cancelForm.valid &&
      this.cancelForm.controls.confirmation.value === this.executionId &&
      !this.cancelSubmitting()
    );
  }

  protected canSubmitStatusQuery(): boolean {
    return Boolean(this.detail() && this.statusForm.valid && !this.statusSubmitting());
  }

  protected submitCancel(): void {
    if (!this.canSubmitCancel()) {
      return;
    }
    const reason = this.cancelForm.controls.reason.value.trim();
    const signature = `${this.executionId}\n${reason}`;
    if (signature !== this.cancelIntentSignature) {
      this.cancelIntentSignature = signature;
      this.cancelIntentKey = this.newIntentKey();
    }

    this.cancelSubmitting.set(true);
    this.cancelMessage.set('');
    this.service
      .cancelExecution(this.executionId, {reason, idempotencyKey: this.cancelIntentKey})
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: () => {
          this.cancelSubmitting.set(false);
          this.cancelMessage.set('Cancellation requested.');
          this.cancelIntentSignature = '';
          this.cancelIntentKey = '';
        },
        error: () => {
          this.cancelSubmitting.set(false);
          this.cancelMessage.set('Cancellation request failed. Retry keeps the same request key.');
        },
      });
  }

  protected submitStatusQuery(): void {
    if (!this.canSubmitStatusQuery()) {
      return;
    }
    const values = this.statusForm.getRawValue();
    const reason = values.reason.trim();
    const signature = `${this.executionId}\n${reason}\n${values.expiresInSeconds}`;
    if (signature !== this.statusIntentSignature) {
      this.statusIntentSignature = signature;
      this.statusIntentKey = this.newIntentKey();
    }

    this.statusSubmitting.set(true);
    this.statusMessage.set('');
    this.service
      .requestExecutionStatus(this.executionId, {
        reason,
        expiresInSeconds: values.expiresInSeconds,
        idempotencyKey: this.statusIntentKey,
      })
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: () => {
          this.statusSubmitting.set(false);
          this.statusMessage.set('Current status requested.');
          this.statusIntentSignature = '';
          this.statusIntentKey = '';
        },
        error: () => {
          this.statusSubmitting.set(false);
          this.statusMessage.set('Status request failed. Retry keeps the same request key.');
        },
      });
  }

  protected planExecutionsHref(): string {
    return `/deployments/executions?deploymentPlanId=${encodeURIComponent(
      this.detail()?.execution.deploymentPlanId ?? ''
    )}`;
  }

  protected factStatusLabel(fact: OperatorPlanFact): string {
    if (!fact.status) {
      return 'Unknown';
    }
    return knownFactStatuses.has(fact.status) ? fact.status : `Unknown status: ${fact.status}`;
  }

  protected isStale(): boolean {
    return this.detail()?.execution.observation === 'STALE';
  }

  protected coordinationFacts(detail: OperatorExecutionDetail): OperatorPlanFact[] {
    return [...detail.tasks, ...detail.steps, ...detail.observations].filter((fact) =>
      /lock|lease|fenc|admission|pause/i.test(`${fact.key} ${fact.kind ?? ''} ${fact.message ?? ''}`)
    );
  }

  private loadDetail(): void {
    if (!this.executionId) {
      this.detailError.set('not-found');
      this.loading.set(false);
      return;
    }
    this.service
      .getExecution(this.executionId)
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: (response) => {
          this.detail.set(response.detail);
          this.loading.set(false);
        },
        error: (error: OperatorControlPlaneError) => {
          if (error?.status === 403 || error?.code === 'FORBIDDEN') this.detailError.set('forbidden');
          else if (error?.status === 404 || error?.code === 'NOT_FOUND') this.detailError.set('not-found');
          else this.detailError.set('failed');
          this.loading.set(false);
        },
      });
  }

  private loadEvidence(): void {
    if (!this.executionId) {
      this.evidenceLoading.set(false);
      return;
    }
    this.service
      .getExecutionEvidence(this.executionId)
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: (page) => {
          this.evidence.set(page.items);
          this.evidenceLoading.set(false);
        },
        error: () => {
          this.evidencePartial.set(true);
          this.evidenceLoading.set(false);
        },
      });
  }

  private newIntentKey(): string {
    return globalThis.crypto?.randomUUID?.() ?? `execution-${Date.now()}-${Math.random().toString(36).slice(2)}`;
  }
}
