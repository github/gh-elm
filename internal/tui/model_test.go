package tui

import (
	"context"
	"encoding/json"
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

func TestModel(t *testing.T) {
	t.Run("loads configuration readiness on startup", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})

		command := model.Init()

		require.NotNil(t, command)
	})

	t.Run("background prefetch errors stay off the home screen", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		updated, _ := model.Update(sourceListMsg{err: assert.AnError})
		model = updated.(*Model)

		assert.Equal(t, screenHome, model.screen)
		assert.NotContains(t, model.View(), assert.AnError.Error())

		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		model = updated.(*Model)
		assert.Contains(t, model.View(), assert.AnError.Error())
	})

	t.Run("uses prefetched migrations without another request", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		updated, _ := model.Update(sourceListMsg{migrations: []elmapi.MigrationSummary{{MigrationID: "source-1"}}})
		model = updated.(*Model)

		updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		model = updated.(*Model)

		assert.Equal(t, screenSourceList, model.screen)
		assert.Nil(t, command)
	})

	t.Run("configuration response does not unlock a pending migration list", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		model.sourceListLoading = true
		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		model = updated.(*Model)
		require.True(t, model.loading)

		updated, _ = model.Update(configMsg{configuration: &workflow.Configuration{}})
		model = updated.(*Model)

		assert.True(t, model.loading)
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

	t.Run("styles the home headline", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})

		view := model.View()

		assert.Contains(t, view, model.styles.Primary.Bold(true).Render("GitHub Enterprise"))
		assert.Contains(t, view, model.styles.Success.Render("Live migrations"))
	})

	t.Run("cancels a destination migration load and returns home", func(t *testing.T) {
		started := make(chan struct{})
		service := &fakeService{
			listTargetMigrations: func(ctx context.Context) ([]elmapi.TargetMigration, error) {
				close(started)
				<-ctx.Done()
				return nil, ctx.Err()
			},
		}
		model := New(t.Context(), service)
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

		assert.NotContains(t, view, "Source authentication")
		assert.NotContains(t, view, "Destination authentication")
		assert.Contains(t, view, "Source token")
		assert.Contains(t, view, "Destination token")
	})

	t.Run("shows authentication rows when prerequisites are configured", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		model.configuration = &workflow.Configuration{
			SourceURL:      "https://source.example",
			SourceTokenSet: true,
			TargetURL:      "https://target.example",
			TargetTokenSet: true,
		}

		view := model.configurationView()

		assert.Contains(t, view, "Source authentication")
		assert.Contains(t, view, "Destination authentication")
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

	t.Run("colors preflight status marks", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})

		assert.Equal(t, model.styles.Success.Render("✓"), model.checkMark(true))
		assert.Equal(t, model.styles.Failure.Render("✗"), model.checkMark(false))
		assert.Equal(t, model.styles.Muted.Render("…"), model.authenticationMark(false, nil))
	})

	t.Run("opens source migration from list", func(t *testing.T) {
		status := "in_progress"
		svc := &fakeService{
			sourceMigrations: []elmapi.MigrationSummary{{
				MigrationID:             "source-1",
				Status:                  &status,
				SourceOrganizationLogin: "source",
				SourceRepositoryName:    "repo",
				TargetOrganizationLogin: "target",
				TargetRepositoryName:    "repo",
			}},
			sourceDetail: &elmapi.MigrationDetail{
				Migration: &elmapi.MigrationSummary{
					MigrationID:       "source-1",
					TargetMigrationID: 42,
				},
			},
		}
		model := New(t.Context(), svc)
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
		assert.Contains(t, model.View(), "Open destination details")

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
		}
		model.targetID = 42
		model.width = 80
		model.height = 24
		model.actionFocus = len(model.sourceActionItems()) - 1

		assert.Contains(t, model.View(), "Open destination details")
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
		assert.Less(t, strings.Index(content, "Migration ID"), strings.Index(content, "Actions"))
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

	t.Run("action screens always select their first action", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		model.cursor = 3

		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		model = updated.(*Model)
		updated, _ = model.Update(configMsg{configuration: &workflow.Configuration{}})
		model = updated.(*Model)

		assert.Equal(t, screenConfiguration, model.screen)
		assert.Zero(t, model.actionFocus)
		assert.Contains(
			t,
			model.actionButtons(configurationActions, model.actionFocus, model.contentWidth()),
			model.styles.FocusedButton.Padding(0, 2).Render("Refresh configuration  r"),
		)
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
		model.form = formState{
			cursor: 3,
			fields: []formField{
				{label: "First", value: "one"},
				{label: "Second", value: "two"},
				{label: "Third", value: "three"},
				{label: "Fourth", value: "four"},
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

	t.Run("home exposes migration creation", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		assert.Contains(t, model.View(), "Create migration")

		model.cursor = 1
		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		model = updated.(*Model)

		assert.Equal(t, screenPicker, model.screen)
		assert.Equal(t, "Select source repository", model.picker.title)
	})

	t.Run("migration creation discovers repositories and organizations", func(t *testing.T) {
		svc := &fakeService{
			sourceRepositories:  []string{"acme/api", "octo/web"},
			targetOrganizations: []string{"acme-cloud", "octo-cloud"},
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
		assert.Equal(t, "api", model.form.fields[0].value)

		command, err := model.form.submit(map[string]string{
			"targetRepo": "renamed-api",
			"visibility": "private",
			"start":      "true",
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
			model := New(t.Context(), &fakeService{sourceRepositoryDetails: []elmapi.Repository{repository}})
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
			model := New(t.Context(), &fakeService{sourceRepositoryDetails: repositories})
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
		require.NoError(t, err)
		require.NotNil(t, command)
		_ = command()
		assert.Equal(t, workflow.SourceCreateInput{
			SourceOwner: "acme",
			SourceRepo:  "api",
			TargetOwner: "acme-cloud",
			TargetRepo:  "renamed-api",
			Visibility:  "private",
			Start:       true,
		}, svc.sourceCreateInput)

		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEscape})
		model = updated.(*Model)
		assert.Equal(t, screenPicker, model.screen)
		assert.Equal(t, "Select destination organization", model.picker.title)

		updated, command = model.Update(tea.KeyMsg{Type: tea.KeyEscape})
		model = updated.(*Model)
		require.NotNil(t, command)
		assert.Equal(t, "Select source repository", model.picker.title)
	})

	t.Run("repository picker filters options", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		model.screen = screenPicker
		model.picker = pickerState{
			items: []pickerItem{{value: "acme/api"}, {value: "octo/web"}},
			input: textinput.New(),
		}
		model.picker.input.Focus()

		for _, character := range "octo" {
			updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{character}})
			model = updated.(*Model)
		}

		assert.Equal(t, []pickerItem{{value: "octo/web"}}, model.visiblePickerItems())
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
		assert.Equal(t, "acme/api", model.form.fields[0].value)
	})

	t.Run("repository picker ignores stale catalog responses", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		model.screen = screenPicker
		model.pickerGeneration = 2
		model.picker = pickerState{
			generation: 2,
			loading:    true,
			input:      textinput.New(),
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
		svc := &fakeService{}
		model := New(t.Context(), svc)
		updated, _ := model.openManualSourceCreateForm(screenHome, "")
		model = updated.(*Model)

		require.Len(t, model.form.fields, 4)
		assert.Equal(t, "Source repository", model.form.fields[0].label)
		assert.Equal(t, "Target repository", model.form.fields[1].label)
		assert.Contains(t, model.formView(), "source-org/source-repo")
		assert.Contains(t, model.formView(), "target-org/target-repo")

		command, err := model.form.submit(map[string]string{
			"source":     "source-org/source-repo",
			"target":     "target-org/target-repo",
			"visibility": "internal",
			"start":      "true",
		})
		require.NoError(t, err)
		require.NotNil(t, command)
		_ = command()

		assert.Equal(t, workflow.SourceCreateInput{
			SourceOwner: "source-org",
			SourceRepo:  "source-repo",
			TargetOwner: "target-org",
			TargetRepo:  "target-repo",
			Visibility:  "internal",
			Start:       true,
		}, svc.sourceCreateInput)
	})

	t.Run("manual migration creation rejects malformed coordinates", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		updated, _ := model.openManualSourceCreateForm(screenHome, "")
		model = updated.(*Model)

		command, err := model.form.submit(map[string]string{
			"source": "source-org/source-repo/extra",
			"target": "target-org/target-repo",
		})

		assert.Nil(t, command)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid source repository")
	})

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
			assert.ElementsMatch(t, []string{"refresh", "watch", "start", "cancel"}, actionIDs(model.sourceActionItems()))
		})

		t.Run("in progress can pause force cutover or cancel", func(t *testing.T) {
			setSourceStatus(model, elmapi.StatusInProgress)
			assert.ElementsMatch(t, []string{"refresh", "watch", "pause", "force-cutover", "cancel"}, actionIDs(model.sourceActionItems()))
		})

		t.Run("ready migration offers normal cutover", func(t *testing.T) {
			setSourceStatus(model, elmapi.StatusInProgress)
			model.sourceDetail.CombinedState = &elmapi.CombinedState{ReadyForCutover: true}
			assert.Contains(t, actionIDs(model.sourceActionItems()), "cutover")
			assert.NotContains(t, actionIDs(model.sourceActionItems()), "force-cutover")
		})

		t.Run("paused can resume or cancel", func(t *testing.T) {
			setSourceStatus(model, elmapi.StatusPaused)
			assert.ElementsMatch(t, []string{"refresh", "watch", "resume", "cancel"}, actionIDs(model.sourceActionItems()))
		})

		t.Run("completed can revert", func(t *testing.T) {
			setSourceStatus(model, elmapi.StatusCompleted)
			assert.ElementsMatch(t, []string{"refresh", "watch", "revert"}, actionIDs(model.sourceActionItems()))
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
		svc := &fakeService{}
		model := New(t.Context(), svc)
		model.screen = screenMannequins

		updated, _ := model.openMannequinReclaimForm(false)
		model = updated.(*Model)
		model.form.fields[0].value = "octo-org"
		model.form.fields[1].value = "mannequin"
		model.form.fields[3].value = "app[bot]"
		model.form.cursor = len(model.form.fields) - 1

		updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		model = updated.(*Model)
		require.NotNil(t, cmd)

		updated, _ = model.Update(cmd())
		model = updated.(*Model)
		require.Equal(t, screenConfirm, model.screen)
		assert.Contains(t, model.View(), "cannot be undone")
		assert.Equal(t, 0, svc.reclaimCalls)

		updated, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
		model = updated.(*Model)
		require.NotNil(t, cmd)
		_, _ = model.Update(cmd())
		assert.Equal(t, 1, svc.reclaimCalls)
	})
}

