package routing

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/distr-sh/distr/internal/executionruntime"
	obsertracing "github.com/distr-sh/distr/internal/observability/tracing"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
)

func TestScopedAuthorizationAdminRoutesArePublishedInOpenAPI(t *testing.T) {
	g := NewWithT(t)
	tracer := obsertracing.NoopTracer{}
	router := NewRouter(
		zap.NewNop(),
		nil,
		nil,
		nil,
		nil,
		nil,
		obsertracing.Tracers{Default: tracer, Agent: tracer},
		nil,
		nil,
	)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodGet, "/docs/openapi.json", nil),
	)
	g.Expect(recorder.Code).To(Equal(http.StatusOK))

	var document struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	g.Expect(json.Unmarshal(recorder.Body.Bytes(), &document)).To(Succeed())
	for _, path := range []string{
		"/api/v1/authorization/roles",
		"/api/v1/authorization/bindings",
		"/api/v1/authorization/groups",
		"/api/v1/authorization/groups/{groupId}/members",
		"/api/v1/authorization/control-plane-enrollments",
	} {
		g.Expect(document.Paths).To(HaveKey(path), path)
		g.Expect(document.Paths[path]).To(HaveKey("get"), path)
		g.Expect(document.Paths[path]).To(HaveKey("post"), path)
		var operation struct {
			Parameters []struct {
				Name string `json:"name"`
			} `json:"parameters"`
		}
		g.Expect(json.Unmarshal(document.Paths[path]["get"], &operation)).To(Succeed())
		g.Expect(operation.Parameters).To(ContainElements(
			HaveField("Name", "cursor"),
			HaveField("Name", "limit"),
		), path)
	}
	for _, path := range []string{
		"/api/v1/authorization/bindings/{bindingId}/revocations",
		"/api/v1/authorization/groups/{groupId}/members/{memberId}/revocations",
	} {
		g.Expect(document.Paths).To(HaveKey(path), path)
		g.Expect(document.Paths[path]).To(HaveKey("post"), path)
	}
	g.Expect(string(recorder.Body.Bytes())).To(ContainSubstring(`"nextCursor"`))
}

