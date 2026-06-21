package main

import (
	"errors"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/stepredaction"
)

func agentDeploymentSecretValues(deployment api.AgentDeployment) []string {
	values := make([]string, 0, len(deployment.RegistryAuth))
	for _, auth := range deployment.RegistryAuth {
		if auth.Password != "" {
			values = append(values, auth.Password)
		}
	}
	return values
}

func redactStringWithSecretValues(value string, secretValues []string) string {
	redacted, _ := stepredaction.RedactStringWithValues(value, secretValues)
	return redacted
}

func redactErrorWithSecretValues(err error, secretValues []string) error {
	if err == nil {
		return nil
	}
	redacted := redactStringWithSecretValues(err.Error(), secretValues)
	if redacted == err.Error() {
		return err
	}
	return errors.New(redacted)
}

func redactStatusUpdater(updateStatus func(string), secretValues []string) func(string) {
	if updateStatus == nil {
		return nil
	}
	return func(status string) {
		updateStatus(redactStringWithSecretValues(status, secretValues))
	}
}
