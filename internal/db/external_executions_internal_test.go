package db

import (
	"net/url"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

func TestExternalExecutionObjectReferenceReplacesExistingVersionID(t *testing.T) {
	g := NewWithT(t)
	checksum := "sha256:" + strings.Repeat("a", 64)
	contract := &types.ReleaseContract{Config: types.ReleaseContractConfig{
		ImmutableObjects: []types.ReleaseContractConfigObject{{
			URI:       "s3://config-bucket/service.json?region=ap-southeast-1&versionId=stale#config",
			VersionID: "v 42", Checksum: checksum,
		}},
	}}

	reference, err := externalExecutionObjectReference(contract, checksum)

	g.Expect(err).NotTo(HaveOccurred())
	parsed, err := url.Parse(reference)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(parsed.Query()["versionId"]).To(Equal([]string{"v 42"}))
	g.Expect(parsed.Query().Get("region")).To(Equal("ap-southeast-1"))
	g.Expect(parsed.Fragment).To(Equal("config"))
}

func TestExternalExecutionObjectReferenceAcceptsContentAddressedObject(t *testing.T) {
	g := NewWithT(t)
	checksum := "sha256:" + strings.Repeat("a", 64)
	uri := "s3://config-bucket/_immutable/sha256/" + strings.Repeat("a", 64) + "/service.json"
	contract := &types.ReleaseContract{Config: types.ReleaseContractConfig{
		ImmutableObjects: []types.ReleaseContractConfigObject{{URI: uri, Checksum: checksum}},
	}}

	reference, err := externalExecutionObjectReference(contract, checksum)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(reference).To(Equal(uri))
}
