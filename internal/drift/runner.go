package drift

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/thefynx/reeve/internal/blob"
	"github.com/thefynx/reeve/internal/core/discovery"
	"github.com/thefynx/reeve/internal/core/redact"
	"github.com/thefynx/reeve/internal/iac"
	reeveotel "github.com/thefynx/reeve/internal/observability/otel"
)

// Engine is the subset of iac capabilities the drift runner needs.
type Engine interface {
	Name() string
	EnumerateStacks(ctx context.Context, root string) ([]discovery.Stack, error)
	DriftCheck(ctx context.Context, stack discovery.Stack, opts iac.PreviewOpts, refreshFirst bool) (iac.PreviewResult, error)
}

// AuthResolver returns env vars for a stack (usually via run.ResolveAuthEnv).
type AuthResolver func(ctx context.Context, stackRef string) (map[string]string, error)

// Options configures a drift run.
type Options struct {
	Engine   Engine
	RepoRoot string
	Decls    []discovery.Declaration
	Filter   discovery.Filter
	// Patterns further narrow the targets. Empty = all declared.
	IncludePatterns []string
	ExcludePatterns []string
	// RefreshFirst triggers `pulumi refresh` before the check.
	RefreshFirst bool
	// Freshness: skip a stack if its last successful check is within
	// FreshnessWindow. Zero disables.
	FreshnessWindow time.Duration
	// Bootstrap: how to handle first runs (no state file).
	BootstrapMode string // baseline | alert_all | require_manual
	// AuthResolver acquires creds per stack.
	AuthResolver AuthResolver
	// Parallel caps concurrent checks. Default 1.
	Parallel int
	// StateStore + SuppressionStore persist outcomes.
	StateStore       *StateStore
	SuppressionStore *SuppressionStore
	// RunID is used for the run artifact prefix.
	RunID string
	// Redactor scrubs stdout.
	Redactor *redact.Redactor
	// OTEL is optional. nil is safe.
	OTEL *reeveotel.Provider
	// PROverlap is optional. If set, drifted stacks are annotated with
	// open PRs touching their paths.
	PROverlap PROverlapFinder
	// Now is injectable for tests.
	Now func() time.Time
}

// RunOutput aggregates per-stack results and events.
type RunOutput struct {
	RunID      string
	Items      []Item
	Skipped    []string // "project/stack" refs
	Events     []Event  // parallel to Items (zero-value Event = none)
	StartedAt  time.Time
	FinishedAt time.Time
	// OverlapWarning is set when the open-PR overlap scan could not check
	// every PR (fetch failure or scan cap). The per-item OverlappingPRs are
	// then a lower bound, never proof of "no overlap"; the report surfaces
	// this warning naming the PRs that could not be checked.
	OverlapWarning string
}

// Item is one stack's drift check outcome.
type Item struct {
	Project        string
	Stack          string
	Env            string
	Outcome        Outcome
	Event          Event
	Counts         iac.PreviewResult
	Fingerprint    string
	Error          string
	DurationMS     int64
	Suppressed     bool
	OverlappingPRs []OverlappingPR
}

// OverlappingPR is an open PR that touches paths owned by a drifted stack.
// Populated by the runner when the optional PROverlapFinder is supplied.
type OverlappingPR struct {
	Number   int       `json:"number"`
	Author   string    `json:"author"`
	OpenedAt time.Time `json:"opened_at"`
	HeadSHA  string    `json:"head_sha"`
	Paths    []string  `json:"paths,omitempty"`
}

// PROverlapFinder resolves open PRs touching the given paths. Usually
// backed by the GitHub VCS adapter's ListOpenPRsTouchingPaths.
type PROverlapFinder interface {
	FindOverlappingPRs(ctx context.Context, paths []string) ([]OverlappingPR, error)
}

// Ref returns "project/stack".
func (i Item) Ref() string { return i.Project + "/" + i.Stack }

