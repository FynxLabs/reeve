package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/thefynx/reeve/internal/auth/factory"
	blobfactory "github.com/thefynx/reeve/internal/blob/factory"
	"github.com/thefynx/reeve/internal/config"
	"github.com/thefynx/reeve/internal/run"
	reeveotel "github.com/thefynx/reeve/internal/observability/otel"
	vgithub "github.com/thefynx/reeve/internal/vcs/github"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	var root string

	cmd := &cobra.Command{
		Use:   "reeve",
		Short: "PR-native GitOps orchestrator for Pulumi",
		SilenceUsage: true,
	}

	cmd.PersistentFlags().StringVar(&root, "root", "", "repo root (default: $GITHUB_WORKSPACE or .)")

	cmd.AddCommand(runCmd(&root))

	return cmd
}

func runCmd(root *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run a reeve operation",
	}
	cmd.AddCommand(previewCmd(root))
	cmd.AddCommand(applyCmd(root))
	cmd.AddCommand(readyCmd(root))
	return cmd
}

// resolveRoot returns the effective repo root.
func resolveRoot(flag string) string {
	if flag != "" {
		return flag
	}
	if ws := os.Getenv("GITHUB_WORKSPACE"); ws != "" {
		return ws
	}
	cwd, _ := os.Getwd()
	return cwd
}

// ghRepo splits "owner/repo" from GITHUB_REPOSITORY.
func ghRepo() (owner, repo string, err error) {
	r := os.Getenv("GITHUB_REPOSITORY")
	if r == "" {
		return "", "", fmt.Errorf("GITHUB_REPOSITORY not set")
	}
	parts := strings.SplitN(r, "/", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("GITHUB_REPOSITORY malformed: %q", r)
	}
	return parts[0], parts[1], nil
}

func ciRunURL() string {
	server := os.Getenv("GITHUB_SERVER_URL")
	repo := os.Getenv("GITHUB_REPOSITORY")
	runID := os.Getenv("GITHUB_RUN_ID")
	if server == "" || repo == "" || runID == "" {
		return ""
	}
	return fmt.Sprintf("%s/%s/actions/runs/%s", server, repo, runID)
}

func runNumber() int {
	n, _ := strconv.Atoi(os.Getenv("GITHUB_RUN_NUMBER"))
	return n
}

func commitSHA() string {
	if sha := os.Getenv("GITHUB_SHA"); sha != "" {
		return sha
	}
	return ""
}

func previewCmd(root *string) *cobra.Command {
	var pr int
	var local bool

	cmd := &cobra.Command{
		Use:   "preview",
		Short: "Run pulumi preview for stacks affected by a PR",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			repoRoot := resolveRoot(*root)

			cfg, err := config.Load(repoRoot)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			if err := cfg.Validate(); err != nil {
				return fmt.Errorf("config: %w", err)
			}

			store, err := blobfactory.Open(ctx, cfg.Shared.Bucket, repoRoot)
			if err != nil {
				return fmt.Errorf("blob store: %w", err)
			}

			authReg, err := factory.Build(ctx, cfg.Auth)
			if err != nil {
				return fmt.Errorf("auth: %w", err)
			}

			otel := (*reeveotel.Provider)(nil)

			var vcsClient *vgithub.Client
			var engine run.Engine

			owner, repo, err := ghRepo()
			if err == nil && !local {
				vcsClient, err = vgithub.New(ctx, os.Getenv("GITHUB_TOKEN"), owner, repo)
				if err != nil {
					return fmt.Errorf("vcs: %w", err)
				}
			}

			engine, err = buildEngine(ctx, cfg, repoRoot)
			if err != nil {
				return err
			}

			in := run.PreviewInput{
				PRNumber:      pr,
				CommitSHA:     commitSHA(),
				RunNumber:     runNumber(),
				CIRunURL:      ciRunURL(),
				RepoRoot:      repoRoot,
				Engine:        engine,
				Config:        cfg.Engines[0],
				Shared:        cfg.Shared,
				AuthConfig:    cfg.Auth,
				AuthRegistry:  authReg,
				Notifications: cfg.Notifications,
				Blob:          store,
				Local:         local,
				OTEL:          otel,
			}
			if vcsClient != nil {
				in.VCS = vcsClient
				in.Comments = vcsClient
			}

			out, err := run.Preview(ctx, in)
			if err != nil {
				return err
			}
			fmt.Printf("preview complete: %d stacks, run %s\n", len(out.Stacks), out.RunID)
			return nil
		},
	}

	cmd.Flags().IntVar(&pr, "pr", 0, "PR number")
	cmd.Flags().BoolVar(&local, "local", false, "run against all stacks locally (no PR)")
	return cmd
}

