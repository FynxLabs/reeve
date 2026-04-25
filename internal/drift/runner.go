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

	// Attach overlapping-PR info to drifted items.
	if opts.PROverlap != nil {
		for i, it := range items {
			if it.Outcome != OutcomeDriftDetected {
				continue
			}
			path := stackPath(targets, it.Project, it.Stack)
			if path == "" {
				continue
			}
			prs, err := opts.PROverlap.FindOverlappingPRs(ctx, []string{path})
			if err == nil && len(prs) > 0 {
				items[i].OverlappingPRs = prs
			}
		}
	}

	finished := now()
	out := &RunOutput{
		RunID:      opts.RunID,
		Items:      items,
		Skipped:    skipped,
		Events:     events,
		StartedAt:  started,
		FinishedAt: finished,
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
		item.Error = "first run with bootstrap=require_manual (run `reeve drift bootstrap`)"
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
	res, _ := opts.Engine.DriftCheck(ctx, s, iac.PreviewOpts{
		Cwd: opts.RepoRoot + "/" + s.Path,
		Env: env,
	}, opts.RefreshFirst)
	item.DurationMS = time.Since(start).Milliseconds()
	item.Counts = res
	item.Counts.PlanSummary = opts.Redactor.Redact(res.PlanSummary)
	item.Counts.FullPlan = opts.Redactor.Redact(res.FullPlan)

	if res.Error != "" {
		item.Outcome = OutcomeError
		item.Error = opts.Redactor.Redact(res.Error)
	} else if res.Counts.Total() > 0 {
		item.Outcome = OutcomeDriftDetected
		item.Fingerprint = Fingerprint(extractURNs(res.FullPlan))
		// Bootstrap mode: first run with baseline = silent.
		if prev.LastCheckedAt.IsZero() && opts.BootstrapMode == "baseline" {
			item.Outcome = OutcomeNoDrift // record as baseline, no event
		}
	} else {
		item.Outcome = OutcomeNoDrift
	}

	result := Result{
		Project: s.Project, Stack: s.Name,
		Outcome: item.Outcome, Fingerprint: item.Fingerprint,
		CheckedAt: now, ErrorMessage: item.Error,
	}
	ev, next := Classify(prev, result)
	item.Event = ev
	if opts.StateStore != nil {
		_ = opts.StateStore.Save(ctx, next)
	}
	return item, ev, false, ""
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

// extractURNs pulls Pulumi URNs from a plan output for fingerprinting.
// Best-effort - pattern matches `"urn":"urn:pulumi:...::...::...::name"`.
func extractURNs(plan string) []string {
	var urns []string
	for _, marker := range []string{`"urn":"urn:`, `"urn": "urn:`} {
		for i := 0; i < len(plan); {
			idx := strings.Index(plan[i:], marker)
			if idx < 0 {
				break
			}
			start := i + idx + len(marker) - len("urn:") // include "urn:"
			end := strings.IndexByte(plan[start:], '"')
			if end < 0 {
				break
			}
			urns = append(urns, plan[start:start+end])
			i = start + end + 1
		}
	}
	return urns
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
