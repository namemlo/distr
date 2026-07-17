# Control Plane Foundations Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement PR-055 through PR-065: feature isolation, canonical deployment registry, immutable target configuration, Component Release v2, Product Release DAG, exact target planning/change logs, and typed database migration plans.

**Architecture:** Build additive vertical slices using the repository's existing API → handler → mapping → DB → types pattern. Reuse current Distr identities and extend `ReleaseBundle` and `DeploymentPlan`; retain all v1 bytes/checksums and introduce explicit schema discriminators and lineage instead of rewriting history.

**Tech Stack:** Go, PostgreSQL, chi/oaswrap, Angular 22, TypeScript/Vitest, Sigstore Go, pnpm, mise.

## Global Constraints

- Read `docs/superpowers/plans/2026-07-14-enterprise-operator-control-plane-program.md` and the approved spec before each PR.
- Complete, review, and merge only one PR task below at a time.
- Migration numbers 139–147 are reserved after PR-054A migration 138; re-check before opening each PR.
- All create/update requests trim and validate at API boundaries and validate organization ownership again in DB/service code.
- Canonicalization is deterministic across map iteration and database row order; array order is either semantic and preserved or explicitly sorted.
- New v2 writes require `operator_control_plane_v2`; historical v1 reads do not.
- Backfills are `--dry-run` by default, cursor/checkpointed, restartable, countable, and leave blocked records unchanged.
- No secret value, private key, client path, hostname, or adopter name may enter release/config JSON or logs.
- Every PR includes focused tests, live PostgreSQL tests, full regression/build verification, fork docs, and a single focused commit.

---

## Task 1: PR-055 — Establish the v2 Isolation Boundary

**Files:**

- Modify: `internal/featureflags/featureflags.go`
- Modify: `internal/featureflags/featureflags_test.go`
- Modify: `frontend/ui/src/app/types/feature-flags.ts`
- Modify: `frontend/ui/src/app/services/feature-flag.service.spec.ts`
- Modify: `docs/fork/FORK_DIFF_INDEX.md`
- Create: `docs/fork/PR-055_OPERATOR_CONTROL_PLANE_FLAGS.md`
- Modify: `docs/observability/feature-flags.md`
- Modify: `docs/fork/UPGRADE_GUIDE.md`

**Interfaces:**

```go
const (
    KeyOperatorControlPlaneV2 Key = "operator_control_plane_v2"
    KeyExecutorProtocolV2     Key = "executor_protocol_v2"
)
```

`executor_protocol_v2` is registered independently but may be effective only when `operator_control_plane_v2` is also effective. Both are process-wide kill switches until PR-066 adds organization/environment enrollment. Neither flag may be enabled in a shared or production environment before PR-083.

- [ ] Add parsing/registry tests proving both keys are accepted, appear in deterministic registry order, and unknown keys still fail.

```powershell
go test ./internal/featureflags -run 'Test(ParseEnabledKeys|RegistryFlags).*ControlPlane' -count=1
```

Expected before implementation: FAIL because the constants/definitions do not exist. Expected after implementation: PASS.

- [ ] Add backend definitions and TypeScript union members; add frontend service fixture assertions for both labels and states.
- [ ] Reconcile the stale `Current Status` paragraph in `FORK_DIFF_INDEX.md` so it recognizes implemented PR-054, then append PR-055.
- [ ] Document layered kill switches, production-off behavior, and unchanged v1 operation.
- [ ] Verify and commit.

```powershell
go test ./internal/featureflags -count=1
pnpm exec ng test --watch=false --include frontend/ui/src/app/services/feature-flag.service.spec.ts
mise run test:go
git diff --check
git add internal/featureflags frontend/ui/src/app/types/feature-flags.ts frontend/ui/src/app/services/feature-flag.service.spec.ts docs
git commit -m "feat: isolate operator control plane v2"
```

## Task 2: PR-056 — Canonical Deployment Registry Identity

**Files:**

- Create: `internal/migrations/sql/139_deployment_registry.up.sql`
- Create: `internal/migrations/sql/139_deployment_registry.down.sql`
- Create: `internal/types/deployment_registry.go`
- Create: `internal/deploymentregistry/validation.go`
- Create: `internal/deploymentregistry/validation_test.go`
- Create: `internal/db/deployment_registry.go`
- Create: `internal/db/deployment_registry_test.go`
- Create: `api/deployment_registry.go`
- Create: `api/deployment_registry_test.go`
- Create: `internal/mapping/deployment_registry.go`
- Create: `internal/mapping/deployment_registry_test.go`
- Create: `internal/handlers/deployment_registry.go`
- Create: `internal/handlers/deployment_registry_test.go`
- Create: `internal/handlers/deployment_registry_integration_test.go`
- Modify: `internal/routing/routing.go`
- Create: `docs/adr/0056-canonical-deployment-registry-identity.md`
- Create: `docs/fork/PR-056_CANONICAL_DEPLOYMENT_REGISTRY.md`
- Modify: `docs/adr/README.md`
- Modify: `docs/fork/FORK_DIFF_INDEX.md`

