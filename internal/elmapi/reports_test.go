package elmapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateReport(t *testing.T) {
	t.Run("posts the stage/state body and returns the raw 202 response", func(t *testing.T) {
		var gotPath, gotMethod, gotAuth, gotContentType string
		var gotBody CreateReportRequest
		const respBody = `{"requestedAt":"2024-01-01T00:00:00Z","alreadyInProgress":true}`
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			gotMethod = r.Method
			gotAuth = r.Header.Get("Authorization")
			gotContentType = r.Header.Get("Content-Type")
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(respBody))
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "tok")
		raw, err := c.CreateReport(t.Context(), 42, ReportStageBackfill, ReportStateAll)
		if err != nil {
			t.Fatalf("CreateReport: %v", err)
		}

		if gotMethod != http.MethodPost {
			t.Errorf("method = %q", gotMethod)
		}
		if gotPath != "/enterprise/migration/42/reports" {
			t.Errorf("path = %q", gotPath)
		}
		if gotAuth != "Bearer tok" {
			t.Errorf("Authorization = %q", gotAuth)
		}
		if gotContentType != "application/json" {
			t.Errorf("Content-Type = %q", gotContentType)
		}
		if gotBody.Stage != ReportStageBackfill || gotBody.State != ReportStateAll {
			t.Errorf("body = %+v", gotBody)
		}
		if string(raw) != respBody {
			t.Errorf("raw = %s, want %s", raw, respBody)
		}
	})

	t.Run("returns HTTPError when the API rejects a non-202", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "boom", http.StatusBadRequest)
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "tok")
		_, err := c.CreateReport(t.Context(), 1, ReportStageBackfill, ReportStateAll)
		if err == nil {
			t.Fatal("expected error")
		}
		var httpErr *HTTPError
		if !errors.As(err, &httpErr) {
			t.Fatalf("expected *HTTPError, got %T", err)
		}
		if httpErr.StatusCode != http.StatusBadRequest {
			t.Errorf("StatusCode = %d", httpErr.StatusCode)
		}
	})

	t.Run("treats a 200 as an error since the API returns 202", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{}`))
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "tok")
		if _, err := c.CreateReport(t.Context(), 1, ReportStageBackfill, ReportStateAll); err == nil {
			t.Fatal("expected error on unexpected 200")
		}
	})
}

func TestGetReportStatus(t *testing.T) {
	t.Run("sends the stage query and returns the raw response", func(t *testing.T) {
		var gotPath, gotStage string
		const respBody = `{"status":"REPORT_STATUS_FINISHED","totalSizeBytes":"1024","stage":"REPORT_STAGE_BACKFILL","state":"REPORT_STATE_ALL","files":[{"name":"nodes.jsonl","sizeBytes":"2048"}]}`
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			gotStage = r.URL.Query().Get("stage")
			_, _ = w.Write([]byte(respBody))
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "tok")
		raw, err := c.GetReportStatus(t.Context(), 7, ReportStageBackfill)
		if err != nil {
			t.Fatalf("GetReportStatus: %v", err)
		}
		if gotPath != "/enterprise/migration/7/reports/status" {
			t.Errorf("path = %q", gotPath)
		}
		if gotStage != ReportStageBackfill {
			t.Errorf("stage = %q", gotStage)
		}
		if string(raw) != respBody {
			t.Errorf("raw = %s, want %s", raw, respBody)
		}
	})
}

func TestGetReportURL(t *testing.T) {
	t.Run("sends the stage query and returns the raw response", func(t *testing.T) {
		var gotPath, gotStage string
		const respBody = `{"url":"https://blob.example/report.zip?sig=abc","expiresAt":"2024-01-01T01:00:00Z"}`
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			gotStage = r.URL.Query().Get("stage")
			_, _ = w.Write([]byte(respBody))
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "tok")
		raw, err := c.GetReportURL(t.Context(), 9, ReportStageLiveUpdates)
		if err != nil {
			t.Fatalf("GetReportURL: %v", err)
		}
		if gotPath != "/enterprise/migration/9/reports/url" {
			t.Errorf("path = %q", gotPath)
		}
		if gotStage != ReportStageLiveUpdates {
			t.Errorf("stage = %q", gotStage)
		}
		if string(raw) != respBody {
			t.Errorf("raw = %s, want %s", raw, respBody)
		}
	})
}
