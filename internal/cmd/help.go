package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/github/gh-elm/internal/render"
	"github.com/github/gh-elm/internal/theme"
)

const directActionAnnotation = "gh-elm/direct-action"

func renderHelp(cmd *cobra.Command, _ []string) {
	var output strings.Builder
	out := &output
	heading := theme.New().Primary.Bold(true)

	writeDescription(out, cmd)
	fmt.Fprintf(out, "\n%s\n  %s\n", heading.Render("USAGE"), usageLine(cmd))

	if len(cmd.Aliases) > 0 {
		fmt.Fprintf(out, "\n%s\n  %s\n", heading.Render("ALIASES"), cmd.NameAndAliases())
	}
	if cmd.HasExample() {
		fmt.Fprintf(out, "\n%s\n%s\n", heading.Render("EXAMPLES"), cmd.Example)
	}
	if cmd.HasAvailableSubCommands() {
		writeCommands(out, heading, cmd)
	}
	if cmd.HasAvailableLocalFlags() {
		fmt.Fprintf(out, "\n%s\n%s", heading.Render("FLAGS"), cmd.NonInheritedFlags().FlagUsages())
	}
	if cmd.HasAvailableInheritedFlags() {
		fmt.Fprintf(out, "\n%s\n%s", heading.Render("INHERITED FLAGS"), cmd.InheritedFlags().FlagUsages())
	}
	if cmd.HasHelpSubCommands() {
		writeHelpTopics(out, heading, cmd)
	}
	if cmd.HasAvailableSubCommands() {
		fmt.Fprintf(out, "\n%s\n  Use `gh %s <command> --help` for more information about a command.\n",
			heading.Render("LEARN MORE"), cmd.CommandPath())
	}
	_, _ = io.WriteString(cmd.OutOrStdout(), render.Human(output.String()))
}

func usageLine(cmd *cobra.Command) string {
	if cmd.Parent() == nil {
		return "gh " + cmd.CommandPath() + " <command> <subcommand> [flags]"
	}
	if cmd.HasAvailableSubCommands() {
		if hasDirectAction(cmd) {
			return "gh " + cmd.UseLine() + "\n  gh " + cmd.CommandPath() + " <command> [flags]"
		}
		return "gh " + cmd.CommandPath() + " <command> [flags]"
	}
	return "gh " + cmd.UseLine()
}

func writeDescription(out io.Writer, cmd *cobra.Command) {
	description := cmd.Long
	if description == "" {
		description = cmd.Short
	}
	fmt.Fprintln(out, strings.TrimSpace(description))
}

func writeCommands(out io.Writer, heading lipgloss.Style, cmd *cobra.Command) {
	if cmd.Parent() == nil {
		writeRootCommands(out, heading, cmd)
		return
	}

	commands := cmd.Commands()
	width := 0
	for _, subcommand := range commands {
		if isListedCommand(subcommand) {
			width = max(width, len(subcommand.Name())+1)
		}
	}

	fmt.Fprintf(out, "\n%s\n", heading.Render("COMMANDS"))
	for _, subcommand := range commands {
		if isListedCommand(subcommand) {
			fmt.Fprintf(out, "  %-*s    %s\n", width, subcommand.Name()+":", subcommand.Short)
		}
	}
}

func writeRootCommands(out io.Writer, heading lipgloss.Style, cmd *cobra.Command) {
	var general []*cobra.Command
	var groups []*cobra.Command
	for _, subcommand := range cmd.Commands() {
		if !isListedCommand(subcommand) {
			continue
		}
		if subcommand.HasAvailableSubCommands() && subcommand.Name() != "completion" {
			groups = append(groups, subcommand)
		} else {
			general = append(general, subcommand)
		}
	}
	writeCommandSection(out, heading, "COMMANDS", general)

	for _, group := range groups {
		writeLeafCommandSection(out, heading, strings.ToUpper(group.Name())+" COMMANDS", group)
	}
}

func writeCommandSection(out io.Writer, heading lipgloss.Style, title string, commands []*cobra.Command) {
	width := 0
	for _, command := range commands {
		width = max(width, len(command.Name())+1)
	}

	fmt.Fprintf(out, "\n%s\n", heading.Render(title))
	for _, command := range commands {
		fmt.Fprintf(out, "  %-*s    %s\n", width, command.Name()+":", command.Short)
	}
}

func writeLeafCommandSection(out io.Writer, heading lipgloss.Style, title string, root *cobra.Command) {
	var commands []helpCommand
	if hasDirectAction(root) {
		commands = append(commands, helpCommand{name: root.Name(), description: root.Short})
	}
	commands = append(commands, leafCommands(root, root.Name())...)
	width := 0
	for _, command := range commands {
		width = max(width, len(command.name)+1)
	}

	fmt.Fprintf(out, "\n%s\n", heading.Render(title))
	for _, command := range commands {
		fmt.Fprintf(out, "  %-*s    %s\n", width, command.name+":", command.description)
	}
}

type helpCommand struct {
	name        string
	description string
}

func leafCommands(cmd *cobra.Command, prefix string) []helpCommand {
	var commands []helpCommand
	for _, child := range cmd.Commands() {
		if !child.IsAvailableCommand() {
			continue
		}

		name := prefix + " " + child.Name()
		if child.HasAvailableSubCommands() {
			if hasDirectAction(child) {
				commands = append(commands, helpCommand{name: name, description: child.Short})
			}
			commands = append(commands, leafCommands(child, name)...)
			continue
		}
		commands = append(commands, helpCommand{name: name, description: child.Short})
	}
	return commands
}

func hasDirectAction(cmd *cobra.Command) bool {
	return cmd.Annotations[directActionAnnotation] == "true"
}

func isListedCommand(cmd *cobra.Command) bool {
	return cmd.IsAvailableCommand() || cmd.Name() == "help"
}

func writeHelpTopics(out io.Writer, heading lipgloss.Style, cmd *cobra.Command) {
	topics := cmd.Commands()
	width := 0
	for _, topic := range topics {
		if topic.IsAdditionalHelpTopicCommand() {
			width = max(width, len(topic.CommandPath())+1)
		}
	}

	fmt.Fprintf(out, "\n%s\n", heading.Render("ADDITIONAL HELP TOPICS"))
	for _, topic := range topics {
		if topic.IsAdditionalHelpTopicCommand() {
			fmt.Fprintf(out, "  %-*s    %s\n", width, topic.CommandPath()+":", topic.Short)
		}
	}
}
