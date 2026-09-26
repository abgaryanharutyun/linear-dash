package ui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/abgaryanharutyun/linear-dash/internal/linear"
)

const (
	idWidth       = 9
	stateWidth    = 14
	assigneeWidth = 14
	updatedWidth  = 4
	minTitleWidth = 20
	// below this terminal width the detail pane is hidden
	splitMinWidth = 110
	cellPadding   = 2
	// wrapping a huge description on every render makes key presses lag; the pane can't show more anyway
	maxDescriptionRunes = 4000
)

var (
	accent    = lipgloss.Color("#7C83FD")
	muted     = lipgloss.Color("#8A8F98")
	errColor  = lipgloss.Color("#F2555A")
	okColor   = lipgloss.Color("#4CB782")
	tabActive = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(accent).Padding(0, 1)
	tabIdle   = lipgloss.NewStyle().Foreground(muted).Padding(0, 1)
	dim       = lipgloss.NewStyle().Foreground(muted)
	bold      = lipgloss.NewStyle().Bold(true)
	detailBox = lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).BorderForeground(muted).Padding(0, 1)
)

var helpText = "tab/h/l section · j/k move · o open · y url · b branch · s status · a assign me · r refresh · q quit"

func newTable() table.Model {
	styles := table.DefaultStyles()
	styles.Header = styles.Header.Foreground(muted).BorderStyle(lipgloss.NormalBorder()).BorderBottom(true).BorderForeground(muted)
	styles.Selected = styles.Selected.Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#3B3F6B"))
	return table.New(table.WithStyles(styles), table.WithFocused(true))
}

func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.WindowTitle = "linear-dash"
	return v
}

func (m Model) render() string {
	if m.width == 0 {
		return ""
	}
	bodyHeight := m.bodyHeight()
	if m.picker != nil && !m.showDetail() {
		return lipgloss.JoinVertical(lipgloss.Left, m.tabsView(), m.sideView(m.width, bodyHeight), m.footerView())
	}
	list := lipgloss.NewStyle().Width(m.listWidth()).Height(bodyHeight).MaxHeight(bodyHeight).Render(m.listView())
	body := list
	if m.showDetail() {
		body = lipgloss.JoinHorizontal(lipgloss.Top, list, m.sideView(m.width-m.listWidth(), bodyHeight))
	}
	return lipgloss.JoinVertical(lipgloss.Left, m.tabsView(), body, m.footerView())
}

func (m Model) bodyHeight() int {
	// tabs line + blank line + footer line
	return max(m.height-3, 3)
}

func (m Model) showDetail() bool {
	return m.width >= splitMinWidth
}

func (m Model) listWidth() int {
	if !m.showDetail() {
		return m.width
	}
	return m.width * 3 / 5
}

// syncTable rebuilds the table columns and rows for the active section and current size.
func (m Model) syncTable() Model {
	fixed := idWidth + stateWidth + assigneeWidth + updatedWidth + 5*cellPadding
	titleWidth := max(m.listWidth()-fixed, minTitleWidth)
	m.table.SetColumns([]table.Column{
		{Title: "ID", Width: idWidth},
		{Title: "Title", Width: titleWidth},
		{Title: "State", Width: stateWidth},
		{Title: "Assignee", Width: assigneeWidth},
		{Title: "Upd", Width: updatedWidth},
	})
	now := time.Now()
	issues := m.sections[m.active].issues
	rows := make([]table.Row, 0, len(issues))
	for _, issue := range issues {
		rows = append(rows, table.Row{issue.Identifier, issue.Title, issue.State.Name, assigneeName(issue), ago(issue.UpdatedAt, now)})
	}
	m.table.SetRows(rows)
	// SetRows leaves the cursor at -1 if rows arrive after an empty render; re-clamp it onto the list
	m.table.SetCursor(m.table.Cursor())
	m.table.SetWidth(m.listWidth())
	m.table.SetHeight(m.bodyHeight())
	return m
}

func (m Model) tabsView() string {
	tabs := make([]string, 0, len(m.cfg.Sections))
	for i, s := range m.cfg.Sections {
		label := fmt.Sprintf("%s %s", s.Title, dim.Render(sectionCount(m.sections[i])))
		style := tabIdle
		if i == m.active {
			label = fmt.Sprintf("%s %s", s.Title, sectionCount(m.sections[i]))
			style = tabActive
		}
		tabs = append(tabs, style.Render(label))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, tabs...) + "\n"
}

func sectionCount(s sectionData) string {
	if s.loading {
		return "…"
	}
	if s.err != nil {
		return "!"
	}
	return fmt.Sprint(len(s.issues))
}

func (m Model) listView() string {
	s := m.sections[m.active]
	if s.err != nil {
		return lipgloss.NewStyle().Foreground(errColor).Width(m.listWidth()).Render("Failed to load section: " + s.err.Error())
	}
	if len(s.issues) == 0 && s.loading {
		return dim.Render("Loading…")
	}
	if len(s.issues) == 0 {
		return dim.Render("No issues match this section's filter.")
	}
	return m.table.View()
}

func (m Model) sideView(width int, height int) string {
	inner := width - 4
	box := detailBox.Width(width).Height(height)
	if m.picker != nil {
		return box.Render(pickerView(*m.picker))
	}
	issue, ok := m.selected()
	if !ok {
		return box.Render(dim.Render("No issue selected"))
	}
	return box.Render(lipgloss.NewStyle().MaxHeight(height - 2).Render(detailView(issue, inner)))
}

func detailView(issue linear.Issue, width int) string {
	wrap := lipgloss.NewStyle().Width(width)
	meta := []string{
		dim.Render("Assignee ") + assigneeName(issue),
		dim.Render("Team     ") + issue.Team.Name,
		dim.Render("Priority ") + issue.PriorityLabel,
	}
	if len(issue.Labels) > 0 {
		meta = append(meta, dim.Render("Labels   ")+strings.Join(issue.Labels, ", "))
	}
	meta = append(meta, dim.Render("Branch   ")+issue.BranchName)

	description := truncate(issue.Description, maxDescriptionRunes)
	if description == "" {
		description = dim.Render("No description")
	}
	return strings.Join([]string{
		wrap.Render(dim.Render(issue.Identifier) + "  " + bold.Render(issue.Title)),
		stateBadge(issue.State),
		"",
		strings.Join(meta, "\n"),
		"",
		dim.Render(strings.Repeat("─", width)),
		wrap.Render(description),
	}, "\n")
}

func pickerView(p picker) string {
	lines := []string{bold.Render("Move " + p.issue.Identifier + " to"), ""}
	for i, s := range p.states {
		cursor := "  "
		if i == p.cursor {
			cursor = lipgloss.NewStyle().Foreground(accent).Render("▶ ")
		}
		lines = append(lines, cursor+stateBadge(s))
	}
	return strings.Join(append(lines, "", dim.Render("enter apply · esc cancel")), "\n")
}

func stateBadge(s linear.State) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(s.Color)).Render("● ") + s.Name
}

func (m Model) footerView() string {
	if m.flash == "" {
		return dim.Render(helpText)
	}
	color := okColor
	if m.flashErr {
		color = errColor
	}
	return lipgloss.NewStyle().Foreground(color).MaxWidth(m.width).Render(strings.Join(strings.Fields(m.flash), " "))
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
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
