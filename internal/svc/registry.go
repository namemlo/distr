package svc

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"syscall"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/distr-sh/distr/internal/buildconfig"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/env"
	"github.com/distr-sh/distr/internal/executionruntime"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/hubexecutor"
	"github.com/distr-sh/distr/internal/jobs"
	"github.com/distr-sh/distr/internal/migrations"
	obsermetrics "github.com/distr-sh/distr/internal/observability/metrics"
	obsertracing "github.com/distr-sh/distr/internal/observability/tracing"
	"github.com/distr-sh/distr/internal/oidc"
	distrprometheus "github.com/distr-sh/distr/internal/prometheus"
	"github.com/distr-sh/distr/internal/registry"
	"github.com/distr-sh/distr/internal/routing"
	"github.com/distr-sh/distr/internal/server"
	"github.com/distr-sh/distr/internal/targetconfig"
	"github.com/distr-sh/distr/internal/tracers"
	"github.com/go-chi/chi/v5"
	"github.com/go-mailx/mailx"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

type Registry struct {
	dbPool                      *pgxpool.Pool
	logger                      *zap.Logger
	mailer                      *mailx.Mailer
	execDbMigrations            bool
	artifactsRegistry           http.Handler
	tracers                     *tracers.Tracers
	jobsScheduler               *jobs.Scheduler
	hubExecutor                 *hubexecutor.Worker
	oidcer                      *oidc.OIDCer
	promRegistry                *prometheus.Registry
	promCollector               *distrprometheus.DistrCollector
	metricsRecorder             obsermetrics.Recorder
	observabilityTracers        obsertracing.Tracers
	observabilityMetricsEnabled bool
	observabilityTracingEnabled bool
	s3Client                    *s3.Client
	targetConfigObjectVerifier  targetconfig.ObjectVerifier
	executionRuntime            executionruntime.Dependencies
}

func New(ctx context.Context, options ...RegistryOption) (*Registry, error) {
	var reg Registry
	for _, opt := range options {
		opt(&reg)
	}
	return newRegistry(ctx, &reg)
}

func NewDefault(ctx context.Context) (*Registry, error) {
	var reg Registry
	return newRegistry(ctx, &reg)
}

func newRegistry(ctx context.Context, reg *Registry) (*Registry, error) {
	reg.logger = createLogger()

	ctx = internalctx.WithLogger(ctx, reg.logger)

	reg.logger.Info("initializing service registry",
		zap.String("version", buildconfig.Version()),
		zap.String("commit", buildconfig.Commit()),
		zap.String("edition", buildconfig.Edition()),
		zap.Bool("release", buildconfig.IsRelease()))

	experimentalFeatures := featureflags.NewRegistry(env.ExperimentalFeatureFlags())
	executionRuntime, err := newExecutionRuntimeDependencies(
		env.ExecutionV2SigningKeysJSON(), env.ExecutionV2ObserverPublicKeysJSON(), experimentalFeatures,
	)
	if err != nil {
		return nil, err
	}
	reg.executionRuntime = executionRuntime
	reg.promCollector = distrprometheus.NewDistrCollector()
	reg.observabilityMetricsEnabled = experimentalFeatures.IsEnabled(featureflags.KeyObservabilityMetrics)
	reg.observabilityTracingEnabled = experimentalFeatures.IsEnabled(featureflags.KeyObservabilityTracing)
	if reg.observabilityMetricsEnabled {
		reg.metricsRecorder = obsermetrics.NewPrometheusRecorder(obsermetrics.BaseLabels{
			Service:     "hub",
			Environment: env.SentryEnvironment(),
			Version:     buildconfig.Version(),
		})
	}
	reg.promRegistry = createPrometheusRegistry(reg.promCollector, prometheusCollector(reg.metricsRecorder))

	if tracers, err := reg.createTracer(ctx, reg.observabilityTracingEnabled); err != nil {
		return nil, err
	} else {
		reg.tracers = tracers
		reg.observabilityTracers = reg.createObservabilityTracers()
	}

	if mailer, err := createMailer(ctx); err != nil {
		return nil, err
	} else {
		reg.mailer = mailer
	}

	if reg.execDbMigrations {
		if err := migrations.Up(ctx, reg.logger); err != nil {
			return nil, err
		}
	}

	if db, err := reg.createDBPool(ctx); err != nil {
		return nil, err
	} else {
		reg.dbPool = db
	}
	reg.hubExecutor = hubexecutor.New(reg.logger, reg.dbPool, hubexecutor.Options{})

	if env.RegistryEnabled() {
		reg.s3Client = newS3Client(ctx)
	}
	reg.targetConfigObjectVerifier = newTargetConfigObjectVerifier(ctx)

	if scheduler, err := reg.createJobsScheduler(); err != nil {
		return nil, err
	} else {
		reg.jobsScheduler = scheduler
	}

	if artifactRegistry, err := reg.createArtifactsRegistry(ctx); err != nil {
		return nil, err
	} else {
		reg.artifactsRegistry = artifactRegistry
	}

	if oidcer, err := reg.createOIDCer(ctx, reg.logger); err != nil {
		return nil, err
	} else {
		reg.oidcer = oidcer
	}

	return reg, nil
}

