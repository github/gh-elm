package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/github/gh-elm/internal/render"
)

// View implements tea.Model.
func (m *Model) View() string {
	if m.width > 0 && (m.width < 48 || m.height < 12) {
		return m.frame("Terminal too small", "Resize to at least 48 columns by 12 rows.\n\nctrl+c quit")
	}

	var title, body, help string
	switch m.screen {
	case screenHome:
		title = "Enterprise Live Migrations"
		body = m.menu([]string{
			"Source migrations",
			"Target migrations",
			"Target mannequins",
			"Configuration",
			"Quit",
		})
		help = "↑/↓ move • enter select • q quit"
	case screenSourceList:
		title = "Source migrations"
		body = m.sourceListView()
		help = "↑/↓ move • enter open • n new • m manual ID • r refresh • esc back"
	case screenSourceDetail:
		title = fmt.Sprintf("Source migration %s", m.sourceID)
		body = m.sourceDetailView()
		help = "↑/↓ move • enter action • r refresh • esc back"
	case screenTargetList:
		title = "Target migrations"
		body = m.targetListView()
		help = "↑/↓ move • enter open • n advanced create • m manual ID • r refresh • esc back"
	case screenTargetDetail:
		title = fmt.Sprintf("Target migration %d", m.targetID)
		body = m.targetDetailView()
		help = "↑/↓ move • enter action • r refresh • esc back"
	case screenMannequins:
		title = "Target mannequins"
		body = m.menu(mannequinActions)
		help = "↑/↓ move • enter select • esc back"
	case screenConfiguration:
		title = "Configuration"
		body = m.configurationView()
		help = "↑/↓ move • enter select • r refresh • esc back"
	case screenForm:
		title = m.form.title
		body = m.formView()
		help = "tab/↑/↓ fields • type edit • ←/→ choose • space toggle • enter continue • esc cancel"
	case screenConfirm:
		title = m.confirm.title
		body = m.styles.Warning.Render(m.confirm.body) + "\n\nContinue? [y/N]"
		help = "y/enter confirm • n/esc cancel"
	case screenResult:
		title = m.result.title
		body = clipWindow(m.result.body, m.result.offset, max(3, displayHeight(m.height)-7))
		help = "↑/↓ scroll • enter/esc return"
	}

	if m.loading {
		body = m.styles.Active.Render("Loading…")
	}
	if m.err != nil {
		body += "\n\n" + m.styles.Failure.Render("Error: "+m.err.Error())
	}
	height := displayHeight(m.height)
	return m.frame(title, clip(body, max(3, height-7))+"\n\n"+m.styles.Muted.Render(help))
}

func (m *Model) frame(title, body string) string {
	width := m.width
	if width <= 0 {
		width = 80
	}
	contentWidth := max(20, width-4)
	header := m.styles.Info.Bold(true).Render(title)
	content := lipgloss.NewStyle().Width(contentWidth).Render(body)
	return lipgloss.NewStyle().Padding(1, 2).Width(width).Render(header + "\n\n" + content)
}

func (m *Model) menu(items []string) string {
	var builder strings.Builder
	for index, item := range items {
		fmt.Fprintf(&builder, "%s %s\n", m.marker(index), item)
	}
	return builder.String()
}

func (m *Model) sourceListView() string {
	if len(m.sourceMigrations) == 0 {
		return "No source migrations found.\n\nPress n to create one or m to open a migration by UUID."
	}
	var builder strings.Builder
	for index, migration := range m.sourceMigrations {
		status := ""
		if migration.Status != nil {
			status = *migration.Status
		}
		fmt.Fprintf(
			&builder,
			"%s %-12s  %s/%s → %s/%s\n  %s\n",
			m.marker(index),
			status,
			migration.SourceOrganizationLogin,
			migration.SourceRepositoryName,
			migration.TargetOrganizationLogin,
			migration.TargetRepositoryName,
			m.styles.Muted.Render(migration.MigrationID),
		)
	}
	return builder.String()
}

func (m *Model) sourceDetailView() string {
	var status strings.Builder
	if m.sourceDetail != nil {
		status.WriteString(render.MigrationStatus(*m.sourceDetail))
	}
	if m.sourceWatching {
		status.WriteString("\n")
		status.WriteString(m.styles.Active.Render("● Live watch enabled (2s refresh)"))
		status.WriteString("\n")
	}
	var actions strings.Builder
	actions.WriteString("Actions\n")
	for index, action := range sourceActions {
		fmt.Fprintf(&actions, "%s %s\n", m.marker(index), action)
	}
	return m.detailLayout(status.String(), actions.String())
}

func (m *Model) targetListView() string {
	if len(m.targetMigrations) == 0 {
		return "No target migrations found.\n\nPress n for advanced direct creation or m to open a numeric target ID."
	}
	var builder strings.Builder
	for index, migration := range m.targetMigrations {
		repositories := strings.Join(migration.Repositories, ", ")
		fmt.Fprintf(
			&builder,
			"%s %-12s  %s\n  ID %s\n",
			m.marker(index),
			friendly(migration.Status),
			repositories,
			m.styles.Muted.Render(migration.MigrationID),
		)
	}
	return builder.String()
}

