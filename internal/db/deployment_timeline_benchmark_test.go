package db_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

func BenchmarkPR049TimelineAndBackfillQueryPaths(b *testing.B) {
	for _, scenario := range pr049DBBenchmarkScenarios() {
		scenario := scenario
		b.Run(scenario.name, func(b *testing.B) {
			ctx := taskQueueDBTestContext(b)
			fixture := createPR049DBBenchmarkFixture(b, ctx, scenario)

			b.Run("timeline_read_through_partial_backfill", func(b *testing.B) {
				expectedTimelineItems := scenario.revisions
				if expectedTimelineItems > 200 {
					expectedTimelineItems = 100
				}
				b.ReportMetric(float64(scenario.revisions), "dataset_revisions")
				b.ReportMetric(float64(expectedTimelineItems), "returned_revisions")
				b.ReportMetric(float64(scenario.targetCount), "targets")
				b.ReportMetric(float64(scenario.valuesHashBytes), "value_hash_bytes")
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					timeline, err := db.GetDeploymentTimeline(ctx, types.DeploymentTimelineQuery{
						OrganizationID: fixture.orgID,
						ApplicationID:  &fixture.applicationID,
						Limit:          scenario.revisions,
					})
					if err != nil {
						b.Fatal(err)
					}
					if len(timeline.Items) != expectedTimelineItems {
						b.Fatalf("expected %d timeline items, got %d", expectedTimelineItems, len(timeline.Items))
					}
					seenTargets := map[uuid.UUID]struct{}{}
					for _, item := range timeline.Items {
						seenTargets[item.DeploymentTargetID] = struct{}{}
					}
					expectedTargets := scenario.targetCount
					if expectedTargets > expectedTimelineItems {
						expectedTargets = expectedTimelineItems
					}
					if len(seenTargets) != expectedTargets {
						b.Fatalf("expected %d distinct targets in timeline page, got %d", expectedTargets, len(seenTargets))
					}
				}
			})

			b.Run("backfill_batched_dry_run_to_exhaustion", func(b *testing.B) {
				b.ReportMetric(float64(scenario.revisions), "revisions")
				b.ReportMetric(float64(scenario.targetCount), "targets")
				b.ReportMetric(float64(scenario.batchSize), "batch_size")
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					report, err := db.BackfillLegacyDeploymentCompatibility(ctx, types.DeploymentCompatibilityBackfillRequest{
						OrganizationID: fixture.orgID,
						Apply:          false,
						BatchSize:      scenario.batchSize,
					})
					if err != nil {
						b.Fatal(err)
					}
					if report.Scanned != scenario.revisions || report.Projected != scenario.revisions {
						b.Fatalf(
							"expected %d scanned/projected candidates, got scanned=%d projected=%d",
							scenario.revisions,
							report.Scanned,
							report.Projected,
						)
					}
				}
			})

			if scenario.onlineAgentCount > 0 {
				b.Run("agent_capability_concurrent_read", func(b *testing.B) {
					benchmarkPR049DBAgentCapabilities(b, ctx, fixture)
				})
			}
			b.Run("release_bundle_component_hydration", func(b *testing.B) {
				benchmarkPR049DBReleaseBundleComponents(b, ctx, fixture)
			})
			b.Run("deployment_plan_step_hydration", func(b *testing.B) {
				benchmarkPR049DBDeploymentPlanSteps(b, ctx, fixture)
			})
			if scenario.stepLogBytes > 0 {
				b.Run("step_log_write_read", func(b *testing.B) {
					benchmarkPR049DBStepLogs(b, ctx, fixture)
				})
			}
			if scenario.scopedVariableCount > 0 {
				b.Run("scoped_variable_resolution", func(b *testing.B) {
					benchmarkPR049DBScopedVariables(b, ctx, fixture)
				})
			}
		})
	}
}

func benchmarkPR049DBAgentCapabilities(
	b *testing.B,
	ctx context.Context,
	fixture pr049DBBenchmarkFixture,
) {
	b.Helper()
	b.ReportMetric(float64(fixture.scenario.onlineAgentCount), "agents")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		errCh := make(chan error, len(fixture.agentTargetIDs))
		for _, targetID := range fixture.agentTargetIDs {
			targetID := targetID
			wg.Add(1)
			go func() {
				defer wg.Done()
				report, err := db.GetAgentCapabilityReportForDeploymentTarget(ctx, targetID, fixture.orgID)
				if err != nil {
					errCh <- err
					return
				}
				if len(report.SupportedActions) != 2 {
					errCh <- fmt.Errorf("expected 2 supported actions, got %d", len(report.SupportedActions))
					return
				}
				errCh <- nil
			}()
		}
		wg.Wait()
		close(errCh)
		for err := range errCh {
			if err != nil {
				b.Fatal(err)
			}
		}
	}
}

