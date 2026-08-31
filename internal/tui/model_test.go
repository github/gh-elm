package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-elm/internal/elmapi"
	"github.com/github/gh-elm/internal/ghapi"
	"github.com/github/gh-elm/internal/workflow"
)

func TestModelUpdate(t *testing.T) {
	t.Run("home disables configuration-dependent actions until preflight passes", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})

		actions := model.homeActionItems()
		for _, action := range actions {
			if action.id == "configuration" || action.id == "quit" {
				assert.False(t, action.disabled)
			} else {
				assert.True(t, action.disabled)
			}
		}
		assert.NotContains(t, model.View(), "(disabled)")
		assert.Contains(t, model.View(), model.styles.Disabled.Render("Migrations"))
		assert.Equal(t, 3, model.cursor)

		model.cursor = 0
		updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		model = updated.(*Model)
		assert.Nil(t, command)
		assert.Equal(t, screenHome, model.screen)

		model.cursor = 0
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
		model = updated.(*Model)
		assert.Equal(t, 3, model.cursor)
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
		model = updated.(*Model)
		assert.Equal(t, 5, model.cursor)
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
		model = updated.(*Model)
		assert.Equal(t, 3, model.cursor)

		setConfigurationReady(model)
		assert.Zero(t, model.cursor)
		for _, action := range model.homeActionItems() {
			assert.False(t, action.disabled)
		}
	})

	t.Run("opening migrations fetches a fresh list with every status", func(t *testing.T) {
		service := &fakeService{
			listSourceMigrations: func(_ context.Context, status string) ([]elmapi.MigrationSummary, error) {
				assert.Equal(t, elmapi.StatusAll, status)
				return []elmapi.MigrationSummary{{MigrationID: "fresh"}}, nil
			},
		}
		model := New(t.Context(), service)
		setConfigurationReady(model)
		model.sourceMigrations = []elmapi.MigrationSummary{{MigrationID: "stale"}}

		updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		model = updated.(*Model)

		assert.Equal(t, screenSourceList, model.screen)
		assert.True(t, model.loading)
		require.NotNil(t, command)

		updated, _ = model.Update(command())
		model = updated.(*Model)

		assert.False(t, model.loading)
		require.Len(t, model.sourceMigrations, 1)
		assert.Equal(t, "fresh", model.sourceMigrations[0].MigrationID)
	})

	t.Run("runtime configuration loss returns home through an alert", func(t *testing.T) {
		service := &fakeService{
			getConfiguration: func(context.Context) (*workflow.Configuration, error) {
				return &workflow.Configuration{}, nil
			},
		}
		model := New(t.Context(), service)
		setConfigurationReady(model)
		model.screen = screenSourceList
		model.loading = true
		model.width = 100
		model.height = 40

		updated, _ := model.Update(sourceListMsg{
			generation: model.sourceListGen,
			err:        fmt.Errorf("loading migrations: %w", workflow.ErrSourceConfigurationMissing),
		})
		model = updated.(*Model)

		assert.Equal(t, screenAlert, model.screen)
		assert.Equal(t, screenSourceList, model.alert.parent)
		assert.Contains(t, model.View(), "Configuration unavailable")
		assert.Contains(t, model.View(), "source URL or token is no longer configured")
		assert.Contains(t, model.View(), "Close")
		assert.NotContains(t, model.View(), "Error:")

		updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		model = updated.(*Model)

		assert.Equal(t, screenHome, model.screen)
		assert.Equal(t, 3, model.cursor)
		assert.Nil(t, model.configuration)
		require.NotNil(t, command)
		for _, action := range model.homeActionItems() {
			if action.id == "configuration" || action.id == "quit" {
				assert.False(t, action.disabled)
			} else {
				assert.True(t, action.disabled)
			}
		}
	})

	t.Run("ignores stale source migration responses", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		model.sourceListGen = 2
		model.sourceMigrations = []elmapi.MigrationSummary{{MigrationID: "current"}}

		updated, _ := model.Update(sourceListMsg{
			migrations: []elmapi.MigrationSummary{{MigrationID: "stale"}},
			generation: 1,
		})
		model = updated.(*Model)

		require.Len(t, model.sourceMigrations, 1)
		assert.Equal(t, "current", model.sourceMigrations[0].MigrationID)
	})

	t.Run("ignores stale configuration and authentication responses", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		model.screen = screenConfiguration
		model.loading = true
		model.configGeneration = 2
		model.configuration = &workflow.Configuration{SourceURL: "https://current.example"}

		updated, _ := model.Update(configMsg{
			configuration: &workflow.Configuration{SourceURL: "https://stale.example"},
			generation:    1,
		})
		model = updated.(*Model)
		assert.True(t, model.loading)
		assert.Equal(t, "https://current.example", model.configuration.SourceURL)

		updated, _ = model.Update(sourceAuthenticationMsg{generation: 1, err: assert.AnError})
		model = updated.(*Model)
		assert.False(t, model.sourceAuthChecked)
		assert.NoError(t, model.sourceAuthErr)
	})

	t.Run("filters migrations with search input", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		model.screen = screenSourceList
		model.sourceMigrations = []elmapi.MigrationSummary{
			{MigrationID: "source-1", SourceOrganizationLogin: "octo", SourceRepositoryName: "api"},
			{MigrationID: "source-2", SourceOrganizationLogin: "acme", SourceRepositoryName: "web"},
		}

		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
		model = updated.(*Model)
		for _, character := range "acme" {
			updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{character}})
			model = updated.(*Model)
		}

		require.Len(t, model.visibleSourceMigrations(), 1)
		assert.Equal(t, "source-2", model.visibleSourceMigrations()[0].MigrationID)
	})

	t.Run("search accepts navigation aliases as query text", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		model.screen = screenSourceList
		model.sourceSearch = true
		model.searchInput.Focus()

		for _, character := range "hjkl" {
			updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{character}})
			model = updated.(*Model)
		}
		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		model = updated.(*Model)

		assert.Equal(t, "hjk", model.searchInput.Value())
	})

	t.Run("shows incomplete configuration warning below the persistent header", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		updated, _ := model.Update(configMsg{configuration: &workflow.Configuration{
			SourceURL:      "https://source.example",
			SourceTokenSet: true,
		}})
		model = updated.(*Model)

		view := model.View()
		warningIndex := strings.Index(view, "Configuration not ready")
		titleIndex := strings.Index(view, "GitHub Enterprise")
		require.NotEqual(t, -1, warningIndex)
		require.NotEqual(t, -1, titleIndex)
		assert.Less(t, titleIndex, warningIndex)
		assert.Contains(t, view, "destination URL, destination token")
	})

	t.Run("cancels a destination migration load and returns home", func(t *testing.T) {
		started := make(chan struct{})
		service := &fakeService{
			listTargetMigrations: func(ctx context.Context, _ string, _ int) ([]elmapi.TargetMigration, error) {
				close(started)
				<-ctx.Done()
				return nil, ctx.Err()
			},
		}
		model := New(t.Context(), service)
		setConfigurationReady(model)
		model.cursor = 4

		updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		model = updated.(*Model)
		require.NotNil(t, command)
		require.Equal(t, screenTargetList, model.screen)
		require.True(t, model.loading)

		response := make(chan tea.Msg)
		go func() {
			response <- command()
		}()
		<-started

		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEscape})
		model = updated.(*Model)
		assert.Equal(t, screenHome, model.screen)
		assert.False(t, model.loading)

		updated, _ = model.Update(<-response)
		model = updated.(*Model)
		assert.Equal(t, screenHome, model.screen)
		assert.NoError(t, model.err)
	})

	t.Run("hides authentication rows until prerequisites are configured", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		model.configuration = &workflow.Configuration{
			SourceURL: "https://source.example",
			TargetURL: "https://target.example",
		}

		view := model.configurationView()

		assert.NotContains(t, view, "Source auth")
		assert.NotContains(t, view, "Destination auth")
		assert.NotContains(t, view, "Preflight")
		assert.NotContains(t, view, "Stored configuration")
		assert.Contains(t, view, "Source URL:         https://source.example")
		assert.Contains(t, view, "Source token:       not set")
		assert.Contains(t, view, "Destination URL:    https://target.example")
		assert.Contains(t, view, "Destination token:  not set")
	})

	t.Run("shows authentication rows when prerequisites are configured", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		model.configuration = &workflow.Configuration{
			SourceURL:      "https://source.example",
			SourceTokenSet: true,
			TargetURL:      "https://target.example",
			TargetTokenSet: true,
		}
		model.sourceAuthChecked = true
		model.targetAuthChecked = true

		view := model.configurationView()

		assert.Contains(t, view, "Source auth")
		assert.Contains(t, view, "Destination auth")
		assert.Equal(t, 2, strings.Count(view, "successful"))
	})

	t.Run("hides warning when configuration is ready", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		updated, _ := model.Update(configMsg{configuration: &workflow.Configuration{
			SourceURL:      "https://source.example",
			SourceTokenSet: true,
			TargetURL:      "https://target.example",
			TargetTokenSet: true,
		}})
		model = updated.(*Model)

		assert.NotContains(t, model.View(), "Configuration not ready")
	})

	t.Run("shows warning when authentication fails", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		updated, _ := model.Update(configMsg{configuration: &workflow.Configuration{
			SourceURL:      "https://source.example",
			SourceTokenSet: true,
			TargetURL:      "https://target.example",
			TargetTokenSet: true,
		}})
		model = updated.(*Model)
		updated, _ = model.Update(sourceAuthenticationMsg{err: assert.AnError})
		model = updated.(*Model)

		assert.Contains(t, model.View(), "Failed source authentication")
	})

	t.Run("hides warning throughout the configuration flow", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		model.screen = screenConfiguration
		model.configuration = &workflow.Configuration{
			SourceURL:      "https://source.example",
			SourceTokenSet: true,
		}
		model.sourceAuthChecked = true
		model.sourceAuthErr = assert.AnError

		view := model.View()

		assert.NotContains(t, view, "Configuration not ready")
		assert.NotContains(t, view, "Open Configuration to finish setup")
		assert.Contains(t, view, "Source auth")

		model.screen = screenForm
		model.form.parent = screenConfiguration
		assert.Empty(t, model.configurationWarning())

		model.screen = screenConfirm
		model.confirm.parent = screenConfiguration
		assert.Empty(t, model.configurationWarning())

		model.screen = screenResult
		model.result.parent = screenConfiguration
		assert.Empty(t, model.configurationWarning())
	})

	t.Run("failed source detail load clears previous migration state", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		model.screen = screenSourceDetail
		model.sourceID = "new-source"
		model.targetID = 42
		setSourceStatus(model, elmapi.StatusCreated)

		updated, _ := model.Update(sourceDetailMsg{err: assert.AnError})
		model = updated.(*Model)

		assert.Nil(t, model.sourceDetail)
		assert.Zero(t, model.targetID)
		assert.NotContains(t, actionIDs(model.sourceActionItems()), "start")
		assert.NotContains(t, actionIDs(model.sourceActionItems()), "cancel")
	})

	t.Run("target detail load clears previous repository", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		model.repository = "old/repository"
		model.targetDetail = &elmapi.TargetMigration{Repositories: []string{"old/repository"}}

		updated, _ := model.Update(targetDetailMsg{migration: &elmapi.TargetMigration{}})
		model = updated.(*Model)
		assert.Empty(t, model.repository)

		model.repository = "another/old-repository"
		updated, _ = model.Update(targetDetailMsg{err: assert.AnError})
		model = updated.(*Model)
		assert.Nil(t, model.targetDetail)
		assert.Empty(t, model.repository)
	})

	t.Run("configuration save reloads source migrations", func(t *testing.T) {
		svc := &fakeService{
			saveConfiguration: func(context.Context, workflow.ConfigurationInput) error {
				return nil
			},
			getConfiguration: func(context.Context) (*workflow.Configuration, error) {
				return &workflow.Configuration{}, nil
			},
			listSourceMigrations: func(context.Context, string) ([]elmapi.MigrationSummary, error) {
				return []elmapi.MigrationSummary{{MigrationID: "new-source"}}, nil
			},
		}
		model := New(t.Context(), svc)
		model.screen = screenConfiguration
		model.sourceMigrations = []elmapi.MigrationSummary{{MigrationID: "old-source"}}
		updated, _ := model.openConfigurationForm()
		model = updated.(*Model)
		model.form.cursor = len(model.form.fields)

		updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		model = updated.(*Model)
		require.NotNil(t, command)
		updated, command = model.Update(command())
		model = updated.(*Model)

		assert.Equal(t, screenConfiguration, model.screen)
		require.NotNil(t, command)
		assert.Empty(t, model.sourceMigrations)

		batch, ok := command().(tea.BatchMsg)
		require.True(t, ok)
		for _, batchCommand := range batch {
			updated, _ = model.Update(batchCommand())
			model = updated.(*Model)
		}

		require.Len(t, model.sourceMigrations, 1)
		assert.Equal(t, "new-source", model.sourceMigrations[0].MigrationID)
		assert.Equal(t, screenConfiguration, model.screen)
	})

	t.Run("configuration form offers save and cancel buttons", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		model.screen = screenConfiguration
		model.configuration = &workflow.Configuration{
			SourceTokenSet: true,
			TargetURL:      "https://api.staffship-01.ghe.com",
			TargetTokenSet: true,
		}
		updated, _ := model.openConfigurationForm()
		model = updated.(*Model)

		view := model.formView()
		assert.NotContains(t, view, "blank preserves current")
		assert.Equal(t, 2, strings.Count(view, "••••••••"))
		assert.Empty(t, *model.form.fields[1].text)
		assert.Equal(t, "https://api.staffship-01.ghe.com", *model.form.fields[2].text)
		assert.Empty(t, *model.form.fields[3].text)
		assert.Contains(t, view, "Save")
		assert.Contains(t, view, "Cancel")
		assert.Equal(t, -1, model.focusedFormAction())

		model.form.cursor = len(model.form.fields) - 1
		updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		model = updated.(*Model)
		assert.Nil(t, command)
		assert.Equal(t, len(model.form.fields), model.form.cursor)
		assert.Zero(t, model.focusedFormAction())

		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
		model = updated.(*Model)
		updated, command = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		model = updated.(*Model)

		assert.Nil(t, command)
		assert.Equal(t, screenConfiguration, model.screen)
	})

	t.Run("configuration reset invalidates source migrations", func(t *testing.T) {
		svc := &fakeService{
			resetConfiguration: func(context.Context) error {
				return nil
			},
		}
		model := New(t.Context(), svc)
		model.screen = screenConfiguration
		model.actionFocus = len(configurationActions) - 1
		model.sourceMigrations = []elmapi.MigrationSummary{{MigrationID: "old-source"}}

		updated, _ := model.activateConfigurationAction()
		model = updated.(*Model)
		updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
		model = updated.(*Model)
		require.NotNil(t, command)
		updated, _ = model.Update(command())
		model = updated.(*Model)
		assert.Equal(t, screenResult, model.screen)
		assert.Equal(t, screenResult, model.screen)
		assert.Empty(t, model.sourceMigrations)
	})
}

