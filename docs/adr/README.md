# Architecture Decision Records

Use this directory for architecture decision records when a change introduces or changes a public API, persistent data model, agent protocol, security boundary, or long-lived abstraction.

## Naming

Use a monotonic number and short kebab-case title:

```text
0001-release-bundle-immutability.md
0002-environment-lifecycle-model.md
```

## Template

```markdown
# ADR-0000: Title

## Status

Proposed | Accepted | Superseded

## Context

Describe the problem, constraints, existing behavior, and compatibility requirements.

## Decision

Describe the decision in concrete terms. Include API, data model, agent protocol, UI, and security implications when relevant.

## Consequences

Describe benefits, trade-offs, operational impact, migration needs, and follow-up work.

## Alternatives Considered

Describe other viable options and why they were not selected.

## Validation

List tests, migration checks, manual verification, and rollout or rollback notes.
```

## Current Control-Plane Program

- [ADR-0056: Canonical Deployment Registry Identity](0056-canonical-deployment-registry-identity.md)
- [ADR-0057: Immutable Target Config Snapshots](0057-immutable-target-config-snapshots.md)
- [ADR-0058: Component Release Contract v2](0058-component-release-contract-v2.md)
- [ADR-0059: Product Release Capability Graph](0059-product-release-capability-graph.md)
- [ADR-0060: Immutable Target Deployment Plan v2](0060-target-deployment-plan-v2.md)
- [ADR-0061: Scoped Authorization and Enrollment](0061-scoped-authorization-and-enrollment.md)
- [ADR-0062: Versioned Calendar Admission](0062-versioned-calendar-admission.md)
- [ADR-0063: Deterministic Deployment Campaigns](0063-deterministic-deployment-campaigns.md)
- [ADR-0064: Fenced Executor Protocol v2](0064-fenced-executor-protocol-v2.md)
- [ADR-0065: Independent Observed State](0065-independent-observed-state.md)
- [ADR-0066: Correlated Control-Plane Audit and External Export](0066-control-plane-audit-export.md)
- [ADR-0067: Operator Read Models and Route Compatibility](0067-operator-read-models-and-route-compatibility.md)
- [ADR-0069: Checksum-Bound Deployment Admission and Emergency Overrides](0069-deployment-admission-emergency-overrides.md)
- [ADR-0070: Enable Validated Target Deployment Plan Execution](0070-validated-target-plan-execution.md)
- [ADR-0071: Review-Material Admission Decisions](0071-review-material-admission-decisions.md)
- [ADR-0072: Native Baseline Adoption](0072-native-baseline-adoption.md)
- [ADR-0073: New-Volume Restore and Protected-History Continuity](0073-new-volume-restore-and-protected-history-continuity.md)
- [ADR-0074: Executor Runtime Trust Contract](0074-executor-runtime-trust-contract.md)
- [ADR-0075: Fresh Dependency Provider Evidence](0075-dependency-provider-evidence.md)
- [ADR-0076: Native Structured Migration Contract Wiring](0076-native-structured-migration-contract-wiring.md)
- [ADR-0077: Baseline Adoption Fact Separation](0077-baseline-adoption-fact-separation.md)
- [ADR-0078: Independent Runtime Measurement Probes](0078-independent-runtime-measurement-probes.md)
- [ADR-0079: Durable Independent Observer Service](0079-durable-independent-observer-service.md)
- [ADR-0081: Lock and Lease Lifecycle Read Model](0081-lock-lease-lifecycle-read-model.md)
- [ADR-0082: Protected-History Artifact Retention](0082-protected-history-artifact-retention.md)
- [ADR-0083: Complete Product Release Manifest Read Model](0083-product-release-manifest-read-model.md)
- [ADR-0084: Native Schema Evidence Gating](0084-native-schema-evidence-gating.md)
- [ADR-0085: Deployment Plan Source-History Validation](0085-deployment-plan-source-history-validation.md)
- [ADR-0087: Scoped Single-Reviewer Pilot Exception](0087-scoped-single-reviewer-pilot-exception.md)
- [ADR-0088: Governed Baseline Adoption](0088-governed-baseline-adoption.md)
