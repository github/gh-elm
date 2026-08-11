package elmapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPErrorEnterpriseVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(enterpriseVersionHeader, "3.18.10")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{
			"message": "Not Found",
			"documentation_url": "https://docs.github.com/rest",
			"correlation_id": "abc-123"
		}`))
	}))
	defer srv.Close()

	err := NewClient(srv.URL, "tok").CheckAuthentication(t.Context())
	require.Error(t, err)

	var httpErr *HTTPError
	require.True(t, errors.As(err, &httpErr))
	assert.Equal(t, "3.18.10", httpErr.EnterpriseVersion)
	assert.Equal(t, "Not Found", httpErr.Message)
	assert.Equal(t, "https://docs.github.com/rest", httpErr.DocumentationURL)
	assert.Equal(t, "abc-123", httpErr.CorrelationID)
	assert.Equal(t, "HTTP 404 404 Not Found: Not Found", httpErr.Error())
}

func TestHTTPErrorFallsBackToRawBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	err := NewClient(srv.URL, "tok").CheckAuthentication(t.Context())
	require.Error(t, err)

	var httpErr *HTTPError
	require.ErrorAs(t, err, &httpErr)
	assert.Equal(t, "temporarily unavailable", httpErr.Message)
	assert.Empty(t, httpErr.DocumentationURL)
	assert.Empty(t, httpErr.CorrelationID)
}
