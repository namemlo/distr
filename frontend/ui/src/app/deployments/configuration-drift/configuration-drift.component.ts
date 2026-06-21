import {ChangeDetectionStrategy, Component, effect, inject, input, signal, untracked} from '@angular/core';
import {take} from 'rxjs';
import {DeploymentTargetsService} from '../../services/deployment-targets.service';
import {
  ConfigurationDrift,
  ConfigurationDriftDefaultChange,
  ConfigurationDriftReferenceChange,
  ConfigurationDriftRemovedValue,
  ConfigurationDriftTypeChange,
  ConfigurationDriftVariable,
} from '../../types/configuration-drift';

@Component({
  selector: 'app-configuration-drift',
  templateUrl: './configuration-drift.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
})
export class ConfigurationDriftComponent {
  public readonly deploymentId = input.required<string>();

  private readonly deploymentTargetsService = inject(DeploymentTargetsService);
  private requestId = 0;

  protected readonly loading = signal(true);
  protected readonly loadError = signal('');
  protected readonly drift = signal<ConfigurationDrift | undefined>(undefined);

  constructor() {
    effect(() => {
      const deploymentId = this.deploymentId();
      untracked(() => this.load(deploymentId));
    });
  }

  protected reload() {
    this.load(this.deploymentId());
  }

  protected changedCount(drift: ConfigurationDrift): number {
    return (
      drift.newRequiredVariables.length +
      drift.missingVariables.length +
      drift.removedVariables.length +
      drift.typeChanges.length +
      drift.defaultChanges.length +
      drift.secretReferenceChanges.length
    );
  }

  protected variableKey(variable: ConfigurationDriftVariable): string {
    return variable.key;
  }

  protected removedKey(value: ConfigurationDriftRemovedValue): string {
    return value.key;
  }

  protected typeChangeKey(change: ConfigurationDriftTypeChange): string {
    return change.key;
  }

  protected defaultChangeKey(change: ConfigurationDriftDefaultChange): string {
    return change.key;
  }

  protected referenceChangeKey(change: ConfigurationDriftReferenceChange): string {
    return change.key;
  }

  private load(deploymentId: string) {
    const requestId = ++this.requestId;
    this.loading.set(true);
    this.loadError.set('');
    this.deploymentTargetsService
      .getConfigurationDrift(deploymentId)
      .pipe(take(1))
      .subscribe({
        next: (drift) => {
          if (requestId !== this.requestId) return;
          this.drift.set(drift);
          this.loading.set(false);
        },
        error: () => {
          if (requestId !== this.requestId) return;
          this.drift.set(undefined);
          this.loadError.set('Could not load configuration drift');
          this.loading.set(false);
        },
      });
  }
}
