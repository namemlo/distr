package db

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/deploymentregistry"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/gomega"
)

func TestMigration139DefinesDeploymentRegistryContract(t *testing.T) {
	g := NewWithT(t)
	up, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "139_deployment_registry.up.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	down, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "139_deployment_registry.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())

	upSQL := string(up)
	for _, table := range []string{
		"DeploymentScope",
		"TargetEnvironmentAssignment",
		"DeploymentUnit",
		"DeploymentUnitSubscriber",
		"ComponentDefinition",
		"ComponentAlias",
		"ComponentInstance",
		"ComponentInstanceRename",
	} {
		g.Expect(upSQL).To(ContainSubstring("CREATE TABLE " + table))
	}
	g.Expect(upSQL).To(ContainSubstring(
		"delivery_model IN ('dedicated', 'shared', 'external')",
	))
	g.Expect(upSQL).To(ContainSubstring(
		strings.Join([]string{
			"management_state IN (",
			"      'managed',",
			"      'observe_only',",
			"      'external',",
			"      'legacy_cutover',",
			"      'backup',",
			"      'retired',",
			"      'unclassified'",
			"    )",
		}, "\n"),
	))
	g.Expect(upSQL).To(ContainSubstring(
		"FOREIGN KEY (customer_organization_id, organization_id)",
	))
	g.Expect(upSQL).To(ContainSubstring(
		"FOREIGN KEY (deployment_target_id, organization_id)",
	))
	g.Expect(upSQL).To(ContainSubstring(
		"FOREIGN KEY (environment_id, organization_id)",
	))
	g.Expect(upSQL).To(ContainSubstring("subscriber_set_checksum"))
	g.Expect(upSQL).To(ContainSubstring("subscriber_set_sealed_at"))
	g.Expect(upSQL).To(ContainSubstring("DeploymentUnitSubscriber_mutation_guard"))
	g.Expect(upSQL).To(ContainSubstring("CREATE CONSTRAINT TRIGGER DeploymentUnit_subscriber_set_matches"))
	g.Expect(upSQL).To(ContainSubstring("CREATE CONSTRAINT TRIGGER DeploymentUnitSubscriber_set_matches"))
	g.Expect(upSQL).To(ContainSubstring("deployment_unit_subscriber_set_checksum"))
	g.Expect(upSQL).To(ContainSubstring("DEFERRABLE INITIALLY DEFERRED"))
	g.Expect(upSQL).To(ContainSubstring("ComponentInstanceRename_append_only"))
	g.Expect(upSQL).To(ContainSubstring("ComponentAlias_rename_history_guard"))
	g.Expect(upSQL).To(ContainSubstring(
		"'distr.deployment_registry_deletion_reason'",
	))
	g.Expect(upSQL).NotTo(ContainSubstring(
		"ORDER BY subscriber.customer_organization_id::text",
	))
	g.Expect(upSQL).To(ContainSubstring(
		"ORDER BY subscriber.customer_organization_id",
	))
	g.Expect(upSQL).To(ContainSubstring("WHERE retired_at IS NULL"))
	g.Expect(upSQL).To(ContainSubstring("TargetEnvironmentAssignment_prevent_overlap"))
	g.Expect(upSQL).To(ContainSubstring("pg_advisory_xact_lock"))
	for _, index := range []struct {
		name  string
		table string
	}{
		{name: "DeploymentScope_registry_page", table: "DeploymentScope"},
		{name: "TargetEnvironmentAssignment_registry_page", table: "TargetEnvironmentAssignment"},
		{name: "DeploymentUnit_registry_page", table: "DeploymentUnit"},
		{name: "DeploymentUnitSubscriber_registry_page", table: "DeploymentUnitSubscriber"},
		{name: "ComponentDefinition_registry_page", table: "ComponentDefinition"},
		{name: "ComponentAlias_registry_page", table: "ComponentAlias"},
		{name: "ComponentInstance_registry_page", table: "ComponentInstance"},
	} {
		g.Expect(upSQL).To(MatchRegexp(
			`(?s)CREATE INDEX ` + index.name +
				`\s+ON ` + index.table +
				`\s*\(\s*organization_id,\s*created_at DESC,\s*id DESC\s*\)`,
		))
	}
	for _, constraint := range []string{
		"name = btrim(name)",
		"physical_identity = btrim(physical_identity)",
		"alias = lower(btrim(alias))",
		"physical_name = btrim(physical_name)",
	} {
		g.Expect(upSQL).To(ContainSubstring(constraint))
	}
	for _, normalization := range []string{
		"NEW.name := btrim(NEW.name)",
		"NEW.physical_identity := btrim(NEW.physical_identity)",
		"NEW.alias := lower(btrim(NEW.alias))",
		"NEW.physical_name := btrim(NEW.physical_name)",
	} {
		g.Expect(upSQL).To(ContainSubstring(normalization))
	}
	g.Expect(string(down)).To(ContainSubstring(
		"downgrade crossing 139 is forbidden while deployment registry rows exist",
	))
	g.Expect(string(down)).To(ContainSubstring("DROP TABLE ComponentInstanceRename"))
}

func TestDeploymentRegistryPostgreSQLCIContract(t *testing.T) {
	g := NewWithT(t)
	workflow, err := os.ReadFile(filepath.Join(
		"..",
		"..",
		".github",
		"workflows",
		"community-release-hardening.yaml",
	))
	g.Expect(err).NotTo(HaveOccurred())
	contents := string(workflow)

	g.Expect(contents).To(ContainSubstring("postgres_image: postgres:16.14-alpine3.23"))
	g.Expect(contents).To(ContainSubstring("postgres_image: postgres:18.4-alpine3.23"))
	g.Expect(contents).To(ContainSubstring(
		"DISTR_TEST_DATABASE_URL: postgres://local:local@localhost:5432/distr?sslmode=disable",
	))
	g.Expect(contents).To(ContainSubstring(
		"Run deployment registry PostgreSQL contract tests",
	))
	g.Expect(contents).To(ContainSubstring(
		"go test -p=1 ./internal/db ./internal/handlers " +
			"-run 'DeploymentRegistry|Migration139' -count=1 -timeout 15m",
	))
}

func TestMigration139AppliesAfter138(t *testing.T) {
	ctx, pool := deploymentRegistryIsolatedPool(t, 138)
	g := NewWithT(t)

	var before *string
	g.Expect(pool.QueryRow(ctx, `SELECT to_regclass('deploymentscope')::text`).Scan(&before)).To(Succeed())
	g.Expect(before).To(BeNil())

	applyDeploymentRegistryMigrationFile(t, ctx, pool, "139_deployment_registry.up.sql")

	for _, table := range []string{
		"deploymentscope",
		"targetenvironmentassignment",
		"deploymentunit",
		"deploymentunitsubscriber",
		"componentdefinition",
		"componentalias",
		"componentinstance",
		"componentinstancerename",
	} {
		var actual *string
		g.Expect(pool.QueryRow(ctx, `SELECT to_regclass(@table)::text`, pgx.NamedArgs{
			"table": table,
		}).Scan(&actual)).To(Succeed())
		g.Expect(actual).NotTo(BeNil(), table)
	}
	var compositeForeignKeys int
	var allDeferredNoAction bool
	g.Expect(pool.QueryRow(ctx, `
		SELECT
		  count(*),
		  bool_and(constraint_row.condeferrable)
		    AND bool_and(NOT constraint_row.condeferred)
		    AND bool_and(constraint_row.confdeltype = 'a')
		    AND bool_and(constraint_row.confupdtype = 'a')
		FROM pg_constraint constraint_row
		JOIN pg_class constrained_table
		  ON constrained_table.oid = constraint_row.conrelid
		WHERE constraint_row.contype = 'f'
		  AND cardinality(constraint_row.conkey) > 1
		  AND constrained_table.relname IN (
		    'deploymentscope',
		    'targetenvironmentassignment',
		    'deploymentunit',
		    'deploymentunitsubscriber',
		    'componentalias',
		    'componentinstance',
		    'componentinstancerename'
		  )`,
	).Scan(&compositeForeignKeys, &allDeferredNoAction)).To(Succeed())
	g.Expect(compositeForeignKeys).To(Equal(13))
	g.Expect(allDeferredNoAction).To(BeTrue())
}