func setSourceStatus(model *Model, status string) {
	model.sourceDetail = &elmapi.MigrationDetail{
		Migration: &elmapi.MigrationSummary{MigrationID: "source-1", Status: &status},
	}
}

func actionIDs(actions []actionItem) []string {
	ids := make([]string, len(actions))
	for index, action := range actions {
		ids[index] = action.id
	}
	return ids
}

type fakeService struct {
	sourceMigrations        []elmapi.MigrationSummary
	sourceRepositories      []string
	sourceRepositoryDetails []elmapi.Repository
	sourceDetail            *elmapi.MigrationDetail
	sourceCreateInput       workflow.SourceCreateInput
	targetOrganizations     []string
	listTargetMigrations    func(context.Context) ([]elmapi.TargetMigration, error)
	reclaimCalls            int
	sourceAuthErr           error
	targetAuthErr           error
}

func (f *fakeService) ListSourceMigrations(context.Context, string) ([]elmapi.MigrationSummary, error) {
	return f.sourceMigrations, nil
}

func (f *fakeService) ListSourceRepositories(context.Context) ([]elmapi.Repository, error) {
	if f.sourceRepositoryDetails != nil {
		return f.sourceRepositoryDetails, nil
	}
	repositories := make([]elmapi.Repository, len(f.sourceRepositories))
	for index, name := range f.sourceRepositories {
		repositories[index].FullName = name
	}
	return repositories, nil
}

