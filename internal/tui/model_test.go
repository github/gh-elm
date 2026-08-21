package tui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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

	t.Run("shows incomplete configuration warning above the title", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		updated, _ := model.Update(configMsg{configuration: &workflow.Configuration{
			SourceURL:      "https://source.example",
			SourceTokenSet: true,
		}})
		model = updated.(*Model)

		view := model.View()
		warningIndex := strings.Index(view, "Configuration not ready")
		titleIndex := strings.Index(view, "Enterprise Live Migrations")
		require.NotEqual(t, -1, warningIndex)
		require.NotEqual(t, -1, titleIndex)
		assert.Less(t, warningIndex, titleIndex)
		assert.Contains(t, view, "destination URL, destination token")
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

		model.cursor = len(model.sourceActionItems()) - 1
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
		model.cursor = len(model.sourceActionItems()) - 1

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
		assert.Less(t, strings.Index(content, "Actions"), strings.Index(content, "Migration ID"))
	})

	t.Run("home exposes migration creation", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		assert.Contains(t, model.View(), "Create migration")

		model.cursor = 1
		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		model = updated.(*Model)

		assert.Equal(t, screenForm, model.screen)
		assert.Equal(t, "Create migration", model.form.title)
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
				model.cursor = index
				break
			}
		}

		updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		model = updated.(*Model)

		assert.Nil(t, cmd)
		assert.Equal(t, screenConfirm, model.screen)
		assert.Contains(t, model.View(), "cannot be undone")
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
	sourceMigrations []elmapi.MigrationSummary
	sourceDetail     *elmapi.MigrationDetail
	reclaimCalls     int
	sourceAuthErr    error
	targetAuthErr    error
}

func (f *fakeService) ListSourceMigrations(context.Context, string) ([]elmapi.MigrationSummary, error) {
	return f.sourceMigrations, nil
}

func (f *fakeService) GetSourceMigration(context.Context, workflow.SourceMigrationID) (*elmapi.MigrationDetail, error) {
	return f.sourceDetail, nil
}

func (f *fakeService) CreateSourceMigration(context.Context, workflow.SourceCreateInput) (*workflow.SourceCreateResult, error) {
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

func (f *fakeService) ListTargetMigrations(context.Context, string, int) ([]elmapi.TargetMigration, error) {
	return nil, nil
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
