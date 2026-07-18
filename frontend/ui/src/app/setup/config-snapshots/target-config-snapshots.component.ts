import {DatePipe, KeyValuePipe} from '@angular/common';
import {ChangeDetectionStrategy, Component, inject, signal} from '@angular/core';
import {toSignal} from '@angular/core/rxjs-interop';
import {FormBuilder, ReactiveFormsModule, Validators} from '@angular/forms';
import {firstValueFrom} from 'rxjs';
import {AuthService} from '../../services/auth.service';
import {FeatureFlagService} from '../../services/feature-flag.service';
import {TargetConfigSnapshotsService} from '../../services/target-config-snapshots.service';
import {
  CreateTargetConfigSnapshotComponent,
  CreateTargetConfigSnapshotRequest,
  CreateTargetConfigSnapshotSecretReference,
  TargetConfigSnapshot,
  TargetConfigSnapshotFeatureFlag,
  TargetConfigSnapshotObject,
  TargetConfigSnapshotObjectKind,
  TargetConfigSnapshotObjectVerification,
  TargetConfigSnapshotVerification,
} from '../../types/target-config-snapshot';

const objectKinds: TargetConfigSnapshotObjectKind[] = ['deployment_descriptor', 'service_config', 'adapter_input'];

@Component({
  selector: 'app-target-config-snapshots',
  imports: [ReactiveFormsModule, DatePipe, KeyValuePipe],
  templateUrl: './target-config-snapshots.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
})
export class TargetConfigSnapshotsComponent {
  private readonly fb = inject(FormBuilder).nonNullable;
  private readonly snapshotsService = inject(TargetConfigSnapshotsService);
  private readonly auth = inject(AuthService);
  private readonly featureFlags = inject(FeatureFlagService);
  private verificationRequestVersion = 0;

  protected readonly canMutate =
    this.auth.isVendor() && !this.auth.isSuperAdmin() && this.auth.hasAnyRole('read_write', 'admin')
      ? toSignal(this.featureFlags.isExperimentalFeatureEnabled$('operator_control_plane_v2'), {initialValue: false})
      : signal(false);

  protected readonly createForm = this.fb.group({
    deploymentUnitId: this.fb.control('', Validators.required),
    targetEnvironmentAssignmentId: this.fb.control('', Validators.required),
    environmentId: this.fb.control('', Validators.required),
    sourceRepository: this.fb.control('', Validators.required),
    sourceCommit: this.fb.control('', Validators.required),
    sourceAdapter: this.fb.control('', Validators.required),
    adapterVersion: this.fb.control('', Validators.required),
    targetPlatform: this.fb.control('linux/amd64', Validators.required),
    runtimeConstraints: this.fb.control('{}', Validators.required),
    objects: this.fb.control('[]', Validators.required),
    components: this.fb.control('[]', Validators.required),
    secretReferences: this.fb.control('[]', Validators.required),
    featureFlags: this.fb.control('[]', Validators.required),
  });

  protected readonly snapshots = signal<TargetConfigSnapshot[]>([]);
  protected readonly nextCursor = signal<string | undefined>(undefined);
  protected readonly selectedSnapshot = signal<TargetConfigSnapshot | undefined>(undefined);
  protected readonly verification = signal<TargetConfigSnapshotVerification | undefined>(undefined);
  protected readonly loading = signal(true);
  protected readonly loadingMore = signal(false);
  protected readonly loadingDetail = signal(false);
  protected readonly creating = signal(false);
  protected readonly verifying = signal(false);
  protected readonly loadError = signal('');
  protected readonly actionError = signal('');
  protected readonly created = signal(false);

  constructor() {
    void this.loadSnapshots();
  }

  protected async loadSnapshots(resetSelection = true): Promise<void> {
    this.loading.set(true);
    this.loadError.set('');
    try {
      const page = await firstValueFrom(this.snapshotsService.list({limit: 50}));
      this.snapshots.set(page.items);
      this.nextCursor.set(page.nextCursor);
      if (resetSelection) {
        this.selectedSnapshot.set(undefined);
        this.invalidateVerification();
      }
    } catch {
      this.snapshots.set([]);
      this.nextCursor.set(undefined);
      this.loadError.set('Could not load target configuration snapshots.');
    } finally {
      this.loading.set(false);
    }
  }

