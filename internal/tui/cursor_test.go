package tui

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type terminalBuffer struct {
	bytes.Buffer
}

func (*terminalBuffer) Close() error {
	return nil
}

func (*terminalBuffer) Fd() uintptr {
	return 1
}

func TestNativeCursorOutput(t *testing.T) {
	t.Run("shows the native cursor at the requested position", func(t *testing.T) {
		var output bytes.Buffer
		writer := NativeCursorOutput(&output)
		render := "rendered output" + nativeCursorCommand("show", 12, 7)

		written, err := writer.Write([]byte(render))

		require.NoError(t, err)
		assert.Equal(t, len(render), written)
		assert.Equal(t, "rendered output"+ansi.CursorPosition(12, 7)+ansi.ShowCursor, output.String())
	})

	t.Run("hides the native cursor when no field is active", func(t *testing.T) {
		var output bytes.Buffer
		writer := NativeCursorOutput(&output)
		render := "rendered output" + nativeCursorCommand("hide", 0, 0)

		written, err := writer.Write([]byte(render))

		require.NoError(t, err)
		assert.Equal(t, len(render), written)
		assert.Equal(t, "rendered output"+ansi.HideCursor, output.String())
	})

	t.Run("does not alter Bubble Tea terminal setup or cleanup writes", func(t *testing.T) {
		var output bytes.Buffer
		writer := NativeCursorOutput(&output)

		written, err := writer.Write([]byte(ansi.ShowCursor))

		require.NoError(t, err)
		assert.Equal(t, len(ansi.ShowCursor), written)
		assert.Equal(t, ansi.ShowCursor, output.String())
	})

	t.Run("preserves terminal file capabilities", func(t *testing.T) {
		output := &terminalBuffer{}

		writer := NativeCursorOutput(output)

		require.Implements(t, (*term.File)(nil), writer)
		assert.Equal(t, output.Fd(), writer.(term.File).Fd())
	})
}

func TestNativeCursorView(t *testing.T) {
	t.Run("positions the cursor at the form marker", func(t *testing.T) {
		view := "first line\n  value" + nativeCursorPositionMarker + "\nlast line"

		render := nativeCursorView(view)
		output, command := stripNativeCursorCommands([]byte(render))

		assert.Equal(t, "first line\n  value\nlast line", string(output))
		assert.Equal(t, ansi.CursorPosition(8, 2)+ansi.ShowCursor, command)
	})

	t.Run("requests a hidden cursor without a form marker", func(t *testing.T) {
		render := nativeCursorView("plain view")

		output, command := stripNativeCursorCommands([]byte(render))

		assert.Equal(t, "plain view", string(output))
		assert.Equal(t, ansi.HideCursor, command)
	})

	t.Run("includes the cursor command on every rendered line", func(t *testing.T) {
		render := nativeCursorView("first line\nsecond line")

		assert.Equal(t, 2, strings.Count(render, nativeCursorCommandPrefix))
	})
}

func TestFormNativeCursor(t *testing.T) {
	value := "https://example.com"
	model := New(t.Context(), &fakeService{})
	model.width = 80
	model.height = 24
	model.screen = screenForm
	model.form = formState{
		title:   "Configuration",
		fields:  []formField{textFormField("Source URL", "", &value)},
		actions: formActions("save", "Save"),
	}

	t.Run("shows the native cursor on the focused text field", func(t *testing.T) {
		output, command := stripNativeCursorCommands([]byte(model.View()))

		assert.NotContains(t, string(output), nativeCursorPositionMarker)
		assert.Contains(t, command, ansi.ShowCursor)
	})

	t.Run("hides the native cursor on the action row", func(t *testing.T) {
		model.form.cursor = len(model.form.fields)

		_, command := stripNativeCursorCommands([]byte(model.View()))

		assert.Equal(t, ansi.HideCursor, command)
	})

	t.Run("hides the native cursor behind an overlay", func(t *testing.T) {
		model.form.cursor = 0
		model.alert = alertState{title: "Alert", body: "Something happened.", parent: screenForm}
		model.screen = screenAlert

		_, command := stripNativeCursorCommands([]byte(model.View()))

		assert.Equal(t, ansi.HideCursor, command)
	})

	t.Run("keeps the native cursor visible for long values", func(t *testing.T) {
		value = strings.Repeat("x", model.width*2) + "tail"
		model.screen = screenForm

		output, command := stripNativeCursorCommands([]byte(model.View()))
		position := strings.TrimSuffix(command, ansi.ShowCursor)
		var row, column int
		_, err := fmt.Sscanf(position, "\x1b[%d;%dH", &row, &column)

		require.NoError(t, err)
		assert.LessOrEqual(t, column, model.width)
		assert.Contains(t, string(output), "tail")
	})
}

func TestFocusedSecretField(t *testing.T) {
	value := ""
	model := New(t.Context(), &fakeService{})
	model.width = 80
	model.height = 24
	model.screen = screenForm
	model.form = formState{
		title:   "Configuration",
		fields:  []formField{secretFormField("Source token", &value, false)},
		actions: formActions("save", "Save"),
	}

	t.Run("does not show empty placeholder", func(t *testing.T) {
		output, command := stripNativeCursorCommands([]byte(model.View()))

		assert.NotContains(t, string(output), "(empty)")
		assert.Contains(t, command, ansi.ShowCursor)
	})

	t.Run("shows empty placeholder when unfocused", func(t *testing.T) {
		model.form.cursor = len(model.form.fields)

		output, _ := stripNativeCursorCommands([]byte(model.View()))

		assert.Contains(t, string(output), "(empty)")
	})

	t.Run("preserves already-set indicator", func(t *testing.T) {
		model.form.fields[0] = secretFormField("Source token", &value, true)
		model.form.cursor = 0

		output, command := stripNativeCursorCommands([]byte(model.View()))

		assert.Contains(t, string(output), "••••••••")
		assert.Contains(t, command, ansi.ShowCursor)
	})
}
