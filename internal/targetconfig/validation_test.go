package targetconfig

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

const targetConfigTestObjectReference = "s3://config-bucket/path/config.json"

func TestTargetConfigValidateDraftRejectsUnsafeInputs(t *testing.T) {
	tests := []struct {
		name string
		edit func(*types.TargetConfigSnapshotDraft)
		code string
	}{
		{
			name: "secret looking runtime metadata",
			edit: func(draft *types.TargetConfigSnapshotDraft) {
				draft.RuntimeConstraints["password"] = "not-allowed"
			},
			code: "secret_boundary",
		},
		{
			name: "mutable object reference",
			edit: func(draft *types.TargetConfigSnapshotDraft) {
				draft.Objects[0].Reference = "s3://config-bucket/current/config.json"
			},
			code: "immutable_reference",
		},
		{
			name: "missing placement",
			edit: func(draft *types.TargetConfigSnapshotDraft) {
				draft.DeploymentUnitID = uuid.Nil
			},
			code: "required",
		},
		{
			name: "cross scope component",
			edit: func(draft *types.TargetConfigSnapshotDraft) {
				draft.Components[0].DeploymentUnitID = uuid.New()
			},
			code: "scope_mismatch",
		},
		{
			name: "plaintext secret reference",
			edit: func(draft *types.TargetConfigSnapshotDraft) {
				draft.SecretReferences[0].Reference = "postgres://user:password@db/client"
			},
			code: "secret_boundary",
		},
		{
			name: "client config path secret reference",
			edit: func(draft *types.TargetConfigSnapshotDraft) {
				draft.SecretReferences[0].Reference = "clients/example/appsettings.json"
			},
			code: "secret_boundary",
		},
		{
			name: "secret assignment used as provider",
			edit: func(draft *types.TargetConfigSnapshotDraft) {
				draft.SecretReferences[0].Provider = "password=hunter2"
			},
			code: "secret_boundary",
		},
		{
			name: "URI userinfo used as provider",
			edit: func(draft *types.TargetConfigSnapshotDraft) {
				draft.SecretReferences[0].Provider = "https://user:password@vault.example.invalid"
			},
			code: "secret_boundary",
		},
		{
			name: "plaintext credential pair used as provider",
			edit: func(draft *types.TargetConfigSnapshotDraft) {
				draft.SecretReferences[0].Provider = "operator:hunter2"
			},
			code: "secret_boundary",
		},
		{
			name: "plain access token used as provider",
			edit: func(draft *types.TargetConfigSnapshotDraft) {
				draft.SecretReferences[0].Provider = "ghp_" + strings.Repeat("a", 20)
			},
			code: "secret_boundary",
		},
		{
			name: "non portable provider identifier",
			edit: func(draft *types.TargetConfigSnapshotDraft) {
				draft.SecretReferences[0].Provider = "Vault Enterprise"
			},
			code: "invalid",
		},
		{
			name: "control byte in version ID",
			edit: func(draft *types.TargetConfigSnapshotDraft) {
				draft.Objects[0].VersionID = "version-\x007"
			},
			code: "immutable_reference",
		},
		{
			name: "secret assignment in version ID",
			edit: func(draft *types.TargetConfigSnapshotDraft) {
				draft.Objects[0].VersionID = "password=version-secret"
			},
			code: "immutable_reference",
		},
		{
			name: "unknown object kind",
			edit: func(draft *types.TargetConfigSnapshotDraft) {
				draft.Objects[0].Kind = "custom"
			},
			code: "unsupported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			draft := validTargetConfigDraft()
			tt.edit(&draft)

			issues := ValidateDraft(draft)

			g.Expect(issues).To(ContainElement(HaveField("Code", tt.code)))
		})
	}
}

