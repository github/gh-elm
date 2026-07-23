package ghapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

// CSVHeader is the header row of the mannequin CSV, shared by fetch-mannequins
// (which writes it) and reclaim-mannequin (which validates it).
const CSVHeader = "mannequin-user,mannequin-id,target-user"

// reclaimClient is the subset of *Client the ReclaimService needs. It exists so
// the service can be unit-tested with a fake.
type reclaimClient interface {
	OrganizationID(ctx context.Context, org string) (string, error)
	UserID(ctx context.Context, login string) (string, error)
	Mannequins(ctx context.Context, orgID string) ([]Mannequin, error)
	MannequinsByLogin(ctx context.Context, orgID, login string) ([]Mannequin, error)
	CreateAttributionInvitation(ctx context.Context, orgID, mannequinID, targetUserID string) (*AttributionResult, error)
	ReattributeMannequinToUser(ctx context.Context, orgID, mannequinID, targetUserID string) (*AttributionResult, error)
}

// Logger receives human-readable progress and warnings from the service.
type Logger interface {
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
}

// ReclaimService orchestrates mannequin reclaiming, mirroring gh-gei's
// ReclaimService: a single reclaim by login/id, or a batch reclaim from CSV.
type ReclaimService struct {
	client reclaimClient
	log    Logger
}

// NewReclaimService builds a ReclaimService over a *Client.
func NewReclaimService(client *Client, log Logger) *ReclaimService {
	return &ReclaimService{client: client, log: log}
}

// ReclaimMannequin reclaims a single mannequin (all identities sharing the given
// login, optionally narrowed by id) to targetUser.
func (s *ReclaimService) ReclaimMannequin(ctx context.Context, mannequinUser, mannequinID, targetUser, githubOrg string, force, skipInvitation bool) error {
	orgID, err := s.client.OrganizationID(ctx, githubOrg)
	if err != nil {
		return err
	}

	byLogin, err := s.client.MannequinsByLogin(ctx, orgID, mannequinUser)
	if err != nil {
		return err
	}
	matches := filterByLoginID(byLogin, mannequinUser, mannequinID)
	if len(matches) == 0 {
		return fmt.Errorf("user %s is not a mannequin", mannequinUser)
	}

	if !force && anyClaimed(matches) {
		return fmt.Errorf("user %s is already mapped to a user; use --force to reclaim again", mannequinUser)
	}

	targetUserID, err := s.client.UserID(ctx, targetUser)
	if err != nil {
		return err
	}

	failed := false
	for _, m := range uniqueMannequins(matches) {
		if !s.reclaimOne(ctx, orgID, m, targetUser, targetUserID, skipInvitation) {
			failed = true
			if skipInvitation {
				// Skip-invitation failures are fail-fast in gh-gei.
				return errors.New("failed to reclaim mannequin")
			}
		}
	}
	if failed {
		return errors.New("failed to send reclaim mannequin invitation(s)")
	}
	return nil
}

// ReclaimMannequins reclaims mannequins listed in a parsed CSV (including the
// header line).
func (s *ReclaimService) ReclaimMannequins(ctx context.Context, lines []string, githubOrg string, force, skipInvitation bool) error {
	if len(lines) == 0 {
		s.log.Warnf("File is empty. Nothing to reclaim.")
		return nil
	}
	if !strings.EqualFold(strings.TrimSpace(lines[0]), CSVHeader) {
		return fmt.Errorf("invalid CSV header, should be: %s", CSVHeader)
	}

	orgID, err := s.client.OrganizationID(ctx, githubOrg)
	if err != nil {
		return err
	}
	all, err := s.client.Mannequins(ctx, orgID)
	if err != nil {
		return err
	}

	parsed := s.parseCSV(lines[1:])

	for _, p := range parsed {
		if p.login == "" {
			continue
		}
		if !force && claimedByLoginID(all, p.login, p.id) {
			s.log.Warnf("%s is already claimed. Skipping (use --force to reclaim).", p.login)
			continue
		}
		if findByLoginID(all, p.login, p.id) == nil {
			s.log.Warnf("Mannequin %s not found. Skipping.", p.login)
			continue
		}
		if countByLoginID(parsed, p.login, p.id) > 1 {
			s.log.Warnf("Mannequin %s is a duplicate. Skipping.", p.login)
			continue
		}

		claimantID, err := s.client.UserID(ctx, p.targetUser)
		if err != nil {
			if errors.Is(err, ErrUserNotFound) {
				s.log.Warnf("Claimant %q not found. Skipping.", p.targetUser)
				continue
			}
			// Auth/network/other failures must not be silently skipped.
			return err
		}

		m := Mannequin{ID: p.id, Login: p.login}
		if !s.reclaimOne(ctx, orgID, m, p.targetUser, claimantID, skipInvitation) && skipInvitation {
			// Fail-fast for skip-invitation, matching gh-gei.
			return nil
		}
	}
	return nil
}

// reclaimOne performs one reclaim and logs the outcome. It returns false on a
// hard failure that the caller should treat as an error.
func (s *ReclaimService) reclaimOne(ctx context.Context, orgID string, m Mannequin, targetUser, targetUserID string, skipInvitation bool) bool {
	if skipInvitation {
		result, err := s.client.ReattributeMannequinToUser(ctx, orgID, m.ID, targetUserID)
		return s.handleReattribution(m, targetUser, targetUserID, result, err)
	}
	result, err := s.client.CreateAttributionInvitation(ctx, orgID, m.ID, targetUserID)
	return s.handleInvitation(m, targetUser, targetUserID, result, err)
}

