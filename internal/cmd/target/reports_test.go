package target

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// failWriter is an io.Writer that always fails, to prove output errors surface.
type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestRenderReportSurfacesWriterError(t *testing.T) {
	raw := json.RawMessage(`{"status":"REPORT_STATUS_FINISHED"}`)

	t.Run("human path", func(t *testing.T) {
		if err := renderReport(failWriter{}, raw, false, printReportStatus); err == nil {
			t.Fatal("expected the writer error to propagate from the human path")
		}
	})

	t.Run("json path", func(t *testing.T) {
		if err := renderReport(failWriter{}, raw, true, printReportStatus); err == nil {
			t.Fatal("expected the writer error to propagate from the --json path")
		}
	})
}

func TestReportCreate(t *testing.T) {
	t.Run("requests a report and prints human-readable confirmation", func(t *testing.T) {
		var gotStage, gotState string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body struct{ Stage, State string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			gotStage, gotState = body.Stage, body.State
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"requestedAt":"2024-01-01T00:00:00Z","alreadyInProgress":false}`))
		}))
		defer srv.Close()

		out := runReports(t, "report", "create", "--migration-id", "42",
			"--stage", "backfill", "--state", "all",
			"--target-url", srv.URL, "--target-token", "tok")

		if gotStage != "REPORT_STAGE_BACKFILL" || gotState != "REPORT_STATE_ALL" {
			t.Errorf("sent stage=%q state=%q", gotStage, gotState)
		}
		if !strings.Contains(out, "Report requested.") {
			t.Errorf("expected human confirmation, got:\n%s", out)
		}
	})

	t.Run("emits the raw API JSON with --json", func(t *testing.T) {
		const respBody = `{"requestedAt":"2024-01-01T00:00:00Z","alreadyInProgress":true}`
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(respBody))
		}))
		defer srv.Close()

		out := runReports(t, "report", "create", "--migration-id", "42", "--stage", "backfill", "--json",
			"--target-url", srv.URL, "--target-token", "tok")

		if strings.TrimSpace(out) != respBody {
			t.Errorf("output = %q, want raw API JSON %q", out, respBody)
		}
	})

	t.Run("defaults --state to all", func(t *testing.T) {
		var gotState string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body struct{ Stage, State string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			gotState = body.State
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{}`))
		}))
		defer srv.Close()

		runReports(t, "report", "create", "--migration-id", "1", "--stage", "backfill",
			"--target-url", srv.URL, "--target-token", "tok")

		if gotState != "REPORT_STATE_ALL" {
			t.Errorf("default state = %q, want REPORT_STATE_ALL", gotState)
		}
	})

	t.Run("rejects an invalid stage", func(t *testing.T) {
		err := runReportsErr(t, "report", "create", "--migration-id", "1", "--stage", "bogus",
			"--target-url", "https://x", "--target-token", "tok")
		if err == nil || !strings.Contains(err.Error(), "invalid --stage") {
			t.Fatalf("expected invalid stage error, got %v", err)
		}
	})

	t.Run("rejects an invalid state", func(t *testing.T) {
		err := runReportsErr(t, "report", "create", "--migration-id", "1", "--stage", "backfill", "--state", "bogus",
			"--target-url", "https://x", "--target-token", "tok")
		if err == nil || !strings.Contains(err.Error(), "invalid --state") {
			t.Fatalf("expected invalid state error, got %v", err)
		}
	})

	t.Run("requires --stage", func(t *testing.T) {
		err := runReportsErr(t, "report", "create", "--migration-id", "1",
			"--target-url", "https://x", "--target-token", "tok")
		if err == nil || !strings.Contains(err.Error(), "stage") {
			t.Fatalf("expected required-flag error, got %v", err)
		}
	})

	t.Run("surfaces an actionable auth error on 401", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
		}))
		defer srv.Close()

		err := runReportsErr(t, "report", "create", "--migration-id", "1", "--stage", "backfill",
			"--target-url", srv.URL, "--target-token", "bad")
		if err == nil {
			t.Fatal("expected an error on 401")
		}
		for _, want := range []string{"authentication failed", "401", "GH_TARGET_TOKEN"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q missing %q", err.Error(), want)
			}
		}
	})
}

