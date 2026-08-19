// Package render provides typed, composable terminal output for gh elm.
package render

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/github/gh-elm/internal/theme"
)

// Write writes a fully rendered human-readable document.
func Write(out io.Writer, document string) error {
	_, err := io.WriteString(out, document)
	return err
}

// Success renders a successful-operation confirmation.
func Success(message string) string {
	return theme.New().Success.Render("✓") + " " + message
}

// WriteRawJSON writes an API JSON document followed by a newline.
func WriteRawJSON(out io.Writer, raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	if _, err := out.Write(raw); err != nil {
		return err
	}
	_, err := fmt.Fprintln(out)
	return err
}
