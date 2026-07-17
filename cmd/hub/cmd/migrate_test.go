package cmd

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/migrations"
	. "github.com/onsi/gomega"
)

const externalExecutionTimestampManifestFlag = "--external-execution-timestamp-manifest"

type migrateRuntimeRecorder struct {
	runCalls    uint64
	initialized uint64
	options     migrations.RunOptions
	manifest    []byte
	readErr     error
}

func (recorder *migrateRuntimeRecorder) runtime() migrateRuntime {
	return migrateRuntime{
		Initialize: func() { recorder.initialized++ },
		ReadFile: func(string) ([]byte, error) {
			return recorder.manifest, recorder.readErr
		},
		Run: func(
			_ context.Context,
			options migrations.RunOptions,
		) error {
			recorder.runCalls++
			recorder.options = options
			return nil
		},
	}
}

func TestMigrateExplicitTargetZero(t *testing.T) {
	g := NewWithT(t)
	recorder := &migrateRuntimeRecorder{}
	command := newMigrateCommand(recorder.runtime())
	command.SetArgs([]string{"--to", "0"})
	g.Expect(command.Execute()).To(Succeed())
	g.Expect(recorder.initialized).To(Equal(uint64(1)))
	g.Expect(recorder.runCalls).To(Equal(uint64(1)))
	g.Expect(recorder.options.Target).NotTo(BeNil())
	g.Expect(*recorder.options.Target).To(Equal(uint(0)))
}

func TestMigrateRejectsInvalidFlagCombinationsBeforeRuntime(t *testing.T) {
	tests := [][]string{
		{"--down", "--to", "137"},
		{"--down", "--check"},
		{externalExecutionTimestampManifestFlag, "approved.json"},
		{
			"--to", "139",
			externalExecutionTimestampManifestFlag, "approved.json",
		},
		{"--lock-timeout=-1ns"},
	}
	for _, args := range tests {
		t.Run(args[0], func(t *testing.T) {
			g := NewWithT(t)
			recorder := &migrateRuntimeRecorder{manifest: []byte("{}")}
			command := newMigrateCommand(recorder.runtime())
			command.SetArgs(args)
			g.Expect(command.Execute()).NotTo(Succeed())
			g.Expect(recorder.initialized).To(Equal(uint64(0)))
			g.Expect(recorder.runCalls).To(Equal(uint64(0)))
		})
	}
}

func TestMigrateLockTimeoutContract(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want time.Duration
	}{
		{name: "omitted", want: 10 * time.Second},
		{name: "positive", args: []string{"--lock-timeout=275ms"}, want: 275 * time.Millisecond},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			recorder := &migrateRuntimeRecorder{}
			command := newMigrateCommand(recorder.runtime())
			command.SetArgs(test.args)
			g.Expect(command.Execute()).To(Succeed())
			g.Expect(recorder.initialized).To(Equal(uint64(1)))
			g.Expect(recorder.runCalls).To(Equal(uint64(1)))
			g.Expect(recorder.options.LockTimeout).To(Equal(test.want))
		})
	}
}

func TestMigrateCheckOnlyReachesRunner(t *testing.T) {
	g := NewWithT(t)
	recorder := &migrateRuntimeRecorder{}
	command := newMigrateCommand(recorder.runtime())
	command.SetArgs([]string{"--check", "--to", "138"})
	g.Expect(command.Execute()).To(Succeed())
	g.Expect(recorder.initialized).To(Equal(uint64(1)))
	g.Expect(recorder.runCalls).To(Equal(uint64(1)))
	g.Expect(recorder.options.CheckOnly).To(BeTrue())
	g.Expect(recorder.options.Target).NotTo(BeNil())
	g.Expect(*recorder.options.Target).To(Equal(uint(138)))
}

func TestMigrateManifestUsesStrictSingleDocumentJSON(t *testing.T) {
	tests := []struct {
		name      string
		manifest  string
		readErr   error
		wantError string
	}{
		{name: "single document", manifest: `{}`},
		{name: "read error", readErr: errors.New("disk failed"), wantError: "read timestamp manifest failed"},
		{name: "unknown field", manifest: `{"unknown":true}`, wantError: "invalid timestamp manifest JSON"},
		{name: "trailing document", manifest: `{} {}`, wantError: "exactly one document"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			recorder := &migrateRuntimeRecorder{
				manifest: []byte(test.manifest),
				readErr:  test.readErr,
			}
			command := newMigrateCommand(recorder.runtime())
			command.SetArgs([]string{
				"--to", "138",
				"--external-execution-timestamp-manifest", "approved.json",
			})
			err := command.Execute()
			if test.wantError == "" {
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(recorder.initialized).To(Equal(uint64(1)))
				g.Expect(recorder.runCalls).To(Equal(uint64(1)))
				g.Expect(recorder.options.ExpandManifest).NotTo(BeNil())
			} else {
				g.Expect(err).To(MatchError(ContainSubstring(test.wantError)))
				g.Expect(recorder.initialized).To(Equal(uint64(0)))
				g.Expect(recorder.runCalls).To(Equal(uint64(0)))
			}
		})
	}
}

func TestRunMigratePropagatesRuntimeError(t *testing.T) {
	g := NewWithT(t)
	want := errors.New("runner failed")
	var initialized uint64
	runtime := migrateRuntime{
		Initialize: func() { initialized++ },
		ReadFile:   func(string) ([]byte, error) { return nil, nil },
		Run: func(context.Context, migrations.RunOptions) error {
			return want
		},
	}
	err := runMigrate(context.Background(), MigrateOptions{}, runtime)
	g.Expect(err).To(MatchError(want))
	g.Expect(initialized).To(Equal(uint64(1)))
}
