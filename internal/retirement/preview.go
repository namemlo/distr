package retirement

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

const (
	allowlistSchema = "sample-retirement-allowlist/v1"
	previewSchema   = "sample-retirement-preview/v1"
	referenceSchema = "sample-retirement-reference-report/v1"
)

var checksumPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type PreviewStore interface {
	InspectSampleRetirementSubjects(
		context.Context,
		uuid.UUID,
		[]types.SampleRetirementSubject,
	) ([]types.SampleRetirementCandidate, error)
	VerifyRetirementReverseReferences(
		context.Context,
		uuid.UUID,
		[]types.SampleRetirementSubject,
	) ([]types.ReferenceReport, error)
	SaveSampleRetirementPreview(
		context.Context,
		*types.SampleRetirementPreview,
	) (*types.SampleRetirementPreview, error)
}

func PreviewSampleRetirement(
	ctx context.Context,
	store PreviewStore,
	request types.SampleRetirementRequest,
) (*types.SampleRetirementPreview, error) {
	if store == nil {
		return nil, errors.New("sample retirement preview store is required")
	}
	if err := validateRequest(request); err != nil {
		return nil, err
	}

	requested := sortedSubjects(request.Items)
	candidates, err := store.InspectSampleRetirementSubjects(
		ctx,
		request.OrganizationID,
		append([]types.SampleRetirementSubject(nil), requested...),
	)
	if err != nil {
		return nil, fmt.Errorf("inspect sample retirement subjects: %w", err)
	}
	candidates, err = validateCandidates(request.OrganizationID, requested, candidates)
	if err != nil {
		return nil, err
	}

	reports, err := store.VerifyRetirementReverseReferences(
		ctx,
		request.OrganizationID,
		append([]types.SampleRetirementSubject(nil), requested...),
	)
	if err != nil {
		return nil, fmt.Errorf("verify retirement reverse references: %w", err)
	}
	reports, err = validateReferenceReports(
		request.OrganizationID,
		candidates,
		reports,
	)
	if err != nil {
		return nil, err
	}

	allowlistChecksum, err := canonicalAllowlistChecksum(
		request.OrganizationID,
		requested,
	)
	if err != nil {
		return nil, fmt.Errorf("checksum sample retirement allowlist: %w", err)
	}
	previewChecksum, err := canonicalPreviewChecksum(
		request,
		allowlistChecksum,
		candidates,
		reports,
	)
	if err != nil {
		return nil, fmt.Errorf("checksum sample retirement preview: %w", err)
	}

	now := time.Now().UTC()
	jobID := uuid.New()
	items := make([]types.SampleRetirementItem, 0, len(candidates))
	reportBySubject := make(map[string]types.ReferenceReport, len(reports))
	auditEventCount := 0
	for _, report := range reports {
		reportBySubject[subjectKey(report.Subject)] = report
		auditEventCount += report.AuditEventCount
	}
	for ordinal, candidate := range candidates {
		report := reportBySubject[subjectKey(candidate.Subject)]
		reportChecksum, reportErr := canonicalReferenceReportChecksum(report)
		if reportErr != nil {
			return nil, fmt.Errorf(
				"checksum reference report for %s: %w",
				subjectKey(candidate.Subject),
				reportErr,
			)
		}
		items = append(items, types.SampleRetirementItem{
			ID:                      uuid.New(),
			CreatedAt:               now,
			UpdatedAt:               now,
			OrganizationID:          request.OrganizationID,
			RetirementJobID:         jobID,
			Ordinal:                 ordinal + 1,
			SubjectType:             candidate.Subject.SubjectType,
			SubjectID:               candidate.Subject.SubjectID,
			OwnershipEvidenceID:     candidate.OwnershipEvidenceID,
			OwnershipMarker:         candidate.OwnershipMarker,
			OwnershipChecksum:       candidate.OwnershipChecksum,
			ExpectedChecksum:        candidate.CurrentChecksum,
			BeforeCount:             candidate.BeforeCount,
			ReferenceReportChecksum: reportChecksum,
			State:                   types.SampleRetirementItemPending,
			Version:                 1,
		})
	}

	preview := &types.SampleRetirementPreview{
		Job: types.SampleRetirementJob{
			ID:                       jobID,
			CreatedAt:                now,
			UpdatedAt:                now,
			OrganizationID:           request.OrganizationID,
			RequestedByUserAccountID: request.RequestedByUserAccountID,
			State:                    types.SampleRetirementJobPreviewed,
			BackupReference:          request.BackupReference,
			BackupChecksum:           request.BackupChecksum,
			RestoreProofReference:    request.RestoreProofReference,
			RestoreProofChecksum:     request.RestoreProofChecksum,
			AllowlistChecksum:        allowlistChecksum,
			PreviewChecksum:          previewChecksum,
			RequestedItemCount:       len(requested),
			PreviewedItemCount:       len(items),
			Version:                  1,
		},
		Items:            items,
		ReferenceReports: reports,
		PreviewChecksum:  previewChecksum,
		RequestedCount:   len(requested),
		RetirableCount:   len(items),
		BlockedCount:     0,
		AuditEventCount:  auditEventCount,
		CreatedAt:        now,
	}

	persisted, err := store.SaveSampleRetirementPreview(ctx, clonePreview(preview))
	if err != nil {
		return nil, fmt.Errorf("save sample retirement preview: %w", err)
	}
	if persisted == nil {
		return nil, errors.New("save sample retirement preview: persisted preview is missing")
	}
	return clonePreview(persisted), nil
}

