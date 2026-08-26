package run

import (
	"context"
	"log/slog"

	"github.com/reeveops/reeve/internal/config/schemas"
	"github.com/reeveops/reeve/internal/core/render"
	"github.com/reeveops/reeve/internal/core/summary"
)

// PartDeleter removes the board parts a shrinking run no longer writes. It is
// optional: a VCS adapter that cannot delete simply leaves stale parts, which is
// worse but not fatal, so the capability is asserted rather than required.
type PartDeleter interface {
	DeleteCommentsByMarkerPrefix(ctx context.Context, number int, prefix string, keepThrough int) (int, error)
}

// commentUpserter is the write side of a board: one call per part.
type commentUpserter interface {
	UpsertComment(ctx context.Context, number int, body, marker string) error
}

// overflowFor reads comments.overflow, defaulting to the drop mode that keeps
// today's single-comment behavior.
func overflowFor(shared *schemas.Shared) render.OverflowConfig {
	if shared == nil {
		return render.OverflowConfig{}
	}
	o := shared.Comments.Overflow
	return render.OverflowConfig{Mode: o.Mode, Split: o.Split, MaxParts: o.MaxParts}
}

// postBoard writes a paginated board: every part upserted under its own marker,
// then the parts a previous larger run left behind removed.
//
// Parts are written in order so a reader watching the PR sees part 1 - which
// holds the table - first.
func postBoard(ctx context.Context, vcs commentUpserter, pr int, op string,
	parts []render.Part, boardMarker string, stacks []summary.StackSummary,
) error {
	var firstErr error
	for _, p := range parts {
		if p.Trim.Lost() || (p.Trim.DroppedFullPlan && op != "preview") {
			logTrimmed(op, stacksByRef(stacks, p.Stacks), p.Trim)
		}
		logOmittedPartStacks(op, stacksByRef(stacks, p.OmittedStacks))
		if err := vcs.UpsertComment(ctx, pr, p.Body, p.Marker); err != nil {
			// Keep going: a failed part must not cost the parts after it, and
			// the run still reports the first failure.
			slog.Error("board part comment failed", "op", op, "pr", pr,
				"part", p.Ordinal, "of", len(parts), "err", err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
	}

	deleteStaleParts(ctx, vcs, pr, op, boardMarker, len(parts))
	return firstErr
}

// postSingleBoard updates part 1 and removes continuations left by a previous
// larger run. Without this path, shrinking all the way to one comment never
// enters postBoard and stale parts remain forever.
func postSingleBoard(ctx context.Context, vcs commentUpserter, pr int, op, body, marker string) error {
	if err := vcs.UpsertComment(ctx, pr, body, marker); err != nil {
		return err
	}
	deleteStaleParts(ctx, vcs, pr, op, marker, 1)
	return nil
}

// postAppendBoard creates every part of an append-style board without editing
// or deleting comments from earlier runs.
func postAppendBoard(ctx context.Context, vcs commentPoster, pr int, op string,
	parts []render.Part, stacks []summary.StackSummary,
) error {
	var firstErr error
	for _, p := range parts {
		if p.Trim.Lost() || (p.Trim.DroppedFullPlan && op != "preview") {
			logTrimmed(op, stacksByRef(stacks, p.Stacks), p.Trim)
		}
		logOmittedPartStacks(op, stacksByRef(stacks, p.OmittedStacks))
		if err := vcs.PostComment(ctx, pr, p.Body); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
	}
	return firstErr
}

func logOmittedPartStacks(op string, stacks []summary.StackSummary) {
	if len(stacks) == 0 {
		return
	}
	logTrimmed(op, stacks, render.Trim{
		DroppedFullPlan: true,
		DroppedDiff:     true,
		DroppedSummary:  true,
		ClampedErrors:   true,
		DroppedSections: len(stacks),
	})
}

// deleteStaleParts removes the parts above the current count. A run that shrank
// from four parts to two leaves parts 3 and 4 on the PR claiming stacks it no
// longer has, and nothing else would ever touch them.
//
// Failure is logged, never propagated: the run's real work has already shipped,
// and a stale comment is a reporting defect rather than a reason to fail.
func deleteStaleParts(ctx context.Context, vcs commentUpserter, pr int, op, boardMarker string, keep int) {
	d, ok := vcs.(PartDeleter)
	if !ok {
		return // adapter cannot delete; stale parts are left in place
	}
	n, err := d.DeleteCommentsByMarkerPrefix(ctx, pr, render.PartMarkerPrefix(boardMarker), keep)
	if err != nil {
		slog.Warn("could not remove stale board parts; the PR may show a part from a larger previous run",
			"op", op, "pr", pr, "removed", n, "err", err)
		return
	}
	if n > 0 {
		slog.Info("removed stale board parts", "op", op, "pr", pr, "removed", n)
	}
}

// stacksByRef selects the summaries a part carried, so the log records that
// part's own stacks rather than the whole run's.
func stacksByRef(all []summary.StackSummary, refs []string) []summary.StackSummary {
	want := make(map[string]bool, len(refs))
	for _, r := range refs {
		want[r] = true
	}
	out := make([]summary.StackSummary, 0, len(refs))
	for _, s := range all {
		if want[s.Ref()] {
			out = append(out, s)
		}
	}
	return out
}
