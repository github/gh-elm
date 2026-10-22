package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-elm/internal/config"
	"github.com/github/gh-elm/internal/creds"
)

// failingStore is a creds.Store whose Get always fails, simulating an unreadable
// or malformed credential backend.
type failingStore struct{ err error }

func (f failingStore) Get(string) (string, error) { return "", f.err }
func (failingStore) Set(string, string) error     { return nil }
func (failingStore) Delete(string) error          { return nil }
func (failingStore) Location() string             { return "test backend" }

func TestConfigureShowPropagatesStoreError(t *testing.T) {
	t.Setenv("GH_ELM_CONFIG_DIR", t.TempDir())

	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := runConfigureShow(cmd, failingStore{err: errors.New("malformed credentials file")})
	require.Error(t, err, "expected an error when the credential store cannot be read")
	assert.Contains(t, err.Error(), "malformed credentials file", "error should surface the backend failure")
	assert.NotContains(t, buf.String(), "not set", "a read failure must not be reported as \"not set\"")
}

// execConfigure runs `gh elm config <args>` through the root command and
// returns combined output. Callers set GH_ELM_CONFIG_DIR / GH_ELM_CREDENTIAL_STORE
// (and seed state) beforehand so the run is isolated to a temp dir.
func execConfigure(args ...string) (string, error) {
	root := NewRootCmd("test")
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(append([]string{"config"}, args...))
	err := root.Execute()
	return buf.String(), err
}

func seedFileStore(t *testing.T) {
	t.Setenv("GH_ELM_CONFIG_DIR", t.TempDir())
	t.Setenv("GH_ELM_CREDENTIAL_STORE", "file")
	t.Setenv(config.EnvSourceURL, "")
	t.Setenv(config.EnvSourceToken, "")
	t.Setenv(config.EnvTargetURL, "")
	t.Setenv(config.EnvTargetToken, "")
}

func TestConfigureShow(t *testing.T) {
	seedFileStore(t)
	require.NoError(t, (&config.Config{SourceURL: "https://ghes.example.com"}).Save(), "seed config")
	store, err := creds.NewStore()
	require.NoError(t, err, "NewStore")
	require.NoError(t, store.Set(creds.SourceToken, "ghp_source"), "seed token")

	out, err := execConfigure("show")
	require.NoError(t, err, "config show")
	assert.False(t, strings.HasPrefix(out, "\n"))
	assert.True(t, strings.HasSuffix(out, "\n\n"))
	assert.Contains(t, out, "https://ghes.example.com", "missing source url in output")
	assert.Contains(t, out, "set (hidden)", "seeded source token should read as set")
	assert.Contains(t, out, "not set", "unset target token should read as not set")
}

func TestMigratorPATInput(t *testing.T) {
	t.Run("body flag", func(t *testing.T) {
		t.Setenv("SOURCE_PAT", "env-pat")
		cmd := &cobra.Command{}
		cmd.SetIn(strings.NewReader("stdin-pat"))

		pat, err := migratorPATInput(cmd, "body-pat", "SOURCE_PAT")
		require.NoError(t, err)
		assert.Equal(t, "body-pat", pat)
	})

	t.Run("environment", func(t *testing.T) {
		t.Setenv("SOURCE_PAT", "env-pat")
		cmd := &cobra.Command{}
		cmd.SetIn(strings.NewReader("stdin-pat"))

		pat, err := migratorPATInput(cmd, "", "SOURCE_PAT")
		require.NoError(t, err)
		assert.Equal(t, "env-pat", pat)
	})

	t.Run("standard input trims line endings only", func(t *testing.T) {
		t.Setenv("SOURCE_PAT", "")
		cmd := &cobra.Command{}
		cmd.SetIn(strings.NewReader("  stdin-pat  \r\n"))

		pat, err := migratorPATInput(cmd, "", "SOURCE_PAT")
		require.NoError(t, err)
		assert.Equal(t, "  stdin-pat  ", pat)
	})

	t.Run("empty standard input", func(t *testing.T) {
		t.Setenv("SOURCE_PAT", "")
		cmd := &cobra.Command{}
		cmd.SetIn(strings.NewReader("\n"))

		_, err := migratorPATInput(cmd, "", "SOURCE_PAT")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "standard input did not contain a PAT")
	})
}

