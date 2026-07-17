package cmd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/migrations"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

const migrateRecoverDirtyTestReason = "Recover verified interrupted migration"

type migrateRecoverDirtyRunnerRecorder struct {
	plan         types.TimestampDirtyRecoveryPlan
	result       types.TimestampDirtyRecoveryResult
	planOptions  migrations.TimestampDirtyRecoveryPlanOptions
	applyPlan    types.TimestampDirtyRecoveryPlan
	applyOptions migrations.TimestampDirtyRecoveryApplyOptions
	planCalls    uint64
	applyCalls   uint64
	closeCalls   uint64
	planErr      error
	applyErr     error
	closeErr     error
}

func (recorder *migrateRecoverDirtyRunnerRecorder) PlanTimestampDirtyRecovery(
	_ context.Context,
	options migrations.TimestampDirtyRecoveryPlanOptions,
) (types.TimestampDirtyRecoveryPlan, error) {
	recorder.planCalls++
	recorder.planOptions = options
	return recorder.plan, recorder.planErr
}

func (recorder *migrateRecoverDirtyRunnerRecorder) ApplyTimestampDirtyRecovery(
	_ context.Context,
	plan types.TimestampDirtyRecoveryPlan,
	options migrations.TimestampDirtyRecoveryApplyOptions,
) (types.TimestampDirtyRecoveryResult, error) {
	recorder.applyCalls++
	recorder.applyPlan = plan
	recorder.applyOptions = options
	result := recorder.result
	result.PlanChecksum = options.PlanDocumentChecksum
	return result, recorder.applyErr
}

func (recorder *migrateRecoverDirtyRunnerRecorder) Close() error {
	recorder.closeCalls++
	return recorder.closeErr
}

func migrateRecoverDirtyTestPlan() types.TimestampDirtyRecoveryPlan {
	return types.TimestampDirtyRecoveryPlan{
		FormatVersion:         types.TimestampDirtyRecoveryFormatVersion,
		RecordType:            types.TimestampDirtyRecoveryRecordTypePlan,
		RecoveryID:            uuid.MustParse("11111111-2222-4333-8444-555555555555"),
		CreatedAt:             time.Date(2026, 7, 17, 1, 2, 3, 4000, time.UTC),
		OperatorIdentity:      "release.operator@example.test",
		Reason:                migrateRecoverDirtyTestReason,
		WriterFenceIdentifier: "choice-tp-dev-fence-42",
		ExpectedDirtyVersion:  137,
		CatalogShape:          types.TimestampRecoveryCatalogShapePredecessor137,
		ForceVersion:          137,
		CatalogChecksum:       "sha256:" + strings.Repeat("a", 64),
	}
}

func migrateRecoverDirtyTestResult(
	plan types.TimestampDirtyRecoveryPlan,
) types.TimestampDirtyRecoveryResult {
	return types.TimestampDirtyRecoveryResult{
		FormatVersion:          types.TimestampDirtyRecoveryFormatVersion,
		RecordType:             types.TimestampDirtyRecoveryRecordTypeResult,
		RecoveryID:             plan.RecoveryID,
		CompletedAt:            time.Date(2026, 7, 17, 1, 3, 4, 5000, time.UTC),
		PlannedStatus:          types.TimestampDirtyRecoverySchemaStatus{Version: 137, Dirty: true},
		ObservedPreApplyStatus: types.TimestampDirtyRecoverySchemaStatus{Version: 137, Dirty: true},
		Action:                 types.TimestampDirtyRecoveryActionForced,
		ForcedVersion:          137,
		CatalogChecksum:        plan.CatalogChecksum,
		Result:                 types.TimestampDirtyRecoveryResultSucceeded,
		PostStatus:             types.TimestampDirtyRecoverySchemaStatus{Version: 137, Dirty: false},
	}
}

func migrateRecoverDirtyTestChecksum(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func migrateRecoverDirtyTestJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return append(data, '\n')
}

