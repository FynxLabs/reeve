package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/thefynx/reeve/internal/auth"
	authfac "github.com/thefynx/reeve/internal/auth/factory"
	"github.com/thefynx/reeve/internal/blob/factory"
	"github.com/thefynx/reeve/internal/config"
	"github.com/thefynx/reeve/internal/config/schemas"
	"github.com/thefynx/reeve/internal/core/discovery"
	"github.com/thefynx/reeve/internal/drift"
	"github.com/thefynx/reeve/internal/iac"
	"github.com/thefynx/reeve/internal/notify"
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

	bootstrapSub := &cobra.Command{Use: "bootstrap", Short: "Record current stack state as the drift baseline (no events)", RunE: driftBootstrap}
	bootstrapSub.Flags().String("pattern", "", "Restrict to stacks matching this pattern")
	bootstrapSub.Flags().String("schedule", "", "Run a named schedule from drift.yaml")
	bootstrapSub.Flags().String("root", "", "Repo root (default: cwd)")

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

	cmd.AddCommand(runSub, bootstrapSub, statusSub, reportSub, suppress)
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
	applyLogConfig(cfg.LogSettings())
	if err := cfg.Validate(); err != nil {
		return nil, nil, "", err
	}
	return context.Background(), cfg, root, nil
}

func driftRun(cmd *cobra.Command, _ []string) error { return runDrift(cmd, false) }

// driftBootstrap records the current state of every stack as the drift
// baseline without emitting any events. Required to unblock
// state_bootstrap.mode=require_manual (whose first real run refuses until a
// baseline exists), and useful to silently seed state in any mode.
func driftBootstrap(cmd *cobra.Command, _ []string) error { return runDrift(cmd, true) }

func runDrift(cmd *cobra.Command, bootstrap bool) error {
	ctx, cfg, root, err := loadDriftCtx(cmd)
	if err != nil {
		return err
	}
	store, err := factory.Open(ctx, cfg.Shared.Bucket, root)
	if err != nil {
		return err
	}
	engineCfg := cfg.Engines[0]
	engine, err := iac.New(engineCfg.Engine)
	if err != nil {
		return err
	}

	// Build the auth resolver on top of the auth registry.
	authReg, err := authfac.Build(ctx, cfg.Auth)
	if err != nil {
		return err
	}
	resolver := func(ctx context.Context, ref string) (map[string]string, error) {
		// Drift currently doesn't expose a per-call cleanup hook; once the
		// drift runner gains stack-scoped lifecycles we should plumb the
		// cleanup func through. For now an unrun cleanup leaks the GCP WIF
		// temp file until the process exits, which is bounded by the run's
		// duration on GitHub Actions.
		env, _, err := run.ResolveAuthEnv(ctx, cfg.Auth, authReg, ref, auth.ModeDrift)
		return env, err
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

	include, exclude, err := buildScope(cfg, cmd)
	if err != nil {
		return err
	}

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

	if bootstrap {
		// Force baseline recording: bypass the require_manual refusal, accept
		// current state (including pre-existing drift) silently, and always
		// check (ignore freshness) so every stack gets a baseline.
		opts.BootstrapMode = "baseline"
		opts.FreshnessWindow = 0
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

	// Bootstrap is silent by design: state is recorded, no channels fire.
	if bootstrap {
		fmt.Fprintf(cmd.OutOrStdout(), "baseline recorded for %d stack(s); drift runs will now compare against it\n", len(out.Items))
		fmt.Fprintln(cmd.OutOrStdout(), report)
		return nil
	}

	// Dispatch to configured channels via the shared notify framework:
	// drift.yaml channels plus any notifications.yaml channels subscribed to
	// drift events.
	repoFull := os.Getenv("GITHUB_REPOSITORY")
	var issues notify.IssueClient
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		if parts := strings.SplitN(repoFull, "/", 2); len(parts) == 2 {
			if client, err := gh.New(ctx, tok, parts[0], parts[1]); err == nil {
				issues = client
			}
		}
	}
	var channelCfgs []schemas.ChannelYAML
	if cfg.Drift != nil {
		channelCfgs = append(channelCfgs, cfg.Drift.Channels...)
	}
	if cfg.Notifications != nil {
		channelCfgs = append(channelCfgs, cfg.Notifications.Channels...)
	}
	channels, serr := notify.Build(ctx, channelCfgs, notify.Deps{
		Blob:       store,
		Issues:     issues,
		Emitters:   run.BuildAnnotationEmitters(cfg.Observability),
		SlackToken: os.Getenv("SLACK_BOT_TOKEN"),
		RepoFull:   repoFull,
	})
	if serr != nil {
		return serr
	}
	if len(channels) > 0 {
		errs := notify.Dispatch(ctx, channels, drift.NotifyPayloads(out))
		for _, e := range errs {
			fmt.Fprintf(cmd.ErrOrStderr(), "channel error: %v\n", e)
		}
	}

	fmt.Fprintln(cmd.OutOrStdout(), report)
	return nil
}

func buildScope(cfg *config.Config, cmd *cobra.Command) (include, exclude []string, err error) {
	if cfg.Drift != nil {
		include = append(include, cfg.Drift.Scope.IncludePatterns...)
		exclude = append(exclude, cfg.Drift.Scope.ExcludePatterns...)
	}
	if sched := flagStringOrDefault(cmd, "schedule", ""); sched != "" {
		// An unknown schedule name must not silently fall back to the
		// global scope - a typo would run drift against every stack.
		var s schemas.Schedule
		ok := false
		if cfg.Drift != nil {
			s, ok = cfg.Drift.Schedules[sched]
		}
		if !ok {
			names := make([]string, 0)
			if cfg.Drift != nil {
				for n := range cfg.Drift.Schedules {
					names = append(names, n)
				}
			}
			sort.Strings(names)
			if len(names) == 0 {
				return nil, nil, fmt.Errorf("unknown schedule %q: no schedules configured in drift.yaml", sched)
			}
			return nil, nil, fmt.Errorf("unknown schedule %q: configured schedules are %s", sched, strings.Join(names, ", "))
		}
		include = append([]string{}, s.Patterns...)
		exclude = append([]string{}, s.ExcludePatterns...)
	}
	if pat := flagStringOrDefault(cmd, "pattern", ""); pat != "" {
		include = []string{pat}
	}
	return include, exclude, nil
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
	body, err := drift.StoredReport(ctx, store, flagStringOrDefault(cmd, "format", "markdown"))
	if err != nil {
		if errors.Is(err, drift.ErrNoRuns) {
			fmt.Fprintln(cmd.OutOrStdout(), "no drift runs found")
			return nil
		}
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), strings.TrimRight(body, "\n"))
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
	dur, err := parseDurationExtended(flagStringOrDefault(cmd, "until", "24h"))
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