Migration 139 creates organization-owned `DeploymentScope`, `TargetEnvironmentAssignment`, `DeploymentUnit`, `DeploymentUnitSubscriber`, `ComponentDefinition`, `ComponentAlias`, and `ComponentInstance`. It reuses `CustomerOrganization`, `DeploymentTarget`, and `Environment` foreign keys. Constraints enforce one physical unit per target/scope identity, explicit active environment intervals, unique subscriber/unit pairs, canonical component keys, alias uniqueness, active instance uniqueness, organization-consistent relations, and deterministic pagination indexes.

```go
type DeliveryModel string // dedicated, shared, external, observe_only
type RegistryManagementState string // managed, external, observe_only, unclassified, retired

type DeploymentRegistryPlacement struct {
    Scope       DeploymentScope
    Assignment  TargetEnvironmentAssignment
    Unit         DeploymentUnit
    Subscribers []DeploymentUnitSubscriber
    Instances   []ComponentInstance
}

func ValidateDeploymentRegistryPlacement(DeploymentRegistryPlacement) []ValidationIssue
func CreateDeploymentScope(context.Context, *types.DeploymentScope) error
func CreateTargetEnvironmentAssignment(context.Context, *types.TargetEnvironmentAssignment) error
func CreateDeploymentUnit(context.Context, *types.DeploymentUnit) error
func CreateDeploymentUnitSubscriber(context.Context, *types.DeploymentUnitSubscriber) error
func CreateComponentDefinition(context.Context, *types.ComponentDefinition) error
func CreateComponentAlias(context.Context, *types.ComponentAlias) error
func CreateComponentInstance(context.Context, *types.ComponentInstance) error
func GetDeploymentRegistryPlacement(context.Context, uuid.UUID, uuid.UUID) (*types.DeploymentRegistryPlacement, error)
func ListDeploymentRegistryPlacements(context.Context, types.RegistryListFilter) (types.Page[types.DeploymentRegistryPlacement], error)
```

API root: `/api/v1/deployment-registry`. Add scoped CRUD/list endpoints for scopes, assignments, units, subscribers, definitions, aliases, instances, and placements. Lists accept `cursor` and `limit` (default 50, maximum 100).

- [ ] Write validation tests for dedicated/shared topology, ambiguous active environment, duplicate physical identity, missing subscriber, orphan instance, alias-required rename, and cross-organization substitution; run them red, then implement pure types/validation green.
- [ ] Write migration/repository tests that apply migration 139, create a complete placement, reject foreign IDs, prove pagination order, and allow down migration only when no registry data exists.
- [ ] Implement schema/repositories in transactions and translate uniqueness/FK failures into non-leaking domain errors.
- [ ] Add API validation, mapping, handlers, feature middleware, and routing. Test admin success, read-only list, unauthorized mutation, foreign ID 404, flag disabled, and bounded pagination.
- [ ] Verify, document, and commit.

```powershell
go test ./internal/deploymentregistry ./api ./internal/mapping ./internal/handlers -run 'Registry|DeploymentScope|ComponentInstance' -count=1
if ([string]::IsNullOrWhiteSpace($env:DISTR_CONTROL_PLANE_TEST_DATABASE_URL)) { throw 'DISTR_CONTROL_PLANE_TEST_DATABASE_URL is required' }
$previousTestDatabaseUrl=$env:DISTR_TEST_DATABASE_URL
try {
  $env:DISTR_TEST_DATABASE_URL=$env:DISTR_CONTROL_PLANE_TEST_DATABASE_URL
  go test ./internal/db -run 'DeploymentRegistry|Migration139' -count=1
  if ($LASTEXITCODE -ne 0) { throw 'live PostgreSQL registry tests failed' }
} finally {
  if ([string]::IsNullOrEmpty($previousTestDatabaseUrl)) { Remove-Item Env:DISTR_TEST_DATABASE_URL -ErrorAction SilentlyContinue } else { $env:DISTR_TEST_DATABASE_URL=$previousTestDatabaseUrl }
}
mise run lint:migrations
git add internal/migrations/sql/139_* internal/types/deployment_registry.go internal/deploymentregistry internal/db/deployment_registry* api/deployment_registry* internal/mapping/deployment_registry* internal/handlers/deployment_registry* internal/routing/routing.go docs
git commit -m "feat: add canonical deployment registry"
```

## Task 3: PR-057 — Registry Import, Classification, and Setup UI

**Files:**

