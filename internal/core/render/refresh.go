package render

import (
	"fmt"
	"strings"

	"github.com/reeveops/reeve/internal/core/summary"
)

// RefreshMarker identifies reeve's refresh PR comment slot. Refresh gets its
// own slot rather than reusing the apply comment: they answer different
// questions, and a refresh overwriting the apply record would erase the
// history of what actually shipped.
const RefreshMarker = "<!-- reeve:refresh:v1 -->"

// RefreshInput is what the refresh renderer consumes. Counts are reused
// from the summary tuple but MEAN something different here - see the header
// the renderer writes.
type RefreshInput struct {
	RunNumber   int
	CommitSHA   string
	DurationSec int
	CIRunURL    string
	// DryRun renders the read-only variant (`--dry-run`): the same counts,
	// described as what a refresh would reconcile rather than what it did.
	DryRun    bool
	Stacks    []summary.StackSummary
	SortMode  string
	StackView string
}

// Refresh renders the refresh comment markdown, guaranteed to fit GitHub's
// comment size limit. See trim.go for the ladder.
func Refresh(in RefreshInput) string {
	body, _ := RefreshTrimmed(in)
	return body
}

// RefreshTrimmed renders the refresh comment and reports what it dropped. As
// with apply, FullPlan here is the engine's own refresh output, so dropping it
// is named in the comment and must be logged by the caller.
func RefreshTrimmed(in RefreshInput) (string, Trim) {
	return descend(
		func(o renderOpts) string { return renderRefresh(in, o) },
		func(omitted string) string {
			return truncationNote(PreviewInput{CIRunURL: in.CIRunURL}) + " (omitted: " + omitted + ")"
		},
		sortApply, // sections render failures-first, same as the table
		in.Stacks, in.StackView, in.SortMode,
		"full refresh output",
		false, // this is the engine's output; never drop it silently
	)
}

func renderRefresh(in RefreshInput, opts renderOpts) string {
	var b strings.Builder
	b.WriteString(RefreshMarker + "\n")

	verb := "refresh"
	if in.DryRun {
		verb = "refresh (dry run)"
	}
	fmt.Fprintf(&b, "## %s reeve · %s · %s · [commit %s]\n\n",
		overallIcon(in.Stacks), verb, runRef(in.RunNumber, in.CIRunURL), shortSHA(in.CommitSHA))

	// The single most important line in this comment: readers pattern-match
	// these tables from apply comments, where "delete 3" means reeve
	// destroyed three resources. Here it never does.
	if in.DryRun {
		b.WriteString("> [!NOTE]\n> Read-only. Counts are what a refresh **would** reconcile between engine state and live infrastructure. No state was written and no infrastructure was touched.\n\n")
	} else {
		b.WriteString("> [!NOTE]\n> Counts are **state reconciliation**, not infrastructure change. A delete here means the resource was already gone from the provider and has been dropped from state — reeve destroyed nothing.\n\n")
	}

	durBit := ""
	if in.DurationSec > 0 {
		durBit = fmt.Sprintf(" · ⏱ %ds", in.DurationSec)
	}
	n := len(in.Stacks)
	noun := "stacks"
	if n == 1 {
		noun = "stack"
	}
	fmt.Fprintf(&b, "**%d %s refreshed**%s\n\n", n, noun, durBit)

	if opts.truncationNote != "" {
		fmt.Fprintf(&b, "> ⚠️ %s\n\n", opts.truncationNote)
	}
	if n == 0 {
		b.WriteString("_No stacks refreshed._\n")
		return b.String()
	}

	rows, hidden := tableRows(sortApply, in.Stacks, in.StackView, in.SortMode, opts.tableLimit)
	b.WriteString("| Stack | Env | ➕ Added to state | 🔄 Updated | ➖ Dropped | 🔁 Replaced | Duration | Status |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|\n")
	for _, s := range rows {
		dur := ""
		if s.DurationMS > 0 {
			dur = fmt.Sprintf("%ds", s.DurationMS/1000)
		}
		fmt.Fprintf(&b, "| %s | %s | %d | %d | %d | %d | %s | %s |\n",
			s.Ref(), envOrDash(s.Env),
			s.Counts.Add, s.Counts.Change, s.Counts.Delete, s.Counts.Replace,
			dur, applyStatusCell(s))
	}
	writeHiddenRowNote(&b, hidden, 8)
	b.WriteString("\n")

	dropped := 0
	for _, s := range sortApply(tableStacks(in.Stacks, StackViewAll), in.SortMode) {
		if s.Status == summary.StatusNoOp {
			continue
		}
		if !opts.sectionRendered(s.Ref()) {
			dropped++
			continue
		}
		b.WriteString("---\n\n")
		fmt.Fprintf(&b, "### %s · %s · %s\n\n", s.Ref(), envOrDash(s.Env), applyStatusCell(s))
		if s.Status == summary.StatusBlocked && len(s.Gates) > 0 {
			for _, g := range s.Gates {
				if g.Outcome == "fail" {
					fmt.Fprintf(&b, "**Blocked:** %s (`%s`)\n\n", g.Reason, g.Gate)
					break
				}
			}
		}
		writeError(&b, opts.clampError(s.Error))
		if s.PlanSummary != "" && opts.includeSummary {
			fmt.Fprintf(&b, "<details><summary>Reconciled resources</summary>\n\n%s\n\n</details>\n\n", s.PlanSummary)
		}
		if s.FullPlan != "" && opts.includeFullPlan {
			b.WriteString("<details><summary>Full refresh output</summary>\n\n```\n")
			b.WriteString(s.FullPlan)
			if !strings.HasSuffix(s.FullPlan, "\n") {
				b.WriteString("\n")
			}
			b.WriteString("```\n\n</details>\n\n")
		}
	}
	writeDroppedSectionNote(&b, dropped)
	return b.String()
}