  protected async selectSnapshot(snapshot: TargetConfigSnapshot): Promise<void> {
    this.invalidateVerification();
    this.loadingDetail.set(true);
    this.actionError.set('');
    try {
      this.selectedSnapshot.set(await firstValueFrom(this.snapshotsService.get(snapshot.id)));
    } catch {
      this.actionError.set('Could not load the target configuration snapshot.');
    } finally {
      this.loadingDetail.set(false);
    }
  }

  protected async loadMore(): Promise<void> {
    const cursor = this.nextCursor();
    if (!cursor || this.loadingMore()) return;

    this.actionError.set('');
    this.loadingMore.set(true);
    try {
      const page = await firstValueFrom(this.snapshotsService.list({cursor, limit: 50}));
      this.snapshots.update((current) => {
        const knownIds = new Set(current.map((snapshot) => snapshot.id));
        return [...current, ...page.items.filter((snapshot) => !knownIds.has(snapshot.id))];
      });
      this.nextCursor.set(page.nextCursor);
    } catch {
      this.actionError.set('Could not load more target configuration snapshots.');
    } finally {
      this.loadingMore.set(false);
    }
  }

  protected async createSnapshot(): Promise<void> {
    if (!this.canMutate()) {
      this.actionError.set('Could not create the target configuration snapshot.');
      return;
    }

    this.actionError.set('');
    this.created.set(false);
    if (this.createForm.invalid) {
      this.createForm.markAllAsTouched();
      return;
    }

    let request: CreateTargetConfigSnapshotRequest;
    try {
      request = this.createRequest();
    } catch {
      this.actionError.set('Configuration JSON must contain only the documented non-secret metadata fields and types.');
      return;
    }

    this.creating.set(true);
    try {
      const snapshot = await firstValueFrom(this.snapshotsService.create(request));
      this.invalidateVerification();
      this.selectedSnapshot.set(snapshot);
      this.created.set(true);
      await this.loadSnapshots(false);
    } catch {
      this.actionError.set('Could not create the target configuration snapshot.');
    } finally {
      this.creating.set(false);
    }
  }

  protected async verifySnapshot(): Promise<void> {
    if (!this.canMutate()) {
      this.actionError.set('Could not verify the target configuration snapshot.');
      return;
    }

    const snapshot = this.selectedSnapshot();
    if (!snapshot || this.verifying()) return;

    const requestVersion = ++this.verificationRequestVersion;
    this.actionError.set('');
    this.verification.set(undefined);
    this.verifying.set(true);
    try {
      const result = await firstValueFrom(this.snapshotsService.verify(snapshot.id));
      if (!this.isCurrentVerification(requestVersion, snapshot.id)) return;
      if (result.snapshotId !== snapshot.id) {
        this.actionError.set('Could not verify the target configuration snapshot.');
        return;
      }
      this.verification.set(result);
    } catch {
      if (this.isCurrentVerification(requestVersion, snapshot.id)) {
        this.actionError.set('Could not verify the target configuration snapshot.');
      }
    } finally {
      if (requestVersion === this.verificationRequestVersion) {
        this.verifying.set(false);
      }
    }
  }

  protected verificationMessage(result: TargetConfigSnapshotObjectVerification): string {
    const message = result.message.replace(/\s+/g, ' ').trim();
    return message.length <= 240 ? message : `${message.slice(0, 239)}…`;
  }

  protected verificationStatusLabel(result: TargetConfigSnapshotVerification): string {
    if (result.verified) return 'Verified';
    return this.verificationUnavailable(result) ? 'Verification unavailable' : 'Mismatch detected';
  }

  protected verificationUnavailable(result: TargetConfigSnapshotVerification): boolean {
    return result.objects.some((object) => object.code === 'verification_unavailable');
  }

  protected snapshotKey(snapshot: TargetConfigSnapshot): string {
    return snapshot.id;
  }

  protected objectKey(object: TargetConfigSnapshotObject): string {
    return object.key;
  }

  private createRequest(): CreateTargetConfigSnapshotRequest {
    const value = this.createForm.getRawValue();
    return {
      deploymentUnitId: value.deploymentUnitId.trim(),
      targetEnvironmentAssignmentId: value.targetEnvironmentAssignmentId.trim(),
      environmentId: value.environmentId.trim(),
      sourceRepository: value.sourceRepository.trim(),
      sourceCommit: value.sourceCommit.trim(),
      sourceAdapter: value.sourceAdapter.trim(),
      adapterVersion: value.adapterVersion.trim(),
      targetPlatform: value.targetPlatform.trim(),
      runtimeConstraints: this.parseRuntimeConstraints(value.runtimeConstraints),
      objects: this.parseObjects(value.objects),
      components: this.parseComponents(value.components),
      secretReferences: this.parseSecretReferences(value.secretReferences),
      featureFlags: this.parseFeatureFlags(value.featureFlags),
    };
  }

