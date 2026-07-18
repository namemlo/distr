package deploymentregistry

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestRegistryImportPreviewIsDeterministicAndNoOpOnCanonicalReimport(t *testing.T) {
	g := NewWithT(t)
	organizationID := uuid.New()
	request := validRegistryImportRequest(organizationID)
	request.Parameters = map[string]string{"format": "compose", "revision": "7"}
	request.Roots[0].Placements = []types.RegistryImportCandidatePlacement{
		{ComponentKey: "worker", PhysicalName: "worker"},
		{ComponentKey: "api", PhysicalName: "api"},
	}
	request.ExistingRoots = append([]types.RegistryImportCandidateRoot(nil), request.Roots...)

	first, err := PreviewImport(context.Background(), request)
	g.Expect(err).NotTo(HaveOccurred())

	request.Parameters = map[string]string{"revision": "7", "format": "compose"}
	request.Roots[0].Placements[0], request.Roots[0].Placements[1] =
		request.Roots[0].Placements[1], request.Roots[0].Placements[0]
	second, err := PreviewImport(context.Background(), request)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(second.PreviewChecksum).To(Equal(first.PreviewChecksum))
	g.Expect(second.Counts).To(Equal(first.Counts))
	g.Expect(second.Diff.Creates).To(BeEmpty())
	g.Expect(second.Diff.Updates).To(BeEmpty())
	g.Expect(second.Diff.Retirements).To(BeEmpty())
	g.Expect(second.Diff.Conflicts).To(BeEmpty())
}

func TestRegistryImportPreviewRejectsInvalidEvidenceAndSecretBearingInput(t *testing.T) {
	g := NewWithT(t)
	request := validRegistryImportRequest(uuid.New())
	request.EvidenceReference = "file:///tmp/report.json"
	_, err := PreviewImport(context.Background(), request)
	g.Expect(err).To(MatchError(ContainSubstring("evidenceReference")))

	request = validRegistryImportRequest(uuid.New())
	request.Parameters["apiToken"] = "not-allowed"
	_, err = PreviewImport(context.Background(), request)
	g.Expect(err).To(MatchError(ContainSubstring("secret-looking")))

	request = validRegistryImportRequest(uuid.New())
	request.Roots[0].SourcePath = `C:\customers\private\compose.yaml`
	_, err = PreviewImport(context.Background(), request)
	g.Expect(err).To(MatchError(ContainSubstring("sourcePath")))
}

func TestRegistryImportPreviewRejectsDuplicateAndUnaliasedRenameWithoutSilentOmission(t *testing.T) {
	g := NewWithT(t)
	request := validRegistryImportRequest(uuid.New())
	request.Roots = append(request.Roots, request.Roots[0])
	_, err := PreviewImport(context.Background(), request)
	g.Expect(err).To(MatchError(ContainSubstring("duplicate root")))

	request = validRegistryImportRequest(uuid.New())
	request.ExistingRoots = []types.RegistryImportCandidateRoot{{
		Key:              request.Roots[0].Key,
		Name:             request.Roots[0].Name,
		DeliveryModel:    types.DeliveryModelDedicated,
		Classification:   types.ImportClassificationStandard,
		PhysicalIdentity: request.Roots[0].PhysicalIdentity,
		Placements: []types.RegistryImportCandidatePlacement{{
			ComponentKey: request.Roots[0].Placements[0].ComponentKey,
			PhysicalName: "old-api",
		}},
	}}
	request.Roots[0].Placements[0].PhysicalName = "new-api"
	preview, err := PreviewImport(context.Background(), request)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(preview.Diff.Conflicts).To(HaveLen(1))
	g.Expect(preview.Counts.DiscoveredPlacements).To(Equal(1))
	g.Expect(preview.Counts.OmittedPlacements).To(Equal(0))
}

