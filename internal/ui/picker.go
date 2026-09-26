package ui

import (
	"context"
	"fmt"
	"maps"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/abgaryanharutyun/linear-dash/internal/linear"
)

type option struct {
	label string
	// color tints the dot drawn before the label; empty draws no dot
	color string
	value string
}

// picker chooses one option; typing filters the list.
type picker struct {
	title   string
	options []option
	filter  textinput.Model
	cursor  int
	apply   func(option) tea.Cmd
}

// multiPicker toggles any number of options, used for labels.
type multiPicker struct {
	title   string
	options []option
	filter  textinput.Model
	cursor  int
	chosen  map[string]bool
	apply   func(values []string) tea.Cmd
}

// compose writes a Markdown comment.
type compose struct {
	issue linear.Issue
	input textarea.Model
	send  func(body string) tea.Cmd
}

type pickerMsg struct {
	picker *picker
	err    error
}

type multiPickerMsg struct {
	picker *multiPicker
	err    error
}

var pickerActions = map[string]func(m Model) (Model, tea.Cmd){
	"up":     func(m Model) (Model, tea.Cmd) { return m.movePicker(-1), nil },
	"ctrl+p": func(m Model) (Model, tea.Cmd) { return m.movePicker(-1), nil },
	"ctrl+k": func(m Model) (Model, tea.Cmd) { return m.movePicker(-1), nil },
	"down":   func(m Model) (Model, tea.Cmd) { return m.movePicker(1), nil },
	"ctrl+n": func(m Model) (Model, tea.Cmd) { return m.movePicker(1), nil },
	"ctrl+j": func(m Model) (Model, tea.Cmd) { return m.movePicker(1), nil },
	"enter":  applyPicker,
	"esc":    cancelOverlay,
	"ctrl+c": quit,
}

var multiPickerActions = map[string]func(m Model) (Model, tea.Cmd){
	"up":     func(m Model) (Model, tea.Cmd) { return m.moveMulti(-1), nil },
	"ctrl+p": func(m Model) (Model, tea.Cmd) { return m.moveMulti(-1), nil },
	"ctrl+k": func(m Model) (Model, tea.Cmd) { return m.moveMulti(-1), nil },
	"down":   func(m Model) (Model, tea.Cmd) { return m.moveMulti(1), nil },
	"ctrl+n": func(m Model) (Model, tea.Cmd) { return m.moveMulti(1), nil },
	"ctrl+j": func(m Model) (Model, tea.Cmd) { return m.moveMulti(1), nil },
	"tab":    toggleMulti,
	"enter":  applyMulti,
	"esc":    cancelOverlay,
	"ctrl+c": quit,
}

var composeActions = map[string]func(m Model) (Model, tea.Cmd){
	"ctrl+s": sendComment,
	"esc":    cancelOverlay,
	"ctrl+c": quit,
}

// priorities are Linear's fixed priority values.
var priorities = []option{
	{label: "No priority", value: "0"},
	{label: "Urgent", color: "#F2555A", value: "1"},
	{label: "High", color: "#F2994A", value: "2"},
	{label: "Medium", color: "#F2C94C", value: "3"},
	{label: "Low", color: "#8A8F98", value: "4"},
}

// estimateScales maps a team's issueEstimationType to its point values and their labels.
var estimateScales = map[string][]option{
	"exponential": {{label: "1", value: "1"}, {label: "2", value: "2"}, {label: "4", value: "4"}, {label: "8", value: "8"}, {label: "16", value: "16"}},
	"fibonacci":   {{label: "1", value: "1"}, {label: "2", value: "2"}, {label: "3", value: "3"}, {label: "5", value: "5"}, {label: "8", value: "8"}},
	"linear":      {{label: "1", value: "1"}, {label: "2", value: "2"}, {label: "3", value: "3"}, {label: "4", value: "4"}, {label: "5", value: "5"}},
	"tShirt":      {{label: "XS", value: "1"}, {label: "S", value: "2"}, {label: "M", value: "3"}, {label: "L", value: "5"}, {label: "XL", value: "8"}},
}

func newFilter() textinput.Model {
	f := textinput.New()
	f.Prompt = "filter: "
	return f
}

func newPicker(title string, options []option, cursor int, apply func(option) tea.Cmd) *picker {
	return &picker{title: title, options: options, filter: newFilter(), cursor: cursor, apply: apply}
}

