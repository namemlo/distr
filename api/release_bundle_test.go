package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/releasebundles"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestCreateUpdateReleaseBundleRequestValidateTrimsFields(t *testing.T) {
	g := NewWithT(t)
	applicationID := uuid.New()
	channelID := uuid.New()
	versionID := uuid.New()
	revisionID := uuid.New()

	request := CreateUpdateReleaseBundleRequest{
		ApplicationID:               applicationID,
		ChannelID:                   channelID,
		DeploymentProcessRevisionID: &revisionID,
		ReleaseNumber:               " 1.2.3 ",
		ReleaseNotes:                " release notes ",
		SourceRevision:              " abc123 ",
		Components: []ReleaseBundleComponentRequest{
			{
				Key:                  " api ",
				Name:                 " API ",
				Type:                 types.ReleaseBundleComponentTypeApplicationVersion,
				Version:              " 1.2.3 ",
				ApplicationVersionID: &versionID,
			},
		},
	}

	g.Expect(request.Validate()).To(Succeed())

	g.Expect(request.ReleaseNumber).To(Equal("1.2.3"))
	g.Expect(request.SourceRevision).To(Equal("abc123"))
	g.Expect(request.DeploymentProcessRevisionID).To(Equal(&revisionID))
	g.Expect(request.Components[0].Key).To(Equal("api"))
	g.Expect(request.Components[0].Name).To(Equal("API"))
	g.Expect(request.Components[0].Version).To(Equal("1.2.3"))
}

func TestCreateUpdateReleaseBundleRequestValidateTrimsSourceMetadata(t *testing.T) {
	g := NewWithT(t)
	versionID := uuid.New()
	request := CreateUpdateReleaseBundleRequest{
		ApplicationID:  uuid.New(),
		ChannelID:      uuid.New(),
		ReleaseNumber:  "2026.06.20",
		SourceRevision: " abc123 ",
		SourceMetadata: &ReleaseBundleSourceMetadata{
			Repository: " https://example.invalid/org/project ",
			Branch:     " main ",
			Tag:        " v1.2.3 ",
			CIProvider: " generic-ci ",
			CIRunID:    " run-123 ",
			CIRunURL:   " https://ci.example.invalid/runs/123 ",
		},
		Components: []ReleaseBundleComponentRequest{
			{
				Key:                  "api",
				Type:                 types.ReleaseBundleComponentTypeApplicationVersion,
				Version:              "1.2.3",
				ApplicationVersionID: &versionID,
			},
		},
	}

	g.Expect(request.Validate()).To(Succeed())

	g.Expect(request.SourceRevision).To(Equal("abc123"))
	g.Expect(request.SourceMetadata).NotTo(BeNil())
	g.Expect(request.SourceMetadata.Repository).To(Equal("https://example.invalid/org/project"))
	g.Expect(request.SourceMetadata.Branch).To(Equal("main"))
	g.Expect(request.SourceMetadata.Tag).To(Equal("v1.2.3"))
	g.Expect(request.SourceMetadata.CIProvider).To(Equal("generic-ci"))
	g.Expect(request.SourceMetadata.CIRunID).To(Equal("run-123"))
	g.Expect(request.SourceMetadata.CIRunURL).To(Equal("https://ci.example.invalid/runs/123"))
}

func TestCreateUpdateReleaseBundleRequestValidateOCIComponentsRequireFullSHA256Digest(t *testing.T) {
	applicationID := uuid.New()
	channelID := uuid.New()
	validDigest := "sha256:" + strings.Repeat("a", 64)

	tests := []struct {
		name    string
		digest  string
		wantErr bool
	}{
		{name: "valid lowercase digest", digest: validDigest},
		{name: "valid uppercase digest", digest: "sha256:" + strings.Repeat("A", 64)},
		{name: "missing hex", digest: "sha256:", wantErr: true},
		{name: "too short", digest: "sha256:" + strings.Repeat("a", 63), wantErr: true},
		{name: "too long", digest: "sha256:" + strings.Repeat("a", 65), wantErr: true},
		{name: "non hex", digest: "sha256:" + strings.Repeat("g", 64), wantErr: true},
		{name: "mutable tag", digest: "registry.example/api:latest", wantErr: true},
		{name: "unsupported algorithm", digest: "sha512:" + strings.Repeat("a", 128), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			request := CreateUpdateReleaseBundleRequest{
				ApplicationID: applicationID,
				ChannelID:     channelID,
				ReleaseNumber: "2026.06.20",
				Components: []ReleaseBundleComponentRequest{
					{
						Key:        "api-image",
						Type:       types.ReleaseBundleComponentTypeOCIImage,
						Version:    "1.2.3",
						PackageRef: "registry.example/api",
						Digest:     tt.digest,
					},
				},
			}

			err := request.Validate()

			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(request.Components[0].Digest).To(Equal(tt.digest))
			}
		})
	}
}

