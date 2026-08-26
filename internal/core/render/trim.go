package render

import (
	"fmt"
	"strings"

	"github.com/reeveops/reeve/internal/core/summary"
)

// The trim ladder. GitHub rejects a comment over its size limit with a
// non-recoverable 422, so a renderer has to guarantee the body fits. It does
// that by dropping content in a fixed order, least-read first, and reporting
// what went so the caller can write it to the run log the comment's note points
// at.
//
// The ladder never cuts the document mid-structure. The original last resort was
// a byte truncate, which sliced through tables, code fences, and <details> tags
// - GitHub rendered the remainder as garbage - and dropped whatever fell past
// the cutoff with no account of it. Every rung here removes whole units and
// names what it removed.
//
// Rungs, in order:
//
//  1. full engine output - the raw plan blob, never read from a comment (silent)
//  2. per-stack diff
//  3. plan summaries
//  4. error text, clamped per stack rather than dropped
//  5. whole per-stack sections, keeping every stack's table row
//  6. table rows, the floor - the only rung where a stack goes unnamed
//
// Rung 1 is silent because nothing a reviewer reads is lost. From rung 2 on the
// body carries a note naming what is missing.

// perStackErrorBudget caps one stack's error text once errors have to be
// clamped. Enough for a stack trace's first frames, small enough that dozens of
// failing stacks still fit.
const perStackErrorBudget = 2_000

// Trim names what a renderer had to drop to fit the comment limit, so the
// caller can put the dropped content where the comment's note says it is.
type Trim struct {
	// DroppedFullPlan is the raw engine blob. For a preview it is silent -
	// nobody reads `pulumi preview --json` from a PR comment and the diff
	// survives - but for an apply or refresh it is the engine's own account of
	// what changed and must be logged.
	DroppedFullPlan bool
	// DroppedDiff means the per-stack diff is gone. That is what reviewers
	// read, so the caller must log it.
	DroppedDiff bool
	// DroppedSummary means per-stack plan summaries are gone.
	DroppedSummary bool
	// ClampedErrors means stack error text was shortened to fit.
	ClampedErrors bool
	// DroppedSections counts stacks reduced to a table row with no detail.
	// Their errors and summaries went with them.
	DroppedSections int
	// DroppedRows counts stacks missing from the table entirely, so they are
	// not named anywhere in the comment and the log is their only record.
	DroppedRows int
}

// Lost reports whether anything a reviewer reads was removed, which is what
// obliges the caller to write the dropped content to the log.
func (t Trim) Lost() bool {
	return t.DroppedDiff || t.DroppedSummary || t.ClampedErrors ||
		t.DroppedSections > 0 || t.DroppedRows > 0
}

// renderOpts gates each rung. A renderer must honor every field, or the ladder
// silently stops shrinking at that rung and the size guarantee is lost.
type renderOpts struct {
	includeFullPlan bool
	includeDiff     bool
	includeSummary  bool
	// errorBudget caps each stack's rendered error text, 0 meaning no cap.
	errorBudget int
	// keepStacks, when non-nil, is the set of stack refs whose per-stack
	// section is rendered. Stacks outside it appear in the table only.
	keepStacks map[string]bool
	// tableLimit caps how many rows the table renders, 0 meaning no cap.
	tableLimit     int
	truncationNote string
}

// sectionRendered reports whether ref gets a per-stack section under opts.
func (o renderOpts) sectionRendered(ref string) bool {
	return o.keepStacks == nil || o.keepStacks[ref]
}

// clampError shortens a stack error to the opts budget, marking the cut. An
// error is never dropped outright: on a failed run it is the whole reason the
// comment is worth reading.
func (o renderOpts) clampError(msg string) string {
	if o.errorBudget <= 0 || len(msg) <= o.errorBudget {
		return msg
	}
	const note = "\n… error truncated; see the run log."
	cut := o.errorBudget - len(note)
	if cut < 0 {
		cut = 0
	}
	return msg[:cut] + note
}

// writeDroppedSectionNote accounts for stack sections the size limit removed.
// Silence would read as "these stacks had nothing to report", so the count is
// stated and the reader is pointed at the log that does carry them.
func writeDroppedSectionNote(b *strings.Builder, dropped int) {
	if dropped == 0 {
		return
	}
	noun := "stacks"
	if dropped == 1 {
		noun = "stack"
	}
	fmt.Fprintf(b, "---\n\n> ⚠️ **%d %s omitted** from the per-stack detail above to fit GitHub's comment size limit. They remain in the table; their full detail is in the run log.\n\n", dropped, noun)
}