func newCompose(issue linear.Issue, client *linear.Client, width int) compose {
	input := textarea.New()
	input.Placeholder = "Write a comment (Markdown)…"
	input.ShowLineNumbers = false
	input.SetWidth(max(width-4, 20))
	input.SetHeight(8)
	return compose{
		issue: issue,
		input: input,
		send: func(body string) tea.Cmd {
			return func() tea.Msg {
				return commentMsg{issue: issue, body: body, err: client.CreateComment(context.Background(), issue.ID, body)}
			}
		},
	}
}

func handlePickerKey(m Model, msg tea.KeyPressMsg) (Model, tea.Cmd) {
	if act, ok := pickerActions[msg.String()]; ok {
		return act(m)
	}
	p := *m.picker
	var cmd tea.Cmd
	p.filter, cmd = p.filter.Update(msg)
	p.cursor = 0
	m.picker = &p
	return m, cmd
}

func handleMultiPickerKey(m Model, msg tea.KeyPressMsg) (Model, tea.Cmd) {
	if act, ok := multiPickerActions[msg.String()]; ok {
		return act(m)
	}
	p := *m.multi
	var cmd tea.Cmd
	p.filter, cmd = p.filter.Update(msg)
	p.cursor = 0
	m.multi = &p
	return m, cmd
}

func handleComposeKey(m Model, msg tea.KeyPressMsg) (Model, tea.Cmd) {
	if act, ok := composeActions[msg.String()]; ok {
		return act(m)
	}
	c := *m.compose
	var cmd tea.Cmd
	c.input, cmd = c.input.Update(msg)
	m.compose = &c
	return m, cmd
}

func (m Model) movePicker(delta int) Model {
	p := *m.picker
	p.cursor = clampCursor(p.cursor+delta, len(filterOptions(p.options, p.filter.Value())))
	m.picker = &p
	return m
}

func (m Model) moveMulti(delta int) Model {
	p := *m.multi
	p.cursor = clampCursor(p.cursor+delta, len(filterOptions(p.options, p.filter.Value())))
	m.multi = &p
	return m
}

func clampCursor(i int, n int) int {
	return max(min(i, n-1), 0)
}

func applyPicker(m Model) (Model, tea.Cmd) {
	p := *m.picker
	visible := filterOptions(p.options, p.filter.Value())
	if len(visible) == 0 {
		return m.withError("nothing matches the filter"), nil
	}
	choice := visible[p.cursor]
	return m.backToList().withInfo(fmt.Sprintf("%s: %s…", p.title, choice.label)), p.apply(choice)
}

func toggleMulti(m Model) (Model, tea.Cmd) {
	p := *m.multi
	visible := filterOptions(p.options, p.filter.Value())
	if len(visible) == 0 {
		return m, nil
	}
	chosen := maps.Clone(p.chosen)
	value := visible[p.cursor].value
	chosen[value] = !chosen[value]
	p.chosen = chosen
	m.multi = &p
	return m, nil
}

func applyMulti(m Model) (Model, tea.Cmd) {
	p := *m.multi
	values := make([]string, 0, len(p.chosen))
	for _, o := range p.options {
		if p.chosen[o.value] {
			values = append(values, o.value)
		}
	}
	return m.backToList().withInfo(p.title + "…"), p.apply(values)
}

func sendComment(m Model) (Model, tea.Cmd) {
	c := *m.compose
	body := strings.TrimSpace(c.input.Value())
	if body == "" {
		return m.withError("comment is empty"), nil
	}
	return m.backToList().withInfo("posting comment on " + c.issue.Identifier), c.send(body)
}

func cancelOverlay(m Model) (Model, tea.Cmd) {
	return m.backToList(), nil
}

func filterOptions(options []option, query string) []option {
	if query == "" {
		return options
	}
	q := strings.ToLower(query)
	out := make([]option, 0, len(options))
	for _, o := range options {
		if strings.Contains(strings.ToLower(o.label), q) {
			out = append(out, o)
		}
	}
	return out
}

func optionIndex(options []option, value string) int {
	for i, o := range options {
		if o.value == value {
			return i
		}
	}
	return 0
}

func failCmd(err error) tea.Cmd {
	return func() tea.Msg { return updatedMsg{err: err} }
}

// updateCmd wraps an issue mutation into an updatedMsg with a readable success message.
func updateCmd(issue linear.Issue, done string, run func(ctx context.Context) (linear.Issue, error)) tea.Cmd {
	return func() tea.Msg {
		updated, err := run(context.Background())
		if err != nil {
			err = fmt.Errorf("%s: %w", done, err)
		}
		return updatedMsg{issue: updated, done: done, err: err}
	}
}

