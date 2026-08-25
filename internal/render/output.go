// Package render provides typed, composable terminal output for gh elm.
package render

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/github/gh-elm/internal/theme"
)

// Field is a labeled value in a human-readable document.
type Field struct {
	Label string
	Value string
}

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

// Warning renders a compact warning suitable for command progress streams.
func Warning(message string) string {
	return theme.New().Warning.Render("!") + " " + message
}

// Fields renders consistently aligned labeled values.
func Fields(fields ...Field) string {
	width := 0
	for _, field := range fields {
		if field.Value == "" {
			continue
		}
		width = max(width, len(field.Label))
	}

	var output strings.Builder
	for _, field := range fields {
		if field.Value == "" {
			continue
		}
		fmt.Fprintf(&output, "  %-*s  %s\n", width, field.Label, field.Value)
	}
	return strings.TrimSuffix(output.String(), "\n")
}

// WriteRawJSON writes an API JSON document with two-space indentation followed
// by a newline.
func WriteRawJSON(out io.Writer, raw json.RawMessage) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil
	}

	var formatted bytes.Buffer
	if err := json.Indent(&formatted, raw, "", "  "); err != nil {
		return fmt.Errorf("formatting JSON response: %w", err)
	}
	formatted.WriteByte('\n')
	if _, err := out.Write(formatted.Bytes()); err != nil {
		return fmt.Errorf("writing JSON response: %w", err)
	}
	return nil
}