func (f *fakeService) GetSourceMigration(context.Context, workflow.SourceMigrationID) (*elmapi.MigrationDetail, error) {
	return f.sourceDetail, nil
}

func (f *fakeService) CreateSourceMigration(_ context.Context, input workflow.SourceCreateInput) (*workflow.SourceCreateResult, error) {
	f.sourceCreateInput = input
	return &workflow.SourceCreateResult{}, nil
}

func (f *fakeService) StartSourceMigration(context.Context, workflow.SourceMigrationID) error {
	return nil
}

func (f *fakeService) PauseSourceMigration(context.Context, workflow.SourceMigrationID) error {
	return nil
}

func (f *fakeService) ResumeSourceMigration(context.Context, workflow.SourceMigrationID) error {
	return nil
}

func (f *fakeService) CancelSourceMigration(context.Context, workflow.SourceMigrationID) error {
	return nil
}

func (f *fakeService) CutoverSourceMigration(context.Context, workflow.SourceMigrationID, bool) error {
	return nil
}

func (f *fakeService) RevertSourceCutover(context.Context, workflow.SourceMigrationID) (*elmapi.RevertCutoverResponse, error) {
	return &elmapi.RevertCutoverResponse{}, nil
}

func (f *fakeService) ListTargetMigrations(ctx context.Context, _ string, _ int) ([]elmapi.TargetMigration, error) {
	if f.listTargetMigrations != nil {
		return f.listTargetMigrations(ctx)
	}
	return nil, nil
}

