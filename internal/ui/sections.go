package ui

import (
	"context"
	"fmt"
	"image/color"
	"sort"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/abgaryanharutyun/linear-dash/internal/config"
	"github.com/abgaryanharutyun/linear-dash/internal/linear"
)

type sectionData struct {
	issues []linear.Issue
	// notes is index-aligned with issues in notifications sections and empty otherwise
	notes []linear.Notification
	// releases is only set in releases sections, which have no issues
	releases []linear.Release
	loading  bool
	err      error
	// seq identifies the latest request so an older, slower response can't overwrite newer data
	seq int
}

type issuesMsg struct {
	section  int
	seq      int
	issues   []linear.Issue
	notes    []linear.Notification
	releases []linear.Release
	err      error
}

type loader func(client *linear.Client, cfg config.Config, i int, seq int) tea.Cmd

var loaders = map[config.Kind]loader{
	config.KindIssues:        loadIssues,
	config.KindNotifications: loadNotifications,
	config.KindReleases:      loadReleases,
}

type rowBuilder func(s sectionData, i int, now time.Time) listRow

var rowBuilders = map[config.Kind]rowBuilder{
	config.KindIssues:        issueRow,
	config.KindNotifications: notificationRow,
	config.KindReleases:      releaseRow,
}

// itemKeys identify the item behind a row across reloads: issue, notification or release ID.
var itemKeys = map[config.Kind]func(s sectionData, i int) string{
	config.KindIssues:        func(s sectionData, i int) string { return s.issues[i].ID },
	config.KindNotifications: func(s sectionData, i int) string { return s.notes[i].ID },
	config.KindReleases:      func(s sectionData, i int) string { return s.releases[i].ID },
}

// itemCounts and matchers let visibleRows walk issues or releases alike.
var itemCounts = map[config.Kind]func(s sectionData) int{
	config.KindIssues:        func(s sectionData) int { return len(s.issues) },
	config.KindNotifications: func(s sectionData) int { return len(s.issues) },
	config.KindReleases:      func(s sectionData) int { return len(s.releases) },
}

var matchers = map[config.Kind]func(s sectionData, i int, query string) bool{
	config.KindIssues:        func(s sectionData, i int, query string) bool { return matchesQuery(s.issues[i], query) },
	config.KindNotifications: func(s sectionData, i int, query string) bool { return matchesQuery(s.issues[i], query) },
	config.KindReleases:      matchesRelease,
}

// grouper orders matching rows and splits them into named groups, returned in display order.
type grouper func(s sectionData, matches []int) (order []string, groups map[string][]int)

// groupers decide what `t` groups by: workflow state for issue lists, pipeline for releases.
var groupers = map[config.Kind]grouper{
	config.KindIssues:        groupByState,
	config.KindNotifications: groupByState,
	config.KindReleases:      groupByPipeline,
}

var groupLabels = map[config.Kind]string{
	config.KindIssues:        "grouped by state",
	config.KindNotifications: "grouped by state",
	config.KindReleases:      "grouped by pipeline",
}

// prStatusColors and prStatusLabels describe Linear's PR statuses; unknown ones are humanized in muted color.
var prStatusColors = map[string]color.Color{
	"draft":            muted,
	"open":             okColor,
	"inReview":         lipgloss.Color("#F2C94C"),
	"approved":         okColor,
	"changesRequested": errColor,
	"merged":           lipgloss.Color("#A371F7"),
	"closed":           errColor,
}

var prStatusLabels = map[string]string{
	"inReview":         "in review",
	"changesRequested": "changes requested",
}

// priorityMarks are Linear-style signal bars per priority (0 none, 1 urgent ... 4 low).
var priorityMarks = map[int]cell{
	0: {text: " – ", width: 3, color: muted},
	1: {text: " ! ", width: 3, color: errColor, bold: true},
	2: {text: "▂▄▆", width: 3},
	3: {text: "▂▄ ", width: 3},
	4: {text: "▂  ", width: 3},
}

// notificationLabels are short names for Linear notification types; unknown types are humanized.
var notificationLabels = map[string]string{
	"issueAssignedToYou":     "assigned to you",
	"issueUnassignedFromYou": "unassigned",
	"issueMention":           "mentioned",
	"issueCommentMention":    "mentioned in comment",
	"issueNewComment":        "new comment",
	"issueCommentReaction":   "reaction",
	"issueEmojiReaction":     "reaction",
	"issueStatusChanged":     "status changed",
	"issueStatusChangedAll":  "status changed",
	"issuePriorityUrgent":    "marked urgent",
	"issueDue":               "due soon",
	"issueCreated":           "created",
	"issueBlocking":          "blocking",
	"issueUnblocked":         "unblocked",
	"issueSubscribed":        "subscribed",
}

func loadSection(client *linear.Client, cfg config.Config, i int, seq int) tea.Cmd {
	return loaders[cfg.Sections[i].Kind](client, cfg, i, seq)
}

