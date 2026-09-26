package ui

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"
)

type action func(m Model) (Model, tea.Cmd)

// listActions handles keys while browsing; unmatched keys go to the table (j/k, arrows, g/G, pgup/pgdn).
var listActions = map[string]action{
	"q":         quit,
	"ctrl+c":    quit,
	"tab":       nextSection,
	"l":         nextSection,
	"right":     nextSection,
	"shift+tab": prevSection,
	"h":         prevSection,
	"left":      prevSection,
	"r":         refresh,
	"o":         openIssue,
	"y":         copyURL,
	"b":         copyBranch,
	"s":         pickState,
	"a":         assignToMe,
}

// pickerActions handles keys while the "move to state" list is open.
var pickerActions = map[string]action{
	"j":      pickerDown,
	"down":   pickerDown,
	"k":      pickerUp,
	"up":     pickerUp,
	"enter":  pickerApply,
	"esc":    pickerCancel,
	"q":      pickerCancel,
	"ctrl+c": quit,
}

// openers maps GOOS to the command that opens a URL in the default browser.
var openers = map[string]string{
	"darwin":  "open",
	"linux":   "xdg-open",
	"freebsd": "xdg-open",
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.flash, m.flashErr = "", false
	if m.picker != nil {
		if act, ok := pickerActions[msg.String()]; ok {
			return act(m)
		}
		return m, nil
	}
	if act, ok := listActions[msg.String()]; ok {
		return act(m)
	}
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func quit(m Model) (Model, tea.Cmd) {
	return m, tea.Quit
}

func nextSection(m Model) (Model, tea.Cmd) {
	m.active = (m.active + 1) % len(m.sections)
	m.table.SetCursor(0)
	return m.syncTable(), nil
}

func prevSection(m Model) (Model, tea.Cmd) {
	m.active = (m.active - 1 + len(m.sections)) % len(m.sections)
	m.table.SetCursor(0)
	return m.syncTable(), nil
}

func refresh(m Model) (Model, tea.Cmd) {
	m, cmd := m.reload(m.active)
	return m.withInfo("refreshing " + m.cfg.Sections[m.active].Title), cmd
}

func openIssue(m Model) (Model, tea.Cmd) {
	issue, ok := m.selected()
	if !ok {
		return m, nil
	}
	if !strings.HasPrefix(issue.URL, "https://") {
		return m.withError(fmt.Sprintf("refusing to open %s: issue URL %q is not https", issue.Identifier, issue.URL)), nil
	}
	opener, ok := openers[runtime.GOOS]
	if !ok {
		return m.withError(fmt.Sprintf("don't know how to open a browser on %s; copy the URL with y", runtime.GOOS)), nil
	}
	return m, func() tea.Msg {
		return openedMsg{url: issue.URL, err: exec.Command(opener, issue.URL).Run()}
	}
}

func copyURL(m Model) (Model, tea.Cmd) {
	issue, ok := m.selected()
	if !ok {
		return m, nil
	}
	return m.withInfo("copied " + issue.URL), tea.SetClipboard(issue.URL)
}

func copyBranch(m Model) (Model, tea.Cmd) {
	issue, ok := m.selected()
	if !ok {
		return m, nil
	}
	return m.withInfo("copied branch " + issue.BranchName), tea.SetClipboard(issue.BranchName)
}

func pickState(m Model) (Model, tea.Cmd) {
	issue, ok := m.selected()
	if !ok {
		return m, nil
	}
	return m.withInfo("loading states for " + issue.Team.Key), loadStates(m.client, issue)
}

func assignToMe(m Model) (Model, tea.Cmd) {
	issue, ok := m.selected()
	if !ok {
		return m, nil
	}
	if m.viewerErr != nil {
		m.viewerErr = nil
		return m.withError("couldn't load your Linear user earlier; retrying, press a again in a moment"), loadViewer(m.client)
	}
	if m.viewer == nil {
		return m.withError("your Linear user hasn't loaded yet; try again in a moment"), nil
	}
	return m.withInfo("assigning " + issue.Identifier), assign(m.client, issue, *m.viewer)
}

func pickerDown(m Model) (Model, tea.Cmd) {
	p := *m.picker
	p.cursor = min(p.cursor+1, len(p.states)-1)
	m.picker = &p
	return m, nil
}

func pickerUp(m Model) (Model, tea.Cmd) {
	p := *m.picker
	p.cursor = max(p.cursor-1, 0)
	m.picker = &p
	return m, nil
}

func pickerApply(m Model) (Model, tea.Cmd) {
	p := *m.picker
	m.picker = nil
	if len(p.states) == 0 {
		return m.withError(p.issue.Team.Key + " has no workflow states"), nil
	}
	state := p.states[p.cursor]
	return m.withInfo(fmt.Sprintf("moving %s to %s", p.issue.Identifier, state.Name)), setState(m.client, p.issue, state)
}

func pickerCancel(m Model) (Model, tea.Cmd) {
	m.picker = nil
	return m, nil
}
