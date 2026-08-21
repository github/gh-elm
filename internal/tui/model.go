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

type mannequinService interface {
	ListMannequins(context.Context, string, bool) ([]ghapi.MannequinRecord, error)
	ExportMannequins(context.Context, string, string, bool) error
	ReclaimMannequins(context.Context, workflow.MannequinReclaimInput, ghapi.Logger) error
}

type configurationService interface {
	GetConfiguration(context.Context) (*workflow.Configuration, error)
	SaveConfiguration(context.Context, workflow.ConfigurationInput) error
	ResetConfiguration(context.Context) error
}

type service interface {
	sourceService
	targetService
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
	title  string
	fields []formField
	cursor int
	parent screen
	submit func(map[string]string) (tea.Cmd, error)
	err    error
}

type confirmState struct {
	title   string
	body    string
	parent  screen
	command tea.Cmd
}

type resultState struct {
	title   string
	body    string
	parent  screen
	refresh bool
	offset  int
}

// Model is the Bubble Tea application model.
type Model struct {
	ctx     context.Context
	service service
	styles  theme.Styles

	screen  screen
	width   int
	height  int
	cursor  int
	loading bool
	err     error

	sourceMigrations []elmapi.MigrationSummary
	sourceID         workflow.SourceMigrationID
	sourceDetail     *elmapi.MigrationDetail
	sourceWatching   bool

	targetMigrations []elmapi.TargetMigration
	targetID         workflow.TargetMigrationID
	targetDetail     *elmapi.TargetMigration
	targetParent     screen
	repository       string

	configuration    *workflow.Configuration
	configurationErr error
	form             formState
	confirm          confirmState
	result           resultState
}

// New creates the main TUI model.
func New(ctx context.Context, svc service) *Model {
	return &Model{
		ctx:          ctx,
		service:      svc,
		styles:       theme.New(),
		screen:       screenHome,
		targetParent: screenTargetList,
	}
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	return m.loadConfigurationCmd()
}

// Update implements tea.Model.
func (m *Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.loading {
			return m, nil
		}
		return m.updateKey(msg)
	case sourceListMsg:
		m.loading = false
		m.err = msg.err
		if msg.err == nil {
			m.sourceMigrations = msg.migrations
			m.cursor = 0
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
		}
		if m.sourceWatching {
			return m, tea.Tick(2*time.Second, func(time.Time) tea.Msg { return watchTickMsg{} })
		}
	case targetListMsg:
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
		}
	case configMsg:
		m.loading = false
		m.configurationErr = msg.err
		if m.screen == screenConfiguration {
			m.err = msg.err
		}
		if msg.err == nil {
			m.configuration = msg.configuration
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
	case confirmRequestMsg:
		m.loading = false
		m.confirm = confirmState(msg)
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
	case screenForm:
		return m.updateForm(msg)
	case screenConfirm:
		return m.updateConfirm(msg)
	case screenResult:
		return m.updateResult(msg)
	}

	switch msg.String() {
	case "q":
		if m.screen == screenHome {
			return m, tea.Quit
		}
		return m.back()
	case "esc", "backspace":
		if m.screen != screenHome {
			return m.back()
		}
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < m.itemCount()-1 {
			m.cursor++
		}
	case "r":
		return m.refresh()
	case "enter":
		return m.activate()
	case "n":
		if m.screen == screenSourceList {
			return m.openSourceCreateForm(screenSourceList)
		}
		if m.screen == screenTargetList {
			return m.openTargetCreateForm()
		}
	case "m":
		if m.screen == screenSourceList {
			return m.openSourceIDForm()
		}
		if m.screen == screenTargetList {
			return m.openTargetIDForm()
		}
	}
	return m, nil
}