func TestRegistryImportClassificationMappingAndCoverageAreExact(t *testing.T) {
	g := NewWithT(t)
	customerID := uuid.New()
	subscriberID := uuid.New()
	roots := []types.RegistryImportCandidateRoot{
		{
			Key: "dedicated", DeliveryModel: types.DeliveryModelDedicated,
			Classification: types.ImportClassificationStandard, Placements: placements(2),
			CustomerOrganizationID: &customerID, DeploymentTargetID: uuid.New(), EnvironmentID: uuid.New(),
		},
		{
			Key: "shared", DeliveryModel: types.DeliveryModelShared,
			Classification: types.ImportClassificationShared, Placements: placements(3),
			SubscriberCustomerOrganizationIDs: []uuid.UUID{subscriberID},
			DeploymentTargetID:                uuid.New(), EnvironmentID: uuid.New(),
		},
		{
			Key: "external", DeliveryModel: types.DeliveryModelExternal,
			Classification: types.ImportClassificationExternal, Placements: placements(1),
			DeploymentTargetID: uuid.New(), EnvironmentID: uuid.New(),
		},
		{
			Key: "observe", DeliveryModel: types.DeliveryModelDedicated,
			Classification: types.ImportClassificationObserveOnly, Placements: placements(4),
			CustomerOrganizationID: &customerID, DeploymentTargetID: uuid.New(), EnvironmentID: uuid.New(),
		},
		{Key: "ignored", DeliveryModel: types.DeliveryModelDedicated, Classification: types.ImportClassificationIgnored, Placements: placements(5)},
		{Key: "pending", DeliveryModel: types.DeliveryModelDedicated, Classification: types.ImportClassificationNeedsDecision, Placements: placements(6)},
	}

	coverage := RegistryCoverage(roots)
	g.Expect(coverage.DiscoveredRoots).To(Equal(6))
	g.Expect(coverage.ClassifiedRoots).To(Equal(5))
	g.Expect(coverage.ActionableManagedRoots).To(Equal(2))
	g.Expect(coverage.ObserveOnlyRoots).To(Equal(1))
	g.Expect(coverage.ExternalRoots).To(Equal(1))
	g.Expect(coverage.IgnoredRoots).To(Equal(1))
	g.Expect(coverage.UnresolvedRoots).To(Equal(1))
	g.Expect(coverage.DiscoveredPlacements).To(Equal(21))
	g.Expect(coverage.OmittedPlacements).To(Equal(0))
	g.Expect(coverage.Complete).To(BeFalse())

	model, state, create, err := ClassificationResult(types.ImportClassificationShared, types.DeliveryModelDedicated)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(model).To(Equal(types.DeliveryModelShared))
	g.Expect(state).To(Equal(types.RegistryManagementStateManaged))
	g.Expect(create).To(BeTrue())

	_, _, create, err = ClassificationResult(types.ImportClassificationIgnored, types.DeliveryModelDedicated)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(create).To(BeFalse())
}

func TestRegistryImportSourceBaselineProducesExactNonzeroOmissions(t *testing.T) {
	g := NewWithT(t)
	request := validRegistryImportRequest(uuid.New())
	request.SourcePlacements = []types.RegistryImportSourcePlacement{
		{RootKey: " customer-dev ", PhysicalName: " service-a "},
		{RootKey: "customer-dev", PhysicalName: "worker-hidden"},
	}

	preview, err := PreviewImport(context.Background(), request)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(preview.Counts.DiscoveredPlacements).To(Equal(2))
	g.Expect(preview.Counts.OmittedPlacements).To(Equal(1))
	g.Expect(preview.Omissions).To(Equal([]string{"customer-dev:worker-hidden"}))
	coverage := RegistryCoverageWithOmissions(preview.Roots, preview.Omissions)
	g.Expect(coverage.OmittedPlacements).To(Equal(1))
	g.Expect(coverage.Omissions).To(Equal([]string{"customer-dev:worker-hidden"}))
	g.Expect(coverage.Complete).To(BeFalse())
}

