package db

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db/queryable"
	"github.com/distr-sh/distr/internal/deploymentregistry"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	. "github.com/onsi/gomega"
)

func TestOperatorFleetSQLFiltersAuthorizationBeforeTotalAndPagination(t *testing.T) {
	g := NewWithT(t)
	normalized := strings.ToLower(operatorFleetSQL)
	authorized := strings.Index(normalized, "authorized_registry as")
	filtered := strings.Index(normalized, "filtered_fleet as")
	paged := strings.Index(normalized, "paged_fleet as")
	total := strings.Index(normalized, "select count(*) from filtered_fleet")

	g.Expect(authorized).To(BeNumerically(">=", 0))
	g.Expect(filtered).To(BeNumerically(">", authorized))
	g.Expect(paged).To(BeNumerically(">", filtered))
	g.Expect(total).To(BeNumerically(">", filtered))
	g.Expect(normalized).To(ContainSubstring("instance.organization_id = @organizationid"))
	g.Expect(normalized).To(ContainSubstring("@organizationwide"))
	g.Expect(normalized).To(ContainSubstring("= any(@customerscopeids::uuid[])"))
	g.Expect(normalized).To(ContainSubstring("= any(@environmentscopeids::uuid[])"))
	g.Expect(normalized).To(ContainSubstring("= any(@deploymentunitscopeids::uuid[])"))
	g.Expect(normalized).To(ContainSubstring("= any(@componentscopeids::uuid[])"))
	g.Expect(normalized).To(ContainSubstring("(created_at, id) <"))
	g.Expect(normalized).To(ContainSubstring("order by created_at desc, id desc"))
}

func TestOperatorFleetSQLKeepsSharedUnitSingleAndProjectsOperationalStates(t *testing.T) {
	g := NewWithT(t)
	normalized := strings.ToLower(operatorFleetSQL)

	g.Expect(normalized).To(ContainSubstring("subscriber_customers as"))
	g.Expect(normalized).To(ContainSubstring("string_agg"))
	g.Expect(normalized).To(ContainSubstring("count(distinct"))
	g.Expect(normalized).To(ContainSubstring("'partial'"))
	g.Expect(normalized).To(ContainSubstring("'stale'"))
	g.Expect(normalized).To(ContainSubstring("'unknown'"))
	g.Expect(normalized).To(ContainSubstring("'conflict'"))
	g.Expect(normalized).To(ContainSubstring("'enabled'"))
	g.Expect(normalized).To(ContainSubstring("'disabled'"))
	g.Expect(normalized).To(ContainSubstring("controlplaneenrollment"))
	g.Expect(normalized).To(ContainSubstring("@decisionat"))
	for _, filter := range []string{
		"@customerorganizationid",
		"@environmentid",
		"@deploymenttargetid",
		"@deploymentunitid",
		"@component",
		"@observedstate",
		"@drift",
		"@enrollment",
		"@search",
	} {
		g.Expect(normalized).To(ContainSubstring(filter))
	}
	g.Expect(normalized).To(ContainSubstring("componentdesiredstatehead"))
	g.Expect(normalized).To(ContainSubstring("pendingdesiredrevision"))
	g.Expect(normalized).To(ContainSubstring("activedesiredrevision"))
	g.Expect(normalized).To(ContainSubstring("componentreleaseartifact"))
	g.Expect(normalized).To(ContainSubstring("latest_pending as"))
	g.Expect(normalized).To(ContainSubstring("latest_attempt as"))
	for _, identityField := range []string{
		"'observedevidencechecksum', fleet.observed_evidence_checksum",
		"'observedartifactdigest', fleet.observed_artifact_digest",
		"'observedconfigchecksum', fleet.observed_config_checksum",
		"'observedschemaversion', fleet.observed_schema_version",
		"'observedcapabilitychecksum', fleet.observed_capability_checksum",
		"'observedplatform', fleet.observed_platform",
		"'observedhealth', fleet.observed_health",
	} {
		g.Expect(normalized).To(ContainSubstring(identityField))
	}
}

