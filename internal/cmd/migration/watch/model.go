package watch

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/github/gh-elm/internal/elmapi"
)

// Model is the bubbletea model for the watch display.
type Model struct {
	migrationID string
	interval    time.Duration
	client      *elmapi.Client
	styles      Styles

	// Current state
	detail      *elmapi.MigrationDetail
	basePhase   Phase
	overlay     Overlay
	lastUpdated time.Time
	fetchErr    error

	// Terminal dimensions
	width  int
	height int
}

// New creates a new Model.
func New(migrationID string, interval time.Duration, client *elmapi.Client) Model {
	return Model{
		migrationID: migrationID,
		interval:    interval,
		client:      client,
		styles:      DefaultStyles(),
	}
}

// Init implements tea.Model. Fires an immediate fetch.
func (m Model) Init() tea.Cmd {
	return fetchStatusCmd(m.client, m.migrationID, m.interval)
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case statusMsg:
		if msg.err != nil {
			m.fetchErr = msg.err
		} else {
			m.detail = msg.detail
			m.basePhase, m.overlay = DerivePhase(msg.detail)
			m.lastUpdated = msg.fetchedAt
			m.fetchErr = nil
		}
		// Schedule next tick
		return m, tickCmd(m.interval)

	case tickMsg:
		// Fetch on tick
		return m, fetchStatusCmd(m.client, m.migrationID, m.interval)
	}

	return m, nil
}