func TestTargetConfigVersionIDValidationIsConsistentAtCreationAndVerification(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		isValid bool
	}{
		{name: "empty checksum identity", value: "", isValid: true},
		{
			name:    "opaque AWS version identity",
			value:   "3/L4kqtJlcpXroDTDmJ+rmSpXd3dIbrHY+MTRCxf3M=",
			isValid: true,
		},
		{name: "opaque identity with internal space", value: "provider version 7", isValid: true},
		{name: "leading whitespace", value: " version-7", isValid: false},
		{name: "ASCII control", value: "version-\x007", isValid: false},
		{name: "delete control", value: "version-\x7f7", isValid: false},
		{name: "secret assignment", value: "password=version-secret", isValid: false},
		{name: "raw GitHub credential", value: "ghp_" + strings.Repeat("1", 20), isValid: false},
		{name: "raw Slack credential", value: "xoxb-" + strings.Repeat("1", 20), isValid: false},
		{name: "raw AWS credential", value: "AKIA" + strings.Repeat("1", 16), isValid: false},
		{name: "oversized", value: strings.Repeat("v", 1025), isValid: false},
		{name: "invalid UTF-8", value: string([]byte{'v', 0xff}), isValid: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			draft := validTargetConfigDraft()
			draft.Objects[0].VersionID = tt.value

			g.Expect(isImmutableTargetConfigObject(draft.Objects[0])).To(Equal(tt.isValid))
			g.Expect(validObservedVersionID(tt.value)).To(Equal(tt.isValid))
		})
	}
}

func TestTargetConfigValidateDraftBoundsDiagnostics(t *testing.T) {
	g := NewWithT(t)
	draft := validTargetConfigDraft()
	draft.Objects = make([]types.TargetConfigSnapshotObjectDraft, 300)
	for index := range draft.Objects {
		draft.Objects[index].Key = strings.Repeat("x", 500)
	}

	issues := ValidateDraft(draft)

	g.Expect(issues).To(HaveLen(maxValidationIssues))
	for _, issue := range issues {
		g.Expect(len(issue.Message)).To(BeNumerically("<=", maxValidationMessageBytes))
	}
}

func TestTargetConfigValidateDraftAcceptsOpaqueManagedSecretReference(t *testing.T) {
	g := NewWithT(t)
	draft := validTargetConfigDraft()
	draft.SecretReferences[0].Provider = "aws-secrets-manager"
	draft.SecretReferences[0].Reference = "projects/acme/secrets/database/versions/7"

	g.Expect(ValidateDraft(draft)).To(BeEmpty())
}

func TestTargetConfigVerifyObjectsReturnsBoundedTamperFacts(t *testing.T) {
	g := NewWithT(t)
	draft := validTargetConfigDraft()
	snapshot := types.TargetConfigSnapshot{
		Objects: []types.TargetConfigSnapshotObject{{
			Key: draft.Objects[0].Key, Reference: draft.Objects[0].Reference,
			Checksum: draft.Objects[0].Checksum, MediaType: draft.Objects[0].MediaType,
			SizeBytes: draft.Objects[0].SizeBytes,
		}},
	}
	verifier := targetConfigVerifierFunc(func(
		_ types.TargetConfigSnapshotObject,
	) (types.VerifiedTargetConfigObject, error) {
		return types.VerifiedTargetConfigObject{
			Reference: snapshot.Objects[0].Reference,
			Checksum:  targetConfigDigest("f"),
			MediaType: snapshot.Objects[0].MediaType,
			SizeBytes: snapshot.Objects[0].SizeBytes,
		}, nil
	})

	result, err := VerifyObjects(t.Context(), snapshot, verifier)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Verified).To(BeFalse())
	g.Expect(result.Objects).To(HaveLen(1))
	g.Expect(result.Objects[0].Code).To(Equal("checksum_mismatch"))
	g.Expect(result.Objects[0].Message).To(Equal("object checksum does not match snapshot"))
}

func TestTargetConfigVerifyObjectsReturnsBoundedUnavailableFact(t *testing.T) {
	g := NewWithT(t)
	draft := validTargetConfigDraft()
	snapshot := types.TargetConfigSnapshot{
		Objects: []types.TargetConfigSnapshotObject{{
			Key:       draft.Objects[0].Key,
			Reference: draft.Objects[0].Reference,
			Checksum:  draft.Objects[0].Checksum,
			MediaType: draft.Objects[0].MediaType,
			SizeBytes: draft.Objects[0].SizeBytes,
		}},
	}

	result, err := VerifyObjects(t.Context(), snapshot, NewUnavailableObjectVerifier())

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Verified).To(BeFalse())
	g.Expect(result.Objects).To(HaveLen(1))
	g.Expect(result.Objects[0].Code).To(Equal("verification_unavailable"))
	g.Expect(result.Objects[0].Message).To(Equal("object verification is unavailable"))
	g.Expect(result.Objects[0].ObservedChecksum).To(BeEmpty())
}

