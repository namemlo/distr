import type {Page} from '@playwright/test';
import {
  attachContractEvidence,
  attachVisualCheckpoint,
  attachVisualCheckpointManifest,
  expect,
  fixtureIds,
  test,
} from './fixtures/control-plane';

test.describe('operator control room route-mocked contract', () => {
  test.describe('vendor administrator', () => {
    test.use({actor: 'vendorAdmin'});

    test('receives 403 for approval decisions outside the administrator fixture authority', async ({
      page,
      controlPlane,
    }) => {
      const response = await postFromPage(
        page,
        `/api/v1/approval-requests/${fixtureIds.approval}/decisions`,
        approvalDecisionBody()
      );
      expect(response.status).toBe(403);
      expect(response.body).toMatchObject({code: 'FORBIDDEN'});
      expect(controlPlane.successfulActions).toEqual([]);

      const approval = await getFromPage(page, `/api/v1/approval-requests/${fixtureIds.approval}`);
      expect(approval.status).toBe(200);
      expect(approval.body).toMatchObject({state: 'PENDING', decisions: []});

      const plan = await getFromPage(page, `/api/v1/control-plane/plans/${fixtureIds.plan}`);
      expect(plan.body).toMatchObject({
        detail: {
          approvals: [expect.objectContaining({message: 'Production approval is pending', blocking: true})],
          issues: [expect.objectContaining({message: 'Approval must be satisfied before execution', blocking: true})],
        },
      });
      const audit = await getFromPage(page, '/api/v1/control-plane/audit');
      expect(audit.body).toMatchObject({items: []});
    });

    test('opens setup readiness and registry import without contacting an adopter system', async ({
      page,
      controlPlane,
    }, testInfo) => {
      await page.goto('/setup');

      await expect(page.getByRole('heading', {name: 'Control-plane setup'})).toBeVisible();
      await expect(page.getByText('Feature readiness')).toBeVisible();
      await expect(page.getByText('Enrollment readiness')).toBeVisible();
      await expect(page.getByText('Configuration readiness')).toBeVisible();
      await expect(page.getByRole('heading', {name: 'Registry setup'})).toBeVisible();

      await page.getByLabel('Evidence reference').fill('fixture://inventory/production');
      await page
        .getByLabel('Evidence checksum')
        .fill('sha256:1111111111111111111111111111111111111111111111111111111111111111');
      await page.getByLabel('Roots (JSON)').fill(
        JSON.stringify([
          {
            key: 'production',
            name: 'Production root',
            deliveryModel: 'dedicated',
            classification: 'needs_decision',
            deploymentTargetId: fixtureIds.target,
            environmentId: fixtureIds.environment,
            physicalIdentity: 'fixture-production-root',
            placements: [{componentKey: 'payments-api', physicalName: 'payments-api'}],
          },
        ])
      );
      await page.getByRole('button', {name: 'Preview import'}).click();
      const classificationReview = page
        .getByRole('heading', {name: '2. Review classifications and topology'})
        .locator('xpath=ancestor::section[1]');
      await expect(classificationReview).toBeVisible();
      await classificationReview.locator('select').selectOption('standard');
      await page.getByLabel('I have reviewed the classifications and preview changes.').check();
      await page.getByRole('button', {name: 'Apply import'}).click();
      await expect(page.getByText('Registry import applied.')).toBeVisible();

      const setupReadiness = page.getByRole('region', {name: 'Setup readiness'});
      await setupReadiness.getByLabel('Registry import ID').fill(fixtureIds.registryImport);
      await setupReadiness.getByLabel('Deployment unit ID').fill(fixtureIds.unitA);
      await setupReadiness.getByRole('button', {name: 'Check setup readiness'}).click();
      await expect(setupReadiness.getByText('Overall readiness').locator('..')).toContainText('Ready');
      await expect
        .poll(() => controlPlane.actions.map((action) => action.path))
        .toEqual(
          expect.arrayContaining([
            '/api/v1/deployment-registry/imports/preview',
            `/api/v1/deployment-registry/imports/${fixtureIds.registryImport}/decisions`,
            `/api/v1/deployment-registry/imports/${fixtureIds.registryImport}/apply`,
          ])
        );

      await attachContractEvidence(testInfo, 'setup-import-boundary.json', {
        actor: 'vendorAdmin',
        externalTraffic: 'blocked',
        actions: controlPlane.actions,
      });
    });

    test('shows component and product releases and compares immutable release evidence', async ({page}, testInfo) => {
      await page.goto('/releases');

      await expect(page.getByRole('heading', {name: 'Releases'})).toBeVisible();
      await expect(page.getByRole('table', {name: 'Release history'})).toContainText('component');
      await expect(page.getByRole('table', {name: 'Release history'})).toContainText('product');

      await page.goto(`/releases/${fixtureIds.productRelease}`);
      await expect(page.getByRole('heading', {name: 'Release detail'})).toBeVisible();
      await page.getByLabel('Compare with release').fill(fixtureIds.componentRelease);
      await page.getByRole('button', {name: 'Compare releases'}).click();
      await expect(page.getByRole('heading', {name: 'Release comparison'})).toBeVisible();
      await expect(page.getByText('digest_changed')).toBeVisible();

      await attachContractEvidence(testInfo, 'release-comparison.json', {
        left: fixtureIds.productRelease,
        right: fixtureIds.componentRelease,
      });
    });

    test('@evidence proves the reference client DEV release, approval, and previous-state journey', async ({
      page,
      controlPlane,
    }, testInfo) => {
      test.setTimeout(90_000);
      const expectedNodeVersion = process.env.DISTR_EVIDENCE_NODE_VERSION;
      if (expectedNodeVersion !== undefined) {
        expect(process.versions.node).toBe(expectedNodeVersion);
      }
      const checkpoints = [];
      await page.goto(`/releases/${fixtureIds.productRelease}`);
      await expect(page.getByText('2026.08.0', {exact: true})).toBeVisible();
      await expect(page.getByText('Reference Client DEV', {exact: true})).toBeVisible();

      const buildProof = page
        .getByRole('heading', {name: 'Source and build proof'})
        .locator('xpath=ancestor::section[1]');
      await expect(buildProof).toContainText('ledger-api');
      await expect(buildProof).toContainText('distr.component-release/v2');
      await expect(buildProof).toContainText('https://slsa.dev/provenance/v1');
      await expect(buildProof).toContainText('VERIFIED');
      await expect(buildProof).toContainText('fixture-build-ledger-43');
      checkpoints.push(
        await attachVisualCheckpoint(
          page,
          testInfo,
          {
            sequence: 1,
            slug: 'version-build',
            actor: 'vendorAdmin',
            entityIds: {releaseId: fixtureIds.productRelease},
            checksums: {release: 'sha256:1111111111111111111111111111111111111111111111111111111111111111'},
          },
          buildProof
        )
      );

      const changelog = page
        .getByRole('heading', {name: 'Accumulated changelog'})
        .locator('xpath=ancestor::section[1]');
      await expect(changelog).toContainText('code · orders-api');
      await expect(changelog).toContainText('migration · ledger-api');
      await expect(changelog).toContainText('2.4.1');
      await expect(changelog).toContainText('2.4.2');
      checkpoints.push(
        await attachVisualCheckpoint(
          page,
          testInfo,
          {
            sequence: 2,
            slug: 'accumulated-changelog',
            actor: 'vendorAdmin',
            entityIds: {releaseId: fixtureIds.productRelease},
            checksums: {baseline: 'sha256:8888888888888888888888888888888888888888888888888888888888888888'},
          },
          changelog
        )
      );

      const graph = page.getByRole('heading', {name: 'Release graph'}).locator('xpath=ancestor::section[1]');
      await expect(graph).toContainText('ledger.transaction.v1');
      await expect(graph).toContainText('>=4.2.0 <5.0.0');
      await expect(graph).toContainText('provider_deploy_and_health_before_consumer');
      checkpoints.push(
        await attachVisualCheckpoint(
          page,
          testInfo,
          {
            sequence: 3,
            slug: 'dependency-constraints',
            actor: 'vendorAdmin',
            entityIds: {releaseId: fixtureIds.productRelease},
            checksums: {graph: 'sha256:6666666666666666666666666666666666666666666666666666666666666666'},
          },
          graph
        )
      );

      await page.goto(`/deployments/plans/${fixtureIds.plan}`);
      await expect(page.getByRole('heading', {name: 'Plan review'})).toBeVisible();
      await expect(page.getByRole('region', {name: 'Provider requirements'})).toContainText(
        'ledger.transaction.v1 >=4.2.0 <5.0.0'
      );
      await expect(page.getByRole('region', {name: 'Steps'})).toContainText('1. Deploy ledger-api 4.3.0');
      await expect(page.getByRole('region', {name: 'Steps'})).toContainText(
        '2. Verify ledger-api health before orders-api'
      );
      const pendingApprovals = page.getByRole('region', {name: 'Approvals'});
      await expect(pendingApprovals).toContainText('Production approval is pending');
      checkpoints.push(
        await attachVisualCheckpoint(
          page,
          testInfo,
          {
            sequence: 4,
            slug: 'plan-approval-pending',
            actor: 'vendorAdmin',
            entityIds: {planId: fixtureIds.plan},
            checksums: {plan: 'sha256:1111111111111111111111111111111111111111111111111111111111111111'},
          },
          pendingApprovals
        )
      );
      const auditBeforeApproval = await getFromPage(page, '/api/v1/control-plane/audit');
      expect(auditBeforeApproval.body).toMatchObject({items: []});

      await controlPlane.signInAs('scopedApprover');
      await page.goto('/approvals');
      await page.getByRole('button', {name: 'View approval'}).first().click();
      const decisionComment = page.getByLabel('Decision comment');
      await decisionComment.fill('Verified immutable release and provider-first plan');
      checkpoints.push(
        await attachVisualCheckpoint(
          page,
          testInfo,
          {
            sequence: 5,
            slug: 'approval-request',
            actor: 'scopedApprover',
            entityIds: {approvalId: fixtureIds.approval, planId: fixtureIds.plan},
            checksums: {approval: 'sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc'},
          },
          decisionComment
        )
      );
      await page.getByRole('button', {name: 'Approve'}).click();
      await confirmOverlay(page, 'Approve');
      const approvedState = page.getByRole('table').getByText('APPROVED', {exact: true});
      await expect(approvedState).toBeVisible();
      checkpoints.push(
        await attachVisualCheckpoint(
          page,
          testInfo,
          {
            sequence: 6,
            slug: 'approval-approved',
            actor: 'scopedApprover',
            entityIds: {approvalId: fixtureIds.approval, planId: fixtureIds.plan},
            checksums: {approval: 'sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc'},
          },
          approvedState
        )
      );
      const auditAfterApproval = await getFromPage(page, '/api/v1/control-plane/audit');
      expect(auditAfterApproval.body).toMatchObject({
        items: [
          expect.objectContaining({
            action: 'approval.decided',
            subjectId: fixtureIds.plan,
            actorUserAccountId: fixtureIds.scopedApprover,
            outcome: 'APPROVE',
            correlationCount: 2,
          }),
        ],
      });
      const auditDetailAfterApproval = await getFromPage(page, `/api/v1/control-plane/audit/${fixtureIds.audit}`);
      expect(auditDetailAfterApproval.body).toMatchObject({
        detail: {
          event: {
            action: 'approval.decided',
            subjectType: 'deployment_plan',
            subjectId: fixtureIds.plan,
            actorUserAccountId: fixtureIds.scopedApprover,
            outcome: 'APPROVE',
            correlationCount: 2,
          },
          correlations: [
            {kind: 'deployment_plan', id: fixtureIds.plan},
            {kind: 'approval', id: fixtureIds.approval},
          ],
          payload: {
            decisionId: fixtureIds.approvalDecision,
            requirementId: fixtureIds.approvalRequirement,
            requestRevision: 3,
            approvalRequestState: 'APPROVED',
          },
          evidence: [
            expect.objectContaining({id: fixtureIds.plan, kind: 'deployment_plan'}),
            expect.objectContaining({id: fixtureIds.approval, kind: 'approval'}),
          ],
        },
      });

      await controlPlane.signInAs('vendorAdmin');
      await page.goto(`/deployments/plans/${fixtureIds.plan}`);
      const satisfiedApprovals = page.getByRole('region', {name: 'Approvals'});
      await expect(satisfiedApprovals).toContainText('Production approval is satisfied');
      await expect(page.getByText('Approval must be satisfied before execution')).toHaveCount(0);
      checkpoints.push(
        await attachVisualCheckpoint(
          page,
          testInfo,
          {
            sequence: 7,
            slug: 'plan-approval-satisfied',
            actor: 'vendorAdmin',
            entityIds: {planId: fixtureIds.plan},
            checksums: {plan: 'sha256:1111111111111111111111111111111111111111111111111111111111111111'},
          },
          satisfiedApprovals
        )
      );

      await page.getByLabel('Successful plan ID').fill(fixtureIds.publishedPlan);
      await page.getByLabel('Reason').fill('Restore the last independently verified state');
      await page.getByRole('button', {name: 'Create previous-state plan'}).click();
      await confirmOverlay(page, 'Create previous-state plan', fixtureIds.publishedPlan);
      await expect(page).toHaveURL(new RegExp(`/deployments/plans/${fixtureIds.previousPlan}$`));
      await expect(page.getByText('2026.07.0', {exact: true})).toBeVisible();
      const previousStateChanges = page.getByRole('region', {name: 'Changes'});
      await expect(previousStateChanges).toContainText('orders-api 2.5.0 to 2.4.0');
      checkpoints.push(
        await attachVisualCheckpoint(
          page,
          testInfo,
          {
            sequence: 8,
            slug: 'previous-state-plan',
            actor: 'vendorAdmin',
            entityIds: {
              planId: fixtureIds.previousPlan,
              sourcePlanId: fixtureIds.plan,
              successfulPlanId: fixtureIds.publishedPlan,
            },
            checksums: {plan: 'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'},
          },
          previousStateChanges
        )
      );

      await page.getByLabel('Other plan ID').fill(fixtureIds.plan);
      await page.getByRole('button', {name: 'Compare'}).click();
      const previousStateComparison = page.getByText(
        'Previous-state plan restores release A while preserving release B'
      );
      await expect(previousStateComparison).toBeVisible();
      checkpoints.push(
        await attachVisualCheckpoint(
          page,
          testInfo,
          {
            sequence: 9,
            slug: 'previous-state-comparison',
            actor: 'vendorAdmin',
            entityIds: {leftPlanId: fixtureIds.previousPlan, rightPlanId: fixtureIds.plan},
            checksums: {
              left: 'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
              right: 'sha256:1111111111111111111111111111111111111111111111111111111111111111',
            },
          },
          previousStateComparison
        )
      );

      await page.goto(`/releases/${fixtureIds.productRelease}`);
      await expect(page.getByText('2026.08.0', {exact: true})).toBeVisible();
      await expect(page.getByText('Reference Client DEV', {exact: true})).toBeVisible();
      const preservedRelease = page
        .getByRole('heading', {name: 'Release detail'})
        .locator('xpath=ancestor::section[1]');
      checkpoints.push(
        await attachVisualCheckpoint(
          page,
          testInfo,
          {
            sequence: 10,
            slug: 'release-b-history-preserved',
            actor: 'vendorAdmin',
            entityIds: {releaseId: fixtureIds.productRelease, previousStatePlanId: fixtureIds.previousPlan},
            checksums: {release: 'sha256:1111111111111111111111111111111111111111111111111111111111111111'},
          },
          preservedRelease
        )
      );

      await page.goto('/audit');
      const auditEvents = page.getByRole('heading', {name: 'Audit events'}).locator('xpath=ancestor::section[1]');
      await expect(auditEvents).toContainText('approval.decided');
      await expect(auditEvents).toContainText('deployment_plan.previous_state_created');
      checkpoints.push(
        await attachVisualCheckpoint(
          page,
          testInfo,
          {
            sequence: 11,
            slug: 'immutable-history-audit',
            actor: 'vendorAdmin',
            entityIds: {planId: fixtureIds.plan, previousStatePlanId: fixtureIds.previousPlan},
            checksums: {audit: 'sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc'},
          },
          auditEvents
        )
      );
      expect(controlPlane.externalAttempts).toEqual([]);
      expect(checkpoints).toHaveLength(11);
      await attachVisualCheckpointManifest(testInfo, checkpoints);
    });

    test('assembles, validates, and publishes a component release with typed confirmation', async ({
      page,
      controlPlane,
    }, testInfo) => {
      await page.goto('/releases');
      await page.getByLabel('Component release request (JSON)').fill(
        JSON.stringify({
          applicationId: fixtureIds.applicationPayments,
          channelId: fixtureIds.channelStable,
          releaseNumber: '25',
          releaseNotes: 'Payments component fixture',
          sourceRevision: '0123456789abcdef',
          components: [],
          releaseContract: {
            schema: 'distr.component-release/v2',
            componentKey: 'payments-api',
            version: '2.5.0',
            source: {
              repository: 'https://example.invalid/payments.git',
              requestedRef: 'refs/tags/2.5.0',
              commit: '0123456789abcdef',
            },
            build: {id: 'fixture-build-25', builder: 'fixture-ci'},
            artifacts: [],
            provides: [],
            requires: [],
            migrations: [],
            changes: {summary: 'Fixture release', commits: ['0123456789abcdef']},
            evidence: {provenance: [], sbom: [], signatures: [], tests: []},
          },
        })
      );
      const createResponse = page.waitForResponse(
        (response) =>
          new URL(response.url()).pathname === '/api/v1/release-bundles' && response.request().method() === 'POST'
      );
      await page.getByRole('button', {name: 'Create component release'}).click();
      const componentCreateContract = await createResponse;
      expect(componentCreateContract.status()).toBe(200);
      await expect(componentCreateContract.json()).resolves.toMatchObject({
        id: fixtureIds.componentReleaseDraft,
        status: 'DRAFT',
      });
      await expect(page.getByText(`Component draft ${fixtureIds.componentReleaseDraft}`)).toBeVisible();
      await page.getByRole('button', {name: 'Validate component release'}).click();
      await expect(page.getByText('Component release is valid and ready to publish.')).toBeVisible();
      await page.getByRole('button', {name: 'Publish component release'}).click();
      await confirmOverlay(page, 'Confirm', fixtureIds.componentReleaseDraft);
      await expect(page).toHaveURL(new RegExp(`/releases/${fixtureIds.componentReleaseDraft}$`));
      await attachContractEvidence(testInfo, 'component-release-assembly.json', {
        releaseId: fixtureIds.componentReleaseDraft,
        actions: controlPlane.actions.filter((action) => action.path.startsWith('/api/v1/release-bundles')),
      });
    });

    test('assembles, validates, and publishes a checksum-pinned product release', async ({
      page,
      controlPlane,
    }, testInfo) => {
      await page.goto('/releases');
      await page.getByLabel('Product release request (JSON)').fill(
        JSON.stringify({
          schema: 'distr.product-release/v1',
          applicationId: fixtureIds.applicationSuite,
          channelId: fixtureIds.channelStable,
          product: 'fixture-suite',
          version: '2026.08',
          dependencyPolicyVersion: 'policy-v1',
          releaseNotes: 'Fixture product release',
          requiredPlatforms: ['linux/amd64'],
          components: [
            {
              componentReleaseId: fixtureIds.componentReleaseDraft,
              componentReleaseChecksum: 'sha256:2222222222222222222222222222222222222222222222222222222222222222',
            },
          ],
          requirements: [],
        })
      );
      const createResponse = page.waitForResponse(
        (response) =>
          new URL(response.url()).pathname === '/api/v1/product-releases' && response.request().method() === 'POST'
      );
      await page.getByRole('button', {name: 'Create product release'}).click();
      const productCreateContract = await createResponse;
      expect(productCreateContract.status()).toBe(200);
      await expect(productCreateContract.json()).resolves.toMatchObject({
        id: fixtureIds.productReleaseDraft,
        status: 'DRAFT',
      });
      await expect(page.getByText(`Product draft ${fixtureIds.productReleaseDraft}`)).toBeVisible();
      await page.getByRole('button', {name: 'Validate product release'}).click();
      await expect(page.getByText('Product release is valid and ready to publish.')).toBeVisible();
      await page.getByRole('button', {name: 'Publish product release'}).click();
      await confirmOverlay(page, 'Confirm', fixtureIds.productReleaseDraft);
      await expect(page).toHaveURL(new RegExp(`/releases/${fixtureIds.productReleaseDraft}$`));
      await attachContractEvidence(testInfo, 'product-release-assembly.json', {
        releaseId: fixtureIds.productReleaseDraft,
        actions: controlPlane.actions.filter((action) => action.path.startsWith('/api/v1/product-releases')),
      });
    });

    test('compares both deployment units sharing one target', async ({page}, testInfo) => {
      await page.goto('/fleet');

      await expect(page.getByRole('heading', {name: 'Fleet', exact: true})).toBeVisible();
      await expect(page.getByRole('table', {name: 'Fleet matrix'})).toContainText('Customer A unit');
      await expect(page.getByRole('table', {name: 'Fleet matrix'})).toContainText('Customer B unit');
      await page.getByRole('button', {name: 'Compare shared target Shared production host'}).first().click();
      const comparison = page
        .getByRole('heading', {name: 'Shared target comparison'})
        .locator('xpath=ancestor::section[1]');
      await expect(comparison).toBeVisible();
      await expect(comparison.getByText('Customer A unit', {exact: true})).toBeVisible();
      await expect(comparison.getByText('Customer B unit', {exact: true})).toBeVisible();

      await attachContractEvidence(testInfo, 'shared-target-comparison.json', {
        target: fixtureIds.target,
        deploymentUnits: [fixtureIds.unitA, fixtureIds.unitB],
      });
    });

    test('keeps every blocking plan checksum visible on the detail route', async ({page}, testInfo) => {
      await page.goto(`/deployments/plans/${fixtureIds.plan}`);

      await expect(page.getByRole('heading', {name: 'Plan review'})).toBeVisible();
      for (const label of [
        'Canonical plan checksum',
        'Product release checksum',
        'Target config checksum',
        'Effective policy checksum',
        'Subscriber set checksum',
        'Graph checksum',
        'Change checksum',
        'Baseline checksum',
        'Provider resolution checksum',
        'Migration checksum',
        'Risk checksum',
        'Approval checksum',
        'Window checksum',
        'Adapter checksum',
        'Intent checksum',
      ]) {
        await expect(page.getByText(label, {exact: true})).toBeVisible();
      }
      await expect(page.getByText('Approval must be satisfied before execution')).toBeVisible();
      await expect(page.getByText('Shared-host blast radius requires approval')).toBeVisible();

      await attachContractEvidence(testInfo, 'plan-review-contract.json', {
        planId: fixtureIds.plan,
        visibleChecksumCount: 15,
        blockingFacts: ['approval', 'shared-host blast radius'],
      });
    });

    test('reloads the resulting immutable plan for publish and previous-state navigation', async ({page}) => {
      await page.goto(`/deployments/plans/${fixtureIds.plan}`);
      await page.getByLabel('Draft ID').fill(fixtureIds.planDraft);
      await page.getByLabel('Expected revision').fill('4');
      await page
        .getByLabel('Expected preview checksum')
        .fill('sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa');
      await page.getByRole('button', {name: 'Publish draft'}).click();
      await confirmOverlay(
        page,
        'Publish immutable plan',
        'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
      );
      await expect(page).toHaveURL(new RegExp(`/deployments/plans/${fixtureIds.publishedPlan}$`));
      await expect(page.getByText(fixtureIds.publishedPlan, {exact: true})).toBeVisible();

      await page.goto(`/deployments/plans/${fixtureIds.plan}`);
      await page.getByLabel('Successful plan ID').fill(fixtureIds.publishedPlan);
      await page.getByLabel('Reason').fill('Restore the last independently verified state');
      await page.getByRole('button', {name: 'Create previous-state plan'}).click();
      await confirmOverlay(page, 'Create previous-state plan', fixtureIds.publishedPlan);
      await expect(page).toHaveURL(new RegExp(`/deployments/plans/${fixtureIds.previousPlan}$`));
      await expect(page.getByText(fixtureIds.previousPlan, {exact: true})).toBeVisible();
    });

    test('preserves query parameters and fragment through the legacy target deep link', async ({page}, testInfo) => {
      await page.goto(`/deployments/${fixtureIds.target}?view=history#evidence`);

      await expect(page).toHaveURL(new RegExp(`/deployments/targets/${fixtureIds.target}\\?view=history#evidence$`));
      await attachContractEvidence(testInfo, 'legacy-deep-link.json', {
        input: `/deployments/${fixtureIds.target}?view=history#evidence`,
        expected: `/deployments/targets/${fixtureIds.target}?view=history#evidence`,
      });
    });

    test('renders partial, stale, and unknown fleet evidence explicitly', async ({page}) => {
      await page.goto('/fleet');

      await expect(page.getByRole('table', {name: 'Fleet matrix'})).toContainText('partial');
      await expect(page.getByRole('table', {name: 'Fleet matrix'})).toContainText('stale');
      await expect(page.getByRole('table', {name: 'Fleet matrix'})).toContainText('unknown');
    });

    test('renders a loading state before fleet data arrives', async ({page, controlPlane}) => {
      controlPlane.setScenario('loading');
      await page.goto('/fleet');

      await expect(page.getByRole('status')).toContainText(/loading/i);
      await expect(page.getByRole('table', {name: 'Fleet matrix'})).toBeVisible();
    });

    test('renders a bounded empty state', async ({page, controlPlane}) => {
      controlPlane.setScenario('empty');
      await page.goto('/fleet');

      await expect(page.getByRole('heading', {name: 'Fleet', exact: true})).toBeVisible();
      await expect(page.getByText(/no fleet|no deployments|no results/i)).toBeVisible();
    });

    test('renders the structured backend error and request identifier', async ({page, controlPlane}, testInfo) => {
      controlPlane.setScenario('error');
      await page.goto('/fleet');

      const alert = page.getByText('The fleet could not be loaded. Try again.', {exact: true});
      await expect(alert).toContainText('The fleet could not be loaded. Try again.');
      await expect(alert).not.toContainText('request-fixture-503');
      await attachContractEvidence(testInfo, 'structured-error.json', {
        status: 503,
        code: 'CONTROL_PLANE_UNAVAILABLE',
        requestId: 'request-fixture-503',
      });
    });

    test('hides the control room when the process feature flag is disabled', async ({page, controlPlane}) => {
      controlPlane.setScenario('disabled');
      await page.goto('/fleet');

      await expect(page).not.toHaveURL(/\/fleet$/);
      await expect(page.getByRole('heading', {name: 'Fleet'})).toHaveCount(0);
    });
  });

  test.describe('scoped approver', () => {
    test.use({actor: 'scopedApprover'});

    test('denies campaign controls and keeps an authorized rejection from unblocking the plan', async ({
      page,
      controlPlane,
    }) => {
      const response = await postFromPage(page, `/api/v1/deployment-campaigns/${fixtureIds.campaignRun}/pause`, {
        requestId: 'fixture-denied-campaign',
        expectedVersion: 3,
        reason: 'Must be denied',
      });
      expect(response.status).toBe(403);
      expect(response.body).toMatchObject({code: 'FORBIDDEN'});

      const rejection = await postFromPage(page, `/api/v1/approval-requests/${fixtureIds.approval}/decisions`, {
        ...approvalDecisionBody(),
        decision: 'REJECT',
        comment: 'Provider-first evidence is insufficient',
        idempotencyKey: 'fixture-approval-rejected',
      });
      expect(rejection.status).toBe(200);
      expect(rejection.body).toMatchObject({decision: 'REJECT'});
      expect(controlPlane.successfulActions).toEqual([
        expect.objectContaining({
          path: `/api/v1/approval-requests/${fixtureIds.approval}/decisions`,
          body: expect.objectContaining({decision: 'REJECT'}),
        }),
      ]);

      const approval = await getFromPage(page, `/api/v1/approval-requests/${fixtureIds.approval}`);
      expect(approval.body).toMatchObject({
        state: 'REJECTED',
        decisions: [expect.objectContaining({decision: 'REJECT'})],
      });

      const plan = await getFromPage(page, `/api/v1/control-plane/plans/${fixtureIds.plan}`);
      expect(plan.body).toMatchObject({
        detail: {
          approvals: [expect.objectContaining({message: 'Production approval was rejected', blocking: true})],
          issues: [expect.objectContaining({message: 'Approval must be satisfied before execution', blocking: true})],
        },
      });
      const audit = await getFromPage(page, '/api/v1/control-plane/audit');
      expect(audit.body).toMatchObject({items: []});
    });

    test('approves the checksum-bound request with the expected immutable revision', async ({
      page,
      controlPlane,
    }, testInfo) => {
      await page.goto('/approvals');

      await expect(page.getByRole('heading', {name: 'Approvals'})).toBeVisible();
      await expect(page.getByText('Invalidated by server', {exact: true})).toBeVisible();
      await expect(page.getByText('PLAN_CHECKSUM_CHANGED', {exact: true})).toBeVisible();
      await page.getByRole('button', {name: 'View approval'}).first().click();
      await expect(page.getByRole('heading', {name: 'Approval request'})).toBeVisible();
      await page.getByLabel('Decision comment').fill('Reviewed production checksum and blockers');
      const decisionResponse = page.waitForResponse(
        (response) =>
          response.url().endsWith(`/api/v1/approval-requests/${fixtureIds.approval}/decisions`) &&
          response.request().method() === 'POST'
      );
      await page.getByRole('button', {name: 'Approve'}).click();
      await confirmOverlay(page, 'Approve');
      const decisionContract = await decisionResponse;
      expect(decisionContract.status()).toBe(200);
      await expect(decisionContract.json()).resolves.toMatchObject({
        approvalRequestId: fixtureIds.approval,
        approvalRequirementId: fixtureIds.approvalRequirement,
        decision: 'APPROVE',
        requestRevision: 3,
      });
      await expect(page.getByRole('table').getByText('APPROVED', {exact: true})).toBeVisible();
      await expect
        .poll(() => controlPlane.actions.some((action) => action.path.endsWith(`/${fixtureIds.approval}/decisions`)))
        .toBe(true);

      await attachContractEvidence(testInfo, 'approval-decision.json', {
        expectedRequestRevision: 3,
        planChecksum: 'sha256:1111…',
        action: controlPlane.actions.find((item) => item.path.endsWith(`/${fixtureIds.approval}/decisions`)),
      });
    });
  });

  test.describe('executor operator', () => {
    test.use({actor: 'executorOperator'});

    test('receives 403 for approval decisions outside the execution scope', async ({page}) => {
      const response = await postFromPage(
        page,
        `/api/v1/approval-requests/${fixtureIds.approval}/decisions`,
        approvalDecisionBody()
      );
      expect(response.status).toBe(403);
      expect(response.body).toMatchObject({code: 'FORBIDDEN'});
    });

    test('mirrors execution cancel and status-query backend response contracts', async ({page}) => {
      const cancel = await postFromPage(page, `/api/v1/executions/${fixtureIds.execution}/cancel`, {
        idempotencyKey: 'fixture-cancel-key',
        reason: 'Contract assertion',
      });
      expect(cancel).toEqual({status: 204, body: null});

      const status = await postFromPage(page, `/api/v1/executions/${fixtureIds.execution}/status-queries`, {
        idempotencyKey: 'fixture-status-key',
        reason: 'Contract assertion',
        expiresInSeconds: 60,
      });
      expect(status.status).toBe(200);
      expect(status.body).toMatchObject({
        query: {
          id: fixtureIds.executionStatusQuery,
          executionId: fixtureIds.execution,
          executionAttemptId: fixtureIds.executionAttempt,
          idempotencyKey: 'fixture-status-key',
          status: 'PENDING',
          requestedTtlSeconds: 60,
        },
      });
    });

    test('pauses and resumes a campaign through versioned controls', async ({page, controlPlane}, testInfo) => {
      await page.goto(`/deployments/campaigns/${fixtureIds.campaign}`);

      await expect(page.getByRole('heading', {name: 'Production canary'})).toBeVisible();
      await expect(page.getByLabel(/run version/i)).toHaveValue('3');
      await page.getByLabel(/reason/i).fill('Validate canary observation');
      await page.getByRole('button', {name: 'Pause campaign'}).click();
      await confirmOverlay(page, 'Pause campaign');
      await expect.poll(() => controlPlane.actions.some((action) => action.path.endsWith('/pause'))).toBe(true);

      await expect(page.getByLabel(/run version/i)).toHaveValue('4');
      await page.getByLabel(/reason/i).fill('Canary observation is healthy');
      await page.getByRole('button', {name: 'Resume campaign'}).click();
      await confirmOverlay(page, 'Resume campaign');
      await expect.poll(() => controlPlane.actions.some((action) => action.path.endsWith('/resume'))).toBe(true);

      await attachContractEvidence(testInfo, 'campaign-controls.json', {
        campaignId: fixtureIds.campaign,
        actions: controlPlane.actions.filter((action) => action.path.includes('/deployment-campaigns/')),
      });
    });

    test('shows previous known state and requests current execution status', async ({page, controlPlane}, testInfo) => {
      await page.goto(`/deployments/executions/${fixtureIds.execution}`);

      await expect(page.getByRole('heading', {name: `Execution ${fixtureIds.execution}`})).toBeVisible();
      await expect(page.getByText('payments-api 2.4.0 was last healthy')).toBeVisible();
      await expect(page.getByRole('link', {name: 'View executions for this plan'})).toHaveAttribute(
        'href',
        new RegExp(`deploymentPlanId=${fixtureIds.plan}`)
      );
      const statusForm = page
        .locator('form')
        .filter({has: page.getByRole('heading', {name: 'Request current status'})});
      await statusForm.getByLabel('Reason').fill('Confirm agent state after observer delay');
      await statusForm.getByLabel('Expires in seconds').fill('60');
      await statusForm.getByRole('button', {name: 'Request current status'}).click();
      await expect
        .poll(() => controlPlane.actions.some((action) => action.path.endsWith('/status-queries')))
        .toBe(true);

      await attachContractEvidence(testInfo, 'execution-previous-state.json', {
        executionId: fixtureIds.execution,
        previousState: 'payments-api 2.4.0',
        actions: controlPlane.actions.filter((action) => action.path.includes('/executions/')),
      });
    });

    test('resolves drift with a reason and retains the evidence deep link', async ({page, controlPlane}, testInfo) => {
      await page.goto('/reconciliation');

      await expect(page.getByRole('heading', {name: 'Reconciliation'})).toBeVisible();
      await page.getByRole('button', {name: 'View reconciliation'}).click();
      await expect(page.getByRole('heading', {name: 'Reconciliation case'})).toBeVisible();
      await page.getByLabel('Resolution action').selectOption('RESTORE_DESIRED');
      await page.getByLabel('Resolution reason').fill('Restore the checksum-bound desired release');
      await page.getByLabel('Outcome observation ID').fill('observation-1');
      await page.getByRole('button', {name: 'Resolve case'}).click();
      await confirmOverlay(page, 'Resolve case');
      await expect
        .poll(() => controlPlane.actions.some((action) => action.path.endsWith(`/${fixtureIds.drift}/resolve`)))
        .toBe(true);
      await expect(page.getByRole('heading', {name: 'Evidence'})).toBeVisible();

      await attachContractEvidence(testInfo, 'drift-resolution.json', {
        driftCaseId: fixtureIds.drift,
        action: controlPlane.actions.find((item) => item.path.endsWith(`/${fixtureIds.drift}/resolve`)),
      });
    });
  });

  test.describe('audit viewer', () => {
    test.use({actor: 'auditViewer'});

    test('receives 403 for audit export sink mutation while retaining audit reads', async ({page}) => {
      const response = await postFromPage(page, '/api/v1/control-plane-audit/export-sinks', {
        name: 'Denied sink',
        kind: 'siem',
        endpointReference: 'secret://fixture/denied',
        configChecksum: 'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
        enabled: true,
      });
      expect(response.status).toBe(403);
      expect(response.body).toMatchObject({code: 'FORBIDDEN'});
    });

    test('opens an audit deep link and preserves correlated evidence metadata', async ({
      page,
      controlPlane,
    }, testInfo) => {
      await page.goto('/');
      await controlPlane.signInAs('scopedApprover');
      const approval = await postFromPage(page, `/api/v1/approval-requests/${fixtureIds.approval}/decisions`, {
        ...approvalDecisionBody(),
        comment: 'Authorize audit-reader fixture event',
        idempotencyKey: 'fixture-audit-reader-approval',
      });
      expect(approval.status).toBe(200);
      await controlPlane.signInAs('auditViewer');
      await page.goto(`/audit?subjectType=deployment_plan&subjectId=${fixtureIds.plan}`);

      await expect(page.getByRole('heading', {name: 'Control-plane audit'})).toBeVisible();
      await expect(page.getByLabel('Subject type')).toHaveValue('deployment_plan');
      await expect(page.getByLabel('Subject ID')).toHaveValue(fixtureIds.plan);
      await expect(page.getByText('approval.decided')).toBeVisible();
      await expect(page.getByText(fixtureIds.plan)).toBeVisible();
      await page.getByRole('button', {name: 'View'}).click();
      const detail = page.getByRole('heading', {name: 'Audit event'}).locator('xpath=ancestor::section[1]');
      await expect(detail.getByText(fixtureIds.audit, {exact: true})).toBeVisible();
      await expect(detail.getByRole('link', {name: 'Build provenance'})).toHaveAttribute(
        'href',
        `/deployments/plans/${fixtureIds.plan}`
      );
      await page.getByLabel('Deployment plan ID').fill(fixtureIds.plan);
      const evidenceResponse = page.waitForResponse(
        (response) =>
          new URL(response.url()).pathname === '/api/v1/control-plane-audit/evidence-bundles' &&
          response.request().method() === 'POST'
      );
      await page.getByRole('button', {name: 'Build evidence bundle'}).click();
      const evidenceContract = await evidenceResponse;
      expect(evidenceContract.status()).toBe(200);
      await expect(evidenceContract.json()).resolves.toMatchObject({
        deploymentPlanId: fixtureIds.plan,
        events: [
          expect.objectContaining({
            id: fixtureIds.audit,
            eventType: 'approval.decided',
            actorId: fixtureIds.scopedApprover,
            outcome: 'APPROVE',
            deploymentPlanId: fixtureIds.plan,
            approvalId: fixtureIds.approval,
            deploymentPlanChecksum: 'sha256:1111111111111111111111111111111111111111111111111111111111111111',
            policyChecksum: 'sha256:4444444444444444444444444444444444444444444444444444444444444444',
            approvalChecksum: 'sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc',
            payload: {
              decisionId: fixtureIds.approvalDecision,
              requirementId: fixtureIds.approvalRequirement,
              requestRevision: 3,
              approvalRequestState: 'APPROVED',
            },
            payloadRedacted: false,
            payloadTruncated: false,
          }),
        ],
        checksum: 'sha256:1111111111111111111111111111111111111111111111111111111111111111',
      });
      const bundle = page.getByRole('heading', {name: 'Evidence bundle'}).locator('xpath=ancestor::section[1]');
      await expect(bundle).toContainText('sha256:1111111111111111111111111111111111111111111111111111111111111111');

      await attachContractEvidence(testInfo, 'audit-deep-link.json', {
        subjectType: 'deployment_plan',
        subjectId: fixtureIds.plan,
        expectedSequence: 42,
        actions: controlPlane.actions.filter(
          (action) =>
            action.path === `/api/v1/control-plane/audit/${fixtureIds.audit}` ||
            action.path === '/api/v1/control-plane-audit/evidence-bundles'
        ),
      });
    });
  });

  test.describe('unauthorized user', () => {
    test.use({actor: 'unauthorized'});

    test('receives 403 for every vendor mutation attempted directly', async ({page, controlPlane}) => {
      const response = await postFromPage(page, `/api/v1/executions/${fixtureIds.execution}/cancel`, {
        idempotencyKey: 'fixture-unauthorized',
        reason: 'Must be denied',
      });
      expect(response.status).toBe(403);
      expect(response.body).toMatchObject({code: 'FORBIDDEN'});

      const previousState = await postFromPage(page, `/api/v1/deployment-plans/${fixtureIds.plan}/previous-state`, {
        successfulDeploymentPlanId: fixtureIds.publishedPlan,
        reason: 'This denied attempt must not create history',
      });
      expect(previousState.status).toBe(403);
      expect(controlPlane.successfulActions).toEqual([]);

      const previousStatePlan = await getFromPage(page, `/api/v1/control-plane/plans/${fixtureIds.previousPlan}`);
      expect(previousStatePlan.status).toBe(404);

      const audit = await getFromPage(page, '/api/v1/control-plane/audit');
      expect(audit.body).toMatchObject({items: []});
      expect((audit.body as {items: Array<{action: string}>}).items).not.toEqual(
        expect.arrayContaining([expect.objectContaining({action: 'deployment_plan.previous_state_created'})])
      );
    });

    test('redirects a customer reader away from vendor control-plane routes', async ({page}) => {
      await page.goto('/fleet');

      await expect(page).not.toHaveURL(/\/fleet$/);
      await expect(page.getByRole('heading', {name: 'Fleet'})).toHaveCount(0);
    });
  });
});

