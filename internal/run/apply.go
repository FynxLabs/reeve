package run

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
	"github.com/thefynx/reeve/internal/notify"
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
	ChecksGreen(ctx context.Context, sha string, opts vcs.ChecksGreenOpts) (bool, []string, error)
	CompareBranches(ctx context.Context, base, head string) (int, error)
	// approvals
	approvals.Source
	// CODEOWNERS
	FetchCodeowners(ctx context.Context) (string, error)
	// team expansion (called by core/approvals to resolve org/team rules)
	ListTeamMembers(ctx context.Context, slug string) ([]string, error)
}

// ApplyInput wires dependencies and run context.
type ApplyInput struct {
	PRNumber  int
	CommitSHA string // best-effort; overridden from PR HEAD post-GetPR
	RunNumber int
	CIRunID   int64
	CIRunURL  string
	// SelfCheckNames is the list of check_run names that belong to reeve
	// itself and must be skipped when computing ChecksGreen (otherwise a
	// previously failed apply pins the gate red on the same SHA forever).
	// Typically populated from $GITHUB_WORKFLOW + $GITHUB_JOB.
	SelfCheckNames []string
	RepoRoot       string
	RepoFull       string // "owner/name" for audit log
	Actor          string
	Engine         applyEngine
	Config         *schemas.Engine
	Shared         *schemas.Shared
	AuthConfig     *schemas.Auth
	AuthRegistry   *auth.Registry
	Notifications  *schemas.Notifications
	Blob           blob.Store
	Locks          *blocks.Store
	VCS            applyVCS
	AuditWriter    *audit.Writer
	OTEL           *reeveotel.Provider
	Annotations    []annotations.Emitter
	// Force re-applies even when this commit is already recorded as applied,
	// bypassing the already-applied guard.
	Force bool
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

	timeline := newApplyTimeline(in.VCS, in.PRNumber, runID, in.RunNumber, in.CommitSHA, in.CIRunURL)
	timeline.add(ctx, "🚀", "apply starting", "")

	// Already-applied guard: if this exact commit was fully applied before and
	// the caller didn't pass --force, there is nothing new to ship. Record it
	// on the timeline and exit success rather than re-running side effects.
	if !in.Force {
		if prior, _ := readAppliedState(ctx, in.Blob, in.PRNumber, in.CommitSHA); prior != nil {
			detail := fmt.Sprintf("commit %s was already applied on run #%d (%s). Comment `/reeve apply --force` to apply again.",
				shortSHA(in.CommitSHA), prior.RunNumber, prior.AppliedAt)
			timeline.add(ctx, "⏭️", "skipped", detail)
			slog.Info("apply skipped: already applied", "sha", in.CommitSHA, "prior_run", prior.RunNumber)
			return &ApplyOutput{RunID: runID, DurationSec: int(time.Since(start).Seconds())}, nil
		}
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
	slog.Debug("changed files", "count", len(changed), "files", changed)
	cm := changeMappingFromConfig(in.Config)
	mapRes := discovery.AffectedDetailed(declared, changed, cm)
	target := mapRes.Stacks
	slog.Debug("apply target stacks", "count", len(target), "reason", mapRes.Reason)
	for _, s := range target {
		slog.Debug("target stack", "ref", s.Ref(), "path", s.Path)
	}

	// Docs/asset-only change: nothing to apply. Record on the timeline and exit.
	if mapRes.Reason == discovery.ReasonDocsOnly {
		timeline.add(ctx, "⏭️", "skipped", "documentation/asset-only changes — no Pulumi stacks affected")
		slog.Info("apply skipped: docs-only changes")
		return &ApplyOutput{RunID: runID, DurationSec: int(time.Since(start).Seconds())}, nil
	}
	if mapRes.Reason == discovery.ReasonBroadened {
		timeline.add(ctx, "📡", "scope broadened", "changed files map to no specific stack; applying all stacks")
	}

	// 2. Per-stack context: PR + checks + upstream-commits + approvals + CODEOWNERS.
	pr, err := in.VCS.GetPR(ctx, in.PRNumber)
	if err != nil {
		return nil, fmt.Errorf("get pr: %w", err)
	}
	slog.Debug("pr fetched", "number", in.PRNumber, "head_sha", pr.HeadSHA, "author", pr.Author, "base_ref", pr.BaseRef, "is_draft", pr.IsDraft, "is_fork", pr.IsFork)

	checksOpts := vcs.ChecksGreenOpts{
		IgnoreRunID: in.CIRunID,
		IgnoreNames: in.SelfCheckNames,
	}
	checksGreen, failingChecks, err := in.VCS.ChecksGreen(ctx, in.CommitSHA, checksOpts)
	if err != nil {
		// Fail-closed: an API outage must not silently pass the gate. The
		// gate-evaluation error is propagated so the caller can distinguish
		// "checks failed" from "checks could not be evaluated".
		return nil, fmt.Errorf("evaluate checks_green: %w", err)
	}
	if !checksGreen {
		slog.Info("required checks not green", "failing", failingChecks, "sha", in.CommitSHA)
	}

	behind, err := in.VCS.CompareBranches(ctx, pr.BaseRef, in.CommitSHA)
	if err != nil {
		// Fail-closed: previously this defaulted to behind=0 which made the
		// up-to-date gate silently pass on VCS outage.
		return nil, fmt.Errorf("compare branches %s..%s: %w", pr.BaseRef, in.CommitSHA, err)
	}
	upToDate := behind == 0

	// Use pr.HeadSHA (from the VCS API) for approval matching so that
	// dismiss_on_new_commit compares against the actual PR HEAD, not the
	// SHA the CI runner happened to check out (which may be a merge commit).
	approvalHeadSHA := pr.HeadSHA
	if approvalHeadSHA == "" {
		approvalHeadSHA = in.CommitSHA
	}
	rawApprovals, err := in.VCS.ListApprovals(ctx, approvals.PR{
		Number: in.PRNumber, HeadSHA: approvalHeadSHA, Author: pr.Author, Changed: changed,
	})
	if err != nil {
		// Fail-closed: previously the error was swallowed and rawApprovals
		// was nil, which made the approvals gate fail with "no approvals"
		// instead of surfacing the underlying VCS error.
		return nil, fmt.Errorf("list approvals: %w", err)
	}
	slog.Debug("raw approvals fetched", "count", len(rawApprovals), "pr_head_sha", in.CommitSHA)
	for _, a := range rawApprovals {
		slog.Debug("raw approval", "approver", a.Approver, "commit_sha", a.CommitSHA, "source", a.Source)
	}

	// CODEOWNERS (optional). A 404 returns "" with nil error; only a real
	// transport error reaches here, and that must not silently pass the
	// codeowners gate.
	coContent, err := in.VCS.FetchCodeowners(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch codeowners: %w", err)
	}
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

	// Pre-resolve team-slug membership once for every stack we're about to
	// process. Without this, a rule like `approvers: [my-org/sre]` would
	// only match if the literal string "my-org/sre" appeared in the
	// approvals - i.e. never. Expansion is per-org, so resolving once
	// outside the loop is correct and avoids hammering the VCS API.
	stackRules := make([]approvals.Rules, 0, len(target))
	for _, s := range target {
		stackRules = append(stackRules, approvals.Resolve(appCfg, s.Ref()))
	}
	// Also expand any teams referenced in CODEOWNERS so matchesOne can
	// resolve them. Without this, a path owned by @org/team that isn't in
	// any stack approval rule never gets expanded and the gate always fails.
	if len(coResolved) > 0 {
		coOwners := make(map[string]struct{})
		for _, owners := range coResolved {
			for _, o := range owners {
				coOwners[o] = struct{}{}
			}
		}
		var coApprovers []string
		for o := range coOwners {
			coApprovers = append(coApprovers, o)
		}
		stackRules = append(stackRules, approvals.Rules{Approvers: coApprovers})
	}
	teamMembers, err := approvals.ExpandTeams(ctx, in.VCS, stackRules...)
	if err != nil {
		// Partial expansion is fine - log slugs that failed but proceed.
		// The unresolved slugs fall back to literal-match (which never
		// fires), so the gate fails closed for them.
		slog.Warn("team expansion partial", "err", err)
	}
	for slug, members := range teamMembers {
		slog.Debug("team expanded", "slug", slug, "members", members)
	}
	if len(teamMembers) == 0 {
		slog.Debug("team expansion returned no members - approvals will use literal matching only")
	}

	now := time.Now()

	// Notify approved + applying before the loop (creates the message on
	// the apply trigger path).
	if in.PRNumber > 0 && in.Notifications != nil {
		sinks := BuildNotifySinks(ctx, in.Notifications, in.Blob, in.VCS)
		preSummaries := make([]summary.StackSummary, 0, len(target))
		for _, s := range target {
			preSummaries = append(preSummaries, summary.StackSummary{
				Project: s.Project, Stack: s.Name, Env: s.Env,
			})
		}
		preInput := PRNotifyInput{
			PR: in.PRNumber, CommitSHA: in.CommitSHA, RunURL: in.CIRunURL,
			PRTitle: pr.Title, PRAuthor: pr.Author, Stacks: preSummaries,
		}
		if err := NotifyPREvent(ctx, sinks, notify.EventApproved, preInput); err != nil {
			slog.Warn("notify approved failed", "err", err, "pr", in.PRNumber)
		}
		if err := NotifyPREvent(ctx, sinks, notify.EventApplying, preInput); err != nil {
			slog.Warn("notify applying failed", "err", err, "pr", in.PRNumber)
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

		// Resolve rules + approvals for this stack. TeamMembers is the
		// pre-resolved expansion shared across all stacks in this run.
		rules := approvals.Resolve(appCfg, s.Ref())
		rules.TeamMembers = teamMembers
		approvalsRes := approvals.Evaluate(rules, rawApprovals, approvals.PR{
			Number: in.PRNumber, HeadSHA: approvalHeadSHA, Author: pr.Author,
		}, coResolved, pr.Author)
		slog.Debug("approvals evaluated",
			"stack", s.Ref(),
			"satisfied", approvalsRes.Satisfied,
			"got", approvalsRes.Got,
			"needed", approvalsRes.TotalNeeded,
			"missing", approvalsRes.Missing,
			"trace", approvalsRes.Trace,
		)

		// Freeze check. The window name flows into preconditions.Inputs so
		// the gate's failure reason can identify which window blocked.
		inFreeze := false
		freezeName := ""
		if name, active, ferr := freezeActiveFor(freezeCfg, s.Ref(), now); ferr != nil {
			// Fail closed: if a window covering this stack can't be evaluated
			// (e.g. a bad cron expression), block rather than silently apply
			// through a freeze we couldn't check. Only the stacks that window
			// covers are affected.
			slog.Warn("freeze evaluation failed; blocking stack", "stack", s.Ref(), "err", ferr)
			inFreeze = true
			freezeName = "freeze evaluation failed: " + ferr.Error()
		} else if active {
			inFreeze = true
			freezeName = name
		}

		// Look up the prior preview manifest from blob for this SHA + stack.
		prev, lookupErr := FindPreviewForStack(ctx, in.Blob, in.PRNumber, in.CommitSHA, s.Ref())
		if lookupErr != nil {
			// Not fatal - treat as "no preview" so the gate fails cleanly.
			slog.Debug("preview lookup failed", "stack", s.Ref(), "sha", in.CommitSHA, "err", lookupErr)
			prev = PreviewStatus{}
		}
		slog.Debug("preview status", "stack", s.Ref(), "found", prev.Found, "succeeded", prev.Succeeded, "age", prev.Age)

		// Run policy hooks against the PREVIEW plan - the plan that will be
		// applied - not the still-empty pre-apply summary (which has no
		// counts or plan body yet, so any plan-content rule always passed).
		redactor := BuildRedactor(in.Shared)
		hooks := HooksFromEngine(in.Config)
		var policyPtr *bool
		if len(hooks) > 0 {
			policyTarget := ss
			if prev.Plan != nil {
				policyTarget = *prev.Plan
			}
			policyPassed, policyResults, policyErr := RunPolicyForStack(ctx, hooks, s, policyTarget, redactor)
			// Fail closed: if hooks are configured but there is no preview
			// plan to evaluate, or a hook failed to execute, the gate must
			// not pass. Applying with policy unevaluated defeats the gate.
			if prev.Plan == nil || policyErr != nil {
				if policyErr != nil {
					slog.Warn("policy execution failed", "stack", s.Ref(), "err", policyErr)
				} else {
					slog.Warn("policy skipped: no preview plan to evaluate", "stack", s.Ref())
				}
				policyPassed = false
			}
			policyPtr = &policyPassed
			if len(policyResults) > 0 {
				// Attach policy rendering into the PR comment via FullPlan
				// suffix - renderer will collapse it with other output.
				ss.FullPlan = ss.FullPlan + policyRender(policyResults)
			}
		}

		// Evaluate preconditions.
		pcInputs := preconditions.Inputs{
			StackRef:           s.Ref(),
			PRIsFork:           pr.IsFork,
			PRIsDraft:          pr.IsDraft,
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
			FreezeName:         freezeName,
		}
		pcResult := preconditions.Evaluate(preCfg, pcInputs)
		ss.Gates = gatesToTrace(pcResult)
		ss.BlockedBy = lockBlockedBy
		slog.Debug("preconditions evaluated", "stack", s.Ref(), "blocked", pcResult.Blocked, "gates", ss.Gates)

		if pcResult.Blocked {
			if ss.Status == "" {
				ss.Status = summary.StatusBlocked
			}
			anyBlocked = true
			if acquired {
				releaseLockOrLog(ctx, in.Locks, s.Project, s.Name, in.PRNumber, "gates blocked")
			}
			summaries = append(summaries, ss)
			continue
		}

		// Gates green - acquire auth creds and run apply. authCleanup must
		// run before the loop iteration ends so on-disk credential
		// artefacts (e.g. GCP WIF token files) do not outlive their use.
		authEnv, authCleanup, aerr := ResolveAuthEnv(ctx, in.AuthConfig, in.AuthRegistry, s.Ref(), auth.ModeApply)
		if aerr != nil {
			ss.Status = summary.StatusError
			ss.Error = redactor.Redact(aerr.Error())
			releaseLockOrLog(ctx, in.Locks, s.Project, s.Name, in.PRNumber, "auth resolve failed")
			summaries = append(summaries, ss)
			continue
		}
		for _, v := range authEnv {
			redactor.AddSecret(v)
		}
		stackCtx, endStack := in.OTEL.StartStackSpan(ctx, s.Project, s.Name, s.Env, "apply")
		stackStart := time.Now()
		res, aerr := in.Engine.Apply(stackCtx, s, iac.ApplyOpts{Cwd: absJoin(in.RepoRoot, s.Path), Env: authEnv})
		authCleanup()
		stackOutcome := "success"
		if aerr != nil {
			ss.Status = summary.StatusError
			ss.Error = redactor.Redact(aerr.Error())
			stackOutcome = "error"
			endStack(stackOutcome, time.Since(stackStart).Seconds())
			releaseLockOrLog(ctx, in.Locks, s.Project, s.Name, in.PRNumber, "engine apply failed")
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
			ss.Status = summary.StatusPlanned
		}
		releaseLockOrLog(ctx, in.Locks, s.Project, s.Name, in.PRNumber, "stack apply complete")
		summaries = append(summaries, ss)
	}

	// 4. Render apply comment.
	sortMode := "status_grouped"
	if in.Shared != nil && in.Shared.Comments.Sort != "" {
		sortMode = in.Shared.Comments.Sort
	}
	commentStyle := "replace"
	if in.Shared != nil && in.Shared.Comments.Style != "" {
		commentStyle = in.Shared.Comments.Style
	}
	dur := int(time.Since(start).Seconds())
	body := render.Apply(render.ApplyInput{
		RunNumber:   in.RunNumber,
		CommitSHA:   in.CommitSHA,
		DurationSec: dur,
		CIRunURL:    in.CIRunURL,
		Stacks:      summaries,
		SortMode:    sortMode,
		Style:       commentStyle,
		StackView:   stackView(in.Shared),
	})

	// 5. Write run manifest.
	if err := writeApplyManifest(ctx, in.Blob, in.PRNumber, runID, summaries, in.CommitSHA); err != nil {
		return nil, fmt.Errorf("write manifest: %w", err)
	}

	// 5b. Timeline terminal event + applied-state pointer. Only a fully clean
	// run (no failures, nothing blocked) records the applied marker, so a
	// blocked/failed run can be safely re-run without tripping the guard.
	finalOutcome := aggregateOutcome(summaries, anyBlocked)
	switch finalOutcome {
	case "failed":
		timeline.add(ctx, "🔴", "failed", failedStacksDetail(summaries))
	case "blocked":
		timeline.add(ctx, "🔒", "blocked", blockedStacksDetail(summaries))
	default:
		timeline.add(ctx, "✅", "applied", changedStacksDetail(summaries))
		if err := writeAppliedState(ctx, in.Blob, AppliedState{
			CommitSHA: in.CommitSHA, RunID: runID, RunNumber: in.RunNumber,
			AppliedAt: time.Now().UTC().Format(time.RFC3339), PR: in.PRNumber,
		}); err != nil {
			// Non-fatal: the apply shipped; a missing pointer only means a
			// future re-run won't be auto-skipped.
			slog.Warn("write applied-state failed", "err", err, "sha", in.CommitSHA)
		}
	}

	// 6. Post PR comment.
	if in.VCS != nil && in.PRNumber > 0 {
		var cerr error
		switch commentStyle {
		case "append":
			cerr = in.VCS.PostComment(ctx, in.PRNumber, body)
		case "section":
			cerr = in.VCS.UpsertComment(ctx, in.PRNumber, body, render.ApplyMarker)
		default:
			cerr = in.VCS.UpsertComment(ctx, in.PRNumber, body, render.Marker)
		}
		if cerr != nil {
			return nil, fmt.Errorf("post pr comment: %w", cerr)
		}
	}

	// 7. Notifications (run last, capture everything above). The apply has
	// already shipped at this point; a notification failure must not abort
	// the run.
	if in.PRNumber > 0 && in.Notifications != nil {
		sinks := BuildNotifySinks(ctx, in.Notifications, in.Blob, in.VCS)
		ev := ApplyOutcomeEvent(summaries, anyBlocked)
		if err := NotifyPREvent(ctx, sinks, ev, PRNotifyInput{
			PR: in.PRNumber, CommitSHA: in.CommitSHA, RunURL: in.CIRunURL,
			PRTitle: pr.Title, PRAuthor: pr.Author, Stacks: summaries,
		}); err != nil {
			slog.Warn("notify applied failed", "err", err, "pr", in.PRNumber, "event", ev)
		}
	}

	// 8. Audit log. Side effects (apply, PR comment, Slack) have already
	// shipped, so a write failure is logged loudly but not propagated -
	// returning an error here would falsely tell the caller the run failed.
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
			slog.Error("audit write failed - run already shipped",
				"err", err, "run_id", runID, "pr", in.PRNumber)
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

// releaseLockOrLog releases a per-stack lock and logs a warning on failure
// instead of swallowing the error. The lock-release path needs to be
// best-effort (the work has already happened or been declined), but a silent
// drop hid leaks until the reaper noticed - operators need a signal.
func releaseLockOrLog(ctx context.Context, store *blocks.Store, project, stack string, pr int, reason string) {
	if store == nil {
		return
	}
	if _, err := store.Release(ctx, project, stack, pr); err != nil {
		slog.Warn("lock release failed",
			"project", project, "stack", stack, "pr", pr, "reason", reason, "err", err)
	}
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
