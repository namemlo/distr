export type ExperimentalFeatureFlagKey =
  | 'environments'
  | 'lifecycles'
  | 'channels'
  | 'release_bundles'
  | 'deployment_processes'
  | 'scoped_variables_v2'
  | 'deployment_plans'
  | 'deployment_timeline'
  | 'step_templates'
  | 'task_queue'
  | 'runbooks'
  | 'retention_policies'
  | 'observability_metrics'
  | 'observability_tracing'
  | 'observability_dashboards'
  | 'observability_correlation';

export interface ExperimentalFeatureFlag {
  key: ExperimentalFeatureFlagKey;
  label: string;
  description: string;
  milestone: string;
  enabled: boolean;
}
