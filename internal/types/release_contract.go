package types

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

const (
	// ReleaseContractSchemaV1 is the historical document discriminator. It must
	// remain unchanged because v1 payloads and checksums are immutable evidence.
	ReleaseContractSchemaV1 = "distr.release-contract/v1"
	// ReleaseContractStorageSchemaV1 classifies historical rows without changing
	// their embedded v1 document.
	ReleaseContractStorageSchemaV1 = "distr.release/v1"
	ReleaseContractSchemaV2        = "distr.component-release/v2"
)

type ReleaseContract struct {
	Schema        string                       `json:"schema"`
	Source        ReleaseContractSource        `json:"source"`
	Build         ReleaseContractBuild         `json:"build"`
	Components    []ReleaseContractComponent   `json:"components"`
	Compatibility ReleaseContractCompatibility `json:"compatibility"`
	Operations    ReleaseContractOperations    `json:"operations"`
	Config        ReleaseContractConfig        `json:"config"`
	Changes       ReleaseContractChanges       `json:"changes"`
	ComponentV2   *ComponentReleaseContractV2  `json:"-"`
	ProductV1     *ProductReleaseManifest      `json:"-"`
}

type ReleaseContractV1 = ReleaseContract

type ReleaseContractSource struct {
	Repository   string `json:"repository"`
	Branch       string `json:"branch"`
	SourceCommit string `json:"sourceCommit"`
	BuiltCommit  string `json:"builtCommit"`
}

type ReleaseContractBuild struct {
	ExternalID  string `json:"externalId"`
	ExternalURL string `json:"externalUrl"`
}

type ReleaseContractComponent struct {
	Name      string   `json:"name"`
	Version   string   `json:"version"`
	Image     string   `json:"image"`
	Platform  string   `json:"platform"`
	Contracts []string `json:"contracts"`
}

type ReleaseContractCompatibility struct {
	Requires           []ReleaseContractRequirement `json:"requires"`
	AffectedComponents []string                     `json:"affectedComponents"`
}

type ReleaseContractRequirement struct {
	Component      string `json:"component"`
	Contract       string `json:"contract,omitempty"`
	MinimumVersion string `json:"minimumVersion,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

type ReleaseContractOperations struct {
	MigrationRequired    bool `json:"migrationRequired"`
	ConfigChangeRequired bool `json:"configChangeRequired"`
}

type ReleaseContractConfig struct {
	RepositoryCommit      string                        `json:"repositoryCommit"`
	ComposePath           string                        `json:"composePath"`
	ServiceConfigPath     string                        `json:"serviceConfigPath"`
	ComposeChecksum       string                        `json:"composeChecksum"`
	ServiceConfigChecksum string                        `json:"serviceConfigChecksum"`
	ImmutableObjects      []ReleaseContractConfigObject `json:"immutableObjects"`
}

type ReleaseContractConfigObject struct {
	URI       string `json:"uri"`
	VersionID string `json:"versionId,omitempty"`
	Checksum  string `json:"checksum"`
}

type ReleaseContractChanges struct {
	Summary string   `json:"summary"`
	Commits []string `json:"commits"`
}

type ComponentReleaseContractV2 struct {
	Schema       string                             `json:"schema"`
	ComponentKey string                             `json:"componentKey"`
	Version      string                             `json:"version"`
	Source       ComponentReleaseSource             `json:"source"`
	Build        ComponentReleaseBuild              `json:"build"`
	Artifacts    []ComponentReleaseArtifact         `json:"artifacts"`
	Provides     []CapabilityDeclaration            `json:"provides"`
	Requires     []CapabilityRequirement            `json:"requires"`
	Migrations   []MigrationDeclaration             `json:"migrations"`
	Changes      ComponentReleaseChanges            `json:"changes"`
	Evidence     ComponentReleaseEvidenceReferences `json:"evidence"`
}

type ComponentReleaseSource struct {
	Repository   string `json:"repository"`
	RequestedRef string `json:"requestedRef"`
	Commit       string `json:"commit"`
}

type ComponentReleaseBuild struct {
	ID      string `json:"id"`
	Builder string `json:"builder"`
}

type ComponentReleaseArtifact struct {
	Key       string                     `json:"key"`
	Type      string                     `json:"type"`
	MediaType string                     `json:"mediaType"`
	Digest    string                     `json:"digest"`
	Platforms []ComponentReleasePlatform `json:"platforms"`
}

type ComponentReleasePlatform struct {
	Platform string `json:"platform"`
	Digest   string `json:"digest"`
}

type CapabilityDeclaration struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type CapabilityRequirement struct {
	Name            string   `json:"name"`
	Range           string   `json:"range"`
	ResolutionStage string   `json:"resolutionStage"`
	AllowedModes    []string `json:"allowedModes"`
}

type MigrationDeclaration struct {
	Key           string `json:"key"`
	Type          string `json:"type"`
	Order         int    `json:"order"`
	Compatibility string `json:"compatibility"`
	FailurePolicy string `json:"failurePolicy"`
	Description   string `json:"description"`
}

type ComponentReleaseChanges struct {
	Summary string   `json:"summary"`
	Commits []string `json:"commits"`
}

type ComponentReleaseEvidenceReferences struct {
	Provenance []string `json:"provenance"`
	SBOM       []string `json:"sbom"`
	Signatures []string `json:"signatures"`
	Tests      []string `json:"tests"`
}

func (c ReleaseContract) MarshalJSON() ([]byte, error) {
	if c.ComponentV2 != nil {
		component := *c.ComponentV2
		component.Schema = ReleaseContractSchemaV2
		return json.Marshal(component)
	}
	if c.ProductV1 != nil {
		product := *c.ProductV1
		product.Schema = ProductReleaseSchemaV1
		return json.Marshal(product)
	}
	type v1 ReleaseContract
	return json.Marshal(v1(c))
}

func (c *ReleaseContract) UnmarshalJSON(data []byte) error {
	var discriminator struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(data, &discriminator); err != nil {
		return err
	}
	if discriminator.Schema == ReleaseContractSchemaV2 {
		var component ComponentReleaseContractV2
		if err := decodeReleaseContractStrict(data, &component); err != nil {
			return err
		}
		*c = ReleaseContract{Schema: ReleaseContractSchemaV2, ComponentV2: &component}
		return nil
	}
	if discriminator.Schema == ProductReleaseSchemaV1 {
		var product ProductReleaseManifest
		if err := decodeReleaseContractStrict(data, &product); err != nil {
			return err
		}
		*c = ReleaseContract{Schema: ProductReleaseSchemaV1, ProductV1: &product}
		return nil
	}
	type v1 ReleaseContract
	var legacy v1
	if err := json.Unmarshal(data, &legacy); err != nil {
		return err
	}
	*c = ReleaseContract(legacy)
	return nil
}

func decodeReleaseContractStrict(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("release contract must contain exactly one JSON value")
		}
		return err
	}
	return nil
}