func TestValidateConfigCredential(t *testing.T) {
	t.Run("source uses the GHES REST API and local CLI scopes", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/v3/user", r.URL.Path)
			w.Header().Set("X-OAuth-Scopes", "admin:enterprise, admin:org")
			_, _ = w.Write([]byte(`{"login":"octocat"}`))
		}))
		defer srv.Close()

		result := validateConfigCredential(t.Context(), sourceConfigCredential, srv.URL, "source-token")
		assert.Equal(t, tokenValid, result.status)
	})

	t.Run("target uses the target API and local CLI scopes", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/user", r.URL.Path)
			w.Header().Set("X-OAuth-Scopes", "admin:enterprise")
			_, _ = w.Write([]byte(`{"login":"octocat"}`))
		}))
		defer srv.Close()

		result := validateConfigCredential(t.Context(), targetConfigCredential, srv.URL, "target-token")
		assert.Equal(t, tokenInvalid, result.status)
		assert.Equal(t, []string{"admin:org"}, result.missingScopes)
	})

	t.Run("missing token is invalid without an API request", func(t *testing.T) {
		var requests atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requests.Add(1)
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer srv.Close()

		result := validateConfigCredential(t.Context(), sourceConfigCredential, srv.URL, "")
		assert.Equal(t, tokenInvalid, result.status)
		require.EqualError(t, result.cause, "token is not configured")
		assert.Zero(t, requests.Load())
	})

	t.Run("missing URL is invalid", func(t *testing.T) {
		result := validateConfigCredential(t.Context(), sourceConfigCredential, "", "token")
		assert.Equal(t, tokenInvalid, result.status)
		assert.EqualError(t, result.cause, "API URL is not configured")
	})
}

