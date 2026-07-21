package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GH_ELM_CONFIG_DIR", dir)

	// A missing file loads as an empty config.
	got, err := Load()
	if err != nil {
		t.Fatalf("Load on empty dir: %v", err)
	}
	if got.SourceURL != "" || got.TargetURL != "" {
		t.Fatalf("expected empty config, got %+v", got)
	}

	want := &Config{SourceURL: "https://ghes.example.com", TargetURL: "https://api.tenant.ghe.com"}
	if err := want.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err = Load()
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if *got != *want {
		t.Errorf("round trip mismatch: got %+v want %+v", got, want)
	}
}

func TestSavePermissions(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GH_ELM_CONFIG_DIR", dir)

	if err := (&Config{SourceURL: "https://x"}).Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config file perm = %o, want 600", perm)
	}
}

func TestDirPrecedence(t *testing.T) {
	t.Setenv("GH_ELM_CONFIG_DIR", "/tmp/elm-explicit")
	t.Setenv("GH_CONFIG_DIR", "/tmp/gh")
	d, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	if d != "/tmp/elm-explicit" {
		t.Errorf("GH_ELM_CONFIG_DIR should win, got %q", d)
	}

	t.Setenv("GH_ELM_CONFIG_DIR", "")
	d, err = Dir()
	if err != nil {
		t.Fatal(err)
	}
	if d != "/tmp/gh/gh-elm" {
		t.Errorf("GH_CONFIG_DIR should be used, got %q", d)
	}
}
