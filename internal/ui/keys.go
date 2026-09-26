package ui

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/abgaryanharutyun/linear-dash/internal/config"
	"github.com/abgaryanharutyun/linear-dash/internal/gitrepo"
	"github.com/abgaryanharutyun/linear-dash/internal/linear"
)

type action func(m Model) (Model, tea.Cmd)

type keyHandler func(m Model, msg tea.KeyPressMsg) (Model, tea.Cmd)

var keyHandlers = map[mode]keyHandler{
	modeList:        handleListKey,
	modeSearch:      handleSearchKey,
	modePicker:      handlePickerKey,
	modeMultiPicker: handleMultiPickerKey,
	modeCompose:     handleComposeKey,
}

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
	"/":         startSearch,
	"esc":       back,
	"enter":     openRelease,
	"t":         toggleGrouping,
	"o":         openIssue,
	"O":         openPullRequest,
	"y":         copyURL,
	"b":         copyBranch,
	"B":         switchBranch,
	"s":         pickState,
	"a":         assignToMe,
	"A":         pickAssignee,
	"p":         pickPriority,
	"e":         pickEstimate,
	"L":         pickLabels,
	"c":         startComment,
	"m":         markNotificationRead,
	"?":         toggleHelp,
}

// searchActions handles keys while typing a filter; everything else edits the query.
var searchActions = map[string]action{
	"enter":  acceptSearch,
	"esc":    clearSearch,
	"ctrl+c": quit,
}

// openers maps GOOS to the command that opens a URL in the default browser.
var openers = map[string]string{
	"darwin":  "open",
	"linux":   "xdg-open",
	"freebsd": "xdg-open",
}

func (m Model) handleKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	m.flash, m.flashErr = "", false
	return keyHandlers[m.mode](m, msg)
}

// listMoves are cursor movements; each returns the target row given the cursor, row count and page height.
var listMoves = map[string]func(cursor int, n int, page int) int{
	"j":      func(c, n, p int) int { return c + 1 },
	"down":   func(c, n, p int) int { return c + 1 },
	"k":      func(c, n, p int) int { return c - 1 },
	"up":     func(c, n, p int) int { return c - 1 },
	"g":      func(c, n, p int) int { return 0 },
	"home":   func(c, n, p int) int { return 0 },
	"G":      func(c, n, p int) int { return n - 1 },
	"end":    func(c, n, p int) int { return n - 1 },
	"ctrl+d": func(c, n, p int) int { return c + p/2 },
	"ctrl+u": func(c, n, p int) int { return c - p/2 },
	"pgdown": func(c, n, p int) int { return c + p },
	"pgup":   func(c, n, p int) int { return c - p },
}

func handleListKey(m Model, msg tea.KeyPressMsg) (Model, tea.Cmd) {
	if act, ok := listActions[msg.String()]; ok {
		return act(m)
	}
	move, ok := listMoves[msg.String()]
	if !ok {
		return m, nil
	}
	before := m.list.Cursor()
	m.list = m.list.withCursor(move(before, len(m.rows), m.bodyHeight()))
	return m.skipHeader(m.list.Cursor() - before), nil
}

// skipHeader moves the cursor off group headers and spacers, continuing in the direction it was moving
// and turning around at either end.
func (m Model) skipHeader(direction int) Model {
	step := 1
	if direction < 0 {
		step = -1
	}
	for _, s := range []int{step, -step} {
		i := m.list.Cursor()
		for i >= 0 && i < len(m.rows) && m.rows[i] < 0 {
			i += s
		}
		if i >= 0 && i < len(m.rows) {
			m.list = m.list.withCursor(i)
			return m
		}
	}
	return m
}

// handleSearchKey filters the list live as you type.
func handleSearchKey(m Model, msg tea.KeyPressMsg) (Model, tea.Cmd) {
	if act, ok := searchActions[msg.String()]; ok {
		return act(m)
	}
	var cmd tea.Cmd
	m.search, cmd = m.search.Update(msg)
	m.query = m.search.Value()
	return m.syncList(), cmd
}

func toggleHelp(m Model) (Model, tea.Cmd) {
	m.showHelp = !m.showHelp
	return m, nil
}

func quit(m Model) (Model, tea.Cmd) {
	return m, tea.Quit
}

func nextSection(m Model) (Model, tea.Cmd) {
	m.drill = nil
	m.active = (m.active + 1) % len(m.sections)
	return m.resetList(), nil
}

func prevSection(m Model) (Model, tea.Cmd) {
	m.drill = nil
	m.active = (m.active - 1 + len(m.sections)) % len(m.sections)
	return m.resetList(), nil
}

func refresh(m Model) (Model, tea.Cmd) {
	m.details = map[string]detailState{}
	if m.drill != nil {
		m, cmd := m.reloadDrill()
		return m.withInfo("refreshing " + releaseLabel(m.drill.release)), cmd
	}
	m, cmd := m.reload(m.active)
	return m.withInfo("refreshing " + m.cfg.Sections[m.active].Title), cmd
}

