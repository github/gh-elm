// Package endpoints resolves the source (GHES) and target (GitHub with Data Residency) API
// endpoints — a base URL and token — applying gh-elm's precedence order:
//
//	explicit flag  >  environment variable  >  stored config/credentials
//
// The environment variables (GH_SOURCE_HOST/GH_SOURCE_TOKEN,
// GH_TARGET_HOST/GH_TARGET_TOKEN) are a working override so scripts and CI can
// bypass `gh elm config` entirely.
package endpoints

import (
	"net"
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

// Target resolves the target (GitHub with Data Residency) endpoint. flagURL and flagToken take
// highest precedence; pass "" when they are not set. The resolved URL accepts
// either the target web hostname or its api. hostname.
func (r *Resolver) Target(flagURL, flagToken string) (Endpoint, error) {
	ep, err := r.resolve(flagURL, flagToken, config.EnvTargetURL, config.EnvTargetToken, r.cfg.TargetURL, creds.TargetToken)
	if err != nil {
		return ep, err
	}
	ep.URL = NormalizeTargetAPIURL(ep.URL)
	return ep, nil
}

func (r *Resolver) resolve(flagURL, flagToken, envURL, envToken, storedURL, tokenKey string) (Endpoint, error) {
	endpointURL := firstNonEmpty(flagURL, os.Getenv(envURL), storedURL)

	token := firstNonEmpty(flagToken, os.Getenv(envToken))
	if token == "" {
		stored, err := r.store.Get(tokenKey)
		if err != nil {
			return Endpoint{}, err
		}
		token = stored
	}

	return Endpoint{URL: normalizeBaseURL(endpointURL), Token: token}, nil
}

// normalizeBaseURL makes a resolved base URL usable by the HTTP client. The
// GH_SOURCE_HOST/GH_TARGET_HOST variables (and their configure equivalents) may
// be given as a bare host with no scheme; default those to https so the client
// does not fail with an "unsupported protocol scheme" error. Trailing forward
// or backward slashes are removed so shell-escaped and accidentally Windows-
// styled suffixes cannot become part of the host name.
func normalizeBaseURL(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.TrimRight(s, `/\`)
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

// NormalizeTargetAPIURL turns a target web hostname into its API hostname.
// Existing API hostnames and local development endpoints are left unchanged.
func NormalizeTargetAPIURL(raw string) string {
	raw = normalizeBaseURL(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}

	hostname := u.Hostname()
	if hostname == "" || strings.EqualFold(hostname, "localhost") || net.ParseIP(hostname) != nil {
		return raw
	}
	firstLabel, _, _ := strings.Cut(hostname, ".")
	if strings.EqualFold(firstLabel, "api") {
		return raw
	}

	u.Host = "api." + u.Host
	return u.String()
}