func TestModelNavigationAndLayout(t *testing.T) {
	t.Run("opens source migration from list", func(t *testing.T) {
		status := "in_progress"
		sourceDetail := &elmapi.MigrationDetail{
			Migration: &elmapi.MigrationSummary{
				MigrationID:       "source-1",
				TargetMigrationID: 42,
			},
		}
		svc := &fakeService{
			listSourceMigrations: func(context.Context, string) ([]elmapi.MigrationSummary, error) {
				return []elmapi.MigrationSummary{{
					MigrationID:             "source-1",
					Status:                  &status,
					SourceOrganizationLogin: "source",
					SourceRepositoryName:    "repo",
					TargetOrganizationLogin: "target",
					TargetRepositoryName:    "repo",
				}}, nil
			},
			getSourceMigration: func(context.Context, workflow.SourceMigrationID) (*elmapi.MigrationDetail, error) {
				return sourceDetail, nil
			},
			getTargetMigration: func(context.Context, workflow.TargetMigrationID) (*elmapi.TargetMigration, error) {
				return &elmapi.TargetMigration{}, nil
			},
		}
		model := New(t.Context(), svc)
		setConfigurationReady(model)
		_, _ = model.Update(tea.WindowSizeMsg{Width: 100, Height: 60})

		updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		model = updated.(*Model)
		require.Equal(t, screenSourceList, model.screen)
		require.NotNil(t, cmd)

		updated, _ = model.Update(cmd())
		model = updated.(*Model)
		require.Len(t, model.sourceMigrations, 1)

		updated, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		model = updated.(*Model)
		require.Equal(t, screenSourceDetail, model.screen)
		require.NotNil(t, cmd)

		updated, _ = model.Update(cmd())
		model = updated.(*Model)
		assert.Equal(t, workflow.SourceMigrationID("source-1"), model.sourceID)
		assert.Equal(t, workflow.TargetMigrationID(42), model.targetID)
		assert.Contains(t, model.View(), "Details")

		model.actionFocus = len(model.sourceActionItems()) - 1
		updated, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		model = updated.(*Model)
		require.Equal(t, screenTargetDetail, model.screen)
		require.NotNil(t, cmd)

		updated, _ = model.Update(cmd())
		model = updated.(*Model)
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEscape})
		model = updated.(*Model)
		assert.Equal(t, screenSourceDetail, model.screen)
	})

	t.Run("manual target form validates ID", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		model.screen = screenTargetList

		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
		model = updated.(*Model)
		require.Equal(t, screenForm, model.screen)

		for _, r := range "not-a-number" {
			updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
			model = updated.(*Model)
		}
		updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		model = updated.(*Model)

		assert.Nil(t, cmd)
		require.Error(t, model.form.err)
		assert.Contains(t, model.form.err.Error(), "positive integer")
	})

	t.Run("source detail clears stale linked target ID", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		model.targetID = 42

		updated, _ := model.Update(sourceDetailMsg{
			detail: &elmapi.MigrationDetail{Migration: &elmapi.MigrationSummary{MigrationID: "source-2"}},
		})
		model = updated.(*Model)

		assert.Zero(t, model.targetID)
	})

	t.Run("source actions remain visible in a standard terminal", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		model.screen = screenSourceDetail
		model.sourceID = "source-1"
		model.sourceDetail = &elmapi.MigrationDetail{
			Migration: &elmapi.MigrationSummary{MigrationID: "source-1"},
			Messages: []elmapi.MigrationMessage{
				{Message: strings.Repeat("long detail ", 80)},
			},
		}
		model.targetID = 42
		model.width = 80
		model.height = 24
		model.actionFocus = len(model.sourceActionItems()) - 1

		assert.Contains(t, model.View(), "Details")
		assert.NotContains(t, model.View(), "Actions")
	})

	t.Run("narrow detail preserves all scrollable content", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		model.width = 80
		model.height = 24
		model.screen = screenSourceDetail
		status := elmapi.StatusInProgress
		model.sourceDetail = &elmapi.MigrationDetail{
			Migration: &elmapi.MigrationSummary{MigrationID: "source-1", Status: &status},
			Messages: []elmapi.MigrationMessage{
				{Message: strings.Repeat("detail ", 30) + "tail marker"},
			},
		}

		content := model.sourceDetailView()
		assert.Contains(t, content, "tail marker")
		assert.NotContains(t, content, "Actions")
	})

	t.Run("detail actions use horizontal focus", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		model.screen = screenSourceDetail
		setSourceStatus(model, elmapi.StatusCreated)

		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRight})
		model = updated.(*Model)
		assert.Equal(t, 1, model.actionFocus)

		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
		model = updated.(*Model)
		assert.Equal(t, 1, model.actionFocus)

		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyLeft})
		model = updated.(*Model)
		assert.Zero(t, model.actionFocus)
		assert.Contains(t, model.View(), "←/→ select action")
	})

	t.Run("advanced destination actions use a vertical menu", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		model.screen = screenTargetDetail
		model.targetDetail = &elmapi.TargetMigration{Status: elmapi.TargetMigrationStatusInProgress}

		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
		model = updated.(*Model)
		assert.Equal(t, 1, model.actionFocus)

		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
		model = updated.(*Model)
		assert.Equal(t, 1, model.actionFocus)

		view := model.detailActionView(screenTargetDetail)
		assert.GreaterOrEqual(t, strings.Count(view, "\n"), len(model.targetActionItems())-1)
		assert.Contains(t, view, "List repository resources")
		assert.Contains(t, model.View(), "↑/k up")
	})

	t.Run("action screens always select their first action", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		model.cursor = 3

		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		model = updated.(*Model)
		updated, _ = model.Update(configMsg{
			configuration: &workflow.Configuration{},
			generation:    model.configGeneration,
		})
		model = updated.(*Model)

		assert.Equal(t, screenConfiguration, model.screen)
		assert.Zero(t, model.actionFocus)
		assert.Contains(
			t,
			model.actionButtons(configurationActions, model.actionFocus, model.contentWidth()),
			model.styles.FocusedButton.Padding(0, 2).Render("Edit configuration  e"),
		)
		assert.NotContains(t, actionIDs(configurationActions), "refresh")

		updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
		model = updated.(*Model)
		assert.NotNil(t, command)
		assert.True(t, model.loading)
	})

	t.Run("action shortcuts focus and activate the matching button", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		model.screen = screenSourceDetail
		model.sourceID = "source-1"
		setSourceStatus(model, elmapi.StatusCreated)

		updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
		model = updated.(*Model)

		assert.Nil(t, command)
		assert.Equal(t, screenConfirm, model.screen)
		assert.Contains(t, model.View(), "Cancel migration")
	})

	t.Run("open alias activates the focused action", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		model.screen = screenTargetDetail
		model.targetDetail = &elmapi.TargetMigration{Status: elmapi.TargetMigrationStatusInProgress}
		for index, action := range model.targetActionItems() {
			if action.id == "pause" {
				model.actionFocus = index
				break
			}
		}

		updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
		model = updated.(*Model)

		assert.Nil(t, command)
		assert.Equal(t, screenConfirm, model.screen)
		assert.Contains(t, model.View(), "Pause target migration")
	})

	t.Run("action buttons wrap without changing focus order", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		actions := []actionItem{
			{id: "one", label: "First"},
			{id: "two", label: "Second"},
			{id: "three", label: "Third"},
		}

		view := model.actionButtons(actions, 1, 20)

		assert.Greater(t, strings.Count(view, "\n"), 0)
		assert.Contains(t, view, model.styles.FocusedButton.Padding(0, 2).Render("Second"))
		for line := range strings.SplitSeq(view, "\n") {
			assert.LessOrEqual(t, lipgloss.Width(line), 20)
		}
	})

	t.Run("selected cards preserve geometry", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})

		selected := model.selectorCard("Repository", true, true)
		unselected := model.selectorCard("Repository", false, true)

		assert.Equal(t, lipgloss.Width(unselected), lipgloss.Width(selected))
		assert.Equal(t, lipgloss.Height(unselected), lipgloss.Height(selected))
	})

	t.Run("forms keep the focused field visible in short terminals", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		model.width = 80
		model.height = 16
		first, second, third, fourth := "one", "two", "three", "four"
		model.form = formState{
			cursor: 3,
			fields: []formField{
				textFormField("First", "", &first),
				textFormField("Second", "", &second),
				textFormField("Third", "", &third),
				textFormField("Fourth", "", &fourth),
			},
		}

		view := model.formView()

		assert.Contains(t, view, "Fourth")
		assert.Contains(t, view, "more field(s)")
		assert.LessOrEqual(t, lipgloss.Height(view), model.bodyHeight())
	})

	t.Run("dynamic warnings synchronize viewport height", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		model = updated.(*Model)
		heightWithoutWarning := model.viewport.Height

		updated, _ = model.Update(configMsg{configuration: &workflow.Configuration{
			SourceURL:      "https://source.example",
			SourceTokenSet: true,
		}})
		model = updated.(*Model)

		assert.Less(t, model.viewport.Height, heightWithoutWarning)
		assert.Equal(t, model.bodyHeight(), model.viewport.Height)
	})

	t.Run("footer stays on the final terminal row", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		model.width = 80
		model.height = 24

		assert.Equal(t, model.height, lipgloss.Height(model.View()))
	})

	t.Run("state changes clamp detail action focus", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		model.screen = screenSourceDetail
		model.actionFocus = 10
		status := elmapi.StatusCompleted

		updated, _ := model.Update(sourceDetailMsg{
			detail: &elmapi.MigrationDetail{
				Migration: &elmapi.MigrationSummary{MigrationID: "source-1", Status: &status},
			},
		})
		model = updated.(*Model)

		assert.Equal(t, len(model.sourceActionItems())-1, model.actionFocus)
	})
}