func TestCreateUpdateReleaseBundleRequestValidateRejectsUnsafeSourceMetadata(t *testing.T) {
	versionID := uuid.New()
	base := CreateUpdateReleaseBundleRequest{
		ApplicationID:  uuid.New(),
		ChannelID:      uuid.New(),
		ReleaseNumber:  "2026.06.20",
		SourceRevision: "abc123",
		Components: []ReleaseBundleComponentRequest{
			{
				Key:                  "api",
				Type:                 types.ReleaseBundleComponentTypeApplicationVersion,
				Version:              "1.2.3",
				ApplicationVersionID: &versionID,
			},
		},
	}

	tests := []struct {
		name     string
		metadata ReleaseBundleSourceMetadata
	}{
		{
			name: "repository too long",
			metadata: ReleaseBundleSourceMetadata{
				Repository: strings.Repeat("r", 513),
			},
		},
		{
			name: "run url too long",
			metadata: ReleaseBundleSourceMetadata{
				CIRunURL: strings.Repeat("u", 2049),
			},
		},
		{
			name: "authorization header value",
			metadata: ReleaseBundleSourceMetadata{
				CIProvider: "Authorization: Bearer secret",
			},
		},
		{
			name: "access token value",
			metadata: ReleaseBundleSourceMetadata{
				CIRunID: "AccessToken distr-secret",
			},
		},
		{
			name: "credential bearing run URL",
			metadata: ReleaseBundleSourceMetadata{
				CIRunURL: "https://user:password@ci.example.invalid/runs/123",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			request := base
			request.SourceMetadata = &tt.metadata

			err := request.Validate()

			g.Expect(err).To(HaveOccurred())
		})
	}
}

func TestCreateUpdateReleaseBundleRequestValidateReleaseContract(t *testing.T) {
	g := NewWithT(t)
	digest := "sha256:" + strings.Repeat("a", 64)
	checksum := "sha256:" + strings.Repeat("b", 64)
	request := CreateUpdateReleaseBundleRequest{
		ApplicationID: uuid.New(),
		ChannelID:     uuid.New(),
		ReleaseNumber: "choice-tp-loyalty-42",
		ReleaseContract: &types.ReleaseContract{
			Schema: types.ReleaseContractSchemaV1,
			Source: types.ReleaseContractSource{
				Repository:   "remittance-b2c-backend",
				Branch:       "customization/emlo-remittance/dev",
				SourceCommit: "1111111111111111111111111111111111111111",
				BuiltCommit:  "1111111111111111111111111111111111111111",
			},
			Build: types.ReleaseContractBuild{ExternalID: "42", ExternalURL: "https://ci.example/job/42"},
			Components: []types.ReleaseContractComponent{{
				Name: "loyalty-api", Version: "1.2.3",
				Image: "registry.example/loyalty-api@" + digest, Platform: "linux/amd64",
			}},
			Compatibility: types.ReleaseContractCompatibility{AffectedComponents: []string{"loyalty-api"}},
			Config: types.ReleaseContractConfig{
				RepositoryCommit:      "2222222222222222222222222222222222222222",
				ComposePath:           "choice-tp_dev/1/docker-compose.yaml",
				ServiceConfigPath:     "choice-tp_dev/1/rmt-loyalty-api/appsettings.Production.json",
				ComposeChecksum:       checksum,
				ServiceConfigChecksum: checksum,
			},
			Changes: types.ReleaseContractChanges{Summary: "Deploy loyalty API"},
		},
		Components: []ReleaseBundleComponentRequest{{
			Key: "loyalty-api", Type: types.ReleaseBundleComponentTypeOCIImage,
			Version: "1.2.3", PackageRef: "registry.example/loyalty-api", Digest: digest,
		}},
	}

	g.Expect(request.Validate()).To(Succeed())
	g.Expect(request.ReleaseContract.Components[0].Platform).To(Equal("linux/amd64"))

	request.ReleaseContract.Components[0].Image = "registry.example/loyalty-api:latest"
	g.Expect(request.Validate()).To(MatchError(ContainSubstring("immutable image digest")))
}