func validateRequest(request types.SampleRetirementRequest) error {
	if request.OrganizationID == uuid.Nil {
		return errors.New("sample retirement organization is required")
	}
	if request.RequestedByUserAccountID == uuid.Nil {
		return errors.New("sample retirement requester is required")
	}
	if request.Selector.Wildcard != "" {
		return errors.New("wildcard sample retirement selectors are forbidden")
	}
	if request.Selector.NamePattern != "" {
		return errors.New("name pattern sample retirement selectors are forbidden")
	}
	if request.Selector.OlderThan != nil {
		return errors.New("age-based sample retirement selectors are forbidden")
	}
	if len(request.Items) == 0 {
		return errors.New("sample retirement requires an exact UUID allowlist")
	}
	if err := validateExactValue(request.BackupReference, "backup reference"); err != nil {
		return err
	}
	if !checksumPattern.MatchString(request.BackupChecksum) {
		return errors.New("backup checksum must be canonical lowercase sha256")
	}
	if err := validateExactValue(request.RestoreProofReference, "restore proof reference"); err != nil {
		return err
	}
	if !checksumPattern.MatchString(request.RestoreProofChecksum) {
		return errors.New("restore proof checksum must be canonical lowercase sha256")
	}
	seenIDs := make(map[uuid.UUID]struct{}, len(request.Items))
	for index, subject := range request.Items {
		if !allowedSubjectType(subject.SubjectType) {
			return fmt.Errorf(
				"sample retirement item %d has unsupported subject type %q",
				index,
				subject.SubjectType,
			)
		}
		if subject.SubjectID == uuid.Nil {
			return fmt.Errorf(
				"sample retirement item %d requires an exact UUID",
				index,
			)
		}
		if _, duplicate := seenIDs[subject.SubjectID]; duplicate {
			return fmt.Errorf(
				"sample retirement allowlist contains duplicate UUID %s",
				subject.SubjectID,
			)
		}
		seenIDs[subject.SubjectID] = struct{}{}
		if err := validateExactValue(
			subject.OwnershipMarker,
			fmt.Sprintf("item %d ownership marker", index),
		); err != nil {
			return err
		}
		if strings.ContainsAny(subject.OwnershipMarker, "*?[]{}%") {
			return fmt.Errorf(
				"sample retirement item %d ownership marker must not contain a pattern",
				index,
			)
		}
		if !checksumPattern.MatchString(subject.OwnershipChecksum) {
			return fmt.Errorf(
				"sample retirement item %d ownership checksum must be canonical lowercase sha256",
				index,
			)
		}
		if subject.OwnershipChecksum != checksumText(subject.OwnershipMarker) {
			return fmt.Errorf(
				"sample retirement item %d ownership checksum does not bind its exact marker",
				index,
			)
		}
		if !checksumPattern.MatchString(subject.ExpectedChecksum) {
			return fmt.Errorf(
				"sample retirement item %d expected checksum must be canonical lowercase sha256",
				index,
			)
		}
	}
	return nil
}

