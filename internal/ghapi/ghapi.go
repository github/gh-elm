// Package ghapi is a minimal GitHub GraphQL/REST client for the mannequin
// commands. Mannequins are a standard GitHub concept (not part of the ELM REST
// API), so these operations talk to the target's GraphQL endpoint
// ({base}/graphql) and a couple of REST endpoints, using the same target base
// URL and bearer token that the other `gh elm target` commands resolve.
package ghapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"
)

const (
	defaultTimeout = 30 * time.Second

	acceptHeader     = "application/vnd.github+json"
	apiVersionHeader = "X-GitHub-Api-Version"
	apiVersion       = "2022-11-28"

	// graphQLFeaturesHeader opts into preview GraphQL schema features. The
	// mannequin_claiming_emu feature exposes the reattributeMannequinToUser
	// mutation used by `mannequin reclaim --skip-invitation`; without it the
	// mutation is absent from the schema and the call fails with "doesn't exist
	// on type 'Mutation'". gh-gei sends this header on every request (it is
	// ignored by REST), so we do the same.
	graphQLFeaturesHeader = "GraphQL-Features"
	graphQLFeatures       = "mannequin_claiming_emu"

	// graphQLPageSize is the number of nodes requested per mannequins page.
	graphQLPageSize = 100

	// maxErrorBody bounds how much of an error response body we surface.
	maxErrorBody = 500
)

// Client talks to the target's GitHub GraphQL and REST APIs with a base URL and
// bearer token.
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

// HTTPError is returned when a request responds with an unexpected status.
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

// GraphQLError is returned when a GraphQL response carries an "errors" array.
// It reports the first message but retains all of them.
type GraphQLError struct {
	Messages []string
}

func (e *GraphQLError) Error() string {
	if len(e.Messages) == 0 {
		return "graphql error"
	}
	return "graphql error: " + strings.Join(e.Messages, "; ")
}

// graphQLRequest is the JSON body of a GraphQL POST.
type graphQLRequest struct {
	Query     string `json:"query"`
	Variables any    `json:"variables,omitempty"`
}

// graphQLResponse captures the parts of a GraphQL response gh-elm inspects.
type graphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// graphQL issues a GraphQL query/mutation and decodes response.data into out.
// A non-empty errors array is returned as a *GraphQLError.
func (c *Client) graphQL(ctx context.Context, query string, variables, out any) error {
	body, err := json.Marshal(graphQLRequest{Query: query, Variables: variables})
	if err != nil {
		return fmt.Errorf("encoding graphql request: %w", err)
	}

	var resp graphQLResponse
	if err := c.doJSON(ctx, http.MethodPost, c.baseURL+"/graphql", body, &resp); err != nil {
		return err
	}

	if len(resp.Errors) > 0 {
		msgs := make([]string, len(resp.Errors))
		for i, e := range resp.Errors {
			msgs[i] = e.Message
		}
		return &GraphQLError{Messages: msgs}
	}

	if out != nil {
		if err := json.Unmarshal(resp.Data, out); err != nil {
			return fmt.Errorf("decoding graphql data: %w", err)
		}
	}
	return nil
}

// restGet issues a GET request and decodes a JSON response into out. wantStatus
// values list the accepted status codes; a response with any other status is
// returned as an *HTTPError so callers can branch on, for example, a 404.
func (c *Client) restGet(ctx context.Context, path string, out any, wantStatus ...int) error {
	return c.doJSONStatus(ctx, http.MethodGet, c.baseURL+path, nil, out, wantStatus...)
}

// doJSON performs a request, treating any non-2xx status as an error.
func (c *Client) doJSON(ctx context.Context, method, endpoint string, body []byte, out any) error {
	return c.doJSONStatus(ctx, method, endpoint, body, out)
}

// doJSONStatus performs a request and decodes a JSON response into out. When
// wantStatus is empty any 2xx is accepted; otherwise only the listed codes are.
func (c *Client) doJSONStatus(ctx context.Context, method, endpoint string, body []byte, out any, wantStatus ...int) error {
	var reqBody io.Reader = http.NoBody
	if body != nil {
		reqBody = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, reqBody)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", acceptHeader)
	req.Header.Set(apiVersionHeader, apiVersion)
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set(graphQLFeaturesHeader, graphQLFeatures)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	if !statusAccepted(resp.StatusCode, wantStatus) {
		return &HTTPError{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			Message:    truncate(strings.TrimSpace(string(respBody)), maxErrorBody),
		}
	}

	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decoding response: %w", err)
		}
	}
	return nil
}

// statusAccepted reports whether code is acceptable. With no explicit want list,
// any 2xx is accepted.
func statusAccepted(code int, want []int) bool {
	if len(want) == 0 {
		return code >= 200 && code < 300
	}
	return slices.Contains(want, code)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
