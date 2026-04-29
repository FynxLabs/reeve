package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/thefynx/reeve/internal/auth"
	authfac "github.com/thefynx/reeve/internal/auth/factory"
	"github.com/thefynx/reeve/internal/blob/factory"
	"github.com/thefynx/reeve/internal/config"
	"github.com/thefynx/reeve/internal/core/discovery"
	"github.com/thefynx/reeve/internal/drift"
	sinkfac "github.com/thefynx/reeve/internal/drift/sinks/factory"
	"github.com/thefynx/reeve/internal/iac/pulumi"
	"github.com/thefynx/reeve/internal/run"
	gh "github.com/thefynx/reeve/internal/vcs/github"
)

func newDriftCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "drift", Short: "Drift detection commands"}

	runSub := &cobra.Command{Use: "run", Short: "Execute a drift check", RunE: driftRun}
	runSub.Flags().String("pattern", "", "Restrict to stacks matching this pattern")
	runSub.Flags().String("schedule", "", "Run a named schedule from drift.yaml")
	runSub.Flags().Bool("if-stale", false, "Skip stacks checked within freshness window")
	runSub.Flags().String("root", "", "Repo root (default: cwd)")

	statusSub := &cobra.Command{Use: "status", Short: "Read last drift run results", RunE: driftStatus}
	statusSub.Flags().String("stack", "", "Limit to a single project/stack")

	reportSub := &cobra.Command{Use: "report", Short: "Render drift report to stdout", RunE: driftReport}
	reportSub.Flags().String("format", "markdown", "Output format (markdown|json)")

	suppress := &cobra.Command{Use: "suppress", Short: "Manage time-bounded suppressions"}
	addSub := &cobra.Command{Use: "add <project/stack>", Args: cobra.ExactArgs(1), Short: "Create a suppression", RunE: driftSuppressAdd}
	addSub.Flags().String("until", "24h", "Suppression duration (e.g. 24h, 7d)")
	addSub.Flags().String("reason", "", "Why suppressed (for audit)")
	suppress.AddCommand(
		addSub,
		&cobra.Command{Use: "list", Short: "List active suppressions", RunE: driftSuppressList},
		&cobra.Command{Use: "clear <project/stack>", Args: cobra.ExactArgs(1), Short: "Clear a suppression", RunE: driftSuppressClear},
	)

	cmd.AddCommand(runSub, statusSub, reportSub, suppress)
	return cmd
}

func loadDriftCtx(cmd *cobra.Command) (context.Context, *config.Config, string, error) {
	root := flagStringOrDefault(cmd, "root", "")
	if root == "" {
		root, _ = os.Getwd()
	}
	abs, _ := filepath.Abs(root)
	root = abs
	cfg, err := config.Load(root)
	if err != nil {
		return nil, nil, "", err
	}
	if err := cfg.Validate(); err != nil {
		return nil, nil, "", err
	}
	return context.Background(), cfg, root, nil
}

