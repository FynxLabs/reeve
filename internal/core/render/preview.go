// Package render builds the PR comment markdown. Pure string-in / string-out.
// Identified by a hidden HTML marker so the VCS adapter can upsert the
// same comment across runs.
package render

import (
	"fmt"
	"sort"
	"strings"

	"github.com/thefynx/reeve/internal/core/summary"
)

// Marker is the hidden HTML comment that identifies reeve's PR comment.
// The VCS adapter uses it to find-or-create on UpsertComment.
const Marker = "<!-- reeve:pr-comment:v1 -->"

// PreviewInput is what the preview renderer consumes. Pure data — no
// imports beyond summary and stdlib.
type PreviewInput struct {
	Op          string // "preview" or "apply"
	RunNumber   int
	CommitSHA   string
	DurationSec int
	CIRunURL    string
	Stacks      []summary.StackSummary
	SortMode    string // "status_grouped" (default), "alphabetical"
}

// Preview returns the full comment body, marker included.
func Preview(in PreviewInput) string {
	var b strings.Builder
	b.WriteString(Marker)
	b.WriteString("\n")
	writeHeader(&b, in)
	writeTable(&b, in)
	writeSections(&b, in)
	return b.String()
}

func writeHeader(b *strings.Builder, in PreviewInput) {
	icon := overallIcon(in.Stacks)
	op := in.Op
	if op == "" {
		op = "preview"
	}
	fmt.Fprintf(b, "## %s reeve · %s · run #%d · [commit %s]\n\n", icon, op, in.RunNumber, shortSHA(in.CommitSHA))

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
	fmt.Fprintf(b, "**%d %s changed**%s%s\n\n", n, noun, durBit, runBit)
}

func writeTable(b *strings.Builder, in PreviewInput) {
	if len(in.Stacks) == 0 {
		b.WriteString("_No stacks affected by this change._\n\n")
		return
	}
	b.WriteString("| Stack | Env | ➕ Add | 🔄 Change | ➖ Delete | 🔁 Replace | Status |\n")
	b.WriteString("|---|---|---|---|---|---|---|\n")
	ordered := sorted(in.Stacks, in.SortMode)
	anyReplace := false
	for _, s := range ordered {
		if s.Counts.Replace > 0 {
			anyReplace = true
		}
		fmt.Fprintf(b, "| %s | %s | %d | %d | %d | %d | %s |\n",
			s.Project+"/"+s.Stack, envOrDash(s.Env),
			s.Counts.Add, s.Counts.Change, s.Counts.Delete, s.Counts.Replace,
			statusCell(s))
	}
	b.WriteString("\n")
	if anyReplace {
		b.WriteString("⚠️ Replacements detected — review carefully.\n\n")
	}
}

func writeSections(b *strings.Builder, in PreviewInput) {
	ordered := sorted(in.Stacks, in.SortMode)
	for _, s := range ordered {
		if s.Status == summary.StatusNoOp {
			continue // no-ops collapse into the table line only
		}
		b.WriteString("---\n\n")
		fmt.Fprintf(b, "### %s · %s · %s\n\n", s.Ref(), envOrDash(s.Env), statusCell(s))
		if s.Status == summary.StatusBlocked && s.BlockedBy > 0 {
			fmt.Fprintf(b, "  Queued behind #%d.\n\n", s.BlockedBy)
		}
		if s.Error != "" {
			fmt.Fprintf(b, "  **Error:** %s\n\n", s.Error)
		}
		if s.PlanSummary != "" {
			fmt.Fprintf(b, "<details><summary>Summary (%d add, %d change, %d delete, %d replace)</summary>\n\n%s\n\n</details>\n\n",
				s.Counts.Add, s.Counts.Change, s.Counts.Delete, s.Counts.Replace,
				s.PlanSummary)
		}
		if s.FullPlan != "" {
			b.WriteString("<details><summary>Full plan output</summary>\n\n```\n")
			b.WriteString(s.FullPlan)
			if !strings.HasSuffix(s.FullPlan, "\n") {
				b.WriteString("\n")
			}
			b.WriteString("```\n\n</details>\n\n")
		}
		if len(s.Gates) > 0 {
			fmt.Fprintf(b, "🔐 %s apply gates:\n", s.Ref())
			for _, g := range s.Gates {
				fmt.Fprintf(b, "  %s %s: %s\n", gateIcon(g.Outcome), g.Gate, g.Reason)
			}
			b.WriteString("\n")
		}
	}
}

func gateIcon(outcome string) string {
	switch outcome {
	case "pass":
		return "✅"
	case "fail":
		return "❌"
	case "warn":
		return "⚠️"
	case "skipped":
		return "⏸"
	}
	return "·"
}

func overallIcon(ss []summary.StackSummary) string {
	errored, blocked, changed := false, false, false
	for _, s := range ss {
		switch s.Status {
		case summary.StatusError:
			errored = true
		case summary.StatusBlocked:
			blocked = true
		case summary.StatusReady:
			if s.Counts.Total() > 0 {
				changed = true
			}
		}
	}
	switch {
	case errored:
		return "🔴"
	case blocked:
		return "🟡"
	case changed:
		return "🟢"
	default:
		return "⚪"
	}
}

func statusCell(s summary.StackSummary) string {
	switch s.Status {
	case summary.StatusBlocked:
		if s.BlockedBy > 0 {
			return fmt.Sprintf("🔒 blocked by #%d", s.BlockedBy)
		}
		return "🔒 blocked"
	case summary.StatusError:
		return "🔴 error"
	case summary.StatusNoOp:
		return "· no-op"
	case summary.StatusReady:
		return "✅ ready"
	}
	return string(s.Status)
}

func envOrDash(env string) string {
	if env == "" {
		return "—"
	}
	return env
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// sorted returns a copy of ss in the requested order.
// status_grouped (default): blocked first, then ready, then no-op, then error last.
// alphabetical: by Ref().
func sorted(ss []summary.StackSummary, mode string) []summary.StackSummary {
	out := make([]summary.StackSummary, len(ss))
	copy(out, ss)
	switch mode {
	case "alphabetical":
		sort.Slice(out, func(i, j int) bool { return out[i].Ref() < out[j].Ref() })
	default: // status_grouped
		rank := map[summary.Status]int{
			summary.StatusBlocked: 0,
			summary.StatusReady:   1,
			summary.StatusError:   2,
			summary.StatusNoOp:    3,
		}
		sort.SliceStable(out, func(i, j int) bool {
			if rank[out[i].Status] != rank[out[j].Status] {
				return rank[out[i].Status] < rank[out[j].Status]
			}
			return out[i].Ref() < out[j].Ref()
		})
	}
	return out
}
