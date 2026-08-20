// Package render provides typed, composable terminal output for gh elm.
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/github/gh-elm/internal/theme"
)

// Write writes a fully rendered human-readable document.
func Write(out io.Writer, document string) error {
	_, err := io.WriteString(out, Human(document))
	return err
}

// Human removes leading blank lines and ends a human-readable document with one
// blank line.
func Human(document string) string {
	document = strings.Trim(document, "\r\n")
	if document == "" {
		return ""
	}
	return document + "\n\n"
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
