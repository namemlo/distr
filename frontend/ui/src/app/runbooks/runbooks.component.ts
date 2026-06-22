import {DatePipe, JsonPipe} from '@angular/common';
import {ChangeDetectionStrategy, Component, inject, signal, TemplateRef, viewChild} from '@angular/core';
import {FormArray, FormBuilder, ReactiveFormsModule, Validators} from '@angular/forms';
import {Application} from '@distr-sh/distr-sdk';
import {FontAwesomeModule} from '@fortawesome/angular-fontawesome';
import {
  faBookOpen,
  faEdit,
  faEye,
  faMagnifyingGlass,
  faPlay,
  faPlus,
  faRotateRight,
  faTrash,
  faTriangleExclamation,
  faXmark,
} from '@fortawesome/free-solid-svg-icons';
import {filter, firstValueFrom, forkJoin, map, startWith} from 'rxjs';
import {getFormDisplayedError} from '../../util/errors';
import {AutotrimDirective} from '../directives/autotrim.directive';
import {ApplicationsService} from '../services/applications.service';
import {DialogRef, OverlayService} from '../services/overlay.service';
import {RunbooksService} from '../services/runbooks.service';
import {ToastService} from '../services/toast.service';
import {
  CreateRunbookRevisionRequest,
  CreateUpdateRunbookRequest,
  Runbook,
  RunbookRevision,
  RunbookStep,
  RunbookStepRequest,
} from '../types/runbook';

type RunbookTab = 'editor' | 'history' | 'schedules';

@Component({
  templateUrl: './runbooks.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  imports: [ReactiveFormsModule, FontAwesomeModule, DatePipe, JsonPipe, AutotrimDirective],
})
export class RunbooksComponent {
  protected readonly faBookOpen = faBookOpen;
  protected readonly faMagnifyingGlass = faMagnifyingGlass;
  protected readonly faPlus = faPlus;
  protected readonly faTrash = faTrash;
  protected readonly faXmark = faXmark;
  protected readonly faEdit = faEdit;
  protected readonly faEye = faEye;
  protected readonly faRotateRight = faRotateRight;
  protected readonly faTriangleExclamation = faTriangleExclamation;
  protected readonly faPlay = faPlay;

  private readonly runbooksService = inject(RunbooksService);
  private readonly applicationsService = inject(ApplicationsService);
  private readonly toast = inject(ToastService);
  private readonly overlay = inject(OverlayService);
  private readonly fb = inject(FormBuilder).nonNullable;

  protected readonly runbooks = signal<Runbook[]>([]);
  protected readonly applications = signal<Application[]>([]);
  protected readonly filteredRunbooks = signal<Runbook[]>([]);
  protected readonly revisions = signal<RunbookRevision[]>([]);
  protected readonly selectedRunbook = signal<Runbook | undefined>(undefined);
  protected readonly selectedRevision = signal<RunbookRevision | undefined>(undefined);
  protected readonly activeTab = signal<RunbookTab>('editor');
  protected readonly loading = signal(true);
  protected readonly revisionsLoading = signal(false);
  protected readonly loadError = signal<string | undefined>(undefined);
  protected readonly revisionsError = signal<string | undefined>(undefined);
  protected readonly formLoading = signal(false);

  protected readonly filterForm = this.fb.group({
    search: this.fb.control(''),
  });

  protected readonly runbookForm = this.fb.group({
    id: this.fb.control(''),
    applicationId: this.fb.control('', [Validators.required]),
    name: this.fb.control('', [Validators.required]),
    description: this.fb.control(''),
    sortOrder: this.fb.control(0, [Validators.required, Validators.min(0)]),
  });

  protected readonly revisionForm = this.fb.group({
    description: this.fb.control(''),
    steps: this.fb.array([this.createStepGroup()]),
  });

  private readonly runbookDialog = viewChild.required<TemplateRef<unknown>>('runbookDialog');
  private readonly revisionDialog = viewChild.required<TemplateRef<unknown>>('revisionDialog');
  private readonly revisionDetailDialog = viewChild.required<TemplateRef<unknown>>('revisionDetailDialog');
  private modalRef?: DialogRef;

  constructor() {
    this.filterForm.controls.search.valueChanges.pipe(startWith('')).subscribe((search) => {
      this.applyFilter(search);
    });
    this.load();
  }

  protected get stepsArray(): FormArray {
    return this.revisionForm.controls.steps;
  }

  protected selectTab(tab: RunbookTab) {
    this.activeTab.set(tab);
  }

  protected load() {
    this.loading.set(true);
    this.loadError.set(undefined);
    forkJoin({
      runbooks: this.runbooksService.list(),
      applications: this.applicationsService.list(),
    }).subscribe({
      next: ({runbooks, applications}) => {
        this.runbooks.set(runbooks);
        this.applications.set(applications);
        this.applyFilter(this.filterForm.controls.search.value);
        this.loading.set(false);
      },
      error: (e) => {
        this.loadError.set(getFormDisplayedError(e) ?? 'Failed to load runbooks.');
        this.loading.set(false);
      },
    });
  }

