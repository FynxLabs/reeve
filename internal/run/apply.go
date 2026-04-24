package run

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/thefynx/reeve/internal/audit"
	"github.com/thefynx/reeve/internal/auth"
	"github.com/thefynx/reeve/internal/blob"
	blocks "github.com/thefynx/reeve/internal/blob/locks"
	"github.com/thefynx/reeve/internal/config/schemas"
	"github.com/thefynx/reeve/internal/core/approvals"
	"github.com/thefynx/reeve/internal/core/discovery"
	corelocks "github.com/thefynx/reeve/internal/core/locks"
	"github.com/thefynx/reeve/internal/core/preconditions"
	"github.com/thefynx/reeve/internal/core/render"
	"github.com/thefynx/reeve/internal/core/summary"
	"github.com/thefynx/reeve/internal/iac"
	"github.com/thefynx/reeve/internal/observability/annotations"
	reeveotel "github.com/thefynx/reeve/internal/observability/otel"
	"github.com/thefynx/reeve/internal/vcs"
	"github.com/thefynx/reeve/internal/vcs/codeowners"
)

// applyEngine is what run/apply.go needs from an IaC adapter.
type applyEngine interface {
	Engine
	iac.Applier
}

// applyVCS is the extended VCS surface for apply-time gate resolution.
type applyVCS interface {
	prReader
	commentPoster
	Capabilities() vcs.CommentCapabilities
	GetPR(ctx context.Context, number int) (*vcs.PR, error)
	// checks / up-to-date
	ChecksGreen(ctx context.Context, sha string, ignoreNames []string) (bool, []string, error)
	CompareBranches(ctx context.Context, base, head string) (int, error)
	// approvals
	approvals.Source
	// CODEOWNERS
	FetchCodeowners(ctx context.Context) (string, error)
	// team expansion (optional; may be called for team slugs in rules)
	ListTeamMembers(ctx context.Context, slug string) ([]string, error)
}

// ApplyInput wires dependencies and run context.
type ApplyInput struct {
	PRNumber      int
	CommitSHA     string
	RunNumber     int
	CIRunURL      string
	RepoRoot      string
	RepoFull      string // "owner/name" for audit log
	Actor         string
	Engine        applyEngine
	Config        *schemas.Engine
	Shared        *schemas.Shared
	AuthConfig    *schemas.Auth
	AuthRegistry  *auth.Registry
	Notifications *schemas.Notifications
	Blob          blob.Store
	Locks         *blocks.Store
	VCS           applyVCS
	AuditWriter   *audit.Writer
	OTEL          *reeveotel.Provider
	Annotations   []annotations.Emitter
}

// ApplyOutput bundles the artifacts from an apply run.
type ApplyOutput struct {
	Stacks      []summary.StackSummary
	CommentBody string
	RunID       string
	DurationSec int
	Blocked     bool // true if any stack was blocked by preconditions
}

