package auditexport

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestListEnabledAuditExportSinksRejectsUnboundedSelection(t *testing.T) {
	t.Parallel()

	for _, limit := range []int{0, 101} {
		_, err := ListEnabledAuditExportSinks(context.Background(), limit)
		if err == nil || !errors.Is(err, ErrInvalidSinkSelectionLimit) {
			t.Fatalf("limit %d error = %v, want bounded-selection error", limit, err)
		}
	}
}

func TestListEnabledAuditExportSinksSelectsAcrossTenantsBeforeAdditionalTenantSinks(t *testing.T) {
	databaseURL := os.Getenv("DISTR_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DISTR_TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	adminPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(adminPool.Close)

	schema := pgx.Identifier{"audit_export_selection_" + strings.ReplaceAll(uuid.NewString(), "-", "")}.Sanitize()
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := adminPool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE"); err != nil {
			t.Logf("drop test schema: %v", err)
		}
	})
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse test database URL: %v", err)
	}
	config.AfterConnect = func(ctx context.Context, connection *pgx.Conn) error {
		_, err := connection.Exec(ctx, "SET search_path TO "+schema)
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("connect to isolated audit export schema: %v", err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, `
		CREATE TABLE AuditExportSink (
			id uuid PRIMARY KEY,
			organization_id uuid NOT NULL,
			name text NOT NULL,
			kind text NOT NULL,
			endpoint_reference text NOT NULL,
			config_checksum text NOT NULL,
			enabled boolean NOT NULL,
			last_success_at timestamptz,
			last_failure_at timestamptz,
			consecutive_failures integer NOT NULL,
			created_at timestamptz NOT NULL,
			updated_at timestamptz NOT NULL
		);
		CREATE TABLE AuditExportCheckpoint (
			sink_id uuid PRIMARY KEY,
			organization_id uuid NOT NULL,
			last_sequence bigint NOT NULL
		);
		CREATE TABLE ControlPlaneAuditEvent (
			id uuid PRIMARY KEY,
			organization_id uuid NOT NULL,
			sequence bigint NOT NULL
		)`); err != nil {
		t.Fatalf("create audit export selection fixture: %v", err)
	}

	firstOrganization := uuid.New()
	secondOrganization := uuid.New()
	firstTenantSinkA := validProductionAuditExportSink(types.AuditExportSinkKindWebhook)
	firstTenantSinkA.OrganizationID = firstOrganization
	firstTenantSinkB := validProductionAuditExportSink(types.AuditExportSinkKindWebhook)
	firstTenantSinkB.OrganizationID = firstOrganization
	secondTenantSink := validProductionAuditExportSink(types.AuditExportSinkKindWebhook)
	secondTenantSink.OrganizationID = secondOrganization
	for index, sink := range []types.AuditExportSink{firstTenantSinkA, firstTenantSinkB, secondTenantSink} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO AuditExportSink (
				id, organization_id, name, kind, endpoint_reference, config_checksum,
				enabled, consecutive_failures, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, true, 0, now(), now() - interval '1 hour')`,
			sink.ID, sink.OrganizationID, sink.Name, sink.Kind,
			sink.EndpointReference, sink.ConfigChecksum,
		); err != nil {
			t.Fatalf("insert sink %d: %v", index, err)
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO AuditExportCheckpoint (sink_id, organization_id, last_sequence) VALUES ($1, $2, 0)`,
			sink.ID, sink.OrganizationID,
		); err != nil {
			t.Fatalf("insert checkpoint %d: %v", index, err)
		}
	}
	for _, organizationID := range []uuid.UUID{firstOrganization, secondOrganization} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO ControlPlaneAuditEvent (id, organization_id, sequence) VALUES ($1, $2, 1)`,
			uuid.New(), organizationID,
		); err != nil {
			t.Fatalf("insert lagging audit event: %v", err)
		}
	}

	sinks, err := ListEnabledAuditExportSinks(internalctx.WithDb(ctx, pool), 2)
	if err != nil {
		t.Fatalf("list enabled audit export sinks: %v", err)
	}
	if len(sinks) != 2 || sinks[0].OrganizationID == sinks[1].OrganizationID {
		t.Fatalf("bounded selection = %#v, want one sink from each tenant", sinks)
	}
}

type recordingAuditExportSink struct{}

func (recordingAuditExportSink) Export(context.Context, types.ControlPlaneAuditEvent) error {
	return nil
}

type failingProductionAuditExportSink struct {
	err error
}

func (sink failingProductionAuditExportSink) Export(context.Context, types.ControlPlaneAuditEvent) error {
	return sink.err
}

func TestNewProductionSinkResolverVerifiesVersionedConfigurationBeforeFactory(t *testing.T) {
	t.Parallel()

	configuration := ResolvedSinkConfiguration{
		Version:       "vault-version-7",
		Configuration: []byte(`{"url":"https://audit.example.test","token":"resolved-secret"}`),
	}
	checksum, err := configuration.CanonicalChecksum()
	if err != nil {
		t.Fatalf("canonical configuration checksum: %v", err)
	}
	sink := validProductionAuditExportSink(types.AuditExportSinkKindWebhook)
	sink.ConfigChecksum = checksum
	resolverCalls := 0
	factoryCalls := 0
	resolve := NewProductionSinkResolver(
		[]types.AuditExportSink{sink},
		ProductionSinkFactories{
			types.AuditExportSinkKindWebhook: func(
				_ context.Context,
				gotSink types.AuditExportSink,
				gotConfiguration ResolvedSinkConfiguration,
			) (Sink, error) {
				factoryCalls++
				if gotSink.ID != sink.ID || gotConfiguration.Version != configuration.Version {
					t.Fatalf("factory input = %#v, %#v", gotSink, gotConfiguration)
				}
				return recordingAuditExportSink{}, nil
			},
		},
		func(_ context.Context, reference string) (ResolvedSinkConfiguration, error) {
			resolverCalls++
			if reference != sink.EndpointReference {
				t.Fatalf("resolved reference = %q, want %q", reference, sink.EndpointReference)
			}
			return configuration, nil
		},
	)

	if _, err := resolve(context.Background(), sink.ID); err != nil {
		t.Fatalf("resolve configured sink: %v", err)
	}
	if resolverCalls != 1 || factoryCalls != 1 {
		t.Fatalf("resolver calls = %d, factory calls = %d", resolverCalls, factoryCalls)
	}
}

func TestResolvedSinkConfigurationCanonicalChecksumNormalizesJSONAndBindsVersion(t *testing.T) {
	t.Parallel()

	first := ResolvedSinkConfiguration{
		Version:       "version-1",
		Configuration: []byte(`{"url":"https://audit.example.test","options":{"timeout":5,"enabled":true}}`),
	}
	reordered := ResolvedSinkConfiguration{
		Version:       " version-1 ",
		Configuration: []byte(` { "options": { "enabled": true, "timeout": 5 }, "url": "https://audit.example.test" } `),
	}
	firstChecksum, err := first.CanonicalChecksum()
	if err != nil {
		t.Fatalf("first canonical checksum: %v", err)
	}
	reorderedChecksum, err := reordered.CanonicalChecksum()
	if err != nil {
		t.Fatalf("reordered canonical checksum: %v", err)
	}
	if firstChecksum != reorderedChecksum {
		t.Fatalf("equivalent configuration checksums differ: %q != %q", firstChecksum, reorderedChecksum)
	}

	reordered.Version = "version-2"
	versionedChecksum, err := reordered.CanonicalChecksum()
	if err != nil {
		t.Fatalf("versioned canonical checksum: %v", err)
	}
	if versionedChecksum == firstChecksum {
		t.Fatal("configuration version was not bound into canonical checksum")
	}
}

func TestNewProductionSinkResolverFailsClosedWithoutConfiguredTransport(t *testing.T) {
	t.Parallel()

	sink := validProductionAuditExportSink(types.AuditExportSinkKindSIEM)
	resolve := NewProductionSinkResolver([]types.AuditExportSink{sink}, nil, nil)

	_, err := resolve(context.Background(), sink.ID)
	if err == nil || !strings.Contains(err.Error(), "siem") ||
		!strings.Contains(err.Error(), "not configured") {
		t.Fatalf("resolve error = %v, want bounded unconfigured siem error", err)
	}
	if strings.Contains(err.Error(), sink.EndpointReference) {
		t.Fatalf("resolve error leaked endpoint reference: %v", err)
	}
}

func TestNewProductionSinkResolverUsesOnlyExactAllowlistedKindFactory(t *testing.T) {
	t.Parallel()

	webhook := validProductionAuditExportSink(types.AuditExportSinkKindWebhook)
	siem := validProductionAuditExportSink(types.AuditExportSinkKindSIEM)
	webhookCalls := 0
	resolve := NewProductionSinkResolver(
		[]types.AuditExportSink{webhook, siem},
		ProductionSinkFactories{
			types.AuditExportSinkKindWebhook: func(
				_ context.Context,
				got types.AuditExportSink,
				_ ResolvedSinkConfiguration,
			) (Sink, error) {
				webhookCalls++
				if got.ID != webhook.ID {
					t.Fatalf("factory sink = %s, want %s", got.ID, webhook.ID)
				}
				return recordingAuditExportSink{}, nil
			},
		},
		defaultProductionSecretResolver,
	)

	if _, err := resolve(context.Background(), webhook.ID); err != nil {
		t.Fatalf("resolve allowlisted webhook: %v", err)
	}
	if webhookCalls != 1 {
		t.Fatalf("webhook factory calls = %d, want 1", webhookCalls)
	}
	if _, err := resolve(context.Background(), siem.ID); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("resolve unconfigured siem error = %v", err)
	}
}

func TestNewProductionSinkResolverRedactsFactoryErrors(t *testing.T) {
	t.Parallel()

	sink := validProductionAuditExportSink(types.AuditExportSinkKindWebhook)
	resolve := NewProductionSinkResolver([]types.AuditExportSink{sink}, ProductionSinkFactories{
		types.AuditExportSinkKindWebhook: func(
			context.Context, types.AuditExportSink, ResolvedSinkConfiguration,
		) (Sink, error) {
			return nil, fmt.Errorf("request rejected token=%s", "do-not-log-this")
		},
	}, defaultProductionSecretResolver)

	_, err := resolve(context.Background(), sink.ID)
	if err == nil || strings.Contains(err.Error(), "do-not-log-this") ||
		!strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("factory error was not safely redacted: %v", err)
	}
}

func TestNewProductionSinkResolverBoundsFactoryErrors(t *testing.T) {
	t.Parallel()

	sink := validProductionAuditExportSink(types.AuditExportSinkKindWebhook)
	resolve := NewProductionSinkResolver([]types.AuditExportSink{sink}, ProductionSinkFactories{
		types.AuditExportSinkKindWebhook: func(
			context.Context, types.AuditExportSink, ResolvedSinkConfiguration,
		) (Sink, error) {
			return nil, errors.New(strings.Repeat("transport unavailable ", 1000))
		},
	}, defaultProductionSecretResolver)

	_, err := resolve(context.Background(), sink.ID)
	if err == nil || len(err.Error()) > 600 {
		t.Fatalf("factory error is not bounded: length=%d", len(err.Error()))
	}
}

func TestNewProductionSinkResolverRejectsUnknownDisabledAndInvalidSinkMetadata(t *testing.T) {
	t.Parallel()

	sink := validProductionAuditExportSink(types.AuditExportSinkKindObjectStore)
	resolve := NewProductionSinkResolver([]types.AuditExportSink{sink}, ProductionSinkFactories{
		types.AuditExportSinkKindObjectStore: func(
			context.Context, types.AuditExportSink, ResolvedSinkConfiguration,
		) (Sink, error) {
			return recordingAuditExportSink{}, nil
		},
	}, defaultProductionSecretResolver)

	if _, err := resolve(context.Background(), uuid.New()); err == nil || !strings.Contains(err.Error(), "not selected") {
		t.Fatalf("unknown sink error = %v", err)
	}

	sink.Enabled = false
	resolve = NewProductionSinkResolver([]types.AuditExportSink{sink}, ProductionSinkFactories{
		types.AuditExportSinkKindObjectStore: func(
			context.Context, types.AuditExportSink, ResolvedSinkConfiguration,
		) (Sink, error) {
			return recordingAuditExportSink{}, nil
		},
	}, defaultProductionSecretResolver)
	if _, err := resolve(context.Background(), sink.ID); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("disabled sink error = %v", err)
	}

	sink.Enabled = true
	sink.ConfigChecksum = "sha256:not-valid"
	resolve = NewProductionSinkResolver([]types.AuditExportSink{sink}, ProductionSinkFactories{
		types.AuditExportSinkKindObjectStore: func(
			context.Context, types.AuditExportSink, ResolvedSinkConfiguration,
		) (Sink, error) {
			return recordingAuditExportSink{}, nil
		},
	}, defaultProductionSecretResolver)
	if _, err := resolve(context.Background(), sink.ID); err == nil || !strings.Contains(err.Error(), "metadata") {
		t.Fatalf("invalid metadata error = %v", err)
	}
}

func TestNewProductionSinkResolverFailsClosedForMissingResolverVersionOrChecksumMismatch(t *testing.T) {
	t.Parallel()

	sink := validProductionAuditExportSink(types.AuditExportSinkKindWebhook)
	factoryCalls := 0
	factories := ProductionSinkFactories{
		types.AuditExportSinkKindWebhook: func(
			context.Context, types.AuditExportSink, ResolvedSinkConfiguration,
		) (Sink, error) {
			factoryCalls++
			return recordingAuditExportSink{}, nil
		},
	}

	resolve := NewProductionSinkResolver([]types.AuditExportSink{sink}, factories, nil)
	if _, err := resolve(context.Background(), sink.ID); err == nil ||
		!strings.Contains(err.Error(), "secret-reference resolver") {
		t.Fatalf("missing resolver error = %v", err)
	}

	resolve = NewProductionSinkResolver(
		[]types.AuditExportSink{sink},
		factories,
		func(context.Context, string) (ResolvedSinkConfiguration, error) {
			return ResolvedSinkConfiguration{Configuration: []byte(`{"url":"https://audit.example.test"}`)}, nil
		},
	)
	if _, err := resolve(context.Background(), sink.ID); err == nil ||
		!strings.Contains(err.Error(), "version") {
		t.Fatalf("missing version error = %v", err)
	}

	resolve = NewProductionSinkResolver(
		[]types.AuditExportSink{sink},
		factories,
		func(context.Context, string) (ResolvedSinkConfiguration, error) {
			return ResolvedSinkConfiguration{
				Version:       "different-version",
				Configuration: defaultProductionConfiguration().Configuration,
			}, nil
		},
	)
	if _, err := resolve(context.Background(), sink.ID); err == nil ||
		!strings.Contains(err.Error(), "checksum") {
		t.Fatalf("checksum mismatch error = %v", err)
	}
	if factoryCalls != 0 {
		t.Fatalf("factory calls before verified configuration = %d, want 0", factoryCalls)
	}
}

func TestNewProductionSinkResolverRedactsSecretResolverErrors(t *testing.T) {
	t.Parallel()

	sink := validProductionAuditExportSink(types.AuditExportSinkKindWebhook)
	resolve := NewProductionSinkResolver(
		[]types.AuditExportSink{sink},
		ProductionSinkFactories{
			types.AuditExportSinkKindWebhook: func(
				context.Context, types.AuditExportSink, ResolvedSinkConfiguration,
			) (Sink, error) {
				return recordingAuditExportSink{}, nil
			},
		},
		func(context.Context, string) (ResolvedSinkConfiguration, error) {
			return ResolvedSinkConfiguration{}, errors.New("provider rejected password=do-not-log-this")
		},
	)

	_, err := resolve(context.Background(), sink.ID)
	if err == nil || strings.Contains(err.Error(), "do-not-log-this") ||
		!strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("resolver error was not safely redacted: %v", err)
	}
}

func TestNewProductionSinkResolverRedactsTransportDeliveryErrors(t *testing.T) {
	t.Parallel()

	sink := validProductionAuditExportSink(types.AuditExportSinkKindWebhook)
	resolve := NewProductionSinkResolver(
		[]types.AuditExportSink{sink},
		ProductionSinkFactories{
			types.AuditExportSinkKindWebhook: func(
				context.Context, types.AuditExportSink, ResolvedSinkConfiguration,
			) (Sink, error) {
				return failingProductionAuditExportSink{
					err: errors.New("delivery rejected token=do-not-log-this"),
				}, nil
			},
		},
		defaultProductionSecretResolver,
	)

	transport, err := resolve(context.Background(), sink.ID)
	if err != nil {
		t.Fatalf("resolve production transport: %v", err)
	}
	err = transport.Export(context.Background(), types.ControlPlaneAuditEvent{})
	if err == nil || strings.Contains(err.Error(), "do-not-log-this") ||
		!strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("transport delivery error was not safely redacted: %v", err)
	}
}

func validProductionAuditExportSink(kind types.AuditExportSinkKind) types.AuditExportSink {
	configuration := defaultProductionConfiguration()
	checksum, err := configuration.CanonicalChecksum()
	if err != nil {
		panic(err)
	}
	return types.AuditExportSink{
		ID:                uuid.New(),
		OrganizationID:    uuid.New(),
		Name:              "external audit",
		Kind:              kind,
		EndpointReference: "secret:control-plane/audit/export",
		ConfigChecksum:    checksum,
		Enabled:           true,
	}
}

func defaultProductionConfiguration() ResolvedSinkConfiguration {
	return ResolvedSinkConfiguration{
		Version:       "secret-version-1",
		Configuration: []byte(`{"url":"https://audit.example.test","token":"resolved-secret"}`),
	}
}

func defaultProductionSecretResolver(
	context.Context,
	string,
) (ResolvedSinkConfiguration, error) {
	return defaultProductionConfiguration(), nil
}
