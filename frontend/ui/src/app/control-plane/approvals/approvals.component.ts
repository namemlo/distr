import {ChangeDetectionStrategy, Component, DestroyRef, inject, signal} from '@angular/core';
import {takeUntilDestroyed} from '@angular/core/rxjs-interop';
import {FormBuilder, ReactiveFormsModule, Validators} from '@angular/forms';
import {ActivatedRoute} from '@angular/router';
import {distinctUntilChanged, firstValueFrom, map} from 'rxjs';
import {OperatorControlPlaneService} from '../../services/operator-control-plane.service';
import {OverlayService} from '../../services/overlay.service';
import {
  OperatorApprovalDecisionRequest,
  OperatorApprovalRequest,
  OperatorApprovalRequirement,
} from '../../types/operator-control-plane';

const approvalPageSize = 25;
const knownApprovalStates = new Set(['PENDING', 'APPROVED', 'REJECTED', 'EXPIRED', 'SUPERSEDED', 'INVALIDATED']);

@Component({
  selector: 'app-approvals',
  imports: [ReactiveFormsModule],
  templateUrl: './approvals.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
})
export class ApprovalsComponent {
  private readonly service = inject(OperatorControlPlaneService);
  private readonly overlay = inject(OverlayService);
  private readonly fb = inject(FormBuilder).nonNullable;
  private readonly route = inject(ActivatedRoute);
  private readonly destroyRef = inject(DestroyRef);
  private readonly idempotencyKeys = new Map<string, string>();
  private fallbackKeySequence = 0;

  protected readonly filterForm = this.fb.group({
    state: this.fb.control(''),
  });
  protected readonly decisionComment = this.fb.control('', [Validators.required, Validators.maxLength(4096)]);
  protected readonly approvals = signal<OperatorApprovalRequest[]>([]);
  protected readonly selected = signal<OperatorApprovalRequest | undefined>(undefined);
  protected readonly nextCursor = signal<string | undefined>(undefined);
  protected readonly loading = signal(true);
  protected readonly loadingMore = signal(false);
  protected readonly detailLoading = signal(false);
  protected readonly deciding = signal(false);
  protected readonly listForbidden = signal(false);
  protected readonly detailForbidden = signal(false);
  protected readonly decisionDenied = signal(false);
  protected readonly detailNotFound = signal(false);
  protected readonly stale = signal(false);
  protected readonly listError = signal('');
  protected readonly detailError = signal('');
  protected readonly decisionError = signal('');

  constructor() {
    void this.loadApprovals(true);
    this.route.queryParamMap
      .pipe(
        map((params) => params.get('requestId')?.trim() ?? ''),
        distinctUntilChanged(),
        takeUntilDestroyed(this.destroyRef)
      )
      .subscribe((requestId) => {
        if (requestId) {
          void this.openApproval(requestId);
        }
      });
  }

  protected async applyFilters(): Promise<void> {
    await this.loadApprovals(true);
  }

  protected async retryList(): Promise<void> {
    await this.loadApprovals(true);
  }

  protected async loadMore(): Promise<void> {
    if (!this.nextCursor() || this.loadingMore()) return;
    await this.loadApprovals(false);
  }

  protected async openApproval(approvalRequestId: string): Promise<void> {
    this.detailLoading.set(true);
    this.detailForbidden.set(false);
    this.detailNotFound.set(false);
    this.detailError.set('');
    this.decisionError.set('');
    try {
      this.selected.set(await firstValueFrom(this.service.getApproval(approvalRequestId)));
    } catch (error) {
      this.selected.set(undefined);
      const status = errorStatus(error);
      if (status === 403) {
        this.detailForbidden.set(true);
      } else if (status === 404) {
        this.detailNotFound.set(true);
      } else {
        this.detailError.set('Could not load approval detail. Try again.');
      }
    } finally {
      this.detailLoading.set(false);
    }
  }

