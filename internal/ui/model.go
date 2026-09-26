// Package ui is the Bubble Tea dashboard: one tab per configured section, an issue list and a detail pane.
package ui

import (
	"context"
	"fmt"
	"maps"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/abgaryanharutyun/linear-dash/internal/config"
	"github.com/abgaryanharutyun/linear-dash/internal/gitrepo"
	"github.com/abgaryanharutyun/linear-dash/internal/linear"
)

// mode decides which key table handles input and what the side pane shows.
type mode string

const (
	modeList        mode = "list"
	modeSearch      mode = "search"
	modePicker      mode = "picker"
	modeMultiPicker mode = "multiPicker"
	modeCompose     mode = "compose"
)

type Model struct {
	client    *linear.Client
	cfg       config.Config
	sections  []sectionData
	active    int
	list      list
	viewer    *linear.User
	viewerErr error

	mode   mode
	search textinput.Model
	query  string
	// grouped is per section, seeded from the config; an opened release tracks its own
	grouped  []bool
	showHelp bool
	// rows maps each list row to an index in the current items; -1 marks a group header or spacer
	rows []int
	// rowKeys holds each row's item ID so the cursor can follow an item when the list is rebuilt
	rowKeys []string
	// drillSeq numbers release opens so a response for an earlier open is never mistaken for the current one
	drillSeq int

	picker  *picker
	multi   *multiPicker
	compose *compose
	drill   *drill

	details map[string]detailState
	// detailPending is the issue whose detail load is waiting for the cursor to settle
	detailPending string
	markdown      *markdownCache

	flash    string
	flashErr bool
	width    int
	height   int
}

type viewerMsg struct {
	user linear.User
	err  error
}

type updatedMsg struct {
	issue linear.Issue
	done  string
	err   error
}

type commentMsg struct {
	issue linear.Issue
	body  string
	err   error
}

type branchMsg struct {
	issue  linear.Issue
	path   string
	result gitrepo.SwitchResult
	err    error
}

type markedMsg struct {
	noteID string
	readAt time.Time
	err    error
}

type openedMsg struct {
	url string
	err error
}

type tickMsg struct{}

type infoMsg struct {
	text string
}

func New(client *linear.Client, cfg config.Config) Model {
	sections := make([]sectionData, len(cfg.Sections))
	grouped := make([]bool, len(cfg.Sections))
	for i := range sections {
		sections[i] = sectionData{loading: true, seq: 1}
		grouped[i] = cfg.Sections[i].Grouped
	}
	search := textinput.New()
	search.Prompt = "/ "
	search.Placeholder = "filter by id, title, state, assignee, label"
	return Model{
		client:   client,
		cfg:      cfg,
		sections: sections,
		grouped:  grouped,
		mode:     modeList,
		search:   search,
		details:  map[string]detailState{},
		markdown: newMarkdownCache(),
	}
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{loadViewer(m.client)}
	for i := range m.cfg.Sections {
		cmds = append(cmds, loadSection(m.client, m.cfg, i, m.sections[i].seq))
	}
	if m.cfg.Refresh > 0 {
		cmds = append(cmds, scheduleTick(m.cfg.Refresh))
	}
	return tea.Batch(cmds...)
}

// Update handles msg, then makes sure the detail pane's data is loading for whatever is now selected.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.update(msg)
	next, detailCmd := next.ensureDetail()
	return next, tea.Batch(cmd, detailCmd)
}

