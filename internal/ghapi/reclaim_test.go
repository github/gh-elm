package ghapi

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// fakeClient implements reclaimClient for service tests.
type fakeClient struct {
	orgID          string
	mannequins     []Mannequin
	byLogin        map[string][]Mannequin
	userIDs        map[string]string
	invitations    []string // "sourceID->targetID"
	reattributions []string
	inviteErr      error
	reattributeErr error
}

func (f *fakeClient) OrganizationID(_ context.Context, _ string) (string, error) {
	return f.orgID, nil
}
func (f *fakeClient) UserID(_ context.Context, login string) (string, error) {
	id, ok := f.userIDs[login]
	if !ok {
		return "", fmt.Errorf("user %q not found", login)
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
	csv := func(rows ...string) []string {
		return append([]string{CSVHeader}, rows...)
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

		err := svc.ReclaimMannequins(t.Context(), csv("alice,m1,alice-t", "bob,m2,bob-t"), "octo", false, false)
		if err != nil {
			t.Fatalf("ReclaimMannequins: %v", err)
		}
		if len(f.invitations) != 2 {
			t.Errorf("invitations = %v", f.invitations)
		}
	})

	t.Run("rejects a bad header", func(t *testing.T) {
		f := &fakeClient{orgID: "ORG"}
		svc, _ := newService(f)

		err := svc.ReclaimMannequins(t.Context(), []string{"nope", "alice,m1,alice-t"}, "octo", false, false)
		if err == nil || !strings.Contains(err.Error(), "invalid CSV header") {
			t.Fatalf("expected header error, got %v", err)
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

		lines := csv(
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

		if err := svc.ReclaimMannequins(t.Context(), csv("alice,m1,alice-t"), "octo", true, false); err != nil {
			t.Fatalf("ReclaimMannequins: %v", err)
		}
		if len(f.invitations) != 1 {
			t.Errorf("invitations = %v", f.invitations)
		}
	})
}

func TestWriteCSV(t *testing.T) {
	mannequins := []Mannequin{
		{ID: "m1", Login: "alice"},
		{ID: "m2", Login: "bob", MappedUser: &Claimant{Login: "bob-target"}},
	}

	t.Run("excludes reclaimed by default", func(t *testing.T) {
		var sb strings.Builder
		if err := WriteCSV(&sb, mannequins, false); err != nil {
			t.Fatalf("WriteCSV: %v", err)
		}
		out := sb.String()
		if !strings.Contains(out, CSVHeader) {
			t.Errorf("missing header:\n%s", out)
		}
		if !strings.Contains(out, "alice,m1,") {
			t.Errorf("missing alice:\n%s", out)
		}
		if strings.Contains(out, "bob") {
			t.Errorf("should exclude reclaimed bob:\n%s", out)
		}
	})

	t.Run("includes reclaimed with the target login", func(t *testing.T) {
		var sb strings.Builder
		if err := WriteCSV(&sb, mannequins, true); err != nil {
			t.Fatalf("WriteCSV: %v", err)
		}
		if !strings.Contains(sb.String(), "bob,m2,bob-target") {
			t.Errorf("missing reclaimed bob row:\n%s", sb.String())
		}
	})
}

func TestIsSkipInvitationUnavailable(t *testing.T) {
	err := &GraphQLError{Messages: []string{"Field 'reattributeMannequinToUser' doesn't exist on type 'Mutation'"}}
	if !isSkipInvitationUnavailable(err) {
		t.Error("expected true for the missing-mutation error")
	}
	if isSkipInvitationUnavailable(&GraphQLError{Messages: []string{"other"}}) {
		t.Error("expected false for an unrelated error")
	}
}
