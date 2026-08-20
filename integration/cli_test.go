//go:build integration

package integration_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersion(t *testing.T) {
	result := runCLI(t, nil, "--version")

	require.Equal(t, 0, result.ExitCode, result.Stderr)
	assert.Equal(t, "gh elm "+integrationVersion+"\n", result.Stdout)
	assert.Empty(t, result.Stderr)
}

func TestHelp(t *testing.T) {
	result := runCLI(t, nil, "--help")

	require.Equal(t, 0, result.ExitCode, result.Stderr)
	assert.Contains(
		t,
		result.Stdout,
		"Drive Enterprise Live Migrations (ELM) against the GitHub Enterprise Server REST API.",
	)
	assert.Contains(t, result.Stdout, "gh elm <command> <subcommand> [flags]")
	assert.Contains(t, result.Stdout, "MIGRATION COMMANDS")
	assert.Contains(t, result.Stdout, "TARGET COMMANDS")
	assert.Empty(t, result.Stderr)
}

func TestInvalidCommand(t *testing.T) {
	result := runCLI(t, nil, "definitely-not-a-command")

	require.NotEqual(t, 0, result.ExitCode)
	assert.Empty(t, result.Stdout)
	assert.Contains(t, result.Stderr, "Error\n")
	assert.Contains(t, result.Stderr, "unknown command")
	assert.NotContains(t, result.Stderr, "Usage:")
}

func TestMigrationStatus(t *testing.T) {
	t.Run("succeeds", func(t *testing.T) {
		const response = `{"migration":{"migration_id":"mig-1","status":"in_progress"}}`

		var requestCount atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount.Add(1)

			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "/api/v3/enterprise/live-migrations/mig-1", r.URL.Path)
			assert.Equal(t, "Bearer source-token", r.Header.Get("Authorization"))

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(response))
		}))
		t.Cleanup(srv.Close)

		result := runCLI(
			t,
			nil,
			"migration",
			"status",
			"mig-1",
			"--source-url",
			srv.URL,
			"--source-token",
			"source-token",
		)

		require.Equal(t, 0, result.ExitCode, result.Stderr)
		for _, want := range []string{"Migration", "mig-1", "In progress"} {
			assert.Contains(t, result.Stdout, want)
		}
		assert.Empty(t, result.Stderr)
		assert.Equal(t, int32(1), requestCount.Load())

		// Tokens must never be included in user-facing output.
		assert.NotContains(t, result.Stdout, "source-token")
		assert.NotContains(t, result.Stderr, "source-token")
	})

	t.Run("reports authentication failure", func(t *testing.T) {
		var requestCount atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount.Add(1)

			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "/api/v3/enterprise/live-migrations/mig-1", r.URL.Path)
			assert.Equal(t, "Bearer invalid-source-token", r.Header.Get("Authorization"))

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
		}))
		t.Cleanup(srv.Close)

		result := runCLI(
			t,
			nil,
			"migration",
			"status",
			"mig-1",
			"--source-url",
			srv.URL,
			"--source-token",
			"invalid-source-token",
		)

		require.NotEqual(t, 0, result.ExitCode)
		assert.Empty(t, result.Stdout)
		assert.Equal(t, int32(1), requestCount.Load())

		assert.Contains(t, result.Stderr, "Error\n")
		assert.Contains(t, result.Stderr, "authentication failed")
		assert.Contains(t, result.Stderr, "HTTP 401")
		assert.Contains(t, result.Stderr, "GH_SOURCE_HOST")
		assert.Contains(t, result.Stderr, "GH_SOURCE_TOKEN")

		// The supplied credential must not be echoed in diagnostics.
		assert.NotContains(t, result.Stdout, "invalid-source-token")
		assert.NotContains(t, result.Stderr, "invalid-source-token")
	})
}

func TestTargetResourcesJSON(t *testing.T) {
	const response = `{
		"nodes": [
			{
				"id": "node-1",
				"type": "issue",
				"origin": "NODE_ORIGIN_BACKFILL",
				"state": "NODE_STATE_PROCESSED",
				"correlationId": "correlation-123"
			}
		],
		"after": ""
	}`

	var requestCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)

		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/enterprise/migration/42/nodes", r.URL.Path)
		assert.Equal(t, "Bearer target-token", r.Header.Get("Authorization"))
		assert.Equal(t, "NODE_ORIGIN_BACKFILL", r.URL.Query().Get("origin"))
		assert.Equal(t, "octo/repo", r.URL.Query().Get("repository_nwo"))
		assert.Equal(t, "100", r.URL.Query().Get("page_size"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(srv.Close)

	result := runCLI(
		t,
		nil,
		"target",
		"resources",
		"42",
		"octo/repo",
		"--origin",
		"backfill",
		"--json",
		"--target-url",
		srv.URL,
		"--target-token",
		"target-token",
	)

	require.Equal(t, 0, result.ExitCode, result.Stderr)
	assert.Empty(t, result.Stderr)
	assert.Equal(t, int32(1), requestCount.Load())
	assert.NotContains(t, result.Stdout, "Found ")
	assert.NotContains(t, result.Stdout, "target-token")
	assert.NotContains(t, result.Stderr, "target-token")

	// Every nonempty output line must be an independently valid JSON object.
	scanner := bufio.NewScanner(strings.NewReader(result.Stdout))

	var nodes []map[string]any
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var node map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &node))
		nodes = append(nodes, node)
	}
	require.NoError(t, scanner.Err())

	require.Len(t, nodes, 1)
	assert.Equal(t, "node-1", nodes[0]["id"])
	assert.Equal(t, "issue", nodes[0]["type"])

	// Unknown API fields must survive the CLI's JSON output.
	assert.Equal(t, "correlation-123", nodes[0]["correlationId"])
}

