export const neutralLiveAcceptanceId = 'AC-53';
export const neutralLiveOwner = 'PR-081';
export const neutralLiveProfile = 'pr081-neutral-live';
export const neutralLiveResultSchema = 'distr.control-plane-neutral-live-result/v1';
export const neutralLiveEvidenceSchema = 'distr.control-plane-acceptance-evidence/v1';
export const neutralLiveTestResultSchema = 'distr.control-plane-test-result/v1';
export const neutralLiveAutomatedTest = 'examples/control-plane-e2e/reference-executor/main_test.go';
export const neutralLiveManualEvidence = 'docs/fork/PR-081_NEUTRAL_CONTROL_PLANE_PROOF.md';
export const neutralLiveRunnerCommand = [
  'node',
  'examples/control-plane-e2e/run.mjs',
  '--mode',
  'clean',
  '--acceptance',
  '--json',
];

// Dependency metadata is excluded: this list binds the code and fixture that execute the local proof.
export const neutralLiveExecutionSourcePaths = [
  'examples/control-plane-e2e/run.mjs',
  'examples/control-plane-e2e/fixture.json',
  'examples/control-plane-e2e/compose.yaml',
  'examples/control-plane-e2e/external-executor.mjs',
  'examples/control-plane-e2e/observer.mjs',
  'examples/control-plane-e2e/reference-executor/main.go',
  'examples/control-plane-e2e/reference-executor/main_test.go',
  'examples/control-plane-e2e/provenance-fixture/main.go',
];
