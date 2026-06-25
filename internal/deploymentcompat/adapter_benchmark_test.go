package deploymentcompat_test

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/actionregistry"
	"github.com/distr-sh/distr/internal/deploymentcompat"
	"github.com/distr-sh/distr/internal/releasebundles"
	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/variableresolution"
	"github.com/google/uuid"
)

func BenchmarkPR049LegacyProjectionScenarios(b *testing.B) {
	scales := pr049BenchmarkScales()
	for _, scale := range scales {
		scale := scale
		b.Run(scale.name, func(b *testing.B) {
			fixtures := make([]legacyProjectionFixture, scale.shape.revisions)
			for i := range fixtures {
				fixtures[i] = newLegacyProjectionFixture(scale.shape, i)
			}
			b.ReportMetric(float64(scale.shape.targetCount), "targets")
			b.ReportMetric(float64(scale.shape.onlineAgentCount), "agents")
			b.ReportMetric(float64(scale.shape.componentCount), "components")
			b.ReportMetric(float64(scale.shape.stepCount), "steps")
			b.ReportMetric(float64(scale.shape.stepLogBytes), "step_log_bytes")
			b.ReportMetric(float64(scale.shape.scopedVariableCount), "scoped_variables")

			b.Run("legacy_projection", func(b *testing.B) {
				benchmarkPR049LegacyProjection(b, scale.shape, fixtures)
			})
			if scale.shape.onlineAgentCount > 0 {
				b.Run("agent_capability_validation_concurrent", func(b *testing.B) {
					benchmarkPR049AgentCapabilityValidation(b, scale.shape, fixtures[0])
				})
			}
			b.Run("bundle_component_canonicalization", func(b *testing.B) {
				benchmarkPR049BundleComponents(b, scale.shape, fixtures[0])
			})
			b.Run("step_wave_action_validation", func(b *testing.B) {
				benchmarkPR049StepWave(b, scale.shape, fixtures[0])
			})
			if scale.shape.stepLogBytes > 0 {
				b.Run("step_log_payload_read_write", func(b *testing.B) {
					benchmarkPR049StepLogPayload(b, scale.shape, fixtures[0])
				})
			}
			if scale.shape.scopedVariableCount > 0 {
				b.Run("scoped_variable_resolution", func(b *testing.B) {
					benchmarkPR049ScopedVariableResolution(b, scale.shape, fixtures[0])
				})
			}
		})
	}
}

func benchmarkPR049LegacyProjection(
	b *testing.B,
	shape pr049FixtureShape,
	fixtures []legacyProjectionFixture,
) {
	b.Helper()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fixture := fixtures[i%len(fixtures)]
		if _, err := deploymentcompat.ProjectLegacyDeployment(
			fixture.deployment,
			fixture.revision,
			fixture.context,
		); err != nil {
			b.Fatal(err)
		}
		if counts := countPR049Fixture(fixture); counts != expectedPR049FixtureCounts(shape) {
			b.Fatalf("unexpected fixture counts: got %+v want %+v", counts, expectedPR049FixtureCounts(shape))
		}
	}
}

func benchmarkPR049AgentCapabilityValidation(
	b *testing.B,
	shape pr049FixtureShape,
	fixture legacyProjectionFixture,
) {
	b.Helper()
	requests := agentCapabilityRequests(fixture.agentReports)
	if len(requests) != shape.onlineAgentCount {
		b.Fatalf("expected %d agent reports, got %d", shape.onlineAgentCount, len(requests))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		errCh := make(chan error, len(requests))
		for _, request := range requests {
			request := request
			wg.Add(1)
			go func() {
				defer wg.Done()
				errCh <- request.Validate()
			}()
		}
		wg.Wait()
		close(errCh)
		validated := 0
		for err := range errCh {
			if err != nil {
				b.Fatal(err)
			}
			validated++
		}
		if validated != shape.onlineAgentCount {
			b.Fatalf("expected %d validated reports, got %d", shape.onlineAgentCount, validated)
		}
	}
}

