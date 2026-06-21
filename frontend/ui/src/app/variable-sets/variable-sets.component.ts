import {DecimalPipe, JsonPipe} from '@angular/common';
import {ChangeDetectionStrategy, Component, inject, signal, TemplateRef, viewChild} from '@angular/core';
import {FormArray, FormBuilder, FormControl, FormGroup, ReactiveFormsModule, Validators} from '@angular/forms';
import {Application, DeploymentTarget} from '@distr-sh/distr-sdk';
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
import {catchError, filter, firstValueFrom, forkJoin, map, of, startWith} from 'rxjs';
import {getFormDisplayedError} from '../../util/errors';
import {AutotrimDirective} from '../directives/autotrim.directive';
import {ApplicationsService} from '../services/applications.service';
import {ChannelsService} from '../services/channels.service';
import {CustomerOrganizationsService} from '../services/customer-organizations.service';
import {DeploymentTargetsService} from '../services/deployment-targets.service';
import {EnvironmentsService} from '../services/environments.service';
import {DialogRef, OverlayService} from '../services/overlay.service';
import {SecretsService} from '../services/secrets.service';
import {ToastService} from '../services/toast.service';
import {VariableSetsService} from '../services/variable-sets.service';
import {Channel} from '../types/channel';
import {Environment} from '../types/environment';
import {Secret} from '../types/secret';
import {
  CreateUpdateVariableSetRequest,
  ResolvedVariable,
  Variable,
  VariableRequest,
  VariableScope,
  VariableScopedValue,
  VariableScopedValueRequest,
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
  scopedValues: FormArray<ScopedValueForm>;
}>;

type ScopeKind =
  | 'application'
  | 'channel'
  | 'environment'
  | 'environmentTargetTag'
  | 'tenantEnvironment'
  | 'tenantEnvironmentChannel'
  | 'tenantEnvironmentTarget'
  | 'tenantEnvironmentTargetChannelStep';

type ScopedValueForm = FormGroup<{
  scopeKind: FormControl<ScopeKind>;
  customerOrganizationId: FormControl<string>;
  environmentId: FormControl<string>;
  channelId: FormControl<string>;
  deploymentTargetId: FormControl<string>;
  applicationId: FormControl<string>;
  targetTag: FormControl<string>;
  processStepKey: FormControl<string>;
  sortOrder: FormControl<number>;
  valueText: FormControl<string>;
  referenceId: FormControl<string>;
  referenceName: FormControl<string>;
}>;