  protected async selectRunbook(runbook: Runbook) {
    this.selectedRevision.set(undefined);
    try {
      const selectedRunbook = await firstValueFrom(this.runbooksService.get(runbook.id));
      this.selectedRunbook.set(selectedRunbook);
      await this.loadRevisions(selectedRunbook.id);
    } catch (e) {
      this.showError(e);
    }
  }

  protected showCreateRunbookDialog() {
    this.closeDialog(false);
    this.runbookForm.reset({
      id: '',
      applicationId: this.applications()[0]?.id ?? '',
      name: '',
      description: '',
      sortOrder: this.nextSortOrder(),
    });
    this.modalRef = this.overlay.showModal(this.runbookDialog());
  }

  protected showUpdateRunbookDialog(runbook: Runbook) {
    this.closeDialog(false);
    this.runbookForm.setValue({
      id: runbook.id,
      applicationId: runbook.applicationId,
      name: runbook.name,
      description: runbook.description,
      sortOrder: runbook.sortOrder,
    });
    this.modalRef = this.overlay.showModal(this.runbookDialog());
  }

  protected closeDialog(reset = true) {
    this.modalRef?.close();
    if (reset) {
      this.runbookForm.reset();
      this.revisionForm.reset();
      this.resetSteps();
      this.selectedRevision.set(undefined);
    }
  }

  protected async submitRunbookForm() {
    this.runbookForm.markAllAsTouched();
    if (this.runbookForm.invalid) {
      return;
    }

    this.formLoading.set(true);
    try {
      const value = this.runbookForm.getRawValue();
      const request: CreateUpdateRunbookRequest = {
        applicationId: value.applicationId,
        name: value.name,
        description: value.description,
        sortOrder: value.sortOrder,
      };
      if (value.id) {
        await firstValueFrom(this.runbooksService.update(value.id, request));
      } else {
        await firstValueFrom(this.runbooksService.create(request));
      }
      this.closeDialog();
      this.load();
    } catch (e) {
      this.showError(e);
    } finally {
      this.formLoading.set(false);
    }
  }

  protected delete(runbook: Runbook) {
    this.overlay
      .confirm({
        message: {
          message: 'Are you sure you want to delete this runbook?',
        },
        requiredConfirmInputText: runbook.name,
      })
      .pipe(
        filter((it) => it === true),
        map(() => runbook.id)
      )
      .subscribe({
        next: async (id) => {
          try {
            await firstValueFrom(this.runbooksService.delete(id));
            if (this.selectedRunbook()?.id === id) {
              this.selectedRunbook.set(undefined);
              this.revisions.set([]);
            }
            this.load();
          } catch (e) {
            this.showError(e);
          }
        },
      });
  }

  protected showCreateRevisionDialog() {
    this.closeDialog(false);
    this.revisionForm.reset({description: ''});
    this.resetSteps();
    this.modalRef = this.overlay.showModal(this.revisionDialog());
  }

  protected showRevisionDetail(revision: RunbookRevision) {
    this.modalRef?.close();
    this.selectedRevision.set(revision);
    this.modalRef = this.overlay.showModal(this.revisionDetailDialog());
  }

  protected addStep() {
    this.stepsArray.push(this.createStepGroup({sortOrder: this.nextStepSortOrder()}));
  }

  protected removeStep(index: number) {
    if (this.stepsArray.length > 1) {
      this.stepsArray.removeAt(index);
    }
  }

  protected async submitRevisionForm() {
    this.revisionForm.markAllAsTouched();
    const runbook = this.selectedRunbook();
    if (this.revisionForm.invalid || !runbook) {
      return;
    }

    const request = this.revisionRequestFromForm();
    if (!request) {
      return;
    }

    this.formLoading.set(true);
    try {
      await firstValueFrom(this.runbooksService.createRevision(runbook.id, request));
      this.closeDialog();
      await this.loadRevisions(runbook.id);
    } catch (e) {
      this.showError(e);
    } finally {
      this.formLoading.set(false);
    }
  }

  protected async publishRevision(revision: RunbookRevision) {
    const runbook = this.selectedRunbook();
    if (!runbook) {
      return;
    }
    this.formLoading.set(true);
    try {
      await firstValueFrom(this.runbooksService.publishRevision(runbook.id, revision.id));
      this.toast.success('Runbook revision published.');
      await this.loadRevisions(runbook.id);
    } catch (e) {
      this.showError(e);
    } finally {
      this.formLoading.set(false);
    }
  }

  protected applicationName(applicationId: string): string {
    return this.applications().find((application) => application.id === applicationId)?.name ?? applicationId;
  }

  protected latestRevision(): RunbookRevision | undefined {
    return [...this.revisions()].sort((a, b) => b.revisionNumber - a.revisionNumber)[0];
  }

