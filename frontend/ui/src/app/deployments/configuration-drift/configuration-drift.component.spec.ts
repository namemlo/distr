import {ComponentFixture, TestBed} from '@angular/core/testing';
import {of, Subject, throwError} from 'rxjs';
import {vi} from 'vitest';
import {DeploymentTargetsService} from '../../services/deployment-targets.service';
import {ConfigurationDrift} from '../../types/configuration-drift';
import {ConfigurationDriftComponent} from './configuration-drift.component';

describe('ConfigurationDriftComponent', () => {
  let deploymentTargetsService: any;

  beforeEach(() => {
    deploymentTargetsService = {
      getConfigurationDrift: vi.fn(),
    };
    TestBed.configureTestingModule({
      imports: [ConfigurationDriftComponent],
      providers: [{provide: DeploymentTargetsService, useValue: deploymentTargetsService}],
    });
  });

  it('shows loading and then an empty state when no drift exists', () => {
    const response = new Subject<ConfigurationDrift>();
    deploymentTargetsService.getConfigurationDrift.mockReturnValue(response.asObservable());
    const {fixture} = createComponent('deployment-1');

    expect(fixture.nativeElement.textContent).toContain('Checking configuration drift');

    response.next(emptyDrift());
    response.complete();
    fixture.detectChanges();

    expect(fixture.nativeElement.textContent).toContain('No configuration drift');
  });

  it('shows API errors', () => {
    deploymentTargetsService.getConfigurationDrift.mockReturnValue(throwError(() => new Error('failed')));
    const {fixture} = createComponent('deployment-1');

    fixture.detectChanges();

    expect(fixture.nativeElement.textContent).toContain('Could not load configuration drift');
  });

  it('shows drift categories without secret values', () => {
    deploymentTargetsService.getConfigurationDrift.mockReturnValue(
      of({
        deploymentId: 'deployment-1',
        applicationId: 'application-1',
        hasDrift: true,
        newRequiredVariables: [{key: 'REPLICAS', type: 'number', isRequired: true, source: 'unresolved'}],
        missingVariables: [{key: 'DEBUG', type: 'boolean', isRequired: false, source: 'default', value: true}],
        removedVariables: [{key: 'OLD_SETTING'}],
        typeChanges: [{key: 'PORT', expectedType: 'number', deployedType: 'string'}],
        defaultChanges: [{key: 'API_URL', type: 'string', currentValue: 'https://new.example'}],
        secretReferenceChanges: [
          {
            key: 'API_TOKEN',
            type: 'secret_reference',
            referenceId: 'secret-1',
            referenceName: 'api_token',
            redacted: true,
          },
        ],
      } satisfies ConfigurationDrift)
    );

    const {fixture} = createComponent('deployment-1');
    fixture.detectChanges();
    const text = fixture.nativeElement.textContent;

    expect(text).toContain('Configuration drift');
    expect(text).toContain('REPLICAS');
    expect(text).toContain('OLD_SETTING');
    expect(text).toContain('API_TOKEN');
    expect(text).not.toContain('secret-value');
  });

  function createComponent(deploymentId: string): {
    fixture: ComponentFixture<ConfigurationDriftComponent>;
    component: ConfigurationDriftComponent;
  } {
    const fixture = TestBed.createComponent(ConfigurationDriftComponent);
    fixture.componentRef.setInput('deploymentId', deploymentId);
    fixture.detectChanges();
    return {fixture, component: fixture.componentInstance};
  }

  function emptyDrift(): ConfigurationDrift {
    return {
      deploymentId: 'deployment-1',
      applicationId: 'application-1',
      hasDrift: false,
      newRequiredVariables: [],
      missingVariables: [],
      removedVariables: [],
      typeChanges: [],
      defaultChanges: [],
      secretReferenceChanges: [],
    };
  }
});
