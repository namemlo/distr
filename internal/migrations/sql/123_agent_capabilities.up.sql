CREATE TABLE AgentCapabilityReport (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  updated_at TIMESTAMP NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  deployment_target_id UUID NOT NULL,
  protocol_version TEXT NOT NULL CHECK (length(trim(protocol_version)) > 0),
  agent_version TEXT NOT NULL CHECK (length(trim(agent_version)) > 0),
  supported_runtimes TEXT[] NOT NULL DEFAULT '{}',
  operating_system TEXT NOT NULL CHECK (length(trim(operating_system)) > 0),
  architecture TEXT NOT NULL CHECK (length(trim(architecture)) > 0),
  available_tooling TEXT[] NOT NULL DEFAULT '{}',
  strategy_capabilities TEXT[] NOT NULL DEFAULT '{}',
  compatibility_warnings TEXT[] NOT NULL DEFAULT '{}',
  CONSTRAINT agentcapabilityreport_target_fk
    FOREIGN KEY (deployment_target_id, organization_id)
    REFERENCES DeploymentTarget(id, organization_id)
    ON DELETE CASCADE,
  CONSTRAINT agentcapabilityreport_target_unique UNIQUE (deployment_target_id)
);

CREATE TABLE AgentActionCapability (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  report_id UUID NOT NULL,
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  deployment_target_id UUID NOT NULL,
  action_type TEXT NOT NULL CHECK (length(trim(action_type)) > 0),
  versions TEXT[] NOT NULL DEFAULT '{}',
  CONSTRAINT agentactioncapability_report_fk
    FOREIGN KEY (report_id)
    REFERENCES AgentCapabilityReport(id)
    ON DELETE CASCADE,
  CONSTRAINT agentactioncapability_target_fk
    FOREIGN KEY (deployment_target_id, organization_id)
    REFERENCES DeploymentTarget(id, organization_id)
    ON DELETE CASCADE,
  CONSTRAINT agentactioncapability_report_action_unique UNIQUE (report_id, action_type)
);

CREATE INDEX AgentCapabilityReport_organization_target
  ON AgentCapabilityReport (organization_id, deployment_target_id);

CREATE INDEX AgentActionCapability_report
  ON AgentActionCapability (report_id, action_type);