func benchmarkPR049BundleComponents(
	b *testing.B,
	shape pr049FixtureShape,
	fixture legacyProjectionFixture,
) {
	b.Helper()
	bundle := types.ReleaseBundle{
		OrganizationID:   fixture.context.OrganizationID,
		ApplicationID:    fixture.context.ApplicationID,
		ChannelID:        uuid.NewSHA1(uuid.NameSpaceOID, []byte("channel-"+shape.name)),
		ReleaseNumber:    "2026.06.23",
		ReleaseNotes:     "PR-049 component benchmark",
		SourceRevision:   fixture.revision.ID.String(),
		SourceRepository: "https://example.test/repo.git",
		Components:       fixture.components,
	}
	if len(bundle.Components) != shape.componentCount {
		b.Fatalf("expected %d components, got %d", shape.componentCount, len(bundle.Components))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		payload, checksum, err := releasebundles.Canonicalize(bundle)
		if err != nil {
			b.Fatal(err)
		}
		bundle.CanonicalPayload = payload
		bundle.CanonicalChecksum = checksum
		if result := releasebundles.ValidateBundleContent(bundle); !result.Valid {
			b.Fatalf("expected bundle content to validate, got %+v", result.Errors)
		}
		if len(payload) == 0 || checksum == "" {
			b.Fatal("expected canonical payload and checksum")
		}
	}
}

func benchmarkPR049StepWave(
	b *testing.B,
	shape pr049FixtureShape,
	fixture legacyProjectionFixture,
) {
	b.Helper()
	registry := actionregistry.DefaultRegistry()
	if len(fixture.steps) != shape.stepCount {
		b.Fatalf("expected %d steps, got %d", shape.stepCount, len(fixture.steps))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		validated := 0
		for _, step := range fixture.steps {
			if _, ok := registry.Get(step.ActionType); !ok {
				b.Fatalf("unknown action type %q", step.ActionType)
			}
			if err := registry.ValidateInput(step.ActionType, step.InputBindings); err != nil {
				b.Fatal(err)
			}
			validated++
		}
		if validated != shape.stepCount {
			b.Fatalf("expected %d validated steps, got %d", shape.stepCount, validated)
		}
	}
}

func benchmarkPR049StepLogPayload(
	b *testing.B,
	shape pr049FixtureShape,
	fixture legacyProjectionFixture,
) {
	b.Helper()
	logs := recordStepRunLogRequests(fixture.stepLogChunks)
	if len(logs) != expectedStepLogChunkCount(shape.stepLogBytes) {
		b.Fatalf("expected %d log chunks, got %d", expectedStepLogChunkCount(shape.stepLogBytes), len(logs))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		request := types.RecordAgentStepRunEventRequest{
			OrganizationID: fixture.context.OrganizationID,
			AgentID:        fixture.deployment.DeploymentTargetID,
			StepRunID:      uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("%s-step-run-%06d", shape.name, i))),
			LeaseToken:     "benchmark-token",
			Sequence:       int64(i + 1),
			Type:           types.StepRunEventTypeLog,
			Logs:           logs,
		}
		total := 0
		for _, log := range request.Logs {
			total += len(log.Body)
		}
		if total != shape.stepLogBytes {
			b.Fatalf("expected %d log bytes, got %d", shape.stepLogBytes, total)
		}
	}
}

func benchmarkPR049ScopedVariableResolution(
	b *testing.B,
	shape pr049FixtureShape,
	fixture legacyProjectionFixture,
) {
	b.Helper()
	variables := scopedVariableResolutionFixture(fixture)
	if len(variables) != 1 || len(variables[0].ScopedValues) != shape.scopedVariableCount {
		b.Fatalf("expected %d scoped values, got %+v", shape.scopedVariableCount, countScopedValues(variables))
	}
	scope := types.VariableResolutionScope{
		EnvironmentID: scopedVariableEnvironmentID(fixture.scopedValues),
		TargetTags:    scopedVariableTargetTags(fixture.scopedValues),
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resolved, err := variableresolution.Resolve(variableresolution.Request{
			Variables: variables,
			Scope:     scope,
		})
		if err != nil {
			b.Fatal(err)
		}
		if len(resolved) != 1 || resolved[0].Status != types.VariableResolutionStatusResolved {
			b.Fatalf("expected resolved variable, got %+v", resolved)
		}
		if len(resolved[0].Trace) != shape.scopedVariableCount+1 {
			b.Fatalf("expected %d trace candidates, got %d", shape.scopedVariableCount+1, len(resolved[0].Trace))
		}
	}
}

