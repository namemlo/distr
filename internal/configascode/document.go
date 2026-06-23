package configascode

type ValidationResult struct {
	Valid     bool
	Documents []DocumentResult
	Errors    []Issue
	Warnings  []Issue
}

type DocumentResult struct {
	Kind              string
	APIVersion        string
	MetadataName      string
	MetadataPath      string
	CanonicalChecksum string
}

type Issue struct {
	DocumentIndex int
	Path          string
	Message       string
}
