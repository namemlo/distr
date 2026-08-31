package releasebundles

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/verify"
)

const (
	testProvenanceIssuer  = "https://issuer.example.invalid"
	testProvenanceSubject = "builder@example.invalid"
	testPredicateType     = "https://slsa.dev/provenance/v1"
	testBuilderID         = "https://build.example.invalid/workers/release"
	testBuildType         = "https://build.example.invalid/types/container/v1"
	testBuildID           = "build-42"
	testSourceURI         = "git+https://code.example.invalid/platform/service"
	testSourceCommit      = "0123456789abcdef0123456789abcdef01234567"
	testSourcePrefix      = "git+https://code.example.invalid/platform/"
)

func TestVerifySignedProvenanceAcceptsTrustedFrozenPolicy(t *testing.T) {
	g := NewWithT(t)
	fixture := newProvenanceFixture(t, validProvenanceStatement(t, testArtifactDigest("artifact")))

	result, err := verifySignedProvenance(
		context.Background(),
		fixture.policy,
		testProvenanceArtifact(testArtifactDigest("artifact")),
		"sha256:"+strings.Repeat("a", 64),
		fixture.policyChecksum,
		fixture.policy.TrustedRoots[0],
		fixture.entity,
		fixture.trustedMaterial,
		verify.WithCurrentTime(),
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.TrustRootID).To(Equal("test-root"))
	g.Expect(result.PredicateType).To(Equal(testPredicateType))
	g.Expect(result.BuilderID).To(Equal(testBuilderID))
	g.Expect(result.SourceURI).To(Equal(testSourceURI))
	g.Expect(result.BuildType).To(Equal(testBuildType))
	g.Expect(result.SignerIssuer).To(Equal(testProvenanceIssuer))
	g.Expect(result.SignerIdentity).To(Equal(testProvenanceSubject))
	g.Expect(result.PolicyChecksum).To(Equal(fixture.policyChecksum))
	g.Expect(result.ExternalParametersChecksum).To(MatchRegexp(`^sha256:[0-9a-f]{64}$`))
}

func TestVerifySignedProvenanceFailsClosedForUntrustedWrongAndExpiredEvidence(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*testing.T, *provenanceFixture)
		artifact  string
		errorCode string
	}{
		{
			name: "untrusted root",
			mutate: func(t *testing.T, fixture *provenanceFixture) {
				other := newProvenanceFixture(t, validProvenanceStatement(t, testArtifactDigest("artifact")))
				fixture.trustedMaterial = other.trustedMaterial
			},
			errorCode: "signature_untrusted",
		},
		{
			name:      "wrong artifact subject",
			artifact:  testArtifactDigest("different"),
			errorCode: "signature_untrusted",
		},
		{
			name: "wrong predicate",
			mutate: func(t *testing.T, fixture *provenanceFixture) {
				fixture.policy.AllowedPredicateTypes = []string{"https://example.invalid/other"}
			},
			errorCode: "predicate_not_allowed",
		},
		{
			name: "wrong builder",
			mutate: func(t *testing.T, fixture *provenanceFixture) {
				fixture.policy.AllowedBuilders = []string{"https://example.invalid/other"}
			},
			errorCode: "builder_not_allowed",
		},
		{
			name: "wrong source",
			mutate: func(t *testing.T, fixture *provenanceFixture) {
				fixture.policy.AllowedSourcePrefixes = []string{"git+https://other.example.invalid/"}
			},
			errorCode: "source_not_allowed",
		},
		{
			name: "wrong build type",
			mutate: func(t *testing.T, fixture *provenanceFixture) {
				fixture.policy.AllowedBuildTypes = []string{"https://example.invalid/other"}
			},
			errorCode: "build_type_not_allowed",
		},
		{
			name: "wrong external parameters",
			mutate: func(t *testing.T, fixture *provenanceFixture) {
				fixture.policy.ExpectedExternalParameters = json.RawMessage(`{"release":false}`)
			},
			errorCode: "external_parameters_mismatch",
		},
		{
			name: "expired root",
			mutate: func(t *testing.T, fixture *provenanceFixture) {
				fixture.policy.TrustedRoots[0].ValidUntil = fixture.entityTime.Add(-time.Second)
			},
			errorCode: "trusted_root_expired",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			fixture := newProvenanceFixture(t, validProvenanceStatement(t, testArtifactDigest("artifact")))
			if tt.mutate != nil {
				tt.mutate(t, &fixture)
			}
			digest := tt.artifact
			if digest == "" {
				digest = testArtifactDigest("artifact")
			}
			result, err := verifySignedProvenance(
				context.Background(),
				fixture.policy,
				testProvenanceArtifact(digest),
				"sha256:"+strings.Repeat("b", 64),
				fixture.policyChecksum,
				fixture.policy.TrustedRoots[0],
				fixture.entity,
				fixture.trustedMaterial,
				verify.WithCurrentTime(),
			)
			g.Expect(result).To(Equal(ProvenanceVerificationResult{}))
			g.Expect(err).To(MatchError("provenance verification failed: " + tt.errorCode))
		})
	}
}

