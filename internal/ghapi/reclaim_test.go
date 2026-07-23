package ghapi

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// fakeClient implements reclaimClient for service tests.
type fakeClient struct {
	orgID               string
	mannequins          []Mannequin
	byLogin             map[string][]Mannequin
	userIDs             map[string]string
	userIDErr           error    // when set, UserID returns this for every lookup
	invitations         []string // "sourceID->targetID"
	reattributions      []string
	inviteErr           error
	reattributeErr      error
	reattributeAttempts int
}

func (f *fakeClient) OrganizationID(_ context.Context, _ string) (string, error) {
	return f.orgID, nil
}
func (f *fakeClient) UserID(_ context.Context, login string) (string, error) {
	if f.userIDErr != nil {
		return "", f.userIDErr
	}
	id, ok := f.userIDs[login]
	if !ok {
		return "", fmt.Errorf("user %q not found: %w", login, ErrUserNotFound)
	}
	return id, nil
}
func (f *fakeClient) Mannequins(_ context.Context, _ string) ([]Mannequin, error) {
	return f.mannequins, nil
}
func (f *fakeClient) MannequinsByLogin(_ context.Context, _, login string) ([]Mannequin, error) {
	return f.byLogin[login], nil
}
func (f *fakeClient) CreateAttributionInvitation(_ context.Context, _, mannequinID, targetUserID string) (*AttributionResult, error) {
	if f.inviteErr != nil {
		return nil, f.inviteErr
	}
	f.invitations = append(f.invitations, mannequinID+"->"+targetUserID)
	return &AttributionResult{SourceID: mannequinID, TargetID: targetUserID}, nil
}
func (f *fakeClient) ReattributeMannequinToUser(_ context.Context, _, mannequinID, targetUserID string) (*AttributionResult, error) {
	f.reattributeAttempts++
	if f.reattributeErr != nil {
		return nil, f.reattributeErr
	}
	f.reattributions = append(f.reattributions, mannequinID+"->"+targetUserID)
	return &AttributionResult{SourceID: mannequinID, TargetID: targetUserID}, nil
}

// capturingLogger records log lines for assertions.
type capturingLogger struct{ lines []string }

func (l *capturingLogger) Infof(format string, args ...any) {
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}
func (l *capturingLogger) Warnf(format string, args ...any) {
	l.lines = append(l.lines, "WARN: "+fmt.Sprintf(format, args...))
}
func (l *capturingLogger) contains(sub string) bool {
	for _, line := range l.lines {
		if strings.Contains(line, sub) {
			return true
		}
	}
	return false
}

func newService(f *fakeClient) (*ReclaimService, *capturingLogger) {
	log := &capturingLogger{}
	return &ReclaimService{client: f, log: log}, log
}

func TestReclaimMannequin(t *testing.T) {
	t.Run("reclaims a single mannequin via invitation", func(t *testing.T) {
		f := &fakeClient{
			orgID:   "ORG",
			byLogin: map[string][]Mannequin{"alice": {{ID: "m1", Login: "alice"}}},
			userIDs: map[string]string{"alice-target": "u1"},
		}
		svc, _ := newService(f)

		err := svc.ReclaimMannequin(t.Context(), "alice", "", "alice-target", "octo", false, false)
		if err != nil {
			t.Fatalf("ReclaimMannequin: %v", err)
		}
		if len(f.invitations) != 1 || f.invitations[0] != "m1->u1" {
			t.Errorf("invitations = %v", f.invitations)
		}
	})

	t.Run("errors when the login is not a mannequin", func(t *testing.T) {
		f := &fakeClient{orgID: "ORG", byLogin: map[string][]Mannequin{}}
		svc, _ := newService(f)

		err := svc.ReclaimMannequin(t.Context(), "ghost", "", "t", "octo", false, false)
		if err == nil || !strings.Contains(err.Error(), "not a mannequin") {
			t.Fatalf("expected not-a-mannequin error, got %v", err)
		}
	})

	t.Run("errors when already claimed without force", func(t *testing.T) {
		f := &fakeClient{
			orgID:   "ORG",
			byLogin: map[string][]Mannequin{"alice": {{ID: "m1", Login: "alice", MappedUser: &Claimant{Login: "someone"}}}},
			userIDs: map[string]string{"alice-target": "u1"},
		}
		svc, _ := newService(f)

		err := svc.ReclaimMannequin(t.Context(), "alice", "", "alice-target", "octo", false, false)
		if err == nil || !strings.Contains(err.Error(), "already mapped") {
			t.Fatalf("expected already-mapped error, got %v", err)
		}
		if len(f.invitations) != 0 {
			t.Errorf("should not have invited: %v", f.invitations)
		}
	})

	t.Run("uses reattribution for skip-invitation", func(t *testing.T) {
		f := &fakeClient{
			orgID:   "ORG",
			byLogin: map[string][]Mannequin{"alice": {{ID: "m1", Login: "alice"}}},
			userIDs: map[string]string{"alice-target": "u1"},
		}
		svc, _ := newService(f)

		err := svc.ReclaimMannequin(t.Context(), "alice", "", "alice-target", "octo", false, true)
		if err != nil {
			t.Fatalf("ReclaimMannequin: %v", err)
		}
		if len(f.reattributions) != 1 || len(f.invitations) != 0 {
			t.Errorf("reattributions=%v invitations=%v", f.reattributions, f.invitations)
		}
	})
}

