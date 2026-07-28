import {ChangeDetectionStrategy, Component, inject, signal} from '@angular/core';
import {FormControl, ReactiveFormsModule} from '@angular/forms';
import {RouterLink} from '@angular/router';
import {firstValueFrom} from 'rxjs';
import {OperatorControlPlaneService} from '../../services/operator-control-plane.service';
import {OperatorCampaignFilters, OperatorCampaignRow} from '../../types/operator-control-plane';

@Component({
  selector: 'app-campaigns',
  imports: [ReactiveFormsModule, RouterLink],
  templateUrl: './campaigns.component.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class CampaignsComponent {
  private readonly controlPlane = inject(OperatorControlPlaneService);

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

  constructor() {
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
}

function errorStatus(error: unknown): number {
  return typeof error === 'object' && error !== null && 'status' in error && typeof error.status === 'number'
    ? error.status
    : 0;
}
