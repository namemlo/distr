import {DatePipe, DecimalPipe, NgTemplateOutlet} from '@angular/common';
import {ChangeDetectionStrategy, Component, inject, signal, TemplateRef, viewChild} from '@angular/core';
import {FormArray, FormBuilder, ReactiveFormsModule, Validators} from '@angular/forms';
import {Application, ApplicationVersion} from '@distr-sh/distr-sdk';
import {FontAwesomeModule} from '@fortawesome/angular-fontawesome';
import {
  faBan,
  faBoxArchive,
  faBoxesStacked,
  faCheck,
  faEdit,
  faEye,
  faMagnifyingGlass,
  faPlus,
  faRocket,
  faRotateRight,
  faTrash,
  faTriangleExclamation,
  faXmark,
} from '@fortawesome/free-solid-svg-icons';
import {filter, firstValueFrom, forkJoin, map, Observable, startWith, take} from 'rxjs';
import {getFormDisplayedError} from '../../util/errors';
import {AutotrimDirective} from '../directives/autotrim.directive';
import {ApplicationsService} from '../services/applications.service';
import {ChannelsService} from '../services/channels.service';
import {DialogRef, OverlayService} from '../services/overlay.service';
import {ReleaseBundlesService} from '../services/release-bundles.service';
import {ToastService} from '../services/toast.service';
import {Channel} from '../types/channel';
import {
  CreateUpdateReleaseBundleRequest,
  ReleaseBundle,
  ReleaseBundleComponent,
  ReleaseBundleComponentRequest,
  ReleaseBundleComponentType,
  ReleaseBundleValidationResponse,
  ReleaseContract,
} from '../types/release-bundle';

@Component({
  templateUrl: './release-bundles.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  imports: [ReactiveFormsModule, FontAwesomeModule, DecimalPipe, DatePipe, NgTemplateOutlet, AutotrimDirective],
})
export class ReleaseBundlesComponent {
  protected readonly faMagnifyingGlass = faMagnifyingGlass;
  protected readonly faPlus = faPlus;
  protected readonly faBoxesStacked = faBoxesStacked;
  protected readonly faTrash = faTrash;
  protected readonly faXmark = faXmark;
  protected readonly faEdit = faEdit;
  protected readonly faEye = faEye;
  protected readonly faCheck = faCheck;
  protected readonly faRotateRight = faRotateRight;
  protected readonly faTriangleExclamation = faTriangleExclamation;
  protected readonly faRocket = faRocket;
  protected readonly faBan = faBan;
  protected readonly faBoxArchive = faBoxArchive;

  protected readonly componentTypes: {value: ReleaseBundleComponentType; label: string}[] = [
    {value: 'application_version', label: 'Application Version'},
    {value: 'oci_image', label: 'OCI Image'},
    {value: 'oci_artifact', label: 'OCI Artifact'},
    {value: 'helm_chart', label: 'Helm Chart'},
    {value: 'child_release_bundle', label: 'Child Release Bundle'},
    {value: 'external_artifact', label: 'External Artifact'},
  ];

  private readonly releaseBundlesService = inject(ReleaseBundlesService);
  private readonly applicationsService = inject(ApplicationsService);
  private readonly channelsService = inject(ChannelsService);
  private readonly toast = inject(ToastService);
  private readonly overlay = inject(OverlayService);
  private readonly fb = inject(FormBuilder).nonNullable;

  protected readonly releaseBundles = signal<ReleaseBundle[]>([]);
  protected readonly applications = signal<Application[]>([]);
  protected readonly channels = signal<Channel[]>([]);
  protected readonly loading = signal(true);
  protected readonly loadError = signal<string | undefined>(undefined);
  protected readonly formLoading = signal(false);
  protected readonly actionLoading = signal<string | undefined>(undefined);
  protected readonly filteredReleaseBundles = signal<ReleaseBundle[]>([]);
  protected readonly selectedReleaseBundle = signal<ReleaseBundle | undefined>(undefined);
  protected readonly validationResults = signal<Record<string, ReleaseBundleValidationResponse>>({});

  protected readonly filterForm = this.fb.group({
    search: this.fb.control(''),
  });

