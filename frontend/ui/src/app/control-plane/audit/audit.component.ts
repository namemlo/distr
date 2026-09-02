import {DatePipe, JsonPipe} from '@angular/common';
import {ChangeDetectionStrategy, Component, inject, signal} from '@angular/core';
import {FormBuilder, ReactiveFormsModule, Validators} from '@angular/forms';
import {ActivatedRoute} from '@angular/router';
import {firstValueFrom} from 'rxjs';
import {OperatorControlPlaneService} from '../../services/operator-control-plane.service';
import {
  OperatorAuditDetail,
  OperatorAuditExportSink,
  OperatorAuditExportSinkKind,
  OperatorAuditExportStatus,
  OperatorAuditFilters,
  OperatorAuditRow,
  OperatorControlPlaneAuditEvent,
  OperatorEvidenceBundle,
  OperatorEvidenceRef,
  OperatorProtectedHistoryArtifact,
  OperatorProtectedHistoryArtifactVerification,
} from '../../types/operator-control-plane';

@Component({
  selector: 'app-audit',
  imports: [ReactiveFormsModule, DatePipe, JsonPipe],
  templateUrl: './audit.component.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class AuditComponent {
  private readonly service = inject(OperatorControlPlaneService);
  private readonly route = inject(ActivatedRoute);
  private readonly fb = inject(FormBuilder).nonNullable;

  protected readonly filters = this.fb.group({
    action: this.fb.control(this.queryFilter('action')),
    subjectType: this.fb.control(this.queryFilter('subjectType')),
    subjectId: this.fb.control(this.queryFilter('subjectId')),
    actorUserAccountId: this.fb.control(this.queryFilter('actorUserAccountId')),
    search: this.fb.control(this.queryFilter('search')),
  });
  protected readonly bundleForm = this.fb.group({
    deploymentPlanId: this.fb.control('', Validators.required),
  });
  protected readonly sinkForm = this.fb.group({
    name: this.fb.control('', [Validators.required, Validators.maxLength(128)]),
    kind: this.fb.control<OperatorAuditExportSinkKind>('siem', Validators.required),
    endpointReference: this.fb.control('', [Validators.required, Validators.pattern(/^secret:/)]),
    configChecksum: this.fb.control('', [Validators.required, Validators.pattern(/^sha256:[0-9a-f]{64}$/)]),
    enabled: this.fb.control(true),
    confirmed: this.fb.control(false, Validators.requiredTrue),
  });
  protected readonly protectedHistoryForm = this.fb.group({
    customerOrganizationIds: this.fb.control(''),
    deploymentTargetIds: this.fb.control(''),
    reviewerUserAccountId: this.fb.control('', Validators.required),
    confirmed: this.fb.control(false, Validators.requiredTrue),
  });
  protected readonly protectedHistoryLookupForm = this.fb.group({
    artifactId: this.fb.control('', Validators.required),
  });

  protected readonly events = signal<OperatorAuditRow[]>([]);
  protected readonly correlatedEvents = signal<OperatorControlPlaneAuditEvent[]>([]);
  protected readonly nextCursor = signal<string | undefined>(undefined);
  protected readonly detail = signal<OperatorAuditDetail | undefined>(undefined);
  protected readonly evidence = signal<OperatorEvidenceRef[]>([]);
  protected readonly bundle = signal<OperatorEvidenceBundle | undefined>(undefined);
  protected readonly sinks = signal<OperatorAuditExportSink[]>([]);
  protected readonly exportStatuses = signal<OperatorAuditExportStatus[]>([]);
  protected readonly protectedHistoryArtifact = signal<OperatorProtectedHistoryArtifact | undefined>(undefined);
  protected readonly protectedHistoryVerification = signal<OperatorProtectedHistoryArtifactVerification | undefined>(
    undefined
  );

  protected readonly loading = signal(true);
  protected readonly loadingMore = signal(false);
  protected readonly loadingDetail = signal(false);
  protected readonly buildingBundle = signal(false);
  protected readonly creatingSink = signal(false);
  protected readonly loadError = signal('');
  protected readonly correlatedEventsError = signal('');
  protected readonly detailError = signal('');
  protected readonly bundleError = signal('');
  protected readonly sinkError = signal('');
  protected readonly sinkSuccess = signal('');
  protected readonly exportStatusError = signal('');
  protected readonly protectedHistoryCreating = signal(false);
  protected readonly protectedHistoryLoading = signal(false);
  protected readonly protectedHistoryVerifying = signal(false);
  protected readonly protectedHistoryError = signal('');

  constructor() {
    void this.loadInitial();
  }

  protected async search(): Promise<void> {
    await this.loadEvents(false);
  }

  protected async loadMore(): Promise<void> {
    const cursor = this.nextCursor();
    if (!cursor || this.loadingMore()) return;

    this.loadingMore.set(true);
    this.loadError.set('');
    try {
      const page = await firstValueFrom(this.service.listAudit({...this.filterRequest(), cursor}));
      this.events.update((current) => {
        const known = new Set(current.map((event) => event.id));
        return [...current, ...page.items.filter((event) => !known.has(event.id))];
      });
      this.nextCursor.set(page.nextCursor);
    } catch (error) {
      this.loadError.set(this.readError(error, 'Could not load more audit events.'));
    } finally {
      this.loadingMore.set(false);
    }
  }

  protected async selectEvent(event: OperatorAuditRow): Promise<void> {
    this.loadingDetail.set(true);
    this.detailError.set('');
    this.detail.set(undefined);
    this.evidence.set([]);
    try {
      const [detailResponse, evidenceResponse] = await Promise.all([
        firstValueFrom(this.service.getAuditEvent(event.id)),
        firstValueFrom(this.service.getAuditEvidence(event.id)),
      ]);
      this.detail.set(detailResponse.detail);
      this.evidence.set(evidenceResponse.items);
    } catch (error) {
      this.detailError.set(
        this.statusOf(error) === 404
          ? 'This audit event was not found.'
          : this.readError(error, 'Could not load this audit event.')
      );
    } finally {
      this.loadingDetail.set(false);
    }
  }

  protected async buildBundle(): Promise<void> {
    if (this.bundleForm.invalid || this.buildingBundle()) {
      this.bundleForm.markAllAsTouched();
      return;
    }

    this.bundle.set(undefined);
    this.bundleError.set('');
    this.buildingBundle.set(true);
    try {
      const planId = this.bundleForm.getRawValue().deploymentPlanId.trim();
      this.bundle.set(await firstValueFrom(this.service.createEvidenceBundle(planId)));
    } catch (error) {
      this.bundleError.set(this.readError(error, 'Could not build the evidence bundle.'));
    } finally {
      this.buildingBundle.set(false);
    }
  }

  protected async createSink(): Promise<void> {
    this.sinkSuccess.set('');
    this.sinkError.set('');
    if (this.sinkForm.invalid || this.creatingSink()) {
      this.sinkForm.markAllAsTouched();
      return;
    }

    const value = this.sinkForm.getRawValue();
    this.creatingSink.set(true);
    try {
      await firstValueFrom(
        this.service.createAuditExportSink({
          name: value.name.trim(),
          kind: value.kind,
          endpointReference: value.endpointReference.trim(),
          configChecksum: value.configChecksum.trim(),
          enabled: value.enabled,
        })
      );
      this.sinkSuccess.set('Export sink created.');
      this.sinkForm.controls.confirmed.setValue(false);
      await Promise.all([this.loadSinks(), this.loadExportStatus()]);
    } catch (error) {
      this.sinkError.set(this.readError(error, 'Could not create the export sink.'));
    } finally {
      this.creatingSink.set(false);
    }
  }

  protected async createProtectedHistoryArtifact(): Promise<void> {
    this.protectedHistoryError.set('');
    if (this.protectedHistoryForm.invalid || this.protectedHistoryCreating()) {
      this.protectedHistoryForm.markAllAsTouched();
      return;
    }
    const value = this.protectedHistoryForm.getRawValue();
    const customerOrganizationIds = this.parseIdList(value.customerOrganizationIds);
    const deploymentTargetIds = this.parseIdList(value.deploymentTargetIds);
    if (customerOrganizationIds.length === 0 && deploymentTargetIds.length === 0) {
      this.protectedHistoryError.set('Enter at least one customer organization ID or deployment target ID.');
      return;
    }

    this.protectedHistoryCreating.set(true);
    this.protectedHistoryVerification.set(undefined);
    try {
      const artifact = await firstValueFrom(
        this.service.createProtectedHistoryArtifact({
          customerOrganizationIds,
          deploymentTargetIds,
          reviewerUserAccountId: value.reviewerUserAccountId.trim(),
        })
      );
      this.protectedHistoryArtifact.set(artifact);
      this.protectedHistoryLookupForm.controls.artifactId.setValue(artifact.id);
      this.protectedHistoryForm.controls.confirmed.setValue(false);
    } catch (error) {
      this.protectedHistoryError.set(this.readError(error, 'Could not retain the protected-history artifact.'));
    } finally {
      this.protectedHistoryCreating.set(false);
    }
  }

  protected async loadProtectedHistoryArtifact(): Promise<void> {
    if (this.protectedHistoryLookupForm.invalid || this.protectedHistoryLoading()) {
      this.protectedHistoryLookupForm.markAllAsTouched();
      return;
    }
    const artifactId = this.protectedHistoryLookupForm.controls.artifactId.value.trim();
    this.protectedHistoryError.set('');
    this.protectedHistoryArtifact.set(undefined);
    this.protectedHistoryVerification.set(undefined);
    this.protectedHistoryLoading.set(true);
    try {
      this.protectedHistoryArtifact.set(await firstValueFrom(this.service.getProtectedHistoryArtifact(artifactId)));
    } catch (error) {
      this.protectedHistoryError.set(this.readError(error, 'Could not load the protected-history artifact.'));
    } finally {
      this.protectedHistoryLoading.set(false);
    }
  }

  protected async verifyProtectedHistoryArtifact(): Promise<void> {
    const artifact = this.protectedHistoryArtifact();
    if (!artifact || this.protectedHistoryVerifying()) return;

    this.protectedHistoryError.set('');
    this.protectedHistoryVerification.set(undefined);
    this.protectedHistoryVerifying.set(true);
    try {
      this.protectedHistoryVerification.set(
        await firstValueFrom(this.service.verifyProtectedHistoryArtifact(artifact.id))
      );
    } catch (error) {
      this.protectedHistoryError.set(this.readError(error, 'Could not verify the protected-history artifact.'));
    } finally {
      this.protectedHistoryVerifying.set(false);
    }
  }

  protected stateLabel(value?: string): string {
    const normalized = value?.trim().toLowerCase() ?? '';
    return normalized === '' ? 'Unknown' : `${normalized.charAt(0).toUpperCase()}${normalized.slice(1)}`;
  }

  protected exportState(status: OperatorAuditExportStatus): string {
    if (!status.sink.enabled) return 'Disabled';
    if (status.alert || status.checkpointLag > 0) return 'Stale';
    return this.stateLabel(status.lastAttemptStatus ?? '');
  }

  protected correlationState(kind: string): string {
    return kind.trim().toLowerCase() === 'unknown' ? 'Unknown' : kind;
  }

  protected eventKey(event: OperatorAuditRow): string {
    return event.id;
  }

  protected evidenceKey(item: OperatorEvidenceRef): string {
    return item.id;
  }

  protected sinkKey(sink: OperatorAuditExportSink): string {
    return sink.id;
  }

  protected correlatedEventKey(event: OperatorControlPlaneAuditEvent): string {
    return event.id;
  }

  protected correlationEntries(event: OperatorControlPlaneAuditEvent): Array<{label: string; value: string}> {
    const labels: Partial<Record<keyof OperatorControlPlaneAuditEvent, string>> = {
      productReleaseId: 'Product release',
      componentReleaseId: 'Component release',
      targetConfigId: 'Target config',
      deploymentPlanId: 'Plan',
      approvalId: 'Approval',
      campaignRunId: 'Campaign run',
      executionId: 'Execution',
      executionAttemptId: 'Attempt',
      adapterRevisionId: 'Adapter',
      observationId: 'Observation',
      driftCaseId: 'Drift case',
      reconciliationId: 'Reconciliation',
      deploymentUnitId: 'Deployment unit',
    };
    return Object.entries(labels).flatMap(([key, label]) => {
      const value = event[key as keyof OperatorControlPlaneAuditEvent];
      return typeof value === 'string' && value ? [{label: label!, value}] : [];
    });
  }

  private async loadInitial(): Promise<void> {
    await Promise.all([this.loadEvents(true), this.loadCorrelatedEvents(), this.loadSinks(), this.loadExportStatus()]);
  }

  private async loadCorrelatedEvents(): Promise<void> {
    this.correlatedEventsError.set('');
    try {
      const page = await firstValueFrom(this.service.listControlPlaneAuditEvents({limit: 100}));
      this.correlatedEvents.set(page.items);
    } catch {
      this.correlatedEvents.set([]);
      this.correlatedEventsError.set(
        'The correlated protocol event stream is unavailable. Legacy audit remains visible.'
      );
    }
  }

  private async loadEvents(initial: boolean): Promise<void> {
    this.loading.set(true);
    this.loadError.set('');
    try {
      const page = await firstValueFrom(this.service.listAudit(this.filterRequest()));
      this.events.set(page.items);
      this.nextCursor.set(page.nextCursor);
    } catch (error) {
      this.events.set([]);
      this.nextCursor.set(undefined);
      this.loadError.set(
        this.statusOf(error) === 403
          ? 'You do not have permission to view control-plane audit.'
          : this.readError(error, 'Could not load control-plane audit.')
      );
    } finally {
      if (initial || !this.loadingMore()) this.loading.set(false);
    }
  }

  private async loadSinks(): Promise<void> {
    this.sinkError.set('');
    try {
      this.sinks.set(await firstValueFrom(this.service.listAuditExportSinks()));
    } catch (error) {
      this.sinks.set([]);
      this.sinkError.set(this.readError(error, 'Export sinks are unavailable.'));
    }
  }

  private async loadExportStatus(): Promise<void> {
    this.exportStatusError.set('');
    try {
      this.exportStatuses.set(await firstValueFrom(this.service.listAuditExportStatus()));
    } catch {
      this.exportStatuses.set([]);
      this.exportStatusError.set('Export status is unavailable.');
    }
  }

  private filterRequest(): OperatorAuditFilters {
    const value = this.filters.getRawValue();
    return {
      limit: 50,
      ...(value.action.trim() ? {action: value.action.trim()} : {}),
      ...(value.subjectType.trim() ? {subjectType: value.subjectType.trim()} : {}),
      ...(value.subjectId.trim() ? {subjectId: value.subjectId.trim()} : {}),
      ...(value.actorUserAccountId.trim() ? {actorUserAccountId: value.actorUserAccountId.trim()} : {}),
      ...(value.search.trim() ? {search: value.search.trim()} : {}),
    };
  }

  private readError(error: unknown, fallback: string): string {
    if (typeof error === 'object' && error !== null && 'message' in error) {
      const message = String(error.message).trim();
      if (message && message !== 'Unknown Error') return message;
    }
    return fallback;
  }

  private statusOf(error: unknown): number {
    return typeof error === 'object' && error !== null && 'status' in error ? Number(error.status) : 0;
  }

  private queryFilter(key: string): string {
    return this.route.snapshot.queryParamMap.get(key)?.trim() ?? '';
  }

  private parseIdList(value: string): string[] {
    return [
      ...new Set(
        value
          .split(/[\s,]+/)
          .map((id) => id.trim())
          .filter(Boolean)
      ),
    ];
  }
}
