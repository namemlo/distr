package svc

import (
	"context"
	"testing"

	"github.com/distr-sh/distr/internal/env"
	"github.com/distr-sh/distr/internal/protectedhistory"
	. "github.com/onsi/gomega"
)

func TestNewProtectedHistoryObjectStoreFailsClosedWhenUnconfigured(t *testing.T) {
	g := NewWithT(t)
	store := newProtectedHistoryObjectStoreWithConfig(
		context.Background(),
		env.ProtectedHistoryObjectStoreConfig{},
	)

	_, err := store.WriteOnce(context.Background(), []byte("history"))
	g.Expect(err).To(MatchError(protectedhistory.ErrObjectVerificationUnavailable))
}

func TestNewProtectedHistoryObjectStoreUsesDedicatedConfiguration(t *testing.T) {
	g := NewWithT(t)
	store := newProtectedHistoryObjectStoreWithConfig(
		context.Background(),
		env.ProtectedHistoryObjectStoreConfig{
			Enabled: true,
			S3: env.S3Config{
				Region: "ap-southeast-1",
				Bucket: "protected-history",
			},
		},
	)

	g.Expect(store).To(BeAssignableToTypeOf(protectedhistory.S3ObjectStore{}))
}
