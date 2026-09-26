package linear

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"strings"
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

type Label struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// PullRequestLink is a GitHub PR attached to an issue, as Linear's GitHub integration reports it.
type PullRequestLink struct {
	URL    string
	Title  string
	Number int
	Repo   string
	// Status is Linear's PR status, e.g. draft, open, inReview, approved, merged, closed
	Status string
}

type Issue struct {
	ID            string
	Identifier    string
	Title         string
	Description   string
	URL           string
	BranchName    string
	Priority      int
	PriorityLabel string
	// Estimate is nil when the issue has no estimate
	Estimate  *float64
	UpdatedAt time.Time
	State     State
	Assignee  *User
	Team      Team
	Labels    []Label
	// PullRequests are the linked GitHub PRs, ones needing attention first
	PullRequests []PullRequestLink
}

// issueNode mirrors the GraphQL shape; Issue flattens its connections.
type issueNode struct {
	ID            string    `json:"id"`
	Identifier    string    `json:"identifier"`
	Title         string    `json:"title"`
	Description   string    `json:"description"`
	URL           string    `json:"url"`
	BranchName    string    `json:"branchName"`
	Priority      float64   `json:"priority"`
	PriorityLabel string    `json:"priorityLabel"`
	Estimate      *float64  `json:"estimate"`
	UpdatedAt     time.Time `json:"updatedAt"`
	State         State     `json:"state"`
	Assignee      *User     `json:"assignee"`
	Team          Team      `json:"team"`
	Labels        struct {
		Nodes []Label `json:"nodes"`
	} `json:"labels"`
	Attachments struct {
		Nodes []attachmentNode `json:"nodes"`
	} `json:"attachments"`
}

type attachmentNode struct {
	URL        string          `json:"url"`
	Title      string          `json:"title"`
	SourceType string          `json:"sourceType"`
	Metadata   json.RawMessage `json:"metadata"`
}

// prMetadata is the part of a GitHub attachment's metadata we read.
type prMetadata struct {
	Number   int    `json:"number"`
	RepoName string `json:"repoName"`
	Status   string `json:"status"`
	Draft    bool   `json:"draft"`
}

// prStatusRank puts PRs that still need attention before finished ones; unknown statuses sort in between.
var prStatusRank = map[string]int{
	"changesRequested": 0,
	"inReview":         1,
	"open":             2,
	"approved":         3,
	"draft":            4,
	"merged":           6,
	"closed":           7,
}

// unrankedPRStatus sits between active and finished PRs.
const unrankedPRStatus = 5

func rankPRStatus(status string) int {
	if rank, ok := prStatusRank[status]; ok {
		return rank
	}
	return unrankedPRStatus
}

// pullRequests keeps GitHub PR attachments, most relevant first; metadata that doesn't parse still yields the link.
func pullRequests(nodes []attachmentNode) []PullRequestLink {
	prs := make([]PullRequestLink, 0, len(nodes))
	for _, a := range nodes {
		if a.SourceType != "github" || !strings.Contains(a.URL, "/pull/") {
			continue
		}
		var meta prMetadata
		if len(a.Metadata) > 0 {
			if err := json.Unmarshal(a.Metadata, &meta); err != nil {
				slog.Warn("unreadable GitHub attachment metadata", "url", a.URL, "error", err)
			}
		}
		status := meta.Status
		if meta.Draft {
			status = "draft"
		}
		prs = append(prs, PullRequestLink{URL: a.URL, Title: clean(a.Title), Number: meta.Number, Repo: clean(meta.RepoName), Status: status})
	}
	sort.SliceStable(prs, func(i, j int) bool { return rankPRStatus(prs[i].Status) < rankPRStatus(prs[j].Status) })
	return prs
}

func (n issueNode) toIssue() Issue {
	return Issue{
		ID:            n.ID,
		Identifier:    n.Identifier,
		Title:         clean(n.Title),
		Description:   clean(n.Description),
		URL:           n.URL,
		BranchName:    n.BranchName,
		Priority:      int(n.Priority),
		PriorityLabel: n.PriorityLabel,
		Estimate:      n.Estimate,
		UpdatedAt:     n.UpdatedAt,
		State:         cleanState(n.State),
		Assignee:      cleanUser(n.Assignee),
		Team:          Team{ID: n.Team.ID, Key: clean(n.Team.Key), Name: clean(n.Team.Name)},
		Labels:        cleanLabels(n.Labels.Nodes),
		PullRequests:  pullRequests(n.Attachments.Nodes),
	}
}

const issueFields = `
fragment IssueFields on Issue {
  id identifier title description url branchName priority priorityLabel estimate updatedAt
  state { id name type color position }
  assignee { id name displayName }
  team { id key name }
  labels(first: 20) { nodes { id name color } }
  attachments(first: 10) { nodes { url title sourceType metadata } }
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
	if err := c.query(ctx, "issues", query, map[string]any{"filter": filter, "first": first}, &data); err != nil {
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
	if err := c.query(ctx, "viewer", query, map[string]any{}, &data); err != nil {
		return User{}, err
	}
	return data.Viewer, nil
}