  protected readonly releaseBundleForm = this.fb.group({
    id: this.fb.control(''),
    applicationId: this.fb.control('', [Validators.required]),
    channelId: this.fb.control('', [Validators.required]),
    releaseNumber: this.fb.control('', [Validators.required]),
    releaseNotes: this.fb.control(''),
    sourceRevision: this.fb.control(''),
    components: this.fb.array([this.createComponentGroup()]),
  });

  private readonly releaseBundleDialog = viewChild.required<TemplateRef<unknown>>('releaseBundleDialog');
  private readonly detailDialog = viewChild.required<TemplateRef<unknown>>('detailDialog');
  private modalRef?: DialogRef;

  constructor() {
    this.filterForm.controls.search.valueChanges.pipe(startWith('')).subscribe((search) => {
      this.applyFilter(search);
    });
    this.releaseBundleForm.controls.applicationId.valueChanges.subscribe((applicationId) => {
      this.ensureChannelForApplication(applicationId);
    });
    this.load();
  }

  protected get componentsArray(): FormArray {
    return this.releaseBundleForm.controls.components;
  }

  protected load() {
    this.loading.set(true);
    this.loadError.set(undefined);
    forkJoin({
      releaseBundles: this.releaseBundlesService.list(),
      applications: this.applicationsService.list().pipe(take(1)),
      channels: this.channelsService.list(),
    }).subscribe({
      next: ({releaseBundles, applications, channels}) => {
        this.releaseBundles.set(releaseBundles.map((bundle) => this.normalizeReleaseBundleCollections(bundle)));
        this.applications.set(applications);
        this.channels.set(channels);
        this.applyFilter(this.filterForm.controls.search.value);
        this.loading.set(false);
      },
      error: (e) => {
        this.loadError.set(getFormDisplayedError(e) ?? 'Failed to load release bundles.');
        this.loading.set(false);
      },
    });
  }

  protected showCreateDialog() {
    this.closeDialog(false);
    this.selectedReleaseBundle.set(undefined);
    const applicationId = this.applications()[0]?.id ?? '';
    const channelId = this.channelsForApplication(applicationId)[0]?.id ?? this.channels()[0]?.id ?? '';
    this.releaseBundleForm.reset({
      id: '',
      applicationId,
      channelId,
      releaseNumber: '',
      releaseNotes: '',
      sourceRevision: '',
    });
    this.resetComponents([this.defaultComponentForApplication(applicationId)]);
    this.modalRef = this.overlay.showModal(this.releaseBundleDialog());
  }

  protected showUpdateDialog(bundle: ReleaseBundle) {
    if (!this.canEdit(bundle)) {
      this.showDetailDialog(bundle);
      return;
    }

    this.closeDialog(false);
    this.selectedReleaseBundle.set(bundle);
    this.releaseBundleForm.patchValue({
      id: bundle.id,
      applicationId: bundle.applicationId,
      channelId: bundle.channelId,
      releaseNumber: bundle.releaseNumber,
      releaseNotes: bundle.releaseNotes,
      sourceRevision: bundle.sourceRevision,
    });
    this.resetComponents(bundle.components);
    this.modalRef = this.overlay.showModal(this.releaseBundleDialog());
  }

  protected showDetailDialog(bundle: ReleaseBundle) {
    this.closeDialog(false);
    this.selectedReleaseBundle.set(bundle);
    this.modalRef = this.overlay.showModal(this.detailDialog());
  }

  protected closeDialog(reset = true) {
    this.modalRef?.close();
    if (reset) {
      this.releaseBundleForm.reset();
      this.selectedReleaseBundle.set(undefined);
      this.resetComponents([this.createComponentGroup().getRawValue()]);
    }
  }

  protected addComponent() {
    this.componentsArray.push(
      this.createComponentGroup(this.defaultComponentForApplication(this.selectedApplicationId()))
    );
  }

  protected removeComponent(index: number) {
    if (this.componentsArray.length > 1) {
      this.componentsArray.removeAt(index);
    }
  }