interface CustomerOrganizationOption {
  id: string;
  name: string;
}

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
  imports: [ReactiveFormsModule, FontAwesomeModule, DecimalPipe, JsonPipe, AutotrimDirective],
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
  private readonly channelsService = inject(ChannelsService);
  private readonly environmentsService = inject(EnvironmentsService);
  private readonly deploymentTargetsService = inject(DeploymentTargetsService);
  private readonly customerOrganizationsService = inject(CustomerOrganizationsService);
  private readonly secretsService = inject(SecretsService);
  private readonly toast = inject(ToastService);
  private readonly overlay = inject(OverlayService);
  private readonly fb = inject(FormBuilder).nonNullable;

  protected readonly variableSets = signal<VariableSet[]>([]);
  protected readonly applications = signal<Application[]>([]);
  protected readonly channels = signal<Channel[]>([]);
  protected readonly environments = signal<Environment[]>([]);
  protected readonly deploymentTargets = signal<DeploymentTarget[]>([]);
  protected readonly customerOrganizations = signal<CustomerOrganizationOption[]>([]);
  protected readonly secrets = signal<Secret[]>([]);
  protected readonly selectedApplicationIds = signal<string[]>([]);
  protected readonly loading = signal(true);
  protected readonly loadError = signal<string | undefined>(undefined);
  protected readonly formLoading = signal(false);
  protected readonly previewLoading = signal(false);
  protected readonly previewError = signal<string | undefined>(undefined);
  protected readonly previewVariableSet = signal<VariableSet | undefined>(undefined);
  protected readonly previewResults = signal<ResolvedVariable[]>([]);

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

  protected readonly previewForm = this.fb.group({
    applicationId: this.fb.control(''),
    channelId: this.fb.control(''),
    environmentId: this.fb.control(''),
    customerOrganizationId: this.fb.control(''),
    deploymentTargetId: this.fb.control(''),
    targetTagsText: this.fb.control(''),
    processStepKey: this.fb.control(''),
  });

  protected readonly filteredVariableSets = signal<VariableSet[]>([]);

  private readonly variableSetDialog = viewChild.required<TemplateRef<unknown>>('variableSetDialog');
  private readonly variablePreviewDialog = viewChild.required<TemplateRef<unknown>>('variablePreviewDialog');
  private modalRef?: DialogRef;
  private previewModalRef?: DialogRef;

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
      channels: this.channelsService.list().pipe(catchError(() => of([] as Channel[]))),
      environments: this.environmentsService.list().pipe(catchError(() => of([] as Environment[]))),
      deploymentTargets: this.deploymentTargetsService.list().pipe(catchError(() => of([] as DeploymentTarget[]))),
      customerOrganizations: this.customerOrganizationsService
        .getCustomerOrganizations()
        .pipe(catchError(() => of([] as CustomerOrganizationOption[]))),
    }).subscribe({
      next: ({
        variableSets,
        applications,
        secrets,
        channels,
        environments,
        deploymentTargets,
        customerOrganizations,
      }) => {
        this.variableSets.set(variableSets);
        this.applications.set(applications);
        this.secrets.set(secrets.filter((secret) => !secret.customerOrganizationId));
        this.channels.set(channels);
        this.environments.set(environments);
        this.deploymentTargets.set(deploymentTargets);
        this.customerOrganizations.set(customerOrganizations.map((it) => ({id: it.id, name: it.name})));
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

  protected closePreviewDialog() {
    this.previewModalRef?.close();
    this.previewVariableSet.set(undefined);
    this.previewResults.set([]);
    this.previewError.set(undefined);
    this.previewForm.reset();
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

  protected scopedValueControls(variable: VariableForm): ScopedValueForm[] {
    return variable.controls.scopedValues.controls;
  }

  protected addScopedValue(variableIndex: number, scopedValue?: VariableScopedValue) {
    this.variables.at(variableIndex).controls.scopedValues.push(this.createScopedValueForm(scopedValue));
  }

  protected removeScopedValue(variableIndex: number, scopedValueIndex: number) {
    this.variables.at(variableIndex).controls.scopedValues.removeAt(scopedValueIndex);
  }

  protected hasScopedValueConflict(variable: VariableForm): boolean {
    const scopes = new Set<string>();
    for (const scopedValue of this.scopedValueControls(variable)) {
      const key = JSON.stringify(this.scopeFromScopedValueForm(scopedValue));
      if (scopes.has(key)) {
        return true;
      }
      scopes.add(key);
    }
    return false;
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

  protected showPreviewDialog(variableSet: VariableSet) {
    this.previewVariableSet.set(variableSet);
    this.previewResults.set([]);
    this.previewError.set(undefined);
    this.previewForm.reset({
      applicationId: variableSet.applicationIds[0] ?? '',
      channelId: '',
      environmentId: '',
      customerOrganizationId: '',
      deploymentTargetId: '',
      targetTagsText: '',
      processStepKey: '',
    });
    this.previewModalRef = this.overlay.showModal(this.variablePreviewDialog());
  }

  protected async loadPreview() {
    const variableSet = this.previewVariableSet();
    if (!variableSet) {
      return;
    }
    this.previewLoading.set(true);
    this.previewError.set(undefined);
    try {
      const request = this.previewRequestFromForm(variableSet);
      const results = await firstValueFrom(this.variableSetsService.resolvePreview(request));
      this.previewResults.set(results);
    } catch (e) {
      this.previewError.set(getFormDisplayedError(e) ?? 'Failed to preview variable resolution.');
    } finally {
      this.previewLoading.set(false);
    }
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
    const group = this.fb.group({
      key: this.fb.control(variable?.key ?? '', [Validators.required]),
      description: this.fb.control(variable?.description ?? ''),
      type: this.fb.control(type, [Validators.required]),
      isRequired: this.fb.control(variable?.isRequired ?? false),
      defaultValueText: this.fb.control(this.defaultValueToText(variable?.defaultValue)),
      referenceId: this.fb.control(variable?.referenceId ?? ''),
      referenceName: this.fb.control(variable?.referenceName ?? ''),
      scopedValues: this.fb.array<ScopedValueForm>([]),
    }) as VariableForm;
    for (const scopedValue of variable?.scopedValues ?? []) {
      group.controls.scopedValues.push(this.createScopedValueForm(scopedValue));
    }
    return group;
  }

  private createScopedValueForm(scopedValue?: VariableScopedValue): ScopedValueForm {
    const scope = scopedValue?.scope ?? {};
    return this.fb.group({
      scopeKind: this.fb.control(this.scopeKindFromScope(scope), [Validators.required]),
      customerOrganizationId: this.fb.control(scope.customerOrganizationId ?? ''),
      environmentId: this.fb.control(scope.environmentId ?? ''),
      channelId: this.fb.control(scope.channelId ?? ''),
      deploymentTargetId: this.fb.control(scope.deploymentTargetId ?? ''),
      applicationId: this.fb.control(scope.applicationId ?? ''),
      targetTag: this.fb.control(scope.targetTag ?? ''),
      processStepKey: this.fb.control(scope.processStepKey ?? ''),
      sortOrder: this.fb.control(scopedValue?.sortOrder ?? 0, [Validators.required, Validators.min(0)]),
      valueText: this.fb.control(this.defaultValueToText(scopedValue?.value)),
      referenceId: this.fb.control(scopedValue?.referenceId ?? ''),
      referenceName: this.fb.control(scopedValue?.referenceName ?? ''),
    }) as ScopedValueForm;
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
      variable.scopedValues = this.scopedValueRequestsFromForm(control, type);
      return variable;
    }

    if (value.isRequired && value.defaultValueText.trim() === '') {
      variable.scopedValues = this.scopedValueRequestsFromForm(control, type);
      return variable;
    }
    variable.defaultValue = this.parseDefaultValue(type, value.defaultValueText);
    variable.scopedValues = this.scopedValueRequestsFromForm(control, type);
    return variable;
  }

  private scopedValueRequestsFromForm(
    control: VariableForm,
    variableType: VariableType
  ): VariableScopedValueRequest[] | undefined {
    const scopedValues = this.scopedValueControls(control).map((scopedValue) =>
      this.scopedValueRequestFromForm(scopedValue, variableType)
    );
    return scopedValues.length > 0 ? scopedValues : undefined;
  }

  private scopedValueRequestFromForm(control: ScopedValueForm, variableType: VariableType): VariableScopedValueRequest {
    const value = control.getRawValue();
    const request: VariableScopedValueRequest = {
      scope: this.scopeFromScopedValueForm(control),
      sortOrder: value.sortOrder,
    };
    if (this.isReferenceType(variableType)) {
      const referenceId = value.referenceId.trim();
      if (referenceId) {
        request.referenceId = referenceId;
      }
      if (variableType !== 'secret_reference') {
        request.referenceName = value.referenceName.trim();
      }
      return request;
    }
    request.value = this.parseDefaultValue(variableType, value.valueText);
    return request;
  }

  private scopeFromScopedValueForm(control: ScopedValueForm): VariableScope {
    const value = control.getRawValue();
    const scope: VariableScope = {};
    if (value.scopeKind === 'application') {
      scope.applicationId = value.applicationId;
    }
    if (
      value.scopeKind === 'channel' ||
      value.scopeKind === 'tenantEnvironmentChannel' ||
      value.scopeKind === 'tenantEnvironmentTargetChannelStep'
    ) {
      scope.channelId = value.channelId;
    }
    if (
      value.scopeKind === 'environment' ||
      value.scopeKind === 'environmentTargetTag' ||
      value.scopeKind === 'tenantEnvironment' ||
      value.scopeKind === 'tenantEnvironmentChannel' ||
      value.scopeKind === 'tenantEnvironmentTarget' ||
      value.scopeKind === 'tenantEnvironmentTargetChannelStep'
    ) {
      scope.environmentId = value.environmentId;
    }
    if (
      value.scopeKind === 'tenantEnvironment' ||
      value.scopeKind === 'tenantEnvironmentChannel' ||
      value.scopeKind === 'tenantEnvironmentTarget' ||
      value.scopeKind === 'tenantEnvironmentTargetChannelStep'
    ) {
      scope.customerOrganizationId = value.customerOrganizationId;
    }
    if (value.scopeKind === 'tenantEnvironmentTarget' || value.scopeKind === 'tenantEnvironmentTargetChannelStep') {
      scope.deploymentTargetId = value.deploymentTargetId;
    }
    if (value.scopeKind === 'environmentTargetTag') {
      scope.targetTag = value.targetTag.trim();
    }
    if (value.scopeKind === 'tenantEnvironmentTargetChannelStep') {
      scope.processStepKey = value.processStepKey.trim();
    }
    return this.stripEmptyScope(scope);
  }

  private previewRequestFromForm(variableSet: VariableSet) {
    const value = this.previewForm.getRawValue();
    return {
      variableSetIds: [variableSet.id],
      scope: this.stripEmptyPreviewScope({
        applicationId: value.applicationId,
        channelId: value.channelId,
        environmentId: value.environmentId,
        customerOrganizationId: value.customerOrganizationId,
        deploymentTargetId: value.deploymentTargetId,
        targetTags: this.textToStringList(value.targetTagsText),
        processStepKey: value.processStepKey.trim(),
      }),
      promptedValues: [],
    };
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

  private scopeKindFromScope(scope: VariableScope): ScopeKind {
    if (scope.applicationId) {
      return 'application';
    }
    if (
      scope.customerOrganizationId &&
      scope.environmentId &&
      scope.deploymentTargetId &&
      scope.channelId &&
      scope.processStepKey
    ) {
      return 'tenantEnvironmentTargetChannelStep';
    }
    if (scope.customerOrganizationId && scope.environmentId && scope.deploymentTargetId) {
      return 'tenantEnvironmentTarget';
    }
    if (scope.customerOrganizationId && scope.environmentId && scope.channelId) {
      return 'tenantEnvironmentChannel';
    }
    if (scope.customerOrganizationId && scope.environmentId) {
      return 'tenantEnvironment';
    }
    if (scope.environmentId && scope.targetTag) {
      return 'environmentTargetTag';
    }
    if (scope.environmentId) {
      return 'environment';
    }
    if (scope.channelId) {
      return 'channel';
    }
    return 'application';
  }

  private stripEmptyScope(scope: VariableScope): VariableScope {
    return Object.fromEntries(Object.entries(scope).filter(([, value]) => value !== undefined && value !== ''));
  }

  private stripEmptyPreviewScope<T extends Record<string, unknown>>(scope: T): T {
    return Object.fromEntries(
      Object.entries(scope).filter(([, value]) => {
        if (Array.isArray(value)) {
          return value.length > 0;
        }
        return value !== undefined && value !== '';
      })
    ) as T;
  }

  private textToStringList(value: string): string[] {
    return value
      .split(',')
      .map((it) => it.trim())
      .filter((it) => it.length > 0);
  }
}
