package pilotexception

import (
	"errors"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

const Key = "scoped-single-reviewer-pilot"

var approvalReferencePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{7,255}$`)

type Evidence struct {
	Key               string `json:"key"`
	ApprovalReference string `json:"approvalReference"`
}

func (e Evidence) Valid() bool {
	return e.Key == Key && approvalReferencePattern.MatchString(e.ApprovalReference)
}

type Config struct {
	enabled            bool
	organizationID     uuid.UUID
	environmentID      uuid.UUID
	deploymentTargetID uuid.UUID
	approvalReference  string
}

func Parse(
	enabled bool,
	organizationID string,
	environmentID string,
	deploymentTargetID string,
	approvalReference string,
) (Config, error) {
	if !enabled {
		return Config{}, nil
	}
	organization, err := uuid.Parse(strings.TrimSpace(organizationID))
	if err != nil || organization == uuid.Nil {
		return Config{}, errors.New("scoped single-reviewer pilot organization id is required")
	}
	environment, err := uuid.Parse(strings.TrimSpace(environmentID))
	if err != nil || environment == uuid.Nil {
		return Config{}, errors.New("scoped single-reviewer pilot environment id is required")
	}
	target, err := uuid.Parse(strings.TrimSpace(deploymentTargetID))
	if err != nil || target == uuid.Nil {
		return Config{}, errors.New("scoped single-reviewer pilot deployment target id is required")
	}
	reference := strings.TrimSpace(approvalReference)
	if !approvalReferencePattern.MatchString(reference) {
		return Config{}, errors.New("scoped single-reviewer pilot approval reference is invalid")
	}
	return Config{
		enabled:            true,
		organizationID:     organization,
		environmentID:      environment,
		deploymentTargetID: target,
		approvalReference:  reference,
	}, nil
}

func (c Config) ApprovalEvidence(
	organizationID uuid.UUID,
	environmentID uuid.UUID,
	deploymentTargetIDs []uuid.UUID,
	requesterID uuid.UUID,
	actorID uuid.UUID,
	approve bool,
) *Evidence {
	if !c.enabled || !approve || requesterID == uuid.Nil || actorID != requesterID ||
		organizationID != c.organizationID || environmentID != c.environmentID ||
		len(deploymentTargetIDs) != 1 || deploymentTargetIDs[0] != c.deploymentTargetID {
		return nil
	}
	return &Evidence{Key: Key, ApprovalReference: c.approvalReference}
}

func (c Config) ProtectedHistoryEvidence(
	organizationID uuid.UUID,
	environmentID uuid.UUID,
	customerOrganizationIDs []string,
	deploymentTargetIDs []string,
	issuerID uuid.UUID,
	reviewerID uuid.UUID,
) *Evidence {
	if !c.enabled || issuerID == uuid.Nil || reviewerID != issuerID ||
		organizationID != c.organizationID || environmentID != c.environmentID ||
		len(customerOrganizationIDs) != 0 ||
		len(deploymentTargetIDs) != 1 || deploymentTargetIDs[0] != c.deploymentTargetID.String() {
		return nil
	}
	return &Evidence{Key: Key, ApprovalReference: c.approvalReference}
}
