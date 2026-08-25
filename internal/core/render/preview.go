// Package render builds the PR comment markdown. Pure string-in / string-out.
// Identified by a hidden HTML marker so the VCS adapter can upsert the
// same comment across runs.
package render

import (
	"fmt"
	"sort"
	"strings"

	"github.com/reeveops/reeve/internal/core/summary"
)

// Marker is the hidden HTML comment that identifies reeve's PR comment.
// The VCS adapter uses it to find-or-create on UpsertComment.
const Marker = "<!-- reeve:pr-comment:v1 -->"

// Style values for comments.style.
const (
	StyleReplace = "replace" // default: one board per PR, edited in place
	StyleSection = "section" // one board per commit SHA
	StyleAppend  = "append"  // a new comment every run
)

// DashboardMarker is the marker the dashboard comment is upserted under.
//
// Under `section` the board is keyed to the commit, so preview, a re-preview,
// and the apply of one SHA all edit one comment while a new SHA mints a new
// one. That is what keeps a plan readable: the previous commit's board is never
// written again. Keying on the operation instead - the original `section` - made
// exactly two boards for the life of the PR and overwrote both on every run.
//
// Under `replace` the marker stays byte-identical to Marker. A PR already
// running under it must keep having its board edited, not orphaned.
//
// Preview and apply MUST derive their marker from here rather than each
// building one, or the two operations drift onto different comments for the
// same commit.
func DashboardMarker(style, commitSHA string) string {
	if style != StyleSection {
		return Marker
	}
	return fmt.Sprintf("<!-- reeve:pr-comment:v1:%s -->", shortSHA(commitSHA))
}

// githubCommentMaxLen is GitHub's hard limit on issue/PR comment body
// length. The 422 error returned past this is non-recoverable, so the
// renderer must guarantee the body never exceeds it. We target a small
// safety margin so any wrapper formatting (e.g. quote-reply prefixes some
// VCS adapters might prepend) doesn't push us over.
const githubCommentMaxLen = 65_000

// PreviewInput is what the preview renderer consumes. Pure data - no
// imports beyond summary and stdlib.
type PreviewInput struct {
	Op          string // "preview" or "apply"
	RunNumber   int
	CommitSHA   string
	DurationSec int
	CIRunURL    string
	Stacks      []summary.StackSummary
	SortMode    string // "status_grouped" (default), "alphabetical"
	StackView   string // "all" (default) lists every stack; "changed" hides no-ops
	Notice      string // optional info banner (e.g. "already applied"); rendered above the table
	// Style is comments.style. It selects the marker the body opens with, so
	// the rendered comment and the upsert target cannot disagree.
	Style string
}

// StackView values for the comment table.
const (
	StackViewAll     = "all"     // default: list every declared stack, no-ops included
	StackViewChanged = "changed" // only list stacks with planned/applied changes
)

// tableStacks returns the stacks to render in the comment table, honoring the
// view mode. "changed" drops no-ops; anything else (including "" -> default)
// keeps every stack.
func tableStacks(stacks []summary.StackSummary, view string) []summary.StackSummary {
	if view != StackViewChanged {
		return stacks
	}
	out := make([]summary.StackSummary, 0, len(stacks))
	for _, s := range stacks {
		if s.Status == summary.StatusNoOp {
			continue
		}
		out = append(out, s)
	}
	return out
}

// renderOpts controls which per-stack sections the renderer emits. Used
// internally to progressively drop content when the body would exceed
// GitHub's comment-size limit.
// Preview returns the full comment body, marker included. If the body would
// exceed GitHub's hard comment-size limit it walks a trim ladder, dropping the
// least-read content first and reporting what went, so the note the comment
// carries always matches what is actually missing.
//
// The ladder never cuts the document mid-structure. An earlier version ended in
// a blind byte truncate, which sliced through tables, code fences, and
// <details> tags - GitHub rendered the remainder as garbage - and discarded
// whatever stacks fell past the cutoff with no account of them at all. The last
// rung now drops whole per-stack sections and says how many.
func Preview(in PreviewInput) string {
	body, _ := PreviewTrimmed(in)
	return body
}

