// Command gh-elm is a gh CLI extension for driving Enterprise Live Migrations
// (ELM) against the GitHub Enterprise Server REST API. It is invoked as
// `gh elm ...`.
package main

import (
	"fmt"
	"os"

	"github.com/github/gh-elm/internal/cmd"
)

// version is injected at release time via -ldflags "-X main.version=<tag>"
// (see .github/workflows/release.yml). It defaults to "dev" for local builds.
var version = "dev"

func main() {
	if err := cmd.NewRootCmd(version).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