func TestValidateTokenScopes(t *testing.T) {
	t.Run("accepts a token with every required scope", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-OAuth-Scopes", "repo, admin:org, admin:repo_hook")
			_, _ = w.Write([]byte(`{"login":"octocat"}`))
		}))
		defer srv.Close()

		result := validateTokenScopes(t.Context(), srv.URL, "token", []string{"repo", "admin:org", "admin:repo_hook"})
		assert.Equal(t, tokenValid, result.status)
		assert.Empty(t, result.missingScopes)
		assert.NoError(t, result.cause)
	})

	t.Run("repo satisfies the source repository hook requirement", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-OAuth-Scopes", "repo, admin:org, admin:org_hook")
			_, _ = w.Write([]byte(`{"login":"octocat"}`))
		}))
		defer srv.Close()

		result := validateTokenScopes(t.Context(), srv.URL, "token", sourcePATRequiredScopes)
		assert.Equal(t, tokenValid, result.status)
		assert.Empty(t, result.missingScopes)
	})

	t.Run("repo satisfies the target repository hook requirement", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-OAuth-Scopes", "repo, workflow, admin:org, admin:enterprise")
			_, _ = w.Write([]byte(`{"login":"octocat"}`))
		}))
		defer srv.Close()

		result := validateTokenScopes(t.Context(), srv.URL, "token", targetPATRequiredScopes)
		assert.Equal(t, tokenValid, result.status)
		assert.Empty(t, result.missingScopes)
	})

	t.Run("rejects a token and identifies missing scopes", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-OAuth-Scopes", "repo")
			_, _ = w.Write([]byte(`{"login":"octocat"}`))
		}))
		defer srv.Close()

		result := validateTokenScopes(t.Context(), srv.URL, "token", []string{"repo", "workflow", "admin:enterprise"})
		assert.Equal(t, tokenInvalid, result.status)
		assert.Equal(t, []string{"workflow", "admin:enterprise"}, result.missingScopes)
	})

	t.Run("warns when scopes are not exposed", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"login":"octocat"}`))
		}))
		defer srv.Close()

		result := validateTokenScopes(t.Context(), srv.URL, "token", []string{"repo"})
		assert.Equal(t, tokenValidationWarning, result.status)
		assert.Error(t, result.cause)
	})

	t.Run("rejects a token that GitHub does not authenticate", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
		}))
		defer srv.Close()

		result := validateTokenScopes(t.Context(), srv.URL, "token", []string{"repo"})
		assert.Equal(t, tokenInvalid, result.status)
		assert.Error(t, result.cause)
	})
}

func TestHandleConfigCredentialValidation(t *testing.T) {
	t.Run("valid token advances without prompting", func(t *testing.T) {
		cmd := &cobra.Command{}
		var stderr bytes.Buffer
		cmd.SetErr(&stderr)
		prompted := false

		decision, err := handleConfigCredentialValidation(
			cmd,
			"Source token",
			tokenValidationResult{status: tokenValid},
			func() (validationDecision, error) {
				prompted = true
				return validationAbort, nil
			},
		)

		require.NoError(t, err)
		assert.Equal(t, validationContinue, decision)
		assert.False(t, prompted)
		assert.Contains(t, stderr.String(), "✓ Source token has all required scopes")
	})

	t.Run("invalid token prints a red x and honors retry", func(t *testing.T) {
		cmd := &cobra.Command{}
		var stderr bytes.Buffer
		cmd.SetErr(&stderr)

		decision, err := handleConfigCredentialValidation(
			cmd,
			"Target token",
			tokenValidationResult{status: tokenInvalid, missingScopes: []string{"admin:org"}},
			func() (validationDecision, error) { return validationRetry, nil },
		)

		require.NoError(t, err)
		assert.Equal(t, validationRetry, decision)
		assert.Contains(t, stderr.String(), "✗ Target token is missing required scopes: admin:org")
	})

	t.Run("unverifiable token prints a warning and honors abort", func(t *testing.T) {
		cmd := &cobra.Command{}
		var stderr bytes.Buffer
		cmd.SetErr(&stderr)

		decision, err := handleConfigCredentialValidation(
			cmd,
			"Source token",
			tokenValidationResult{status: tokenValidationWarning, cause: errors.New("connection refused")},
			func() (validationDecision, error) { return validationAbort, nil },
		)

		require.NoError(t, err)
		assert.Equal(t, validationAbort, decision)
		assert.Contains(t, stderr.String(), "⚠ Could not validate Source token scopes: connection refused")
	})

	t.Run("continue anyway advances after invalid authentication", func(t *testing.T) {
		cmd := &cobra.Command{}
		var stderr bytes.Buffer
		cmd.SetErr(&stderr)

		decision, err := handleConfigCredentialValidation(
			cmd,
			"Source token",
			tokenValidationResult{status: tokenInvalid, cause: errors.New("GitHub rejected the token")},
			func() (validationDecision, error) { return validationContinue, nil },
		)

		require.NoError(t, err)
		assert.Equal(t, validationContinue, decision)
		assert.Contains(t, stderr.String(), "✗ Source token is invalid: GitHub rejected the token")
	})
}

func TestRunConfigCredentialPhase(t *testing.T) {
	t.Run("retry collects and validates the credential again", func(t *testing.T) {
		var requests atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempt := requests.Add(1)
			if attempt == 1 {
				http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
				return
			}
			w.Header().Set("X-OAuth-Scopes", "admin:enterprise, admin:org")
			_, _ = w.Write([]byte(`{"login":"octocat"}`))
		}))
		defer srv.Close()

		cmd := &cobra.Command{}
		cmd.SetContext(t.Context())
		cmd.SetErr(&bytes.Buffer{})
		url := srv.URL
		var token string
		collectCalls := 0
		chooseCalls := 0

		err := runConfigCredentialPhase(
			cmd,
			"Target token",
			targetConfigCredential,
			&url,
			&token,
			func() error {
				collectCalls++
				token = fmt.Sprintf("token-%d", collectCalls)
				return nil
			},
			func() (validationDecision, error) {
				chooseCalls++
				return validationRetry, nil
			},
		)

		require.NoError(t, err)
		assert.Equal(t, 2, collectCalls)
		assert.Equal(t, 1, chooseCalls)
		assert.Equal(t, int32(2), requests.Load())
	})

	t.Run("abort cancels the phase", func(t *testing.T) {
		cmd := &cobra.Command{}
		cmd.SetContext(t.Context())
		cmd.SetErr(&bytes.Buffer{})
		url := "http://127.0.0.1:1"
		token := "token"

		err := runConfigCredentialPhase(
			cmd,
			"Source token",
			sourceConfigCredential,
			&url,
			&token,
			func() error { return nil },
			func() (validationDecision, error) { return validationAbort, nil },
		)

		require.EqualError(t, err, "configuration cancelled")
	})

	t.Run("continue anyway accepts an unverifiable credential", func(t *testing.T) {
		cmd := &cobra.Command{}
		cmd.SetContext(t.Context())
		cmd.SetErr(&bytes.Buffer{})
		url := "http://127.0.0.1:1"
		token := "token"

		err := runConfigCredentialPhase(
			cmd,
			"Source token",
			sourceConfigCredential,
			&url,
			&token,
			func() error { return nil },
			func() (validationDecision, error) { return validationContinue, nil },
		)

		require.NoError(t, err)
	})
}

func TestReportTokenValidation(t *testing.T) {
	t.Run("interactive valid token prints a green check", func(t *testing.T) {
		cmd := &cobra.Command{}
		var stderr bytes.Buffer
		cmd.SetErr(&stderr)

		err := reportTokenValidation(cmd, "SOURCE_PAT", tokenValidationResult{status: tokenValid}, true)
		require.NoError(t, err)
		assert.Contains(t, stderr.String(), "✓")
		assert.Contains(t, stderr.String(), "SOURCE_PAT has all required scopes")
	})

	t.Run("noninteractive valid token adds no output", func(t *testing.T) {
		cmd := &cobra.Command{}
		var stderr bytes.Buffer
		cmd.SetErr(&stderr)

		err := reportTokenValidation(cmd, "SOURCE_PAT", tokenValidationResult{status: tokenValid}, false)
		require.NoError(t, err)
		assert.Empty(t, stderr.String())
	})

	t.Run("warning prints a yellow triangle and remains nonfatal", func(t *testing.T) {
		cmd := &cobra.Command{}
		var stderr bytes.Buffer
		cmd.SetErr(&stderr)

		err := reportTokenValidation(cmd, "TARGET_PAT", tokenValidationResult{
			status: tokenValidationWarning,
			cause:  errors.New("API did not expose token scopes"),
		}, false)
		require.NoError(t, err)
		assert.Contains(t, stderr.String(), "⚠")
		assert.Contains(t, stderr.String(), "Could not validate TARGET_PAT")
		assert.Contains(t, stderr.String(), "API did not expose token scopes")
	})

	t.Run("invalid token returns an error naming missing scopes", func(t *testing.T) {
		cmd := &cobra.Command{}

		err := reportTokenValidation(cmd, "TARGET_PAT", tokenValidationResult{
			status:        tokenInvalid,
			missingScopes: []string{"workflow", "admin:enterprise"},
		}, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "TARGET_PAT is missing required scopes")
		assert.Contains(t, err.Error(), "workflow, admin:enterprise")
		assert.Contains(t, FormatError(err), "✗")
	})
}

func TestSetMigratorPATCommands(t *testing.T) {
	cmd := newConfigCmd()
	sourceCmd := findSubcommand(cmd, "set-source-pat")
	targetCmd := findSubcommand(cmd, "set-target-pat")
	require.NotNil(t, sourceCmd)
	require.NotNil(t, targetCmd)
	assert.NotNil(t, sourceCmd.Flags().Lookup("org"))
	assert.NotNil(t, targetCmd.Flags().Lookup("org"))

	sourceCmd.SetIn(strings.NewReader(""))
	err := sourceCmd.RunE(sourceCmd, nil)
	require.Error(t, err)
	assert.EqualError(t, err, "organization required: pass ORG or use --org")
}

func TestSetMigratorPATValidation(t *testing.T) {
	seedFileStore(t)

	t.Run("missing source scopes prevent storing the secret", func(t *testing.T) {
		var secretRequests atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v3/user":
				w.Header().Set("X-OAuth-Scopes", "repo")
				_, _ = w.Write([]byte(`{"login":"octocat"}`))
			default:
				secretRequests.Add(1)
				http.Error(w, "unexpected secret request", http.StatusInternalServerError)
			}
		}))
		defer srv.Close()

		out, err := execConfigure(
			"set-source-pat", "octo-org",
			"--body", "candidate-token",
			"--source-url", srv.URL+"/api/v3",
			"--source-token", "admin-token",
		)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "admin:org, admin:org_hook")
		assert.Zero(t, secretRequests.Load())
		assert.NotContains(t, out, "Set SOURCE_PAT")
	})

	t.Run("unavailable source scopes warn and still store the secret", func(t *testing.T) {
		var secretPuts atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/api/v3/user":
				_, _ = w.Write([]byte(`{"login":"octocat"}`))
			case strings.HasSuffix(r.URL.Path, "/public-key"):
				_, _ = w.Write([]byte(`{"key_id":"key-1","key":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}`))
			case r.Method == http.MethodPut:
				secretPuts.Add(1)
				w.WriteHeader(http.StatusCreated)
			default:
				http.NotFound(w, r)
			}
		}))
		defer srv.Close()

		out, err := execConfigure(
			"set-source-pat", "octo-org",
			"--body", "candidate-token",
			"--source-url", srv.URL+"/api/v3",
			"--source-token", "admin-token",
		)
		require.NoError(t, err)
		assert.Contains(t, out, "⚠ Could not validate SOURCE_PAT")
		assert.Contains(t, out, "Set SOURCE_PAT migrator secret")
		assert.Equal(t, int32(1), secretPuts.Load())
	})

	t.Run("target token is validated against the target API", func(t *testing.T) {
		var targetCalled atomic.Bool
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			targetCalled.Store(true)
			assert.Equal(t, "/user", r.URL.Path)
			assert.Equal(t, "Bearer candidate-token", r.Header.Get("Authorization"))
			w.Header().Set("X-OAuth-Scopes", "repo, workflow, admin:org, admin:repo_hook, admin:enterprise")
			_, _ = w.Write([]byte(`{"login":"octocat"}`))
		}))
		defer target.Close()

		var secretPuts atomic.Int32
		source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasSuffix(r.URL.Path, "/public-key"):
				_, _ = w.Write([]byte(`{"key_id":"key-1","key":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}`))
			case r.Method == http.MethodPut:
				secretPuts.Add(1)
				w.WriteHeader(http.StatusCreated)
			default:
				http.NotFound(w, r)
			}
		}))
		defer source.Close()

		out, err := execConfigure(
			"set-target-pat", "octo-org",
			"--body", "candidate-token",
			"--source-url", source.URL+"/api/v3",
			"--source-token", "admin-token",
			"--target-url", target.URL,
		)
		require.NoError(t, err)
		assert.True(t, targetCalled.Load())
		assert.NotContains(t, out, "has all required scopes")
		assert.Contains(t, out, "Set TARGET_PAT migrator secret")
		assert.Equal(t, int32(1), secretPuts.Load())
	})
}

func TestSetMigratorPATPairValidatesBothBeforeStoringEither(t *testing.T) {
	var secretRequests atomic.Int32
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/user":
			w.Header().Set("X-OAuth-Scopes", "repo, admin:org, admin:repo_hook, admin:org_hook")
			_, _ = w.Write([]byte(`{"login":"source-user"}`))
		default:
			secretRequests.Add(1)
			http.Error(w, "secret should not be stored", http.StatusInternalServerError)
		}
	}))
	defer source.Close()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/user", r.URL.Path)
		w.Header().Set("X-OAuth-Scopes", "repo, workflow, admin:org, admin:repo_hook")
		_, _ = w.Write([]byte(`{"login":"target-user"}`))
	}))
	defer target.Close()

	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)

	err := setMigratorPATPair(
		cmd,
		"octo-org",
		source.URL+"/api/v3",
		"source-admin-token",
		target.URL,
		"source-pat",
		"target-pat",
		false,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TARGET_PAT is missing required scopes: admin:enterprise")
	assert.Zero(t, secretRequests.Load())
}

func TestResolveOrganization(t *testing.T) {
	t.Run("positional", func(t *testing.T) {
		org, err := resolveOrganization([]string{" octo-org "}, "", false)
		require.NoError(t, err)
		assert.Equal(t, "octo-org", org)
	})

	t.Run("flag", func(t *testing.T) {
		org, err := resolveOrganization(nil, " octo-org ", true)
		require.NoError(t, err)
		assert.Equal(t, "octo-org", org)
	})

	t.Run("both", func(t *testing.T) {
		_, err := resolveOrganization([]string{"octo-org"}, "other-org", true)
		require.Error(t, err)
		assert.EqualError(t, err, "ORG cannot be combined with --org")
	})

	t.Run("missing", func(t *testing.T) {
		_, err := resolveOrganization(nil, "", false)
		require.Error(t, err)
		assert.EqualError(t, err, "organization required: pass ORG or use --org")
	})
}

func findSubcommand(cmd *cobra.Command, name string) *cobra.Command {
	for _, child := range cmd.Commands() {
		if child.Name() == name {
			return child
		}
	}
	return nil
}

func TestConfigConfirmAccess(t *testing.T) {
	t.Run("validates source and skips an unconfigured target", func(t *testing.T) {
		seedFileStore(t)
		source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/v3/user", r.URL.Path)
			w.Header().Set("X-OAuth-Scopes", "admin:enterprise, admin:org")
			_, _ = w.Write([]byte(`{"login":"source-user"}`))
		}))
		defer source.Close()

		require.NoError(t, (&config.Config{SourceURL: source.URL}).Save())
		store, err := creds.NewStore()
		require.NoError(t, err)
		require.NoError(t, store.Set(creds.SourceToken, "source-token"))

		out, err := execConfigure("confirm-access", "--check")
		require.NoError(t, err)
		assert.Contains(t, out, "✓ Source CLI token has all required scopes")
		assert.NotContains(t, out, "Target CLI token")
	})

	t.Run("validates a configured target against its own API", func(t *testing.T) {
		seedFileStore(t)
		source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-OAuth-Scopes", "admin:enterprise, admin:org")
			_, _ = w.Write([]byte(`{"login":"source-user"}`))
		}))
		defer source.Close()
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/user", r.URL.Path)
			assert.Equal(t, "Bearer target-token", r.Header.Get("Authorization"))
			w.Header().Set("X-OAuth-Scopes", "admin:enterprise, admin:org")
			_, _ = w.Write([]byte(`{"login":"target-user"}`))
		}))
		defer target.Close()

		require.NoError(t, (&config.Config{SourceURL: source.URL, TargetURL: target.URL}).Save())
		store, err := creds.NewStore()
		require.NoError(t, err)
		require.NoError(t, store.Set(creds.SourceToken, "source-token"))
		require.NoError(t, store.Set(creds.TargetToken, "target-token"))

		out, err := execConfigure("confirm-access", "--check")
		require.NoError(t, err)
		assert.Contains(t, out, "✓ Source CLI token has all required scopes")
		assert.Contains(t, out, "✓ Target CLI token has all required scopes")
	})

	t.Run("missing source token fails without making a request", func(t *testing.T) {
		seedFileStore(t)
		var requests atomic.Int32
		source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requests.Add(1)
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer source.Close()
		require.NoError(t, (&config.Config{SourceURL: source.URL}).Save())

		out, err := execConfigure("confirm-access", "--check")
		require.Error(t, err)
		assert.Contains(t, out, "✗ Source CLI token is invalid: token is not configured")
		assert.Zero(t, requests.Load())
	})

	t.Run("missing target scopes fail and identify the scopes", func(t *testing.T) {
		seedFileStore(t)
		source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-OAuth-Scopes", "admin:enterprise, admin:org")
			_, _ = w.Write([]byte(`{"login":"source-user"}`))
		}))
		defer source.Close()
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-OAuth-Scopes", "admin:enterprise")
			_, _ = w.Write([]byte(`{"login":"target-user"}`))
		}))
		defer target.Close()

		require.NoError(t, (&config.Config{SourceURL: source.URL, TargetURL: target.URL}).Save())
		store, err := creds.NewStore()
		require.NoError(t, err)
		require.NoError(t, store.Set(creds.SourceToken, "source-token"))
		require.NoError(t, store.Set(creds.TargetToken, "target-token"))

		out, err := execConfigure("confirm-access", "--check")
		require.Error(t, err)
		assert.Contains(t, out, "✗ Target CLI token is missing required scopes: admin:org")
	})

	t.Run("unverifiable credentials fail with a warning", func(t *testing.T) {
		seedFileStore(t)
		source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"login":"source-user"}`))
		}))
		defer source.Close()

		require.NoError(t, (&config.Config{SourceURL: source.URL}).Save())
		store, err := creds.NewStore()
		require.NoError(t, err)
		require.NoError(t, store.Set(creds.SourceToken, "source-token"))

		out, err := execConfigure("confirm-access", "--check")
		require.Error(t, err)
		assert.Contains(t, out, "⚠ Could not validate Source CLI token scopes")
	})
}

