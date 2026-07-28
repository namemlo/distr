import {DatePipe} from '@angular/common';
import {ChangeDetectionStrategy, Component, DestroyRef, inject, signal} from '@angular/core';
import {takeUntilDestroyed} from '@angular/core/rxjs-interop';
import {FormBuilder, ReactiveFormsModule} from '@angular/forms';
import {ActivatedRoute} from '@angular/router';
import {OperatorControlPlaneService} from '../../services/operator-control-plane.service';
import {
  OperatorControlPlaneError,
  OperatorExecutionFilters,
  OperatorExecutionRow,
} from '../../types/operator-control-plane';

const knownExecutionStatuses = new Set([
  'PENDING',
  'CLAIMED',
  'RUNNING',
  'SUCCEEDED',
  'FAILED',
  'CANCELED',
  'TIMED_OUT',
  'FENCED',
  'UNKNOWN',
]);

@Component({
  templateUrl: './executions.component.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [DatePipe, ReactiveFormsModule],
})
export class ExecutionsComponent {
  private readonly service = inject(OperatorControlPlaneService);
  private readonly route = inject(ActivatedRoute);
  private readonly destroyRef = inject(DestroyRef);
  private readonly fb = inject(FormBuilder).nonNullable;

  protected readonly executions = signal<OperatorExecutionRow[]>([]);
  protected readonly loading = signal(true);
  protected readonly loadingMore = signal(false);
  protected readonly nextCursor = signal<string | undefined>(undefined);
  protected readonly activePlanId = signal('');
  protected readonly error = signal<'forbidden' | 'not-found' | 'retryable' | 'failed' | undefined>(undefined);
  protected readonly partialError = signal(false);

  protected readonly filterForm = this.fb.group({
    status: this.fb.control(''),
    campaignId: this.fb.control(''),
    deploymentTargetId: this.fb.control(''),
    from: this.fb.control(''),
    to: this.fb.control(''),
  });

  constructor() {
    this.route.queryParams.pipe(takeUntilDestroyed(this.destroyRef)).subscribe((params) => {
      this.activePlanId.set(typeof params['deploymentPlanId'] === 'string' ? params['deploymentPlanId'] : '');
      this.loadFirstPage();
    });
  }

  protected applyFilters(): void {
    this.loadFirstPage();
  }

  protected clearPlanScope(): void {
    this.activePlanId.set('');
    this.loadFirstPage();
  }

  protected loadNextPage(): void {
    const cursor = this.nextCursor();
    if (!cursor || this.loadingMore()) {
      return;
    }
    this.loadingMore.set(true);
    this.partialError.set(false);
    this.service
      .listExecutions({...this.filters(), cursor})
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: (page) => {
          this.executions.update((items) => [...items, ...page.items]);
          this.nextCursor.set(page.nextCursor);
          this.loadingMore.set(false);
        },
        error: () => {
          this.partialError.set(true);
          this.loadingMore.set(false);
        },
      });
  }

  protected statusLabel(status: string): string {
    if (!status) {
      return 'Unknown';
    }
    return knownExecutionStatuses.has(status) ? status : `Unknown status: ${status}`;
  }

  protected isStale(execution: OperatorExecutionRow): boolean {
    return execution.observation === 'STALE';
  }

  protected detailHref(executionId: string): string {
    return `/deployments/executions/${encodeURIComponent(executionId)}`;
  }

  private loadFirstPage(): void {
    this.loading.set(true);
    this.error.set(undefined);
    this.partialError.set(false);
    this.nextCursor.set(undefined);
    this.service
      .listExecutions(this.filters())
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: (page) => {
          this.executions.set(page.items);
          this.nextCursor.set(page.nextCursor);
          this.loading.set(false);
        },
        error: (error: OperatorControlPlaneError) => {
          this.executions.set([]);
          this.error.set(this.errorKind(error));
          this.loading.set(false);
        },
      });
  }

  private filters(): OperatorExecutionFilters {
    const values = this.filterForm.getRawValue();
    const filters: OperatorExecutionFilters = {limit: 50};
    const planId = this.activePlanId().trim();
    if (planId) filters.deploymentPlanId = planId;
    if (values.status.trim()) filters.status = values.status.trim();
    if (values.campaignId.trim()) filters.campaignId = values.campaignId.trim();
    if (values.deploymentTargetId.trim()) filters.deploymentTargetId = values.deploymentTargetId.trim();
    if (values.from.trim()) filters.from = new Date(values.from).toISOString();
    if (values.to.trim()) filters.to = new Date(values.to).toISOString();
    return filters;
  }

  private errorKind(error: OperatorControlPlaneError): 'forbidden' | 'not-found' | 'retryable' | 'failed' {
    if (error?.status === 403 || error?.code === 'FORBIDDEN') return 'forbidden';
    if (error?.status === 404 || error?.code === 'NOT_FOUND') return 'not-found';
    if (error?.retryable || error?.status === 0 || error?.status >= 500) return 'retryable';
    return 'failed';
  }
}