func TestS3ObjectVerifierPinsVersionAndComputesDigestWithoutReturningBody(t *testing.T) {
	g := NewWithT(t)
	body := []byte("immutable config")
	client := &targetConfigS3GetObjectClient{
		output: &s3.GetObjectOutput{
			Body:          io.NopCloser(bytes.NewReader(body)),
			ContentLength: aws.Int64(int64(len(body))),
			ContentType:   aws.String("application/json"),
			VersionId:     aws.String("version-7"),
		},
	}
	object := types.TargetConfigSnapshotObject{
		Reference: targetConfigTestObjectReference,
		VersionID: "version-7",
		SizeBytes: int64(len(body)),
	}

	observed, err := NewS3ObjectVerifier(client).Verify(t.Context(), object)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(aws.ToString(client.input.Bucket)).To(Equal("config-bucket"))
	g.Expect(aws.ToString(client.input.Key)).To(Equal("path/config.json"))
	g.Expect(aws.ToString(client.input.VersionId)).To(Equal("version-7"))
	g.Expect(observed.Reference).To(Equal(object.Reference))
	g.Expect(observed.VersionID).To(Equal("version-7"))
	g.Expect(observed.MediaType).To(Equal("application/json"))
	g.Expect(observed.SizeBytes).To(Equal(int64(len(body))))
	g.Expect(observed.Checksum).To(MatchRegexp(`^sha256:[0-9a-f]{64}$`))
}

func TestS3ObjectVerifierRejectsUnsafeProviderMetadata(t *testing.T) {
	tests := []struct {
		name        string
		versionID   string
		contentType string
	}{
		{
			name:        "oversized version id",
			versionID:   strings.Repeat("v", 1025),
			contentType: "application/json",
		},
		{
			name:        "version id with newline",
			versionID:   "version-7\nforged-header",
			contentType: "application/json",
		},
		{
			name:        "version id with control byte",
			versionID:   "version-\x007",
			contentType: "application/json",
		},
		{
			name:        "oversized content type",
			versionID:   "version-7",
			contentType: "application/" + strings.Repeat("x", 117),
		},
		{
			name:        "content type with newline",
			versionID:   "version-7",
			contentType: "application/json\nforged-header",
		},
		{
			name:        "content type with control byte",
			versionID:   "version-7",
			contentType: "application/\x00json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			body := []byte("immutable config")
			client := &targetConfigS3GetObjectClient{
				output: &s3.GetObjectOutput{
					Body:          io.NopCloser(bytes.NewReader(body)),
					ContentLength: aws.Int64(int64(len(body))),
					ContentType:   aws.String(tt.contentType),
					VersionId:     aws.String(tt.versionID),
				},
			}
			object := types.TargetConfigSnapshotObject{
				Reference: targetConfigTestObjectReference,
				VersionID: "version-7",
				SizeBytes: int64(len(body)),
			}

			observed, err := NewS3ObjectVerifier(client).Verify(t.Context(), object)

			g.Expect(err).To(MatchError("object provider returned invalid metadata"))
			g.Expect(observed).To(Equal(types.VerifiedTargetConfigObject{}))
		})
	}
}