- Create: `internal/migrations/sql/140_deployment_registry_imports.up.sql`
- Create: `internal/migrations/sql/140_deployment_registry_imports.down.sql`
- Modify: `internal/types/deployment_registry.go`
- Create: `internal/deploymentregistry/import.go`
- Create: `internal/deploymentregistry/import_test.go`
- Modify: `internal/db/deployment_registry.go`
- Modify: `internal/db/deployment_registry_test.go`
- Modify: `api/deployment_registry.go`
- Modify: `internal/mapping/deployment_registry.go`
- Modify: `internal/handlers/deployment_registry.go`
- Create: `frontend/ui/src/app/types/deployment-registry.ts`
- Create: `frontend/ui/src/app/services/deployment-registry.service.ts`
- Create: `frontend/ui/src/app/services/deployment-registry.service.spec.ts`
- Create: `frontend/ui/src/app/setup/registry/deployment-registry.component.ts`
- Create: `frontend/ui/src/app/setup/registry/deployment-registry.component.html`
- Create: `frontend/ui/src/app/setup/registry/deployment-registry.component.spec.ts`
- Modify: `frontend/ui/src/app/app-logged-in.routes.ts`
- Modify: `frontend/ui/src/app/components/side-bar/side-bar.component.ts`
- Modify: `frontend/ui/src/app/components/side-bar/side-bar.component.html`
- Create: `docs/fork/PR-057_DEPLOYMENT_REGISTRY_IMPORT.md`
- Modify: `docs/fork/FORK_DIFF_INDEX.md`

Migration 140 creates `RegistryImport`, `RegistryImportRoot`, `RegistryImportPlacement`, and `RegistryImportDecision`. Store source kind, tool/version, source commit, canonical parameters, raw report object reference/checksum, preview checksum, counts, state, creator, and checkpoints; reject raw secret-bearing content.

```go
type ImportMode string // preview, apply
type ImportClassification string // standard, shared, external, observe_only, ignored, needs_decision
type RegistryImportDiff struct { Creates, Updates, Retirements, Conflicts []RegistryImportChange }

func PreviewImport(context.Context, types.RegistryImportRequest) (*types.RegistryImportPreview, error)
func ApplyImport(context.Context, uuid.UUID, string) (*types.RegistryImportResult, error)
func ClassifyImportRoot(context.Context, types.RegistryImportDecision) error
func CoverageReport(context.Context, uuid.UUID) (*types.RegistryCoverageReport, error)
```

Routes: `POST /deployment-registry/imports/preview`, `POST /deployment-registry/imports/{id}/decisions`, `POST /deployment-registry/imports/{id}/apply`, `GET /deployment-registry/imports/{id}`, and `GET /deployment-registry/coverage`.

- [ ] Test deterministic no-op re-import, unaliased rename conflict, classification-required block, preview checksum, apply idempotency, interrupted restart, and no silent omission.
- [ ] Implement pure diff/import and deterministic checkpointed batches.
- [ ] Add repository/API tests for optimistic preview checksum, actor/audit data, cross-org rejection, bounded diagnostics, and exact counts.
- [ ] Build `/setup/registry` with preview, classification editor, confirmation, coverage, and loading/empty/error/partial/stale states.
- [ ] Test neutral fixtures representing 26 standard roots, 19 classifications, and 28 services without adopter names.
- [ ] Verify and commit.

```powershell
go test ./internal/deploymentregistry ./internal/db ./api ./internal/mapping ./internal/handlers -run 'RegistryImport|Coverage' -count=1
pnpm exec ng test --watch=false --include frontend/ui/src/app/setup/registry/deployment-registry.component.spec.ts --include frontend/ui/src/app/services/deployment-registry.service.spec.ts
pnpm run build:community
mise run lint:migrations
git add internal/migrations/sql/140_* internal/types/deployment_registry.go internal/deploymentregistry internal/db/deployment_registry* api/deployment_registry* internal/mapping/deployment_registry* internal/handlers/deployment_registry* frontend/ui/src/app docs
git commit -m "feat: import and classify deployment registry"
```

## Task 4: PR-058 — Immutable Target Config Snapshots

**Files:**

- Create: `internal/migrations/sql/141_target_config_snapshots.up.sql`
- Create: `internal/migrations/sql/141_target_config_snapshots.down.sql`
- Create: `internal/types/target_config_snapshot.go`
- Create: `internal/targetconfig/canonical.go`
- Create: `internal/targetconfig/canonical_test.go`
- Create: `internal/targetconfig/validation.go`
- Create: `internal/targetconfig/validation_test.go`
- Create: `internal/db/target_config_snapshots.go`
- Create: `internal/db/target_config_snapshots_test.go`
- Create: `api/target_config_snapshot.go`
- Create: `api/target_config_snapshot_test.go`
- Create: `internal/mapping/target_config_snapshot.go`
- Create: `internal/mapping/target_config_snapshot_test.go`
- Create: `internal/handlers/target_config_snapshots.go`
- Create: `internal/handlers/target_config_snapshots_test.go`
- Modify: `internal/routing/routing.go`
- Create: `frontend/ui/src/app/types/target-config-snapshot.ts`
- Create: `frontend/ui/src/app/services/target-config-snapshots.service.ts`
- Create: `frontend/ui/src/app/setup/config-snapshots/target-config-snapshots.component.ts`
- Create: `frontend/ui/src/app/setup/config-snapshots/target-config-snapshots.component.html`
- Create: `frontend/ui/src/app/setup/config-snapshots/target-config-snapshots.component.spec.ts`
- Create: `docs/adr/0057-immutable-target-config-snapshots.md`
- Create: `docs/fork/PR-058_TARGET_CONFIG_SNAPSHOTS.md`

