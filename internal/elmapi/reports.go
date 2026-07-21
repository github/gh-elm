package elmapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// Report stages accepted by the target reports API (wire values).
const (
	ReportStageUnknown     = "REPORT_STAGE_UNKNOWN"
	ReportStageBackfill    = "REPORT_STAGE_BACKFILL"
	ReportStageLiveUpdates = "REPORT_STAGE_LIVE_UPDATES"
)

// Report states accepted by the target reports API (wire values).
const (
	ReportStateUnknown    = "REPORT_STATE_UNKNOWN"
	ReportStateMigrated   = "REPORT_STATE_MIGRATED"
	ReportStateUnmigrated = "REPORT_STATE_UNMIGRATED"
	ReportStateAll        = "REPORT_STATE_ALL"
)

// CreateReportRequest is the body of a create-report call. Stage and State are
// wire values (see the ReportStage*/ReportState* constants).
type CreateReportRequest struct {
	Stage string `json:"stage"`
	State string `json:"state"`
}

// CreateReport requests a node report for a migration and returns the API's raw
// JSON response. The report is generated asynchronously; poll GetReportStatus
// and then call GetReportURL to download it.
// POST /enterprise/migration/:id/reports.
func (c *Client) CreateReport(ctx context.Context, migrationID int64, stage, state string) (json.RawMessage, error) {
	path := fmt.Sprintf("/enterprise/migration/%d/reports", migrationID)
	body := CreateReportRequest{Stage: stage, State: state}

	var raw json.RawMessage
	if err := c.post(ctx, path, body, &raw, http.StatusAccepted); err != nil {
		return nil, fmt.Errorf("creating report: %w", err)
	}
	return raw, nil
}

// GetReportStatus returns the API's raw JSON describing a node report's status.
// GET /enterprise/migration/:id/reports/status.
func (c *Client) GetReportStatus(ctx context.Context, migrationID int64, stage string) (json.RawMessage, error) {
	path := fmt.Sprintf("/enterprise/migration/%d/reports/status", migrationID)
	q := url.Values{}
	q.Set("stage", stage)

	var raw json.RawMessage
	if err := c.get(ctx, path, q, &raw); err != nil {
		return nil, fmt.Errorf("getting report status: %w", err)
	}
	return raw, nil
}

// GetReportURL returns the API's raw JSON containing a short-lived, read-only
// signed URL to download a finished report archive.
// GET /enterprise/migration/:id/reports/url.
func (c *Client) GetReportURL(ctx context.Context, migrationID int64, stage string) (json.RawMessage, error) {
	path := fmt.Sprintf("/enterprise/migration/%d/reports/url", migrationID)
	q := url.Values{}
	q.Set("stage", stage)

	var raw json.RawMessage
	if err := c.get(ctx, path, q, &raw); err != nil {
		return nil, fmt.Errorf("getting report URL: %w", err)
	}
	return raw, nil
}
