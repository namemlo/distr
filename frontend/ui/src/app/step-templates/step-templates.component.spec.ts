import {HttpErrorResponse} from '@angular/common/http';
import {ComponentFixture, TestBed} from '@angular/core/testing';
import {of, throwError} from 'rxjs';
import {vi} from 'vitest';
import {ConfigAsCodeService} from '../services/config-as-code.service';
import {FeatureFlagService} from '../services/feature-flag.service';
import {OverlayService} from '../services/overlay.service';
import {StepTemplatesService} from '../services/step-templates.service';
import {ToastService} from '../services/toast.service';
import {ConfigAsCodeAuthority} from '../types/config-as-code';
import {StepTemplate} from '../types/step-template';
import {StepTemplatesComponent} from './step-templates.component';

describe('StepTemplatesComponent', () => {
  let stepTemplatesService: any;
  let configAsCodeService: any;
  let featureFlagService: any;
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
  const gitManagedTemplateAuthority: ConfigAsCodeAuthority = {
    resourceKind: 'StepTemplateReference',
    resourceId: 'template-1',
    authority: 'GIT_MANAGED',
    repositoryPath: 'step-templates/http-health-check.yaml',
    sourceRevision: '6dcb09f',
    documentChecksum: 'sha256:1234',
    updatedByUserId: 'user-1',
    updatedAt: '2026-06-22T11:00:00Z',
  };

  beforeEach(() => {
    stepTemplatesService = {
      list: vi.fn(),
      get: vi.fn(),
      importTemplate: vi.fn(),
    };
    configAsCodeService = {
      listAuthorities: vi.fn(),
    };
    featureFlagService = {
      isConfigAsCodeEnabled$: of(false),
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
    configAsCodeService.listAuthorities.mockReturnValue(of({authorities: []}));
    overlay.showModal.mockReturnValue({close: vi.fn()} as any);

    TestBed.configureTestingModule({
      imports: [StepTemplatesComponent],
      providers: [
        {provide: StepTemplatesService, useValue: stepTemplatesService},
        {provide: ConfigAsCodeService, useValue: configAsCodeService},
        {provide: FeatureFlagService, useValue: featureFlagService},
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

  it('loads config-as-code authorities and blocks Git-managed template re-imports', async () => {
    featureFlagService.isConfigAsCodeEnabled$ = of(true);
    configAsCodeService.listAuthorities.mockReturnValue(of({authorities: [gitManagedTemplateAuthority]}));
    const {component} = createComponent();

    expect((component as any).authorityFor(installedTemplates[0])).toEqual(gitManagedTemplateAuthority);
    expect((component as any).isGitManaged(installedTemplates[0])).toBe(true);

    await (component as any).importCatalogTemplate((component as any).catalogTemplates[0]);

    expect(stepTemplatesService.importTemplate).not.toHaveBeenCalled();
    expect(toast.error).toHaveBeenCalledWith('This step template is managed from Git.');
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
