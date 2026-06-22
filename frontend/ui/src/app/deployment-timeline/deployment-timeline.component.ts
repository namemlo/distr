import {DatePipe} from '@angular/common';
import {ChangeDetectionStrategy, Component, inject, signal} from '@angular/core';
import {FormBuilder, ReactiveFormsModule} from '@angular/forms';
import {FontAwesomeModule} from '@fortawesome/angular-fontawesome';
import {
  faCheck,
  faClockRotateLeft,
  faEye,
  faListCheck,
  faMagnifyingGlass,
  faRotateRight,
  faShuffle,
  faTriangleExclamation,
} from '@fortawesome/free-solid-svg-icons';
import {firstValueFrom, startWith} from 'rxjs';
import {getFormDisplayedError} from '../../util/errors';
import {AutotrimDirective} from '../directives/autotrim.directive';
import {DeploymentTimelineService} from '../services/deployment-timeline.service';
import {OverlayService} from '../services/overlay.service';
import {ToastService} from '../services/toast.service';
import {
  DeploymentTaskStatus,
  DeploymentTimelineChangeKind,
  DeploymentTimelineComparison,
  DeploymentTimelineComponentChange,
  DeploymentTimelineItem,
  DeploymentTimelineStepChange,
  DeploymentTimelineVariableChange,
} from '../types/deployment-timeline';

const deployPreviousReleaseWarning =
  'Deploy previous release creates a new deployment plan for the selected release. It does not reverse external state or database changes.';

@Component({
  templateUrl: './deployment-timeline.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  imports: [ReactiveFormsModule, FontAwesomeModule, DatePipe, AutotrimDirective],
})
export class DeploymentTimelineComponent {
  protected readonly faCheck = faCheck;
  protected readonly faClockRotateLeft = faClockRotateLeft;
  protected readonly faEye = faEye;
  protected readonly faListCheck = faListCheck;
  protected readonly faMagnifyingGlass = faMagnifyingGlass;
  protected readonly faRotateRight = faRotateRight;
  protected readonly faShuffle = faShuffle;
  protected readonly faTriangleExclamation = faTriangleExclamation;

  private readonly deploymentTimelineService = inject(DeploymentTimelineService);
  private readonly overlay = inject(OverlayService);
  private readonly toast = inject(ToastService);
  private readonly fb = inject(FormBuilder).nonNullable;

  protected readonly timelineItems = signal<DeploymentTimelineItem[]>([]);
  protected readonly filteredTimelineItems = signal<DeploymentTimelineItem[]>([]);
  protected readonly comparison = signal<DeploymentTimelineComparison | undefined>(undefined);
  protected readonly selectedBaseTaskId = signal<string | undefined>(undefined);
  protected readonly selectedCompareTaskId = signal<string | undefined>(undefined);
  protected readonly loading = signal(true);
  protected readonly compareLoading = signal(false);
  protected readonly loadError = signal<string | undefined>(undefined);
  protected readonly compareError = signal<string | undefined>(undefined);
  protected readonly redeployLoadingTaskId = signal<string | undefined>(undefined);

  protected readonly statuses: Array<DeploymentTaskStatus | ''> = [
    '',
    'QUEUED',
    'RUNNING',
    'SUCCEEDED',
    'FAILED',
    'CANCELED',
  ];

  protected readonly filterForm = this.fb.group({
    search: this.fb.control(''),
    status: this.fb.control<DeploymentTaskStatus | ''>(''),
    includeNonTerminal: this.fb.control(true),
  });

  constructor() {
    this.filterForm.controls.search.valueChanges.pipe(startWith('')).subscribe(() => this.applyFilter());
    this.filterForm.controls.status.valueChanges.pipe(startWith('')).subscribe(() => this.applyFilter());
    this.filterForm.controls.includeNonTerminal.valueChanges.subscribe(() => this.load());
    this.load();
  }

  protected load() {
    this.loading.set(true);
    this.loadError.set(undefined);
    this.deploymentTimelineService
      .list({
        includeNonTerminal: this.filterForm.controls.includeNonTerminal.value,
        limit: 100,
      })
      .subscribe({
        next: (timeline) => {
          this.timelineItems.set(timeline.items);
          this.applyFilter();
          this.dropMissingSelections();
          this.loading.set(false);
        },
        error: (e) => {
          this.loadError.set(getFormDisplayedError(e) ?? 'Failed to load deployment timeline.');
          this.loading.set(false);
        },
      });
  }

  protected selectBase(taskId: string) {
    this.selectedBaseTaskId.set(taskId);
  }

  protected selectCompare(taskId: string) {
    this.selectedCompareTaskId.set(taskId);
  }

  protected canCompare(): boolean {
    return Boolean(this.selectedBaseTaskId() && this.selectedCompareTaskId());
  }

  protected async compare() {
    const baseTaskId = this.selectedBaseTaskId();
    const compareTaskId = this.selectedCompareTaskId();
    if (!baseTaskId || !compareTaskId) {
      return;
    }

    this.compareLoading.set(true);
    this.compareError.set(undefined);
    try {
      this.comparison.set(await firstValueFrom(this.deploymentTimelineService.compare(baseTaskId, compareTaskId)));
    } catch (e) {
      this.compareError.set(getFormDisplayedError(e) ?? 'Failed to compare deployments.');
    } finally {
      this.compareLoading.set(false);
    }
  }

