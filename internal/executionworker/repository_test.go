package executionworker

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/executionprotocol"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

type frozenSignerProviderStub struct {
	reference string
	version   string
	signer    executionprotocol.IntentSigner
}

func TestResolveFrozenPlanArtifactRequiresExactlyOnePlatformArtifact(t *testing.T) {
	g := NewWithT(t)
	releaseID := uuid.New()
	canonical := types.TargetDeploymentPlanCanonical{
		DeploymentTargetID: uuid.New(), EnvironmentID: uuid.New(),
		TargetPlatform: "linux/amd64", ProtocolVersion: types.DeploymentPlanProtocolV2,
		Graph: types.TargetPlanGraph{Steps: []types.TargetPlanStep{{
			StepKey: "deploy-api", ComponentKey: "transaction-api", ComponentReleaseID: &releaseID,
		}}},
		ComponentReleasePins: []types.ComponentReleasePin{{
			ComponentKey: "transaction-api", ComponentReleaseID: releaseID, Version: "2.4.1",
			Artifacts: []types.PinnedReleaseArtifact{{
				Key: "image", Platform: "linux/amd64",
				PlatformDigest: "sha256:" + strings.Repeat("a", 64),
			}},
		}},
	}
	payload, err := json.Marshal(canonical)
	g.Expect(err).NotTo(HaveOccurred())
	_, step, pin, artifact, err := resolveFrozenPlanArtifact(payload, "deploy-api")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(step.ComponentKey).To(Equal("transaction-api"))
	g.Expect(pin.ComponentReleaseID).To(Equal(releaseID))
	g.Expect(artifact.PlatformDigest).To(Equal("sha256:" + strings.Repeat("a", 64)))

	canonical.ComponentReleasePins[0].Artifacts = append(
		canonical.ComponentReleasePins[0].Artifacts,
		types.PinnedReleaseArtifact{
			Key: "second-image", Platform: "linux/amd64",
			PlatformDigest: "sha256:" + strings.Repeat("b", 64),
		},
	)
	payload, err = json.Marshal(canonical)
	g.Expect(err).NotTo(HaveOccurred())
	_, _, _, _, err = resolveFrozenPlanArtifact(payload, "deploy-api")
	g.Expect(err).To(MatchError(ContainSubstring("exactly one")))
}

func TestDeriveFrozenAttemptInputsBindsAdapterLineageAndExactTimeout(t *testing.T) {
	g := NewWithT(t)
	adapter := frozenAdapterEvidence{
		AssignmentID: uuid.New(), ImplementationID: uuid.New(),
		ImplementationVersion: "2.1.0", Capability: "distr.compose.deploy",
		CapabilityVersion: "2.0.0", ScopeType: "deployment_target",
		ScopeReference: uuid.NewString(), ConfigSnapshotID: uuid.New(),
		ConfigChecksum: "sha256:" + strings.Repeat("c", 64), KeyID: "choice-tp-dev",
		PublicKeyFingerprint:         "sha256:" + strings.Repeat("d", 64),
		SigningKeyReference:          "secret-provider://executor/choice-tp-dev",
		SigningKeyVersionFingerprint: "sha256:" + strings.Repeat("e", 64),
		CancelCapabilityVersion:      "2.0.0",
		RetrySafeCapabilityVersion:   "2.0.0",
		TimeoutSeconds:               420,
	}
	step := types.TargetPlanStep{
		StepKey: "deploy-api", TimeoutSeconds: 420, RetryClass: "safe",
		CancellationBehavior: "cooperative",
	}
	plan := types.DeploymentPlan{CanonicalChecksum: "sha256:" + strings.Repeat("f", 64)}
	inputs, err := deriveFrozenAttemptInputs(
		plan, step, "sha256:"+strings.Repeat("a", 64), adapter,
		"sha256:"+strings.Repeat("b", 64), nil, false,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(inputs.IntentTTL).To(Equal(420 * time.Second))
	g.Expect(inputs.AdapterRevision).To(MatchRegexp(`^sha256:[0-9a-f]{64}$`))
	g.Expect(inputs.ConfigChecksum).To(Equal(adapter.ConfigChecksum))
	g.Expect(inputs.PublicKeyFingerprint).To(Equal(adapter.PublicKeyFingerprint))
	g.Expect(inputs.SigningKeyReference).To(Equal(adapter.SigningKeyReference))
	g.Expect(inputs.SigningKeyVersionFingerprint).To(Equal(adapter.SigningKeyVersionFingerprint))
	g.Expect(inputs.Cancellable).To(BeTrue())
	g.Expect(inputs.RetrySafe).To(BeTrue())
	withoutControlFacts := adapter
	withoutControlFacts.CancelCapabilityVersion = ""
	withoutControlFacts.RetrySafeCapabilityVersion = ""
	withoutControlRevision, err := frozenAdapterRevision(withoutControlFacts)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(withoutControlRevision).NotTo(Equal(inputs.AdapterRevision))

	rotated := adapter
	rotated.SigningKeyVersionFingerprint = "sha256:" + strings.Repeat("1", 64)
	rotatedRevision, err := frozenAdapterRevision(rotated)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rotatedRevision).NotTo(Equal(inputs.AdapterRevision))

	adapter.TimeoutSeconds++
	_, err = deriveFrozenAttemptInputs(
		plan, step, inputs.ArtifactDigest, adapter, inputs.ResourceKey, nil, false,
	)
	g.Expect(err).To(MatchError(ContainSubstring("timeouts do not match")))
}

