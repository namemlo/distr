# PR-080 — Operator Control Room UI

## Purpose

PR-080 adds a role-aware Angular control room for reviewing and operating the release-to-deployment lifecycle. It keeps immutable checksums, blocking facts, uncertainty, and retained evidence visible at the point where an operator makes a decision.

The primary routes are:

| Area                   | Routes                                                                                                                            |
| ---------------------- | --------------------------------------------------------------------------------------------------------------------------------- |
| Fleet and releases     | `/fleet`, `/releases`, `/releases/:releaseId`                                                                                     |
| Targets and plans      | `/deployments/targets`, `/deployments/targets/:deploymentTargetId`, `/deployments/plans`, `/deployments/plans/:planId`            |
| Runtime operations     | `/deployments/campaigns`, `/deployments/campaigns/:campaignId`, `/deployments/executions`, `/deployments/executions/:executionId` |
| Decisions and evidence | `/approvals`, `/reconciliation`, `/audit`                                                                                         |
| Adoption               | `/setup`                                                                                                                          |

`/deployments` redirects to `/deployments/targets`. Static deployment children take precedence over the legacy target-detail route. A legacy `/deployments/:deploymentTargetId` deep link redirects to `/deployments/targets/:deploymentTargetId` while preserving query parameters and the fragment.

## Operator roles

The UI derives vendor route access from the authenticated context. Scoped mutation authority remains server-enforced:
the local fixture rejects out-of-authority mutation requests with `403`, and the browser suite covers a negative scoped
mutation for every actor. The UI does not claim to reproduce the backend authorization engine client-side.

| Fixture actor                | Representative authority                                    | Covered operator behavior                                                                                                   |
| ---------------------------- | ----------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------- |
| Vendor administrator         | Registry, release, plan, campaign, and audit administration | Registry preview/classification/apply, readiness, release assembly and publication, fleet/shared-target review, plan review |
| Scoped approver              | Approval decisions scoped to the production environment     | Checksum-bound approval and server-reported invalidation                                                                    |
| Executor operator            | Campaign, execution, and reconciliation controls            | Versioned pause/resume, execution status query, drift resolution                                                            |
| Audit viewer                 | Read-only audit access                                      | Filter deep link, event detail, evidence correlation, deterministic bundle metadata                                         |
| Unauthorized customer reader | No vendor control-plane authority                           | Redirect away from the control-room route                                                                                   |

These browser actors are deterministic test claims. The mock API consumes those claims to enforce the documented local
allow/deny matrix, but that does not prove the authorization rules of a deployed backend.

## Route-mocked browser contract

`frontend/ui/e2e/control-plane.spec.ts` runs in Chromium against the real Angular development server. `frontend/ui/e2e/fixtures/control-plane.ts` intercepts the control-plane HTTP boundary and returns deterministic contract-shaped responses. Any non-local request is aborted, so the suite cannot contact adopter, customer, registry, executor, observer, or other external services.

The suite covers:

- setup readiness plus registry import preview, classification decision, coverage, apply, and readiness recheck;
- component and product release history, immutable comparison, assembly, validation, typed publication confirmation, and resulting release deep links;
- two deployment units sharing one physical target;
- all visible plan checksums and blocking facts;
- exact two-service dependency resolution in `included` and
  `pinned_existing` modes, including immutable provider identity and observed
  state evidence;
- scoped approval plus server-reported invalidation;
- guided campaign draft creation and update, validation, immutable publication,
  run creation, and allowed version-bound run transitions;
- campaign pause/resume with server-provided run versions;
- retained failed and retried execution attempts with unchanged plan,
  artifact, and config material plus an increased fence generation;
- execution previous-known-state evidence and a current-status request;
- drift detail, evidence, and reasoned resolution;
- audit query hydration, event detail, evidence deep link, export health, and evidence-bundle metadata;
- legacy route preservation, disabled feature behavior, unauthorized access, and loading, empty, structured error, partial, stale, and unknown states.