  protected onApplicationChanged() {
    const applicationId = this.selectedApplicationId();
    this.ensureChannelForApplication(applicationId);
    if (this.componentsArray.length === 0) {
      this.addComponent();
    }
  }

  protected onApplicationVersionChanged(index: number) {
    const group = this.componentsArray.at(index);
    const version = this.applicationVersionsForSelectedApplication().find(
      (candidate) => candidate.id === group.value.applicationVersionId
    );
    if (version) {
      group.patchValue({version: version.name});
    }
  }

  protected async submitForm() {
    this.releaseBundleForm.markAllAsTouched();
    if (this.releaseBundleForm.invalid) {
      return;
    }

    this.formLoading.set(true);
    try {
      const value = this.releaseBundleForm.getRawValue();
      const request = this.releaseBundleRequestFromForm();
      if (value.id) {
        await firstValueFrom(this.releaseBundlesService.update(value.id, request));
      } else {
        await firstValueFrom(this.releaseBundlesService.create(request));
      }
      this.closeDialog();
      this.load();
    } catch (e) {
      const msg = getFormDisplayedError(e);
      if (msg) {
        this.toast.error(msg);
      }
    } finally {
      this.formLoading.set(false);
    }
  }

  protected async validate(bundle: ReleaseBundle): Promise<ReleaseBundleValidationResponse | undefined> {
    this.actionLoading.set(`validate:${bundle.id}`);
    try {
      const result = await firstValueFrom(this.releaseBundlesService.validate(bundle.id));
      this.setValidationResult(bundle.id, result);
      if (result.valid) {
        this.toast.success('Release bundle validation passed');
      }
      return result;
    } catch (e) {
      const msg = getFormDisplayedError(e);
      if (msg) {
        this.toast.error(msg);
      }
      return undefined;
    } finally {
      this.actionLoading.set(undefined);
    }
  }

  protected async publish(bundle: ReleaseBundle) {
    const result = await this.validate(bundle);
    if (!result?.valid) {
      return;
    }

    const confirmed = await firstValueFrom(
      this.overlay.confirm({
        message: {
          message: `Publish release ${bundle.releaseNumber}?`,
          alert: {type: 'info', message: 'Published releases are immutable.'},
        },
        confirmLabel: 'Publish',
      })
    );
    if (!confirmed) {
      return;
    }
    await this.runAction(`publish:${bundle.id}`, () => this.releaseBundlesService.publish(bundle.id));
  }

  protected async block(bundle: ReleaseBundle) {
    const confirmed = await firstValueFrom(
      this.overlay.confirm({
        message: {
          message: `Block release ${bundle.releaseNumber}?`,
          alert: {type: 'warning', message: 'Blocked releases remain visible for history.'},
        },
        confirmLabel: 'Block',
      })
    );
    if (confirmed) {
      await this.runAction(`block:${bundle.id}`, () => this.releaseBundlesService.block(bundle.id));
    }
  }

  protected async archive(bundle: ReleaseBundle) {
    const confirmed = await firstValueFrom(
      this.overlay.confirm({
        message: {
          message: `Archive release ${bundle.releaseNumber}?`,
          alert: {type: 'warning', message: 'Archived releases are removed from normal selection.'},
        },
        confirmLabel: 'Archive',
      })
    );
    if (confirmed) {
      await this.runAction(`archive:${bundle.id}`, () => this.releaseBundlesService.archive(bundle.id));
    }
  }

  protected delete(bundle: ReleaseBundle) {
    this.overlay
      .confirm({
        message: {
          message: 'Are you sure you want to delete this draft release?',
        },
        requiredConfirmInputText: bundle.releaseNumber,
      })
      .pipe(
        filter((it) => it === true),
        map(() => bundle.id)
      )
      .subscribe({
        next: async (id) => {
          await this.runAction(`delete:${id}`, () => this.releaseBundlesService.delete(id));
        },
      });
  }

  protected applicationName(applicationId: string): string {
    return this.applications().find((application) => application.id === applicationId)?.name ?? applicationId;
  }

  protected channelName(channelId: string): string {
    return this.channels().find((channel) => channel.id === channelId)?.name ?? channelId;
  }

