# Design

## Part markers

Part 1 keeps the board's existing marker, byte-identical. Later parts append an
ordinal: `<!-- reeve:pr-comment:v1:part2 -->`, and under `section` the SHA comes
first, `<!-- reeve:pr-comment:v1:abc1234:part2 -->`.

Part 1 being unchanged is what makes this safe to ship: a PR mid-flight keeps
having its board edited rather than being orphaned, and a run that fits in one
comment is byte-identical to today.

`UpsertComment` already does find-or-create by marker, so each part needs no new
write path - only its own marker.

## Splitting

The table always lives on part 1, whole. It is the index: a reader must be able
to see every stack in one place and know which part holds its detail. Per-stack
sections are what paginates.

Each part carries `part N of M` in its header, and part 1 links forward. A part
is never split mid-section: a stack's detail is contiguous.

### divided (default)

Find the smallest part count M where an even split fits, then distribute stacks
evenly. Binary search on M, measuring real rendered parts.

Even distribution is the point: filling greedily leaves a last part with one
stack, which reads as though something went wrong. Two parts of 20 read as a
deliberate split.

### stack

Fill each part to capacity in render order, then start the next. Fewer parts than
`divided` for the same content, at the cost of a ragged last part.

### group

One part per status group, in the order the sections already sort: failures,
blocked, applied, no-op. A reader after failures opens exactly one part.

A single group can still exceed the limit, in which case that group's part
splits again by the `stack` rule and keeps its group label.

## When even one stack does not fit

A single stack whose detail exceeds a whole comment cannot be paginated by
stack. Its part falls back to the trim ladder for that stack alone - diff, then
summary, then clamped error - so the rest of the run still paginates cleanly and
only the pathological stack loses content.

## Stale parts

The run records how many parts it wrote. A later run needing fewer must delete
the surplus, or a stale part keeps claiming stacks the run no longer has.

`DeleteCommentsByMarkerPrefix` on the VCS client: list, match the board's part
prefix, delete those with an ordinal above the current count. Deleting reeve's
own comment by its own marker is the narrowest capability that does the job.

Failing to delete is logged, not fatal: the run's real work has already shipped,
and a stale part is a reporting defect, not a reason to fail the run.

## Alternatives rejected

One comment per stack, always. Creating a comment fires an `issue_comment`
webhook where editing is silent - the reason the timeline consolidated per
commit - and 40 comments per run is unreadable regardless.

Put the table on every part. Duplicates the largest fixed cost on every comment,
which is what forces the extra parts.
