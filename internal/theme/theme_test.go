package theme

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
)

// TestNew pins the styles to the gh CLI's own ColorScheme values.
func TestNew(t *testing.T) {
	s := New()

	t.Run("semantic colours come from the theme palette", func(t *testing.T) {
		assert.Equal(t, lipgloss.NoColor{}, s.Primary.GetForeground())
		assert.Equal(t, colorGreen, s.Success.GetForeground())
		assert.Equal(t, colorGreen, s.Active.GetForeground())
		assert.Equal(t, warningColor, s.Warning.GetForeground())
		assert.Equal(t, warningColor, s.Paused.GetForeground())
		assert.Equal(t, githubRed, s.Failure.GetForeground())
		assert.Equal(t, colorBlue, s.Info.GetForeground())
		assert.Equal(t, colorSecondary, s.Secondary.GetForeground())
		assert.Equal(t, colorDisabled, s.Disabled.GetForeground())
		assert.Equal(t, colorPlaceholder, s.Placeholder.GetForeground())
		assert.Equal(t, progressBarFill, s.ProgressBarFill.GetForeground())
		assert.Equal(t, progressBarTrack, s.ProgressBarTrack.GetForeground())
		assert.Equal(t, colorBlue, s.FocusedButton.GetBackground())
		assert.Equal(t, colorButtonText, s.FocusedButton.GetForeground())
		assert.Equal(t, buttonIdleBackground, s.BlurredButton.GetBackground())
		assert.Equal(t, buttonIdleForeground, s.BlurredButton.GetForeground())
	})

	t.Run("ANSI palette values are pinned", func(t *testing.T) {
		assert.Equal(t, colorRed, lipgloss.Color("1"))
		assert.Equal(t, colorGreen, lipgloss.Color("2"))
		assert.Equal(t, colorYellow, lipgloss.Color("3"))
		assert.Equal(t, colorBlue, lipgloss.Color("4"))
	})

	t.Run("GitHub palette values are pinned", func(t *testing.T) {
		assert.Equal(t, githubBlue, lipgloss.Color("#4493f8"))
		assert.Equal(t, githubGreen, lipgloss.Color("#3fb950"))
		assert.Equal(t, githubRed, lipgloss.Color("#d1242f"))
		assert.Equal(t, githubPurple, lipgloss.Color("#8250df"))
		assert.Equal(t, githubTerracotta, lipgloss.Color("#bc4c00"))
		assert.Equal(t, githubBrown, lipgloss.Color("#9a6700"))
		assert.Equal(t, githubStar, lipgloss.Color("#eac54f"))
	})

	t.Run("warnings adapt to terminal background", func(t *testing.T) {
		assert.Equal(t, string(githubBrown), warningColor.Light)
		assert.Equal(t, string(githubStar), warningColor.Dark)
	})

	t.Run("muted is gh's 242 grey", func(t *testing.T) {
		assert.Equal(t, lipgloss.Color("242"), s.Muted.GetForeground())
	})

	t.Run("disabled is a lighter grey than muted", func(t *testing.T) {
		assert.Equal(t, lipgloss.Color("244"), s.Disabled.GetForeground())
	})

	t.Run("emphasis carries no colour of its own", func(t *testing.T) {
		assert.True(t, s.Bold.GetBold())
		assert.Equal(t, lipgloss.NoColor{}, s.Bold.GetForeground())
	})

	t.Run("other styles use fixed colours", func(t *testing.T) {
		for name, style := range map[string]lipgloss.Style{
			"Info": s.Info, "Secondary": s.Secondary, "Muted": s.Muted, "Disabled": s.Disabled,
			"Placeholder": s.Placeholder, "Success": s.Success, "Active": s.Active,
			"Failure": s.Failure,
		} {
			assert.IsType(t, lipgloss.Color(""), style.GetForeground(), name)
		}
	})
}

func TestForm(t *testing.T) {
	f := Form()
	s := New()

	t.Run("drops the accent colours ThemeBase16 brings", func(t *testing.T) {
		assert.NotEqual(t, lipgloss.Color("5"), f.Focused.FocusedButton.GetBackground())
		assert.NotEqual(t, lipgloss.Color("5"), f.Focused.TextInput.Cursor.GetForeground())
	})

	t.Run("titles reflect their hierarchy", func(t *testing.T) {
		assert.Equal(t, colorBlue, f.Focused.Title.GetForeground())
		assert.True(t, f.Focused.Title.GetBold())
		assert.Equal(t, s.Primary.GetForeground(), f.Focused.NoteTitle.GetForeground())
		assert.True(t, f.Focused.NoteTitle.GetBold())
		assert.Equal(t, f.Focused.NoteTitle, f.Blurred.NoteTitle)
		assert.Equal(t, s.Secondary.GetForeground(), f.Blurred.Title.GetForeground())
	})

	t.Run("focused controls use the terminal ANSI blue", func(t *testing.T) {
		assert.Equal(t, colorBlue, f.Focused.SelectSelector.GetForeground())
		assert.Equal(t, colorBlue, f.Focused.MultiSelectSelector.GetForeground())
		assert.Equal(t, colorBlue, f.Focused.SelectedOption.GetForeground())
		assert.Equal(t, colorBlue, f.Focused.SelectedPrefix.GetForeground())
		assert.Equal(t, colorBlue, f.Focused.FocusedButton.GetBackground())
		assert.Equal(t, colorButtonText, f.Focused.FocusedButton.GetForeground())
		assert.Equal(t, buttonIdleBackground, f.Focused.BlurredButton.GetBackground())
		assert.Equal(t, buttonIdleForeground, f.Focused.BlurredButton.GetForeground())
		assert.Equal(t, colorBlue, f.Focused.TextInput.Cursor.GetForeground())
		assert.Equal(t, colorBlue, f.Focused.TextInput.Prompt.GetForeground())
	})

	t.Run("secondary text has a clear hierarchy", func(t *testing.T) {
		assert.Equal(t, s.Muted, f.Focused.Description)
		assert.Equal(t, s.Placeholder, f.Focused.TextInput.Placeholder)
	})

	t.Run("ordinary content inherits the terminal foreground", func(t *testing.T) {
		assert.Equal(t, s.Primary.GetForeground(), f.Focused.Base.GetBorderLeftForeground())
		assert.Equal(t, lipgloss.NoColor{}, f.Focused.Option.GetForeground())
		assert.Equal(t, lipgloss.NoColor{}, f.Focused.UnselectedOption.GetForeground())
		assert.Equal(t, lipgloss.NoColor{}, f.Focused.TextInput.Text.GetForeground())
		assert.Equal(t, lipgloss.NoColor{}, f.Blurred.TextInput.Text.GetForeground())
	})

	t.Run("help keys stand out from their descriptions", func(t *testing.T) {
		assert.Equal(t, s.Secondary, f.Help.ShortKey)
		assert.Equal(t, s.Secondary, f.Help.FullKey)
		assert.Equal(t, s.Muted, f.Help.ShortDesc)
		assert.Equal(t, s.Muted, f.Help.FullDesc)
		assert.Equal(t, s.Muted, f.Help.ShortSeparator)
		assert.Equal(t, s.Muted, f.Help.FullSeparator)
		assert.Equal(t, s.Muted, f.Help.Ellipsis)
	})
}
