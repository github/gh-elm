package ghapi

import (
	"strings"
	"testing"
)

func TestToMannequinRecords(t *testing.T) {
	mannequins := []Mannequin{
		{ID: "m1", Login: "alice"},
		{ID: "m2", Login: "bob", MappedUser: &Claimant{Login: "bob-target"}},
	}

	t.Run("excludes reclaimed by default", func(t *testing.T) {
		got := ToMannequinRecords(mannequins, false)
		if len(got) != 1 || got[0] != (MannequinRecord{MannequinUser: "alice", MannequinID: "m1"}) {
			t.Errorf("records = %+v", got)
		}
	})

	t.Run("includes reclaimed with the target login", func(t *testing.T) {
		got := ToMannequinRecords(mannequins, true)
		if len(got) != 2 {
			t.Fatalf("records = %+v", got)
		}
		if got[1] != (MannequinRecord{MannequinUser: "bob", MannequinID: "m2", TargetUser: "bob-target"}) {
			t.Errorf("reclaimed record = %+v", got[1])
		}
	})
}

func TestWriteMannequinCSV(t *testing.T) {
	var sb strings.Builder
	records := []MannequinRecord{
		{MannequinUser: "alice", MannequinID: "m1"},
		{MannequinUser: "bob", MannequinID: "m2", TargetUser: "bob-target"},
	}
	if err := WriteMannequinCSV(&sb, records); err != nil {
		t.Fatalf("WriteMannequinCSV: %v", err)
	}
	out := sb.String()
	if !strings.Contains(out, "mannequin-user,mannequin-id,target-user") {
		t.Errorf("missing header:\n%s", out)
	}
	if !strings.Contains(out, "alice,m1,") {
		t.Errorf("missing alice row:\n%s", out)
	}
	if !strings.Contains(out, "bob,m2,bob-target") {
		t.Errorf("missing bob row:\n%s", out)
	}
}

func TestReadMannequinCSV(t *testing.T) {
	t.Run("parses data rows and trims whitespace", func(t *testing.T) {
		in := "mannequin-user,mannequin-id,target-user\nalice, m1 ,alice-t\nbob,m2,bob-t\n"
		got, err := ReadMannequinCSV(strings.NewReader(in))
		if err != nil {
			t.Fatalf("ReadMannequinCSV: %v", err)
		}
		want := []MannequinRecord{
			{MannequinUser: "alice", MannequinID: "m1", TargetUser: "alice-t"},
			{MannequinUser: "bob", MannequinID: "m2", TargetUser: "bob-t"},
		}
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			t.Errorf("records = %+v, want %+v", got, want)
		}
	})

	t.Run("handles CRLF and quoted fields", func(t *testing.T) {
		in := "mannequin-user,mannequin-id,target-user\r\n\"al, ice\",m1,\"target\"\r\n"
		got, err := ReadMannequinCSV(strings.NewReader(in))
		if err != nil {
			t.Fatalf("ReadMannequinCSV: %v", err)
		}
		if len(got) != 1 || got[0].MannequinUser != "al, ice" || got[0].TargetUser != "target" {
			t.Errorf("records = %+v", got)
		}
	})

	t.Run("rejects a bad header", func(t *testing.T) {
		in := "nope,mannequin-id,target-user\nalice,m1,alice-t\n"
		_, err := ReadMannequinCSV(strings.NewReader(in))
		if err == nil || !strings.Contains(err.Error(), "invalid CSV header") {
			t.Fatalf("expected header error, got %v", err)
		}
	})

	t.Run("rejects an inconsistent field count", func(t *testing.T) {
		in := "mannequin-user,mannequin-id,target-user\nalice,m1\n"
		_, err := ReadMannequinCSV(strings.NewReader(in))
		if err == nil {
			t.Fatal("expected an error for a row with the wrong number of fields")
		}
	})

	t.Run("empty input yields no records", func(t *testing.T) {
		got, err := ReadMannequinCSV(strings.NewReader(""))
		if err != nil {
			t.Fatalf("ReadMannequinCSV: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("records = %+v, want none", got)
		}
	})
}

func TestMannequinCSVRoundTrip(t *testing.T) {
	records := []MannequinRecord{
		{MannequinUser: "alice", MannequinID: "m1", TargetUser: "alice-t"},
		{MannequinUser: "bob", MannequinID: "m2", TargetUser: "bob-t"},
	}
	var sb strings.Builder
	if err := WriteMannequinCSV(&sb, records); err != nil {
		t.Fatalf("WriteMannequinCSV: %v", err)
	}
	got, err := ReadMannequinCSV(strings.NewReader(sb.String()))
	if err != nil {
		t.Fatalf("ReadMannequinCSV: %v", err)
	}
	if len(got) != len(records) || got[0] != records[0] || got[1] != records[1] {
		t.Errorf("round-trip = %+v, want %+v", got, records)
	}
}
