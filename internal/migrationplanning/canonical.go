package migrationplanning

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/distr-sh/distr/internal/types"
)

type canonicalMigrationContract struct {
	ID                               string                       `json:"id"`
	ComponentKey                     string                       `json:"componentKey"`
	DatabaseResourceKey              string                       `json:"databaseResourceKey"`
	ExpectedSourceVersion            string                       `json:"expectedSourceVersion"`
	ExpectedSourceChecksum           string                       `json:"expectedSourceChecksum"`
	ResultingVersion                 string                       `json:"resultingVersion"`
	ResultingSchemaChecksum          string                       `json:"resultingSchemaChecksum"`
	Phase                            types.MigrationPhase         `json:"phase"`
	DependsOn                        []string                     `json:"dependsOn,omitempty"`
	LockType                         string                       `json:"lockType"`
	LockTimeoutSeconds               int                          `json:"lockTimeoutSeconds"`
	OperationalImpact                string                       `json:"operationalImpact"`
	BackupRequired                   bool                         `json:"backupRequired"`
	BackupVerifier                   string                       `json:"backupVerifier,omitempty"`
	PreconditionProbes               []types.MigrationProbe       `json:"preconditionProbes"`
	PostconditionProbes              []types.MigrationProbe       `json:"postconditionProbes"`
	RetryClass                       types.MigrationRetryClass    `json:"retryClass"`
	IdempotencyKey                   string                       `json:"idempotencyKey,omitempty"`
	Reversibility                    types.MigrationReversibility `json:"reversibility"`
	PreviousApplicationCompatibility string                       `json:"previousApplicationCompatibility"`
	RecoveryProcedureReference       string                       `json:"recoveryProcedureReference"`
	RequiresForwardFix               bool                         `json:"requiresForwardFix"`
	AdapterType                      string                       `json:"adapterType,omitempty"`
	ArtifactDigest                   string                       `json:"artifactDigest,omitempty"`
	EvidenceRetentionDays            int                          `json:"evidenceRetentionDays"`
}

func NormalizeMigrationContract(contract types.MigrationContract) types.MigrationContract {
	normalized := contract
	normalized.ID = strings.TrimSpace(contract.ID)
	normalized.Checksum = strings.TrimSpace(contract.Checksum)
	normalized.ComponentKey = strings.TrimSpace(contract.ComponentKey)
	normalized.DatabaseResourceKey = strings.TrimSpace(contract.DatabaseResourceKey)
	normalized.ExpectedSourceVersion = strings.TrimSpace(contract.ExpectedSourceVersion)
	normalized.ExpectedSourceChecksum = strings.TrimSpace(contract.ExpectedSourceChecksum)
	normalized.ResultingVersion = strings.TrimSpace(contract.ResultingVersion)
	normalized.ResultingSchemaChecksum = strings.TrimSpace(contract.ResultingSchemaChecksum)
	normalized.LockType = strings.TrimSpace(contract.LockType)
	normalized.OperationalImpact = strings.TrimSpace(contract.OperationalImpact)
	normalized.BackupVerifier = strings.TrimSpace(contract.BackupVerifier)
	normalized.IdempotencyKey = strings.TrimSpace(contract.IdempotencyKey)
	normalized.PreviousApplicationCompatibility = strings.TrimSpace(
		contract.PreviousApplicationCompatibility,
	)
	normalized.RecoveryProcedureReference = strings.TrimSpace(contract.RecoveryProcedureReference)
	normalized.AdapterType = strings.TrimSpace(contract.AdapterType)
	normalized.ArtifactDigest = strings.TrimSpace(contract.ArtifactDigest)
	normalized.DependsOn = slices.Clone(contract.DependsOn)
	for index := range normalized.DependsOn {
		normalized.DependsOn[index] = strings.TrimSpace(normalized.DependsOn[index])
	}
	sort.Strings(normalized.DependsOn)
	normalized.PreconditionProbes = normalizeMigrationProbes(contract.PreconditionProbes)
	normalized.PostconditionProbes = normalizeMigrationProbes(contract.PostconditionProbes)
	return normalized
}