func driftRun(cmd *cobra.Command, _ []string) error {
	ctx, cfg, root, err := loadDriftCtx(cmd)
	if err != nil {
		return err
	}
	store, err := factory.Open(ctx, cfg.Shared.Bucket, root)
	if err != nil {
		return err
	}
	engineCfg := cfg.Engines[0]
	engine := pulumi.New(engineCfg.Engine.Binary.Path)

	// Build the auth resolver on top of the auth registry.
	authReg, err := authfac.Build(ctx, cfg.Auth)
	if err != nil {
		return err
	}
	resolver := func(ctx context.Context, ref string) (map[string]string, error) {
		return run.ResolveAuthEnv(ctx, cfg.Auth, authReg, ref, auth.ModeDrift)
	}

	decls := make([]discovery.Declaration, 0, len(engineCfg.Engine.Stacks))
	for _, s := range engineCfg.Engine.Stacks {
		decls = append(decls, discovery.Declaration{
			Project: s.Project, Path: s.Path, Pattern: s.Pattern, Stacks: s.Stacks,
		})
	}
	var filter discovery.Filter
	for _, ex := range engineCfg.Engine.Filters.Exclude {
		if ex.Stack != "" {
			filter.StackPatterns = append(filter.StackPatterns, ex.Stack)
		}
		if ex.Pattern != "" {
			filter.PathPatterns = append(filter.PathPatterns, ex.Pattern)
		}
	}

	include, exclude := buildScope(cfg, cmd)

	otelProvider, _ := run.BuildOTEL(ctx, cfg.Observability)
	defer func() {
		if otelProvider != nil {
			_ = otelProvider.Shutdown(ctx)
		}
	}()

	// Optional PR-overlap support (drift reports link back to open PRs).
	var overlap drift.PROverlapFinder
	repoFullForOverlap := os.Getenv("GITHUB_REPOSITORY")
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" && repoFullForOverlap != "" {
		if parts := strings.SplitN(repoFullForOverlap, "/", 2); len(parts) == 2 {
			if client, err := gh.New(ctx, tok, parts[0], parts[1]); err == nil {
				overlap = drift.NewGitHubPROverlap(client)
			}
		}
	}

	opts := drift.Options{
		Engine:           engine,
		RepoRoot:         root,
		Decls:            decls,
		Filter:           filter,
		IncludePatterns:  include,
		ExcludePatterns:  exclude,
		AuthResolver:     resolver,
		StateStore:       &drift.StateStore{Blob: store},
		SuppressionStore: &drift.SuppressionStore{Blob: store},
		Redactor:         run.BuildRedactor(cfg.Shared),
		OTEL:             otelProvider,
		PROverlap:        overlap,
	}
	if cfg.Drift != nil {
		opts.RefreshFirst = cfg.Drift.Behavior.RefreshBeforeCheck
		opts.Parallel = cfg.Drift.Behavior.MaxParallelStacks
		if cfg.Drift.Behavior.StateBootstrap.Mode != "" {
			opts.BootstrapMode = cfg.Drift.Behavior.StateBootstrap.Mode
		}
		if w := cfg.Drift.Freshness.Window; w != "" && (cfg.Drift.Freshness.Enabled || flagBool(cmd, "if-stale")) {
			if d, err := time.ParseDuration(w); err == nil {
				opts.FreshnessWindow = d
			}
		}
	}

	out, err := drift.Run(ctx, opts)
	if err != nil {
		return err
	}

	report := drift.ReportMarkdown(out)
	if err := drift.WriteArtifacts(ctx, store, out, report); err != nil {
		return err
	}

	// $GITHUB_STEP_SUMMARY: always write when set.
	// 0644 is correct here - the file is rendered in the public Actions UI
	// and the runner writes it as the workflow user; tighter perms break
	// the runner's own reader.
	if p := os.Getenv("GITHUB_STEP_SUMMARY"); p != "" {
		_ = os.WriteFile(p, []byte(report), 0o644) // #nosec G306
	}

	// Dispatch to configured drift sinks.
	repoFull := os.Getenv("GITHUB_REPOSITORY")
	owner, repo := "", ""
	if parts := strings.SplitN(repoFull, "/", 2); len(parts) == 2 {
		owner, repo = parts[0], parts[1]
	}
	annotationEmitters := run.BuildAnnotationEmitters(cfg.Observability)
	sinks, serr := sinkfac.Build(ctx, cfg.Drift, sinkfac.Options{
		SlackToken:         os.Getenv("SLACK_BOT_TOKEN"),
		GitHubToken:        os.Getenv("GITHUB_TOKEN"),
		GitHubOwner:        owner,
		GitHubRepo:         repo,
		AnnotationEmitters: annotationEmitters,
	})
	if serr != nil {
		return serr
	}
	if len(sinks) > 0 {
		// Import the sinks package from its own path to avoid a cycle.
		errs := dispatchSinks(ctx, sinks, out)
		for _, e := range errs {
			fmt.Fprintf(cmd.ErrOrStderr(), "sink error: %v\n", e)
		}
	}

	fmt.Fprintln(cmd.OutOrStdout(), report)
	return nil
}

func buildScope(cfg *config.Config, cmd *cobra.Command) (include, exclude []string) {
	if cfg.Drift != nil {
		include = append(include, cfg.Drift.Scope.IncludePatterns...)
		exclude = append(exclude, cfg.Drift.Scope.ExcludePatterns...)
	}
	if sched := flagStringOrDefault(cmd, "schedule", ""); sched != "" && cfg.Drift != nil {
		if s, ok := cfg.Drift.Schedules[sched]; ok {
			include = append([]string{}, s.Patterns...)
			exclude = append([]string{}, s.ExcludePatterns...)
		}
	}
	if pat := flagStringOrDefault(cmd, "pattern", ""); pat != "" {
		include = []string{pat}
	}
	return
}

