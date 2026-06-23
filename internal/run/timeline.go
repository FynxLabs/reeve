package run

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/thefynx/reeve/internal/core/render"
	"github.com/thefynx/reeve/internal/core/summary"
)

// applyTimeline accumulates a single apply run's timeline and upserts it to
// ONE PR comment (pinned by a per-run marker), editing in place as each event
// lands so the comment reads as a chronological log:
//
//	🚀 apply starting
//	✅ applied: 3 stacks
//
// It is best-effort: comment failures are logged, never fatal, because the
// apply itself must not hinge on PR comment delivery.
type applyTimeline struct {
	vcs     commentPoster
	pr      int
	marker  string
	in      render.TimelineInput
	enabled bool
}

func newApplyTimeline(vcs commentPoster, pr int, runID string, runNumber int, sha, ciRunURL string) *applyTimeline {
	return &applyTimeline{
		vcs:     vcs,
		pr:      pr,
		marker:  render.ApplyTimelineMarker(runID),
		enabled: vcs != nil && pr > 0,
		in: render.TimelineInput{
			RunID:     runID,
			RunNumber: runNumber,
			CommitSHA: sha,
			CIRunURL:  ciRunURL,
		},
	}
}

// add appends one event and re-upserts the comment.
func (t *applyTimeline) add(ctx context.Context, icon, label, detail string) {
	if t == nil || !t.enabled {
		return
	}
	t.in.Entries = append(t.in.Entries, render.TimelineEntry{Icon: icon, Label: label, Detail: detail})
	body := render.ApplyTimeline(t.in)
	if err := t.vcs.UpsertComment(ctx, t.pr, body, t.marker); err != nil {
		slog.Warn("apply timeline comment failed", "err", err, "pr", t.pr, "label", label)
	}
}

// changedStacksDetail summarizes which stacks actually applied changes.
func changedStacksDetail(ss []summary.StackSummary) string {
	var refs []string
	for _, s := range ss {
		if s.Status == summary.StatusPlanned {
			refs = append(refs, s.Ref())
		}
	}
	if len(refs) == 0 {
		return "no changes"
	}
	return fmt.Sprintf("%d stack(s): %s", len(refs), strings.Join(refs, ", "))
}

func failedStacksDetail(ss []summary.StackSummary) string {
	var refs []string
	for _, s := range ss {
		if s.Status == summary.StatusError {
			refs = append(refs, s.Ref())
		}
	}
	return strings.Join(refs, ", ")
}

func blockedStacksDetail(ss []summary.StackSummary) string {
	var parts []string
	for _, s := range ss {
		if s.Status != summary.StatusBlocked {
			continue
		}
		reason := ""
		for _, g := range s.Gates {
			if g.Outcome == "fail" {
				reason = g.Reason
				break
			}
		}
		if reason != "" {
			parts = append(parts, fmt.Sprintf("%s (%s)", s.Ref(), reason))
		} else {
			parts = append(parts, s.Ref())
		}
	}
	return strings.Join(parts, ", ")
}
