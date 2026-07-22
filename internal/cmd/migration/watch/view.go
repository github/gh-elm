package watch

import (
	"fmt"
	"strings"
	"time"
)

// w writes s to b, discarding the return values.
// strings.Builder.WriteString never returns an error.
func w(b *strings.Builder, s string) {
	_, _ = b.WriteString(s)
}

// parseTime parses an ISO-8601 (RFC3339) timestamp string, returning ok=false
// when the pointer is nil/empty or the value does not parse. The REST API emits
// timestamps as strings (nil when unset), replacing the proto timestamppb.
func parseTime(s *string) (time.Time, bool) {
	if s == nil || *s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, *s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// View renders the full watch display.
func (m Model) View() string {
	if m.detail == nil {
		return "Loading migration status...\n"
	}

	var b strings.Builder

	w(&b, m.renderHeader())
	w(&b, "\n")
	w(&b, m.renderTimeline())
	w(&b, m.renderPreflight())
	w(&b, m.renderGitSync())
	w(&b, m.renderMessages())
	w(&b, "\n")
	w(&b, m.renderFooter())

	return b.String()
}

func (m Model) renderHeader() string {
	info := m.detail.Migration
	if info == nil {
		return m.styles.MigrationID.Render("Migration") + "  " + m.migrationID + "\n"
	}

	sourceNwo := info.SourceOrganizationLogin + "/" + info.SourceRepositoryName
	targetNwo := info.TargetOrganizationLogin + "/" + info.TargetRepositoryName

	id := m.migrationID
	if len(id) > 12 {
		id = id[:12] + "..."
	}

	line1 := fmt.Sprintf("%s  %s  %s → %s",
		m.styles.Label.Render("Migration"),
		m.styles.MigrationID.Render(id),
		sourceNwo,
		targetNwo,
	)

	line2 := fmt.Sprintf("           %s: %s  %s: %s  %s: %s",
		m.styles.Label.Render("Source org"),
		info.SourceOrganizationLogin,
		m.styles.Label.Render("Target org"),
		info.TargetOrganizationLogin,
		m.styles.Label.Render("Visibility"),
		visibilityString(info.TargetVisibility),
	)

	return line1 + "\n" + line2 + "\n"
}

func visibilityString(v *string) string {
	if v == nil || *v == "" {
		return "unknown"
	}
	return *v
}

// orderedPhases defines the display order for the timeline.
var orderedPhases = []Phase{
	PhaseCreated,
	PhaseValidating,
	PhaseQueued,
	PhaseExporting,
	PhaseBackfilling,
	PhaseReadyForCutover,
	PhaseCuttingOver,
	PhaseCompleted,
}

func (m Model) renderTimeline() string {
	var b strings.Builder

	for _, p := range orderedPhases {
		indicator := m.phaseIndicator(p)
		name := m.phaseNameStyled(p)
		ts := m.phaseTimestamp(p)

		if ts != "" {
			w(&b, fmt.Sprintf("  %s %s %s\n", indicator, name, m.styles.Timestamp.Render(ts)))
		} else {
			w(&b, fmt.Sprintf("  %s %s\n", indicator, name))
		}

		detail := m.phaseDetail(p)
		if detail != "" {
			for line := range strings.SplitSeq(detail, "\n") {
				w(&b, "    "+line+"\n")
			}
		}

		w(&b, "\n")
	}

	// Show overlay states below the timeline
	switch m.overlay {
	case OverlayPaused:
		w(&b, fmt.Sprintf("  %s %s\n",
			m.styles.PhasePaused.Render("⏸"),
			m.styles.PhaseName.Render("Paused"),
		))
		w(&b, "    Migration paused\n\n")
	case OverlayDegraded:
		w(&b, fmt.Sprintf("  %s %s\n",
			m.styles.Warning.Render("⚠"),
			m.styles.PhaseName.Render("Degraded"),
		))
		if cs := m.detail.CombinedState; cs != nil && cs.DisplayMessage != "" {
			w(&b, "    "+cs.DisplayMessage+"\n\n")
		} else {
			w(&b, "    Migration running in degraded state\n\n")
		}
	}

	return b.String()
}

func (m Model) phaseIndicator(p Phase) string {
	switch {
	case m.overlay == OverlayFailed && p == PhaseCompleted:
		return m.styles.PhaseFailed.Render("✗")
	case m.overlay == OverlayTerminated && p == PhaseCompleted:
		return m.styles.PhaseFailed.Render("✗")
	case p == m.basePhase:
		if m.overlay == OverlayFailed || m.overlay == OverlayTerminated {
			return m.styles.PhaseFailed.Render("✗")
		}
		return m.styles.PhaseActive.Render("◉")
	case phaseOrdinal(p) < phaseOrdinal(m.basePhase):
		return m.styles.PhaseComplete.Render("✓")
	default:
		return m.styles.PhasePending.Render("○")
	}
}

func phaseOrdinal(p Phase) int {
	for i, op := range orderedPhases {
		if op == p {
			return i
		}
	}
	return -1
}

func (m Model) phaseNameStyled(p Phase) string {
	if p == m.basePhase {
		return m.styles.PhaseNameActive.Render(p.String())
	}
	return m.styles.PhaseName.Render(p.String())
}

func (m Model) phaseTimestamp(p Phase) string {
	info := m.detail.Migration
	if info == nil {
		return ""
	}

	var t time.Time
	var ok bool
	switch p {
	case PhaseCreated:
		t, ok = parseTime(info.CreatedAt)
	case PhaseExporting, PhaseQueued:
		if phaseOrdinal(p) <= phaseOrdinal(m.basePhase) {
			t, ok = parseTime(info.StartedAt)
		}
	case PhaseCompleted:
		t, ok = parseTime(info.CompletedAt)
	}

	if !ok {
		return ""
	}

	return formatTimestamp(t)
}

func formatTimestamp(t time.Time) string {
	now := time.Now().UTC()
	t = t.UTC()

	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return t.Format("15:04:05 UTC")
	}
	return t.Format("2006-01-02 15:04:05 UTC")
}

