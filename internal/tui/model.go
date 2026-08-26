// Package tui implements the interactive no-argument gh-elm experience.
package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/github/gh-elm/internal/elmapi"
	"github.com/github/gh-elm/internal/ghapi"
	"github.com/github/gh-elm/internal/render"
	"github.com/github/gh-elm/internal/theme"
	"github.com/github/gh-elm/internal/workflow"
)

type sourceService interface {
	ListSourceMigrations(context.Context, string) ([]elmapi.MigrationSummary, error)
	GetSourceMigration(context.Context, workflow.SourceMigrationID) (*elmapi.MigrationDetail, error)
	CreateSourceMigration(context.Context, workflow.SourceCreateInput) (*workflow.SourceCreateResult, error)
	StartSourceMigration(context.Context, workflow.SourceMigrationID) error
	PauseSourceMigration(context.Context, workflow.SourceMigrationID) error
	ResumeSourceMigration(context.Context, workflow.SourceMigrationID) error
	CancelSourceMigration(context.Context, workflow.SourceMigrationID) error
	CutoverSourceMigration(context.Context, workflow.SourceMigrationID, bool) error
	RevertSourceCutover(context.Context, workflow.SourceMigrationID) (*elmapi.RevertCutoverResponse, error)
}

type targetService interface {
	ListTargetMigrations(context.Context, string, int) ([]elmapi.TargetMigration, error)
	CreateTargetMigration(context.Context, workflow.TargetCreateInput) (json.RawMessage, error)
	GetTargetMigration(context.Context, workflow.TargetMigrationID) (*elmapi.TargetMigration, error)
	PauseTargetMigration(context.Context, workflow.TargetMigrationID) error
	ResumeTargetMigration(context.Context, workflow.TargetMigrationID) error
	AbortTargetMigration(context.Context, workflow.TargetMigrationID) error
	ListResources(context.Context, workflow.ResourceInput) ([]elmapi.Node, error)
	RequestReport(context.Context, workflow.ReportInput) (json.RawMessage, error)
	ReportStatus(context.Context, workflow.ReportInput) (json.RawMessage, error)
	ReportURL(context.Context, workflow.ReportInput) (json.RawMessage, error)
}

type catalogService interface {
	ListSourceRepositories(context.Context) ([]elmapi.Repository, error)
	ListTargetOrganizations(context.Context) ([]string, error)
}

type pickerItem struct {
	value      string
	repository *elmapi.Repository
}

type mannequinService interface {
	ListMannequins(context.Context, string, bool) ([]ghapi.MannequinRecord, error)
	ExportMannequins(context.Context, string, string, bool) error
	ReclaimMannequins(context.Context, workflow.MannequinReclaimInput, ghapi.Logger) error
}

type configurationService interface {
	GetConfiguration(context.Context) (*workflow.Configuration, error)
	CheckSourceAuthentication(context.Context) error
	CheckTargetAuthentication(context.Context) error
	SaveConfiguration(context.Context, workflow.ConfigurationInput) error
	ResetConfiguration(context.Context) error
}

type service interface {
	sourceService
	targetService
	catalogService
	mannequinService
	configurationService
}

type screen int

const (
	screenHome screen = iota
	screenSourceList
	screenSourceDetail
	screenTargetList
	screenTargetDetail
	screenMannequins
	screenConfiguration
	screenPicker
	screenForm
	screenConfirm
	screenResult
)

type fieldKind int

const (
	fieldText fieldKind = iota
	fieldSecret
	fieldBool
	fieldSelect
)

type formField struct {
	key         string
	label       string
	description string
	kind        fieldKind
	value       string
	options     []string
}

type formState struct {
	title       string
	description string
	fields      []formField
	cursor      int
	parent      screen
	submit      func(map[string]string) (tea.Cmd, error)
	err         error
}

type pickerKind int

const (
	pickerSourceRepository pickerKind = iota
	pickerTargetOrganization
)

type pickerState struct {
	kind       pickerKind
	title      string
	parent     screen
	items      []pickerItem
	cursor     int
	input      textinput.Model
	loading    bool
	err        error
	source     string
	generation uint64
}

type confirmState struct {
	title   string
	body    string
	parent  screen
	command tea.Cmd
	focus   int
}

type resultState struct {
	title   string
	body    string
	parent  screen
	refresh bool
}

// Model is the Bubble Tea application model.
type Model struct {
	ctx     context.Context
	service service
	styles  theme.Styles

	screen        screen
	width         int
	height        int
	cursor        int
	actionFocus   int
	loading       bool
	err           error
	viewport      viewport.Model
	viewportReady bool
	showHelp      bool

	sourceMigrations  []elmapi.MigrationSummary
	sourceListLoaded  bool
	sourceListLoading bool
	sourceListErr     error
	sourceID          workflow.SourceMigrationID
	sourceDetail      *elmapi.MigrationDetail
	sourceWatching    bool
	sourceSearch      bool
	searchInput       textinput.Model
	compact           bool
	densityUserSet    bool

	targetMigrations []elmapi.TargetMigration
	targetID         workflow.TargetMigrationID
	targetDetail     *elmapi.TargetMigration
	targetParent     screen
	repository       string
	targetListCancel context.CancelFunc
	targetListGen    uint64

	configuration     *workflow.Configuration
	configurationErr  error
	configGeneration  uint64
	sourceAuthChecked bool
	sourceAuthErr     error
	targetAuthChecked bool
	targetAuthErr     error
	picker            pickerState
	pickerGeneration  uint64
	pickerInfoOpen    bool
	form              formState
	confirm           confirmState
	result            resultState
}

// New creates the main TUI model.
func New(ctx context.Context, svc service) *Model {
	searchInput := textinput.New()
	searchInput.Prompt = ""
	searchInput.Placeholder = "migration ID or repository"
	searchInput.CharLimit = 160

	return &Model{
		ctx:          ctx,
		service:      svc,
		styles:       theme.New(),
		screen:       screenHome,
		targetParent: screenTargetList,
		searchInput:  searchInput,
	}
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	m.sourceListLoading = true
	return tea.Batch(m.startConfigurationLoad(), m.loadSourceListCmd())
}