func TestCreateUpdateReleaseBundleRequestAcceptsStrictComponentReleaseV2(t *testing.T) {
	g := NewWithT(t)
	digest := "sha256:" + strings.Repeat("a", 64)
	platformDigest := "sha256:" + strings.Repeat("b", 64)
	raw := `{
		"applicationId":"` + uuid.NewString() + `",
		"channelId":"` + uuid.NewString() + `",
		"releaseNumber":"2.4.0",
		"releaseNotes":"target-neutral component build",
		"sourceRevision":"0123456789abcdef0123456789abcdef01234567",
		"releaseContract":{
			"schema":"distr.component-release/v2",
			"componentKey":"payments.api",
			"version":"2.4.0",
			"source":{
				"repository":"source/payments-api",
				"requestedRef":"refs/tags/v2.4.0",
				"commit":"0123456789abcdef0123456789abcdef01234567"
			},
			"build":{"id":"build-42","builder":"generic-ci"},
			"artifacts":[{
				"key":"image",
				"type":"oci-image",
				"mediaType":"application/vnd.oci.image.index.v1+json",
				"digest":"` + digest + `",
				"platforms":[{"platform":"linux/amd64","digest":"` + platformDigest + `"}]
			}],
			"provides":[{"name":"payments.api","version":"2.4.0"}],
			"requires":[],
			"migrations":[],
			"changes":{"summary":"release","commits":["0123456789abcdef0123456789abcdef01234567"]},
			"evidence":{"provenance":[],"sbom":[],"signatures":[],"tests":[]}
		},
		"components":[{
			"key":"image",
			"name":"Payments API",
			"type":"oci_image",
			"version":"2.4.0",
			"packageRef":"registry.example/payments-api",
			"digest":"` + digest + `",
			"checksum":""
		}]
	}`
	var request CreateUpdateReleaseBundleRequest

	g.Expect(json.Unmarshal([]byte(raw), &request)).To(Succeed())
	g.Expect(request.Validate()).To(Succeed())

	g.Expect(request.ReleaseContract).NotTo(BeNil())
	g.Expect(request.ReleaseContract.ComponentV2).NotTo(BeNil())
	g.Expect(request.ReleaseContract.ComponentV2.Source.RequestedRef).To(Equal("refs/tags/v2.4.0"))
	g.Expect(request.ReleaseContract.ComponentV2.Source.Commit).To(Equal(
		"0123456789abcdef0123456789abcdef01234567",
	))
	g.Expect(request.SourceRevision).To(Equal("0123456789abcdef0123456789abcdef01234567"))
	g.Expect(request.SourceMetadata).NotTo(BeNil())
	g.Expect(request.SourceMetadata.Repository).To(Equal("source/payments-api"))
	g.Expect(request.SourceMetadata.Branch).To(BeEmpty())
	g.Expect(request.SourceMetadata.Tag).To(Equal("v2.4.0"))
	g.Expect(request.SourceMetadata.CIProvider).To(Equal("generic-ci"))
	g.Expect(request.SourceMetadata.CIRunID).To(Equal("build-42"))
	g.Expect(request.SourceMetadata.CIRunURL).To(BeEmpty())

	request.Components[0].Type = types.ReleaseBundleComponentTypeOCIArtifact
	g.Expect(request.Validate()).To(MatchError(ContainSubstring(
		"component release artifact type must exactly match the release bundle component type",
	)))

	request.Components[0].Type = types.ReleaseBundleComponentTypeOCIImage
	request.ReleaseContract.ComponentV2.Artifacts = make([]types.ComponentReleaseArtifact, 257)
	g.Expect(request.Validate()).To(MatchError(ContainSubstring("artifacts contains too many entries")))
}

