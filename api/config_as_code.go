package api

type ConfigAsCodeValidateRequest struct {
	Documents []ConfigAsCodeValidateDocumentRequest `json:"documents"`
}

type ConfigAsCodeValidateDocumentRequest struct {
	Content string `json:"content"`
}

type ConfigAsCodeValidateResponse struct {
	Valid     bool                         `json:"valid"`
	Documents []ConfigAsCodeDocumentResult `json:"documents"`
	Errors    []ConfigAsCodeIssue          `json:"errors"`
	Warnings  []ConfigAsCodeIssue          `json:"warnings"`
}

type ConfigAsCodeDocumentResult struct {
	Kind              string `json:"kind"`
	APIVersion        string `json:"apiVersion"`
	MetadataName      string `json:"metadataName,omitempty"`
	MetadataPath      string `json:"metadataPath,omitempty"`
	CanonicalChecksum string `json:"canonicalChecksum"`
}

type ConfigAsCodeIssue struct {
	DocumentIndex int    `json:"documentIndex"`
	Path          string `json:"path"`
	Message       string `json:"message"`
}

type ConfigAsCodeAuthority struct {
	ResourceKind     string  `json:"resourceKind"`
	ResourceID       string  `json:"resourceId"`
	Authority        string  `json:"authority"`
	RepositoryPath   string  `json:"repositoryPath"`
	SourceRevision   string  `json:"sourceRevision"`
	DocumentChecksum string  `json:"documentChecksum"`
	UpdatedByUserID  *string `json:"updatedByUserId,omitempty"`
	UpdatedAt        string  `json:"updatedAt"`
}

type ConfigAsCodeAuthorityListResponse struct {
	Authorities []ConfigAsCodeAuthority `json:"authorities"`
}

type ConfigAsCodeAuthorityUpdateRequest struct {
	Authority        string `json:"authority"`
	RepositoryPath   string `json:"repositoryPath"`
	SourceRevision   string `json:"sourceRevision"`
	DocumentChecksum string `json:"documentChecksum"`
}
