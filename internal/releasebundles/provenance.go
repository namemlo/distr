package releasebundles

import (
	"bytes"
	"context"
	"crypto"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/distr-sh/distr/internal/types"
	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/verify"
	"github.com/sigstore/sigstore/pkg/signature"
)

const (
	ProvenancePolicyVersion            = "distr.provenance-policy/v1"
	KeyfulProvenancePolicyVersion      = "distr.provenance-policy/v2"
	KeyfulProvenanceTrustRootMediaType = "application/vnd.distr.cosign.public-key.v1+json"
	ProvenanceVerificationModeKeyless  = "keyless"
	ProvenanceVerificationModeKeyful   = "keyful"
	maxProvenanceBundleBytes           = 4 << 20
	maxTrustedRootBytes                = 1 << 20
	maxTrustedPublicKeyBytes           = 64 << 10
	maxPolicyValues                    = 64
	maxProvenanceTextBytes             = 1024
	maxSourceURIBytes                  = 2048
	maxEvidenceReferenceBytes          = 2048
	maxArtifactKeyBytes                = 128
	maxTrustRootIDBytes                = 256
	maxExternalParametersBytes         = 1 << 20
	maxPublicationEvidence             = 128
	keyfulSignerIssuerPrefix           = "keyid:"
)

type TrustRoot struct {
	ID         string
	JSON       []byte
	ValidFrom  time.Time
	ValidUntil time.Time
}

type SignerIdentity struct {
	Issuer  string
	Subject string
}

// ProvenancePolicy is a complete, frozen verification policy. Callers supply
// the trusted-root bytes; verification never discovers or refreshes trust over
// the network.
type ProvenancePolicy struct {
	Version                    string
	VerificationMode           string
	TrustedRoots               []TrustRoot
	AllowedSignerIdentities    []SignerIdentity
	AllowedPredicateTypes      []string
	AllowedBuilders            []string
	AllowedSourcePrefixes      []string
	AllowedBuildTypes          []string
	ExpectedExternalParameters json.RawMessage
}

type ProvenanceArtifact struct {
	Key              string
	Platform         string
	Digest           string
	SourceRepository string
	SourceCommit     string
	BuildID          string
	BuilderID        string
}

type ComponentReleaseEvidence struct {
	Reference   string
	TrustRootID string
	BundleJSON  []byte
}

type ProvenanceVerificationResult struct {
	EvidenceDigest             string
	PolicyChecksum             string
	VerificationMode           string
	TrustRootID                string
	KeyID                      string
	KeyFingerprint             string
	PredicateType              string
	BuilderID                  string
	BuildID                    string
	SourceURI                  string
	SourceCommit               string
	BuildType                  string
	ExternalParametersChecksum string
	SignerIssuer               string
	SignerIdentity             string
	VerifiedAt                 time.Time
}

type ProvenanceVerifier interface {
	Verify(
		context.Context,
		ProvenancePolicy,
		ProvenanceArtifact,
		ComponentReleaseEvidence,
	) (ProvenanceVerificationResult, error)
}

type SigstoreProvenanceVerifier struct{}

type PublicationProvenanceEvidence struct {
	ArtifactKey string
	Platform    string
	Evidence    ComponentReleaseEvidence
}

type PublicationProvenance struct {
	Policy   ProvenancePolicy
	Evidence []PublicationProvenanceEvidence
}

type ProvenanceError struct {
	Code string
}

func (e *ProvenanceError) Error() string {
	return "provenance verification failed: " + e.Code
}

func provenanceError(code string) error {
	return &ProvenanceError{Code: code}
}

func (SigstoreProvenanceVerifier) Verify(
	ctx context.Context,
	policy ProvenancePolicy,
	artifact ProvenanceArtifact,
	evidence ComponentReleaseEvidence,
) (ProvenanceVerificationResult, error) {
	if err := ctx.Err(); err != nil {
		return ProvenanceVerificationResult{}, provenanceError("cancelled")
	}
	if len(evidence.BundleJSON) == 0 {
		return ProvenanceVerificationResult{}, provenanceError("evidence_missing")
	}
	if len(evidence.BundleJSON) > maxProvenanceBundleBytes {
		return ProvenanceVerificationResult{}, provenanceError("evidence_oversized")
	}
	if policy.Version == KeyfulProvenancePolicyVersion {
		return verifyKeyfulProvenance(ctx, policy, artifact, evidence)
	}
	trustRoot, policyChecksum, err := validateAndSelectProvenancePolicy(policy, evidence.TrustRootID)
	if err != nil {
		return ProvenanceVerificationResult{}, err
	}
	if len(trustRoot.JSON) > maxTrustedRootBytes {
		return ProvenanceVerificationResult{}, provenanceError("trusted_root_oversized")
	}
	if err := ValidateProvenanceJSONDocument(trustRoot.JSON); err != nil {
		return ProvenanceVerificationResult{}, provenanceError("trusted_root_malformed")
	}
	trustedMaterial, err := root.NewTrustedRootFromJSON(trustRoot.JSON)
	if err != nil {
		return ProvenanceVerificationResult{}, provenanceError("trusted_root_malformed")
	}
	var signedBundle bundle.Bundle
	if err := ValidateProvenanceJSONDocument(evidence.BundleJSON); err != nil {
		return ProvenanceVerificationResult{}, provenanceError("evidence_malformed")
	}
	if err := signedBundle.UnmarshalJSON(evidence.BundleJSON); err != nil {
		return ProvenanceVerificationResult{}, provenanceError("evidence_malformed")
	}
	sum := sha256.Sum256(evidence.BundleJSON)
	return verifySignedProvenance(
		ctx,
		policy,
		artifact,
		"sha256:"+hex.EncodeToString(sum[:]),
		policyChecksum,
		trustRoot,
		&signedBundle,
		trustedMaterial,
	)
}

type provenanceVerificationTrust struct {
	mode            string
	trustMaterialID string
	keyID           string
	keyFingerprint  string
	keyHint         string
	validFrom       time.Time
	validUntil      time.Time
}