func TestDeploymentRegistryRoutesArePublishedInOpenAPI(t *testing.T) {
	const subscriberCollectionPath = "/api/v1/deployment-registry/subscribers"
	g := NewWithT(t)
	tracer := obsertracing.NoopTracer{}
	router := NewRouter(
		zap.NewNop(),
		nil,
		nil,
		nil,
		nil,
		nil,
		obsertracing.Tracers{Default: tracer, Agent: tracer},
		nil,
		nil,
		executionruntime.Dependencies{},
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/docs/openapi.json", nil)

	router.ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusOK))
	var document struct {
		Paths      map[string]map[string]json.RawMessage `json:"paths"`
		Components struct {
			SecuritySchemes map[string]json.RawMessage `json:"securitySchemes"`
		} `json:"components"`
	}
	g.Expect(json.Unmarshal(recorder.Body.Bytes(), &document)).To(Succeed())
	g.Expect(document.Components.SecuritySchemes).To(
		And(HaveKey("accessToken"), HaveKey("bearer")),
	)

	resources := []struct {
		collection       string
		item             string
		id               string
		collectionSchema string
		itemSchema       string
		updateStatuses   []string
		deleteStatuses   []string
	}{
		{
			collection:       "/api/v1/deployment-registry/scopes",
			item:             "/api/v1/deployment-registry/scopes/{scopeId}",
			id:               "scopeId",
			collectionSchema: "#/components/schemas/ApiDeploymentScopePage",
			itemSchema:       "#/components/schemas/ApiDeploymentScope",
			updateStatuses:   []string{"200", "400", "403", "404"},
			deleteStatuses:   []string{"204", "403", "404", "409"},
		},
		{
			collection:       "/api/v1/deployment-registry/assignments",
			item:             "/api/v1/deployment-registry/assignments/{assignmentId}",
			id:               "assignmentId",
			collectionSchema: "#/components/schemas/ApiTargetEnvironmentAssignmentPage",
			itemSchema:       "#/components/schemas/ApiTargetEnvironmentAssignment",
			updateStatuses:   []string{"200", "400", "403", "404", "409"},
			deleteStatuses:   []string{"204", "403", "404", "409"},
		},
		{
			collection:       "/api/v1/deployment-registry/units",
			item:             "/api/v1/deployment-registry/units/{unitId}",
			id:               "unitId",
			collectionSchema: "#/components/schemas/ApiDeploymentUnitPage",
			itemSchema:       "#/components/schemas/ApiDeploymentUnit",
			updateStatuses:   []string{"200", "400", "403", "404"},
			deleteStatuses:   []string{"204", "403", "404", "409"},
		},
		{
			collection:       subscriberCollectionPath,
			item:             subscriberCollectionPath + "/{subscriberId}",
			id:               "subscriberId",
			collectionSchema: "#/components/schemas/ApiDeploymentUnitSubscriberPage",
			itemSchema:       "#/components/schemas/ApiDeploymentUnitSubscriber",
			updateStatuses:   []string{"200", "400", "403", "404", "409"},
			deleteStatuses:   []string{"403", "404", "409"},
		},
		{
			collection:       "/api/v1/deployment-registry/definitions",
			item:             "/api/v1/deployment-registry/definitions/{definitionId}",
			id:               "definitionId",
			collectionSchema: "#/components/schemas/ApiComponentDefinitionPage",
			itemSchema:       "#/components/schemas/ApiComponentDefinition",
			updateStatuses:   []string{"200", "400", "403", "404"},
			deleteStatuses:   []string{"204", "403", "404", "409"},
		},
		{
			collection:       "/api/v1/deployment-registry/aliases",
			item:             "/api/v1/deployment-registry/aliases/{aliasId}",
			id:               "aliasId",
			collectionSchema: "#/components/schemas/ApiComponentAliasPage",
			itemSchema:       "#/components/schemas/ApiComponentAlias",
			updateStatuses:   []string{"200", "400", "403", "404", "409"},
			deleteStatuses:   []string{"204", "403", "404", "409"},
		},
		{
			collection:       "/api/v1/deployment-registry/instances",
			item:             "/api/v1/deployment-registry/instances/{instanceId}",
			id:               "instanceId",
			collectionSchema: "#/components/schemas/ApiComponentInstancePage",
			itemSchema:       "#/components/schemas/ApiComponentInstance",
			updateStatuses:   []string{"200", "400", "403", "404", "409"},
			deleteStatuses:   []string{"204", "403", "404", "409"},
		},
	}
	for _, resource := range resources {
		listOperation := readDeploymentRegistryOpenAPIOperation(
			t,
			document.Paths,
			resource.collection,
			"get",
		)
		expectDeploymentRegistryListOperation(t, listOperation, resource.collectionSchema)
		createOperation := readDeploymentRegistryOpenAPIOperation(
			t,
			document.Paths,
			resource.collection,
			"post",
		)
		expectDeploymentRegistrySecurity(t, createOperation)
		expectDeploymentRegistryJSONRequest(t, createOperation)
		if resource.collection == subscriberCollectionPath {
			expectDeploymentRegistryResponseStatuses(
				t,
				createOperation,
				"400",
				"403",
				"404",
				"409",
			)
			expectDeploymentRegistryPlainTextResponses(
				t,
				createOperation,
				"400",
				"403",
				"404",
				"409",
			)
			g.Expect(createOperation.Responses).NotTo(HaveKey("200"))
		} else {
			expectDeploymentRegistryResponseStatuses(
				t,
				createOperation,
				"200",
				"400",
				"403",
				"404",
				"409",
			)
			expectDeploymentRegistryJSONResponse(t, createOperation, resource.itemSchema)
			expectDeploymentRegistryPlainTextResponses(
				t,
				createOperation,
				"400",
				"403",
				"404",
				"409",
			)
		}

		getOperation := readDeploymentRegistryOpenAPIOperation(
			t,
			document.Paths,
			resource.item,
			"get",
		)
		expectDeploymentRegistryPathParameter(t, getOperation, resource.id)
		expectDeploymentRegistryItemGetOperation(t, getOperation, resource.itemSchema)
		updateOperation := readDeploymentRegistryOpenAPIOperation(
			t,
			document.Paths,
			resource.item,
			"put",
		)
		expectDeploymentRegistryPathParameter(t, updateOperation, resource.id)
		expectDeploymentRegistrySecurity(t, updateOperation)
		expectDeploymentRegistryJSONRequest(t, updateOperation)
		expectDeploymentRegistryResponseStatuses(
			t,
			updateOperation,
			resource.updateStatuses...,
		)
		expectDeploymentRegistryJSONResponse(t, updateOperation, resource.itemSchema)
		expectDeploymentRegistryPlainTextResponses(
			t,
			updateOperation,
			deploymentRegistryErrorStatuses(resource.updateStatuses)...,
		)
		deleteOperation := readDeploymentRegistryOpenAPIOperation(
			t,
			document.Paths,
			resource.item,
			"delete",
		)
		expectDeploymentRegistryPathParameter(t, deleteOperation, resource.id)
		expectDeploymentRegistrySecurity(t, deleteOperation)
		expectDeploymentRegistryResponseStatuses(
			t,
			deleteOperation,
			resource.deleteStatuses...,
		)
		expectDeploymentRegistryPlainTextResponses(
			t,
			deleteOperation,
			deploymentRegistryErrorStatuses(resource.deleteStatuses)...,
		)
		if slices.Contains(resource.deleteStatuses, "204") {
			expectDeploymentRegistryNoContentResponse(t, deleteOperation, "204")
		} else {
			g.Expect(deleteOperation.Responses).NotTo(HaveKey("204"))
		}
	}

	placementList := readDeploymentRegistryOpenAPIOperation(
		t,
		document.Paths,
		"/api/v1/deployment-registry/placements",
		"get",
	)
	expectDeploymentRegistryListOperation(
		t,
		placementList,
		"#/components/schemas/ApiDeploymentRegistryPlacementPage",
	)
	placementGet := readDeploymentRegistryOpenAPIOperation(
		t,
		document.Paths,
		"/api/v1/deployment-registry/placements/{unitId}",
		"get",
	)
	expectDeploymentRegistryPathParameter(t, placementGet, "unitId")
	expectDeploymentRegistryItemGetOperation(
		t,
		placementGet,
		"#/components/schemas/ApiDeploymentRegistryPlacement",
	)

	for _, method := range []string{"post", "put", "delete"} {
		path := subscriberCollectionPath
		if method != "post" {
			path += "/{subscriberId}"
		}
		operation := readDeploymentRegistryOpenAPIOperation(t, document.Paths, path, method)
		g.Expect(operation.Description).To(And(
			ContainSubstring("Compatibility"),
			ContainSubstring("atomically"),
		))
		if method == "put" {
			g.Expect(operation.Description).To(ContainSubstring("200 OK"))
		}
		g.Expect(operation.Description).To(ContainSubstring("409 Conflict"))
	}
}

