// Package elmapi is a minimal REST client for the Enterprise Live Migrations
// (ELM) REST API. The same client type talks to either side of a migration —
// the source (GHES) API that backs the `migration` commands, or the target
// (GHEC/Proxima) API that backs the `target` commands — since both are GitHub
// REST APIs reached with a base URL and a bearer token. gh-elm resolves those
// two values elsewhere (see the endpoints package) and hands them to NewClient;
// the per-endpoint request/response types live in their own files (for example
// nodes.go for target migration nodes).
package elmapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultTimeout = 30 * time.Second

	acceptHeader     = "application/vnd.github+json"
	apiVersionHeader = "X-GitHub-Api-Version"
	apiVersion       = "2022-11-28"

	// maxErrorBody bounds how much of an error response body we surface.
	maxErrorBody = 500
)

// Client talks to an ELM REST API — either the source (GHES) or target
// (GHEC/Proxima) endpoint. It carries a base URL and bearer token and provides
// the shared request, auth, and error-handling plumbing; endpoint-specific
// operations are methods defined alongside their wire types.
type Client struct {
	baseURL    string
	token      string
	userAgent  string
	httpClient *http.Client
}

// Option customizes a Client.
type Option func(*Client)

// WithHTTPClient overrides the default HTTP client. Useful in tests.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

// WithUserAgent sets the User-Agent header sent with every request.
func WithUserAgent(ua string) Option {
	return func(c *Client) {
		if ua != "" {
			c.userAgent = ua
		}
	}
}

// NewClient builds a Client for the given base URL and bearer token.
func NewClient(baseURL, token string, opts ...Option) *Client {
	c := &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      token,
		userAgent:  "gh-elm",
		httpClient: &http.Client{Timeout: defaultTimeout},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// HTTPError is returned when the API responds with a non-2xx status.
type HTTPError struct {
	StatusCode int
	Status     string
	Message    string
}

func (e *HTTPError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("HTTP %d %s: %s", e.StatusCode, e.Status, e.Message)
	}
	return fmt.Sprintf("HTTP %d %s", e.StatusCode, e.Status)
}

// get issues a GET request and decodes a JSON response into out.
func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	endpoint := c.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", acceptHeader)
	req.Header.Set(apiVersionHeader, apiVersion)
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return &HTTPError{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			Message:    truncate(strings.TrimSpace(string(body)), maxErrorBody),
		}
	}

	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("decoding response: %w", err)
		}
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
