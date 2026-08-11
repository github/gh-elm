package render

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/github/gh-elm/internal/elmapi"
	"github.com/github/gh-elm/internal/theme"
)

const emptyValue = "—"

// MigrationCreate renders a create-migration response.
func MigrationCreate(v elmapi.CreateMigrationResponse) string {
	styles := theme.New()
	expiresAt := valueOrEmpty(pointerString(v.ExpiresAt))
	return styles.Success.Bold(true).Render("Migration successfully created") + "\n" +
		field("Migration ID", styles.Bold.Render(valueOrEmpty(v.MigrationID))) + "\n" +
		styles.Muted.Render(fmt.Sprintf("  %-*s%s", fieldLabelWidth, "Expires", expiresAt)) + "\n\n"
}

// MigrationCancel renders a successful migration cancellation.
func MigrationCancel(migrationID string) string {
	styles := theme.New()
	return styles.Success.Render("✓") + " Migration " + valueOrEmpty(migrationID) + " cancelled.\n\n"
}

// MigrationStatus renders a migration status response.
func MigrationStatus(v elmapi.MigrationDetail) string {
	if v.Migration == nil && v.TargetState == nil && v.CombinedState == nil && len(v.Messages) == 0 {
		return "No migration status data returned.\n"
	}

	return joinSections(
		renderMigrationSummary(v.Migration),
		renderTargetState(v.TargetState),
		renderCombinedState(v.CombinedState),
		renderMessages(v.Messages),
	)
}

func renderMigrationSummary(migration *elmapi.MigrationSummary) string {
	if migration == nil {
		return ""
	}

	styles := theme.New()
	source := repositoryName(migration.SourceOrganizationLogin, migration.SourceRepositoryName)
	target := repositoryName(migration.TargetOrganizationLogin, migration.TargetRepositoryName)
	title := "Migration"
	if source != emptyValue || target != emptyValue {
		title = source + " → " + target
	}

	lines := []string{
		bullet(statusGlyph(pointerString(migration.Status)), statusText(pointerString(migration.Status))),
		field("Migration ID", styles.Bold.Render(valueOrEmpty(migration.MigrationID))),
	}
	if migration.TargetMigrationID != 0 {
		lines = append(lines, field("Target migration ID", styles.Bold.Render(strconv.FormatInt(migration.TargetMigrationID, 10))))
	}
	lines = append(lines,
		field("Visibility", pointerValue(migration.TargetVisibility)),
		field("Created", pointerValue(migration.CreatedAt)),
		field("Started", pointerValue(migration.StartedAt)),
		field("Completed", completedValue(migration.CompletedAt)),
		field("Expires", boldPointerValue(migration.ExpiresAt)),
	)

	return renderSection(title, lines...)
}

func renderTargetState(target *elmapi.TargetState) string {
	if target == nil {
		return ""
	}

	var sections []string
	availability := failureState(!target.TargetUnavailable, "Target available", "Target unavailable")
	sections = append(sections, renderSection("Target",
		bullet(statusGlyph(pointerString(target.Status)), statusText(pointerString(target.Status))),
		bullet(availability.glyph, availability.text),
	))

	for _, progress := range target.RepositoryProgress {
		sections = append(sections, renderRepositoryProgress(progress))
	}
	return joinSections(sections...)
}

func renderRepositoryProgress(progress elmapi.RepositoryProgress) string {
	lines := []string{
		progressLine("Backfill", progress.BackfillResourcesProcessed, progress.BackfillResourcesAdded, progress.BackfillResourcesFailed),
		progressLine("Live updates", progress.LiveUpdateResourcesProcessed, progress.LiveUpdateResourcesAdded, progress.LiveUpdateResourcesFailed),
	}

	resources := positiveState(progress.AllResourcesSent, "All resources sent", "Resources still being sent")
	gitPush := positiveState(progress.InitialGitPushComplete, "Initial Git push complete", "Initial Git push pending")
	lock := neutralState(progress.RepositoryLocked, "Source repository locked", "Source repository unlocked")
	lines = append(lines,
		bullet(resources.glyph, resources.text),
		bullet(gitPush.glyph, gitPush.text),
		bullet(lock.glyph, lock.text),
	)

	return renderSection("Progress · "+valueOrEmpty(progress.RepositoryNWO), lines...)
}

