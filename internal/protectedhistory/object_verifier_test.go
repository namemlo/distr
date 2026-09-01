package protectedhistory

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	. "github.com/onsi/gomega"
)

type protectedHistoryS3ClientFunc func(
	context.Context,
	*s3.GetObjectInput,
	...func(*s3.Options),
) (*s3.GetObjectOutput, error)

func (function protectedHistoryS3ClientFunc) GetObject(
	ctx context.Context,
	input *s3.GetObjectInput,
	options ...func(*s3.Options),
) (*s3.GetObjectOutput, error) {
	return function(ctx, input, options...)
}

type protectedHistoryS3ObjectClient struct {
	get func(
		context.Context,
		*s3.GetObjectInput,
		...func(*s3.Options),
	) (*s3.GetObjectOutput, error)
	put func(
		context.Context,
		*s3.PutObjectInput,
		...func(*s3.Options),
	) (*s3.PutObjectOutput, error)
}

func (client protectedHistoryS3ObjectClient) GetObject(
	ctx context.Context,
	input *s3.GetObjectInput,
	options ...func(*s3.Options),
) (*s3.GetObjectOutput, error) {
	return client.get(ctx, input, options...)
}

func (client protectedHistoryS3ObjectClient) PutObject(
	ctx context.Context,
	input *s3.PutObjectInput,
	options ...func(*s3.Options),
) (*s3.PutObjectOutput, error) {
	return client.put(ctx, input, options...)
}

func TestS3ObjectVerifierReadsBackExactProtectedHistoryIdentity(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	payload := []byte("protected-history")
	checksum := ContentChecksum(payload)
	expected := ObjectIdentity{
		Reference: immutableProtectedHistoryReference(checksum),
		MediaType: ArtifactMediaTypeV1, ByteLength: int64(len(payload)),
		Checksum: checksum,
	}
	verifier := NewS3ObjectVerifier(protectedHistoryS3ClientFunc(func(
		_ context.Context,
		input *s3.GetObjectInput,
		_ ...func(*s3.Options),
	) (*s3.GetObjectOutput, error) {
		g.Expect(aws.ToString(input.Bucket)).To(Equal("history"))
		g.Expect(aws.ToString(input.Key)).To(ContainSubstring("_immutable/sha256/"))
		return &s3.GetObjectOutput{
			Body:          io.NopCloser(strings.NewReader(string(payload))),
			ContentLength: aws.Int64(int64(len(payload))), ContentType: aws.String(ArtifactMediaTypeV1),
		}, nil
	}))

	observed, err := verifier.Readback(context.Background(), expected)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(observed).To(Equal(expected))
	g.Expect(VerifyObjectIdentity(expected, observed)).To(Succeed())
}

func TestVerifyObjectIdentityRejectsEveryReadbackMismatch(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	checksum := ContentChecksum([]byte("protected-history"))
	expected := ObjectIdentity{
		Reference: immutableProtectedHistoryReference(checksum), MediaType: ArtifactMediaTypeV1,
		ByteLength: 17, Checksum: checksum,
	}
	mutations := []func(*ObjectIdentity){
		func(identity *ObjectIdentity) { identity.Reference += ".copy" },
		func(identity *ObjectIdentity) { identity.MediaType = "application/json" },
		func(identity *ObjectIdentity) { identity.ByteLength++ },
		func(identity *ObjectIdentity) { identity.Checksum = checksumOfString("other") },
	}
	for _, mutate := range mutations {
		observed := expected
		mutate(&observed)
		g.Expect(VerifyObjectIdentity(expected, observed)).To(HaveOccurred())
	}
}

func TestS3ObjectStoreCreatesOnceAndReplaysIdenticalBytes(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	payload := []byte("protected-history")
	objects := map[string][]byte{}
	client := protectedHistoryS3ObjectClient{}
	client.put = func(
		_ context.Context,
		input *s3.PutObjectInput,
		_ ...func(*s3.Options),
	) (*s3.PutObjectOutput, error) {
		g.Expect(aws.ToString(input.IfNoneMatch)).To(Equal("*"))
		key := aws.ToString(input.Bucket) + "/" + aws.ToString(input.Key)
		if _, exists := objects[key]; exists {
			return nil, errors.New("precondition failed")
		}
		body, err := io.ReadAll(input.Body)
		g.Expect(err).NotTo(HaveOccurred())
		objects[key] = body
		return &s3.PutObjectOutput{}, nil
	}
	client.get = func(
		_ context.Context,
		input *s3.GetObjectInput,
		_ ...func(*s3.Options),
	) (*s3.GetObjectOutput, error) {
		body := objects[aws.ToString(input.Bucket)+"/"+aws.ToString(input.Key)]
		return &s3.GetObjectOutput{
			Body:          io.NopCloser(strings.NewReader(string(body))),
			ContentLength: aws.Int64(int64(len(body))),
			ContentType:   aws.String(ArtifactMediaTypeV1),
		}, nil
	}
	store := NewS3ObjectStore(client, "history")

	created, err := store.WriteOnce(context.Background(), payload)
	g.Expect(err).NotTo(HaveOccurred())
	replayed, err := store.WriteOnce(context.Background(), payload)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(replayed).To(Equal(created))
	g.Expect(objects).To(HaveLen(1))
}

func TestS3ObjectStoreRejectsChangedBytesAtChecksumAddress(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	client := protectedHistoryS3ObjectClient{
		put: func(
			context.Context,
			*s3.PutObjectInput,
			...func(*s3.Options),
		) (*s3.PutObjectOutput, error) {
			return nil, errors.New("precondition failed")
		},
		get: func(
			context.Context,
			*s3.GetObjectInput,
			...func(*s3.Options),
		) (*s3.GetObjectOutput, error) {
			body := []byte("changed-history")
			return &s3.GetObjectOutput{
				Body:          io.NopCloser(strings.NewReader(string(body))),
				ContentLength: aws.Int64(int64(len(body))),
				ContentType:   aws.String(ArtifactMediaTypeV1),
			}, nil
		},
	}

	_, err := NewS3ObjectStore(client, "history").WriteOnce(
		context.Background(),
		[]byte("protected-history"),
	)
	g.Expect(err).To(MatchError(ErrObjectConflict))
}

func TestS3ObjectVerifierRejectsReferenceOutsideConfiguredBucket(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	checksum := ContentChecksum([]byte("protected-history"))
	verifier := NewS3ObjectVerifierForBucket(
		protectedHistoryS3ClientFunc(func(
			context.Context,
			*s3.GetObjectInput,
			...func(*s3.Options),
		) (*s3.GetObjectOutput, error) {
			t.Fatal("out-of-bucket reference must fail before provider access")
			return nil, nil
		}),
		"configured-history",
	)

	_, err := verifier.Readback(context.Background(), ObjectIdentity{
		Reference:  immutableProtectedHistoryReference(checksum),
		MediaType:  ArtifactMediaTypeV1,
		ByteLength: int64(len("protected-history")),
		Checksum:   checksum,
	})
	g.Expect(err).To(MatchError(ContainSubstring("outside the configured bucket")))
}
