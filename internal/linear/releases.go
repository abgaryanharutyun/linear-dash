package linear

import (
	"context"
	"sort"
	"time"
)

type ReleaseStage struct {
	Name string `json:"name"`
	// Type is one of planned, started, completed, canceled
	Type     string  `json:"type"`
	Color    string  `json:"color"`
	Position float64 `json:"position"`
}

type Release struct {
	ID          string
	Name        string
	Version     string
	URL         string
	Description string
	Stage       ReleaseStage
	Pipeline    string
	IssueCount  int
	// TargetDate is a calendar date (YYYY-MM-DD) or empty
	TargetDate  string
	StartedAt   *time.Time
	CompletedAt *time.Time
	UpdatedAt   time.Time
	NoteTitle   string
	NoteContent string
}

type releaseNode struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Version     *string      `json:"version"`
	URL         string       `json:"url"`
	Description *string      `json:"description"`
	Stage       ReleaseStage `json:"stage"`
	Pipeline    struct {
		Name string `json:"name"`
	} `json:"pipeline"`
	IssueCount  int        `json:"issueCount"`
	TargetDate  *string    `json:"targetDate"`
	StartedAt   *time.Time `json:"startedAt"`
	CompletedAt *time.Time `json:"completedAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	ReleaseNote *struct {
		Title           *string `json:"title"`
		DocumentContent *struct {
			Content *string `json:"content"`
		} `json:"documentContent"`
	} `json:"releaseNote"`
}

func (n releaseNode) toRelease() Release {
	r := Release{
		ID:          n.ID,
		Name:        clean(n.Name),
		Version:     clean(deref(n.Version)),
		URL:         n.URL,
		Description: clean(deref(n.Description)),
		Stage:       ReleaseStage{Name: clean(n.Stage.Name), Type: n.Stage.Type, Color: n.Stage.Color, Position: n.Stage.Position},
		Pipeline:    clean(n.Pipeline.Name),
		IssueCount:  n.IssueCount,
		TargetDate:  deref(n.TargetDate),
		StartedAt:   n.StartedAt,
		CompletedAt: n.CompletedAt,
		UpdatedAt:   n.UpdatedAt,
	}
	if n.ReleaseNote != nil {
		r.NoteTitle = clean(deref(n.ReleaseNote.Title))
		if n.ReleaseNote.DocumentContent != nil {
			r.NoteContent = clean(deref(n.ReleaseNote.DocumentContent.Content))
		}
	}
	return r
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// releaseStageOrder puts what's moving first: in progress, then planned, then finished.
var releaseStageOrder = map[string]int{
	"started":   0,
	"planned":   1,
	"completed": 2,
	"canceled":  3,
}

// ActiveReleases returns planned and in-progress releases plus those completed within recentWindow
// (an ISO 8601 duration such as P30D), in-progress first.
func (c *Client) ActiveReleases(ctx context.Context, recentWindow string, first int) ([]Release, error) {
	const query = `query Releases($filter: ReleaseFilter, $first: Int!) {
  releases(filter: $filter, first: $first, orderBy: updatedAt) {
    nodes {
      id name version url description issueCount targetDate startedAt completedAt updatedAt
      stage { name type color position }
      pipeline { name }
      releaseNote { title documentContent { content } }
    }
  }
}`
	filter := map[string]any{"or": []any{
		map[string]any{"stage": map[string]any{"type": map[string]any{"in": []string{"planned", "started"}}}},
		map[string]any{"completedAt": map[string]any{"gt": "-" + recentWindow}},
	}}
	var data struct {
		Releases struct {
			Nodes []releaseNode `json:"nodes"`
		} `json:"releases"`
	}
	if err := c.query(ctx, "releases", query, map[string]any{"filter": filter, "first": first}, &data); err != nil {
		return nil, err
	}
	releases := make([]Release, 0, len(data.Releases.Nodes))
	for _, n := range data.Releases.Nodes {
		releases = append(releases, n.toRelease())
	}
	sort.SliceStable(releases, func(i, j int) bool {
		return releaseStageOrder[releases[i].Stage.Type] < releaseStageOrder[releases[j].Stage.Type]
	})
	return releases, nil
}

// ReleaseIssues returns up to first issues in a release, most recently updated first.
func (c *Client) ReleaseIssues(ctx context.Context, releaseID string, first int) ([]Issue, error) {
	const query = `query ReleaseIssues($id: String!, $first: Int!) {
  release(id: $id) { issues(first: $first, orderBy: updatedAt) { nodes { ...IssueFields } } }
}` + issueFields

	var data struct {
		Release struct {
			Issues struct {
				Nodes []issueNode `json:"nodes"`
			} `json:"issues"`
		} `json:"release"`
	}
	if err := c.query(ctx, "releaseIssues", query, map[string]any{"id": releaseID, "first": first}, &data); err != nil {
		return nil, err
	}
	issues := make([]Issue, 0, len(data.Release.Issues.Nodes))
	for _, n := range data.Release.Issues.Nodes {
		issues = append(issues, n.toIssue())
	}
	return issues, nil
}
