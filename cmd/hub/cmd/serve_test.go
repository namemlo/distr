package cmd

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
)

func TestServeDatabaseStartupOrdersReadinessBeforeDependentWrites(t *testing.T) {
	calls := make([]string, 0, 3)
	err := runServeDatabaseStartup(
		context.Background(),
		serveDatabaseStartupHooks{
			requireTimestampReadiness: func(context.Context) error {
				calls = append(calls, "readiness")
				return nil
			},
			createAgentVersion: func(context.Context) error {
				calls = append(calls, "agent-version")
				return nil
			},
			reconcileEditionFeatures: func(context.Context) error {
				calls = append(calls, "subscription")
				return nil
			},
		},
	)
	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(calls).To(Equal([]string{
		"readiness", "agent-version", "subscription",
	}))
}

func TestServeDatabaseStartupStopsAtEachFailedHook(t *testing.T) {
	tests := []struct {
		name      string
		failAt    string
		wantCalls []string
		wantError string
	}{
		{
			name:   "readiness",
			failAt: "readiness", wantCalls: []string{"readiness"},
			wantError: "external-execution timestamp readiness",
		},
		{
			name:      "agent version",
			failAt:    "agent-version",
			wantCalls: []string{"readiness", "agent-version"},
			wantError: "create agent version",
		},
		{
			name:      "subscription",
			failAt:    "subscription",
			wantCalls: []string{"readiness", "agent-version", "subscription"},
			wantError: "reconcile edition features",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := make([]string, 0, 3)
			hook := func(name string) func(context.Context) error {
				return func(context.Context) error {
					calls = append(calls, name)
					if name == test.failAt {
						return errors.New("blocked")
					}
					return nil
				}
			}
			err := runServeDatabaseStartup(
				context.Background(),
				serveDatabaseStartupHooks{
					requireTimestampReadiness: hook("readiness"),
					createAgentVersion:        hook("agent-version"),
					reconcileEditionFeatures:  hook("subscription"),
				},
			)
			g := NewWithT(t)
			g.Expect(err).To(MatchError(ContainSubstring(test.wantError)))
			g.Expect(calls).To(Equal(test.wantCalls))
		})
	}
}

func TestServeRefusesTimestampSchemaBeforeDependentInitialization(t *testing.T) {
	data, err := os.ReadFile("serve.go")
	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	source := string(data)
	logContext := strings.Index(source, "dbLogCtx := internalctx.WithLogger")
	startup := strings.Index(source, "runServeDatabaseStartup(dbLogCtx")
	g.Expect(logContext).To(BeNumerically(">=", 0))
	g.Expect(startup).To(BeNumerically(">", logContext))
	for _, dependent := range []string{
		"if env.MetricsEnabled()",
		"server := registry.GetServer()",
		"registry.GetHubExecutor().Start(sigCtx)",
		"registry.GetJobsScheduler().Start()",
	} {
		index := strings.Index(source, dependent)
		g.Expect(index).To(BeNumerically(">", startup), dependent)
	}
}
