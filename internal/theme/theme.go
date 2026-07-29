// Package theme defines reusable semantic styles for gh elm.
package theme

import (
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

const (
	colorPrimary     = lipgloss.Color("#ffffff")
	colorRed         = lipgloss.Color("1")
	colorGreen       = lipgloss.Color("#1a7f37")
	colorYellow      = lipgloss.Color("3")
	colorBlack       = lipgloss.Color("0")
	colorPlaceholder = lipgloss.Color("238")
	colorMuted       = lipgloss.Color("242")
	colorSecondary   = lipgloss.Color("245")
)

// Styles is the set of semantic styles used across the extension.
type Styles struct {
	// Primary marks content that should have maximum contrast.
	Primary lipgloss.Style
	// Bold emphasises without colour, for identifiers and headings.
	Bold lipgloss.Style
	// Info marks field labels and section headings — structural chrome rather
	// than status.
	Info lipgloss.Style
	// Secondary distinguishes supporting text that should remain prominent.
	Secondary lipgloss.Style
	// Muted de-emphasises secondary text: timestamps, hints, pending items.
	Muted lipgloss.Style
	// Placeholder de-emphasises example input beneath surrounding help text.
	Placeholder lipgloss.Style
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
		Primary:     lipgloss.NewStyle().Foreground(colorPrimary),
		Bold:        lipgloss.NewStyle().Bold(true),
		Info:        lipgloss.NewStyle().Foreground(colorGreen),
		Secondary:   lipgloss.NewStyle().Foreground(colorSecondary),
		Muted:       lipgloss.NewStyle().Foreground(colorMuted),
		Placeholder: lipgloss.NewStyle().Foreground(colorPlaceholder),
		Success:     lipgloss.NewStyle().Foreground(colorGreen),
		Active:      lipgloss.NewStyle().Foreground(colorYellow),
		Warning:     lipgloss.NewStyle().Foreground(colorYellow),
		Paused:      lipgloss.NewStyle().Foreground(colorYellow),
		Failure:     lipgloss.NewStyle().Foreground(colorRed),
	}
}

// Form returns the huh theme for interactive prompts.
func Form() *huh.Theme {
	s := New()
	t := huh.ThemeBase16()

	t.Focused.Base = t.Focused.Base.BorderForeground(s.Primary.GetForeground())
	t.Focused.Card = t.Focused.Base
	t.Focused.Title = t.Focused.Title.Foreground(colorGreen).Bold(true)
	t.Focused.NoteTitle = s.Primary.Bold(true)
	t.Focused.Directory = t.Focused.Directory.Foreground(colorGreen)
	t.Focused.Description = s.Muted
	t.Focused.ErrorIndicator = t.Focused.ErrorIndicator.Foreground(colorRed)
	t.Focused.ErrorMessage = t.Focused.ErrorMessage.Foreground(colorRed)
	t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(colorGreen)
	t.Focused.NextIndicator = t.Focused.NextIndicator.Foreground(colorGreen)
	t.Focused.PrevIndicator = t.Focused.PrevIndicator.Foreground(colorGreen)
	t.Focused.Option = t.Focused.Option.UnsetForeground()
	t.Focused.MultiSelectSelector = t.Focused.MultiSelectSelector.Foreground(colorGreen)
	t.Focused.SelectedOption = t.Focused.SelectedOption.Foreground(colorGreen)
	t.Focused.SelectedPrefix = t.Focused.SelectedPrefix.Foreground(colorGreen)
	t.Focused.UnselectedOption = t.Focused.UnselectedOption.UnsetForeground()
	t.Focused.FocusedButton = t.Focused.FocusedButton.Foreground(colorBlack).Background(colorGreen)
	t.Focused.BlurredButton = t.Focused.BlurredButton.Foreground(colorMuted).Background(colorBlack)
	t.Focused.TextInput.Cursor = t.Focused.TextInput.Cursor.Foreground(colorGreen)
	t.Focused.TextInput.Placeholder = s.Placeholder
	t.Focused.TextInput.Prompt = t.Focused.TextInput.Prompt.Foreground(colorGreen)
	t.Focused.TextInput.Text = t.Focused.TextInput.Text.UnsetForeground()

	// huh derives blurred styles from the focused styles.
	t.Blurred = t.Focused
	t.Blurred.Base = t.Blurred.Base.BorderStyle(lipgloss.HiddenBorder())
	t.Blurred.Card = t.Blurred.Base
	t.Blurred.Title = s.Secondary.Bold(true)
	t.Blurred.NoteTitle = t.Focused.NoteTitle
	t.Blurred.TextInput.Prompt = t.Blurred.TextInput.Prompt.Foreground(colorMuted)
	t.Blurred.TextInput.Text = t.Blurred.TextInput.Text.UnsetForeground()
	t.Blurred.NextIndicator = lipgloss.NewStyle()
	t.Blurred.PrevIndicator = lipgloss.NewStyle()

	t.Group.Title = t.Focused.NoteTitle
	t.Group.Description = t.Focused.Description
	t.Help.Ellipsis = s.Muted
	t.Help.ShortKey = s.Secondary
	t.Help.ShortDesc = s.Muted
	t.Help.ShortSeparator = s.Muted
	t.Help.FullKey = s.Secondary
	t.Help.FullDesc = s.Muted
	t.Help.FullSeparator = s.Muted

	return t
}
