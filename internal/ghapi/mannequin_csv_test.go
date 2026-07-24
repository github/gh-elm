package ghapi

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToMannequinRecords(t *testing.T) {
	mannequins := []Mannequin{
		{ID: "m1", Login: "alice"},
		{ID: "m2", Login: "bob", MappedUser: &Claimant{Login: "bob-target"}},
	}

	t.Run("excludes reclaimed by default", func(t *testing.T) {
		got := ToMannequinRecords(mannequins, false)
		assert.Equal(t, []MannequinRecord{{MannequinUser: "alice", MannequinID: "m1"}}, got)
	})

	t.Run("includes reclaimed with the target login", func(t *testing.T) {
		got := ToMannequinRecords(mannequins, true)
		require.Len(t, got, 2)
		assert.Equal(t, MannequinRecord{MannequinUser: "bob", MannequinID: "m2", TargetUser: "bob-target"}, got[1])
	})
}

func TestWriteMannequinCSV(t *testing.T) {
	var sb strings.Builder
	records := []MannequinRecord{
		{MannequinUser: "alice", MannequinID: "m1"},
		{MannequinUser: "bob", MannequinID: "m2", TargetUser: "bob-target"},
	}
	require.NoError(t, WriteMannequinCSV(&sb, records), "WriteMannequinCSV")
	out := sb.String()
	assert.Contains(t, out, "mannequin-user,mannequin-id,target-user")
	assert.Contains(t, out, "alice,m1,")
	assert.Contains(t, out, "bob,m2,bob-target")
}

func TestReadMannequinCSV(t *testing.T) {
	t.Run("parses data rows and trims whitespace", func(t *testing.T) {
		in := "mannequin-user,mannequin-id,target-user\nalice, m1 ,alice-t\nbob,m2,bob-t\n"
		got, err := ReadMannequinCSV(strings.NewReader(in))
		require.NoError(t, err, "ReadMannequinCSV")
		assert.Equal(t, []MannequinRecord{
			{MannequinUser: "alice", MannequinID: "m1", TargetUser: "alice-t"},
			{MannequinUser: "bob", MannequinID: "m2", TargetUser: "bob-t"},
		}, got)
	})

	t.Run("handles CRLF and quoted fields", func(t *testing.T) {
		in := "mannequin-user,mannequin-id,target-user\r\n\"al, ice\",m1,\"target\"\r\n"
		got, err := ReadMannequinCSV(strings.NewReader(in))
		require.NoError(t, err, "ReadMannequinCSV")
		require.Len(t, got, 1)
		assert.Equal(t, "al, ice", got[0].MannequinUser)
		assert.Equal(t, "target", got[0].TargetUser)
	})

	t.Run("rejects a bad header", func(t *testing.T) {
		in := "nope,mannequin-id,target-user\nalice,m1,alice-t\n"
		_, err := ReadMannequinCSV(strings.NewReader(in))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid CSV header")
	})

	t.Run("rejects an inconsistent field count", func(t *testing.T) {
		in := "mannequin-user,mannequin-id,target-user\nalice,m1\n"
		_, err := ReadMannequinCSV(strings.NewReader(in))
		assert.Error(t, err, "expected an error for a row with the wrong number of fields")
	})

	t.Run("empty input yields no records", func(t *testing.T) {
		got, err := ReadMannequinCSV(strings.NewReader(""))
		require.NoError(t, err, "ReadMannequinCSV")
		assert.Empty(t, got)
	})
}

func TestMannequinCSVRoundTrip(t *testing.T) {
	records := []MannequinRecord{
		{MannequinUser: "alice", MannequinID: "m1", TargetUser: "alice-t"},
		{MannequinUser: "bob", MannequinID: "m2", TargetUser: "bob-t"},
	}
	var sb strings.Builder
	require.NoError(t, WriteMannequinCSV(&sb, records), "WriteMannequinCSV")
	got, err := ReadMannequinCSV(strings.NewReader(sb.String()))
	require.NoError(t, err, "ReadMannequinCSV")
	assert.Equal(t, records, got)
}
