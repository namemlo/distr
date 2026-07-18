# PR-060 - Component Release Contract v2

## Generic User Story

As a release operator, I want CI to publish one immutable, target-neutral component release with exact
multi-platform artifacts and evidence so that the same verified build can be planned for different targets without
mixing in target configuration or secrets.

## Scope

- Add strict Component Release Contract v2 parsing, canonicalization, and validation.
- Preserve Release Contract v1 payloads, checksums, publication behavior, route family, and UI.
- Add release kind/schema metadata plus normalized artifact, evidence, capability, and migration facts.
- Gate only new component writes with `operator_control_plane_v2`.
- Show additive component-release summaries in the existing Release Bundles detail view.

## Contract Summary

| Concern           | Contract                                                                                 |
| ----------------- | ---------------------------------------------------------------------------------------- |
| Discriminator     | `distr.component-release/v2`                                                             |
| Source            | Required canonical repository, requested ref, and resolved lowercase 40-character commit |
| Artifact identity | Lowercase `sha256:<64 hex>` manifest/index and per-platform digests                      |
| Platforms         | `linux/amd64`, `linux/arm64`                                                             |
| Capabilities      | Strict provided versions and required ranges with explicit resolution                    |
| Migrations        | Ordered symbolic type, compatibility, failure policy, and description                    |
| Evidence          | Provenance, SBOM, signature, and test references with no credentials                     |
| Target data       | Forbidden; no target, customer, environment, path, URL, snapshot, or secret              |
| Retry             | Exact re-publish of the same published component resource is idempotent                  |
| Conflict          | Same organization/component/version/platform with another digest returns stable conflict |

## Required Impact Report

### Database/schema impact

Migration 143 adds `ReleaseBundle.kind` (`legacy`, `component`, `product`) and `release_contract_schema`. Existing
rows receive `legacy` and `distr.release/v1` defaults only; neither `release_contract`, `canonical_payload`, nor
`canonical_checksum` is rewritten.

`ComponentReleaseArtifact`, `ComponentReleaseEvidence`, `ComponentReleaseCapability`, and
`ComponentReleaseMigrationDeclaration` store normalized query facts linked through organization-consistent foreign
keys. The canonical JSON contract remains authoritative. Downgrade refuses while component/product facts exist.

### Public API impact

The existing `/api/v1/release-bundles` create, validate, publish, list, and get flow accepts v1 and v2 contracts.
Responses add `kind` and `releaseContractSchema`; `releaseContract` remains the discriminated payload. Cross-tenant
lookups retain 404 behavior and expected errors do not expose database details.

### Frontend/UI impact

The existing detail panel retains the complete v1 view. V2 details add schema/kind, requested and resolved source,
artifact manifest and platform digests, capability requirements, migrations, evidence counts, and changes.

### Agent/protocol impact

None. Component Release v2 is a control-plane publication record and does not change agent or external executor v1
messages.

### Feature-flag impact

Creating, updating, or publishing a v2 component release requires `operator_control_plane_v2`. V1 writes and all
historical release reads retain their existing `release_bundles` gate and behavior.

### Security impact

Strict unknown-field decoding and target-neutral validation reject undeclared target fields, credentials,
secret-looking values, mutable digests, unsafe paths, duplicate platform identities, and unsupported media types.
Every normalized fact carries the owning organization boundary.

### Backward-compatibility impact

Historical v1 contract JSON, canonical bytes, and checksums are unchanged. Legacy publication still creates its
Variable Snapshot and retains non-idempotent state transitions. Component publication does not require a target or
Variable Snapshot. Migration 143 remains reserved while 140 through 142 are absent from this speculative branch.

## Validation

Fast RED/GREEN checks cover parser, canonicalization, validation, API DTOs, mapping, handlers, and compilation.
PostgreSQL 16/18, integration handlers, production Angular, browser, container, full Go, and migration-contiguity
gates are written or documented and deferred until the branch is rebased onto PR-059.