// Apply runs apply for stacks affected by the PR. For each stack:
// 1. Acquire lock (or queue).
// 2. Evaluate preconditions.
// 3. If gates pass, run engine apply.
// 4. Release lock (promotes queue).
// 5. Emit audit entry.
// The PR comment is updated at the end with the aggregated results.
func Apply(ctx context.Context, in ApplyInput) (*ApplyOutput, error) {
	start := time.Now()
	runID := fmt.Sprintf("apply-%d-%s", in.RunNumber, shortSHA(in.CommitSHA))

	// OTEL root span for this run. Finished at return.
	ctx, endRun := in.OTEL.StartRunSpan(ctx, "apply", in.PRNumber, in.CommitSHA)
	// "outcome" is filled in at return below.
	outcome := "success"
	defer func() { endRun(outcome) }()

	// Annotation: apply started.
	PostAnnotation(ctx, in.Annotations, annotations.EventApplyStarted,
		"", "", "", "", "", in.PRNumber, in.CommitSHA)

	if err := PulumiLogin(ctx, in.Config); err != nil {
		return nil, err
	}

	// 1. Resolve affected stacks (same pipeline as preview).
	enum, err := in.Engine.EnumerateStacks(ctx, in.RepoRoot)
	if err != nil {
		return nil, fmt.Errorf("enumerate stacks: %w", err)
	}
	decls, filter := declarationsFromConfig(in.Config)
	declared := discovery.Resolve(enum, decls, filter)

	changed, err := in.VCS.ListChangedFiles(ctx, in.PRNumber)
	if err != nil {
		return nil, fmt.Errorf("list changed files: %w", err)
	}
	cm := changeMappingFromConfig(in.Config)
	target := discovery.Affected(declared, changed, cm)

	// 2. Per-stack context: PR + checks + upstream-commits + approvals + CODEOWNERS.
	pr, err := in.VCS.GetPR(ctx, in.PRNumber)
	if err != nil {
		return nil, fmt.Errorf("get pr: %w", err)
	}

	checksGreen, failingChecks, err := in.VCS.ChecksGreen(ctx, in.CommitSHA, []string{"reeve"})
	if err != nil {
		// Not fatal — record as failing and continue; gate evaluator will block.
		checksGreen = false
	}
	_ = failingChecks

	behind, err := in.VCS.CompareBranches(ctx, pr.BaseRef, in.CommitSHA)
	if err != nil {
		behind = 0
	}
	upToDate := behind == 0

	rawApprovals, _ := in.VCS.ListApprovals(ctx, approvals.PR{Number: in.PRNumber, HeadSHA: in.CommitSHA, Author: pr.Author, Changed: changed})

	// CODEOWNERS (optional).
	coContent, _ := in.VCS.FetchCodeowners(ctx)
	var coResolved map[string][]string
	if coContent != "" {
		rules := codeowners.Parse(strings.NewReader(coContent))
		coResolved = codeowners.Resolve(rules, changed)
	}

	// Freeze config.
	freezeCfg := toFreezeConfig(in.Shared)
	preCfg := toPreconditionsConfig(in.Shared)
	appCfg := toApprovalsConfig(in.Shared)
	ttl := lockTTL(in.Shared)

	now := time.Now()

	// Slack: notify applying before the loop (creates message on apply trigger path).
	if in.PRNumber > 0 && in.Notifications != nil {
		slackBackend := BuildSlackBackend(in.Notifications, in.Blob)
		preSummaries := make([]summary.StackSummary, 0, len(target))
		for _, s := range target {
			preSummaries = append(preSummaries, summary.StackSummary{
				Project: s.Project, Stack: s.Name, Env: s.Env,
			})
		}
		if err := NotifySlackApproved(ctx, slackBackend, in.Notifications,
			in.PRNumber, in.CommitSHA, in.CIRunURL, "", pr.Author, preSummaries); err != nil {
			fmt.Printf("slack notify approved: %v\n", err)
		}
		if err := NotifySlackApplying(ctx, slackBackend, in.Notifications,
			in.PRNumber, in.CommitSHA, in.CIRunURL, "", pr.Author, preSummaries); err != nil {
			fmt.Printf("slack notify applying: %v\n", err)
		}
	}

	// 3. Per-stack: acquire lock → eval gates → apply or block.
	summaries := make([]summary.StackSummary, 0, len(target))
	anyBlocked := false
	for _, s := range target {
		ss := summary.StackSummary{
			Project: s.Project, Stack: s.Name, Env: s.Env,
		}

		// Lock acquire.
		lock, acquired, err := in.Locks.TryAcquire(ctx, s.Project, s.Name, corelocks.Holder{
			PR: in.PRNumber, CommitSHA: in.CommitSHA, RunID: runID, Actor: in.Actor,
		}, ttl)
		if err != nil {
			ss.Status = summary.StatusError
			ss.Error = fmt.Sprintf("lock acquire: %v", err)
			summaries = append(summaries, ss)
			continue
		}
		lockBlockedBy := 0
		if !acquired && lock.Holder != nil {
			lockBlockedBy = lock.Holder.PR
		}

		// Resolve rules + approvals for this stack.
		rules := approvals.Resolve(appCfg, s.Ref())
		approvalsRes := approvals.Evaluate(rules, rawApprovals, approvals.PR{
			Number: in.PRNumber, HeadSHA: in.CommitSHA, Author: pr.Author,
		}, coResolved, pr.Author)

		// Freeze check.
		inFreeze := false
		freezeName := ""
		if name, active, ferr := freezeActiveFor(freezeCfg, s.Ref(), now); ferr == nil && active {
			inFreeze = true
			freezeName = name
		}
		_ = freezeName

		// Run policy hooks against the stack's current summary.
		redactor := BuildRedactor(in.Shared)
		hooks := HooksFromEngine(in.Config)
		policyPassed, policyResults, _ := RunPolicyForStack(ctx, hooks, s, ss, redactor)
		var policyPtr *bool
		if len(hooks) > 0 {
			policyPtr = &policyPassed
		}
		if len(policyResults) > 0 {
			// Attach policy rendering into the PR comment via FullPlan
			// suffix — renderer will collapse it with other output.
			ss.FullPlan = ss.FullPlan + policyRender(policyResults)
		}

		// Look up the prior preview manifest from blob for this SHA + stack.
		prev, lookupErr := FindPreviewForStack(ctx, in.Blob, in.PRNumber, in.CommitSHA, s.Ref())
		if lookupErr != nil {
			// Not fatal — treat as "no preview" so the gate fails cleanly.
			prev = PreviewStatus{}
		}

		// Evaluate preconditions.
		pcInputs := preconditions.Inputs{
			StackRef:           s.Ref(),
			PRIsFork:           pr.IsFork,
			ForkOptInAllowed:   in.Shared != nil && in.Shared.Apply.AllowForkPRs,
			UpToDate:           upToDate,
			CommitsBehind:      behind,
			ChecksGreen:        checksGreen,
			HasFreshPreview:    prev.Found,
			PreviewAge:         prev.Age,
			PreviewSucceeded:   prev.Succeeded,
			PolicyPassed:       policyPtr,
			ApprovalsSatisfied: approvalsRes.Satisfied,
			LockAcquirable:     acquired,
			LockBlockedByPR:    lockBlockedBy,
			InFreeze:           inFreeze,
		}
		pcResult := preconditions.Evaluate(preCfg, pcInputs)
		ss.Gates = gatesToTrace(pcResult)
		ss.BlockedBy = lockBlockedBy

		if pcResult.Blocked {
			if ss.Status == "" {
				ss.Status = summary.StatusBlocked
			}
			anyBlocked = true
			// If we acquired the lock but gates failed, release it.
			if acquired {
				_, _ = in.Locks.Release(ctx, s.Project, s.Name, in.PRNumber)
			}
			summaries = append(summaries, ss)
			continue
		}

		// Gates green — acquire auth creds and run apply.
		authEnv, aerr := ResolveAuthEnv(ctx, in.AuthConfig, in.AuthRegistry, s.Ref(), auth.ModeApply)
		if aerr != nil {
			ss.Status = summary.StatusError
			ss.Error = redactor.Redact(aerr.Error())
			_, _ = in.Locks.Release(ctx, s.Project, s.Name, in.PRNumber)
			summaries = append(summaries, ss)
			continue
		}
		for _, v := range authEnv {
			redactor.AddSecret(v)
		}
		stackCtx, endStack := in.OTEL.StartStackSpan(ctx, s.Project, s.Name, s.Env, "apply")
		stackStart := time.Now()
		res, aerr := in.Engine.Apply(stackCtx, s, iac.ApplyOpts{Cwd: absJoin(in.RepoRoot, s.Path), Env: authEnv})
		stackOutcome := "success"
		if aerr != nil {
			ss.Status = summary.StatusError
			ss.Error = redactor.Redact(aerr.Error())
			stackOutcome = "error"
			endStack(stackOutcome, time.Since(stackStart).Seconds())
			_, _ = in.Locks.Release(ctx, s.Project, s.Name, in.PRNumber)
			summaries = append(summaries, ss)
			PostAnnotation(ctx, in.Annotations, annotations.EventApplyFailed,
				s.Project, s.Name, s.Env, "failed", ss.Error, in.PRNumber, in.CommitSHA)
			continue
		}
		endStack(stackOutcome, time.Since(stackStart).Seconds())
		ss.Counts = res.Counts
		ss.DurationMS = res.DurationMS
		ss.FullPlan = redactor.Redact(res.Output)
		in.OTEL.RecordStackChanges(ctx, s.Project, s.Name, res.Counts.Add, res.Counts.Change, res.Counts.Delete, res.Counts.Replace)
		if res.Error != "" {
			ss.Status = summary.StatusError
			ss.Error = redactor.Redact(res.Error)
		} else if res.Counts.Total() == 0 {
			ss.Status = summary.StatusNoOp
		} else {
			ss.Status = summary.StatusReady
		}
		_, _ = in.Locks.Release(ctx, s.Project, s.Name, in.PRNumber)
		summaries = append(summaries, ss)
	}

	// 4. Render apply comment.
	sortMode := "status_grouped"
	if in.Shared != nil && in.Shared.Comments.Sort != "" {
		sortMode = in.Shared.Comments.Sort
	}
	dur := int(time.Since(start).Seconds())
	body := render.Apply(render.ApplyInput{
		RunNumber:   in.RunNumber,
		CommitSHA:   in.CommitSHA,
		DurationSec: dur,
		CIRunURL:    in.CIRunURL,
		Stacks:      summaries,
		SortMode:    sortMode,
	})

	// 5. Write run manifest.
	if err := writeApplyManifest(ctx, in.Blob, in.PRNumber, runID, summaries, in.CommitSHA); err != nil {
		return nil, fmt.Errorf("write manifest: %w", err)
	}

	// 6. Upsert PR comment.
	if in.VCS != nil && in.PRNumber > 0 {
		if err := in.VCS.UpsertComment(ctx, in.PRNumber, body, render.Marker); err != nil {
			return nil, fmt.Errorf("upsert pr comment: %w", err)
		}
	}

	// 7. Slack notification (runs last, captures everything above).
	if in.PRNumber > 0 && in.Notifications != nil {
		backend := BuildSlackBackend(in.Notifications, in.Blob)
		if err := NotifySlackApplied(ctx, backend, in.Notifications,
			in.PRNumber, in.CommitSHA, in.CIRunURL, "", pr.Author, summaries, anyBlocked); err != nil {
			fmt.Printf("slack notify: %v\n", err)
		}
	}

	// 8. Audit log.
	if in.AuditWriter != nil {
		entry := audit.Entry{
			RunID:      runID,
			Op:         "apply",
			StartedAt:  start.UTC().Format(time.RFC3339),
			FinishedAt: time.Now().UTC().Format(time.RFC3339),
			Actor:      in.Actor,
			PR:         in.PRNumber,
			CommitSHA:  in.CommitSHA,
			Repo:       in.RepoFull,
			Outcome:    aggregateOutcome(summaries, anyBlocked),
			Stacks:     toAuditStacks(summaries),
			DurationMS: time.Since(start).Milliseconds(),
		}
		if err := in.AuditWriter.Write(ctx, entry); err != nil && !errors.Is(err, blob.ErrPreconditionFailed) {
			return nil, fmt.Errorf("audit write: %w", err)
		}
	}

	outcome = aggregateOutcome(summaries, anyBlocked)
	runEvent := annotations.EventApplyCompleted
	if outcome == "failed" {
		runEvent = annotations.EventApplyFailed
	}
	PostAnnotation(ctx, in.Annotations, runEvent,
		"", "", "", outcome, "", in.PRNumber, in.CommitSHA)

	return &ApplyOutput{
		Stacks:      summaries,
		CommentBody: body,
		RunID:       runID,
		DurationSec: dur,
		Blocked:     anyBlocked,
	}, nil
}