func (m *Model) activate() (tea.Model, tea.Cmd) {
	switch m.screen {
	case screenHome:
		switch m.cursor {
		case 0:
			m.screen, m.loading, m.err = screenSourceList, true, nil
			command := m.loadSourceListCmd()
			return m, command
		case 1:
			return m.openSourceCreateForm(screenHome)
		case 2:
			m.screen, m.cursor = screenMannequins, 0
		case 3:
			m.screen, m.loading, m.err = screenConfiguration, true, nil
			command := m.loadConfigurationCmd()
			return m, command
		case 4:
			m.screen, m.loading, m.err = screenTargetList, true, nil
			command := m.loadTargetListCmd()
			return m, command
		case 5:
			return m, tea.Quit
		}
	case screenSourceList:
		if len(m.sourceMigrations) == 0 {
			return m.openSourceCreateForm(screenSourceList)
		}
		m.sourceID = workflow.SourceMigrationID(m.sourceMigrations[m.cursor].MigrationID)
		m.targetID = 0
		m.screen, m.loading, m.err = screenSourceDetail, true, nil
		m.cursor = 0
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
		m.cursor = 0
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
	switch m.screen {
	case screenSourceList, screenTargetList, screenMannequins, screenConfiguration, screenHome:
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
		command := m.loadSourceListCmd()
		return m, command
	case screenSourceDetail:
		m.loading = true
		command := m.loadSourceDetailCmd()
		return m, command
	case screenTargetList:
		m.loading = true
		command := m.loadTargetListCmd()
		return m, command
	case screenTargetDetail:
		m.loading = true
		command := m.loadTargetDetailCmd()
		return m, command
	case screenConfiguration:
		m.loading = true
		command := m.loadConfigurationCmd()
		return m, command
	}
	return m, nil
}

func (m *Model) itemCount() int {
	switch m.screen {
	case screenHome:
		return 6
	case screenSourceList:
		return len(m.sourceMigrations)
	case screenSourceDetail:
		return len(sourceActions)
	case screenTargetList:
		return len(m.targetMigrations)
	case screenTargetDetail:
		return len(targetActions)
	case screenMannequins:
		return len(mannequinActions)
	case screenConfiguration:
		return len(configurationActions)
	default:
		return 0
	}
}

var sourceActions = []string{
	"Refresh status",
	"Toggle live watch",
	"Start migration",
	"Pause migration",
	"Resume migration",
	"Cancel migration",
	"Initiate cutover",
	"Force cutover",
	"Show cutover status",
	"Revert cutover",
	"Open destination details",
}

func (m *Model) activateSourceAction() (tea.Model, tea.Cmd) {
	switch m.cursor {
	case 0:
		return m.refresh()
	case 1:
		m.sourceWatching = !m.sourceWatching
		if m.sourceWatching {
			m.loading = true
			command := m.loadSourceDetailCmd()
			return m, command
		}
	case 2:
		return m.confirmAction("Start migration", "Start this migration?", screenSourceDetail,
			m.sourceMutationCmd("Migration started", m.service.StartSourceMigration))
	case 3:
		return m.confirmAction("Pause migration", "Pause this migration?", screenSourceDetail,
			m.sourceMutationCmd("Migration paused", m.service.PauseSourceMigration))
	case 4:
		return m.confirmAction("Resume migration", "Resume this migration?", screenSourceDetail,
			m.sourceMutationCmd("Migration resumed", m.service.ResumeSourceMigration))
	case 5:
		return m.confirmAction("Cancel migration", "This permanently terminates the source migration and cannot be undone.", screenSourceDetail,
			m.sourceMutationCmd("Migration cancelled", m.service.CancelSourceMigration))
	case 6:
		return m.confirmAction("Initiate cutover", "Archive the source repository and initiate cutover?", screenSourceDetail,
			m.cutoverCmd(false))
	case 7:
		return m.confirmAction("Force cutover", "Bypass readiness checks and force cutover?", screenSourceDetail,
			m.cutoverCmd(true))
	case 8:
		body := "No combined cutover state is available."
		if m.sourceDetail != nil {
			body = render.CutoverStatus(*m.sourceDetail)
		}
		m.result = resultState{title: "Cutover status", body: body, parent: screenSourceDetail}
		m.screen = screenResult
	case 9:
		return m.confirmAction("Revert cutover", "Revert cutover effects and terminate work still in progress?", screenSourceDetail,
			m.revertCutoverCmd())
	case 10:
		if m.targetID <= 0 {
			m.err = errors.New("this source migration does not expose a target migration ID yet")
			return m, nil
		}
		m.screen, m.loading, m.err = screenTargetDetail, true, nil
		m.targetParent = screenSourceDetail
		m.cursor = 0
		command := m.loadTargetDetailCmd()
		return m, command
	}
	return m, nil
}

var targetActions = []string{
	"Refresh status",
	"List repository resources",
	"Request node report",
	"Check report status",
	"Get report download URL",
	"Pause target migration",
	"Resume target migration",
	"Abort target migration",
}

func (m *Model) activateTargetAction() (tea.Model, tea.Cmd) {
	switch m.cursor {
	case 0:
		return m.refresh()
	case 1:
		return m.openResourcesForm()
	case 2:
		return m.openReportForm("Request report", "request")
	case 3:
		return m.openReportForm("Report status", "status")
	case 4:
		return m.openReportForm("Report URL", "url")
	case 5:
		return m.confirmAction("Pause target migration", "Pause the target migration?", screenTargetDetail,
			m.targetMutationCmd("Target migration paused", m.service.PauseTargetMigration))
	case 6:
		return m.confirmAction("Resume target migration", "Resume the target migration?", screenTargetDetail,
			m.targetMutationCmd("Target migration resumed", m.service.ResumeTargetMigration))
	case 7:
		return m.confirmAction("Abort target migration", "This permanently aborts the target migration and cannot be undone.", screenTargetDetail,
			m.targetMutationCmd("Target migration aborted", m.service.AbortTargetMigration))
	}
	return m, nil
}

var mannequinActions = []string{
	"List mannequins",
	"Export mannequins to CSV",
	"Reclaim a mannequin",
	"Reclaim mannequins from CSV",
}

func (m *Model) activateMannequinAction() (tea.Model, tea.Cmd) {
	switch m.cursor {
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

var configurationActions = []string{
	"Refresh configuration",
	"Edit configuration",
	"Reset configuration",
}

func (m *Model) activateConfigurationAction() (tea.Model, tea.Cmd) {
	switch m.cursor {
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

func (m *Model) confirmAction(title, body string, parent screen, command tea.Cmd) (tea.Model, tea.Cmd) {
	m.confirm = confirmState{title: title, body: body, parent: parent, command: command}
	m.screen = screenConfirm
	return m, nil
}

func (m *Model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y", "enter":
		m.screen = m.confirm.parent
		m.loading = true
		return m, m.confirm.command
	case "n", "N", "esc", "q":
		m.screen = m.confirm.parent
	}
	return m, nil
}

func (m *Model) updateResult(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.result.offset > 0 {
			m.result.offset--
		}
	case "down", "j":
		if m.result.offset < max(0, len(strings.Split(m.result.body, "\n"))-1) {
			m.result.offset++
		}
	case "pgup":
		m.result.offset = max(0, m.result.offset-10)
	case "pgdown":
		m.result.offset += 10
	case "enter", "esc", "q":
		parent := m.result.parent
		refresh := m.result.refresh
		m.screen, m.cursor, m.err = parent, 0, nil
		if refresh {
			return m.refresh()
		}
	}
	return m, nil
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
			return m.loadSourceDetailCmd(), nil
		},
	})
}

