# ADR-0087: Scoped Single-Reviewer Pilot Exception

## Status

Accepted.

## Context

Enterprise deployment and protected-history workflows require distinct authenticated people by default. A time-bounded adopter pilot may temporarily have only one enrolled operator, but inventing a reviewer identity or silently weakening the existing policy would make the resulting evidence misleading.

## Decision

Add a generic, default-off `scoped_single_reviewer_pilot` feature. Enabling it requires exact organization, environment, and deployment-target UUIDs plus a non-secret owner-approval reference. Startup fails closed if any value is missing or malformed.

The exception applies only to an `APPROVE` decision where the authenticated actor is the requester, the frozen deployment plan contains exactly the configured target in the configured environment, and all normal role/group authorization succeeds. Protected-history retention permits issuer and reviewer to be the same authenticated account only for an export scoped exclusively to that exact target.

Append-only approval decisions and protected-history artifacts store `scoped-single-reviewer-pilot` and the owner-approval reference. Protected-history request and retention checksums and the correlated audit event bind those fields. No second identity is created or inferred.

## Consequences

Four-eyes behavior remains unchanged when the flag is absent, disabled, or outside the exact configured scope; malformed enabled configuration prevents startup. The exception cannot authorize customer-wide exports, multi-target plans, rejection, sample retirement, or an actor lacking the normal approval authority. Existing exception evidence remains visible after disabling the flag.

Migration 171 adds nullable exception evidence to the two append-only tables and refuses downgrade while such evidence exists. Operators must disable the flag after the pilot and enroll a real second reviewer before production use.

## Alternatives Considered

Fabricating a service reviewer was rejected because it misrepresents human review. Removing separation constraints from the deployment policy was rejected because it is broad and difficult to distinguish in historical evidence. Encoding the waiver only in comments was rejected because comments are not a typed, checksum-bound control.

## Validation

Unit tests cover default-off parsing, exact scope matching, protected-history checksums, requester self-approval denial by default, and labelled exception acceptance. Migration checks cover typed evidence, audit binding, and downgrade refusal. No live target or client database is contacted.