func validateExactValue(value, field string) error {
	if value == "" || strings.TrimSpace(value) != value {
		return fmt.Errorf("sample retirement %s must be a non-empty exact value", field)
	}
	return nil
}

func allowedSubjectType(subjectType types.SampleRetirementSubjectType) bool {
	switch subjectType {
	case types.SampleRetirementSubjectApplication,
		types.SampleRetirementSubjectDeploymentTarget,
		types.SampleRetirementSubjectEnvironment:
		return true
	default:
		return false
	}
}

func validateCandidates(
	organizationID uuid.UUID,
	requested []types.SampleRetirementSubject,
	candidates []types.SampleRetirementCandidate,
) ([]types.SampleRetirementCandidate, error) {
	requestedByKey := make(map[string]types.SampleRetirementSubject, len(requested))
	for _, subject := range requested {
		requestedByKey[subjectKey(subject)] = subject
	}
	seen := make(map[string]struct{}, len(candidates))
	normalized := append([]types.SampleRetirementCandidate(nil), candidates...)
	for _, candidate := range normalized {
		key := subjectKey(candidate.Subject)
		if _, duplicate := seen[key]; duplicate {
			return nil, fmt.Errorf("duplicate current sample retirement subject %s", key)
		}
		seen[key] = struct{}{}
	}
	if len(candidates) != len(requested) {
		return nil, fmt.Errorf(
			"current subjects do not match the exact allowlist: got %d, want %d",
			len(candidates),
			len(requested),
		)
	}
	for _, candidate := range normalized {
		key := subjectKey(candidate.Subject)
		expected, found := requestedByKey[key]
		if !found {
			return nil, fmt.Errorf(
				"current subject %s is outside the exact allowlist",
				key,
			)
		}
		if candidate.OrganizationID != organizationID {
			return nil, fmt.Errorf(
				"sample retirement subject %s is cross-organization",
				key,
			)
		}
		if !candidate.Immutable {
			return nil, fmt.Errorf(
				"sample retirement subject %s has mutable preview facts",
				key,
			)
		}
		if candidate.BeforeCount != 1 {
			return nil, fmt.Errorf(
				"sample retirement subject %s has invalid before count",
				key,
			)
		}
		if !checksumPattern.MatchString(candidate.CurrentChecksum) ||
			candidate.CurrentChecksum != expected.ExpectedChecksum ||
			candidate.Subject.ExpectedChecksum != expected.ExpectedChecksum {
			return nil, fmt.Errorf(
				"sample retirement subject %s has a stale expected checksum",
				key,
			)
		}
		if candidate.OwnershipMarker != expected.OwnershipMarker ||
			candidate.Subject.OwnershipMarker != expected.OwnershipMarker {
			return nil, fmt.Errorf(
				"sample retirement subject %s ownership marker changed",
				key,
			)
		}
		if candidate.OwnershipChecksum != expected.OwnershipChecksum ||
			candidate.Subject.OwnershipChecksum != expected.OwnershipChecksum {
			return nil, fmt.Errorf(
				"sample retirement subject %s ownership checksum changed",
				key,
			)
		}
	}
	sort.Slice(normalized, func(i, j int) bool {
		return lessSubject(normalized[i].Subject, normalized[j].Subject)
	})
	return normalized, nil
}

