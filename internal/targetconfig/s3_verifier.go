package targetconfig

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/distr-sh/distr/internal/types"
)

type S3GetObjectClient interface {
	GetObject(
		context.Context,
		*s3.GetObjectInput,
		...func(*s3.Options),
	) (*s3.GetObjectOutput, error)
}

type S3ObjectVerifier struct {
	client         S3GetObjectClient
	expectedBucket string
}

func NewS3ObjectVerifier(client S3GetObjectClient) S3ObjectVerifier {
	return S3ObjectVerifier{client: client}
}

func NewS3ObjectVerifierForBucket(client S3GetObjectClient, expectedBucket string) S3ObjectVerifier {
	return S3ObjectVerifier{client: client, expectedBucket: expectedBucket}
}

func (verifier S3ObjectVerifier) Verify(
	ctx context.Context,
	object types.TargetConfigSnapshotObject,
) (types.VerifiedTargetConfigObject, error) {
	if verifier.client == nil {
		return types.VerifiedTargetConfigObject{}, errors.New("S3 object verifier is not configured")
	}
	parsed, err := url.Parse(object.Reference)
	if err != nil || parsed.Scheme != "s3" || parsed.Host == "" {
		return types.VerifiedTargetConfigObject{}, errors.New("object reference is invalid")
	}
	if verifier.expectedBucket != "" && parsed.Host != verifier.expectedBucket {
		return types.VerifiedTargetConfigObject{}, errors.New("object reference is outside configured bucket")
	}
	input := &s3.GetObjectInput{
		Bucket: aws.String(parsed.Host),
		Key:    aws.String(strings.TrimPrefix(parsed.Path, "/")),
	}
	if object.VersionID != "" {
		input.VersionId = aws.String(object.VersionID)
	}
	output, err := verifier.client.GetObject(ctx, input)
	if err != nil {
		return types.VerifiedTargetConfigObject{}, errors.New("object provider verification failed")
	}
	if output == nil || output.Body == nil {
		return types.VerifiedTargetConfigObject{}, errors.New("object provider returned no body")
	}
	defer output.Body.Close()
	providerVersionID := aws.ToString(output.VersionId)
	providerMediaType := aws.ToString(output.ContentType)
	if !validObservedVersionID(providerVersionID) || !validObservedMediaType(providerMediaType) {
		return types.VerifiedTargetConfigObject{}, errors.New("object provider returned invalid metadata")
	}

	limit := int64(maxTargetConfigObjectSize) + 1
	hash := sha256.New()
	size, err := io.Copy(hash, io.LimitReader(output.Body, limit))
	if err != nil {
		return types.VerifiedTargetConfigObject{}, errors.New("object provider body could not be verified")
	}
	if size > maxTargetConfigObjectSize {
		return types.VerifiedTargetConfigObject{}, errors.New("object exceeds verification limit")
	}
	if output.ContentLength != nil && *output.ContentLength != size {
		return types.VerifiedTargetConfigObject{}, fmt.Errorf("object provider size metadata mismatch")
	}
	observedVersionID := ""
	if object.VersionID != "" {
		observedVersionID = providerVersionID
	}
	return types.VerifiedTargetConfigObject{
		Reference: object.Reference,
		VersionID: observedVersionID,
		MediaType: providerMediaType,
		SizeBytes: size,
		Checksum:  "sha256:" + hex.EncodeToString(hash.Sum(nil)),
	}, nil
}
