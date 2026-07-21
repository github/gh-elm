// Package endpoints resolves the source (GHES) and target (GHEC/Proxima) API
// endpoints — a base URL and token — applying gh-elm's precedence order:
//
//	explicit flag  >  environment variable  >  stored config/credentials
//
// The environment variables (GH_SOURCE_HOST/GH_SOURCE_TOKEN,
// GH_TARGET_HOST/GH_TARGET_TOKEN) are a working override so scripts and CI can
// bypass `gh elm configure` entirely.
package endpoints

import (
	"net/url"
	"os"
	"strings"

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
// precedence; pass "" when they are not set. The resolved base URL is normalized
// to the GHES REST root (scheme defaulted to https, /api/v3 appended when the
// URL carries no path) so a bare host such as GH_SOURCE_HOST=ghes.example.com is
// usable directly.
func (r *Resolver) Source(flagURL, flagToken string) (Endpoint, error) {
	ep, err := r.resolve(flagURL, flagToken, config.EnvSourceURL, config.EnvSourceToken, r.cfg.SourceURL, creds.SourceToken)
	if err != nil {
		return ep, err
	}
	ep.URL = ensureGHESRESTBase(ep.URL)
	return ep, nil
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

	return Endpoint{URL: normalizeBaseURL(url), Token: token}, nil
}

// normalizeBaseURL makes a resolved base URL usable by the HTTP client. The
// GH_SOURCE_HOST/GH_TARGET_HOST variables (and their configure equivalents) may
// be given as a bare host with no scheme; default those to https so the client
// does not fail with an "unsupported protocol scheme" error. A URL that already
// carries a scheme is returned unchanged.
func normalizeBaseURL(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	return s
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// ensureGHESRESTBase turns a GHES appliance base URL into its REST API root by
// appending /api/v3 when the URL carries no path. GH_SOURCE_HOST is typically a
// bare host (for example ghes.example.com); the GHES REST API is served under
// /api/v3, so a base without a path would otherwise hit the web frontend and
// fail (a 302 redirect, or 406 for a JSON Accept header). A base that already
// carries a path (for example .../api/v3) is left untouched.
func ensureGHESRESTBase(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = "/api/v3"
	}
	return u.String()
}
