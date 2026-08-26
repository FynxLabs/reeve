package render

import (
	"fmt"
	"strings"

	"github.com/reeveops/reeve/internal/core/summary"
)

// Comment overflow. A board that exceeds GitHub's size limit continues into
// further comments instead of dropping per-stack detail.
//
// The trim ladder in trim.go is still what runs when overflow is off, and its
// cheapest rung still runs when overflow is on: the raw engine blob is dropped
// before paginating, because it is hundreds of KB per stack that nobody reads
// from a comment and paginating it would multiply the part count for nothing.
// Everything a reviewer actually reads - diffs, summaries, errors - paginates.

// Overflow modes.
const (
	OverflowDrop     = "drop"     // default: one comment, trimmed by the ladder
	OverflowContinue = "continue" // spill into further comments
)

// Split modes.
const (
	SplitDivided = "divided" // default: balance stacks evenly across the parts needed
	SplitStack   = "stack"   // fill each part to capacity in render order
	SplitGroup   = "group"   // one part per status group
)

// defaultMaxParts bounds how many comments one board may occupy. A run large
// enough to exceed this is pathological, and burying a PR under an unbounded
// number of comments is worse than naming what did not fit.
const defaultMaxParts = 10

// OverflowConfig is the renderer's view of comments.overflow. The zero value is
// the default: no overflow, so a caller that does not set it keeps today's
// single-comment behavior exactly.
type OverflowConfig struct {
	Mode     string
	Split    string
	MaxParts int
}

// enabled reports whether a board should paginate rather than trim.
func (c OverflowConfig) enabled() bool { return c.Mode == OverflowContinue }

// splitMode resolves the split, defaulting to divided.
func (c OverflowConfig) splitMode() string {
	switch c.Split {
	case SplitStack, SplitGroup:
		return c.Split
	default:
		return SplitDivided
	}
}

// maxParts resolves the cap, defaulting to defaultMaxParts.
func (c OverflowConfig) maxParts() int {
	if c.MaxParts > 0 {
		return c.MaxParts
	}
	return defaultMaxParts
}

// Part is one comment of a board. Part 1 carries the table and the marker the
// board has always used; later parts carry their own marker and only sections.
type Part struct {
	// Ordinal is 1-based.
	Ordinal int
	// Marker is what the caller upserts this part under.
	Marker string
	// Body is the rendered comment.
	Body string
	// Trim reports what this part had to drop. Under overflow it is normally
	// empty past the engine blob: only a stack too large for a whole comment
	// forces a part to trim.
	Trim Trim
	// Stacks are the refs whose detail this part carries, for the run log.
	Stacks []string
	// OmittedStacks are the refs excluded by max_parts. The last part carries
	// them so the caller can write their complete detail to the run log.
	OmittedStacks []string
}

// PartMarker is the marker for one part of a board. Part 1 is byte-identical to
// the board's own marker, so a PR already carrying a board keeps having it
// edited rather than orphaned by a marker change, and a run that fits in one
// comment is indistinguishable from today.
func PartMarker(boardMarker string, part int) string {
	if part <= 1 {
		return boardMarker
	}
	// Insert the ordinal before the closing delimiter so a part marker stays in
	// the board's own namespace and a prefix match finds every part.
	trimmed := strings.TrimSuffix(boardMarker, " -->")
	return trimmed + fmt.Sprintf(":part%d -->", part)
}

// PartMarkerPrefix is what a caller matches on to find every part of a board,
// including the surplus parts a shrinking run has to delete.
func PartMarkerPrefix(boardMarker string) string {
	return strings.TrimSuffix(boardMarker, " -->")
}

// splitStacks divides stacks into groups, one per part, honoring the split mode.
// fits reports whether a given group renders within the limit as part n of m.
//
// Every mode returns whole stacks: a stack's detail is never split across two
// parts, so a reader never has to stitch one stack back together.
func splitStacks(stacks []summary.StackSummary, mode string, sortMode string, maxParts int,
	fits func(group []summary.StackSummary, part, total int) bool,
) [][]summary.StackSummary {
	switch mode {
	case SplitGroup:
		return splitByGroup(stacks, sortMode, maxParts, fits)
	case SplitStack:
		return splitByCapacity(sorted(stacks, sortMode), maxParts, fits)
	default:
		return splitDivided(sorted(stacks, sortMode), maxParts, fits)
	}
}