func TestPR049BenchmarkScenariosUseDistinctFixtureShapes(t *testing.T) {
	scenarios := pr049BenchmarkScenarios()
	if len(scenarios) != 6 {
		t.Fatalf("expected six PR-049 benchmark scenarios, got %d", len(scenarios))
	}

	seen := map[pr049FixtureCounts]string{}
	for _, scenario := range scenarios {
		for _, shape := range []pr049FixtureShape{scenario.ci, scenario.full} {
			fixture := newLegacyProjectionFixture(shape, 0)
			counts := countPR049Fixture(fixture)
			if counts != expectedPR049FixtureCounts(shape) {
				t.Fatalf("scenario %s expected %+v, got %+v", shape.name, expectedPR049FixtureCounts(shape), counts)
			}
			if previous, ok := seen[counts]; ok {
				t.Fatalf("scenario %s duplicates fixture shape from %s: %+v", shape.name, previous, counts)
			}
			seen[counts] = shape.name
		}
	}
}

type pr049Scale struct {
	name  string
	shape pr049FixtureShape
}

type pr049BenchmarkScenario struct {
	name string
	ci   pr049FixtureShape
	full pr049FixtureShape
}

type pr049FixtureShape struct {
	name                string
	revisions           int
	targetCount         int
	onlineAgentCount    int
	componentCount      int
	stepCount           int
	stepLogBytes        int
	scopedVariableCount int
	valuesHashBytes     int
	appNameBytes        int
	versionNameBytes    int
}

type pr049FixtureCounts struct {
	Targets         int
	OnlineAgents    int
	Components      int
	Steps           int
	StepLogChunks   int
	StepLogBytes    int
	ScopedVariables int
	ValuesHashBytes int
}

func pr049BenchmarkScales() []pr049Scale {
	full := os.Getenv("PR049_FULL_BENCH") == "1"
	scales := make([]pr049Scale, 0, len(pr049BenchmarkScenarios()))
	for _, scenario := range pr049BenchmarkScenarios() {
		shape := scenario.ci
		name := "ci_" + scenario.name
		if full {
			shape = scenario.full
			name = "full_" + scenario.name
		}
		shape.name = name
		scales = append(scales, pr049Scale{name: name, shape: shape})
	}
	return scales
}

func pr049BenchmarkScenarios() []pr049BenchmarkScenario {
	return []pr049BenchmarkScenario{
		{
			name: "deployment_targets",
			ci:   pr049FixtureShape{name: "ci_deployment_targets", revisions: 10, targetCount: 10, componentCount: 1, stepCount: 1, valuesHashBytes: 64, appNameBytes: 48, versionNameBytes: 32},
			full: pr049FixtureShape{name: "full_deployment_targets", revisions: 1000, targetCount: 1000, componentCount: 1, stepCount: 1, valuesHashBytes: 64, appNameBytes: 48, versionNameBytes: 32},
		},
		{
			name: "online_agents",
			ci:   pr049FixtureShape{name: "ci_online_agents", revisions: 10, targetCount: 10, onlineAgentCount: 10, componentCount: 1, stepCount: 1, valuesHashBytes: 1024, appNameBytes: 64, versionNameBytes: 48},
			full: pr049FixtureShape{name: "full_online_agents", revisions: 100, targetCount: 100, onlineAgentCount: 100, componentCount: 1, stepCount: 1, valuesHashBytes: 1024, appNameBytes: 64, versionNameBytes: 48},
		},
		{
			name: "component_release_bundle",
			ci:   pr049FixtureShape{name: "ci_component_release_bundle", revisions: 10, targetCount: 2, componentCount: 10, stepCount: 1, valuesHashBytes: 4096, appNameBytes: 96, versionNameBytes: 80},
			full: pr049FixtureShape{name: "full_component_release_bundle", revisions: 100, targetCount: 2, componentCount: 100, stepCount: 1, valuesHashBytes: 4096, appNameBytes: 96, versionNameBytes: 80},
		},
		{
			name: "step_aggregate_wave",
			ci:   pr049FixtureShape{name: "ci_step_aggregate_wave", revisions: 10, targetCount: 2, componentCount: 1, stepCount: 10, valuesHashBytes: 8192, appNameBytes: 112, versionNameBytes: 96},
			full: pr049FixtureShape{name: "full_step_aggregate_wave", revisions: 500, targetCount: 10, componentCount: 1, stepCount: 500, valuesHashBytes: 8192, appNameBytes: 112, versionNameBytes: 96},
		},
		{
			name: "large_step_logs",
			ci:   pr049FixtureShape{name: "ci_large_step_logs", revisions: 10, targetCount: 2, componentCount: 1, stepCount: 1, stepLogBytes: 16 * 1024, valuesHashBytes: 8192, appNameBytes: 128, versionNameBytes: 112},
			full: pr049FixtureShape{name: "full_large_step_logs", revisions: 250, targetCount: 5, componentCount: 1, stepCount: 1, stepLogBytes: 64 * 1024, valuesHashBytes: 8192, appNameBytes: 128, versionNameBytes: 112},
		},
		{
			name: "scoped_variable_candidates",
			ci:   pr049FixtureShape{name: "ci_scoped_variable_candidates", revisions: 10, targetCount: 2, componentCount: 1, stepCount: 1, scopedVariableCount: 10, valuesHashBytes: 32768, appNameBytes: 144, versionNameBytes: 128},
			full: pr049FixtureShape{name: "full_scoped_variable_candidates", revisions: 500, targetCount: 10, componentCount: 1, stepCount: 1, scopedVariableCount: 500, valuesHashBytes: 32768, appNameBytes: 144, versionNameBytes: 128},
		},
	}
}

