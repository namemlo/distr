package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/releasebundles"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	bundlev1 "github.com/sigstore/protobuf-specs/gen/pb-go/bundle/v1"
	commonv1 "github.com/sigstore/protobuf-specs/gen/pb-go/common/v1"
	dssev1 "github.com/sigstore/protobuf-specs/gen/pb-go/dsse"
	rekorv1 "github.com/sigstore/protobuf-specs/gen/pb-go/rekor/v1"
	sigbundle "github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/testing/ca"
	"github.com/sigstore/sigstore-go/pkg/verify"
)

const (
	issuer        = "https://issuer.fixture.invalid"
	subject       = "control-plane-builder@fixture.invalid"
	predicateType = "https://slsa.dev/provenance/v1"
	buildType     = "https://build.fixture.invalid/types/container/v1"
	trustRootID   = "neutral-control-plane-root"
)

type input struct {
	ArtifactKey      string `json:"artifactKey"`
	Platform         string `json:"platform"`
	Digest           string `json:"digest"`
	SourceRepository string `json:"sourceRepository"`
	SourceCommit     string `json:"sourceCommit"`
	BuildID          string `json:"buildId"`
	BuilderID        string `json:"builderId"`
}

type output struct {
	ContractEvidence types.ComponentReleaseEvidenceReferences `json:"contractEvidence"`
	Publication      api.PublishReleaseBundleRequest          `json:"publication"`
	Verification     verification                             `json:"verification"`
}

type verification struct {
	Valid          bool   `json:"valid"`
	PolicyChecksum string `json:"policyChecksum"`
	EvidenceDigest string `json:"evidenceDigest"`
}