func TestMigrateRecoverDirtyPlanPublishesCanonicalCreateNewEvidence(t *testing.T) {
	g := NewWithT(t)
	plan := migrateRecoverDirtyTestPlan()
	runner := &migrateRecoverDirtyRunnerRecorder{plan: plan}
	var stdout bytes.Buffer
	var initialized, opened uint64
	output := filepath.Join(t.TempDir(), "dirty-recovery-plan.json")
	command := newMigrateRecoverDirtyCommand(migrateRecoverDirtyRuntime{
		Initialize: func() { initialized++ },
		OpenRunner: func() (migrateRecoverDirtyRunner, error) {
			opened++
			return runner, nil
		},
		Stdout: &stdout,
	})
	command.SetArgs([]string{
		"plan",
		"--expected-dirty-version", "137",
		"--operator-identity", "release.operator@example.test",
		"--reason", migrateRecoverDirtyTestReason,
		"--writer-fence-id", "choice-tp-dev-fence-42",
		"--output", output,
		"--lock-timeout", "275ms",
	})

	g.Expect(command.Execute()).To(Succeed())

	expected := migrateRecoverDirtyTestJSON(t, plan)
	actual, err := os.ReadFile(output)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(actual).To(Equal(expected))
	info, err := os.Stat(output)
	g.Expect(err).NotTo(HaveOccurred())
	if runtime.GOOS != "windows" {
		g.Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o600)))
	}
	g.Expect(stdout.String()).To(Equal(migrateRecoverDirtyTestChecksum(expected) + "\n"))
	g.Expect(initialized).To(Equal(uint64(1)))
	g.Expect(opened).To(Equal(uint64(1)))
	g.Expect(runner.planCalls).To(Equal(uint64(1)))
	g.Expect(runner.closeCalls).To(Equal(uint64(1)))
	g.Expect(runner.planOptions.ExpectedDirtyVersion).To(Equal(uint(137)))
	g.Expect(runner.planOptions.OperatorIdentity).To(Equal(
		"release.operator@example.test",
	))
	g.Expect(runner.planOptions.LockTimeout).To(Equal(275 * time.Millisecond))
	g.Expect(runner.planOptions.Manifest).To(BeNil())
	_, err = os.Lstat(migrateRecoveryEvidenceTempPath(output, plan.RecoveryID))
	g.Expect(errors.Is(err, os.ErrNotExist)).To(BeTrue())
}

func TestMigrateRecoverDirtyPlanRejectsReturnedFieldMismatchBeforePublication(
	t *testing.T,
) {
	for _, test := range []struct {
		name   string
		mutate func(*types.TimestampDirtyRecoveryPlan)
	}{
		{
			name: "expected dirty version",
			mutate: func(plan *types.TimestampDirtyRecoveryPlan) {
				plan.ExpectedDirtyVersion = 138
			},
		},
		{
			name: "operator identity",
			mutate: func(plan *types.TimestampDirtyRecoveryPlan) {
				plan.OperatorIdentity = "different.operator@example.test"
			},
		},
		{
			name: "reason",
			mutate: func(plan *types.TimestampDirtyRecoveryPlan) {
				plan.Reason = "Different reviewed recovery reason"
			},
		},
		{
			name: "writer fence",
			mutate: func(plan *types.TimestampDirtyRecoveryPlan) {
				plan.WriterFenceIdentifier = "different-fence-42"
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			plan := migrateRecoverDirtyTestPlan()
			test.mutate(&plan)
			runner := &migrateRecoverDirtyRunnerRecorder{plan: plan}
			var reserveCalls uint64
			output := filepath.Join(t.TempDir(), "plan.json")
			command := newMigrateRecoverDirtyCommand(migrateRecoverDirtyRuntime{
				Initialize: func() {},
				OpenRunner: func() (migrateRecoverDirtyRunner, error) {
					return runner, nil
				},
				ReserveEvidence: func(
					string,
					uuid.UUID,
				) (migrateRecoveryEvidenceReservation, error) {
					reserveCalls++
					return nil, errors.New("must not reserve")
				},
			})
			command.SetArgs([]string{
				"plan",
				"--expected-dirty-version", "137",
				"--operator-identity", "release.operator@example.test",
				"--reason", migrateRecoverDirtyTestReason,
				"--writer-fence-id", "choice-tp-dev-fence-42",
				"--output", output,
			})

			err := command.Execute()

			g.Expect(err).To(MatchError(
				"timestamp dirty recovery plan does not match the requested inputs",
			))
			g.Expect(runner.planCalls).To(Equal(uint64(1)))
			g.Expect(runner.closeCalls).To(Equal(uint64(1)))
			g.Expect(reserveCalls).To(BeZero())
			_, err = os.Lstat(output)
			g.Expect(errors.Is(err, os.ErrNotExist)).To(BeTrue())
		})
	}
}