func verifyKeyfulProvenance(
	ctx context.Context,
	policy ProvenancePolicy,
	artifact ProvenanceArtifact,
	evidence ComponentReleaseEvidence,
) (ProvenanceVerificationResult, error) {
	trustedKey, publicKey, keyHint, keyFingerprint, policyChecksum, err := validateAndSelectKeyfulProvenancePolicy(
		policy,
		evidence.TrustRootID,
	)
	if err != nil {
		return ProvenanceVerificationResult{}, err
	}
	if err := ValidateProvenanceJSONDocument(evidence.BundleJSON); err != nil {
		return ProvenanceVerificationResult{}, provenanceError("evidence_malformed")
	}
	var signedBundle bundle.Bundle
	if err := signedBundle.UnmarshalJSON(evidence.BundleJSON); err != nil {
		return ProvenanceVerificationResult{}, provenanceError("evidence_malformed")
	}
	verificationContent, err := signedBundle.VerificationContent()
	if err != nil || verificationContent.PublicKey() == nil {
		return ProvenanceVerificationResult{}, provenanceError("key_signature_required")
	}
	if verificationContent.PublicKey().Hint() != keyHint {
		return ProvenanceVerificationResult{}, provenanceError("key_substitution")
	}
	publicKeyVerifier, err := signature.LoadDefaultVerifier(publicKey)
	if err != nil {
		return ProvenanceVerificationResult{}, provenanceError("trusted_key_invalid")
	}
	trustedMaterial := root.NewTrustedPublicKeyMaterialFromMapping(map[string]*root.ExpiringKey{
		keyHint: root.NewExpiringKey(publicKeyVerifier, trustedKey.ValidFrom, trustedKey.ValidUntil),
	})
	sum := sha256.Sum256(evidence.BundleJSON)
	return verifySignedProvenanceWithTrust(
		ctx,
		policy,
		artifact,
		"sha256:"+hex.EncodeToString(sum[:]),
		policyChecksum,
		provenanceVerificationTrust{
			mode: ProvenanceVerificationModeKeyful, trustMaterialID: trustedKey.ID,
			keyID: trustedKey.ID, keyFingerprint: keyFingerprint, keyHint: keyHint,
			validFrom: trustedKey.ValidFrom, validUntil: trustedKey.ValidUntil,
		},
		&signedBundle,
		trustedMaterial,
		verify.WithCurrentTime(),
	)
}

func verifySignedProvenance(
	ctx context.Context,
	policy ProvenancePolicy,
	artifact ProvenanceArtifact,
	evidenceDigest string,
	policyChecksum string,
	trustRoot TrustRoot,
	entity verify.SignedEntity,
	trustedMaterial root.TrustedMaterial,
	verifierOptions ...verify.VerifierOption,
) (ProvenanceVerificationResult, error) {
	return verifySignedProvenanceWithTrust(
		ctx,
		policy,
		artifact,
		evidenceDigest,
		policyChecksum,
		provenanceVerificationTrust{
			mode: ProvenanceVerificationModeKeyless, trustMaterialID: trustRoot.ID,
			validFrom: trustRoot.ValidFrom, validUntil: trustRoot.ValidUntil,
		},
		entity,
		trustedMaterial,
		verifierOptions...,
	)
}

func verifySignedProvenanceWithTrust(
	ctx context.Context,
	policy ProvenancePolicy,
	artifact ProvenanceArtifact,
	evidenceDigest string,
	policyChecksum string,
	trust provenanceVerificationTrust,
	entity verify.SignedEntity,
	trustedMaterial root.TrustedMaterial,
	verifierOptions ...verify.VerifierOption,
) (ProvenanceVerificationResult, error) {
	digestBytes, err := parseSHA256Digest(artifact.Digest)
	if err != nil || !safeProvenanceText(artifact.Key) || !safeProvenanceText(artifact.Platform) {
		return ProvenanceVerificationResult{}, provenanceError("artifact_invalid")
	}
	if len(verifierOptions) == 0 {
		verifierOptions = []verify.VerifierOption{
			verify.WithTransparencyLog(1),
			verify.WithObserverTimestamps(1),
		}
	}
	statementJSON, err := signedStatementJSON(entity)
	if err != nil {
		return ProvenanceVerificationResult{}, err
	}
	verifier, err := verify.NewVerifier(trustedMaterial, verifierOptions...)
	if err != nil {
		return ProvenanceVerificationResult{}, provenanceError("trusted_root_invalid")
	}
	policyOptions := make([]verify.PolicyOption, 0, len(policy.AllowedSignerIdentities)+1)
	if trust.mode == ProvenanceVerificationModeKeyful {
		policyOptions = append(policyOptions, verify.WithKey())
	} else {
		for _, identity := range policy.AllowedSignerIdentities {
			certificateIdentity, err := verify.NewShortCertificateIdentity(
				identity.Issuer,
				"",
				identity.Subject,
				"",
			)
			if err != nil {
				return ProvenanceVerificationResult{}, provenanceError("policy_invalid")
			}
			policyOptions = append(policyOptions, verify.WithCertificateIdentity(certificateIdentity))
		}
	}
	verification, err := verifier.Verify(
		entity,
		verify.NewPolicy(verify.WithArtifactDigest("sha256", digestBytes), policyOptions...),
	)
	if err != nil {
		return ProvenanceVerificationResult{}, provenanceError("signature_untrusted")
	}
	if err := ctx.Err(); err != nil {
		return ProvenanceVerificationResult{}, provenanceError("cancelled")
	}
	verifiedAt, ok := earliestVerifiedTimestamp(verification.VerifiedTimestamps)
	if !ok {
		return ProvenanceVerificationResult{}, provenanceError("timestamp_missing")
	}
	if !timestampsWithinValidity(verification.VerifiedTimestamps, trust.validFrom, trust.validUntil) {
		return ProvenanceVerificationResult{}, provenanceError("trusted_root_expired")
	}
	statement, err := decodeVerifiedStatement(statementJSON)
	if err != nil {
		return ProvenanceVerificationResult{}, err
	}
	sourceURI, externalChecksum, err := validateProvenanceStatement(policy, artifact, statement)
	if err != nil {
		return ProvenanceVerificationResult{}, err
	}
	signerIssuer, signerIdentity := "", ""
	if trust.mode == ProvenanceVerificationModeKeyful {
		if verification.Signature == nil || verification.Signature.PublicKeyID == nil ||
			string(*verification.Signature.PublicKeyID) != trust.keyHint {
			return ProvenanceVerificationResult{}, provenanceError("key_substitution")
		}
		signerIssuer = keyfulSignerIssuerPrefix + trust.keyID
		signerIdentity = trust.keyFingerprint
	} else if verification.VerifiedIdentity != nil {
		signerIssuer = verification.VerifiedIdentity.Issuer.Issuer
		signerIdentity = verification.VerifiedIdentity.SubjectAlternativeName.SubjectAlternativeName
	}
	return ProvenanceVerificationResult{
		EvidenceDigest:             evidenceDigest,
		PolicyChecksum:             policyChecksum,
		VerificationMode:           trust.mode,
		TrustRootID:                trust.trustMaterialID,
		KeyID:                      trust.keyID,
		KeyFingerprint:             trust.keyFingerprint,
		PredicateType:              statement.PredicateType,
		BuilderID:                  statement.Predicate.RunDetails.Builder.ID,
		BuildID:                    statement.Predicate.RunDetails.Metadata.InvocationID,
		SourceURI:                  sourceURI,
		SourceCommit:               artifact.SourceCommit,
		BuildType:                  statement.Predicate.BuildDefinition.BuildType,
		ExternalParametersChecksum: externalChecksum,
		SignerIssuer:               signerIssuer,
		SignerIdentity:             signerIdentity,
		VerifiedAt:                 verifiedAt.UTC(),
	}, nil
}

