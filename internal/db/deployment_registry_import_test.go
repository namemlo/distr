package db

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/deploymentregistry"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	. "github.com/onsi/gomega"
)

func TestImportCandidateChecksumReturnsMarshalError(t *testing.T) {
	g := NewWithT(t)
	checksum, err := importCandidateChecksum(make(chan int))
	g.Expect(err).To(HaveOccurred())
	g.Expect(checksum).To(BeEmpty())
}

func TestMigration140DefinesRegistryImportEvidenceAndCheckpointContract(t *testing.T) {
	g := NewWithT(t)
	up, err := os.ReadFile("../migrations/sql/140_deployment_registry_imports.up.sql")
	g.Expect(err).NotTo(HaveOccurred())
	sql := string(up)

	for _, table := range []string{
		"RegistryImport", "RegistryImportRoot", "RegistryImportPlacement", "RegistryImportDecision",
	} {
		g.Expect(sql).To(ContainSubstring("CREATE TABLE " + table))
	}
	g.Expect(sql).To(ContainSubstring(`evidence_reference TEXT NOT NULL CHECK`))
	g.Expect(sql).To(ContainSubstring(`evidence://sha256/`))
	g.Expect(sql).To(ContainSubstring(`preview_checksum TEXT NOT NULL`))
	g.Expect(sql).To(ContainSubstring(`omissions JSONB NOT NULL`))
	g.Expect(sql).To(ContainSubstring(`jsonb_typeof(omissions) = 'array'`))
	g.Expect(sql).To(ContainSubstring(`last_committed_checkpoint INTEGER NOT NULL DEFAULT 0`))
	g.Expect(sql).To(ContainSubstring(`apply_claim_id UUID`))
	g.Expect(sql).To(ContainSubstring(`apply_claimed_at TIMESTAMPTZ`))
	g.Expect(sql).To(ContainSubstring(`registryimport_apply_claim_state_check`))
	g.Expect(sql).To(ContainSubstring(`decision_ordinal INTEGER NOT NULL`))
	g.Expect(sql).To(ContainSubstring(`registryimportdecision_root_ordinal_unique`))
	g.Expect(sql).To(ContainSubstring(`actor_useraccount_id UUID NOT NULL`))
	g.Expect(sql).To(ContainSubstring(`applied_by_useraccount_id IS NOT NULL`))
	g.Expect(sql).To(ContainSubstring(`registryimportroot_id_import_organization_unique`))
	g.Expect(sql).To(ContainSubstring(
		`registry_import_root_id, registry_import_id, organization_id`,
	))
	g.Expect(sql).To(ContainSubstring(`registry_import_root_validate_org_references`))
	g.Expect(sql).To(ContainSubstring(
		`registry_import_root_id, component_key`,
	))
	g.Expect(sql).To(ContainSubstring(
		`registry_import_root_id, lower(physical_name)`,
	))
	g.Expect(sql).To(ContainSubstring(`ORGANIZATION_RETENTION`))
	g.Expect(strings.ToLower(sql)).NotTo(ContainSubstring("raw_report"))
	g.Expect(sql).To(ContainSubstring(`organization_id UUID NOT NULL REFERENCES Organization(id)`))

	down, err := os.ReadFile("../migrations/sql/140_deployment_registry_imports.down.sql")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(down)).To(ContainSubstring("downgrade crossing 140 is forbidden"))
	g.Expect(string(down)).To(ContainSubstring(
		"DROP FUNCTION registry_import_root_validate_org_references()",
	))
}

func TestRegistryImportAssignmentReuseSelectsBeforeInsertWithSerializedTarget(t *testing.T) {
	g := NewWithT(t)
	source, err := os.ReadFile("deployment_registry_import.go")
	g.Expect(err).NotTo(HaveOccurred())
	body := string(source)
	start := strings.Index(body, "func findOrCreateRegistryImportAssignment(")
	end := strings.Index(body[start:], "\nfunc registryImportActiveUnit(")
	g.Expect(start).To(BeNumerically(">=", 0))
	g.Expect(end).To(BeNumerically(">", 0))
	body = body[start : start+end]

	advisoryLock := strings.Index(body, "pg_advisory_xact_lock")
	targetLock := strings.Index(body, "FROM DeploymentTarget")
	exactReuse := strings.Index(body, "FROM TargetEnvironmentAssignment")
	insert := strings.Index(body, "INSERT INTO TargetEnvironmentAssignment")
	g.Expect(advisoryLock).To(BeNumerically(">=", 0))
	g.Expect(targetLock).To(BeNumerically(">", advisoryLock))
	g.Expect(exactReuse).To(BeNumerically(">", targetLock))
	g.Expect(insert).To(BeNumerically(">", exactReuse))
	g.Expect(body).To(ContainSubstring("FOR UPDATE"))
	g.Expect(body).NotTo(ContainSubstring("ON CONFLICT"))
}