func TestMigration139DownRefusesWithRegistryRowsAndSucceedsEmpty(t *testing.T) {
	t.Run("refuses while rows exist", func(t *testing.T) {
		ctx, pool := deploymentRegistryIsolatedPool(t, 139)
		g := NewWithT(t)
		orgID := createDeploymentRegistryOrganization(t, ctx)
		customerID := createDeploymentRegistryCustomer(t, ctx, orgID)
		_, err := pool.Exec(ctx, `
			INSERT INTO DeploymentScope (
				organization_id,
				customer_organization_id,
				key,
				name,
				delivery_model,
				management_state
			) VALUES (
				@organizationID,
				@customerOrganizationID,
				'dedicated-scope',
				'Dedicated scope',
				'dedicated',
				'managed'
			)`,
			pgx.NamedArgs{
				"organizationID":         orgID,
				"customerOrganizationID": customerID,
			},
		)
		g.Expect(err).NotTo(HaveOccurred())

		down, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "139_deployment_registry.down.sql"))
		g.Expect(err).NotTo(HaveOccurred())
		_, err = pool.Exec(ctx, string(down))
		g.Expect(err).To(MatchError(ContainSubstring(
			"downgrade crossing 139 is forbidden while deployment registry rows exist",
		)))

		var table *string
		g.Expect(pool.QueryRow(ctx, `SELECT to_regclass('deploymentscope')::text`).Scan(&table)).To(Succeed())
		g.Expect(table).NotTo(BeNil())
	})

	t.Run("succeeds when empty", func(t *testing.T) {
		ctx, pool := deploymentRegistryIsolatedPool(t, 139)
		g := NewWithT(t)

		applyDeploymentRegistryMigrationFile(t, ctx, pool, "139_deployment_registry.down.sql")

		for _, table := range []string{
			"componentinstancerename",
			"componentinstance",
			"componentalias",
			"componentdefinition",
			"deploymentunitsubscriber",
			"deploymentunit",
			"targetenvironmentassignment",
			"deploymentscope",
		} {
			var actual *string
			g.Expect(pool.QueryRow(ctx, `SELECT to_regclass(@table)::text`, pgx.NamedArgs{
				"table": table,
			}).Scan(&actual)).To(Succeed())
			g.Expect(actual).To(BeNil(), table)
		}
	})
}

func TestDeploymentRegistryRepositoryCreatesCompletePlacement(t *testing.T) {
	ctx := deploymentRegistryDBTestContext(t)
	g := NewWithT(t)
	deps := createDeploymentRegistryDependencies(t, ctx)

	placement := createDeploymentRegistryPlacement(t, ctx, deps, "primary", time.Now().UTC())
	actual, err := GetDeploymentRegistryPlacement(ctx, deps.organizationID, placement.Unit.ID)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(actual.Scope.ID).To(Equal(placement.Scope.ID))
	g.Expect(actual.Assignment.ID).To(Equal(placement.Assignment.ID))
	g.Expect(actual.Unit.ID).To(Equal(placement.Unit.ID))
	g.Expect(actual.Definitions).To(ConsistOf(placement.Definitions))
	g.Expect(actual.Instances).To(ConsistOf(placement.Instances))
	g.Expect(deploymentregistry.ValidateDeploymentRegistryPlacement(*actual)).To(BeEmpty())
}

