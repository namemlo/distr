package agentmanifest

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/url"
	"path"
	"text/template"

	"github.com/distr-sh/distr/internal/buildconfig"
	"github.com/distr-sh/distr/internal/customdomains"
	"github.com/distr-sh/distr/internal/env"
	"github.com/distr-sh/distr/internal/resources"
	"github.com/distr-sh/distr/internal/types"
)

func Get(
	ctx context.Context,
	deploymentTarget types.DeploymentTargetFull,
	org types.Organization,
	secret *string,
) (io.Reader, error) {
	if tmpl, err := getTemplate(deploymentTarget); err != nil {
		return nil, err
	} else if data, err := getTemplateData(deploymentTarget, org, secret); err != nil {
		return nil, err
	} else {
		var buf bytes.Buffer
		return &buf, tmpl.Execute(&buf, data)
	}
}

func getTemplateData(
	deploymentTarget types.DeploymentTargetFull,
	org types.Organization,
	secret *string,
) (map[string]any, error) {
	var (
		loginEndpoint        string
		manifestEndpoint     string
		resourcesEndpoint    string
		statusEndpoint       string
		metricsEndpoint      string
		capabilitiesEndpoint string
		leaseEndpoint        string
		heartbeatEndpoint    string
		logsEndpoint         string
		agentLogsEndpoint    string
	)

	if u, err := url.Parse(customdomains.AppDomainOrDefault(org)); err != nil {
		return nil, err
	} else {
		agentEndpoint := u.JoinPath("api", "v1", "agent")
		loginEndpoint = agentEndpoint.JoinPath("login").String()
		manifestEndpoint = agentEndpoint.JoinPath("manifest").String()
		resourcesEndpoint = agentEndpoint.JoinPath("resources").String()
		statusEndpoint = agentEndpoint.JoinPath("status").String()
		metricsEndpoint = agentEndpoint.JoinPath("metrics").String()
		logsEndpoint = agentEndpoint.JoinPath("logs").String()
		agentLogsEndpoint = agentEndpoint.JoinPath("deployment-target-logs").String()
		capabilitiesEndpoint = u.JoinPath(
			"api",
			"v1",
			"agents",
			deploymentTarget.ID.String(),
			"capabilities",
		).String()
		leaseEndpoint = u.JoinPath(
			"api",
			"v1",
			"agents",
			deploymentTarget.ID.String(),
			"lease",
		).String()
		heartbeatEndpoint = u.JoinPath(
			"api",
			"v1",
			"agents",
			deploymentTarget.ID.String(),
			"tasks",
		).String() + "/{taskId}/heartbeat"
	}

	result := map[string]any{
		"agentDockerConfig":             base64.StdEncoding.EncodeToString(env.AgentDockerConfig()),
		"agentInterval":                 env.AgentInterval(),
		"agentVersion":                  deploymentTarget.AgentVersion.Name,
		"agentVersionId":                deploymentTarget.AgentVersion.ID,
		"autohealAll":                   deploymentTarget.AutohealEnabled,
		"loginEndpoint":                 loginEndpoint,
		"manifestEndpoint":              manifestEndpoint,
		"metricsEndpoint":               metricsEndpoint,
		"capabilitiesEndpoint":          capabilitiesEndpoint,
		"leaseEndpoint":                 leaseEndpoint,
		"taskHeartbeatEndpointTemplate": heartbeatEndpoint,
		"registryEnabled":               env.RegistryEnabled(),
		"registryHost":                  customdomains.RegistryDomainOrDefault(org),
		"registryPlainHttp":             buildconfig.IsDevelopment(),
		"resourcesEndpoint":             resourcesEndpoint,
		"statusEndpoint":                statusEndpoint,
		"targetId":                      deploymentTarget.ID,
		"targetSecret":                  secret,
		"logsEndpoint":                  logsEndpoint,
		"agentLogsEndpoint":             agentLogsEndpoint,
		"metricsEnabled":                deploymentTarget.MetricsEnabled,
	}
	if deploymentTarget.Namespace != nil {
		result["targetNamespace"] = *deploymentTarget.Namespace
	}
	if deploymentTarget.Scope != nil {
		result["targetScope"] = *deploymentTarget.Scope
	}
	if deploymentTarget.Resources != nil {
		result["targetResources"] = deploymentTarget.Resources
	}
	return result, nil
}

func getTemplate(deploymentTarget types.DeploymentTargetFull) (*template.Template, error) {
	if deploymentTarget.Type == types.DeploymentTypeDocker {
		return resources.GetTemplate(path.Join(
			"agent/docker",
			deploymentTarget.AgentVersion.ComposeFileRevision,
			"docker-compose.yaml.tmpl",
		))
	} else {
		return resources.GetTemplate(path.Join(
			"agent/kubernetes",
			deploymentTarget.AgentVersion.ManifestFileRevision,
			"manifest.yaml.tmpl",
		))
	}
}