func TestConfirmAccessInteractiveMode(t *testing.T) {
	t.Run("check flag disables prompts on a terminal", func(t *testing.T) {
		assert.False(t, confirmAccessIsInteractive(true, true, true))
	})

	t.Run("both input and output must be terminals", func(t *testing.T) {
		assert.True(t, confirmAccessIsInteractive(false, true, true))
		assert.False(t, confirmAccessIsInteractive(false, false, true))
		assert.False(t, confirmAccessIsInteractive(false, true, false))
	})
}

func TestHandleConfirmAccessFailure(t *testing.T) {
	failure := errors.New("access confirmation failed")

	t.Run("noninteractive mode returns the failure without prompting", func(t *testing.T) {
		prompted := false
		err := handleConfirmAccessFailure(
			failure,
			false,
			func() (bool, error) {
				prompted = true
				return true, nil
			},
			func() error { return nil },
			func() error { return nil },
		)
		require.ErrorIs(t, err, failure)
		assert.False(t, prompted)
	})

	t.Run("interactive acceptance runs the wizard and rechecks access", func(t *testing.T) {
		wizardRan := false
		rechecked := false
		err := handleConfirmAccessFailure(
			failure,
			true,
			func() (bool, error) { return true, nil },
			func() error {
				wizardRan = true
				return nil
			},
			func() error {
				rechecked = true
				return nil
			},
		)
		require.NoError(t, err)
		assert.True(t, wizardRan)
		assert.True(t, rechecked)
	})

	t.Run("failed recheck remains an error after the wizard", func(t *testing.T) {
		recheckFailure := errors.New("environment token is still invalid")
		err := handleConfirmAccessFailure(
			failure,
			true,
			func() (bool, error) { return true, nil },
			func() error { return nil },
			func() error { return recheckFailure },
		)
		require.ErrorIs(t, err, recheckFailure)
	})

	t.Run("interactive rejection returns the failure", func(t *testing.T) {
		err := handleConfirmAccessFailure(
			failure,
			true,
			func() (bool, error) { return false, nil },
			func() error { return nil },
			func() error { return nil },
		)
		require.ErrorIs(t, err, failure)
	})
}