func TestMigrateRecoverDirtyPlanRejectsReturnedManifestMismatchBeforePublication(
	t *testing.T,
) {
	g := NewWithT(t)
	directory := t.TempDir()
	manifest := externalExecutionTimestampCommandApprovedManifest(t)
	manifestData := migrateRecoverDirtyTestJSON(t, manifest)
	manifestPath := filepath.Join(directory, "manifest.json")
	g.Expect(os.WriteFile(manifestPath, manifestData, 0o600)).To(Succeed())
	binding, err := validateMigrateRecoveryManifest(
		manifest,
		migrateRecoverDirtyTestChecksum(manifestData),
	)
	g.Expect(err).NotTo(HaveOccurred())
	plan := migrateRecoverDirtyTestPlan()
	plan.Manifest = binding
	plan.Manifest.DocumentChecksum = "sha256:" + strings.Repeat("f", 64)
	runner := &migrateRecoverDirtyRunnerRecorder{plan: plan}
	var reserveCalls uint64
	output := filepath.Join(directory, "plan.json")
	command := newMigrateRecoverDirtyCommand(migrateRecoverDirtyRuntime{
		Initialize: func() {},
		OpenRunner: func() (migrateRecoverDirtyRunner, error) {
			return runner, nil
		},
		ReserveEvidence: func(
			string,
			uuid.UUID,
		) (migrateRecoveryEvidenceReservation, error) {
			reserveCalls++
			return nil, errors.New("must not reserve")
		},
	})
	command.SetArgs([]string{
		"plan",
		"--expected-dirty-version", "137",
		"--operator-identity", "release.operator@example.test",
		"--reason", migrateRecoverDirtyTestReason,
		"--writer-fence-id", "choice-tp-dev-fence-42",
		externalExecutionTimestampManifestFlag, manifestPath,
		"--output", output,
	})

	err = command.Execute()

	g.Expect(err).To(MatchError(
		"timestamp dirty recovery plan does not match the requested inputs",
	))
	g.Expect(runner.planCalls).To(Equal(uint64(1)))
	g.Expect(reserveCalls).To(BeZero())
	_, err = os.Lstat(output)
	g.Expect(errors.Is(err, os.ErrNotExist)).To(BeTrue())
}

func TestNewMigrateCommandRegistersRecoverDirtyPlanAndApply(t *testing.T) {
	g := NewWithT(t)
	command := NewMigrateCommand()

	plan, _, err := command.Find([]string{"recover-dirty", "plan"})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(plan.CommandPath()).To(HaveSuffix("migrate recover-dirty plan"))
	apply, _, err := command.Find([]string{"recover-dirty", "apply"})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(apply.CommandPath()).To(HaveSuffix("migrate recover-dirty apply"))
}