// Update implements tea.Model.
func (m *Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if !m.viewportReady {
			m.viewport = viewport.New(m.contentWidth(), m.bodyHeight())
			m.viewportReady = true
		}
		m.syncViewportSize()
		return m, nil
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.cancelTargetListLoad()
			return m, tea.Quit
		}
		if m.loading {
			if m.screen == screenTargetList &&
				(key.Matches(msg, keys.Back) || key.Matches(msg, keys.Quit)) {
				m.cancelTargetListLoad()
				return m.back()
			}
			return m, nil
		}
		return m.updateKey(msg)
	case sourceListMsg:
		m.sourceListLoading = false
		m.sourceListLoaded = true
		m.sourceListErr = msg.err
		if m.screen == screenSourceList {
			m.loading = false
			m.err = msg.err
		}
		if msg.err == nil {
			m.sourceMigrations = msg.migrations
			if m.screen == screenSourceList {
				m.cursor = 0
			}
		}
	case sourceDetailMsg:
		m.loading = false
		m.err = msg.err
		if msg.err == nil {
			m.sourceDetail = msg.detail
			m.targetID = 0
			if msg.detail.Migration != nil && msg.detail.Migration.TargetMigrationID > 0 {
				m.targetID = workflow.TargetMigrationID(msg.detail.Migration.TargetMigrationID)
			}
			m.clampActionFocus()
		}
		if m.sourceWatching {
			return m, tea.Tick(2*time.Second, func(time.Time) tea.Msg { return watchTickMsg{} })
		}
	case targetListMsg:
		if msg.generation != m.targetListGen {
			return m, nil
		}
		m.targetListCancel = nil
		m.loading = false
		m.err = msg.err
		if msg.err == nil {
			m.targetMigrations = msg.migrations
			m.cursor = 0
		}
	case targetDetailMsg:
		m.loading = false
		m.err = msg.err
		if msg.err == nil {
			m.targetDetail = msg.migration
			if len(msg.migration.Repositories) > 0 {
				m.repository = msg.migration.Repositories[0]
			}
			m.clampActionFocus()
		}
	case configMsg:
		if msg.generation != m.configGeneration {
			return m, nil
		}
		m.configurationErr = msg.err
		if m.screen == screenConfiguration {
			m.loading = false
			m.err = msg.err
		}
		if msg.err == nil {
			m.configuration = msg.configuration
			m.sourceAuthChecked = false
			m.sourceAuthErr = nil
			m.targetAuthChecked = false
			m.targetAuthErr = nil
			sourceURL, sourceTokenSet := effectiveSourceConfiguration(msg.configuration)
			targetURL, targetTokenSet := effectiveTargetConfiguration(msg.configuration)
			var commands []tea.Cmd
			if validHTTPURL(sourceURL) && sourceTokenSet {
				commands = append(commands, m.checkSourceAuthenticationCmd(msg.generation))
			}
			if validHTTPURL(targetURL) && targetTokenSet {
				commands = append(commands, m.checkTargetAuthenticationCmd(msg.generation))
			}
			m.syncViewportSize()
			return m, tea.Batch(commands...)
		}
		m.syncViewportSize()
	case sourceAuthenticationMsg:
		if msg.generation != m.configGeneration {
			return m, nil
		}
		m.sourceAuthChecked = true
		m.sourceAuthErr = msg.err
		m.syncViewportSize()
	case targetAuthenticationMsg:
		if msg.generation != m.configGeneration {
			return m, nil
		}
		m.targetAuthChecked = true
		m.targetAuthErr = msg.err
		m.syncViewportSize()
	case pickerCatalogMsg:
		if msg.generation != m.pickerGeneration {
			return m, nil
		}
		m.picker.loading = false
		m.picker.err = msg.err
		if msg.err == nil {
			m.picker.items = msg.items
			m.picker.cursor = 0
		}
	case actionMsg:
		m.loading = false
		m.err = nil
		if msg.err != nil {
			m.result = resultState{title: "Action failed", body: msg.err.Error(), parent: msg.parent}
		} else {
			m.result = resultState{title: msg.title, body: msg.body, parent: msg.parent, refresh: msg.refresh}
		}
		m.screen = screenResult
		m.cursor = 0
		m.resetViewport()
	case confirmRequestMsg:
		m.loading = false
		m.confirm = confirmState{
			title:   msg.title,
			body:    msg.body,
			parent:  msg.parent,
			command: msg.command,
		}
		m.screen = screenConfirm
	case watchTickMsg:
		if m.sourceWatching && m.screen == screenSourceDetail {
			m.loading = true
			command := m.loadSourceDetailCmd()
			return m, command
		}
	}
	return m, nil
}

func (m *Model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.screen {
	case screenPicker:
		return m.updatePicker(msg)
	case screenForm:
		return m.updateForm(msg)
	case screenConfirm:
		return m.updateConfirm(msg)
	case screenResult:
		return m.updateResult(msg)
	}
	if m.showHelp {
		switch {
		case key.Matches(msg, keys.Help), key.Matches(msg, keys.Back):
			m.showHelp = false
			return m, nil
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, keys.Up), key.Matches(msg, keys.Down),
			key.Matches(msg, keys.PageUp), key.Matches(msg, keys.PageDown):
			return m.updateViewport(msg)
		default:
			return m, nil
		}
	}
	if m.sourceSearch && m.screen == screenSourceList {
		return m.updateSourceSearch(msg)
	}

	switch {
	case m.actionScreen() && m.activateActionShortcut(msg.String()):
		return m.activate()
	case key.Matches(msg, keys.Help):
		m.showHelp = !m.showHelp
		return m, nil
	case key.Matches(msg, keys.Quit):
		if m.screen == screenHome {
			return m, tea.Quit
		}
		return m.back()
	case key.Matches(msg, keys.Back):
		if m.screen != screenHome {
			return m.back()
		}
	case key.Matches(msg, keys.Left):
		if m.actionScreen() && m.actionFocus > 0 {
			m.actionFocus--
		}
	case key.Matches(msg, keys.Right):
		if m.actionScreen() && m.actionFocus < m.itemCount()-1 {
			m.actionFocus++
		}
	case key.Matches(msg, keys.Up):
		if !m.actionScreen() && m.cursor > 0 {
			m.cursor--
		}
	case key.Matches(msg, keys.Down):
		if !m.actionScreen() && m.cursor < m.itemCount()-1 {
			m.cursor++
		}
	case key.Matches(msg, keys.Refresh):
		return m.refresh()
	case key.Matches(msg, keys.Open):
		return m.activate()
	case key.Matches(msg, keys.New):
		if m.screen == screenSourceList {
			return m.openSourceCreateForm(screenSourceList)
		}
		if m.screen == screenTargetList {
			return m.openTargetCreateForm()
		}
	case key.Matches(msg, keys.Manual):
		if m.screen == screenSourceList {
			return m.openSourceIDForm()
		}
		if m.screen == screenTargetList {
			return m.openTargetIDForm()
		}
	case key.Matches(msg, keys.Search):
		if m.screen == screenSourceList {
			m.sourceSearch = true
			m.searchInput.Focus()
			m.cursor = 0
			return m, textinput.Blink
		}
	case key.Matches(msg, keys.Density):
		if m.screen == screenSourceList {
			m.densityUserSet = true
			m.compact = !m.compact
		}
	case key.Matches(msg, keys.PageUp), key.Matches(msg, keys.PageDown):
		if m.scrollableScreen() {
			return m.updateViewport(msg)
		}
	}
	return m, nil
}

func (m *Model) updateSourceSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		if m.searchInput.Value() != "" {
			m.searchInput.SetValue("")
			m.cursor = 0
			return m, nil
		}
		m.sourceSearch = false
		m.searchInput.Blur()
		return m, nil
	case "enter":
		if len(m.visibleSourceMigrations()) > 0 {
			m.sourceSearch = false
			m.searchInput.Blur()
			return m.activate()
		}
		return m, nil
	case "up":
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil
	case "down":
		if m.cursor < len(m.visibleSourceMigrations())-1 {
			m.cursor++
		}
		return m, nil
	}
	var command tea.Cmd
	m.searchInput, command = m.searchInput.Update(msg)
	if m.cursor >= len(m.visibleSourceMigrations()) {
		m.cursor = max(0, len(m.visibleSourceMigrations())-1)
	}
	return m, command
}

