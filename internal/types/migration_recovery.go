package types

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

const TimestampDirtyRecoveryFormatVersion = "distr.timestamp-dirty-recovery/v1"

type TimestampDirtyRecoveryRecordType string

const (
	TimestampDirtyRecoveryRecordTypePlan   TimestampDirtyRecoveryRecordType = "PLAN"
	TimestampDirtyRecoveryRecordTypeResult TimestampDirtyRecoveryRecordType = "RESULT"
)

type TimestampRecoveryCatalogShape string

const (
	TimestampRecoveryCatalogShapePredecessor137 TimestampRecoveryCatalogShape = "PREDECESSOR_137"
	TimestampRecoveryCatalogShapeExpand138      TimestampRecoveryCatalogShape = "EXPAND_138"
	TimestampRecoveryCatalogShapeUnknown        TimestampRecoveryCatalogShape = "UNKNOWN"
)

type TimestampRecoveryCatalogCategory string

const (
	TimestampRecoveryCatalogCategoryRelation   TimestampRecoveryCatalogCategory = "RELATION"
	TimestampRecoveryCatalogCategoryColumn     TimestampRecoveryCatalogCategory = "COLUMN"
	TimestampRecoveryCatalogCategoryConstraint TimestampRecoveryCatalogCategory = "CONSTRAINT"
	TimestampRecoveryCatalogCategoryIndex      TimestampRecoveryCatalogCategory = "INDEX"
	TimestampRecoveryCatalogCategoryTrigger    TimestampRecoveryCatalogCategory = "TRIGGER"
	TimestampRecoveryCatalogCategoryFunction   TimestampRecoveryCatalogCategory = "FUNCTION"
)

type TimestampRecoveryCatalogRecord struct {
	Category     TimestampRecoveryCatalogCategory
	RelationName string
	ObjectName   string
	Definition   string
}

type TimestampDirtyRecoveryManifestBinding struct {
	ID                       uuid.UUID `json:"id"`
	DocumentChecksum         string    `json:"documentChecksum"`
	DecisionContentChecksum  string    `json:"decisionContentChecksum"`
	RawSetChecksum           string    `json:"rawSetChecksum"`
	DatabaseIdentityChecksum string    `json:"databaseIdentityChecksum"`
	ExecutionCount           uint64    `json:"executionCount"`
	EventCount               uint64    `json:"eventCount"`
	RawCellCount             uint64    `json:"rawCellCount"`
}

type TimestampDirtyRecoveryPlan struct {
	FormatVersion         string                                 `json:"formatVersion"`
	RecordType            TimestampDirtyRecoveryRecordType       `json:"recordType"`
	RecoveryID            uuid.UUID                              `json:"recoveryId"`
	CreatedAt             time.Time                              `json:"createdAt"`
	OperatorIdentity      string                                 `json:"operatorIdentity"`
	Reason                string                                 `json:"reason"`
	WriterFenceIdentifier string                                 `json:"writerFenceIdentifier"`
	ExpectedDirtyVersion  uint                                   `json:"expectedDirtyVersion"`
	CatalogShape          TimestampRecoveryCatalogShape          `json:"catalogShape"`
	ForceVersion          uint                                   `json:"forceVersion"`
	CatalogChecksum       string                                 `json:"catalogChecksum"`
	Manifest              *TimestampDirtyRecoveryManifestBinding `json:"manifest,omitempty"`
}

type TimestampDirtyRecoverySchemaStatus struct {
	Version uint `json:"version"`
	Dirty   bool `json:"dirty"`
}

type TimestampDirtyRecoveryAction string

const (
	TimestampDirtyRecoveryActionForced               TimestampDirtyRecoveryAction = "FORCED"
	TimestampDirtyRecoveryActionObservedAlreadyClean TimestampDirtyRecoveryAction = "OBSERVED_ALREADY_CLEAN"
)

type TimestampDirtyRecoveryResultValue string

const TimestampDirtyRecoveryResultSucceeded TimestampDirtyRecoveryResultValue = "SUCCEEDED"