func TestMigrateRecoverDirtyRejectsNonPositiveLockTimeoutBeforeRuntime(
	t *testing.T,
) {
	for _, test := range []struct {
		name string
		args []string
	}{
		{
			name: "plan zero",
			args: []string{
				"plan",
				"--expected-dirty-version", "137",
				"--operator-identity", "release.operator@example.test",
				"--reason", migrateRecoverDirtyTestReason,
				"--writer-fence-id", "choice-tp-dev-fence-42",
				"--output", "plan.json",
				"--lock-timeout=0s",
			},
		},
		{
			name: "plan negative",
			args: []string{
				"plan",
				"--expected-dirty-version", "137",
				"--operator-identity", "release.operator@example.test",
				"--reason", migrateRecoverDirtyTestReason,
				"--writer-fence-id", "choice-tp-dev-fence-42",
				"--output", "plan.json",
				"--lock-timeout=-1ns",
			},
		},
		{
			name: "apply zero",
			args: []string{
				"apply",
				"--plan", "plan.json",
				"--plan-checksum", "sha256:" + strings.Repeat("a", 64),
				"--writer-fence-id", "choice-tp-dev-fence-42",
				"--output", "result.json",
				"--lock-timeout=0s",
			},
		},
		{
			name: "apply negative",
			args: []string{
				"apply",
				"--plan", "plan.json",
				"--plan-checksum", "sha256:" + strings.Repeat("a", 64),
				"--writer-fence-id", "choice-tp-dev-fence-42",
				"--output", "result.json",
				"--lock-timeout=-1ns",
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			var initialized, opened, read, reserved uint64
			command := newMigrateRecoverDirtyCommand(migrateRecoverDirtyRuntime{
				Initialize: func() { initialized++ },
				OpenRunner: func() (migrateRecoverDirtyRunner, error) {
					opened++
					return &migrateRecoverDirtyRunnerRecorder{}, nil
				},
				ReadFile: func(string) ([]byte, error) {
					read++
					return nil, errors.New("must not read")
				},
				ReserveEvidence: func(
					string,
					uuid.UUID,
				) (migrateRecoveryEvidenceReservation, error) {
					reserved++
					return nil, errors.New("must not reserve")
				},
			})
			command.SetArgs(test.args)

			err := command.Execute()

			g.Expect(err).To(MatchError("--lock-timeout must be positive"))
			g.Expect(initialized).To(BeZero())
			g.Expect(opened).To(BeZero())
			g.Expect(read).To(BeZero())
			g.Expect(reserved).To(BeZero())
		})
	}
}

func TestMigrateRecoverDirtyRejectsDashInputPathsBeforeRead(t *testing.T) {
	t.Run("plan manifest", func(t *testing.T) {
		g := NewWithT(t)
		var readCalls uint64
		command := newMigrateRecoverDirtyCommand(migrateRecoverDirtyRuntime{
			ReadFile: func(string) ([]byte, error) {
				readCalls++
				return nil, errors.New("must not read")
			},
		})
		command.SetArgs([]string{
			"plan",
			"--expected-dirty-version", "137",
			"--operator-identity", "release.operator@example.test",
			"--reason", migrateRecoverDirtyTestReason,
			"--writer-fence-id", "choice-tp-dev-fence-42",
			externalExecutionTimestampManifestFlag, "-",
			"--output", "plan.json",
		})

		err := command.Execute()

		g.Expect(err).To(MatchError(
			"--external-execution-timestamp-manifest must be a file path",
		))
		g.Expect(readCalls).To(BeZero())
	})

	t.Run("apply plan", func(t *testing.T) {
		g := NewWithT(t)
		var readCalls uint64
		command := newMigrateRecoverDirtyCommand(migrateRecoverDirtyRuntime{
			ReadFile: func(string) ([]byte, error) {
				readCalls++
				return nil, errors.New("must not read")
			},
		})
		command.SetArgs([]string{
			"apply",
			"--plan", "-",
			"--plan-checksum", "sha256:" + strings.Repeat("a", 64),
			"--writer-fence-id", "choice-tp-dev-fence-42",
			"--output", "result.json",
		})

		err := command.Execute()

		g.Expect(err).To(MatchError("--plan must be a file path"))
		g.Expect(readCalls).To(BeZero())
	})

	t.Run("apply manifest", func(t *testing.T) {
		g := NewWithT(t)
		manifest := externalExecutionTimestampCommandApprovedManifest(t)
		manifestData := migrateRecoverDirtyTestJSON(t, manifest)
		binding, err := validateMigrateRecoveryManifest(
			manifest,
			migrateRecoverDirtyTestChecksum(manifestData),
		)
		g.Expect(err).NotTo(HaveOccurred())
		plan := migrateRecoverDirtyTestPlan()
		plan.Manifest = binding
		planData := migrateRecoverDirtyTestJSON(t, plan)
		var readPaths []string
		command := newMigrateRecoverDirtyCommand(migrateRecoverDirtyRuntime{
			ReadFile: func(path string) ([]byte, error) {
				readPaths = append(readPaths, path)
				if path == "plan.json" {
					return planData, nil
				}
				return nil, errors.New("must not read manifest dash path")
			},
		})
		command.SetArgs([]string{
			"apply",
			"--plan", "plan.json",
			"--plan-checksum", migrateRecoverDirtyTestChecksum(planData),
			"--writer-fence-id", plan.WriterFenceIdentifier,
			externalExecutionTimestampManifestFlag, "-",
			"--output", "result.json",
		})

		err = command.Execute()

		g.Expect(err).To(MatchError(
			"--external-execution-timestamp-manifest must be a file path",
		))
		g.Expect(readPaths).To(Equal([]string{"plan.json"}))
	})
}