func TestReclaimMannequins(t *testing.T) {
	recs := func(rows ...string) []MannequinRecord {
		out := make([]MannequinRecord, 0, len(rows))
		for _, r := range rows {
			fields := strings.Split(r, ",")
			out = append(out, MannequinRecord{MannequinUser: fields[0], MannequinID: fields[1], TargetUser: fields[2]})
		}
		return out
	}

	t.Run("reclaims each unique unclaimed row", func(t *testing.T) {
		f := &fakeClient{
			orgID: "ORG",
			mannequins: []Mannequin{
				{ID: "m1", Login: "alice"},
				{ID: "m2", Login: "bob"},
			},
			userIDs: map[string]string{"alice-t": "u1", "bob-t": "u2"},
		}
		svc, _ := newService(f)

		err := svc.ReclaimMannequins(t.Context(), recs("alice,m1,alice-t", "bob,m2,bob-t"), "octo", false, false)
		if err != nil {
			t.Fatalf("ReclaimMannequins: %v", err)
		}
		if len(f.invitations) != 2 {
			t.Errorf("invitations = %v", f.invitations)
		}
	})

	t.Run("skips claimed, missing, and duplicate rows", func(t *testing.T) {
		f := &fakeClient{
			orgID: "ORG",
			mannequins: []Mannequin{
				{ID: "m1", Login: "alice", MappedUser: &Claimant{Login: "old"}}, // already claimed
				{ID: "m2", Login: "bob"},
			},
			userIDs: map[string]string{"alice-t": "u1", "bob-t": "u2", "dup-t": "u3"},
		}
		svc, log := newService(f)

		lines := recs(
			"alice,m1,alice-t", // claimed -> skipped
			"ghost,m9,x",       // not found -> skipped
			"bob,m2,bob-t",     // duplicate below -> both skipped
			"bob,m2,dup-t",
		)
		if err := svc.ReclaimMannequins(t.Context(), lines, "octo", false, false); err != nil {
			t.Fatalf("ReclaimMannequins: %v", err)
		}
		if len(f.invitations) != 0 {
			t.Errorf("expected no reclaims, got %v", f.invitations)
		}
		if !log.contains("already claimed") || !log.contains("not found") || !log.contains("duplicate") {
			t.Errorf("missing skip warnings: %v", log.lines)
		}
	})

	t.Run("force reclaims an already-claimed row", func(t *testing.T) {
		f := &fakeClient{
			orgID:      "ORG",
			mannequins: []Mannequin{{ID: "m1", Login: "alice", MappedUser: &Claimant{Login: "old"}}},
			userIDs:    map[string]string{"alice-t": "u1"},
		}
		svc, _ := newService(f)

		if err := svc.ReclaimMannequins(t.Context(), recs("alice,m1,alice-t"), "octo", true, false); err != nil {
			t.Fatalf("ReclaimMannequins: %v", err)
		}
		if len(f.invitations) != 1 {
			t.Errorf("invitations = %v", f.invitations)
		}
	})

	t.Run("skips a row whose claimant does not resolve", func(t *testing.T) {
		f := &fakeClient{
			orgID: "ORG",
			mannequins: []Mannequin{
				{ID: "m1", Login: "alice"},
				{ID: "m2", Login: "bob"},
			},
			userIDs: map[string]string{"bob-t": "u2"}, // alice-t is absent -> ErrUserNotFound
		}
		svc, log := newService(f)

		err := svc.ReclaimMannequins(t.Context(), recs("alice,m1,alice-t", "bob,m2,bob-t"), "octo", false, false)
		if err != nil {
			t.Fatalf("ReclaimMannequins: %v", err)
		}
		// alice-t is skipped; bob-t still reclaimed.
		if len(f.invitations) != 1 || f.invitations[0] != "m2->u2" {
			t.Errorf("invitations = %v", f.invitations)
		}
		if !log.contains("Claimant \"alice-t\" not found") {
			t.Errorf("missing not-found warning: %v", log.lines)
		}
	})

	t.Run("propagates a non-not-found UserID error", func(t *testing.T) {
		authErr := &HTTPError{StatusCode: 401, Status: "401 Unauthorized", Message: "Bad credentials"}
		f := &fakeClient{
			orgID:      "ORG",
			mannequins: []Mannequin{{ID: "m1", Login: "alice"}},
			userIDErr:  authErr,
		}
		svc, _ := newService(f)

		err := svc.ReclaimMannequins(t.Context(), recs("alice,m1,alice-t"), "octo", false, false)
		if !errors.Is(err, authErr) {
			t.Fatalf("expected the auth error to propagate, got %v", err)
		}
		if len(f.invitations) != 0 {
			t.Errorf("should not have reclaimed on a hard error: %v", f.invitations)
		}
	})

	t.Run("skip-invitation fails fast when unavailable (non-EMU)", func(t *testing.T) {
		f := &fakeClient{
			orgID: "ORG",
			mannequins: []Mannequin{
				{ID: "m1", Login: "alice"},
				{ID: "m2", Login: "bob"},
			},
			userIDs:        map[string]string{"alice-t": "u1", "bob-t": "u2"},
			reattributeErr: &GraphQLError{Messages: []string{"acme is not an Enterprise Managed Users (EMU) organization"}},
		}
		svc, log := newService(f)

		err := svc.ReclaimMannequins(t.Context(), recs("alice,m1,alice-t", "bob,m2,bob-t"), "octo", false, true)
		if err != nil {
			t.Fatalf("ReclaimMannequins: %v", err)
		}
		// Must stop after the first row rather than treating it as a soft skip
		// and continuing through the batch.
		if f.reattributeAttempts != 1 {
			t.Errorf("reattributeAttempts = %d, want 1 (fail-fast)", f.reattributeAttempts)
		}
		if !log.contains("not enabled") {
			t.Errorf("missing unavailability warning: %v", log.lines)
		}
	})
}

func TestIsSkipInvitationUnavailable(t *testing.T) {
	t.Run("missing mutation", func(t *testing.T) {
		err := &GraphQLError{Messages: []string{"Field 'reattributeMannequinToUser' doesn't exist on type 'Mutation'"}}
		if !isSkipInvitationUnavailable(err) {
			t.Error("expected true for the missing-mutation error")
		}
	})

	t.Run("non-EMU organization", func(t *testing.T) {
		err := &GraphQLError{Messages: []string{"acme is not an Enterprise Managed Users (EMU) organization"}}
		if !isSkipInvitationUnavailable(err) {
			t.Error("expected true for the non-EMU rejection")
		}
	})

	t.Run("unrelated error", func(t *testing.T) {
		if isSkipInvitationUnavailable(&GraphQLError{Messages: []string{"other"}}) {
			t.Error("expected false for an unrelated error")
		}
	})
}
