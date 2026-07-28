import {ChangeDetectionStrategy, Component, inject, signal} from '@angular/core';
import {ActivatedRoute, RouterLink} from '@angular/router';
import {firstValueFrom, Observable} from 'rxjs';
import {OperatorControlPlaneService} from '../../services/operator-control-plane.service';
import {OverlayService} from '../../services/overlay.service';
import {
  OperatorCampaignControlAction,
  OperatorCampaignControlResult,
  OperatorCampaignDetail,
  OperatorCampaignMember,
  OperatorCampaignMemberControlAction,
  OperatorEvidenceRef,
  OperatorPlanFact,
} from '../../types/operator-control-plane';

@Component({
  selector: 'app-campaign-detail',
  imports: [RouterLink],
  templateUrl: './campaign-detail.component.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class CampaignDetailComponent {
  private readonly controlPlane = inject(OperatorControlPlaneService);
  private readonly overlay = inject(OverlayService);
  private readonly campaignId = inject(ActivatedRoute).snapshot.paramMap.get('campaignId') ?? '';
  private controlIntent: Observable<OperatorCampaignControlResult> | null = null;
  private memberControlIntent: Observable<unknown> | null = null;

  protected readonly detail = signal<OperatorCampaignDetail | null>(null);
  protected readonly evidence = signal<OperatorEvidenceRef[]>([]);
  protected readonly loading = signal(true);
  protected readonly evidenceLoading = signal(true);
  protected readonly detailState = signal<'forbidden' | 'not-found' | 'error' | ''>('');
  protected readonly evidenceError = signal(false);
  protected readonly expectedVersion = signal<number | null>(null);
  protected readonly reason = signal('');
  protected readonly protocolVersion = signal<'' | 'v1' | 'v2'>('');
  protected readonly actionLoading = signal(false);
  protected readonly actionError = signal('');
  protected readonly stale = signal(false);
  protected readonly retryAvailable = signal(false);

  constructor() {
    void this.load();
  }

  protected setReason(event: Event): void {
    this.reason.set((event.target as HTMLTextAreaElement).value);
  }

  protected setProtocolVersion(event: Event): void {
    const value = (event.target as HTMLSelectElement).value;
    this.protocolVersion.set(value === 'v1' || value === 'v2' ? value : '');
  }

  protected canControl(action: OperatorCampaignControlAction): boolean {
    const campaign = this.detail()?.campaign;
    if (!campaign?.runId || !this.expectedVersion() || !this.reason().trim() || this.actionLoading()) {
      return false;
    }
    const status = campaign.status.toUpperCase();
    if (action === 'pause') return status === 'RUNNING';
    if (action === 'resume') return status === 'PAUSED';
    return ['PENDING', 'RUNNING', 'PAUSED', 'BLOCKED'].includes(status);
  }

  protected canControlMember(member: OperatorCampaignMember, action: OperatorCampaignMemberControlAction): boolean {
    const campaign = this.detail()?.campaign;
    const campaignMutable = campaign
      ? ['PENDING', 'RUNNING', 'PAUSED', 'BLOCKED'].includes(campaign.status.toUpperCase())
      : false;
    return Boolean(
      campaignMutable &&
      campaign?.runId &&
      member.memberRunId &&
      this.expectedVersion() &&
      this.reason().trim() &&
      !this.actionLoading() &&
      (action === 'exclude' || (['FAILED', 'CANCELED'].includes(member.status.toUpperCase()) && this.protocolVersion()))
    );
  }

  protected async requestControl(action: OperatorCampaignControlAction): Promise<void> {
    const detail = this.detail();
    const expectedVersion = this.expectedVersion();
    if (!detail?.campaign.runId || !expectedVersion || !this.canControl(action)) return;

    const confirmed = await firstValueFrom(this.overlay.confirm(this.confirmation(action, detail.campaign.name)));
    if (!confirmed) return;

    this.controlIntent = this.controlPlane.controlCampaign(detail.campaign.runId, action, {
      expectedVersion,
      reason: this.reason().trim(),
    });
    await this.executeControlIntent();
  }

  protected async requestMemberControl(
    member: OperatorCampaignMember,
    action: OperatorCampaignMemberControlAction
  ): Promise<void> {
    const campaignRunId = this.detail()?.campaign.runId;
    const expectedVersion = this.expectedVersion();
    if (!campaignRunId || !member.memberRunId || !expectedVersion || !this.canControlMember(member, action)) return;

    const confirmed = await firstValueFrom(this.overlay.confirm(this.memberConfirmation(action, member)));
    if (!confirmed) return;

    this.memberControlIntent = this.controlPlane.controlCampaignMember(campaignRunId, action, {
      expectedVersion,
      reason: this.reason().trim(),
      memberRunId: member.memberRunId,
      ...(action === 'retry' ? {protocolVersion: this.protocolVersion() as 'v1' | 'v2'} : {}),
    });
    await this.executeMemberControlIntent();
  }

  protected async retryControl(): Promise<void> {
    if (this.actionLoading()) return;
    if (this.memberControlIntent) {
      await this.executeMemberControlIntent();
    } else if (this.controlIntent) {
      await this.executeControlIntent();
    }
  }

  protected statusLabel(status: string): string {
    return status.trim() && status !== 'UNKNOWN' ? status : 'Unknown';
  }

  protected orderedFacts(facts: OperatorPlanFact[]): OperatorPlanFact[] {
    return [...facts].sort((left, right) => left.order - right.order);
  }

  private async load(): Promise<void> {
    if (!this.campaignId) {
      this.detailState.set('not-found');
      this.loading.set(false);
      this.evidenceLoading.set(false);
      return;
    }
    await Promise.all([this.loadDetail(), this.loadEvidence()]);
  }

  private async loadDetail(): Promise<void> {
    this.detailState.set('');
    this.loading.set(true);
    try {
      const response = await firstValueFrom(this.controlPlane.getCampaign(this.campaignId));
      this.detail.set(response.detail);
      this.expectedVersion.set(response.detail.runVersion ?? null);
    } catch (error) {
      this.detail.set(null);
      const status = errorStatus(error);
      this.detailState.set(status === 403 ? 'forbidden' : status === 404 ? 'not-found' : 'error');
    } finally {
      this.loading.set(false);
    }
  }

  private async loadEvidence(): Promise<void> {
    this.evidenceLoading.set(true);
    this.evidenceError.set(false);
    try {
      const page = await firstValueFrom(this.controlPlane.getCampaignEvidence(this.campaignId));
      this.evidence.set(page.items);
    } catch {
      this.evidence.set([]);
      this.evidenceError.set(true);
    } finally {
      this.evidenceLoading.set(false);
    }
  }

  private async executeControlIntent(): Promise<void> {
    if (!this.controlIntent) return;
    this.actionLoading.set(true);
    this.actionError.set('');
    this.stale.set(false);
    this.retryAvailable.set(false);
    try {
      const result = await firstValueFrom(this.controlIntent);
      this.expectedVersion.set(result.run.version);
      this.detail.update((current) =>
        current
          ? {
              ...current,
              campaign: {...current.campaign, status: result.run.state},
              runVersion: result.run.version,
            }
          : current
      );
      this.controlIntent = null;
    } catch (error) {
      const status = errorStatus(error);
      if (status === 409) {
        this.stale.set(true);
        this.actionError.set('Campaign state is stale. Refresh and review the latest run version.');
      } else {
        this.actionError.set('The campaign action could not be completed.');
        this.retryAvailable.set(isRetryable(error));
      }
    } finally {
      this.actionLoading.set(false);
    }
  }

  private async executeMemberControlIntent(): Promise<void> {
    if (!this.memberControlIntent) return;
    this.actionLoading.set(true);
    this.actionError.set('');
    this.stale.set(false);
    this.retryAvailable.set(false);
    try {
      await firstValueFrom(this.memberControlIntent);
      this.memberControlIntent = null;
      await this.loadDetail();
    } catch (error) {
      const status = errorStatus(error);
      if (status === 409) {
        this.stale.set(true);
        this.actionError.set('Campaign state is stale. Refresh and review the latest run version.');
      } else {
        this.actionError.set('The member action could not be completed.');
        this.retryAvailable.set(isRetryable(error));
      }
    } finally {
      this.actionLoading.set(false);
    }
  }

  private confirmation(action: OperatorCampaignControlAction, campaignName: string) {
    const label = `${titleCase(action)} campaign`;
    const alertType =
      action === 'cancel' ? ('danger' as const) : action === 'pause' ? ('warning' as const) : ('info' as const);
    return {
      confirmLabel: label,
      ...(action === 'cancel' ? {requiredConfirmInputText: campaignName} : {}),
      message: {
        message: `${label} using the displayed expected run version and reason?`,
        alert: {
          type: alertType,
          message:
            action === 'cancel'
              ? 'Cancellation is irreversible and may require reconciliation.'
              : 'This changes campaign admissions and execution flow.',
        },
      },
    };
  }

  private memberConfirmation(action: OperatorCampaignMemberControlAction, member: OperatorCampaignMember) {
    const exclude = action === 'exclude';
    return {
      confirmLabel: exclude ? 'Exclude member' : 'Retry member',
      ...(exclude ? {requiredConfirmInputText: member.deploymentUnitId} : {}),
      message: {
        message: `${exclude ? 'Exclude' : 'Retry'} deployment unit ${member.deploymentUnitId}?`,
        alert: {
          type: exclude ? ('danger' as const) : ('warning' as const),
          message: exclude
            ? 'Exclusion leaves the campaign visibly incomplete and may create drift.'
            : `Retry will create a new immutable ${this.protocolVersion()} deployment plan.`,
        },
      },
    };
  }
}

function errorStatus(error: unknown): number {
  return typeof error === 'object' && error !== null && 'status' in error && typeof error.status === 'number'
    ? error.status
    : 0;
}

function isRetryable(error: unknown): boolean {
  return typeof error === 'object' && error !== null && 'retryable' in error && error.retryable === true;
}

function titleCase(value: string): string {
  return value.charAt(0).toUpperCase() + value.slice(1);
}