func TestMigrateRecoverDirtyApplyVerifiesRawPlanChecksumAndConditionalManifest(
	t *testing.T,
) {
	g := NewWithT(t)
	directory := t.TempDir()
	plan := migrateRecoverDirtyTestPlan()
	manifest := externalExecutionTimestampCommandApprovedManifest(t)
	manifestData := migrateRecoverDirtyTestJSON(t, manifest)
	manifestPath := filepath.Join(directory, "manifest.json")
	g.Expect(os.WriteFile(manifestPath, manifestData, 0o600)).To(Succeed())
	plan.Manifest = &types.TimestampDirtyRecoveryManifestBinding{
		ID:                       manifest.ID,
		DocumentChecksum:         migrateRecoverDirtyTestChecksum(manifestData),
		DecisionContentChecksum:  manifest.DecisionContentChecksum,
		RawSetChecksum:           manifest.RawCellChecksum,
		DatabaseIdentityChecksum: manifest.DatabaseIdentityChecksum,
		ExecutionCount:           manifest.ExecutionCount,
		EventCount:               manifest.EventCount,
		RawCellCount:             manifest.RawCellCount,
	}
	planData := migrateRecoverDirtyTestJSON(t, plan)
	planPath := filepath.Join(directory, "plan.json")
	g.Expect(os.WriteFile(planPath, planData, 0o600)).To(Succeed())
	output := filepath.Join(directory, "result.json")
	result := migrateRecoverDirtyTestResult(plan)
	result.PlanChecksum = migrateRecoverDirtyTestChecksum(planData)
	runner := &migrateRecoverDirtyRunnerRecorder{result: result}
	var stdout bytes.Buffer
	command := newMigrateRecoverDirtyCommand(migrateRecoverDirtyRuntime{
		Initialize: func() {},
		OpenRunner: func() (migrateRecoverDirtyRunner, error) {
			return runner, nil
		},
		Stdout: &stdout,
	})
	command.SetArgs([]string{
		"apply",
		"--plan", planPath,
		"--plan-checksum", migrateRecoverDirtyTestChecksum(planData),
		"--writer-fence-id", plan.WriterFenceIdentifier,
		externalExecutionTimestampManifestFlag, manifestPath,
		"--output", output,
		"--lock-timeout", "350ms",
	})

	g.Expect(command.Execute()).To(Succeed())

	expected := migrateRecoverDirtyTestJSON(t, result)
	actual, err := os.ReadFile(output)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(actual).To(Equal(expected))
	g.Expect(stdout.String()).To(Equal(migrateRecoverDirtyTestChecksum(expected) + "\n"))
	g.Expect(runner.applyCalls).To(Equal(uint64(1)))
	g.Expect(runner.applyPlan).To(Equal(plan))
	g.Expect(runner.applyOptions.PlanDocumentChecksum).To(Equal(
		migrateRecoverDirtyTestChecksum(planData),
	))
	g.Expect(runner.applyOptions.WriterFenceIdentifier).To(Equal(
		plan.WriterFenceIdentifier,
	))
	g.Expect(runner.applyOptions.Manifest).NotTo(BeNil())
	g.Expect(runner.applyOptions.ManifestDocumentChecksum).To(Equal(
		migrateRecoverDirtyTestChecksum(manifestData),
	))
	g.Expect(runner.applyOptions.LockTimeout).To(Equal(350 * time.Millisecond))
}

