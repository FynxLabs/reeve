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
