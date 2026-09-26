package linear

import (
	"context"
	"sort"
)

// TeamStates returns a team's workflow states in board order (by type, then position).
func (c *Client) TeamStates(ctx context.Context, teamID string) ([]State, error) {
	const query = `query TeamStates($id: String!) {
  team(id: $id) { states { nodes { id name type color position } } }
}`
	var data struct {
		Team struct {
			States struct {
				Nodes []State `json:"nodes"`
			} `json:"states"`
		} `json:"team"`
	}
	if err := c.query(ctx, "teamStates", query, map[string]any{"id": teamID}, &data); err != nil {
		return nil, err
	}
	return SortStates(data.Team.States.Nodes), nil
}

// SortStates orders states the way Linear's boards do: by state type, then position.
func SortStates(states []State) []State {
	out := append([]State(nil), states...)
	sort.SliceStable(out, func(i, j int) bool { return StateLess(out[i], out[j]) })
	return out
}

// StateLess reports whether a comes before b on a Linear board.
func StateLess(a State, b State) bool {
	if stateTypeOrder[a.Type] != stateTypeOrder[b.Type] {
		return stateTypeOrder[a.Type] < stateTypeOrder[b.Type]
	}
	return a.Position < b.Position
}

// stateTypeOrder is the column order Linear uses on its boards.
var stateTypeOrder = map[string]int{
	"triage":    0,
	"backlog":   1,
	"unstarted": 2,
	"started":   3,
	"completed": 4,
	"canceled":  5,
	"duplicate": 6,
}

// TeamLabels returns the labels usable on a team's issues, including workspace-wide labels.
func (c *Client) TeamLabels(ctx context.Context, teamID string) ([]Label, error) {
	const query = `query TeamLabels($id: ID) {
  issueLabels(first: 250, filter: { or: [{ team: { id: { eq: $id } } }, { team: { null: true } }] }) {
    nodes { id name color }
  }
}`
	var data struct {
		IssueLabels struct {
			Nodes []Label `json:"nodes"`
		} `json:"issueLabels"`
	}
	if err := c.query(ctx, "teamLabels", query, map[string]any{"id": teamID}, &data); err != nil {
		return nil, err
	}
	labels := append([]Label(nil), data.IssueLabels.Nodes...)
	sort.Slice(labels, func(i, j int) bool { return labels[i].Name < labels[j].Name })
	return labels, nil
}

// TeamMembers returns a team's active members sorted by display name.
func (c *Client) TeamMembers(ctx context.Context, teamID string) ([]User, error) {
	const query = `query TeamMembers($id: String!) {
  team(id: $id) { members(first: 250) { nodes { id name displayName } } }
}`
	var data struct {
		Team struct {
			Members struct {
				Nodes []User `json:"nodes"`
			} `json:"members"`
		} `json:"team"`
	}
	if err := c.query(ctx, "teamMembers", query, map[string]any{"id": teamID}, &data); err != nil {
		return nil, err
	}
	users := append([]User(nil), data.Team.Members.Nodes...)
	sort.Slice(users, func(i, j int) bool { return users[i].DisplayName < users[j].DisplayName })
	return users, nil
}

// Estimation is a team's estimate scale settings.
type Estimation struct {
	// Type is one of notUsed, exponential, fibonacci, linear, tShirt
	Type      string `json:"issueEstimationType"`
	AllowZero bool   `json:"issueEstimationAllowZero"`
}

// TeamEstimation returns how a team estimates issues.
func (c *Client) TeamEstimation(ctx context.Context, teamID string) (Estimation, error) {
	const query = `query TeamEstimation($id: String!) {
  team(id: $id) { issueEstimationType issueEstimationAllowZero }
}`
	var data struct {
		Team Estimation `json:"team"`
	}
	if err := c.query(ctx, "teamEstimation", query, map[string]any{"id": teamID}, &data); err != nil {
		return Estimation{}, err
	}
	return data.Team, nil
}
