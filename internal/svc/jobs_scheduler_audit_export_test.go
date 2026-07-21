package svc

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/auditexport"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type auditExportBatcherFake struct {
	calls      []uuid.UUID
	failureFor uuid.UUID
}

type resolvingAuditExportBatcher struct {
	resolve auditexport.SinkResolver
}

func (batcher resolvingAuditExportBatcher) ExportAuditBatch(
	ctx context.Context,
	sinkID uuid.UUID,
	_ int,
) (types.ExportBatchResult, error) {
	sink, err := batcher.resolve(ctx, sinkID)
	if err != nil {
		return types.ExportBatchResult{SinkID: sinkID}, err
	}
	if err := sink.Export(ctx, types.ControlPlaneAuditEvent{}); err != nil {
		return types.ExportBatchResult{SinkID: sinkID}, err
	}
	return types.ExportBatchResult{SinkID: sinkID, Exported: 1}, nil
}

type configuredAuditExportSink struct{}

func (configuredAuditExportSink) Export(context.Context, types.ControlPlaneAuditEvent) error {
	return nil
}

func (fake *auditExportBatcherFake) ExportAuditBatch(
	_ context.Context,
	sinkID uuid.UUID,
	limit int,
) (types.ExportBatchResult, error) {
	if limit != controlPlaneAuditExportEventBatchSize {
		return types.ExportBatchResult{}, errors.New("unexpected event batch size")
	}
	fake.calls = append(fake.calls, sinkID)
	result := types.ExportBatchResult{SinkID: sinkID, Exported: 1, CheckpointLag: 7}
	if sinkID == fake.failureFor {
		return result, errors.New("transport unavailable")
	}
	return result, nil
}

func TestControlPlaneAuditExportRegistrationRequiresOperatorControlPlaneFlag(t *testing.T) {
	tests := []struct {
		name       string
		flags      []featureflags.Key
		registered bool
	}{
		{name: "disabled"},
		{name: "executor only", flags: []featureflags.Key{featureflags.KeyExecutorProtocolV2}},
		{
			name:       "operator enabled",
			flags:      []featureflags.Key{featureflags.KeyOperatorControlPlaneV2},
			registered: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registrar := &durationJobRegistrarFake{}
			if err := registerControlPlaneAuditExport(
				registrar,
				featureflags.NewRegistry(test.flags),
				controlPlaneAuditExportJobDependencies{},
			); err != nil {
				t.Fatalf("register audit export: %v", err)
			}
			wantCalls := 0
			if test.registered {
				wantCalls = 1
			}
			if registrar.calls != wantCalls {
				t.Fatalf("registration calls = %d, want %d", registrar.calls, wantCalls)
			}
			if test.registered && registrar.duration != controlPlaneAuditExportInterval {
				t.Fatalf("duration = %s, want %s", registrar.duration, controlPlaneAuditExportInterval)
			}
		})
	}
}

func TestControlPlaneAuditExportJobProcessesStableBoundedSinkBatchAndReportsLag(t *testing.T) {
	t.Parallel()

	first := types.AuditExportSink{ID: uuid.New(), Enabled: true}
	second := types.AuditExportSink{ID: uuid.New(), Enabled: true}
	batcher := &auditExportBatcherFake{failureFor: first.ID}
	reports := make([]types.ExportBatchResult, 0, 2)
	reportErrors := make([]error, 0, 2)
	job := controlPlaneAuditExportJob(controlPlaneAuditExportJobDependencies{
		ListEnabledSinks: func(_ context.Context, limit int) ([]types.AuditExportSink, error) {
			if limit != controlPlaneAuditExportSinkBatchSize {
				t.Fatalf("sink batch limit = %d, want %d", limit, controlPlaneAuditExportSinkBatchSize)
			}
			return []types.AuditExportSink{first, second}, nil
		},
		NewSinkResolver: func(got []types.AuditExportSink) auditexport.SinkResolver {
			if len(got) != 2 || got[0].ID != first.ID || got[1].ID != second.ID {
				t.Fatalf("resolver sinks = %#v", got)
			}
			return func(context.Context, uuid.UUID) (auditexport.Sink, error) {
				return configuredAuditExportSink{}, nil
			}
		},
		NewBatcher: func(auditexport.SinkResolver) auditExportBatcher {
			return batcher
		},
		Report: func(_ context.Context, result types.ExportBatchResult, err error) {
			reports = append(reports, result)
			reportErrors = append(reportErrors, err)
		},
	})

	err := job(context.Background())
	if err == nil || !strings.Contains(err.Error(), first.ID.String()) ||
		!strings.Contains(err.Error(), "transport unavailable") {
		t.Fatalf("job error = %v", err)
	}
	if len(batcher.calls) != 2 || batcher.calls[0] != first.ID || batcher.calls[1] != second.ID {
		t.Fatalf("processed sinks = %#v", batcher.calls)
	}
	if len(reports) != 2 || reports[0].CheckpointLag != 7 || reports[1].CheckpointLag != 7 {
		t.Fatalf("reported results = %#v", reports)
	}
	if reportErrors[0] == nil || reportErrors[1] != nil {
		t.Fatalf("reported errors = %#v", reportErrors)
	}
}

