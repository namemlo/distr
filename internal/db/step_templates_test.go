package db_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestStepTemplateRepositoryImportsAndListsTenantTemplates(t *testing.T) {
	ctx := stepTemplateDBTestContext(t)
	g := NewWithT(t)
	orgID, _, _ := createChannelDependencies(t, ctx)
	otherOrgID, _, _ := createChannelDependencies(t, ctx)
	actorID := createReleaseBundleTestUser(t, ctx, orgID)

	request := stepTemplateImportFixture(orgID, actorID)
	created, err := db.ImportStepTemplate(ctx, request)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(created.ID).NotTo(Equal(uuid.Nil))
	g.Expect(created.OrganizationID).To(Equal(orgID))
	g.Expect(created.SourceType).To(Equal(types.StepTemplateSourceBuiltin))
	g.Expect(created.SourceRef).To(Equal("builtin/http-health-check"))
	g.Expect(created.InstalledByUserAccountID).To(Equal(&actorID))
	g.Expect(created.Versions).To(HaveLen(1))
	g.Expect(created.Versions[0].Version).To(Equal("1.0.0"))
	g.Expect(created.Versions[0].ActionType).To(Equal("distr.http.check"))
	g.Expect(created.Versions[0].DefaultInputBindings).To(HaveKeyWithValue("url", "https://example.com/health"))

	templates, err := db.GetStepTemplatesByOrganizationID(ctx, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(templates).To(HaveLen(1))
	g.Expect(templates[0].ID).To(Equal(created.ID))
	g.Expect(templates[0].Versions).To(HaveLen(1))

	loaded, err := db.GetStepTemplate(ctx, created.ID, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(loaded.ID).To(Equal(created.ID))

	_, err = db.GetStepTemplate(ctx, created.ID, otherOrgID)
	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())
}

func TestStepTemplateRepositoryPreventsDuplicateInstall(t *testing.T) {
	ctx := stepTemplateDBTestContext(t)
	g := NewWithT(t)
	orgID, _, _ := createChannelDependencies(t, ctx)
	actorID := createReleaseBundleTestUser(t, ctx, orgID)
	request := stepTemplateImportFixture(orgID, actorID)
	g.Expect(func() error {
		_, err := db.ImportStepTemplate(ctx, request)
		return err
	}()).To(Succeed())

	_, err := db.ImportStepTemplate(ctx, request)

	g.Expect(errors.Is(err, apierrors.ErrAlreadyExists)).To(BeTrue())
}

func TestStepTemplateRepositoryRejectsInvalidImports(t *testing.T) {
	ctx := stepTemplateDBTestContext(t)
	orgID, _, _ := createChannelDependencies(t, ctx)
	actorID := createReleaseBundleTestUser(t, ctx, orgID)

	tests := []struct {
		name   string
		mutate func(*types.StepTemplateImport)
		want   string
	}{
		{
			name: "unknown action type",
			mutate: func(request *types.StepTemplateImport) {
				request.ActionType = "distr.unknown"
			},
			want: "unknown actionType",
		},
		{
			name: "invalid default inputs",
			mutate: func(request *types.StepTemplateImport) {
				request.DefaultInputBindings = map[string]any{"expectedStatusCodes": []any{float64(200)}}
			},
			want: "url",
		},
		{
			name: "non object input schema",
			mutate: func(request *types.StepTemplateImport) {
				request.InputSchema = map[string]any{"type": "array"}
			},
			want: "input schema must be a JSON object schema",
		},
		{
			name: "missing source ref",
			mutate: func(request *types.StepTemplateImport) {
				request.SourceRef = " "
			},
			want: "sourceRef is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			request := stepTemplateImportFixture(orgID, actorID)
			tt.mutate(&request)

			_, err := db.ImportStepTemplate(ctx, request)

			g.Expect(errors.Is(err, apierrors.ErrBadRequest)).To(BeTrue())
			g.Expect(err.Error()).To(ContainSubstring(tt.want))
		})
	}
}

func TestStepTemplateMigrationDefinesInstallSchema(t *testing.T) {
	g := NewWithT(t)

	up, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "126_step_templates.up.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	sql := string(up)
	g.Expect(sql).To(ContainSubstring("CREATE TABLE StepTemplate"))
	g.Expect(sql).To(ContainSubstring("CREATE TABLE StepTemplateVersion"))
	g.Expect(sql).To(ContainSubstring("steptemplate_organization_source_unique"))
	g.Expect(sql).To(ContainSubstring("steptemplateversion_template_version_unique"))
	g.Expect(sql).To(ContainSubstring("installed_by_useraccount_id UUID REFERENCES UserAccount(id) ON DELETE SET NULL"))

	down, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "126_step_templates.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(down)).To(ContainSubstring("DROP TABLE IF EXISTS StepTemplateVersion"))
	g.Expect(string(down)).To(ContainSubstring("DROP TABLE IF EXISTS StepTemplate"))
}

func stepTemplateDBTestContext(t *testing.T) context.Context {
	t.Helper()
	return channelDBTestContext(t)
}

func stepTemplateImportFixture(orgID, actorID uuid.UUID) types.StepTemplateImport {
	return types.StepTemplateImport{
		OrganizationID:            orgID,
		InstalledByUserAccountID:  &actorID,
		SourceType:                types.StepTemplateSourceBuiltin,
		SourceRef:                 "builtin/http-health-check",
		Name:                      "HTTP health check",
		Description:               "Checks that an HTTP endpoint returns a healthy status.",
		Category:                  "Health",
		Version:                   "1.0.0",
		ActionType:                "distr.http.check",
		ExecutionLocation:         "hub",
		InputSchema:               map[string]any{"type": "object", "additionalProperties": true},
		OutputSchema:              map[string]any{"type": "object", "additionalProperties": true},
		DefaultInputBindings:      map[string]any{"url": "https://example.com/health"},
		MinimumAgentVersion:       "1.0.0",
		CompatibleActionVersion:   types.AgentActionVersionV1,
		RuntimeCompatibilityNotes: "Uses the built-in HTTP check action.",
	}
}
