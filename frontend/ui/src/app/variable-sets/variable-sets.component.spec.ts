import {HttpErrorResponse} from '@angular/common/http';
import {ComponentFixture, TestBed} from '@angular/core/testing';
import {Application} from '@distr-sh/distr-sdk';
import {of, throwError} from 'rxjs';
import {vi} from 'vitest';
import {ApplicationsService} from '../services/applications.service';
import {ChannelsService} from '../services/channels.service';
import {ConfigAsCodeService} from '../services/config-as-code.service';
import {CustomerOrganizationsService} from '../services/customer-organizations.service';
import {DeploymentTargetsService} from '../services/deployment-targets.service';
import {EnvironmentsService} from '../services/environments.service';
import {FeatureFlagService} from '../services/feature-flag.service';
import {OverlayService} from '../services/overlay.service';
import {SecretsService} from '../services/secrets.service';
import {ToastService} from '../services/toast.service';
import {VariableSetsService} from '../services/variable-sets.service';
import {ConfigAsCodeAuthority} from '../types/config-as-code';
import {Secret} from '../types/secret';
import {VariableSet} from '../types/variable-set';
import {VariableSetsComponent} from './variable-sets.component';

describe('VariableSetsComponent', () => {
  let variableSetsService: any;
  let configAsCodeService: any;
  let featureFlagService: any;
  let applicationsService: any;
  let channelsService: any;
  let environmentsService: any;
  let deploymentTargetsService: any;
  let customerOrganizationsService: any;
  let secretsService: any;
  let overlay: any;
  let toast: any;

  const variableSets: VariableSet[] = [
    {
      id: 'variable-set-1',
      createdAt: '2026-06-21T09:30:00Z',
      updatedAt: '2026-06-21T10:45:00Z',
      name: 'Runtime Defaults',
      description: 'Shared application variables',
      sortOrder: 10,
      applicationIds: ['application-1'],
      variables: [
        {
          id: 'variable-1',
          createdAt: '2026-06-21T09:30:00Z',
          updatedAt: '2026-06-21T10:45:00Z',
          key: 'api_url',
          description: '',
          type: 'string',
          isRequired: false,
          defaultValue: 'https://example.test',
          scopedValues: [
            {
              id: 'scoped-value-1',
              createdAt: '2026-06-21T09:30:00Z',
              updatedAt: '2026-06-21T10:45:00Z',
              scope: {applicationId: 'application-1'},
              sortOrder: 0,
              value: 'https://application.example',
            },
          ],
        },
        {
          id: 'variable-2',
          createdAt: '2026-06-21T09:30:00Z',
          updatedAt: '2026-06-21T10:45:00Z',
          key: 'api_token',
          description: '',
          type: 'secret_reference',
          isRequired: false,
          referenceId: 'secret-1',
          referenceName: 'api_token',
        },
      ],
    },
  ];
  const gitManagedVariableSetAuthority: ConfigAsCodeAuthority = {
    resourceKind: 'VariableSetDefinition',
    resourceId: 'variable-set-1',
    authority: 'GIT_MANAGED',
    repositoryPath: 'variable-sets/runtime-defaults.yaml',
    sourceRevision: '6dcb09f',
    documentChecksum: 'sha256:1234',
    updatedByUserId: 'user-1',
    updatedAt: '2026-06-21T11:00:00Z',
  };
  const applications = [{id: 'application-1', name: 'Payments'}] as Application[];
  const channels = [
    {
      id: 'channel-1',
      createdAt: '2026-06-21T09:30:00Z',
      updatedAt: '2026-06-21T10:45:00Z',
      applicationId: 'application-1',
      lifecycleId: 'lifecycle-1',
      name: 'Stable',
      description: '',
      sortOrder: 10,
      isDefault: true,
      allowedVersionRanges: [],
      allowedPrereleasePatterns: [],
      allowedSourceBranches: [],
      allowedSourceTags: [],
    },
  ];
  const environments = [
    {
      id: 'environment-1',
      createdAt: '2026-06-21T09:30:00Z',
      updatedAt: '2026-06-21T10:45:00Z',
      name: 'Production',
      description: '',
      sortOrder: 10,
      isProduction: true,
      allowDynamicTargets: false,
    },
  ];
  const deploymentTargets = [{id: 'target-1', name: 'cluster-a'}];
  const customerOrganizations = [{id: 'customer-1', name: 'Acme'}];
  const secrets: Secret[] = [
    {
      id: 'secret-1',
      createdAt: '2026-06-21T09:30:00Z',
      updatedAt: '2026-06-21T10:45:00Z',
      key: 'api_token',
    },
  ];

  beforeEach(() => {
    variableSetsService = {
      list: vi.fn(),
      create: vi.fn(),
      update: vi.fn(),
      delete: vi.fn(),
      resolvePreview: vi.fn(),
    };
    configAsCodeService = {
      listAuthorities: vi.fn(),
    };
    featureFlagService = {
      isConfigAsCodeEnabled$: of(false),
    };
    applicationsService = {
      list: vi.fn(),
    };
    channelsService = {
      list: vi.fn(),
    };
    environmentsService = {
      list: vi.fn(),
    };
    deploymentTargetsService = {
      list: vi.fn(),
    };
    customerOrganizationsService = {
      getCustomerOrganizations: vi.fn(),
    };
    secretsService = {
      list: vi.fn(),
    };
    overlay = {
      showModal: vi.fn(),
      confirm: vi.fn(),
    };
    toast = {
      error: vi.fn(),
    };

    variableSetsService.list.mockReturnValue(of(variableSets));
    variableSetsService.create.mockReturnValue(of(variableSets[0]));
    variableSetsService.update.mockReturnValue(of(variableSets[0]));
    variableSetsService.delete.mockReturnValue(of(undefined));
    variableSetsService.resolvePreview.mockReturnValue(
      of([
        {
          variableSetId: 'variable-set-1',
          variableId: 'variable-1',
          key: 'api_url',
          type: 'string',
          isRequired: false,
          status: 'resolved',
          source: 'application',
          value: 'https://application.example',
          redacted: false,
          trace: [{source: 'application', scope: {applicationId: 'application-1'}, selected: true, reason: 'matched'}],
        },
      ])
    );
    configAsCodeService.listAuthorities.mockReturnValue(of({authorities: []}));
    applicationsService.list.mockReturnValue(of(applications));
    channelsService.list.mockReturnValue(of(channels));
    environmentsService.list.mockReturnValue(of(environments));
    deploymentTargetsService.list.mockReturnValue(of(deploymentTargets));
    customerOrganizationsService.getCustomerOrganizations.mockReturnValue(of(customerOrganizations));
    secretsService.list.mockReturnValue(of(secrets));
    overlay.showModal.mockReturnValue({close: vi.fn()} as any);
    overlay.confirm.mockReturnValue(of(true));

    TestBed.configureTestingModule({
      imports: [VariableSetsComponent],
      providers: [
        {provide: VariableSetsService, useValue: variableSetsService},
        {provide: ConfigAsCodeService, useValue: configAsCodeService},
        {provide: FeatureFlagService, useValue: featureFlagService},
        {provide: ApplicationsService, useValue: applicationsService},
        {provide: ChannelsService, useValue: channelsService},
        {provide: EnvironmentsService, useValue: environmentsService},
        {provide: DeploymentTargetsService, useValue: deploymentTargetsService},
        {provide: CustomerOrganizationsService, useValue: customerOrganizationsService},
        {provide: SecretsService, useValue: secretsService},
        {provide: OverlayService, useValue: overlay},
        {provide: ToastService, useValue: toast},
      ],
    });
  });

  it('loads variable sets with application and secret lookup data', () => {
    const {component} = createComponent();

    expect((component as any).variableSets()).toEqual(variableSets);
    expect((component as any).applicationNames(['application-1'])).toBe('Payments');
    expect((component as any).secrets()).toEqual(secrets);
    expect((component as any).channels()).toEqual(channels);
    expect((component as any).environments()).toEqual(environments);
  });

  it('loads config-as-code authorities and blocks Git-managed variable set mutations', async () => {
    featureFlagService.isConfigAsCodeEnabled$ = of(true);
    configAsCodeService.listAuthorities.mockReturnValue(of({authorities: [gitManagedVariableSetAuthority]}));
    const {component} = createComponent();

    expect((component as any).authorityFor(variableSets[0])).toEqual(gitManagedVariableSetAuthority);
    expect((component as any).isGitManaged(variableSets[0])).toBe(true);

    (component as any).showUpdateDialog(variableSets[0]);
    (component as any).delete(variableSets[0]);
    await Promise.resolve();

    expect(overlay.showModal).not.toHaveBeenCalled();
    expect(overlay.confirm).not.toHaveBeenCalled();
    expect(variableSetsService.delete).not.toHaveBeenCalled();
    expect(toast.error).toHaveBeenCalledWith('This variable set is managed from Git.');
  });

  it('shows load errors', () => {
    variableSetsService.list.mockReturnValue(
      throwError(() => new HttpErrorResponse({status: 400, error: 'Could not load variable sets'}))
    );

    const {component} = createComponent();

    expect((component as any).loadError()).toBe('Could not load variable sets');
  });

  it('creates variable sets with application links and reference-safe variables', async () => {
    const {component} = createComponent();

    (component as any).showCreateDialog();
    (component as any).toggleApplication('application-1', true);
    (component as any).variableSetForm.patchValue({
      name: 'Runtime Defaults',
      description: 'Shared application variables',
      sortOrder: 10,
    });
    (component as any).variableControls()[0].patchValue({
      key: 'api_url',
      type: 'string',
      defaultValueText: 'https://example.test',
    });
    (component as any).addScopedValue(0);
    (component as any).scopedValueControls((component as any).variableControls()[0])[0].patchValue({
      scopeKind: 'application',
      applicationId: 'application-1',
      valueText: 'https://application.example',
    });
    (component as any).addVariable();
    (component as any).variableControls()[1].patchValue({
      key: 'api_token',
      type: 'secret_reference',
      referenceId: 'secret-1',
    });
    await (component as any).submitForm();

    expect(variableSetsService.create).toHaveBeenCalledWith({
      name: 'Runtime Defaults',
      description: 'Shared application variables',
      sortOrder: 10,
      applicationIds: ['application-1'],
      variables: [
        {
          key: 'api_url',
          description: '',
          type: 'string',
          isRequired: false,
          defaultValue: 'https://example.test',
          scopedValues: [
            {
              scope: {applicationId: 'application-1'},
              sortOrder: 0,
              value: 'https://application.example',
            },
          ],
        },
        {
          key: 'api_token',
          description: '',
          type: 'secret_reference',
          isRequired: false,
          referenceId: 'secret-1',
        },
      ],
    });
  });

  it('updates variable sets', async () => {
    const {component} = createComponent();

    (component as any).showUpdateDialog(variableSets[0]);
    (component as any).variableSetForm.patchValue({name: 'Tenant Defaults'});
    await (component as any).submitForm();

    expect(variableSetsService.update).toHaveBeenCalledWith('variable-set-1', {
      name: 'Tenant Defaults',
      description: 'Shared application variables',
      sortOrder: 10,
      applicationIds: ['application-1'],
      variables: [
        {
          key: 'api_url',
          description: '',
          type: 'string',
          isRequired: false,
          defaultValue: 'https://example.test',
          scopedValues: [
            {
              scope: {applicationId: 'application-1'},
              sortOrder: 0,
              value: 'https://application.example',
            },
          ],
        },
        {
          key: 'api_token',
          description: '',
          type: 'secret_reference',
          isRequired: false,
          referenceId: 'secret-1',
        },
      ],
    });
  });

  it('rejects invalid JSON variable defaults before calling the API', async () => {
    const {component} = createComponent();

    (component as any).showCreateDialog();
    (component as any).variableSetForm.patchValue({name: 'Runtime Defaults'});
    (component as any).variableControls()[0].patchValue({
      key: 'metadata',
      type: 'json',
      defaultValueText: '{bad',
    });
    await (component as any).submitForm();

    expect(variableSetsService.create).not.toHaveBeenCalled();
    expect(toast.error).toHaveBeenCalledWith('JSON variables require valid JSON.');
  });

  it('confirms before deleting variable sets', async () => {
    const {component} = createComponent();

    (component as any).delete(variableSets[0]);
    await Promise.resolve();

    expect(overlay.confirm).toHaveBeenCalled();
    expect(variableSetsService.delete).toHaveBeenCalledWith('variable-set-1');
  });

  it('previews scoped variable resolution', async () => {
    const {component} = createComponent();

    (component as any).showPreviewDialog(variableSets[0]);
    (component as any).previewForm.patchValue({
      applicationId: 'application-1',
      targetTagsText: ' linux ',
    });
    await (component as any).loadPreview();

    expect(variableSetsService.resolvePreview).toHaveBeenCalledWith({
      variableSetIds: ['variable-set-1'],
      scope: {
        applicationId: 'application-1',
        targetTags: ['linux'],
      },
      promptedValues: [],
    });
    expect((component as any).previewResults()[0].source).toBe('application');
  });

  it('detects duplicate scoped value conflicts in the variable editor', () => {
    const {component} = createComponent();

    (component as any).showCreateDialog();
    (component as any).addScopedValue(0);
    (component as any).addScopedValue(0);
    for (const scopedValue of (component as any).scopedValueControls((component as any).variableControls()[0])) {
      scopedValue.patchValue({
        scopeKind: 'application',
        applicationId: 'application-1',
        valueText: 'https://example.test',
      });
    }

    expect((component as any).hasScopedValueConflict((component as any).variableControls()[0])).toBe(true);
  });

  function createComponent(): {fixture: ComponentFixture<VariableSetsComponent>; component: VariableSetsComponent} {
    const fixture = TestBed.createComponent(VariableSetsComponent);
    fixture.detectChanges();
    return {fixture, component: fixture.componentInstance};
  }
});
