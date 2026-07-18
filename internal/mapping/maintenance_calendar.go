package mapping

import (
	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
)

func MaintenanceWindowRuleToAPI(rule types.MaintenanceWindowRule) api.MaintenanceWindowRule {
	return api.MaintenanceWindowRule{
		ID:          rule.ID,
		Name:        rule.Name,
		Weekdays:    append([]int32(nil), rule.Weekdays...),
		StartMinute: rule.StartMinute,
		EndMinute:   rule.EndMinute,
		SortOrder:   rule.SortOrder,
	}
}

func PublishedMaintenanceWindowRuleToAPI(
	rule types.MaintenanceWindowRule,
) api.MaintenanceWindowRule {
	mapped := MaintenanceWindowRuleToAPI(rule)
	mapped.VersionRuleID = uuidPtrIfNotNil(rule.VersionRuleID)
	return mapped
}

func MaintenanceCalendarToAPI(calendar types.MaintenanceCalendar) api.MaintenanceCalendar {
	return api.MaintenanceCalendar{
		ID:                     calendar.ID,
		CreatedAt:              calendar.CreatedAt,
		UpdatedAt:              calendar.UpdatedAt,
		Name:                   calendar.Name,
		Description:            calendar.Description,
		DraftIANAZone:          calendar.DraftIANAZone,
		DraftRuleVersion:       calendar.DraftRuleVersion,
		DraftRules:             List(calendar.DraftRules, MaintenanceWindowRuleToAPI),
		DraftRevision:          calendar.DraftRevision,
		LastPublishedVersionID: calendar.LastPublishedVersionID,
		CreatedBy:              calendar.CreatedBy,
		UpdatedBy:              calendar.UpdatedBy,
	}
}

func MaintenanceCalendarVersionToAPI(
	version types.MaintenanceCalendarVersion,
) api.MaintenanceCalendarVersion {
	return api.MaintenanceCalendarVersion{
		ID:                  version.ID,
		CalendarID:          version.CalendarID,
		VersionNumber:       version.VersionNumber,
		SourceDraftRevision: version.SourceDraftRevision,
		Name:                version.Name,
		Description:         version.Description,
		IANAZone:            version.IANAZone,
		RuleVersion:         version.RuleVersion,
		Checksum:            version.Checksum,
		PublishedAt:         version.PublishedAt,
		PublishedBy:         version.PublishedBy,
		WindowRules:         List(version.WindowRules, PublishedMaintenanceWindowRuleToAPI),
	}
}

func DeploymentFreezeToAPI(freeze types.DeploymentFreeze) api.DeploymentFreeze {
	return api.DeploymentFreeze{
		ID:               freeze.ID,
		CreatedAt:        freeze.CreatedAt,
		UpdatedAt:        freeze.UpdatedAt,
		Name:             freeze.Name,
		DraftStartAt:     freeze.DraftStartAt,
		DraftEndAt:       freeze.DraftEndAt,
		DraftIANAZone:    freeze.DraftIANAZone,
		DraftRuleVersion: freeze.DraftRuleVersion,
		DraftScope: types.CalendarScopeRef{
			Kind: freeze.DraftScopeKind,
			ID:   freeze.DraftScopeID,
		},
		DraftPriority:           freeze.DraftPriority,
		DraftReason:             freeze.DraftReason,
		DraftRevision:           freeze.DraftRevision,
		LastPublishedRevisionID: freeze.LastPublishedRevisionID,
		CreatedBy:               freeze.CreatedBy,
		UpdatedBy:               freeze.UpdatedBy,
	}
}

func DeploymentFreezeRevisionToAPI(
	revision types.DeploymentFreezeRevision,
) api.DeploymentFreezeRevision {
	return api.DeploymentFreezeRevision{
		ID:                  revision.ID,
		FreezeID:            revision.FreezeID,
		VersionNumber:       revision.VersionNumber,
		SourceDraftRevision: revision.SourceDraftRevision,
		Name:                revision.Name,
		StartAt:             revision.StartAt,
		EndAt:               revision.EndAt,
		IANAZone:            revision.IANAZone,
		RuleVersion:         revision.RuleVersion,
		Scope: types.CalendarScopeRef{
			Kind: revision.ScopeKind,
			ID:   revision.ScopeID,
		},
		Priority:    revision.Priority,
		Reason:      revision.Reason,
		Checksum:    revision.Checksum,
		PublishedAt: revision.PublishedAt,
		PublishedBy: revision.PublishedBy,
	}
}

func MaintenanceCalendarPageToAPI(
	page types.Page[types.MaintenanceCalendar],
) api.MaintenanceCalendarPage {
	return api.MaintenanceCalendarPage{
		Items:      List(page.Items, MaintenanceCalendarToAPI),
		NextCursor: page.NextCursor,
	}
}

func MaintenanceCalendarVersionPageToAPI(
	page types.Page[types.MaintenanceCalendarVersion],
) api.MaintenanceCalendarVersionPage {
	return api.MaintenanceCalendarVersionPage{
		Items:      List(page.Items, MaintenanceCalendarVersionToAPI),
		NextCursor: page.NextCursor,
	}
}

func DeploymentFreezePageToAPI(
	page types.Page[types.DeploymentFreeze],
) api.DeploymentFreezePage {
	return api.DeploymentFreezePage{
		Items:      List(page.Items, DeploymentFreezeToAPI),
		NextCursor: page.NextCursor,
	}
}

func DeploymentFreezeRevisionPageToAPI(
	page types.Page[types.DeploymentFreezeRevision],
) api.DeploymentFreezeRevisionPage {
	return api.DeploymentFreezeRevisionPage{
		Items:      List(page.Items, DeploymentFreezeRevisionToAPI),
		NextCursor: page.NextCursor,
	}
}
