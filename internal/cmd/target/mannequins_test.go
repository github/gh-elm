package target

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// mannequinsServer builds a test server that answers OrganizationID and the
// mannequins query, returning the org id and one page of mannequins.
func mannequinsServer(t *testing.T, nodesJSON string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Query string `json:"query"`
		}
		_ = json.Unmarshal(body, &req)
		switch {
		case strings.Contains(req.Query, "organization(login"):
			_, _ = io.WriteString(w, `{"data":{"organization":{"id":"ORG"}}}`)
		case strings.Contains(req.Query, "mannequins"):
			_, _ = io.WriteString(w, `{"data":{"node":{"mannequins":{"pageInfo":{"endCursor":"","hasNextPage":false},"nodes":`+nodesJSON+`}}}}`)
		default:
			t.Errorf("unexpected query: %s", req.Query)
		}
	}))
}

// runMannequin executes a mannequin subcommand built by newCmd with isolated
// config/creds.
func runMannequin(t *testing.T, newCmd func() *cobra.Command, stdin string, args ...string) (string, error) {
	t.Helper()
	t.Setenv("GH_ELM_CONFIG_DIR", t.TempDir())
	t.Setenv("GH_ELM_CREDENTIAL_STORE", "file")

	cmd := newCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestMannequinList(t *testing.T) {
	t.Run("writes CSV to stdout excluding reclaimed", func(t *testing.T) {
		srv := mannequinsServer(t, `[
			{"id":"m1","login":"alice","claimant":null},
			{"id":"m2","login":"bob","claimant":{"id":"u2","login":"bob-t"}}
		]`)
		defer srv.Close()

		out, err := runMannequin(t, newMannequinListCmd, "",
			"--github-org", "octo", "--target-url", srv.URL, "--target-token", "tok")
		if err != nil {
			t.Fatalf("list: %v\n%s", err, out)
		}
		if !strings.Contains(out, "mannequin-user,mannequin-id,target-user") {
			t.Errorf("missing header:\n%s", out)
		}
		if !strings.Contains(out, "alice,m1,") {
			t.Errorf("missing alice:\n%s", out)
		}
		if strings.Contains(out, "bob,m2") {
			t.Errorf("reclaimed bob should be excluded:\n%s", out)
		}
	})

	t.Run("writes CSV to a file with --output", func(t *testing.T) {
		srv := mannequinsServer(t, `[{"id":"m1","login":"alice","claimant":null}]`)
		defer srv.Close()

		path := filepath.Join(t.TempDir(), "mannequins.csv")
		if _, err := runMannequin(t, newMannequinListCmd, "",
			"--github-org", "octo", "--output", path,
			"--target-url", srv.URL, "--target-token", "tok"); err != nil {
			t.Fatalf("list: %v", err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read output: %v", err)
		}
		if !strings.Contains(string(data), "alice,m1,") {
			t.Errorf("output file missing alice:\n%s", data)
		}
	})

	t.Run("requires --github-org", func(t *testing.T) {
		_, err := runMannequin(t, newMannequinListCmd, "", "--target-url", "https://x", "--target-token", "tok")
		if err == nil || !strings.Contains(err.Error(), "github-org") {
			t.Fatalf("expected required-flag error, got %v", err)
		}
	})
}

func TestMannequinClaim(t *testing.T) {
	t.Run("requires csv or user+target", func(t *testing.T) {
		_, err := runMannequin(t, newMannequinClaimCmd, "",
			"--github-org", "octo", "--target-url", "https://x", "--target-token", "tok")
		if err == nil || !strings.Contains(err.Error(), "either --csv") {
			t.Fatalf("expected validation error, got %v", err)
		}
	})

	t.Run("reclaims a single mannequin via invitation", func(t *testing.T) {
		var invited bool
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			var req struct {
				Query string `json:"query"`
			}
			_ = json.Unmarshal(body, &req)
			switch {
			case strings.Contains(req.Query, "organization(login"):
				_, _ = io.WriteString(w, `{"data":{"organization":{"id":"ORG"}}}`)
			case strings.Contains(req.Query, "mannequins"):
				_, _ = io.WriteString(w, `{"data":{"node":{"mannequins":{"pageInfo":{"endCursor":"","hasNextPage":false},"nodes":[{"id":"m1","login":"alice","claimant":null}]}}}}`)
			case strings.Contains(req.Query, "user(login"):
				_, _ = io.WriteString(w, `{"data":{"user":{"id":"u1"}}}`)
			case strings.Contains(req.Query, "createAttributionInvitation"):
				invited = true
				_, _ = io.WriteString(w, `{"data":{"createAttributionInvitation":{"source":{"id":"m1","login":"alice"},"target":{"id":"u1","login":"alice-t"}}}}`)
			default:
				t.Errorf("unexpected query: %s", req.Query)
			}
		}))
		defer srv.Close()

		out, err := runMannequin(t, newMannequinClaimCmd, "",
			"--github-org", "octo", "--mannequin-user", "alice", "--target-user", "alice-t",
			"--target-url", srv.URL, "--target-token", "tok")
		if err != nil {
			t.Fatalf("claim: %v\n%s", err, out)
		}
		if !invited {
			t.Error("expected createAttributionInvitation to be called")
		}
	})

	t.Run("skip-invitation requires org admin", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/orgs/") {
				_, _ = io.WriteString(w, `{"role":"member"}`)
				return
			}
			body, _ := io.ReadAll(r.Body)
			var req struct {
				Query string `json:"query"`
			}
			_ = json.Unmarshal(body, &req)
			if strings.Contains(req.Query, "viewer") {
				_, _ = io.WriteString(w, `{"data":{"viewer":{"login":"alice"}}}`)
				return
			}
			t.Errorf("unexpected query: %s", req.Query)
		}))
		defer srv.Close()

		_, err := runMannequin(t, newMannequinClaimCmd, "",
			"--github-org", "octo", "--mannequin-user", "alice", "--target-user", "alice-t",
			"--skip-invitation", "--no-prompt",
			"--target-url", srv.URL, "--target-token", "tok")
		if err == nil || !strings.Contains(err.Error(), "not an org admin") {
			t.Fatalf("expected admin error, got %v", err)
		}
	})
}
