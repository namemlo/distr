package api

import (
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestCreateAdapterImplementationRequestValidatesVersionedCapabilities(t *testing.T) {
	g := NewWithT(t)
	request := CreateAdapterImplementationRequest{
		Key: "compose", Name: "Compose adapter", Version: "2.0.0",
		Capabilities: []AdapterCapabilityRequest{{
			Capability: "deployment.compose", Version: "1.0.0",
		}},
	}

	g.Expect(request.Validate()).To(Succeed())

	request.Capabilities[0].Version = "latest"
	g.Expect(request.Validate()).To(MatchError(ContainSubstring("strict semantic version")))
}

func TestCreateAdapterAssignmentRequestAllowsOnlyNonSecretKeyConfiguration(t *testing.T) {
	g := NewWithT(t)
	request := CreateAdapterAssignmentRequest{
		AdapterImplementationID: uuid.New(),
		ScopeType:               types.AdapterScopeDeploymentTarget,
		ScopeReference:          uuid.NewString(),
		ConfigSnapshotID:        uuid.New(),
		ConfigChecksum:          "sha256:" + strings.Repeat("a", 64),
		KeyConfiguration: AdapterKeyConfigurationRequest{
			KeyID:                        "signing-v3",
			PublicKeyFingerprint:         "sha256:" + strings.Repeat("b", 64),
			SigningKeyReference:          "secret-provider://adapter-signing",
			SigningKeyVersionFingerprint: "sha256:" + strings.Repeat("c", 64),
		},
		Enabled: true,
	}

	g.Expect(request.Validate()).To(Succeed())

	request.KeyConfiguration.SigningKeyReference = "inline-key-material"
	g.Expect(request.Validate()).To(MatchError(ContainSubstring("opaque secret-provider reference")))
}

func TestCreateAdapterAssignmentRequestValidatesTypedScopeReferences(t *testing.T) {
	base := func(scopeType types.AdapterScopeType, reference string) CreateAdapterAssignmentRequest {
		return CreateAdapterAssignmentRequest{
			AdapterImplementationID: uuid.New(),
			ScopeType:               scopeType,
			ScopeReference:          reference,
			ConfigSnapshotID:        uuid.New(),
			ConfigChecksum:          "sha256:" + strings.Repeat("a", 64),
			KeyConfiguration: AdapterKeyConfigurationRequest{
				KeyID: "signing-v3", PublicKeyFingerprint: "sha256:" + strings.Repeat("b", 64),
				SigningKeyReference:          "secret-provider://adapter-signing",
				SigningKeyVersionFingerprint: "sha256:" + strings.Repeat("c", 64),
			},
		}
	}
	g := NewWithT(t)

	database := base(types.AdapterScopeDatabaseResource, "postgres:ledger")
	observer := base(types.AdapterScopeObserverRegistration, uuid.NewString())
	invalidObserver := base(types.AdapterScopeObserverRegistration, "postgres:ledger")
	g.Expect(database.Validate()).To(Succeed())
	g.Expect(observer.Validate()).To(Succeed())
	g.Expect(invalidObserver.Validate()).
		To(MatchError(ContainSubstring("scopeReference")))
}
