package tui

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"

	"github.com/github/gh-elm/internal/elmapi"
	"github.com/github/gh-elm/internal/render"
	"github.com/github/gh-elm/internal/workflow"
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
			"Migrations",
			"Create migration",
			"Target mannequins",
			"Configuration",
			"Advanced destination operations",
			"Quit",
		})
		help = helpLine(keys.Up, keys.Down, keys.Open, keys.Help, keys.Quit)
	case screenSourceList:
		title = "Migrations"
		if m.sourceSearch {
			title += " · search: " + m.searchInput.View()
		}
		body = m.sourceListView()
		if m.sourceSearch {
			help = "type to filter • " + helpLine(keys.Up, keys.Down, keys.Open, keys.Back)
		} else {
			help = helpLine(keys.Up, keys.Down, keys.Open, keys.New, keys.Search, keys.Density, keys.Refresh, keys.Back)
		}
	case screenSourceDetail:
		title = fmt.Sprintf("Migration %s", m.sourceID)
		body = m.sourceDetailView()
		help = helpLine(keys.Up, keys.Down, keys.Open, keys.PageUp, keys.PageDown, keys.Refresh, keys.Back)
	case screenTargetList:
		title = "Advanced destination migrations"
		body = m.targetListView()
		help = helpLine(keys.Up, keys.Down, keys.Open, keys.New, keys.Manual, keys.Refresh, keys.Back)
	case screenTargetDetail:
		title = fmt.Sprintf("Destination migration %d (advanced)", m.targetID)
		body = m.targetDetailView()
		help = helpLine(keys.Up, keys.Down, keys.Open, keys.PageUp, keys.PageDown, keys.Refresh, keys.Back)
	case screenMannequins:
		title = "Target mannequins"
		body = m.menu(mannequinActions)
		help = helpLine(keys.Up, keys.Down, keys.Open, keys.Back)
	case screenConfiguration:
		title = "Configuration"
		body = m.configurationView()
		help = helpLine(keys.Up, keys.Down, keys.Open, keys.PageUp, keys.PageDown, keys.Refresh, keys.Back)
	case screenPicker:
		title = m.picker.title
		body = m.pickerView()
		help = "type to search • ↑/↓ select • enter continue • ctrl+e manual entry • esc cancel"
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
		body = m.result.body
		help = helpLine(keys.Up, keys.Down, keys.PageUp, keys.PageDown, keys.Back)
	}

	if m.showHelp {
		title = "Keyboard help"
		body = m.fullHelpView()
		help = "? close • q quit"
	}
	if m.loading {
		body = m.styles.Active.Render("Loading…")
	}
	if m.err != nil {
		body += "\n\n" + m.styles.Failure.Render("Error: "+m.err.Error())
	}
	height := displayHeight(m.height)
	bodyHeight := max(3, height-7)
	if m.scrollableScreen() || m.showHelp {
		body = m.viewportView(body, bodyHeight)
	} else {
		body = clip(body, bodyHeight)
	}
	return m.frame(title, body+"\n\n"+m.styles.Muted.Render(help))
}

func (m *Model) frame(title, body string) string {
	width := m.width
	if width <= 0 {
		width = 80
	}
	contentWidth := max(20, width-4)
	header := m.styles.Info.Bold(true).Render(title)
	if warning := m.configurationWarning(); warning != "" {
		header = m.styles.Warning.Bold(true).Render("⚠ Configuration not ready") + "\n" +
			m.styles.Warning.Render(warning) + "\n\n" + header
	}
	content := lipgloss.NewStyle().Width(contentWidth).Render(body)
	return lipgloss.NewStyle().Padding(1, 2).Width(width).Render(header + "\n\n" + content)
}

