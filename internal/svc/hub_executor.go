package svc

import "github.com/distr-sh/distr/internal/hubexecutor"

func (r *Registry) GetHubExecutor() *hubexecutor.Worker {
	return r.hubExecutor
}
