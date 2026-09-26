// Package linear is a minimal client for the Linear GraphQL API.
package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"
)

// Client talks to the Linear GraphQL API with a personal API key.
type Client struct {
	http     *http.Client
	endpoint string
	apiKey   string
	attempts int
}

// NewClient builds a client; attempts is how many times a failed request is tried before giving up.
func NewClient(apiKey string, endpoint string, timeout time.Duration, attempts int) *Client {
	return &Client{
		http:     &http.Client{Timeout: timeout},
		endpoint: endpoint,
		apiKey:   apiKey,
		attempts: attempts,
	}
}

// HTTPError is a non-2xx response from the API.
type HTTPError struct {
	Operation string
	Status    int
	Body      string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("linear %s: HTTP %d: %s", e.Operation, e.Status, e.Body)
}

// GraphQLError is a 200 response that carries GraphQL errors (bad filter, missing permission, ...).
type GraphQLError struct {
	Operation string
	Messages  []string
	Codes     []string
}

func (e *GraphQLError) Error() string {
	return fmt.Sprintf("linear %s: %s", e.Operation, strings.Join(e.Messages, "; "))
}

type request struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

type response struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message    string `json:"message"`
		Extensions struct {
			Code string `json:"code"`
		} `json:"extensions"`
	} `json:"errors"`
}

// query runs a read, retrying rate limits, server errors and network failures.
func (c *Client) query(ctx context.Context, operation string, query string, variables map[string]any, out any) error {
	return c.do(ctx, operation, query, variables, out, retryable)
}

// mutate runs a write, retrying only rate limits: after a timeout or 5xx the write may already have landed,
// and retrying would post a comment twice.
func (c *Client) mutate(ctx context.Context, operation string, query string, variables map[string]any, out any) error {
	return c.do(ctx, operation, query, variables, out, rateLimited)
}

// do runs one GraphQL operation and decodes its data into out, retrying failures shouldRetry accepts.
func (c *Client) do(ctx context.Context, operation string, query string, variables map[string]any, out any, shouldRetry func(error) bool) error {
	body, err := json.Marshal(request{Query: query, Variables: variables})
	if err != nil {
		return fmt.Errorf("linear %s: encode request: %w", operation, err)
	}

	var lastErr error
	for attempt := 1; attempt <= c.attempts; attempt++ {
		data, err := c.post(ctx, operation, body)
		if err == nil {
			if err := json.Unmarshal(data, out); err != nil {
				return fmt.Errorf("linear %s: decode data: %w, body=%s", operation, err, data)
			}
			return nil
		}
		if !shouldRetry(err) {
			return err
		}
		lastErr = err
		if attempt == c.attempts {
			break
		}
		slog.Warn("linear request failed, retrying", "operation", operation, "attempt", attempt, "error", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt) * 500 * time.Millisecond):
		}
	}
	return lastErr
}

func (c *Client) post(ctx context.Context, operation string, body []byte) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("linear %s: build request: %w", operation, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", c.apiKey)

	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("linear %s: %w", operation, err)
	}
	defer res.Body.Close()

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("linear %s: read body (HTTP %d): %w", operation, res.StatusCode, err)
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return nil, &HTTPError{Operation: operation, Status: res.StatusCode, Body: string(raw)}
	}

	var parsed response
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("linear %s: decode response: %w, body=%s", operation, err, raw)
	}
	if len(parsed.Errors) > 0 {
		messages := make([]string, 0, len(parsed.Errors))
		codes := make([]string, 0, len(parsed.Errors))
		for _, e := range parsed.Errors {
			messages = append(messages, e.Message)
			codes = append(codes, e.Extensions.Code)
		}
		return nil, &GraphQLError{Operation: operation, Messages: messages, Codes: codes}
	}
	return parsed.Data, nil
}

// rateLimited reports whether Linear refused the request for rate limiting, so it wasn't processed.
func rateLimited(err error) bool {
	var gqlErr *GraphQLError
	if errors.As(err, &gqlErr) {
		return slices.Contains(gqlErr.Codes, "RATELIMITED")
	}
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.Status == http.StatusTooManyRequests || strings.Contains(httpErr.Body, "RATELIMITED")
	}
	return false
}

// retryable reports whether a failure is worth another attempt: rate limits, server errors and network errors.
func retryable(err error) bool {
	var gqlErr *GraphQLError
	if errors.As(err, &gqlErr) {
		return slices.Contains(gqlErr.Codes, "RATELIMITED")
	}
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.Status == http.StatusTooManyRequests || httpErr.Status >= 500 || strings.Contains(httpErr.Body, "RATELIMITED")
	}
	return true
}