func TestCreateUpdateReleaseBundleRequestRejectsContradictoryComponentReleaseSourceProjection(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*CreateUpdateReleaseBundleRequest)
		field  string
	}{
		{
			name: "resolved commit mismatch",
			mutate: func(request *CreateUpdateReleaseBundleRequest) {
				request.SourceRevision = strings.Repeat("f", 40)
			},
			field: "sourceRevision",
		},
		{
			name: "repository mismatch",
			mutate: func(request *CreateUpdateReleaseBundleRequest) {
				request.SourceMetadata.Repository = "source/other"
			},
			field: "sourceMetadata.repository",
		},
		{
			name: "requested ref mismatch",
			mutate: func(request *CreateUpdateReleaseBundleRequest) {
				request.SourceMetadata.Tag = "v9.9.9"
			},
			field: "sourceMetadata.tag",
		},
		{
			name: "builder projection mismatch",
			mutate: func(request *CreateUpdateReleaseBundleRequest) {
				request.SourceMetadata.CIProvider = "other-ci"
			},
			field: "sourceMetadata.ciProvider",
		},
		{
			name: "build id projection mismatch",
			mutate: func(request *CreateUpdateReleaseBundleRequest) {
				request.SourceMetadata.CIRunID = "other-build"
			},
			field: "sourceMetadata.ciRunId",
		},
		{
			name: "build URL is irrelevant",
			mutate: func(request *CreateUpdateReleaseBundleRequest) {
				request.SourceMetadata.CIRunURL = "https://ci.example.invalid/runs/42"
			},
			field: "sourceMetadata.ciRunUrl",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			request := validComponentReleaseRequest()
			tt.mutate(&request)

			err := request.Validate()

			g.Expect(err).To(MatchError(ContainSubstring(tt.field)))
		})
	}
}

func TestCreateUpdateReleaseBundleRequestBoundsComponentReleaseOuterProjection(t *testing.T) {
	g := NewWithT(t)
	request := validComponentReleaseRequest()
	request.Components = make(
		[]ReleaseBundleComponentRequest,
		releasebundles.MaxComponentReleaseProjectionItems+1,
	)

	err := request.Validate()

	g.Expect(err).To(MatchError(ContainSubstring("components contains too many entries")))
}

func validComponentReleaseRequest() CreateUpdateReleaseBundleRequest {
	digest := "sha256:" + strings.Repeat("a", 64)
	contract := types.ComponentReleaseContractV2{
		Schema:       types.ReleaseContractSchemaV2,
		ComponentKey: "payments.api",
		Version:      "2.4.0",
		Source: types.ComponentReleaseSource{
			Repository:   "source/payments-api",
			RequestedRef: "refs/tags/v2.4.0",
			Commit:       "0123456789abcdef0123456789abcdef01234567",
		},
		Build: types.ComponentReleaseBuild{ID: "build-42", Builder: "generic-ci"},
		Artifacts: []types.ComponentReleaseArtifact{{
			Key:       "image",
			Type:      "oci-image",
			MediaType: "application/vnd.oci.image.index.v1+json",
			Digest:    digest,
			Platforms: []types.ComponentReleasePlatform{{
				Platform: "linux/amd64",
				Digest:   "sha256:" + strings.Repeat("b", 64),
			}},
		}},
		Changes: types.ComponentReleaseChanges{Summary: "Release payments API"},
	}
	return CreateUpdateReleaseBundleRequest{
		ApplicationID:  uuid.New(),
		ChannelID:      uuid.New(),
		ReleaseNumber:  "2.4.0",
		SourceRevision: contract.Source.Commit,
		SourceMetadata: &ReleaseBundleSourceMetadata{
			Repository: contract.Source.Repository,
			Tag:        "v2.4.0",
		},
		ReleaseContract: &types.ReleaseContract{
			Schema:      types.ReleaseContractSchemaV2,
			ComponentV2: &contract,
		},
		Components: []ReleaseBundleComponentRequest{{
			Key:        "image",
			Name:       "Payments API",
			Type:       types.ReleaseBundleComponentTypeOCIImage,
			Version:    contract.Version,
			PackageRef: "registry.example/payments-api",
			Digest:     digest,
		}},
	}
}