func (m Model) phaseDetail(p Phase) string {
	switch {
	case p == PhaseCreated && phaseOrdinal(p) <= phaseOrdinal(m.basePhase):
		return "Migration created"
	case p == PhaseQueued && phaseOrdinal(p) == phaseOrdinal(m.basePhase):
		return "Migration queued, waiting to start..."
	case p == PhaseValidating && phaseOrdinal(p) == phaseOrdinal(m.basePhase):
		return "Running preflight checks..."
	case p == PhaseValidating && phaseOrdinal(p) < phaseOrdinal(m.basePhase):
		return "Preflight checks passed"
	case p == PhaseExporting && phaseOrdinal(p) == phaseOrdinal(m.basePhase):
		return "Exporting data from source..."
	case p == PhaseExporting && phaseOrdinal(p) < phaseOrdinal(m.basePhase):
		return "Export complete"
	case p == PhaseBackfilling && phaseOrdinal(p) <= phaseOrdinal(m.basePhase):
		return m.backfillDetail()
	case p == PhaseReadyForCutover && phaseOrdinal(p) == phaseOrdinal(m.basePhase):
		return m.readyForCutoverDetail()
	case p == PhaseCuttingOver && phaseOrdinal(p) <= phaseOrdinal(m.basePhase):
		return m.cutoverDetail()
	case p == PhaseCompleted && m.basePhase == PhaseCompleted && m.overlay == OverlayNone:
		return m.completedDetail()
	case p == PhaseCompleted && m.overlay == OverlayTerminated:
		return m.styles.PhaseFailed.Render("Migration terminated")
	case p == PhaseCompleted && m.overlay == OverlayFailed:
		return m.failedDetail()
	}
	return ""
}

func (m Model) backfillDetail() string {
	rp := firstProgress(m.detail)
	if rp == nil {
		return ""
	}

	sent := rp.BackfillResourcesAdded
	processed := rp.BackfillResourcesProcessed
	failed := rp.BackfillResourcesFailed

	barWidth := int64(40)
	if m.width > 0 && m.width < 80 {
		barWidth = max(int64(m.width-40), 20)
	}

	var b strings.Builder
	w(&b, renderProgressBar(processed, sent, rp.AllResourcesSent, barWidth, m.styles))

	gitPush := boolCheck(rp.InitialGitPushComplete, m.styles)
	resourcesSent := boolCheck(rp.AllResourcesSent, m.styles)

	failedStr := fmt.Sprintf("%d", failed)
	if failed > 0 {
		failedStr = m.styles.PhaseFailed.Render(fmt.Sprintf("%d", failed))
	}

	w(&b, fmt.Sprintf("Failed: %s  Git push: %s  All resources sent: %s",
		failedStr, gitPush, resourcesSent))

	// Live updates run alongside backfill; surface their counts when present.
	if rp.LiveUpdateResourcesAdded > 0 || rp.LiveUpdateResourcesProcessed > 0 {
		liveFailed := fmt.Sprintf("%d", rp.LiveUpdateResourcesFailed)
		if rp.LiveUpdateResourcesFailed > 0 {
			liveFailed = m.styles.PhaseFailed.Render(liveFailed)
		}
		w(&b, fmt.Sprintf("\nLive updates: %d processed / %d sent  Failed: %s",
			rp.LiveUpdateResourcesProcessed, rp.LiveUpdateResourcesAdded, liveFailed))
	}

	return b.String()
}