Significant contract evidence is attached to the corresponding Playwright result as JSON. Failures retain a trace, screenshot, and video.
The current suite contains 27 Playwright tests. AC-63 executes the purpose-built
`@evidence` reference-client DEV release, approval, and previous-state journey
through `playwright.control-plane-evidence.config.ts`. Each of its eleven visual
checkpoints centers the viewport on the exact asserted claim, and the evidence
configuration fixes retries at zero. The evidence packager rejects reused
screenshot bytes and retains `AC-63-checkpoints.json`, which binds every PNG to
its exact route, actor, entity IDs, domain checksums, filename, and immutable
SHA-256. The screenshots and JSON evidence remain local route-mocked browser
proof, not deployed-client evidence.

The same evidence configuration also selects a client-shaped two-service
journey with nine visual checkpoints. It proves an exact capability binding,
the `included` and `pinned_existing` plans, baseline and mixed-version states,
a controlled pre-mutation failure, a version-bound retry with retained attempt
history, and UI-created previous-state lineage. The previous-state detail is
unavailable until the recorded creation request succeeds.

## Running the proof

Install the Chromium runtime once when needed:

```powershell
pnpm exec playwright install chromium
```

List or execute the deterministic browser suite:

```powershell
pnpm run test:e2e:control-plane -- --list
pnpm run test:e2e:control-plane
```

The configuration deliberately uses one worker. Reports and artifacts are written to:

- `output/playwright/control-plane-html`
- `output/playwright/control-plane-junit.xml`
- `output/playwright/control-plane-artifacts`

The local Angular compile can be checked independently:

```powershell
pnpm exec ng build --configuration=development
```

## Proof boundary

### Confirmed locally

- The Angular application compiles in development mode.
- The real routed UI renders the role-specific operator journeys above.
- Browser requests, mutation payloads, and the approval/execution response contracts conform to the deterministic fixture contracts.
- Every fixture actor receives a deterministic `403` for a mutation outside its declared authority.
- Confirmation overlays, navigation, query hydration, error-state rendering, and significant evidence attachments behave as asserted.
- No external or adopter system is contacted by this suite.

### Not confirmed by this suite

- Authentication against a deployed identity provider.
- Backend enforcement of role, scope, separation-of-duty, enrollment, or feature-gate rules.
- Compatibility with a live backend schema or database migration.
- Real registry, CI, executor, observer, audit sink, object store, webhook, or client-host integration.
- Staging, production, load, failover, or cross-service end-to-end behavior.
- Downloadable audit evidence; the current API returns deterministic bundle metadata only.

Staging proof must therefore run separately with approved credentials and scoped test data. Its evidence should identify the environment, backend build, actor/scope, request IDs, immutable checksums, and cleanup outcome. A mocked green suite must not be presented as staging or production validation.

## Known contract gaps and follow-up proof

- Approval invalidation is displayed when returned by the server; the UI intentionally exposes no client-side invalidate action because no such backend mutation contract is available.
- Evidence pages are consumed as bounded snapshots. Pagination and retention behavior still require backend/staging verification.
- Campaign member controls depend on runtime identifiers and version metadata being returned by the backend; missing metadata disables those controls rather than guessing.
- Audit evidence bundles contain metadata and checksums, not a downloadable archive.
- Fixture claims and denials demonstrate only the local routed UI/API matrix. Backend authorization remains the source of truth and must reject out-of-scope requests independently.
- Retry, conflict, and degraded external-integration behavior requires service or staging fault-injection proof beyond this route-mocked suite.
- Setup readiness is a conservative client-side aggregate because no server readiness endpoint exists yet. It resolves the
  selected deployment unit to its target-config snapshot environment, then applies the backend's current organization plus
  environment enrollment rule using only the latest active revision at the decision time. A future server aggregate should
  replace this client composition so readiness and authorization cannot drift.
