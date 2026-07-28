package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var (
	sampleRetirementChecksumPattern   = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	sampleRetirementSourceKindPattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,127}$`)
)

type sampleRetirementCommandRuntime struct {
	Stdout io.Writer
	Stderr io.Writer
	Client *http.Client
	Getenv func(string) string
}

type sampleRetirementCommandOptions struct {
	Server string
	Token  string
}

type sampleRetirementItem struct {
	SubjectType       string    `json:"subjectType"`
	SubjectID         uuid.UUID `json:"subjectId"`
	OwnershipMarker   string    `json:"ownershipMarker"`
	OwnershipChecksum string    `json:"ownershipChecksum"`
	ExpectedChecksum  string    `json:"expectedChecksum"`
}

type sampleRetirementPreviewRequest struct {
	BackupReference       string                 `json:"backupReference"`
	BackupChecksum        string                 `json:"backupChecksum"`
	RestoreProofReference string                 `json:"restoreProofReference"`
	RestoreProofChecksum  string                 `json:"restoreProofChecksum"`
	Items                 []sampleRetirementItem `json:"items"`
}

type sampleRetirementPreviewOptions struct {
	Items            []string
	BackupReference  string
	BackupChecksum   string
	RestoreReference string
	RestoreChecksum  string
}

type sampleRetirementApplyOptions struct {
	PreviewChecksum  string
	ApprovalID       string
	ApprovalChecksum string
}

type sampleRetirementApplyRequest struct {
	PreviewChecksum  string `json:"previewChecksum"`
	ApprovalID       string `json:"approvalId"`
	ApprovalChecksum string `json:"approvalChecksum"`
}

type sampleRetirementOwnershipEvidenceOptions struct {
	SubjectType       string
	SubjectID         string
	OwnershipMarker   string
	OwnershipChecksum string
	SourceReference   string
	SourceChecksum    string
}

type sampleRetirementOwnershipEvidenceRequest struct {
	SubjectType       string    `json:"subjectType"`
	SubjectID         uuid.UUID `json:"subjectId"`
	OwnershipMarker   string    `json:"ownershipMarker"`
	OwnershipChecksum string    `json:"ownershipChecksum"`
	SourceReference   string    `json:"sourceReference"`
	SourceChecksum    string    `json:"sourceChecksum"`
}

type sampleRetirementRecoveryEvidenceOptions struct {
	EvidenceKind   string
	Reference      string
	Checksum       string
	SourceKind     string
	SourceID       string
	SourceChecksum string
	VerifiedAt     string
}

type sampleRetirementRecoveryEvidenceRequest struct {
	EvidenceKind   string    `json:"evidenceKind"`
	Reference      string    `json:"reference"`
	Checksum       string    `json:"checksum"`
	SourceKind     string    `json:"sourceKind"`
	SourceID       uuid.UUID `json:"sourceId"`
	SourceChecksum string    `json:"sourceChecksum"`
	VerifiedAt     time.Time `json:"verifiedAt"`
}

func NewRetireSampleDomainCommand() *cobra.Command {
	return newRetireSampleDomainCommand(sampleRetirementCommandRuntime{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Client: http.DefaultClient,
		Getenv: os.Getenv,
	})
}

func newRetireSampleDomainCommand(runtime sampleRetirementCommandRuntime) *cobra.Command {
	runtime = runtime.withDefaults()
	opts := sampleRetirementCommandOptions{}
	command := &cobra.Command{
		Use:   "retire-sample-domain",
		Short: "retire an exact allowlist of sample-domain records",
	}
	configureReleaseCommandErrors(command)
	command.PersistentFlags().StringVar(&opts.Server, "server", "", "Distr server URL")
	command.PersistentFlags().StringVar(&opts.Token, "token", "", "Distr API token")
	command.AddCommand(
		newSampleRetirementPreviewCommand(runtime, &opts),
		newSampleRetirementApplyCommand(runtime, &opts),
		newSampleRetirementVerifyCommand(runtime, &opts),
		newSampleRetirementOwnershipEvidenceCommand(runtime, &opts),
		newSampleRetirementRecoveryEvidenceCommand(runtime, &opts),
	)
	return command
}

func (runtime sampleRetirementCommandRuntime) withDefaults() sampleRetirementCommandRuntime {
	if runtime.Stdout == nil {
		runtime.Stdout = os.Stdout
	}
	if runtime.Stderr == nil {
		runtime.Stderr = os.Stderr
	}
	if runtime.Client == nil {
		runtime.Client = http.DefaultClient
	}
	if runtime.Getenv == nil {
		runtime.Getenv = os.Getenv
	}
	return runtime
}

func newSampleRetirementPreviewCommand(
	runtime sampleRetirementCommandRuntime,
	commandOptions *sampleRetirementCommandOptions,
) *cobra.Command {
	opts := sampleRetirementPreviewOptions{}
	command := &cobra.Command{
		Use:   "preview",
		Short: "preview retirement of exact sample-domain records",
		Args:  releaseNoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			config, err := resolveSampleRetirementCommandConfig(*commandOptions, runtime)
			if err != nil {
				return err
			}
			request, err := resolveSampleRetirementPreviewRequest(opts)
			if err != nil {
				return err
			}
			body, err := json.Marshal(request)
			if err != nil {
				return newReleaseExitError(releaseExitUsage, "failed to encode preview request")
			}
			response, err := doSampleRetirementAPIRequest(
				command,
				runtime,
				config,
				"/api/v1/sample-retirements/preview",
				body,
			)
			if err != nil {
				return err
			}
			return writeSampleRetirementJSON(runtime.Stdout, response, config.Token)
		},
	}
	configureReleaseCommandErrors(command)
	command.Flags().StringArrayVar(
		&opts.Items,
		"item",
		nil,
		"exact item as TYPE,UUID,OWNERSHIP_MARKER,OWNERSHIP_CHECKSUM,EXPECTED_CHECKSUM; repeat for each record",
	)
	command.Flags().StringVar(
		&opts.BackupReference,
		"backup-reference",
		"",
		"immutable backup evidence reference",
	)
	command.Flags().StringVar(
		&opts.BackupChecksum,
		"backup-checksum",
		"",
		"lowercase sha256 checksum of the backup evidence",
	)
	command.Flags().StringVar(
		&opts.RestoreReference,
		"restore-reference",
		"",
		"isolated restore-verification evidence reference",
	)
	command.Flags().StringVar(
		&opts.RestoreChecksum,
		"restore-checksum",
		"",
		"lowercase sha256 checksum of the restore-verification evidence",
	)
	return command
}

func resolveSampleRetirementCommandConfig(
	opts sampleRetirementCommandOptions,
	runtime sampleRetirementCommandRuntime,
) (releaseCommandConfig, error) {
	return resolveReleaseCommandConfig(
		releaseCommandOptions{
			Server: opts.Server,
			Token:  opts.Token,
			Output: "json",
		},
		releaseCommandRuntime{Getenv: runtime.Getenv},
	)
}

func newSampleRetirementApplyCommand(
	runtime sampleRetirementCommandRuntime,
	commandOptions *sampleRetirementCommandOptions,
) *cobra.Command {
	opts := sampleRetirementApplyOptions{}
	command := &cobra.Command{
		Use:   "apply JOB_ID",
		Short: "apply an approved sample-domain retirement preview",
		Args:  releaseExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			jobID, err := parseSampleRetirementJobID(args[0])
			if err != nil {
				return err
			}
			request, err := resolveSampleRetirementApplyRequest(opts)
			if err != nil {
				return err
			}
			config, err := resolveSampleRetirementCommandConfig(*commandOptions, runtime)
			if err != nil {
				return err
			}
			body, err := json.Marshal(request)
			if err != nil {
				return newReleaseExitError(releaseExitUsage, "failed to encode apply request")
			}
			response, err := doSampleRetirementAPIRequest(
				command,
				runtime,
				config,
				"/api/v1/sample-retirements/"+jobID.String()+"/apply",
				body,
			)
			if err != nil {
				return err
			}
			return writeSampleRetirementJSON(runtime.Stdout, response, config.Token)
		},
	}
	configureReleaseCommandErrors(command)
	command.Flags().StringVar(
		&opts.PreviewChecksum,
		"preview-checksum",
		"",
		"immutable checksum returned by preview",
	)
	command.Flags().StringVar(
		&opts.ApprovalID,
		"approval-id",
		"",
		"approved cleanup decision identifier",
	)
	command.Flags().StringVar(
		&opts.ApprovalChecksum,
		"approval-checksum",
		"",
		"lowercase sha256 checksum of the approved cleanup decision",
	)
	return command
}

func resolveSampleRetirementApplyRequest(
	opts sampleRetirementApplyOptions,
) (sampleRetirementApplyRequest, error) {
	request := sampleRetirementApplyRequest{
		PreviewChecksum:  strings.TrimSpace(opts.PreviewChecksum),
		ApprovalID:       strings.TrimSpace(opts.ApprovalID),
		ApprovalChecksum: strings.TrimSpace(opts.ApprovalChecksum),
	}
	if !sampleRetirementChecksumPattern.MatchString(request.PreviewChecksum) {
		return request, newReleaseExitError(
			releaseExitUsage,
			"--preview-checksum must be a lowercase sha256 checksum",
		)
	}
	if request.ApprovalID == "" {
		return request, newReleaseExitError(releaseExitUsage, "--approval-id is required")
	}
	if !sampleRetirementChecksumPattern.MatchString(request.ApprovalChecksum) {
		return request, newReleaseExitError(
			releaseExitUsage,
			"--approval-checksum must be a lowercase sha256 checksum",
		)
	}
	return request, nil
}

func newSampleRetirementVerifyCommand(
	runtime sampleRetirementCommandRuntime,
	commandOptions *sampleRetirementCommandOptions,
) *cobra.Command {
	command := &cobra.Command{
		Use:   "verify JOB_ID",
		Short: "verify an applied sample-domain retirement",
		Args:  releaseExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			jobID, err := parseSampleRetirementJobID(args[0])
			if err != nil {
				return err
			}
			config, err := resolveSampleRetirementCommandConfig(*commandOptions, runtime)
			if err != nil {
				return err
			}
			response, err := doSampleRetirementAPIRequest(
				command,
				runtime,
				config,
				"/api/v1/sample-retirements/"+jobID.String()+"/verify",
				[]byte(`{}`),
			)
			if err != nil {
				return err
			}
			return writeSampleRetirementJSON(runtime.Stdout, response, config.Token)
		},
	}
	configureReleaseCommandErrors(command)
	return command
}

func newSampleRetirementOwnershipEvidenceCommand(
	runtime sampleRetirementCommandRuntime,
	commandOptions *sampleRetirementCommandOptions,
) *cobra.Command {
	opts := sampleRetirementOwnershipEvidenceOptions{}
	command := &cobra.Command{
		Use:   "register-ownership-evidence",
		Short: "register append-only evidence for one exact sample-owned subject",
		Args:  releaseNoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			request, err := resolveSampleRetirementOwnershipEvidenceRequest(opts)
			if err != nil {
				return err
			}
			config, err := resolveSampleRetirementCommandConfig(*commandOptions, runtime)
			if err != nil {
				return err
			}
			body, err := json.Marshal(request)
			if err != nil {
				return newReleaseExitError(
					releaseExitUsage,
					"failed to encode ownership evidence request",
				)
			}
			response, err := doSampleRetirementAPIRequest(
				command,
				runtime,
				config,
				"/api/v1/sample-retirement-evidence/ownership",
				body,
			)
			if err != nil {
				return err
			}
			return writeSampleRetirementJSON(runtime.Stdout, response, config.Token)
		},
	}
	configureReleaseCommandErrors(command)
	command.Flags().StringVar(&opts.SubjectType, "subject-type", "", "exact subject type")
	command.Flags().StringVar(&opts.SubjectID, "subject-id", "", "exact subject UUID")
	command.Flags().StringVar(&opts.OwnershipMarker, "ownership-marker", "", "sample ownership marker")
	command.Flags().StringVar(
		&opts.OwnershipChecksum,
		"ownership-checksum",
		"",
		"lowercase sha256 checksum of the ownership marker",
	)
	command.Flags().StringVar(
		&opts.SourceReference,
		"source-reference",
		"",
		"immutable ownership inventory reference",
	)
	command.Flags().StringVar(
		&opts.SourceChecksum,
		"source-checksum",
		"",
		"lowercase sha256 checksum of the ownership inventory",
	)
	return command
}

func resolveSampleRetirementOwnershipEvidenceRequest(
	opts sampleRetirementOwnershipEvidenceOptions,
) (sampleRetirementOwnershipEvidenceRequest, error) {
	request := sampleRetirementOwnershipEvidenceRequest{
		SubjectType:       strings.TrimSpace(opts.SubjectType),
		OwnershipMarker:   strings.TrimSpace(opts.OwnershipMarker),
		OwnershipChecksum: strings.TrimSpace(opts.OwnershipChecksum),
		SourceReference:   strings.TrimSpace(opts.SourceReference),
		SourceChecksum:    strings.TrimSpace(opts.SourceChecksum),
	}
	if !validSampleRetirementSubjectType(request.SubjectType) {
		return request, newReleaseExitError(
			releaseExitUsage,
			"--subject-type must be application, deployment_target, or environment",
		)
	}
	subjectID, err := uuid.Parse(strings.TrimSpace(opts.SubjectID))
	if err != nil || subjectID == uuid.Nil {
		return request, newReleaseExitError(
			releaseExitUsage,
			"--subject-id must be a non-empty UUID",
		)
	}
	request.SubjectID = subjectID
	if !validSampleRetirementEvidenceText(request.OwnershipMarker, 256) {
		return request, newReleaseExitError(
			releaseExitUsage,
			"--ownership-marker must be non-empty and at most 256 characters",
		)
	}
	if !sampleRetirementChecksumPattern.MatchString(request.OwnershipChecksum) {
		return request, newReleaseExitError(
			releaseExitUsage,
			"--ownership-checksum must be a lowercase sha256 checksum",
		)
	}
	if !validSampleRetirementEvidenceReference(request.SourceReference) {
		return request, newReleaseExitError(
			releaseExitUsage,
			"--source-reference must be non-empty, newline-free, and at most 1024 characters",
		)
	}
	if !sampleRetirementChecksumPattern.MatchString(request.SourceChecksum) {
		return request, newReleaseExitError(
			releaseExitUsage,
			"--source-checksum must be a lowercase sha256 checksum",
		)
	}
	return request, nil
}

func newSampleRetirementRecoveryEvidenceCommand(
	runtime sampleRetirementCommandRuntime,
	commandOptions *sampleRetirementCommandOptions,
) *cobra.Command {
	opts := sampleRetirementRecoveryEvidenceOptions{}
	command := &cobra.Command{
		Use:   "register-recovery-evidence",
		Short: "register append-only backup or restore-proof evidence",
		Args:  releaseNoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			request, err := resolveSampleRetirementRecoveryEvidenceRequest(opts)
			if err != nil {
				return err
			}
			config, err := resolveSampleRetirementCommandConfig(*commandOptions, runtime)
			if err != nil {
				return err
			}
			body, err := json.Marshal(request)
			if err != nil {
				return newReleaseExitError(
					releaseExitUsage,
					"failed to encode recovery evidence request",
				)
			}
			response, err := doSampleRetirementAPIRequest(
				command,
				runtime,
				config,
				"/api/v1/sample-retirement-evidence/recovery",
				body,
			)
			if err != nil {
				return err
			}
			return writeSampleRetirementJSON(runtime.Stdout, response, config.Token)
		},
	}
	configureReleaseCommandErrors(command)
	command.Flags().StringVar(
		&opts.EvidenceKind,
		"evidence-kind",
		"",
		"evidence kind: backup or restore_proof",
	)
	command.Flags().StringVar(&opts.Reference, "reference", "", "immutable recovery evidence reference")
	command.Flags().StringVar(
		&opts.Checksum,
		"checksum",
		"",
		"lowercase sha256 checksum of the recovery evidence",
	)
	command.Flags().StringVar(&opts.SourceKind, "source-kind", "", "exact recovery source kind")
	command.Flags().StringVar(&opts.SourceID, "source-id", "", "exact recovery source UUID")
	command.Flags().StringVar(
		&opts.SourceChecksum,
		"source-checksum",
		"",
		"lowercase sha256 checksum binding the exact recovery source",
	)
	command.Flags().StringVar(
		&opts.VerifiedAt,
		"verified-at",
		"",
		"explicit RFC3339 verification timestamp",
	)
	return command
}

func resolveSampleRetirementRecoveryEvidenceRequest(
	opts sampleRetirementRecoveryEvidenceOptions,
) (sampleRetirementRecoveryEvidenceRequest, error) {
	request := sampleRetirementRecoveryEvidenceRequest{
		EvidenceKind:   strings.TrimSpace(opts.EvidenceKind),
		Reference:      strings.TrimSpace(opts.Reference),
		Checksum:       strings.TrimSpace(opts.Checksum),
		SourceKind:     strings.TrimSpace(opts.SourceKind),
		SourceChecksum: strings.TrimSpace(opts.SourceChecksum),
	}
	if request.EvidenceKind != "backup" && request.EvidenceKind != "restore_proof" {
		return request, newReleaseExitError(
			releaseExitUsage,
			"--evidence-kind must be backup or restore_proof",
		)
	}
	if !validSampleRetirementEvidenceReference(request.Reference) {
		return request, newReleaseExitError(
			releaseExitUsage,
			"--reference must be non-empty, newline-free, and at most 1024 characters",
		)
	}
	if !sampleRetirementChecksumPattern.MatchString(request.Checksum) {
		return request, newReleaseExitError(
			releaseExitUsage,
			"--checksum must be a lowercase sha256 checksum",
		)
	}
	if !sampleRetirementSourceKindPattern.MatchString(request.SourceKind) {
		return request, newReleaseExitError(
			releaseExitUsage,
			"--source-kind must be a lowercase identifier",
		)
	}
	sourceID, err := uuid.Parse(strings.TrimSpace(opts.SourceID))
	if err != nil || sourceID == uuid.Nil {
		return request, newReleaseExitError(
			releaseExitUsage,
			"--source-id must be a non-empty UUID",
		)
	}
	request.SourceID = sourceID
	if !sampleRetirementChecksumPattern.MatchString(request.SourceChecksum) {
		return request, newReleaseExitError(
			releaseExitUsage,
			"--source-checksum must be a lowercase sha256 checksum",
		)
	}
	verifiedAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(opts.VerifiedAt))
	if err != nil {
		return request, newReleaseExitError(
			releaseExitUsage,
			"--verified-at must be an RFC3339 timestamp",
		)
	}
	if verifiedAt.After(time.Now()) {
		return request, newReleaseExitError(
			releaseExitUsage,
			"--verified-at must not be in the future",
		)
	}
	request.VerifiedAt = verifiedAt
	return request, nil
}

func validSampleRetirementEvidenceText(value string, maximumLength int) bool {
	return value != "" &&
		len(value) <= maximumLength &&
		!strings.ContainsAny(value, "\r\n")
}

func validSampleRetirementEvidenceReference(value string) bool {
	if !validSampleRetirementEvidenceText(value, 1024) {
		return false
	}
	if strings.IndexFunc(value, func(character rune) bool {
		return character <= ' ' || character == '\u007f'
	}) >= 0 {
		return false
	}
	parsed, err := url.Parse(value)
	return err == nil &&
		parsed.Scheme != "" &&
		(parsed.Host != "" || parsed.Opaque != "" || parsed.Path != "")
}

func parseSampleRetirementJobID(value string) (uuid.UUID, error) {
	jobID, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil || jobID == uuid.Nil {
		return uuid.Nil, newReleaseExitError(
			releaseExitUsage,
			"job ID must be a non-empty UUID",
		)
	}
	return jobID, nil
}

func resolveSampleRetirementPreviewRequest(
	opts sampleRetirementPreviewOptions,
) (sampleRetirementPreviewRequest, error) {
	request := sampleRetirementPreviewRequest{
		BackupReference:       strings.TrimSpace(opts.BackupReference),
		BackupChecksum:        strings.TrimSpace(opts.BackupChecksum),
		RestoreProofReference: strings.TrimSpace(opts.RestoreReference),
		RestoreProofChecksum:  strings.TrimSpace(opts.RestoreChecksum),
	}
	if len(opts.Items) == 0 {
		return request, newReleaseExitError(
			releaseExitUsage,
			"at least one exact --item TYPE,UUID,OWNERSHIP_MARKER,OWNERSHIP_CHECKSUM,EXPECTED_CHECKSUM is required",
		)
	}
	for _, value := range opts.Items {
		item, err := parseSampleRetirementItem(value)
		if err != nil {
			return request, err
		}
		request.Items = append(request.Items, item)
	}
	if !validSampleRetirementEvidenceReference(request.BackupReference) {
		return request, newReleaseExitError(
			releaseExitUsage,
			"--backup-reference must be an immutable evidence reference",
		)
	}
	if !sampleRetirementChecksumPattern.MatchString(request.BackupChecksum) {
		return request, newReleaseExitError(
			releaseExitUsage,
			"--backup-checksum must be a lowercase sha256 checksum",
		)
	}
	if !validSampleRetirementEvidenceReference(request.RestoreProofReference) {
		return request, newReleaseExitError(
			releaseExitUsage,
			"--restore-reference must be an immutable evidence reference",
		)
	}
	if !sampleRetirementChecksumPattern.MatchString(request.RestoreProofChecksum) {
		return request, newReleaseExitError(
			releaseExitUsage,
			"--restore-checksum must be a lowercase sha256 checksum",
		)
	}
	return request, nil
}

func parseSampleRetirementItem(value string) (sampleRetirementItem, error) {
	parts := strings.Split(strings.TrimSpace(value), ",")
	if len(parts) != 5 {
		return sampleRetirementItem{}, newReleaseExitError(
			releaseExitUsage,
			"--item must use exact TYPE,UUID,OWNERSHIP_MARKER,OWNERSHIP_CHECKSUM,EXPECTED_CHECKSUM syntax",
		)
	}
	for index := range parts {
		parts[index] = strings.TrimSpace(parts[index])
	}
	if !validSampleRetirementSubjectType(parts[0]) {
		return sampleRetirementItem{}, newReleaseExitError(
			releaseExitUsage,
			"--item type must be application, deployment_target, or environment",
		)
	}
	subjectID, err := uuid.Parse(strings.TrimSpace(parts[1]))
	if err != nil || subjectID == uuid.Nil {
		return sampleRetirementItem{}, newReleaseExitError(
			releaseExitUsage,
			"--item ID must be a non-empty UUID",
		)
	}
	if parts[2] == "" {
		return sampleRetirementItem{}, newReleaseExitError(
			releaseExitUsage,
			"--item ownership marker is required",
		)
	}
	if !sampleRetirementChecksumPattern.MatchString(parts[3]) {
		return sampleRetirementItem{}, newReleaseExitError(
			releaseExitUsage,
			"--item ownership checksum must be a lowercase sha256 checksum",
		)
	}
	if !sampleRetirementChecksumPattern.MatchString(parts[4]) {
		return sampleRetirementItem{}, newReleaseExitError(
			releaseExitUsage,
			"--item expected checksum must be a lowercase sha256 checksum",
		)
	}
	return sampleRetirementItem{
		SubjectType:       parts[0],
		SubjectID:         subjectID,
		OwnershipMarker:   parts[2],
		OwnershipChecksum: parts[3],
		ExpectedChecksum:  parts[4],
	}, nil
}

func validSampleRetirementSubjectType(value string) bool {
	switch value {
	case "application", "deployment_target", "environment":
		return true
	default:
		return false
	}
}

func doSampleRetirementAPIRequest(
	command *cobra.Command,
	runtime sampleRetirementCommandRuntime,
	config releaseCommandConfig,
	path string,
	body []byte,
) ([]byte, error) {
	return doReleaseAPIRequest(
		command.Context(),
		releaseCommandRuntime{
			Stdout: runtime.Stdout,
			Stderr: runtime.Stderr,
			Client: runtime.Client,
			Getenv: runtime.Getenv,
		},
		config,
		http.MethodPost,
		path,
		body,
		"",
		false,
	)
}

func writeSampleRetirementJSON(writer io.Writer, response []byte, token string) error {
	response = bytes.TrimSpace(response)
	if !json.Valid(response) {
		return newReleaseExitError(releaseExitAPI, "API returned invalid JSON")
	}
	decoder := json.NewDecoder(bytes.NewReader(response))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return newReleaseExitError(releaseExitAPI, "API returned invalid JSON")
	}
	value = redactSampleRetirementJSONValue(value, token)
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

func redactSampleRetirementJSONValue(value any, token string) any {
	switch typed := value.(type) {
	case string:
		return redactSampleRetirementSecretString(typed, token)
	case []any:
		for index := range typed {
			typed[index] = redactSampleRetirementJSONValue(typed[index], token)
		}
	case map[string]any:
		for key := range typed {
			typed[key] = redactSampleRetirementJSONValue(typed[key], token)
		}
	}
	return value
}

func redactSampleRetirementSecretString(value string, token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return value
	}
	scheme, credential := releaseCredentialParts(token)
	redacted := value
	for _, candidate := range []string{
		token,
		"AccessToken " + credential,
		"Bearer " + credential,
		scheme + " " + credential,
	} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || candidate == credential {
			continue
		}
		redacted = replaceCaseInsensitive(redacted, candidate, "[REDACTED]")
	}
	if len(credential) >= 8 {
		return replaceCaseInsensitive(redacted, credential, "[REDACTED]")
	}
	return replaceDelimitedCredential(redacted, credential)
}

func replaceDelimitedCredential(value string, credential string) string {
	if credential == "" {
		return value
	}
	var builder strings.Builder
	lowerValue := strings.ToLower(value)
	lowerCredential := strings.ToLower(credential)
	searchFrom := 0
	lastWritten := 0
	for {
		relativeIndex := strings.Index(lowerValue[searchFrom:], lowerCredential)
		if relativeIndex < 0 {
			break
		}
		index := searchFrom + relativeIndex
		end := index + len(credential)
		leftDelimited := index == 0 || !isCredentialCharacter(value[index-1])
		rightDelimited := end == len(value) || !isCredentialCharacter(value[end])
		if leftDelimited && rightDelimited {
			builder.WriteString(value[lastWritten:index])
			builder.WriteString("[REDACTED]")
			lastWritten = end
			searchFrom = end
			continue
		}
		searchFrom = index + 1
	}
	if lastWritten == 0 {
		return value
	}
	builder.WriteString(value[lastWritten:])
	return builder.String()
}

func isCredentialCharacter(value byte) bool {
	return value >= 'a' && value <= 'z' ||
		value >= 'A' && value <= 'Z' ||
		value >= '0' && value <= '9'
}

func init() {
	RootCommand.AddCommand(NewRetireSampleDomainCommand())
}