func (m *Model) updatePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.pickerInfoOpen {
		switch msg.String() {
		case "esc", "enter", "?", "q":
			m.pickerInfoOpen = false
		}
		return m, nil
	}
	switch msg.String() {
	case "ctrl+e":
		source := ""
		if m.picker.kind == pickerTargetOrganization {
			source = m.picker.source
		}
		return m.openManualSourceCreateForm(m.picker.parent, source)
	case "ctrl+r":
		return m.reloadPicker()
	case "?":
		items := m.visiblePickerItems()
		if m.picker.kind == pickerSourceRepository &&
			m.picker.cursor >= 0 && m.picker.cursor < len(items) &&
			items[m.picker.cursor].repository != nil {
			m.pickerInfoOpen = true
		}
		return m, nil
	case "esc":
		if m.picker.input.Value() != "" {
			m.picker.input.SetValue("")
			m.picker.cursor = 0
			return m, nil
		}
		if m.picker.kind == pickerTargetOrganization {
			return m.openSourceCreateForm(m.picker.parent)
		}
		m.picker.input.Blur()
		m.screen = m.picker.parent
		return m, nil
	case "enter":
		if m.picker.loading || m.picker.err != nil {
			return m, nil
		}
		items := m.visiblePickerItems()
		if len(items) == 0 {
			return m, nil
		}
		selected := items[m.picker.cursor].value
		if m.picker.kind == pickerSourceRepository {
			return m.openTargetOrganizationPicker(m.picker.parent, selected)
		}
		return m.openDiscoveredSourceCreateForm(m.picker.source, selected)
	case "up":
		if m.picker.cursor > 0 {
			m.picker.cursor--
		}
		return m, nil
	case "down":
		if m.picker.cursor < len(m.visiblePickerItems())-1 {
			m.picker.cursor++
		}
		return m, nil
	}
	if m.picker.loading || m.picker.err != nil {
		return m, nil
	}
	var command tea.Cmd
	m.picker.input, command = m.picker.input.Update(msg)
	if m.picker.cursor >= len(m.visiblePickerItems()) {
		m.picker.cursor = max(0, len(m.visiblePickerItems())-1)
	}
	return m, command
}

func (m *Model) visiblePickerItems() []pickerItem {
	query := strings.ToLower(strings.TrimSpace(m.picker.input.Value()))
	if query == "" {
		return m.picker.items
	}
	items := make([]pickerItem, 0, len(m.picker.items))
	for _, item := range m.picker.items {
		if strings.Contains(strings.ToLower(item.value), query) {
			items = append(items, item)
		}
	}
	return items
}

func (m *Model) activate() (tea.Model, tea.Cmd) {
	switch m.screen {
	case screenHome:
		switch m.cursor {
		case 0:
			m.screen, m.err = screenSourceList, m.sourceListErr
			switch {
			case m.sourceListLoading:
				m.loading = true
				return m, nil
			case m.sourceListLoaded:
				m.loading = false
				return m, nil
			default:
				m.loading = true
				m.sourceListLoading = true
				command := m.loadSourceListCmd()
				return m, command
			}
		case 1:
			return m.openSourceCreateForm(screenHome)
		case 2:
			m.screen, m.actionFocus = screenMannequins, 0
		case 3:
			m.screen, m.loading, m.err = screenConfiguration, true, nil
			m.actionFocus = 0
			m.resetViewport()
			command := m.startConfigurationLoad()
			return m, command
		case 4:
			m.screen, m.loading, m.err = screenTargetList, true, nil
			command := m.startTargetListLoad()
			return m, command
		case 5:
			return m, tea.Quit
		}
	case screenSourceList:
		migrations := m.visibleSourceMigrations()
		if len(migrations) == 0 {
			return m.openSourceCreateForm(screenSourceList)
		}
		m.sourceID = workflow.SourceMigrationID(migrations[m.cursor].MigrationID)
		m.targetID = 0
		m.screen, m.loading, m.err = screenSourceDetail, true, nil
		m.actionFocus = 0
		m.resetViewport()
		command := m.loadSourceDetailCmd()
		return m, command
	case screenSourceDetail:
		return m.activateSourceAction()
	case screenTargetList:
		if len(m.targetMigrations) == 0 {
			return m.openTargetCreateForm()
		}
		id, err := workflow.ParseTargetMigrationID(m.targetMigrations[m.cursor].MigrationID)
		if err != nil {
			m.err = err
			return m, nil
		}
		m.targetID = id
		m.targetParent = screenTargetList
		m.screen, m.loading, m.err = screenTargetDetail, true, nil
		m.actionFocus = 0
		m.resetViewport()
		command := m.loadTargetDetailCmd()
		return m, command
	case screenTargetDetail:
		return m.activateTargetAction()
	case screenMannequins:
		return m.activateMannequinAction()
	case screenConfiguration:
		return m.activateConfigurationAction()
	}
	return m, nil
}

func (m *Model) back() (tea.Model, tea.Cmd) {
	m.err = nil
	m.cursor = 0
	m.actionFocus = 0
	switch m.screen {
	case screenSourceList:
		m.sourceSearch = false
		m.searchInput.Blur()
		m.searchInput.SetValue("")
		m.screen = screenHome
	case screenTargetList, screenMannequins, screenConfiguration, screenHome:
		m.screen = screenHome
	case screenSourceDetail:
		m.sourceWatching = false
		m.screen = screenSourceList
	case screenTargetDetail:
		m.screen = m.targetParent
	}
	return m, nil
}

func (m *Model) refresh() (tea.Model, tea.Cmd) {
	m.err = nil
	switch m.screen {
	case screenSourceList:
		m.loading = true
		m.sourceListLoading = true
		command := m.loadSourceListCmd()
		return m, command
	case screenSourceDetail:
		m.loading = true
		command := m.loadSourceDetailCmd()
		return m, command
	case screenTargetList:
		m.loading = true
		command := m.startTargetListLoad()
		return m, command
	case screenTargetDetail:
		m.loading = true
		command := m.loadTargetDetailCmd()
		return m, command
	case screenConfiguration:
		m.loading = true
		command := m.startConfigurationLoad()
		return m, command
	}
	return m, nil
}

func (m *Model) itemCount() int {
	switch m.screen {
	case screenHome:
		return 6
	case screenSourceList:
		return len(m.visibleSourceMigrations())
	case screenSourceDetail:
		return len(m.sourceActionItems())
	case screenTargetList:
		return len(m.targetMigrations)
	case screenTargetDetail:
		return len(m.targetActionItems())
	case screenMannequins:
		return len(mannequinActions)
	case screenConfiguration:
		return len(configurationActions)
	default:
		return 0
	}
}

func (m *Model) actionScreen() bool {
	switch m.screen {
	case screenSourceDetail, screenTargetDetail, screenMannequins, screenConfiguration:
		return true
	default:
		return false
	}
}

func (m *Model) clampActionFocus() {
	m.actionFocus = min(max(0, m.actionFocus), max(0, m.itemCount()-1))
}

type actionItem struct {
	id       string
	label    string
	shortcut string
}

