# ADR-0058: Component Release Contract v2

## Status

Accepted

## Context

Release Contract v1 combines target configuration with build and component identity. That contract remains valid
historical evidence, but it cannot represent one reusable build across several targets or distinguish a requested
source ref from the commit actually built.

The control plane needs a strict, target-neutral component record with immutable artifact identity for every
supported platform. Existing v1 JSON bytes, canonical payloads, checksums, publication behavior, API routes, and
views must remain unchanged.

## Decision

Component releases use the exact discriminator `distr.component-release/v2`. The historical embedded v1
discriminator remains `distr.release-contract/v1`; additive row metadata classifies those rows as
`distr.release/v1` and `legacy` without rewriting their contract, canonical payload, or checksum.

A v2 contract contains:

- `componentKey` and a strict semantic `version`;
- source `repository`, required `requestedRef`, and the resolved lowercase 40-character `commit`;
- portable build `id` and `builder`;
- artifacts with stable keys, type-specific media types, a lowercase manifest/index digest, and lowercase
  per-platform digests for `linux/amd64` or `linux/arm64`; each artifact has exactly one outer bundle component
  with the corresponding component type;
- symbolic capability `provides` versions and `requires` semantic ranges, resolution stages, and allowed modes;
- ordered typed migration declarations with compatibility and failure policy;
- change summary and commit references; and
- provenance, SBOM, signature, and test evidence references.

The requested source ref is a fully qualified `refs/heads/<branch>` or `refs/tags/<tag>` value. Its repository,
branch or tag, and resolved commit are projected into the outer Release Bundle source fields before canonicalization.
Contradictory caller-supplied projections are rejected, and channel source policy is evaluated from the contract
projection rather than independently supplied outer metadata. Outer CI provider and run ID are likewise bound to
the contract builder and build ID; a separate CI run URL is forbidden because v2 has no authoritative field for it.

The parser dispatches only on an exact schema. V2 decoding rejects unknown fields. Canonicalization sorts only
set-like collections by stable identity, emits empty arrays for null, omitted, or empty collections, and rejects
duplicate stable identities. Once any release in an organization/component/version lineage is published, its
complete artifact identity (keys, types, media types, manifest digests, and complete platform/digest sets) is
immutable even after block or archive. An exact publish retry of the same already-published resource is idempotent.

Component releases contain no target, customer, environment, variable snapshot, target config snapshot, hostname,
concrete path, credential, or secret. Publishing a component release therefore does not create or require a
Variable Snapshot. Required capability ranges are explicit and non-empty. Product-stage requirements have no target
resolution modes; target-stage requirements declare at least one supported mode. New component writes require
`operator_control_plane_v2`; when the flag is off, an update is rejected if either its stored or incoming contract
is v2, while historical reads and v1 writes remain available.

Validation runs before normalization or database persistence. Every collection is bounded to 256 entries, with the
supported platform set bounded to two and target resolution modes bounded to five. Source and build fields are at
most 512 bytes, evidence references at most 2,048 bytes, change summaries and migration descriptions at most 4,096
bytes, and the normalized contract payload at most 512 KiB. Sensitive-text validation runs only after those bounds
are established and rejects credential-bearing URI userinfo, authorization/secret assignments, and PEM private-key
markers. Embedded Unix, Windows drive, and UNC path tokens are rejected wherever portable v2 text is accepted.
Evidence references are restricted to credential-free, digest-pinned OCI references or normalized HTTPS references
whose path binds a lowercase SHA-256 digest, including content-addressed paths with a filename after the digest.

The outer component projection is also bounded to 256 entries before canonicalization. Every persisted string field
is bounded and secret-checked. Package references must parse as canonical credential-free OCI repositories and are
bound to the contract artifact digest as one immutable reference. Application-version IDs, child-release IDs, and
checksums are rejected because they have no meaning in a v2 artifact projection.

The Release Bundle detail view labels the embedded contract schema and outer row storage classification separately.
For a historical release, `distr.release-contract/v1` remains the audit contract schema while `legacy` and
`distr.release/v1` describe its additive storage metadata.
The existing `/api/v1/release-bundles` route family remains the only release-bundle API.

Migration 143 adds `kind` and `release_contract_schema` to `ReleaseBundle`, and normalized artifact, evidence,
capability, and migration fact tables. The canonical contract payload and checksum remain the immutable audit
source.

## Consequences

One build can be promoted to differently configured targets without rebuilding. Source intent and actual build
identity are auditable, and multi-platform artifacts cannot drift beneath one component/version identity.

The normalized fact tables duplicate selected canonical facts for safe queries; writers must replace them from the
canonical contract in the same transaction. Product manifests and provenance policy are separate later slices.
Migration 143 intentionally follows missing speculative migrations 140 through 142 and must be rebased before
integration gates run.

## Alternatives Considered

- Extend v1 in place. Rejected because it would change historical payload meaning and checksums.
- Add a parallel component-release endpoint family. Rejected because schema and kind already discriminate the
  additive resource while the established release-bundle authorization and tenancy boundary can be preserved.
- Store only normalized rows. Rejected because normalized projections are not a sufficient immutable audit source.
- Keep mutable image tags. Rejected because tags cannot prove artifact identity or stable retry behavior.

## Validation

- Pure parser, canonicalization, validation, API DTO, mapping, and handler tests cover strict schemas, v1
  compatibility, source identity, target-neutral data, digest and platform rules, deterministic checksums, and the
  v2 write gate.
- PostgreSQL tests cover additive persistence, normalized facts, exact retry, conflicting digests, tenant isolation,
  and the absence of a Variable Snapshot. They remain deferred on this speculative branch.
- Production Angular, browser, containers, migration contiguity, full-repository Go, and PostgreSQL 16/18 gates run
  after PR-057 through PR-059 are integrated.
