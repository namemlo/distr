package mapping

import (
	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
)

func OperatorFleetPageToAPI(page types.OperatorPage[types.FleetRow]) api.OperatorFleetPage {
	return api.OperatorFleetPage{
		Items: page.Items, NextCursor: page.NextCursor, Total: page.Total,
	}
}

func OperatorReleasePageToAPI(
	page types.OperatorPage[types.OperatorReleaseRow],
) api.OperatorReleasePage {
	return api.OperatorReleasePage{
		Items: page.Items, NextCursor: page.NextCursor, Total: page.Total,
	}
}

func OperatorPlanPageToAPI(
	page types.OperatorPage[types.OperatorPlanRow],
) api.OperatorPlanPage {
	return api.OperatorPlanPage{
		Items: page.Items, NextCursor: page.NextCursor, Total: page.Total,
	}
}

func OperatorCampaignPageToAPI(
	page types.OperatorPage[types.OperatorCampaignRow],
) api.OperatorCampaignPage {
	return api.OperatorCampaignPage{
		Items: page.Items, NextCursor: page.NextCursor, Total: page.Total,
	}
}

func OperatorExecutionPageToAPI(
	page types.OperatorPage[types.OperatorExecutionRow],
) api.OperatorExecutionPage {
	return api.OperatorExecutionPage{
		Items: page.Items, NextCursor: page.NextCursor, Total: page.Total,
	}
}

func OperatorReconciliationPageToAPI(
	page types.OperatorPage[types.OperatorReconciliationRow],
) api.OperatorReconciliationPage {
	return api.OperatorReconciliationPage{
		Items: page.Items, NextCursor: page.NextCursor, Total: page.Total,
	}
}

func OperatorAuditPageToAPI(
	page types.OperatorPage[types.OperatorAuditRow],
) api.OperatorAuditPage {
	return api.OperatorAuditPage{
		Items: page.Items, NextCursor: page.NextCursor, Total: page.Total,
	}
}

func OperatorEvidenceToAPI(items []types.OperatorEvidenceRef) api.OperatorEvidencePage {
	return api.OperatorEvidencePage{Items: items}
}

func OperatorReleaseDetailToAPI(
	detail types.OperatorReleaseDetail,
) api.OperatorReleaseDetailResponse {
	return api.OperatorReleaseDetailResponse{Detail: detail}
}

func OperatorReleaseCompareToAPI(
	comparison types.OperatorReleaseCompare,
) api.OperatorReleaseCompareResponse {
	return api.OperatorReleaseCompareResponse{Comparison: comparison}
}

func OperatorPlanDetailToAPI(
	detail types.OperatorPlanDetail,
) api.OperatorPlanDetailResponse {
	return api.OperatorPlanDetailResponse{Detail: detail}
}

func OperatorPlanCompareToAPI(
	comparison types.OperatorPlanCompare,
) api.OperatorPlanCompareResponse {
	return api.OperatorPlanCompareResponse{Comparison: comparison}
}

func OperatorCampaignDetailToAPI(
	detail types.OperatorCampaignDetail,
) api.OperatorCampaignDetailResponse {
	return api.OperatorCampaignDetailResponse{Detail: detail}
}

func OperatorExecutionDetailToAPI(
	detail types.OperatorExecutionDetail,
) api.OperatorExecutionDetailResponse {
	return api.OperatorExecutionDetailResponse{Detail: detail}
}

func OperatorReconciliationDetailToAPI(
	detail types.OperatorReconciliationDetail,
) api.OperatorReconciliationDetailResponse {
	return api.OperatorReconciliationDetailResponse{Detail: detail}
}

func OperatorAuditDetailToAPI(
	detail types.OperatorAuditDetail,
) api.OperatorAuditDetailResponse {
	return api.OperatorAuditDetailResponse{Detail: detail}
}
