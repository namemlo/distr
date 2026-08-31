package productrelease

import (
	"slices"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/migrationplanning"
	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

func TestCanonicalizeProductReleaseIsStableAcrossInputOrder(t *testing.T) {
	g := NewWithT(t)
	first := neutralProviderConsumerManifest()
	second := neutralProviderConsumerManifest()
	second.OrganizationID = first.OrganizationID
	second.DependencyPolicyVersion = first.DependencyPolicyVersion
	second.Components = slices.Clone(first.Components)
	slices.Reverse(second.Components)
	second.RequiredPlatforms = []string{"linux/arm64", "linux/amd64"}
	first.RequiredPlatforms = []string{"linux/amd64", "linux/arm64"}

	firstPayload, firstChecksum, err := CanonicalizeProductRelease(first)
	g.Expect(err).NotTo(HaveOccurred())
	secondPayload, secondChecksum, err := CanonicalizeProductRelease(second)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(secondPayload).To(Equal(firstPayload))
	g.Expect(secondChecksum).To(Equal(firstChecksum))
	g.Expect(firstChecksum).To(MatchRegexp(`^sha256:[0-9a-f]{64}$`))
	g.Expect(string(firstPayload)).NotTo(ContainSubstring("migrationContracts"))
}

func TestCanonicalizeProductReleaseFreezesStructuredMigrationContracts(t *testing.T) {
	g := NewWithT(t)
	first := neutralProviderConsumerManifest()
	contract := productMigrationContract(t)
	first.Components[1].MigrationContracts = []types.MigrationContract{contract}
	second := first
	second.Components = slices.Clone(first.Components)
	second.Components[1].MigrationContracts = slices.Clone(first.Components[1].MigrationContracts)
	second.Components[1].MigrationContracts[0].ResultingVersion = "43"
	second.Components[1].MigrationContracts[0].Checksum, _ =
		migrationplanning.CanonicalMigrationContractChecksum(
			second.Components[1].MigrationContracts[0],
		)

	firstPayload, firstChecksum, err := CanonicalizeProductRelease(first)
	g.Expect(err).NotTo(HaveOccurred())
	secondPayload, secondChecksum, err := CanonicalizeProductRelease(second)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(string(firstPayload)).To(ContainSubstring(`"migrationContracts"`))
	g.Expect(string(firstPayload)).To(ContainSubstring(contract.Checksum))
	g.Expect(secondPayload).NotTo(Equal(firstPayload))
	g.Expect(secondChecksum).NotTo(Equal(firstChecksum))
}

func productMigrationContract(t *testing.T) types.MigrationContract {
	t.Helper()
	contract := types.MigrationContract{
		ID: "ledger.042", ComponentKey: "consumer",
		DatabaseResourceKey: "postgres:ledger", ExpectedSourceVersion: "41",
		ExpectedSourceChecksum: "sha256:" + strings.Repeat("1", 64),
		ResultingVersion:       "42", ResultingSchemaChecksum: "sha256:" + strings.Repeat("2", 64),
		Phase: types.MigrationPhaseExpand, LockType: "exclusive", LockTimeoutSeconds: 30,
		OperationalImpact: "brief write lock", BackupRequired: true,
		BackupVerifier: "backup-verifier:v1",
		PreconditionProbes: []types.MigrationProbe{{
			Name: "source", Reference: "probe:ledger:source:v1",
			ExpectedChecksum: "sha256:" + strings.Repeat("3", 64),
		}},
		PostconditionProbes: []types.MigrationProbe{{
			Name: "result", Reference: "probe:ledger:result:v1",
			ExpectedChecksum: "sha256:" + strings.Repeat("4", 64),
		}},
		RetryClass: types.MigrationRetrySafe, IdempotencyKey: "ledger.042",
		Reversibility:                    types.MigrationReversibilityReversible,
		PreviousApplicationCompatibility: ">=1.8.0",
		RecoveryProcedureReference:       "recovery:ledger.042:v1",
		AdapterType:                      "database.migrate",
		ArtifactDigest:                   "registry.example.com/migrations/ledger@sha256:" + strings.Repeat("5", 64),
		EvidenceRetentionDays:            90,
	}
	checksum, err := migrationplanning.CanonicalMigrationContractChecksum(contract)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	contract.Checksum = checksum
	return contract
}

func TestCanonicalizeProductReleasePinsDependencyPolicyChecksum(t *testing.T) {
	g := NewWithT(t)
	first := neutralProviderConsumerManifest()
	second := first
	second.DependencyPolicyChecksum = "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"

	firstPayload, firstChecksum, err := CanonicalizeProductRelease(first)
	g.Expect(err).NotTo(HaveOccurred())
	secondPayload, secondChecksum, err := CanonicalizeProductRelease(second)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(secondPayload).NotTo(Equal(firstPayload))
	g.Expect(secondChecksum).NotTo(Equal(firstChecksum))
}
