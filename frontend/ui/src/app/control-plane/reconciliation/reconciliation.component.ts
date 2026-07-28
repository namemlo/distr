import {ChangeDetectionStrategy, Component, inject, signal} from '@angular/core';
import {FormBuilder, ReactiveFormsModule, Validators} from '@angular/forms';
import {firstValueFrom} from 'rxjs';
import {OperatorControlPlaneService} from '../../services/operator-control-plane.service';
import {OverlayService} from '../../services/overlay.service';
import {
  OperatorEvidenceRef,
  OperatorPlanFact,
  OperatorReconciliationDecisionRequest,
  OperatorReconciliationDetail,
  OperatorReconciliationRow,
} from '../../types/operator-control-plane';

const reconciliationPageSize = 25;
const knownReconciliationStates = new Set(['OPEN', 'ASSIGNED', 'EXCEPTION', 'RESOLVED']);
type ReconciliationAction = OperatorReconciliationDecisionRequest['action'];

@Component({
  selector: 'app-reconciliation',
  imports: [ReactiveFormsModule],
  templateUrl: './reconciliation.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
})
export class ReconciliationComponent {
  private readonly service = inject(OperatorControlPlaneService);
  private readonly overlay = inject(OverlayService);
  private readonly fb = inject(FormBuilder).nonNullable;

  protected readonly filterForm = this.fb.group({
    status: this.fb.control(''),
    drift: this.fb.control(''),
  });
  protected readonly resolutionForm = this.fb.group({
    action: this.fb.control<ReconciliationAction>('RESTORE_DESIRED', Validators.required),
    reason: this.fb.control('', [Validators.required, Validators.maxLength(2048)]),
    deploymentPlanId: this.fb.control(''),
    outcomeObservationId: this.fb.control(''),
    acceptedUntil: this.fb.control(''),
  });
  protected readonly cases = signal<OperatorReconciliationRow[]>([]);
  protected readonly detail = signal<OperatorReconciliationDetail | undefined>(undefined);
  protected readonly evidence = signal<OperatorEvidenceRef[]>([]);
  protected readonly nextCursor = signal<string | undefined>(undefined);
  protected readonly loading = signal(true);
  protected readonly loadingMore = signal(false);
  protected readonly detailLoading = signal(false);
  protected readonly resolving = signal(false);
  protected readonly listForbidden = signal(false);
  protected readonly detailForbidden = signal(false);
  protected readonly mutationDenied = signal(false);
  protected readonly detailNotFound = signal(false);
  protected readonly stale = signal(false);
  protected readonly evidencePartial = signal(false);
  protected readonly listError = signal('');
  protected readonly detailError = signal('');
  protected readonly resolutionError = signal('');

  constructor() {
    void this.loadCases(true);
  }

  protected async applyFilters(): Promise<void> {
    await this.loadCases(true);
  }

  protected async retryList(): Promise<void> {
    await this.loadCases(true);
  }

  protected async loadMore(): Promise<void> {
    if (!this.nextCursor() || this.loadingMore()) return;
    await this.loadCases(false);
  }

  protected async openReconciliation(reconciliationId: string): Promise<void> {
    await this.loadSelected(reconciliationId);
  }

  protected async resolve(): Promise<void> {
    const detail = this.detail();
    if (!detail || !this.canResolve()) return;
    const request = this.resolutionRequest();
    const actionLabel = this.actionLabel(request.action);
    const confirmed = await firstValueFrom(
      this.overlay.confirm({
        message: {
          message: `${actionLabel} for ${detail.reconciliation.component}?`,
          alert: {
            type: request.action === 'ACCEPT_DEVIATION' ? 'danger' : 'warning',
            message:
              request.action === 'ACCEPT_DEVIATION'
                ? 'This temporarily accepts observed drift until the recorded expiry.'
                : 'The server records this action and its evidence against the current drift case.',
          },
        },
        confirmLabel: 'Resolve case',
      })
    );
    if (!confirmed) return;

    this.resolving.set(true);
    this.stale.set(false);
    this.resolutionError.set('');
    try {
      await firstValueFrom(this.service.resolveDriftCase(detail.reconciliation.driftCaseId, request));
      await Promise.all([this.loadCases(true), this.loadSelected(detail.reconciliation.id)]);
    } catch (error) {
      const status = errorStatus(error);
      if (status === 403) {
        this.mutationDenied.set(true);
      } else if (status === 409) {
        this.stale.set(true);
        await this.loadSelected(detail.reconciliation.id);
      } else if (status === 404) {
        this.detailNotFound.set(true);
        this.detail.set(undefined);
      } else {
        this.resolutionError.set('Could not resolve the reconciliation case. Try again.');
      }
    } finally {
      this.resolving.set(false);
    }
  }