// tableRows applies the view mode, sort, and row cap, returning the rows to
// render and how many were held back.
func tableRows(order stackOrder, stacks []summary.StackSummary, view, sortMode string, limit int) (rows []summary.StackSummary, hidden int) {
	rows = order(tableStacks(stacks, view), sortMode)
	if limit > 0 && len(rows) > limit {
		hidden = len(rows) - limit
		rows = rows[:limit]
	}
	return rows, hidden
}

// writeHiddenRowNote names the rows the floor rung removed. Those stacks appear
// nowhere else in the comment. columns is the table's column count, so the note
// spans exactly one row: the preview table has 7, the apply and refresh tables 8
// (they add Duration), and a short row renders ragged.
func writeHiddenRowNote(b *strings.Builder, hidden, columns int) {
	if hidden == 0 {
		return
	}
	fmt.Fprintf(b, "| _+%d more stacks - see the run log_ |%s\n",
		hidden, strings.Repeat(" |", columns-1))
}

// stackOrder returns stacks in the order a renderer emits its per-stack
// sections. descend keeps the retained-section prefix in this same order, so the
// sections rung drops the ones the reader sees last rather than a prefix of a
// different sort.
type stackOrder func(stacks []summary.StackSummary, sortMode string) []summary.StackSummary

// sectionRefs lists the stack refs that get a per-stack section, in the render
// order the caller supplies. No-ops have no section.
func sectionRefs(order stackOrder, stacks []summary.StackSummary, sortMode string) []string {
	ordered := order(stacks, sortMode)
	refs := make([]string, 0, len(ordered))
	for _, s := range ordered {
		if s.Status == summary.StatusNoOp {
			continue
		}
		refs = append(refs, s.Ref())
	}
	return refs
}

// omittedPhrase describes a Trim in the reviewer-facing banner. Deriving the
// wording from the reported fields is what keeps the two honest: a rung that
// sets a field it does not describe, or describes a drop it did not make, is not
// expressible.
func omittedPhrase(t Trim, fullPlanLabel string, silentFullPlan bool) string {
	var dropped []string
	if t.DroppedFullPlan && !silentFullPlan {
		dropped = append(dropped, fullPlanLabel)
	}
	if t.DroppedDiff {
		dropped = append(dropped, "per-stack diff")
	}
	if t.DroppedSummary {
		dropped = append(dropped, "plan summaries")
	}
	phrase := strings.Join(dropped, ", ")

	var shortened []string
	if t.ClampedErrors {
		shortened = append(shortened, "errors shortened")
	}
	if t.DroppedSections > 0 {
		shortened = append(shortened, "some stack detail")
	}
	if t.DroppedRows > 0 {
		shortened = append(shortened, "some table rows")
	}
	if len(shortened) > 0 {
		if phrase != "" {
			phrase += "; "
		}
		phrase += strings.Join(shortened, "; ")
	}
	return phrase
}