type TimestampDirtyRecoveryResult struct {
	FormatVersion          string                             `json:"formatVersion"`
	RecordType             TimestampDirtyRecoveryRecordType   `json:"recordType"`
	RecoveryID             uuid.UUID                          `json:"recoveryId"`
	PlanChecksum           string                             `json:"planChecksum"`
	CompletedAt            time.Time                          `json:"completedAt"`
	PlannedStatus          TimestampDirtyRecoverySchemaStatus `json:"plannedStatus"`
	ObservedPreApplyStatus TimestampDirtyRecoverySchemaStatus `json:"observedPreApplyStatus"`
	Action                 TimestampDirtyRecoveryAction       `json:"action"`
	ForcedVersion          uint                               `json:"forcedVersion"`
	CatalogChecksum        string                             `json:"catalogChecksum"`
	Result                 TimestampDirtyRecoveryResultValue  `json:"result"`
	PostStatus             TimestampDirtyRecoverySchemaStatus `json:"postStatus"`
}

var (
	timestampRecoveryChecksumPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	timestampRecoveryIdentityPattern = regexp.MustCompile(
		`^[A-Za-z0-9][A-Za-z0-9._@:+-]{0,127}$`,
	)
	timestampRecoveryFencePattern = regexp.MustCompile(
		`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`,
	)
	timestampRecoverySensitiveReasonPattern = regexp.MustCompile(
		`(?i)(?:postgres(?:ql)?://|[a-z][a-z0-9+.-]*://|(?:password|passwd|secret|token|dsn)\s*[:=])`,
	)
	timestampRecoveryPathReasonPattern = regexp.MustCompile(
		`(?i)(?:^|[\s("'=])(?:[a-z]:[\\/]|\\\\|~/|/[^/\s]+/[^/\s]+)`,
	)
)