func pickState(m Model) (Model, tea.Cmd) {
	issue, ok := m.selected()
	if !ok {
		return m, nil
	}
	client := m.client
	return m.withInfo("loading states for " + issue.Team.Key), func() tea.Msg {
		states, err := client.TeamStates(context.Background(), issue.Team.ID)
		if err != nil {
			return pickerMsg{err: fmt.Errorf("load states for %s: %w", issue.Team.Key, err)}
		}
		if len(states) == 0 {
			return pickerMsg{err: fmt.Errorf("team %s has no workflow states", issue.Team.Key)}
		}
		options := make([]option, 0, len(states))
		byID := make(map[string]linear.State, len(states))
		for _, s := range states {
			options = append(options, option{label: s.Name, color: s.Color, value: s.ID})
			byID[s.ID] = s
		}
		return pickerMsg{picker: newPicker("Move "+issue.Identifier, options, optionIndex(options, issue.State.ID), func(o option) tea.Cmd {
			return updateCmd(issue, fmt.Sprintf("%s → %s", issue.Identifier, o.label), func(ctx context.Context) (linear.Issue, error) {
				return client.SetState(ctx, issue.ID, byID[o.value].ID)
			})
		})}
	}
}

func pickAssignee(m Model) (Model, tea.Cmd) {
	issue, ok := m.selected()
	if !ok {
		return m, nil
	}
	client := m.client
	return m.withInfo("loading " + issue.Team.Key + " members"), func() tea.Msg {
		users, err := client.TeamMembers(context.Background(), issue.Team.ID)
		if err != nil {
			return pickerMsg{err: fmt.Errorf("load members of %s: %w", issue.Team.Key, err)}
		}
		options := make([]option, 0, len(users))
		byID := make(map[string]linear.User, len(users))
		for _, u := range users {
			options = append(options, option{label: fmt.Sprintf("%s (%s)", u.DisplayName, u.Name), value: u.ID})
			byID[u.ID] = u
		}
		current := ""
		if issue.Assignee != nil {
			current = issue.Assignee.ID
		}
		return pickerMsg{picker: newPicker("Assign "+issue.Identifier, options, optionIndex(options, current), func(o option) tea.Cmd {
			return assign(client, issue, byID[o.value])
		})}
	}
}

func pickPriority(m Model) (Model, tea.Cmd) {
	issue, ok := m.selected()
	if !ok {
		return m, nil
	}
	client := m.client
	p := newPicker("Priority "+issue.Identifier, priorities, optionIndex(priorities, fmt.Sprint(issue.Priority)), func(o option) tea.Cmd {
		priority, err := strconv.Atoi(o.value)
		if err != nil {
			return failCmd(fmt.Errorf("priority option %q is not a number: %w", o.value, err))
		}
		return updateCmd(issue, fmt.Sprintf("%s priority → %s", issue.Identifier, o.label), func(ctx context.Context) (linear.Issue, error) {
			return client.SetPriority(ctx, issue.ID, priority)
		})
	})
	m.picker, m.mode = p, modePicker
	return m, m.picker.filter.Focus()
}

func pickEstimate(m Model) (Model, tea.Cmd) {
	issue, ok := m.selected()
	if !ok {
		return m, nil
	}
	client := m.client
	return m.withInfo("loading " + issue.Team.Key + " estimate scale"), func() tea.Msg {
		est, err := client.TeamEstimation(context.Background(), issue.Team.ID)
		if err != nil {
			return pickerMsg{err: fmt.Errorf("load estimate scale for %s: %w", issue.Team.Key, err)}
		}
		scale, ok := estimateScales[est.Type]
		if !ok {
			return pickerMsg{err: fmt.Errorf("team %s doesn't use estimates (issueEstimationType=%s)", issue.Team.Key, est.Type)}
		}
		options := []option{{label: "No estimate", value: ""}}
		if est.AllowZero {
			options = append(options, option{label: "0", value: "0"})
		}
		options = append(options, scale...)
		current := ""
		if issue.Estimate != nil {
			current = fmt.Sprint(int(*issue.Estimate))
		}
		return pickerMsg{picker: newPicker("Estimate "+issue.Identifier, options, optionIndex(options, current), func(o option) tea.Cmd {
			var estimate *int
			if o.value != "" {
				points, err := strconv.Atoi(o.value)
				if err != nil {
					return failCmd(fmt.Errorf("estimate option %q is not a number: %w", o.value, err))
				}
				estimate = &points
			}
			return updateCmd(issue, fmt.Sprintf("%s estimate → %s", issue.Identifier, o.label), func(ctx context.Context) (linear.Issue, error) {
				return client.SetEstimate(ctx, issue.ID, estimate)
			})
		})}
	}
}

