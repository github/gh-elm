package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestPingCommands(t *testing.T) {
	for _, args := range [][]string{
		{"target", "ping"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			root := NewRootCmd("test")

			var out bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&out)
			root.SetArgs(args)

			if err := root.Execute(); err != nil {
				t.Fatalf("%v returned error: %v", args, err)
			}

			if got := strings.TrimSpace(out.String()); got != "pong" {
				t.Errorf("expected %q, got %q", "pong", got)
			}
		})
	}
}

func TestUnknownSubcommandFails(t *testing.T) {
	// A mistyped subcommand must report "unknown command". Bare typos are caught
	// by cobra's NoArgs; a typo *followed by a flag* exercises the shared
	// FlagErrorFunc (without it, cobra reports "unknown flag" first — the
	// original bug: `target repot url --migration-id 5`). Every group is covered
	// with a flag-bearing case — including a root-level typo — so each changed
	// group definition is actually guarded.
	cases := [][]string{
		// Bare typos (NoArgs path).
		{"migration", "definitely-not-a-command"},
		{"target", "definitely-not-a-command"},

		// Typos followed by a leaf-style flag (FlagErrorFunc path).
		{"bogus", "--migration-id", "5"},                         // root
		{"migration", "creat", "--migration-id", "5"},            // migration group
		{"target", "repot", "url", "--migration-id", "5"},        // target group
		{"target", "report", "bogus", "--migration-id", "5"},     // report group
		{"target", "mannequin", "bogus", "--github-org", "acme"}, // mannequin group
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			root := NewRootCmd("test")
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&out)
			root.SetArgs(args)

			err := root.Execute()
			if err == nil {
				t.Fatalf("expected an error for %v", args)
			}
			if !strings.Contains(err.Error(), "unknown command") {
				t.Errorf("expected an \"unknown command\" error, got %v", err)
			}
		})
	}
}

// TestUnknownFlagOnLeafStillFails guards the scoping of the unknown-flag
// handling: a genuine unknown flag on a leaf command must still be rejected
// rather than silently ignored.
func TestUnknownFlagOnLeafStillFails(t *testing.T) {
	root := NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"target", "report", "create", "--bogus", "--migration-id", "5", "--stage", "backfill"})

	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("expected an \"unknown flag\" error on a leaf command, got %v", err)
	}
}

// TestUnknownFlagWithoutSubcommandFails ensures a bad flag with no mistyped
// subcommand still errors (rather than being swallowed and exiting 0), since
// there is no positional for NoArgs to reject.
func TestUnknownFlagWithoutSubcommandFails(t *testing.T) {
	for _, args := range [][]string{
		{"--bogus"},
		{"target", "--bogus"},
		{"migration", "--bogus"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			root := NewRootCmd("test")
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&out)
			root.SetArgs(args)

			err := root.Execute()
			if err == nil || !strings.Contains(err.Error(), "unknown flag") {
				t.Fatalf("expected an \"unknown flag\" error for %v, got %v", args, err)
			}
		})
	}
}

func TestRootVersion(t *testing.T) {
	root := NewRootCmd("1.2.3")

	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--version"})

	if err := root.Execute(); err != nil {
		t.Fatalf("--version returned error: %v", err)
	}

	if got := out.String(); !strings.Contains(got, "gh elm 1.2.3") {
		t.Errorf("expected version output to contain %q; got:\n%s", "gh elm 1.2.3", got)
	}
}
