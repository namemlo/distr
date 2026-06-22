package db_test

import (
	"context"
	"testing"
	"time"

	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	obsertracing "github.com/distr-sh/distr/internal/observability/tracing"
	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

func TestTaskQueueRepositoryRecordsTaskExecutionSpans(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	tracer := &recordingTaskTracer{}
	ctx = internalctx.WithObservabilityTracer(ctx, tracer)
	deps := createReadyDeploymentPlanForTaskQueue(t, ctx, "cluster-a")

	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())

	_, err = db.TransitionTaskState(ctx, types.TransitionTaskStateRequest{
		OrganizationID: deps.orgID,
		TaskID:         tasks[0].ID,
		Status:         types.TaskStatusRunning,
	})
	g.Expect(err).NotTo(HaveOccurred())
	time.Sleep(10 * time.Millisecond)

	_, err = db.TransitionTaskState(ctx, types.TransitionTaskStateRequest{
		OrganizationID: deps.orgID,
		TaskID:         tasks[0].ID,
		Status:         types.TaskStatusSucceeded,
	})
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(tracer.spans).To(HaveLen(2))
	g.Expect(tracer.spans[0].name).To(Equal("task.execution.start"))
	g.Expect(attributeValue(tracer.spans[0].attrs, "task.status")).To(Equal("running"))
	g.Expect(tracer.spans[1].name).To(Equal("task.execution"))
	g.Expect(attributeValue(tracer.spans[1].attrs, "task.status")).To(Equal("succeeded"))
	g.Expect(tracer.spans[1].startTime.IsZero()).To(BeFalse())
	g.Expect(tracer.spans[1].endTime.IsZero()).To(BeFalse())
}

type recordingTaskTracer struct {
	spans []recordedTaskSpan
}

func (r *recordingTaskTracer) Enabled() bool {
	return true
}

func (r *recordingTaskTracer) Start(ctx context.Context, name string, options obsertracing.SpanStartOptions) (context.Context, obsertracing.Span) {
	return ctx, &recordingTaskSpanHandle{
		tracer:    r,
		name:      name,
		attrs:     append([]obsertracing.Attribute{}, options.Attributes...),
		startTime: options.StartTime,
	}
}

type recordedTaskSpan struct {
	name      string
	attrs     []obsertracing.Attribute
	startTime time.Time
	endTime   time.Time
	err       error
}

type recordingTaskSpanHandle struct {
	tracer    *recordingTaskTracer
	name      string
	attrs     []obsertracing.Attribute
	startTime time.Time
	err       error
}

func (s *recordingTaskSpanHandle) End(options obsertracing.SpanEndOptions) {
	s.tracer.spans = append(s.tracer.spans, recordedTaskSpan{
		name:      s.name,
		attrs:     s.attrs,
		startTime: s.startTime,
		endTime:   options.EndTime,
		err:       s.err,
	})
}

func (s *recordingTaskSpanHandle) SetName(name string) {
	s.name = name
}

func (s *recordingTaskSpanHandle) SetAttributes(attrs ...obsertracing.Attribute) {
	s.attrs = append(s.attrs, attrs...)
}

func (s *recordingTaskSpanHandle) RecordError(err error) {
	s.err = err
}

func (s *recordingTaskSpanHandle) Context() obsertracing.SpanContext {
	return obsertracing.SpanContext{TraceID: "trace-1", SpanID: "span-1", Sampled: true}
}

func attributeValue(attrs []obsertracing.Attribute, key string) string {
	for _, attr := range attrs {
		if attr.Key == key {
			if value, ok := attr.Value.(string); ok {
				return value
			}
		}
	}
	return ""
}
