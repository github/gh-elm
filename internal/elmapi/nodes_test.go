package elmapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		require.NoError(t, err, "ListMigrationNodes")

		assert.Equal(t, "/enterprise/migration/42/nodes", gotPath)
		assert.Equal(t, "Bearer tok", gotAuth)
		assert.Equal(t, acceptHeader, gotAccept)
		parsed, err := url.ParseQuery(gotQuery)
		require.NoError(t, err, "parse query")
		assert.Equal(t, "octo/repo", parsed.Get("repository_nwo"))
		assert.Equal(t, "NODE_ORIGIN_BACKFILL", parsed.Get("origin"))
		assert.Equal(t, "NODE_STATE_PENDING", parsed.Get("state"))
		assert.Equal(t, "100", parsed.Get("page_size"))
		assert.Equal(t, "cursor", parsed.Get("after"))
		require.Len(t, resp.Nodes, 1)
		assert.Equal(t, "n1", resp.Nodes[0].ID)
	})

	t.Run("omits empty filters", func(t *testing.T) {
		var gotQuery string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotQuery = r.URL.RawQuery
			_, _ = w.Write([]byte(`{"nodes":[],"after":""}`))
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "tok")
		_, err := c.ListMigrationNodes(t.Context(), 1, ListNodesOptions{})
		require.NoError(t, err, "ListMigrationNodes")
		assert.Empty(t, gotQuery, "expected empty query")
	})

	t.Run("returns HTTPError on non-200", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "boom", http.StatusUnprocessableEntity)
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "tok")
		_, err := c.ListMigrationNodes(t.Context(), 1, ListNodesOptions{})
		require.Error(t, err)
		var httpErr *HTTPError
		require.ErrorAs(t, err, &httpErr)
		assert.Equal(t, http.StatusUnprocessableEntity, httpErr.StatusCode)
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
			require.NoError(t, err, "iter")
			ids = append(ids, node.ID)
		}
		assert.Equal(t, []string{"n1", "n2", "n3"}, ids)
	})

	t.Run("stops early when caller breaks", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"nodes":[{"id":"n1"},{"id":"n2"}],"after":"more"}`))
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "tok")
		count := 0
		for _, err := range c.IterNodes(t.Context(), 7, ListNodesOptions{}) {
			require.NoError(t, err, "iter")
			count++
			break
		}
		assert.Equal(t, 1, count)
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
		assert.Error(t, gotErr, "expected an error from iteration")
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
			require.NoError(t, err, "iter")
			ids = append(ids, node.ID)
		}
		assert.Equal(t, []string{"n1"}, ids)
	})

	t.Run("stops when the API repeats a cursor to prevent a loop", func(t *testing.T) {
		// A misbehaving filter that returns the same cursor forever must not
		// spin the loop indefinitely; a already-seen cursor ends iteration.
		calls := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls++
			if calls > 5 {
				// Break the loop deterministically if the repeated-cursor guard
				// regresses; otherwise the iterator would spin forever and the
				// test would hang instead of failing.
				http.Error(w, "IterNodes did not stop on a repeated cursor", http.StatusInternalServerError)
				return
			}
			_, _ = w.Write([]byte(`{"nodes":[],"after":"never-ending"}`))
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "tok")
		count := 0
		for range c.IterNodes(t.Context(), 7, ListNodesOptions{}) {
			count++
		}
		assert.Equal(t, 0, count)
		// One request advances to "never-ending"; the second sees it repeated
		// and stops.
		assert.Equal(t, 2, calls, "expected exactly 2 requests")
	})
}
