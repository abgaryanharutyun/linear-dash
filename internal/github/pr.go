// Package github reads pull request status through the gh CLI, reusing the user's gh auth.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

var prURL = regexp.MustCompile(`^https://github\.com/[^/]+/[^/]+/pull/\d+`)

// IsPullRequestURL reports whether url points at a GitHub pull request.
func IsPullRequestURL(url string) bool {
	return prURL.MatchString(url)
}

type Checks struct {
	Passed  int
	Failed  int
	Pending int
}

type PullRequest struct {
	URL            string
	Number         int
	Title          string
	State          string
	IsDraft        bool
	ReviewDecision string
	Checks         Checks
}

type rollupItem struct {
	Typename   string `json:"__typename"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	State      string `json:"state"`
}

// checkOutcomes maps a CheckRun conclusion or StatusContext state to a bucket; anything unlisted is pending.
var checkOutcomes = map[string]string{
	"SUCCESS":         "passed",
	"NEUTRAL":         "passed",
	"SKIPPED":         "passed",
	"FAILURE":         "failed",
	"ERROR":           "failed",
	"TIMED_OUT":       "failed",
	"CANCELLED":       "failed",
	"ACTION_REQUIRED": "failed",
	"STARTUP_FAILURE": "failed",
}

// PullRequestStatus runs `gh pr view` for one PR URL.
func PullRequestStatus(ctx context.Context, url string) (PullRequest, error) {
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", url, "--json", "number,title,state,isDraft,reviewDecision,statusCheckRollup")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return PullRequest{}, fmt.Errorf("gh pr view %s: %w: %s", url, err, strings.TrimSpace(stderr.String()))
	}

	var raw struct {
		Number            int          `json:"number"`
		Title             string       `json:"title"`
		State             string       `json:"state"`
		IsDraft           bool         `json:"isDraft"`
		ReviewDecision    string       `json:"reviewDecision"`
		StatusCheckRollup []rollupItem `json:"statusCheckRollup"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		return PullRequest{}, fmt.Errorf("decode gh pr view %s: %w, output=%s", url, err, stdout.String())
	}
	return PullRequest{
		URL:            url,
		Number:         raw.Number,
		Title:          raw.Title,
		State:          raw.State,
		IsDraft:        raw.IsDraft,
		ReviewDecision: raw.ReviewDecision,
		Checks:         summarize(raw.StatusCheckRollup),
	}, nil
}

func summarize(items []rollupItem) Checks {
	counts := map[string]int{}
	for _, item := range items {
		// CheckRuns report a conclusion once completed; StatusContexts only have a state
		outcome := item.Conclusion
		if item.Typename == "StatusContext" {
			outcome = item.State
		}
		bucket, ok := checkOutcomes[outcome]
		if !ok {
			bucket = "pending"
		}
		counts[bucket]++
	}
	return Checks{Passed: counts["passed"], Failed: counts["failed"], Pending: counts["pending"]}
}
