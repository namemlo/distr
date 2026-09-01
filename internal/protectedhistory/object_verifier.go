package protectedhistory

import (
	"bytes"
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
)

var ErrObjectVerificationUnavailable = errors.New("protected-history object verification is unavailable")
var ErrObjectConflict = errors.New("protected-history immutable object conflicts with retained identity")

type ObjectIdentity struct {
	Reference  string `json:"reference"`
	MediaType  string `json:"mediaType"`
	ByteLength int64  `json:"byteLength"`
	Checksum   string `json:"checksum"`
}

type ObjectVerifier interface {
	Readback(context.Context, ObjectIdentity) (ObjectIdentity, error)
}

type ObjectStore interface {
	WriteOnce(context.Context, []byte) (ObjectIdentity, error)
	ObjectVerifier
}

type S3GetObjectClient interface {
	GetObject(
		context.Context,
		*s3.GetObjectInput,
		...func(*s3.Options),
	) (*s3.GetObjectOutput, error)
}

type S3PutObjectClient interface {
	PutObject(
		context.Context,
		*s3.PutObjectInput,
		...func(*s3.Options),
	) (*s3.PutObjectOutput, error)
}

type S3ObjectClient interface {
	S3GetObjectClient
	S3PutObjectClient
}

type S3ObjectVerifier struct {
	client         S3GetObjectClient
	expectedBucket string
}

func NewS3ObjectVerifier(client S3GetObjectClient) ObjectVerifier {
	if client == nil {
		return unavailableObjectVerifier{}
	}
	return S3ObjectVerifier{client: client}
}

func NewS3ObjectVerifierForBucket(client S3GetObjectClient, bucket string) ObjectVerifier {
	if client == nil || strings.TrimSpace(bucket) == "" {
		return unavailableObjectVerifier{}
	}
	return S3ObjectVerifier{client: client, expectedBucket: bucket}
}

type S3ObjectStore struct {
	client S3ObjectClient
	bucket string
}

func NewS3ObjectStore(client S3ObjectClient, bucket string) ObjectStore {
	if client == nil || strings.TrimSpace(bucket) == "" {
		return unavailableObjectStore{}
	}
	return S3ObjectStore{client: client, bucket: bucket}
}

func NewUnavailableObjectStore() ObjectStore {
	return unavailableObjectStore{}
}

type unavailableObjectVerifier struct{}

type unavailableObjectStore struct{}

func (unavailableObjectVerifier) Readback(
	context.Context,
	ObjectIdentity,
) (ObjectIdentity, error) {
	return ObjectIdentity{}, ErrObjectVerificationUnavailable
}

func (unavailableObjectStore) WriteOnce(context.Context, []byte) (ObjectIdentity, error) {
	return ObjectIdentity{}, ErrObjectVerificationUnavailable
}

func (unavailableObjectStore) Readback(
	context.Context,
	ObjectIdentity,
) (ObjectIdentity, error) {
	return ObjectIdentity{}, ErrObjectVerificationUnavailable
}

func (store S3ObjectStore) WriteOnce(ctx context.Context, payload []byte) (ObjectIdentity, error) {
	if len(payload) < 1 || len(payload) > maximumArtifactBytes {
		return ObjectIdentity{}, errors.New("protected-history object body is outside the storage limit")
	}
	identity := ObjectIdentityForPayload(store.bucket, payload)
	parsed, _ := url.Parse(identity.Reference)
	_, err := store.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(parsed.Host),
		Key:           aws.String(strings.TrimPrefix(parsed.Path, "/")),
		Body:          bytes.NewReader(payload),
		ContentLength: aws.Int64(identity.ByteLength),
		ContentType:   aws.String(identity.MediaType),
		IfNoneMatch:   aws.String("*"),
	})
	if err == nil {
		return identity, nil
	}
	observed, readErr := store.Readback(ctx, identity)
	if readErr == nil && VerifyObjectIdentity(identity, observed) == nil {
		return identity, nil
	}
	if readErr == nil {
		return ObjectIdentity{}, ErrObjectConflict
	}
	return ObjectIdentity{}, errors.New("protected-history object provider write failed")
}

func (store S3ObjectStore) Readback(
	ctx context.Context,
	expected ObjectIdentity,
) (ObjectIdentity, error) {
	return S3ObjectVerifier{client: store.client, expectedBucket: store.bucket}.Readback(ctx, expected)
}

func ObjectIdentityForPayload(bucket string, payload []byte) ObjectIdentity {
	checksum := ContentChecksum(payload)
	digest := strings.TrimPrefix(checksum, "sha256:")
	return ObjectIdentity{
		Reference:  fmt.Sprintf("s3://%s/_immutable/sha256/%s/protected-history.json", bucket, digest),
		MediaType:  ArtifactMediaTypeV1,
		ByteLength: int64(len(payload)),
		Checksum:   checksum,
	}
}

func (verifier S3ObjectVerifier) Readback(
	ctx context.Context,
	expected ObjectIdentity,
) (ObjectIdentity, error) {
	if !validImmutableObjectReference(expected.Reference, expected.Checksum) ||
		expected.MediaType != ArtifactMediaTypeV1 || expected.ByteLength < 1 ||
		expected.ByteLength > maximumArtifactBytes {
		return ObjectIdentity{}, errors.New("protected-history object identity is invalid")
	}
	parsed, err := url.Parse(expected.Reference)
	if err != nil {
		return ObjectIdentity{}, errors.New("protected-history object reference is invalid")
	}
	if verifier.expectedBucket != "" && parsed.Host != verifier.expectedBucket {
		return ObjectIdentity{}, errors.New("protected-history object reference is outside the configured bucket")
	}
	output, err := verifier.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(parsed.Host), Key: aws.String(strings.TrimPrefix(parsed.Path, "/")),
	})
	if err != nil {
		return ObjectIdentity{}, errors.New("protected-history object provider readback failed")
	}
	if output == nil || output.Body == nil {
		return ObjectIdentity{}, errors.New("protected-history object provider returned no body")
	}
	defer output.Body.Close()

	hash := sha256.New()
	length, err := io.Copy(hash, io.LimitReader(output.Body, maximumArtifactBytes+1))
	if err != nil {
		return ObjectIdentity{}, errors.New("protected-history object body could not be read back")
	}
	if length > maximumArtifactBytes {
		return ObjectIdentity{}, errors.New("protected-history object exceeds the verification limit")
	}
	if output.ContentLength != nil && aws.ToInt64(output.ContentLength) != length {
		return ObjectIdentity{}, errors.New("protected-history object size metadata mismatch")
	}
	return ObjectIdentity{
		Reference: expected.Reference, MediaType: aws.ToString(output.ContentType), ByteLength: length,
		Checksum: "sha256:" + hex.EncodeToString(hash.Sum(nil)),
	}, nil
}

func VerifyObjectIdentity(expected, observed ObjectIdentity) error {
	switch {
	case observed.Reference != expected.Reference:
		return errors.New("protected-history object reference readback mismatch")
	case observed.MediaType != expected.MediaType:
		return errors.New("protected-history object media type readback mismatch")
	case observed.ByteLength != expected.ByteLength:
		return errors.New("protected-history object byte length readback mismatch")
	case observed.Checksum != expected.Checksum:
		return errors.New("protected-history object checksum readback mismatch")
	default:
		return nil
	}
}