func driftStatus(cmd *cobra.Command, _ []string) error {
	ctx, cfg, root, err := loadDriftCtx(cmd)
	if err != nil {
		return err
	}
	store, err := factory.Open(ctx, cfg.Shared.Bucket, root)
	if err != nil {
		return err
	}
	ss := &drift.StateStore{Blob: store}
	keys, err := store.List(ctx, "drift/state")
	if err != nil {
		return err
	}
	want := flagStringOrDefault(cmd, "stack", "")
	w := cmd.OutOrStdout()
	for _, k := range keys {
		trimmed := strings.TrimPrefix(k, "drift/state/")
		trimmed = strings.TrimSuffix(trimmed, ".json")
		parts := strings.SplitN(trimmed, "/", 2)
		if len(parts) != 2 {
			continue
		}
		if want != "" && trimmed != want {
			continue
		}
		st, err := ss.Load(ctx, parts[0], parts[1])
		if err != nil {
			continue
		}
		fmt.Fprintf(w, "%s/%s\tlast=%s\tat=%s\tongoing_since=%s\n",
			st.Project, st.Stack, st.LastOutcome,
			st.LastCheckedAt.Format(time.RFC3339),
			st.OngoingSince.Format(time.RFC3339))
	}
	return nil
}

func driftReport(cmd *cobra.Command, _ []string) error {
	ctx, cfg, root, err := loadDriftCtx(cmd)
	if err != nil {
		return err
	}
	store, err := factory.Open(ctx, cfg.Shared.Bucket, root)
	if err != nil {
		return err
	}
	// Render latest report.md from drift/runs/ (latest = alphabetically
	// last run-id; run IDs include timestamps so this is chronological).
	keys, err := store.List(ctx, "drift/runs")
	if err != nil {
		return err
	}
	var latest string
	for _, k := range keys {
		if strings.HasSuffix(k, "/report.md") && k > latest {
			latest = k
		}
	}
	if latest == "" {
		fmt.Fprintln(cmd.OutOrStdout(), "no drift runs found")
		return nil
	}
	rc, _, err := store.Get(ctx, latest)
	if err != nil {
		return err
	}
	defer rc.Close()
	buf := make([]byte, 64*1024)
	n, _ := rc.Read(buf)
	fmt.Fprint(cmd.OutOrStdout(), string(buf[:n]))
	return nil
}

func driftSuppressAdd(cmd *cobra.Command, args []string) error {
	ctx, cfg, root, err := loadDriftCtx(cmd)
	if err != nil {
		return err
	}
	parts := splitRef(args[0])
	if parts == nil {
		return fmt.Errorf("expected project/stack, got %q", args[0])
	}
	dur, err := time.ParseDuration(flagStringOrDefault(cmd, "until", "24h"))
	if err != nil {
		return err
	}
	store, err := factory.Open(ctx, cfg.Shared.Bucket, root)
	if err != nil {
		return err
	}
	ss := &drift.SuppressionStore{Blob: store}
	return ss.Set(ctx, drift.Suppression{
		Project: parts[0], Stack: parts[1],
		Until:  time.Now().Add(dur),
		Reason: flagStringOrDefault(cmd, "reason", ""),
	})
}

func driftSuppressList(cmd *cobra.Command, _ []string) error {
	ctx, cfg, root, err := loadDriftCtx(cmd)
	if err != nil {
		return err
	}
	store, err := factory.Open(ctx, cfg.Shared.Bucket, root)
	if err != nil {
		return err
	}
	ss := &drift.SuppressionStore{Blob: store}
	active, err := ss.List(ctx, time.Now())
	if err != nil {
		return err
	}
	w := cmd.OutOrStdout()
	for _, s := range active {
		fmt.Fprintf(w, "%s/%s\tuntil=%s\treason=%s\n", s.Project, s.Stack, s.Until.Format(time.RFC3339), s.Reason)
	}
	return nil
}

func driftSuppressClear(cmd *cobra.Command, args []string) error {
	ctx, cfg, root, err := loadDriftCtx(cmd)
	if err != nil {
		return err
	}
	parts := splitRef(args[0])
	if parts == nil {
		return fmt.Errorf("expected project/stack, got %q", args[0])
	}
	store, err := factory.Open(ctx, cfg.Shared.Bucket, root)
	if err != nil {
		return err
	}
	ss := &drift.SuppressionStore{Blob: store}
	return ss.Clear(ctx, parts[0], parts[1])
}