func (m *Model) activateSourceAction() (tea.Model, tea.Cmd) {
	actions := m.sourceActionItems()
	if m.actionFocus < 0 || m.actionFocus >= len(actions) {
		return m, nil
	}
	switch actions[m.actionFocus].id {
	case "refresh":
		return m.refresh()
	case "watch":
		m.sourceWatching = !m.sourceWatching
		if m.sourceWatching {
			m.loading = true
			command := m.loadSourceDetailCmd()
			return m, command
		}
	case "start":
		return m.confirmAction("Start migration", "Start this migration?", screenSourceDetail,
			m.sourceMutationCmd("Migration started", m.service.StartSourceMigration))
	case "pause":
		return m.confirmAction("Pause migration", "Pause this migration?", screenSourceDetail,
			m.sourceMutationCmd("Migration paused", m.service.PauseSourceMigration))
	case "resume":
		return m.confirmAction("Resume migration", "Resume this migration?", screenSourceDetail,
			m.sourceMutationCmd("Migration resumed", m.service.ResumeSourceMigration))
	case "cancel":
		return m.confirmAction("Cancel migration", "This permanently terminates the source migration and cannot be undone.", screenSourceDetail,
			m.sourceMutationCmd("Migration cancelled", m.service.CancelSourceMigration))
	case "cutover":
		return m.confirmAction("Initiate cutover", "Archive the source repository and initiate cutover?", screenSourceDetail,
			m.cutoverCmd(false))
	case "force-cutover":
		return m.confirmAction("Force cutover", "Bypass readiness checks and force cutover?", screenSourceDetail,
			m.cutoverCmd(true))
	case "cutover-status":
		body := "No combined cutover state is available."
		if m.sourceDetail != nil {
			body = render.CutoverStatus(*m.sourceDetail)
		}
		m.result = resultState{title: "Cutover status", body: body, parent: screenSourceDetail}
		m.screen = screenResult
		m.resetViewport()
	case "revert":
		return m.confirmAction("Revert cutover", "Revert cutover effects and terminate work still in progress?", screenSourceDetail,
			m.revertCutoverCmd())
	case "destination":
		if m.targetID <= 0 {
			m.err = errors.New("this source migration does not expose a target migration ID yet")
			return m, nil
		}
		m.screen, m.loading, m.err = screenTargetDetail, true, nil
		m.targetParent = screenSourceDetail
		m.actionFocus = 0
		m.resetViewport()
		command := m.loadTargetDetailCmd()
		return m, command
	}
	return m, nil
}

func (m *Model) sourceActionItems() []actionItem {
	actions := []actionItem{
		{id: "refresh", label: "Refresh status", shortcut: "r"},
		{id: "watch", label: watchLabel(m.sourceWatching), shortcut: "w"},
	}

	status := ""
	if m.sourceDetail != nil && m.sourceDetail.Migration != nil && m.sourceDetail.Migration.Status != nil {
		status = normalizedStatus(*m.sourceDetail.Migration.Status)
	}
	readyForCutover := m.sourceDetail != nil &&
		m.sourceDetail.CombinedState != nil &&
		m.sourceDetail.CombinedState.ReadyForCutover

	switch status {
	case "created":
		actions = append(actions,
			actionItem{id: "start", label: "Start migration", shortcut: "s"},
			actionItem{id: "cancel", label: "Cancel migration", shortcut: "x"},
		)
	case "queued", "in progress":
		actions = append(actions, actionItem{id: "pause", label: "Pause migration", shortcut: "p"})
		if readyForCutover {
			actions = append(actions, actionItem{id: "cutover", label: "Initiate cutover", shortcut: "c"})
		} else {
			actions = append(actions, actionItem{id: "force-cutover", label: "Force cutover", shortcut: "c"})
		}
		actions = append(actions, actionItem{id: "cancel", label: "Cancel migration", shortcut: "x"})
	case "paused":
		actions = append(actions,
			actionItem{id: "resume", label: "Resume migration", shortcut: "u"},
			actionItem{id: "cancel", label: "Cancel migration", shortcut: "x"},
		)
	case "completed":
		actions = append(actions, actionItem{id: "revert", label: "Revert cutover", shortcut: "v"})
	}
	if m.sourceDetail != nil && m.sourceDetail.CombinedState != nil {
		actions = append(actions, actionItem{id: "cutover-status", label: "Show cutover status", shortcut: "i"})
	}
	if m.targetID > 0 {
		actions = append(actions, actionItem{id: "destination", label: "Open destination details", shortcut: "d"})
	}
	return actions
}

func (m *Model) activateTargetAction() (tea.Model, tea.Cmd) {
	actions := m.targetActionItems()
	if m.actionFocus < 0 || m.actionFocus >= len(actions) {
		return m, nil
	}
	switch actions[m.actionFocus].id {
	case "refresh":
		return m.refresh()
	case "resources":
		return m.openResourcesForm()
	case "report-request":
		return m.openReportForm("Request report", "request")
	case "report-status":
		return m.openReportForm("Report status", "status")
	case "report-url":
		return m.openReportForm("Report URL", "url")
	case "pause":
		return m.confirmAction("Pause target migration", "Pause the target migration?", screenTargetDetail,
			m.targetMutationCmd("Target migration paused", m.service.PauseTargetMigration))
	case "resume":
		return m.confirmAction("Resume target migration", "Resume the target migration?", screenTargetDetail,
			m.targetMutationCmd("Target migration resumed", m.service.ResumeTargetMigration))
	case "abort":
		return m.confirmAction("Abort target migration", "This permanently aborts the target migration and cannot be undone.", screenTargetDetail,
			m.targetMutationCmd("Target migration aborted", m.service.AbortTargetMigration))
	}
	return m, nil
}

func (m *Model) targetActionItems() []actionItem {
	actions := []actionItem{
		{id: "refresh", label: "Refresh status", shortcut: "r"},
		{id: "resources", label: "List repository resources", shortcut: "o"},
		{id: "report-request", label: "Request node report", shortcut: "n"},
		{id: "report-status", label: "Check report status", shortcut: "s"},
		{id: "report-url", label: "Get report download URL", shortcut: "u"},
	}
	status := ""
	if m.targetDetail != nil {
		status = normalizedStatus(m.targetDetail.Status)
	}
	switch status {
	case "in progress":
		actions = append(actions,
			actionItem{id: "pause", label: "Pause destination migration", shortcut: "p"},
			actionItem{id: "abort", label: "Abort destination migration", shortcut: "x"},
		)
	case "paused":
		actions = append(actions,
			actionItem{id: "resume", label: "Resume destination migration", shortcut: "m"},
			actionItem{id: "abort", label: "Abort destination migration", shortcut: "x"},
		)
	}
	return actions
}

func watchLabel(watching bool) string {
	if watching {
		return "Stop live watch"
	}
	return "Start live watch"
}

func normalizedStatus(status string) string {
	status = strings.TrimPrefix(status, "STATUS_TYPE_")
	status = strings.ReplaceAll(status, "_", " ")
	return strings.ToLower(strings.TrimSpace(status))
}

var mannequinActions = []actionItem{
	{id: "list", label: "List mannequins", shortcut: "a"},
	{id: "export", label: "Export mannequins to CSV", shortcut: "e"},
	{id: "reclaim", label: "Reclaim a mannequin", shortcut: "r"},
	{id: "reclaim-csv", label: "Reclaim mannequins from CSV", shortcut: "c"},
}

func (m *Model) activateMannequinAction() (tea.Model, tea.Cmd) {
	switch m.actionFocus {
	case 0:
		return m.openMannequinListForm(false)
	case 1:
		return m.openMannequinListForm(true)
	case 2:
		return m.openMannequinReclaimForm(false)
	case 3:
		return m.openMannequinReclaimForm(true)
	}
	return m, nil
}

var configurationActions = []actionItem{
	{id: "refresh", label: "Refresh configuration", shortcut: "r"},
	{id: "edit", label: "Edit configuration", shortcut: "e"},
	{id: "reset", label: "Reset configuration", shortcut: "x"},
}

