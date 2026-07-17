package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/externalexecutiontimestamp"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/gomega"
)

const task8CommandDatabaseURL = "postgres://operator@127.0.0.1:15432/distr"

type externalExecutionTimestampFakePool struct {
	closeCalls int
}

func (pool *externalExecutionTimestampFakePool) Close() {
	pool.closeCalls++
}

func (pool *externalExecutionTimestampFakePool) Exec(
	context.Context,
	string,
	...any,
) (pgconn.CommandTag, error) {
	panic("unexpected fake pool Exec")
}

func (pool *externalExecutionTimestampFakePool) Query(
	context.Context,
	string,
	...any,
) (pgx.Rows, error) {
	panic("unexpected fake pool Query")
}

func (pool *externalExecutionTimestampFakePool) QueryRow(
	context.Context,
	string,
	...any,
) pgx.Row {
	panic("unexpected fake pool QueryRow")
}

func (pool *externalExecutionTimestampFakePool) CopyFrom(
	context.Context,
	pgx.Identifier,
	[]string,
	pgx.CopyFromSource,
) (int64, error) {
	panic("unexpected fake pool CopyFrom")
}

func (pool *externalExecutionTimestampFakePool) Begin(context.Context) (pgx.Tx, error) {
	panic("unexpected fake pool Begin")
}

func externalExecutionTimestampCommandDraft(
	t *testing.T,
) types.ExternalExecutionTimestampManifest {
	t.Helper()
	rowID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	values := []*string{
		commandStringPointer("2026-07-15T10:00:00.000001"),
		commandStringPointer("2026-07-15T10:01:00.000002"),
		nil,
		nil,
		commandStringPointer("2026-07-15T10:04:00.000005"),
	}
	columns := []string{
		"created_at", "updated_at", "started_at", "completed_at",
		"callback_deadline_at",
	}
	rawCells := make([]types.ExternalExecutionTimestampRawCell, 0, 5)
	decisions := make([]types.ExternalExecutionTimestampCellDecision, 0, 5)
	for index, column := range columns {
		raw := types.ExternalExecutionTimestampRawCell{
			SourceTable: "externalexecution", SourceRowID: rowID,
			SourceColumn: column, ColumnOrdinal: uint8(index + 1),
			RawValue: values[index],
		}
		checksum, err := externalexecutiontimestamp.ComputeRawCellChecksum(raw)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
		raw.RawCellChecksum = checksum
		decision := types.ExternalExecutionTimestampDecisionUnresolved
		if raw.RawValue == nil {
			decision = types.ExternalExecutionTimestampDecisionNull
		}
		rawCells = append(rawCells, raw)
		decisions = append(decisions, types.ExternalExecutionTimestampCellDecision{
			ExternalExecutionTimestampRawCell: raw,
			Decision:                          decision,
			ConversionExpressionVersion:       externalexecutiontimestamp.ConversionExpressionVersion,
		})
	}
	rawSet, err := externalexecutiontimestamp.ComputeRawSetChecksum(rawCells)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	identity, err := externalexecutiontimestamp.ComputeDatabaseIdentityChecksum(
		137, []uuid.UUID{rowID}, nil, 5, rawSet,
	)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	manifest := types.ExternalExecutionTimestampManifest{
		ID:                       uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		DatabaseIdentityChecksum: identity, SourceSchemaVersion: 137,
		SnapshotStartedAt: "2026-07-15T10:20:00.000000Z",
		SnapshotEndedAt:   "2026-07-15T10:20:01.000000Z",
		ExecutionCount:    1, RawCellCount: 5, PopulatedCellCount: 3,
		RawCellChecksum: rawSet, ToolVersion: "distr-test",
		ConversionExpressionVersion: externalexecutiontimestamp.ConversionExpressionVersion,
		State:                       types.ExternalExecutionTimestampManifestStateDraft,
		Cells:                       decisions,
	}
	manifest.DecisionContentChecksum, err = externalexecutiontimestamp.ComputeDecisionContentChecksum(manifest)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return manifest
}

func commandStringPointer(value string) *string { return &value }

func externalExecutionTimestampCommandEnvironment() map[string]string {
	return map[string]string{
		"DATABASE_URL":                       "postgres://operator:DO_NOT_CONNECT@127.0.0.1:1/distr",
		"DISTR_TIMESTAMP_AUTHOR":             "release-author@example.invalid",
		"DISTR_TIMESTAMP_REVIEWER":           "release-reviewer@example.invalid",
		"DISTR_TIMESTAMP_EVIDENCE_REFERENCE": "evidence:bundle-42",
		"DISTR_TIMESTAMP_EVIDENCE_CHECKSUM":  "sha256:" + strings.Repeat("a", 64),
		"DISTR_RELEASE_COMMIT":               strings.Repeat("b", 40),
		"DISTR_IMAGE_DIGEST":                 "sha256:" + strings.Repeat("c", 64),
	}
}

