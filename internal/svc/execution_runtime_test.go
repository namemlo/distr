package svc

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/distr-sh/distr/internal/executionprotocol"
	"github.com/distr-sh/distr/internal/executionruntime"
	"github.com/distr-sh/distr/internal/featureflags"
	. "github.com/onsi/gomega"
)

func TestRegistryRetainsConfigVerifierAndExecutorRuntimeDependencies(t *testing.T) {
	g := NewWithT(t)
	registry := Registry{
		targetConfigObjectVerifier: nil,
		executionRuntime:           executionruntime.Dependencies{},
	}

	g.Expect(registry.targetConfigObjectVerifier).To(BeNil())
	g.Expect(registry.executionRuntime).To(Equal(executionruntime.Dependencies{}))
}

func TestNewExecutionRuntimeDependenciesUsesConfiguredIndependentKeySets(t *testing.T) {
	g := NewWithT(t)
	intentPublic, intentPrivate, err := ed25519.GenerateKey(rand.Reader)
	g.Expect(err).NotTo(HaveOccurred())
	observerPublic, _, err := ed25519.GenerateKey(rand.Reader)
	g.Expect(err).NotTo(HaveOccurred())
	versionFingerprint := executionRuntimeChecksum("intent-key-v7")
	signingConfig, err := json.Marshal([]configuredExecutionSigningKey{{
		Reference: "secret-provider://executor/choice-tp-dev", VersionFingerprint: versionFingerprint,
		PrivateKey: base64.StdEncoding.EncodeToString(intentPrivate),
	}})
	g.Expect(err).NotTo(HaveOccurred())
	observerKeyID := executionprotocol.PublicKeyFingerprint(observerPublic)
	observerConfig, err := json.Marshal(map[string]string{
		observerKeyID: base64.StdEncoding.EncodeToString(observerPublic),
	})
	g.Expect(err).NotTo(HaveOccurred())

	dependencies, err := newExecutionRuntimeDependencies(
		signingConfig, observerConfig,
		featureflags.NewRegistry([]featureflags.Key{
			featureflags.KeyOperatorControlPlaneV2, featureflags.KeyExecutorProtocolV2,
		}),
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(dependencies.ProtocolDispatcher).NotTo(BeNil())
	verifier := dependencies.ReconciliationEvidenceVerifier.(executionprotocol.Ed25519ReconciliationEvidenceVerifier)
	g.Expect(verifier.Keys).To(HaveKey(observerKeyID))
	g.Expect(verifier.Keys).NotTo(HaveKey(executionprotocol.PublicKeyFingerprint(intentPublic)))
}

func TestNewExecutionRuntimeDependenciesRejectsMissingOrSharedTrustKeys(t *testing.T) {
	g := NewWithT(t)
	flags := featureflags.NewRegistry([]featureflags.Key{
		featureflags.KeyOperatorControlPlaneV2, featureflags.KeyExecutorProtocolV2,
	})
	_, err := newExecutionRuntimeDependencies(nil, nil, flags)
	g.Expect(err).To(MatchError(ContainSubstring("signing key configuration")))

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	g.Expect(err).NotTo(HaveOccurred())
	keyID := executionprotocol.PublicKeyFingerprint(publicKey)
	signingConfig, _ := json.Marshal([]configuredExecutionSigningKey{{
		Reference: "secret-provider://executor/shared", VersionFingerprint: executionRuntimeChecksum("v1"),
		PrivateKey: base64.StdEncoding.EncodeToString(privateKey),
	}})
	observerConfig, _ := json.Marshal(map[string]string{
		keyID: base64.StdEncoding.EncodeToString(publicKey),
	})
	_, err = newExecutionRuntimeDependencies(signingConfig, observerConfig, flags)
	g.Expect(err).To(MatchError(ContainSubstring("independent")))
}
