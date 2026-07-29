package theme

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
)

// TestNew pins the styles to the gh CLI's own ColorScheme values.
func TestNew(t *testing.T) {
	s := New()

	t.Run("semantic colours come from the gh palette", func(t *testing.T) {
		assert.Equal(t, ColorGreen, s.Success.GetForeground())
		assert.Equal(t, ColorYellow, s.Active.GetForeground())
		assert.Equal(t, ColorYellow, s.Warning.GetForeground())
		assert.Equal(t, ColorYellow, s.Paused.GetForeground())
		assert.Equal(t, ColorRed, s.Failure.GetForeground())
		assert.Equal(t, ColorCyan, s.Info.GetForeground())
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
			"Info": s.Info, "Muted": s.Muted, "Success": s.Success,
			"Active": s.Active, "Warning": s.Warning, "Paused": s.Paused,
			"Failure": s.Failure,
		} {
			assert.IsType(t, lipgloss.Color(""), style.GetForeground(), name)
		}
	})
}

func TestForm(t *testing.T) {
	t.Run("drops the accent colours ThemeBase16 brings", func(t *testing.T) {
		// ThemeBase16 accents the focused button and the cursor with magenta,
		// which carries no meaning here.
		f := Form()
		assert.NotEqual(t, lipgloss.Color("5"), f.Focused.FocusedButton.GetBackground())
		assert.NotEqual(t, lipgloss.Color("5"), f.Focused.TextInput.Cursor.GetForeground())
	})

	t.Run("titles and descriptions match the shared styles", func(t *testing.T) {
		f := Form()
		assert.Equal(t, ColorCyan, f.Focused.Title.GetForeground())
		assert.Equal(t, ColorMuted, f.Focused.Description.GetForeground())
	})

	t.Run("ordinary content inherits the terminal foreground", func(t *testing.T) {
		f := Form()
		assert.Equal(t, lipgloss.NoColor{}, f.Focused.Option.GetForeground())
		assert.Equal(t, lipgloss.NoColor{}, f.Focused.UnselectedOption.GetForeground())
		assert.Equal(t, lipgloss.NoColor{}, f.Blurred.TextInput.Text.GetForeground())
	})

	t.Run("help uses the shared muted style", func(t *testing.T) {
		f := Form()
		for name, style := range map[string]lipgloss.Style{
			"ellipsis": f.Help.Ellipsis, "short key": f.Help.ShortKey,
			"short description": f.Help.ShortDesc, "short separator": f.Help.ShortSeparator,
			"full key": f.Help.FullKey, "full description": f.Help.FullDesc,
			"full separator": f.Help.FullSeparator,
		} {
			assert.Equal(t, ColorMuted, style.GetForeground(), name)
		}
	})
}