func TestConfigureCompatibility(t *testing.T) {
	t.Run("legacy configure --show still works", func(t *testing.T) {
		seedFileStore(t)

		root := NewRootCmd("test")
		var output bytes.Buffer
		root.SetOut(&output)
		root.SetErr(&output)
		root.SetArgs([]string{"configure", "--show"})

		require.NoError(t, root.Execute())
		assert.Contains(t, output.String(), "Source (GHES)")
	})

	t.Run("legacy configure is hidden from root help", func(t *testing.T) {
		root := NewRootCmd("test")
		var output bytes.Buffer
		root.SetOut(&output)
		root.SetArgs([]string{"--help"})

		require.NoError(t, root.Execute())
		assert.NotContains(t, output.String(), "  configure:")
	})

	t.Run("legacy mode flags are hidden from config help", func(t *testing.T) {
		root := NewRootCmd("test")
		var output bytes.Buffer
		root.SetOut(&output)
		root.SetArgs([]string{"config", "--help"})

		require.NoError(t, root.Execute())
		assert.NotContains(t, output.String(), "--show")
		assert.NotContains(t, output.String(), "--reset")
	})
}

func TestConfigureReset(t *testing.T) {
	seedFileStore(t)
	require.NoError(t, (&config.Config{SourceURL: "https://ghes.example.com", TargetURL: "https://api.tenant.ghe.com"}).Save(), "seed config")
	store, err := creds.NewStore()
	require.NoError(t, err, "NewStore")
	require.NoError(t, store.Set(creds.SourceToken, "ghp_source"))
	require.NoError(t, store.Set(creds.TargetToken, "ghp_target"))

	out, err := execConfigure("reset")
	require.NoError(t, err, "config reset")
	assert.False(t, strings.HasPrefix(out, "\n"))
	assert.True(t, strings.HasSuffix(out, "\n\n"))
	assert.Contains(t, out, "✓ Cleared", "expected a successful cleared message")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Empty(t, cfg.SourceURL, "SourceURL not cleared")
	assert.Empty(t, cfg.TargetURL, "TargetURL not cleared")

	store2, err := creds.NewStore()
	require.NoError(t, err)
	v, _ := store2.Get(creds.SourceToken)
	assert.Empty(t, v, "source token not cleared")
	v, _ = store2.Get(creds.TargetToken)
	assert.Empty(t, v, "target token not cleared")
}

func TestConfigureShowAndResetMutuallyExclusive(t *testing.T) {
	seedFileStore(t)
	root := NewRootCmd("test")
	root.SetArgs([]string{"configure", "--show", "--reset"})
	err := root.Execute()
	require.Error(t, err, "expected an error when --show and --reset are combined")
	assert.Contains(t, err.Error(), "show", "error should name the conflicting flags")
	assert.Contains(t, err.Error(), "reset", "error should name the conflicting flags")
}

func TestValidateURL(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		for _, s := range []string{
			"https://ghes.example.com",
			"http://localhost:8080",
			"https://api.tenant.ghe.com/",
		} {
			t.Run(s, func(t *testing.T) {
				assert.NoError(t, validateURL(s))
			})
		}
	})

	t.Run("invalid", func(t *testing.T) {
		for _, s := range []string{"", "   ", "ftp://host", "notaurl", "https://"} {
			t.Run(s, func(t *testing.T) {
				require.Error(t, validateURL(s))
			})
		}
	})
}