func benchmarkPR049DBReleaseBundleComponents(
	b *testing.B,
	ctx context.Context,
	fixture pr049DBBenchmarkFixture,
) {
	b.Helper()
	b.ReportMetric(float64(fixture.scenario.componentCount), "components")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		bundle, err := db.GetReleaseBundle(ctx, fixture.releaseBundleID, fixture.orgID)
		if err != nil {
			b.Fatal(err)
		}
		if len(bundle.Components) != fixture.scenario.componentCount {
			b.Fatalf("expected %d components, got %d", fixture.scenario.componentCount, len(bundle.Components))
		}
	}
}

func benchmarkPR049DBDeploymentPlanSteps(
	b *testing.B,
	ctx context.Context,
	fixture pr049DBBenchmarkFixture,
) {
	b.Helper()
	b.ReportMetric(float64(fixture.scenario.stepCount), "steps")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		plan, err := db.GetDeploymentPlan(ctx, fixture.planID, fixture.orgID)
		if err != nil {
			b.Fatal(err)
		}
		if len(plan.Steps) != fixture.scenario.stepCount {
			b.Fatalf("expected %d steps, got %d", fixture.scenario.stepCount, len(plan.Steps))
		}
		if fixture.scenario.scopedVariableCount > 0 && len(plan.Variables) != fixture.scenario.scopedVariableCount {
			b.Fatalf("expected %d plan variables, got %d", fixture.scenario.scopedVariableCount, len(plan.Variables))
		}
	}
}

func benchmarkPR049DBStepLogs(
	b *testing.B,
	ctx context.Context,
	fixture pr049DBBenchmarkFixture,
) {
	b.Helper()
	b.ReportMetric(float64(fixture.scenario.stepLogBytes), "step_log_bytes")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := db.RecordAgentStepRunEvent(ctx, types.RecordAgentStepRunEventRequest{
			OrganizationID: fixture.orgID,
			AgentID:        fixture.taskLogAgentID,
			StepRunID:      fixture.taskLogStepRunID,
			LeaseToken:     fixture.taskLogLeaseToken,
			Sequence:       int64(i + 2),
			Type:           types.StepRunEventTypeLog,
			Logs:           fixture.logChunks,
		})
		if err != nil {
			b.Fatal(err)
		}
		logs, err := db.GetTaskLogs(ctx, fixture.taskLogTaskID, fixture.orgID)
		if err != nil {
			b.Fatal(err)
		}
		expectedMin := (i + 1) * len(fixture.logChunks)
		if len(logs) < expectedMin {
			b.Fatalf("expected at least %d log chunks, got %d", expectedMin, len(logs))
		}
	}
}

func benchmarkPR049DBScopedVariables(
	b *testing.B,
	ctx context.Context,
	fixture pr049DBBenchmarkFixture,
) {
	b.Helper()
	b.ReportMetric(float64(fixture.scenario.scopedVariableCount), "scoped_variables")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		resolved, err := db.ResolveVariablesPreview(
			ctx,
			fixture.orgID,
			[]uuid.UUID{fixture.variableSetID},
			types.VariableResolutionScope{EnvironmentID: &fixture.environmentID, TargetTags: fixture.variableTargetTags},
			nil,
		)
		if err != nil {
			b.Fatal(err)
		}
		if len(resolved) != fixture.scenario.scopedVariableCount {
			b.Fatalf("expected %d resolved variables, got %d", fixture.scenario.scopedVariableCount, len(resolved))
		}
		if countResolvedVariables(resolved) != fixture.scenario.scopedVariableCount {
			b.Fatalf("expected all variables resolved, got %+v", resolved)
		}
	}
}