func (m Model) update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m.syncList(), nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case issuesMsg:
		if msg.seq != m.sections[msg.section].seq {
			return m, nil
		}
		prev := m.sections[msg.section]
		if msg.err != nil && itemCounts[m.cfg.Sections[msg.section].Kind](prev) > 0 {
			// a failed refresh keeps what's on screen instead of blanking the tab
			m.sections = replaceAt(m.sections, msg.section, sectionData{issues: prev.issues, notes: prev.notes, releases: prev.releases, seq: msg.seq})
			return m.withError(fmt.Sprintf("refresh %s: %v", m.cfg.Sections[msg.section].Title, msg.err)).syncList(), nil
		}
		m.sections = replaceAt(m.sections, msg.section, sectionData{issues: msg.issues, notes: msg.notes, releases: msg.releases, err: msg.err, seq: msg.seq})
		return m.syncList(), nil

	case drillMsg:
		return m.applyDrillMsg(msg), nil

	case viewerMsg:
		if msg.err != nil {
			m.viewerErr = msg.err
			return m.withError(fmt.Sprintf("load Linear user: %v", msg.err)), nil
		}
		m.viewer, m.viewerErr = &msg.user, nil
		return m, nil

	case pickerMsg:
		if msg.err != nil {
			return m.withError(msg.err.Error()), nil
		}
		if m.mode != modeList {
			// something else opened while this loaded; don't replace it
			return m, nil
		}
		m.picker, m.mode = msg.picker, modePicker
		return m.withInfo(""), m.picker.filter.Focus()

	case multiPickerMsg:
		if msg.err != nil {
			return m.withError(msg.err.Error()), nil
		}
		if m.mode != modeList {
			return m, nil
		}
		m.multi, m.mode = msg.picker, modeMultiPicker
		return m.withInfo(""), m.multi.filter.Focus()

	case detailDueMsg:
		return m.startDetail(msg)

	case infoMsg:
		return m.withInfo(msg.text), nil

	case detailMsg:
		m.details = withDetail(m.details, msg.issueID, msg.state)
		return m, nil

	case updatedMsg:
		if msg.err != nil {
			return m.withError(msg.err.Error()), nil
		}
		m.sections = replaceIssue(m.sections, msg.issue)
		if m.drill != nil {
			d := *m.drill
			d.data = replaceIssue([]sectionData{d.data}, msg.issue)[0]
			m.drill = &d
		}
		m.details = withoutDetail(m.details, msg.issue.ID)
		// the change may move the issue in or out of other sections' filters
		m, cmd := m.reloadAll()
		return m.withInfo(msg.done).syncList(), cmd

	case commentMsg:
		if msg.err != nil {
			m = m.withError(fmt.Sprintf("comment on %s failed, draft restored: %v", msg.issue.Identifier, msg.err))
			if m.mode != modeList {
				return m, nil
			}
			c := newCompose(msg.issue, m.client, m.sideWidth())
			c.input.SetValue(msg.body)
			m.compose, m.mode = &c, modeCompose
			return m, m.compose.input.Focus()
		}
		m.details = withoutDetail(m.details, msg.issue.ID)
		return m.withInfo("commented on " + msg.issue.Identifier), nil

	case branchMsg:
		if msg.err != nil {
			return m.withError(fmt.Sprintf("branch for %s: %v", msg.issue.Identifier, msg.err)), nil
		}
		return m.withInfo(fmt.Sprintf("%s %s in %s", msg.result, msg.issue.BranchName, msg.path)), nil

	case markedMsg:
		if msg.err != nil {
			return m.withError(fmt.Sprintf("mark notification read: %v", msg.err)), nil
		}
		m.sections = markRead(m.sections, msg.noteID, msg.readAt)
		return m.withInfo("marked read").syncList(), nil

	case openedMsg:
		if msg.err != nil {
			return m.withError(fmt.Sprintf("open %s: %v", msg.url, msg.err)), nil
		}
		return m.withInfo("opened " + msg.url), nil

	case tickMsg:
		m.details = map[string]detailState{}
		m, cmd := m.reloadAll()
		return m, tea.Batch(cmd, scheduleTick(m.cfg.Refresh))
	}
	return m.forwardToInput(msg)
}

// inputForwarders pass non-key messages (cursor blink etc.) to whichever text input the mode shows.
var inputForwarders = map[mode]func(m Model, msg tea.Msg) (Model, tea.Cmd){
	modeList: func(m Model, msg tea.Msg) (Model, tea.Cmd) { return m, nil },
	modeSearch: func(m Model, msg tea.Msg) (Model, tea.Cmd) {
		var cmd tea.Cmd
		m.search, cmd = m.search.Update(msg)
		return m, cmd
	},
	modePicker: func(m Model, msg tea.Msg) (Model, tea.Cmd) {
		p := *m.picker
		var cmd tea.Cmd
		p.filter, cmd = p.filter.Update(msg)
		m.picker = &p
		return m, cmd
	},
	modeMultiPicker: func(m Model, msg tea.Msg) (Model, tea.Cmd) {
		p := *m.multi
		var cmd tea.Cmd
		p.filter, cmd = p.filter.Update(msg)
		m.multi = &p
		return m, cmd
	},
	modeCompose: func(m Model, msg tea.Msg) (Model, tea.Cmd) {
		c := *m.compose
		var cmd tea.Cmd
		c.input, cmd = c.input.Update(msg)
		m.compose = &c
		return m, cmd
	},
}

func (m Model) forwardToInput(msg tea.Msg) (Model, tea.Cmd) {
	return inputForwarders[m.mode](m, msg)
}

// selected returns the issue under the cursor, or false on a group header or empty list.
func (m Model) selected() (linear.Issue, bool) {
	data, kind := m.current()
	i := m.list.Cursor()
	if kind == config.KindReleases || i < 0 || i >= len(m.rows) || m.rows[i] < 0 {
		return linear.Issue{}, false
	}
	return data.issues[m.rows[i]], true
}

// selectedNote returns the notification under the cursor in a notifications section.
func (m Model) selectedNote() (linear.Notification, bool) {
	data, _ := m.current()
	i := m.list.Cursor()
	notes := data.notes
	if i < 0 || i >= len(m.rows) || m.rows[i] < 0 || m.rows[i] >= len(notes) {
		return linear.Notification{}, false
	}
	return notes[m.rows[i]], true
}

func (m Model) withInfo(s string) Model {
	m.flash, m.flashErr = s, false
	return m
}

func (m Model) withError(s string) Model {
	m.flash, m.flashErr = s, true
	return m
}

func (m Model) backToList() Model {
	m.mode, m.picker, m.multi, m.compose = modeList, nil, nil, nil
	return m
}

func scheduleTick(every time.Duration) tea.Cmd {
	return tea.Tick(every, func(time.Time) tea.Msg { return tickMsg{} })
}

func loadViewer(client *linear.Client) tea.Cmd {
	return func() tea.Msg {
		user, err := client.Viewer(context.Background())
		return viewerMsg{user: user, err: err}
	}
}

func withDetail(details map[string]detailState, id string, state detailState) map[string]detailState {
	out := maps.Clone(details)
	out[id] = state
	return out
}

func withoutDetail(details map[string]detailState, id string) map[string]detailState {
	out := maps.Clone(details)
	delete(out, id)
	return out
}
