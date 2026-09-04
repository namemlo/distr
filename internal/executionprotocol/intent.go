package executionprotocol

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

var intentChecksumPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

const (
	executionIntentSchemaV3 = "distr.execution-intent/v3"
	executionIntentSchemaV4 = "distr.execution-intent/v4"
)

type intentSignerContextKey struct{}

func WithIntentSigner(ctx context.Context, signer IntentSigner) context.Context {
	return context.WithValue(ctx, intentSignerContextKey{}, signer)
}

type canonicalIntent struct {
	Schema                               string                                `json:"schema"`
	OrganizationID                       uuid.UUID                             `json:"organizationId"`
	DeploymentTargetID                   uuid.UUID                             `json:"deploymentTargetId"`
	AttemptID                            uuid.UUID                             `json:"attemptId"`
	TaskID                               uuid.UUID                             `json:"taskId"`
	StepRunID                            uuid.UUID                             `json:"stepRunId"`
	ExecutionID                          uuid.UUID                             `json:"executionId"`
	AttemptNumber                        int                                   `json:"attemptNumber"`
	StepKey                              string                                `json:"stepKey"`
	PlanChecksum                         string                                `json:"planChecksum"`
	ArtifactDigest                       string                                `json:"artifactDigest"`
	ConfigChecksum                       string                                `json:"configChecksum"`
	RuntimeManifestChecksum              string                                `json:"runtimeManifestChecksum,omitempty"`
	DesiredServiceConfigChecksum         string                                `json:"desiredServiceConfigChecksum,omitempty"`
	AdapterRevision                      string                                `json:"adapterRevision"`
	RuntimeContractVersion               types.ExecutionRuntimeContractVersion `json:"runtimeContractVersion"`
	ExpectedObservedStateVersion         int64                                 `json:"expectedObservedStateVersion"`
	ExpectedObservedStateChecksum        string                                `json:"expectedObservedStateChecksum"`
	ExpectedCurrentImageDigest           string                                `json:"expectedCurrentImageDigest"`
	ExpectedCurrentConfigChecksum        string                                `json:"expectedCurrentConfigChecksum"`
	ExpectedCurrentServiceConfigChecksum string                                `json:"expectedCurrentServiceConfigChecksum,omitempty"`
	ExpectedPlatform                     types.DeploymentTargetPlatform        `json:"expectedPlatform"`
	CallerBinding                        string                                `json:"callerBinding"`
	Audience                             string                                `json:"audience"`
	ResourceKey                          string                                `json:"resourceKey"`
	FenceGeneration                      int64                                 `json:"fenceGeneration"`
	IssuedAt                             time.Time                             `json:"issuedAt"`
	ExpiresAt                            time.Time                             `json:"expiresAt"`
}