type pr049DBBenchmarkFixture struct {
	scenario           pr049DBBenchmarkScenario
	orgID              uuid.UUID
	applicationID      uuid.UUID
	environmentID      uuid.UUID
	releaseBundleID    uuid.UUID
	planID             uuid.UUID
	variableSetID      uuid.UUID
	targetIDs          []uuid.UUID
	agentTargetIDs     []uuid.UUID
	variableTargetTags []string
	taskLogTaskID      uuid.UUID
	taskLogStepRunID   uuid.UUID
	taskLogAgentID     uuid.UUID
	taskLogLeaseToken  string
	logChunks          []types.RecordStepRunLogChunkRequest
}

func createPR049DBBenchmarkFixture(
	b *testing.B,
	ctx context.Context,
	scenario pr049DBBenchmarkScenario,
) pr049DBBenchmarkFixture {
	b.Helper()
	deps := createReleaseBundleEligibilityDependencies(b, ctx)
	actorID := createReleaseBundleTestUser(b, ctx, deps.orgID)
	targetIDs := make([]uuid.UUID, scenario.targetCount)
	for i := range targetIDs {
		targetIDs[i] = createReleaseBundleDockerTargetForOrganization(b, ctx, deps.orgID, fmt.Sprintf("%s-target-%03d", scenario.name, i))
	}
	if len(targetIDs) != scenario.targetCount {
		b.Fatalf("expected %d targets, got %d", scenario.targetCount, len(targetIDs))
	}

	agentTargetIDs := upsertPR049DBAgentReports(b, ctx, deps.orgID, targetIDs, scenario)
	variableSetID, variableTargetTags := createPR049DBVariableSet(b, ctx, deps.orgID, deps.applicationID, deps.devEnvironmentID, scenario)
	processRevisionID := createPR049DBProcessRevision(b, ctx, deps.orgID, deps.applicationID, scenario)

	bundle := releaseBundleFixture(deps.orgID, deps.applicationID, deps.channelID, deps.versionID)
	bundle.ReleaseNumber = "2026.06." + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	bundle.Components = pr049DBComponents(scenario)
	bundle.DeploymentProcessRevisionID = &processRevisionID
	if err := db.CreateReleaseBundle(ctx, &bundle); err != nil {
		b.Fatalf("create release bundle: %v", err)
	}
	published, result, err := db.PublishReleaseBundle(ctx, bundle.ID, deps.orgID, actorID)
	if err != nil {
		b.Fatalf("publish release bundle: %v", err)
	}
	if !result.Valid {
		b.Fatalf("expected release bundle validation to pass, got %+v", result.Errors)
	}
	plan, err := db.CreateDeploymentPlan(ctx, types.CreateDeploymentPlanRequest{
		OrganizationID:  deps.orgID,
		ReleaseBundleID: published.ID,
		EnvironmentID:   deps.devEnvironmentID,
		TargetIDs:       targetIDs,
	})
	if err != nil {
		b.Fatalf("create deployment plan: %v", err)
	}
	if plan.Status != types.DeploymentPlanStatusReady {
		b.Fatalf("expected ready deployment plan, got %s issues=%+v", plan.Status, plan.Issues)
	}
	if len(plan.Steps) != scenario.stepCount {
		b.Fatalf("expected %d persisted plan steps, got %d", scenario.stepCount, len(plan.Steps))
	}
	if scenario.scopedVariableCount > 0 && len(plan.Variables) != scenario.scopedVariableCount {
		b.Fatalf("expected %d persisted plan variables, got %d", scenario.scopedVariableCount, len(plan.Variables))
	}

	for i := 0; i < scenario.revisions; i++ {
		createLegacyDeploymentRevisionForTimelineTest(b, ctx, targetIDs[i%len(targetIDs)], deps.versionID, pr049DBValuesHash(scenario, i))
	}

	fixture := pr049DBBenchmarkFixture{
		scenario:           scenario,
		orgID:              deps.orgID,
		applicationID:      deps.applicationID,
		environmentID:      deps.devEnvironmentID,
		releaseBundleID:    published.ID,
		planID:             plan.ID,
		variableSetID:      variableSetID,
		targetIDs:          targetIDs,
		agentTargetIDs:     agentTargetIDs,
		variableTargetTags: variableTargetTags,
		logChunks:          pr049DBLogChunks(scenario.stepLogBytes),
	}
	if scenario.stepLogBytes > 0 {
		fixture = createPR049DBTaskLogFixture(b, ctx, fixture, actorID)
	}
	return fixture
}

