package command

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// PRInfo contains information about a pull request
type PRInfo struct {
	Number      int    `json:"number"`
	URL         string `json:"url"`
	Title       string `json:"title"`
	HeadRefName string `json:"headRefName"`
}

// GhRunner abstracts gh CLI command execution for testing
type GhRunner interface {
	// PRCreate creates a new PR and returns the PR URL
	PRCreate(ctx context.Context, dir string, title, body, head, base string) (prURL string, err error)
	// PREdit updates the body of an existing PR
	PREdit(ctx context.Context, dir string, prNumber int, body string) error
	// PRClose closes a PR
	PRClose(ctx context.Context, dir string, prNumber int) error
	// PRView returns PR info as JSON. If prNumber is 0, it views the current branch's PR.
	PRView(ctx context.Context, dir string, prNumber int, jsonFields string, jqQuery string) (output string, err error)
	// PRChecks returns CI check status as JSON
	PRChecks(ctx context.Context, dir string, prNumber int, jsonFields string) (output string, err error)
	// GetPRBaseBranch returns the base branch name for a pull request
	GetPRBaseBranch(ctx context.Context, dir string, prNumber string) (string, error)
	// RunRerun reruns failed/cancelled jobs for a workflow run
	RunRerun(ctx context.Context, dir string, runID int64) error
	// GetLatestRunID gets the latest workflow run ID for a PR
	GetLatestRunID(ctx context.Context, dir string, prNumber int) (int64, error)
	// ListPRs returns all PRs for a specific branch
	ListPRs(ctx context.Context, dir string, branch string) ([]PRInfo, error)
}

// ghRunner implements GhRunner interface
type ghRunner struct {
	runner Runner
}

// NewGhRunner creates a new gh runner
func NewGhRunner(runner Runner) GhRunner {
	return &ghRunner{
		runner: runner,
	}
}

// PRCreate creates a new PR and returns the PR URL
func (g *ghRunner) PRCreate(ctx context.Context, dir string, title, body, head, base string) (string, error) {
	if title == "" {
		return "", fmt.Errorf("title cannot be empty")
	}
	if head == "" {
		return "", fmt.Errorf("head branch cannot be empty")
	}

	args := []string{"pr", "create", "--title", title, "--body", body, "--head", head}
	if base != "" {
		args = append(args, "--base", base)
	}

	stdout, stderr, err := g.runner.RunInDir(ctx, dir, "gh", args...)
	if err != nil {
		return "", fmt.Errorf("failed to create PR: %w (stderr: %s)", err, stderr)
	}

	return strings.TrimSpace(stdout), nil
}

// PREdit updates the body of an existing PR
func (g *ghRunner) PREdit(ctx context.Context, dir string, prNumber int, body string) error {
	if prNumber <= 0 {
		return fmt.Errorf("PR number must be positive, got %d", prNumber)
	}

	args := []string{"pr", "edit", fmt.Sprintf("%d", prNumber), "--body", body}

	_, stderr, err := g.runner.RunInDir(ctx, dir, "gh", args...)
	if err != nil {
		return fmt.Errorf("failed to edit PR %d: %w (stderr: %s)", prNumber, err, stderr)
	}

	return nil
}

// PRClose closes a PR
func (g *ghRunner) PRClose(ctx context.Context, dir string, prNumber int) error {
	if prNumber <= 0 {
		return fmt.Errorf("PR number must be positive, got %d", prNumber)
	}

	args := []string{"pr", "close", fmt.Sprintf("%d", prNumber)}

	_, stderr, err := g.runner.RunInDir(ctx, dir, "gh", args...)
	if err != nil {
		return fmt.Errorf("failed to close PR %d: %w (stderr: %s)", prNumber, err, stderr)
	}

	return nil
}

