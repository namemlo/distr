package db

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db/queryable"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	. "github.com/onsi/gomega"
)

func TestTargetRequirementsFromGraphPreservesEverySymbolicTargetRequirement(t *testing.T) {
	g := NewWithT(t)
	graph := types.ProductReleaseGraph{Edges: []types.GraphEdge{
		{
			Key: "product", From: "component:provider", To: "component:consumer",
			Capability: "cache", ResolutionStage: types.CapabilityResolutionStageProduct,
		},
		{
			Key: "target-b", From: "target:consumer:email", To: "component:consumer",
			Capability: "email", VersionRange: "^2.0.0",
			ResolutionStage: types.CapabilityResolutionStageTarget,
			AllowedModes: []types.RequirementResolutionMode{
				types.RequirementResolutionModeApprovedExternal,
			},
		},
		{
			Key: "target-a", From: "target:consumer:cache", To: "component:consumer",
			Capability: "cache", VersionRange: "^1.0.0",
			ResolutionStage: types.CapabilityResolutionStageTarget,
			AllowedModes: []types.RequirementResolutionMode{
				types.RequirementResolutionModeIncluded,
			},
		},
	}}

	requirements := targetRequirementsFromGraph(graph)

	g.Expect(requirements).To(HaveLen(2))
	g.Expect(requirements[0].Key).To(Equal("target:consumer:cache"))
	g.Expect(requirements[1].Key).To(Equal("target:consumer:email"))
}

func TestProjectTargetPlanStepsFreezesDependencies(t *testing.T) {
	g := NewWithT(t)
	graph := types.TargetPlanGraph{
		Steps: []types.TargetPlanStep{
			{StepKey: "a", Name: "A", InputBindings: []byte(`{}`), V1Compatible: true},
			{StepKey: "b", Name: "B", InputBindings: []byte(`{}`), V1Compatible: true},
		},
		Edges: []types.DeploymentPlanStepEdge{
			{Key: "a->b", FromStepKey: "a", ToStepKey: "b"},
		},
	}

	steps := projectTargetPlanSteps(graph)

	g.Expect(steps).To(HaveLen(2))
	g.Expect(steps[1].Dependencies).To(Equal([]string{"a"}))
	g.Expect(steps[0].ID).NotTo(Equal(uuid.Nil))
}

func TestAdapterScopeBindingsUsePersistedDatabaseAndObserverBoundaries(t *testing.T) {
	g := NewWithT(t)
	instanceID := uuid.New()
	observerID := uuid.New()
	pins := []types.ComponentReleasePin{{
		ComponentKey: "catalog",
		Migrations: []types.MigrationDeclaration{{
			Key: "schema-v2",
		}},
		AdapterRequirements: []types.AdapterRequirement{
			{StepKind: "deploy", Capability: "distr.compose.deploy", Version: "1.0.0"},
			{StepKind: "migration", Capability: "database.migrate", Version: "1.0.0"},
			{StepKind: "health", Capability: "health.observe", Version: "1.0.0"},
		},
	}}
	bindings := adapterScopeBindingsFromPlacement(
		pins,
		[]types.ConfigComponentBinding{{
			ComponentKey: "catalog", ComponentInstanceID: instanceID,
		}},
		[]types.ComponentInstance{{
			ID: instanceID, DatabaseBoundary: "postgres:catalog",
		}},
		[]types.ObserverRegistration{{
			ID: observerID, ComponentInstanceID: &instanceID, Enabled: true,
		}},
	)

	g.Expect(bindings).To(Equal([]types.AdapterStepScopeBinding{
		{
			StepKey:        "component:catalog:health",
			ScopeType:      types.AdapterScopeObserverRegistration,
			ScopeReference: observerID.String(),
		},
		{
			StepKey:        "component:catalog:migration:schema-v2",
			ScopeType:      types.AdapterScopeDatabaseResource,
			ScopeReference: "postgres:catalog",
		},
	}))
}

