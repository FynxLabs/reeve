package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	authfac "github.com/thefynx/reeve/internal/auth/factory"
	"github.com/thefynx/reeve/internal/blob/factory"
	"github.com/thefynx/reeve/internal/config"
	"github.com/thefynx/reeve/internal/iac/pulumi"
	"github.com/thefynx/reeve/internal/run"
	gh "github.com/thefynx/reeve/internal/vcs/github"
)

func newRunCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "run", Short: "Execute preview or apply for the current PR context"}

	preview := &cobra.Command{
		Use:   "preview",
		Short: "Run preview for stacks touched by this PR",
		RunE:  runPreview,
	}
	addPreviewFlags(preview)

	cmd.AddCommand(preview, newApplyCmd(), newReadyCmd(), newHelpCmd())
	return cmd
}

func newPlanRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan-run",
		Short: "Simulate a PR run with no side effects (alias for `run preview --local`)",
		RunE: func(c *cobra.Command, args []string) error {
			c.Flag("local").Value.Set("true")
			return runPreview(c, args)
		},
	}
	addPreviewFlags(cmd)
	return cmd
}

func newRenderCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "render",
		Short: "Render PR comment markdown to stdout (alias for `run preview --local`)",
		RunE: func(c *cobra.Command, args []string) error {
			c.Flag("local").Value.Set("true")
			return runPreview(c, args)
		},
	}
	addPreviewFlags(cmd)
	return cmd
}

func addPreviewFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("local", false, "Run against real cloud with local artifacts; skip VCS interactions")
	cmd.Flags().Int("pr", 0, "PR number")
	cmd.Flags().String("sha", "", "Commit SHA (default: $GITHUB_SHA)")
	cmd.Flags().String("run-number", "", "CI run number (default: $GITHUB_RUN_NUMBER)")
	cmd.Flags().String("run-url", "", "CI run URL")
	cmd.Flags().String("repo", "", "owner/repo (default: $GITHUB_REPOSITORY)")
	cmd.Flags().String("token", "", "GitHub token (default: $GITHUB_TOKEN)")
	cmd.Flags().String("root", "", "Repo root (default: cwd)")
}

func runPreview(cmd *cobra.Command, _ []string) error {
	ctx := context.Background()

	local := flagBool(cmd, "local")
	pr := flagInt(cmd, "pr")
	sha := flagStringOrEnv(cmd, "sha", "GITHUB_SHA")
	runNum := flagIntOrEnv(cmd, "run-number", "GITHUB_RUN_NUMBER")
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
	abs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	root = abs

	cfg, err := config.Load(root)
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	store, err := factory.Open(ctx, cfg.Shared.Bucket, root)
	if err != nil {
		return err
	}

	engineCfg := cfg.Engines[0]
	engine := pulumi.New(engineCfg.Engine.Binary.Path)

	authReg, err := authfac.Build(ctx, cfg.Auth)
	if err != nil {
		return fmt.Errorf("build auth registry: %w", err)
	}

	otelProvider, _ := run.BuildOTEL(ctx, cfg.Observability)
	defer func() {
		if otelProvider != nil {
			_ = otelProvider.Shutdown(ctx)
		}
	}()
	annotationEmitters := run.BuildAnnotationEmitters(cfg.Observability)

	in := run.PreviewInput{
		PRNumber:      pr,
		CommitSHA:     sha,
		RunNumber:     runNum,
		CIRunURL:      runURL,
		RepoRoot:      root,
		Engine:        engine,
		Config:        engineCfg,
		Shared:        cfg.Shared,
		AuthConfig:    cfg.Auth,
		AuthRegistry:  authReg,
		Notifications: cfg.Notifications,
		OTEL:          otelProvider,
		Annotations:   annotationEmitters,
		Blob:          store,
		Local:         local,
	}

	if !local && repoFull != "" && token != "" && pr > 0 {
		parts := strings.SplitN(repoFull, "/", 2)
		if len(parts) == 2 {
			client, err := gh.New(ctx, token, parts[0], parts[1])
			if err != nil {
				return err
			}
			in.VCS = client
			in.Comments = client
		}
	}

	out, err := run.Preview(ctx, in)
	if err != nil {
		return err
	}

	if local || in.Comments == nil {
		// Print the rendered comment to stdout so operators can review.
		fmt.Fprintln(cmd.OutOrStdout(), out.CommentBody)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "posted preview comment for PR #%d (run_id=%s, %d stacks)\n",
			pr, out.RunID, len(out.Stacks))
	}
	return nil
}

func flagBool(cmd *cobra.Command, name string) bool {
	f := cmd.Flag(name)
	if f == nil {
		return false
	}
	v, _ := strconv.ParseBool(f.Value.String())
	return v
}

func flagInt(cmd *cobra.Command, name string) int {
	f := cmd.Flag(name)
	if f == nil {
		return 0
	}
	n, _ := strconv.Atoi(f.Value.String())
	return n
}

func flagStringOrDefault(cmd *cobra.Command, name, def string) string {
	f := cmd.Flag(name)
	if f == nil {
		return def
	}
	if v := f.Value.String(); v != "" {
		return v
	}
	return def
}

func flagStringOrEnv(cmd *cobra.Command, name, envKey string) string {
	if v := flagStringOrDefault(cmd, name, ""); v != "" {
		return v
	}
	if envKey != "" {
		return os.Getenv(envKey)
	}
	return ""
}

func flagIntOrEnv(cmd *cobra.Command, name, envKey string) int {
	if n := flagInt(cmd, name); n != 0 {
		return n
	}
	if envKey != "" {
		n, _ := strconv.Atoi(os.Getenv(envKey))
		return n
	}
	return 0
}