func (r *Registry) Shutdown(ctx context.Context) error {
	if err := r.hubExecutor.Shutdown(ctx); err != nil {
		r.logger.Warn("Hub task executor shutdown failed", zap.Error(err))
	}
	if err := r.jobsScheduler.Shutdown(); err != nil {
		r.logger.Warn("job scheduler shutdown failed", zap.Error(err))
	}

	r.logger.Warn("shutting down database connections")
	r.dbPool.Close()

	if err := r.tracers.Shutdown(ctx); err != nil {
		r.logger.Warn("tracer shutdown failed", zap.Error(err))
	}

	// some devices like stdout and stderr can not be synced by the OS
	if err := r.logger.Sync(); err != nil && !errors.Is(err, syscall.EINVAL) {
		return fmt.Errorf("logger sync failed: %w", err)
	}

	return nil
}

func (reg *Registry) createArtifactsRegistry(ctx context.Context) (http.Handler, error) {
	return registry.NewDefault(
		ctx,
		reg.GetLogger().With(zap.String("component", "registry")),
		reg.GetDbPool(),
		reg.GetMailer(),
		reg.GetTracers().Registry(),
		reg.s3Client,
	)
}

func (r *Registry) GetRouter() http.Handler {
	return routing.NewRouter(
		r.GetLogger(),
		r.GetDbPool(),
		r.GetMailer(),
		r.GetOIDCer(),
		r.GetPrometheusCollector(),
		r.GetObservabilityMetricsRecorder(),
		r.GetObservabilityTracers(),
		r.GetS3Client(),
		r.targetConfigObjectVerifier,
		r.executionRuntime,
	)
}

func (r *Registry) GetArtifactsRouter() http.Handler {
	return r.artifactsRegistry
}

func (r *Registry) GetMetricsRouter() http.Handler {
	m := chi.NewMux()
	if !r.observabilityMetricsEnabled {
		return m
	}

	if metricsToken := env.MetricsBearerToken(); metricsToken != nil {
		expectedToken := []byte(*metricsToken)
		m.Use(func(h http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				authorization := r.Header.Get("Authorization")
				if strings.HasPrefix(authorization, "Bearer ") {
					providedToken := []byte(authorization[len("Bearer "):])
					if subtle.ConstantTimeCompare(expectedToken, providedToken) == 1 {
						h.ServeHTTP(w, r)
						return
					}
				}

				w.Header().Set("WWW-Authenticate", "Bearer")
				http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			})
		})
	}

	h := promhttp.HandlerFor(r.promRegistry, promhttp.HandlerOpts{})
	m.Get("/metrics", h.ServeHTTP)
	return m
}

func (r *Registry) GetServer() server.Server {
	return server.NewServer(r.GetRouter(), r.logger.With(zap.String("server", "main")))
}

func (r *Registry) GetArtifactsServer() server.Server {
	if env.RegistryEnabled() {
		return server.NewServer(r.GetArtifactsRouter(), r.logger.With(zap.String("server", "registry")))
	} else {
		return server.NewNoop()
	}
}

func (r *Registry) GetMetricsServer() server.Server {
	if env.MetricsEnabled() && r.observabilityMetricsEnabled {
		return server.NewServer(r.GetMetricsRouter(), r.logger.With(zap.String("server", "metrics")))
	} else {
		return server.NewNoop()
	}
}

func (r *Registry) GetPrometheusCollector() *distrprometheus.DistrCollector {
	return r.promCollector
}

func (r *Registry) GetObservabilityMetricsRecorder() obsermetrics.Recorder {
	return r.metricsRecorder
}

func (r *Registry) GetObservabilityTracers() obsertracing.Tracers {
	return r.observabilityTracers
}

func (r *Registry) createObservabilityTracers() obsertracing.Tracers {
	if !r.observabilityTracingEnabled {
		noop := obsertracing.NoopTracer{}
		return obsertracing.Tracers{Default: noop, Agent: noop}
	}
	return obsertracing.Tracers{
		Default: obsertracing.NewOtelTracer(r.GetTracers().Default(), "github.com/distr-sh/distr/hub"),
		Agent:   obsertracing.NewOtelTracer(r.GetTracers().Agent(), "github.com/distr-sh/distr/agent-api"),
	}
}

func prometheusCollector(recorder obsermetrics.Recorder) prometheus.Collector {
	collector, _ := recorder.(prometheus.Collector)
	return collector
}

func (r *Registry) GetS3Client() *s3.Client {
	return r.s3Client
}

func (r *Registry) GetTargetConfigObjectVerifier() targetconfig.ObjectVerifier {
	return r.targetConfigObjectVerifier
}
