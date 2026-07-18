package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestRegistryImportPreviewRequestToDomainNormalizesFullPlacement(t *testing.T) {
	g := NewWithT(t)
	organizationID, actorID := uuid.New(), uuid.New()
	firstSubscriber, secondSubscriber := uuid.New(), uuid.New()
	checksum := strings.Repeat("a", 64)
	request := RegistryImportPreviewRequest{
		SourceKind: " compose ", ToolName: " scanner ", ToolVersion: " 1.0 ",
		Parameters:        map[string]string{" format ": " compose "},
		EvidenceReference: " evidence://sha256/" + checksum + " ",
		EvidenceChecksum:  " " + checksum + " ",
		SourcePlacements: []RegistryImportSourcePlacement{{
			RootKey: " Choice-TP-DEV ", PhysicalName: " api ",
		}},
		Roots: []RegistryImportCandidateRoot{{
			Key: " Choice-TP-DEV ", Name: " Choice TP DEV ",
			DeliveryModel: types.DeliveryModelShared, Classification: types.ImportClassificationShared,
			DeploymentTargetID: uuid.New(), EnvironmentID: uuid.New(),
			SubscriberCustomerOrganizationIDs: []uuid.UUID{
				secondSubscriber, firstSubscriber, secondSubscriber,
			},
			PhysicalIdentity: " compose:choice-tp-dev ",
			Placements: []RegistryImportCandidatePlacement{{
				ComponentKey: " API ", PhysicalName: " api ",
				ConfigNamespace: " config ", DatabaseBoundary: " database ",
				HealthAdapter: " health ", RenamedFrom: " old-api ",
			}},
		}},
	}

	domain := request.ToDomain(organizationID, actorID)

	g.Expect(domain.OrganizationID).To(Equal(organizationID))
	g.Expect(domain.ActorID).To(Equal(actorID))
	g.Expect(domain.SourceKind).To(Equal("compose"))
	g.Expect(domain.Parameters).To(Equal(map[string]string{"format": "compose"}))
	g.Expect(domain.SourcePlacements).To(Equal([]types.RegistryImportSourcePlacement{{
		RootKey: "choice-tp-dev", PhysicalName: "api",
	}}))
	g.Expect(domain.Roots).To(HaveLen(1))
	g.Expect(domain.Roots[0].Key).To(Equal("choice-tp-dev"))
	g.Expect(domain.Roots[0].SubscriberCustomerOrganizationIDs).To(HaveLen(2))
	g.Expect(domain.Roots[0].Placements).To(Equal([]types.RegistryImportCandidatePlacement{{
		ComponentKey: "api", PhysicalName: "api", ConfigNamespace: "config",
		DatabaseBoundary: "database", HealthAdapter: "health", RenamedFrom: "old-api",
	}}))
	g.Expect(request.Validate(context.Background())).To(Succeed())
}

