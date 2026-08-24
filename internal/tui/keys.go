package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
)

type keyMap struct {
	Up       key.Binding
	Down     key.Binding
	Left     key.Binding
	Right    key.Binding
	Open     key.Binding
	Back     key.Binding
	Refresh  key.Binding
	New      key.Binding
	Manual   key.Binding
	Search   key.Binding
	Density  key.Binding
	Help     key.Binding
	Quit     key.Binding
	PageUp   key.Binding
	PageDown key.Binding
}

var keys = keyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	),
	Left: key.NewBinding(
		key.WithKeys("left"),
		key.WithHelp("←/→", "select action"),
	),
	Right: key.NewBinding(
		key.WithKeys("right"),
		key.WithHelp("←/→", "select action"),
	),
	Open: key.NewBinding(
		key.WithKeys("enter", "l"),
		key.WithHelp("enter", "open"),
	),
	Back: key.NewBinding(
		key.WithKeys("esc", "backspace", "h"),
		key.WithHelp("esc", "back"),
	),
	Refresh: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "refresh"),
	),
	New: key.NewBinding(
		key.WithKeys("n"),
		key.WithHelp("n", "new migration"),
	),
	Manual: key.NewBinding(
		key.WithKeys("m"),
		key.WithHelp("m", "open by ID"),
	),
	Search: key.NewBinding(
		key.WithKeys("/", "f"),
		key.WithHelp("/", "search"),
	),
	Density: key.NewBinding(
		key.WithKeys("ctrl+v"),
		key.WithHelp("ctrl+v", "density"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "help"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
	PageUp: key.NewBinding(
		key.WithKeys("pgup", "["),
		key.WithHelp("[", "page up"),
	),
	PageDown: key.NewBinding(
		key.WithKeys("pgdown", "]"),
		key.WithHelp("]", "page down"),
	),
}

func helpLine(bindings ...key.Binding) string {
	parts := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		help := binding.Help()
		parts = append(parts, fmt.Sprintf("%s %s", help.Key, help.Desc))
	}
	return strings.Join(parts, " • ")
}
