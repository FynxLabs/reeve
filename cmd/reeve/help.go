package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/thefynx/reeve/internal/blob/factory"
	"github.com/thefynx/reeve/internal/config"
	gh "github.com/thefynx/reeve/internal/vcs/github"
)

func newHelpCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pr-help",
		Short: "Post a help comment to the PR listing available reeve commands",
		RunE:  runHelp,
	}
	cmd.Flags().Int("pr", 0, "PR number")
	cmd.Flags().String("repo", "", "owner/repo (default: $GITHUB_REPOSITORY)")
	cmd.Flags().String("token", "", "GitHub token (default: $GITHUB_TOKEN)")
	return cmd
}

func runHelp(cmd *cobra.Command, _ []string) error {
	ctx := context.Background()

	pr := flagInt(cmd, "pr")
	repoFull := flagStringOrEnv(cmd, "repo", "GITHUB_REPOSITORY")
	token := flagStringOrEnv(cmd, "token", "GITHUB_TOKEN")
	if token == "" {
		token = os.Getenv("REEVE_GITHUB_TOKEN")
	}
	root := flagStringOrDefault(cmd, "root", "")
	if root == "" {
		root, _ = os.Getwd()
	}
	abs, _ := filepath.Abs(root)
	root = abs

	if repoFull == "" || token == "" {
		return fmt.Errorf("help requires --repo (or $GITHUB_REPOSITORY) and --token (or $GITHUB_TOKEN)")
	}

	parts := strings.SplitN(repoFull, "/", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid --repo %q (want owner/name)", repoFull)
	}
	client, err := gh.New(ctx, token, parts[0], parts[1])
	if err != nil {
		return err
	}

	cfg, err := config.Load(root)
	if err != nil {
		return err
	}

	store, err := factory.Open(ctx, cfg.Shared.Bucket, root)
	if err != nil {
		return err
	}
	_ = store

	body := buildHelpComment(cfg.Shared.Apply.AutoReady)

	if pr > 0 {
		if err := client.UpsertComment(ctx, pr, body, "<!-- reeve:help -->"); err != nil {
			return fmt.Errorf("post help comment: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "posted help comment on PR #%d\n", pr)
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), body)
	}
	return nil
}

func buildHelpComment(autoReady bool) string {
	var b strings.Builder
	b.WriteString("<!-- reeve:help -->\n")
	b.WriteString("## reeve commands\n\n")
	b.WriteString("| Command | Description |\n")
	b.WriteString("|---|---|\n")
	b.WriteString("| `/reeve apply` | Apply all planned stacks for this PR |\n")
	b.WriteString("| `/reeve ready` | Mark PR as ready for apply, notify Slack |\n")
	b.WriteString("| `/reeve help` | Show this help message |\n")
	b.WriteString("\n")
	if autoReady {
		b.WriteString("> `auto_ready` is enabled - reeve will automatically mark this PR ready after a successful plan.\n\n")
	}
	b.WriteString("_reeve runs automatically on PR open/push (preview) and on comment commands above._\n")
	return b.String()
}
