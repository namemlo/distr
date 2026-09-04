package externalexecution

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

var preMutationHoldChecksumPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

var ErrPreMutationHoldWaiting = errors.New("external execution is waiting at the pre-mutation hold")

func ParsePreMutationHold(value []byte) (*types.ExternalExecutionPreMutationHold, error) {
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	var control types.ExternalExecutionPreMutationHold
	if err := decoder.Decode(&control); err != nil {
		return nil, fmt.Errorf("decode pre-mutation hold: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return nil, err
	}
	control.Component = strings.TrimSpace(control.Component)
	control.Reason = strings.TrimSpace(control.Reason)
	control.ExpiresAt = control.ExpiresAt.UTC()
	control.ControlChecksum = ""
	if err := ValidatePreMutationHold(control); err != nil {
		return nil, err
	}
	checksum, err := PreMutationHoldChecksum(control)
	if err != nil {
		return nil, err
	}
	control.ControlChecksum = checksum
	return &control, nil
}

func ValidatePreMutationHold(control types.ExternalExecutionPreMutationHold) error {
	switch {
	case control.Schema != types.ExternalExecutionPreMutationHoldSchemaV1:
		return fmt.Errorf("pre-mutation hold schema is invalid")
	case control.ControlID == uuid.Nil:
		return fmt.Errorf("pre-mutation hold controlId is required")
	case control.OrganizationID == uuid.Nil:
		return fmt.Errorf("pre-mutation hold organizationId is required")
	case control.DeploymentPlanID == uuid.Nil:
		return fmt.Errorf("pre-mutation hold deploymentPlanId is required")
	case control.DeploymentTargetID == uuid.Nil:
		return fmt.Errorf("pre-mutation hold deploymentTargetId is required")
	case !preMutationHoldChecksumPattern.MatchString(control.PlanChecksum):
		return fmt.Errorf("pre-mutation hold planChecksum is invalid")
	case strings.TrimSpace(control.Component) == "" || len(control.Component) > 128:
		return fmt.Errorf("pre-mutation hold component is invalid")
	case strings.ContainsAny(control.Component, "\r\n"):
		return fmt.Errorf("pre-mutation hold component is invalid")
	case strings.TrimSpace(control.Reason) == "" || len(control.Reason) > 512:
		return fmt.Errorf("pre-mutation hold reason is invalid")
	case strings.ContainsAny(control.Reason, "\r\n"):
		return fmt.Errorf("pre-mutation hold reason is invalid")
	case control.ExpiresAt.IsZero():
		return fmt.Errorf("pre-mutation hold expiresAt is required")
	case control.ControlChecksum != "" && !preMutationHoldChecksumPattern.MatchString(control.ControlChecksum):
		return fmt.Errorf("pre-mutation hold controlChecksum is invalid")
	default:
		return nil
	}
}

func PreMutationHoldChecksum(control types.ExternalExecutionPreMutationHold) (string, error) {
	control.Component = strings.TrimSpace(control.Component)
	control.Reason = strings.TrimSpace(control.Reason)
	control.ExpiresAt = control.ExpiresAt.UTC()
	control.ControlChecksum = ""
	if err := ValidatePreMutationHold(control); err != nil {
		return "", err
	}
	payload, err := json.Marshal(control)
	if err != nil {
		return "", fmt.Errorf("canonicalize pre-mutation hold: %w", err)
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func ParsePreMutationHoldRelease(value []byte) (*types.ExternalExecutionPreMutationHoldRelease, error) {
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	var release types.ExternalExecutionPreMutationHoldRelease
	if err := decoder.Decode(&release); err != nil {
		return nil, fmt.Errorf("decode pre-mutation hold release: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return nil, err
	}
	release.Component = strings.TrimSpace(release.Component)
	if err := ValidatePreMutationHoldRelease(release); err != nil {
		return nil, err
	}
	return &release, nil
}

func ValidatePreMutationHoldRelease(release types.ExternalExecutionPreMutationHoldRelease) error {
	switch {
	case release.Schema != types.ExternalExecutionPreMutationHoldReleaseSchemaV1:
		return fmt.Errorf("pre-mutation hold release schema is invalid")
	case release.Action != string(types.ExternalExecutionPreMutationHoldReleaseFail):
		return fmt.Errorf("pre-mutation hold release action must be RELEASE_FAIL")
	case release.ControlID == uuid.Nil:
		return fmt.Errorf("pre-mutation hold release controlId is required")
	case !preMutationHoldChecksumPattern.MatchString(release.ControlChecksum):
		return fmt.Errorf("pre-mutation hold release controlChecksum is invalid")
	case release.OrganizationID == uuid.Nil:
		return fmt.Errorf("pre-mutation hold release organizationId is required")
	case release.DeploymentPlanID == uuid.Nil:
		return fmt.Errorf("pre-mutation hold release deploymentPlanId is required")
	case release.DeploymentTargetID == uuid.Nil:
		return fmt.Errorf("pre-mutation hold release deploymentTargetId is required")
	case !preMutationHoldChecksumPattern.MatchString(release.PlanChecksum):
		return fmt.Errorf("pre-mutation hold release planChecksum is invalid")
	case strings.TrimSpace(release.Component) == "" || len(release.Component) > 128:
		return fmt.Errorf("pre-mutation hold release component is invalid")
	case strings.ContainsAny(release.Component, "\r\n"):
		return fmt.Errorf("pre-mutation hold release component is invalid")
	default:
		return nil
	}
}

func MatchesPreMutationHoldRelease(
	release types.ExternalExecutionPreMutationHoldRelease,
	control types.ExternalExecutionPreMutationHold,
) bool {
	return release.ControlID == control.ControlID &&
		release.ControlChecksum == control.ControlChecksum &&
		release.OrganizationID == control.OrganizationID &&
		release.DeploymentPlanID == control.DeploymentPlanID &&
		release.DeploymentTargetID == control.DeploymentTargetID &&
		release.PlanChecksum == control.PlanChecksum &&
		strings.TrimSpace(release.Component) == strings.TrimSpace(control.Component)
}

func WaitForPreMutationHoldRelease(
	ctx context.Context,
	control types.ExternalExecutionPreMutationHold,
	releaseFile string,
	pollInterval time.Duration,
) (types.ExternalExecutionPreMutationHoldResolution, error) {
	if err := ValidatePreMutationHold(control); err != nil {
		return "", err
	}
	releaseFile = strings.TrimSpace(releaseFile)
	if pollInterval <= 0 {
		pollInterval = 250 * time.Millisecond
	}
	for {
		remaining := time.Until(control.ExpiresAt)
		if remaining <= 0 {
			return types.ExternalExecutionPreMutationHoldTimedOut, nil
		}
		if releaseFile != "" {
			if value, err := os.ReadFile(releaseFile); err == nil {
				release, parseErr := ParsePreMutationHoldRelease(value)
				if parseErr == nil && MatchesPreMutationHoldRelease(*release, control) {
					return types.ExternalExecutionPreMutationHoldReleaseFail, nil
				}
			}
		}
		wait := min(pollInterval, remaining)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", ctx.Err()
		case <-timer.C:
		}
	}
}

func MatchesPreMutationHold(
	control types.ExternalExecutionPreMutationHold,
	execution types.ExternalExecution,
) bool {
	return control.OrganizationID == execution.OrganizationID &&
		control.DeploymentPlanID == execution.DeploymentPlanID &&
		control.DeploymentTargetID == execution.DeploymentTargetID &&
		control.PlanChecksum == execution.PlanChecksum &&
		strings.TrimSpace(control.Component) == execution.Component
}

func requireJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("decode pre-mutation hold: multiple JSON values are not allowed")
		}
		return fmt.Errorf("decode pre-mutation hold: %w", err)
	}
	return nil
}
