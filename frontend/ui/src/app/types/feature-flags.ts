export type ExperimentalFeatureFlagKey =
  | 'environments'
  | 'lifecycles'
  | 'channels'
  | 'release_bundles'
  | 'deployment_processes'
  | 'scoped_variables_v2'
  | 'deployment_plans'
  | 'runbooks';

export interface ExperimentalFeatureFlag {
  key: ExperimentalFeatureFlagKey;
  label: string;
  description: string;
  milestone: string;
  enabled: boolean;
}