Migration 141 creates `TargetConfigSnapshot`, `TargetConfigSnapshotObject`, `TargetConfigSnapshotComponent`, `TargetConfigSnapshotSecretReference`, and `TargetConfigSnapshotFeatureFlag`. The immutable parent is scoped to one organization, deployment unit, assignment/environment, source commit, and adapter version. Object bodies remain in immutable object storage; the DB stores media type, size, reference, and digest. Secrets retain only provider/reference/version fingerprint.

```go
func Canonicalize(types.TargetConfigSnapshotDraft) ([]byte, string, error)
func ValidateDraft(types.TargetConfigSnapshotDraft) []types.ValidationIssue
func VerifyObjects(context.Context, types.TargetConfigSnapshot) (*types.ObjectVerificationResult, error)
func CreateTargetConfigSnapshot(context.Context, *types.TargetConfigSnapshotDraft) (*types.TargetConfigSnapshot, error)
func GetTargetConfigSnapshot(context.Context, uuid.UUID, uuid.UUID) (*types.TargetConfigSnapshot, error)
func ListTargetConfigSnapshots(context.Context, types.TargetConfigListFilter) (types.Page[types.TargetConfigSnapshot], error)
```

API root: `/api/v1/target-config-snapshots`; operations are create, list, get, verify. Immutable rows have no update/delete route.

- [ ] Add stable-checksum and material-change canonical tests.
- [ ] Add validation tests for secret-looking values, mutable references, oversized diagnostics, missing placement, and cross-scope IDs.
- [ ] Implement validation/canonicalization, schema/repository/API, and object tamper verification.
- [ ] Build setup UI displaying only non-secret metadata/fingerprints.
- [ ] Verify and commit.

```powershell
go test ./internal/targetconfig ./internal/db ./api ./internal/mapping ./internal/handlers -run 'TargetConfig|ConfigSnapshot' -count=1
pnpm exec ng test --watch=false --include frontend/ui/src/app/setup/config-snapshots/target-config-snapshots.component.spec.ts
mise run lint:migrations
git add internal/migrations/sql/141_* internal/types/target_config_snapshot.go internal/targetconfig internal/db/target_config_snapshots* api/target_config_snapshot* internal/mapping/target_config_snapshot* internal/handlers/target_config_snapshots* internal/routing/routing.go frontend/ui/src/app docs
git commit -m "feat: add immutable target config snapshots"
```

## Task 5: PR-059 — Extract v1 Configuration Without Rewriting History

**Files:**

- Create: `internal/migrations/sql/142_release_contract_v1_extraction.up.sql`
- Create: `internal/migrations/sql/142_release_contract_v1_extraction.down.sql`
- Modify: `internal/types/target_config_snapshot.go`
- Create: `internal/targetconfig/v1_extraction.go`
- Create: `internal/targetconfig/v1_extraction_test.go`
- Modify: `internal/db/target_config_snapshots.go`
- Modify: `internal/db/target_config_snapshots_test.go`
- Create: `cmd/hub/cmd/backfill_target_config_snapshots.go`
- Create: `cmd/hub/cmd/backfill_target_config_snapshots_test.go`
- Create: `docs/fork/PR-059_V1_CONFIG_EXTRACTION.md`
- Modify: `docs/fork/UPGRADE_GUIDE.md`

Migration 142 creates `ReleaseContractV1ExtractionLineage` and `BackfillCheckpoint`. Each row stores original release/plan ID/checksum, derived snapshot ID/checksum, extractor version, status, blocked reason, and timestamps; originals are never updated.

```text
hub backfill-target-config-snapshots --dry-run --batch-size 100
hub backfill-target-config-snapshots --apply --checkpoint-id <uuid> --batch-size 100
hub backfill-target-config-snapshots --report <checkpoint-id>
```

- [ ] Test exact v1 byte/checksum preservation, deterministic derivation, ambiguous/multi-component block, secret conversion/block, restart, repeated no-op, and v1 reads with flags disabled.
- [ ] Implement pure extraction and dry-run-first checkpointed CLI.
- [ ] Add mixed v1/v2 integration proof and blocked-item report.
- [ ] Verify, document, and commit.

```powershell
go test ./internal/targetconfig ./internal/db ./cmd/hub/cmd -run 'V1Extraction|BackfillTargetConfig' -count=1
mise run lint:migrations
mise run test:go
git add internal/migrations/sql/142_* internal/types/target_config_snapshot.go internal/targetconfig internal/db/target_config_snapshots* cmd/hub/cmd/backfill_target_config_snapshots* docs
git commit -m "feat: derive target config from v1 history"
```

## Task 6: PR-060 — Component Release Contract v2

**Files:**

