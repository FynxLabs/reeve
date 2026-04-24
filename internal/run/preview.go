package run

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/thefynx/reeve/internal/auth"
	"github.com/thefynx/reeve/internal/blob"
	"github.com/thefynx/reeve/internal/config/schemas"
	"github.com/thefynx/reeve/internal/core/discovery"
	"github.com/thefynx/reeve/internal/core/render"
	"github.com/thefynx/reeve/internal/core/summary"
	"github.com/thefynx/reeve/internal/iac"
	"github.com/thefynx/reeve/internal/observability/annotations"
	reeveotel "github.com/thefynx/reeve/internal/observability/otel"
)

// prReader is the subset of VCS we need for preview.
type prReader interface {
	ListChangedFiles(ctx context.Context, number int) ([]string, error)
}

// commentPoster is what preview writes back to.
type commentPoster interface {
	UpsertComment(ctx context.Context, number int, body, marker string) error
}

// Engine is what run/preview.go needs from an IaC adapter.
type Engine interface {
	iac.Enumerator
	iac.Previewer
	Name() string
}

// PreviewInput wires the dependencies and run context together.
type PreviewInput struct {
	PRNumber      int
	CommitSHA     string
	RunNumber     int
	CIRunURL      string
	RepoRoot      string
	Engine        Engine
	Config        *schemas.Engine
	Shared        *schemas.Shared
	AuthConfig    *schemas.Auth
	AuthRegistry  *auth.Registry
	Notifications *schemas.Notifications
	OTEL          *reeveotel.Provider
	Annotations   []annotations.Emitter
	Blob          blob.Store
	VCS           prReader      // may be nil for --local
	Comments      commentPoster // may be nil for --local
	// Local skips change-mapping (run on all declared stacks).
	Local bool
}

// PreviewOutput bundles the artifacts from a preview run.
type PreviewOutput struct {
	Stacks      []summary.StackSummary
	CommentBody string
	RunID       string
	DurationSec int
}

// Preview runs preview for every stack affected by the PR's changed files
// (or every declared stack if Local is true), writes artifacts to Blob,
// renders a PR comment, and — if Comments is non-nil — upserts it.
func Preview(ctx context.Context, in PreviewInput) (*PreviewOutput, error) {
	start := time.Now()
	runID := fmt.Sprintf("run-%d-%s", in.RunNumber, shortSHA(in.CommitSHA))

	// OTEL root span for this preview run.
	ctx, endRun := in.OTEL.StartRunSpan(ctx, "preview", in.PRNumber, in.CommitSHA)
	outcome := "success"
	defer func() { endRun(outcome) }()

	if err := PulumiLogin(ctx, in.Config); err != nil {
		outcome = "failed"
		return nil, err
	}

	enum, err := in.Engine.EnumerateStacks(ctx, in.RepoRoot)
	if err != nil {
		outcome = "failed"
		return nil, fmt.Errorf("enumerate stacks: %w", err)
	}

	decls, filter := declarationsFromConfig(in.Config)
	declared := discovery.Resolve(enum, decls, filter)

	var target []discovery.Stack
	if in.Local || in.VCS == nil {
		target = declared
	} else {
		changed, err := in.VCS.ListChangedFiles(ctx, in.PRNumber)
		if err != nil {
			return nil, fmt.Errorf("list changed files: %w", err)
		}
		cm := changeMappingFromConfig(in.Config)
		target = discovery.Affected(declared, changed, cm)
	}

	summaries := make([]summary.StackSummary, 0, len(target))
	for _, s := range target {
		ss := runPreviewOne(ctx, in, s)
		summaries = append(summaries, ss)
	}

	sort := "status_grouped"
	if in.Shared != nil && in.Shared.Comments.Sort != "" {
		sort = in.Shared.Comments.Sort
	}
	dur := int(time.Since(start).Seconds())

	body := render.Preview(render.PreviewInput{
		Op:          "preview",
		RunNumber:   in.RunNumber,
		CommitSHA:   in.CommitSHA,
		DurationSec: dur,
		CIRunURL:    in.CIRunURL,
		Stacks:      summaries,
		SortMode:    sort,
	})

	if err := writeManifest(ctx, in.Blob, in.PRNumber, runID, summaries, in.CommitSHA); err != nil {
		return nil, fmt.Errorf("write manifest: %w", err)
	}

	if in.Comments != nil && in.PRNumber > 0 {
		if err := in.Comments.UpsertComment(ctx, in.PRNumber, body, render.Marker); err != nil {
			return nil, fmt.Errorf("upsert pr comment: %w", err)
		}
	}

	// Slack runs last in the pipeline so upstream failures are captured.
	if in.PRNumber > 0 && in.Notifications != nil {
		slackBackend := BuildSlackBackend(in.Notifications, in.Blob)
		if err := NotifySlackPlanReady(ctx, slackBackend, in.Notifications,
			in.PRNumber, in.CommitSHA, in.CIRunURL, "", "", nil, summaries); err != nil {
			fmt.Printf("slack notify: %v\n", err)
		}
	}

	return &PreviewOutput{
		Stacks:      summaries,
		CommentBody: body,
		RunID:       runID,
		DurationSec: dur,
	}, nil
}