func (m *Model) activateConfigurationAction() (tea.Model, tea.Cmd) {
	switch m.actionFocus {
	case 0:
		return m.refresh()
	case 1:
		return m.openConfigurationForm()
	case 2:
		return m.confirmAction("Reset configuration", "Remove all stored endpoint URLs and credentials?", screenConfiguration,
			func() tea.Msg {
				err := m.service.ResetConfiguration(m.ctx)
				return actionMsg{title: "Configuration reset", body: "Stored configuration and credentials were cleared.", parent: screenConfiguration, refresh: true, err: err}
			})
	}
	return m, nil
}

func (m *Model) activateActionShortcut(value string) bool {
	if len(value) != 1 {
		return false
	}
	var actions []actionItem
	switch m.screen {
	case screenSourceDetail:
		actions = m.sourceActionItems()
	case screenTargetDetail:
		actions = m.targetActionItems()
	case screenMannequins:
		actions = mannequinActions
	case screenConfiguration:
		actions = configurationActions
	}
	for index, action := range actions {
		if strings.EqualFold(value, action.shortcut) {
			m.actionFocus = index
			return true
		}
	}
	return false
}

func (m *Model) confirmAction(title, body string, parent screen, command tea.Cmd) (tea.Model, tea.Cmd) {
	m.confirm = confirmState{title: title, body: body, parent: parent, command: command, focus: 0}
	m.screen = screenConfirm
	return m, nil
}

func (m *Model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Left):
		m.confirm.focus = max(0, m.confirm.focus-1)
	case key.Matches(msg, keys.Right):
		m.confirm.focus = min(1, m.confirm.focus+1)
	case msg.String() == "y" || msg.String() == "Y":
		m.screen = m.confirm.parent
		m.loading = true
		return m, m.confirm.command
	case msg.String() == "n" || msg.String() == "N" || msg.String() == "esc" || msg.String() == "q":
		m.screen = m.confirm.parent
	case key.Matches(msg, keys.Open):
		m.screen = m.confirm.parent
		if m.confirm.focus == 1 {
			return m, nil
		}
		m.loading = true
		return m, m.confirm.command
	}
	return m, nil
}

func (m *Model) updateResult(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Up), key.Matches(msg, keys.Down),
		key.Matches(msg, keys.PageUp), key.Matches(msg, keys.PageDown):
		return m.updateViewport(msg)
	case key.Matches(msg, keys.Open), key.Matches(msg, keys.Back), key.Matches(msg, keys.Quit):
		parent := m.result.parent
		refresh := m.result.refresh
		m.screen, m.cursor, m.actionFocus, m.err = parent, 0, 0, nil
		if refresh {
			return m.refresh()
		}
	}
	return m, nil
}

func (m *Model) visibleSourceMigrations() []elmapi.MigrationSummary {
	query := strings.ToLower(strings.TrimSpace(m.searchInput.Value()))
	if query == "" {
		return m.sourceMigrations
	}
	migrations := make([]elmapi.MigrationSummary, 0, len(m.sourceMigrations))
	for _, migration := range m.sourceMigrations {
		haystack := strings.ToLower(strings.Join([]string{
			migration.MigrationID,
			migration.SourceOrganizationLogin,
			migration.SourceRepositoryName,
			migration.TargetOrganizationLogin,
			migration.TargetRepositoryName,
			fmt.Sprintf("%d", migration.TargetMigrationID),
		}, " "))
		if strings.Contains(haystack, query) {
			migrations = append(migrations, migration)
		}
	}
	return migrations
}

func (m *Model) scrollableScreen() bool {
	return isScrollableScreen(m.screen)
}

func isScrollableScreen(current screen) bool {
	switch current {
	case screenSourceDetail, screenTargetDetail, screenConfiguration, screenResult:
		return true
	default:
		return false
	}
}

func (m *Model) updateViewport(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if !m.viewportReady {
		m.viewport = viewport.New(m.contentWidth(), m.bodyHeight())
		m.viewportReady = true
	}
	m.syncViewportSize()
	m.viewport.SetContent(m.scrollableContent())
	var command tea.Cmd
	m.viewport, command = m.viewport.Update(msg)
	return m, command
}

func (m *Model) syncViewportSize() {
	if !m.viewportReady {
		return
	}
	m.viewport.Width = m.contentWidth()
	m.viewport.Height = m.bodyHeight()
}

func (m *Model) resetViewport() {
	if m.viewportReady {
		m.viewport.GotoTop()
	}
}

func (m *Model) scrollableContent() string {
	if m.showHelp {
		return m.fullHelpView()
	}
	switch m.screen {
	case screenSourceDetail:
		return m.sourceDetailView()
	case screenTargetDetail:
		return m.targetDetailView()
	case screenConfiguration:
		return m.configurationView()
	case screenResult:
		return m.result.body
	default:
		return ""
	}
}

func (m *Model) openForm(form formState) (tea.Model, tea.Cmd) {
	m.form = form
	m.form.err = nil
	m.screen = screenForm
	return m, nil
}

func (m *Model) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	field := &m.form.fields[m.form.cursor]
	if msg.Type == tea.KeyRunes {
		if field.kind == fieldText || field.kind == fieldSecret {
			field.value += string(msg.Runes)
			return m, nil
		}
	}
	switch msg.String() {
	case "esc":
		m.screen = m.form.parent
	case "up", "shift+tab":
		if m.form.cursor > 0 {
			m.form.cursor--
		}
	case "down", "tab":
		if m.form.cursor < len(m.form.fields)-1 {
			m.form.cursor++
		}
	case "left":
		cycleOption(field, -1)
	case "right", " ":
		if field.kind == fieldBool {
			if field.value == "true" {
				field.value = "false"
			} else {
				field.value = "true"
			}
		} else {
			cycleOption(field, 1)
		}
	case "backspace":
		if (field.kind == fieldText || field.kind == fieldSecret) && field.value != "" {
			runes := []rune(field.value)
			field.value = string(runes[:len(runes)-1])
		}
	case "enter":
		if m.form.cursor < len(m.form.fields)-1 {
			m.form.cursor++
			return m, nil
		}
		values := make(map[string]string, len(m.form.fields))
		for _, f := range m.form.fields {
			values[f.key] = f.value
		}
		command, err := m.form.submit(values)
		if err != nil {
			m.form.err = err
			return m, nil
		}
		m.screen = m.form.parent
		m.loading = true
		return m, command
	}
	return m, nil
}

func cycleOption(field *formField, delta int) {
	if field.kind != fieldSelect || len(field.options) == 0 {
		return
	}
	index := 0
	for i, option := range field.options {
		if option == field.value {
			index = i
			break
		}
	}
	index = (index + delta + len(field.options)) % len(field.options)
	field.value = field.options[index]
}

func (m *Model) openSourceIDForm() (tea.Model, tea.Cmd) {
	return m.openForm(formState{
		title:  "Open source migration",
		parent: screenSourceList,
		fields: []formField{{key: "id", label: "Source migration UUID", kind: fieldText}},
		submit: func(values map[string]string) (tea.Cmd, error) {
			id := strings.TrimSpace(values["id"])
			if id == "" {
				return nil, errors.New("source migration UUID is required")
			}
			m.sourceID = workflow.SourceMigrationID(id)
			m.form.parent = screenSourceDetail
			m.resetViewport()
			return m.loadSourceDetailCmd(), nil
		},
	})
}