func gatesToTrace(r preconditions.Result) []summary.GateTrace {
	out := make([]summary.GateTrace, 0, len(r.Gates))
	for _, g := range r.Gates {
		out = append(out, summary.GateTrace{
			Gate:    string(g.Gate),
			Outcome: string(g.Outcome),
			Reason:  g.Reason,
		})
	}
	return out
}

func writeApplyManifest(ctx context.Context, store blob.Store, pr int, runID string, stacks []summary.StackSummary, sha string) error {
	if store == nil {
		return nil
	}
	m := manifest{
		RunID: runID, PR: pr, CommitSHA: sha, Op: "apply",
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
	_, err = store.Put(ctx, key, bytes.NewReader(data))
	return err
}

func toAuditStacks(ss []summary.StackSummary) []audit.Stack {
	out := make([]audit.Stack, 0, len(ss))
	for _, s := range ss {
		out = append(out, audit.Stack{
			Ref:    s.Ref(),
			Env:    s.Env,
			Status: string(s.Status),
			Add:    s.Counts.Add, Change: s.Counts.Change,
			Delete: s.Counts.Delete, Replace: s.Counts.Replace,
			DurationMS: s.DurationMS,
			Error:      s.Error,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref < out[j].Ref })
	return out
}

func aggregateOutcome(ss []summary.StackSummary, anyBlocked bool) string {
	for _, s := range ss {
		if s.Status == summary.StatusError {
			return "failed"
		}
	}
	if anyBlocked {
		return "blocked"
	}
	return "success"
}