type legacyProjectionFixture struct {
	deployment    types.Deployment
	revision      types.DeploymentRevision
	context       deploymentcompat.ProjectionContext
	targetIDs     []uuid.UUID
	agentReports  []types.AgentCapabilityReport
	components    []types.ReleaseBundleComponent
	steps         []types.DeploymentPlanStep
	stepLogChunks []types.StepRunLogChunk
	scopedValues  []types.VariableScopedValue
}

func newLegacyProjectionFixture(shape pr049FixtureShape, index int) legacyProjectionFixture {
	deploymentID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("%s-deployment-%06d", shape.name, index)))
	targetIDs := scenarioTargetIDs(shape, index)
	return legacyProjectionFixture{
		deployment: types.Deployment{
			Base:               types.Base{ID: deploymentID},
			DeploymentTargetID: targetIDs[index%len(targetIDs)],
		},
		revision: types.DeploymentRevision{
			Base:                 types.Base{ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("%s-revision-%06d", shape.name, index)))},
			DeploymentID:         deploymentID,
			ApplicationVersionID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("%s-version-%06d", shape.name, index))),
			ValuesHash:           scenarioValuesHash(shape, index),
		},
		context: deploymentcompat.ProjectionContext{
			OrganizationID:         uuid.NewSHA1(uuid.NameSpaceOID, []byte("org-"+shape.name)),
			ApplicationID:          uuid.NewSHA1(uuid.NameSpaceOID, []byte("application-"+shape.name)),
			ApplicationName:        scenarioText("Application", shape, index, shape.appNameBytes),
			ApplicationVersionName: scenarioText("Version", shape, index, shape.versionNameBytes),
		},
		targetIDs:     targetIDs,
		agentReports:  scenarioAgentReports(shape, targetIDs),
		components:    scenarioComponents(shape),
		steps:         scenarioSteps(shape),
		stepLogChunks: scenarioStepLogChunks(shape),
		scopedValues:  scenarioScopedValues(shape, targetIDs),
	}
}

func countPR049Fixture(fixture legacyProjectionFixture) pr049FixtureCounts {
	logBytes := 0
	for _, chunk := range fixture.stepLogChunks {
		logBytes += len(chunk.Body)
	}
	return pr049FixtureCounts{
		Targets:         len(fixture.targetIDs),
		OnlineAgents:    len(fixture.agentReports),
		Components:      len(fixture.components),
		Steps:           len(fixture.steps),
		StepLogChunks:   len(fixture.stepLogChunks),
		StepLogBytes:    logBytes,
		ScopedVariables: len(fixture.scopedValues),
		ValuesHashBytes: len(fixture.revision.ValuesHash),
	}
}

func expectedPR049FixtureCounts(shape pr049FixtureShape) pr049FixtureCounts {
	return pr049FixtureCounts{
		Targets:         shape.targetCount,
		OnlineAgents:    shape.onlineAgentCount,
		Components:      shape.componentCount,
		Steps:           shape.stepCount,
		StepLogChunks:   expectedStepLogChunkCount(shape.stepLogBytes),
		StepLogBytes:    shape.stepLogBytes,
		ScopedVariables: shape.scopedVariableCount,
		ValuesHashBytes: shape.valuesHashBytes,
	}
}

