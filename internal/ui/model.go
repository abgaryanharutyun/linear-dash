// Package ui is the Bubble Tea dashboard: one tab per configured section, an issue list and a detail pane.
package ui

import (
	"context"
	"fmt"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"

	"github.com/abgaryanharutyun/linear-dash/internal/config"
	"github.com/abgaryanharutyun/linear-dash/internal/linear"
)

type sectionData struct {
	issues  []linear.Issue
	loading bool
	err     error
	// seq identifies the latest request so an older, slower response can't overwrite newer data
	seq int
}

// picker is the "move to state" list shown in place of the detail pane.
type picker struct {
	issue  linear.Issue
	states []linear.State
	cursor int
}

type Model struct {
	client    *linear.Client
	cfg       config.Config
	sections  []sectionData
	active    int
	table     table.Model
	viewer    *linear.User
	viewerErr error
	picker    *picker
	flash     string
	flashErr  bool
	width     int
	height    int
}

type issuesMsg struct {
	section int
	seq     int
	issues  []linear.Issue
	err     error
}

type viewerMsg struct {
	user linear.User
	err  error
}

type statesMsg struct {
	issue  linear.Issue
	states []linear.State
	err    error
}

type updatedMsg struct {
	issue linear.Issue
	done  string
	err   error
}

type openedMsg struct {
	url string
	err error
}

func New(client *linear.Client, cfg config.Config) Model {
	sections := make([]sectionData, len(cfg.Sections))
	for i := range sections {
		sections[i] = sectionData{loading: true, seq: 1}
	}
	return Model{
		client:   client,
		cfg:      cfg,
		sections: sections,
		table:    newTable(),
	}
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{loadViewer(m.client)}
	for i := range m.cfg.Sections {
		cmds = append(cmds, loadSection(m.client, m.cfg, i, m.sections[i].seq))
	}
	return tea.Batch(cmds...)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m.syncTable(), nil

	case issuesMsg:
		if msg.seq != m.sections[msg.section].seq {
			return m, nil
		}
		m.sections = replaceAt(m.sections, msg.section, sectionData{issues: msg.issues, err: msg.err, seq: msg.seq})
		return m.syncTable(), nil

	case viewerMsg:
		if msg.err != nil {
			m.viewerErr = msg.err
			return m.withError(fmt.Sprintf("load Linear user: %v", msg.err)), nil
		}
		m.viewer, m.viewerErr = &msg.user, nil
		return m, nil

	case statesMsg:
		if msg.err != nil {
			return m.withError(fmt.Sprintf("load states for %s: %v", msg.issue.Identifier, msg.err)), nil
		}
		if len(msg.states) == 0 {
			return m.withError(fmt.Sprintf("team %s has no workflow states", msg.issue.Team.Key)), nil
		}
		m.picker = &picker{issue: msg.issue, states: msg.states, cursor: stateIndex(msg.states, msg.issue.State.ID)}
		return m, nil

	case updatedMsg:
		if msg.err != nil {
			return m.withError(msg.err.Error()), nil
		}
		m.sections = replaceIssue(m.sections, msg.issue)
		// the change may move the issue in or out of other sections' filters
		m, cmd := m.reloadAll()
		return m.withInfo(msg.done).syncTable(), cmd

	case openedMsg:
		if msg.err != nil {
			return m.withError(fmt.Sprintf("open %s: %v", msg.url, msg.err)), nil
		}
		return m.withInfo("opened " + msg.url), nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// selected returns the issue under the cursor in the active section.
func (m Model) selected() (linear.Issue, bool) {
	issues := m.sections[m.active].issues
	i := m.table.Cursor()
	if i < 0 || i >= len(issues) {
		return linear.Issue{}, false
	}
	return issues[i], true
}

func (m Model) withInfo(s string) Model {
	m.flash, m.flashErr = s, false
	return m
}

func (m Model) withError(s string) Model {
	m.flash, m.flashErr = s, true
	return m
}

func loadViewer(client *linear.Client) tea.Cmd {
	return func() tea.Msg {
		user, err := client.Viewer(context.Background())
		return viewerMsg{user: user, err: err}
	}
}

// reload starts a fresh request for section i, keeping its current issues on screen until the response lands.
func (m Model) reload(i int) (Model, tea.Cmd) {
	s := m.sections[i]
	m.sections = replaceAt(m.sections, i, sectionData{issues: s.issues, loading: true, seq: s.seq + 1})
	return m, loadSection(m.client, m.cfg, i, s.seq+1)
}

func (m Model) reloadAll() (Model, tea.Cmd) {
	cmds := make([]tea.Cmd, 0, len(m.sections))
	for i := range m.sections {
		var cmd tea.Cmd
		m, cmd = m.reload(i)
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func loadSection(client *linear.Client, cfg config.Config, i int, seq int) tea.Cmd {
	return func() tea.Msg {
		issues, err := client.Issues(context.Background(), cfg.Sections[i].Filter, cfg.Limit)
		return issuesMsg{section: i, seq: seq, issues: issues, err: err}
	}
}

func loadStates(client *linear.Client, issue linear.Issue) tea.Cmd {
	return func() tea.Msg {
		states, err := client.TeamStates(context.Background(), issue.Team.ID)
		return statesMsg{issue: issue, states: states, err: err}
	}
}

func setState(client *linear.Client, issue linear.Issue, state linear.State) tea.Cmd {
	return func() tea.Msg {
		updated, err := client.SetState(context.Background(), issue.ID, state.ID)
		if err != nil {
			err = fmt.Errorf("move %s to %s: %w", issue.Identifier, state.Name, err)
		}
		return updatedMsg{issue: updated, done: fmt.Sprintf("%s → %s", issue.Identifier, state.Name), err: err}
	}
}

func assign(client *linear.Client, issue linear.Issue, user linear.User) tea.Cmd {
	return func() tea.Msg {
		updated, err := client.Assign(context.Background(), issue.ID, user.ID)
		if err != nil {
			err = fmt.Errorf("assign %s to %s: %w", issue.Identifier, user.DisplayName, err)
		}
		return updatedMsg{issue: updated, done: fmt.Sprintf("%s assigned to you", issue.Identifier), err: err}
	}
}

func replaceAt(sections []sectionData, i int, data sectionData) []sectionData {
	out := append([]sectionData(nil), sections...)
	out[i] = data
	return out
}

// replaceIssue swaps an updated issue into every section that lists it.
func replaceIssue(sections []sectionData, issue linear.Issue) []sectionData {
	out := make([]sectionData, len(sections))
	for i, s := range sections {
		issues := append([]linear.Issue(nil), s.issues...)
		for j := range issues {
			if issues[j].ID == issue.ID {
				issues[j] = issue
			}
		}
		out[i] = sectionData{issues: issues, loading: s.loading, err: s.err, seq: s.seq}
	}
	return out
}

func stateIndex(states []linear.State, id string) int {
	for i, s := range states {
		if s.ID == id {
			return i
		}
	}
	return 0
}
