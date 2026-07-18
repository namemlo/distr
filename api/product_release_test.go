package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestCreateProductReleaseRequestValidate(t *testing.T) {
	g := NewWithT(t)
	componentReleaseID := uuid.New()
	request := CreateProductReleaseRequest{
		ApplicationID:           uuid.New(),
		ChannelID:               uuid.New(),
		Product:                 " neutral-suite ",
		Version:                 " 2026.07.14.1 ",
		DependencyPolicyVersion: uuid.New(),
		RequiredPlatforms:       []string{" linux/amd64 "},
		Components: []ProductReleaseComponentRequest{{
			ComponentReleaseID:       componentReleaseID,
			ComponentReleaseChecksum: " sha256:" + strings.Repeat("a", 64) + " ",
		}},
		Requirements: []types.CapabilityRequirement{{
			Name: " transactions ", Range: " ^1.0.0 ", ResolutionStage: " target ",
			AllowedModes: []string{" shared_provider "},
		}},
	}

	g.Expect(request.Validate()).To(Succeed())
	g.Expect(request.Schema).To(Equal(types.ProductReleaseSchemaV1))
	g.Expect(request.Product).To(Equal("neutral-suite"))
	g.Expect(request.Version).To(Equal("2026.07.14.1"))
	g.Expect(request.RequiredPlatforms).To(Equal([]string{"linux/amd64"}))
	g.Expect(request.Components[0].ComponentReleaseChecksum).To(Equal(
		"sha256:" + strings.Repeat("a", 64),
	))
	g.Expect(request.Requirements[0].AllowedModes).To(Equal([]string{"shared_provider"}))
}

func TestProductReleaseContractRejectsUnknownFields(t *testing.T) {
	g := NewWithT(t)
	raw := `{
		"schema":"distr.product-release/v1",
		"product":"neutral-suite",
		"version":"1.2.3",
		"dependencyPolicyVersion":"` + uuid.NewString() + `",
		"releaseNotes":"",
		"requiredPlatforms":[],
		"components":[],
		"requirements":[],
		"graphChecksum":"sha256:` + strings.Repeat("a", 64) + `",
		"targetId":"` + uuid.NewString() + `"
	}`
	var contract types.ReleaseContract
	g.Expect(json.Unmarshal([]byte(raw), &contract)).To(MatchError(ContainSubstring("unknown field")))
}

func TestGenericReleaseBundleRejectsProductReleaseContract(t *testing.T) {
	g := NewWithT(t)
	request := CreateUpdateReleaseBundleRequest{
		ApplicationID: uuid.New(),
		ChannelID:     uuid.New(),
		ReleaseNumber: "2026.07.14.1",
		ReleaseContract: &types.ReleaseContract{
			ProductV1: &types.ProductReleaseManifest{
				Schema:  types.ProductReleaseSchemaV1,
				Product: "neutral-suite",
				Version: "2026.07.14.1",
			},
		},
		Components: []ReleaseBundleComponentRequest{{
			Key: "api", Type: types.ReleaseBundleComponentTypeOCIImage, Version: "1.0.0",
			PackageRef: "registry.invalid/api", Digest: "sha256:" + strings.Repeat("a", 64),
		}},
	}
	g.Expect(request.Validate()).To(MatchError(ContainSubstring("product-releases endpoint")))
}

func TestCreateProductReleaseRequestRejectsDuplicateAndMutablePins(t *testing.T) {
	componentReleaseID := uuid.New()
	base := CreateProductReleaseRequest{
		ApplicationID:           uuid.New(),
		ChannelID:               uuid.New(),
		Product:                 "neutral-suite",
		Version:                 "1.2.3",
		DependencyPolicyVersion: uuid.New(),
		Components: []ProductReleaseComponentRequest{{
			ComponentReleaseID:       componentReleaseID,
			ComponentReleaseChecksum: "sha256:" + strings.Repeat("a", 64),
		}},
	}

	t.Run("duplicate release id", func(t *testing.T) {
		g := NewWithT(t)
		request := base
		request.Components = append(request.Components, request.Components[0])
		g.Expect(request.Validate()).To(MatchError(ContainSubstring("must be unique")))
	})
	t.Run("mutable checksum", func(t *testing.T) {
		g := NewWithT(t)
		request := base
		request.Components[0].ComponentReleaseChecksum = "registry.invalid/component:latest"
		g.Expect(request.Validate()).To(MatchError(ContainSubstring("lowercase sha256")))
	})
}

func TestCreateProductReleaseRequestRejectsBoundedCollectionsAndIndexedVersion(t *testing.T) {
	valid := func() CreateProductReleaseRequest {
		return CreateProductReleaseRequest{
			ApplicationID:           uuid.New(),
			ChannelID:               uuid.New(),
			Product:                 "neutral-suite",
			Version:                 "1.2.3",
			DependencyPolicyVersion: uuid.New(),
			Components: []ProductReleaseComponentRequest{{
				ComponentReleaseID:       uuid.New(),
				ComponentReleaseChecksum: "sha256:" + strings.Repeat("a", 64),
			}},
		}
	}
	tests := []struct {
		name   string
		mutate func(*CreateProductReleaseRequest)
		want   string
	}{
		{
			name: "indexed version bytes",
			mutate: func(request *CreateProductReleaseRequest) {
				request.Version = strings.Repeat("v", types.ProductReleaseMaxVersionBytes+1)
			},
			want: "version is too long",
		},
		{
			name: "components",
			mutate: func(request *CreateProductReleaseRequest) {
				request.Components = make(
					[]ProductReleaseComponentRequest,
					types.ProductReleaseMaxComponents+1,
				)
			},
			want: "too many component releases",
		},
		{
			name: "requirements",
			mutate: func(request *CreateProductReleaseRequest) {
				request.Requirements = make(
					[]types.CapabilityRequirement,
					types.ProductReleaseMaxRequirements+1,
				)
			},
			want: "too many product requirements",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			request := valid()
			tt.mutate(&request)
			g.Expect(request.Validate()).To(MatchError(ContainSubstring(tt.want)))
		})
	}
}
