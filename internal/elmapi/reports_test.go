package elmapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		require.NoError(t, err, "CreateReport")

		assert.Equal(t, http.MethodPost, gotMethod)
		assert.Equal(t, "/enterprise/migration/42/reports", gotPath)
		assert.Equal(t, "Bearer tok", gotAuth)
		assert.Equal(t, "application/json", gotContentType)
		assert.Equal(t, ReportStageBackfill, gotBody.Stage)
		assert.Equal(t, ReportStateAll, gotBody.State)
		assert.Equal(t, respBody, string(raw)) //nolint:testifylint // encoded-compare
	})

	t.Run("returns HTTPError when the API rejects a non-202", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "boom", http.StatusBadRequest)
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "tok")
		_, err := c.CreateReport(t.Context(), 1, ReportStageBackfill, ReportStateAll)
		require.Error(t, err)
		var httpErr *HTTPError
		require.ErrorAs(t, err, &httpErr, "expected *HTTPError")
		assert.Equal(t, http.StatusBadRequest, httpErr.StatusCode)
	})

	t.Run("treats a 200 as an error since the API returns 202", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{}`))
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "tok")
		_, err := c.CreateReport(t.Context(), 1, ReportStageBackfill, ReportStateAll)
		assert.Error(t, err, "expected error on unexpected 200")
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
		require.NoError(t, err, "GetReportStatus")
		assert.Equal(t, "/enterprise/migration/7/reports/status", gotPath)
		assert.Equal(t, ReportStageBackfill, gotStage)
		assert.Equal(t, respBody, string(raw)) //nolint:testifylint // encoded-compare
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
		require.NoError(t, err, "GetReportURL")
		assert.Equal(t, "/enterprise/migration/9/reports/url", gotPath)
		assert.Equal(t, ReportStageLiveUpdates, gotStage)
		assert.Equal(t, respBody, string(raw)) //nolint:testifylint // encoded-compare
	})
}
