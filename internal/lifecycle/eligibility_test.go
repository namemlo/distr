package lifecycle

import (
	"context"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
)

func TestEligibilityServiceAllowsPublishedReleaseIntoFirstMatchingPhase(t *testing.T) {
	g := NewWithT(t)
	releaseBundleID := uuid.New()
	applicationID := uuid.New()
	channelID := uuid.New()
	lifecycleID := uuid.New()
	devPhaseID := uuid.New()
	prodPhaseID := uuid.New()
	devEnvironmentID := uuid.New()
	prodEnvironmentID := uuid.New()
	service := NewEligibilityService()

	result := service.Explain(context.Background(), EligibilityRequest{
		ReleaseBundle: types.ReleaseBundle{
			ID:            releaseBundleID,
			ApplicationID: applicationID,
			ChannelID:     channelID,
			Status:        types.ReleaseBundleStatusPublished,
		},
		Channel: types.Channel{
			ID:            channelID,
			ApplicationID: applicationID,
			LifecycleID:   lifecycleID,
		},
		Lifecycle: types.Lifecycle{
			ID: lifecycleID,
			Phases: []types.LifecyclePhase{
				{ID: prodPhaseID, Name: "Production", SortOrder: 20, EnvironmentIDs: []uuid.UUID{prodEnvironmentID}},
				{ID: devPhaseID, Name: "Development", SortOrder: 10, EnvironmentIDs: []uuid.UUID{devEnvironmentID}},
			},
		},
		EnvironmentID: devEnvironmentID,
	})

	g.Expect(result.EngineReady).To(BeTrue())
	g.Expect(result.Eligible).To(BeTrue())
	g.Expect(result.ReleaseBundleID).To(Equal(releaseBundleID))
	g.Expect(result.ApplicationID).To(Equal(applicationID))
	g.Expect(result.ChannelID).To(Equal(channelID))
	g.Expect(result.LifecycleID).To(Equal(lifecycleID))
	g.Expect(result.EnvironmentID).To(Equal(devEnvironmentID))
	g.Expect(result.TargetPhase).NotTo(BeNil())
	g.Expect(*result.TargetPhase).To(MatchFields(IgnoreExtras, Fields{
		"ID":                 Equal(devPhaseID),
		"Name":               Equal("Development"),
		"SortOrder":          Equal(10),
		"EnvironmentIDs":     Equal([]uuid.UUID{devEnvironmentID}),
		"MatchesEnvironment": BeTrue(),
	}))
	g.Expect(result.Phases).To(ConsistOf(
		MatchFields(IgnoreExtras, Fields{
			"ID":                 Equal(devPhaseID),
			"SortOrder":          Equal(10),
			"MatchesEnvironment": BeTrue(),
		}),
		MatchFields(IgnoreExtras, Fields{
			"ID":                 Equal(prodPhaseID),
			"SortOrder":          Equal(20),
			"MatchesEnvironment": BeFalse(),
		}),
	))
	g.Expect(result.Phases[0].ID).To(Equal(devPhaseID))
	g.Expect(result.Phases[1].ID).To(Equal(prodPhaseID))
	g.Expect(result.Reasons).To(BeEmpty())
}

func TestEligibilityServiceRejectsReleaseStatuses(t *testing.T) {
	tests := []struct {
		name     string
		status   types.ReleaseBundleStatus
		wantCode EligibilityReasonCode
	}{
		{name: "draft", status: types.ReleaseBundleStatusDraft, wantCode: EligibilityReasonReleaseNotPublished},
		{name: "validating", status: types.ReleaseBundleStatusValidating, wantCode: EligibilityReasonReleaseNotPublished},
		{name: "blocked", status: types.ReleaseBundleStatusBlocked, wantCode: EligibilityReasonReleaseBlocked},
		{name: "archived", status: types.ReleaseBundleStatusArchived, wantCode: EligibilityReasonReleaseArchived},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			envID := uuid.New()
			lifecycleID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
			result := NewEligibilityService().Explain(context.Background(), EligibilityRequest{
				ReleaseBundle: types.ReleaseBundle{
					ID:     uuid.New(),
					Status: tt.status,
				},
				Channel: types.Channel{LifecycleID: lifecycleID},
				Lifecycle: types.Lifecycle{
					ID: lifecycleID,
					Phases: []types.LifecyclePhase{
						{ID: uuid.New(), Name: "Development", SortOrder: 10, EnvironmentIDs: []uuid.UUID{envID}},
					},
				},
				EnvironmentID: envID,
			})

			g.Expect(result.Eligible).To(BeFalse())
			g.Expect(result.Reasons).To(ContainElement(MatchFields(IgnoreExtras, Fields{
				"Code": Equal(tt.wantCode),
			})))
		})
	}
}

