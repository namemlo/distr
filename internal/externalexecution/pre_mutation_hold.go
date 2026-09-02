package externalexecution

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

var preMutationHoldChecksumPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

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
	case control.ControlChecksum != "" && !preMutationHoldChecksumPattern.MatchString(control.ControlChecksum):
		return fmt.Errorf("pre-mutation hold controlChecksum is invalid")
	default:
		return nil
	}
}

func PreMutationHoldChecksum(control types.ExternalExecutionPreMutationHold) (string, error) {
	control.Component = strings.TrimSpace(control.Component)
	control.Reason = strings.TrimSpace(control.Reason)
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