  protected async deployPreviousRelease(item: DeploymentTimelineItem) {
    const confirmed = await firstValueFrom(
      this.overlay.confirm({
        message: {
          message: 'Deploy previous release?',
          alert: {
            type: 'warning',
            message: deployPreviousReleaseWarning,
          },
        },
        confirmLabel: 'Deploy previous release',
      })
    );
    if (!confirmed) {
      return;
    }

    this.redeployLoadingTaskId.set(item.taskId);
    try {
      const result = await firstValueFrom(this.deploymentTimelineService.redeploy(item.taskId));
      this.toast.success(`Deployment plan ${this.shortId(result.plan.id)} created`);
      this.load();
    } catch (e) {
      const msg = getFormDisplayedError(e);
      this.toast.error(msg ?? 'Failed to deploy previous release.');
    } finally {
      this.redeployLoadingTaskId.set(undefined);
    }
  }

  protected componentSummary(item: DeploymentTimelineItem): string {
    if (item.components.length === 0) {
      return '-';
    }
    const visible = item.components.slice(0, 2).map((component) => `${component.name} ${component.version}`);
    if (item.components.length > visible.length) {
      visible.push(`+${item.components.length - visible.length}`);
    }
    return visible.join(', ');
  }

  protected eventTime(item: DeploymentTimelineItem): string {
    return item.completedAt ?? item.startedAt ?? item.queuedAt;
  }

  protected shortId(id: string | undefined): string {
    return id ? id.slice(0, 8) : '-';
  }

  protected actorLabel(item: DeploymentTimelineItem): string {
    return this.shortId(item.actorUserAccountId);
  }

  protected logUrl(item: DeploymentTimelineItem): string {
    return `/api/v1/tasks/${item.taskId}/logs`;
  }

  protected statusClass(status: DeploymentTaskStatus): string {
    switch (status) {
      case 'SUCCEEDED':
        return 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-300';
      case 'FAILED':
      case 'CANCELED':
        return 'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-300';
      case 'RUNNING':
        return 'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-300';
      default:
        return 'bg-gray-100 text-gray-800 dark:bg-gray-700 dark:text-gray-300';
    }
  }

  protected changeClass(kind: DeploymentTimelineChangeKind): string {
    switch (kind) {
      case 'added':
        return 'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-300';
      case 'removed':
        return 'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-300';
      case 'changed':
        return 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-300';
      default:
        return 'bg-gray-100 text-gray-800 dark:bg-gray-700 dark:text-gray-300';
    }
  }

  protected changedCount(
    changes: Array<DeploymentTimelineComponentChange | DeploymentTimelineStepChange | DeploymentTimelineVariableChange>
  ): number {
    return changes.filter((change) => change.kind !== 'unchanged').length;
  }

  protected visibleComponentChanges(comparison: DeploymentTimelineComparison): DeploymentTimelineComponentChange[] {
    return comparison.components.filter((change) => change.kind !== 'unchanged');
  }

  protected visibleStepChanges(comparison: DeploymentTimelineComparison): DeploymentTimelineStepChange[] {
    return comparison.steps.filter((change) => change.kind !== 'unchanged');
  }

  protected visibleVariableChanges(comparison: DeploymentTimelineComparison): DeploymentTimelineVariableChange[] {
    return comparison.variables.filter((change) => change.kind !== 'unchanged');
  }

  protected variableDisplay(change: DeploymentTimelineVariableChange, side: 'base' | 'compare'): string {
    const redacted = side === 'base' ? change.baseRedacted : change.compareRedacted;
    const reference = side === 'base' ? change.baseReference : change.compareReference;
    const status = side === 'base' ? change.baseStatus : change.compareStatus;
    if (redacted) {
      return 'redacted';
    }
    return reference || status || '-';
  }

  private applyFilter() {
    const search = this.filterForm.controls.search.value.trim().toLowerCase();
    const status = this.filterForm.controls.status.value;
    this.filteredTimelineItems.set(
      this.timelineItems().filter((item) => {
        if (status && item.status !== status) {
          return false;
        }
        if (!search) {
          return true;
        }
        return [
          item.taskId,
          item.deploymentPlanId,
          item.applicationName,
          item.releaseNumber,
          item.channelName,
          item.environmentName,
          item.deploymentTargetName,
          item.status,
          item.actorUserAccountId ?? '',
          ...item.components.flatMap((component) => [component.name, component.version, component.key]),
        ]
          .join(' ')
          .toLowerCase()
          .includes(search);
      })
    );
  }

  private dropMissingSelections() {
    const taskIDs = new Set(this.timelineItems().map((item) => item.taskId));
    if (this.selectedBaseTaskId() && !taskIDs.has(this.selectedBaseTaskId()!)) {
      this.selectedBaseTaskId.set(undefined);
    }
    if (this.selectedCompareTaskId() && !taskIDs.has(this.selectedCompareTaskId()!)) {
      this.selectedCompareTaskId.set(undefined);
    }
  }
}