func TestSourceConfigurationPrecedence(t *testing.T) {
	configDir := t.TempDir()

	storedServer, storedRequests := newStatusServer(t, "stored", "stored-token")
	t.Cleanup(storedServer.Close)

	envServer, envRequests := newStatusServer(t, "environment", "env-token")
	t.Cleanup(envServer.Close)

	flagServer, flagRequests := newStatusServer(t, "flag", "flag-token")
	t.Cleanup(flagServer.Close)

	writeStoredConfiguration(
		t,
		configDir,
		storedServer.URL,
		"stored-token",
	)

	baseEnv := map[string]string{
		"GH_ELM_CONFIG_DIR": configDir,
	}

	t.Run("uses stored configuration when no override is present", func(t *testing.T) {
		result := runCLI(
			t,
			baseEnv,
			"migration",
			"status",
			"mig-1",
			"--json",
		)

		require.Equal(t, 0, result.ExitCode, result.Stderr)
		assert.Contains(t, result.Stdout, `"source":"stored"`)
		assert.Empty(t, result.Stderr)
	})

	t.Run("environment overrides stored configuration", func(t *testing.T) {
		result := runCLI(
			t,
			map[string]string{
				"GH_ELM_CONFIG_DIR": configDir,
				"GH_SOURCE_HOST":    envServer.URL,
				"GH_SOURCE_TOKEN":   "env-token",
			},
			"migration",
			"status",
			"mig-1",
			"--json",
		)

		require.Equal(t, 0, result.ExitCode, result.Stderr)
		assert.Contains(t, result.Stdout, `"source":"environment"`)
		assert.Empty(t, result.Stderr)
	})

	t.Run("flags override environment and stored configuration", func(t *testing.T) {
		result := runCLI(
			t,
			map[string]string{
				"GH_ELM_CONFIG_DIR": configDir,
				"GH_SOURCE_HOST":    envServer.URL,
				"GH_SOURCE_TOKEN":   "env-token",
			},
			"migration",
			"status",
			"mig-1",
			"--json",
			"--source-url",
			flagServer.URL,
			"--source-token",
			"flag-token",
		)

		require.Equal(t, 0, result.ExitCode, result.Stderr)
		assert.Contains(t, result.Stdout, `"source":"flag"`)
		assert.Empty(t, result.Stderr)
	})

	assert.Equal(t, int32(1), storedRequests.Load())
	assert.Equal(t, int32(1), envRequests.Load())
	assert.Equal(t, int32(1), flagRequests.Load())
}

// newStatusServer returns a source API server that identifies itself in its
// response. This lets the precedence test prove which endpoint was selected.
func newStatusServer(
	t *testing.T,
	source string,
	expectedToken string,
) (*httptest.Server, *atomic.Int32) {
	var requestCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)

		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v3/enterprise/live-migrations/mig-1", r.URL.Path)
		assert.Equal(t, "Bearer "+expectedToken, r.Header.Get("Authorization"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(
			w,
			`{"migration":{"migration_id":"mig-1"},"source":%q}`,
			source,
		)
	}))

	return srv, &requestCount
}

// writeStoredConfiguration creates the files used by the CLI's file-backed
// configuration and credential stores.
func writeStoredConfiguration(
	t *testing.T,
	configDir string,
	sourceURL string,
	sourceToken string,
) {
	require.NoError(t, os.MkdirAll(configDir, 0o700))

	config := struct {
		SourceURL string `json:"source_url"`
	}{
		SourceURL: sourceURL,
	}
	configData, err := json.MarshalIndent(config, "", "  ")
	require.NoError(t, err)

	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(configDir, "config.json"),
			append(configData, '\n'),
			0o600,
		),
	)

	credentials := map[string]string{
		"source-token": sourceToken,
	}
	credentialsData, err := json.MarshalIndent(credentials, "", "  ")
	require.NoError(t, err)

	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(configDir, "credentials.json"),
			append(credentialsData, '\n'),
			0o600,
		),
	)
}
