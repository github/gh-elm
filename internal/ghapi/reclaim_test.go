package ghapi

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeClient implements reclaimClient for service tests.
type fakeClient struct {
	orgID               string
	mannequins          []Mannequin
	byLogin             map[string][]Mannequin
	userIDs             map[string]string
	botIDs              map[string]string
	userIDErr           error    // when set, UserID returns this for every lookup
	invitations         []string // "sourceID->targetID"
	reattributions      []string
	botReattributions   []string
	inviteErr           error
	reattributeErr      error
	botReattributeErr   error
	reattributeAttempts int
	botAttempts         int
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
func (f *fakeClient) BotID(_ context.Context, login string) (string, error) {
	id, ok := f.botIDs[login]
	if !ok {
		return "", fmt.Errorf("bot %q not found: %w", login, ErrUserNotFound)
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
func (f *fakeClient) ReattributeMannequinToBot(_ context.Context, _, mannequinID, targetBotID string) (*AttributionResult, error) {
	f.botAttempts++
	if f.botReattributeErr != nil {
		return nil, f.botReattributeErr
	}
	f.botReattributions = append(f.botReattributions, mannequinID+"->"+targetBotID)
	return &AttributionResult{SourceID: mannequinID, TargetID: targetBotID}, nil
}

// capturingLogger records log lines for assertions.
type capturingLogger struct{ lines []string }

func (l *capturingLogger) Infof(format string, args ...any) {
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}
func (l *capturingLogger) Successf(format string, args ...any) {
	l.lines = append(l.lines, "SUCCESS: "+fmt.Sprintf(format, args...))
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
		svc, log := newService(f)

		require.NoError(t, svc.ReclaimMannequin(t.Context(), "alice", "", "alice-target", "octo", false, false), "ReclaimMannequin")
		assert.Equal(t, []string{"m1->u1"}, f.invitations)
		assert.True(t, log.contains("SUCCESS: Mannequin reclaim invitation email successfully sent"))
	})

	t.Run("errors when the login is not a mannequin", func(t *testing.T) {
		f := &fakeClient{orgID: "ORG", byLogin: map[string][]Mannequin{}}
		svc, _ := newService(f)

		err := svc.ReclaimMannequin(t.Context(), "ghost", "", "t", "octo", false, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not a mannequin")
	})

	t.Run("errors when already claimed without force", func(t *testing.T) {
		f := &fakeClient{
			orgID:   "ORG",
			byLogin: map[string][]Mannequin{"alice": {{ID: "m1", Login: "alice", MappedUser: &Claimant{Login: "someone"}}}},
			userIDs: map[string]string{"alice-target": "u1"},
		}
		svc, _ := newService(f)

		err := svc.ReclaimMannequin(t.Context(), "alice", "", "alice-target", "octo", false, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already mapped")
		assert.Empty(t, f.invitations, "should not have invited")
	})

	t.Run("uses reattribution for skip-invitation", func(t *testing.T) {
		f := &fakeClient{
			orgID:   "ORG",
			byLogin: map[string][]Mannequin{"alice": {{ID: "m1", Login: "alice"}}},
			userIDs: map[string]string{"alice-target": "u1"},
		}
		svc, log := newService(f)

		require.NoError(t, svc.ReclaimMannequin(t.Context(), "alice", "", "alice-target", "octo", false, true), "ReclaimMannequin")
		assert.Len(t, f.reattributions, 1)
		assert.Empty(t, f.invitations)
		assert.True(t, log.contains("SUCCESS: Successfully reclaimed content"))
	})

	t.Run("reclaims a bot target via reattributeMannequinToBot", func(t *testing.T) {
		f := &fakeClient{
			orgID:   "ORG",
			byLogin: map[string][]Mannequin{"alice": {{ID: "m1", Login: "alice"}}},
			botIDs:  map[string]string{"example-ci[bot]": "b1"},
		}
		svc, _ := newService(f)

		err := svc.ReclaimMannequin(t.Context(), "alice", "", "example-ci[bot]", "octo", false, false)
		require.NoError(t, err, "ReclaimMannequin")
		assert.Equal(t, []string{"m1->b1"}, f.botReattributions)
		assert.Empty(t, f.invitations, "should not have used the invitation path")
		assert.Zero(t, f.reattributeAttempts, "should not have used the user reattribution path")
	})

	t.Run("returns an error when the bot mutation fails", func(t *testing.T) {
		f := &fakeClient{
			orgID:             "ORG",
			byLogin:           map[string][]Mannequin{"alice": {{ID: "m1", Login: "alice"}}},
			botIDs:            map[string]string{"example-ci[bot]": "b1"},
			botReattributeErr: &GraphQLError{Messages: []string{"Target must be an admin of the organization"}},
		}
		svc, _ := newService(f)

		err := svc.ReclaimMannequin(t.Context(), "alice", "", "example-ci[bot]", "octo", false, false)
		require.Error(t, err)
		assert.Empty(t, f.botReattributions, "no successful reattribution should be recorded")
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

		require.NoError(t, svc.ReclaimMannequins(t.Context(), recs("alice,m1,alice-t", "bob,m2,bob-t"), "octo", false, false), "ReclaimMannequins")
		assert.Len(t, f.invitations, 2)
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
		require.NoError(t, svc.ReclaimMannequins(t.Context(), lines, "octo", false, false), "ReclaimMannequins")
		assert.Empty(t, f.invitations, "expected no reclaims")
		assert.True(t, log.contains("already claimed"), "missing already-claimed warning: %v", log.lines)
		assert.True(t, log.contains("not found"), "missing not-found warning: %v", log.lines)
		assert.True(t, log.contains("duplicate"), "missing duplicate warning: %v", log.lines)
	})

	t.Run("force reclaims an already-claimed row", func(t *testing.T) {
		f := &fakeClient{
			orgID:      "ORG",
			mannequins: []Mannequin{{ID: "m1", Login: "alice", MappedUser: &Claimant{Login: "old"}}},
			userIDs:    map[string]string{"alice-t": "u1"},
		}
		svc, _ := newService(f)

		require.NoError(t, svc.ReclaimMannequins(t.Context(), recs("alice,m1,alice-t"), "octo", true, false), "ReclaimMannequins")
		assert.Len(t, f.invitations, 1)
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

		require.NoError(t, svc.ReclaimMannequins(t.Context(), recs("alice,m1,alice-t", "bob,m2,bob-t"), "octo", false, false), "ReclaimMannequins")
		// alice-t is skipped; bob-t still reclaimed.
		assert.Equal(t, []string{"m2->u2"}, f.invitations)
		assert.True(t, log.contains("Claimant \"alice-t\" not found"), "missing not-found warning: %v", log.lines)
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
		require.True(t, errors.Is(err, authErr), "expected the auth error to propagate, got %v", err)
		assert.Empty(t, f.invitations, "should not have reclaimed on a hard error")
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
		// Must stop after the first row rather than treating it as a soft skip and
		// continuing through the batch, and surface the failure to the caller.
		require.Error(t, err)
		assert.Equal(t, 1, f.reattributeAttempts, "reattributeAttempts (fail-fast)")
		assert.True(t, log.contains("not enabled"), "missing unavailability warning: %v", log.lines)
	})
}

func TestBotReclaimAdvisory(t *testing.T) {
	t.Run("classifies bot and user targets and flags mis-targets", func(t *testing.T) {
		botCount, userCount, mistargets := BotReclaimAdvisory([]MannequinRecord{
			{MannequinUser: "legacy-ci[bot]", TargetUser: "example-ci[bot]"},
			{MannequinUser: "human", TargetUser: "app[bot]"},
			{MannequinUser: "alice", TargetUser: "alice-t"},
		})
		assert.Equal(t, 2, botCount)
		assert.Equal(t, 1, userCount)
		assert.Equal(t, []string{"human"}, mistargets)
	})

	t.Run("trims the target and matches casing", func(t *testing.T) {
		botCount, userCount, mistargets := BotReclaimAdvisory([]MannequinRecord{
			{MannequinUser: "human", TargetUser: "app[BOT] "},
		})
		assert.Equal(t, 1, botCount)
		assert.Zero(t, userCount)
		assert.Equal(t, []string{"human"}, mistargets)
	})

	t.Run("deduplicates mis-target source logins", func(t *testing.T) {
		_, _, mistargets := BotReclaimAdvisory([]MannequinRecord{
			{MannequinUser: "human", TargetUser: "a[bot]"},
			{MannequinUser: "human", TargetUser: "b[bot]"},
		})
		assert.Equal(t, []string{"human"}, mistargets)
	})
}

func TestIsSkipInvitationUnavailable(t *testing.T) {
	t.Run("missing mutation", func(t *testing.T) {
		err := &GraphQLError{Messages: []string{"Field 'reattributeMannequinToUser' doesn't exist on type 'Mutation'"}}
		assert.True(t, isSkipInvitationUnavailable(err))
	})

	t.Run("non-EMU organization", func(t *testing.T) {
		err := &GraphQLError{Messages: []string{"acme is not an Enterprise Managed Users (EMU) organization"}}
		assert.True(t, isSkipInvitationUnavailable(err))
	})

	t.Run("unrelated error", func(t *testing.T) {
		assert.False(t, isSkipInvitationUnavailable(&GraphQLError{Messages: []string{"other"}}))
	})
}