func validateReferenceReports(
	organizationID uuid.UUID,
	candidates []types.SampleRetirementCandidate,
	reports []types.ReferenceReport,
) ([]types.ReferenceReport, error) {
	if len(reports) != len(candidates) {
		return nil, fmt.Errorf(
			"reverse reference report count does not match exact allowlist: got %d, want %d",
			len(reports),
			len(candidates),
		)
	}
	candidatesByKey := make(map[string]types.SampleRetirementCandidate, len(candidates))
	for _, candidate := range candidates {
		candidatesByKey[subjectKey(candidate.Subject)] = candidate
	}
	seen := make(map[string]struct{}, len(reports))
	normalized := cloneReports(reports)
	for reportIndex := range normalized {
		report := &normalized[reportIndex]
		key := subjectKey(report.Subject)
		if _, duplicate := seen[key]; duplicate {
			return nil, fmt.Errorf("duplicate reference report for %s", key)
		}
		seen[key] = struct{}{}
		candidate, found := candidatesByKey[key]
		if !found ||
			report.Subject != candidate.Subject {
			return nil, fmt.Errorf(
				"reference report subject %s does not match the exact allowlist",
				key,
			)
		}
		if report.SubjectOrganizationID != organizationID {
			return nil, fmt.Errorf(
				"reference report for %s is cross-organization",
				key,
			)
		}
		if !report.Immutable {
			return nil, fmt.Errorf(
				"reference report for %s has mutable preview inputs",
				key,
			)
		}
		if report.CurrentChecksum != candidate.CurrentChecksum ||
			report.BeforeCount != candidate.BeforeCount {
			return nil, fmt.Errorf(
				"reference report for %s is stale",
				key,
			)
		}
		if report.ProtectedReferenceCount < 0 ||
			report.CrossOrganizationReferenceCount < 0 ||
			report.AuditEventCount < 0 {
			return nil, fmt.Errorf(
				"reference report for %s contains an invalid count",
				key,
			)
		}
		if report.ProtectedReferenceCount > 0 {
			return nil, fmt.Errorf(
				"sample retirement subject %s has a protected reverse reference",
				key,
			)
		}
		if report.CrossOrganizationReferenceCount > 0 {
			return nil, fmt.Errorf(
				"sample retirement subject %s has a cross-organization reverse reference",
				key,
			)
		}
		for referenceIndex, reference := range report.References {
			if reference.Protected {
				return nil, fmt.Errorf(
					"sample retirement subject %s has a protected reverse reference",
					key,
				)
			}
			if reference.OrganizationID != organizationID {
				return nil, fmt.Errorf(
					"sample retirement subject %s has a cross-organization reverse reference",
					key,
				)
			}
			if reference.SourceID == uuid.Nil ||
				strings.TrimSpace(reference.SourceType) == "" ||
				strings.TrimSpace(reference.Relationship) == "" {
				return nil, fmt.Errorf(
					"reference report for %s contains invalid reference %d",
					key,
					referenceIndex,
				)
			}
		}
		if !report.Retirable {
			return nil, fmt.Errorf(
				"sample retirement subject %s is not retirable: %s",
				key,
				strings.Join(report.BlockingReasons, ", "),
			)
		}
		if len(report.BlockingReasons) != 0 {
			return nil, fmt.Errorf(
				"sample retirement subject %s has blocking reasons",
				key,
			)
		}
		sort.Slice(report.References, func(i, j int) bool {
			return lessReference(report.References[i], report.References[j])
		})
	}
	sort.Slice(normalized, func(i, j int) bool {
		return lessSubject(normalized[i].Subject, normalized[j].Subject)
	})
	return normalized, nil
}

func canonicalAllowlistChecksum(
	organizationID uuid.UUID,
	subjects []types.SampleRetirementSubject,
) (string, error) {
	document := struct {
		Schema         string                          `json:"schema"`
		OrganizationID string                          `json:"organizationId"`
		Items          []types.SampleRetirementSubject `json:"items"`
	}{
		Schema:         allowlistSchema,
		OrganizationID: organizationID.String(),
		Items:          sortedSubjects(subjects),
	}
	return checksumJSON(document)
}

