package endpoints

import (
	"testing"

	"github.com/github/gh-elm/internal/config"
	"github.com/github/gh-elm/internal/creds"
)

func newTestResolver(t *testing.T) *Resolver {
	t.Helper()
	t.Setenv("GH_ELM_CONFIG_DIR", t.TempDir())
	t.Setenv("GH_ELM_CREDENTIAL_STORE", "file") // Force file backend to avoid real keyring.
	r, err := NewResolver()
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	return r
}

func TestSourcePrecedence(t *testing.T) {
	// Stored config + credentials are the lowest precedence.
	t.Setenv("GH_ELM_CONFIG_DIR", t.TempDir())
	t.Setenv("GH_ELM_CREDENTIAL_STORE", "file") // Force file backend to avoid polluting real keyring.
	if err := (&config.Config{SourceURL: "https://stored"}).Save(); err != nil {
		t.Fatal(err)
	}
	store, err := creds.NewStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(creds.SourceToken, "stored-token"); err != nil {
		t.Fatal(err)
	}

	r, err := NewResolver()
	if err != nil {
		t.Fatal(err)
	}

	// Stored only.
	ep, err := r.Source("", "")
	if err != nil {
		t.Fatal(err)
	}
	if ep.URL != "https://stored" || ep.Token != "stored-token" {
		t.Fatalf("stored resolution = %+v", ep)
	}

	// Env overrides stored.
	t.Setenv(config.EnvSourceURL, "https://env")
	t.Setenv(config.EnvSourceToken, "env-token")
	ep, _ = r.Source("", "")
	if ep.URL != "https://env" || ep.Token != "env-token" {
		t.Fatalf("env should override stored, got %+v", ep)
	}

	// Flag overrides env.
	ep, _ = r.Source("https://flag", "flag-token")
	if ep.URL != "https://flag" || ep.Token != "flag-token" {
		t.Fatalf("flag should override env, got %+v", ep)
	}
}

func TestTargetResolvesIndependently(t *testing.T) {
	r := newTestResolver(t)
	t.Setenv(config.EnvTargetURL, "https://target-env")
	t.Setenv(config.EnvTargetToken, "target-env-token")

	ep, err := r.Target("", "")
	if err != nil {
		t.Fatal(err)
	}
	if ep.URL != "https://target-env" || ep.Token != "target-env-token" {
		t.Fatalf("target resolution = %+v", ep)
	}
}