func TestListOperatorFleetRowsUsesOneBoundedQueryAndDecodesEmptyAndPartialRows(t *testing.T) {
	g := NewWithT(t)
	organizationID := uuid.New()
	environmentID := uuid.New()
	targetID := uuid.New()
	unitID := uuid.New()
	componentID := uuid.New()
	createdAt := time.Date(2026, time.July, 22, 5, 20, 0, 0, time.UTC)
	row := types.FleetRow{
		ID:                         componentID,
		CreatedAt:                  createdAt,
		EnvironmentID:              environmentID,
		Environment:                "Production",
		DeploymentTargetID:         targetID,
		Target:                     "target-a",
		DeploymentUnitID:           unitID,
		Unit:                       "shared-runtime",
		ComponentID:                &componentID,
		Component:                  "api",
		ObservedState:              "partial",
		ObservedEvidenceChecksum:   "sha256:evidence",
		ObservedArtifactDigest:     "sha256:artifact",
		ObservedConfigChecksum:     "sha256:config",
		ObservedSchemaVersion:      "2026-07-28",
		ObservedCapabilityChecksum: "sha256:capabilities",
		ObservedPlatform:           "linux/amd64",
		ObservedHealth:             "HEALTHY",
		Drift:                      "unknown",
		Enrollment:                 "enabled",
	}
	payload, err := json.Marshal(operatorFleetPayload{
		Total: 1,
		Items: []types.FleetRow{row},
	})
	g.Expect(err).NotTo(HaveOccurred())
	database := &operatorFleetQueryable{payload: string(payload)}

	result, err := ListOperatorFleetRows(
		internalctx.WithDb(context.Background(), database),
		OperatorFleetQuery{
			OrganizationID:         organizationID,
			DecisionAt:             createdAt,
			EnvironmentScopeIDs:    []uuid.UUID{environmentID},
			DeploymentUnitScopeIDs: []uuid.UUID{},
			CustomerScopeIDs:       []uuid.UUID{},
			ComponentScopeIDs:      []uuid.UUID{},
			Filter: types.FleetFilter{
				EnvironmentID: &environmentID,
				ObservedState: "partial",
			},
			Limit: 51,
		},
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Total).To(Equal(int64(1)))
	g.Expect(result.Items).To(Equal([]types.FleetRow{row}))
	g.Expect(database.queryCalls).To(Equal(1))
	g.Expect(strings.ToLower(database.query)).To(ContainSubstring("limit @fetchlimit"))
	g.Expect(database.arguments).To(HaveLen(1))
	arguments := database.arguments[0].(pgx.NamedArgs)
	g.Expect(arguments["organizationID"]).To(Equal(organizationID))
	g.Expect(arguments["decisionAt"]).To(Equal(createdAt))
	g.Expect(arguments["environmentID"]).To(Equal(&environmentID))
	g.Expect(arguments["observedState"]).To(Equal("partial"))
	g.Expect(arguments["fetchLimit"]).To(Equal(51))

	database.payload = `{"total":0,"items":[]}`
	result, err = ListOperatorFleetRows(
		internalctx.WithDb(context.Background(), database),
		OperatorFleetQuery{
			OrganizationID:         organizationID,
			DecisionAt:             createdAt,
			OrganizationWide:       true,
			CustomerScopeIDs:       []uuid.UUID{},
			EnvironmentScopeIDs:    []uuid.UUID{},
			DeploymentUnitScopeIDs: []uuid.UUID{},
			ComponentScopeIDs:      []uuid.UUID{},
			Limit:                  1,
		},
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Items).To(BeEmpty())
	g.Expect(result.Total).To(BeZero())
}