// descend walks the ladder for one renderer. render must honor every renderOpts
// field; note wraps the omission phrase in the reviewer-facing banner.
//
// Returning the first body that fits keeps the rungs honest: a rung is only
// climbed past when it demonstrably did not fit, measured on the real rendered
// body rather than estimated.
func descend(
	render func(renderOpts) string,
	note func(omitted string) string,
	order stackOrder,
	stacks []summary.StackSummary,
	view, sortMode string,
	fullPlanLabel string,
	silentFullPlan bool,
) (string, Trim) {
	fits := func(o renderOpts) (string, bool) {
		body := render(o)
		return body, len(body) <= githubCommentMaxLen
	}
	// noteFor derives the banner from what trim says is gone, so the text and
	// the reported fields cannot disagree.
	noteFor := func(t Trim) string {
		phrase := omittedPhrase(t, fullPlanLabel, silentFullPlan)
		if phrase == "" {
			return ""
		}
		return note(phrase)
	}

	if body, ok := fits(renderOpts{includeFullPlan: true, includeDiff: true, includeSummary: true}); ok {
		return body, Trim{}
	}

	// Rung 1: the raw engine blob. Silent for a preview, where the diff the
	// reviewer reads survives; named for apply and refresh, where this IS the
	// engine's output.
	trim := Trim{DroppedFullPlan: true}
	if body, ok := fits(renderOpts{
		includeDiff: true, includeSummary: true, truncationNote: noteFor(trim),
	}); ok {
		return body, trim
	}

	// Rung 2: the diff. Content reviewers read, so every rung from here stamps
	// a note.
	trim.DroppedDiff = true
	if body, ok := fits(renderOpts{includeSummary: true, truncationNote: noteFor(trim)}); ok {
		return body, trim
	}

	// Rung 3: summaries. A run with many failing stacks can exceed the limit on
	// summaries alone.
	trim.DroppedSummary = true
	if body, ok := fits(renderOpts{truncationNote: noteFor(trim)}); ok {
		return body, trim
	}

	// Rung 4: clamp errors rather than drop them.
	trim.ClampedErrors = true
	if body, ok := fits(renderOpts{
		errorBudget: perStackErrorBudget, truncationNote: noteFor(trim),
	}); ok {
		return body, trim
	}

	// Rung 5: whole sections, keeping every table row. Sections are kept in
	// render order, so the stacks a reader most needs - failures sort first -
	// are the ones retained.
	//
	// The note grows by naming the dropped sections, so it is set from a trim
	// that already claims them before the search measures. Searching against a
	// shorter note than the one rendered would put the body back over the limit
	// by exactly that difference.
	refs := sectionRefs(order, stacks, sortMode)
	probe := trim
	probe.DroppedSections = len(refs)
	base := renderOpts{errorBudget: perStackErrorBudget, truncationNote: noteFor(probe)}
	keep, droppedSections := fitSections(render, base, refs)
	trim.DroppedSections = droppedSections
	base.keepStacks = keep
	if droppedSections > 0 {
		// Re-derive: fewer sections dropped than the probe assumed does not
		// change the wording, but a zero count would, and must not claim a drop
		// that did not happen.
		base.truncationNote = noteFor(trim)
	}
	if body, ok := fits(base); ok {
		return body, trim
	}

	// Rung 6, the floor: the table itself is oversize, so rows go too. Same
	// pre-claim as rung 5 - the note names the dropped rows, and it has to be
	// the note the search measures against.
	total := len(tableStacks(stacks, view))
	probe = trim
	probe.DroppedRows = total
	base.truncationNote = noteFor(probe)
	rows, droppedRows := fitTable(render, base, total)
	trim.DroppedRows = droppedRows
	base.tableLimit = rows
	base.truncationNote = noteFor(trim)
	return render(base), trim
}

// fitSections picks the per-stack sections that fit, keeping a prefix of render
// order, and returns them with the number dropped.
//
// A prefix, not a subset: sections are kept in render order and the first one
// that does not fit ends it. Skipping a large section to squeeze in a later
// small one would make the omission order arbitrary to a reader scanning the
// comment top to bottom.
//
// Because a longer prefix is never smaller than a shorter one, the fitting
// length is monotonic and found by binary search - the same shape as fitTable.
func fitSections(render func(renderOpts) string, base renderOpts, refs []string) (map[string]bool, int) {
	build := func(n int) map[string]bool {
		keep := make(map[string]bool, n)
		for _, ref := range refs[:n] {
			keep[ref] = true
		}
		return keep
	}
	fits := func(n int) bool {
		probe := base
		probe.keepStacks = build(n)
		return len(render(probe)) <= githubCommentMaxLen
	}

	// lo is the largest known-good prefix length. 0 sections is always
	// renderable - it is the table plus the note - so lo starts there.
	lo, hi := 0, len(refs)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if fits(mid) {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return build(lo), len(refs) - lo
}

// fitTable returns the row count that fits and how many rows that drops, found
// by binary search. base must already carry the note the caller will render
// with, or the measurement is against a different document than the one posted.
func fitTable(render func(renderOpts) string, base renderOpts, total int) (rows, dropped int) {
	// tableLimit of 0 means "no cap", so the search never probes 0. lo starts
	// at 1 and a table that cannot fit even one row falls out below.
	lo, hi := 1, total
	for lo < hi {
		mid := (lo + hi + 1) / 2
		probe := base
		probe.tableLimit = mid
		if len(render(probe)) <= githubCommentMaxLen {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	probe := base
	probe.tableLimit = lo
	if len(render(probe)) > githubCommentMaxLen {
		// Not even one row fits: the fixed parts - header, banner, note - are
		// themselves oversize. Nothing here can fix that; report every stack as
		// dropped so the caller logs all of them.
		return lo, total
	}
	return lo, total - lo
}