  protected canResolve(): boolean {
    const detail = this.detail();
    if (
      !detail ||
      !['OPEN', 'ASSIGNED', 'EXCEPTION'].includes(detail.reconciliation.status) ||
      this.mutationDenied() ||
      this.resolving()
    ) {
      return false;
    }
    const value = this.resolutionForm.getRawValue();
    const reason = value.reason.trim();
    if (!reason || reason.length > 2048 || /[\r\n]/.test(value.reason)) return false;
    if (value.action === 'CREATE_PLAN') return Boolean(value.deploymentPlanId.trim());
    if (value.action === 'RESTORE_DESIRED' || value.action === 'CLOSE_WITH_EVIDENCE') {
      return Boolean(value.outcomeObservationId.trim());
    }
    if (value.action === 'ACCEPT_DEVIATION') {
      const acceptedUntil = Date.parse(value.acceptedUntil);
      return Number.isFinite(acceptedUntil) && acceptedUntil > Date.now();
    }
    return false;
  }

  protected statusLabel(status: string): string {
    return knownReconciliationStates.has(status) ? status : `Unknown (${status || 'empty'})`;
  }

  protected actionLabel(action: ReconciliationAction): string {
    switch (action) {
      case 'RESTORE_DESIRED':
        return 'Restore desired state';
      case 'CREATE_PLAN':
        return 'Create replacement plan';
      case 'ACCEPT_DEVIATION':
        return 'Accept bounded deviation';
      case 'CLOSE_WITH_EVIDENCE':
        return 'Close with evidence';
    }
  }

  protected factValue(fact: OperatorPlanFact | undefined): string {
    if (!fact) return 'Unavailable';
    return fact.actual || fact.expected || fact.message || fact.status || fact.checksum || fact.id || 'Unavailable';
  }

  private async loadCases(reset: boolean): Promise<void> {
    if (reset) {
      this.loading.set(true);
      this.listForbidden.set(false);
      this.listError.set('');
    } else {
      this.loadingMore.set(true);
    }
    const filters = this.filterForm.getRawValue();
    const cursor = reset ? undefined : this.nextCursor();
    try {
      const page = await firstValueFrom(
        this.service.listReconciliation({
          limit: reconciliationPageSize,
          ...(filters.status.trim() ? {status: filters.status.trim()} : {}),
          ...(filters.drift.trim() ? {drift: filters.drift.trim()} : {}),
          ...(cursor ? {cursor} : {}),
        })
      );
      this.cases.update((current) => (reset ? page.items : [...current, ...page.items]));
      this.nextCursor.set(page.nextCursor);
    } catch (error) {
      if (reset) this.cases.set([]);
      if (errorStatus(error) === 403) {
        this.listForbidden.set(true);
      } else {
        this.listError.set('Could not load reconciliation cases. Try again.');
      }
    } finally {
      this.loading.set(false);
      this.loadingMore.set(false);
    }
  }

  private async loadSelected(reconciliationId: string): Promise<void> {
    this.detailLoading.set(true);
    this.detailForbidden.set(false);
    this.detailNotFound.set(false);
    this.detailError.set('');
    this.resolutionError.set('');
    this.evidencePartial.set(false);
    const [detailResult, evidenceResult] = await Promise.allSettled([
      firstValueFrom(this.service.getReconciliation(reconciliationId)),
      firstValueFrom(this.service.getReconciliationEvidence(reconciliationId)),
    ]);

    if (detailResult.status === 'fulfilled') {
      this.detail.set(detailResult.value.detail);
    } else {
      this.detail.set(undefined);
      const status = errorStatus(detailResult.reason);
      if (status === 403) {
        this.detailForbidden.set(true);
        this.mutationDenied.set(true);
      } else if (status === 404) {
        this.detailNotFound.set(true);
      } else {
        this.detailError.set('Could not load reconciliation detail. Try again.');
      }
    }

    if (evidenceResult.status === 'fulfilled') {
      this.evidence.set(evidenceResult.value.items);
    } else {
      this.evidence.set([]);
      this.evidencePartial.set(true);
    }
    this.detailLoading.set(false);
  }

  private resolutionRequest(): OperatorReconciliationDecisionRequest {
    const value = this.resolutionForm.getRawValue();
    const request: OperatorReconciliationDecisionRequest = {
      action: value.action,
      reason: value.reason.trim(),
    };
    if (value.action === 'CREATE_PLAN') request.deploymentPlanId = value.deploymentPlanId.trim();
    if (value.action === 'RESTORE_DESIRED' || value.action === 'CLOSE_WITH_EVIDENCE') {
      request.outcomeObservationId = value.outcomeObservationId.trim();
    }
    if (value.action === 'ACCEPT_DEVIATION') {
      request.acceptedUntil = new Date(value.acceptedUntil).toISOString();
    }
    return request;
  }
}

function errorStatus(error: unknown): number {
  return typeof error === 'object' && error !== null && 'status' in error && typeof error.status === 'number'
    ? error.status
    : 0;
}
