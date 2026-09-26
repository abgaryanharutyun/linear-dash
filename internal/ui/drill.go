package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/abgaryanharutyun/linear-dash/internal/config"
	"github.com/abgaryanharutyun/linear-dash/internal/linear"
)

// drill is a release opened from a releases section; its issues replace the list until esc.
type drill struct {
	release linear.Release
	data    sectionData
	grouped bool
}

type drillMsg struct {
	releaseID string
	seq       int
	issues    []linear.Issue
	err       error
}

// current returns what the list shows: the opened release's issues, or the active section.
func (m Model) current() (sectionData, config.Kind) {
	if m.drill != nil {
		return m.drill.data, config.KindIssues
	}
	return m.sections[m.active], m.cfg.Sections[m.active].Kind
}

func (m Model) isGrouped() bool {
	if m.drill != nil {
		return m.drill.grouped
	}
	return m.grouped[m.active]
}

// selectedRelease returns the release under the cursor in a releases section.
func (m Model) selectedRelease() (linear.Release, bool) {
	data, kind := m.current()
	i := m.list.Cursor()
	if kind != config.KindReleases || i < 0 || i >= len(m.rows) || m.rows[i] < 0 {
		return linear.Release{}, false
	}
	return data.releases[m.rows[i]], true
}

func openRelease(m Model) (Model, tea.Cmd) {
	release, ok := m.selectedRelease()
	if !ok {
		return m, nil
	}
	m.drillSeq++
	m.drill = &drill{release: release, data: sectionData{loading: true, seq: m.drillSeq}}
	m.query = ""
	m.search.SetValue("")
	return m.resetList(), loadReleaseIssues(m.client, m.cfg, release.ID, m.drillSeq)
}

func closeDrill(m Model) Model {
	m.drill = nil
	return m.resetList()
}

func (m Model) reloadDrill() (Model, tea.Cmd) {
	m.drillSeq++
	d := *m.drill
	d.data = sectionData{issues: d.data.issues, loading: true, seq: m.drillSeq}
	m.drill = &d
	return m, loadReleaseIssues(m.client, m.cfg, d.release.ID, m.drillSeq)
}

func loadReleaseIssues(client *linear.Client, cfg config.Config, releaseID string, seq int) tea.Cmd {
	return func() tea.Msg {
		issues, err := client.ReleaseIssues(context.Background(), releaseID, cfg.Limit)
		return drillMsg{releaseID: releaseID, seq: seq, issues: issues, err: err}
	}
}

func (m Model) applyDrillMsg(msg drillMsg) Model {
	if m.drill == nil || m.drill.release.ID != msg.releaseID || m.drill.data.seq != msg.seq {
		return m
	}
	d := *m.drill
	if msg.err != nil && len(d.data.issues) > 0 {
		d.data = sectionData{issues: d.data.issues, seq: msg.seq}
		m.drill = &d
		return m.withError(fmt.Sprintf("refresh %s: %v", releaseLabel(d.release), msg.err)).syncList()
	}
	d.data = sectionData{issues: msg.issues, err: msg.err, seq: msg.seq}
	m.drill = &d
	return m.syncList()
}

func releaseLabel(r linear.Release) string {
	if r.Version != "" {
		return r.Version
	}
	return r.Name
}

func (m Model) releaseDetailView(r linear.Release, width int) string {
	wrap := lipgloss.NewStyle().Width(width)
	stage := lipgloss.NewStyle().Foreground(lipgloss.Color(r.Stage.Color)).Render("● ") + r.Stage.Name
	lines := []string{
		wrap.Render(dim.Render(r.Pipeline) + "  " + bold.Render(releaseLabel(r))),
		stage,
		"",
		dim.Render("Name      ") + r.Name,
		dim.Render("Issues    ") + fmt.Sprint(r.IssueCount),
	}
	if r.TargetDate != "" {
		lines = append(lines, dim.Render("Target    ")+r.TargetDate)
	}
	if r.StartedAt != nil {
		lines = append(lines, dim.Render("Started   ")+r.StartedAt.Local().Format("Jan 2 15:04"))
	}
	if r.CompletedAt != nil {
		lines = append(lines, dim.Render("Released  ")+r.CompletedAt.Local().Format("Jan 2 15:04")+dim.Render(" ("+ago(*r.CompletedAt, time.Now())+" ago)"))
	}
	lines = append(lines, "", dim.Render("enter shows this release's issues"))

	if r.Description != "" {
		lines = append(lines, "", dim.Render(strings.Repeat("─", width)), m.markdown.renderText("release-desc|"+r.ID, r.Description, width))
	}
	if r.NoteContent != "" {
		title := "Release notes"
		if r.NoteTitle != "" {
			title = r.NoteTitle
		}
		lines = append(lines, "", dim.Render(strings.Repeat("─", width)), bold.Render(title), m.markdown.renderText("release-note|"+r.ID, r.NoteContent, width))
	}
	return strings.Join(lines, "\n")
}