- Create: `internal/migrations/sql/143_component_release_contract_v2.up.sql`
- Create: `internal/migrations/sql/143_component_release_contract_v2.down.sql`
- Modify: `internal/types/release_contract.go`
- Modify: `internal/types/release_bundle.go`
- Create: `internal/releasebundles/release_contract_v2_test.go`
- Modify: `internal/releasebundles/release_contract.go`
- Modify: `internal/releasebundles/canonical.go`
- Modify: `internal/releasebundles/canonical_test.go`
- Modify: `internal/releasebundles/validation.go`
- Modify: `internal/releasebundles/validation_test.go`
- Modify: `internal/db/release_bundles.go`
- Modify: `internal/db/release_bundles_test.go`
- Modify: `api/release_bundle.go`
- Modify: `api/release_bundle_test.go`
- Modify: `internal/mapping/release_bundle.go`
- Modify: `internal/handlers/release_bundles.go`
- Modify: `internal/handlers/release_bundles_test.go`
- Modify: `frontend/ui/src/app/types/release-bundle.ts`
- Create: `docs/adr/0058-component-release-contract-v2.md`
- Create: `docs/fork/PR-060_COMPONENT_RELEASE_CONTRACT_V2.md`

Migration 143 adds `ReleaseBundle.kind` (`legacy`, `component`, `product`) and `release_contract_schema`, plus `ComponentReleaseArtifact`, `ComponentReleaseEvidence`, `ComponentReleaseCapability`, and `ComponentReleaseMigrationDeclaration`. Existing rows become `legacy`/v1 without changing payload/checksum.

```go
const (
    ReleaseContractSchemaV1 = "distr.release/v1"
    ReleaseContractSchemaV2 = "distr.component-release/v2"
)

type ReleaseContractV1 struct { /* exact existing fields */ }
type ComponentReleaseContractV2 struct {
    ComponentKey string
    Version string
    Source ComponentReleaseSource
    Build ComponentReleaseBuild
    Artifacts []ComponentReleaseArtifact
    Provides []CapabilityDeclaration
    Requires []CapabilityRequirement
    Migrations []MigrationDeclaration
    Changes ComponentReleaseChanges
    Evidence ComponentReleaseEvidenceReferences
}

func ParseReleaseContract([]byte) (schema string, contract any, err error)
func NormalizeReleaseContract(any) ([]byte, error)
func ValidateReleaseContract(any) []ValidationIssue
func ValidateComponentReleaseContractV2(ComponentReleaseContractV2) []ValidationIssue
func ValidateTargetNeutralContract(ComponentReleaseContractV2) []ValidationIssue
func ValidateArtifactIdentity(ComponentReleaseContractV2) []ValidationIssue
```

ADR-0058 fixes exact representations: `requestedRef` and actual commit are distinct required source fields; manifest/index and platform digests use `sha256:<64 lowercase hex>`; migration declarations are typed symbolic contracts with no target credential/path.

- [ ] Add parser tests: unknown/missing schema, unchanged v1, unknown v2 fields, target path/URL/secret, duplicate platform, invalid digest, missing commit, and version/platform digest conflict.
- [ ] Add canonical/publication tests: immutable component/version/platform identity, audit conflict, deterministic checksum, and no target Variable Snapshot requirement for component publication.
- [ ] Implement discriminated validation and additive storage while preserving `/api/v1/release-bundles`.
- [ ] Test API create/validate/publish/get for v1 and v2, feature flags, and foreign organization.
- [ ] Render schema/kind/artifact/capability summaries without removing v1 views.
- [ ] Verify and commit.

```powershell
go test ./internal/releasebundles ./internal/db ./api ./internal/mapping ./internal/handlers -run 'ReleaseContractV2|ComponentRelease|ReleaseBundle' -count=1
pnpm exec ng test --watch=false --include frontend/ui/src/app/release-bundles/release-bundles.component.spec.ts
mise run lint:migrations
git add internal/migrations/sql/143_* internal/types/release_* internal/releasebundles internal/db/release_bundles* api/release_bundle* internal/mapping/release_bundle* internal/handlers/release_bundles* frontend/ui/src/app/types/release-bundle.ts docs
git commit -m "feat: add component release contract v2"
```

## Task 7: PR-061 — Provenance Verification and Safe Release Backfill

**Files:**

- Create: `internal/releasebundles/provenance.go`
- Create: `internal/releasebundles/provenance_test.go`
- Modify: `internal/db/release_bundles.go`
- Create: `cmd/hub/cmd/backfill_release_contract_v2.go`
- Create: `cmd/hub/cmd/backfill_release_contract_v2_test.go`
- Modify: `cmd/hub/cmd/root.go`
- Modify: `cmd/hub/cmd/release.go`
- Modify: `cmd/hub/cmd/release_test.go`
- Modify: `docs/cli/release.md`
- Modify: `docs/api/community-release-api-index.md`
- Create: `docs/fork/PR-061_RELEASE_PROVENANCE_BACKFILL.md`

