package watch

import (
	"github.com/charmbracelet/lipgloss"
)

// Styles holds all lipgloss styles used by the watch display.
type Styles struct {
	// Header
	MigrationID lipgloss.Style
	Label       lipgloss.Style
	Value       lipgloss.Style

	// Phase indicators
	PhaseComplete lipgloss.Style
	PhaseActive   lipgloss.Style
	PhasePending  lipgloss.Style
	PhaseFailed   lipgloss.Style
	PhasePaused   lipgloss.Style

	// Phase names
	PhaseName       lipgloss.Style
	PhaseNameActive lipgloss.Style

	// Content
	Timestamp     lipgloss.Style
	Detail        lipgloss.Style
	ProgressFull  lipgloss.Style
	ProgressEmpty lipgloss.Style
	CheckOK       lipgloss.Style
	CheckFail     lipgloss.Style
	Warning       lipgloss.Style

	// Messages
	MessageInfo  lipgloss.Style
	MessageError lipgloss.Style

	// Footer
	Footer lipgloss.Style
}

// DefaultStyles returns the default adaptive styles for the watch display.
func DefaultStyles() Styles {
	green := lipgloss.AdaptiveColor{Light: "002", Dark: "002"}
	yellow := lipgloss.AdaptiveColor{Light: "003", Dark: "003"}
	red := lipgloss.AdaptiveColor{Light: "001", Dark: "001"}
	cyan := lipgloss.AdaptiveColor{Light: "006", Dark: "006"}
	dim := lipgloss.AdaptiveColor{Light: "007", Dark: "245"}

	return Styles{
		MigrationID: lipgloss.NewStyle().Bold(true),
		Label:       lipgloss.NewStyle().Foreground(cyan),
		Value:       lipgloss.NewStyle(),

		PhaseComplete: lipgloss.NewStyle().Foreground(green),
		PhaseActive:   lipgloss.NewStyle().Foreground(yellow),
		PhasePending:  lipgloss.NewStyle().Foreground(dim),
		PhaseFailed:   lipgloss.NewStyle().Foreground(red),
		PhasePaused:   lipgloss.NewStyle().Foreground(cyan),

		PhaseName:       lipgloss.NewStyle().Bold(true),
		PhaseNameActive: lipgloss.NewStyle().Bold(true).Foreground(yellow),

		Timestamp:     lipgloss.NewStyle().Foreground(dim),
		Detail:        lipgloss.NewStyle().Foreground(dim),
		ProgressFull:  lipgloss.NewStyle().Foreground(green),
		ProgressEmpty: lipgloss.NewStyle().Foreground(dim),
		CheckOK:       lipgloss.NewStyle().Foreground(green),
		CheckFail:     lipgloss.NewStyle().Foreground(red),
		Warning:       lipgloss.NewStyle().Foreground(yellow),

		MessageInfo:  lipgloss.NewStyle().Foreground(dim),
		MessageError: lipgloss.NewStyle().Foreground(red),

		Footer: lipgloss.NewStyle().Foreground(dim),
	}
}