func TestEligibilityServiceExplainsRequiredAndOptionalPriorPhases(t *testing.T) {
	g := NewWithT(t)
	devPhaseID := uuid.New()
	qaPhaseID := uuid.New()
	prodPhaseID := uuid.New()
	devEnvironmentID := uuid.New()
	qaEnvironmentID := uuid.New()
	prodEnvironmentID := uuid.New()
	lifecycleID := uuid.New()

	result := NewEligibilityService().Explain(context.Background(), EligibilityRequest{
		ReleaseBundle: types.ReleaseBundle{ID: uuid.New(), Status: types.ReleaseBundleStatusPublished},
		Channel:       types.Channel{LifecycleID: lifecycleID},
		Lifecycle: types.Lifecycle{
			ID: lifecycleID,
			Phases: []types.LifecyclePhase{
				{
					ID:                           prodPhaseID,
					Name:                         "Production",
					SortOrder:                    30,
					EnvironmentIDs:               []uuid.UUID{prodEnvironmentID},
					MinimumSuccessfulDeployments: 2,
				},
				{
					ID:             qaPhaseID,
					Name:           "QA",
					SortOrder:      20,
					EnvironmentIDs: []uuid.UUID{qaEnvironmentID},
					Optional:       true,
				},
				{
					ID:                           devPhaseID,
					Name:                         "Development",
					SortOrder:                    10,
					EnvironmentIDs:               []uuid.UUID{devEnvironmentID},
					MinimumSuccessfulDeployments: 1,
				},
			},
		},
		EnvironmentID: prodEnvironmentID,
	})

	g.Expect(result.Eligible).To(BeFalse())
	g.Expect(result.TargetPhase).NotTo(BeNil())
	g.Expect(result.TargetPhase.ID).To(Equal(prodPhaseID))
	g.Expect(result.Phases).To(ConsistOf(
		MatchFields(IgnoreExtras, Fields{
			"ID":                   Equal(devPhaseID),
			"RequiredBeforeTarget": BeTrue(),
			"BlocksEligibility":    BeTrue(),
		}),
		MatchFields(IgnoreExtras, Fields{
			"ID":                   Equal(qaPhaseID),
			"Optional":             BeTrue(),
			"RequiredBeforeTarget": BeFalse(),
			"BlocksEligibility":    BeFalse(),
		}),
		MatchFields(IgnoreExtras, Fields{
			"ID":                 Equal(prodPhaseID),
			"MatchesEnvironment": BeTrue(),
		}),
	))
	g.Expect(result.Reasons).To(ConsistOf(MatchFields(IgnoreExtras, Fields{
		"Code":    Equal(EligibilityReasonRequiredPriorPhaseIncomplete),
		"Field":   Equal("phases." + devPhaseID.String()),
		"Message": ContainSubstring("Development"),
	})))
}

func TestEligibilityServiceRejectsEnvironmentOutsideLifecycle(t *testing.T) {
	g := NewWithT(t)
	lifecycleID := uuid.New()

	result := NewEligibilityService().Explain(context.Background(), EligibilityRequest{
		ReleaseBundle: types.ReleaseBundle{ID: uuid.New(), Status: types.ReleaseBundleStatusPublished},
		Channel:       types.Channel{LifecycleID: lifecycleID},
		Lifecycle: types.Lifecycle{
			ID: lifecycleID,
			Phases: []types.LifecyclePhase{
				{ID: uuid.New(), Name: "Development", SortOrder: 10, EnvironmentIDs: []uuid.UUID{uuid.New()}},
			},
		},
		EnvironmentID: uuid.New(),
	})

	g.Expect(result.Eligible).To(BeFalse())
	g.Expect(result.TargetPhase).To(BeNil())
	g.Expect(result.Reasons).To(ContainElement(MatchFields(IgnoreExtras, Fields{
		"Code": Equal(EligibilityReasonEnvironmentNotInLifecycle),
	})))
}

func TestEligibilityServiceRejectsLifecycleMismatchAndApprovalRequired(t *testing.T) {
	g := NewWithT(t)
	targetPhaseID := uuid.New()
	environmentID := uuid.New()
	approvalPolicyID := uuid.New()

	result := NewEligibilityService().Explain(context.Background(), EligibilityRequest{
		ReleaseBundle: types.ReleaseBundle{ID: uuid.New(), Status: types.ReleaseBundleStatusPublished},
		Channel:       types.Channel{LifecycleID: uuid.New()},
		Lifecycle: types.Lifecycle{
			ID: uuid.New(),
			Phases: []types.LifecyclePhase{
				{
					ID:               targetPhaseID,
					Name:             "Production",
					SortOrder:        10,
					EnvironmentIDs:   []uuid.UUID{environmentID},
					ApprovalPolicyID: &approvalPolicyID,
				},
			},
		},
		EnvironmentID: environmentID,
	})

	g.Expect(result.Eligible).To(BeFalse())
	g.Expect(result.Reasons).To(ContainElements(
		MatchFields(IgnoreExtras, Fields{"Code": Equal(EligibilityReasonChannelLifecycleMismatch)}),
		MatchFields(IgnoreExtras, Fields{
			"Code":  Equal(EligibilityReasonApprovalRequired),
			"Field": Equal("phases." + targetPhaseID.String() + ".approvalPolicyId"),
		}),
	))
}
