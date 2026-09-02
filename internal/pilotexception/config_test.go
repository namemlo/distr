package pilotexception

import (
	"testing"

	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestParseIsDefaultOff(t *testing.T) {
	g := NewWithT(t)
	config, err := Parse(false, "", "", "", "")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(config.ApprovalEvidence(
		uuid.New(), uuid.New(), []uuid.UUID{uuid.New()}, uuid.New(), uuid.New(), true,
	)).To(BeNil())
}

func TestConfigScopesExceptionToOneAdopterPilot(t *testing.T) {
	g := NewWithT(t)
	organizationID, environmentID, targetID, actorID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	config, err := Parse(
		true,
		organizationID.String(),
		environmentID.String(),
		targetID.String(),
		"owner-approval:adopter-dev-20260903",
	)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(config.ApprovalEvidence(
		organizationID, environmentID, []uuid.UUID{targetID}, actorID, actorID, true,
	)).To(Equal(&Evidence{
		Key: Key, ApprovalReference: "owner-approval:adopter-dev-20260903",
	}))
	g.Expect(config.ApprovalEvidence(
		organizationID, uuid.New(), []uuid.UUID{targetID}, actorID, actorID, true,
	)).To(BeNil())
	g.Expect(config.ApprovalEvidence(
		organizationID, environmentID, []uuid.UUID{targetID}, actorID, uuid.New(), true,
	)).To(BeNil())
	g.Expect(config.ProtectedHistoryEvidence(
		organizationID, nil, []string{targetID.String()}, actorID, actorID,
	)).NotTo(BeNil())
	g.Expect(config.ProtectedHistoryEvidence(
		organizationID, []string{uuid.NewString()}, []string{targetID.String()}, actorID, actorID,
	)).To(BeNil())
}

func TestParseEnabledConfigFailsClosed(t *testing.T) {
	g := NewWithT(t)
	_, err := Parse(true, "", "", "", "")
	g.Expect(err).To(MatchError("scoped single-reviewer pilot organization id is required"))
}
