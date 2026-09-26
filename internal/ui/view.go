package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/abgaryanharutyun/linear-dash/internal/config"
)

const (
	// below this terminal width the detail pane is hidden
	splitMinWidth = 110
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
	sideBox   = lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).BorderForeground(muted).Padding(0, 1)
)

var helpTexts = map[mode]string{
	modeList:        "tab section · / search · t group · s status · a assign · c comment · o open · O open PR · ? all keys · q quit",
	modeSearch:      "enter keep filter · esc clear",
	modePicker:      "type to filter · ↑/↓ move · enter apply · esc cancel",
	modeMultiPicker: "type to filter · ↑/↓ move · tab toggle · enter apply · esc cancel",
	modeCompose:     "ctrl+s send · esc cancel",
}

// overlays are the modes that replace the detail pane (or the list, on narrow terminals).
var overlays = map[mode]func(m Model, height int) string{
	modePicker:      func(m Model, height int) string { return pickerView(*m.picker, height) },
	modeMultiPicker: func(m Model, height int) string { return multiPickerView(*m.multi, height) },
	modeCompose:     func(m Model, height int) string { return composeView(*m.compose) },
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
	_, overlay := overlays[m.mode]
	if overlay && !m.showDetail() {
		return lipgloss.JoinVertical(lipgloss.Left, m.tabsView(), m.sideView(m.width, bodyHeight), m.footerView())
	}
	list := lipgloss.NewStyle().Width(m.listWidth()).Height(bodyHeight).MaxHeight(bodyHeight).Render(m.listView())
	body := list
	if m.showDetail() {
		body = lipgloss.JoinHorizontal(lipgloss.Top, list, m.sideView(m.sideWidth(), bodyHeight))
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

func (m Model) sideWidth() int {
	if !m.showDetail() {
		return m.width
	}
	return m.width - m.listWidth()
}

// syncList rebuilds the list rows for the active section, search query, grouping and size.
// The cursor follows the selected item by key, so re-sorting after an edit can't leave it on a different issue.
func (m Model) syncList() Model {
	selectedKey := ""
	if c := m.list.Cursor(); c >= 0 && c < len(m.rowKeys) {
		selectedKey = m.rowKeys[c]
	}
	data, kind := m.current()
	rows, index := visibleRows(data, kind, m.query, m.isGrouped(), time.Now())
	keys := make([]string, len(index))
	for r, i := range index {
		if i >= 0 {
			keys[r] = itemKeys[kind](data, i)
		}
	}
	m.rows, m.rowKeys = index, keys
	m.list = m.list.withRows(rows).withHeight(m.bodyHeight())
	if selectedKey != "" {
		if r := slices.Index(keys, selectedKey); r >= 0 {
			m.list = m.list.withCursor(r)
		}
	}
	return m.skipHeader(1)
}

// resetList starts a fresh list at the top, for switching to a different set of items.
func (m Model) resetList() Model {
	m.list = list{height: m.list.height}
	m.rowKeys = nil
	return m.syncList()
}

func (m Model) tabsView() string {
	tabs := make([]string, 0, len(m.cfg.Sections))
	for i, s := range m.cfg.Sections {
		title := s.Title
		count := sectionCount(m.sections[i], s.Kind)
		if i == m.active && m.drill != nil {
			title = s.Title + " › " + releaseLabel(m.drill.release)
			count = sectionCount(m.drill.data, config.KindIssues)
		}
		if i == m.active && m.query != "" {
			data, kind := m.current()
			if !data.loading && data.err == nil {
				count = fmt.Sprintf("%d/%d", countIssues(m.rows), itemCounts[kind](data))
			}
		}
		style, label := tabIdle, title+" "+dim.Render(count)
		if i == m.active {
			style, label = tabActive, title+" "+count
		}
		tabs = append(tabs, style.Render(label))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, tabs...) + "\n"
}

func countIssues(rows []int) int {
	n := 0
	for _, i := range rows {
		if i >= 0 {
			n++
		}
	}
	return n
}

func sectionCount(s sectionData, kind config.Kind) string {
	n := itemCounts[kind](s)
	if s.loading && n == 0 {
		return "…"
	}
	if s.err != nil {
		return "!"
	}
	return fmt.Sprint(n)
}

func (m Model) listView() string {
	s, kind := m.current()
	n := itemCounts[kind](s)
	if s.err != nil {
		return lipgloss.NewStyle().Foreground(errColor).Width(m.listWidth()).Render("Failed to load section: " + s.err.Error())
	}
	if n == 0 && s.loading {
		return dim.Render("Loading…")
	}
	if n == 0 {
		return dim.Render("Nothing in this section.")
	}
	if len(m.rows) == 0 {
		return dim.Render(fmt.Sprintf("No issues match %q (esc clears the filter).", m.query))
	}
	return m.list.view(m.listWidth())
}

func (m Model) sideView(width int, height int) string {
	inner := width - 4
	box := sideBox.Width(width).Height(height)
	content := ""
	if overlay, ok := overlays[m.mode]; ok {
		content = overlay(m, height-2)
	} else if m.showHelp {
		content = keysView()
	} else if issue, ok := m.selected(); ok {
		content = m.detailView(issue, inner)
	} else if release, ok := m.selectedRelease(); ok {
		content = m.releaseDetailView(release, inner)
	} else {
		content = dim.Render("No issue selected")
	}
	return box.Render(lipgloss.NewStyle().MaxHeight(height - 2).Render(content))
}

// keyHelp is the full key list shown by `?`.
var keyHelp = [][2]string{
	{"tab / shift+tab", "next / previous section"},
	{"j k  g G  ctrl+d ctrl+u", "move"},
	{"enter / esc", "open a release's issues / back"},
	{"/", "filter (enter keeps, esc clears)"},
	{"t", "group by state or pipeline"},
	{"o  O", "open issue · open linked PR"},
	{"y  b", "copy URL · copy branch"},
	{"B", "check out the issue's branch"},
	{"s", "change status"},
	{"a  A", "assign to me · to a teammate"},
	{"p  e  L", "priority · estimate · labels"},
	{"c", "comment (ctrl+s sends)"},
	{"m", "mark notification read"},
	{"r", "refresh"},
	{"?", "close this help"},
	{"q", "quit"},
}

func keysView() string {
	lines := []string{bold.Render("Keys"), ""}
	for _, k := range keyHelp {
		lines = append(lines, lipgloss.NewStyle().Foreground(accent).Width(26).Render(k[0])+dim.Render(k[1]))
	}
	return strings.Join(lines, "\n")
}

func (m Model) footerView() string {
	if m.mode == modeSearch {
		return m.search.View()
	}
	if m.flash == "" {
		return dim.MaxWidth(m.width).Render(helpTexts[m.mode])
	}
	color := okColor
	if m.flashErr {
		color = errColor
	}
	return lipgloss.NewStyle().Foreground(color).MaxWidth(m.width).Render(strings.Join(strings.Fields(m.flash), " "))
}