func TestExternalExecutionTimestampCommandSealIsOfflineCreateNewAnd0600(t *testing.T) {
	g := NewWithT(t)
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "reviewed.json")
	outputPath := filepath.Join(directory, "approved.json")
	data, err := json.Marshal(externalExecutionTimestampCommandDraft(t))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(os.WriteFile(inputPath, data, 0o600)).To(Succeed())
	factoryCalls := 0
	var requestedMode os.FileMode
	environment := externalExecutionTimestampCommandEnvironment()

	stdout, stderr, err := executeExternalExecutionTimestampCommandForTest(
		t,
		externalExecutionTimestampCommandRuntime{
			Getenv: func(name string) string { return environment[name] },
			Now: func() time.Time {
				return time.Date(2026, 7, 15, 3, 4, 5, 123456000, time.UTC)
			},
			NewPool: func(context.Context, string) (externalExecutionTimestampPool, error) {
				factoryCalls++
				return nil, errors.New("pool must not be created while sealing")
			},
			OpenOutput: func(path string, flags int, mode os.FileMode) (io.WriteCloser, error) {
				requestedMode = mode
				return os.OpenFile(path, flags, mode)
			},
		},
		"seal-manifest", "--input", inputPath, "--output", outputPath,
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(stdout).To(BeEmpty())
	g.Expect(stderr).To(BeEmpty())
	g.Expect(factoryCalls).To(Equal(0))
	g.Expect(requestedMode).To(Equal(os.FileMode(0o600)))
	sealedData, err := os.ReadFile(outputPath)
	g.Expect(err).NotTo(HaveOccurred())
	var sealed types.ExternalExecutionTimestampManifest
	g.Expect(json.Unmarshal(sealedData, &sealed)).To(Succeed())
	g.Expect(sealed.State).To(Equal(types.ExternalExecutionTimestampManifestStateApproved))
	g.Expect(sealed.ApprovedAt).To(Equal("2026-07-15T03:04:05.123456Z"))
}

func TestExternalExecutionTimestampCommandSealNeverOverwrites(t *testing.T) {
	g := NewWithT(t)
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "reviewed.json")
	outputPath := filepath.Join(directory, "approved.json")
	data, err := json.Marshal(externalExecutionTimestampCommandDraft(t))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(os.WriteFile(inputPath, data, 0o600)).To(Succeed())
	g.Expect(os.WriteFile(outputPath, []byte("SENTINEL"), 0o600)).To(Succeed())
	environment := externalExecutionTimestampCommandEnvironment()

	_, _, err = executeExternalExecutionTimestampCommandForTest(
		t,
		externalExecutionTimestampCommandRuntime{
			Getenv: func(name string) string { return environment[name] },
		},
		"seal-manifest", "--input", inputPath, "--output", outputPath,
	)

	g.Expect(errors.Is(err, os.ErrExist)).To(BeTrue())
	contents, readErr := os.ReadFile(outputPath)
	g.Expect(readErr).NotTo(HaveOccurred())
	g.Expect(string(contents)).To(Equal("SENTINEL"))
}

