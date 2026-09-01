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
	observed, _, err := verifier.read(ctx, object, maxTargetConfigObjectSize, false)
	return observed, err
}

func (verifier S3ObjectVerifier) Read(
	ctx context.Context,
	object types.TargetConfigSnapshotObject,
	maxBytes int64,
) (types.VerifiedTargetConfigObject, []byte, error) {
	if maxBytes < 1 || maxBytes > maxTargetConfigObjectSize {
		return types.VerifiedTargetConfigObject{}, nil, errors.New("object read limit is invalid")
	}
	return verifier.read(ctx, object, maxBytes, true)
}

func (verifier S3ObjectVerifier) read(
	ctx context.Context,
	object types.TargetConfigSnapshotObject,
	maxBytes int64,
	retainBody bool,
) (types.VerifiedTargetConfigObject, []byte, error) {
	if verifier.client == nil {
		return types.VerifiedTargetConfigObject{}, nil, errors.New("S3 object verifier is not configured")
	}
	parsed, err := url.Parse(object.Reference)
	if err != nil || parsed.Scheme != "s3" || parsed.Host == "" {
		return types.VerifiedTargetConfigObject{}, nil, errors.New("object reference is invalid")
	}
	if verifier.expectedBucket != "" && parsed.Host != verifier.expectedBucket {
		return types.VerifiedTargetConfigObject{}, nil, errors.New("object reference is outside configured bucket")
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
		return types.VerifiedTargetConfigObject{}, nil, errors.New("object provider verification failed")
	}
	if output == nil || output.Body == nil {
		return types.VerifiedTargetConfigObject{}, nil, errors.New("object provider returned no body")
	}
	defer output.Body.Close()
	providerVersionID := aws.ToString(output.VersionId)
	providerMediaType := aws.ToString(output.ContentType)
	if !validObservedVersionID(providerVersionID) || !validObservedMediaType(providerMediaType) {
		return types.VerifiedTargetConfigObject{}, nil, errors.New("object provider returned invalid metadata")
	}

	limit := maxBytes + 1
	hash := sha256.New()
	var body []byte
	var size int64
	if retainBody {
		body, err = io.ReadAll(io.LimitReader(output.Body, limit))
		size = int64(len(body))
		if err == nil {
			_, err = hash.Write(body)
		}
	} else {
		size, err = io.Copy(hash, io.LimitReader(output.Body, limit))
	}
	if err != nil {
		return types.VerifiedTargetConfigObject{}, nil, errors.New("object provider body could not be verified")
	}
	if size > maxBytes {
		return types.VerifiedTargetConfigObject{}, nil, errors.New("object exceeds verification limit")
	}
	if output.ContentLength != nil && *output.ContentLength != size {
		return types.VerifiedTargetConfigObject{}, nil, fmt.Errorf("object provider size metadata mismatch")
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
	}, body, nil
}
