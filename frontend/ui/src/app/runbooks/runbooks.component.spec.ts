import {HttpErrorResponse} from '@angular/common/http';
import {ComponentFixture, TestBed} from '@angular/core/testing';
import {Application} from '@distr-sh/distr-sdk';
import {of, throwError} from 'rxjs';
import {vi} from 'vitest';
import {ApplicationsService} from '../services/applications.service';
import {OverlayService} from '../services/overlay.service';
import {RunbooksService} from '../services/runbooks.service';
import {ToastService} from '../services/toast.service';
import {Runbook, RunbookRevision, RunbookStepRequest} from '../types/runbook';
import {RunbooksComponent} from './runbooks.component';

describe('RunbooksComponent', () => {
  let runbooksService: any;
  let applicationsService: any;
  let overlay: any;
  let toast: any;

  const applications = [
    {id: 'application-1', name: 'Payments', type: 'docker', versions: []},
    {id: 'application-2', name: 'Portal', type: 'docker', versions: []},
  ] as Application[];

  const runbooks: Runbook[] = [
    {
      id: 'runbook-1',
      createdAt: '2026-06-22T08:00:00Z',
      updatedAt: '2026-06-22T08:00:00Z',
      applicationId: 'application-1',
      name: 'Rotate keys',
      description: 'Rotate service signing keys',
      sortOrder: 10,
    },
  ];

  const revisions: RunbookRevision[] = [
    {
      id: 'revision-1',
      createdAt: '2026-06-22T08:30:00Z',
      updatedAt: '2026-06-22T08:30:00Z',
      runbookId: 'runbook-1',
      revisionNumber: 1,
      description: 'Initial revision',
      steps: [
        {
          id: 'step-1',
          runbookRevisionId: 'revision-1',
          key: 'verify',
          name: 'Verify',
          actionType: 'distr.preflight',
          stepTemplateVersionId: 'template-version-1',
          executionLocation: 'hub',
          inputBindings: {},
          condition: 'always()',
          failureMode: 'fail',
          timeoutSeconds: 120,
          retryPolicy: {maxAttempts: 2, intervalSeconds: 30},
          requiredPermissions: ['runbook.execute'],
          sortOrder: 10,
          dependencies: [],
        },
      ],
    },
  ];

  beforeEach(() => {
    runbooksService = {
      list: vi.fn(),
      get: vi.fn(),
      create: vi.fn(),
      update: vi.fn(),
      delete: vi.fn(),
      listRevisions: vi.fn(),
      getRevision: vi.fn(),
      createRevision: vi.fn(),
      publishRevision: vi.fn(),
    };
    applicationsService = {list: vi.fn()};
    overlay = {
      showModal: vi.fn(),
      confirm: vi.fn(),
    };
    toast = {
      error: vi.fn(),
      success: vi.fn(),
    };

    runbooksService.list.mockReturnValue(of(runbooks));
    runbooksService.get.mockReturnValue(of(runbooks[0]));
    runbooksService.create.mockReturnValue(of(runbooks[0]));
    runbooksService.update.mockReturnValue(of(runbooks[0]));
    runbooksService.delete.mockReturnValue(of(undefined));
    runbooksService.listRevisions.mockReturnValue(of(revisions));
    runbooksService.getRevision.mockReturnValue(of(revisions[0]));
    runbooksService.createRevision.mockReturnValue(of(revisions[0]));
    runbooksService.publishRevision.mockReturnValue(
      of({
        id: 'snapshot-1',
        runbookId: 'runbook-1',
        runbookRevisionId: 'revision-1',
        revisionNumber: 1,
        canonicalChecksum: 'sha256:abc',
        revision: revisions[0],
      })
    );
    applicationsService.list.mockReturnValue(of(applications));
    overlay.showModal.mockReturnValue({close: vi.fn()} as any);
    overlay.confirm.mockReturnValue(of(true));

    TestBed.configureTestingModule({
      imports: [RunbooksComponent],
      providers: [
        {provide: RunbooksService, useValue: runbooksService},
        {provide: ApplicationsService, useValue: applicationsService},
        {provide: OverlayService, useValue: overlay},
        {provide: ToastService, useValue: toast},
      ],
    });
  });

  it('loads runbooks with application lookup data', () => {
    const {component} = createComponent();

    expect((component as any).runbooks()).toEqual(runbooks);
    expect((component as any).applicationName('application-1')).toBe('Payments');
  });

  it('shows load errors', () => {
    runbooksService.list.mockReturnValue(
      throwError(() => new HttpErrorResponse({status: 400, error: 'Could not load runbooks'}))
    );

    const {component} = createComponent();

    expect((component as any).loadError()).toBe('Could not load runbooks');
  });

  it('switches between editor, history, and schedules tabs', () => {
    const {component} = createComponent();

    expect((component as any).activeTab()).toBe('editor');
    (component as any).selectTab('history');
    expect((component as any).activeTab()).toBe('history');
    (component as any).selectTab('schedules');
    expect((component as any).activeTab()).toBe('schedules');
  });

  it('creates runbooks with selected application references', async () => {
    const {component} = createComponent();

    (component as any).showCreateRunbookDialog();
    (component as any).runbookForm.patchValue({
      name: 'Database backup',
      description: 'Backs up the database',
      sortOrder: 20,
    });
    await (component as any).submitRunbookForm();

    expect(runbooksService.create).toHaveBeenCalledWith({
      applicationId: 'application-1',
      name: 'Database backup',
      description: 'Backs up the database',
      sortOrder: 20,
    });
  });

  it('loads revision history for a selected runbook', async () => {
    const {component} = createComponent();

    await (component as any).selectRunbook(runbooks[0]);

    expect(runbooksService.get).toHaveBeenCalledWith('runbook-1');
    expect(runbooksService.listRevisions).toHaveBeenCalledWith('runbook-1');
    expect((component as any).selectedRunbook()).toEqual(runbooks[0]);
    expect((component as any).revisions()).toEqual(revisions);
  });

  it('creates revisions with structured steps', async () => {
    const {component} = createComponent();

    await (component as any).selectRunbook(runbooks[0]);
    (component as any).showCreateRevisionDialog();
    (component as any).revisionForm.patchValue({description: 'Initial revision'});
    (component as any).stepsArray.at(0).patchValue({
      key: 'verify',
      name: 'Verify',
      actionType: 'distr.preflight',
      executionLocation: 'hub',
      inputBindingsText: '{}',
      condition: 'always()',
      failureMode: 'fail',
      timeoutSeconds: 120,
      retryMaxAttempts: 2,
      retryIntervalSeconds: 30,
      requiredPermissionsText: 'runbook.execute',
      sortOrder: 10,
      dependenciesText: '',
    });
    await (component as any).submitRevisionForm();

    expect(runbooksService.createRevision).toHaveBeenCalledWith('runbook-1', {
      description: 'Initial revision',
      steps: [
        {
          key: 'verify',
          name: 'Verify',
          actionType: 'distr.preflight',
          executionLocation: 'hub',
          inputBindings: {},
          condition: 'always()',
          failureMode: 'fail',
          timeoutSeconds: 120,
          retryPolicy: {maxAttempts: 2, intervalSeconds: 30},
          requiredPermissions: ['runbook.execute'],
          sortOrder: 10,
          dependencies: [],
        } satisfies RunbookStepRequest,
      ],
    });
  });

  it('publishes selected revisions', async () => {
    const {component} = createComponent();

    await (component as any).selectRunbook(runbooks[0]);
    await (component as any).publishRevision(revisions[0]);

    expect(runbooksService.publishRevision).toHaveBeenCalledWith('runbook-1', 'revision-1');
    expect(toast.success).toHaveBeenCalledWith('Runbook revision published.');
    expect(runbooksService.listRevisions).toHaveBeenCalledTimes(2);
  });

  it('does not create revisions with invalid input binding JSON', async () => {
    const {component} = createComponent();

    await (component as any).selectRunbook(runbooks[0]);
    (component as any).showCreateRevisionDialog();
    (component as any).stepsArray.at(0).patchValue({
      key: 'verify',
      name: 'Verify',
      actionType: 'distr.preflight',
      executionLocation: 'hub',
      inputBindingsText: '{not json',
    });
    await (component as any).submitRevisionForm();

    expect(runbooksService.createRevision).not.toHaveBeenCalled();
    expect(toast.error).toHaveBeenCalledWith('Step input bindings must be valid JSON.');
  });

  function createComponent(): {
    fixture: ComponentFixture<RunbooksComponent>;
    component: RunbooksComponent;
  } {
    const fixture = TestBed.createComponent(RunbooksComponent);
    fixture.detectChanges();
    return {fixture, component: fixture.componentInstance};
  }
});