// PRView returns PR info as JSON. If prNumber is 0, it views the current branch's PR.
func (g *ghRunner) PRView(ctx context.Context, dir string, prNumber int, jsonFields string, jqQuery string) (string, error) {
	args := []string{"pr", "view"}
	if prNumber > 0 {
		args = append(args, fmt.Sprintf("%d", prNumber))
	}
	args = append(args, "--json", jsonFields, "-q", jqQuery)

	stdout, stderr, err := g.runner.RunInDir(ctx, dir, "gh", args...)
	if err != nil {
		return "", fmt.Errorf("failed to view PR: %w (stderr: %s)", err, stderr)
	}

	return stdout, nil
}

// PRChecks returns CI check status as JSON
func (g *ghRunner) PRChecks(ctx context.Context, dir string, prNumber int, jsonFields string) (string, error) {
	var args []string
	if prNumber > 0 {
		args = []string{"pr", "checks", fmt.Sprintf("%d", prNumber), "--json", jsonFields}
	} else {
		args = []string{"pr", "checks", "--json", jsonFields}
	}

	stdout, stderr, err := g.runner.RunInDir(ctx, dir, "gh", args...)
	if err != nil {
		return "", fmt.Errorf("failed to check PR status: %w (stderr: %s)", err, stderr)
	}

	return stdout, nil
}

// GetPRBaseBranch returns the base branch name for the specified PR number
func (g *ghRunner) GetPRBaseBranch(ctx context.Context, dir string, prNumber string) (string, error) {
	args := []string{"pr", "view", prNumber, "--json", "baseRefName", "--jq", ".baseRefName"}

	stdout, stderr, err := g.runner.RunInDir(ctx, dir, "gh", args...)
	if err != nil {
		return "", fmt.Errorf("failed to get PR base branch: %w (stderr: %s)", err, stderr)
	}

	return strings.TrimSpace(stdout), nil
}

// RunRerun reruns failed/cancelled jobs for a workflow run
func (g *ghRunner) RunRerun(ctx context.Context, dir string, runID int64) error {
	args := []string{"run", "rerun", fmt.Sprintf("%d", runID), "--failed"}

	_, stderr, err := g.runner.RunInDir(ctx, dir, "gh", args...)
	if err != nil {
		return fmt.Errorf("failed to rerun workflow: %w (stderr: %s)", err, stderr)
	}

	return nil
}

// GetLatestRunID gets the latest workflow run ID for a PR
func (g *ghRunner) GetLatestRunID(ctx context.Context, dir string, prNumber int) (int64, error) {
	args := []string{"pr", "checks", fmt.Sprintf("%d", prNumber), "--json", "databaseId"}

	stdout, stderr, err := g.runner.RunInDir(ctx, dir, "gh", args...)
	if err != nil {
		return 0, fmt.Errorf("failed to get workflow run ID: %w (stderr: %s)", err, stderr)
	}

	// Parse JSON array output: [{"databaseId": 123}, ...]
	var checks []struct {
		DatabaseID int64 `json:"databaseId"`
	}
	if err := json.Unmarshal([]byte(stdout), &checks); err != nil {
		return 0, fmt.Errorf("failed to parse workflow run ID from output: %w", err)
	}

	if len(checks) == 0 {
		return 0, fmt.Errorf("no workflow runs found for PR %d", prNumber)
	}

	// Return the first (latest) run ID
	return checks[0].DatabaseID, nil
}

// ListPRs returns all PRs for a specific branch
func (g *ghRunner) ListPRs(ctx context.Context, dir string, branch string) ([]PRInfo, error) {
	if branch == "" {
		return nil, fmt.Errorf("branch cannot be empty")
	}

	args := []string{"pr", "list", "--head", branch, "--json", "number,url,title,headRefName", "--limit", "1"}

	stdout, stderr, err := g.runner.RunInDir(ctx, dir, "gh", args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list PRs for branch %s: %w (stderr: %s)", branch, err, stderr)
	}

	var prs []PRInfo
	if err := json.Unmarshal([]byte(stdout), &prs); err != nil {
		return nil, fmt.Errorf("failed to parse PR list from output: %w", err)
	}

	return prs, nil
}
