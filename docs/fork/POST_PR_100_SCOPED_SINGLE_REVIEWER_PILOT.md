# Post-PR-100: Scoped Single-Reviewer Pilot Exception

## Purpose

Permit one explicitly configured adopter pilot to proceed without fabricating a second identity, while keeping enterprise four-eyes behavior as the default.

## Generic user story

As a platform owner running a bounded proof of concept, I need one already-authorized operator to complete approval and protected-history retention when no second account is enrolled, and I need every resulting record to state that a governance exception was used.

## Scope and behavior

- Feature flag: default-off, explicit-only `scoped_single_reviewer_pilot`; `all` does not enable it.
- Required configuration: exact organization, environment, deployment target, and owner-approval reference.
- Deployment approval: same requester/approver is accepted only for an `APPROVE` decision on a one-target plan matching the configured environment and target; group authorization still applies.
- Protected history: same issuer/reviewer is accepted only for the exact configured target with no customer-wide scope and an active target assignment matching the configured environment at capture time. Distr derives and transactionally revalidates this evidence.
- Evidence: append-only records and audit payloads expose the exception key and approval reference; protected-history checksums bind both.

## Data, API, and compatibility

Migration 171 adds nullable governance-exception fields to `ApprovalDecision` and `ProtectedHistoryArtifact`, updates protected-history checksum and audit constraints, supports schema 171 exports, and refuses downgrade while exception evidence exists. A refused guarded downgrade verifies the schema-171 shape and restores clean migration metadata at version 171. API responses expose the two optional fields. Existing rows and standard checksums remain unchanged.

There are no UI or agent-protocol changes. No adopter name, service, registry, CI system, database engine, or runtime address exists in core behavior. A separate adopter example documents concrete configuration.

## Verification

Focused Go tests cover configuration, governance, protected history, API/mapping, handlers, database code, and migration structure. Full repository verification remains part of integration certification.
