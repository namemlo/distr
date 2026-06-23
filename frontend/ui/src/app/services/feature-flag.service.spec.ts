import {provideHttpClient} from '@angular/common/http';
import {HttpTestingController, provideHttpClientTesting} from '@angular/common/http/testing';
import {TestBed} from '@angular/core/testing';
import {of} from 'rxjs';
import {FeatureFlagService} from './feature-flag.service';
import {OrganizationService} from './organization.service';

describe('FeatureFlagService', () => {
  let http: HttpTestingController;
  let service: FeatureFlagService;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        {
          provide: OrganizationService,
          useValue: {
            get: () => of({features: [], subscriptionType: 'community'}),
          },
        },
      ],
    });
    http = TestBed.inject(HttpTestingController);
    service = TestBed.inject(FeatureFlagService);
  });

  afterEach(() => {
    http.verify();
  });

  it('loads experimental feature flags from the admin endpoint', () => {
    service.getExperimentalFeatureFlags().subscribe((flags) => {
      expect(flags).toEqual([
        {
          key: 'environments',
          label: 'Environments',
          description: 'Groups deployment targets by promotion stage or operational purpose.',
          milestone: 'Milestone B',
          enabled: true,
        },
      ]);
    });

    const req = http.expectOne('/api/v1/experimental-feature-flags');
    expect(req.request.method).toBe('GET');
    req.flush([
      {
        key: 'environments',
        label: 'Environments',
        description: 'Groups deployment targets by promotion stage or operational purpose.',
        milestone: 'Milestone B',
        enabled: true,
      },
    ]);
  });

  it('exposes release bundle, deployment process, scoped variable, deployment plan, task queue, runbook, retention policy, observability, and config as code feature flag state', () => {
    service.isReleaseBundlesEnabled$.subscribe((enabled) => {
      expect(enabled).toBe(true);
    });
    service.isDeploymentProcessesEnabled$.subscribe((enabled) => {
      expect(enabled).toBe(true);
    });
    service.isScopedVariablesV2Enabled$.subscribe((enabled) => {
      expect(enabled).toBe(true);
    });
    service.isDeploymentPlansEnabled$.subscribe((enabled) => {
      expect(enabled).toBe(true);
    });
    service.isTaskQueueEnabled$.subscribe((enabled) => {
      expect(enabled).toBe(true);
    });
    service.isRunbooksEnabled$.subscribe((enabled) => {
      expect(enabled).toBe(true);
    });
    service.isRetentionPoliciesEnabled$.subscribe((enabled) => {
      expect(enabled).toBe(true);
    });
    service.isObservabilityMetricsEnabled$.subscribe((enabled) => {
      expect(enabled).toBe(true);
    });
    service.isObservabilityTracingEnabled$.subscribe((enabled) => {
      expect(enabled).toBe(true);
    });
    service.isObservabilityDashboardsEnabled$.subscribe((enabled) => {
      expect(enabled).toBe(true);
    });
    service.isObservabilityCorrelationEnabled$.subscribe((enabled) => {
      expect(enabled).toBe(true);
    });
    service.isConfigAsCodeEnabled$.subscribe((enabled) => {
      expect(enabled).toBe(true);
    });

    const req = http.expectOne('/api/v1/experimental-feature-flags');
    expect(req.request.method).toBe('GET');
    req.flush([
      {
        key: 'release_bundles',
        label: 'Release Bundles',
        description: 'Draft and publish immutable release bundles.',
        milestone: 'Milestone C',
        enabled: true,
      },
      {
        key: 'deployment_processes',
        label: 'Deployment Processes',
        description: 'Create reusable deployment process revisions.',
        milestone: 'Milestone C',
        enabled: true,
      },
      {
        key: 'scoped_variables_v2',
        label: 'Scoped Variables',
        description: 'Manage typed variable sets and references.',
        milestone: 'Milestone C',
        enabled: true,
      },
      {
        key: 'deployment_plans',
        label: 'Deployment Plans',
        description: 'Preview deployment plans before execution.',
        milestone: 'Milestone D',
        enabled: true,
      },
      {
        key: 'task_queue',
        label: 'Task Queue',
        description: 'Create durable task records from deployment plans.',
        milestone: 'Milestone D',
        enabled: true,
      },
      {
        key: 'runbooks',
        label: 'Runbooks',
        description: 'Version operational workflows.',
        milestone: 'Milestone F',
        enabled: true,
      },
      {
        key: 'retention_policies',
        label: 'Retention Policies',
        description: 'Preview cleanup candidates before running retention jobs.',
        milestone: 'Milestone G',
        enabled: true,
      },
      {
        key: 'observability_metrics',
        label: 'Observability Metrics',
        description: 'Enables Prometheus metrics for HTTP traffic and task execution instrumentation.',
        milestone: 'Milestone G',
        enabled: true,
      },
      {
        key: 'observability_tracing',
        label: 'Observability Tracing',
        description: 'Enables OpenTelemetry tracing for HTTP traffic and task execution instrumentation.',
        milestone: 'Milestone G',
        enabled: true,
      },
      {
        key: 'observability_dashboards',
        label: 'Observability Dashboards',
        description: 'Publishes static Grafana dashboard templates for observability review.',
        milestone: 'Milestone G',
        enabled: true,
      },
      {
        key: 'observability_correlation',
        label: 'Observability Correlation',
        description: 'Builds deterministic Grafana links between traces, metrics, and dashboards.',
        milestone: 'Milestone G',
        enabled: true,
      },
      {
        key: 'config_as_code',
        label: 'Config as Code',
        description: 'Validates declarative configuration documents and governs Git-managed resource authority.',
        milestone: 'Milestone H',
        enabled: true,
      },
    ]);
  });
});