func TestSigstoreProvenanceVerifierRejectsMalformedAndOversizedWithoutNetwork(t *testing.T) {
	g := NewWithT(t)
	verifier := SigstoreProvenanceVerifier{}
	policy := ProvenancePolicy{
		Version: ProvenancePolicyVersion,
		TrustedRoots: []TrustRoot{{
			ID: "root", JSON: []byte(`{}`), ValidFrom: time.Now().Add(-time.Hour), ValidUntil: time.Now().Add(time.Hour),
		}},
		AllowedSignerIdentities:    []SignerIdentity{{Issuer: testProvenanceIssuer, Subject: testProvenanceSubject}},
		AllowedPredicateTypes:      []string{testPredicateType},
		AllowedBuilders:            []string{testBuilderID},
		AllowedSourcePrefixes:      []string{testSourcePrefix},
		AllowedBuildTypes:          []string{testBuildType},
		ExpectedExternalParameters: json.RawMessage(`{}`),
	}
	artifact := testProvenanceArtifact(testArtifactDigest("artifact"))

	_, err := verifier.Verify(context.Background(), policy, artifact, ComponentReleaseEvidence{
		Reference: "oci://evidence", TrustRootID: "root", BundleJSON: []byte(`{not-json`),
	})
	g.Expect(err).To(MatchError("provenance verification failed: trusted_root_malformed"))

	_, err = verifier.Verify(context.Background(), policy, artifact, ComponentReleaseEvidence{
		Reference: "oci://evidence", TrustRootID: "root", BundleJSON: make([]byte, maxProvenanceBundleBytes+1),
	})
	g.Expect(err).To(MatchError("provenance verification failed: evidence_oversized"))
}

func TestSigstoreProvenanceVerifierRejectsMalformedEvidenceWithValidFrozenRoot(t *testing.T) {
	g := NewWithT(t)
	fixture := newProvenanceFixture(t, validProvenanceStatement(t, testArtifactDigest("artifact")))

	_, err := (SigstoreProvenanceVerifier{}).Verify(
		context.Background(),
		fixture.policy,
		testProvenanceArtifact(testArtifactDigest("artifact")),
		ComponentReleaseEvidence{
			Reference:   "oci://evidence/provenance",
			TrustRootID: fixture.policy.TrustedRoots[0].ID,
			BundleJSON:  []byte(`{not-json`),
		},
	)

	g.Expect(err).To(MatchError("provenance verification failed: evidence_malformed"))
}

func TestSigstoreProvenanceVerifierAcceptsFrozenKeyfulPolicy(t *testing.T) {
	g := NewWithT(t)
	fixture := newKeyfulProvenanceFixture(t, testArtifactDigest("artifact"))

	result, err := (SigstoreProvenanceVerifier{}).Verify(
		context.Background(),
		fixture.policy,
		testProvenanceArtifact(testArtifactDigest("artifact")),
		fixture.evidence,
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.VerificationMode).To(Equal(ProvenanceVerificationModeKeyful))
	g.Expect(result.KeyID).To(Equal(fixture.policy.TrustedRoots[0].ID))
	g.Expect(result.KeyFingerprint).To(Equal(fixture.fingerprint))
	g.Expect(result.TrustRootID).To(Equal(fixture.policy.TrustedRoots[0].ID))
	g.Expect(result.SignerIssuer).To(Equal("keyid:release-key-1"))
	g.Expect(result.SignerIdentity).To(Equal(fixture.fingerprint))
	g.Expect(result.PolicyChecksum).To(MatchRegexp(`^sha256:[0-9a-f]{64}$`))
}

