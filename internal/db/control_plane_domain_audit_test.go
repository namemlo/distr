package db

import (
	"context"
	"errors"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

func TestDomainAuditAppendHookFailurePropagatesToOwningBoundary(t *testing.T) {
	expected := errors.New("audit append failed")
	ctx := WithControlPlaneDomainAuditHook(context.Background(), ControlPlaneAuditAppendHookFunc(func(
		context.Context,
		types.ControlPlaneAuditEventInput,
	) error {
		return expected
	}))

	input := releaseControlPlaneAuditInput(types.ReleaseBundle{
		ID: uuid.New(), OrganizationID: uuid.New(),
		Kind:              types.ReleaseBundleKindComponent,
		CanonicalChecksum: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "component_release.draft.created", nil, "SUCCEEDED")
	if err := recordReleaseControlPlaneAuditMutation(ctx, input); !errors.Is(err, expected) {
		t.Fatalf("recordReleaseControlPlaneAuditMutation() error = %v", err)
	}
}

func TestReleaseControlPlaneEventTypeUsesStableDomainPrefix(t *testing.T) {
	for _, test := range []struct {
		kind types.ReleaseBundleKind
		want string
	}{
		{types.ReleaseBundleKindComponent, "component_release.blocked"},
		{types.ReleaseBundleKindProduct, "product_release.blocked"},
		{"", "release.blocked"},
	} {
		if got := releaseControlPlaneEventType(types.ReleaseBundle{Kind: test.kind}, "blocked"); got != test.want {
			t.Fatalf("releaseControlPlaneEventType(%q) = %q, want %q", test.kind, got, test.want)
		}
	}
}

func TestReleaseControlPlaneAuditInputUsesKindSpecificChecksumCorrelation(t *testing.T) {
	organizationID := uuid.New()
	actorID := uuid.New()
	releaseID := uuid.New()
	checksum := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	component := releaseControlPlaneAuditInput(types.ReleaseBundle{
		ID: releaseID, OrganizationID: organizationID,
		Kind: types.ReleaseBundleKindComponent, CanonicalChecksum: checksum,
	}, "component_release.published", &actorID, "SUCCEEDED")
	if component.ReleaseID == nil || *component.ReleaseID != releaseID ||
		component.ComponentReleaseID == nil || *component.ComponentReleaseID != releaseID {
		t.Fatalf("component release correlations = %#v", component)
	}
	if component.ReleaseChecksum != checksum || component.ComponentReleaseChecksum != checksum {
		t.Fatalf("component release checksums = %#v", component)
	}
	if component.ProductReleaseID != nil || component.ProductReleaseChecksum != "" {
		t.Fatalf("component release leaked product correlation = %#v", component)
	}

	product := releaseControlPlaneAuditInput(types.ReleaseBundle{
		ID: releaseID, OrganizationID: organizationID,
		Kind: types.ReleaseBundleKindProduct, CanonicalChecksum: checksum,
	}, "product_release.validated", &actorID, "REJECTED")
	if product.ProductReleaseID == nil || *product.ProductReleaseID != releaseID ||
		product.ProductReleaseChecksum != checksum || product.ComponentReleaseID != nil {
		t.Fatalf("product release correlations = %#v", product)
	}
}

func TestProductReleaseComponentAuditInputConnectsFrozenChild(t *testing.T) {
	productReleaseID := uuid.New()
	componentReleaseID := uuid.New()
	organizationID := uuid.New()
	productChecksum := "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	componentChecksum := "sha256:2222222222222222222222222222222222222222222222222222222222222222"

	input := productReleaseComponentAuditInput(
		types.ReleaseBundle{
			ID: productReleaseID, OrganizationID: organizationID,
			Kind: types.ReleaseBundleKindProduct, CanonicalChecksum: productChecksum,
		},
		types.ProductReleaseComponent{
			ComponentReleaseID:       componentReleaseID,
			ComponentReleaseChecksum: componentChecksum,
		},
		"product_release.component.pinned",
	)
	if input.ProductReleaseID == nil || *input.ProductReleaseID != productReleaseID ||
		input.ProductReleaseChecksum != productChecksum || input.ComponentReleaseID == nil ||
		*input.ComponentReleaseID != componentReleaseID ||
		input.ComponentReleaseChecksum != componentChecksum {
		t.Fatalf("product component audit input = %#v", input)
	}
}

func TestTargetConfigControlPlaneAuditInputUsesSnapshotChecksumAndPlacement(t *testing.T) {
	organizationID := uuid.New()
	actorID := uuid.New()
	snapshotID := uuid.New()
	environmentID := uuid.New()
	deploymentUnitID := uuid.New()
	checksum := "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	input := targetConfigControlPlaneAuditInput(types.TargetConfigSnapshot{
		ID: snapshotID, OrganizationID: organizationID,
		CreatedByUserAccountID: actorID, EnvironmentID: environmentID,
		DeploymentUnitID: deploymentUnitID, CanonicalChecksum: checksum,
	}, "target_config.published")
	if input.TargetConfigID == nil || *input.TargetConfigID != snapshotID ||
		input.TargetConfigChecksum != checksum || input.ActorID == nil || *input.ActorID != actorID ||
		input.EnvironmentID == nil || *input.EnvironmentID != environmentID ||
		input.DeploymentUnitID == nil || *input.DeploymentUnitID != deploymentUnitID {
		t.Fatalf("target config audit input = %#v", input)
	}
}

