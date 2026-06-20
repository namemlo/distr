import {DecimalPipe} from '@angular/common';
import {ChangeDetectionStrategy, Component, inject, signal, TemplateRef, viewChild} from '@angular/core';
import {FormArray, FormControl, FormGroup, ReactiveFormsModule, Validators} from '@angular/forms';
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
import {EnvironmentsService} from '../services/environments.service';
import {LifecyclesService} from '../services/lifecycles.service';
import {DialogRef, OverlayService} from '../services/overlay.service';
import {ToastService} from '../services/toast.service';
import {Environment} from '../types/environment';
import {
  CreateUpdateLifecyclePhaseRequest,
  CreateUpdateLifecycleRequest,
  Lifecycle,
  LifecyclePhase,
} from '../types/lifecycle';

type PhaseFormGroup = FormGroup<{
  name: FormControl<string>;
  description: FormControl<string>;
  sortOrder: FormControl<number>;
  environmentIds: FormControl<string[]>;
  optional: FormControl<boolean>;
  automaticPromotion: FormControl<boolean>;
  minimumSuccessfulDeployments: FormControl<number>;
}>;

@Component({
  templateUrl: './lifecycles.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  imports: [ReactiveFormsModule, FontAwesomeModule, DecimalPipe, AutotrimDirective],
})
export class LifecyclesComponent {
  protected readonly faMagnifyingGlass = faMagnifyingGlass;
  protected readonly faPlus = faPlus;
  protected readonly faLayerGroup = faLayerGroup;
  protected readonly faTrash = faTrash;
  protected readonly faXmark = faXmark;
  protected readonly faEdit = faEdit;
  protected readonly faCheck = faCheck;
  protected readonly faRotateRight = faRotateRight;
  protected readonly faTriangleExclamation = faTriangleExclamation;

  private readonly lifecyclesService = inject(LifecyclesService);
  private readonly environmentsService = inject(EnvironmentsService);
  private readonly toast = inject(ToastService);
  private readonly overlay = inject(OverlayService);

  protected readonly lifecycles = signal<Lifecycle[]>([]);
  protected readonly environments = signal<Environment[]>([]);
  protected readonly loading = signal(true);
  protected readonly loadError = signal<string | undefined>(undefined);
  protected readonly formLoading = signal(false);

  protected readonly filterForm = new FormGroup({
    search: new FormControl('', {nonNullable: true}),
  });

  protected readonly lifecycleForm = new FormGroup({
    id: new FormControl('', {nonNullable: true}),
    name: new FormControl('', {nonNullable: true, validators: [Validators.required]}),
    description: new FormControl('', {nonNullable: true}),
    sortOrder: new FormControl(0, {nonNullable: true, validators: [Validators.required, Validators.min(0)]}),
    phases: new FormArray<PhaseFormGroup>([]),
  });

  protected readonly filteredLifecycles = signal<Lifecycle[]>([]);

  private readonly lifecycleDialog = viewChild.required<TemplateRef<unknown>>('lifecycleDialog');
  private modalRef?: DialogRef;

  constructor() {
    this.filterForm.controls.search.valueChanges.pipe(startWith('')).subscribe((search) => {
      this.applyFilter(search);
    });
    this.load();
  }

  protected get phasesArray(): FormArray<PhaseFormGroup> {
    return this.lifecycleForm.controls.phases;
  }

  protected load() {
    this.loading.set(true);
    this.loadError.set(undefined);
    forkJoin({
      lifecycles: this.lifecyclesService.list(),
      environments: this.environmentsService.list(),
    }).subscribe({
      next: ({lifecycles, environments}) => {
        this.lifecycles.set(lifecycles);
        this.environments.set(environments);
        this.applyFilter(this.filterForm.controls.search.value);
        this.loading.set(false);
      },
      error: (e) => {
        this.loadError.set(getFormDisplayedError(e) ?? 'Failed to load lifecycles.');
        this.loading.set(false);
      },
    });
  }

  protected showCreateDialog() {
    this.closeDialog(false);
    this.clearPhases();
    this.lifecycleForm.reset({
      id: '',
      name: '',
      description: '',
      sortOrder: this.nextSortOrder(),
    });
    this.addPhase();
    this.modalRef = this.overlay.showModal(this.lifecycleDialog());
  }

