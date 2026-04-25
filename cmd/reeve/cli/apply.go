package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/thefynx/reeve/internal/audit"
	authfac "github.com/thefynx/reeve/internal/auth/factory"
	"github.com/thefynx/reeve/internal/blob/factory"
	blocks "github.com/thefynx/reeve/internal/blob/locks"
	"github.com/thefynx/reeve/internal/config"
	"github.com/thefynx/reeve/internal/iac/pulumi"
	"github.com/thefynx/reeve/internal/run"
	gh "github.com/thefynx/reeve/internal/vcs/github"
)

func newApplyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Run apply for approved stacks",
		RunE:  runApply,
	}
	addPreviewFlags(cmd)
	cmd.Flags().String("actor", "", "User triggering the apply (default: $GITHUB_ACTOR)")
	return cmd
}

func runApply(cmd *cobra.Command, _ []string) error {
	ctx := context.Background()

	pr := flagInt(cmd, "pr")
	sha := flagStringOrEnv(cmd, "sha", "GITHUB_SHA")
	runNum := flagIntOrEnv(cmd, "run-number", "GITHUB_RUN_NUMBER")
	runURL := flagStringOrEnv(cmd, "run-url", "")
	repoFull := flagStringOrEnv(cmd, "repo", "GITHUB_REPOSITORY")
	token := flagStringOrEnv(cmd, "token", "GITHUB_TOKEN")
	if token == "" {
		token = os.Getenv("REEVE_GITHUB_TOKEN")
	}
	actor := flagStringOrEnv(cmd, "actor", "GITHUB_ACTOR")
	root := flagStringOrDefault(cmd, "root", "")
	if root == "" {
		root, _ = os.Getwd()
	}
	abs, _ := filepath.Abs(root)
	root = abs

	if pr == 0 || repoFull == "" || token == "" {
		return fmt.Errorf("apply requires --pr, --repo (or $GITHUB_REPOSITORY), and --token (or $GITHUB_TOKEN)")
	}

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

	// Opportunistic reaper before acquiring any locks.
	lockStore := blocks.New(store)
	if n, _ := lockStore.ReapAll(ctx); n > 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "reaped %d expired lock(s)\n", n)
	}

	parts := strings.SplitN(repoFull, "/", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid --repo %q (want owner/name)", repoFull)
	}
	client, err := gh.New(ctx, token, parts[0], parts[1])
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

	out, err := run.Apply(ctx, run.ApplyInput{
		PRNumber:      pr,
		CommitSHA:     sha,
		RunNumber:     runNum,
		CIRunURL:      runURL,
		RepoRoot:      root,
		RepoFull:      repoFull,
		Actor:         actor,
		Engine:        engine,
		Config:        engineCfg,
		Shared:        cfg.Shared,
		AuthConfig:    cfg.Auth,
		AuthRegistry:  authReg,
		Notifications: cfg.Notifications,
		OTEL:          otelProvider,
		Annotations:   annotationEmitters,
		Blob:          store,
		Locks:         lockStore,
		VCS:           client,
		AuditWriter:   audit.NewWriter(store),
	})
	if err != nil {
		return err
	}

	if out.Blocked {
		fmt.Fprintf(cmd.OutOrStdout(), "apply blocked by preconditions for one or more stacks (PR #%d, run_id=%s)\n", pr, out.RunID)
		os.Exit(2) // non-zero but distinct from crashes
	}
	fmt.Fprintf(cmd.OutOrStdout(), "apply complete (PR #%d, run_id=%s, %d stacks, %ds)\n",
		pr, out.RunID, len(out.Stacks), out.DurationSec)
	return nil
}
