package api

import (
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/validation"
	"github.com/google/uuid"
)

type CreateUpdateReleaseBundleRequest struct {
	ApplicationID  uuid.UUID                       `json:"applicationId"`
	ChannelID      uuid.UUID                       `json:"channelId"`
	ReleaseNumber  string                          `json:"releaseNumber"`
	ReleaseNotes   string                          `json:"releaseNotes"`
	SourceRevision string                          `json:"sourceRevision"`
	Components     []ReleaseBundleComponentRequest `json:"components"`
}

func (r *CreateUpdateReleaseBundleRequest) Validate() error {
	r.ReleaseNumber = strings.TrimSpace(r.ReleaseNumber)
	r.SourceRevision = strings.TrimSpace(r.SourceRevision)
	if r.ReleaseNumber == "" {
		return validation.NewValidationFailedError("releaseNumber is required")
	}
	if r.ApplicationID == uuid.Nil {
		return validation.NewValidationFailedError("applicationId is required")
	}
	if r.ChannelID == uuid.Nil {
		return validation.NewValidationFailedError("channelId is required")
	}
	if len(r.Components) == 0 {
		return validation.NewValidationFailedError("at least one component is required")
	}

	seenKeys := map[string]struct{}{}
	for i := range r.Components {
		component := &r.Components[i]
		component.trim()
		if component.Key == "" {
			return validation.NewValidationFailedError("component key is required")
		}
		if _, ok := seenKeys[component.Key]; ok {
			return validation.NewValidationFailedError("component keys must be unique")
		}
		seenKeys[component.Key] = struct{}{}
		if err := component.validate(); err != nil {
			return err
		}
	}
	return nil
}

type ReleaseBundleComponentRequest struct {
	Key                  string                           `json:"key"`
	Name                 string                           `json:"name"`
	Type                 types.ReleaseBundleComponentType `json:"type"`
	Version              string                           `json:"version"`
	ApplicationVersionID *uuid.UUID                       `json:"applicationVersionId,omitempty"`
	PackageRef           string                           `json:"packageRef"`
	Digest               string                           `json:"digest"`
	Checksum             string                           `json:"checksum"`
	ChildReleaseBundleID *uuid.UUID                       `json:"childReleaseBundleId,omitempty"`
}

func (r *ReleaseBundleComponentRequest) trim() {
	r.Key = strings.TrimSpace(r.Key)
	r.Name = strings.TrimSpace(r.Name)
	r.Version = strings.TrimSpace(r.Version)
	r.PackageRef = strings.TrimSpace(r.PackageRef)
	r.Digest = strings.TrimSpace(r.Digest)
	r.Checksum = strings.TrimSpace(r.Checksum)
}

func (r ReleaseBundleComponentRequest) validate() error {
	if !r.Type.IsValid() {
		return validation.NewValidationFailedError("component type is invalid")
	}
	if r.Version == "" {
		return validation.NewValidationFailedError("component version is required")
	}
	switch r.Type {
	case types.ReleaseBundleComponentTypeApplicationVersion:
		if r.ApplicationVersionID == nil || *r.ApplicationVersionID == uuid.Nil {
			return validation.NewValidationFailedError("component applicationVersionId is required")
		}
		if r.ChildReleaseBundleID != nil {
			return validation.NewValidationFailedError("application version component cannot reference a child release bundle")
		}
	case types.ReleaseBundleComponentTypeOCIImage, types.ReleaseBundleComponentTypeOCIArtifact:
		if r.PackageRef == "" {
			return validation.NewValidationFailedError("component packageRef is required")
		}
		if !strings.HasPrefix(r.Digest, "sha256:") {
			return validation.NewValidationFailedError("component digest must be a sha256 digest")
		}
		if r.ApplicationVersionID != nil || r.ChildReleaseBundleID != nil {
			return validation.NewValidationFailedError("oci component cannot reference application versions or child bundles")
		}
	case types.ReleaseBundleComponentTypeHelmChart:
		if r.PackageRef == "" {
			return validation.NewValidationFailedError("component packageRef is required")
		}
		if r.ApplicationVersionID != nil || r.ChildReleaseBundleID != nil {
			return validation.NewValidationFailedError(
				"helm chart component cannot reference application versions or child bundles",
			)
		}
	case types.ReleaseBundleComponentTypeChildReleaseBundle:
		if r.ChildReleaseBundleID == nil || *r.ChildReleaseBundleID == uuid.Nil {
			return validation.NewValidationFailedError("component childReleaseBundleId is required")
		}
		if r.ApplicationVersionID != nil {
			return validation.NewValidationFailedError("child release bundle component cannot reference an application version")
		}
	case types.ReleaseBundleComponentTypeExternalArtifact:
		if r.PackageRef == "" {
			return validation.NewValidationFailedError("component packageRef is required")
		}
		if r.Checksum == "" {
			return validation.NewValidationFailedError("component checksum is required")
		}
		if r.ApplicationVersionID != nil || r.ChildReleaseBundleID != nil {
			return validation.NewValidationFailedError(
				"external artifact component cannot reference application versions or child bundles",
			)
		}
	}
	return nil
}

type ReleaseBundle struct {
	ID                uuid.UUID                 `json:"id"`
	CreatedAt         time.Time                 `json:"createdAt"`
	UpdatedAt         time.Time                 `json:"updatedAt"`
	ApplicationID     uuid.UUID                 `json:"applicationId"`
	ChannelID         uuid.UUID                 `json:"channelId"`
	ReleaseNumber     string                    `json:"releaseNumber"`
	ReleaseNotes      string                    `json:"releaseNotes"`
	SourceRevision    string                    `json:"sourceRevision"`
	Status            types.ReleaseBundleStatus `json:"status"`
	CanonicalChecksum string                    `json:"canonicalChecksum"`
	Components        []ReleaseBundleComponent  `json:"components"`
}

type ReleaseBundleComponent struct {
	ID                   uuid.UUID                        `json:"id"`
	ReleaseBundleID      uuid.UUID                        `json:"releaseBundleId"`
	Key                  string                           `json:"key"`
	Name                 string                           `json:"name"`
	Type                 types.ReleaseBundleComponentType `json:"type"`
	Version              string                           `json:"version"`
	ApplicationVersionID *uuid.UUID                       `json:"applicationVersionId,omitempty"`
	PackageRef           string                           `json:"packageRef"`
	Digest               string                           `json:"digest"`
	Checksum             string                           `json:"checksum"`
	ChildReleaseBundleID *uuid.UUID                       `json:"childReleaseBundleId,omitempty"`
}