type verifiedStatement struct {
	Type    string `json:"_type"`
	Subject []struct {
		Name   string            `json:"name"`
		Digest map[string]string `json:"digest"`
	} `json:"subject"`
	PredicateType string `json:"predicateType"`
	Predicate     struct {
		BuildDefinition struct {
			BuildType            string          `json:"buildType"`
			ExternalParameters   json.RawMessage `json:"externalParameters"`
			ResolvedDependencies []struct {
				URI    string            `json:"uri"`
				Digest map[string]string `json:"digest"`
			} `json:"resolvedDependencies"`
		} `json:"buildDefinition"`
		RunDetails struct {
			Builder struct {
				ID string `json:"id"`
			} `json:"builder"`
			Metadata struct {
				InvocationID string `json:"invocationId"`
			} `json:"metadata"`
		} `json:"runDetails"`
	} `json:"predicate"`
}

func signedStatementJSON(entity verify.SignedEntity) ([]byte, error) {
	signature, err := entity.SignatureContent()
	if err != nil || signature == nil || signature.EnvelopeContent() == nil {
		return nil, provenanceError("statement_malformed")
	}
	envelope := signature.EnvelopeContent().RawEnvelope()
	if envelope == nil {
		return nil, provenanceError("statement_malformed")
	}
	raw, err := envelope.DecodeB64Payload()
	if err != nil || len(raw) == 0 || len(raw) > maxProvenanceBundleBytes {
		return nil, provenanceError("statement_malformed")
	}
	if err := ValidateProvenanceJSONDocument(raw); err != nil {
		return nil, provenanceError("statement_malformed")
	}
	return raw, nil
}

func decodeVerifiedStatement(raw []byte) (verifiedStatement, error) {
	var statement verifiedStatement
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&statement); err != nil {
		return verifiedStatement{}, provenanceError("statement_malformed")
	}
	return statement, nil
}

func validateProvenanceStatement(
	policy ProvenancePolicy,
	artifact ProvenanceArtifact,
	statement verifiedStatement,
) (string, string, error) {
	if statement.Type != "https://in-toto.io/Statement/v1" {
		return "", "", provenanceError("statement_type_not_allowed")
	}
	if !slices.Contains(policy.AllowedPredicateTypes, statement.PredicateType) {
		return "", "", provenanceError("predicate_not_allowed")
	}
	if !canonicalSourceURI(artifact.SourceRepository) ||
		!isLowerHexGitCommit(artifact.SourceCommit) ||
		!safeProvenanceText(artifact.BuildID) ||
		!safeProvenanceText(artifact.BuilderID) {
		return "", "", provenanceError("release_identity_invalid")
	}
	if statement.Predicate.RunDetails.Builder.ID != artifact.BuilderID {
		return "", "", provenanceError("builder_mismatch")
	}
	if !slices.Contains(policy.AllowedBuilders, artifact.BuilderID) {
		return "", "", provenanceError("builder_not_allowed")
	}
	if statement.Predicate.RunDetails.Metadata.InvocationID != artifact.BuildID {
		return "", "", provenanceError("build_id_mismatch")
	}
	if !slices.Contains(policy.AllowedBuildTypes, statement.Predicate.BuildDefinition.BuildType) {
		return "", "", provenanceError("build_type_not_allowed")
	}
	subjectMatches := false
	for _, subject := range statement.Subject {
		if "sha256:"+subject.Digest["sha256"] == artifact.Digest {
			subjectMatches = true
			break
		}
	}
	if !subjectMatches {
		return "", "", provenanceError("artifact_subject_mismatch")
	}
	sourceMatches := 0
	sourceAllowed := false
	for _, dependency := range statement.Predicate.BuildDefinition.ResolvedDependencies {
		if dependency.URI != artifact.SourceRepository ||
			dependency.Digest["gitCommit"] != artifact.SourceCommit {
			continue
		}
		sourceMatches++
		for _, prefix := range policy.AllowedSourcePrefixes {
			if canonicalSourcePrefix(prefix) && strings.HasPrefix(dependency.URI, prefix) {
				sourceAllowed = true
				break
			}
		}
	}
	if sourceMatches == 0 {
		return "", "", provenanceError("source_dependency_mismatch")
	}
	if sourceMatches != 1 {
		return "", "", provenanceError("source_ambiguous")
	}
	if !sourceAllowed {
		return "", "", provenanceError("source_not_allowed")
	}
	expected, err := canonicalExternalParameters(policy.ExpectedExternalParameters)
	if err != nil {
		return "", "", provenanceError("policy_invalid")
	}
	actual, err := canonicalExternalParameters(statement.Predicate.BuildDefinition.ExternalParameters)
	if err != nil || !bytes.Equal(expected, actual) {
		return "", "", provenanceError("external_parameters_mismatch")
	}
	sum := sha256.Sum256(actual)
	return artifact.SourceRepository, "sha256:" + hex.EncodeToString(sum[:]), nil
}

