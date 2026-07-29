// Package theme defines the single palette and style set `gh elm` renders with.
//
// The palette is the gh CLI's own. gh builds its ColorScheme almost entirely
// from the basic ANSI colours — red, green, yellow, blue, magenta, cyan and
// bright black — with one exception: de-emphasised text uses 256-colour index
// 242. Naming palette indices rather than hex leaves the exact shades to
// whatever theme the user has already configured in their terminal, which is
// the reason no light/dark adaptation appears here. Index 242 is a mid grey
// that stays legible on either background, and lipgloss downsamples it to
// bright black on 4-bit terminals — the same fallback gh itself uses.
//
// Styles are expressed as roles rather than colours so call sites say what a
// thing means and this package decides how it looks.
package theme

import (
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// The gh palette.
const (
	ColorRed    = lipgloss.Color("1")
	ColorGreen  = lipgloss.Color("2")
	ColorYellow = lipgloss.Color("3")
	ColorCyan   = lipgloss.Color("6")
	ColorBlack  = lipgloss.Color("0")
	ColorMuted  = lipgloss.Color("242")
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
		Info:    lipgloss.NewStyle().Foreground(ColorCyan),
		Muted:   lipgloss.NewStyle().Foreground(ColorMuted),
		Success: lipgloss.NewStyle().Foreground(ColorGreen),
		Active:  lipgloss.NewStyle().Foreground(ColorYellow),
		Warning: lipgloss.NewStyle().Foreground(ColorYellow),
		Paused:  lipgloss.NewStyle().Foreground(ColorYellow),
		Failure: lipgloss.NewStyle().Foreground(ColorRed),
	}
}

// Form returns the huh theme for interactive prompts.
//
// gh has no huh forms of its own, so there is nothing upstream to copy, but its
// palette is plain ANSI and a form can be made to sit alongside gh output
// rather than look borrowed. huh.ThemeBase16 is already close — cyan titles,
// red errors, green selections — so this only replaces the accents it picks
// that carry no meaning here, and points its greys at the palette above.
func Form() *huh.Theme {
	s := New()
	t := huh.ThemeBase16()

	t.Focused.Base = t.Focused.Base.BorderForeground(ColorMuted)
	t.Focused.Card = t.Focused.Base
	t.Focused.Title = t.Focused.Title.Foreground(ColorCyan)
	t.Focused.NoteTitle = t.Focused.NoteTitle.Foreground(ColorCyan)
	t.Focused.Directory = t.Focused.Directory.Foreground(ColorCyan)
	t.Focused.Description = s.Muted
	t.Focused.ErrorIndicator = t.Focused.ErrorIndicator.Foreground(ColorRed)
	t.Focused.ErrorMessage = t.Focused.ErrorMessage.Foreground(ColorRed)
	t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(ColorCyan)
	t.Focused.NextIndicator = t.Focused.NextIndicator.Foreground(ColorCyan)
	t.Focused.PrevIndicator = t.Focused.PrevIndicator.Foreground(ColorCyan)
	t.Focused.Option = t.Focused.Option.UnsetForeground()
	t.Focused.MultiSelectSelector = t.Focused.MultiSelectSelector.Foreground(ColorCyan)
	t.Focused.SelectedOption = t.Focused.SelectedOption.Foreground(ColorGreen)
	t.Focused.SelectedPrefix = t.Focused.SelectedPrefix.Foreground(ColorGreen)
	t.Focused.UnselectedOption = t.Focused.UnselectedOption.UnsetForeground()
	t.Focused.FocusedButton = t.Focused.FocusedButton.Foreground(ColorBlack).Background(ColorGreen)
	t.Focused.BlurredButton = t.Focused.BlurredButton.Foreground(ColorMuted).Background(ColorBlack)
	t.Focused.TextInput.Cursor = t.Focused.TextInput.Cursor.Foreground(ColorCyan)
	t.Focused.TextInput.Placeholder = t.Focused.TextInput.Placeholder.Foreground(ColorMuted)
	t.Focused.TextInput.Prompt = t.Focused.TextInput.Prompt.Foreground(ColorCyan)

	// huh derives the blurred styles by copying the focused ones, so this has
	// to come after every Focused assignment above.
	t.Blurred = t.Focused
	t.Blurred.Base = t.Blurred.Base.BorderStyle(lipgloss.HiddenBorder())
	t.Blurred.Card = t.Blurred.Base
	t.Blurred.Title = t.Blurred.Title.Foreground(ColorMuted)
	t.Blurred.NoteTitle = t.Blurred.NoteTitle.Foreground(ColorMuted)
	t.Blurred.TextInput.Prompt = t.Blurred.TextInput.Prompt.Foreground(ColorMuted)
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