func TestListOperatorFleetRowsProjectsNativeObservationIdentity(t *testing.T) {
	ctx, _ := deploymentRegistryIsolatedPool(t, 160)
	g := NewWithT(t)
	deps := createDeploymentRegistryDependencies(t, ctx)
	decisionAt := time.Date(2026, time.July, 22, 5, 45, 0, 0, time.UTC)
	placement := createDeploymentRegistryPlacement(
		t, ctx, deps, "fleet-observation-identity", decisionAt.Add(-time.Hour),
	)
	recordOperatorFleetObservation(
		t, ctx, placement, decisionAt, "COMPLETE", decisionAt.Add(time.Hour), "d",
	)

	result, err := ListOperatorFleetRows(ctx, OperatorFleetQuery{
		OrganizationID:         deps.organizationID,
		DecisionAt:             decisionAt,
		OrganizationWide:       true,
		CustomerScopeIDs:       []uuid.UUID{},
		EnvironmentScopeIDs:    []uuid.UUID{},
		DeploymentUnitScopeIDs: []uuid.UUID{},
		ComponentScopeIDs:      []uuid.UUID{},
		Limit:                  10,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Items).To(HaveLen(1))

	checksum := "sha256:" + strings.Repeat("d", 64)
	row := result.Items[0]
	g.Expect(row.ObservedEvidenceChecksum).To(Equal(checksum))
	g.Expect(row.ObservedArtifactDigest).To(Equal(checksum))
	g.Expect(row.ObservedConfigChecksum).To(Equal(checksum))
	g.Expect(row.ObservedSchemaVersion).To(Equal("1"))
	g.Expect(row.ObservedCapabilityChecksum).To(Equal(checksum))
	g.Expect(row.ObservedPlatform).To(Equal("linux/amd64"))
	g.Expect(row.ObservedHealth).To(Equal("HEALTHY"))
}

func TestListOperatorFleetRowsIsolatesTenantKeepsSharedUnitSingleAndPaginatesTies(t *testing.T) {
	ctx, _ := deploymentRegistryIsolatedPool(t, 160)
	g := NewWithT(t)
	first := createDeploymentRegistryDependencies(t, ctx)
	second := createDeploymentRegistryDependencies(t, ctx)
	createdAt := time.Date(2026, time.July, 22, 5, 30, 0, 0, time.UTC)
	dedicated := createDeploymentRegistryPlacement(t, ctx, first, "fleet-dedicated", createdAt)
	foreign := createDeploymentRegistryPlacement(t, ctx, second, "fleet-foreign", createdAt)
	shared, secondCustomerID := createOperatorFleetSharedPlacement(t, ctx, first, createdAt)
	setOperatorFleetComponentCreatedAt(t, ctx, dedicated.Instances[0].ID, first.organizationID, createdAt)
	setOperatorFleetComponentCreatedAt(t, ctx, foreign.Instances[0].ID, second.organizationID, createdAt)
	setOperatorFleetComponentCreatedAt(t, ctx, shared.Instances[0].ID, first.organizationID, createdAt)

	query := OperatorFleetQuery{
		OrganizationID:         first.organizationID,
		DecisionAt:             createdAt.Add(time.Minute),
		CustomerScopeIDs:       []uuid.UUID{first.customerOrganizationID},
		EnvironmentScopeIDs:    []uuid.UUID{},
		DeploymentUnitScopeIDs: []uuid.UUID{},
		ComponentScopeIDs:      []uuid.UUID{},
		Limit:                  1,
	}
	firstPage, err := ListOperatorFleetRows(ctx, query)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(firstPage.Total).To(Equal(int64(2)))
	g.Expect(firstPage.Items).To(HaveLen(1))
	g.Expect(firstPage.Items[0].ID).NotTo(Equal(foreign.Instances[0].ID))

	query.Cursor = &OperatorFleetCursor{
		CreatedAt: firstPage.Items[0].CreatedAt,
		ID:        firstPage.Items[0].ID,
	}
	secondPage, err := ListOperatorFleetRows(ctx, query)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(secondPage.Total).To(Equal(int64(2)))
	g.Expect(secondPage.Items).To(HaveLen(1))
	g.Expect(secondPage.Items[0].ID).NotTo(Equal(firstPage.Items[0].ID))

	all := append(firstPage.Items, secondPage.Items...)
	var sharedRows []types.FleetRow
	for _, item := range all {
		if item.DeploymentUnitID == shared.Unit.ID {
			sharedRows = append(sharedRows, item)
		}
	}
	g.Expect(sharedRows).To(HaveLen(1))
	g.Expect(sharedRows[0].CustomerOrganizationID).To(BeNil())
	g.Expect(sharedRows[0].Customer).To(ContainSubstring("Registry Customer"))
	g.Expect(sharedRows[0].Customer).To(ContainSubstring(", "))
	g.Expect(secondCustomerID).NotTo(Equal(first.customerOrganizationID))
}

func TestListOperatorFleetRowsProjectsEmptyPartialStaleAndUnknownObservationStates(t *testing.T) {
	ctx, _ := deploymentRegistryIsolatedPool(t, 160)
	g := NewWithT(t)
	deps := createDeploymentRegistryDependencies(t, ctx)
	decisionAt := time.Date(2026, time.July, 22, 5, 40, 0, 0, time.UTC)
	placements := make(map[string]types.DeploymentRegistryPlacement)
	for _, state := range []string{"empty", "partial", "stale", "unknown"} {
		deps.deploymentTargetID = createDeploymentRegistryTarget(
			t, ctx, deps.organizationID,
		)
		placements[state] = createDeploymentRegistryPlacement(
			t, ctx, deps, "fleet-observation-"+state, decisionAt.Add(-time.Hour),
		)
	}
	recordOperatorFleetObservation(
		t, ctx, placements["partial"], decisionAt,
		"PARTIAL", decisionAt.Add(time.Hour), "a",
	)
	recordOperatorFleetObservation(
		t, ctx, placements["stale"], decisionAt,
		"COMPLETE", decisionAt.Add(-time.Second), "b",
	)
	recordOperatorFleetObservation(
		t, ctx, placements["unknown"], decisionAt,
		"UNKNOWN", decisionAt.Add(time.Hour), "c",
	)

	result, err := ListOperatorFleetRows(ctx, OperatorFleetQuery{
		OrganizationID:         deps.organizationID,
		DecisionAt:             decisionAt,
		OrganizationWide:       true,
		CustomerScopeIDs:       []uuid.UUID{},
		EnvironmentScopeIDs:    []uuid.UUID{},
		DeploymentUnitScopeIDs: []uuid.UUID{},
		ComponentScopeIDs:      []uuid.UUID{},
		Limit:                  10,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Items).To(HaveLen(4))
	states := make(map[uuid.UUID]string, len(result.Items))
	for _, item := range result.Items {
		states[item.ID] = item.ObservedState
	}
	g.Expect(states[placements["empty"].Instances[0].ID]).To(Equal("unknown"))
	g.Expect(states[placements["partial"].Instances[0].ID]).To(Equal("partial"))
	g.Expect(states[placements["stale"].Instances[0].ID]).To(Equal("stale"))
	g.Expect(states[placements["unknown"].Instances[0].ID]).To(Equal("unknown"))
}

func TestListOperatorFleetRowsUsesLatestEffectiveOrganizationAndEnvironmentEnrollment(t *testing.T) {
	ctx, _ := deploymentRegistryIsolatedPool(t, 160)
	g := NewWithT(t)
	deps := createDeploymentRegistryDependencies(t, ctx)
	decisionAt := time.Date(2026, time.July, 22, 5, 50, 0, 0, time.UTC)
	placement := createDeploymentRegistryPlacement(
		t, ctx, deps, "fleet-enrollment", decisionAt.Add(-time.Hour),
	)
	var actorID uuid.UUID
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		INSERT INTO UserAccount (email)
		VALUES (@email)
		RETURNING id`,
		pgx.NamedArgs{"email": "fleet-enrollment-" + uuid.NewString() + "@example.test"},
	).Scan(&actorID)
	g.Expect(err).NotTo(HaveOccurred())
	insertOperatorFleetEnrollment(
		t, ctx, deps.organizationID, "organization", deps.organizationID,
		actorID, true, 1, decisionAt.Add(-time.Hour),
	)
	insertOperatorFleetEnrollment(
		t, ctx, deps.organizationID, "environment", deps.environmentID,
		actorID, true, 1, decisionAt.Add(-time.Hour),
	)

	query := OperatorFleetQuery{
		OrganizationID:         deps.organizationID,
		DecisionAt:             decisionAt,
		OrganizationWide:       true,
		CustomerScopeIDs:       []uuid.UUID{},
		EnvironmentScopeIDs:    []uuid.UUID{},
		DeploymentUnitScopeIDs: []uuid.UUID{},
		ComponentScopeIDs:      []uuid.UUID{},
		Filter: types.FleetFilter{
			DeploymentUnitID: &placement.Unit.ID,
		},
		Limit: 10,
	}
	result, err := ListOperatorFleetRows(ctx, query)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Items).To(HaveLen(1))
	g.Expect(result.Items[0].Enrollment).To(Equal("enabled"))

	insertOperatorFleetEnrollment(
		t, ctx, deps.organizationID, "environment", deps.environmentID,
		actorID, false, 2, decisionAt.Add(-time.Minute),
	)
	result, err = ListOperatorFleetRows(ctx, query)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Items).To(HaveLen(1))
	g.Expect(result.Items[0].Enrollment).To(Equal("disabled"))
}

func createOperatorFleetSharedPlacement(
	t *testing.T,
	ctx context.Context,
	deps deploymentRegistryDependencies,
	createdAt time.Time,
) (types.DeploymentRegistryPlacement, uuid.UUID) {
	t.Helper()
	g := NewWithT(t)
	secondCustomerID := createDeploymentRegistryCustomer(t, ctx, deps.organizationID)
	sharedTargetID := createDeploymentRegistryTarget(t, ctx, deps.organizationID)
	scope := types.DeploymentScope{
		OrganizationID:  deps.organizationID,
		Key:             "fleet-shared-" + uuid.NewString(),
		Name:            "Fleet Shared",
		DeliveryModel:   types.DeliveryModelShared,
		ManagementState: types.RegistryManagementStateManaged,
	}
	g.Expect(CreateDeploymentScope(ctx, &scope)).To(Succeed())
	assignment := types.TargetEnvironmentAssignment{
		OrganizationID:     deps.organizationID,
		DeploymentTargetID: sharedTargetID,
		EnvironmentID:      deps.environmentID,
		ActiveFrom:         createdAt.Add(-time.Hour),
	}
	g.Expect(CreateTargetEnvironmentAssignment(ctx, &assignment)).To(Succeed())
	subscribers := []types.DeploymentUnitSubscriber{
		{OrganizationID: deps.organizationID, CustomerOrganizationID: deps.customerOrganizationID},
		{OrganizationID: deps.organizationID, CustomerOrganizationID: secondCustomerID},
	}
	unit := types.DeploymentUnit{
		OrganizationID:                deps.organizationID,
		DeploymentScopeID:             scope.ID,
		TargetEnvironmentAssignmentID: assignment.ID,
		DeploymentTargetID:            sharedTargetID,
		Key:                           "fleet-shared-unit-" + uuid.NewString(),
		Name:                          "Fleet Shared Unit",
		PhysicalIdentity:              "compose:fleet-shared-" + uuid.NewString(),
		ManagementState:               types.RegistryManagementStateManaged,
		SubscriberSetChecksum:         deploymentregistry.SubscriberSetChecksum(subscribers),
	}
	g.Expect(CreateDeploymentUnitWithSubscribers(ctx, &unit, subscribers)).To(Succeed())
	definition := types.ComponentDefinition{
		OrganizationID:  deps.organizationID,
		Key:             "fleet-shared-component-" + uuid.NewString(),
		Name:            "Fleet Shared Component",
		ManagementState: types.RegistryManagementStateManaged,
	}
	g.Expect(CreateComponentDefinition(ctx, &definition)).To(Succeed())
	instance := types.ComponentInstance{
		OrganizationID:        deps.organizationID,
		DeploymentUnitID:      unit.ID,
		ComponentDefinitionID: definition.ID,
		PhysicalName:          "fleet-shared-service-" + uuid.NewString(),
		ManagementState:       types.RegistryManagementStateManaged,
	}
	g.Expect(CreateComponentInstance(ctx, &instance)).To(Succeed())
	return types.DeploymentRegistryPlacement{
		Scope: scope, Assignment: assignment, Unit: unit,
		Subscribers: subscribers,
		Definitions: []types.ComponentDefinition{definition},
		Instances:   []types.ComponentInstance{instance},
	}, secondCustomerID
}

func setOperatorFleetComponentCreatedAt(
	t *testing.T,
	ctx context.Context,
	componentInstanceID uuid.UUID,
	organizationID uuid.UUID,
	createdAt time.Time,
) {
	t.Helper()
	_, err := internalctx.GetDb(ctx).Exec(ctx, `
		UPDATE ComponentInstance
		SET created_at = @createdAt
		WHERE id = @id AND organization_id = @organizationID`,
		pgx.NamedArgs{
			"createdAt": createdAt, "id": componentInstanceID,
			"organizationID": organizationID,
		},
	)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
}

func recordOperatorFleetObservation(
	t *testing.T,
	ctx context.Context,
	placement types.DeploymentRegistryPlacement,
	decisionAt time.Time,
	outcome string,
	freshUntil time.Time,
	checksumCharacter string,
) {
	t.Helper()
	checksum := "sha256:" + strings.Repeat(checksumCharacter, 64)
	var observerID uuid.UUID
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		INSERT INTO ObserverRegistration (
		  organization_id, deployment_unit_id, component_instance_id,
		  observer_key, adapter_implementation, adapter_version,
		  credential_fingerprint, max_freshness_seconds,
		  max_clock_skew_seconds, measurements
		) VALUES (
		  @organizationID, @deploymentUnitID, @componentInstanceID,
		  @observerKey, 'test-observer', '1',
		  @checksum, 3600, 30, ARRAY['artifact', 'health']::text[]
		)
		RETURNING id`, pgx.NamedArgs{
		"organizationID":      placement.Unit.OrganizationID,
		"deploymentUnitID":    placement.Unit.ID,
		"componentInstanceID": placement.Instances[0].ID,
		"observerKey":         "fleet-observer-" + checksumCharacter,
		"checksum":            checksum,
	}).Scan(&observerID)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())

	_, err = internalctx.GetDb(ctx).Exec(ctx, `
		INSERT INTO ObservedComponentState (
		  organization_id, observer_id, deployment_unit_id,
		  component_instance_id, component_key, source_sequence,
		  captured_at, fresh_until, evidence_checksum,
		  artifact_digest, config_checksum, schema_version,
		  capability_checksum, platform, topology_checksum,
		  health, outcome, disposition, trusted, is_current,
		  state_checksum, runtime_state_checksum
		) VALUES (
		  @organizationID, @observerID, @deploymentUnitID,
		  @componentInstanceID, @componentKey, 1,
		  @capturedAt, @freshUntil, @checksum,
		  @checksum, @checksum, '1',
		  @checksum, 'linux/amd64', @checksum,
		  'HEALTHY', @outcome, 'ACCEPTED', TRUE, TRUE,
		  @checksum, @checksum
		)`, pgx.NamedArgs{
		"organizationID":      placement.Unit.OrganizationID,
		"observerID":          observerID,
		"deploymentUnitID":    placement.Unit.ID,
		"componentInstanceID": placement.Instances[0].ID,
		"componentKey":        placement.Definitions[0].Key,
		"capturedAt":          decisionAt.Add(-time.Minute),
		"freshUntil":          freshUntil,
		"checksum":            checksum,
		"outcome":             outcome,
	})
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
}