func validateCommonProvenancePolicy(policy ProvenancePolicy) error {
	lists := [][]string{
		policy.AllowedPredicateTypes,
		policy.AllowedBuilders,
		policy.AllowedBuildTypes,
	}
	for _, values := range lists {
		if len(values) == 0 || len(values) > maxPolicyValues {
			return provenanceError("policy_invalid")
		}
		seen := map[string]struct{}{}
		for _, value := range values {
			if !safeProvenanceText(value) {
				return provenanceError("policy_invalid")
			}
			if _, duplicate := seen[value]; duplicate {
				return provenanceError("policy_invalid")
			}
			seen[value] = struct{}{}
		}
	}
	if len(policy.AllowedSourcePrefixes) == 0 || len(policy.AllowedSourcePrefixes) > maxPolicyValues {
		return provenanceError("policy_invalid")
	}
	seenPrefixes := map[string]struct{}{}
	for _, prefix := range policy.AllowedSourcePrefixes {
		if !canonicalSourcePrefix(prefix) {
			return provenanceError("policy_invalid")
		}
		if _, duplicate := seenPrefixes[prefix]; duplicate {
			return provenanceError("policy_invalid")
		}
		seenPrefixes[prefix] = struct{}{}
	}
	if len(policy.ExpectedExternalParameters) > maxExternalParametersBytes {
		return provenanceError("policy_invalid")
	}
	if _, err := canonicalExternalParameters(policy.ExpectedExternalParameters); err != nil {
		return provenanceError("policy_invalid")
	}
	return nil
}

func validateAndSelectProvenancePolicy(policy ProvenancePolicy, trustRootID string) (TrustRoot, string, error) {
	if policy.Version != ProvenancePolicyVersion ||
		policy.VerificationMode != "" ||
		len(policy.TrustedRoots) == 0 ||
		len(policy.TrustedRoots) > maxPolicyValues ||
		len(policy.AllowedSignerIdentities) == 0 ||
		len(policy.AllowedSignerIdentities) > maxPolicyValues {
		return TrustRoot{}, "", provenanceError("policy_invalid")
	}
	if err := validateCommonProvenancePolicy(policy); err != nil {
		return TrustRoot{}, "", err
	}
	var selected TrustRoot
	seenRoots := map[string]struct{}{}
	totalRootBytes := 0
	for _, candidate := range policy.TrustedRoots {
		totalRootBytes += len(candidate.JSON)
		if !safeBoundedProvenanceText(candidate.ID, maxTrustRootIDBytes) ||
			len(candidate.JSON) == 0 ||
			(len(candidate.JSON) > maxTrustedRootBytes && candidate.ID != trustRootID) ||
			totalRootBytes > maxProvenanceBundleBytes ||
			candidate.ValidFrom.IsZero() ||
			candidate.ValidUntil.IsZero() ||
			!candidate.ValidFrom.Before(candidate.ValidUntil) {
			return TrustRoot{}, "", provenanceError("policy_invalid")
		}
		if _, exists := seenRoots[candidate.ID]; exists {
			return TrustRoot{}, "", provenanceError("policy_invalid")
		}
		seenRoots[candidate.ID] = struct{}{}
		if candidate.ID == trustRootID {
			selected = candidate
		} else {
			if err := ValidateProvenanceJSONDocument(candidate.JSON); err != nil {
				return TrustRoot{}, "", provenanceError("policy_invalid")
			}
			if _, err := root.NewTrustedRootFromJSON(candidate.JSON); err != nil {
				return TrustRoot{}, "", provenanceError("policy_invalid")
			}
		}
	}
	for _, identity := range policy.AllowedSignerIdentities {
		if !safeProvenanceText(identity.Issuer) || !safeProvenanceText(identity.Subject) ||
			strings.HasPrefix(identity.Issuer, keyfulSignerIssuerPrefix) {
			return TrustRoot{}, "", provenanceError("policy_invalid")
		}
	}
	seenIdentities := map[string]struct{}{}
	for _, identity := range policy.AllowedSignerIdentities {
		key := identity.Issuer + "\x00" + identity.Subject
		if _, duplicate := seenIdentities[key]; duplicate {
			return TrustRoot{}, "", provenanceError("policy_invalid")
		}
		seenIdentities[key] = struct{}{}
	}
	if selected.ID == "" {
		return TrustRoot{}, "", provenanceError("trusted_root_unknown")
	}
	checksum, err := provenancePolicyChecksum(policy)
	if err != nil {
		return TrustRoot{}, "", provenanceError("policy_invalid")
	}
	return selected, checksum, nil
}