func (m *Model) configurationWarning() string {
	if m.configurationErr != nil {
		return "Unable to load configuration: " + m.configurationErr.Error()
	}
	if m.configuration == nil {
		return ""
	}

	var missing, invalid, unavailable []string
	sourceURL, sourceTokenSet := effectiveSourceConfiguration(m.configuration)
	targetURL, targetTokenSet := effectiveTargetConfiguration(m.configuration)
	if sourceURL == "" {
		missing = append(missing, "source URL")
	} else if !validHTTPURL(sourceURL) {
		invalid = append(invalid, "source URL")
	}
	if !sourceTokenSet {
		missing = append(missing, "source token")
	}
	if targetURL == "" {
		missing = append(missing, "destination URL")
	} else if !validHTTPURL(targetURL) {
		invalid = append(invalid, "destination URL")
	}
	if !targetTokenSet {
		missing = append(missing, "destination token")
	}
	if sourceURL != "" && sourceTokenSet && m.sourceAuthChecked && m.sourceAuthErr != nil {
		unavailable = append(unavailable, "source authentication")
	}
	if targetURL != "" && targetTokenSet && m.targetAuthChecked && m.targetAuthErr != nil {
		unavailable = append(unavailable, "destination authentication")
	}
	if len(missing) == 0 && len(invalid) == 0 && len(unavailable) == 0 {
		return ""
	}
	var issues []string
	if len(missing) > 0 {
		issues = append(issues, "Missing "+strings.Join(missing, ", "))
	}
	if len(invalid) > 0 {
		issues = append(issues, "Invalid "+strings.Join(invalid, ", "))
	}
	if len(unavailable) > 0 {
		issues = append(issues, "Failed "+strings.Join(unavailable, ", "))
	}
	return strings.Join(issues, ". ") + ". Open Configuration to finish setup."
}

func (m *Model) menu(items []string) string {
	var builder strings.Builder
	for index, item := range items {
		fmt.Fprintf(&builder, "%s %s\n", m.marker(index), item)
	}
	return builder.String()
}

func (m *Model) sourceListView() string {
	migrations := m.visibleSourceMigrations()
	if len(migrations) == 0 {
		if m.sourceSearch && strings.TrimSpace(m.searchInput.Value()) != "" {
			return fmt.Sprintf("No migrations match %q.", strings.TrimSpace(m.searchInput.Value()))
		}
		return "No migrations found.\n\nPress n to create one or m to open a migration by UUID."
	}
	var builder strings.Builder
	start, end := m.sourceListBounds(len(migrations))
	for index := start; index < end; index++ {
		if index > start && !m.compactSourceList() {
			builder.WriteString("\n")
		}
		builder.WriteString(m.sourceMigrationCard(migrations[index], index == m.cursor))
		builder.WriteString("\n")
	}
	if end < len(migrations) {
		builder.WriteString(m.styles.Muted.Render(fmt.Sprintf("↓ %d more", len(migrations)-end)))
	}
	if start > 0 {
		return m.styles.Muted.Render(fmt.Sprintf("↑ %d more\n", start)) + builder.String()
	}
	return builder.String()
}

func (m *Model) sourceMigrationCard(migration elmapi.MigrationSummary, selected bool) string {
	status := ""
	if migration.Status != nil {
		status = *migration.Status
	}
	glyph, statusText := m.statusDisplay(status)
	source := migration.SourceOrganizationLogin + "/" + migration.SourceRepositoryName
	target := migration.TargetOrganizationLogin + "/" + migration.TargetRepositoryName

	var card strings.Builder
	fmt.Fprintf(&card, "%s %s  %s\n", glyph, statusText, m.styles.Bold.Render(source+" → "+target))
	if m.compactSourceList() {
		fmt.Fprintf(&card, "  %s", m.styles.Muted.Render(migration.MigrationID))
	} else {
		fmt.Fprintf(&card, "  %s %s", m.styles.Muted.Render("ID"), m.styles.Muted.Render(migration.MigrationID))
		if migration.TargetMigrationID > 0 {
			fmt.Fprintf(&card, "%s%s", m.styles.Muted.Render(" · destination "), m.styles.Bold.Render(fmt.Sprintf("%d", migration.TargetMigrationID)))
		}
		if migration.CreatedAt != nil && *migration.CreatedAt != "" {
			fmt.Fprintf(&card, "%s%s", m.styles.Muted.Render(" · created "), m.styles.Muted.Render(*migration.CreatedAt))
		}
	}

	style := lipgloss.NewStyle().Border(lipgloss.HiddenBorder(), false, false, false, true).PaddingLeft(1)
	if selected {
		style = style.Border(lipgloss.ThickBorder(), false, false, false, true).BorderForeground(lipgloss.Color("4"))
	}
	return style.Render(card.String())
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
	for index, action := range m.sourceActionItems() {
		fmt.Fprintf(&actions, "%s %s\n", m.marker(index), action.label)
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
		if len(migration.RepositoryProgress) > 0 {
			detail.WriteString("\nRepository progress\n")
		}
		for index, progress := range migration.RepositoryProgress {
			fmt.Fprintf(
				&detail,
				"%s\n  Resources %s %d/%d\n  Events    %s %d/%d\n  Acknowledged: %d backfill · %d live updates\n  Sent: %s resources · %s live updates\n",
				progress.RepositoryNWO,
				render.ProgressBar(progress.ResourcesProcessed, progress.ResourcesAdded, 12),
				progress.ResourcesProcessed,
				progress.ResourcesAdded,
				render.ProgressBar(progress.EventsProcessed, progress.EventsAdded, 12),
				progress.EventsProcessed,
				progress.EventsAdded,
				progress.BackfillResourcesAcknowledged,
				progress.LiveUpdateResourcesAcknowledged,
				yesNo(progress.AllResourcesSent),
				yesNo(progress.AllLiveUpdatesSent),
			)
			if index < len(migration.RepositoryProgress)-1 {
				detail.WriteString("\n")
			}
		}
	}
	var actions strings.Builder
	actions.WriteString("Actions\n")
	for index, action := range m.targetActionItems() {
		fmt.Fprintf(&actions, "%s %s\n", m.marker(index), action.label)
	}
	return m.detailLayout(detail.String(), actions.String())
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
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
	return actions + "\n" + detail
}