func upsertPR049DBAgentReports(
	b *testing.B,
	ctx context.Context,
	orgID uuid.UUID,
	targetIDs []uuid.UUID,
	scenario pr049DBBenchmarkScenario,
) []uuid.UUID {
	b.Helper()
	agentTargetIDs := make([]uuid.UUID, 0, scenario.onlineAgentCount)
	for i := 0; i < scenario.onlineAgentCount; i++ {
		targetID := targetIDs[i%len(targetIDs)]
		report := types.AgentCapabilityReport{
			OrganizationID:       orgID,
			DeploymentTargetID:   targetID,
			ProtocolVersion:      types.AgentCapabilityProtocolV1,
			AgentVersion:         fmt.Sprintf("agent-%03d", i),
			SupportedRuntimes:    []string{"docker"},
			OperatingSystem:      "linux",
			Architecture:         "amd64",
			AvailableTooling:     []string{"compose", "oci"},
			StrategyCapabilities: []string{"rolling", "canary"},
			SupportedActions: []types.AgentActionCapability{
				{ActionType: "distr.http.check", Versions: []string{types.AgentActionVersionV1}},
				{ActionType: "distr.oci.job", Versions: []string{types.AgentActionVersionV1}},
			},
		}
		if _, err := db.UpsertAgentCapabilityReport(ctx, report); err != nil {
			b.Fatalf("upsert agent capability report: %v", err)
		}
		agentTargetIDs = append(agentTargetIDs, targetID)
	}
	return agentTargetIDs
}

func createPR049DBVariableSet(
	b *testing.B,
	ctx context.Context,
	orgID uuid.UUID,
	applicationID uuid.UUID,
	environmentID uuid.UUID,
	scenario pr049DBBenchmarkScenario,
) (uuid.UUID, []string) {
	b.Helper()
	if scenario.scopedVariableCount == 0 {
		return uuid.Nil, nil
	}
	variables := make([]types.Variable, scenario.scopedVariableCount)
	tags := make([]string, scenario.scopedVariableCount)
	for i := range variables {
		tag := fmt.Sprintf("pr049-%s-%03d", strings.ReplaceAll(scenario.name, "_", "-"), i)
		tags[i] = tag
		variables[i] = types.Variable{
			Key:          fmt.Sprintf("PR049_%03d", i),
			Type:         types.VariableTypeString,
			IsRequired:   true,
			DefaultValue: []byte(`"default"`),
			ScopedValues: []types.VariableScopedValue{
				{
					SortOrder: 1,
					Scope: types.VariableScope{
						EnvironmentID: &environmentID,
						TargetTag:     tag,
					},
					Value: []byte(fmt.Sprintf(`"value-%03d"`, i)),
				},
			},
		}
	}
	variableSet := types.VariableSet{
		OrganizationID: orgID,
		Name:           "PR-049 benchmark variables " + uuid.NewString(),
		ApplicationIDs: []uuid.UUID{applicationID},
		Variables:      variables,
	}
	if err := db.CreateVariableSet(ctx, &variableSet); err != nil {
		b.Fatalf("create variable set: %v", err)
	}
	return variableSet.ID, tags
}

func createPR049DBProcessRevision(
	b *testing.B,
	ctx context.Context,
	orgID uuid.UUID,
	applicationID uuid.UUID,
	scenario pr049DBBenchmarkScenario,
) uuid.UUID {
	b.Helper()
	process := types.DeploymentProcess{
		OrganizationID: orgID,
		ApplicationID:  applicationID,
		Name:           "PR-049 benchmark process " + uuid.NewString(),
	}
	if err := db.CreateDeploymentProcess(ctx, &process); err != nil {
		b.Fatalf("create deployment process: %v", err)
	}
	steps := make([]types.DeploymentProcessStep, scenario.stepCount)
	for i := range steps {
		steps[i] = types.DeploymentProcessStep{
			Key:                  fmt.Sprintf("step-%03d", i),
			Name:                 fmt.Sprintf("Step %03d", i),
			ActionType:           "distr.http.check",
			ExecutionLocation:    "target",
			InputBindings:        map[string]any{"url": fmt.Sprintf("https://example.test/health/%03d", i)},
			FailureMode:          "fail",
			TimeoutSeconds:       300,
			RetryMaxAttempts:     2,
			RetryIntervalSeconds: 5,
			SortOrder:            i + 1,
		}
	}
	revision := types.DeploymentProcessRevision{
		OrganizationID:      orgID,
		DeploymentProcessID: process.ID,
		Description:         "PR-049 benchmark revision",
		Steps:               steps,
	}
	if err := db.CreateDeploymentProcessRevision(ctx, &revision); err != nil {
		b.Fatalf("create deployment process revision: %v", err)
	}
	return revision.ID
}