func (m *Model) openSourceCreateForm(parent screen) (tea.Model, tea.Cmd) {
	input := textinput.New()
	input.Prompt = ""
	input.Placeholder = "filter source repositories"
	input.CharLimit = 160
	input.Focus()

	m.pickerGeneration++
	m.pickerInfoOpen = false
	m.picker = pickerState{
		kind:       pickerSourceRepository,
		title:      "Select source repository",
		parent:     parent,
		input:      input,
		loading:    true,
		generation: m.pickerGeneration,
	}
	m.screen = screenPicker
	command := m.loadPickerCatalogCmd(pickerSourceRepository, m.pickerGeneration)
	return m, command
}

func (m *Model) openTargetOrganizationPicker(parent screen, source string) (tea.Model, tea.Cmd) {
	input := textinput.New()
	input.Prompt = ""
	input.Placeholder = "filter destination organizations"
	input.CharLimit = 160
	input.Focus()

	m.pickerGeneration++
	m.pickerInfoOpen = false
	m.picker = pickerState{
		kind:       pickerTargetOrganization,
		title:      "Select destination organization",
		parent:     parent,
		input:      input,
		loading:    true,
		source:     source,
		generation: m.pickerGeneration,
	}
	m.screen = screenPicker
	command := m.loadPickerCatalogCmd(pickerTargetOrganization, m.pickerGeneration)
	return m, command
}

func (m *Model) reloadPicker() (tea.Model, tea.Cmd) {
	m.pickerGeneration++
	m.picker.generation = m.pickerGeneration
	m.picker.loading = true
	m.picker.err = nil
	m.picker.items = nil
	m.picker.cursor = 0
	command := m.loadPickerCatalogCmd(m.picker.kind, m.pickerGeneration)
	return m, command
}

func (m *Model) openDiscoveredSourceCreateForm(source, targetOrganization string) (tea.Model, tea.Cmd) {
	_, sourceRepository, err := workflow.ParseRepositoryCoordinate(source)
	if err != nil {
		m.picker.err = err
		return m, nil
	}
	return m.openForm(formState{
		title:       "Create migration",
		description: fmt.Sprintf("Source: %s\nDestination organization: %s", source, targetOrganization),
		parent:      screenPicker,
		fields: []formField{
			{
				key:         "targetRepo",
				label:       "Destination repository name",
				description: "Defaults to the source repository name; edit it if the destination should differ.",
				kind:        fieldText,
				value:       sourceRepository,
			},
			{key: "visibility", label: "Target visibility", kind: fieldSelect, value: "internal", options: []string{"internal", "private"}},
			{key: "start", label: "Start after creation", kind: fieldBool, value: "false"},
		},
		submit: func(values map[string]string) (tea.Cmd, error) {
			sourceOwner, sourceRepo, err := workflow.ParseRepositoryCoordinate(source)
			if err != nil {
				return nil, fmt.Errorf("invalid source repository: %w", err)
			}
			targetRepo := strings.TrimSpace(values["targetRepo"])
			if targetRepo == "" || strings.Contains(targetRepo, "/") {
				return nil, errors.New("destination repository name must be non-empty and must not contain a slash")
			}
			return m.createSourceMigrationCmd(workflow.SourceCreateInput{
				SourceOwner: sourceOwner,
				SourceRepo:  sourceRepo,
				TargetOwner: targetOrganization,
				TargetRepo:  targetRepo,
				Visibility:  values["visibility"],
				Start:       values["start"] == "true",
			}), nil
		},
	})
}

func (m *Model) openManualSourceCreateForm(parent screen, source string) (tea.Model, tea.Cmd) {
	return m.openForm(formState{
		title:       "Create migration manually",
		description: "Enter repositories directly when API discovery is unavailable.",
		parent:      parent,
		fields: []formField{
			{
				key:         "source",
				label:       "Source repository",
				description: "Format: org/repo (for example, source-org/source-repo)",
				kind:        fieldText,
				value:       source,
			},
			{
				key:         "target",
				label:       "Target repository",
				description: "Format: org/repo (for example, target-org/target-repo)",
				kind:        fieldText,
			},
			{key: "visibility", label: "Target visibility", kind: fieldSelect, value: "internal", options: []string{"internal", "private"}},
			{key: "start", label: "Start after creation", kind: fieldBool, value: "false"},
		},
		submit: func(values map[string]string) (tea.Cmd, error) {
			sourceOwner, sourceRepository, err := workflow.ParseRepositoryCoordinate(values["source"])
			if err != nil {
				return nil, fmt.Errorf("invalid source repository: %w", err)
			}
			targetOwner, targetRepository, err := workflow.ParseRepositoryCoordinate(values["target"])
			if err != nil {
				return nil, fmt.Errorf("invalid target repository: %w", err)
			}
			input := workflow.SourceCreateInput{
				SourceOwner: sourceOwner,
				SourceRepo:  sourceRepository,
				TargetOwner: targetOwner,
				TargetRepo:  targetRepository,
				Visibility:  values["visibility"],
				Start:       values["start"] == "true",
			}
			return m.createSourceMigrationCmd(input), nil
		},
	})
}

func (m *Model) createSourceMigrationCmd(input workflow.SourceCreateInput) tea.Cmd {
	return func() tea.Msg {
		result, err := m.service.CreateSourceMigration(m.ctx, input)
		if err != nil {
			return actionMsg{parent: screenSourceList, err: err}
		}
		m.sourceID = workflow.SourceMigrationID(result.Migration.MigrationID)
		body := render.MigrationCreate(result.Migration)
		if result.Started {
			body = fmt.Sprintf("Migration %s created and started.", result.Migration.MigrationID)
		}
		return actionMsg{title: "Migration created", body: body, parent: screenSourceDetail, refresh: true}
	}
}

func (m *Model) openTargetIDForm() (tea.Model, tea.Cmd) {
	return m.openForm(formState{
		title:  "Open target migration",
		parent: screenTargetList,
		fields: []formField{{key: "id", label: "Numeric target migration ID", kind: fieldText}},
		submit: func(values map[string]string) (tea.Cmd, error) {
			id, err := workflow.ParseTargetMigrationID(values["id"])
			if err != nil {
				return nil, err
			}
			m.targetID = id
			m.targetParent = screenTargetList
			m.form.parent = screenTargetDetail
			m.resetViewport()
			return m.loadTargetDetailCmd(), nil
		},
	})
}

func (m *Model) openTargetCreateForm() (tea.Model, tea.Cmd) {
	return m.openForm(formState{
		title:  "Create target migration (advanced)",
		parent: screenTargetList,
		fields: []formField{
			{key: "sourceURL", label: "Source repository URL", kind: fieldText},
			{key: "repository", label: "Target owner/repository", kind: fieldText},
			{key: "description", label: "Description", kind: fieldText},
			{key: "guid", label: "Exporter migration GUID", kind: fieldText},
		},
		submit: func(values map[string]string) (tea.Cmd, error) {
			input := workflow.TargetCreateInput{
				SourceRepositoryURL: values["sourceURL"],
				Repository:          values["repository"],
				Description:         values["description"],
				ExporterGUID:        values["guid"],
			}
			return func() tea.Msg {
				raw, err := m.service.CreateTargetMigration(m.ctx, input)
				return actionMsg{title: "Target migration created", body: prettyJSON(raw), parent: screenTargetList, refresh: true, err: err}
			}, nil
		},
	})
}

