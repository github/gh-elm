package preflight

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckEndpoint(t *testing.T) {
	t.Run("reports a server failure separately from network reachability", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		}))
		defer server.Close()

		var stderr bytes.Buffer
		err := CheckEndpoint(t.Context(), &stderr, "Source", server.URL, "token")

		require.Error(t, err)
		assert.Contains(t, stderr.String(), "[OK] Source network: reachable (HTTP 503 Service Unavailable)")
		assert.Contains(t, stderr.String(), "[FAIL] Source service: unhealthy (HTTP 503 Service Unavailable)")
		assert.Contains(t, stderr.String(), "[FAIL] Source authentication: HTTP 503 Service Unavailable; credentials could not be verified")
	})

	t.Run("reports a transport failure separately from authentication", func(t *testing.T) {
		server := httptest.NewServer(http.NotFoundHandler())
		serverURL := server.URL
		server.Close()

		var stderr bytes.Buffer
		err := CheckEndpoint(t.Context(), &stderr, "Source", serverURL, "token")

		require.Error(t, err)
		assert.Contains(t, stderr.String(), "[FAIL] Source network:")
		assert.Contains(t, stderr.String(), "[FAIL] Source service: not checked because the network probe failed")
		assert.Contains(t, stderr.String(), "[FAIL] Source authentication: could not verify credentials")
	})
}