func TestRegistryImportSourceBaselineKeepsRetirementsSeparateFromOmissions(t *testing.T) {
	g := NewWithT(t)
	request := validRegistryImportRequest(uuid.New())
	request.ExistingRoots = canonicalRoots(request.Roots)
	request.ExistingRoots[0].Placements = append(
		request.ExistingRoots[0].Placements,
		types.RegistryImportCandidatePlacement{
			ComponentKey: "worker", PhysicalName: "worker-retired",
		},
	)
	request.SourcePlacements = []types.RegistryImportSourcePlacement{{
		RootKey: "customer-dev", PhysicalName: "service-a",
	}}

	preview, err := PreviewImport(context.Background(), request)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(preview.Omissions).To(BeEmpty())
	g.Expect(preview.Counts.OmittedPlacements).To(Equal(0))
	g.Expect(preview.Diff.Retirements).To(ConsistOf(types.RegistryImportChange{
		Kind: "retire_placement", RootKey: "customer-dev",
		PlacementKey: "worker", PhysicalName: "worker-retired",
		Message: "placement absent from current discovery",
	}))
}

func TestRegistryImportRejectsMappedPlacementOutsideSourceBaseline(t *testing.T) {
	g := NewWithT(t)
	request := validRegistryImportRequest(uuid.New())
	request.SourcePlacements = []types.RegistryImportSourcePlacement{{
		RootKey: "customer-dev", PhysicalName: "different-service",
	}}

	preview, err := PreviewImport(context.Background(), request)

	g.Expect(preview).To(BeNil())
	g.Expect(err).To(MatchError(
		"candidate placement is absent from the source placement baseline",
	))
}

func TestRegistryImportDiagnosticsAreStableAndBounded(t *testing.T) {
	g := NewWithT(t)
	request := validRegistryImportRequest(uuid.New())
	for index := 0; index < 300; index++ {
		request.Roots = append(request.Roots, types.RegistryImportCandidateRoot{
			Key:              fmt.Sprintf("root-%03d", index),
			Name:             "Needs topology",
			DeliveryModel:    types.DeliveryModelDedicated,
			Classification:   types.ImportClassificationStandard,
			PhysicalIdentity: "compose:invalid",
		})
	}
	preview, err := PreviewImport(context.Background(), request)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(preview.Diagnostics).To(HaveLen(MaximumImportDiagnostics))
	g.Expect(preview.DiagnosticsTruncated).To(BeTrue())
}

func TestRegistryImportNeutralRoadmapScaleFixtureHasExactCoverage(t *testing.T) {
	g := NewWithT(t)
	roots := make([]types.RegistryImportCandidateRoot, 0, 45)
	for index := 0; index < 26; index++ {
		customerID := uuid.New()
		root := types.RegistryImportCandidateRoot{
			Key: fmt.Sprintf("standard-root-%02d", index), DeliveryModel: types.DeliveryModelDedicated,
			Classification:         types.ImportClassificationStandard,
			CustomerOrganizationID: &customerID, DeploymentTargetID: uuid.New(), EnvironmentID: uuid.New(),
		}
		if index == 0 {
			root.Placements = placements(28)
		}
		roots = append(roots, root)
	}
	classifications := []types.ImportClassification{
		types.ImportClassificationShared,
		types.ImportClassificationExternal,
		types.ImportClassificationObserveOnly,
		types.ImportClassificationIgnored,
	}
	for index := 0; index < 19; index++ {
		root := types.RegistryImportCandidateRoot{
			Key: fmt.Sprintf("classified-root-%02d", index), DeliveryModel: types.DeliveryModelDedicated,
			Classification: classifications[index%len(classifications)],
		}
		switch root.Classification {
		case types.ImportClassificationShared:
			root.DeploymentTargetID, root.EnvironmentID = uuid.New(), uuid.New()
			root.SubscriberCustomerOrganizationIDs = []uuid.UUID{uuid.New()}
		case types.ImportClassificationExternal:
			root.DeploymentTargetID, root.EnvironmentID = uuid.New(), uuid.New()
		case types.ImportClassificationObserveOnly:
			customerID := uuid.New()
			root.CustomerOrganizationID = &customerID
			root.DeploymentTargetID, root.EnvironmentID = uuid.New(), uuid.New()
		}
		roots = append(roots, root)
	}

	coverage := RegistryCoverage(roots)
	g.Expect(coverage.DiscoveredRoots).To(Equal(45))
	g.Expect(coverage.ClassifiedRoots).To(Equal(45))
	g.Expect(coverage.ActionableManagedRoots).To(Equal(31))
	g.Expect(coverage.DiscoveredPlacements).To(Equal(28))
	g.Expect(coverage.Services).To(Equal(28))
	g.Expect(coverage.OmittedPlacements).To(Equal(0))
	g.Expect(coverage.Complete).To(BeTrue())
}

