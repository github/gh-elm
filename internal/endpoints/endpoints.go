// Package endpoints resolves the source (GHES) and target (GHEC/Proxima) API
// endpoints — a base URL and token — applying gh-elm's precedence order:
//
//	explicit flag  >  environment variable  >  stored config/credentials
//
// This keeps the elm CLI's environment variables (GHES_URL/GHES_TOKEN,
// MIGRATION_TARGET_URL/MIGRATION_TARGET_TOKEN) as a working override so scripts
// and CI can bypass `gh elm configure` entirely.
package endpoints

import (
	"os"

	"github.com/github/gh-elm/internal/config"
	"github.com/github/gh-elm/internal/creds"
)

// Endpoint is a resolved API base URL and token.
type Endpoint struct {
	URL   string
	Token string
}

// Resolver combines stored config, stored credentials, and the environment.
type Resolver struct {
	cfg   *config.Config
	store creds.Store
}

// NewResolver loads the stored config and opens the default credential store.
func NewResolver() (*Resolver, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	store, err := creds.NewStore()
	if err != nil {
		return nil, err
	}
	return &Resolver{cfg: cfg, store: store}, nil
}

// Source resolves the source (GHES) endpoint. flagURL and flagToken take highest
// precedence; pass "" when they are not set.
func (r *Resolver) Source(flagURL, flagToken string) (Endpoint, error) {
	return r.resolve(flagURL, flagToken, config.EnvSourceURL, config.EnvSourceToken, r.cfg.SourceURL, creds.SourceToken)
}

// Target resolves the target (GHEC/Proxima) endpoint. flagURL and flagToken take
// highest precedence; pass "" when they are not set.
func (r *Resolver) Target(flagURL, flagToken string) (Endpoint, error) {
	return r.resolve(flagURL, flagToken, config.EnvTargetURL, config.EnvTargetToken, r.cfg.TargetURL, creds.TargetToken)
}

func (r *Resolver) resolve(flagURL, flagToken, envURL, envToken, storedURL, tokenKey string) (Endpoint, error) {
	url := firstNonEmpty(flagURL, os.Getenv(envURL), storedURL)

	token := firstNonEmpty(flagToken, os.Getenv(envToken))
	if token == "" {
		stored, err := r.store.Get(tokenKey)
		if err != nil {
			return Endpoint{}, err
		}
		token = stored
	}

	return Endpoint{URL: url, Token: token}, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