func pr049DBComponents(scenario pr049DBBenchmarkScenario) []types.ReleaseBundleComponent {
	components := make([]types.ReleaseBundleComponent, scenario.componentCount)
	for i := range components {
		components[i] = types.ReleaseBundleComponent{
			Key:        fmt.Sprintf("component-%03d", i),
			Name:       fmt.Sprintf("Component %03d", i),
			Type:       types.ReleaseBundleComponentTypeOCIImage,
			Version:    fmt.Sprintf("2026.06.%03d", i),
			PackageRef: fmt.Sprintf("registry.example.test/%s/component-%03d", strings.ReplaceAll(scenario.name, "_", "-"), i),
			Digest:     "sha256:" + strings.Repeat(fmt.Sprintf("%x", i%16), 64),
		}
	}
	return components
}

func createPR049DBTaskLogFixture(
	b *testing.B,
	ctx context.Context,
	fixture pr049DBBenchmarkFixture,
	actorID uuid.UUID,
) pr049DBBenchmarkFixture {
	b.Helper()
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     fixture.orgID,
		DeploymentPlanID:   fixture.planID,
		ActorUserAccountID: actorID,
	})
	if err != nil {
		b.Fatalf("create tasks for log fixture: %v", err)
	}
	if len(tasks) == 0 || len(tasks[0].StepRuns) == 0 {
		b.Fatal("expected task and step run for log fixture")
	}
	lease, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: fixture.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})
	if err != nil {
		b.Fatalf("lease task for log fixture: %v", err)
	}
	if len(lease.Steps) == 0 {
		b.Fatal("expected leased step for log fixture")
	}
	_, err = db.RecordAgentStepRunEvent(ctx, types.RecordAgentStepRunEventRequest{
		OrganizationID: fixture.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
		StepRunID:      lease.Steps[0].StepRunID,
		LeaseToken:     lease.LeaseToken,
		Sequence:       1,
		Type:           types.StepRunEventTypeStarted,
	})
	if err != nil {
		b.Fatalf("start step run for log fixture: %v", err)
	}
	fixture.taskLogTaskID = tasks[0].ID
	fixture.taskLogStepRunID = lease.Steps[0].StepRunID
	fixture.taskLogAgentID = tasks[0].DeploymentTargetID
	fixture.taskLogLeaseToken = lease.LeaseToken
	return fixture
}

func pr049DBLogChunks(bytes int) []types.RecordStepRunLogChunkRequest {
	chunks := make([]types.RecordStepRunLogChunkRequest, 0, pr049DBExpectedStepLogChunkCount(bytes))
	remaining := bytes
	for remaining > 0 {
		size := remaining
		if size > types.MaxStepRunLogChunkBodyLength {
			size = types.MaxStepRunLogChunkBodyLength
		}
		chunks = append(chunks, types.RecordStepRunLogChunkRequest{
			Stream:   types.StepRunLogStreamStdout,
			Severity: types.StepRunLogSeverityInfo,
			Body:     strings.Repeat("L", size),
		})
		remaining -= size
	}
	return chunks
}

func pr049DBExpectedStepLogChunkCount(bytes int) int {
	if bytes == 0 {
		return 0
	}
	return (bytes + types.MaxStepRunLogChunkBodyLength - 1) / types.MaxStepRunLogChunkBodyLength
}
func countResolvedVariables(values []types.ResolvedVariable) int {
	count := 0
	for _, value := range values {
		if value.Status == types.VariableResolutionStatusResolved {
			count++
		}
	}
	return count
}

type pr049DBBenchmarkScenario struct {
	name                string
	revisions           int
	targetCount         int
	onlineAgentCount    int
	componentCount      int
	stepCount           int
	stepLogBytes        int
	scopedVariableCount int
	valuesHashBytes     int
	batchSize           int
}