func TestDeploymentRegistryListRequestValidate(t *testing.T) {
	tests := []struct {
		name    string
		request DeploymentRegistryListRequest
		wantErr bool
	}{
		{name: "accepts zero as internal default", request: DeploymentRegistryListRequest{Limit: 0}},
		{name: "accepts public minimum", request: DeploymentRegistryListRequest{Limit: 1}},
		{name: "accepts maximum", request: DeploymentRegistryListRequest{Limit: 100, Cursor: "eyJ2IjoxfQ"}},
		{name: "rejects negative", request: DeploymentRegistryListRequest{Limit: -1}, wantErr: true},
		{name: "rejects over maximum", request: DeploymentRegistryListRequest{Limit: 101}, wantErr: true},
		{name: "rejects malformed cursor", request: DeploymentRegistryListRequest{Cursor: "not a cursor!"}, wantErr: true},
		{
			name:    "rejects oversized cursor",
			request: DeploymentRegistryListRequest{Cursor: strings.Repeat("a", 2049)},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.request.Validate()
			if tt.wantErr && err == nil {
				t.Fatal("expected validation error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}

func TestCreateDeploymentScopeRequestValidate(t *testing.T) {
	customerID := uuid.New()
	valid := CreateDeploymentScopeRequest{
		CustomerOrganizationID: &customerID,
		Key:                    "customer.production",
		Name:                   "Customer production",
		DeliveryModel:          types.DeliveryModelDedicated,
		ManagementState:        types.RegistryManagementStateManaged,
	}

	tests := []struct {
		name    string
		mutate  func(*CreateDeploymentScopeRequest)
		wantErr bool
	}{
		{name: "accepts dedicated scope"},
		{
			name:    "rejects non-canonical key",
			mutate:  func(r *CreateDeploymentScopeRequest) { r.Key = "Customer Production" },
			wantErr: true,
		},
		{name: "rejects blank name", mutate: func(r *CreateDeploymentScopeRequest) { r.Name = " " }, wantErr: true},
		{
			name:    "rejects unknown delivery model",
			mutate:  func(r *CreateDeploymentScopeRequest) { r.DeliveryModel = "per_tenant" },
			wantErr: true,
		},
		{
			name:    "requires customer for dedicated",
			mutate:  func(r *CreateDeploymentScopeRequest) { r.CustomerOrganizationID = nil },
			wantErr: true,
		},
		{
			name:    "rejects customer for shared",
			mutate:  func(r *CreateDeploymentScopeRequest) { r.DeliveryModel = types.DeliveryModelShared },
			wantErr: true,
		},
		{
			name: "requires matching retirement",
			mutate: func(r *CreateDeploymentScopeRequest) {
				r.ManagementState = types.RegistryManagementStateRetired
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := valid
			if tt.mutate != nil {
				tt.mutate(&request)
			}
			err := request.Validate()
			if tt.wantErr {
				NewWithT(t).Expect(err).To(HaveOccurred())
			} else {
				NewWithT(t).Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}

func TestDeploymentRegistryWriteRequestsValidate(t *testing.T) {
	now := time.Now().UTC()
	later := now.Add(time.Hour)
	validChecksum := "sha256:" + strings.Repeat("a", 64)
	customerID := uuid.New()

	tests := []struct {
		name    string
		request interface{ Validate() error }
		wantErr bool
	}{
		{
			name: "accepts assignment",
			request: CreateTargetEnvironmentAssignmentRequest{
				DeploymentTargetID: uuid.New(),
				EnvironmentID:      uuid.New(),
				ActiveFrom:         now,
				ActiveUntil:        &later,
				PolicyConstraints:  json.RawMessage(`{"region":"eu"}`),
			},
		},
		{
			name: "rejects assignment interval",
			request: CreateTargetEnvironmentAssignmentRequest{
				DeploymentTargetID: uuid.New(),
				EnvironmentID:      uuid.New(),
				ActiveFrom:         later,
				ActiveUntil:        &now,
			},
			wantErr: true,
		},
		{
			name: "accepts unit",
			request: CreateDeploymentUnitRequest{
				DeploymentScopeID:             uuid.New(),
				TargetEnvironmentAssignmentID: uuid.New(),
				DeploymentTargetID:            uuid.New(),
				Key:                           "cluster-1",
				Name:                          "Cluster 1",
				PhysicalIdentity:              "cluster-1.internal",
				ManagementState:               types.RegistryManagementStateManaged,
				SubscriberSetChecksum:         validChecksum,
			},
		},
		{
			name: "rejects invalid checksum",
			request: CreateDeploymentUnitRequest{
				DeploymentScopeID:             uuid.New(),
				TargetEnvironmentAssignmentID: uuid.New(),
				DeploymentTargetID:            uuid.New(),
				Key:                           "cluster-1",
				Name:                          "Cluster 1",
				PhysicalIdentity:              "cluster-1.internal",
				ManagementState:               types.RegistryManagementStateManaged,
				SubscriberSetChecksum:         "sha256:nope",
			},
			wantErr: true,
		},
		{
			name: "rejects duplicate subscriber IDs",
			request: CreateDeploymentUnitRequest{
				DeploymentScopeID:                 uuid.New(),
				TargetEnvironmentAssignmentID:     uuid.New(),
				DeploymentTargetID:                uuid.New(),
				Key:                               "cluster-1",
				Name:                              "Cluster 1",
				PhysicalIdentity:                  "cluster-1.internal",
				ManagementState:                   types.RegistryManagementStateManaged,
				SubscriberSetChecksum:             validChecksum,
				SubscriberCustomerOrganizationIDs: []uuid.UUID{customerID, customerID},
			},
			wantErr: true,
		},
		{
			name: "accepts subscriber",
			request: CreateDeploymentUnitSubscriberRequest{
				DeploymentUnitID:       uuid.New(),
				CustomerOrganizationID: uuid.New(),
			},
		},
		{
			name: "accepts definition",
			request: CreateComponentDefinitionRequest{
				Key:             "billing-api",
				Name:            "Billing API",
				CapabilityScope: "billing",
				ManagementState: types.RegistryManagementStateObserveOnly,
			},
		},
		{
			name: "accepts alias",
			request: CreateComponentAliasRequest{
				ComponentDefinitionID: uuid.New(),
				Alias:                 "billing-api-v1",
			},
		},
		{
			name: "accepts instance",
			request: CreateComponentInstanceRequest{
				DeploymentUnitID:      uuid.New(),
				ComponentDefinitionID: uuid.New(),
				PhysicalName:          "billing-api",
				ManagementState:       types.RegistryManagementStateManaged,
			},
		},
		{
			name: "rejects instance without definition",
			request: CreateComponentInstanceRequest{
				DeploymentUnitID: uuid.New(),
				PhysicalName:     "billing-api",
				ManagementState:  types.RegistryManagementStateManaged,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.request.Validate()
			if tt.wantErr {
				NewWithT(t).Expect(err).To(HaveOccurred())
			} else {
				NewWithT(t).Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}
