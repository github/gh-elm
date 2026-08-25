package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const buttonGap = "  "

func (m *Model) selectorCard(content string, selected, compact bool) string {
	style := lipgloss.NewStyle().
		Border(lipgloss.HiddenBorder(), false, false, false, true).
		PaddingLeft(1)
	if !compact {
		style = style.MarginBottom(1)
	}
	if selected {
		style = style.
			Border(lipgloss.ThickBorder(), false, false, false, true).
			BorderForeground(m.styles.Info.GetForeground())
	}
	return style.Render(content)
}

func (m *Model) actionButtons(items []actionItem, focus, width int) string {
	if len(items) == 0 {
		return ""
	}

	width = max(1, width)
	rows := make([]string, 0, len(items))
	row := ""
	for index, item := range items {
		style := m.styles.BlurredButton
		if index == focus {
			style = m.styles.FocusedButton
		}
		button := style.Padding(0, 2).Render(item.label)
		candidate := button
		if row != "" {
			candidate = row + buttonGap + button
		}
		if row != "" && lipgloss.Width(candidate) > width {
			rows = append(rows, row)
			row = button
			continue
		}
		row = candidate
	}
	if row != "" {
		rows = append(rows, row)
	}
	return strings.Join(rows, "\n")
}

func (m *Model) panel(content string) string {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.styles.Muted.GetForeground()).
		Padding(1, 3).
		Render(content)
}

func overlayCenter(background, foreground string, width, height int) string {
	backgroundLines := strings.Split(background, "\n")
	for len(backgroundLines) < height {
		backgroundLines = append(backgroundLines, "")
	}
	foregroundLines := strings.Split(foreground, "\n")
	foregroundWidth := 0
	for _, line := range foregroundLines {
		foregroundWidth = max(foregroundWidth, ansi.StringWidth(line))
	}

	startRow := max(0, (height-len(foregroundLines))*2/5)
	startColumn := max(0, (width-foregroundWidth)/2)
	for index, foregroundLine := range foregroundLines {
		row := startRow + index
		if row >= len(backgroundLines) {
			break
		}
		backgroundLine := backgroundLines[row]
		left := ansi.Truncate(backgroundLine, startColumn, "")
		if leftWidth := ansi.StringWidth(left); leftWidth < startColumn {
			left += strings.Repeat(" ", startColumn-leftWidth)
		}
		right := ansi.TruncateLeft(backgroundLine, startColumn+ansi.StringWidth(foregroundLine), "")
		backgroundLines[row] = left + "\x1b[0m" + foregroundLine + "\x1b[0m" + right
	}
	return strings.Join(backgroundLines, "\n")
}