func TestTargetConfigSnapshotRoutesArePublishedWithoutMutators(t *testing.T) {
	g := NewWithT(t)
	tracer := obsertracing.NoopTracer{}
	router := NewRouter(
		zap.NewNop(),
		nil,
		nil,
		nil,
		nil,
		nil,
		obsertracing.Tracers{Default: tracer, Agent: tracer},
		nil,
		nil,
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/docs/openapi.json", nil)

	router.ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusOK))
	var document struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	g.Expect(json.Unmarshal(recorder.Body.Bytes(), &document)).To(Succeed())
	collection := document.Paths["/api/v1/target-config-snapshots"]
	item := document.Paths["/api/v1/target-config-snapshots/{snapshotId}"]
	verify := document.Paths["/api/v1/target-config-snapshots/{snapshotId}/verify"]
	g.Expect(collection).To(And(HaveKey("get"), HaveKey("post")))
	g.Expect(item).To(HaveKey("get"))
	g.Expect(item).NotTo(Or(HaveKey("put"), HaveKey("patch"), HaveKey("delete")))
	g.Expect(verify).To(HaveKey("post"))
}

type deploymentRegistryOpenAPIOperation struct {
	Description string `json:"description"`
	Parameters  []struct {
		Name     string          `json:"name"`
		In       string          `json:"in"`
		Required bool            `json:"required"`
		Schema   json.RawMessage `json:"schema"`
	} `json:"parameters"`
	RequestBody *struct {
		Content map[string]struct {
			Schema json.RawMessage `json:"schema"`
		} `json:"content"`
	} `json:"requestBody"`
	Responses map[string]struct {
		Content map[string]struct {
			Schema json.RawMessage `json:"schema"`
		} `json:"content"`
	} `json:"responses"`
	Security []map[string][]string `json:"security"`
}

func readDeploymentRegistryOpenAPIOperation(
	t *testing.T,
	paths map[string]map[string]json.RawMessage,
	path string,
	method string,
) deploymentRegistryOpenAPIOperation {
	t.Helper()
	g := NewWithT(t)
	g.Expect(paths).To(HaveKey(path), path)
	g.Expect(paths[path]).To(HaveKey(method), path+" "+method)
	var operation deploymentRegistryOpenAPIOperation
	g.Expect(json.Unmarshal(paths[path][method], &operation)).To(Succeed())
	return operation
}

