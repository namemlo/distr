import {HttpErrorResponse} from '@angular/common/http';
import {ComponentFixture, TestBed} from '@angular/core/testing';
import {Application} from '@distr-sh/distr-sdk';
import {of, throwError} from 'rxjs';
import {vi} from 'vitest';
import {ApplicationsService} from '../services/applications.service';
import {OverlayService} from '../services/overlay.service';
import {SecretsService} from '../services/secrets.service';
import {ToastService} from '../services/toast.service';
import {VariableSetsService} from '../services/variable-sets.service';
import {Secret} from '../types/secret';
import {VariableSet} from '../types/variable-set';
import {VariableSetsComponent} from './variable-sets.component';

describe('VariableSetsComponent', () => {
  let variableSetsService: any;
  let applicationsService: any;
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
  const applications = [{id: 'application-1', name: 'Payments'}] as Application[];
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
    };
    applicationsService = {
      list: vi.fn(),
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
    applicationsService.list.mockReturnValue(of(applications));
    secretsService.list.mockReturnValue(of(secrets));
    overlay.showModal.mockReturnValue({close: vi.fn()} as any);
    overlay.confirm.mockReturnValue(of(true));

    TestBed.configureTestingModule({
      imports: [VariableSetsComponent],
      providers: [
        {provide: VariableSetsService, useValue: variableSetsService},
        {provide: ApplicationsService, useValue: applicationsService},
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

  function createComponent(): {fixture: ComponentFixture<VariableSetsComponent>; component: VariableSetsComponent} {
    const fixture = TestBed.createComponent(VariableSetsComponent);
    fixture.detectChanges();
    return {fixture, component: fixture.componentInstance};
  }
});
