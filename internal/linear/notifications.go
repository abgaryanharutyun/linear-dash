package linear

import (
	"context"
	"fmt"
	"time"
)

// Notification is an inbox entry about an issue; notifications about projects, docs etc. are skipped.
type Notification struct {
	ID          string
	Type        string
	ReadAt      *time.Time
	CreatedAt   time.Time
	Actor       string
	CommentBody string
	Issue       Issue
}

type notificationNode struct {
	ID        string     `json:"id"`
	Type      string     `json:"type"`
	ReadAt    *time.Time `json:"readAt"`
	CreatedAt time.Time  `json:"createdAt"`
	Actor     *User      `json:"actor"`
	Issue     *issueNode `json:"issue"`
	Comment   *struct {
		Body string `json:"body"`
	} `json:"comment"`
}

// Notifications returns up to first inbox notifications about issues, newest first.
func (c *Client) Notifications(ctx context.Context, first int) ([]Notification, error) {
	const query = `query Notifications($first: Int!) {
  notifications(first: $first) {
    nodes {
      id type readAt createdAt
      actor { id name displayName }
      ... on IssueNotification { issue { ...IssueFields } comment { body } }
    }
  }
}` + issueFields

	var data struct {
		Notifications struct {
			Nodes []notificationNode `json:"nodes"`
		} `json:"notifications"`
	}
	if err := c.query(ctx, "notifications", query, map[string]any{"first": first}, &data); err != nil {
		return nil, err
	}
	notes := make([]Notification, 0, len(data.Notifications.Nodes))
	for _, n := range data.Notifications.Nodes {
		if n.Issue == nil {
			continue
		}
		note := Notification{ID: n.ID, Type: n.Type, ReadAt: n.ReadAt, CreatedAt: n.CreatedAt, Issue: n.Issue.toIssue()}
		if n.Actor != nil {
			note.Actor = clean(n.Actor.DisplayName)
		}
		if n.Comment != nil {
			note.CommentBody = clean(n.Comment.Body)
		}
		notes = append(notes, note)
	}
	return notes, nil
}

// MarkNotificationRead marks one notification read at readAt.
func (c *Client) MarkNotificationRead(ctx context.Context, id string, readAt time.Time) error {
	const query = `mutation MarkRead($id: String!, $input: NotificationUpdateInput!) {
  notificationUpdate(id: $id, input: $input) { success }
}`
	var data struct {
		NotificationUpdate struct {
			Success bool `json:"success"`
		} `json:"notificationUpdate"`
	}
	input := map[string]any{"readAt": readAt.UTC().Format(time.RFC3339)}
	if err := c.mutate(ctx, "markNotificationRead", query, map[string]any{"id": id, "input": input}, &data); err != nil {
		return err
	}
	if !data.NotificationUpdate.Success {
		return fmt.Errorf("linear markNotificationRead: success=false for notification %s", id)
	}
	return nil
}
