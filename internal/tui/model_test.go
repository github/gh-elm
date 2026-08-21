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

	t.Run("shows incomplete configuration warning above the title", func(t *testing.T) {
		model := New(t.Context(), &fakeService{})
		updated, _ := model.Update(configMsg{configuration: &workflow.Configuration{
			SourceURL:      "https://source.example",
			SourceTokenSet: true,
		}})
		model = updated.(*Model)

		view := model.View()
		warningIndex := strings.Index(view, "Configuration incomplete")
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

		assert.NotContains(t, model.View(), "Configuration incomplete")
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

		model.cursor = len(sourceActions) - 1
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
		model.width = 80
		model.height = 24
		model.cursor = len(sourceActions) - 1

		assert.Contains(t, model.View(), "Open destination details")
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
		model.cursor = 5

		updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		model = updated.(*Model)

		assert.Nil(t, cmd)
		assert.Equal(t, screenConfirm, model.screen)
		assert.Contains(t, model.View(), "cannot be undone")
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

type fakeService struct {
	sourceMigrations []elmapi.MigrationSummary
	sourceDetail     *elmapi.MigrationDetail
	reclaimCalls     int
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

func (f *fakeService) SaveConfiguration(context.Context, workflow.ConfigurationInput) error {
	return nil
}

func (f *fakeService) ResetConfiguration(context.Context) error {
	return nil
}