func TestMigrationCreation(t *testing.T) {
	t.Run("home exposes migration creation", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		setConfigurationReady(model)
		assert.Contains(t, model.View(), "Create migration")

		model.cursor = 1
		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		model = updated.(*Model)

		assert.Equal(t, screenPicker, model.screen)
		assert.Equal(t, "Select source repository", model.picker.title)
	})

	t.Run("migration creation discovers repositories and organizations", func(t *testing.T) {
		var sourceCreateInput workflow.SourceCreateInput
		svc := &fakeService{
			listSourceRepositories: func(context.Context) ([]elmapi.Repository, error) {
				return []elmapi.Repository{{FullName: "acme/api"}, {FullName: "octo/web"}}, nil
			},
			listTargetOrganizations: func(context.Context) ([]string, error) {
				return []string{"acme-cloud", "octo-cloud"}, nil
			},
			createSourceMigration: func(_ context.Context, input workflow.SourceCreateInput) (*workflow.SourceCreateResult, error) {
				sourceCreateInput = input
				return &workflow.SourceCreateResult{
					Migration: elmapi.CreateMigrationResponse{MigrationID: "created-1"},
				}, nil
			},
		}
		model := New(t.Context(), svc)
		updated, command := model.openSourceCreateForm(screenHome)
		model = updated.(*Model)
		require.NotNil(t, command)

		updated, _ = model.Update(command())
		model = updated.(*Model)
		assert.Contains(t, model.pickerView(), "acme/api")

		updated, command = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		model = updated.(*Model)
		require.NotNil(t, command)
		updated, _ = model.Update(command())
		model = updated.(*Model)
		assert.Equal(t, "Select destination organization", model.picker.title)

		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		model = updated.(*Model)
		require.Equal(t, screenForm, model.screen)
		assert.Contains(t, model.form.description, "Source: acme/api")
		assert.Contains(t, model.form.description, "Destination organization: acme-cloud")
		assert.Equal(t, "api", *model.form.fields[0].text)
		assert.Equal(t, createMigrationActions, model.form.actions)
		assert.Contains(t, model.formView(), "Create")
		assert.Contains(t, model.formView(), "Cancel")

		*model.form.fields[0].text = "renamed-api"
		*model.form.fields[1].text = "private"
		*model.form.fields[2].boolean = true
		model.form.cursor = len(model.form.fields)
		updated, command = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		model = updated.(*Model)
		require.NotNil(t, command)
		message := command()
		assert.Empty(t, model.sourceID)
		updated, _ = model.Update(message)
		model = updated.(*Model)

		assert.Equal(t, screenResult, model.screen)
		assert.True(t, model.result.popup)
		assert.Contains(t, model.View(), "Migration created")
		assert.Contains(t, model.View(), "Close")
		assert.Equal(t, workflow.SourceMigrationID("created-1"), model.sourceID)
		assert.Equal(t, workflow.SourceCreateInput{
			SourceOwner: "acme",
			SourceRepo:  "api",
			TargetOwner: "acme-cloud",
			TargetRepo:  "renamed-api",
			Visibility:  "private",
			Start:       true,
		}, sourceCreateInput)

		updated, command = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		model = updated.(*Model)
		assert.Equal(t, screenSourceDetail, model.screen)
		assert.NotNil(t, command)
	})

	t.Run("repository picker presents real metadata", func(t *testing.T) {
		repository := elmapi.Repository{
			FullName:       "acme/api",
			Description:    "Public API for Acme products.",
			Language:       "Go",
			Visibility:     "private",
			Stargazers:     42,
			OpenIssueCount: 7,
		}
		repository.Owner.Type = "Organization"
		model := New(t.Context(), &fakeService{
			listSourceRepositories: func(context.Context) ([]elmapi.Repository, error) {
				return []elmapi.Repository{repository}, nil
			},
		})
		model.width = 120
		model.height = 40

		updated, command := model.openSourceCreateForm(screenHome)
		model = updated.(*Model)
		require.NotNil(t, command)
		updated, _ = model.Update(command())
		model = updated.(*Model)

		view := model.pickerView()
		assert.Contains(t, view, "★ 42")
		assert.Contains(t, view, "≡ 7")
		assert.Contains(t, view, "◆ Go")
		assert.Contains(t, view, "Public API for Acme products.")

		model.width = 80
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
		model = updated.(*Model)
		assert.True(t, model.pickerInfoOpen)
		assert.Contains(t, model.View(), "Open issues")
	})

	t.Run("repository details remain visible after the picker scrolls", func(t *testing.T) {
		repositories := make([]elmapi.Repository, 20)
		for index := range repositories {
			repositories[index].FullName = fmt.Sprintf("acme/repo-%02d", index)
			repositories[index].Description = fmt.Sprintf("Repository %02d", index)
		}
		model := New(t.Context(), &fakeService{
			listSourceRepositories: func(context.Context) ([]elmapi.Repository, error) {
				return repositories, nil
			},
		})
		model.width = 120
		model.height = 16
		updated, command := model.openSourceCreateForm(screenHome)
		model = updated.(*Model)
		updated, _ = model.Update(command())
		model = updated.(*Model)
		model.picker.cursor = len(repositories) - 1

		view := model.pickerView()

		assert.Contains(t, view, "↑")
		assert.Contains(t, view, "Repository 19")
	})

	t.Run("repository picker filters options", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		model.screen = screenPicker
		model.picker = pickerState{
			title: "Select source repository",
			items: []pickerItem{{value: "acme/api"}, {value: "octo/web"}},
			input: textinput.New(),
		}

		assert.NotContains(t, model.View(), "filter source repositories")
		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
		model = updated.(*Model)
		assert.Empty(t, model.picker.input.Value())

		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
		model = updated.(*Model)
		assert.True(t, model.picker.search)
		assert.True(t, model.picker.input.Focused())

		for _, character := range "octo" {
			updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{character}})
			model = updated.(*Model)
		}

		assert.Equal(t, []pickerItem{{value: "octo/web"}}, model.visiblePickerItems())
		assert.Contains(t, model.View(), "Select source repository · filter")
		assert.NotContains(t, model.pickerView(), "Search:")

		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
		model = updated.(*Model)
		assert.False(t, model.picker.search)
		assert.False(t, model.picker.input.Focused())
		assert.Len(t, model.visiblePickerItems(), 2)
	})

	t.Run("repository picker offers manual fallback", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		updated, _ := model.openSourceCreateForm(screenHome)
		model = updated.(*Model)

		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
		model = updated.(*Model)

		assert.Equal(t, screenForm, model.screen)
		assert.Equal(t, "Create migration manually", model.form.title)
	})

	t.Run("destination picker fallback preserves the source", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		model.screen = screenPicker
		model.picker = pickerState{
			kind:   pickerTargetOrganization,
			parent: screenHome,
			source: "acme/api",
			input:  textinput.New(),
		}

		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
		model = updated.(*Model)

		require.Equal(t, screenForm, model.screen)
		assert.Equal(t, "acme/api", *model.form.fields[0].text)
	})

	t.Run("repository picker ignores stale catalog responses", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		model.screen = screenPicker
		model.pickerGeneration = 2
		model.picker = pickerState{
			loading: true,
			input:   textinput.New(),
		}

		updated, _ := model.Update(pickerCatalogMsg{
			generation: 1,
			items:      []pickerItem{{value: "stale/repo"}},
		})
		model = updated.(*Model)

		assert.True(t, model.picker.loading)
		assert.Empty(t, model.picker.items)
	})

	t.Run("manual migration creation uses source and target coordinates", func(t *testing.T) {
		var sourceCreateInput workflow.SourceCreateInput
		svc := &fakeService{
			createSourceMigration: func(_ context.Context, input workflow.SourceCreateInput) (*workflow.SourceCreateResult, error) {
				sourceCreateInput = input
				return &workflow.SourceCreateResult{
					Migration: elmapi.CreateMigrationResponse{MigrationID: "created-1"},
				}, nil
			},
		}
		model := New(t.Context(), svc)
		updated, _ := model.openManualSourceCreateForm(screenHome, "")
		model = updated.(*Model)

		require.Len(t, model.form.fields, 4)
		assert.Equal(t, "Source repository", model.form.fields[0].label)
		assert.Equal(t, "Target repository", model.form.fields[1].label)
		assert.Contains(t, model.formView(), "source-org/source-repo")
		assert.Contains(t, model.formView(), "target-org/target-repo")
		assert.Equal(t, createMigrationActions, model.form.actions)

		*model.form.fields[0].text = "source-org/source-repo"
		*model.form.fields[1].text = "target-org/target-repo"
		*model.form.fields[2].text = "internal"
		*model.form.fields[3].boolean = true
		model.form.cursor = len(model.form.fields)
		updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		model = updated.(*Model)
		require.NotNil(t, command)
		updated, _ = model.Update(command())
		model = updated.(*Model)

		assert.Equal(t, screenResult, model.screen)
		assert.True(t, model.result.popup)
		assert.Equal(t, workflow.SourceMigrationID("created-1"), model.sourceID)
		assert.Equal(t, workflow.SourceCreateInput{
			SourceOwner: "source-org",
			SourceRepo:  "source-repo",
			TargetOwner: "target-org",
			TargetRepo:  "target-repo",
			Visibility:  "internal",
			Start:       true,
		}, sourceCreateInput)
	})

	t.Run("manual migration creation rejects malformed coordinates", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		updated, _ := model.openManualSourceCreateForm(screenHome, "")
		model = updated.(*Model)

		*model.form.fields[0].text = "source-org/source-repo/extra"
		*model.form.fields[1].text = "target-org/target-repo"
		model.form.cursor = len(model.form.fields)
		updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		model = updated.(*Model)

		assert.Nil(t, command)
		require.Error(t, model.form.err)
		assert.Contains(t, model.form.err.Error(), "invalid source repository")
	})
}

