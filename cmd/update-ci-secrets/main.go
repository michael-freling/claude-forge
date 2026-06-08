// Command update-ci-secrets sets the CLAUDE_CODE_OAUTH_TOKEN GitHub Actions
// secret so the Claude Code PR review workflow can authenticate.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/michael-freling/claude-code-tools/internal/cisecrets"
	"github.com/spf13/cobra"
)

// updater is the subset of *cisecrets.Updater used by the command. Tests
// override newUpdater to inject a fake.
type updater interface {
	Update(ctx context.Context, token string) (string, error)
	UpdateFromCredentials(ctx context.Context, claudeDir string) (string, error)
}

// newUpdater builds the updater for the given repo. Overridable in tests.
var newUpdater = func(repo string) updater { return cisecrets.NewUpdater(repo) }

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var (
		repo       string
		oauthToken string
		fromCreds  bool
	)

	cmd := &cobra.Command{
		Use:   "update-ci-secrets",
		Short: "Update the CLAUDE_CODE_OAUTH_TOKEN GitHub Actions secret",
		Long: `update-ci-secrets sets the CLAUDE_CODE_OAUTH_TOKEN secret on the GitHub
repository so the Claude Code PR review workflow (.github/workflows/claude-review.yml)
can authenticate.

The token comes from --oauth-token, or is resolved from the local Claude Code
credentials (~/.claude/.credentials.json) when --from-credentials is set.

Requires the gh CLI to be installed and authenticated with repo admin access.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if oauthToken == "" && !fromCreds {
				return fmt.Errorf("provide --oauth-token or --from-credentials")
			}
			if oauthToken != "" && fromCreds {
				return fmt.Errorf("--oauth-token and --from-credentials are mutually exclusive")
			}

			if repo == "" {
				repo = cisecrets.DefaultRepo
			}
			u := newUpdater(repo)
			ctx := cmd.Context()

			var (
				masked string
				err    error
			)
			if fromCreds {
				home, herr := os.UserHomeDir()
				if herr != nil {
					return fmt.Errorf("failed to locate home directory: %w", herr)
				}
				masked, err = u.UpdateFromCredentials(ctx, filepath.Join(home, ".claude"))
			} else {
				masked, err = u.Update(ctx, oauthToken)
			}
			if err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Set %s (%s) on %s\n", cisecrets.SecretName, masked, repo)
			return nil
		},
	}

	cmd.Flags().StringVar(&repo, "repo", cisecrets.DefaultRepo, "GitHub repository (owner/name)")
	cmd.Flags().StringVar(&oauthToken, "oauth-token", "", "OAuth token to set directly")
	cmd.Flags().BoolVar(&fromCreds, "from-credentials", false, "Resolve token from local Claude Code credentials")

	return cmd
}