func renderCombinedState(combined *elmapi.CombinedState) string {
	if combined == nil {
		return ""
	}

	styles := theme.New()
	readiness := positiveState(combined.ReadyForCutover, "Ready for cutover", "Not ready for cutover")
	lines := []string{
		bullet(statusGlyph(pointerString(combined.Status)), statusText(pointerString(combined.Status))),
		bullet(readiness.glyph, readiness.text),
	}
	if combined.DisplayMessage != "" {
		lines = append(lines, detail(combined.DisplayMessage))
	}
	for _, blocker := range combined.CutoverBlockers {
		lines = append(lines, bullet(styles.Warning.Render("!"), styles.Warning.Render(blocker)))
	}

	var sections []string
	sections = append(sections, renderSection("Cutover", lines...))
	if len(combined.Repositories) > 0 {
		repositoryLines := make([]string, 0, len(combined.Repositories))
		for _, repository := range combined.Repositories {
			phase := friendlyValue(pointerString(repository.Phase))
			status := valueOrEmpty(repository.DisplayStatus)
			repositoryLines = append(repositoryLines, bullet(
				styles.Muted.Render("•"),
				styles.Bold.Render(valueOrEmpty(repository.RepositoryNWO))+
					styles.Muted.Render(" · "+phase+" · "+status),
			))
		}
		sections = append(sections, renderSection("Repository states", repositoryLines...))
	}
	return joinSections(sections...)
}

func renderMessages(messages []elmapi.MigrationMessage) string {
	if len(messages) == 0 {
		return ""
	}

	styles := theme.New()
	lines := make([]string, 0, len(messages))
	for _, message := range messages {
		glyph := styles.Muted.Render("•")
		textStyle := styles.Primary
		switch strings.ToLower(message.MessageType) {
		case "error", "failed", "failure":
			glyph = styles.Failure.Render("✗")
			textStyle = styles.Failure
		case "warning", "warn":
			glyph = styles.Warning.Render("!")
			textStyle = styles.Warning
		case "success":
			glyph = styles.Success.Render("✓")
			textStyle = styles.Success
		}

		line := bullet(glyph, textStyle.Render(valueOrEmpty(message.Message)))
		if message.CreatedAt != nil && *message.CreatedAt != "" {
			line += styles.Muted.Render(" · " + *message.CreatedAt)
		}
		lines = append(lines, line)
	}
	return renderSection("Messages", lines...)
}

// MigrationList renders a migration list response.
func MigrationList(v elmapi.ListMigrationsResponse) string {
	styles := theme.New()
	if len(v.Migrations) == 0 || v.TotalCount == 0 {
		return strings.Join([]string{
			styles.Bold.Render("Migrations"),
			"  No migrations available.",
			styles.Muted.Render("  Create one with `gh elm migration create --help`."),
		}, "\n") + "\n\n"
	}

	var cards []string
	for _, migration := range v.Migrations {
		source := repositoryName(migration.SourceOrganizationLogin, migration.SourceRepositoryName)
		target := repositoryName(migration.TargetOrganizationLogin, migration.TargetRepositoryName)
		card := []string{
			bullet(statusGlyph(pointerString(migration.Status)), statusText(pointerString(migration.Status))+
				"  "+styles.Bold.Render(source+" → "+target)),
			field("Migration ID", valueOrEmpty(migration.MigrationID)),
		}
		if migration.TargetMigrationID != 0 {
			card = append(card, field("Target migration ID", strconv.FormatInt(migration.TargetMigrationID, 10)))
		}
		card = append(card,
			field("Visibility", pointerValue(migration.TargetVisibility)),
			field("Created", pointerValue(migration.CreatedAt)),
			field("Started", pointerValue(migration.StartedAt)),
			field("Completed", completedValue(migration.CompletedAt)),
			field("Expires", boldPointerValue(migration.ExpiresAt)),
		)
		cards = append(cards, strings.Join(card, "\n"))
	}

	return joinSections(
		styles.Bold.Render(fmt.Sprintf("Migrations (%d)", len(v.Migrations))),
		strings.Join(cards, "\n\n"),
		renderListFooter(v),
	)
}

func renderListFooter(v elmapi.ListMigrationsResponse) string {
	styles := theme.New()
	summary := fmt.Sprintf("Showing %d", len(v.Migrations))
	if v.TotalCount > 0 {
		summary += fmt.Sprintf(" of %d", v.TotalCount)
	}
	if v.NextCursor != "" {
		summary += " · next cursor: " + v.NextCursor
	}
	return styles.Muted.Render(summary)
}

// MigrationRevertCutover renders a revert-cutover response.
func MigrationRevertCutover(v elmapi.RevertCutoverResponse) string {
	styles := theme.New()
	result := failureState(v.Success, "Cutover reverted", "Cutover was not reverted")
	return joinSections(
		bullet(result.glyph, styles.Bold.Render(result.text)),
		renderSection("Changes",
			stateLine(v.UnarchivedSourceRepository, "Source repository unarchived", "Source repository was already unarchived"),
			stateLine(v.InProgressCutoverTerminated, "In-progress cutover terminated", "No in-progress cutover to terminate"),
			stateLine(v.InProgressMigrationTerminated, "In-progress migration terminated", "No in-progress migration to terminate"),
		),
	)
}

