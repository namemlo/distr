package api

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestObservationRequestValidationRequiresBoundedIndependentEvidence(t *testing.T) {
	g := NewWithT(t)
	request := validObservationRequest()
	g.Expect(request.Validate()).To(Succeed())

	request.SourceSequence = 0
	g.Expect(request.Validate()).To(HaveOccurred())
	request = validObservationRequest()
	request.EvidenceReference = string(make([]byte, 2049))
	g.Expect(request.Validate()).To(HaveOccurred())
	request = validObservationRequest()
	request.Outcome = types.ObservationOutcome("SUCCESS")
	g.Expect(request.Validate()).To(HaveOccurred())
}

func TestObserverRegistrationRequestDoesNotAcceptPlaintextCredentialPersistence(t *testing.T) {
	g := NewWithT(t)
	request := CreateObserverRegistrationRequest{
		DeploymentUnitID:      uuid.New(),
		ObserverKey:           "runtime-http-json",
		AdapterImplementation: "observe.http-json",
		AdapterVersion:        "1.0.0",
		Credential:            "observer-secret-at-least-32-characters",
		MaxFreshnessSeconds:   120,
		MaxClockSkewSeconds:   15,
		Measurements:          []string{"artifact", "config", "schema", "health"},
	}
	g.Expect(request.Validate()).To(Succeed())
	g.Expect(ObserverCredentialFingerprint(request.Credential)).
		To(HavePrefix("sha256:"))
	g.Expect(ObserverCredentialFingerprint(request.Credential)).
		NotTo(ContainSubstring(request.Credential))
}

func validObservationRequest() ObservationRequest {
	return ObservationRequest{
		OrganizationID:      uuid.New(),
		ObserverID:          uuid.New(),
		DeploymentUnitID:    uuid.New(),
		ComponentInstanceID: uuid.New(),
		ComponentKey:        "api",
		SourceSequence:      1,
		CapturedAt:          time.Now().UTC(),
		EvidenceChecksum:    observationDigest("evidence"),
		EvidenceReference:   "oci://evidence/1",
		ArtifactDigest:      observationDigest("artifact"),
		ConfigChecksum:      observationDigest("config"),
		SchemaVersion:       "2026071801",
		CapabilityChecksum:  observationDigest("capability"),
		Platform:            "linux/amd64",
		TopologyChecksum:    observationDigest("topology"),
		Health:              types.ObservedHealthHealthy,
		Outcome:             types.ObservationOutcomeComplete,
	}
}

func observationDigest(value string) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(value)))
}