  protected showUpdateDialog(lifecycle: Lifecycle) {
    this.closeDialog(false);
    this.clearPhases();
    this.lifecycleForm.patchValue({
      id: lifecycle.id,
      name: lifecycle.name,
      description: lifecycle.description,
      sortOrder: lifecycle.sortOrder,
    });
    for (const phase of lifecycle.phases) {
      this.addPhase(phase);
    }
    if (this.phasesArray.length === 0) {
      this.addPhase();
    }
    this.modalRef = this.overlay.showModal(this.lifecycleDialog());
  }

  protected closeDialog(reset = true) {
    this.modalRef?.close();
    if (reset) {
      this.lifecycleForm.reset();
      this.clearPhases();
    }
  }

  protected addPhase(phase?: LifecyclePhase | CreateUpdateLifecyclePhaseRequest) {
    this.phasesArray.push(this.createPhaseGroup(phase));
  }

  protected removePhase(index: number) {
    this.phasesArray.removeAt(index);
  }

  protected async submitForm() {
    this.lifecycleForm.markAllAsTouched();
    if (this.lifecycleForm.invalid || this.phasesArray.length === 0) {
      return;
    }

    this.formLoading.set(true);
    try {
      const value = this.lifecycleForm.getRawValue();
      const request: CreateUpdateLifecycleRequest = {
        name: value.name,
        description: value.description,
        sortOrder: value.sortOrder,
        phases: value.phases,
      };
      if (value.id) {
        await firstValueFrom(this.lifecyclesService.update(value.id, request));
      } else {
        await firstValueFrom(this.lifecyclesService.create(request));
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

  protected delete(lifecycle: Lifecycle) {
    this.overlay
      .confirm({
        message: {
          message: 'Are you sure you want to delete this lifecycle?',
        },
        requiredConfirmInputText: lifecycle.name,
      })
      .pipe(
        filter((it) => it === true),
        map(() => lifecycle.id)
      )
      .subscribe({
        next: async (id) => {
          try {
            await firstValueFrom(this.lifecyclesService.delete(id));
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

  protected environmentNames(environmentIds: string[]): string {
    const names = environmentIds
      .map((id) => this.environments().find((environment) => environment.id === id)?.name)
      .filter((name): name is string => typeof name === 'string');
    return names.join(', ');
  }

  private createPhaseGroup(phase?: LifecyclePhase | CreateUpdateLifecyclePhaseRequest): PhaseFormGroup {
    return new FormGroup({
      name: new FormControl(phase?.name ?? '', {nonNullable: true, validators: [Validators.required]}),
      description: new FormControl(phase?.description ?? '', {nonNullable: true}),
      sortOrder: new FormControl(phase?.sortOrder ?? this.nextPhaseSortOrder(), {
        nonNullable: true,
        validators: [Validators.required, Validators.min(0)],
      }),
      environmentIds: new FormControl(phase?.environmentIds ?? [], {
        nonNullable: true,
        validators: [Validators.required],
      }),
      optional: new FormControl(phase?.optional ?? false, {nonNullable: true}),
      automaticPromotion: new FormControl(phase?.automaticPromotion ?? false, {nonNullable: true}),
      minimumSuccessfulDeployments: new FormControl(phase?.minimumSuccessfulDeployments ?? 0, {
        nonNullable: true,
        validators: [Validators.required, Validators.min(0)],
      }),
    });
  }

  private clearPhases() {
    while (this.phasesArray.length > 0) {
      this.phasesArray.removeAt(0);
    }
  }

  private applyFilter(search: string) {
    const normalized = search.toLowerCase();
    this.filteredLifecycles.set(
      this.lifecycles().filter(
        (lifecycle) =>
          normalized.length === 0 ||
          lifecycle.name.toLowerCase().includes(normalized) ||
          lifecycle.description.toLowerCase().includes(normalized)
      )
    );
  }

  private nextSortOrder(): number {
    const maxSortOrder = Math.max(0, ...this.lifecycles().map((lifecycle) => lifecycle.sortOrder));
    return maxSortOrder + 10;
  }

  private nextPhaseSortOrder(): number {
    const maxSortOrder = Math.max(0, ...this.phasesArray.controls.map((phase) => phase.controls.sortOrder.value));
    return maxSortOrder + 10;
  }
}
