package cmd

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/github/gh-elm/internal/elmapi"
)

func TestFormatError(t *testing.T) {
	t.Run("renders a structured API error", func(t *testing.T) {
		err := fmt.Errorf("creating migration: %w", &elmapi.HTTPError{
			StatusCode:       503,
			Status:           "503 Service Unavailable",
			Message:          "Error creating migration: The migration service is temporarily unavailable. Please retry later.",
			DocumentationURL: "https://docs.github.com/enterprise-server@3.22/rest/enterprise-admin/live-migrations",
			CorrelationID:    "c515203b-1baf-46ff-bac8-44cab2af11f2",
		})

		assert.Equal(t, `Error (503 - Service unavailable)
creating migration: Error creating migration: The migration service is temporarily unavailable. Please retry later.
Correlation ID: c515203b-1baf-46ff-bac8-44cab2af11f2
Documentation: https://docs.github.com/enterprise-server@3.22/rest/enterprise-admin/live-migrations
`, FormatError(err))
	})

	t.Run("preserves context when a created migration fails to start", func(t *testing.T) {
		err := fmt.Errorf("migration mig-1 created but failed to start: %w", &elmapi.HTTPError{
			StatusCode: 503,
			Status:     "503 Service Unavailable",
			Message:    "The migration service is temporarily unavailable. Please retry later.",
		})

		assert.Equal(t, `Error (503 - Service unavailable)
migration mig-1 created but failed to start: The migration service is temporarily unavailable. Please retry later.
`, FormatError(err))
	})

	t.Run("preserves the existing format for other errors", func(t *testing.T) {
		assert.Equal(t, "Error: something broke\n", FormatError(errors.New("something broke")))
	})

	t.Run("capitalizes a non-ASCII fallback status", func(t *testing.T) {
		assert.Equal(t, "Échec", formatHTTPStatus(0, "0 ÉCHEC"))
	})
}