```go
type ProvenancePolicy struct {
    TrustedRoots []TrustRoot
    AllowedPredicateTypes []string
    AllowedBuilders []string
    AllowedSourcePrefixes []string
    AllowedBuildTypes []string
}

func VerifyProvenance(context.Context, ProvenancePolicy, ComponentReleaseArtifact, ComponentReleaseEvidence) VerificationResult
func RecordComponentReleaseEvidenceVerification(context.Context, types.EvidenceVerification) error
func BackfillComponentReleaseV2(context.Context, types.ReleaseBackfillRequest) (types.ReleaseBackfillReport, error)
```

- [ ] Test trusted envelope plus untrusted/self-signed, wrong subject/predicate/builder/source/build type/external parameter, expired root, malformed/oversized, and tampered artifact.
- [ ] Promote only the required existing Sigstore module from indirect to direct in `go.mod`.
- [ ] Verify at publication and plan preflight; persist bounded facts/evidence digest, not unbounded text.
- [ ] Test dry-run/checkpoint/backfill with blocked ambiguous rows and unchanged v1 bytes/checksums.
- [ ] Extend release CLI to carry v2 schema and print bundle/checksum/digests while retaining v1 flags.
- [ ] Verify and commit.

```powershell
go test ./internal/releasebundles ./internal/db ./cmd/hub/cmd -run 'Provenance|BackfillRelease|ComponentRelease' -count=1
go mod tidy
git diff -- go.mod go.sum
mise run test:go
mise run build:hub:community
git add go.mod go.sum internal/releasebundles internal/db/release_bundles.go cmd docs
git commit -m "feat: verify component release provenance"
```

## Task 8: PR-062 — Product Release Capability DAG

**Files:**

- Create: `internal/migrations/sql/144_product_release_capability_graph.up.sql`
- Create: `internal/migrations/sql/144_product_release_capability_graph.down.sql`
- Create: `internal/types/product_release.go`
- Create: `internal/productrelease/graph.go`
- Create: `internal/productrelease/graph_test.go`
- Create: `internal/productrelease/canonical.go`
- Create: `internal/productrelease/canonical_test.go`
- Create: `internal/db/product_releases.go`
- Create: `internal/db/product_releases_test.go`
- Create: `api/product_release.go`
- Create: `api/product_release_test.go`
- Create: `internal/mapping/product_release.go`
- Create: `internal/handlers/product_releases.go`
- Create: `internal/handlers/product_releases_test.go`
- Modify: `internal/routing/routing.go`
- Create: `docs/adr/0059-product-release-capability-graph.md`
- Create: `docs/fork/PR-062_PRODUCT_RELEASE_CAPABILITY_GRAPH.md`

Migration 144 creates `ProductReleaseComponent` and `ProductReleaseCapabilityEdge`; the parent remains `ReleaseBundle(kind=product)`. Child rows pin published Component Release IDs/checksums. Edges distinguish `product` and `target` resolution stages and allowed modes.

```go
type CapabilityResolutionStage string // product, target
type RequirementResolutionMode string // included, pinned_existing, shared_provider, approved_external, feature_disabled
type ProductReleaseManifest struct { ReleaseBundleID uuid.UUID; Components []ProductReleaseComponent; Requirements []CapabilityRequirement }
type ProductReleaseGraph struct { Nodes []GraphNode; Edges []GraphEdge; TopologicalOrder []string }

func BuildProductReleaseGraph(ProductReleaseManifest) ProductReleaseGraph
func ValidateProductReleaseGraph(ProductReleaseManifest) []ValidationIssue
func CanonicalizeProductRelease(ProductReleaseManifest) ([]byte, string, error)
func CreateProductReleaseDraft(context.Context, *types.ProductReleaseManifest) (*types.ReleaseBundle, error)
func PublishProductRelease(context.Context, uuid.UUID, uuid.UUID) (*types.ReleaseBundle, error)
func GetProductReleaseGraph(context.Context, uuid.UUID, uuid.UUID) (*types.ProductReleaseGraph, error)
```

Routes: `POST /api/v1/product-releases`, `GET /{id}`, `POST /{id}/validate`, `POST /{id}/publish`, `GET /{id}/graph`.

- [ ] Test exact cycle path, missing/ambiguous provider, incompatible range, unpublished/foreign child, duplicate component, stable order, product-stage gap, and valid target-deferred node.
- [ ] Implement graph/canonical code and persistence/API; publication freezes child IDs/checksums and graph checksum.
- [ ] Add a neutral provider/consumer fixture proving provider deploy/health precedes consumer.
- [ ] Verify and commit.

```powershell
go test ./internal/productrelease ./internal/db ./api ./internal/mapping ./internal/handlers -run 'ProductRelease|CapabilityGraph' -count=1
mise run lint:migrations
git add internal/migrations/sql/144_* internal/types/product_release.go internal/productrelease internal/db/product_releases* api/product_release* internal/mapping/product_release* internal/handlers/product_releases* internal/routing/routing.go docs
git commit -m "feat: publish product release capability graphs"
```

