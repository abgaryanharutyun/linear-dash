package linear

import (
	"context"
	"sort"
	"time"
)

type Comment struct {
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
	User      *User     `json:"user"`
}

type SubIssue struct {
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	State      State  `json:"state"`
}

type Attachment struct {
	Title      string `json:"title"`
	Subtitle   string `json:"subtitle"`
	URL        string `json:"url"`
	SourceType string `json:"sourceType"`
}

// Detail is the extra data shown in the detail pane, fetched only for the selected issue.
type Detail struct {
	Comments    []Comment
	Children    []SubIssue
	Attachments []Attachment
}

// IssueDetail returns an issue's comments (oldest first), sub-issues and attachments.
func (c *Client) IssueDetail(ctx context.Context, issueID string) (Detail, error) {
	const query = `query IssueDetail($id: String!) {
  issue(id: $id) {
    comments(first: 50, orderBy: createdAt) { nodes { body createdAt user { id name displayName } } }
    children(first: 50) { nodes { identifier title state { id name type color position } } }
    attachments(first: 20) { nodes { title subtitle url sourceType } }
  }
}`
	var data struct {
		Issue struct {
			Comments struct {
				Nodes []Comment `json:"nodes"`
			} `json:"comments"`
			Children struct {
				Nodes []SubIssue `json:"nodes"`
			} `json:"children"`
			Attachments struct {
				Nodes []Attachment `json:"nodes"`
			} `json:"attachments"`
		} `json:"issue"`
	}
	if err := c.query(ctx, "issueDetail", query, map[string]any{"id": issueID}, &data); err != nil {
		return Detail{}, err
	}
	comments := make([]Comment, 0, len(data.Issue.Comments.Nodes))
	for _, c := range data.Issue.Comments.Nodes {
		comments = append(comments, Comment{Body: clean(c.Body), CreatedAt: c.CreatedAt, User: cleanUser(c.User)})
	}
	children := make([]SubIssue, 0, len(data.Issue.Children.Nodes))
	for _, c := range data.Issue.Children.Nodes {
		children = append(children, SubIssue{Identifier: c.Identifier, Title: clean(c.Title), State: c.State})
	}
	sort.Slice(comments, func(i, j int) bool { return comments[i].CreatedAt.Before(comments[j].CreatedAt) })
	return Detail{
		Comments:    comments,
		Children:    children,
		Attachments: append([]Attachment(nil), data.Issue.Attachments.Nodes...),
	}, nil
}