func loadIssues(client *linear.Client, cfg config.Config, i int, seq int) tea.Cmd {
	return func() tea.Msg {
		issues, err := client.Issues(context.Background(), cfg.Sections[i].Filter, cfg.Limit)
		return issuesMsg{section: i, seq: seq, issues: issues, err: err}
	}
}

func loadNotifications(client *linear.Client, cfg config.Config, i int, seq int) tea.Cmd {
	return func() tea.Msg {
		notes, err := client.Notifications(context.Background(), cfg.Limit)
		issues := make([]linear.Issue, 0, len(notes))
		for _, n := range notes {
			issues = append(issues, n.Issue)
		}
		return issuesMsg{section: i, seq: seq, issues: issues, notes: notes, err: err}
	}
}

func loadReleases(client *linear.Client, cfg config.Config, i int, seq int) tea.Cmd {
	return func() tea.Msg {
		window := fmt.Sprintf("P%dD", cfg.Sections[i].RecentDays)
		releases, err := client.ActiveReleases(context.Background(), window, cfg.Limit)
		return issuesMsg{section: i, seq: seq, releases: releases, err: err}
	}
}

// reload starts a fresh request for section i, keeping its current issues on screen until the response lands.
func (m Model) reload(i int) (Model, tea.Cmd) {
	s := m.sections[i]
	m.sections = replaceAt(m.sections, i, sectionData{issues: s.issues, notes: s.notes, releases: s.releases, loading: true, seq: s.seq + 1})
	return m, loadSection(m.client, m.cfg, i, s.seq+1)
}