func (m *Model) targetDetailView() string {
	var detail strings.Builder
	if migration := m.targetDetail; migration != nil {
		fmt.Fprintf(&detail, "Status:       %s\n", friendly(migration.Status))
		fmt.Fprintf(&detail, "Repositories: %s\n", strings.Join(migration.Repositories, ", "))
		if migration.Description != "" {
			fmt.Fprintf(&detail, "Description:  %s\n", migration.Description)
		}
		if !migration.ExpiresAt.IsZero() {
			fmt.Fprintf(&detail, "Expires:      %s\n", migration.ExpiresAt.Format("2006-01-02 15:04:05Z07:00"))
		}
		for _, progress := range migration.RepositoryProgress {
			fmt.Fprintf(
				&detail,
				"\n%s\n  resources %d/%d • events %d/%d\n",
				progress.RepositoryNWO,
				progress.ResourcesProcessed,
				progress.ResourcesAdded,
				progress.EventsProcessed,
				progress.EventsAdded,
			)
		}
	}
	var actions strings.Builder
	actions.WriteString("Actions\n")
	for index, action := range targetActions {
		fmt.Fprintf(&actions, "%s %s\n", m.marker(index), action)
	}
	return m.detailLayout(detail.String(), actions.String())
}

func (m *Model) detailLayout(detail, actions string) string {
	if m.width >= 100 {
		columnWidth := max(30, (m.width-8)/2)
		return lipgloss.JoinHorizontal(
			lipgloss.Top,
			lipgloss.NewStyle().Width(columnWidth).Render(detail),
			lipgloss.NewStyle().Width(columnWidth).Render(actions),
		)
	}
	available := max(3, displayHeight(m.height)-7)
	actionLines := len(strings.Split(strings.TrimSuffix(actions, "\n"), "\n"))
	detailLines := max(2, available-actionLines-2)
	return clip(detail, detailLines) + "\n\n" + actions
}

func (m *Model) configurationView() string {
	var builder strings.Builder
	if configuration := m.configuration; configuration != nil {
		fmt.Fprintf(&builder, "Source URL:   %s\n", orUnset(configuration.SourceURL))
		fmt.Fprintf(&builder, "Source token: %s\n", setStatus(configuration.SourceTokenSet))
		fmt.Fprintf(&builder, "Target URL:   %s\n", orUnset(configuration.TargetURL))
		fmt.Fprintf(&builder, "Target token: %s\n", setStatus(configuration.TargetTokenSet))
		fmt.Fprintf(&builder, "Config:       %s\n", configuration.ConfigPath)
		fmt.Fprintf(&builder, "Credentials:  %s\n", configuration.CredentialStore)
	}
	builder.WriteString("\nActions\n")
	for index, action := range configurationActions {
		fmt.Fprintf(&builder, "%s %s\n", m.marker(index), action)
	}
	return builder.String()
}

func (m *Model) formView() string {
	var builder strings.Builder
	for index, field := range m.form.fields {
		value := field.value
		switch field.kind {
		case fieldSecret:
			value = strings.Repeat("•", len([]rune(value)))
		case fieldBool:
			if value == "true" {
				value = "[x]"
			} else {
				value = "[ ]"
			}
		case fieldSelect:
			value = "‹ " + value + " ›"
		}
		if value == "" {
			value = m.styles.Placeholder.Render("(empty)")
		}
		label := field.label
		if index == m.form.cursor {
			label = m.styles.Info.Bold(true).Render(label)
		}
		marker := " "
		if index == m.form.cursor {
			marker = m.styles.Info.Render("›")
		}
		fmt.Fprintf(&builder, "%s %s\n    %s\n", marker, label, value)
		if field.description != "" {
			fmt.Fprintf(&builder, "    %s\n", m.styles.Muted.Render(field.description))
		}
	}
	if m.form.err != nil {
		fmt.Fprintf(&builder, "\n%s\n", m.styles.Failure.Render("Error: "+m.form.err.Error()))
	}
	return builder.String()
}

func (m *Model) marker(index int) string {
	if index == m.cursor {
		return m.styles.Info.Render("›")
	}
	return " "
}

func orUnset(value string) string {
	if value == "" {
		return "(not set)"
	}
	return value
}

func setStatus(set bool) string {
	if set {
		return "set (hidden)"
	}
	return "not set"
}

func clip(value string, lines int) string {
	if lines <= 0 {
		return ""
	}
	parts := strings.Split(value, "\n")
	if len(parts) <= lines {
		return value
	}
	return strings.Join(parts[:lines-1], "\n") + "\n…"
}

func clipWindow(value string, offset, lines int) string {
	parts := strings.Split(value, "\n")
	if offset > max(0, len(parts)-1) {
		offset = max(0, len(parts)-1)
	}
	end := min(len(parts), offset+lines)
	window := strings.Join(parts[offset:end], "\n")
	if end < len(parts) {
		window += "\n…"
	}
	return window
}

func displayHeight(height int) int {
	if height <= 0 {
		return 24
	}
	return height
}