func TestVerifyComponentReleasePublicationRecordsKeyfulModeAndIdentity(t *testing.T) {
	g := NewWithT(t)
	bundle := componentPublicationBundle()
	fixture := newKeyfulProvenanceFixture(t, testArtifactDigest("artifact"))

	facts, result := VerifyComponentReleasePublication(
		context.Background(),
		bundle,
		&PublicationProvenance{
			Policy: fixture.policy,
			Evidence: []PublicationProvenanceEvidence{{
				ArtifactKey: "service", Platform: "linux/amd64", Evidence: fixture.evidence,
			}},
		},
		SigstoreProvenanceVerifier{},
	)

	g.Expect(result.Valid).To(BeTrue(), result.Errors)
	g.Expect(facts).To(HaveLen(1))
	g.Expect(facts[0].VerificationMode).To(Equal(ProvenanceVerificationModeKeyful))
	g.Expect(facts[0].TrustRootID).To(Equal("release-key-1"))
	g.Expect(facts[0].KeyID).To(Equal("release-key-1"))
	g.Expect(facts[0].KeyFingerprint).To(Equal(fixture.fingerprint))
}

func TestSigstoreProvenanceVerifierRejectsKeyfulSubstitutionUnknownKeyWrongDigestAndMalformedSignature(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*testing.T, *keyfulProvenanceFixture)
		digest    string
		errorCode string
	}{
		{
			name: "unknown key id",
			mutate: func(_ *testing.T, fixture *keyfulProvenanceFixture) {
				fixture.evidence.TrustRootID = "unknown-key"
			},
			errorCode: "trusted_key_unknown",
		},
		{
			name: "substituted configured key",
			mutate: func(t *testing.T, fixture *keyfulProvenanceFixture) {
				replacement := newKeyfulProvenanceFixture(t, testArtifactDigest("artifact"))
				fixture.policy.TrustedRoots[0].JSON = replacement.policy.TrustedRoots[0].JSON
				fixture.policy.AllowedSignerIdentities[0].Subject = replacement.fingerprint
			},
			errorCode: "key_substitution",
		},
		{
			name:      "wrong artifact digest",
			digest:    testArtifactDigest("different"),
			errorCode: "signature_untrusted",
		},
		{
			name: "malformed signature",
			mutate: func(t *testing.T, fixture *keyfulProvenanceFixture) {
				var document map[string]any
				g := NewWithT(t)
				g.Expect(json.Unmarshal(fixture.evidence.BundleJSON, &document)).To(Succeed())
				envelope := document["dsseEnvelope"].(map[string]any)
				signatures := envelope["signatures"].([]any)
				signatures[0].(map[string]any)["sig"] = []byte("not-an-ecdsa-signature")
				encoded, err := json.Marshal(document)
				g.Expect(err).NotTo(HaveOccurred())
				fixture.evidence.BundleJSON = encoded
			},
			errorCode: "signature_untrusted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			fixture := newKeyfulProvenanceFixture(t, testArtifactDigest("artifact"))
			if tt.mutate != nil {
				tt.mutate(t, &fixture)
			}
			digest := tt.digest
			if digest == "" {
				digest = testArtifactDigest("artifact")
			}

			result, err := (SigstoreProvenanceVerifier{}).Verify(
				context.Background(), fixture.policy, testProvenanceArtifact(digest), fixture.evidence,
			)

			g.Expect(result).To(Equal(ProvenanceVerificationResult{}))
			g.Expect(err).To(MatchError("provenance verification failed: " + tt.errorCode))
		})
	}
}

func TestKeyfulProvenancePolicyRejectsFingerprintOrMediaTypeMismatchAndChangesChecksumWithKeyIdentity(t *testing.T) {
	g := NewWithT(t)
	fixture := newKeyfulProvenanceFixture(t, testArtifactDigest("artifact"))

	original, err := provenancePolicyChecksum(fixture.policy)
	g.Expect(err).NotTo(HaveOccurred())
	changed := fixture.policy
	replacement := newKeyfulProvenanceFixture(t, testArtifactDigest("artifact"))
	changed.TrustedRoots = append([]TrustRoot(nil), fixture.policy.TrustedRoots...)
	changed.TrustedRoots[0].JSON = replacement.policy.TrustedRoots[0].JSON
	changed.AllowedSignerIdentities = append([]SignerIdentity(nil), fixture.policy.AllowedSignerIdentities...)
	changed.AllowedSignerIdentities[0].Subject = replacement.fingerprint
	changedChecksum, err := provenancePolicyChecksum(changed)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(changedChecksum).NotTo(Equal(original))

	var rootDocument map[string]any
	g.Expect(json.Unmarshal(fixture.policy.TrustedRoots[0].JSON, &rootDocument)).To(Succeed())
	rootDocument["fingerprint"] = "sha256:" + strings.Repeat("f", 64)
	fixture.policy.TrustedRoots[0].JSON, err = json.Marshal(rootDocument)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = (SigstoreProvenanceVerifier{}).Verify(
		context.Background(), fixture.policy, testProvenanceArtifact(testArtifactDigest("artifact")), fixture.evidence,
	)
	g.Expect(err).To(MatchError("provenance verification failed: policy_invalid"))

	fixture = newKeyfulProvenanceFixture(t, testArtifactDigest("artifact"))
	g.Expect(json.Unmarshal(fixture.policy.TrustedRoots[0].JSON, &rootDocument)).To(Succeed())
	rootDocument["mediaType"] = "application/vnd.example.cosign.public-key.v1+json"
	fixture.policy.TrustedRoots[0].JSON, err = json.Marshal(rootDocument)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = (SigstoreProvenanceVerifier{}).Verify(
		context.Background(), fixture.policy, testProvenanceArtifact(testArtifactDigest("artifact")), fixture.evidence,
	)
	g.Expect(err).To(MatchError("provenance verification failed: policy_invalid"))
}

