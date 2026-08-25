package target

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mannequinsServer builds a test server that answers OrganizationID and the
// mannequins query, returning the org id and one page of mannequins.
func mannequinsServer(t *testing.T, nodesJSON string) *httptest.Server {
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
			assert.Failf(t, "unexpected query", "%s", req.Query)
		}
	}))
}

// runMannequin executes a mannequin subcommand built by newCmd with isolated
// config/creds.
func runMannequin(t *testing.T, newCmd func() *cobra.Command, stdin string, args ...string) (string, error) {
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
			"octo", "--target-url", srv.URL, "--target-token", "tok")
		require.NoErrorf(t, err, "list output:\n%s", out)
		assert.Contains(t, out, "mannequin-user,mannequin-id,target-user", "missing header")
		assert.Contains(t, out, "alice,m1,", "missing alice")
		assert.NotContains(t, out, "bob,m2", "reclaimed bob should be excluded")
	})

	t.Run("writes CSV to a file with --output", func(t *testing.T) {
		srv := mannequinsServer(t, `[{"id":"m1","login":"alice","claimant":null}]`)
		defer srv.Close()

		path := filepath.Join(t.TempDir(), "mannequins.csv")
		out, err := runMannequin(t, newMannequinListCmd, "",
			"--github-org", "octo", "--output", path,
			"--target-url", srv.URL, "--target-token", "tok")
		require.NoError(t, err, "list")
		assert.Contains(t, out, "✓ Wrote CSV to "+path+".")
		data, err := os.ReadFile(path)
		require.NoError(t, err, "read output")
		assert.Contains(t, string(data), "alice,m1,", "output file missing alice")
	})

	t.Run("requires an organization", func(t *testing.T) {
		_, err := runMannequin(t, newMannequinListCmd, "", "--target-url", "https://x", "--target-token", "tok")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ORGANIZATION")
	})
}