func TestRegistryImportNormalizesEveryReturnedFieldAndSubscriberSet(t *testing.T) {
	g := NewWithT(t)
	request := validRegistryImportRequest(uuid.New())
	firstSubscriber, secondSubscriber := uuid.New(), uuid.New()
	request.SourceKind = "  normalized-compose-inventory "
	request.ToolName = " registry-scanner "
	request.ToolVersion = " 1.2.3 "
	request.Parameters = map[string]string{" format ": " compose "}
	root := &request.Roots[0]
	root.Key = " Customer-DEV "
	root.Name = " Customer DEV "
	root.DeliveryModel = types.DeliveryModelShared
	root.Classification = types.ImportClassificationShared
	root.CustomerOrganizationID = nil
	root.SubscriberCustomerOrganizationIDs = []uuid.UUID{
		secondSubscriber, firstSubscriber, secondSubscriber,
	}
	root.PhysicalIdentity = " compose:customer-dev "
	root.Placements[0] = types.RegistryImportCandidatePlacement{
		ComponentKey: " API ", PhysicalName: " api ",
		ConfigNamespace: " app-config ", DatabaseBoundary: " customer-db ",
		HealthAdapter: " health-command ", RenamedFrom: " old-api ",
	}

	preview, err := PreviewImport(context.Background(), request)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(preview.Roots).To(HaveLen(1))
	normalized := preview.Roots[0]
	g.Expect(normalized.Key).To(Equal("customer-dev"))
	g.Expect(normalized.Name).To(Equal("Customer DEV"))
	g.Expect(normalized.PhysicalIdentity).To(Equal("compose:customer-dev"))
	expectedSubscribers := []uuid.UUID{firstSubscriber, secondSubscriber}
	sort.Slice(expectedSubscribers, func(i, j int) bool {
		return expectedSubscribers[i].String() < expectedSubscribers[j].String()
	})
	g.Expect(normalized.SubscriberCustomerOrganizationIDs).To(Equal(expectedSubscribers))
	g.Expect(normalized.Placements).To(Equal([]types.RegistryImportCandidatePlacement{{
		ComponentKey: "api", PhysicalName: "api", ConfigNamespace: "app-config",
		DatabaseBoundary: "customer-db", HealthAdapter: "health-command",
		RenamedFrom: "old-api",
	}}))
}

func TestRegistryImportSanitizesEveryPlacementStringOnEveryOS(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*types.RegistryImportCandidatePlacement)
	}{
		{
			name: "physical name windows path",
			mutate: func(value *types.RegistryImportCandidatePlacement) {
				value.PhysicalName = `C:\clients\choice-tp\api`
			},
		},
		{
			name: "config namespace unix path",
			mutate: func(value *types.RegistryImportCandidatePlacement) {
				value.ConfigNamespace = "/srv/choice-tp/config"
			},
		},
		{
			name: "database boundary hostname",
			mutate: func(value *types.RegistryImportCandidatePlacement) {
				value.DatabaseBoundary = "choice-db.internal"
			},
		},
		{
			name: "health adapter URL",
			mutate: func(value *types.RegistryImportCandidatePlacement) {
				value.HealthAdapter = "https://choice-tp.example/health"
			},
		},
		{
			name: "renamed from secret",
			mutate: func(value *types.RegistryImportCandidatePlacement) {
				value.RenamedFrom = "api-token"
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := validRegistryImportRequest(uuid.New())
			tt.mutate(&request.Roots[0].Placements[0])
			_, err := PreviewImport(context.Background(), request)
			NewWithT(t).Expect(err).To(HaveOccurred())
		})
	}
}