func pr049DBBenchmarkScenarios() []pr049DBBenchmarkScenario {
	full := os.Getenv("PR049_FULL_BENCH") == "1"
	definitions := []struct {
		name                string
		ciRevisions         int
		fullRevisions       int
		ciTargets           int
		fullTargets         int
		ciOnlineAgents      int
		fullOnlineAgents    int
		ciComponents        int
		fullComponents      int
		ciSteps             int
		fullSteps           int
		ciStepLogBytes      int
		fullStepLogBytes    int
		ciScopedVariables   int
		fullScopedVariables int
		valuesHashBytes     int
		batchSize           int
	}{
		{name: "deployment_targets", ciRevisions: 10, fullRevisions: 1000, ciTargets: 10, fullTargets: 1000, ciComponents: 1, fullComponents: 1, ciSteps: 1, fullSteps: 1, valuesHashBytes: 64, batchSize: 7},
		{name: "online_agents", ciRevisions: 10, fullRevisions: 100, ciTargets: 10, fullTargets: 100, ciOnlineAgents: 10, fullOnlineAgents: 100, ciComponents: 1, fullComponents: 1, ciSteps: 1, fullSteps: 1, valuesHashBytes: 1024, batchSize: 9},
		{name: "component_release_bundle", ciRevisions: 10, fullRevisions: 100, ciTargets: 2, fullTargets: 2, ciComponents: 10, fullComponents: 100, ciSteps: 1, fullSteps: 1, valuesHashBytes: 4096, batchSize: 11},
		{name: "step_aggregate_wave", ciRevisions: 10, fullRevisions: 500, ciTargets: 2, fullTargets: 10, ciComponents: 1, fullComponents: 1, ciSteps: 10, fullSteps: 500, valuesHashBytes: 8192, batchSize: 13},
		{name: "large_step_logs", ciRevisions: 10, fullRevisions: 250, ciTargets: 2, fullTargets: 5, ciComponents: 1, fullComponents: 1, ciSteps: 1, fullSteps: 1, ciStepLogBytes: 16 * 1024, fullStepLogBytes: 64 * 1024, valuesHashBytes: 8192, batchSize: 17},
		{name: "scoped_variable_candidates", ciRevisions: 10, fullRevisions: 500, ciTargets: 2, fullTargets: 10, ciComponents: 1, fullComponents: 1, ciSteps: 1, fullSteps: 1, ciScopedVariables: 10, fullScopedVariables: 500, valuesHashBytes: 32768, batchSize: 19},
	}
	scenarios := make([]pr049DBBenchmarkScenario, 0, len(definitions))
	for _, definition := range definitions {
		scenario := pr049DBBenchmarkScenario{
			name:                fmt.Sprintf("ci_%d_%s", definition.ciRevisions, definition.name),
			revisions:           definition.ciRevisions,
			targetCount:         definition.ciTargets,
			onlineAgentCount:    definition.ciOnlineAgents,
			componentCount:      definition.ciComponents,
			stepCount:           definition.ciSteps,
			stepLogBytes:        definition.ciStepLogBytes,
			scopedVariableCount: definition.ciScopedVariables,
			valuesHashBytes:     definition.valuesHashBytes,
			batchSize:           definition.batchSize,
		}
		if full {
			scenario.name = fmt.Sprintf("%d_%s", definition.fullRevisions, definition.name)
			scenario.revisions = definition.fullRevisions
			scenario.targetCount = definition.fullTargets
			scenario.onlineAgentCount = definition.fullOnlineAgents
			scenario.componentCount = definition.fullComponents
			scenario.stepCount = definition.fullSteps
			scenario.stepLogBytes = definition.fullStepLogBytes
			scenario.scopedVariableCount = definition.fullScopedVariables
		}
		scenarios = append(scenarios, scenario)
	}
	return scenarios
}

func pr049DBValuesHash(scenario pr049DBBenchmarkScenario, index int) string {
	seed := fmt.Sprintf(
		"%s:%06d:targets=%d:agents=%d:components=%d:steps=%d:logs=%d:variables=%d:batch=%d|",
		scenario.name,
		index,
		scenario.targetCount,
		scenario.onlineAgentCount,
		scenario.componentCount,
		scenario.stepCount,
		scenario.stepLogBytes,
		scenario.scopedVariableCount,
		scenario.batchSize,
	)
	if len(seed) >= scenario.valuesHashBytes {
		return seed[:scenario.valuesHashBytes]
	}
	var builder strings.Builder
	builder.Grow(scenario.valuesHashBytes)
	for builder.Len() < scenario.valuesHashBytes {
		builder.WriteString(seed)
	}
	return builder.String()[:scenario.valuesHashBytes]
}
