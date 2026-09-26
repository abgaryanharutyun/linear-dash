package linear

import (
	"context"
	"fmt"
)

// SetState moves an issue to a workflow state and returns the updated issue.
func (c *Client) SetState(ctx context.Context, issueID string, stateID string) (Issue, error) {
	return c.updateIssue(ctx, "setState", issueID, map[string]any{"stateId": stateID})
}

// Assign sets an issue's assignee and returns the updated issue.
func (c *Client) Assign(ctx context.Context, issueID string, userID string) (Issue, error) {
	return c.updateIssue(ctx, "assign", issueID, map[string]any{"assigneeId": userID})
}

// SetPriority sets an issue's priority: 0 none, 1 urgent, 2 high, 3 medium, 4 low.
func (c *Client) SetPriority(ctx context.Context, issueID string, priority int) (Issue, error) {
	return c.updateIssue(ctx, "setPriority", issueID, map[string]any{"priority": priority})
}

// SetEstimate sets an issue's estimate in points; nil clears it.
func (c *Client) SetEstimate(ctx context.Context, issueID string, estimate *int) (Issue, error) {
	return c.updateIssue(ctx, "setEstimate", issueID, map[string]any{"estimate": estimate})
}

// ChangeLabels adds and removes labels, leaving any others on the issue untouched.
func (c *Client) ChangeLabels(ctx context.Context, issueID string, added []string, removed []string) (Issue, error) {
	return c.updateIssue(ctx, "changeLabels", issueID, map[string]any{"addedLabelIds": added, "removedLabelIds": removed})
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
	if err := c.mutate(ctx, operation, query, map[string]any{"id": issueID, "input": input}, &data); err != nil {
		return Issue{}, err
	}
	if !data.IssueUpdate.Success {
		return Issue{}, fmt.Errorf("linear %s: issueUpdate returned success=false for issue %s, input=%v", operation, issueID, input)
	}
	return data.IssueUpdate.Issue.toIssue(), nil
}

// CreateComment posts a Markdown comment on an issue.
func (c *Client) CreateComment(ctx context.Context, issueID string, body string) error {
	const query = `mutation CreateComment($input: CommentCreateInput!) {
  commentCreate(input: $input) { success }
}`
	var data struct {
		CommentCreate struct {
			Success bool `json:"success"`
		} `json:"commentCreate"`
	}
	input := map[string]any{"issueId": issueID, "body": body}
	if err := c.mutate(ctx, "createComment", query, map[string]any{"input": input}, &data); err != nil {
		return err
	}
	if !data.CommentCreate.Success {
		return fmt.Errorf("linear createComment: success=false for issue %s", issueID)
	}
	return nil
}