func scenarioTargetIDs(shape pr049FixtureShape, index int) []uuid.UUID {
	ids := make([]uuid.UUID, shape.targetCount)
	for i := range ids {
		ids[i] = uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("%s-target-%06d-%06d", shape.name, index, i)))
	}
	return ids
}

func scenarioAgentReports(shape pr049FixtureShape, targetIDs []uuid.UUID) []types.AgentCapabilityReport {
	reports := make([]types.AgentCapabilityReport, shape.onlineAgentCount)
	for i := range reports {
		reports[i] = types.AgentCapabilityReport{
			ID:                   uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("%s-agent-report-%06d", shape.name, i))),
			OrganizationID:       uuid.NewSHA1(uuid.NameSpaceOID, []byte("org-"+shape.name)),
			DeploymentTargetID:   targetIDs[i%len(targetIDs)],
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
	}
	return reports
}

func scenarioComponents(shape pr049FixtureShape) []types.ReleaseBundleComponent {
	components := make([]types.ReleaseBundleComponent, shape.componentCount)
	for i := range components {
		components[i] = types.ReleaseBundleComponent{
			ID:      uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("%s-component-%06d", shape.name, i))),
			Key:     fmt.Sprintf("component-%03d", i),
			Name:    fmt.Sprintf("Component %03d", i),
			Type:    types.ReleaseBundleComponentTypeOCIImage,
			Version: fmt.Sprintf("2026.06.%03d", i),
			PackageRef: fmt.Sprintf(
				"registry.example.test/%s/component-%03d",
				strings.ReplaceAll(shape.name, "_", "-"),
				i,
			),
			Digest: "sha256:" + strings.Repeat(fmt.Sprintf("%x", i%16), 64),
		}
	}
	return components
}

func scenarioSteps(shape pr049FixtureShape) []types.DeploymentPlanStep {
	steps := make([]types.DeploymentPlanStep, shape.stepCount)
	for i := range steps {
		steps[i] = types.DeploymentPlanStep{
			ID:                   uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("%s-step-%06d", shape.name, i))),
			StepKey:              fmt.Sprintf("step-%03d", i),
			Name:                 fmt.Sprintf("Step %03d", i),
			ActionType:           "distr.http.check",
			ActionName:           "HTTP check",
			ExecutionLocation:    "target",
			InputBindings:        map[string]any{"url": fmt.Sprintf("https://example.test/health/%03d", i)},
			FailureMode:          "fail",
			TimeoutSeconds:       300,
			RetryMaxAttempts:     2,
			RetryIntervalSeconds: 5,
			SortOrder:            i + 1,
			Included:             true,
		}
	}
	return steps
}

func scenarioStepLogChunks(shape pr049FixtureShape) []types.StepRunLogChunk {
	chunks := make([]types.StepRunLogChunk, 0, expectedStepLogChunkCount(shape.stepLogBytes))
	remaining := shape.stepLogBytes
	for index := 0; remaining > 0; index++ {
		size := remaining
		if size > types.MaxStepRunLogChunkBodyLength {
			size = types.MaxStepRunLogChunkBodyLength
		}
		chunks = append(chunks, types.StepRunLogChunk{
			ID:         uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("%s-log-%06d", shape.name, index))),
			ChunkIndex: index,
			Stream:     types.StepRunLogStreamStdout,
			Severity:   types.StepRunLogSeverityInfo,
			Body:       strings.Repeat("L", size),
		})
		remaining -= size
	}
	return chunks
}

func scenarioScopedValues(shape pr049FixtureShape, targetIDs []uuid.UUID) []types.VariableScopedValue {
	environmentID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("environment-"+shape.name))
	values := make([]types.VariableScopedValue, shape.scopedVariableCount)
	for i := range values {
		values[i] = types.VariableScopedValue{
			ID:        uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("%s-scoped-variable-%06d", shape.name, i))),
			SortOrder: i + 1,
			Scope: types.VariableScope{
				EnvironmentID: &environmentID,
				TargetTag:     fmt.Sprintf("pr049-%s-%03d", strings.ReplaceAll(shape.name, "_", "-"), i),
			},
			Value: []byte(fmt.Sprintf(`"value-%03d"`, i)),
		}
	}
	return values
}

