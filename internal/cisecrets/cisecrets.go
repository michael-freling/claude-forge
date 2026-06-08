// Package cisecrets updates the GitHub Actions secrets used by CI, in
// particular the Claude Code OAuth token consumed by the PR review workflow.
package cisecrets

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/michael-freling/claude-code-tools/internal/forge/auth"
)

// SecretName is the GitHub Actions secret holding the Claude Code OAuth token.
const SecretName = "CLAUDE_CODE_OAUTH_TOKEN"

// secretSetter sets a GitHub Actions secret. It is overridable in tests.
type secretSetter func(ctx context.Context, repo, name, value string) error

// Updater sets the Claude Code OAuth token secret on a GitHub repository.
type Updater struct {
	// Repo is the target repository (owner/name). When empty, gh resolves the
	// repository from the current working directory's git remote.
	Repo   string
	setter secretSetter
}

// NewUpdater returns an Updater that uses the gh CLI to set secrets. An empty
// repo lets gh resolve the repository from the current directory.
func NewUpdater(repo string) *Updater {
	return &Updater{Repo: repo, setter: ghSecretSet}
}

// repoLabel describes the target repository for log/error messages.
func (u *Updater) repoLabel() string {
	if u.Repo == "" {
		return "the current repository"
	}
	return u.Repo
}

// Update sets SecretName to token and returns the masked value that was set.
func (u *Updater) Update(ctx context.Context, token string) (string, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", fmt.Errorf("empty token")
	}
	if err := u.setter(ctx, u.Repo, SecretName, token); err != nil {
		return "", fmt.Errorf("failed to set %s on %s: %w", SecretName, u.repoLabel(), err)
	}
	return mask(token), nil
}

// UpdateFromCredentials resolves the OAuth token from the Claude Code
// credentials under claudeDir (e.g. ~/.claude) and sets it as the secret. It
// reuses auth.Resolve, so it also honours the ANTHROPIC_API_KEY and
// CLAUDE_CODE_OAUTH_TOKEN environment variables and rejects expired tokens.
func (u *Updater) UpdateFromCredentials(ctx context.Context, claudeDir string) (string, error) {
	creds, err := auth.Resolve(claudeDir)
	if err != nil {
		return "", err
	}
	if creds.AuthType != "oauth" {
		return "", fmt.Errorf("resolved credential is %q, not an OAuth token; %s requires an OAuth login (run 'claude' to authenticate)", creds.AuthType, SecretName)
	}
	return u.Update(ctx, creds.Token)
}

// mask hides all but the first 8 and last 4 characters of a token.
func mask(token string) string {
	if len(token) <= 12 {
		return "***"
	}
	return token[:8] + "..." + token[len(token)-4:]
}

// secretSetArgs builds the gh argument list. An empty repo omits --repo so gh
// resolves the repository from the current directory.
func secretSetArgs(repo, name string) []string {
	args := []string{"secret", "set", name}
	if repo != "" {
		args = append(args, "--repo", repo)
	}
	return args
}

// ghSecretSet sets a repository secret via the gh CLI, passing the value on
// stdin so it never appears in the process argument list.
func ghSecretSet(ctx context.Context, repo, name, value string) error {
	cmd := exec.CommandContext(ctx, "gh", secretSetArgs(repo, name)...)
	cmd.Stdin = strings.NewReader(value)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if trimmed := strings.TrimSpace(string(out)); trimmed != "" {
			return fmt.Errorf("%w: %s", err, trimmed)
		}
		return err
	}
	return nil
}
