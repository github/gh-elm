package ghapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// graphQLServer starts a test server that decodes the GraphQL request and calls
// respond with the query string so the test can branch on it.
func graphQLServer(t *testing.T, respond func(query string, vars map[string]any) string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/graphql", r.URL.Path)
		assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
		// The mannequin_claiming_emu and mannequin_claiming_bot features must be
		// requested so the reattributeMannequinToUser (--skip-invitation) and
		// reattributeMannequinToBot mutations exist.
		assert.Equal(t, "mannequin_claiming_emu,mannequin_claiming_bot", r.Header.Get("GraphQL-Features"),
			"GraphQL-Features header must request mannequin_claiming_emu,mannequin_claiming_bot")
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.Unmarshal(body, &req)
		_, _ = io.WriteString(w, respond(req.Query, req.Variables))
	}))
}

func TestOrganizationID(t *testing.T) {
	t.Run("returns the org node id", func(t *testing.T) {
		srv := graphQLServer(t, func(q string, _ map[string]any) string {
			assert.Contains(t, q, "organization(login")
			return `{"data":{"organization":{"id":"ORG123"}}}`
		})
		defer srv.Close()

		id, err := NewClient(srv.URL, "tok").OrganizationID(t.Context(), "octo")
		require.NoError(t, err, "OrganizationID")
		assert.Equal(t, "ORG123", id)
	})

	t.Run("errors when the org is missing", func(t *testing.T) {
		srv := graphQLServer(t, func(_ string, _ map[string]any) string {
			return `{"data":{"organization":null}}`
		})
		defer srv.Close()

		_, err := NewClient(srv.URL, "tok").OrganizationID(t.Context(), "nope")
		assert.Error(t, err, "expected an error for a missing org")
	})

	t.Run("surfaces graphql errors", func(t *testing.T) {
		srv := graphQLServer(t, func(_ string, _ map[string]any) string {
			return `{"errors":[{"message":"Something went wrong"}]}`
		})
		defer srv.Close()

		_, err := NewClient(srv.URL, "tok").OrganizationID(t.Context(), "octo")
		require.Error(t, err)
		var gqlErr *GraphQLError
		require.ErrorAs(t, err, &gqlErr, "expected *GraphQLError")
		assert.Contains(t, gqlErr.Error(), "Something went wrong")
	})
}

func TestMannequins(t *testing.T) {
	t.Run("follows pagination and maps claimants", func(t *testing.T) {
		srv := graphQLServer(t, func(_ string, vars map[string]any) string {
			after, _ := vars["after"].(string)
			if after == "" {
				return `{"data":{"node":{"mannequins":{
					"pageInfo":{"endCursor":"p2","hasNextPage":true},
					"nodes":[
						{"id":"m1","login":"alice","claimant":null},
						{"id":"m2","login":"bob","claimant":{"id":"u2","login":"bob-target"}}
					]}}}}`
			}
			return `{"data":{"node":{"mannequins":{
				"pageInfo":{"endCursor":"","hasNextPage":false},
				"nodes":[{"id":"m3","login":"carol","claimant":null}]}}}}`
		})
		defer srv.Close()

		got, err := NewClient(srv.URL, "tok").Mannequins(t.Context(), "ORG")
		require.NoError(t, err, "Mannequins")
		require.Len(t, got, 3)
		assert.Nil(t, got[0].MappedUser, "alice should be unclaimed")
		require.NotNil(t, got[1].MappedUser)
		assert.Equal(t, "bob-target", got[1].MappedUser.Login)
	})
}

func TestOrgMembership(t *testing.T) {
	t.Run("returns role on 200", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/orgs/octo/memberships/alice", r.URL.Path)
			_, _ = io.WriteString(w, `{"role":"admin"}`)
		}))
		defer srv.Close()

		role, err := NewClient(srv.URL, "tok").OrgMembership(t.Context(), "octo", "alice")
		require.NoError(t, err, "OrgMembership")
		assert.Equal(t, "admin", role)
	})

	t.Run("returns empty on 404", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		}))
		defer srv.Close()

		role, err := NewClient(srv.URL, "tok").OrgMembership(t.Context(), "octo", "ghost")
		require.NoError(t, err, "OrgMembership")
		assert.Empty(t, role)
	})
}

func TestReattributeMannequinToUser(t *testing.T) {
	t.Run("returns the source/target pair", func(t *testing.T) {
		srv := graphQLServer(t, func(q string, _ map[string]any) string {
			assert.Contains(t, q, "reattributeMannequinToUser", "unexpected query")
			return `{"data":{"reattributeMannequinToUser":{"source":{"id":"m1","login":"alice"},"target":{"id":"u1","login":"alice-target"}}}}`
		})
		defer srv.Close()

		res, err := NewClient(srv.URL, "tok").ReattributeMannequinToUser(t.Context(), "ORG", "m1", "u1")
		require.NoError(t, err, "ReattributeMannequinToUser")
		assert.Equal(t, "m1", res.SourceID)
		assert.Equal(t, "u1", res.TargetID)
	})
}

func TestBotID(t *testing.T) {
	t.Run("returns the node id for a bot", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.True(t, strings.HasPrefix(r.URL.Path, "/users/"), "path = %q", r.URL.Path)
			_, _ = io.WriteString(w, `{"type":"Bot","node_id":"BOT_kgDNAbc"}`)
		}))
		defer srv.Close()

		id, err := NewClient(srv.URL, "tok").BotID(t.Context(), "example-ci[bot]")
		require.NoError(t, err, "BotID")
		assert.Equal(t, "BOT_kgDNAbc", id)
	})

	t.Run("errors when the account is not a bot", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"type":"User","node_id":"U_abc"}`)
		}))
		defer srv.Close()

		_, err := NewClient(srv.URL, "tok").BotID(t.Context(), "mona")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not a GitHub App / bot account")
	})

	t.Run("returns ErrUserNotFound on 404", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		}))
		defer srv.Close()

		_, err := NewClient(srv.URL, "tok").BotID(t.Context(), "ghost[bot]")
		require.ErrorIs(t, err, ErrUserNotFound)
	})
}

func TestReattributeMannequinToBot(t *testing.T) {
	t.Run("returns the source/target pair", func(t *testing.T) {
		srv := graphQLServer(t, func(q string, _ map[string]any) string {
			assert.Contains(t, q, "reattributeMannequinToBot", "unexpected query")
			return `{"data":{"reattributeMannequinToBot":{"source":{"id":"m1","login":"alice"},"target":{"id":"b1","login":"example-ci[bot]"}}}}`
		})
		defer srv.Close()

		res, err := NewClient(srv.URL, "tok").ReattributeMannequinToBot(t.Context(), "ORG", "m1", "b1")
		require.NoError(t, err, "ReattributeMannequinToBot")
		assert.Equal(t, "m1", res.SourceID)
		assert.Equal(t, "b1", res.TargetID)
	})
}