func TestMannequinReclaim(t *testing.T) {
	t.Run("requires mannequin and target user without csv", func(t *testing.T) {
		_, err := runMannequin(t, newMannequinReclaimCmd, "",
			"octo", "--target-url", "https://x", "--target-token", "tok")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "MANNEQUIN is required")
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
				assert.Failf(t, "unexpected query", "%s", req.Query)
			}
		}))
		defer srv.Close()

		out, err := runMannequin(t, newMannequinReclaimCmd, "",
			"octo", "alice", "alice-t",
			"--target-url", srv.URL, "--target-token", "tok")
		require.NoErrorf(t, err, "claim output:\n%s", out)
		assert.True(t, invited, "expected createAttributionInvitation to be called")
		assert.Contains(t, out, "✓ Mannequin reclaim invitation email successfully sent")
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
			assert.Failf(t, "unexpected query", "%s", req.Query)
		}))
		defer srv.Close()

		_, err := runMannequin(t, newMannequinReclaimCmd, "",
			"octo", "alice", "alice-t",
			"--skip-invitation", "--no-prompt",
			"--target-url", srv.URL, "--target-token", "tok")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not an org admin")
	})

	t.Run("skip-invitation aborts when the admin declines the prompt", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/orgs/") {
				_, _ = io.WriteString(w, `{"role":"admin"}`)
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
			assert.Failf(t, "unexpected query (should abort before any reclaim)", "%s", req.Query)
		}))
		defer srv.Close()

		// Admin passes the eligibility check, but declines the confirmation
		// prompt ("n"); the command must abort before any reclaim call.
		_, err := runMannequin(t, newMannequinReclaimCmd, "n\n",
			"--github-org", "octo", "--mannequin-user", "alice", "--target-user", "alice-t",
			"--skip-invitation",
			"--target-url", srv.URL, "--target-token", "tok")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "aborted")
	})
	t.Run("accepts the legacy claim command and flags", func(t *testing.T) {
		_, err := runMannequin(t, newMannequinClaimCmd, "",
			"--github-org", "octo", "--target-url", "https://x", "--target-token", "tok")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "MANNEQUIN is required")
		assert.NotContains(t, err.Error(), "unknown command")
	})

	t.Run("rejects positional and flag organizations together", func(t *testing.T) {
		_, err := runMannequin(t, newMannequinReclaimCmd, "",
			"octo", "alice", "alice-t", "--org", "other")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicates a value already supplied by flag")
	})

	t.Run("mixes positional users with compatibility flags", func(t *testing.T) {
		_, err := runMannequin(t, newMannequinReclaimCmd, "", "--org", "octo", "alice", "alice-t")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no target URL configured")
		assert.NotContains(t, err.Error(), "duplicates")

		_, err = runMannequin(t, newMannequinReclaimCmd, "", "octo", "--mannequin-user", "alice", "alice-t")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no target URL configured")
		assert.NotContains(t, err.Error(), "duplicates")
	})

	t.Run("reclaims to a bot after confirmation", func(t *testing.T) {
		srv, called := botClaimServer(t, "legacy-ci[bot]")
		defer srv.Close()

		out, err := runMannequin(t, newMannequinReclaimCmd, "y\n",
			"octo", "legacy-ci[bot]", "example-ci[bot]",
			"--target-url", srv.URL, "--target-token", "tok")
		require.NoErrorf(t, err, "claim output:\n%s", out)
		assert.True(t, *called, "expected reattributeMannequinToBot to be called")
	})

	t.Run("aborts a bot reclaim when the admin declines the prompt", func(t *testing.T) {
		srv, called := botClaimServer(t, "legacy-ci[bot]")
		defer srv.Close()

		_, err := runMannequin(t, newMannequinReclaimCmd, "n\n",
			"octo", "legacy-ci[bot]", "example-ci[bot]",
			"--target-url", srv.URL, "--target-token", "tok")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "aborted")
		assert.False(t, *called, "no reclaim should occur after declining")
	})

	t.Run("bot reclaim with --no-prompt proceeds without confirmation", func(t *testing.T) {
		srv, called := botClaimServer(t, "legacy-ci[bot]")
		defer srv.Close()

		out, err := runMannequin(t, newMannequinReclaimCmd, "",
			"octo", "legacy-ci[bot]", "example-ci[bot]",
			"--no-prompt", "--target-url", srv.URL, "--target-token", "tok")
		require.NoErrorf(t, err, "claim output:\n%s", out)
		assert.True(t, *called, "expected reattributeMannequinToBot to be called")
	})

	t.Run("warns when the source mannequin does not look like a bot", func(t *testing.T) {
		srv, called := botClaimServer(t, "alice")
		defer srv.Close()

		out, err := runMannequin(t, newMannequinReclaimCmd, "y\n",
			"octo", "alice", "example-ci[bot]",
			"--target-url", srv.URL, "--target-token", "tok")
		require.NoErrorf(t, err, "claim output:\n%s", out)
		assert.True(t, *called, "expected reattributeMannequinToBot to be called")
		assert.Contains(t, out, "does not look like a bot mannequin", "expected soft-signal advisory warning")
	})

	t.Run("reclaims a bot row from a CSV with mixed targets", func(t *testing.T) {
		srv, botCalled, invited := botCSVServer(t)
		defer srv.Close()

		path := filepath.Join(t.TempDir(), "mannequins.csv")
		csv := "mannequin-user,mannequin-id,target-user\n" +
			"legacy-ci[bot],m1,example-ci[bot]\n" +
			"alice,m2,alice-t\n"
		require.NoError(t, os.WriteFile(path, []byte(csv), 0o600))

		out, err := runMannequin(t, newMannequinReclaimCmd, "y\n",
			"octo", "--csv", path,
			"--target-url", srv.URL, "--target-token", "tok")
		require.NoErrorf(t, err, "claim output:\n%s", out)
		assert.True(t, *botCalled, "expected reattributeMannequinToBot to be called for the bot row")
		assert.True(t, *invited, "expected createAttributionInvitation to be called for the human row")
	})
}