func TestDeploymentRegistryPlacementReadUsesOneRepeatableReadSnapshot(t *testing.T) {
	ctx, pool := deploymentRegistryIsolatedPool(t, 139)
	g := NewWithT(t)
	deps := createDeploymentRegistryDependencies(t, ctx)
	placement := createDeploymentRegistryPlacement(
		t,
		ctx,
		deps,
		"snapshot",
		time.Now().UTC(),
	)
	originalDefinitionName := placement.Definitions[0].Name

	blockerConnection, err := pool.Acquire(ctx)
	g.Expect(err).NotTo(HaveOccurred())
	defer blockerConnection.Release()
	blockerTransaction, err := blockerConnection.Begin(ctx)
	g.Expect(err).NotTo(HaveOccurred())
	blockerReleased := false
	defer func() {
		if !blockerReleased {
			_ = blockerTransaction.Rollback(context.Background())
		}
	}()
	_, err = blockerTransaction.Exec(
		ctx,
		`LOCK TABLE ComponentInstance IN ACCESS EXCLUSIVE MODE`,
	)
	g.Expect(err).NotTo(HaveOccurred())

	readerConnection, err := pool.Acquire(ctx)
	g.Expect(err).NotTo(HaveOccurred())
	defer readerConnection.Release()
	var readerBackendPID int32
	g.Expect(readerConnection.QueryRow(ctx, `SELECT pg_backend_pid()`).
		Scan(&readerBackendPID)).To(Succeed())

	type placementReadResult struct {
		placement *types.DeploymentRegistryPlacement
		err       error
	}
	readContext, cancelRead := context.WithTimeout(ctx, 10*time.Second)
	defer cancelRead()
	results := make(chan placementReadResult, 1)
	go func() {
		page, readErr := ListDeploymentRegistryPlacements(
			internalctx.WithDb(readContext, readerConnection),
			types.RegistryListFilter{
				OrganizationID: deps.organizationID,
				Limit:          100,
			},
		)
		var actual *types.DeploymentRegistryPlacement
		if readErr == nil {
			for index := range page.Items {
				if page.Items[index].Unit.ID == placement.Unit.ID {
					actual = &page.Items[index]
					break
				}
			}
			if actual == nil {
				readErr = errors.New("snapshot placement is missing from list")
			}
		}
		results <- placementReadResult{placement: actual, err: readErr}
	}()

	waitContext, cancelWait := context.WithTimeout(ctx, 5*time.Second)
	defer cancelWait()
	waitTicker := time.NewTicker(10 * time.Millisecond)
	defer waitTicker.Stop()
	for {
		select {
		case result := <-results:
			t.Fatalf("placement read completed before relation lock: %v", result.err)
		default:
		}
		var waitingOnRelationLock bool
		err = pool.QueryRow(waitContext, `
			SELECT EXISTS (
			  SELECT 1
			  FROM pg_stat_activity
			  WHERE pid = @readerBackendPID
			    AND wait_event_type = 'Lock'
			    AND query LIKE '%FROM ComponentInstance ci%'
			)`,
			pgx.NamedArgs{"readerBackendPID": readerBackendPID},
		).Scan(&waitingOnRelationLock)
		g.Expect(err).NotTo(HaveOccurred())
		if waitingOnRelationLock {
			break
		}
		select {
		case <-waitContext.Done():
			t.Fatalf("placement reader did not reach relation lock: %v", waitContext.Err())
		case <-waitTicker.C:
		}
	}

	_, err = pool.Exec(ctx, `
		UPDATE ComponentDefinition
		SET name = @name,
		    updated_at = now()
		WHERE id = @id
		  AND organization_id = @organizationID`,
		pgx.NamedArgs{
			"name":           "Definition updated between aggregate reads",
			"id":             placement.Definitions[0].ID,
			"organizationID": deps.organizationID,
		},
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(blockerTransaction.Commit(ctx)).To(Succeed())
	blockerReleased = true

	var result placementReadResult
	select {
	case result = <-results:
	case <-readContext.Done():
		t.Fatalf("placement read did not finish: %v", readContext.Err())
	}
	g.Expect(result.err).NotTo(HaveOccurred())
	g.Expect(result.placement).NotTo(BeNil())
	g.Expect(result.placement.Definitions).To(HaveLen(1))
	g.Expect(result.placement.Definitions[0].Name).To(Equal(originalDefinitionName))
}

func TestDeploymentRegistryPlacementListUsesAtMostEightQueriesAtMaximumLimit(t *testing.T) {
	ctx, pool := deploymentRegistryIsolatedPool(t, 139)
	g := NewWithT(t)
	deps := createDeploymentRegistryDependencies(t, ctx)
	base := time.Date(2026, time.July, 18, 9, 0, 0, 0, time.UTC)
	for index := 0; index < 5; index++ {
		if index > 0 {
			deps.deploymentTargetID = createDeploymentRegistryTarget(
				t,
				ctx,
				deps.organizationID,
			)
		}
		createDeploymentRegistryPlacement(
			t,
			ctx,
			deps,
			"batch-"+strconv.Itoa(index),
			base.Add(time.Duration(index)*time.Minute),
		)
	}

	counting := &deploymentRegistryQueryCountingPool{Pool: pool}
	page, err := ListDeploymentRegistryPlacements(
		internalctx.WithDb(ctx, counting),
		types.RegistryListFilter{
			OrganizationID: deps.organizationID,
			Limit:          100,
		},
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(page.Items).To(HaveLen(5))
	g.Expect(counting.queries.Load()).To(BeNumerically("<=", 8))
	for _, placement := range page.Items {
		g.Expect(placement.Assignments).NotTo(BeEmpty())
		g.Expect(placement.Units).NotTo(BeEmpty())
		g.Expect(placement.Definitions).To(HaveLen(1))
		g.Expect(placement.Instances).To(HaveLen(1))
	}
}

func TestDeploymentRegistryRepositoryCreatesImmutableSharedSubscriberSetAtomically(t *testing.T) {
	ctx := deploymentRegistryDBTestContext(t)
	g := NewWithT(t)
	deps := createDeploymentRegistryDependencies(t, ctx)
	secondCustomerID := createDeploymentRegistryCustomer(t, ctx, deps.organizationID)
	scope := types.DeploymentScope{
		OrganizationID:  deps.organizationID,
		Key:             "shared",
		Name:            "Shared",
		DeliveryModel:   types.DeliveryModelShared,
		ManagementState: types.RegistryManagementStateManaged,
	}
	g.Expect(CreateDeploymentScope(ctx, &scope)).To(Succeed())
	assignment := types.TargetEnvironmentAssignment{
		OrganizationID:     deps.organizationID,
		DeploymentTargetID: deps.deploymentTargetID,
		EnvironmentID:      deps.environmentID,
		ActiveFrom:         time.Now().UTC().Add(-time.Hour),
	}
	g.Expect(CreateTargetEnvironmentAssignment(ctx, &assignment)).To(Succeed())
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
	unit := types.DeploymentUnit{
		OrganizationID:                deps.organizationID,
		DeploymentScopeID:             scope.ID,
		TargetEnvironmentAssignmentID: assignment.ID,
		DeploymentTargetID:            deps.deploymentTargetID,
		Key:                           "shared-unit",
		Name:                          "Shared unit",
		PhysicalIdentity:              "compose:shared",
		ManagementState:               types.RegistryManagementStateManaged,
		SubscriberSetChecksum:         deploymentregistry.SubscriberSetChecksum(subscribers),
	}
	g.Expect(CreateDeploymentUnitWithSubscribers(ctx, &unit, subscribers)).To(Succeed())
	g.Expect(subscribers[0].ID).NotTo(Equal(uuid.Nil))
	g.Expect(subscribers[1].ID).NotTo(Equal(uuid.Nil))

	var databaseChecksum string
	g.Expect(internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT deployment_unit_subscriber_set_checksum(
		  @organizationID,
		  @deploymentUnitID
		)`,
		pgx.NamedArgs{
			"organizationID":   deps.organizationID,
			"deploymentUnitID": unit.ID,
		},
	).Scan(&databaseChecksum)).To(Succeed())
	g.Expect(databaseChecksum).To(Equal(unit.SubscriberSetChecksum))

	_, err := internalctx.GetDb(ctx).Exec(ctx, `
		INSERT INTO DeploymentUnit (
		  organization_id,
		  deployment_scope_id,
		  target_environment_assignment_id,
		  deployment_target_id,
		  key,
		  name,
		  physical_identity,
		  management_state,
		  subscriber_set_checksum
		) VALUES (
		  @organizationID,
		  @deploymentScopeID,
		  @targetEnvironmentAssignmentID,
		  @deploymentTargetID,
		  'unsealed-shared-unit',
		  'Unsealed shared unit',
		  'compose:unsealed-shared',
		  'managed',
		  @subscriberSetChecksum
		)`,
		pgx.NamedArgs{
			"organizationID":                deps.organizationID,
			"deploymentScopeID":             scope.ID,
			"targetEnvironmentAssignmentID": assignment.ID,
			"deploymentTargetID":            deps.deploymentTargetID,
			"subscriberSetChecksum":         unit.SubscriberSetChecksum,
		},
	)
	g.Expect(err).To(MatchError(ContainSubstring(
		"requires atomically sealed subscriber initialization",
	)))

	first := subscribers[0]
	first.RetiredAt = new(time.Time)
	g.Expect(UpdateDeploymentUnitSubscriber(ctx, &first)).To(MatchError(ContainSubstring(
		"subscriber set is immutable",
	)))
	g.Expect(DeleteDeploymentUnitSubscriber(ctx, deps.organizationID, subscribers[0].ID)).
		To(MatchError(ContainSubstring("subscriber set is immutable")))

	thirdCustomerID := createDeploymentRegistryCustomer(t, ctx, deps.organizationID)
	_, err = internalctx.GetDb(ctx).Exec(ctx, `
		INSERT INTO DeploymentUnitSubscriber (
		  organization_id,
		  deployment_unit_id,
		  customer_organization_id
		) VALUES (
		  @organizationID,
		  @deploymentUnitID,
		  @customerOrganizationID
		)`,
		pgx.NamedArgs{
			"organizationID":         deps.organizationID,
			"deploymentUnitID":       unit.ID,
			"customerOrganizationID": thirdCustomerID,
		},
	)
	g.Expect(err).To(MatchError(ContainSubstring(
		"subscriber set is immutable after atomic initialization",
	)))
	_, err = internalctx.GetDb(ctx).Exec(ctx, `
		UPDATE DeploymentUnitSubscriber
		SET retired_at = now()
		WHERE id = @id
		  AND organization_id = @organizationID`,
		pgx.NamedArgs{
			"id":             subscribers[0].ID,
			"organizationID": deps.organizationID,
		},
	)
	g.Expect(err).To(MatchError(ContainSubstring(
		"subscriber set is immutable after atomic initialization",
	)))
	_, err = internalctx.GetDb(ctx).Exec(ctx, `
		DELETE FROM DeploymentUnitSubscriber
		WHERE id = @id
		  AND organization_id = @organizationID`,
		pgx.NamedArgs{
			"id":             subscribers[0].ID,
			"organizationID": deps.organizationID,
		},
	)
	g.Expect(err).To(MatchError(ContainSubstring(
		"subscriber set is immutable after atomic initialization",
	)))
	wrongMarkerTx, err := internalctx.GetDb(ctx).Begin(ctx)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = wrongMarkerTx.Exec(ctx, `
		SELECT set_config(
		  'distr.deployment_registry_deletion_reason',
		  'OPERATOR_DELETE',
		  true
		)`)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = wrongMarkerTx.Exec(ctx, `
		DELETE FROM DeploymentUnitSubscriber
		WHERE id = @id
		  AND organization_id = @organizationID`,
		pgx.NamedArgs{
			"id":             subscribers[0].ID,
			"organizationID": deps.organizationID,
		},
	)
	g.Expect(err).To(MatchError(ContainSubstring(
		"subscriber set is immutable after atomic initialization",
	)))
	g.Expect(wrongMarkerTx.Rollback(ctx)).To(Succeed())

	var activeCount int
	g.Expect(internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT count(*)
		FROM DeploymentUnitSubscriber
		WHERE organization_id = @organizationID
		  AND deployment_unit_id = @deploymentUnitID
		  AND retired_at IS NULL`,
		pgx.NamedArgs{
			"organizationID":   deps.organizationID,
			"deploymentUnitID": unit.ID,
		},
	).Scan(&activeCount)).To(Succeed())
	g.Expect(activeCount).To(Equal(2))
}

func TestDeploymentRegistryRepositoryRejectsCrossOrganizationReferences(t *testing.T) {
	ctx := deploymentRegistryDBTestContext(t)
	g := NewWithT(t)
	first := createDeploymentRegistryDependencies(t, ctx)
	second := createDeploymentRegistryDependencies(t, ctx)

	scope := types.DeploymentScope{
		OrganizationID:         first.organizationID,
		CustomerOrganizationID: &second.customerOrganizationID,
		Key:                    "foreign-customer",
		Name:                   "Foreign customer",
		DeliveryModel:          types.DeliveryModelDedicated,
		ManagementState:        types.RegistryManagementStateManaged,
	}
	g.Expect(CreateDeploymentScope(ctx, &scope)).To(MatchError(apierrors.ErrNotFound))

	scope.CustomerOrganizationID = &first.customerOrganizationID
	g.Expect(CreateDeploymentScope(ctx, &scope)).To(Succeed())

	assignment := types.TargetEnvironmentAssignment{
		OrganizationID:     first.organizationID,
		DeploymentTargetID: second.deploymentTargetID,
		EnvironmentID:      first.environmentID,
		ActiveFrom:         time.Now().UTC(),
	}
	g.Expect(CreateTargetEnvironmentAssignment(ctx, &assignment)).To(MatchError(apierrors.ErrNotFound))
	assignment.DeploymentTargetID = first.deploymentTargetID
	assignment.EnvironmentID = second.environmentID
	g.Expect(CreateTargetEnvironmentAssignment(ctx, &assignment)).To(MatchError(apierrors.ErrNotFound))
	assignment.EnvironmentID = first.environmentID
	g.Expect(CreateTargetEnvironmentAssignment(ctx, &assignment)).To(Succeed())

	unit := types.DeploymentUnit{
		OrganizationID:                first.organizationID,
		DeploymentScopeID:             scope.ID,
		TargetEnvironmentAssignmentID: assignment.ID,
		DeploymentTargetID:            second.deploymentTargetID,
		Key:                           "foreign-target",
		Name:                          "Foreign target",
		PhysicalIdentity:              "compose:foreign-target",
		ManagementState:               types.RegistryManagementStateManaged,
		SubscriberSetChecksum:         deploymentregistry.SubscriberSetChecksum(nil),
	}
	g.Expect(CreateDeploymentUnit(ctx, &unit)).To(MatchError(apierrors.ErrNotFound))
	unit.DeploymentTargetID = first.deploymentTargetID
	g.Expect(CreateDeploymentUnit(ctx, &unit)).To(Succeed())

	subscriber := types.DeploymentUnitSubscriber{
		OrganizationID:         first.organizationID,
		DeploymentUnitID:       unit.ID,
		CustomerOrganizationID: second.customerOrganizationID,
	}
	g.Expect(CreateDeploymentUnitSubscriber(ctx, &subscriber)).To(MatchError(apierrors.ErrNotFound))

	definition := types.ComponentDefinition{
		OrganizationID:  first.organizationID,
		Key:             "example-api",
		Name:            "Example API",
		ManagementState: types.RegistryManagementStateManaged,
	}
	g.Expect(CreateComponentDefinition(ctx, &definition)).To(Succeed())

	foreignAlias := types.ComponentAlias{
		OrganizationID:        second.organizationID,
		ComponentDefinitionID: definition.ID,
		Alias:                 "legacy-api",
	}
	g.Expect(CreateComponentAlias(ctx, &foreignAlias)).To(MatchError(apierrors.ErrNotFound))

	foreignDefinition := types.ComponentDefinition{
		OrganizationID:  second.organizationID,
		Key:             "foreign-api",
		Name:            "Foreign API",
		ManagementState: types.RegistryManagementStateManaged,
	}
	g.Expect(CreateComponentDefinition(ctx, &foreignDefinition)).To(Succeed())
	instance := types.ComponentInstance{
		OrganizationID:        first.organizationID,
		DeploymentUnitID:      unit.ID,
		ComponentDefinitionID: foreignDefinition.ID,
		PhysicalName:          "foreign-api",
		ManagementState:       types.RegistryManagementStateManaged,
	}
	g.Expect(CreateComponentInstance(ctx, &instance)).To(MatchError(apierrors.ErrNotFound))

	_, err := GetDeploymentRegistryPlacement(ctx, first.organizationID, uuid.New())
	g.Expect(err).To(MatchError(apierrors.ErrNotFound))
	_, err = GetDeploymentRegistryPlacement(ctx, second.organizationID, unit.ID)
	g.Expect(err).To(MatchError(apierrors.ErrNotFound))
}

func TestDeploymentRegistryRepositoryUsesOpaqueBoundedKeysetPagination(t *testing.T) {
	ctx := deploymentRegistryDBTestContext(t)
	g := NewWithT(t)
	deps := createDeploymentRegistryDependencies(t, ctx)
	base := time.Date(2026, time.July, 18, 0, 0, 0, 0, time.UTC)

	first := createDeploymentRegistryPlacement(t, ctx, deps, "first", base.Add(time.Minute))
	secondDeps := deps
	secondDeps.deploymentTargetID = createDeploymentRegistryTarget(t, ctx, deps.organizationID)
	second := createDeploymentRegistryPlacement(
		t,
		ctx,
		secondDeps,
		"second",
		base.Add(2*time.Minute),
	)
	thirdDeps := deps
	thirdDeps.deploymentTargetID = createDeploymentRegistryTarget(t, ctx, deps.organizationID)
	third := createDeploymentRegistryPlacement(
		t,
		ctx,
		thirdDeps,
		"third",
		base.Add(3*time.Minute),
	)

	pageOne, err := ListDeploymentRegistryPlacements(ctx, types.RegistryListFilter{
		OrganizationID: deps.organizationID,
		Limit:          2,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(pageOne.Items).To(HaveLen(2))
	g.Expect(pageOne.NextCursor).NotTo(BeEmpty())
	g.Expect([]uuid.UUID{pageOne.Items[0].Unit.ID, pageOne.Items[1].Unit.ID}).To(Equal(
		[]uuid.UUID{third.Unit.ID, second.Unit.ID},
	))

	pageTwo, err := ListDeploymentRegistryPlacements(ctx, types.RegistryListFilter{
		OrganizationID: deps.organizationID,
		Cursor:         pageOne.NextCursor,
		Limit:          2,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(pageTwo.Items).To(HaveLen(1))
	g.Expect(pageTwo.Items[0].Unit.ID).To(Equal(first.Unit.ID))
	g.Expect(pageTwo.NextCursor).To(BeEmpty())

	_, err = ListDeploymentRegistryPlacements(ctx, types.RegistryListFilter{
		OrganizationID: deps.organizationID,
		Cursor:         "not-an-opaque-cursor",
	})
	g.Expect(errors.Is(err, apierrors.ErrBadRequest)).To(BeTrue())
	_, err = ListDeploymentRegistryPlacements(ctx, types.RegistryListFilter{
		OrganizationID: deps.organizationID,
		Limit:          101,
	})
	g.Expect(errors.Is(err, apierrors.ErrBadRequest)).To(BeTrue())
}

func TestDeploymentRegistryRepositoryCRUDIsOrganizationScopedAndProtectsHistory(t *testing.T) {
	ctx := deploymentRegistryDBTestContext(t)
	g := NewWithT(t)
	first := createDeploymentRegistryDependencies(t, ctx)
	second := createDeploymentRegistryDependencies(t, ctx)
	placement := createDeploymentRegistryPlacement(t, ctx, first, "crud", time.Now().UTC())

	scope, err := GetDeploymentScope(ctx, first.organizationID, placement.Scope.ID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(scope.ID).To(Equal(placement.Scope.ID))
	_, err = GetDeploymentScope(ctx, second.organizationID, placement.Scope.ID)
	g.Expect(err).To(MatchError(apierrors.ErrNotFound))

	scope.Name = "Renamed display label"
	g.Expect(UpdateDeploymentScope(ctx, scope)).To(Succeed())
	g.Expect(scope.Name).To(Equal("Renamed display label"))
	g.Expect(scope.Key).To(Equal("scope-crud"))

	page, err := ListDeploymentScopes(ctx, types.RegistryListFilter{
		OrganizationID: first.organizationID,
		Limit:          1,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(page.Items).To(HaveLen(1))
	g.Expect(page.Items[0].OrganizationID).To(Equal(first.organizationID))

	g.Expect(DeleteDeploymentScope(ctx, first.organizationID, placement.Scope.ID)).
		To(MatchError(apierrors.ErrConflict))
	stillPresent, err := GetDeploymentScope(ctx, first.organizationID, placement.Scope.ID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(stillPresent.ID).To(Equal(placement.Scope.ID))
}

func TestDeploymentRegistryRenameHistoryIsAppendOnlyAndProtectsEvidence(t *testing.T) {
	ctx := deploymentRegistryDBTestContext(t)
	g := NewWithT(t)
	deps := createDeploymentRegistryDependencies(t, ctx)
	placement := createDeploymentRegistryPlacement(t, ctx, deps, "rename-history", time.Now().UTC())
	instance := placement.Instances[0]

	firstAlias := types.ComponentAlias{
		OrganizationID:        deps.organizationID,
		ComponentDefinitionID: instance.ComponentDefinitionID,
		Alias:                 instance.PhysicalName,
	}
	g.Expect(CreateComponentAlias(ctx, &firstAlias)).To(Succeed())
	firstFrom := instance.PhysicalName
	instance.PhysicalName = "service-renamed-once"
	instance.RenamedFrom = firstFrom
	g.Expect(UpdateComponentInstance(ctx, &instance)).To(Succeed())

	secondAlias := types.ComponentAlias{
		OrganizationID:        deps.organizationID,
		ComponentDefinitionID: instance.ComponentDefinitionID,
		Alias:                 instance.PhysicalName,
	}
	g.Expect(CreateComponentAlias(ctx, &secondAlias)).To(Succeed())
	secondFrom := instance.PhysicalName
	instance.PhysicalName = "service-renamed-twice"
	instance.RenamedFrom = secondFrom
	g.Expect(UpdateComponentInstance(ctx, &instance)).To(Succeed())

	type renameHop struct {
		From string `db:"from_physical_name"`
		To   string `db:"to_physical_name"`
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT from_physical_name, to_physical_name
		FROM ComponentInstanceRename
		WHERE organization_id = @organizationID
		  AND component_instance_id = @componentInstanceID
		ORDER BY created_at, id`,
		pgx.NamedArgs{
			"organizationID":      deps.organizationID,
			"componentInstanceID": instance.ID,
		},
	)
	g.Expect(err).NotTo(HaveOccurred())
	hops, err := pgx.CollectRows(rows, pgx.RowToStructByName[renameHop])
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(hops).To(Equal([]renameHop{
		{From: firstFrom, To: "service-renamed-once"},
		{From: secondFrom, To: "service-renamed-twice"},
	}))

	importedDefinition := types.ComponentDefinition{
		OrganizationID:  deps.organizationID,
		Key:             "imported-rename-definition",
		Name:            "Imported rename definition",
		ManagementState: types.RegistryManagementStateManaged,
	}
	g.Expect(CreateComponentDefinition(ctx, &importedDefinition)).To(Succeed())
	importedAlias := types.ComponentAlias{
		OrganizationID:        deps.organizationID,
		ComponentDefinitionID: importedDefinition.ID,
		Alias:                 "imported-service-old",
	}
	g.Expect(CreateComponentAlias(ctx, &importedAlias)).To(Succeed())
	importedInstance := types.ComponentInstance{
		OrganizationID:        deps.organizationID,
		DeploymentUnitID:      placement.Unit.ID,
		ComponentDefinitionID: importedDefinition.ID,
		PhysicalName:          "imported-service-new",
		ManagementState:       types.RegistryManagementStateManaged,
		RenamedFrom:           importedAlias.Alias,
	}
	g.Expect(CreateComponentInstance(ctx, &importedInstance)).To(Succeed())
	var importedHistoryCount int
	g.Expect(internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT count(*)
		FROM ComponentInstanceRename
		WHERE organization_id = @organizationID
		  AND component_instance_id = @componentInstanceID
		  AND component_alias_id = @componentAliasID
		  AND from_physical_name = @fromPhysicalName
		  AND to_physical_name = @toPhysicalName`,
		pgx.NamedArgs{
			"organizationID":      deps.organizationID,
			"componentInstanceID": importedInstance.ID,
			"componentAliasID":    importedAlias.ID,
			"fromPhysicalName":    importedAlias.Alias,
			"toPhysicalName":      importedInstance.PhysicalName,
		},
	).Scan(&importedHistoryCount)).To(Succeed())
	g.Expect(importedHistoryCount).To(Equal(1))

	retirement := time.Now().UTC()
	firstAlias.RetiredAt = &retirement
	g.Expect(UpdateComponentAlias(ctx, &firstAlias)).To(MatchError(apierrors.ErrConflict))
	g.Expect(DeleteComponentAlias(ctx, deps.organizationID, firstAlias.ID)).
		To(MatchError(apierrors.ErrConflict))
	g.Expect(DeleteComponentInstance(ctx, deps.organizationID, instance.ID)).
		To(MatchError(apierrors.ErrConflict))

	_, err = internalctx.GetDb(ctx).Exec(ctx, `
		UPDATE ComponentAlias
		SET retired_at = now()
		WHERE id = @id AND organization_id = @organizationID`,
		pgx.NamedArgs{"id": secondAlias.ID, "organizationID": deps.organizationID},
	)
	expectDeploymentRegistryPostgreSQLCode(t, err, pgerrcode.CheckViolation)
	_, err = internalctx.GetDb(ctx).Exec(ctx, `
		UPDATE ComponentInstanceRename
		SET from_physical_name = 'rewritten'
		WHERE component_instance_id = @id AND organization_id = @organizationID`,
		pgx.NamedArgs{"id": instance.ID, "organizationID": deps.organizationID},
	)
	expectDeploymentRegistryPostgreSQLCode(t, err, pgerrcode.CheckViolation)
	_, err = internalctx.GetDb(ctx).Exec(ctx, `
		DELETE FROM ComponentInstanceRename
		WHERE component_instance_id = @id AND organization_id = @organizationID`,
		pgx.NamedArgs{"id": instance.ID, "organizationID": deps.organizationID},
	)
	expectDeploymentRegistryPostgreSQLCode(t, err, pgerrcode.CheckViolation)
}

func TestDeploymentRegistryConcurrentRenamesSerializeOnCurrentPhysicalName(t *testing.T) {
	ctx, pool := deploymentRegistryIsolatedPool(t, 139)
	g := NewWithT(t)
	deps := createDeploymentRegistryDependencies(t, ctx)
	placement := createDeploymentRegistryPlacement(t, ctx, deps, "concurrent-rename", time.Now().UTC())
	original := placement.Instances[0]
	alias := types.ComponentAlias{
		OrganizationID:        deps.organizationID,
		ComponentDefinitionID: original.ComponentDefinitionID,
		Alias:                 original.PhysicalName,
	}
	g.Expect(CreateComponentAlias(ctx, &alias)).To(Succeed())

	blocker, err := pool.Begin(ctx)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = blocker.Exec(ctx, `
		SELECT id FROM ComponentInstance
		WHERE id = @id AND organization_id = @organizationID
		FOR UPDATE`,
		pgx.NamedArgs{"id": original.ID, "organizationID": deps.organizationID},
	)
	g.Expect(err).NotTo(HaveOccurred())
	blockerOpen := true
	defer func() {
		if blockerOpen {
			_ = blocker.Rollback(context.Background())
		}
	}()

	firstConnection, err := pool.Acquire(ctx)
	g.Expect(err).NotTo(HaveOccurred())
	defer firstConnection.Release()
	secondConnection, err := pool.Acquire(ctx)
	g.Expect(err).NotTo(HaveOccurred())
	defer secondConnection.Release()
	pids := []int32{
		deploymentRegistryBackendPID(t, ctx, firstConnection),
		deploymentRegistryBackendPID(t, ctx, secondConnection),
	}
	results := make(chan error, 2)
	for index, connection := range []*pgxpool.Conn{firstConnection, secondConnection} {
		candidate := original
		candidate.PhysicalName = "concurrent-name-" + strconv.Itoa(index)
		candidate.RenamedFrom = original.PhysicalName
		go func(conn *pgxpool.Conn, value types.ComponentInstance) {
			results <- UpdateComponentInstance(internalctx.WithDb(ctx, conn), &value)
		}(connection, candidate)
	}
	waitForDeploymentRegistryLockWaits(t, ctx, pool, pids, 2)
	g.Expect(blocker.Commit(ctx)).To(Succeed())
	blockerOpen = false

	successes := 0
	failures := 0
	for range 2 {
		if result := <-results; result == nil {
			successes++
		} else {
			failures++
		}
	}
	g.Expect(successes).To(Equal(1))
	g.Expect(failures).To(Equal(1))
	var historyCount int
	g.Expect(pool.QueryRow(ctx, `
		SELECT count(*) FROM ComponentInstanceRename
		WHERE organization_id = @organizationID
		  AND component_instance_id = @componentInstanceID`,
		pgx.NamedArgs{
			"organizationID":      deps.organizationID,
			"componentInstanceID": original.ID,
		},
	).Scan(&historyCount)).To(Succeed())
	g.Expect(historyCount).To(Equal(1))
}

func TestDeploymentRegistryRenameRacesWithAliasRetirementAndDeletion(t *testing.T) {
	for _, mutation := range []string{"retire", "delete"} {
		t.Run(mutation, func(t *testing.T) {
			ctx, pool := deploymentRegistryIsolatedPool(t, 139)
			g := NewWithT(t)
			deps := createDeploymentRegistryDependencies(t, ctx)
			placement := createDeploymentRegistryPlacement(
				t,
				ctx,
				deps,
				"alias-race-"+mutation,
				time.Now().UTC(),
			)
			instance := placement.Instances[0]
			alias := types.ComponentAlias{
				OrganizationID:        deps.organizationID,
				ComponentDefinitionID: instance.ComponentDefinitionID,
				Alias:                 instance.PhysicalName,
			}
			g.Expect(CreateComponentAlias(ctx, &alias)).To(Succeed())

			blocker, err := pool.Begin(ctx)
			g.Expect(err).NotTo(HaveOccurred())
			_, err = blocker.Exec(ctx, `
				SELECT id FROM ComponentAlias
				WHERE id = @id AND organization_id = @organizationID
				FOR UPDATE`,
				pgx.NamedArgs{"id": alias.ID, "organizationID": deps.organizationID},
			)
			g.Expect(err).NotTo(HaveOccurred())
			blockerOpen := true
			defer func() {
				if blockerOpen {
					_ = blocker.Rollback(context.Background())
				}
			}()

			renameConnection, err := pool.Acquire(ctx)
			g.Expect(err).NotTo(HaveOccurred())
			defer renameConnection.Release()
			mutationConnection, err := pool.Acquire(ctx)
			g.Expect(err).NotTo(HaveOccurred())
			defer mutationConnection.Release()
			renamePID := deploymentRegistryBackendPID(t, ctx, renameConnection)
			mutationPID := deploymentRegistryBackendPID(t, ctx, mutationConnection)
			renameResults := make(chan error, 1)
			mutationResults := make(chan error, 1)
			renamed := instance
			renamed.PhysicalName += "-renamed"
			renamed.RenamedFrom = instance.PhysicalName
			go func() {
				renameResults <- UpdateComponentInstance(
					internalctx.WithDb(ctx, renameConnection),
					&renamed,
				)
			}()
			waitForDeploymentRegistryLockWaits(
				t,
				ctx,
				pool,
				[]int32{renamePID},
				1,
			)
			go func() {
				mutationContext := internalctx.WithDb(ctx, mutationConnection)
				if mutation == "retire" {
					retired := alias
					retiredAt := time.Now().UTC()
					retired.RetiredAt = &retiredAt
					mutationResults <- UpdateComponentAlias(mutationContext, &retired)
					return
				}
				mutationResults <- DeleteComponentAlias(
					mutationContext,
					deps.organizationID,
					alias.ID,
				)
			}()
			waitForDeploymentRegistryLockWaits(
				t,
				ctx,
				pool,
				[]int32{renamePID, mutationPID},
				2,
			)
			g.Expect(blocker.Commit(ctx)).To(Succeed())
			blockerOpen = false

			g.Expect(<-renameResults).To(Succeed())
			g.Expect(<-mutationResults).To(MatchError(apierrors.ErrConflict))
			var invalidState bool
			g.Expect(pool.QueryRow(ctx, `
				SELECT EXISTS (
				  SELECT 1
				  FROM ComponentInstanceRename history
				  LEFT JOIN ComponentAlias alias
				    ON alias.id = history.component_alias_id
				   AND alias.organization_id = history.organization_id
				  WHERE history.organization_id = @organizationID
				    AND history.component_instance_id = @componentInstanceID
				    AND (alias.id IS NULL OR alias.retired_at IS NOT NULL)
				)`,
				pgx.NamedArgs{
					"organizationID":      deps.organizationID,
					"componentInstanceID": instance.ID,
				},
			).Scan(&invalidState)).To(Succeed())
			g.Expect(invalidState).To(BeFalse())
		})
	}
}

func TestDeploymentRegistryOrganizationRetentionPurgesSealedSharedRegistry(t *testing.T) {
	ctx, pool := deploymentRegistryIsolatedPool(t, 139)
	g := NewWithT(t)
	deps := createDeploymentRegistryDependencies(t, ctx)
	secondCustomerID := createDeploymentRegistryCustomer(t, ctx, deps.organizationID)
	scope := types.DeploymentScope{
		OrganizationID:  deps.organizationID,
		Key:             "retention-shared",
		Name:            "Retention shared",
		DeliveryModel:   types.DeliveryModelShared,
		ManagementState: types.RegistryManagementStateManaged,
	}
	g.Expect(CreateDeploymentScope(ctx, &scope)).To(Succeed())
	assignment := types.TargetEnvironmentAssignment{
		OrganizationID:     deps.organizationID,
		DeploymentTargetID: deps.deploymentTargetID,
		EnvironmentID:      deps.environmentID,
		ActiveFrom:         time.Now().UTC().Add(-time.Hour),
	}
	g.Expect(CreateTargetEnvironmentAssignment(ctx, &assignment)).To(Succeed())
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
	unit := types.DeploymentUnit{
		OrganizationID:                deps.organizationID,
		DeploymentScopeID:             scope.ID,
		TargetEnvironmentAssignmentID: assignment.ID,
		DeploymentTargetID:            deps.deploymentTargetID,
		Key:                           "retention-shared-unit",
		Name:                          "Retention shared unit",
		PhysicalIdentity:              "compose:retention-shared",
		ManagementState:               types.RegistryManagementStateManaged,
		SubscriberSetChecksum:         deploymentregistry.SubscriberSetChecksum(subscribers),
	}
	g.Expect(CreateDeploymentUnitWithSubscribers(ctx, &unit, subscribers)).To(Succeed())
	definition := types.ComponentDefinition{
		OrganizationID:  deps.organizationID,
		Key:             "retention-api",
		Name:            "Retention API",
		ManagementState: types.RegistryManagementStateManaged,
	}
	g.Expect(CreateComponentDefinition(ctx, &definition)).To(Succeed())
	instance := types.ComponentInstance{
		OrganizationID:        deps.organizationID,
		DeploymentUnitID:      unit.ID,
		ComponentDefinitionID: definition.ID,
		PhysicalName:          "retention-api-old",
		ManagementState:       types.RegistryManagementStateManaged,
	}
	g.Expect(CreateComponentInstance(ctx, &instance)).To(Succeed())
	alias := types.ComponentAlias{
		OrganizationID:        deps.organizationID,
		ComponentDefinitionID: definition.ID,
		Alias:                 instance.PhysicalName,
	}
	g.Expect(CreateComponentAlias(ctx, &alias)).To(Succeed())
	instance.RenamedFrom = instance.PhysicalName
	instance.PhysicalName = "retention-api"
	g.Expect(UpdateComponentInstance(ctx, &instance)).To(Succeed())
	_, err := pool.Exec(ctx, `
		UPDATE Organization
		SET deleted_at = now() - interval '2 hours'
		WHERE id = @organizationID`,
		pgx.NamedArgs{"organizationID": deps.organizationID},
	)
	g.Expect(err).NotTo(HaveOccurred())

	deleted, err := DeleteOrganizationsOlderThan(ctx, time.Hour)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(deleted).To(Equal(int64(1)))
	var organizationCount, registryCount, historyCount int
	g.Expect(pool.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM Organization WHERE id = @organizationID),
		  (SELECT count(*) FROM DeploymentUnit WHERE organization_id = @organizationID),
		  (SELECT count(*) FROM ComponentInstanceRename WHERE organization_id = @organizationID)`,
		pgx.NamedArgs{"organizationID": deps.organizationID},
	).Scan(&organizationCount, &registryCount, &historyCount)).To(Succeed())
	g.Expect([]int{organizationCount, registryCount, historyCount}).To(Equal([]int{0, 0, 0}))
}

func TestDeploymentRegistrySubscriberChecksumUsesNativeUUIDOrderAcrossTextCollations(
	t *testing.T,
) {
	ctx := deploymentRegistryDBTestContext(t)
	g := NewWithT(t)
	deps := createDeploymentRegistryDependencies(t, ctx)
	secondCustomerID := createDeploymentRegistryCustomer(t, ctx, deps.organizationID)
	scope := types.DeploymentScope{
		OrganizationID:  deps.organizationID,
		Key:             "checksum-collation",
		Name:            "Checksum collation",
		DeliveryModel:   types.DeliveryModelShared,
		ManagementState: types.RegistryManagementStateManaged,
	}
	g.Expect(CreateDeploymentScope(ctx, &scope)).To(Succeed())
	assignment := types.TargetEnvironmentAssignment{
		OrganizationID:     deps.organizationID,
		DeploymentTargetID: deps.deploymentTargetID,
		EnvironmentID:      deps.environmentID,
		ActiveFrom:         time.Now().UTC().Add(-time.Hour),
	}
	g.Expect(CreateTargetEnvironmentAssignment(ctx, &assignment)).To(Succeed())
	subscribers := []types.DeploymentUnitSubscriber{
		{
			OrganizationID:         deps.organizationID,
			CustomerOrganizationID: secondCustomerID,
		},
		{
			OrganizationID:         deps.organizationID,
			CustomerOrganizationID: deps.customerOrganizationID,
		},
	}
	unit := types.DeploymentUnit{
		OrganizationID:                deps.organizationID,
		DeploymentScopeID:             scope.ID,
		TargetEnvironmentAssignmentID: assignment.ID,
		DeploymentTargetID:            deps.deploymentTargetID,
		Key:                           "checksum-collation-unit",
		Name:                          "Checksum collation unit",
		PhysicalIdentity:              "compose:checksum-collation",
		ManagementState:               types.RegistryManagementStateManaged,
		SubscriberSetChecksum:         deploymentregistry.SubscriberSetChecksum(subscribers),
	}
	g.Expect(CreateDeploymentUnitWithSubscribers(ctx, &unit, subscribers)).To(Succeed())

	_, err := internalctx.GetDb(ctx).Exec(ctx, `
		CREATE COLLATION deployment_registry_non_default_text_order (
		  provider = libc,
		  locale = 'C'
		)`)
	g.Expect(err).NotTo(HaveOccurred())
	var uuidHasNoCollation bool
	var databaseChecksum string
	var explicitTextOrder []string
	g.Expect(internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT
		  (SELECT typcollation = 0 FROM pg_type WHERE typname = 'uuid'),
		  deployment_unit_subscriber_set_checksum(
		    @organizationID,
		    @deploymentUnitID
		  ),
		  (
		    SELECT array_agg(
		      subscriber.customer_organization_id::text
		      ORDER BY subscriber.customer_organization_id::text
		        COLLATE deployment_registry_non_default_text_order
		    )
		    FROM DeploymentUnitSubscriber subscriber
		    WHERE subscriber.organization_id = @organizationID
		      AND subscriber.deployment_unit_id = @deploymentUnitID
		  )`,
		pgx.NamedArgs{
			"organizationID":   deps.organizationID,
			"deploymentUnitID": unit.ID,
		},
	).Scan(
		&uuidHasNoCollation,
		&databaseChecksum,
		&explicitTextOrder,
	)).To(Succeed())
	g.Expect(uuidHasNoCollation).To(BeTrue())
	g.Expect(explicitTextOrder).To(HaveLen(2))
	g.Expect(databaseChecksum).To(Equal(deploymentregistry.SubscriberSetChecksum(subscribers)))
}

func TestDeploymentRegistryUpdateReturnsNotFoundWhenRowDisappears(t *testing.T) {
	ctx, pool := deploymentRegistryIsolatedPool(t, 139)
	g := NewWithT(t)
	deps := createDeploymentRegistryDependencies(t, ctx)
	missing := types.ComponentDefinition{
		ID:              uuid.New(),
		OrganizationID:  deps.organizationID,
		Key:             "missing-definition",
		Name:            "Missing definition",
		ManagementState: types.RegistryManagementStateManaged,
	}
	g.Expect(UpdateComponentDefinition(ctx, &missing)).To(MatchError(apierrors.ErrNotFound))

	definition := missing
	definition.ID = uuid.Nil
	definition.Key = "concurrent-definition"
	definition.Name = "Concurrent definition"
	g.Expect(CreateComponentDefinition(ctx, &definition)).To(Succeed())
	deleteConnection, err := pool.Acquire(ctx)
	g.Expect(err).NotTo(HaveOccurred())
	defer deleteConnection.Release()
	deleteTx, err := deleteConnection.Begin(ctx)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = deleteTx.Exec(ctx, `
		DELETE FROM ComponentDefinition
		WHERE id = @id AND organization_id = @organizationID`,
		pgx.NamedArgs{"id": definition.ID, "organizationID": deps.organizationID},
	)
	g.Expect(err).NotTo(HaveOccurred())
	deleteOpen := true
	defer func() {
		if deleteOpen {
			_ = deleteTx.Rollback(context.Background())
		}
	}()

	updateConnection, err := pool.Acquire(ctx)
	g.Expect(err).NotTo(HaveOccurred())
	defer updateConnection.Release()
	updatePID := deploymentRegistryBackendPID(t, ctx, updateConnection)
	results := make(chan error, 1)
	definition.Name = "Concurrent update"
	go func() {
		results <- UpdateComponentDefinition(
			internalctx.WithDb(ctx, updateConnection),
			&definition,
		)
	}()
	waitForDeploymentRegistryLockWaits(t, ctx, pool, []int32{updatePID}, 1)
	g.Expect(deleteTx.Commit(ctx)).To(Succeed())
	deleteOpen = false
	g.Expect(<-results).To(MatchError(apierrors.ErrNotFound))
}

func TestDeploymentRegistrySchemaNormalizesDirectSQLIdentityValues(t *testing.T) {
	ctx := deploymentRegistryDBTestContext(t)
	g := NewWithT(t)
	deps := createDeploymentRegistryDependencies(t, ctx)
	placement := createDeploymentRegistryPlacement(t, ctx, deps, "canonical", time.Now().UTC())
	alias := types.ComponentAlias{
		OrganizationID:        deps.organizationID,
		ComponentDefinitionID: placement.Definitions[0].ID,
		Alias:                 "Legacy-Service",
	}
	g.Expect(CreateComponentAlias(ctx, &alias)).To(Succeed())
	g.Expect(alias.Alias).To(Equal("legacy-service"))

	var scopeName string
	g.Expect(internalctx.GetDb(ctx).QueryRow(ctx, `
		UPDATE DeploymentScope
		SET name = ' ' || name || ' '
		WHERE id = @id AND organization_id = @organizationID
		RETURNING name`,
		pgx.NamedArgs{
			"id":             placement.Scope.ID,
			"organizationID": deps.organizationID,
		},
	).Scan(&scopeName)).To(Succeed())
	g.Expect(scopeName).To(Equal(placement.Scope.Name))

	var unitName, physicalIdentity string
	g.Expect(internalctx.GetDb(ctx).QueryRow(ctx, `
		UPDATE DeploymentUnit
		SET name = ' ' || name || ' ',
		    physical_identity = ' ' || physical_identity || ' '
		WHERE id = @id AND organization_id = @organizationID
		RETURNING name, physical_identity`,
		pgx.NamedArgs{
			"id":             placement.Unit.ID,
			"organizationID": deps.organizationID,
		},
	).Scan(&unitName, &physicalIdentity)).To(Succeed())
	g.Expect(unitName).To(Equal(placement.Unit.Name))
	g.Expect(physicalIdentity).To(Equal(placement.Unit.PhysicalIdentity))

	var definitionName string
	g.Expect(internalctx.GetDb(ctx).QueryRow(ctx, `
		UPDATE ComponentDefinition
		SET name = ' ' || name || ' '
		WHERE id = @id AND organization_id = @organizationID
		RETURNING name`,
		pgx.NamedArgs{
			"id":             placement.Definitions[0].ID,
			"organizationID": deps.organizationID,
		},
	).Scan(&definitionName)).To(Succeed())
	g.Expect(definitionName).To(Equal(placement.Definitions[0].Name))

	var physicalName string
	g.Expect(internalctx.GetDb(ctx).QueryRow(ctx, `
		UPDATE ComponentInstance
		SET physical_name = ' ' || physical_name || ' '
		WHERE id = @id AND organization_id = @organizationID
		RETURNING physical_name`,
		pgx.NamedArgs{
			"id":             placement.Instances[0].ID,
			"organizationID": deps.organizationID,
		},
	).Scan(&physicalName)).To(Succeed())
	g.Expect(physicalName).To(Equal(placement.Instances[0].PhysicalName))

	var directAlias string
	g.Expect(internalctx.GetDb(ctx).QueryRow(ctx, `
		INSERT INTO ComponentAlias (
		  organization_id,
		  component_definition_id,
		  alias
		) VALUES (
		  @organizationID,
		  @componentDefinitionID,
		  ' OTHER-ALIAS '
		)
		RETURNING alias`,
		pgx.NamedArgs{
			"organizationID":        deps.organizationID,
			"componentDefinitionID": placement.Definitions[0].ID,
		},
	).Scan(&directAlias)).To(Succeed())
	g.Expect(directAlias).To(Equal("other-alias"))

	_, err := internalctx.GetDb(ctx).Exec(ctx, `
		UPDATE DeploymentScope
		SET name = '   '
		WHERE id = @id AND organization_id = @organizationID`,
		pgx.NamedArgs{
			"id":             placement.Scope.ID,
			"organizationID": deps.organizationID,
		},
	)
	expectDeploymentRegistryPostgreSQLCode(t, err, pgerrcode.CheckViolation)

	_, err = internalctx.GetDb(ctx).Exec(ctx, `
		INSERT INTO DeploymentUnit (
		  organization_id,
		  deployment_scope_id,
		  target_environment_assignment_id,
		  deployment_target_id,
		  key,
		  name,
		  physical_identity,
		  management_state,
		  subscriber_set_checksum,
		  subscriber_set_sealed_at
		) VALUES (
		  @organizationID,
		  @deploymentScopeID,
		  @targetEnvironmentAssignmentID,
		  @deploymentTargetID,
		  'case-duplicate-unit',
		  ' Case duplicate unit ',
		  ' ' || upper(@physicalIdentity) || ' ',
		  'managed',
		  @subscriberSetChecksum,
		  now()
		)`,
		pgx.NamedArgs{
			"organizationID":                deps.organizationID,
			"deploymentScopeID":             placement.Scope.ID,
			"targetEnvironmentAssignmentID": placement.Assignment.ID,
			"deploymentTargetID":            deps.deploymentTargetID,
			"physicalIdentity":              placement.Unit.PhysicalIdentity,
			"subscriberSetChecksum":         deploymentregistry.SubscriberSetChecksum(nil),
		},
	)
	expectDeploymentRegistryPostgreSQLCode(t, err, pgerrcode.UniqueViolation)

	secondDefinition := types.ComponentDefinition{
		OrganizationID:  deps.organizationID,
		Key:             "case-duplicate-definition",
		Name:            "Case duplicate definition",
		ManagementState: types.RegistryManagementStateManaged,
	}
	g.Expect(CreateComponentDefinition(ctx, &secondDefinition)).To(Succeed())
	_, err = internalctx.GetDb(ctx).Exec(ctx, `
		INSERT INTO ComponentInstance (
		  organization_id,
		  deployment_unit_id,
		  component_definition_id,
		  physical_name,
		  management_state
		) VALUES (
		  @organizationID,
		  @deploymentUnitID,
		  @componentDefinitionID,
		  ' ' || upper(@physicalName) || ' ',
		  'managed'
		)`,
		pgx.NamedArgs{
			"organizationID":        deps.organizationID,
			"deploymentUnitID":      placement.Unit.ID,
			"componentDefinitionID": secondDefinition.ID,
			"physicalName":          placement.Instances[0].PhysicalName,
		},
	)
	expectDeploymentRegistryPostgreSQLCode(t, err, pgerrcode.UniqueViolation)

	_, err = internalctx.GetDb(ctx).Exec(ctx, `
		INSERT INTO ComponentAlias (
		  organization_id,
		  component_definition_id,
		  alias
		) VALUES (
		  @organizationID,
		  @componentDefinitionID,
		  ' LEGACY-SERVICE '
		)`,
		pgx.NamedArgs{
			"organizationID":        deps.organizationID,
			"componentDefinitionID": placement.Definitions[0].ID,
		},
	)
	expectDeploymentRegistryPostgreSQLCode(t, err, pgerrcode.UniqueViolation)
}

func expectDeploymentRegistryPostgreSQLCode(t *testing.T, err error, code string) {
	t.Helper()
	g := NewWithT(t)
	g.Expect(err).To(HaveOccurred())
	var postgresError *pgconn.PgError
	g.Expect(errors.As(err, &postgresError)).To(BeTrue())
	g.Expect(postgresError.Code).To(Equal(code))
}

type deploymentRegistryDependencies struct {
	organizationID         uuid.UUID
	customerOrganizationID uuid.UUID
	environmentID          uuid.UUID
	deploymentTargetID     uuid.UUID
}

func createDeploymentRegistryPlacement(
	t *testing.T,
	ctx context.Context,
	deps deploymentRegistryDependencies,
	suffix string,
	createdAt time.Time,
) types.DeploymentRegistryPlacement {
	t.Helper()
	g := NewWithT(t)
	scope := types.DeploymentScope{
		OrganizationID:         deps.organizationID,
		CustomerOrganizationID: &deps.customerOrganizationID,
		Key:                    "scope-" + suffix,
		Name:                   "Scope " + suffix,
		DeliveryModel:          types.DeliveryModelDedicated,
		ManagementState:        types.RegistryManagementStateManaged,
	}
	g.Expect(CreateDeploymentScope(ctx, &scope)).To(Succeed())
	assignment := types.TargetEnvironmentAssignment{
		OrganizationID:     deps.organizationID,
		DeploymentTargetID: deps.deploymentTargetID,
		EnvironmentID:      deps.environmentID,
		ActiveFrom:         createdAt.Add(-time.Hour),
	}
	g.Expect(CreateTargetEnvironmentAssignment(ctx, &assignment)).To(Succeed())
	unit := types.DeploymentUnit{
		OrganizationID:                deps.organizationID,
		DeploymentScopeID:             scope.ID,
		TargetEnvironmentAssignmentID: assignment.ID,
		DeploymentTargetID:            deps.deploymentTargetID,
		Key:                           "unit-" + suffix,
		Name:                          "Unit " + suffix,
		PhysicalIdentity:              "compose:" + suffix,
		ManagementState:               types.RegistryManagementStateManaged,
		SubscriberSetChecksum:         deploymentregistry.SubscriberSetChecksum(nil),
	}
	g.Expect(CreateDeploymentUnit(ctx, &unit)).To(Succeed())
	_, err := internalctx.GetDb(ctx).Exec(ctx,
		`UPDATE DeploymentUnit
		 SET created_at = @createdAt, updated_at = @createdAt
		 WHERE id = @id AND organization_id = @organizationID`,
		pgx.NamedArgs{
			"createdAt":      createdAt,
			"id":             unit.ID,
			"organizationID": deps.organizationID,
		},
	)
	g.Expect(err).NotTo(HaveOccurred())
	unit.CreatedAt = createdAt
	unit.UpdatedAt = createdAt

	definition := types.ComponentDefinition{
		OrganizationID:  deps.organizationID,
		Key:             "component-" + suffix,
		Name:            "Component " + suffix,
		ManagementState: types.RegistryManagementStateManaged,
	}
	g.Expect(CreateComponentDefinition(ctx, &definition)).To(Succeed())
	instance := types.ComponentInstance{
		OrganizationID:        deps.organizationID,
		DeploymentUnitID:      unit.ID,
		ComponentDefinitionID: definition.ID,
		PhysicalName:          "service-" + suffix,
		ManagementState:       types.RegistryManagementStateManaged,
	}
	g.Expect(CreateComponentInstance(ctx, &instance)).To(Succeed())
	return types.DeploymentRegistryPlacement{
		EffectiveAt: createdAt,
		Scope:       scope,
		Assignment:  assignment,
		Assignments: []types.TargetEnvironmentAssignment{assignment},
		Unit:        unit,
		Units:       []types.DeploymentUnit{unit},
		Definitions: []types.ComponentDefinition{definition},
		Instances:   []types.ComponentInstance{instance},
	}
}

func createDeploymentRegistryDependencies(
	t *testing.T,
	ctx context.Context,
) deploymentRegistryDependencies {
	t.Helper()
	organizationID := createDeploymentRegistryOrganization(t, ctx)
	return deploymentRegistryDependencies{
		organizationID:         organizationID,
		customerOrganizationID: createDeploymentRegistryCustomer(t, ctx, organizationID),
		environmentID:          createDeploymentRegistryEnvironment(t, ctx, organizationID),
		deploymentTargetID:     createDeploymentRegistryTarget(t, ctx, organizationID),
	}
}

func createDeploymentRegistryOrganization(t *testing.T, ctx context.Context) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := internalctx.GetDb(ctx).QueryRow(ctx,
		`INSERT INTO Organization (name) VALUES (@name) RETURNING id`,
		pgx.NamedArgs{"name": "Registry Organization " + uuid.NewString()},
	).Scan(&id); err != nil {
		t.Fatalf("create deployment registry organization: %v", err)
	}
	return id
}

func createDeploymentRegistryCustomer(
	t *testing.T,
	ctx context.Context,
	organizationID uuid.UUID,
) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := internalctx.GetDb(ctx).QueryRow(ctx,
		`INSERT INTO CustomerOrganization (organization_id, name)
		 VALUES (@organizationID, @name)
		 RETURNING id`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"name":           "Registry Customer " + uuid.NewString(),
		},
	).Scan(&id); err != nil {
		t.Fatalf("create deployment registry customer: %v", err)
	}
	return id
}

func createDeploymentRegistryEnvironment(
	t *testing.T,
	ctx context.Context,
	organizationID uuid.UUID,
) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := internalctx.GetDb(ctx).QueryRow(ctx,
		`INSERT INTO Environment (organization_id, name)
		 VALUES (@organizationID, @name)
		 RETURNING id`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"name":           "Registry Environment " + uuid.NewString(),
		},
	).Scan(&id); err != nil {
		t.Fatalf("create deployment registry environment: %v", err)
	}
	return id
}

func createDeploymentRegistryTarget(
	t *testing.T,
	ctx context.Context,
	organizationID uuid.UUID,
) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := internalctx.GetDb(ctx).QueryRow(ctx,
		`INSERT INTO DeploymentTarget (
			name, type, organization_id, agent_version_id
		) VALUES (
			@name, 'docker', @organizationID, (SELECT id FROM AgentVersion LIMIT 1)
		)
		RETURNING id`,
		pgx.NamedArgs{
			"name":           "Registry Target " + uuid.NewString(),
			"organizationID": organizationID,
		},
	).Scan(&id); err != nil {
		t.Fatalf("create deployment registry target: %v", err)
	}
	return id
}