## Task 9: PR-063 — Target Plan Draft and Requirement Resolver

**Files:**

- Create: `internal/migrations/sql/145_target_deployment_plan_v2.up.sql`
- Create: `internal/migrations/sql/145_target_deployment_plan_v2.down.sql`
- Create: `internal/types/plan_v2.go`
- Create: `internal/planning/resolver.go`
- Create: `internal/planning/resolver_test.go`
- Create: `internal/planning/graph.go`
- Create: `internal/planning/graph_test.go`
- Create: `internal/db/deployment_plan_drafts.go`
- Create: `internal/db/deployment_plan_drafts_test.go`
- Create: `api/deployment_plan_draft.go`
- Create: `api/deployment_plan_draft_test.go`
- Create: `internal/handlers/deployment_plan_drafts.go`
- Create: `internal/handlers/deployment_plan_drafts_test.go`
- Modify: `internal/types/deployment_plan.go`
- Modify: `internal/db/deployment_plans.go`
- Modify: `api/deployment_plan.go`
- Modify: `internal/mapping/deployment_plan.go`
- Modify: `internal/routing/routing.go`
- Create: `docs/adr/0060-target-deployment-plan-v2.md`
- Create: `docs/fork/PR-063_TARGET_DEPLOYMENT_PLAN_V2.md`

Migration 145 creates `DeploymentPlanDraft`, `DeploymentPlanResolvedRequirement`, and `DeploymentPlanStepEdge`; extends `DeploymentPlan` with `plan_schema`, `draft_id`, `deployment_unit_id`, `target_config_snapshot_id`, `protocol_version`, `supersedes_deployment_plan_id`, and `supersede_reason`. Current `POST /api/v1/deployment-plans` remains v1.

```go
type PlanDraft struct { ProductReleaseID, DeploymentUnitID, EnvironmentAssignmentID, TargetConfigSnapshotID uuid.UUID; ProtocolVersion string }
type RequirementResolution struct { RequirementKey string; Mode RequirementResolutionMode; ProviderReleaseID, ObservationID *uuid.UUID; BindingChecksum string }

func ResolveTargetRequirements(context.Context, PlanDraft) ([]RequirementResolution, []ValidationIssue)
func BuildTargetPlanGraph(context.Context, PlanDraft, []RequirementResolution) (types.TargetPlanGraph, error)
func ValidatePlanDraft(context.Context, PlanDraft) []ValidationIssue
func PublishTargetDeploymentPlan(context.Context, uuid.UUID, string) (*types.DeploymentPlan, error)
```

Routes: `POST/PATCH /api/v1/deployment-plan-drafts`, `GET /{id}`, `POST /{id}/validate`, `POST /{id}/publish`.

- [ ] Test one active environment selection, ambiguity, included/pinned/shared/external/disabled resolution, unresolved block, config/release/platform/provenance mismatch, stale expected state, and deterministic checksum.
- [ ] Implement draft/resolver/graph and immutable publish with optimistic preview checksum.
- [ ] Prove `protocol_version=v1` works only for v1-compatible steps; v2 stays non-executable until PR-075.
- [ ] Verify and commit.

```powershell
go test ./internal/planning ./internal/db ./api ./internal/mapping ./internal/handlers -run 'PlanDraft|ResolveTarget|TargetPlanGraph' -count=1
mise run lint:migrations
git add internal/migrations/sql/145_* internal/types/plan_v2.go internal/types/deployment_plan.go internal/planning internal/db/deployment_plan* api/deployment_plan* internal/handlers/deployment_plan* internal/mapping/deployment_plan.go internal/routing/routing.go docs
git commit -m "feat: resolve immutable target deployment plans"
```

## Task 10: PR-064 — Exact Baseline, Change Set, and Previous State

**Files:**

- Create: `internal/migrations/sql/146_deployment_plan_baseline_changes.up.sql`
- Create: `internal/migrations/sql/146_deployment_plan_baseline_changes.down.sql`
- Create: `internal/planning/baseline.go`
- Create: `internal/planning/baseline_test.go`
- Create: `internal/planning/changeset.go`
- Create: `internal/planning/changeset_test.go`
- Create: `internal/planning/risk.go`
- Create: `internal/planning/risk_test.go`
- Create: `internal/db/deployment_plan_changes.go`
- Create: `internal/db/deployment_plan_changes_test.go`
- Modify: `api/deployment_plan.go`
- Modify: `internal/mapping/deployment_plan.go`
- Modify: `internal/handlers/deployment_plans.go`
- Modify: `frontend/ui/src/app/types/deployment-plan.ts`
- Create: `docs/fork/PR-064_EXACT_PLAN_CHANGESET.md`

Migration 146 creates `DeploymentPlanBaseline`, `DeploymentPlanChangeEntry`, and `DeploymentPlanRiskEntry`. A baseline pins the last trusted healthy active desired revision plus observation ID/checksum; legacy executor projection is labeled and cannot authorize v2 execution.