func TestRegistryImportRejectsUnsafeExistingBaselineBeforeDiff(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*types.RegistryImportCandidateRoot)
	}{
		{
			name: "baseline physical name is a path",
			mutate: func(root *types.RegistryImportCandidateRoot) {
				root.Placements[0].PhysicalName = `C:\clients\choice-tp\api`
			},
		},
		{
			name: "baseline database boundary is a hostname",
			mutate: func(root *types.RegistryImportCandidateRoot) {
				root.Placements[0].DatabaseBoundary = "choice-db.internal"
			},
		},
		{
			name: "baseline config namespace is secret looking",
			mutate: func(root *types.RegistryImportCandidateRoot) {
				root.Placements[0].ConfigNamespace = "choice-api-token"
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			request := validRegistryImportRequest(uuid.New())
			request.ExistingRoots = canonicalRoots(request.Roots)
			tt.mutate(&request.ExistingRoots[0])

			preview, err := PreviewImport(context.Background(), request)

			g.Expect(preview).To(BeNil())
			g.Expect(err).To(MatchError("existing registry baseline contains unsafe data"))
			g.Expect(err.Error()).NotTo(ContainSubstring("choice-db.internal"))
			g.Expect(err.Error()).NotTo(ContainSubstring(`C:\clients`))
			g.Expect(err.Error()).NotTo(ContainSubstring("choice-api-token"))
		})
	}
}

func TestRegistryImportRejectsUnpersistableIdentityBeforeDiagnostics(t *testing.T) {
	g := NewWithT(t)
	request := validRegistryImportRequest(uuid.New())
	request.Roots[0].Key = "invalid key"
	_, err := PreviewImport(context.Background(), request)
	g.Expect(err).To(MatchError(ContainSubstring("canonical lowercase")))

	request = validRegistryImportRequest(uuid.New())
	request.Roots[0].Classification = "mystery"
	_, err = PreviewImport(context.Background(), request)
	g.Expect(err).To(MatchError(ContainSubstring("classification is invalid")))

	request = validRegistryImportRequest(uuid.New())
	request.Roots[0].Placements[0].ComponentKey = "invalid key"
	_, err = PreviewImport(context.Background(), request)
	g.Expect(err).To(MatchError(ContainSubstring("unsafe or unbounded placement")))
}

func TestRegistryImportDiffSeparatesPlacementCreateAndMetadataUpdate(t *testing.T) {
	g := NewWithT(t)
	request := validRegistryImportRequest(uuid.New())
	request.ExistingRoots = canonicalRoots(request.Roots)
	request.Roots[0].Placements = append(
		request.Roots[0].Placements,
		types.RegistryImportCandidatePlacement{ComponentKey: "worker", PhysicalName: "worker"},
	)

	preview, err := PreviewImport(context.Background(), request)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(preview.Diff.Creates).To(ConsistOf(types.RegistryImportChange{
		Kind: "create_placement", RootKey: "customer-dev",
		PlacementKey: "worker", PhysicalName: "worker", Message: "new component placement",
	}))
	g.Expect(preview.Diff.Creates).NotTo(ContainElement(HaveField("Kind", "create_root")))

	request = validRegistryImportRequest(uuid.New())
	request.ExistingRoots = canonicalRoots(request.Roots)
	request.Roots[0].Placements[0].ConfigNamespace = "updated-config"
	preview, err = PreviewImport(context.Background(), request)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(preview.Diff.Updates).To(ConsistOf(types.RegistryImportChange{
		Kind: "update_placement", RootKey: "customer-dev",
		PlacementKey: "component-a", PhysicalName: "service-a",
		Message: "component placement metadata changed",
	}))
}