  protected channelsForApplication(applicationId: string): Channel[] {
    return this.channels().filter((channel) => channel.applicationId === applicationId);
  }

  protected applicationVersionsForSelectedApplication(): ApplicationVersion[] {
    const applicationId = this.selectedApplicationId();
    return (
      this.applications()
        .find((application) => application.id === applicationId)
        ?.versions?.filter((version) => !version.archivedAt) ?? []
    );
  }

  protected childReleaseBundleOptions(): ReleaseBundle[] {
    const currentBundleId = this.releaseBundleForm.controls.id.value;
    return this.releaseBundles().filter((bundle) => bundle.status === 'PUBLISHED' && bundle.id !== currentBundleId);
  }

  protected isSelectedDraft(): boolean {
    return this.selectedReleaseBundle()?.status === 'DRAFT';
  }

  protected canEdit(bundle: ReleaseBundle): boolean {
    return bundle.status === 'DRAFT';
  }

  protected canPublish(bundle: ReleaseBundle): boolean {
    return bundle.status === 'DRAFT';
  }

  protected canBlock(bundle: ReleaseBundle): boolean {
    return bundle.status === 'PUBLISHED';
  }

  protected canArchive(bundle: ReleaseBundle): boolean {
    return bundle.status === 'PUBLISHED' || bundle.status === 'BLOCKED';
  }

  protected componentTypeLabel(type: ReleaseBundleComponentType): string {
    return this.componentTypes.find((componentType) => componentType.value === type)?.label ?? type;
  }

  protected statusClass(status: string): string {
    switch (status) {
      case 'PUBLISHED':
        return 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-300';
      case 'BLOCKED':
        return 'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-300';
      case 'ARCHIVED':
        return 'bg-gray-100 text-gray-800 dark:bg-gray-700 dark:text-gray-300';
      default:
        return 'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-300';
    }
  }

  private async runAction(actionKey: string, action: () => Observable<unknown>) {
    this.actionLoading.set(actionKey);
    try {
      await firstValueFrom(action());
      this.closeDialog();
      this.load();
    } catch (e) {
      const msg = getFormDisplayedError(e);
      if (msg) {
        this.toast.error(msg);
      }
    } finally {
      this.actionLoading.set(undefined);
    }
  }

  private applyFilter(search: string) {
    const normalized = search.toLowerCase();
    this.filteredReleaseBundles.set(
      this.releaseBundles().filter((bundle) => {
        const applicationName = this.applicationName(bundle.applicationId).toLowerCase();
        const channelName = this.channelName(bundle.channelId).toLowerCase();
        return (
          normalized.length === 0 ||
          bundle.releaseNumber.toLowerCase().includes(normalized) ||
          bundle.releaseNotes.toLowerCase().includes(normalized) ||
          bundle.sourceRevision.toLowerCase().includes(normalized) ||
          bundle.status.toLowerCase().includes(normalized) ||
          applicationName.includes(normalized) ||
          channelName.includes(normalized)
        );
      })
    );
  }

  private selectedApplicationId(): string {
    return this.releaseBundleForm.controls.applicationId.value;
  }

  private ensureChannelForApplication(applicationId: string) {
    const availableChannels = this.channelsForApplication(applicationId);
    const currentChannel = this.releaseBundleForm.controls.channelId.value;
    if (availableChannels.length > 0 && availableChannels.every((channel) => channel.id !== currentChannel)) {
      this.releaseBundleForm.controls.channelId.setValue(availableChannels[0].id);
    }
  }

  private setValidationResult(bundleId: string, result: ReleaseBundleValidationResponse) {
    this.validationResults.update((current) => ({...current, [bundleId]: result}));
  }

  private resetComponents(components: Partial<ReleaseBundleComponentRequest>[]) {
    this.componentsArray.clear();
    const fallbackComponents =
      components.length > 0 ? components : [this.defaultComponentForApplication(this.selectedApplicationId())];
    for (const component of fallbackComponents) {
      this.componentsArray.push(this.createComponentGroup(component));
    }
  }