func TestModelActions(t *testing.T) {
	t.Run("destructive source action requires confirmation", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		model.screen = screenSourceDetail
		model.sourceID = "source-1"
		status := elmapi.StatusCreated
		model.sourceDetail = &elmapi.MigrationDetail{
			Migration: &elmapi.MigrationSummary{MigrationID: "source-1", Status: &status},
		}
		for index, action := range model.sourceActionItems() {
			if action.id == "cancel" {
				model.actionFocus = index
				break
			}
		}

		updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		model = updated.(*Model)

		assert.Nil(t, cmd)
		assert.Equal(t, screenConfirm, model.screen)
		assert.Contains(t, model.View(), "cannot be undone")
	})

	t.Run("confirmation overlays preserve parent content and support button focus", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		model.width = 100
		model.height = 40
		model.screen = screenSourceDetail
		model.sourceID = "source-1"
		setSourceStatus(model, elmapi.StatusCreated)
		updated, _ := model.confirmAction(
			"Cancel migration",
			"This cannot be undone.",
			screenSourceDetail,
			func() tea.Msg { return nil },
		)
		model = updated.(*Model)

		view := model.View()
		assert.Contains(t, view, "Migration source-1")
		assert.Contains(t, view, "Cancel migration")
		assert.Contains(t, view, "Confirm")

		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
		model = updated.(*Model)
		assert.Equal(t, 1, model.confirm.focus)

		updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		model = updated.(*Model)
		assert.Nil(t, command)
		assert.Equal(t, screenSourceDetail, model.screen)
	})

	t.Run("source actions follow migration state", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})

		t.Run("created can start or cancel", func(t *testing.T) {
			setSourceStatus(model, elmapi.StatusCreated)
			assert.ElementsMatch(t, []string{"refresh", "start", "cancel"}, actionIDs(model.sourceActionItems()))
		})

		t.Run("in progress can pause force cutover or cancel", func(t *testing.T) {
			setSourceStatus(model, elmapi.StatusInProgress)
			assert.ElementsMatch(t, []string{"refresh", "pause", "force-cutover", "cancel"}, actionIDs(model.sourceActionItems()))
		})

		t.Run("ready migration offers normal cutover", func(t *testing.T) {
			setSourceStatus(model, elmapi.StatusInProgress)
			model.sourceDetail.CombinedState = &elmapi.CombinedState{ReadyForCutover: true}
			assert.Contains(t, actionIDs(model.sourceActionItems()), "cutover")
			assert.NotContains(t, actionIDs(model.sourceActionItems()), "force-cutover")
		})

		t.Run("paused can resume or cancel", func(t *testing.T) {
			setSourceStatus(model, elmapi.StatusPaused)
			assert.ElementsMatch(t, []string{"refresh", "resume", "cancel"}, actionIDs(model.sourceActionItems()))
		})

		t.Run("completed can revert", func(t *testing.T) {
			setSourceStatus(model, elmapi.StatusCompleted)
			assert.ElementsMatch(t, []string{"refresh", "revert"}, actionIDs(model.sourceActionItems()))
		})
	})

	t.Run("destination actions follow migration state", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})

		t.Run("in progress can pause or abort", func(t *testing.T) {
			model.targetDetail = &elmapi.TargetMigration{Status: elmapi.TargetMigrationStatusInProgress}
			assert.ElementsMatch(t,
				[]string{"refresh", "resources", "report-request", "report-status", "report-url", "pause", "abort"},
				actionIDs(model.targetActionItems()),
			)
		})

		t.Run("paused can resume or abort", func(t *testing.T) {
			model.targetDetail = &elmapi.TargetMigration{Status: elmapi.TargetMigrationStatusPaused}
			assert.ElementsMatch(t,
				[]string{"refresh", "resources", "report-request", "report-status", "report-url", "resume", "abort"},
				actionIDs(model.targetActionItems()),
			)
		})

		t.Run("completed has no lifecycle mutation", func(t *testing.T) {
			model.targetDetail = &elmapi.TargetMigration{Status: elmapi.TargetMigrationStatusComplete}
			assert.ElementsMatch(t,
				[]string{"refresh", "resources", "report-request", "report-status", "report-url"},
				actionIDs(model.targetActionItems()),
			)
		})
	})

	t.Run("immediate mannequin reclaim requires confirmation", func(t *testing.T) {
		reclaimCalls := 0
		svc := &fakeService{
			reclaimMannequins: func(context.Context, workflow.MannequinReclaimInput, ghapi.Logger) error {
				reclaimCalls++
				return nil
			},
		}
		model := New(t.Context(), svc)
		model.screen = screenMannequins

		updated, _ := model.openMannequinReclaimForm(false)
		model = updated.(*Model)
		*model.form.fields[0].text = "octo-org"
		*model.form.fields[1].text = "mannequin"
		*model.form.fields[3].text = "app[bot]"
		model.form.cursor = len(model.form.fields) - 1

		updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		model = updated.(*Model)
		require.NotNil(t, cmd)

		updated, _ = model.Update(cmd())
		model = updated.(*Model)
		require.Equal(t, screenConfirm, model.screen)
		assert.Contains(t, model.View(), "cannot be undone")
		assert.Zero(t, reclaimCalls)

		updated, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
		model = updated.(*Model)
		require.NotNil(t, cmd)
		_, _ = model.Update(cmd())
		assert.Equal(t, 1, reclaimCalls)
	})
}

