package creds

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/github/gh-elm/internal/config"
)

// fileStore keeps secrets in a 0600 JSON file in gh-elm's config directory. It
// mirrors gh's behavior when an OS keyring is unavailable.
type fileStore struct {
	path string
}

func newFileStore() (*fileStore, error) {
	d, err := config.Dir()
	if err != nil {
		return nil, err
	}
	return &fileStore{path: filepath.Join(d, "credentials.json")}, nil
}

func (s *fileStore) read() (map[string]string, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", s.path, err)
	}
	m := map[string]string{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", s.path, err)
	}
	return m, nil
}

func (s *fileStore) write(m map[string]string) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(s.path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", s.path, err)
	}
	// os.WriteFile applies the mode only when creating; a pre-existing file
	// keeps its old permissions. Explicitly tighten so a restored or
	// miscreated 0644 file doesn't stay world-readable with tokens in it.
	if err := os.Chmod(s.path, 0o600); err != nil {
		return fmt.Errorf("setting permissions on %s: %w", s.path, err)
	}
	return nil
}

func (s *fileStore) Get(key string) (string, error) {
	m, err := s.read()
	if err != nil {
		return "", err
	}
	return m[key], nil
}

func (s *fileStore) Set(key, value string) error {
	m, err := s.read()
	if err != nil {
		return err
	}
	m[key] = value
	return s.write(m)
}

func (s *fileStore) Delete(key string) error {
	m, err := s.read()
	if err != nil {
		return err
	}
	if _, ok := m[key]; !ok {
		return nil
	}
	delete(m, key)
	return s.write(m)
}

func (s *fileStore) Location() string {
	return s.path
}
