package policy

import "strings"

type circuitState string

const (
	circuitClosed   circuitState = "closed"
	circuitOpen     circuitState = "open"
	circuitHalfOpen circuitState = "half_open"
)

type circuitBreakerRegistry struct {
	states         map[string]circuitState
	halfOpenProbes map[string]int
	halfOpenLimit  int
}

func newCircuitBreakerRegistry(openHosts []string, halfOpenHosts []string, halfOpenLimit int) circuitBreakerRegistry {
	if halfOpenLimit <= 0 {
		halfOpenLimit = 1
	}
	registry := circuitBreakerRegistry{
		states:         map[string]circuitState{},
		halfOpenProbes: map[string]int{},
		halfOpenLimit:  halfOpenLimit,
	}
	for _, host := range openHosts {
		if normalized := normalizePolicyToken(host); normalized != "" {
			registry.states[normalized] = circuitOpen
		}
	}
	for _, host := range halfOpenHosts {
		if normalized := normalizePolicyToken(host); normalized != "" {
			registry.states[normalized] = circuitHalfOpen
		}
	}
	return registry
}

func (r *circuitBreakerRegistry) allow(host string) (bool, string) {
	host = normalizePolicyToken(host)
	switch r.states[host] {
	case circuitOpen:
		return false, "circuit breaker open"
	case circuitHalfOpen:
		if r.halfOpenProbes[host] >= r.halfOpenLimit {
			return false, "circuit breaker half-open probe limit exceeded"
		}
		r.halfOpenProbes[host]++
		return true, ""
	default:
		return true, ""
	}
}

func normalizePolicyToken(value string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
}