func BuildExecutionIntent(
	ctx context.Context,
	attempt types.ExecutionAttempt,
) (types.SignedExecutionIntent, error) {
	signer, ok := ctx.Value(intentSignerContextKey{}).(IntentSigner)
	if !ok || signer == nil {
		return types.SignedExecutionIntent{}, errors.New("intent signer is not configured")
	}
	if err := validateIntentAttempt(attempt); err != nil {
		return types.SignedExecutionIntent{}, err
	}
	value := canonicalIntent{
		Schema: executionIntentSchema(attempt.RuntimeContractVersion), OrganizationID: attempt.OrganizationID,
		DeploymentTargetID: attempt.DeploymentTargetID, AttemptID: attempt.ID,
		TaskID: attempt.TaskID, StepRunID: attempt.StepRunID,
		ExecutionID:   attempt.Identity.ExecutionID,
		AttemptNumber: attempt.Identity.AttemptNumber, StepKey: strings.TrimSpace(attempt.Identity.StepKey),
		PlanChecksum: attempt.PlanChecksum, ArtifactDigest: attempt.ArtifactDigest,
		ConfigChecksum: attempt.ConfigChecksum, AdapterRevision: strings.TrimSpace(attempt.AdapterRevision),
		RuntimeManifestChecksum:              attempt.RuntimeManifestChecksum,
		DesiredServiceConfigChecksum:         attempt.DesiredServiceConfigChecksum,
		RuntimeContractVersion:               attempt.RuntimeContractVersion,
		ExpectedObservedStateVersion:         attempt.ExpectedObservedStateVersion,
		ExpectedObservedStateChecksum:        attempt.ExpectedObservedStateChecksum,
		ExpectedCurrentImageDigest:           attempt.ExpectedCurrentImageDigest,
		ExpectedCurrentConfigChecksum:        attempt.ExpectedCurrentConfigChecksum,
		ExpectedCurrentServiceConfigChecksum: attempt.ExpectedCurrentServiceConfigChecksum,
		ExpectedPlatform:                     attempt.ExpectedPlatform,
		CallerBinding:                        strings.TrimSpace(attempt.CallerBinding),
		Audience:                             strings.TrimSpace(attempt.Audience),
		ResourceKey:                          strings.TrimSpace(attempt.Fence.ResourceKey), FenceGeneration: attempt.Fence.Generation,
		IssuedAt: attempt.IntentIssuedAt.UTC(), ExpiresAt: attempt.IntentExpiresAt.UTC(),
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return types.SignedExecutionIntent{}, fmt.Errorf("marshal execution intent: %w", err)
	}
	sum := sha256.Sum256(payload)
	checksum := "sha256:" + hex.EncodeToString(sum[:])
	signature, err := signer.Sign(ctx, signingMessage(payload, checksum))
	if err != nil {
		return types.SignedExecutionIntent{}, fmt.Errorf("sign execution intent: %w", err)
	}
	return types.SignedExecutionIntent{
		Payload: payload, Checksum: checksum, KeyID: signer.KeyID(), Signature: encodeSignature(signature),
	}, nil
}

func ValidateExecutionIntentBinding(
	attempt types.ExecutionAttempt,
	intent types.SignedExecutionIntent,
) error {
	var payload canonicalIntent
	if err := json.Unmarshal(intent.Payload, &payload); err != nil {
		return fmt.Errorf("execution intent payload is invalid: %w", err)
	}
	expected := canonicalIntent{
		Schema: executionIntentSchema(attempt.RuntimeContractVersion), OrganizationID: attempt.OrganizationID,
		DeploymentTargetID: attempt.DeploymentTargetID, AttemptID: attempt.ID,
		TaskID: attempt.TaskID, StepRunID: attempt.StepRunID,
		ExecutionID:   attempt.Identity.ExecutionID,
		AttemptNumber: attempt.Identity.AttemptNumber, StepKey: strings.TrimSpace(attempt.Identity.StepKey),
		PlanChecksum: attempt.PlanChecksum, ArtifactDigest: attempt.ArtifactDigest,
		ConfigChecksum: attempt.ConfigChecksum, AdapterRevision: strings.TrimSpace(attempt.AdapterRevision),
		RuntimeManifestChecksum:              attempt.RuntimeManifestChecksum,
		DesiredServiceConfigChecksum:         attempt.DesiredServiceConfigChecksum,
		RuntimeContractVersion:               attempt.RuntimeContractVersion,
		ExpectedObservedStateVersion:         attempt.ExpectedObservedStateVersion,
		ExpectedObservedStateChecksum:        attempt.ExpectedObservedStateChecksum,
		ExpectedCurrentImageDigest:           attempt.ExpectedCurrentImageDigest,
		ExpectedCurrentConfigChecksum:        attempt.ExpectedCurrentConfigChecksum,
		ExpectedCurrentServiceConfigChecksum: attempt.ExpectedCurrentServiceConfigChecksum,
		ExpectedPlatform:                     attempt.ExpectedPlatform,
		CallerBinding:                        strings.TrimSpace(attempt.CallerBinding),
		Audience:                             strings.TrimSpace(attempt.Audience),
		ResourceKey:                          strings.TrimSpace(attempt.Fence.ResourceKey), FenceGeneration: attempt.Fence.Generation,
		IssuedAt: attempt.IntentIssuedAt.UTC(), ExpiresAt: attempt.IntentExpiresAt.UTC(),
	}
	if payload != expected {
		return errors.New("execution intent binding mismatch")
	}
	return nil
}

