import {ChangeDetectionStrategy, Component, computed, inject, signal} from '@angular/core';
import {firstValueFrom} from 'rxjs';
import {OperatorControlPlaneService} from '../../services/operator-control-plane.service';
import {OperatorControlPlaneError, OperatorFleetRow} from '../../types/operator-control-plane';

type EvidenceState = 'complete' | 'partial' | 'stale' | 'unknown';

@Component({
  selector: 'app-fleet',
  templateUrl: './fleet.component.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class FleetComponent {
  private readonly controlPlane = inject(OperatorControlPlaneService);

  protected readonly rows = signal<OperatorFleetRow[]>([]);
  protected readonly nextCursor = signal<string | undefined>(undefined);
  protected readonly loading = signal(true);
  protected readonly loadingMore = signal(false);
  protected readonly error = signal('');
  protected readonly selectedTargetId = signal('');
  protected readonly sharedTargetRows = computed(() => {
    const targetId = this.selectedTargetId();
    return targetId ? this.rows().filter((row) => row.deploymentTargetId === targetId) : [];
  });

  constructor() {
    void this.load();
  }

  protected async load(): Promise<void> {
    this.loading.set(true);
    this.error.set('');
    this.selectedTargetId.set('');
    try {
      const page = await firstValueFrom(this.controlPlane.listFleet({limit: 50}));
      this.rows.set(page.items);
      this.nextCursor.set(page.nextCursor);
    } catch (error) {
      this.rows.set([]);
      this.nextCursor.set(undefined);
      this.error.set(this.errorMessage(error));
    } finally {
      this.loading.set(false);
    }
  }

  protected async loadMore(): Promise<void> {
    const cursor = this.nextCursor();
    if (!cursor || this.loadingMore()) return;

    this.loadingMore.set(true);
    this.error.set('');
    try {
      const page = await firstValueFrom(this.controlPlane.listFleet({cursor, limit: 50}));
      this.rows.update((current) => {
        const knownIds = new Set(current.map((row) => row.id));
        return [...current, ...page.items.filter((row) => !knownIds.has(row.id))];
      });
      this.nextCursor.set(page.nextCursor);
    } catch (error) {
      this.error.set(this.errorMessage(error));
    } finally {
      this.loadingMore.set(false);
    }
  }

  protected compareTarget(targetId: string): void {
    this.selectedTargetId.set(targetId);
  }

  protected placementCount(targetId: string): number {
    return this.rows().filter((row) => row.deploymentTargetId === targetId).length;
  }

  protected evidenceState(row: OperatorFleetRow): EvidenceState {
    const facts = [row.observedState, row.drift, row.enrollment, row.lastExecution].map((value) => value.toUpperCase());
    if (facts.some((value) => value.includes('UNKNOWN'))) return 'unknown';
    if (facts.some((value) => value.includes('STALE'))) return 'stale';
    if (!row.activeReleaseId || !row.activeRelease || !row.componentId) return 'partial';
    return 'complete';
  }

  protected evidenceLabel(row: OperatorFleetRow): string {
    switch (this.evidenceState(row)) {
      case 'partial':
        return 'Partial evidence';
      case 'stale':
        return 'Stale evidence';
      case 'unknown':
        return 'Unknown evidence';
      default:
        return 'Complete evidence';
    }
  }

  protected badgeClass(state: string): string {
    const value = state.toUpperCase();
    if (value.includes('FAIL') || value.includes('DRIFT') || value.includes('UNKNOWN')) {
      return 'border-red-200 bg-red-50 text-red-800 dark:border-red-900 dark:bg-red-950 dark:text-red-200';
    }
    if (value.includes('PENDING') || value.includes('PARTIAL') || value.includes('STALE')) {
      return 'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900 dark:bg-amber-950 dark:text-amber-200';
    }
    return 'border-green-200 bg-green-50 text-green-800 dark:border-green-900 dark:bg-green-950 dark:text-green-200';
  }

  private errorMessage(error: unknown): string {
    const status = (error as Partial<OperatorControlPlaneError> | null)?.status;
    if (status === 403) return 'You are not authorized to view the operator fleet.';
    if (status === 404) return 'The operator control plane is disabled for this organization.';
    return 'The fleet could not be loaded. Try again.';
  }
}