func TestVerifySignedProvenanceRejectsDuplicateSignedStatementMembers(t *testing.T) {
	g := NewWithT(t)
	statement := string(validProvenanceStatement(t, testArtifactDigest("artifact")))
	statement = strings.Replace(
		statement,
		`"predicateType":`,
		`"predicateType":"https://example.invalid/ambiguous","predicateType":`,
		1,
	)
	fixture := newProvenanceFixture(t, []byte(statement))

	_, err := verifySignedProvenance(
		context.Background(),
		fixture.policy,
		testProvenanceArtifact(testArtifactDigest("artifact")),
		"sha256:"+strings.Repeat("a", 64),
		fixture.policyChecksum,
		fixture.policy.TrustedRoots[0],
		fixture.entity,
		fixture.trustedMaterial,
		verify.WithCurrentTime(),
	)

	g.Expect(err).To(MatchError("provenance verification failed: statement_malformed"))
}

func TestCanonicalJSONRejectsTrailingOrNonObjectExternalParameters(t *testing.T) {
	g := NewWithT(t)

	_, err := canonicalJSON(json.RawMessage(`{"release":true} trailing`))
	g.Expect(err).To(HaveOccurred())
	_, err = canonicalJSON(json.RawMessage(`{"release":true,"release":false}`))
	g.Expect(err).To(HaveOccurred())
	_, err = canonicalExternalParameters(json.RawMessage(`["not","an","object"]`))
	g.Expect(err).To(HaveOccurred())
}

func TestVerifyComponentReleasePublicationRequiresEveryPlatformAndReturnsBoundedFacts(t *testing.T) {
	g := NewWithT(t)
	bundle := componentPublicationBundle()
	verifiedAt := time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC)
	fake := provenanceVerifierFunc(func(
		_ context.Context,
		_ ProvenancePolicy,
		artifact ProvenanceArtifact,
		evidence ComponentReleaseEvidence,
	) (ProvenanceVerificationResult, error) {
		return ProvenanceVerificationResult{
			EvidenceDigest:             "sha256:" + strings.Repeat("1", 64),
			PolicyChecksum:             "sha256:" + strings.Repeat("2", 64),
			VerificationMode:           ProvenanceVerificationModeKeyless,
			TrustRootID:                evidence.TrustRootID,
			PredicateType:              testPredicateType,
			BuilderID:                  testBuilderID,
			BuildID:                    testBuildID,
			SourceURI:                  testSourceURI,
			SourceCommit:               testSourceCommit,
			BuildType:                  testBuildType,
			ExternalParametersChecksum: "sha256:" + strings.Repeat("3", 64),
			SignerIssuer:               testProvenanceIssuer,
			SignerIdentity:             testProvenanceSubject,
			VerifiedAt:                 verifiedAt,
		}, nil
	})

	facts, result := VerifyComponentReleasePublication(context.Background(), bundle, &PublicationProvenance{
		Evidence: []PublicationProvenanceEvidence{{
			ArtifactKey: "service",
			Platform:    "linux/amd64",
			Evidence: ComponentReleaseEvidence{
				Reference: "oci://evidence/provenance", TrustRootID: "root",
			},
		}},
	}, fake)
	g.Expect(result.Valid).To(BeTrue())
	g.Expect(facts).To(HaveLen(1))
	g.Expect(facts[0].ReleaseBundleID).To(Equal(bundle.ID))
	g.Expect(facts[0].ArtifactDigest).To(Equal(testArtifactDigest("artifact")))
	g.Expect(facts[0].SourceCommit).To(Equal(testSourceCommit))
	g.Expect(facts[0].BuildID).To(Equal(testBuildID))
	g.Expect(facts[0].VerifiedAt).To(Equal(verifiedAt))

	facts, result = VerifyComponentReleasePublication(context.Background(), bundle, nil, fake)
	g.Expect(facts).To(BeNil())
	g.Expect(result.Valid).To(BeFalse())
	g.Expect(result.Errors).To(ContainElement(ValidationIssue{
		Field: "releaseContract.evidence.provenance", Rule: "verified",
		Message: "signed provenance verification is required before component publication",
	}))

	facts, result = VerifyComponentReleasePublication(context.Background(), bundle, &PublicationProvenance{
		Evidence: []PublicationProvenanceEvidence{
			{
				ArtifactKey: "service",
				Platform:    "linux/amd64",
				Evidence: ComponentReleaseEvidence{
					Reference: "oci://evidence/provenance", TrustRootID: "root",
				},
			},
			{
				ArtifactKey: "unexpected",
				Platform:    "linux/amd64",
				Evidence: ComponentReleaseEvidence{
					Reference: "oci://evidence/provenance", TrustRootID: "root",
				},
			},
		},
	}, fake)
	g.Expect(facts).To(BeNil())
	g.Expect(result.Valid).To(BeFalse())
}

