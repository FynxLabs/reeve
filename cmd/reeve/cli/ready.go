package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/thefynx/reeve/internal/blob/factory"
	"github.com/thefynx/reeve/internal/config"
	"github.com/thefynx/reeve/internal/run"
	gh "github.com/thefynx/reeve/internal/vcs/github"
)

func newReadyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ready",
		Short: "Mark a PR as ready for apply and notify Slack",
		RunE:  runReady,
	}
	cmd.Flags().Int("pr", 0, "PR number (required)")
	cmd.Flags().String("sha", "", "Commit SHA (default: $GITHUB_SHA)")
	cmd.Flags().String("run-url", "", "CI run URL")
	cmd.Flags().String("repo", "", "owner/repo (default: $GITHUB_REPOSITORY)")
	cmd.Flags().String("token", "", "GitHub token (default: $GITHUB_TOKEN)")
	cmd.Flags().String("root", "", "Repo root (default: cwd)")
	_ = cmd.MarkFlagRequired("pr")
	return cmd
}

func runReady(cmd *cobra.Command, _ []string) error {
	ctx := context.Background()

	pr := flagInt(cmd, "pr")
	sha := flagStringOrEnv(cmd, "sha", "GITHUB_SHA")
	runURL := flagStringOrEnv(cmd, "run-url", "")
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
		return fmt.Errorf("ready requires --repo (or $GITHUB_REPOSITORY) and --token (or $GITHUB_TOKEN)")
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

	prMeta, err := client.GetPR(ctx, pr)
	if err != nil {
		return fmt.Errorf("get pr: %w", err)
	}
	if sha == "" {
		sha = prMeta.HeadSHA
	}

	backend := run.BuildSlackBackend(cfg.Notifications, store)
	if err := run.NotifySlackReady(ctx, backend, cfg.Notifications,
		pr, sha, runURL, "", prMeta.Author, nil, nil); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "slack notify: %v\n", err)
	}

	comment := "<!-- reeve:ready -->\n:white_check_mark: **Ready for apply.** Comment `/reeve apply` to apply."
	if err := client.UpsertComment(ctx, pr, comment, "<!-- reeve:ready -->"); err != nil {
		return fmt.Errorf("post comment: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "PR #%d marked ready\n", pr)
	return nil
}
