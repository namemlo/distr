package auditexport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const maxAuditExportSinkSelection = 100
const maxAuditExportTransportErrorBytes = 512

var (
	ErrInvalidSinkSelectionLimit = errors.New("audit export sink selection limit must be between 1 and 100")
	auditExportConfigChecksum    = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// ResolvedSinkConfiguration is the versioned opaque JSON configuration returned
// by an operator-owned secret provider for one audit export sink.
type ResolvedSinkConfiguration struct {
	Version       string
	Configuration json.RawMessage
}

// CanonicalChecksum binds the normalized JSON configuration to its provider
// version so rotation cannot silently reuse an older persisted checksum.
func (configuration ResolvedSinkConfiguration) CanonicalChecksum() (string, error) {
	version := strings.TrimSpace(configuration.Version)
	if version == "" {
		return "", errors.New("resolved audit export configuration version is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(configuration.Configuration))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", errors.New("resolved audit export configuration is invalid")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return "", errors.New("resolved audit export configuration is invalid")
	}
	canonical, err := json.Marshal(struct {
		Version       string `json:"version"`
		Configuration any    `json:"configuration"`
	}{Version: version, Configuration: value})
	if err != nil {
		return "", errors.New("resolved audit export configuration is invalid")
	}
	digest := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

// SecretReferenceResolver resolves an opaque stored reference without placing
// provider-specific behavior in the audit export core.
type SecretReferenceResolver func(
	context.Context,
	string,
) (ResolvedSinkConfiguration, error)

// ProductionSinkFactory constructs an allowlisted generic transport only after
// the resolved configuration has passed canonical checksum verification.
type ProductionSinkFactory func(
	context.Context,
	types.AuditExportSink,
	ResolvedSinkConfiguration,
) (Sink, error)

// ProductionSinkFactories maps generic sink kinds to explicitly configured
// production transport adapters.
type ProductionSinkFactories map[types.AuditExportSinkKind]ProductionSinkFactory

type redactingProductionSink struct {
	sink Sink
}

func (sink redactingProductionSink) Export(
	ctx context.Context,
	event types.ControlPlaneAuditEvent,
) error {
	if err := sink.sink.Export(ctx, event); err != nil {
		return safeProductionAdapterError("export audit event", err)
	}
	return nil
}

// ListEnabledAuditExportSinks returns only enabled sinks that have checkpoint lag.
// The tenant rank orders one sink from each organization before a second sink
// from any organization, while updated_at rotates attempted sinks across ticks.
func ListEnabledAuditExportSinks(
	ctx context.Context,
	limit int,
) ([]types.AuditExportSink, error) {
	if limit < 1 || limit > maxAuditExportSinkSelection {
		return nil, ErrInvalidSinkSelectionLimit
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		WITH eligible AS (
			SELECT
			s.id,
			s.organization_id,
			s.name,
			s.kind,
			s.endpoint_reference,
			s.config_checksum,
			s.enabled,
			s.last_success_at,
			s.last_failure_at,
			s.consecutive_failures,
			s.created_at,
			s.updated_at,
			ROW_NUMBER() OVER (
				PARTITION BY s.organization_id
				ORDER BY s.updated_at, s.id
			) AS tenant_rank
			FROM AuditExportSink s
			JOIN AuditExportCheckpoint c
			  ON c.sink_id = s.id
			 AND c.organization_id = s.organization_id
			WHERE s.enabled
			  AND EXISTS (
				SELECT 1
				FROM ControlPlaneAuditEvent e
				WHERE e.organization_id = s.organization_id
				  AND e.sequence > c.last_sequence
			  )
		)
		SELECT
			id,
			organization_id,
			name,
			kind,
			endpoint_reference,
			config_checksum,
			enabled,
			last_success_at,
			last_failure_at,
			consecutive_failures,
			created_at,
			updated_at
		FROM eligible
		ORDER BY tenant_rank, updated_at, organization_id, id
		LIMIT @limit`,
		pgx.NamedArgs{"limit": limit},
	)
	if err != nil {
		return nil, fmt.Errorf("list enabled audit export sinks: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[types.AuditExportSink])
}

// NewProductionSinkResolver binds the exact sink metadata selected for one job
// tick to generic, explicitly registered transport factories. Distr registers no
// default external transport: an operator must provide a secret-reference
// resolver and a factory for the selected generic sink kind.
func NewProductionSinkResolver(
	sinks []types.AuditExportSink,
	factories ProductionSinkFactories,
	resolveSecret SecretReferenceResolver,
) SinkResolver {
	selected := make(map[uuid.UUID]types.AuditExportSink, len(sinks))
	for _, sink := range sinks {
		selected[sink.ID] = sink
	}
	registered := make(ProductionSinkFactories, len(factories))
	for kind, factory := range factories {
		if kind.Valid() && factory != nil {
			registered[kind] = factory
		}
	}

	return func(ctx context.Context, sinkID uuid.UUID) (Sink, error) {
		sink, ok := selected[sinkID]
		if !ok || sinkID == uuid.Nil {
			return nil, errors.New("audit export sink was not selected for this worker tick")
		}
		if !sink.Enabled {
			return nil, errors.New("audit export sink is disabled")
		}
		if !validProductionSinkMetadata(sink) {
			return nil, errors.New("audit export sink metadata is invalid")
		}
		factory := registered[sink.Kind]
		if factory == nil {
			return nil, fmt.Errorf("audit export transport for kind %s is not configured", sink.Kind)
		}
		if resolveSecret == nil {
			return nil, errors.New("audit export secret-reference resolver is not configured")
		}
		configuration, err := resolveSecret(ctx, strings.TrimSpace(sink.EndpointReference))
		if err != nil {
			return nil, safeProductionAdapterError("resolve audit export configuration", err)
		}
		checksum, err := configuration.CanonicalChecksum()
		if err != nil {
			return nil, err
		}
		if checksum != sink.ConfigChecksum {
			return nil, errors.New("resolved audit export configuration checksum does not match configured checksum")
		}
		transport, err := factory(ctx, sink, configuration)
		if err != nil {
			return nil, safeProductionAdapterError(
				fmt.Sprintf("configure audit export transport for kind %s", sink.Kind),
				err,
			)
		}
		if transport == nil {
			return nil, fmt.Errorf("audit export transport for kind %s is not configured", sink.Kind)
		}
		return redactingProductionSink{sink: transport}, nil
	}
}

func safeProductionAdapterError(operation string, err error) error {
	summary, _ := RedactAuditText(err.Error())
	summary = boundAuditExportTransportError(summary)
	return fmt.Errorf("%s: %s", operation, summary)
}

func boundAuditExportTransportError(value string) string {
	if len(value) <= maxAuditExportTransportErrorBytes {
		return value
	}
	cut := maxAuditExportTransportErrorBytes
	for cut > 0 && !utf8.ValidString(value[:cut]) {
		cut--
	}
	return value[:cut] + "..."
}

func validProductionSinkMetadata(sink types.AuditExportSink) bool {
	return sink.ID != uuid.Nil && sink.OrganizationID != uuid.Nil &&
		sink.Kind.Valid() &&
		types.ValidAuditExportEndpointReference(strings.TrimSpace(sink.EndpointReference)) &&
		auditExportConfigChecksum.MatchString(sink.ConfigChecksum)
}