// PreviewTrimmed renders the comment and reports what it dropped, so the caller
// can emit the dropped content where the trim note points. Preview wraps it for
// callers that do not care.
//
// FullPlan is dropped silently here: it is the raw `pulumi preview --json` blob,
// hundreds of KB per stack, and the diff that reviewers actually read survives
// that rung. Apply and refresh name it, because for them it is the engine's own
// output.
func PreviewTrimmed(in PreviewInput) (string, Trim) {
	return descend(
		func(o renderOpts) string { return renderPreview(in, o) },
		func(omitted string) string { return truncationNote(in) + " (omitted: " + omitted + ")" },
		in.Stacks, in.StackView, in.SortMode,
		"full plan output",
		true, // silent: the diff survives this rung
	)
}

// renderPreview builds the body honoring per-section opts. Pure
// string-builder; called multiple times by Preview when shrinking.
func renderPreview(in PreviewInput, opts renderOpts) string {
	var b strings.Builder
	b.WriteString(DashboardMarker(in.Style, in.CommitSHA))
	b.WriteString("\n")
	writeHeader(&b, in)
	if opts.truncationNote != "" {
		fmt.Fprintf(&b, "> ⚠️ %s\n\n", opts.truncationNote)
	}
	if in.Notice != "" {
		fmt.Fprintf(&b, "> ℹ️ %s\n\n", in.Notice)
	}
	writeTable(&b, in, opts)
	writeSections(&b, in, opts)
	return b.String()
}

func truncationNote(in PreviewInput) string {
	note := "Output trimmed to fit GitHub's 65,536-char comment limit."
	if in.CIRunURL != "" {
		note += fmt.Sprintf(" See the [full run output](%s) for the complete plan.", in.CIRunURL)
	}
	return note
}

func writeHeader(b *strings.Builder, in PreviewInput) {
	icon := overallIcon(in.Stacks)
	op := in.Op
	if op == "" {
		op = "preview"
	}
	fmt.Fprintf(b, "## %s reeve · %s · %s · [commit %s]\n\n", icon, op, runRef(in.RunNumber, in.CIRunURL), shortSHA(in.CommitSHA))

	// "X stacks changed" counts only stacks that actually have planned/applied
	// changes -- no-op stacks appear in the per-stack table for completeness
	// but they didn't change anything, so summing them in the headline is
	// misleading (was reading "18 stacks changed" with 15 no-ops + 3 actual).
	n := 0
	for _, s := range in.Stacks {
		if s.Status != summary.StatusNoOp {
			n++
		}
	}
	noun := "stacks"
	if n == 1 {
		noun = "stack"
	}
	durBit := ""
	if in.DurationSec > 0 {
		durBit = fmt.Sprintf(" · ⏱ %ds", in.DurationSec)
	}
	fmt.Fprintf(b, "**%d %s changed**%s\n\n", n, noun, durBit)
}

func writeTable(b *strings.Builder, in PreviewInput, opts renderOpts) {
	if len(in.Stacks) == 0 {
		b.WriteString("_No stacks affected by this change._\n\n")
		return
	}
	rows, hidden := tableRows(in.Stacks, in.StackView, in.SortMode, opts.tableLimit)
	if len(rows) == 0 && hidden == 0 {
		b.WriteString("_No stacks with changes._\n\n")
		return
	}
	b.WriteString("| Stack | Env | ➕ Add | 🔄 Change | ➖ Delete | 🔁 Replace | Status |\n")
	b.WriteString("|---|---|---|---|---|---|---|\n")
	anyReplace := false
	for _, s := range rows {
		if s.Counts.Replace > 0 {
			anyReplace = true
		}
		fmt.Fprintf(b, "| %s | %s | %d | %d | %d | %d | %s |\n",
			s.Project+"/"+s.Stack, envOrDash(s.Env),
			s.Counts.Add, s.Counts.Change, s.Counts.Delete, s.Counts.Replace,
			statusCell(s))
	}
	writeHiddenRowNote(b, hidden)
	b.WriteString("\n")
	b.WriteString("<sub>Legend: `+` create · `~` update in place · `-` delete · `±` replace (delete & recreate)</sub>\n\n")
	if anyReplace {
		b.WriteString("⚠️ Replacements detected - review carefully.\n\n")
	}
}