func (m Model) readyForCutoverDetail() string {
	var b strings.Builder
	w(&b, "Backfill complete. Ready for cutover.\n")

	if cs := m.detail.CombinedState; cs != nil {
		blockers := cs.CutoverBlockers
		if len(blockers) > 0 {
			w(&b, m.styles.Warning.Render("Blockers:")+"\n")
			for _, blocker := range blockers {
				w(&b, "  - "+blocker+"\n")
			}
		}
	}

	w(&b, fmt.Sprintf("Run: gh elm migration cutover-to-destination --migration-id %s", m.migrationID))
	return b.String()
}

func (m Model) cutoverDetail() string {
	rp := firstProgress(m.detail)
	if rp == nil {
		return "Waiting for cutover status..."
	}

	locked := boolCheck(rp.RepositoryLocked, m.styles)
	gitPush := boolCheck(rp.InitialGitPushComplete, m.styles)
	resourcesSent := boolCheck(rp.AllResourcesSent, m.styles)

	var b strings.Builder
	w(&b, fmt.Sprintf("Repo locked: %s  Git push: %s  All resources sent: %s",
		locked, gitPush, resourcesSent))

	if cs := m.detail.CombinedState; cs != nil && cs.DisplayMessage != "" {
		w(&b, "\n"+m.styles.Warning.Render(cs.DisplayMessage))
	}

	return b.String()
}

func (m Model) completedDetail() string {
	info := m.detail.Migration
	if info == nil {
		return "Migration completed successfully."
	}

	msg := "Migration completed successfully."

	started, sok := parseTime(info.StartedAt)
	completed, cok := parseTime(info.CompletedAt)
	if sok && cok {
		duration := completed.Sub(started)
		msg += fmt.Sprintf("\nDuration: %s", formatDuration(duration))
	}

	return msg
}

func (m Model) failedDetail() string {
	if cs := m.detail.CombinedState; cs != nil && cs.DisplayMessage != "" {
		return m.styles.PhaseFailed.Render(cs.DisplayMessage)
	}
	return m.styles.PhaseFailed.Render("Migration failed")
}

// gitSyncMessagePrefix is the prefix the server attaches to git-sync messages
// in MigrationMessage.Message. Format: "[git sync: <source>] <detail>".
const gitSyncMessagePrefix = "[git sync: "

// preflightMessagePrefix is the prefix the server attaches to preflight check
// messages in MigrationMessage.Message. Format: "[preflight: <check>] <detail>".
const preflightMessagePrefix = "[preflight: "

// parsePrefixedMessage returns the label and detail text from a message of the
// form "<prefix><label>] <detail>", or ok=false if the prefix is not present.
func parsePrefixedMessage(prefix, msg string) (label, detail string, ok bool) {
	if !strings.HasPrefix(msg, prefix) {
		return "", "", false
	}
	rest := msg[len(prefix):]
	label, detail, ok = strings.Cut(rest, "] ")
	if !ok {
		return "", "", false
	}
	return label, detail, true
}

// parseGitSyncMessage returns the source name and detail text from a git-sync
// message, or ok=false if the message is not a git-sync message.
func parseGitSyncMessage(msg string) (source, detail string, ok bool) {
	return parsePrefixedMessage(gitSyncMessagePrefix, msg)
}

// parsePreflightMessage returns the check name and detail text from a preflight
// message, or ok=false if the message is not a preflight message.
func parsePreflightMessage(msg string) (check, detail string, ok bool) {
	return parsePrefixedMessage(preflightMessagePrefix, msg)
}

