import {HttpErrorResponse} from '@angular/common/http';
import {ComponentFixture, TestBed} from '@angular/core/testing';
import {of, throwError} from 'rxjs';
import {vi} from 'vitest';
import {OverlayService} from '../services/overlay.service';
import {StepTemplatesService} from '../services/step-templates.service';
import {ToastService} from '../services/toast.service';
import {StepTemplate} from '../types/step-template';
import {StepTemplatesComponent} from './step-templates.component';

describe('StepTemplatesComponent', () => {
  let stepTemplatesService: any;
  let overlay: any;
  let toast: any;

  const installedTemplates: StepTemplate[] = [
    {
      id: 'template-1',
      createdAt: '2026-06-22T10:00:00Z',
      updatedAt: '2026-06-22T10:00:00Z',
      sourceType: 'builtin',
      sourceRef: 'builtin/http-health-check',
      name: 'HTTP health check',
      description: 'Checks that an HTTP endpoint returns a healthy status.',
      category: 'Health',
      installedAt: '2026-06-22T10:00:00Z',
      installedByUserAccountId: 'user-1',
      versions: [
        {
          id: 'version-1',
          createdAt: '2026-06-22T10:00:00Z',
          stepTemplateId: 'template-1',
          version: '1.0.0',
          actionType: 'distr.http.check',
          executionLocation: 'hub',
          inputSchema: {type: 'object'},
          outputSchema: {type: 'object'},
          defaultInputBindings: {url: 'https://example.com/health'},
          minimumAgentVersion: '1.0.0',
          compatibleActionVersion: '1',
          runtimeCompatibilityNotes: 'Uses the built-in HTTP check action.',
          deprecated: false,
        },
      ],
    },
  ];

  beforeEach(() => {
    stepTemplatesService = {
      list: vi.fn(),
      get: vi.fn(),
      importTemplate: vi.fn(),
    };
    overlay = {
      showModal: vi.fn(),
    };
    toast = {
      error: vi.fn(),
      success: vi.fn(),
    };
    stepTemplatesService.list.mockReturnValue(of(installedTemplates));
    stepTemplatesService.importTemplate.mockReturnValue(of(installedTemplates[0]));
    overlay.showModal.mockReturnValue({close: vi.fn()} as any);

    TestBed.configureTestingModule({
      imports: [StepTemplatesComponent],
      providers: [
        {provide: StepTemplatesService, useValue: stepTemplatesService},
        {provide: OverlayService, useValue: overlay},
        {provide: ToastService, useValue: toast},
      ],
    });
  });

  it('loads installed templates and marks matching catalog entries installed', () => {
    const {component} = createComponent();

    expect((component as any).templates()).toEqual(installedTemplates);
    expect((component as any).isCatalogInstalled((component as any).catalogTemplates[0])).toBe(true);
    expect((component as any).catalogTemplates.length).toBeGreaterThan(1);
  });

  it('shows load errors', () => {
    stepTemplatesService.list.mockReturnValue(
      throwError(() => new HttpErrorResponse({status: 400, error: 'Could not load templates'}))
    );

    const {component} = createComponent();

    expect((component as any).loadError()).toBe('Could not load templates');
  });

  it('previews a catalog template before import', () => {
    const {component} = createComponent();
    const catalogTemplate = (component as any).catalogTemplates[1];

    (component as any).showCatalogPreview(catalogTemplate);

    expect((component as any).selectedCatalogTemplate()).toEqual(catalogTemplate);
    expect(overlay.showModal).toHaveBeenCalled();
  });

  it('imports catalog templates and reloads installed state', async () => {
    const {component} = createComponent();
    const catalogTemplate = (component as any).catalogTemplates[1];

    await (component as any).importCatalogTemplate(catalogTemplate);

    expect(stepTemplatesService.importTemplate).toHaveBeenCalledWith(catalogTemplate.request);
    expect(stepTemplatesService.list).toHaveBeenCalledTimes(2);
    expect(toast.success).toHaveBeenCalledWith('Step template installed.');
  });

  function createComponent(): {fixture: ComponentFixture<StepTemplatesComponent>; component: StepTemplatesComponent} {
    const fixture = TestBed.createComponent(StepTemplatesComponent);
    fixture.detectChanges();
    return {fixture, component: fixture.componentInstance};
  }
});