// Run executes drift checks for every declared-and-included stack. State
// transitions persist; events surface via RunOutput.Events.
func Run(ctx context.Context, opts Options) (*RunOutput, error) {
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}
	started := now()
	if opts.RunID == "" {
		opts.RunID = fmt.Sprintf("drift-%s", started.UTC().Format("20060102T150405Z"))
	}
	if opts.Parallel < 1 {
		opts.Parallel = 1
	}
	if opts.Redactor == nil {
		opts.Redactor = redact.New()
	}

	enum, err := opts.Engine.EnumerateStacks(ctx, opts.RepoRoot)
	if err != nil {
		return nil, fmt.Errorf("enumerate: %w", err)
	}
	declared := discovery.Resolve(enum, opts.Decls, opts.Filter)
	targets := filterPatterns(declared, opts.IncludePatterns, opts.ExcludePatterns)

	sem := make(chan struct{}, opts.Parallel)
	var mu sync.Mutex
	items := make([]Item, 0, len(targets))
	events := make([]Event, 0, len(targets))
	skipped := []string{}
	var wg sync.WaitGroup

	for _, s := range targets {
		s := s
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			item, ev, skip, reason := runOne(ctx, opts, s, now())
			mu.Lock()
			defer mu.Unlock()
			if skip {
				skipped = append(skipped, s.Ref()+":"+reason)
				return
			}
			items = append(items, item)
			events = append(events, ev)
		}()
	}
	wg.Wait()

	// Attach overlapping-PR info to drifted items. One scan for every
	// drifted path (the per-PR file listing is the expensive part; scanning
	// once per drifted stack multiplied it), then match PRs to items
	// locally. A scan failure degrades to a warning - it must never read as
	// "no overlap".
	overlapWarning := ""
	if opts.PROverlap != nil {
		pathByItem := map[int]string{}
		var paths []string
		for i, it := range items {
			if it.Outcome != OutcomeDriftDetected {
				continue
			}
			if path := stackPath(targets, it.Project, it.Stack); path != "" {
				pathByItem[i] = path
				paths = append(paths, path)
			}
		}
		if len(paths) > 0 {
			prs, err := opts.PROverlap.FindOverlappingPRs(ctx, paths)
			for i, path := range pathByItem {
				for _, pr := range prs {
					if prTouchesPath(pr, path) {
						items[i].OverlappingPRs = append(items[i].OverlappingPRs, pr)
					}
				}
			}
			if err != nil {
				overlapWarning = "open-PR overlap scan incomplete - overlap info may be missing: " + err.Error()
			}
		}
	}

	finished := now()
	out := &RunOutput{
		RunID:          opts.RunID,
		Items:          items,
		Skipped:        skipped,
		Events:         events,
		StartedAt:      started,
		FinishedAt:     finished,
		OverlapWarning: overlapWarning,
	}

	// Emit OTEL metrics for the whole run.
	if opts.OTEL != nil {
		runOutcome := "success"
		stacksInDriftByEnv := map[string]int64{}
		for i, it := range items {
			opts.OTEL.RecordDriftDuration(ctx, it.Project, it.Stack, it.Env, float64(it.DurationMS)/1000.0)
			ev := events[i]
			opts.OTEL.RecordDriftDetection(ctx, it.Project, it.Stack, it.Env, string(it.Outcome))
			if it.Outcome == OutcomeError {
				runOutcome = "failed"
			}
			if it.Outcome == OutcomeDriftDetected {
				stacksInDriftByEnv[it.Env]++
			}
			// ongoing_duration: emit hours drifted for ongoing items.
			if ev == EventDriftOngoing {
				// Load state to find OngoingSince.
				if opts.StateStore != nil {
					if st, err := opts.StateStore.Load(ctx, it.Project, it.Stack); err == nil && !st.OngoingSince.IsZero() {
						hours := finished.Sub(st.OngoingSince).Hours()
						opts.OTEL.RecordOngoingDuration(ctx, it.Project, it.Stack, hours)
					}
				}
			}
		}
		for env, n := range stacksInDriftByEnv {
			opts.OTEL.RecordStacksInDrift(ctx, env, n)
		}
		opts.OTEL.RecordDriftRun(ctx, runOutcome)
	}

	return out, nil
}