func TestProvenancePreflightRequiresExactArtifactDigestAndPolicy(t *testing.T) {
	g := NewWithT(t)
	artifact := ProvenanceArtifact{
		Key:              "service",
		Platform:         "linux/amd64",
		Digest:           testArtifactDigest("artifact"),
		SourceRepository: testSourceURI,
		SourceCommit:     testSourceCommit,
		BuildID:          testBuildID,
		BuilderID:        testBuilderID,
	}
	fact := types.EvidenceVerification{
		ArtifactKey: artifact.Key, Platform: artifact.Platform, ArtifactDigest: artifact.Digest,
		PolicyChecksum: "sha256:" + strings.Repeat("4", 64),
		SourceURI:      artifact.SourceRepository,
		SourceCommit:   artifact.SourceCommit,
		BuildID:        artifact.BuildID,
		BuilderID:      artifact.BuilderID,
	}
	g.Expect(ProvenancePreflight([]ProvenanceArtifact{artifact}, []types.EvidenceVerification{fact}, fact.PolicyChecksum).Valid).To(BeTrue())
	fact.ArtifactDigest = testArtifactDigest("tampered")
	g.Expect(ProvenancePreflight([]ProvenanceArtifact{artifact}, []types.EvidenceVerification{fact}, fact.PolicyChecksum).Valid).To(BeFalse())

	fact.ArtifactDigest = artifact.Digest
	fact.SourceCommit = strings.Repeat("f", 40)
	g.Expect(ProvenancePreflight([]ProvenanceArtifact{artifact}, []types.EvidenceVerification{fact}, fact.PolicyChecksum).Valid).To(BeFalse())

	fact.SourceCommit = artifact.SourceCommit
	fact.BuildID = "build-43"
	g.Expect(ProvenancePreflight([]ProvenanceArtifact{artifact}, []types.EvidenceVerification{fact}, fact.PolicyChecksum).Valid).To(BeFalse())

	fact.BuildID = artifact.BuildID
	fact.SourceURI = "git+https://code.example.invalid/platform/other"
	g.Expect(ProvenancePreflight([]ProvenanceArtifact{artifact}, []types.EvidenceVerification{fact}, fact.PolicyChecksum).Valid).To(BeFalse())

	fact.SourceURI = artifact.SourceRepository
	fact.BuilderID = "https://builder.example.invalid/other"
	g.Expect(ProvenancePreflight([]ProvenanceArtifact{artifact}, []types.EvidenceVerification{fact}, fact.PolicyChecksum).Valid).To(BeFalse())

	fact.BuilderID = artifact.BuilderID
	g.Expect(ProvenancePreflight(
		[]ProvenanceArtifact{artifact},
		[]types.EvidenceVerification{fact, fact},
		fact.PolicyChecksum,
	).Valid).To(BeFalse())

	extra := fact
	extra.ArtifactKey = "unexpected"
	g.Expect(ProvenancePreflight(
		[]ProvenanceArtifact{artifact},
		[]types.EvidenceVerification{fact, extra},
		fact.PolicyChecksum,
	).Valid).To(BeFalse())
}

