package ghapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ErrTokenScopesUnavailable indicates that GitHub authenticated the request but
// did not expose classic PAT scopes in the response headers.
var ErrTokenScopesUnavailable = errors.New("token scopes are not available in the API response")

// TokenScopes returns the classic OAuth scopes granted to the client's token.
func (c *Client) TokenScopes(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/user", http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", acceptHeader)
	req.Header.Set(apiVersionHeader, apiVersion)
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, fmt.Errorf("reading response: %w", readErr)
		}
		return nil, &HTTPError{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			Message:    truncate(strings.TrimSpace(string(body)), maxErrorBody),
		}
	}

	values, ok := resp.Header[http.CanonicalHeaderKey("X-OAuth-Scopes")]
	if !ok {
		return nil, ErrTokenScopesUnavailable
	}

	var scopes []string
	for scope := range strings.SplitSeq(strings.Join(values, ","), ",") {
		if scope = strings.TrimSpace(scope); scope != "" {
			scopes = append(scopes, scope)
		}
	}
	return scopes, nil
}
