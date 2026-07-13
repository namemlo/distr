package mapping

import (
	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
)

func VariableSnapshotToAPI(snapshot types.VariableSnapshot) api.VariableSnapshot {
	return api.VariableSnapshot{
		ID:                snapshot.ID,
		CreatedAt:         snapshot.CreatedAt,
		ReleaseBundleID:   snapshot.ReleaseBundleID,
		ApplicationID:     snapshot.ApplicationID,
		ChannelID:         snapshot.ChannelID,
		CanonicalChecksum: snapshot.CanonicalChecksum,
		ResolutionMode:    string(snapshot.ResolutionMode),
		Values:            List(snapshot.Values, VariableSnapshotValueToAPI),
	}
}

func VariableSnapshotValueToAPI(value types.VariableSnapshotValue) api.VariableSnapshotValue {
	return api.VariableSnapshotValue{
		ID:                 value.ID,
		VariableSnapshotID: value.VariableSnapshotID,
		VariableSetID:      value.VariableSetID,
		VariableID:         value.VariableID,
		Key:                value.Key,
		Type:               api.VariableType(value.Type),
		IsRequired:         value.IsRequired,
		Status:             string(value.Status),
		Source:             string(value.Source),
		Value:              value.Value,
		ReferenceID:        value.ReferenceID,
		ReferenceName:      value.ReferenceName,
		Redacted:           value.Redacted,
		Trace:              List(value.Trace, VariableResolutionTraceEntryToAPI),
	}
}

func ConfigurationDriftToAPI(drift types.ConfigurationDrift) api.ConfigurationDrift {
	return api.ConfigurationDrift{
		DeploymentID:           drift.DeploymentID,
		ApplicationID:          drift.ApplicationID,
		HasDrift:               drift.HasDrift,
		NewRequiredVariables:   List(drift.NewRequiredVariables, ConfigurationDriftVariableToAPI),
		MissingVariables:       List(drift.MissingVariables, ConfigurationDriftVariableToAPI),
		RemovedVariables:       List(drift.RemovedVariables, ConfigurationDriftRemovedValueToAPI),
		TypeChanges:            List(drift.TypeChanges, ConfigurationDriftTypeChangeToAPI),
		DefaultChanges:         List(drift.DefaultChanges, ConfigurationDriftDefaultChangeToAPI),
		SecretReferenceChanges: List(drift.SecretReferenceChanges, ConfigurationDriftReferenceChangeToAPI),
	}
}

func ConfigurationDriftVariableToAPI(variable types.ConfigurationDriftVariable) api.ConfigurationDriftVariable {
	return api.ConfigurationDriftVariable{
		Key:           variable.Key,
		Type:          api.VariableType(variable.Type),
		IsRequired:    variable.IsRequired,
		Source:        string(variable.Source),
		Value:         variable.Value,
		ReferenceID:   variable.ReferenceID,
		ReferenceName: variable.ReferenceName,
		Redacted:      variable.Redacted,
	}
}

func ConfigurationDriftRemovedValueToAPI(
	value types.ConfigurationDriftRemovedValue,
) api.ConfigurationDriftRemovedValue {
	return api.ConfigurationDriftRemovedValue{Key: value.Key}
}

func ConfigurationDriftTypeChangeToAPI(change types.ConfigurationDriftTypeChange) api.ConfigurationDriftTypeChange {
	return api.ConfigurationDriftTypeChange{
		Key:          change.Key,
		ExpectedType: api.VariableType(change.ExpectedType),
		DeployedType: change.DeployedType,
	}
}

func ConfigurationDriftDefaultChangeToAPI(
	change types.ConfigurationDriftDefaultChange,
) api.ConfigurationDriftDefaultChange {
	return api.ConfigurationDriftDefaultChange{
		Key:           change.Key,
		Type:          api.VariableType(change.Type),
		CurrentValue:  change.CurrentValue,
		DeployedValue: change.DeployedValue,
	}
}

func ConfigurationDriftReferenceChangeToAPI(
	change types.ConfigurationDriftReferenceChange,
) api.ConfigurationDriftReferenceChange {
	return api.ConfigurationDriftReferenceChange{
		Key:           change.Key,
		Type:          api.VariableType(change.Type),
		ReferenceID:   change.ReferenceID,
		ReferenceName: change.ReferenceName,
		Redacted:      change.Redacted,
	}
}