```go
func SelectVerifiedBaseline(context.Context, types.BaselineQuery) (*types.DeploymentPlanBaseline, error)
func BuildTargetChangeSet(types.BaselineState, types.PlannedState, []types.ReleaseNote) []types.DeploymentPlanChangeEntry
func ClassifyDeploymentRisk([]types.DeploymentPlanChangeEntry, types.EffectivePolicy) []types.DeploymentPlanRiskEntry
func CreatePreviousStatePlan(context.Context, currentPlanID, successfulPlanID uuid.UUID, reason string) (*types.DeploymentPlan, error)
```

- [ ] Test exact last-healthy baseline, skipped-note accumulation, bootstrap, stale CAS, image/config/provider/schema changes, stable ordering, forward-only block, and B-to-A new history.
- [ ] Implement and expose `baseline`, `changes`, `risks`, and `bootstrap` without removing v1 fields.
- [ ] Verify and commit.

```powershell
go test ./internal/planning ./internal/db ./api ./internal/mapping ./internal/handlers -run 'Baseline|ChangeSet|PreviousState|Risk' -count=1
mise run lint:migrations
git add internal/migrations/sql/146_* internal/planning internal/db/deployment_plan_changes* api/deployment_plan.go internal/mapping/deployment_plan.go internal/handlers/deployment_plans.go frontend/ui/src/app/types/deployment-plan.ts docs
git commit -m "feat: calculate exact target deployment changes"
```

## Task 11: PR-065 — Typed Database Migration and Recovery Graph

**Files:**

- Create: `internal/migrations/sql/147_structured_migration_plans.up.sql`
- Create: `internal/migrations/sql/147_structured_migration_plans.down.sql`
- Create: `internal/types/migration_contract.go`
- Create: `internal/migrationplanning/validation.go`
- Create: `internal/migrationplanning/validation_test.go`
- Create: `internal/migrationplanning/expansion.go`
- Create: `internal/migrationplanning/expansion_test.go`
- Create: `internal/migrationplanning/recovery.go`
- Create: `internal/migrationplanning/recovery_test.go`
- Create: `internal/actionregistry/database_actions.go`
- Create: `internal/actionregistry/database_actions_test.go`
- Modify: `internal/types/deployment_plan.go`
- Modify: `internal/db/deployment_plans.go`
- Modify: `internal/deploymentpreflight/evaluate.go`
- Modify: `internal/deploymentpreflight/evaluate_test.go`
- Create: `docs/fork/PR-065_STRUCTURED_MIGRATION_PLANS.md`

Migration 147 creates `DeploymentPlanMigration` and adds `step_input_checksum`, `retry_class`, `cancellation_behavior`, `observation_requirement`, `target_lock_key`, and `database_lock_key` to `DeploymentPlanStep`.

Exact action types:

```text
database.backup.create
database.backup.verify
database.migration.apply
database.migration.validate
database.migration.reverse
database.restore.execute
database.restore.verify
```

```go
func ValidateMigrationContract(types.MigrationContract) []types.ValidationIssue
func ExpandMigrationGraph(types.MigrationContract, types.TargetPlanGraph) (types.TargetPlanGraph, error)
func ValidatePreviousReleaseCompatibility(types.SchemaState, types.PlannedState) []types.ValidationIssue
func BuildRecoveryPlan(types.FailedPlan, types.RecoveryRequest) (*types.PlanDraft, error)
```

- [ ] Test backup verify before mutation, stable retry key, DB lock conflicts, probes, reverse dependency recovery, forward-only block, manual recovery, separately approved restore, and evidence retention.
- [ ] Register seven bounded/redacted action schemas; restore has no normal-plan shortcut.
- [ ] Expand plan nodes/edges and preflight backup/schema/lock/adapter checks.
- [ ] Prove failed backup prevents all mutation and repeated callback cannot duplicate retry-safe migration.
- [ ] Verify and commit.

```powershell
go test ./internal/migrationplanning ./internal/actionregistry ./internal/deploymentpreflight ./internal/planning ./internal/db -run 'Migration|Backup|Recovery|Restore' -count=1
mise run lint:migrations
mise run test:go
git add internal/migrations/sql/147_* internal/types/migration_contract.go internal/types/deployment_plan.go internal/migrationplanning internal/actionregistry/database_actions* internal/db/deployment_plans.go internal/deploymentpreflight docs
git commit -m "feat: add structured migration deployment plans"
```

## Foundations Exit Gate

- [ ] PR-055 through PR-065 are individually reviewed and accepted.
- [ ] Migrations 139–147 apply to a clean DB and upgrade a migration-138 fixture.
- [ ] A v1 fixture retains byte-identical release/plan JSON and checksums with both flags disabled.
- [ ] A neutral v2 fixture publishes one AMD64/ARM64 component, freezes two configs, publishes a provider/consumer Product Release, and creates deterministic plans using identical artifact digests.
- [ ] Exact change view covers image, config, provider, migration, and skipped-release notes.
- [ ] Protocol v2 remains deliberately non-executable pending governance/execution work.
