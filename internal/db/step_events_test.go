package db_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	. "github.com/onsi/gomega"
)

func TestStepEventRepositoryRecordsEventLogsAndOutputs(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	fixture := createStepEventFixture(t, ctx)

	event, err := db.RecordAgentStepRunEvent(ctx, types.RecordAgentStepRunEventRequest{
		OrganizationID: fixture.orgID,
		AgentID:        fixture.agentID,
		StepRunID:      fixture.stepRunID,
		LeaseToken:     fixture.leaseToken,
		Sequence:       1,
		Type:           types.StepRunEventTypeStarted,
		Message:        "Authorization: Bearer abc123",
		Details: map[string]any{
			"token":   "secret",
			"message": "password=plaintext",
		},
		Logs: []types.RecordStepRunLogChunkRequest{
			{
				Stream:   types.StepRunLogStreamStdout,
				Severity: types.StepRunLogSeverityInfo,
				Body:     "password=plaintext",
			},
		},
		Outputs: []types.RecordStepRunOutputRequest{
			{Name: "url", Value: "https://example.com"},
			{Name: "token", Value: "plain-token", Sensitive: true},
		},
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(event.Sequence).To(Equal(int64(1)))
	g.Expect(event.Type).To(Equal(types.StepRunEventTypeStarted))
	g.Expect(event.Message).To(Equal("Authorization: Bearer [REDACTED]"))
	g.Expect(event.Redacted).To(BeTrue())
	g.Expect(event.Logs).To(HaveLen(1))
	g.Expect(event.Logs[0].Body).To(Equal("password=[REDACTED]"))
	g.Expect(event.Logs[0].Redacted).To(BeTrue())
	g.Expect(event.Outputs).To(HaveLen(2))
	g.Expect(event.Outputs).To(ContainElement(WithTransform(
		func(output types.StepRunOutput) string { return output.Name },
		Equal("url"),
	)))
	var tokenOutput *types.StepRunOutput
	for i := range event.Outputs {
		if event.Outputs[i].Name == "token" {
			tokenOutput = &event.Outputs[i]
			break
		}
	}
	g.Expect(tokenOutput).NotTo(BeNil())
	g.Expect(tokenOutput.Value).To(BeNil())
	g.Expect(tokenOutput.Redacted).To(BeTrue())

	task, err := db.GetTask(ctx, fixture.taskID, fixture.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(task.Status).To(Equal(types.TaskStatusRunning))
	g.Expect(task.StepRuns[0].Status).To(Equal(types.StepRunStatusRunning))
	g.Expect(task.StepRuns[0].StartedAt).NotTo(BeNil())

	timeline, err := db.GetTaskTimeline(ctx, fixture.taskID, fixture.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(timeline.TaskID).To(Equal(fixture.taskID))
	g.Expect(timeline.Events).To(HaveLen(1))
	g.Expect(timeline.Events[0].ID).To(Equal(event.ID))

	logs, err := db.GetTaskLogs(ctx, fixture.taskID, fixture.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(logs).To(HaveLen(1))
	g.Expect(logs[0].Body).To(Equal("password=[REDACTED]"))
}

func TestStepEventRepositoryRedactsResolvedComposeRegistrySecretFromEventsLogsAndOutputs(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	const secretValue = "super-secret-password"
	fixture := createComposeRegistrySecretStepEventFixture(t, ctx, secretValue)

	event, err := db.RecordAgentStepRunEvent(ctx, types.RecordAgentStepRunEventRequest{
		OrganizationID: fixture.orgID,
		AgentID:        fixture.agentID,
		StepRunID:      fixture.stepRunID,
		LeaseToken:     fixture.leaseToken,
		Sequence:       1,
		Type:           types.StepRunEventTypeStarted,
		Message:        "pull failed: " + secretValue,
		Details: map[string]any{
			"error": "registry returned " + secretValue,
		},
		Logs: []types.RecordStepRunLogChunkRequest{
			{
				Stream:   types.StepRunLogStreamStderr,
				Severity: types.StepRunLogSeverityError,
				Body:     "stderr contains " + secretValue,
			},
		},
		Outputs: []types.RecordStepRunOutputRequest{
			{
				Name: "diagnostics",
				Value: map[string]any{
					"message": "output contains " + secretValue,
				},
			},
		},
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(event.Redacted).To(BeTrue())
	g.Expect(event.Message).To(Equal("pull failed: [REDACTED]"))
	g.Expect(event.Details["error"]).To(Equal("registry returned [REDACTED]"))
	g.Expect(event.Logs).To(HaveLen(1))
	g.Expect(event.Logs[0].Body).To(Equal("stderr contains [REDACTED]"))
	g.Expect(event.Logs[0].Redacted).To(BeTrue())
	g.Expect(event.Outputs).To(HaveLen(1))
	g.Expect(string(event.Outputs[0].Value)).To(ContainSubstring("[REDACTED]"))
	g.Expect(string(event.Outputs[0].Value)).NotTo(ContainSubstring(secretValue))
	eventPayload, err := json.Marshal(event)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(eventPayload)).NotTo(ContainSubstring(secretValue))

	timeline, err := db.GetTaskTimeline(ctx, fixture.taskID, fixture.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	timelinePayload, err := json.Marshal(timeline)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(timelinePayload)).NotTo(ContainSubstring(secretValue))

	logs, err := db.GetTaskLogs(ctx, fixture.taskID, fixture.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	logsPayload, err := json.Marshal(logs)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(logsPayload)).NotTo(ContainSubstring(secretValue))
}

func TestStepEventRepositoryRedactsResolvedOCIJobSecretFromEventsLogsAndOutputs(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	const secretValue = "super-secret-job-token"
	fixture := createOCIJobSecretStepEventFixture(t, ctx, secretValue)

	event, err := db.RecordAgentStepRunEvent(ctx, types.RecordAgentStepRunEventRequest{
		OrganizationID: fixture.orgID,
		AgentID:        fixture.agentID,
		StepRunID:      fixture.stepRunID,
		LeaseToken:     fixture.leaseToken,
		Sequence:       1,
		Type:           types.StepRunEventTypeStarted,
		Message:        "job failed with " + secretValue,
		Details: map[string]any{
			"error": "job stderr returned " + secretValue,
		},
		Logs: []types.RecordStepRunLogChunkRequest{
			{
				Stream:   types.StepRunLogStreamStderr,
				Severity: types.StepRunLogSeverityError,
				Body:     "stderr contains " + secretValue,
			},
		},
		Outputs: []types.RecordStepRunOutputRequest{
			{
				Name: "diagnostics",
				Value: map[string]any{
					"message": "output contains " + secretValue,
				},
			},
		},
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(event.Redacted).To(BeTrue())
	g.Expect(event.Message).To(Equal("job failed with [REDACTED]"))
	g.Expect(event.Details["error"]).To(Equal("job stderr returned [REDACTED]"))
	g.Expect(event.Logs).To(HaveLen(1))
	g.Expect(event.Logs[0].Body).To(Equal("stderr contains [REDACTED]"))
	g.Expect(event.Logs[0].Redacted).To(BeTrue())
	g.Expect(event.Outputs).To(HaveLen(1))
	g.Expect(string(event.Outputs[0].Value)).To(ContainSubstring("[REDACTED]"))
	g.Expect(string(event.Outputs[0].Value)).NotTo(ContainSubstring(secretValue))
	eventPayload, err := json.Marshal(event)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(eventPayload)).NotTo(ContainSubstring(secretValue))

	timeline, err := db.GetTaskTimeline(ctx, fixture.taskID, fixture.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	timelinePayload, err := json.Marshal(timeline)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(timelinePayload)).NotTo(ContainSubstring(secretValue))

	logs, err := db.GetTaskLogs(ctx, fixture.taskID, fixture.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	logsPayload, err := json.Marshal(logs)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(logsPayload)).NotTo(ContainSubstring(secretValue))
}

func TestStepEventRepositoryRedactsResolvedFileRenderSecretFromEventsLogsAndOutputs(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	const secretValue = "render-value-9274"
	fixture := createFileRenderSecretStepEventFixture(t, ctx, secretValue)

	event, err := db.RecordAgentStepRunEvent(ctx, types.RecordAgentStepRunEventRequest{
		OrganizationID: fixture.orgID,
		AgentID:        fixture.agentID,
		StepRunID:      fixture.stepRunID,
		LeaseToken:     fixture.leaseToken,
		Sequence:       1,
		Type:           types.StepRunEventTypeStarted,
		Message:        "render failed with " + secretValue,
		Details: map[string]any{
			"error": "render stderr returned " + secretValue,
		},
		Logs: []types.RecordStepRunLogChunkRequest{
			{
				Stream:   types.StepRunLogStreamStderr,
				Severity: types.StepRunLogSeverityError,
				Body:     "stderr contains " + secretValue,
			},
		},
		Outputs: []types.RecordStepRunOutputRequest{
			{
				Name: "diagnostics",
				Value: map[string]any{
					"message": "output contains " + secretValue,
				},
			},
		},
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(event.Redacted).To(BeTrue())
	g.Expect(event.Message).To(Equal("render failed with [REDACTED]"))
	g.Expect(event.Details["error"]).To(Equal("render stderr returned [REDACTED]"))
	g.Expect(event.Logs).To(HaveLen(1))
	g.Expect(event.Logs[0].Body).To(Equal("stderr contains [REDACTED]"))
	g.Expect(event.Logs[0].Redacted).To(BeTrue())
	g.Expect(event.Outputs).To(HaveLen(1))
	g.Expect(string(event.Outputs[0].Value)).To(ContainSubstring("[REDACTED]"))
	g.Expect(string(event.Outputs[0].Value)).NotTo(ContainSubstring(secretValue))
	eventPayload, err := json.Marshal(event)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(eventPayload)).NotTo(ContainSubstring(secretValue))

	timeline, err := db.GetTaskTimeline(ctx, fixture.taskID, fixture.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	timelinePayload, err := json.Marshal(timeline)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(timelinePayload)).NotTo(ContainSubstring(secretValue))

	logs, err := db.GetTaskLogs(ctx, fixture.taskID, fixture.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	logsPayload, err := json.Marshal(logs)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(logsPayload)).NotTo(ContainSubstring(secretValue))
}

func TestStepEventRepositoryRedactsResolvedWebhookSecretsFromEventsLogsAndOutputs(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	const headerSecret = "Bearer webhook-value-9274"
	const signingSecret = "webhook-signing-value-9274"
	fixture := createWebhookSecretStepEventFixture(t, ctx, headerSecret, signingSecret)

	event, err := db.RecordAgentStepRunEvent(ctx, types.RecordAgentStepRunEventRequest{
		OrganizationID: fixture.orgID,
		AgentID:        fixture.agentID,
		StepRunID:      fixture.stepRunID,
		LeaseToken:     fixture.leaseToken,
		Sequence:       1,
		Type:           types.StepRunEventTypeStarted,
		Message:        "webhook failed with " + headerSecret,
		Details: map[string]any{
			"error":     "webhook stderr returned " + signingSecret,
			"signature": signingSecret,
		},
		Logs: []types.RecordStepRunLogChunkRequest{
			{
				Stream:   types.StepRunLogStreamStderr,
				Severity: types.StepRunLogSeverityError,
				Body:     "stderr contains " + headerSecret + " and " + signingSecret,
			},
		},
		Outputs: []types.RecordStepRunOutputRequest{
			{
				Name: "diagnostics",
				Value: map[string]any{
					"message": "output contains " + headerSecret,
					"signing": signingSecret,
				},
			},
		},
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(event.Redacted).To(BeTrue())
	g.Expect(event.Message).To(Equal("webhook failed with [REDACTED]"))
	g.Expect(event.Details["error"]).To(Equal("webhook stderr returned [REDACTED]"))
	g.Expect(event.Details["signature"]).To(Equal("[REDACTED]"))
	g.Expect(event.Logs).To(HaveLen(1))
	g.Expect(event.Logs[0].Body).To(Equal("stderr contains [REDACTED] and [REDACTED]"))
	g.Expect(event.Logs[0].Redacted).To(BeTrue())
	g.Expect(event.Outputs).To(HaveLen(1))
	g.Expect(string(event.Outputs[0].Value)).To(ContainSubstring("[REDACTED]"))
	g.Expect(string(event.Outputs[0].Value)).NotTo(ContainSubstring(headerSecret))
	g.Expect(string(event.Outputs[0].Value)).NotTo(ContainSubstring(signingSecret))
	eventPayload, err := json.Marshal(event)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(eventPayload)).NotTo(ContainSubstring(headerSecret))
	g.Expect(string(eventPayload)).NotTo(ContainSubstring(signingSecret))

	timeline, err := db.GetTaskTimeline(ctx, fixture.taskID, fixture.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	timelinePayload, err := json.Marshal(timeline)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(timelinePayload)).NotTo(ContainSubstring(headerSecret))
	g.Expect(string(timelinePayload)).NotTo(ContainSubstring(signingSecret))

	logs, err := db.GetTaskLogs(ctx, fixture.taskID, fixture.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	logsPayload, err := json.Marshal(logs)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(logsPayload)).NotTo(ContainSubstring(headerSecret))
	g.Expect(string(logsPayload)).NotTo(ContainSubstring(signingSecret))
}

func TestStepEventRepositoryRedactsWebhookSignaturesFromEventsLogsAndOutputs(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	const headerSecret = "Bearer webhook-value-9274"
	const signingSecret = "webhook-signing-value-9274"
	const signature = "sha256=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	fixture := createWebhookSecretStepEventFixture(t, ctx, headerSecret, signingSecret)

	event, err := db.RecordAgentStepRunEvent(ctx, types.RecordAgentStepRunEventRequest{
		OrganizationID: fixture.orgID,
		AgentID:        fixture.agentID,
		StepRunID:      fixture.stepRunID,
		LeaseToken:     fixture.leaseToken,
		Sequence:       1,
		Type:           types.StepRunEventTypeStarted,
		Message:        "webhook failed with signature " + signature,
		Details: map[string]any{
			"error":     "webhook stderr returned " + signature,
			"signature": signature,
		},
		Logs: []types.RecordStepRunLogChunkRequest{
			{
				Stream:   types.StepRunLogStreamStderr,
				Severity: types.StepRunLogSeverityError,
				Body:     "stderr contains " + signature,
			},
		},
		Outputs: []types.RecordStepRunOutputRequest{
			{
				Name: "diagnostics",
				Value: map[string]any{
					"message": "output contains " + signature,
				},
			},
		},
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(event.Redacted).To(BeTrue())
	g.Expect(event.Message).To(Equal("webhook failed with signature [REDACTED]"))
	g.Expect(event.Details["error"]).To(Equal("webhook stderr returned [REDACTED]"))
	g.Expect(event.Details["signature"]).To(Equal("[REDACTED]"))
	g.Expect(event.Logs).To(HaveLen(1))
	g.Expect(event.Logs[0].Body).To(Equal("stderr contains [REDACTED]"))
	g.Expect(event.Outputs).To(HaveLen(1))
	g.Expect(string(event.Outputs[0].Value)).To(ContainSubstring("[REDACTED]"))
	g.Expect(string(event.Outputs[0].Value)).NotTo(ContainSubstring(signature))
	eventPayload, err := json.Marshal(event)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(eventPayload)).NotTo(ContainSubstring(signature))

	timeline, err := db.GetTaskTimeline(ctx, fixture.taskID, fixture.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	timelinePayload, err := json.Marshal(timeline)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(timelinePayload)).NotTo(ContainSubstring(signature))

	logs, err := db.GetTaskLogs(ctx, fixture.taskID, fixture.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	logsPayload, err := json.Marshal(logs)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(logsPayload)).NotTo(ContainSubstring(signature))
}

func TestStepEventRepositoryRedactsWireNormalizedWebhookSecretHeaderValue(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	const headerSecret = " secret-value "
	const normalizedHeaderSecret = "secret-value"
	const signingSecret = "webhook-signing-value-9274"
	fixture := createWebhookSecretStepEventFixture(t, ctx, headerSecret, signingSecret)

	event, err := db.RecordAgentStepRunEvent(ctx, types.RecordAgentStepRunEventRequest{
		OrganizationID: fixture.orgID,
		AgentID:        fixture.agentID,
		StepRunID:      fixture.stepRunID,
		LeaseToken:     fixture.leaseToken,
		Sequence:       1,
		Type:           types.StepRunEventTypeStarted,
		Message:        "webhook echoed " + normalizedHeaderSecret,
		Details: map[string]any{
			"error": "webhook stderr returned " + normalizedHeaderSecret,
		},
		Logs: []types.RecordStepRunLogChunkRequest{
			{
				Stream:   types.StepRunLogStreamStderr,
				Severity: types.StepRunLogSeverityError,
				Body:     "stderr contains " + normalizedHeaderSecret,
			},
		},
		Outputs: []types.RecordStepRunOutputRequest{
			{
				Name: "diagnostics",
				Value: map[string]any{
					"message": "output contains " + normalizedHeaderSecret,
				},
			},
		},
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(event.Redacted).To(BeTrue())
	g.Expect(event.Message).To(Equal("webhook echoed [REDACTED]"))
	g.Expect(event.Details["error"]).To(Equal("webhook stderr returned [REDACTED]"))
	g.Expect(event.Logs).To(HaveLen(1))
	g.Expect(event.Logs[0].Body).To(Equal("stderr contains [REDACTED]"))
	g.Expect(event.Outputs).To(HaveLen(1))
	g.Expect(string(event.Outputs[0].Value)).To(ContainSubstring("[REDACTED]"))
	g.Expect(string(event.Outputs[0].Value)).NotTo(ContainSubstring(normalizedHeaderSecret))
	eventPayload, err := json.Marshal(event)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(eventPayload)).NotTo(ContainSubstring(normalizedHeaderSecret))

	timeline, err := db.GetTaskTimeline(ctx, fixture.taskID, fixture.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	timelinePayload, err := json.Marshal(timeline)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(timelinePayload)).NotTo(ContainSubstring(normalizedHeaderSecret))

	logs, err := db.GetTaskLogs(ctx, fixture.taskID, fixture.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	logsPayload, err := json.Marshal(logs)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(logsPayload)).NotTo(ContainSubstring(normalizedHeaderSecret))
}

func TestStepEventRepositoryReplaysSameSequenceIdempotently(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	fixture := createStepEventFixture(t, ctx)
	request := types.RecordAgentStepRunEventRequest{
		OrganizationID: fixture.orgID,
		AgentID:        fixture.agentID,
		StepRunID:      fixture.stepRunID,
		LeaseToken:     fixture.leaseToken,
		Sequence:       1,
		Type:           types.StepRunEventTypeStarted,
		Message:        "started",
	}

	first, err := db.RecordAgentStepRunEvent(ctx, request)
	g.Expect(err).NotTo(HaveOccurred())
	second, err := db.RecordAgentStepRunEvent(ctx, request)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(second.ID).To(Equal(first.ID))
	g.Expect(countStepRunEventsForTest(t, ctx, fixture.stepRunID)).To(Equal(1))
}

func TestStepEventRepositoryRejectsChangedReplayForSameSequence(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	fixture := createStepEventFixture(t, ctx)
	request := types.RecordAgentStepRunEventRequest{
		OrganizationID: fixture.orgID,
		AgentID:        fixture.agentID,
		StepRunID:      fixture.stepRunID,
		LeaseToken:     fixture.leaseToken,
		Sequence:       1,
		Type:           types.StepRunEventTypeStarted,
		Message:        "started",
		Logs: []types.RecordStepRunLogChunkRequest{
			{Stream: types.StepRunLogStreamStdout, Severity: types.StepRunLogSeverityInfo, Body: "started"},
		},
		Outputs: []types.RecordStepRunOutputRequest{
			{Name: "digest", Value: "sha256:first"},
		},
	}

	first, err := db.RecordAgentStepRunEvent(ctx, request)
	g.Expect(err).NotTo(HaveOccurred())
	request.Message = "changed"
	request.Outputs[0].Value = "sha256:changed"
	second, err := db.RecordAgentStepRunEvent(ctx, request)

	g.Expect(second).To(BeNil())
	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
	g.Expect(countStepRunEventsForTest(t, ctx, fixture.stepRunID)).To(Equal(1))
	g.Expect(countStepRunOutputRowsForTest(t, ctx, fixture.stepRunID)).To(Equal(1))
	replayed, err := db.RecordAgentStepRunEvent(ctx, types.RecordAgentStepRunEventRequest{
		OrganizationID: fixture.orgID,
		AgentID:        fixture.agentID,
		StepRunID:      fixture.stepRunID,
		LeaseToken:     fixture.leaseToken,
		Sequence:       1,
		Type:           types.StepRunEventTypeStarted,
		Message:        "started",
		Logs: []types.RecordStepRunLogChunkRequest{
			{Stream: types.StepRunLogStreamStdout, Severity: types.StepRunLogSeverityInfo, Body: "started"},
		},
		Outputs: []types.RecordStepRunOutputRequest{
			{Name: "digest", Value: "sha256:first"},
		},
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(replayed.ID).To(Equal(first.ID))
}

func TestStepEventRepositoryRejectsOutOfOrderSequences(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	fixture := createStepEventFixture(t, ctx)

	_, err := db.RecordAgentStepRunEvent(ctx, types.RecordAgentStepRunEventRequest{
		OrganizationID: fixture.orgID,
		AgentID:        fixture.agentID,
		StepRunID:      fixture.stepRunID,
		LeaseToken:     fixture.leaseToken,
		Sequence:       2,
		Type:           types.StepRunEventTypeStarted,
	})

	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
}

func TestStepEventRepositoryKeepsOutputsImmutablePerEvent(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	fixture := createStepEventFixture(t, ctx)
	_, err := db.RecordAgentStepRunEvent(ctx, types.RecordAgentStepRunEventRequest{
		OrganizationID: fixture.orgID,
		AgentID:        fixture.agentID,
		StepRunID:      fixture.stepRunID,
		LeaseToken:     fixture.leaseToken,
		Sequence:       1,
		Type:           types.StepRunEventTypeStarted,
	})
	g.Expect(err).NotTo(HaveOccurred())
	firstOutput, err := db.RecordAgentStepRunEvent(ctx, types.RecordAgentStepRunEventRequest{
		OrganizationID: fixture.orgID,
		AgentID:        fixture.agentID,
		StepRunID:      fixture.stepRunID,
		LeaseToken:     fixture.leaseToken,
		Sequence:       2,
		Type:           types.StepRunEventTypeOutput,
		Outputs: []types.RecordStepRunOutputRequest{
			{Name: "digest", Value: "sha256:first"},
		},
	})
	g.Expect(err).NotTo(HaveOccurred())
	secondOutput, err := db.RecordAgentStepRunEvent(ctx, types.RecordAgentStepRunEventRequest{
		OrganizationID: fixture.orgID,
		AgentID:        fixture.agentID,
		StepRunID:      fixture.stepRunID,
		LeaseToken:     fixture.leaseToken,
		Sequence:       3,
		Type:           types.StepRunEventTypeOutput,
		Outputs: []types.RecordStepRunOutputRequest{
			{Name: "digest", Value: "sha256:second"},
		},
	})
	g.Expect(err).NotTo(HaveOccurred())

	timeline, err := db.GetTaskTimeline(ctx, fixture.taskID, fixture.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	eventsByID := map[uuid.UUID]types.StepRunEvent{}
	for _, event := range timeline.Events {
		eventsByID[event.ID] = event
	}
	g.Expect(eventsByID[firstOutput.ID].Outputs).To(HaveLen(1))
	g.Expect(eventsByID[firstOutput.ID].Outputs[0].Name).To(Equal("digest"))
	g.Expect(string(eventsByID[firstOutput.ID].Outputs[0].Value)).To(Equal(`"sha256:first"`))
	g.Expect(eventsByID[secondOutput.ID].Outputs).To(HaveLen(1))
	g.Expect(string(eventsByID[secondOutput.ID].Outputs[0].Value)).To(Equal(`"sha256:second"`))
	g.Expect(countStepRunOutputRowsForTest(t, ctx, fixture.stepRunID)).To(Equal(2))
}

func TestStepEventRepositoryOrdersTimelineAndLogsByLeaseAttempt(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	fixture := createStepEventFixture(t, ctx)
	_, err := db.RecordAgentStepRunEvent(ctx, types.RecordAgentStepRunEventRequest{
		OrganizationID: fixture.orgID,
		AgentID:        fixture.agentID,
		StepRunID:      fixture.stepRunID,
		LeaseToken:     fixture.leaseToken,
		Sequence:       1,
		Type:           types.StepRunEventTypeStarted,
		Message:        "attempt-1 started",
		Logs: []types.RecordStepRunLogChunkRequest{
			{Stream: types.StepRunLogStreamStdout, Severity: types.StepRunLogSeverityInfo, Body: "attempt-1 started"},
		},
	})
	g.Expect(err).NotTo(HaveOccurred())
	_, err = db.RecordAgentStepRunEvent(ctx, types.RecordAgentStepRunEventRequest{
		OrganizationID: fixture.orgID,
		AgentID:        fixture.agentID,
		StepRunID:      fixture.stepRunID,
		LeaseToken:     fixture.leaseToken,
		Sequence:       2,
		Type:           types.StepRunEventTypeLog,
		Message:        "attempt-1 continued",
		Logs: []types.RecordStepRunLogChunkRequest{
			{Stream: types.StepRunLogStreamStdout, Severity: types.StepRunLogSeverityInfo, Body: "attempt-1 continued"},
		},
	})
	g.Expect(err).NotTo(HaveOccurred())
	expireTaskLeaseForTest(t, ctx, fixture.leaseID)
	nextLease, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: fixture.orgID,
		AgentID:        fixture.agentID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	_, err = db.RecordAgentStepRunEvent(ctx, types.RecordAgentStepRunEventRequest{
		OrganizationID: fixture.orgID,
		AgentID:        fixture.agentID,
		StepRunID:      fixture.stepRunID,
		LeaseToken:     nextLease.LeaseToken,
		Sequence:       1,
		Type:           types.StepRunEventTypeStarted,
		Message:        "attempt-2 restarted",
		Logs: []types.RecordStepRunLogChunkRequest{
			{Stream: types.StepRunLogStreamStdout, Severity: types.StepRunLogSeverityInfo, Body: "attempt-2 restarted"},
		},
	})
	g.Expect(err).NotTo(HaveOccurred())

	timeline, err := db.GetTaskTimeline(ctx, fixture.taskID, fixture.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(timeline.Events).To(HaveLen(3))
	g.Expect([]string{
		timeline.Events[0].Message,
		timeline.Events[1].Message,
		timeline.Events[2].Message,
	}).To(Equal([]string{"attempt-1 started", "attempt-1 continued", "attempt-2 restarted"}))
	logs, err := db.GetTaskLogs(ctx, fixture.taskID, fixture.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(logs).To(HaveLen(3))
	g.Expect([]string{logs[0].Body, logs[1].Body, logs[2].Body}).
		To(Equal([]string{"attempt-1 started", "attempt-1 continued", "attempt-2 restarted"}))
}

func TestStepEventRepositoryRejectsCumulativeDistinctOutputNameLimit(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	fixture := createStepEventFixture(t, ctx)
	_, err := db.RecordAgentStepRunEvent(ctx, types.RecordAgentStepRunEventRequest{
		OrganizationID: fixture.orgID,
		AgentID:        fixture.agentID,
		StepRunID:      fixture.stepRunID,
		LeaseToken:     fixture.leaseToken,
		Sequence:       1,
		Type:           types.StepRunEventTypeStarted,
	})
	g.Expect(err).NotTo(HaveOccurred())
	outputs := make([]types.RecordStepRunOutputRequest, 0, types.MaxStepRunEventOutputItemCount)
	for i := 0; i < types.MaxStepRunEventOutputItemCount; i++ {
		outputs = append(outputs, types.RecordStepRunOutputRequest{
			Name:  fmt.Sprintf("output-%02d", i),
			Value: i,
		})
	}
	_, err = db.RecordAgentStepRunEvent(ctx, types.RecordAgentStepRunEventRequest{
		OrganizationID: fixture.orgID,
		AgentID:        fixture.agentID,
		StepRunID:      fixture.stepRunID,
		LeaseToken:     fixture.leaseToken,
		Sequence:       2,
		Type:           types.StepRunEventTypeOutput,
		Outputs:        outputs,
	})
	g.Expect(err).NotTo(HaveOccurred())
	_, err = db.RecordAgentStepRunEvent(ctx, types.RecordAgentStepRunEventRequest{
		OrganizationID: fixture.orgID,
		AgentID:        fixture.agentID,
		StepRunID:      fixture.stepRunID,
		LeaseToken:     fixture.leaseToken,
		Sequence:       3,
		Type:           types.StepRunEventTypeOutput,
		Outputs: []types.RecordStepRunOutputRequest{
			{Name: "output-32", Value: 32},
		},
	})

	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
	g.Expect(countStepRunOutputRowsForTest(t, ctx, fixture.stepRunID)).
		To(Equal(types.MaxStepRunEventOutputItemCount))
}

func TestStepEventRepositoryPreservesOrganizationAndAgentIsolation(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	fixture := createStepEventFixture(t, ctx)
	otherOrgID := createReleaseBundleTestOrganization(t, ctx)

	_, err := db.RecordAgentStepRunEvent(ctx, types.RecordAgentStepRunEventRequest{
		OrganizationID: otherOrgID,
		AgentID:        fixture.agentID,
		StepRunID:      fixture.stepRunID,
		LeaseToken:     fixture.leaseToken,
		Sequence:       1,
		Type:           types.StepRunEventTypeStarted,
	})
	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())

	_, err = db.RecordAgentStepRunEvent(ctx, types.RecordAgentStepRunEventRequest{
		OrganizationID: fixture.orgID,
		AgentID:        uuid.New(),
		StepRunID:      fixture.stepRunID,
		LeaseToken:     fixture.leaseToken,
		Sequence:       1,
		Type:           types.StepRunEventTypeStarted,
	})
	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())
}

func TestStepEventRepositoryRejectsExpiredLease(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	fixture := createStepEventFixture(t, ctx)
	expireTaskLeaseForTest(t, ctx, fixture.leaseID)

	_, err := db.RecordAgentStepRunEvent(ctx, types.RecordAgentStepRunEventRequest{
		OrganizationID: fixture.orgID,
		AgentID:        fixture.agentID,
		StepRunID:      fixture.stepRunID,
		LeaseToken:     fixture.leaseToken,
		Sequence:       1,
		Type:           types.StepRunEventTypeStarted,
	})

	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
}

func TestStepEventRepositoryCompletesStepRunAndTaskOnSucceededEvent(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	fixture := createStepEventFixture(t, ctx)
	_, err := db.RecordAgentStepRunEvent(ctx, types.RecordAgentStepRunEventRequest{
		OrganizationID: fixture.orgID,
		AgentID:        fixture.agentID,
		StepRunID:      fixture.stepRunID,
		LeaseToken:     fixture.leaseToken,
		Sequence:       1,
		Type:           types.StepRunEventTypeStarted,
	})
	g.Expect(err).NotTo(HaveOccurred())

	_, err = db.RecordAgentStepRunEvent(ctx, types.RecordAgentStepRunEventRequest{
		OrganizationID: fixture.orgID,
		AgentID:        fixture.agentID,
		StepRunID:      fixture.stepRunID,
		LeaseToken:     fixture.leaseToken,
		Sequence:       2,
		Type:           types.StepRunEventTypeSucceeded,
	})

	g.Expect(err).NotTo(HaveOccurred())
	task, err := db.GetTask(ctx, fixture.taskID, fixture.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(task.Status).To(Equal(types.TaskStatusSucceeded))
	g.Expect(task.CompletedAt).NotTo(BeNil())
	g.Expect(task.StepRuns[0].Status).To(Equal(types.StepRunStatusSucceeded))
	g.Expect(countActiveTaskLeasesForTest(t, ctx, fixture.taskID)).To(Equal(0))
}

func TestStepEventRepositoryFailsTaskAndStopsLeasingAfterStartedFailedEvents(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	fixture := createStepEventFixture(t, ctx)
	_, err := db.RecordAgentStepRunEvent(ctx, types.RecordAgentStepRunEventRequest{
		OrganizationID: fixture.orgID,
		AgentID:        fixture.agentID,
		StepRunID:      fixture.stepRunID,
		LeaseToken:     fixture.leaseToken,
		Sequence:       1,
		Type:           types.StepRunEventTypeStarted,
		Message:        "started before validation",
	})
	g.Expect(err).NotTo(HaveOccurred())

	_, err = db.RecordAgentStepRunEvent(ctx, types.RecordAgentStepRunEventRequest{
		OrganizationID: fixture.orgID,
		AgentID:        fixture.agentID,
		StepRunID:      fixture.stepRunID,
		LeaseToken:     fixture.leaseToken,
		Sequence:       2,
		Type:           types.StepRunEventTypeFailed,
		Message:        "validation failed",
	})

	g.Expect(err).NotTo(HaveOccurred())
	task, err := db.GetTask(ctx, fixture.taskID, fixture.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(task.Status).To(Equal(types.TaskStatusFailed))
	g.Expect(task.CompletedAt).NotTo(BeNil())
	g.Expect(task.StepRuns[0].Status).To(Equal(types.StepRunStatusFailed))
	g.Expect(countActiveTaskLeasesForTest(t, ctx, fixture.taskID)).To(Equal(0))
	nextLease, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: fixture.orgID,
		AgentID:        fixture.agentID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(nextLease).To(BeNil())
}

func TestStepEventMigrationDefinesEventLogAndOutputTables(t *testing.T) {
	g := NewWithT(t)
	upSQL := readStepEventMigrationForTest(t, "up")
	g.Expect(upSQL).To(ContainSubstring("CREATE TABLE StepRunEvent"))
	g.Expect(upSQL).To(ContainSubstring("CREATE TABLE StepRunLogChunk"))
	g.Expect(upSQL).To(ContainSubstring("CREATE TABLE StepRunOutput"))
	g.Expect(upSQL).To(ContainSubstring("UNIQUE (step_run_id, task_lease_id, sequence)"))
	g.Expect(upSQL).To(ContainSubstring("FOREIGN KEY (task_lease_id, task_id, agent_id, organization_id)"))
	g.Expect(upSQL).To(ContainSubstring("UNIQUE (event_id, name)"))
	g.Expect(upSQL).To(ContainSubstring("octet_length(body) <="))

	downSQL := readStepEventMigrationForTest(t, "down")
	g.Expect(downSQL).To(ContainSubstring("DROP TABLE IF EXISTS StepRunOutput"))
	g.Expect(downSQL).To(ContainSubstring("DROP TABLE IF EXISTS StepRunLogChunk"))
	g.Expect(downSQL).To(ContainSubstring("DROP TABLE IF EXISTS StepRunEvent"))
}

func readStepEventMigrationForTest(t *testing.T, direction string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "125_step_events."+direction+".sql"))
	if err != nil {
		t.Fatalf("read step event %s migration: %v", direction, err)
	}
	return string(data)
}

type stepEventFixture struct {
	orgID      uuid.UUID
	agentID    uuid.UUID
	taskID     uuid.UUID
	stepRunID  uuid.UUID
	leaseID    uuid.UUID
	leaseToken string
}

func createStepEventFixture(t *testing.T, ctx context.Context) stepEventFixture {
	t.Helper()
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskLease(t, ctx)
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	lease, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	return stepEventFixture{
		orgID:      deps.orgID,
		agentID:    tasks[0].DeploymentTargetID,
		taskID:     tasks[0].ID,
		stepRunID:  tasks[0].StepRuns[0].ID,
		leaseID:    lease.ID,
		leaseToken: lease.LeaseToken,
	}
}

func createComposeRegistrySecretStepEventFixture(
	t *testing.T,
	ctx context.Context,
	secretValue string,
) stepEventFixture {
	t.Helper()
	g := NewWithT(t)
	compose := taskLeaseComposeDeployStep("compose", "Compose deploy", 10)
	compose.InputBindings = map[string]any{
		"applicationVersion": map[string]any{
			"composeFile": "services:\n  web:\n    image: registry.example.com/app:latest\n",
			"registryAuth": map[string]any{
				"registry.example.com": map[string]any{
					"username":          "deploy-user",
					"passwordSecretRef": "docker_password",
				},
			},
		},
		"projectName": "step-event-registry-secret",
	}
	deps := createReadyDeploymentPlanForTaskLeaseWithSteps(t, ctx, []types.DeploymentProcessStep{compose})
	_, err := internalctx.GetDb(ctx).Exec(
		ctx,
		`INSERT INTO Secret (organization_id, key, value, updated_by_useraccount_id)
		VALUES (@organizationId, @key, @value, @updatedBy)`,
		pgx.NamedArgs{
			"organizationId": deps.orgID,
			"key":            "docker_password",
			"value":          secretValue,
			"updatedBy":      deps.actorID,
		},
	)
	g.Expect(err).NotTo(HaveOccurred())
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	lease, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	return stepEventFixture{
		orgID:      deps.orgID,
		agentID:    tasks[0].DeploymentTargetID,
		taskID:     tasks[0].ID,
		stepRunID:  tasks[0].StepRuns[0].ID,
		leaseID:    lease.ID,
		leaseToken: lease.LeaseToken,
	}
}

func createOCIJobSecretStepEventFixture(
	t *testing.T,
	ctx context.Context,
	secretValue string,
) stepEventFixture {
	t.Helper()
	g := NewWithT(t)
	job := taskLeaseOCIJobStep("job", "OCI job", 10)
	deps := createReadyDeploymentPlanForTaskLeaseWithSteps(t, ctx, []types.DeploymentProcessStep{job})
	_, err := internalctx.GetDb(ctx).Exec(
		ctx,
		`INSERT INTO Secret (organization_id, key, value, updated_by_useraccount_id)
		VALUES (@organizationId, @key, @value, @updatedBy)`,
		pgx.NamedArgs{
			"organizationId": deps.orgID,
			"key":            "job_api_token",
			"value":          secretValue,
			"updatedBy":      deps.actorID,
		},
	)
	g.Expect(err).NotTo(HaveOccurred())
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	lease, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	return stepEventFixture{
		orgID:      deps.orgID,
		agentID:    tasks[0].DeploymentTargetID,
		taskID:     tasks[0].ID,
		stepRunID:  tasks[0].StepRuns[0].ID,
		leaseID:    lease.ID,
		leaseToken: lease.LeaseToken,
	}
}

func createFileRenderSecretStepEventFixture(
	t *testing.T,
	ctx context.Context,
	secretValue string,
) stepEventFixture {
	t.Helper()
	g := NewWithT(t)
	render := taskLeaseFileRenderStep("render", "Render config", 10)
	deps := createReadyDeploymentPlanForTaskLeaseWithSteps(t, ctx, []types.DeploymentProcessStep{render})
	_, err := internalctx.GetDb(ctx).Exec(
		ctx,
		`INSERT INTO Secret (organization_id, key, value, updated_by_useraccount_id)
		VALUES (@organizationId, @key, @value, @updatedBy)`,
		pgx.NamedArgs{
			"organizationId": deps.orgID,
			"key":            "render_api_token",
			"value":          secretValue,
			"updatedBy":      deps.actorID,
		},
	)
	g.Expect(err).NotTo(HaveOccurred())
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	lease, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	return stepEventFixture{
		orgID:      deps.orgID,
		agentID:    tasks[0].DeploymentTargetID,
		taskID:     tasks[0].ID,
		stepRunID:  tasks[0].StepRuns[0].ID,
		leaseID:    lease.ID,
		leaseToken: lease.LeaseToken,
	}
}

func createWebhookSecretStepEventFixture(
	t *testing.T,
	ctx context.Context,
	headerSecret string,
	signingSecret string,
) stepEventFixture {
	t.Helper()
	g := NewWithT(t)
	webhook := taskLeaseWebhookStep("webhook", "Notify webhook", 10)
	deps := createReadyDeploymentPlanForTaskLeaseWithSteps(t, ctx, []types.DeploymentProcessStep{webhook})
	_, err := internalctx.GetDb(ctx).Exec(
		ctx,
		`INSERT INTO Secret (organization_id, key, value, updated_by_useraccount_id)
		VALUES
			(@organizationId, @authKey, @authValue, @updatedBy),
			(@organizationId, @signingKey, @signingValue, @updatedBy)`,
		pgx.NamedArgs{
			"organizationId": deps.orgID,
			"authKey":        "webhook_auth_token",
			"authValue":      headerSecret,
			"signingKey":     "webhook_signing_key",
			"signingValue":   signingSecret,
			"updatedBy":      deps.actorID,
		},
	)
	g.Expect(err).NotTo(HaveOccurred())
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	lease, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	return stepEventFixture{
		orgID:      deps.orgID,
		agentID:    tasks[0].DeploymentTargetID,
		taskID:     tasks[0].ID,
		stepRunID:  tasks[0].StepRuns[0].ID,
		leaseID:    lease.ID,
		leaseToken: lease.LeaseToken,
	}
}

func countStepRunEventsForTest(t *testing.T, ctx context.Context, stepRunID uuid.UUID) int {
	t.Helper()
	var count int
	err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`SELECT count(*) FROM StepRunEvent WHERE step_run_id = @stepRunId`,
		pgx.NamedArgs{"stepRunId": stepRunID},
	).Scan(&count)
	if err != nil {
		t.Fatalf("count step events: %v", err)
	}
	return count
}

func countStepRunOutputRowsForTest(t *testing.T, ctx context.Context, stepRunID uuid.UUID) int {
	t.Helper()
	var count int
	err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`SELECT count(*) FROM StepRunOutput WHERE step_run_id = @stepRunId`,
		pgx.NamedArgs{"stepRunId": stepRunID},
	).Scan(&count)
	if err != nil {
		t.Fatalf("count step outputs: %v", err)
	}
	return count
}
