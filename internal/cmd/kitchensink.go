package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/github/gh-elm/internal/theme"
)

func newKitchenSinkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "kitchensink",
		Short: "Preview gh elm styles and interactive controls",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := writeKitchenSink(cmd.OutOrStdout()); err != nil {
				return err
			}

			input, ok := cmd.InOrStdin().(*os.File)
			//nolint:gosec // os.File.Fd() returns a small, non-negative descriptor that fits in an int
			if !ok || !term.IsTerminal(int(input.Fd())) {
				return errors.New("gh elm kitchensink needs an interactive terminal to preview form controls")
			}

			return runForm(newKitchenSinkForm().
				WithInput(cmd.InOrStdin()).
				WithOutput(cmd.ErrOrStderr()))
		},
	}

	return cmd
}

func writeKitchenSink(out io.Writer) error {
	s := theme.New()

	lines := []string{
		s.Bold.Render("TYPOGRAPHY"),
		"  " + s.Primary.Render("Primary text — normal content and values"),
		"  " + s.Bold.Render("Bold text — identifiers and headings"),
		"  " + s.Info.Render("Info text — labels and structural accents"),
		"  " + s.Secondary.Render("Secondary text — supporting content"),
		"  " + s.Muted.Render("Muted text — timestamps, hints, and pending items"),
		"  " + s.Placeholder.Render("Placeholder text — example input"),
		"",
		s.Bold.Render("SEMANTIC COLORS"),
		"  " + s.Info.Render("● Info — structural information"),
		"  " + s.Active.Render("◉ Active — work in progress or current focus"),
		"  " + s.Success.Render("✓ Success — completed or passing"),
		"  " + s.Warning.Render("⚠ Warning — attention needed"),
		"  " + s.Paused.Render("⏸ Paused — work deliberately halted"),
		"  " + s.Failure.Render("✗ Error — failed or blocking"),
		"",
		s.Bold.Render("OUTPUT EXAMPLES"),
		"  " + s.Info.Render("Migration") + "  " + s.Bold.Render("42") + "  octo/source → octo/target",
		"  " + s.Success.Render("✓") + " Repository migration completed",
		"  " + s.Active.Render("◉") + " Importing repository data",
		"  " + s.Muted.Render("○ Waiting for cutover"),
		"  " + s.Warning.Render("⚠ Migration is taking longer than expected"),
		"  " + s.Failure.Render("✗ Failed to refresh migration status"),
		"",
		s.Bold.Render("INTERACTIVE CONTROLS"),
		"  Use Tab/Shift-Tab or arrow keys to move through the real themed controls below.",
		"  Submit the empty first input to preview validation error styling.",
		"",
	}

	for _, line := range lines {
		if _, err := fmt.Fprintln(out, line); err != nil {
			return err
		}
	}
	return nil
}

func newKitchenSinkForm() *huh.Form {
	var (
		name        string
		environment = "Production"
		features    = []string{"Repositories"}
		notes       string
		confirmed   bool
	)

	return huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Interactive component gallery").
				Description("These are the same components and theme used by gh elm configure."),
			huh.NewInput().
				Title("Migration name").
				Description("Press Enter while empty to preview the error state.").
				Placeholder("octo-enterprise-migration").
				Value(&name).
				Validate(validateRequired("a migration name")),
			huh.NewSelect[string]().
				Title("Target environment").
				Description("Compare focused, selected, and inactive options.").
				Options(huh.NewOptions("Production", "Staging", "Development")...).
				Value(&environment),
			huh.NewMultiSelect[string]().
				Title("Migration features").
				Description("Space toggles an option; focus and selection use separate indicators.").
				Options(
					huh.NewOption("Repositories", "Repositories").Selected(true),
					huh.NewOption("Issues", "Issues"),
					huh.NewOption("Pull requests", "Pull requests"),
				).
				Value(&features),
			huh.NewText().
				Title("Notes").
				Description("A multiline field with placeholder text.").
				Placeholder("Add optional migration context...").
				Value(&notes),
			huh.NewConfirm().
				Title("Does the theme look correct?").
				Description("Compare focused and unfocused button treatments.").
				Affirmative("Looks good").
				Negative("Needs work").
				Value(&confirmed),
		),
	).WithTheme(theme.Form())
}