func (f *fakeService) ListTargetOrganizations(context.Context) ([]string, error) {
	return f.targetOrganizations, nil
}

func (f *fakeService) CreateTargetMigration(context.Context, workflow.TargetCreateInput) (json.RawMessage, error) {
	return nil, nil
}

func (f *fakeService) GetTargetMigration(context.Context, workflow.TargetMigrationID) (*elmapi.TargetMigration, error) {
	return &elmapi.TargetMigration{}, nil
}

func (f *fakeService) PauseTargetMigration(context.Context, workflow.TargetMigrationID) error {
	return nil
}

func (f *fakeService) ResumeTargetMigration(context.Context, workflow.TargetMigrationID) error {
	return nil
}

func (f *fakeService) AbortTargetMigration(context.Context, workflow.TargetMigrationID) error {
	return nil
}

func (f *fakeService) ListResources(context.Context, workflow.ResourceInput) ([]elmapi.Node, error) {
	return nil, nil
}

func (f *fakeService) RequestReport(context.Context, workflow.ReportInput) (json.RawMessage, error) {
	return nil, nil
}

func (f *fakeService) ReportStatus(context.Context, workflow.ReportInput) (json.RawMessage, error) {
	return nil, nil
}

func (f *fakeService) ReportURL(context.Context, workflow.ReportInput) (json.RawMessage, error) {
	return nil, nil
}

func (f *fakeService) ListMannequins(context.Context, string, bool) ([]ghapi.MannequinRecord, error) {
	return nil, nil
}

func (f *fakeService) ExportMannequins(context.Context, string, string, bool) error {
	return nil
}

func (f *fakeService) ReclaimMannequins(context.Context, workflow.MannequinReclaimInput, ghapi.Logger) error {
	f.reclaimCalls++
	return nil
}

func (f *fakeService) GetConfiguration(context.Context) (*workflow.Configuration, error) {
	return &workflow.Configuration{}, nil
}

func (f *fakeService) CheckSourceAuthentication(context.Context) error {
	return f.sourceAuthErr
}

func (f *fakeService) CheckTargetAuthentication(context.Context) error {
	return f.targetAuthErr
}

func (f *fakeService) SaveConfiguration(context.Context, workflow.ConfigurationInput) error {
	return nil
}

func (f *fakeService) ResetConfiguration(context.Context) error {
	return nil
}
