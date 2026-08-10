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
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	}))
	defer srv.Close()

	err := NewClient(srv.URL, "tok").CheckAuthentication(t.Context())
	require.Error(t, err)

	var httpErr *HTTPError
	require.True(t, errors.As(err, &httpErr))
	assert.Equal(t, "3.18.10", httpErr.EnterpriseVersion)
	assert.Equal(t, `HTTP 404 404 Not Found: {"message":"Not Found"}`, httpErr.Error())
}
