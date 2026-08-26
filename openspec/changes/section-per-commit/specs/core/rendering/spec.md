# Rendering delta

## MODIFIED Requirements

### Requirement: `comments.style`

Controls how reeve posts dashboard comments. Three modes:

- `replace` (default) MUST upsert a single comment per pull request under
  `<!-- reeve:pr-comment:v1 -->`. Every operation edits that comment.
- `section` MUST upsert one comment per commit SHA, under a marker derived from
  the dashboard marker and the short SHA. Preview and apply of the same SHA MUST
  share that comment and edit it in place. A new SHA MUST mint a new comment, and
  a previous SHA's comment MUST NOT be edited again, so the plan it recorded
  stays readable for the life of the PR.
- `append` MUST post a new comment on every run without editing the previous one.

Under `replace` the marker MUST remain byte-identical to
`<!-- reeve:pr-comment:v1 -->`, so a PR already running under it keeps having its
comment edited rather than orphaned.

`section` MUST NOT split by operation. The marker `<!-- reeve:apply:v1 -->` is
retired; reeve MUST NOT write it, and comments already posted under it MUST be
left in place.

## ADDED Requirements

### Requirement: A trimmed comment stays a well-formed document

Trimming a comment to fit the size limit MUST remove whole units - an engine
output block, a diff, a summary, a per-stack section, a table row. It MUST NOT
truncate the body at a byte offset, which can leave an unclosed code fence,
`<details>` block, or table row.

Content MUST be dropped in a fixed order, least-read first: full engine output,
per-stack diff, plan summaries, error text (clamped, not dropped), whole
per-stack sections, then table rows.

Dropping full engine output MUST be silent for a preview, where the diff a
reviewer reads survives. For an apply or refresh that output is the engine's own
account of what changed and MUST be named. Every later rung MUST stamp a note
naming what is missing.

Omitted per-stack sections and omitted table rows MUST be stated in the comment
with a count. A reader MUST NOT be left to infer that an omitted stack had
nothing to report.

### Requirement: A trim note must point at content that exists

Whatever a renderer reports dropping MUST be written to the run log, so the
note's pointer at the full run output holds what it promises. A stack error MUST
be logged whenever it was clamped or its section was dropped, and a stack absent
from the table MUST be logged, because the comment names it nowhere.

Content written to the log MUST already have passed through redaction.