// splitDivided finds the fewest parts that fit and spreads stacks evenly across
// them.
//
// Even distribution is the point of the default. Filling greedily leaves a last
// part holding one stack, which reads as though something went wrong; two parts
// of twenty read as a deliberate split.
func splitDivided(ordered []summary.StackSummary, maxParts int,
	fits func([]summary.StackSummary, int, int) bool,
) [][]summary.StackSummary {
	for total := 1; total <= maxParts; total++ {
		groups := distribute(ordered, total)
		ok := true
		for i, g := range groups {
			if !fits(g, i+1, total) {
				ok = false
				break
			}
		}
		if ok {
			return groups
		}
	}
	// Nothing fit within the cap. Fall back to capacity packing, which drops
	// what will not fit rather than returning parts known to be oversize.
	return splitByCapacity(ordered, maxParts, fits)
}

// distribute spreads items across n groups as evenly as possible, keeping order.
// The first remainder groups take one extra, so sizes differ by at most one.
func distribute(items []summary.StackSummary, n int) [][]summary.StackSummary {
	if n <= 1 {
		return [][]summary.StackSummary{items}
	}
	groups := make([][]summary.StackSummary, 0, n)
	base, extra := len(items)/n, len(items)%n
	start := 0
	for i := 0; i < n; i++ {
		size := base
		if i < extra {
			size++
		}
		groups = append(groups, items[start:start+size])
		start += size
	}
	return groups
}

// splitByCapacity fills each part to capacity in render order.
//
// The part count is not known until the packing is done, but the rendered
// footer names it, so the pass runs twice: once to learn the count, once to pack
// against the real footer. Packing against a guessed count could overflow the
// final part by exactly the footer's width.
func splitByCapacity(ordered []summary.StackSummary, maxParts int,
	fits func([]summary.StackSummary, int, int) bool,
) [][]summary.StackSummary {
	pack := func(total int) [][]summary.StackSummary {
		var groups [][]summary.StackSummary
		var cur []summary.StackSummary
		for _, s := range ordered {
			trial := append(append([]summary.StackSummary{}, cur...), s)
			if len(cur) > 0 && !fits(trial, len(groups)+1, total) {
				if len(groups)+1 >= maxParts {
					// The part being filled is the last one allowed. Close it
					// and stop: the stacks left over are reported as dropped
					// rather than silently packed into a part past the cap.
					groups = append(groups, cur)
					return groups
				}
				groups = append(groups, cur)
				cur = []summary.StackSummary{s}
				continue
			}
			cur = trial
		}
		if len(cur) > 0 {
			groups = append(groups, cur)
		}
		return groups
	}
	// Iterate to a fixed point. Crossing a digit boundary (9 parts to 10) can
	// make the real footer wider than the first estimate and require one more
	// part; stopping after two passes would render that new count unmeasured.
	total := 1
	for i := 0; i <= maxParts; i++ {
		groups := pack(total)
		if len(groups) <= 1 || len(groups) == total {
			return groups
		}
		total = len(groups)
	}
	return pack(total)
}

// splitByGroup gives each status group its own part, in the order the per-stack
// sections already sort. A reader after failures opens exactly one comment.
//
// A group too large for one part is packed by the capacity rule, so it becomes
// several parts rather than an oversize one.
func splitByGroup(stacks []summary.StackSummary, sortMode string, maxParts int,
	fits func([]summary.StackSummary, int, int) bool,
) [][]summary.StackSummary {
	buckets := map[summary.Status][]summary.StackSummary{}
	for _, s := range sorted(stacks, sortMode) {
		buckets[s.Status] = append(buckets[s.Status], s)
	}
	var groups [][]summary.StackSummary
	for _, st := range []summary.Status{
		summary.StatusError, summary.StatusBlocked, summary.StatusPlanned, summary.StatusNoOp,
	} {
		b := buckets[st]
		if len(b) == 0 {
			continue
		}
		if fits(b, len(groups)+1, 0) {
			groups = append(groups, b)
			continue
		}
		// This group alone is oversize; pack it into as many parts as it needs.
		groups = append(groups, splitByCapacity(b, maxParts-len(groups), fits)...)
	}
	if len(groups) > maxParts {
		groups = groups[:maxParts]
	}
	return groups
}

// boardRenderer is what a paginating board needs from a concrete renderer: a
// body for a given stack subset and options, and the ladder for the case where
// a part still does not fit.
type boardRenderer struct {
	// render builds a body from the given stacks under the given options.
	render func(stacks []summary.StackSummary, o renderOpts) string
	// descend runs the trim ladder over the given stacks, for a part that
	// cannot fit even with only its own stacks.
	descend func(stacks []summary.StackSummary, o renderOpts) (string, Trim)
	// keepFullPlan says the engine output is this operation's real content and
	// must paginate rather than be dropped.
	//
	// For a preview it is the raw `pulumi preview --json` blob: nobody reads it
	// from a comment, the diff carries what is readable, and paginating it would
	// multiply the part count for nothing. For an apply or refresh it is the
	// engine's own account of what changed - dropping it to avoid paginating
	// would be the comment claiming completeness it does not have.
	keepFullPlan bool
	// sortMode and view come from the board's input.
	sortMode string
	view     string
}