  private createComponentGroup(component: Partial<ReleaseBundleComponent | ReleaseBundleComponentRequest> = {}) {
    return this.fb.group({
      key: this.fb.control(component.key ?? '', [Validators.required]),
      name: this.fb.control(component.name ?? ''),
      type: this.fb.control<ReleaseBundleComponentType>(component.type ?? 'application_version', [Validators.required]),
      version: this.fb.control(component.version ?? '', [Validators.required]),
      applicationVersionId: this.fb.control(component.applicationVersionId ?? ''),
      packageRef: this.fb.control(component.packageRef ?? ''),
      digest: this.fb.control(component.digest ?? ''),
      checksum: this.fb.control(component.checksum ?? ''),
      childReleaseBundleId: this.fb.control(component.childReleaseBundleId ?? ''),
    });
  }

  private defaultComponentForApplication(applicationId: string): ReleaseBundleComponentRequest {
    const version = this.applications()
      .find((application) => application.id === applicationId)
      ?.versions?.find((candidate) => !candidate.archivedAt);
    return {
      key: 'app',
      name: 'Application',
      type: 'application_version',
      version: version?.name ?? '',
      applicationVersionId: version?.id,
      packageRef: '',
      digest: '',
      checksum: '',
    };
  }

  private releaseBundleRequestFromForm(): CreateUpdateReleaseBundleRequest {
    const value = this.releaseBundleForm.getRawValue();
    const releaseContract = this.selectedReleaseBundle()?.releaseContract;
    return {
      applicationId: value.applicationId,
      channelId: value.channelId,
      releaseNumber: value.releaseNumber.trim(),
      releaseNotes: value.releaseNotes,
      sourceRevision: value.sourceRevision.trim(),
      ...(releaseContract ? {releaseContract} : {}),
      components: value.components.map((component) => this.releaseBundleComponentRequest(component)),
    };
  }

  private releaseBundleComponentRequest(component: ReleaseBundleComponentRequest): ReleaseBundleComponentRequest {
    const type = component.type;
    return {
      key: component.key.trim(),
      name: component.name.trim(),
      type,
      version: component.version.trim(),
      applicationVersionId:
        type === 'application_version' && component.applicationVersionId ? component.applicationVersionId : undefined,
      packageRef: component.packageRef.trim(),
      digest: component.digest.trim(),
      checksum: component.checksum.trim(),
      childReleaseBundleId:
        type === 'child_release_bundle' && component.childReleaseBundleId ? component.childReleaseBundleId : undefined,
    };
  }

  private normalizeReleaseBundleCollections(bundle: ReleaseBundle): ReleaseBundle {
    return {
      ...bundle,
      components: bundle.components ?? [],
      releaseContract: this.normalizeReleaseContractCollections(bundle.releaseContract),
    };
  }

  private normalizeReleaseContractCollections(contract: ReleaseContract | undefined): ReleaseContract | undefined {
    if (!contract) {
      return undefined;
    }
    if (contract.schema === 'distr.component-release/v2') {
      return {
        ...contract,
        artifacts: (contract.artifacts ?? []).map((artifact) => ({
          ...artifact,
          platforms: artifact.platforms ?? [],
        })),
        provides: contract.provides ?? [],
        requires: (contract.requires ?? []).map((requirement) => ({
          ...requirement,
          allowedModes: requirement.allowedModes ?? [],
        })),
        migrations: contract.migrations ?? [],
        changes: {...contract.changes, commits: contract.changes.commits ?? []},
        evidence: {
          ...contract.evidence,
          provenance: contract.evidence.provenance ?? [],
          sbom: contract.evidence.sbom ?? [],
          signatures: contract.evidence.signatures ?? [],
          tests: contract.evidence.tests ?? [],
        },
      };
    }
    return {
      ...contract,
      components: (contract.components ?? []).map((component) => ({
        ...component,
        contracts: component.contracts ?? [],
      })),
      compatibility: {
        ...contract.compatibility,
        requires: contract.compatibility.requires ?? [],
        affectedComponents: contract.compatibility.affectedComponents ?? [],
      },
      config: {
        ...contract.config,
        immutableObjects: contract.config.immutableObjects ?? [],
      },
      changes: {...contract.changes, commits: contract.changes.commits ?? []},
    };
  }
}
