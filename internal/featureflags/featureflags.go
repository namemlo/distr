package featureflags

import (
	"fmt"
	"slices"
	"strings"
)

type Key string

const (
	KeyEnvironments         Key = "environments"
	KeyLifecycles           Key = "lifecycles"
	KeyChannels             Key = "channels"
	KeyReleaseBundles       Key = "release_bundles"
	KeyDeploymentProcesses  Key = "deployment_processes"
	KeyScopedVariablesV2    Key = "scoped_variables_v2"
	KeyDeploymentPlans      Key = "deployment_plans"
	KeyTaskQueue            Key = "task_queue"
	KeyAgentCapabilities    Key = "agent_capabilities"
	KeyAgentTaskLeases      Key = "agent_task_leases"
	KeyStepEvents           Key = "step_events"
	KeyStepTemplates        Key = "step_templates"
	KeyRunbooks             Key = "runbooks"
	KeyDeploymentTimeline   Key = "deployment_timeline"
	KeyRetentionPolicies    Key = "retention_policies"
	KeyObservabilityMetrics Key = "observability_metrics"
	KeyObservabilityTracing Key = "observability_tracing"
)

type Flag struct {
	Key         Key
	Label       string
	Description string
	Milestone   string
	Enabled     bool
}

type definition struct {
	Key         Key
	Label       string
	Description string
	Milestone   string
}

type Registry struct {
	enabled []Key
}

var definitions = []definition{
	{
		Key:         KeyEnvironments,
		Label:       "Environments",
		Description: "Groups deployment targets by promotion stage or operational purpose.",
		Milestone:   "Milestone B",
	},
	{
		Key:         KeyLifecycles,
		Label:       "Lifecycles",
		Description: "Defines ordered phases, promotion requirements, automatic deployment rules, and retention behavior.",
		Milestone:   "Milestone B",
	},
	{
		Key:         KeyChannels,
		Label:       "Channels",
		Description: "Selects lifecycle, version rules, source restrictions, process conditions, and eligible tenant tags.",
		Milestone:   "Milestone B",
	},
	{
		Key:         KeyReleaseBundles,
		Label:       "Release Bundles",
		Description: "Creates immutable deployable snapshots coordinating one or more application versions and artifacts.",
		Milestone:   "Milestone B",
	},
	{
		Key:         KeyDeploymentProcesses,
		Label:       "Deployment Processes",
		Description: "Models reusable ordered or grouped sets of typed deployment steps.",
		Milestone:   "Milestone C",
	},
	{
		Key:         KeyScopedVariablesV2,
		Label:       "Scoped Variables",
		Description: "Models reusable variable sets, typed values, and safe secret references.",
		Milestone:   "Milestone C",
	},
	{
		Key:         KeyDeploymentPlans,
		Label:       "Deployment Plans",
		Description: "Resolves an immutable preview of what a deployment will do before execution.",
		Milestone:   "Milestone D",
	},
	{
		Key:         KeyTaskQueue,
		Label:       "Task Queue",
		Description: "Stores durable deployment tasks and step runs with deterministic queue ordering.",
		Milestone:   "Milestone D",
	},
	{
		Key:         KeyAgentCapabilities,
		Label:       "Agent Capabilities",
		Description: "Lets agents advertise protocol, runtime, and action support for plan compatibility checks.",
		Milestone:   "Milestone D",
	},
	{
		Key:         KeyAgentTaskLeases,
		Label:       "Agent Task Leases",
		Description: "Lets agents claim durable task leases and heartbeat active work.",
		Milestone:   "Milestone D",
	},
	{
		Key:         KeyStepEvents,
		Label:       "Step Events",
		Description: "Stores structured step lifecycle events, redacted log chunks, and bounded step outputs.",
		Milestone:   "Milestone D",
	},
	{
		Key:         KeyStepTemplates,
		Label:       "Step Templates",
		Description: "Installs reusable step definitions with versioned schemas and compatibility metadata.",
		Milestone:   "Milestone E",
	},
	{
		Key:         KeyRunbooks,
		Label:       "Runbooks",
		Description: "Provides versioned operational processes that are not tied to release promotion.",
		Milestone:   "Milestone F",
	},
	{
		Key:         KeyDeploymentTimeline,
		Label:       "Deployment Timeline",
		Description: "Displays deployment history, compares releases and plan snapshots, and previews deploying previous releases.",
		Milestone:   "Milestone F",
	},
	{
		Key:         KeyRetentionPolicies,
		Label:       "Retention Policies",
		Description: "Previews release, task, and log cleanup with safety blocks before retention jobs can run.",
		Milestone:   "Milestone G",
	},
	{
		Key:         KeyObservabilityMetrics,
		Label:       "Observability Metrics",
		Description: "Enables Prometheus metrics for HTTP traffic and task execution instrumentation.",
		Milestone:   "Milestone G",
	},
	{
		Key:         KeyObservabilityTracing,
		Label:       "Observability Tracing",
		Description: "Enables OpenTelemetry tracing for HTTP traffic and task execution instrumentation.",
		Milestone:   "Milestone G",
	},
}

func AllKeys() []Key {
	keys := make([]Key, 0, len(definitions))
	for _, def := range definitions {
		keys = append(keys, def.Key)
	}
	return keys
}

func ParseEnabledKeys(value string) ([]Key, error) {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\t' || r == ' '
	})

	enabled := map[Key]struct{}{}
	for _, field := range fields {
		key := strings.TrimSpace(field)
		if key == "" {
			continue
		}
		if key == "all" {
			return AllKeys(), nil
		}
		parsed := Key(key)
		if !isKnown(parsed) {
			return nil, fmt.Errorf("unknown experimental feature flag %q", key)
		}
		enabled[parsed] = struct{}{}
	}

	keys := make([]Key, 0, len(enabled))
	for _, key := range AllKeys() {
		if _, ok := enabled[key]; ok {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

func NewRegistry(enabled []Key) Registry {
	return Registry{enabled: slices.Clone(enabled)}
}

func (r Registry) IsEnabled(key Key) bool {
	return slices.Contains(r.enabled, key)
}

func (r Registry) Flags() []Flag {
	flags := make([]Flag, 0, len(definitions))
	for _, def := range definitions {
		flags = append(flags, Flag{
			Key:         def.Key,
			Label:       def.Label,
			Description: def.Description,
			Milestone:   def.Milestone,
			Enabled:     r.IsEnabled(def.Key),
		})
	}
	return flags
}

func isKnown(key Key) bool {
	return slices.Contains(AllKeys(), key)
}
