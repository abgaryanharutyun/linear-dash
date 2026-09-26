package ui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/abgaryanharutyun/linear-dash/internal/github"
	"github.com/abgaryanharutyun/linear-dash/internal/linear"
)

const (
	// wrapping a huge description on every render makes key presses lag; the pane can't show more anyway
	maxDescriptionRunes = 4000
	prTimeout           = 15 * time.Second
	// detailDelay waits for the cursor to settle so scrolling through a list doesn't fire a load per row
	detailDelay = 150 * time.Millisecond
	// maxParallelPRs bounds concurrent `gh pr view` processes for one issue
	maxParallelPRs = 4
)

type prResult struct {
	url string
	pr  github.PullRequest
	err error
}

// detailState is the lazily loaded extra data for one issue.
type detailState struct {
	loading bool
	detail  linear.Detail
	prs     []prResult
	err     error
}

type detailMsg struct {
	issueID string
	state   detailState
}

// detailDueMsg fires after detailDelay; the load starts only if the same issue is still selected.
type detailDueMsg struct {
	issueID string
}

// prStateColors colors a PR by GitHub state.
var prStateColors = map[string]string{
	"OPEN":   "#4CB782",
	"MERGED": "#A371F7",
	"CLOSED": "#F2555A",
}

var reviewLabels = map[string]string{
	"APPROVED":          "approved",
	"CHANGES_REQUESTED": "changes requested",
	"REVIEW_REQUIRED":   "review required",
}

// ensureDetail schedules loading comments, sub-issues and PR status once the cursor rests on an issue.
func (m Model) ensureDetail() (Model, tea.Cmd) {
	issue, ok := m.selected()
	if !ok || !m.showDetail() || issue.ID == m.detailPending {
		return m, nil
	}
	if _, cached := m.details[issue.ID]; cached {
		return m, nil
	}
	m.detailPending = issue.ID
	return m, tea.Tick(detailDelay, func(time.Time) tea.Msg { return detailDueMsg{issueID: issue.ID} })
}

// startDetail begins the load if the cursor is still on the issue the timer was set for.
func (m Model) startDetail(msg detailDueMsg) (Model, tea.Cmd) {
	if m.detailPending == msg.issueID {
		m.detailPending = ""
	}
	issue, ok := m.selected()
	if !ok || issue.ID != msg.issueID {
		return m, nil
	}
	if _, cached := m.details[issue.ID]; cached {
		return m, nil
	}
	m.details = withDetail(m.details, issue.ID, detailState{loading: true})
	return m, loadDetail(m.client, issue)
}

func loadDetail(client *linear.Client, issue linear.Issue) tea.Cmd {
	return func() tea.Msg {
		detail, err := client.IssueDetail(context.Background(), issue.ID)
		if err != nil {
			return detailMsg{issueID: issue.ID, state: detailState{err: err}}
		}
		urls := make([]string, 0, len(detail.Attachments))
		for _, a := range detail.Attachments {
			if github.IsPullRequestURL(a.URL) {
				urls = append(urls, a.URL)
			}
		}
		return detailMsg{issueID: issue.ID, state: detailState{detail: detail, prs: pullRequestStatuses(urls)}}
	}
}

// pullRequestStatuses runs `gh pr view` for each URL, at most maxParallelPRs at a time, keeping input order.
func pullRequestStatuses(urls []string) []prResult {
	results := make([]prResult, len(urls))
	slots := make(chan struct{}, maxParallelPRs)
	var wg sync.WaitGroup
	for i, url := range urls {
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			ctx, cancel := context.WithTimeout(context.Background(), prTimeout)
			defer cancel()
			pr, err := github.PullRequestStatus(ctx, url)
			results[i] = prResult{url: url, pr: pr, err: err}
		}()
	}
	wg.Wait()
	return results
}

// markdownCache holds glamour renderers per width and rendered descriptions; it's a render cache shared by
// all Model copies, so View can fill it without changing the model.
type markdownCache struct {
	width    int
	renderer *glamour.TermRenderer
	rendered map[string]string
}

// maxRendered caps cached renders; past it the cache starts over rather than growing all session.
const maxRendered = 500

func newMarkdownCache() *markdownCache {
	return &markdownCache{rendered: map[string]string{}}
}

func (c *markdownCache) render(issue linear.Issue, width int) string {
	return c.renderText(fmt.Sprintf("%s|%s", issue.ID, issue.UpdatedAt), issue.Description, width)
}

// renderText renders Markdown once per id and width; id must change whenever text does.
func (c *markdownCache) renderText(id string, text string, width int) string {
	if c.renderer == nil || c.width != width || len(c.rendered) >= maxRendered {
		r, err := glamour.NewTermRenderer(glamour.WithStandardStyle("dark"), glamour.WithWordWrap(width))
		if err != nil {
			return lipgloss.NewStyle().Foreground(errColor).Render("markdown renderer: " + err.Error())
		}
		c.width, c.renderer, c.rendered = width, r, map[string]string{}
	}
	key := id
	if out, ok := c.rendered[key]; ok {
		return out
	}
	out, err := c.renderer.Render(truncate(text, maxDescriptionRunes))
	if err != nil {
		return lipgloss.NewStyle().Foreground(errColor).Render("render description: " + err.Error())
	}
	out = strings.Trim(out, "\n")
	c.rendered[key] = out
	return out
}

