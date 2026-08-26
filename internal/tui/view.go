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

const homeTitle = "Live migrations"

// View implements tea.Model.
func (m *Model) View() string {
	if m.width > 0 && (m.width < 48 || m.height < 12) {
		return m.frame("Terminal too small", "Resize to at least 48 columns by 12 rows.", "ctrl+c quit")
	}

	current := m.screen
	confirming := current == screenConfirm
	if confirming {
		current = m.confirm.parent
	}

	var title, body, help string
	switch current {
	case screenHome:
		title = homeTitle
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
		help = helpLine(keys.Left, keys.Open, keys.PageUp, keys.PageDown, keys.Refresh, keys.Back)
	case screenTargetList:
		title = "Advanced destination migrations"
		body = m.targetListView()
		help = helpLine(keys.Up, keys.Down, keys.Open, keys.New, keys.Manual, keys.Refresh, keys.Back)
	case screenTargetDetail:
		title = fmt.Sprintf("Destination migration %d (advanced)", m.targetID)
		body = m.targetDetailView()
		help = helpLine(keys.Left, keys.Open, keys.PageUp, keys.PageDown, keys.Refresh, keys.Back)
	case screenMannequins:
		title = "Target mannequins"
		body = m.actionButtons(mannequinActions, m.actionFocus, m.contentWidth())
		help = helpLine(keys.Left, keys.Open, keys.Back)
	case screenConfiguration:
		title = "Configuration"
		body = m.configurationView()
		help = helpLine(keys.Left, keys.Open, keys.PageUp, keys.PageDown, keys.Refresh, keys.Back)
	case screenPicker:
		title = m.picker.title
		body = m.pickerView()
		help = "type to search • ↑/↓ select • enter continue • ctrl+e manual entry • esc cancel"
		if m.picker.kind == pickerSourceRepository {
			help = "type to search • ↑/↓ select • enter continue • ? repository details • ctrl+e manual entry • esc cancel"
		}
	case screenForm:
		title = m.form.title
		body = m.formView()
		help = "tab/↑/↓ fields • type edit • ←/→ choose • space toggle • enter continue • esc cancel"
	case screenResult:
		title = m.result.title
		body = m.result.body
		help = helpLine(keys.Up, keys.Down, keys.PageUp, keys.PageDown, keys.Back)
	}

	if m.showHelp && !confirming {
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
	bodyHeight := m.bodyHeight()
	if isScrollableScreen(current) || m.showHelp {
		body = m.viewportView(body, bodyHeight)
	} else {
		body = clip(body, bodyHeight)
	}
	if confirming {
		help = "←/→ select action • enter activate • y confirm • n/esc cancel"
	}
	rendered := m.frame(title, body, help)
	if confirming {
		rendered = overlayCenter(
			rendered,
			m.confirmationOverlay(),
			m.displayWidth(),
			displayHeight(m.height),
		)
	} else if m.pickerInfoOpen {
		if overlay := m.pickerInfoOverlay(); overlay != "" {
			rendered = overlayCenter(
				rendered,
				overlay,
				m.displayWidth(),
				displayHeight(m.height),
			)
		}
	}
	return rendered
}

func (m *Model) frame(title, body, help string) string {
	contentWidth := m.contentWidth()
	brand := m.styles.Primary.Bold(true).Render("GitHub Enterprise")
	subtitle := m.styles.Info.Bold(true).Render(title)
	if title == homeTitle {
		subtitle = m.styles.Success.Render(title)
	}
	header := brand + "\n" + subtitle + "\n" +
		m.styles.Muted.Render(strings.Repeat("─", contentWidth))

	var warningBlock string
	if warning := m.configurationWarning(); warning != "" {
		warning = m.styles.Warning.Width(contentWidth).Render(warning)
		warningBlock = "\n" + m.styles.Warning.Bold(true).Render("⚠ Configuration not ready") + "\n" +
			warning
	}
	content := lipgloss.NewStyle().Width(contentWidth).Render(body)
	footer := m.styles.Muted.Render(help)
	return lipgloss.NewStyle().Padding(0, 1).Render(
		header + warningBlock + "\n\n" + content + "\n\n" + footer,
	)
}

func (m *Model) displayWidth() int {
	if m.width > 0 {
		return m.width
	}
	return 80
}

func (m *Model) contentWidth() int {
	return max(20, m.displayWidth()-2)
}

func (m *Model) bodyHeight() int {
	height := displayHeight(m.height) - 6
	if warning := m.configurationWarning(); warning != "" {
		height -= lipgloss.Height(lipgloss.NewStyle().Width(m.contentWidth()).Render(warning)) + 1
	}
	return max(3, height)
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
		if index > 0 {
			builder.WriteString("\n")
		}
		builder.WriteString(m.selectorCard(item, index == m.cursor, true))
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
		if index > start {
			builder.WriteString("\n")
		}
		builder.WriteString(m.sourceMigrationCard(migrations[index], index == m.cursor))
	}
	if end < len(migrations) {
		builder.WriteString("\n")
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
	fmt.Fprintf(&card, "%s %s  %s\n", glyph, statusText, m.repositoryChip(source+" → "+target))
	if m.compactSourceList() {
		fmt.Fprintf(&card, "  %s %s", m.styles.Muted.Render("id:"), m.styles.Muted.Render(migration.MigrationID))
	} else {
		fmt.Fprintf(&card, "  %s %s", m.styles.Muted.Render("id:"), m.styles.Muted.Render(migration.MigrationID))
		if migration.TargetMigrationID > 0 {
			fmt.Fprintf(&card, "%s%s", m.styles.Muted.Render(" · destination "), m.styles.Bold.Render(fmt.Sprintf("%d", migration.TargetMigrationID)))
		}
		if migration.CreatedAt != nil && *migration.CreatedAt != "" {
			fmt.Fprintf(&card, "%s%s", m.styles.Muted.Render(" · created "), m.styles.Muted.Render(*migration.CreatedAt))
		}
		if migration.TargetVisibility != nil && *migration.TargetVisibility != "" {
			fmt.Fprintf(&card, "  %s", m.metadataBadge(*migration.TargetVisibility))
		}
	}

	return m.selectorCard(card.String(), selected, m.compactSourceList())
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
	actions.WriteString(m.styles.Bold.Render("Actions") + "\n\n")
	actions.WriteString(m.actionButtons(m.sourceActionItems(), m.actionFocus, m.contentWidth()))
	return m.detailLayout(status.String(), actions.String())
}

func (m *Model) targetListView() string {
	if len(m.targetMigrations) == 0 {
		return "No target migrations found.\n\nPress n for advanced direct creation or m to open a numeric target ID."
	}
	var builder strings.Builder
	capacity := max(1, (m.bodyHeight()-2)/4)
	start, end := pickerBounds(m.cursor, len(m.targetMigrations), capacity)
	if start > 0 {
		builder.WriteString(m.styles.Muted.Render(fmt.Sprintf("↑ %d more\n", start)))
	}
	for index := start; index < end; index++ {
		migration := m.targetMigrations[index]
		repositories := strings.Join(migration.Repositories, ", ")
		var card strings.Builder
		glyph, status := m.statusDisplay(migration.Status)
		fmt.Fprintf(&card, "%s %s  %s\n", glyph, status, m.repositoryChip(repositories))
		fmt.Fprintf(&card, "%s %s", m.styles.Muted.Render("id:"), m.styles.Muted.Render(migration.MigrationID))
		if index > start {
			builder.WriteString("\n")
		}
		builder.WriteString(m.selectorCard(card.String(), index == m.cursor, false))
	}
	if end < len(m.targetMigrations) {
		fmt.Fprintf(&builder, "\n%s", m.styles.Muted.Render(fmt.Sprintf("↓ %d more", len(m.targetMigrations)-end)))
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
	actions.WriteString(m.styles.Bold.Render("Actions") + "\n\n")
	actions.WriteString(m.actionButtons(m.targetActionItems(), m.actionFocus, m.contentWidth()))
	return m.detailLayout(detail.String(), actions.String())
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func (m *Model) detailLayout(detail, actions string) string {
	if detail == "" {
		return actions
	}
	if actions == "" {
		return detail
	}
	return detail + "\n\n" + actions
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
	builder.WriteString("\n" + m.styles.Bold.Render("Actions") + "\n\n")
	builder.WriteString(m.actionButtons(configurationActions, m.actionFocus, m.contentWidth()))
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
	var prefix strings.Builder
	if m.form.description != "" {
		prefix.WriteString(m.styles.Muted.Render(m.form.description))
		prefix.WriteString("\n\n")
	}
	blocks := make([]string, 0, len(m.form.fields))
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
		var content strings.Builder
		fmt.Fprintf(&content, "%s\n  %s", label, value)
		if field.description != "" {
			fmt.Fprintf(&content, "\n  %s", m.styles.Muted.Render(field.description))
		}
		blocks = append(blocks, m.selectorCard(content.String(), index == m.form.cursor, true))
	}

	var suffix string
	if m.form.err != nil {
		suffix = "\n\n" + m.styles.Failure.Render("Error: "+m.form.err.Error())
	}

	reserved := 0
	if prefix.Len() > 0 {
		reserved += lipgloss.Height(prefix.String())
	}
	if suffix != "" {
		reserved += lipgloss.Height(suffix)
	}
	available := m.bodyHeight() - reserved
	start, end := focusedBlockRange(blocks, m.form.cursor, max(1, available-2))
	var builder strings.Builder
	builder.WriteString(prefix.String())
	if start > 0 {
		builder.WriteString(m.styles.Muted.Render(fmt.Sprintf("↑ %d more field(s)", start)) + "\n")
	}
	builder.WriteString(strings.Join(blocks[start:end], "\n"))
	if end < len(blocks) {
		builder.WriteString("\n" + m.styles.Muted.Render(fmt.Sprintf("↓ %d more field(s)", len(blocks)-end)))
	}
	builder.WriteString(suffix)
	return builder.String()
}

func focusedBlockRange(blocks []string, focus, height int) (start, end int) {
	if len(blocks) == 0 {
		return 0, 0
	}
	focus = min(max(0, focus), len(blocks)-1)
	start, end = focus, focus+1
	used := lipgloss.Height(blocks[focus])
	for end < len(blocks) {
		next := lipgloss.Height(blocks[end]) + 1
		if used+next > height {
			break
		}
		used += next
		end++
	}
	for start > 0 {
		next := lipgloss.Height(blocks[start-1]) + 1
		if used+next > height {
			break
		}
		used += next
		start--
	}
	return start, end
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
	start, end := pickerBounds(m.picker.cursor, len(items), max(3, m.bodyHeight()-3))
	for index := start; index < end; index++ {
		if index > start {
			builder.WriteString("\n")
		}
		builder.WriteString(m.pickerItemView(items[index], index == m.picker.cursor))
	}
	if end < len(items) {
		fmt.Fprintf(&builder, "\n%s", m.styles.Muted.Render(fmt.Sprintf("↓ %d more", len(items)-end)))
	}
	list := builder.String()
	if start > 0 {
		list = m.styles.Muted.Render(fmt.Sprintf("↑ %d more\n", start)) + list
	}
	if m.picker.kind != pickerSourceRepository || m.contentWidth() < 92 {
		return list
	}
	selected := items[m.picker.cursor]
	if selected.repository == nil {
		return list
	}
	info := m.repositoryInfoPanel(*selected.repository, false)
	listWidth := max(30, m.contentWidth()-lipgloss.Width(info)-4)
	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		lipgloss.NewStyle().Width(listWidth).Render(list),
		"    ",
		info,
	)
}

func (m *Model) pickerItemView(item pickerItem, selected bool) string {
	if item.repository == nil {
		return m.selectorCard(item.value, selected, true)
	}
	repository := item.repository
	var metadata []string
	if repository.Stargazers > 0 {
		metadata = append(metadata, fmt.Sprintf("★ %d", repository.Stargazers))
	}
	if repository.OpenIssueCount > 0 {
		metadata = append(metadata, fmt.Sprintf("≡ %d", repository.OpenIssueCount))
	}
	if repository.Language != "" {
		metadata = append(metadata, "◆ "+repository.Language)
	}
	switch {
	case repository.Archived:
		metadata = append(metadata, "archived")
	case repository.Visibility != "":
		metadata = append(metadata, repository.Visibility)
	case repository.Private:
		metadata = append(metadata, "private")
	}
	if repository.Fork {
		metadata = append(metadata, "fork")
	}
	content := m.styles.Bold.Render(repository.FullName)
	if len(metadata) > 0 {
		content += "  " + m.styles.Muted.Render(strings.Join(metadata, " · "))
	}
	return m.selectorCard(content, selected, true)
}

func (m *Model) pickerInfoOverlay() string {
	items := m.visiblePickerItems()
	if m.picker.cursor < 0 || m.picker.cursor >= len(items) || items[m.picker.cursor].repository == nil {
		return ""
	}
	return m.repositoryInfoPanel(*items[m.picker.cursor].repository, true)
}

func (m *Model) repositoryInfoPanel(repository elmapi.Repository, closeButton bool) string {
	const width = 36
	row := func(label, value string) string {
		return m.styles.Muted.Render(fmt.Sprintf("%-14s", label)) + value
	}
	visibility := repository.Visibility
	if visibility == "" && repository.Private {
		visibility = "private"
	}
	if visibility == "" {
		visibility = "unknown"
	}
	description := repository.Description
	if description == "" {
		description = "No description."
	}
	lines := []string{
		m.styles.Bold.Render(repository.FullName),
		"",
		m.styles.Muted.Render(description),
		"",
		row("★ Stars", fmt.Sprintf("%d", repository.Stargazers)),
		row("≡ Open issues", fmt.Sprintf("%d", repository.OpenIssueCount)),
		row("◆ Language", orUnset(repository.Language)),
		row("Visibility", visibility),
	}
	if repository.Archived {
		lines = append(lines, row("State", m.styles.Warning.Render("archived")))
	}
	if repository.Fork {
		lines = append(lines, row("Type", "fork"))
	}
	if closeButton {
		lines = append(lines, "", m.actionButtons([]actionItem{{id: "close", label: "Close", shortcut: "esc"}}, 0, width))
	}
	content := lipgloss.NewStyle().Width(width).Render(strings.Join(lines, "\n"))
	return m.panel(content)
}

func (m *Model) confirmationOverlay() string {
	width := min(60, max(24, m.contentWidth()-8))
	content := m.styles.Bold.Render(m.confirm.title) + "\n\n" +
		m.styles.Warning.Render(m.confirm.body) + "\n\n" +
		m.actionButtons(
			[]actionItem{{id: "confirm", label: "Confirm"}, {id: "cancel", label: "Cancel"}},
			m.confirm.focus,
			width-6,
		)
	return lipgloss.NewStyle().Width(width).Render(m.panel(content))
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
	capacity := max(1, (m.bodyHeight()-1)/linesPerCard)
	start = max(0, m.cursor-capacity+1)
	end = min(total, start+capacity)
	if end-start < capacity {
		start = max(0, end-capacity)
	}
	return start, end
}

func (m *Model) viewportView(body string, height int) string {
	width := m.contentWidth()
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
		"Actions",
		"  " + helpLine(keys.Left, keys.Open),
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
