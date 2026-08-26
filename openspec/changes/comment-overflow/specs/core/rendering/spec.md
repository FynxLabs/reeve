# Rendering delta

## ADDED Requirements

### Requirement: An oversize board may continue into further comments

When `comments.overflow.mode` is `continue` and a board exceeds GitHub's comment
size limit, reeve MUST continue the board into further comments rather than drop
per-stack detail. Diffs, plan summaries, and error text MUST NOT be dropped in
this mode, except for stacks beyond `comments.overflow.max_parts`, which the
"Part count is bounded" requirement below governs.

The raw engine output MAY still be dropped before paginating. It is hundreds of
KB per stack and paginating it would multiply the part count without adding
anything a reviewer reads.

The first part MUST carry the board's existing marker, byte-identical, so a PR
already carrying a board keeps having it edited rather than orphaned. Later parts
MUST carry a marker derived from it and the part ordinal.

A board that fits in one comment MUST be byte-identical to the same board
rendered with overflow disabled.

### Requirement: Every part is readable on its own

The stack table MUST appear whole on the first part and MUST NOT be repeated on
later parts. It is the index of the run: a reader MUST be able to see every stack
retained within `max_parts`, and which part holds its detail, from the first
comment. Stacks dropped by the cap MUST be named there with a count.

When a board spans more than one part, every part MUST state its ordinal and the
total, and the first part MUST link to the others. A single-part board MUST NOT
carry this metadata, so it stays byte-identical to the same board rendered with
overflow disabled.

A single stack's detail MUST NOT be split across two parts.

### Requirement: Split modes

`comments.overflow.split` selects how stacks are distributed:

- `divided` (default) MUST use the fewest parts that fit and distribute stacks
  evenly across them, so no part carries a remainder of one or two stacks while
  another is full.
- `stack` MUST fill each part to capacity in render order before starting the
  next.
- `group` MUST place each status group in its own part, in the order the per-stack
  sections already sort. A group that alone exceeds the limit MUST split by the
  `stack` rule while keeping its group label.

### Requirement: A stack too large for any part still trims

A single stack whose detail exceeds a whole comment cannot be paginated. Its part
MUST fall back to the trim ladder for that stack alone - diff, then summary, then
clamped error - so other parts are unaffected and only that stack loses content.
The loss MUST be stated in the part and written to the run log.

### Requirement: Surplus parts are removed

A run needing fewer parts than the previous run MUST delete the surplus comments.
A stale part left on the PR claims stacks the current run does not have.

Failing to delete a surplus part MUST be logged and MUST NOT fail the run: the
run's real work has already shipped, and a stale part is a reporting defect.

### Requirement: Part count is bounded

`comments.overflow.max_parts` MUST bound how many comments one board can occupy.
This cap is the sole exception to the no-drop guarantee above: stacks past it
MUST be dropped from the comments, stated in the last part with a count, and
written to the run log.
