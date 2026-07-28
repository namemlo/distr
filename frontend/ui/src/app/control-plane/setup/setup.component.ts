import {ChangeDetectionStrategy, Component, DestroyRef, inject, signal} from '@angular/core';
import {takeUntilDestroyed} from '@angular/core/rxjs-interop';
import {FormBuilder, ReactiveFormsModule, Validators} from '@angular/forms';
import {firstValueFrom} from 'rxjs';
import {OperatorControlPlaneService} from '../../services/operator-control-plane.service';
import {TargetConfigSnapshotsComponent} from '../../setup/config-snapshots/target-config-snapshots.component';
import {DeploymentRegistryComponent} from '../../setup/registry/deployment-registry.component';
import {OperatorSetupReadiness} from '../../types/operator-control-plane';

@Component({
  selector: 'app-control-plane-setup',
  imports: [ReactiveFormsModule, DeploymentRegistryComponent, TargetConfigSnapshotsComponent],
  templateUrl: './setup.component.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class SetupComponent {
  private readonly service = inject(OperatorControlPlaneService);
  private readonly fb = inject(FormBuilder).nonNullable;
  private readonly destroyRef = inject(DestroyRef);

  protected readonly readinessForm = this.fb.group({
    importId: this.fb.control('', Validators.required),
    deploymentUnitId: this.fb.control('', Validators.required),
  });
  protected readonly readiness = signal<OperatorSetupReadiness | undefined>(undefined);
  protected readonly loading = signal(false);
  protected readonly error = signal('');
  protected readonly stale = signal(false);

  constructor() {
    this.readinessForm.valueChanges.pipe(takeUntilDestroyed(this.destroyRef)).subscribe(() => {
      if (this.readiness()) this.stale.set(true);
    });
  }

  protected async refreshReadiness(): Promise<void> {
    if (this.readinessForm.invalid || this.loading()) {
      this.readinessForm.markAllAsTouched();
      return;
    }

    const value = this.readinessForm.getRawValue();
    this.loading.set(true);
    this.error.set('');
    try {
      this.readiness.set(
        await firstValueFrom(
          this.service.loadSetupReadiness({
            importId: value.importId.trim(),
            deploymentUnitId: value.deploymentUnitId.trim(),
          })
        )
      );
      this.stale.set(false);
    } catch (error) {
      this.error.set(
        this.statusOf(error) === 403
          ? 'You do not have permission to assess control-plane setup.'
          : 'Could not assess setup readiness. Try again.'
      );
    } finally {
      this.loading.set(false);
    }
  }

  protected state(value: boolean, truthy: string, falsy: string): string {
    return value ? truthy : falsy;
  }

  private statusOf(error: unknown): number {
    return typeof error === 'object' && error !== null && 'status' in error ? Number(error.status) : 0;
  }
}