func runPreviewOne(ctx context.Context, in PreviewInput, s discovery.Stack) summary.StackSummary {
	redactor := BuildRedactor(in.Shared)

	authEnv, authErr := ResolveAuthEnv(ctx, in.AuthConfig, in.AuthRegistry, s.Ref(), auth.ModePreview)
	if authErr != nil {
		return summary.StackSummary{
			Project: s.Project, Stack: s.Name, Env: s.Env,
			Status: summary.StatusError, Error: redactor.Redact(authErr.Error()),
		}
	}
	// Register every credential literal with the redactor — if any leaks
	// into stdout, it gets scrubbed.
	for _, v := range authEnv {
		redactor.AddSecret(v)
	}

	stackCtx, endStack := in.OTEL.StartStackSpan(ctx, s.Project, s.Name, s.Env, "preview")
	stackStart := time.Now()
	res, err := in.Engine.Preview(stackCtx, s, iac.PreviewOpts{
		Cwd: absJoin(in.RepoRoot, s.Path),
		Env: authEnv,
	})
	ss := summary.StackSummary{
		Project: s.Project,
		Stack:   s.Name,
		Env:     s.Env,
	}
	defer func() {
		outcome := "success"
		if ss.Status == summary.StatusError {
			outcome = "error"
		}
		endStack(outcome, time.Since(stackStart).Seconds())
	}()
	if err != nil {
		ss.Status = summary.StatusError
		ss.Error = redactor.Redact(err.Error())
		return ss
	}
	ss.Counts = res.Counts
	ss.PlanSummary = redactor.Redact(res.PlanSummary)
	ss.FullPlan = redactor.Redact(res.FullPlan)
	in.OTEL.RecordStackChanges(ctx, s.Project, s.Name, res.Counts.Add, res.Counts.Change, res.Counts.Delete, res.Counts.Replace)
	if res.Error != "" {
		ss.Status = summary.StatusError
		ss.Error = redactor.Redact(res.Error)
		return ss
	}
	if ss.Counts.Total() == 0 {
		ss.Status = summary.StatusNoOp
	} else {
		ss.Status = summary.StatusReady
	}
	return ss
}

func declarationsFromConfig(e *schemas.Engine) ([]discovery.Declaration, discovery.Filter) {
	if e == nil {
		return nil, discovery.Filter{}
	}
	decls := make([]discovery.Declaration, 0, len(e.Engine.Stacks))
	for _, s := range e.Engine.Stacks {
		d := discovery.Declaration{
			Project: s.Project,
			Path:    s.Path,
			Pattern: s.Pattern,
			Stacks:  s.Stacks,
		}
		decls = append(decls, d)
	}
	var filter discovery.Filter
	for _, ex := range e.Engine.Filters.Exclude {
		if ex.Stack != "" {
			filter.StackPatterns = append(filter.StackPatterns, ex.Stack)
		}
		if ex.Pattern != "" {
			filter.PathPatterns = append(filter.PathPatterns, ex.Pattern)
		}
	}
	return decls, filter
}

func changeMappingFromConfig(e *schemas.Engine) discovery.ChangeMapping {
	if e == nil {
		return discovery.ChangeMapping{}
	}
	cm := discovery.ChangeMapping{
		IgnoreChanges: e.Engine.ChangeMapping.IgnoreChanges,
	}
	if len(e.Engine.ChangeMapping.ExtraTriggers) > 0 {
		cm.ExtraTriggers = map[string][]string{}
		for _, t := range e.Engine.ChangeMapping.ExtraTriggers {
			cm.ExtraTriggers[t.Project] = append(cm.ExtraTriggers[t.Project], t.Paths...)
		}
	}
	return cm
}

// manifest is the JSON we write to Blob for each run. Consumed by
// Phase 2 apply (freshness check, saved plan lookup).
type manifest struct {
	RunID     string                 `json:"run_id"`
	PR        int                    `json:"pr"`
	CommitSHA string                 `json:"commit_sha"`
	Op        string                 `json:"op"`
	CreatedAt string                 `json:"created_at"`
	Stacks    []summary.StackSummary `json:"stacks"`
}

func writeManifest(ctx context.Context, store blob.Store, pr int, runID string, stacks []summary.StackSummary, sha string) error {
	if store == nil {
		return nil
	}
	m := manifest{
		RunID:     runID,
		PR:        pr,
		CommitSHA: sha,
		Op:        "preview",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Stacks:    stacks,
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	key := fmt.Sprintf("runs/pr-%d/%s/manifest.json", pr, runID)
	if pr == 0 {
		key = fmt.Sprintf("runs/local/%s/manifest.json", runID)
	}
	_, err = store.Put(ctx, key, strings.NewReader(string(data)))
	return err
}

func shortSHA(s string) string {
	if len(s) >= 7 {
		return s[:7]
	}
	if s == "" {
		return "unknown"
	}
	return s
}

func absJoin(root, rel string) string {
	if rel == "" || rel == "." {
		return root
	}
	return root + "/" + rel
}