func canonicalPreviewChecksum(
	request types.SampleRetirementRequest,
	allowlistChecksum string,
	candidates []types.SampleRetirementCandidate,
	reports []types.ReferenceReport,
) (string, error) {
	document := struct {
		Schema                   string                            `json:"schema"`
		OrganizationID           string                            `json:"organizationId"`
		RequestedByUserAccountID string                            `json:"requestedByUserAccountId"`
		BackupReference          string                            `json:"backupReference"`
		BackupChecksum           string                            `json:"backupChecksum"`
		RestoreProofReference    string                            `json:"restoreProofReference"`
		RestoreProofChecksum     string                            `json:"restoreProofChecksum"`
		AllowlistChecksum        string                            `json:"allowlistChecksum"`
		Candidates               []types.SampleRetirementCandidate `json:"candidates"`
		ReferenceReports         []types.ReferenceReport           `json:"referenceReports"`
	}{
		Schema:                   previewSchema,
		OrganizationID:           request.OrganizationID.String(),
		RequestedByUserAccountID: request.RequestedByUserAccountID.String(),
		BackupReference:          request.BackupReference,
		BackupChecksum:           request.BackupChecksum,
		RestoreProofReference:    request.RestoreProofReference,
		RestoreProofChecksum:     request.RestoreProofChecksum,
		AllowlistChecksum:        allowlistChecksum,
		Candidates: append(
			[]types.SampleRetirementCandidate(nil),
			candidates...,
		),
		ReferenceReports: cloneReports(reports),
	}
	return checksumJSON(document)
}

func canonicalReferenceReportChecksum(
	report types.ReferenceReport,
) (string, error) {
	document := struct {
		Schema string                `json:"schema"`
		Report types.ReferenceReport `json:"report"`
	}{
		Schema: referenceSchema,
		Report: report,
	}
	return checksumJSON(document)
}

func checksumJSON(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func checksumText(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func sortedSubjects(
	subjects []types.SampleRetirementSubject,
) []types.SampleRetirementSubject {
	result := append([]types.SampleRetirementSubject(nil), subjects...)
	sort.Slice(result, func(i, j int) bool {
		return lessSubject(result[i], result[j])
	})
	return result
}

func lessSubject(
	left types.SampleRetirementSubject,
	right types.SampleRetirementSubject,
) bool {
	if left.SubjectType != right.SubjectType {
		return left.SubjectType < right.SubjectType
	}
	return left.SubjectID.String() < right.SubjectID.String()
}

func lessReference(
	left types.RetirementReference,
	right types.RetirementReference,
) bool {
	if left.SourceType != right.SourceType {
		return left.SourceType < right.SourceType
	}
	if left.SourceID != right.SourceID {
		return left.SourceID.String() < right.SourceID.String()
	}
	if left.Relationship != right.Relationship {
		return left.Relationship < right.Relationship
	}
	return left.OrganizationID.String() < right.OrganizationID.String()
}

func subjectKey(subject types.SampleRetirementSubject) string {
	return string(subject.SubjectType) + "/" + subject.SubjectID.String()
}

func cloneReports(reports []types.ReferenceReport) []types.ReferenceReport {
	result := make([]types.ReferenceReport, len(reports))
	for index, report := range reports {
		result[index] = report
		result[index].References = append(
			[]types.RetirementReference(nil),
			report.References...,
		)
		result[index].BlockingReasons = append(
			[]string(nil),
			report.BlockingReasons...,
		)
	}
	return result
}

func clonePreview(
	preview *types.SampleRetirementPreview,
) *types.SampleRetirementPreview {
	if preview == nil {
		return nil
	}
	result := *preview
	result.Items = append([]types.SampleRetirementItem(nil), preview.Items...)
	result.ReferenceReports = cloneReports(preview.ReferenceReports)
	return &result
}
