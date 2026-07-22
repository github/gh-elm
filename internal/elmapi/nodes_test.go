package elmapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestListMigrationNodes(t *testing.T) {
	t.Run("sends filters and decodes a page", func(t *testing.T) {
		var gotPath, gotQuery, gotAuth, gotAccept string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			gotQuery = r.URL.RawQuery
			gotAuth = r.Header.Get("Authorization")
			gotAccept = r.Header.Get("Accept")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"nodes":[{"id":"n1","type":"issue","origin":"NODE_ORIGIN_BACKFILL","state":"NODE_STATE_PENDING"}],"after":""}`))
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "tok")
		resp, err := c.ListMigrationNodes(t.Context(), 42, ListNodesOptions{
			RepositoryNWO: "octo/repo",
			Origin:        OriginBackfill,
			State:         StatePending,
			PageSize:      100,
			After:         "cursor",
		})
		if err != nil {
			t.Fatalf("ListMigrationNodes: %v", err)
		}

		if gotPath != "/enterprise/migration/42/nodes" {
			t.Errorf("path = %q", gotPath)
		}
		if gotAuth != "Bearer tok" {
			t.Errorf("Authorization = %q", gotAuth)
		}
		if gotAccept != acceptHeader {
			t.Errorf("Accept = %q", gotAccept)
		}
		for _, want := range []string{"repository_nwo=octo%2Frepo", "origin=NODE_ORIGIN_BACKFILL", "state=NODE_STATE_PENDING", "page_size=100", "after=cursor"} {
			if !containsQuery(gotQuery, want) {
				t.Errorf("query %q missing %q", gotQuery, want)
			}
		}
		if len(resp.Nodes) != 1 || resp.Nodes[0].ID != "n1" {
			t.Fatalf("unexpected nodes: %+v", resp.Nodes)
		}
	})

	t.Run("omits empty filters", func(t *testing.T) {
		var gotQuery string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotQuery = r.URL.RawQuery
			_, _ = w.Write([]byte(`{"nodes":[],"after":""}`))
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "tok")
		if _, err := c.ListMigrationNodes(t.Context(), 1, ListNodesOptions{}); err != nil {
			t.Fatalf("ListMigrationNodes: %v", err)
		}
		if gotQuery != "" {
			t.Errorf("expected empty query, got %q", gotQuery)
		}
	})

	t.Run("returns HTTPError on non-200", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "boom", http.StatusUnprocessableEntity)
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "tok")
		_, err := c.ListMigrationNodes(t.Context(), 1, ListNodesOptions{})
		if err == nil {
			t.Fatal("expected error")
		}
		var httpErr *HTTPError
		if !errors.As(err, &httpErr) {
			t.Fatalf("expected *HTTPError, got %T", err)
		}
		if httpErr.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("StatusCode = %d", httpErr.StatusCode)
		}
	})
}

func TestIterNodes(t *testing.T) {
	t.Run("follows pagination across pages", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Query().Get("after") {
			case "":
				_, _ = w.Write([]byte(`{"nodes":[{"id":"n1"},{"id":"n2"}],"after":"page2"}`))
			case "page2":
				_, _ = w.Write([]byte(`{"nodes":[{"id":"n3"}],"after":""}`))
			default:
				t.Errorf("unexpected after cursor %q", r.URL.Query().Get("after"))
			}
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "tok")
		var ids []string
		for node, err := range c.IterNodes(t.Context(), 7, ListNodesOptions{}) {
			if err != nil {
				t.Fatalf("iter: %v", err)
			}
			ids = append(ids, node.ID)
		}
		if want := []string{"n1", "n2", "n3"}; !equal(ids, want) {
			t.Errorf("ids = %v, want %v", ids, want)
		}
	})

	t.Run("stops early when caller breaks", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"nodes":[{"id":"n1"},{"id":"n2"}],"after":"more"}`))
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "tok")
		count := 0
		for _, err := range c.IterNodes(t.Context(), 7, ListNodesOptions{}) {
			if err != nil {
				t.Fatalf("iter: %v", err)
			}
			count++
			break
		}
		if count != 1 {
			t.Errorf("count = %d, want 1", count)
		}
	})

	t.Run("surfaces errors", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "nope", http.StatusInternalServerError)
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "tok")
		var gotErr error
		for _, err := range c.IterNodes(t.Context(), 7, ListNodesOptions{}) {
			gotErr = err
		}
		if gotErr == nil {
			t.Fatal("expected an error from iteration")
		}
	})

	t.Run("follows the cursor past an empty page to later matching nodes", func(t *testing.T) {
		// A migration/repo filter can return an early page with no matching
		// nodes but a non-empty cursor pointing at later pages that do match.
		// Iteration must keep following the cursor rather than stop early.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Query().Get("after") {
			case "":
				_, _ = w.Write([]byte(`{"nodes":[],"after":"page2"}`))
			case "page2":
				_, _ = w.Write([]byte(`{"nodes":[{"id":"n1"}],"after":""}`))
			default:
				t.Errorf("unexpected after cursor %q", r.URL.Query().Get("after"))
			}
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "tok")
		var ids []string
		for node, err := range c.IterNodes(t.Context(), 7, ListNodesOptions{}) {
			if err != nil {
				t.Fatalf("iter: %v", err)
			}
			ids = append(ids, node.ID)
		}
		if want := []string{"n1"}; !equal(ids, want) {
			t.Errorf("ids = %v, want %v", ids, want)
		}
	})

	t.Run("stops when the API repeats a cursor to prevent a loop", func(t *testing.T) {
		// A misbehaving filter that returns the same cursor forever must not
		// spin the loop indefinitely; a already-seen cursor ends iteration.
		calls := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls++
			if calls > 5 {
				t.Fatalf("IterNodes did not stop on a repeated cursor (called %d times)", calls)
			}
			_, _ = w.Write([]byte(`{"nodes":[],"after":"never-ending"}`))
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "tok")
		count := 0
		for range c.IterNodes(t.Context(), 7, ListNodesOptions{}) {
			count++
		}
		if count != 0 {
			t.Errorf("count = %d, want 0", count)
		}
		// One request advances to "never-ending"; the second sees it repeated
		// and stops.
		if calls != 2 {
			t.Errorf("expected exactly 2 requests, got %d", calls)
		}
	})
}

func containsQuery(raw, want string) bool {
	return slices.Contains(splitAmp(raw), want)
}

func splitAmp(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '&' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start <= len(s) {
		out = append(out, s[start:])
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
