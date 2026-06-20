package mapping

import (
	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
)

func ReleaseBundleToAPI(bundle types.ReleaseBundle) api.ReleaseBundle {
	return api.ReleaseBundle{
		ID:                       bundle.ID,
		CreatedAt:                bundle.CreatedAt,
		UpdatedAt:                bundle.UpdatedAt,
		ApplicationID:            bundle.ApplicationID,
		ChannelID:                bundle.ChannelID,
		ReleaseNumber:            bundle.ReleaseNumber,
		ReleaseNotes:             bundle.ReleaseNotes,
		SourceRevision:           bundle.SourceRevision,
		SourceMetadata:           ReleaseBundleSourceMetadataToAPI(bundle),
		Status:                   bundle.Status,
		PublishedByUserAccountID: bundle.PublishedByUserAccountID,
		PublishedAt:              bundle.PublishedAt,
		CanonicalChecksum:        bundle.CanonicalChecksum,
		Components:               List(bundle.Components, ReleaseBundleComponentToAPI),
	}
}

func ReleaseBundleSourceMetadataToAPI(bundle types.ReleaseBundle) *api.ReleaseBundleSourceMetadata {
	if bundle.SourceRepository == "" &&
		bundle.SourceBranch == "" &&
		bundle.SourceTag == "" &&
		bundle.CIProvider == "" &&
		bundle.CIRunID == "" &&
		bundle.CIRunURL == "" {
		return nil
	}
	return &api.ReleaseBundleSourceMetadata{
		Repository: bundle.SourceRepository,
		Branch:     bundle.SourceBranch,
		Tag:        bundle.SourceTag,
		CIProvider: bundle.CIProvider,
		CIRunID:    bundle.CIRunID,
		CIRunURL:   bundle.CIRunURL,
	}
}

func ReleaseBundleComponentToAPI(component types.ReleaseBundleComponent) api.ReleaseBundleComponent {
	return api.ReleaseBundleComponent{
		ID:                   component.ID,
		ReleaseBundleID:      component.ReleaseBundleID,
		Key:                  component.Key,
		Name:                 component.Name,
		Type:                 component.Type,
		Version:              component.Version,
		ApplicationVersionID: component.ApplicationVersionID,
		PackageRef:           component.PackageRef,
		Digest:               component.Digest,
		Checksum:             component.Checksum,
		ChildReleaseBundleID: component.ChildReleaseBundleID,
	}
}