func TestRegistryImportCoverageBlocksEveryUnapplyableTopology(t *testing.T) {
	customerID := uuid.New()
	tests := []struct {
		name string
		root types.RegistryImportCandidateRoot
	}{
		{
			name: "missing target",
			root: types.RegistryImportCandidateRoot{
				Key: "missing-target", DeliveryModel: types.DeliveryModelDedicated,
				Classification:         types.ImportClassificationStandard,
				CustomerOrganizationID: &customerID, EnvironmentID: uuid.New(),
			},
		},
		{
			name: "missing environment",
			root: types.RegistryImportCandidateRoot{
				Key: "missing-environment", DeliveryModel: types.DeliveryModelDedicated,
				Classification:         types.ImportClassificationStandard,
				CustomerOrganizationID: &customerID, DeploymentTargetID: uuid.New(),
			},
		},
		{
			name: "standard missing customer",
			root: types.RegistryImportCandidateRoot{
				Key: "missing-customer", DeliveryModel: types.DeliveryModelDedicated,
				Classification:     types.ImportClassificationStandard,
				DeploymentTargetID: uuid.New(), EnvironmentID: uuid.New(),
			},
		},
		{
			name: "shared missing subscriber",
			root: types.RegistryImportCandidateRoot{
				Key: "missing-subscriber", DeliveryModel: types.DeliveryModelShared,
				Classification:     types.ImportClassificationShared,
				DeploymentTargetID: uuid.New(), EnvironmentID: uuid.New(),
			},
		},
		{
			name: "observe only invalid delivery",
			root: types.RegistryImportCandidateRoot{
				Key: "observe-invalid", DeliveryModel: "unknown",
				Classification:     types.ImportClassificationObserveOnly,
				DeploymentTargetID: uuid.New(), EnvironmentID: uuid.New(),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := RegistryCoverage([]types.RegistryImportCandidateRoot{tt.root})
			NewWithT(t).Expect(report.Complete).To(BeFalse())
			NewWithT(t).Expect(report.UnresolvedRoots).To(Equal(1))
			NewWithT(t).Expect(report.Omissions).NotTo(BeEmpty())
		})
	}
}

func validRegistryImportRequest(organizationID uuid.UUID) types.RegistryImportRequest {
	checksum := strings.Repeat("a", 64)
	customerID := uuid.New()
	return types.RegistryImportRequest{
		OrganizationID:    organizationID,
		SourceKind:        "normalized-compose-inventory",
		ToolName:          "registry-scanner",
		ToolVersion:       "1.2.3",
		SourceCommit:      strings.Repeat("b", 40),
		Parameters:        map[string]string{"format": "compose"},
		EvidenceReference: "evidence://sha256/" + checksum,
		EvidenceChecksum:  checksum,
		ActorID:           uuid.New(),
		Roots: []types.RegistryImportCandidateRoot{{
			Key:                    "customer-dev",
			Name:                   "Customer DEV",
			DeliveryModel:          types.DeliveryModelDedicated,
			Classification:         types.ImportClassificationStandard,
			CustomerOrganizationID: &customerID,
			DeploymentTargetID:     uuid.New(),
			EnvironmentID:          uuid.New(),
			PhysicalIdentity:       "compose:customer-dev",
			Placements:             placements(1),
		}},
	}
}

func placements(count int) []types.RegistryImportCandidatePlacement {
	result := make([]types.RegistryImportCandidatePlacement, count)
	for index := range result {
		result[index] = types.RegistryImportCandidatePlacement{
			ComponentKey: "component-" + string(rune('a'+index)),
			PhysicalName: "service-" + string(rune('a'+index)),
		}
	}
	return result
}
