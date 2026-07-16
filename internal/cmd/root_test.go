package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestPingCommands(t *testing.T) {
	for _, args := range [][]string{
		{"migration", "ping"},
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
