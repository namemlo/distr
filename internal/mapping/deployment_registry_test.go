package mapping

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestDeploymentRegistryPlacementToAPI(t *testing.T) {
	g := NewWithT(t)
	organizationID := uuid.New()
	customerID := uuid.New()
	scopeID := uuid.New()
	assignmentID := uuid.New()
	unitID := uuid.New()
	definitionID := uuid.New()
	now := time.Now().UTC()

	placement := types.DeploymentRegistryPlacement{
		Scope: types.DeploymentScope{
			ID:                     scopeID,
			CreatedAt:              now,
			UpdatedAt:              now,
			OrganizationID:         organizationID,
			CustomerOrganizationID: &customerID,
			Key:                    "customer.production",
			Name:                   "Customer production",
			DeliveryModel:          types.DeliveryModelDedicated,
			ManagementState:        types.RegistryManagementStateManaged,
		},
		Assignment: types.TargetEnvironmentAssignment{
			ID:                 assignmentID,
			CreatedAt:          now,
			UpdatedAt:          now,
			OrganizationID:     organizationID,
			DeploymentTargetID: uuid.New(),
			EnvironmentID:      uuid.New(),
			ActiveFrom:         now,
			PolicyConstraints:  json.RawMessage(`{"region":"eu"}`),
		},
		Unit: types.DeploymentUnit{
			ID:                            unitID,
			CreatedAt:                     now,
			UpdatedAt:                     now,
			OrganizationID:                organizationID,
			DeploymentScopeID:             scopeID,
			TargetEnvironmentAssignmentID: assignmentID,
			DeploymentTargetID:            uuid.New(),
			Key:                           "cluster-1",
			Name:                          "Cluster 1",
			PhysicalIdentity:              "cluster-1.internal",
			ManagementState:               types.RegistryManagementStateManaged,
			SubscriberSetChecksum:         "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		Subscribers: []types.DeploymentUnitSubscriber{
			{
				ID:                     uuid.New(),
				CreatedAt:              now,
				OrganizationID:         organizationID,
				DeploymentUnitID:       unitID,
				CustomerOrganizationID: customerID,
			},
		},
		Definitions: []types.ComponentDefinition{
			{
				ID:              definitionID,
				CreatedAt:       now,
				UpdatedAt:       now,
				OrganizationID:  organizationID,
				Key:             "billing-api",
				Name:            "Billing API",
				ManagementState: types.RegistryManagementStateObserveOnly,
			},
		},
		Aliases: []types.ComponentAlias{
			{
				ID:                    uuid.New(),
				CreatedAt:             now,
				OrganizationID:        organizationID,
				ComponentDefinitionID: definitionID,
				Alias:                 "billing-api-v1",
			},
		},
		Instances: []types.ComponentInstance{
			{
				ID:                    uuid.New(),
				CreatedAt:             now,
				UpdatedAt:             now,
				OrganizationID:        organizationID,
				DeploymentUnitID:      unitID,
				ComponentDefinitionID: definitionID,
				PhysicalName:          "billing-api",
				ManagementState:       types.RegistryManagementStateManaged,
			},
		},
	}

	response := DeploymentRegistryPlacementToAPI(placement)

	g.Expect(response.Scope.ID).To(Equal(scopeID))
	g.Expect(response.Scope.CustomerOrganizationID).To(Equal(&customerID))
	g.Expect(response.Assignment.PolicyConstraints).To(MatchJSON(`{"region":"eu"}`))
	g.Expect(response.Unit.ID).To(Equal(unitID))
	g.Expect(response.Subscribers).To(HaveLen(1))
	g.Expect(response.Definitions).To(HaveLen(1))
	g.Expect(response.Aliases).To(HaveLen(1))
	g.Expect(response.Instances).To(HaveLen(1))

	payload, err := json.Marshal(response)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(payload)).NotTo(ContainSubstring(organizationID.String()))
}

func TestDeploymentRegistryPagesToAPI(t *testing.T) {
	g := NewWithT(t)
	scope := types.DeploymentScope{
		ID:              uuid.New(),
		OrganizationID:  uuid.New(),
		Key:             "shared",
		Name:            "Shared",
		DeliveryModel:   types.DeliveryModelShared,
		ManagementState: types.RegistryManagementStateManaged,
	}

	response := DeploymentScopePageToAPI(types.Page[types.DeploymentScope]{
		Items:      []types.DeploymentScope{scope},
		NextCursor: "opaque",
	})

	g.Expect(response.Items).To(HaveLen(1))
	g.Expect(response.Items[0].ID).To(Equal(scope.ID))
	g.Expect(response.NextCursor).To(Equal("opaque"))
}