func TestDeriveFrozenAttemptInputsRequiresVersionedAdapterControlCapabilities(t *testing.T) {
	g := NewWithT(t)
	adapter := frozenAdapterEvidence{
		AssignmentID: uuid.New(), ImplementationID: uuid.New(),
		ImplementationVersion: "2.1.0", Capability: "distr.compose.deploy",
		CapabilityVersion: "2.0.0", ScopeType: "deployment_target",
		ScopeReference: uuid.NewString(), ConfigSnapshotID: uuid.New(),
		ConfigChecksum: "sha256:" + strings.Repeat("c", 64), KeyID: "choice-tp-dev",
		PublicKeyFingerprint:         "sha256:" + strings.Repeat("d", 64),
		SigningKeyReference:          "secret-provider://executor/choice-tp-dev",
		SigningKeyVersionFingerprint: "sha256:" + strings.Repeat("e", 64),
		TimeoutSeconds:               420,
	}
	step := types.TargetPlanStep{
		StepKey: "deploy-api", TimeoutSeconds: 420, RetryClass: "safe",
		CancellationBehavior: "cooperative",
	}
	plan := types.DeploymentPlan{CanonicalChecksum: "sha256:" + strings.Repeat("f", 64)}

	inputs, err := deriveFrozenAttemptInputs(
		plan, step, "sha256:"+strings.Repeat("a", 64), adapter,
		"sha256:"+strings.Repeat("b", 64), nil, false,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(inputs.Cancellable).To(BeFalse())
	g.Expect(inputs.RetrySafe).To(BeFalse())

	adapter.CancelCapabilityVersion = "1.9.0"
	adapter.RetrySafeCapabilityVersion = adapter.CapabilityVersion
	inputs, err = deriveFrozenAttemptInputs(
		plan, step, "sha256:"+strings.Repeat("a", 64), adapter,
		"sha256:"+strings.Repeat("b", 64), nil, false,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(inputs.Cancellable).To(BeFalse())
	g.Expect(inputs.RetrySafe).To(BeTrue())

	adapter.CancelCapabilityVersion = adapter.CapabilityVersion
	step.CancellationBehavior = "none"
	step.RetryClass = "unsafe"
	inputs, err = deriveFrozenAttemptInputs(
		plan, step, "sha256:"+strings.Repeat("a", 64), adapter,
		"sha256:"+strings.Repeat("b", 64), nil, false,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(inputs.Cancellable).To(BeFalse())
	g.Expect(inputs.RetrySafe).To(BeFalse())
}

func (p *frozenSignerProviderStub) ResolveIntentSigner(
	_ context.Context, reference, version string,
) (executionprotocol.IntentSigner, error) {
	p.reference, p.version = reference, version
	return p.signer, nil
}

func TestResolveFrozenIntentSignerRequiresExactAdapterKeyLineage(t *testing.T) {
	g := NewWithT(t)
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	g.Expect(err).NotTo(HaveOccurred())
	keyID := executionprotocol.PublicKeyFingerprint(publicKey)
	signer, err := executionprotocol.NewEd25519IntentSigner(keyID, privateKey)
	g.Expect(err).NotTo(HaveOccurred())
	provider := &frozenSignerProviderStub{signer: signer}
	inputs := FrozenAttemptInputs{
		SigningKeyReference:          "secret-provider://executor/choice-tp-dev",
		SigningKeyVersionFingerprint: "sha256:" + strings.Repeat("7", 64),
		PublicKeyFingerprint:         keyID,
	}
	resolved, err := resolveFrozenIntentSigner(context.Background(), provider, inputs)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(resolved.KeyID()).To(Equal(keyID))
	g.Expect(provider.reference).To(Equal(inputs.SigningKeyReference))
	g.Expect(provider.version).To(Equal(inputs.SigningKeyVersionFingerprint))

	inputs.PublicKeyFingerprint = "sha256:" + strings.Repeat("8", 64)
	_, err = resolveFrozenIntentSigner(context.Background(), provider, inputs)
	g.Expect(err).To(MatchError(ContainSubstring("public-key fingerprint")))
}

func TestDatabaseRuntimeRepositoryReadsExactGovernanceAndFrozenLineage(t *testing.T) {
	content, err := os.ReadFile("repository.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(content)
	for _, required := range []string{
		"ControlPlaneEnrollment", "AgentCapabilityReport", "AgentActionCapability",
		"ApprovalRequest", "AdmissionEvaluation", "DeploymentPlanStepAdapter",
		"AdapterAssignment", "AdapterImplementation", "AdapterCapability",
		"ComponentReleaseArtifact", "platform_digest", "public_key_fingerprint",
		"signing_key_reference", "signing_key_version_fingerprint", "timeout_seconds",
		"CanonicalExecutionFenceResourceKey", "distr.execution.cancel",
		"distr.execution.retry-safe",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("production runtime repository does not bind %q", required)
		}
	}
	for _, forbidden := range []string{
		"bundle.CanonicalChecksum", "RetryMaxAttempts >", "5 * time.Minute",
		"plan.Status == types.DeploymentPlanStatusExecuted",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("production runtime repository still contains placeholder trust %q", forbidden)
		}
	}
}