func deploymentRegistryDBTestContext(t *testing.T) context.Context {
	t.Helper()
	ctx, pool := deploymentRegistryIsolatedPool(t, 139)
	g := NewWithT(t)
	g.Expect(internalctx.GetDb(ctx)).To(Equal(pool))
	return ctx
}

type deploymentRegistryQueryCountingPool struct {
	*pgxpool.Pool
	queries atomic.Int64
}

func (pool *deploymentRegistryQueryCountingPool) BeginTx(
	ctx context.Context,
	options pgx.TxOptions,
) (pgx.Tx, error) {
	tx, err := pool.Pool.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return &deploymentRegistryQueryCountingTx{
		Tx:      tx,
		queries: &pool.queries,
	}, nil
}

type deploymentRegistryQueryCountingTx struct {
	pgx.Tx
	queries *atomic.Int64
}

func (tx *deploymentRegistryQueryCountingTx) Query(
	ctx context.Context,
	sql string,
	args ...any,
) (pgx.Rows, error) {
	tx.queries.Add(1)
	return tx.Tx.Query(ctx, sql, args...)
}

func (tx *deploymentRegistryQueryCountingTx) QueryRow(
	ctx context.Context,
	sql string,
	args ...any,
) pgx.Row {
	tx.queries.Add(1)
	return tx.Tx.QueryRow(ctx, sql, args...)
}

