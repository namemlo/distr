package planning

import "github.com/distr-sh/distr/internal/types"

const (
	MaxTargetPlanComponents   = 256
	MaxTargetPlanRequirements = 1024
	MaxTargetPlanSteps        = 4096
	MaxTargetPlanEdges        = 8192
	MaxTargetPlanPayloadBytes = 4 * 1024 * 1024
)

func validateTargetPlanSize(input types.PlanResolutionInput) []types.ValidationIssue {
	issues := make([]types.ValidationIssue, 0, 4)
	if len(input.ReleasePins) > MaxTargetPlanComponents {
		issues = append(issues, types.ValidationIssue{
			Code:    "plan_components_limit_exceeded",
			Field:   "productReleaseId",
			Message: "target plan exceeds the component limit",
		})
	}
	if len(input.Requirements) > MaxTargetPlanRequirements {
		issues = append(issues, types.ValidationIssue{
			Code:    "plan_requirements_limit_exceeded",
			Field:   "requirements",
			Message: "target plan exceeds the requirement limit",
		})
	}
	if len(input.ProductEdges) > MaxTargetPlanEdges {
		issues = append(issues, types.ValidationIssue{
			Code:    "plan_edges_limit_exceeded",
			Field:   "productReleaseId",
			Message: "target plan exceeds the dependency edge limit",
		})
	}
	return issues
}