func (m *Model) openResourcesForm() (tea.Model, tea.Cmd) {
	defaultRepo := m.repository
	return m.openForm(formState{
		title:  "List target resources",
		parent: screenTargetDetail,
		fields: []formField{
			{key: "repository", label: "Repository owner/name", kind: fieldText, value: defaultRepo},
			{key: "origin", label: "Origin", kind: fieldSelect, value: "all", options: []string{"all", "backfill", "live-update"}},
			{key: "state", label: "State", kind: fieldSelect, value: "all", options: []string{"all", "pending", "processed", "failed", "eligible"}},
			{key: "max", label: "Maximum results (0 = all)", kind: fieldText, value: "100"},
		},
		submit: func(values map[string]string) (tea.Cmd, error) {
			maxResults, err := strconv.Atoi(strings.TrimSpace(values["max"]))
			if err != nil || maxResults < 0 {
				return nil, errors.New("maximum results must be zero or a positive integer")
			}
			origin := values["origin"]
			if origin == "all" {
				origin = ""
			}
			state := values["state"]
			if state == "all" {
				state = ""
			}
			input := workflow.ResourceInput{
				MigrationID: m.targetID,
				Repository:  values["repository"],
				Origin:      origin,
				State:       state,
				MaxResults:  maxResults,
			}
			m.repository = strings.TrimSpace(values["repository"])
			return func() tea.Msg {
				nodes, err := m.service.ListResources(m.ctx, input)
				return actionMsg{title: "Target resources", body: renderNodes(nodes), parent: screenTargetDetail, err: err}
			}, nil
		},
	})
}

func (m *Model) openReportForm(title, operation string) (tea.Model, tea.Cmd) {
	fields := []formField{
		{key: "stage", label: "Stage", kind: fieldSelect, value: "backfill", options: []string{"backfill", "live-update"}},
	}
	if operation == "request" {
		fields = append(fields, formField{key: "state", label: "Node state", kind: fieldSelect, value: "all", options: []string{"all", "migrated", "unmigrated"}})
	}
	return m.openForm(formState{
		title:  title,
		parent: screenTargetDetail,
		fields: fields,
		submit: func(values map[string]string) (tea.Cmd, error) {
			input := workflow.ReportInput{MigrationID: m.targetID, Stage: values["stage"], State: values["state"]}
			return func() tea.Msg {
				var (
					raw json.RawMessage
					err error
				)
				switch operation {
				case "request":
					raw, err = m.service.RequestReport(m.ctx, input)
				case "status":
					raw, err = m.service.ReportStatus(m.ctx, input)
				case "url":
					raw, err = m.service.ReportURL(m.ctx, input)
				}
				return actionMsg{title: title, body: prettyJSON(raw), parent: screenTargetDetail, err: err}
			}, nil
		},
	})
}

func (m *Model) openMannequinListForm(export bool) (tea.Model, tea.Cmd) {
	fields := []formField{
		{key: "org", label: "Target organization", kind: fieldText},
		{key: "include", label: "Include reclaimed mannequins", kind: fieldBool, value: "false"},
	}
	if export {
		fields = append(fields, formField{key: "path", label: "CSV output path", kind: fieldText, value: "mannequins.csv"})
	}
	title := "List mannequins"
	if export {
		title = "Export mannequins"
	}
	return m.openForm(formState{
		title:  title,
		parent: screenMannequins,
		fields: fields,
		submit: func(values map[string]string) (tea.Cmd, error) {
			org := values["org"]
			include := values["include"] == "true"
			return func() tea.Msg {
				if export {
					err := m.service.ExportMannequins(m.ctx, org, values["path"], include)
					return actionMsg{title: "Mannequins exported", body: fmt.Sprintf("Wrote mannequin CSV to %s.", values["path"]), parent: screenMannequins, err: err}
				}
				records, err := m.service.ListMannequins(m.ctx, org, include)
				var buffer bytes.Buffer
				if err == nil {
					err = workflow.WriteMannequinCSV(&buffer, records)
				}
				return actionMsg{title: "Mannequins", body: buffer.String(), parent: screenMannequins, err: err}
			}, nil
		},
	})
}

func (m *Model) openMannequinReclaimForm(csvMode bool) (tea.Model, tea.Cmd) {
	fields := []formField{{key: "org", label: "Target organization", kind: fieldText}}
	if csvMode {
		fields = append(fields, formField{key: "csv", label: "Mannequin CSV path", kind: fieldText})
	} else {
		fields = append(fields,
			formField{key: "mannequin", label: "Mannequin login", kind: fieldText},
			formField{key: "mannequinID", label: "Mannequin ID (optional)", kind: fieldText},
			formField{key: "target", label: "Target user or app[bot]", kind: fieldText},
		)
	}
	fields = append(fields,
		formField{key: "force", label: "Force already-reclaimed mannequins", kind: fieldBool, value: "false"},
		formField{key: "skip", label: "Immediate reattribution (EMU)", kind: fieldBool, value: "false"},
	)
	return m.openForm(formState{
		title:  "Reclaim mannequins",
		parent: screenMannequins,
		fields: fields,
		submit: func(values map[string]string) (tea.Cmd, error) {
			input := workflow.MannequinReclaimInput{
				Organization:   values["org"],
				CSVPath:        values["csv"],
				Mannequin:      values["mannequin"],
				MannequinID:    values["mannequinID"],
				TargetUser:     values["target"],
				Force:          values["force"] == "true",
				SkipInvitation: values["skip"] == "true",
			}
			if strings.HasSuffix(input.TargetUser, "[bot]") {
				input.SkipInvitation = true
			}
			action := func() tea.Msg {
				log := &bufferLogger{}
				err := m.service.ReclaimMannequins(m.ctx, input, log)
				body := log.String()
				if body == "" && err == nil {
					body = "Mannequin reclaim completed."
				}
				return actionMsg{title: "Mannequin reclaim", body: body, parent: screenMannequins, err: err}
			}
			confirmation := "Send reclaim invitations for the selected mannequin data?"
			if csvMode {
				confirmation = "Reclaim mannequins from this CSV? Rows targeting app[bot] accounts are reattributed immediately and cannot be undone."
			}
			if input.SkipInvitation || strings.HasSuffix(input.TargetUser, "[bot]") {
				confirmation = "This immediately reattributes mannequin content and cannot be undone."
			}
			return func() tea.Msg {
				return confirmRequestMsg{
					title:   "Confirm mannequin reclaim",
					body:    confirmation,
					parent:  screenMannequins,
					command: action,
				}
			}, nil
		},
	})
}

func (m *Model) openConfigurationForm() (tea.Model, tea.Cmd) {
	sourceURL, targetURL := "", ""
	if m.configuration != nil {
		sourceURL = m.configuration.SourceURL
		targetURL = m.configuration.TargetURL
	}
	return m.openForm(formState{
		title:  "Edit configuration",
		parent: screenConfiguration,
		fields: []formField{
			{key: "sourceURL", label: "Source URL", kind: fieldText, value: sourceURL},
			{key: "sourceToken", label: "Source token (blank preserves current)", kind: fieldSecret},
			{key: "targetURL", label: "Target URL", kind: fieldText, value: targetURL},
			{key: "targetToken", label: "Target token (blank preserves current)", kind: fieldSecret},
		},
		submit: func(values map[string]string) (tea.Cmd, error) {
			input := workflow.ConfigurationInput{
				SourceURL:   values["sourceURL"],
				SourceToken: values["sourceToken"],
				TargetURL:   values["targetURL"],
				TargetToken: values["targetToken"],
			}
			return func() tea.Msg {
				err := m.service.SaveConfiguration(m.ctx, input)
				return actionMsg{title: "Configuration saved", body: "Stored gh elm configuration.", parent: screenConfiguration, refresh: true, err: err}
			}, nil
		},
	})
}