func insertOperatorFleetEnrollment(
	t *testing.T,
	ctx context.Context,
	organizationID uuid.UUID,
	scopeKind string,
	scopeID uuid.UUID,
	actorID uuid.UUID,
	enabled bool,
	revision int,
	effectiveFrom time.Time,
) {
	t.Helper()
	_, err := internalctx.GetDb(ctx).Exec(ctx, `
		INSERT INTO ControlPlaneEnrollment (
		  organization_id, scope_kind, scope_id, enabled,
		  effective_from, actor_useraccount_id, reason, revision
		) VALUES (
		  @organizationID, @scopeKind, @scopeID, @enabled,
		  @effectiveFrom, @actorID, @reason, @revision
		)`, pgx.NamedArgs{
		"organizationID": organizationID,
		"scopeKind":      scopeKind,
		"scopeID":        scopeID,
		"enabled":        enabled,
		"effectiveFrom":  effectiveFrom,
		"actorID":        actorID,
		"reason":         "operator fleet enrollment test",
		"revision":       revision,
	})
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
}

type operatorFleetQueryable struct {
	queryable.Queryable
	payload    string
	query      string
	arguments  []any
	queryCalls int
}

func (database *operatorFleetQueryable) QueryRow(
	_ context.Context,
	query string,
	arguments ...any,
) pgx.Row {
	database.queryCalls++
	database.query = query
	database.arguments = arguments
	return operatorFleetRow{payload: database.payload}
}

type operatorFleetRow struct {
	payload string
	err     error
}

func (row operatorFleetRow) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(destinations) != 1 {
		return errors.New("unexpected fleet scan width")
	}
	value, ok := destinations[0].(*string)
	if !ok {
		return errors.New("unexpected fleet scan destination")
	}
	*value = row.payload
	return nil
}
