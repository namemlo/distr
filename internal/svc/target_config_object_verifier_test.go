package svc

import (
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/distr-sh/distr/internal/env"
	"github.com/distr-sh/distr/internal/targetconfig"
	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

func TestNewTargetConfigObjectVerifierReturnsBoundedUnavailableWhenUnconfigured(t *testing.T) {
	g := NewWithT(t)
	verifier := newTargetConfigObjectVerifierWithConfig(
		t.Context(),
		env.TargetConfigObjectStoreConfig{},
	)

	_, err := verifier.Verify(t.Context(), types.TargetConfigSnapshotObject{})

	g.Expect(errors.Is(err, targetconfig.ErrObjectVerificationUnavailable)).To(BeTrue())
}

func TestNewTargetConfigObjectVerifierBindsConfiguredBucket(t *testing.T) {
	g := NewWithT(t)
	endpoint := "https://objects.example.invalid"
	accessKey := "access-key"
	secretKey := "generated-secret"
	verifier := newTargetConfigObjectVerifierWithConfig(
		t.Context(),
		env.TargetConfigObjectStoreConfig{
			Enabled: true,
			S3: env.S3Config{
				Region:          "ap-southeast-1",
				Endpoint:        &endpoint,
				Bucket:          "config-bucket",
				AccessKeyID:     &accessKey,
				SecretAccessKey: &secretKey,
			},
		},
	)

	_, err := verifier.Verify(t.Context(), types.TargetConfigSnapshotObject{
		Reference: "s3://other-bucket/path/config.json",
		VersionID: "version-7",
	})

	g.Expect(err).To(MatchError("object reference is outside configured bucket"))
}

func TestS3ClientOptionsLeaveAWSDefaultsForEmptyOptionalConfig(t *testing.T) {
	g := NewWithT(t)
	empty := ""
	options := s3.Options{}

	s3ClientOptions(env.S3Config{
		Region:          "ap-southeast-1",
		Endpoint:        &empty,
		AccessKeyID:     &empty,
		SecretAccessKey: &empty,
	})(&options)

	g.Expect(options.Region).To(Equal("ap-southeast-1"))
	g.Expect(options.BaseEndpoint).To(BeNil())
	g.Expect(options.Credentials).To(BeNil())
}
