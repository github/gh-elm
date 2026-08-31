package workflow

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-elm/internal/config"
	"github.com/github/gh-elm/internal/elmapi"
)

func TestParseTargetMigrationID(t *testing.T) {
	t.Run("accepts positive integer", func(t *testing.T) {
		id, err := ParseTargetMigrationID(" 42 ")

		require.NoError(t, err)
		assert.Equal(t, TargetMigrationID(42), id)
	})

	t.Run("rejects non-positive integer", func(t *testing.T) {
		_, err := ParseTargetMigrationID("0")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "positive integer")
	})

	t.Run("rejects non-numeric value", func(t *testing.T) {
		_, err := ParseTargetMigrationID("source-uuid")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "positive integer")
	})
}

func TestParseRepositoryCoordinate(t *testing.T) {
	t.Run("parses owner and repository", func(t *testing.T) {
		owner, repository, err := ParseRepositoryCoordinate(" octo-org / octo-repo ")

		require.NoError(t, err)
		assert.Equal(t, "octo-org", owner)
		assert.Equal(t, "octo-repo", repository)
	})

	t.Run("rejects a missing slash", func(t *testing.T) {
		_, _, err := ParseRepositoryCoordinate("octo-repo")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "exactly one slash")
	})

	t.Run("rejects additional path segments", func(t *testing.T) {
		_, _, err := ParseRepositoryCoordinate("octo-org/team/octo-repo")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "exactly one slash")
	})

	t.Run("rejects an empty component", func(t *testing.T) {
		_, _, err := ParseRepositoryCoordinate("octo-org/")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "non-empty owner and repository")
	})
}

func TestConfiguration(t *testing.T) {
	t.Setenv("GH_ELM_CONFIG_DIR", t.TempDir())
	t.Setenv("GH_ELM_CREDENTIAL_STORE", "file")

	service := New()
	require.NoError(t, service.SaveConfiguration(t.Context(), ConfigurationInput{
		SourceURL:   "https://source.example",
		SourceToken: "source-token",
		TargetURL:   "https://target.example",
		TargetToken: "target-token",
	}))

	configuration, err := service.GetConfiguration(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "https://source.example", configuration.SourceURL)
	assert.True(t, configuration.SourceTokenSet)
	assert.Equal(t, "https://target.example", configuration.TargetURL)
	assert.True(t, configuration.TargetTokenSet)
	assert.Equal(t, "https://source.example/api/v3", configuration.ResolvedSourceURL)
	assert.True(t, configuration.ResolvedSourceTokenSet)
	assert.Equal(t, "https://api.target.example", configuration.ResolvedTargetURL)
	assert.True(t, configuration.ResolvedTargetTokenSet)

	t.Setenv(config.EnvSourceURL, "source-env.example")
	t.Setenv(config.EnvSourceToken, "source-env-token")
	t.Setenv(config.EnvTargetURL, "target-env.example")
	t.Setenv(config.EnvTargetToken, "target-env-token")
	configuration, err = service.GetConfiguration(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "https://source-env.example/api/v3", configuration.ResolvedSourceURL)
	assert.True(t, configuration.ResolvedSourceTokenSet)
	assert.Equal(t, "https://api.target-env.example", configuration.ResolvedTargetURL)
	assert.True(t, configuration.ResolvedTargetTokenSet)

	require.NoError(t, service.ResetConfiguration(t.Context()))
	configuration, err = service.GetConfiguration(t.Context())
	require.NoError(t, err)
	assert.Empty(t, configuration.SourceURL)
	assert.False(t, configuration.SourceTokenSet)
	assert.Empty(t, configuration.TargetURL)
	assert.False(t, configuration.TargetTokenSet)
}