// botCSVServer answers the org id, all-mannequins listing, REST bot lookup, user
// lookup, and both the reattributeMannequinToBot and createAttributionInvitation
// calls made by a mixed CSV reclaim (bot "example-ci[bot]" plus human "alice-t").
// The returned bools are set when the bot mutation and the invitation are
// invoked, respectively.
func botCSVServer(t *testing.T) (srv *httptest.Server, botCalled, invited *bool) {
	botCalled = new(bool)
	invited = new(bool)
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/users/") {
			_, _ = io.WriteString(w, `{"type":"Bot","node_id":"BOT1"}`)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Query string `json:"query"`
		}
		_ = json.Unmarshal(body, &req)
		switch {
		case strings.Contains(req.Query, "organization(login"):
			_, _ = io.WriteString(w, `{"data":{"organization":{"id":"ORG"}}}`)
		case strings.Contains(req.Query, "mannequins"):
			_, _ = io.WriteString(w, `{"data":{"node":{"mannequins":{"pageInfo":{"endCursor":"","hasNextPage":false},"nodes":[{"id":"m1","login":"legacy-ci[bot]","claimant":null},{"id":"m2","login":"alice","claimant":null}]}}}}`)
		case strings.Contains(req.Query, "user(login"):
			_, _ = io.WriteString(w, `{"data":{"user":{"id":"u2"}}}`)
		case strings.Contains(req.Query, "reattributeMannequinToBot"):
			*botCalled = true
			_, _ = io.WriteString(w, `{"data":{"reattributeMannequinToBot":{"source":{"id":"m1","login":"legacy-ci[bot]"},"target":{"id":"BOT1","login":"example-ci[bot]"}}}}`)
		case strings.Contains(req.Query, "createAttributionInvitation"):
			*invited = true
			_, _ = io.WriteString(w, `{"data":{"createAttributionInvitation":{"source":{"id":"m2","login":"alice"},"target":{"id":"u2","login":"alice-t"}}}}`)
		default:
			assert.Failf(t, "unexpected query", "%s", req.Query)
		}
	}))
	return srv, botCalled, invited
}

// botClaimServer answers the org id, mannequins-by-login, REST bot lookup, and
// reattributeMannequinToBot calls made by a bot reclaim to target
// "example-ci[bot]". The returned bool is set to true when the bot mutation is
// invoked.
func botClaimServer(t *testing.T, mannequinLogin string) (*httptest.Server, *bool) {
	const (
		mannequinID = "m1"
		botLogin    = "example-ci[bot]"
		botNodeID   = "BOT1"
	)
	called := new(bool)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/users/") {
			_, _ = fmt.Fprintf(w, `{"type":"Bot","node_id":%q}`, botNodeID)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Query string `json:"query"`
		}
		_ = json.Unmarshal(body, &req)
		switch {
		case strings.Contains(req.Query, "organization(login"):
			_, _ = io.WriteString(w, `{"data":{"organization":{"id":"ORG"}}}`)
		case strings.Contains(req.Query, "mannequins"):
			_, _ = fmt.Fprintf(w, `{"data":{"node":{"mannequins":{"pageInfo":{"endCursor":"","hasNextPage":false},"nodes":[{"id":%q,"login":%q,"claimant":null}]}}}}`, mannequinID, mannequinLogin)
		case strings.Contains(req.Query, "reattributeMannequinToBot"):
			*called = true
			_, _ = fmt.Fprintf(w, `{"data":{"reattributeMannequinToBot":{"source":{"id":%q,"login":%q},"target":{"id":%q,"login":%q}}}}`, mannequinID, mannequinLogin, botNodeID, botLogin)
		default:
			assert.Failf(t, "unexpected query", "%s", req.Query)
		}
	}))
	return srv, called
}
