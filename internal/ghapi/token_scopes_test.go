package ghapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenScopes(t *testing.T) {
	t.Run("returns scopes from the authenticated user response", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/user", r.URL.Path)
			assert.Equal(t, "Bearer candidate-token", r.Header.Get("Authorization"))
			w.Header().Set("X-OAuth-Scopes", "repo, admin:org, workflow")
			_, _ = w.Write([]byte(`{"login":"octocat"}`))
		}))
		defer srv.Close()

		scopes, err := NewClient(srv.URL, "candidate-token").TokenScopes(t.Context())
		require.NoError(t, err)
		assert.Equal(t, []string{"repo", "admin:org", "workflow"}, scopes)
	})

	t.Run("reports when the server does not expose scopes", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"login":"octocat"}`))
		}))
		defer srv.Close()

		_, err := NewClient(srv.URL, "candidate-token").TokenScopes(t.Context())
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrTokenScopesUnavailable)
	})

	t.Run("returns an authentication error for an invalid token", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
		}))
		defer srv.Close()

		_, err := NewClient(srv.URL, "bad-token").TokenScopes(t.Context())
		require.Error(t, err)
		assert.False(t, errors.Is(err, ErrTokenScopesUnavailable))
		var httpErr *HTTPError
		require.ErrorAs(t, err, &httpErr)
		assert.Equal(t, http.StatusUnauthorized, httpErr.StatusCode)
	})
}
