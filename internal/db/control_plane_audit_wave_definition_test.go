package db

import (
	"os"
	"testing"

	. "github.com/onsi/gomega"
)

func TestControlPlaneAuditUsesExplicitCampaignWaveDefinitionContract(t *testing.T) {
	g := NewWithT(t)

	migration, err := os.ReadFile("../migrations/sql/160_control_plane_audit_export.up.sql")
	g.Expect(err).NotTo(HaveOccurred())
	persistence, err := os.ReadFile("control_plane_audit.go")
	g.Expect(err).NotTo(HaveOccurred())

	for name, source := range map[string]string{
		"migration 160": string(migration),
		"persistence":   string(persistence),
	} {
		g.Expect(source).To(ContainSubstring("campaign_wave_definition_id"), name)
		g.Expect(source).NotTo(ContainSubstring("campaign_wave_id"), name)
	}
	g.Expect(string(persistence)).To(ContainSubstring("campaignWaveDefinitionId"))
	g.Expect(string(persistence)).NotTo(ContainSubstring("campaignWaveId"))
}
