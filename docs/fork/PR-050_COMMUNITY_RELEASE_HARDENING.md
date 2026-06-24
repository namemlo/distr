# PR-050 - Community Release Hardening

## Summary

PR-050 closes the roadmap with a community-neutral release-readiness package. It documents security gates,
operator procedures, upgrade limits, feature flags, API/CLI surfaces, upstream contribution slices, and a
repeatable API-only live demo and neutral fixture verifier.

## Generic User Story

As a community operator or upstream reviewer, I want a complete release package that explains how to run,
secure, validate, upgrade, and contribute the fork so that I can evaluate it without relying on adopter-specific
knowledge or commercial credentials.

## Included

- ADR-0050 for the release-hardening approach.
- Security checklist mapped to existing code and regression-test surfaces.
- Release readiness, compatibility, feature-flag, and known-limitation documentation.
- Operator smoke-test and upgrade checklists.
- API and CLI index for the community release.
- Upstream contribution breakdown with dependency order.
- Neutral `examples/community-e2e/` live demo, fixture, verifier, isolated Compose dependencies, and Docker wrapper.
- API-only live release-to-task demo for publish, task creation, agent lease, safe HTTP action execution, step events/logs, and operator timeline reads.
- `hack/pr050-license-scan.mjs` for installed Node package and Go module license validation.
- `hack/pr050-validate-release-hardening.mjs` for deterministic docs, demo, link, workflow, and secret-safety checks.
- CI workflow for the PR-050 validation subset.

## Out of Scope

- New product domains or roadmap features.
- New action adapters.
- New deployment, approval, rollout, runbook, Config as Code sync, or observability capabilities.
- Provider-specific integrations or adopter-specific workflows.
- Breaking API, schema, or agent-protocol changes.
- Major UI redesign.
- Speculative refactoring or performance work.

## Required Impact Report

### Database/schema impact

None. PR-050 adds no migrations and no persistent columns.

### Public API impact

None. PR-050 adds no endpoints and changes no request or response payloads.

### Frontend/UI impact

None. PR-050 adds no UI routes, components, labels, or navigation.

### Agent/protocol impact

None. PR-050 changes no agent request, lease, event, or capability protocol.

### Feature-flag impact

None. Existing experimental flags remain documented and unchanged.

### Security impact

Positive documentation and validation impact. The PR-050 security review records the release gates and maps
them to existing regression tests for organization isolation, RBAC, leases, redaction, path safety, webhook
hardening, Config as Code validation, and compatibility metadata.

### Backward-compatibility impact

Existing simple deployments, advanced roadmap APIs, and supported agent behavior are unchanged.

### Upgrade/downgrade impact

No new migration is added. Upgrade and downgrade behavior is documented in the operator checklist and
compatibility notes.

## Security Review Summary

The scoped PR-050 review inspected existing test and documentation surfaces for:

- organization isolation and RBAC boundaries;
- agent authentication, target matching, leases, and replay-sensitive flows;
- secret redaction in action inputs, agent output, logs, compatibility metadata, and demo fixtures;
- path traversal and symlink protections for file-render and OCI job actions;
- webhook signing, replay, and network hardening;
- Config as Code strict validation and authority mutation guards;
- feature-flag boundaries for experimental roadmap features.

No PR-050 runtime defect requiring a schema, API, UI, or agent change was identified. If a later review finds a
concrete vulnerability, it should be fixed in a focused security PR with a negative regression test.

## Validation

Primary deterministic validation:

```shell
DISTR_DEMO_DISPOSABLE_HUB=true node examples/community-e2e/live-demo.mjs --require-running-hub
node examples/community-e2e/run-demo.mjs
pnpm install --frozen-lockfile
node hack/pr050-license-scan.mjs
node hack/pr050-validate-release-hardening.mjs
```

Recommended release validation before tagging:

```shell
go test -p=1 ./...
go vet ./...
golangci-lint run ./...
pnpm run lint
pnpm run test
pnpm run build:community
mise run build:hub:community
mise run build:agent:docker
mise run build:agent:kubernetes
hack/validate-migrations.sh
git diff --check
```

The live community demo uses public Hub and agent APIs only after the Hub is running. It generates per-run credentials, soft-deletes the demo organization, and verifies the organization is no longer accessible; running-Hub mode requires disposable infrastructure through `DISTR_DEMO_DISPOSABLE_HUB=true` unless a shared Hub is explicitly acknowledged with `DISTR_DEMO_ALLOW_SHARED_HUB=true`. `--start-local` ignores ambient `DISTR_HOST` and `DATABASE_URL`; use `DISTR_DEMO_HOST` or `DISTR_DEMO_DATABASE_URL` for demo-specific overrides. `DISTR_TEST_DATABASE_URL` is scoped to Go integration tests and is not required by `examples/community-e2e/live-demo.mjs`.

## Manual Verification

- Run the community demo verifier from a clean checkout.
- Follow `docs/operations/operator-smoke-test.md` against a local Hub.
- Review `docs/security/release-hardening-checklist.md` and record accepted findings.
- Review `docs/upstream/contribution-breakdown.md` before proposing upstream PRs.

## Known Limitations

- The deterministic fixture verifier is credential-free and supplements, but does not replace, the live Hub smoke test and API-only live release-to-task journey.
- Security and dependency scans depend on the scanner installed by the operator or CI environment.
- Feature flags remain experimental until a later stabilization release removes them with a documented
  migration path.
