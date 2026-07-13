import {DatePipe, DecimalPipe} from '@angular/common';
import {ChangeDetectionStrategy, Component, inject, signal, TemplateRef, viewChild} from '@angular/core';
import {FormBuilder, ReactiveFormsModule, Validators} from '@angular/forms';
import {Router} from '@angular/router';
import {Application, DeploymentTarget} from '@distr-sh/distr-sdk';
import {FontAwesomeModule} from '@fortawesome/angular-fontawesome';
import {
  faDownload,
  faEye,
  faListCheck,
  faMagnifyingGlass,
  faPlay,
  faPlus,
  faRotateRight,
  faTriangleExclamation,
  faXmark,
} from '@fortawesome/free-solid-svg-icons';
import {firstValueFrom, forkJoin, startWith, take} from 'rxjs';
import {getFormDisplayedError} from '../../util/errors';
import {AutotrimDirective} from '../directives/autotrim.directive';
import {ApplicationsService} from '../services/applications.service';
import {ChannelsService} from '../services/channels.service';
import {DeploymentPlansService} from '../services/deployment-plans.service';
import {DeploymentTargetsService} from '../services/deployment-targets.service';
import {EnvironmentsService} from '../services/environments.service';
import {DialogRef, OverlayService} from '../services/overlay.service';
import {ReleaseBundlesService} from '../services/release-bundles.service';
import {ToastService} from '../services/toast.service';
import {Channel} from '../types/channel';
import {DeploymentPlan, DeploymentPlanIssueSeverity, DeploymentPlanVariable} from '../types/deployment-plan';
import {Environment} from '../types/environment';
import {ReleaseBundle} from '../types/release-bundle';

@Component({
  templateUrl: './deployment-plans.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  imports: [ReactiveFormsModule, FontAwesomeModule, DecimalPipe, DatePipe, AutotrimDirective],
})
export class DeploymentPlansComponent {
  protected readonly faMagnifyingGlass = faMagnifyingGlass;
  protected readonly faPlay = faPlay;
  protected readonly faPlus = faPlus;
  protected readonly faListCheck = faListCheck;
  protected readonly faEye = faEye;
  protected readonly faDownload = faDownload;
  protected readonly faRotateRight = faRotateRight;
  protected readonly faTriangleExclamation = faTriangleExclamation;
  protected readonly faXmark = faXmark;

  private readonly deploymentPlansService = inject(DeploymentPlansService);
  private readonly releaseBundlesService = inject(ReleaseBundlesService);
  private readonly applicationsService = inject(ApplicationsService);
  private readonly channelsService = inject(ChannelsService);
  private readonly environmentsService = inject(EnvironmentsService);
  private readonly deploymentTargetsService = inject(DeploymentTargetsService);
  private readonly toast = inject(ToastService);
  private readonly overlay = inject(OverlayService);
  private readonly router = inject(Router);
  private readonly fb = inject(FormBuilder).nonNullable;

  protected readonly deploymentPlans = signal<DeploymentPlan[]>([]);
  protected readonly releaseBundles = signal<ReleaseBundle[]>([]);
  protected readonly applications = signal<Application[]>([]);
  protected readonly channels = signal<Channel[]>([]);
  protected readonly environments = signal<Environment[]>([]);
  protected readonly deploymentTargets = signal<DeploymentTarget[]>([]);
  protected readonly loading = signal(true);
  protected readonly loadError = signal<string | undefined>(undefined);
  protected readonly formLoading = signal(false);
  protected readonly filteredDeploymentPlans = signal<DeploymentPlan[]>([]);
  protected readonly selectedDeploymentPlan = signal<DeploymentPlan | undefined>(undefined);
  protected readonly executionLoadingPlanId = signal<string | undefined>(undefined);

  protected readonly filterForm = this.fb.group({
    search: this.fb.control(''),
  });

  protected readonly planForm = this.fb.group({
    releaseBundleId: this.fb.control('', [Validators.required]),
    environmentId: this.fb.control('', [Validators.required]),
    targetIds: this.fb.control<string[]>([], [Validators.required]),
  });