func (m Model) detailView(issue linear.Issue, width int) string {
	wrap := lipgloss.NewStyle().Width(width)
	sections := []string{
		wrap.Render(dim.Render(issue.Identifier) + "  " + bold.Render(issue.Title)),
		stateBadge(issue.State),
		"",
		strings.Join(metaLines(issue), "\n"),
	}

	state := m.details[issue.ID]
	switch {
	case state.loading:
		sections = append(sections, "", dim.Render("loading comments, sub-issues and PRs…"))
	case state.err != nil:
		sections = append(sections, "", lipgloss.NewStyle().Foreground(errColor).Width(width).Render("load details: "+state.err.Error()))
	default:
		sections = append(sections, prLines(state.prs, width)...)
		sections = append(sections, subIssueLines(state.detail.Children, width)...)
	}

	sections = append(sections, "", dim.Render(strings.Repeat("─", width)))
	if issue.Description == "" {
		sections = append(sections, dim.Render("No description"))
	} else {
		sections = append(sections, m.markdown.render(issue, width))
	}
	if !state.loading && state.err == nil {
		sections = append(sections, commentLines(state.detail.Comments, width)...)
	}
	return strings.Join(sections, "\n")
}

func metaLines(issue linear.Issue) []string {
	estimate := "—"
	if issue.Estimate != nil {
		estimate = fmt.Sprint(*issue.Estimate)
	}
	lines := []string{
		dim.Render("Assignee ") + assigneeName(issue),
		dim.Render("Team     ") + issue.Team.Name,
		dim.Render("Priority ") + issue.PriorityLabel + dim.Render("   Estimate ") + estimate,
	}
	if len(issue.Labels) > 0 {
		names := make([]string, 0, len(issue.Labels))
		for _, l := range issue.Labels {
			names = append(names, lipgloss.NewStyle().Foreground(lipgloss.Color(l.Color)).Render("●")+" "+l.Name)
		}
		lines = append(lines, dim.Render("Labels   ")+strings.Join(names, "  "))
	}
	return append(lines, dim.Render("Branch   ")+issue.BranchName)
}

func prLines(prs []prResult, width int) []string {
	if len(prs) == 0 {
		return nil
	}
	lines := []string{"", bold.Render("Pull requests")}
	for _, r := range prs {
		if r.err != nil {
			lines = append(lines, lipgloss.NewStyle().Foreground(errColor).Width(width).Render(r.err.Error()))
			continue
		}
		state := r.pr.State
		if r.pr.IsDraft {
			state = "DRAFT"
		}
		color, ok := prStateColors[r.pr.State]
		if !ok || r.pr.IsDraft {
			color = "#8A8F98"
		}
		status := []string{lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(strings.ToLower(state))}
		if c := r.pr.Checks; c.Passed+c.Failed+c.Pending > 0 {
			status = append(status, checksSummary(c))
		}
		if label, ok := reviewLabels[r.pr.ReviewDecision]; ok {
			status = append(status, label)
		}
		lines = append(lines, ansi.Truncate(fmt.Sprintf("#%d %s  %s", r.pr.Number, strings.Join(status, " · "), r.pr.Title), width, "…"))
	}
	return lines
}

func checksSummary(c github.Checks) string {
	parts := []string{lipgloss.NewStyle().Foreground(okColor).Render(fmt.Sprintf("✓%d", c.Passed))}
	if c.Failed > 0 {
		parts = append(parts, lipgloss.NewStyle().Foreground(errColor).Render(fmt.Sprintf("✗%d", c.Failed)))
	}
	if c.Pending > 0 {
		parts = append(parts, dim.Render(fmt.Sprintf("•%d", c.Pending)))
	}
	return strings.Join(parts, " ")
}

func subIssueLines(children []linear.SubIssue, width int) []string {
	if len(children) == 0 {
		return nil
	}
	lines := []string{"", bold.Render(fmt.Sprintf("Sub-issues (%d)", len(children)))}
	for _, c := range children {
		lines = append(lines, ansi.Truncate(stateDot(c.State)+dim.Render(c.Identifier)+" "+c.Title, width, "…"))
	}
	return lines
}

func commentLines(comments []linear.Comment, width int) []string {
	if len(comments) == 0 {
		return nil
	}
	now := time.Now()
	wrap := lipgloss.NewStyle().Width(width)
	lines := []string{"", dim.Render(strings.Repeat("─", width)), bold.Render(fmt.Sprintf("Comments (%d)", len(comments)))}
	for _, c := range comments {
		// comments from integrations have no user
		author := "integration"
		if c.User != nil {
			author = c.User.DisplayName
		}
		lines = append(lines, "", dim.Render(author+" · "+ago(c.CreatedAt, now)), wrap.Render(truncate(c.Body, maxDescriptionRunes)))
	}
	return lines
}

func stateDot(s linear.State) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(s.Color)).Render("● ")
}

func stateBadge(s linear.State) string {
	return stateDot(s) + s.Name
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}
