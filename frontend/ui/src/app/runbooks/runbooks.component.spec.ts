import {HttpErrorResponse} from '@angular/common/http';
import {ComponentFixture, TestBed} from '@angular/core/testing';
import {Application} from '@distr-sh/distr-sdk';
import {of, throwError} from 'rxjs';
import {vi} from 'vitest';
import {ApplicationsService} from '../services/applications.service';
import {ConfigAsCodeService} from '../services/config-as-code.service';
import {FeatureFlagService} from '../services/feature-flag.service';
import {OverlayService} from '../services/overlay.service';
import {RunbooksService} from '../services/runbooks.service';
import {ToastService} from '../services/toast.service';
import {ConfigAsCodeAuthority} from '../types/config-as-code';
import {Runbook, RunbookRevision, RunbookStepRequest} from '../types/runbook';
import {RunbooksComponent} from './runbooks.component';

describe('RunbooksComponent', () => {
  let runbooksService: any;
  let configAsCodeService: any;
  let featureFlagService: any;
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
  const gitManagedRunbookAuthority: ConfigAsCodeAuthority = {
    resourceKind: 'Runbook',
    resourceId: 'runbook-1',
    authority: 'GIT_MANAGED',
    repositoryPath: 'runbooks/rotate-keys.yaml',
    sourceRevision: '6dcb09f',
    documentChecksum: 'sha256:1234',
    updatedByUserId: 'user-1',
    updatedAt: '2026-06-22T09:00:00Z',
  };

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
    configAsCodeService = {
      listAuthorities: vi.fn(),
    };
    featureFlagService = {
      isConfigAsCodeEnabled$: of(false),
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
    configAsCodeService.listAuthorities.mockReturnValue(of({authorities: []}));
    applicationsService.list.mockReturnValue(of(applications));
    overlay.showModal.mockReturnValue({close: vi.fn()} as any);
    overlay.confirm.mockReturnValue(of(true));

    TestBed.configureTestingModule({
      imports: [RunbooksComponent],
      providers: [
        {provide: RunbooksService, useValue: runbooksService},
        {provide: ConfigAsCodeService, useValue: configAsCodeService},
        {provide: FeatureFlagService, useValue: featureFlagService},
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

  it('loads config-as-code authorities and blocks Git-managed runbook mutations', async () => {
    featureFlagService.isConfigAsCodeEnabled$ = of(true);
    configAsCodeService.listAuthorities.mockReturnValue(of({authorities: [gitManagedRunbookAuthority]}));
    const {component} = createComponent();

    expect((component as any).authorityFor(runbooks[0])).toEqual(gitManagedRunbookAuthority);
    expect((component as any).isGitManaged(runbooks[0])).toBe(true);

    (component as any).showUpdateRunbookDialog(runbooks[0]);
    (component as any).delete(runbooks[0]);
    (component as any).selectedRunbook.set(runbooks[0]);
    (component as any).showCreateRevisionDialog();
    await (component as any).publishRevision(revisions[0]);

    expect(overlay.showModal).not.toHaveBeenCalled();
    expect(overlay.confirm).not.toHaveBeenCalled();
    expect(runbooksService.publishRevision).not.toHaveBeenCalled();
    expect(toast.error).toHaveBeenCalledWith('This runbook is managed from Git.');
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
