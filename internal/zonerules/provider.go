// Package zonerules owns the deterministic timezone-rule dataset used for
// calendar canonicalization and admission evaluation.
package zonerules

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/KarpelesLab/gotz"
)

const (
	// ProductionRuleVersion is the caller-visible IANA release binding.
	ProductionRuleVersion = "2026a"
	// ProductionRuleDataIdentity identifies the exact embedded module artifact.
	// go.sum verifies this artifact at build time; gotz embeds the compiled data.
	ProductionRuleDataIdentity = "iana-2026a+gotz-v0.1.2+module-sum=h1:8kQPIqpfUnDRGiBNE1rmLpNeGLU165q5Y+uJcDXaZIQ="
)

type Provider interface {
	RuleVersion() string
	Identity() string
	LoadLocation(string) (*time.Location, error)
}

type embeddedProvider struct{}

func Production() Provider {
	return embeddedProvider{}
}

func (embeddedProvider) RuleVersion() string {
	return ProductionRuleVersion
}

func (embeddedProvider) Identity() string {
	return ProductionRuleDataIdentity
}

func (embeddedProvider) LoadLocation(name string) (*time.Location, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("IANA zone is required")
	}
	if name == "Local" {
		return nil, errors.New(`IANA zone "Local" is process-dependent and forbidden`)
	}
	zone, err := gotz.Load(name)
	if err != nil {
		return nil, fmt.Errorf("load pinned IANA zone %q: %w", name, err)
	}
	location, err := zone.Location()
	if err != nil {
		return nil, fmt.Errorf("construct pinned IANA zone %q: %w", name, err)
	}
	return location, nil
}

func ValidateBinding(
	provider Provider,
	ianaZone, declaredRuleVersion string,
) (*time.Location, error) {
	if provider == nil {
		return nil, errors.New("timezone rule provider is required")
	}
	declaredRuleVersion = strings.TrimSpace(declaredRuleVersion)
	if declaredRuleVersion == "" {
		return nil, errors.New("timezone rule version is required")
	}
	if declaredRuleVersion != provider.RuleVersion() {
		return nil, fmt.Errorf(
			"timezone rule version %q does not match runtime %q (%s)",
			declaredRuleVersion,
			provider.RuleVersion(),
			provider.Identity(),
		)
	}
	return provider.LoadLocation(ianaZone)
}
