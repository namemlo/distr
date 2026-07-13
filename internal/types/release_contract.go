package types

const ReleaseContractSchemaV1 = "distr.release-contract/v1"

type ReleaseContract struct {
	Schema        string                       `json:"schema"`
	Source        ReleaseContractSource        `json:"source"`
	Build         ReleaseContractBuild         `json:"build"`
	Components    []ReleaseContractComponent   `json:"components"`
	Compatibility ReleaseContractCompatibility `json:"compatibility"`
	Operations    ReleaseContractOperations    `json:"operations"`
	Config        ReleaseContractConfig        `json:"config"`
	Changes       ReleaseContractChanges       `json:"changes"`
}

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
	Name     string `json:"name"`
	Image    string `json:"image"`
	Platform string `json:"platform"`
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
	VersionID string `json:"versionId"`
	Checksum  string `json:"checksum"`
}

type ReleaseContractChanges struct {
	Summary string   `json:"summary"`
	Commits []string `json:"commits"`
}