  private readonly createPlanDialog = viewChild.required<TemplateRef<unknown>>('createPlanDialog');
  private readonly detailDialog = viewChild.required<TemplateRef<unknown>>('detailDialog');
  private modalRef?: DialogRef;

  constructor() {
    this.filterForm.controls.search.valueChanges.pipe(startWith('')).subscribe((search) => {
      this.applyFilter(search);
    });
    this.planForm.controls.releaseBundleId.valueChanges.subscribe((releaseBundleId) => {
      this.ensureTargetsForRelease(releaseBundleId);
    });
    this.load();
  }

  protected load() {
    this.loading.set(true);
    this.loadError.set(undefined);
    forkJoin({
      deploymentPlans: this.deploymentPlansService.list(),
      releaseBundles: this.releaseBundlesService.list(),
      applications: this.applicationsService.list().pipe(take(1)),
      channels: this.channelsService.list(),
      environments: this.environmentsService.list(),
      deploymentTargets: this.deploymentTargetsService.list().pipe(take(1)),
    }).subscribe({
      next: ({deploymentPlans, releaseBundles, applications, channels, environments, deploymentTargets}) => {
        this.deploymentPlans.set(deploymentPlans);
        this.releaseBundles.set(releaseBundles);
        this.applications.set(applications);
        this.channels.set(channels);
        this.environments.set(environments);
        this.deploymentTargets.set(deploymentTargets);
        this.applyFilter(this.filterForm.controls.search.value);
        this.loading.set(false);
      },
      error: (e) => {
        this.loadError.set(getFormDisplayedError(e) ?? 'Failed to load deployment plans.');
        this.loading.set(false);
      },
    });
  }

  protected showCreateDialog() {
    this.closeDialog(false);
    this.selectedDeploymentPlan.set(undefined);
    const releaseBundleId = this.publishedReleaseBundles()[0]?.id ?? '';
    const environmentId = this.environments()[0]?.id ?? '';
    this.planForm.reset({
      releaseBundleId,
      environmentId,
      targetIds: this.targetsForRelease(releaseBundleId).map((target) => target.id ?? ''),
    });
    this.modalRef = this.overlay.showModal(this.createPlanDialog());
  }

  protected showDetailDialog(plan: DeploymentPlan) {
    this.closeDialog(false);
    this.selectedDeploymentPlan.set(plan);
    this.modalRef = this.overlay.showModal(this.detailDialog());
  }

  protected closeDialog(reset = true) {
    this.modalRef?.close();
    if (reset) {
      this.planForm.reset();
      this.selectedDeploymentPlan.set(undefined);
    }
  }

  protected async submitForm() {
    this.planForm.markAllAsTouched();
    if (this.planForm.invalid || this.planForm.controls.targetIds.value.length === 0) {
      return;
    }

    this.formLoading.set(true);
    try {
      const value = this.planForm.getRawValue();
      await firstValueFrom(
        this.deploymentPlansService.create({
          releaseBundleId: value.releaseBundleId,
          environmentId: value.environmentId,
          targetIds: value.targetIds,
        })
      );
      this.toast.success('Deployment plan created');
      this.closeDialog();
      this.load();
    } catch (e) {
      const msg = getFormDisplayedError(e);
      if (msg) {
        this.toast.error(msg);
      }
    } finally {
      this.formLoading.set(false);
    }
  }

  protected canExecute(plan: DeploymentPlan): boolean {
    return plan.status === 'READY' && this.issueCount(plan, 'blocker') === 0;
  }

