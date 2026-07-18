package executionprotocol

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
)

const signingDomain = "distr.execution-intent.v2"

type IntentSigner interface {
	KeyID() string
	PublicKey() ed25519.PublicKey
	Sign(context.Context, []byte) ([]byte, error)
}

type ed25519IntentSigner struct {
	keyID  string
	signer crypto.Signer
	public ed25519.PublicKey
}

func NewEd25519IntentSigner(keyID string, signer crypto.Signer) (IntentSigner, error) {
	if signer == nil {
		return nil, errors.New("intent signer is required")
	}
	public, ok := signer.Public().(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("intent signer must use Ed25519")
	}
	if PublicKeyFingerprint(public) != keyID {
		return nil, errors.New("keyId must equal the versioned public-key fingerprint")
	}
	return &ed25519IntentSigner{keyID: keyID, signer: signer, public: public}, nil
}

func (s *ed25519IntentSigner) KeyID() string {
	return s.keyID
}

func (s *ed25519IntentSigner) PublicKey() ed25519.PublicKey {
	return append(ed25519.PublicKey(nil), s.public...)
}

func (s *ed25519IntentSigner) Sign(_ context.Context, message []byte) ([]byte, error) {
	return s.signer.Sign(rand.Reader, message, crypto.Hash(0))
}

func PublicKeyFingerprint(publicKey ed25519.PublicKey) string {
	sum := sha256.Sum256(publicKey)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func signingMessage(payload []byte, checksum string) []byte {
	result := make([]byte, 0, len(signingDomain)+len(checksum)+len(payload)+2)
	result = append(result, signingDomain...)
	result = append(result, '\n')
	result = append(result, checksum...)
	result = append(result, '\n')
	result = append(result, payload...)
	return result
}

func encodeSignature(signature []byte) string {
	return base64.RawStdEncoding.EncodeToString(signature)
}

func decodeSignature(signature string) ([]byte, error) {
	value, err := base64.RawStdEncoding.DecodeString(signature)
	if err != nil {
		return nil, fmt.Errorf("signature is not valid base64: %w", err)
	}
	return value, nil
}