func TestCheckAuthentication(t *testing.T) {
	t.Setenv("GH_ELM_CONFIG_DIR", t.TempDir())
	t.Setenv("GH_ELM_CREDENTIAL_STORE", "file")

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v3/user", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(source.Close)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/user", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(target.Close)

	service := New()
	require.NoError(t, service.SaveConfiguration(t.Context(), ConfigurationInput{
		SourceURL:   source.URL + `\`,
		SourceToken: "source-token",
		TargetURL:   target.URL + `\/`,
		TargetToken: "target-token",
	}))

	assert.NoError(t, service.CheckSourceAuthentication(t.Context()))
	assert.NoError(t, service.CheckTargetAuthentication(t.Context()))
}

func TestTargetLifecycleRequests(t *testing.T) {
	t.Setenv("GH_ELM_CONFIG_DIR", t.TempDir())
	t.Setenv("GH_ELM_CREDENTIAL_STORE", "file")

	bodies := make(map[string]map[string]any)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		bodies[r.URL.Path] = body
		if r.URL.Path == "/enterprise/migration/create" {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"migrationId":"42"}`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	service := New()
	require.NoError(t, service.SaveConfiguration(t.Context(), ConfigurationInput{
		SourceURL:   "https://source.example",
		TargetURL:   server.URL,
		TargetToken: "target-token",
	}))

	_, err := service.CreateTargetMigration(t.Context(), TargetCreateInput{
		SourceRepositoryURL: "https://source.example/octo/repo",
		Repository:          "octo/repo",
		Description:         "test migration",
		ExporterGUID:        "11111111-1111-1111-1111-111111111111",
	})
	require.NoError(t, err)
	require.NoError(t, service.PauseTargetMigration(t.Context(), 42))
	require.NoError(t, service.ResumeTargetMigration(t.Context(), 42))
	require.NoError(t, service.AbortTargetMigration(t.Context(), 42))

	createBody := bodies["/enterprise/migration/create"]
	assert.Equal(t, "https://source.example/octo/repo", createBody["source_url"])
	assert.Equal(t, []any{"octo/repo"}, createBody["repositories"])
	assert.Equal(t, "test migration", createBody["description"])
	assert.Equal(t, "11111111-1111-1111-1111-111111111111", createBody["exporter_migration_guid"])

	operationIDs := make([]string, 0, 4)
	for _, path := range []string{
		"/enterprise/migration/create",
		"/enterprise/migration/42/pause",
		"/enterprise/migration/42/resume",
		"/enterprise/migration/42/abort",
	} {
		body, ok := bodies[path]
		require.True(t, ok, "missing request to %s", path)
		operationIDs = append(operationIDs, assertWorkflowCustomerTransition(t, body))
	}
	assert.Len(t, map[string]struct{}{
		operationIDs[0]: {},
		operationIDs[1]: {},
		operationIDs[2]: {},
		operationIDs[3]: {},
	}, 4, "each TUI action must use a fresh operation ID")
}

func TestRepositoryCatalog(t *testing.T) {
	t.Setenv("GH_ELM_CONFIG_DIR", t.TempDir())
	t.Setenv("GH_ELM_CREDENTIAL_STORE", "file")

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v3/user/repos", r.URL.Path)
		_, _ = w.Write([]byte(`[
			{"full_name":"octo/source","owner":{"type":"Organization"}},
			{"full_name":"personal/source","owner":{"type":"User"}}
		]`))
	}))
	t.Cleanup(source.Close)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/user/orgs", r.URL.Path)
		assert.NoError(t, json.NewEncoder(w).Encode([]elmapi.Organization{
			{Login: "octo-target"},
		}))
	}))
	t.Cleanup(target.Close)

	service := New()
	require.NoError(t, service.SaveConfiguration(t.Context(), ConfigurationInput{
		SourceURL:   source.URL + `\`,
		SourceToken: "source-token",
		TargetURL:   target.URL + `\/`,
		TargetToken: "target-token",
	}))

	repositories, err := service.ListSourceRepositories(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{"octo/source"}, repositories)

	organizations, err := service.ListTargetOrganizations(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{"octo-target"}, organizations)
}

func TestListSourceMigrations(t *testing.T) {
	t.Setenv("GH_ELM_CONFIG_DIR", t.TempDir())
	t.Setenv("GH_ELM_CREDENTIAL_STORE", "file")

	var statuses []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		statuses = append(statuses, r.URL.Query().Get("status"))
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("status") == "created" {
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"migrations":  []map[string]any{{"migration_id": "created-1"}},
				"total_count": 1,
			}))
			return
		}
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"migrations":  []any{},
			"total_count": 0,
		}))
	}))
	t.Cleanup(server.Close)

	service := New()
	require.NoError(t, service.SaveConfiguration(t.Context(), ConfigurationInput{
		SourceURL:   server.URL,
		SourceToken: "source-token",
	}))

	migrations, err := service.ListSourceMigrations(t.Context(), "")

	require.NoError(t, err)
	require.Len(t, migrations, 1)
	assert.Equal(t, "created-1", migrations[0].MigrationID)
	assert.Equal(t, []string{"", "created"}, statuses)
}

func assertWorkflowCustomerTransition(t *testing.T, body map[string]any) string {
	t.Helper()

	assert.Equal(t, elmapi.TargetMigrationInitiatorCustomer, body["initiator"])
	assert.NotContains(t, body, "actor")
	operationID, ok := body["operation_id"].(string)
	require.True(t, ok, "operation_id must be a string")
	require.NoError(t, uuid.Validate(operationID))
	return operationID
}