func TestReportStatus(t *testing.T) {
	t.Run("prints human-readable status by default", func(t *testing.T) {
		var gotStage string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotStage = r.URL.Query().Get("stage")
			_, _ = w.Write([]byte(`{"status":"REPORT_STATUS_FINISHED","stage":"REPORT_STAGE_BACKFILL","files":[{"name":"nodes.jsonl","sizeBytes":"2048"}]}`))
		}))
		defer srv.Close()

		out := runReports(t, "report", "status", "--migration-id", "3", "--stage", "backfill",
			"--target-url", srv.URL, "--target-token", "tok")

		if gotStage != "REPORT_STAGE_BACKFILL" {
			t.Errorf("stage query = %q", gotStage)
		}
		if !strings.Contains(out, "Status: finished") {
			t.Errorf("expected humanized status, got:\n%s", out)
		}
		if !strings.Contains(out, "nodes.jsonl") {
			t.Errorf("expected file listing, got:\n%s", out)
		}
	})

	t.Run("emits the raw API JSON with --json", func(t *testing.T) {
		const respBody = `{"status":"REPORT_STATUS_FINISHED","stage":"REPORT_STAGE_BACKFILL"}`
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(respBody))
		}))
		defer srv.Close()

		out := runReports(t, "report", "status", "--migration-id", "3", "--stage", "backfill", "--json",
			"--target-url", srv.URL, "--target-token", "tok")

		if strings.TrimSpace(out) != respBody {
			t.Errorf("output = %q, want raw API JSON %q", out, respBody)
		}
	})

	t.Run("requires --migration-id", func(t *testing.T) {
		err := runReportsErr(t, "report", "status", "--stage", "backfill",
			"--target-url", "https://x", "--target-token", "tok")
		if err == nil || !strings.Contains(err.Error(), "migration-id") {
			t.Fatalf("expected required-flag error, got %v", err)
		}
	})
}

func TestReportURL(t *testing.T) {
	t.Run("prints the URL and expiry by default", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"url":"https://blob.example/report.zip?sig=abc","expiresAt":"2024-01-01T01:00:00Z"}`))
		}))
		defer srv.Close()

		out := runReports(t, "report", "url", "--migration-id", "3", "--stage", "backfill",
			"--target-url", srv.URL, "--target-token", "tok")

		if !strings.Contains(out, "https://blob.example/report.zip?sig=abc") {
			t.Errorf("expected URL, got:\n%s", out)
		}
		if !strings.Contains(out, "Expires at:") {
			t.Errorf("expected expiry line, got:\n%s", out)
		}
	})

	t.Run("emits the raw API JSON with --json", func(t *testing.T) {
		const respBody = `{"url":"https://blob.example/report.zip?sig=abc","expiresAt":"2024-01-01T01:00:00Z"}`
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(respBody))
		}))
		defer srv.Close()

		out := runReports(t, "report", "url", "--migration-id", "3", "--stage", "backfill", "--json",
			"--target-url", srv.URL, "--target-token", "tok")

		if strings.TrimSpace(out) != respBody {
			t.Errorf("output = %q, want raw API JSON %q", out, respBody)
		}
	})
}

// runReports executes a `target` subcommand and returns output, failing on error.
func runReports(t *testing.T, args ...string) string {
	t.Helper()
	out, err := execReports(t, args...)
	if err != nil {
		t.Fatalf("target %v: %v\noutput:\n%s", args, err, out)
	}
	return out
}

// runReportsErr executes a `target` subcommand and returns the error (if any).
func runReportsErr(t *testing.T, args ...string) error {
	t.Helper()
	_, err := execReports(t, args...)
	return err
}

func execReports(t *testing.T, args ...string) (string, error) {
	t.Helper()
	t.Setenv("GH_ELM_CONFIG_DIR", t.TempDir())
	t.Setenv("GH_ELM_CREDENTIAL_STORE", "file")

	cmd := NewCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)

	err := cmd.Execute()
	return buf.String(), err
}
