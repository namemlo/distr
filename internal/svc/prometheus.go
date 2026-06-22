package svc

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

func createPrometheusRegistry(collectorsToRegister ...prometheus.Collector) *prometheus.Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	for _, collector := range collectorsToRegister {
		if collector != nil {
			reg.MustRegister(collector)
		}
	}
	return reg
}
