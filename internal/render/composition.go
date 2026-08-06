package render

import (
	"fmt"
	"strings"

	"github.com/github/gh-elm/internal/theme"
)

const fieldLabelWidth = 20

func renderSection(title string, lines ...string) string {
	if len(lines) == 0 {
		return ""
	}

	styles := theme.New()
	var output strings.Builder
	output.WriteString(styles.Bold.Render(title))
	_ = output.WriteByte('\n')
	for _, line := range lines {
		if line == "" {
			continue
		}
		output.WriteString(line)
		if !strings.HasSuffix(line, "\n") {
			_ = output.WriteByte('\n')
		}
	}
	return output.String()
}

func field(label, value string) string {
	styles := theme.New()
	paddedLabel := fmt.Sprintf("%-*s", fieldLabelWidth, label)
	return "  " + styles.Muted.Render(paddedLabel) + value
}

func detail(value string) string {
	return "  " + theme.New().Muted.Render(value)
}

func bullet(glyph, value string) string {
	return fmt.Sprintf("  %s %s", glyph, value)
}

func joinSections(sections ...string) string {
	var nonEmpty []string
	for _, section := range sections {
		if section != "" {
			nonEmpty = append(nonEmpty, strings.TrimRight(section, "\n"))
		}
	}
	if len(nonEmpty) == 0 {
		return ""
	}
	return strings.Join(nonEmpty, "\n\n") + "\n"
}
