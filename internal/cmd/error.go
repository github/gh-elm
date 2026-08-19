package cmd

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

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
	context := strings.TrimSuffix(err.Error(), httpErr.Error())
	context = strings.TrimSuffix(context, ": ")
	if context == "" {
		return httpErr.Message
	}
	if httpErr.Message == "" {
		return context
	}
	return context + ": " + httpErr.Message
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
	return strings.ToUpper(statusText[:1]) + statusText[1:]
}
