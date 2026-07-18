package auditexport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/distr-sh/distr/internal/types"
)

const (
	defaultAuditPayloadLimit = 32 * 1024
	redactedValue            = "[REDACTED]"
)

func BuildDeploymentEvidenceBundle(
	query types.EvidenceBundleQuery,
	events []types.ControlPlaneAuditEvent,
) (*types.EvidenceBundle, error) {
	if query.OrganizationID == [16]byte{} || query.DeploymentPlanID == [16]byte{} {
		return nil, errors.New("organization and deployment plan are required")
	}

	ordered := append([]types.ControlPlaneAuditEvent(nil), events...)
	for i := range ordered {
		if ordered[i].OrganizationID != query.OrganizationID {
			return nil, errors.New("audit event belongs to another organization")
		}
		if ordered[i].DeploymentPlanID == nil || *ordered[i].DeploymentPlanID != query.DeploymentPlanID {
			return nil, errors.New("audit event belongs to another deployment plan")
		}
		payload, redacted, truncated, err := RedactAuditPayload(ordered[i].Payload, defaultAuditPayloadLimit)
		if err != nil {
			return nil, fmt.Errorf("redact audit event %s: %w", ordered[i].ID, err)
		}
		ordered[i].Payload = payload
		ordered[i].PayloadRedacted = ordered[i].PayloadRedacted || redacted
		ordered[i].PayloadTruncated = ordered[i].PayloadTruncated || truncated
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Sequence != ordered[j].Sequence {
			return ordered[i].Sequence < ordered[j].Sequence
		}
		if !ordered[i].CreatedAt.Equal(ordered[j].CreatedAt) {
			return ordered[i].CreatedAt.Before(ordered[j].CreatedAt)
		}
		return ordered[i].ID.String() < ordered[j].ID.String()
	})

	canonical := struct {
		OrganizationID   [16]byte                       `json:"organizationId"`
		DeploymentPlanID [16]byte                       `json:"deploymentPlanId"`
		Events           []types.ControlPlaneAuditEvent `json:"events"`
	}{
		OrganizationID:   query.OrganizationID,
		DeploymentPlanID: query.DeploymentPlanID,
		Events:           ordered,
	}
	payload, err := json.Marshal(canonical)
	if err != nil {
		return nil, fmt.Errorf("marshal evidence bundle: %w", err)
	}
	sum := sha256.Sum256(payload)
	return &types.EvidenceBundle{
		OrganizationID:   query.OrganizationID,
		DeploymentPlanID: query.DeploymentPlanID,
		Events:           ordered,
		Checksum:         "sha256:" + hex.EncodeToString(sum[:]),
	}, nil
}

func RedactAuditPayload(
	payload json.RawMessage,
	maxBytes int,
) (redacted json.RawMessage, changed bool, truncated bool, err error) {
	if len(payload) == 0 {
		return nil, false, false, nil
	}
	if maxBytes < 48 {
		return nil, false, false, errors.New("audit payload limit is too small")
	}

	var value any
	if err := json.Unmarshal(payload, &value); err != nil {
		return nil, false, false, errors.New("audit payload must be valid JSON")
	}
	value, changed = redactValue("", value)
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, false, false, fmt.Errorf("marshal redacted payload: %w", err)
	}
	if len(encoded) <= maxBytes {
		return encoded, changed, false, nil
	}
	fallback, err := json.Marshal(map[string]any{
		"redacted":  true,
		"truncated": true,
	})
	if err != nil {
		return nil, false, false, err
	}
	return fallback, true, true, nil
}

func redactValue(key string, value any) (any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		changed := false
		for childKey, childValue := range typed {
			if secretKey(childKey) {
				typed[childKey] = redactedValue
				changed = true
				continue
			}
			next, childChanged := redactValue(childKey, childValue)
			typed[childKey] = next
			changed = changed || childChanged
		}
		return typed, changed
	case []any:
		changed := false
		for i := range typed {
			next, childChanged := redactValue(key, typed[i])
			typed[i] = next
			changed = changed || childChanged
		}
		return typed, changed
	case string:
		lower := strings.ToLower(typed)
		if strings.Contains(lower, "bearer ") ||
			strings.Contains(lower, "password=") ||
			strings.Contains(lower, "token=") ||
			strings.Contains(lower, "api_key=") {
			return redactedValue, true
		}
	}
	return value, false
}

func secretKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "-", ""), "_", ""))
	switch normalized {
	case "authorization", "password", "secret", "token", "apikey", "privatekey", "credential":
		return true
	default:
		return false
	}
}
