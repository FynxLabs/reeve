# section-per-commit

## Why

`comments.style: section` was specified as a split by operation: one preview
comment under `<!-- reeve:pr-comment:v1 -->`, one apply comment under
`<!-- reeve:apply:v1 -->`, for the life of the PR. Both are global, so both are
overwritten in place by every later run.

On a PR that previews, errors, and then attempts an apply, that produces two
permanent boards the reader has to jump between, and neither one holds the run
it was reading. A preview that planned 8 stacks is replaced by a later preview's
16 error rows, so the plan is no longer recoverable from the PR at all - the
comment that recorded it is gone.

Operators choose `section` to keep separate boards. Splitting by operation is
the wrong axis: it makes exactly two boards no matter how many runs happen, and
guarantees each one is overwritten.

## What

1. Key a `section` board to the commit SHA, not to the operation. Preview,
   re-preview, and apply of one SHA share one board, edited in place.
2. A new SHA mints a new board. A previous SHA's board is never edited again, so
   the plan it recorded stays readable for the life of the PR.
3. `replace` keeps its meaning: one board per PR, always edited in place.
4. Retire the operation-split marker `<!-- reeve:apply:v1 -->`. Comments already
   posted under it are left in place.

## Also fixed

The size-limit trim note said "see the full run output for the complete plan"
while nothing wrote the plan there - `PlanDiff` and `FullPlan` only ever reached
the comment. The note named an output that never held it.

Worse, the last resort was a byte truncate. It sliced through tables, code
fences, and `<details>` tags, so GitHub rendered the remainder as garbage, and
it discarded whatever stacks fell past the cutoff with no account of them. Stack
errors and plan summaries were not on the trim ladder at all, so a run with many
failing stacks went straight from "drop the diffs" to that byte cut.

The ladder now runs: engine output, diff, summaries, clamped errors, whole
sections, table rows. Every rung removes whole units and reports what went, and
the run pipeline logs whatever was reported.

## Scope

- In: the `section` marker key, the size-limit trim ladder, the logging of
  dropped content, their specs, and their docs.
- Out: `replace`, `append`, the timeline comments, `comments.sort`,
  `comments.stack_view`, help and ready comments, refresh and explain markers.

## Compatibility

No config change. A PR mid-flight under the old `section` keeps its two existing
comments untouched and gets a per-SHA board from the next run on.
