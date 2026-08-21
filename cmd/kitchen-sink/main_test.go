package main

import (
	"bytes"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRun(t *testing.T) {
	t.Run("renders every human-readable response", func(t *testing.T) {
		var output bytes.Buffer
		require.NoError(t, run(&output, false))

		for _, want := range []string{
			"Migration successfully created",
			"Progress · target-org/monolith",
			"Live updates are falling behind.",
			"Completed",
			"Failed",
			"Cutover reverted",
		} {
			assert.Contains(t, output.String(), want)
		}
	})

	t.Run("renders ANSI colors when forced", func(t *testing.T) {
		previousProfile := lipgloss.ColorProfile()
		lipgloss.SetColorProfile(termenv.ANSI256)
		t.Cleanup(func() {
			lipgloss.SetColorProfile(previousProfile)
		})

		var output bytes.Buffer
		require.NoError(t, run(&output, false))
		assert.Contains(t, output.String(), "\x1b[")
	})

	t.Run("renders every formatted JSON response", func(t *testing.T) {
		var output bytes.Buffer
		require.NoError(t, run(&output, true))

		for _, want := range []string{
			`"migration_id": "7e3f16ca-44da-4f9a-a806-2c798a6afda7"`,
			`"repository_progress"`,
			`"migrations"`,
			`"unarchived_source_repository": true`,
		} {
			assert.Contains(t, output.String(), want)
		}
	})
}