func TestExternalExecutionTimestampCommandInspectSerializesDraftFromDirectPool(t *testing.T) {
	g := NewWithT(t)
	outputPath := filepath.Join(t.TempDir(), "draft.json")
	pool := &externalExecutionTimestampFakePool{}
	factoryCalls := 0
	inspectCalls := 0
	var requestedMode os.FileMode
	draft := externalExecutionTimestampCommandDraft(t)

	stdout, stderr, err := executeExternalExecutionTimestampCommandForTest(
		t,
		externalExecutionTimestampCommandRuntime{
			Getenv: func(name string) string {
				if name == "DATABASE_URL" {
					return task8CommandDatabaseURL
				}
				return ""
			},
			NewPool: func(context.Context, string) (externalExecutionTimestampPool, error) {
				factoryCalls++
				return pool, nil
			},
			Inspect: func(context.Context) (*types.ExternalExecutionTimestampManifest, error) {
				inspectCalls++
				return &draft, nil
			},
			OpenOutput: func(path string, flags int, mode os.FileMode) (io.WriteCloser, error) {
				requestedMode = mode
				return os.OpenFile(path, flags, mode)
			},
		},
		"inspect", "--output", outputPath,
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(stdout).To(BeEmpty())
	g.Expect(stderr).To(BeEmpty())
	g.Expect(factoryCalls).To(Equal(1))
	g.Expect(inspectCalls).To(Equal(1))
	g.Expect(pool.closeCalls).To(Equal(1))
	g.Expect(requestedMode).To(Equal(os.FileMode(0o600)))
	data, err := os.ReadFile(outputPath)
	g.Expect(err).NotTo(HaveOccurred())
	var actual types.ExternalExecutionTimestampManifest
	g.Expect(json.Unmarshal(data, &actual)).To(Succeed())
	g.Expect(actual.ID).To(Equal(draft.ID))
	g.Expect(actual.State).To(Equal(types.ExternalExecutionTimestampManifestStateDraft))
}

func TestExternalExecutionTimestampCommandSealSupportsStdinStdout(t *testing.T) {
	g := NewWithT(t)
	data, err := json.Marshal(externalExecutionTimestampCommandDraft(t))
	g.Expect(err).NotTo(HaveOccurred())
	environment := externalExecutionTimestampCommandEnvironment()

	stdout, stderr, err := executeExternalExecutionTimestampCommandForTest(
		t,
		externalExecutionTimestampCommandRuntime{
			Stdin:  bytes.NewReader(data),
			Getenv: func(name string) string { return environment[name] },
		},
		"seal-manifest", "--input", "-", "--output", "-",
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(stderr).To(BeEmpty())
	var sealed types.ExternalExecutionTimestampManifest
	g.Expect(json.Unmarshal([]byte(stdout), &sealed)).To(Succeed())
	g.Expect(sealed.State).To(Equal(types.ExternalExecutionTimestampManifestStateApproved))
}

func TestExternalExecutionTimestampCommandValidateWritesRootReportFromDirectPool(
	t *testing.T,
) {
	g := NewWithT(t)
	manifest := externalExecutionTimestampCommandDraft(t)
	data, err := json.Marshal(manifest)
	g.Expect(err).NotTo(HaveOccurred())
	pool := &externalExecutionTimestampFakePool{}
	validateCalls := 0
	report := &types.ExternalExecutionTimestampValidationReport{
		ManifestID: manifest.ID, SchemaVersion: 138,
		ExecutionCount: manifest.ExecutionCount, RawCellCount: manifest.RawCellCount,
		PopulatedCellCount:       manifest.PopulatedCellCount,
		UnresolvedCellCount:      manifest.PopulatedCellCount,
		RawSetChecksum:           manifest.RawCellChecksum,
		DatabaseIdentityChecksum: manifest.DatabaseIdentityChecksum,
		DecisionContentChecksum:  manifest.DecisionContentChecksum,
	}

	stdout, stderr, err := executeExternalExecutionTimestampCommandForTest(
		t,
		externalExecutionTimestampCommandRuntime{
			Stdin: bytes.NewReader(data),
			Getenv: func(name string) string {
				if name == "DATABASE_URL" {
					return task8CommandDatabaseURL
				}
				return ""
			},
			NewPool: func(context.Context, string) (externalExecutionTimestampPool, error) {
				return pool, nil
			},
			Validate: func(
				_ context.Context,
				actual types.ExternalExecutionTimestampManifest,
			) (*types.ExternalExecutionTimestampValidationReport, error) {
				validateCalls++
				g.Expect(actual.ID).To(Equal(manifest.ID))
				g.Expect(actual.SupersedesManifestID).To(BeNil())
				return report, nil
			},
		},
		"validate-manifest", "--manifest", "-",
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(stderr).To(BeEmpty())
	g.Expect(validateCalls).To(Equal(1))
	g.Expect(pool.closeCalls).To(Equal(1))
	var actual types.ExternalExecutionTimestampValidationReport
	g.Expect(json.Unmarshal([]byte(stdout), &actual)).To(Succeed())
	g.Expect(actual).To(Equal(*report))
}

func TestExternalExecutionTimestampCommandValidateRedactsChildFailure(t *testing.T) {
	manifest := externalExecutionTimestampCommandDraft(t)
	manifest.ID = uuid.New()
	parentID := uuid.New()
	manifest.SupersedesManifestID = &parentID
	secret := "CHILD_MANIFEST_SECRET_CANARY"
	manifest.Cells[0].EvidenceReference = secret
	checksum, err := externalexecutiontimestamp.ComputeDecisionContentChecksum(manifest)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	manifest.DecisionContentChecksum = checksum
	data, err := json.Marshal(manifest)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	factoryCalls := 0

	_, stderr, err := executeExternalExecutionTimestampCommandForTest(
		t,
		externalExecutionTimestampCommandRuntime{
			Stdin: bytes.NewReader(data),
			Getenv: func(name string) string {
				if name == "DATABASE_URL" {
					return task8CommandDatabaseURL
				}
				return ""
			},
			NewPool: func(context.Context, string) (externalExecutionTimestampPool, error) {
				factoryCalls++
				return &externalExecutionTimestampFakePool{}, nil
			},
		},
		"validate-manifest", "--manifest", "-",
	)

	g := NewWithT(t)
	g.Expect(err).To(MatchError(ContainSubstring(
		"superseding manifest live validation requires verified-tip provenance",
	)))
	g.Expect(err.Error()).NotTo(ContainSubstring(secret))
	g.Expect(stderr).NotTo(ContainSubstring(secret))
	g.Expect(factoryCalls).To(Equal(0))
}

func TestExternalExecutionTimestampCommandValidateRedactsRepositoryError(t *testing.T) {
	manifest := externalExecutionTimestampCommandDraft(t)
	data, err := json.Marshal(manifest)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	pool := &externalExecutionTimestampFakePool{}
	secret := "VALIDATION_ERROR_SECRET_CANARY"

	_, stderr, err := executeExternalExecutionTimestampCommandForTest(
		t,
		externalExecutionTimestampCommandRuntime{
			Stdin: bytes.NewReader(data),
			Getenv: func(name string) string {
				if name == "DATABASE_URL" {
					return task8CommandDatabaseURL
				}
				return ""
			},
			NewPool: func(context.Context, string) (externalExecutionTimestampPool, error) {
				return pool, nil
			},
			Validate: func(
				context.Context,
				types.ExternalExecutionTimestampManifest,
			) (*types.ExternalExecutionTimestampValidationReport, error) {
				return nil, fmt.Errorf("repository failure containing %s", secret)
			},
		},
		"validate-manifest", "--manifest", "-",
	)

	g := NewWithT(t)
	g.Expect(err).To(MatchError(ContainSubstring("manifest validation failed")))
	g.Expect(err.Error()).NotTo(ContainSubstring(secret))
	g.Expect(stderr).NotTo(ContainSubstring(secret))
	g.Expect(pool.closeCalls).To(Equal(1))
}

func TestExternalExecutionTimestampCommandRejectsUnknownAndTrailingJSONWithoutLeaks(
	t *testing.T,
) {
	manifestJSON, err := json.Marshal(externalExecutionTimestampCommandDraft(t))
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	unknown := append([]byte(nil), manifestJSON[:len(manifestJSON)-1]...)
	unknown = append(unknown, []byte(`,"unknown":"SECRET_CANARY"}`)...)
	tests := []struct {
		name  string
		input []byte
	}{
		{name: "unknown field", input: unknown},
		{name: "trailing document", input: append(manifestJSON, []byte(` {"secret":"SECRET_CANARY"}`)...)},
		{name: "malformed value", input: []byte(`{"id":"SECRET_CANARY"}`)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			environment := externalExecutionTimestampCommandEnvironment()
			_, stderr, err := executeExternalExecutionTimestampCommandForTest(
				t,
				externalExecutionTimestampCommandRuntime{
					Stdin:  bytes.NewReader(test.input),
					Getenv: func(name string) string { return environment[name] },
				},
				"seal-manifest", "--input", "-", "--output",
				filepath.Join(t.TempDir(), "approved.json"),
			)
			g := NewWithT(t)
			g.Expect(err).To(MatchError(ContainSubstring("manifest JSON")))
			g.Expect(err.Error()).NotTo(ContainSubstring("SECRET_CANARY"))
			g.Expect(stderr).NotTo(ContainSubstring("SECRET_CANARY"))
		})
	}
}

func TestExternalExecutionTimestampCommandSealRequiresRuntimeEvidence(t *testing.T) {
	environment := externalExecutionTimestampCommandEnvironment()
	delete(environment, "DISTR_TIMESTAMP_REVIEWER")
	data, err := json.Marshal(externalExecutionTimestampCommandDraft(t))
	NewWithT(t).Expect(err).NotTo(HaveOccurred())

	_, _, err = executeExternalExecutionTimestampCommandForTest(
		t,
		externalExecutionTimestampCommandRuntime{
			Stdin:  bytes.NewReader(data),
			Getenv: func(name string) string { return environment[name] },
		},
		"seal-manifest", "--input", "-", "--output", "-",
	)

	NewWithT(t).Expect(err).To(MatchError(ContainSubstring(
		"--reviewer or DISTR_TIMESTAMP_REVIEWER is required",
	)))
}

func TestExternalExecutionTimestampCommandInspectUsesOnlyDirectPoolFactory(t *testing.T) {
	factoryCalls := 0
	secret := "DATABASE_PASSWORD_CANARY"
	_, _, err := executeExternalExecutionTimestampCommandForTest(
		t,
		externalExecutionTimestampCommandRuntime{
			Getenv: func(name string) string {
				if name == "DATABASE_URL" {
					return "postgres://operator:" + secret + "@127.0.0.1:1/distr"
				}
				return ""
			},
			NewPool: func(_ context.Context, databaseURL string) (externalExecutionTimestampPool, error) {
				factoryCalls++
				return nil, fmt.Errorf("failed for %s", databaseURL)
			},
		},
		"inspect", "--output", "-",
	)

	g := NewWithT(t)
	g.Expect(factoryCalls).To(Equal(1))
	g.Expect(err).To(MatchError(ContainSubstring("open direct database pool")))
	g.Expect(err.Error()).NotTo(ContainSubstring(secret))
}

func TestExternalExecutionTimestampCommandClosesPoolReturnedWithFactoryError(t *testing.T) {
	pool := &externalExecutionTimestampFakePool{}
	_, _, err := executeExternalExecutionTimestampCommandForTest(
		t,
		externalExecutionTimestampCommandRuntime{
			Getenv: func(name string) string {
				if name == "DATABASE_URL" {
					return task8CommandDatabaseURL
				}
				return ""
			},
			NewPool: func(context.Context, string) (externalExecutionTimestampPool, error) {
				return pool, errors.New("injected constructor error")
			},
		},
		"inspect", "--output", "-",
	)

	g := NewWithT(t)
	g.Expect(err).To(MatchError(ContainSubstring("open direct database pool")))
	g.Expect(pool.closeCalls).To(Equal(1))
}

type externalExecutionTimestampFailingWriteCloser struct {
	file      *os.File
	failWrite bool
	failClose bool
}

func (writer *externalExecutionTimestampFailingWriteCloser) Write(data []byte) (int, error) {
	if writer.failWrite {
		if len(data) > 8 {
			_, _ = writer.file.Write(data[:8])
		}
		return 0, errors.New("injected output write failure")
	}
	return writer.file.Write(data)
}

func (writer *externalExecutionTimestampFailingWriteCloser) Close() error {
	err := writer.file.Close()
	if writer.failClose {
		return errors.Join(err, errors.New("injected output close failure"))
	}
	return err
}

func TestExternalExecutionTimestampCommandRemovesNewPartialOutputOnFailure(t *testing.T) {
	for _, failure := range []string{"write", "close"} {
		t.Run(failure, func(t *testing.T) {
			g := NewWithT(t)
			outputPath := filepath.Join(t.TempDir(), "draft.json")
			pool := &externalExecutionTimestampFakePool{}
			draft := externalExecutionTimestampCommandDraft(t)
			_, _, err := executeExternalExecutionTimestampCommandForTest(
				t,
				externalExecutionTimestampCommandRuntime{
					Getenv: func(name string) string {
						if name == "DATABASE_URL" {
							return task8CommandDatabaseURL
						}
						return ""
					},
					NewPool: func(context.Context, string) (externalExecutionTimestampPool, error) {
						return pool, nil
					},
					Inspect: func(context.Context) (*types.ExternalExecutionTimestampManifest, error) {
						return &draft, nil
					},
					OpenOutput: func(path string, flags int, mode os.FileMode) (io.WriteCloser, error) {
						file, err := os.OpenFile(path, flags, mode)
						if err != nil {
							return nil, err
						}
						return &externalExecutionTimestampFailingWriteCloser{
							file: file, failWrite: failure == "write", failClose: failure == "close",
						}, nil
					},
				},
				"inspect", "--output", outputPath,
			)
			g.Expect(err).To(HaveOccurred())
			_, statErr := os.Stat(outputPath)
			g.Expect(errors.Is(statErr, os.ErrNotExist)).To(BeTrue())
		})
	}
}

func executeExternalExecutionTimestampCommandForTest(
	t *testing.T,
	runtime externalExecutionTimestampCommandRuntime,
	args ...string,
) (string, string, error) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if runtime.Stdin == nil {
		runtime.Stdin = strings.NewReader("")
	}
	if runtime.Stdout == nil {
		runtime.Stdout = &stdout
	}
	if runtime.Stderr == nil {
		runtime.Stderr = &stderr
	}
	if runtime.Getenv == nil {
		runtime.Getenv = func(string) string { return "" }
	}
	command := newExternalExecutionTimestampsCommand(runtime)
	command.SetArgs(args)
	command.SetIn(runtime.Stdin)
	command.SetOut(runtime.Stdout)
	command.SetErr(runtime.Stderr)
	err := command.Execute()
	return stdout.String(), stderr.String(), err
}

func externalExecutionTimestampCommandApprovedManifest(
	t *testing.T,
) types.ExternalExecutionTimestampManifest {
	t.Helper()
	draft := externalExecutionTimestampCommandDraft(t)
	approved, err := externalexecutiontimestamp.SealManifest(
		draft,
		types.ExternalExecutionTimestampSealOptions{
			AuthorIdentity:          "release-author@example.invalid",
			ReviewerIdentity:        "release-reviewer@example.invalid",
			EvidenceBundleReference: "evidence:bundle-42",
			EvidenceBundleChecksum:  "sha256:" + strings.Repeat("a", 64),
			TargetReleaseCommit:     strings.Repeat("b", 40),
			TargetImageDigest:       "sha256:" + strings.Repeat("c", 64),
		},
		time.Date(2026, 7, 15, 10, 30, 0, 0, time.UTC),
	)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return approved
}

func TestApplyExternalExecutionTimestampCommandDryRun(t *testing.T) {
	manifest := externalExecutionTimestampCommandApprovedManifest(t)
	data, err := json.Marshal(manifest)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	pool := &externalExecutionTimestampFakePool{}
	var captured types.ExternalExecutionTimestampApplyRequest
	report := &types.ExternalExecutionTimestampApplyReport{
		ManifestID: manifest.ID,
		DryRun:     true, WouldPopulateCount: 3, UnresolvedCount: 3,
		RawSetChecksum:           manifest.RawCellChecksum,
		DatabaseIdentityChecksum: manifest.DatabaseIdentityChecksum,
	}

	stdout, stderr, err := executeExternalExecutionTimestampCommandForTest(
		t,
		externalExecutionTimestampCommandRuntime{
			Stdin: bytes.NewReader(data),
			Getenv: func(name string) string {
				if name == "DATABASE_URL" {
					return task8CommandDatabaseURL
				}
				return ""
			},
			NewPool: func(context.Context, string) (externalExecutionTimestampPool, error) {
				return pool, nil
			},
			Apply: func(
				_ context.Context,
				request types.ExternalExecutionTimestampApplyRequest,
			) (*types.ExternalExecutionTimestampApplyReport, error) {
				captured = request
				return report, nil
			},
		},
		"apply", "--manifest", "-",
	)

	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(stderr).To(BeEmpty())
	g.Expect(captured.Manifest.ID).To(Equal(manifest.ID))
	g.Expect(captured.Apply).To(BeFalse())
	g.Expect(pool.closeCalls).To(Equal(1))
	var actual types.ExternalExecutionTimestampApplyReport
	g.Expect(json.Unmarshal([]byte(stdout), &actual)).To(Succeed())
	g.Expect(actual).To(Equal(*report))
}

func TestApplyExternalExecutionTimestampCommandUsesApprovedEvidence(t *testing.T) {
	manifest := externalExecutionTimestampCommandApprovedManifest(t)
	data, err := json.Marshal(manifest)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	environment := map[string]string{
		"DATABASE_URL":                      task8CommandDatabaseURL,
		"DISTR_TIMESTAMP_FENCE_ID":          "fence:release-42",
		"DISTR_TIMESTAMP_BACKUP_REFERENCE":  "backup:release-42",
		"DISTR_TIMESTAMP_BACKUP_CHECKSUM":   "sha256:" + strings.Repeat("d", 64),
		"DISTR_TIMESTAMP_RESTORE_REFERENCE": "restore:release-42",
		"DISTR_TIMESTAMP_RESTORE_CHECKSUM":  "sha256:" + strings.Repeat("e", 64),
	}
	pool := &externalExecutionTimestampFakePool{}
	var captured types.ExternalExecutionTimestampApplyRequest

	_, stderr, err := executeExternalExecutionTimestampCommandForTest(
		t,
		externalExecutionTimestampCommandRuntime{
			Stdin:  bytes.NewReader(data),
			Getenv: func(name string) string { return environment[name] },
			NewPool: func(context.Context, string) (externalExecutionTimestampPool, error) {
				return pool, nil
			},
			Apply: func(
				_ context.Context,
				request types.ExternalExecutionTimestampApplyRequest,
			) (*types.ExternalExecutionTimestampApplyReport, error) {
				captured = request
				return &types.ExternalExecutionTimestampApplyReport{
					ManifestID: request.Manifest.ID,
				}, nil
			},
		},
		"apply", "--manifest", "-", "--apply",
	)

	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(stderr).To(BeEmpty())
	g.Expect(captured.Apply).To(BeTrue())
	g.Expect(captured.WriterFenceIdentifier).To(Equal("fence:release-42"))
	g.Expect(captured.BackupReference).To(Equal("backup:release-42"))
	g.Expect(captured.BackupChecksum).To(Equal(environment["DISTR_TIMESTAMP_BACKUP_CHECKSUM"]))
	g.Expect(captured.RestoreVerificationReference).To(Equal("restore:release-42"))
	g.Expect(captured.RestoreVerificationChecksum).To(Equal(
		environment["DISTR_TIMESTAMP_RESTORE_CHECKSUM"],
	))
	g.Expect(pool.closeCalls).To(Equal(1))
}

func TestApplyExternalExecutionTimestampCommandRejectsEvidenceFlagsWithoutApply(
	t *testing.T,
) {
	manifest := externalExecutionTimestampCommandApprovedManifest(t)
	data, err := json.Marshal(manifest)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	factoryCalls := 0

	_, _, err = executeExternalExecutionTimestampCommandForTest(
		t,
		externalExecutionTimestampCommandRuntime{
			Stdin: bytes.NewReader(data),
			Getenv: func(name string) string {
				if name == "DATABASE_URL" {
					return task8CommandDatabaseURL
				}
				return ""
			},
			NewPool: func(context.Context, string) (externalExecutionTimestampPool, error) {
				factoryCalls++
				return &externalExecutionTimestampFakePool{}, nil
			},
		},
		"apply", "--manifest", "-", "--writer-fence-id", "fence:must-not-run",
	)

	g := NewWithT(t)
	g.Expect(err).To(MatchError(ContainSubstring("--apply")))
	g.Expect(factoryCalls).To(BeZero())
}

func TestApplyExternalExecutionTimestampCommandRedactsRepositoryFailure(t *testing.T) {
	manifest := externalExecutionTimestampCommandApprovedManifest(t)
	data, err := json.Marshal(manifest)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	pool := &externalExecutionTimestampFakePool{}
	secret := "raw=2026-07-15T10:00:00 evidence=backup-secret"

	stdout, stderr, err := executeExternalExecutionTimestampCommandForTest(
		t,
		externalExecutionTimestampCommandRuntime{
			Stdin: bytes.NewReader(data),
			Getenv: func(name string) string {
				if name == "DATABASE_URL" {
					return task8CommandDatabaseURL
				}
				return ""
			},
			NewPool: func(context.Context, string) (externalExecutionTimestampPool, error) {
				return pool, nil
			},
			Apply: func(
				context.Context,
				types.ExternalExecutionTimestampApplyRequest,
			) (*types.ExternalExecutionTimestampApplyReport, error) {
				return nil, errors.New(secret)
			},
		},
		"apply", "--manifest", "-",
	)

	g := NewWithT(t)
	g.Expect(err).To(MatchError("manifest apply failed"))
	g.Expect(stdout + stderr + err.Error()).NotTo(ContainSubstring(secret))
	g.Expect(pool.closeCalls).To(Equal(1))
}

func TestVerifyExternalExecutionTimestampCommand(t *testing.T) {
	manifestID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	pool := &externalExecutionTimestampFakePool{}
	var captured uuid.UUID
	report := &types.ExternalExecutionTimestampVerificationReport{
		ManifestID:              manifestID,
		SchemaVersion:           138,
		SourceExecutionCount:    5,
		CurrentExecutionCount:   5,
		RawSetChecksum:          "sha256:" + strings.Repeat("1", 64),
		DecisionContentChecksum: "sha256:" + strings.Repeat("2", 64),
	}

	stdout, stderr, err := executeExternalExecutionTimestampCommandForTest(
		t,
		externalExecutionTimestampCommandRuntime{
			Getenv: func(name string) string {
				if name == "DATABASE_URL" {
					return task8CommandDatabaseURL
				}
				return ""
			},
			NewPool: func(context.Context, string) (externalExecutionTimestampPool, error) {
				return pool, nil
			},
			Verify: func(
				_ context.Context,
				actual uuid.UUID,
			) (*types.ExternalExecutionTimestampVerificationReport, error) {
				captured = actual
				return report, nil
			},
		},
		"verify", "--manifest-id", manifestID.String(),
	)

	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(stderr).To(BeEmpty())
	g.Expect(captured).To(Equal(manifestID))
	g.Expect(pool.closeCalls).To(Equal(1))
	var actual types.ExternalExecutionTimestampVerificationReport
	g.Expect(json.Unmarshal([]byte(stdout), &actual)).To(Succeed())
	g.Expect(actual).To(Equal(*report))
}

func TestVerifyExternalExecutionTimestampCommandRejectsInvalidManifestID(t *testing.T) {
	factoryCalls := 0
	_, _, err := executeExternalExecutionTimestampCommandForTest(
		t,
		externalExecutionTimestampCommandRuntime{
			NewPool: func(context.Context, string) (externalExecutionTimestampPool, error) {
				factoryCalls++
				return &externalExecutionTimestampFakePool{}, nil
			},
		},
		"verify", "--manifest-id", "not-a-uuid",
	)
	g := NewWithT(t)
	g.Expect(err).To(MatchError(ContainSubstring("manifest id")))
	g.Expect(factoryCalls).To(BeZero())
}

func TestExternalExecutionTimestampReadinessCommandUsesDirectPool(t *testing.T) {
	pool := &externalExecutionTimestampFakePool{}
	report := &types.ExternalExecutionTimestampReadiness{
		SchemaVersion: 138, TransitionKind: "ZERO_HISTORY",
	}
	readinessCalls := 0
	stdout, stderr, err := executeExternalExecutionTimestampCommandForTest(
		t,
		externalExecutionTimestampCommandRuntime{
			Getenv: func(name string) string {
				if name == "DATABASE_URL" {
					return task8CommandDatabaseURL
				}
				return ""
			},
			NewPool: func(context.Context, string) (externalExecutionTimestampPool, error) {
				return pool, nil
			},
			Readiness: func(
				context.Context,
			) (*types.ExternalExecutionTimestampReadiness, error) {
				readinessCalls++
				return report, nil
			},
		},
		"readiness",
	)
	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(stderr).To(BeEmpty())
	g.Expect(readinessCalls).To(Equal(1))
	g.Expect(pool.closeCalls).To(Equal(1))
	decoder := json.NewDecoder(strings.NewReader(stdout))
	var actual types.ExternalExecutionTimestampReadiness
	g.Expect(decoder.Decode(&actual)).To(Succeed())
	g.Expect(actual).To(Equal(*report))
	var trailing any
	g.Expect(decoder.Decode(&trailing)).To(MatchError(io.EOF))
}

type task8CommandTestDatabase struct {
	url    string
	schema string
	pool   *pgxpool.Pool
}

func task8CommandMigrationVersion(t *testing.T, path string) int {
	t.Helper()
	version, err := strconv.Atoi(strings.SplitN(filepath.Base(path), "_", 2)[0])
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return version
}

func task8CommandPoolConfig(
	t *testing.T,
	databaseURL string,
	schema string,
) *pgxpool.Config {
	t.Helper()
	config, err := pgxpool.ParseConfig(databaseURL)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	config.AfterConnect = func(ctx context.Context, connection *pgx.Conn) error {
		_, err := connection.Exec(ctx, "SET search_path TO "+quotedSchema)
		return err
	}
	return config
}

func newTask8CommandTestDatabase(t *testing.T) *task8CommandTestDatabase {
	t.Helper()
	databaseURL := os.Getenv("DISTR_TEST_DATABASE_URL")
	if strings.TrimSpace(databaseURL) == "" {
		t.Skip("DISTR_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, databaseURL)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	t.Cleanup(admin.Close)
	schema := "timestamp_task8_cmd_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	_, err = admin.Exec(ctx, "CREATE SCHEMA "+quotedSchema)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() {
		_, dropErr := admin.Exec(
			context.Background(), "DROP SCHEMA IF EXISTS "+quotedSchema+" CASCADE",
		)
		if dropErr != nil {
			t.Logf("drop task 8 command schema: %v", dropErr)
		}
	})
	pool, err := pgxpool.NewWithConfig(
		ctx, task8CommandPoolConfig(t, databaseURL, schema),
	)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	t.Cleanup(pool.Close)

	files, err := filepath.Glob(filepath.Join(
		"..", "..", "..", "internal", "migrations", "sql", "*.up.sql",
	))
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	sort.Slice(files, func(left, right int) bool {
		return task8CommandMigrationVersion(t, files[left]) <
			task8CommandMigrationVersion(t, files[right])
	})
	for _, file := range files {
		if task8CommandMigrationVersion(t, file) > 138 {
			continue
		}
		data, readErr := os.ReadFile(file)
		NewWithT(t).Expect(readErr).NotTo(HaveOccurred())
		_, execErr := pool.Exec(ctx, string(data))
		NewWithT(t).Expect(execErr).NotTo(HaveOccurred(), file)
	}
	_, err = pool.Exec(ctx, `
CREATE TABLE schema_migrations (
	version BIGINT PRIMARY KEY,
  dirty BOOLEAN NOT NULL
);
INSERT INTO schema_migrations (version, dirty) VALUES (138, FALSE)`)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return &task8CommandTestDatabase{
		url: databaseURL, schema: schema, pool: pool,
	}
}

func (database *task8CommandTestDatabase) newPool(
	ctx context.Context,
) (externalExecutionTimestampPool, error) {
	config, err := pgxpool.ParseConfig(database.url)
	if err != nil {
		return nil, err
	}
	quotedSchema := pgx.Identifier{database.schema}.Sanitize()
	config.AfterConnect = func(ctx context.Context, connection *pgx.Conn) error {
		_, err := connection.Exec(ctx, "SET search_path TO "+quotedSchema)
		return err
	}
	return pgxpool.NewWithConfig(ctx, config)
}

func task8CommandInsertPairedExecution(
	t *testing.T,
	database *task8CommandTestDatabase,
) uuid.UUID {
	t.Helper()
	_, err := database.pool.Exec(context.Background(), `
DO $$
DECLARE item RECORD;
BEGIN
  FOR item IN
    SELECT relation.relname AS table_name, constraint_row.conname
    FROM pg_constraint constraint_row
    JOIN pg_class relation ON relation.oid=constraint_row.conrelid
    WHERE relation.relnamespace=to_regnamespace(current_schema())
      AND relation.relname IN ('externalexecution','externalexecutionevent')
      AND constraint_row.contype='f'
  LOOP
    EXECUTE format('ALTER TABLE %I DROP CONSTRAINT %I',
      item.table_name, item.conname);
  END LOOP;
END $$`)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	var transitionedAt time.Time
	err = database.pool.QueryRow(context.Background(), `
SELECT transitioned_at FROM ExternalExecutionTimestampExpandState`).Scan(
		&transitionedAt,
	)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	executionID := uuid.New()
	created := transitionedAt.UTC().Add(time.Second)
	updated := created.Add(time.Second)
	deadline := updated.Add(time.Second)
	_, err = database.pool.Exec(context.Background(), `
INSERT INTO ExternalExecution (
  id, created_at, created_at_instant, updated_at, updated_at_instant,
  started_at, started_at_instant, completed_at, completed_at_instant,
  callback_deadline_at, callback_deadline_at_instant,
  organization_id, step_run_id, task_id, deployment_plan_id,
  deployment_plan_target_id, deployment_target_id, application_id,
  release_bundle_id, component, plan_checksum, idempotency_key,
  expected_state_version, expected_version, expected_image, expected_platform,
  expected_config_reference, expected_config_checksum
) VALUES (
  $1, $2 AT TIME ZONE 'UTC', $2, $3 AT TIME ZONE 'UTC', $3,
  NULL, CAST(NULL AS timestamptz), NULL, CAST(NULL AS timestamptz),
  $4 AT TIME ZONE 'UTC', $4,
  $5, $6, $7, $8, $9, $10, $11, $12,
  'api-image', 'sha256:' || repeat('1', 64), $13, 0, '2.0.0',
  'repo/image@sha256:' || repeat('2', 64), 'linux/amd64',
  'config:readiness-command', 'sha256:' || repeat('3', 64)
)`, executionID, created, updated, deadline,
		uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(),
		uuid.New(), uuid.New(), "readiness-command-"+executionID.String())
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	evolved := deadline.Add(time.Second)
	_, err = database.pool.Exec(context.Background(), `
UPDATE ExternalExecution
SET updated_at=$2 AT TIME ZONE 'UTC', updated_at_instant=$2
WHERE id=$1`, executionID, evolved)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return executionID
}

func TestExternalExecutionTimestampReadinessCommandValidatesRealPairedLifecycle(
	t *testing.T,
) {
	database := newTask8CommandTestDatabase(t)
	executionID := task8CommandInsertPairedExecution(t, database)
	runtime := externalExecutionTimestampCommandRuntime{
		Getenv: func(name string) string {
			if name == "DATABASE_URL" {
				return database.url
			}
			return ""
		},
		NewPool: func(
			ctx context.Context,
			_ string,
		) (externalExecutionTimestampPool, error) {
			return database.newPool(ctx)
		},
	}

	stdout, stderr, err := executeExternalExecutionTimestampCommandForTest(
		t, runtime, "readiness",
	)
	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(stderr).To(BeEmpty())
	decoder := json.NewDecoder(strings.NewReader(stdout))
	var report types.ExternalExecutionTimestampReadiness
	g.Expect(decoder.Decode(&report)).To(Succeed())
	g.Expect(report.TransitionKind).To(Equal("ZERO_HISTORY"))
	g.Expect(report.ExecutionCount).To(Equal(uint64(1)))
	g.Expect(report.PostTransitionPairCount).To(Equal(uint64(5)))
	var trailing any
	g.Expect(decoder.Decode(&trailing)).To(MatchError(io.EOF))

	task8CommandCorruptTimestampPair(t, database, `
UPDATE ExternalExecution
SET updated_at_instant=updated_at_instant + INTERVAL '1 hour'
WHERE id=$1`, executionID)
	_, _, err = executeExternalExecutionTimestampCommandForTest(
		t, runtime, "readiness",
	)
	g.Expect(err).To(MatchError(ContainSubstring("updated_at pair")))
}

func task8CommandCorruptTimestampPair(
	t *testing.T,
	database *task8CommandTestDatabase,
	statement string,
	arguments ...any,
) {
	t.Helper()
	g := NewWithT(t)
	_, err := database.pool.Exec(context.Background(), `
ALTER TABLE ExternalExecution
DISABLE TRIGGER ExternalExecution_timestamp_pair_guard`)
	g.Expect(err).NotTo(HaveOccurred())
	guardEnabled := false
	t.Cleanup(func() {
		if guardEnabled {
			return
		}
		_, cleanupErr := database.pool.Exec(context.Background(), `
ALTER TABLE ExternalExecution
ENABLE TRIGGER ExternalExecution_timestamp_pair_guard`)
		g.Expect(cleanupErr).NotTo(HaveOccurred())
	})
	_, err = database.pool.Exec(context.Background(), statement, arguments...)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = database.pool.Exec(context.Background(), `
ALTER TABLE ExternalExecution
ENABLE TRIGGER ExternalExecution_timestamp_pair_guard`)
	g.Expect(err).NotTo(HaveOccurred())
	guardEnabled = true
}

func TestExternalExecutionTimestampReadinessCommandRejectsMalformedLedger(
	t *testing.T,
) {
	for _, test := range []struct {
		name      string
		arrange   string
		wantError string
	}{
		{
			name: "view",
			arrange: `DROP TABLE schema_migrations;
CREATE VIEW schema_migrations AS
SELECT 138::BIGINT AS version, FALSE::BOOLEAN AS dirty`,
			wantError: "ordinary table",
		},
		{
			name:      "missing primary key",
			arrange:   `ALTER TABLE schema_migrations DROP CONSTRAINT schema_migrations_pkey`,
			wantError: "primary key",
		},
		{
			name: "wrong primary key",
			arrange: `ALTER TABLE schema_migrations
DROP CONSTRAINT schema_migrations_pkey;
ALTER TABLE schema_migrations ADD PRIMARY KEY (dirty)`,
			wantError: "primary key",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			database := newTask8CommandTestDatabase(t)
			_, err := database.pool.Exec(context.Background(), test.arrange)
			NewWithT(t).Expect(err).NotTo(HaveOccurred())
			runtime := externalExecutionTimestampCommandRuntime{
				Getenv: func(name string) string {
					if name == "DATABASE_URL" {
						return database.url
					}
					return ""
				},
				NewPool: func(
					ctx context.Context,
					_ string,
				) (externalExecutionTimestampPool, error) {
					return database.newPool(ctx)
				},
			}
			_, _, err = executeExternalExecutionTimestampCommandForTest(
				t, runtime, "readiness",
			)
			NewWithT(t).Expect(err).To(MatchError(ContainSubstring(
				test.wantError,
			)))
		})
	}
}