func setSourceStatus(model *Model, status string) {
	model.sourceDetail = &elmapi.MigrationDetail{
		Migration: &elmapi.MigrationSummary{MigrationID: "source-1", Status: &status},
	}
}

func setConfigurationReady(model *Model) {
	model.configuration = &workflow.Configuration{
		SourceURL:      "https://source.example",
		SourceTokenSet: true,
		TargetURL:      "https://target.example",
		TargetTokenSet: true,
	}
	model.sourceAuthChecked = true
	model.targetAuthChecked = true
	model.homeCursorSet = false
	model.syncHomeCursor()
}

func actionIDs(actions []actionItem) []string {
	ids := make([]string, len(actions))
	for index, action := range actions {
		ids[index] = action.id
	}
	return ids
}

type fakeService struct {
	service
	listSourceMigrations    func(context.Context, string) ([]elmapi.MigrationSummary, error)
	getSourceMigration      func(context.Context, workflow.SourceMigrationID) (*elmapi.MigrationDetail, error)
	createSourceMigration   func(context.Context, workflow.SourceCreateInput) (*workflow.SourceCreateResult, error)
	listSourceRepositories  func(context.Context) ([]elmapi.Repository, error)
	listTargetOrganizations func(context.Context) ([]string, error)
	listTargetMigrations    func(context.Context, string, int) ([]elmapi.TargetMigration, error)
	getTargetMigration      func(context.Context, workflow.TargetMigrationID) (*elmapi.TargetMigration, error)
	reclaimMannequins       func(context.Context, workflow.MannequinReclaimInput, ghapi.Logger) error
	getConfiguration        func(context.Context) (*workflow.Configuration, error)
	saveConfiguration       func(context.Context, workflow.ConfigurationInput) error
	resetConfiguration      func(context.Context) error
}

