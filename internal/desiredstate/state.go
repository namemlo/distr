package desiredstate

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

var checksumPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func Admit(
	input types.PendingDesiredRevisionInput,
	active *types.ActiveDesiredRevision,
	now time.Time,
) (*types.PendingDesiredRevision, *types.ActiveDesiredRevision, error) {
	if err := validateInput(input, now); err != nil {
		return nil, active, err
	}
	revision := int64(1)
	if active != nil {
		revision = active.Revision + 1
	}
	pending := &types.PendingDesiredRevision{
		ID: uuid.New(), CreatedAt: now, UpdatedAt: now,
		OrganizationID: input.OrganizationID, DeploymentPlanID: input.DeploymentPlanID,
		ExecutionID: input.ExecutionID, DeploymentUnitID: input.DeploymentUnitID,
		ComponentInstanceID: input.ComponentInstanceID, ComponentKey: strings.TrimSpace(input.ComponentKey),
		Revision: revision, ArtifactDigest: input.ArtifactDigest,
		ConfigChecksum: input.ConfigChecksum, SchemaVersion: strings.TrimSpace(input.SchemaVersion),
		CapabilityChecksum: input.CapabilityChecksum, Platform: strings.TrimSpace(input.Platform),
		TopologyChecksum: input.TopologyChecksum, ObservationDeadline: input.ObservationDeadline,
		Status: types.PendingDesiredStatusPending,
	}
	return pending, active, nil
}

func Advance(
	active *types.ActiveDesiredRevision,
	pending types.PendingDesiredRevision,
	gate types.ObservationGateResult,
	now time.Time,
) (*types.ActiveDesiredRevision, types.PendingDesiredRevision, error) {
	if pending.Status != types.PendingDesiredStatusPending {
		return active, pending, errors.New("pending desired revision is already terminal")
	}
	pending.UpdatedAt = now
	pending.TerminalReason = gate.Reason
	pending.TerminalAt = new(now)
	switch gate.Status {
	case types.ObservationGateStatusVerified:
		if gate.ObservationID == uuid.Nil || !checksumPattern.MatchString(gate.ObservationChecksum) {
			return active, pending, errors.New("verified gate requires observation identity and checksum")
		}
		pending.Status = types.PendingDesiredStatusVerified
		pending.VerifiedObservationID = gate.ObservationID
		next := &types.ActiveDesiredRevision{
			ID: uuid.New(), CreatedAt: now, OrganizationID: pending.OrganizationID,
			PendingRevisionID: pending.ID, DeploymentPlanID: pending.DeploymentPlanID,
			ExecutionID: pending.ExecutionID, DeploymentUnitID: pending.DeploymentUnitID,
			ComponentInstanceID: pending.ComponentInstanceID, ComponentKey: pending.ComponentKey,
			Revision: pending.Revision, ArtifactDigest: pending.ArtifactDigest,
			ConfigChecksum: pending.ConfigChecksum, SchemaVersion: pending.SchemaVersion,
			CapabilityChecksum: pending.CapabilityChecksum, Platform: pending.Platform,
			TopologyChecksum: pending.TopologyChecksum, VerifiedObservationID: gate.ObservationID,
		}
		return next, pending, nil
	case types.ObservationGateStatusPartial:
		if err := requireTerminalEvidence(gate); err != nil {
			return active, pending, err
		}
		pending.Status = types.PendingDesiredStatusPartial
	case types.ObservationGateStatusFailed:
		if err := requireTerminalEvidence(gate); err != nil {
			return active, pending, err
		}
		pending.Status = types.PendingDesiredStatusFailed
	case types.ObservationGateStatusCancelled:
		if err := requireTerminalEvidence(gate); err != nil {
			return active, pending, err
		}
		pending.Status = types.PendingDesiredStatusCancelled
	case types.ObservationGateStatusUnknown:
		if err := requireTerminalEvidence(gate); err != nil {
			return active, pending, err
		}
		pending.Status = types.PendingDesiredStatusUnknown
	case types.ObservationGateStatusTimedOut:
		pending.Status = types.PendingDesiredStatusTimedOut
	case types.ObservationGateStatusConflict:
		if err := requireTerminalEvidence(gate); err != nil {
			return active, pending, err
		}
		pending.Status = types.PendingDesiredStatusConflict
	default:
		pending.TerminalAt = nil
		return active, pending, nil
	}
	if gate.Status != types.ObservationGateStatusTimedOut {
		pending.TerminalObservationID = gate.ObservationID
	}
	return active, pending, nil
}

func requireTerminalEvidence(gate types.ObservationGateResult) error {
	if gate.ObservationID == uuid.Nil ||
		!checksumPattern.MatchString(gate.ObservationChecksum) {
		return errors.New(
			"non-timeout terminal gate requires trusted observation identity and checksum",
		)
	}
	return nil
}

func validateInput(input types.PendingDesiredRevisionInput, now time.Time) error {
	if input.OrganizationID == uuid.Nil || input.DeploymentPlanID == uuid.Nil ||
		input.ExecutionID == uuid.Nil || input.DeploymentUnitID == uuid.Nil ||
		input.ComponentInstanceID == uuid.Nil {
		return errors.New("desired state identities are required")
	}
	if strings.TrimSpace(input.ComponentKey) == "" || strings.TrimSpace(input.SchemaVersion) == "" ||
		strings.TrimSpace(input.Platform) == "" {
		return errors.New("desired component key, schema version, and platform are required")
	}
	for _, checksum := range []string{
		input.ArtifactDigest, input.ConfigChecksum, input.CapabilityChecksum, input.TopologyChecksum,
	} {
		if !checksumPattern.MatchString(checksum) {
			return errors.New("desired state checksums must be lowercase sha256")
		}
	}
	if !input.ObservationDeadline.After(now) {
		return errors.New("observation deadline must be in the future")
	}
	return nil
}
