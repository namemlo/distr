package svc

import (
	"crypto/ed25519"
	"crypto/sha256"
	"errors"

	"github.com/distr-sh/distr/internal/executionprotocol"
	"github.com/distr-sh/distr/internal/executionruntime"
	"github.com/distr-sh/distr/internal/executionworker"
	"github.com/distr-sh/distr/internal/featureflags"
)

const executionV2SigningKeyDomain = "distr.execution-v2.signing-key.v1\x00"

func newExecutionRuntimeDependencies(
	rootSecret []byte,
	flags featureflags.Registry,
) (executionruntime.Dependencies, error) {
	if len(rootSecret) == 0 {
		return executionruntime.Dependencies{}, errors.New("execution v2 root secret is required")
	}
	material := make([]byte, 0, len(executionV2SigningKeyDomain)+len(rootSecret))
	material = append(material, executionV2SigningKeyDomain...)
	material = append(material, rootSecret...)
	seed := sha256.Sum256(material)
	privateKey := ed25519.NewKeyFromSeed(seed[:])
	publicKey := privateKey.Public().(ed25519.PublicKey)
	signer, err := executionprotocol.NewEd25519IntentSigner(
		executionprotocol.PublicKeyFingerprint(publicKey), privateKey,
	)
	if err != nil {
		return executionruntime.Dependencies{}, err
	}
	return executionruntime.NewProductionDependencies(executionruntime.ProductionConfig{
		Flags: flags, Signer: signer,
		Repository:     executionworker.DatabaseRuntimeRepository{},
		CampaignBridge: executionruntime.NewDatabaseCampaignControlBridge(),
	})
}
