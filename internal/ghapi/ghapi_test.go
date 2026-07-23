package ghapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// graphQLServer starts a test server that decodes the GraphQL request and calls
// respond with the query string so the test can branch on it.
func graphQLServer(t *testing.T, respond func(query string, vars map[string]any) string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("Authorization = %q", got)
		}
		// The mannequin_claiming_emu feature must be requested so the
		// reattributeMannequinToUser mutation (--skip-invitation) exists.
		if got := r.Header.Get("GraphQL-Features"); got != "mannequin_claiming_emu" {
			t.Errorf("GraphQL-Features = %q, want mannequin_claiming_emu", got)
		}
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
			if !strings.Contains(q, "organization(login") {
				t.Errorf("unexpected query: %s", q)
			}
			return `{"data":{"organization":{"id":"ORG123"}}}`
		})
		defer srv.Close()

		id, err := NewClient(srv.URL, "tok").OrganizationID(t.Context(), "octo")
		if err != nil {
			t.Fatalf("OrganizationID: %v", err)
		}
		if id != "ORG123" {
			t.Errorf("id = %q", id)
		}
	})

	t.Run("errors when the org is missing", func(t *testing.T) {
		srv := graphQLServer(t, func(_ string, _ map[string]any) string {
			return `{"data":{"organization":null}}`
		})
		defer srv.Close()

		if _, err := NewClient(srv.URL, "tok").OrganizationID(t.Context(), "nope"); err == nil {
			t.Fatal("expected an error for a missing org")
		}
	})

	t.Run("surfaces graphql errors", func(t *testing.T) {
		srv := graphQLServer(t, func(_ string, _ map[string]any) string {
			return `{"errors":[{"message":"Something went wrong"}]}`
		})
		defer srv.Close()

		_, err := NewClient(srv.URL, "tok").OrganizationID(t.Context(), "octo")
		if err == nil {
			t.Fatal("expected an error")
		}
		var gqlErr *GraphQLError
		if !errors.As(err, &gqlErr) {
			t.Fatalf("expected *GraphQLError, got %T", err)
		}
		if !strings.Contains(gqlErr.Error(), "Something went wrong") {
			t.Errorf("error = %v", gqlErr)
		}
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
		if err != nil {
			t.Fatalf("Mannequins: %v", err)
		}
		if len(got) != 3 {
			t.Fatalf("got %d mannequins, want 3: %+v", len(got), got)
		}
		if got[0].MappedUser != nil {
			t.Error("alice should be unclaimed")
		}
		if got[1].MappedUser == nil || got[1].MappedUser.Login != "bob-target" {
			t.Errorf("bob claimant = %+v", got[1].MappedUser)
		}
	})
}

func TestOrgMembership(t *testing.T) {
	t.Run("returns role on 200", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/orgs/octo/memberships/alice" {
				t.Errorf("path = %q", r.URL.Path)
			}
			_, _ = io.WriteString(w, `{"role":"admin"}`)
		}))
		defer srv.Close()

		role, err := NewClient(srv.URL, "tok").OrgMembership(t.Context(), "octo", "alice")
		if err != nil {
			t.Fatalf("OrgMembership: %v", err)
		}
		if role != "admin" {
			t.Errorf("role = %q", role)
		}
	})

	t.Run("returns empty on 404", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		}))
		defer srv.Close()

		role, err := NewClient(srv.URL, "tok").OrgMembership(t.Context(), "octo", "ghost")
		if err != nil {
			t.Fatalf("OrgMembership: %v", err)
		}
		if role != "" {
			t.Errorf("role = %q, want empty", role)
		}
	})
}

func TestReattributeMannequinToUser(t *testing.T) {
	t.Run("returns the source/target pair", func(t *testing.T) {
		srv := graphQLServer(t, func(q string, _ map[string]any) string {
			if !strings.Contains(q, "reattributeMannequinToUser") {
				t.Errorf("unexpected query: %s", q)
			}
			return `{"data":{"reattributeMannequinToUser":{"source":{"id":"m1","login":"alice"},"target":{"id":"u1","login":"alice-target"}}}}`
		})
		defer srv.Close()

		res, err := NewClient(srv.URL, "tok").ReattributeMannequinToUser(t.Context(), "ORG", "m1", "u1")
		if err != nil {
			t.Fatalf("ReattributeMannequinToUser: %v", err)
		}
		if res.SourceID != "m1" || res.TargetID != "u1" {
			t.Errorf("result = %+v", res)
		}
	})
}