func TestVerifySignedProvenanceBindsExactReleaseSourceAndBuildFacts(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*ProvenanceArtifact)
		errorCode string
	}{
		{
			name: "repository mismatch",
			mutate: func(artifact *ProvenanceArtifact) {
				artifact.SourceRepository = "git+https://code.example.invalid/platform/other"
			},
			errorCode: "source_dependency_mismatch",
		},
		{
			name: "commit mismatch",
			mutate: func(artifact *ProvenanceArtifact) {
				artifact.SourceCommit = strings.Repeat("f", 40)
			},
			errorCode: "source_dependency_mismatch",
		},
		{
			name: "invocation mismatch",
			mutate: func(artifact *ProvenanceArtifact) {
				artifact.BuildID = "build-43"
			},
			errorCode: "build_id_mismatch",
		},
		{
			name: "builder mismatch",
			mutate: func(artifact *ProvenanceArtifact) {
				artifact.BuilderID = "https://build.example.invalid/workers/other"
			},
			errorCode: "builder_mismatch",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			fixture := newProvenanceFixture(t, validProvenanceStatement(t, testArtifactDigest("artifact")))
			artifact := testProvenanceArtifact(testArtifactDigest("artifact"))
			tt.mutate(&artifact)

			_, err := verifySignedProvenance(
				context.Background(),
				fixture.policy,
				artifact,
				"sha256:"+strings.Repeat("a", 64),
				fixture.policyChecksum,
				fixture.policy.TrustedRoots[0],
				fixture.entity,
				fixture.trustedMaterial,
				verify.WithCurrentTime(),
			)

			g.Expect(err).To(MatchError("provenance verification failed: " + tt.errorCode))
		})
	}
}

func TestValidateProvenanceStatementRejectsMissingReleaseBindings(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*verifiedStatement)
		errorCode string
	}{
		{
			name: "source dependency",
			mutate: func(statement *verifiedStatement) {
				statement.Predicate.BuildDefinition.ResolvedDependencies = nil
			},
			errorCode: "source_dependency_mismatch",
		},
		{
			name: "invocation id",
			mutate: func(statement *verifiedStatement) {
				statement.Predicate.RunDetails.Metadata.InvocationID = ""
			},
			errorCode: "build_id_mismatch",
		},
		{
			name: "builder id",
			mutate: func(statement *verifiedStatement) {
				statement.Predicate.RunDetails.Builder.ID = ""
			},
			errorCode: "builder_mismatch",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			var statement verifiedStatement
			g.Expect(json.Unmarshal(
				validProvenanceStatement(t, testArtifactDigest("artifact")),
				&statement,
			)).To(Succeed())
			tt.mutate(&statement)

			_, _, err := validateProvenanceStatement(
				newProvenanceFixture(t, validProvenanceStatement(t, testArtifactDigest("artifact"))).policy,
				testProvenanceArtifact(testArtifactDigest("artifact")),
				statement,
			)

			g.Expect(err).To(MatchError("provenance verification failed: " + tt.errorCode))
		})
	}
}

type provenanceFixture struct {
	entity          *bundle.Bundle
	entityTime      time.Time
	policy          ProvenancePolicy
	policyChecksum  string
	trustedMaterial root.TrustedMaterial
}

type keyfulProvenanceFixture struct {
	policy      ProvenancePolicy
	evidence    ComponentReleaseEvidence
	fingerprint string
}

