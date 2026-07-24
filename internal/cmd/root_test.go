package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
			require.Error(t, err, "expected an error")
			assert.Contains(t, err.Error(), "unknown command")
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
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown flag")
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
			require.Error(t, err)
			assert.Contains(t, err.Error(), "unknown flag")
		})
	}
}

func TestRootVersion(t *testing.T) {
	root := NewRootCmd("1.2.3")

	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--version"})

	require.NoError(t, root.Execute(), "--version returned error")
	assert.Contains(t, out.String(), "gh elm 1.2.3")
}