  protected async decide(
    requirement: OperatorApprovalRequirement,
    decision: OperatorApprovalDecisionRequest['decision']
  ): Promise<void> {
    const approval = this.selected();
    if (!approval || !this.canDecide()) return;
    const comment = this.decisionComment.value.trim();
    const confirmed = await firstValueFrom(
      this.overlay.confirm({
        message: {
          message: `${decision === 'APPROVE' ? 'Approve' : 'Reject'} this checksum-bound request?`,
          alert: {
            type: decision === 'APPROVE' ? 'warning' : 'danger',
            message: `The decision is recorded against request revision ${approval.revision} and cannot be edited.`,
          },
        },
        confirmLabel: decision === 'APPROVE' ? 'Approve' : 'Reject',
      })
    );
    if (!confirmed) return;

    const keyScope = `${approval.id}:${approval.revision}:${requirement.id}:${decision}`;
    const request: OperatorApprovalDecisionRequest = {
      approvalRequirementId: requirement.id,
      decision,
      comment,
      expectedRequestRevision: approval.revision,
      idempotencyKey: this.idempotencyKey(keyScope),
    };

    this.deciding.set(true);
    this.stale.set(false);
    this.decisionError.set('');
    try {
      await firstValueFrom(this.service.decideApproval(approval.id, request));
      this.decisionComment.reset();
      await Promise.all([this.loadApprovals(true), this.openApproval(approval.id)]);
    } catch (error) {
      const status = errorStatus(error);
      if (status === 403) {
        this.decisionDenied.set(true);
      } else if (status === 409) {
        this.stale.set(true);
        await this.openApproval(approval.id);
      } else if (status === 404) {
        this.detailNotFound.set(true);
        this.selected.set(undefined);
      } else {
        this.decisionError.set('Could not record the approval decision. Try again.');
      }
    } finally {
      this.deciding.set(false);
    }
  }

  protected canDecide(): boolean {
    const approval = this.selected();
    if (
      !approval ||
      approval.state !== 'PENDING' ||
      this.isExpired(approval) ||
      this.decisionDenied() ||
      this.deciding()
    ) {
      return false;
    }
    const comment = this.decisionComment.value.trim();
    return comment.length > 0 && comment.length <= 4096;
  }

  protected isInvalidated(approval: OperatorApprovalRequest): boolean {
    return approval.state === 'INVALIDATED' || Boolean(approval.invalidatedAt || approval.invalidationReason);
  }

  protected approvalEvidencePartial(approval: OperatorApprovalRequest): boolean {
    return (
      approval.requirements.length === 0 ||
      !approval.subjectChecksum ||
      !approval.effectivePolicyChecksum ||
      !approval.subscriberSetChecksum
    );
  }

  protected statusLabel(state: string): string {
    return knownApprovalStates.has(state) ? state : `Unknown (${state || 'empty'})`;
  }

  protected decisionFor(approval: OperatorApprovalRequest, requirementId: string): string {
    const decisions = approval.decisions.filter((decision) => decision.approvalRequirementId === requirementId);
    return decisions.length === 0 ? 'No decision recorded' : decisions.map((decision) => decision.decision).join(', ');
  }

  private async loadApprovals(reset: boolean): Promise<void> {
    if (reset) {
      this.loading.set(true);
      this.nextCursor.set(undefined);
      this.listForbidden.set(false);
      this.listError.set('');
    } else {
      this.loadingMore.set(true);
    }
    const state = this.filterForm.controls.state.value.trim();
    const cursor = reset ? undefined : this.nextCursor();
    try {
      const page = await firstValueFrom(
        this.service.listApprovals({
          limit: approvalPageSize,
          ...(state ? {state} : {}),
          ...(cursor ? {cursor} : {}),
        })
      );
      this.approvals.update((current) => (reset ? page.items : [...current, ...page.items]));
      this.nextCursor.set(page.nextCursor);
    } catch (error) {
      if (reset) {
        this.approvals.set([]);
        this.nextCursor.set(undefined);
      }
      if (errorStatus(error) === 403) {
        this.listForbidden.set(true);
      } else {
        this.listError.set('Could not load approvals. Try again.');
      }
    } finally {
      this.loading.set(false);
      this.loadingMore.set(false);
    }
  }

  private isExpired(approval: OperatorApprovalRequest): boolean {
    const expiresAt = Date.parse(approval.expiresAt);
    return Number.isFinite(expiresAt) && expiresAt <= Date.now();
  }

  private idempotencyKey(scope: string): string {
    const existing = this.idempotencyKeys.get(scope);
    if (existing) return existing;
    const key =
      globalThis.crypto?.randomUUID?.() ??
      `approval-${Date.now().toString(36)}-${(++this.fallbackKeySequence).toString(36)}`;
    this.idempotencyKeys.set(scope, key);
    return key;
  }
}

function errorStatus(error: unknown): number {
  return typeof error === 'object' && error !== null && 'status' in error && typeof error.status === 'number'
    ? error.status
    : 0;
}