func newKeyfulProvenanceFixture(t *testing.T, artifactDigest string) keyfulProvenanceFixture {
	t.Helper()
	g := NewWithT(t)
	now := time.Now().UTC()
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	g.Expect(err).NotTo(HaveOccurred())
	publicKeyDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	g.Expect(err).NotTo(HaveOccurred())
	fingerprintBytes := sha256.Sum256(publicKeyDER)
	fingerprint := "sha256:" + hex.EncodeToString(fingerprintBytes[:])
	hint := base64.StdEncoding.EncodeToString(fingerprintBytes[:])
	publicKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicKeyDER})
	trustedRootJSON, err := json.Marshal(map[string]any{
		"mediaType": KeyfulProvenanceTrustRootMediaType,
		"keyId":     "release-key-1", "fingerprint": fingerprint, "publicKeyPem": string(publicKeyPEM),
	})
	g.Expect(err).NotTo(HaveOccurred())
	statement := validProvenanceStatement(t, artifactDigest)
	preAuthEncoding := []byte(fmt.Sprintf(
		"DSSEv1 %d %s %d %s",
		len(bundle.IntotoMediaType),
		bundle.IntotoMediaType,
		len(statement),
		statement,
	))
	preAuthDigest := sha256.Sum256(preAuthEncoding)
	signature, err := ecdsa.SignASN1(rand.Reader, privateKey, preAuthDigest[:])
	g.Expect(err).NotTo(HaveOccurred())
	bundleDocument := map[string]any{
		"mediaType": "application/vnd.dev.sigstore.bundle.v0.3+json",
		"verificationMaterial": map[string]any{
			"publicKey": map[string]any{"hint": hint},
		},
		"dsseEnvelope": map[string]any{
			"payload":     statement,
			"payloadType": bundle.IntotoMediaType,
			"signatures":  []any{map[string]any{"sig": signature}},
		},
	}
	bundleJSON, err := json.Marshal(bundleDocument)
	g.Expect(err).NotTo(HaveOccurred())
	policy := ProvenancePolicy{
		Version:          KeyfulProvenancePolicyVersion,
		VerificationMode: ProvenanceVerificationModeKeyful,
		TrustedRoots: []TrustRoot{{
			ID: "release-key-1", JSON: trustedRootJSON,
			ValidFrom: now.Add(-time.Hour), ValidUntil: now.Add(time.Hour),
		}},
		AllowedSignerIdentities:    []SignerIdentity{{Issuer: "keyid:release-key-1", Subject: fingerprint}},
		AllowedPredicateTypes:      []string{testPredicateType},
		AllowedBuilders:            []string{testBuilderID},
		AllowedSourcePrefixes:      []string{testSourcePrefix},
		AllowedBuildTypes:          []string{testBuildType},
		ExpectedExternalParameters: json.RawMessage(`{"release":true,"target":"container"}`),
	}
	return keyfulProvenanceFixture{
		policy: policy, fingerprint: fingerprint,
		evidence: ComponentReleaseEvidence{
			Reference: "oci://evidence/provenance", TrustRootID: "release-key-1", BundleJSON: bundleJSON,
		},
	}
}

func newProvenanceFixture(t *testing.T, statement []byte) provenanceFixture {
	t.Helper()
	g := NewWithT(t)
	now := time.Now().UTC()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	g.Expect(err).NotTo(HaveOccurred())
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-provenance-root"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	g.Expect(err).NotTo(HaveOccurred())
	caCertificate, err := x509.ParseCertificate(caDER)
	g.Expect(err).NotTo(HaveOccurred())
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	g.Expect(err).NotTo(HaveOccurred())
	leafTemplate := &x509.Certificate{
		SerialNumber:   big.NewInt(2),
		Subject:        pkix.Name{CommonName: "test-provenance-builder"},
		EmailAddresses: []string{testProvenanceSubject},
		NotBefore:      now.Add(-time.Minute),
		NotAfter:       now.Add(time.Hour),
		KeyUsage:       x509.KeyUsageDigitalSignature,
		ExtKeyUsage:    []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},
		ExtraExtensions: []pkix.Extension{{
			Id: asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 1}, Value: []byte(testProvenanceIssuer),
		}},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, caCertificate, &leafKey.PublicKey, caKey)
	g.Expect(err).NotTo(HaveOccurred())
	trusted, err := root.NewTrustedRoot(root.TrustedRootMediaType01, []root.CertificateAuthority{
		&root.FulcioCertificateAuthority{
			Root: caCertificate, ValidityPeriodStart: now.Add(-time.Hour), ValidityPeriodEnd: now.Add(time.Hour),
			URI: "https://fulcio.example.invalid",
		},
	}, nil, nil, nil)
	g.Expect(err).NotTo(HaveOccurred())
	rootJSON, err := trusted.MarshalJSON()
	g.Expect(err).NotTo(HaveOccurred())
	preAuthEncoding := []byte(fmt.Sprintf(
		"DSSEv1 %d %s %d %s",
		len(bundle.IntotoMediaType),
		bundle.IntotoMediaType,
		len(statement),
		statement,
	))
	preAuthDigest := sha256.Sum256(preAuthEncoding)
	signature, err := ecdsa.SignASN1(rand.Reader, leafKey, preAuthDigest[:])
	g.Expect(err).NotTo(HaveOccurred())
	bundleDocument := map[string]any{
		"mediaType": "application/vnd.dev.sigstore.bundle.v0.3+json",
		"verificationMaterial": map[string]any{
			"certificate": map[string]any{"rawBytes": leafDER},
		},
		"dsseEnvelope": map[string]any{
			"payload":     statement,
			"payloadType": bundle.IntotoMediaType,
			"signatures":  []any{map[string]any{"sig": signature}},
		},
	}
	bundleJSON, err := json.Marshal(bundleDocument)
	g.Expect(err).NotTo(HaveOccurred())
	var entity bundle.Bundle
	g.Expect(entity.UnmarshalJSON(bundleJSON)).To(Succeed())
	trustedMaterial, err := root.NewTrustedRootFromJSON(rootJSON)
	g.Expect(err).NotTo(HaveOccurred())
	policy := ProvenancePolicy{
		Version: ProvenancePolicyVersion,
		TrustedRoots: []TrustRoot{{
			ID: "test-root", JSON: rootJSON, ValidFrom: now.Add(-time.Hour), ValidUntil: now.Add(time.Hour),
		}},
		AllowedSignerIdentities:    []SignerIdentity{{Issuer: testProvenanceIssuer, Subject: testProvenanceSubject}},
		AllowedPredicateTypes:      []string{testPredicateType},
		AllowedBuilders:            []string{testBuilderID},
		AllowedSourcePrefixes:      []string{testSourcePrefix},
		AllowedBuildTypes:          []string{testBuildType},
		ExpectedExternalParameters: json.RawMessage(`{"release":true,"target":"container"}`),
	}
	checksum, err := provenancePolicyChecksum(policy)
	g.Expect(err).NotTo(HaveOccurred())
	return provenanceFixture{
		entity: &entity, entityTime: now, policy: policy, policyChecksum: checksum, trustedMaterial: trustedMaterial,
	}
}