func TestMigrateRecoverDirtyRejectsUnknownAndTrailingJSONBeforeRunner(
	t *testing.T,
) {
	for _, test := range []struct {
		name string
		data []byte
	}{
		{name: "unknown field", data: []byte(`{"unknown":true}`)},
		{name: "trailing document", data: []byte(`{} {}`)},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			directory := t.TempDir()
			planPath := filepath.Join(directory, "plan.json")
			g.Expect(os.WriteFile(planPath, test.data, 0o600)).To(Succeed())
			var initialized, opened uint64
			command := newMigrateRecoverDirtyCommand(migrateRecoverDirtyRuntime{
				Initialize: func() { initialized++ },
				OpenRunner: func() (migrateRecoverDirtyRunner, error) {
					opened++
					return &migrateRecoverDirtyRunnerRecorder{}, nil
				},
			})
			command.SetArgs([]string{
				"apply",
				"--plan", planPath,
				"--plan-checksum", migrateRecoverDirtyTestChecksum(test.data),
				"--writer-fence-id", "choice-tp-dev-fence-42",
				"--output", filepath.Join(directory, "result.json"),
			})

			err := command.Execute()

			g.Expect(err).To(HaveOccurred())
			g.Expect(initialized).To(BeZero())
			g.Expect(opened).To(BeZero())
		})
	}
}

func TestMigrateRecoverDirtyApplyOutputOrTempCollisionStopsBeforeInitializeAndRunner(
	t *testing.T,
) {
	for _, test := range []struct {
		name       string
		createPath func(string, types.TimestampDirtyRecoveryPlan) string
	}{
		{
			name: "final output",
			createPath: func(output string, _ types.TimestampDirtyRecoveryPlan) string {
				return output
			},
		},
		{
			name: "reserved temp",
			createPath: func(output string, plan types.TimestampDirtyRecoveryPlan) string {
				return migrateRecoveryEvidenceTempPath(output, plan.RecoveryID)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			directory := t.TempDir()
			plan := migrateRecoverDirtyTestPlan()
			planData := migrateRecoverDirtyTestJSON(t, plan)
			planPath := filepath.Join(directory, "plan.json")
			output := filepath.Join(directory, "result.json")
			g.Expect(os.WriteFile(planPath, planData, 0o600)).To(Succeed())
			collision := test.createPath(output, plan)
			g.Expect(os.WriteFile(collision, []byte("SENTINEL"), 0o600)).To(Succeed())
			var initialized, opened uint64
			runner := &migrateRecoverDirtyRunnerRecorder{
				result: migrateRecoverDirtyTestResult(plan),
			}
			command := newMigrateRecoverDirtyCommand(migrateRecoverDirtyRuntime{
				Initialize: func() { initialized++ },
				OpenRunner: func() (migrateRecoverDirtyRunner, error) {
					opened++
					return runner, nil
				},
			})
			command.SetArgs([]string{
				"apply",
				"--plan", planPath,
				"--plan-checksum", migrateRecoverDirtyTestChecksum(planData),
				"--writer-fence-id", plan.WriterFenceIdentifier,
				"--output", output,
			})

			err := command.Execute()

			g.Expect(errors.Is(err, os.ErrExist)).To(BeTrue())
			g.Expect(initialized).To(BeZero())
			g.Expect(opened).To(BeZero())
			g.Expect(runner.applyCalls).To(BeZero())
			contents, readErr := os.ReadFile(collision)
			g.Expect(readErr).NotTo(HaveOccurred())
			g.Expect(contents).To(Equal([]byte("SENTINEL")))
		})
	}
}