async function confirmOverlay(page: Page, label: string, requiredText?: string): Promise<void> {
  const overlay = page.locator('.cdk-overlay-container');
  await expect(overlay.getByRole('heading', {name: 'Confirm'})).toBeVisible();
  if (requiredText) {
    await overlay.locator('input#deleteConfirm').fill(requiredText);
  }
  await overlay.getByRole('button', {name: label, exact: true}).click();
}

async function postFromPage(page: Page, path: string, body: unknown): Promise<{status: number; body: unknown}> {
  await page.goto('/');
  return page.evaluate(
    async ({path, body}) => {
      const response = await fetch(path, {
        method: 'POST',
        headers: {'content-type': 'application/json'},
        body: JSON.stringify(body),
      });
      const text = await response.text();
      return {status: response.status, body: text ? JSON.parse(text) : null};
    },
    {path, body}
  );
}

async function getFromPage(page: Page, path: string): Promise<{status: number; body: unknown}> {
  await page.goto('/');
  return page.evaluate(async (path) => {
    const response = await fetch(path);
    const text = await response.text();
    return {status: response.status, body: text ? JSON.parse(text) : null};
  }, path);
}

function approvalDecisionBody() {
  return {
    approvalRequirementId: fixtureIds.approvalRequirement,
    decision: 'APPROVE',
    comment: 'Fixture authority denial',
    expectedRequestRevision: 3,
    idempotencyKey: 'fixture-approval-denied',
  };
}