func (m *Model) configurationView() string {
	var builder strings.Builder
	if configuration := m.configuration; configuration != nil {
		sourceURL, sourceTokenSet := effectiveSourceConfiguration(configuration)
		targetURL, targetTokenSet := effectiveTargetConfiguration(configuration)
		builder.WriteString(m.styles.Bold.Render("Preflight") + "\n")
		fmt.Fprintf(&builder, "  %s Source URL\n", m.checkMark(sourceURL != "" && validHTTPURL(sourceURL)))
		fmt.Fprintf(&builder, "  %s Source token\n", m.checkMark(sourceTokenSet))
		if validHTTPURL(sourceURL) && sourceTokenSet {
			fmt.Fprintf(&builder, "  %s Source authentication%s\n", m.authenticationMark(m.sourceAuthChecked, m.sourceAuthErr), m.authenticationDetail(m.sourceAuthChecked, m.sourceAuthErr))
		}
		fmt.Fprintf(&builder, "  %s Destination URL\n", m.checkMark(targetURL != "" && validHTTPURL(targetURL)))
		fmt.Fprintf(&builder, "  %s Destination token\n", m.checkMark(targetTokenSet))
		if validHTTPURL(targetURL) && targetTokenSet {
			fmt.Fprintf(&builder, "  %s Destination authentication%s\n", m.authenticationMark(m.targetAuthChecked, m.targetAuthErr), m.authenticationDetail(m.targetAuthChecked, m.targetAuthErr))
		}
		builder.WriteString("\n")

		builder.WriteString("Stored configuration\n")
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

func effectiveSourceConfiguration(configuration *workflow.Configuration) (string, bool) {
	if configuration.ResolvedSourceURL != "" || configuration.ResolvedSourceTokenSet {
		return configuration.ResolvedSourceURL, configuration.ResolvedSourceTokenSet
	}
	return configuration.SourceURL, configuration.SourceTokenSet
}

func effectiveTargetConfiguration(configuration *workflow.Configuration) (string, bool) {
	if configuration.ResolvedTargetURL != "" || configuration.ResolvedTargetTokenSet {
		return configuration.ResolvedTargetURL, configuration.ResolvedTargetTokenSet
	}
	return configuration.TargetURL, configuration.TargetTokenSet
}

func validHTTPURL(value string) bool {
	parsed, err := url.ParseRequestURI(value)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func (m *Model) checkMark(ok bool) string {
	if ok {
		return m.styles.Success.Render("✓")
	}
	return m.styles.Failure.Render("✗")
}

func (m *Model) authenticationMark(checked bool, err error) string {
	if !checked {
		return m.styles.Muted.Render("…")
	}
	return m.checkMark(err == nil)
}

func (m *Model) authenticationDetail(checked bool, err error) string {
	if !checked {
		return m.styles.Muted.Render(" (checking)")
	}
	if err != nil {
		return m.styles.Failure.Render(" (" + err.Error() + ")")
	}
	return ""
}

func (m *Model) formView() string {
	var builder strings.Builder
	if m.form.description != "" {
		builder.WriteString(m.styles.Muted.Render(m.form.description))
		builder.WriteString("\n\n")
	}
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

func (m *Model) pickerView() string {
	if m.picker.loading {
		return m.styles.Active.Render("Loading options…") + "\n\n" +
			m.styles.Muted.Render("Press Ctrl+E to enter repositories manually.")
	}
	if m.picker.err != nil {
		return m.styles.Failure.Render("Unable to load options: "+m.picker.err.Error()) + "\n\n" +
			m.styles.Muted.Render("Press Ctrl+E to enter repositories manually.")
	}

	items := m.visiblePickerItems()
	var builder strings.Builder
	fmt.Fprintf(&builder, "Search: %s\n\n", m.picker.input.View())
	if len(items) == 0 {
		fmt.Fprintf(&builder, "%s\n", m.styles.Muted.Render("No matching options."))
		return builder.String()
	}
	start, end := pickerBounds(m.picker.cursor, len(items), max(3, displayHeight(m.height)-11))
	for index := start; index < end; index++ {
		fmt.Fprintf(&builder, "%s %s\n", m.markerFor(index, m.picker.cursor), items[index])
	}
	if end < len(items) {
		fmt.Fprintf(&builder, "%s", m.styles.Muted.Render(fmt.Sprintf("↓ %d more", len(items)-end)))
	}
	if start > 0 {
		return m.styles.Muted.Render(fmt.Sprintf("↑ %d more\n", start)) + builder.String()
	}
	return builder.String()
}

func (m *Model) markerFor(index, cursor int) string {
	if index == cursor {
		return m.styles.Info.Render("›")
	}
	return " "
}

func pickerBounds(cursor, total, capacity int) (start, end int) {
	if total == 0 {
		return 0, 0
	}
	capacity = max(1, capacity)
	start = max(0, cursor-capacity+1)
	end = min(total, start+capacity)
	if end-start < capacity {
		start = max(0, end-capacity)
	}
	return start, end
}

func (m *Model) marker(index int) string {
	if index == m.cursor {
		return m.styles.Info.Render("›")
	}
	return " "
}

func (m *Model) statusDisplay(status string) (glyph, label string) {
	normalized := normalizedStatus(status)
	switch normalized {
	case "completed", "complete":
		return m.styles.Success.Render("✓"), m.styles.Success.Bold(true).Render("Completed")
	case "in progress", "processing":
		return m.styles.Active.Render("●"), m.styles.Active.Bold(true).Render("In progress")
	case "created":
		return m.styles.Info.Render("●"), m.styles.Bold.Render("Created")
	case "queued":
		return m.styles.Muted.Render("○"), m.styles.Muted.Render("Queued")
	case "paused":
		return m.styles.Warning.Render("●"), m.styles.Warning.Render("Paused")
	case "failed":
		return m.styles.Failure.Render("✗"), m.styles.Failure.Render("Failed")
	case "terminated":
		return m.styles.Failure.Render("⊘"), m.styles.Failure.Render("Terminated")
	case "cancelled":
		return m.styles.Failure.Render("⊘"), m.styles.Failure.Render("Cancelled")
	default:
		return m.styles.Muted.Render("●"), m.styles.Muted.Render(friendly(status))
	}
}

func (m *Model) compactSourceList() bool {
	if m.densityUserSet {
		return m.compact
	}
	return len(m.visibleSourceMigrations()) > 10
}

func (m *Model) sourceListBounds(total int) (start, end int) {
	if total == 0 {
		return 0, 0
	}
	linesPerCard := 4
	if m.compactSourceList() {
		linesPerCard = 2
	}
	capacity := max(1, (displayHeight(m.height)-8)/linesPerCard)
	start = max(0, m.cursor-capacity+1)
	end = min(total, start+capacity)
	if end-start < capacity {
		start = max(0, end-capacity)
	}
	return start, end
}

func (m *Model) viewportView(body string, height int) string {
	width := max(20, m.width-4)
	view := m.viewport
	if !m.viewportReady {
		view = viewport.New(width, height)
	}
	view.Width = width
	view.Height = height
	view.SetContent(body)
	return view.View()
}

func (m *Model) fullHelpView() string {
	return strings.Join([]string{
		"Global",
		"  " + helpLine(keys.Up, keys.Down, keys.Open, keys.Back, keys.Help, keys.Quit),
		"",
		"Migrations",
		"  " + helpLine(keys.New, keys.Manual, keys.Search, keys.Density, keys.Refresh),
		"",
		"Scrollable views",
		"  " + helpLine(keys.PageUp, keys.PageDown),
	}, "\n")
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

func displayHeight(height int) int {
	if height <= 0 {
		return 24
	}
	return height
}