func TestMigrateRecoverDirtyApplyChecksumOrManifestMismatchStopsBeforeReservation(
	t *testing.T,
) {
	g := NewWithT(t)
	directory := t.TempDir()
	plan := migrateRecoverDirtyTestPlan()
	planData := migrateRecoverDirtyTestJSON(t, plan)
	planPath := filepath.Join(directory, "plan.json")
	g.Expect(os.WriteFile(planPath, planData, 0o600)).To(Succeed())
	var reserveCalls uint64
	command := newMigrateRecoverDirtyCommand(migrateRecoverDirtyRuntime{
		ReserveEvidence: func(
			string,
			uuid.UUID,
		) (migrateRecoveryEvidenceReservation, error) {
			reserveCalls++
			return nil, errors.New("must not reserve")
		},
	})
	command.SetArgs([]string{
		"apply",
		"--plan", planPath,
		"--plan-checksum", "sha256" + strings.Repeat("0", 64),
		"--writer-fence-id", plan.WriterFenceIdentifier,
		externalExecutionTimestampManifestFlag,
		filepath.Join(directory, "unexpected.json"),
		"--output", filepath.Join(directory, "result.json"),
	})

	err := command.Execute()

	g.Expect(err).To(HaveOccurred())
	g.Expect(reserveCalls).To(BeZero())
}

func TestMigrateRecoverDirtyApplyRejectsResultNotBoundToPlanAndLeavesReservation(
	t *testing.T,
) {
	g := NewWithT(t)
	directory := t.TempDir()
	plan := migrateRecoverDirtyTestPlan()
	planData := migrateRecoverDirtyTestJSON(t, plan)
	planPath := filepath.Join(directory, "plan.json")
	output := filepath.Join(directory, "result.json")
	g.Expect(os.WriteFile(planPath, planData, 0o600)).To(Succeed())
	result := migrateRecoverDirtyTestResult(plan)
	result.RecoveryID = uuid.MustParse("99999999-2222-4333-8444-555555555555")
	runner := &migrateRecoverDirtyRunnerRecorder{result: result}
	command := newMigrateRecoverDirtyCommand(migrateRecoverDirtyRuntime{
		Initialize: func() {},
		OpenRunner: func() (migrateRecoverDirtyRunner, error) {
			return runner, nil
		},
	})
	command.SetArgs([]string{
		"apply",
		"--plan", planPath,
		"--plan-checksum", migrateRecoverDirtyTestChecksum(planData),
		"--writer-fence-id", plan.WriterFenceIdentifier,
		"--output", output,
	})

	err := command.Execute()

	g.Expect(err).To(MatchError(ContainSubstring(
		"result does not match the recovery plan",
	)))
	g.Expect(runner.applyCalls).To(Equal(uint64(1)))
	_, err = os.Lstat(output)
	g.Expect(errors.Is(err, os.ErrNotExist)).To(BeTrue())
	tempInfo, err := os.Stat(migrateRecoveryEvidenceTempPath(
		output,
		plan.RecoveryID,
	))
	g.Expect(err).NotTo(HaveOccurred())
	if runtime.GOOS != "windows" {
		g.Expect(tempInfo.Mode().Perm()).To(Equal(os.FileMode(0o600)))
	}
}

func TestMigrateRecoverDirtyApplyOpenRunnerFailureCancelsReservation(t *testing.T) {
	g := NewWithT(t)
	directory := t.TempDir()
	plan := migrateRecoverDirtyTestPlan()
	planData := migrateRecoverDirtyTestJSON(t, plan)
	planPath := filepath.Join(directory, "plan.json")
	output := filepath.Join(directory, "result.json")
	g.Expect(os.WriteFile(planPath, planData, 0o600)).To(Succeed())
	var initialized, opened uint64
	command := newMigrateRecoverDirtyCommand(migrateRecoverDirtyRuntime{
		Initialize: func() { initialized++ },
		OpenRunner: func() (migrateRecoverDirtyRunner, error) {
			opened++
			return nil, errors.New("open runner failed")
		},
	})
	command.SetArgs([]string{
		"apply",
		"--plan", planPath,
		"--plan-checksum", migrateRecoverDirtyTestChecksum(planData),
		"--writer-fence-id", plan.WriterFenceIdentifier,
		"--output", output,
	})

	err := command.Execute()

	g.Expect(err).To(MatchError(ContainSubstring("open runner failed")))
	g.Expect(initialized).To(Equal(uint64(1)))
	g.Expect(opened).To(Equal(uint64(1)))
	_, err = os.Lstat(output)
	g.Expect(errors.Is(err, os.ErrNotExist)).To(BeTrue())
	_, err = os.Lstat(migrateRecoveryEvidenceTempPath(output, plan.RecoveryID))
	g.Expect(errors.Is(err, os.ErrNotExist)).To(BeTrue())
}