func startSearch(m Model) (Model, tea.Cmd) {
	m.mode = modeSearch
	m.search.SetValue(m.query)
	return m, m.search.Focus()
}

func acceptSearch(m Model) (Model, tea.Cmd) {
	m.mode = modeList
	m.search.Blur()
	return m, nil
}

// back clears an active filter first, then leaves an opened release.
func back(m Model) (Model, tea.Cmd) {
	if m.query == "" && m.drill != nil {
		return closeDrill(m), nil
	}
	return clearSearch(m)
}

func clearSearch(m Model) (Model, tea.Cmd) {
	m.mode, m.query = modeList, ""
	m.search.SetValue("")
	m.search.Blur()
	return m.syncList(), nil
}

func toggleGrouping(m Model) (Model, tea.Cmd) {
	if m.drill != nil {
		d := *m.drill
		d.grouped = !d.grouped
		m.drill = &d
	} else {
		grouped := append([]bool(nil), m.grouped...)
		grouped[m.active] = !grouped[m.active]
		m.grouped = grouped
	}
	_, kind := m.current()
	label := map[bool]string{true: groupLabels[kind], false: "ungrouped"}[m.isGrouped()]
	return m.withInfo(label).syncList(), nil
}

func openIssue(m Model) (Model, tea.Cmd) {
	issue, ok := m.selected()
	if !ok {
		return m, nil
	}
	return m, openURL(issue.URL)
}

// openPullRequest opens the issue's linked PR, or offers a picker when there are several.
func openPullRequest(m Model) (Model, tea.Cmd) {
	issue, ok := m.selected()
	if !ok {
		return m, nil
	}
	if len(issue.PullRequests) == 0 {
		return m.withError(issue.Identifier + " has no linked pull request"), nil
	}
	if len(issue.PullRequests) == 1 {
		return m, openURL(issue.PullRequests[0].URL)
	}
	options := make([]option, 0, len(issue.PullRequests))
	for _, pr := range issue.PullRequests {
		options = append(options, option{label: fmt.Sprintf("%s #%d  %s", pr.Repo, pr.Number, pr.Title), value: pr.URL})
	}
	m.picker, m.mode = newPicker("Open PR for "+issue.Identifier, options, 0, func(o option) tea.Cmd { return openURL(o.value) }), modePicker
	return m, m.picker.filter.Focus()
}

// openURL opens an https URL in the default browser.
func openURL(url string) tea.Cmd {
	return func() tea.Msg {
		if !strings.HasPrefix(url, "https://") {
			return openedMsg{url: url, err: fmt.Errorf("refusing to open non-https URL")}
		}
		opener, ok := openers[runtime.GOOS]
		if !ok {
			return openedMsg{url: url, err: fmt.Errorf("don't know how to open a browser on %s", runtime.GOOS)}
		}
		return openedMsg{url: url, err: exec.Command(opener, url).Run()}
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

func switchBranch(m Model) (Model, tea.Cmd) {
	issue, ok := m.selected()
	if !ok {
		return m, nil
	}
	path, ok := m.cfg.RepoPaths[issue.Team.Key]
	if !ok {
		return m.withError(fmt.Sprintf("no repo for team %s: add `repoPaths: { %s: ~/path/to/repo }` to your config", issue.Team.Key, issue.Team.Key)), nil
	}
	return m.withInfo(fmt.Sprintf("switching %s to %s", path, issue.BranchName)), func() tea.Msg {
		result, err := gitrepo.Switch(path, issue.BranchName)
		return branchMsg{issue: issue, path: path, result: result, err: err}
	}
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

func startComment(m Model) (Model, tea.Cmd) {
	issue, ok := m.selected()
	if !ok {
		return m, nil
	}
	c := newCompose(issue, m.client, m.sideWidth())
	m.compose, m.mode = &c, modeCompose
	return m, m.compose.input.Focus()
}

func markNotificationRead(m Model) (Model, tea.Cmd) {
	if m.cfg.Sections[m.active].Kind != config.KindNotifications {
		return m.withError("m marks notifications read; switch to a notifications section"), nil
	}
	note, ok := m.selectedNote()
	if !ok {
		return m, nil
	}
	if note.ReadAt != nil {
		return m.withInfo("already read"), nil
	}
	client := m.client
	return m, func() tea.Msg {
		now := time.Now()
		return markedMsg{noteID: note.ID, readAt: now, err: client.MarkNotificationRead(context.Background(), note.ID, now)}
	}
}

func assign(client *linear.Client, issue linear.Issue, user linear.User) tea.Cmd {
	return func() tea.Msg {
		updated, err := client.Assign(context.Background(), issue.ID, user.ID)
		if err != nil {
			err = fmt.Errorf("assign %s to %s: %w", issue.Identifier, user.DisplayName, err)
		}
		return updatedMsg{issue: updated, done: fmt.Sprintf("%s assigned to %s", issue.Identifier, user.DisplayName), err: err}
	}
}
