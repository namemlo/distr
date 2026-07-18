package zonerules

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/gomega"
)

func TestProductionProviderReportsPinnedRuleDataIdentity(t *testing.T) {
	g := NewWithT(t)
	provider := Production()

	g.Expect(provider.RuleVersion()).To(Equal(ProductionRuleVersion))
	g.Expect(provider.Identity()).To(Equal(ProductionRuleDataIdentity))
	g.Expect(provider.Identity()).To(ContainSubstring("iana-2026a"))
	g.Expect(provider.Identity()).To(ContainSubstring("gotz-v0.1.2"))
	g.Expect(provider.Identity()).To(ContainSubstring("h1:8kQPIqpf"))
}

func TestProductionProviderIgnoresHostZoneInfoAndRejectsLocal(t *testing.T) {
	g := NewWithT(t)
	invalidZoneInfo := filepath.Join(t.TempDir(), "zoneinfo.zip")
	g.Expect(os.WriteFile(invalidZoneInfo, []byte("not tzdata"), 0o600)).To(Succeed())
	t.Setenv("ZONEINFO", invalidZoneInfo)

	location, err := Production().LoadLocation("America/New_York")
	g.Expect(err).NotTo(HaveOccurred())
	local := time.Date(2026, 3, 8, 7, 15, 0, 0, time.UTC).In(location)
	g.Expect(local.Format("2006-01-02T15:04:05 -07:00")).To(
		Equal("2026-03-08T03:15:00 -04:00"),
	)

	_, err = Production().LoadLocation("Local")
	g.Expect(err).To(MatchError(ContainSubstring("Local")))
}

func TestValidateBindingRejectsDeclaredRuntimeMismatch(t *testing.T) {
	g := NewWithT(t)

	_, err := ValidateBinding(Production(), "Asia/Bangkok", "2025b")
	g.Expect(err).To(MatchError(ContainSubstring("does not match runtime")))

	location, err := ValidateBinding(
		Production(),
		"Asia/Bangkok",
		ProductionRuleVersion,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(location).NotTo(BeNil())
}