func CanonicalMigrationContractChecksum(contract types.MigrationContract) (string, error) {
	contract = NormalizeMigrationContract(contract)
	payload, err := json.Marshal(canonicalMigrationContract{
		ID: contract.ID, ComponentKey: contract.ComponentKey,
		DatabaseResourceKey:     contract.DatabaseResourceKey,
		ExpectedSourceVersion:   contract.ExpectedSourceVersion,
		ExpectedSourceChecksum:  contract.ExpectedSourceChecksum,
		ResultingVersion:        contract.ResultingVersion,
		ResultingSchemaChecksum: contract.ResultingSchemaChecksum,
		Phase:                   contract.Phase, DependsOn: slices.Clone(contract.DependsOn),
		LockType: contract.LockType, LockTimeoutSeconds: contract.LockTimeoutSeconds,
		OperationalImpact: contract.OperationalImpact,
		BackupRequired:    contract.BackupRequired, BackupVerifier: contract.BackupVerifier,
		PreconditionProbes:  slices.Clone(contract.PreconditionProbes),
		PostconditionProbes: slices.Clone(contract.PostconditionProbes),
		RetryClass:          contract.RetryClass, IdempotencyKey: contract.IdempotencyKey,
		Reversibility:                    contract.Reversibility,
		PreviousApplicationCompatibility: contract.PreviousApplicationCompatibility,
		RecoveryProcedureReference:       contract.RecoveryProcedureReference,
		RequiresForwardFix:               contract.RequiresForwardFix,
		AdapterType:                      contract.AdapterType, ArtifactDigest: contract.ArtifactDigest,
		EvidenceRetentionDays: contract.EvidenceRetentionDays,
	})
	if err != nil {
		return "", fmt.Errorf("marshal canonical migration contract: %w", err)
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func ValidateMigrationContractIntegrity(contract types.MigrationContract) []types.ValidationIssue {
	contract = NormalizeMigrationContract(contract)
	issues := ValidateMigrationContract(contract)
	expected, err := CanonicalMigrationContractChecksum(contract)
	if err != nil {
		issues = append(issues, types.ValidationIssue{
			Code: "migration_checksum_failed", Field: "checksum",
			Message: "migration contract checksum could not be calculated",
		})
	} else if checksumPattern.MatchString(contract.Checksum) && contract.Checksum != expected {
		issues = append(issues, types.ValidationIssue{
			Code: "migration_checksum_mismatch", Field: "checksum",
			Message: "migration contract checksum does not match its canonical contents",
		})
	}
	slices.SortFunc(issues, func(a, b types.ValidationIssue) int {
		if compare := strings.Compare(a.Field, b.Field); compare != 0 {
			return compare
		}
		return strings.Compare(a.Code, b.Code)
	})
	return issues
}

func OrderMigrationContracts(contracts []types.MigrationContract) ([]types.MigrationContract, error) {
	normalized := make([]types.MigrationContract, len(contracts))
	byID := make(map[string]types.MigrationContract, len(contracts))
	indegree := make(map[string]int, len(contracts))
	dependents := make(map[string][]string, len(contracts))
	for index, contract := range contracts {
		contract = NormalizeMigrationContract(contract)
		normalized[index] = contract
		if issues := ValidateMigrationContractIntegrity(contract); len(issues) > 0 {
			return nil, fmt.Errorf("invalid migration contract %q: %s", contract.ID, issues[0].Message)
		}
		if _, duplicate := byID[contract.ID]; duplicate {
			return nil, fmt.Errorf("duplicate migration contract %q", contract.ID)
		}
		byID[contract.ID] = contract
		indegree[contract.ID] = 0
	}
	for _, contract := range normalized {
		for _, dependency := range contract.DependsOn {
			if _, exists := byID[dependency]; !exists {
				return nil, fmt.Errorf(
					"migration contract %q depends on missing contract %q",
					contract.ID,
					dependency,
				)
			}
			indegree[contract.ID]++
			dependents[dependency] = append(dependents[dependency], contract.ID)
		}
	}
	ready := make([]string, 0)
	for id, count := range indegree {
		if count == 0 {
			ready = append(ready, id)
		}
	}
	sort.Strings(ready)
	result := make([]types.MigrationContract, 0, len(contracts))
	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]
		result = append(result, byID[id])
		sort.Strings(dependents[id])
		for _, dependent := range dependents[id] {
			indegree[dependent]--
			if indegree[dependent] == 0 {
				position, _ := slices.BinarySearch(ready, dependent)
				ready = slices.Insert(ready, position, dependent)
			}
		}
	}
	if len(result) != len(contracts) {
		return nil, fmt.Errorf("migration contract dependency graph contains a cycle")
	}
	return result, nil
}

func normalizeMigrationProbes(probes []types.MigrationProbe) []types.MigrationProbe {
	result := slices.Clone(probes)
	for index := range result {
		result[index].Name = strings.TrimSpace(result[index].Name)
		result[index].Reference = strings.TrimSpace(result[index].Reference)
		result[index].ExpectedChecksum = strings.TrimSpace(result[index].ExpectedChecksum)
	}
	slices.SortFunc(result, func(a, b types.MigrationProbe) int {
		if compare := strings.Compare(a.Reference, b.Reference); compare != 0 {
			return compare
		}
		if compare := strings.Compare(a.Name, b.Name); compare != 0 {
			return compare
		}
		return strings.Compare(a.ExpectedChecksum, b.ExpectedChecksum)
	})
	return result
}
