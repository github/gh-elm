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
		assert.Equal(t, githubGreen, s.Success.GetForeground())
		assert.Equal(t, colorYellow, s.Active.GetForeground())
		assert.Equal(t, colorYellow, s.Warning.GetForeground())
		assert.Equal(t, colorYellow, s.Paused.GetForeground())
		assert.Equal(t, githubRed, s.Failure.GetForeground())
		assert.Equal(t, githubBlue, s.Info.GetForeground())
		assert.Equal(t, colorSecondary, s.Secondary.GetForeground())
		assert.Equal(t, colorPlaceholder, s.Placeholder.GetForeground())
	})

	t.Run("GitHub palette values are pinned", func(t *testing.T) {
		assert.Equal(t, githubBlue, lipgloss.Color("#0969da"))
		assert.Equal(t, githubGreen, lipgloss.Color("#1a7f37"))
		assert.Equal(t, githubRed, lipgloss.Color("#d1242f"))
		assert.Equal(t, githubPurple, lipgloss.Color("#8250df"))
		assert.Equal(t, githubTerracotta, lipgloss.Color("#bc4c00"))
		assert.Equal(t, githubBrown, lipgloss.Color("#9a6700"))
		assert.Equal(t, githubStar, lipgloss.Color("#eac54f"))
	})

	t.Run("muted is gh's 242 grey", func(t *testing.T) {
		assert.Equal(t, lipgloss.Color("242"), s.Muted.GetForeground())
	})

	t.Run("emphasis carries no colour of its own", func(t *testing.T) {
		assert.True(t, s.Bold.GetBold())
		assert.Equal(t, lipgloss.NoColor{}, s.Bold.GetForeground())
	})

	t.Run("no style uses an adaptive colour", func(t *testing.T) {
		// The palette is deliberately flat: terminal palette indices already
		// follow the user's own light or dark theme, and 242 stays legible on
		// either, so adapting on top would fight the terminal.
		for name, style := range map[string]lipgloss.Style{
			"Info": s.Info, "Secondary": s.Secondary, "Muted": s.Muted,
			"Placeholder": s.Placeholder, "Success": s.Success, "Active": s.Active,
			"Warning": s.Warning, "Paused": s.Paused, "Failure": s.Failure,
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
		assert.Equal(t, githubBlue, f.Focused.Title.GetForeground())
		assert.True(t, f.Focused.Title.GetBold())
		assert.Equal(t, s.Primary.GetForeground(), f.Focused.NoteTitle.GetForeground())
		assert.True(t, f.Focused.NoteTitle.GetBold())
		assert.Equal(t, f.Focused.NoteTitle, f.Blurred.NoteTitle)
		assert.Equal(t, s.Secondary.GetForeground(), f.Blurred.Title.GetForeground())
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
