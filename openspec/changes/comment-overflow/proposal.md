# comment-overflow

## Why

GitHub rejects a comment over 65,536 chars, so an oversize board has to lose
something. Today it drops content in order: engine output, the diff, plan
summaries, clamped error text, whole stack sections, then table rows. On a PR
with 40 stacks the reviewer is told to read the run log for most of what they
came for.

Dropping content is the wrong trade when a second comment is available. A board
that spills into continuation comments loses nothing.

## What

Add a `comments.overflow` section. When a board exceeds the size limit it
continues into more comments instead of dropping stack detail, up to a
`max_parts` cap; stacks past the cap are dropped and named.

Three split modes:

- `divided` (default) - balance stacks evenly across the parts needed, so two
  parts carry ~20 stacks each rather than 39 and 1.
- `stack` - fill each part to capacity in render order, then continue.
- `group` - one part per status group: failures, blocked, applied, no-op.

Off by default (`overflow.mode: drop`), because reeve posting N comments on a PR
is a visible behavior change for every existing user.

## Scope

- In: the `comments.overflow` config section, part markers, the split modes,
  deleting parts a shrinking run no longer needs, spec and docs.
- Out: the existing trim ladder, which still runs under `drop` and still runs
  under `continue` for the rungs above per-stack sections; the timeline
  comments; `comments.style`.

## Interaction with the trim ladder

Under `continue`, the cheap rungs still apply first: the raw engine blob is
still dropped, because it is hundreds of KB per stack that nobody reads from a
comment and paginating it would mean dozens of parts. Diffs, summaries, and
errors paginate instead of being dropped, except for stacks past `max_parts`,
which are dropped and named.

Under `drop` nothing changes.

## Stale parts

A run that needs fewer parts than the last one must remove the extra comments,
or a stale part sits on the PR claiming stacks that are no longer in the run.
This needs a delete-by-marker capability on the VCS client, which does not exist
today.