func unexpectedServiceCall(name string) {
	panic("unexpected service call: " + name)
}

func (f *fakeService) ListSourceMigrations(ctx context.Context, status string) ([]elmapi.MigrationSummary, error) {
	if f.listSourceMigrations == nil {
		unexpectedServiceCall("ListSourceMigrations")
	}
	return f.listSourceMigrations(ctx, status)
}

func (f *fakeService) ListSourceRepositories(ctx context.Context) ([]elmapi.Repository, error) {
	if f.listSourceRepositories == nil {
		unexpectedServiceCall("ListSourceRepositories")
	}
	return f.listSourceRepositories(ctx)
}

func (f *fakeService) GetSourceMigration(ctx context.Context, id workflow.SourceMigrationID) (*elmapi.MigrationDetail, error) {
	if f.getSourceMigration == nil {
		unexpectedServiceCall("GetSourceMigration")
	}
	return f.getSourceMigration(ctx, id)
}

func (f *fakeService) CreateSourceMigration(ctx context.Context, input workflow.SourceCreateInput) (*workflow.SourceCreateResult, error) {
	if f.createSourceMigration == nil {
		unexpectedServiceCall("CreateSourceMigration")
	}
	return f.createSourceMigration(ctx, input)
}

func (f *fakeService) ListTargetMigrations(ctx context.Context, status string, maxResults int) ([]elmapi.TargetMigration, error) {
	if f.listTargetMigrations == nil {
		unexpectedServiceCall("ListTargetMigrations")
	}
	return f.listTargetMigrations(ctx, status, maxResults)
}

