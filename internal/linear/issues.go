package linear

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

type User struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

type Team struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
}

type State struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Type     string  `json:"type"`
	Color    string  `json:"color"`
	Position float64 `json:"position"`
}

type Issue struct {
	ID            string
	Identifier    string
	Title         string
	Description   string
	URL           string
	BranchName    string
	PriorityLabel string
	UpdatedAt     time.Time
	State         State
	Assignee      *User
	Team          Team
	Labels        []string
}

// issueNode mirrors the GraphQL shape; Issue flattens its connections.
type issueNode struct {
	ID            string    `json:"id"`
	Identifier    string    `json:"identifier"`
	Title         string    `json:"title"`
	Description   string    `json:"description"`
	URL           string    `json:"url"`
	BranchName    string    `json:"branchName"`
	PriorityLabel string    `json:"priorityLabel"`
	UpdatedAt     time.Time `json:"updatedAt"`
	State         State     `json:"state"`
	Assignee      *User     `json:"assignee"`
	Team          Team      `json:"team"`
	Labels        struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	} `json:"labels"`
}

func (n issueNode) toIssue() Issue {
	labels := make([]string, 0, len(n.Labels.Nodes))
	for _, l := range n.Labels.Nodes {
		labels = append(labels, l.Name)
	}
	return Issue{
		ID:            n.ID,
		Identifier:    n.Identifier,
		Title:         n.Title,
		Description:   n.Description,
		URL:           n.URL,
		BranchName:    n.BranchName,
		PriorityLabel: n.PriorityLabel,
		UpdatedAt:     n.UpdatedAt,
		State:         n.State,
		Assignee:      n.Assignee,
		Team:          n.Team,
		Labels:        labels,
	}
}

const issueFields = `
fragment IssueFields on Issue {
  id identifier title description url branchName priorityLabel updatedAt
  state { id name type color position }
  assignee { id name displayName }
  team { id key name }
  labels(first: 20) { nodes { name } }
}`

// Issues returns up to first issues matching a Linear IssueFilter, most recently updated first.
func (c *Client) Issues(ctx context.Context, filter json.RawMessage, first int) ([]Issue, error) {
	const query = `query Issues($filter: IssueFilter, $first: Int!) {
  issues(filter: $filter, first: $first, orderBy: updatedAt) { nodes { ...IssueFields } }
}` + issueFields

	var data struct {
		Issues struct {
			Nodes []issueNode `json:"nodes"`
		} `json:"issues"`
	}
	if err := c.do(ctx, "issues", query, map[string]any{"filter": filter, "first": first}, &data); err != nil {
		return nil, err
	}
	issues := make([]Issue, 0, len(data.Issues.Nodes))
	for _, n := range data.Issues.Nodes {
		issues = append(issues, n.toIssue())
	}
	return issues, nil
}

// Viewer returns the user who owns the API key.
func (c *Client) Viewer(ctx context.Context) (User, error) {
	const query = `query Viewer { viewer { id name displayName } }`
	var data struct {
		Viewer User `json:"viewer"`
	}
	if err := c.do(ctx, "viewer", query, map[string]any{}, &data); err != nil {
		return User{}, err
	}
	return data.Viewer, nil
}

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
	if err := c.do(ctx, "teamStates", query, map[string]any{"id": teamID}, &data); err != nil {
		return nil, err
	}
	states := append([]State(nil), data.Team.States.Nodes...)
	sort.SliceStable(states, func(i, j int) bool {
		if stateTypeOrder[states[i].Type] != stateTypeOrder[states[j].Type] {
			return stateTypeOrder[states[i].Type] < stateTypeOrder[states[j].Type]
		}
		return states[i].Position < states[j].Position
	})
	return states, nil
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

// SetState moves an issue to a workflow state and returns the updated issue.
func (c *Client) SetState(ctx context.Context, issueID string, stateID string) (Issue, error) {
	return c.updateIssue(ctx, "setState", issueID, map[string]any{"stateId": stateID})
}

// Assign sets an issue's assignee and returns the updated issue.
func (c *Client) Assign(ctx context.Context, issueID string, userID string) (Issue, error) {
	return c.updateIssue(ctx, "assign", issueID, map[string]any{"assigneeId": userID})
}

func (c *Client) updateIssue(ctx context.Context, operation string, issueID string, input map[string]any) (Issue, error) {
	const query = `mutation UpdateIssue($id: String!, $input: IssueUpdateInput!) {
  issueUpdate(id: $id, input: $input) { success issue { ...IssueFields } }
}` + issueFields

	var data struct {
		IssueUpdate struct {
			Success bool      `json:"success"`
			Issue   issueNode `json:"issue"`
		} `json:"issueUpdate"`
	}
	if err := c.do(ctx, operation, query, map[string]any{"id": issueID, "input": input}, &data); err != nil {
		return Issue{}, err
	}
	if !data.IssueUpdate.Success {
		return Issue{}, fmt.Errorf("linear %s: issueUpdate returned success=false for issue %s, input=%v", operation, issueID, input)
	}
	return data.IssueUpdate.Issue.toIssue(), nil
}
