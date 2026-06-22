import {DatePipe, JsonPipe} from '@angular/common';
import {ChangeDetectionStrategy, Component, inject, signal, TemplateRef, viewChild} from '@angular/core';
import {FormBuilder, ReactiveFormsModule} from '@angular/forms';
import {FontAwesomeModule} from '@fortawesome/angular-fontawesome';
import {
  faDownload,
  faEye,
  faMagnifyingGlass,
  faRotateRight,
  faTriangleExclamation,
  faXmark,
} from '@fortawesome/free-solid-svg-icons';
import {firstValueFrom, startWith} from 'rxjs';
import {getFormDisplayedError} from '../../util/errors';
import {AutotrimDirective} from '../directives/autotrim.directive';
import {DialogRef, OverlayService} from '../services/overlay.service';
import {StepTemplatesService} from '../services/step-templates.service';
import {ToastService} from '../services/toast.service';
import {ImportStepTemplateRequest, StepTemplate} from '../types/step-template';

interface CatalogStepTemplate {
  key: string;
  request: ImportStepTemplateRequest;
}

const builtInStepTemplateCatalog: CatalogStepTemplate[] = [
  {
    key: 'builtin/http-health-check:1.0.0',
    request: {
      sourceType: 'builtin',
      sourceRef: 'builtin/http-health-check',
      name: 'HTTP health check',
      description: 'Checks that an HTTP endpoint returns a healthy status.',
      category: 'Health',
      version: '1.0.0',
      actionType: 'distr.http.check',
      executionLocation: 'hub',
      inputSchema: {type: 'object', additionalProperties: true},
      outputSchema: {type: 'object', additionalProperties: true},
      defaultInputBindings: {url: 'https://example.com/health'},
      minimumAgentVersion: '1.0.0',
      compatibleActionVersion: '1',
      runtimeCompatibilityNotes: 'Uses the built-in HTTP check action.',
      deprecated: false,
    },
  },
  {
    key: 'builtin/wait-cooldown:1.0.0',
    request: {
      sourceType: 'builtin',
      sourceRef: 'builtin/wait-cooldown',
      name: 'Wait cooldown',
      description: 'Adds a bounded wait step between dependent actions.',
      category: 'Control',
      version: '1.0.0',
      actionType: 'distr.wait',
      executionLocation: 'target',
      inputSchema: {type: 'object', additionalProperties: true},
      outputSchema: {type: 'object', additionalProperties: true},
      defaultInputBindings: {durationSeconds: 30},
      minimumAgentVersion: '1.0.0',
      compatibleActionVersion: '1',
      runtimeCompatibilityNotes: 'Uses the built-in wait action.',
      deprecated: false,
    },
  },
];

@Component({
  templateUrl: './step-templates.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  imports: [ReactiveFormsModule, FontAwesomeModule, DatePipe, JsonPipe, AutotrimDirective],
})
export class StepTemplatesComponent {
  protected readonly faMagnifyingGlass = faMagnifyingGlass;
  protected readonly faDownload = faDownload;
  protected readonly faEye = faEye;
  protected readonly faRotateRight = faRotateRight;
  protected readonly faTriangleExclamation = faTriangleExclamation;
  protected readonly faXmark = faXmark;

  private readonly stepTemplatesService = inject(StepTemplatesService);
  private readonly toast = inject(ToastService);
  private readonly overlay = inject(OverlayService);
  private readonly fb = inject(FormBuilder).nonNullable;

  protected readonly catalogTemplates = builtInStepTemplateCatalog;
  protected readonly templates = signal<StepTemplate[]>([]);
  protected readonly filteredTemplates = signal<StepTemplate[]>([]);
  protected readonly selectedCatalogTemplate = signal<CatalogStepTemplate | undefined>(undefined);
  protected readonly selectedInstalledTemplate = signal<StepTemplate | undefined>(undefined);
  protected readonly loading = signal(true);
  protected readonly loadError = signal<string | undefined>(undefined);
  protected readonly formLoading = signal(false);

  protected readonly filterForm = this.fb.group({
    search: this.fb.control(''),
  });

  private readonly templatePreviewDialog = viewChild.required<TemplateRef<unknown>>('templatePreviewDialog');
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
    this.stepTemplatesService.list().subscribe({
      next: (templates) => {
        this.templates.set(templates);
        this.applyFilter(this.filterForm.controls.search.value);
        this.loading.set(false);
      },
      error: (e) => {
        this.loadError.set(getFormDisplayedError(e) ?? 'Failed to load step templates.');
        this.loading.set(false);
      },
    });
  }

  protected showCatalogPreview(template: CatalogStepTemplate) {
    this.selectedInstalledTemplate.set(undefined);
    this.selectedCatalogTemplate.set(template);
    this.modalRef = this.overlay.showModal(this.templatePreviewDialog());
  }

  protected showInstalledPreview(template: StepTemplate) {
    this.selectedCatalogTemplate.set(undefined);
    this.selectedInstalledTemplate.set(template);
    this.modalRef = this.overlay.showModal(this.templatePreviewDialog());
  }

  protected closeDialog() {
    this.modalRef?.close();
    this.selectedCatalogTemplate.set(undefined);
    this.selectedInstalledTemplate.set(undefined);
  }

  protected async importCatalogTemplate(template: CatalogStepTemplate) {
    this.formLoading.set(true);
    try {
      await firstValueFrom(this.stepTemplatesService.importTemplate(template.request));
      this.closeDialog();
      this.toast.success('Step template installed.');
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

  protected isCatalogInstalled(template: CatalogStepTemplate): boolean {
    return this.templates().some(
      (installed) =>
        installed.sourceType === template.request.sourceType &&
        installed.sourceRef === template.request.sourceRef &&
        installed.versions.some((version) => version.version === template.request.version)
    );
  }

  protected latestVersion(template: StepTemplate): string {
    return template.versions[template.versions.length - 1]?.version ?? '-';
  }

  protected latestActionType(template: StepTemplate): string {
    return template.versions[template.versions.length - 1]?.actionType ?? '-';
  }

  private applyFilter(search: string) {
    const normalized = search.toLowerCase();
    this.filteredTemplates.set(
      this.templates().filter((template) => {
        return (
          normalized.length === 0 ||
          template.name.toLowerCase().includes(normalized) ||
          template.description.toLowerCase().includes(normalized) ||
          template.sourceRef.toLowerCase().includes(normalized) ||
          template.category.toLowerCase().includes(normalized)
        );
      })
    );
  }
}
