import {DatePipe} from '@angular/common';
import {ChangeDetectionStrategy, Component, inject, OnInit, signal} from '@angular/core';
import {FormBuilder, ReactiveFormsModule} from '@angular/forms';
import {ActivatedRoute, Router, RouterLink} from '@angular/router';
import {OperatorControlPlaneService} from '../../services/operator-control-plane.service';
import {OperatorControlPlaneError, OperatorPlanFilters, OperatorPlanRow} from '../../types/operator-control-plane';

const knownPlanStatuses = new Set(['DRAFT', 'VALIDATING', 'BLOCKED', 'READY', 'EXPIRED', 'EXECUTED', 'PUBLISHED']);

@Component({
  selector: 'app-control-plane-plan-list',
  templateUrl: './plan-list.component.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [DatePipe, ReactiveFormsModule, RouterLink],
})
export class PlanListComponent implements OnInit {
  private readonly service = inject(OperatorControlPlaneService);
  private readonly route = inject(ActivatedRoute);
  private readonly router = inject(Router);
  private readonly fb = inject(FormBuilder);

  protected readonly plans = signal<OperatorPlanRow[]>([]);
  protected readonly loading = signal(true);
  protected readonly loadingMore = signal(false);
  protected readonly error = signal<OperatorControlPlaneError | null>(null);
  protected readonly nextCursor = signal<string | undefined>(undefined);
  protected readonly total = signal<number | undefined>(undefined);

  protected readonly filters = this.fb.nonNullable.group({
    status: this.route.snapshot.queryParamMap.get('status') ?? '',
    environmentId: this.route.snapshot.queryParamMap.get('environmentId') ?? '',
    deploymentUnitId: this.route.snapshot.queryParamMap.get('deploymentUnitId') ?? '',
    productReleaseId: this.route.snapshot.queryParamMap.get('productReleaseId') ?? '',
  });

  ngOnInit() {
    this.load();
  }

  protected load(cursor?: string) {
    const append = cursor !== undefined;
    append ? this.loadingMore.set(true) : this.loading.set(true);
    this.error.set(null);
    this.service.listPlans(this.request(cursor)).subscribe({
      next: (page) => {
        this.plans.update((current) => (append ? [...current, ...page.items] : page.items));
        this.nextCursor.set(page.nextCursor);
        this.total.set(page.total);
        this.loading.set(false);
        this.loadingMore.set(false);
      },
      error: (error: OperatorControlPlaneError) => {
        this.error.set(error);
        this.loading.set(false);
        this.loadingMore.set(false);
      },
    });
  }

  protected loadMore() {
    const cursor = this.nextCursor();
    if (cursor && !this.loadingMore()) {
      this.load(cursor);
    }
  }

  protected applyFilters() {
    const queryParams = this.filterValues();
    void this.router.navigate([], {
      queryParams,
      queryParamsHandling: 'merge',
      replaceUrl: true,
    });
    this.plans.set([]);
    this.nextCursor.set(undefined);
    this.load();
  }

  protected clearFilters() {
    this.filters.reset({
      status: '',
      environmentId: '',
      deploymentUnitId: '',
      productReleaseId: '',
    });
    this.applyFilters();
  }

  protected hasPartialPlans(): boolean {
    return this.plans().some(
      (plan) =>
        !plan.id || !plan.productReleaseVersion || !plan.environment || !plan.deploymentUnit || !plan.canonicalChecksum
    );
  }

  protected statusLabel(status: string): string {
    if (status === 'STALE') {
      return 'Stale plan';
    }
    if (status === 'DISABLED') {
      return 'Plan disabled';
    }
    return knownPlanStatuses.has(status) ? status : `Unknown status: ${status || 'missing'}`;
  }

  protected isForbidden(): boolean {
    return this.error()?.status === 403;
  }

  private request(cursor?: string): OperatorPlanFilters {
    return {
      cursor,
      limit: 25,
      ...this.filterValues(),
    };
  }

  private filterValues(): Omit<OperatorPlanFilters, 'cursor' | 'limit'> {
    const value = this.filters.getRawValue();
    return {
      status: value.status || undefined,
      environmentId: value.environmentId || undefined,
      deploymentUnitId: value.deploymentUnitId || undefined,
      productReleaseId: value.productReleaseId || undefined,
    };
  }
}
