import {DatePipe, DecimalPipe, JsonPipe} from '@angular/common';
import {ChangeDetectionStrategy, Component, inject, signal, TemplateRef, viewChild} from '@angular/core';
import {FormArray, FormBuilder, ReactiveFormsModule, Validators} from '@angular/forms';
import {Application} from '@distr-sh/distr-sdk';
import {FontAwesomeModule} from '@fortawesome/angular-fontawesome';
import {
  faCode,
  faDiagramProject,
  faEdit,
  faEye,
  faMagnifyingGlass,
  faPlus,
  faRotateRight,
  faTrash,
  faTriangleExclamation,
  faXmark,
} from '@fortawesome/free-solid-svg-icons';
import {catchError, filter, firstValueFrom, forkJoin, map, of, startWith, switchMap, take} from 'rxjs';
import {getFormDisplayedError} from '../../util/errors';
import {ConfigAsCodeAuthorityBadgeComponent} from '../components/config-as-code-authority-badge/config-as-code-authority-badge.component';
import {AutotrimDirective} from '../directives/autotrim.directive';
import {ApplicationsService} from '../services/applications.service';
import {ChannelsService} from '../services/channels.service';
import {ConfigAsCodeService} from '../services/config-as-code.service';
import {DeploymentProcessesService} from '../services/deployment-processes.service';
import {EnvironmentsService} from '../services/environments.service';
import {FeatureFlagService} from '../services/feature-flag.service';
import {DialogRef, OverlayService} from '../services/overlay.service';
import {ToastService} from '../services/toast.service';
import {Channel} from '../types/channel';
import {ConfigAsCodeAuthority} from '../types/config-as-code';
import {
  CreateDeploymentProcessRevisionRequest,
  CreateUpdateDeploymentProcessRequest,
  DeploymentProcess,
  DeploymentProcessRevision,
  DeploymentProcessStep,
  DeploymentProcessStepRequest,
} from '../types/deployment-process';
import {Environment} from '../types/environment';

@Component({
  templateUrl: './deployment-processes.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  imports: [
    ReactiveFormsModule,
    FontAwesomeModule,
    DecimalPipe,
    DatePipe,
    JsonPipe,
    AutotrimDirective,
    ConfigAsCodeAuthorityBadgeComponent,
  ],
})
export class DeploymentProcessesComponent {
  protected readonly faMagnifyingGlass = faMagnifyingGlass;
  protected readonly faPlus = faPlus;
  protected readonly faDiagramProject = faDiagramProject;
  protected readonly faTrash = faTrash;
  protected readonly faXmark = faXmark;
  protected readonly faEdit = faEdit;
  protected readonly faEye = faEye;
  protected readonly faRotateRight = faRotateRight;
  protected readonly faTriangleExclamation = faTriangleExclamation;
  protected readonly faCode = faCode;

  private readonly deploymentProcessesService = inject(DeploymentProcessesService);
  private readonly configAsCodeService = inject(ConfigAsCodeService);
  private readonly featureFlagService = inject(FeatureFlagService);
  private readonly applicationsService = inject(ApplicationsService);
  private readonly channelsService = inject(ChannelsService);
  private readonly environmentsService = inject(EnvironmentsService);
  private readonly toast = inject(ToastService);
  private readonly overlay = inject(OverlayService);
  private readonly fb = inject(FormBuilder).nonNullable;

  protected readonly deploymentProcesses = signal<DeploymentProcess[]>([]);
  protected readonly applications = signal<Application[]>([]);
  protected readonly channels = signal<Channel[]>([]);
  protected readonly environments = signal<Environment[]>([]);
  protected readonly authorities = signal<Record<string, ConfigAsCodeAuthority>>({});
  protected readonly filteredDeploymentProcesses = signal<DeploymentProcess[]>([]);
  protected readonly revisions = signal<DeploymentProcessRevision[]>([]);
  protected readonly selectedProcess = signal<DeploymentProcess | undefined>(undefined);
  protected readonly selectedRevision = signal<DeploymentProcessRevision | undefined>(undefined);
  protected readonly loading = signal(true);
  protected readonly revisionsLoading = signal(false);
  protected readonly loadError = signal<string | undefined>(undefined);
  protected readonly revisionsError = signal<string | undefined>(undefined);
  protected readonly formLoading = signal(false);

  protected readonly filterForm = this.fb.group({
    search: this.fb.control(''),
  });