func (m Model) reloadAll() (Model, tea.Cmd) {
	cmds := make([]tea.Cmd, 0, len(m.sections)+1)
	for i := range m.sections {
		var cmd tea.Cmd
		m, cmd = m.reload(i)
		cmds = append(cmds, cmd)
	}
	if m.drill != nil {
		var cmd tea.Cmd
		m, cmd = m.reloadDrill()
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

// visibleRows applies the search query and optional grouping to a section, returning table rows and
// the issue index behind each row (-1 for group headers and spacers).
func visibleRows(s sectionData, kind config.Kind, query string, grouped bool, now time.Time) ([]listRow, []int) {
	n := itemCounts[kind](s)
	matches := make([]int, 0, n)
	for i := range n {
		if matchers[kind](s, i, query) {
			matches = append(matches, i)
		}
	}
	build := rowBuilders[kind]
	if !grouped {
		rows := make([]listRow, 0, len(matches))
		for _, i := range matches {
			rows = append(rows, build(s, i, now))
		}
		return rows, matches
	}

	order, groups := groupers[kind](s, matches)
	rows := make([]listRow, 0, len(matches)+2*len(order))
	index := make([]int, 0, len(matches)+2*len(order))
	for g, name := range order {
		if g > 0 {
			rows = append(rows, listRow{})
			index = append(index, -1)
		}
		rows = append(rows, listRow{header: name, count: len(groups[name])})
		index = append(index, -1)
		for _, i := range groups[name] {
			rows = append(rows, build(s, i, now))
			index = append(index, i)
		}
	}
	return rows, index
}

// groupByState groups by state name in board order; every team has its own "Backlog", but one header reads better.
func groupByState(s sectionData, matches []int) ([]string, map[string][]int) {
	sorted := append([]int(nil), matches...)
	sort.SliceStable(sorted, func(a, b int) bool {
		return linear.StateLess(s.issues[sorted[a]].State, s.issues[sorted[b]].State)
	})
	return groupInOrder(sorted, func(i int) string { return s.issues[i].State.Name })
}

// groupByPipeline keeps each pipeline's releases in the list's in-progress-first order.
func groupByPipeline(s sectionData, matches []int) ([]string, map[string][]int) {
	return groupInOrder(matches, func(i int) string { return s.releases[i].Pipeline })
}

// groupInOrder buckets indices by key, ordering groups by first appearance.
func groupInOrder(indices []int, key func(i int) string) ([]string, map[string][]int) {
	var order []string
	groups := map[string][]int{}
	for _, i := range indices {
		k := key(i)
		if _, seen := groups[k]; !seen {
			order = append(order, k)
		}
		groups[k] = append(groups[k], i)
	}
	return order, groups
}

func matchesQuery(issue linear.Issue, query string) bool {
	if query == "" {
		return true
	}
	fields := []string{issue.Identifier, issue.Title, issue.State.Name, assigneeName(issue)}
	for _, l := range issue.Labels {
		fields = append(fields, l.Name)
	}
	return strings.Contains(strings.ToLower(strings.Join(fields, " ")), strings.ToLower(query))
}

func matchesRelease(s sectionData, i int, query string) bool {
	if query == "" {
		return true
	}
	r := s.releases[i]
	haystack := strings.ToLower(strings.Join([]string{r.Version, r.Name, r.Stage.Name, r.Pipeline}, " "))
	return strings.Contains(haystack, strings.ToLower(query))
}

func releaseRow(s sectionData, i int, now time.Time) listRow {
	r := s.releases[i]
	version := r.Version
	if version == "" {
		version = "—"
	}
	return listRow{sub: releaseSubline(r, now), cells: []cell{
		{text: version, width: 9, bold: true},
		{text: r.Name},
		{text: "●", width: 2, color: lipgloss.Color(r.Stage.Color)},
		{text: r.Stage.Name, width: 12, color: muted, tight: true},
		{text: r.Pipeline, width: 14, color: muted},
		{text: fmt.Sprint(r.IssueCount), width: 3, color: muted, right: true},
	}}
}

func issueRow(s sectionData, i int, now time.Time) listRow {
	issue := s.issues[i]
	prio, ok := priorityMarks[issue.Priority]
	if !ok {
		prio = priorityMarks[0]
	}
	return listRow{sub: []cell{{width: 3}, {width: 9}, teamCell(issue), prCell(issue.PullRequests)}, cells: []cell{
		prio,
		{text: issue.Identifier, width: 9, color: muted},
		{text: issue.Title},
		{text: "●", width: 2, color: lipgloss.Color(issue.State.Color)},
		{text: issue.State.Name, width: 13, color: muted, tight: true},
		{text: assigneeName(issue), width: 12, color: muted},
		{text: ago(issue.UpdatedAt, now), width: 3, color: muted, right: true},
	}}
}

func notificationRow(s sectionData, i int, now time.Time) listRow {
	note := s.notes[i]
	unread := note.ReadAt == nil
	marker := cell{text: " ", width: 1}
	if unread {
		marker = cell{text: "●", width: 1, color: accent}
	}
	return listRow{sub: []cell{{width: 1}, {width: 9}, teamCell(note.Issue), prCell(note.Issue.PullRequests)}, cells: []cell{
		marker,
		{text: note.Issue.Identifier, width: 9, color: muted},
		{text: note.Issue.Title, bold: unread},
		{text: notificationLabel(note.Type), width: 18, color: muted},
		{text: note.Actor, width: 12, color: muted},
		{text: ago(note.CreatedAt, now), width: 3, color: muted, right: true},
	}}
}

func teamCell(issue linear.Issue) cell {
	return cell{text: issue.Team.Name, width: 16, color: muted}
}

// prCell summarizes linked PRs as "⎇ repo #123 in review", noting how many more there are.
func prCell(prs []linear.PullRequestLink) cell {
	if len(prs) == 0 {
		return cell{}
	}
	pr := prs[0]
	label, ok := prStatusLabels[pr.Status]
	if !ok {
		label = humanize(pr.Status)
	}
	text := fmt.Sprintf("⎇ %s #%d  %s", pr.Repo, pr.Number, label)
	if len(prs) > 1 {
		text += fmt.Sprintf("  +%d more", len(prs)-1)
	}
	c, ok := prStatusColors[pr.Status]
	if !ok {
		c = muted
	}
	return cell{text: text, color: c}
}

func releaseSubline(r linear.Release, now time.Time) []cell {
	parts := []string{}
	if r.StartedAt != nil {
		parts = append(parts, "started "+r.StartedAt.Local().Format("Jan 2"))
	}
	if r.TargetDate != "" {
		parts = append(parts, "target "+r.TargetDate)
	}
	if r.CompletedAt != nil {
		parts = append(parts, "released "+ago(*r.CompletedAt, now)+" ago")
	}
	return []cell{{width: 9}, {text: strings.Join(parts, " · "), color: muted}}
}

func notificationLabel(kind string) string {
	if label, ok := notificationLabels[kind]; ok {
		return label
	}
	return humanize(strings.TrimPrefix(kind, "issue"))
}

// humanize turns camelCase into lower-case words: "StatusChanged" -> "status changed".
func humanize(s string) string {
	var b strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) && i > 0 {
			b.WriteRune(' ')
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
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
		out[i] = sectionData{issues: issues, notes: s.notes, releases: s.releases, loading: s.loading, err: s.err, seq: s.seq}
	}
	return out
}

// markRead sets ReadAt on a notification wherever it's listed.
func markRead(sections []sectionData, noteID string, readAt time.Time) []sectionData {
	out := make([]sectionData, len(sections))
	for i, s := range sections {
		notes := append([]linear.Notification(nil), s.notes...)
		for j := range notes {
			if notes[j].ID == noteID {
				notes[j].ReadAt = &readAt
			}
		}
		out[i] = sectionData{issues: s.issues, notes: notes, releases: s.releases, loading: s.loading, err: s.err, seq: s.seq}
	}
	return out
}

func assigneeName(issue linear.Issue) string {
	if issue.Assignee == nil {
		return "—"
	}
	return issue.Assignee.DisplayName
}

// ago renders a compact relative age such as 5m, 3h, 2d or 4mo.
func ago(t time.Time, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dmo", int(d.Hours()/(24*30)))
	}
}
