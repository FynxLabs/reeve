package render

import (
	"fmt"
	"sort"
	"strings"

	"github.com/thefynx/reeve/internal/core/summary"
)

// ApplyInput mirrors PreviewInput but carries apply-specific data.
type ApplyInput struct {
	RunNumber   int
	CommitSHA   string
	DurationSec int
	CIRunURL    string
	Stacks      []summary.StackSummary
	SortMode    string
}

// Apply renders the apply comment markdown.
func Apply(in ApplyInput) string {
	var b strings.Builder
	b.WriteString(Marker)
	b.WriteString("\n")

	icon := overallIcon(in.Stacks)
	fmt.Fprintf(&b, "## %s reeve · apply · run #%d · [commit %s]\n\n", icon, in.RunNumber, shortSHA(in.CommitSHA))

	n := len(in.Stacks)
	noun := "stacks"
	if n == 1 {
		noun = "stack"
	}
	durBit := ""
	if in.DurationSec > 0 {
		durBit = fmt.Sprintf(" · ⏱ %ds", in.DurationSec)
	}
	runBit := ""
	if in.CIRunURL != "" {
		runBit = fmt.Sprintf(" · [View run](%s)", in.CIRunURL)
	}
	fmt.Fprintf(&b, "**%d %s applied**%s%s\n\n", n, noun, durBit, runBit)

	if n == 0 {
		b.WriteString("_No stacks applied._\n")
		return b.String()
	}

	// Table: failures first.
	b.WriteString("| Stack | Env | ➕ Add | 🔄 Change | ➖ Delete | 🔁 Replace | Duration | Status |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|\n")
	ordered := sortApply(in.Stacks, in.SortMode)
	for _, s := range ordered {
		dur := ""
		if s.DurationMS > 0 {
			dur = fmt.Sprintf("%ds", s.DurationMS/1000)
		}
		fmt.Fprintf(&b, "| %s | %s | %d | %d | %d | %d | %s | %s |\n",
			s.Ref(), envOrDash(s.Env),
			s.Counts.Add, s.Counts.Change, s.Counts.Delete, s.Counts.Replace,
			dur, applyStatusCell(s))
	}
	b.WriteString("\n")

	// Per-stack details, failures first.
	for _, s := range ordered {
		if s.Status == summary.StatusNoOp {
			continue
		}
		b.WriteString("---\n\n")
		fmt.Fprintf(&b, "### %s · %s · %s\n\n", s.Ref(), envOrDash(s.Env), applyStatusCell(s))
		if s.Error != "" {
			fmt.Fprintf(&b, "  **Error:** %s\n\n", s.Error)
		}
		if s.PlanSummary != "" {
			fmt.Fprintf(&b, "<details><summary>Summary (%d add, %d change, %d delete, %d replace)</summary>\n\n%s\n\n</details>\n\n",
				s.Counts.Add, s.Counts.Change, s.Counts.Delete, s.Counts.Replace,
				s.PlanSummary)
		}
		if s.FullPlan != "" {
			b.WriteString("<details><summary>Full apply output</summary>\n\n```\n")
			b.WriteString(s.FullPlan)
			if !strings.HasSuffix(s.FullPlan, "\n") {
				b.WriteString("\n")
			}
			b.WriteString("```\n\n</details>\n\n")
		}
	}

	return b.String()
}

func applyStatusCell(s summary.StackSummary) string {
	switch s.Status {
	case summary.StatusError:
		return "🔴 failed"
	case summary.StatusNoOp:
		return "· no-op"
	case summary.StatusBlocked:
		if s.BlockedBy > 0 {
			return fmt.Sprintf("🔒 blocked by #%d", s.BlockedBy)
		}
		return "🔒 blocked"
	case summary.StatusReady:
		return "✅ applied"
	}
	return string(s.Status)
}

// sortApply surfaces failures first, then applied, then blocked, then no-op.
func sortApply(ss []summary.StackSummary, mode string) []summary.StackSummary {
	out := make([]summary.StackSummary, len(ss))
	copy(out, ss)
	if mode == "alphabetical" {
		sort.Slice(out, func(i, j int) bool { return out[i].Ref() < out[j].Ref() })
		return out
	}
	rank := map[summary.Status]int{
		summary.StatusError:   0,
		summary.StatusBlocked: 1,
		summary.StatusReady:   2,
		summary.StatusNoOp:    3,
	}
	sort.SliceStable(out, func(i, j int) bool {
		if rank[out[i].Status] != rank[out[j].Status] {
			return rank[out[i].Status] < rank[out[j].Status]
		}
		return out[i].Ref() < out[j].Ref()
	})
	return out
}
