package auditexport

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
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
		payload, redacted, truncated, err := RedactAuditPayload(ordered[i].Payload, defaultAuditPayloadLimit)
		if err != nil {
			return nil, fmt.Errorf("redact audit event %s: %w", ordered[i].ID, err)
		}
		ordered[i].Payload = payload
		ordered[i].PayloadRedacted = ordered[i].PayloadRedacted || redacted
		ordered[i].PayloadTruncated = ordered[i].PayloadTruncated || truncated
	}
	if err := validateEvidenceCorrelationGraph(query.DeploymentPlanID, ordered); err != nil {
		return nil, err
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
	if !json.Valid(payload) {
		return nil, false, false, errors.New("audit payload must be valid JSON")
	}

	var value any
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
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
		return RedactAuditText(typed)
	}
	return value, false
}

func secretKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "-", ""), "_", ""))
	switch normalized {
	case "authorization", "proxyauthorization", "password", "passwd", "passphrase",
		"secret", "clientsecret", "token", "accesstoken", "refreshtoken", "idtoken",
		"apikey", "accesskey", "secretaccesskey", "privatekey", "sshkey", "credential",
		"credentials", "cookie", "setcookie", "session", "sessionid", "connectionstring",
		"certificate", "pfx", "pkcs12":
		return true
	default:
		return strings.HasSuffix(normalized, "password") ||
			strings.HasSuffix(normalized, "secret") ||
			strings.HasSuffix(normalized, "token") ||
			strings.HasSuffix(normalized, "privatekey") ||
			strings.HasSuffix(normalized, "credential")
	}
}

func RedactAuditText(value string) (string, bool) {
	lower := strings.ToLower(value)
	for _, marker := range []string{
		"bearer ", "basic ", "password=", "passwd=", "token=", "api_key=", "apikey=",
		"client_secret=", "secret=", "authorization:", "set-cookie:", "-----begin private key-----",
		"-----begin rsa private key-----", "-----begin openssh private key-----",
	} {
		if strings.Contains(lower, marker) {
			return redactedValue, true
		}
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Scheme != "" && parsed.User != nil {
		return redactedValue, true
	}
	parts := strings.Split(value, ".")
	if len(parts) == 3 && len(parts[0]) >= 10 && len(parts[1]) >= 10 && len(parts[2]) >= 10 {
		return redactedValue, true
	}
	return value, false
}

func validateEvidenceCorrelationGraph(
	deploymentPlanID [16]byte,
	events []types.ControlPlaneAuditEvent,
) error {
	if len(events) == 0 {
		return errors.New("deployment evidence is empty")
	}
	byCorrelation := map[types.AuditCorrelation][]int{}
	visited := make([]bool, len(events))
	queue := make([]int, 0, len(events))
	for i, event := range events {
		if event.DeploymentPlanID != nil && *event.DeploymentPlanID != deploymentPlanID {
			continue
		}
		for _, correlation := range event.Correlations() {
			byCorrelation[correlation] = append(byCorrelation[correlation], i)
		}
		if event.DeploymentPlanID != nil && *event.DeploymentPlanID == deploymentPlanID {
			visited[i] = true
			queue = append(queue, i)
		}
	}
	if len(queue) == 0 {
		return errors.New("deployment evidence has no plan root")
	}
	for len(queue) > 0 {
		index := queue[0]
		queue = queue[1:]
		for _, correlation := range events[index].Correlations() {
			for _, candidate := range byCorrelation[correlation] {
				if !visited[candidate] {
					visited[candidate] = true
					queue = append(queue, candidate)
				}
			}
		}
	}
	for _, connected := range visited {
		if !connected {
			return errors.New("audit event is disconnected from deployment plan")
		}
	}
	return nil
}
