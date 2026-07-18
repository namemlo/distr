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
- [ADR-0069: Checksum-Bound Deployment Admission and Emergency Overrides](0069-deployment-admission-emergency-overrides.md)
