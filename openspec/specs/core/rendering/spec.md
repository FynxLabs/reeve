# PR Comment Rendering

## Single comment, edited in place

Identified by hidden HTML marker (`<!-- reeve:pr-comment:v1 -->`). VCS
adapter's `UpsertComment` handles find-or-create via marker match. If the
VCS adapter reports `CommentCapabilities.SupportsEdit == false`, append
fallback kicks in - out of scope until a non-GitHub adapter ships.

## Layout

Header line: `## <status-icon> reeve · <op> · run #<n> · [commit <sha>]`
followed by total counts, duration, and a link to the CI run.

Table summarizing all affected stacks with columns:
`Stack | Env | Add | Change | Delete | Replace | Status`.

Per-stack sections below the table - status-grouped sort order (blocked,
ready, no-op last), each with: required approvers (if any), then collapsed
`<details>` for Summary and Full plan output. No-op stacks collapse to a
single table line with no section.

A help comment is upserted separately under marker `<!-- reeve:help -->`,
listing available commands. An auto-ready comment is upserted under
`<!-- reeve:ready -->` when `auto_ready: true` and plan succeeds.

Apply comment mirrors preview structure, adds durations, floats failures
to top.

## Safety rails

- Secrets marked by Pulumi `[secret]` are redacted before render.
- All rendered output funnels through `internal/core/redact` - no output
  path bypasses redaction.
- Replacement counts > 0 trigger a prominent warning block.

## Sort orders

- `status_grouped` (default): blocked → ready → no-op.
- `alphabetical`: by `{project}/{stack}`.
- `env_priority`: configured priority order (e.g. `prod > staging > dev`).

## Testing

Golden files. Every rendering change requires a new golden file + diff
review.