func (f *fakeService) ListTargetOrganizations(ctx context.Context) ([]string, error) {
	if f.listTargetOrganizations == nil {
		unexpectedServiceCall("ListTargetOrganizations")
	}
	return f.listTargetOrganizations(ctx)
}

func (f *fakeService) GetTargetMigration(ctx context.Context, id workflow.TargetMigrationID) (*elmapi.TargetMigration, error) {
	if f.getTargetMigration == nil {
		unexpectedServiceCall("GetTargetMigration")
	}
	return f.getTargetMigration(ctx, id)
}

func (f *fakeService) ReclaimMannequins(ctx context.Context, input workflow.MannequinReclaimInput, logger ghapi.Logger) error {
	if f.reclaimMannequins == nil {
		unexpectedServiceCall("ReclaimMannequins")
	}
	return f.reclaimMannequins(ctx, input, logger)
}

func (f *fakeService) GetConfiguration(ctx context.Context) (*workflow.Configuration, error) {
	if f.getConfiguration == nil {
		unexpectedServiceCall("GetConfiguration")
	}
	return f.getConfiguration(ctx)
}

func (f *fakeService) SaveConfiguration(ctx context.Context, input workflow.ConfigurationInput) error {
	if f.saveConfiguration == nil {
		unexpectedServiceCall("SaveConfiguration")
	}
	return f.saveConfiguration(ctx, input)
}

func (f *fakeService) ResetConfiguration(ctx context.Context) error {
	if f.resetConfiguration == nil {
		unexpectedServiceCall("ResetConfiguration")
	}
	return f.resetConfiguration(ctx)
}
