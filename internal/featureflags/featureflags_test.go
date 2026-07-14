package featureflags

import (
	"testing"

	. "github.com/onsi/gomega"
)

func TestParseEnabledKeys(t *testing.T) {
	g := NewWithT(t)

	keys, err := ParseEnabledKeys(
		"release_bundles, environments\nlifecycles agent_capabilities agent_task_leases step_events observability_metrics observability_tracing observability_dashboards observability_correlation config_as_code release_bundles",
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(keys).To(Equal([]Key{
		KeyEnvironments,
		KeyLifecycles,
		KeyReleaseBundles,
		KeyAgentCapabilities,
		KeyAgentTaskLeases,
		KeyStepEvents,
		KeyObservabilityMetrics,
		KeyObservabilityTracing,
		KeyObservabilityDashboards,
		KeyObservabilityCorrelation,
		KeyConfigAsCode,
	}))
}

func TestParseEnabledKeysAll(t *testing.T) {
	g := NewWithT(t)

	keys, err := ParseEnabledKeys("all")

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(keys).To(Equal(AllKeys()))
}

func TestParseEnabledKeysRejectsUnknownFlags(t *testing.T) {
	g := NewWithT(t)

	_, err := ParseEnabledKeys("environments,not_a_flag")

	g.Expect(err).To(MatchError(ContainSubstring(`unknown experimental feature flag "not_a_flag"`)))
}

func TestParseEnabledKeysControlPlane(t *testing.T) {
	g := NewWithT(t)

	keys, err := ParseEnabledKeys("executor_protocol_v2 operator_control_plane_v2")

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(keys).To(Equal([]Key{
		KeyOperatorControlPlaneV2,
		KeyExecutorProtocolV2,
	}))
}

func TestRegistryFlagsRequireControlPlaneForExecutorProtocol(t *testing.T) {
	g := NewWithT(t)

	disabled := NewRegistry(nil)
	disabledFlags := disabled.Flags()

	g.Expect(disabled.IsEnabled(KeyOperatorControlPlaneV2)).To(BeFalse())
	g.Expect(disabled.IsEnabled(KeyExecutorProtocolV2)).To(BeFalse())
	g.Expect(findFlag(disabledFlags, KeyOperatorControlPlaneV2).Enabled).To(BeFalse())
	g.Expect(findFlag(disabledFlags, KeyExecutorProtocolV2).Enabled).To(BeFalse())

	executorOnly := NewRegistry([]Key{KeyExecutorProtocolV2})
	executorOnlyFlags := executorOnly.Flags()
	executorOnlyOperator := findFlag(executorOnlyFlags, KeyOperatorControlPlaneV2)
	executorOnlyProtocol := findFlag(executorOnlyFlags, KeyExecutorProtocolV2)

	g.Expect(executorOnly.IsEnabled(KeyOperatorControlPlaneV2)).To(BeFalse())
	g.Expect(executorOnly.IsEnabled(KeyExecutorProtocolV2)).To(BeFalse())
	g.Expect(executorOnlyOperator.Enabled).To(BeFalse())
	g.Expect(executorOnlyProtocol.Enabled).To(BeFalse())

	operatorOnly := NewRegistry([]Key{KeyOperatorControlPlaneV2})
	operatorOnlyFlags := operatorOnly.Flags()

	g.Expect(operatorOnly.IsEnabled(KeyOperatorControlPlaneV2)).To(BeTrue())
	g.Expect(operatorOnly.IsEnabled(KeyExecutorProtocolV2)).To(BeFalse())
	g.Expect(findFlag(operatorOnlyFlags, KeyOperatorControlPlaneV2).Enabled).To(BeTrue())
	g.Expect(findFlag(operatorOnlyFlags, KeyExecutorProtocolV2).Enabled).To(BeFalse())

	registry := NewRegistry([]Key{KeyOperatorControlPlaneV2, KeyExecutorProtocolV2})
	flags := registry.Flags()
	operator := findFlag(flags, KeyOperatorControlPlaneV2)
	protocol := findFlag(flags, KeyExecutorProtocolV2)

	g.Expect(registry.IsEnabled(KeyOperatorControlPlaneV2)).To(BeTrue())
	g.Expect(registry.IsEnabled(KeyExecutorProtocolV2)).To(BeTrue())
	g.Expect(operator.Key).To(Equal(KeyOperatorControlPlaneV2))
	g.Expect(operator.Label).To(Equal("Operator Control Plane v2"))
	g.Expect(operator.Description).NotTo(BeEmpty())
	g.Expect(operator.Enabled).To(BeTrue())
	g.Expect(protocol.Key).To(Equal(KeyExecutorProtocolV2))
	g.Expect(protocol.Label).To(Equal("Executor Protocol v2"))
	g.Expect(protocol.Description).NotTo(BeEmpty())
	g.Expect(protocol.Enabled).To(BeTrue())
	g.Expect(AllKeys()).To(ContainElement(KeyOperatorControlPlaneV2))
	g.Expect(AllKeys()).To(ContainElement(KeyExecutorProtocolV2))
	g.Expect(indexOfKey(AllKeys(), KeyOperatorControlPlaneV2)).To(BeNumerically("<", indexOfKey(AllKeys(), KeyExecutorProtocolV2)))
}

func TestRegistryMarksEnabledFlags(t *testing.T) {
	g := NewWithT(t)

	registry := NewRegistry([]Key{KeyReleaseBundles, KeyEnvironments})
	flags := registry.Flags()
	environments := findFlag(flags, KeyEnvironments)
	lifecycles := findFlag(flags, KeyLifecycles)

	g.Expect(registry.IsEnabled(KeyEnvironments)).To(BeTrue())
	g.Expect(registry.IsEnabled(KeyReleaseBundles)).To(BeTrue())
	g.Expect(registry.IsEnabled(KeyLifecycles)).To(BeFalse())
	g.Expect(environments.Key).To(Equal(KeyEnvironments))
	g.Expect(environments.Label).To(Equal("Environments"))
	g.Expect(environments.Description).NotTo(BeEmpty())
	g.Expect(environments.Milestone).To(Equal("Milestone B"))
	g.Expect(environments.Enabled).To(BeTrue())
	g.Expect(lifecycles.Key).To(Equal(KeyLifecycles))
	g.Expect(lifecycles.Enabled).To(BeFalse())
	scopedVariables := findFlag(flags, KeyScopedVariablesV2)
	g.Expect(scopedVariables.Key).To(Equal(KeyScopedVariablesV2))
	g.Expect(scopedVariables.Label).To(Equal("Scoped Variables"))
	g.Expect(scopedVariables.Description).NotTo(BeEmpty())
	g.Expect(scopedVariables.Milestone).To(Equal("Milestone C"))
	agentCapabilities := findFlag(flags, KeyAgentCapabilities)
	g.Expect(agentCapabilities.Key).To(Equal(KeyAgentCapabilities))
	g.Expect(agentCapabilities.Label).To(Equal("Agent Capabilities"))
	g.Expect(agentCapabilities.Description).NotTo(BeEmpty())
	g.Expect(agentCapabilities.Milestone).To(Equal("Milestone D"))
	agentTaskLeases := findFlag(flags, KeyAgentTaskLeases)
	g.Expect(agentTaskLeases.Key).To(Equal(KeyAgentTaskLeases))
	g.Expect(agentTaskLeases.Label).To(Equal("Agent Task Leases"))
	g.Expect(agentTaskLeases.Description).NotTo(BeEmpty())
	g.Expect(agentTaskLeases.Milestone).To(Equal("Milestone D"))
	stepEvents := findFlag(flags, KeyStepEvents)
	g.Expect(stepEvents.Key).To(Equal(KeyStepEvents))
	g.Expect(stepEvents.Label).To(Equal("Step Events"))
	g.Expect(stepEvents.Description).NotTo(BeEmpty())
	g.Expect(stepEvents.Milestone).To(Equal("Milestone D"))
	observabilityMetrics := findFlag(flags, KeyObservabilityMetrics)
	g.Expect(observabilityMetrics.Key).To(Equal(KeyObservabilityMetrics))
	g.Expect(observabilityMetrics.Label).To(Equal("Observability Metrics"))
	g.Expect(observabilityMetrics.Description).NotTo(BeEmpty())
	g.Expect(observabilityMetrics.Milestone).To(Equal("Milestone G"))
	observabilityTracing := findFlag(flags, KeyObservabilityTracing)
	g.Expect(observabilityTracing.Key).To(Equal(KeyObservabilityTracing))
	g.Expect(observabilityTracing.Label).To(Equal("Observability Tracing"))
	g.Expect(observabilityTracing.Description).NotTo(BeEmpty())
	g.Expect(observabilityTracing.Milestone).To(Equal("Milestone G"))
	observabilityDashboards := findFlag(flags, KeyObservabilityDashboards)
	g.Expect(observabilityDashboards.Key).To(Equal(KeyObservabilityDashboards))
	g.Expect(observabilityDashboards.Label).To(Equal("Observability Dashboards"))
	g.Expect(observabilityDashboards.Description).NotTo(BeEmpty())
	g.Expect(observabilityDashboards.Milestone).To(Equal("Milestone G"))
	observabilityCorrelation := findFlag(flags, KeyObservabilityCorrelation)
	g.Expect(observabilityCorrelation.Key).To(Equal(KeyObservabilityCorrelation))
	g.Expect(observabilityCorrelation.Label).To(Equal("Observability Correlation"))
	g.Expect(observabilityCorrelation.Description).NotTo(BeEmpty())
	g.Expect(observabilityCorrelation.Milestone).To(Equal("Milestone G"))
	configAsCode := findFlag(flags, KeyConfigAsCode)
	g.Expect(configAsCode.Key).To(Equal(KeyConfigAsCode))
	g.Expect(configAsCode.Label).To(Equal("Config as Code"))
	g.Expect(configAsCode.Description).NotTo(BeEmpty())
	g.Expect(configAsCode.Milestone).To(Equal("Milestone H"))
}

func findFlag(flags []Flag, key Key) Flag {
	for _, flag := range flags {
		if flag.Key == key {
			return flag
		}
	}
	return Flag{}
}

func indexOfKey(keys []Key, key Key) int {
	for index, candidate := range keys {
		if candidate == key {
			return index
		}
	}
	return -1
}
