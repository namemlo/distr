package svc

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/distr-sh/distr/internal/executionprotocol"
	"github.com/distr-sh/distr/internal/executionruntime"
	"github.com/distr-sh/distr/internal/executionworker"
	"github.com/distr-sh/distr/internal/featureflags"
)

var executionRuntimeFingerprintPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type configuredExecutionSigningKey struct {
	Reference          string `json:"reference"`
	VersionFingerprint string `json:"versionFingerprint"`
	PrivateKey         string `json:"privateKey"`
}

type configuredIntentSignerProvider struct {
	values map[string]executionprotocol.IntentSigner
}

func (p configuredIntentSignerProvider) ResolveIntentSigner(
	_ context.Context,
	reference, versionFingerprint string,
) (executionprotocol.IntentSigner, error) {
	signer, ok := p.values[reference+"\x00"+versionFingerprint]
	if !ok {
		return nil, errors.New("configured secret-provider key version was not found")
	}
	return signer, nil
}

func newExecutionRuntimeDependencies(
	signingConfiguration, observerTrustConfiguration []byte,
	flags featureflags.Registry,
) (executionruntime.Dependencies, error) {
	if !flags.IsEnabled(featureflags.KeyExecutorProtocolV2) {
		return executionruntime.Dependencies{}, nil
	}
	provider, signingKeyIDs, err := parseExecutionSigningKeys(signingConfiguration)
	if err != nil {
		return executionruntime.Dependencies{}, err
	}
	observerKeys, err := parseExecutionObserverKeys(observerTrustConfiguration)
	if err != nil {
		return executionruntime.Dependencies{}, err
	}
	for keyID := range observerKeys {
		if _, shared := signingKeyIDs[keyID]; shared {
			return executionruntime.Dependencies{}, errors.New(
				"execution intent and independent observer trust keys must be independent",
			)
		}
	}
	return executionruntime.NewProductionDependencies(executionruntime.ProductionConfig{
		Flags: flags, SignerProvider: provider, ObserverKeys: observerKeys,
		Repository:     executionworker.DatabaseRuntimeRepository{},
		CampaignBridge: executionruntime.NewDatabaseCampaignControlBridge(),
	})
}

func parseExecutionSigningKeys(
	configuration []byte,
) (configuredIntentSignerProvider, map[string]struct{}, error) {
	if len(configuration) == 0 {
		return configuredIntentSignerProvider{}, nil,
			errors.New("execution v2 signing key configuration is required")
	}
	var entries []configuredExecutionSigningKey
	if err := json.Unmarshal(configuration, &entries); err != nil || len(entries) == 0 {
		return configuredIntentSignerProvider{}, nil,
			errors.New("execution v2 signing key configuration is invalid")
	}
	provider := configuredIntentSignerProvider{values: map[string]executionprotocol.IntentSigner{}}
	keyIDs := map[string]struct{}{}
	for _, entry := range entries {
		entry.Reference = strings.TrimSpace(entry.Reference)
		if !strings.HasPrefix(entry.Reference, "secret-provider://") ||
			!executionRuntimeFingerprintPattern.MatchString(entry.VersionFingerprint) {
			return configuredIntentSignerProvider{}, nil,
				errors.New("execution v2 signing key reference or version fingerprint is invalid")
		}
		raw, err := base64.StdEncoding.DecodeString(entry.PrivateKey)
		if err != nil || len(raw) != ed25519.PrivateKeySize {
			return configuredIntentSignerProvider{}, nil,
				errors.New("execution v2 signing private key must be base64 Ed25519 private key bytes")
		}
		privateKey := ed25519.PrivateKey(append([]byte(nil), raw...))
		publicKey := privateKey.Public().(ed25519.PublicKey)
		keyID := executionprotocol.PublicKeyFingerprint(publicKey)
		signer, err := executionprotocol.NewEd25519IntentSigner(keyID, privateKey)
		if err != nil {
			return configuredIntentSignerProvider{}, nil, err
		}
		identity := entry.Reference + "\x00" + entry.VersionFingerprint
		if _, duplicate := provider.values[identity]; duplicate {
			return configuredIntentSignerProvider{}, nil,
				errors.New("execution v2 signing key configuration contains duplicate key version")
		}
		provider.values[identity] = signer
		keyIDs[keyID] = struct{}{}
	}
	return provider, keyIDs, nil
}

func parseExecutionObserverKeys(configuration []byte) (map[string]ed25519.PublicKey, error) {
	if len(configuration) == 0 {
		return nil, errors.New("execution v2 observer trust key configuration is required")
	}
	var encoded map[string]string
	if err := json.Unmarshal(configuration, &encoded); err != nil || len(encoded) == 0 {
		return nil, errors.New("execution v2 observer trust key configuration is invalid")
	}
	result := make(map[string]ed25519.PublicKey, len(encoded))
	for keyID, value := range encoded {
		raw, err := base64.StdEncoding.DecodeString(value)
		if err != nil || len(raw) != ed25519.PublicKeySize {
			return nil, errors.New("execution v2 observer key must be base64 Ed25519 public key bytes")
		}
		publicKey := ed25519.PublicKey(append([]byte(nil), raw...))
		if !executionRuntimeFingerprintPattern.MatchString(keyID) ||
			executionprotocol.PublicKeyFingerprint(publicKey) != keyID {
			return nil, errors.New("execution v2 observer key fingerprint is invalid")
		}
		result[keyID] = publicKey
	}
	return result, nil
}

func executionRuntimeChecksum(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}