func main() {
	var request input
	if err := json.NewDecoder(os.Stdin).Decode(&request); err != nil {
		fail(fmt.Errorf("decode input: %w", err))
	}
	result, err := build(request)
	if err != nil {
		fail(err)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(result); err != nil {
		fail(fmt.Errorf("encode output: %w", err))
	}
}

func fail(err error) {
	_, _ = fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func build(request input) (output, error) {
	if request.ArtifactKey == "" ||
		request.Platform != "linux/amd64" ||
		!releasebundles.IsSHA256Digest(request.Digest) ||
		len(request.SourceCommit) != 40 {
		return output{}, fmt.Errorf("invalid component publication fixture input")
	}
	externalParameters := json.RawMessage(`{"release":true,"target":"container"}`)
	statement, err := json.Marshal(map[string]any{
		"_type": "https://in-toto.io/Statement/v1",
		"subject": []any{map[string]any{
			"name": request.ArtifactKey,
			"digest": map[string]string{
				"sha256": strings.TrimPrefix(request.Digest, "sha256:"),
			},
		}},
		"predicateType": predicateType,
		"predicate": map[string]any{
			"buildDefinition": map[string]any{
				"buildType":          buildType,
				"externalParameters": map[string]any{"release": true, "target": "container"},
				"resolvedDependencies": []any{map[string]any{
					"uri": request.SourceRepository,
					"digest": map[string]string{
						"gitCommit": request.SourceCommit,
					},
				}},
			},
			"runDetails": map[string]any{
				"builder": map[string]any{"id": request.BuilderID},
				"metadata": map[string]any{
					"invocationId": request.BuildID,
				},
			},
		},
	})
	if err != nil {
		return output{}, fmt.Errorf("marshal statement: %w", err)
	}

	virtual, err := ca.NewVirtualSigstore()
	if err != nil {
		return output{}, fmt.Errorf("create virtual Sigstore: %w", err)
	}
	now := time.Now().UTC()
	entity, err := virtual.AttestAtTime(subject, issuer, statement, now.Add(5*time.Minute), true)
	if err != nil {
		return output{}, fmt.Errorf("sign provenance: %w", err)
	}
	bundleJSON, err := marshalBundle(entity)
	if err != nil {
		return output{}, err
	}
	rekorLogs, err := normalizedTransparencyLogs(virtual.RekorLogs())
	if err != nil {
		return output{}, err
	}
	ctLogs, err := normalizedTransparencyLogs(virtual.CTLogs())
	if err != nil {
		return output{}, err
	}
	trusted, err := root.NewTrustedRoot(
		root.TrustedRootMediaType01,
		virtual.FulcioCertificateAuthorities(),
		ctLogs,
		virtual.TimestampingAuthorities(),
		rekorLogs,
	)
	if err != nil {
		return output{}, fmt.Errorf("create trusted root: %w", err)
	}
	trustedRootJSON, err := trusted.MarshalJSON()
	if err != nil {
		return output{}, fmt.Errorf("marshal trusted root: %w", err)
	}
	var parsedBundle sigbundle.Bundle
	if err := parsedBundle.UnmarshalJSON(bundleJSON); err != nil {
		return output{}, fmt.Errorf("parse generated Sigstore bundle: %w", err)
	}
	identity, err := verify.NewShortCertificateIdentity(issuer, "", subject, "")
	if err != nil {
		return output{}, fmt.Errorf("create signer identity: %w", err)
	}
	digest, err := hex.DecodeString(strings.TrimPrefix(request.Digest, "sha256:"))
	if err != nil {
		return output{}, fmt.Errorf("decode artifact digest: %w", err)
	}
	roundTrippedTrusted, err := root.NewTrustedRootFromJSON(trustedRootJSON)
	if err != nil {
		return output{}, fmt.Errorf("parse generated trusted root: %w", err)
	}
	verifier, err := verify.NewVerifier(
		roundTrippedTrusted,
		verify.WithTransparencyLog(1),
		verify.WithObserverTimestamps(1),
	)
	if err != nil {
		return output{}, fmt.Errorf("create Sigstore verifier: %w", err)
	}
	if _, err := verifier.Verify(
		&parsedBundle,
		verify.NewPolicy(
			verify.WithArtifactDigest("sha256", digest),
			verify.WithCertificateIdentity(identity),
		),
	); err != nil {
		return output{}, fmt.Errorf("verify generated Sigstore bundle: %w", err)
	}
	bundleDigest := sha256Digest(bundleJSON)
	sbomDocument, err := json.Marshal(map[string]any{
		"spdxVersion": "SPDX-2.3",
		"dataLicense": "CC0-1.0",
		"SPDXID":      "SPDXRef-DOCUMENT",
		"name":        request.ArtifactKey,
		"packages": []any{map[string]any{
			"SPDXID":           "SPDXRef-Package",
			"name":             request.ArtifactKey,
			"versionInfo":      request.BuildID,
			"downloadLocation": "NOASSERTION",
			"checksums": []any{map[string]any{
				"algorithm":     "SHA256",
				"checksumValue": strings.TrimPrefix(request.Digest, "sha256:"),
			}},
		}},
	})
	if err != nil {
		return output{}, fmt.Errorf("marshal SBOM: %w", err)
	}
	provenanceReference := fmt.Sprintf(
		"oci://evidence.fixture.invalid/%s/provenance@%s",
		request.ArtifactKey,
		bundleDigest,
	)
	sbomReference := fmt.Sprintf(
		"oci://evidence.fixture.invalid/%s/sbom@%s",
		request.ArtifactKey,
		sha256Digest(sbomDocument),
	)
	validFrom, validUntil := now.Add(-time.Hour), now.Add(time.Hour)
	publication := api.PublishReleaseBundleRequest{
		Provenance: &api.ComponentReleasePublicationProvenance{
			Policy: api.ComponentReleaseProvenancePolicy{
				Version: releasebundles.ProvenancePolicyVersion,
				TrustedRoots: []api.ComponentReleaseProvenanceTrustRoot{{
					ID: trustRootID, TrustedRoot: trustedRootJSON,
					ValidFrom: validFrom, ValidUntil: validUntil,
				}},
				AllowedSignerIdentities: []api.ComponentReleaseProvenanceSignerIdentity{{
					Issuer: issuer, Subject: subject,
				}},
				AllowedPredicateTypes:      []string{predicateType},
				AllowedBuilders:            []string{request.BuilderID},
				AllowedSourcePrefixes:      []string{sourcePrefix(request.SourceRepository)},
				AllowedBuildTypes:          []string{buildType},
				ExpectedExternalParameters: externalParameters,
			},
			Evidence: []api.ComponentReleaseProvenanceEvidence{{
				ArtifactKey: request.ArtifactKey,
				Platform:    request.Platform,
				Reference:   provenanceReference,
				TrustRootID: trustRootID,
				Bundle:      bundleJSON,
			}},
		},
	}
	contract := types.ComponentReleaseContractV2{
		Schema:       types.ReleaseContractSchemaV2,
		ComponentKey: request.ArtifactKey,
		Version:      "1.0.0",
		Source: types.ComponentReleaseSource{
			Repository:   request.SourceRepository,
			RequestedRef: "refs/tags/v1.0.0",
			Commit:       request.SourceCommit,
		},
		Build: types.ComponentReleaseBuild{ID: request.BuildID, Builder: request.BuilderID},
		Artifacts: []types.ComponentReleaseArtifact{{
			Key: request.ArtifactKey, Type: "oci-image",
			MediaType: "application/vnd.oci.image.manifest.v1+json",
			Digest:    request.Digest,
			Platforms: []types.ComponentReleasePlatform{{
				Platform: request.Platform, Digest: request.Digest,
			}},
		}},
		Changes: types.ComponentReleaseChanges{Summary: "Neutral fixture publication"},
		Evidence: types.ComponentReleaseEvidenceReferences{
			Provenance: []string{provenanceReference},
			SBOM:       []string{sbomReference},
			Signatures: []string{},
			Tests:      []string{},
		},
	}
	if issues := releasebundles.ValidateComponentReleaseContractV2(contract); len(issues) > 0 {
		return output{}, fmt.Errorf("component contract rejected: %v", issues)
	}
	releaseBundle := types.ReleaseBundle{
		ID: uuid.New(), OrganizationID: uuid.New(),
		Kind:            types.ReleaseBundleKindComponent,
		ReleaseContract: &types.ReleaseContract{ComponentV2: &contract},
	}
	facts, validation := releasebundles.VerifyComponentReleasePublication(
		context.Background(),
		releaseBundle,
		publication.PublicationProvenance(),
		releasebundles.SigstoreProvenanceVerifier{},
	)
	if !validation.Valid || len(facts) != 1 {
		return output{}, fmt.Errorf("production provenance verification rejected fixture: %+v", validation.Errors)
	}
	return output{
		ContractEvidence: contract.Evidence,
		Publication:      publication,
		Verification: verification{
			Valid: true, PolicyChecksum: facts[0].PolicyChecksum, EvidenceDigest: facts[0].EvidenceDigest,
		},
	}, nil
}

func sourcePrefix(repository string) string {
	index := strings.LastIndex(repository, "/")
	if index < len("https://") {
		return repository + "/"
	}
	return repository[:index+1]
}

func sha256Digest(value []byte) string {
	sum := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func normalizedTransparencyLogs(logs map[string]*root.TransparencyLog) (map[string]*root.TransparencyLog, error) {
	for key, log := range logs {
		decoded, err := hex.DecodeString(key)
		if err != nil {
			return nil, fmt.Errorf("decode transparency log ID: %w", err)
		}
		log.ID = decoded
	}
	return logs, nil
}

func marshalBundle(entity *ca.TestEntity) (json.RawMessage, error) {
	verificationContent, err := entity.VerificationContent()
	if err != nil {
		return nil, fmt.Errorf("read verification content: %w", err)
	}
	certificate, ok := verificationContent.(*sigbundle.Certificate)
	if !ok || certificate.Certificate() == nil {
		return nil, fmt.Errorf("signed provenance did not contain a certificate")
	}
	signatureContent, err := entity.SignatureContent()
	if err != nil || signatureContent.EnvelopeContent() == nil {
		return nil, fmt.Errorf("signed provenance did not contain a DSSE envelope")
	}
	envelope := signatureContent.EnvelopeContent().RawEnvelope()
	payload, err := envelope.DecodeB64Payload()
	if err != nil || len(envelope.Signatures) != 1 {
		return nil, fmt.Errorf("decode DSSE envelope")
	}
	signature, err := base64.StdEncoding.DecodeString(envelope.Signatures[0].Sig)
	if err != nil {
		return nil, fmt.Errorf("decode DSSE signature: %w", err)
	}
	entries, err := entity.TlogEntries()
	if err != nil {
		return nil, fmt.Errorf("read transparency entries: %w", err)
	}
	timestamps, err := entity.Timestamps()
	if err != nil {
		return nil, fmt.Errorf("read signed timestamps: %w", err)
	}
	protoEntries := make([]*rekorv1.TransparencyLogEntry, 0, len(entries))
	for _, entry := range entries {
		protoEntry := entry.TransparencyLogEntry()
		protoEntry.KindVersion = &rekorv1.KindVersion{Kind: "intoto", Version: "0.0.2"}
		protoEntries = append(protoEntries, protoEntry)
	}
	protoTimestamps := make([]*commonv1.RFC3161SignedTimestamp, 0, len(timestamps))
	for _, timestamp := range timestamps {
		protoTimestamps = append(protoTimestamps, &commonv1.RFC3161SignedTimestamp{SignedTimestamp: timestamp})
	}
	protoBundle := &bundlev1.Bundle{
		MediaType: "application/vnd.dev.sigstore.bundle.v0.3+json",
		VerificationMaterial: &bundlev1.VerificationMaterial{
			Content: &bundlev1.VerificationMaterial_Certificate{
				Certificate: &commonv1.X509Certificate{RawBytes: certificate.Certificate().Raw},
			},
			TlogEntries: protoEntries,
			TimestampVerificationData: &bundlev1.TimestampVerificationData{
				Rfc3161Timestamps: protoTimestamps,
			},
		},
		Content: &bundlev1.Bundle_DsseEnvelope{
			DsseEnvelope: &dssev1.Envelope{
				Payload: payload, PayloadType: envelope.PayloadType,
				Signatures: []*dssev1.Signature{{
					Keyid: envelope.Signatures[0].KeyID,
					Sig:   signature,
				}},
			},
		},
	}
	parsed, err := sigbundle.NewBundle(protoBundle)
	if err != nil {
		return nil, fmt.Errorf("construct Sigstore bundle: %w", err)
	}
	encoded, err := parsed.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("marshal Sigstore bundle: %w", err)
	}
	return encoded, nil
}
