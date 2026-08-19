//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

const integrationVersion = "integration-test"

var cliBinary string

type cliResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

func TestMain(m *testing.M) {
	buildDir, err := os.MkdirTemp("", "gh-elm-integration-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "creating build directory: %v\n", err)
		os.Exit(1)
	}

	binaryName := "gh-elm"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	cliBinary = filepath.Join(buildDir, binaryName)

	// Find the enclosing Go module without assuming where this file lives.
	repositoryRoot, err := findModuleRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "locating repository root: %v\n", err)
		_ = os.RemoveAll(buildDir)
		os.Exit(1)
	}

	// Avoid leaving CI stuck if the Go build stops making progress.
	buildCtx, cancelBuild := context.WithTimeout(context.Background(), 2*time.Minute)
	build := exec.CommandContext(
		buildCtx,
		"go",
		"build",
		"-trimpath",
		"-ldflags",
		"-X main.version="+integrationVersion,
		"-o",
		cliBinary,
		".",
	)
	build.Dir = repositoryRoot
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr

	buildErr := build.Run()
	cancelBuild()

	if buildErr != nil {
		if errors.Is(buildCtx.Err(), context.DeadlineExceeded) {
			fmt.Fprintln(os.Stderr, "building gh-elm integration binary timed out")
		} else {
			fmt.Fprintf(os.Stderr, "building gh-elm integration binary: %v\n", buildErr)
		}
		_ = os.RemoveAll(buildDir)
		os.Exit(1)
	}

	code := m.Run()

	if err := os.RemoveAll(buildDir); err != nil {
		fmt.Fprintf(os.Stderr, "removing integration build directory: %v\n", err)
		if code == 0 {
			code = 1
		}
	}

	os.Exit(code)
}

// findModuleRoot walks upward from this source file until it finds the nearest
// enclosing Go module.
func findModuleRoot() (string, error) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("could not determine integration test source path")
	}

	startDir := filepath.Dir(sourceFile)
	dir := startDir

	for {
		goModPath := filepath.Join(dir, "go.mod")
		info, err := os.Stat(goModPath)

		switch {
		case err == nil && !info.IsDir():
			return dir, nil
		case err == nil:
			return "", fmt.Errorf("%s is a directory, expected a file", goModPath)
		case !errors.Is(err, os.ErrNotExist):
			return "", fmt.Errorf("checking %s: %w", goModPath, err)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod found above %s", startDir)
		}
		dir = parent
	}
}

func runCLI(t *testing.T, env map[string]string, args ...string) cliResult {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, cliBinary, args...)
	cmd.Env = integrationEnvironment(t, env)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf(
			"gh-elm %v timed out\nstdout:\n%s\nstderr:\n%s",
			args,
			stdout.String(),
			stderr.String(),
		)
	}

	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf(
				"starting gh-elm %v: %v\nstdout:\n%s\nstderr:\n%s",
				args,
				err,
				stdout.String(),
				stderr.String(),
			)
		}
		exitCode = exitErr.ExitCode()
	}

	return cliResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}
}

func integrationEnvironment(t *testing.T, overrides map[string]string) []string {
	// Preserve settings such as PATH and HOME, but remove configuration that
	// could make tests depend on the developer or CI runner.
	remove := []string{
		"GH_SOURCE_HOST",
		"GH_SOURCE_TOKEN",
		"GH_TARGET_HOST",
		"GH_TARGET_TOKEN",
		"GH_ELM_CONFIG_DIR",
		"GH_ELM_CREDENTIAL_STORE",
		"GH_CONFIG_DIR",
	}

	values := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, found := strings.Cut(entry, "=")
		if !found || environmentKeyInList(key, remove) {
			continue
		}
		values[key] = value
	}

	// Each invocation gets isolated configuration unless the caller supplies a
	// shared directory for a multi-command test.
	values["GH_ELM_CONFIG_DIR"] = t.TempDir()
	values["GH_ELM_CREDENTIAL_STORE"] = "file"
	values["NO_COLOR"] = "1"

	for key, value := range overrides {
		values[key] = value
	}

	// Sorting is not required by os/exec, but keeps the environment deterministic.
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, key+"="+values[key])
	}
	return env
}

func environmentKeyInList(key string, list []string) bool {
	for _, candidate := range list {
		// Environment-variable names are case-insensitive on Windows.
		if strings.EqualFold(key, candidate) {
			return true
		}
	}
	return false
}