func writeSections(b *strings.Builder, in PreviewInput, opts renderOpts) {
	ordered := sorted(in.Stacks, in.SortMode)
	dropped := 0
	for _, s := range ordered {
		if s.Status == summary.StatusNoOp {
			continue // no-ops collapse into the table line only
		}
		if !opts.sectionRendered(s.Ref()) {
			dropped++
			continue
		}
		b.WriteString("---\n\n")
		fmt.Fprintf(b, "### %s · %s · %s\n\n", s.Ref(), envOrDash(s.Env), statusCell(s))
		if s.Status == summary.StatusBlocked && s.BlockedBy > 0 {
			fmt.Fprintf(b, "  Queued behind #%d.\n\n", s.BlockedBy)
		}
		writeError(b, opts.clampError(s.Error))
		if len(s.RequiredApprovers) > 0 {
			fmt.Fprintf(b, "👥 **Required approvers:** %s\n\n", strings.Join(s.RequiredApprovers, ", "))
		}
		if s.PlanSummary != "" && opts.includeSummary {
			fmt.Fprintf(b, "<details><summary>Summary (%d add, %d change, %d delete, %d replace)</summary>\n\n```diff\n%s\n```\n\n</details>\n\n",
				s.Counts.Add, s.Counts.Change, s.Counts.Delete, s.Counts.Replace,
				s.PlanSummary)
		}
		if s.PlanDiff != "" && opts.includeDiff {
			b.WriteString("<details><summary>Diff</summary>\n\n```diff\n")
			b.WriteString(s.PlanDiff)
			if !strings.HasSuffix(s.PlanDiff, "\n") {
				b.WriteString("\n")
			}
			b.WriteString("```\n\n</details>\n\n")
		}
		if s.FullPlan != "" && opts.includeFullPlan {
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
	writeDroppedSectionNote(b, dropped)
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
		case summary.StatusPlanned:
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

// writeError renders a stack's error. A single line stays inline; anything
// multi-line goes in a fenced block, because engine diagnostics are the part
// an operator acts on and folding them into one markdown line either breaks
// the layout or loses everything after the first newline.
func writeError(b *strings.Builder, msg string) {
	msg = strings.TrimRight(msg, "\n")
	if msg == "" {
		return
	}
	if !strings.Contains(msg, "\n") {
		fmt.Fprintf(b, "  **Error:** %s\n\n", msg)
		return
	}
	b.WriteString("  **Error:**\n\n```\n")
	// A ``` run in the message would close the fence early and let the rest
	// render as markup. Engine errors quote resource names and properties
	// that come from the PR, so this text is not trusted to be fence-safe.
	b.WriteString(strings.ReplaceAll(msg, "```", "`\u200b`\u200b`"))
	b.WriteString("\n```\n\n")
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
	case summary.StatusPlanned:
		return "✅ planned"
	}
	return string(s.Status)
}

func envOrDash(env string) string {
	if env == "" {
		return "-"
	}
	return env
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// runRef renders the "run #N" header token. GitHub auto-links a bare commit
// SHA, but never the run number, so when the CI run URL is known we hyperlink
// the run number straight to its run; otherwise it stays plain text. This is
// the single link to the run - callers no longer append a separate
// "[View run]" bit.
func runRef(runNumber int, ciRunURL string) string {
	if ciRunURL != "" {
		return fmt.Sprintf("[run #%d](%s)", runNumber, ciRunURL)
	}
	return fmt.Sprintf("run #%d", runNumber)
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
			summary.StatusPlanned: 1,
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