// paginate splits a board into parts, each within the size limit.
//
// Returns nil when the board fits in one comment, which is the common case and
// must stay byte-identical to an unpaginated render.
func paginate(r boardRenderer, stacks []summary.StackSummary, cfg OverflowConfig, boardMarker string) []Part {
	maxParts := cfg.maxParts()
	all := stacks
	reserve := len(overCapNote(len(stacks)))
	budget := func(part, total int) int {
		if part == total || total == 0 {
			return githubCommentMaxLen - reserve
		}
		return githubCommentMaxLen
	}

	// fits measures a real rendered part, not an estimate. partsTotal of 0 for
	// the group split means "count not yet known": measure against the widest
	// plausible footer so learning the real count cannot push a part over.
	partOpts := func(part, total int) renderOpts {
		budgetTotal := total
		if total == 0 {
			total = maxParts
		}
		o := renderOpts{
			includeFullPlan: r.keepFullPlan,
			includeDiff:     true,
			includeSummary:  true,
			part:            part,
			partsTotal:      total,
			suppressTable:   part > 1,
			commentLimit:    budget(part, budgetTotal) - (len(PartMarker(boardMarker, part)) - len(boardMarker)),
		}
		if part == 1 {
			// The table on part 1 indexes the whole board, not just the stacks
			// whose detail part 1 carries.
			o.tableStacksOverride = all
			o.stackParts = make(map[string]int, len(all))
			for _, stack := range all {
				o.stackParts[stack.Ref()] = maxParts
			}
		}
		return o
	}
	fits := func(group []summary.StackSummary, part, total int) bool {
		o := partOpts(part, total)
		return len(r.render(group, o)) <= o.commentLimit
	}

	groups := splitStacks(stacks, cfg.splitMode(), r.sortMode, maxParts, fits)
	if len(groups) <= 1 {
		return nil // fits in one comment; caller renders it the ordinary way
	}

	// Account for stacks the cap left out. They are named in the last part and
	// written to the log, never silently absent.
	placed := 0
	for _, g := range groups {
		placed += len(g)
	}
	overCap := len(stacks) - placed

	total := len(groups)
	stackParts := make(map[string]int, len(stacks))
	for _, stack := range stacks {
		stackParts[stack.Ref()] = 0
	}
	for i, group := range groups {
		for _, stack := range group {
			stackParts[stack.Ref()] = i + 1
		}
	}
	parts := make([]Part, 0, total)
	for i, g := range groups {
		o := partOpts(i+1, total)
		o.stackParts = stackParts
		body := r.render(g, o)
		trim := Trim{DroppedFullPlan: !r.keepFullPlan}
		if len(body) > o.commentLimit {
			// A single stack whose detail exceeds a whole comment cannot be
			// paginated. Trim this part alone so the rest of the board still
			// paginates cleanly and only the pathological stack loses content.
			body, trim = r.descend(g, o)
		}
		if i == total-1 && overCap > 0 {
			body += overCapNote(overCap)
		}
		partMarker := PartMarker(boardMarker, i+1)
		body = strings.Replace(body, boardMarker, partMarker, 1)
		refs := make([]string, 0, len(g))
		for _, s := range g {
			refs = append(refs, s.Ref())
		}
		part := Part{
			Ordinal: i + 1,
			Marker:  partMarker,
			Body:    body,
			Trim:    trim,
			Stacks:  refs,
		}
		if i == total-1 && overCap > 0 {
			placedRefs := make(map[string]bool, placed)
			for _, group := range groups {
				for _, stack := range group {
					placedRefs[stack.Ref()] = true
				}
			}
			for _, stack := range stacks {
				if !placedRefs[stack.Ref()] {
					part.OmittedStacks = append(part.OmittedStacks, stack.Ref())
				}
			}
		}
		parts = append(parts, part)
	}
	return parts
}

// overCapNote names the stacks the part cap left out of the comments entirely.
// They appear in the table on part 1 but have no detail anywhere, so the log is
// their only record.
func overCapNote(n int) string {
	noun := "stacks"
	if n == 1 {
		noun = "stack"
	}
	return fmt.Sprintf("\n---\n\n> ⚠️ **%d %s omitted** past the comment limit for this board. Their detail is in the run log.\n", n, noun)
}