  protected readonly processForm = this.fb.group({
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

  private readonly processDialog = viewChild.required<TemplateRef<unknown>>('processDialog');
  private readonly revisionDialog = viewChild.required<TemplateRef<unknown>>('revisionDialog');
  private readonly revisionsDialog = viewChild.required<TemplateRef<unknown>>('revisionsDialog');
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

  protected load() {
    this.loading.set(true);
    this.loadError.set(undefined);
    forkJoin({
      deploymentProcesses: this.deploymentProcessesService.list(),
      applications: this.applicationsService.list().pipe(take(1)),
      channels: this.channelsService.list(),
      environments: this.environmentsService.list(),
      configAsCodeEnabled: this.featureFlagService.isConfigAsCodeEnabled$.pipe(catchError(() => of(false))),
    })
      .pipe(
        switchMap((loaded) => {
          if (!loaded.configAsCodeEnabled) {
            return of({...loaded, authorities: [] as ConfigAsCodeAuthority[]});
          }
          return this.configAsCodeService
            .listAuthorities()
            .pipe(map((response) => ({...loaded, authorities: response.authorities})));
        })
      )
      .subscribe({
        next: ({deploymentProcesses, applications, channels, environments, authorities}) => {
          this.deploymentProcesses.set(deploymentProcesses);
          this.applications.set(applications);
          this.channels.set(channels);
          this.environments.set(environments);
          this.authorities.set(
            Object.fromEntries(
              authorities
                .filter((authority) => authority.resourceKind === 'DeploymentProcess')
                .map((authority) => [authority.resourceId, authority])
            )
          );
          this.applyFilter(this.filterForm.controls.search.value);
          this.loading.set(false);
        },
        error: (e) => {
          this.loadError.set(getFormDisplayedError(e) ?? 'Failed to load deployment processes.');
          this.loading.set(false);
        },
      });
  }

  protected showCreateProcessDialog() {
    this.closeDialog(false);
    this.processForm.reset({
      id: '',
      applicationId: this.applications()[0]?.id ?? '',
      name: '',
      description: '',
      sortOrder: this.nextSortOrder(),
    });
    this.modalRef = this.overlay.showModal(this.processDialog());
  }

  protected showUpdateProcessDialog(process: DeploymentProcess) {
    if (this.blockGitManaged(process)) {
      return;
    }
    this.closeDialog(false);
    this.processForm.setValue({
      id: process.id,
      applicationId: process.applicationId,
      name: process.name,
      description: process.description,
      sortOrder: process.sortOrder,
    });
    this.modalRef = this.overlay.showModal(this.processDialog());
  }

  protected closeDialog(reset = true) {
    this.modalRef?.close();
    if (reset) {
      this.processForm.reset();
      this.revisionForm.reset();
      this.resetSteps();
      this.selectedProcess.set(undefined);
      this.selectedRevision.set(undefined);
      this.revisions.set([]);
    }
  }

  protected async submitProcessForm() {
    this.processForm.markAllAsTouched();
    if (this.processForm.invalid) {
      return;
    }

    this.formLoading.set(true);
    try {
      const value = this.processForm.getRawValue();
      const request: CreateUpdateDeploymentProcessRequest = {
        applicationId: value.applicationId,
        name: value.name,
        description: value.description,
        sortOrder: value.sortOrder,
      };
      if (value.id) {
        const process = this.deploymentProcesses().find((item) => item.id === value.id);
        if (process && this.blockGitManaged(process)) {
          return;
        }
        await firstValueFrom(this.deploymentProcessesService.update(value.id, request));
      } else {
        await firstValueFrom(this.deploymentProcessesService.create(request));
      }
      this.closeDialog();
      this.load();
    } catch (e) {
      this.showError(e);
    } finally {
      this.formLoading.set(false);
    }
  }

  protected delete(process: DeploymentProcess) {
    if (this.blockGitManaged(process)) {
      return;
    }
    this.overlay
      .confirm({
        message: {
          message: 'Are you sure you want to delete this deployment process?',
        },
        requiredConfirmInputText: process.name,
      })
      .pipe(
        filter((it) => it === true),
        map(() => process.id)
      )
      .subscribe({
        next: async (id) => {
          try {
            await firstValueFrom(this.deploymentProcessesService.delete(id));
            this.load();
          } catch (e) {
            this.showError(e);
          }
        },
      });
  }

  protected async showRevisionsDialog(process: DeploymentProcess) {
    this.closeDialog(false);
    this.selectedProcess.set(process);
    this.selectedRevision.set(undefined);
    this.revisions.set([]);
    this.modalRef = this.overlay.showModal(this.revisionsDialog());
    await this.loadRevisions(process.id);
  }

  protected showCreateRevisionDialog(process: DeploymentProcess) {
    if (this.blockGitManaged(process)) {
      return;
    }
    this.closeDialog(false);
    this.selectedProcess.set(process);
    this.revisionForm.reset({description: ''});
    this.resetSteps();
    this.modalRef = this.overlay.showModal(this.revisionDialog());
  }

  protected showRevisionDetail(revision: DeploymentProcessRevision) {
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
    if (this.revisionForm.invalid || !this.selectedProcess()) {
      return;
    }

    const request = this.revisionRequestFromForm();
    if (!request) {
      return;
    }

    this.formLoading.set(true);
    try {
      const process = this.selectedProcess()!;
      if (this.blockGitManaged(process)) {
        return;
      }
      await firstValueFrom(this.deploymentProcessesService.createRevision(process.id, request));
      this.closeDialog();
      await this.showRevisionsDialog(process);
    } catch (e) {
      this.showError(e);
    } finally {
      this.formLoading.set(false);
    }
  }

  protected applicationName(applicationId: string): string {
    return this.applications().find((application) => application.id === applicationId)?.name ?? applicationId;
  }

  protected channelName(channelId: string): string {
    return this.channels().find((channel) => channel.id === channelId)?.name ?? channelId;
  }

  protected environmentName(environmentId: string): string {
    return this.environments().find((environment) => environment.id === environmentId)?.name ?? environmentId;
  }

  protected channelNames(channelIds: string[]): string {
    return channelIds.length ? channelIds.map((channelId) => this.channelName(channelId)).join(', ') : '-';
  }

  protected environmentNames(environmentIds: string[]): string {
    return environmentIds.length
      ? environmentIds.map((environmentId) => this.environmentName(environmentId)).join(', ')
      : '-';
  }

  protected channelsForSelectedProcess(): Channel[] {
    const applicationId = this.selectedProcess()?.applicationId ?? this.processForm.controls.applicationId.value;
    return this.channels().filter((channel) => channel.applicationId === applicationId);
  }

  protected authorityFor(process: DeploymentProcess): ConfigAsCodeAuthority | undefined {
    return this.authorities()[process.id];
  }

  protected isGitManaged(process: DeploymentProcess): boolean {
    return this.authorityFor(process)?.authority === 'GIT_MANAGED';
  }

  private async loadRevisions(processId: string) {
    this.revisionsLoading.set(true);
    this.revisionsError.set(undefined);
    try {
      this.revisions.set(await firstValueFrom(this.deploymentProcessesService.listRevisions(processId)));
    } catch (e) {
      this.revisionsError.set(getFormDisplayedError(e) ?? 'Failed to load deployment process revisions.');
    } finally {
      this.revisionsLoading.set(false);
    }
  }

  private applyFilter(search: string) {
    const normalized = search.toLowerCase();
    this.filteredDeploymentProcesses.set(
      this.deploymentProcesses().filter((process) => {
        const applicationName = this.applicationName(process.applicationId).toLowerCase();
        return (
          normalized.length === 0 ||
          process.name.toLowerCase().includes(normalized) ||
          process.description.toLowerCase().includes(normalized) ||
          applicationName.includes(normalized)
        );
      })
    );
  }

  private nextSortOrder(): number {
    const maxSortOrder = Math.max(0, ...this.deploymentProcesses().map((process) => process.sortOrder));
    return maxSortOrder + 10;
  }

  private nextStepSortOrder(): number {
    const sortOrders = this.stepsArray.controls.map((control) => Number(control.value.sortOrder ?? 0));
    return Math.max(0, ...sortOrders) + 10;
  }

  private resetSteps(steps: Partial<DeploymentProcessStep | DeploymentProcessStepRequest>[] = []) {
    this.stepsArray.clear();
    const fallback = steps.length > 0 ? steps : [{sortOrder: 10}];
    for (const step of fallback) {
      this.stepsArray.push(this.createStepGroup(step));
    }
  }

  private createStepGroup(step: Partial<DeploymentProcessStep | DeploymentProcessStepRequest> = {}) {
    return this.fb.group({
      key: this.fb.control(step.key ?? '', [Validators.required]),
      name: this.fb.control(step.name ?? '', [Validators.required]),
      actionType: this.fb.control(step.actionType ?? '', [Validators.required]),
      stepTemplateVersionId: this.fb.control(step.stepTemplateVersionId ?? ''),
      executionLocation: this.fb.control(step.executionLocation ?? 'target', [Validators.required]),
      inputBindingsText: this.fb.control(JSON.stringify(step.inputBindings ?? {}, null, 2)),
      condition: this.fb.control(step.condition ?? ''),
      channelIds: this.fb.control<string[]>([...(step.channelIds ?? [])]),
      environmentIds: this.fb.control<string[]>([...(step.environmentIds ?? [])]),
      targetTagsText: this.fb.control(this.stringListToText(step.targetTags ?? [])),
      failureMode: this.fb.control(step.failureMode ?? 'fail'),
      timeoutSeconds: this.fb.control(step.timeoutSeconds ?? 0, [Validators.min(0)]),
      retryMaxAttempts: this.fb.control(step.retryPolicy?.maxAttempts ?? 0, [Validators.min(0)]),
      retryIntervalSeconds: this.fb.control(step.retryPolicy?.intervalSeconds ?? 0, [Validators.min(0)]),
      requiredPermissionsText: this.fb.control(this.stringListToText(step.requiredPermissions ?? [])),
      sortOrder: this.fb.control(step.sortOrder ?? 10, [Validators.required, Validators.min(0)]),
      dependenciesText: this.fb.control(this.stringListToText(step.dependencies ?? [])),
    });
  }

  private revisionRequestFromForm(): CreateDeploymentProcessRevisionRequest | undefined {
    const value = this.revisionForm.getRawValue();
    const steps: DeploymentProcessStepRequest[] = [];
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
        channelIds: step.channelIds,
        environmentIds: step.environmentIds,
        targetTags: this.textToStringList(step.targetTagsText),
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

  private blockGitManaged(process: DeploymentProcess): boolean {
    if (!this.isGitManaged(process)) {
      return false;
    }
    this.toast.error('This deployment process is managed from Git.');
    return true;
  }
}
