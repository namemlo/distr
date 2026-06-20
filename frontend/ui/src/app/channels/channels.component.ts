import {DecimalPipe} from '@angular/common';
import {ChangeDetectionStrategy, Component, inject, signal, TemplateRef, viewChild} from '@angular/core';
import {FormBuilder, ReactiveFormsModule, Validators} from '@angular/forms';
import {Application} from '@distr-sh/distr-sdk';
import {FontAwesomeModule} from '@fortawesome/angular-fontawesome';
import {
  faCheck,
  faEdit,
  faLayerGroup,
  faMagnifyingGlass,
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
import {ChannelsService} from '../services/channels.service';
import {LifecyclesService} from '../services/lifecycles.service';
import {DialogRef, OverlayService} from '../services/overlay.service';
import {ToastService} from '../services/toast.service';
import {Channel, CreateUpdateChannelRequest} from '../types/channel';
import {Lifecycle} from '../types/lifecycle';

@Component({
  templateUrl: './channels.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  imports: [ReactiveFormsModule, FontAwesomeModule, DecimalPipe, AutotrimDirective],
})
export class ChannelsComponent {
  protected readonly faMagnifyingGlass = faMagnifyingGlass;
  protected readonly faPlus = faPlus;
  protected readonly faLayerGroup = faLayerGroup;
  protected readonly faTrash = faTrash;
  protected readonly faXmark = faXmark;
  protected readonly faEdit = faEdit;
  protected readonly faCheck = faCheck;
  protected readonly faRotateRight = faRotateRight;
  protected readonly faTriangleExclamation = faTriangleExclamation;

  private readonly channelsService = inject(ChannelsService);
  private readonly applicationsService = inject(ApplicationsService);
  private readonly lifecyclesService = inject(LifecyclesService);
  private readonly toast = inject(ToastService);
  private readonly overlay = inject(OverlayService);
  private readonly fb = inject(FormBuilder).nonNullable;

  protected readonly channels = signal<Channel[]>([]);
  protected readonly applications = signal<Application[]>([]);
  protected readonly lifecycles = signal<Lifecycle[]>([]);
  protected readonly loading = signal(true);
  protected readonly loadError = signal<string | undefined>(undefined);
  protected readonly formLoading = signal(false);

  protected readonly filterForm = this.fb.group({
    search: this.fb.control(''),
  });

  protected readonly channelForm = this.fb.group({
    id: this.fb.control(''),
    applicationId: this.fb.control('', [Validators.required]),
    lifecycleId: this.fb.control('', [Validators.required]),
    name: this.fb.control('', [Validators.required]),
    description: this.fb.control(''),
    sortOrder: this.fb.control(0, [Validators.required, Validators.min(0)]),
    isDefault: this.fb.control(false),
    allowedVersionRangesText: this.fb.control(''),
    allowedPrereleasePatternsText: this.fb.control(''),
    allowedSourceBranchesText: this.fb.control(''),
    allowedSourceTagsText: this.fb.control(''),
  });

  protected readonly filteredChannels = signal<Channel[]>([]);

  private readonly channelDialog = viewChild.required<TemplateRef<unknown>>('channelDialog');
  private modalRef?: DialogRef;

  constructor() {
    this.filterForm.controls.search.valueChanges.pipe(startWith('')).subscribe((search) => {
      this.applyFilter(search);
    });
    this.load();
  }

  protected load() {
    this.loading.set(true);
    this.loadError.set(undefined);
    forkJoin({
      channels: this.channelsService.list(),
      applications: this.applicationsService.list(),
      lifecycles: this.lifecyclesService.list(),
    }).subscribe({
      next: ({channels, applications, lifecycles}) => {
        this.channels.set(channels);
        this.applications.set(applications);
        this.lifecycles.set(lifecycles);
        this.applyFilter(this.filterForm.controls.search.value);
        this.loading.set(false);
      },
      error: (e) => {
        this.loadError.set(getFormDisplayedError(e) ?? 'Failed to load channels.');
        this.loading.set(false);
      },
    });
  }

  protected showCreateDialog() {
    this.closeDialog(false);
    this.channelForm.reset({
      id: '',
      applicationId: this.applications()[0]?.id ?? '',
      lifecycleId: this.lifecycles()[0]?.id ?? '',
      name: '',
      description: '',
      sortOrder: this.nextSortOrder(),
      isDefault: false,
      allowedVersionRangesText: '',
      allowedPrereleasePatternsText: '',
      allowedSourceBranchesText: '',
      allowedSourceTagsText: '',
    });
    this.modalRef = this.overlay.showModal(this.channelDialog());
  }

  protected showUpdateDialog(channel: Channel) {
    this.closeDialog(false);
    this.channelForm.setValue({
      id: channel.id,
      applicationId: channel.applicationId,
      lifecycleId: channel.lifecycleId,
      name: channel.name,
      description: channel.description,
      sortOrder: channel.sortOrder,
      isDefault: channel.isDefault,
      allowedVersionRangesText: this.ruleListToText(channel.allowedVersionRanges),
      allowedPrereleasePatternsText: this.ruleListToText(channel.allowedPrereleasePatterns),
      allowedSourceBranchesText: this.ruleListToText(channel.allowedSourceBranches),
      allowedSourceTagsText: this.ruleListToText(channel.allowedSourceTags),
    });
    this.modalRef = this.overlay.showModal(this.channelDialog());
  }

  protected closeDialog(reset = true) {
    this.modalRef?.close();
    if (reset) {
      this.channelForm.reset();
    }
  }

  protected async submitForm() {
    this.channelForm.markAllAsTouched();
    if (this.channelForm.invalid) {
      return;
    }

    this.formLoading.set(true);
    try {
      const value = this.channelForm.getRawValue();
      const request: CreateUpdateChannelRequest = {
        applicationId: value.applicationId,
        lifecycleId: value.lifecycleId,
        name: value.name,
        description: value.description,
        sortOrder: value.sortOrder,
        isDefault: value.isDefault,
        allowedVersionRanges: this.ruleListFromText(value.allowedVersionRangesText),
        allowedPrereleasePatterns: this.ruleListFromText(value.allowedPrereleasePatternsText),
        allowedSourceBranches: this.ruleListFromText(value.allowedSourceBranchesText),
        allowedSourceTags: this.ruleListFromText(value.allowedSourceTagsText),
      };
      if (value.id) {
        await firstValueFrom(this.channelsService.update(value.id, request));
      } else {
        await firstValueFrom(this.channelsService.create(request));
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

  protected delete(channel: Channel) {
    this.overlay
      .confirm({
        message: {
          message: 'Are you sure you want to delete this channel?',
        },
        requiredConfirmInputText: channel.name,
      })
      .pipe(
        filter((it) => it === true),
        map(() => channel.id)
      )
      .subscribe({
        next: async (id) => {
          try {
            await firstValueFrom(this.channelsService.delete(id));
            this.load();
          } catch (e) {
            const msg = getFormDisplayedError(e);
            if (msg) {
              this.toast.error(msg);
            }
          }
        },
      });
  }

  protected applicationName(applicationID: string): string {
    return this.applications().find((application) => application.id === applicationID)?.name ?? applicationID;
  }

  protected lifecycleName(lifecycleID: string): string {
    return this.lifecycles().find((lifecycle) => lifecycle.id === lifecycleID)?.name ?? lifecycleID;
  }

  private applyFilter(search: string) {
    const normalized = search.toLowerCase();
    this.filteredChannels.set(
      this.channels().filter((channel) => {
        const applicationName = this.applicationName(channel.applicationId).toLowerCase();
        const lifecycleName = this.lifecycleName(channel.lifecycleId).toLowerCase();
        return (
          normalized.length === 0 ||
          channel.name.toLowerCase().includes(normalized) ||
          channel.description.toLowerCase().includes(normalized) ||
          applicationName.includes(normalized) ||
          lifecycleName.includes(normalized)
        );
      })
    );
  }

  private nextSortOrder(): number {
    const maxSortOrder = Math.max(0, ...this.channels().map((channel) => channel.sortOrder));
    return maxSortOrder + 10;
  }

  private ruleListFromText(value: string): string[] {
    return value
      .split(/\r?\n/)
      .map((item) => item.trim())
      .filter((item) => item.length > 0);
  }

  private ruleListToText(values: string[]): string {
    return values.join('\n');
  }
}
