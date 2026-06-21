package types

import (
	"time"

	"github.com/google/uuid"
)

const (
	AgentCapabilityProtocolV1 = "v1"
	AgentActionVersionV1      = "1"
)

type AgentCapabilityReport struct {
	ID                    uuid.UUID               `db:"id" json:"id"`
	CreatedAt             time.Time               `db:"created_at" json:"createdAt"`
	UpdatedAt             time.Time               `db:"updated_at" json:"updatedAt"`
	OrganizationID        uuid.UUID               `db:"organization_id" json:"organizationId"`
	DeploymentTargetID    uuid.UUID               `db:"deployment_target_id" json:"deploymentTargetId"`
	ProtocolVersion       string                  `db:"protocol_version" json:"protocolVersion"`
	AgentVersion          string                  `db:"agent_version" json:"agentVersion"`
	SupportedRuntimes     []string                `db:"supported_runtimes" json:"supportedRuntimes"`
	OperatingSystem       string                  `db:"operating_system" json:"operatingSystem"`
	Architecture          string                  `db:"architecture" json:"architecture"`
	AvailableTooling      []string                `db:"available_tooling" json:"availableTooling"`
	StrategyCapabilities  []string                `db:"strategy_capabilities" json:"strategyCapabilities"`
	CompatibilityWarnings []string                `db:"compatibility_warnings" json:"compatibilityWarnings"`
	SupportedActions      []AgentActionCapability `db:"-" json:"supportedActions"`
}

type AgentActionCapability struct {
	ID                 uuid.UUID `db:"id" json:"id"`
	ReportID           uuid.UUID `db:"report_id" json:"reportId"`
	OrganizationID     uuid.UUID `db:"organization_id" json:"organizationId"`
	DeploymentTargetID uuid.UUID `db:"deployment_target_id" json:"deploymentTargetId"`
	ActionType         string    `db:"action_type" json:"actionType"`
	Versions           []string  `db:"versions" json:"versions"`
}