func VerifyExecutionIntent(intent types.SignedExecutionIntent, policy types.TrustPolicy) error {
	if err := ValidateTrustPolicy(policy); err != nil {
		return err
	}
	sum := sha256.Sum256(intent.Payload)
	checksum := "sha256:" + hex.EncodeToString(sum[:])
	if checksum != intent.Checksum {
		return errors.New("execution intent checksum mismatch")
	}
	publicKey, ok := policy.Keys[intent.KeyID]
	if !ok {
		return errors.New("execution intent keyId is not trusted")
	}
	if PublicKeyFingerprint(publicKey) != intent.KeyID {
		return errors.New("execution intent key fingerprint mismatch")
	}
	signature, err := decodeSignature(intent.Signature)
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, signingMessage(intent.Payload, intent.Checksum), signature) {
		return errors.New("execution intent signature is invalid")
	}
	var payload canonicalIntent
	if err := json.Unmarshal(intent.Payload, &payload); err != nil {
		return fmt.Errorf("execution intent payload is invalid: %w", err)
	}
	if !validIntentSchemaContract(payload.Schema, payload.RuntimeContractVersion) {
		return errors.New("execution intent schema is invalid")
	}
	now := time.Now().UTC()
	if policy.Now != nil {
		now = policy.Now().UTC()
	}
	if !payload.ExpiresAt.After(now) {
		return errors.New("execution intent is expired")
	}
	if revokedAt, revoked := policy.RevokedKeyIDs[intent.KeyID]; revoked && !now.Before(revokedAt) {
		return errors.New("execution intent key is revoked")
	}
	if policy.ExpectedArtifactDigest != "" && payload.ArtifactDigest != policy.ExpectedArtifactDigest {
		return errors.New("execution intent artifact digest mismatch")
	}
	if policy.ExpectedConfigChecksum != "" && payload.ConfigChecksum != policy.ExpectedConfigChecksum {
		return errors.New("execution intent config checksum mismatch")
	}
	if policy.ExpectedRuntimeManifestChecksum != "" &&
		payload.RuntimeManifestChecksum != policy.ExpectedRuntimeManifestChecksum {
		return errors.New("execution intent runtime manifest checksum mismatch")
	}
	if policy.ExpectedDesiredServiceConfigChecksum != "" &&
		payload.DesiredServiceConfigChecksum != policy.ExpectedDesiredServiceConfigChecksum {
		return errors.New("execution intent desired service config checksum mismatch")
	}
	if policy.ExpectedObservedStateVersion > 0 &&
		payload.ExpectedObservedStateVersion != policy.ExpectedObservedStateVersion {
		return errors.New("execution intent expected observed-state version mismatch")
	}
	if policy.ExpectedObservedStateChecksum != "" &&
		payload.ExpectedObservedStateChecksum != policy.ExpectedObservedStateChecksum {
		return errors.New("execution intent expected observed-state checksum mismatch")
	}
	if policy.ExpectedCurrentImageDigest != "" &&
		payload.ExpectedCurrentImageDigest != policy.ExpectedCurrentImageDigest {
		return errors.New("execution intent expected current image digest mismatch")
	}
	if policy.ExpectedCurrentConfigChecksum != "" &&
		payload.ExpectedCurrentConfigChecksum != policy.ExpectedCurrentConfigChecksum {
		return errors.New("execution intent expected current config checksum mismatch")
	}
	if policy.ExpectedCurrentServiceConfigChecksum != "" &&
		payload.ExpectedCurrentServiceConfigChecksum != policy.ExpectedCurrentServiceConfigChecksum {
		return errors.New("execution intent expected current service config checksum mismatch")
	}
	if policy.ExpectedPlatform != "" && payload.ExpectedPlatform != policy.ExpectedPlatform {
		return errors.New("execution intent expected platform mismatch")
	}
	if policy.ExpectedCallerBinding != "" && payload.CallerBinding != policy.ExpectedCallerBinding {
		return errors.New("execution intent caller binding mismatch")
	}
	if policy.ExpectedAudience != "" && payload.Audience != policy.ExpectedAudience {
		return errors.New("execution intent audience mismatch")
	}
	return nil
}