func validProvenanceStatement(t *testing.T, digest string) []byte {
	t.Helper()
	document := map[string]any{
		"_type": "https://in-toto.io/Statement/v1",
		"subject": []any{map[string]any{
			"name": "service", "digest": map[string]string{"sha256": strings.TrimPrefix(digest, "sha256:")},
		}},
		"predicateType": testPredicateType,
		"predicate": map[string]any{
			"buildDefinition": map[string]any{
				"buildType":          testBuildType,
				"externalParameters": map[string]any{"target": "container", "release": true},
				"resolvedDependencies": []any{map[string]any{
					"uri": testSourceURI, "digest": map[string]string{"gitCommit": testSourceCommit},
				}},
			},
			"runDetails": map[string]any{
				"builder": map[string]any{"id": testBuilderID},
				"metadata": map[string]any{
					"invocationId": testBuildID,
				},
			},
		},
	}
	encoded, err := json.Marshal(document)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return encoded
}

func testArtifactDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func componentPublicationBundle() types.ReleaseBundle {
	digest := testArtifactDigest("artifact")
	return types.ReleaseBundle{
		ID:             uuid.New(),
		OrganizationID: uuid.New(),
		Kind:           types.ReleaseBundleKindComponent,
		ReleaseContract: &types.ReleaseContract{ComponentV2: &types.ComponentReleaseContractV2{
			Schema:       types.ReleaseContractSchemaV2,
			ComponentKey: "service",
			Version:      "1.2.3",
			Source: types.ComponentReleaseSource{
				Repository: testSourceURI, RequestedRef: "refs/heads/main", Commit: testSourceCommit,
			},
			Build: types.ComponentReleaseBuild{ID: testBuildID, Builder: testBuilderID},
			Artifacts: []types.ComponentReleaseArtifact{{
				Key: "service", Digest: digest,
				Platforms: []types.ComponentReleasePlatform{{Platform: "linux/amd64", Digest: digest}},
			}},
			Evidence: types.ComponentReleaseEvidenceReferences{
				Provenance: []string{"oci://evidence/provenance"},
			},
		}},
	}
}

func testProvenanceArtifact(digest string) ProvenanceArtifact {
	return ProvenanceArtifact{
		Key:              "service",
		Platform:         "linux/amd64",
		Digest:           digest,
		SourceRepository: testSourceURI,
		SourceCommit:     testSourceCommit,
		BuildID:          testBuildID,
		BuilderID:        testBuilderID,
	}
}

type provenanceVerifierFunc func(
	context.Context,
	ProvenancePolicy,
	ProvenanceArtifact,
	ComponentReleaseEvidence,
) (ProvenanceVerificationResult, error)

func (f provenanceVerifierFunc) Verify(
	ctx context.Context,
	policy ProvenancePolicy,
	artifact ProvenanceArtifact,
	evidence ComponentReleaseEvidence,
) (ProvenanceVerificationResult, error) {
	return f(ctx, policy, artifact, evidence)
}