func validateAndSelectKeyfulProvenancePolicy(
	policy ProvenancePolicy,
	keyID string,
) (TrustRoot, crypto.PublicKey, string, string, string, error) {
	if policy.Version != KeyfulProvenancePolicyVersion ||
		policy.VerificationMode != ProvenanceVerificationModeKeyful ||
		len(policy.TrustedRoots) == 0 ||
		len(policy.TrustedRoots) > maxPolicyValues ||
		len(policy.AllowedSignerIdentities) != len(policy.TrustedRoots) ||
		!safeBoundedProvenanceText(keyID, maxTrustRootIDBytes) {
		return TrustRoot{}, nil, "", "", "", provenanceError("policy_invalid")
	}
	if err := validateCommonProvenancePolicy(policy); err != nil {
		return TrustRoot{}, nil, "", "", "", err
	}
	var selected TrustRoot
	var selectedPublicKey crypto.PublicKey
	var selectedHint string
	var selectedFingerprint string
	seenIDs := map[string]struct{}{}
	seenFingerprints := map[string]struct{}{}
	totalRootBytes := 0
	for _, candidate := range policy.TrustedRoots {
		totalRootBytes += len(candidate.JSON)
		if !safeBoundedProvenanceText(candidate.ID, maxTrustRootIDBytes) ||
			len(candidate.JSON) == 0 ||
			len(candidate.JSON) > maxTrustedRootBytes ||
			totalRootBytes > maxProvenanceBundleBytes ||
			candidate.ValidFrom.IsZero() ||
			candidate.ValidUntil.IsZero() ||
			!candidate.ValidFrom.Before(candidate.ValidUntil) {
			return TrustRoot{}, nil, "", "", "", provenanceError("policy_invalid")
		}
		if _, duplicate := seenIDs[candidate.ID]; duplicate {
			return TrustRoot{}, nil, "", "", "", provenanceError("policy_invalid")
		}
		seenIDs[candidate.ID] = struct{}{}
		publicKeyPEM, declaredKeyID, declaredFingerprint, err := parseKeyfulTrustedRoot(candidate.JSON)
		if err != nil || declaredKeyID != candidate.ID {
			return TrustRoot{}, nil, "", "", "", provenanceError("policy_invalid")
		}
		publicKey, fingerprint, hint, err := parseTrustedPublicKey(publicKeyPEM)
		if err != nil || fingerprint != declaredFingerprint {
			return TrustRoot{}, nil, "", "", "", provenanceError("policy_invalid")
		}
		if _, duplicate := seenFingerprints[fingerprint]; duplicate {
			return TrustRoot{}, nil, "", "", "", provenanceError("policy_invalid")
		}
		seenFingerprints[fingerprint] = struct{}{}
		if !slices.Contains(policy.AllowedSignerIdentities, SignerIdentity{
			Issuer:  "keyid:" + candidate.ID,
			Subject: fingerprint,
		}) {
			return TrustRoot{}, nil, "", "", "", provenanceError("policy_invalid")
		}
		if candidate.ID == keyID {
			selected = candidate
			selectedPublicKey = publicKey
			selectedHint = hint
			selectedFingerprint = fingerprint
		}
	}
	if selected.ID == "" {
		return TrustRoot{}, nil, "", "", "", provenanceError("trusted_key_unknown")
	}
	checksum, err := provenancePolicyChecksum(policy)
	if err != nil {
		return TrustRoot{}, nil, "", "", "", provenanceError("policy_invalid")
	}
	return selected, selectedPublicKey, selectedHint, selectedFingerprint, checksum, nil
}

