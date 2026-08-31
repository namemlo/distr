export const browserEvidenceTitle =
  '@evidence proves the reference client DEV release, approval, and previous-state journey';

export const browserScreenshotNames = [
  '01-version-build.png',
  '02-accumulated-changelog.png',
  '03-dependency-constraints.png',
  '04-plan-approval-pending.png',
  '05-approval-request.png',
  '06-approval-approved.png',
  '07-plan-approval-satisfied.png',
  '08-previous-state-plan.png',
  '09-previous-state-comparison.png',
  '10-release-b-history-preserved.png',
  '11-immutable-history-audit.png',
];

export const browserCheckpointManifestName = 'AC-63-checkpoints.json';
export const browserCheckpointManifestSchema = 'distr.control-plane-browser-checkpoints/v1';

export const browserCheckpointClaims = [
  checkpoint(1, 'version-build', 'vendorAdmin', '/releases/00000000-0000-4000-8000-000000000203', {
    entityIds: {releaseId: '00000000-0000-4000-8000-000000000203'},
    checksums: {release: digest('1')},
  }),
  checkpoint(2, 'accumulated-changelog', 'vendorAdmin', '/releases/00000000-0000-4000-8000-000000000203', {
    entityIds: {releaseId: '00000000-0000-4000-8000-000000000203'},
    checksums: {baseline: digest('8')},
  }),
  checkpoint(3, 'dependency-constraints', 'vendorAdmin', '/releases/00000000-0000-4000-8000-000000000203', {
    entityIds: {releaseId: '00000000-0000-4000-8000-000000000203'},
    checksums: {graph: digest('6')},
  }),
  checkpoint(4, 'plan-approval-pending', 'vendorAdmin', '/deployments/plans/00000000-0000-4000-8000-000000000301', {
    entityIds: {planId: '00000000-0000-4000-8000-000000000301'},
    checksums: {plan: digest('1')},
  }),
  checkpoint(5, 'approval-request', 'scopedApprover', '/approvals', {
    entityIds: {
      approvalId: '00000000-0000-4000-8000-000000000701',
      planId: '00000000-0000-4000-8000-000000000301',
    },
    checksums: {approval: digest('c')},
  }),
  checkpoint(6, 'approval-approved', 'scopedApprover', '/approvals', {
    entityIds: {
      approvalId: '00000000-0000-4000-8000-000000000701',
      planId: '00000000-0000-4000-8000-000000000301',
    },
    checksums: {approval: digest('c')},
  }),
  checkpoint(7, 'plan-approval-satisfied', 'vendorAdmin', '/deployments/plans/00000000-0000-4000-8000-000000000301', {
    entityIds: {planId: '00000000-0000-4000-8000-000000000301'},
    checksums: {plan: digest('1')},
  }),
  checkpoint(8, 'previous-state-plan', 'vendorAdmin', '/deployments/plans/00000000-0000-4000-8000-000000000304', {
    entityIds: {
      planId: '00000000-0000-4000-8000-000000000304',
      sourcePlanId: '00000000-0000-4000-8000-000000000301',
      successfulPlanId: '00000000-0000-4000-8000-000000000303',
    },
    checksums: {plan: digest('a')},
  }),
  checkpoint(9, 'previous-state-comparison', 'vendorAdmin', '/deployments/plans/00000000-0000-4000-8000-000000000304', {
    entityIds: {
      leftPlanId: '00000000-0000-4000-8000-000000000304',
      rightPlanId: '00000000-0000-4000-8000-000000000301',
    },
    checksums: {left: digest('a'), right: digest('1')},
  }),
  checkpoint(10, 'release-b-history-preserved', 'vendorAdmin', '/releases/00000000-0000-4000-8000-000000000203', {
    entityIds: {
      releaseId: '00000000-0000-4000-8000-000000000203',
      previousStatePlanId: '00000000-0000-4000-8000-000000000304',
    },
    checksums: {release: digest('1')},
  }),
  checkpoint(11, 'immutable-history-audit', 'vendorAdmin', '/audit', {
    entityIds: {
      planId: '00000000-0000-4000-8000-000000000301',
      previousStatePlanId: '00000000-0000-4000-8000-000000000304',
    },
    checksums: {audit: digest('c')},
  }),
];

function digest(character) {
  return `sha256:${character.repeat(64)}`;
}

function checkpoint(sequence, slug, actor, route, evidence) {
  return {sequence, slug, actor, route, ...evidence};
}