func TestRegistryImportApplyabilityRejectsExactOmissions(t *testing.T) {
	g := NewWithT(t)
	customerID := uuid.New()
	preview := types.RegistryImportPreview{
		Counts:    types.RegistryImportCounts{OmittedPlacements: 1},
		Omissions: []string{"choice-tp-dev:worker-hidden"},
		Roots: []types.RegistryImportCandidateRoot{{
			Key: "choice-tp-dev", DeliveryModel: types.DeliveryModelDedicated,
			Classification:         types.ImportClassificationStandard,
			CustomerOrganizationID: &customerID,
			DeploymentTargetID:     uuid.New(), EnvironmentID: uuid.New(),
		}},
	}

	err := validateRegistryImportApplyability(preview)

	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
	g.Expect(err.Error()).To(ContainSubstring("omissions"))
}

func TestRegistryImportRepositoryPersistsDiagnosticsAndRejectsForeignReferences(t *testing.T) {
	ctx, _ := deploymentRegistryIsolatedPool(t, 140)
	g := NewWithT(t)
	deps := createDeploymentRegistryDependencies(t, ctx)
	actorID := createRegistryImportActor(t, ctx, deps.organizationID)
	request := registryImportTestRequest(deps, actorID, "choice-tp-dev")
	request.Roots[0].EnvironmentID = uuid.Nil
	request.SourcePlacements = []types.RegistryImportSourcePlacement{
		{RootKey: "choice-tp-dev", PhysicalName: "choice-api"},
		{RootKey: "choice-tp-dev", PhysicalName: "choice-worker-unmapped"},
	}

	preview, err := deploymentregistry.PreviewImport(ctx, request)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(preview.Diagnostics).NotTo(BeEmpty())
	g.Expect(preview.Counts.OmittedPlacements).To(Equal(1))
	g.Expect(CreateRegistryImportPreview(ctx, request, preview)).To(Succeed())

	stored, err := GetRegistryImportPreview(ctx, deps.organizationID, preview.ID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(stored.Diagnostics).To(Equal(preview.Diagnostics))
	g.Expect(stored.Omissions).To(Equal([]string{"choice-tp-dev:choice-worker-unmapped"}))
	g.Expect(stored.Roots[0].SubscriberCustomerOrganizationIDs).NotTo(BeNil())
	g.Expect(stored.Roots[0].Placements[0]).To(Equal(preview.Roots[0].Placements[0]))
	coverage, err := CoverageReport(ctx, deps.organizationID, preview.ID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(coverage.OmittedPlacements).To(Equal(1))
	g.Expect(coverage.Omissions).To(ContainElement("choice-tp-dev:choice-worker-unmapped"))
	g.Expect(coverage.Complete).To(BeFalse())

	foreign := createDeploymentRegistryDependencies(t, ctx)
	request = registryImportTestRequest(deps, actorID, "choice-tp-foreign")
	request.Roots[0].DeploymentTargetID = foreign.deploymentTargetID
	preview, err = deploymentregistry.PreviewImport(ctx, request)
	g.Expect(err).NotTo(HaveOccurred())
	err = CreateRegistryImportPreview(ctx, request, preview)
	g.Expect(errors.Is(err, apierrors.ErrBadRequest)).To(BeTrue())
}

func TestRegistryImportApplyRejectsPersistedOmissionWithoutTopologyMutation(t *testing.T) {
	ctx, _ := deploymentRegistryIsolatedPool(t, 140)
	g := NewWithT(t)
	deps := createDeploymentRegistryDependencies(t, ctx)
	actorID := createRegistryImportActor(t, ctx, deps.organizationID)
	request := registryImportTestRequest(deps, actorID, "choice-tp-dev")
	request.SourcePlacements = []types.RegistryImportSourcePlacement{
		{RootKey: "choice-tp-dev", PhysicalName: "choice-api"},
		{RootKey: "choice-tp-dev", PhysicalName: "choice-worker-unmapped"},
	}
	preview, err := deploymentregistry.PreviewImport(ctx, request)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(CreateRegistryImportPreview(ctx, request, preview)).To(Succeed())

	result, err := ApplyImport(
		ctx, deps.organizationID, preview.ID, preview.PreviewChecksum, actorID,
	)

	g.Expect(result).To(BeNil())
	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
	var scopeCount int
	g.Expect(internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT count(*) FROM DeploymentScope WHERE organization_id = @organizationID`,
		pgx.NamedArgs{"organizationID": deps.organizationID},
	).Scan(&scopeCount)).To(Succeed())
	g.Expect(scopeCount).To(Equal(0))
	var state string
	g.Expect(internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT state FROM RegistryImport
		WHERE id = @importID AND organization_id = @organizationID`,
		pgx.NamedArgs{
			"importID": preview.ID, "organizationID": deps.organizationID,
		},
	).Scan(&state)).To(Succeed())
	g.Expect(state).To(Equal("previewed"))
}

func TestRegistryImportApplyIsIdempotentAndReusesOpenAssignment(t *testing.T) {
	ctx, _ := deploymentRegistryIsolatedPool(t, 140)
	g := NewWithT(t)
	deps := createDeploymentRegistryDependencies(t, ctx)
	actorID := createRegistryImportActor(t, ctx, deps.organizationID)
	secondCustomerID := createDeploymentRegistryCustomer(t, ctx, deps.organizationID)
	request := registryImportTestRequest(deps, actorID, "choice-tp-dev")
	secondRoot := request.Roots[0]
	secondRoot.Key = "choice-tp-uat"
	secondRoot.Name = "choice-tp-uat"
	secondRoot.CustomerOrganizationID = &secondCustomerID
	secondRoot.PhysicalIdentity = "compose:choice-tp-uat"
	secondRoot.Placements = []types.RegistryImportCandidatePlacement{{
		ComponentKey: "worker", PhysicalName: "choice-worker",
	}}
	request.Roots = append(request.Roots, secondRoot)
	preview, err := deploymentregistry.PreviewImport(ctx, request)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(CreateRegistryImportPreview(ctx, request, preview)).To(Succeed())

	first, err := ApplyImport(
		ctx, deps.organizationID, preview.ID, preview.PreviewChecksum, actorID,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(first.Applied).To(BeTrue())
	replay, err := ApplyImport(
		ctx, deps.organizationID, preview.ID, preview.PreviewChecksum, actorID,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(replay.Applied).To(BeFalse())
	g.Expect(replay.State).To(Equal("applied"))

	for table, expected := range map[string]int{
		"DeploymentScope": 2, "TargetEnvironmentAssignment": 1,
		"DeploymentUnit": 2, "ComponentInstance": 2,
	} {
		var count int
		err = internalctx.GetDb(ctx).QueryRow(
			ctx,
			"SELECT count(*) FROM "+table+" WHERE organization_id = @organizationID",
			pgx.NamedArgs{"organizationID": deps.organizationID},
		).Scan(&count)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(count).To(Equal(expected), table)
	}
}

func TestRegistryImportConcurrentApplyHasSingleTopologyWriter(t *testing.T) {
	ctx, _ := deploymentRegistryIsolatedPool(t, 140)
	g := NewWithT(t)
	deps := createDeploymentRegistryDependencies(t, ctx)
	actorID := createRegistryImportActor(t, ctx, deps.organizationID)
	request := registryImportTestRequest(deps, actorID, "choice-tp-concurrent")
	preview, err := deploymentregistry.PreviewImport(ctx, request)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(CreateRegistryImportPreview(ctx, request, preview)).To(Succeed())

	start := make(chan struct{})
	type applyOutcome struct {
		result *types.RegistryImportResult
		err    error
	}
	outcomes := make(chan applyOutcome, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			result, applyErr := ApplyImport(
				ctx, deps.organizationID, preview.ID, preview.PreviewChecksum, actorID,
			)
			outcomes <- applyOutcome{result: result, err: applyErr}
		}()
	}
	close(start)
	wait.Wait()
	close(outcomes)

	appliedWriters := 0
	for outcome := range outcomes {
		if outcome.err != nil {
			g.Expect(errors.Is(outcome.err, apierrors.ErrConflict)).To(BeTrue())
			continue
		}
		if outcome.result.Applied {
			appliedWriters++
		}
	}
	g.Expect(appliedWriters).To(Equal(1))
	var scopes int
	g.Expect(internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT count(*) FROM DeploymentScope WHERE organization_id = @organizationID`,
		pgx.NamedArgs{"organizationID": deps.organizationID},
	).Scan(&scopes)).To(Succeed())
	g.Expect(scopes).To(Equal(1))
}

func TestRegistryImportCreatePlacementDoesNotRecreateRoot(t *testing.T) {
	ctx, _ := deploymentRegistryIsolatedPool(t, 140)
	g := NewWithT(t)
	deps := createDeploymentRegistryDependencies(t, ctx)
	actorID := createRegistryImportActor(t, ctx, deps.organizationID)
	request := registryImportTestRequest(deps, actorID, "choice-tp-placement")
	preview, err := deploymentregistry.PreviewImport(ctx, request)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(CreateRegistryImportPreview(ctx, request, preview)).To(Succeed())
	_, err = ApplyImport(
		ctx, deps.organizationID, preview.ID, preview.PreviewChecksum, actorID,
	)
	g.Expect(err).NotTo(HaveOccurred())

	baseline, err := RegistryImportBaseline(ctx, deps.organizationID)
	g.Expect(err).NotTo(HaveOccurred())
	request = registryImportTestRequest(deps, actorID, "choice-tp-placement")
	request.ExistingRoots = baseline
	request.Roots[0].Placements = append(
		request.Roots[0].Placements,
		types.RegistryImportCandidatePlacement{
			ComponentKey: "worker", PhysicalName: "choice-worker",
			ConfigNamespace: "choice-worker-config",
		},
	)
	preview, err = deploymentregistry.PreviewImport(ctx, request)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(preview.Diff.Creates).To(ConsistOf(HaveField("Kind", "create_placement")))
	g.Expect(CreateRegistryImportPreview(ctx, request, preview)).To(Succeed())
	_, err = ApplyImport(
		ctx, deps.organizationID, preview.ID, preview.PreviewChecksum, actorID,
	)
	g.Expect(err).NotTo(HaveOccurred())

	for table, expected := range map[string]int{
		"DeploymentScope": 1, "TargetEnvironmentAssignment": 1,
		"DeploymentUnit": 1, "ComponentInstance": 2,
	} {
		var count int
		err = internalctx.GetDb(ctx).QueryRow(
			ctx,
			"SELECT count(*) FROM "+table+" WHERE organization_id = @organizationID",
			pgx.NamedArgs{"organizationID": deps.organizationID},
		).Scan(&count)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(count).To(Equal(expected), table)
	}
}

func createRegistryImportActor(
	t *testing.T,
	ctx context.Context,
	organizationID uuid.UUID,
) uuid.UUID {
	t.Helper()
	var actorID uuid.UUID
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		INSERT INTO UserAccount (email)
		VALUES (@email)
		RETURNING id`,
		pgx.NamedArgs{"email": uuid.NewString() + "@registry-import.test"},
	).Scan(&actorID)
	if err != nil {
		t.Fatalf("create registry import actor: %v", err)
	}
	_, err = internalctx.GetDb(ctx).Exec(ctx, `
		INSERT INTO Organization_UserAccount (
			organization_id, user_account_id, user_role
		) VALUES (@organizationID, @actorID, 'vendor')`,
		pgx.NamedArgs{"organizationID": organizationID, "actorID": actorID},
	)
	if err != nil {
		t.Fatalf("assign registry import actor: %v", err)
	}
	return actorID
}

func registryImportTestRequest(
	deps deploymentRegistryDependencies,
	actorID uuid.UUID,
	rootKey string,
) types.RegistryImportRequest {
	checksum := strings.Repeat("a", 64)
	return types.RegistryImportRequest{
		OrganizationID: deps.organizationID, ActorID: actorID,
		SourceKind: "normalized-compose-inventory", ToolName: "registry-scanner",
		ToolVersion: "1.0.0", Parameters: map[string]string{},
		EvidenceReference: "evidence://sha256/" + checksum, EvidenceChecksum: checksum,
		Roots: []types.RegistryImportCandidateRoot{{
			Key: rootKey, Name: rootKey, DeliveryModel: types.DeliveryModelDedicated,
			Classification:         types.ImportClassificationStandard,
			CustomerOrganizationID: &deps.customerOrganizationID,
			DeploymentTargetID:     deps.deploymentTargetID,
			EnvironmentID:          deps.environmentID,
			PhysicalIdentity:       "compose:" + rootKey,
			Placements: []types.RegistryImportCandidatePlacement{{
				ComponentKey: "api", PhysicalName: "choice-api",
				ConfigNamespace: "choice-config", DatabaseBoundary: "choice-db",
				HealthAdapter: "choice-health",
			}},
		}},
	}
}
