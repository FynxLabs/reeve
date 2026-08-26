package run

import (
	"log/slog"

	"github.com/reeveops/reeve/internal/core/render"
	"github.com/reeveops/reeve/internal/core/summary"
)

// logTrimmed writes whatever the size limit kept out of the PR comment to the
// run log.
//
// The comment's trim banner tells the reviewer to read the full run output for
// what is missing. Nothing else in the pipeline puts per-stack plan or engine
// output there - it otherwise only ever reaches the comment - so without this
// the banner names an output that does not hold what it promises.
//
// What is emitted follows what the renderer reports it dropped, so the two
// cannot drift: a new rung on the ladder that this does not cover shows up as a
// stack whose content is in neither the comment nor the log.
//
// Every summary is redacted where it is built, so this adds no path around
// internal/core/redact.
func logTrimmed(op string, stacks []summary.StackSummary, trim render.Trim) {
	// For apply and refresh the engine output IS the record of what changed, so
	// it must reach the log whenever it was dropped - even when it is the only
	// omission (Trim.Lost stays false for a raw-plan-only drop). For a preview
	// the raw plan blob alone is not worth hundreds of KB in every trimmed run's
	// log: the diff survived that rung and the reviewer lost nothing.
	fullPlanLogged := trim.DroppedFullPlan && op != "preview"
	if !trim.Lost() && !fullPlanLogged {
		return
	}

	slog.Info("PR comment trimmed to fit the size limit; dropped content follows",
		"op", op,
		"dropped_diff", trim.DroppedDiff,
		"dropped_summaries", trim.DroppedSummary,
		"clamped_errors", trim.ClampedErrors,
		"stacks_without_detail", trim.DroppedSections,
		"stacks_not_in_table", trim.DroppedRows,
	)

	for _, s := range stacks {
		// Error first: on a failed run it is what the reviewer came for, and it
		// is logged whenever it was clamped or its whole section went.
		if s.Error != "" && (trim.ClampedErrors || trim.DroppedSections > 0 || trim.DroppedRows > 0) {
			slog.Error("stack error omitted or shortened in the PR comment",
				"op", op, "stack", s.Ref(), "error", s.Error)
		}
		if s.PlanDiff != "" && trim.DroppedDiff {
			slog.Info("stack diff omitted from the PR comment",
				"op", op, "stack", s.Ref(), "diff", s.PlanDiff)
		}
		if s.PlanSummary != "" && trim.DroppedSummary {
			slog.Info("stack plan summary omitted from the PR comment",
				"op", op, "stack", s.Ref(), "summary", s.PlanSummary)
		}
		// FullPlan is the engine's own output. For an apply or refresh that is
		// the record of what changed, so it is logged whenever the comment lost
		// reviewer-visible content; for a preview it is the raw blob and the
		// diff above already carries what is readable.
		if s.FullPlan != "" && op != "preview" && trim.DroppedFullPlan {
			slog.Info("stack engine output omitted from the PR comment",
				"op", op, "stack", s.Ref(), "output", s.FullPlan)
		}
	}
}