func (plan TimestampDirtyRecoveryPlan) Validate() error {
	var problems []error
	add := func(format string, values ...any) {
		problems = append(problems, fmt.Errorf(format, values...))
	}

	if plan.FormatVersion != TimestampDirtyRecoveryFormatVersion {
		add("format version must be %s", TimestampDirtyRecoveryFormatVersion)
	}
	if plan.RecordType != TimestampDirtyRecoveryRecordTypePlan {
		add("record type must be PLAN")
	}
	if plan.RecoveryID == uuid.Nil {
		add("recovery id cannot be nil UUID")
	}
	if err := validateTimestampRecoveryTime("created at", plan.CreatedAt); err != nil {
		problems = append(problems, err)
	}
	if !timestampRecoveryIdentityPattern.MatchString(plan.OperatorIdentity) {
		add("operator identity must be a 1-128 character safe identifier")
	}
	if err := validateTimestampRecoveryReason(plan.Reason); err != nil {
		problems = append(problems, err)
	}
	if !timestampRecoveryFencePattern.MatchString(plan.WriterFenceIdentifier) {
		add("writer fence identifier must be a 1-128 character safe identifier")
	}
	if plan.ExpectedDirtyVersion != 137 && plan.ExpectedDirtyVersion != 138 {
		add("expected dirty version must be 137 or 138")
	}
	expectedForceVersion := uint(0)
	switch plan.CatalogShape {
	case TimestampRecoveryCatalogShapePredecessor137:
		expectedForceVersion = 137
	case TimestampRecoveryCatalogShapeExpand138:
		expectedForceVersion = 138
	default:
		add("catalog shape must be PREDECESSOR_137 or EXPAND_138")
	}
	if plan.ForceVersion != 137 && plan.ForceVersion != 138 {
		add("force version must be 137 or 138")
	} else if expectedForceVersion != 0 && plan.ForceVersion != expectedForceVersion {
		add(
			"force version %d does not match catalog shape %s",
			plan.ForceVersion,
			plan.CatalogShape,
		)
	}
	if !timestampRecoveryChecksumPattern.MatchString(plan.CatalogChecksum) {
		add("catalog checksum must use lowercase sha256 format")
	}
	if plan.Manifest != nil {
		if manifestProblems := plan.Manifest.validationErrors(); len(manifestProblems) > 0 {
			problems = append(problems, manifestProblems...)
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("invalid timestamp dirty recovery plan: %w", errors.Join(problems...))
	}
	return nil
}

func (binding TimestampDirtyRecoveryManifestBinding) validationErrors() []error {
	var problems []error
	if binding.ID == uuid.Nil {
		problems = append(problems, errors.New("manifest id cannot be nil UUID"))
	}
	for _, checksum := range []struct {
		name  string
		value string
	}{
		{name: "document checksum", value: binding.DocumentChecksum},
		{name: "decision content checksum", value: binding.DecisionContentChecksum},
		{name: "raw set checksum", value: binding.RawSetChecksum},
		{name: "database identity checksum", value: binding.DatabaseIdentityChecksum},
	} {
		if !timestampRecoveryChecksumPattern.MatchString(checksum.value) {
			problems = append(
				problems,
				fmt.Errorf("%s must use lowercase sha256 format", checksum.name),
			)
		}
	}
	if binding.ExecutionCount == 0 && binding.EventCount == 0 {
		problems = append(
			problems,
			errors.New("manifest binding requires non-empty history"),
		)
	}
	if binding.ExecutionCount > (math.MaxUint64-binding.EventCount)/5 {
		problems = append(
			problems,
			errors.New("manifest raw cell count calculation overflows"),
		)
	} else if expected := 5*binding.ExecutionCount + binding.EventCount; binding.RawCellCount != expected {
		problems = append(
			problems,
			fmt.Errorf(
				"manifest raw cell count %d must equal %d",
				binding.RawCellCount,
				expected,
			),
		)
	}
	return problems
}

func (result TimestampDirtyRecoveryResult) Validate() error {
	var problems []error
	add := func(format string, values ...any) {
		problems = append(problems, fmt.Errorf(format, values...))
	}

	if result.FormatVersion != TimestampDirtyRecoveryFormatVersion {
		add("format version must be %s", TimestampDirtyRecoveryFormatVersion)
	}
	if result.RecordType != TimestampDirtyRecoveryRecordTypeResult {
		add("record type must be RESULT")
	}
	if result.RecoveryID == uuid.Nil {
		add("recovery id cannot be nil UUID")
	}
	if !timestampRecoveryChecksumPattern.MatchString(result.PlanChecksum) {
		add("plan checksum must use lowercase sha256 format")
	}
	if err := validateTimestampRecoveryTime("completed at", result.CompletedAt); err != nil {
		problems = append(problems, err)
	}
	if (result.PlannedStatus.Version != 137 && result.PlannedStatus.Version != 138) ||
		!result.PlannedStatus.Dirty {
		add("planned status must be dirty at version 137 or 138")
	}
	if result.ForcedVersion != 137 && result.ForcedVersion != 138 {
		add("forced version must be 137 or 138")
	}
	switch result.Action {
	case TimestampDirtyRecoveryActionForced:
		if result.ObservedPreApplyStatus != result.PlannedStatus {
			add("FORCED action requires observed pre-apply status to equal planned dirty status")
		}
	case TimestampDirtyRecoveryActionObservedAlreadyClean:
		if result.ObservedPreApplyStatus.Dirty ||
			result.ObservedPreApplyStatus.Version != result.ForcedVersion {
			add("OBSERVED_ALREADY_CLEAN action requires a clean observed forced version")
		}
	default:
		add("action must be FORCED or OBSERVED_ALREADY_CLEAN")
	}
	if result.PostStatus.Dirty {
		add("post status must be clean")
	}
	if result.PostStatus.Version != result.ForcedVersion {
		add("post status version must equal forced version")
	}
	if !timestampRecoveryChecksumPattern.MatchString(result.CatalogChecksum) {
		add("catalog checksum must use lowercase sha256 format")
	}
	if result.Result != TimestampDirtyRecoveryResultSucceeded {
		add("result must be SUCCEEDED")
	}

	if len(problems) > 0 {
		return fmt.Errorf("invalid timestamp dirty recovery result: %w", errors.Join(problems...))
	}
	return nil
}

func validateTimestampRecoveryTime(name string, value time.Time) error {
	if value.IsZero() {
		return fmt.Errorf("%s timestamp is required", name)
	}
	_, offset := value.Zone()
	if offset != 0 {
		return fmt.Errorf("%s timestamp must use UTC", name)
	}
	return nil
}

func validateTimestampRecoveryReason(reason string) error {
	count := utf8.RuneCountInString(reason)
	if count < 1 || count > 256 || strings.TrimSpace(reason) != reason {
		return errors.New("reason must be 1-256 trimmed printable characters")
	}
	if !utf8.ValidString(reason) {
		return errors.New("reason must be valid UTF-8")
	}
	for _, value := range reason {
		if !unicode.IsPrint(value) || unicode.IsControl(value) {
			return errors.New("reason must contain only printable single-line characters")
		}
	}
	if timestampRecoverySensitiveReasonPattern.MatchString(reason) ||
		timestampRecoveryPathReasonPattern.MatchString(reason) {
		return errors.New("reason must not contain connection, credential, or path material")
	}
	return nil
}