func pickLabels(m Model) (Model, tea.Cmd) {
	issue, ok := m.selected()
	if !ok {
		return m, nil
	}
	client := m.client
	return m.withInfo("loading " + issue.Team.Key + " labels"), func() tea.Msg {
		labels, err := client.TeamLabels(context.Background(), issue.Team.ID)
		if err != nil {
			return multiPickerMsg{err: fmt.Errorf("load labels for %s: %w", issue.Team.Key, err)}
		}
		options := make([]option, 0, len(labels))
		for _, l := range labels {
			options = append(options, option{label: l.Name, color: l.Color, value: l.ID})
		}
		chosen := make(map[string]bool, len(issue.Labels))
		for _, l := range issue.Labels {
			chosen[l.ID] = true
		}
		return multiPickerMsg{picker: &multiPicker{
			title:   "Labels " + issue.Identifier,
			options: options,
			filter:  newFilter(),
			chosen:  chosen,
			apply: func(values []string) tea.Cmd {
				added, removed := labelDiff(issue.Labels, values, options)
				if len(added) == 0 && len(removed) == 0 {
					return func() tea.Msg { return infoMsg{text: issue.Identifier + " labels unchanged"} }
				}
				return updateCmd(issue, fmt.Sprintf("%s labels updated", issue.Identifier), func(ctx context.Context) (linear.Issue, error) {
					return client.ChangeLabels(ctx, issue.ID, added, removed)
				})
			},
		}}
	}
}

// labelDiff compares the picker's result with the issue's labels. Only labels the picker offered can be
// removed, so labels it never loaded (other teams, past the page size) stay on the issue.
func labelDiff(current []linear.Label, chosen []string, offered []option) ([]string, []string) {
	had := make(map[string]bool, len(current))
	for _, l := range current {
		had[l.ID] = true
	}
	want := make(map[string]bool, len(chosen))
	for _, id := range chosen {
		want[id] = true
	}
	added, removed := []string{}, []string{}
	for _, o := range offered {
		switch {
		case want[o.value] && !had[o.value]:
			added = append(added, o.value)
		case had[o.value] && !want[o.value]:
			removed = append(removed, o.value)
		}
	}
	return added, removed
}

func pickerView(p picker, height int) string {
	visible := filterOptions(p.options, p.filter.Value())
	lines := []string{bold.Render(p.title), p.filter.View(), ""}
	lines = append(lines, optionLines(visible, p.cursor, func(option) string { return "" }, height-6)...)
	return strings.Join(append(lines, "", dim.Render("type to filter · ↑/↓ move · enter apply · esc cancel")), "\n")
}

func multiPickerView(p multiPicker, height int) string {
	visible := filterOptions(p.options, p.filter.Value())
	lines := []string{bold.Render(p.title), p.filter.View(), ""}
	check := func(o option) string {
		if p.chosen[o.value] {
			return "[x] "
		}
		return "[ ] "
	}
	lines = append(lines, optionLines(visible, p.cursor, check, height-6)...)
	return strings.Join(append(lines, "", dim.Render("type to filter · ↑/↓ move · tab toggle · enter apply · esc cancel")), "\n")
}

// optionLines renders a window of options around the cursor so long lists stay navigable.
func optionLines(options []option, cursor int, prefix func(option) string, height int) []string {
	height = max(height, 3)
	start := max(min(cursor-height/2, len(options)-height), 0)
	end := min(start+height, len(options))
	lines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		o := options[i]
		pointer := "  "
		if i == cursor {
			pointer = lipgloss.NewStyle().Foreground(accent).Render("▶ ")
		}
		dot := ""
		if o.color != "" {
			dot = lipgloss.NewStyle().Foreground(lipgloss.Color(o.color)).Render("● ")
		}
		lines = append(lines, pointer+prefix(o)+dot+o.label)
	}
	if len(options) == 0 {
		lines = append(lines, dim.Render("  no matches"))
	}
	return lines
}

func composeView(c compose) string {
	return strings.Join([]string{
		bold.Render("Comment on " + c.issue.Identifier),
		dim.Render(c.issue.Title),
		"",
		c.input.View(),
		"",
		dim.Render("ctrl+s send · esc cancel"),
	}, "\n")
}
