package linear

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// clean strips escape sequences and control characters (keeping newlines and tabs) from text written by
// other people, so a title can't hide a hyperlink or redraw the terminal.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, ansi.Strip(s))
}

func cleanUser(u *User) *User {
	if u == nil {
		return nil
	}
	return &User{ID: u.ID, Name: clean(u.Name), DisplayName: clean(u.DisplayName)}
}

func cleanState(s State) State {
	s.Name = clean(s.Name)
	return s
}

func cleanLabels(labels []Label) []Label {
	out := make([]Label, 0, len(labels))
	for _, l := range labels {
		out = append(out, Label{ID: l.ID, Name: clean(l.Name), Color: l.Color})
	}
	return out
}
