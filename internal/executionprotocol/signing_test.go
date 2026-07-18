package executionprotocol

import (
	"crypto/ed25519"
	"crypto/sha256"
	"testing"

	. "github.com/onsi/gomega"
)

func TestSignedIntentKeyIDIsFrozenPublicKeyFingerprint(t *testing.T) {
	g := NewWithT(t)
	seed := sha256.Sum256([]byte("signing-provider-key"))
	privateKey := ed25519.NewKeyFromSeed(seed[:])
	publicKey := privateKey.Public().(ed25519.PublicKey)

	_, err := NewEd25519IntentSigner("revision-name", privateKey)
	g.Expect(err).To(MatchError(ContainSubstring("public-key fingerprint")))

	signer, err := NewEd25519IntentSigner(PublicKeyFingerprint(publicKey), privateKey)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(signer.PublicKey()).To(Equal(publicKey))
}