func deploymentRegistryBackendPID(
	t *testing.T,
	ctx context.Context,
	connection *pgxpool.Conn,
) int32 {
	t.Helper()
	var pid int32
	NewWithT(t).Expect(connection.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid)).To(Succeed())
	return pid
}

func waitForDeploymentRegistryLockWaits(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	backendPIDs []int32,
	expected int,
) {
	t.Helper()
	waitContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting int
		err := pool.QueryRow(waitContext, `
			SELECT count(*)
			FROM pg_stat_activity
			WHERE pid = ANY(@backendPIDs)
			  AND wait_event_type = 'Lock'`,
			pgx.NamedArgs{"backendPIDs": backendPIDs},
		).Scan(&waiting)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
		if waiting >= expected {
			return
		}
		select {
		case <-waitContext.Done():
			t.Fatalf(
				"timed out waiting for %d deployment registry lock waits; saw %d",
				expected,
				waiting,
			)
		case <-ticker.C:
		}
	}
}

func deploymentRegistryIsolatedPool(
	t *testing.T,
	targetVersion int,
) (context.Context, *pgxpool.Pool) {
	t.Helper()
	databaseURL := os.Getenv("DISTR_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DISTR_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	adminPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to deployment registry test database: %v", err)
	}
	t.Cleanup(adminPool.Close)

	schema := "deployment_registry_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatalf("create deployment registry test schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := adminPool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+quotedSchema+" CASCADE"); err != nil {
			t.Logf("drop deployment registry test schema: %v", err)
		}
	})

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse deployment registry test database url: %v", err)
	}
	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET search_path TO "+quotedSchema)
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("connect to deployment registry isolated schema: %v", err)
	}
	t.Cleanup(pool.Close)

	files, err := filepath.Glob(filepath.Join("..", "migrations", "sql", "*.up.sql"))
	if err != nil {
		t.Fatalf("list deployment registry migration files: %v", err)
	}
	sort.Slice(files, func(i, j int) bool {
		return deploymentRegistryMigrationVersion(t, files[i]) <
			deploymentRegistryMigrationVersion(t, files[j])
	})
	for _, file := range files {
		if deploymentRegistryMigrationVersion(t, file) > targetVersion {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read deployment registry migration %s: %v", file, err)
		}
		if _, err := pool.Exec(ctx, string(data)); err != nil {
			t.Fatalf("run deployment registry migration %s: %v", file, err)
		}
	}
	return internalctx.WithDb(ctx, pool), pool
}

func deploymentRegistryMigrationVersion(t *testing.T, file string) int {
	t.Helper()
	version, err := strconv.Atoi(strings.SplitN(filepath.Base(file), "_", 2)[0])
	if err != nil {
		t.Fatalf("parse deployment registry migration version %s: %v", file, err)
	}
	return version
}

func applyDeploymentRegistryMigrationFile(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	name string,
) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "migrations", "sql", name))
	if err != nil {
		t.Fatalf("read deployment registry migration %s: %v", name, err)
	}
	if _, err := pool.Exec(ctx, string(data)); err != nil {
		t.Fatalf("run deployment registry migration %s: %v", name, err)
	}
}