func applyCmd(root *string) *cobra.Command {
	var pr int

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply stacks affected by a PR",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			repoRoot := resolveRoot(*root)

			cfg, err := config.Load(repoRoot)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			if err := cfg.Validate(); err != nil {
				return fmt.Errorf("config: %w", err)
			}

			store, err := blobfactory.Open(ctx, cfg.Shared.Bucket, repoRoot)
			if err != nil {
				return fmt.Errorf("blob store: %w", err)
			}

			authReg, err := factory.Build(ctx, cfg.Auth)
			if err != nil {
				return fmt.Errorf("auth: %w", err)
			}

			otel := (*reeveotel.Provider)(nil)

			owner, repo, err := ghRepo()
			if err != nil {
				return fmt.Errorf("vcs: %w", err)
			}
			vcsClient, err := vgithub.New(ctx, os.Getenv("GITHUB_TOKEN"), owner, repo)
			if err != nil {
				return fmt.Errorf("vcs: %w", err)
			}

			engine, err := buildApplyEngine(ctx, cfg, repoRoot)
			if err != nil {
				return err
			}

			lockStore := buildLockStore(store)

			in := run.ApplyInput{
				PRNumber:      pr,
				CommitSHA:     commitSHA(),
				RunNumber:     runNumber(),
				CIRunURL:      ciRunURL(),
				RepoRoot:      repoRoot,
				RepoFull:      os.Getenv("GITHUB_REPOSITORY"),
				Actor:         os.Getenv("GITHUB_ACTOR"),
				Engine:        engine,
				Config:        cfg.Engines[0],
				Shared:        cfg.Shared,
				AuthConfig:    cfg.Auth,
				AuthRegistry:  authReg,
				Notifications: cfg.Notifications,
				Blob:          store,
				Locks:         lockStore,
				VCS:           vcsClient,
				OTEL:          otel,
			}

			out, err := run.Apply(ctx, in)
			if err != nil {
				return err
			}
			fmt.Printf("apply complete: %d stacks, blocked=%v, run %s\n", len(out.Stacks), out.Blocked, out.RunID)
			return nil
		},
	}

	cmd.Flags().IntVar(&pr, "pr", 0, "PR number")
	_ = cmd.MarkFlagRequired("pr")
	return cmd
}

func readyCmd(root *string) *cobra.Command {
	var pr int

	cmd := &cobra.Command{
		Use:   "ready",
		Short: "Mark a PR as ready for apply and notify Slack",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			repoRoot := resolveRoot(*root)

			cfg, err := config.Load(repoRoot)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			store, err := blobfactory.Open(ctx, cfg.Shared.Bucket, repoRoot)
			if err != nil {
				return fmt.Errorf("blob store: %w", err)
			}

			owner, repo, err := ghRepo()
			if err != nil {
				return fmt.Errorf("vcs: %w", err)
			}
			vcsClient, err := vgithub.New(ctx, os.Getenv("GITHUB_TOKEN"), owner, repo)
			if err != nil {
				return fmt.Errorf("vcs: %w", err)
			}

			prMeta, err := vcsClient.GetPR(ctx, pr)
			if err != nil {
				return fmt.Errorf("get pr: %w", err)
			}

			backend := run.BuildSlackBackend(cfg.Notifications, store)
			if err := run.NotifySlackReady(ctx, backend, cfg.Notifications,
				pr, prMeta.HeadSHA, ciRunURL(), "", prMeta.Author, nil, nil); err != nil {
				fmt.Fprintf(os.Stderr, "slack notify: %v\n", err)
			}

			// Post a PR comment confirming ready state.
			comment := "<!-- reeve:ready -->\n:white_check_mark: **Ready for apply.** Run `/reeve apply` to apply."
			if err := vcsClient.UpsertComment(ctx, pr, comment, "<!-- reeve:ready -->"); err != nil {
				return fmt.Errorf("post comment: %w", err)
			}

			fmt.Printf("PR #%d marked ready\n", pr)
			return nil
		},
	}

	cmd.Flags().IntVar(&pr, "pr", 0, "PR number")
	_ = cmd.MarkFlagRequired("pr")
	return cmd
}