func TestS3ObjectVerifierAcceptsProviderMetadataAtExactLimits(t *testing.T) {
	g := NewWithT(t)
	body := []byte("immutable config")
	versionID := strings.Repeat("v", maxTargetConfigVersionIDBytes)
	contentType := "application/" + strings.Repeat("x", maxObservedMediaTypeBytes-len("application/"))
	client := &targetConfigS3GetObjectClient{
		output: &s3.GetObjectOutput{
			Body:          io.NopCloser(bytes.NewReader(body)),
			ContentLength: aws.Int64(int64(len(body))),
			ContentType:   aws.String(contentType),
			VersionId:     aws.String(versionID),
		},
	}

	observed, err := NewS3ObjectVerifier(client).Verify(t.Context(), types.TargetConfigSnapshotObject{
		Reference: targetConfigTestObjectReference,
		VersionID: versionID,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(observed.VersionID).To(Equal(versionID))
	g.Expect(observed.MediaType).To(Equal(contentType))
}

func TestTargetConfigVerifyObjectsDropsUnsafeObservedMetadata(t *testing.T) {
	draft := validTargetConfigDraft()
	snapshot := types.TargetConfigSnapshot{
		Objects: []types.TargetConfigSnapshotObject{{
			Key:       draft.Objects[0].Key,
			Reference: draft.Objects[0].Reference,
			VersionID: "version-7",
			Checksum:  draft.Objects[0].Checksum,
			MediaType: draft.Objects[0].MediaType,
			SizeBytes: draft.Objects[0].SizeBytes,
		}},
	}
	tests := []struct {
		name      string
		versionID string
		mediaType string
	}{
		{
			name:      "unsafe version id",
			versionID: strings.Repeat("v", 1025) + "\nforged-header",
			mediaType: snapshot.Objects[0].MediaType,
		},
		{
			name:      "unsafe media type",
			versionID: snapshot.Objects[0].VersionID,
			mediaType: "application/json\nforged-header",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			verifier := targetConfigVerifierFunc(func(
				_ types.TargetConfigSnapshotObject,
			) (types.VerifiedTargetConfigObject, error) {
				return types.VerifiedTargetConfigObject{
					Reference: snapshot.Objects[0].Reference,
					VersionID: tt.versionID,
					Checksum:  snapshot.Objects[0].Checksum,
					MediaType: tt.mediaType,
					SizeBytes: snapshot.Objects[0].SizeBytes,
				}, nil
			})

			result, err := VerifyObjects(t.Context(), snapshot, verifier)

			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(result.Verified).To(BeFalse())
			g.Expect(result.Objects).To(HaveLen(1))
			g.Expect(result.Objects[0].Code).To(Equal("verification_failed"))
			g.Expect(result.Objects[0].Message).To(Equal("object could not be verified"))
			g.Expect(result.Objects[0].ObservedVersionID).To(BeEmpty())
			g.Expect(result.Objects[0].ObservedMediaType).To(BeEmpty())
			g.Expect(result.Objects[0].ObservedChecksum).To(BeEmpty())
			g.Expect(result.Objects[0].ObservedSizeBytes).To(BeNil())
		})
	}
}

func TestTargetConfigVerifyObjectsPreservesObservedZeroSizePresence(t *testing.T) {
	g := NewWithT(t)
	draft := validTargetConfigDraft()
	snapshot := types.TargetConfigSnapshot{
		Objects: []types.TargetConfigSnapshotObject{{
			Key:       draft.Objects[0].Key,
			Reference: draft.Objects[0].Reference,
			Checksum:  draft.Objects[0].Checksum,
			MediaType: draft.Objects[0].MediaType,
			SizeBytes: 0,
		}},
	}
	verifier := targetConfigVerifierFunc(func(
		expected types.TargetConfigSnapshotObject,
	) (types.VerifiedTargetConfigObject, error) {
		return types.VerifiedTargetConfigObject{
			Reference: expected.Reference,
			Checksum:  expected.Checksum,
			MediaType: expected.MediaType,
			SizeBytes: 0,
		}, nil
	})

	result, err := VerifyObjects(t.Context(), snapshot, verifier)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Verified).To(BeTrue())
	g.Expect(result.Objects).To(HaveLen(1))
	g.Expect(result.Objects[0].ObservedSizeBytes).NotTo(BeNil())
	g.Expect(*result.Objects[0].ObservedSizeBytes).To(BeZero())
}

func TestS3ObjectVerifierRejectsObjectsOutsideConfiguredBucket(t *testing.T) {
	g := NewWithT(t)
	client := &targetConfigS3GetObjectClient{}
	object := types.TargetConfigSnapshotObject{
		Reference: "s3://other-bucket/path/config.json",
		VersionID: "version-7",
	}

	observed, err := NewS3ObjectVerifierForBucket(client, "config-bucket").Verify(t.Context(), object)

	g.Expect(err).To(MatchError("object reference is outside configured bucket"))
	g.Expect(observed).To(Equal(types.VerifiedTargetConfigObject{}))
	g.Expect(client.input).To(BeNil())
}

type targetConfigVerifierFunc func(
	types.TargetConfigSnapshotObject,
) (types.VerifiedTargetConfigObject, error)

func (fn targetConfigVerifierFunc) Verify(
	_ context.Context,
	object types.TargetConfigSnapshotObject,
) (types.VerifiedTargetConfigObject, error) {
	return fn(object)
}

type targetConfigS3GetObjectClient struct {
	input  *s3.GetObjectInput
	output *s3.GetObjectOutput
	err    error
}

func (client *targetConfigS3GetObjectClient) GetObject(
	_ context.Context,
	input *s3.GetObjectInput,
	_ ...func(*s3.Options),
) (*s3.GetObjectOutput, error) {
	client.input = input
	return client.output, client.err
}