func (m Model) renderGitSync() string {
	if m.detail == nil {
		return ""
	}

	var lines []string
	for _, msg := range m.detail.Messages {
		source, detail, ok := parseGitSyncMessage(msg.Message)
		if !ok {
			continue
		}
		ts := ""
		if t, ok := parseTime(msg.CreatedAt); ok {
			ts = " " + m.styles.Timestamp.Render(formatTimestamp(t))
		}
		if msg.MessageType == "error" {
			lines = append(lines, "  "+m.styles.MessageError.Render("✗ "+source+": "+detail)+ts)
		} else {
			lines = append(lines, "  "+m.styles.MessageInfo.Render("✓ "+source+": "+detail)+ts)
		}
	}

	if len(lines) == 0 {
		return ""
	}

	var b strings.Builder
	w(&b, m.styles.Label.Render("Git Sync")+"\n")
	for _, line := range lines {
		w(&b, line+"\n")
	}
	return b.String()
}

func (m Model) renderPreflight() string {
	if m.detail == nil {
		return ""
	}

	var lines []string
	for _, msg := range m.detail.Messages {
		check, detail, ok := parsePreflightMessage(msg.Message)
		if !ok {
			continue
		}
		ts := ""
		if t, ok := parseTime(msg.CreatedAt); ok {
			ts = " " + m.styles.Timestamp.Render(formatTimestamp(t))
		}
		if msg.MessageType == "error" {
			lines = append(lines, "  "+m.styles.MessageError.Render("✗ "+check+": "+detail)+ts)
		} else {
			lines = append(lines, "  "+m.styles.MessageInfo.Render("✓ "+check+": "+detail)+ts)
		}
	}

	if len(lines) == 0 {
		return ""
	}

	var b strings.Builder
	w(&b, m.styles.Label.Render("Preflight")+"\n")
	for _, line := range lines {
		w(&b, line+"\n")
	}
	return b.String()
}

func (m Model) renderMessages() string {
	if m.detail == nil {
		return ""
	}

	var lines []string
	for _, msg := range m.detail.Messages {
		if _, _, isGitSync := parseGitSyncMessage(msg.Message); isGitSync {
			continue
		}
		if _, _, isPreflight := parsePreflightMessage(msg.Message); isPreflight {
			continue
		}
		if msg.MessageType == "error" {
			lines = append(lines, "  "+m.styles.MessageError.Render("✗ "+msg.Message))
		} else {
			lines = append(lines, "  "+m.styles.MessageInfo.Render("· "+msg.Message))
		}
	}

	var b strings.Builder
	w(&b, m.styles.Label.Render("Messages")+"\n")
	if len(lines) == 0 {
		w(&b, "  "+m.styles.MessageInfo.Render("No messages")+"\n")
		return b.String()
	}
	for _, line := range lines {
		w(&b, line+"\n")
	}
	return b.String()
}

func (m Model) renderFooter() string {
	var parts []string

	if !m.lastUpdated.IsZero() {
		parts = append(parts, "Last updated: "+formatTimestamp(m.lastUpdated))
	}
	parts = append(parts, fmt.Sprintf("Refreshing every %s", m.interval), "Ctrl-C to exit")

	footer := m.styles.Footer.Render(strings.Join(parts, " · "))

	if m.fetchErr != nil {
		warning := m.styles.Warning.Render("⚠ Failed to refresh (retrying...)")
		return warning + "\n" + footer
	}

	return footer
}

// Helper functions

// renderProgressBar renders the backfill progress bar. The trailing
// percentage is suppressed until AllResourcesSent is true.
func renderProgressBar(processed, sent int64, allSent bool, width int64, s Styles) string {
	if width <= 0 {
		width = 40
	}

	var filled int64
	if sent > 0 {
		filled = processed * width / sent
	}
	if filled > width {
		filled = width
	}

	bar := s.ProgressFull.Render(strings.Repeat("█", int(filled))) +
		s.ProgressEmpty.Render(strings.Repeat("░", int(width-filled)))

	if allSent && sent > 0 {
		pct := processed * 100 / sent
		return fmt.Sprintf("%s %d processed / %d sent (%d%%)\n", bar, processed, sent, pct)
	}
	return fmt.Sprintf("%s %d processed / %d sent (discovering...)\n", bar, processed, sent)
}

func boolCheck(val bool, s Styles) string {
	if val {
		return s.CheckOK.Render("✓")
	}
	return s.CheckFail.Render("✗")
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
}
