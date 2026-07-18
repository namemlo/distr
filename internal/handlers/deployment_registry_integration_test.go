package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/auth"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/deploymentregistry"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
)

func TestDeploymentRegistryHandlersAdminReadPaginationIsolationAndProtectedDelete(t *testing.T) {
	ctx := channelHandlerDBTestContext(t)
	g := NewWithT(t)
	first := createDeploymentRegistryHandlerDependencies(t, ctx)
	second := createDeploymentRegistryHandlerDependencies(t, ctx)

	createRecorder := httptest.NewRecorder()
	createRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/deployment-registry/scopes",
		strings.NewReader(`{
			"customerOrganizationId":"`+first.customerOrganizationID.String()+`",
			"key":"primary",
			"name":"Primary",
			"deliveryModel":"dedicated",
			"managementState":"managed"
		}`),
	)
	createRequest = createRequest.WithContext(
		deploymentRegistryHandlerContext(ctx, first.organizationID, types.UserRoleAdmin),
	)

	createDeploymentScopeHandler().ServeHTTP(createRecorder, createRequest)

	g.Expect(createRecorder.Code).To(Equal(http.StatusOK))
	var created api.DeploymentScope
	g.Expect(json.Unmarshal(createRecorder.Body.Bytes(), &created)).To(Succeed())
	g.Expect(created.ID).NotTo(Equal(uuid.Nil))

	for _, key := range []string{"secondary", "tertiary"} {
		scope := types.DeploymentScope{
			OrganizationID:         first.organizationID,
			CustomerOrganizationID: &first.customerOrganizationID,
			Key:                    key,
			Name:                   key,
			DeliveryModel:          types.DeliveryModelDedicated,
			ManagementState:        types.RegistryManagementStateManaged,
		}
		g.Expect(db.CreateDeploymentScope(ctx, &scope)).To(Succeed())
	}

	listRecorder := httptest.NewRecorder()
	listRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/deployment-registry/scopes?limit=2",
		nil,
	)
	listRequest = listRequest.WithContext(
		deploymentRegistryHandlerContext(ctx, first.organizationID, types.UserRoleReadOnly),
	)

	getDeploymentScopesHandler().ServeHTTP(listRecorder, listRequest)

	g.Expect(listRecorder.Code).To(Equal(http.StatusOK))
	var page api.DeploymentScopePage
	g.Expect(json.Unmarshal(listRecorder.Body.Bytes(), &page)).To(Succeed())
	g.Expect(page.Items).To(HaveLen(2))
	g.Expect(page.NextCursor).NotTo(BeEmpty())

	boundedRecorder := httptest.NewRecorder()
	boundedRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/deployment-registry/scopes?limit=101",
		nil,
	)
	boundedRequest = boundedRequest.WithContext(
		deploymentRegistryHandlerContext(ctx, first.organizationID, types.UserRoleReadOnly),
	)
	getDeploymentScopesHandler().ServeHTTP(boundedRecorder, boundedRequest)
	g.Expect(boundedRecorder.Code).To(Equal(http.StatusBadRequest))

	foreignRecorder := httptest.NewRecorder()
	foreignRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/deployment-registry/scopes/"+created.ID.String(),
		nil,
	)
	foreignRequest.SetPathValue("scopeId", created.ID.String())
	foreignRequest = foreignRequest.WithContext(
		deploymentRegistryHandlerContext(ctx, second.organizationID, types.UserRoleReadOnly),
	)

	getDeploymentScopeHandler().ServeHTTP(foreignRecorder, foreignRequest)

	g.Expect(foreignRecorder.Code).To(Equal(http.StatusNotFound))

	assignment := types.TargetEnvironmentAssignment{
		OrganizationID:     first.organizationID,
		DeploymentTargetID: first.deploymentTargetID,
		EnvironmentID:      first.environmentID,
		ActiveFrom:         time.Now().UTC(),
	}
	g.Expect(db.CreateTargetEnvironmentAssignment(ctx, &assignment)).To(Succeed())
	unit := types.DeploymentUnit{
		OrganizationID:                first.organizationID,
		DeploymentScopeID:             created.ID,
		TargetEnvironmentAssignmentID: assignment.ID,
		DeploymentTargetID:            first.deploymentTargetID,
		Key:                           "primary-unit",
		Name:                          "Primary unit",
		PhysicalIdentity:              "compose:primary",
		ManagementState:               types.RegistryManagementStateManaged,
		SubscriberSetChecksum:         deploymentregistry.SubscriberSetChecksum(nil),
	}
	g.Expect(db.CreateDeploymentUnit(ctx, &unit)).To(Succeed())

	deleteRecorder := httptest.NewRecorder()
	deleteRequest := httptest.NewRequest(
		http.MethodDelete,
		"/api/v1/deployment-registry/scopes/"+created.ID.String(),
		nil,
	)
	deleteRequest.SetPathValue("scopeId", created.ID.String())
	deleteRequest = deleteRequest.WithContext(
		deploymentRegistryHandlerContext(ctx, first.organizationID, types.UserRoleAdmin),
	)

	deleteDeploymentScopeHandler().ServeHTTP(deleteRecorder, deleteRequest)

	g.Expect(deleteRecorder.Code).To(Equal(http.StatusConflict))
	g.Expect(deleteRecorder.Header().Get("Content-Type")).To(Equal("text/plain; charset=utf-8"))
	g.Expect(deleteRecorder.Body.String()).To(Equal("deployment scope is in use\n"))
	g.Expect(strings.ToLower(deleteRecorder.Body.String())).NotTo(Or(
		ContainSubstring("sqlstate"),
		ContainSubstring("constraint"),
		ContainSubstring("foreign key"),
	))
	_, err := db.GetDeploymentScope(ctx, first.organizationID, created.ID)
	g.Expect(err).NotTo(HaveOccurred())
}