func (m *Model) sourceMutationCmd(title string, action func(context.Context, workflow.SourceMigrationID) error) tea.Cmd {
	id := m.sourceID
	return func() tea.Msg {
		err := action(m.ctx, id)
		return actionMsg{title: title, body: fmt.Sprintf("%s (%s).", title, id), parent: screenSourceDetail, refresh: true, err: err}
	}
}

func (m *Model) targetMutationCmd(title string, action func(context.Context, workflow.TargetMigrationID) error) tea.Cmd {
	id := m.targetID
	return func() tea.Msg {
		err := action(m.ctx, id)
		return actionMsg{title: title, body: fmt.Sprintf("%s (%d).", title, id), parent: screenTargetDetail, refresh: true, err: err}
	}
}

func (m *Model) cutoverCmd(force bool) tea.Cmd {
	id := m.sourceID
	return func() tea.Msg {
		err := m.service.CutoverSourceMigration(m.ctx, id, force)
		return actionMsg{title: "Cutover initiated", body: fmt.Sprintf("Cutover initiated for migration %s.", id), parent: screenSourceDetail, refresh: true, err: err}
	}
}

func (m *Model) revertCutoverCmd() tea.Cmd {
	id := m.sourceID
	return func() tea.Msg {
		result, err := m.service.RevertSourceCutover(m.ctx, id)
		body := ""
		if result != nil {
			body = render.MigrationRevertCutover(*result)
		}
		return actionMsg{title: "Cutover reverted", body: body, parent: screenSourceDetail, refresh: true, err: err}
	}
}

type sourceListMsg struct {
	migrations []elmapi.MigrationSummary
	err        error
}

type sourceDetailMsg struct {
	detail *elmapi.MigrationDetail
	err    error
}

type targetListMsg struct {
	migrations []elmapi.TargetMigration
	generation uint64
	err        error
}

type targetDetailMsg struct {
	migration *elmapi.TargetMigration
	err       error
}

type configMsg struct {
	configuration *workflow.Configuration
	generation    uint64
	err           error
}

type sourceAuthenticationMsg struct {
	generation uint64
	err        error
}

type targetAuthenticationMsg struct {
	generation uint64
	err        error
}

type pickerCatalogMsg struct {
	generation uint64
	items      []pickerItem
	err        error
}

type actionMsg struct {
	title   string
	body    string
	parent  screen
	refresh bool
	err     error
}

type confirmRequestMsg struct {
	title   string
	body    string
	parent  screen
	command tea.Cmd
}

type watchTickMsg struct{}

func (m *Model) loadSourceListCmd() tea.Cmd {
	return func() tea.Msg {
		migrations, err := m.service.ListSourceMigrations(m.ctx, "")
		return sourceListMsg{migrations: migrations, err: err}
	}
}

func (m *Model) loadSourceDetailCmd() tea.Cmd {
	id := m.sourceID
	return func() tea.Msg {
		detail, err := m.service.GetSourceMigration(m.ctx, id)
		return sourceDetailMsg{detail: detail, err: err}
	}
}

func (m *Model) startTargetListLoad() tea.Cmd {
	m.cancelTargetListLoad()
	ctx, cancel := context.WithCancel(m.ctx)
	m.targetListCancel = cancel
	generation := m.targetListGen
	return func() tea.Msg {
		migrations, err := m.service.ListTargetMigrations(ctx, "", 0)
		return targetListMsg{migrations: migrations, generation: generation, err: err}
	}
}

func (m *Model) cancelTargetListLoad() {
	if m.targetListCancel == nil {
		return
	}
	m.targetListCancel()
	m.targetListCancel = nil
	m.targetListGen++
	m.loading = false
}

func (m *Model) loadTargetDetailCmd() tea.Cmd {
	id := m.targetID
	return func() tea.Msg {
		migration, err := m.service.GetTargetMigration(m.ctx, id)
		return targetDetailMsg{migration: migration, err: err}
	}
}

func (m *Model) startConfigurationLoad() tea.Cmd {
	m.configGeneration++
	generation := m.configGeneration
	return func() tea.Msg {
		configuration, err := m.service.GetConfiguration(m.ctx)
		return configMsg{configuration: configuration, generation: generation, err: err}
	}
}

func (m *Model) checkSourceAuthenticationCmd(generation uint64) tea.Cmd {
	return func() tea.Msg {
		return sourceAuthenticationMsg{
			generation: generation,
			err:        m.service.CheckSourceAuthentication(m.ctx),
		}
	}
}

func (m *Model) checkTargetAuthenticationCmd(generation uint64) tea.Cmd {
	return func() tea.Msg {
		return targetAuthenticationMsg{
			generation: generation,
			err:        m.service.CheckTargetAuthentication(m.ctx),
		}
	}
}

func (m *Model) loadPickerCatalogCmd(kind pickerKind, generation uint64) tea.Cmd {
	return func() tea.Msg {
		var items []pickerItem
		var err error
		switch kind {
		case pickerSourceRepository:
			var repositories []elmapi.Repository
			repositories, err = m.service.ListSourceRepositories(m.ctx)
			items = make([]pickerItem, 0, len(repositories))
			for index := range repositories {
				items = append(items, pickerItem{
					value:      repositories[index].FullName,
					repository: &repositories[index],
				})
			}
		case pickerTargetOrganization:
			var organizations []string
			organizations, err = m.service.ListTargetOrganizations(m.ctx)
			items = make([]pickerItem, 0, len(organizations))
			for _, organization := range organizations {
				items = append(items, pickerItem{value: organization})
			}
		}
		return pickerCatalogMsg{generation: generation, items: items, err: err}
	}
}

func prettyJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var buffer bytes.Buffer
	if err := json.Indent(&buffer, raw, "", "  "); err != nil {
		return string(raw)
	}
	return buffer.String()
}

func renderNodes(nodes []elmapi.Node) string {
	if len(nodes) == 0 {
		return "No resources found."
	}
	var buffer strings.Builder
	for _, node := range nodes {
		fmt.Fprintf(&buffer, "%s  %s  %s  %s\n", node.ID, friendly(node.Type), friendly(node.Origin), friendly(node.State))
		if node.Error != "" {
			fmt.Fprintf(&buffer, "  error: %s\n", node.Error)
		}
	}
	return buffer.String()
}

func friendly(value string) string {
	value = strings.TrimPrefix(value, "NODE_ORIGIN_")
	value = strings.TrimPrefix(value, "NODE_STATE_")
	value = strings.TrimPrefix(value, "STATUS_TYPE_")
	return strings.ToLower(strings.ReplaceAll(value, "_", " "))
}

type bufferLogger struct {
	buffer strings.Builder
}

func (l *bufferLogger) Infof(format string, args ...any) {
	fmt.Fprintf(&l.buffer, format+"\n", args...)
}

func (l *bufferLogger) Successf(format string, args ...any) {
	fmt.Fprintf(&l.buffer, format+"\n", args...)
}

func (l *bufferLogger) Warnf(format string, args ...any) {
	fmt.Fprintf(&l.buffer, "warning: "+format+"\n", args...)
}

func (l *bufferLogger) String() string {
	return l.buffer.String()
}
