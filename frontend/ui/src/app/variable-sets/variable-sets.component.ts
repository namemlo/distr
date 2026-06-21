import {DecimalPipe} from '@angular/common';
import {ChangeDetectionStrategy, Component, inject, signal, TemplateRef, viewChild} from '@angular/core';
import {FormArray, FormBuilder, FormControl, FormGroup, ReactiveFormsModule, Validators} from '@angular/forms';
import {Application} from '@distr-sh/distr-sdk';
import {FontAwesomeModule} from '@fortawesome/angular-fontawesome';
import {
  faCodeBranch,
  faEdit,
  faKey,
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
import {DialogRef, OverlayService} from '../services/overlay.service';
import {SecretsService} from '../services/secrets.service';
import {ToastService} from '../services/toast.service';
import {VariableSetsService} from '../services/variable-sets.service';
import {Secret} from '../types/secret';
import {
  CreateUpdateVariableSetRequest,
  Variable,
  VariableRequest,
  VariableSet,
  VariableType,
} from '../types/variable-set';

interface VariableTypeOption {
  value: VariableType;
  label: string;
}

type VariableForm = FormGroup<{
  key: FormControl<string>;
  description: FormControl<string>;
  type: FormControl<VariableType>;
  isRequired: FormControl<boolean>;
  defaultValueText: FormControl<string>;
  referenceId: FormControl<string>;
  referenceName: FormControl<string>;
}>;

const variableTypeOptions: VariableTypeOption[] = [
  {value: 'string', label: 'String'},
  {value: 'number', label: 'Number'},
  {value: 'boolean', label: 'Boolean'},
  {value: 'json', label: 'JSON'},
  {value: 'secret_reference', label: 'Secret Reference'},
  {value: 'account_reference', label: 'Account Reference'},
  {value: 'certificate_reference', label: 'Certificate Reference'},
];

@Component({
  templateUrl: './variable-sets.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  imports: [ReactiveFormsModule, FontAwesomeModule, DecimalPipe, AutotrimDirective],
})
export class VariableSetsComponent {
  protected readonly faMagnifyingGlass = faMagnifyingGlass;
  protected readonly faPlus = faPlus;
  protected readonly faCodeBranch = faCodeBranch;
  protected readonly faKey = faKey;
  protected readonly faTrash = faTrash;
  protected readonly faXmark = faXmark;
  protected readonly faEdit = faEdit;
  protected readonly faRotateRight = faRotateRight;
  protected readonly faTriangleExclamation = faTriangleExclamation;

  protected readonly variableTypeOptions = variableTypeOptions;

  private readonly variableSetsService = inject(VariableSetsService);
  private readonly applicationsService = inject(ApplicationsService);
  private readonly secretsService = inject(SecretsService);
  private readonly toast = inject(ToastService);
  private readonly overlay = inject(OverlayService);
  private readonly fb = inject(FormBuilder).nonNullable;

  protected readonly variableSets = signal<VariableSet[]>([]);
  protected readonly applications = signal<Application[]>([]);
  protected readonly secrets = signal<Secret[]>([]);
  protected readonly selectedApplicationIds = signal<string[]>([]);
  protected readonly loading = signal(true);
  protected readonly loadError = signal<string | undefined>(undefined);
  protected readonly formLoading = signal(false);

  protected readonly filterForm = this.fb.group({
    search: this.fb.control(''),
  });

  protected readonly variableSetForm = this.fb.group({
    id: this.fb.control(''),
    name: this.fb.control('', [Validators.required]),
    description: this.fb.control(''),
    sortOrder: this.fb.control(0, [Validators.required, Validators.min(0)]),
    variables: this.fb.array<VariableForm>([]),
  });

  protected readonly filteredVariableSets = signal<VariableSet[]>([]);

  private readonly variableSetDialog = viewChild.required<TemplateRef<unknown>>('variableSetDialog');
  private modalRef?: DialogRef;

  constructor() {
    this.filterForm.controls.search.valueChanges.pipe(startWith('')).subscribe((search) => {
      this.applyFilter(search);
    });
    this.load();
  }

  protected get variables(): FormArray<VariableForm> {
    return this.variableSetForm.controls.variables;
  }

  protected variableControls(): VariableForm[] {
    return this.variables.controls;
  }

  protected load() {
    this.loading.set(true);
    this.loadError.set(undefined);
    forkJoin({
      variableSets: this.variableSetsService.list(),
      applications: this.applicationsService.list(),
      secrets: this.secretsService.list(),
    }).subscribe({
      next: ({variableSets, applications, secrets}) => {
        this.variableSets.set(variableSets);
        this.applications.set(applications);
        this.secrets.set(secrets.filter((secret) => !secret.customerOrganizationId));
        this.applyFilter(this.filterForm.controls.search.value);
        this.loading.set(false);
      },
      error: (e) => {
        this.loadError.set(getFormDisplayedError(e) ?? 'Failed to load variable sets.');
        this.loading.set(false);
      },
    });
  }

  protected showCreateDialog() {
    this.closeDialog(false);
    this.selectedApplicationIds.set([]);
    this.variableSetForm.reset({
      id: '',
      name: '',
      description: '',
      sortOrder: this.nextSortOrder(),
    });
    this.clearVariables();
    this.addVariable();
    this.modalRef = this.overlay.showModal(this.variableSetDialog());
  }

  protected showUpdateDialog(variableSet: VariableSet) {
    this.closeDialog(false);
    this.selectedApplicationIds.set([...variableSet.applicationIds]);
    this.variableSetForm.reset({
      id: variableSet.id,
      name: variableSet.name,
      description: variableSet.description,
      sortOrder: variableSet.sortOrder,
    });
    this.clearVariables();
    for (const variable of variableSet.variables) {
      this.addVariable(variable);
    }
    this.modalRef = this.overlay.showModal(this.variableSetDialog());
  }

  protected closeDialog(reset = true) {
    this.modalRef?.close();
    if (reset) {
      this.variableSetForm.reset();
      this.selectedApplicationIds.set([]);
      this.clearVariables();
    }
  }

  protected addVariable(variable?: Variable) {
    this.variables.push(this.createVariableForm(variable));
  }

  protected removeVariable(index: number) {
    this.variables.removeAt(index);
  }

  protected onVariableTypeChanged(index: number) {
    const control = this.variables.at(index);
    const type = control.controls.type.value;
    if (this.isReferenceType(type)) {
      control.patchValue({defaultValueText: ''});
    } else {
      control.patchValue({referenceId: '', referenceName: ''});
      if (type === 'boolean' && control.controls.defaultValueText.value.trim() === '') {
        control.patchValue({defaultValueText: 'false'});
      }
    }
  }

  protected toggleApplication(applicationId: string, checked: boolean) {
    const current = new Set(this.selectedApplicationIds());
    if (checked) {
      current.add(applicationId);
    } else {
      current.delete(applicationId);
    }
    this.selectedApplicationIds.set(
      this.applications()
        .map((app) => app.id)
        .filter((id): id is string => typeof id === 'string' && current.has(id))
    );
  }

  protected isApplicationSelected(applicationId: string): boolean {
    return this.selectedApplicationIds().includes(applicationId);
  }

  protected async submitForm() {
    this.variableSetForm.markAllAsTouched();
    if (this.variableSetForm.invalid) {
      return;
    }

    let request: CreateUpdateVariableSetRequest;
    try {
      request = this.variableSetRequestFromForm();
    } catch (e) {
      if (e instanceof Error) {
        this.toast.error(e.message);
      }
      return;
    }

    this.formLoading.set(true);
    try {
      const id = this.variableSetForm.controls.id.value;
      if (id) {
        await firstValueFrom(this.variableSetsService.update(id, request));
      } else {
        await firstValueFrom(this.variableSetsService.create(request));
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

  protected delete(variableSet: VariableSet) {
    this.overlay
      .confirm({
        message: {
          message: 'Are you sure you want to delete this variable set?',
        },
        requiredConfirmInputText: variableSet.name,
      })
      .pipe(
        filter((it) => it === true),
        map(() => variableSet.id)
      )
      .subscribe({
        next: async (id) => {
          try {
            await firstValueFrom(this.variableSetsService.delete(id));
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

  protected applicationNames(applicationIds: string[]): string {
    if (applicationIds.length === 0) {
      return '-';
    }
    return applicationIds.map((id) => this.applicationName(id)).join(', ');
  }

  protected applicationName(applicationId: string): string {
    return this.applications().find((application) => application.id === applicationId)?.name ?? applicationId;
  }

  protected variableSummary(variableSet: VariableSet): string {
    if (variableSet.variables.length === 0) {
      return 'No variables';
    }
    const keys = variableSet.variables.slice(0, 3).map((variable) => variable.key);
    if (variableSet.variables.length > keys.length) {
      keys.push(`+${variableSet.variables.length - keys.length}`);
    }
    return keys.join(', ');
  }

  protected isReferenceType(type: VariableType): boolean {
    return type === 'secret_reference' || type === 'account_reference' || type === 'certificate_reference';
  }

  protected isSecretReference(type: VariableType): boolean {
    return type === 'secret_reference';
  }

  protected isBooleanType(type: VariableType): boolean {
    return type === 'boolean';
  }

  protected isJsonType(type: VariableType): boolean {
    return type === 'json';
  }

  private applyFilter(search: string) {
    const normalized = search.toLowerCase();
    this.filteredVariableSets.set(
      this.variableSets().filter((variableSet) => {
        const applications = this.applicationNames(variableSet.applicationIds).toLowerCase();
        const variables = variableSet.variables
          .map((variable) => variable.key)
          .join(' ')
          .toLowerCase();
        return (
          normalized.length === 0 ||
          variableSet.name.toLowerCase().includes(normalized) ||
          variableSet.description.toLowerCase().includes(normalized) ||
          applications.includes(normalized) ||
          variables.includes(normalized)
        );
      })
    );
  }

  private nextSortOrder(): number {
    const maxSortOrder = Math.max(0, ...this.variableSets().map((variableSet) => variableSet.sortOrder));
    return maxSortOrder + 10;
  }

  private clearVariables() {
    while (this.variables.length > 0) {
      this.variables.removeAt(0);
    }
  }

  private createVariableForm(variable?: Variable): VariableForm {
    const type = variable?.type ?? 'string';
    return this.fb.group({
      key: this.fb.control(variable?.key ?? '', [Validators.required]),
      description: this.fb.control(variable?.description ?? ''),
      type: this.fb.control(type, [Validators.required]),
      isRequired: this.fb.control(variable?.isRequired ?? false),
      defaultValueText: this.fb.control(this.defaultValueToText(variable?.defaultValue)),
      referenceId: this.fb.control(variable?.referenceId ?? ''),
      referenceName: this.fb.control(variable?.referenceName ?? ''),
    }) as VariableForm;
  }

  private variableSetRequestFromForm(): CreateUpdateVariableSetRequest {
    const value = this.variableSetForm.getRawValue();
    return {
      name: value.name,
      description: value.description,
      sortOrder: value.sortOrder,
      applicationIds: this.selectedApplicationIds(),
      variables: this.variables.controls.map((control) => this.variableRequestFromForm(control)),
    };
  }

  private variableRequestFromForm(control: VariableForm): VariableRequest {
    const value = control.getRawValue();
    const type = value.type;
    const variable: VariableRequest = {
      key: value.key,
      description: value.description,
      type,
      isRequired: value.isRequired,
    };

    if (this.isReferenceType(type)) {
      const referenceId = value.referenceId.trim();
      if (referenceId) {
        variable.referenceId = referenceId;
      }
      if (type !== 'secret_reference') {
        variable.referenceName = value.referenceName.trim();
      }
      return variable;
    }

    if (value.isRequired && value.defaultValueText.trim() === '') {
      return variable;
    }
    variable.defaultValue = this.parseDefaultValue(type, value.defaultValueText);
    return variable;
  }

  private parseDefaultValue(type: VariableType, value: string): unknown {
    if (type === 'string') {
      return value;
    }
    const trimmed = value.trim();
    if (trimmed === '') {
      throw new Error('Default value is required unless the variable is required.');
    }
    if (type === 'number') {
      const parsed = Number(trimmed);
      if (Number.isNaN(parsed)) {
        throw new Error('Number variables require a numeric default value.');
      }
      return parsed;
    }
    if (type === 'boolean') {
      return trimmed === 'true';
    }
    if (type === 'json') {
      try {
        return JSON.parse(trimmed);
      } catch {
        throw new Error('JSON variables require valid JSON.');
      }
    }
    return undefined;
  }

  private defaultValueToText(value: unknown): string {
    if (value === undefined || value === null) {
      return '';
    }
    if (typeof value === 'string') {
      return value;
    }
    if (typeof value === 'object') {
      return JSON.stringify(value, null, 2);
    }
    return String(value);
  }
}