func (s *ReclaimService) handleInvitation(m Mannequin, targetUser, targetUserID string, result *AttributionResult, err error) bool {
	if err != nil {
		s.log.Warnf("Failed to send reclaim invitation email to %s for mannequin %s (%s): %v", targetUser, m.Login, m.ID, err)
		return false
	}
	if result == nil || result.SourceID != m.ID || result.TargetID != targetUserID {
		s.log.Warnf("Failed to send reclaim invitation email to %s for mannequin %s (%s)", targetUser, m.Login, m.ID)
		return false
	}
	s.log.Infof("Mannequin reclaim invitation email successfully sent to %s for %s (%s)", targetUser, m.Login, m.ID)
	return true
}

func (s *ReclaimService) handleReattribution(m Mannequin, targetUser, targetUserID string, result *AttributionResult, err error) bool {
	if err != nil {
		if isSkipInvitationUnavailable(err) {
			s.log.Warnf("Reclaiming mannequins with --skip-invitation is not enabled for your GitHub organization. For more details, contact GitHub Support.")
			return false
		}
		// "Target must be a member" and similar are per-mannequin soft failures.
		s.log.Warnf("Failed to reattribute content belonging to mannequin %s (%s) to %s: %v", m.Login, m.ID, targetUser, err)
		return true
	}
	if result == nil || result.SourceID != m.ID || result.TargetID != targetUserID {
		s.log.Warnf("Failed to reattribute content belonging to mannequin %s (%s) to %s", m.Login, m.ID, targetUser)
		return true
	}
	s.log.Infof("Successfully reclaimed content belonging to mannequin %s (%s) to %s", m.Login, m.ID, targetUser)
	return true
}

// isSkipInvitationUnavailable reports whether err indicates --skip-invitation is
// unavailable for the target org. This has two forms: older orgs where the
// reattributeMannequinToUser mutation doesn't exist at all, and orgs where the
// mutation exists but rejects the call because the org is not EMU. Both must be
// treated as fail-fast (not a soft per-mannequin skip) so a batch can't report
// success while reclaiming nothing.
func isSkipInvitationUnavailable(err error) bool {
	var gqlErr *GraphQLError
	if !errors.As(err, &gqlErr) {
		return false
	}
	for _, m := range gqlErr.Messages {
		if (strings.Contains(m, "reattributeMannequinToUser") && strings.Contains(m, "doesn't exist")) ||
			strings.Contains(m, "is not an Enterprise Managed Users (EMU) organization") {
			return true
		}
	}
	return false
}

// csvRow is one parsed, trimmed CSV line.
type csvRow struct {
	login      string
	id         string
	targetUser string
}

// parseCSV parses data rows (no header), warning on and skipping malformed rows.
func (s *ReclaimService) parseCSV(lines []string) []csvRow {
	var rows []csvRow
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, ",")
		if len(fields) != 3 {
			s.log.Warnf("Invalid line: %q. Will ignore it.", line)
			continue
		}
		login := strings.TrimSpace(fields[0])
		id := strings.TrimSpace(fields[1])
		target := strings.TrimSpace(fields[2])
		if login == "" || id == "" || target == "" {
			s.log.Warnf("Invalid line: %q. Missing a required field. Will ignore it.", line)
			continue
		}
		rows = append(rows, csvRow{login: login, id: id, targetUser: target})
	}
	return rows
}

// WriteCSV writes mannequins as CSV to w. When includeReclaimed is false, only
// unclaimed mannequins are written.
func WriteCSV(w io.Writer, mannequins []Mannequin, includeReclaimed bool) error {
	if _, err := fmt.Fprintln(w, CSVHeader); err != nil {
		return err
	}
	for _, m := range mannequins {
		if !includeReclaimed && m.MappedUser != nil {
			continue
		}
		target := ""
		if m.MappedUser != nil {
			target = m.MappedUser.Login
		}
		if _, err := fmt.Fprintf(w, "%s,%s,%s\n", m.Login, m.ID, target); err != nil {
			return err
		}
	}
	return nil
}

func filterByLoginID(mannequins []Mannequin, login, id string) []Mannequin {
	var out []Mannequin
	for _, m := range mannequins {
		if strings.EqualFold(m.Login, login) && (id == "" || strings.EqualFold(m.ID, id)) {
			out = append(out, m)
		}
	}
	return out
}

func findByLoginID(mannequins []Mannequin, login, id string) *Mannequin {
	for i := range mannequins {
		if strings.EqualFold(mannequins[i].Login, login) && strings.EqualFold(mannequins[i].ID, id) {
			return &mannequins[i]
		}
	}
	return nil
}

func claimedByLoginID(mannequins []Mannequin, login, id string) bool {
	for _, m := range mannequins {
		if strings.EqualFold(m.Login, login) && strings.EqualFold(m.ID, id) && m.MappedUser != nil {
			return true
		}
	}
	return false
}

func anyClaimed(mannequins []Mannequin) bool {
	for _, m := range mannequins {
		if m.MappedUser != nil {
			return true
		}
	}
	return false
}

func countByLoginID(rows []csvRow, login, id string) int {
	n := 0
	for _, r := range rows {
		if r.login == login && r.id == id {
			n++
		}
	}
	return n
}

// uniqueMannequins de-duplicates by id+login so we map each identity once.
func uniqueMannequins(mannequins []Mannequin) []Mannequin {
	seen := make(map[string]bool, len(mannequins))
	var out []Mannequin
	for _, m := range mannequins {
		key := m.ID + "__" + m.Login
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, m)
	}
	return out
}