func TestDeploymentRegistryCreateUnitHandlerAtomicallyInitializesSharedSubscribers(t *testing.T) {
	ctx := channelHandlerDBTestContext(t)
	g := NewWithT(t)
	deps := createDeploymentRegistryHandlerDependencies(t, ctx)
	secondCustomerID := createDeploymentRegistryHandlerCustomer(t, ctx, deps.organizationID)

	scope := types.DeploymentScope{
		OrganizationID:  deps.organizationID,
		Key:             "shared-handler",
		Name:            "Shared handler",
		DeliveryModel:   types.DeliveryModelShared,
		ManagementState: types.RegistryManagementStateManaged,
	}
	g.Expect(db.CreateDeploymentScope(ctx, &scope)).To(Succeed())
	assignment := types.TargetEnvironmentAssignment{
		OrganizationID:     deps.organizationID,
		DeploymentTargetID: deps.deploymentTargetID,
		EnvironmentID:      deps.environmentID,
		ActiveFrom:         time.Now().UTC(),
	}
	g.Expect(db.CreateTargetEnvironmentAssignment(ctx, &assignment)).To(Succeed())
	subscribers := []types.DeploymentUnitSubscriber{
		{
			OrganizationID:         deps.organizationID,
			CustomerOrganizationID: deps.customerOrganizationID,
		},
		{
			OrganizationID:         deps.organizationID,
			CustomerOrganizationID: secondCustomerID,
		},
	}
	payload, err := json.Marshal(api.CreateDeploymentUnitRequest{
		DeploymentScopeID:             scope.ID,
		TargetEnvironmentAssignmentID: assignment.ID,
		DeploymentTargetID:            deps.deploymentTargetID,
		Key:                           "shared-handler-unit",
		Name:                          "Shared handler unit",
		PhysicalIdentity:              "compose:shared-handler",
		ManagementState:               types.RegistryManagementStateManaged,
		SubscriberSetChecksum:         deploymentregistry.SubscriberSetChecksum(subscribers),
		SubscriberCustomerOrganizationIDs: []uuid.UUID{
			deps.customerOrganizationID,
			secondCustomerID,
		},
	})
	g.Expect(err).NotTo(HaveOccurred())

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/deployment-registry/units",
		strings.NewReader(string(payload)),
	)
	request = request.WithContext(
		deploymentRegistryHandlerContext(ctx, deps.organizationID, types.UserRoleAdmin),
	)

	createDeploymentUnitHandler().ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusOK), recorder.Body.String())
	var created api.DeploymentUnit
	g.Expect(json.Unmarshal(recorder.Body.Bytes(), &created)).To(Succeed())
	g.Expect(created.ID).NotTo(Equal(uuid.Nil))
	page, err := db.ListDeploymentUnitSubscribers(ctx, types.RegistryListFilter{
		OrganizationID: deps.organizationID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(page.Items).To(HaveLen(2))
	g.Expect(page.Items).To(ConsistOf(
		HaveField("CustomerOrganizationID", deps.customerOrganizationID),
		HaveField("CustomerOrganizationID", secondCustomerID),
	))
}

func TestDeploymentRegistryRoutedResourceFamiliesAuthorizationAndCompatibility(t *testing.T) {
	ctx := channelHandlerDBTestContext(t)
	g := NewWithT(t)
	first := createDeploymentRegistryHandlerDependencies(t, ctx)
	second := createDeploymentRegistryHandlerDependencies(t, ctx)
	secondCustomerID := createDeploymentRegistryHandlerCustomer(t, ctx, first.organizationID)
	thirdCustomerID := createDeploymentRegistryHandlerCustomer(t, ctx, first.organizationID)
	router := deploymentRegistryRoutedTestHandler(
		[]featureflags.Key{featureflags.KeyOperatorControlPlaneV2},
	)
	const root = "/api/v1/deployment-registry"

	mainScope := decodeDeploymentRegistryRouteResponse[api.DeploymentScope](
		t,
		deploymentRegistryRouteRequest(
			t,
			router,
			ctx,
			first.organizationID,
			types.UserRoleAdmin,
			http.MethodPost,
			root+"/scopes/",
			api.CreateDeploymentScopeRequest{
				CustomerOrganizationID: &first.customerOrganizationID,
				Key:                    "routed-main",
				Name:                   "Routed main",
				DeliveryModel:          types.DeliveryModelDedicated,
				ManagementState:        types.RegistryManagementStateManaged,
			},
			http.StatusOK,
		),
	)
	flagOffScope := decodeDeploymentRegistryRouteResponse[api.DeploymentScope](
		t,
		deploymentRegistryRouteRequest(
			t,
			deploymentRegistryRoutedTestHandler(nil),
			ctx,
			first.organizationID,
			types.UserRoleReadOnly,
			http.MethodGet,
			root+"/scopes/"+mainScope.ID.String()+"/",
			nil,
			http.StatusOK,
		),
	)
	g.Expect(flagOffScope.ID).To(Equal(mainScope.ID))
	disposableScope := decodeDeploymentRegistryRouteResponse[api.DeploymentScope](
		t,
		deploymentRegistryRouteRequest(
			t,
			router,
			ctx,
			first.organizationID,
			types.UserRoleAdmin,
			http.MethodPost,
			root+"/scopes/",
			api.CreateDeploymentScopeRequest{
				CustomerOrganizationID: &first.customerOrganizationID,
				Key:                    "routed-disposable",
				Name:                   "Routed disposable",
				DeliveryModel:          types.DeliveryModelDedicated,
				ManagementState:        types.RegistryManagementStateManaged,
			},
			http.StatusOK,
		),
	)
	sharedScope := decodeDeploymentRegistryRouteResponse[api.DeploymentScope](
		t,
		deploymentRegistryRouteRequest(
			t,
			router,
			ctx,
			first.organizationID,
			types.UserRoleAdmin,
			http.MethodPost,
			root+"/scopes/",
			api.CreateDeploymentScopeRequest{
				Key:             "routed-shared",
				Name:            "Routed shared",
				DeliveryModel:   types.DeliveryModelShared,
				ManagementState: types.RegistryManagementStateManaged,
			},
			http.StatusOK,
		),
	)

	scopePageOne := decodeDeploymentRegistryRouteResponse[api.DeploymentScopePage](
		t,
		deploymentRegistryRouteRequest(
			t,
			router,
			ctx,
			first.organizationID,
			types.UserRoleReadOnly,
			http.MethodGet,
			root+"/scopes/?limit=1",
			nil,
			http.StatusOK,
		),
	)
	g.Expect(scopePageOne.Items).To(HaveLen(1))
	g.Expect(scopePageOne.NextCursor).NotTo(BeEmpty())
	scopePageTwo := decodeDeploymentRegistryRouteResponse[api.DeploymentScopePage](
		t,
		deploymentRegistryRouteRequest(
			t,
			router,
			ctx,
			first.organizationID,
			types.UserRoleReadOnly,
			http.MethodGet,
			root+"/scopes/?limit=1&cursor="+scopePageOne.NextCursor,
			nil,
			http.StatusOK,
		),
	)
	g.Expect(scopePageTwo.Items).To(HaveLen(1))
	g.Expect(scopePageTwo.Items[0].ID).NotTo(Equal(scopePageOne.Items[0].ID))
	deploymentRegistryRouteRequest(
		t,
		router,
		ctx,
		first.organizationID,
		types.UserRoleReadOnly,
		http.MethodGet,
		root+"/scopes/?limit=101",
		nil,
		http.StatusBadRequest,
	)
	deploymentRegistryRouteRequest(
		t,
		router,
		ctx,
		first.organizationID,
		types.UserRoleReadOnly,
		http.MethodGet,
		root+"/scopes/"+mainScope.ID.String()+"/",
		nil,
		http.StatusOK,
	)
	deploymentRegistryRouteRequest(
		t,
		router,
		ctx,
		first.organizationID,
		types.UserRoleAdmin,
		http.MethodPut,
		root+"/scopes/"+mainScope.ID.String()+"/",
		api.UpdateDeploymentScopeRequest{
			Name:            "Routed main updated",
			Description:     "updated through routed handler",
			ManagementState: types.RegistryManagementStateManaged,
		},
		http.StatusOK,
	)
	deploymentRegistryRouteRequest(
		t,
		router,
		ctx,
		first.organizationID,
		types.UserRoleAdmin,
		http.MethodDelete,
		root+"/scopes/"+disposableScope.ID.String()+"/",
		nil,
		http.StatusNoContent,
	)

	activeFrom := time.Now().UTC().Add(-time.Minute)
	mainAssignment := decodeDeploymentRegistryRouteResponse[api.TargetEnvironmentAssignment](
		t,
		deploymentRegistryRouteRequest(
			t,
			router,
			ctx,
			first.organizationID,
			types.UserRoleAdmin,
			http.MethodPost,
			root+"/assignments/",
			api.CreateTargetEnvironmentAssignmentRequest{
				DeploymentTargetID: first.deploymentTargetID,
				EnvironmentID:      first.environmentID,
				ActiveFrom:         activeFrom,
				PolicyConstraints:  json.RawMessage(`{"region":"primary"}`),
			},
			http.StatusOK,
		),
	)
	disposableTargetID := createDeploymentRegistryHandlerTarget(t, ctx, first.organizationID)
	disposableAssignment := decodeDeploymentRegistryRouteResponse[api.TargetEnvironmentAssignment](
		t,
		deploymentRegistryRouteRequest(
			t,
			router,
			ctx,
			first.organizationID,
			types.UserRoleAdmin,
			http.MethodPost,
			root+"/assignments/",
			api.CreateTargetEnvironmentAssignmentRequest{
				DeploymentTargetID: disposableTargetID,
				EnvironmentID:      first.environmentID,
				ActiveFrom:         activeFrom,
			},
			http.StatusOK,
		),
	)
	deploymentRegistryRouteRequest(
		t,
		router,
		ctx,
		first.organizationID,
		types.UserRoleAdmin,
		http.MethodPut,
		root+"/assignments/"+mainAssignment.ID.String()+"/",
		api.UpdateTargetEnvironmentAssignmentRequest{
			PolicyConstraints: json.RawMessage(`{"region":"updated"}`),
		},
		http.StatusOK,
	)
	deploymentRegistryRouteRequest(
		t,
		router,
		ctx,
		first.organizationID,
		types.UserRoleAdmin,
		http.MethodDelete,
		root+"/assignments/"+disposableAssignment.ID.String()+"/",
		nil,
		http.StatusNoContent,
	)

	emptySubscriberChecksum := deploymentregistry.SubscriberSetChecksum(nil)
	mainUnit := decodeDeploymentRegistryRouteResponse[api.DeploymentUnit](
		t,
		deploymentRegistryRouteRequest(
			t,
			router,
			ctx,
			first.organizationID,
			types.UserRoleAdmin,
			http.MethodPost,
			root+"/units/",
			api.CreateDeploymentUnitRequest{
				DeploymentScopeID:             mainScope.ID,
				TargetEnvironmentAssignmentID: mainAssignment.ID,
				DeploymentTargetID:            first.deploymentTargetID,
				Key:                           "routed-main-unit",
				Name:                          "Routed main unit",
				PhysicalIdentity:              "compose:routed-main",
				ManagementState:               types.RegistryManagementStateManaged,
				SubscriberSetChecksum:         emptySubscriberChecksum,
			},
			http.StatusOK,
		),
	)
	disposableUnit := decodeDeploymentRegistryRouteResponse[api.DeploymentUnit](
		t,
		deploymentRegistryRouteRequest(
			t,
			router,
			ctx,
			first.organizationID,
			types.UserRoleAdmin,
			http.MethodPost,
			root+"/units/",
			api.CreateDeploymentUnitRequest{
				DeploymentScopeID:             mainScope.ID,
				TargetEnvironmentAssignmentID: mainAssignment.ID,
				DeploymentTargetID:            first.deploymentTargetID,
				Key:                           "routed-disposable-unit",
				Name:                          "Routed disposable unit",
				PhysicalIdentity:              "compose:routed-disposable",
				ManagementState:               types.RegistryManagementStateManaged,
				SubscriberSetChecksum:         emptySubscriberChecksum,
			},
			http.StatusOK,
		),
	)
	sharedSubscribers := []types.DeploymentUnitSubscriber{
		{
			OrganizationID:         first.organizationID,
			CustomerOrganizationID: first.customerOrganizationID,
		},
		{
			OrganizationID:         first.organizationID,
			CustomerOrganizationID: secondCustomerID,
		},
	}
	sharedUnit := decodeDeploymentRegistryRouteResponse[api.DeploymentUnit](
		t,
		deploymentRegistryRouteRequest(
			t,
			router,
			ctx,
			first.organizationID,
			types.UserRoleAdmin,
			http.MethodPost,
			root+"/units/",
			api.CreateDeploymentUnitRequest{
				DeploymentScopeID:             sharedScope.ID,
				TargetEnvironmentAssignmentID: mainAssignment.ID,
				DeploymentTargetID:            first.deploymentTargetID,
				Key:                           "routed-shared-unit",
				Name:                          "Routed shared unit",
				PhysicalIdentity:              "compose:routed-shared",
				ManagementState:               types.RegistryManagementStateManaged,
				SubscriberSetChecksum: deploymentregistry.SubscriberSetChecksum(
					sharedSubscribers,
				),
				SubscriberCustomerOrganizationIDs: []uuid.UUID{
					first.customerOrganizationID,
					secondCustomerID,
				},
			},
			http.StatusOK,
		),
	)
	deploymentRegistryRouteRequest(
		t,
		router,
		ctx,
		first.organizationID,
		types.UserRoleAdmin,
		http.MethodPut,
		root+"/units/"+mainUnit.ID.String()+"/",
		api.UpdateDeploymentUnitRequest{
			Name:            "Routed main unit updated",
			ManagementState: types.RegistryManagementStateManaged,
		},
		http.StatusOK,
	)
	deploymentRegistryRouteRequest(
		t,
		router,
		ctx,
		first.organizationID,
		types.UserRoleAdmin,
		http.MethodDelete,
		root+"/units/"+disposableUnit.ID.String()+"/",
		nil,
		http.StatusNoContent,
	)

	subscriberPage := decodeDeploymentRegistryRouteResponse[api.DeploymentUnitSubscriberPage](
		t,
		deploymentRegistryRouteRequest(
			t,
			router,
			ctx,
			first.organizationID,
			types.UserRoleReadOnly,
			http.MethodGet,
			root+"/subscribers/",
			nil,
			http.StatusOK,
		),
	)
	g.Expect(subscriberPage.Items).To(HaveLen(2))
	var firstSubscriber api.DeploymentUnitSubscriber
	for _, subscriber := range subscriberPage.Items {
		if subscriber.DeploymentUnitID == sharedUnit.ID {
			firstSubscriber = subscriber
			break
		}
	}
	g.Expect(firstSubscriber.ID).NotTo(Equal(uuid.Nil))
	subscriberConflictBody := "deployment unit subscriber is in use\n"
	for _, conflict := range []struct {
		method string
		path   string
		body   any
	}{
		{
			method: http.MethodPost,
			path:   root + "/subscribers/",
			body: api.CreateDeploymentUnitSubscriberRequest{
				DeploymentUnitID:       sharedUnit.ID,
				CustomerOrganizationID: thirdCustomerID,
			},
		},
		{
			method: http.MethodPut,
			path:   root + "/subscribers/" + firstSubscriber.ID.String() + "/",
			body: api.UpdateDeploymentUnitSubscriberRequest{
				RetiredAt: new(time.Time),
			},
		},
		{
			method: http.MethodDelete,
			path:   root + "/subscribers/" + firstSubscriber.ID.String() + "/",
		},
	} {
		recorder := deploymentRegistryRouteRequest(
			t,
			router,
			ctx,
			first.organizationID,
			types.UserRoleAdmin,
			conflict.method,
			conflict.path,
			conflict.body,
			http.StatusConflict,
		)
		g.Expect(recorder.Body.String()).To(Equal(subscriberConflictBody))
	}
	noOpSubscriber := decodeDeploymentRegistryRouteResponse[api.DeploymentUnitSubscriber](
		t,
		deploymentRegistryRouteRequest(
			t,
			router,
			ctx,
			first.organizationID,
			types.UserRoleAdmin,
			http.MethodPut,
			root+"/subscribers/"+firstSubscriber.ID.String()+"/",
			api.UpdateDeploymentUnitSubscriberRequest{},
			http.StatusOK,
		),
	)
	g.Expect(noOpSubscriber.ID).To(Equal(firstSubscriber.ID))

	mainDefinition := decodeDeploymentRegistryRouteResponse[api.ComponentDefinition](
		t,
		deploymentRegistryRouteRequest(
			t,
			router,
			ctx,
			first.organizationID,
			types.UserRoleAdmin,
			http.MethodPost,
			root+"/definitions/",
			api.CreateComponentDefinitionRequest{
				Key:             "routed-service",
				Name:            "Routed service",
				CapabilityScope: "service",
				ManagementState: types.RegistryManagementStateManaged,
			},
			http.StatusOK,
		),
	)
	disposableDefinition := decodeDeploymentRegistryRouteResponse[api.ComponentDefinition](
		t,
		deploymentRegistryRouteRequest(
			t,
			router,
			ctx,
			first.organizationID,
			types.UserRoleAdmin,
			http.MethodPost,
			root+"/definitions/",
			api.CreateComponentDefinitionRequest{
				Key:             "routed-disposable-service",
				Name:            "Routed disposable service",
				ManagementState: types.RegistryManagementStateManaged,
			},
			http.StatusOK,
		),
	)
	deploymentRegistryRouteRequest(
		t,
		router,
		ctx,
		first.organizationID,
		types.UserRoleAdmin,
		http.MethodPut,
		root+"/definitions/"+mainDefinition.ID.String()+"/",
		api.UpdateComponentDefinitionRequest{
			Name:            "Routed service updated",
			Description:     "updated through routed handler",
			CapabilityScope: "service",
			ManagementState: types.RegistryManagementStateManaged,
		},
		http.StatusOK,
	)
	mainAlias := decodeDeploymentRegistryRouteResponse[api.ComponentAlias](
		t,
		deploymentRegistryRouteRequest(
			t,
			router,
			ctx,
			first.organizationID,
			types.UserRoleAdmin,
			http.MethodPost,
			root+"/aliases/",
			api.CreateComponentAliasRequest{
				ComponentDefinitionID: mainDefinition.ID,
				Alias:                 " Service-Old ",
			},
			http.StatusOK,
		),
	)
	g.Expect(mainAlias.Alias).To(Equal("service-old"))
	disposableAlias := decodeDeploymentRegistryRouteResponse[api.ComponentAlias](
		t,
		deploymentRegistryRouteRequest(
			t,
			router,
			ctx,
			first.organizationID,
			types.UserRoleAdmin,
			http.MethodPost,
			root+"/aliases/",
			api.CreateComponentAliasRequest{
				ComponentDefinitionID: mainDefinition.ID,
				Alias:                 "disposable-alias",
			},
			http.StatusOK,
		),
	)
	deploymentRegistryRouteRequest(
		t,
		router,
		ctx,
		first.organizationID,
		types.UserRoleAdmin,
		http.MethodPut,
		root+"/aliases/"+mainAlias.ID.String()+"/",
		api.UpdateComponentAliasRequest{},
		http.StatusOK,
	)
	deploymentRegistryRouteRequest(
		t,
		router,
		ctx,
		first.organizationID,
		types.UserRoleAdmin,
		http.MethodDelete,
		root+"/aliases/"+disposableAlias.ID.String()+"/",
		nil,
		http.StatusNoContent,
	)

	mainInstance := decodeDeploymentRegistryRouteResponse[api.ComponentInstance](
		t,
		deploymentRegistryRouteRequest(
			t,
			router,
			ctx,
			first.organizationID,
			types.UserRoleAdmin,
			http.MethodPost,
			root+"/instances/",
			api.CreateComponentInstanceRequest{
				DeploymentUnitID:      mainUnit.ID,
				ComponentDefinitionID: mainDefinition.ID,
				PhysicalName:          "service-old",
				ManagementState:       types.RegistryManagementStateManaged,
			},
			http.StatusOK,
		),
	)
	disposableInstance := decodeDeploymentRegistryRouteResponse[api.ComponentInstance](
		t,
		deploymentRegistryRouteRequest(
			t,
			router,
			ctx,
			first.organizationID,
			types.UserRoleAdmin,
			http.MethodPost,
			root+"/instances/",
			api.CreateComponentInstanceRequest{
				DeploymentUnitID:      mainUnit.ID,
				ComponentDefinitionID: disposableDefinition.ID,
				PhysicalName:          "disposable-service",
				ManagementState:       types.RegistryManagementStateManaged,
			},
			http.StatusOK,
		),
	)
	renamedInstance := decodeDeploymentRegistryRouteResponse[api.ComponentInstance](
		t,
		deploymentRegistryRouteRequest(
			t,
			router,
			ctx,
			first.organizationID,
			types.UserRoleAdmin,
			http.MethodPut,
			root+"/instances/"+mainInstance.ID.String()+"/",
			api.UpdateComponentInstanceRequest{
				PhysicalName:     "service-new",
				ConfigNamespace:  "service",
				DatabaseBoundary: "service",
				HealthAdapter:    "http",
				ManagementState:  types.RegistryManagementStateManaged,
				RenamedFrom:      "service-old",
			},
			http.StatusOK,
		),
	)
	g.Expect(renamedInstance.PhysicalName).To(Equal("service-new"))
	retiredAliasAt := time.Now().UTC()
	protectedAliasRetirement := deploymentRegistryRouteRequest(
		t,
		router,
		ctx,
		first.organizationID,
		types.UserRoleAdmin,
		http.MethodPut,
		root+"/aliases/"+mainAlias.ID.String()+"/",
		api.UpdateComponentAliasRequest{RetiredAt: &retiredAliasAt},
		http.StatusConflict,
	)
	g.Expect(protectedAliasRetirement.Body.String()).To(Equal("component alias is in use\n"))
	protectedAliasDelete := deploymentRegistryRouteRequest(
		t,
		router,
		ctx,
		first.organizationID,
		types.UserRoleAdmin,
		http.MethodDelete,
		root+"/aliases/"+mainAlias.ID.String()+"/",
		nil,
		http.StatusConflict,
	)
	g.Expect(protectedAliasDelete.Body.String()).To(Equal("component alias is in use\n"))
	protectedInstanceDelete := deploymentRegistryRouteRequest(
		t,
		router,
		ctx,
		first.organizationID,
		types.UserRoleAdmin,
		http.MethodDelete,
		root+"/instances/"+mainInstance.ID.String()+"/",
		nil,
		http.StatusConflict,
	)
	g.Expect(protectedInstanceDelete.Body.String()).To(Equal("component instance is in use\n"))
	deploymentRegistryRouteRequest(
		t,
		router,
		ctx,
		first.organizationID,
		types.UserRoleAdmin,
		http.MethodDelete,
		root+"/instances/"+disposableInstance.ID.String()+"/",
		nil,
		http.StatusNoContent,
	)
	deploymentRegistryRouteRequest(
		t,
		router,
		ctx,
		first.organizationID,
		types.UserRoleAdmin,
		http.MethodDelete,
		root+"/definitions/"+disposableDefinition.ID.String()+"/",
		nil,
		http.StatusNoContent,
	)

	for _, path := range []string{
		root + "/scopes/",
		root + "/assignments/",
		root + "/units/",
		root + "/subscribers/",
		root + "/definitions/",
		root + "/aliases/",
		root + "/instances/",
		root + "/placements/",
	} {
		deploymentRegistryRouteRequest(
			t,
			router,
			ctx,
			first.organizationID,
			types.UserRoleReadOnly,
			http.MethodGet,
			path,
			nil,
			http.StatusOK,
		)
	}
	itemPaths := []string{
		root + "/scopes/" + mainScope.ID.String() + "/",
		root + "/assignments/" + mainAssignment.ID.String() + "/",
		root + "/units/" + mainUnit.ID.String() + "/",
		root + "/subscribers/" + firstSubscriber.ID.String() + "/",
		root + "/definitions/" + mainDefinition.ID.String() + "/",
		root + "/aliases/" + mainAlias.ID.String() + "/",
		root + "/instances/" + mainInstance.ID.String() + "/",
		root + "/placements/" + mainUnit.ID.String(),
	}
	for _, path := range itemPaths {
		deploymentRegistryRouteRequest(
			t,
			router,
			ctx,
			first.organizationID,
			types.UserRoleReadOnly,
			http.MethodGet,
			path,
			nil,
			http.StatusOK,
		)
		deploymentRegistryRouteRequest(
			t,
			router,
			ctx,
			second.organizationID,
			types.UserRoleReadOnly,
			http.MethodGet,
			path,
			nil,
			http.StatusNotFound,
		)
	}

	placementPage := decodeDeploymentRegistryRouteResponse[api.DeploymentRegistryPlacementPage](
		t,
		deploymentRegistryRouteRequest(
			t,
			router,
			ctx,
			first.organizationID,
			types.UserRoleReadOnly,
			http.MethodGet,
			root+"/placements/?limit=1",
			nil,
			http.StatusOK,
		),
	)
	g.Expect(placementPage.Items).To(HaveLen(1))
	g.Expect(placementPage.NextCursor).NotTo(BeEmpty())

	protectedDelete := deploymentRegistryRouteRequest(
		t,
		router,
		ctx,
		first.organizationID,
		types.UserRoleAdmin,
		http.MethodDelete,
		root+"/scopes/"+mainScope.ID.String()+"/",
		nil,
		http.StatusConflict,
	)
	g.Expect(protectedDelete.Body.String()).To(Equal("deployment scope is in use\n"))
}

func deploymentRegistryRouteRequest(
	t *testing.T,
	router http.Handler,
	ctx context.Context,
	organizationID uuid.UUID,
	role types.UserRole,
	method string,
	path string,
	body any,
	wantStatus int,
) *httptest.ResponseRecorder {
	t.Helper()
	var requestBody string
	if body != nil {
		payload, err := json.Marshal(body)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
		requestBody = string(payload)
	}
	request := httptest.NewRequest(method, path, strings.NewReader(requestBody))
	request = request.WithContext(
		deploymentRegistryHandlerContext(ctx, organizationID, role),
	)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	NewWithT(t).Expect(recorder.Code).To(Equal(wantStatus), recorder.Body.String())
	return recorder
}

func decodeDeploymentRegistryRouteResponse[T any](
	t *testing.T,
	recorder *httptest.ResponseRecorder,
) T {
	t.Helper()
	var response T
	NewWithT(t).Expect(json.Unmarshal(recorder.Body.Bytes(), &response)).To(Succeed())
	return response
}

type deploymentRegistryHandlerDependencies struct {
	organizationID         uuid.UUID
	customerOrganizationID uuid.UUID
	environmentID          uuid.UUID
	deploymentTargetID     uuid.UUID
}

func createDeploymentRegistryHandlerDependencies(
	t *testing.T,
	ctx context.Context,
) deploymentRegistryHandlerDependencies {
	t.Helper()
	var result deploymentRegistryHandlerDependencies
	err := internalctx.GetDb(ctx).QueryRow(ctx,
		`INSERT INTO Organization (name) VALUES (@name) RETURNING id`,
		pgx.NamedArgs{"name": "Registry handler organization " + uuid.NewString()},
	).Scan(&result.organizationID)
	if err != nil {
		t.Fatalf("create registry handler organization: %v", err)
	}
	err = internalctx.GetDb(ctx).QueryRow(ctx,
		`INSERT INTO CustomerOrganization (organization_id, name)
		 VALUES (@organizationID, @name)
		 RETURNING id`,
		pgx.NamedArgs{
			"organizationID": result.organizationID,
			"name":           "Registry handler customer " + uuid.NewString(),
		},
	).Scan(&result.customerOrganizationID)
	if err != nil {
		t.Fatalf("create registry handler customer: %v", err)
	}
	err = internalctx.GetDb(ctx).QueryRow(ctx,
		`INSERT INTO Environment (organization_id, name)
		 VALUES (@organizationID, @name)
		 RETURNING id`,
		pgx.NamedArgs{
			"organizationID": result.organizationID,
			"name":           "Registry handler environment " + uuid.NewString(),
		},
	).Scan(&result.environmentID)
	if err != nil {
		t.Fatalf("create registry handler environment: %v", err)
	}
	err = internalctx.GetDb(ctx).QueryRow(ctx,
		`INSERT INTO DeploymentTarget (name, type, organization_id, agent_version_id)
		 VALUES (
		   @name,
		   'docker',
		   @organizationID,
		   (SELECT id FROM AgentVersion ORDER BY created_at LIMIT 1)
		 )
		 RETURNING id`,
		pgx.NamedArgs{
			"organizationID": result.organizationID,
			"name":           "Registry handler target " + uuid.NewString(),
		},
	).Scan(&result.deploymentTargetID)
	if err != nil {
		t.Fatalf("create registry handler target: %v", err)
	}
	return result
}

func createDeploymentRegistryHandlerCustomer(
	t *testing.T,
	ctx context.Context,
	organizationID uuid.UUID,
) uuid.UUID {
	t.Helper()
	var customerOrganizationID uuid.UUID
	err := internalctx.GetDb(ctx).QueryRow(ctx,
		`INSERT INTO CustomerOrganization (organization_id, name)
		 VALUES (@organizationID, @name)
		 RETURNING id`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"name":           "Registry handler customer " + uuid.NewString(),
		},
	).Scan(&customerOrganizationID)
	if err != nil {
		t.Fatalf("create registry handler customer: %v", err)
	}
	return customerOrganizationID
}

func createDeploymentRegistryHandlerTarget(
	t *testing.T,
	ctx context.Context,
	organizationID uuid.UUID,
) uuid.UUID {
	t.Helper()
	var deploymentTargetID uuid.UUID
	err := internalctx.GetDb(ctx).QueryRow(ctx,
		`INSERT INTO DeploymentTarget (name, type, organization_id, agent_version_id)
		 VALUES (
		   @name,
		   'docker',
		   @organizationID,
		   (SELECT id FROM AgentVersion ORDER BY created_at LIMIT 1)
		 )
		 RETURNING id`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"name":           "Registry handler target " + uuid.NewString(),
		},
	).Scan(&deploymentTargetID)
	if err != nil {
		t.Fatalf("create registry handler target: %v", err)
	}
	return deploymentTargetID
}

func deploymentRegistryHandlerContext(
	ctx context.Context,
	organizationID uuid.UUID,
	role types.UserRole,
) context.Context {
	ctx = internalctx.WithLogger(ctx, zap.NewNop())
	userAuth := testChannelAuth()
	userAuth.orgID = organizationID
	userAuth.role = role
	return auth.Authentication.NewContext(ctx, userAuth)
}
