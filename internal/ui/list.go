package ui

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// cell is one column of a list row; width 0 makes it the flexible column that takes the remaining space.
type cell struct {
	text  string
	width int
	color color.Color
	bold  bool
	// right aligns the text, used for ages and counts
	right bool
	// tight drops the gap before this cell, e.g. a state name right after its colored dot
	tight bool
}

// listRow is either a group header (header set), a blank spacer (both empty) or an item (cells set).
// Items may carry a second line (sub) that shares the row's selection highlight.
type listRow struct {
	header string
	count  int
	cells  []cell
	sub    []cell
}

func (r listRow) lines() int {
	if r.sub != nil {
		return 2
	}
	return 1
}

// list is a scrollable, selectable list of rows drawn cell by cell so the selection highlight spans styled text.
type list struct {
	rows   []listRow
	cursor int
	// offset is the first visible row; height is measured in terminal lines, not rows
	offset int
	height int
}

const cellGap = 2

var (
	selectedBg = lipgloss.Color("#2A2D4A")
	headerFg   = lipgloss.Color("#C9CCD6")
)

func (l list) Cursor() int {
	return l.cursor
}

func (l list) withRows(rows []listRow) list {
	l.rows = rows
	return l.withCursor(l.cursor)
}

func (l list) withHeight(h int) list {
	l.height = h
	return l.withCursor(l.cursor)
}

// withCursor clamps the cursor onto the rows and scrolls so the whole cursor row stays visible.
func (l list) withCursor(i int) list {
	l.cursor = max(min(i, len(l.rows)-1), 0)
	l.offset = max(min(l.offset, l.cursor), 0)
	for l.offset < l.cursor && l.linesBetween(l.offset, l.cursor) > l.height {
		l.offset++
	}
	return l
}

// linesBetween counts terminal lines used by rows from..to inclusive.
func (l list) linesBetween(from int, to int) int {
	n := 0
	for i := from; i <= to && i < len(l.rows); i++ {
		n += l.rows[i].lines()
	}
	return n
}

func (l list) view(width int) string {
	lines := make([]string, 0, l.height)
	for i := l.offset; i < len(l.rows) && len(lines)+l.rows[i].lines() <= l.height; i++ {
		lines = append(lines, renderRow(l.rows[i], width, i == l.cursor)...)
	}
	return strings.Join(lines, "\n")
}

func renderRow(r listRow, width int, selected bool) []string {
	if r.cells == nil {
		if r.header == "" {
			return []string{""}
		}
		return []string{" " + lipgloss.NewStyle().Bold(true).Foreground(headerFg).Render(r.header) + "  " + dim.Render(fmt.Sprint(r.count))}
	}
	lines := []string{renderCells(r.cells, width, selected)}
	if r.sub != nil {
		lines = append(lines, renderCells(r.sub, width, selected))
	}
	return lines
}

// renderCells lays out one line of cells; the selection background covers every cell and gap.
func renderCells(cells []cell, width int, selected bool) string {

	fixed := 0
	for i, c := range cells {
		fixed += c.width
		if i > 0 && !c.tight {
			fixed += cellGap
		}
	}
	// 2-column gutter for the selection bar
	flex := max(width-2-fixed, 8)

	gutter := "  "
	base := lipgloss.NewStyle()
	if selected {
		base = base.Background(selectedBg)
		gutter = lipgloss.NewStyle().Foreground(accent).Background(selectedBg).Render("▌ ")
	}
	gap := base.Render(strings.Repeat(" ", cellGap))

	parts := make([]string, 0, len(cells)*2)
	for i, c := range cells {
		w := c.width
		if w == 0 {
			w = flex
		}
		style := base.Width(w).MaxWidth(w)
		if c.color != nil {
			style = style.Foreground(c.color)
		}
		if c.bold {
			style = style.Bold(true)
		}
		if c.right {
			style = style.Align(lipgloss.Right)
		}
		if i > 0 && !c.tight {
			parts = append(parts, gap)
		}
		parts = append(parts, style.Render(ansi.Truncate(c.text, w, "…")))
	}
	return gutter + strings.Join(parts, "")
}