func TestCreateUpdateReleaseBundleRequestValidateRejectsInvalidPayloads(t *testing.T) {
	applicationID := uuid.New()
	channelID := uuid.New()
	versionID := uuid.New()
	childBundleID := uuid.New()

	validComponent := ReleaseBundleComponentRequest{
		Key:                  "api",
		Type:                 types.ReleaseBundleComponentTypeApplicationVersion,
		Version:              "1.2.3",
		ApplicationVersionID: &versionID,
	}

	tests := []struct {
		name    string
		request CreateUpdateReleaseBundleRequest
	}{
		{
			name: "empty release number",
			request: CreateUpdateReleaseBundleRequest{
				ApplicationID: applicationID,
				ChannelID:     channelID,
				ReleaseNumber: " ",
				Components:    []ReleaseBundleComponentRequest{validComponent},
			},
		},
		{
			name: "missing application",
			request: CreateUpdateReleaseBundleRequest{
				ChannelID:     channelID,
				ReleaseNumber: "1.2.3",
				Components:    []ReleaseBundleComponentRequest{validComponent},
			},
		},
		{
			name: "missing channel",
			request: CreateUpdateReleaseBundleRequest{
				ApplicationID: applicationID,
				ReleaseNumber: "1.2.3",
				Components:    []ReleaseBundleComponentRequest{validComponent},
			},
		},
		{
			name: "no components",
			request: CreateUpdateReleaseBundleRequest{
				ApplicationID:  applicationID,
				ChannelID:      channelID,
				ReleaseNumber:  "1.2.3",
				SourceRevision: "abc123",
			},
		},
		{
			name: "empty deployment process revision id",
			request: CreateUpdateReleaseBundleRequest{
				ApplicationID:               applicationID,
				ChannelID:                   channelID,
				DeploymentProcessRevisionID: &uuid.Nil,
				ReleaseNumber:               "1.2.3",
				Components:                  []ReleaseBundleComponentRequest{validComponent},
			},
		},
		{
			name: "duplicate trimmed component keys",
			request: CreateUpdateReleaseBundleRequest{
				ApplicationID: applicationID,
				ChannelID:     channelID,
				ReleaseNumber: "1.2.3",
				Components: []ReleaseBundleComponentRequest{
					validComponent,
					{
						Key:                  " api ",
						Type:                 types.ReleaseBundleComponentTypeApplicationVersion,
						Version:              "1.2.4",
						ApplicationVersionID: &versionID,
					},
				},
			},
		},
		{
			name: "invalid component type",
			request: CreateUpdateReleaseBundleRequest{
				ApplicationID: applicationID,
				ChannelID:     channelID,
				ReleaseNumber: "1.2.3",
				Components: []ReleaseBundleComponentRequest{
					{
						Key:     "api",
						Type:    types.ReleaseBundleComponentType("unsupported"),
						Version: "1.2.3",
					},
				},
			},
		},
		{
			name: "application version component requires applicationVersionId",
			request: CreateUpdateReleaseBundleRequest{
				ApplicationID: applicationID,
				ChannelID:     channelID,
				ReleaseNumber: "1.2.3",
				Components: []ReleaseBundleComponentRequest{
					{
						Key:     "api",
						Type:    types.ReleaseBundleComponentTypeApplicationVersion,
						Version: "1.2.3",
					},
				},
			},
		},
		{
			name: "oci image component requires digest",
			request: CreateUpdateReleaseBundleRequest{
				ApplicationID: applicationID,
				ChannelID:     channelID,
				ReleaseNumber: "1.2.3",
				Components: []ReleaseBundleComponentRequest{
					{
						Key:        "api-image",
						Type:       types.ReleaseBundleComponentTypeOCIImage,
						Version:    "1.2.3",
						PackageRef: "registry.example/api",
					},
				},
			},
		},
		{
			name: "child bundle component requires childReleaseBundleId",
			request: CreateUpdateReleaseBundleRequest{
				ApplicationID: applicationID,
				ChannelID:     channelID,
				ReleaseNumber: "1.2.3",
				Components: []ReleaseBundleComponentRequest{
					{
						Key:     "platform",
						Type:    types.ReleaseBundleComponentTypeChildReleaseBundle,
						Version: "2026.06.20",
					},
				},
			},
		},
		{
			name: "external artifact requires checksum",
			request: CreateUpdateReleaseBundleRequest{
				ApplicationID: applicationID,
				ChannelID:     channelID,
				ReleaseNumber: "1.2.3",
				Components: []ReleaseBundleComponentRequest{
					{
						Key:        "manual",
						Type:       types.ReleaseBundleComponentTypeExternalArtifact,
						Version:    "1.2.3",
						PackageRef: "https://example.com/manual.zip",
					},
				},
			},
		},
		{
			name: "child bundle cannot also specify application version",
			request: CreateUpdateReleaseBundleRequest{
				ApplicationID: applicationID,
				ChannelID:     channelID,
				ReleaseNumber: "1.2.3",
				Components: []ReleaseBundleComponentRequest{
					{
						Key:                  "platform",
						Type:                 types.ReleaseBundleComponentTypeChildReleaseBundle,
						Version:              "2026.06.20",
						ChildReleaseBundleID: &childBundleID,
						ApplicationVersionID: &versionID,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			err := tt.request.Validate()

			g.Expect(err).To(HaveOccurred())
		})
	}
}