func TestDeploymentPlanControlPlaneAuditInputUsesFrozenChecksumAndLineage(t *testing.T) {
	organizationID := uuid.New()
	actorID := uuid.New()
	planID := uuid.New()
	environmentID := uuid.New()
	deploymentUnitID := uuid.New()
	productReleaseID := uuid.New()
	checksum := "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"

	input := deploymentPlanControlPlaneAuditInput(types.DeploymentPlan{
		ID: planID, OrganizationID: organizationID,
		PublishedByUserAccountID: &actorID, EnvironmentID: environmentID,
		DeploymentUnitID: &deploymentUnitID, ReleaseBundleID: productReleaseID,
		ReleaseContract:   &types.ReleaseContract{ProductV1: &types.ProductReleaseManifest{}},
		CanonicalChecksum: checksum,
	}, "plan.published", &actorID)
	if input.DeploymentPlanID == nil || *input.DeploymentPlanID != planID ||
		input.DeploymentPlanChecksum != checksum || input.ProductReleaseID == nil ||
		*input.ProductReleaseID != productReleaseID || input.EnvironmentID == nil ||
		*input.EnvironmentID != environmentID || input.DeploymentUnitID == nil ||
		*input.DeploymentUnitID != deploymentUnitID {
		t.Fatalf("deployment plan audit input = %#v", input)
	}
}

func TestDeploymentPlanValidationAuditInputConnectsFrozenInputs(t *testing.T) {
	organizationID := uuid.New()
	actorID := uuid.New()
	productReleaseID := uuid.New()
	targetConfigID := uuid.New()
	environmentID := uuid.New()
	deploymentUnitID := uuid.New()
	productChecksum := "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	configChecksum := "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	previewChecksum := "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	validation := types.PlanDraftValidation{
		Draft: types.PlanDraft{
			OrganizationID: organizationID, UpdatedByUserAccountID: actorID,
			ProductReleaseID: productReleaseID, TargetConfigSnapshotID: targetConfigID,
			DeploymentUnitID: deploymentUnitID,
			ResolutionInput: &types.PlanResolutionInput{
				ProductChecksum: productChecksum,
				Assignment:      types.TargetEnvironmentAssignment{EnvironmentID: environmentID},
				Config:          types.TargetConfigBinding{CanonicalChecksum: configChecksum},
			},
		},
		PreviewChecksum: previewChecksum,
	}

	input := deploymentPlanValidationAuditInput(validation, "SUCCEEDED")
	if input.EventType != "plan.validated" || input.ProductReleaseID == nil ||
		*input.ProductReleaseID != productReleaseID || input.ProductReleaseChecksum != productChecksum ||
		input.TargetConfigID == nil || *input.TargetConfigID != targetConfigID ||
		input.TargetConfigChecksum != configChecksum || input.DeploymentPlanChecksum != previewChecksum ||
		input.EnvironmentID == nil || *input.EnvironmentID != environmentID {
		t.Fatalf("deployment plan validation audit input = %#v", input)
	}
}

func TestDeploymentPlanDraftAuditInputUsesResolvableSubjects(t *testing.T) {
	productReleaseID := uuid.New()
	targetConfigID := uuid.New()
	deploymentUnitID := uuid.New()
	actorID := uuid.New()
	draft := types.PlanDraft{
		OrganizationID: uuid.New(), UpdatedByUserAccountID: actorID,
		ProductReleaseID: productReleaseID, TargetConfigSnapshotID: targetConfigID,
		DeploymentUnitID: deploymentUnitID,
	}

	input := deploymentPlanDraftAuditInput(draft, "plan.draft.updated")
	if input.ActorID == nil || *input.ActorID != actorID || input.ProductReleaseID == nil ||
		*input.ProductReleaseID != productReleaseID || input.TargetConfigID == nil ||
		*input.TargetConfigID != targetConfigID || input.DeploymentUnitID == nil ||
		*input.DeploymentUnitID != deploymentUnitID {
		t.Fatalf("deployment plan draft audit input = %#v", input)
	}
}
