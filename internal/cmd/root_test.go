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
	for _, group := range []string{"migration", "target"} {
		t.Run(group, func(t *testing.T) {
			root := NewRootCmd("test")
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&out)
			root.SetArgs([]string{group, "definitely-not-a-command"})

			err := root.Execute()
			if err == nil {
				t.Fatalf("expected an error for an unknown %s subcommand", group)
			}
			if !strings.Contains(err.Error(), "unknown command") {
				t.Errorf("expected an \"unknown command\" error, got %v", err)
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