func runOne(ctx context.Context, opts Options, s discovery.Stack, now time.Time) (Item, Event, bool, string) {
	ref := s.Ref()
	item := Item{Project: s.Project, Stack: s.Name, Env: s.Env}

	// Suppression check.
	if opts.SuppressionStore != nil {
		sup, ok, _ := opts.SuppressionStore.Get(ctx, s.Project, s.Name)
		if ok && sup.Active(now) {
			item.Suppressed = true
			item.Outcome = OutcomeSkipped
			return item, EventNone, true, "suppressed"
		}
	}

	// Freshness check.
	prev := State{Project: s.Project, Stack: s.Name}
	if opts.StateStore != nil {
		p, err := opts.StateStore.Load(ctx, s.Project, s.Name)
		if err == nil {
			prev = p
		}
	}
	if opts.FreshnessWindow > 0 && prev.LastOutcome != OutcomeDriftDetected {
		if !prev.LastSuccessfulAt.IsZero() && now.Sub(prev.LastSuccessfulAt) < opts.FreshnessWindow {
			item.Outcome = OutcomeSkipped
			return item, EventNone, true, "fresh"
		}
	}

	// Bootstrap guard: first run with require_manual → refuse.
	if prev.LastCheckedAt.IsZero() && opts.BootstrapMode == "require_manual" {
		item.Outcome = OutcomeError
		item.Error = "first run with state_bootstrap.mode=require_manual; run `reeve drift bootstrap` to record the baseline"
		return item, EventCheckFailed, false, ""
	}

	// Auth.
	var env map[string]string
	if opts.AuthResolver != nil {
		var err error
		env, err = opts.AuthResolver(ctx, ref)
		if err != nil {
			item.Outcome = OutcomeError
			item.Error = opts.Redactor.Redact(err.Error())
			return item, EventCheckFailed, false, ""
		}
		for _, v := range env {
			opts.Redactor.AddSecret(v)
		}
	}

	start := time.Now()
	res, checkErr := opts.Engine.DriftCheck(ctx, s, iac.PreviewOpts{
		Cwd: opts.RepoRoot + "/" + s.Path,
		Env: env,
	}, opts.RefreshFirst)
	item.DurationMS = time.Since(start).Milliseconds()
	item.Counts = res
	item.Counts.PlanSummary = opts.Redactor.Redact(res.PlanSummary)
	item.Counts.FullPlan = opts.Redactor.Redact(res.FullPlan)

	// A failed check (non-nil error, or an Error string) is NEVER "no drift".
	// Treating an empty/failed result as no-drift would falsely resolve an
	// active drift alert, so classify it as an error and fail closed.
	switch {
	case checkErr != nil || res.Error != "":
		item.Outcome = OutcomeError
		if res.Error != "" {
			item.Error = opts.Redactor.Redact(res.Error)
		} else {
			item.Error = opts.Redactor.Redact(checkErr.Error())
		}
	case res.Counts.Total() > 0:
		item.Outcome = OutcomeDriftDetected
		// Fingerprint only the drifted resources so a change in *which*
		// resources drift re-fires the alert (see Classify fingerprint check).
		item.Fingerprint = Fingerprint(res.DriftedURNs)
	default:
		item.Outcome = OutcomeNoDrift
	}

	result := Result{
		Project: s.Project, Stack: s.Name,
		Outcome: item.Outcome, Fingerprint: item.Fingerprint,
		CheckedAt: now, ErrorMessage: item.Error,
	}
	ev, next := Classify(prev, result)

	// Baseline bootstrap: on the first-ever check, accept whatever state we
	// find (including pre-existing drift) as the baseline and stay silent.
	// The state - fingerprint included - is still persisted, so only *new*
	// drift beyond the baseline alerts on later runs. Previously this
	// recorded no_drift and dropped the fingerprint, so identical drift
	// re-fired as a fresh detection on the second run.
	if prev.LastCheckedAt.IsZero() && opts.BootstrapMode == "baseline" && ev != EventCheckFailed {
		ev = EventNone
	}
	item.Event = ev
	if opts.StateStore != nil {
		_ = opts.StateStore.Save(ctx, next)
	}
	return item, ev, false, ""
}

// prTouchesPath reports whether any of the PR's changed files fall under
// the stack path (exact match or directory prefix). Mirrors the VCS
// adapter's own path intersection so a multi-path scan can be re-split
// per stack.
func prTouchesPath(pr OverlappingPR, path string) bool {
	for _, f := range pr.Paths {
		if f == path || strings.HasPrefix(f, path+"/") {
			return true
		}
	}
	return false
}

// stackPath returns the filesystem path declared for a stack ref,
// searching the enumerated targets. Empty if not found.
func stackPath(stacks []discovery.Stack, project, name string) string {
	for _, s := range stacks {
		if s.Project == project && s.Name == name {
			return s.Path
		}
	}
	return ""
}

func filterPatterns(stacks []discovery.Stack, include, exclude []string) []discovery.Stack {
	out := make([]discovery.Stack, 0, len(stacks))
outer:
	for _, s := range stacks {
		ref := s.Ref()
		if len(include) > 0 {
			hit := false
			for _, p := range include {
				if ok, _ := doublestar.Match(p, ref); ok {
					hit = true
					break
				}
			}
			if !hit {
				continue
			}
		}
		for _, p := range exclude {
			if ok, _ := doublestar.Match(p, ref); ok {
				continue outer
			}
		}
		out = append(out, s)
	}
	return out
}

// --- artifact writing ---

// WriteArtifacts persists run artifacts under drift/runs/{run-id}/.
// Returns the rendered report body (for callers that want to forward
// to $GITHUB_STEP_SUMMARY).
func WriteArtifacts(ctx context.Context, store blob.Store, out *RunOutput, report string) error {
	if store == nil {
		return nil
	}
	manifest := map[string]any{
		"run_id":      out.RunID,
		"started_at":  out.StartedAt.Format(time.RFC3339),
		"finished_at": out.FinishedAt.Format(time.RFC3339),
		"item_count":  len(out.Items),
		"skipped":     out.Skipped,
	}
	if out.OverlapWarning != "" {
		manifest["overlap_warning"] = out.OverlapWarning
	}
	mb, _ := json.MarshalIndent(manifest, "", "  ")
	if _, err := store.Put(ctx, fmt.Sprintf("drift/runs/%s/manifest.json", out.RunID), bytes.NewReader(mb)); err != nil {
		return err
	}
	for _, it := range out.Items {
		data, _ := json.MarshalIndent(it, "", "  ")
		key := fmt.Sprintf("drift/runs/%s/results/%s-%s.json", out.RunID, it.Project, it.Stack)
		if _, err := store.Put(ctx, key, bytes.NewReader(data)); err != nil {
			return err
		}
	}
	if report != "" {
		if _, err := store.Put(ctx, fmt.Sprintf("drift/runs/%s/report.md", out.RunID), bytes.NewReader([]byte(report))); err != nil {
			return err
		}
	}
	return nil
}
