// Package preflight checks API connectivity and authentication before commands
// perform ELM operations.
package preflight

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/github/gh-elm/internal/elmapi"
)

// CheckEndpoint reports network reachability, API service health, and
// authentication independently. It runs every applicable check even after a
// failure so the output is useful for diagnosing configuration problems.
func CheckEndpoint(ctx context.Context, stderr io.Writer, name, baseURL, token string) error {
	name = strings.TrimSpace(name)
	if baseURL == "" {
		fmt.Fprintf(stderr, "[FAIL] %s network: URL is not configured\n", name)
		fmt.Fprintf(stderr, "[FAIL] %s service: not checked because the URL is not configured\n", name)
		fmt.Fprintf(stderr, "[FAIL] %s authentication: not checked because the URL is not configured\n", name)
		return fmt.Errorf("%s preflight failed: URL is not configured; run `gh elm config`", strings.ToLower(name))
	}

	userEndpoint := strings.TrimRight(baseURL, "/") + "/user"
	if request, err := http.NewRequest(http.MethodGet, userEndpoint, nil); err == nil {
		request.URL.User = nil
		userEndpoint = request.URL.String()
	}
	client := elmapi.NewClient(baseURL, token)
	authenticationErr := client.CheckAuthentication(ctx)
	var httpErr *elmapi.HTTPError
	switch {
	case authenticationErr == nil:
		fmt.Fprintf(stderr, "[OK] %s network: reachable (HTTP 200 OK)\n", name)
		fmt.Fprintf(stderr, "[OK] %s service: responding (HTTP 200 OK)\n", name)
	case errors.As(authenticationErr, &httpErr):
		status := fmt.Sprintf("HTTP %d %s", httpErr.StatusCode, http.StatusText(httpErr.StatusCode))
		fmt.Fprintf(stderr, "[OK] %s network: reachable (%s)\n", name, status)
		if httpErr.StatusCode >= http.StatusInternalServerError {
			fmt.Fprintf(stderr, "[FAIL] %s service: unhealthy (%s)\n", name, status)
		} else {
			fmt.Fprintf(stderr, "[OK] %s service: responding (%s)\n", name, status)
		}
	default:
		fmt.Fprintf(stderr, "[FAIL] %s network: %v\n", name, authenticationErr)
		fmt.Fprintf(stderr, "[FAIL] %s service: not checked because the network probe failed\n", name)
	}

	switch {
	case token == "":
		fmt.Fprintf(stderr, "[FAIL] %s authentication: token is not configured\n", name)
	case authenticationErr != nil:
		fmt.Fprintf(stderr, "[FAIL] %s authentication: %s\n", name, authenticationFailure(authenticationErr, userEndpoint))
	default:
		fmt.Fprintf(stderr, "[OK] %s authentication: credentials accepted by %s\n", name, userEndpoint)
		return nil
	}

	return fmt.Errorf("%s preflight failed; see checks above", strings.ToLower(name))
}

func authenticationFailure(err error, userEndpoint string) string {
	var httpErr *elmapi.HTTPError
	if !errors.As(err, &httpErr) {
		return fmt.Sprintf("could not verify credentials: %v", err)
	}

	status := fmt.Sprintf("HTTP %d %s", httpErr.StatusCode, http.StatusText(httpErr.StatusCode))
	switch httpErr.StatusCode {
	case http.StatusUnauthorized:
		return status + "; the configured token was rejected or has expired"
	case http.StatusForbidden:
		return status + "; the configured token lacks access to " + userEndpoint
	default:
		if httpErr.StatusCode >= http.StatusInternalServerError {
			return status + "; credentials could not be verified because the API service is unhealthy"
		}
		return fmt.Sprintf("%s: %s", status, httpErr.Message)
	}
}
