package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/auth"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
)

func TestValidateChannelVersionHandlerEvaluatesPersistedRules(t *testing.T) {
	ctx := channelHandlerDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, lifecycleID := createChannelHandlerDependencies(t, ctx)
	channel := types.Channel{
		OrganizationID:              orgID,
		ApplicationID:               applicationID,
		LifecycleID:                 lifecycleID,
		Name:                        "Preview",
		AllowedVersionRanges:        []string{">=1.0.0 <2.0.0"},
		AllowedPrereleasePatterns:   []string{"rc.*"},
		AllowedSourceBranchPatterns: []string{"release/*"},
	}
	g.Expect(db.CreateChannel(ctx, &channel)).To(Succeed())

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/channels/"+channel.ID.String()+"/validate-version",
		strings.NewReader(`{"version":"2.0.0","sourceBranch":"feature/demo"}`),
	)
	request.SetPathValue("channelId", channel.ID.String())
	request = request.WithContext(authenticatedChannelHandlerContext(ctx, orgID))

	validateChannelVersionHandler().ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusOK))
	var response api.ChannelVersionValidationResponse
	g.Expect(json.Unmarshal(recorder.Body.Bytes(), &response)).To(Succeed())
	g.Expect(response.Valid).To(BeFalse())
	g.Expect(response.Errors).To(ContainElements(
		api.ChannelValidationError{
			Field:   "version",
			Rule:    ">=1.0.0 <2.0.0",
			Message: channelVersionRangeMessage,
		},
		api.ChannelValidationError{
			Field:   "sourceBranch",
			Rule:    "release/*",
			Message: "source branch does not match an allowed pattern",
		},
	))
}

func TestValidateChannelVersionHandlerReturnsNotFoundForCrossOrganizationChannel(t *testing.T) {
	ctx := channelHandlerDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, lifecycleID := createChannelHandlerDependencies(t, ctx)
	otherOrgID, _, _ := createChannelHandlerDependencies(t, ctx)
	channel := types.Channel{
		OrganizationID: orgID,
		ApplicationID:  applicationID,
		LifecycleID:    lifecycleID,
		Name:           "Stable",
	}
	g.Expect(db.CreateChannel(ctx, &channel)).To(Succeed())

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/channels/"+channel.ID.String()+"/validate-version",
		strings.NewReader(`{"version":"1.2.3"}`),
	)
	request.SetPathValue("channelId", channel.ID.String())
	request = request.WithContext(authenticatedChannelHandlerContext(ctx, otherOrgID))

	validateChannelVersionHandler().ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusNotFound))
}

func authenticatedChannelHandlerContext(ctx context.Context, orgID uuid.UUID) context.Context {
	ctx = internalctx.WithLogger(ctx, zap.NewNop())
	channelAuth := testChannelAuth()
	channelAuth.orgID = orgID
	return auth.Authentication.NewContext(ctx, channelAuth)
}

func channelHandlerDBTestContext(t *testing.T) context.Context {
	t.Helper()
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

	schema := "channel_handler_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := adminPool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+quotedSchema+" CASCADE"); err != nil {
			t.Logf("drop test schema: %v", err)
		}
	})

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse test database url: %v", err)
	}
	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET search_path TO "+quotedSchema)
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("connect to isolated test schema: %v", err)
	}
	t.Cleanup(pool.Close)

	runChannelHandlerTestMigrations(t, ctx, pool)
	return internalctx.WithDb(ctx, pool)
}

func runChannelHandlerTestMigrations(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "migrations", "sql", "*.up.sql"))
	if err != nil {
		t.Fatalf("list migration files: %v", err)
	}
	sort.Slice(files, func(i, j int) bool {
		return channelHandlerMigrationVersion(t, files[i]) < channelHandlerMigrationVersion(t, files[j])
	})
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read migration %s: %v", file, err)
		}
		if _, err := pool.Exec(ctx, string(data)); err != nil {
			t.Fatalf("run migration %s: %v", file, err)
		}
	}
}

func channelHandlerMigrationVersion(t *testing.T, file string) int {
	t.Helper()
	base := filepath.Base(file)
	version, err := strconv.Atoi(strings.SplitN(base, "_", 2)[0])
	if err != nil {
		t.Fatalf("parse migration version %s: %v", file, err)
	}
	return version
}

func createChannelHandlerDependencies(t *testing.T, ctx context.Context) (uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	var orgID uuid.UUID
	if err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`INSERT INTO Organization (name) VALUES (@name) RETURNING id`,
		pgx.NamedArgs{"name": "Organization " + uuid.NewString()},
	).Scan(&orgID); err != nil {
		t.Fatalf("create organization: %v", err)
	}
	application := types.Application{
		Name: "Application " + uuid.NewString(),
		Type: types.DeploymentTypeDocker,
	}
	if err := db.CreateApplication(ctx, &application, orgID); err != nil {
		t.Fatalf("create application: %v", err)
	}
	var lifecycleID uuid.UUID
	if err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`INSERT INTO Lifecycle (organization_id, name) VALUES (@organizationId, @name) RETURNING id`,
		pgx.NamedArgs{"organizationId": orgID, "name": "Lifecycle " + uuid.NewString()},
	).Scan(&lifecycleID); err != nil {
		t.Fatalf("create lifecycle: %v", err)
	}
	return orgID, application.ID, lifecycleID
}