func parseKeyfulTrustedRoot(raw []byte) ([]byte, string, string, error) {
	if err := ValidateProvenanceJSONDocument(raw); err != nil {
		return nil, "", "", err
	}
	var document struct {
		MediaType    string `json:"mediaType"`
		KeyID        string `json:"keyId"`
		Fingerprint  string `json:"fingerprint"`
		PublicKeyPEM string `json:"publicKeyPem"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return nil, "", "", err
	}
	if !validKeyfulTrustedRootMediaType(document.MediaType) ||
		!safeBoundedProvenanceText(document.KeyID, maxTrustRootIDBytes) ||
		!IsSHA256Digest(document.Fingerprint) ||
		len(document.PublicKeyPEM) == 0 || len(document.PublicKeyPEM) > maxTrustedPublicKeyBytes {
		return nil, "", "", fmt.Errorf("keyful trusted root is invalid")
	}
	return []byte(document.PublicKeyPEM), document.KeyID, document.Fingerprint, nil
}

func validKeyfulTrustedRootMediaType(value string) bool {
	return value == KeyfulProvenanceTrustRootMediaType
}

func parseTrustedPublicKey(publicKeyPEM []byte) (crypto.PublicKey, string, string, error) {
	block, rest := pem.Decode(publicKeyPEM)
	if block == nil || block.Type != "PUBLIC KEY" || len(bytes.TrimSpace(rest)) != 0 {
		return nil, "", "", fmt.Errorf("public key PEM is invalid")
	}
	publicKey, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, "", "", err
	}
	canonicalDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return nil, "", "", err
	}
	sum := sha256.Sum256(canonicalDER)
	return publicKey,
		"sha256:" + hex.EncodeToString(sum[:]),
		base64.StdEncoding.EncodeToString(sum[:]),
		nil
}

func provenancePolicyChecksum(policy ProvenancePolicy) (string, error) {
	if policy.Version == KeyfulProvenancePolicyVersion {
		return keyfulProvenancePolicyChecksum(policy)
	}
	type rootChecksum struct {
		ID         string `json:"id"`
		Digest     string `json:"digest"`
		ValidFrom  string `json:"validFrom"`
		ValidUntil string `json:"validUntil"`
	}
	roots := make([]rootChecksum, 0, len(policy.TrustedRoots))
	for _, trustRoot := range policy.TrustedRoots {
		sum := sha256.Sum256(trustRoot.JSON)
		roots = append(roots, rootChecksum{
			ID:         trustRoot.ID,
			Digest:     "sha256:" + hex.EncodeToString(sum[:]),
			ValidFrom:  trustRoot.ValidFrom.UTC().Format(time.RFC3339Nano),
			ValidUntil: trustRoot.ValidUntil.UTC().Format(time.RFC3339Nano),
		})
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].ID < roots[j].ID })
	identities := slices.Clone(policy.AllowedSignerIdentities)
	sort.Slice(identities, func(i, j int) bool {
		if identities[i].Issuer == identities[j].Issuer {
			return identities[i].Subject < identities[j].Subject
		}
		return identities[i].Issuer < identities[j].Issuer
	})
	external, err := canonicalExternalParameters(policy.ExpectedExternalParameters)
	if err != nil {
		return "", err
	}
	document := struct {
		Version                    string           `json:"version"`
		TrustedRoots               []rootChecksum   `json:"trustedRoots"`
		AllowedSignerIdentities    []SignerIdentity `json:"allowedSignerIdentities"`
		AllowedPredicateTypes      []string         `json:"allowedPredicateTypes"`
		AllowedBuilders            []string         `json:"allowedBuilders"`
		AllowedSourcePrefixes      []string         `json:"allowedSourcePrefixes"`
		AllowedBuildTypes          []string         `json:"allowedBuildTypes"`
		ExpectedExternalParameters json.RawMessage  `json:"expectedExternalParameters"`
	}{
		Version:                    policy.Version,
		TrustedRoots:               roots,
		AllowedSignerIdentities:    identities,
		AllowedPredicateTypes:      sortedUnique(policy.AllowedPredicateTypes),
		AllowedBuilders:            sortedUnique(policy.AllowedBuilders),
		AllowedSourcePrefixes:      sortedUnique(policy.AllowedSourcePrefixes),
		AllowedBuildTypes:          sortedUnique(policy.AllowedBuildTypes),
		ExpectedExternalParameters: external,
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func keyfulProvenancePolicyChecksum(policy ProvenancePolicy) (string, error) {
	type rootChecksum struct {
		ID         string `json:"id"`
		Digest     string `json:"digest"`
		ValidFrom  string `json:"validFrom"`
		ValidUntil string `json:"validUntil"`
	}
	roots := make([]rootChecksum, 0, len(policy.TrustedRoots))
	for _, trustRoot := range policy.TrustedRoots {
		sum := sha256.Sum256(trustRoot.JSON)
		roots = append(roots, rootChecksum{
			ID: trustRoot.ID, Digest: "sha256:" + hex.EncodeToString(sum[:]),
			ValidFrom:  trustRoot.ValidFrom.UTC().Format(time.RFC3339Nano),
			ValidUntil: trustRoot.ValidUntil.UTC().Format(time.RFC3339Nano),
		})
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].ID < roots[j].ID })
	identities := slices.Clone(policy.AllowedSignerIdentities)
	sort.Slice(identities, func(i, j int) bool {
		if identities[i].Issuer == identities[j].Issuer {
			return identities[i].Subject < identities[j].Subject
		}
		return identities[i].Issuer < identities[j].Issuer
	})
	external, err := canonicalExternalParameters(policy.ExpectedExternalParameters)
	if err != nil {
		return "", err
	}
	document := struct {
		Version                    string           `json:"version"`
		VerificationMode           string           `json:"verificationMode"`
		TrustedRoots               []rootChecksum   `json:"trustedRoots"`
		AllowedSignerIdentities    []SignerIdentity `json:"allowedSignerIdentities"`
		AllowedPredicateTypes      []string         `json:"allowedPredicateTypes"`
		AllowedBuilders            []string         `json:"allowedBuilders"`
		AllowedSourcePrefixes      []string         `json:"allowedSourcePrefixes"`
		AllowedBuildTypes          []string         `json:"allowedBuildTypes"`
		ExpectedExternalParameters json.RawMessage  `json:"expectedExternalParameters"`
	}{
		Version:                    policy.Version,
		VerificationMode:           policy.VerificationMode,
		TrustedRoots:               roots,
		AllowedSignerIdentities:    identities,
		AllowedPredicateTypes:      sortedUnique(policy.AllowedPredicateTypes),
		AllowedBuilders:            sortedUnique(policy.AllowedBuilders),
		AllowedSourcePrefixes:      sortedUnique(policy.AllowedSourcePrefixes),
		AllowedBuildTypes:          sortedUnique(policy.AllowedBuildTypes),
		ExpectedExternalParameters: external,
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func VerifyComponentReleasePublication(
	ctx context.Context,
	releaseBundle types.ReleaseBundle,
	publication *PublicationProvenance,
	verifier ProvenanceVerifier,
) ([]types.EvidenceVerification, ValidationResult) {
	result := NewValidResult()
	if releaseBundle.ReleaseContract == nil || releaseBundle.ReleaseContract.ComponentV2 == nil {
		return nil, result
	}
	if publication == nil || verifier == nil {
		result.AddError(
			"releaseContract.evidence.provenance",
			"verified",
			"signed provenance verification is required before component publication",
		)
		return nil, result
	}
	contract := releaseBundle.ReleaseContract.ComponentV2
	declared := map[string]struct{}{}
	for _, reference := range contract.Evidence.Provenance {
		if !safeBoundedProvenanceText(reference, maxEvidenceReferenceBytes) {
			result.AddError(
				"releaseContract.evidence.provenance",
				"bounded",
				"provenance references must be bounded single-line values",
			)
			continue
		}
		declared[reference] = struct{}{}
	}
	expected := map[string]struct{}{}
	for _, artifact := range contract.Artifacts {
		for _, platform := range artifact.Platforms {
			expected[artifact.Key+"\x00"+platform.Platform] = struct{}{}
		}
	}
	if len(expected) == 0 {
		result.AddError(
			"releaseContract.artifacts",
			"required",
			"component publication requires at least one platform artifact",
		)
	}
	if len(publication.Evidence) > maxPublicationEvidence || len(publication.Evidence) != len(expected) {
		result.AddError(
			"provenance.evidence",
			"limit",
			"provenance evidence must contain exactly one bounded input per platform artifact",
		)
	}
	inputs := map[string]PublicationProvenanceEvidence{}
	for _, input := range publication.Evidence {
		if !safeBoundedProvenanceText(input.ArtifactKey, maxArtifactKeyBytes) ||
			(input.Platform != "linux/amd64" && input.Platform != "linux/arm64") ||
			!safeBoundedProvenanceText(input.Evidence.Reference, maxEvidenceReferenceBytes) ||
			!safeBoundedProvenanceText(input.Evidence.TrustRootID, maxTrustRootIDBytes) {
			result.AddError("provenance.evidence", "bounded", "provenance evidence identity is invalid")
			continue
		}
		key := input.ArtifactKey + "\x00" + input.Platform
		if _, duplicate := inputs[key]; duplicate {
			result.AddError("provenance."+input.ArtifactKey+"."+input.Platform, "unique", "provenance evidence must be unique")
			continue
		}
		if _, ok := expected[key]; !ok {
			result.AddError(
				"provenance."+input.ArtifactKey+"."+input.Platform,
				"declared",
				"provenance evidence does not match a declared platform artifact",
			)
			continue
		}
		if _, ok := declared[input.Evidence.Reference]; !ok {
			result.AddError("provenance."+input.ArtifactKey+"."+input.Platform, "declared", "provenance reference is not declared")
			continue
		}
		inputs[key] = input
	}
	if len(result.Errors) > 0 {
		result.Valid = false
		return nil, result
	}
	facts := make([]types.EvidenceVerification, 0)
	for _, artifact := range contract.Artifacts {
		for _, platform := range artifact.Platforms {
			key := artifact.Key + "\x00" + platform.Platform
			input, ok := inputs[key]
			field := "provenance." + artifact.Key + "." + platform.Platform
			if !ok {
				result.AddError(field, "required", "verified provenance is required for every platform artifact")
				continue
			}
			verified, err := verifier.Verify(ctx, publication.Policy, ProvenanceArtifact{
				Key:              artifact.Key,
				Platform:         platform.Platform,
				Digest:           platform.Digest,
				SourceRepository: contract.Source.Repository,
				SourceCommit:     contract.Source.Commit,
				BuildID:          contract.Build.ID,
				BuilderID:        contract.Build.Builder,
			}, input.Evidence)
			if err != nil {
				var provenanceErr *ProvenanceError
				code := "verification_failed"
				if errors.As(err, &provenanceErr) {
					code = provenanceErr.Code
				}
				result.AddError(field, code, "signed provenance did not satisfy the frozen publication policy")
				continue
			}
			trustIdentityMatches := verified.TrustRootID == input.Evidence.TrustRootID &&
				(verified.VerificationMode != ProvenanceVerificationModeKeyful ||
					verified.KeyID == input.Evidence.TrustRootID)
			if !validProvenanceVerificationResult(verified) ||
				!trustIdentityMatches ||
				verified.SourceURI != contract.Source.Repository ||
				verified.SourceCommit != contract.Source.Commit ||
				verified.BuildID != contract.Build.ID ||
				verified.BuilderID != contract.Build.Builder {
				result.AddError(
					field,
					"verification_result_invalid",
					"signed provenance did not produce one bounded deterministic verification result",
				)
				continue
			}
			storedTrustMaterialID := verified.TrustRootID
			if verified.VerificationMode == ProvenanceVerificationModeKeyful {
				storedTrustMaterialID = verified.KeyID
			}
			facts = append(facts, types.EvidenceVerification{
				OrganizationID:             releaseBundle.OrganizationID,
				ReleaseBundleID:            releaseBundle.ID,
				ArtifactKey:                artifact.Key,
				Platform:                   platform.Platform,
				ArtifactDigest:             platform.Digest,
				EvidenceReference:          input.Evidence.Reference,
				EvidenceDigest:             verified.EvidenceDigest,
				PolicyChecksum:             verified.PolicyChecksum,
				VerificationMode:           verified.VerificationMode,
				TrustRootID:                storedTrustMaterialID,
				KeyID:                      verified.KeyID,
				KeyFingerprint:             verified.KeyFingerprint,
				PredicateType:              verified.PredicateType,
				BuilderID:                  verified.BuilderID,
				BuildID:                    verified.BuildID,
				SourceURI:                  verified.SourceURI,
				SourceCommit:               verified.SourceCommit,
				BuildType:                  verified.BuildType,
				ExternalParametersChecksum: verified.ExternalParametersChecksum,
				SignerIssuer:               verified.SignerIssuer,
				SignerIdentity:             verified.SignerIdentity,
				VerifiedAt:                 verified.VerifiedAt,
			})
		}
	}
	result.Valid = len(result.Errors) == 0
	if !result.Valid {
		return nil, result
	}
	return facts, result
}

// ProvenancePreflight is deliberately independent from deployment-plan types
// so later planners can consume the same immutable facts without creating a
// release-to-planner package dependency.
func ProvenancePreflight(
	artifacts []ProvenanceArtifact,
	verifications []types.EvidenceVerification,
	policyChecksum string,
) ValidationResult {
	result := NewValidResult()
	if !IsSHA256Digest(policyChecksum) {
		result.AddError("provenance.policyChecksum", "sha256", "provenance policy checksum is invalid")
		return result
	}
	if len(artifacts) == 0 {
		result.AddError("provenance.artifacts", "required", "provenance preflight requires platform artifacts")
		return result
	}
	seenArtifacts := map[string]struct{}{}
	matchedVerifications := make([]bool, len(verifications))
	for _, artifact := range artifacts {
		key := artifact.Key + "\x00" + artifact.Platform + "\x00" + artifact.Digest
		field := "provenance.artifact"
		if !safeBoundedProvenanceText(artifact.Key, maxArtifactKeyBytes) ||
			(artifact.Platform != "linux/amd64" && artifact.Platform != "linux/arm64") ||
			!IsSHA256Digest(artifact.Digest) ||
			!safeProvenanceText(artifact.SourceRepository) ||
			!isLowerHexGitCommit(artifact.SourceCommit) ||
			!safeProvenanceText(artifact.BuildID) ||
			!safeProvenanceText(artifact.BuilderID) {
			result.AddError(field, "artifact", "provenance preflight artifact identity is invalid")
			continue
		}
		field = "provenance." + artifact.Key + "." + artifact.Platform
		if _, duplicate := seenArtifacts[key]; duplicate {
			result.AddError(field, "unique", "artifact provenance preflight input is ambiguous")
			continue
		}
		seenArtifacts[key] = struct{}{}
		matches := 0
		for i, fact := range verifications {
			if fact.ArtifactKey == artifact.Key &&
				fact.Platform == artifact.Platform &&
				fact.ArtifactDigest == artifact.Digest &&
				fact.SourceURI == artifact.SourceRepository &&
				fact.SourceCommit == artifact.SourceCommit &&
				fact.BuildID == artifact.BuildID &&
				fact.BuilderID == artifact.BuilderID &&
				fact.PolicyChecksum == policyChecksum {
				matches++
				matchedVerifications[i] = true
			}
		}
		if matches == 0 {
			result.AddError(field, "verified", "artifact has no matching provenance verification fact")
		} else if matches > 1 {
			result.AddError(field, "unique", "artifact has ambiguous provenance verification facts")
		}
	}
	for _, matched := range matchedVerifications {
		if !matched {
			result.AddError(
				"provenance.verifications",
				"exact",
				"provenance verification facts must exactly match the requested artifacts and policy",
			)
			break
		}
	}
	result.Valid = len(result.Errors) == 0
	return result
}

func parseSHA256Digest(value string) ([]byte, error) {
	if !IsSHA256Digest(value) {
		return nil, fmt.Errorf("invalid sha256 digest")
	}
	return hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
}

func isLowerHexGitCommit(value string) bool {
	if len(value) != 40 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func earliestVerifiedTimestamp(values []verify.TimestampVerificationResult) (time.Time, bool) {
	var earliest time.Time
	for _, value := range values {
		if value.Timestamp.IsZero() {
			continue
		}
		if earliest.IsZero() || value.Timestamp.Before(earliest) {
			earliest = value.Timestamp
		}
	}
	return earliest, !earliest.IsZero()
}

func timestampsWithinTrustRoot(values []verify.TimestampVerificationResult, trustRoot TrustRoot) bool {
	return timestampsWithinValidity(values, trustRoot.ValidFrom, trustRoot.ValidUntil)
}

func timestampsWithinValidity(values []verify.TimestampVerificationResult, validFrom, validUntil time.Time) bool {
	found := false
	for _, value := range values {
		if value.Timestamp.IsZero() {
			continue
		}
		found = true
		if value.Timestamp.Before(validFrom) || !value.Timestamp.Before(validUntil) {
			return false
		}
	}
	return found
}

func canonicalJSON(raw json.RawMessage) ([]byte, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("JSON value is required")
	}
	if err := ValidateProvenanceJSONDocument(raw); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("multiple JSON values")
	}
	return json.Marshal(value)
}

// ValidateProvenanceJSONDocument rejects duplicate object members and multiple
// top-level values so security-sensitive policy inputs have one interpretation.
func ValidateProvenanceJSONDocument(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := consumeUniqueJSONValue(decoder); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err != nil {
			return err
		}
		return fmt.Errorf("multiple JSON values")
	}
	return nil
}

func consumeUniqueJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("JSON object member name is invalid")
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate JSON object member")
			}
			seen[key] = struct{}{}
			if err := consumeUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim('}') {
			return fmt.Errorf("JSON object is malformed")
		}
	case '[':
		for decoder.More() {
			if err := consumeUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim(']') {
			return fmt.Errorf("JSON array is malformed")
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter")
	}
	return nil
}

func canonicalExternalParameters(raw json.RawMessage) ([]byte, error) {
	canonical, err := canonicalJSON(raw)
	if err != nil {
		return nil, err
	}
	var value map[string]any
	if err := json.Unmarshal(canonical, &value); err != nil || value == nil {
		return nil, fmt.Errorf("external parameters must be a JSON object")
	}
	return canonical, nil
}

func canonicalSourceURI(value string) bool {
	if !safeBoundedProvenanceText(value, maxSourceURIBytes) {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil ||
		parsed.Scheme == "" ||
		parsed.Host == "" ||
		parsed.User != nil ||
		parsed.Fragment != "" ||
		parsed.RawQuery != "" ||
		parsed.RawPath != "" ||
		strings.Contains(parsed.Path, "//") ||
		(parsed.Path != "" && path.Clean(parsed.Path) != parsed.Path) {
		return false
	}
	return parsed.Scheme == strings.ToLower(parsed.Scheme) &&
		parsed.Host == strings.ToLower(parsed.Host) &&
		parsed.String() == value
}

func canonicalSourcePrefix(value string) bool {
	if !strings.HasSuffix(value, "/") {
		return false
	}
	return canonicalSourceURI(strings.TrimSuffix(value, "/"))
}

func safeProvenanceText(value string) bool {
	return safeBoundedProvenanceText(value, maxProvenanceTextBytes)
}

func safeBoundedProvenanceText(value string, limit int) bool {
	return value != "" &&
		len(value) <= limit &&
		utf8.ValidString(value) &&
		strings.TrimSpace(value) == value &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}

func validProvenanceVerificationResult(result ProvenanceVerificationResult) bool {
	baseValid := IsSHA256Digest(result.EvidenceDigest) &&
		IsSHA256Digest(result.PolicyChecksum) &&
		IsSHA256Digest(result.ExternalParametersChecksum) &&
		safeProvenanceText(result.PredicateType) &&
		safeProvenanceText(result.BuilderID) &&
		safeProvenanceText(result.BuildID) &&
		canonicalSourceURI(result.SourceURI) &&
		isLowerHexGitCommit(result.SourceCommit) &&
		safeProvenanceText(result.BuildType) &&
		safeProvenanceText(result.SignerIssuer) &&
		safeProvenanceText(result.SignerIdentity) &&
		!result.VerifiedAt.IsZero()
	if !baseValid {
		return false
	}
	switch result.VerificationMode {
	case ProvenanceVerificationModeKeyless:
		return safeBoundedProvenanceText(result.TrustRootID, maxTrustRootIDBytes) &&
			result.KeyID == "" && result.KeyFingerprint == "" &&
			!strings.HasPrefix(result.SignerIssuer, keyfulSignerIssuerPrefix)
	case ProvenanceVerificationModeKeyful:
		return safeBoundedProvenanceText(result.KeyID, maxTrustRootIDBytes) &&
			result.TrustRootID == result.KeyID &&
			IsSHA256Digest(result.KeyFingerprint) &&
			result.SignerIssuer == keyfulSignerIssuerPrefix+result.KeyID &&
			result.SignerIdentity == result.KeyFingerprint
	default:
		return false
	}
}

func sortedUnique(values []string) []string {
	result := slices.Clone(values)
	sort.Strings(result)
	return slices.Compact(result)
}