func ValidateTrustPolicy(policy types.TrustPolicy) error {
	if len(policy.Keys) == 0 {
		return errors.New("trust policy has no public keys")
	}
	for keyID, publicKey := range policy.Keys {
		if len(publicKey) != ed25519.PublicKeySize {
			return fmt.Errorf("trust policy key %q is not Ed25519", keyID)
		}
		if PublicKeyFingerprint(publicKey) != keyID {
			return fmt.Errorf("trust policy key %q does not match its public-key fingerprint", keyID)
		}
	}
	return nil
}

func validateIntentAttempt(attempt types.ExecutionAttempt) error {
	if attempt.ID == uuid.Nil || attempt.OrganizationID == uuid.Nil ||
		attempt.DeploymentTargetID == uuid.Nil || attempt.TaskID == uuid.Nil ||
		attempt.StepRunID == uuid.Nil || attempt.Identity.ExecutionID == uuid.Nil ||
		attempt.Identity.AttemptNumber <= 0 ||
		strings.TrimSpace(attempt.Identity.StepKey) == "" {
		return errors.New("execution identity is invalid")
	}
	if !intentChecksumPattern.MatchString(attempt.PlanChecksum) {
		return errors.New("plan checksum is invalid")
	}
	if !intentChecksumPattern.MatchString(attempt.ArtifactDigest) {
		return errors.New("artifact digest is invalid")
	}
	if !intentChecksumPattern.MatchString(attempt.ConfigChecksum) {
		return errors.New("config checksum is invalid")
	}
	if strings.TrimSpace(attempt.AdapterRevision) == "" {
		return errors.New("adapter revision is required")
	}
	if (attempt.RuntimeContractVersion != types.ExecutionRuntimeContractVersionV3 &&
		attempt.RuntimeContractVersion != types.ExecutionRuntimeContractVersionV4) ||
		attempt.ExpectedObservedStateVersion <= 0 ||
		!intentChecksumPattern.MatchString(attempt.ExpectedObservedStateChecksum) ||
		!intentChecksumPattern.MatchString(attempt.ExpectedCurrentImageDigest) ||
		!intentChecksumPattern.MatchString(attempt.ExpectedCurrentConfigChecksum) ||
		!attempt.ExpectedPlatform.IsValid() {
		return errors.New("execution runtime trust preconditions are invalid")
	}
	if attempt.RuntimeContractVersion == types.ExecutionRuntimeContractVersionV3 {
		if attempt.RuntimeManifestChecksum != "" || attempt.DesiredServiceConfigChecksum != "" ||
			attempt.ExpectedCurrentServiceConfigChecksum != "" {
			return errors.New("execution v3 runtime checksum identities must be empty")
		}
	} else if !intentChecksumPattern.MatchString(attempt.RuntimeManifestChecksum) ||
		!intentChecksumPattern.MatchString(attempt.DesiredServiceConfigChecksum) ||
		!intentChecksumPattern.MatchString(attempt.ExpectedCurrentServiceConfigChecksum) {
		return errors.New("execution v4 runtime checksum identities are invalid")
	}
	if caller, audience := strings.TrimSpace(attempt.CallerBinding), strings.TrimSpace(attempt.Audience); caller == "" || len(caller) > 512 || strings.ContainsAny(caller, "\r\n") ||
		audience == "" || len(audience) > 512 || strings.ContainsAny(audience, "\r\n") {
		return errors.New("execution runtime caller or audience binding is invalid")
	}
	if strings.TrimSpace(attempt.Fence.ResourceKey) == "" || attempt.Fence.Generation <= 0 {
		return errors.New("execution fence is invalid")
	}
	if attempt.IntentIssuedAt.IsZero() || !attempt.IntentExpiresAt.After(attempt.IntentIssuedAt) ||
		attempt.IntentExpiresAt.Sub(attempt.IntentIssuedAt) > 15*time.Minute {
		return errors.New("execution intent validity interval is invalid")
	}
	return nil
}

func executionIntentSchema(version types.ExecutionRuntimeContractVersion) string {
	if version == types.ExecutionRuntimeContractVersionV4 {
		return executionIntentSchemaV4
	}
	return executionIntentSchemaV3
}

func validIntentSchemaContract(schema string, version types.ExecutionRuntimeContractVersion) bool {
	return (schema == executionIntentSchemaV3 && version == types.ExecutionRuntimeContractVersionV3) ||
		(schema == executionIntentSchemaV4 && version == types.ExecutionRuntimeContractVersionV4)
}