func progressLine(label string, processed, added, failed int64) string {
	styles := theme.New()
	counts := fmt.Sprintf("%s / %s processed",
		styles.Bold.Render(strconv.FormatInt(processed, 10)),
		strconv.FormatInt(added, 10),
	)
	if failed > 0 {
		counts += "  " + styles.Failure.Render(fmt.Sprintf("✗ %d failed", failed))
	} else {
		counts += "  " + styles.Success.Render("✓ no failures")
	}
	return field(label, counts)
}

type state struct {
	glyph string
	text  string
}

func positiveState(value bool, trueText, falseText string) state {
	styles := theme.New()
	if value {
		return state{
			glyph: styles.Success.Render("✓"),
			text:  styles.Success.Render(trueText),
		}
	}
	return state{
		glyph: styles.Muted.Render("○"),
		text:  styles.Muted.Render(falseText),
	}
}

func failureState(value bool, trueText, falseText string) state {
	styles := theme.New()
	if value {
		return state{
			glyph: styles.Success.Render("✓"),
			text:  styles.Success.Render(trueText),
		}
	}
	return state{
		glyph: styles.Failure.Render("✗"),
		text:  styles.Failure.Render(falseText),
	}
}

func neutralState(value bool, trueText, falseText string) state {
	styles := theme.New()
	if value {
		return state{
			glyph: styles.Warning.Render("●"),
			text:  styles.Warning.Render(trueText),
		}
	}
	return state{
		glyph: styles.Muted.Render("○"),
		text:  styles.Muted.Render(falseText),
	}
}

func stateLine(value bool, trueText, falseText string) string {
	result := positiveState(value, trueText, falseText)
	return bullet(result.glyph, result.text)
}

func statusGlyph(status string) string {
	styles := theme.New()
	switch strings.ToLower(status) {
	case "completed", "complete", "success", "succeeded", "ready_for_cutover", "ready for cutover":
		return styles.Success.Render("✓")
	case "failed", "failure", "terminated", "cancelled", "canceled", "aborted", "expired":
		return styles.Failure.Render("✗")
	case "paused", "degraded":
		return styles.Paused.Render("Ⅱ")
	case "in_progress", "in progress", "processing", "backfilling", "exporting", "validating",
		"cutover_pending", "cutover pending", "cutover_finalizing", "cutover finalizing",
		"cutting_over", "cutting over":
		return styles.Active.Render("●")
	case "queued", "created", "":
		return styles.Muted.Render("○")
	default:
		return styles.Info.Render("●")
	}
}

func statusText(status string) string {
	styles := theme.New()
	text := friendlyValue(status)
	switch strings.ToLower(status) {
	case "completed", "complete", "success", "succeeded", "ready_for_cutover", "ready for cutover":
		return styles.Success.Bold(true).Render(text)
	case "failed", "failure", "terminated", "cancelled", "canceled", "aborted", "expired":
		return styles.Failure.Bold(true).Render(text)
	case "paused", "degraded":
		return styles.Paused.Bold(true).Render(text)
	case "in_progress", "in progress", "processing", "backfilling", "exporting", "validating",
		"cutover_pending", "cutover pending", "cutover_finalizing", "cutover finalizing",
		"cutting_over", "cutting over":
		return styles.Active.Bold(true).Render(text)
	case "queued", "created", "":
		return styles.Muted.Bold(true).Render(text)
	default:
		return styles.Info.Bold(true).Render(text)
	}
}

func friendlyValue(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "_", " "))
	if value == "" {
		return emptyValue
	}
	runes := []rune(strings.ToLower(value))
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

func pointerString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func pointerValue(value *string) string {
	styles := theme.New()
	if value == nil || *value == "" {
		return styles.Muted.Render(emptyValue)
	}
	return *value
}

func boldPointerValue(value *string) string {
	styles := theme.New()
	if value == nil || *value == "" {
		return styles.Muted.Render(emptyValue)
	}
	return styles.Bold.Render(*value)
}

func completedValue(value *string) string {
	styles := theme.New()
	if value == nil || *value == "" {
		return styles.Muted.Render(emptyValue)
	}
	return styles.Success.Render(*value)
}

func valueOrEmpty(value string) string {
	if value == "" {
		return emptyValue
	}
	return value
}

func repositoryName(owner, name string) string {
	switch {
	case owner == "":
		return valueOrEmpty(name)
	case name == "":
		return owner
	default:
		return fmt.Sprintf("%s/%s", owner, name)
	}
}