func TestMigration145HasTenantFencesImmutabilityAndRollbackRefusal(t *testing.T) {
	g := NewWithT(t)
	root := filepath.Join("..", "migrations", "sql")
	up, err := os.ReadFile(filepath.Join(root, "145_target_deployment_plan_v2.up.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	down, err := os.ReadFile(filepath.Join(root, "145_target_deployment_plan_v2.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	upText := string(up)
	downText := string(down)

	for _, table := range []string{
		"CREATE TABLE DeploymentPlanDraft",
		"CREATE TABLE DeploymentPlanDraftAuditEvent",
		"CREATE TABLE DeploymentPlanResolvedRequirement",
		"CREATE TABLE DeploymentPlanStepEdge",
	} {
		g.Expect(upText).To(ContainSubstring(table))
	}
	for _, column := range []string{
		"plan_schema", "draft_id", "deployment_unit_id",
		"target_config_snapshot_id", "protocol_version",
		"supersedes_deployment_plan_id", "supersede_reason",
		"created_by_user_account_id", "updated_by_user_account_id",
		"published_by_user_account_id", "sealed_at",
	} {
		g.Expect(upText).To(ContainSubstring(column))
	}
	g.Expect(upText).To(ContainSubstring("FOREIGN KEY (draft_id, organization_id)"))
	g.Expect(upText).To(ContainSubstring("DeploymentPlanDraft_publication_guard"))
	g.Expect(upText).To(ContainSubstring("DeploymentPlan_v2_immutable_guard"))
	g.Expect(upText).To(ContainSubstring("DeploymentPlanResolvedRequirement_append_only"))
	g.Expect(upText).To(ContainSubstring("DeploymentPlanStepEdge_append_only"))
	g.Expect(upText).To(ContainSubstring("DeploymentPlanTarget_v2_seal_guard"))
	g.Expect(upText).To(ContainSubstring("DeploymentPlanStep_v2_seal_guard"))
	g.Expect(upText).To(ContainSubstring("DeploymentPlanIssue_v2_seal_guard"))
	g.Expect(upText).To(ContainSubstring("DeploymentPlanDraftAuditEvent_append_only"))
	g.Expect(upText).To(ContainSubstring("status = 'BLOCKED'"))
	g.Expect(upText).To(ContainSubstring("DeploymentPlan_v2_supersedes_unique"))
	g.Expect(upText).To(ContainSubstring("OLD.deployment_plan_id"))
	g.Expect(upText).To(ContainSubstring("NEW.deployment_plan_id"))
	g.Expect(upText).To(ContainSubstring(
		"NEW.deployment_plan_id IS DISTINCT FROM OLD.deployment_plan_id",
	))
	g.Expect(downText).To(ContainSubstring("LOCK TABLE"))
	g.Expect(downText).To(ContainSubstring("ACCESS EXCLUSIVE MODE"))
	g.Expect(
		strings.Index(downText, "LOCK TABLE"),
	).To(BeNumerically("<", strings.Index(downText, "refusing migration 145 rollback")))
	g.Expect(downText).To(ContainSubstring("refusing migration 145 rollback"))
}

func TestMigration163EnablesOnlyValidatedReadyTargetPlans(t *testing.T) {
	g := NewWithT(t)
	root := filepath.Join("..", "migrations", "sql")
	up, err := os.ReadFile(filepath.Join(root, "163_enable_validated_target_plan_execution.up.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	down, err := os.ReadFile(filepath.Join(root, "163_enable_validated_target_plan_execution.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	upText := string(up)
	downText := string(down)

	g.Expect(upText).To(ContainSubstring("DROP CONSTRAINT deploymentplan_v2_shape_check"))
	g.Expect(upText).To(ContainSubstring("SET LOCAL lock_timeout = '10s'"))
	g.Expect(upText).To(ContainSubstring("SET LOCAL statement_timeout = '5min'"))
	g.Expect(upText).To(ContainSubstring("status IN ('BLOCKED', 'READY', 'EXECUTED')"))
	g.Expect(upText).To(ContainSubstring("target deployment plan must be inserted unsealed"))
	g.Expect(upText).To(ContainSubstring("BEFORE INSERT OR UPDATE OR DELETE ON DeploymentPlan"))
	g.Expect(upText).To(ContainSubstring("NEW.status <> 'READY'"))
	g.Expect(upText).To(ContainSubstring("issue.severity = 'blocker'"))
	g.Expect(upText).To(ContainSubstring("OLD.status = 'READY'"))
	g.Expect(upText).To(ContainSubstring("NEW.status = 'EXECUTED'"))
	g.Expect(upText).To(ContainSubstring("persisted_status NOT IN ('READY', 'EXECUTED')"))
	g.Expect(upText).NotTo(ContainSubstring("target_plan_execution_deferred"))
	g.Expect(downText).To(ContainSubstring("refusing migration 163 rollback"))
	g.Expect(downText).To(ContainSubstring("SET LOCAL lock_timeout = '10s'"))
	g.Expect(downText).To(ContainSubstring("SET LOCAL statement_timeout = '5min'"))
	g.Expect(downText).To(ContainSubstring("DROP CONSTRAINT deploymentplan_v2_shape_check"))
	g.Expect(downText).To(ContainSubstring("status = 'BLOCKED'"))
	g.Expect(downText).To(ContainSubstring("BEFORE UPDATE OR DELETE ON DeploymentPlan"))
	g.Expect(downText).To(ContainSubstring("target_plan_execution_deferred"))
}

func TestMigration164PreservesLegacyAndAddsNativePlanningLineage(t *testing.T) {
	g := NewWithT(t)
	root := filepath.Join("..", "migrations", "sql")
	up, err := os.ReadFile(filepath.Join(root, "164_native_v2_planning_lineage.up.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	down, err := os.ReadFile(filepath.Join(root, "164_native_v2_planning_lineage.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	upText, downText := string(up), string(down)

	for _, fact := range []string{
		"active_desired_revision_id", "observed_component_state_id",
		"REFERENCES ActiveDesiredRevision", "REFERENCES ObservedComponentState",
		"projection = 'legacy_projection'", "projection = 'verified_v2'",
	} {
		g.Expect(upText).To(ContainSubstring(fact))
	}
	g.Expect(upText).To(ContainSubstring("deploymentplanresolvedrequirement_native_pair_check"))
	g.Expect(downText).To(ContainSubstring("ACCESS EXCLUSIVE MODE"))
	g.Expect(downText).To(ContainSubstring("refusing migration 164 rollback: native planning lineage exists"))
}

func TestMigration168FreezesFreshProviderApprovalAndContractProbeEvidence(t *testing.T) {
	g := NewWithT(t)
	root := filepath.Join("..", "migrations", "sql")
	up, err := os.ReadFile(filepath.Join(root, "168_dependency_provider_evidence.up.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	down, err := os.ReadFile(filepath.Join(root, "168_dependency_provider_evidence.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	upText, downText := string(up), string(down)

	for _, fact := range []string{
		"provider_evidence_version", "observation_fresh_until",
		"observation_trusted", "observation_current",
		"provider_approval_request_id", "provider_approval_checksum",
		"contract_probe_observation_id", "contract_probe_evidence_checksum",
		"REFERENCES ApprovalRequest", "REFERENCES ObservedComponentState",
		"contract_probe_observation_id = observed_component_state_id",
	} {
		g.Expect(upText).To(ContainSubstring(fact))
	}
	g.Expect(downText).To(ContainSubstring("ACCESS EXCLUSIVE MODE"))
	g.Expect(downText).To(ContainSubstring(
		"refusing migration 168 rollback: checksum-bound provider evidence exists",
	))
}

func TestObservedProviderCandidatesRequireCurrentFreshEvidenceAndApprovedExternalBinding(t *testing.T) {
	g := NewWithT(t)
	source, err := os.ReadFile("deployment_plan_drafts.go")
	g.Expect(err).NotTo(HaveOccurred())
	text := string(source)

	g.Expect(text).To(ContainSubstring("observed.is_current AND observed.trusted"))
	g.Expect(text).To(ContainSubstring("observed.fresh_until >= @effectiveAt"))
	g.Expect(text).To(ContainSubstring("request.subject_type = 'deployment_plan'"))
	g.Expect(text).To(ContainSubstring("request.state = 'APPROVED'"))
	g.Expect(text).To(ContainSubstring("request.expires_at > @effectiveAt"))
	g.Expect(text).To(ContainSubstring("approvalEvidenceChecksum(request)"))
	g.Expect(text).To(ContainSubstring("candidate.ContractProbeEvidenceChecksum"))
}

func TestLoadObservedProviderCandidatesScopesNativeAndExternalProvidersToTargetEnvironment(
	t *testing.T,
) {
	g := NewWithT(t)
	devEnvironmentID := uuid.MustParse("60000000-0000-0000-0000-000000000001")
	stgEnvironmentID := uuid.MustParse("60000000-0000-0000-0000-000000000002")
	prodEnvironmentID := uuid.MustParse("60000000-0000-0000-0000-000000000003")
	organizationID := uuid.New()
	selectedUnitID := uuid.New()
	effectiveAt := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	rows := &observedProviderRows{rows: [][]any{
		observedProviderTestRow(devEnvironmentID, types.DeliveryModelShared,
			types.RegistryManagementStateManaged, effectiveAt, false),
		observedProviderTestRow(stgEnvironmentID, types.DeliveryModelShared,
			types.RegistryManagementStateManaged, effectiveAt, false),
		observedProviderTestRow(prodEnvironmentID, types.DeliveryModelShared,
			types.RegistryManagementStateManaged, effectiveAt, false),
		observedProviderTestRow(devEnvironmentID, types.DeliveryModelExternal,
			types.RegistryManagementStateExternal, effectiveAt, true),
		observedProviderTestRow(stgEnvironmentID, types.DeliveryModelExternal,
			types.RegistryManagementStateExternal, effectiveAt, true),
		observedProviderTestRow(prodEnvironmentID, types.DeliveryModelExternal,
			types.RegistryManagementStateExternal, effectiveAt, true),
	}}
	recorder := &observedProviderQueryRecorder{rows: rows}
	ctx := internalctx.WithDb(context.Background(), recorder)
	input := types.PlanResolutionInput{
		EffectiveAt: effectiveAt,
		Assignment: types.TargetEnvironmentAssignment{
			EnvironmentID: stgEnvironmentID,
		},
		Unit: types.DeploymentUnit{ID: selectedUnitID},
		Requirements: []types.TargetRequirement{
			{
				Key: "target:consumer:shared", Capability: "transaction.api",
				AllowedModes: []types.RequirementResolutionMode{
					types.RequirementResolutionModeSharedProvider,
				},
			},
			{
				Key: "target:consumer:external", Capability: "transaction.api",
				AllowedModes: []types.RequirementResolutionMode{
					types.RequirementResolutionModeApprovedExternal,
				},
			},
		},
	}

	candidates, err := loadObservedProviderCandidates(ctx, organizationID, input)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(candidates).To(HaveLen(2))
	g.Expect([]types.RequirementResolutionMode{
		candidates[0].Mode,
		candidates[1].Mode,
	}).To(ConsistOf(
		types.RequirementResolutionModeSharedProvider,
		types.RequirementResolutionModeApprovedExternal,
	))
	for _, candidate := range candidates {
		g.Expect(candidate.ProviderEnvironmentID).To(Equal(stgEnvironmentID))
	}
	g.Expect(recorder.sql).To(ContainSubstring(
		"assignment.environment_id = @environmentID",
	))
	g.Expect(recorder.sql).To(ContainSubstring(
		"assignment.organization_id = unit.organization_id",
	))
	namedArgs, ok := recorder.args[0].(pgx.NamedArgs)
	g.Expect(ok).To(BeTrue())
	g.Expect(namedArgs["environmentID"]).To(Equal(stgEnvironmentID))
}

func TestDraftPublicationUsesRowLockAndExactOptimisticChecksum(t *testing.T) {
	g := NewWithT(t)
	source, err := os.ReadFile("deployment_plan_drafts.go")
	g.Expect(err).NotTo(HaveOccurred())
	text := string(source)
	g.Expect(text).To(ContainSubstring("RunTxIso(ctx, pgx.Serializable"))
	g.Expect(text).To(ContainSubstring("FOR UPDATE"))
	g.Expect(text).To(ContainSubstring("draft.Revision != expectedRevision"))
	g.Expect(text).To(ContainSubstring("validation.PreviewChecksum != expectedPreviewChecksum"))
	g.Expect(text).To(ContainSubstring("sealPublishedTargetPlan"))
	g.Expect(text).To(ContainSubstring("lockAndValidateTargetPlanSupersession"))
	g.Expect(text).To(ContainSubstring("ResolveEffectivePolicyForDeploymentUnit("))
	g.Expect(text).To(ContainSubstring("EffectivePolicy:            &effectivePolicy"))
	g.Expect(text).To(ContainSubstring(`"effectivePolicy":            plan.EffectivePolicy`))
	g.Expect(text).To(ContainSubstring(`"effectivePolicyChecksum":    plan.EffectivePolicyChecksum`))
	g.Expect(text).To(ContainSubstring(`"subscriberSetChecksum":      plan.SubscriberSetChecksum`))
	g.Expect(text).To(ContainSubstring("Status:                     types.DeploymentPlanStatusReady"))
	g.Expect(text).NotTo(ContainSubstring("target_plan_execution_deferred"))
	g.Expect(text).To(ContainSubstring(`"status":     types.DeploymentPlanStatusReady`))
	g.Expect(strings.Count(text, "organization_id = @organizationID")).To(
		BeNumerically(">=", 8),
	)
}

func TestTargetConfigVerificationUsesObservedObjectEvidence(t *testing.T) {
	g := NewWithT(t)
	expected := types.TargetPlanConfigObject{
		Key: "compose", Reference: "s3://config/_immutable/sha256/" +
			strings.Repeat("a", 64) + "/compose.yaml",
		VersionID: "version-1", MediaType: "application/yaml",
		SizeBytes: 42, Checksum: checksumForDraftTest("a"),
	}
	verifier := targetPlanConfigVerifierFunc(func(
		_ context.Context,
		object types.TargetPlanConfigObject,
	) (types.TargetPlanConfigObservation, error) {
		g.Expect(object).To(Equal(expected))
		return types.TargetPlanConfigObservation{
			Reference: object.Reference, VersionID: object.VersionID,
			MediaType: object.MediaType, SizeBytes: object.SizeBytes,
			Checksum: object.Checksum,
		}, nil
	})

	fact := verifyTargetPlanConfigObject(t.Context(), verifier, expected)

	g.Expect(fact.Verified).To(BeTrue())
	g.Expect(fact.ObservedReference).To(Equal(expected.Reference))
	g.Expect(fact.ObservedVersionID).To(Equal(expected.VersionID))
	g.Expect(fact.ObservedMediaType).To(Equal(expected.MediaType))
	g.Expect(fact.ObservedSizeBytes).To(Equal(expected.SizeBytes))
	g.Expect(fact.ObservedChecksum).To(Equal(expected.Checksum))
}

func TestTargetConfigVerificationFailsClosedWhenVerifierUnavailable(t *testing.T) {
	g := NewWithT(t)
	fact := verifyTargetPlanConfigObject(
		t.Context(),
		NewUnavailableTargetConfigObjectVerifier(),
		types.TargetPlanConfigObject{Key: "compose", Checksum: checksumForDraftTest("a")},
	)

	g.Expect(fact.Verified).To(BeFalse())
	g.Expect(fact.ObservedChecksum).To(BeEmpty())
	g.Expect(fact.VerificationCode).To(Equal("verification_unavailable"))
}

func TestTargetConfigVerificationRejectsEmptyObjectSet(t *testing.T) {
	g := NewWithT(t)

	facts, err := verifyTargetPlanConfigObjects(
		t.Context(),
		NewUnavailableTargetConfigObjectVerifier(),
		nil,
	)

	g.Expect(err).To(MatchError(ContainSubstring("at least one object")))
	g.Expect(facts).To(BeNil())
}

func TestTargetConfigVerificationRejectsOversizedSetBeforeVerifierCalls(t *testing.T) {
	g := NewWithT(t)
	calls := 0
	verifier := targetPlanConfigVerifierFunc(func(
		_ context.Context,
		_ types.TargetPlanConfigObject,
	) (types.TargetPlanConfigObservation, error) {
		calls++
		return types.TargetPlanConfigObservation{}, nil
	})
	objects := make(
		[]types.TargetPlanConfigObject,
		maxTargetPlanConfigObjects+1,
	)

	facts, err := verifyTargetPlanConfigObjects(t.Context(), verifier, objects)

	g.Expect(err).To(MatchError(ContainSubstring("object limit")))
	g.Expect(facts).To(BeNil())
	g.Expect(calls).To(Equal(0))
}

func TestTargetPlanProviderBoundsRejectRowsAndCandidateCrossProduct(t *testing.T) {
	g := NewWithT(t)

	g.Expect(validateTargetPlanProviderRowCount(maxTargetPlanProviderRows + 1)).
		To(MatchError(ContainSubstring("provider row limit")))
	candidates := make(
		[]types.RequirementProviderCandidate,
		maxTargetPlanCandidates,
	)
	result, err := appendTargetPlanCandidate(
		candidates,
		types.RequirementProviderCandidate{},
	)
	g.Expect(err).To(MatchError(ContainSubstring("candidate limit")))
	g.Expect(result).To(BeNil())
}

func TestObservedProviderQueryAppliesDatabaseRowLimit(t *testing.T) {
	g := NewWithT(t)
	source, err := os.ReadFile("deployment_plan_drafts.go")
	g.Expect(err).NotTo(HaveOccurred())
	text := string(source)

	g.Expect(text).To(ContainSubstring("LIMIT @providerRowLimit"))
	g.Expect(text).To(MatchRegexp(
		`"providerRowLimit"\s*:\s*maxTargetPlanProviderRows \+ 1`,
	))
}

type targetPlanConfigVerifierFunc func(
	context.Context,
	types.TargetPlanConfigObject,
) (types.TargetPlanConfigObservation, error)

func (fn targetPlanConfigVerifierFunc) VerifyTargetConfigObject(
	ctx context.Context,
	object types.TargetPlanConfigObject,
) (types.TargetPlanConfigObservation, error) {
	return fn(ctx, object)
}

func checksumForDraftTest(seed string) string {
	return "sha256:" + strings.Repeat(seed, 64)
}

type observedProviderQueryRecorder struct {
	queryable.Queryable
	sql  string
	args []any
	rows pgx.Rows
}

func (recorder *observedProviderQueryRecorder) Query(
	_ context.Context,
	sql string,
	args ...any,
) (pgx.Rows, error) {
	recorder.sql = sql
	recorder.args = args
	return recorder.rows, nil
}

type observedProviderRows struct {
	rows  [][]any
	index int
}

func (rows *observedProviderRows) Close() {}

func (rows *observedProviderRows) Err() error {
	return nil
}

func (rows *observedProviderRows) CommandTag() pgconn.CommandTag {
	return pgconn.CommandTag{}
}

func (rows *observedProviderRows) FieldDescriptions() []pgconn.FieldDescription {
	return nil
}

func (rows *observedProviderRows) Next() bool {
	if rows.index >= len(rows.rows) {
		return false
	}
	rows.index++
	return true
}

func (rows *observedProviderRows) Scan(dest ...any) error {
	values := rows.rows[rows.index-1]
	if len(dest) != len(values) {
		return pgx.ErrNoRows
	}
	for index, value := range values {
		target := reflect.ValueOf(dest[index]).Elem()
		if value == nil {
			target.SetZero()
			continue
		}
		source := reflect.ValueOf(value)
		if source.Type().AssignableTo(target.Type()) {
			target.Set(source)
			continue
		}
		if target.Kind() == reflect.Pointer && source.Type().AssignableTo(target.Type().Elem()) {
			pointer := reflect.New(target.Type().Elem())
			pointer.Elem().Set(source)
			target.Set(pointer)
			continue
		}
		return pgx.ErrNoRows
	}
	return nil
}

func (rows *observedProviderRows) Values() ([]any, error) {
	return rows.rows[rows.index-1], nil
}

func (rows *observedProviderRows) RawValues() [][]byte {
	return nil
}

func (rows *observedProviderRows) Conn() *pgx.Conn {
	return nil
}

func observedProviderTestRow(
	environmentID uuid.UUID,
	deliveryModel types.DeliveryModel,
	managementState types.RegistryManagementState,
	effectiveAt time.Time,
	approved bool,
) []any {
	var approvalID, approvalSubjectID any
	var approvalSubjectRevision, approvalRevision any
	var approvalSubjectChecksum, approvalPolicyChecksum, approvalSubscriberChecksum any
	var approvalState any
	if approved {
		approvalID, approvalSubjectID = uuid.New(), uuid.New()
		approvalSubjectRevision, approvalRevision = int64(1), int64(1)
		approvalSubjectChecksum = checksumForDraftTest("a")
		approvalPolicyChecksum = checksumForDraftTest("b")
		approvalSubscriberChecksum = checksumForDraftTest("c")
		approvalState = types.ApprovalRequestStateApproved
	}
	return []any{
		"transaction.api", uuid.New(), "1.0.0", "linux/amd64",
		checksumForDraftTest("d"), uuid.New(), uuid.New(), environmentID,
		checksumForDraftTest("e"), uuid.New(), uuid.New(), int64(1),
		checksumForDraftTest("f"), effectiveAt.Add(time.Hour), true, true,
		checksumForDraftTest("1"), deliveryModel, managementState, true,
		checksumForDraftTest("2"), approvalID, approvalSubjectID,
		approvalSubjectRevision, approvalSubjectChecksum, approvalPolicyChecksum,
		approvalSubscriberChecksum, approvalRevision, approvalState,
	}
}
