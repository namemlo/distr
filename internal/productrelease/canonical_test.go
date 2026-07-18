package productrelease

import (
	"slices"
	"testing"

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
}
