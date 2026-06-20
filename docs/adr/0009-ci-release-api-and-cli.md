# ADR-0009: CI Release API and CLI

## Status

Accepted for PR-009.

## Context

PR-006 introduced draft Release Bundle CRUD, PR-007 introduced validation and publication, and PR-008 added an administration UI for those existing endpoints. The roadmap next requires CI-friendly release creation and a neutral command-line interface without adding deployment planning, lifecycle promotion, approval, retention, execution, or agent behavior.

CI systems need to submit the same release request safely after retries, network failures, or parallel jobs. They also need to attach non-secret build provenance and immutable artifact digests while continuing to use the public Release Bundle API and the existing server-side validation and publication rules.

## Decision

Extend `POST /api/v1/release-bundles` with optional `Idempotency-Key` support behind the existing `release_bundles` experimental feature flag.

When no idempotency key is provided, create behavior remains unchanged.

When a key is provided:

- the key is scoped to the authenticated organization;
- the raw key is never stored, logged, or returned;
- a hash of the key, a canonical request checksum, and the created Release Bundle ID are stored transactionally;
- the same organization, key, and canonical request returns the original Release Bundle without creating duplicate bundles, components, or audit events;
- the same organization and key with a different canonical request returns `409 Conflict` with a stable structured error code;
- different organizations may reuse the same key independently;
- concurrent same-key requests serialize through the database transaction path and create at most one Release Bundle;
- draft bundles referenced by an idempotency key cannot be deleted, preserving stable retry responses.

Add generic CI source metadata to Release Bundles:

- source repository;
- source revision;
- source branch;
- source tag;
- CI provider;
- CI run ID;
- CI run URL.

Only non-secret metadata is accepted. Immutable artifact references continue to use the existing component model. OCI image and OCI artifact digest fields must be provider-neutral immutable digests in `sha256:<64 hex characters>` form. Mutable tags are not accepted as digests.

Add release CLI commands to the existing community-neutral `distr` command:

- `distr release create`;
- `distr release validate`;
- `distr release publish`.

The CLI uses the public API only. It supports `--server`, `--token`, `--output json|text`, `DISTR_SERVER_URL`, and `DISTR_API_TOKEN`. Create accepts `--file`, `--file -`, and `--idempotency-key`. The CLI must not print access tokens, authorization headers, or other secret material.

## Consequences

- CI systems can create draft Release Bundles idempotently and then explicitly validate and publish them.
- Existing clients that omit `Idempotency-Key` retain current behavior.
- The server remains the source of truth for authorization, organization isolation, validation, publication, and immutability.
- The database gains an additive idempotency table and source metadata columns with reversible migrations.
- PR-009 does not add lifecycle eligibility, promotion, deployment process snapshots, deployment planning, approvals, retention, execution, provider-specific registry integrations, or agent changes.