func TestMigrateRecoverDirtyApplyErrorLeavesReservationAndNoFinal(t *testing.T) {
	g := NewWithT(t)
	directory := t.TempDir()
	plan := migrateRecoverDirtyTestPlan()
	planData := migrateRecoverDirtyTestJSON(t, plan)
	planPath := filepath.Join(directory, "plan.json")
	output := filepath.Join(directory, "result.json")
	g.Expect(os.WriteFile(planPath, planData, 0o600)).To(Succeed())
	runner := &migrateRecoverDirtyRunnerRecorder{
		applyErr: errors.New("apply failed"),
	}
	var stdout bytes.Buffer
	command := newMigrateRecoverDirtyCommand(migrateRecoverDirtyRuntime{
		Initialize: func() {},
		OpenRunner: func() (migrateRecoverDirtyRunner, error) {
			return runner, nil
		},
		Stdout: &stdout,
	})
	command.SetArgs([]string{
		"apply",
		"--plan", planPath,
		"--plan-checksum", migrateRecoverDirtyTestChecksum(planData),
		"--writer-fence-id", plan.WriterFenceIdentifier,
		"--output", output,
	})

	err := command.Execute()

	g.Expect(err).To(MatchError(ContainSubstring("apply failed")))
	g.Expect(runner.applyCalls).To(Equal(uint64(1)))
	g.Expect(runner.closeCalls).To(Equal(uint64(1)))
	g.Expect(stdout.String()).To(BeEmpty())
	_, err = os.Lstat(output)
	g.Expect(errors.Is(err, os.ErrNotExist)).To(BeTrue())
	tempInfo, err := os.Stat(migrateRecoveryEvidenceTempPath(
		output,
		plan.RecoveryID,
	))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(tempInfo.Size()).To(BeZero())
}

func TestMigrateRecoverDirtyApplyRejectsValidNoncanonicalPlanBeforeReservation(
	t *testing.T,
) {
	g := NewWithT(t)
	directory := t.TempDir()
	plan := migrateRecoverDirtyTestPlan()
	planData, err := json.Marshal(plan)
	g.Expect(err).NotTo(HaveOccurred())
	planPath := filepath.Join(directory, "plan.json")
	g.Expect(os.WriteFile(planPath, planData, 0o600)).To(Succeed())
	var initialized, opened, reserved uint64
	command := newMigrateRecoverDirtyCommand(migrateRecoverDirtyRuntime{
		Initialize: func() { initialized++ },
		OpenRunner: func() (migrateRecoverDirtyRunner, error) {
			opened++
			return &migrateRecoverDirtyRunnerRecorder{}, nil
		},
		ReserveEvidence: func(
			string,
			uuid.UUID,
		) (migrateRecoveryEvidenceReservation, error) {
			reserved++
			return nil, errors.New("must not reserve")
		},
	})
	command.SetArgs([]string{
		"apply",
		"--plan", planPath,
		"--plan-checksum", migrateRecoverDirtyTestChecksum(planData),
		"--writer-fence-id", plan.WriterFenceIdentifier,
		"--output", filepath.Join(directory, "result.json"),
	})

	err = command.Execute()

	g.Expect(err).To(MatchError(ContainSubstring(
		"must use canonical two-space JSON",
	)))
	g.Expect(initialized).To(BeZero())
	g.Expect(opened).To(BeZero())
	g.Expect(reserved).To(BeZero())
}
