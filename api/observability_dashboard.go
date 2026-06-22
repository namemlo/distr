package api

import "encoding/json"

type ObservabilityDashboardListResponse struct {
	Dashboards []ObservabilityDashboard `json:"dashboards"`
}

type ObservabilityDashboard struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Category    string          `json:"category"`
	Version     string          `json:"version"`
	Template    json.RawMessage `json:"template"`
}