  private async loadRevisions(runbookId: string) {
    this.revisionsLoading.set(true);
    this.revisionsError.set(undefined);
    try {
      this.revisions.set(await firstValueFrom(this.runbooksService.listRevisions(runbookId)));
    } catch (e) {
      this.revisionsError.set(getFormDisplayedError(e) ?? 'Failed to load runbook revisions.');
    } finally {
      this.revisionsLoading.set(false);
    }
  }

  private applyFilter(search: string) {
    const normalized = search.toLowerCase();
    this.filteredRunbooks.set(
      this.runbooks().filter((runbook) => {
        const applicationName = this.applicationName(runbook.applicationId).toLowerCase();
        return (
          normalized.length === 0 ||
          runbook.name.toLowerCase().includes(normalized) ||
          runbook.description.toLowerCase().includes(normalized) ||
          applicationName.includes(normalized)
        );
      })
    );
  }

  private nextSortOrder(): number {
    const maxSortOrder = Math.max(0, ...this.runbooks().map((runbook) => runbook.sortOrder));
    return maxSortOrder + 10;
  }

  private nextStepSortOrder(): number {
    const sortOrders = this.stepsArray.controls.map((control) => Number(control.value.sortOrder ?? 0));
    return Math.max(0, ...sortOrders) + 10;
  }

  private resetSteps(steps: Partial<RunbookStep | RunbookStepRequest>[] = []) {
    this.stepsArray.clear();
    const fallback = steps.length > 0 ? steps : [{sortOrder: 10, executionLocation: 'hub', failureMode: 'fail'}];
    for (const step of fallback) {
      this.stepsArray.push(this.createStepGroup(step));
    }
  }

  private createStepGroup(step: Partial<RunbookStep | RunbookStepRequest> = {}) {
    return this.fb.group({
      key: this.fb.control(step.key ?? '', [Validators.required]),
      name: this.fb.control(step.name ?? '', [Validators.required]),
      actionType: this.fb.control(step.actionType ?? '', [Validators.required]),
      stepTemplateVersionId: this.fb.control(step.stepTemplateVersionId ?? ''),
      executionLocation: this.fb.control(step.executionLocation ?? 'hub', [Validators.required]),
      inputBindingsText: this.fb.control(JSON.stringify(step.inputBindings ?? {}, null, 2)),
      condition: this.fb.control(step.condition ?? ''),
      failureMode: this.fb.control(step.failureMode ?? 'fail'),
      timeoutSeconds: this.fb.control(step.timeoutSeconds ?? 0, [Validators.min(0)]),
      retryMaxAttempts: this.fb.control(step.retryPolicy?.maxAttempts ?? 0, [Validators.min(0)]),
      retryIntervalSeconds: this.fb.control(step.retryPolicy?.intervalSeconds ?? 0, [Validators.min(0)]),
      requiredPermissionsText: this.fb.control(this.stringListToText(step.requiredPermissions ?? [])),
      sortOrder: this.fb.control(step.sortOrder ?? 10, [Validators.required, Validators.min(0)]),
      dependenciesText: this.fb.control(this.stringListToText(step.dependencies ?? [])),
    });
  }

  private revisionRequestFromForm(): CreateRunbookRevisionRequest | undefined {
    const value = this.revisionForm.getRawValue();
    const steps: RunbookStepRequest[] = [];
    for (const step of value.steps) {
      const inputBindings = this.parseInputBindings(step.inputBindingsText);
      if (!inputBindings) {
        return undefined;
      }
      steps.push({
        key: step.key.trim(),
        name: step.name.trim(),
        actionType: step.actionType.trim(),
        stepTemplateVersionId: step.stepTemplateVersionId.trim() || undefined,
        executionLocation: step.executionLocation.trim(),
        inputBindings,
        condition: step.condition.trim(),
        failureMode: step.failureMode.trim(),
        timeoutSeconds: step.timeoutSeconds,
        retryPolicy: {
          maxAttempts: step.retryMaxAttempts,
          intervalSeconds: step.retryIntervalSeconds,
        },
        requiredPermissions: this.textToStringList(step.requiredPermissionsText),
        sortOrder: step.sortOrder,
        dependencies: this.textToStringList(step.dependenciesText),
      });
    }
    return {description: value.description.trim(), steps};
  }

  private parseInputBindings(value: string): Record<string, unknown> | undefined {
    const trimmed = value.trim();
    if (!trimmed) {
      return {};
    }
    try {
      const parsed = JSON.parse(trimmed);
      if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
        return parsed;
      }
    } catch {
      this.toast.error('Step input bindings must be valid JSON.');
      return undefined;
    }
    this.toast.error('Step input bindings must be a JSON object.');
    return undefined;
  }

  private textToStringList(value: string): string[] {
    return value
      .split(/[\r\n,]+/)
      .map((item) => item.trim())
      .filter((item) => item.length > 0);
  }

  private stringListToText(values: string[]): string {
    return values.join('\n');
  }

  private showError(e: unknown) {
    const msg = getFormDisplayedError(e);
    if (msg) {
      this.toast.error(msg);
    }
  }
}