func expectDeploymentRegistryListOperation(
	t *testing.T,
	operation deploymentRegistryOpenAPIOperation,
	responseSchema string,
) {
	t.Helper()
	g := NewWithT(t)
	g.Expect(operation.Parameters).To(ContainElements(
		HaveField("Name", "cursor"),
		HaveField("Name", "limit"),
	))
	for _, parameter := range operation.Parameters {
		if parameter.Name == "cursor" || parameter.Name == "limit" {
			g.Expect(parameter.In).To(Equal("query"))
			g.Expect(parameter.Schema).NotTo(BeEmpty())
		}
	}
	expectDeploymentRegistrySecurity(t, operation)
	expectDeploymentRegistryResponseStatuses(t, operation, "200", "400", "403")
	expectDeploymentRegistryJSONResponse(t, operation, responseSchema)
	expectDeploymentRegistryPlainTextResponses(t, operation, "400", "403")
}

func expectDeploymentRegistryItemGetOperation(
	t *testing.T,
	operation deploymentRegistryOpenAPIOperation,
	responseSchema string,
) {
	t.Helper()
	expectDeploymentRegistrySecurity(t, operation)
	expectDeploymentRegistryResponseStatuses(t, operation, "200", "403", "404")
	expectDeploymentRegistryJSONResponse(t, operation, responseSchema)
	expectDeploymentRegistryPlainTextResponses(t, operation, "403", "404")
}

func expectDeploymentRegistryPathParameter(
	t *testing.T,
	operation deploymentRegistryOpenAPIOperation,
	name string,
) {
	t.Helper()
	g := NewWithT(t)
	for _, parameter := range operation.Parameters {
		if parameter.Name == name {
			g.Expect(parameter.In).To(Equal("path"))
			g.Expect(parameter.Required).To(BeTrue())
			g.Expect(parameter.Schema).NotTo(BeEmpty())
			return
		}
	}
	t.Fatalf("OpenAPI operation is missing path parameter %q", name)
}

func expectDeploymentRegistryJSONRequest(
	t *testing.T,
	operation deploymentRegistryOpenAPIOperation,
) {
	t.Helper()
	g := NewWithT(t)
	g.Expect(operation.RequestBody).NotTo(BeNil())
	g.Expect(operation.RequestBody.Content).To(HaveKey("application/json"))
	g.Expect(operation.RequestBody.Content["application/json"].Schema).NotTo(BeEmpty())
}

func expectDeploymentRegistryResponseStatuses(
	t *testing.T,
	operation deploymentRegistryOpenAPIOperation,
	statuses ...string,
) {
	t.Helper()
	actual := make([]string, 0, len(operation.Responses))
	for status := range operation.Responses {
		actual = append(actual, status)
	}
	NewWithT(t).Expect(actual).To(ConsistOf(statuses))
}

func expectDeploymentRegistryJSONResponse(
	t *testing.T,
	operation deploymentRegistryOpenAPIOperation,
	expectedRef string,
) {
	t.Helper()
	g := NewWithT(t)
	g.Expect(operation.Responses).To(HaveKey("200"))
	content := operation.Responses["200"].Content
	g.Expect(content).To(HaveLen(1))
	g.Expect(content).To(HaveKey("application/json"))
	var schema struct {
		Ref string `json:"$ref"`
	}
	g.Expect(json.Unmarshal(content["application/json"].Schema, &schema)).To(Succeed())
	g.Expect(schema.Ref).To(Equal(expectedRef))
}

func deploymentRegistryErrorStatuses(statuses []string) []string {
	errors := make([]string, 0, len(statuses))
	for _, status := range statuses {
		if status != "200" && status != "204" {
			errors = append(errors, status)
		}
	}
	return errors
}

func expectDeploymentRegistryPlainTextResponses(
	t *testing.T,
	operation deploymentRegistryOpenAPIOperation,
	statuses ...string,
) {
	t.Helper()
	g := NewWithT(t)
	for _, status := range statuses {
		g.Expect(operation.Responses).To(HaveKey(status))
		content := operation.Responses[status].Content
		g.Expect(content).To(HaveLen(1), status)
		g.Expect(content).To(HaveKey("text/plain"), status)
		var schema struct {
			Type string `json:"type"`
		}
		g.Expect(json.Unmarshal(content["text/plain"].Schema, &schema)).To(Succeed())
		g.Expect(schema.Type).To(Equal("string"), status)
	}
}

func expectDeploymentRegistryNoContentResponse(
	t *testing.T,
	operation deploymentRegistryOpenAPIOperation,
	status string,
) {
	t.Helper()
	g := NewWithT(t)
	g.Expect(operation.Responses).To(HaveKey(status))
	g.Expect(operation.Responses[status].Content).To(BeEmpty())
}

func expectDeploymentRegistrySecurity(
	t *testing.T,
	operation deploymentRegistryOpenAPIOperation,
) {
	t.Helper()
	NewWithT(t).Expect(operation.Security).To(ConsistOf(
		map[string][]string{"accessToken": {}},
		map[string][]string{"bearer": {}},
	))
}
