// Package theme defines semantic styles based on the gh CLI's ANSI palette.
package theme

import (
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

const (
	colorRed    = lipgloss.Color("1")
	colorGreen  = lipgloss.Color("2")
	colorYellow = lipgloss.Color("3")
	colorCyan   = lipgloss.Color("6")
	colorBlack  = lipgloss.Color("0")
	colorMuted  = lipgloss.Color("242")
)

// Styles is the set of semantic styles used across the extension.
type Styles struct {
	// Bold emphasises without colour, for identifiers and headings.
	Bold lipgloss.Style
	// Info marks field labels and section headings — structural chrome rather
	// than status.
	Info lipgloss.Style
	// Muted de-emphasises secondary text: timestamps, hints, pending items.
	Muted lipgloss.Style
	// Success marks a completed or passing item.
	Success lipgloss.Style
	// Active marks work currently in progress.
	Active lipgloss.Style
	// Warning marks something that needs attention but is not a failure.
	Warning lipgloss.Style
	// Paused marks work deliberately halted. It shares Warning's colour
	// because both mean the migration is not progressing normally; the glyph
	// at the call site is what separates it from Active, which is the same
	// colour but the opposite state.
	Paused lipgloss.Style
	// Failure marks a failed or blocking item.
	Failure lipgloss.Style
}

// New returns the `gh elm` styles.
func New() Styles {
	return Styles{
		Bold:    lipgloss.NewStyle().Bold(true),
		Info:    lipgloss.NewStyle().Foreground(colorCyan),
		Muted:   lipgloss.NewStyle().Foreground(colorMuted),
		Success: lipgloss.NewStyle().Foreground(colorGreen),
		Active:  lipgloss.NewStyle().Foreground(colorYellow),
		Warning: lipgloss.NewStyle().Foreground(colorYellow),
		Paused:  lipgloss.NewStyle().Foreground(colorYellow),
		Failure: lipgloss.NewStyle().Foreground(colorRed),
	}
}

// Form returns the huh theme for interactive prompts.
func Form() *huh.Theme {
	s := New()
	t := huh.ThemeBase16()

	t.Focused.Base = t.Focused.Base.BorderForeground(colorMuted)
	t.Focused.Card = t.Focused.Base
	t.Focused.Title = t.Focused.Title.Foreground(colorCyan)
	t.Focused.NoteTitle = t.Focused.NoteTitle.Foreground(colorCyan)
	t.Focused.Directory = t.Focused.Directory.Foreground(colorCyan)
	t.Focused.Description = s.Muted
	t.Focused.ErrorIndicator = t.Focused.ErrorIndicator.Foreground(colorRed)
	t.Focused.ErrorMessage = t.Focused.ErrorMessage.Foreground(colorRed)
	t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(colorCyan)
	t.Focused.NextIndicator = t.Focused.NextIndicator.Foreground(colorCyan)
	t.Focused.PrevIndicator = t.Focused.PrevIndicator.Foreground(colorCyan)
	t.Focused.Option = t.Focused.Option.UnsetForeground()
	t.Focused.MultiSelectSelector = t.Focused.MultiSelectSelector.Foreground(colorCyan)
	t.Focused.SelectedOption = t.Focused.SelectedOption.Foreground(colorGreen)
	t.Focused.SelectedPrefix = t.Focused.SelectedPrefix.Foreground(colorGreen)
	t.Focused.UnselectedOption = t.Focused.UnselectedOption.UnsetForeground()
	t.Focused.FocusedButton = t.Focused.FocusedButton.Foreground(colorBlack).Background(colorGreen)
	t.Focused.BlurredButton = t.Focused.BlurredButton.Foreground(colorMuted).Background(colorBlack)
	t.Focused.TextInput.Cursor = t.Focused.TextInput.Cursor.Foreground(colorCyan)
	t.Focused.TextInput.Placeholder = t.Focused.TextInput.Placeholder.Foreground(colorMuted)
	t.Focused.TextInput.Prompt = t.Focused.TextInput.Prompt.Foreground(colorCyan)

	// huh derives blurred styles from the focused styles.
	t.Blurred = t.Focused
	t.Blurred.Base = t.Blurred.Base.BorderStyle(lipgloss.HiddenBorder())
	t.Blurred.Card = t.Blurred.Base
	t.Blurred.Title = t.Blurred.Title.Foreground(colorMuted)
	t.Blurred.NoteTitle = t.Blurred.NoteTitle.Foreground(colorMuted)
	t.Blurred.TextInput.Prompt = t.Blurred.TextInput.Prompt.Foreground(colorMuted)
	t.Blurred.TextInput.Text = t.Blurred.TextInput.Text.UnsetForeground()
	t.Blurred.NextIndicator = lipgloss.NewStyle()
	t.Blurred.PrevIndicator = lipgloss.NewStyle()

	t.Group.Title = t.Focused.Title
	t.Group.Description = t.Focused.Description
	t.Help.Ellipsis = s.Muted
	t.Help.ShortKey = s.Muted
	t.Help.ShortDesc = s.Muted
	t.Help.ShortSeparator = s.Muted
	t.Help.FullKey = s.Muted
	t.Help.FullDesc = s.Muted
	t.Help.FullSeparator = s.Muted

	return t
}
