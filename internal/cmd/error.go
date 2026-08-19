package cmd

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/github/gh-elm/internal/elmapi"
	"github.com/github/gh-elm/internal/theme"
)

// FormatError renders an error for terminal display.
func FormatError(err error) string {
	var httpErr *elmapi.HTTPError
	if !errors.As(err, &httpErr) {
		return fmt.Sprintf("Error: %v\n", err)
	}

	styles := theme.New()
	var output strings.Builder
	fmt.Fprintf(
		&output,
		"%s\n%s\n",
		styles.Failure.Bold(true).Render(fmt.Sprintf(
			"Error (%d - %s)",
			httpErr.StatusCode,
			formatHTTPStatus(httpErr.StatusCode, httpErr.Status),
		)),
		formatHTTPErrorMessage(err, httpErr),
	)
	if httpErr.CorrelationID != "" {
		fmt.Fprintln(&output, styles.Muted.Render("Correlation ID: "+httpErr.CorrelationID))
	}
	if httpErr.DocumentationURL != "" {
		fmt.Fprintf(
			&output,
			"Documentation: %s\n",
			styles.Info.Underline(true).Render(httpErr.DocumentationURL),
		)
	}
	return output.String()
}

func formatHTTPErrorMessage(err error, httpErr *elmapi.HTTPError) string {
	prefix := strings.TrimSuffix(err.Error(), httpErr.Error())
	prefix = strings.TrimSuffix(prefix, ": ")
	if prefix == "" {
		return httpErr.Message
	}
	if httpErr.Message == "" {
		return prefix
	}
	return prefix + ": " + httpErr.Message
}

func formatHTTPStatus(statusCode int, status string) string {
	statusText := http.StatusText(statusCode)
	if statusText == "" {
		statusText = strings.TrimSpace(strings.TrimPrefix(status, fmt.Sprintf("%d", statusCode)))
	}
	if statusText == "" {
		return "unknown error"
	}

	statusText = strings.ToLower(statusText)
	first, size := utf8.DecodeRuneInString(statusText)
	return string(unicode.ToUpper(first)) + statusText[size:]
}