  protected async executePlan(plan: DeploymentPlan) {
    if (!this.canExecute(plan) || this.executionLoadingPlanId()) {
      return;
    }

    const targetCount = plan.targets.length;
    const confirmed = await firstValueFrom(
      this.overlay.confirm({
        message: {
          message: `Execute this deployment plan for ${targetCount} ${targetCount === 1 ? 'target' : 'targets'}?`,
          alert: {
            type: 'warning',
            message: 'This queues immutable deployment tasks using the resolved release, process, and variables.',
          },
        },
        confirmLabel: 'Execute plan',
      })
    );
    if (!confirmed) {
      return;
    }

    this.executionLoadingPlanId.set(plan.id);
    try {
      const tasks = await firstValueFrom(this.deploymentPlansService.execute(plan.id));
      const taskCount = tasks.length;
      this.toast.success(`Deployment started for ${taskCount} ${taskCount === 1 ? 'target' : 'targets'}`);
      this.closeDialog();
      await this.router.navigate(['/deployment-timeline']);
    } catch (e) {
      this.toast.error(getFormDisplayedError(e) ?? 'Failed to execute deployment plan.');
    } finally {
      this.executionLoadingPlanId.set(undefined);
    }
  }

  protected publishedReleaseBundles(): ReleaseBundle[] {
    return this.releaseBundles().filter((bundle) => bundle.status === 'PUBLISHED');
  }

  protected targetsForRelease(releaseBundleId: string): DeploymentTarget[] {
    const bundle = this.releaseBundles().find((candidate) => candidate.id === releaseBundleId);
    const application = bundle
      ? this.applications().find((candidate) => candidate.id === bundle.applicationId)
      : undefined;
    if (!application) {
      return [];
    }
    return this.deploymentTargets().filter((target) => target.type === application.type);
  }

  protected issueCount(plan: DeploymentPlan, severity: DeploymentPlanIssueSeverity): number {
    return plan.issues.filter((issue) => issue.severity === severity).length;
  }

  protected issues(plan: DeploymentPlan, severity: DeploymentPlanIssueSeverity) {
    return plan.issues.filter((issue) => issue.severity === severity);
  }

  protected applicationName(applicationId: string): string {
    return this.applications().find((application) => application.id === applicationId)?.name ?? applicationId;
  }

  protected channelName(channelId: string): string {
    return this.channels().find((channel) => channel.id === channelId)?.name ?? channelId;
  }

  protected environmentName(environmentId: string): string {
    return this.environments().find((environment) => environment.id === environmentId)?.name ?? environmentId;
  }

  protected releaseLabel(releaseBundleId: string): string {
    return this.releaseBundles().find((bundle) => bundle.id === releaseBundleId)?.releaseNumber ?? releaseBundleId;
  }

  protected statusClass(status: string): string {
    switch (status) {
      case 'READY':
        return 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-300';
      case 'BLOCKED':
        return 'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-300';
      case 'EXPIRED':
      case 'EXECUTED':
        return 'bg-gray-100 text-gray-800 dark:bg-gray-700 dark:text-gray-300';
      default:
        return 'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-300';
    }
  }

  protected deploymentPlanJson(plan: DeploymentPlan): string {
    return JSON.stringify(plan, null, 2);
  }

  protected deploymentPlanMarkdown(plan: DeploymentPlan): string {
    const lines = [
      `# Deployment Plan ${plan.id}`,
      '',
      `Status: ${plan.status}`,
      `Checksum: \`${plan.canonicalChecksum}\``,
      `Release Bundle: ${this.releaseLabel(plan.releaseBundleId)}`,
      `Application: ${this.applicationName(plan.applicationId)}`,
      `Channel: ${this.channelName(plan.channelId)}`,
      `Environment: ${this.environmentName(plan.environmentId)}`,
      ...(plan.releaseContract
        ? [
            '',
            '## Frozen Release Contract',
            `- Schema: ${plan.releaseContract.schema}`,
            `- Build: ${plan.releaseContract.build.externalId}`,
            `- Source: ${plan.releaseContract.source.repository}@${plan.releaseContract.source.builtCommit}`,
            ...plan.releaseContract.components.map(
              (component) => `- ${component.name}: ${component.image} (${component.platform})`
            ),
            `- Compose: ${plan.releaseContract.config.composePath} (${plan.releaseContract.config.composeChecksum})`,
            `- Config: ${plan.releaseContract.config.serviceConfigPath} (${plan.releaseContract.config.serviceConfigChecksum})`,
            `- Changes: ${plan.releaseContract.changes.summary}`,
          ]
        : []),
      '',
      '## Blockers',
      ...this.issueLines(plan, 'blocker'),
      '',
      '## Warnings',
      ...this.issueLines(plan, 'warning'),
      '',
      '## Targets',
      ...this.targetLines(plan),
      '',
      '## Steps',
      ...this.stepLines(plan),
      '',
      '## Variables',
      ...this.variableLines(plan),
      '',
    ];
    return lines.join('\n');
  }

