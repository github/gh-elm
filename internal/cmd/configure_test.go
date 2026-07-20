package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

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
	if err == nil {
		t.Fatal("expected an error when the credential store cannot be read")
	}
	if !strings.Contains(err.Error(), "malformed credentials file") {
		t.Errorf("error should surface the backend failure, got: %v", err)
	}
	if strings.Contains(buf.String(), "not set") {
		t.Errorf("a read failure must not be reported as \"not set\"; output:\n%s", buf.String())
	}
}

// execConfigure runs `gh elm configure <args>` through the root command and
// returns combined output. Callers set GH_ELM_CONFIG_DIR / GH_ELM_CREDENTIAL_STORE
// (and seed state) beforehand so the run is isolated to a temp dir.
func execConfigure(args ...string) (string, error) {
	root := NewRootCmd("test")
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(append([]string{"configure"}, args...))
	err := root.Execute()
	return buf.String(), err
}

func seedFileStore(t *testing.T) {
	t.Helper()
	t.Setenv("GH_ELM_CONFIG_DIR", t.TempDir())
	t.Setenv("GH_ELM_CREDENTIAL_STORE", "file")
}

func TestConfigureShow(t *testing.T) {
	seedFileStore(t)
	if err := (&config.Config{SourceURL: "https://ghes.example.com"}).Save(); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	store, err := creds.NewStore()
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.Set(creds.SourceToken, "ghp_source"); err != nil {
		t.Fatalf("seed token: %v", err)
	}

	out, err := execConfigure("--show")
	if err != nil {
		t.Fatalf("configure --show: %v", err)
	}
	if !strings.Contains(out, "https://ghes.example.com") {
		t.Errorf("missing source url in output:\n%s", out)
	}
	if !strings.Contains(out, "set (hidden)") {
		t.Errorf("seeded source token should read as set:\n%s", out)
	}
	if !strings.Contains(out, "not set") {
		t.Errorf("unset target token should read as not set:\n%s", out)
	}
}

func TestConfigureReset(t *testing.T) {
	seedFileStore(t)
	if err := (&config.Config{SourceURL: "https://ghes.example.com", TargetURL: "https://api.tenant.ghe.com"}).Save(); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	store, err := creds.NewStore()
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.Set(creds.SourceToken, "ghp_source"); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(creds.TargetToken, "ghp_target"); err != nil {
		t.Fatal(err)
	}

	out, err := execConfigure("--reset")
	if err != nil {
		t.Fatalf("configure --reset: %v", err)
	}
	if !strings.Contains(out, "Cleared") {
		t.Errorf("expected a cleared message, got:\n%s", out)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SourceURL != "" || cfg.TargetURL != "" {
		t.Errorf("config not cleared: %+v", cfg)
	}
	store2, err := creds.NewStore()
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := store2.Get(creds.SourceToken); v != "" {
		t.Errorf("source token not cleared: %q", v)
	}
	if v, _ := store2.Get(creds.TargetToken); v != "" {
		t.Errorf("target token not cleared: %q", v)
	}
}

func TestConfigureShowAndResetMutuallyExclusive(t *testing.T) {
	seedFileStore(t)
	_, err := execConfigure("--show", "--reset")
	if err == nil {
		t.Fatal("expected an error when --show and --reset are combined")
	}
	if !strings.Contains(err.Error(), "show") || !strings.Contains(err.Error(), "reset") {
		t.Errorf("error should name the conflicting flags, got: %v", err)
	}
}

func TestValidateURL(t *testing.T) {
	valid := []string{
		"https://ghes.example.com",
		"http://localhost:8080",
		"https://api.tenant.ghe.com/",
	}
	for _, s := range valid {
		if err := validateURL(s); err != nil {
			t.Errorf("validateURL(%q) = %v, want nil", s, err)
		}
	}

	invalid := []string{"", "   ", "ftp://host", "notaurl", "https://"}
	for _, s := range invalid {
		if err := validateURL(s); err == nil {
			t.Errorf("validateURL(%q) = nil, want error", s)
		}
	}
}