func TestControlPlaneAuditExportJobFailsClosedWhenSelectionOrDependenciesFail(t *testing.T) {
	t.Parallel()

	job := controlPlaneAuditExportJob(controlPlaneAuditExportJobDependencies{})
	if err := job(context.Background()); !errors.Is(err, errControlPlaneAuditExportUnconfigured) {
		t.Fatalf("missing dependencies error = %v", err)
	}

	want := errors.New("catalog unavailable")
	job = controlPlaneAuditExportJob(controlPlaneAuditExportJobDependencies{
		ListEnabledSinks: func(context.Context, int) ([]types.AuditExportSink, error) {
			return nil, want
		},
		NewSinkResolver: func([]types.AuditExportSink) auditexport.SinkResolver {
			t.Fatal("resolver must not be built after list failure")
			return nil
		},
		NewBatcher: func(auditexport.SinkResolver) auditExportBatcher {
			t.Fatal("batcher must not be built after list failure")
			return nil
		},
		Report: func(context.Context, types.ExportBatchResult, error) {},
	})
	if err := job(context.Background()); err == nil || !strings.Contains(err.Error(), want.Error()) {
		t.Fatalf("selection error = %v", err)
	}
}

func TestRegistryControlPlaneAuditExportDependenciesUseConfiguredResolverAndFactory(t *testing.T) {
	t.Parallel()

	configuration := auditexport.ResolvedSinkConfiguration{
		Version:       "secret-version-3",
		Configuration: []byte(`{"endpoint":"https://audit.example.test"}`),
	}
	checksum, err := configuration.CanonicalChecksum()
	if err != nil {
		t.Fatalf("canonical configuration checksum: %v", err)
	}
	sink := types.AuditExportSink{
		ID:                uuid.New(),
		OrganizationID:    uuid.New(),
		Name:              "external audit",
		Kind:              types.AuditExportSinkKindWebhook,
		EndpointReference: "secret:control-plane/audit/export",
		ConfigChecksum:    checksum,
		Enabled:           true,
	}
	resolverCalls := 0
	factoryCalls := 0
	reg := Registry{logger: zap.NewNop()}
	ControlPlaneAuditExportAdapters(
		auditexport.ProductionSinkFactories{
			types.AuditExportSinkKindWebhook: func(
				context.Context,
				types.AuditExportSink,
				auditexport.ResolvedSinkConfiguration,
			) (auditexport.Sink, error) {
				factoryCalls++
				return configuredAuditExportSink{}, nil
			},
		},
		func(context.Context, string) (auditexport.ResolvedSinkConfiguration, error) {
			resolverCalls++
			return configuration, nil
		},
	)(&reg)

	dependencies := reg.controlPlaneAuditExportJobDependencies()
	dependencies.ListEnabledSinks = func(context.Context, int) ([]types.AuditExportSink, error) {
		return []types.AuditExportSink{sink}, nil
	}
	dependencies.NewBatcher = func(resolve auditexport.SinkResolver) auditExportBatcher {
		return resolvingAuditExportBatcher{resolve: resolve}
	}
	if err := controlPlaneAuditExportJob(dependencies)(context.Background()); err != nil {
		t.Fatalf("configured scheduler tick: %v", err)
	}
	if resolverCalls != 1 || factoryCalls != 1 {
		t.Fatalf("resolver calls = %d, factory calls = %d", resolverCalls, factoryCalls)
	}
}

func TestRegistryControlPlaneAuditExportDependenciesFailClosedWithoutAdapters(t *testing.T) {
	t.Parallel()

	sink := types.AuditExportSink{
		ID:                uuid.New(),
		OrganizationID:    uuid.New(),
		Name:              "external audit",
		Kind:              types.AuditExportSinkKindWebhook,
		EndpointReference: "secret:control-plane/audit/export",
		ConfigChecksum:    "sha256:" + strings.Repeat("a", 64),
		Enabled:           true,
	}
	reg := Registry{logger: zap.NewNop()}
	dependencies := reg.controlPlaneAuditExportJobDependencies()
	dependencies.ListEnabledSinks = func(context.Context, int) ([]types.AuditExportSink, error) {
		return []types.AuditExportSink{sink}, nil
	}
	dependencies.NewBatcher = func(resolve auditexport.SinkResolver) auditExportBatcher {
		return resolvingAuditExportBatcher{resolve: resolve}
	}

	err := controlPlaneAuditExportJob(dependencies)(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("default scheduler tick error = %v, want fail-closed configuration error", err)
	}
}