  private parseRuntimeConstraints(value: string): Record<string, string> {
    const parsed = this.parseJson(value);
    if (!this.isRecord(parsed) || Object.values(parsed).some((constraint) => typeof constraint !== 'string')) {
      throw new Error('invalid runtime constraints');
    }
    return parsed as Record<string, string>;
  }

  private parseObjects(value: string): TargetConfigSnapshotObject[] {
    const parsed = this.parseArray(value);
    return parsed.map((candidate) => {
      this.requireExactKeys(candidate, ['key', 'kind', 'reference', 'versionId', 'mediaType', 'sizeBytes', 'checksum']);
      const kind = this.requireString(candidate, 'kind') as TargetConfigSnapshotObjectKind;
      if (!objectKinds.includes(kind)) throw new Error('invalid object kind');
      const versionId = candidate['versionId'];
      if (versionId !== undefined && typeof versionId !== 'string') throw new Error('invalid version ID');
      const sizeBytes = candidate['sizeBytes'];
      if (!Number.isSafeInteger(sizeBytes) || (sizeBytes as number) < 0) throw new Error('invalid object size');
      return {
        key: this.requireString(candidate, 'key'),
        kind,
        reference: this.requireString(candidate, 'reference'),
        ...(versionId === undefined ? {} : {versionId}),
        mediaType: this.requireString(candidate, 'mediaType'),
        sizeBytes: sizeBytes as number,
        checksum: this.requireString(candidate, 'checksum'),
      };
    });
  }

  private parseComponents(value: string): CreateTargetConfigSnapshotComponent[] {
    return this.parseArray(value).map((candidate) => {
      this.requireExactKeys(candidate, ['physicalName', 'componentInstanceId', 'deploymentUnitId']);
      return {
        physicalName: this.requireString(candidate, 'physicalName'),
        componentInstanceId: this.requireString(candidate, 'componentInstanceId'),
        deploymentUnitId: this.requireString(candidate, 'deploymentUnitId'),
      };
    });
  }

  private parseSecretReferences(value: string): CreateTargetConfigSnapshotSecretReference[] {
    return this.parseArray(value).map((candidate) => {
      this.requireExactKeys(candidate, ['key', 'provider', 'reference', 'versionFingerprint']);
      return {
        key: this.requireString(candidate, 'key'),
        provider: this.requireString(candidate, 'provider'),
        reference: this.requireString(candidate, 'reference'),
        versionFingerprint: this.requireString(candidate, 'versionFingerprint'),
      };
    });
  }

  private parseFeatureFlags(value: string): TargetConfigSnapshotFeatureFlag[] {
    return this.parseArray(value).map((candidate) => {
      this.requireExactKeys(candidate, ['key', 'enabled']);
      if (typeof candidate['enabled'] !== 'boolean') throw new Error('invalid feature flag');
      return {key: this.requireString(candidate, 'key'), enabled: candidate['enabled']};
    });
  }

  private parseArray(value: string): Record<string, unknown>[] {
    const parsed = this.parseJson(value);
    if (!Array.isArray(parsed) || parsed.some((candidate) => !this.isRecord(candidate))) {
      throw new Error('expected array of objects');
    }
    return parsed as Record<string, unknown>[];
  }

  private parseJson(value: string): unknown {
    return JSON.parse(value);
  }

  private requireExactKeys(candidate: Record<string, unknown>, allowed: string[]): void {
    if (Object.keys(candidate).some((key) => !allowed.includes(key))) throw new Error('unexpected metadata field');
  }

  private requireString(candidate: Record<string, unknown>, key: string): string {
    const value = candidate[key];
    if (typeof value !== 'string' || value.trim() === '') throw new Error('missing metadata field');
    return value.trim();
  }

  private isRecord(value: unknown): value is Record<string, unknown> {
    return value !== null && typeof value === 'object' && !Array.isArray(value);
  }

  private invalidateVerification(): void {
    this.verificationRequestVersion++;
    this.verification.set(undefined);
    this.verifying.set(false);
  }

  private isCurrentVerification(requestVersion: number, snapshotId: string): boolean {
    return requestVersion === this.verificationRequestVersion && this.selectedSnapshot()?.id === snapshotId;
  }
}