  protected downloadJson(plan: DeploymentPlan) {
    this.downloadText(`deployment-plan-${plan.id}.json`, this.deploymentPlanJson(plan), 'application/json');
  }

  protected downloadMarkdown(plan: DeploymentPlan) {
    this.downloadText(`deployment-plan-${plan.id}.md`, this.deploymentPlanMarkdown(plan), 'text/markdown');
  }

  private issueLines(plan: DeploymentPlan, severity: DeploymentPlanIssueSeverity): string[] {
    const issues = this.issues(plan, severity);
    if (issues.length === 0) {
      return ['- None'];
    }
    return issues.map((issue) => `- ${issue.field ? `${issue.field}: ` : ''}${issue.message} (${issue.code})`);
  }

  private targetLines(plan: DeploymentPlan): string[] {
    if (plan.targets.length === 0) {
      return ['- None'];
    }
    return plan.targets.map((target) => `- ${target.name} (${target.type}, ${target.deploymentTargetId})`);
  }

  private stepLines(plan: DeploymentPlan): string[] {
    if (plan.steps.length === 0) {
      return ['- None'];
    }
    return plan.steps.map((step) => {
      const state = step.included ? 'included' : `excluded: ${step.excludedReason || 'not applicable'}`;
      return `- ${step.sortOrder}. ${step.name} [${step.actionName || step.actionType}] - ${state}`;
    });
  }

  private variableLines(plan: DeploymentPlan): string[] {
    if (plan.variables.length === 0) {
      return ['- None'];
    }
    return plan.variables.map((variable) => {
      const required = variable.isRequired ? ', required' : '';
      return `- ${variable.key}: ${variable.status}/${variable.source}/${variable.type}${required} = ${this.variableValue(variable)}`;
    });
  }

  private variableValue(variable: DeploymentPlanVariable): string {
    if (variable.redacted) {
      return 'redacted';
    }
    if (variable.referenceName || variable.referenceId) {
      return variable.referenceName || variable.referenceId || '-';
    }
    if (variable.value === undefined || variable.value === null) {
      return '-';
    }
    return typeof variable.value === 'string' ? variable.value : JSON.stringify(variable.value);
  }

  private downloadText(filename: string, content: string, type: string) {
    const blob = new Blob([content], {type});
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement('a');
    anchor.href = url;
    anchor.download = filename;
    anchor.click();
    URL.revokeObjectURL(url);
  }

  private ensureTargetsForRelease(releaseBundleId: string) {
    const availableTargetIds = new Set(this.targetsForRelease(releaseBundleId).map((target) => target.id));
    const selectedTargetIds = this.planForm.controls.targetIds.value.filter((targetId) =>
      availableTargetIds.has(targetId)
    );
    if (selectedTargetIds.length !== this.planForm.controls.targetIds.value.length) {
      this.planForm.controls.targetIds.setValue(selectedTargetIds);
    }
  }

  private applyFilter(search: string) {
    const normalized = search.toLowerCase();
    this.filteredDeploymentPlans.set(
      this.deploymentPlans().filter((plan) => {
        const applicationName = this.applicationName(plan.applicationId).toLowerCase();
        const releaseLabel = this.releaseLabel(plan.releaseBundleId).toLowerCase();
        const environmentName = this.environmentName(plan.environmentId).toLowerCase();
        return (
          normalized.length === 0 ||
          plan.id.toLowerCase().includes(normalized) ||
          plan.status.toLowerCase().includes(normalized) ||
          plan.canonicalChecksum.toLowerCase().includes(normalized) ||
          applicationName.includes(normalized) ||
          releaseLabel.includes(normalized) ||
          environmentName.includes(normalized)
        );
      })
    );
  }
}