func expectedStepLogChunkCount(bytes int) int {
	if bytes == 0 {
		return 0
	}
	return (bytes + types.MaxStepRunLogChunkBodyLength - 1) / types.MaxStepRunLogChunkBodyLength
}

func scenarioValuesHash(shape pr049FixtureShape, index int) []byte {
	seed := fmt.Sprintf("%s:%06d:targets=%d:agents=%d:components=%d:steps=%d:variables=%d|", shape.name, index, shape.targetCount, shape.onlineAgentCount, shape.componentCount, shape.stepCount, shape.scopedVariableCount)
	if len(seed) >= shape.valuesHashBytes {
		return []byte(seed[:shape.valuesHashBytes])
	}
	var builder strings.Builder
	builder.Grow(shape.valuesHashBytes)
	for builder.Len() < shape.valuesHashBytes {
		builder.WriteString(seed)
	}
	return []byte(builder.String()[:shape.valuesHashBytes])
}

func scenarioText(prefix string, shape pr049FixtureShape, index int, minLength int) string {
	text := fmt.Sprintf("%s %s %06d targets=%d", prefix, strings.ReplaceAll(shape.name, "_", " "), index, shape.targetCount)
	if len(text) >= minLength {
		return text
	}
	return text + strings.Repeat("x", minLength-len(text))
}

func agentCapabilityRequests(reports []types.AgentCapabilityReport) []api.AgentCapabilitiesRequest {
	requests := make([]api.AgentCapabilitiesRequest, 0, len(reports))
	for _, report := range reports {
		actions := make([]api.AgentActionCapabilityRequest, 0, len(report.SupportedActions))
		for _, action := range report.SupportedActions {
			actions = append(actions, api.AgentActionCapabilityRequest{
				ActionType: action.ActionType,
				Versions:   action.Versions,
			})
		}
		requests = append(requests, api.AgentCapabilitiesRequest{
			ProtocolVersion:      report.ProtocolVersion,
			AgentVersion:         report.AgentVersion,
			SupportedRuntimes:    report.SupportedRuntimes,
			SupportedActions:     actions,
			OperatingSystem:      report.OperatingSystem,
			Architecture:         report.Architecture,
			AvailableTooling:     report.AvailableTooling,
			StrategyCapabilities: report.StrategyCapabilities,
		})
	}
	return requests
}

func recordStepRunLogRequests(chunks []types.StepRunLogChunk) []types.RecordStepRunLogChunkRequest {
	logs := make([]types.RecordStepRunLogChunkRequest, 0, len(chunks))
	for _, chunk := range chunks {
		logs = append(logs, types.RecordStepRunLogChunkRequest{
			Stream:   chunk.Stream,
			Severity: chunk.Severity,
			Body:     chunk.Body,
		})
	}
	return logs
}

func scopedVariableResolutionFixture(fixture legacyProjectionFixture) []types.Variable {
	if len(fixture.scopedValues) == 0 {
		return nil
	}
	variableSetID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("variable-set-"+fixture.context.OrganizationID.String()))
	variableID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("variable-"+fixture.context.OrganizationID.String()))
	scopedValues := make([]types.VariableScopedValue, 0, len(fixture.scopedValues))
	for _, scopedValue := range fixture.scopedValues {
		scopedValue.VariableSetID = variableSetID
		scopedValue.VariableID = variableID
		scopedValues = append(scopedValues, scopedValue)
	}
	return []types.Variable{
		{
			ID:             variableID,
			OrganizationID: fixture.context.OrganizationID,
			VariableSetID:  variableSetID,
			Key:            "api_url",
			Type:           types.VariableTypeString,
			DefaultValue:   []byte(`"https://default.example.test"`),
			ScopedValues:   scopedValues,
		},
	}
}

func scopedVariableEnvironmentID(values []types.VariableScopedValue) *uuid.UUID {
	if len(values) == 0 {
		return nil
	}
	return values[0].Scope.EnvironmentID
}
func scopedVariableTargetTags(values []types.VariableScopedValue) []string {
	tags := make([]string, 0, len(values))
	for _, value := range values {
		tags = append(tags, value.Scope.TargetTag)
	}
	return tags
}

func countScopedValues(variables []types.Variable) int {
	count := 0
	for _, variable := range variables {
		count += len(variable.ScopedValues)
	}
	return count
}
