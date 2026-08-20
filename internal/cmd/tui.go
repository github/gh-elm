package cmd

import (
	"errors"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	elmtui "github.com/github/gh-elm/internal/tui"
	"github.com/github/gh-elm/internal/workflow"
)

func runRoot(cmd *cobra.Command) error {
	if !isInteractiveTerminal(cmd) {
		return cmd.Help()
	}

	model := elmtui.New(cmd.Context(), workflow.New())
	program := tea.NewProgram(
		model,
		tea.WithContext(cmd.Context()),
		tea.WithAltScreen(),
		tea.WithInput(cmd.InOrStdin()),
		tea.WithOutput(cmd.OutOrStdout()),
	)
	if _, err := program.Run(); err != nil {
		if errors.Is(err, tea.ErrProgramKilled) && cmd.Context().Err() != nil {
			return cmd.Context().Err()
		}
		return fmt.Errorf("interactive display error: %w", err)
	}
	return nil
}

func isInteractiveTerminal(cmd *cobra.Command) bool {
	input, inputOK := cmd.InOrStdin().(*os.File)
	output, outputOK := cmd.OutOrStdout().(*os.File)
	if !inputOK || !outputOK {
		return false
	}
	//nolint:gosec // terminal file descriptors are small non-negative integers
	return term.IsTerminal(int(input.Fd())) && term.IsTerminal(int(output.Fd()))
}
