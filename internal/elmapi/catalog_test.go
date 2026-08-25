package elmapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListRepositories(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/user/repos", r.URL.Path)
		assert.Equal(t, "100", r.URL.Query().Get("per_page"))
		assert.Equal(t, "owner,collaborator,organization_member", r.URL.Query().Get("affiliation"))
		_, _ = w.Write([]byte(`[
			{"full_name":"zeta/repo","owner":{"type":"Organization"}},
			{"full_name":"Acme/api","owner":{"type":"Organization"}}
		]`))
	}))
	t.Cleanup(server.Close)

	repositories, err := NewClient(server.URL, "token").ListRepositories(t.Context())

	require.NoError(t, err)
	assert.Equal(t, []Repository{
		repository("Acme/api", "Organization"),
		repository("zeta/repo", "Organization"),
	}, repositories)
}

func TestListOrganizations(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/user/orgs", r.URL.Path)
		assert.Equal(t, "100", r.URL.Query().Get("per_page"))
		_, _ = w.Write([]byte(`[{"login":"zeta"},{"login":"Acme"}]`))
	}))
	t.Cleanup(server.Close)

	organizations, err := NewClient(server.URL, "token").ListOrganizations(t.Context())

	require.NoError(t, err)
	assert.Equal(t, []Organization{
		{Login: "Acme"},
		{Login: "zeta"},
	}, organizations)
}

func repository(fullName, ownerType string) Repository {
	result := Repository{FullName: fullName}
	result.Owner.Type = ownerType
	return result
}