func (m *Model) openSourceCreateForm(parent screen) (tea.Model, tea.Cmd) {
	return m.openForm(formState{
		title:  "Create migration",
		parent: parent,
		fields: []formField{
			{key: "sourceOwner", label: "Source owner", kind: fieldText},
			{key: "sourceRepo", label: "Source repository", kind: fieldText},
			{key: "targetOwner", label: "Target owner", kind: fieldText},
			{key: "targetRepo", label: "Target repository", kind: fieldText},
			{key: "visibility", label: "Target visibility", kind: fieldSelect, value: "internal", options: []string{"internal", "private"}},
			{key: "start", label: "Start after creation", kind: fieldBool, value: "false"},
		},
		submit: func(values map[string]string) (tea.Cmd, error) {
			input := workflow.SourceCreateInput{
				SourceOwner: values["sourceOwner"],
				SourceRepo:  values["sourceRepo"],
				TargetOwner: values["targetOwner"],
				TargetRepo:  values["targetRepo"],
				Visibility:  values["visibility"],
				Start:       values["start"] == "true",
			}
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
			}, nil
		},
	})
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
	err        error
}

type targetDetailMsg struct {
	migration *elmapi.TargetMigration
	err       error
}

type configMsg struct {
	configuration *workflow.Configuration
	err           error
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

func (m *Model) loadTargetListCmd() tea.Cmd {
	return func() tea.Msg {
		migrations, err := m.service.ListTargetMigrations(m.ctx, "", 0)
		return targetListMsg{migrations: migrations, err: err}
	}
}

func (m *Model) loadTargetDetailCmd() tea.Cmd {
	id := m.targetID
	return func() tea.Msg {
		migration, err := m.service.GetTargetMigration(m.ctx, id)
		return targetDetailMsg{migration: migration, err: err}
	}
}

func (m *Model) loadConfigurationCmd() tea.Cmd {
	return func() tea.Msg {
		configuration, err := m.service.GetConfiguration(m.ctx)
		return configMsg{configuration: configuration, err: err}
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
